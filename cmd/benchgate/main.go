// benchgate is a Go benchmark regression gate.
//
// It runs benchmarks, compares base and candidate results with
// benchstat-style statistics (Mann-Whitney U test via golang.org/x/perf),
// and emits a PASS/REGRESSION/INCONCLUSIVE/WAIVED/ERROR verdict.
//
// Usage:
//
//	benchgate run [flags]                 collect and compare in one step
//	benchgate compare [flags]              compare two saved result files
//	benchgate github-report [flags]        trusted GitHub PR comment helper
//	benchgate [flags]                      legacy CV-only mode
//
// Exit codes: 0 = pass or valid waiver, 1 = regression or inconclusive, 2 = error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kakkoyun/benchlab/internal/benchgate"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		return runLegacy(args)
	}

	switch args[0] {
	case "run":
		return runCompare(args[1:])
	case "compare":
		return runCompareCmd(args[1:])
	case "github-report":
		return runGitHubReport(args[1:])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return 0
	default:
		// Legacy mode: no subcommand, all args are flags.
		if strings.HasPrefix(args[0], "-") {
			return runLegacy(args)
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		printUsage(os.Stderr)
		return 2
	}
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `benchgate — Go benchmark regression gate

Usage:
  benchgate run [flags]                 Collect base+candidate and compare
  benchgate compare [flags]             Compare two saved result files
  benchgate github-report [flags]       Trusted GitHub PR comment helper
  benchgate [flags]                      Legacy CV-only mode

Commands:
  run          Create a base worktree, run counterbalanced benchmarks in both
               worktrees, compare with the statistical engine, and emit a verdict.
  compare      Compare two pre-collected result files (-base, -candidate).
  github-report  Trusted helper that posts/updates a PR comment from validated
               report JSON. Never checks out or executes PR code.

Exit codes: 0 = pass or valid waiver, 1 = regression or inconclusive, 2 = error.
`)
}

// exitCodeForVerdict maps a verdict to the process exit code.
func exitCodeForVerdict(v benchgate.Verdict) int {
	switch v {
	case benchgate.VerdictPass, benchgate.VerdictWaived:
		return 0
	case benchgate.VerdictRegression, benchgate.VerdictInconclusive:
		return 1
	default:
		return 2
	}
}

// --- run / compare shared flags ---

type compareFlags struct {
	pkg              string
	bench            string
	count            int
	benchtime        string
	cpu              string
	setup            string
	baseDir          string
	candDir          string
	workDir          string
	baseFile         string
	candidateFile    string
	runtimeThreshold float64
	bytesThreshold   float64
	allocsThreshold  float64
	cvThreshold      float64
	alpha            float64
	minSamples       int
	allowEnvMismatch bool
	jsonOut          string
	markdownOut      string
	artifactURL      string
	// identity
	repo     string
	baseSHA  string
	headSHA  string
	mergeSHA string
	prNumber int
	runID    string
	attempt  int
	// waiver
	waiverEnabled bool
	waiverActor   string
	waiverLabel   string
	waiverHeadSHA string
}

func registerCompareFlags(fs *flag.FlagSet, f *compareFlags) {
	fs.StringVar(&f.pkg, "pkg", "./...", "package pattern to benchmark")
	fs.StringVar(&f.bench, "bench", ".", "benchmark regexp")
	fs.IntVar(&f.count, "count", 10, "number of samples per side")
	fs.StringVar(&f.benchtime, "benchtime", "1s", "go test -benchtime value")
	fs.StringVar(&f.cpu, "cpu", "", "optional -cpu value (e.g. 1,2,4)")
	fs.StringVar(&f.setup, "setup", "", "setup command run once per worktree before collection")
	fs.StringVar(&f.baseDir, "base-dir", "", "base worktree directory")
	fs.StringVar(&f.candDir, "cand-dir", "", "candidate worktree directory (default: cwd)")
	fs.StringVar(&f.workDir, "work-dir", "", "directory for generated files (default: $RUNNER_TEMP)")
	fs.StringVar(&f.baseFile, "base", "", "path to saved base benchmark output (compare mode)")
	fs.StringVar(&f.candidateFile, "candidate", "", "path to saved candidate benchmark output (compare mode)")
	fs.Float64Var(&f.runtimeThreshold, "runtime-threshold", 10.0, "runtime regression threshold percent (sec/op)")
	fs.Float64Var(&f.bytesThreshold, "bytes-threshold", 0.0, "bytes regression threshold percent (B/op)")
	fs.Float64Var(&f.allocsThreshold, "allocs-threshold", 0.0, "allocs regression threshold percent (allocs/op)")
	fs.Float64Var(&f.cvThreshold, "cv-threshold", 5.0, "max acceptable CV percent")
	fs.Float64Var(&f.alpha, "alpha", 0.05, "significance alpha for Mann-Whitney U test")
	fs.IntVar(&f.minSamples, "min-samples", 10, "minimum samples per side for a conclusive decision")
	fs.BoolVar(&f.allowEnvMismatch, "allow-env-mismatch", false, "allow comparison across incompatible environments")
	fs.StringVar(&f.jsonOut, "json-out", "", "write JSON report to path")
	fs.StringVar(&f.markdownOut, "markdown-out", "", "write Markdown report to path")
	fs.StringVar(&f.artifactURL, "artifact-url", "", "URL to workflow artifact (for Markdown link)")
	// identity
	fs.StringVar(&f.repo, "repo", "", "repository (owner/repo)")
	fs.StringVar(&f.baseSHA, "base-sha", "", "base SHA")
	fs.StringVar(&f.headSHA, "head-sha", "", "head SHA")
	fs.StringVar(&f.mergeSHA, "merge-sha", "", "merge SHA")
	fs.IntVar(&f.prNumber, "pr", 0, "PR number")
	fs.StringVar(&f.runID, "run-id", "", "GitHub Actions run ID")
	fs.IntVar(&f.attempt, "attempt", 0, "GitHub Actions run attempt")
	// waiver
	fs.BoolVar(&f.waiverEnabled, "waiver-enabled", false, "enable one-shot regression waiver")
	fs.StringVar(&f.waiverActor, "waiver-actor", "", "GitHub actor who applied the waiver")
	fs.StringVar(&f.waiverLabel, "waiver-label", benchgate.WaiverLabel, "waiver label name")
	fs.StringVar(&f.waiverHeadSHA, "waiver-head-sha", "", "head SHA the waiver applies to")
}

func (f *compareFlags) toPolicy() benchgate.Policy {
	p := benchgate.DefaultPolicy()
	p.Alpha = f.alpha
	p.MaxCV = f.cvThreshold
	p.MinSamples = f.minSamples
	p.RuntimeThreshold = f.runtimeThreshold
	p.BytesThreshold = f.bytesThreshold
	p.AllocsThreshold = f.allocsThreshold
	p.AllowEnvMismatch = f.allowEnvMismatch
	// Rebuild gated units with custom thresholds.
	p.GatedUnits = []benchgate.GatedUnit{
		{Unit: "sec/op", Direction: benchgate.DirectionLowerIsBetter, Threshold: f.runtimeThreshold},
		{Unit: "B/op", Direction: benchgate.DirectionLowerIsBetter, Threshold: f.bytesThreshold},
		{Unit: "allocs/op", Direction: benchgate.DirectionLowerIsBetter, Threshold: f.allocsThreshold},
	}
	return p
}

func (f *compareFlags) toIdentity() benchgate.Identity {
	return benchgate.Identity{
		Repository: f.repo,
		BaseSHA:    f.baseSHA,
		HeadSHA:    f.headSHA,
		MergeSHA:   f.mergeSHA,
		PRNumber:   f.prNumber,
		RunID:      f.runID,
		Attempt:    f.attempt,
	}
}

func (f *compareFlags) toWaiver() benchgate.WaiverMetadata {
	return benchgate.WaiverMetadata{
		Enabled: f.waiverEnabled,
		Actor:   f.waiverActor,
		Label:   f.waiverLabel,
		HeadSHA: f.waiverHeadSHA,
	}
}

// --- benchgate run ---

func runCompare(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	f := &compareFlags{}
	registerCompareFlags(fs, f)
	fs.Parse(args)

	policy := f.toPolicy()

	// Collect benchmarks.
	collectOpts := benchgate.CollectOptions{
		Pkg:       f.pkg,
		Bench:     f.bench,
		Count:     f.count,
		Benchtime: f.benchtime,
		CPU:       f.cpu,
		Setup:     f.setup,
		BaseDir:   f.baseDir,
		CandDir:   f.candDir,
		WorkDir:   f.workDir,
	}
	collectResult, err := benchgate.Collect(collectOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchgate: collection failed: %v\n", err)
		return 2
	}

	// Parse and compare.
	baseParsed, perr := benchgate.ParseBenchOutput(strings.NewReader(collectResult.BaseOutput), "base.txt")
	if perr != nil {
		fmt.Fprintf(os.Stderr, "benchgate: parse base: %v\n", perr)
		return 2
	}
	candParsed, perr := benchgate.ParseBenchOutput(strings.NewReader(collectResult.CandidateOutput), "candidate.txt")
	if perr != nil {
		fmt.Fprintf(os.Stderr, "benchgate: parse candidate: %v\n", perr)
		return 2
	}

	report, err := benchgate.Compare(baseParsed, candParsed, policy)
	if err != nil && report != nil {
		// Operational error but we still have a report to write.
		report.Identity = f.toIdentity()
		report.BatchOrder = collectResult.BatchOrder
		writeReportFiles(f, report)
		fmt.Fprintf(os.Stderr, "benchgate: %v\n", err)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchgate: %v\n", err)
		return 2
	}

	report.Identity = f.toIdentity()
	report.BatchOrder = collectResult.BatchOrder

	// Apply waiver if enabled.
	if f.waiverEnabled {
		benchgate.ApplyWaiver(report, f.toWaiver())
	}

	writeReportFiles(f, report)
	benchgate.WriteText(os.Stdout, report)
	return exitCodeForVerdict(report.Verdict)
}

// --- benchgate compare ---

func runCompareCmd(args []string) int {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	f := &compareFlags{}
	registerCompareFlags(fs, f)
	fs.Parse(args)

	if f.baseFile == "" || f.candidateFile == "" {
		fmt.Fprintln(os.Stderr, "benchgate compare: -base and -candidate are required")
		return 2
	}

	baseData, err := os.ReadFile(f.baseFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchgate: read base %q: %v\n", f.baseFile, err)
		return 2
	}
	candData, err := os.ReadFile(f.candidateFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchgate: read candidate %q: %v\n", f.candidateFile, err)
		return 2
	}

	policy := f.toPolicy()

	baseParsed, perr := benchgate.ParseBenchOutput(strings.NewReader(string(baseData)), f.baseFile)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "benchgate: parse base: %v\n", perr)
		return 2
	}
	candParsed, perr := benchgate.ParseBenchOutput(strings.NewReader(string(candData)), f.candidateFile)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "benchgate: parse candidate: %v\n", perr)
		return 2
	}

	report, err := benchgate.Compare(baseParsed, candParsed, policy)
	if err != nil && report != nil {
		report.Identity = f.toIdentity()
		writeReportFiles(f, report)
		fmt.Fprintf(os.Stderr, "benchgate: %v\n", err)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchgate: %v\n", err)
		return 2
	}

	report.Identity = f.toIdentity()

	if f.waiverEnabled {
		benchgate.ApplyWaiver(report, f.toWaiver())
	}

	writeReportFiles(f, report)
	benchgate.WriteText(os.Stdout, report)
	return exitCodeForVerdict(report.Verdict)
}

func writeReportFiles(f *compareFlags, report *benchgate.ComparisonReport) {
	if f.jsonOut != "" {
		file, err := os.Create(f.jsonOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchgate: create %q: %v\n", f.jsonOut, err)
			return
		}
		defer file.Close()
		if err := benchgate.WriteJSON(file, report); err != nil {
			fmt.Fprintf(os.Stderr, "benchgate: write json: %v\n", err)
		}
	}
	if f.markdownOut != "" {
		file, err := os.Create(f.markdownOut)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchgate: create %q: %v\n", f.markdownOut, err)
			return
		}
		defer file.Close()
		if err := benchgate.WriteMarkdown(file, report, f.artifactURL); err != nil {
			fmt.Fprintf(os.Stderr, "benchgate: write markdown: %v\n", err)
		}
	}
}

// --- legacy mode (CV-only, backward compatible) ---

func runLegacy(args []string) int {
	pkg := flag.String("pkg", "./...", "package pattern to benchmark")
	bench := flag.String("bench", ".", "benchmark regexp")
	count := flag.Int("count", 10, "number of benchmark runs")
	benchtime := flag.String("benchtime", "1s", "go test -benchtime value")
	cvThreshold := flag.Float64("cv-threshold", 5.0, "max acceptable CV percent")
	jsonOut := flag.Bool("json", false, "emit JSON output")
	baseline := flag.String("baseline", "", "path to saved go test -bench output for comparison")
	save := flag.String("save", "", "path to write raw benchmark output")
	fs := flag.NewFlagSet("benchgate", flag.ExitOnError)
	fs.Usage = func() { printUsage(os.Stderr) }
	fs.Parse(args)

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

	out, err := runGoTest(goArgs, workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchgate: go test failed: %v\n%s\n", err, out)
		return 2
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

	results, failing := computeCVResults(samples, *cvThreshold)
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
		printLegacyText(results, *cvThreshold, overallPass, failing)
	}

	if *baseline != "" {
		// Use the new comparison engine for baseline comparison.
		exitCode := compareWithBaseline(rawOutput, *baseline, workDir, *pkg, *bench, *count, *benchtime)
		if exitCode != 0 && overallPass {
			return exitCode
		}
	}

	if !overallPass {
		return 1
	}
	return 0
}

func compareWithBaseline(newOutput, baselinePath, workDir, pkg, bench string, count int, benchtime string) int {
	baseData, err := os.ReadFile(baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchgate: read baseline %q: %v\n", baselinePath, err)
		return 2
	}
	baseParsed, perr := benchgate.ParseBenchOutput(strings.NewReader(string(baseData)), baselinePath)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "benchgate: parse baseline: %v\n", perr)
		return 2
	}
	candParsed, perr := benchgate.ParseBenchOutput(strings.NewReader(newOutput), "current")
	if perr != nil {
		fmt.Fprintf(os.Stderr, "benchgate: parse current: %v\n", perr)
		return 2
	}
	report, err := benchgate.Compare(baseParsed, candParsed, benchgate.DefaultPolicy())
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchgate: compare: %v\n", err)
		return 2
	}
	fmt.Println("\n--- benchgate comparison ---")
	benchgate.WriteText(os.Stdout, report)
	return exitCodeForVerdict(report.Verdict)
}
