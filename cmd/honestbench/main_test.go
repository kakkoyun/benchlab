package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type jsonDiagnostic struct {
	Category string `json:"category"`
	Message  string `json:"message"`
}

func TestCLI(t *testing.T) {
	binary := buildHonestbench(t)

	t.Run("clean package pattern", func(t *testing.T) {
		module := writeModule(t, `package sample

import "testing"

func BenchmarkClean(b *testing.B) {
	for b.Loop() {}
}
`)
		cmd := exec.Command(binary, "./...")
		cmd.Dir = module
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("honestbench ./...: %v\n%s", err, output)
		}
	})

	t.Run("default and advisory toggle", func(t *testing.T) {
		module := writeModule(t, `package sample

import "testing"

func consume(int) {}
func BenchmarkLegacy(b *testing.B) {
	for range b.N { consume(1) }
}
`)
		defaultCmd := exec.Command(binary, "./...")
		defaultCmd.Dir = module
		if output, err := defaultCmd.CombinedOutput(); err != nil {
			t.Fatalf("default analyzer unexpectedly failed: %v\n%s", err, output)
		}

		advisoryCmd := exec.Command(binary, "-advisory", "./...")
		advisoryCmd.Dir = module
		output, err := advisoryCmd.CombinedOutput()
		if err == nil {
			t.Fatalf("advisory analyzer unexpectedly succeeded:\n%s", output)
		}
		if !strings.Contains(string(output), "canonical b.N loop can use") {
			t.Fatalf("advisory output does not contain the migration diagnostic:\n%s", output)
		}
	})

	t.Run("vettool", func(t *testing.T) {
		module := writeModule(t, `package sample

import "testing"

func BenchmarkClean(b *testing.B) {
	for b.Loop() {}
}
`)
		cmd := exec.Command("go", "vet", "-vettool="+binary, "./...")
		cmd.Dir = module
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go vet -vettool: %v\n%s", err, output)
		}
	})

	t.Run("standard json", func(t *testing.T) {
		module := writeModule(t, `package sample

import "testing"

func BenchmarkMissing(b *testing.B) {}
`)
		cmd := exec.Command(binary, "-json", "./...")
		cmd.Dir = module
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = cmd.Stdout
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		output, readErr := io.ReadAll(stdout)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("standard JSON driver failed: %v\n%s", err, output)
		}
		var packages map[string]map[string][]jsonDiagnostic
		if err := json.Unmarshal(output, &packages); err != nil {
			t.Fatalf("decode standard analysis JSON: %v\n%s", err, output)
		}
		var found bool
		for _, analyzers := range packages {
			for _, diagnostic := range analyzers["honestbench"] {
				if diagnostic.Category == "missing-loop" {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("standard analysis JSON did not contain missing-loop category:\n%s", output)
		}
	})
}

func buildHonestbench(t *testing.T) string {
	t.Helper()
	root := repositoryRoot(t)
	name := "honestbench"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/honestbench")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build honestbench: %v\n%s", err, output)
	}
	return binary
}

func writeModule(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/sample\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample_test.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
