package benchenv

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	probeImage   = "busybox:1.37.0"
	probeTimeout = 90 * time.Second
	probeMemoryB = 134217728 // 128 MiB
)

// discoveryScript reads the engine's effective CPU set from inside a container.
const discoveryScript = `if [ -f /sys/fs/cgroup/cpuset.cpus.effective ]; then
  cat /sys/fs/cgroup/cpuset.cpus.effective
elif [ -f /sys/fs/cgroup/cpuset/cpuset.cpus ]; then
  cat /sys/fs/cgroup/cpuset/cpuset.cpus
else
  echo 0
fi`

// probeScript detects the cgroup version and reads the effective isolation
// values from inside the constrained container.
const probeScript = `if [ -f /sys/fs/cgroup/cpuset.cpus.effective ]; then
  echo v2
  cat /sys/fs/cgroup/cpuset.cpus.effective
  cat /sys/fs/cgroup/cpu.max
  cat /sys/fs/cgroup/memory.max
  cat /sys/fs/cgroup/memory.swap.max
elif [ -f /sys/fs/cgroup/cpuset/cpuset.cpus ]; then
  echo v1
  cat /sys/fs/cgroup/cpuset/cpuset.cpus
  cat /sys/fs/cgroup/cpu/cpu.cfs_quota_us
  cat /sys/fs/cgroup/cpu/cpu.cfs_period_us
  cat /sys/fs/cgroup/memory/memory.limit_in_bytes
  cat /sys/fs/cgroup/memory/memory.memsw.limit_in_bytes
else
  echo none
fi`

// runIsolationProbe launches a disposable probe container to verify effective
// Docker cgroup isolation. It never corrupts JSON or aborts execution on
// failure; errors become reportable findings.
func (p *prober) runIsolationProbe(dkr Docker, plat Platform) *IsolationProbe {
	probe := &IsolationProbe{Ran: true}

	// Only probe local daemons.
	if !dkr.Available || !dkr.Local {
		probe.Ran = false
		return probe
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// Phase 1: discovery — determine the engine's effective CPU set.
	discoveryOut, err := p.exec.Run(ctx, "docker", "run", "--rm", "--network=none",
		"--pull=missing", probeImage, "sh", "-c", discoveryScript)
	if err != nil {
		probe.Error = fmt.Sprintf("discovery container failed: %v", err)
		probe.Findings = []Check{{
			Name:   "Docker isolation probe",
			Status: StatusUnavailable,
			Detail: probe.Error,
			Remedy: "ensure the Docker daemon is reachable and can pull busybox:1.37.0",
		}}
		return probe
	}

	cpuset := strings.TrimSpace(discoveryOut)
	selectedCPU, err := firstCPU(cpuset)
	if err != nil {
		probe.Error = fmt.Sprintf("could not parse CPU set %q: %v", cpuset, err)
		probe.Findings = []Check{{
			Name:   "Docker isolation probe",
			Status: StatusUnavailable,
			Detail: probe.Error,
		}}
		return probe
	}
	probe.SelectedCPU = strconv.Itoa(selectedCPU)

	// Phase 2: constrained probe with the selected CPU.
	platformFlag := "linux/" + dkr.EngineArch
	constrainedOut, err := p.exec.Run(ctx, "docker", "run", "--rm", "--network=none",
		"--pull=missing",
		"--platform="+platformFlag,
		"--cpuset-cpus="+probe.SelectedCPU,
		"--cpus=1",
		"--memory=128m",
		"--memory-swap=128m",
		probeImage, "sh", "-c", probeScript)
	if err != nil {
		probe.Error = fmt.Sprintf("constrained probe failed: %v", err)
		probe.Findings = []Check{{
			Name:   "Docker isolation probe",
			Status: StatusWarn,
			Detail: probe.Error,
			Remedy: "docker run --rm --network=none --pull=missing --platform=" + platformFlag +
				" --cpuset-cpus=" + probe.SelectedCPU + " --cpus=1 --memory=128m --memory-swap=128m " + probeImage,
		}}
		return probe
	}

	// Phase 3: verify effective cgroup values.
	lines := strings.Split(strings.TrimSpace(constrainedOut), "\n")
	if len(lines) == 0 {
		probe.Error = "empty probe output"
		return probe
	}

	version := strings.TrimSpace(lines[0])
	probe.CgroupVersion = version

	switch version {
	case "v2":
		p.verifyCgroupV2(probe, lines, selectedCPU)
	case "v1":
		p.verifyCgroupV1(probe, lines, selectedCPU)
	case "none":
		probe.Error = "no cgroup controller found inside the container"
		probe.Findings = []Check{{
			Name:   "Docker isolation probe",
			Status: StatusUnavailable,
			Detail: probe.Error,
			Remedy: "use a Docker engine with cgroup v1 or v2 support",
		}}
	default:
		probe.Error = fmt.Sprintf("unrecognized cgroup version %q", version)
	}

	probe.Passed = len(probe.Findings) == 0 && probe.Error == ""
	return probe
}

// verifyCgroupV2 checks cgroup v2 effective values against the requested limits.
func (p *prober) verifyCgroupV2(probe *IsolationProbe, lines []string, selectedCPU int) {
	if len(lines) < 5 {
		probe.Error = fmt.Sprintf("insufficient cgroup v2 output (%d lines)", len(lines))
		probe.Findings = append(probe.Findings, Check{
			Name: "Docker isolation probe", Status: StatusUnavailable, Detail: probe.Error,
		})
		return
	}

	effCPUSet := strings.TrimSpace(lines[1])
	cpuMax := strings.TrimSpace(lines[2])
	memMax := strings.TrimSpace(lines[3])
	swapMax := strings.TrimSpace(lines[4])

	probe.EffectiveCPUSet = effCPUSet
	probe.CPUQuota = cpuMax
	probe.MemoryMax = memMax
	probe.MemorySwapMax = swapMax

	// Check: selected CPU is in the effective cpuset.
	if !cpuInSet(selectedCPU, effCPUSet) {
		probe.Findings = append(probe.Findings, Check{
			Name:   "cpuset isolation",
			Status: StatusWarn,
			Detail: fmt.Sprintf("selected CPU %d not in effective cpuset %q", selectedCPU, effCPUSet),
			Remedy: fmt.Sprintf("docker run --cpuset-cpus=%d ...", selectedCPU),
		})
	}

	// Check: CPU quota ≤ 1 CPU.
	quota, period, unlimited := parseCPUMaxV2(cpuMax)
	if unlimited {
		probe.Findings = append(probe.Findings, Check{
			Name:   "CPU quota",
			Status: StatusWarn,
			Detail: fmt.Sprintf("cpu.max=%q — quota is unlimited, not capped to 1 CPU", cpuMax),
			Remedy: "docker run --cpus=1 ...",
		})
	} else if period > 0 && float64(quota)/float64(period) > 1.0 {
		probe.Findings = append(probe.Findings, Check{
			Name:   "CPU quota",
			Status: StatusWarn,
			Detail: fmt.Sprintf("cpu.max=%q — quota %.2f CPUs exceeds 1 CPU", cpuMax, float64(quota)/float64(period)),
			Remedy: "docker run --cpus=1 ...",
		})
	}

	// Check: memory max is the expected 128 MiB.
	if memMax != "max" {
		if n, err := strconv.ParseInt(memMax, 10, 64); err == nil && n != probeMemoryB {
			probe.Findings = append(probe.Findings, Check{
				Name:   "memory limit",
				Status: StatusWarn,
				Detail: fmt.Sprintf("memory.max=%s — expected %d (128 MiB)", memMax, probeMemoryB),
				Remedy: "docker run --memory=128m ...",
			})
		}
	} else {
		probe.Findings = append(probe.Findings, Check{
			Name:   "memory limit",
			Status: StatusWarn,
			Detail: "memory.max=max — memory is unlimited, not capped",
			Remedy: "docker run --memory=128m ...",
		})
	}

	// Check: swap is disabled (swap.max == 0).
	swapVal := strings.TrimSpace(swapMax)
	if swapVal != "0" {
		probe.Findings = append(probe.Findings, Check{
			Name:   "swap limit",
			Status: StatusWarn,
			Detail: fmt.Sprintf("memory.swap.max=%q — swap is not disabled (expected 0)", swapMax),
			Remedy: "docker run --memory=128m --memory-swap=128m ...",
		})
	}
}

// verifyCgroupV1 checks cgroup v1 effective values against the requested limits.
func (p *prober) verifyCgroupV1(probe *IsolationProbe, lines []string, selectedCPU int) {
	if len(lines) < 6 {
		probe.Error = fmt.Sprintf("insufficient cgroup v1 output (%d lines)", len(lines))
		probe.Findings = append(probe.Findings, Check{
			Name: "Docker isolation probe", Status: StatusUnavailable, Detail: probe.Error,
		})
		return
	}

	effCPUSet := strings.TrimSpace(lines[1])
	quotaUs := strings.TrimSpace(lines[2])
	periodUs := strings.TrimSpace(lines[3])
	memLimit := strings.TrimSpace(lines[4])
	memswLimit := strings.TrimSpace(lines[5])

	probe.EffectiveCPUSet = effCPUSet
	probe.CPUQuota = quotaUs + "/" + periodUs
	probe.MemoryMax = memLimit
	probe.MemorySwapMax = memswLimit

	// Check: selected CPU is in the effective cpuset.
	if !cpuInSet(selectedCPU, effCPUSet) {
		probe.Findings = append(probe.Findings, Check{
			Name:   "cpuset isolation",
			Status: StatusWarn,
			Detail: fmt.Sprintf("selected CPU %d not in effective cpuset %q", selectedCPU, effCPUSet),
			Remedy: fmt.Sprintf("docker run --cpuset-cpus=%d ...", selectedCPU),
		})
	}

	// Check: CPU quota ≤ 1 CPU.
	quota, qerr := strconv.ParseInt(quotaUs, 10, 64)
	period, perr := strconv.ParseInt(periodUs, 10, 64)
	if qerr != nil || perr != nil {
		probe.Findings = append(probe.Findings, Check{
			Name:   "CPU quota",
			Status: StatusUnavailable,
			Detail: fmt.Sprintf("cannot parse CFS quota/period: %q/%q", quotaUs, periodUs),
		})
	} else if quota < 0 {
		probe.Findings = append(probe.Findings, Check{
			Name:   "CPU quota",
			Status: StatusWarn,
			Detail: fmt.Sprintf("cpu.cfs_quota_us=%d — quota is unlimited, not capped to 1 CPU", quota),
			Remedy: "docker run --cpus=1 ...",
		})
	} else if period > 0 && float64(quota)/float64(period) > 1.0 {
		probe.Findings = append(probe.Findings, Check{
			Name:   "CPU quota",
			Status: StatusWarn,
			Detail: fmt.Sprintf("CFS quota %.2f CPUs exceeds 1 CPU", float64(quota)/float64(period)),
			Remedy: "docker run --cpus=1 ...",
		})
	}

	// Check: memory limit is the expected 128 MiB.
	if n, err := strconv.ParseInt(memLimit, 10, 64); err == nil && n != probeMemoryB {
		probe.Findings = append(probe.Findings, Check{
			Name:   "memory limit",
			Status: StatusWarn,
			Detail: fmt.Sprintf("memory.limit_in_bytes=%s — expected %d (128 MiB)", memLimit, probeMemoryB),
			Remedy: "docker run --memory=128m ...",
		})
	}

	// Check: swap is disabled (memsw limit == memory limit, so swap = 0).
	memLimitVal, _ := strconv.ParseInt(memLimit, 10, 64)
	memswLimitVal, _ := strconv.ParseInt(memswLimit, 10, 64)
	if memswLimitVal != memLimitVal {
		probe.Findings = append(probe.Findings, Check{
			Name:   "swap limit",
			Status: StatusWarn,
			Detail: fmt.Sprintf("memory.memsw.limit_in_bytes=%s != memory.limit_in_bytes=%s — swap is enabled", memswLimit, memLimit),
			Remedy: "docker run --memory=128m --memory-swap=128m ...",
		})
	}
}

// inspectCurrentContainer reads the current cgroup limits when benchenv is
// already running inside a container, and reports which isolation flags are
// absent.
func (p *prober) inspectCurrentContainer() *IsolationProbe {
	probe := &IsolationProbe{Ran: true, CgroupVersion: "current"}

	// Try cgroup v2 first.
	if data, err := p.fs.ReadFile("/sys/fs/cgroup/cpuset.cpus.effective"); err == nil {
		probe.CgroupVersion = "v2"
		probe.EffectiveCPUSet = strings.TrimSpace(data)
		if cpuMax, err := p.fs.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
			probe.CPUQuota = strings.TrimSpace(cpuMax)
		}
		if memMax, err := p.fs.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
			probe.MemoryMax = strings.TrimSpace(memMax)
		}
		if swapMax, err := p.fs.ReadFile("/sys/fs/cgroup/memory.swap.max"); err == nil {
			probe.MemorySwapMax = strings.TrimSpace(swapMax)
		}
		probe.Findings = p.analyzeCurrentContainerV2(probe)
		return probe
	}

	// Fall back to cgroup v1.
	if data, err := p.fs.ReadFile("/sys/fs/cgroup/cpuset/cpuset.cpus"); err == nil {
		probe.CgroupVersion = "v1"
		probe.EffectiveCPUSet = strings.TrimSpace(data)
		if q, err := p.fs.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_quota_us"); err == nil {
			probe.CPUQuota = strings.TrimSpace(q)
		}
		if mem, err := p.fs.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
			probe.MemoryMax = strings.TrimSpace(mem)
		}
		if memsw, err := p.fs.ReadFile("/sys/fs/cgroup/memory/memory.memsw.limit_in_bytes"); err == nil {
			probe.MemorySwapMax = strings.TrimSpace(memsw)
		}
		probe.Findings = p.analyzeCurrentContainerV1(probe)
		return probe
	}

	probe.Error = "no cgroup controller accessible"
	probe.Findings = []Check{{
		Name:   "container cgroup inspection",
		Status: StatusUnavailable,
		Detail: "cannot read cgroup files; isolation state unknown",
	}}
	return probe
}

// analyzeCurrentContainerV2 reports which isolation flags are absent for the
// current cgroup v2 container.
func (p *prober) analyzeCurrentContainerV2(probe *IsolationProbe) []Check {
	var findings []Check

	// CPU quota: "max <period>" means unlimited.
	if probe.CPUQuota != "" {
		_, _, unlimited := parseCPUMaxV2(probe.CPUQuota)
		if unlimited {
			findings = append(findings, Check{
				Name:   "CPU quota",
				Status: StatusWarn,
				Detail: fmt.Sprintf("cpu.max=%q — no CPU quota set; add --cpus=1", probe.CPUQuota),
				Remedy: "restart the container with --cpus=1",
			})
		}
	}

	// Memory: "max" means unlimited.
	if probe.MemoryMax == "max" {
		findings = append(findings, Check{
			Name:   "memory limit",
			Status: StatusWarn,
			Detail: "memory.max=max — no memory limit set; add --memory=128m",
			Remedy: "restart the container with --memory=128m",
		})
	}

	// Swap: non-zero means enabled.
	if probe.MemorySwapMax != "" && probe.MemorySwapMax != "0" {
		findings = append(findings, Check{
			Name:   "swap limit",
			Status: StatusWarn,
			Detail: fmt.Sprintf("memory.swap.max=%q — swap is enabled; add --memory-swap=128m", probe.MemorySwapMax),
			Remedy: "restart the container with --memory-swap=128m",
		})
	}

	// CPU set: if it spans all CPUs, no pinning.
	if probe.EffectiveCPUSet != "" && !looksPinned(probe.EffectiveCPUSet, p.numCPU) {
		findings = append(findings, Check{
			Name:   "cpuset isolation",
			Status: StatusWarn,
			Detail: fmt.Sprintf("effective cpuset=%q spans multiple CPUs; add --cpuset-cpus=<cpu>", probe.EffectiveCPUSet),
			Remedy: "restart the container with --cpuset-cpus=<single-cpu>",
		})
	}

	return findings
}

// analyzeCurrentContainerV1 reports which isolation flags are absent for the
// current cgroup v1 container.
func (p *prober) analyzeCurrentContainerV1(probe *IsolationProbe) []Check {
	var findings []Check

	// CPU quota: -1 means unlimited.
	if probe.CPUQuota != "" {
		if q, err := strconv.ParseInt(probe.CPUQuota, 10, 64); err == nil && q < 0 {
			findings = append(findings, Check{
				Name:   "CPU quota",
				Status: StatusWarn,
				Detail: fmt.Sprintf("cpu.cfs_quota_us=%d — no CPU quota set; add --cpus=1", q),
				Remedy: "restart the container with --cpus=1",
			})
		}
	}

	// Memory: very large value means unlimited.
	if probe.MemoryMax != "" {
		if n, err := strconv.ParseInt(probe.MemoryMax, 10, 64); err == nil && n > 1<<62 {
			findings = append(findings, Check{
				Name:   "memory limit",
				Status: StatusWarn,
				Detail: "memory.limit_in_bytes is unlimited; add --memory=128m",
				Remedy: "restart the container with --memory=128m",
			})
		}
	}

	// Swap: memsw != memory means swap is enabled.
	if probe.MemoryMax != "" && probe.MemorySwapMax != "" {
		if probe.MemorySwapMax != probe.MemoryMax {
			findings = append(findings, Check{
				Name:   "swap limit",
				Status: StatusWarn,
				Detail: "memory.memsw.limit_in_bytes != memory.limit_in_bytes — swap is enabled",
				Remedy: "restart the container with --memory-swap=128m",
			})
		}
	}

	// CPU set.
	if probe.EffectiveCPUSet != "" && !looksPinned(probe.EffectiveCPUSet, p.numCPU) {
		findings = append(findings, Check{
			Name:   "cpuset isolation",
			Status: StatusWarn,
			Detail: fmt.Sprintf("effective cpuset=%q spans multiple CPUs; add --cpuset-cpus=<cpu>", probe.EffectiveCPUSet),
			Remedy: "restart the container with --cpuset-cpus=<single-cpu>",
		})
	}

	return findings
}

// looksPinned reports whether a cpuset string represents a single pinned CPU.
func looksPinned(cpuset string, numCPU int) bool {
	cpuset = strings.TrimSpace(cpuset)
	if cpuset == "" {
		return false
	}
	// A single CPU or a single range of one CPU.
	parts := strings.Split(cpuset, ",")
	if len(parts) != 1 {
		return false
	}
	rangeParts := strings.SplitN(parts[0], "-", 2)
	if len(rangeParts) == 1 {
		return true // single CPU like "3"
	}
	start, _ := strconv.Atoi(rangeParts[0])
	end, _ := strconv.Atoi(rangeParts[1])
	return start == end // range of exactly one CPU
}

// firstCPU parses a cpuset string and returns the first CPU number.
func firstCPU(cpuset string) (int, error) {
	cpuset = strings.TrimSpace(cpuset)
	if cpuset == "" {
		return 0, fmt.Errorf("empty cpuset")
	}
	first := strings.SplitN(cpuset, ",", 2)[0]
	parts := strings.SplitN(first, "-", 2)
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, fmt.Errorf("parse cpu %q: %w", first, err)
	}
	return n, nil
}

// cpuInSet reports whether a CPU number is contained in a cpuset string.
func cpuInSet(cpu int, cpuset string) bool {
	cpuset = strings.TrimSpace(cpuset)
	if cpuset == "" {
		return false
	}
	for _, r := range strings.Split(cpuset, ",") {
		r = strings.TrimSpace(r)
		parts := strings.SplitN(r, "-", 2)
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		end := start
		if len(parts) == 2 {
			if e, err := strconv.Atoi(parts[1]); err == nil {
				end = e
			}
		}
		if cpu >= start && cpu <= end {
			return true
		}
	}
	return false
}

// parseCPUMaxV2 parses a cgroup v2 cpu.max value "quota period" or "max period".
func parseCPUMaxV2(s string) (quota, period int, unlimited bool) {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return 0, 0, true
	}
	if fields[0] == "max" {
		period, _ = strconv.Atoi(fields[1])
		return 0, period, true
	}
	quota, _ = strconv.Atoi(fields[0])
	period, _ = strconv.Atoi(fields[1])
	return quota, period, false
}
