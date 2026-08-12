//go:build darwin

package benchenv

import (
	"errors"
	"testing"
)

// TestDiagnoseFullPipelineDarwin verifies the full Diagnose pipeline on
// macOS produces a complete report with all fields populated.
func TestDiagnoseFullPipelineDarwin(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			// macOS platform detection
			case name == "uname" && hasPrefix(args, "-m"):
				return "arm64\n", nil
			case name == "sysctl" && hasPrefix(args, "-n", "sysctl.proc_translated"):
				return "0\n", nil
			case name == "sysctl" && hasPrefix(args, "-n", "machdep.cpu.brand_string"):
				return "Apple M2\n", nil
			case name == "sysctl" && hasPrefix(args, "-n", "vm.loadavg"):
				return "{ 0.10 0.20 0.30 }\n", nil
			case name == "pmset" && hasPrefix(args, "-g", "ps"):
				return "Now drawing from 'AC Power'\n", nil
			case name == "pmset" && hasPrefix(args, "-g", "therm"):
				return "\n", nil
			case name == "pmset" && hasPrefix(args, "-g"):
				return "  lowpowermode     0\n", nil
			// Docker detection
			case name == "docker" && hasPrefix(args, "info", "--format"):
				return "linux\naarch64\nUbuntu 22.04\n4\n8589934592", nil
			case name == "docker" && hasPrefix(args, "context", "show"):
				return "colima\n", nil
			case name == "docker" && hasPrefix(args, "context", "inspect"):
				return `[{"Name":"colima","Endpoints":{"docker":{"Host":"unix:///Users/foo/.colima/docker.sock"}}}]`, nil
			case name == "colima" && hasPrefix(args, "status", "--json"):
				return `{"driver":"macOS Virtualization.Framework","arch":"aarch64","cpu":4,"memory":8589934592,"runtime":"docker","status":"Running"}`, nil
			case name == "docker" && hasPrefix(args, "run"):
				constrained := false
				for _, a := range args {
					if a == "--cpuset-cpus=0" {
						constrained = true
					}
				}
				if !constrained {
					return "0-3\n", nil
				}
				return "v2\n0\n100000 100000\n134217728\n0\n", nil
			default:
				return "", errors.New("unexpected: " + name)
			}
		},
		lookFn: constLookFn("docker", "colima"),
	}

	report := Diagnose(
		withExec(exec), withFS(&fakeFS{files: map[string]string{}}),
		withOS("darwin"), withArch("arm64"), withNumCPU(10),
	)

	// Legacy fields.
	if report.OS != "darwin" {
		t.Errorf("OS = %q, want darwin", report.OS)
	}
	if report.Arch != "arm64" {
		t.Errorf("Arch = %q, want arm64", report.Arch)
	}
	if report.NumCPU != 10 {
		t.Errorf("NumCPU = %d, want 10", report.NumCPU)
	}
	if len(report.Checks) == 0 {
		t.Error("expected checks")
	}
	if report.Summary.OK+report.Summary.Warn+report.Summary.Unavailable == 0 {
		t.Error("expected non-zero summary")
	}

	// Platform.
	if report.Platform.CPUModel != "Apple M2" {
		t.Errorf("CPUModel = %q, want Apple M2", report.Platform.CPUModel)
	}
	if report.Platform.Translation != "none" {
		t.Errorf("Translation = %q, want none", report.Platform.Translation)
	}

	// Docker.
	if !report.Docker.Available {
		t.Error("expected Docker available")
	}
	if report.Docker.Backend != "colima-vz" {
		t.Errorf("Backend = %q, want colima-vz", report.Docker.Backend)
	}
	if report.Docker.Isolation == nil || !report.Docker.Isolation.Passed {
		t.Error("expected passing isolation probe")
	}

	// Readiness.
	if report.Readiness.Overall != GradeLimited {
		t.Errorf("Overall = %q, want limited (macOS)", report.Readiness.Overall)
	}
	if report.Readiness.RecommendedPath != "native" {
		t.Errorf("RecommendedPath = %q, want native", report.Readiness.RecommendedPath)
	}

	// Actions.
	if len(report.Actions) == 0 {
		t.Error("expected actions")
	}

	// Recipes.
	if report.Recipes.Native == "" {
		t.Error("expected native recipe")
	}
	if report.Recipes.Docker == "" {
		t.Error("expected docker recipe")
	}
}

// TestDiagnoseDockerUnavailableDarwin verifies Docker absence doesn't break
// Diagnose on macOS.
func TestDiagnoseDockerUnavailableDarwin(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "uname" && hasPrefix(args, "-m"):
				return "arm64\n", nil
			case name == "sysctl" && hasPrefix(args, "-n", "sysctl.proc_translated"):
				return "0\n", nil
			case name == "sysctl" && hasPrefix(args, "-n", "machdep.cpu.brand_string"):
				return "Apple M2\n", nil
			case name == "sysctl" && hasPrefix(args, "-n", "vm.loadavg"):
				return "{ 0.10 0.20 0.30 }\n", nil
			case name == "pmset" && hasPrefix(args, "-g", "ps"):
				return "Now drawing from 'AC Power'\n", nil
			case name == "pmset" && hasPrefix(args, "-g", "therm"):
				return "\n", nil
			case name == "pmset" && hasPrefix(args, "-g"):
				return "  lowpowermode     0\n", nil
			default:
				return "", errors.New("not found")
			}
		},
		lookFn: func(string) error { return errors.New("not found") },
	}

	report := Diagnose(
		withExec(exec), withFS(&fakeFS{files: map[string]string{}}),
		withOS("darwin"), withArch("arm64"), withNumCPU(10),
	)

	if report.Docker.Available {
		t.Error("expected Docker unavailable")
	}
	if report.Readiness.Docker != GradeUnavailable {
		t.Errorf("Docker grade = %q, want unavailable", report.Readiness.Docker)
	}
	// Docker recipe should be empty when unavailable.
	if report.Recipes.Docker != "" {
		t.Error("expected empty Docker recipe when unavailable")
	}
}
