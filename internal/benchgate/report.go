package benchgate

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const schemaVersion = "benchgate/v0.2.0"

// maxCommentChars is the truncation limit for Markdown comment bodies.
const maxCommentChars = 18000

// WriteJSON writes the versioned JSON report to w.
func WriteJSON(w io.Writer, report *ComparisonReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// WriteText writes a human-readable summary to w.
func WriteText(w io.Writer, report *ComparisonReport) error {
	if report.Verdict == VerdictError && len(report.Warnings) > 0 {
		for _, warn := range report.Warnings {
			fmt.Fprintf(w, "ERROR: %s\n", warn)
		}
		return nil
	}

	fmt.Fprintf(w, "benchgate comparison — verdict: %s\n\n", report.Verdict)
	if report.Waiver != nil && report.Waiver.Enabled {
		fmt.Fprintf(w, "waiver: accepted by %s for SHA %s\n", report.Waiver.Actor, report.Waiver.HeadSHA)
		for _, a := range report.Waiver.Accepted {
			fmt.Fprintf(w, "  accepted: %s\n", a)
		}
		fmt.Fprintln(w)
	}

	for _, row := range report.Rows {
		writeTextRow(w, &row)
	}

	fmt.Fprintf(w, "\nsummary: %d total, %d pass, %d regression, %d inconclusive, %d waived, %d improvement, %d new, %d removed, %d informational\n",
		report.Summary.Total, report.Summary.Pass, report.Summary.Regression,
		report.Summary.Inconclusive, report.Summary.Waived, report.Summary.Improvement,
		report.Summary.New, report.Summary.Removed, report.Summary.Informational)
	return nil
}

func writeTextRow(w io.Writer, row *ComparisonRow) {
	mark := statusMark(row.Status)
	name := row.Key.FullName
	if row.Key.Unit != "" {
		name += " " + row.Key.Unit
	}
	if row.Key.GOMAXPROCS != "" {
		name += " (G" + row.Key.GOMAXPROCS + ")"
	}

	var baseMed, candMed, deltaStr string
	if row.Base != nil {
		baseMed = fmt.Sprintf("%.4g", row.Base.Median)
	}
	if row.Candidate != nil {
		candMed = fmt.Sprintf("%.4g", row.Candidate.Median)
	}
	if row.Base != nil && row.Candidate != nil {
		if row.InfiniteRegression {
			deltaStr = "+∞%"
		} else {
			deltaStr = fmt.Sprintf("%+.2f%%", row.Delta)
		}
	}

	fmt.Fprintf(w, "  %-12s %-50s base=%-12s cand=%-12s delta=%-10s p=%.3f\n",
		mark, name, baseMed, candMed, deltaStr, row.PValue)

	for _, warn := range row.Warnings {
		fmt.Fprintf(w, "    ⚠ %s\n", warn)
	}
}

func statusMark(s RowStatus) string {
	switch s {
	case RowPass:
		return "✓ PASS"
	case RowRegression:
		return "✗ REGRESSION"
	case RowInconclusive:
		return "? INCONCLUSIVE"
	case RowWaived:
		return "⊘ WAIVED"
	case RowImprovement:
		return "↑ IMPROVEMENT"
	case RowNew:
		return "+ NEW"
	case RowRemoved:
		return "- REMOVED"
	case RowInformational:
		return "ℹ INFO"
	default:
		return string(s)
	}
}

// WriteMarkdown writes a GitHub-flavored Markdown report to w.
// Benchmark-controlled text is escaped. Long reports are truncated with
// a link to the workflow artifact.
func WriteMarkdown(w io.Writer, report *ComparisonReport, artifactURL string) error {
	var b strings.Builder

	b.WriteString("## benchgate — ")
	b.WriteString(string(report.Verdict))
	b.WriteString("\n\n")

	if report.Identity.PRNumber > 0 {
		fmt.Fprintf(&b, "**PR #%d** | base `%s` → head `%s`", report.Identity.PRNumber,
			shortSHA(report.Identity.BaseSHA), shortSHA(report.Identity.HeadSHA))
		if report.Identity.MergeSHA != "" {
			fmt.Fprintf(&b, " | merge `%s`", shortSHA(report.Identity.MergeSHA))
		}
		b.WriteString("\n\n")
	}

	if report.Waiver != nil && report.Waiver.Enabled {
		fmt.Fprintf(&b, "> **Waiver applied** by @%s for head `%s`.\n", report.Waiver.Actor, shortSHA(report.Waiver.HeadSHA))
		b.WriteString("> Accepted regressions:\n")
		for _, a := range report.Waiver.Accepted {
			fmt.Fprintf(&b, "> - %s\n", escapeMD(a))
		}
		b.WriteString("\n")
	}

	if report.Verdict == VerdictError {
		b.WriteString("### Error\n\n")
		for _, warn := range report.Warnings {
			fmt.Fprintf(&b, "- %s\n", escapeMD(warn))
		}
		content := b.String()
		if len(content) > maxCommentChars {
			content = content[:maxCommentChars]
			content += "\n\n*Report truncated. See the workflow artifact for the full report.*\n"
		}
		_, err := io.WriteString(w, content)
		return err
	}

	// Summary line.
	fmt.Fprintf(&b, "**%d** series: %d pass, %d regression, %d inconclusive, %d waived, %d improvement, %d new, %d removed\n\n",
		report.Summary.Total, report.Summary.Pass, report.Summary.Regression,
		report.Summary.Inconclusive, report.Summary.Waived, report.Summary.Improvement,
		report.Summary.New, report.Summary.Removed)

	// Table of gated rows.
	var gated []ComparisonRow
	for _, row := range report.Rows {
		if row.IsGated() || row.Status == RowInformational {
			gated = append(gated, row)
		}
	}

	if len(gated) > 0 {
		b.WriteString("| Benchmark | Unit | Base | Candidate | Δ | p | Status |\n")
		b.WriteString("|---|---|---|---|---|---|---|\n")
		for _, row := range gated {
			writeMarkdownRow(&b, &row)
		}
		b.WriteString("\n")
	}

	// New and removed.
	if report.Summary.New > 0 || report.Summary.Removed > 0 {
		b.WriteString("<details><summary>New / removed benchmarks</summary>\n\n")
		for _, row := range report.Rows {
			if row.Status == RowNew {
				fmt.Fprintf(&b, "- **new**: %s %s\n", escapeMD(row.Key.FullName), escapeMD(row.Key.Unit))
			}
		}
		for _, row := range report.Rows {
			if row.Status == RowRemoved {
				fmt.Fprintf(&b, "- **removed**: %s %s\n", escapeMD(row.Key.FullName), escapeMD(row.Key.Unit))
			}
		}
		b.WriteString("</details>\n\n")
	}

	// Policy.
	fmt.Fprintf(&b, "<details><summary>Policy</summary>\n\n")
	fmt.Fprintf(&b, "- alpha: %.2f\n- max CV: %.1f%%\n- min samples: %d\n- runtime threshold: %.1f%%\n- bytes threshold: %.1f%%\n- allocs threshold: %.1f%%\n",
		report.Policy.Alpha, report.Policy.MaxCV, report.Policy.MinSamples,
		report.Policy.RuntimeThreshold, report.Policy.BytesThreshold, report.Policy.AllocsThreshold)
	b.WriteString("</details>\n\n")

	if artifactURL != "" {
		fmt.Fprintf(&b, "[Raw benchmark files](%s)\n", artifactURL)
	}

	content := b.String()
	if len(content) > maxCommentChars {
		content = content[:maxCommentChars]
		content += "\n\n*Report truncated. See the workflow artifact for the full report.*\n"
	}

	_, err := io.WriteString(w, content)
	return err
}

func writeMarkdownRow(b *strings.Builder, row *ComparisonRow) {
	name := escapeMD(row.Key.FullName)
	unit := escapeMD(row.Key.Unit)
	var baseMed, candMed, deltaStr, pStr, statusStr string
	if row.Base != nil {
		baseMed = fmt.Sprintf("%.4g", row.Base.Median)
	}
	if row.Candidate != nil {
		candMed = fmt.Sprintf("%.4g", row.Candidate.Median)
	}
	if row.Base != nil && row.Candidate != nil {
		if row.InfiniteRegression {
			deltaStr = "+∞%"
		} else {
			deltaStr = fmt.Sprintf("%+.2f%%", row.Delta)
		}
	}
	if row.Significant {
		pStr = fmt.Sprintf("%.3f", row.PValue)
	} else if row.Base != nil && row.Candidate != nil {
		pStr = fmt.Sprintf("%.3f", row.PValue)
	}
	statusStr = string(row.Status)
	if len(row.Warnings) > 0 {
		statusStr += " ⚠"
	}
	fmt.Fprintf(b, "| %s | %s | %s | %s | %s | %s | %s |\n",
		name, unit, baseMed, candMed, deltaStr, pStr, statusStr)
}

// escapeMD escapes benchmark-controlled text for safe Markdown rendering.
func escapeMD(s string) string {
	r := strings.NewReplacer("|", "\\|", "`", "\\`", "<", "&lt;", ">", "&gt;", "[", "\\[", "]", "\\]")
	return r.Replace(s)
}

// shortSHA returns the first 7 characters of a SHA.
func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}
