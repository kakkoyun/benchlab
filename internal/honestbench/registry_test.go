package honestbench

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestEveryRuleHasPositiveFixture(t *testing.T) {
	patterns := map[string]string{
		"missing-loop":                       "benchmark scope has no B.Loop",
		"noncanonical-b-loop":                "B.Loop must be the sole condition",
		"multiple-b-loop":                    "only the first testing.B.Loop",
		"mixed-loop":                         "mixes testing.B.Loop with b.N",
		"b-n-with-b-loop":                    "b.N is only guaranteed",
		"wrong-b-n-count":                    "provably not executed exactly b.N times",
		"reset-timer-in-loop":                "ResetTimer inside a measured iteration",
		"stoptimer-without-starttimer":       "reachable iteration path stops timing",
		"work-while-timer-stopped":           "every reachable work statement",
		"outer-b-in-subbenchmark":            "subbenchmark callback uses the parent",
		"runparallel-missing-next":           "neither iterates with pb.Next",
		"runparallel-wrong-loop":             "must iterate with pb.Next",
		"runparallel-timer":                  "timer methods have global effect",
		"runparallel-subbenchmark":           "must not start a subbenchmark",
		"setparallelism-order":               "definitely executes after RunParallel",
		"suggest-bloop":                      "canonical b.N loop can use",
		"timed-setup":                        "nontrivial setup before a legacy b.N loop",
		"timed-cleanup":                      "nontrivial cleanup after a legacy b.N loop",
		"discarded-result":                   "result-returning call is discarded",
		"missing-sink":                       "result is used only by a blank assignment",
		"package-write-in-loop":              "writes a package variable",
		"benchmark-config-in-loop":           "benchmark configuration should not execute",
		"noncanonical-b-n-loop":              "unusual and its exact iteration count cannot be proven",
		"setparallelism-without-runparallel": "has no matching RunParallel",
		"reused-mutated-input":               "in-place sort or reverse operation reuses input",
		"redundant-b-loop-timer":             "duplicates B.Loop behavior",
	}
	if len(patterns) != len(ruleRegistry) {
		t.Fatalf("coverage pattern count = %d; rule registry count = %d", len(patterns), len(ruleRegistry))
	}

	files := []string{
		"testdata/src/default/default_test.go",
		"testdata/src/advisory/advisory_test.go",
		"testdata/src/fixes/fixes_test.go",
	}
	var comments strings.Builder
	fset := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, group := range file.Comments {
			comments.WriteString(group.Text())
		}
	}
	for id := range ruleRegistry {
		pattern, ok := patterns[id]
		if !ok {
			t.Errorf("rule %q has no fixture pattern", id)
			continue
		}
		if !strings.Contains(comments.String(), pattern) {
			t.Errorf("rule %q has no positive fixture matching %q", id, pattern)
		}
	}
}
