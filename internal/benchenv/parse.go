package benchenv

import "strings"

// parseRosettaTranslation parses the output of "sysctl -n sysctl.proc_translated".
// Returns "rosetta" if the process is translated, "none" otherwise.
func parseRosettaTranslation(output string) string {
	if strings.TrimSpace(output) == "1" {
		return "rosetta"
	}
	return "none"
}

// parseDarwinPowerSource parses "pmset -g ps" output and returns "ac", "battery",
// or "unknown".
func parseDarwinPowerSource(output string) string {
	if strings.Contains(output, "AC Power") {
		return "ac"
	}
	if strings.Contains(output, "Battery Power") {
		return "battery"
	}
	return "unknown"
}

// parseDarwinPowerMode parses "pmset -g" output for the lowpowermode setting.
// Returns "low" if Low Power Mode is active, "automatic" otherwise.
func parseDarwinPowerMode(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "lowpowermode") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] == "1" {
				return "low"
			}
			return "automatic"
		}
	}
	return "automatic"
}

// parseDarwinThermal parses "pmset -g therm" output for thermal throttling.
// Returns a non-empty description if throttling is active.
func parseDarwinThermal(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "CPU_Scheduler_Limit") || strings.Contains(line, "CPU_Speed_Limit") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				pct := strings.TrimSpace(parts[1])
				if pct != "100" && pct != "" {
					return "throttling: " + strings.TrimSpace(parts[0]) + "=" + pct + "%"
				}
			}
		}
	}
	return ""
}

// classifyVirtFromSystemd maps systemd-detect-virt --vm output to a canonical
// virtualization label.
func classifyVirtFromSystemd(output string) (virt string, ok bool) {
	v := strings.TrimSpace(output)
	switch v {
	case "none":
		return "none", true
	case "kvm":
		return "kvm", true
	case "qemu":
		return "qemu", true
	case "apple":
		return "apple", true
	case "xen":
		return "xen", true
	case "oracle", "vmware", "microsoft", "bochs", "uml":
		return v, true
	}
	return "", false
}

// classifyVirtFromDMI maps DMI sys_vendor and product_name strings to a
// virtualization label.
func classifyVirtFromDMI(vendor, product string) (virt string, ok bool) {
	vl := strings.ToLower(vendor)
	pl := strings.ToLower(product)
	switch {
	case strings.Contains(vl, "apple virtualization"):
		return "apple", true
	case strings.Contains(vl, "qemu"):
		return "qemu", true
	case strings.Contains(vl, "kvm"):
		return "kvm", true
	case strings.Contains(pl, "vmware"):
		return "vmware", true
	case strings.Contains(pl, "virtualbox"):
		return "virtualbox", true
	}
	return "", false
}

// classifyVirtFromDeviceTree maps a device-tree model string to a
// virtualization label.
func classifyVirtFromDeviceTree(model string) (virt string, ok bool) {
	ml := strings.ToLower(model)
	switch {
	case strings.Contains(ml, "apple virtualization"):
		return "apple", true
	case strings.Contains(ml, "qemu"):
		return "qemu", true
	}
	return "", false
}

// detectContainerFromCgroup checks /proc/1/cgroup content for container
// runtime hints.
func detectContainerFromCgroup(content string) bool {
	for _, hint := range []string{"docker", "containerd", "kubepods", "lxc"} {
		if strings.Contains(content, hint) {
			return true
		}
	}
	return false
}

// detectHypervisorFlag checks /proc/cpuinfo content for the "hypervisor" CPU
// flag.
func detectHypervisorFlag(cpuinfo string) bool {
	return strings.Contains(cpuinfo, "hypervisor")
}

// parseCPUModelFromCPUInfo extracts the model name from /proc/cpuinfo content.
func parseCPUModelFromCPUInfo(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
