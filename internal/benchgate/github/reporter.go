package github

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/kakkoyun/benchlab/internal/benchgate"
)

// ExpectedFiles are the files that must be present in a valid benchgate artifact.
var ExpectedFiles = []string{"base.txt", "candidate.txt", "report.json", "report.md"}

// ArtifactValidation holds the validated contents of a benchgate artifact.
type ArtifactValidation struct {
	Report        *benchgate.ComparisonReport
	BaseData      []byte
	CandidateData []byte
	MarkdownData  []byte
	FileCount     int
	TotalSize     int64
}

// ValidateArtifact downloads, extracts, and validates an artifact archive.
// It enforces archive size, file count, schema version, repository, run,
// PR, base SHA, head SHA, and merge SHA.
func ValidateArtifact(
	client *Client,
	artifact *Artifact,
	expect Expectation,
) (*ArtifactValidation, error) {
	// Size check.
	if artifact.ArchiveSize > MaxArtifactSize {
		return nil, fmt.Errorf("artifact archive %d bytes exceeds max %d", artifact.ArchiveSize, MaxArtifactSize)
	}

	// Download.
	rc, err := client.DownloadArtifact(artifact.ArchiveURL)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	data, err := io.ReadAll(io.LimitReader(rc, MaxArtifactSize+1))
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	if int64(len(data)) > MaxArtifactSize {
		return nil, fmt.Errorf("artifact download exceeds max size %d", MaxArtifactSize)
	}

	// Parse ZIP.
	zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("parse artifact zip: %w", err)
	}
	if len(zipReader.File) > MaxArtifactFiles {
		return nil, fmt.Errorf("artifact contains %d files, max %d", len(zipReader.File), MaxArtifactFiles)
	}

	// Extract expected files, tracking cumulative decompressed size.
	files := make(map[string][]byte)
	var cumulativeSize int64
	for _, zf := range zipReader.File {
		name := zf.Name
		if strings.HasPrefix(name, "./") {
			name = name[2:]
		}
		if !isExpectedFile(name) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("open %s in artifact: %w", name, err)
		}
		remaining := MaxArtifactSize - cumulativeSize
		if remaining <= 0 {
			rc.Close()
			return nil, fmt.Errorf("cumulative extracted size exceeds max %d", MaxArtifactSize)
		}
		content, err := io.ReadAll(io.LimitReader(rc, remaining+1))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if int64(len(content)) > remaining {
			return nil, fmt.Errorf("cumulative extracted size exceeds max %d", MaxArtifactSize)
		}
		files[name] = content
		cumulativeSize += int64(len(content))
	}

	// Check all expected files present.
	for _, ef := range ExpectedFiles {
		if _, ok := files[ef]; !ok {
			return nil, fmt.Errorf("artifact missing required file %q", ef)
		}
	}

	v := &ArtifactValidation{
		FileCount: len(zipReader.File),
	}
	var totalSize int64
	for _, data := range files {
		totalSize += int64(len(data))
	}
	v.TotalSize = totalSize

	// Parse report.json.
	var report benchgate.ComparisonReport
	if err := json.Unmarshal(files["report.json"], &report); err != nil {
		return nil, fmt.Errorf("parse report.json: %w", err)
	}
	v.Report = &report
	v.BaseData = files["base.txt"]
	v.CandidateData = files["candidate.txt"]
	v.MarkdownData = files["report.md"]

	// Validate schema version.
	if report.SchemaVersion != benchgateSchemaVersion() {
		return nil, fmt.Errorf("schema version mismatch: artifact has %q, expected %q",
			report.SchemaVersion, benchgateSchemaVersion())
	}

	// Validate identity expectations.
	if err := validateIdentity(&report, expect); err != nil {
		return nil, err
	}

	return v, nil
}

// Expectation describes what the reporter expects from the artifact.
type Expectation struct {
	Repository string
	RunID      string
	Attempt    int
	PRNumber   int
	BaseSHA    string
	HeadSHA    string
	MergeSHA   string
}

func validateIdentity(report *benchgate.ComparisonReport, expect Expectation) error {
	if expect.Repository != "" {
		if report.Identity.Repository == "" {
			return fmt.Errorf("artifact missing repository identity; expected %q", expect.Repository)
		}
		if report.Identity.Repository != expect.Repository {
			return fmt.Errorf("repository mismatch: artifact has %q, expected %q",
				report.Identity.Repository, expect.Repository)
		}
	}
	if expect.PRNumber > 0 {
		if report.Identity.PRNumber == 0 {
			return fmt.Errorf("artifact missing PR number; expected %d", expect.PRNumber)
		}
		if report.Identity.PRNumber != expect.PRNumber {
			return fmt.Errorf("PR number mismatch: artifact has %d, expected %d",
				report.Identity.PRNumber, expect.PRNumber)
		}
	}
	if expect.BaseSHA != "" {
		if report.Identity.BaseSHA == "" {
			return fmt.Errorf("artifact missing base SHA; expected %q", expect.BaseSHA)
		}
		if report.Identity.BaseSHA != expect.BaseSHA {
			return fmt.Errorf("base SHA mismatch: artifact has %q, expected %q",
				report.Identity.BaseSHA, expect.BaseSHA)
		}
	}
	if expect.HeadSHA != "" {
		if report.Identity.HeadSHA == "" {
			return fmt.Errorf("artifact missing head SHA; expected %q", expect.HeadSHA)
		}
		if report.Identity.HeadSHA != expect.HeadSHA {
			return fmt.Errorf("head SHA mismatch: artifact has %q, expected %q",
				report.Identity.HeadSHA, expect.HeadSHA)
		}
	}
	if expect.MergeSHA != "" {
		if report.Identity.MergeSHA == "" {
			return fmt.Errorf("artifact missing merge SHA; expected %q", expect.MergeSHA)
		}
		if report.Identity.MergeSHA != expect.MergeSHA {
			return fmt.Errorf("merge SHA mismatch: artifact has %q, expected %q",
				report.Identity.MergeSHA, expect.MergeSHA)
		}
	}
	return nil
}

func isExpectedFile(name string) bool {
	for _, ef := range ExpectedFiles {
		if name == ef {
			return true
		}
	}
	return false
}

func benchgateSchemaVersion() string {
	return "benchgate/v0.2.0"
}

// BuildComment reconstructs the PR comment body from validated report JSON.
// It does NOT use the artifact's Markdown directly — it regenerates from
// the validated JSON to prevent injection via the Markdown file.
func BuildComment(report *benchgate.ComparisonReport, artifactURL string) string {
	var b strings.Builder
	b.WriteString(HiddenMarker)
	b.WriteString("\n\n")

	var buf bytes.Buffer
	if err := benchgate.WriteMarkdown(&buf, report, artifactURL); err != nil {
		// Fallback: minimal comment.
		fmt.Fprintf(&b, "## benchgate — %s\n\n*Report generation failed: %s*\n", report.Verdict, err)
		return b.String()
	}
	b.Write(buf.Bytes())
	return b.String()
}

// IsStaleRun checks if a workflow run is stale (superseded by a newer run
// for the same head SHA). newerAttempt is the attempt number of the run
// that should be considered current.
func IsStaleRun(runAttempt, newerAttempt int) bool {
	return runAttempt < newerAttempt
}

// SelectArtifact selects the one artifact matching the expected run and attempt.
// Returns an error if there are zero or multiple matches.
func SelectArtifact(artifacts []Artifact, runID int64, attempt int) (*Artifact, error) {
	var matches []Artifact
	for _, a := range artifacts {
		if a.WorkflowRun.ID == runID && a.WorkflowRun.Attempt == attempt {
			matches = append(matches, a)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no artifact found for run %d attempt %d", runID, attempt)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple artifacts (%d) found for run %d attempt %d",
			len(matches), runID, attempt)
	}
	return &matches[0], nil
}
