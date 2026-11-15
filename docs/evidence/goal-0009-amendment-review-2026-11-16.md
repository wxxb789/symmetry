# Goal 0009: independent amendment review

## Review boundary

This records independent read-only review of the A/B/C amendment, separate from
the implementing primary session and worker contexts. The configured
`reviewer` invocations were `AmendmentAuthorityReview` and
`AmendmentSubjectReview`; neither was assigned write ownership. No explicit
model override, cross-provider review or cross-model-family independence is
claimed. The primary session owns integration and all executed checks.

The implementation and run receipt is
[`goal-0009-achieve-contract-amendments-2026-11-15.md`](goal-0009-achieve-contract-amendments-2026-11-15.md).
Review conclusions are scoped source evidence, not an owner verdict, a CI pass,
a native-capability promotion or proof that an unexecuted scenario worked.

## A/B: authority, binding and projections

`AmendmentAuthorityReview` inspected the amended Goal transitions, contract
validation, read model, schema, design contracts and regression coverage. Its
source review found no authority bypass, proposal/hash rewrite or readiness
defect in the amendment. In particular:

- Operator achieve replaces only the operator's redundant completion ceremony.
  Other authority paths and supplied decisions retain their existing binding;
  required outcomes, evidence, integration Subject, unresolved decisions and
  nonterminal tasks remain guarded.
- Required cardinality, not optional item count, derives the sole integration
  item. Conflicting explicit or stored designations fail closed. Other
  cardinalities retain explicit marker and payload requirements.
- Original canonical proposal/hash approval is checked before the derived
  persisted role; accepted proposal identity and replay are not rewritten.
- Public `decision_id` stays required and nullable. The integration payload
  field becomes optional in the wire shape, with its cardinality-dependent
  obligation enforced by Control.

### Witness defect found and removed

The reviewer found that the proposed real-run driver incorrectly expected Goal
pause to cancel an undispatched Goal task. `safe_pause_task_ids` excludes these
tasks; legacy Task cancellation is not a substitute. That sequence was removed
before it was executed.

A follow-up source review also ruled out admitting another validator on the
already accepted required item: both admission paths call
`ensure_unaccepted_item!`, while earlier admission conflicts with the active
task/parallelism guards. Racing terminal task settlement is not an acceptable
alternative. No authority, queue-settlement rule or database row was changed
to manufacture this window.

The replacement uses the already approved optional item. After the required
validator is terminal and its accepted outcome exists, the driver admits the
third and final permitted task, observes the nonterminal-task refusal, then
lets the real runtime execute it. The next stage waits for its actual terminal
state, records that state and rechecks the accepted Subject and readiness.
Completed, failed and cancelled are existing terminal states; an optional
failure is not relabelled as successful work. The reviewer confirmed this
sequence against the admission and settlement guards. Actual run observations
belong to the linked receipt, not this source recommendation.

## C: persistent stream, fences and teardown

`AmendmentSubjectReview` reviewed the two Subject consumers, shared stream
implementation, refusal/cancellation tests and canonical digest contract. It
found no digest-format or canonical-byte defect. It found two actionable P2
issues:

| Finding | Resolution and observed evidence |
| --- | --- |
| A failing path-specific `Cancel` callback returned before falling back to stopping the retained child. | `subjectGitBatchStream.terminate` retains the cancellation error, falls back to `command.Process.Kill`, joins a second error and leaves the existing single `Wait` and unresolved fence unchanged. The new regression failed before repair after 5.19 seconds and required its explicit escape cancellation; after repair it passed in 0.12 seconds without cancelling its own context. |
| Malformed batch framing lacked direct subprocess coverage. | A ten-row real-helper table covers a valid independent-oracle control, wrong OID/type, malformed and negative size, malformed/truncated header, partial content and bad/missing trailer. Every row checks child reaping; all ten passed. Existing oversized-object coverage remains. |

The parent ran the red and green commands. Their outputs are
`.tmp/0009/subject-review-red.log` (exit 1) and
`.tmp/0009/subject-review-green.log` (exit 0; 33 top-level passed, one opt-in
measurement skipped; package 23.423 seconds).

The reviewer then reread the final `terminate`, its `close` caller and both new
tests, and closed both P2 findings. It confirmed retained-process ownership,
kill before the single `Wait`, preservation of the original cancellation error
and `ErrLinuxCloneProcessUnresolved`, and no conversion of an unproved process
group stop into a successful stop.

### Bounded follow-up and retained trade-offs

- The follow-up found that the new regression's diagnostic cancellation counter
  could race on its failure/escape path. The parent changed it to
  `atomic.Int32` and corrected the `WaitDelay` comment. This changes test
  synchronization, not production behavior or a proof condition.
- The valid helper control uses the existing one-blob README fixture. Its
  independent digest assertion already fails if that fixture's content shape
  drifts; no extra fixture-count assertion was added.
- The existing trace-start cancellation test has a timing window if its small
  digest completes before cancellation. It was retained, not weakened or
  rewritten to obtain a pass; its observed executions and this residual test
  limitation remain explicit.
- Desynchronization conservatively retains the unresolved-process fence and
  Linux clone scratch even when the direct child has been reaped. This is the
  existing recovery boundary, not permission to discard uncertain state.
- The shared hasher still copies the slice returned by `Hash.Sum`; its public
  interface does not guarantee that the supplied backing array was reused.

No separate solution guide was added: the cleanup invariant is pinned directly
by the new regression; this record holds the run-specific review evidence.

## Owner boundary

This independent review is produced evidence. The owner has not supplied a
verdict on the amended contract, wire schema and run evidence. Nothing in this
record signs for that owner or changes the Goal's acceptance contract.
