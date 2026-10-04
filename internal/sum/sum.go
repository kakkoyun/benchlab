// Package sum is a tiny target for the PR benchmark gate demo.
package sum

// Ints returns the sum of xs.
func Ints(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}
