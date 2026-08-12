package benchgate

// Verdict is the overall gate verdict for a comparison.
type Verdict string

const (
	// VerdictPass means all gated series passed or improved.
	VerdictPass Verdict = "PASS"
	// VerdictRegression means at least one gated series regressed beyond threshold.
	VerdictRegression Verdict = "REGRESSION"
	// VerdictInconclusive means at least one selected series could not be decided.
	VerdictInconclusive Verdict = "INCONCLUSIVE"
	// VerdictWaived means a regression was accepted via the one-shot waiver.
	VerdictWaived Verdict = "WAIVED"
	// VerdictError means an execution, configuration, or parsing error occurred.
	VerdictError Verdict = "ERROR"
)

// RowStatus is the status of a single comparison row.
type RowStatus string

const (
	RowPass          RowStatus = "PASS"
	RowRegression    RowStatus = "REGRESSION"
	RowInconclusive  RowStatus = "INCONCLUSIVE"
	RowWaived        RowStatus = "WAIVED"
	RowImprovement   RowStatus = "IMPROVEMENT"
	RowNew           RowStatus = "NEW"
	RowRemoved       RowStatus = "REMOVED"
	RowInformational RowStatus = "INFORMATIONAL"
)

// IsGated reports whether this status is a gated (decision-bearing) status.
// New, removed, and informational rows are not gated.
func (s RowStatus) IsGated() bool {
	switch s {
	case RowPass, RowRegression, RowInconclusive, RowWaived, RowImprovement:
		return true
	default:
		return false
	}
}

// IsFailing reports whether this row status should fail the gate (before waiver).
func (s RowStatus) IsFailing() bool {
	return s == RowRegression || s == RowInconclusive
}

// SeriesKey uniquely identifies a benchmark series across base and candidate.
type SeriesKey struct {
	Package    string `json:"package"`
	FullName   string `json:"full_name"`  // full benchmark name including sub-benchmarks and GOMAXPROCS
	GOMAXPROCS string `json:"gomaxprocs"` // extracted GOMAXPROCS variant, empty if absent
	Unit       string `json:"unit"`       // normalized unit (sec/op, B/op, allocs/op, ...)
}

// SideStats holds statistics for one side (base or candidate) of a series.
type SideStats struct {
	N      int       `json:"n"`
	Median float64   `json:"median"`
	Mean   float64   `json:"mean"`
	Stddev float64   `json:"stddev"`
	CV     float64   `json:"cv"`     // coefficient of variation in percent
	Values []float64 `json:"values"` // sorted ascending
}

// ComparisonRow is a single row in the comparison report.
type ComparisonRow struct {
	Key         SeriesKey  `json:"key"`
	Base        *SideStats `json:"base,omitempty"`
	Candidate   *SideStats `json:"candidate,omitempty"`
	Delta       float64    `json:"delta"`       // percent change: (candidate-base)/base*100
	PValue      float64    `json:"p_value"`     // Mann-Whitney p-value
	Significant bool       `json:"significant"` // p < alpha
	Threshold   float64    `json:"threshold"`   // applied threshold in percent
	Direction   int        `json:"direction"`   // +1 higher is better, -1 lower is better, 0 unknown
	Status      RowStatus  `json:"status"`
	Warnings    []string   `json:"warnings,omitempty"`
}

// IsGated reports whether this row is a gated series (has both base and candidate
// and uses a gated unit).
func (r *ComparisonRow) IsGated() bool {
	return r.Status.IsGated()
}

// Identity records repository and SHA metadata for the report.
type Identity struct {
	Repository string `json:"repository,omitempty"` // owner/repo
	BaseSHA    string `json:"base_sha,omitempty"`
	HeadSHA    string `json:"head_sha,omitempty"`
	MergeSHA   string `json:"merge_sha,omitempty"`
	PRNumber   int    `json:"pr_number,omitempty"`
	RunID      string `json:"run_id,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
}

// Environment captures file-level configuration from both sides.
type Environment struct {
	BaseGOOS      string `json:"base_goos,omitempty"`
	BaseGOARCH    string `json:"base_goarch,omitempty"`
	BasePkg       string `json:"base_pkg,omitempty"`
	BaseCPU       string `json:"base_cpu,omitempty"`
	BaseGoVersion string `json:"base_go_version,omitempty"`
	CandGOOS      string `json:"cand_goos,omitempty"`
	CandGOARCH    string `json:"cand_goarch,omitempty"`
	CandPkg       string `json:"cand_pkg,omitempty"`
	CandCPU       string `json:"cand_cpu,omitempty"`
	CandGoVersion string `json:"cand_go_version,omitempty"`
}

// ReportSummary aggregates row counts by status.
type ReportSummary struct {
	Total         int `json:"total"`
	Gated         int `json:"gated"`
	Pass          int `json:"pass"`
	Regression    int `json:"regression"`
	Inconclusive  int `json:"inconclusive"`
	Waived        int `json:"waived"`
	Improvement   int `json:"improvement"`
	New           int `json:"new"`
	Removed       int `json:"removed"`
	Informational int `json:"informational"`
}

// WaiverMetadata records the one-shot waiver state.
type WaiverMetadata struct {
	Enabled  bool     `json:"enabled"`
	Actor    string   `json:"actor,omitempty"`
	Label    string   `json:"label,omitempty"`
	HeadSHA  string   `json:"head_sha,omitempty"`
	Accepted []string `json:"accepted,omitempty"` // full names of accepted regressions
}

// ComparisonReport is the versioned top-level report.
type ComparisonReport struct {
	SchemaVersion string          `json:"schema_version"`
	Verdict       Verdict         `json:"verdict"`
	Policy        Policy          `json:"policy"`
	Identity      Identity        `json:"identity,omitempty"`
	Environment   Environment     `json:"environment,omitempty"`
	BatchOrder    []string        `json:"batch_order,omitempty"`
	Rows          []ComparisonRow `json:"rows"`
	Summary       ReportSummary   `json:"summary"`
	Waiver        *WaiverMetadata `json:"waiver,omitempty"`
	Warnings      []string        `json:"warnings,omitempty"`
}
