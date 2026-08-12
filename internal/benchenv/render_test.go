package benchenv

import (
	"encoding/json"
	"strings"
	"testing"
)

// sampleReport builds a representative report for renderer/JSON tests.
func sampleReport() Report {
	return Report{
		OS:     "darwin",
		Arch:   "arm64",
		NumCPU: 10,
		Checks: []Check{
			{Name: "macOS CPU controls", Status: StatusUnavailable,
				Detail: "macOS cannot prove or control the physical CPU scheduler",
				Remedy: "use a Linux bare-metal runner for publication-quality numbers"},
			{Name: "load average", Status: StatusWarn,
				Detail: "1-min load 6.00 > 5.0 — competing workloads add scheduling noise",
				Remedy: "close background applications before benchmarking"},
			{Name: "perflock not installed", Status: StatusWarn,
				Detail: "perflock not found on PATH",
				Remedy: "go install github.com/aclements/perflock@latest"},
		},
		Summary: Summary{OK: 0, Warn: 2, Unavailable: 1},
		Platform: Platform{
			OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none",
			CPUModel: "Apple M2", Power: "ac", LoadAvg: 6.0,
		},
		Docker: Docker{
			Available: true, Context: "colima", Local: true,
			Backend: "colima-vz", EngineArch: "arm64", Translation: "none",
			Isolation: &IsolationProbe{Ran: true, Passed: true, CgroupVersion: "v2", SelectedCPU: "0"},
		},
		Readiness: Readiness{
			Overall: GradeNotReady, RecommendedPath: "native",
			Native: GradeNotReady, Docker: GradeLimited,
		},
		Actions: []Action{
			{Priority: 1, Scope: "platform", Reason: "reduce background load",
				Commands: []string{"# Close browser, IDE, Slack"}},
		},
		Recipes: Recipes{
			Native: "# macOS has a higher noise floor\ngo test -bench=. -count=20 ./...",
			Docker: "docker run --rm --network=none --platform=linux/arm64 --cpus=1 ...",
		},
	}
}

func TestJSONLegacyFieldsPresent(t *testing.T) {
	r := sampleReport()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	// Legacy fields must remain present.
	for _, key := range []string{"os", "arch", "numcpu", "checks", "summary"} {
		if _, ok := m[key]; !ok {
			t.Errorf("legacy field %q missing from JSON", key)
		}
	}
}

func TestJSONNewFieldsPresent(t *testing.T) {
	r := sampleReport()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	// New structured fields must be present.
	for _, key := range []string{"platform", "docker", "readiness", "actions", "recipes"} {
		if _, ok := m[key]; !ok {
			t.Errorf("new field %q missing from JSON", key)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	r := sampleReport()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var r2 Report
	if err := json.Unmarshal(data, &r2); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if r2.OS != r.OS || r2.Arch != r.Arch || r2.NumCPU != r.NumCPU {
		t.Errorf("round-trip mismatch: os=%q arch=%q numcpu=%d", r2.OS, r2.Arch, r2.NumCPU)
	}
	if r2.Readiness.Overall != r.Readiness.Overall {
		t.Errorf("readiness overall mismatch: %q vs %q", r2.Readiness.Overall, r.Readiness.Overall)
	}
	if r2.Docker.Backend != r.Docker.Backend {
		t.Errorf("docker backend mismatch: %q vs %q", r2.Docker.Backend, r.Docker.Backend)
	}
}

func TestRenderTextContainsReasonAndRemedy(t *testing.T) {
	r := sampleReport()
	text := RenderText(r)
	// The warn check's detail (reason) must be present.
	if !strings.Contains(text, "competing workloads add scheduling noise") {
		t.Error("text missing check detail (reason)")
	}
	// The warn check's remedy must also be present.
	if !strings.Contains(text, "close background applications") {
		t.Error("text missing check remedy (command)")
	}
	// Both must appear, not just one replacing the other.
	if !strings.Contains(text, "remedy:") {
		t.Error("text missing 'remedy:' label")
	}
}

func TestRenderTextLeadsWithArchitecture(t *testing.T) {
	r := sampleReport()
	text := RenderText(r)
	// Architecture must appear early in the Platform section.
	platIdx := strings.Index(text, "Platform")
	if platIdx < 0 {
		t.Fatal("missing Platform section")
	}
	archIdx := strings.Index(text[platIdx:], "architecture:")
	if archIdx < 0 {
		t.Error("missing architecture line in Platform section")
	}
	// Translation and virtualization must also be visible.
	if !strings.Contains(text, "translation:") {
		t.Error("missing translation in Platform section")
	}
	if !strings.Contains(text, "virtualization:") {
		t.Error("missing virtualization in Platform section")
	}
}

func TestRenderTextContainsReadiness(t *testing.T) {
	r := sampleReport()
	text := RenderText(r)
	if !strings.Contains(text, "Readiness") {
		t.Error("missing Readiness section")
	}
	if !strings.Contains(text, "overall:") {
		t.Error("missing overall grade in Readiness")
	}
	if !strings.Contains(text, "recommended path:") {
		t.Error("missing recommended path in Readiness")
	}
}

func TestRenderTextContainsRecipes(t *testing.T) {
	r := sampleReport()
	text := RenderText(r)
	if !strings.Contains(text, "Benchmark recipes") {
		t.Error("missing Benchmark recipes section")
	}
	if !strings.Contains(text, "Native:") {
		t.Error("missing Native recipe")
	}
	if !strings.Contains(text, "Docker:") {
		t.Error("missing Docker recipe")
	}
}

func TestRenderMacOSNoLinuxRemedies(t *testing.T) {
	r := sampleReport()
	r.OS = "darwin"
	r.Checks = []Check{
		{Name: "macOS CPU controls", Status: StatusUnavailable, Detail: "macOS cannot control CPU"},
	}
	text := RenderText(r)
	// Must NOT contain Linux-specific sysfs remedies.
	linuxRemedies := []string{
		"/sys/devices/system/cpu/smt/control",
		"/sys/devices/system/cpu/cpu*/cpufreq/scaling_governor",
		"/sys/devices/system/cpu/intel_pstate/no_turbo",
		"/sys/devices/system/cpu/cpufreq/boost",
	}
	for _, remedy := range linuxRemedies {
		if strings.Contains(text, remedy) {
			t.Errorf("macOS output contains Linux-specific remedy: %s", remedy)
		}
	}
}

func TestRenderLinuxHasLinuxRemedies(t *testing.T) {
	r := Report{
		OS: "linux", Arch: "amd64", NumCPU: 8,
		Checks: []Check{
			{Name: "SMT control", Status: StatusWarn,
				Detail: "SMT enabled", Remedy: "echo off | sudo tee /sys/devices/system/cpu/smt/control"},
		},
		Summary:  Summary{Warn: 1},
		Platform: Platform{OS: "linux", Arch: "amd64", Virtualization: "none", Translation: "none"},
		Readiness: Readiness{Overall: GradeNotReady, RecommendedPath: "native",
			Native: GradeNotReady, Docker: GradeUnavailable},
	}
	text := RenderText(r)
	if !strings.Contains(text, "/sys/devices/system/cpu/smt/control") {
		t.Error("Linux output missing Linux-specific remedy")
	}
}

func TestRenderDockerUnavailable(t *testing.T) {
	r := Report{
		OS: "linux", Arch: "arm64", NumCPU: 8,
		Checks:   []Check{},
		Summary:  Summary{},
		Platform: Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"},
		Docker:   Docker{Available: false, UnavailableMsg: "docker CLI not found on PATH"},
		Readiness: Readiness{Overall: GradeReady, RecommendedPath: "native",
			Native: GradeReady, Docker: GradeUnavailable},
	}
	text := RenderText(r)
	if !strings.Contains(text, "available:  no") {
		t.Error("missing Docker unavailable indicator")
	}
	if !strings.Contains(text, "docker CLI not found") {
		t.Error("missing unavailable message")
	}
}

// Exit code policy tests.

func TestExitCodeDefault(t *testing.T) {
	// Default mode always exits 0 after completed diagnosis.
	r := sampleReport()
	if code := simulateExitCode(r, false); code != 0 {
		t.Errorf("default exit = %d, want 0", code)
	}
}

func TestExitCodeStrictNotReady(t *testing.T) {
	// -strict exits 1 when overall is not ready.
	r := sampleReport() // overall = not_ready
	if code := simulateExitCode(r, true); code != 1 {
		t.Errorf("strict exit = %d, want 1 (not_ready)", code)
	}
}

func TestExitCodeStrictReady(t *testing.T) {
	// -strict exits 0 when overall is ready.
	r := sampleReport()
	r.Readiness.Overall = GradeReady
	if code := simulateExitCode(r, true); code != 0 {
		t.Errorf("strict exit = %d, want 0 (ready)", code)
	}
}

func TestExitCodeStrictLimited(t *testing.T) {
	// -strict exits 1 for limited (macOS, VM-backed).
	r := sampleReport()
	r.Readiness.Overall = GradeLimited
	if code := simulateExitCode(r, true); code != 1 {
		t.Errorf("strict exit = %d, want 1 (limited)", code)
	}
}

// simulateExitCode mirrors the CLI's exit code policy.
func simulateExitCode(r Report, strict bool) int {
	if strict && r.Readiness.Overall != GradeReady {
		return 1
	}
	return 0
}
