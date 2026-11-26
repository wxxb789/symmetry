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
post-kill wait (`production_linux_v2_owner_cli_test.go:1972-1979` after this goal's fix; the
planning-time anchor on the base bytes it reds at is `:1958-1964`, fatal at `:1963`):

```
witness output:     production_linux_v2_native_witness_test.go:232: A helper did not exit with owner loss after the stage-replay kill: exact process pidfd remained live beyond the bounded wait
```

Which row/helper/process: the leg's held boundary is `native_release`; the pidfd is the exact
`reservation.Helper` identity (`-linux-owner-helper-v2`, pid 7168 in that run), opened by
`linuxV2OpenStageOwnerPIDFD` at `owner_cli_test.go:1933`, not the released B target (pid 7182 =
`owners.gate_pid`, captured live at `owner_cli_test.go:1936-1958`). Retained-run facts:
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
   recovery has a dedicated live-helper branch (`app/linux_owner_recovery_linux.go:1242`,
   `1271-1292`, `1337-1340`), and the daemon-crash witness *requires* it — the helper must
   survive the daemon SIGKILL (`production_linux_v2_native_witness_test.go:2553-2556`) and the
   recovered tombstone's stop proof must be helper-issued with no helper exit (`:2628-2632`).
5. The fixture contradicts that at every helper stage: `owner_cli_test.go:1972-1979` supplies the
   wait that fails on a live helper after 90 s with the message quoted above. At the pre-release
   stages the same wait
   passes because there the stop is unprovable (the anchor has no gate authorization and no
   authorized exec transition), `stopOwned` errors, and the helper self-kills; the released-gate
   stages (`native_release`, `stop_receipt_durable`) are exactly the ones whose stop is proven,
   so the wait can never be satisfied there. Candidate “the claude process tree outlives the
   owner” is excluded: no step of the owner-loss path is harness-specific, and the retained
   artifact binds the claude target as the single exact leaf member
   (`exact_leaf_membership: true`, `native_command_verified: true`).

Not a production defect in `daemon`/`execution` code: the product's own recovery design (item 4)
requires this state. The fix therefore belongs on the fixture side (Stage 2), and the product-side
alternative is recorded as rejected in §3.3.

## 3. The fix

### 3.1 Fixture semantics chosen

At the two stages whose durable boundary proves the helper's owner-loss stop —
`native_release` with the held marker's `released` state, `stop_receipt_durable` with the durable
receipt (both already asserted by `linuxV2ValidateStageJournalBoundary`,
`owner_cli_test.go:1495-1596`) — the leg no longer requires the helper to exit *before* the
recovery. Instead:

- the pre-recovery helper-exit wait with its unchanged 90 s bound and unchanged message stays for
  every other helper stage;
- at the two released-gate stages the leg proceeds to the same-RunKey recovery, and then requires
  the helper's exit within 30 s (the recovery's `ReleaseAfterClear`+retire is what ends a resident
  helper: `linux_owner_recovery_linux.go:1381-1388`), so the leg still ends with a hard
  helper-exit assertion in every branch;
- the recovery response, the RunJournal tombstone (physical stop flags), the leaf-absence check
  and the post-recovery target-pidfd wait (`owner_cli_test.go:2041-2049`) are unchanged, so the
  owner-loss physical-stop and replay assertions keep their exact strength.

### 3.2 The decision is a pure, pinnable predicate

`linuxV2StageReplayHelperStopIsProven(stage, marker, journal)` carries the whole branch decision
so it can be pinned without the installed guest; the runner keeps only the two call sites. The
predicate is consulted *after* `linuxV2ValidateStageJournalBoundary` has already validated the
stage's exact durable shape, which is why it can key on the marker state / receipt presence.

### 3.3 Rejected alternative (product side)

Make a proven-stop owner-loss path end the helper itself (self-kill after publishing its evidence)
so the pre-recovery wait holds everywhere. Rejected: it contradicts the product's own recovery
design and the daemon-crash witness (record §2 item 4), it would need a new anchor-retained
evidence channel plus e2e changes in files this goal does not own, and the contract routes
production changes outside the witness seam to DECISION_NEEDED. Recorded, not silently dropped:
if the conductor reads the residency as a product defect, that is a new goal with this plan's
§2/§3.3 as its diagnosis.

## 4. Public/API shape

No production, validator, harness or fixture-script change; no new exports. One new unexported
predicate and one table row in `daemon/e2e/production_linux_v2_owner_cli_test.go` (in the tree,
uncommitted — the conductor commits the stage), plus the runner sequencing above. Nothing else in
the module changes.

## 5. Per-clause baseline and proof plan

| Contract clause | Base behavior (before) | Clause test / proof |
| --- | --- | --- |
| 1 root cause named with source + log evidence | the retained red at `owner_cli_test.go:1972-1979` (base anchor `:1958-1964`); the four candidates | plan §2 with file:line; the record's census rows; the WSL scratch residency probe (unproven→success control flow): a helper with cached stop evidence returns from `markOwnerLost` without self-kill and without closing its listener |
| 2 fix on the wrong side, no lengthened bound | the wait is 90 s > the 30 s product bound; the released-gate stages can never satisfy it | the predicate row (`TestLinuxV2StageReplayHelperStopIsProven`) plus the runner branch; the 90 s and 30 s budgets unchanged/new-but-guaranteed (`helper_v2_stop.go:1310-1320`) |
| 2 never weaken the owner-loss/containment/replay assertions | — | the unchanged validators: `linuxV2ValidateStageReplayResponse` (`:1144-1150`) / `…ResponseState` (`:1175-1309`), `…StageReplayJournal` (`:1311-1417`), the target-pidfd wait (`:2041-2049`); the helper-exit assertion is re-sequenced, not removed |
| Invariants (≤2-line comments, `go vet`, no host-layout tests) | — | comment audit; `go vet`; the row uses only struct literals |
| Acceptance (conductor Bind: the stop is gone) | — | carried to the conductor; this stage records the local rows |

## 6. Local gates

From `daemon/`: `gofmt -l .`; `go vet ./...`; the FULL `go test ./...` on Windows and in WSL
Ubuntu-24.04 (sequenced, never beside another harness); the predicate row alone in WSL; the
mutant row with the predicate inverted (via `go test -overlay`, never written into the tree) to
show red-first; the comment-cap script before the final re-measure; the record sanitizers on the
new records. Scratch work lives in the operator's scratch root, never in the worktree.

## 7. Out of scope (with reasons)

- Any `daemon/internal/platform/**` or `execution/**` change (not owned; §3.3 records the option).
- The `stop_receipt_durable` and remaining released-gate legs' *next* stops after this wait (for
  example the pre-checkout clone-retention reconciliation): the conductor's Bind records them
  verbatim; this goal removes only the asserted stop.
- The `shared` set's pre-existing reds and the `codex_containment` leg's own next stop: reported,
  never repaired here.
