package main

import (
	"encoding/json"
	"errors"
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
	Posn     string `json:"posn"`
}

func TestCLI(t *testing.T) {
	binary := buildHonestbench(t)

	t.Run("clean package pattern", func(t *testing.T) {
		module := writeModuleFiles(t, map[string]string{
			"sample_test.go": `package sample

import "testing"

func BenchmarkClean(b *testing.B) {
	for b.Loop() {}
}
`,
			"nested/nested_test.go": `package nested

import "testing"

func BenchmarkNestedClean(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {}
	})
}
`,
		})
		output, err := runCommand(module, binary, "./...")
		if err != nil {
			t.Fatalf("honestbench ./...: %v\n%s", err, output)
		}
		if len(output) != 0 {
			t.Fatalf("clean analyzer emitted output:\n%s", output)
		}
	})

	t.Run("diagnostic exits nonzero", func(t *testing.T) {
		module := writeModule(t, `package sample

import "testing"

func BenchmarkMissing(b *testing.B) {}
`)
		output, err := runCommand(module, binary, "./...")
		if err == nil {
			t.Fatalf("diagnostic analyzer unexpectedly succeeded:\n%s", output)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
			t.Fatalf("expected a nonzero process exit, got %v", err)
		}
		if !strings.Contains(string(output), "benchmark scope has no B.Loop") {
			t.Fatalf("text output does not contain missing-loop diagnostic:\n%s", output)
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
		if output, err := runCommand(module, binary, "./..."); err != nil || len(output) != 0 {
			t.Fatalf("default analyzer should be clean: %v\n%s", err, output)
		}

		output, err := runCommand(module, binary, "-advisory", "./...")
		if err == nil {
			t.Fatalf("advisory analyzer unexpectedly succeeded:\n%s", output)
		}
		if !strings.Contains(string(output), "canonical b.N loop can use") {
			t.Fatalf("advisory output does not contain the migration diagnostic:\n%s", output)
		}
	})

	t.Run("vettool clean and diagnostic", func(t *testing.T) {
		clean := writeModule(t, `package sample

import "testing"

func BenchmarkClean(b *testing.B) { for b.Loop() {} }
`)
		if output, err := runCommand(clean, "go", "vet", "-vettool="+binary, "./..."); err != nil {
			t.Fatalf("clean go vet -vettool: %v\n%s", err, output)
		}

		bad := writeModule(t, `package sample

import "testing"

func BenchmarkMissing(b *testing.B) {}
`)
		output, err := runCommand(bad, "go", "vet", "-vettool="+binary, "./...")
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("vettool failed without a process exit: %v\n%s", err, output)
			}
		}
		if !strings.Contains(string(output), "benchmark scope has no B.Loop") {
			t.Fatalf("vettool output does not contain missing-loop diagnostic:\n%s", output)
		}
	})

	t.Run("standard json", func(t *testing.T) {
		module := writeModule(t, `package sample

import "testing"

func BenchmarkMissing(b *testing.B) {}
`)
		output, err := runCommand(module, binary, "-json", "./...")
		if err != nil {
			t.Fatalf("standard JSON driver failed: %v\n%s", err, output)
		}
		var packages map[string]map[string][]jsonDiagnostic
		if err := json.Unmarshal(output, &packages); err != nil {
			t.Fatalf("decode standard analysis JSON: %v\n%s", err, output)
		}
		var found bool
		for packageKey, analyzers := range packages {
			if !strings.Contains(packageKey, "example.com/sample") {
				continue
			}
			for _, diagnostic := range analyzers["honestbench"] {
				if diagnostic.Category == "missing-loop" && diagnostic.Message != "" && strings.Contains(diagnostic.Posn, "sample_test.go") {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("standard analysis JSON did not contain a positioned missing-loop category:\n%s", output)
		}
	})

	t.Run("internal and external test packages", func(t *testing.T) {
		module := writeModuleFiles(t, map[string]string{
			"sample.go": "package sample\n",
			"internal_test.go": `package sample

import "testing"
func BenchmarkInternalMissing(b *testing.B) {}
`,
			"external_test.go": `package sample_test

import "testing"
func BenchmarkExternalMissing(b *testing.B) {}
`,
		})
		output, err := runCommand(module, binary, "-json", "./...")
		if err != nil {
			t.Fatalf("standard JSON driver failed: %v\n%s", err, output)
		}
		var packages map[string]map[string][]jsonDiagnostic
		if err := json.Unmarshal(output, &packages); err != nil {
			t.Fatalf("decode standard analysis JSON: %v\n%s", err, output)
		}
		files := map[string]bool{}
		for _, analyzers := range packages {
			for _, diagnostic := range analyzers["honestbench"] {
				if diagnostic.Category == "missing-loop" {
					files[filepath.Base(strings.Split(diagnostic.Posn, ":")[0])] = true
				}
			}
		}
		if !files["internal_test.go"] || !files["external_test.go"] {
			t.Fatalf("expected diagnostics from internal and external test packages, got %#v\n%s", files, output)
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
	if output, err := runCommand(root, "go", "build", "-o", binary, "./cmd/honestbench"); err != nil {
		t.Fatalf("build honestbench: %v\n%s", err, output)
	}
	return binary
}

func writeModule(t *testing.T, source string) string {
	t.Helper()
	return writeModuleFiles(t, map[string]string{"sample_test.go": source})
}

func writeModuleFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module example.com/sample\n\ngo 1.24\n"
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runCommand(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
