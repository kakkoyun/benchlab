// honestbench checks Go benchmarks for structural correctness and documented
// testing API misuse. It accepts the standard package patterns and flags
// provided by the go/analysis singlechecker driver.
//
// Usage:
//
//	honestbench ./...
//	honestbench -advisory ./...
//	honestbench -json ./...
//	go vet -vettool=/path/to/honestbench ./...
package main

import (
	"github.com/kakkoyun/benchlab/internal/honestbench"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(honestbench.Analyzer)
}
