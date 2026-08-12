package benchenv

import "strings"

// computeReadiness grades the native and Docker execution paths and selects
// the best viable path as the overall recommendation.
func computeReadiness(plat Platform, dkr Docker, checks []Check) Readiness {
	hostNoise := hasHostNoiseWarns(checks)
	nativeGrade := gradeNative(plat, hostNoise)
	dockerGrade := gradeDocker(plat, dkr, hostNoise)
	overall, recommended := bestPath(nativeGrade, dockerGrade)
	return Readiness{
		Overall:         overall,
		RecommendedPath: recommended,
		Native:          nativeGrade,
		Docker:          dockerGrade,
	}
}

// hasHostNoiseWarns reports whether any platform-specific check (excluding
// optional tool availability and runtime info) is in a warn state.
func hasHostNoiseWarns(checks []Check) bool {
	for _, c := range checks {
		if !isHostNoiseCheck(c) {
			continue
		}
		if c.Status == StatusWarn {
			return true
		}
	}
	return false
}

// isHostNoiseCheck reports whether a check is a platform-specific host-noise
// probe rather than an optional tool or runtime info check.
func isHostNoiseCheck(c Check) bool {
	if strings.Contains(c.Name, "installed") {
		return false
	}
	if strings.Contains(c.Name, "GOMAXPROCS") {
		return false
	}
	return true
}

// gradeNative grades the native (non-containerized) benchmarking path.
func gradeNative(plat Platform, hostNoiseWarns bool) PathGrade {
	// Cross-architecture/translation is a fixable hazard.
	if plat.Translation == "rosetta" || plat.Translation == "qemu" {
		return GradeNotReady
	}
	// Fixable host noise hazards (SMT, governor, turbo, load, battery, etc.).
	if hostNoiseWarns {
		return GradeNotReady
	}
	// No fixable hazards. Can we certify?
	if isBareMetalLinux(plat) {
		return GradeReady
	}
	// macOS, VM, other platforms — no fixable blocker but cannot certify.
	return GradeLimited
}

// gradeDocker grades the Docker benchmarking path.
func gradeDocker(plat Platform, dkr Docker, hostNoiseWarns bool) PathGrade {
	if !dkr.Available {
		return GradeUnavailable
	}
	// Remote daemon: cannot bind-mount or run a local probe.
	if !dkr.Local {
		return GradeLimited
	}
	// Cross-architecture execution (QEMU/Rosetta emulation) is a fixable hazard.
	if dkr.Translation == "qemu" {
		return GradeNotReady
	}
	// Failed effective cgroup limits are a fixable hazard.
	if dkr.Isolation != nil {
		if dkr.Isolation.Ran && !dkr.Isolation.Passed {
			return GradeNotReady
		}
		if dkr.Isolation.Error != "" {
			return GradeNotReady
		}
	}
	// Host noise hazards affect the Docker path too.
	if hostNoiseWarns {
		return GradeNotReady
	}
	// No fixable hazards. Can we certify?
	if isBareMetalLinux(plat) && !isVMBackend(dkr.Backend) &&
		dkr.Isolation != nil && dkr.Isolation.Passed {
		return GradeReady
	}
	// VM-backed engine, macOS host, or unknown backend — limited.
	return GradeLimited
}

// isBareMetalLinux reports whether the platform is native bare-metal Linux.
func isBareMetalLinux(plat Platform) bool {
	return plat.OS == "linux" && plat.Virtualization == "none"
}

// isVMBackend reports whether the Docker backend is itself a VM.
func isVMBackend(backend string) bool {
	return backend != "engine"
}

// bestPath selects the highest-graded path, preferring native on ties.
func bestPath(native, docker PathGrade) (PathGrade, string) {
	if gradeRank(native) >= gradeRank(docker) {
		return native, "native"
	}
	return docker, "docker"
}

// gradeRank orders grades from worst (0) to best (3).
func gradeRank(g PathGrade) int {
	switch g {
	case GradeReady:
		return 3
	case GradeLimited:
		return 2
	case GradeNotReady:
		return 1
	case GradeUnavailable:
		return 0
	default:
		return 0
	}
}
