package benchenv

import (
	"context"
	"fmt"
)

// fakeExec is a test execRunner backed by function closures.
type fakeExec struct {
	runFn  func(name string, args ...string) (string, error)
	lookFn func(file string) error
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) (string, error) {
	return f.runFn(name, args...)
}

func (f *fakeExec) LookPath(file string) error {
	if f.lookFn != nil {
		return f.lookFn(file)
	}
	return fmt.Errorf("not found: %s", file)
}

// fakeFS is a test fsReader with canned file contents.
type fakeFS struct {
	files map[string]string
}

func (f *fakeFS) ReadFile(path string) (string, error) {
	if content, ok := f.files[path]; ok {
		return content, nil
	}
	return "", fmt.Errorf("file not found: %s", path)
}

func (f *fakeFS) Exists(path string) bool {
	_, ok := f.files[path]
	return ok
}

// hasPrefix checks whether args starts with the given prefix.
func hasPrefix(args []string, prefix ...string) bool {
	if len(args) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if args[i] != p {
			return false
		}
	}
	return true
}

// constLookFn returns a lookFn that reports the given binaries as present.
func constLookFn(present ...string) func(string) error {
	m := make(map[string]bool)
	for _, p := range present {
		m[p] = true
	}
	return func(file string) error {
		if m[file] {
			return nil
		}
		return fmt.Errorf("not found: %s", file)
	}
}
