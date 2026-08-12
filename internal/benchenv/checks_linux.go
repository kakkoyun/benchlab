//go:build linux

package benchenv

import (
	"fmt"
	"strings"
)

// linuxChecks returns Linux-specific checks tailored to the detected
// architecture and virtualization vendor.
func (p *prober) platformChecks(plat Platform) []Check {
	var checks []Check
	checks = append(checks, p.checkSMT(plat))
	checks = append(checks, p.checkGovernor(plat))
	checks = append(checks, p.checkTurbo(plat))
	checks = append(checks, p.checkLoadAvgLinux(plat))
	checks = append(checks, p.checkContainer(plat))
	return checks
}

func (p *prober) checkSMT(plat Platform) Check {
	const path = "/sys/devices/system/cpu/smt/control"
	data, err := p.fs.ReadFile(path)
	if err != nil {
		return Check{
			Name:   "SMT control",
			Status: StatusUnavailable,
			Detail: fmt.Sprintf("cannot read %s: %v (may be a VM, single-core CPU, or ARM without SMT)", path, err),
		}
	}
	status, detail, remedy := smtResult(strings.TrimSpace(data))
	return Check{
		Name:   "SMT control",
		Status: status,
		Detail: detail,
		Remedy: remedy,
	}
}

func (p *prober) checkGovernor(plat Platform) Check {
	const path = "/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor"
	data, err := p.fs.ReadFile(path)
	if err != nil {
		return Check{
			Name:   "CPU frequency governor",
			Status: StatusUnavailable,
			Detail: fmt.Sprintf("cannot read %s: %v (cpufreq driver may not be loaded, or running in a VM)", path, err),
		}
	}
	status, detail, remedy := governorResult(strings.TrimSpace(data))
	return Check{
		Name:   "CPU frequency governor",
		Status: status,
		Detail: detail,
		Remedy: remedy,
	}
}

func (p *prober) checkTurbo(plat Platform) Check {
	// ARM64 does not use Intel/AMD pstate drivers; skip x86-specific knobs.
	if plat.Arch == "arm64" {
		return Check{
			Name:   "CPU boost/turbo",
			Status: StatusUnavailable,
			Detail: "ARM64 CPUs do not expose Intel/AMD pstate turbo knobs; frequency control depends on the SoC driver",
		}
	}

	// Intel pstate driver.
	const intelPath = "/sys/devices/system/cpu/intel_pstate/no_turbo"
	if data, err := p.fs.ReadFile(intelPath); err == nil {
		status, detail, remedy := turboIntelResult(strings.TrimSpace(string(data)))
		return Check{
			Name:   "Turbo Boost (Intel)",
			Status: status,
			Detail: detail,
			Remedy: remedy,
		}
	}

	// AMD cpufreq boost knob.
	const amdPath = "/sys/devices/system/cpu/cpufreq/boost"
	if data, err := p.fs.ReadFile(amdPath); err == nil {
		status, detail, remedy := turboAMDResult(strings.TrimSpace(string(data)))
		return Check{
			Name:   "Turbo Boost (AMD)",
			Status: status,
			Detail: detail,
			Remedy: remedy,
		}
	}

	return Check{
		Name:   "Turbo Boost",
		Status: StatusUnavailable,
		Detail: "neither intel_pstate nor AMD cpufreq boost knob found; may be a VM or unsupported CPU",
	}
}

func (p *prober) checkLoadAvgLinux(plat Platform) Check {
	const path = "/proc/loadavg"
	data, err := p.fs.ReadFile(path)
	if err != nil {
		return Check{
			Name:   "load average",
			Status: StatusUnavailable,
			Detail: fmt.Sprintf("cannot read %s: %v", path, err),
		}
	}
	load, err := parseLoadAvg(strings.TrimSpace(data))
	if err != nil {
		return Check{
			Name:   "load average",
			Status: StatusUnavailable,
			Detail: fmt.Sprintf("parse error: %v", err),
		}
	}
	status, detail, remedy := loadAvgResult(load, p.numCPU)
	return Check{
		Name:   "load average",
		Status: status,
		Detail: detail,
		Remedy: remedy,
	}
}

func (p *prober) checkContainer(plat Platform) Check {
	if plat.Containerized {
		return Check{
			Name:   "container environment",
			Status: StatusOK,
			Detail: "running inside a container — use --cpuset-cpus and --cpus to pin and cap resources",
		}
	}
	return Check{
		Name:   "container environment",
		Status: StatusOK,
		Detail: "not running in a container — direct host environment",
	}
}
