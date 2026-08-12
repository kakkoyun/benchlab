---
name: benchstat-gate
description: |
  Runs Go benchmarks, compares base and candidate results with Mann-Whitney U test
  statistics via golang.org/x/perf, and emits a PASS/REGRESSION/INCONCLUSIVE/WAIVED/ERROR
  verdict. Supports GitHub Actions PR gates, one-shot waivers, and counterbalanced
  collection.
  USE WHEN: "benchmark regression gate", "benchmark CI gate", "compare benchmark results",
  "benchstat alternative", "benchmark A/B comparison", "GitHub Actions benchmark gate",
  "benchmark waiver", "benchgate".
license: MIT
compatibility: Requires Go 1.25 or newer on PATH. Uses golang.org/x/perf for statistics.
disable-model-invocation: false
---

# benchstat-gate

Run `honest-benchmark` first. Source structure must be correct before sample stability means anything. This skill is the second stage: stabilize repeated samples and compare them statistically. If variance remains high, hand off to `diagnose-noisy-bench` for environment inspection.

`benchgate` is a Go CLI that wraps `go test -bench` to collect benchmark samples,
compare base and candidate results with Mann-Whitney U test statistics
(`golang.org/x/perf/benchmath.AssumeNothing`), and emit a verdict that can
block merges in CI.

## Commands

```bash
# Collect and compare in one step (requires a base worktree)
benchgate run -base-dir ../base-worktree -pkg ./... -count 10

# Compare two pre-collected result files
benchgate compare -base before.txt -candidate after.txt

# Trusted GitHub PR comment helper (used by the reporter workflow)
benchgate github-report -repo owner/repo -token $GITHUB_TOKEN

# Legacy CV-only mode (backward compatible)
benchgate -pkg ./... -count 10 -cv-threshold 5.0
benchgate -pkg ./... -count 10 -save before.txt
benchgate -pkg ./... -count 10 -baseline before.txt
```

## Run the CLI

Install it once for repeated use:

```bash
go install github.com/kakkoyun/benchlab/cmd/benchgate@latest
```

Or run it immediately without installing:

```bash
go run github.com/kakkoyun/benchlab/cmd/benchgate@latest compare -base before.txt -candidate after.txt
```

## Statistics and defaults

- Match by package, full benchmark name, CPU/GOMAXPROCS variant, and normalized unit
- Gate `sec/op`, `B/op`, and `allocs/op`
- Default to 10 samples per side, alpha 0.05, and maximum CV 5%
- Fail runtime only when the candidate is more than 10% slower and p < 0.05
- Fail bytes or allocation count on any increase when p < 0.05
- Treat an increase from zero as an infinite allocation regression
- Pass deltas exactly at their threshold
- Report improvements without failing

### Verdicts

| Verdict | Exit code | Meaning |
| --- | --- | --- |
| `PASS` | 0 | All gated series passed or improved |
| `REGRESSION` | 1 | At least one gated series regressed beyond threshold |
| `INCONCLUSIVE` | 1 | At least one series could not be decided (insufficient samples, high CV, missing unit, or statistical warning) |
| `WAIVED` | 0 | A regression was accepted via the one-shot waiver |
| `ERROR` | 2 | Execution, configuration, or parsing error |

## GitHub Actions

Add a read-only PR gate:

```yaml
- uses: kakkoyun/benchlab/actions/benchgate@v0.2.0
  with:
    count: '10'
    runtime-threshold: '10'
    cv-threshold: '5'
```

See `docs/benchgate-github-actions.md` for the full setup, including the trusted
reporter workflow, permissions, fork behavior, branch protection, and the
one-shot `benchgate:accept-regression` waiver label.

## Why benchgate uses worktrees, counterbalanced batches, and pinned x/perf

- **Worktrees:** the base is collected in a detached git worktree at the exact
  base SHA. The candidate uses the checked-out merge commit. This avoids
  checking out the base branch over the candidate working tree.
- **Counterbalanced batches:** collection order is base half, candidate half,
  candidate remainder, base remainder. This counterbalances temporal drift.
- **`-run '^$'` and `-p=1`:** tests are excluded from benchmark collection.
  Packages do not compete with each other.
- **Pinned `golang.org/x/perf`:** the comparison uses `benchfmt` for parsing
  and `benchmath.AssumeNothing` for the Mann-Whitney U test, all in process.
  No `benchstat@latest` install, no regex parsing of human-readable output.
- **CV guard:** a series with CV > 5% is INCONCLUSIVE, not a false pass or
  false fail.
- **Separate reporter:** the PR comment is posted by a trusted `workflow_run`
  reporter that never checks out or executes PR code.

## Flags (compare and run)

| Flag | Default | Description |
| ------ | --------- | ------------- |
| `-pkg` | `./...` | Package pattern passed to `go test` |
| `-bench` | `.` | Benchmark regexp |
| `-count` | `10` | Number of samples per side |
| `-benchtime` | `1s` | Per-iteration time budget |
| `-cpu` | — | Optional `-cpu` value |
| `-setup` | — | Setup command run once per worktree |
| `-base-dir` | — | Base worktree directory (run mode) |
| `-cand-dir` | cwd | Candidate worktree directory (run mode) |
| `-base` | — | Path to saved base output (compare mode) |
| `-candidate` | — | Path to saved candidate output (compare mode) |
| `-runtime-threshold` | `10.0` | Runtime regression threshold percent |
| `-bytes-threshold` | `0.0` | Bytes regression threshold percent |
| `-allocs-threshold` | `0.0` | Allocs regression threshold percent |
| `-cv-threshold` | `5.0` | Max acceptable CV percent |
| `-alpha` | `0.05` | Significance alpha |
| `-min-samples` | `10` | Minimum samples per side |
| `-json-out` | — | Write JSON report to path |
| `-markdown-out` | — | Write Markdown report to path |
| `-allow-env-mismatch` | false | Allow comparison across incompatible environments |

## CV > 5%? Fix the environment first

High CV means the OS scheduler, frequency scaling, or competing processes are
drowning the signal. Before tuning code, stabilise the measurement:

- **Linux:** pin to an isolated core and disable frequency scaling:

  ```bash
  perflock -governor performance taskset -c 2 go test -bench=. -count=20 ./...
  ```

- **macOS:** close background apps, disable Spotlight indexing, and prefer
  `-benchtime 5s` to amortise timer jitter over longer runs.
- **CI:** run benchmarks on a dedicated bare-metal runner, not a shared VM.

Only after CV drops below your threshold should you treat benchmark deltas as
signal rather than noise.
