# rf-w4-17a command-queue module plan — `commandQueue` out of the `daemon` field bag

Contract: `goal-rf-w4-17a-command-queue-module.md` (GDD, 2026-11-21), binding with its
conductor design clauses 1-5 and the program-wide rules. Base revision
`7954836892f39bc8a92abe11e5efcb5b3bac3476` — the revision every line coordinate below resolves at.
The branch later merged `origin/main` three times (`fc8e95a2` as `a4770eda`, `94d04e42` as `9207e5a4`,
`9e473513` as `2715b780`) and `origin/main` has advanced past `9e473513`; no commit on `origin/main`
changed a delivered file (`git diff` over the nine paths of the delivered range is empty at every step).
Shape reference: `docs/plans/2026-11-21-rf-w1-05-app-go-split-plan.md`
+ `docs/evidence/rf-w1-05-app-go-split-2026-11-21.md` (same package, same wave-4 program; plan +
`docs/evidence/<slice>-<date>.md` record + mechanical move/shape proofs).

## Measured basis (base revision)

Loose queue state on the 52-field `daemon` struct, mutated from three files:

| field | declaration | initialisation | writers (base) |
|---|---|---|---|
| `commandWake chan struct{}` | `app.go:369` | `app.go:492` | `commands.go:58,:70,:227` (read) |
| `commandQueue []*queuedCommand` | `app.go:370` | — (nil) | `commands.go:69,:193-194` |
| `queuedCommands map[commandKey]*queuedCommand` | `app.go:371` | `app.go:493` | `commands.go:61-68,:94,:173,:207,:213,:219`; `app.go:1772-1778`; `outbox.go:1152-1155` |
| `commandLanes map[state.RunKey]*commandLane` | `app.go:372` | — (lazy, `commands.go:127-133`) | `commands.go:127-133,:160-162` |
| `commandRequests map[uint64]map[commandKey]struct{}` | `app.go:373` | `app.go:494` | `commands.go:299,:310,:324,:341-346,:357`; `outbox.go:1128-1133` |
| `nextCommandRequest uint64` | `app.go:374` | — (zero) | `commands.go:344-345` |

Reader/writer call sites (base), by method:

- `clearCompletedCommandReceipts` (`app.go:1763`) → `releaseCommandReceiptsLocked` (`app.go:1771`), called
  from `cleanup.go:954`; `releaseRun` (`app.go:1720`) → same, called from `app.go:1365`, `cleanup.go:128`,
  `native_run.go:1507,:1575,:2018`.
- `startBackground` (`app.go:481`) resets `queuedCommands`/`commandRequests` under `daemon.mu` (`:493-494`).
- `enqueueCommand`/`enqueueCommandLocked` (`commands.go:39,:53`); `nextQueuedCommand` (`:186`);
  `nextLaneCommand` (`:157`); `dispatchQueuedCommand` (`:91`); `executeQueuedCommand` (`:171`);
  `finishQueuedCommand` (`:198`) → `finishQueuedCommandLocked` (`:212`); `finishDeferredCommand` (`:204`,
  callers `commands.go:382`, `app.go:1432`, `native_run.go:807,:817,:825`, `recovery_glue.go:892`);
  `enqueueSnapshotCommandsForRequest` (`:293`); `beginCommandRequest` (`:336`, callers `app.go:1135,:1149,:1192`,
  `lease.go:458`); `finishCommandRequest` (`:350`, callers `app.go:1139,:1153,:1196,:1239`, `lease.go:203-281,:462`).
- `markCommandAcknowledgementDelivered` (`outbox.go:1124`) writes the tombstones across live requests
  (`:1128-1133`, before the journal write) and marks/evicts the dedupe entry (`:1152-1155`, after it).
- `queueTerminalTransitionAndAcknowledgementWithContext` (`outbox.go:186`) holds `commandReceiptMu` across one
  persistence attempt (`:213-241`) and touches no queue field.

`daemon.mu` lock sites in the touched methods (base): `commands.go:40/42` (`enqueueCommand`),
`commands.go:93/96/101/140` + `:126` region (`dispatchQueuedCommand`), `commands.go:158/159` (`nextLaneCommand`),
`commands.go:172/174` (`executeQueuedCommand`), `commands.go:187/188` (`nextQueuedCommand`),
`commands.go:199/200` + `:205/206` (`finishQueuedCommand`/`finishDeferredCommand`), `commands.go:337-338`+`:340`
(`beginCommandRequest`), `commands.go:354-355`+`:356/358` (`finishCommandRequest`), `commands.go:294-295`+`:298/311/323`
(`enqueueSnapshotCommandsForRequest`), `app.go:1723/1725/1744` (`releaseRun`), `app.go:1765/1767/1769`
(`clearCompletedCommandReceipts`), `outbox.go:1127-1128/1135`, `:1148-1149/1154` (`markCommandAcknowledgementDelivered`).
`commandReceiptMu` sites: `commands.go:294,:337,:354`, `app.go:1721,:1764`, `outbox.go:213,:1126,:1149`
(always outer to `daemon.mu`, or alone). Order and count are the audit subject; the tables land in the
evidence record.

## Deliverable shape

1. **New `daemon/internal/app/command_queue.go`** — the whole module in one file:
   `commandQueue` (queue state + 8 methods), `commandReceipts` (request-id + tombstone bookkeeping, 4 methods),
   the moved entry types `queuedCommand`/`commandKey`/`commandLane`, `newCommandQueue()`, and the file-level
   lock contract ("every method requires the caller to hold `daemon.mu`; the module takes no lock and performs
   no I/O"). `commandCompletion` stays in `commands.go`: it is the daemon-side wait handle the snapshot loop
   returns, not queue state.
2. **`daemon/internal/app/app.go`** — delete the five loose fields (`commandQueue`, `queuedCommands`,
   `commandLanes`, `commandRequests`, `nextCommandRequest`); add `commands commandQueue`
   (+ `commandWake` stays: the executor wake channel belongs to the daemon lifecycle, not to queue state).
   `startBackground` constructs `daemon.commands = newCommandQueue()`. `releaseRun` /
   `clearCompletedCommandReceipts` call `daemon.commands.dropRun(key)`; `releaseCommandReceiptsLocked` is
   deleted (its body is `dropRun`).
3. **`daemon/internal/app/commands.go`** — the queue methods' bodies are replaced by queue calls:
   `enqueueCommandLocked` (gate stays: `CommandID`/`commandWake`/`backgroundStop` are daemon lifecycle state),
   `nextQueuedCommand`, `nextLaneCommand`, `dispatchQueuedCommand` (membership check → `current`, lane block →
   `lane`), `executeQueuedCommand` (membership check → `current`), `finishQueuedCommand`,
   `finishDeferredCommand`, `enqueueSnapshotCommandsForRequest` (tombstone read/delete → `receipts`),
   `beginCommandRequest`, `finishCommandRequest`; `finishQueuedCommandLocked` is deleted (its body is
   `commandQueue.finish`).
4. **`daemon/internal/app/outbox.go`** — `markCommandAcknowledgementDelivered` calls
   `daemon.commands.receipts.suppress(key)` before and `daemon.commands.markDelivered(key)` after the journal
   write; lock sites unchanged.
5. **`daemon/internal/app/app_test.go`** — mechanical rewrite only (no assertion change): 18 `&daemon{…}`
   literals `queuedCommands: make(map[commandKey]*queuedCommand)` → `commands: newCommandQueue()`; eight
   direct-inspection lines rewritten to `daemon.commands.byKey` / `daemon.commands.receipts.requests`
   (base `:5895-5900` two assertion blocks, `:6956` one map read, `:8709`/`:8720`/`:8727` three helper
   reads).
6. **New `daemon/internal/app/command_queue_test.go`** — queue/receipt invariants at the new interface, no
   `&daemon{}`, each test with a recorded red mutant (clause 4).
7. **This plan + `docs/evidence/rf-w4-17a-command-queue-module-2026-11-21.md`** (method list with callers,
   lock-site table, control-flow audit, red-mutant records, gate outputs).
8. `docs/TODO.md` / `docs/archive/CLOSED-TODO.md`: no entry closes — the slice was dispatched from the
   conductor's arch decision, `docs/TODO.md` has no W4-17a entry (checked at base: `grep -n "W4-17\|command
   queue\|commandQueue" docs/TODO.md` → no match). No contraction index or grammar document names these
   fields (checked: `docs/` and `contracts/` carry no citation of `queuedCommands`/`commandLanes`/
   `commandRequests`; the only hit is the archived plan `docs/plans/2026-11-21-daemon-test-enablers-plan.md:92`,
   a historical plan file, not an audited document).

## Interface (final; the method list with callers is reproduced in the evidence record)

`commandQueue` — 8 methods, all "caller holds `daemon.mu`", none locking, none doing I/O:

| method | intent | callers (after) |
|---|---|---|
| `enqueue(command) (*queuedCommand, bool)` | dedupe by command key, append to the arrival FIFO; `false` = the key was already queued | `enqueueCommandLocked` |
| `next() (*queuedCommand, bool)` | pop the arrival FIFO head | `nextQueuedCommand` |
| `lane(key, queued) bool` | append to the run's lane; `true` = the caller must start the lane's drain goroutine | `dispatchQueuedCommand` |
| `nextInLane(key) (*queuedCommand, bool)` | pop the run lane's head; drop the drained lane | `nextLaneCommand` |
| `current(queued) bool` | is `queued` still the entry registered for its key | `dispatchQueuedCommand`, `executeQueuedCommand` |
| `finish(key, registered, receipt)` | complete the registered entry (nil `registered` = whatever `key` maps to, the deferred path); receipt rule below | `finishQueuedCommand`, `finishDeferredCommand` |
| `dropRun(key)` | release every entry of a run (close waiters, evict) | `releaseRun`, `clearCompletedCommandReceipts` |
| `markDelivered(key)` | record a delivered receipt; evict a completed entry | `markCommandAcknowledgementDelivered` |

`commandReceipts` — 4 methods, same lock contract, holds `requests map[uint64]map[commandKey]struct{}` +
`nextRequest`:

| method | intent | callers (after) |
|---|---|---|
| `begin() uint64` | open a suppression window; return its id | `beginCommandRequest` |
| `finish(requestID)` | close a window | `finishCommandRequest`, `enqueueSnapshotCommandsForRequest` |
| `suppressed(requestID, key) bool` | would this window suppress `key` | `enqueueSnapshotCommandsForRequest` |
| `suppress(key)` | tombstone `key` in every live window | `markCommandAcknowledgementDelivered` |

## Contract-preserving decisions (recorded, not guessed)

**D1 — the receipt bookkeeping is a sibling value type.** Design clause 1 asks for the three queue fields
*plus* "the request-id bookkeeping used by begin/finishCommandRequest and the command-receipt state if it is
only touched by these methods (if receipts are also touched elsewhere, leave them and record why)". The
receipt state is touched elsewhere: `markCommandAcknowledgementDelivered` (outbox.go:1124-1149) writes the
tombstones *before* and marks the delivered entry *after* a `Store` write. Both writes are queue calls under
the caller's locks — `receipts.suppress` under `commandReceiptMu` + `daemon.mu` (outbox.go:1126-1128) before
the write, and `commands.markDelivered` under the same two locks (outbox.go:1143-1146) after re-acquiring
them — so `commandReceiptMu` is *released across* the journal write and is not held while that write may
block; the delivered mark (`queuedCommand.delivered`) is queue state, not receipt state. The only site that
holds `commandReceiptMu` across a journal write is the terminal-acknowledgement persistence loop
(outbox.go:210-241), which never touches this bookkeeping. A suppression window is ordered against
acknowledgement delivery by that mutex, not by queue flow, so the receipt bookkeeping stays a sibling value
type. Counting the operations the daemon actually needs (13: arrival put/pop, lane put/pop, membership,
complete-instance, complete-by-key, release-run, window open/close, tombstone, delivered-evict) leaves 8
queue-flow/guard/completion/release verbs. The split is *not* a budget artefact:
the binding budget is at most 12 methods (arch-decision-2, superseding amendment 1's 11), and the delivered
file counts exactly 12 (8 + 4), so one type with all 12 would also fit. Options considered: (a) 12 methods
on one type — within the amended budget, but the module's single lock contract would then also cover the
suppression windows, whose mutations in `markCommandAcknowledgementDelivered` are ordered by
`commandReceiptMu` around a journal write (both phases under `commandReceiptMu` + `daemon.mu`, with the lock
released across the write), so the file-level comment would have to qualify itself;
(b) one or two enum-dispatch "god methods" (`finish(key, instance, stage)` carrying unrelated
eviction/completion effects) — a worse interface, and the enum would have to bypass the completion effects
for the delivery phase; (c) package-level helper functions over queue state — keeps the count but hides the
surface, not a design argument. **Recommendation, adopted:** the receipt bookkeeping is a sibling value type
in the same module file, held by the queue (`commandQueue.receipts`), with 4 methods of its own; the queue
itself stays at 8. The queue still *holds* the request-id bookkeeping (nested), the module owns every write
to it, and both are tested without `&daemon{}`.

Correction (2026-11-21, simplify review + apply stage): this decision's first version justified the split
with the superseded method budget ("the ≤8-method budget cannot carry the receipt lifecycle", option (a)
rejected as over budget) and the module comment said the queue "stays within its method budget" under the
old per-type reading. Both readings are stale after arch-decision-2; the two types, their contents and
their lock contract are unchanged — only the retired budget argument is replaced by the cohesion and
lock-contract reasons above.

**D2 — the snapshot enqueue loop stays in `commands.go`.** `enqueueSnapshotCommandsForRequest` gates every
command on `commandWake == nil || backgroundStop` (daemon lifecycle state, read in non-queue contexts too,
e.g. `scheduleSnapshotForRequest`) and returns the wake channel to signal; moving the loop into the queue
would either drag that daemon state into the module or add a 9th method. The loop is composition: it calls
`enqueueCommandLocked` (gate + `enqueue`) and the receipt window methods; the invariants it drives (dedupe,
suppression, window deletion on failure and on success) live in the module.

**D3 — `commandWake` and `commandReceiptMu` stay on `daemon`.** `commandWake` is the background executor's
wake channel (`startBackground`/`stopBackground`/`commandSignal`), not queue state. `commandReceiptMu` is an
ordering mutex between journal writes and queue mutation; it keeps exactly its base acquisition points, and
the "if it moves" clause does not apply.

**D4 — `startBackground` constructs a fresh queue.** Base reset `queuedCommands` and `commandRequests` under
`daemon.mu` and left `commandQueue`/`commandLanes` (empty at that point: the executor starts after) alone; it
did not reset `nextCommandRequest`. `daemon.commands = newCommandQueue()` resets all four plus the counter.
The counter restart is unobservable: a window id never outlives the reset (every snapshot path consumes its
window inside the same `startBackground` lifetime), no test or production path threads a request id across
`startBackground`, and window ids are compared only within one lifetime.

## Preserved semantics (edge cases the queue must reproduce exactly)

1. **Dedupe by command key.** `enqueue` returns the existing entry and appends nothing; the caller gets the
   existing `done` channel and no wake. Empty `CommandID` and the `commandWake == nil || backgroundStop` gate
   are checked *before* the entry is created (base order), so a refused command creates no dedupe entry.
2. **Arrival FIFO.** `next()` pops the head and nils the slot before re-slicing (no retained pointer).
3. **Per-run lanes.** First append for a run starts the drain (`lane` → `true`), later appends return
   `false`; `nextInLane` pops the head and deletes the lane when drained (also when absent — a no-op delete);
   cancels never enter a lane (the daemon's `dispatch` keeps the cancel branch); `current` is checked before
   the lane/cancel decision, so a released entry is dropped without touching `daemon.running`.
4. **Released entries in flight.** `dropRun` evicts `byKey` entries and closes the waiters of that run only;
   the arrival FIFO and the lanes may still hold pointers to released entries — exactly as base. Such an entry
   fails `current`, so `dispatch` and `execute` drop it; that is the documented consistency contract of
   `dropRun` (base behavior, not fixed here).
5. **Completion.** `finish` acts only when the entry is still registered for its key (identity, not key
   equality), closes `done` once (`completed` guard), marks it complete, and evicts unless
   `receipt && !delivered`. Deferred completion resolves the entry the key currently maps to.
6. **Receipts.** `markDelivered` marks the entry delivered and evicts it only when already completed;
   `suppress` tombstones a key in every live window (and tolerates a nil window set, as base);
   `suppressed` reads a window's set; `finish(window)` deletes it. Nothing else removes a window.
7. **Locks.** No new mutex; `daemon.mu` is taken only by the daemon wrappers, at the base sites, in the base
   order; `commandReceiptMu` keeps its base sites and stays outer to `daemon.mu` where base had it that way.
   No goroutine, channel, `WaitGroup` or timer change.
8. **Text.** No error string, log key or test name changes in any touched file.

## Round-2 note (2026-11-21) — opus review 2 (N1, N2); review-1 finding F3 declined

Review-1 finding F3 (replace `finish(key, registered, receipt)` with `finish(key, receipt)` and move the
identity check into `finishQueuedCommand`) was applied in the fix round and then **declined** by the
conductor (2026-11-21) under option (b): the identity check must stay inside the queue method so the
module-level test guards the stale-pointer invariant — `finishQueuedCommand`'s check and the
dispatch/execute callers' `current` checks run in different critical sections, and with the check moved
out of `finish` the guard-removal mutant survived the whole package (review 2, G1). The delivered shape is
the round-1 `finish(key, registered, receipt)`; the revert restored `finishQueuedCommand` /
`finishDeferredCommand` to their round-1 bodies and the stale-pointer test to a call of the real queue
method, with mutant M7 red again and G1 equivalent to M7 in the restored shape (evidence fix-round-3
note). Review-2 finding N2 added `TestCommandQueueRepeatedFinishClosesDoneOnce`: a second, deferred
completion of the same key closes `done` exactly once and evicts; mutant M12 (drop the `!current.completed`
guard) is red.

## Evidence plan (all foreground; heavy suites serialized)

1. `gofmt -l ./internal/app/` (daemon/) → empty; `go vet ./...`; `GOOS=linux go build ./...`;
   `GOOS=windows go build ./...` → exit 0 each.
2. Windows focused `go test -count=1 -race ./internal/app/ -run 'Command|Queue|Lane|Outbox|Receipt|Acknowledg'`
   (if `-race` is unavailable on this host — `CGO_ENABLED=0`, no `gcc` — the attempt and its error are
   recorded and the same pattern runs without `-race`), then full `go test -count=1 ./internal/app/...`.
3. WSL `wsl -d Ubuntu-24.04 -u accept` in `daemon/`: `go test -count=1 -race ./internal/app/...` (the race
   oracle for this slice). This host's WSL instance is single-CPU (`nproc` = 1); if the full-suite race run
   overruns Go's test timeout there, the same suite is run in three name shards (`^Test[A-H]`, `^Test[I-P]`,
   `^Test[Q-Z]`) with `-v` and a per-shard `-timeout`, so the coverage claim stays complete and the
   per-shard outputs localize any outlier; every attempt and its outcome is recorded either way.
4. Red mutants: for each new test, one temporary edit in `command_queue.go` breaking the invariant it pins,
   with the failing output recorded, then reverted (the tree left uncommitted, mutants not delivered).
5. Base-vs-HEAD control-flow audit for every rewritten method (lesson 32): each base early return,
   fall-through, default and error short-circuit mapped to its HEAD equivalent, with the row marked preserved
   or lost, plus the mapping for the two deleted methods (`finishQueuedCommandLocked`,
   `releaseCommandReceiptsLocked`) into their new queue counterparts.
6. Lock-site table before/after: every `daemon.mu` / `commandReceiptMu` Lock/Unlock in the touched methods,
   in order, extracted from both revisions.
7. `grep -rn 'daemon\.\(commandQueue\|queuedCommands\|commandLanes\)' daemon/internal/app` → 0 matches
   outside `command_queue.go`.
8. Citation gate: `python3 scripts/ci/check-citation-existence.py --base <base> --head <tree>`; the slice
   adds/modifies no audited document (`docs/runbooks/**`, `docs/design/quality.md`,
   `docs/archive/CLOSED-TODO.md`), so the expected result is "nothing to check" — and that claim is verified
   against the changed-path list, not assumed.
9. Host-path and DLP checks over the working-tree diff: no `us[e]rs/`-style profile path, no backslash-u
   sequence in any added line.
10. arch-decision-2 R9: `daemon/internal/app/command_queue.go` is added to `SOURCE_PATHS` in
    `scripts/ci/omo-control-native-evidence.py` (owned for that line only; appended at the end of the list
    so the entries the contract cites by line keep their coordinates), the moved declarations are mapped to
    their listed paths in the evidence record, and `python3 scripts/ci/test-omo-control-native-evidence.py`
    runs green. Without the path the native-evidence identity set (`source_files`) would exclude the file
    the slice's behaviour moved into.

## Out of scope (with reasons)

- `runningRun` / run-lifecycle ownership (W4-17b), goal admission (W4-01), test doubles (W4-12), the Control
  client: named out of scope by the contract.
- The `commandWake`/`backgroundStop` lifecycle: daemon state, not queue state (D3).
- Fixing the tolerated staleness of `dropRun` for entries already in the FIFO/lanes: base behavior, and the
  contract requires `dropRun` to release exactly what `releaseCommandReceiptsLocked` released.
- Unifying the receipt mutex with the queue (a new lock contract): forbidden by design clause 3.

## Risks and limits

- The Windows host cannot run `-race` (no cgo toolchain): the race evidence is the WSL run, as the contract
  anticipates; the Windows focused run without `-race` still exercises the moved paths.
- The evidence record carries the exact commands, exit codes and counts; anything not run is reported as
  missing, never as passed.

## Comment-policy sweep (2026-11-22)

Module intent removed from `daemon/internal/app/command_queue_test.go` by the 2026-11-22
comment-policy sweep is recorded below (workstream record "ws3b-app-tests").

- `daemon/internal/app/command_queue.go` — module ownership and budget contract, removed from the `commandQueue`/`commandReceipts` docs (the lock-contract and lock-order lines stayed under K1):
  - "commandQueue owns the daemon's command queue: the arrival FIFO, the dedupe index keyed by command key, and the per-run serialization lanes, together with the acknowledgement-receipt bookkeeping (commandReceipts) that fences late control-plane responses against delivered acknowledgements."
  - "A commandQueue may be used as a zero value: every map it reads is created on first use."
  - "It is a value inside commandQueue rather than more queue methods because it keeps its own ordering: every write to it happens inside a queue call under the caller's locks, but markCommandAcknowledgementDelivered orders the tombstone against a journal write with daemon.commandReceiptMu. … commandReceiptMu is held across a journal write only by the terminal-acknowledgement persistence loop (outbox.go:210-241), which never touches this bookkeeping. One intent per type, every write to the bookkeeping inside this file, and the module stays at the contract's binding budget (at most 12 methods: the 8 queue methods above plus the 4 receipt methods below). Lock contract and zero-value tolerance are commandQueue's."
  - "newCommandQueue returns an empty queue. startBackground constructs one per background lifetime, replacing the fields the daemon used to reset there." (`D2` narration; folded into the zero-value item)
- `daemon/internal/app/command_queue_test.go` (header block, pre-sweep lines 10–15): "This file exercises the command-queue module (command_queue.go) without a daemon: every invariant the queue and its receipt bookkeeping own is asserted at their own interface. The daemon-level behaviour oracle stays the existing app tests, which drive the same invariants through the executor." The trailing red-mutant/slice-evidence sentence was provenance, not moved.

The sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
