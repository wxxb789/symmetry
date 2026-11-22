# rf-w4-12a `journalStore` — evidence record (2026-11-22)

Slice: W4-12a of `goal-rf-w4-12-go-test-doubles.md` (arch-decision-2 R1, R0; verification
amendments; comment policy 2026-11-22; PR #234 conductor amendment). Plan:
`docs/plans/2026-11-22-rf-w4-12a-journal-store-plan.md`.
Base revision of the working tree: `adfe7d6a` (`refactor(app): split startGoalAdmission into
phases (#234)`), the branch HEAD; the slice is uncommitted for the verification stage.

Dispatch gates re-checked at this revision (`git merge-base --is-ancestor`): W4-01 `adfe7d6a` yes,
W3-08 `6d428d8f` (`refactor(state): unexport the test-only state surface (#214)`) yes, W3-15
`de526861` (`test: split the giant test units (#217)`) yes.

## 1. Deliverable shape

| File | Kind | Size |
| --- | --- | --- |
| `daemon/internal/app/journal_store.go` | new: `journalStoreBase` (99 methods) + `var _ journalStore = (*state.Store)(nil)` | 116 L |
| `daemon/internal/app/journal_store_linux_amd64.go` | new: `journalStore` = base + 24 linux/amd64-only methods | 40 L |
| `daemon/internal/app/journal_store_other_targets.go` | new: `journalStore` alias on every other target | 7 L |
| `daemon/internal/app/journal_store_test.go` | new: member-set pin + 24 storeless-branch probes | 319 L |
| `daemon/internal/app/journal_store_linux_test.go` | new: linux/amd64 member-set pin + 6 storeless-branch probes | 112 L |
| `daemon/internal/app/app.go` | field-type edits (`options.store`, `daemon.store`) | 2 lines |
| `daemon/internal/app/{lease.go,linux_owner_recovery_linux.go,linux_owner_v2_linux.go,linux_owner_witness_linux.go,pi_provider_bridge.go}` | same-value retype of the declarations that carry the field | 18 lines |
| `daemon/internal/app/cleanup_glue.go` | lazy store fallback in `selectStoreWrite` + its 8 call sites (§6) | 38 lines changed |
| `daemon/internal/app/export_test.go` | `withStore(nil)` writes an untyped nil (§7) | 8 lines |
| `daemon/internal/app/lease_stale_persistence_test.go` | `expireLeaseBeforeCleanup` parameter takes the seam type (it receives `app.store`) | 1 line |
| `daemon/internal/state/goal_delivery.go` | one-line alias for the late-usage ledger (§5) | 4 lines |
| `docs/TODO.md` | the PR #234 review-round-2 test-debt entry (conductor amendment) | 8 lines |
| `docs/plans/2026-11-22-rf-w4-12a-journal-store-plan.md` | this slice's plan | 187 L |

Loc delta on tracked files: `git diff --stat` = 11 files, **+72 / −34**; untracked additions
`journal_store*.go` = 542 L including tests. D2 (LOC) is report-only.

Revision identity of the delivered bytes: `git diff | sha256sum` =
`4962138dc2fd367130eabb2f770848c8e502167d703739c00164233163d64b33`; new files
(`sha256sum`): `journal_store.go` `f34cb9deaa627788890c267aea7ee4c1a55f9b29b1bd96a51c70c394c5407b76`,
`journal_store_linux_amd64.go` `909096b3164999236cc56bdb6b6fc87f11d6e81bc92ab1ae65e29f9412b1bd8c`,
`journal_store_other_targets.go` `c0e156c47d23191251219ab7adbb37c59482282008b72e9a8d752b041e5f6585`,
`journal_store_test.go` `70ec902d4568226d30bddd02b3f675c69c1cbb083c173babc446d6ae80f5585c`,
`journal_store_linux_test.go` `8e65bbbc897ad46dc111babc91903c90e61024124b28f043c07a5678f1b59de2`.

No production wrapper, adapter or forwarding type: the only `journalStore` implementer assertion in
the tree is `daemon/internal/app/journal_store.go:116` (`var _ journalStore = (*state.Store)(nil)`).

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
(22 at base).

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

## 7. D6 — the test injection seam writes an untyped nil

`withStore(store *state.Store)` assigned a possibly nil `*state.Store` to the interface field, which
would turn an absent store into a **typed** nil (`options.store != nil` would then be true and every
`store == nil` check would change meaning). It now assigns `nil` when its argument is nil
(`export_test.go:9-17`), and the probe `app.go:524 injected absent store stays untyped nil` pins it
(mutant M4).

## 8. Storeless-daemon branch audit (base vs HEAD)

`TestStorelessDaemonTakesEveryStoreNilBranch` (24 probes) and
`TestStorelessDaemonTakesEveryLinuxStoreNilBranch` (6 probes) drive a daemon built without a Store
(`&daemon{}`) through every `daemon.store == nil` / `options.store == nil` branch and pin the branch
outcome. Each probe asserts the *refusal or skip* of its branch, which a typed nil or a flipped
guard turns into a panic or a different result — that is what makes the probe load-bearing (§11).

Probe sites (source line at base): `app.go:189`, `app.go:524`, `app.go:708`, `app.go:801`,
`cleanup.go:545`, `cleanup.go:769`, `cleanup_glue.go:42` (guard), `cleanup_glue.go:44` (override),
`cleanup_glue.go:59`, `cleanup_glue.go:78`, `cleanup_glue.go:93`, `cleanup_glue.go:290`,
`cleanup_glue.go:396`, `deterministic_validation.go:90`, `native_run.go:217`, `native_run.go:227`,
`native_run.go:246`, `native_run.go:428`, `pi_provider_bridge.go:50`, `pi_provider_bridge.go:180`,
`recovery.go:619`, `recovery.go:635`, `recovery.go:705`, `recovery_glue.go:33`, and on linux/amd64
`linux_owner_v2_linux.go:1180`, `:1209`, `:1549`, `linux_owner_witness_linux.go:1416`, `:2479`,
`:2599`.

Control-flow preservation was measured, not assumed: the same probe files were run against the base
revision (a throwaway copy of `adfe7d6a` with a two-line shim mapping `journalStore` to
`*state.Store`, since the interface does not exist at base):

| Run | Command | Result |
| --- | --- | --- |
| base, windows probes | `go test -count=1 -run TestStorelessDaemon ./internal/app/` in the base copy | `ok … 0.271s` |
| base, linux/amd64 probes | same, under WSL `Ubuntu-24.04` | `ok … 0.020s` |
| HEAD, windows probes | `go test -count=1 -run TestStorelessDaemon -v ./internal/app/` | `ok … 0.299s`, 24 subtests pass |
| HEAD, linux/amd64 probes | WSL: `go test -count=1 -run 'TestStorelessDaemon\|TestJournalStore' ./internal/app/` | `ok … 0.180s`, exit 0 (row 19 of §10); the `-v` form of the same command prints `--- PASS: TestJournalStoreMethodSetIsExactOnLinuxAMD64` (123 members), `--- PASS: TestJournalStoreBaseMethodSetIsExact`, `--- PASS: TestStorelessDaemonTakesEveryLinuxStoreNilBranch` with all 6 subtests passing, and the 24 untagged probes |

Branches **not** probed, with the reason: the 10 unguarded mirror fallbacks
(`abortGoalSessionLaunchBeforeNativeStart` and siblings) and `cleanup_glue.go:415` accept a store
call on a nil receiver at base too (the nil `*state.Store` method call panics inside `state`), so
they are not `store == nil` branches and have no base outcome to preserve; the storeless behaviour
of those paths is a panic at base and at HEAD.

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

Assertions in existing tests: none changed, none deleted, no `t.Run` name touched
(`git diff --stat -- '*_test.go'` = `export_test.go` +9/−1 and `lease_stale_persistence_test.go`
1 line: a helper parameter type and the `withStore` nil guard). The app suite's CI-pinned name
`TestOpenCodeLegacyAssignmentFailsBeforeRunningWorkspaceAndProcess` still resolves
(`harness_test.go:783`, file untouched), and the CI `--assert` rows that name it are unchanged.

## 10. Gates

Run foreground on the host (Windows) unless the row says WSL; `cd daemon` for the Go commands.
Exit codes are the command's own (captured without a pipe where the row reports one).

| # | Command | Result |
| --- | --- | --- |
| 1 | `gofmt -l daemon/internal/app` | no output, exit 0 |
| 2 | `git diff --check` | no output, exit 0 |
| 3 | `rg -n 'store +\*state\.Store' daemon/internal/app --glob '!*_test.go'` | 0 rows (22 at base) |
| 4 | `rg -n 'var _ journalStore' daemon/internal/app/*.go` | 1 row: `journal_store.go:116` with `(*state.Store)(nil)` |
| 5 | `go build ./...` (windows/amd64) | exit 0 |
| 6 | `GOOS=windows GOARCH=arm64 go build ./...` | exit 0 |
| 7 | `GOOS=linux GOARCH=amd64 go build ./...` | exit 0 |
| 8 | `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 |
| 9 | `GOOS=darwin GOARCH=amd64 go build ./...` | exit 0 |
| 10 | `GOOS=darwin GOARCH=arm64 go build ./...` | exit 0 |
| 11 | `GOOS=linux GOARCH=amd64 go vet ./internal/app/` | exit 0 (the `linux && amd64` test files compile) |
| 12 | `go vet ./...` | exit 0 |
| 13 | `go test -count=1 ./internal/app/...` (Windows) | `ok … 141.405s` |
| 14 | `go test -count=1 -coverprofile=… ./internal/app/...` (Windows) | `ok … 130.091s`, coverage 75.3% of statements, exit 0 |
| 15 | `go test -count=1 -run 'TestStorelessDaemon\|TestJournalStore' -v ./internal/app/` | exit 0; 24 probe subtests + 1 member-set test pass |
| 16 | `python3 scripts/ci/test-omo-control-native-evidence.py` (R9) | `Ran 15 tests … OK`, exit 0 |
| 17 | WSL `Ubuntu-24.04`: `go test -count=1 -race ./internal/app/...` (with `-timeout=55m`) | `ok … 2349.759s`, exit 0; wall 41m45s, `user 19m22s / sys 12m10s`; two shorter-timeout attempts are recorded below |
| 18 | `go test -count=1 ./internal/state/...` (the one touched package outside `app`) | `ok … 16.831s`, exit 0 |
| 19 | WSL: `go test -count=1 -run 'TestStorelessDaemon\|TestJournalStore' ./internal/app/` (linux/amd64 probes and both pins) | `ok … 0.180s`, exit 0 |

Row 13 is the contract's exact command; its pipeline exit code was `tail`'s, so the captured exit
code of the same tree appears on row 14 (identical test set, coverage instrumented). Row 15 is the
scoped re-run used by the mutants.

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
in a scratch directory; no repository file is edited). Command for all four (the mutant sources were
regenerated from the delivered bytes after the last comment fix; the test output is piped so the
killing subtest and the captured status are the whole output):

```
go test -overlay=<mutant>.json -count=1 -run '<killing test>' ./internal/app/ 2>&1 | tail -1
```

| Mutant | Production change | Killing test | Observed (final bytes) |
| --- | --- | --- | --- |
| M1 (widening) | `journalStoreBase` gains `LoadLateGoalUsage(key state.RunKey) (state.LateGoalUsageLedger, error)`, a method `*state.Store` has and app never selects | `TestJournalStoreBaseMethodSetIsExact` | `--- FAIL: TestJournalStoreBaseMethodSetIsExact`, `added [LoadLateGoalUsage]`, `FAIL … 0.855s`, exit 1 |
| M2 (nil guard) | `verifyDurableContainmentStopReceipt`'s `if daemon.store == nil` → `!= nil` | `…/cleanup_glue.go:290_verifyDurableContainmentStopReceipt` | `--- FAIL: …/cleanup_glue.go:290_…` then `panic: runtime error: invalid memory address or nil pointer dereference`, `FAIL … 0.767s`, exit 1 |
| M3 (D5 reverted) | `selectStoreWrite` binds `fromStore(daemon.store)` before the guard, as base did | `…/cleanup_glue.go:42_selectStoreWrite_refuses` | `--- FAIL: …/cleanup_glue.go:42_…` plus the same panic, `FAIL … 1.525s`, exit 1 |
| M4 (typed nil) | `withStore` assigns its `*state.Store` argument directly into the interface field | `…/app.go:524_injected_absent_store_stays_untyped_nil` | `injected absent store = (*state.Store)(nil), want an untyped nil`, exit 1 |

Each mutant kills exactly one test and leaves the others green (M2/M3 fail at the subtest that owns
their branch; M1 only the member-set pin), so every new test is load-bearing and no test is a
tautology over the others.

## 12. R2 coverage of touched files

Both runs are the whole app suite with `-coverprofile` on the same test set: HEAD tree and a
throwaway copy of `adfe7d6a` (with the two member-set pins neutralised, since the interface does not
exist at base; the probes ran unchanged in both).

| File | Base | HEAD |
| --- | --- | --- |
| `app.go` | 74.60% (67 funcs) | 74.60% (67 funcs) |
| `cleanup_glue.go` | 58.15% (22 funcs) | **62.64%** (22 funcs) |
| `goal_delivery.go` (app) | 85.48% (28 funcs) | 85.48% (28 funcs) |
| `lease.go` | 79.36% (22 funcs) | 79.36% (22 funcs) |
| `native_run.go` | 77.00% (78 funcs) | 77.00% (78 funcs) |
| `recovery.go` | 78.47% (13 funcs) | 78.47% (13 funcs) |
| package total | 75.4% of statements | 75.3% of statements |

No touched file lost coverage; `cleanup_glue.go` gained 4.5 pp because the lazy fallbacks execute
under the existing tests. `journal_store*.go` declares no statements (types and one interface
assertion), and the `state` alias adds none, so neither can move a line-coverage number.

## 13. R9 / `SOURCE_PATHS`

No code moved out of a `SOURCE_PATHS` file, and `journal_store.go` received no declaration from one:
`app.go` keeps both field declarations (only their type spelling changes) and stays pinned, and the
new files declare new code. Per the contract's condition ("add … if it receives any declaration from
a listed path") `SOURCE_PATHS` is therefore **unchanged**. Alternative reading recorded in the plan
(§D3): pin `journal_store.go` as well, because it projects the method set of
`daemon/internal/state/state.go` (itself pinned). Not applied — it widens the native-evidence freeze
for every future app edit and is the conductor's call.

Gate: `python3 scripts/ci/test-omo-control-native-evidence.py` → `Ran 15 tests in 6.780s … OK`,
exit 0 (row 16 of §10).

## 14. Known limits

- The `selectStoreWrite` fallback shape change (D5) is required by the typed-nil clause but is the
  one edit outside "field-type edits"; W4-12b deletes the function, so its lifetime is one slice.
- The one-line `state` alias (D1) is outside the slice's owned set; recorded as the smallest
  amendment, with the alternative rejected for duplicating the store reference.
- linux/arm64 uses the base set (97 methods; the callers of the 24 are `linux && amd64`), so the
  composite interface exists only on linux/amd64.
- Latent divergence left in place, by analysis rather than probe (none of these sites is a
  `store == nil` branch, so they have no branch outcome to preserve): `cleanup_glue.go:415` and the
  10 unguarded mirror fallbacks (`abortGoalSessionLaunchBeforeNativeStart` and siblings) take a
  store method value that a nil store then calls. `daemon/internal/state` has no
  `if store == nil` guard (`rg -n "if store == nil" daemon/internal/state/*.go` = 0 rows), so at
  base the nil receiver is dereferenced and panics; at HEAD the method value itself panics before
  the call. Both fail with a runtime panic on a nil store; no store method, argument or order is
  involved in either.
- Environment limit (not this slice's): the WSL `-race` app suite passes, but needs more than the Go
  default (10 min) and more than 30 min of wall time in this worktree — 2349.759s with
  `-timeout=55m` — dominated by `TestRawOutputBudgetDropsWithoutFailingSemanticEvents` through the
  pre-existing `protocol.scanJSONValue` path (§10 row 17). Anyone re-running the contract's WSL
  command verbatim must raise `-timeout` or the harness's 10-minute default fires first; the
  Windows runs of the identical test set are green without it.
