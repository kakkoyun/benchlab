package benchenv

import "testing"

func TestGradeNative(t *testing.T) {
	tests := []struct {
		name      string
		plat      Platform
		hostNoise bool
		want      PathGrade
	}{
		{
			"bare-metal Linux no warns",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"},
			false, GradeReady,
		},
		{
			"bare-metal Linux with warns",
			Platform{OS: "linux", Arch: "amd64", Virtualization: "none", Translation: "none"},
			true, GradeNotReady,
		},
		{
			"Linux in KVM VM",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "kvm", Translation: "none"},
			false, GradeLimited,
		},
		{
			"Linux in QEMU VM",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "qemu", Translation: "none"},
			false, GradeLimited,
		},
		{
			"macOS native no warns",
			Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"},
			false, GradeLimited,
		},
		{
			"macOS Rosetta",
			Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "rosetta"},
			false, GradeNotReady,
		},
		{
			"macOS with host noise",
			Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"},
			true, GradeNotReady,
		},
		{
			"other platform",
			Platform{OS: "windows", Arch: "amd64", Virtualization: "unknown", Translation: "none"},
			false, GradeLimited,
		},
		{
			"Linux QEMU translation",
			Platform{OS: "linux", Arch: "amd64", Virtualization: "qemu", Translation: "qemu"},
			false, GradeNotReady,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gradeNative(tt.plat, tt.hostNoise); got != tt.want {
				t.Errorf("gradeNative() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGradeDocker(t *testing.T) {
	passed := true
	tests := []struct {
		name      string
		plat      Platform
		dkr       Docker
		hostNoise bool
		want      PathGrade
	}{
		{
			"unavailable",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "none"},
			Docker{Available: false},
			false, GradeUnavailable,
		},
		{
			"remote daemon",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: false, Backend: "engine"},
			false, GradeLimited,
		},
		{
			"native engine bare-metal passing probe",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: true, Backend: "engine", EngineArch: "arm64",
				Isolation: &IsolationProbe{Ran: true, Passed: passed}},
			false, GradeReady,
		},
		{
			"native engine bare-metal failing probe",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: true, Backend: "engine", EngineArch: "arm64",
				Isolation: &IsolationProbe{Ran: true, Passed: false, Findings: []Check{{Name: "x", Status: StatusWarn}}}},
			false, GradeNotReady,
		},
		{
			"colima VZ on macOS passing probe",
			Platform{OS: "darwin", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: true, Backend: "colima-vz", EngineArch: "arm64",
				Isolation: &IsolationProbe{Ran: true, Passed: passed}},
			false, GradeLimited,
		},
		{
			"docker desktop on macOS",
			Platform{OS: "darwin", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: true, Backend: "docker-desktop-apple", EngineArch: "arm64",
				Isolation: &IsolationProbe{Ran: true, Passed: passed}},
			false, GradeLimited,
		},
		{
			"QEMU cross-arch",
			Platform{OS: "darwin", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: true, Backend: "colima-qemu", EngineArch: "amd64",
				Translation: "qemu"},
			false, GradeNotReady,
		},
		{
			"unknown backend",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: true, Backend: "unknown", EngineArch: "arm64",
				Isolation: &IsolationProbe{Ran: true, Passed: passed}},
			false, GradeLimited,
		},
		{
			"host noise warns",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: true, Backend: "engine", EngineArch: "arm64",
				Isolation: &IsolationProbe{Ran: true, Passed: passed}},
			true, GradeNotReady,
		},
		{
			"probe error",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "none"},
			Docker{Available: true, Local: true, Backend: "engine", EngineArch: "arm64",
				Isolation: &IsolationProbe{Ran: true, Passed: false, Error: "timeout"}},
			false, GradeNotReady,
		},
		{
			"Linux VM with native engine",
			Platform{OS: "linux", Arch: "arm64", Virtualization: "kvm"},
			Docker{Available: true, Local: true, Backend: "engine", EngineArch: "arm64",
				Isolation: &IsolationProbe{Ran: true, Passed: passed}},
			false, GradeLimited,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gradeDocker(tt.plat, tt.dkr, tt.hostNoise); got != tt.want {
				t.Errorf("gradeDocker() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBestPath(t *testing.T) {
	tests := []struct {
		native    PathGrade
		docker    PathGrade
		wantGrade PathGrade
		wantPath  string
	}{
		{GradeReady, GradeReady, GradeReady, "native"},
		{GradeReady, GradeLimited, GradeReady, "native"},
		{GradeLimited, GradeReady, GradeReady, "docker"},
		{GradeLimited, GradeLimited, GradeLimited, "native"},
		{GradeNotReady, GradeLimited, GradeLimited, "docker"},
		{GradeUnavailable, GradeReady, GradeReady, "docker"},
		{GradeUnavailable, GradeUnavailable, GradeUnavailable, "native"},
		{GradeNotReady, GradeUnavailable, GradeNotReady, "native"},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got, path := bestPath(tt.native, tt.docker)
			if got != tt.wantGrade {
				t.Errorf("bestPath(%q, %q) grade = %q, want %q", tt.native, tt.docker, got, tt.wantGrade)
			}
			if path != tt.wantPath {
				t.Errorf("bestPath(%q, %q) path = %q, want %q", tt.native, tt.docker, path, tt.wantPath)
			}
		})
	}
}

func TestHasHostNoiseWarns(t *testing.T) {
	tests := []struct {
		name   string
		checks []Check
		want   bool
	}{
		{"no warns", []Check{{Name: "SMT control", Status: StatusOK}}, false},
		{"warn on host check", []Check{{Name: "SMT control", Status: StatusWarn}}, true},
		{"warn on tool check excluded", []Check{{Name: "perflock not installed", Status: StatusWarn}}, false},
		{"warn on runtime excluded", []Check{{Name: "GOMAXPROCS / NumCPU", Status: StatusWarn}}, false},
		{"mixed", []Check{
			{Name: "perflock not installed", Status: StatusWarn},
			{Name: "load average", Status: StatusWarn},
		}, true},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasHostNoiseWarns(tt.checks); got != tt.want {
				t.Errorf("hasHostNoiseWarns() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBareMetalLinux(t *testing.T) {
	tests := []struct {
		plat Platform
		want bool
	}{
		{Platform{OS: "linux", Virtualization: "none"}, true},
		{Platform{OS: "linux", Virtualization: "kvm"}, false},
		{Platform{OS: "darwin", Virtualization: "none"}, false},
		{Platform{OS: "linux", Virtualization: ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.plat.OS+"/"+tt.plat.Virtualization, func(t *testing.T) {
			if got := isBareMetalLinux(tt.plat); got != tt.want {
				t.Errorf("isBareMetalLinux() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeReadinessMatrix(t *testing.T) {
	// Bare-metal Linux, no warns, native engine, passing probe → ready.
	t.Run("bare-metal Linux ready", func(t *testing.T) {
		plat := Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"}
		dkr := Docker{Available: true, Local: true, Backend: "engine", EngineArch: "arm64",
			Isolation: &IsolationProbe{Ran: true, Passed: true}}
		checks := []Check{{Name: "SMT control", Status: StatusOK}}
		rd := computeReadiness(plat, dkr, checks)
		if rd.Overall != GradeReady {
			t.Errorf("overall = %q, want ready", rd.Overall)
		}
		if rd.Native != GradeReady {
			t.Errorf("native = %q, want ready", rd.Native)
		}
		if rd.Docker != GradeReady {
			t.Errorf("docker = %q, want ready", rd.Docker)
		}
	})

	// macOS with Colima VZ, no warns → limited.
	t.Run("macOS Colima limited", func(t *testing.T) {
		plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"}
		dkr := Docker{Available: true, Local: true, Backend: "colima-vz", EngineArch: "arm64",
			Isolation: &IsolationProbe{Ran: true, Passed: true}}
		checks := []Check{{Name: "macOS CPU controls", Status: StatusUnavailable}}
		rd := computeReadiness(plat, dkr, checks)
		if rd.Overall != GradeLimited {
			t.Errorf("overall = %q, want limited", rd.Overall)
		}
		if rd.RecommendedPath != "native" {
			t.Errorf("recommended = %q, want native", rd.RecommendedPath)
		}
	})

	// Docker unavailable, clean native Linux → ready.
	t.Run("Docker unavailable native ready", func(t *testing.T) {
		plat := Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"}
		dkr := Docker{Available: false}
		checks := []Check{{Name: "SMT control", Status: StatusOK}}
		rd := computeReadiness(plat, dkr, checks)
		if rd.Overall != GradeReady {
			t.Errorf("overall = %q, want ready", rd.Overall)
		}
		if rd.Docker != GradeUnavailable {
			t.Errorf("docker = %q, want unavailable", rd.Docker)
		}
	})

	// QEMU cross-arch with host noise → not_ready (both paths blocked).
	t.Run("QEMU cross-arch not_ready", func(t *testing.T) {
		plat := Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"}
		dkr := Docker{Available: true, Local: true, Backend: "colima-qemu", EngineArch: "amd64",
			Translation: "qemu"}
		checks := []Check{{Name: "SMT control", Status: StatusWarn}}
		rd := computeReadiness(plat, dkr, checks)
		if rd.Overall != GradeNotReady {
			t.Errorf("overall = %q, want not_ready", rd.Overall)
		}
	})
}
