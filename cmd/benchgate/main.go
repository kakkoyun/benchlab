// benchgate runs Go benchmarks N times, computes the coefficient of variation
// (CV) per benchmark, and emits a pass/fail verdict against a CV threshold.
// Optionally compares against a baseline file using benchstat.
//
// Exit codes: 0 = PASS, 1 = FAIL, 2 = error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/kakkoyun/benchlab/internal/benchgate"
)

func run() int {
	pkg := flag.String("pkg", "./...", "package pattern to benchmark")
	bench := flag.String("bench", ".", "benchmark regexp")
	count := flag.Int("count", 10, "number of benchmark runs")
	benchtime := flag.String("benchtime", "1s", "go test -benchtime value")
	cvThreshold := flag.Float64("cv-threshold", 5.0, "max acceptable CV percent")
	jsonOut := flag.Bool("json", false, "emit JSON output")
	baseline := flag.String("baseline", "", "path to saved go test -bench output for benchstat comparison")
	save := flag.String("save", "", "path to write raw benchmark output (future baseline)")
	flag.Parse()

	workDir, pkgPattern := benchgate.ResolvePackageTarget(*pkg)

	goArgs := []string{
		"test",
		"-run", "^$",
		fmt.Sprintf("-bench=%s", *bench),
		"-benchmem",
		fmt.Sprintf("-count=%d", *count),
		fmt.Sprintf("-benchtime=%s", *benchtime),
		pkgPattern,
	}

	cmdStr := fmt.Sprintf("go test -bench=%s -count=%d -benchtime=%s %s", *bench, *count, *benchtime, pkgPattern)
	if !*jsonOut {
		fmt.Printf("benchgate: %s\n\n", cmdStr)
	}

	goCmd := exec.Command("go", goArgs...) //nolint:gosec
	if workDir != "" {
		goCmd.Dir = workDir
	}
	out, err := goCmd.CombinedOutput()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			fmt.Fprintf(os.Stderr, "benchgate: go test failed: %v\n%s\n", err, out)
			return 2
		}
		// Exit code 2 from go test means compile or vet error.
		if exitErr.ExitCode() == 2 {
			fmt.Fprintf(os.Stderr, "benchgate: go test build/vet error:\n%s\n", out)
			return 2
		}
		// Exit code 1 means a test (not benchmark) failed; benchmark output
		// may still be present, so continue processing.
	}

	rawOutput := string(out)

	if *save != "" {
		if werr := os.WriteFile(*save, out, 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "benchgate: save %q: %v\n", *save, werr)
			return 2
		}
	}

	samples := benchgate.ParseOutput(rawOutput)
	if len(samples) == 0 {
		fmt.Fprintf(os.Stderr, "benchgate: no benchmark results found\n%s\n", rawOutput)
		return 2
	}

	// Sort benchmark names for deterministic output.
	names := make([]string, 0, len(samples))
	for n := range samples {
		names = append(names, n)
	}
	sort.Strings(names)

	results := make([]benchgate.Result, 0, len(names))
	failing := 0

	for _, name := range names {
		mean, stddev, cv, note := benchgate.ComputeCV(samples[name])
		pass := cv <= *cvThreshold
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

	overallPass := failing == 0

	if *jsonOut {
		rep := benchgate.Report{
			Verdict:    benchgate.VerdictLabel(overallPass),
			Threshold:  *cvThreshold,
			Benchmarks: results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(rep); encErr != nil {
			fmt.Fprintf(os.Stderr, "benchgate: json encode: %v\n", encErr)
			return 2
		}
	} else {
		for _, r := range results {
			mark := "✓"
			suffix := ""
			if !r.Pass {
				mark = "✗"
				suffix = fmt.Sprintf(" (exceeds %.1f%% threshold)", *cvThreshold)
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
				len(results), *cvThreshold)
		} else {
			fmt.Printf("VERDICT: FAIL — %d/%d benchmarks exceed CV threshold %.1f%%\n",
				failing, len(results), *cvThreshold)
		}
	}

	if *baseline != "" {
		tmp, tmpErr := os.CreateTemp("", "benchgate-new-*.txt")
		if tmpErr != nil {
			fmt.Fprintf(os.Stderr, "benchgate: create temp file: %v\n", tmpErr)
			return 2
		}
		defer os.Remove(tmp.Name())

		if _, wErr := tmp.WriteString(rawOutput); wErr != nil {
			tmp.Close()
			fmt.Fprintf(os.Stderr, "benchgate: write temp file: %v\n", wErr)
			return 2
		}
		tmp.Close()

		fmt.Println("\n--- benchstat comparison ---")
		bsOut, bsErr := exec.Command("benchstat", *baseline, tmp.Name()).CombinedOutput() //nolint:gosec
		fmt.Print(string(bsOut))
		if bsErr != nil {
			fmt.Fprintf(os.Stderr, "benchgate: benchstat: %v\n", bsErr)
		}
	}

	if !overallPass {
		return 1
	}
	return 0
}

func main() {
	os.Exit(run())
}
