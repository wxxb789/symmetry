# rf-w4-12a `journalStore` — evidence record (2026-11-22)

Slice: W4-12a of `goal-rf-w4-12-go-test-doubles.md` (arch-decision-2 R1, R0; verification
amendments; comment policy 2026-11-22; PR #234 conductor amendment). Plan:
`docs/plans/2026-11-22-rf-w4-12a-journal-store-plan.md`.
Base revision of the working tree: `adfe7d6a` (`refactor(app): split startGoalAdmission into
phases (#234)`); the delivered revision identity is recorded in §1.

Dispatch gates re-checked at this revision (`git merge-base --is-ancestor`): W4-01 `adfe7d6a` yes,
W3-08 `6d428d8f` (`refactor(state): unexport the test-only state surface (#214)`) yes, W3-15
`de526861` (`test: split the giant test units (#217)`) yes.

## 1. Deliverable shape

| File | Kind | Size |
| --- | --- | --- |
| `daemon/internal/app/journal_store.go` | new: `journalStoreBase` (99 methods) + `var _ journalStore = (*state.Store)(nil)` | 115 L |
| `daemon/internal/app/journal_store_linux_amd64.go` | new: `journalStore` = base + 24 linux/amd64-only methods | 38 L |
| `daemon/internal/app/journal_store_other_targets.go` | new: `journalStore` alias on every other target | 5 L |
| `daemon/internal/app/journal_store_test.go` | new: member-set pin + 23 storeless-branch probes + the shared valid-authority fixture | 433 L |
| `daemon/internal/app/journal_store_linux_test.go` | new: linux/amd64 member-set pin + 7 storeless-branch probes | 112 L |
| `daemon/internal/app/app.go` | field-type edits (`options.store`, `daemon.store`) | 2 lines |
| `daemon/internal/app/{lease.go,linux_owner_recovery_linux.go,linux_owner_v2_linux.go,linux_owner_witness_linux.go,pi_provider_bridge.go}` | same-value retype of the declarations that carry the field | 18 lines |
| `daemon/internal/app/cleanup_glue.go` | lazy store fallback in `selectStoreWrite` + its 8 call sites (§6) | 38 lines changed |
| `daemon/internal/app/export_test.go` | `withStore(nil)` writes an untyped nil (§7) | 8 lines |
| `daemon/internal/app/lease_stale_persistence_test.go` | `expireLeaseBeforeCleanup` parameter takes the seam type (it receives `app.store`) | 1 line |
| `daemon/internal/state/goal_delivery.go` | one-line alias for the late-usage ledger (§5) | 4 lines |
| `docs/TODO.md` | the PR #234 review-round-2 test-debt entry (conductor amendment) | 8 lines |
| `docs/plans/2026-11-22-rf-w4-12a-journal-store-plan.md` | this slice's plan | 190 L |

Loc delta on tracked files: the 11 files this slice modified are **+72 / −34**
(`git diff --stat adfe7d6a..99acb371` over those paths; the commit's 18-file total adds the five new
`journal_store*.go` files and this record's plan/evidence files). The five new `journal_store*.go`
files = **703 L** including tests — the sizes in the table above, re-derived with `wc -l` on the
fix-round tree (the pre-apply tree measured 594 L; the apply stage removed the redundant
drop-status counter and the inert `app.go:189` probe and shared the two probe runners; the fix round
added the shared valid-authority fixture and the `persistLinuxV2Mutation` probe). D2 (LOC) is
report-only.

Revision identity of the delivered bytes: slice commit `f9735123` over the merge `8af47444` (its
pre-rewrite twin is `99acb371`), merged with `origin/main` as tree `e49d374a`; the fix-round edits
are uncommitted over `e49d374a`. Five files (`sha256sum`, re-derived on the fix-round tree):
`journal_store.go` `306a7a13760caf86c2d617acbbe558816bb01ac09e202387ee5cf81770c872f9`,
`journal_store_linux_amd64.go` `2bc8575db4b87ed083e84f7738785457dee0a0f91aafd60e4969d2e1c2e49f1e`,
`journal_store_other_targets.go` `d4a6a7ea421ded4c5953b77c68875e05afcc6e9cc00cf63480c450fb7a18be37`,
`journal_store_test.go` `95218ca3763cf2b15ab885c879dc838fc7563906981bf88ef1a4f3ce912e6734`,
`journal_store_linux_test.go` `83c9900f1cb2d866af9391315e4e42aed0a68b0ea77fcd7fb10675ea2e246ba5`.

No production wrapper, adapter or forwarding type: the only `journalStore` implementer assertion in
the tree is `daemon/internal/app/journal_store.go:115` (`var _ journalStore = (*state.Store)(nil)`).

The apply stage (2026-11-22) changed no production executable line: the three `journal_store*.go`
seam files differ from `99acb371` by comments only (the four seam comments became the two-line
`journalStoreBase` why — comment policy 2026-11-22 §1/§4, the target split is recorded in §3), and
the test changes are the dropped redundant `calls` counter in the drop-status probe (§11 M5), the
dropped inert `app.go:189` probe (§8), and the two shared `assertMethodSet`/`storelessProbe`
runners that replaced the duplicated pins and probe loops.

The fix round (2026-11-22, review findings STD-A01..A03 and SPEC-A01..A03) changed no production
executable line either: the four zero-valued storeless probes now build valid sibling authorities
from the shared `journalStoreValidCloneEvidence` fixture, seven `cleanup_glue.go` probe names are
symbol-anchored (their base line numbers had drifted), and one linux probe pins
`persistLinuxV2Mutation`'s parameter guard (§11 M6–M10); the plan and this record carry the
documentation corrections.

## 2. Method-set census (the gate table)

Command (one statement, throwaway tool: `go list -export -deps -json ./internal/app` + a `go/types`
walk that records every selector whose receiver type is `*state.Store`, run per GOOS/GOARCH and
unioned; the tool lived in `daemon/_census` and is deleted before the gates):

```
go run ./_census windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
```

Measured on the **base** revision (the interface cannot change which methods the base code selects):

| Target | Production methods selected | Test-only additions | Declared `journalStore` |
| --- | --- | --- | --- |
| windows/amd64, windows/arm64, darwin/amd64, darwin/arm64, linux/arm64 | 97 | 2 | 99 (`journalStoreBase`, aliased) |
| linux/amd64 | 121 | 2 | 123 (`journalStoreBase` + 24) |

`comm` check: `comm -23 <windows set> <linux amd64 set>` = **0 rows** → the linux/amd64 census is
the complete production set, so its call-site list below covers every target.

Two methods are selected only by app **tests** through the seam (found by the Windows test compile;
they are interface members so "existing app tests run unchanged" holds):

| Method | Test call sites | Target |
| --- | --- | --- |
| `SaveJournal` | `decision_requirement_test.go:37,73,170`, `retention_failure_test.go:84` | all targets |
| `QueueTerminalTransition` | `supervisory_test.go:234` | all targets |

Full production table (121 rows): method ↔ `*state.Store` method (same name) ↔ first caller ↔
targets. Call-site counts are from the linux/amd64 census.

| Method | Calls | First caller | Targets |
| --- | --- | --- | --- |
| `AbortGoalSessionLaunchBeforeNativeStart` | 2 | `goal_admission.go:323` | all targets |
| `AbortLinuxCgroupReservation` | 1 | `linux_owner_v2_linux.go:580` | linux && amd64 |
| `AbortLinuxCloneReuseBeforeNativeStart` | 1 | `native_run.go:235` | all targets |
| `AbortNeverCreatedLinuxCgroupReservation` | 1 | `linux_owner_recovery_linux.go:675` | linux && amd64 |
| `AdvanceLeaseExpiry` | 2 | `app.go:1203` | all targets |
| `ArmLinuxCgroupLauncherGrant` | 1 | `linux_owner_v2_linux.go:459` | linux && amd64 |
| `BeginLinuxCloneCleanup` | 1 | `cleanup.go:623` | all targets |
| `BindLinuxCgroupGate` | 1 | `linux_owner_v2_linux.go:501` | linux && amd64 |
| `BindLinuxCgroupHelper` | 1 | `linux_owner_v2_linux.go:435` | linux && amd64 |
| `BindLinuxCgroupLauncher` | 1 | `linux_owner_v2_linux.go:446` | linux && amd64 |
| `BindLinuxCgroupLeaf` | 1 | `linux_owner_v2_linux.go:424` | linux && amd64 |
| `BindLinuxCloneReuse` | 1 | `native_run.go:221` | all targets |
| `BindLinuxCloneSeed` | 1 | `linux_owner_v2_linux.go:659` | linux && amd64 |
| `BindSupervisorHandoff` | 1 | `cleanup_glue.go:337` | all targets |
| `BindSupervisorHandoffV2` | 1 | `linux_owner_v2_linux.go:522` | linux && amd64 |
| `CheckLinuxV2NativeAdmission` | 1 | `native_run.go:212` | all targets |
| `CheckLinuxV2NativeAdmissionBeforeClaim` | 1 | `native_run.go:206` | all targets |
| `ClearContainmentUnproven` | 1 | `cleanup_glue.go:125` | all targets |
| `ClearLinuxCgroupReservationV2` | 1 | `linux_owner_v2_linux.go:620` | linux && amd64 |
| `ClearLinuxCloneNoTargetRetention` | 1 | `linux_owner_v2_linux.go:1527` | linux && amd64 |
| `ClearNativeUsageRecoveryRequired` | 4 | `native_run.go:1788` | all targets |
| `ClearProcessDetails` | 1 | `cleanup_glue.go:415` | all targets |
| `ClearProcessDetailsV2` | 1 | `linux_owner_v2_linux.go:605` | linux && amd64 |
| `ClearSupervisorHandoff` | 1 | `cleanup_glue.go:373` | all targets |
| `ClearSupervisorHandoffAfterAbort` | 1 | `cleanup_glue.go:385` | all targets |
| `ClearSupervisorHandoffV2` | 1 | `linux_owner_v2_linux.go:592` | linux && amd64 |
| `Close` | 8 | `app.go:709` | all targets |
| `CloseGoalSession` | 7 | `goal_admission.go:609` | all targets |
| `CommitSupervisorHandoff` | 2 | `cleanup_glue.go:349` | all targets |
| `CompleteControlCommand` | 2 | `supervisory.go:91` | all targets |
| `CompleteLinuxCloneAbort` | 2 | `linux_owner_v2_linux.go:1486` | linux && amd64 |
| `CompleteLinuxCloneCleanup` | 2 | `cleanup.go:718` | all targets |
| `CompleteProvideInput` | 1 | `commands.go:802` | all targets |
| `CompleteProviderAction` | 1 | `pi_provider_bridge.go:96` | all targets |
| `DeleteEnrollmentIntent` | 2 | `app.go:574` | all targets |
| `DeleteJournal` | 1 | `cleanup.go:940` | all targets |
| `DiscardUnreadyGoalSessionAttachDelivery` | 3 | `goal_admission_tx.go:1063` | all targets |
| `EnterCleanupPending` | 1 | `cleanup.go:886` | all targets |
| `FenceLinuxCloneBeforeCheckout` | 1 | `linux_owner_v2_linux.go:1420` | linux && amd64 |
| `ListGoalSessions` | 6 | `cleanup.go:986` | all targets |
| `ListJournals` | 11 | `app.go:1142` | all targets |
| `ListLateGoalUsage` | 1 | `goal_delivery.go:778` | all targets |
| `LoadEnrollmentIntent` | 1 | `app.go:668` | all targets |
| `LoadGoalSession` | 16 | `goal_admission.go:392` | all targets |
| `LoadGoalSessionByControlSessionID` | 2 | `goal_admission.go:124` | all targets |
| `LoadGoalSessionByHandle` | 5 | `goal_delivery.go:102` | all targets |
| `LoadIdentity` | 1 | `app.go:532` | all targets |
| `LoadJournal` | 133 | `app.go:888` | all targets |
| `MarkCommandAcknowledgementsDelivered` | 1 | `outbox.go:1128` | all targets |
| `MarkContainmentUnproven` | 1 | `cleanup_glue.go:113` | all targets |
| `MarkEventsDelivered` | 1 | `outbox.go:1030` | all targets |
| `MarkEventsDeliveredAndQueueOutputTruncatedMarker` | 1 | `outbox.go:832` | all targets |
| `MarkGoalDeliveryDelivered` | 1 | `goal_delivery.go:686` | all targets |
| `MarkGoalSessionAttachDeliveryReady` | 1 | `goal_delivery.go:32` | all targets |
| `MarkGoalSessionAvailable` | 1 | `goal_delivery.go:682` | all targets |
| `MarkGoalSessionLaunchStarted` | 1 | `goal_admission_tx.go:571` | all targets |
| `MarkGoalSessionResumeNativeStopped` | 4 | `goal_admission.go:408` | all targets |
| `MarkGoalSessionResumeRejected` | 1 | `goal_admission_tx.go:1052` | all targets |
| `MarkGoalSessionResumeStartAttempted` | 1 | `goal_admission_tx.go:852` | all targets |
| `MarkGoalSessionStoppedPending` | 2 | `goal_delivery.go:201` | all targets |
| `MarkGoalSessionUncertain` | 8 | `goal_admission.go:345` | all targets |
| `MarkLateGoalUsageDelivered` | 1 | `goal_delivery.go:808` | all targets |
| `MarkLinuxCgroupCreateStarted` | 1 | `linux_owner_v2_linux.go:407` | linux && amd64 |
| `MarkNativeUsageRecoveryRequired` | 1 | `native_run.go:1023` | all targets |
| `MarkNativeUsageRecoverySettlement` | 3 | `native_run.go:1083` | all targets |
| `MarkTransitionAttempted` | 1 | `outbox.go:693` | all targets |
| `MarkTransitionsDelivered` | 2 | `outbox.go:736` | all targets |
| `MarkWorkspaceRecoveryRequired` | 3 | `app.go:1458` | all targets |
| `PersistGoalSessionControlAttachment` | 1 | `goal_delivery.go:651` | all targets |
| `PersistGoalSessionHandle` | 1 | `goal_admission_tx.go:1267` | all targets |
| `PrepareControlCommand` | 1 | `supervisory.go:57` | all targets |
| `PrepareProvideInput` | 1 | `commands.go:673` | all targets |
| `PrepareProviderAction` | 1 | `pi_provider_bridge.go:62` | all targets |
| `PrepareSupervisorHandoff` | 2 | `cleanup_glue.go:325` | all targets |
| `QueueCommandAcknowledgement` | 1 | `commands.go:763` | all targets |
| `QueueGoalEvidenceAndTerminalTransition` | 1 | `deterministic_validation.go:424` | all targets |
| `QueueGoalSessionAttach` | 2 | `goal_admission_tx.go:499` | all targets |
| `QueueGoalSessionStopped` | 3 | `goal_delivery.go:191` | all targets |
| `QueueGoalUsage` | 2 | `native_run.go:1042` | all targets |
| `QueueNativeUsageRecovery` | 1 | `native_run.go:1032` | all targets |
| `QueueNativeUsageRecoverySettlement` | 3 | `native_run.go:1089` | all targets |
| `QueueNextEvent` | 1 | `events.go:451` | all targets |
| `QueueOutputEvent` | 1 | `events.go:419` | all targets |
| `QueueOutputTruncatedMarker` | 1 | `outbox.go:844` | all targets |
| `QueueRunningTransition` | 1 | `app.go:1671` | all targets |
| `QueueTerminalTransitionAndAcknowledgementAt` | 1 | `outbox.go:217` | all targets |
| `QueueTerminalTransitionAt` | 3 | `outbox.go:112` | all targets |
| `QueueTransition` | 1 | `events.go:518` | all targets |
| `QueueWaitingForInput` | 1 | `events.go:499` | all targets |
| `RebindGoalSessionForResume` | 1 | `goal_admission_tx.go:513` | all targets |
| `RecordContainmentStopReceipt` | 3 | `cleanup_glue.go:137` | all targets |
| `RecordLinuxCgroupHelperExit` | 1 | `linux_owner_v2_linux.go:490` | linux && amd64 |
| `RecordLinuxCgroupLauncherExit` | 1 | `linux_owner_v2_linux.go:476` | linux && amd64 |
| `RecordLinuxCgroupPreGateStopReceipt` | 1 | `linux_owner_v2_linux.go:562` | linux && amd64 |
| `RecordLinuxCloneCheckoutReady` | 1 | `linux_owner_v2_linux.go:669` | linux && amd64 |
| `RecordLinuxClonePromotion` | 1 | `linux_owner_v2_linux.go:697` | linux && amd64 |
| `RecordLinuxCloneTargetRemoval` | 1 | `cleanup.go:774` | all targets |
| `RecordNativeUsageObservation` | 1 | `native_run.go:745` | all targets |
| `RecordSupervisorHandoffStopReceipt` | 2 | `linux_owner_v2_linux.go:550` | all targets |
| `RecordSupervisorRecoveryScanSeal` | 2 | `recovery.go:622` | all targets |
| `RecoverProviderAction` | 3 | `pi_provider_bridge.go:70` | all targets |
| `RecoverProviderActions` | 1 | `pi_provider_bridge.go:189` | all targets |
| `ReserveLinuxCgroup` | 1 | `linux_owner_v2_linux.go:388` | linux && amd64 |
| `ReserveLinuxCloneIntent` | 1 | `linux_owner_v2_linux.go:643` | linux && amd64 |
| `ReserveLinuxClonePromotion` | 1 | `linux_owner_v2_linux.go:683` | linux && amd64 |
| `ResolveGoalSessionUncertainStopped` | 2 | `goal_delivery.go:152` | all targets |
| `ResolveTerminal` | 1 | `outbox.go:1054` | all targets |
| `ResolveTerminalForCleanup` | 2 | `outbox.go:603` | all targets |
| `RetainWorkspace` | 6 | `linux_owner_v2_linux.go:754` | all targets |
| `RetireGoalDelivery` | 2 | `goal_delivery.go:240` | all targets |
| `RetireLateGoalUsage` | 1 | `goal_delivery.go:797` | all targets |
| `SaveClaimGrant` | 2 | `app.go:1379` | all targets |
| `SaveClaimIntent` | 2 | `app.go:1355` | all targets |
| `SaveEnrollmentIntent` | 1 | `app.go:692` | all targets |
| `SaveGoalSessionLaunchIntent` | 1 | `goal_admission_tx.go:657` | all targets |
| `SaveIdentity` | 1 | `app.go:571` | all targets |
| `SetGoalSessionState` | 3 | `goal_delivery.go:176` | all targets |
| `SetLocalState` | 7 | `app.go:1679` | all targets |
| `SetProcessDetails` | 2 | `app.go:1636` | all targets |
| `SetProcessDetailsWithAuthority` | 1 | `recovery.go:678` | all targets |
| `SetWorkspacePath` | 1 | `app.go:2121` | all targets |

Member-set pins (executable): `TestJournalStoreBaseMethodSetIsExact` (99 names, every target) and
`TestJournalStoreMethodSetIsExactOnLinuxAMD64` (123 names = base ∪ 24) fail on any widening or
narrowing of the declared set, which the compile-time assertion cannot catch (an added method
`*state.Store` already has passes it). Mutant M1 in §11 exercises exactly that.

## 3. Split of the interface across build targets (derived, not in the contract text)

The declaration file the contract names is `journal_store.go`; the measured sets forced two more
files, both listed here:

- `*state.Store` has the 24 cgroup-v2/clone-abort methods **only on linux** (verified by the
  windows/darwin censuses and by the failing assertion when they are declared unconditionally), and
  only `linux && amd64` app files select them (`linux_owner_v2_linux.go`,
  `linux_owner_recovery_linux.go`, `linux_owner_witness_linux.go` are all `//go:build linux && amd64`).
- Hence `journalStore` (the field type) is the base set plus those methods on linux/amd64, and an
  alias of the base everywhere else. The base set is written once; the linux/amd64 file adds the 24.
- `journal_store_test.go` pins the base set on every target; `journal_store_linux_test.go` pins the
  composite.

Conductor amendment (2026-11-22, whole-increment review SPEC-A04): the two extra files named here
and `journal_store_linux_test.go` are ratified as in-scope minimal corrections.

## 4. Field and declaration retypes (re-derived list)

Field types: `daemon/internal/app/app.go` `options.store:189`, `daemon.store:301` → `journalStore`.
Same-value retypes of declarations that receive those fields (a concrete `*state.Store` still
satisfies them, so every existing caller compiles unchanged):

| Declaration | Line |
| --- | --- |
| `linuxCloneCleanupPersistMutation` function type (`lease.go`) | 43 |
| its linux assignment closure parameter (`linux_owner_recovery_linux.go`) | 29 |
| `persistLinuxV2Mutation` parameter (`linux_owner_v2_linux.go`) | 1828 |
| `newDurablePiProviderBridgeExecutor` parameter (`pi_provider_bridge.go`) | 49 |
| 16 witness parameters (`linux_owner_witness_linux.go`) | 1415, 1796, 1834, 2025, 2478, 2596, 2796, 2918, 2969, 3445, 3481, 3603, 3620, 3682, 3760, 3797 |
| `expireLeaseBeforeCleanup` test helper (`lease_stale_persistence_test.go`) | 206 |

Gate: `rg -n 'store +\*state\.Store' daemon/internal/app --glob '!*_test.go'` = **0** lines at HEAD
(21 at base for this pattern; 22 only under the broader `\*state\.Store` count, which adds the
unnamed parameter at `lease.go:43`).

## 5. D1 — the late-usage ledger is unnameable from package `app` (amendment)

`ListLateGoalUsage`, `MarkLateGoalUsageDelivered` and `RetireLateGoalUsage` are selected by
`daemon.store` in `goal_delivery.go:778/797/808` and return `state.lateGoalUsageLedger`, an
**unexported** type of another package: no app-local interface can declare those signatures, and
substituting an exported type breaks satisfaction. Options and the decision are recorded in the plan
(§D1). Implemented: the smallest amendment, one alias in the owning package —

```go
// LateGoalUsageLedger names lateGoalUsageLedger for the packages that must declare the Store
// methods returning it; the alias adds no runtime or persisted shape.
type LateGoalUsageLedger = lateGoalUsageLedger
```

`daemon/internal/state/goal_delivery.go:94-96`. It is the same type (compile-time alias): no wire
byte, persisted byte, JSON tag, error string or call order changes. Rejected alternatives (recorded
in the plan): a second concrete `*state.Store` field or a type assertion in `flushLateGoalUsage`
(duplicate store reference and an un-injectable hole in the seam), which would also fail the
`store *state.Store` gate above.

Conductor amendment (2026-11-22, SPEC-A04): the alias is ratified as the smallest in-scope
correction.

## 6. D5 — `selectStoreWrite` binds its fallback lazily (required adaptation)

At base, `selectStoreWrite(daemon, override, daemon.store.Method)` evaluated the store method value
*before* the guard. With `*state.Store` that is legal for a nil receiver, so the guard still refused
a storeless daemon with `errDaemonStoreClosing`. With a nil `journalStore` the same expression
panics ("method value of nil interface") before the guard runs — i.e. the guard would have changed
meaning for its 8 callers. Fixed by binding the fallback inside the guard:

| Condition (lock held) | Base | HEAD |
| --- | --- | --- |
| `storeClosing` | refuse `errDaemonStoreClosing` | refuse (same) |
| no store, no override | refuse | refuse (same, guard reached) |
| no store, override set | override runs, no store touched | override runs, no store touched (same) |
| store present, no override | store method bound eagerly, then called | store method bound in the guard, then called (same call, same receiver, same order) |

The 8 call sites (all in `cleanup_glue.go`; base line → HEAD line of the callback that owns it):
`markContainmentUnprovenCallback` 113 → 113, `clearContainmentUnprovenCallback` 125 → 127,
`persistContainmentStopReceiptCallback` 137 → 141, `prepareSupervisorHandoffCallback` 325 → 331,
`bindSupervisorHandoffCallback` 337 → 345, `commitSupervisorHandoffCallback` 349 → 359,
`clearSupervisorHandoffCallback` 373 → 385, `clearSupervisorHandoffAfterAbortCallback` 385 → 399.
Lock site, refusal predicate and error identity are unchanged; only the binding time moves inside the
existing read lock.

Conductor amendment (2026-11-22, SPEC-A04): this rewrite is ratified as the smallest in-scope
correction, superseding Change 3's "stay unchanged" for `selectStoreWrite` alone provided behaviour
stays byte-equivalent (§6 table) and the plan's D4 wording is reconciled (plan §D4).

## 7. D6 — the test injection seam writes an untyped nil

`withStore(store *state.Store)` assigned a possibly nil `*state.Store` to the interface field, which
would turn an absent store into a **typed** nil (`options.store != nil` would then be true and every
`store == nil` check would change meaning). It now assigns `nil` when its argument is nil
(`export_test.go:9-17`), and the probe `app.go:524 injected absent store stays untyped nil` pins it
(mutant M4).

## 8. Storeless-daemon branch audit (base vs HEAD)

`TestStorelessDaemonTakesEveryStoreNilBranch` (23 probes) and
`TestStorelessDaemonTakesEveryLinuxStoreNilBranch` (7 probes) drive a daemon built without a Store
(`&daemon{}`) through every `daemon.store == nil` / `options.store == nil` branch and pin the branch
outcome. Each probe asserts the *refusal or skip* of its branch, which a typed nil or a flipped
guard turns into a panic or a different result — that is what makes the probe load-bearing (§11).

The 23 probes are the 21 cross-target guard sites a probe drives directly, the injection-seam pin
(`app.go:524`, mutant M4 — it pins the typing that `app.go:523`'s `options.store != nil` injection
test depends on) and the override probe (`cleanup_glue.go selectStoreWrite override`); the tree's cross-target
store-absent branches are those 21 plus the injection branch. The apply stage dropped the
`app.go:189` probe (a read of two zero-valued fields on a freshly built `&daemon{}`): `app.go:189`
is the field declaration, not a branch, and the assertion could not fire. The base-copy runs below
used the pre-drop set (30 probes, with 24 cross-target ones); the dropped probe was inert on both
trees, so the base-vs-HEAD comparison is unchanged.

The fix round (2026-11-22) raised the linux set from 6 to 7 by adding `linux_owner_v2_linux.go:1837`
(`persistLinuxV2Mutation`'s parameter guard; §11 M10) and corrected four probes whose zero-valued
sibling inputs made the sibling predicate fire the identical error string before their
`daemon.store == nil` term: `persistLinuxCloneTargetRemoval` and `loadLinuxCloneRunJournal` now take a
valid grant/intent/proof, `linuxCloneAbortHasRetentionOwner` a non-nil `LinuxCloneIntent` with
`LinuxCloneAbortPending`, and `recoverLinuxV2NativeWitnessRunKey` a non-zero `RunJournal` key. Each
store term is now the only refusing predicate, and deleting it reddens the probe (§11 M6–M9). The
added `:1837` probe has no base-copy run: its base twin is the byte-identical guard
`if daemon.storeClosing || store == nil || store != daemon.store` at `adfe7d6a` with `*state.Store`
(verified with `git show adfe7d6a:…/linux_owner_v2_linux.go`), so a base-copy run takes the same
`errDaemonStoreClosing` refusal.

Probe sites: base-revision line references, except the `cleanup_glue.go` probes, which are
symbol-anchored because D5 shifted that file's lines: `app.go:524`, `app.go:708`, `app.go:801`,
`cleanup.go:545`, `cleanup.go:769`, `cleanup_glue.go selectStoreWrite` (guard),
`cleanup_glue.go selectStoreWrite override` (override), `cleanup_glue.go
persistProcessAuthorityCallback`, `cleanup_glue.go persistProcessMarker`, `cleanup_glue.go
persistProcessMarker mirror error`, `cleanup_glue.go verifyDurableContainmentStopReceipt`,
`cleanup_glue.go clearPersistedProcessDetails`, `deterministic_validation.go:90`, `native_run.go:217`,
`native_run.go:227`, `native_run.go:246`, `native_run.go:428`, `pi_provider_bridge.go:50`,
`pi_provider_bridge.go:180`, `recovery.go:619`, `recovery.go:635`, `recovery.go:705`,
`recovery_glue.go:33`, and on linux/amd64 `linux_owner_v2_linux.go:1180`, `:1209`, `:1549`, `:1837`,
`linux_owner_witness_linux.go:1416`, `:2479`, `:2599`.

Control-flow preservation was measured, not assumed: the apply-stage probe files were run against the
base revision (a throwaway copy of `adfe7d6a` with a two-line shim mapping `journalStore` to
`*state.Store`, since the interface does not exist at base):

| Run | Command | Result |
| --- | --- | --- |
| base, windows probes | `go test -count=1 -run TestStorelessDaemon ./internal/app/` in the base copy | `ok … 0.271s` |
| base, linux/amd64 probes | same, under WSL `Ubuntu-24.04` | `ok … 0.020s` |
| fix round, windows probes | `go test -count=1 -run 'TestJournalStore\|TestStorelessDaemon' -v ./internal/app/` | `ok … 0.294s`, exit 0; 23 subtests pass (row 15 of §10) |
| fix round, linux/amd64 probes | WSL: `go test -count=1 -run 'TestJournalStore\|TestStorelessDaemon' -v ./internal/app/` | `ok … 0.116s`, exit 0 (row 19 of §10); the run prints `--- PASS: TestJournalStoreMethodSetIsExactOnLinuxAMD64` (123 members), `--- PASS: TestJournalStoreBaseMethodSetIsExact`, `--- PASS: TestStorelessDaemonTakesEveryLinuxStoreNilBranch` with all 7 subtests passing, and the 23 untagged probes |

Branches **not** probed, with the reason: the 10 unguarded mirror fallbacks
(`cleanup_glue.go:433`, `goal_admission.go:323/345/601/609`, `goal_delivery.go:32/58`,
`native_run.go:729/1042`, `supervisory.go:232`; `recovery_glue.go:36` is guarded at :33) accept a
store call on a nil receiver at base too (the nil `*state.Store` method call panics inside `state`),
so they are not `store == nil` branches and have no base outcome to preserve; the storeless
behaviour of those paths is a panic at base and at HEAD.

## 9. Preserved semantics

- Same `*state.Store` method per call, same arguments, same order: every call site keeps its
  expression; only static types changed. The two comparisons `store != daemon.store`
  (`linux_owner_v2_linux.go:1837`) compare the same `*state.Store` pointer before and after.
- `storeLifetimeMu` / `storeClosing`: unchanged lock sites; `selectStoreWrite` still takes the read
  lock first and returns the release; the refusal predicate is equivalent
  (`storeClosing || (override == nil && store == nil)` ≡ base's `!(!storeClosing && (override != nil || store != nil))`).
- `close()` keeps `options.store == nil && daemon.store != nil` as its ownership test, so an
  injected store is still never closed by the daemon.
- The 25 mirrors, `storeCallbackWrite` and every mirror read are unchanged (W4-12b deletes them).
- No production byte, error string, log line, SQL or persisted shape changed; the only production
  edits are type spellings plus the `selectStoreWrite` binding move, both behaviour-identical per
  the table in §6.

Assertions in pre-existing tests: none changed, none deleted, no pre-existing `t.Run` name touched
(the slice's diffs to the two pre-existing test files are `export_test.go` +9/−1 and
`lease_stale_persistence_test.go` 1 line: a helper parameter type and the `withStore` nil guard).
The fix round renames seven of the slice's own probe subtests (STD-A02) and adds one linux probe
(STD-A03); no CI-pinned name is involved. The app suite's CI-pinned name
`TestOpenCodeLegacyAssignmentFailsBeforeRunningWorkspaceAndProcess` still resolves
(`harness_test.go:783`, file untouched), and the CI `--assert` rows that name it are unchanged.

## 10. Gates

Run foreground on the host (Windows) unless the row says WSL; `cd daemon` for the Go commands.
Exit codes are the command's own (captured without a pipe where the row reports one).

| # | Command | Result |
| --- | --- | --- |
| 1 | `gofmt -l daemon/internal/app` | no output, exit 0 |
| 2 | `git diff --check` | no output, exit 0 |
| 3 | `rg -n 'store +\*state\.Store' daemon/internal/app --glob '!*_test.go'` | 0 rows (21 at base for this pattern; 22 under the broader `\*state\.Store` count, which adds `lease.go:43`) |
| 4 | `rg -n 'var _ journalStore' daemon/internal/app/*.go` | 1 row: `journal_store.go:115` with `(*state.Store)(nil)` |
| 5 | `go build ./...` (windows/amd64) | exit 0 |
| 6 | `GOOS=windows GOARCH=arm64 go build ./...` | exit 0 |
| 7 | `GOOS=linux GOARCH=amd64 go build ./...` | exit 0 |
| 8 | `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 |
| 9 | `GOOS=darwin GOARCH=amd64 go build ./...` | exit 0 |
| 10 | `GOOS=darwin GOARCH=arm64 go build ./...` | exit 0 |
| 11 | `GOOS=linux GOARCH=amd64 go vet ./internal/app/` | exit 0 (the `linux && amd64` test files compile) |
| 12 | `go vet ./...` | exit 0 |
| 13 | `go test -count=1 ./internal/app/...` (Windows) | `ok … 491.915s`, exit 0 (harness exit code captured without a pipe) |
| 14 | `go test -count=1 -coverprofile=… ./internal/app/...` (Windows) | `ok … 554.041s`, coverage 75.3% of statements, exit 0 |
| 15 | `go test -count=1 -run 'TestJournalStore\|TestStorelessDaemon' -v ./internal/app/` | exit 0; `ok … 0.294s`, 23 probe subtests + 1 member-set test pass |
| 16 | `python3 scripts/ci/test-omo-control-native-evidence.py` (R9) | `Ran 15 tests in 36.015s … OK`, exit 0 |
| 17 | WSL `Ubuntu-24.04` in this slice's worktree (`<worktree>/daemon`): the contract's `go test -count=1 -race ./internal/app/...`, run as `go test -race -c -o <scratch>/app.race.test ./internal/app/` then `cd daemon/internal/app && <scratch>/app.race.test -test.timeout=55m -test.count=1` (binary form keeps the 55-minute timeout inside the harness limit) | `PASS`, exit 0, wall 683 s; the race binary's `-test.list '.*'` is identical to `go test -list '.*' ./internal/app/` on the same tree (682 tests), so the full linux test set ran |
| 18 | `go test -count=1 ./internal/state/...` (the one touched package outside `app`) | `ok … 14.868s`, exit 0 |
| 19 | WSL (this slice's worktree, `<worktree>/daemon`): `go test -count=1 -run 'TestJournalStore\|TestStorelessDaemon' -v ./internal/app/` (linux/amd64 probes and both pins) | `ok … 0.116s`, exit 0; 23 cross-target subtests + all 7 linux subtests + both member-set pins pass |

Rows 1–12, 15, 16, 18 and 19 were re-run in the fix round (2026-11-22) on the fix-round tree; rows
13, 14 and 17 are the apply-stage runs of the full Windows suite, its coverage-instrumented twin and
the WSL `-race` suite (conductor-owned full suites; the fix round's edits are test-file and
document-only, with no production line changed, so those rows are not re-run). Rows 13/14 are
slower than the implement-stage measurement (491.9 s vs 141.4 s for the same set); row 17 came in
faster than the recorded 2349.759 s because the binary form takes the `go test` build step over the
Windows-drive mount out of the timed window (build vs run and host load are not separated here). Row 13
is the contract's exact command, captured without a pipe so its exit code is the command's own; row
14 is the identical test set, coverage instrumented. Rows 15 and 19 are the fix round's scoped runs
of the changed test set on both platforms and back §11's fix-round mutant runs.

Row 17 is the contract's slow oracle in this environment, and it does not fit the harness defaults:

| Attempt | Command | Result |
| --- | --- | --- |
| 1 | `-race` (10-minute Go default) | `panic: test timed out after 10m0s`, `FAIL … 600.101s`, goroutine dump, exit non-zero |
| 2 | `-race -timeout=30m` | `panic: test timed out after 30m0s` with `running tests: TestRawOutputBudgetDropsWithoutFailingSemanticEvents (3m21s)`, `FAIL … 1800.187s`, exit non-zero (`tail` in the same pipeline reported its own status) |
| 3 | `-race -timeout=55m` | **`ok … 2349.759s`, exit 0** (the recorded run; the same suite needs 726.951s in the conductor's W4-01 measurement, so this worktree is ~3× slower under `-race`) |

Attempt 2's dump attributes the wall time to a pre-existing slow path, not to this slice: the running
test sat in `protocol.scanJSONValue` (`protocol/goal_evidence_batch.go:68/93`) via
`protocol.RejectDuplicateJSONMembers` ← `state.validRawMessage` (`state.go:6442`) ←
`state.(*Store).QueueOutputEvent` ← `app.queueRawEvent` (`events.go:419`) ←
`TestRawOutputBudgetDropsWithoutFailingSemanticEvents` (`app_test.go:4647`), scanning a 43 KB
payload under `-race`. None of `protocol/**`, `state/state.go`, `app/events.go` or `app_test.go` is
in this slice's diff, and the same test is the recorded slowest app test at **125.79 s** without
race (`docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md:310`). Both Windows full-suite
runs of the same test set were green on the delivered bytes (rows 13, 14).

## 11. Red mutants

Mutants are applied with `go test -overlay=<mutant>.json` against the delivered tree (mutant sources
in a scratch directory; no repository file is edited). Command for all of them (the mutant sources
were regenerated from the delivered bytes after the apply-stage edits; the fix round re-derived M2/M3
after the STD-A02 renames and added M6–M10 from the fix-round bytes, redirecting the whole output to
a log so the killing subtest and the captured status are the whole evidence):

```
go test -overlay=<mutant>.json -count=1 -run '<killing test>' ./internal/app/ 2>&1 | tail -1
```

| Mutant | Production change | Killing test | Observed (final bytes) |
| --- | --- | --- | --- |
| M1 (widening) | `journalStoreBase` gains `LoadLateGoalUsage(key state.RunKey) (state.LateGoalUsageLedger, error)`, a method `*state.Store` has and app never selects | `TestJournalStoreBaseMethodSetIsExact` | `--- FAIL: TestJournalStoreBaseMethodSetIsExact`, `added [LoadLateGoalUsage]`, `removed []`, `FAIL … 0.710s`, exit 1 (reported at the shared `assertMethodSet`, `journal_store_test.go:126`) |
| M2 (nil guard) | `verifyDurableContainmentStopReceipt`'s `if daemon.store == nil` → `!= nil` | `…/cleanup_glue.go_verifyDurableContainmentStopReceipt` | `--- FAIL: …/cleanup_glue.go_verifyDurableContainmentStopReceipt` then `panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]`, `FAIL … 0.392s`, exit 1 |
| M3 (D5 reverted) | `selectStoreWrite` binds `fromStore(daemon.store)` before the guard, as base did | `…/cleanup_glue.go_selectStoreWrite_refuses` | `--- FAIL: …/cleanup_glue.go_selectStoreWrite_refuses` plus the same panic, `FAIL … 0.306s`, exit 1 |
| M4 (typed nil) | `withStore` assigns its `*state.Store` argument directly into the interface field | `…/app.go:524_injected_absent_store_stays_untyped_nil` | `journal_store_test.go:158: injected absent store = (*state.Store)(nil), want an untyped nil`, `FAIL … 0.638s`, exit 1 |
| M5 (apply-stage redundancy proof) | `captureHelperDropStatus` guard drops `store == nil`, so the drop-status reader is consulted with a nil store | `…/linux_owner_witness_linux.go:2479_captureHelperDropStatus` (WSL) | `storeless drop-status capture = storeless probe must not consult the drop-status reader, want nil`, exit 1 — the pruned probe still reddens through its retained error tripwire |
| M6 (STD-A01, cross-target) | `persistLinuxCloneTargetRemoval` guard drops `daemon.store == nil` | `…/cleanup.go:769_persistLinuxCloneTargetRemoval` (Windows) | `--- FAIL: …/cleanup.go:769_…`, `journal_store_test.go:178: error = persist exact B target-removal receipt: Linux clone cleanup persistence is supported only by the Linux v2 App, want "Linux clone target-removal proof is incomplete or differs from its cleanup intent"`, `FAIL … 0.376s`, exit 1 |
| M7 (STD-A01) | `loadLinuxCloneRunJournal` guard drops `daemon.store == nil` | `…/linux_owner_v2_linux.go:1180_loadLinuxCloneRunJournal` (WSL) | `--- FAIL: …:1180_…` then `panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]`, `FAIL … 0.025s`, exit 1 |
| M8 (STD-A01) | `linuxCloneAbortHasRetentionOwner` guard drops `daemon.store == nil` | `…/linux_owner_v2_linux.go:1549_linuxCloneAbortHasRetentionOwner` (WSL) | `--- FAIL: …:1549_…` then the same panic, `FAIL … 0.026s`, exit 1 |
| M9 (STD-A01) | `recoverLinuxV2NativeWitnessRunKey` guard drops `store == nil` | `…/linux_owner_witness_linux.go:2599_recoverLinuxV2NativeWitnessRunKey` (WSL) | `--- FAIL: …:2599_…`, `journal_store_linux_test.go:100: error = <nil>, want "exact owner-witness recovery requires its live context, locked Store, and selected RunJournal"`, `FAIL … 0.026s`, exit 1 |
| M10 (STD-A03) | `persistLinuxV2Mutation` guard drops `store == nil` | `…/linux_owner_v2_linux.go:1837_persistLinuxV2Mutation_refuses_a_missing_Store` (WSL) | `--- FAIL: …:1837_…` then a panic inside `persistLinuxV2Mutation` (nil store read), `FAIL … 0.024s`, exit 1 |

Each mutant kills exactly one test and leaves the others green (M2/M3 fail at the subtest that owns
their branch; M1 only the member-set pin), so every new test is load-bearing and no test is a
tautology over the others. M5 is the apply stage's evidence for dropping the `calls` counter from
the drop-status probe: the counter could only fire after that assertion had already failed, and the
retained error tripwire alone kills the reader-consulting mutant.

M1/M4/M5 are the apply-stage runs; their production changes and killing test names are unchanged by
the fix round, so they are not re-run. M6–M10 are the fix round's sensitivity proof for the four
corrected probes and the added `:1837` probe: each deletion of a probe's store term fails exactly
that probe (M7/M8/M10 panic inside the store call the guard was refusing, M6 takes the platform
stub's refusal path, M9 returns nil instead of the refusal), and M2/M3 are the re-runs after the
STD-A02 renames.

## 12. R2 coverage of touched files

Both runs are the whole app suite with `-coverprofile` on the same test set: HEAD tree and a
throwaway copy of `adfe7d6a` (with the two member-set pins neutralised, since the interface does not
exist at base; the probes ran unchanged in both). Percentages are the unweighted mean of the
per-function figures `go tool cover -func` prints (the tree's function count is in parentheses); the
package total is `go test`'s statement coverage. The HEAD column is re-measured on the delivered
tree (row 14 of §10); the base column is the implement-stage base-copy run.

| File | Base | HEAD |
| --- | --- | --- |
| `app.go` | 74.60% (67 funcs) | 74.60% (67 funcs) |
| `cleanup_glue.go` | 58.15% (22 funcs) | **62.64%** (22 funcs) |
| `goal_delivery.go` (app) | 85.48% (28 funcs) | 85.61% (28 funcs) |
| `lease.go` | 79.36% (22 funcs) | 79.36% (22 funcs) |
| `native_run.go` | 77.00% (78 funcs) | 77.00% (78 funcs) |
| `recovery.go` | 78.47% (13 funcs) | 78.47% (13 funcs) |
| package total | 75.4% of statements | 75.3% of statements |

No touched file lost coverage; `cleanup_glue.go` gained 4.5 pp because the lazy fallbacks execute
under the existing tests, and `goal_delivery.go` re-measures 0.13 pp higher than the recorded value
(neither file is touched by the apply stage). `journal_store*.go` declares no statements (types and
one interface assertion), and the `state` alias adds none, so neither can move a line-coverage
number. The fix round's edits are test-file and document-only, so the per-file figures above still
bind the fix-round tree.

## 13. R9 / `SOURCE_PATHS`

No code moved out of a `SOURCE_PATHS` file, and `journal_store.go` received no declaration from one:
`app.go` keeps both field declarations (only their type spelling changes) and stays pinned, and the
new files declare new code. Per the contract's condition ("add … if it receives any declaration from
a listed path") `SOURCE_PATHS` is therefore **unchanged**. Alternative reading recorded in the plan
(§D3): pin `journal_store.go` as well, because it projects the method set of
`daemon/internal/state/state.go` (itself pinned). Not applied — it widens the native-evidence freeze
for every future app edit and is the conductor's call.

Gate: `python3 scripts/ci/test-omo-control-native-evidence.py` → `Ran 15 tests in 36.015s … OK`,
exit 0 (row 16 of §10; the fix-round re-run).

## 14. Known limits

- `persistLinuxV2Mutation`'s parameter guard (`linux_owner_v2_linux.go:1837`,
  `daemon.storeClosing || store == nil || store != daemon.store`) is probed since the fix round
  (§8/§11 M10): a storeless daemon with otherwise valid inputs is refused with `errDaemonStoreClosing`,
  and deleting the `store == nil` term panics on the store read. The interface comparison
  `store != daemon.store` diverges from base's pointer check when both operands hold the same **typed**
  nil: the comparison passes where base's `store == nil` refused, so the call would proceed into
  `state` and panic there. Unreachable today — no assignment path writes a typed nil into either field (`withStore`
  guards, every other assignment writes an untyped nil) — so the divergence is latent, not a
  behaviour change; recorded here rather than tested.
- The `selectStoreWrite` fallback shape change (D5) is required by the typed-nil clause but is the
  one edit outside "field-type edits"; W4-12b deletes the function, so its lifetime is one slice.
  Ratified as an in-scope correction (conductor amendment 2026-11-22, SPEC-A04).
- The one-line `state` alias (D1) is outside the slice's owned set; recorded as the smallest
  amendment, with the alternative rejected for duplicating the store reference. Ratified as an
  in-scope correction (conductor amendment 2026-11-22, SPEC-A04).
- linux/arm64 uses the base set (97 methods; the callers of the 24 are `linux && amd64`), so the
  composite interface exists only on linux/amd64.
- Latent divergence left in place, by analysis rather than probe (none of these sites is a
  `store == nil` branch, so they have no branch outcome to preserve): the 10 unguarded mirror
  fallbacks (`cleanup_glue.go:433`, `goal_admission.go:323/345/601/609`, `goal_delivery.go:32/58`,
  `native_run.go:729/1042`, `supervisory.go:232`; `recovery_glue.go:36` is guarded at :33) take a
  store method value that a nil store then calls. `daemon/internal/state` has no
  `if store == nil` guard (`rg -n "if store == nil" daemon/internal/state/*.go` = 0 rows), so at
  base the nil receiver is dereferenced and panics; at HEAD the method value itself panics before
  the call. Both fail with a runtime panic on a nil store; no store method, argument or order is
  involved in either.
- Environment limit (not this slice's): the WSL `-race` app suite passes. The implement-stage
  `go test` form needed 2349.759s with `-timeout=55m` (so it exceeds the Go default of 10 min and the
  harness default); the apply-stage binary form measured the suite itself at 683 s wall (§10 row 17)
  — the difference is the `go test` build step over the Windows-drive mount plus host load, not separated
  here. The slowest single test is `TestRawOutputBudgetDropsWithoutFailingSemanticEvents` through
  the pre-existing `protocol.scanJSONValue` path (§10's attempt-2 dump; 125.79 s without race in
  `docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md:310`). Anyone re-running the
  contract's WSL command verbatim must raise `-timeout` or the default fires first; the Windows runs
  of the identical test set are green without it.
