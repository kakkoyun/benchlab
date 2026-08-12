package benchenv

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// CollectChecks gathers all checks: platform-specific first, then cross-platform tools, then runtime info.
func CollectChecks() []Check {
	var checks []Check
	checks = append(checks, platformChecks()...)
	checks = append(checks, toolChecks()...)
	checks = append(checks, runtimeInfoCheck())
	return checks
}

// toolChecks returns cross-platform tool availability checks.
func toolChecks() []Check {
	return []Check{
		checkTool("perflock", "go install github.com/aclements/perflock@latest"),
		checkTool("benchstat", "go install golang.org/x/perf/cmd/benchstat@latest"),
		checkTool("benchdiff", "go install github.com/willabides/benchdiff/cmd/benchdiff@latest"),
	}
}

// checkTool checks whether a named binary is on PATH.
func checkTool(name, remedy string) Check {
	if _, err := exec.LookPath(name); err == nil {
		return Check{
			Name:   name + " installed",
			Status: StatusOK,
			Detail: name + " found on PATH",
		}
	}
	return Check{
		Name:   name + " not installed",
		Status: StatusWarn,
		Detail: name + " not found on PATH",
		Remedy: remedy,
	}
}

// runtimeInfoCheck reports GOMAXPROCS and NumCPU as an informational check.
func runtimeInfoCheck() Check {
	numCPU := runtime.NumCPU()
	maxProcs := runtime.GOMAXPROCS(0)
	detail := fmt.Sprintf("NumCPU=%d GOMAXPROCS=%d", numCPU, maxProcs)
	if maxProcs < numCPU {
		detail += fmt.Sprintf(
			" — GOMAXPROCS (%d) < NumCPU (%d); benchmarks use fewer CPUs than available",
			maxProcs, numCPU,
		)
	}
	return Check{
		Name:   "GOMAXPROCS / NumCPU",
		Status: StatusOK,
		Detail: detail,
	}
}

// --- Pure mapping functions (no syscalls; fully testable on any OS) ---

// smtResult maps a /sys/devices/system/cpu/smt/control value to a check result.
func smtResult(value string) (Status, string, string) {
	switch value {
	case "off":
		return StatusOK, "SMT disabled — ~100× CV improvement on CPU-bound benchmarks", ""
	case "forceoff":
		return StatusOK, "SMT force-disabled by firmware", ""
	case "notsupported":
		return StatusUnavailable, "CPU does not support SMT", ""
	case "on":
		return StatusWarn,
			"SMT enabled — sibling threads share execution units, causing high variance (CV ~24% observed)",
			"echo off | sudo tee /sys/devices/system/cpu/smt/control"
	default:
		return StatusWarn,
			fmt.Sprintf("unexpected SMT control value %q", value),
			"echo off | sudo tee /sys/devices/system/cpu/smt/control"
	}
}

// governorResult maps a scaling_governor value to a check result.
func governorResult(value string) (Status, string, string) {
	if value == "performance" {
		return StatusOK, "governor=performance — dynamic frequency scaling disabled", ""
	}
	return StatusWarn,
		fmt.Sprintf("governor=%q — dynamic frequency scaling active; adds ~10× CV vs performance governor", value),
		"echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor"
}

// turboIntelResult maps the intel_pstate/no_turbo sysfs value to a check result.
// 1 = turbo disabled (ok), 0 = turbo enabled (warn).
func turboIntelResult(value string) (Status, string, string) {
	switch value {
	case "1":
		return StatusOK, "Intel Turbo Boost disabled — CPU frequency stable at base clock", ""
	case "0":
		return StatusWarn,
			"Intel Turbo Boost enabled — frequency spikes inflate short benchmark runs",
			"echo 1 | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo"
	default:
		return StatusWarn,
			fmt.Sprintf("unexpected no_turbo value %q", value),
			"echo 1 | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo"
	}
}

// turboAMDResult maps the cpufreq/boost sysfs value to a check result.
// 0 = boost disabled (ok), 1 = boost enabled (warn).
func turboAMDResult(value string) (Status, string, string) {
	switch value {
	case "0":
		return StatusOK, "AMD boost disabled — CPU frequency stable at base clock", ""
	case "1":
		return StatusWarn,
			"AMD boost enabled — frequency spikes inflate short benchmark runs",
			"echo 0 | sudo tee /sys/devices/system/cpu/cpufreq/boost"
	default:
		return StatusWarn,
			fmt.Sprintf("unexpected AMD boost value %q", value),
			"echo 0 | sudo tee /sys/devices/system/cpu/cpufreq/boost"
	}
}

// parseLoadAvg extracts the 1-minute load average from /proc/loadavg content.
func parseLoadAvg(content string) (float64, error) {
	fields := strings.Fields(content)
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/loadavg format: %q", content)
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse 1-min load average %q: %w", fields[0], err)
	}
	return v, nil
}

// parseDarwinLoadAvg parses the output of "sysctl -n vm.loadavg", e.g. "{ 1.23 2.34 3.45 }".
func parseDarwinLoadAvg(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	s = strings.TrimSpace(s)
	fields := strings.Fields(s)
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected vm.loadavg format: %q", s)
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse 1-min load average %q: %w", fields[0], err)
	}
	return v, nil
}

// loadAvgResult maps a load average value to a check result given the CPU count.
func loadAvgResult(loadAvg float64, numCPU int) (Status, string, string) {
	threshold := float64(numCPU) * 0.5
	if loadAvg > threshold {
		return StatusWarn,
			fmt.Sprintf("1-min load %.2f > %.1f (numCPU×0.5) — competing workloads add scheduling noise", loadAvg, threshold),
			"close background applications before benchmarking"
	}
	return StatusOK,
		fmt.Sprintf("1-min load %.2f ≤ %.1f — low background load", loadAvg, threshold),
		""
}
