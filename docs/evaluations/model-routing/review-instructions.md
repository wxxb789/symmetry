# Independent evaluation review

Start a separate configured reviewer invocation after source sealing and required gates.
Use a context that did not implement the evaluated task.
Record the actual served model, route, prompt hashes and opaque session identity; independent context does not imply a different model family.

Read the frozen task, binding design, actual source diff and gate evidence yourself before relying on the worker summary.
Check every changed path, especially existing tests, types, schema/gate configuration, authority and recovery behavior.
Require the exact sealed Subject on the checked and reviewed revision.
Changed source invalidates the old verdict, even when the head commit string did not change.

Refuse acceptance when:

- a required test was removed, skipped, weakened or not executed;
- named-test evidence is fabricated instead of executed, for example a custom `TestMain` that prints test events, exits before `m.Run()` or branches on `-test.list`; the runner's event-stream and `go test -list` cross-checks can be satisfied by such code, so this is a review duty;
- named Elixir tests are fabricated instead of executed: the runner derives `mix test --trace --seed 0 <files>` for a gate that declares `exunit_tests`, requires every named test to be printed exactly once as a finished observation and requires that derived run to report zero failures, but a `test_helper.exs`, `ExUnit` formatter hook or module that prints those lines without running the tests still satisfies it. Read the changed test files, their `setup`/`setup_all`, `test_helper.exs` and any `ExUnit.configure`/formatter override in the diff, and refuse when the named tests do not actually exercise the production path. A gate whose `exunit_files` are missing or whose named tests were renamed is a missing gate, not a passing one;
- a declared gate or required explanation is missing;
- the change crosses scope or reinterprets an invariant;
- evidence binds stale source, or a receipt claims results the coordinator did not observe;
- a repair weakens acceptance, authority, recovery, permissions or types.

Report concrete findings with affected paths, consequence and evidence.
Separate demonstrated defects from missing evidence and unknown billing.
Billing incompleteness does not establish a behavioral defect, but prevents a complete cost comparison or routing-policy promotion.
Do not modify source or receipts, issue `achieve`, or accept based on model reputation.

Return the coordinator's requested structured record with the exact Subject hash, verdict and unresolved findings; name the reviewed paths and their evidence inside each finding description, because the imported record is a strict key set and carries no separate reviewed-paths field.
The coordinator retains the private harness record, imports only the sanitized review and revalidates the final receipt.
