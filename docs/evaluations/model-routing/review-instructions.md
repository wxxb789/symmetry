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
- a declared gate or required explanation is missing;
- the change crosses scope or reinterprets an invariant;
- evidence binds stale source, or a receipt claims results the coordinator did not observe;
- a repair weakens acceptance, authority, recovery, permissions or types.

Report concrete findings with affected paths, consequence and evidence.
Separate demonstrated defects from missing evidence and unknown billing.
Billing incompleteness does not establish a behavioral defect, but prevents a complete cost comparison or routing-policy promotion.
Do not modify source or receipts, issue `achieve`, or accept based on model reputation.

Return the coordinator's requested structured record with the exact Subject hash, reviewed paths, verdict and unresolved findings.
The coordinator retains the private harness record, imports only the sanitized review and revalidates the final receipt.
