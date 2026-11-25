# Daemon process stop and terminal cleanup

Status: **local-state contract and verification recipe** for the daemon's
process-stop boundary. Applies when changing daemon execution, supervision,
cleanup or native containment code. Control's terminal verdict, the execution
slot and machine-local process cleanup are three separate facts: PostgreSQL and
[protocol v1](../protocol-v1.md) own the server verdict, the daemon's local store
owns the recovery marker, and only the rules below make a stop evidenced. Dated
run records for this boundary live under [`../evidence/`](../evidence/); the
gates are listed in [`../design/quality.md`](../design/quality.md).
Passing these checks is not a release, native-capability or goal-completion
claim; goal 0006's unresolved requirements stay recorded in
[`../goals/README.md`](../goals/README.md) and its evidence records.

## Local state contract

A model response, an accepted terminal HTTP reply, or a submitted kill signal is
not physical-stop evidence. An unresolved PID, creation identity or start-time
marker prevents both entering `cleanup_pending` and deleting the journal. A
conclusive rejection still records its original verdict and retires ordinary
unreachable outbox entries; independent goal receipts remain deliverable.
Accepted or conclusively rejected executions still release their slot once,
without authorizing workspace deletion.

Journal replacements cannot add, remove or replace process details. Registration
uses `SetProcessDetails`; clearing uses `ClearProcessDetails` with the expected
PID and creation identity. Repeating registration for the same process preserves
the original start time; a different owner needs a cleared marker or a different
RunKey. Cleanup states cannot be entered or reopened through the general
replacement/state-setter APIs, and first recovery imports remain readable.

## Stop evidence

- The waiter uses the identity of the process it actually waited for, not
  whichever identity is present in a later journal snapshot.
- Ordinary nonzero exit, output failure and stop proof are different outcomes. An
  unresolved containment or termination failure cannot authorize marker removal.
  A failed stop-evidence write retains the marker; retries reuse the same proof
  and expected identity, not a new numeric-PID termination attempt.
- Recovered stop success is retained in memory for the same RunKey, PID, creation
  identity and start-time marker until its durable clear succeeds; native
  receipt-write failures keep that proof as well, preserving the
  stop → receipt → clear ordering. The proof never transfers to a replacement
  marker or another daemon. If the daemon crashes before any durable stop evidence
  commits, recovery may remain unresolved even though the process stopped: a
  missing leader is not proof.
- After an OS launch, a failed `Start` still returns any process/session cleanup
  owner alongside the original error, and callers retain it before further journal
  operations. Pre-persistence output is discarded rather than replayed to the
  sink. Missing identity keeps launch uncertainty; it is never synthesized. Native
  cleanup requires bounded `Close` and final `Wait` evidence before recording a
  stop receipt or clearing the original marker, and task failure or nonzero exit
  is not itself unresolved physical-stop evidence.

## Native boundaries

- **Linux.** Process-group operations require `PIDFD_SIGNAL_PROCESS_GROUP`
  support (Linux 6.9 or later, permitted by the host security policy);
  unsupported hosts must fail before launch rather than silently use
  numeric-PGID signaling. The owned pidfd is independently duplicated with
  close-on-exec and retained across root reaping; both signals and empty-group
  readback use that original descriptor, because keeping a pidfd while signaling
  a numeric PID would not prevent PID-reuse races. Launches also set
  `Pdeathsig=SIGKILL` so a daemon crash before marker persistence cannot leave
  the direct child ownerless.
- **Windows.** The original process handle is bound to a Job Object, and creation
  time is read from that handle. An unavailable safe soft signal is distinct from
  a failed hard stop; the existing grace and owned-Job fallback remain. Stop
  confirmation observes zero active Job members before releasing the handle.
  Repeated close does not target a reused PID or discard an earlier unproven
  outcome; a failed observation still requires best-effort handle release, with
  errors retained. A PID-only journal cannot recreate lost anonymous Job
  membership proof, so absence of the leader or a successful `taskkill` is not
  enough to clear it.
- **Restart recovery.** Recovery makes a bounded attempt per captured process
  marker. The cleanup worker owns terminal generic marker retries and is joined
  during shutdown; repeated discovery does not reset its backoff. Input restart
  recovery must drain the old outbox (or observe conclusive authority loss) and
  durably queue `failed` before registration; an unproven physical stop instead
  retains the marker and the workspace, allowing that failed terminal journal to
  enter the cleanup worker. Retention, drain and terminal-persistence failures
  still block registration after other entries are attempted. This is the
  explicit ordering amendment in protocol v1's *Restart During Input Delivery*
  section, not a waiver of the durable fallback.
- Native goal recovery retains its receipt ordering and returns typed pending
  recovery to the existing reconcile backoff; a pending native entry does not
  abort initialization or prevent unrelated recovery and snapshot
  command/assignment dispatch. Terminal delivery is woken through the outbox
  owner, not performed by a process waiter. A Linux recovery lookup must match the
  expected identity before and inside the handle operation.

These are owned-container boundaries, not unconditional isolation of every
possible descendant. Processes that escape a Unix group, or that run before
Windows post-start Job assignment, are not made verified by this contract; a
missing creation identity or lost native containment evidence remains an explicit
recovery limitation, and no synthetic identity or claimed native capability fills
it.

## Retained pre-checkout clone after a normal release

A same-RunKey recovery that consumes an authenticated physical release for a run
whose B-owned clone never checked out keeps the clone row, the B checkout, the A
seed bundle and that RunKey's `refs/heads/symmetry/clone-v2/base/<hash>` ref on
disk. That retention is the recorded durable shape, not a failure: the row keeps
its `linux_clone_intent` (`seed_path`, `target`), `linux_clone_seed_receipt` and
the containment release proof, and carries no checkout, promotion, abort or
cleanup field. A normal release proves that containment stopped; it does not
prove that B never ran, so a release tombstone never authorizes clone deletion
and no product disposition deletes this row today. The row no longer blocks
unrelated native admission, and its seed and target paths and its cgroup leaf
name stay reserved for new reservations until an explicitly authorized
disposition exists.

Read-only locator over the daemon's configured StateDir; it changes nothing:

```bash
state_dir=$(jq -r .state_dir /etc/symmetry/daemon.json)
for journal in "$state_dir"/runs/journal-*.json; do
  jq -e 'select(.linux_clone_intent != null and .containment_cgroup_release_proof != null
    and .linux_clone_checkout_receipt == null and .linux_clone_abort_pending != true
    and .linux_clone_abort_proof == null and .linux_clone_cleanup_intent == null
    and .linux_clone_cleanup_proof == null and .linux_clone_promotion_receipt == null)' \
    "$journal" >/dev/null || continue
  run_id=$(jq -r .run_id "$journal")
  generation=$(jq -r .generation "$journal")
  base_ref="refs/heads/symmetry/clone-v2/base/$(printf '%s\0%s' "$run_id" "$generation" | sha256sum | cut -d' ' -f1)"
  jq -r --arg ref "$base_ref" '"\(.run_id) \(.generation) \(.linux_clone_intent.seed_path) \(.linux_clone_intent.target) \($ref)"' "$journal"
done
```

The row's B clone, A seed and base ref stay on disk until an authorized
disposition exists; `docs/TODO.md` tracks the disposition command that does not
exist yet.

## Run the checks

From `daemon/`, per [`daemon/AGENTS.md`](../../daemon/AGENTS.md):

```text
gofmt -l .            # changed files must not appear
go vet ./...
go test ./...
go test -race ./...   # concurrency and execution changes on supported runners
```

Windows behavior requires real Windows tests, not cross-compilation. When using
Docker for the Linux gates, pass `docker run --init` so orphaned test descendants
are reaped; without an init process a terminated descendant can remain a zombie
and fail the existing process-absence assertion even after its stdout descriptor
closed. Do not weaken the assertion to hide an incorrect test-container setup.

CI runs its own forms of these gates (see *Commands and gates* in
[`../design/quality.md`](../design/quality.md)): the `daemon` job owns the Linux
`gofmt`, `go vet` and Go-test passes, the `daemon-windows` job owns the Windows
suite, and `.github/workflows/ci.yml` is the owner of the exact step inventory
rather than the summary here. Native OpenCode transport checks are opt-in and
documented beside their versioned fixtures under
`daemon/internal/harness/testdata/opencode/`; the
[peer-ownership recipe](daemon-opencode-peer-ownership.md) covers the transport's
loopback boundary.

Required regression cases include terminal acceptance while `Wait` is
unresolved; normal and nonzero process exits; stop-persistence failure and
restart; old waiters versus replacement ownership; unresolved recovery alongside
another recoverable entry; goal-receipt delivery while a marker remains;
duplicate `Close`; failed observation and handle-release retry; and original
group/Job targeting after leader exit without numeric-PID fallback. The Linux
capability-probe regression also disables GC while observing `/proc/self/fd`:
sixteen probes must not retain sixteen pidfds once the process owner is released.
