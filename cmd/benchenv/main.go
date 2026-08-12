// benchenv diagnoses the benchmarking environment for common sources of
// benchmark noise. It distinguishes the local machine, the running process,
// the connected Docker engine/VM, and a probe container, then emits
// prioritized guidance for improving benchmark reliability.
//
// Usage:
//
//	benchenv          # human-readable text output
//	benchenv -json    # machine-readable JSON
//	benchenv -strict  # exit 1 unless the environment is publication-grade
//
// Exit codes:
//
//	0  diagnosis completed (default mode)
//	0  overall grade is "ready" (-strict mode)
//	1  overall grade is not "ready" (-strict mode)
//	2  CLI usage or encoding error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/kakkoyun/benchlab/internal/benchenv"
)

func main() {
	os.Exit(run())
}

func run() int {
	jsonOut := flag.Bool("json", false, "output results as JSON")
	strict := flag.Bool("strict", false, "exit 1 unless the environment is publication-grade (overall ready)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: benchenv [flags]")
		flag.PrintDefaults()
	}
	flag.Parse()

	report := benchenv.Diagnose()

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "benchenv: json encode: %v\n", err)
			return 2
		}
	} else {
		fmt.Print(benchenv.RenderText(report))
	}

	if *strict {
		if report.Readiness.Overall != benchenv.GradeReady {
			return 1
		}
	}
	return 0
}
