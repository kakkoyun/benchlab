package benchgate

import "fmt"

// ApplyWaiver converts REGRESSION rows to WAIVED when the waiver is valid.
// It cannot override INCONCLUSIVE or ERROR rows. The waiver applies to
// one PR head SHA only.
func ApplyWaiver(report *ComparisonReport, waiver WaiverMetadata) {
	if !waiver.Enabled {
		return
	}
	if report.Waiver == nil {
		report.Waiver = &WaiverMetadata{}
	}
	report.Waiver.Enabled = true
	report.Waiver.Actor = waiver.Actor
	report.Waiver.Label = waiver.Label
	report.Waiver.HeadSHA = waiver.HeadSHA

	var accepted []string
	for i := range report.Rows {
		row := &report.Rows[i]
		if row.Status == RowRegression {
			row.Status = RowWaived
			accepted = append(accepted, row.Key.FullName+" "+row.Key.Unit)
		}
	}
	report.Waiver.Accepted = accepted
	report.Summary = summarizeRows(report.Rows)

	// Recompute verdict: INCONCLUSIVE still takes priority over WAIVED.
	verdict := computeVerdict(report.Rows)
	if verdict == VerdictPass {
		report.Verdict = VerdictWaived
	} else {
		report.Verdict = verdict
	}
}

// WaiverLabel is the GitHub label name for the one-shot regression waiver.
const WaiverLabel = "benchgate:accept-regression"

// WaiverDescription is the GitHub label description.
const WaiverDescription = "Accept benchmark regressions for one PR head only. Removed after a valid waived run passes."

// ValidateWaiverEvent checks whether a labeled event should enable a waiver.
// Returns an error if the event action or label do not match.
func ValidateWaiverEvent(action, label, headSHA string) error {
	if action != "labeled" {
		return fmt.Errorf("waiver requires a labeled event, got %q", action)
	}
	if label != WaiverLabel {
		return fmt.Errorf("waiver requires label %q, got %q", WaiverLabel, label)
	}
	if headSHA == "" {
		return fmt.Errorf("waiver requires a non-empty head SHA")
	}
	return nil
}
