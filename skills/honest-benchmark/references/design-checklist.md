# Benchmark design checklist

Use this checklist after structural diagnostics are clean.

- Define the operation and boundaries: setup, measured work, cleanup, reporting.
- Use realistic inputs for the production path. Record why the chosen sizes and distributions matter.
- Clone or reset state when an in-place operation would otherwise benchmark already-mutated input.
- Decide whether allocations are part of the operation. Call `ReportAllocs` only when the metric helps answer the question.
- Use `SetBytes` when throughput is meaningful and the byte count per operation is stable.
- Keep `RunParallel` state goroutine-local unless shared contention is the behavior under test.
- Warm caches only when measuring a warm-cache path. Cold-cache claims need explicit cache control and documentation.
- Run repeated samples, save raw output, and compare with `benchstat`; never publish a single timing.
- Keep toolchain, CPU, power, load, affinity, and environment consistent across baseline and experiment.
- Remember that profilers, tracing, and cache observers can change the workload. Use them separately from final timing runs.

Workflow: `honest-benchmark` → `benchstat-gate` → `diagnose-noisy-bench`.
