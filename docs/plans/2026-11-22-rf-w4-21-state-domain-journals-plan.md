# rf-w4-21 — state domain-journal sub-ledgers over one Store core (plan)

Contract: `goal-rf-w4-21-state-domain-journals.md` (SHA-256 bound at BOUND), last
binding sections = conductor decision arch-decision-2 (2026-11-21), its
open-item rulings (O27/O28), the verification amendments, and the owner comment
policy (2026-11-22). This plan records only the implementation choices the
contract leaves open; the accepted design is executed as written.

Base: `2db5ed0ceb` (= `origin/main`, worktree branch
`refactor/rf-w4-21-state-domain-journals`). Dependencies re-verified merged at
base: W2-09 #165, W3-07 #200, W3-08 #214, W3-15 #217, W4-12a #244;
arch-decision-2 BOUND (`docs/goals/0010-refactor-program.md:172,597-598`).

## 1. Sub-ledger membership (binding names, membership re-derived here)

Contiguity is load-bearing: the embedded value sub-ledgers must occupy exactly
the declaration positions their fields hold today so `encoding/json` writes the
same keys in the same order. Base spans are `daemon/internal/state/state.go`
lines at `2db5ed0c`.

| Sub-ledger | Fields (base order) | Base span |
|---|---|---|
| `RunCloneState` | `workspace_path`, `workspace_recovery_required`, `retain_workspace`, `workspace_binding_key`, `linux_clone_intent`, `linux_clone_seed_receipt`, `linux_clone_checkout_receipt`, `linux_clone_reuse_grant`, `linux_clone_reuse_retired`, `linux_clone_reuse_aborted_before_native_start`, `linux_clone_promotion_intent`, `linux_clone_promotion_receipt`, `linux_clone_abort_pending`, `linux_clone_abort_proof`, `linux_clone_cleanup_pending`, `linux_clone_cleanup_intent`, `linux_clone_target_removal_proof`, `linux_clone_cleanup_proof` (18) | `state.go:680-722` |
| `RunContainmentState` | `pid`, `process_identity`, `started_at`, `containment_authority`, `containment_handoff`, `containment_cgroup`, `containment_cgroup_release_proof`, `containment_cgroup_pre_gate_release_proof`, `containment_scans`, `containment_unproven` (10) | `state.go:723-742` |
| `RunOutboxState` | `last_event_sequence`, `pending_events`, `dropped_output_chunks`, `dropped_output_bytes`, `pending_transitions`, `attempted_transition_ids`, `pending_command_acknowledgements` (7) | `state.go:743-749` |
| `RunGoalDeliveryState` | `pending_goal_deliveries`, `delivered_goal_deliveries`, `retired_goal_deliveries`, `goal_delivery_enabled` (4) | `state.go:750-753` |

Remaining `RunJournal` fields (24) stay direct: run identity/lease/terminal
block, `work`, native-usage block, input/control/provider intents. JSON
byte-identity is proved by the golden suite (§5) and by the JSON key-order table
in the evidence record.

Workspace ownership joins `RunCloneState` because every validator that binds
the reserved clone target reads that block's workspace/clone fields (`validateLinuxCloneAbortPending`,
`…CleanupPending`, `…CleanupProof`, `linuxV2ReleasedCloneStateSafe`,
`linuxCloneReuseSourceMatches`); the alternative (workspace in the core) leaves
the clone sub-ledger with one owned predicate and forces contrived parameter
lists. `RunCloneState` therefore owns the run's workspace/clone-ownership
authority, not Linux-only fields.

## 2. Predicate / validation moves

Rule: a predicate moves to a sub-ledger when every *field* it reads belongs to
that sub-ledger and its remaining context can travel as a parameter. Bodies stay
byte-identical where possible; the receiver keeps the name `journal`, so field
reads compile unchanged.

* `RunContainmentState`: `validateContainmentAuthority`,
  `validateContainmentHandoff(granted bool)`, `validateContainmentCgroup(granted bool)`,
  `validateContainmentScans`, `validateContainmentCgroupReleaseProof`,
  `validateContainmentCgroupPreGateReleaseProof`,
  `requireLinuxScansCleanForAuthority`, `requireLinuxScansSealedForHandoff`,
  `requireLinuxScansSealedForAuthority`.
* `RunCloneState`: `validateLinuxCloneIntentAbsent`,
  `validateLinuxCloneCleanupPending`, `validateLinuxCloneCleanupProof`,
  `linuxCloneStageCanAdvance(localState string)` — the four shipped receivers.
  `validateLinuxCloneAbortPending(journal RunJournal, intent *authority.LinuxCloneIntent)`
  and `linuxV2ReleasedCloneStateSafe(journal RunJournal) bool` stay free functions on
  the whole journal: the first calls `linuxCloneAbortFenceAvailable` /
  `linuxCloneAbortPendingReleaseUnresolved`, which scan containment state, and the
  second compares `journal.RunID` / `journal.Generation`; both bodies stay
  byte-identical to base (`linux_clone_validation.go:9`, `:221`).
* `RunOutboxState`: `validatePendingEntries(runID string, fence protocol.Fence)`
  (split of `validatePendingJournalEntriesAndDeliveries`, which keeps the
  `validateGoalDeliveries` tail because that validator lives in
  `goal_delivery.go`, outside this slice's owned set); the transition helper
  stays the free function `hasPendingTransition(transitions, transitionID)`
  (`run_outbox.go:872`) with `RunJournal.HasPendingTransition` delegating to it.
* `RunGoalDeliveryState`: `hasExactUsageDelivery(runID string, fence protocol.Fence, usage protocol.Usage)`.

Exported predicates (`HasProcessDetails`, `HasPendingContainment`,
`ContainmentObservationUnresolved`, `RecoveryHelperScanSealEligible`,
`HasPendingTransition`, `LinuxCloneCleanupEligible`,
`LinuxCloneCleanupBeginEligible`, `LinuxCloneCleanupIntentForJournal`) stay
declared on `RunJournal`: the gates clause reads "`go doc` diff now records the
intended exported-type additions only", which is the stricter of the two
clauses; keeping exported methods in place makes the doc diff exactly the four
new types plus `RunJournal`'s embedded field block. They still read the moved
fields through promotion; callers are unchanged. Free functions called from
non-owned files or non-owned tests (`appendEvent` from `control.go`,
`hasPendingTerminalTransition` from `control.go`, `linuxCloneArtifactReceiptRequired`
from `linux_clone_journal_test.go`, `pendingTerminalState` from `state_test.go`)
keep their free-function signatures.

## 3. Shared fenced-CAS helper

New kernel declaration in `state.go`:

```go
func (store *Store) mutateJournalCAS(key RunKey, fence protocol.Fence, stale error, mutate func(*RunJournal) error) (RunJournal, error)
```

It owns exactly the CAS premise the folded sites duplicate: under the Store lock
after `loadJournalLocked`, `journal.Fence() != fence` returns the site's own
`stale` error (the requested RunKey already matched in `loadJournalLocked`),
then `mutate` runs and the existing capacity-guarded write follows. Folded sites
(base → helper, base lines):

| # | Site (base) | Base fence check | Folded shape | Base mutant (hunks) | Killing test |
|---|---|---|---|---|---|
| 1 | `AbortLinuxCloneReuseBeforeNativeStart` `:1702` | `:1710` key/fence + grant | helper + grant check in closure | `:1710` remove key/fence comparands | new `TestLinuxCloneReuseAbortRejectsStaleFence` |
| 2 | `BeginLinuxCloneCleanup` `:1943` | `:1948` | helper only | `:1948` + `:956` `LinuxCloneCleanupBeginEligible` fence comparand | new `TestLinuxCloneCleanupBeginRejectsStaleFence` |
| 3 | `RecordLinuxCloneTargetRemoval` `:2007` | `:2017` | helper only | `:2017` + `:956` | `TestLinuxCloneCleanupBeginCompleteRequiresExactReadbacks` `:919` |
| 4 | `CompleteLinuxCloneCleanup` `:2040` | `:2048` | helper only | `:2048` | new `TestLinuxCloneCleanupCompleteReplayRejectsStaleFence` |
| 5 | `ReserveLinuxCgroup` `:2686` | `:2703` fence + admission fence, **after** the `:2700` claim-grant check | **retained inline** — the helper's comparand runs before the closure, so folding moves the fence check ahead of `journal.hasClaimGrant()` and turns the `journal has no claim grant` error into a stale-fence error for a grantless journal (`Linux cgroup reservation RunKey/fence is stale`); the frozen error-identity/order set forbids that. The mutant therefore edits this site's own comparand (`linux_cgroup_journal.go:562` at HEAD) plus the `:415`/`:464` scan duplicates | `:2703` fence comparand + `:415` `checkLinuxV2NativeAdmissionLocked` comparand + `:464` `targetFound` proof | `TestLinuxV2AdmissionAllowsLiveFencedAndPhysicallyReleasedOwners` `:737` |
| 6 | `MarkLinuxCgroupCreateStarted` `:2736` | `:2749` | helper + `validLinuxV2AdmissionFence` in closure (no grant check to reorder) | `:2749` + `:415` | same test, `:780` |
| 7 | `ArmLinuxCgroupLauncherGrant` `:2871` | `:2883` | helper + `validLinuxV2AdmissionFence` in closure, reusing the site's stale error | `:2883` + `:415` | new `TestLinuxCgroupLauncherGrantRejectsStaleFence` |
| 8 | `AbortNeverCreatedLinuxCgroupReservation` `:3030` | `:3049` fence + grant | helper + `hasClaimGrant` in closure | `:3049` fence comparand | `TestAbortNeverCreatedLinuxCgroupReservationUsesOnlyUnstartedFence` `:266` |
| 9 | `BindLinuxCloneReuse` `:1534` | `:1546` fence + grant + effect-ready | helper + grant and effect-ready checks in closure | `:1546` fence comparand | new `TestLinuxCloneReuseBindRejectsStaleFence` |

Sites 2/3/5/6/7 carry a duplicate of the same comparand on the same path. The
fold keeps those duplicates for their non-CAS callers (`LinuxCloneCleanupBeginEligible`
keeps its own comparison; the admission scan keeps `target.Fence() != fence` and
its `targetFound` proof), so each per-site mutant must remove both the helper's
comparand and the site's remaining copy; the base mutant removes both copies the
same way. Every mutant is run red at base and re-run at HEAD with those
comparands removed (evidence record). Clone mutations whose fence proof
is not a bare key/fence compare (`requireLinuxCloneJournalFence`: intent,
binding key, admitted subject) keep their own proof; they are not folded and the
table above is the complete folded set.

## 4. Files (kernel keeps the path)

`daemon/internal/state/state.go` stays the Store kernel (`Store`, `RunKey`,
`New`/`Close`, atomic seam, load/save/mutate/CAS, JSON IO, path helpers,
`writeAtomic`, shared scalar validators). New same-package files:
`identity.go`, `run_journal.go`, `journal_validation.go`, `run_outbox.go`,
`run_goal_delivery_journal.go`, `linux_cgroup_journal.go`,
`linux_clone_journal.go`, `linux_clone_validation.go`,
`containment_journal.go`, `supervisor_handoff_journal.go`. Every file of the
package this slice writes is ≤ 1,500 lines (`wc -l` table gate); pre-existing
files not owned by this slice (`goal_session.go` 2,149 L and the pre-existing
test files) are reported in the table as untouched.

Test split: `state_test.go` (3,324 L) splits into `store_test.go` (370 L,
kernel/IO), `identity_test.go` (266 L), `run_journal_test.go` (638 L),
`run_outbox_test.go` (1,012 L), `containment_test.go` (108 L, the
process-marker tests) and `journal_validation_test.go` (890 L, the
`validateJournal` pin suite and its fixtures, which does not fit
`run_journal_test.go` under the 1,500-L cap); test functions move
verbatim, shared helpers stay in the family file that uses them or in
`state_helpers_test.go` when ≥2 files use them (recorded: `mustStore`,
`testJournal`, `stoppedTestJournal`, `pinnedJournalLauncherBoundCgroupReservation`,
`pinnedJournalAbortedCgroupReservation`, `pinnedJournalPreGateStopReceipt`);
the three primitives those `pinnedJournal*` fixtures call
(`pinnedJournalCgroupLauncher`, `pinnedJournalMinimalCgroupReservation`,
`pinnedJournalHelperBoundCgroupReservation`) stay in `journal_validation_test.go`
even though two files use them: moving them would churn about 20 call sites in
the validator pin suite for no behaviour change (recorded exception);
`go test -list` stays a superset of base on Windows and WSL. New files this
slice adds to the package: `state_fence_test.go` (fence-pin tests,
`//go:build linux`), `golden_test.go` and `testdata/goldens/*.json`.

## 5. Goldens

`golden_test.go` asserts, per golden file: (a) the committed bytes decode with
the production decoder and re-encode byte-identically; (b) the code-built value
encodes to the same bytes. Families and encoders: `run-journal-{minimal,clone,
containment,outbox,goal-delivery}.json` (`decodeJSONWithLimitAndTags` +
`serializeRunJournal`), `goal-session-journal.json`, `late-goal-usage.json`,
`machine-identity.json`, `enrollment-intent.json` (`json.Unmarshal` +
`json.Marshal`, the `writeJSON` path). Goldens are generated at base by a
temporary `golden_gen_test.go` and deleted with it; the guards are encoder-level
round-trips over representative values, not validation fixtures (documented in
the evidence record). Red mutant: a `json` tag renamed on a moved field makes
both directions fail.

## 6. Composite-literal impact (O28)

15 `RunJournal` composite literals set a moved field. Outside
`daemon/internal/state` (12, mechanical field-path edits allowed by O28):
`cmd/symmetry-daemon/preauthority_witness_command_windows.go:470`,
`e2e/core_test.go:1154,1388`,
`e2e/production_linux_containment_witness_test.go:3386`,
`internal/app/app_test.go:4903`, `internal/app/journal_store_linux_test.go:73`,
`internal/app/process_persistence_test.go:565`,
`internal/app/recovery_handoff_test.go:465`,
`internal/app/recovery_linux_test.go:62`,
`internal/execution/linux_owner_v2_test.go:494`,
`internal/workspace/linux_clone_linux_test.go:1238,1283`.
Inside the package (3): `state.go:1316` (kernel, owned), `state_test.go:2387`
(owned), and `goal_delivery_test.go:971`.

The toolchain accepts the promoted field keys unchanged: Go 1.27.4 type-checks
and routes `Outer{Inner: Inner{A: "y"}}` and `Outer{A: "y"}` to the same field
(probe recorded in the evidence record), so all 15 sites compile and set the
same fields with **no literal edit at all** — including the `goal_delivery_test.go`
line, which therefore needs no change and no non-owned file is touched. O28's
mechanical-edit allowance stays unused; the AST literal dump base vs HEAD must
therefore be byte-equal (recorded in the evidence record).

## 7. Citation refresh and R9

`scripts/ci/check-citation-existence.py` checks only path existence plus, for
prose-bound symbol citations, whole-word occurrence in the cited file; line
numbers are stripped. `state.go` keeps its path, so the gate stays green.
Living documents are refreshed to the new file:line (`docs/TODO.md`,
`docs/archive/CLOSED-TODO.md`, `docs/design/**`). Historical records
(`docs/plans/**`, `docs/evidence/**`, `docs/goals/**`) are revision-bound
evidence for their own base revision; they keep their old `state.go:<line>`
citations and the evidence record lists them with their new locations as a
reference table. R9: `scripts/ci/omo-control-native-evidence.py` `SOURCE_PATHS`
gains every new file that receives `state.go` code, and the mapping table
(new file → base spans) is recorded in answer.md; the script's own test runs at
the slice head.

## 8. Out of scope

Validator decomposition, error wrapping, unexporting (merged prerequisites);
`goal_session.go`, `goal_delivery.go`, `provider_action.go`, `control.go`,
`goal_usage.go`, `cgroup_release_linux.go`, the readers and the platform files;
any behavioural change; test-unit splits inside functions; the app-side CAS
replay duplication (O9); `RunJournalRecorder`; `RunCloneState` fields that would
require editing non-owned production files.

## 9. Evidence plan

Order: (1) base baselines (`go doc -all`, `go test -list`, four builds, `wc -l`)
and the AST declaration inventory; (2) the fence-pin tests and the eight base
mutants; (3) goldens at base and their red mutant; (4) the split + deepenings,
with `gofmt`, `go vet`, four builds, `go doc` diff, `go test -list` superset,
Windows tests, WSL `-race` state+app tests, golden suite, HEAD CAS mutant,
citation checker, `test-omo-control-native-evidence.py`, `wc -l`, `git diff
--check`; (5) `docs/evidence/rf-w4-21-state-domain-journals-2026-11-22.md` and
the implement-stage report. The AST tools and mutant harness run from a scratch
directory outside the repository and are not committed.
