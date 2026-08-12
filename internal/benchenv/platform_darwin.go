//go:build darwin

package benchenv

import (
	"strings"
)

// detectPlatform gathers macOS machine/process facts.
func (p *prober) detectPlatform() Platform {
	plat := Platform{
		OS:             "darwin",
		Arch:           p.arch,
		Virtualization: "none",
		Translation:    "none",
	}

	// Machine architecture from uname -m.
	if out, err := p.run("uname", "-m"); err == nil {
		raw := strings.TrimSpace(out)
		plat.RawArch = raw
	}

	// Rosetta translation: sysctl proc_translated returns 1 when the
	// current process is running under Rosetta.
	plat.Translation = "none"
	if out, err := p.run("sysctl", "-n", "sysctl.proc_translated"); err == nil {
		plat.Translation = parseRosettaTranslation(out)
	}

	// Apple CPU model.
	plat.CPUModel = p.darwinCPUModel()

	// Power source: AC or battery.
	plat.Power = p.darwinPowerSource()

	// Configured power mode (Low Power / Automatic / High).
	plat.PowerMode = p.darwinPowerMode()

	// Load average.
	if out, err := p.run("sysctl", "-n", "vm.loadavg"); err == nil {
		if load, perr := parseDarwinLoadAvg(strings.TrimSpace(out)); perr == nil {
			plat.LoadAvg = load
		}
	}

	// Thermal warnings via pmset.
	plat.Thermal = p.darwinThermal()

	plat.Evidence = "uname, sysctl proc_translated, pmset"
	return plat
}

// darwinCPUModel returns the Apple CPU model name.
func (p *prober) darwinCPUModel() string {
	out, err := p.run("sysctl", "-n", "machdep.cpu.brand_string")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// darwinPowerSource determines AC vs battery power.
func (p *prober) darwinPowerSource() string {
	out, err := p.run("pmset", "-g", "ps")
	if err != nil {
		return "unknown"
	}
	return parseDarwinPowerSource(out)
}

// darwinPowerMode returns the configured power mode.
func (p *prober) darwinPowerMode() string {
	out, err := p.run("pmset", "-g")
	if err != nil {
		return ""
	}
	return parseDarwinPowerMode(out)
}

// darwinThermal reports thermal pressure if available.
func (p *prober) darwinThermal() string {
	out, err := p.run("pmset", "-g", "therm")
	if err != nil {
		return ""
	}
	return parseDarwinThermal(out)
}

// darwinChecks returns macOS-specific checks.
func (p *prober) platformChecks(plat Platform) []Check {
	var checks []Check

	// One explicit macOS CPU-control limitation check.
	checks = append(checks, Check{
		Name:   "macOS CPU controls",
		Status: StatusUnavailable,
		Detail: "macOS cannot prove or control the physical CPU scheduler, SMT, governor, or boost state — " +
			"benchmark numbers cannot be certified as publication-grade",
		Remedy: "use a Linux bare-metal runner for publication-quality numbers",
	})

	// Rosetta translation warning.
	if plat.Translation == "rosetta" {
		checks = append(checks, Check{
			Name:   "Rosetta translation",
			Status: StatusWarn,
			Detail: "process is running under Rosetta translation — AMD64 binaries are emulated, distorting timing",
			Remedy: "build and run native arm64 binaries; avoid GOARCH=amd64 on Apple Silicon",
		})
	}

	// Power source.
	switch plat.Power {
	case "battery":
		checks = append(checks, Check{
			Name:   "power source",
			Status: StatusWarn,
			Detail: "running on battery power — frequency scaling and thermal management add variance",
			Remedy: "connect to AC power before benchmarking",
		})
	case "ac":
		checks = append(checks, Check{
			Name:   "power source",
			Status: StatusOK,
			Detail: "running on AC power",
		})
	default:
		checks = append(checks, Check{
			Name:   "power source",
			Status: StatusUnavailable,
			Detail: "could not determine power source",
		})
	}

	// Power mode.
	switch plat.PowerMode {
	case "low":
		checks = append(checks, Check{
			Name:   "power mode",
			Status: StatusWarn,
			Detail: "Low Power Mode active — CPU performance is throttled",
			Remedy: "sudo pmset -a lowpowermode 0  # restore Automatic power mode",
		})
	case "automatic":
		checks = append(checks, Check{
			Name:   "power mode",
			Status: StatusOK,
			Detail: "Automatic power mode",
		})
	}

	// Load average.
	status, detail, remedy := loadAvgResult(plat.LoadAvg, p.numCPU)
	checks = append(checks, Check{
		Name:   "load average",
		Status: status,
		Detail: detail,
		Remedy: remedy,
	})

	// Thermal warnings.
	if plat.Thermal != "" {
		checks = append(checks, Check{
			Name:   "thermal pressure",
			Status: StatusWarn,
			Detail: plat.Thermal + " — sustained benchmark runs may be throttled",
			Remedy: "let the machine cool before benchmarking",
		})
	} else {
		checks = append(checks, Check{
			Name:   "thermal pressure",
			Status: StatusOK,
			Detail: "no active thermal throttling reported by pmset",
		})
	}

	return checks
}
