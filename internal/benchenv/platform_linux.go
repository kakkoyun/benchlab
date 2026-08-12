//go:build linux

package benchenv

import (
	"strings"
)

// detectPlatform gathers Linux machine/process facts.
func (p *prober) detectPlatform() Platform {
	plat := Platform{
		OS:   "linux",
		Arch: p.arch,
	}

	// Machine architecture from uname -m (may differ from process arch
	// under Rosetta-style translation, though that is uncommon on Linux).
	if out, err := p.run("uname", "-m"); err == nil {
		raw := strings.TrimSpace(out)
		plat.RawArch = raw
	}

	// CPU model from /proc/cpuinfo.
	if data, err := p.fs.ReadFile("/proc/cpuinfo"); err == nil {
		plat.CPUModel = parseCPUModelFromCPUInfo(data)
	}

	// Container detection (independent from VM detection).
	plat.Containerized = p.linuxInContainer()

	// VM detection.
	virt, evidence := p.linuxVirt()
	plat.Virtualization = virt
	plat.Evidence = evidence

	// Translation: on Linux, QEMU user-mode emulation or cross-arch
	// containers are the main translation paths. We infer from the
	// machine arch vs process arch mismatch or a QEMU virt signature.
	plat.Translation = p.linuxTranslation(plat)

	// Load average.
	if data, err := p.fs.ReadFile("/proc/loadavg"); err == nil {
		if load, perr := parseLoadAvg(strings.TrimSpace(data)); perr == nil {
			plat.LoadAvg = load
		}
	}

	return plat
}

// linuxCPUModel extracts the model name from /proc/cpuinfo.
func (p *prober) linuxCPUModel() string {
	data, err := p.fs.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	return parseCPUModelFromCPUInfo(data)
}

// linuxInContainer detects whether the process is running inside a container.
func (p *prober) linuxInContainer() bool {
	if p.fs.Exists("/.dockerenv") {
		return true
	}
	if data, err := p.fs.ReadFile("/proc/1/cgroup"); err == nil {
		return detectContainerFromCgroup(data)
	}
	return false
}

// linuxVirt detects virtualization technology using systemd-detect-virt,
// then falls back to DMI, device-tree, and CPU flags.
func (p *prober) linuxVirt() (virt string, evidence string) {
	// systemd-detect-virt --vm is the most reliable signal.
	if out, err := p.run("systemd-detect-virt", "--vm"); err == nil {
		if v, ok := classifyVirtFromSystemd(out); ok {
			return v, "systemd-detect-virt --vm: " + strings.TrimSpace(out)
		}
	}

	// DMI vendor/product fallback.
	vendor := p.fsReadTrim("/sys/class/dmi/id/sys_vendor")
	product := p.fsReadTrim("/sys/class/dmi/id/product_name")
	if v, ok := classifyVirtFromDMI(vendor, product); ok {
		if vendor != "" {
			return v, "DMI sys_vendor: " + vendor
		}
		return v, "DMI product_name: " + product
	}

	// Device-tree fallback (common on ARM).
	if p.fs.Exists("/proc/device-tree") {
		model := p.fsReadTrim("/proc/device-tree/model")
		if v, ok := classifyVirtFromDeviceTree(model); ok {
			return v, "device-tree model: " + model
		}
	}

	// CPU flags fallback: the "hypervisor" flag in /proc/cpuinfo.
	if data, err := p.fs.ReadFile("/proc/cpuinfo"); err == nil {
		if detectHypervisorFlag(data) {
			return "unknown", "cpuinfo hypervisor flag set"
		}
	}

	return "none", "no virtualization evidence found"
}

// linuxTranslation determines whether the process is running translated.
func (p *prober) linuxTranslation(plat Platform) string {
	if plat.Virtualization == "qemu" {
		return "qemu"
	}
	// Machine arch vs process arch mismatch suggests user-mode emulation.
	if plat.RawArch != "" {
		machineArch := normalizeArch(plat.RawArch)
		if machineArch != plat.Arch && machineArch != "" {
			return "qemu"
		}
	}
	return "none"
}

// fsReadTrim reads a file and returns its trimmed contents, or empty on error.
func (p *prober) fsReadTrim(path string) string {
	data, err := p.fs.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(data)
}
