package benchenv

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// dockerContextEndpoint is the host endpoint for a Docker context.
type dockerContextEndpoint struct {
	Host string `json:"Host"`
}

// dockerContext is the subset of `docker context inspect` output we need.
type dockerContext struct {
	Name      string                           `json:"Name"`
	Endpoints map[string]dockerContextEndpoint `json:"Endpoints"`
}

// dockerInfo holds the parsed `docker info` fields.
type dockerInfo struct {
	OSType          string
	Architecture    string
	OperatingSystem string
	NCPU            int
	MemTotal        int64
}

// colimaStatus holds the parsed `colima status --json` output.
type colimaStatus struct {
	Driver  string `json:"driver"`
	Arch    string `json:"arch"`
	CPU     int    `json:"cpu"`
	Memory  int    `json:"memory"`
	Runtime string `json:"runtime"`
	Status  string `json:"status"`
}

// detectDocker inspects the active Docker context and daemon. Docker absence
// or an unreachable daemon becomes an unavailable Docker path, not a fatal
// program error.
func (p *prober) detectDocker(plat Platform) Docker {
	dkr := Docker{}

	// If benchenv is inside a container, inspect its current cgroup limits
	// regardless of Docker CLI availability — no nested probe is launched.
	if plat.Containerized {
		dkr.Containerized = true
		dkr.Isolation = p.inspectCurrentContainer()
	}

	// Docker CLI availability.
	if err := p.exec.LookPath("docker"); err != nil {
		dkr.Available = false
		dkr.UnavailableMsg = "docker CLI not found on PATH"
		return dkr
	}

	// Daemon reachability.
	info, err := p.dockerEngineInfo()
	if err != nil {
		dkr.Available = false
		dkr.UnavailableMsg = fmt.Sprintf("docker daemon unreachable: %v", err)
		return dkr
	}
	dkr.Available = true
	dkr.EngineOS = info.OSType
	dkr.EngineArch = normalizeArch(info.Architecture)

	// Context and endpoint.
	ctxName, endpoint := p.dockerContextInfo()
	dkr.Context = ctxName
	dkr.Endpoint = endpoint
	dkr.Local = isLocalEndpoint(endpoint)

	// Backend detection.
	dkr.Backend, dkr.VMResources, dkr.Translation = p.detectBackend(ctxName, endpoint, info, plat)

	// Run the active isolation probe when not containerized and the daemon
	// is local. Containerized inspection was already handled above.
	if !plat.Containerized && dkr.Available && dkr.Local {
		dkr.Isolation = p.runIsolationProbe(dkr, plat)
	}

	return dkr
}

// dockerContextInfo returns the current context name and endpoint host.
func (p *prober) dockerContextInfo() (name, endpoint string) {
	if out, err := p.run("docker", "context", "show"); err == nil {
		name = strings.TrimSpace(out)
	}
	if out, err := p.run("docker", "context", "inspect"); err == nil {
		var ctxs []dockerContext
		if jerr := json.Unmarshal([]byte(out), &ctxs); jerr == nil && len(ctxs) > 0 {
			if name == "" {
				name = ctxs[0].Name
			}
			if ep, ok := ctxs[0].Endpoints["docker"]; ok {
				endpoint = ep.Host
			}
		}
	}
	return name, endpoint
}

// dockerEngineInfo parses key fields from `docker info`.
func (p *prober) dockerEngineInfo() (dockerInfo, error) {
	out, err := p.run("docker", "info", "--format",
		"{{.OSType}}\n{{.Architecture}}\n{{.OperatingSystem}}\n{{.NCPU}}\n{{.MemTotal}}")
	if err != nil {
		return dockerInfo{}, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	info := dockerInfo{}
	if len(lines) >= 1 {
		info.OSType = strings.TrimSpace(lines[0])
	}
	if len(lines) >= 2 {
		info.Architecture = strings.TrimSpace(lines[1])
	}
	if len(lines) >= 3 {
		info.OperatingSystem = strings.TrimSpace(lines[2])
	}
	if len(lines) >= 4 {
		info.NCPU, _ = strconv.Atoi(strings.TrimSpace(lines[3]))
	}
	if len(lines) >= 5 {
		info.MemTotal, _ = strconv.ParseInt(strings.TrimSpace(lines[4]), 10, 64)
	}
	return info, nil
}

// isLocalEndpoint reports whether a Docker endpoint points to a local daemon.
func isLocalEndpoint(endpoint string) bool {
	if endpoint == "" {
		return true // default unix socket
	}
	lower := strings.ToLower(endpoint)
	switch {
	case strings.HasPrefix(lower, "unix://"):
		return true
	case strings.Contains(lower, "localhost"), strings.Contains(lower, "127.0.0.1"):
		return true
	default:
		return false
	}
}

// detectBackend identifies the Docker backend using evidence in priority order.
func (p *prober) detectBackend(ctxName, endpoint string, info dockerInfo, plat Platform) (backend string, vm VMResources, translation string) {
	// Colima: context name or socket path contains "colima".
	if isColima(ctxName, endpoint) {
		return p.detectColimaBackend(info, plat)
	}

	// Docker Desktop: context name or OS string indicates it.
	if isDockerDesktop(ctxName, info.OperatingSystem) {
		return p.detectDockerDesktopBackend(info, plat)
	}

	// Native Docker Engine on Linux.
	if p.os == "linux" && info.OSType == "linux" {
		vm = VMResources{CPUs: info.NCPU}
		translation = computeDockerTranslation(info.Architecture, plat.Arch)
		return "engine", vm, translation
	}

	// Unknown backend.
	vm = VMResources{CPUs: info.NCPU}
	translation = computeDockerTranslation(info.Architecture, plat.Arch)
	return "unknown", vm, translation
}

// isColima reports whether the Docker context or endpoint indicates Colima.
func isColima(ctxName, endpoint string) bool {
	if strings.HasPrefix(strings.ToLower(ctxName), "colima") {
		return true
	}
	if strings.Contains(strings.ToLower(endpoint), "colima") {
		return true
	}
	return false
}

// isDockerDesktop reports whether the context or OS string indicates Docker Desktop.
func isDockerDesktop(ctxName, osString string) bool {
	lower := strings.ToLower(osString)
	if strings.Contains(lower, "docker desktop") {
		return true
	}
	switch strings.ToLower(ctxName) {
	case "desktop-linux", "desktop":
		return true
	}
	return false
}

// detectColimaBackend uses `colima status --json` to identify the driver and arch.
func (p *prober) detectColimaBackend(info dockerInfo, plat Platform) (string, VMResources, string) {
	vm := VMResources{CPUs: info.NCPU}
	if info.MemTotal > 0 {
		vm.Memory = formatBytes(info.MemTotal)
	}

	if err := p.exec.LookPath("colima"); err != nil {
		// Colima not on PATH; infer from engine arch.
		translation := computeDockerTranslation(info.Architecture, plat.Arch)
		return "colima-unknown", vm, translation
	}

	out, err := p.run("colima", "status", "--json")
	if err != nil {
		translation := computeDockerTranslation(info.Architecture, plat.Arch)
		return "colima-unknown", vm, translation
	}

	var st colimaStatus
	if jerr := json.Unmarshal([]byte(out), &st); jerr != nil {
		translation := computeDockerTranslation(info.Architecture, plat.Arch)
		return "colima-unknown", vm, translation
	}

	if st.CPU > 0 {
		vm.CPUs = st.CPU
	}
	if st.Memory > 0 {
		vm.Memory = formatColimaMemory(st.Memory)
	}

	var backend string
	switch strings.ToLower(st.Driver) {
	case "vz", "macos virtualization.framework", "macos virtualization framework":
		backend = "colima-vz"
	case "qemu":
		backend = "colima-qemu"
	default:
		backend = "colima-unknown"
	}

	// Translation: QEMU driver with cross-arch means emulation.
	colimaArch := normalizeArch(st.Arch)
	translation := "none"
	if colimaArch != "" && colimaArch != plat.Arch {
		translation = "qemu"
	} else if strings.ToLower(st.Driver) == "qemu" && colimaArch == plat.Arch {
		// Native arch but QEMU driver — still native execution.
		translation = "none"
	}

	return backend, vm, translation
}

// detectDockerDesktopBackend reads Docker Desktop settings to identify the
// virtualization backend. Uses evidence only; falls back to unknown.
func (p *prober) detectDockerDesktopBackend(info dockerInfo, plat Platform) (string, VMResources, string) {
	vm := VMResources{CPUs: info.NCPU}
	if info.MemTotal > 0 {
		vm.Memory = formatBytes(info.MemTotal)
	}

	translation := computeDockerTranslation(info.Architecture, plat.Arch)

	// Try to read Docker Desktop settings for the backend type.
	backend := p.dockerDesktopBackendFromSettings()
	if backend == "" {
		backend = "docker-desktop-unknown"
	}

	// If the engine arch differs from the host arch, it is translated.
	if translation != "none" {
		// Cross-architecture on Docker Desktop is QEMU/Rosetta emulation.
		// Keep the backend label but note translation separately.
	}

	return backend, vm, translation
}

// dockerDesktopBackendFromSettings reads the Docker Desktop settings file
// (read-only) and looks for explicit recognized keys.
func (p *prober) dockerDesktopBackendFromSettings() string {
	for _, path := range dockerDesktopSettingsPaths() {
		data, err := p.fs.ReadFile(path)
		if err != nil {
			continue
		}
		return classifyDockerDesktopSettings(data)
	}
	return ""
}

// classifyDockerDesktopSettings inspects the settings JSON text for known
// backend indicators. Returns "" if no recognized pattern is found.
func classifyDockerDesktopSettings(data string) string {
	// Look for the virtualization framework setting. Docker Desktop has
	// used different key names across versions; check common ones.
	if strings.Contains(data, "\"useVmm\":true") ||
		strings.Contains(data, "\"useVirtualizationFramework\":true") {
		return "docker-desktop-apple"
	}
	if strings.Contains(data, "\"useVmm\":false") ||
		strings.Contains(data, "\"useVirtualizationFramework\":false") {
		return "docker-desktop-qemu"
	}
	return ""
}

// computeDockerTranslation determines whether the engine architecture differs
// from the host architecture, indicating translation/emulation.
func computeDockerTranslation(engineArch, hostArch string) string {
	ea := normalizeArch(engineArch)
	ha := normalizeArch(hostArch)
	if ea == "" || ha == "" {
		return "none"
	}
	if ea != ha {
		return "qemu"
	}
	return "none"
}

// formatBytes converts a byte count to a human-readable string.
func formatBytes(bytes int64) string {
	const gib = 1024 * 1024 * 1024
	if bytes >= gib {
		return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(gib))
	}
	const mib = 1024 * 1024
	if bytes >= mib {
		return fmt.Sprintf("%.0f MiB", float64(bytes)/float64(mib))
	}
	return fmt.Sprintf("%d B", bytes)
}

// formatColimaMemory formats a Colima memory value, handling GiB vs bytes.
func formatColimaMemory(memory int) string {
	if memory > 100000 {
		// Likely bytes.
		return formatBytes(int64(memory))
	}
	// Likely GiB.
	return fmt.Sprintf("%d GiB", memory)
}

// dockerDesktopSettingsPaths returns candidate settings file paths.
func dockerDesktopSettingsPaths() []string {
	if home := homeDir(); home != "" {
		return []string{
			home + "/Library/Group Containers/group.com.docker/settings-store.json",
			home + "/Library/Group Containers/group.com.docker/settings.json",
		}
	}
	return nil
}

// homeDir returns the user's home directory.
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
