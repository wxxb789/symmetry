# Go core App/State/Workspace test-ablation inventory

Date: 2026-09-29  
Scope: `daemon/internal/app`, `daemon/internal/state`, and `daemon/internal/workspace`, plus only production code needed to interpret assertions.  
Mode: source inventory and candidate discovery only. No test, source, fixture, journal, service, credential, or Git/index mutation was performed.

## Subject and counting rules

- Git HEAD is `d14debbf6ae3ded09e5013ef90952d347c01474d` on `codex/goal-0006b-crash-safe-process-containment`; the working tree had 666 Git-visible dirty/untracked paths when inspected. Scoped dirty and untracked files are part of this working-tree inventory but are not evidence for HEAD.
- There are **59 test files and 947 top-level Go test declarations** in scope: App **42 files / 633 declarations**, State **12 / 242**, Workspace **5 / 72**. There are no scoped benchmarks or examples.
- Counts include two subprocess fixture declarations which Go also discovers as tests: `TestDecisionLivenessChild` and `TestWorkspaceOutputHelper`. They are no-op unless their parent supplies the private environment selector.
- A "test count" below is the stable top-level declaration count. Dynamic/table subtests are described where relevant but are not added to it. No exact duplicate test body was found by whitespace/comment-normalized comparison across all 947 declarations.
- Categories: **U** unit/state transaction; **H** multi-component harness or subprocess; **S** smoke/startup path; **V** input/contract validation; **X** platform/native verification. Verification does not promote a local fake to qualified native acceptance.
- Ordinary entrypoints are `cd daemon && go test ./internal/app`, `go test ./internal/state`, and `go test ./internal/workspace`; focused execution adds `-run '^TestName$'`. Build-constrained files run only on their named platform. Linux owner-v2 requires Linux amd64. Windows files require native Windows execution; cross-compilation is not equivalent.

## Manifest reconciliation

The parent manifest at SHA-256 `a2156454bb520fd70b97917fc39c73dff58493be1935c06f0ea797884acea3fa` contains all 59 scoped `_test.go` files and no scoped false-positive test file. Its 42 App + 12 State + 5 Workspace paths exactly match the filesystem and Git cached/non-ignored-untracked enumeration.

One scoped test-support omission exists in the manifest's additional fixture/entrypoint section:

- `daemon/internal/app/pi_control_e2e_options.go` (SHA-256 `64afff868233902ad0579e2e501210bcf601438323ce75f9a2eaa5d1f2847b8a`) is build-tagged `symmetry_pi_control_e2e`. It exposes only `WithPiControlE2EWitnessRegistry` and `WithPiControlE2EProcessObserver` to the explicitly built Pi-Control E2E daemon child. It is **H support**, not a test, production entrypoint, or removal candidate; retain it with its external E2E consumer.

All other non-test `.go` files in these directories are production implementations or platform adapters, not fixture candidates. In particular, the untracked `linux_owner_{preflight,recovery,v2,witness}*.go` and `state/linux_clone_cleanup_reader_*.go` own production preflight, recovery, authority, witness, or durable readback behavior. Treating them as extra test fixtures would be a false positive. There is no scoped `testdata` directory.

## Per-file exhaustive ledger

Disposition is **retain** unless a candidate ID is shown. "Retain" means no source-grounded deletion candidate in the file; it does not preclude fixture-helper extraction after a separately proven experiment.

### App (42 files, 633 declarations)

| File | Count | Cat. | Guarded behavior / boundary | Platform and disposition |
|---|---:|---|---|---|
| `app_test.go` | 173 | U/H/S | enrollment/claim/retry, input and command ordering, outbox/terminal/cleanup, lease/liveness, recovery, JSONL/redaction/backpressure, concurrency, native settlement | all; retain except C1-C4 experiments |
| `app_unsupported_test.go` | 1 | S/V | unsupported OS fails before enrollment without process identity | `!linux && !windows`; retain |
| `atomic_terminal_ack_interleaving_test.go` | 2 | U/H | real Store atomic terminal+ack write, unknown post-write interleaving, conflict readback | all; retain |
| `cleanup_test.go` | 15 | U/H | cleanup cadence/cancellation, unrelated outbox progress, recovery intent, candidate artifact gate, process-wait barriers | all; retain |
| `control_failure_test.go` | 2 | U/H | supervisory stdin failure idempotence and paused-exit retention failure | all; retain |
| `decision_liveness_test.go` | 3 | H/S/V | real blocked child stopped on required-decision failure; legacy question remains live | all; retain; one declaration is fixture support |
| `decision_requirement_test.go` | 5 | U/V | required-vs-legacy decision validation, >10 options, persisted capability, terminal error cause | all; retain |
| `deterministic_validation_test.go` | 21 | U/H/V | immutable artifact-only validation, exact subject/path/evidence, deadline/cancel/fence/native exclusion, atomic terminal queue | all; retain |
| `generic_process_stop_test.go` | 1 | U/H | containment/termination errors retain recovery barrier with/without marker (4 rows) | all; retain |
| `goal_delivery_test.go` | 51 | U/H | native usage ledger, attach/stop delivery, recovery/readback, stale/conflict handling, terminal-owner races | all; retain |
| `harness_test.go` | 91 | U/H/V | registry/probe, strict admission, Pi resume/rebind, native lifecycle/compensation, subject/provider/deadline gates | all; C5 setup consolidation only |
| `lease_renewal_test.go` | 7 | U | lifecycle fence, relative deadline/skew, first renewal, legacy expiry | all; retain |
| `lease_stale_persistence_test.go` | 10 | U/H | stale write/readback, native owner retention, stop-witness and owner-release settlement | all; retain |
| `linux_owner_preflight_linux_test.go` | 22 | U/V/X | installed identity, legacy drain, systemd/delegation, trusted Git/ancestors, ACL/group topology | Linux amd64; retain; not installed positive acceptance |
| `linux_owner_v2_app_test.go` | 44 | U/H/X | App callbacks, promotion/cleanup journals, recovery ordering, v2 admission/capacity/lease/reuse/retention | Linux amd64; retain; C6 explicitly nonredundant |
| `linux_owner_witness_linux_test.go` | 19 | U/V/X | witness opt-in/path trust, A/B denials, shared Store, replay categories, release/clear publication, group contracts | Linux amd64; retain; qualified host needed for positive X |
| `linux_owner_witness_unsupported_test.go` | 1 | V/X | explicit unsupported-host witness refusal | `!linux || !amd64`; retain |
| `native_lease_settlement_test.go` | 2 | U/H | waiter owns lease expiry; observed terminal beats watchdog | all; retain |
| `native_process_stop_test.go` | 7 | U/H | stop proof vs task failure, final-stop error, persistence/discard/close ownership | all; retain |
| `native_settlement_final_regression_test.go` | 9 | U/H | usage restart/retry, task-result/usage readback, cancellation/open races, bounded final wait | all; retain |
| `native_settlement_regression_test.go` | 19 | U/H | publication cancellation, output evidence, terminal/cancel ACK loss, usage/cleanup races | all; retain |
| `native_start_attachment_barrier_test.go` | 1 | U/H | cancellation cannot cross blocked durable attach-ready receipt | all; retain |
| `output_settlement_test.go` | 3 | U/H | truncation not hidden by semantic success; terminal flush drains marker | all; retain |
| `pi_provider_bridge_durable_test.go` | 14 | U/H/V | Store-backed replay, token redaction, changed input, unknown outcomes, capacity, stale state | all; retain |
| `pi_provider_bridge_test.go` | 17 | U/H/V | HTTP/control mapping, cancellation, strict result/token safety, bridge preparation | all; retain; fake control is not provider acceptance |
| `prelaunch_recovery_fault_test.go` | 1 | U/H | prelaunch intent survives terminal persistence fault/restart without fabricated usage/retention | all; retain |
| `process_cleanup_test.go` | 12 | U/H | generic/native marker cleanup, stop-proof reuse, stale waiter, usage-before-cleanup | all; retain |
| `process_finalization_test.go` | 3 | U/H | exited-process finalization, retry bound, replacement-owner exclusion | all; retain |
| `process_persistence_test.go` | 8 | U/H | atomic marker publication, post-rename replay, supervisor callback fence/abort proof | all; retain |
| `recovered_stop_witness_test.go` | 6 | U/H | recovered stop-proof reuse, clear/receipt failures, replacement/fresh-daemon exclusion | all; retain |
| `recovery_handoff_test.go` | 11 | U/H | prepared/bound supervisor recovery, retention, abort fallback, receipt-before-release, input ordering | all; retain |
| `recovery_isolation_test.go` | 6 | U/H | unrelated recovery progress, typed pending retry, snapshot/retry scheduling | all; retain with prior-review P2 timer-source limitation |
| `recovery_linux_test.go` | 3 | U/X | containment-unproven marker, marker requirement, authenticated mirror seal ordering | Linux; retain |
| `recovery_receipt_release_test.go` | 8 | U/H/X | receipt before helper release, post-rename replay, mismatch/persistence/retry authority | all with platform doubles; retain |
| `recovery_registration_test.go` | 8 | U/H | terminal queue failure blocks registration, independent progress, canonical replay | all; retain |
| `retention_failure_test.go` | 3 | U/H | retention failure cannot bypass cancellation/lease fencing or process-control ordering | all; retain |
| `stale_ack_settlement_test.go` | 2 | U/H | stale cleanup waits for ACK; conclusive ACK error retires first | all; retain |
| `supervisory_test.go` | 10 | U/H/V | guidance replay, pause/resume/cancel/restart, capability and decision semantics | all; retain |
| `terminal_settlement_race_test.go` | 2 | U/H | first durable terminal wins; matching usage owner finalizes under concurrency | all; retain; race run distinct |
| `terminate_linux_test.go` | 6 | U/X | identity-bound terminator, absent proof, changed identity, fail-closed release | Linux; retain |
| `terminate_windows_test.go` | 3 | U/X | PID identity mismatch, no PID-only stop, unverified identity refusal | Windows; retain |
| `timestamp_test.go` | 1 | U/V | Control UTC microsecond timestamp encoding | all; retain |

### State (12 files, 242 declarations)

| File | Count | Cat. | Guarded behavior / boundary | Platform and disposition |
|---|---:|---|---|---|
| `control_test.go` | 7 | U | durable intents/receipts, pause/resume/wait/cancel, forged ACK rejection, cleanup retirement | all; retain |
| `event_sequence_test.go` | 1 | U | concurrent output/control receipt allocate one monotonic sequence (21 events) | all; retain |
| `goal_delivery_test.go` | 22 | U | attach/stop/usage durability, digest, atomic evidence+terminal, ready/rebind gates | all; retain |
| `goal_session_test.go` | 44 | U | launch/lineage/rebind/restart/compatibility, stop certificates, uncertain/close, atomic failures | all; retain |
| `linux_cgroup_v2_test.go` | 10 | U/X | reservation/commit/release replay, workspace barrier, foreign admission, v1/v2 disjointness | Linux; retain |
| `linux_clone_journal_test.go` | 13 | U/X | legacy decode, clone abort/no-target proof, CAS/fence, cleanup begin/complete, checkout/promotion replay | Linux; retain |
| `process_cleanup_test.go` | 18 | U | process/authority transactions, stop receipt/CAS, cleanup barriers, stale snapshot exclusion | all; retain |
| `provide_input_idempotence_test.go` | 1 | U | confirmed command rejects changed payload and accepts exact replay | all; C7 retain |
| `provider_action_test.go` | 30 | U/V | prepare/dispatch/recovery/idempotence, byte budgets, duplicate JSON, result normalization | all; retain |
| `state_test.go` | 69 | U/V | identity/security/atomic files, journals/outbox/input/terminal/cleanup/lease/wait, corruption/concurrency | all; C7 comparison |
| `state_windows_test.go` | 5 | U/X | protected DACL, sharing retry/bound, rename classification | Windows; retain |
| `supervisor_handoff_test.go` | 22 | U/X | prepare/bind/commit/receipt/release/abort CAS and unknown writes; scan seals/legacy | all; retain |

### Workspace (5 files, 72 declarations)

| File | Count | Cat. | Guarded behavior / boundary | Platform and disposition |
|---|---:|---|---|---|
| `artifact_test.go` | 9 | U/H/V | exact committed blobs ignore checkout/replace refs, reject unsafe/missing paths, preserve literal/binary bytes | all, real Git; C8 retain |
| `fingerprint_test.go` | 4 | U/H/V | canonical aliases, repository/worktree binding, non-Git rejection, cancellation | all, real Git; retain |
| `linux_clone_linux_test.go` | 22 | U/H/X | real filesystem/Git seed, temp recovery, no-follow/inode/mount guards, promotion/phased cleanup | Linux amd64; retain |
| `subject_test.go` | 15 | U/H/V | detached committed subject/materialization, replace-ref/dirty resistance, output bounds/cancel | all, real Git; retain; one subprocess fixture declaration |
| `workspace_test.go` | 22 | U/H/X | prepare/recover/adopt/reservation concurrency, foreign refusal, durable journal, cleanup ownership | all, real Git/filesystem; C9 retain |

## Candidate ledger and smallest experiments

These commands and mutations are proposals for the parent-owned experiment phase; none ran here. A deletion is not admitted until baseline and failure-oriented variant both establish the named surviving coverage.

### C1 - one duplicated startup scenario (actionable merge/ablation)

`app_test.go` SHA-256 `c7b34a0216dce620bf97ba76b30e5828bd925a4f7d1bd02f044a1d97349cc1c2`: `TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration` and `TestTerminalFlushFailureDoesNotPreventNewSessionRegistration` have identical Store setup, terminal journal, identity, context, `terminalRecoveryControl` failure, `Run` call, and journal-retention assertion. The first uniquely asserts registration occurs before terminal flush (`registeredAfterFlush == false`); the second only adds exactly one registration call. Same platform, boundary, environment, and fault make the second a concrete merge/removal candidate.

- Baseline: `go test ./internal/app -run '^(TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration|TestTerminalFlushFailureDoesNotPreventNewSessionRegistration)$'`.
- Variant: add `control.registerCalls == 1` to the first, then ablate only the second declaration.
- Mutations: reorder startup so flush precedes registration (ordering assertion red); duplicate `RegisterSession` (count red); allow failed flush to delete journal (retention red).
- Exact survivor: registration-before-flush, exactly one registration, temporary transition failure, unchanged terminal journal.

### C2 - claim-ID tests are not redundant (retain)

`TestClaimRetryReusesPersistedClaimID` ends a first assignment on definitive Control `state_conflict`, starts the assignment again, and proves persisted-intent reuse. `TestTransientClaimRetryUsesPersistedClaimID` injects transport interruption and proves internal retry-loop reuse within one assignment. Setup and assertion overlap, but failure boundary and entrypoint lifetime differ.

Experiment: mutate internal transport retry to allocate a new ID (only transient test red), then mutate new-assignment reload to discard persisted intent (only conflict/restart test red). Both rows survive; only setup factoring is possible.

### C3 - terminal verdict fixtures can be tabled, assertions cannot be ablated

`TestTerminalAcceptanceReleasesSlotBeforeCleanup` and `TestTerminalVerdictPersistsAndReleasesSlotBeforeCleanup` share Store/clock/slot/failing-cleanup setup. The first requires accepted + `cleanup_pending`; the second injects `terminal_grace_expired` and requires a distinct durable verdict. Both require pre-cleanup slot release.

Experiment: table both rows. Mutate grace-expiry classification to accepted (grace row red), then prevent accepted cleanup-pending (accepted row red). Exact coverage retains both verdicts, timestamp, slot release, and accepted cleanup state.

### C4 - native late-cancel pair is a table candidate, not ablation

`TestLateCancelAfterStagedTerminalObservationCannotWin` and `TestObservedNativeFailureWinsLateCancel` share staged-session setup and synchronization. Success requires completed + rejected cancel ACK; observation error requires failed + failed ACK. Those are distinct external outcomes.

Experiment: table injected wait error and expected terminal/ACK. Independently mutate failed-observation classification and successful-observation classification; corresponding rows must fail. Preserve exact channel event and bounded timeout.

### C5 - admission rejection fixtures share setup, not guards

`harness_test.go` SHA-256 `658cac07b2c1d092e32d69f7c8eb4d42fc703c91a81464f6f580ebd283171b33`: strict cost cap, runtime repository mismatch, and runtime profile mismatch repeat admission and no-side-effect checks. They validate independent fields/error tokens; profile additionally asserts no workspace recovery/path persistence.

Experiment: table mutation and expected token, retaining profile-only assertions. Mutate each validator independently; only its row should turn red. Keep all three validation rows.

### C6 - cleanup replay matrices are intentionally distinct (retain)

`linux_owner_v2_app_test.go` SHA-256 `4fbb4d5b0dfb7f28e1e515fae80aa7d03d38a9deefed3ae9b416396b93122a77`: `TestLinuxCloneCleanupRecoversAcrossAAndCompleteFaults` has three rows and uniquely covers post-rename Complete ACK loss/exact write count. `TestLinuxCloneCleanupReplaysAcrossAAndCompleteFaults` has two rows and uniquely asserts A-material/effect state on each side of the external effect. This confirms prior independent review; the recently repaired capacity fixture also remains guarded.

Experiment if reconsidered: mutate Complete replay to write twice (first matrix red), then run A effect before pre-A fault or retain material after effect (second red). A survivor must preserve all five obligations. Until then retain both.

### C7 - specialized provide-input idempotence is distinct

`provide_input_idempotence_test.go` SHA-256 `748f9a99f853d18a147fb71b5613392f0a62b58a3b64fd0c4775aa667f07e2b1` versus `state_test.go` SHA-256 `68a356a058b70eb9a3100394b7b475a9ee908e3691e01eed3947e4a85876c022`: the broad test covers atomic completion/delivery/episodes. The specialized test changes payload digest, transition ID, and ACK ID after confirmation and delivery, asserts no replacement, then accepts exact replay.

Experiment: mutate `PrepareProvideInput` to reuse a delivered command ID with changed payload. Specialized test must fail while broad test can remain green. Retain specialized coverage.

### C8 - artifact positive setup is shared; detection is distinct

`artifact_test.go` SHA-256 `ab1fcee78bad2656d289161b19ac519bae175e686cb889e89c6c63070f12b85e`: dirty checkout, literal wildcard/colon path, and binary/no-newline tests all assert bytes/digest, but detect object-DB-vs-checkout, pathspec interpretation, colon syntax, and byte preservation respectively.

Experiment: independently mutate reads to checkout bytes, pass path as Git pathspec, or trim/text-convert blob output. Dirty, wildcard/colon, and binary rows respectively must fail. Factor setup only.

### C9 - Prepare and Recover adoption use distinct public entrypoints

`workspace_test.go` SHA-256 `d242cc6608437990155561b43781d12773ff6c43ed70bdfa72daae7a25fed060`: `TestPrepareAdoptsReservedWorktreeAfterJournalLoss` and `TestRecoverAdoptsRegisteredWorktreeAfterJournalLoss` share missing-journal state. One re-enters `Prepare`; one invokes explicit `Recover` with persisted path. App startup and new assignment use different entrypoints.

Experiment: mutate only `Prepare` adoption, then only `Recover`; corresponding test must fail independently. Shared setup is possible, but coverage must invoke both methods and verify same path, restored journal, and owned cleanup.

## Repeated-row conclusion

Inspection covered table loops, repeated fixtures, generic-versus-specialized tests, setup, assertions, failure modes, platforms, boundaries, and environment rather than names alone. The main matrices protect distinct classifier rows (HTTP status, admission field, fault stage, fence, platform, lifecycle state). The only supported declaration ablation is C1 after moving its one additional assertion. C2-C5 and C7-C9 permit at most table/helper consolidation; C6 explicitly retains both matrices. No support fixture can be removed while its parent remains.

Real `state.Store` transactions, Git object reads, native identity/authority, platform build tags, ordinary versus race execution, and qualified installed witnesses remain distinct. A fake native/control session is not real-provider acceptance. Unavailable Linux/Windows prerequisites are blocked evidence, not redundancy.

## Blockers and evidence limits

- This was intentionally read-only inventory. No baseline, ablation, mutation, package suite, race suite, native witness, build, service, or credentialed command ran. Experiments remain parent-owned.
- Native Windows requires Windows. Linux owner-v2 positive verification requires qualified Linux amd64 systemd A/B topology, delegated cgroup, installed identities, and where applicable protected Pi/OpenCode/Control inputs. Their absence cannot justify deletion.
- The dirty tree includes untracked production and tests. Hashes bind candidate statements to inspected working-tree bytes; later edits require affected re-inventory.
- Actual served model: **`ghc/gpt-5.6-sol-fast`** (`PI_PROVIDER=ghc`, `PI_MODEL=gpt-5.6-sol-fast`).
