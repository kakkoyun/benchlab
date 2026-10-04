// Package sum is a tiny target for the PR benchmark gate demo.
package sum

// Ints returns the sum of xs.
func Ints(xs []int) int {
	total := 0
	for _, x := range xs {
		total = add(total, x)
	}
	return total
}

// add is the deliberate regression: a per-element call the compiler is told not to inline.
//
//go:noinline
func add(a, b int) int { return a + b }
