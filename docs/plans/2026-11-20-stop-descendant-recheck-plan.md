# stop-descendant-recheck plan — give `stopDescendantMonitor` the post-timer recheck

Contract: `goal-stop-descendant-recheck.md` (GDD, 2026-11-20), branch `fix/stop-descendant-recheck`,
baseline `origin/main` = `fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, this worktree's HEAD).
Source item: `docs/TODO.md:104-122`, "Give `stopDescendantMonitor` the post-timer recheck its sibling
join already has." (line numbers in the item were written against `978abba`; every anchor below is
re-verified at `fdf332d`).

## Deliverable shape

1. `daemon/internal/platform/process_unix.go`: the post-timer recheck in `stopDescendantMonitor`
   (`:746-770`), same shape and comment style as PR #105 (`e64f8c0`) applied to the barrier join
   (`waitForInitialDescendantScan` `:665-688`, recheck `:682-686`). No exported symbol, signature,
   struct field or channel changes.
2. `daemon/internal/platform/process_unix_linux_test.go`: two probes (one deterministic, one
   deadline-race pin) placed next to the barrier pair, both `//go:build linux` like their siblings.
3. `docs/TODO.md` + `docs/archive/CLOSED-TODO.md`: the item moves out with its closure statement
   (branch, decision, pinning test names); the residual the item itself names (the final-scan RPC's
   timer arms) stays open as its own entry, so the archive does not claim more than it measured.
4. This plan, and the before/after runs in the stage's `answer.md` (outside the repository).

## Public/API shape and exact edge-case semantics

`stopDescendantMonitor(deadline)` is package-internal; the observable surface is its error, consumed
by `closeUntil` (`:1121`, `:1277`, `:1351`), `closePreparedAbort` (`:1542`),
`containment_supervisor_linux.go:1836` and `prepareDescendantMonitorForOwnerLoss` (`:771-794`), where
any error becomes unproven containment (`descendantScanLost`). Semantics after the change:

| State when the deadline expires | Before | After |
| --- | --- | --- |
| `monitorDone` already closed and the timer spent (`both arms ready`, Go picks uniformly) | ~50 % of such picks return "descendant monitor did not stop before deadline" | always joined: `nil` |
| `monitorDone` still open (the monitor never exits) | deadline error after the full wait | unchanged: the same deadline error, after the full wait (fail-closed) |
| deadline already spent at entry (`time.Until <= 0`) | "descendant monitor stop deadline elapsed" | unchanged (the sibling join's entry guard is likewise unchanged; see trade-off 2) |
| `monitorStop == nil` (no monitor was started) | `nil` | unchanged |

The recheck is a non-blocking second `select` on `monitorDone` with a `default` arm: it can never
wait, so the function keeps its single bounded wait and cannot extend a caller's deadline. It reports
success only when the monitor really exited (`close(group.monitorDone)` at `:809` is the only
production close), which is the invariant the entry states.

## Evidence plan (exact commands; no measurement run beside the full suite on this host)

RED — the pin against the pre-fix source: the worktree itself before the fix is applied (its
`process_unix.go` is still HEAD's bytes), and the mutant is re-derivable in a throwaway copy outside
the worktree with the module cache already warm:

- `git show HEAD:daemon/internal/platform/process_unix.go` written over the copy's file (exact
  pre-fix bytes), then in WSL Ubuntu-24.04 with the toolchain the module pins (go1.27.0):
  `go test -count=1 -run 'TestProcessGroupStopDescendantMonitor' -v ./internal/platform/`
  → expect the deadline probe red (iteration and message recorded), the deterministic probe green,
  3 consecutive runs recorded.

GREEN — the same probes on the fixed tree: focused run, then the touched package's full suite, plain
and `-race`, in WSL:
`go test -count=1 -timeout 600s ./internal/platform/` and `go test -race -count=1 -timeout 600s ./internal/platform/`
(plus `-race -count=5 -run TestProcessGroupStopDescendantMonitor` for repeat stability, and one full
`-race ./...` at the end if the memory rule above allows it after the focused runs finish).

Windows (native, from `daemon/`, mirroring the `daemon-windows` CI job):
`gofmt -l <changed files>`, `go vet ./...`, `go test -count=1 -timeout 600s ./...`.

Docs gates: `git diff --check`; `python scripts/ci/model-routing-record-sanitize.py --check
--root-only` over the changed files (added lines carry no host path).

## Scope decision the entry does not settle: the final-scan RPC's timer arms

The item names two further timer arms in the same paragraph — `requestDescendantMonitorScan`
(`:1021-1058`): the request arm `:1042-1043` ("final scan request timed out") and the result arm
`:1055-1056` ("final scan timed out"), neither of which rechecks `monitorDone`/`resultChannel` when
the timer wins (the `monitorDone` arm at `:1048-1054` does recheck `resultChannel`). Both are the same
manufactured-failure shape and both feed `descendantScanLost` through `closeUntil` (`:1188`, `:1229`).

- **Option A (chosen): fix the titled site only; keep the two RPC timer arms as their own open entry.**
  The item's title and its fix sentence scope the change ("Give `stopDescendantMonitor` the
  post-timer recheck"; "pin both directions (a monitor that exits by the deadline joins; one that
  never exits still fails)"), and the stage contract restates exactly those two probes while
  requiring the fix "minimally". The RPC sentence arrived in the item as a citation a doc review
  demanded (the same shape is named there, not directed to be fixed). This also matches the
  repository's own precedent for this defect class: PR #105 left `stopDescendantMonitor` unfixed and
  recorded it in `docs/TODO.md`.
- **Option B (rejected): apply the recheck to the RPC arms in the same increment.** Two more
  production changes, and each needs its own pin; unlike the stop site, neither arm can be pinned
  deterministically (both waits are entered only after a preceding handshake, so a probe can only
  busy-wait into the both-ready window). Recording it keeps this increment's pins honest instead of
  adding two more weak ones.

Consequence of Option A, discharged in the same increment: the moved item must not read as if its
RPC sentence was fixed, so the archive's closure statement says the arms are unchanged, and
`docs/TODO.md` gains that residual as an entry with the anchors above.

## Design trade-offs

1. **No production test hook for a deterministic timer pin.** The defect is a both-ready select; the
   only deterministic fabrication would be a hook that lets a test decide when the timer has fired.
   PR #105's expired-timer probe solved this with a busy-wait plus a tolerant assertion and a measured
   detection claim; this plan reuses that shape and records the same kind of measurement, including
   the known `-race -cpu 1` weakness (documented in the test comment, not normalized away).
2. **Entry guard unchanged.** `remaining <= 0` keeps returning its own error without a recheck, exactly
   as the barrier join does; the stage contract's tolerance for "deadline elapsed" depends on it, and
   a monitor is not answered by rechecking when there is no wait left to perform.
3. **Probe placement next to the barrier pair** (`process_unix_linux_test.go:2162`) so the two
   deadline-recheck pins of the same family stay adjacent and a later reader sees the symmetry.
4. **No helper extraction.** The recheck is three lines inside a function the file already keeps long
   and comment-heavy; a shared helper would hide the different arm sets (`monitorDone` only here,
   `resultChannel` then `monitorDone` in the RPC).

## Out of scope (with reasons)

- The final-scan RPC's timer arms (Option B above; recorded, not fixed).
- Other `docs/TODO.md` entries, including the Control provider-action timer class and the subject
  batch stream's stdin refusal.
- `probeLinuxProcessGroupExit`'s poll timer (`:1637-1645`): a sleep arm with no second ready channel,
  so the shape does not apply.
- Refactors of the monitor's terminal-reason bookkeeping, the barrier join, or the containment
  surface; no test renames or deletions.
- `docs/design/**`: no design text describes the join's timer semantics (checked: no `monitorDone`
  or "descendant monitor" occurrence in `docs/design/` or `docs/runbooks/`), so nothing is made
  stale; the closure record carries the decision instead.

## Residual risks and limits

- The deadline probe's detection is probabilistic by construction; the measured mutant rate and the
  `-cpu 1` caveat are recorded in its comment, and the deterministic probe plus the untouched
  fail-closed arm keep the direction pinned where detection is not required.
- The WSL host keeps the known TCP-loopback family limitation recorded for PR #105's run; if it
  reproduces, the affected cases are re-run at the baseline to separate environment from change.
