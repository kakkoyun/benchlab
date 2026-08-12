//go:build !linux && !darwin

package benchenv

// detectPlatform provides architecture/runtime facts for unsupported
// platforms and marks native benchmarking as limited.
func (p *prober) detectPlatform() Platform {
	plat := Platform{
		OS:             p.os,
		Arch:           p.arch,
		Virtualization: "unknown",
		Translation:    "none",
		Evidence:       "runtime only; platform-specific probes not implemented for " + p.os,
	}
	return plat
}

// otherChecks returns checks for platforms without explicit support.
func (p *prober) platformChecks(plat Platform) []Check {
	return []Check{
		{
			Name:   "platform-specific checks",
			Status: StatusUnavailable,
			Detail: "platform-specific checks are not implemented for " + p.os + "; Linux and macOS are supported",
			Remedy: "use a native Linux bare-metal runner for publication-quality numbers",
		},
	}
}
