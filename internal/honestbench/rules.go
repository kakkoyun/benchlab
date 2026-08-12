package honestbench

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/analysis"
)

func (r *runner) checkBenchmarkRules(facts *scopeFacts) {
	loops := sortedLoops(facts.loops)
	var bLoops, canonicalBLoops, bNLoops []*loopInfo
	for _, loop := range loops {
		switch loop.kind {
		case bLoop:
			bLoops = append(bLoops, loop)
			if loop.canonical {
				canonicalBLoops = append(canonicalBLoops, loop)
			} else {
				fixes := r.normalizeBLoopFix(loop)
				r.report(loop.stmt.Pos(), loop.stmt.End(), "noncanonical-b-loop", "testing.B.Loop must be the sole condition of a for loop", fixes...)
			}
		case bNLoop:
			bNLoops = append(bNLoops, loop)
			if loop.wrongCount {
				r.report(loop.stmt.Pos(), loop.stmt.End(), "wrong-b-n-count", "this loop is provably not executed exactly b.N times")
			} else if !loop.canonical {
				r.report(loop.stmt.Pos(), loop.stmt.End(), "noncanonical-b-n-loop", "this b.N loop is unusual and its exact iteration count cannot be proven")
			}
		}
	}

	if len(bLoops) == 0 && len(bNLoops) == 0 && len(facts.runCalls) == 0 && len(facts.runParallelCalls) == 0 && !facts.unknownBDelegation {
		r.report(facts.pos, token.NoPos, "missing-loop", "benchmark scope has no B.Loop, valid b.N loop, RunParallel, subbenchmark, or receiver delegation")
	}
	if len(canonicalBLoops) > 1 {
		for _, loop := range canonicalBLoops[1:] {
			r.report(loop.stmt.Pos(), loop.stmt.End(), "multiple-b-loop", "only the first testing.B.Loop in a benchmark scope can perform a measurement")
		}
	}
	if len(bLoops) > 0 && len(bNLoops) > 0 {
		pos, end := bNLoops[0].stmt.Pos(), bNLoops[0].stmt.End()
		if bLoops[0].stmt.Pos() > pos {
			pos, end = bLoops[0].stmt.Pos(), bLoops[0].stmt.End()
		}
		r.report(pos, end, "mixed-loop", "benchmark scope mixes testing.B.Loop with b.N iteration")
	}
	r.checkBNReadsWithBLoop(facts, bLoops)

	for _, loop := range append(append([]*loopInfo(nil), bLoops...), bNLoops...) {
		r.checkTimerLoop(loop, facts)
		r.checkLoopCalls(loop, facts)
	}
	for _, call := range facts.outerControlCalls {
		r.report(call.Pos(), call.End(), "outer-b-in-subbenchmark", "subbenchmark callback uses the parent *testing.B instead of its callback receiver")
	}
	for _, region := range facts.regions {
		r.checkSetParallelismOrder(region.block, region.activeB, false)
	}

	if !r.advisory {
		return
	}
	for _, loop := range bNLoops {
		if loop.canonical && !loop.wrongCount {
			fixes := r.suggestBLoopFix(loop)
			r.report(loop.stmt.Pos(), loop.stmt.End(), "suggest-bloop", "canonical b.N loop can use the preferred testing.B.Loop form", fixes...)
		}
		r.checkLegacyLoopAdvisories(loop, facts)
	}
	if len(facts.setParallelismCalls) > 0 && len(facts.runParallelCalls) == 0 {
		for _, call := range facts.setParallelismCalls {
			r.report(call.Pos(), call.End(), "setparallelism-without-runparallel", "SetParallelism has no matching RunParallel in this benchmark scope")
		}
	}
	for _, loop := range canonicalBLoops {
		r.checkRedundantBLoopTimer(loop, facts)
	}
}

func (r *runner) checkParallelRules(facts *scopeFacts) {
	loops := sortedLoops(facts.loops)
	var nextLoops []*loopInfo
	for _, loop := range loops {
		switch loop.kind {
		case pbNextLoop:
			nextLoops = append(nextLoops, loop)
		case bLoop, bNLoop:
			r.report(loop.stmt.Pos(), loop.stmt.End(), "runparallel-wrong-loop", "RunParallel callback must iterate with pb.Next, not testing.B.Loop or b.N")
		}
	}
	if len(nextLoops) == 0 && !facts.unknownPBDelegation {
		r.report(facts.pos, token.NoPos, "runparallel-missing-next", "RunParallel callback neither iterates with pb.Next nor delegates its *testing.PB")
	}
	if len(nextLoops) > 1 {
		for _, loop := range nextLoops[1:] {
			r.report(loop.stmt.Pos(), loop.stmt.End(), "runparallel-wrong-loop", "RunParallel callback contains more than one pb.Next loop")
		}
	}
	for _, call := range facts.parallelTimerCalls {
		r.report(call.Pos(), call.End(), "runparallel-timer", "timer methods have global effect and must not be called from a RunParallel callback")
	}
	for _, call := range facts.parallelRunCalls {
		r.report(call.Pos(), call.End(), "runparallel-subbenchmark", "RunParallel callback must not start a subbenchmark")
	}
}

func (r *runner) checkBNReadsWithBLoop(facts *scopeFacts, bLoops []*loopInfo) {
	if len(bLoops) == 0 {
		return
	}
	for _, ref := range facts.bNRefs {
		for _, loop := range bLoops {
			if r.filename(ref.Pos()) != r.filename(loop.stmt.Pos()) {
				continue
			}
			if ref.Pos() < loop.stmt.Pos() || (ref.Pos() >= loop.stmt.Pos() && ref.Pos() <= loop.stmt.End()) {
				r.report(ref.Pos(), ref.End(), "b-n-with-b-loop", "b.N is only guaranteed to hold the final iteration count after B.Loop returns false")
				break
			}
		}
	}
}

func (r *runner) checkLoopCalls(loop *loopInfo, facts *scopeFacts) {
	ast.Inspect(loop.body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		recv, method, family := r.testingMethod(call)
		if family != "B" || !facts.activeReceiver(recv, loop.recv) {
			return true
		}
		switch method {
		case "ResetTimer":
			r.report(call.Pos(), call.End(), "reset-timer-in-loop", "ResetTimer inside a measured iteration repeatedly clears elapsed time, allocation counters, and custom metrics")
		case "ReportAllocs", "SetBytes", "SetParallelism", "ReportMetric":
			r.report(call.Pos(), call.End(), "benchmark-config-in-loop", "benchmark configuration should not execute inside every measured iteration")
		}
		return true
	})
	if r.advisory {
		r.checkPackageWrites(loop)
		r.checkReusedMutatedInput(loop)
	}
}

func (facts *scopeFacts) activeReceiver(recv, loopRecv types.Object) bool {
	return recv != nil && (recv == loopRecv || facts.activeB[recv])
}

func (r *runner) checkSetParallelismOrder(block *ast.BlockStmt, activeB map[types.Object]bool, seenRunParallel bool) bool {
	seen := seenRunParallel
	for _, stmt := range block.List {
		calls := r.directTestingCalls(stmt, activeB)
		for _, call := range calls {
			_, method, _ := r.testingMethod(call)
			switch method {
			case "RunParallel":
				seen = true
			case "SetParallelism":
				if seen {
					r.report(call.Pos(), call.End(), "setparallelism-order", "SetParallelism definitely executes after RunParallel and cannot affect that run")
				}
			}
		}
		switch s := stmt.(type) {
		case *ast.IfStmt:
			thenSeen := r.checkSetParallelismOrder(s.Body, activeB, seen)
			elseSeen := seen
			if s.Else != nil {
				elseSeen = r.checkSetParallelismStatement(s.Else, activeB, seen)
			}
			seen = thenSeen && elseSeen
		case *ast.BlockStmt:
			seen = r.checkSetParallelismOrder(s, activeB, seen)
		case *ast.ForStmt:
			r.checkSetParallelismOrder(s.Body, activeB, seen)
		case *ast.RangeStmt:
			r.checkSetParallelismOrder(s.Body, activeB, seen)
		}
	}
	return seen
}

func (r *runner) checkSetParallelismStatement(stmt ast.Stmt, activeB map[types.Object]bool, seen bool) bool {
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		return r.checkSetParallelismOrder(s, activeB, seen)
	case *ast.IfStmt:
		thenSeen := r.checkSetParallelismOrder(s.Body, activeB, seen)
		elseSeen := seen
		if s.Else != nil {
			elseSeen = r.checkSetParallelismStatement(s.Else, activeB, seen)
		}
		return thenSeen && elseSeen
	default:
		block := &ast.BlockStmt{List: []ast.Stmt{stmt}}
		return r.checkSetParallelismOrder(block, activeB, seen)
	}
}

func (r *runner) directTestingCalls(stmt ast.Stmt, activeB map[types.Object]bool) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(stmt, func(node ast.Node) bool {
		if node != stmt {
			if _, ok := node.(ast.Stmt); ok {
				return false
			}
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		recv, _, family := r.testingMethod(call)
		if family == "B" && activeB[recv] {
			calls = append(calls, call)
		}
		return true
	})
	sort.Slice(calls, func(i, j int) bool { return calls[i].Pos() < calls[j].Pos() })
	return calls
}

func (r *runner) normalizeBLoopFix(loop *loopInfo) []analysis.SuggestedFix {
	forStmt, ok := loop.stmt.(*ast.ForStmt)
	if !ok || forStmt.Init != nil || forStmt.Post != nil || r.isGenerated(loop.stmt.Pos()) {
		return nil
	}
	call := r.normalizableBLoopCall(forStmt.Cond, loop.recv)
	if call == nil || r.hasCommentBetween(forStmt.Cond.Pos(), forStmt.Cond.End()) {
		return nil
	}
	var replacement bytes.Buffer
	if err := format.Node(&replacement, r.pass.Fset, call); err != nil {
		return nil
	}
	return []analysis.SuggestedFix{{
		Message: "make B.Loop the sole condition",
		TextEdits: []analysis.TextEdit{{
			Pos:     forStmt.Cond.Pos(),
			End:     forStmt.Cond.End(),
			NewText: replacement.Bytes(),
		}},
	}}
}

func (r *runner) normalizableBLoopCall(expr ast.Expr, recv types.Object) *ast.CallExpr {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return r.normalizableBLoopCall(value.X, recv)
	case *ast.CallExpr:
		actual, method, family := r.testingMethod(value)
		if actual == recv && family == "B" && method == "Loop" && len(value.Args) == 0 {
			return value
		}
	case *ast.BinaryExpr:
		if value.Op != token.EQL {
			return nil
		}
		if isTrueConstant(r.pass, value.X) {
			return r.normalizableBLoopCall(value.Y, recv)
		}
		if isTrueConstant(r.pass, value.Y) {
			return r.normalizableBLoopCall(value.X, recv)
		}
	}
	return nil
}

func isTrueConstant(pass *analysis.Pass, expr ast.Expr) bool {
	value := pass.TypesInfo.Types[expr].Value
	return value != nil && value.Kind() == 1 && value.ExactString() == "true"
}

func (r *runner) suggestBLoopFix(loop *loopInfo) []analysis.SuggestedFix {
	if r.isGenerated(loop.stmt.Pos()) || r.hasCommentBetween(loop.stmt.Pos(), loop.body.Lbrace) || !r.loopIndexUnused(loop) {
		return nil
	}
	name := loop.recv.Name()
	if name == "" {
		return nil
	}
	return []analysis.SuggestedFix{{
		Message: "replace the legacy loop with B.Loop",
		TextEdits: []analysis.TextEdit{{
			Pos:     loop.stmt.Pos(),
			End:     loop.body.Lbrace,
			NewText: []byte("for " + name + ".Loop() "),
		}},
	}}
}

func (r *runner) loopIndexUnused(loop *loopInfo) bool {
	if loop.indexObj == nil || loop.indexObj.Name() == "_" {
		return true
	}
	used := false
	ast.Inspect(loop.body, func(node ast.Node) bool {
		id, ok := node.(*ast.Ident)
		if ok && r.pass.TypesInfo.Uses[id] == loop.indexObj {
			used = true
			return false
		}
		return !used
	})
	return !used
}
