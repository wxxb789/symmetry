# rf-acc-owner-loss-capture-exit — evidence record

Contract: `goal-rf-acc-owner-loss-capture-exit.md` (GDD state root, outside the repository).
Dispatch base: `d45e3298` (#305, `main`). Plan:
`docs/plans/2026-11-27-rf-acc-owner-loss-capture-exit-plan.md`.

## 1. Subject bytes (sha256)

Historical delivered bytes at PR head `fb728c798006e17a5e93eeca0db670ba588cb70b`
and post-S1 scratch receipt `6935505431d7158ffd6b09f7a1e28c8b046b9f7d`. The archived
Bind table uses this meaning of “§1 delivered digests.”

| File | Before (`d45e3298`) | Delivered |
| --- | --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `3bf974a170e1fe237c2eebe1428719fac4467705c7c1dfe7ac9d3e1945006dcd` | `1b645c6e0120df260633346e3c2aa43d3c37264ad950a9685bfa824e96f78cfd` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `f8cdf9b5ff54f5713a157c19e4e805fb6186e1eeb3ee8d272aea39926e42d6a1` | `4aee0ec96c054bc902f313ad7864d893f9f1c44365dc053849f48e24635c5563` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `1bde11fbcfda1e7175a07f4c8407d69435658426e135b52c9bcc12d350a235f4` | `d62606b97db63e62ddf286426fc8758977170d00a65c7c10039b4377ef3a0f53` |

Fix-round Go file deltas relative to that subject (§§3–6 cover fresh checks on these bytes):

| File | Fix-round SHA-256 |
| --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `9607c6196f70ca61c35db3a378e76032a13bf76c555f02426b499619924f8953` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `e5c6d99a87068602ce5898e35209d1fa8fd8ce39eb1c7a445d4e1330da64431f` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `751eaf4d5e4d29852b4fa23e4cec2be85a5b524ad5705f40e3cd2fc322a749a7` |

The implementation receipt `574b9c36` is superseded by `6935505431d7158ffd6b09f7a1e28c8b046b9f7d`.
Its e2e SHA-256 was `e0c971dfbaf81ea66adf33c8a4e2af3cecb6692748fd1f9dc7c2b5195c728f75`;
it did not match the post-S1 e2e bytes. The two app files did not change from implementation to
post-S1. The post-S1 receipt is the verified pre-fix receipt, superseded for the fix-round landing
decision by the exact-byte receipt in the PR handoff. The Bind is separate (§9).

## 2. Slice 1 — the early return and why the exit is terminal

### 2.1 The classification, verbatim

`platform.LinuxOwnerAnchorClient.LookupHelper`
(`daemon/internal/platform/linux_owner_anchor_linux.go:479-576`) returns a helper whose
`Control` is non-nil only for the anchor's `attached` status (`:575`); for
`helper_exited` it returns `Control == nil` plus the exit proof and the two run pins (`:497-525`).
The anchor serves `helper_exited` when `run.helperExit != nil`
(`daemon/internal/platform/anchor_session.go:260,290-292`), and `run.helperExit` is written only by
`linuxOwnerAnchorRun.waitHelper` (`daemon/internal/platform/anchor_run.go:1304-1321`) after
`linuxOwnerAnchorWaitPIDFD` proved the wait and reap of the exact helper identity (proof
`Waited: true, Reaped: true`, `:1316-1320`). No other assignment to `run.helperExit` exists in
`anchor_run.go`, `anchor_session.go` or `anchor_server.go`; `waitHelper` itself refuses an
already-exited run (`:1305`), so the value is monotone for the life of the run.

### 2.2 Why the latch and the stop proof cannot appear after the exit

- The owner-loss latch has exactly one writer: `linuxOwnerHelperV2.markOwnerLost`
  (`daemon/internal/platform/helper_v2_recovery.go:191-200`, called from `watchOwnerPipe`
  `:144-176`). It is the helper process's own field, and it is served only from inside that
  process's connection handlers (`helper_v2_server.go:706`, `helper_v2_stop.go:320`).
- The stop proof the snapshot stores (`status.StopEvidence.Proof`) is created by the same
  process's `stopOwned` (`helper_v2_stop.go:714`, cached `:890`/`:1014`; field
  `helper_v2_server.go:324`) and served only from that memory (`stopEvidenceSnapshot`
  `helper_v2_stop.go:275-296`, `writeResponse` `:316`). Neither field has a durable copy the anchor
  can serve: the anchor's own run entry keeps
  pins and the exit proof, not the helper's latch or its `LinuxCgroupStopProof`.
- `helper.Control.Status` is reachable only through `dialLinuxOwnerHelperV2`
  (`helper_v2_protocol.go:337-380`), which authenticates `SO_PEERCRED` against the exact helper
  `{PID, Identity}`; a reaped process cannot accept a connection, and no code path re-creates a
  helper for the same RunKey (`handleStartHelper` refuses an existing run; `handleLookup` serves
  `attached` only while `helperExit == nil`).
- Therefore, after the first `Control == nil` response for the exact held helper, every later
  `LookupHelper` for the same grant returns the same terminal classification (or `retired` / an
  error) and the only statements that set `Captured`/`OwnerLost`/`StopProof`
  (`captureLinuxV2NativeWitnessOwnerLoss`’s live `Status` arm) are unreachable. The re-dial can only re-observe the
  exit at 250 ms cadence until the 40 s bound.

### 2.3 What changed

The first exact terminal exit ends `captureLinuxV2NativeWitnessOwnerLoss` after closing the
lookup’s local pins. It does not widen terminal classification to a foreign PID / creation
identity. The 40 s capture budget, 250 ms poll cadence and live `Status` arm are unchanged.
The snapshot schema is unchanged: terminal on the first observation yields an uncaptured snapshot
with the held identity, and a partial earlier `Status` observation can still populate fields.
The exact terminal reason now wins immediately, rather than a later lookup error at the old
deadline (for example `LinuxOwnerAnchorClient.exchange` rejecting an expired context).
The unexported `linuxV2NativeWitnessOwnerLossAnchor` defaults to `owner.anchor`; the plan explains
the test seam. The fix-round change to this production file deletes the seam comment only.

## 3. Slice 1 — terminal classification rows

Historical red-first run on the seam-only revision (before the early return), exit 1:

```
$ go test -count=1 -run '^TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit$' ./internal/app
--- FAIL: TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit (40.10s)
    linux_owner_witness_linux_test.go:2580: terminal helper_exited capture dials = 161, want the single terminal classification
FAIL	github.com/wxxb789/symmetry/daemon/internal/app	40.121s
```

With the early return, `TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit`
passed in 0.00 s and verified one dial, the terminal-first field shape and the exact diagnostic.
The historical `m1_no_early_return` overlay removed the early return and gave 161 dials in
40.156 s (exit 1); the re-run below is only of the distinct foreign-exit control.

The foreign-helper row now cancels its context from the stub’s second lookup, not from a fixed
500 ms deadline. It still checks that capture made at least two calls, kept the unproven shape
and echoed the exact held identity and diagnostic. Cancellation does not supply the desired
result: the widened terminal result returns before the second lookup can request it.

Fix-round overlay `r4_widen_terminal.json` replaces `daemon/internal/app/linux_owner_witness_linux.go`
with `r4_widen_terminal.go` (SHA-256 `7918f96b69b695a1cf6faa3782e4292e7973da5e5d44bf4c558c710c6f20579f`).
Its sole edit is `exited := helper.Control == nil`, dropping the held PID and identity terms.
`go test -count=1 -timeout 20s -v -overlay=<overlay> -run
'^TestLinuxV2NativeWitnessOwnerLossCaptureKeepsPollingForAnotherHelpersExit$' ./internal/app`
exited 1: `foreign helper_exited capture dials = 1, want the poll to continue past one classification`.
The subsequent unoverlaid §6 targeted run exited 0; the foreign exit and exact terminal rows
passed in 0.25 s and 0.00 s, respectively.

## 4. Slice 2 — stage-replay fixture wiring

`runProductionLinuxV2StageCrashReplayCase` passes stage, exact marker and journal, named pins,
and the real waiters to `linuxV2WaitStageReplayPreRecovery` after the App kill and anchor liveness
check. The shared step derives the snapshot flag, guards both waits on the helper pin and
returns the same flag used later for snapshot checks. Previously the matrix derived and supplied
its own Boolean and held the guard outside the step; both were invisible to the local wait rows.

| Inputs | Wait before recovery | Snapshot required |
| --- | --- | --- |
| Helper pin and released `native_release` marker | Target containment, 90 s | Yes |
| Helper pin and `stop_receipt_durable` with durable receipt | None | Yes |
| Helper pin and any other stage/evidence combination | Helper exit, 90 s | No |
| No helper pin | None | Determined by stage/evidence, not by the pin |

The wait table tests this step through real stage evidence with recording waiters. It keeps
the exact arm, fd, and error-propagation checks, and compares each bound against a test-owned
90 s literal. Added cases cover an absent receipt, both handoff stages, and no helper pin with
either snapshot decision. The separate 13-row predicate and the snapshot/proof tests keep their
assertions. The helper response-loss fixture still calls the same predicate for its durable
receipt check; it only passes references to the same marker and journal instead of copying them.
The matrix’s actual pin acquisition and kernel waiters still require the guest.

### Fix-round overlays

Each `<name>.json` is a Go overlay replacing `daemon/e2e/production_linux_v2_owner_cli_test.go`
with the named scratch copy `<name>.go`, with only the edit listed. Invocations used
`go test -count=1 -timeout 20s -v -overlay=<overlay> -run '^TestLinuxV2StageReplayPreRecoveryWait' ./e2e`.

| Overlay (replacement SHA-256) | Exact edit | Named red observation, exit 1 |
| --- | --- | --- |
| `r1_invert_shared_arm` (`99859780ac7cc4194f5b041656e0a122345a15bca2ace518e2006cffdc383c34`) | Negate `linuxV2StageReplayOwnerLossSnapshotRequired(stage, marker, journal)` at the assignment feeding the actual fixture wait selection in the shared step | `…WaitArms/released_native_release_hold_takes_the_snapshot_arm_containment_wait`: `stage native_release pre-recovery wait arms = "helper_exit", want "target_containment" (snapshot required = false)` |
| `r1_shorten_wait` (`42e1f345382c2aace42d55bf8b6be825849e78bf193b7dd4d6820a0cb6275b33`) | Replace `linuxV2OwnerLossStopWait = 90 * time.Second` with `linuxV2OwnerLossStopWait = time.Second` | `…WaitArms/helper-bound_hold_keeps_the_unproven_helper-exit_wait`: `unproven arm helper-exit wait = pidfd 8 bound 1s, want pidfd 8 bound 1m30s`; released `native_release` also reported `bound 1s` vs `bound 1m30s` |
| `r1_drop_pin_guard` (`a8be916e8905d78244d0994193b34e1d4cc03cf8dce0b067aea64235faf6130f`) | Delete `if pins.helperPIDFD == nil { return snapshotRequired, nil }` before the switch | `…WaitArms/released_native_release_without_helper_pin_keeps_the_snapshot_without_a_wait`: `stage native_release pre-recovery wait arms = "target_containment", want "" (snapshot required = true)`; the unproven `leaf_removed` row received fd −1 |

Restored green: §6’s unoverlaid targeted run exited 0, including all 13 arm rows and both
error-propagation subtests. The older `m3_swap_arms`, `m4_drop_unproven_arm` and
`m5_widen_target_arm` ran against the post-S1 router, not this wiring; those receipts
are historical and are not the proof for this fix round.

## 5. Assertion homes and environment

| Assertion | Conductor Bind case sets | Linux local / CI home |
| --- | --- | --- |
| Exact terminal lookup ends the capture; foreign exit requires a second lookup | No local row; Run 45972088295 exercised live helpers, not the terminal return | `TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit` and `TestLinuxV2NativeWitnessOwnerLossCaptureKeepsPollingForAnotherHelpersExit` in `daemon/internal/app/linux_owner_witness_linux_test.go` |
| Arm, pin, fixed 90 s budget, propagated error and returned flag (including absent pin and both handoff stages) | No local row; the matrix exercises the real happy-path waits for its selected stages. The fix-round matrix has not yet been re-bound. | `TestLinuxV2StageReplayPreRecoveryWaitArms` and `TestLinuxV2StageReplayPreRecoveryWaitFailurePropagates` in `daemon/e2e/production_linux_v2_owner_cli_test.go` |

These four top-level local rows are `linux && amd64` and run in the `coverage-daemon` job
(`.github/workflows/ci.yml:682`) and the `daemon` Linux race pass (`:1063`), not `daemon-windows`.
The pre-fix Bind selected `TestProductionLinuxV2StageCrashReplayMatrix/<stage>`
(`scripts/ci/linux-v2-native-acceptance.sh:2545`), which does not select those local rows.
`TestLinuxV2NativeWitnessOwnerLossCaptureKeepsTheRecoveryTimeout` checks the cleanup budget with
a substitute capture, not the real live-helper arm. The Bind supplies the live-helper evidence.

Runner-mirror deviation: CI runs Ubuntu 26.04 Resolute; the fix-round assertions are Go logic
checks, not permission, kernel-readiness, runner-environment or workflow assertions. Recording
waiters do not poll a pidfd or time a wait, and the foreign-exit result is driven by lookup count.
No Resolute red/green was run or claimed. WSL printed `VERSION_CODENAME=noble` from
`/etc/os-release` and used `go1.27.0 linux/amd64` as user `accept` (`umask 022`).

## 6. Local gates

The post-S1 receipt `6935505431d7158ffd6b09f7a1e28c8b046b9f7d` measured the whole daemon module:
WSL `go test -count=1 -timeout 900s ./...` exited 0 (31 packages, 222 s wall); Windows exited 0
(27 packages, 259 s including vet). Those timings and results are historical, not fix-round gates.
The older implementation timings (WSL 267 s, Windows 232 s, both exit 0) belong to `574b9c36`
and were already superseded by the post-S1 receipt.

Fix-round results from `daemon/` (Windows Go `go1.27.4 windows/amd64`):

| Command | Exit | Observation |
| --- | ---: | --- |
| Windows and WSL `gofmt -l` on the three Go files in §1 (once per OS) | 0 / 0 | empty output |
| Windows and WSL `go vet ./internal/app ./e2e` (once per OS) | 0 / 0 | Linux-tagged code compiled in WSL, not on Windows |
| WSL `go test -count=1 -timeout 20s -v -run '^TestLinuxV2(NativeWitnessOwnerLoss\|StageReplay\|OwnerLossSnapshotBeforeRecovery)' ./internal/app ./e2e` | 0 | app 0.533 s; e2e 0.326 s |
| WSL `go test -count=1 -timeout 40s ./internal/app ./e2e` | 1 | e2e passed (1.400 s); app reached the imposed timeout in `TestOutputBackpressureRecoversWithOneTruncationMarker`, outside the witness slice. No app package-wide pass claimed. |
| WSL `go test -count=1 -timeout 20s -run '^TestLinuxV2' ./internal/app` | 0 | All Linux v2 app rows, 6.246 s |
| WSL `go test -count=1 -timeout 20s ./e2e` | 0 | Affected e2e package, 1.377 s |

## 7. Committed-blob gates (scratch clone)

The pre-fix post-S1 receipt recorded exit 0 for `jscpd-changed.py`, `check-record-cleanliness.py`
and `comment-cap.py`. The fix-round handoff carries the fresh receipt against the seven final
paths (including this record and the preserved Bind). The receipt and record digest are external
so that sealing the document does not invalidate its own gate. The commands in the receipt are
`jscpd-changed.py --base d45e3298 --head HEAD`,
`check-record-cleanliness.py --base d45e3298 --head HEAD`, and
`comment-cap.py <scratch-clone> d45e3298`, plus a direct root-only record check.

Historical anchor and input digest list for the pre-fix receipt; Bind uses this archived list:

`daemon/internal/platform/linux_owner_anchor_linux.go` `1adb63e19df20bdc32db315762206402e5e2f5079b53faddfe8db802befbf331`,
`daemon/internal/platform/anchor_session.go` `487cfcc104b6704dafe1ee7e604e2fd605f58a23d8d5376b65ebe1d9126e4e00`,
`daemon/internal/platform/anchor_run.go` `eebec5a3b6ee26866b9a41b8230812cf25f7570c06c8e27d2efd92d6b5b4f112`,
`daemon/internal/platform/helper_v2_recovery.go` `56c6cc8145cf273b79af6fcba55fee64ff25e7e0aa5b65f885e0288f9229b161`,
`daemon/internal/platform/helper_v2_stop.go` `f8d5b77f706177418f3309f51dd61aecd78c2c61910903de5e7352cdb068ab87`,
`daemon/internal/platform/helper_v2_server.go` `a00c6606f9da60b12322d93835f6d3f9d549cbce6b662dd5aefa1b8982c2da6b`,
`daemon/internal/platform/helper_v2_protocol.go` `4ac73de34f901c91a3cef10e3704e9530f520db321deb6af4e38669bc6eb9d75`,
`daemon/internal/app/linux_owner_witness_linux.go` `1b645c6e0120df260633346e3c2aa43d3c37264ad950a9685bfa824e96f78cfd`,
`daemon/internal/app/linux_owner_witness_linux_test.go` `4aee0ec96c054bc902f313ad7864d893f9f1c44365dc053849f48e24635c5563`,
`daemon/e2e/production_linux_v2_owner_cli_test.go` `d62606b97db63e62ddf286426fc8758977170d00a65c7c10039b4377ef3a0f53`,
`daemon/e2e/production_linux_v2_native_witness_test.go` `d988ca3bf8a0364f7d2f1c9aabe4cef14b53216e501a6002c49950e18d508abf`,
`.github/workflows/ci.yml` `f3e29fab798f010fb256dd0a49a78711abe232d84b9fc843461aa0d092fc73f7`,
`scripts/ci/linux-v2-native-acceptance.sh` `a7c453cb75958c218bd7a09dac4e56a1f749aef2f008e22886b23ac46307c360`.

## 8. Residuals

- The real anchor seam default needs the installed A/B configuration; the local capture rows
  substitute the anchor. The pre-fix Bind ran the real default and recorded live helper proof.
- Non-terminal outcomes, including `retired` and transient lookup errors, keep the bounded poll.

## 9. Acceptance by revision

**BINDING pre-fix run:** Run 45972088295 binds `fb728c798006e17a5e93eeca0db670ba588cb70b`.
Both sets passed and preserved the live owner-loss snapshot. The run did not emit capture elapsed
wall time; the configured budget is not a measurement. The archival Bind below is verbatim.
The conductor must re-bind the fixed head; this run does not cover the fix-round bytes.

## Bind 2

**BINDING on `5ccf14bd6ab292b1bbcbd0cfa53f1e77d160be73`:**
[Run 45985272938](https://github.com/wxxb789/symmetry/actions/runs/45985272938), attempt 1.
Run 45972088295 remains the pre-fix binding on `fb728c79` (§9); the archival Bind is in the
`5ccf14bd` revision of this record. Bind 2 now covers the fixed matrix discussed in §5.

Both containment sets were green: ten verified rows each, including
`TestProductionLinuxV2StageCrashReplayMatrix/native_release_claude` and
`TestProductionLinuxV2StageCrashReplayMatrix/native_release_codex`. Both export manifests were
`passed` with no missing allowlisted files. On each replay, the exported `owner_loss` snapshot
has `captured`, `owner_lost`, and `release_authorized` true, omits `error`, and names
the held helper as issuer and reaper of the `helper` stop proof (Claude PID 7240; Codex PID 7266).
This verifies the live-helper path, not §3's terminal-exit path or a measured capture duration.

The run commit `0b88140f2555f90e4a6bdc90ef8e6b0cdbe5cdd1` is a direct child of `5ccf14bd`.
It adds only the scratch workflow and gh shim, with the same bytes as the first Bind. GitHub
reported `total_count=1` from run creation through deletion of the branch and scratch worktree.

Run-head vs PR-head SHA-256 deltas from the pre-fix equality table (the other rows are unchanged):

| Executed input | Run head | PR head |
| --- | --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `9607c6196f70ca61c35db3a378e76032a13bf76c555f02426b499619924f8953` | same |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `751eaf4d5e4d29852b4fa23e4cec2be85a5b524ad5705f40e3cd2fc322a749a7` | same |

Downloaded ZIPs matched GitHub's SHA-256 digests:

- Native evidence `15191567837`: `8c39c1c18b89e48b4f2043f271e3e2ee61a4590af030503d5a6a44e9da514120`.
- Host prerequisite `15191282020`: `0f6cf23d19895d2b14c626b9ee7e04381b0be5757cb9f233789c2ab75880fac1`.
