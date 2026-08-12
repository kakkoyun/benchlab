package benchenv

import (
	"fmt"
	"strings"
)

// RenderText produces the human-readable diagnostic report.
func RenderText(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "benchenv: benchmarking environment diagnosis (%s/%s, %d CPUs)\n\n", r.OS, r.Arch, r.NumCPU)

	b.WriteString("Platform\n")
	b.WriteString(renderPlatform(r.Platform))
	b.WriteString("\n")

	b.WriteString("Docker\n")
	b.WriteString(renderDocker(r.Docker))
	b.WriteString("\n")

	b.WriteString("Checks\n")
	for _, c := range r.Checks {
		b.WriteString(renderCheck(c))
	}
	b.WriteString("\n")

	if len(r.Actions) > 0 {
		b.WriteString("Prioritized actions\n")
		for i, a := range r.Actions {
			b.WriteString(renderAction(i+1, a))
		}
		b.WriteString("\n")
	}

	if r.Recipes.Native != "" || r.Recipes.Docker != "" {
		b.WriteString("Benchmark recipes\n")
		if r.Recipes.Native != "" {
			b.WriteString("  Native:\n")
			indentBlock(&b, r.Recipes.Native, "    ")
		}
		if r.Recipes.Docker != "" {
			b.WriteString("  Docker:\n")
			indentBlock(&b, r.Recipes.Docker, "    ")
		}
		b.WriteString("\n")
	}

	b.WriteString("Readiness\n")
	b.WriteString(renderReadiness(r.Readiness))

	return b.String()
}

// renderPlatform renders the platform section, leading with architecture,
// virtualization, and translation.
func renderPlatform(plat Platform) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  architecture:   %s\n", formatArch(plat))
	fmt.Fprintf(&b, "  virtualization: %s\n", formatVirt(plat.Virtualization))
	fmt.Fprintf(&b, "  translation:    %s\n", formatTranslation(plat.Translation))
	if plat.CPUModel != "" {
		fmt.Fprintf(&b, "  CPU model:      %s\n", plat.CPUModel)
	}
	if plat.OS == "darwin" {
		fmt.Fprintf(&b, "  power:          %s\n", plat.Power)
		if plat.PowerMode != "" {
			fmt.Fprintf(&b, "  power mode:     %s\n", plat.PowerMode)
		}
	}
	if plat.LoadAvg > 0 || plat.OS == "linux" {
		fmt.Fprintf(&b, "  load average:   %.2f\n", plat.LoadAvg)
	}
	if plat.Thermal != "" {
		fmt.Fprintf(&b, "  thermal:        %s\n", plat.Thermal)
	}
	if plat.Evidence != "" {
		fmt.Fprintf(&b, "  evidence:       %s\n", plat.Evidence)
	}
	return b.String()
}

// renderDocker renders the Docker section.
func renderDocker(dkr Docker) string {
	var b strings.Builder
	if !dkr.Available {
		fmt.Fprintf(&b, "  available:  no")
		if dkr.UnavailableMsg != "" {
			fmt.Fprintf(&b, " — %s", dkr.UnavailableMsg)
		}
		b.WriteString("\n")
		// When containerized, render the current-container isolation
		// inspection even when Docker CLI/daemon is unavailable.
		if dkr.Containerized {
			fmt.Fprintf(&b, "  containerized: yes\n")
			if dkr.Isolation != nil {
				b.WriteString(renderIsolation(dkr.Isolation))
			}
		}
		return b.String()
	}

	fmt.Fprintf(&b, "  available:    yes\n")
	if dkr.Context != "" {
		fmt.Fprintf(&b, "  context:      %s\n", dkr.Context)
	}
	if dkr.Endpoint != "" {
		fmt.Fprintf(&b, "  endpoint:     %s\n", dkr.Endpoint)
	}
	fmt.Fprintf(&b, "  local:        %v\n", dkr.Local)
	fmt.Fprintf(&b, "  backend:      %s\n", formatBackend(dkr))
	if dkr.EngineOS != "" {
		fmt.Fprintf(&b, "  engine OS:    %s\n", dkr.EngineOS)
	}
	if dkr.EngineArch != "" {
		fmt.Fprintf(&b, "  engine arch:  %s\n", dkr.EngineArch)
	}
	fmt.Fprintf(&b, "  translation: %s\n", formatTranslation(dkr.Translation))
	if dkr.VMResources.CPUs > 0 {
		fmt.Fprintf(&b, "  VM CPUs:      %d\n", dkr.VMResources.CPUs)
	}
	if dkr.VMResources.Memory != "" {
		fmt.Fprintf(&b, "  VM memory:    %s\n", dkr.VMResources.Memory)
	}
	if dkr.Containerized {
		fmt.Fprintf(&b, "  containerized: yes\n")
	}
	if dkr.Isolation != nil {
		b.WriteString(renderIsolation(dkr.Isolation))
	}
	return b.String()
}

// renderIsolation renders the isolation probe result.
func renderIsolation(probe *IsolationProbe) string {
	var b strings.Builder
	if !probe.Ran {
		return ""
	}
	if probe.Passed {
		fmt.Fprintf(&b, "  isolation:    passed (cgroup %s", probe.CgroupVersion)
		if probe.SelectedCPU != "" {
			fmt.Fprintf(&b, ", cpu %s", probe.SelectedCPU)
		}
		b.WriteString(")\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  isolation:    failed (cgroup %s)\n", probe.CgroupVersion)
	for _, f := range probe.Findings {
		fmt.Fprintf(&b, "    - %s: %s", f.Name, f.Detail)
		if f.Remedy != "" {
			fmt.Fprintf(&b, " | remedy: %s", f.Remedy)
		}
		b.WriteString("\n")
	}
	if probe.Error != "" {
		fmt.Fprintf(&b, "    error: %s\n", probe.Error)
	}
	return b.String()
}

// renderCheck renders a single check, showing both the reason and the remedy.
func renderCheck(c Check) string {
	var b strings.Builder
	label := fmt.Sprintf("[%s]", c.Status)
	fmt.Fprintf(&b, "  %-15s %s", label, c.Name)
	if c.Detail != "" {
		fmt.Fprintf(&b, " — %s", c.Detail)
	}
	b.WriteString("\n")
	if c.Remedy != "" {
		fmt.Fprintf(&b, "                  remedy: %s\n", c.Remedy)
	}
	return b.String()
}

// renderAction renders a single prioritized action.
func renderAction(n int, a Action) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %d. [%s] %s\n", n, a.Scope, a.Reason)
	for _, cmd := range a.Commands {
		fmt.Fprintf(&b, "     %s\n", cmd)
	}
	return b.String()
}

// renderReadiness renders the readiness section.
func renderReadiness(rd Readiness) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  overall:           %s\n", rd.Overall)
	if rd.RecommendedPath != "" {
		fmt.Fprintf(&b, "  recommended path: %s\n", rd.RecommendedPath)
	}
	fmt.Fprintf(&b, "  native:            %s\n", rd.Native)
	fmt.Fprintf(&b, "  docker:            %s\n", rd.Docker)
	return b.String()
}

// indentBlock indents each line of a block by the given prefix.
func indentBlock(b *strings.Builder, block, prefix string) {
	for _, line := range strings.Split(block, "\n") {
		fmt.Fprintf(b, "%s%s\n", prefix, line)
	}
}
