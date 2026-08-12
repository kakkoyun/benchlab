// Package honestbench defines the analyzer used by the honestbench command.
package honestbench

import (
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
)

// Rule describes one stable honestbench diagnostic category.
type Rule struct {
	ID       string
	Advisory bool
	Summary  string
}

var ruleRegistry = map[string]Rule{
	"missing-loop":                       {ID: "missing-loop", Summary: "benchmark work has no iteration driver"},
	"noncanonical-b-loop":                {ID: "noncanonical-b-loop", Summary: "B.Loop is not the sole for-loop condition"},
	"multiple-b-loop":                    {ID: "multiple-b-loop", Summary: "benchmark scope contains multiple B.Loop loops"},
	"mixed-loop":                         {ID: "mixed-loop", Summary: "benchmark scope mixes B.Loop and b.N iteration"},
	"b-n-with-b-loop":                    {ID: "b-n-with-b-loop", Summary: "b.N is read before B.Loop has finished"},
	"wrong-b-n-count":                    {ID: "wrong-b-n-count", Summary: "legacy loop provably does not run b.N times"},
	"reset-timer-in-loop":                {ID: "reset-timer-in-loop", Summary: "ResetTimer executes inside a measured iteration"},
	"stoptimer-without-starttimer":       {ID: "stoptimer-without-starttimer", Summary: "timer can remain stopped before measured work"},
	"work-while-timer-stopped":           {ID: "work-while-timer-stopped", Summary: "all reachable iteration work runs while timing is stopped"},
	"outer-b-in-subbenchmark":            {ID: "outer-b-in-subbenchmark", Summary: "subbenchmark uses its parent benchmark receiver"},
	"runparallel-missing-next":           {ID: "runparallel-missing-next", Summary: "RunParallel callback does not iterate with PB.Next"},
	"runparallel-wrong-loop":             {ID: "runparallel-wrong-loop", Summary: "RunParallel callback uses the wrong or repeated loop"},
	"runparallel-timer":                  {ID: "runparallel-timer", Summary: "RunParallel callback changes the global benchmark timer"},
	"runparallel-subbenchmark":           {ID: "runparallel-subbenchmark", Summary: "RunParallel callback starts a subbenchmark"},
	"setparallelism-order":               {ID: "setparallelism-order", Summary: "SetParallelism definitely executes after RunParallel"},
	"suggest-bloop":                      {ID: "suggest-bloop", Advisory: true, Summary: "legacy b.N loop can use B.Loop"},
	"timed-setup":                        {ID: "timed-setup", Advisory: true, Summary: "legacy-loop setup is included in timing"},
	"timed-cleanup":                      {ID: "timed-cleanup", Advisory: true, Summary: "legacy-loop cleanup is included in timing"},
	"discarded-result":                   {ID: "discarded-result", Advisory: true, Summary: "legacy-loop call result is discarded"},
	"missing-sink":                       {ID: "missing-sink", Advisory: true, Summary: "legacy-loop result has no observable use"},
	"package-write-in-loop":              {ID: "package-write-in-loop", Advisory: true, Summary: "measured loop writes a package variable"},
	"benchmark-config-in-loop":           {ID: "benchmark-config-in-loop", Advisory: true, Summary: "benchmark configuration executes inside an iteration"},
	"noncanonical-b-n-loop":              {ID: "noncanonical-b-n-loop", Advisory: true, Summary: "b.N loop has an unusual iteration form"},
	"setparallelism-without-runparallel": {ID: "setparallelism-without-runparallel", Advisory: true, Summary: "SetParallelism has no RunParallel"},
	"reused-mutated-input":               {ID: "reused-mutated-input", Advisory: true, Summary: "in-place mutation reuses pre-loop input"},
	"redundant-b-loop-timer":             {ID: "redundant-b-loop-timer", Advisory: true, Summary: "timer call duplicates B.Loop timer behavior"},
}

// Rules returns the analyzer's stable diagnostic categories in ID order.
func Rules() []Rule {
	rules := make([]Rule, 0, len(ruleRegistry))
	for _, rule := range ruleRegistry {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return rules
}

// Analyzer is the type-aware analyzer run by cmd/honestbench.
var Analyzer = newAnalyzer()

func newAnalyzer() *analysis.Analyzer {
	var advisory bool
	a := &analysis.Analyzer{
		Name: "honestbench",
		Doc:  "checks Go benchmarks for structural correctness and documented testing API misuse",
	}
	a.Flags = *flag.NewFlagSet(a.Name, flag.ExitOnError)
	a.Flags.BoolVar(&advisory, "advisory", false, "enable heuristic benchmark-design diagnostics")
	a.Run = func(pass *analysis.Pass) (any, error) {
		r := newRunner(pass, advisory)
		return nil, r.run()
	}
	return a
}

type runner struct {
	pass           *analysis.Pass
	advisory       bool
	funcDecls      map[*types.Func]*ast.FuncDecl
	files          map[*token.File]*ast.File
	generated      map[*ast.File]bool
	diagnostics    map[string]struct{}
	analyzedScopes map[token.Pos]struct{}
}

func newRunner(pass *analysis.Pass, advisory bool) *runner {
	r := &runner{
		pass:           pass,
		advisory:       advisory,
		funcDecls:      make(map[*types.Func]*ast.FuncDecl),
		files:          make(map[*token.File]*ast.File),
		generated:      make(map[*ast.File]bool),
		diagnostics:    make(map[string]struct{}),
		analyzedScopes: make(map[token.Pos]struct{}),
	}
	for _, file := range pass.Files {
		tf := pass.Fset.File(file.Pos())
		if tf != nil {
			r.files[tf] = file
		}
		r.generated[file] = ast.IsGenerated(file)
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
				r.funcDecls[fn] = fd
			}
		}
	}
	return r
}

func (r *runner) run() error {
	for _, file := range r.pass.Files {
		filename := r.pass.Fset.PositionFor(file.Pos(), false).Filename
		if !strings.HasSuffix(filename, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil || !isBenchmarkName(fd.Name.Name) {
				continue
			}
			fn, _ := r.pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if fn == nil || !validBenchmarkSignature(fn.Type()) {
				continue
			}
			recv := firstParamObject(r.pass, fd.Type)
			if recv == nil {
				continue
			}
			r.analyzeBenchmarkScope(fd.Body, recv, nil, fd.Name.Pos(), fd.Name.Name)
		}
	}
	return nil
}

func isBenchmarkName(name string) bool {
	const prefix = "Benchmark"
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(r)
}

func validBenchmarkSignature(typ types.Type) bool {
	sig, ok := typ.(*types.Signature)
	return ok && sig.Params().Len() == 1 && sig.Results().Len() == 0 && isTestingPointer(sig.Params().At(0).Type(), "B")
}

func isTestingPointer(typ types.Type, name string) bool {
	ptr, ok := types.Unalias(typ).(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "testing" && named.Obj().Name() == name
}

func firstParamObject(pass *analysis.Pass, ft *ast.FuncType) types.Object {
	if ft.Params == nil || len(ft.Params.List) == 0 || len(ft.Params.List[0].Names) == 0 {
		return nil
	}
	return pass.TypesInfo.Defs[ft.Params.List[0].Names[0]]
}

func (r *runner) report(pos, end token.Pos, ruleID, message string, fixes ...analysis.SuggestedFix) {
	rule, ok := ruleRegistry[ruleID]
	if !ok {
		panic(fmt.Sprintf("honestbench: unregistered rule %q", ruleID))
	}
	if rule.Advisory && !r.advisory {
		return
	}
	key := fmt.Sprintf("%s:%d", ruleID, pos)
	if _, duplicate := r.diagnostics[key]; duplicate {
		return
	}
	r.diagnostics[key] = struct{}{}
	if end == token.NoPos {
		end = pos
	}
	diagnostic := analysis.Diagnostic{
		Pos:            pos,
		End:            end,
		Category:       ruleID,
		Message:        message,
		SuggestedFixes: fixes,
	}
	r.pass.Report(diagnostic)
}

func (r *runner) fileAt(pos token.Pos) *ast.File {
	tf := r.pass.Fset.File(pos)
	return r.files[tf]
}

func (r *runner) isGenerated(pos token.Pos) bool {
	file := r.fileAt(pos)
	return file != nil && r.generated[file]
}

func (r *runner) hasCommentBetween(start, end token.Pos) bool {
	file := r.fileAt(start)
	if file == nil || r.fileAt(end) != file {
		return true
	}
	for _, group := range file.Comments {
		if group.Pos() >= start && group.Pos() < end {
			return true
		}
	}
	return false
}

func (r *runner) filename(pos token.Pos) string {
	return filepath.Clean(r.pass.Fset.PositionFor(pos, false).Filename)
}
