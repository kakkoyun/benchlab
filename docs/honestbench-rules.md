# honestbench rule catalog

`honestbench` is a type-aware `go/analysis` checker for active Go test packages. It recognizes the real `testing.B` and `testing.PB` types, import aliases, nested callbacks, and same-package helpers. Default rules enforce documented API contracts and structural correctness. `-advisory` adds heuristics that need human judgment.

The official [`testing` package documentation](https://pkg.go.dev/testing) is authoritative. The Go blog's [B.Loop introduction](https://go.dev/blog/testing-b-loop), the [Go 1.24 release notes](https://go.dev/doc/go1.24), the [B.Loop proposal](https://go.dev/issue/61515), and the [official B.Loop example](https://go.dev/src/testing/example_loop_test.go) provide background.

## Default rules

| Rule | What it reports | Safe fix |
| --- | --- | --- |
| `missing-loop` | Direct benchmark work has no recognized iteration driver, subbenchmark, parallel run, or receiver delegation. | None. Choose the intended driver. |
| `noncanonical-b-loop` | `B.Loop` is not the sole condition of `for b.Loop() {}`. | Normalizes simple parentheses or `== true` wrappers. |
| `multiple-b-loop` | A scope contains more than one canonical `B.Loop`. | None. Merge or split the measured work. |
| `mixed-loop` | A scope mixes `B.Loop` and a `b.N` iteration. | None. Pick one iteration API. |
| `b-n-with-b-loop` | Code reads `b.N` before or inside `B.Loop`. A read after the loop for metrics is valid. | None. Move a justified metric read after the loop. |
| `wrong-b-n-count` | A simple legacy loop provably runs a count other than `b.N`. | None. Correct the loop or migrate it. |
| `reset-timer-in-loop` | `ResetTimer` repeatedly clears timing, allocation, and custom metric state. | None. Timer placement depends on the workload. |
| `stoptimer-without-starttimer` | A reachable iteration path stops timing without a certain resume before measured work. | None. Review every path. |
| `work-while-timer-stopped` | Every reachable work statement in an iteration is timed as stopped. | None. Decide which work should be measured. |
| `outer-b-in-subbenchmark` | A `b.Run` callback uses its parent receiver for iteration or control methods. | None. Use the callback receiver. |
| `runparallel-missing-next` | A parallel callback has neither `pb.Next` nor a `PB` helper delegation. | None. Add the intended parallel iteration. |
| `runparallel-wrong-loop` | A parallel callback uses `B.Loop`, `b.N`, or repeated `pb.Next` loops. | None. Use one `pb.Next` loop. |
| `runparallel-timer` | A parallel callback changes the global benchmark timer. | None. Move timer control outside the callback. |
| `runparallel-subbenchmark` | A parallel callback starts `B.Run`. | None. Separate the benchmark structures. |
| `setparallelism-order` | `SetParallelism` definitely executes after `RunParallel`. | None. Move it only after confirming intent. |

## Advisory rules

| Rule | What it reports | Safe fix |
| --- | --- | --- |
| `suggest-bloop` | A canonical legacy loop can use `B.Loop`. | Rewrites headers when the index is absent or unused and the header has no comments. |
| `timed-setup` | Nontrivial setup precedes a legacy loop without a later `ResetTimer`. | None. Cost and intent are workload-specific. |
| `timed-cleanup` | Nontrivial or deferred cleanup follows a legacy loop without `StopTimer`. | None. |
| `discarded-result` | A result-returning call is discarded in a legacy iteration. Exact `B.Loop` bodies are exempt because the compiler keeps loop calls and values alive. | None. A sink may change escape behavior. |
| `missing-sink` | A legacy-loop result has no observable post-loop use or only a blank assignment. | None. Package sinks are a legacy fallback. |
| `package-write-in-loop` | A measured iteration writes package state. | None. Moving the write may change semantics. |
| `benchmark-config-in-loop` | Reporting or parallelism configuration executes every iteration. | None. |
| `noncanonical-b-n-loop` | A `b.N` loop is unusual, but its count cannot be proven wrong. | None. |
| `setparallelism-without-runparallel` | `SetParallelism` has no matching parallel run. | None. |
| `reused-mutated-input` | A pre-loop value reaches a known in-place `sort` or `slices` mutator without an obvious reassignment. | None. Clone strategy depends on what is measured. |
| `redundant-b-loop-timer` | Standalone `ResetTimer` before or `StopTimer` after a canonical `B.Loop` duplicates its timing behavior. | Removes the standalone call when it has no attached comment. |

## Limits

- Analysis follows same-package helpers once, with cycle protection. Passing a receiver to an external or unresolved helper suppresses only conclusions that would require guessing.
- Timer analysis merges branch and loop-back states. Unknown helper effects suppress uncertain timer conclusions.
- Generated benchmarks are analyzed, but suggested edits are withheld.
- Build tags and platform files follow the package configuration used by the standard driver.
- The checker does not infer benchmark intent. It does not require `ReportAllocs`, `SetBytes`, particular input sizes, cache warming, or machine controls.

Use [`examples/honestbench`](../examples/honestbench) for clean patterns. Intentionally invalid examples remain under analyzer `testdata`, where normal repository benchmark runs ignore them.
