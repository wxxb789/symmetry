# Goal 0008a: auditable acceptance workflow

## Contract and source provenance

The binding contract is [`docs/goals/archive/0008a-auditable-acceptance-workflow.md`](../goals/archive/0008a-auditable-acceptance-workflow.md), including its standalone Done-when.
Implementation started from commit `343dea6a62484d73e7b577a0be2683ca233510f6` on the existing feature branch with an initially clean working tree.
No commit, push or history rewrite is authorized or performed.
Consequently base/head commit IDs remain genuine Git provenance; uncommitted content is bound separately by the evaluation Subject rather than a fabricated head commit.

Shape reference: executable-evidence increment `8ded77150ec0ae49d52355670fdad78a348bf1d5` (OmO native witness, Python unittest refusal controls and its exact-Subject evidence convention).
The implementation reuses `scripts/ci/linux-v2-source-subject.py` and `scripts/ci/assert-go-test-json.py` unchanged.
It adds no service, provider profile, credential, spend cap or routing-policy promotion.
No benchmark, savings comparison or strong-only-versus-mixed quality claim is made.

## Startup routing and trust boundary

Both existing configured routes completed actual startup requests before implementation:

- `WorkerRoutePreflight`: configured `scout` override `@flash_high`.
- `ReviewerRoutePreflight`: configured `reviewer` override `@flash_max`.

The live bounded implementation used `sonic`, also configured through `@flash_high`.
Observed served identity: provider `tencent-litellm`, model `deepseek-v4.1-flash-fast`, API `openai-completions`; runtime effort metadata is `high` for the worker and `max` for the reviewer.
No per-call model override or fallback was requested.
Credential-route evidence identifies the existing configured provider route and successful authenticated requests, never credential values.

The reviewer uses a separate invocation/context, not a different model family.
This process is not an OS sandbox: the trusted coordinator owns the external frozen evidence, while same-user filesystem tampering is outside the mechanism's guarantee.
The startup reviewer inspected local configuration/session metadata beyond its requested two-document read scope; that invocation establishes route availability only, not task acceptance or read-sandbox enforcement.
Raw prompts, tool payloads and private transcripts remain outside Git.

## Frozen live Go work

Before worker execution, the coordinator copied the task contracts to external evidence storage:

| Task | Frozen SHA-256 |
| --- | --- |
| `0008a-go-subject-receiver` | `e9cafd283338ff091f3d865ec480b16b95fa34ac7d30a06422323f05677c6926` |
| `0008a-go-linux-final-scan-boundary` | `a797887fd0bd16a5c161611d3ed6378716fdc0a20e7e704c8f2b5806aa290a28` |

The original isolated Go checkout was qualified with:

```sh
python scripts/ci/linux-v2-source-subject.py ISOLATED_CHECKOUT 343dea6a62484d73e7b577a0be2683ca233510f6
```

Exit 0; protocol tree digest `sha256:035087b99858427f7fbe43364d2ee5d9378091601d524e86f14b2598a504d99a`.
The worker added only `daemon/internal/protocol/goal_subject_receiver_test.go`, exercising the actual production `Subject.UnmarshalJSON` through `json.Unmarshal` without a mock.
The two named tests cover 11 rejected-input cases plus valid replacement of all three identity fields.
The production decoder and every existing test remain unchanged.
The coordinator simplified the initial test delivery before verification by requesting removal of duplicate canonical/hash and fixture-premise assertions; both frozen test names and all 11 rejection cases were preserved.
The resulting file has 63 lines and was integrated into the canonical working tree.

## Observed commands

All commands below were actually executed; model evaluation/measurement did not run concurrently with the full Go suite.
Raw command output is retained in the private evidence root `symmetry-0008a-8mdb7ctb`.
For the command paths below, `EVIDENCE_ROOT='C:/Users/lhan/AppData/Local/Temp/symmetry-0008a-8mdb7ctb'`.

| Command and surface | Observed outcome | Exit |
| --- | --- | --- |
| `python scripts/ci/test-linux-v2-source-subject.py` | 4 tests, 0 failures | 0 |
| Isolated base: `go test -json -count=1 ./internal/protocol` | 54 top-level tests / 449 tests including subtests passed; both new required names absent | 0 |
| `python scripts/ci/assert-go-test-json.py "$EVIDENCE_ROOT/baseline-go-events.jsonl" --package github.com/wxxb789/symmetry/daemon/internal/protocol TestSubjectUnmarshalPreservesReceiverOnFailure TestSubjectUnmarshalReplacesReceiverOnSuccess` | Both names `run=0/pass=0/skip=0`; correctly refused despite Go exit 0 | 1 |
| Scoped `gofmt -w` on the 63-line regression file in the canonical and isolated checkouts | Formatter completed | 0 |
| `python scripts/ci/test-model-routing-omp-usage.py`, initial implementation | 22 tests: 1 failure, 1 error | 1 |
| Same parser suite, first repair | 25 tests: 1 failure | 1 |
| Same parser suite, final repaired version | 25 tests, 0 failures/errors | 0 |
| `python scripts/ci/test-model-routing-evaluation.py`, initial implementation | 16 tests, 22 failing subcases, 1 platform skip; root gate `cwd: "."` incorrectly refused | 1 |
| Runner positive-flow test after the exact-root cwd repair | 1 test passed; traversal checks remained intact | 0 |
| Runner accounting integrity regression checks | 2 tests, 4 failed assertions: phantom repair, duplicate session, omitted implementing worker, empty prompt hashes | 1 |
| Runner source-mutation receipt regression, corrected verifier | 1 test passed; the existing `complete=false` assertion was preserved | 0 |
| Runner suite after adding refusal coverage, before final fixture correction | 20 tests: 1 failure, 1 platform skip; a purported partial-attempt fixture had only one required gate | 1 |
| `python scripts/ci/test-model-routing-evaluation.py -v`, final version | 20 tests: 19 passed, 1 skip (`executable file modes are not tracked on this host`), 0 failures; 218.622 s | 0 |
| Actual `LightweightGoSubject.jsonl` extraction with `--stage implementation --credential-route omp-tencent-configured --provenance task.agentModelOverrides.sonic` | Actual identity/usage and one follow-up prompt hash exported; no raw content | 0 |
| `go vet ./...` from `daemon/`, subprocess `close_fds=True` | Completed in 15.390 s | 0 |
| `go test -json -count=1 -timeout 300s ./...` from `daemon/`, subprocess `close_fds=True` | 22 packages: 21 pass, 1 no-test skip; 1,787 top-level tests: 1,732 pass, 55 skip, 0 fail; including subtests: 4,071 pass, 55 skip; 220.313 s | 0 |

The parser failures were repaired without dropping cases or repinning expected behavior:
the initial test iterated a list with `.items()`, the implementation emitted the wrong missing-task limitation code, and a later scenario expected system/task hashes without supplying its `session_init` fixture.
The parser also now distinguishes `provider/model:effort` configuration from the matching served identity and hashes parent `custom_message` repair instructions.
Its source digest binds every private JSONL byte, including unrecognized event types, without exporting their content.

The runner's initial path validator rejected the legitimate exact-root gate cwd `.`.
Only that cwd value was admitted; allowed/protected path lists still reject `.` and traversal.
The accounting regression checks then demonstrated that an implementing worker could be omitted, a whole-session export could be double-counted, and prompt provenance could be empty while a receipt still claimed acceptance.
Those assertions were observed red before repair.
The fix requires unique model sessions, prompt provenance and every implementing worker referenced by review, and takes the actual repair-attempt count from the frozen coordinator ledger instead of counting mandatory no-work phase rows.
No assertion was weakened to obtain these fixes.

## Exact live Subject and chain

The runner separately prepared coordinator-owned positive and negative checkouts at the frozen base.
The implementing worker's original isolated checkout and both materialized verification checkouts were hashed before their gates: all three contained exactly 1,094 source entries with manifest digest `sha256:6fc242e1f52d982d9c8174405616d0230d8d7ceb86a504ffd068bb9c7c10960b`.
Thus materializing the already-correct worker artifact did not substitute a fixture, change bytes/modes or invent a second implementation.
The frozen task bytes predate worker execution; the runner reused those same bytes and actual model-produced source.

| Binding | Positive sample | Real-boundary negative sample |
| --- | --- | --- |
| Task | `0008a-go-subject-receiver` | `0008a-go-linux-final-scan-boundary` |
| Base/head | Both `343dea6a62484d73e7b577a0be2683ca233510f6` | Same actual base/head |
| Evaluation Subject | `sha256:b14e84696205aa4f2b4f7e6f9f7ea61790d89a72206a850a429ef76d58829d81` | `sha256:b8a846985010e1585627382fa713f1f5d6daaed66eb7257a2bb810949cfce3dc` |
| Check attempt | `attempt-0001`, `sha256:38ad6dee0ac97f52c2b8a67a7ea41be29ac4d2ee5990be0651e90c846852fb60` | `attempt-0001`, `sha256:c62ca8b839f67b1ed980291e3f6cde22eb4bda14c0b3e4d4869b021d4b062be9` |
| Route digest | `sha256:03829f19f1dde773326fb520f9a64c3c3fe4c20ea5b4081d8f14b36142c1fd3e` | Same configured route record |
| Gate outcome | Both frozen gates passed; both required names `run=1/pass=1/skip=0` | Preserved protocol gate passed; Linux qualification refused |
| Independent source review | Separate reviewer session `01a1dfc1-8d1c-73dd-a966-d2244d547b60`; source verdict `pass`; import exit 0 | Negative handoff recorded, no accepted-result review invented |

The namespace includes the task identity, so identical source bytes under different frozen tasks have different evaluation Subject hashes.
Neither identity is a Control resource or an `achieve` authorization.

The positive review import is bound by digest `sha256:1f38f35d4ad56a727bb9392d4d80ffe132107e1372557160d0a3c3130f77cf3f`.
The first fully derived positive receipt was actually accepted (both `receipt` and `accept` exit 0), with `accepted=true`, `accounting_complete=false`, and receipt digest `sha256:acb7bd0cf38edbf9939431ce481b74790a4a8d7ccd181067e52006055d492bb2`.
That bootstrap receipt and its private frozen usage remain the source of the disposable refusal controls; final accounting uses a later explicitly marked coordinator snapshot, not invented final billing.

The real negative gate executed exactly:

```sh
go test -json -count=1 ./internal/platform -run '^TestCleanDescendantTerminalProofReportsUnprovenState$'
```

Native exit 0 included `testing: warning: no tests to run`; the unchanged named-test validator returned 1 with `run=0/pass=0/skip=0`.
The lightweight worker returned `decision=escalate` with the failing evidence and preserved Go test file, handing authority back to the existing Sol primary.
The coordinator retained a nonaccepted receipt (`receipt` exit 0, `accept` exit 1), with the original protocol gate and source unchanged.
This establishes a real frozen native-Windows execution boundary, not a Linux runtime qualification or a census of other hosts/WSL.

Independent review also caught a stale negative receipt generated before the clock fix: it reported the last phase's 61.944 s instead of the ledger's 507.776538 s.
Re-deriving with the current runner produced matching `wall_time=507.776538`, included tooling-source fingerprints and again refused acceptance; the recorded native Go failure was not rerun or weakened.

## Observed refusal controls

[`0008a-controls.json`](../evaluations/model-routing/0008a-controls.json) records the exact argv, exits, output and output digests for eight disposable copies of the real accepted Go run.
All nine requested CLI refusals returned exit 1; the extra refusal is the missing-gate control's separate `check` and `accept` operations.
The original accepted source and evidence were not modified by these controls.

| Control | Actual mutation/input | Observed refusal |
| --- | --- | --- |
| Stale content revision | Append a newline to the produced Go test; same head commit, different source bytes | `accept`: current source does not match the sealed Subject |
| Missing gate / superseding attempt | Run only `protocol-tests` from a two-gate frozen task; new `attempt-0002` incomplete | `check` refuses; `accept` cannot reuse the earlier passing attempt |
| Weakened test | Insert `t.Skip` into baseline `TestSubjectHashUsesCompleteCanonicalSubject` in the disposable copy | `seal` refuses baseline-test modification before executing a weaker gate |
| Incomplete receipt | Remove `environment` from an otherwise valid receipt | `accept` refuses the different key set |
| Fabricated billing | Replace observed unknown `reported_cost: null` with `0` | `accept` refuses `$.cost.reported_cost: 0 != None` |
| Weakened frozen contract | Remove a required gate from frozen task JSON | `accept` refuses changed frozen task bytes |
| Omitted implementing worker | Remove its usage row while all six phase names remain | `receipt` refuses the missing implementing session |
| Double-counted model session | Append the same observed worker session again | `receipt` refuses duplicate model-session cost allocation |

The permanent CLI suite additionally covers malformed/duplicate/nonfinite JSON, named-test failures/skips, ignored source, symlinks, source mutation during a gate, independent-review identity, missing explanations, unknown/no-model accounting and tooling-revision changes.
Its executable-mode case is the one explicitly skipped platform predicate; it is not counted as passed or inferred as Linux qualification.

## Accounting observations

The completed lightweight worker session `01a1dfa7-64bf-72e4-bf4b-d093426ea348` records 23,336 input, 25,559 output and 486,464 cached tokens across 19 assistant records, including the simplification request.
Observed harness estimate: USD `0.037671599999999986`; reported/provider-billed cost and price version are null.
This is an unversioned harness estimate, not a bill, zero cost, measured saving or complete accounting.
Its private source snapshot digest is `sha256:bcf5f155ccf31f564ca871d8bdd9f968db23bd849012009609c191abb948179d`.

The completed-session population was independently enumerated from the current run's metadata: seven child sessions plus the primary coordinator, eight unique charged session IDs, zero unaccounted observed sessions and zero child delegation calls.
The positive row charges shared exploration, one-time evaluator construction, worker/reviewer execution and repairs once; the negative row charges only incremental escalation work.
This allocation is not a measurement of normal Go-edit cost and is not compared with a baseline.
The still-open primary session is an explicitly incomplete observed prefix: its known token subtotals are retained, complete-session token/cost totals remain null, and its timing window is recorded rather than summing overlapping session durations.
Unreported provider bills, price versions, harness-helper usage and the post-cutoff coordinator tail remain unknown, never zero.

## Verification boundaries

- The CLI and live Go surface were actually executed; neither required real sample was replaced with a mock provider or fixture-only success.
- The scoped skill was exercised by explicit loading in fresh worker/escalation contexts. Automatic model-invoked discovery was not measured; root `AGENTS.md` now points to the package.
- Separate configured reviewer invocation/context is proved, not model-family diversity or immunity to correlated mistakes.
- The local coordinator store is not an OS sandbox or adversarial append-only log. Torn attempts and abnormal validator-process records remain fail-closed and preserved, not automatically resumable.
- Source binding is before/after each gate, not atomic detection of transient edits restored inside one gate. Worker execution ends before coordinator verification.
- Existing full-Go output is regression context for unchanged Go package sources; the isolated focused gates, full manifest and independent review supply the exact evaluated-Subject binding.
- Hosted CI, Linux/race, Control, wire and browser gates were not run here. Their production surfaces are unchanged; these local results do not replace the required pre-merge CI matrix or qualify an unavailable native surface.
- No owner routing-policy verdict, provider bill, release/native-support promotion or Control `achieve` is issued by these receipts.

## Standalone Done-when mapping

| Required witness | Evidence and observed result |
| --- | --- |
| Frozen contract end to end, positive and negative real Go work | [Positive receipt](../evaluations/model-routing/0008a-positive.json) is `accepted=true`, with actual base/head, routing, task/check/review/usage digests and Subject `b14e8469…`; [negative receipt](../evaluations/model-routing/0008a-negative.json) is `accepted=false` on Subject `b8a84698…` |
| One bounded lightweight-worker task and explanations | `goal_subject_receiver_test.go`, frozen positive task and receipt `reviewed_explanations`; required names both ran and passed once, no skips; 56 top-level / 462 protocol tests including subtests passed |
| Real-boundary escalation with correct work retained | [Escalation handoff](../evaluations/model-routing/0008a-escalation.json): actual native Go exit 0 with no matching Linux test, validator exit 1, explicit escalation to Sol and preserved manifest `6fc242e1…` |
| Stale, missing, weakened and incomplete/fabricated inputs refused | [Live controls](../evaluations/model-routing/0008a-controls.json): 8 cases, 9 expected CLI refusals, every refusal exit 1; full argv and messages retained |
| Total-cost discipline, failed attempts and unknown evidence | Positive/negative phase rows cover all six stages without duplicate sessions; 8 observed unique model sessions charged once, actual prompt/source hashes and nullable totals; provider bills/price versions and the open coordinator tail remain unknown |
| Instructions/skills and independent review prevent silent weakening | [Source review](../evaluations/model-routing/0008a-review.json), configured separate reviewer session, protected-baseline and frozen-contract controls, exact named-test execution and additive CI gate audit; no model-confidence acceptance signal |

The independent source review and closeout judged these witness clauses complete and retained only the documented nonblocking limits.
Its source hash remains `sha256:1420761251e7764af223fc7c8d03f30291b854baa1b8ad3ff85e0ab638edeee1`; the final test-file hash is `sha256:99f0b36f93dc204e65553453224761cd3901bb97c5836145749aad239c011ef0`.
The reviewer did not execute the gates; the coordinator's observed outputs are the acceptance oracle.
The final data audit records reviewer dispositions and corrects its count shorthand: the positive row has seven model sessions and the negative row one, for eight globally unique sessions, not eight in the positive row alone.

## Closure and simplification

The goal moved to `docs/goals/archive/0008a-auditable-acceptance-workflow.md` without a text change: 5,441 bytes, SHA-256 `7ba127867073cb28285f50be340f42f6036588f6134142e5de7e75a6e37ed6f0`, byte-identical to the original base-contract copy.
The active path is absent, its index is updated, and five scoped canonical local-link targets were checked successfully (exit 0).
One attempted archival patch had an unnecessary mismatching README context hunk; the tool rejected it atomically, no file changed, and the corrected patch preserved the contract bytes.

Scoped simplification removed the initial Go test's duplicate canonical/hash/fixture assertions and wrapper-only helpers, reducing that delivery from 201 to 63 lines while retaining both required test names and all 11 rejection cases.
The runner reuses the existing source binder and Go event gate rather than wrapping a new model/provider service.
One incompleteness message now has a single owner; the shadowed timing variable was removed instead of papered over.
Protected assets, whole-checkout hashing, independent review, provenance and reproducible validator behavior were retained because each enforces a named contract boundary.

The final ledger's repair count is the number of explicitly listed corrective/rework intervention batches, not the number of mandatory repair-phase rows or failing assertions.
It includes rejected repair approaches, correction of an evidence-log extractor that initially matched its own assistant command, and the atomic archival-patch retry.
The rejected one-byte extractor artifact was retained privately and never used as gate evidence; the real delivered 20-test output was captured from a nonassistant event, normalized to LF, and bound by `sha256:b3975bd3a26463de5c043dfdc283d3854dd2a8d4b05e07d7c4f62e7a69d79969`.

The local data audit supplies the final receipt/ledger/review digests, explicit source-file manifest, session-accounting population and closeout dispositions.
Generated evidence and receipts are excluded from that source manifest to avoid a self-referential digest; all changed executable, gate, instruction and contract paths are included.
No new executable source or acceptance rule changed after the final 20-case gate and independent source review; subsequent changes are evidence generation, clarified metric labels and unchanged-contract archival/pointers.
The current checkout is left uncommitted and unpublished for verification.

## Final frozen audit

[`0008a-audit.json`](../evaluations/model-routing/0008a-audit.json) is the final machine-readable audit: 17 scoped source files, eight unique charged model sessions, 13 explicitly listed corrective/rework batches and no unresolved acceptance blocker.
Its SHA-256 is `697adc3cdcf998a7d32281188e29d7d8501462b1a73adf1fd11186a3f7fff636`; the scoped source-manifest digest is `bffb96cd27c4a5a986a8cbe80c84df85f27f04ed8465d523214d8a1a97a2bf00`.
The final positive receipt digest is `524e3beed5ccf310169400f27b21a6c73ab166814e49e43f7c925a159abceb24` (`receipt` exit 0, `accept` exit 0); the final negative digest is `a9b7260635f317f2fa66b57ca4587be4b932c79cfe8eb126dd77caf268d40c52` (`receipt` exit 0, `accept` exit 1).
Review/attempt/ledger/tooling digests are retained in each row's `evidence` block and in the audit.

The observed complete-record-plus-primary-prefix usage is 1,482,259 input, 710,029 output and 60,935,815 cached tokens; these are not asserted as complete-session totals.
The sum of available unversioned harness estimates is USD `1.006311`, not a provider bill, established spend, saving or financial lower bound.
Reported and total-estimated cost remain null, all authoritative price versions remain unknown, and `accounting_complete=false`.
The final snapshot includes the independent closeout call; the remaining coordinator/report tail is explicitly unknown rather than recorded as zero.
