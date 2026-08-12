//go:build darwin

package benchenv

import (
	"testing"
)

// TestDarwinChecksNoLinuxRemedies verifies macOS checks don't emit Linux remedies.
func TestDarwinChecksNoLinuxRemedies(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			switch {
			case name == "sysctl" && hasPrefix(args, "-n", "vm.loadavg"):
				return "{ 0.10 0.20 0.30 }", nil
			case name == "pmset":
				return "", nil
			default:
				return "", nil
			}
		},
		lookFn: constLookFn(),
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("darwin"), withArch("arm64"), withNumCPU(10))
	plat := Platform{OS: "darwin", Arch: "arm64", Virtualization: "none", Translation: "none", Power: "ac", PowerMode: "automatic"}
	checks := p.platformChecks(plat)

	linuxRemedies := []string{
		"/sys/devices/system/cpu/smt/control",
		"/sys/devices/system/cpu/cpu*/cpufreq/scaling_governor",
		"/sys/devices/system/cpu/intel_pstate/no_turbo",
	}
	for _, c := range checks {
		for _, remedy := range linuxRemedies {
			if c.Remedy == remedy {
				t.Errorf("macOS check %q has Linux-specific remedy: %s", c.Name, remedy)
			}
		}
	}
}

// TestDarwinChecksRosettaWarn verifies Rosetta translation produces a warning.
func TestDarwinChecksRosettaWarn(t *testing.T) {
	p := newProber(withOS("darwin"), withArch("arm64"), withNumCPU(10))
	plat := Platform{OS: "darwin", Arch: "arm64", Translation: "rosetta"}
	checks := p.platformChecks(plat)

	found := false
	for _, c := range checks {
		if c.Name == "Rosetta translation" {
			found = true
			if c.Status != StatusWarn {
				t.Errorf("Rosetta check status = %q, want warn", c.Status)
			}
		}
	}
	if !found {
		t.Error("expected Rosetta translation warning check")
	}
}

// TestDarwinChecksBatteryWarn verifies battery power produces a warning.
func TestDarwinChecksBatteryWarn(t *testing.T) {
	p := newProber(withOS("darwin"), withArch("arm64"), withNumCPU(10))
	plat := Platform{OS: "darwin", Arch: "arm64", Translation: "none", Power: "battery"}
	checks := p.platformChecks(plat)

	found := false
	for _, c := range checks {
		if c.Name == "power source" && c.Status == StatusWarn {
			found = true
		}
	}
	if !found {
		t.Error("expected battery power warning")
	}
}

// TestDarwinChecksLowPowerModeWarn verifies Low Power Mode produces a warning.
func TestDarwinChecksLowPowerModeWarn(t *testing.T) {
	p := newProber(withOS("darwin"), withArch("arm64"), withNumCPU(10))
	plat := Platform{OS: "darwin", Arch: "arm64", Translation: "none", Power: "ac", PowerMode: "low"}
	checks := p.platformChecks(plat)

	found := false
	for _, c := range checks {
		if c.Name == "power mode" && c.Status == StatusWarn {
			found = true
		}
	}
	if !found {
		t.Error("expected Low Power Mode warning")
	}
}

// TestDarwinDetectPlatform verifies macOS platform detection with fakes.
func TestDarwinDetectPlatform(t *testing.T) {
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
				return "{ 0.50 0.40 0.30 }", nil
			case name == "pmset" && hasPrefix(args, "-g", "ps"):
				return "Now drawing from 'AC Power'", nil
			case name == "pmset" && hasPrefix(args, "-g"):
				return "  lowpowermode     0\n", nil
			case name == "pmset" && hasPrefix(args, "-g", "therm"):
				return "", nil
			default:
				return "", nil
			}
		},
		lookFn: constLookFn(),
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("darwin"), withArch("arm64"), withNumCPU(10))
	plat := p.detectPlatform()

	if plat.Arch != "arm64" {
		t.Errorf("arch = %q, want arm64", plat.Arch)
	}
	if plat.Translation != "none" {
		t.Errorf("translation = %q, want none", plat.Translation)
	}
	if plat.CPUModel != "Apple M2" {
		t.Errorf("CPU model = %q, want Apple M2", plat.CPUModel)
	}
	if plat.Power != "ac" {
		t.Errorf("power = %q, want ac", plat.Power)
	}
	if plat.PowerMode != "automatic" {
		t.Errorf("power mode = %q, want automatic", plat.PowerMode)
	}
	if plat.LoadAvg != 0.50 {
		t.Errorf("load avg = %f, want 0.50", plat.LoadAvg)
	}
}
