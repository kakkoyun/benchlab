---
name: honest-benchmark
description: |
  Authors and audits trustworthy Go benchmarks with the type-aware honestbench analyzer.
  USE WHEN: "audit go benchmark", "write go benchmark", "benchmark dead code elimination",
  "is my benchmark measuring real work", "benchmark timer order".
license: MIT
compatibility: Requires Go 1.24 or newer on PATH.
disable-model-invocation: false
---

# honest-benchmark

Make the benchmark source structurally honest before collecting numbers.

## Workflow

1. Read the benchmark and state what one iteration should measure.
2. Prefer exact `for b.Loop() { ... }` syntax. Use one loop per benchmark or subbenchmark.
3. Keep setup outside the loop unless setup cost is the subject. Clone mutable inputs inside the loop when each iteration needs a fresh value.
4. Use the callback receiver in `b.Run`. Use one `for pb.Next()` loop and goroutine-local state in `RunParallel`.
5. Run both analyzer modes:

```bash
honestbench ./...
honestbench -advisory ./...

# Without installation
go run github.com/kakkoyun/benchlab/cmd/honestbench@latest -advisory ./...
```

1. Fix default diagnostics first. Review advisory diagnostics against workload intent; do not apply timer, sink, reporting, or clone changes mechanically.
2. Prove the benchmark terminates cheaply:

```bash
go test -run='^$' -bench=. -benchtime=1x -count=1 ./...
```

1. Hand off to `benchstat-gate` for repeated samples. If variance is high, hand off to `diagnose-noisy-bench`.

## Compiler guidance

Inside an exact `B.Loop` body, the compiler keeps function-call arguments, results, and assigned values alive. A discarded-looking call is therefore valid there. Do not add a package sink by default.

Legacy `b.N` loops still need observable work. A package sink can be a fallback, but it may add global-write cost or alter escape behavior. Prefer migrating a safe loop to `B.Loop`; otherwise choose the smallest observable use that matches the workload.

## Output

`honestbench` uses standard `go/analysis` text and JSON output. It exits zero when clean and nonzero otherwise. `-advisory` is off by default.

## References

- [Rule catalog](references/rules.md)
- [Benchmark design checklist](references/design-checklist.md)
- [Runnable examples](../../examples/honestbench)
