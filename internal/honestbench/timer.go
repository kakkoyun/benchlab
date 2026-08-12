package honestbench

import (
	"go/ast"
	"go/token"
	"go/types"
)

type timerMask uint8

const (
	timerRunning timerMask = 1 << iota
	timerStopped
	timerUnknown
)

type timerSummary struct {
	end                  timerMask
	workRunning          bool
	workStopped          bool
	workUnknown          bool
	stopCalls            []*ast.CallExpr
	unknownHelperEffects bool
}

func (r *runner) checkTimerLoop(loop *loopInfo, facts *scopeFacts) {
	if facts.kind == parallelScope {
		return
	}
	summary := r.analyzeTimerBlock(loop.body, timerRunning, facts, loop.recv)
	if len(summary.stopCalls) > 0 && summary.end&timerStopped != 0 && !summary.unknownHelperEffects {
		call := summary.stopCalls[0]
		r.report(call.Pos(), call.End(), "stoptimer-without-starttimer", "a reachable iteration path stops timing and cannot resume before later measured work")
	}
	if summary.workStopped && !summary.workRunning && !summary.workUnknown && !summary.unknownHelperEffects {
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
		summary := r.analyzeTimerStmt(stmt, state, facts, recv)
		state = summary.end
		result.workRunning = result.workRunning || summary.workRunning
		result.workStopped = result.workStopped || summary.workStopped
		result.workUnknown = result.workUnknown || summary.workUnknown
		result.stopCalls = append(result.stopCalls, summary.stopCalls...)
		result.unknownHelperEffects = result.unknownHelperEffects || summary.unknownHelperEffects
	}
	result.end = state
	return result
}

func (r *runner) analyzeTimerStmt(stmt ast.Stmt, input timerMask, facts *scopeFacts, recv types.Object) timerSummary {
	if call := callExpression(stmt); call != nil {
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
			if fn == nil || r.funcDecls[fn] == nil {
				return timerSummary{end: timerUnknown, workUnknown: true, unknownHelperEffects: true}
			}
		}
		if r.isWorkCall(call) {
			return workSummary(input)
		}
		return timerSummary{end: input}
	}

	switch s := stmt.(type) {
	case *ast.IfStmt:
		state := input
		prefix := timerSummary{end: state}
		if s.Init != nil {
			prefix = r.analyzeTimerStmt(s.Init, state, facts, recv)
			state = prefix.end
		}
		thenSummary := r.analyzeTimerBlock(s.Body, state, facts, recv)
		elseSummary := timerSummary{end: state}
		if s.Else != nil {
			elseSummary = r.analyzeTimerStmt(s.Else, state, facts, recv)
		}
		result := mergeTimerSummaries(prefix, thenSummary, elseSummary)
		result.end = thenSummary.end | elseSummary.end
		return result
	case *ast.BlockStmt:
		return r.analyzeTimerBlock(s, input, facts, recv)
	case *ast.ForStmt:
		loopInput := input
		combined := timerSummary{end: input}
		for range 4 {
			body := r.analyzeTimerBlock(s.Body, loopInput, facts, recv)
			combined = mergeTimerSummaries(combined, body)
			next := input | body.end
			if next == loopInput {
				break
			}
			loopInput = next
		}
		combined.end = input | loopInput
		return combined
	case *ast.RangeStmt:
		loopInput := input
		combined := timerSummary{end: input}
		for range 4 {
			body := r.analyzeTimerBlock(s.Body, loopInput, facts, recv)
			combined = mergeTimerSummaries(combined, body)
			next := input | body.end
			if next == loopInput {
				break
			}
			loopInput = next
		}
		combined.end = input | loopInput
		return combined
	case *ast.SwitchStmt:
		combined := timerSummary{}
		for _, clause := range s.Body.List {
			cc, ok := clause.(*ast.CaseClause)
			if !ok {
				continue
			}
			body := &ast.BlockStmt{List: cc.Body}
			combined = mergeTimerSummaries(combined, r.analyzeTimerBlock(body, input, facts, recv))
		}
		combined.end |= input
		return combined
	case *ast.DeferStmt:
		return timerSummary{end: input}
	case *ast.ReturnStmt, *ast.BranchStmt, *ast.EmptyStmt:
		return timerSummary{end: input}
	}

	if statementHasWork(stmt) {
		return workSummary(input)
	}
	return timerSummary{end: input}
}

func workSummary(state timerMask) timerSummary {
	return timerSummary{
		end:         state,
		workRunning: state&timerRunning != 0,
		workStopped: state&timerStopped != 0,
		workUnknown: state&timerUnknown != 0,
	}
}

func mergeTimerSummaries(items ...timerSummary) timerSummary {
	var result timerSummary
	for _, item := range items {
		result.end |= item.end
		result.workRunning = result.workRunning || item.workRunning
		result.workStopped = result.workStopped || item.workStopped
		result.workUnknown = result.workUnknown || item.workUnknown
		result.stopCalls = append(result.stopCalls, item.stopCalls...)
		result.unknownHelperEffects = result.unknownHelperEffects || item.unknownHelperEffects
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
