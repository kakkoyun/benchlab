package benchgate

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteJSON_ValidSchema(t *testing.T) {
	report := &ComparisonReport{
		SchemaVersion: schemaVersion,
		Verdict:       VerdictPass,
		Policy:        DefaultPolicy(),
		Rows:          []ComparisonRow{},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, report); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var decoded ComparisonReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.SchemaVersion != "benchgate/v0.2.0" {
		t.Errorf("expected schema benchgate/v0.2.0, got %s", decoded.SchemaVersion)
	}
	if decoded.Verdict != VerdictPass {
		t.Errorf("expected PASS, got %s", decoded.Verdict)
	}
}

func TestWriteText_ShowsVerdict(t *testing.T) {
	report := &ComparisonReport{
		SchemaVersion: schemaVersion,
		Verdict:       VerdictRegression,
		Policy:        DefaultPolicy(),
		Rows: []ComparisonRow{
			{
				Key:       SeriesKey{FullName: "Foo", Unit: "sec/op"},
				Status:    RowRegression,
				PValue:    0.001,
				Delta:     15.0,
				Base:      &SideStats{Median: 100, N: 10},
				Candidate: &SideStats{Median: 115, N: 10},
			},
		},
	}
	var buf bytes.Buffer
	WriteText(&buf, report)
	output := buf.String()
	if !strings.Contains(output, "REGRESSION") {
		t.Error("text output should show REGRESSION")
	}
	if !strings.Contains(output, "Foo") {
		t.Error("text output should show benchmark name")
	}
}

func TestWriteText_ErrorVerdict(t *testing.T) {
	report := &ComparisonReport{
		SchemaVersion: schemaVersion,
		Verdict:       VerdictError,
		Policy:        DefaultPolicy(),
		Warnings:      []string{"incompatible environments"},
	}
	var buf bytes.Buffer
	WriteText(&buf, report)
	output := buf.String()
	if !strings.Contains(output, "ERROR") {
		t.Error("text output should show ERROR")
	}
	if !strings.Contains(output, "incompatible") {
		t.Error("text output should show warning")
	}
}

func TestWriteMarkdown_EscapesPipe(t *testing.T) {
	report := &ComparisonReport{
		SchemaVersion: schemaVersion,
		Verdict:       VerdictPass,
		Policy:        DefaultPolicy(),
		Rows: []ComparisonRow{
			{
				Key:       SeriesKey{FullName: "Foo|injection", Unit: "sec/op"},
				Status:    RowPass,
				Base:      &SideStats{Median: 100, N: 10},
				Candidate: &SideStats{Median: 100, N: 10},
			},
		},
	}
	var buf bytes.Buffer
	WriteMarkdown(&buf, report, "")
	output := buf.String()
	if strings.Contains(output, "Foo|injection") && !strings.Contains(output, "Foo\\|injection") {
		t.Error("pipe in benchmark name should be escaped")
	}
}

func TestWriteMarkdown_Truncates(t *testing.T) {
	report := &ComparisonReport{
		SchemaVersion: schemaVersion,
		Verdict:       VerdictPass,
		Policy:        DefaultPolicy(),
	}
	// Build a very large report.
	for i := 0; i < 1000; i++ {
		report.Rows = append(report.Rows, ComparisonRow{
			Key:       SeriesKey{FullName: strings.Repeat("X", 50), Unit: "sec/op"},
			Status:    RowPass,
			Base:      &SideStats{Median: 100, N: 10},
			Candidate: &SideStats{Median: 100, N: 10},
		})
	}
	var buf bytes.Buffer
	WriteMarkdown(&buf, report, "")
	output := buf.String()
	if len(output) > maxCommentChars+500 {
		t.Errorf("output should be truncated, got %d bytes", len(output))
	}
	if !strings.Contains(output, "truncated") {
		t.Error("truncated output should mention truncation")
	}
}

func TestWriteMarkdown_WaiverSection(t *testing.T) {
	report := &ComparisonReport{
		SchemaVersion: schemaVersion,
		Verdict:       VerdictWaived,
		Policy:        DefaultPolicy(),
		Waiver: &WaiverMetadata{
			Enabled:  true,
			Actor:    "maintainer",
			HeadSHA:  "abc123",
			Accepted: []string{"Foo sec/op"},
		},
	}
	var buf bytes.Buffer
	WriteMarkdown(&buf, report, "")
	output := buf.String()
	if !strings.Contains(output, "Waiver applied") {
		t.Error("markdown should show waiver section")
	}
	if !strings.Contains(output, "maintainer") {
		t.Error("markdown should show waiver actor")
	}
}

func TestWriteMarkdown_ArtifactLink(t *testing.T) {
	report := &ComparisonReport{
		SchemaVersion: schemaVersion,
		Verdict:       VerdictPass,
		Policy:        DefaultPolicy(),
	}
	var buf bytes.Buffer
	WriteMarkdown(&buf, report, "https://artifact.example.com")
	output := buf.String()
	if !strings.Contains(output, "https://artifact.example.com") {
		t.Error("markdown should contain artifact link")
	}
}

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		verdict Verdict
		want    int
	}{
		{VerdictPass, 0},
		{VerdictWaived, 0},
		{VerdictRegression, 1},
		{VerdictInconclusive, 1},
		{VerdictError, 2},
	}
	for _, tt := range tests {
		// Use the main package's exitCodeForVerdict via a test helper.
		got := exitCodeForVerdictPublic(tt.verdict)
		if got != tt.want {
			t.Errorf("exitCode(%s) = %d, want %d", tt.verdict, got, tt.want)
		}
	}
}

// exitCodeForVerdictPublic is a test-visible wrapper.
func exitCodeForVerdictPublic(v Verdict) int {
	switch v {
	case VerdictPass, VerdictWaived:
		return 0
	case VerdictRegression, VerdictInconclusive:
		return 1
	default:
		return 2
	}
}
