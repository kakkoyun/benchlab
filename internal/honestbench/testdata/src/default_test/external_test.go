package defaultpkg_test

import "testing"

func consume(int) {}

func BenchmarkExternalPackage(b *testing.B) {
	for b.Loop() {
		consume(1)
	}
}
