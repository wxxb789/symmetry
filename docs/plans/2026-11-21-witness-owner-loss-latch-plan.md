# witness-owner-loss-latch plan — latch owner loss at every supervisor recovery entry

Root cause: the flake analysis of CI run `44876100236` (2026-11-21), whose evidence root is kept
locally (`flake/run-44876100236/`, `flake/root-cause-1.md` — local state, not committed). Baseline
for every coordinate below: `a503eab79e4bee55f48e11b26a4b42371e068ed1` (`a503eab7`, this worktree's
HEAD = `origin/main` at creation). Every cited daemon file is byte-identical between the failing
revision `6a815c0fc72393f9ccc1cdde5a516a2f8176eb9c` and `a503eab7`
(`git diff --quiet 6a815c0f a503eab7 -- <file>` per file: `supervisor_linux_helper.go`,
`process_unix.go`, `containment_supervisor_linux_test.go`, `recovery_glue.go` all identical; the
only daemon changes in between are the `cleanup_glue.go` refactor #159 and the authority split
#158). Touched packages: `daemon/internal/platform` (fix + regression test), `daemon/internal/app`
(one diagnostic log field), `docs/` (this plan, TODO closure). No wire, contract, schema, workflow
or `daemon/e2e` change. The fix commit on branch `fix/witness-owner-loss-latch` is
`e9c457d8b9f6491d888ca649835d22190991dec2` (`e9c457d8`), parent `a503eab7`.

## Verified state at the baseline

The failing CI subtest `TestProductionLinuxContainmentWitnessSpine/daemon_sigkill_after_lease_arm_before_resume`
stalls 45 s waiting for the `stop-receipt-before-release.reached` marker: the restarted daemon
emits 18 × `stop_recovered_process_failed` (first 7.8 ms after `runtime_registered`, then a 30 s
lease-expiry gap and a 1 Hz train) and never reaches the receipt callback. Evidence: the witness
artifact's frozen journal (`has_stop_receipt:false`, helper scan `completed_sequence:1`, mirror
sealed 2, target gone), the second daemon's exported sanitized tail, and the whole-stop failure
matrix in `root-cause-1.md` §2–§4.

Confirmed chain (root cause §3):

1. The first daemon is SIGKILLed. The helper observes the **control-pipe EOF** before the owner
   watchdog goroutine has published `close(lost)` on the owner pipe
   (`linux_supervisor_protocol_linux.go:169-206`).
2. `enterRecovery` (`supervisor_linux_helper.go:923-928` at the baseline) evaluated
   `ownerLostLatched = ownerLostLatched || channelClosedV2(watchdog.ownerLost())` and therefore saw
   `false`; the first `stop()` computed `ownerLostStop = false` and took the **live-owner branch**
   (`:838`, `:842`).
3. That branch's `closeUntil` has to finish the descendant proof with a final monitor scan
   (`process_unix.go:1192-1208`). The scan persists its `begin` fence over the helper→daemon
   **metadata socketpair** (`process_unix.go:865-871` → `reportLinuxSupervisorScanEvent`
   `supervisor_linux_helper.go:348-387`), whose daemon-side peer died with the SIGKILLed daemon.
   The write/ack fails, `fail("pre_scan_ack_failure", …)` sets the sticky `descendantScanLost`
   (`process_unix.go:840-856`), and `closeUntil` converts the failed final scan into the sticky
   unproven latch (`:1247-1252`) → `closeContainmentUnproven`.
4. Every stop the restarted daemon then sends over the recovery endpoint is refused fast
   (`prepareDescendantMonitorForOwnerLoss`, `process_unix.go:779-803`, "owner loss interrupted
   descendant observation" once the watchdog had latched) or locally (`forceStopUnprovenClose`,
   "descendant observation was uncertain" while the watchdog had not), so the stop receipt is
   never minted and the 45 s harness barrier expires.

The owner-loss branch never writes to the owner transport: `prepareDescendantMonitorForOwnerLoss`
sets `skipNextDescendantScan` + `ownerLossSealPendingRecovery`, `closeUntil` skips the final scan,
and `sealOwnerLossScanAfterStop` (`process_unix.go:520-577`) seals from local `/proc` state and
moves the sequence to previous+1. The same tree passes ~98 % of the time because the watchdog
usually wins the race; the regression test below constructs the losing interleaving deterministically.

## Control-flow audit of `enterRecovery` callers (baseline vs HEAD)

`enterRecovery` is a closure in `runLinuxSupervisorHelperLoop`; `grep -n 'enterRecovery()'` finds
exactly nine call sites at the baseline: `:952, :973, :979, :984, :1002, :1013, :1018, :1033,
:1092` (HEAD after the fix and the round-1 comment correction: `:977, :998, :1004, :1009, :1027,
:1038, :1043, :1058, :1117`):

| # | baseline line | trigger | owner-transport-loss evidence |
| --- | --- | --- | --- |
| 1 | `:952` | `case <-ownerLost` — watchdog publication | the watchdog latches on owner-pipe EOF/read error; this arm already sets `ownerLostLatched` itself |
| 2 | `:973` | control-pipe read EOF/error (`requestEvents`) | the owner's only command transport is gone; the in-code comment already declares it an owner-loss boundary |
| 3 | `:979` | unbound abort: `stop()` failed **and** the error-response write failed | response pipe reader is gone (below) |
| 4 | `:984` | unbound abort: `"aborted"` response write failed | same |
| 5 | `:1002` | bind response write failed | same |
| 6 | `:1013` | `!bound` abort/stop/close: `stop()` failed and the error-response write failed | same |
| 7 | `:1018` | `!bound` abort/stop/close: `"stopped"` response write failed | same |
| 8 | `:1033` | bound pre-commit abort response write failed | same |
| 9 | `:1092` | main-switch response write failed | same |

Why a failed response write is an owner-transport-loss boundary and cannot misclassify a reachable
live owner — with the review round 1 corrections noted after the list:

- The response pipe's read end is held only by the daemon (`linuxSupervisor.responseRead`), read in
  `request()` (`supervisor_linux_client.go:1511-1530`). A live but slow reader makes the write
  **block**, not fail; reaching arm 3/4/5/6/7/8/9 requires every read-end descriptor to be closed.
- The daemon closes `responseRead` only (a) together with `ownerWrite` and `controlWrite` in
  `shutdown`/`Close`/`Release` (`supervisor_linux_client.go:1159-1174`, `:1308-1314`,
  `:1394-1403`), or (b) in `dropResponseTransportOnce` (`:1560-1572`), the explicitly gated
  production-witness response-loss seam, which closes `controlWrite` in the same critical section
  and immediately marks `responseTransportLost` — the owner has deliberately abandoned the in-band
  channel and the recovery endpoint is the designed migration (the `stop-response-lost` /
  `release-response-lost` witness cases).
- The one stop that is *not* an owner-transport boundary — the lease-expiry retry
  (`supervisor_linux_helper.go:978-993` at HEAD) — deliberately stays on the live-owner branch and
  does not call `enterRecovery` at all. The pre-commit/unbound abort path is likewise unchanged:
  owner-loss preparation is gated on `ownerLostStop && (bound || resumed) && !precommitAbort`, and
  `precommitAbort` returns through `closePreparedAbort` before the scan assertion.

Corrections to this audit (review round 1, finding F1) — two entries can be reached while the owner
process is still alive:

- Call site 2 (control EOF/error) is reachable with a live daemon: the daemon itself closes its
  control writer on any response-transport loss (`request()`'s already-lost/control-write/
  response-read failures, `closeControlAfterResponseLossRequestLocked`, called by
  `recoverResponseLossStop` and `ReleaseContainment`) while `ownerWrite` and the metadata transport
  stay open. `TestLinuxSupervisorControlEOFFailsClosedBeforeOwnerLoss` constructs exactly that
  state. The bullet list above only covers the daemon *exiting* or the gated witness seam, not this
  live-owner close.
- A response write can fail before any pipe I/O with a live reader:
  `writeLinuxSupervisorResponseV2` returns the `ProcessIdentity(os.Getpid())` error, and
  `writeLinuxSupervisorFrame` returns a marshal error or `errLinuxSupervisorFrameTooLarge`. Arms
  3–8 stay inert on the latch (`precommitAbort`), and arm 5 is only reachable on a bind replayed
  after commit; arm 9 (main-switch write) does take the latched owner-loss branch with a live
  daemon, which is then blocked on its deadline-free response read until it restarts — the
  restarted-daemon consumer records the returned seal correctly.

Measured consequence for a live owner (review probe C): the helper takes the owner-loss branch,
stops its monitor before the kill and seals locally at previous+1 with no observer writes, so the
owner's durable helper progress keeps its pre-latch sequence (`completed=1, sealed=0`). The
restarted-daemon consumer records the returned seal (`RecordSupervisorRecoveryScanSeal`,
Completed+1) before the receipt; the in-process `recoverResponseLossStop` consumer does not, so
`assert_all_sealed` refuses and that stop fails closed into a durable containment-unproven mark.
Pinned by `TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed`. Conclusion of the
audit after the correction: the latch covers every recovery entry, but not every entry is an
owner-transport-loss event — the live-owner entries above are the semantic delta, and the follow-up
choice (record the returned seal in the in-process path, or keep the live-owner branch for
live-owner entries via root-cause §5 P2) is tracked in `docs/TODO.md`.

## Fix

`daemon/internal/platform/supervisor_linux_helper.go`, `enterRecovery` (baseline `:923-928`):
replace the watchdog-channel read with the unconditional latch, keeping the authenticated
endpoint and the `stop()`+recovery-loop shape:

```go
enterRecovery := func() error {
    // The paths that reach here are the owner watchdog publishing owner loss,
    // the control pipe reaching EOF/a read error, or a response write failing.
    // … two of them can be reached while the owner process is still alive
    // (full comment in the file).
    ownerLostLatched = true
    _ = stop()
    return runLinuxSupervisorRecoveryLoop(…)
}
```

Invariant restored: *a stop attempted after the owner transport is unusable must seal descendant
scans with the local owner-loss seal and must never depend on persisting scan events to the owner.*
The recovery response carries the local `recoverySealSequence` (previous clean sequence + 1), which
the restarted-daemon path records durably with `RecordSupervisorRecoveryScanSeal`
(`state.go:3786-3823`, call site `daemon/internal/app/recovery.go:449-466`) before the receipt; the
in-process response-loss consumer does not yet (audit corrections above, `docs/TODO.md`).

Fences and idempotency preserved (unchanged code, re-stated as the fence inventory):

- The latch is the existing linearization point (`leaseState.latchOwnerLost`); once set, the owner
  can no longer renew or resume over a dead control pipe anyway.
- The recovery endpoint still authenticates the exact helper authority + bound secret and validates
  the full launch/target identity before touching anything (`supervisor_linux_helper.go:1157-1174`).
- `stop()` stays memoized by `receipt`; `sealOwnerLossScanAfterStop` is memoized by
  `descendantScanSealed` and re-checks every guard under `scanSealMutex`+`group.mutex`;
  `RecordSupervisorRecoveryScanSeal` accepts exactly `Completed+1` and replays idempotently;
  `persistContainmentStopReceipt` and the barrier keep their CAS/readback replays. Replayed stop
  requests return the same receipt and seal sequence.

## Regression test

`TestLinuxSupervisorControlEOFWithDeadOwnerMetadataUsesOwnerLossSeal`
(`daemon/internal/platform/containment_supervisor_linux_test.go`, appended after the existing
`TestLinuxSupervisorControlEOFFailsClosedBeforeOwnerLoss`; base/HEAD line `:3153`). It mirrors the
pre-resume crash state with the real helper binary and constructs the racy interleaving with no
scheduler dependence:

1. Start/Commit, `RenewLease(30s, 1)` (keeps the helper's lease timer out of the window), no
   `Resume`; wait on the `Prepare` callback until the helper's initial clean scan is durably
   acked (`Sequence == 1`, `Phase == retire`, `Owner == helper`, no failure kind).
2. Close the daemon-side metadata connection (`supervisor.closeScanTransport`), then close the
   control pipe while `ownerWrite` stays open — the control EOF is the only owner-loss boundary and
   the helper's next scan persist is guaranteed to hit a closed peer.
3. One `StopPersistedLinuxSupervisorWithScanSeal` call must mint a receipt with
   `sealSequence == 2` (previous clean sequence 1 + 1), prove target/process-group absence, and
   release. The stop succeeding over the closed metadata transport is the observer-free invariant;
   the scan-update counter that originally accompanied it was removed in review round 1 (finding
   F4) because `closeScanTransport` joins the scan server first, so the counter could never fail.
4. Cleanup kills the helper process if a red run leaves it alive (the CI occurrence's own failure
   shape: the helper survived its stopped owner), so repeated red runs leak nothing.

`TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed` (review round 1, finding F1)
pins the other side of the latch: a daemon-side control close with a live owner. It starts the
supervisor with a test double for the app's scan-persistence callback (per-owner progress plus the
`assert_clean`/`assert_all_sealed` rules), commits, renews a 30 s lease, closes the control writer
exactly as `closeControlAfterResponseLossRequestLocked` does, and calls `Terminate(true)`. The
helper seals locally at sequence 2 with no observer writes, the durable all-sealed assertion is
refused, the stop fails closed with `ErrLinuxSupervisorStopUnproven`/`ErrLinuxSupervisorResponseLost`
and exactly one containment-unproven callback, and the helper's terminal seal is still available
over the recovery endpoint (`sealSequence == 2`). Run against the pre-latch helper
(`git show a503eab7:…supervisor_linux_helper.go` through `go test -overlay`), the same test fails
at the first assertion with `<nil>`: the live-owner branch minted the receipt through observer
writes.

## Diagnostics (root cause §7 D1)

`daemon/internal/app/recovery_glue.go:381`: the `stop_recovered_process_failed` warning now carries
`"reason", reason` — one line, no behavior change. Every value reaching this field is already in the
exporter's `reason` allowlist (`scripts/ci/export-linux-v2-redacted-evidence.py`,
`DAEMON_LOG_TEXT_VALUES["reason"]`): the `terminateForLease` literals, the
`protocol.ReconcileDecisionKind` values, and the ownership reasons are all present, and a value
outside a field's set is dropped per-key while the record keeps `time`/`level` and the rest — never
a dropped line. The exporter's drift scan
(`scripts/ci/test-linux-v2-evidence.py::test_daemon_log_allowlists_match_daemon_producers`) only
collects literal values, so a variable-valued field adds no literal and the scan stays green (the
suite is run as a gate below). D2–D4 from the root cause are not needed and are not implemented.

## Gates

Filled by the runs below; all commands run on this host, WSL = Ubuntu-24.04, Go 1.27.0.

### Red evidence (origin/main, unfixed, deterministic)

The regression test was added first on the unfixed tree. `go test ./internal/platform/ -run
'^TestLinuxSupervisorControlEOFWithDeadOwnerMetadataUsesOwnerLossSeal$' -count=20` in WSL:
`rc=1`, **20/20 failures**, each in 0.06–0.13 s, every one through the same sticky
`descendantScanLost` latch and `ErrLinuxDescendantContainmentUnproven` class — with the owner
writer still open the message is `stop after control EOF with a dead owner metadata transport =
Linux descendant containment is unproven: descendant observation was uncertain` (the local
`forceStopUnprovenClose` path); once the owner pipe closes, root-cause §3's text appears instead,
`owner loss interrupted descendant observation`, which is what the CI retries would have read (the
CI error text itself was never exported). `leaked_helpers=0`. Single-run transcript in
`root-cause-1.md` §6 vocabulary: pre-fix the first stop's final scan persist dies on the closed
peer, latches sticky descendant uncertainty, and the recovery-loop retry is refused locally.

### Green evidence

`go test … -run '^TestLinuxSupervisorControlEOFWithDeadOwnerMetadataUsesOwnerLossSeal$' -count=50`
after the fix: `rc=0`, 0 failures, receipt with `sealSequence == 2`, `gofmt -l` clean on both
changed files.

### Full gates

This WSL guest cannot connect to its own loopback: a 5 s listen/connect probe against 127.0.0.1
fails (`connect failed after 5.01s: TimeoutError('timed out')`), the same host limitation recorded
in the archived `fix/stop-descendant-recheck` entry. The runs below therefore name-skip the tests
that dial 127.0.0.1, and the excluded names are recorded with each gate.

- Platform suite: `go test ./internal/platform/... -count=1 -timeout 1800s -skip
  "^Test(VerifyLoopbackTCPPeerWithOwnedHelperProcess|VerifyLoopbackTCPPeerWaits|VerifyLoopbackTCPPeerDoesNotExtend|VerifyLoopbackTCPPeerV2|LinuxTCPPeerV2RejectsDifferentNetworkCookie)"`
  (the six documented TCP-peer tests) → `ok … 22.930s`, rc 0. The same command **without** the
  skip fails after 832.2 s on exactly those six tests (`dial tcp4 127.0.0.1:…: connect: connection
  timed out`, 134–135 s each) and passes everything else.
- Focused race run (execution/concurrency change, daemon `AGENTS.md`): `go test -race
  ./internal/platform/ -run '^TestLinuxSupervisorControlEOF' -count=10 -timeout 900s` →
  `ok … 28.150s`, rc 0.
- App recovery tests: `go test ./internal/app/ -run 'Recover|Recovery|Witness' -count=1 -timeout
  1200s -skip '^TestRestartInputRecoveryRetryDelayUsesFallbackAndMinimumCadence$'` →
  `ok … 260.818s`, rc 0.
  Without the skip, only that test fails, in both subtests that dial their own `httptest` loopback
  server (`dial tcp 127.0.0.1:…: connect: connection timed out`; the "retry delay = (7s, true)"
  assertion is the transport-error fallback) — code the change cannot reach.
- `GOOS=linux go vet ./internal/platform/... ./internal/app/...` → rc 0.
- `GOOS=windows GOARCH=amd64 go build ./...` (daemon module) → rc 0.
- `python3 scripts/ci/test-linux-v2-evidence.py` (WSL; exporter allowlist/drift suite, D1):
  `Ran 36 tests … OK`, rc 0 — the added `reason` field introduces no literal, so the drift scan
  stays green and the projection is unchanged for every other record.

CI remains the merge gate for the gated production witness matrix itself
(`TestProductionLinuxContainmentWitness*` needs the privileged CI runner and its witness env).

## Out of scope

- Root-cause §5 P2 (do not latch sticky `descendantScanLost` on a final-scan persistence failure;
  clear `descendantSealPending` on observer seal failure): defense in depth, not required for the
  observed interleaving, and it changes retry semantics — a separate increment, now tracked in
  `docs/TODO.md` as the residual for the lease-expiry arm (review round 1, finding F2).
- Recording the helper's returned recovery seal in the in-process `recoverResponseLossStop`
  consumer (option (a) of review finding F1): a follow-up increment, tracked in `docs/TODO.md`.
- The 45 s harness barrier and the 30 s daemon receipt bound: declared bounds, untouched.
- `daemon/e2e` witness code: unchanged; the platform-level regression test is the deterministic
  pin, and the CI matrix is the end-to-end confirmation.

## Residual risks

- The exact originating error text (EPIPE vs ack-EOF suffix) was never exported for the CI
  occurrence; D1 now names the retry *reason*, and the next failing artifact can cross-check the
  stitch (`reason:"lease expired"` / a boot reason plus the unproven refusal). The fix removes the
  branch that writes to the dead transport regardless of which suffix produced it.
- The new test pins the control-EOF entry (call site 2) because it is the observed one; the audit
  above covers the other eight entries by code reading, and the response-write arms are exercised
  by the existing response-loss fixtures (which use fake servers and are unaffected by P1).
- The latch also covers live-owner entries (control EOF from a daemon-side response-loss close and
  non-I/O response-write failures), where the local owner-loss seal is not yet consumed by the
  in-process path and the stop fails closed into a durable containment-unproven mark; pinned by
  `TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed` and tracked in `docs/TODO.md`.
- The lease-expiry arm (`supervisor_linux_helper.go:978-993`) stays on the live-owner branch: if
  the owner dies or wedges as the lease expires and the timer arm wins the `select`, the same
  sticky refusal class is still reachable (review finding F2; root-cause §5 P2 direction in
  `docs/TODO.md`).
