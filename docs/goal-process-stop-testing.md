# Process stop and terminal cleanup evidence

This Goal 0006 change keeps Control's terminal verdict, execution-slot release,
and machine-local process cleanup as separate facts. PostgreSQL and the v1
wire protocol still own the server verdict; the local Store owns the recovery
marker. A model response, accepted terminal HTTP reply, or submitted kill signal
is not physical stop evidence.

## Local State Contract

An unresolved PID, creation identity, or start-time marker prevents both entering
`cleanup_pending` and deleting its journal. A conclusive rejection still records
its original verdict and retires ordinary unreachable outbox entries. Independent
Goal receipts remain deliverable. Accepted or conclusively rejected executions
still release their slot once, without authorizing workspace deletion.

The earlier implementation allowed `cleanup_pending` before a generic process
finished, relying on an in-memory cleanup barrier. That was insufficient after
restart and inconsistent with the failed-start ownership test. The stricter
contract deliberately retains `terminal_pending` until the exact process marker
is cleared. Existing slot-release, no-duplicate-start, and cleanup-once checks
remain required; their local-state assertions now express this stronger boundary.

Existing journal replacements cannot add, remove, or replace process details.
Registration uses `SetProcessDetails`; clearing uses `ClearProcessDetails` with
the expected PID and creation identity. Repeating registration for the same
process preserves the original start time. A different owner needs a cleared
marker or a different RunKey. Cleanup states cannot be entered or reopened by
the general replacement/state-setter APIs. First recovery imports remain readable.

## Stop Evidence

The waiter uses the identity of the Process it actually waited for, not whichever
identity is present in a later journal snapshot. Ordinary nonzero exit, output
failure, and stop proof are different outcomes. An unresolved containment or
termination failure cannot authorize marker removal. A failed stop-evidence
write retains the marker; retries reuse the same proof and expected identity,
not a new numeric-PID termination attempt.

Recovered stop success is retained in memory for the same RunKey, PID, creation
identity, and start-time marker until its durable clear succeeds. Native receipt
write failures keep that proof as well, preserving stop, receipt, then clear
ordering. The proof never transfers to a replacement marker or another daemon.
If the daemon crashes before any durable stop evidence commits, recovery may
remain unresolved even though the process stopped; a missing leader is not proof.

After an OS launch, a failed `Start` still returns any process/session cleanup
owner alongside the original error. Callers retain it before attempting further
journal operations. Pre-persistence output is discarded rather than replayed to
the sink. Missing identity keeps launch uncertainty; it is never synthesized.
Native cleanup requires bounded Close and final Wait evidence before recording a
stop receipt or clearing the original process marker. Task failure and nonzero
exit are not themselves unresolved physical-stop evidence.

Native handle binding requires Go 1.27. Linux process-group operations require
`PIDFD_SIGNAL_PROCESS_GROUP` support (Linux 6.9 or later); unsupported hosts must
fail before launch rather than silently use numeric-PGID signaling. The owned
pidfd is independently duplicated with close-on-exec and retained across root
reaping. Both signals and empty-group readback use that original descriptor.
Keeping a pidfd while signaling a numeric PID would not prevent PID-reuse races.

Windows binds the original process handle to a Job Object and reads creation
time from that handle. An unavailable safe soft signal is distinct from a failed
hard stop; the existing grace and owned-Job fallback remain. Stop confirmation
observes zero active Job members before releasing the handle. Repeated close
does not target a reused PID or discard an earlier unproven outcome. Failed
observation still requires best-effort handle release, with errors retained.

Restart recovery performs a bounded attempt for each captured process marker.
The existing cleanup worker owns terminal generic marker retries and is joined
during shutdown; repeated discovery does not reset its backoff. Input restart
recovery must drain the old outbox (or observe conclusive authority loss) and
durably queue `failed` before registration. Unproven physical stop instead retains
the marker and durably retains the workspace, allowing that failed terminal
journal to enter the existing cleanup worker. Retention, drain, and terminal
persistence failures still block registration after other entries are attempted.
This is the explicit, narrow ordering amendment in protocol v1's "Restart During
Input Delivery" section, not a waiver of the durable fallback.

Native Goal recovery retains its receipt ordering and returns typed pending
recovery to the existing reconcile backoff. A pending native entry does not abort
initialization or prevent unrelated recovery and snapshot command/assignment
dispatch. Terminal delivery is woken through the outbox owner, not performed by
a process waiter. A Linux recovery lookup must match the expected identity
before and inside the handle
operation. A PID-only Windows journal cannot recreate its lost anonymous Job
membership proof, so absence of the leader or a successful `taskkill` is not
enough to clear it.

These are the existing owned-container boundaries, not unconditional isolation
of every possible descendant. Processes that escape a Unix group or run before
Windows post-start Job assignment are not made verified by this patch. Missing
creation identity or lost native containment evidence remains an explicit
recovery limitation; no synthetic identity or claimed native capability fills it.

## Verification

Run from `daemon/`:

```text
go vet ./...
go test -count=1 -timeout 240s ./...
```

On Linux with race instrumentation:

```text
go test -race -count=1 -timeout 600s ./...
```

Docker test runs require `--init` to reap orphaned test descendants. Native
OpenCode transport checks are opt-in and documented beside its versioned
fixtures; they do not submit a model turn or verify native usage/lifecycle.

Required regression cases include terminal acceptance while Wait is unresolved;
normal and nonzero process exits; stop-persistence failure and restart; old
waiters versus replacement ownership; unresolved recovery alongside another
recoverable entry; Goal receipt delivery while a marker remains; duplicate Close;
failed observation and handle-release retry; and original group/Job targeting
after leader exit without numeric-PID fallback.

The Linux capability-probe resource regression disables GC while observing
`/proc/self/fd`: sixteen probes previously retained sixteen pidfds; explicit
`Process.Release` leaves the count unchanged. Recovery handle lookups also
release their borrowed process owner after the bounded attempt.

## Observed Checks

The following checks passed on 2026-09-11 for this change based on `a7563b5`.
Counts include named Go subtests. The verified daemon tree is
`083d9a4abd07ca64588fa5287b09df8f4506dd49` (`git write-tree --prefix=daemon/`).

| Check | Result |
| --- | --- |
| Windows `go test -count=1 -timeout 240s -json ./...` | 2,007 passed, 15 opt-in skips |
| Linux `go test -race -count=1 -timeout 600s -json ./...` | 2,021 passed, 15 opt-in skips |
| Windows and Linux `go vet ./...` | Passed |
| Native OpenCode 1.18.30 transport smoke, Windows | Passed, 7.87 seconds |
| Native OpenCode 1.18.30 transport smoke, Linux with `-race` | Passed, 3.94 seconds |

Final local test logs are `.symmetry/process-stop-windows-final3.jsonl` and
`.symmetry/process-stop-linux-final2.jsonl`; these runtime artifacts are not
committed. Independent CE review `20260911-153901-03e5d2eb` completed with no
remaining actionable findings. The parent subsequently closed its final Linux
verification gap with the full race and vet checks above.

The first full Linux race run exposed an unlocked assertion in the native
close-retry fixture while the original completion goroutine was still active.
The intermediate owner check now takes the daemon mutex; final cleanup and
usage assertions remain after joining that goroutine. The corrected scenario
passed ten focused race iterations and the subsequent full race gate.

Registration regressions cover failed terminal ID generation and terminal
persistence, a later successful initialization that observes durable `failed`
before registration, and a mixed recovery batch that finishes an independent
journal without swallowing the blocking error. Actual Run tests distinguish
terminal input cleanup-worker backoff from native Goal reconcile backoff and
verify unrelated snapshot command/assignment dispatch. Stop-witness regressions
cover failed marker/receipt writes, replacement markers, and loss of the volatile
proof across daemon restart.

Scoped simplification reused the marker predicate and Job-handle close seam,
reused the same OpenCode start error, and removed the duplicate generic startup
stop path in favor of the existing cleanup worker. Typed-nil normalization was
retained as an interface invariant; no capability cache or new scheduler was added.
The exact-marker recovered stop witness is retained because a failed durable write
must not destroy stop evidence already observed by the same daemon.

The opt-in live Control/daemon E2E, credentialed model work, Pi native tests and
Claude validation are not inferred from these results. OpenCode's separately
executed transport smoke does not prove model-task, usage, resume or permission
semantics. Goal 0006 remains incomplete.
