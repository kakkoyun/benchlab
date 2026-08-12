package honestbench

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
)

type timerMask uint8

const (
	timerRunning timerMask = 1 << iota
	timerStopped
	timerUnknown
)

type timerSummary struct {
	end         timerMask
	breaks      timerMask
	continues   timerMask
	workRunning bool
	workStopped bool
	workUnknown bool
	stopCalls   []*ast.CallExpr
}

func (r *runner) checkTimerLoop(loop *loopInfo, facts *scopeFacts) {
	if facts.kind == parallelScope {
		return
	}
	summary := r.analyzeTimerBlock(loop.body, timerRunning, facts, loop.recv)
	summary.end |= summary.continues
	if len(summary.stopCalls) > 0 && summary.end&timerStopped != 0 {
		call := summary.stopCalls[0]
		r.report(call.Pos(), call.End(), "stoptimer-without-starttimer", "a reachable iteration path stops timing and cannot resume before later measured work")
	}
	if summary.workStopped && !summary.workRunning && !summary.workUnknown {
		pos := loop.body.Lbrace
		if len(summary.stopCalls) > 0 {
			pos = summary.stopCalls[0].Pos()
		}
		r.report(pos, token.NoPos, "work-while-timer-stopped", "every reachable work statement in this iteration executes while the benchmark timer is stopped")
	}
}

func (r *runner) analyzeTimerBlock(block *ast.BlockStmt, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	state := input
	var result timerSummary
	for _, stmt := range block.List {
		if state == 0 {
			break
		}
		summary := r.analyzeTimerStmt(stmt, state, facts, recv)
		state = summary.end
		result = mergeTimerFacts(result, summary)
	}
	result.end = state
	return result
}

func (r *runner) analyzeTimerStmt(stmt ast.Stmt, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	if call := callExpression(stmt); call != nil {
		return r.analyzeTimerCall(call, input, facts, recv)
	}

	switch s := stmt.(type) {
	case *ast.IfStmt:
		prefix := timerSummary{end: input}
		if s.Init != nil {
			prefix = r.analyzeTimerStmt(s.Init, input, facts, recv)
		}
		condition := r.analyzeTimerExpr(s.Cond, prefix.end, facts, recv)
		state := condition.end
		thenSummary := r.analyzeTimerBlock(s.Body, state, facts, recv)
		elseSummary := timerSummary{end: state}
		if s.Else != nil {
			elseSummary = r.analyzeTimerStmt(s.Else, state, facts, recv)
		}
		result := mergeTimerFacts(prefix, condition, thenSummary, elseSummary)
		result.end = thenSummary.end | elseSummary.end
		return result
	case *ast.BlockStmt:
		return r.analyzeTimerBlock(s, input, facts, recv)
	case *ast.ForStmt:
		return r.analyzeTimerFor(s, input, facts, recv)
	case *ast.RangeStmt:
		return r.analyzeTimerRange(s, input, facts, recv)
	case *ast.SwitchStmt:
		return r.analyzeTimerSwitch(s, input, facts, recv)
	case *ast.TypeSwitchStmt:
		return r.analyzeTimerTypeSwitch(s, input, facts, recv)
	case *ast.SelectStmt:
		return r.analyzeTimerSelect(s, input, facts, recv)
	case *ast.ReturnStmt:
		result := r.analyzeTimerExprs(s.Results, input, facts, recv)
		result.end = 0
		return result
	case *ast.BranchStmt:
		if s.Label != nil {
			return timerSummary{end: input}
		}
		switch s.Tok {
		case token.BREAK:
			return timerSummary{breaks: input}
		case token.CONTINUE:
			return timerSummary{continues: input}
		default:
			return timerSummary{end: input}
		}
	case *ast.DeferStmt, *ast.EmptyStmt:
		return timerSummary{end: input}
	case *ast.AssignStmt:
		result := r.analyzeTimerExprs(s.Rhs, input, facts, recv)
		if !result.hasWork() && statementHasWork(stmt) {
			result = mergeTimerFacts(result, workSummary(result.end))
		}
		return result
	case *ast.DeclStmt:
		decl, _ := s.Decl.(*ast.GenDecl)
		result := timerSummary{end: input}
		if decl != nil {
			for _, spec := range decl.Specs {
				value, _ := spec.(*ast.ValueSpec)
				if value != nil {
					result = sequenceTimerSummaries(result, r.analyzeTimerExprs(value.Values, result.end, facts, recv))
				}
			}
		}
		if !result.hasWork() && statementHasWork(stmt) {
			result = mergeTimerFacts(result, workSummary(result.end))
		}
		return result
	}

	if statementHasWork(stmt) {
		return workSummary(input)
	}
	return timerSummary{end: input}
}

func (r *runner) analyzeTimerCall(call *ast.CallExpr, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	actual, method, family := r.testingMethod(call)
	if family == "B" && facts.activeReceiver(actual, recv) {
		switch method {
		case "StopTimer":
			return timerSummary{end: timerStopped, stopCalls: []*ast.CallExpr{call}}
		case "StartTimer", "ResetTimer":
			return timerSummary{end: timerRunning}
		default:
			return timerSummary{end: input}
		}
	}
	if callPassesActiveObject(r.pass, call, facts.activeB) {
		fn := r.calledFunction(call)
		fd := r.funcDecls[fn]
		if fn == nil || fd == nil || facts.timerHelpers[fn] {
			return timerSummary{end: timerUnknown, workUnknown: true}
		}
		facts.timerHelpers[fn] = true
		helperB := r.helperBenchmarkObjects(call, fn, fd, facts.activeB)
		summary := r.analyzeTimerBlock(fd.Body, input, facts, firstObject(helperB))
		delete(facts.timerHelpers, fn)
		return summary
	}
	if r.isWorkCall(call) {
		return workSummary(input)
	}
	return timerSummary{end: input}
}

func (r *runner) analyzeTimerFor(loop *ast.ForStmt, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	prefix := timerSummary{end: input}
	if loop.Init != nil {
		prefix = r.analyzeTimerStmt(loop.Init, input, facts, recv)
	}
	entry := prefix.end
	loopInput := entry
	combined := prefix
	var exits timerMask
	for range 4 {
		condition := r.analyzeTimerExpr(loop.Cond, loopInput, facts, recv)
		combined = mergeTimerFacts(combined, condition)
		if loop.Cond != nil {
			exits |= condition.end
		}
		body := r.analyzeTimerBlock(loop.Body, condition.end, facts, recv)
		combined = mergeTimerFacts(combined, body)
		exits |= body.breaks
		flow := body.end | body.continues
		post := timerSummary{end: flow}
		if loop.Post != nil && flow != 0 {
			post = r.analyzeTimerStmt(loop.Post, flow, facts, recv)
			combined = mergeTimerFacts(combined, post)
		}
		next := loopInput | post.end
		if next == loopInput {
			break
		}
		loopInput = next
	}
	combined.end = exits
	combined.breaks = 0
	combined.continues = 0
	return combined
}

func (r *runner) analyzeTimerRange(loop *ast.RangeStmt, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	rangeExpr := r.analyzeTimerExpr(loop.X, input, facts, recv)
	loopInput := rangeExpr.end
	combined := rangeExpr
	exits := rangeExpr.end
	for range 4 {
		body := r.analyzeTimerBlock(loop.Body, loopInput, facts, recv)
		combined = mergeTimerFacts(combined, body)
		exits |= body.breaks
		next := loopInput | body.end | body.continues
		if next == loopInput {
			break
		}
		loopInput = next
	}
	combined.end = exits
	combined.breaks = 0
	combined.continues = 0
	return combined
}

func (r *runner) analyzeTimerSwitch(stmt *ast.SwitchStmt, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	prefix := timerSummary{end: input}
	if stmt.Init != nil {
		prefix = r.analyzeTimerStmt(stmt.Init, input, facts, recv)
	}
	tag := r.analyzeTimerExpr(stmt.Tag, prefix.end, facts, recv)
	return r.analyzeTimerCaseClauses(stmt.Body.List, prefix, tag, facts, recv)
}

func (r *runner) analyzeTimerTypeSwitch(stmt *ast.TypeSwitchStmt, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	prefix := timerSummary{end: input}
	if stmt.Init != nil {
		prefix = r.analyzeTimerStmt(stmt.Init, input, facts, recv)
	}
	assignment := r.analyzeTimerStmt(stmt.Assign, prefix.end, facts, recv)
	return r.analyzeTimerCaseClauses(stmt.Body.List, prefix, assignment, facts, recv)
}

func (r *runner) analyzeTimerCaseClauses(clauses []ast.Stmt, prefix, selector timerSummary, facts *scopeFacts, recv types.Object) timerSummary {
	state := selector.end
	combined := mergeTimerFacts(prefix, selector)
	var exits timerMask
	hasDefault := false
	for _, clause := range clauses {
		cc, ok := clause.(*ast.CaseClause)
		if !ok {
			continue
		}
		if len(cc.List) == 0 {
			hasDefault = true
		}
		caseExprs := r.analyzeTimerExprs(cc.List, state, facts, recv)
		body := r.analyzeTimerBlock(&ast.BlockStmt{List: cc.Body}, caseExprs.end, facts, recv)
		combined = mergeTimerFacts(combined, caseExprs, body)
		exits |= body.end | body.breaks
	}
	if !hasDefault {
		exits |= state
	}
	combined.end = exits
	combined.breaks = 0
	return combined
}

func (r *runner) analyzeTimerSelect(stmt *ast.SelectStmt, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	var combined timerSummary
	var exits timerMask
	for _, clause := range stmt.Body.List {
		cc, ok := clause.(*ast.CommClause)
		if !ok {
			continue
		}
		branch := timerSummary{end: input}
		if cc.Comm != nil {
			branch = r.analyzeTimerStmt(cc.Comm, input, facts, recv)
		}
		body := r.analyzeTimerBlock(&ast.BlockStmt{List: cc.Body}, branch.end, facts, recv)
		combined = mergeTimerFacts(combined, branch, body)
		exits |= body.end | body.breaks
	}
	combined.end = exits
	combined.breaks = 0
	return combined
}

func (r *runner) analyzeTimerExprs(exprs []ast.Expr, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	result := timerSummary{end: input}
	for _, expr := range exprs {
		result = sequenceTimerSummaries(result, r.analyzeTimerExpr(expr, result.end, facts, recv))
	}
	return result
}

func (r *runner) analyzeTimerExpr(expr ast.Expr, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	if expr == nil {
		return timerSummary{end: input}
	}
	var calls []*ast.CallExpr
	ast.Inspect(expr, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			calls = append(calls, call)
		}
		return true
	})
	sort.SliceStable(calls, func(i, j int) bool { return calls[i].End() < calls[j].End() })
	result := timerSummary{end: input}
	for _, call := range calls {
		result = sequenceTimerSummaries(result, r.analyzeTimerCall(call, result.end, facts, recv))
	}
	return result
}

func workSummary(state timerMask) timerSummary {
	return timerSummary{
		end:         state,
		workRunning: state&timerRunning != 0,
		workStopped: state&timerStopped != 0,
		workUnknown: state&timerUnknown != 0,
	}
}

func (s timerSummary) hasWork() bool {
	return s.workRunning || s.workStopped || s.workUnknown
}

func sequenceTimerSummaries(prefix, next timerSummary) timerSummary {
	result := mergeTimerFacts(prefix, next)
	result.end = next.end
	return result
}

func mergeTimerFacts(items ...timerSummary) timerSummary {
	var result timerSummary
	for _, item := range items {
		result.end |= item.end
		result.breaks |= item.breaks
		result.continues |= item.continues
		result.workRunning = result.workRunning || item.workRunning
		result.workStopped = result.workStopped || item.workStopped
		result.workUnknown = result.workUnknown || item.workUnknown
		result.stopCalls = append(result.stopCalls, item.stopCalls...)
	}
	return result
}

func callExpression(stmt ast.Stmt) *ast.CallExpr {
	switch value := stmt.(type) {
	case *ast.ExprStmt:
		call, _ := unparen(value.X).(*ast.CallExpr)
		return call
	case *ast.AssignStmt:
		if len(value.Rhs) == 1 {
			call, _ := unparen(value.Rhs[0]).(*ast.CallExpr)
			return call
		}
	case *ast.DeclStmt:
		decl, _ := value.Decl.(*ast.GenDecl)
		if decl != nil && len(decl.Specs) == 1 {
			spec, _ := decl.Specs[0].(*ast.ValueSpec)
			if spec != nil && len(spec.Values) == 1 {
				call, _ := unparen(spec.Values[0]).(*ast.CallExpr)
				return call
			}
		}
	}
	return nil
}

func (r *runner) isWorkCall(call *ast.CallExpr) bool {
	_, method, family := r.testingMethod(call)
	if family != "" {
		switch method {
		case "Loop", "ResetTimer", "StartTimer", "StopTimer", "ReportAllocs", "SetBytes", "SetParallelism", "ReportMetric", "Run", "RunParallel", "Cleanup", "Helper", "Name", "TempDir":
			return false
		}
	}
	return true
}

func statementHasWork(stmt ast.Stmt) bool {
	switch stmt.(type) {
	case *ast.AssignStmt, *ast.IncDecStmt, *ast.SendStmt, *ast.GoStmt:
		return true
	}
	return false
}
