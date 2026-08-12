package benchgate

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"golang.org/x/perf/benchfmt"
	"golang.org/x/perf/benchmath"
)

// ParsedResults holds benchmark samples grouped by series key, plus
// the file-level environment extracted from the first result.
type ParsedResults struct {
	Series     map[SeriesKey][]float64
	Env        fileEnv
	SyntaxErrs []string
}

type fileEnv struct {
	GOOS      string
	GOARCH    string
	Pkg       string
	CPU       string
	GoVersion string
}

// ParseBenchOutput parses Go benchmark output (from go test -bench) into
// grouped samples. label is used for error messages only.
func ParseBenchOutput(r io.Reader, label string) (*ParsedResults, error) {
	reader := benchfmt.NewReader(r, label)
	parsed := &ParsedResults{Series: make(map[SeriesKey][]float64)}
	first := true

	for reader.Scan() {
		switch rec := reader.Result().(type) {
		case *benchfmt.SyntaxError:
			parsed.SyntaxErrs = append(parsed.SyntaxErrs, rec.Error())
			continue
		case *benchfmt.Result:
			if first {
				parsed.Env = extractEnv(rec)
				first = false
			}
			pkg := rec.GetConfig("pkg")
			fullName := string(rec.Name.Full())
			gomaxprocs := extractGomaxprocs(rec.Name)
			for _, val := range rec.Values {
				key := SeriesKey{
					Package:    pkg,
					FullName:   fullName,
					GOMAXPROCS: gomaxprocs,
					Unit:       val.Unit,
				}
				parsed.Series[key] = append(parsed.Series[key], val.Value)
			}
		}
	}
	if err := reader.Err(); err != nil {
		return parsed, fmt.Errorf("parse %s: %w", label, err)
	}
	return parsed, nil
}

// extractEnv pulls file-level environment from a result's configuration.
func extractEnv(r *benchfmt.Result) fileEnv {
	return fileEnv{
		GOOS:      r.GetConfig("goos"),
		GOARCH:    r.GetConfig("goarch"),
		Pkg:       r.GetConfig("pkg"),
		CPU:       r.GetConfig("cpu"),
		GoVersion: r.GetConfig("go-version"),
	}
}

// extractGomaxprocs returns the GOMAXPROCS suffix (e.g. "8") from a
// benchmark name, or "" if absent.
func extractGomaxprocs(name benchfmt.Name) string {
	_, parts := name.Parts()
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if len(last) > 0 && last[0] == '-' {
		return string(last[1:])
	}
	return ""
}

// envMismatch returns a description of incompatible environment metadata
// between base and candidate, or "" if compatible.
func envMismatch(base, cand fileEnv) string {
	var diffs []string
	if base.GOOS != "" && cand.GOOS != "" && base.GOOS != cand.GOOS {
		diffs = append(diffs, fmt.Sprintf("GOOS: %q vs %q", base.GOOS, cand.GOOS))
	}
	if base.GOARCH != "" && cand.GOARCH != "" && base.GOARCH != cand.GOARCH {
		diffs = append(diffs, fmt.Sprintf("GOARCH: %q vs %q", base.GOARCH, cand.GOARCH))
	}
	if base.CPU != "" && cand.CPU != "" && base.CPU != cand.CPU {
		diffs = append(diffs, fmt.Sprintf("CPU: %q vs %q", base.CPU, cand.CPU))
	}
	if base.GoVersion != "" && cand.GoVersion != "" && base.GoVersion != cand.GoVersion {
		diffs = append(diffs, fmt.Sprintf("Go version: %q vs %q", base.GoVersion, cand.GoVersion))
	}
	if len(diffs) == 0 {
		return ""
	}
	return "incompatible benchmark environments: " + strings.Join(diffs, "; ")
}

// benchGroup collects the units present on each side for one benchmark
// identity (package + full name).
type benchGroup struct {
	pkg      string
	fullName string
	base     map[string][]float64 // unit → samples
	cand     map[string][]float64
}

// Compare runs the statistical comparison between base and candidate
// parsed results under the given policy.
func Compare(base, cand *ParsedResults, policy Policy) (*ComparisonReport, error) {
	report := &ComparisonReport{
		SchemaVersion: "benchgate/v0.2.0",
		Policy:        policy,
		Environment: Environment{
			BaseGOOS: base.Env.GOOS, BaseGOARCH: base.Env.GOARCH,
			BasePkg: base.Env.Pkg, BaseCPU: base.Env.CPU, BaseGoVersion: base.Env.GoVersion,
			CandGOOS: cand.Env.GOOS, CandGOARCH: cand.Env.GOARCH,
			CandPkg: cand.Env.Pkg, CandCPU: cand.Env.CPU, CandGoVersion: cand.Env.GoVersion,
		},
	}

	// Environment compatibility check.
	if !policy.AllowEnvMismatch {
		if msg := envMismatch(base.Env, cand.Env); msg != "" {
			report.Verdict = VerdictError
			report.Warnings = append(report.Warnings, msg)
			report.Summary = summarizeRows(nil)
			return report, fmt.Errorf("%s", msg)
		}
	}

	// Group by (package, fullName) to detect new/removed benchmarks and
	// per-unit presence.
	groups := make(map[string]*benchGroup)
	collect := func(pr *ParsedResults, isBase bool) {
		for key, samples := range pr.Series {
			gk := key.Package + "\x00" + key.FullName
			g, ok := groups[gk]
			if !ok {
				g = &benchGroup{
					pkg: key.Package, fullName: key.FullName,
					base: make(map[string][]float64), cand: make(map[string][]float64),
				}
				groups[gk] = g
			}
			if isBase {
				g.base[key.Unit] = samples
			} else {
				g.cand[key.Unit] = samples
			}
		}
	}
	collect(base, true)
	collect(cand, false)

	// Sort group keys for deterministic output.
	groupKeys := make([]string, 0, len(groups))
	for k := range groups {
		groupKeys = append(groupKeys, k)
	}
	sort.Strings(groupKeys)

	var rows []ComparisonRow
	comparableGated := 0

	for _, gk := range groupKeys {
		g := groups[gk]
		baseOnly := len(g.cand) == 0
		candOnly := len(g.base) == 0

		if baseOnly {
			for unit, samples := range g.base {
				rows = append(rows, makeRemovedRow(g, unit, samples))
			}
			continue
		}
		if candOnly {
			for unit, samples := range g.cand {
				rows = append(rows, makeNewRow(g, unit, samples))
			}
			continue
		}

		// Both sides present: compare per unit.
		allUnits := make(map[string]bool)
		for u := range g.base {
			allUnits[u] = true
		}
		for u := range g.cand {
			allUnits[u] = true
		}
		units := make([]string, 0, len(allUnits))
		for u := range allUnits {
			units = append(units, u)
		}
		sort.Strings(units)

		for _, unit := range units {
			bSamples, hasBase := g.base[unit]
			cSamples, hasCand := g.cand[unit]
			gated := policy.IsGatedUnit(unit)

			if !hasBase || !hasCand {
				// Unit exists on only one side.
				if gated {
					rows = append(rows, makeInconclusiveMissingUnit(g, unit, hasBase, bSamples, hasCand, cSamples,
						"gated unit exists on only one side"))
				} else {
					rows = append(rows, makeInfoMissingUnit(g, unit, hasBase, bSamples, hasCand, cSamples))
				}
				continue
			}

			if gated {
				comparableGated++
			}

			row := compareSeries(g, unit, bSamples, cSamples, policy)
			rows = append(rows, row)
		}
	}

	// Zero comparable gated series is an operational error.
	if comparableGated == 0 {
		report.Verdict = VerdictError
		report.Warnings = append(report.Warnings,
			"no comparable gated series found (zero common sec/op, B/op, or allocs/op benchmarks)")
		report.Rows = rows
		report.Summary = summarizeRows(rows)
		return report, fmt.Errorf("no comparable gated series found")
	}

	report.Rows = rows
	report.Summary = summarizeRows(rows)
	report.Verdict = computeVerdict(rows)
	return report, nil
}

// compareSeries compares one gated or informational series present on both sides.
func compareSeries(g *benchGroup, unit string, bSamples, cSamples []float64, policy Policy) ComparisonRow {
	threshold, direction, gated := policy.ThresholdForUnit(unit)
	key := SeriesKey{
		Package: g.pkg, FullName: g.fullName,
		GOMAXPROCS: extractGomaxprocsFromName(g.fullName), Unit: unit,
	}
	row := ComparisonRow{
		Key:       key,
		Threshold: threshold,
		Direction: int(direction),
	}

	baseStats := computeStats(bSamples)
	candStats := computeStats(cSamples)
	row.Base = baseStats
	row.Candidate = candStats

	if !gated {
		row.Status = RowInformational
		return row
	}

	// Pre-statistical inconclusive checks: sample count and CV.
	var preWarnings []string
	if baseStats.N < policy.MinSamples || candStats.N < policy.MinSamples {
		preWarnings = append(preWarnings, fmt.Sprintf("insufficient samples: base=%d candidate=%d (need %d)",
			baseStats.N, candStats.N, policy.MinSamples))
	}
	if baseStats.CV > policy.MaxCV || candStats.CV > policy.MaxCV {
		preWarnings = append(preWarnings, fmt.Sprintf("high CV: base=%.1f%% candidate=%.1f%% (max %.1f%%)",
			baseStats.CV, candStats.CV, policy.MaxCV))
	}
	if len(preWarnings) > 0 {
		row.Status = RowInconclusive
		row.Warnings = preWarnings
		return row
	}

	// Zero-to-nonzero allocation regression (checked before U-test since
	// all-zero samples can cause the U-test to error).
	if baseStats.Median == 0 && candStats.Median > 0 && direction == DirectionLowerIsBetter {
		row.Status = RowRegression
		row.Delta = math.Inf(1)
		row.Warnings = append(row.Warnings, "infinite regression: increased from zero")
		return row
	}

	// If both medians are equal, there is no change regardless of U-test
	// warnings (the U-test may error on identical samples).
	if baseStats.Median == candStats.Median {
		row.Status = RowPass
		row.Delta = 0
		return row
	}

	// Statistical comparison.
	thresholds := &benchmath.Thresholds{CompareAlpha: policy.Alpha}
	bSample := benchmath.NewSample(copyFloats(bSamples), thresholds)
	cSample := benchmath.NewSample(copyFloats(cSamples), thresholds)
	cmp := benchmath.AssumeNothing.Compare(bSample, cSample)

	row.PValue = cmp.P
	row.Significant = cmp.P < cmp.Alpha

	// U-test warnings prevent a sound decision when medians differ.
	if len(cmp.Warnings) > 0 {
		row.Status = RowInconclusive
		for _, w := range cmp.Warnings {
			row.Warnings = append(row.Warnings, w.Error())
		}
		return row
	}

	// Not significant → no meaningful difference.
	if !row.Significant {
		row.Status = RowPass
		return row
	}

	// Significant change: compute delta and classify.
	delta := percentDelta(baseStats.Median, candStats.Median)
	row.Delta = delta

	if direction == DirectionLowerIsBetter {
		// Positive delta = slower/more = worse.
		if delta > threshold {
			row.Status = RowRegression
		} else if delta < 0 {
			row.Status = RowImprovement
		} else {
			row.Status = RowPass // 0 <= delta <= threshold
		}
	} else if direction == DirectionHigherIsBetter {
		// Negative delta = lower = worse.
		if -delta > threshold {
			row.Status = RowRegression
		} else if delta > 0 {
			row.Status = RowImprovement
		} else {
			row.Status = RowPass
		}
	} else {
		// Unknown direction: informational only.
		row.Status = RowInformational
	}

	return row
}

// computeStats calculates N, median, mean, stddev, and CV for a sample set.
func computeStats(samples []float64) *SideStats {
	n := len(samples)
	if n == 0 {
		return &SideStats{N: 0}
	}
	sorted := copyFloats(samples)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(n)

	var stddev float64
	if n > 1 {
		var variance float64
		for _, v := range sorted {
			delta := v - mean
			variance += delta * delta
		}
		stddev = math.Sqrt(variance / float64(n-1))
	}

	var cv float64
	if mean > 0 {
		cv = 100 * stddev / mean
	}

	return &SideStats{
		N:      n,
		Median: median(sorted),
		Mean:   mean,
		Stddev: stddev,
		CV:     cv,
		Values: sorted,
	}
}

// median returns the median of a sorted sample set.
func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// percentDelta computes (candidate-base)/base*100, handling base==0.
func percentDelta(base, candidate float64) float64 {
	if base == 0 {
		if candidate == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return (candidate - base) / base * 100
}

// copyFloats returns a copy of the slice to avoid mutating the caller's data
// (benchmath.NewSample sorts in place).
func copyFloats(s []float64) []float64 {
	out := make([]float64, len(s))
	copy(out, s)
	return out
}

// extractGomaxprocsFromName extracts the GOMAXPROCS suffix from a full name string.
func extractGomaxprocsFromName(fullName string) string {
	for i := len(fullName) - 1; i >= 0; i-- {
		if fullName[i] == '-' && i < len(fullName)-1 {
			// Check that everything after '-' is digits.
			rest := fullName[i+1:]
			for _, c := range rest {
				if c < '0' || c > '9' {
					return ""
				}
			}
			return rest
		}
		if fullName[i] < '0' || fullName[i] > '9' {
			break
		}
	}
	return ""
}

// computeVerdict determines the overall verdict from rows.
func computeVerdict(rows []ComparisonRow) Verdict {
	for _, r := range rows {
		if r.Status == RowInconclusive {
			return VerdictInconclusive
		}
	}
	for _, r := range rows {
		if r.Status == RowRegression {
			return VerdictRegression
		}
	}
	return VerdictPass
}

// summarizeRows counts rows by status.
func summarizeRows(rows []ComparisonRow) ReportSummary {
	s := ReportSummary{Total: len(rows)}
	for _, r := range rows {
		switch r.Status {
		case RowPass:
			s.Pass++
			s.Gated++
		case RowRegression:
			s.Regression++
			s.Gated++
		case RowInconclusive:
			s.Inconclusive++
			s.Gated++
		case RowWaived:
			s.Waived++
			s.Gated++
		case RowImprovement:
			s.Improvement++
			s.Gated++
		case RowNew:
			s.New++
		case RowRemoved:
			s.Removed++
		case RowInformational:
			s.Informational++
		}
	}
	return s
}

// --- Row constructors for non-comparable series ---

func makeRemovedRow(g *benchGroup, unit string, samples []float64) ComparisonRow {
	return ComparisonRow{
		Key:       SeriesKey{Package: g.pkg, FullName: g.fullName, GOMAXPROCS: extractGomaxprocsFromName(g.fullName), Unit: unit},
		Base:      computeStats(samples),
		Status:    RowRemoved,
		Direction: -1,
	}
}

func makeNewRow(g *benchGroup, unit string, samples []float64) ComparisonRow {
	return ComparisonRow{
		Key:       SeriesKey{Package: g.pkg, FullName: g.fullName, GOMAXPROCS: extractGomaxprocsFromName(g.fullName), Unit: unit},
		Candidate: computeStats(samples),
		Status:    RowNew,
		Direction: -1,
	}
}

func makeInconclusiveMissingUnit(g *benchGroup, unit string, hasBase bool, bSamples []float64, hasCand bool, cSamples []float64, reason string) ComparisonRow {
	row := ComparisonRow{
		Key:       SeriesKey{Package: g.pkg, FullName: g.fullName, GOMAXPROCS: extractGomaxprocsFromName(g.fullName), Unit: unit},
		Status:    RowInconclusive,
		Warnings:  []string{reason},
		Direction: -1,
	}
	if hasBase {
		row.Base = computeStats(bSamples)
	}
	if hasCand {
		row.Candidate = computeStats(cSamples)
	}
	return row
}

func makeInfoMissingUnit(g *benchGroup, unit string, hasBase bool, bSamples []float64, hasCand bool, cSamples []float64) ComparisonRow {
	row := ComparisonRow{
		Key:       SeriesKey{Package: g.pkg, FullName: g.fullName, GOMAXPROCS: extractGomaxprocsFromName(g.fullName), Unit: unit},
		Status:    RowInformational,
		Direction: 0,
	}
	if hasBase {
		row.Base = computeStats(bSamples)
	}
	if hasCand {
		row.Candidate = computeStats(cSamples)
	}
	return row
}
