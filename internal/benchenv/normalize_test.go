package benchenv

import "testing"

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"aarch64", "arm64"},
		{"arm64", "arm64"},
		{"AARCH64", "arm64"},
		{"  arm64  ", "arm64"},
		{"x86_64", "amd64"},
		{"amd64", "amd64"},
		{"AMD64", "amd64"},
		{"x86", "x86"},         // unknown retained
		{"", ""},               // empty retained
		{"riscv64", "riscv64"}, // unknown retained
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeArch(tt.input)
			if got != tt.want {
				t.Errorf("normalizeArch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
