package benchgate

import (
	"strings"
	"testing"
)

// These benchmarks exercise the core benchgate engine paths: parsing,
// comparison, and report rendering. They use B.Loop, ReportAllocs, and
// correct timer boundaries. Results are written to package-level sinks to
// defeat dead-code elimination.

// sinkBench prevents the compiler from eliminating benchmark work.
var sinkBench any

// BenchmarkParseBenchOutput measures parsing of realistic benchmark output.
func BenchmarkParseBenchOutput(b *testing.B) {
	output := benchOutputForBench()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = ParseBenchOutput(strings.NewReader(output), "bench")
	}
}

// BenchmarkCompare measures the full comparison pipeline.
func BenchmarkCompare(b *testing.B) {
	output := benchOutputForBench()
	base, _ := ParseBenchOutput(strings.NewReader(output), "base")
	cand, _ := ParseBenchOutput(strings.NewReader(output), "cand")
	policy := DefaultPolicy()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report, _ := Compare(base, cand, policy)
		sinkBench = report
	}
}

// BenchmarkWriteText measures text report rendering.
func BenchmarkWriteText(b *testing.B) {
	report := reportForBench()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = WriteText(&strings.Builder{}, report)
	}
}

// BenchmarkWriteJSON measures JSON report rendering.
func BenchmarkWriteJSON(b *testing.B) {
	report := reportForBench()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = WriteJSON(&strings.Builder{}, report)
	}
}

// BenchmarkWriteMarkdown measures Markdown report rendering.
func BenchmarkWriteMarkdown(b *testing.B) {
	report := reportForBench()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = WriteMarkdown(&strings.Builder{}, report, "")
	}
}

// BenchmarkComputeStats measures per-series statistics computation.
func BenchmarkComputeStats(b *testing.B) {
	samples := make([]float64, 10)
	for i := range samples {
		samples[i] = float64(100 + i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sinkBench = computeStats(samples)
	}
}

// benchOutputForBench builds realistic benchmark output for benchmarking.
func benchOutputForBench() string {
	var b strings.Builder
	b.WriteString("goos: linux\n")
	b.WriteString("goarch: amd64\n")
	b.WriteString("pkg: github.com/example/test\n")
	b.WriteString("cpu: Intel(R) Core(TM) i7\n")
	for i := 0; i < 10; i++ {
		b.WriteString("BenchmarkFoo-8\t1000000\t100 ns/op\t0 B/op\t0 allocs/op\n")
	}
	b.WriteString("PASS\n")
	return b.String()
}

// reportForBench builds a realistic report for benchmarking rendering.
func reportForBench() *ComparisonReport {
	base, _ := ParseBenchOutput(strings.NewReader(benchOutputForBench()), "base")
	cand, _ := ParseBenchOutput(strings.NewReader(benchOutputForBench()), "cand")
	report, _ := Compare(base, cand, DefaultPolicy())
	return report
}
