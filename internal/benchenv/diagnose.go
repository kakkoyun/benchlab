package benchenv

// Diagnose runs the full platform-aware diagnostic pipeline and returns a
// Report. It distinguishes the local machine, the running benchenv process,
// the connected Docker engine/VM, and (when reachable) a probe container.
//
// The optional Options allow tests to inject fake command runners and
// filesystems so no host state is touched.
func Diagnose(opts ...Option) Report {
	p := newProber(opts...)

	plat := p.detectPlatform()
	dkr := p.detectDocker(plat)
	checks := p.collectChecks(plat)
	readiness := computeReadiness(plat, dkr, checks)
	actions := buildActions(plat, dkr, readiness, p.numCPU)
	recipes := buildRecipes(plat, dkr, readiness, p.numCPU)

	return Report{
		OS:        plat.OS,
		Arch:      plat.Arch,
		NumCPU:    p.numCPU,
		Checks:    checks,
		Summary:   Summarize(checks),
		Platform:  plat,
		Docker:    dkr,
		Readiness: readiness,
		Actions:   actions,
		Recipes:   recipes,
	}
}

// collectChecks returns platform-specific checks tailored to the detected
// architecture and vendor, followed by cross-platform tool and runtime checks.
// platformChecks is implemented per-OS in platform_*.go.
func (p *prober) collectChecks(plat Platform) []Check {
	var checks []Check
	checks = append(checks, p.platformChecks(plat)...)
	checks = append(checks, toolChecks()...)
	checks = append(checks, runtimeInfoCheck(p.numCPU))
	return checks
}
