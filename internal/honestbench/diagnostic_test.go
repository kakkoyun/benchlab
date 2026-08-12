package honestbench

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestAllEmittedCategoriesAreRegistered(t *testing.T) {
	fset := token.NewFileSet()
	for _, fileName := range []string{"rules.go", "timer.go", "advisory.go"} {
		parsed, err := parser.ParseFile(fset, fileName, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", fileName, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 3 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "report" {
				return true
			}
			lit, ok := call.Args[2].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			id := strings.Trim(lit.Value, `"`)
			if _, registered := ruleRegistry[id]; !registered {
				t.Errorf("%s emits unregistered category %q", fileName, id)
			}
			return true
		})
	}
}
