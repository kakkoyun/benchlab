package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectFilesSingleFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "bench_test.go")
	if err := os.WriteFile(file, []byte("package bench\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := collectFiles(file, false)
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}
	if len(files) != 1 || files[0] != file {
		t.Fatalf("collectFiles returned %v; want [%s]", files, file)
	}
}

func TestCollectFilesRecursivePattern(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"root_test.go", filepath.Join("nested", "nested_test.go")} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package bench\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := collectFiles(filepath.Join(root, "..."), false)
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("collectFiles returned %d files; want 2: %v", len(files), files)
	}
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			t.Errorf("collectFiles returned non-test file: %s", file)
		}
	}
}

func TestCollectFilesDoesNotSkipDotRoot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "root_test.go")
	if err := os.WriteFile(file, []byte("package bench\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	files, err := collectFiles(".", true)
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}
	if len(files) != 1 || files[0] != "root_test.go" {
		t.Fatalf("collectFiles returned %v; want [root_test.go]", files)
	}
}
