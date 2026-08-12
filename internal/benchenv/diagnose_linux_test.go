//go:build linux

package benchenv

import (
	"errors"
	"testing"
)

// TestDiagnoseFullPipelineLinux verifies the full Diagnose pipeline on
// Linux produces a complete report with all fields populated.
func TestDiagnoseFullPipelineLinux(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/proc/cpuinfo":                       "processor\t: 0\nmodel name\t: AMD EPYC 7742 64-Core Processor\n",
		"/proc/loadavg":                       "0.10 0.20 0.30 1/100 12345\n",
		"/sys/devices/system/cpu/smt/control": "off\n",
		"/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor": "performance\n",
		"/sys/devices/system/cpu/intel_pstate/no_turbo":         "1\n",
	}}
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "uname" && hasPrefix(args, "-m"):
				return "x86_64\n", nil
			case name == "systemd-detect-virt":
				return "none\n", nil
			// Docker detection
			case name == "docker" && hasPrefix(args, "info", "--format"):
				return "linux\namd64\nUbuntu 22.04.3 LTS\n8\n17179869184", nil
			case name == "docker" && hasPrefix(args, "context", "show"):
				return "default\n", nil
			case name == "docker" && hasPrefix(args, "context", "inspect"):
				return `[{"Name":"default","Endpoints":{"docker":{"Host":"unix:///var/run/docker.sock"}}}]`, nil
			case name == "docker" && hasPrefix(args, "run"):
				constrained := false
				for _, a := range args {
					if a == "--cpuset-cpus=0" {
						constrained = true
					}
				}
				if !constrained {
					return "0-7\n", nil
				}
				return "v2\n0\n100000 100000\n134217728\n0\n", nil
			default:
				return "", errors.New("unexpected: " + name)
			}
		},
		lookFn: constLookFn("docker", "systemd-detect-virt"),
	}

	report := Diagnose(
		withExec(exec), withFS(fs),
		withOS("linux"), withArch("amd64"), withNumCPU(8),
	)

	// Legacy fields.
	if report.OS != "linux" {
		t.Errorf("OS = %q, want linux", report.OS)
	}
	if report.Arch != "amd64" {
		t.Errorf("Arch = %q, want amd64", report.Arch)
	}
	if report.NumCPU != 8 {
		t.Errorf("NumCPU = %d, want 8", report.NumCPU)
	}
	if len(report.Checks) == 0 {
		t.Error("expected checks")
	}

	// Platform.
	if report.Platform.CPUModel != "AMD EPYC 7742 64-Core Processor" {
		t.Errorf("CPUModel = %q, want AMD EPYC 7742", report.Platform.CPUModel)
	}
	if report.Platform.Virtualization != "none" {
		t.Errorf("Virtualization = %q, want none", report.Platform.Virtualization)
	}
	if report.Platform.Translation != "none" {
		t.Errorf("Translation = %q, want none", report.Platform.Translation)
	}

	// Docker.
	if !report.Docker.Available {
		t.Error("expected Docker available")
	}
	if report.Docker.Backend != "engine" {
		t.Errorf("Backend = %q, want engine", report.Docker.Backend)
	}
	if report.Docker.Isolation == nil || !report.Docker.Isolation.Passed {
		t.Error("expected passing isolation probe")
	}

	// Readiness: bare-metal Linux, no host noise, passing probe → ready.
	if report.Readiness.Overall != GradeReady {
		t.Errorf("Overall = %q, want ready (bare-metal Linux)", report.Readiness.Overall)
	}
	if report.Readiness.Native != GradeReady {
		t.Errorf("Native = %q, want ready", report.Readiness.Native)
	}
	if report.Readiness.Docker != GradeReady {
		t.Errorf("Docker = %q, want ready", report.Readiness.Docker)
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

// TestDiagnoseDockerUnavailableLinux verifies Docker absence doesn't break
// Diagnose on Linux.
func TestDiagnoseDockerUnavailableLinux(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/proc/cpuinfo":                       "processor\t: 0\nmodel name\t: AMD EPYC 7742 64-Core Processor\n",
		"/proc/loadavg":                       "0.10 0.20 0.30 1/100 12345\n",
		"/sys/devices/system/cpu/smt/control": "off\n",
		"/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor": "performance\n",
		"/sys/devices/system/cpu/intel_pstate/no_turbo":         "1\n",
	}}
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "uname" && hasPrefix(args, "-m"):
				return "x86_64\n", nil
			case name == "systemd-detect-virt":
				return "none\n", nil
			default:
				return "", errors.New("not found")
			}
		},
		lookFn: func(string) error { return errors.New("not found") },
	}

	report := Diagnose(
		withExec(exec), withFS(fs),
		withOS("linux"), withArch("amd64"), withNumCPU(8),
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
	// Native path should still be ready on bare-metal Linux.
	if report.Readiness.Native != GradeReady {
		t.Errorf("Native = %q, want ready", report.Readiness.Native)
	}
}
