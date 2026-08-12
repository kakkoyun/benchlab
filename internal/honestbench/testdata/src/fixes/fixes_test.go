package fixes

import "testing"

func consume(int) {}

func BenchmarkLegacyRange(b *testing.B) {
	for range b.N { // want "canonical b.N loop can use"
		consume(1)
	}
}

func BenchmarkLegacyClassic(b *testing.B) {
	for i := 0; i < b.N; i++ { // want "canonical b.N loop can use"
		consume(1)
	}
}

func BenchmarkNormalizeLoop(b *testing.B) {
	for b.Loop() == true { // want "B.Loop must be the sole condition"
		consume(1)
	}
}

func BenchmarkRemoveTimerCalls(b *testing.B) {
	b.ResetTimer() // want "duplicates B.Loop behavior"
	for b.Loop() {
		consume(1)
	}
	b.StopTimer() // want "duplicates B.Loop behavior"
}
