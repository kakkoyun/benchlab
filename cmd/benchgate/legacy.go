package main

import (
	"fmt"
	"os/exec"
	"sort"

	"github.com/kakkoyun/benchlab/internal/benchgate"
)

// runGoTest executes go test with the given args and working directory.
func runGoTest(args []string, dir string) ([]byte, error) {
	cmd := exec.Command("go", args...) //nolint:gosec
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 2 {
				return out, fmt.Errorf("go test build/vet error (exit %d)", exitErr.ExitCode())
			}
			// Exit code 1: test failure; benchmark output may still be present.
		}
		return out, err
	}
	return out, nil
}

// computeCVResults computes CV for each benchmark and returns sorted results
// plus the count of failing benchmarks.
func computeCVResults(samples map[string][]float64, threshold float64) ([]benchgate.Result, int) {
	names := make([]string, 0, len(samples))
	for n := range samples {
		names = append(names, n)
	}
	sort.Strings(names)

	results := make([]benchgate.Result, 0, len(names))
	failing := 0
	for _, name := range names {
		mean, stddev, cv, note := benchgate.ComputeCV(samples[name])
		pass := cv <= threshold
		if !pass {
			failing++
		}
		results = append(results, benchgate.Result{
			Name:   name,
			Mean:   mean,
			Stddev: stddev,
			CV:     cv,
			Pass:   pass,
			Note:   note,
		})
	}
	return results, failing
}

// printLegacyText prints the legacy CV-only text output.
func printLegacyText(results []benchgate.Result, threshold float64, overallPass bool, failing int) {
	for _, r := range results {
		mark := "✓"
		suffix := ""
		if !r.Pass {
			mark = "✗"
			suffix = fmt.Sprintf(" (exceeds %.1f%% threshold)", threshold)
		}
		if r.Note != "" {
			suffix = fmt.Sprintf(" [%s]", r.Note)
		}
		fmt.Printf("  %-44s  mean=%8.1f ns/op  cv=%5.1f%%  %s%s\n",
			r.Name, r.Mean, r.CV, mark, suffix)
	}
	fmt.Println()
	if overallPass {
		fmt.Printf("VERDICT: PASS — all %d benchmarks within CV threshold %.1f%%\n",
			len(results), threshold)
	} else {
		fmt.Printf("VERDICT: FAIL — %d/%d benchmarks exceed CV threshold %.1f%%\n",
			failing, len(results), threshold)
	}
}
