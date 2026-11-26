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

| file | sha256 at the delivered bytes |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `11616074bc08516038d79cca2346a2b93967d7283287ef962344fd6d06ac4939` |
| `docs/plans/2026-11-26-rf-acc-claude-stage-replay-plan.md` | `54ad76943ca377afcd680c36760b075c20bdce3c4b027559ba2d0f23de0483f7` |

`docs/TODO.md` and `docs/archive/CLOSED-TODO.md` are deliberately not hashed anywhere. No production,
validator, fixture-script or `pi/**` byte changed (§4).

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
is `marker.Owners.Helper` opened at `production_linux_v2_owner_cli_test.go:1933`
(`linuxV2OpenStageOwnerPIDFD` re-verifies the exact creation identity across the open) — the
`-linux-owner-helper-v2` process (pid 7168 in that run), **not** the released B target (pid 7182,
captured live at `:1939-1962`; artifact `claude-stage-crash-replay-native-release.json` shows it as
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
4. **The live-helper-after-owner-loss state is the product's supported behavior.** The recovery has
   a dedicated live-helper branch that asks the resident helper for its exact stop evidence
   (`app/linux_owner_recovery_linux.go:1242`, `1271-1292`) and then ACKs the receipt, releases the
   helper after the durable clear and retires it (`:1337-1340`, `:1381-1388`); the daemon-crash
   witness *requires* it — the helper must survive the daemon SIGKILL
   (`production_linux_v2_native_witness_test.go:2553-2556`) and the recovered tombstone's stop proof
   must be helper-issued with no helper exit (`:2628-2632`, `proof.Kind == helper`,
   `proof.HelperExit == nil`).
5. **The fixture contradicts that at exactly the released-gate stages.** The post-kill wait
   (`production_linux_v2_owner_cli_test.go:1972-1979`, the fixed bytes; the base anchor is
   `:1958-1964`, where the same 90 s wait fatals unconditionally) holds for
   every stage in its helper list (`:1930-1932`). At the pre-release stages the same wait passes
   because there the stop is unproven (no gate authorization, no authorized exec transition), so
   `stopOwned` errors and the helper self-kills — the guest history records exactly that
   (`docs/evidence/rf-acc-owner-loss-semantics-2026-11-25.md` §8/M12: the pre-release fresh-request
   leg observes the helper's owner-loss exit at 0.23 s in green runs). At `native_release` and
   `stop_receipt_durable` — the two stages whose durable boundary proves the released gate
   (`linuxV2ValidateStageJournalBoundary:1554-1563` for `native_release`, `:1570-1574` for
   `stop_receipt_durable`) — the stop is proven, so the wait can never
   be satisfied. Contract candidate “the claude process tree outlives the owner” is excluded: no
   step of the owner-loss path is harness-specific, and the retained artifact binds the claude
   target as the single exact leaf member.

`[INFERENCE]` on one link: the retained artifacts do not carry the helper's in-memory state, so
“the stop succeeded” is inferred from the code paths in items 2 and 3 (every error path returns to
`watchOwnerPipe`, which then SIGKILLs the helper within the 30 s bound; no wait on the
released-gate path ignores its context) plus item 4's product requirement. The fix below is safe
under the alternative reading (a stop that hangs): the recovery would then fail on the helper's own
in-flight stop bound and the Bind would record that exact next stop.

Not a production defect in `daemon`/`execution` code outside the witness: the product's recovery
design (item 4) needs this state, so Stage 2 fixes the fixture side. The rejected product-side
alternative (make a proven-stop helper end itself) is recorded in the plan §3.3.

## 3. The fix

- `production_linux_v2_owner_cli_test.go` gains one pure predicate,
  `linuxV2StageReplayHelperStopIsProven(stage, marker, journal)` (`:1162-1173`): `native_release`
  with the held marker's `released` state, `stop_receipt_durable` with the durable receipt — the
  two shapes the stage-boundary validator has already validated exactly
  (`linuxV2ValidateStageJournalBoundary`, `:1495-1596`).
- The crash-replay runner keeps the 90 s pre-recovery wait for every helper stage (so a slow
  unproven stop is waited out before the recovery starts, and no new race is introduced), but fails
  on it only when the stage's stop is *not* proven (`:1972-1979`).
- At the proven stages the runner then requires the helper's exit within 30 s after the same-RunKey
  recovery (`:2050-2055`), which the recovery's `ReleaseAfterClear` + retire guarantees
  (`app/linux_owner_recovery_linux.go:1381-1388`); a helper that outlives the whole leg still reds.
- No validator, bound, production, harness or fixture-script change: the response validator
  (`linuxV2ValidateStageReplayResponse` `:1144-1150`, `linuxV2ValidateStageReplayResponseState`
  `:1175-1309`), the journal tombstone validator (`linuxV2ValidateStageReplayJournal`
  `:1311-1417`), the post-recovery target-pidfd wait (`:2041-2049`) and the leaf checks stay
  byte-identical.

## 4. Red-first and mutant rows

The new branch is pinned by the row `TestLinuxV2StageReplayHelperStopIsProven` — `:2352-2382`,
8 rows: `released native_release
hold` and `stop-receipt durable hold` positive; the not-released/unknown marker, the receipt-less
authority, the authority-less durable hold, and both pre-release shapes negative — and reddens
when the `native_release` term is inverted. Mutant (scratch overlay, never written into the
tree; `go test -overlay=… -run '^TestLinuxV2StageReplayHelperStopIsProven$' ./e2e/`, WSL):
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

The delivered bytes pass the same row (`ok e2e`, 8/8 subtests, §6).

## 5. Root-cause probes (scratch, WSL, never in the tree)

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

| Gate | Command (working directory `daemon/`) | Exit | Result |
| --- | --- | --- | --- |
| formatting | `gofmt -l .` (Windows) | 0 | empty list |
| vet | `go vet ./...` (Windows, go1.27.4) and (WSL Ubuntu-24.04, go1.27.0) | 0 / 0 | no findings on both surfaces |
| predicate row | `go test -count=1 -v -run '^TestLinuxV2StageReplayHelperStopIsProven$' ./e2e/` (WSL) | 0 | `ok e2e`, 8/8 subtests PASS |
| mutant row | `go test -overlay=… -count=1 -run '^TestLinuxV2StageReplayHelperStopIsProven$' ./e2e/` (WSL) | 1 | §4: 3 rows red, `MUTANT-EXIT=1` |
| probes | `go test -overlay=… -count=1 -v -run '^TestScratch…$' ./internal/platform/` (WSL) | 0 | §5: both PASS (0.03 s / 0.04 s) |
| full suite (Windows) | `go test -count=1 ./...` | 0 | 27 `ok`, 3 `no test files`, 0 FAIL, 4m04.5s |
| full suite (WSL, unprivileged `accept`, CI umask) | `umask 022; go test -count=1 ./...` | 0 | 31 `ok`, 3 `no test files`, 0 FAIL, 5m11.8s |
| full suite (WSL, `accept`, as-is umask 0002) | `go test -count=1 ./...` | 1 | 28 `ok`; `internal/linuxpreflight` 2 + `internal/platform` 8 + `internal/workspace` 21 tests fail — the same 31 at the base revision (row below) |
| full suite (WSL, root) | `go test -count=1 ./...` | 1 | 30 `ok`; `internal/platform`'s 13 `TestLinuxOwnerAnchorShape*`/`…Wire*` tests refuse with the documented root guard (`wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`) |
| base attribution | `go test -count=1 ./internal/linuxpreflight/ ./internal/platform/ ./internal/workspace/` in a pristine `git archive 78166f2e` tree (WSL, `accept`, as-is umask) | 1 | the identical 31 failing test names (set difference empty both ways) |
| comment cap | `python …/comment-cap.py <worktree>` (official) + the uncommitted-diff scan | 0 / 0 | no added comment block over 2 lines |
| sanitize | `python scripts/ci/model-routing-record-sanitize.py --check --root-only` per changed doc | 0/0/0/1 | record/plan/TODO pass; `CLOSED-TODO` exit 1 identical at base (pre-existing quoted specimens; the added lines carry no host path) |
| duplication (jscpd) | `python scripts/ci/jscpd-changed.py --base 7111828b --head HEAD` (scratch clone, pinned `node_modules/jscpd` 4.3.0) | 0 | 5 changed file(s), 1775 scanned source(s), 1254 clone(s) in the corpus, 0 tolerated, none new; notices: the changed 5,046-line Go test file and `CLOSED-TODO.md` exceed the audited `--max-lines 5000` ceiling, reported, never silently skipped |

The full suites ran one at a time, never beside another harness. The `accept`/022 row is the
CI-parity surface (the CI daemon job runs as the non-root runner user); the as-is `accept` and root
rows are carried because they expose this host's own preconditions, and both failure sets are
attributed: at the same umask the base revision fails the same 31 tests, and the root refusals are
the documented root guard.

## 7. Residual risks and limits

- The `[INFERENCE]` of §2 (the helper's stop *succeeded* in the failing run) cannot be closed from
  the retained artifacts; the fix does not depend on it (a hanging stop surfaces as the recovery's
  own error, which the conductor's Bind would record verbatim).
- The claude leg's *next* stop after this wait is unknown until the Bind; the acceptance explicitly
  records it verbatim. The same sequencing change applies to the codex kind leg and to the pi
  `native_release`/`stop_receipt_durable` stage legs, which share the runner; none of them has ever
  been observed green at a released-gate stage, so their next stops are equally the Bind's to
  record.
- The product-side alternative (plan §3.3) remains available to the conductor as a new goal if the
  helper's post-stop residency is ruled a defect; it would need `platform/**` ownership plus a
  change to the daemon-crash witness's `Kind == helper`/`HelperExit == nil` requirement.
- `stop_receipt_durable`'s pre-recovery wait now tolerates a resident helper under the same
  predicate; the stage-replay matrix and the `stale_leaf` witness both exercise that boundary with
  their own recovered-evidence assertions unchanged.
- The changed `production_linux_v2_owner_cli_test.go` is 5,046 lines, above the duplication gate's
  audited `--max-lines 5000` ceiling, so `jscpd-changed.py` reports it unscanned (§6): clones
  inside its added lines are not checked by that gate, before or after this change. Raising the
  audited ceiling is a separate gate change, outside this goal's owned files.

## 8. Close-out

Delivered, pending the conductor's Bind. The closure row moves to
`docs/archive/CLOSED-TODO.md` with the plan/record paths; `docs/TODO.md` drops the entry that
carried this stop as its next stop. The last step ran the comment cap *before* this final
re-measure: `daemon/e2e/production_linux_v2_owner_cli_test.go` re-hashes to
`11616074bc08516038d79cca2346a2b93967d7283287ef962344fd6d06ac4939` and the plan to
`54ad76943ca377afcd680c36760b075c20bdce3c4b027559ba2d0f23de0483f7`, both unchanged across every
gate row above; the §2/§3/§4/§5 anchors were re-read at these bytes.
