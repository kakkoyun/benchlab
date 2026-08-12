package honestbench

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "default", "default_test")
}

func TestAnalyzerAdvisory(t *testing.T) {
	if err := Analyzer.Flags.Set("advisory", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Analyzer.Flags.Set("advisory", "false"); err != nil {
			t.Errorf("reset advisory flag: %v", err)
		}
	})
	analysistest.Run(t, analysistest.TestData(), Analyzer, "advisory")
}

func TestSuggestedFixes(t *testing.T) {
	if err := Analyzer.Flags.Set("advisory", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Analyzer.Flags.Set("advisory", "false"); err != nil {
			t.Errorf("reset advisory flag: %v", err)
		}
	})
	analysistest.RunWithSuggestedFixes(t, analysistest.TestData(), Analyzer, "fixes", "generated", "commentfix")
}

func TestAnalyzerValid(t *testing.T) {
	if err := analysis.Validate([]*analysis.Analyzer{Analyzer}); err != nil {
		t.Fatalf("analysis.Validate: %v", err)
	}
}

func TestRuleRegistry(t *testing.T) {
	for id, rule := range ruleRegistry {
		if id == "" || rule.ID != id || rule.Summary == "" {
			t.Errorf("invalid rule registry entry %q: %#v", id, rule)
		}
		if strings.TrimSpace(rule.Summary) != rule.Summary {
			t.Errorf("rule %q has surrounding whitespace", id)
		}
	}
}
