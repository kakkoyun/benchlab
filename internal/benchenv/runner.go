package benchenv

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// execRunner executes a command with a context and returns combined
// stdout+stderr output. On success the output is typically just stdout.
// LookPath reports whether a binary is on PATH.
type execRunner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
	LookPath(file string) error
}

// fsReader reads file contents and tests for file existence without
// touching the network or running commands.
type fsReader interface {
	ReadFile(path string) (string, error)
	Exists(path string) bool
}

// realExec is the production execRunner backed by os/exec.
type realExec struct{}

func (realExec) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // diagnostic tooling
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func (realExec) LookPath(file string) error {
	_, err := exec.LookPath(file)
	return err
}

// realFS is the production fsReader backed by os.
type realFS struct{}

func (realFS) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (realFS) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// prober holds the dependencies and host facts used by all diagnostic
// collectors. Tests inject fake execRunner/fsReader implementations.
type prober struct {
	exec   execRunner
	fs     fsReader
	os     string // runtime.GOOS
	arch   string // normalized runtime.GOARCH
	numCPU int
}

// Option configures a prober for testing.
type Option func(*prober)

func withExec(r execRunner) Option { return func(p *prober) { p.exec = r } }
func withFS(r fsReader) Option     { return func(p *prober) { p.fs = r } }
func withOS(os string) Option      { return func(p *prober) { p.os = os } }
func withArch(arch string) Option  { return func(p *prober) { p.arch = normalizeArch(arch) } }
func withNumCPU(n int) Option      { return func(p *prober) { p.numCPU = n } }

func newProber(opts ...Option) *prober {
	p := &prober{
		exec:   realExec{},
		fs:     realFS{},
		os:     runtime.GOOS,
		arch:   goArch(),
		numCPU: runtime.NumCPU(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// runTimeout runs a command with a bounded timeout and returns its output.
func (p *prober) runTimeout(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return p.exec.Run(ctx, name, args...)
}

// run executes a command with a generous default timeout.
func (p *prober) run(name string, args ...string) (string, error) {
	return p.runTimeout(10*time.Second, name, args...)
}
