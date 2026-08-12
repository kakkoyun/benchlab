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

The commands are stdlib-only Go programs. The skills teach coding agents when to run them, how to interpret their output, and how to fix the problems they find.

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

`benchgate` runs benchmarks repeatedly, computes the coefficient of variation for each benchmark, and fails when a result exceeds the configured threshold. It can save raw output and compare it with a baseline through `benchstat`.

```bash
benchgate -pkg ./... -count 10 -cv-threshold 5.0
benchgate -pkg ./... -count 10 -json
benchgate -pkg ./... -count 10 -save before.txt
benchgate -pkg ./... -count 10 -baseline before.txt
```

Exit codes: `0` for a passing gate, `1` for an unstable sample, and `2` for an error.

## benchenv

`benchenv` diagnoses the benchmarking environment for common sources of benchmark noise. It distinguishes the local machine, the running process, the connected Docker engine/VM, and a probe container, then emits prioritized guidance for improving benchmark reliability.

### What it checks

- **Platform**: process and machine architecture (ARM64 vs AMD64), CPU model, virtualization (bare metal, KVM, QEMU, Apple Virtualization.framework), translation (Rosetta, QEMU), power source, power mode, load, and thermal warnings.
- **Docker**: availability, context/endpoint locality, engine OS/architecture, backend (native Engine, Colima VZ/QEMU, Docker Desktop), translation, VM resources, and an active cgroup isolation probe.
- **Checks**: platform-specific CPU controls (SMT, governor, boost on Linux; power/thermal on macOS), load, container detection, and optional tool availability.

### Active Docker isolation probe

When a local Docker daemon is reachable and `benchenv` is not itself containerized, `benchenv` runs a disposable `busybox:1.37.0` probe container with `--network=none`, `--cpuset-cpus`, `--cpus=1`, `--memory=128m`, and `--memory-swap=128m`. It verifies the **effective** cgroup v1/v2 values inside the container rather than trusting accepted Docker flags: cpuset, CPU quota, memory max, and swap. Missing controllers or mismatched values produce specific findings. When `benchenv` is already inside a container, it inspects the current cgroup limits instead of launching a nested probe.

### Readiness grades

| Grade | Meaning |
|---|---|
| `ready` | Publication-grade evidence: native bare-metal Linux, no active critical host-noise findings, native architecture, passing isolation probe |
| `limited` | No fixable blocker, but the environment cannot be certified (macOS, VM-backed engine, unknown backend) |
| `not_ready` | Active fixable hazard: QEMU/cross-arch, missing CPU isolation, noisy CPU controls, high load, Low Power Mode, failed cgroup limits |
| `unavailable` | That path cannot be used (no Docker daemon) |

The overall result selects the best viable path and records it as `recommended_path`. Optional analysis tools do not affect readiness.

### Usage

```bash
benchenv           # human-readable text output
benchenv -json     # machine-readable JSON
benchenv -strict   # exit 1 unless the environment is publication-grade
```

Exit codes: `0` for a completed diagnosis (default mode), `0` for overall `ready` (`-strict` mode), `1` for non-ready (`-strict` mode), and `2` for a CLI or encoding error. Under `-strict`, macOS and VM-backed environments exit `1` because they cannot be certified as publication-grade.

### JSON output

The JSON output preserves the legacy fields (`os`, `arch`, `numcpu`, `checks`, `summary`) and adds structured fields: `platform`, `docker`, `readiness`, `actions`, and `recipes`.

### Representative output (macOS / Colima)

```
benchenv: benchmarking environment diagnosis (darwin/arm64, 10 CPUs)

Platform
  architecture:   arm64
  virtualization: bare metal
  translation:    native
  CPU model:      Apple M2
  power:          ac
  power mode:     automatic
  load average:   0.10

Docker
  available:    yes
  context:      colima
  backend:      colima-vz
  engine arch:  arm64
  translation: native
  isolation:    passed (cgroup v2, cpu 0)

Readiness
  overall:           limited
  recommended path: native
  native:            limited
  docker:            limited
```

### Generated recipes

- **Linux native**: `taskset -c 0 perflock go test -bench=. -benchmem -count=10 -benchtime=2s ./...`
- **macOS native**: `go test -bench=. -benchmem -count=20 -benchtime=2s ./...` (higher sample count, higher noise warning)
- **Local Docker**: `docker run --rm --network=none --platform=linux/arm64 --cpuset-cpus=0 --cpus=1 --memory=512m --memory-swap=512m -v $(pwd):/work -w /work golang:1.24 go test -bench=. ...`

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

Go 1.24 or newer is required.

```bash
make check
```

The tools originated in *Why Your Go Benchmarks Are Lying (And How to Stop Them)*, prepared for GopherCon UK 2026. Supporting research is copied under [`docs/research/`](docs/research/). The private talk repository remains the authoritative source for those notes.

## License

MIT
