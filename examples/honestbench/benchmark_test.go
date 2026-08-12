package honestbench

import (
	"bytes"
	"slices"
	"testing"
)

func sum(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func BenchmarkLoop(b *testing.B) {
	input := []int{1, 2, 3, 4, 5}
	for b.Loop() {
		sum(input)
	}
}

func BenchmarkSubbenchmarks(b *testing.B) {
	for _, size := range []int{64, 1024} {
		b.Run(stringSize(size), func(b *testing.B) {
			input := bytes.Repeat([]byte("g"), size)
			for b.Loop() {
				bytes.Count(input, []byte("g"))
			}
		})
	}
}

func BenchmarkSortClonesInput(b *testing.B) {
	original := []int{5, 1, 4, 2, 3}
	b.ReportAllocs()
	for b.Loop() {
		input := slices.Clone(original)
		slices.Sort(input)
	}
}

func BenchmarkThroughput(b *testing.B) {
	payload := bytes.Repeat([]byte("benchlab"), 128)
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		bytes.Count(payload, []byte("b"))
	}
}

func BenchmarkParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		buffer := make([]byte, 0, 128)
		for pb.Next() {
			buffer = append(buffer[:0], "benchlab"...)
			bytes.Count(buffer, []byte("b"))
		}
	})
}

func stringSize(size int) string {
	switch size {
	case 64:
		return "64"
	case 1024:
		return "1024"
	default:
		return "other"
	}
}
