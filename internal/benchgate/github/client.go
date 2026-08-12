// Package github implements the trusted reporter helper for benchgate.
// It resolves PRs from workflow-run data, downloads and validates artifacts,
// reconstructs comments from validated JSON, and manages the one-shot
// waiver label. It never checks out or executes PR code.
package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	// HiddenMarker is the stable HTML comment that identifies a benchgate
	// sticky comment. The reporter creates or updates exactly one comment
	// containing this marker.
	HiddenMarker = "<!-- benchgate-report -->"

	// MaxArtifactSize is the maximum allowed decompressed artifact size in bytes (10 MB).
	MaxArtifactSize = 10 * 1024 * 1024

	// MaxArtifactFiles is the maximum number of files in a valid artifact archive.
	MaxArtifactFiles = 10
)

// Client wraps an HTTP client and GitHub API base URL.
type Client struct {
	HTTP      *http.Client
	BaseURL   string // e.g. "https://api.github.com"
	Token     string
	Repo      string // owner/repo
	UserAgent string
}

// NewClient creates a GitHub API client.
func NewClient(token, repo string) *Client {
	return &Client{
		HTTP:      http.DefaultClient,
		BaseURL:   "https://api.github.com",
		Token:     token,
		Repo:      repo,
		UserAgent: "benchgate-github-report",
	}
}

// WorkflowRun represents the relevant fields from a workflow_run event.
type WorkflowRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HeadBranch string `json:"head_branch"`
	HeadSHA    string `json:"head_sha"`
	RunAttempt int    `json:"run_attempt"`
	Event      string `json:"event"`
	HTMLURL    string `json:"html_url"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequests []PullRequest `json:"pull_requests"`
}

// PullRequest represents the relevant fields from a GitHub PR.
type PullRequest struct {
	Number int `json:"number"`
	Head   struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"base"`
}

// Artifact represents a workflow run artifact.
type Artifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ArchiveSize int64  `json:"size_in_bytes"`
	ArchiveURL  string `json:"archive_download_url"`
	WorkflowRun struct {
		ID      int64 `json:"id"`
		Attempt int   `json:"run_attempt"`
	} `json:"workflow_run"`
	ExpiresAt string `json:"expires_at"`
}

// Comment represents a PR comment.
type Comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

// Label represents a GitHub label.
type Label struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// --- API methods ---

func (c *Client) do(method, path string, body io.Reader) (*http.Response, error) {
	u := c.BaseURL + path
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.UserAgent)
	return c.HTTP.Do(req)
}

// ListArtifacts lists artifacts for a workflow run, filtered by name.
func (c *Client) ListArtifacts(runID int64, name string) ([]Artifact, error) {
	path := fmt.Sprintf("/repos/%s/actions/runs/%d/artifacts?per_page=100", c.Repo, runID)
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list artifacts: %s: %s", resp.Status, body)
	}
	var result struct {
		Artifacts []Artifact `json:"artifacts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode artifacts: %w", err)
	}
	var filtered []Artifact
	for _, a := range result.Artifacts {
		if a.Name == name {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

// DownloadArtifact downloads an artifact archive. The returned ReadCloser
// is a ZIP archive.
func (c *Client) DownloadArtifact(archiveURL string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", archiveURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download artifact: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download artifact: %s: %s", resp.Status, body)
	}
	return resp.Body, nil
}

// ListPRComments lists all issue comments on a PR.
func (c *Client) ListPRComments(prNumber int) ([]Comment, error) {
	path := fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", c.Repo, prNumber)
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list comments: %s: %s", resp.Status, body)
	}
	var comments []Comment
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return nil, fmt.Errorf("decode comments: %w", err)
	}
	return comments, nil
}

// CreateComment creates a new PR comment.
func (c *Client) CreateComment(prNumber int, body string) error {
	payload, _ := json.Marshal(map[string]string{"body": body})
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", c.Repo, prNumber)
	resp, err := c.do("POST", path, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create comment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create comment: %s: %s", resp.Status, body)
	}
	return nil
}

// UpdateComment updates an existing PR comment.
func (c *Client) UpdateComment(commentID int64, body string) error {
	payload, _ := json.Marshal(map[string]string{"body": body})
	path := fmt.Sprintf("/repos/%s/issues/comments/%d", c.Repo, commentID)
	resp, err := c.do("PATCH", path, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("update comment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update comment: %s: %s", resp.Status, body)
	}
	return nil
}

// FindStickyComment searches PR comments for one containing the hidden marker
// that was authored by a bot (not a human user). This prevents a user from
// hijacking the sticky comment slot by including the marker in their own comment.
func (c *Client) FindStickyComment(prNumber int) (*Comment, error) {
	comments, err := c.ListPRComments(prNumber)
	if err != nil {
		return nil, err
	}
	for i := range comments {
		if strings.Contains(comments[i].Body, HiddenMarker) && comments[i].User.Type == "Bot" {
			return &comments[i], nil
		}
	}
	return nil, nil
}

// PostOrUpdateComment creates or updates the sticky comment.
func (c *Client) PostOrUpdateComment(prNumber int, body string) error {
	existing, err := c.FindStickyComment(prNumber)
	if err != nil {
		return err
	}
	if existing != nil {
		return c.UpdateComment(existing.ID, body)
	}
	return c.CreateComment(prNumber, body)
}

// GetLabel checks if a label exists.
func (c *Client) GetLabel(name string) (*Label, error) {
	path := fmt.Sprintf("/repos/%s/labels/%s", c.Repo, url.PathEscape(name))
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("get label: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get label: %s: %s", resp.Status, body)
	}
	var label Label
	if err := json.NewDecoder(resp.Body).Decode(&label); err != nil {
		return nil, fmt.Errorf("decode label: %w", err)
	}
	return &label, nil
}

// CreateLabel creates a new label.
func (c *Client) CreateLabel(name, description string) error {
	payload, _ := json.Marshal(map[string]string{
		"name":        name,
		"description": description,
		"color":       "d73a4a",
	})
	path := fmt.Sprintf("/repos/%s/labels", c.Repo)
	resp, err := c.do("POST", path, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create label: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create label: %s: %s", resp.Status, body)
	}
	return nil
}

// RemoveLabel removes a label from a PR.
func (c *Client) RemoveLabel(prNumber int, name string) error {
	path := fmt.Sprintf("/repos/%s/issues/%d/labels/%s", c.Repo, prNumber, url.PathEscape(name))
	resp, err := c.do("DELETE", path, nil)
	if err != nil {
		return fmt.Errorf("remove label: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remove label: %s: %s", resp.Status, body)
	}
	return nil
}

// EnsureLabel creates the label if it does not exist.
func (c *Client) EnsureLabel(name, description string) error {
	existing, err := c.GetLabel(name)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	return c.CreateLabel(name, description)
}

// FindPRByHeadSHA finds a PR with the given head SHA via the search API.
// This is the fallback for fork runs where pull_requests is empty.
func (c *Client) FindPRByHeadSHA(headSHA string) (*PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/pulls?state=open&per_page=100", c.Repo)
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("list PRs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list PRs: %s: %s", resp.Status, body)
	}
	var prs []PullRequest
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return nil, fmt.Errorf("decode PRs: %w", err)
	}
	for i := range prs {
		if prs[i].Head.SHA == headSHA {
			return &prs[i], nil
		}
	}
	return nil, nil
}
