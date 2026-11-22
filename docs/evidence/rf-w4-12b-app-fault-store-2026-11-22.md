# rf-w4-12b `apptest.FaultStore` — evidence record (2026-11-22)

Slice: W4-12b of `goal-rf-w4-12b-app-fault-store.md` (arch-decision-2 R1/R2 and its verification
amendments; comment policy 2026-11-22). Plan:
`docs/plans/2026-11-22-rf-w4-12b-app-fault-store-plan.md`.
Base revision of the working tree: `2db5ed0c` (`refactor(app): put the journal store behind a
narrow interface (#244)`, W4-12a merged). The delivered revision identity is recorded in §1.

Dispatch gates re-checked at this revision (`git merge-base --is-ancestor`): W4-12a `2db5ed0c` yes,
W3-08 `6d428d8f` yes, W3-15 `de526861` yes; arch-decision-2 BOUND (conductor dispatch 2026-11-22).

## 1. Deliverable shape

| File | Kind | Lines | sha256 (first 16) |
| --- | --- | --- | --- |
| `daemon/internal/app/apptest/faultstore.go` | new: `FaultStore` over `*state.Store` with 27 `OnX` hooks | 247 | `15061a6e9a199e9e` |
| `daemon/internal/app/apptest/faultstore_test.go` | new: per-hook replace/delegate table, shared-store pin, hook independence | 555 | `534962b4f112143f` |
| `daemon/internal/app/apptest/control.go` | new: configurable `ControlAPI` double | 187 | `c26da32723fb26b9` |
| `daemon/internal/app/apptest/control_test.go` | new: defaults, per-method hooks, detached recordings | 435 | `91291e42c5691144` |
| `daemon/internal/app/apptest/process.go` | new: configurable `Process` double | 97 | `fe8a1e913b916c55` |
| `daemon/internal/app/apptest/process_test.go` | new: defaults incl. `KeepPIDAfterTerminate`, hooks, detached accessors | 171 | `601266fd435dcad2` |
| `daemon/internal/app/apptest/eventsink.go` | new: recording `harness.EventSink` | 52 | `32fb93a7a2a9f45c` |
| `daemon/internal/app/apptest/eventsink_test.go` | new: detached copies, count, `OnHandle` | 100 | `32700ec6311c71c6` |
| `daemon/internal/app/apptest_contract_test.go` | new: `var _ journalStore = (*apptest.FaultStore)(nil)` | 7 | `8ff38f3e766a9887` |

Loc delta on tracked files (`git diff --stat`): 30 files changed, 616 insertions(+), 596
deletions(-) — 9 production files + 21 app test files; the nine new files add 1,931 lines. The
whole tracked code delta is bound by `git diff -- daemon | sha256sum` =
`9c66fe7e30b837b10a61cecedf8b78b3bd86a144c3d1202592f5b34e63a50680`. No production
forwarding/adapter type: the only `journalStore` implementers in the tree are
`*state.Store` (`journal_store.go`) and `*apptest.FaultStore` (test-support package). `apptest`
imports `internal/state`, `internal/authority`, `internal/protocol`, `internal/execution`,
`internal/harness` only; no `apptest` file imports `internal/app` or an importer of it (§9 gate).

The delivered tree is uncommitted by rule (the implement stage leaves the tree for verification);
the base is `2db5ed0c` and the full delta is `git diff` in the worktree.

Full sha256 of the nine new files (64 hex chars, re-derived before handoff):
`faultstore.go` `15061a6e9a199e9e24ee57f27087bd2eb76cae28cf90c518b073940bf5f19e64`;
`faultstore_test.go` `534962b4f112143f5cf4febb1fbbbaec823f673598e0b99b823d91fe2e89220b`;
`control.go` `c26da32723fb26b95bbcae4377244ac0864e3c81f5b05836d99b48d62b63729a`;
`control_test.go` `91291e42c5691144e0165c69b24b3706054323a85023e932c25a5fd637190b31`;
`process.go` `fe8a1e913b916c555b92d06b8fe5a01a880bcf03b9420f1d8695adabc42e1f58`;
`process_test.go` `601266fd435dcad22ee1e334290359c1d287bb765e778d918eaf617a9505739b`;
`eventsink.go` `32fb93a7a2a9f45c1b5a84c8a9d46f7d7352d9cdacb3ddc9a5ebe70be6104d86`;
`eventsink_test.go` `32700ec6311c71c63570db82ebd999778a4a5ec66576af35eb6dda26ce1a6d3a`;
`apptest_contract_test.go` `8ff38f3e766a98879f2d4a22d8a8e4917940e912333ffff9bac7fdda08f6e562`.

## 2. Mirror census (before / after)

One statement over the mirror name list of the `options` struct block (`app.go:206-230` at base):

```
rg -n "options\.(recordWorkspace|recordProcess|recordProcessAuthority|markContainmentUnproven|clearContainmentUnproven|recordContainmentStopReceipt|prepareSupervisorHandoff|bindSupervisorHandoff|commitSupervisorHandoff|recordSupervisorHandoffStopReceipt|clearSupervisorHandoff|clearSupervisorHandoffAfterAbort|queueTerminalTransition|queueGoalUsage|markGoalSessionAttachDeliveryReady|discardUnreadyGoalSessionAttachDelivery|abortGoalSessionLaunchBeforeNativeStart|markGoalSessionUncertain|markCommandAcknowledgementsDelivered|queueTerminalTransitionAndAcknowledgement|queueCancelledTransitionAndAcknowledgement|retainWorkspace|loadGoalSession|closeGoalSession|clearProcessDetails)" daemon/internal/app
```

Base (`2db5ed0c`): 41 non-test reads in 9 files (`app.go` 4, `cleanup_glue.go` 13,
`goal_admission.go` 4, `goal_delivery.go` 2, `native_run.go` 4, `outbox.go` 8, `recovery.go` 4,
`recovery_glue.go` 1, `supervisory.go` 1) and 92 mirror-*setting* sites in 20 app test files
(textual hits 93: `cleanup_test.go`'s table value `recordProcess` plus the
`daemon.options.recordProcess = test.recordProcess` assignment are one logical site).
HEAD: the statement returns no match (`rg` exit 1), and the 25 mirror fields are gone from the
`options` block. `rg -n 'storeCallbackWrite'` = no match; the `selectStoreWrite` override parameter
and branch are gone (§3, D1); the store-lifetime read lock, `storeClosing` predicate and
`errDaemonStoreClosing` refusal remain.

Mirror-setting sites -> hook sites: §4.

## 3. Dead-in-production table (one row per removed branch)

Production never set a mirror (`Run` builds 10 unrelated defaults; `cmd/symmetry-daemon` passes
only `WithLogWriter`), so every removed mirror branch was dead in production and the retained path
is the exact store call the base fallback made. Base line numbers are at `2db5ed0c`.

| # | base site | mirror branch (removed) | retained path (byte-identical to the base fallback) |
| --- | --- | --- | --- |
| 1 | `app.go:1633-1634` | `recordProcess` inline override in `startAssigned` | `daemon.store.SetProcessDetails(key, pid, identity, daemon.options.clock())` |
| 2 | `app.go:2118-2119` | `recordWorkspace` inline override in `persistWorkspacePath` | `daemon.store.SetWorkspacePath(key, path)` |
| 3 | `cleanup_glue.go:61` | `recordProcessAuthority` half of the `persistProcessAuthorityCallback` guard | `storeClosing \|\| store == nil` |
| 4 | `cleanup_glue.go:80` | `recordProcess` half of the `persistProcessMarker` guard | `storeClosing \|\| store == nil` |
| 5 | `cleanup_glue.go:84-85` | `recordProcess` override in `persistProcessMarker` | `daemon.store.SetProcessDetails(key, pid, identity, startedAt)` |
| 6 | `cleanup_glue.go:115` | `markContainmentUnproven` selector override | `store.MarkContainmentUnproven` |
| 7 | `cleanup_glue.go:129` | `clearContainmentUnproven` selector override | `store.ClearContainmentUnproven` |
| 8 | `cleanup_glue.go:143` | `recordContainmentStopReceipt` selector override | `store.RecordContainmentStopReceipt` |
| 9 | `cleanup_glue.go:333` | `prepareSupervisorHandoff` selector override | `store.PrepareSupervisorHandoff` |
| 10 | `cleanup_glue.go:347` | `bindSupervisorHandoff` selector override | `store.BindSupervisorHandoff` |
| 11 | `cleanup_glue.go:361` | `commitSupervisorHandoff` selector override | `store.CommitSupervisorHandoff` |
| 12 | `cleanup_glue.go:387` | `clearSupervisorHandoff` selector override | `store.ClearSupervisorHandoff` |
| 13 | `cleanup_glue.go:401` | `clearSupervisorHandoffAfterAbort` selector override | `store.ClearSupervisorHandoffAfterAbort` |
| 14 | `cleanup_glue.go:431-436` | `clearProcessDetails` override-else-store | `daemon.store.ClearProcessDetails(key, pid, identity)` |
| 15 | `goal_admission.go:321-325` | `abortGoalSessionLaunchBeforeNativeStart` override-else-store | `daemon.store.AbortGoalSessionLaunchBeforeNativeStart(key)` |
| 16 | `goal_admission.go:343-347` | `markGoalSessionUncertain` override-else-store | `daemon.store.MarkGoalSessionUncertain(key, reason)` |
| 17 | `goal_admission.go:599-603` | `loadGoalSession` override-else-store | `daemon.store.LoadGoalSession(key)` |
| 18 | `goal_admission.go:607-611` | `closeGoalSession` override-else-store | `daemon.store.CloseGoalSession(key)` |
| 19 | `goal_delivery.go:30-34` | `markGoalSessionAttachDeliveryReady` override-else-store | `daemon.store.MarkGoalSessionAttachDeliveryReady(key, localHandleID)` |
| 20 | `goal_delivery.go:56-60` | `discardUnreadyGoalSessionAttachDelivery` override-else-store | `daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID)` |
| 21 | `native_run.go:727-731` | `retainWorkspace` override in `retainUnknownGoalLaunchWorkspaceChecked` | `daemon.store.RetainWorkspace(key)` |
| 22 | `native_run.go:1022-1032` | `queueGoalUsage` two-step branch in `queueNativeUnknownUsageRecovery` (`MarkNativeUsageRecoveryRequired` + mirror) | `daemon.store.QueueNativeUsageRecovery(key, usage)` |
| 23 | `native_run.go:1040-1046` | `queueGoalUsage` override in `queueNativeGoalUsageRecord` | `daemon.store.QueueGoalUsage(key, usage)` |
| 24 | `native_run.go:1084-1087` | `queueGoalUsage` two-step branch in `queueNativeGoalUsageSettlement` (`MarkNativeUsageRecoverySettlement` + mirror) | `daemon.store.QueueNativeUsageRecoverySettlement(key, *usage, settlement)` |
| 25 | `outbox.go:142-146` | `queueTerminalTransition` override in `queueTerminalTransitionWithRetry` | `daemon.store.QueueTerminalTransitionAt(key, transition, enteredAt)` |
| 26 | `outbox.go:212-219` | `queueTerminalTransitionAndAcknowledgement` / `queueCancelledTransitionAndAcknowledgement` ("cancelled" + "applied" special case) | `daemon.store.QueueTerminalTransitionAndAcknowledgementAt(key, transition, acknowledgement, enteredAt)` |
| 27 | `outbox.go:1125-1129` | `markCommandAcknowledgementsDelivered` override in `markCommandAcknowledgementDelivered` | `daemon.store.MarkCommandAcknowledgementsDelivered(key, []string{acknowledgement.AckID})` |
| 28 | `recovery.go:674-677` | `recordProcessAuthority` override in `persistProcessAuthority` | `daemon.store.SetProcessDetailsWithAuthority(key, pid, identity, daemon.now(), candidate)` |
| 29 | `recovery.go:760-762` | `queueTerminalTransition` override in `queueRecoveryTerminalTransition` | `daemon.store.QueueTerminalTransitionAt(key, transition, pendingAt)` |
| 30 | `recovery_glue.go:31-38` | `recordSupervisorHandoffStopReceipt` override-else-store | `store == nil` refusal, then `daemon.store.RecordSupervisorHandoffStopReceipt(key, expected.Clone(), receipt)` |
| 31 | `supervisory.go:230-233` | `retainWorkspace` override in `persistWorkspaceRetention` | `daemon.store.RetainWorkspace(key)` |
| 32 | `cleanup_glue.go:31-39,46-58` | `storeCallbackWrite` constraint type; `selectStoreWrite`'s `override != nil` branch and `override`/`Fn` plumbing | `selectStoreWrite[Fn any]` returns `fromStore(store)` under the same read lock and the same `!storeClosing && store != nil` predicate |

Removed-branch count: 41 read/override sites across 31 symbol rows, plus the selector mechanism
(row 32). Every removed branch tested the mirror for nil; production left it nil, so the branch was
unreachable there. The two`queueGoalUsage` two-step branches (rows 22, 24) performed more than one
store write; tests that injected through them now inject through the single production write (§5).

## 4. Migration table (mirror sites -> FaultStore hooks)

The mapping was derived mechanically and is reproducible in one command set: base files via
`git show 2db5ed0c:<file>`; base sites by the two regexes `options\.<mirror>\s*=` and
`(^|[{,])\s*<mirror>\s*:` (the second catches single-line `options{...}` literals); HEAD hooks by
`\.(On[A-Z][A-Za-z0-9]*)\s*=`; both bucketed by the enclosing `func Test...` and compared per
function (the throwaway generator is not part of the delivered tree; the regexes rebuild it).
Result: 92 base logical sites in 20 files -> 92 hook sites in 20 files; per-function counts are
equal everywhere (the only textual 93rd base hit is `cleanup_test.go`'s table value
`recordProcess:` whose value is assigned to `daemon.options.recordProcess`; §3 rows 5/22/24/26
carry the corresponding production deletions), and the per-function name mapping is 1:1 except the
divergences in §5.

| mapping class | sites | example |
| --- | --- | --- |
| same-method rename (`options.X` -> `OnX` with the same Store method) | 74 | `recordWorkspace` -> `OnSetWorkspacePath` (`cleanup_test.go`), `recordProcess` -> `OnSetProcessDetails`, `recordProcessAuthority` -> `OnSetProcessDetailsWithAuthority` |
| cancelled/atomic terminal pair collapsed to the one production method | 8 | `queueTerminalTransitionAndAcknowledgement` and `queueCancelledTransitionAndAcknowledgement` -> `OnQueueTerminalTransitionAndAcknowledgementAt` |
| `queueGoalUsage` two-step branches -> the single production write | 10 | `queueGoalUsage` -> `OnQueueNativeUsageRecoverySettlement` (9), `OnQueueNativeUsageRecovery` (1) — see §5 |
| storeless -> real temp Store (setup change, names kept) | 4 cases | 3 `journal_store_test.go` subtests + the `overrideOnly` block of `TestStoreCallbackFactoriesRefuseClosingStore` — §5a |

## 5. Divergence table

Rules applied: clause 1's "unmodified assertions and subtest names" was verified normalized
(whitespace-stripped) across all 21 changed test files: the only delta is 3 added `t.Fatal(err)`
lines in `journal_store_test.go`; no assertion text, no subtest name, no test function name, and no
test-local `type` declaration changed anywhere (set-diff empty). Every row below is a setup or
store-interaction change with the base and HEAD outcomes recorded.

### 5a. `store == nil` decisions (verification-amendment table)

The 21 PROBES of `TestStorelessDaemonTakesEveryStoreNilBranch` were re-derived at HEAD: 18 probes
still run a storeless daemon and still take their nil-store branch; 3 subtests now install a real
`FaultStore` and take the store path. Base vs HEAD outcome per migrated subtest:

| subtest (name unchanged) | base setup | base `store == nil` decision | HEAD setup | HEAD decision | nil-branch coverage retained by |
| --- | --- | --- | --- | --- | --- |
| `cleanup_glue.go selectStoreWrite override wins without a store` | `&daemon{}` + `markContainmentUnproven` mirror | taken (mirror wins) | `FaultStore` + `OnMarkContainmentUnproven` | not taken (store path) | `cleanup_glue.go selectStoreWrite refuses` (same test, predates slice) |
| `cleanup_glue.go persistProcessMarker mirror error skips the readback` | `&daemon{}` + `recordProcess` mirror | taken (mirror error) | `FaultStore` + `OnSetProcessDetails` | not taken (store path) | `cleanup_glue.go persistProcessMarker` (same test, predates slice) |
| `cleanup_glue.go clearPersistedProcessDetails skips the readback` | `&daemon{}` + `clearContainmentUnproven`/`clearProcessDetails` mirrors | taken | `FaultStore` + `OnClearContainmentUnproven`/`OnClearProcessDetails` | not taken | `cleanup_glue.go selectStoreWrite refuses` (same test, predates slice) |

One further migrated test reaches a `store == nil` decision, found by the first full suite run to
complete the package (the earlier 601 s run aborted before it): the last block of
`TestStoreCallbackFactoriesRefuseClosingStore` (`process_persistence_test.go:427-434`) ran
`&daemon{}` + mirror at base ("override wins without a store") and initially still ran `&daemon{}`
at HEAD, where the hook lives on the store the daemon does not hold, so `selectStoreWrite`
correctly refused with `errDaemonStoreClosing` and all ten subtests reddened
(`process_persistence_test.go:431: override without a store: error = daemon state store is closing,
want store callback override probe`). Per Design 4 the daemon now holds the same `FaultStore` all
other blocks of the test use (`overrideOnly := &daemon{store: fault}`); the subtest names
(`persistProcessAuthorityCallback`, ...) and the failure-message text are unchanged; the nil-store
side of this test is still exercised by its own `pin.invoke(&daemon{})` block ("no store and no
override"), which predates the slice. After the fix:
`go test -count=1 -run TestStoreCallbackFactoriesRefuseClosingStore ./internal/app/` = ok, exit 0.
No other base mirror site ran with `store == nil`.

### 5b. Per-test store-interaction divergences

| test | base mirrors | HEAD hooks | base store interaction | HEAD store interaction | assertion outcome |
| --- | --- | --- | --- | --- | --- |
| `app_test.go` `TestCancelDuringBlockedPrepareAcknowledgesAndCleansUp` | `queueCancelledTransitionAndAcknowledgement` | `OnQueueTerminalTransitionAndAcknowledgementAt` | cancelled mirror branch (1 mirror call) | production atomic write (1 hook call) | unchanged, green |
| `app_test.go` `TestCancelledReceiptTreatsDurablePersistenceAsIdempotent` | same | same | mirror fallback called `store.QueueCancelledTransitionAndAcknowledgementAt` | hook fallback calls `store.QueueTerminalTransitionAndAcknowledgementAt` (the method production calls) | unchanged, green |
| `app_test.go` `TestRecoveredCancelQueuesReceiptBeforeResolvingGoalSessionBarrier` | same | same | as above | as above | unchanged, green |
| `app_test.go` `TestScheduledSnapshotCancelPreemptsBlockedInputAndJoins` | same | same | as above | as above | unchanged, green |
| `retention_failure_test.go` `TestCancellationStopsWhileRetentionAndTerminalJournalWritesFail` | same | same | as above | as above | unchanged, green |
| `atomic_terminal_ack_interleaving_test.go` (2 tests) | `queueTerminalTransitionAndAcknowledgement` | `OnQueueTerminalTransitionAndAcknowledgementAt` | interception at the non-cancelled mirror | interception at the same production write | unchanged, green |
| `native_settlement_regression_test.go` `TestNativeUsageRetryExhaustionKeepsAtomicCancelOutcomeAndOutputEvidence` | same | same | as above | as above | unchanged, green |
| `goal_delivery_test.go` `TestNativeGoalUsageFailureBlocksTerminalizationAndRetainsRun`, `TestNativeGoalUsageRetrySucceedsWithoutRestartAndFinalizesOnce`, `TestNativeGoalUsageRetryExhaustionBlocksRenewalAndRetainsEvidence` | `queueGoalUsage` | `OnQueueNativeUsageRecoverySettlement` (hook delegates for the retry-success and finalization call sites, reproduces the base two-step for the settlement attempt) | two-step: `MarkNativeUsageRecoverySettlement` + mirror | one production write `QueueNativeUsageRecoverySettlement` for the settlement attempt; other call sites delegate to the real Store | unchanged, green |
| `goal_delivery_test.go` `TestNativeCancellationOwnerStopsLosingTerminalWaiter` | `queueGoalUsage` | `OnQueueNativeUsageRecoverySettlement` | two-step | one production write | unchanged, green |
| `native_settlement_regression_test.go` `TestCancelledNativeStartCloseFailureQueuesExplicitUsageBeforeTerminal`, `TestLateCancelSubjectMismatchSurvivesUsageRetry`, `TestOrphanedNativeUsageRetryCannotReleaseCancellationOwner` | `queueGoalUsage` | `OnQueueNativeUsageRecoverySettlement` | two-step | one production write | order tokens recorded at the same logical point; unchanged, green |
| `harness_test.go` `TestGoalCancellationRetriesNativeUsageBeforeReceipt` | `queueGoalUsage` | `OnQueueNativeUsageRecoverySettlement` | two-step | one production write | unchanged, green |
| `recovery_registration_test.go` `TestNativeRecoveryPersistenceFailuresBlockRegistration` | `queueGoalUsage` | `OnQueueNativeUsageRecovery` | two-step (`MarkNativeUsageRecoveryRequired` + mirror) | one production write `QueueNativeUsageRecovery` | unchanged, green |
| `goal_delivery_test.go` `TestNativeTerminalReservationSurvivesLeaseTerminationDuringUsageRetry` | `queueGoalUsage` | `OnQueueNativeUsageRecoverySettlement` | two-step | one production write | unchanged, green |
| `journal_store_test.go` (3 subtests) | 4 mirrors | 4 hooks | storeless mirror branch | real temp Store path | see 5a |
| `process_persistence_test.go` `TestPersistProcessAuthorityCallbackDoesNotCallMarkerOverride` | `recordProcess` | `OnSetProcessDetails` | the *marker* mirror must not be called | the *marker* hook must not be called | unchanged, green |
| `process_persistence_test.go` `TestCommitSupervisorHandoffCallbackDoesNotObserveFailedCommit`, `TestStoreCallbackObserversFollowTheOverride` | 12 mirrors | 12 hooks | observers follow the mirror | observers follow the hook | unchanged, green |

No assertion outcome changed. Every divergence is a test-side injection point that moved from a
deleted mirror branch to the production write the base fallback already used; the two-step branches
(§3 rows 22, 24, 26) existed only when a mirror was set, i.e. only in tests.

### 5c. Per-file fakes that stay local

The shared doubles are consumed by the migrated files as `FaultStore`; the three real-seam doubles
(`ControlAPI`, `Process`, `EventSink`) ship with their own unit tests and mutants (§8). The
per-file fakes stay local. Classifier (read-only inventory over all 58 app `_test.go` files at
HEAD, method-set matching by own receivers, then external `.field`/channel reads outside the
type's own methods and construction sites):

- ControlAPI seam: 4 types reach 9/9 own receivers — `restartRecoveryControl`,
  `zeroRetryClaimControl`, `fakeControl` (all LOCAL-REQUIRED: counters/`calls`/`claimCalls`/
  `registerCalls`/`machineIDs`/`registrations`/`claimIDs`/`heartbeat` are read by `t.Fatalf`
  predicates at 20+ sites) and `deadlineRecordingControl` (no external reads, but its capability
  is Context recording + `assertBoundedAndCancelled`, which `apptest.ControlAPI` does not offer;
  converting needs new shared-double API and rewiring the helper call, and its two use sites are
  `app_test.go:6652/:6679`).
- 36 further `app_test.go` types get the ControlAPI method set by embedding `fakeControl` /
  `*fakeControl` (`fixedClaimControl`, `gatedAppendControl`, `orderingControl`,
  `terminalRetryControl`, `clockSnapshotControl`, ...). Each adds one to three overrides whose
  state or gate channels are read by assertions; none is a pure hook configuration.
- Process seam: 18 types reach 4/4. Ten are LOCAL-REQUIRED (channel choreography or counters read
  by assertions: `recordingProcess` `.writes`/`.terminations`, `blockingInputProcess`,
  `inputTimeoutProcess`, `startFailureProcess`, `unsupportedCompletionProcess`,
  `blockingProcess`, `deferredExitProcess`, `startupFailureProcess`, `cancelReturnedProcess`,
  `leaseRenewalProcess`, `finalizingProcess`, `failingSupervisoryInputProcess`). The remaining
  candidates and why each stays:

| fake | declaration | why it stays local |
| --- | --- | --- |
| `fakeProcess` | `app_test.go:9175` | expressible (`apptest.Process{KeepPIDAfterTerminate: true}`, plan D6) but 46 use sites span nine files, four of them outside the STRICT owned set (`decision_requirement_test.go`, `deterministic_validation_test.go`, `generic_process_stop_test.go`, `recovery_isolation_test.go`) |
| `typedNilProcess` | `app_test.go:9428` | exists to be a typed-nil `Process`; a shared concrete double cannot carry that semantics |
| `failingTerminateProcess` | `app_test.go:9504` | only use site `lease_stale_persistence_test.go:337` (not owned) |
| `terminatingInputProcess` | `app_test.go:9520` | only use site `decision_requirement_test.go:198` (not owned) |
| `recoveryBlockingProcess` | `recovery_isolation_test.go:766` | declared in a file that sets no mirror and declares no migrated fake (not owned) |
| `lifecycleProcess` | `run_lifecycle_test.go:32` | same (not owned) |
| `linuxV2WitnessDropStatusFakeProcess`, `linuxV2AdmissionStatusFakeProcess`, `journalStoreDropProbeProcess` | `linux_owner_witness_linux_test.go:507`, `linux_owner_v2_app_test.go:2642`, `journal_store_linux_test.go:52` | additionally implement the non-seam `LinuxV2WitnessDropStatus`/`LinuxV2LiveOwnerStatus` interfaces, which `apptest.Process` does not offer |
| EventSink seam | — | 0 app-local `Handle` receivers at base and HEAD; nothing to migrate (new infrastructure, parent contract) |

### 5d. Double defaults vs the fakes they encode (no fake was replaced)

No app fake was deleted, so the parent contract's "a migrated double answers like the fake it
replaced" clause is vacuous; the defaults still encode the majority behaviour, verified by reading
the fakes:

| double method | `apptest` default | source fake behaviour |
| --- | --- | --- |
| `ControlAPI.RegisterSession` | `{Runtimes: [{default, runtime-1, 1}], LeaseDurationMS: 120_000}` | `fakeControl.RegisterSession` returns exactly this (`app_test.go:8833-8841` region) |
| `ControlAPI.Heartbeat` | zero `RuntimeSnapshot` | `fakeControl.Heartbeat` |
| `ControlAPI.Dispatch` | zero `RuntimeSnapshot` | `fakeControl.Dispatch` when no assignment is pending (its pending-assignment case is an `OnDispatch` hook) |
| `ControlAPI.Claim` | `{RunID, Generation, ClaimID, "lease", now+1m, Work{Goal:"work"}}` | `fakeControl.Claim`; its `providerAccess` pass-through is an `OnClaim` hook |
| `ControlAPI.RenewLease` | `{LeaseExpiresAt: now+1m}` | `fakeControl.RenewLease` |
| `ControlAPI.AppendEvents` / `Reconcile` / `AcknowledgeCommand` | `nil` / zero response / `nil` | same in `fakeControl` |
| `ControlAPI.Transition` | `nil` | `fakeControl.Transition` (its `cancel` side effect is an `OnTransition` hook) |
| `Process` | `WriteInput`/`Terminate` succeed; `Wait` returns `Result`; `ProcessDetails` = `(42, "test:42")`, `(0, "")` after terminate | `fakeProcess` = the same with the pid kept alive (`KeepPIDAfterTerminate: true`, plan D6); `recordingProcess` adds recorded inputs |
| `EventSink` | records detached copies; `OnHandle` replaces the error | no source double (0 `Handle` receivers app-wide) |

### 5e. Handoff-related hooks (informational; O5 as superseded)

Per the verification amendment, this slice does **not** supply W6-03's durable-supervisor-handoff
capability (the supplier is W4-12c's Recorder handoff-capability query). For W6-03's later re-draft
the in-process handoff control an app test can install today is the FaultStore hook set:
`OnPrepareSupervisorHandoff`, `OnBindSupervisorHandoff`, `OnCommitSupervisorHandoff`,
`OnRecordSupervisorHandoffStopReceipt`, `OnClearSupervisorHandoff`,
`OnClearSupervisorHandoffAfterAbort` — each replaces exactly the store write named in §3 rows
9–13, 30.

## 6. Base-vs-HEAD control-flow audit per deleted fake (lesson 32)

No test-local type was deleted, added or renamed in the slice, and no test function was deleted,
added or renamed (set diffs of `^type ` and `^func Test` over the 21 changed files are empty). The
base-vs-HEAD control-flow audit therefore covers the one migration with internal branching — the
`queueGoalUsage` two-step hooks — and the `FaultStore` delegations:

| base control-flow branch | HEAD reproduction | preserved |
| --- | --- | --- |
| mirror set -> call mirror, ignore store | hook set -> call hook, ignore embedded Store | yes (per-method hook replace; `faultstore_test.go` `TestFaultStoreHookReplacesEveryWrite`) |
| mirror nil -> bind store method, call it | hook nil -> call embedded Store method with the same arguments | yes (`TestFaultStoreNilHookDelegatesEveryWrite`) |
| `queueNativeUsageRecovery`: mirror -> `MarkNativeUsageRecoveryRequired` then mirror | `OnQueueNativeUsageRecovery` hook replaces the single atomic `QueueNativeUsageRecovery` write | yes for the injection point; the two-step journal write no longer exists (dead in production) |
| `queueNativeGoalUsageSettlement`: mirror -> `MarkNativeUsageRecoverySettlement` then mirror | `OnQueueNativeUsageRecoverySettlement` hook; migrated hooks delegate for the retry-success/finalization call sites and reproduce the two-step only for the settlement attempt | yes per test assertion; branch evidence in §5b |
| `queueTerminalTransitionAndAcknowledgement` "cancelled"+"applied" special case | production always called the atomic method (mirror nil); the hook intercepts that method | yes |
| storeless mirror override wins with `daemon.store == nil` | hook wins with a real Store installed | changed by design (D5); nil-path coverage retained (§5a) |
| `FaultStore` hook + embedded Store both present | hook replaces, embedded Store untouched | yes (`TestFaultStoreHooksAreIndependent`) |

## 7. Receiver-method counts before/after

Base (`2db5ed0c`), app-only (`daemon/internal/app/*_test.go`), one statement per method:
`git grep -o "func ([^)]*) <Method>(" -- 'daemon/internal/app/*_test.go' | sort -u | wc -l`;
module-wide the same over `daemon/**/*_test.go`.

| Method | app base -> HEAD | module base -> HEAD |
| --- | --- | --- |
| RegisterSession | 10 -> 10 | 10 -> 10 |
| Heartbeat | 6 -> 6 | 6 -> 6 |
| Dispatch | 7 -> 7 | 7 -> 7 |
| Claim | 10 -> 10 | 10 -> 10 |
| RenewLease | 18 -> 18 | 21 -> 21 |
| AppendEvents | 17 -> 17 | 17 -> 17 |
| Transition | 24 -> 24 | 24 -> 24 |
| Reconcile | 12 -> 12 | 12 -> 12 |
| AcknowledgeCommand | 15 -> 15 | 15 -> 15 |
| WriteInput | 19 -> 19 | 21 -> 21 |
| Terminate | 19 -> 19 | 37 -> 37 |
| Wait | 26 -> 26 | 35 -> 35 |
| ProcessDetails | 24 -> 24 | 32 -> 32 |
| Handle | 0 -> 0 | 20 -> 20 |

Every count is identical base vs HEAD, as expected: no test-local receiver was added or removed
(the shared doubles live in non-`_test.go` files and this command counts `_test.go` receivers
only). The duplicate-declaration drop this slice owes the parent contract is the 25 `options`
mirror fields plus the selector mechanism, not a `_test.go` receiver count (§2, §3); the
`apptest` doubles are new infrastructure whose own receivers are outside this census by
construction. Note: the drafting artifact's `Handle module = 19` under-counts by one; re-derived
with the stated command both revisions give 20 (the artifact's dedup/glob differed, not the tree).

## 8. Mutants

One run per mutant: apply the mutation, run the named test, restore the file, verify its sha256
against the pre-mutation copy (`sha256sum -c`). Every mutant reddened the intended test; every
restore verified `OK`; `gofmt -l` on the restored files is clean.

| mutant | mutation | command | observing test | result |
| --- | --- | --- | --- | --- |
| (a) control default flipped to refusal | `apptest/control.go` `Claim` returns `errors.New("mutant: claim refused")` | `go test -count=1 -run TestControlAPIDefaultsAnswerTheMajorityBehaviour ./internal/app/apptest/` | `TestControlAPIDefaultsAnswerTheMajorityBehaviour/Claim` | FAIL, exit 1: `control_test.go:53: Claim() error = mutant: claim refused` |
| (b) sink copy dropped | `apptest/eventsink.go` `Handle` appends `event` instead of `copyEvent(event)` | `go test -count=1 -run TestEventSinkHandleRecordsDetachedCopies ./internal/app/apptest/` | `TestEventSinkHandleRecordsDetachedCopies` | FAIL, exit 1: `eventsink_test.go:40: Events()[0].Data = "Xvent-data", want "event-data"` |
| (c) fake process keeps a live pid after termination | `apptest/process.go` `ProcessDetails` drops the `terminated && !KeepPIDAfterTerminate` branch | `go test -count=1 -run TestProcessDefaultsAnswerTheMajorityBehaviour ./internal/app/apptest/` | `TestProcessDefaultsAnswerTheMajorityBehaviour/ProcessDetails` | FAIL, exit 1: `ProcessDetails() after Terminate() = (42, "test:42"), want (0, "")` |
| (d) migrated hook dropped (option not load-bearing) | `native_settlement_regression_test.go` `TestCancelledNativeStartAbortFailureRetainsRecoveryAndCannotCleanup` loses its `OnAbortGoalSessionLaunchBeforeNativeStart` hook | `go test -count=1 -run TestCancelledNativeStartAbortFailureRetainsRecoveryAndCannotCleanup ./internal/app/` | same test | FAIL, exit 1: `native_settlement_regression_test.go:1027: settle uncommitted nil-session abort = true, <nil>` |
| (e) store hook deleted | `apptest/faultstore.go` `SetProcessDetails` ignores `OnSetProcessDetails` | `go test -count=1 -run TestFaultStoreHookReplacesEveryWrite ./internal/app/apptest/` | `TestFaultStoreHookReplacesEveryWrite/SetProcessDetails` | FAIL, exit 1: `faultstore_test.go:402: SetProcessDetails() error = process details are invalid, want fault hook` |
| (f) store nil-guard flipped | `cleanup_glue.go` `persistProcessMarker` guard becomes `if daemon.storeClosing` | `go test -count=1 -run TestStorelessDaemonTakesEveryStoreNilBranch ./internal/app/` | `TestStorelessDaemonTakesEveryStoreNilBranch/cleanup_glue.go_persistProcessMarker` | FAIL, exit 1: `panic: runtime error: invalid memory address or nil pointer dereference` in `persistProcessMarker.func1` |

Mutant (d) is the original gates' "(d) in one migrated file, drop an option so a case's refusal
path is not taken". The (a)/(c) mutants are the original (a)/(c); (b) is the original (b). No
mutant required an assertion change; all kill via pinned unit tests or unchanged app assertions.

## 9. Gate outputs

Working directory `daemon/` unless the command starts with a repo-root path. All gates ran on the
delivered (uncommitted) tree; the Windows suite and the WSL race suite never ran concurrently.

| gate | command | result |
| --- | --- | --- |
| mirror census | `rg -n "options\.(recordWorkspace\|...\|clearProcessDetails)" daemon/internal/app` (repo root; full 25-name pattern, §2) | no match, `rg` exit 1 |
| override mechanism | `rg -n 'storeCallbackWrite' daemon/internal/app` | no match, exit 1 |
| `selectStoreWrite` | `sed -n '/^\/\/ Takes the store lifetime read lock/,/^}/p' internal/app/cleanup_glue.go` | `selectStoreWrite[Fn any](daemon, fromStore)`; same read lock, same `!storeClosing && store != nil` predicate, same `errDaemonStoreClosing`; no override parameter — pasted in §3 row 32 context |
| gofmt | `gofmt -l .` | empty, exit 0 |
| whitespace | `git diff --check` (repo root) | empty, exit 0 |
| vet | `go vet ./...` | exit 0 |
| vet, GOOS=linux | `GOOS=linux GOARCH=amd64 go vet ./internal/app/` (linux-only test files compile) | exit 0 |
| vet, GOOS=windows | `GOOS=windows GOARCH=amd64 go vet ./internal/app/` | exit 0 |
| builds | `GOOS=linux GOARCH=amd64 go build ./...`, `GOOS=windows GOARCH=amd64 go build ./...`, `GOOS=darwin GOARCH=amd64 go build ./...` | exit 0 each |
| apptest import boundary | `go list -deps ./internal/app/apptest \| grep "symmetry/daemon/internal/app"` | only `.../internal/app/apptest` itself |
| assertion/subtest-name invariance | one normalized set diff per changed test file — `diff <(git show 2db5ed0c:"$f" \| grep -E 't\.(Fatalf\|Fatal\|Errorf\|Error\|Skip\|Skipf)\(\|t\.Run\(\|name: "' \| sed 's/^[[:space:]]*//' \| sort) <(grep -E <same> "$f" \| sed <same> \| sort)` — plus the same over `^(type \|func Test)` | only 3 added `t.Fatal(err)` in `journal_store_test.go`; type and test-name set diffs empty |
| Windows suite | `go test -count=1 -timeout 1200s ./internal/app/...` | final tree: `ok .../internal/app 494.980s`; `ok .../internal/app/apptest 2.527s`; exit 0; 0 `--- FAIL` lines. Earlier green run on the pre-lint-fix tree: `ok .../internal/app 418.945s`; `ok .../internal/app/apptest 4.071s` |
| suite-timeout note | first attempt without `-timeout` aborted at `panic: test timed out after 10m0s` (601.008s) while `TestDurablePiProviderBridgeExecutorReservesCompletionCapacityBeforeDispatch` was 4 s in, with **no** failing test before it; that run also overlapped foreground `go vet`/`go build` gates (CPU contention) | recorded; CI uses `-timeout 1200s` (`ci.yml:681`) |
| earlier partial run | the same suite at `-timeout 1200s` completed in 541 s and exposed the one real failure fixed in §5a (`TestStoreCallbackFactoriesRefuseClosingStore`, 1 `--- FAIL`) | fixed; rerun above = green |
| WSL race | `wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd /mnt/q/repos/wt-w4-12b-app-fault-store/symmetry/daemon && go test -count=1 -race -timeout 3000s ./internal/app/... ./internal/platform/... ./internal/execution/...'` | `ok .../internal/app 1138.724s`; `ok .../internal/app/apptest 2.101s`; `ok .../internal/platform 67.882s`; `ok .../internal/execution 24.494s`; exit 0; 0 `--- FAIL`, 0 `WARNING: DATA RACE` |
| WSL race, first attempt | same command with `-timeout 1200s` | `FAIL .../internal/app 1200.179s` (timeout panic; the same `TestDurablePiProviderBridgeExecutorReservesCompletionCapacityBeforeDispatch` was 6 s in at the deadline; platform/execution/apptest ok); no failing test before the deadline, no data race. CI's runner reports ~386 s for this package; this WSL needs ~1139 s |
| R9 | `python scripts/ci/test-omo-control-native-evidence.py` | `Ran 15 tests ... OK`, exit 0 |
| lint ratchet | `GOOS=linux golangci-lint run --config .golangci.yml --max-issues-per-linter=0 --max-same-issues=0 ./internal/app/...` (local golangci-lint 2.15.0 = the CI pin), at base (`2db5ed0c` scratch tree) and HEAD | base 146 findings; HEAD 145 (one new S1021 fixed, see below); `internal/app/apptest` = **0** findings; no new finding of any category at HEAD |
| R2 coverage | `go test -count=1 -timeout 2400s -coverprofile=... ./internal/app/` at base and HEAD; `go test -count=1 -cover ./internal/app/apptest/` | base `ok ... 411.086s coverage: 75.3%`; HEAD `ok ... 408.687s coverage: 75.2%`; apptest `ok ... 99.6%`; exit 0 each; per-file table in §10 (no touched file has more uncovered statements than at base) |

The lint set diff (normalized by file + message, line/col dropped) between base and final HEAD is
exactly: `app.go` `startAssigned` gocyclo 88 -> 87, and the `outbox.go` `ifElseChain` finding
removed with the deleted mirror branch; one finding the slice had introduced
(`atomic_terminal_ack_interleaving_test.go` staticcheck `S1021: should merge variable declaration
with assignment on next line`, from moving the hook out of the `daemon` literal) was fixed by
merging `var app *daemon` / `app = &daemon{...}` into `app := &daemon{...}` — a declaration merge,
no assertion or subtest-name change, and the file's two tests re-run green on Windows.

Successive Windows suites, the WSL race suite, the coverage suites and the golangci-lint runs were
serialized except two low-load pairs: the four apptest mutants (a), (b), (c), (e) ran while the
second Windows suite was in flight (its test binaries were already built and started — its
`apptest` line lands at 06:04:48, before the first mutation), and the two coverage suites ran
alongside the final WSL race run (one test binary each, 16 cores, ~26 GB free). No timeout or
failure is attributed to either overlap; the one timeout observed is explained in its own row.

## 10. R2 coverage of touched files

Commands (serialized):
base `cd .scratch-w4-12b-check/base/daemon && go test -count=1 -timeout 2400s -coverprofile=cover-base.out ./internal/app/`
-> `ok ... 411.086s coverage: 75.3% of statements`, exit 0;
HEAD `cd daemon && go test -count=1 -timeout 2400s -coverprofile=../.scratch-w4-12b-check/cover-head.out ./internal/app/`
-> `ok ... 408.687s coverage: 75.2% of statements`, exit 0;
`go test -count=1 -cover ./internal/app/apptest/` -> `ok ... 99.6% of statements`, exit 0.

Per-file statement coverage of the nine touched production files (aggregated from the profiles by
`awk` per file: covered statements, total statements, uncovered = total - covered):

| file | base covered/total | HEAD covered/total | base uncovered | HEAD uncovered |
| --- | --- | --- | --- | --- |
| `app.go` | 833/1152 (72.31%) | 829/1148 (72.21%) | 319 | 319 |
| `cleanup_glue.go` | 136/233 (58.37%) | 130/225 (57.78%) | 97 | 95 |
| `goal_admission.go` | 252/318 (79.25%) | 240/306 (78.43%) | 66 | 66 |
| `goal_delivery.go` | 377/476 (79.20%) | 372/470 (79.15%) | 99 | 98 |
| `native_run.go` | 948/1278 (74.18%) | 936/1262 (74.17%) | 330 | 326 |
| `outbox.go` | 585/679 (86.16%) | 579/671 (86.29%) | 94 | 92 |
| `recovery.go` | 314/436 (72.02%) | 309/431 (71.69%) | 122 | 122 |
| `recovery_glue.go` | 325/487 (66.74%) | 323/485 (66.60%) | 162 | 162 |
| `supervisory.go` | 125/155 (80.65%) | 122/152 (80.26%) | 30 | 30 |
| **sum** | 3895/5214 | 3840/5150 | **1319** | **1310** |

Reading: the touched files lose 64 statement blocks (the §3 deletions) and **no touched file has
more uncovered statements than at base**; the uncovered sum drops 1319 -> 1310. The small
percentage drops are arithmetic, not lost coverage: deleting fully-covered statements from a
partially-covered file lowers `covered/total` mechanically (`(c-k)/(t-k) < c/t` while `c < t`) —
e.g. `app.go` removes exactly 4 covered statements (833 -> 829 covered, 1152 -> 1148 total,
uncovered stays 319) yet its ratio moves 72.31% -> 72.21%. The gate's intent — no line that was
covered becomes uncovered — holds for all nine files.

## 11. R9 note

No declaration moved out of `daemon/internal/app/app.go` or `cleanup_glue.go`: both files lose
declarations only (no declaration was relocated into a new file), so `SOURCE_PATHS` in
`scripts/ci/omo-control-native-evidence.py` is not touched. Gate:
`python scripts/ci/test-omo-control-native-evidence.py` — 15 tests, OK, exit 0 (§9).

## 12. Limitations

1. The delivered tree is uncommitted by rule; every sha256 in §1 binds the working-tree bytes at
   the moment of writing, not a commit id. A later verifier must re-derive them.
2. Timeouts on this host exceed CI's runner: the Windows suite needed `-timeout 1200s` (CI's own
   budget) and aborted at 601 s under the default; the WSL race leg needs more than 1200 s where
   CI's runner reports ≈386 s for the same package (parent-contract figure). A local timeout is
   not a code failure — the aborted run had no failing test and no data race (§9).
3. `apptest` compiles into normal builds (non-`_test.go` package). The advisory golangci-lint
   ratchet is measured on the touched packages only, not the whole module (§9); the whole-module
   CI metric is advisory and slow (12-minute budget in `ci.yml`).
4. The per-file fakes listed in §5c remain; only `FaultStore` was adopted by the app tests. The
   `ControlAPI`/`Process`/`EventSink` doubles currently have no app-test consumer, by the
   assertion-invariance clause and the STRICT owned file set (§5c, plan D7).
5. The §5a/§5b divergences are test-side injection-point moves with unchanged assertion outcomes;
   an independent reviewer can re-derive them with the §4/§5 regexes and the §8 mutants.
6. The scratch logs quoted in §9 (suite logs, mutant outputs, WSL log) are deleted before handoff;
   the quoted lines above are the transcript. `git status` at handoff lists only the slice's own
   files.
