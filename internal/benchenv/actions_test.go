package benchenv

import (
	"strings"
	"testing"
)

func TestBuildActionsMacOS(t *testing.T) {
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := Docker{Available: true, Local: true, Backend: "colima-vz", EngineArch: "arm64",
		Isolation: &IsolationProbe{Ran: true, Passed: true}}
	rd := Readiness{Overall: GradeLimited, RecommendedPath: "native", Native: GradeLimited, Docker: GradeLimited}
	actions := buildActions(plat, dkr, rd, 10)

	if len(actions) == 0 {
		t.Fatal("expected actions")
	}

	// First action should be about macOS certification (priority 2).
	foundMacOSCert := false
	for _, a := range actions {
		if a.Scope == "platform" && strings.Contains(a.Reason, "macOS cannot control") {
			foundMacOSCert = true
		}
	}
	if !foundMacOSCert {
		t.Error("expected macOS certification action")
	}

	// Should include tool installation actions.
	foundTools := false
	for _, a := range actions {
		if a.Scope == "tools" {
			foundTools = true
		}
	}
	if !foundTools {
		t.Error("expected tool installation actions")
	}

	// Actions should be sorted by priority.
	for i := 1; i < len(actions); i++ {
		if actions[i-1].Priority > actions[i].Priority {
			t.Errorf("action %d priority %d > action %d priority %d (not sorted)",
				i-1, actions[i-1].Priority, i, actions[i].Priority)
		}
	}
}

func TestBuildActionsRosetta(t *testing.T) {
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "rosetta"}
	dkr := Docker{}
	rd := Readiness{Overall: GradeNotReady}
	actions := buildActions(plat, dkr, rd, 10)

	foundRosetta := false
	for _, a := range actions {
		if strings.Contains(a.Reason, "Rosetta") {
			foundRosetta = true
			if a.Priority != 1 {
				t.Errorf("Rosetta action priority = %d, want 1", a.Priority)
			}
		}
	}
	if !foundRosetta {
		t.Error("expected Rosetta translation action")
	}
}

func TestBuildActionsDockerQEMU(t *testing.T) {
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := Docker{Available: true, Local: true, Backend: "colima-qemu", EngineArch: "amd64",
		Translation: "qemu"}
	rd := Readiness{Overall: GradeNotReady}
	actions := buildActions(plat, dkr, rd, 10)

	foundQEMU := false
	for _, a := range actions {
		if a.Scope == "docker" && strings.Contains(a.Reason, "QEMU emulation") {
			foundQEMU = true
			if a.Priority != 1 {
				t.Errorf("QEMU action priority = %d, want 1", a.Priority)
			}
		}
	}
	if !foundQEMU {
		t.Error("expected QEMU emulation action")
	}

	// Should include the Colima bench profile command.
	foundColimaCmd := false
	for _, a := range actions {
		for _, cmd := range a.Commands {
			if strings.Contains(cmd, "colima start --profile benchlab") {
				foundColimaCmd = true
			}
		}
	}
	if !foundColimaCmd {
		t.Error("expected Colima bench profile command")
	}
}

func TestBuildActionsDeduplication(t *testing.T) {
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := Docker{Available: true, Local: true, Backend: "colima-vz", EngineArch: "arm64",
		Isolation: &IsolationProbe{Ran: true, Passed: true}}
	rd := Readiness{Overall: GradeLimited}
	actions := buildActions(plat, dkr, rd, 10)

	seen := make(map[string]bool)
	for _, a := range actions {
		if seen[a.Reason] {
			t.Errorf("duplicate action reason: %s", a.Reason)
		}
		seen[a.Reason] = true
	}
}

func TestBuildRecipesMacOS(t *testing.T) {
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := Docker{Available: true, Local: true, Backend: "colima-vz", EngineArch: "arm64",
		Isolation: &IsolationProbe{Ran: true, Passed: true, SelectedCPU: "0"}}
	rd := Readiness{Native: GradeLimited, Docker: GradeLimited}
	recipes := buildRecipes(plat, dkr, rd, 10)

	if !strings.Contains(recipes.Native, "count=20") {
		t.Errorf("macOS native recipe should use count=20, got: %s", recipes.Native)
	}
	if !strings.Contains(recipes.Docker, "--platform=linux/arm64") {
		t.Error("Docker recipe should specify native platform")
	}
	if !strings.Contains(recipes.Docker, "--cpuset-cpus=0") {
		t.Error("Docker recipe should use verified CPU from probe")
	}
	if !strings.Contains(recipes.Docker, "--memory=512m") {
		t.Error("Docker recipe should use 512m memory")
	}
	if !strings.Contains(recipes.Docker, "--memory-swap=512m") {
		t.Error("Docker recipe should disable swap")
	}
}

func TestBuildRecipesLinux(t *testing.T) {
	plat := Platform{OS: "linux", Arch: "amd64", Virtualization: "none", Translation: "none"}
	dkr := Docker{Available: true, Local: true, Backend: "engine", EngineArch: "amd64",
		Isolation: &IsolationProbe{Ran: true, Passed: true, SelectedCPU: "2"}}
	rd := Readiness{Native: GradeReady, Docker: GradeReady}
	recipes := buildRecipes(plat, dkr, rd, 8)

	if !strings.Contains(recipes.Native, "taskset") {
		t.Error("Linux native recipe should use taskset")
	}
	if !strings.Contains(recipes.Native, "perflock") {
		t.Error("Linux native recipe should use perflock")
	}
	if !strings.Contains(recipes.Docker, "--cpuset-cpus=2") {
		t.Error("Docker recipe should use verified CPU 2")
	}
}

func TestBuildRecipesDockerUnavailable(t *testing.T) {
	plat := Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := Docker{Available: false}
	rd := Readiness{Native: GradeReady, Docker: GradeUnavailable}
	recipes := buildRecipes(plat, dkr, rd, 8)

	if recipes.Docker != "" {
		t.Error("expected empty Docker recipe when unavailable")
	}
}

func TestColimaBenchProfileCmd(t *testing.T) {
	cmd := colimaBenchProfileCmd()
	if !strings.Contains(cmd, "colima start --profile benchlab") {
		t.Error("expected colima start --profile benchlab")
	}
	if !strings.Contains(cmd, "--arch aarch64") {
		t.Error("expected --arch aarch64")
	}
	if !strings.Contains(cmd, "--vm-type vz") {
		t.Error("expected --vm-type vz")
	}
}
