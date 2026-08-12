package benchgate

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var benchmarkRE = regexp.MustCompile(`^(Benchmark[^\s-]+)(?:-\d+)?\s+\d+\s+([\d.]+)\s+ns/op`)

// Result holds statistics for one benchmark.
type Result struct {
	Name   string  `json:"name"`
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
	CV     float64 `json:"cv"`
	Pass   bool    `json:"pass"`
	Note   string  `json:"note,omitempty"`
}

// Report is the top-level JSON output structure.
type Report struct {
	Verdict    string   `json:"verdict"`
	Threshold  float64  `json:"threshold"`
	Benchmarks []Result `json:"benchmarks"`
}

// ParseOutput groups ns/op samples by benchmark name, stripping GOMAXPROCS suffixes.
func ParseOutput(output string) map[string][]float64 {
	samples := make(map[string][]float64)
	for _, line := range strings.Split(output, "\n") {
		match := benchmarkRE.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		value, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			continue
		}
		samples[match[1]] = append(samples[match[1]], value)
	}
	return samples
}

// ComputeCV returns the mean, sample standard deviation, coefficient of variation,
// and an explanatory note for degenerate sample sets.
func ComputeCV(samples []float64) (mean, stddev, cv float64, note string) {
	if len(samples) == 0 {
		return 0, 0, 0, "no samples"
	}
	var sum float64
	for _, value := range samples {
		sum += value
	}
	mean = sum / float64(len(samples))
	if len(samples) < 2 {
		return mean, 0, 0, "insufficient samples"
	}
	var variance float64
	for _, value := range samples {
		delta := value - mean
		variance += delta * delta
	}
	stddev = math.Sqrt(variance / float64(len(samples)-1))
	if mean > 0 {
		cv = 100 * stddev / mean
	}
	return mean, stddev, cv, ""
}

// ResolvePackageTarget returns the working directory and package pattern for go test.
func ResolvePackageTarget(pkg string) (dir, pattern string) {
	if !strings.HasPrefix(pkg, ".") && !strings.HasPrefix(pkg, "/") {
		return "", pkg
	}

	base := strings.TrimSuffix(strings.TrimSuffix(pkg, "..."), "/")
	if base == "" {
		base = "."
	}
	resolved, err := filepath.Abs(base)
	if err != nil {
		return "", pkg
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", pkg
	}
	_, cwdModErr := os.Stat(filepath.Join(cwd, "go.mod"))
	_, targetModErr := os.Stat(filepath.Join(resolved, "go.mod"))
	if targetModErr == nil && (cwdModErr != nil || resolved != cwd) {
		return resolved, "./..."
	}
	return "", pkg
}

// VerdictLabel returns PASS or FAIL.
func VerdictLabel(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}
