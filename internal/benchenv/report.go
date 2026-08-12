package benchenv

// Status represents the health status of a diagnostic check.
type Status string

const (
	StatusOK          Status = "ok"
	StatusWarn        Status = "warn"
	StatusUnavailable Status = "unavailable"
)

// Check is the result of a single diagnostic probe.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Remedy string `json:"remedy,omitempty"`
}

// Summary counts check results by status.
type Summary struct {
	OK          int `json:"ok"`
	Warn        int `json:"warn"`
	Unavailable int `json:"unavailable"`
}

// PathGrade grades an execution path for benchmark readiness.
type PathGrade string

const (
	// GradeReady means publication-grade evidence: native bare-metal Linux,
	// no active critical host-noise findings, native architecture, and—when
	// evaluating the Docker path—a passing isolation probe.
	GradeReady PathGrade = "ready"
	// GradeLimited means no active fixable blocker, but the physical
	// environment cannot be certified (macOS, any VM-backed engine, unknown
	// backend).
	GradeLimited PathGrade = "limited"
	// GradeNotReady means an active fixable hazard: QEMU/cross-architecture
	// execution, missing CPU isolation, enabled noisy CPU controls, high
	// load, Low Power Mode, or failed effective cgroup limits.
	GradeNotReady PathGrade = "not_ready"
	// GradeUnavailable means that execution path cannot be used (no Docker
	// daemon, for example).
	GradeUnavailable PathGrade = "unavailable"
)

// Platform describes the process and machine architecture, CPU model,
// virtualization, translation, power/load facts, and the evidence source.
type Platform struct {
	OS             string  `json:"os"`
	Arch           string  `json:"arch"`               // normalized: arm64 or amd64
	RawArch        string  `json:"raw_arch,omitempty"` // unnormalized uname value
	CPUModel       string  `json:"cpu_model,omitempty"`
	Virtualization string  `json:"virtualization,omitempty"` // none, kvm, qemu, apple, xen, unknown
	Translation    string  `json:"translation,omitempty"`    // none, rosetta, qemu, native
	Containerized  bool    `json:"containerized,omitempty"`
	Power          string  `json:"power,omitempty"` // ac, battery, unknown
	PowerMode      string  `json:"power_mode,omitempty"`
	LoadAvg        float64 `json:"load_avg,omitempty"`
	Thermal        string  `json:"thermal,omitempty"`
	Evidence       string  `json:"evidence,omitempty"`
}

// VMResources reports the resources allocated to a Docker engine VM.
type VMResources struct {
	CPUs   int    `json:"cpus,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// IsolationProbe records the result of the active Docker cgroup probe.
type IsolationProbe struct {
	Ran             bool    `json:"ran"`
	Passed          bool    `json:"passed"`
	CgroupVersion   string  `json:"cgroup_version,omitempty"` // v1 or v2
	SelectedCPU     string  `json:"selected_cpu,omitempty"`
	EffectiveCPUSet string  `json:"effective_cpuset,omitempty"`
	CPUQuota        string  `json:"cpu_quota,omitempty"`
	MemoryMax       string  `json:"memory_max,omitempty"`
	MemorySwapMax   string  `json:"memory_swap_max,omitempty"`
	Findings        []Check `json:"findings,omitempty"`
	Error           string  `json:"error,omitempty"`
}

// Docker describes the Docker engine, its backend, locality, and isolation.
type Docker struct {
	Available      bool            `json:"available"`
	Context        string          `json:"context,omitempty"`
	Endpoint       string          `json:"endpoint,omitempty"`
	Local          bool            `json:"local"`
	EngineOS       string          `json:"engine_os,omitempty"`
	EngineArch     string          `json:"engine_arch,omitempty"` // normalized
	Backend        string          `json:"backend,omitempty"`     // engine, colima-vz, colima-qemu, docker-desktop-*, unknown
	Translation    string          `json:"translation,omitempty"`
	VMResources    VMResources     `json:"vm_resources,omitempty"`
	Isolation      *IsolationProbe `json:"isolation,omitempty"`
	Containerized  bool            `json:"containerized,omitempty"`
	UnavailableMsg string          `json:"unavailable_msg,omitempty"`
}

// Readiness records the overall grade and per-path grades.
type Readiness struct {
	Overall         PathGrade `json:"overall"`
	RecommendedPath string    `json:"recommended_path"`
	Native          PathGrade `json:"native"`
	Docker          PathGrade `json:"docker"`
}

// Action is a single piece of prioritized, deduplicated guidance.
type Action struct {
	Priority int      `json:"priority"`
	Scope    string   `json:"scope"` // platform, docker, tools
	Reason   string   `json:"reason"`
	Commands []string `json:"commands,omitempty"`
}

// Recipes holds complete benchmark commands for each usable path.
type Recipes struct {
	Native string `json:"native,omitempty"`
	Docker string `json:"docker,omitempty"`
}

// Report is the top-level structure for JSON output. The legacy fields
// (OS, Arch, NumCPU, Checks, Summary) are preserved; the additive fields
// (Platform, Docker, Readiness, Actions, Recipes) carry the context-aware
// diagnostics.
type Report struct {
	OS        string    `json:"os"`
	Arch      string    `json:"arch"` // normalized
	NumCPU    int       `json:"numcpu"`
	Checks    []Check   `json:"checks"`
	Summary   Summary   `json:"summary"`
	Platform  Platform  `json:"platform"`
	Docker    Docker    `json:"docker"`
	Readiness Readiness `json:"readiness"`
	Actions   []Action  `json:"actions"`
	Recipes   Recipes   `json:"recipes"`
}

// Summarize counts checks by status.
func Summarize(checks []Check) Summary {
	var summary Summary
	for _, check := range checks {
		switch check.Status {
		case StatusOK:
			summary.OK++
		case StatusWarn:
			summary.Warn++
		case StatusUnavailable:
			summary.Unavailable++
		}
	}
	return summary
}
