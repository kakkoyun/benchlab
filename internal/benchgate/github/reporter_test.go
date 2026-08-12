package github

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kakkoyun/benchlab/internal/benchgate"
)

// mockGitHub sets up a test GitHub API server.
type mockGitHub struct {
	server      *httptest.Server
	artifacts   []Artifact
	comments    []Comment
	labels      map[string]bool
	prs         []PullRequest
	artifactZip []byte
}

func newMockGitHub(t *testing.T, artifactZip []byte) *mockGitHub {
	t.Helper()
	m := &mockGitHub{
		labels:      make(map[string]bool),
		artifactZip: artifactZip,
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

func (m *mockGitHub) close() { m.server.Close() }

func (m *mockGitHub) handle(w http.ResponseWriter, r *http.Request) {
	// Order matters: more specific paths first.
	switch {
	case strings.Contains(r.URL.Path, "/issues/comments/"):
		// Update existing comment (PATCH)
		if r.Method == "PATCH" {
			body, _ := io.ReadAll(r.Body)
			var c struct{ Body string }
			json.Unmarshal(body, &c)
			for i := range m.comments {
				if strings.Contains(r.URL.Path, fmt.Sprintf("/comments/%d", m.comments[i].ID)) {
					m.comments[i].Body = c.Body
				}
			}
			w.WriteHeader(http.StatusOK)
		}

	case strings.Contains(r.URL.Path, "/issues/") && strings.Contains(r.URL.Path, "/comments"):
		switch r.Method {
		case "GET":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(m.comments)
		case "POST":
			body, _ := io.ReadAll(r.Body)
			var c struct{ Body string }
			json.Unmarshal(body, &c)
			m.comments = append(m.comments, Comment{ID: int64(len(m.comments) + 1), Body: c.Body})
			w.WriteHeader(http.StatusCreated)
		}

	case strings.Contains(r.URL.Path, "/labels"):
		switch r.Method {
		case "GET":
			name := strings.TrimPrefix(r.URL.Path, "/repos/test/labels/")
			if m.labels[name] {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(Label{Name: name})
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		case "POST":
			w.WriteHeader(http.StatusCreated)
		}

	case strings.Contains(r.URL.Path, "/pulls"):
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m.prs)

	case strings.Contains(r.URL.Path, "/archive_download"):
		w.Header().Set("Content-Type", "application/zip")
		w.Write(m.artifactZip)

	case strings.Contains(r.URL.Path, "/actions/runs/") && strings.Contains(r.URL.Path, "/artifacts"):
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"artifacts": m.artifacts})

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func makeArtifactZip(t *testing.T, report *benchgate.ComparisonReport) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	reportJSON, _ := json.Marshal(report)
	files := map[string][]byte{
		"base.txt":      []byte("base output"),
		"candidate.txt": []byte("candidate output"),
		"report.json":   reportJSON,
		"report.md":     []byte("## markdown report"),
	}
	for name, data := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data)
	}
	zw.Close()
	return buf.Bytes()
}

func makeValidReport() *benchgate.ComparisonReport {
	return &benchgate.ComparisonReport{
		SchemaVersion: "benchgate/v0.2.0",
		Verdict:       benchgate.VerdictPass,
		Policy:        benchgate.DefaultPolicy(),
		Identity: benchgate.Identity{
			Repository: "test/repo",
			PRNumber:   42,
			BaseSHA:    "abc123",
			HeadSHA:    "def456",
			MergeSHA:   "ghi789",
		},
	}
}

func newTestClient(m *mockGitHub) *Client {
	return &Client{
		HTTP:      m.server.Client(),
		BaseURL:   m.server.URL,
		Token:     "test-token",
		Repo:      "test/repo",
		UserAgent: "test",
	}
}

// --- Tests ---

func TestValidateArtifact_Valid(t *testing.T) {
	report := makeValidReport()
	zipData := makeArtifactZip(t, report)
	m := newMockGitHub(t, zipData)
	defer m.close()

	m.artifacts = []Artifact{{
		ID:          1,
		Name:        "benchgate-100-1",
		ArchiveURL:  m.server.URL + "/archive_download",
		ArchiveSize: int64(len(zipData)),
		WorkflowRun: struct {
			ID      int64 `json:"id"`
			Attempt int   `json:"run_attempt"`
		}{ID: 100, Attempt: 1},
	}}

	client := newTestClient(m)
	artifact, err := SelectArtifact(m.artifacts, 100, 1)
	if err != nil {
		t.Fatalf("SelectArtifact: %v", err)
	}

	v, err := ValidateArtifact(client, artifact, Expectation{
		Repository: "test/repo",
		PRNumber:   42,
		BaseSHA:    "abc123",
		HeadSHA:    "def456",
		MergeSHA:   "ghi789",
	})
	if err != nil {
		t.Fatalf("ValidateArtifact: %v", err)
	}
	if v.Report.Verdict != benchgate.VerdictPass {
		t.Errorf("expected PASS, got %s", v.Report.Verdict)
	}
	if v.FileCount != 4 {
		t.Errorf("expected 4 files, got %d", v.FileCount)
	}
}

func TestValidateArtifact_SchemaMismatch(t *testing.T) {
	report := makeValidReport()
	report.SchemaVersion = "benchgate/v0.1.0"
	zipData := makeArtifactZip(t, report)
	m := newMockGitHub(t, zipData)
	defer m.close()

	m.artifacts = []Artifact{{
		ID:          1,
		Name:        "benchgate-100-1",
		ArchiveURL:  m.server.URL + "/archive_download",
		ArchiveSize: int64(len(zipData)),
		WorkflowRun: struct {
			ID      int64 `json:"id"`
			Attempt int   `json:"run_attempt"`
		}{ID: 100, Attempt: 1},
	}}

	client := newTestClient(m)
	artifact, _ := SelectArtifact(m.artifacts, 100, 1)
	_, err := ValidateArtifact(client, artifact, Expectation{})
	if err == nil {
		t.Fatal("expected schema mismatch error")
	}
	if !strings.Contains(err.Error(), "schema version") {
		t.Errorf("expected schema version error, got: %v", err)
	}
}

func TestValidateArtifact_IdentityMismatch(t *testing.T) {
	report := makeValidReport()
	zipData := makeArtifactZip(t, report)
	m := newMockGitHub(t, zipData)
	defer m.close()

	m.artifacts = []Artifact{{
		ID:          1,
		Name:        "benchgate-100-1",
		ArchiveURL:  m.server.URL + "/archive_download",
		ArchiveSize: int64(len(zipData)),
		WorkflowRun: struct {
			ID      int64 `json:"id"`
			Attempt int   `json:"run_attempt"`
		}{ID: 100, Attempt: 1},
	}}

	client := newTestClient(m)
	artifact, _ := SelectArtifact(m.artifacts, 100, 1)
	_, err := ValidateArtifact(client, artifact, Expectation{
		HeadSHA: "wrong-sha",
	})
	if err == nil {
		t.Fatal("expected identity mismatch error")
	}
}

func TestValidateArtifact_MissingFile(t *testing.T) {
	// Create a zip missing report.md.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	report := makeValidReport()
	reportJSON, _ := json.Marshal(report)
	files := map[string][]byte{
		"base.txt":      []byte("base"),
		"candidate.txt": []byte("cand"),
		"report.json":   reportJSON,
	}
	for name, data := range files {
		f, _ := zw.Create(name)
		f.Write(data)
	}
	zw.Close()

	m := newMockGitHub(t, buf.Bytes())
	defer m.close()
	m.artifacts = []Artifact{{
		ID:          1,
		Name:        "benchgate-100-1",
		ArchiveURL:  m.server.URL + "/archive_download",
		ArchiveSize: int64(buf.Len()),
		WorkflowRun: struct {
			ID      int64 `json:"id"`
			Attempt int   `json:"run_attempt"`
		}{ID: 100, Attempt: 1},
	}}

	client := newTestClient(m)
	artifact, _ := SelectArtifact(m.artifacts, 100, 1)
	_, err := ValidateArtifact(client, artifact, Expectation{})
	if err == nil {
		t.Fatal("expected missing file error")
	}
	if !strings.Contains(err.Error(), "missing required file") {
		t.Errorf("expected missing file error, got: %v", err)
	}
}

func TestValidateArtifact_Oversized(t *testing.T) {
	report := makeValidReport()
	zipData := makeArtifactZip(t, report)
	m := newMockGitHub(t, zipData)
	defer m.close()

	m.artifacts = []Artifact{{
		ID:          1,
		Name:        "benchgate-100-1",
		ArchiveURL:  m.server.URL + "/archive_download",
		ArchiveSize: MaxArtifactSize + 1,
		WorkflowRun: struct {
			ID      int64 `json:"id"`
			Attempt int   `json:"run_attempt"`
		}{ID: 100, Attempt: 1},
	}}

	client := newTestClient(m)
	artifact, _ := SelectArtifact(m.artifacts, 100, 1)
	_, err := ValidateArtifact(client, artifact, Expectation{})
	if err == nil {
		t.Fatal("expected oversized error")
	}
}

func TestSelectArtifact_NoMatch(t *testing.T) {
	artifacts := []Artifact{{ID: 1, WorkflowRun: struct {
		ID      int64 `json:"id"`
		Attempt int   `json:"run_attempt"`
	}{ID: 100, Attempt: 1}}}
	_, err := SelectArtifact(artifacts, 999, 1)
	if err == nil {
		t.Fatal("expected no match error")
	}
}

func TestSelectArtifact_Duplicate(t *testing.T) {
	a := Artifact{WorkflowRun: struct {
		ID      int64 `json:"id"`
		Attempt int   `json:"run_attempt"`
	}{ID: 100, Attempt: 1}}
	artifacts := []Artifact{a, a}
	_, err := SelectArtifact(artifacts, 100, 1)
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestBuildComment_HasMarker(t *testing.T) {
	report := makeValidReport()
	comment := BuildComment(report, "https://artifact-url")
	if !strings.HasPrefix(comment, HiddenMarker) {
		t.Error("comment should start with hidden marker")
	}
}

func TestBuildComment_EscapesMarkdown(t *testing.T) {
	report := makeValidReport()
	report.Identity.HeadSHA = "abc`malicious"
	comment := BuildComment(report, "")
	if strings.Contains(comment, "abc`malicious") && !strings.Contains(comment, "abc\\`malicious") {
		t.Error("backtick should be escaped")
	}
}

func TestPostOrUpdateComment_CreatesNew(t *testing.T) {
	m := newMockGitHub(t, nil)
	defer m.close()
	client := newTestClient(m)

	if err := client.PostOrUpdateComment(42, HiddenMarker+"\n\ntest comment"); err != nil {
		t.Fatalf("PostOrUpdateComment: %v", err)
	}
	if len(m.comments) != 1 {
		t.Errorf("expected 1 comment, got %d", len(m.comments))
	}
}

func TestPostOrUpdateComment_UpdatesExisting(t *testing.T) {
	m := newMockGitHub(t, nil)
	defer m.close()
	m.comments = []Comment{{ID: 5, Body: HiddenMarker + "\n\nold comment"}}
	client := newTestClient(m)

	if err := client.PostOrUpdateComment(42, HiddenMarker+"\n\nnew comment"); err != nil {
		t.Fatalf("PostOrUpdateComment: %v", err)
	}
	if len(m.comments) != 1 {
		t.Errorf("expected 1 comment (updated), got %d", len(m.comments))
	}
	if !strings.Contains(m.comments[0].Body, "new comment") {
		t.Error("comment should be updated")
	}
}

func TestEnsureLabel_CreatesIfMissing(t *testing.T) {
	m := newMockGitHub(t, nil)
	defer m.close()
	client := newTestClient(m)

	if err := client.EnsureLabel("benchgate:accept-regression", "test desc"); err != nil {
		t.Fatalf("EnsureLabel: %v", err)
	}
}

func TestFindPRByHeadSHA_Found(t *testing.T) {
	m := newMockGitHub(t, nil)
	defer m.close()
	m.prs = []PullRequest{{Number: 42, Head: struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	}{SHA: "abc123"}}}
	client := newTestClient(m)

	pr, err := client.FindPRByHeadSHA("abc123")
	if err != nil {
		t.Fatalf("FindPRByHeadSHA: %v", err)
	}
	if pr == nil || pr.Number != 42 {
		t.Error("expected PR 42")
	}
}

func TestFindPRByHeadSHA_NotFound(t *testing.T) {
	m := newMockGitHub(t, nil)
	defer m.close()
	m.prs = []PullRequest{{Number: 42, Head: struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	}{SHA: "other"}}}
	client := newTestClient(m)

	pr, err := client.FindPRByHeadSHA("abc123")
	if err != nil {
		t.Fatalf("FindPRByHeadSHA: %v", err)
	}
	if pr != nil {
		t.Error("expected nil PR")
	}
}

func TestIsStaleRun(t *testing.T) {
	if !IsStaleRun(1, 3) {
		t.Error("attempt 1 should be stale when 3 is current")
	}
	if IsStaleRun(3, 3) {
		t.Error("attempt 3 should not be stale when 3 is current")
	}
}
