# benchgate GitHub Actions regression gate

This document explains how to use benchgate as a GitHub Actions regression gate that runs on every pull request, posts a PR comment, and supports a one-shot maintainer waiver.

## Overview

Benchgate keeps the basic loop from the [OneUptime post](https://github.com/oneuptime/oneuptime/blob/master/_doc/observability/go-benchmarks.md): collect repeated base and candidate results, compare them with benchstat-style statistics, write a job summary, and upload the raw files. It replaces the fragile parts with a secure, fail-closed integration.

### What benchgate keeps from the OneUptime post

- `-count=10` as the minimum default sample count
- `-benchmem` and gates for runtime, bytes, and allocation count
- Base and candidate measurements on the same runner
- Mann-Whitney significance testing through `golang.org/x/perf`
- Actions job summaries and uploaded raw benchmark files
- Sub-benchmark and CPU-variant names as distinct benchmark series

### What benchgate does differently

- **Worktrees, not checkout-over.** The base is collected in a detached git worktree at the exact base SHA. The candidate uses the checked-out merge commit. Result files are stored under `$RUNNER_TEMP`, never inside either source tree.
- **In-process comparison, not benchstat subprocess.** `golang.org/x/perf` is pinned in `go.mod`. The comparison engine uses `benchfmt` for parsing and `benchmath.AssumeNothing` for the Mann-Whitney U test. No `benchstat@latest` install, no regex parsing of human-readable output, no missing `pipefail`.
- **`-run '^$'` and `-p=1`.** Tests are excluded from benchmark collection. Packages do not compete with each other.
- **Counterbalanced batches.** Collection order is: base half, candidate half, candidate remainder, base remainder. This counterbalances temporal drift on shared runners.
- **CV guard.** A series with CV > 5% is INCONCLUSIVE, not a false pass or false fail.
- **Separate reporter.** The PR comment is posted by a trusted `workflow_run` reporter that never checks out or executes PR code.

## Setup

### 1. The gate workflow

Add `.github/workflows/benchgate.yml` (see `examples/benchgate/github-actions/benchgate.yml`):

```yaml
name: benchgate

on:
  pull_request:
    types: [opened, synchronize, reopened, ready_for_review, labeled]

permissions:
  contents: read

concurrency:
  group: benchgate-${{ github.event.pull_request.number }}
  cancel-in-progress: true

jobs:
  benchgate:
    name: benchgate
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          persist-credentials: false
          fetch-depth: 0
      - uses: kakkoyun/benchlab/actions/benchgate@v0.2.0
        with:
          go-version: stable
          count: '10'
```

The job and required check name is fixed as `benchgate`.

### 2. The reporter workflow

Add `.github/workflows/benchgate-reporter.yml` (see `examples/benchgate/github-actions/benchgate-reporter.yml`):

```yaml
name: benchgate-reporter

on:
  workflow_run:
    workflows: [benchgate]
    types: [completed]

permissions:
  actions: read
  contents: read
  pull-requests: read
  issues: write

jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - name: Build benchgate
        shell: bash
        run: |
          git clone --depth 1 https://github.com/kakkoyun/benchlab.git "$RUNNER_TEMP/benchlab"
          cd "$RUNNER_TEMP/benchlab"
          git fetch --depth 1 origin v0.2.0
          git checkout v0.2.0
          go build -o "$RUNNER_TEMP/benchgate" ./cmd/benchgate
      - name: Report
        shell: bash
        env:
          GITHUB_EVENT_PATH: ${{ env.GITHUB_EVENT_PATH }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          "$RUNNER_TEMP/benchgate" github-report -repo "$GITHUB_REPOSITORY" -token "$GITHUB_TOKEN"
```

The reporter never checks out or executes PR code. It downloads the artifact, validates it, and reconstructs the comment from the validated JSON.

## Permissions

### Gate workflow

```yaml
permissions:
  contents: read
```

The gate receives no secrets and no write token. It only needs to read the repository to check out the merge commit and create a base worktree.

**Fork PRs:** the gate runs untrusted PR code. Use ephemeral or isolated runners. Do not pass secrets through the `setup` input.

### Reporter workflow

```yaml
permissions:
  actions: read       # download artifacts
  contents: read      # clone benchlab for the helper
  pull-requests: read # resolve PR metadata
  issues: write        # post and update comments, manage labels
```

The reporter uses `issues: write` to create/update the sticky PR comment and to manage the waiver label.

## Inputs

| Input | Default | Description |
| --- | --- | --- |
| `pkg` | `./...` | Package pattern to benchmark |
| `bench` | `.` | Benchmark regexp |
| `workdir` | `.` | Working directory for the candidate checkout |
| `setup` | — | Setup command run once per worktree |
| `count` | `10` | Samples per side |
| `benchtime` | `1s` | Per-iteration time budget |
| `cpu` | — | Optional `-cpu` value (e.g. `1,2,4`) |
| `runtime-threshold` | `10` | Runtime regression threshold percent |
| `bytes-threshold` | `0` | Bytes regression threshold percent |
| `allocs-threshold` | `0` | Allocs regression threshold percent |
| `cv-threshold` | `5` | Max acceptable CV percent |
| `alpha` | `0.05` | Significance alpha |
| `min-samples` | `10` | Minimum samples per side |
| `retention-days` | `14` | Artifact retention |
| `go-version` | `stable` | Go version for both sides |

## Verdicts

| Verdict | Exit code | Meaning |
| --- | --- | --- |
| `PASS` | 0 | All gated series passed or improved |
| `REGRESSION` | 1 | At least one gated series regressed beyond threshold |
| `INCONCLUSIVE` | 1 | At least one series could not be decided |
| `WAIVED` | 0 | A regression was accepted via the one-shot waiver |
| `ERROR` | 2 | Execution, configuration, or parsing error |

## Waiver lifecycle

Use the `benchgate:accept-regression` label to accept a regression.

1. A maintainer adds the `benchgate:accept-regression` label to the PR. The label is created automatically if it does not exist, with a description stating it applies to one PR head only.
2. Only a `labeled` event for that exact label enables a waiver. There is no direct boolean waiver input.
3. The waiver changes only `REGRESSION` to `WAIVED`. It cannot override `INCONCLUSIVE` or `ERROR`.
4. The sticky comment lists every accepted runtime or allocation regression.
5. After a valid waived run passes, the trusted reporter removes the label.
6. Any later commit runs unwaived. A stale label also grants nothing unless a new matching `labeled` event occurs.

The PR description or review discussion carries the human reason for accepting the tradeoff. The report records a fixed note, actor, label, and SHA.

## Branch protection

After confirming a normal PR run exposes the exact `benchgate` check name:

1. Add strict `main` branch protection requiring the `benchgate` check.
2. Keep admin bypass and current review settings unchanged.
3. Do not add the verify matrix to branch protection in this release.

## Noise

Shared GitHub Actions runners (`ubuntu-latest`) are multi-tenant. A 10% regression can vanish into shared-runner noise. For trustworthy gates:

- Use dedicated or self-hosted bare-metal runners with SMT and frequency scaling disabled.
- The counterbalanced batch order helps but does not eliminate noise.
- The CV guard (5%) catches high-variance runs and marks them INCONCLUSIVE rather than passing or failing on noise.

See `docs/research/04-ci-continuous.md` for detailed noise data and environment controls.

## Troubleshooting

| Problem | Cause | Fix |
| --- | --- | --- |
| `no comparable gated series found` | Base and candidate have no common benchmarks | Check that the benchmark pattern matches on both sides |
| `incompatible benchmark environments` | GOOS, GOARCH, CPU, or Go version differ | Use the same runner for both sides; or set `allow-env-mismatch` |
| `INCONCLUSIVE: insufficient samples` | Fewer than `min-samples` per side | Increase `count` |
| `INCONCLUSIVE: high CV` | CV exceeds `cv-threshold` | Reduce runner noise; increase `benchtime` |
| Comment not posted | Reporter lacks `issues: write` permission | Add `issues: write` to the reporter workflow |
| Artifact not found | Run was cancelled or superseded | Check that the gate workflow completed successfully |
