# rf-w4-22 response-loss seal consumer — plan

Contract: `goal-rf-w4-22-response-loss-seal-consumer.md` (GDD state root, external to the
repository), with the 2026-11-21 arch-decision-2 rulings (R1, R14), the verification amendments, the
program-wide conductor rules and the 2026-11-22 comment policy binding over the earlier text. Base
revision: `e13450bc03499a62e77b125ac02596cdb433ce05` (#185 `4b034f50` and W4-12c `e13450bc`/`#263`
both merged; dependencies satisfied). Branch: `refactor/rf-w4-22-response-loss-seal-consumer`.
Owned files are the contract's strict set, re-derived in §1, plus this plan and
`docs/evidence/rf-w4-22-response-loss-seal-consumer-2026-11-23.md`.

Goal shape: the in-process `(*linuxSupervisor).recoverResponseLossStop` consumer records the
helper's authenticated owner-loss terminal scan seal through one new `execution.Recorder` method
before it freezes the daemon mirror and adopts the stop receipt, so a live-owner response-loss stop
completes with the exact `Completed+1` durable helper seal instead of refusing in
`assert_all_sealed`. Exactly one durable write is added, at one position; the absent-write and
seal-write-failure paths stay fail-closed; the ten frozen OpenCode members, the helper side, the
Windows client and every other durable order are untouched.

## 1. Owned-file re-derivation at dispatch

| Contract entry | HEAD reality | Action |
| --- | --- | --- |
| `platform/supervisor_linux_client.go` `recoverResponseLossStop` `:862` | `:852`; the success arm calls `verifyMirrorObservation` then `freezeMirrorObservation` (`:1232`) and discards the helper's seal sequence (`:871`) | record the returned sequence through the port between the mirror seal and the all-sealed assertion |
| `platform/supervisor_linux_helper.go` comment `:940-951` | the helper loop and `enterRecovery` moved to `platform/supervisor_session.go` (`:375-383`); the 20-line #185 comment was shortened by the 2026-11-22 comment-policy sweep, and nothing in the surviving 2 lines claims the deferred amendment | no helper-side edit; the move is recorded as the re-derivation |
| `platform/containment_supervisor_linux_test.go` pin `:3380` | pin `:3334-3460`, its journal model `:3256-3332` | rewrite the pin (success + durable seal + order), add the seal-write-failure arm, extend the model with the store's `RecordSupervisorRecoveryScanSeal` rules |
| `state.go:3786` (doc `:3784-3785`) | `state/containment_journal.go:390` (`RecordSupervisorRecoveryScanSeal`), `state/run_journal.go:222` (`RecoveryHelperScanSealEligible`) | read-only citations; no state edit |
| `execution` Recorder method + `process_recorder.go` | port at `execution/recorder.go:47`; the launch spec at `execution/process_launcher_linux.go:37-46`; the app factory at `app/process_recorder.go:11`; the store seam test double at `app/apptest/faultstore.go` | add the eleventh member, its capability, the launcher plumbing, the app factory and the FaultStore hook |
| port member enumerations | `execution/recorder_test.go:70-145`,`:255-375`; `app/process_recorder_test.go:23-35` | extend the enumerations (a member added to the port breaks them otherwise) |
| `docs/TODO.md` entries | sticky-refusal bullet (in-process consumer) `:780-790`; comment-policy bullet `:1450-1454` | both describe exactly this debt; close both into `docs/archive/CLOSED-TODO.md` |

## 2. Public/API shape

```go
// execution
type RecorderCapabilities struct { RecordSupervisorRecoveryScanSeal bool /* + the ten */ }
type Recorder interface { RecordSupervisorRecoveryScanSeal(authority.Supervisor, uint64) error /* + the ten */ }
type RecorderFuncs struct { RecordSupervisorRecoveryScanSeal func(authority.Supervisor, uint64) error /* + the ten */ }

// platform
type LinuxSupervisorStartSpec struct { RecordRecoveryScanSeal func(authority.Supervisor, uint64) error /* + Prepare, Bind */ }
```

- The method binds the run key in the app (`processRecorder(key, …)`); its arguments are exactly the
  store's `expected authority.Supervisor` CAS value and the helper's returned seal sequence.
- `processRecorder` adds `RecordSupervisorRecoveryScanSeal: daemon.recordSupervisorRecoveryScanSealCallback(key)`,
  a thin factory over the existing restart-consumer implementation
  `daemon.persistSupervisorRecoveryScanSeal` (one exact replay, same wrapping) — the same shape as
  `recordSupervisorHandoffStopReceiptCallback`.
- The launcher passes `writes.RecordSupervisorRecoveryScanSeal` into the spec; the supervisor copies
  it into `recoveryScanSeal` and calls it from a new `recordRecoveryScanSeal(sequence)` step.
- A nil function is an absent capability: the step is skipped and the all-sealed assertion refuses
  exactly as at base. This is the OpenCode-rebuilt-invocation path (the frozen members cannot carry
  the new write); the launch stays valid and its response-loss stop stays fail-closed until W6-04.

## 3. Edge-case semantics the contract demands

1. **Write position (as-built amendment).** Contract Design 2 says "before the mirror freeze and
   before adopting the receipt". The literal position before `freezeMirrorObservation()` is
   infeasible: `state.Store.RecordSupervisorRecoveryScanSeal` refuses without a durable mirror seal
   (`state/containment_journal.go:413-415`, pinned by
   `TestLinuxRecoverySealRequiresCleanHelperProofAndPriorMirrorSeal`), and the mirror's durable seal
   *is* `freezeMirrorObservation`'s third step. Smallest contract-preserving amendment: record the
   seal after the mirror's durable seal and before the durable `assert_all_sealed` assertion — both
   inside the freeze region and strictly before the receipt adoption, which is the binding
   requirement in clause 1 and the target of the "write the seal after the receipt/mirror freeze"
   mutant. Implemented by splitting `freezeMirrorObservation` into `sealMirrorObservation`
   (monitor stop, barrier restore, durable mirror seal, local assert) and the existing
   `assertDurableScanStateSealed`, with `freezeMirrorObservation` = both (the live path's call
   sequence is unchanged) and `recoverResponseLossStop` calling the three steps explicitly.
2. **One new durable write, one position.** The recovery arm's durable writes are
   `verifyMirrorObservation` (assert_clean) → mirror seal → recovery scan seal → assert_all_sealed.
   No other durable write, order, argument or error string changes; `stop()`'s live sequence is
   byte-identical.
3. **Seal-write failure.** `recordRecoveryScanSeal` returns the write error; the caller keeps its
   existing wrapper (`response-loss mirror freeze: %w`) and fires the containment-unproven callback
   exactly once, so the stop stays fail-closed with the same sentinel class
   (`ErrLinuxSupervisorStopUnproven` + `ErrLinuxSupervisorResponseLost`), no adopted receipt and no
   durable advance. "Same error text" is read as that unchanged surface; the inner cause names the
   failed write (the alternative — swallow the error and let the assertion refuse — would
   misattribute the cause; both are fail-closed, and for the *absent* capability the byte-identical
   base refusal is preserved). Recorded as a decision, not a silent deviation.
4. **Crash windows.** Seal recorded, crash before the receipt: the durable state is legitimately
   sealed (mirror + helper), so a restarted daemon either takes the plain
   `terminatePersistAuthority` route (helper alive, `RecoveryHelperScanSealEligible` is false
   because the helper is sealed) or, on an exact replay of the same proof, the store's idempotent
   no-op branch; a second in-process stop replays the exact proof idempotently. Crash before the
   seal: today's restart path (`RecoveryHelperScanSealEligible` → `terminate…WithScanSeal` →
   `RecordSupervisorRecoveryScanSeal`). Both are tabled with their durable states in the evidence
   record and pinned by the store's own tests plus the rewritten pin.
5. **Pin.** `TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed` flips to
   `TestLinuxSupervisorLiveOwnerControlCloseInProcessStopRecordsSeal` (rename recorded with the old
   name): the same live-owner control-close scenario now asserts a successful stop, zero unproven
   callbacks, the adopted receipt, the durable state (helper `completed=2, sealed=2`; mirror
   `1/1`), exactly one seal write with the exact authority and `Completed+1`, the ordered event tail
   (`mirror:seal:1`, `recovery-scan-seal:2`, `mirror:assert_all_sealed:1`) and no refused
   assertion. A subtest with a failing seal write pins §3.3. The pin's journal model gains the
   store's recovery-seal rules (authority CAS, prior mirror seal, exact `Completed+1`, idempotent
   replay, no unproven journal).

## 4. Evidence plan

- Red first: mutant m1 (drop the write) is exactly base behaviour on this path; m2 (write after the
  mirror freeze's assertion), m3 (`Completed+2`) each kill the pin; recorded with first red lines.
- Durable-write order table (base vs HEAD) and crash-window table from the model's event log and the
  state rules; state-level idempotency/replay from the existing store tests (re-run).
- Gates: `gofmt -l .`, `git diff --check`, `go vet ./...`, `GOOS=linux|windows|darwin go build ./...`,
  WSL `go test -count=1 -race ./internal/platform/... ./internal/app/... ./internal/execution/...`,
  Windows `go test -count=1 ./internal/platform/... ./internal/app/...`, R9
  `python3 scripts/ci/test-omo-control-native-evidence.py`.
- Durable-transition review record plus an independent review (contract clause 3): the implement
  stage dispatched both as read-only reviews of the working tree (evidence record §10), and the
  gdd review stage stays the independent reviewer of record.

## 5. Out of scope (with reason)

- Windows supervisor client; splitting `supervisor_{linux,windows}_client.go` (R14).
- The lease-expiry sticky-refusal arm (`docs/TODO.md`, separate increment).
- `sealDescendantScanAfterMonitorStop` retry semantics (separate TODO entry).
- Frozen OpenCode members / `MirrorRecorderMembers` (E3; W6-04 deletes them) — adding the new write
  to the mirrors would resurrect a second carriage for a non-legacy write.

## 6. Risks

- The in-process authority must Equal the journal's CAS value; the rewritten model checks the CAS
  explicitly (not just the sequence), and the platform value is the same one the app persisted at
  commit (`handoff.ToSupervisor()` at `state/supervisor_handoff_journal.go:379`).
- A response-loss stop whose helper response already carried a durable helper seal is untouched
  (the store's idempotent branch); no test path depends on it.
- The platform suite is Linux-only; the Windows evidence is vet/build plus the app/execution suites.

## 7. As-built note (2026-11-23, implement stage, after the two independent reviews)

The plan's §3.3/§3.5 pin shape became four arms over one scenario helper (the rewritten pin, the
failing seal write, the absent write, and a resumed helper's non-2 terminal seal), plus a new
Linux-only carrier test `daemon/internal/execution/process_launcher_linux_test.go` over the
extracted `linuxSupervisorStartSpec`. The reviews' accepted findings added the app-side
store-lifetime guard on the new callback, the second-`Terminate` retry assertions, and the
corrected replay wording in the evidence record; the two P2 control gaps they found (a constant
sequence and a deleted launch carrier surviving every other test) are closed by mutants m5/m6. The
plan's mutant list (m1-m3) is therefore m1-m6 in the record.
