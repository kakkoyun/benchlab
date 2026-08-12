package benchenv

import (
	"errors"
	"testing"
)

func TestFirstCPU(t *testing.T) {
	tests := []struct {
		cpuset  string
		want    int
		wantErr bool
	}{
		{"0", 0, false},
		{"0-3", 0, false},
		{"0,2,4", 0, false},
		{"2-4", 2, false},
		{"2,4,6-8", 2, false},
		{"  3  ", 3, false},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.cpuset, func(t *testing.T) {
			got, err := firstCPU(tt.cpuset)
			if (err != nil) != tt.wantErr {
				t.Errorf("firstCPU(%q) error = %v, wantErr %v", tt.cpuset, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("firstCPU(%q) = %d, want %d", tt.cpuset, got, tt.want)
			}
		})
	}
}

func TestCPUInSet(t *testing.T) {
	tests := []struct {
		cpu  int
		set  string
		want bool
	}{
		{0, "0", true},
		{0, "0-3", true},
		{3, "0-3", true},
		{4, "0-3", false},
		{2, "0,2,4", true},
		{1, "0,2,4", false},
		{8, "0-3,6,8-10", true},
		{5, "0-3,6,8-10", false},
		{0, "", false},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if got := cpuInSet(tt.cpu, tt.set); got != tt.want {
				t.Errorf("cpuInSet(%d, %q) = %v, want %v", tt.cpu, tt.set, got, tt.want)
			}
		})
	}
}

func TestParseCPUMaxV2(t *testing.T) {
	tests := []struct {
		s         string
		quota     int
		period    int
		unlimited bool
	}{
		{"100000 100000", 100000, 100000, false},
		{"50000 100000", 50000, 100000, false},
		{"200000 100000", 200000, 100000, false},
		{"max 100000", 0, 100000, true},
		{"garbage", 0, 0, true},
		{"", 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			q, p, u := parseCPUMaxV2(tt.s)
			if q != tt.quota || p != tt.period || u != tt.unlimited {
				t.Errorf("parseCPUMaxV2(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.s, q, p, u, tt.quota, tt.period, tt.unlimited)
			}
		})
	}
}

func TestLooksPinned(t *testing.T) {
	tests := []struct {
		cpuset string
		want   bool
	}{
		{"0", true},
		{"3", true},
		{"0-0", true},
		{"0-3", false},
		{"0,2", false},
		{"", false},
		{"0-1", false},
	}
	for _, tt := range tests {
		t.Run(tt.cpuset, func(t *testing.T) {
			if got := looksPinned(tt.cpuset, 8); got != tt.want {
				t.Errorf("looksPinned(%q) = %v, want %v", tt.cpuset, got, tt.want)
			}
		})
	}
}

// TestRunIsolationProbeV2Passing verifies a passing cgroup v2 probe.
func TestRunIsolationProbeV2Passing(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			// Discovery returns the CPU set.
			if hasPrefix(args, "run") && len(args) > 0 && args[0] == "run" {
				// Check if this is the discovery (no --cpuset-cpus) or constrained probe.
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
			}
			return "", errors.New("unexpected")
		},
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("linux"), withArch("arm64"), withNumCPU(8))
	dkr := Docker{Available: true, Local: true, EngineArch: "arm64"}
	probe := p.runIsolationProbe(dkr, Platform{OS: "linux", Arch: "arm64"})

	if !probe.Ran {
		t.Fatal("expected probe to run")
	}
	if !probe.Passed {
		t.Errorf("expected probe to pass, got findings: %v", probe.Findings)
	}
	if probe.CgroupVersion != "v2" {
		t.Errorf("cgroup version = %q, want v2", probe.CgroupVersion)
	}
	if probe.SelectedCPU != "0" {
		t.Errorf("selected CPU = %q, want 0", probe.SelectedCPU)
	}
}

// TestRunIsolationProbeV2FailingMemory verifies a failing cgroup v2 probe
// (memory not capped).
func TestRunIsolationProbeV2FailingMemory(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			constrained := false
			for _, a := range args {
				if a == "--cpuset-cpus=0" {
					constrained = true
				}
			}
			if !constrained {
				return "0-7\n", nil
			}
			// memory.max = max (unlimited)
			return "v2\n0\n100000 100000\nmax\n0\n", nil
		},
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("linux"), withArch("arm64"), withNumCPU(8))
	dkr := Docker{Available: true, Local: true, EngineArch: "arm64"}
	probe := p.runIsolationProbe(dkr, Platform{OS: "linux", Arch: "arm64"})

	if probe.Passed {
		t.Error("expected probe to fail")
	}
	found := false
	for _, f := range probe.Findings {
		if f.Name == "memory limit" {
			found = true
		}
	}
	if !found {
		t.Error("expected memory limit finding")
	}
}

// TestRunIsolationProbeV2FailingSwap verifies a failing cgroup v2 probe
// (swap not disabled).
func TestRunIsolationProbeV2FailingSwap(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			constrained := false
			for _, a := range args {
				if a == "--cpuset-cpus=0" {
					constrained = true
				}
			}
			if !constrained {
				return "0-7\n", nil
			}
			// memory.swap.max = 134217728 (not 0, swap enabled)
			return "v2\n0\n100000 100000\n134217728\n134217728\n", nil
		},
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("linux"), withArch("arm64"), withNumCPU(8))
	dkr := Docker{Available: true, Local: true, EngineArch: "arm64"}
	probe := p.runIsolationProbe(dkr, Platform{OS: "linux", Arch: "arm64"})

	if probe.Passed {
		t.Error("expected probe to fail")
	}
	found := false
	for _, f := range probe.Findings {
		if f.Name == "swap limit" {
			found = true
		}
	}
	if !found {
		t.Error("expected swap limit finding")
	}
}

// TestRunIsolationProbeV1Passing verifies a passing cgroup v1 probe.
func TestRunIsolationProbeV1Passing(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			constrained := false
			for _, a := range args {
				if a == "--cpuset-cpus=0" {
					constrained = true
				}
			}
			if !constrained {
				return "0-3\n", nil
			}
			return "v1\n0\n100000\n100000\n134217728\n134217728\n", nil
		},
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("linux"), withArch("amd64"), withNumCPU(4))
	dkr := Docker{Available: true, Local: true, EngineArch: "amd64"}
	probe := p.runIsolationProbe(dkr, Platform{OS: "linux", Arch: "amd64"})

	if !probe.Passed {
		t.Errorf("expected probe to pass, got: %v", probe.Findings)
	}
	if probe.CgroupVersion != "v1" {
		t.Errorf("cgroup version = %q, want v1", probe.CgroupVersion)
	}
}

// TestRunIsolationProbeV1FailingQuota verifies a failing cgroup v1 probe
// (unlimited quota).
func TestRunIsolationProbeV1FailingQuota(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			constrained := false
			for _, a := range args {
				if a == "--cpuset-cpus=0" {
					constrained = true
				}
			}
			if !constrained {
				return "0-3\n", nil
			}
			// cpu.cfs_quota_us = -1 (unlimited)
			return "v1\n0\n-1\n100000\n134217728\n134217728\n", nil
		},
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("linux"), withArch("amd64"), withNumCPU(4))
	dkr := Docker{Available: true, Local: true, EngineArch: "amd64"}
	probe := p.runIsolationProbe(dkr, Platform{OS: "linux", Arch: "amd64"})

	if probe.Passed {
		t.Error("expected probe to fail")
	}
	found := false
	for _, f := range probe.Findings {
		if f.Name == "CPU quota" {
			found = true
		}
	}
	if !found {
		t.Error("expected CPU quota finding")
	}
}

// TestRunIsolationProbeDiscoveryFailure verifies a discovery failure is
// reported as a finding, not a crash.
func TestRunIsolationProbeDiscoveryFailure(t *testing.T) {
	exec := &fakeExec{
		runFn: func(name string, args ...string) (string, error) {
			return "", errors.New("Cannot connect to the Docker daemon")
		},
	}
	p := newProber(withExec(exec), withFS(&fakeFS{}), withOS("linux"), withArch("arm64"), withNumCPU(8))
	dkr := Docker{Available: true, Local: true, EngineArch: "arm64"}
	probe := p.runIsolationProbe(dkr, Platform{OS: "linux", Arch: "arm64"})

	if probe.Passed {
		t.Error("expected probe to fail")
	}
	if probe.Error == "" {
		t.Error("expected error message")
	}
}

// TestInspectCurrentContainerV2 verifies cgroup v2 current-container inspection.
func TestInspectCurrentContainerV2(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/sys/fs/cgroup/cpuset.cpus.effective": "0-7\n",
		"/sys/fs/cgroup/cpu.max":               "max 100000\n",
		"/sys/fs/cgroup/memory.max":            "max\n",
		"/sys/fs/cgroup/memory.swap.max":       "0\n",
	}}
	p := newProber(withFS(fs), withOS("linux"), withArch("arm64"), withNumCPU(8))
	probe := p.inspectCurrentContainer()

	if probe.CgroupVersion != "v2" {
		t.Errorf("cgroup version = %q, want v2", probe.CgroupVersion)
	}
	// Should find: CPU quota unlimited, memory unlimited, cpuset not pinned.
	hasCPUQuota := false
	hasMemory := false
	hasCPUSet := false
	for _, f := range probe.Findings {
		switch f.Name {
		case "CPU quota":
			hasCPUQuota = true
		case "memory limit":
			hasMemory = true
		case "cpuset isolation":
			hasCPUSet = true
		}
	}
	if !hasCPUQuota {
		t.Error("expected CPU quota finding")
	}
	if !hasMemory {
		t.Error("expected memory limit finding")
	}
	if !hasCPUSet {
		t.Error("expected cpuset isolation finding (0-7 spans all CPUs)")
	}
}

// TestInspectCurrentContainerV1 verifies cgroup v1 current-container inspection.
func TestInspectCurrentContainerV1(t *testing.T) {
	fs := &fakeFS{files: map[string]string{
		"/sys/fs/cgroup/cpuset/cpuset.cpus":                 "0\n",
		"/sys/fs/cgroup/cpu/cpu.cfs_quota_us":               "100000\n",
		"/sys/fs/cgroup/memory/memory.limit_in_bytes":       "134217728\n",
		"/sys/fs/cgroup/memory/memory.memsw.limit_in_bytes": "134217728\n",
	}}
	p := newProber(withFS(fs), withOS("linux"), withArch("amd64"), withNumCPU(4))
	probe := p.inspectCurrentContainer()

	if probe.CgroupVersion != "v1" {
		t.Errorf("cgroup version = %q, want v1", probe.CgroupVersion)
	}
	// Pinned to CPU 0, quota set, memory set, swap disabled → no findings.
	if len(probe.Findings) != 0 {
		t.Errorf("expected no findings, got %d: %v", len(probe.Findings), probe.Findings)
	}
}
