---
name: diagnose-noisy-bench
description: |
  Diagnoses the current machine's benchmarking environment and reports active noise sources with prioritized guidance.
  Wraps the `benchenv` CLI from github.com/kakkoyun/benchlab.
  USE WHEN: "diagnose noisy benchmark", "why is my benchmark flaky", "check benchmarking environment",
  "benchmark variance too high", "benchmark results unreliable", "set up for benchmarking".
license: MIT
compatibility: Requires Go 1.24 or newer on PATH.
disable-model-invocation: false
---

# diagnose-noisy-bench

Use this after `honest-benchmark` has made the source structurally correct and `benchstat-gate` has shown unstable repeated samples. Environment controls cannot repair a benchmark that measures the wrong work.

Runs `benchenv` to diagnose the benchmarking environment. It distinguishes the local machine, the running process, the connected Docker engine/VM, and a probe container, then emits prioritized guidance for improving benchmark reliability.

## Run the CLI

Install it once for repeated use:

```bash
go install github.com/kakkoyun/benchlab/cmd/benchenv@latest
```

Or run it immediately without installing:

```bash
go run github.com/kakkoyun/benchlab/cmd/benchenv@latest
```

## Run

```bash
# Human-readable text output
benchenv

# Machine-readable JSON (for scripting / CI)
benchenv -json

# Strict mode: exit 1 unless the environment is publication-grade
benchenv -strict
```

## Exit codes

| Code | Default mode | `-strict` mode |
| --- | --- | --- |
| `0` | Diagnosis completed | Overall grade is `ready` |
| `1` | — | Overall grade is not `ready` (limited, not_ready, unavailable) |
| `2` | CLI usage or encoding error | CLI usage or encoding error |

Under `-strict`, macOS and VM-backed environments exit `1` because they cannot be certified as publication-grade. Use `-strict` in CI gates that require publication-grade evidence.

## Understanding readiness

The report grades two execution paths — **native** and **docker** — and selects the best viable path as the overall recommendation.

| Grade | Meaning |
| --- | --- |
| `ready` | Publication-grade: native bare-metal Linux, no active host-noise findings, native architecture, passing isolation probe (Docker path) |
| `limited` | No fixable blocker, but cannot be certified (macOS, VM-backed engine, unknown backend) |
| `not_ready` | Active fixable hazard: QEMU/cross-arch, missing CPU isolation, noisy CPU controls, high load, Low Power Mode, failed cgroup limits |
| `unavailable` | That path cannot be used (no Docker daemon) |

**Key distinction**: a VM-backed engine (Colima, Docker Desktop) can pass its cgroup isolation probe, but VM vCPU pinning is **not** physical-core pinning. Such environments are `limited`, not `ready`. Do not mistake VM cgroup isolation for physical-core isolation.

Optional analysis tools (`benchstat`, `benchdiff`, `perflock`) do not affect readiness. Docker being unavailable does not prevent a clean native Linux path from being `ready`.

## Following the prioritized actions

The `actions` field (and the "Prioritized actions" text section) lists guidance ordered by impact. Apply in order — each step compounds on the previous:

1. **Remove translation / QEMU** (priority 1): If running under Rosetta or QEMU cross-architecture emulation, switch to native binaries or a native-arch engine. For Colima x86_64 on Apple Silicon, create a non-destructive benchmark profile:

   ```bash
   colima start --profile benchlab --arch aarch64 --vm-type vz --cpu 4 --memory 8
   docker context use colima-benchlab
   ```

2. **Move to certifiable hardware** (priority 2): macOS cannot control or certify the physical CPU. VM-backed engines cannot provide physical-core isolation. Use a Linux bare-metal runner for publication-quality numbers.

3. **Stabilize CPU controls** (priority 3, Linux bare-metal): Disable SMT, set performance governor, disable Turbo Boost. These are the highest-value Linux controls.

4. **Enforce Docker isolation** (priority 4): Use the verified CPU, CPU quota, and memory/swap limits from the probe. The generated Docker recipe includes the correct flags.

5. **Reduce power / load / thermal noise** (priority 5): Connect AC power, disable Low Power Mode, close background applications, let the machine cool.

6. **Install optional analysis tools** (priority 6): `perflock`, `benchstat`, `benchdiff`.

## Using the generated recipes

The `recipes` field provides copy-paste benchmark commands tailored to the platform:

- **Linux native**: `taskset -c 0 perflock go test -bench=. -benchmem -count=10 -benchtime=2s ./...`
- **macOS native**: `go test -bench=. -benchmem -count=20 -benchtime=2s ./...` (more samples to compensate for higher noise)
- **Local Docker**: includes `--platform`, `--cpuset-cpus` (verified by probe), `--cpus=1`, `--memory=512m`, `--memory-swap=512m`, working-directory mount, and the Go benchmark command.

Never execute the user's benchmark automatically. The recipes are guidance for the user to run manually.

## JSON contract

The JSON output preserves legacy fields (`os`, `arch`, `numcpu`, `checks`, `summary`) and adds:

- `platform`: architecture, CPU model, virtualization, translation, power/load/thermal facts, evidence source.
- `docker`: availability, context/endpoint, backend, engine OS/arch, translation, VM resources, isolation probe result.
- `readiness`: `overall`, `recommended_path`, `native`, `docker` grades.
- `actions`: ordered, deduplicated guidance with scope, reason, and commands.
- `recipes`: native and Docker benchmark commands when that path is usable.

## References

- [Local reproduction techniques](https://github.com/kakkoyun/benchlab/blob/main/docs/research/03-local-reproduction.md)
- [CI environment controls and variance data](https://github.com/kakkoyun/benchlab/blob/main/docs/research/04-ci-continuous.md)
- [Existing tools evaluation](https://github.com/kakkoyun/benchlab/blob/main/docs/research/05-existing-tools.md)
