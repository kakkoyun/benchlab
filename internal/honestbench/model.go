package honestbench

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/analysis"
)

type scopeKind uint8

const (
	benchmarkScope scopeKind = iota
	subbenchmarkScope
	parallelScope
)

type loopKind uint8

const (
	unknownLoop loopKind = iota
	bLoop
	bNLoop
	pbNextLoop
	unusualBNLoop
)

type loopInfo struct {
	stmt       ast.Stmt
	body       *ast.BlockStmt
	parent     *ast.BlockStmt
	index      int
	kind       loopKind
	canonical  bool
	wrongCount bool
	recv       types.Object
	indexObj   types.Object
}

type scopeFacts struct {
	kind                scopeKind
	pos                 token.Pos
	name                string
	activeB             map[types.Object]bool
	activePB            map[types.Object]bool
	outerB              map[types.Object]bool
	loops               []*loopInfo
	bNRefs              []*ast.SelectorExpr
	runCalls            []*ast.CallExpr
	runParallelCalls    []*ast.CallExpr
	setParallelismCalls []*ast.CallExpr
	outerControlCalls   []*ast.CallExpr
	parallelTimerCalls  []*ast.CallExpr
	parallelRunCalls    []*ast.CallExpr
	unknownBDelegation  bool
	unknownPBDelegation bool
	visitedHelpers      map[*types.Func]bool
	regions             []scopeRegion
}

type scopeRegion struct {
	block    *ast.BlockStmt
	activeB  map[types.Object]bool
	activePB map[types.Object]bool
}

func (r *runner) analyzeBenchmarkScope(body *ast.BlockStmt, recv types.Object, outer map[types.Object]bool, pos token.Pos, name string) {
	if _, done := r.analyzedScopes[pos]; done {
		return
	}
	r.analyzedScopes[pos] = struct{}{}
	facts := &scopeFacts{
		kind:           benchmarkScope,
		pos:            pos,
		name:           name,
		activeB:        map[types.Object]bool{recv: true},
		activePB:       make(map[types.Object]bool),
		outerB:         cloneObjectSet(outer),
		visitedHelpers: make(map[*types.Func]bool),
	}
	if len(facts.outerB) > 0 {
		facts.kind = subbenchmarkScope
	}
	r.scanBlock(facts, body, facts.activeB, facts.activePB)
	r.checkBenchmarkRules(facts)
}

func (r *runner) analyzeParallelScope(body *ast.BlockStmt, pb types.Object, outerB map[types.Object]bool, pos token.Pos, name string) {
	if _, done := r.analyzedScopes[pos]; done {
		return
	}
	r.analyzedScopes[pos] = struct{}{}
	facts := &scopeFacts{
		kind:           parallelScope,
		pos:            pos,
		name:           name,
		activeB:        cloneObjectSet(outerB),
		activePB:       map[types.Object]bool{pb: true},
		outerB:         cloneObjectSet(outerB),
		visitedHelpers: make(map[*types.Func]bool),
	}
	r.scanBlock(facts, body, facts.activeB, facts.activePB)
	r.checkParallelRules(facts)
}

func (r *runner) scanBlock(facts *scopeFacts, block *ast.BlockStmt, inheritedB, inheritedPB map[types.Object]bool) {
	activeB := cloneObjectSet(inheritedB)
	activePB := cloneObjectSet(inheritedPB)
	r.discoverAliases(block, activeB, activePB)
	facts.regions = append(facts.regions, scopeRegion{block: block, activeB: activeB, activePB: activePB})

	for i, stmt := range block.List {
		r.scanStatement(facts, stmt, block, i, activeB, activePB)
	}
}

func (r *runner) scanStatement(facts *scopeFacts, stmt ast.Stmt, parent *ast.BlockStmt, index int, activeB, activePB map[types.Object]bool) {
	if loop := r.classifyLoop(stmt, parent, index, activeB, activePB); loop != nil {
		facts.loops = append(facts.loops, loop)
	}

	ast.Inspect(stmt, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		if node != stmt {
			if _, nestedStatement := node.(ast.Stmt); nestedStatement {
				return false
			}
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch n := node.(type) {
		case *ast.SelectorExpr:
			if r.isBNSelector(n, activeB) {
				facts.bNRefs = append(facts.bNRefs, n)
			}
		case *ast.CallExpr:
			r.scanCall(facts, n, activeB, activePB)
		}
		return true
	})

	switch s := stmt.(type) {
	case *ast.BlockStmt:
		r.scanBlock(facts, s, activeB, activePB)
	case *ast.IfStmt:
		if s.Init != nil {
			r.scanStatement(facts, s.Init, parent, index, activeB, activePB)
		}
		r.scanBlock(facts, s.Body, activeB, activePB)
		if s.Else != nil {
			r.scanStatement(facts, s.Else, parent, index, activeB, activePB)
		}
	case *ast.ForStmt:
		r.scanBlock(facts, s.Body, activeB, activePB)
	case *ast.RangeStmt:
		r.scanBlock(facts, s.Body, activeB, activePB)
	case *ast.SwitchStmt:
		for _, clause := range s.Body.List {
			cc, ok := clause.(*ast.CaseClause)
			if !ok {
				continue
			}
			r.scanBlock(facts, &ast.BlockStmt{Lbrace: cc.Colon, List: cc.Body, Rbrace: cc.End()}, activeB, activePB)
		}
	case *ast.TypeSwitchStmt:
		for _, clause := range s.Body.List {
			cc, ok := clause.(*ast.CaseClause)
			if !ok {
				continue
			}
			r.scanBlock(facts, &ast.BlockStmt{Lbrace: cc.Colon, List: cc.Body, Rbrace: cc.End()}, activeB, activePB)
		}
	case *ast.SelectStmt:
		for _, clause := range s.Body.List {
			cc, ok := clause.(*ast.CommClause)
			if !ok {
				continue
			}
			r.scanBlock(facts, &ast.BlockStmt{Lbrace: cc.Colon, List: cc.Body, Rbrace: cc.End()}, activeB, activePB)
		}
	case *ast.LabeledStmt:
		r.scanStatement(facts, s.Stmt, parent, index, activeB, activePB)
	}
}

func (r *runner) scanCall(facts *scopeFacts, call *ast.CallExpr, activeB, activePB map[types.Object]bool) {
	recv, method, family := r.testingMethod(call)
	if recv != nil {
		switch {
		case facts.kind == parallelScope && family == "B" && facts.outerB[recv]:
			switch method {
			case "StartTimer", "StopTimer", "ResetTimer":
				facts.parallelTimerCalls = append(facts.parallelTimerCalls, call)
			case "Run":
				facts.parallelRunCalls = append(facts.parallelRunCalls, call)
			}
		case family == "B" && activeB[recv]:
			switch method {
			case "Run":
				facts.runCalls = append(facts.runCalls, call)
				if body, callbackRecv, pos, ok := r.callback(call, 1, "B"); ok {
					outer := cloneObjectSet(facts.outerB)
					mergeObjectSet(outer, activeB)
					r.analyzeBenchmarkScope(body, callbackRecv, outer, pos, facts.name+"/Run")
				}
			case "RunParallel":
				facts.runParallelCalls = append(facts.runParallelCalls, call)
				if body, callbackRecv, pos, ok := r.callback(call, 0, "PB"); ok {
					outer := cloneObjectSet(facts.outerB)
					mergeObjectSet(outer, activeB)
					r.analyzeParallelScope(body, callbackRecv, outer, pos, facts.name+"/RunParallel")
				}
			case "SetParallelism":
				facts.setParallelismCalls = append(facts.setParallelismCalls, call)
			}
		case facts.kind == subbenchmarkScope && family == "B" && facts.outerB[recv] && isBenchmarkControlMethod(method):
			facts.outerControlCalls = append(facts.outerControlCalls, call)
		}
	}

	fn := r.calledFunction(call)
	if fn == nil {
		if callPassesActiveObject(r.pass, call, activeB) {
			facts.unknownBDelegation = true
		}
		if callPassesActiveObject(r.pass, call, activePB) {
			facts.unknownPBDelegation = true
		}
		return
	}
	fd := r.funcDecls[fn]
	if fd == nil {
		if callPassesActiveObject(r.pass, call, activeB) {
			facts.unknownBDelegation = true
		}
		if callPassesActiveObject(r.pass, call, activePB) {
			facts.unknownPBDelegation = true
		}
		return
	}
	if facts.visitedHelpers[fn] {
		return
	}

	sig, _ := fn.Type().(*types.Signature)
	if sig == nil {
		return
	}
	helperB := make(map[types.Object]bool)
	helperPB := make(map[types.Object]bool)
	for i, arg := range call.Args {
		if i >= sig.Params().Len() {
			break
		}
		argObj := baseObject(r.pass, arg)
		paramObj := parameterObject(r.pass, fd.Type, i)
		if paramObj == nil {
			continue
		}
		if activeB[argObj] && isTestingPointer(sig.Params().At(i).Type(), "B") {
			helperB[paramObj] = true
		}
		if activePB[argObj] && isTestingPointer(sig.Params().At(i).Type(), "PB") {
			helperPB[paramObj] = true
		}
	}
	if len(helperB) == 0 && len(helperPB) == 0 {
		return
	}
	facts.visitedHelpers[fn] = true
	r.scanBlock(facts, fd.Body, helperB, helperPB)
}

func (r *runner) callback(call *ast.CallExpr, argIndex int, family string) (*ast.BlockStmt, types.Object, token.Pos, bool) {
	if argIndex >= len(call.Args) {
		return nil, nil, token.NoPos, false
	}
	switch cb := unparen(call.Args[argIndex]).(type) {
	case *ast.FuncLit:
		recv := firstParamObject(r.pass, cb.Type)
		if recv == nil || !isTestingPointer(recv.Type(), family) {
			return nil, nil, token.NoPos, false
		}
		return cb.Body, recv, cb.Pos(), true
	case *ast.Ident:
		fn, _ := r.pass.TypesInfo.Uses[cb].(*types.Func)
		fd := r.funcDecls[fn]
		if fd == nil {
			return nil, nil, token.NoPos, false
		}
		recv := firstParamObject(r.pass, fd.Type)
		if recv == nil || !isTestingPointer(recv.Type(), family) {
			return nil, nil, token.NoPos, false
		}
		return fd.Body, recv, fd.Pos(), true
	}
	return nil, nil, token.NoPos, false
}

func (r *runner) discoverAliases(block *ast.BlockStmt, activeB, activePB map[types.Object]bool) {
	for changed := true; changed; {
		changed = false
		ast.Inspect(block, func(node ast.Node) bool {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
			switch n := node.(type) {
			case *ast.AssignStmt:
				for i := range n.Lhs {
					if i >= len(n.Rhs) {
						break
					}
					lhs := baseObject(r.pass, n.Lhs[i])
					rhs := baseObject(r.pass, n.Rhs[i])
					if lhs == nil || rhs == nil {
						continue
					}
					if activeB[rhs] && !activeB[lhs] {
						activeB[lhs] = true
						changed = true
					}
					if activePB[rhs] && !activePB[lhs] {
						activePB[lhs] = true
						changed = true
					}
				}
			case *ast.ValueSpec:
				for i, value := range n.Values {
					if i >= len(n.Names) {
						break
					}
					lhs := r.pass.TypesInfo.Defs[n.Names[i]]
					rhs := baseObject(r.pass, value)
					if activeB[rhs] && !activeB[lhs] {
						activeB[lhs] = true
						changed = true
					}
					if activePB[rhs] && !activePB[lhs] {
						activePB[lhs] = true
						changed = true
					}
				}
			}
			return true
		})
	}
}

func (r *runner) classifyLoop(stmt ast.Stmt, parent *ast.BlockStmt, index int, activeB, activePB map[types.Object]bool) *loopInfo {
	info := &loopInfo{stmt: stmt, parent: parent, index: index}
	switch loop := stmt.(type) {
	case *ast.ForStmt:
		info.body = loop.Body
		if recv, ok := r.exactTestingLoopCall(loop.Cond, "Loop", activeB); ok && loop.Init == nil && loop.Post == nil {
			info.kind, info.canonical, info.recv = bLoop, isExactCallExpr(loop.Cond), recv
			return info
		}
		if recv, ok := r.containsTestingLoopCall(loop.Cond, "Loop", activeB); ok {
			info.kind, info.recv = bLoop, recv
			return info
		}
		if recv, indexObj, classification := r.classifyBNFor(loop, activeB); classification != unknownLoop {
			info.kind, info.recv, info.indexObj = bNLoop, recv, indexObj
			info.canonical = classification == bNLoop
			info.wrongCount = classification == unusualBNLoop
			return info
		}
		if recv, ok := r.exactTestingLoopCall(loop.Cond, "Next", activePB); ok && loop.Init == nil && loop.Post == nil {
			info.kind, info.canonical, info.recv = pbNextLoop, isExactCallExpr(loop.Cond), recv
			return info
		}
		if recv, ok := r.containsTestingLoopCall(loop.Cond, "Next", activePB); ok {
			info.kind, info.recv = pbNextLoop, recv
			return info
		}
	case *ast.RangeStmt:
		info.body = loop.Body
		if recv, ok := r.activeBNSelector(loop.X, activeB); ok {
			info.kind, info.canonical, info.recv = bNLoop, true, recv
			if id, ok := loop.Key.(*ast.Ident); ok {
				if loop.Tok == token.DEFINE {
					info.indexObj = r.pass.TypesInfo.Defs[id]
				} else {
					info.indexObj = r.pass.TypesInfo.Uses[id]
				}
			}
			return info
		}
	}
	return nil
}

// classifyBNFor returns bNLoop for a proven exact loop, unusualBNLoop for a
// proven wrong count, pbNextLoop for an unproven unusual form, and unknownLoop
// when the loop does not use b.N.
func (r *runner) classifyBNFor(loop *ast.ForStmt, activeB map[types.Object]bool) (types.Object, types.Object, loopKind) {
	recv, ok := r.containsActiveBN(loop.Cond, activeB)
	if !ok {
		return nil, nil, unknownLoop
	}
	indexObj, start, startOK := r.forInit(loop.Init)
	operator, boundIndex, condOK := r.forCondition(loop.Cond, activeB)
	step, stepOK := r.forStep(loop.Post, indexObj)
	if !startOK || !condOK || !stepOK || indexObj == nil || boundIndex != indexObj {
		return recv, indexObj, pbNextLoop
	}
	if operator == token.LSS && start == 0 && step == 1 {
		return recv, indexObj, bNLoop
	}
	if operator == token.NEQ && start == 0 && step == 1 {
		return recv, indexObj, bNLoop
	}
	if operator == token.LEQ || start != 0 || step != 1 {
		return recv, indexObj, unusualBNLoop
	}
	return recv, indexObj, pbNextLoop
}

func (r *runner) forInit(stmt ast.Stmt) (types.Object, int64, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, 0, false
	}
	id, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, 0, false
	}
	value := r.pass.TypesInfo.Types[assign.Rhs[0]].Value
	if value == nil || value.Kind() != constant.Int {
		return nil, 0, false
	}
	start, ok := constant.Int64Val(value)
	if !ok {
		return nil, 0, false
	}
	obj := r.pass.TypesInfo.Defs[id]
	if obj == nil {
		obj = r.pass.TypesInfo.Uses[id]
	}
	return obj, start, true
}

func (r *runner) forCondition(expr ast.Expr, activeB map[types.Object]bool) (token.Token, types.Object, bool) {
	binary, ok := unparen(expr).(*ast.BinaryExpr)
	if !ok {
		return token.ILLEGAL, nil, false
	}
	left := baseObject(r.pass, binary.X)
	if _, ok := r.activeBNSelector(binary.Y, activeB); ok {
		return binary.Op, left, true
	}
	right := baseObject(r.pass, binary.Y)
	if _, ok := r.activeBNSelector(binary.X, activeB); ok {
		switch binary.Op {
		case token.GTR:
			return token.LSS, right, true
		case token.GEQ:
			return token.LEQ, right, true
		}
	}
	return token.ILLEGAL, nil, false
}

func (r *runner) forStep(stmt ast.Stmt, indexObj types.Object) (int64, bool) {
	switch post := stmt.(type) {
	case *ast.IncDecStmt:
		if baseObject(r.pass, post.X) != indexObj {
			return 0, false
		}
		if post.Tok == token.INC {
			return 1, true
		}
		return -1, true
	case *ast.AssignStmt:
		if len(post.Lhs) != 1 || len(post.Rhs) != 1 || baseObject(r.pass, post.Lhs[0]) != indexObj {
			return 0, false
		}
		if post.Tok != token.ADD_ASSIGN {
			return 0, false
		}
		value := r.pass.TypesInfo.Types[post.Rhs[0]].Value
		if value == nil || value.Kind() != constant.Int {
			return 0, false
		}
		step, ok := constant.Int64Val(value)
		return step, ok
	}
	return 0, false
}

func (r *runner) testingMethod(call *ast.CallExpr) (types.Object, string, string) {
	sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, "", ""
	}
	selection := r.pass.TypesInfo.Selections[sel]
	if selection == nil {
		return nil, "", ""
	}
	method, ok := selection.Obj().(*types.Func)
	if !ok || method.Pkg() == nil || method.Pkg().Path() != "testing" {
		return nil, "", ""
	}
	family := ""
	if isTestingPointer(selection.Recv(), "B") {
		family = "B"
	} else if isTestingPointer(selection.Recv(), "PB") {
		family = "PB"
	}
	if family == "" {
		return nil, "", ""
	}
	return baseObject(r.pass, sel.X), method.Name(), family
}

func (r *runner) exactTestingLoopCall(expr ast.Expr, method string, active map[types.Object]bool) (types.Object, bool) {
	call, ok := unparen(expr).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	recv, name, _ := r.testingMethod(call)
	return recv, name == method && active[recv] && len(call.Args) == 0
}

func (r *runner) containsTestingLoopCall(expr ast.Expr, method string, active map[types.Object]bool) (types.Object, bool) {
	var found types.Object
	ast.Inspect(expr, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || found != nil {
			return found == nil
		}
		recv, name, _ := r.testingMethod(call)
		if name == method && active[recv] && len(call.Args) == 0 {
			found = recv
			return false
		}
		return true
	})
	return found, found != nil
}

func (r *runner) containsActiveBN(expr ast.Expr, active map[types.Object]bool) (types.Object, bool) {
	var found types.Object
	ast.Inspect(expr, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok || found != nil {
			return found == nil
		}
		if recv, ok := r.activeBNSelector(sel, active); ok {
			found = recv
			return false
		}
		return true
	})
	return found, found != nil
}

func (r *runner) activeBNSelector(sel ast.Expr, active map[types.Object]bool) (types.Object, bool) {
	selector, ok := unparen(sel).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "N" {
		return nil, false
	}
	selection := r.pass.TypesInfo.Selections[selector]
	if selection == nil || !isTestingPointer(selection.Recv(), "B") {
		return nil, false
	}
	recv := baseObject(r.pass, selector.X)
	return recv, active[recv]
}

func (r *runner) isBNSelector(sel *ast.SelectorExpr, active map[types.Object]bool) bool {
	_, ok := r.activeBNSelector(sel, active)
	return ok
}

func (r *runner) calledFunction(call *ast.CallExpr) *types.Func {
	switch fn := unparen(call.Fun).(type) {
	case *ast.Ident:
		result, _ := r.pass.TypesInfo.Uses[fn].(*types.Func)
		return result
	case *ast.SelectorExpr:
		if selection := r.pass.TypesInfo.Selections[fn]; selection != nil {
			result, _ := selection.Obj().(*types.Func)
			return result
		}
		result, _ := r.pass.TypesInfo.Uses[fn.Sel].(*types.Func)
		return result
	}
	return nil
}

func parameterObject(pass *analysis.Pass, ft *ast.FuncType, index int) types.Object {
	if ft.Params == nil {
		return nil
	}
	flat := 0
	for _, field := range ft.Params.List {
		for _, name := range field.Names {
			if flat == index {
				return pass.TypesInfo.Defs[name]
			}
			flat++
		}
	}
	return nil
}

func baseObject(pass *analysis.Pass, expr ast.Expr) types.Object {
	switch value := unparen(expr).(type) {
	case *ast.Ident:
		if obj := pass.TypesInfo.Uses[value]; obj != nil {
			return obj
		}
		return pass.TypesInfo.Defs[value]
	}
	return nil
}

func callPassesActiveObject(pass *analysis.Pass, call *ast.CallExpr, active map[types.Object]bool) bool {
	for _, arg := range call.Args {
		if active[baseObject(pass, arg)] {
			return true
		}
	}
	return false
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func isExactCallExpr(expr ast.Expr) bool {
	_, ok := expr.(*ast.CallExpr)
	return ok
}

func isBenchmarkControlMethod(method string) bool {
	switch method {
	case "Loop", "ResetTimer", "StartTimer", "StopTimer", "Run", "RunParallel", "SetParallelism", "ReportAllocs", "SetBytes", "ReportMetric":
		return true
	}
	return false
}

func cloneObjectSet(source map[types.Object]bool) map[types.Object]bool {
	result := make(map[types.Object]bool, len(source))
	for obj := range source {
		result[obj] = true
	}
	return result
}

func mergeObjectSet(dst, source map[types.Object]bool) {
	for obj := range source {
		dst[obj] = true
	}
}

func sortedLoops(loops []*loopInfo) []*loopInfo {
	result := append([]*loopInfo(nil), loops...)
	sort.Slice(result, func(i, j int) bool { return result[i].stmt.Pos() < result[j].stmt.Pos() })
	return result
}
