package benchenv

import (
	"runtime"
	"strings"
)

// normalizeArch maps the many architecture spellings produced by uname,
// runtime.GOARCH, and Docker to the canonical forms "arm64" and "amd64".
// Unknown values are returned unchanged rather than guessed.
func normalizeArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	default:
		return arch
	}
}

// goArch returns the normalized runtime architecture.
func goArch() string {
	return normalizeArch(runtime.GOARCH)
}
