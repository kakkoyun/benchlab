package benchenv

import (
	"errors"
	"strings"
	"testing"
)

func TestIsLocalEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     bool
	}{
		{"unix:///var/run/docker.sock", true},
		{"unix:///Users/foo/.colima/docker.sock", true},
		{"tcp://localhost:2375", true},
		{"tcp://127.0.0.1:2375", true},
		{"", true}, // default unix socket
		{"tcp://10.0.0.5:2375", false},
		{"ssh://user@remote-host", false},
		{"tcp://docker.example.com:2376", false},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			if got := isLocalEndpoint(tt.endpoint); got != tt.want {
				t.Errorf("isLocalEndpoint(%q) = %v, want %v", tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestIsColima(t *testing.T) {
	tests := []struct {
		ctxName  string
		endpoint string
		want     bool
	}{
		{"colima", "unix:///foo", true},
		{"colima-benchlab", "unix:///foo", true},
		{"default", "unix:///Users/foo/.colima/docker.sock", true},
		{"desktop-linux", "unix:///var/run/docker.sock", false},
		{"default", "tcp://localhost:2375", false},
	}
	for _, tt := range tests {
		t.Run(tt.ctxName+"/"+tt.endpoint, func(t *testing.T) {
			if got := isColima(tt.ctxName, tt.endpoint); got != tt.want {
				t.Errorf("isColima(%q, %q) = %v, want %v", tt.ctxName, tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestIsDockerDesktop(t *testing.T) {
	tests := []struct {
		ctxName  string
		osString string
		want     bool
	}{
		{"desktop-linux", "Docker Desktop", true},
		{"desktop", "Alpine Linux v3.20", true},
		{"default", "Docker Desktop", true},
		{"colima", "Ubuntu 22.04", false},
		{"default", "Ubuntu 22.04", false},
	}
	for _, tt := range tests {
		t.Run(tt.ctxName+"/"+tt.osString, func(t *testing.T) {
			if got := isDockerDesktop(tt.ctxName, tt.osString); got != tt.want {
				t.Errorf("isDockerDesktop(%q, %q) = %v, want %v", tt.ctxName, tt.osString, got, tt.want)
			}
		})
	}
}

func TestComputeDockerTranslation(t *testing.T) {
	tests := []struct {
		engineArch string
		hostArch   string
		want       string
	}{
		{"arm64", "arm64", "none"},
		{"amd64", "amd64", "none"},
		{"amd64", "arm64", "qemu"},
		{"arm64", "amd64", "qemu"},
		{"aarch64", "arm64", "none"},
		{"x86_64", "amd64", "none"},
		{"", "arm64", "none"},
		{"arm64", "", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.engineArch+"/"+tt.hostArch, func(t *testing.T) {
			if got := computeDockerTranslation(tt.engineArch, tt.hostArch); got != tt.want {
				t.Errorf("computeDockerTranslation(%q, %q) = %q, want %q", tt.engineArch, tt.hostArch, got, tt.want)
			}
		})
	}
}

func TestIsVMBackend(t *testing.T) {
	tests := []struct {
		backend string
		want    bool
	}{
		{"engine", false},
		{"colima-vz", true},
		{"colima-qemu", true},
		{"colima-unknown", true},
		{"docker-desktop-apple", true},
		{"docker-desktop-qemu", true},
		{"docker-desktop-unknown", true},
		{"unknown", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.backend, func(t *testing.T) {
			if got := isVMBackend(tt.backend); got != tt.want {
				t.Errorf("isVMBackend(%q) = %v, want %v", tt.backend, got, tt.want)
			}
		})
	}
}

func TestClassifyDockerDesktopSettings(t *testing.T) {
	tests := []struct {
		json string
		want string
	}{
		{`{"useVmm":true}`, "docker-desktop-vmm"},
		{`{"useVirtualizationFramework":true}`, "docker-desktop-apple"},
		{`{"useVmm":false}`, ""},                     // false flag alone does not prove QEMU
		{`{"useVirtualizationFramework":false}`, ""}, // false flag alone does not prove QEMU
		{`{"otherSetting":42}`, ""},
		{`{}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.json, func(t *testing.T) {
			if got := classifyDockerDesktopSettings(tt.json); got != tt.want {
				t.Errorf("classifyDockerDesktopSettings(%q) = %q, want %q", tt.json, got, tt.want)
			}
		})
	}
}

// TestDetectDockerColimaVZ verifies Colima with Apple Virtualization.framework
// is detected correctly using fake command output.
func TestDetectDockerColimaVZ(t *testing.T) {
	fs := &fakeFS{files: map[string]string{}}
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "docker" && hasPrefix(args, "info", "--format"):
				return "linux\naarch64\nUbuntu 22.04\n4\n8589934592", nil
			case name == "docker" && hasPrefix(args, "context", "show"):
				return "colima", nil
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
				return "", errors.New("unexpected command: " + name)
			}
		},
		lookFn: constLookFn("docker", "colima"),
	}

	p := newProber(
		withExec(exec), withFS(fs),
		withOS("darwin"), withArch("arm64"), withNumCPU(10),
	)
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := p.detectDocker(plat)

	if !dkr.Available {
		t.Fatal("expected Docker available")
	}
	if dkr.Backend != "colima-vz" {
		t.Errorf("backend = %q, want colima-vz", dkr.Backend)
	}
	if dkr.EngineArch != "arm64" {
		t.Errorf("engine arch = %q, want arm64", dkr.EngineArch)
	}
	if dkr.Translation != "none" {
		t.Errorf("translation = %q, want none", dkr.Translation)
	}
	if !dkr.Local {
		t.Error("expected local daemon")
	}
	if dkr.VMResources.CPUs != 4 {
		t.Errorf("VM CPUs = %d, want 4", dkr.VMResources.CPUs)
	}
}

// TestDetectDockerColimaQEMU verifies Colima with QEMU driver and cross-arch.
func TestDetectDockerColimaQEMU(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "docker" && hasPrefix(args, "info", "--format"):
				return "linux\nx86_64\nUbuntu 22.04\n4\n8589934592", nil
			case name == "docker" && hasPrefix(args, "context", "show"):
				return "colima-x86", nil
			case name == "docker" && hasPrefix(args, "context", "inspect"):
				return `[{"Name":"colima-x86","Endpoints":{"docker":{"Host":"unix:///Users/foo/.colima-x86/docker.sock"}}}]`, nil
			case name == "colima" && hasPrefix(args, "status", "--json"):
				return `{"driver":"QEMU","arch":"x86_64","cpu":4,"memory":8,"runtime":"docker","status":"Running"}`, nil
			case name == "docker" && hasPrefix(args, "run"):
				return "0-3\n", nil
			default:
				return "", errors.New("unexpected")
			}
		},
		lookFn: constLookFn("docker", "colima"),
	}

	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("darwin"), withArch("arm64"), withNumCPU(10))
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := p.detectDocker(plat)

	if dkr.Backend != "colima-qemu" {
		t.Errorf("backend = %q, want colima-qemu", dkr.Backend)
	}
	if dkr.Translation != "qemu" {
		t.Errorf("translation = %q, want qemu (cross-arch)", dkr.Translation)
	}
}

// TestDetectDockerUnavailable verifies Docker absence is not a fatal error.
func TestDetectDockerUnavailable(t *testing.T) {
	exec := &fakeExec{
		runFn:  func(name string, args ...string) (string, error) { return "", errors.New("not found") },
		lookFn: func(string) error { return errors.New("docker not on PATH") },
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("darwin"), withArch("arm64"), withNumCPU(8))
	dkr := p.detectDocker(Platform{OS: "darwin", Arch: "arm64"})

	if dkr.Available {
		t.Error("expected Docker unavailable")
	}
	if dkr.UnavailableMsg == "" {
		t.Error("expected unavailable message")
	}
}

// TestDetectDockerRemote verifies remote daemon handling.
func TestDetectDockerRemote(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "docker" && hasPrefix(args, "info", "--format"):
				return "linux\narm64\nUbuntu 22.04\n4\n8589934592", nil
			case name == "docker" && hasPrefix(args, "context", "show"):
				return "remote", nil
			case name == "docker" && hasPrefix(args, "context", "inspect"):
				return `[{"Name":"remote","Endpoints":{"docker":{"Host":"tcp://10.0.0.5:2375"}}}]`, nil
			default:
				return "", errors.New("unexpected")
			}
		},
		lookFn: constLookFn("docker"),
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("linux"), withArch("arm64"), withNumCPU(8))
	plat := Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := p.detectDocker(plat)

	if !dkr.Available {
		t.Fatal("expected Docker available")
	}
	if dkr.Local {
		t.Error("expected remote daemon")
	}
	// Remote daemon should not run a probe.
	if dkr.Isolation != nil && dkr.Isolation.Ran {
		t.Error("expected no probe for remote daemon")
	}
}

// TestDetectDockerNativeEngine verifies native Docker Engine on Linux.
func TestDetectDockerNativeEngine(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "docker" && hasPrefix(args, "info", "--format"):
				return "linux\narm64\nUbuntu 22.04.3 LTS\n8\n17179869184", nil
			case name == "docker" && hasPrefix(args, "context", "show"):
				return "default", nil
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
				return "", errors.New("unexpected")
			}
		},
		lookFn: constLookFn("docker"),
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("linux"), withArch("arm64"), withNumCPU(8))
	plat := Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := p.detectDocker(plat)

	if dkr.Backend != "engine" {
		t.Errorf("backend = %q, want engine", dkr.Backend)
	}
	if dkr.Isolation == nil || !dkr.Isolation.Passed {
		t.Error("expected passing isolation probe")
	}
}

// TestDetectDockerDockerDesktop verifies Docker Desktop detection.
func TestDetectDockerDockerDesktop(t *testing.T) {
	fs := &fakeFS{files: map[string]string{}}
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "docker" && hasPrefix(args, "info", "--format"):
				return "linux\narm64\nDocker Desktop\n8\n8589934592", nil
			case name == "docker" && hasPrefix(args, "context", "show"):
				return "desktop-linux", nil
			case name == "docker" && hasPrefix(args, "context", "inspect"):
				return `[{"Name":"desktop-linux","Endpoints":{"docker":{"Host":"unix:///Users/foo/.docker/run/docker.sock"}}}]`, nil
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
				return "", errors.New("unexpected")
			}
		},
		lookFn: constLookFn("docker"),
	}
	p := newProber(withExec(exec), withFS(fs), withOS("darwin"), withArch("arm64"), withNumCPU(10))
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none"}
	dkr := p.detectDocker(plat)

	if !strings.HasPrefix(dkr.Backend, "docker-desktop") {
		t.Errorf("backend = %q, want docker-desktop-*", dkr.Backend)
	}
}
