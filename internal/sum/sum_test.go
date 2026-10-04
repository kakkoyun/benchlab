package sum

import "testing"

func TestInts(t *testing.T) {
	if got := Ints([]int{1, 2, 3, 4}); got != 10 {
		t.Fatalf("Ints() = %d, want 10", got)
	}
}

var sink int

func BenchmarkInts(b *testing.B) {
	xs := make([]int, 4096)
	for i := range xs {
		xs[i] = i
	}
	for b.Loop() {
		sink = Ints(xs)
	}
}
