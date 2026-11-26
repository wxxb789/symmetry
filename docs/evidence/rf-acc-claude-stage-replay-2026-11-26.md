# rf-acc-claude-stage-replay — evidence record

Goal `rf-acc-claude-stage-replay` (contract: the goal file under the GDD contracts root; conductor
2026-11-26). Branch `refactor/rf-acc-claude-stage-replay`, base
`78166f2ef728c66ad28a854f624118e23084447d` (#297, `main`). Plan:
`docs/plans/2026-11-26-rf-acc-claude-stage-replay-plan.md`. All paths are relative to the repository
or to the GDD state root. The goal's acceptance row (the conductor's Bind runs `claude_containment`
once on the PR head) is the conductor's; this record carries the Stage-1 root cause, the fix and
the local rows. Every scratch file cited below lives outside the repository, in the stage's own
scratch dir under the GDD scratch root (file names only, as cited); the host restart wiped the
first WSL-side copy, so the probe source and the mutant copy were rebuilt there and re-run on the
final bytes.

## 1. Delivered files and digests

| file | sha256 at the delivered r2 bytes |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `3c8759c4aa83ddea43e542edff78d75f7c798c3ec1102e04f8cfe2b0885a028d` |
| `docs/plans/2026-11-26-rf-acc-claude-stage-replay-plan.md` | `9dc0667b966cdd3568fc744e73c9c3ba383056065b9c2071c2f51b198078df40` |

`docs/TODO.md` and `docs/archive/CLOSED-TODO.md` are deliberately not hashed anywhere. No production,
validator, fixture-script or `pi/**` byte changed (§4).

Fix round r3 changes the run inputs, so the delivered r3 bytes have their own table (the four
record-only docs are not run inputs):

| file | sha256 at the delivered r3 bytes |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `0d2aa2a252dff51e47f697a8f68a17a90784cbfba2c8688f52d4a2e96c72e5a5` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `db47b73835b3e8bd4797caf0499aa8797e0a9e18220d5de01af3f8b8a7f1df7d` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `bb0587666f8dffb7e0970d11a6ca1a7984ca60b20b3b13cf7efe8e9082c7980a` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `0755eab396b9040d855faa3f130353eba8a102c3b971c0ba950eefd3e506c463` |
| `daemon/internal/app/journal_store_linux_test.go` | `bb0b8e8eedb9627e8e38a0016601f12fbca8a64808abf1efae645f1293cc24e6` |
| `docs/plans/2026-11-26-rf-acc-claude-stage-replay-plan.md` | `6c08587f48b66368d73a5666d93cce200947e142dd6129231df68f7aeebaeee6` |

`linux_owner_witness_linux.go` and `journal_store_linux_test.go` are the contract's owned witness
seam and its callers; no `platform/**`, `authority/**` or `execution/**` byte changed (§8).

## 2. Stage 1 — the root cause, named verbatim

**The helper's owner-loss stop at `native_release` succeeds, and the product leaves a helper whose
stop is proven resident for the same-RunKey recovery client that retires it; the fixture's post-kill
wait asserts the *unproven*-stop outcome, which the released-gate stages can never produce.**

Retained rows (Bind297r2 run `45735420588`, raw log rows 4413-4418; artifacts
`7e8bd1e3…`/`1c4df47d…`; `findings/297-binding-run-2.md`):

```
witness output: === RUN   TestProductionLinuxV2StageCrashReplayMatrix
witness output: === RUN   TestProductionLinuxV2StageCrashReplayMatrix/native_release_claude
witness output:     production_linux_v2_native_witness_test.go:232: A helper did not exit with owner loss after the stage-replay kill: exact process pidfd remained live beyond the bounded wait
witness output: --- FAIL: TestProductionLinuxV2StageCrashReplayMatrix/native_release_claude (93.39s)
```

Which row, which helper, which process: the held row is the claude kind leg
`native_release_claude` (`production_linux_v2_native_witness_test.go:195-233`; the `claude_containment`
case set runs exactly this subtest, `linux-v2-native-acceptance.sh:2541-2547,3204-3207`). The pidfd
is `marker.Owners.Helper` opened at `production_linux_v2_owner_cli_test.go:1947`
(`linuxV2OpenStageOwnerPIDFD` re-verifies the exact creation identity across the open) — the
`-linux-owner-helper-v2` process (pid 7168 in that run), **not** the released B target (pid 7182,
captured live at `:1953-1976`; artifact `claude-stage-crash-replay-native-release.json` shows it as
`native_command_verified: true`, `pidfd_opened: true`, `exact_leaf_membership: true`,
`provider_action_succeeded: false`). The same run's daemon StateDir inventory (raw rows 4517-4524;
the `s owner-….sock 0` row is 4524) still lists the helper's recovery socket
`owner-a4ce2453df6d2414432b3b1e30d4714b1a4d724ee01cfc79a33fe5ea0d55797c.sock` at failure time;
that hash is the stage-replay leg's RunKey digest, recomputed as
`sha256(runID + "\x00" + generation)` from `6aabe8c4-9f8c-4c55-9524-e73edc294c89`/`1`
(`state/state.go:465-469` names the journal, `app/native_run.go:146-153` the socket), i.e. the
helper never reached `serve()`'s socket-unlink exit path.

Chain, all at the base revision:

1. **The owner-death latch fires.** `StartHelper` creates the pipe and keeps the write end as
   `OwnerFD` (`platform/linux_owner_anchor_linux.go:316,325,395`); only the read end is transferred
   (SCM_RIGHTS at `:325`, then `unix.FcntlInt(…, F_DUPFD_CLOEXEC, 0)` at
   `platform/linux_owner_helper_v2_linux.go:182-193`), both ends are close-on-exec (`os.Pipe()`), and
   the write-end census over the non-test tree shows every other mention is a `Close` on the App-side
   handle (scratch `record-censuses.txt`). The App's SIGKILL therefore closes the last writer; the
   helper's `watchOwnerPipe` (`platform/helper_v2_recovery.go:144-176`) reads EOF and latches owner
   loss. Contract candidate “the parent-death fence is not armed on the claude path” is excluded:
   the helper's `Pdeathsig` is armed against its parent, the resident anchor
   (`platform/linux_owner_helper_v2_linux.go:212`), by design — the owner-loss fence is the pipe,
   and it fires.
2. **With a released gate the stop is provable without the owner.** At `native_release` the App has
   driven the one-use gate RELEASE: the anchor registered the authorization
   (`platform/anchor_gate.go:350`; fence equality `platform/anchor_run.go:281-283`) and holds the
   authorized same-PID B exec transition (`anchor_gate.go:441`). The helper's released-gate stop
   path therefore completes: `verifyLeafMembershipBeforeStop` (`platform/helper_v2_stop.go:955` →
   `:464-693`), the anchor `StopLeaf` (`:974`), the exact gate wait (`:979`), the adopted-child reap
   (`:984`), and the evidence is cached (`:1010-1014`). Contract candidate “the bounded wait is too
   short for claude's shutdown” is excluded as the cause: every wait on that path is
   context-bounded (helper stop `platform/helper_v2_protocol.go:31-33`, anchor waits
   `platform/anchor_types.go:21-25`, leaf empty/pidfd polls with per-iteration `ctx.Err()`), and the
   fixture's 90 s bound is three times the product's 30 s helper stop bound.
3. **A successful stop does not end the helper.** `markOwnerLost` returns nil
   (`platform/helper_v2_recovery.go:191-238`), so `requestAnchorTakeover` — the only owner-loss
   self-kill, called exactly when `markOwnerLost` returns an error (`:153-156`) or on the signal
   path — is not reached (`:241-267`); the helper keeps serving
   (`platform/helper_v2_server.go:376-463`). Its listener is closed only by `finalizeAfterClear`
   (`platform/helper_v2_stop.go:1310-1320`), which only the same-RunKey recovery client can drive
   (`app/linux_owner_recovery_linux.go:1242,1274,1338,1381,1388`); the witness-action exit is
   fixture-only (`helper_v2_recovery.go:403-431`). Contract candidate “the pidfd targets the wrong
   PID” is excluded: `linuxV2OpenStageOwnerPIDFD` re-verifies `platform.ProcessIdentity` before and
   after the pidfd open and matches `fdinfo`'s PID, and the retained `owners` row is
   identity-tagged (`linux:v2:…:7168:34900`).
4. **The live-helper-after-owner-loss state is the product's supported behavior.** The owner
   contract states it (`docs/evidence/goal-0006-linux-owner-design-2026-09-22.md:44`: EOF is an
   owner-loss event, and the helper “retains its recovery endpoint until an exact stop receipt and
   release proof are durable”; `:901`: it “survives ordinary daemon death and keeps its
   authenticated recovery endpoint until the same receipt is durable”; the Windows sibling
   `platform/supervisor_windows_helper.go:597-598` states the same rationale), and the production
   retirement chain implements it: the recovery has a dedicated live-helper branch that asks the
   resident helper for its exact stop evidence (`app/linux_owner_recovery_linux.go:1242`,
   `1271-1292`), then ACKs the receipt, releases the helper after the durable clear and retires it
   (`:1337-1340`, `:1381-1388`); the anchor answers `retired` only after `run.waitHelper` observed
   the helper's pidfd exit under the 30 s bound
   (`platform/helper_v2_recovery.go:490-567` esp. `:544`, `platform/anchor_types.go:21-25`), the
   helper's listener is closed only by `finalizeAfterClear`
   (`platform/helper_v2_stop.go:1310-1320`) and `serve()` returns on `isFinalized`
   (`platform/helper_v2_server.go:449-452`). The daemon-crash witness *requires* the residency —
   the helper must survive the daemon SIGKILL
   (`production_linux_v2_native_witness_test.go:2553-2556`) and the recovered tombstone's stop proof
   must be helper-issued with no helper exit (`:2628-2632`, `proof.Kind == helper`,
   `proof.HelperExit == nil`).
5. **The fixture contradicts that at exactly the released-gate stages.** The r1 post-kill wait
   (`production_linux_v2_owner_cli_test.go:2116-2127` at the r2 bytes; the base anchor is
   `:1958-1964`, where the same 90 s wait fatals unconditionally) holds for
   every stage in its helper list (`:2074-2076`). At the pre-release stages the same wait passes
   because there the stop is unproven (no gate authorization, no authorized exec transition), so
   `stopOwned` errors and the helper self-kills — the guest history records exactly that
   (`docs/evidence/rf-acc-owner-loss-semantics-2026-11-25.md` §8/M12: the pre-release fresh-request
   leg observes the helper's owner-loss exit at 0.23 s in green runs). At `native_release` and
   `stop_receipt_durable` — the two stages whose durable boundary proves the released gate
   (`linuxV2ValidateStageJournalBoundary:1655-1660` for `native_release`, `:1714-1717` for
   `stop_receipt_durable`) — the stop is proven, so the wait can never
   be satisfied. Contract candidate “the claude process tree outlives the owner” is excluded: no
   step of the owner-loss path is harness-specific, and the retained artifact binds the claude
   target as the single exact leaf member. The class has a second observed instance on `origin/main`
   `ff0975a7`: the conductor's measurement run `45775969711` (findings
   `main-measure-codex-synthetic.md`) reds the codex kind leg `native_release_codex` with the same
   message at `production_linux_v2_native_witness_test.go:233`, and both kind legs
   (`native_release_codex`/`native_release_claude`) feed the same runner with
   `barrierStage == "native_release"` (`:194-197`, `:199-205`, `:233`), so the fix below covers
   both by construction.

`[INFERENCE]` on one link: the retained artifacts do not carry the helper's in-memory state, so the
r1 record inferred “the stop succeeded” from the code paths in items 2 and 3 plus item 4's product
requirement. Fix round r3 supersedes the inference with the recovery client's own pre-stop
snapshot at both stages (§8): the witness process reads the held helper's `OwnerLost` latch and
its cached helper-issued stop proof before the recovery's first `Stop`. §8 states the snapshot's
limits honestly (witness telemetry; structural ordering). The old 90 s survival itself already
bounds the alternative reading: an *unproven* owner-loss stop returns an error and self-kills the
helper through `requestAnchorTakeover` within the 30 s `linuxOwnerHelperV2StopTimeout`
(`platform/helper_v2_recovery.go:153-156,191-238,241-267`), so a helper still resident past the
old wait cannot be a hanging stop.

Not a production defect in `daemon`/`execution` code outside the witness: the product's recovery
design (item 4) needs this state, so Stage 2 fixes the fixture side. The rejected product-side
alternative (make a proven-stop helper end itself) is recorded in the plan §3.3.

## 3. The fix

- `production_linux_v2_owner_cli_test.go` gains one pure predicate,
  `linuxV2StageReplayHelperStopIsProven(stage, marker, journal)` (`:1162-1173`): `native_release`
  with the held marker's `released` state, `stop_receipt_durable` with the durable receipt — the
  two shapes the stage-boundary validator has already validated exactly
  (`linuxV2ValidateStageJournalBoundary`, `:1639-1740`).
- The crash-replay runner keeps the 90 s pre-recovery wait for every helper stage (so a slow
  unproven stop is waited out before the recovery starts, and no new race is introduced), but fails
  on it only when the stage's stop is *not* proven (`:2116-2127`; fix round r2 removed the
  tolerated proven-stage arm entirely, below).
- Fix round r1 (review finding STD-A01): a second pure predicate,
  `linuxV2RequireHelperIssuedReleaseStopProof(helper, file, found)` (`:1175-1187`), is consulted at
  the same two proven stages right after the journal validator (`:2170-2175`): the recovered
  release tombstone's `StopReceipt.LinuxProof` must be `Kind == helper` with the held helper as
  both `Issuer` and `Reaper` and no `HelperExit` — the helper's own released-gate stop
  (`platform/helper_v2_stop.go:985-1000` builds exactly that proof). An anchor-reap mirror proof
  (`Kind == mirror`, a distinct mirror identity as `Issuer`, the anchor as `Reaper`, a set
  `HelperExit`; `platform/helper_v2_stop.go:182-192`, `app/linux_owner_recovery_linux.go:1117-1125`)
  is rejected. Before r1 this provenance was unpinned at `native_release` (the shared released
  branch of `linuxV2ValidateStageReplayJournal`, `:1471-1474` at the delivered bytes, checks only
  the five physical stop booleans), while `stop_receipt_durable` already chained the final receipt
  to the helper's live Status StopEvidence through the receipt-equality block (`:2162-2169`,
  `authority/supervisor.go:716-725` compares `LinuxProof` including `Kind`); the r1 check makes the
  provenance explicit and uniform at both stages. The daemon-crash witness keeps owning the
  separate "helper survives the daemon SIGKILL" property
  (`production_linux_v2_native_witness_test.go:2629-2632`).
- At the proven stages the runner keeps its post-recovery helper-exit poll within 30 s
  (`:2204-2209`). It is defence-in-depth, not the leg's first detector (review finding STD-A02
  correction): the recovery only answers `released` after `anchor.RetireHelper` observed the
  helper's pidfd exit inside the same 30 s anchor bound (`platform/anchor_types.go:21-25`,
  `platform/helper_v2_recovery.go:541-550`), and a recovery that cannot retire the helper exits
  non-zero, which `requireLinuxV2RecoveryExitStatus` fatals on before the poll runs. The poll's own
  sensitivity is exact: `linuxV2WaitPIDFDExited` returns any non-timeout poll error verbatim
  (`production_linux_v2_native_witness_test.go:2600-2604`), so only its bounded-wait sentinel means
  "helper still live".
- No validator, bound, production, harness or fixture-script change: the response validator
  (`linuxV2ValidateStageReplayResponse` `:1144-1150`, `linuxV2ValidateStageReplayResponseState`
  `:1319-1453`), the journal tombstone validator (`linuxV2ValidateStageReplayJournal`
  `:1455-1561`), the post-recovery target-pidfd wait (`:2195-2203`) and the leaf checks stay
  byte-identical; the r1 and r2 predicates and their call sites are additions outside them.
- Fix round r2 (contract ruling R1-R6; DS audit P1 = the valid unresolved thread
  `PRRT_kwDOUMNTZc68QecV`; **superseded by fix round r3, §8**): the tolerated pre-recovery wait
  was replaced at exactly the two proven stages by the owner-loss assertions. `native_release`
  required the already-open exact B target pidfd to have exited inside the same 90 s fence
  (`linuxV2RequireTargetExitedBeforeRecovery`); `stop_receipt_durable` required the held helper's
  authenticated Status — `OwnerLost`, the exact held identity, its own cached stop proof
  (`Kind == helper`, `Issuer == Reaper == held helper`, `HelperExit == nil`) equal to the durable
  receipt proof (`linuxV2RequireHeldHelperOwnerLostStopProof` /
  `linuxV2WaitHeldHelperOwnerLostStopProof`), polled by the *fixture* through the anchor RunKey map
  (`LookupHelper` → `Control.Status`); `linuxV2RequireOwnerLossStopBeforeRecovery` routed the two
  stages. The old wait with its unconditional fatal stays for every unproven helper stage, so no
  tolerance remains and every poll error is fatal (ruling R2 closed by obviation). The predicate
  table covers all seven non-proven helper stages (ruling R3), the stale “must wait strictly past
  that product bound” comment is gone with the wait it described and the kept comments are 2-line
  why-only (ruling R4; the product-design rationale lives in this record), and DS P3's
  `checkout_ready` census is recorded as a `docs/TODO.md` residual (ruling R6). The branch is keyed
  on the stage, not the harness: both kind legs present `barrierStage == "native_release"` to the
  same runner (`production_linux_v2_native_witness_test.go:194-197`, `199-205`, `233`).
  **Round-3 corrections (DS r2 audit P1-1/P1-2, three consults; §8):** the r2 `native_release` arm
  measured only containment — the claude target self-exits on the App's stdin EOF (measured, exact
  pinned 2.1.323 bytes: `rc=0` 0.30 s after EOF; the App is the sole writer) — and its
  `stop_receipt_durable` arm was unrunnable: the fixture is not `initialOwner`, and the first
  post-loss non-anchor peer claims the single `recoveryOwner` slot, so the fixture's dial is
  refused or steals the slot the real recovery needs (`platform/helper_v2_server.go:497-508`).
  Round 3 deletes both fixture dials and moves the read into the recovery client (§8).
- Assertion-removal mapping (standing conductor rule, 2026-11-26): the two removal events are the
  r2 replacement of the pre-recovery helper-exit wait at the two proven stages, and the r3
  replacement of the r2 arms themselves. Every guarantee is mapped to a surviving or new check
  with a red mutant:
  1. “the owner-loss path itself stopped the target before recovery” (the arm the r1 relaxation
     dropped; DS P1) — r2 mapped it to the target-pidfd exit and the fixture-side Status poll, and
     r3 re-maps it to the recovery client's own pre-stop snapshot: the held helper had latched
     owner loss and cached its own helper-issued stop proof (Kind helper, Issuer/Reaper = held
     helper, HelperExit nil, released-gate authorization) before the recovery's first Stop, equal
     to the durable receipt proof (§8; `linuxV2RequireOwnerLossSnapshotBeforeRecovery`). Red
     mutants: `m1` (snapshot taken after the recovery call), `m4` (latch dropped), `m5` (proof
     shape dropped), `m6` (receipt equality dropped), `m9` (released-gate authorization dropped),
     §4/§8.
  2. “an unproven owner-loss stop cannot leave the helper resident” — carried by the product's own
     fail-safe (self-kill in ≤30 s; the §5 probe
     `TestScratchUnprovenOwnerLossStopSelfKillsTheHelper` observes it) and, on the recovery route,
     by the r1 provenance pin; the unproven-stage wait itself is byte-identical in effect (same
     call, bound and message, fatal on every error), so no new branch exists there; red mutant
     `m8`/`m5`-class (mirror proof accepted) pins the recovery route, and the unproven-stage
     fatal's own oracle is the guest, unchanged from the base bytes.
  3. “no helper outlives the containment lifecycle” — carried by the recovery's `RetireHelper`
     pidfd-exit gate (`platform/helper_v2_recovery.go:544`, `platform/anchor_types.go:21-25`) and
     the unchanged post-recovery 30 s poll (`:2204-2209`); no changed branch, pinned by the
     unchanged bytes (STD-A02) and the recovery-exit fatal.
  The deleted wait's green arm proved nothing at these stages: on the base bytes it passed only
  when the helper self-killed (the unproven-stop case the released gate cannot produce) and its
  red arm conflated correct residency with a skipped stop — the r1 root cause.
- No leg-order or shared-state change (standing conductor rule, 2026-11-26): r2 alters only checks
  inside one leg before its recovery starts. The r2 fixture-side `LookupHelper` + `Control.Status`
  calls were *not* the read-only pattern the fixture could run: the fixture is a different peer and
  the helper admits exactly one post-loss recovery peer
  (`platform/helper_v2_server.go:497-508`; DS r2 audit P1-2). Round 3 deletes them (§8) and moves
  the read into the recovery client process, which is the same peer as the subsequent production
  `LookupHelper` (§8 premise). The recovery's inputs (marker, journal, socket), the
  response/journal validators and the post-recovery polls are byte-identical, so every downstream
  consumer sees the same state the r1 wait left — minus the dead time at the proven stages.

## 4. Red-first and mutant rows

The tolerance branch is pinned by the row `TestLinuxV2StageReplayHelperStopIsProven` — `:2371-2401`
at the delivered r1 bytes,
8 rows: `released native_release
hold` and `stop-receipt durable hold` positive; the not-released/unknown marker, the receipt-less
authority, the authority-less durable hold, and both pre-release shapes negative — and reddens
when the `native_release` term is inverted. Mutant (scratch overlay, never written into the
tree; `go test -overlay=… -run '^TestLinuxV2StageReplayHelperStopIsProven$' ./e2e/`, WSL), at the
implement-stage bytes:
the scratch copy `mutant-owner-cli.go` (a copy of the tree file, so its line numbers match) with
`:1167` changed from
`return marker.NativeReleaseState == "released"` to
`return marker.NativeReleaseState != "released"`:

```
--- FAIL: TestLinuxV2StageReplayHelperStopIsProven (0.00s)
    --- FAIL: TestLinuxV2StageReplayHelperStopIsProven/released_native_release_hold (0.00s)
        production_linux_v2_owner_cli_test.go:2378: helper-stop-proven predicate = false, want true
    --- FAIL: TestLinuxV2StageReplayHelperStopIsProven/not-released_native_release_hold (0.00s)
        production_linux_v2_owner_cli_test.go:2378: helper-stop-proven predicate = true, want false
    --- FAIL: TestLinuxV2StageReplayHelperStopIsProven/unknown_native_release_hold (0.00s)
        production_linux_v2_owner_cli_test.go:2378: helper-stop-proven predicate = true, want false
FAIL
FAIL	github.com/wxxb789/symmetry/daemon/e2e	0.036s
FAIL
MUTANT-EXIT=1
```

Fix round r1 re-ran that same inversion against the delivered r1 bytes (scratch
`mutant-r1-predicate.go`, sha256 `fe9ffcbb22fcd070fb3697086e9bed94cceacd02ecf266315ada2d63eada4fa2`;
overlay `overlay-r1-predicate.json`, sha256
`ebb175bd9992e312dcb8758155dc83131fe1a5725836769baf00d73eadd4f5bc`; WSL): the same three rows red,
exit non-zero, with its `t.Fatalf` line at `:2397` for the delivered bytes:

```
=== RUN   TestLinuxV2StageReplayHelperStopIsProven
=== RUN   TestLinuxV2StageReplayHelperStopIsProven/released_native_release_hold
    production_linux_v2_owner_cli_test.go:2397: helper-stop-proven predicate = false, want true
=== RUN   TestLinuxV2StageReplayHelperStopIsProven/not-released_native_release_hold
    production_linux_v2_owner_cli_test.go:2397: helper-stop-proven predicate = true, want false
=== RUN   TestLinuxV2StageReplayHelperStopIsProven/unknown_native_release_hold
    production_linux_v2_owner_cli_test.go:2397: helper-stop-proven predicate = true, want false
--- FAIL: TestLinuxV2StageReplayHelperStopIsProven (0.00s)
    --- FAIL: TestLinuxV2StageReplayHelperStopIsProven/released_native_release_hold (0.00s)
    --- FAIL: TestLinuxV2StageReplayHelperStopIsProven/not-released_native_release_hold (0.00s)
    --- FAIL: TestLinuxV2StageReplayHelperStopIsProven/unknown_native_release_hold (0.00s)
FAIL
FAIL	github.com/wxxb789/symmetry/daemon/e2e	0.024s
FAIL
```

The r1 proof-provenance pin is its own row, `TestLinuxV2StageReplayHelperIssuedReleaseStopProof` —
`:2403-2456`, 8 rows: the held helper's own released-gate proof positive; an anchor-reap mirror
proof, a mirror kind carrying the held helper identity, a helper-kind proof reaped by the anchor,
a helper-kind proof for another issuer, a helper-kind proof carrying a helper exit, a release
tombstone without a stop proof, and a missing held-helper identity negative. Mutant (scratch
`mutant-r1-lf.go`, sha256 `3838b06faf7163d5abdf7308bd2664a3404dfca3676c37e3494895d90485ceed`;
overlay `overlay-r1-lf.json`, sha256
`c87b3fbbf035d9f22945f8236f60f480e1e8f362c77b3179b474a4dc787633b7`): the provenance terms (`Kind`,
`Reaper`, `HelperExit`) dropped, leaving only the issuer equality — 3 rows red at `:2451`, exit
non-zero:

```
=== RUN   TestLinuxV2StageReplayHelperIssuedReleaseStopProof
=== RUN   TestLinuxV2StageReplayHelperIssuedReleaseStopProof/held_helper's_own_released-gate_proof
=== RUN   TestLinuxV2StageReplayHelperIssuedReleaseStopProof/anchor-reap_mirror_proof
=== RUN   TestLinuxV2StageReplayHelperIssuedReleaseStopProof/mirror_kind_carrying_the_held_helper_identity
    production_linux_v2_owner_cli_test.go:2451: helper-issued release stop proof error = <nil>, want ok=false
=== RUN   TestLinuxV2StageReplayHelperIssuedReleaseStopProof/helper-kind_proof_reaped_by_the_anchor
    production_linux_v2_owner_cli_test.go:2451: helper-issued release stop proof error = <nil>, want ok=false
=== RUN   TestLinuxV2StageReplayHelperIssuedReleaseStopProof/helper-kind_proof_for_another_issuer
=== RUN   TestLinuxV2StageReplayHelperIssuedReleaseStopProof/helper-kind_proof_carrying_a_helper_exit
    production_linux_v2_owner_cli_test.go:2451: helper-issued release stop proof error = <nil>, want ok=false
--- FAIL: TestLinuxV2StageReplayHelperIssuedReleaseStopProof (0.00s)
    --- PASS: TestLinuxV2StageReplayHelperIssuedReleaseStopProof/held_helper's_own_released-gate_proof (0.00s)
    --- PASS: TestLinuxV2StageReplayHelperIssuedReleaseStopProof/anchor-reap_mirror_proof (0.00s)
    --- FAIL: TestLinuxV2StageReplayHelperIssuedReleaseStopProof/mirror_kind_carrying_the_held_helper_identity (0.00s)
    --- FAIL: TestLinuxV2StageReplayHelperIssuedReleaseStopProof/helper-kind_proof_reaped_by_the_anchor (0.00s)
    --- PASS: TestLinuxV2StageReplayHelperIssuedReleaseStopProof/helper-kind_proof_for_another_issuer (0.00s)
    --- FAIL: TestLinuxV2StageReplayHelperIssuedReleaseStopProof/helper-kind_proof_carrying_a_helper_exit (0.00s)
FAIL
FAIL	github.com/wxxb789/symmetry/daemon/e2e	0.069s
FAIL
```

The delivered r1 bytes pass both rows (`ok e2e`, 8/8 subtests each, §6). The r1 assertion's
end-to-end firing (a mirror-proof tombstone) needs the privileged guest, so its first end-to-end
oracle is the dispatch-only CI job `linux-v2-native-acceptance` (non-root runner user), which runs
this matrix leg.

Fix round r2 re-ran the focused rows at the delivered r2 bytes (§1; `ok e2e`, 41 subtest PASS
rows = 6 + 12 + 13 + 2 + 8, §6) and extended the enumeration:
`TestLinuxV2StageReplayHelperStopIsProven` now has 13 rows — the two positive rows, the
not-released/unknown marker rows, the receipt-less and authority-less rows, and one row for each
of the seven non-proven helper stages (`helper_bound`, `launcher_bound`, `gate_bound`,
`handoff_prepared`, `handoff_committed`, `lease_armed`, `checkout_ready`). The r2 rows are
`TestLinuxV2OwnerLossStopBeforeRecovery` (3 routing rows + 3 local-replica rows on a real pidfd of
a spawned process — stopped target green, live target red at a 300 ms bound, missing pidfd red;
the guest leg cannot run locally, so this is the local replica of the target-exit arm),
`TestLinuxV2HeldHelperOwnerLostStopProof` (12 rows: the positive OwnerLost/helper-issued/
receipt-equal Status; the latch, error-code, held-identity, missing-evidence, four proof-shape and
two receipt rows negative) and `TestLinuxV2StageReplayKindLegsShareTheProvenReleaseBranch` (2
subtests, `native_release_codex` and `native_release_claude`: each leg's case must present the
`native_release` barrier the proven-stop predicate selects — the codex leg's row evidence).

Eight byte mutants (scratch copies + `go test -overlay`, never written into the tree) each go red
at the focused rows:

| mutant (sha256) | change | red rows (focused run, exit 1) |
| --- | --- | --- |
| `mutant-m1-lease.go` `c9eff14dfa0152beece3540d389f80738634cdd380549d087591f869181be1d5` | `case "lease_armed": return true` in the predicate | `lease-armed hold with a released marker and a durable receipt` (1 row; `:2537` at the delivered bytes) |
| `mutant-m2-latch.go` `5925c390d8168e25e8fef978ba382b2432aa3e2b8eb51dded9997d016ad58072` | `!status.OwnerLost` dropped | `owner-loss latch absent` (1 row; `:2734`) |
| `mutant-m3-kind.go` `41b2ecc9e0ba530e4c8482c48f00a235b2949dfa9910528a7aeee41fcf4ed887` | the four proof-shape terms dropped | `mirror kind even with a matching receipt`, `anchor-reaped proof even with a matching receipt`, `foreign-issuer proof even with a matching receipt`, `helper-exit proof even with a matching receipt` (4 rows) |
| `mutant-m4-want.go` `3379bd6dec26f483b073a203e8d2ac28acd682865ad5d508a20798dcfecaa759` | the durable-receipt equality and its nil guard dropped | `proof differing from the durable receipt`, `durable receipt proof missing` (2 rows; `:2734`) |
| `mutant-m5-live.go` `3666116d5877aca475df2ad60f135b25800177aae895eceba017fb8e6abc04d8` | the target-exit failure swallowed | `local replica: target still live` (1 row; `:2672`) |
| `mutant-m6-default.go` `2ddf557ef22a3edf798f8cb4b5f3474e4e9611ebf547d9e70e60a9c32412abc3` | the routing default widened to `return nil` | `unproven stage refused` (1 row; `:2631`) |
| `mutant-m7-invert.go` `b6bb4ba26d3234ff43b6b83f9ad133fdddc371b8e10161549200d8d79a1f1c7f` | the `native_release` term inverted (the r1 mutant re-run at the r2 bytes) | `released`/`not-released`/`unknown native_release hold` (3 rows; `:2537`) plus `TestLinuxV2StageReplayKindLegsShareTheProvenReleaseBranch/native_release_codex` and `…/native_release_claude` (`kind leg "native_release_codex" does not present the shared proven native_release boundary`, `:2555`) |
| `mutant-m8-provenance.go` `2cbd3eda41ec645cacc9a4741b5376f6fd8c1f060b4ec4bd0f26d472f3f2b0b0` | the r1 provenance terms dropped to the issuer equality (the r1 mutant re-run at the r2 bytes) | `mirror kind carrying the held helper identity`, `helper-kind proof reaped by the anchor`, `helper-kind proof carrying a helper exit` (3 rows) |

The r2 assertions' end-to-end firing (a skipped owner-loss stop leaves the target live; a stale or
foreign helper proof) needs the privileged guest for the same reason, so the dispatch-only CI job
is its first end-to-end oracle; for the codex leg the r1/r2 decision layer is the same
stage-keyed input (the m7 mutant names the leg), and the post-fix Bind runs `codex_containment`
beside `claude_containment`. The runner's two-arm branch is the same
`if helperStopProvenWithoutOwner` decision the extended predicate table pins
(call site `:2116-2127`).

Fix round r3 replaces the r2 row set: the deleted r2 rows/mutants above describe the r2 bytes and
are kept as the r2 record; the delivered r3 rows are enumerated in §8.4, and their ten mutants
(`m1`…`m10`, scratch copies + `go test -overlay`, never written into the tree) each go red at
their rows on the r3 bytes (§6, WSL):

| mutant (sha256) | change | red rows (focused run, exit 1) |
| --- | --- | --- |
| `m1-order.go` `31413904f767b15f470c6e56b52ed95d61fb72d8ffb792319f6bcdbd771ec2fa` | the seam capture moved after the recovery invocation | `TestLinuxV2NativeWitnessOwnerLossSnapshotPrecedesRecovery` (`owner-loss seam order = [recovery snapshot]`) |
| `m2-app-stage-widen.go` `f406e291462610b874cb0bd381a6a54a1f9f72fc5bbec015fd76dea7502127b6` | `lease_armed` added to the app stage predicate | `…OwnerLossStage/pre-release_hold` + `…SnapshotPrecedesRecovery/unproven_hold_carries_no_snapshot` (2 rows) |
| `m3-app-shape-drop.go` `77c8e4e9181222320283f94b4cef92f956188d1a61d806e8b83c54b62ad1b82e` | the app "captured or explained" shape check dropped | `…OwnerLossSummaryShape/silent_failure` (1 row) |
| `m4-latch-drop.go` `e73f9799322eeeb666e0197c7edebfbfa196f54bcf0e21f1fbad4acaa38c17dd` | the `OwnerLost` term dropped | `owner-loss latch absent` (1 row) |
| `m5-proof-shape-drop.go` `ce505865e1cfc801b6de36fdaf65cc134a33cad79a292a9013203b6a2fbfd8dc` | the four proof-shape terms dropped | `mirror kind` / `foreign issuer` / `anchor-reaped` / `helper-exit proof even when the receipt matches` (4 rows) |
| `m6-receipt-eq-drop.go` `630cde026debcc3734159d9e0ec822c7990ec1d933a34e8dc20b237046383f0a` | the receipt equality and its nil guard dropped | `proof replaced after the snapshot`, `receipt proof missing` (2 rows) |
| `m7-helper-id-drop.go` `6982ce328b5af0bd87a59692f6f8a692f584829e9a80db4df2a4c47c7f31c152` | the held-helper identity check dropped | `snapshot for another helper` (1 row) |
| `m8-clean-capture-drop.go` `238ded9ce1bd8932681f4f98647af0d706fb7109eae0580e231f7d2e9d79d366` | the stage/clean-capture checks dropped | `capture failed`, `snapshot for the wrong stage` (2 rows) |
| `m9-release-auth-drop.go` `e339b54bd5d1bc8a727c7a5cf7b216d09a2c8d1b65456b589495a62e07ddea95` | the released-gate authorization check dropped | `released-gate authorization missing` (1 row) |
| `m10-required-widen.go` `4a57da5739a3cc5f16e5d87062f6d3ba2644887dae70178f7728134c12930903` | `lease_armed` widened into the snapshot-required predicate | `lease-armed hold with a released marker and a durable receipt` (1 row) |

`m5` is why the four shape rows carry the mutation in *both* the snapshot proof and the expected
receipt proof: an equality-only check would have accepted a mirror-kind pair (the first `m5`
attempt stayed green and exposed exactly that gap before the rows were strengthened).

## 5. Root-cause probes (scratch, WSL, never in the tree)

Fix round r2's local replica of the target-exit arm is delivered inside the fixture row
(`TestLinuxV2OwnerLossStopBeforeRecovery`, §4), not a scratch probe; the §5 arms below stay the
root-cause probes for the r1 residency finding.

Two arms drive the real owner-loss latch on a bare helper built in the platform package, added to
the package by `go test -overlay` only (scratch `probe_helper_residency_test.go`, log `probe.log`,
rebuilt after the host restart and re-run on the final bytes — `PROBE-EXIT=0`):

- `TestScratchOwnerLossProvenStopKeepsHelperResident` (PASS, 0.03 s): a real pipe's EOF reaches
  `watchOwnerPipe`, `markOwnerLost` latches owner loss **without** the self-kill (a completed
  successful finalization returns nil from its finalizing branch), and a dial-and-write round trip
  to the helper's listener still succeeds (no `finalizeAfterClear`); the sensitivity arm then calls
  `finalizeAfterClear` and the same round trip fails fast instead of by timeout, so the probe can
  tell a resident helper from an ended one.
- `TestScratchUnprovenOwnerLossStopSelfKillsTheHelper` (PASS, 0.04 s): on the same latch with no
  provable stop state, the helper process dies by SIGKILL (`requestAnchorTakeover`), asserted from
  the child's wait status (`child wait status: 9`).

## 6. Local gates

| Gate | Command (working directory `daemon/` unless noted) | Exit | Result |
| --- | --- | --- | --- |
| formatting | `gofmt -l .` (Windows and WSL, r2 bytes) | 0 / 0 | empty list on both surfaces |
| vet | `go vet ./...` (Windows, go1.27.4; WSL Ubuntu-24.04, go1.27.0) | 0 / 0 | empty diagnostics on both surfaces |
| r2 focused rows | `go test -count=1 -v -run '^(TestLinuxV2OwnerLossStopBeforeRecovery|TestLinuxV2HeldHelperOwnerLostStopProof|TestLinuxV2StageReplayHelperStopIsProven|TestLinuxV2StageReplayKindLegsShareTheProvenReleaseBranch|TestLinuxV2StageReplayHelperIssuedReleaseStopProof)$' ./e2e/` (WSL) | 0 | `ok e2e`, 41 subtest PASS rows (6 + 12 + 13 + 2 + 8; includes the r1 predicate/provenance rows) |
| r2 mutant rows | the eight overlays in §4, `go test -overlay=… -count=1 -run … ./e2e/` (WSL) | 1 × 8 | §4: 1 / 1 / 4 / 2 / 1 / 1 / 5 / 3 rows red; m7 names `native_release_codex` and `native_release_claude` |
| probes | `go test -overlay=… -count=1 -v -run '^TestScratch…$' ./internal/platform/` (WSL) | 0 | §5: both PASS (0.03 s / 0.04 s) |
| full suite (Windows) | `go test -count=1 ./...` | 0 | 27 `ok`, 3 `no test files`, 0 FAIL, 3m11.1s |
| full suite (WSL, unprivileged `accept`, CI umask 022) | `umask 022; go test -count=1 ./...` as user `accept` | 0 | 31 `ok`, 3 `no test files`, 0 FAIL, 2m59.8s |
| full suite (WSL, root) | `go test -count=1 ./...` | 1 | 30 `ok`; `internal/platform`'s 13 `TestLinuxOwnerAnchorShape*`/`…Wire*` tests refuse with the documented root guard (`wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`) — identical to the r1/implement-stage set |
| full suite (WSL, `accept`, as-is umask 0002) | `go test -count=1 ./...` | 1 | implement-stage environment row (r1), not re-run at r2: 28 `ok`; `internal/linuxpreflight` 2 + `internal/platform` 8 + `internal/workspace` 21 tests fail |
| base attribution | `go test -count=1 ./internal/linuxpreflight/ ./internal/platform/ ./internal/workspace/` in a pristine `git archive 78166f2e` tree (WSL, `accept`, as-is umask) | 1 | the identical 31 failing test names (set difference empty both ways) |
| anchors | `bash verify-anchors-r2.sh` (scratch) | 0 | every cited line range in this record and the plan re-read at the delivered r2 bytes; all exact |
| comment cap | `python comment-cap.py <scratch clone> 7111828b` (official, on the scratch-clone commit `5b1d9656` whose Go-file sha256 equals §1's `3c8759c4…`) | 0 | empty output — no added comment block over 2 lines |
| sanitize | `python scripts/ci/model-routing-record-sanitize.py --check --root-only` per changed doc | 0/0/0/1 | record/plan/TODO pass; `CLOSED-TODO` exit 1 identical at base (pre-existing quoted specimens; the added lines carry no host path) |
| duplication (jscpd) | `python scripts/ci/jscpd-changed.py --base 7111828b --head HEAD` (scratch-clone commit `5b1d9656`, Go-file sha256 = §1's; pinned `node_modules/jscpd` 4.3.0 via the installed junction) | 0 | 5 changed file(s), 1775 scanned source(s), 1254 clone(s) in the corpus, 0 tolerated, none new; notices: the changed 5,402-line Go test file and `CLOSED-TODO.md` exceed the audited `--max-lines 5000` ceiling, reported, never silently skipped |

The full suites ran one at a time, never beside another harness, and the formatting, vet, focused
rows, mutants, suites and anchor sweep above were all run at the delivered r2 bytes of §1. The
`accept`/022 row is the CI-parity surface (the CI daemon job runs as the non-root runner user); the
root row re-ran on the r2 bytes with the identical refusal set, and the as-is-umask row is this
host's implement-stage environment record, kept with its base attribution.

Fix round r3 re-ran the whole sequence on the delivered r3 bytes (§1's r3 table), one surface at a
time:

| Gate (r3) | Command (working directory `daemon/` unless noted) | Exit | Result |
| --- | --- | --- | --- |
| formatting + vet | `gofmt -l .`; `go vet ./...` (Windows go1.27.4 and WSL Ubuntu-24.04 go1.27.0) | 0 / 0 | both clean on both surfaces |
| focused rows, app | `go test -count=1 -v -run '^(TestLinuxV2NativeWitnessOwnerLossStage\|TestLinuxV2NativeWitnessOwnerLossSummaryShape\|TestLinuxV2NativeWitnessOwnerLossSnapshotPrecedesRecovery)$' ./internal/app/` (WSL) | 0 | `ok`, 18 subtest PASS rows, 0 FAIL |
| focused rows, e2e | `go test -count=1 -v -run '^(TestLinuxV2StageReplayOwnerLossSnapshotRequired\|TestLinuxV2OwnerLossSnapshotBeforeRecovery\|TestLinuxV2StageReplayKindLegsShareTheProvenReleaseBranch\|TestLinuxV2StageReplayHelperIssuedReleaseStopProof)$' ./e2e/` (WSL) | 0 | `ok`, 42 subtest PASS rows, 0 FAIL |
| r3 mutant rows | the ten overlays in §4, `go test -overlay=… -count=1 -run … ./internal/app/ ./e2e/` (WSL) | 1 × 10 | §4: 1 / 2 / 1 / 1 / 4 / 2 / 1 / 2 / 1 / 1 rows red |
| full suite (Windows) | `go test -count=1 ./...` | 0 | 27 `ok`, 3 `no test files`, 0 FAIL |
| full suite (WSL, `accept`, `umask 022`) | `go test -count=1 ./...` as user `accept` | 0 | 31 `ok`, 3 `no test files`, 0 FAIL |
| comment cap | `python comment-cap.py <scratch clone> 94e36995` on the scratch-clone commit whose `production_linux_v2_owner_cli_test.go` sha256 equals §1's r3 `0d2aa2a2…` | 0 | empty output — no added comment block over 2 lines (the first r3 attempt flagged a 3-line doc comment at `:1193`; it was compressed to 2 lines and the cap re-ran clean) |
| duplication (jscpd) | `python scripts/ci/jscpd-changed.py --base 94e36995 --head HEAD` in the same scratch clone (junctioned `node_modules/jscpd` 4.3.0) | 0 | 9 changed file(s), 1780 scanned source(s), 1245 clone(s) in the corpus, 0 tolerated, none new; notices: `production_linux_v2_owner_cli_test.go` and `CLOSED-TODO.md` still above the audited `--max-lines 5000` ceiling (the r2 rows' pre-existing class) |
| sanitize | `python scripts/ci/model-routing-record-sanitize.py --check --root-only` per changed doc | 0/0/0/1 | record/plan/TODO pass; `CLOSED-TODO` exit 1 identical at base (pre-existing quoted specimens; the added lines carry no host path) |

The r3 mutants were regenerated from the final e2e bytes after the comment compression and re-run
(§4's hashes are those files); the app-package mutants are byte-stable because
`linux_owner_witness_linux.go` did not change after them.

## 7. Residual risks and limits

- The r1 `[INFERENCE]` of §2 is closed by fix round r3's recovery-client pre-stop snapshot (§8;
  and by the 30 s self-kill bound, which rules out a hanging stop behind the old 90 s survival);
  the snapshot is witness telemetry and its ordering is structural, not proved in-band by the
  helper — §8 states both limits — and the end-to-end firing still needs the privileged guest (see
  the oracle bullet below).
- The acceptance legs' *next* stop after this wait is unknown until the Bind, which now runs both
  containment sets (the conductor's main measurement run `45775969711` reds `native_release_codex`
  with the same class, §2 item 5; the diagnostic Bind301 below observed `claude_containment`
  only); the acceptance records it verbatim. The same sequencing change applies to the pi
  `native_release`/`stop_receipt_durable` stage legs, which share the runner; none of them has ever
  been observed green at a released-gate stage, so their next stops are equally the Bind's to
  record.
- The product-side alternative (plan §3.3) remains available to the conductor as a new goal if the
  helper's post-stop residency is ruled a defect; it would need `platform/**` ownership plus a
  change to the daemon-crash witness's `Kind == helper`/`HelperExit == nil` requirement.
- At the two proven stages the pre-recovery evidence is now the recovery client's own snapshot
  (§8) plus the `native_release` target-containment wait; every unproven stage keeps the 90 s
  fatal, and the stage-replay matrix and the `stale_leaf` witness exercise those boundaries with
  their own recovered-evidence assertions unchanged.
- Fix round r1: the two proven stages pin the recovered tombstone to the held helper's own
  released-gate proof (§3, call site `:2170-2175`), so a product regression that routes them
  through the anchor-reap mirror path reds the leg at that assertion instead of passing on the
  physical booleans alone; the daemon-crash witness still owns the separate "helper survived the
  daemon SIGKILL" property for its own case.
- The r1 assertion's end-to-end firing (a mirror-proof tombstone) and the r2 assertions' end-to-end
  firing (a skipped owner-loss stop; a stale or foreign stop proof) need the privileged guest; their
  decision layers are pinned by the r1/r2 rows and their mutants (§4), and the dispatch-only CI job
  `linux-v2-native-acceptance` (non-root runner user) is the first end-to-end oracle.
- The changed `production_linux_v2_owner_cli_test.go` is 5,402 lines, above the duplication gate's
  audited `--max-lines 5000` ceiling, so `jscpd-changed.py` reports it unscanned (§6): clones
  inside its added lines are not checked by that gate, before or after this change. Raising the
  audited ceiling is a separate gate change, outside this goal's owned files.

## 8. Fix round r3 — the recovery-client pre-stop snapshot (contract Z1-Z5)

Round 3 is the contract's A3/D1 choice: the witness seam — an owned production byte — hosts the
pre-recovery read, the fixture's helper dials are deleted, and the two arms' claims are re-worded
honestly.

### 8.1 Premise (the round's STOP-condition check): the witness process is the recovery peer

Verified from code at the r3 base; the premise holds, so the fix proceeds.

- **Same process.** `recoverLinuxV2NativeWitnessRunKey` (`linux_owner_witness_linux.go:2995`)
  constructs the recovery daemon and calls `recoverContainmentOnlyJournals`
  (`linux_owner_recovery_linux.go:1622`) synchronously; that reaches
  `recoverLinuxV2OwnerJournal` → `owner.anchor.LookupHelper` (`:169`) and the recovery's own
  `Control.Stop` (`bindRecoveredV2UnhandedHelperStop`, call `:274`). `LookupHelper`
  (`linux_owner_anchor_linux.go:479`) dials the helper from the *caller* process:
  `dialLinuxOwnerHelperV2` (`helper_v2_protocol.go:338`) stamps
  `client: {PID: os.Getpid(), Identity: ProcessIdentity(os.Getpid())}` (`:363`). The snapshot runs
  in that same process, before the recovery call, so both dials carry the identical peer identity.
- **The helper admits it, and the slot the recovery needs is not consumed.**
  `serveOwnerConnection` (`helper_v2_server.go:475-562`): pre-loss, a non-anchor peer that is not
  the authenticated StartHelper caller is refused (`:497-500` — the snapshot's dial is therefore
  refused until the owner-death latch fires, which the bounded poll waits out); post-loss
  (`recoveryOnly := helper.ownerLost`), the **first** non-anchor peer claims `helper.recoveryOwner`
  and a later connection is refused only when it is a *different* peer
  (`recoveryOwner.Equal(peerIdentity)`, `:503-508`). The snapshot's connection claims the slot as
  the recovery client itself; the recovery's own `LookupHelper` from the same process is admitted
  by that equality. Nothing else is consumed: the anchor's `handleLookup`
  (`anchor_session.go:248-317`) is stateless and re-dups the helper-pidfd/leaf/parent pins per
  call, and `status` is on the post-loss allowlist
  (`helper_v2_server.go:579-581`) and read-only (`handleStatus`, `:646-742`; pinned by
  `TestLinuxOwnerHelperV2StatusDoesNotLatchExpiredLease`). A same-peer re-lookup is therefore
  admitted, including the `Control.Status` parallel connection (`openParallelControl`,
  `linux_owner_helper_v2_linux.go:788-823`).
- **What this closes.** DS audit P1-2: the fixture is a different peer, so its dials were refused
  pre-loss and, post-loss, claimed the single slot the recovery needs. Round 3 deletes them (§8.3)
  and moves the read into the recovery client, where the design already places the first post-loss
  attacher.

### 8.2 What the seam records and what the fixture asserts (Z1, Z2)

- `linux_owner_witness_linux.go` gains the snapshot type `linuxV2NativeWitnessOwnerLoss`
  (`:2866`), the stage predicate `linuxV2NativeWitnessOwnerLossStage` (`:2879`, exactly
  `stage_replay` with `Barrier.HoldAfter` in `{native_release, stop_receipt_durable}`), the
  committed-live boundary predicate `linuxV2NativeWitnessOwnerLossExpected` (`:2892`), the bounded
  read `captureLinuxV2NativeWitnessOwnerLoss` (`:2916`: `LookupHelper` + `Control.Status` poll,
  40 s bound, 250 ms interval; records `OwnerLost`, the cached `StopProof`, the released-gate
  authorization and the held helper identity; never erroring), the shape validator
  `validateLinuxV2NativeWitnessOwnerLoss` (`:2900`, called from `validateFinalSummary` `:747`),
  the summary field `owner_loss` (`:316`), and the two injectable seam variables (`:2991`).
- The fixture mirrors the field (`production_linux_v2_native_witness_test.go:1312`) and asserts it
  at the two stages via `linuxV2RequireOwnerLossSnapshotBeforeRecovery`
  (`production_linux_v2_owner_cli_test.go:1208`, call `:2096`): a clean capture of the exact held
  helper, `OwnerLost`, a helper-issued proof (`Kind == helper`,
  `Issuer == Reaper == held helper`, `HelperExit == nil`), `GateReleaseAuthorized` at
  `native_release`, and equality to the receipt proof — the final release tombstone's proof at
  `native_release`, the pre-existing durable receipt proof at `stop_receipt_durable`. The
  `native_release` target-pidfd wait is kept as a *containment* check only
  (`linuxV2RequireTargetContainedBeforeRecovery`, `:1196`).
- The helper response-loss runner re-anchors its `stop_receipt_durable` receipt checks to the
  journal receipt plus the same snapshot field (`:4350`), with the receipt proof taken from the
  journal (`:4273-4279`).
- Honest limits. The snapshot is witness telemetry: its trust root is the recovery process's own
  response (the same trust as every other witness field, weaker than a fixture dial). Its ordering
  before the recovery's first stop is structural — the capture call precedes the
  `recoverContainmentOnlyJournals` call, pinned by the owned ordering row, not proved in-band by
  the helper (the helper cannot tell same-identity callers apart). At `stop_receipt_durable` the
  helper's cached proof predates the kill and `stopOwned` returns it from its cached fast path
  (`platform/helper_v2_stop.go:716-721`), so the claim there is latch + resident retention +
  receipt identity, not a new stop; at `native_release` the claim is that the released-gate
  owner-loss stop's proof existed before the recovery acted (the skip-`stopOwned` mutant leaves
  the snapshot without a proof, but its end-to-end red needs the privileged guest, §8.5).
  `release-run`-finalized helpers are out of scope: if the helper is already finalized the capture
  records a bounded failure and the recovery proceeds as before.

### 8.3 Deletions (Z3)

Deleted from `production_linux_v2_owner_cli_test.go`: `linuxV2ReadHeldHelperStatus`,
`linuxV2WaitHeldHelperOwnerLostStopProof`, `linuxV2RequireHeldHelperOwnerLostStopProof`,
`linuxV2RequireOwnerLossStopBeforeRecovery`, `linuxV2CaptureNormalAnchorStopEvidence`, and the
pre-kill `normalStop`/`AnchorStop` captures at `stop_receipt_durable` in both the stage matrix and
the helper response-loss runner; `linuxV2StageReplayHelperStopIsProven` is renamed
`linuxV2StageReplayOwnerLossSnapshotRequired` with its logic unchanged (the name and doc no longer
claim the boundary *proves* a stop), and
`linuxV2RequireTargetExitedBeforeRecovery` is renamed
`linuxV2RequireTargetContainedBeforeRecovery`. No other actor's state changes: the runner deletion
is inside one leg before its recovery starts; the response-loss runner's receipt equality is now
journal-sourced; the recovery's inputs (marker, journal, socket), the response/journal validators
and the post-recovery polls are untouched.

### 8.4 Red-first rows and mutants (Z4)

New/renamed rows (all local, WSL): `TestLinuxV2NativeWitnessOwnerLossStage` (8 stage rows plus the
committed-live/helper-exit predicate arms), `TestLinuxV2NativeWitnessOwnerLossSummaryShape`
(8 shape rows), `TestLinuxV2NativeWitnessOwnerLossSnapshotPrecedesRecovery` (the ordering row plus
the unproven-hold and legacy-journal no-snapshot rows — the seam-ordering pin with the injectable
capture/recovery variables), `TestLinuxV2StageReplayOwnerLossSnapshotRequired` (13 rows, the r2
enumeration kept), `TestLinuxV2OwnerLossSnapshotBeforeRecovery` (16 predicate rows plus the three
local containment-replica rows) and the extended
`TestLinuxV2StageReplayKindLegsShareTheProvenReleaseBranch` (both kind legs). Ten byte mutants
(`go test -overlay`, never written into the tree) each go red at their rows: the seam order
reversed (m1), the app stage predicate widened (m2), the app shape check dropped (m3), the latch
term dropped (m4), the four proof-shape terms dropped (m5, four rows), the receipt equality
dropped (m6, two rows), the held-helper identity check dropped (m7), the stage/clean-capture
checks dropped (m8, two rows), the released-gate authorization dropped (m9), and the required
predicate widened to `lease_armed` (m10) — table and hashes in §4, runs in §6. The two Bind-leg
decisions the ruling asks for are covered as far as the local surfaces can: both kind legs present
`barrierStage == "native_release"` and the matrix now passes `testCase.barrierStage` into the
runner (DS P3-2 remediation), and the app-side stage table pins the same two literals the legs'
requests carry.

### 8.5 Guest-only rows (recorded, not claimed)

The product-level mutant “`markOwnerLost` latches owner loss without calling `stopOwned`” reds the
`native_release`/`native_release_codex` snapshot assertion (no cached proof in the snapshot; the
recovery's later `Stop` would otherwise supply one) and the daemon-crash witness — the dispatch
job's matrix legs are that mutant's first end-to-end oracle, exactly as in r1/r2. The
`native_release` containment wait is satisfied by the claude target's stdin-EOF self-exit in both
the green and mutant cases, so it carries no attribution claim (the round's P1-1 correction).

### 8.6 Records (Z5)

“Equal-strength” is removed from the record, plan and closure text; the r2 arms are marked
superseded; P1-1 (measured claude stdin-EOF self-exit on the exact pinned 2.1.323 bytes; the App is
the sole writer) and P1-2 (fixture peer refusal / single-slot claim) are recorded as facts; the
DS P3-2 matrix-literal hole is fixed; the DS P3s kept open are the guest-only product-mutant row
(§8.5) and the `checkout_ready` census residual (`docs/TODO.md`). The assertion mapping is in §3;
the Bind-2 section below is relabelled diagnostic for the r2 bytes it bound.

### 8.7 Ruling checklist (Z1-Z5)

| Item | State | Evidence |
| --- | --- | --- |
| Z1 witness-hosted pre-recovery `LookupHelper` + `Control.Status` into a validated response field, no `platform/**`/`authority/**`/`execution/**` change | done | §8.1/§8.2; `linux_owner_witness_linux.go:2866-3026`; premise verified (same-peer admission) |
| Z2 honest fixture assertions (snapshot proof at `native_release`; latch + cached proof = durable receipt at `stop_receipt_durable`; target wait labelled containment) | done | §8.2; `linuxV2RequireOwnerLossSnapshotBeforeRecovery` (`owner_cli_test.go:1208`), `linuxV2RequireTargetContainedBeforeRecovery` (`:1196`) |
| Z3 delete the fixture dials and the pre-kill `normalStop`/`AnchorStop` capture; re-anchor `stop_receipt_durable` to journal receipt + the Z1 field | done | §8.3; the response-loss runner's journal-sourced receipt proof (`:4273-4279`) and snapshot assertion (`:4350`) |
| Z4 red-first rows: skip-`stopOwned` (guest, §8.5), seam ordering, latch, evidence replacement, codex+claude kind legs | done locally | §8.4/§4: ordering row + ten mutants, 16 rows red; the product mutant's guest row is the residual |
| Z5 records: no “equal-strength”/“no other actor”, P1-1/P1-2 as facts, DS P3s | done | this section, §3, the plan §8, `docs/TODO.md`, `CLOSED-TODO` closure |

## 9. Close-out

Delivered, pending the conductor's Bind (both containment sets, `claude_containment` and
`codex_containment`): the implement round plus fix rounds r1, r2 (superseded) and r3 — the
contract's round-3 ruling Z1-Z5 (§8), on top of the r1 review findings STD-A01/STD-A02, the DS r1
P1 (the valid unresolved thread `PRRT_kwDOUMNTZc68QecV`, contract ruling R1-R6) and the DS r2
P1-1/P1-2 plus the three round-3 consults. The r3 row set and its ten mutants are §8.4/§4; the
deleted r2 arms and the r1 pin remain as the r1/r2 record. The closure row in
`docs/archive/CLOSED-TODO.md` carries the plan/record paths and the r3 assertions; `docs/TODO.md`
carries the `checkout_ready` census residual and the round-3 guest-only product-mutant residual.
The last step ran the comment cap *before* the final re-measure: §1's r3 digests were re-read at
the delivered bytes and are unchanged across every gate row above.

## Bind — diagnostic: binding run on the pre-r2 PR head c0749664 (claude_containment)

Diagnostic only, superseded by the post-fix Bind: this run is GREEN on `c0749664` — the r1 bytes,
exactly the ones this record's r1 gate rows used — and every digest below describes those bytes,
not the r2 bytes of §1. It is kept because it is the last observed end-to-end oracle of the leg on
the pre-r2 assertion set (`findings/301-binding-run.md`).

One `claude_containment` run on the exact PR-head bytes plus the two scratch-only files, watched
once, then the scratch branch deleted and the temporary worktree removed. Full report:
`findings/301-binding-run.md`; the run log is retained at `scratch/bind301/run-45773564544.log`
under the GDD state root (sha256
`942401154d2058e121ccb1c638ccec2a3d1992b8a28aee57e23d02e1cef9e8bc`, 712540 B, 3814 lines).

- Run `45773564544` (event `push`, `run_attempt=1`, head
  `7b0baeb2ffd2c4b0924302d7409afd981ed95f82` = `c0749664` + the two scratch-only files, created
  07:12:10Z, conclusion **success** — the branch's first green push-event run); `total_count=1` at
  every read (right after creation 07:12:17Z, after settlement 07:18:27Z, before the branch
  deletion 07:19:40Z, and after it 07:19:48Z).
- The recorded stop `A helper did not exit with owner loss after the stage-replay kill` is
  **gone**: 0 occurrences, with 0 of `exact process pidfd remained live beyond the bounded wait`,
  `did not exit with owner loss`, `Go native acceptance event gate failed`, `--- FAIL` and
  `pass=0` in the whole log; the leg that carried it,
  `TestProductionLinuxV2StageCrashReplayMatrix/native_release_claude`, verifies green
  (`run=1 pass=1 skip=0`), as does `TestProductionLinuxV2StageCrashReplayMatrix`.
- Every `verified ` line in the log: the contracts job's lockfile row (181, not acceptance
  evidence); the seven AppArmor rows (`TestLinuxAppArmorUsernsDecisionRows`,
  `…ProfileTextMatchesTheInstaller`, `…LoadedProfileScan`, `…HostStateRows`, `…CompileRows`,
  `…AttachmentSensitivePolicyHash`, `…DecisionResetsPerVerify`, 2488-2494);
  `TestProductionLinuxV2ClaudeNativeContainment` (3075);
  `TestProductionLinuxV2StageCrashReplayMatrix` and
  `TestProductionLinuxV2StageCrashReplayMatrix/native_release_claude` (3649-3650), all
  `run=1 pass=1 skip=0`.
- Case-set summary (rows 3657-3658, 3660-3662):

  ```
  Linux amd64 A/B case set claude_containment completed; verifying guest/Control teardown and exporting allowlisted evidence. This witness does not establish Goal acceptance or promote native capabilities.
  allowlisted redacted evidence: 2 files; missing=0
  Linux amd64 installed A/B case set claude_containment passed after verified guest/Control stop and allowlisted evidence export; private journals/checkouts remain retained, and guest teardown is not a containment StopReceipt.
  Case set claude_containment exited 0
  All 1 case sets exited 0.
  ```

  Green — no first failing line.
- The containment witness's own response: outcome `completed`, recovery `released`,
  `containment.native_open_reached: true`, `leaf_absent_after_receipt: true`, with
  `initial_lease_acked`, `native_released`, `stop_receipt_durable`, `leaf_removed` and
  `journal_cleared` true, and its stop proof `stop_proof_kind: helper` with issuer = reaper = the
  helper pid 6971. The matrix leg's response carries the same helper-issued released stop proof
  (`stop_proof_kind: helper`, issuer = reaper = helper pid 7234, `stop_issuer_identity` =
  `stop_reaper_identity`), `leaf_absent_after_receipt: true`, `recovery_state: released` — the r1
  provenance predicate exercised end-to-end.
- Artifacts (sha256 = zip bytes = API `digest`): `linux-v2-native-evidence-45773564544-1`
  id `15095910468` sha256
  `aad74cb5a116352645b9554fe689df925cd1c1d14cbc24f7508953c0a4fc8635` (members
  `claude_containment/manifest.json`
  `4dc8f5fa39391dbf6fc96be6b4a85d37316f45fcb965cbfe788b90b52fd7682e` — `status: "passed"`,
  `source_commit` = the run head; `claude-native-containment.json`
  `3f66fd53957074e530227a8247da54bd7c9cd1d2e999cbaece443cb89b57169a`;
  `claude-stage-crash-replay-native-release.json`
  `8582ebe1dff5be3f0b1185272ce6eb8cda544d95c9719960b0a7b53969957e86`);
  `linux-v2-host-prerequisite-45773564544-1` id `15095201946` sha256
  `164e263cdb0fdbd72efe5dcca3b05c5db15e99910dc1604f77f84d2225508a79` (`status: passed`,
  `source_commit` = the run head).
- Run-input sha256 equality (run head `7b0baeb2ff` vs PR head `c0749664`):

| file | Run-head bytes | PR-head bytes | Equal? |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `86002180c91244c1dd017a4a4498438affa07a6894f9d5372b7024e422c4b531` | `86002180c91244c1dd017a4a4498438affa07a6894f9d5372b7024e422c4b531` | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `04bb9e8f700bfd672dcf9c7f89cebf1ed2a7b8c5266d77ecf1c2a37c4c882aad` | `04bb9e8f700bfd672dcf9c7f89cebf1ed2a7b8c5266d77ecf1c2a37c4c882aad` | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `b687669b0826da767502464978c7af596e549e28b45a7cc54b2abc064e508c50` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

The Go-file digest equals §1's delivered digest; the four record-only docs (`docs/TODO.md`,
`docs/archive/CLOSED-TODO.md`, the plan, this record) are not run inputs.

## Bind 2 — diagnostic: binding run on the r2 PR head 94e36995 (claude_containment + codex_containment)

Diagnostic only, superseded by the round-3 delivery: this run is GREEN on `94e36995` — the r2
bytes, exactly the ones this record's r2 gate rows used, and before the round-3 seam/fixture
change that moves the owner-loss read into the recovery client and deletes the r2 arms (§8) — and
every digest below describes those bytes, not the delivered r3 bytes. It is kept because it is the
last observed end-to-end oracle of both legs on the r2 assertion set.

The conductor's post-fix Bind on the exact PR-head bytes plus the two scratch-only files, watched
once, then the scratch branch deleted and the temporary worktree removed. Full report:
`findings/301-binding-run-2.md`; the run log is retained at
`scratch/bind301r2/run-45780779253.log` under the GDD state root (sha256
`524271801356c5bd5c2cfb9f35657495a184a7d037ed0d0e825dd9630f78ddd9`, 1065891 B, 5155 lines).

- Run `45780779253` (event `push`, `run_attempt=1`, head
  `dbd71f408b3b010dde7344350c48cd063a0503be` = `94e36995` + the two scratch-only files, created
  08:09:20Z, conclusion **success**); `total_count=1` at every read (right after creation, after
  settlement, immediately before the branch deletion and after it).
- `claude_containment`: the recorded stop `A helper did not exit with owner loss after the
  stage-replay kill` is **gone** (0 occurrences; 0 of `exact process pidfd remained live beyond the
  bounded wait`, `did not exit with owner loss`, `Go native acceptance event gate failed`,
  `--- FAIL` and `pass=0`); `TestProductionLinuxV2ClaudeNativeContainment`,
  `TestProductionLinuxV2StageCrashReplayMatrix` and
  `TestProductionLinuxV2StageCrashReplayMatrix/native_release_claude` verify green
  (`run=1 pass=1 skip=0`, log 3073, 3647-3648) beside the seven AppArmor rows (2486-2492);
  `Case set claude_containment exited 0`; the export manifest is `status: "passed"` with
  `missing_allowlisted_files: []`.
- `codex_containment`: the same stop is **gone**; `TestProductionLinuxV2CodexNativeContainment`,
  `TestProductionLinuxV2StageCrashReplayMatrix` and
  `TestProductionLinuxV2StageCrashReplayMatrix/native_release_codex` verify green
  (`run=1 pass=1 skip=0`, log 4416, 4990-4991) beside the seven AppArmor rows (3841-3847);
  `Case set codex_containment exited 0` and `All 2 case sets exited 0.`; the export manifest is
  `status: "passed"` with `missing_allowlisted_files: []`. No first failing line (0 `pass=0`
  rows); both jobs `success`.
- Artifacts (sha256 = zip bytes = API `digest`): `linux-v2-native-evidence-45780779253-1` id
  `15098945253` sha256
  `71d00ea649188edabed710a3500e582aea4445eed9cb3ee147da1d92ee056eeb` (members
  `claude_containment/manifest.json`
  `bcd82c24ba16894dbdaf79f57c5d631c6c1b072faf62eb21b12bac864201d4a6`;
  `claude_containment/claude-native-containment.json`
  `230273d2c0a773b0d531d586631d42fa94d9ddb8fa742e18f540a3f3584d0b78`;
  `claude_containment/claude-stage-crash-replay-native-release.json`
  `bb36978939d9604ad56fe3c57e4d9f8420c2028dbd04bb51c253c1f10ba3298d`;
  `codex_containment/manifest.json`
  `bd52ec658330c4ec4f9eac6cecb320247f25ba5610f91459fbe83d67e62314c2`;
  `codex_containment/codex-native-containment.json`
  `0824fdeec2c10176b6c02cbe2f32e100ea250e5402b83a5069723ced4be5e43b`;
  `codex_containment/codex-stage-crash-replay-native-release.json`
  `4ffb5fc836b347155c1200686fc47d8bd6a6f0631ec0e30e6b5945b8e541f405`);
  `linux-v2-host-prerequisite-45780779253-1` id `15098856013` sha256
  `c0eaa75d111f76d0b5f30175fccd163b9120d0c9c0fc5b7fabe6231e7aaffed5` (`status: passed`,
  `source_commit` = the run head).
- Run-input sha256 equality (run head `dbd71f408b` vs PR head `94e36995`):

| file | Run-head bytes | PR-head bytes | Equal? |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `3c8759c4aa83ddea43e542edff78d75f7c798c3ec1102e04f8cfe2b0885a028d` | `3c8759c4aa83ddea43e542edff78d75f7c798c3ec1102e04f8cfe2b0885a028d` | yes |
| `scripts/ci/linux-v2-native-acceptance.sh` | `6c7efcf062a3190a2559e35505c38cf036bd62a40f6f889bd6724733c33c973b` | `6c7efcf062a3190a2559e35505c38cf036bd62a40f6f889bd6724733c33c973b` | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `3eaada89cf74e116282fe74cfd1fbda8c418197c09f6c29785cd6533060d7f04` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

The Go-file digest equals §1's delivered r2 digest; the shim carries the recovered blob unchanged,
and the workflow carries the single `case_sets` delta (`claude_containment` first,
`codex_containment` second). The four record-only docs are not run inputs.
