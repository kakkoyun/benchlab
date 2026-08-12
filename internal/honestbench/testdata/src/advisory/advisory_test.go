package advisory

import (
	"sort"
	"testing"
)

var packageSink int

func value() int  { return 1 }
func consume(int) {}

func BenchmarkSuggestBLoop(b *testing.B) {
	for range b.N { // want "canonical b.N loop can use"
		consume(value())
	}
}

func BenchmarkTimedSetup(b *testing.B) {
	input := value() // want "nontrivial setup before a legacy b.N loop"
	for range b.N {  // want "canonical b.N loop can use"
		consume(input)
	}
}

func BenchmarkTimedCleanup(b *testing.B) {
	var result int  // want "nontrivial setup before a legacy b.N loop"
	for range b.N { // want "canonical b.N loop can use"
		result = value()
	}
	consume(result) // want "nontrivial cleanup after a legacy b.N loop"
}

func BenchmarkDiscardedResult(b *testing.B) {
	for range b.N { // want "canonical b.N loop can use"
		value() // want "result-returning call is discarded"
	}
}

func BenchmarkMissingSink(b *testing.B) {
	var result int  // want "nontrivial setup before a legacy b.N loop"
	for range b.N { // want "canonical b.N loop can use"
		result = value() // want "result is used only by a blank assignment"
	}
	_ = result
}

func BenchmarkPackageWrite(b *testing.B) {
	for range b.N { // want "canonical b.N loop can use"
		packageSink = value() // want "writes a package variable"
	}
}

func BenchmarkConfigInLoop(b *testing.B) {
	for range b.N { // want "canonical b.N loop can use"
		b.ReportAllocs() // want "benchmark configuration should not execute"
		consume(value())
	}
}

func BenchmarkNoncanonicalBN(b *testing.B) {
	for i := 0; i < b.N; i += value() { // want "unusual and its exact iteration count cannot be proven"
		consume(i)
	}
}

func BenchmarkParallelismWithoutRunParallel(b *testing.B) {
	b.SetParallelism(2) // want "has no matching RunParallel"
	for b.Loop() {
		consume(value())
	}
}

func BenchmarkReusedMutatedInput(b *testing.B) {
	input := []int{3, 2, 1} // want "nontrivial setup before a legacy b.N loop"
	for range b.N {         // want "canonical b.N loop can use"
		sort.Ints(input) // want "in-place sort or reverse operation reuses input"
	}
}

func BenchmarkRedundantBLoopTimer(b *testing.B) {
	b.ResetTimer() // want "duplicates B.Loop behavior"
	for b.Loop() {
		consume(value())
	}
	b.StopTimer() // want "duplicates B.Loop behavior"
}

func BenchmarkExactBLoopKeepsValuesAlive(b *testing.B) {
	for b.Loop() {
		value()
	}
}

func BenchmarkCleanClone(b *testing.B) {
	base := []int{3, 2, 1}
	for b.Loop() {
		input := append([]int(nil), base...)
		sort.Ints(input)
		consume(input[0])
	}
}
