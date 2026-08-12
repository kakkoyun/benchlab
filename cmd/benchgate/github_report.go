package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kakkoyun/benchlab/internal/benchgate"
	"github.com/kakkoyun/benchlab/internal/benchgate/github"
)

// runGitHubReport implements `benchgate github-report`.
// It is the trusted helper invoked by the reusable reporter workflow.
// It never checks out or executes PR code.
func runGitHubReport(args []string) int {
	fs := flag.NewFlagSet("github-report", flag.ExitOnError)
	token := fs.String("token", "", "GitHub API token (reads GITHUB_TOKEN env if empty)")
	repo := fs.String("repo", "", "repository (owner/repo)")
	artifactName := fs.String("artifact-name", "", "artifact name prefix (default: benchgate-<run-id>-<attempt>)")
	runID := fs.Int64("run-id", 0, "workflow run ID")
	attempt := fs.Int("attempt", 1, "workflow run attempt")
	prNumber := fs.Int("pr", 0, "PR number (0 = resolve from workflow run)")
	baseSHA := fs.String("base-sha", "", "expected base SHA")
	headSHA := fs.String("head-sha", "", "expected head SHA")
	mergeSHA := fs.String("merge-sha", "", "expected merge SHA")
	artifactURL := fs.String("artifact-url", "", "URL to the workflow artifact for the comment link")
	waiverLabel := fs.String("waiver-label", benchgate.WaiverLabel, "waiver label name")
	waiverActor := fs.String("waiver-actor", "", "GitHub actor who applied the waiver")
	waiverHeadSHA := fs.String("waiver-head-sha", "", "head SHA the waiver applies to")
	eventPath := fs.String("event-path", "", "path to the workflow_run event payload (default: GITHUB_EVENT_PATH)")
	dryRun := fs.Bool("dry-run", false, "print the comment to stdout instead of posting")
	fs.Parse(args)

	if *token == "" {
		*token = os.Getenv("GITHUB_TOKEN")
	}
	if *token == "" && !*dryRun {
		fmt.Fprintln(os.Stderr, "github-report: -token or GITHUB_TOKEN is required (use -dry-run to skip)")
		return 2
	}
	if *repo == "" {
		fmt.Fprintln(os.Stderr, "github-report: -repo is required")
		return 2
	}
	if *eventPath == "" {
		*eventPath = os.Getenv("GITHUB_EVENT_PATH")
	}

	// Parse the workflow_run event if available.
	var run github.WorkflowRun
	if *eventPath != "" {
		data, err := os.ReadFile(*eventPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "github-report: read event: %v\n", err)
			return 2
		}
		if err := json.Unmarshal(data, &run); err != nil {
			fmt.Fprintf(os.Stderr, "github-report: parse event: %v\n", err)
			return 2
		}
		// Override run ID and attempt from event if not set.
		if *runID == 0 {
			*runID = run.ID
		}
		if *attempt == 0 {
			*attempt = run.RunAttempt
		}
		if *headSHA == "" {
			*headSHA = run.HeadSHA
		}
		if *repo == "" && run.Repository.FullName != "" {
			*repo = run.Repository.FullName
		}
	}

	if *runID == 0 {
		fmt.Fprintln(os.Stderr, "github-report: run-id is required (set -run-id or provide GITHUB_EVENT_PATH)")
		return 2
	}

	// Resolve PR.
	prNum := *prNumber
	if prNum == 0 && len(run.PullRequests) > 0 {
		prNum = run.PullRequests[0].Number
		if *baseSHA == "" {
			*baseSHA = run.PullRequests[0].Base.SHA
		}
	}
	if prNum == 0 && *headSHA != "" {
		// Fork fallback: search PRs by head SHA.
		client := github.NewClient(*token, *repo)
		pr, err := client.FindPRByHeadSHA(*headSHA)
		if err != nil {
			fmt.Fprintf(os.Stderr, "github-report: find PR by SHA: %v\n", err)
			return 2
		}
		if pr != nil {
			prNum = pr.Number
			if *baseSHA == "" {
				*baseSHA = pr.Base.SHA
			}
		}
	}
	if prNum == 0 {
		fmt.Fprintln(os.Stderr, "github-report: could not resolve PR number")
		return 2
	}

	// Determine artifact name.
	artName := *artifactName
	if artName == "" {
		artName = fmt.Sprintf("benchgate-%d-%d", *runID, *attempt)
	}

	client := github.NewClient(*token, *repo)

	// List artifacts for the run.
	artifacts, err := client.ListArtifacts(*runID, artName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "github-report: list artifacts: %v\n", err)
		return 2
	}

	// Select exactly one artifact.
	artifact, err := github.SelectArtifact(artifacts, *runID, *attempt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "github-report: %v\n", err)
		return 2
	}

	// Validate the artifact.
	expect := github.Expectation{
		Repository: *repo,
		RunID:      fmt.Sprintf("%d", *runID),
		Attempt:    *attempt,
		PRNumber:   prNum,
		BaseSHA:    *baseSHA,
		HeadSHA:    *headSHA,
		MergeSHA:   *mergeSHA,
	}
	validation, err := github.ValidateArtifact(client, artifact, expect)
	if err != nil {
		fmt.Fprintf(os.Stderr, "github-report: validate artifact: %v\n", err)
		return 2
	}

	report := validation.Report

	// Apply waiver if the label event triggered this report.
	if *waiverActor != "" && *waiverHeadSHA != "" {
		if err := benchgate.ValidateWaiverEvent("labeled", *waiverLabel, *waiverHeadSHA); err != nil {
			fmt.Fprintf(os.Stderr, "github-report: waiver validation: %v\n", err)
		} else {
			benchgate.ApplyWaiver(report, benchgate.WaiverMetadata{
				Enabled: true,
				Actor:   *waiverActor,
				Label:   *waiverLabel,
				HeadSHA: *waiverHeadSHA,
			})
		}
	}

	// Build comment from validated JSON.
	comment := github.BuildComment(report, *artifactURL)

	if *dryRun {
		fmt.Print(comment)
		return exitCodeForVerdict(report.Verdict)
	}

	// Ensure the waiver label exists.
	if err := client.EnsureLabel(benchgate.WaiverLabel, benchgate.WaiverDescription); err != nil {
		fmt.Fprintf(os.Stderr, "github-report: ensure label: %v\n", err)
		// Non-fatal: continue to post comment.
	}

	// Post or update the sticky comment.
	if err := client.PostOrUpdateComment(prNum, comment); err != nil {
		fmt.Fprintf(os.Stderr, "github-report: post comment: %v\n", err)
		// Leave the gate result untouched if comment delivery fails.
		return exitCodeForVerdict(report.Verdict)
	}

	// If the waiver was applied and the run passed (WAIVED), remove the label.
	if report.Verdict == benchgate.VerdictWaived {
		if err := client.RemoveLabel(prNum, benchgate.WaiverLabel); err != nil {
			fmt.Fprintf(os.Stderr, "github-report: remove label: %v\n", err)
		}
	}

	return exitCodeForVerdict(report.Verdict)
}

// suppressUnused ensures strings import is retained for potential future use.
var _ = strings.TrimSpace
