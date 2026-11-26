# rf-acc-claude-stage-replay — implement plan

Contract: `goal-rf-acc-claude-stage-replay.md` (GDD state root, external to the repository).
Base revision: `78166f2ef728c66ad28a854f624118e23084447d` (#297, `main`). Branch:
`refactor/rf-acc-claude-stage-replay`. Failing evidence: Bind297r2 run `45735420588` (log raw
4413-4418; artifacts `7e8bd1e3…`/`1c4df47d…`); findings `297-binding-run-2.md`.

The accepted plan is the contract plus that evidence; this file resolves only what they leave
open: the Stage-1 root cause, the side the fix belongs to, the exact fixture semantics, the
per-clause baseline, and what is out of scope.

## 1. Base census (before any edit)

The `claude_containment` case set runs `run_native_owner_witness claude_containment` (green:
`verified TestProductionLinuxV2ClaudeNativeContainment: run=1 pass=1 skip=0`) and then
`run_kind_stage_replay_case … claude` (`linux-v2-native-acceptance.sh:2441-2447,3103-3106`), which
runs exactly one subtest: `TestProductionLinuxV2StageCrashReplayMatrix/native_release_claude`
(`production_linux_v2_native_witness_test.go:194-232`). It reds after 93.39 s at the fixture's own
post-kill wait (`production_linux_v2_owner_cli_test.go:2116-2127` at the r2 bytes; the
planning-time anchor on the base bytes it reds at is `:1958-1964`, fatal at `:1962`):

```
witness output:     production_linux_v2_native_witness_test.go:232: A helper did not exit with owner loss after the stage-replay kill: exact process pidfd remained live beyond the bounded wait
```

Which row/helper/process: the leg's held boundary is `native_release`; the pidfd is the exact
`reservation.Helper` identity (`-linux-owner-helper-v2`, pid 7168 in that run), opened by
`linuxV2OpenStageOwnerPIDFD` at `owner_cli_test.go:2077`, not the released B target (pid 7182 =
`owners.gate_pid`, captured live at `owner_cli_test.go:2083-2106`). Retained-run facts:
`claude-stage-crash-replay-native-release.json` (target `native_command_verified: true`,
`pidfd_opened: true`, `exact_leaf_membership: true`, `provider_action_succeeded: false`), and the
daemon StateDir inventory in the same log (rows 4517-4520) still shows the helper's recovery
socket `owner-a4ce2453….sock` at failure time.

## 2. Stage 1 — the root cause, named verbatim

**The helper's owner-loss stop at `native_release` succeeds, and the product leaves a helper with
a proven owner-loss stop resident for the same-RunKey recovery client; the fixture's post-kill
wait asserts the *unproven*-stop outcome, which this stage can never produce.**

Chain, all at the base revision:

1. The owner-death latch's only writer-side holder is the App process. `StartHelper` creates the
   pipe and keeps the write end as `OwnerFD` (`platform/linux_owner_anchor_linux.go:316,325,395`);
   only the read end travels (SCM_RIGHTS to the anchor, then `F_DUPFD_CLOEXEC` into the helper,
   `platform/linux_owner_helper_v2_linux.go:182-193`), both ends are `O_CLOEXEC`, and no code path
   sends, dups or `ExtraFiles`-inherits the write end (`grep` census in the record §2). The
   fixture's SIGKILL of the App therefore closes it; the helper's `watchOwnerPipe`
   (`platform/helper_v2_recovery.go:144-176`) latches owner loss. Candidate “the parent-death
   fence is not armed on the claude path” is excluded: the helper's `Pdeathsig` is armed against
   its parent, the resident anchor (`platform/linux_owner_helper_v2_linux.go:212`), by design —
   the owner-loss fence is the pipe, and it does fire.
2. With a released gate the stop is provable without the owner: the anchor registered the
   one-use gate authorization at RELEASE (`platform/anchor_gate.go:350`,
   `platform/anchor_run.go:281-283`) and holds the authorized same-PID B exec transition
   (`anchor_gate.go:441`), so `stopOwned`'s released-gate path (`platform/helper_v2_stop.go:955-1014`)
   completes: `verifyLeafMembershipBeforeStop` (`:464-693`), the anchor `StopLeaf`
   (`:974`), the exact gate wait (`:979`) and the adopted-child reap (`:984`) all succeed and the
   evidence is cached (`:1010-1014`). Candidate “the bounded wait is too short for claude's
   shutdown” is excluded as the *cause*: every step of that path is context-bounded (each with
   its own bound, census in the record §2), and the observed 90 s exceeds the product's 30 s
   helper stop bound threefold.
3. A successful stop does not end the helper. `markOwnerLost` returns nil
   (`platform/helper_v2_recovery.go:191-238`), so `requestAnchorTakeover` — the only owner-loss
   self-kill, armed exactly on an unproven stop (`:153-156,241-267`) — is not called, and the
   helper keeps serving (`platform/helper_v2_server.go:376-463`). Its listener is closed only by
   `finalizeAfterClear` (`platform/helper_v2_stop.go:1310-1320`), which only a same-RunKey
   recovery client can drive (`app/linux_owner_recovery_linux.go:1242,1274,1338,1381,1388`); the
   witness action path is fixture-only (`helper_v2_recovery.go:403-431`). Candidate “the pidfd
   targets the wrong PID” is excluded: `linuxV2OpenStageOwnerPIDFD` re-verifies the exact
   creation identity across the pidfd open, and the retained run's identity-tagged owners row
   matches.
4. The live-helper-after-owner-loss state is the product's supported behavior, not a defect: the
   owner contract states it (`docs/evidence/goal-0006-linux-owner-design-2026-09-22.md:44`: EOF is
   an owner-loss event and the helper "retains its recovery endpoint until an exact stop receipt
   and release proof are durable"; `:901`: it "survives ordinary daemon death and keeps its
   authenticated recovery endpoint until the same receipt is durable"; the Windows sibling
   `platform/supervisor_windows_helper.go:597-598`), the recovery has a dedicated live-helper
   branch (`app/linux_owner_recovery_linux.go:1242`, `1271-1292`, `1337-1340`) whose retirement
   awaits the helper's pidfd exit (`platform/helper_v2_recovery.go:544`,
   `platform/anchor_types.go:21-25`), and the daemon-crash witness *requires* the residency — the
   helper must survive the daemon SIGKILL
   (`production_linux_v2_native_witness_test.go:2553-2556`) and the recovered tombstone's stop
   proof must be helper-issued with no helper exit (`:2628-2632`).
5. The fixture contradicts that at every helper stage: on the base bytes the pre-recovery wait
   (`owner_cli_test.go:1958-1964`; the r2 block is `:2116-2127`) supplies the wait that fails on a
   live helper after 90 s with the message quoted above. At the pre-release stages the same wait
   passes because there the stop is unprovable (the anchor has no gate authorization and no
   authorized exec transition), `stopOwned` errors, and the helper self-kills; the released-gate
   stages (`native_release`, `stop_receipt_durable`) are exactly the ones whose stop is proven,
   so the wait can never be satisfied there. Candidate “the claude process tree outlives the
   owner” is excluded: no step of the owner-loss path is harness-specific, and the retained
   artifact binds the claude target as the single exact leaf member
   (`exact_leaf_membership: true`, `native_command_verified: true`). The class has a second
   observed instance on `origin/main` `ff0975a7`: the conductor's measurement run `45775969711`
   reds the codex kind leg `native_release_codex` with the same message
   (`findings/main-measure-codex-synthetic.md`).

Not a production defect in `daemon`/`execution` code: the product's own recovery design (item 4)
requires this state. The fix therefore belongs on the fixture side (Stage 2), and the product-side
alternative is recorded as rejected in §3.3.

## 3. The fix

### 3.1 Fixture semantics chosen

At the two stages whose durable boundary carries the held helper's owner-loss evidence —
`native_release` with the held marker's `released` state, `stop_receipt_durable` with the durable
receipt (both already asserted by `linuxV2ValidateStageJournalBoundary`,
`owner_cli_test.go:1639-1740`) — the leg no longer requires the helper to exit *before* the
recovery; fix round r2 (contract ruling R1/R5) required instead that the owner-loss path itself
stopped the target *before* the recovery client starts. **Round 3 replaces those two r2 arms**
(the `native_release` target-pidfd arm measured only containment, because the claude target
self-exits on the App's stdin EOF; the `stop_receipt_durable` fixture dial was refused or stole
the helper's single post-loss recovery slot — DS audit P1-1/P1-2): `native_release` keeps only a
target-containment wait (`linuxV2RequireTargetContainedBeforeRecovery`), and both stages require
the *recovery client's* own pre-stop snapshot of the held helper's `OwnerLost` latch and cached
helper-issued stop proof (`linuxV2RequireOwnerLossSnapshotBeforeRecovery`, field
`owner_loss`), taken in the witness seam before `recoverContainmentOnlyJournals` — §8. The branch
is keyed on the stage, not the harness: both stage-replay kind legs (`native_release_codex`,
`native_release_claude`) present `barrierStage == "native_release"` to the same runner
(`production_linux_v2_native_witness_test.go:194-197`, `199-205`), and the matrix now passes the
leg's own `testCase.barrierStage` (round 3, DS P3-2) so no second literal can drift; the pin
`TestLinuxV2StageReplayKindLegsShareTheProvenReleaseBranch` covers both legs. In that state:

- the pre-recovery helper-exit wait with its unchanged 90 s bound, unchanged message and
  unconditional fatal stays for every other helper stage; at the two proven stages it is deleted
  rather than tolerated, so no error class can be swallowed (ruling R2 is obviated: no tolerance
  remains on any pre-recovery wait);
- at the two released-gate stages the leg proceeds to the same-RunKey recovery, and then requires
  the helper's exit within 30 s (the recovery's `ReleaseAfterClear`+retire is what ends a resident
  helper: `linux_owner_recovery_linux.go:1381-1388`), so the leg still ends with a hard
  helper-exit assertion in every branch; that poll is defence-in-depth, because the recovery only
  reports `released` after its own `RetireHelper` observed the helper's pidfd exit inside the same
  30 s anchor bound, and a recovery that cannot retire the helper exits non-zero and reds the leg
  before the poll (review finding STD-A02, fix round r1);
- fix round r1 (review finding STD-A01): at those same two stages the recovered release tombstone's
  stop receipt must be the held helper's own released-gate proof — `Kind == helper`, the held
  helper as both `Issuer` and `Reaper`, `HelperExit == nil`
  (`linuxV2RequireHelperIssuedReleaseStopProof`, `owner_cli_test.go:1175-1187`, consulted at
  `:2170-2175`) — so a regression that routes these stages through the anchor-reap mirror path
  (mirror `Kind`, anchor `Reaper`, set `HelperExit`) reds the leg explicitly at both stages
  (`native_release` had no provenance pin before r1; `stop_receipt_durable` was already chained
  through its receipt-equality block, record §3);
- the recovery response, the RunJournal tombstone's physical stop flags, the leaf-absence check
  and the post-recovery target-pidfd wait (`owner_cli_test.go:2195-2203`) keep their exact
  strength, so the owner-loss physical-stop and replay assertions are unchanged outside the two
  proven stages.

### 3.2 The decisions are pure, pinnable predicates

`linuxV2StageReplayOwnerLossSnapshotRequired(stage, marker, journal)` carries the stage decision
so it can be pinned without the installed guest; the r1
`linuxV2RequireHelperIssuedReleaseStopProof(helper, file, found)` (record §3/§4) carries the
proof-provenance decision the same way, and round 3's
`linuxV2RequireOwnerLossSnapshotBeforeRecovery(snapshot, stage, held, want)` carries the
pre-recovery owner-loss decision from the recovery client's snapshot field, with
`linuxV2RequireTargetContainedBeforeRecovery` (the real-pidfd containment wait) as its
`native_release` companion — so every arm is pinned by tables and byte mutants without the guest.
On the product side the two pure predicates `linuxV2NativeWitnessOwnerLossStage` and
`linuxV2NativeWitnessOwnerLossExpected`, the summary-shape validator and the injectable
seam-ordering variables (record §8) carry the snapshot's own decisions into
`linux_owner_witness_linux_test.go`. The runner keeps only the call sites, and the predicates are
consulted *after* the exact durable shape has already been validated
(`linuxV2ValidateStageJournalBoundary` / `linuxV2ValidateStageReplayJournal`), which is why they
can key on the marker state / receipt presence.

### 3.3 Rejected alternative (product side)

Make a proven-stop owner-loss path end the helper itself (self-kill after publishing its evidence)
so the pre-recovery wait holds everywhere. Rejected: it contradicts the product's own recovery
design and the daemon-crash witness (record §2 item 4), it would need a new anchor-retained
evidence channel plus e2e changes in files this goal does not own, and the contract routes
production changes outside the witness seam to DECISION_NEEDED. Recorded, not silently dropped:
if the conductor reads the residency as a product defect, that is a new goal with this plan's
§2/§3.3 as its diagnosis.

## 4. Public/API shape

No validator, harness or fixture-script change; no new exports. Unexported predicates and their
table rows in `daemon/e2e/production_linux_v2_owner_cli_test.go` (r1's proof-provenance predicate;
the owner-loss snapshot predicate with its containment companion), plus the runner sequencing
above. Round 3 additionally touches the owned witness seam
`daemon/internal/app/linux_owner_witness_linux.go` (the snapshot type, its stage/expected
predicates, the bounded read, the summary field and validator, and the two injectable seam
variables) and its unit rows in `linux_owner_witness_linux_test.go` — production bytes inside the
contract's owned set; no `platform/**`, `authority/**` or `execution/**` change. Nothing else in
the module changes.

## 5. Per-clause baseline and proof plan

| Contract clause | Base behavior (before) | Clause test / proof |
| --- | --- | --- |
| 1 root cause named with source + log evidence | the retained red at the base `owner_cli_test.go:1958-1964` (r2 delivery: the pre-recovery block `:2116-2127`); the four candidates | plan §2 with file:line; the record's census rows; the WSL scratch residency probe (unproven→success control flow): a helper with cached stop evidence returns from `markOwnerLost` without self-kill and without closing its listener |
| 2 fix on the wrong side, no lengthened bound | the wait is 90 s > the 30 s product bound; the released-gate stages can never satisfy it | the predicate row (`TestLinuxV2StageReplayOwnerLossSnapshotRequired`, 13 rows covering all seven non-proven helper stages) plus the r1 proof-provenance row, the round-3 rows (`TestLinuxV2OwnerLossSnapshotBeforeRecovery`, `TestLinuxV2StageReplayKindLegsShareTheProvenReleaseBranch`) and the witness-seam rows (`TestLinuxV2NativeWitnessOwnerLossStage`, `…SnapshotShape`, `…SnapshotPrecedesRecovery`), all with their `go test -overlay` mutants red (record §8), and the runner branches; the 90 s bound now guards only the unproven stages it was sized for, and the snapshot's 40 s wait outlasts the product's own 30 s stop bound (`helper_v2_protocol.go:31-33`) without changing any product bound |
| 2 never weaken the owner-loss/containment/replay assertions | — | the physically unchanged validators: `linuxV2ValidateStageReplayResponse` (`:1144-1150`) / `…ResponseState` (`:1319-1453`), `…StageReplayJournal` (`:1455-1561`), the post-recovery target-pidfd wait; the pre-recovery wait at the two proven stages is replaced by the recovery client's pre-stop snapshot (owner-loss latch, the held helper's own helper-issued stop proof, equality to the durable receipt proof) with the `native_release` target wait kept as containment only; the unproven stages keep the byte-identical fatal; the r1 provenance pin keeps its strength at the same two stages; record §8 carries the assertion mapping and the honest limits |
| Invariants (≤2-line comments, `go vet`, no host-layout tests; standing conductor rules 2026-11-26) | — | comment audit; `go vet`; the rows use only struct literals; the comment cap runs on the scratch-clone commit of the delivered code, and record §8 carries the assertion-removal mapping (each removed guarantee → surviving/new check + red mutant) and the no-leg-order/no-shared-state statement |
| Acceptance (conductor Bind: the stop is gone) | — | carried to the conductor; this stage records the local rows |

## 6. Local gates

From `daemon/`: `gofmt -l .`; `go vet ./...`; the FULL `go test ./...` on Windows and in WSL
Ubuntu-24.04 (sequenced, never beside another harness); the predicate row alone in WSL; the
predicate rows and the round-3 rows (`TestLinuxV2OwnerLossSnapshotBeforeRecovery`,
`TestLinuxV2StageReplayKindLegsShareTheProvenReleaseBranch`) plus the witness-seam rows
(`TestLinuxV2NativeWitnessOwnerLossStage`, `…SnapshotShape`, `…SnapshotPrecedesRecovery`) in WSL;
the mutants (the seam order reversed, the app stage widening, the dropped app shape check, the
dropped latch / proof-shape / receipt-equality / helper-identity / released-gate / clean-capture
terms, the required-predicate widening) via `go test -overlay`, never written into the tree; the
comment-cap script before the final re-measure; the record sanitizers on the new records. Scratch
work lives in the operator's scratch root, never in the worktree.

## 7. Out of scope (with reasons)

- Any `daemon/internal/platform/**` or `execution/**` change (not owned; §3.3 records the option).
- The `stop_receipt_durable` and remaining released-gate legs' *next* stops after this wait (for
  example the pre-checkout clone-retention reconciliation): the conductor's Bind records them
  verbatim; this goal removes only the asserted stop.
- The `shared` set's pre-existing reds and the `codex_containment` leg's own next stop: reported,
  never repaired here.
- The `checkout_ready` helper stage's pre-release self-kill remains unestablished (DS audit P3):
  no case set runs it with a helper-exit assertion any record documents, so the kept 90 s fatal is
  a latent false red if the stop is provable there; recorded as a `docs/TODO.md` residual, not
  repaired here.

## 8. Fix round r3 — the recovery client's pre-stop snapshot (contract Z1-Z5)

Round 3 replaces the r2 pre-recovery arms with the only reader that can legitimately reach the
helper after owner loss: the recovery client process itself.

- Premise, verified from code at the r3 base (this was the round's STOP-condition check; it holds):
  the witness process that runs `recoverLinuxV2NativeWitnessRunKey`
  (`linux_owner_witness_linux.go:2852`) and the process that later runs the production
  `LookupHelper` are the same process — `recoverContainmentOnlyJournals` →
  `recoverLinuxV2OwnerJournal` are called synchronously inside it — and `dialLinuxOwnerHelperV2`
  stamps the caller's own `{PID, ProcessIdentity}` (`helper_v2_protocol.go:356`). The helper admits
  the first post-loss non-anchor peer and then any connection from that same peer identity
  (`helper_v2_server.go:503-508`), while the pre-loss gate refuses other peers (`:497-500`); the
  anchor's `handleLookup` is stateless and re-dups the run pins per call
  (`anchor_session.go:248-317`). The snapshot's `LookupHelper` therefore claims the single
  `recoveryOwner` slot *as the recovery client* and the recovery's own lookup is admitted by
  `recoveryOwner.Equal(peerIdentity)` — no slot the recovery needs is consumed. `status` is
  allowed post-loss and read-only (`helper_v2_server.go:575-585` region, `handleStatus` `:646-742`).
- The seam captures, before the recovery's first `Control.Stop`, a bounded `LookupHelper` +
  `Control.Status` poll (40 s; outlasts the product's 30 s owner-loss stop bound,
  `helper_v2_protocol.go:31-33`) and records `OwnerLost`, the helper-issued `StopProof`, the
  released-gate authorization and the held helper identity in the summary's new `owner_loss` field
  (shape validator + unit rows).
- The fixture requires the snapshot at both proven stages
  (`linuxV2RequireOwnerLossSnapshotBeforeRecovery`): `native_release` additionally requires
  `GateReleaseAuthorized` and equality to the final receipt proof; `stop_receipt_durable` requires
  equality to the durable receipt proof. The target-pidfd wait stays as a containment check only.
- Deleted: the r2 fixture dials (`linuxV2ReadHeldHelperStatus`,
  `linuxV2WaitHeldHelperOwnerLostStopProof`, `linuxV2RequireHeldHelperOwnerLostStopProof`,
  `linuxV2RequireOwnerLossStopBeforeRecovery`) and the pre-existing pre-kill
  `linuxV2CaptureNormalAnchorStopEvidence` with both call sites (stage matrix and helper
  response-loss runner).
- Honest limits (record §8): the snapshot is witness telemetry, its trust root the recovery
  process's own response (weaker than a fixture dial); its ordering is structural — the capture
  precedes the recovery invocation, pinned by the owned ordering row — not proved in-band by the
  helper; at `stop_receipt_durable` the cached proof predates the kill, so the claim is latch +
  retention + receipt identity, not a new stop; the product-level skip-`stopOwned` mutant's
  end-to-end red needs the privileged guest.
- DS P3-2 is fixed: the matrix now passes `testCase.barrierStage` into the runner (no second
  literal), and the kind-leg pin covers `native_release_codex` and `native_release_claude`.
