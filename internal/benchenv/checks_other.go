//go:build !linux && !darwin

package benchenv

import "runtime"

// platformChecks returns a stub for platforms not explicitly supported.
func platformChecks() []Check {
	return []Check{
		{
			Name:   "platform-specific checks",
			Status: StatusUnavailable,
			Detail: "platform-specific checks are not implemented for " + runtime.GOOS + "; Linux and macOS are supported",
		},
	}
}
