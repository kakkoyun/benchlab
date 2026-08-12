package benchgate

// MetricDirection indicates whether higher or lower values are better.
type MetricDirection int

const (
	DirectionLowerIsBetter  MetricDirection = -1
	DirectionHigherIsBetter MetricDirection = +1
	DirectionUnknown        MetricDirection = 0
)

// GatedUnit is a unit that benchgate enforces thresholds on.
type GatedUnit struct {
	Unit      string // normalized unit name: "sec/op", "B/op", "allocs/op"
	Direction MetricDirection
	// Threshold is the minimum percent change in the worse direction
	// required to fail. For sec/op, a 10% threshold means the candidate
	// must be more than 10% slower to fail. For B/op and allocs/op,
	// a 0 threshold means any increase fails.
	Threshold float64
}

// DefaultGatedUnits returns the v0.2.0 default gated units.
func DefaultGatedUnits() []GatedUnit {
	return []GatedUnit{
		{Unit: "sec/op", Direction: DirectionLowerIsBetter, Threshold: 10.0},
		{Unit: "B/op", Direction: DirectionLowerIsBetter, Threshold: 0.0},
		{Unit: "allocs/op", Direction: DirectionLowerIsBetter, Threshold: 0.0},
	}
}

// Policy configures the comparison engine thresholds and behavior.
type Policy struct {
	// Alpha is the significance level for the Mann-Whitney U test.
	Alpha float64 `json:"alpha"`
	// MaxCV is the maximum coefficient of variation (percent) for a
	// selected series to be considered conclusive.
	MaxCV float64 `json:"max_cv"`
	// MinSamples is the minimum number of samples per side for a
	// selected series to be considered conclusive.
	MinSamples int `json:"min_samples"`
	// Confidence is the confidence level for summary intervals.
	Confidence float64 `json:"confidence"`
	// RuntimeThreshold is the percent regression threshold for sec/op.
	// The candidate fails only when it is more than this percent slower
	// AND p < Alpha.
	RuntimeThreshold float64 `json:"runtime_threshold"`
	// BytesThreshold is the percent regression threshold for B/op.
	// Default 0 means any increase fails.
	BytesThreshold float64 `json:"bytes_threshold"`
	// AllocsThreshold is the percent regression threshold for allocs/op.
	// Default 0 means any increase fails.
	AllocsThreshold float64 `json:"allocs_threshold"`
	// GatedUnits lists the units that are gated. Custom units are
	// informational only in v0.2.0.
	GatedUnits []GatedUnit `json:"gated_units"`
	// AllowEnvMismatch allows comparison across incompatible environments.
	AllowEnvMismatch bool `json:"allow_env_mismatch"`
}

// DefaultPolicy returns the v0.2.0 default policy.
func DefaultPolicy() Policy {
	p := Policy{
		Alpha:            0.05,
		MaxCV:            5.0,
		MinSamples:       10,
		Confidence:       0.95,
		RuntimeThreshold: 10.0,
		BytesThreshold:   0.0,
		AllocsThreshold:  0.0,
		AllowEnvMismatch: false,
	}
	p.GatedUnits = []GatedUnit{
		{Unit: "sec/op", Direction: DirectionLowerIsBetter, Threshold: p.RuntimeThreshold},
		{Unit: "B/op", Direction: DirectionLowerIsBetter, Threshold: p.BytesThreshold},
		{Unit: "allocs/op", Direction: DirectionLowerIsBetter, Threshold: p.AllocsThreshold},
	}
	return p
}

// ThresholdForUnit returns the configured threshold for a unit, or -1
// if the unit is not gated.
func (p Policy) ThresholdForUnit(unit string) (threshold float64, direction MetricDirection, gated bool) {
	for _, gu := range p.GatedUnits {
		if gu.Unit == unit {
			return gu.Threshold, gu.Direction, true
		}
	}
	return 0, DirectionUnknown, false
}

// IsGatedUnit reports whether the unit is in the gated set.
func (p Policy) IsGatedUnit(unit string) bool {
	_, _, gated := p.ThresholdForUnit(unit)
	return gated
}
