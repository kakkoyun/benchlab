//go:build linux

package benchenv

import (
	"testing"
)

// TestLinuxChecksARM64NoIntelTurbo verifies that ARM64 does not produce
// Intel/AMD turbo checks.
func TestLinuxChecksARM64NoIntelTurbo(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/sys/devices/system/cpu/smt/control":                   "off\n",
		"/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor": "performance\n",
		"/proc/loadavg": "0.10 0.20 0.30 1/100 12345\n",
	}}
	exec := &fakeExec{
		runFn:  func(name string, args ...string) (string, error) { return "", nil },
		lookFn: constLookFn("systemd-detect-virt"),
	}
	p := newProber(withExec(exec), withFS(fs), withOS("linux"), withArch("arm64"), withNumCPU(8))
	plat := Platform{OS: "linux", Arch: "arm64", Virtualization: "none", Translation: "none"}
	checks := p.platformChecks(plat)

	for _, c := range checks {
		if c.Name == "Turbo Boost (Intel)" || c.Name == "Turbo Boost (AMD)" {
			t.Errorf("ARM64 should not produce Intel/AMD turbo check, got: %s", c.Name)
		}
	}
	// ARM64 should have a CPU boost/turbo unavailable check.
	foundTurbo := false
	for _, c := range checks {
		if c.Name == "CPU boost/turbo" {
			foundTurbo = true
			if c.Status != StatusUnavailable {
				t.Errorf("ARM64 turbo check status = %q, want unavailable", c.Status)
			}
		}
	}
	if !foundTurbo {
		t.Error("expected CPU boost/turbo check for ARM64")
	}
}

// TestLinuxChecksAMD64HasIntelTurbo verifies that AMD64 produces Intel/AMD
// turbo checks.
func TestLinuxChecksAMD64HasIntelTurbo(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/sys/devices/system/cpu/smt/control":                   "off\n",
		"/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor": "performance\n",
		"/sys/devices/system/cpu/intel_pstate/no_turbo":         "1\n",
		"/proc/loadavg": "0.10 0.20 0.30 1/100 12345\n",
	}}
	exec := &fakeExec{
		runFn:  func(name string, args ...string) (string, error) { return "", nil },
		lookFn: constLookFn("systemd-detect-virt"),
	}
	p := newProber(withExec(exec), withFS(fs), withOS("linux"), withArch("amd64"), withNumCPU(8))
	plat := Platform{OS: "linux", Arch: "amd64", Virtualization: "none", Translation: "none"}
	checks := p.platformChecks(plat)

	foundIntel := false
	for _, c := range checks {
		if c.Name == "Turbo Boost (Intel)" {
			foundIntel = true
		}
	}
	if !foundIntel {
		t.Error("expected Intel Turbo Boost check for AMD64")
	}
}

// TestLinuxVirtDetectionBareMetal verifies bare-metal detection.
func TestLinuxVirtDetectionBareMetal(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			if name == "systemd-detect-virt" {
				return "none\n", nil
			}
			return "", nil
		},
		lookFn: constLookFn("systemd-detect-virt"),
	}
	fs := &fakeFS{files: map[string]string{}}
	p := newProber(withExec(exec), withFS(fs), withOS("linux"), withArch("arm64"), withNumCPU(8))
	virt, evidence := p.linuxVirt()
	if virt != "none" {
		t.Errorf("virt = %q, want none", virt)
	}
	if evidence == "" {
		t.Error("expected non-empty evidence")
	}
}

// TestLinuxVirtDetectionKVM verifies KVM detection via DMI.
func TestLinuxVirtDetectionKVM(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			return "", nil // systemd-detect-virt not available
		},
		lookFn: func(string) error { return nil },
	}
	fs := &fakeFS{files: map[string]string{
		"/sys/class/dmi/id/sys_vendor": "QEMU\n",
	}}
	p := newProber(withExec(exec), withFS(fs), withOS("linux"), withArch("amd64"), withNumCPU(4))
	virt, _ := p.linuxVirt()
	if virt != "qemu" {
		t.Errorf("virt = %q, want qemu", virt)
	}
}

// TestLinuxContainerDetection verifies container detection.
func TestLinuxContainerDetection(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/.dockerenv": "",
	}}
	p := newProber(withFS(fs), withOS("linux"), withArch("arm64"), withNumCPU(8))
	if !p.linuxInContainer() {
		t.Error("expected container detection via /.dockerenv")
	}
}

// TestLinuxContainerDetectionCgroup verifies container detection via cgroup.
func TestLinuxContainerDetectionCgroup(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/proc/1/cgroup": "0::/docker/abc123\n",
	}}
	p := newProber(withFS(fs), withOS("linux"), withArch("arm64"), withNumCPU(8))
	if !p.linuxInContainer() {
		t.Error("expected container detection via cgroup")
	}
}

// TestLinuxContainerDetectionNone verifies no false positive.
func TestLinuxContainerDetectionNone(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/proc/1/cgroup": "0::/system.slice/sshd.service\n",
	}}
	p := newProber(withFS(fs), withOS("linux"), withArch("arm64"), withNumCPU(8))
	if p.linuxInContainer() {
		t.Error("expected no container detection")
	}
}
