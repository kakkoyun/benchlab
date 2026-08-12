# honestbench rules

The complete catalog is maintained in [`docs/honestbench-rules.md`](../../../docs/honestbench-rules.md).

Default rules are structural or documented API checks. Treat them as blockers unless inspection proves the analyzer followed an unresolved helper or other documented limit.

Advisory rules cover migration, setup and cleanup timing, legacy compiler observability, package writes, configuration placement, and reused mutable inputs. They need workload context.

Safe fixes are intentionally narrow:

- canonical legacy loop to `B.Loop` when no loop index or header comment would be lost;
- simple `B.Loop() == true` or parenthesis normalization;
- standalone redundant timer calls around canonical `B.Loop`.

Do not auto-place timer calls, add sinks, move reports, or clone data.
