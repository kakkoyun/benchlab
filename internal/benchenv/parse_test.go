package benchenv

import "testing"

func TestParseRosettaTranslation(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1", "rosetta"},
		{"0", "none"},
		{"", "none"},
		{"  1  ", "rosetta"},
		{"2", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseRosettaTranslation(tt.input); got != tt.want {
				t.Errorf("parseRosettaTranslation(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDarwinPowerSource(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Now drawing from 'AC Power'", "ac"},
		{"Now drawing from 'Battery Power'", "battery"},
		{"", "unknown"},
		{"some other output", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseDarwinPowerSource(tt.input); got != tt.want {
				t.Errorf("parseDarwinPowerSource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDarwinPowerMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  lowpowermode     1\n", "low"},
		{"  lowpowermode     0\n", "automatic"},
		{"  standby              1\n", "automatic"},
		{"", "automatic"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseDarwinPowerMode(tt.input); got != tt.want {
				t.Errorf("parseDarwinPowerMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDarwinThermal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"CPU_Speed_Limit = 50\n", "throttling: CPU_Speed_Limit=50%"},
		{"CPU_Scheduler_Limit=75\n", "throttling: CPU_Scheduler_Limit=75%"},
		{"CPU_Speed_Limit = 100\n", ""},
		{"", ""},
		{"no thermal info", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseDarwinThermal(tt.input); got != tt.want {
				t.Errorf("parseDarwinThermal(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClassifyVirtFromSystemd(t *testing.T) {
	tests := []struct {
		input string
		virt  string
		ok    bool
	}{
		{"none", "none", true},
		{"kvm", "kvm", true},
		{"qemu", "qemu", true},
		{"apple", "apple", true},
		{"xen", "xen", true},
		{"vmware", "vmware", true},
		{"oracle", "oracle", true},
		{"microsoft", "microsoft", true},
		{"bochs", "bochs", true},
		{"uml", "uml", true},
		{"unknownvirt", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			v, ok := classifyVirtFromSystemd(tt.input)
			if v != tt.virt || ok != tt.ok {
				t.Errorf("classifyVirtFromSystemd(%q) = (%q, %v), want (%q, %v)", tt.input, v, ok, tt.virt, tt.ok)
			}
		})
	}
}

func TestClassifyVirtFromDMI(t *testing.T) {
	tests := []struct {
		vendor  string
		product string
		virt    string
		ok      bool
	}{
		{"Apple Virtualization", "", "apple", true},
		{"QEMU", "", "qemu", true},
		{"", "VMware Virtual Platform", "vmware", true},
		{"", "VirtualBox", "virtualbox", true},
		{"KVM", "", "kvm", true},
		{"Dell Inc.", "PowerEdge R750", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.vendor+"/"+tt.product, func(t *testing.T) {
			v, ok := classifyVirtFromDMI(tt.vendor, tt.product)
			if v != tt.virt || ok != tt.ok {
				t.Errorf("classifyVirtFromDMI(%q, %q) = (%q, %v), want (%q, %v)", tt.vendor, tt.product, v, ok, tt.virt, tt.ok)
			}
		})
	}
}

func TestClassifyVirtFromDeviceTree(t *testing.T) {
	tests := []struct {
		model string
		virt  string
		ok    bool
	}{
		{"Apple Virtualization Generic Platform", "apple", true},
		{"QEMU virt machine", "qemu", true},
		{"Raspberry Pi 4 Model B Rev 1.4", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			v, ok := classifyVirtFromDeviceTree(tt.model)
			if v != tt.virt || ok != tt.ok {
				t.Errorf("classifyVirtFromDeviceTree(%q) = (%q, %v), want (%q, %v)", tt.model, v, ok, tt.virt, tt.ok)
			}
		})
	}
}

func TestDetectContainerFromCgroup(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"0::/docker/abc123\n", true},
		{"12:memory:/kubepods/pod123\n", true},
		{"0::/system.slice/sshd.service\n", false},
		{"11:devices:/lxc/test\n", true},
		{"0::/containerd/abc\n", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := detectContainerFromCgroup(tt.input); got != tt.want {
				t.Errorf("detectContainerFromCgroup(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectHypervisorFlag(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"flags\t\t: fpu vme de pse hypervisor\n", true},
		{"flags\t\t: fpu vme de pse\n", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if got := detectHypervisorFlag(tt.input); got != tt.want {
				t.Errorf("detectHypervisorFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseCPUModelFromCPUInfo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"processor\t: 0\nmodel name\t: AMD EPYC 7742 64-Core Processor\n", "AMD EPYC 7742 64-Core Processor"},
		{"Hardware\t: Apple M2\n", "Apple M2"},
		{"processor\t: 0\n", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			if got := parseCPUModelFromCPUInfo(tt.input); got != tt.want {
				t.Errorf("parseCPUModelFromCPUInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{134217728, "128 MiB"},
		{1073741824, "1.0 GiB"},
		{8589934592, "8.0 GiB"},
		{1048576, "1 MiB"},
		{512, "512 B"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatBytes(tt.bytes); got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestFormatColimaMemory(t *testing.T) {
	tests := []struct {
		memory int
		want   string
	}{
		{8, "8 GiB"},            // GiB
		{2147483648, "2.0 GiB"}, // bytes
		{4, "4 GiB"},            // GiB
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatColimaMemory(tt.memory); got != tt.want {
				t.Errorf("formatColimaMemory(%d) = %q, want %q", tt.memory, got, tt.want)
			}
		})
	}
}
