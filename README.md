# benchlab

[![Go Reference](https://pkg.go.dev/badge/github.com/kakkoyun/benchlab.svg)](https://pkg.go.dev/github.com/kakkoyun/benchlab)
[![skills.sh](https://skills.sh/b/kakkoyun/benchlab)](https://skills.sh/kakkoyun/benchlab)

Small tools and agent skills for trustworthy Go benchmarks.

## Install

Install all three commands:

```bash
go install github.com/kakkoyun/benchlab/cmd/...@latest
```

Install all three skills for your coding agents:

```bash
npx skills add kakkoyun/benchlab --all
```

Install one skill for one agent:

```bash
npx skills add kakkoyun/benchlab --skill honest-benchmark -a claude-code
```

| Command | Skill | Question |
|---|---|---|
| `honestbench` | `honest-benchmark` | Is the compiler measuring real work? |
| `benchgate` | `benchstat-gate` | Is the sample stable enough? |
| `benchenv` | `diagnose-noisy-bench` | What is making the benchmark noisy? |

The tools are no longer stdlib-only: `benchgate` depends on `golang.org/x/perf` for benchmark parsing and statistical comparison. `honestbench` and `benchenv` remain stdlib-only. The skills teach coding agents when to run them, how to interpret their output, and how to fix the problems they find.

## honestbench

`honestbench` parses Go benchmark source with `go/ast`. It reports discarded results, missing sinks, timer ordering mistakes, and `b.N` loops that can migrate to `testing.B.Loop`.

```bash
honestbench -r ./...
honestbench -json ./mypkg | jq .

# Run without installing
go run github.com/kakkoyun/benchlab/cmd/honestbench@latest -r ./...
```

Exit codes: `0` for no findings, `1` for findings, and `2` for an error.

## benchgate

`benchgate` runs benchmarks, compares base and candidate results with Mann-Whitney U test statistics via `golang.org/x/perf`, and emits a `PASS`, `REGRESSION`, `INCONCLUSIVE`, `WAIVED`, or `ERROR` verdict.

```bash
# Collect and compare in one step (requires a base worktree)
benchgate run -base-dir ../base-worktree -pkg ./... -count 10

# Compare two pre-collected result files
benchgate compare -base before.txt -candidate after.txt

# Legacy CV-only mode (backward compatible)
benchgate -pkg ./... -count 10 -cv-threshold 5.0
benchgate -pkg ./... -count 10 -save before.txt
benchgate -pkg ./... -count 10 -baseline before.txt
```

Exit codes: `0` for pass or valid waiver, `1` for regression or inconclusive result, `2` for error.

### GitHub Actions quick start

Add a read-only PR gate that runs on every pull request:

```yaml
- uses: kakkoyun/benchlab/actions/benchgate@v0.2.0
  with:
    count: '10'
    runtime-threshold: '10'
    cv-threshold: '5'
```

The gate receives no secrets and no write token. A separate trusted reporter workflow posts the PR comment and manages the one-shot `benchgate:accept-regression` waiver label. See [`docs/benchgate-github-actions.md`](docs/benchgate-github-actions.md) for the full setup, permissions, fork behavior, branch protection, and waiver lifecycle.

## benchenv

`benchenv` checks the current machine for common sources of benchmark noise. Linux checks include SMT, CPU frequency scaling, Turbo Boost, load, and container detection. macOS reports which controls the operating system does not expose and checks load and thermal visibility.

```bash
benchenv
benchenv -json
```

## Agent skills

The repository follows the [Agent Skills specification](https://agentskills.io/specification). `npx skills` discovers every `skills/<name>/SKILL.md` file without a plugin manifest.

```bash
# Choose skills and agents interactively
npx skills add kakkoyun/benchlab

# Install every benchlab skill globally for every supported agent
npx skills add kakkoyun/benchlab --all -g
```

Each skill includes a `go run github.com/kakkoyun/benchlab/cmd/<tool>@latest` path, so an agent can use the command without a separate installation step.

## Development

Go 1.25 or newer is required.

```bash
make check
```

The tools originated in *Why Your Go Benchmarks Are Lying (And How to Stop Them)*, prepared for GopherCon UK 2026. Supporting research is copied under [`docs/research/`](docs/research/). The private talk repository remains the authoritative source for those notes.

## License

MIT
