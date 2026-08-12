package benchgate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CollectOptions configures benchmark collection.
type CollectOptions struct {
	Pkg       string // package pattern (default ./...)
	Bench     string // benchmark regexp (default .)
	Count     int    // total samples per side
	Benchtime string // -benchtime value (default 1s)
	CPU       string // optional -cpu value (e.g. "1,2,4")
	Setup     string // optional setup command run once per worktree
	BaseDir   string // base worktree directory
	CandDir   string // candidate worktree directory (default cwd)
	WorkDir   string // directory for generated files (default RUNNER_TEMP or cwd)
	GoVersion string // optional Go version to record in output
}

// CollectResult holds the raw output from base and candidate collection.
type CollectResult struct {
	BaseOutput      string
	CandidateOutput string
	BaseFile        string
	CandidateFile   string
	BatchOrder      []string
}

// Collect runs counterbalanced benchmark collection across base and
// candidate worktrees. The batch order is:
//
//	base half, candidate half, candidate remainder, base remainder
//
// This counterbalances temporal drift. Each batch uses:
//
//	go test -run '^$' -bench=<pattern> -benchmem -count=<batch> -benchtime=<time> -p=1 <packages>
func Collect(opts CollectOptions) (*CollectResult, error) {
	if opts.Count < 2 {
		return nil, fmt.Errorf("count must be at least 2, got %d", opts.Count)
	}
	if opts.Benchtime == "" {
		opts.Benchtime = "1s"
	}
	if opts.Bench == "" {
		opts.Bench = "."
	}
	if opts.Pkg == "" {
		opts.Pkg = "./..."
	}
	if opts.CandDir == "" {
		if dir, err := os.Getwd(); err == nil {
			opts.CandDir = dir
		} else {
			opts.CandDir = "."
		}
	}
	if opts.WorkDir == "" {
		opts.WorkDir = runnerTemp()
	}

	// Split count into halves for counterbalancing.
	baseHalf := opts.Count / 2
	baseRem := opts.Count - baseHalf
	candHalf := opts.Count / 2
	candRem := opts.Count - candHalf

	// Run setup once per worktree.
	if opts.Setup != "" {
		if err := runSetup(opts.Setup, opts.BaseDir); err != nil {
			return nil, fmt.Errorf("setup (base): %w", err)
		}
		if err := runSetup(opts.Setup, opts.CandDir); err != nil {
			return nil, fmt.Errorf("setup (candidate): %w", err)
		}
	}

	var baseOutput, candOutput strings.Builder
	batchOrder := []string{
		fmt.Sprintf("base/%d", baseHalf),
		fmt.Sprintf("candidate/%d", candHalf),
		fmt.Sprintf("candidate/%d", candRem),
		fmt.Sprintf("base/%d", baseRem),
	}

	// Batch 1: base half
	if err := runBatch(&baseOutput, opts, opts.BaseDir, baseHalf); err != nil {
		return nil, fmt.Errorf("batch base/%d: %w", baseHalf, err)
	}
	// Batch 2: candidate half
	if err := runBatch(&candOutput, opts, opts.CandDir, candHalf); err != nil {
		return nil, fmt.Errorf("batch candidate/%d: %w", candHalf, err)
	}
	// Batch 3: candidate remainder
	if err := runBatch(&candOutput, opts, opts.CandDir, candRem); err != nil {
		return nil, fmt.Errorf("batch candidate/%d: %w", candRem, err)
	}
	// Batch 4: base remainder
	if err := runBatch(&baseOutput, opts, opts.BaseDir, baseRem); err != nil {
		return nil, fmt.Errorf("batch base/%d: %w", baseRem, err)
	}

	// Write output files.
	baseFile := filepath.Join(opts.WorkDir, "base.txt")
	candFile := filepath.Join(opts.WorkDir, "candidate.txt")
	if err := os.WriteFile(baseFile, []byte(baseOutput.String()), 0o644); err != nil {
		return nil, fmt.Errorf("write base.txt: %w", err)
	}
	if err := os.WriteFile(candFile, []byte(candOutput.String()), 0o644); err != nil {
		return nil, fmt.Errorf("write candidate.txt: %w", err)
	}

	return &CollectResult{
		BaseOutput:      baseOutput.String(),
		CandidateOutput: candOutput.String(),
		BaseFile:        baseFile,
		CandidateFile:   candFile,
		BatchOrder:      batchOrder,
	}, nil
}

// runBatch executes one batch of go test -bench in the given directory.
func runBatch(out *strings.Builder, opts CollectOptions, dir string, count int) error {
	if count <= 0 {
		return nil
	}
	args := []string{
		"test",
		"-run", "^$",
		fmt.Sprintf("-bench=%s", opts.Bench),
		"-benchmem",
		fmt.Sprintf("-count=%d", count),
		fmt.Sprintf("-benchtime=%s", opts.Benchtime),
		"-p=1",
	}
	if opts.CPU != "" {
		args = append(args, fmt.Sprintf("-cpu=%s", opts.CPU))
	}
	args = append(args, opts.Pkg)

	cmd := exec.Command("go", args...) //nolint:gosec
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stderr = os.Stderr
	stdout, err := cmd.Output()
	if err != nil {
		// Exit code 1 from go test may mean a test failure, but benchmark
		// output may still be present.
		if exitErr, ok := err.(*exec.ExitError); ok {
			out.Write(exitErr.Stderr)
			out.Write(stdout)
			return fmt.Errorf("go test exit %d", exitErr.ExitCode())
		}
		return fmt.Errorf("go test: %w", err)
	}
	out.Write(stdout)
	return nil
}

// runSetup executes the setup command once in the given directory.
func runSetup(setup, dir string) error {
	cmd := exec.Command("sh", "-c", setup) //nolint:gosec
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runnerTemp returns $RUNNER_TEMP or the current directory.
func runnerTemp() string {
	if t := os.Getenv("RUNNER_TEMP"); t != "" {
		return t
	}
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}
