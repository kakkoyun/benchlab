package honestbench

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

func (r *runner) checkLegacyLoopAdvisories(loop *loopInfo, facts *scopeFacts) {
	if loop.kind != bNLoop {
		return
	}
	r.checkTimedSetupCleanup(loop, facts)
	r.checkDiscardedResults(loop, facts)
	r.checkMissingSink(loop)
}

func (r *runner) checkTimedSetupCleanup(loop *loopInfo, facts *scopeFacts) {
	if loop.parent == nil {
		return
	}
	resetBefore := false
	for i := 0; i < loop.index; i++ {
		stmt := loop.parent.List[i]
		if call := callExpression(stmt); call != nil {
			recv, method, family := r.testingMethod(call)
			if family == "B" && facts.activeReceiver(recv, loop.recv) && method == "ResetTimer" {
				resetBefore = true
			}
		}
	}
	if !resetBefore {
		for i := 0; i < loop.index; i++ {
			stmt := loop.parent.List[i]
			if isNontrivialStatement(stmt) {
				r.report(stmt.Pos(), stmt.End(), "timed-setup", "nontrivial setup before a legacy b.N loop is included in the benchmark timing")
				break
			}
		}
	}

	stopAfter := false
	for i := loop.index + 1; i < len(loop.parent.List); i++ {
		stmt := loop.parent.List[i]
		if call := callExpression(stmt); call != nil {
			recv, method, family := r.testingMethod(call)
			if family == "B" && facts.activeReceiver(recv, loop.recv) && method == "StopTimer" {
				stopAfter = true
				break
			}
		}
	}
	if !stopAfter {
		for i := loop.index + 1; i < len(loop.parent.List); i++ {
			stmt := loop.parent.List[i]
			if isNontrivialCleanup(stmt) {
				r.report(stmt.Pos(), stmt.End(), "timed-cleanup", "nontrivial cleanup after a legacy b.N loop remains in the measured interval")
				break
			}
		}
	}
	for _, stmt := range loop.parent.List[:loop.index] {
		if deferStmt, ok := stmt.(*ast.DeferStmt); ok {
			r.report(deferStmt.Pos(), deferStmt.End(), "timed-cleanup", "deferred cleanup remains in the measured interval of this legacy b.N benchmark")
			break
		}
	}
}

func isNontrivialStatement(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.EmptyStmt:
		return false
	case *ast.ExprStmt:
		_, ok := s.X.(*ast.BasicLit)
		return !ok
	}
	return true
}

func isNontrivialCleanup(stmt ast.Stmt) bool {
	if assign, ok := stmt.(*ast.AssignStmt); ok && len(assign.Lhs) == 1 {
		if id, ok := assign.Lhs[0].(*ast.Ident); ok && id.Name == "_" {
			return false
		}
	}
	return isNontrivialStatement(stmt)
}

func (r *runner) checkDiscardedResults(loop *loopInfo, facts *scopeFacts) {
	ast.Inspect(loop.body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch value := node.(type) {
		case *ast.ExprStmt:
			call, ok := unparen(value.X).(*ast.CallExpr)
			if ok && r.callReturnsValues(call) && r.isWorkCall(call) {
				r.report(value.Pos(), value.End(), "discarded-result", "result-returning call is discarded inside a legacy b.N iteration")
			}
		case *ast.AssignStmt:
			if len(value.Lhs) != len(value.Rhs) {
				return true
			}
			for i, lhs := range value.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name != "_" {
					continue
				}
				call, ok := unparen(value.Rhs[i]).(*ast.CallExpr)
				if ok && r.callReturnsValues(call) && r.isWorkCall(call) {
					r.report(value.Pos(), value.End(), "discarded-result", "call result is assigned to the blank identifier inside a legacy b.N iteration")
				}
			}
		}
		return true
	})
}

func (r *runner) callReturnsValues(call *ast.CallExpr) bool {
	typ := r.pass.TypesInfo.TypeOf(call)
	if typ == nil {
		return false
	}
	if tuple, ok := types.Unalias(typ).(*types.Tuple); ok {
		return tuple.Len() > 0
	}
	return true
}

func (r *runner) checkMissingSink(loop *loopInfo) {
	assigned := make(map[types.Object]token.Pos)
	ast.Inspect(loop.body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			obj := baseObject(r.pass, lhs)
			if obj != nil && obj.Parent() != r.pass.Pkg.Scope() {
				assigned[obj] = lhs.Pos()
			}
		}
		return true
	})
	if len(assigned) == 0 || loop.parent == nil {
		return
	}
	observed := make(map[types.Object]bool)
	blankOnly := make(map[types.Object]bool)
	for i := loop.index + 1; i < len(loop.parent.List); i++ {
		stmt := loop.parent.List[i]
		if assign, ok := stmt.(*ast.AssignStmt); ok && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
			lhs, lhsOK := assign.Lhs[0].(*ast.Ident)
			rhs, rhsOK := assign.Rhs[0].(*ast.Ident)
			if lhsOK && lhs.Name == "_" && rhsOK {
				if obj := r.pass.TypesInfo.Uses[rhs]; assigned[obj] != token.NoPos {
					blankOnly[obj] = true
					continue
				}
			}
		}
		ast.Inspect(stmt, func(node ast.Node) bool {
			id, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			obj := r.pass.TypesInfo.Uses[id]
			if _, tracked := assigned[obj]; tracked {
				observed[obj] = true
			}
			return true
		})
	}
	for obj, pos := range assigned {
		if !observed[obj] {
			message := "legacy-loop result has no observable use after the loop"
			if blankOnly[obj] {
				message = "legacy-loop result is used only by a blank assignment after the loop"
			}
			r.report(pos, token.NoPos, "missing-sink", message)
		}
	}
}

func (r *runner) checkPackageWrites(loop *loopInfo) {
	ast.Inspect(loop.body, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			obj := baseObject(r.pass, lhs)
			if obj != nil && obj.Parent() == r.pass.Pkg.Scope() {
				r.report(lhs.Pos(), lhs.End(), "package-write-in-loop", "measured iteration writes a package variable, adding global-write cost and possibly changing escape behavior")
			}
		}
		return true
	})
}

func (r *runner) checkReusedMutatedInput(loop *loopInfo) {
	if loop.parent == nil {
		return
	}
	preLoop := make(map[types.Object]bool)
	for i := 0; i < loop.index; i++ {
		ast.Inspect(loop.parent.List[i], func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.AssignStmt:
				for _, lhs := range value.Lhs {
					if obj := baseObject(r.pass, lhs); obj != nil {
						preLoop[obj] = true
					}
				}
			case *ast.ValueSpec:
				for _, name := range value.Names {
					if obj := r.pass.TypesInfo.Defs[name]; obj != nil {
						preLoop[obj] = true
					}
				}
			}
			return true
		})
	}
	if len(preLoop) == 0 {
		return
	}
	ast.Inspect(loop.body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !r.isKnownInPlaceMutator(call) || len(call.Args) == 0 {
			return true
		}
		obj := baseObject(r.pass, call.Args[0])
		if preLoop[obj] && !r.objectReassignedBefore(loop.body, call.Pos(), obj) {
			r.report(call.Pos(), call.End(), "reused-mutated-input", "in-place sort or reverse operation reuses input created before the measured loop")
		}
		return true
	})
}

func (r *runner) isKnownInPlaceMutator(call *ast.CallExpr) bool {
	fn := r.calledFunction(call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() {
	case "sort":
		switch fn.Name() {
		case "Ints", "Float64s", "Strings", "Slice", "SliceStable", "Sort", "Stable", "Reverse":
			return true
		}
	case "slices":
		switch fn.Name() {
		case "Sort", "SortFunc", "SortStableFunc", "Reverse":
			return true
		}
	}
	return false
}

func (r *runner) objectReassignedBefore(block *ast.BlockStmt, before token.Pos, obj types.Object) bool {
	for _, stmt := range block.List {
		if stmt.Pos() >= before {
			break
		}
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}
		for _, lhs := range assign.Lhs {
			if baseObject(r.pass, lhs) == obj {
				return true
			}
		}
	}
	return false
}

func (r *runner) checkRedundantBLoopTimer(loop *loopInfo, facts *scopeFacts) {
	if loop.parent == nil || r.isGenerated(loop.stmt.Pos()) {
		return
	}
	if loop.index > 0 {
		stmt := loop.parent.List[loop.index-1]
		if call := callExpression(stmt); call != nil {
			recv, method, family := r.testingMethod(call)
			if family == "B" && facts.activeReceiver(recv, loop.recv) && method == "ResetTimer" {
				fix := r.removeStatementFix(stmt, "remove redundant ResetTimer")
				r.report(call.Pos(), call.End(), "redundant-b-loop-timer", "standalone ResetTimer immediately before B.Loop duplicates B.Loop behavior", fix...)
			}
		}
	}
	if loop.index+1 < len(loop.parent.List) {
		stmt := loop.parent.List[loop.index+1]
		if call := callExpression(stmt); call != nil {
			recv, method, family := r.testingMethod(call)
			if family == "B" && facts.activeReceiver(recv, loop.recv) && method == "StopTimer" {
				fix := r.removeStatementFix(stmt, "remove redundant StopTimer")
				r.report(call.Pos(), call.End(), "redundant-b-loop-timer", "standalone StopTimer immediately after B.Loop duplicates B.Loop behavior", fix...)
			}
		}
	}
}

func (r *runner) removeStatementFix(stmt ast.Stmt, message string) []analysis.SuggestedFix {
	if r.hasCommentBetween(stmt.Pos(), stmt.End()) {
		return nil
	}
	return []analysis.SuggestedFix{{Message: message, TextEdits: []analysis.TextEdit{{Pos: stmt.Pos(), End: stmt.End()}}}}
}
