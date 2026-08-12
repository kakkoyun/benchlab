package commentfix

import "testing"

func consume(int) {}

func BenchmarkCommentedLegacyHeader(b *testing.B) {
	for range /* preserve */ b.N { // want "canonical b.N loop can use"
		consume(1)
	}
}

func BenchmarkCommentedNoncanonical(b *testing.B) {
	for b.Loop() /* preserve */ == true { // want "B.Loop must be the sole condition"
		consume(1)
	}
}

func BenchmarkCommentedTimer(b *testing.B) {
	b.ResetTimer() /* preserve */ // want "duplicates B.Loop behavior"
	for b.Loop() {
		consume(1)
	}
}
