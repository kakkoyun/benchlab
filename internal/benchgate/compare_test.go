package benchgate

import (
	"fmt"
	"strings"
	"testing"
)

// benchOutput builds realistic go test -bench output for testing.
func benchOutput(benchmarks []benchLine) string {
	var b strings.Builder
	b.WriteString("goos: linux\n")
	b.WriteString("goarch: amd64\n")
	b.WriteString("pkg: github.com/example/test\n")
	b.WriteString("cpu: Intel(R) Core(TM) i7\n")
	for _, line := range benchmarks {
		b.WriteString(line.text + "\n")
	}
	b.WriteString("PASS\n")
	return b.String()
}

type benchLine struct {
	text string
}

// sampleLine builds a benchmark result line with ns/op, B/op, allocs/op.
func sampleLine(name string, nsPerOp, bPerOp, allocsPerOp float64) string {
	return fmt.Sprintf("%s\t1000000\t%s ns/op\t%s B/op\t%s allocs/op",
		name, fmtFloat(nsPerOp), fmtFloat(bPerOp), fmtFloat(allocsPerOp))
}

func fmtFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

// makeSamples generates n identical ns/op values for a benchmark.
func makeSamples(name string, n int, nsPerOp, bPerOp, allocsPerOp float64) []benchLine {
	lines := make([]benchLine, n)
	for i := range lines {
		lines[i] = benchLine{sampleLine(name, nsPerOp, bPerOp, allocsPerOp)}
	}
	return lines
}

// makeVariedSamples generates n ns/op values with some variance.
func makeVariedSamples(name string, values []float64) []benchLine {
	lines := make([]benchLine, len(values))
	for i, v := range values {
		lines[i] = benchLine{sampleLine(name, v, 0, 0)}
	}
	return lines
}

func mustParse(t *testing.T, output, label string) *ParsedResults {
	t.Helper()
	pr, err := ParseBenchOutput(strings.NewReader(output), label)
	if err != nil {
		t.Fatalf("ParseBenchOutput(%s): %v", label, err)
	}
	return pr
}

// --- Parsing tests ---

func TestParseBenchOutput_GroupsByUnit(t *testing.T) {
	output := benchOutput([]benchLine{
		{sampleLine("BenchmarkFoo-8", 100, 64, 1)},
	})
	pr := mustParse(t, output, "test")

	var hasSec, hasB, hasAllocs bool
	for key := range pr.Series {
		switch key.Unit {
		case "sec/op":
			hasSec = true
		case "B/op":
			hasB = true
		case "allocs/op":
			hasAllocs = true
		}
	}
	if !hasSec {
		t.Error("expected sec/op unit")
	}
	if !hasB {
		t.Error("expected B/op unit")
	}
	if !hasAllocs {
		t.Error("expected allocs/op unit")
	}
}

func TestParseBenchOutput_PreservesFullName(t *testing.T) {
	output := benchOutput([]benchLine{
		{sampleLine("BenchmarkEncode/format=json-8", 100, 64, 1)},
	})
	pr := mustParse(t, output, "test")

	for key := range pr.Series {
		// benchfmt strips the "Benchmark" prefix from the name.
		if key.FullName != "Encode/format=json-8" {
			t.Errorf("expected full name Encode/format=json-8, got %q", key.FullName)
		}
		if key.GOMAXPROCS != "8" {
			t.Errorf("expected GOMAXPROCS 8, got %q", key.GOMAXPROCS)
		}
	}
}

func TestParseBenchOutput_ExtractsEnv(t *testing.T) {
	output := benchOutput([]benchLine{
		{sampleLine("BenchmarkFoo-8", 100, 64, 1)},
	})
	pr := mustParse(t, output, "test")
	if pr.Env.GOOS != "linux" {
		t.Errorf("expected GOOS linux, got %q", pr.Env.GOOS)
	}
	if pr.Env.GOARCH != "amd64" {
		t.Errorf("expected GOARCH amd64, got %q", pr.Env.GOARCH)
	}
	if pr.Env.CPU != "Intel(R) Core(TM) i7" {
		t.Errorf("expected CPU, got %q", pr.Env.CPU)
	}
}

func TestParseBenchOutput_DuplicateNames(t *testing.T) {
	output := benchOutput(makeSamples("BenchmarkFoo-8", 10, 100, 0, 0))
	pr := mustParse(t, output, "test")
	for key := range pr.Series {
		if key.Unit == "sec/op" {
			if len(pr.Series[key]) != 10 {
				t.Errorf("expected 10 samples, got %d", len(pr.Series[key]))
			}
		}
	}
}

// --- Comparison tests ---

func TestCompare_Pass_IdenticalResults(t *testing.T) {
	lines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	output := benchOutput(lines)
	base := mustParse(t, output, "base")
	cand := mustParse(t, output, "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s", report.Verdict)
	}
}

func TestCompare_Regression_RuntimeSignificant(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 120, 0, 0) // 20% slower
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictRegression {
		t.Errorf("expected REGRESSION, got %s", report.Verdict)
	}
	// Check that the sec/op row is REGRESSION
	for _, row := range report.Rows {
		if row.Key.Unit == "sec/op" && row.Status != RowRegression {
			t.Errorf("expected sec/op REGRESSION, got %s", row.Status)
		}
	}
}

func TestCompare_NoRegression_RuntimeWithinThreshold(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 105, 0, 0) // 5% slower, within 10% threshold
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictPass {
		t.Errorf("expected PASS (within threshold), got %s", report.Verdict)
	}
}

func TestCompare_Regression_AllocsAnyIncrease(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 1) // allocs 0→1
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictRegression {
		t.Errorf("expected REGRESSION for alloc increase, got %s", report.Verdict)
	}
}

func TestCompare_Regression_ZeroToNonzeroAllocs(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 100, 64, 2) // B/op 0→64, allocs 0→2
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictRegression {
		t.Errorf("expected REGRESSION for zero-to-nonzero, got %s", report.Verdict)
	}
	// Check that the B/op and allocs/op rows have infinite regression flag
	for _, row := range report.Rows {
		if row.Key.Unit == "B/op" || row.Key.Unit == "allocs/op" {
			if row.Status != RowRegression {
				t.Errorf("expected %s REGRESSION, got %s", row.Key.Unit, row.Status)
			}
			if !row.InfiniteRegression {
				t.Errorf("expected %s InfiniteRegression=true", row.Key.Unit)
			}
		}
	}
}

func TestCompare_Improvement_NotFailing(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 120, 64, 2)
	candLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0) // faster, fewer allocs
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictPass {
		t.Errorf("expected PASS (improvements don't fail), got %s", report.Verdict)
	}
	hasImprovement := false
	for _, row := range report.Rows {
		if row.Status == RowImprovement {
			hasImprovement = true
		}
	}
	if !hasImprovement {
		t.Error("expected at least one IMPROVEMENT row")
	}
}

func TestCompare_NewBenchmark_Informational(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines = append(candLines, makeSamples("BenchmarkBar-8", 10, 50, 0, 0)...)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictPass {
		t.Errorf("expected PASS with new benchmark informational, got %s", report.Verdict)
	}
	if report.Summary.New == 0 {
		t.Error("expected at least one NEW benchmark")
	}
}

func TestCompare_RemovedBenchmark_Informational(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	baseLines = append(baseLines, makeSamples("BenchmarkBar-8", 10, 50, 0, 0)...)
	candLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictPass {
		t.Errorf("expected PASS with removed benchmark informational, got %s", report.Verdict)
	}
	if report.Summary.Removed == 0 {
		t.Error("expected at least one REMOVED benchmark")
	}
}

func TestCompare_NoCommonGated_Error(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkBar-8", 10, 100, 0, 0) // different name, no common
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	_, err := Compare(base, cand, DefaultPolicy())
	if err == nil {
		t.Fatal("expected error for no common gated series")
	}
}

func TestCompare_Inconclusive_InsufficientSamples(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 5, 100, 0, 0) // only 5 samples
	candLines := makeSamples("BenchmarkFoo-8", 5, 120, 0, 0)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictInconclusive {
		t.Errorf("expected INCONCLUSIVE for insufficient samples, got %s", report.Verdict)
	}
}

func TestCompare_Inconclusive_HighCV(t *testing.T) {
	// Create samples with high variance (CV > 5%).
	baseValues := []float64{80, 90, 100, 110, 120, 80, 90, 100, 110, 120}
	candValues := []float64{100, 110, 120, 130, 140, 100, 110, 120, 130, 140}
	baseLines := makeVariedSamples("BenchmarkFoo-8", baseValues)
	candLines := makeVariedSamples("BenchmarkFoo-8", candValues)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictInconclusive {
		t.Errorf("expected INCONCLUSIVE for high CV, got %s", report.Verdict)
	}
}

func TestCompare_EnvMismatch_Error(t *testing.T) {
	baseOutput := "goos: linux\ngoarch: amd64\npkg: test\ncpu: AMD\nBenchmarkFoo-8\t1000\t100 ns/op\t0 B/op\t0 allocs/op\nPASS\n"
	candOutput := "goos: darwin\ngoarch: arm64\npkg: test\ncpu: Apple M4\nBenchmarkFoo-8\t1000\t100 ns/op\t0 B/op\t0 allocs/op\nPASS\n"
	base := mustParse(t, baseOutput, "base")
	cand := mustParse(t, candOutput, "cand")

	_, err := Compare(base, cand, DefaultPolicy())
	if err == nil {
		t.Fatal("expected error for environment mismatch")
	}
}

func TestCompare_EnvMismatch_Allowed(t *testing.T) {
	baseOutput := "goos: linux\ngoarch: amd64\npkg: test\ncpu: AMD\n" +
		strings.Repeat("BenchmarkFoo-8\t1000000\t100 ns/op\t0 B/op\t0 allocs/op\n", 10) + "PASS\n"
	candOutput := "goos: darwin\ngoarch: arm64\npkg: test\ncpu: Apple M4\n" +
		strings.Repeat("BenchmarkFoo-8\t1000000\t100 ns/op\t0 B/op\t0 allocs/op\n", 10) + "PASS\n"
	base := mustParse(t, baseOutput, "base")
	cand := mustParse(t, candOutput, "cand")

	policy := DefaultPolicy()
	policy.AllowEnvMismatch = true
	report, err := Compare(base, cand, policy)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictPass {
		t.Errorf("expected PASS with env mismatch allowed, got %s", report.Verdict)
	}
}

func TestCompare_Inconclusive_MissingUnit(t *testing.T) {
	// Base has sec/op and B/op, candidate only has sec/op.
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 64, 0)
	candLines := make([]benchLine, 10)
	for i := range candLines {
		candLines[i] = benchLine{"BenchmarkFoo-8\t1000000\t100 ns/op\t0 allocs/op"}
	}
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	// B/op exists on only one side → INCONCLUSIVE
	if report.Verdict != VerdictInconclusive {
		t.Errorf("expected INCONCLUSIVE for missing gated unit, got %s", report.Verdict)
	}
}

func TestCompare_MixedVerdicts(t *testing.T) {
	// One benchmark passes, one regresses.
	baseLines := makeSamples("BenchmarkPass-8", 10, 100, 0, 0)
	baseLines = append(baseLines, makeSamples("BenchmarkFail-8", 10, 100, 0, 0)...)
	candLines := makeSamples("BenchmarkPass-8", 10, 100, 0, 0)
	candLines = append(candLines, makeSamples("BenchmarkFail-8", 10, 120, 0, 0)...)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictRegression {
		t.Errorf("expected REGRESSION (mixed), got %s", report.Verdict)
	}
}

func TestCompare_DeltaExactlyAtThreshold_Pass(t *testing.T) {
	// 10% slower, threshold 10% → exactly at threshold → PASS (must be MORE than threshold)
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 110, 0, 0) // exactly 10%
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	// Check the sec/op row specifically
	for _, row := range report.Rows {
		if row.Key.Unit == "sec/op" {
			if row.Status == RowRegression {
				t.Errorf("expected PASS at exactly threshold (10%%), got REGRESSION. delta=%.2f", row.Delta)
			}
		}
	}
}

func TestCompare_SubBenchmarks_DistinctSeries(t *testing.T) {
	baseLines := makeSamples("BenchmarkEncode/format=json-8", 10, 100, 0, 0)
	baseLines = append(baseLines, makeSamples("BenchmarkEncode/format=gob-8", 10, 200, 0, 0)...)
	candLines := makeSamples("BenchmarkEncode/format=json-8", 10, 100, 0, 0)
	candLines = append(candLines, makeSamples("BenchmarkEncode/format=gob-8", 10, 240, 0, 0)...) // gob 20% slower
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictRegression {
		t.Errorf("expected REGRESSION for gob sub-benchmark, got %s", report.Verdict)
	}
}

func TestCompare_CPUVariants_DistinctSeries(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	baseLines = append(baseLines, makeSamples("BenchmarkFoo-16", 10, 80, 0, 0)...)
	candLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines = append(candLines, makeSamples("BenchmarkFoo-16", 10, 96, 0, 0)...) // -16 20% slower
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictRegression {
		t.Errorf("expected REGRESSION for CPU variant, got %s", report.Verdict)
	}
}

func TestCompare_CustomUnit_Informational(t *testing.T) {
	// Custom unit "items/sec" — not gated, informational only.
	baseOutput := "goos: linux\ngoarch: amd64\npkg: test\ncpu: Intel\n" +
		strings.Repeat("BenchmarkFoo-8\t1000000\t100 ns/op\t0 B/op\t0 allocs/op\t500 items/sec\n", 10) + "PASS\n"
	candOutput := "goos: linux\ngoarch: amd64\npkg: test\ncpu: Intel\n" +
		strings.Repeat("BenchmarkFoo-8\t1000000\t100 ns/op\t0 B/op\t0 allocs/op\t300 items/sec\n", 10) + "PASS\n"
	base := mustParse(t, baseOutput, "base")
	cand := mustParse(t, candOutput, "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictPass {
		t.Errorf("expected PASS (custom unit informational), got %s", report.Verdict)
	}
	hasInfo := false
	for _, row := range report.Rows {
		if row.Key.Unit == "items/sec" && row.Status == RowInformational {
			hasInfo = true
		}
	}
	if !hasInfo {
		t.Error("expected informational row for custom unit")
	}
}

// --- Waiver tests ---

func TestApplyWaiver_RegressionToWaived(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 120, 0, 0)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictRegression {
		t.Fatalf("expected REGRESSION before waiver, got %s", report.Verdict)
	}

	ApplyWaiver(report, WaiverMetadata{
		Enabled: true,
		Actor:   "maintainer",
		Label:   WaiverLabel,
		HeadSHA: "abc123",
	})

	if report.Verdict != VerdictWaived {
		t.Errorf("expected WAIVED after waiver, got %s", report.Verdict)
	}
	hasWaived := false
	for _, row := range report.Rows {
		if row.Status == RowWaived {
			hasWaived = true
		}
	}
	if !hasWaived {
		t.Error("expected at least one WAIVED row")
	}
}

func TestApplyWaiver_CannotOverrideInconclusive(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 5, 100, 0, 0) // insufficient samples
	candLines := makeSamples("BenchmarkFoo-8", 5, 120, 0, 0)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, err := Compare(base, cand, DefaultPolicy())
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if report.Verdict != VerdictInconclusive {
		t.Fatalf("expected INCONCLUSIVE before waiver, got %s", report.Verdict)
	}

	ApplyWaiver(report, WaiverMetadata{
		Enabled: true,
		Actor:   "maintainer",
		Label:   WaiverLabel,
		HeadSHA: "abc123",
	})

	if report.Verdict != VerdictInconclusive {
		t.Errorf("waiver cannot override INCONCLUSIVE, got %s", report.Verdict)
	}
}

func TestApplyWaiver_Disabled_Noop(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 120, 0, 0)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, _ := Compare(base, cand, DefaultPolicy())
	ApplyWaiver(report, WaiverMetadata{Enabled: false})

	if report.Verdict != VerdictRegression {
		t.Errorf("disabled waiver should not change verdict, got %s", report.Verdict)
	}
}

func TestApplyWaiver_SHAMismatch_Noop(t *testing.T) {
	baseLines := makeSamples("BenchmarkFoo-8", 10, 100, 0, 0)
	candLines := makeSamples("BenchmarkFoo-8", 10, 120, 0, 0)
	base := mustParse(t, benchOutput(baseLines), "base")
	cand := mustParse(t, benchOutput(candLines), "cand")

	report, _ := Compare(base, cand, DefaultPolicy())
	report.Identity.HeadSHA = "actual-head-sha"
	// Waiver for a different head SHA should not apply.
	ApplyWaiver(report, WaiverMetadata{
		Enabled: true,
		Actor:   "maintainer",
		Label:   WaiverLabel,
		HeadSHA: "different-head-sha",
	})

	if report.Verdict != VerdictRegression {
		t.Errorf("waiver with mismatched SHA should not apply, got %s", report.Verdict)
	}
}

func TestCompare_SyntaxErrors_Error(t *testing.T) {
	// Malformed benchmark line (missing value).
	baseOutput := "goos: linux\ngoarch: amd64\npkg: test\nBenchmarkFoo-8\t1000000\t ns/op\nPASS\n"
	candOutput := "goos: linux\ngoarch: amd64\npkg: test\nBenchmarkFoo-8\t1000000\t100 ns/op\t0 B/op\t0 allocs/op\nPASS\n"
	base := mustParse(t, baseOutput, "base")
	cand := mustParse(t, candOutput, "cand")

	_, err := Compare(base, cand, DefaultPolicy())
	if err == nil {
		t.Fatal("expected error for syntax errors in benchmark output")
	}
}

func TestValidateWaiverEvent_Valid(t *testing.T) {
	if err := ValidateWaiverEvent("labeled", WaiverLabel, "abc123"); err != nil {
		t.Errorf("expected valid: %v", err)
	}
}

func TestValidateWaiverEvent_WrongAction(t *testing.T) {
	if err := ValidateWaiverEvent("synchronize", WaiverLabel, "abc123"); err == nil {
		t.Error("expected error for wrong action")
	}
}

func TestValidateWaiverEvent_WrongLabel(t *testing.T) {
	if err := ValidateWaiverEvent("labeled", "other-label", "abc123"); err == nil {
		t.Error("expected error for wrong label")
	}
}

func TestValidateWaiverEvent_EmptySHA(t *testing.T) {
	if err := ValidateWaiverEvent("labeled", WaiverLabel, ""); err == nil {
		t.Error("expected error for empty SHA")
	}
}

// --- Stats tests ---

func TestComputeStats_Median(t *testing.T) {
	stats := computeStats([]float64{1, 2, 3, 4, 5})
	if stats.Median != 3 {
		t.Errorf("expected median 3, got %f", stats.Median)
	}
	if stats.N != 5 {
		t.Errorf("expected N 5, got %d", stats.N)
	}
}

func TestComputeStats_EvenMedian(t *testing.T) {
	stats := computeStats([]float64{1, 2, 3, 4})
	if stats.Median != 2.5 {
		t.Errorf("expected median 2.5, got %f", stats.Median)
	}
}

func TestComputeStats_CV(t *testing.T) {
	stats := computeStats([]float64{100, 100, 100})
	if stats.CV != 0 {
		t.Errorf("expected CV 0 for identical samples, got %f", stats.CV)
	}
}

func TestPercentDelta(t *testing.T) {
	tests := []struct {
		base, cand, want float64
	}{
		{100, 110, 10.0},
		{100, 90, -10.0},
		{100, 100, 0.0},
		{0, 0, 0.0},
	}
	for _, tt := range tests {
		got := percentDelta(tt.base, tt.cand)
		if got != tt.want && !(tt.base == 0 && tt.cand > 0) {
			if tt.base == 0 && tt.cand == 0 {
				if got != 0 {
					t.Errorf("percentDelta(%f,%f) = %f, want %f", tt.base, tt.cand, got, tt.want)
				}
			} else {
				if got != tt.want {
					t.Errorf("percentDelta(%f,%f) = %f, want %f", tt.base, tt.cand, got, tt.want)
				}
			}
		}
	}
}

func TestPercentDelta_ZeroToNonzero(t *testing.T) {
	d := percentDelta(0, 1)
	// percentDelta returns 0 for base==0; callers use InfiniteRegression flag.
	if d != 0 {
		t.Errorf("expected 0 for zero-to-nonzero (base==0), got %f", d)
	}
}
