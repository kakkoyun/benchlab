package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/kakkoyun/benchlab/internal/benchenv"
)

func main() {
	jsonOut := flag.Bool("json", false, "output results as JSON")
	flag.Parse()

	checks := benchenv.CollectChecks()
	sum := benchenv.Summarize(checks)
	report := benchenv.Report{
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		NumCPU:  runtime.NumCPU(),
		Checks:  checks,
		Summary: sum,
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "benchenv: json encode: %v\n", err)
			os.Exit(2)
		}
		return
	}

	printText(report)
}

func printText(r benchenv.Report) {
	fmt.Printf("benchenv: benchmarking environment diagnosis (%s/%s, %d CPUs)\n\n", r.OS, r.Arch, r.NumCPU)
	for _, c := range r.Checks {
		label := fmt.Sprintf("[%s]", c.Status)
		var suffix string
		switch c.Status {
		case benchenv.StatusWarn:
			if c.Remedy != "" {
				suffix = " — " + c.Remedy
			} else if c.Detail != "" {
				suffix = " — " + c.Detail
			}
		case benchenv.StatusUnavailable:
			if c.Detail != "" {
				suffix = " — " + c.Detail
			}
		case benchenv.StatusOK:
			if c.Detail != "" {
				suffix = " — " + c.Detail
			}
		}
		fmt.Printf("  %-15s %s%s\n", label, c.Name, suffix)
	}
	fmt.Printf("\nSummary: %d ok, %d warn, %d unavailable. Fix warnings before trusting benchmark numbers.\n",
		r.Summary.OK, r.Summary.Warn, r.Summary.Unavailable)
}
