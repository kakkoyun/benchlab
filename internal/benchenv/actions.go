package benchenv

import (
	"fmt"
	"strings"
)

// buildActions generates ordered, deduplicated guidance with scope, reason,
// and copy-paste commands. Actions are ordered by impact.
func buildActions(plat Platform, dkr Docker, readiness Readiness, numCPU int) []Action {
	var actions []Action
	seen := make(map[string]bool)

	add := func(priority int, scope, reason string, cmds ...string) {
		if seen[reason] {
			return
		}
		seen[reason] = true
		actions = append(actions, Action{
			Priority: priority,
			Scope:    scope,
			Reason:   reason,
			Commands: cmds,
		})
	}

	// Priority 1: Remove translation / QEMU emulation.
	if plat.Translation == "rosetta" {
		add(1, "platform",
			"Rosetta translation distorts timing — build and run native arm64 binaries",
			"GOARCH=arm64 go build ./...",
			"GOARCH=arm64 go test -bench=. -count=10 ./...")
	}
	if dkr.Available && dkr.Translation == "qemu" {
		add(1, "docker",
			"Docker engine architecture differs from host — containers run under QEMU emulation",
			colimaBenchProfileCmd(),
			"docker context use colima-benchlab")
	}

	// Priority 2: Move to certifiable hardware.
	if plat.OS == "darwin" {
		add(2, "platform",
			"macOS cannot control or certify the physical CPU scheduler, SMT, governor, or boost",
			"# Use a Linux bare-metal CI runner for publication-quality numbers")
	}
	if plat.OS == "linux" && plat.Virtualization != "none" && plat.Virtualization != "" {
		add(2, "platform",
			fmt.Sprintf("running in a %s VM — move to bare-metal Linux for publication-grade benchmarks", plat.Virtualization))
	}
	if dkr.Available && isVMBackend(dkr.Backend) && dkr.Backend != "unknown" {
		add(2, "docker",
			fmt.Sprintf("Docker backend %q is VM-backed — VM vCPU pinning is not physical-core pinning", dkr.Backend),
			"# Use a native Docker Engine on bare-metal Linux for publication-grade Docker benchmarks")
	}
	if dkr.Available && dkr.Backend == "unknown" {
		add(2, "docker",
			"Docker backend is unknown — cannot certify isolation quality",
			"# Identify the Docker backend or switch to a known one (Docker Engine, Colima, Docker Desktop)")
	}
	if dkr.Available && strings.HasPrefix(dkr.Backend, "docker-desktop") {
		add(2, "docker",
			"Docker Desktop backend — change the virtualization framework in Settings > General",
			"open -a Docker  # then Settings > General > Use Virtualization framework")
	}
	if dkr.Available && !dkr.Local {
		add(2, "docker",
			"remote Docker daemon — bind-mount sources must live on the daemon host, not the client",
			"# Copy benchmark sources to the daemon host or use a local daemon")
	}

	// Priority 3: Stabilize CPU controls (Linux bare-metal).
	if plat.OS == "linux" {
		addCPUControlActions(plat, add)
	}

	// Priority 4: Enforce Docker isolation.
	if dkr.Available && dkr.Local {
		addDockerIsolationActions(dkr, add)
	}

	// Priority 5: Reduce power / load / thermal noise.
	addNoiseActions(plat, numCPU, add)

	// Priority 6: Install optional analysis tools.
	addToolActions(add)

	// Sort by priority (stable to preserve insertion order within a priority).
	sortActions(actions)
	return actions
}

// addCPUControlActions adds Linux CPU control stabilization actions.
func addCPUControlActions(plat Platform, add func(int, string, string, ...string)) {
	if plat.Arch != "arm64" {
		// x86: SMT, governor, turbo.
		add(3, "platform",
			"disable SMT — sibling threads share execution units, causing high variance",
			"echo off | sudo tee /sys/devices/system/cpu/smt/control")
		add(3, "platform",
			"set performance governor — disable dynamic frequency scaling",
			"echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor")
		add(3, "platform",
			"disable Turbo Boost — stabilize CPU frequency at base clock",
			"echo 1 | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo  # Intel",
			"echo 0 | sudo tee /sys/devices/system/cpu/cpufreq/boost           # AMD")
	} else {
		// ARM64: governor and boost (no Intel/AMD pstate).
		add(3, "platform",
			"set performance governor if available — disable dynamic frequency scaling",
			"echo performance | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor")
	}
}

// addDockerIsolationActions adds Docker isolation enforcement actions.
func addDockerIsolationActions(dkr Docker, add func(int, string, string, ...string)) {
	if dkr.Isolation == nil {
		return
	}
	if !dkr.Isolation.Ran {
		return
	}
	if dkr.Isolation.Passed {
		return // no action needed
	}
	// Probe failed or found issues.
	cpu := dkr.Isolation.SelectedCPU
	if cpu == "" {
		cpu = "0"
	}
	arch := dkr.EngineArch
	if arch == "" {
		arch = "arm64"
	}
	add(4, "docker",
		"Docker cgroup isolation not effective — verify and enforce resource limits",
		fmt.Sprintf("docker run --rm --network=none --platform=linux/%s --cpuset-cpus=%s --cpus=1 --memory=512m --memory-swap=512m <image>", arch, cpu))
	if dkr.Containerized {
		add(4, "docker",
			"benchenv is inside a container without full isolation flags — restart with resource limits",
			fmt.Sprintf("docker run --cpuset-cpus=%s --cpus=1 --memory=512m --memory-swap=512m <image>", cpu))
	}
}

// addNoiseActions adds power, load, and thermal noise reduction actions.
func addNoiseActions(plat Platform, numCPU int, add func(int, string, string, ...string)) {
	if plat.OS == "darwin" {
		if plat.Power == "battery" {
			add(5, "platform",
				"running on battery power — frequency scaling and thermal management add variance",
				"# Connect to AC power before benchmarking")
		}
		if plat.PowerMode == "low" {
			add(5, "platform",
				"Low Power Mode active — CPU performance is throttled",
				"sudo pmset -a lowpowermode 0  # restore Automatic power mode")
		}
		if plat.Thermal != "" {
			add(5, "platform",
				"thermal throttling detected — let the machine cool before benchmarking",
				"# Wait for the machine to cool and re-run")
		}
	}
	if plat.LoadAvg > 0 {
		// High load is handled by the check, but add a general action if load is high.
		threshold := float64(numCPU) * 0.5
		if plat.LoadAvg > threshold {
			add(5, "platform",
				fmt.Sprintf("1-min load %.2f exceeds %.1f — close background applications", plat.LoadAvg, threshold),
				"# Close browser, IDE, Slack, and other CPU-hungry processes")
		}
	}
}

// addToolActions adds optional analysis tool installation actions.
func addToolActions(add func(int, string, string, ...string)) {
	add(6, "tools",
		"install perflock to prevent CPU frequency changes during benchmark runs (Linux)",
		"go install github.com/aclements/perflock@latest")
	add(6, "tools",
		"install benchstat for statistical A/B comparison of benchmark results",
		"go install golang.org/x/perf/cmd/benchstat@latest")
	add(6, "tools",
		"install benchdiff to automate the git stash/run/compare cycle",
		"go install github.com/willabides/benchdiff/cmd/benchdiff@latest")
}

// colimaBenchProfileCmd returns the non-destructive Colima benchmark profile command.
func colimaBenchProfileCmd() string {
	return "colima start --profile benchlab --arch aarch64 --vm-type vz --cpu 4 --memory 8"
}

// sortActions sorts actions by priority using a stable insertion sort.
func sortActions(actions []Action) {
	for i := 1; i < len(actions); i++ {
		for j := i; j > 0 && actions[j-1].Priority > actions[j].Priority; j-- {
			actions[j-1], actions[j] = actions[j], actions[j-1]
		}
	}
}

// formatArch returns the platform architecture for display.
func formatArch(plat Platform) string {
	if plat.RawArch != "" && plat.RawArch != plat.Arch {
		return plat.Arch + " (uname: " + plat.RawArch + ")"
	}
	return plat.Arch
}

// formatBackend returns a human-readable Docker backend label.
func formatBackend(dkr Docker) string {
	if !dkr.Available {
		return "unavailable"
	}
	if dkr.Backend == "" {
		return "unknown"
	}
	return dkr.Backend
}

// formatTranslation returns a human-readable translation label.
func formatTranslation(t string) string {
	if t == "" || t == "none" {
		return "native"
	}
	return t
}

// formatVirt returns a human-readable virtualization label.
func formatVirt(v string) string {
	if v == "" || v == "none" {
		return "bare metal"
	}
	return v
}
