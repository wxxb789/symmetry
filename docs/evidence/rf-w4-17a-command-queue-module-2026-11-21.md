# rf-w4-17a command-queue module — evidence record (2026-11-21)

Subject: the daemon command-queue field cluster moved out of `daemon` into a new
`daemon/internal/app/command_queue.go` module (`commandQueue` + `commandReceipts`), with the daemon
keeping I/O and lock ownership. Contract: `goal-rf-w4-17a-command-queue-module.md`. Base revision
`7954836892f39bc8a92abe11e5efcb5b3bac3476` (= `origin/main`; all coordinates below resolve there).
Plan: `docs/plans/2026-11-21-rf-w4-17a-command-queue-module-plan.md`. Nothing was committed when the
gates below ran; they exercised the working tree described in section 1 (the operator committed it as
`2350d55c` and merged `origin/main` as `a4770ed` afterwards, and the apply-stage note at the end of
section 6 re-ran the gates on the post-simplification-review working tree).

Subject identity (sha256 of the delivered working-tree files; this record's own hash is naturally
not self-referential and is omitted): `command_queue.go 5e42f1d4c682c3acf86fe72e35ad84b98794f78906466b5a11c6b24cf360d9bf`,
`command_queue_test.go c486ed9d4c6d41d5efe71687df29bbbf268b733a6f16f5353b756624389d0e8e`,
`app.go ff26a5ee977d7a8790c41116cb48469522fad2b075bd6f8524ca2d70cf49cde9`,
`commands.go 8f9667ee3d04b72a7bcc2c52cc3d77cfbe2b812386a2275d19c41f96f14cb046`,
`outbox.go 9a704d713a7e3de13bb15919f37a32d01dd6d15b8892534c7958c6d295d9766e`,
`app_test.go 2af9a9b922971ae5cc7ae43e3e6c38464e76c333a1578fdfc54729d045522656`,
`plan 794da6ca8042718ad50196a12730859abddb7215b2a850e9005a4f7a88a12582`.
The first two values and the plan value are the apply-stage re-measurement; the other four are the
implement-stage files, untouched by the accepted findings, so their hashes still bind the delivered tree.

## 1. Deliverable shape (files, counts)

| file | status | lines (base → head) | note |
|---|---|---:|---|
| `daemon/internal/app/command_queue.go` | new | — → 251 | module: 5 types + 12 methods + `newCommandQueue` |
| `daemon/internal/app/command_queue_test.go` | new | — → 317 | 7 tests at the module interface, no `&daemon{}` |
| `daemon/internal/app/app.go` | edited | 2238 → 2221 | 5 fields removed, `commands commandQueue` added; `startBackground` reset; `releaseRun`/`clearCompletedCommandReceipts` delegate; `releaseCommandReceiptsLocked` deleted |
| `daemon/internal/app/commands.go` | edited | 1009 → 939 | wrappers delegate; `finishQueuedCommandLocked` deleted; 3 entry types moved |
| `daemon/internal/app/outbox.go` | edited | 1175 → 1164 | `markCommandAcknowledgementDelivered` delegates both receipt phases |
| `daemon/internal/app/app_test.go` | edited | 10078 → 10078 | 26 mechanical lines (`git diff -w`: 26 insertions / 26 deletions); column realignment only besides those |
| `docs/plans/2026-11-21-rf-w4-17a-command-queue-module-plan.md` | new | — → 242 | plan + recorded contract-preserving decisions D1-D4 |
| `docs/evidence/rf-w4-17a-command-queue-module-2026-11-21.md` | new | — | this record (method list, lock table, control-flow audit, mutants, gates); its own line count is not self-measured |

Line budget: no file crosses 1500 L. `app.go` (2221) and `app_test.go` (10078) already exceeded it
at base; both shrink or stay equal under this change, and the two new files are 251 and 317 L.

## 2. Interface and callers (clause 2)

`commandQueue` — 8 methods, each documented "caller holds `daemon.mu`", none locking, none I/O
(`grep -n "Lock()" daemon/internal/app/command_queue.go` → no matches):

| method | callers (after the change) |
|---|---|
| `enqueue(command) (*queuedCommand, bool)` | `enqueueCommandLocked` |
| `next() (*queuedCommand, bool)` | `nextQueuedCommand` |
| `lane(key, queued) bool` | `dispatchQueuedCommand` |
| `nextInLane(key) (*queuedCommand, bool)` | `nextLaneCommand` |
| `current(queued) bool` | `dispatchQueuedCommand`, `executeQueuedCommand` |
| `finish(key, registered, receipt)` | `finishQueuedCommand`, `finishDeferredCommand` |
| `dropRun(key)` | `releaseRun`, `clearCompletedCommandReceipts` |
| `markDelivered(key)` | `markCommandAcknowledgementDelivered` |

`commandReceipts` — 4 methods, same lock contract, held by the queue (`commandQueue.receipts`):

| method | callers (after the change) |
|---|---|
| `begin() uint64` | `beginCommandRequest` |
| `finish(requestID)` | `finishCommandRequest`, `enqueueSnapshotCommandsForRequest` (both exits) |
| `suppressed(requestID, key) bool` | `enqueueSnapshotCommandsForRequest` |
| `suppress(key)` | `markCommandAcknowledgementDelivered` |

The 8/4 split and its reason are the plan's decision D1 (design clause 1's escape hatch: the receipt
state is written by the outbox delivery path across a journal write, outside any queue call, and its
wire is `commandReceiptMu`, held while that write may block). D2 records why the snapshot loop stays
in `commands.go` (per-command gate on `commandWake`/`backgroundStop`, which is daemon lifecycle
state); D3 keeps `commandWake` and `commandReceiptMu` on the daemon; D4 makes `startBackground`
construct a fresh queue.

Mapping from the contract's example interface (design clause 2) to the delivered one, so the
deviation is auditable rather than implicit:

| contract example | delivered | note |
|---|---|---|
| `enqueue(command) (done, release)` | `enqueue(command) (*queuedCommand, bool)` | the wake channel is daemon lifecycle state (`commandWake`), so the daemon wrapper selects it; dedupe and the arrival append are the queue's |
| `enqueueSnapshot(requestID, commands)` | stays in `commands.go` | per-command gate on `commandWake`/`backgroundStop` (D2); it composes `enqueue` with `receipts.suppressed`/`receipts.finish` |
| `next()` | `next()` | identical |
| `nextInLane(key)` | `nextInLane(key)` | identical (also drops the drained lane) |
| `finish(queued, receipt)` | `finish(key, registered, receipt)` | one method covers the instance path and `finishDeferredCommand`'s key lookup (`registered == nil`) |
| `dropRun(key)` | `dropRun(key)` | identical (`releaseCommandReceiptsLocked`'s body) |
| `beginRequest()` / `finishRequest(id)` | `receipts.begin()` / `receipts.finish(id)` | on the receipt value, per D1 |
| — (not in the example) | `lane(key, queued) bool` | the lane put and the "start the drain" decision the executor needs |
| — (not in the example) | `current(queued) bool` | the membership check `dispatchQueuedCommand` and `executeQueuedCommand` both had inline |
| — (not in the example) | `markDelivered(key)` | the post-persist eviction in `markCommandAcknowledgementDelivered` |
| — (not in the example) | `receipts.suppressed` / `receipts.suppress` | the window read and the tombstone write |

The module is a pure value module: `grep -n "daemon\." daemon/internal/app/command_queue.go` matches
comments only (lines 13, 14, 201); imports are `protocol` and `state` (types) only; no lock call, no
`Store`/`log`/`os`/`net`/`io` reference, no `//nolint`, no goroutine and no channel creation beyond
the per-entry `done` channel the base created in `enqueueCommandLocked`.

### Native-evidence source mapping (arch-decision-2 R9)

`daemon/internal/app/command_queue.go` is listed in `SOURCE_PATHS` of
`scripts/ci/omo-control-native-evidence.py` (appended after the last entry, so the `:73`/`:75`/`:81`
coordinates the contract cites keep their lines), so the native-evidence identity set (`source_files`)
covers the file this slice's behaviour moved into. Every moved declaration and the listed path it left:

| moved declaration (now in `command_queue.go`) | base home (listed path) |
|---|---|
| `commandQueue` + its 8 methods | `app.go` (the five loose fields, `releaseCommandReceiptsLocked`, `clearCompletedCommandReceipts`, `releaseRun`, the `startBackground` reset), `commands.go` (arrival/lane/completion wrappers, `finishQueuedCommandLocked`) |
| `commandReceipts` + its 4 methods | `commands.go` (`beginCommandRequest`, `finishCommandRequest`, `enqueueSnapshotCommandsForRequest` window open/read/close), `outbox.go` (`markCommandAcknowledgementDelivered` tombstone + delivered phases) |
| `queuedCommand`, `commandKey`, `commandLane`, `newCommandQueue` | `app.go` (type declarations, the lazily created maps), `commands.go` (lane construction in `dispatchQueuedCommand`) |

## 3. Lock-site table before/after (every `daemon.mu` / `commandReceiptMu` site in the touched methods)

Extracted mechanically (scratch tool `w4-17a-locks.py`, `git show HEAD:<file>` vs the working tree);
`cRM` = `daemon.commandReceiptMu`. Order and count are identical in every row.

| method | base sequence | head sequence | identical |
|---|---|---|---|
| `enqueueCommand` | mu.Lock; mu.Unlock | mu.Lock; mu.Unlock | yes |
| `enqueueCommandLocked` | — | — | yes |
| `dispatchQueuedCommand` | mu.Lock; mu.Unlock ×6 | mu.Lock; mu.Unlock ×6 | yes |
| `runCommandLane` | — | — | yes |
| `nextLaneCommand` | mu.Lock; defer mu.Unlock | mu.Lock; defer mu.Unlock | yes |
| `executeQueuedCommand` | mu.Lock; mu.Unlock | mu.Lock; mu.Unlock | yes |
| `nextQueuedCommand` | mu.Lock; defer mu.Unlock | mu.Lock; defer mu.Unlock | yes |
| `finishQueuedCommand` | mu.Lock; defer mu.Unlock | mu.Lock; defer mu.Unlock | yes |
| `finishDeferredCommand` | mu.Lock; defer mu.Unlock | mu.Lock; defer mu.Unlock | yes |
| `finishQueuedCommandLocked` → `commandQueue.finish` | — (locked caller) | — (caller-held contract) | yes |
| `enqueueSnapshotCommandsForRequest` | cRM.Lock; defer cRM.Unlock; mu.Lock; mu.Unlock ×2 | identical | yes |
| `beginCommandRequest` | cRM.Lock; defer cRM.Unlock; mu.Lock; defer mu.Unlock | identical | yes |
| `finishCommandRequest` | cRM.Lock; defer cRM.Unlock; mu.Lock; mu.Unlock | identical | yes |
| `releaseRun` | cRM.Lock; defer cRM.Unlock; mu.Lock; mu.Unlock | identical | yes |
| `clearCompletedCommandReceipts` | cRM.Lock; defer cRM.Unlock; mu.Lock; defer mu.Unlock | identical | yes |
| `releaseCommandReceiptsLocked` → `commandQueue.dropRun` | — (locked caller) | — (caller-held contract) | yes |
| `markCommandAcknowledgementDelivered` | cRM.Lock; mu.Lock; mu.Unlock; cRM.Unlock; cRM.Lock; defer cRM.Unlock; mu.Lock; mu.Unlock | identical | yes |

No new mutex: the module takes no lock at all; `commandReceiptMu` keeps every base acquisition point
and stays outer to `daemon.mu` where base had it that way.

## 4. Base-vs-HEAD control-flow audit (lesson 32)

Point extraction: scratch tool `w4-17a-flow.py` (base revision vs working tree, per method); the
table maps every base decision/effect to its HEAD equivalent. No row is lost.

| method (base → head) | base control-flow points | HEAD equivalent | preserved |
|---|---|---|---|
| `enqueueCommandLocked` → same | empty-ID early return; wake/stop early return; lazy map init; duplicate early return (no wake); insert; append; return done+wake | first `if` holds both early-return conditions before any queue call; `enqueue` owns the lazy init, the duplicate return (`created=false` → `done`, no wake) and the insert+append | yes |
| `dispatchQueuedCommand` → same | membership early return; stop early return (finish without receipt); cancel branch (running/terminal/claimed/park); lane create + append; `lane.running` early return; start flag; `Add(1)` + goroutine | membership early return via `current`; stop early return unchanged; cancel branch unchanged byte-for-byte; create/append/`running`/start-flag moved verbatim into `lane`; `!lane(...)` reproduces the "already draining" return; `Add(1)` + goroutine unchanged | yes |
| `nextQueuedCommand` → `commandQueue.next` | empty early return; pop head; nil slot; re-slice; return true | identical statements inside the queue | yes |
| `nextLaneCommand` → `commandQueue.nextInLane` | `lane == nil \|\| empty` → delete + false; pop head; return true | identical | yes |
| `executeQueuedCommand` → same | membership early return; `ctx.Err()` early return (finish without receipt); handleCommand; finish | `current` for membership; the other three unchanged | yes |
| membership checks (`commands.go:94`, `:173`) → `commandQueue.current` | `byKey[key] != queued` / `== queued` | `queue.byKey[queued.key] == queued` | yes |
| `finishQueuedCommandLocked` → `commandQueue.finish` | `byKey[key] == queued` guard; close once (`completed`); set `completed`; evict unless `receipt && !delivered` | guard becomes `current == nil \|\| (registered != nil && current != registered)`; close/set/evict unchanged | yes |
| `finishDeferredCommand` → same | `byKey[key]` lookup guard, then the locked finish | the lookup is the `registered == nil` arm of `finish`; the wrapper calls it once | yes |
| `releaseCommandReceiptsLocked` → `commandQueue.dropRun` | range `byKey`; run match; close once (`completed`); evict | identical statements | yes |
| `enqueueSnapshotCommandsForRequest` → same | per command: window read; suppressed → `continue`; `enqueueCommandLocked`; nil completion → delete window + `return nil,false`; wake capture; append completion; after the loop: delete window; signal wake; `return completions,true` | `receipts.suppressed` per command; `receipts.finish` on both exits; the rest unchanged. The base read the window map once before the loop; HEAD reads it per command — inside the loop both locks (`commandReceiptMu` + `daemon.mu`) are held, so no window mutation can interleave (`suppress`/`finishRequest` both take `commandReceiptMu`), and tombstone additions mutate the same map instance, so the two forms are equivalent | yes |
| `beginCommandRequest` → same | lazy init; increment; create set; return id | `receipts.begin` with the same statements | yes |
| `finishCommandRequest` → same | requestID 0 early return; locks; delete window | the early return stays in the wrapper (so the lock sites are unchanged); `receipts.finish` deletes | yes |
| `releaseRun` → same | queue part: `releaseCommandReceiptsLocked` | `commands.dropRun(key)` at the same point, inside the same two locks | yes |
| `clearCompletedCommandReceipts` → same | `releaseCommandReceiptsLocked` under both locks | `commands.dropRun(key)` under both locks | yes |
| `markCommandAcknowledgementDelivered` → same | tombstone every live window (pre-write); persist; error early return; mark delivered + evict-if-completed (post-write) | `receipts.suppress` and `commands.markDelivered` at the same two phases; the persist branch and the error return unchanged | yes |
| `startBackground` → same | reset `queuedCommands` + `commandRequests` under `daemon.mu` | `daemon.commands = newCommandQueue()` under `daemon.mu` (also clears the empty arrival FIFO/lanes and restarts the window counter, recorded as plan D4; unobservable, no test or production path threads a window id across `startBackground`) | yes |

## 5. Red-mutant records (clause 4)

Each test in `command_queue_test.go` names its mutant. Recorded failure output (mutant applied to
`command_queue.go`, target test run alone, then reverted; the delivered module file hashes
`sha256 5e42f1d4c682c3acf86fe72e35ad84b98794f78906466b5a11c6b24cf360d9bf` before and after the round;
rows 7a/7b were added by the apply stage under the same rule):

| # | test | mutant | observed failure |
|---|---|---|---|
| 1 | `TestCommandQueueDeduplicatesByCommandKey` | delete the `byKey` existence check in `enqueue` | `command_queue_test.go:53: duplicate command key created a second entry` |
| 2 | `TestCommandQueueLanePreservesFIFOOrderPerRun` | `nextInLane` pops `pending[len-1]` (LIFO) | `command_queue_test.go:88: first lane pop = 0x…1860/true, want 0x…17c0` |
| 3 | `TestCommandQueueArrivalOrderAndLanesAreIndependentPerRun` | key the lane map by `RunID` only | `command_queue_test.go:133: the next generation of the same run id reused the previous generation's lane` |
| 4 | `TestCommandQueueDropRunReleasesExactlyOneRun` | `dropRun` closes waiters but never evicts | `command_queue_test.go:174: registered entries after dropping run-a = 3, want only run-b's` |
| 5 | `TestCommandQueueFinishHonoursReceiptAndDelivery` | `finish` ignores `delivered` (`if !receipt`) | `command_queue_test.go:235: completion kept an entry whose receipt was already delivered` |
| 5b | `TestCommandQueueFinishHonoursReceiptAndDelivery` | drop the nil-entry guard in `markDelivered` | `panic: runtime error: invalid memory address or nil pointer dereference` inside `(*commandQueue).markDelivered` (the absent-key resolution is a documented no-op) |
| 6 | `TestCommandQueueRequestWindowsAccountTombstones` | `suppress` returns after the first window | `command_queue_test.go:272: live windows were not both tombstoned` |
| 7a | `TestCommandQueueZeroValueCreatesItsMaps` | delete the `byKey` nil guard in `enqueue` | `panic: assignment to entry in nil map [recovered, repanicked]` inside `(*commandQueue).enqueue`, `command_queue.go:76`, call frame `command_queue_test.go:297` |
| 7b | `TestCommandQueueZeroValueCreatesItsMaps` | delete the `lanes` nil guard in `lane` | `panic: assignment to entry in nil map [recovered, repanicked]` inside `(*commandQueue).lane`, `command_queue.go:103` |

Two mutants survived the first attempt, and the tests were extended (not weakened) until each was
red: #3, because the test's runs differed in `RunID` while the mutant only drops the generation — the
test now also pins two generations of one `RunID` in separate lanes; #5, because the test only
exercised delivery *after* completion while the mutant breaks delivery *before* completion — the test
now pins that path too. The mutants were re-run and are red as recorded. No assertion of an existing
test was changed, so no further mutant records are required by the owner rules.

"No assertion change" is mechanical, not a claim: every `t.Fatal*`/`t.Error*` line and every
`if … daemon.…` guard in `app_test.go` (1166 lines at base, 1166 at HEAD) was extracted from both
revisions and compared after normalizing only the three moved-field expressions
(`daemon.queuedCommands` → `daemon.commands.byKey`, `daemon.commandRequests` →
`daemon.commands.receipts.requests`): 0 mismatches. Independently, the multiset of string literals on
both sides of the whole production+test diff is identical (5 distinct literals removed, the same 5
re-added), so no error, log or test-message string changed.

## 6. Gate results (exact commands, counts, exit codes)

All gates ran foreground in this worktree on 2026-11-21; heavy suites were serialized.

| gate | command (cwd) | result |
|---|---|---|
| gofmt | `gofmt -l ./internal/app/` (`daemon/`) | no output, exit 0 |
| vet | `go vet ./...` (`daemon/`) | exit 0 |
| build linux | `GOOS=linux go build ./...` (`daemon/`) | exit 0 |
| build windows | `GOOS=windows go build ./...` (`daemon/`) | exit 0 |
| extra cross-builds | `GOOS=linux GOARCH=arm64 go build ./...`; `GOOS=darwin GOARCH=arm64 go build ./...`; `GOOS=windows GOARCH=arm64 go build ./...` (`daemon/`) | exit 0 each |
| race probe (windows) | `go test -count=1 -race ./internal/app/ -run 'Command\|Queue\|Lane\|Outbox\|Receipt\|Acknowledg'` | refused: `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`; `CGO_ENABLED=1` retry: `cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%` → `[build failed]` — the race oracle is the WSL run |
| focused (windows, no race) | `go test -count=1 -v ./internal/app/ -run 'Command\|Queue\|Lane\|Outbox\|Receipt\|Acknowledg'` | final tree: `ok github.com/wxxb789/symmetry/daemon/internal/app 54.003s`, exit 0, 175 PASS lines, 0 FAIL lines (the first pass, before the extra "5b" assertion in the new test file, was `ok … 73.085s` with the same counts); 115 top-level tests selected (`go test -list`) |
| full (windows) | `go test -count=1 ./internal/app/...` | final tree: `ok github.com/wxxb789/symmetry/daemon/internal/app 330.382s`, exit 0 (slower than the first pass's `ok … 207.555s` because the WSL race suite was in flight on this host; both passes exit 0) |
| race (wsl Ubuntu-24.04), attempt 1 | `wsl -d Ubuntu-24.04 -u accept` → `go test -count=1 -race ./internal/app/...` (`daemon/`) | `panic: test timed out after 10m0s` at `FAIL … 600.152s`, no `--- FAIL` line and no `DATA RACE`: Go's default 10-minute test timeout killed a run that was stuck on the pre-existing hang diagnosed below |
| race cost control (windows, one P) | `go test -count=1 -cpu=1 ./internal/app/...` (`daemon/`) | `ok github.com/wxxb789/symmetry/daemon/internal/app 203.397s`, exit 0 — the suite is ~200 s with one logical CPU, and the shard diagnosis below shows normal race-instrumented durations, so the failed full WSL runs were the hang, not uniform race overhead |
| race (wsl Ubuntu-24.04), attempt 2 | same command with `-timeout 90m` | ran ~85 minutes without completing and was cancelled deliberately; the shard diagnosis below shows it was stuck on the same pre-existing hang (no attempt-2 result exists) |
| race (wsl, shard diagnosis) | sharded run `-race -timeout 90m -v -run '^Test[A-H]'` (script `wsl-race-shards.sh`, scratch), cancelled after ~55 minutes | the shard's output file stops growing at `=== RUN TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart/mixed_null_marker_survives_replay_and_restart`; every earlier test passed with normal durations (0.00-0.23 s), so the race cost is near-native on this host and the run was stuck on that one test, not slow overall |
| race hang attribution (HEAD) | `wsl … go test -count=1 -race -timeout 3m ./internal/app/ -run '^TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart$'` | `panic: test timed out after 3m0s`; the running tests are `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart` and its subtest `mixed_null_marker_survives_replay_and_restart` (3m0s each); frames are the test's own `func3` call and `net/http/httptest.(*Server).goServe`; `FAIL … 180.616s` |
| race hang attribution (base `79548368`, detached in the scratch clone) | same command in the clone at base | identical artifact: same panic, same two running tests, same `httptest` frames, `FAIL … 180.303s` — the hang is pre-existing on this host, not caused by this slice |
| race (wsl, full suite minus the pre-existing hanger) | `go test -count=1 -race -timeout 45m -skip '^TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart$' ./internal/app/...` | did not complete either; cancelled after ~20 minutes and its output file is empty (0 bytes: without `-v` Go buffers a single package's output), so a second stall is not named. The same loopback-`httptest` pattern also appears in `omo_attach_ack_test.go` and `pi_provider_bridge_test.go` (3 files, 15 `httptest` references) — a plausible second stall of the same class, not verified |
| race (wsl, focused subset) | `wsl -d Ubuntu-24.04 -u accept` → `go test -count=1 -race -timeout 30m -v ./internal/app/ -run 'TestCommandQueue\|TestCommandRequest\|TestReleaseRun\|TestSameRunCommands\|TestSameCommandID\|TestCancel\|TestDelivered\|TestAcknowledgement\|TestRenewal\|TestConclusiveCleanup\|TestBlockedInput\|TestBlockedCommand\|TestFailedAcknowledgement\|TestCancelledSnapshot\|TestScheduledCancel'` (`daemon/`) | `ok github.com/wxxb789/symmetry/daemon/internal/app 10.789s`, exit 0; 52 top-level PASS (all six `TestCommandQueue…` module tests among them), 0 FAIL, 0 `DATA RACE` — the touched paths under the race detector |
| module lock check | `grep -n "Lock()" daemon/internal/app/command_queue.go` | no matches (exit 1) |
| field-access gate | `grep -rc 'daemon\.\(commandQueue\|queuedCommands\|commandLanes\)' daemon/internal/app/*.go \| grep -v ':0$'`; `grep -rho … \| wc -l` | no non-zero file, 0 matches package-wide (even inside the module) |
| module purity | `grep -n "daemon\." daemon/internal/app/command_queue.go`; `grep -nE "daemon\.store\|daemon\.log\|os\.\|net/\|http\.\|io\." daemon/internal/app/command_queue.go`; `grep -rn "nolint"` over the diff and the new files | comments only / no matches / no matches (exit 1 each) |
| citation gate | snapshot commit `37a935a6` in a throwaway clone outside the worktree, carrying exactly this worktree's eight changed paths; `python3 scripts/ci/check-citation-existence.py --base 7954836892f39bc8a92abe11e5efcb5b3bac3476 --head HEAD` | `no audited document added or modified between 7954836892f3 and 37a935a6727c; nothing to check`, exit 0 |
| host-path / DLP | `git diff \| grep -i "users/"`; `grep -rnP '\\[u]'` over every added line and new file (pattern spelled with a character class so this record does not carry a backslash immediately followed by the letter u) | no matches (exit 1 each) |
| exported surface | `grep -nE "^func [A-Z]\|^type [A-Z]\|^var [A-Z]" daemon/internal/app/command_queue.go` | no matches (exit 1): the module adds no public API |
| whitespace | `git diff --check` | exit 0 (no whitespace errors) |
| stale-document scan | `grep -rn "command executor\|command queue\|command lane\|queued command\|daemon\.commands\|commandQueue\|commandLane" docs/design docs/runbooks contracts` | no match outside this record and the W1-05 move manifest (a record bound to its own subject revision): no design, runbook, contract or TODO document names the queue fields, so the slice leaves none stale and closes no tracked item |
| native-evidence identity set (arch-decision-2 R9) | `python3 scripts/ci/test-omo-control-native-evidence.py` (repo root) | `Ran 10 tests in 3.059s`, `OK`, exit 0 — run by the apply stage, after `daemon/internal/app/command_queue.go` joined `SOURCE_PATHS`; the moved-declaration → listed-path mapping is in section 2 |

### Apply-stage note (2026-11-21) — accepted simplify findings, gates re-run

The simplify review of `fc8e95a..a4770ed` was applied on top of this record's tree. Accepted set: the D1
rationale correction, the `dropRun` loop-variable rename, the zero-value queue test, the `SOURCE_PATHS`
addition (mapping in section 2) and this record's count/hash refresh. Five paths changed: the two module
files, the plan, this record, the evidence script. Every gate below ran foreground on that apply-stage
working tree and heavy runs were serialized; the header's identity hashes are the post-change values.

| gate | command (cwd) | result |
|---|---|---|
| gofmt | `gofmt -l ./internal/app/` (`daemon/`) | no output, exit 0 |
| vet | `go vet ./...` (`daemon/`) | exit 0 |
| build linux / windows | `GOOS=linux go build ./...`; `GOOS=windows go build ./...` (`daemon/`) | exit 0 each |
| extra cross-builds | `GOOS=linux\|darwin\|windows GOARCH=arm64 go build ./...` (`daemon/`) | exit 0 each (3 builds) |
| focused (windows, no race) | `go test -count=1 -v ./internal/app/ -run 'Command\|Queue\|Lane\|Outbox\|Receipt\|Acknowledg'` | `ok github.com/wxxb789/symmetry/daemon/internal/app 22.645s`, exit 0; 176 PASS lines, 0 FAIL; 116 top-level tests selected (`go test -list`, was 115) |
| full (windows) | `go test -count=1 ./internal/app/...` | `ok github.com/wxxb789/symmetry/daemon/internal/app 135.025s`, exit 0 |
| race (wsl Ubuntu-24.04, focused subset) | same `-run` pattern as the focused row, `-race -timeout 30m -v` (`daemon/`) | `ok github.com/wxxb789/symmetry/daemon/internal/app 7.887s`, exit 0; 53 top-level PASS (all 7 `TestCommandQueue…` module tests among them), 0 FAIL, 0 `DATA RACE` |
| race (wsl, full suite, re-attempt) | `go test -count=1 -race -timeout 20m ./internal/app/...` (`daemon/`) | `panic: test timed out after 20m0s`, `FAIL github.com/wxxb789/symmetry/daemon/internal/app 1200.159s`: the two running tests are `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart` and its `mixed_null_marker_survives_replay_and_restart` subtest (19m59s each) — the pre-existing hang of section 8, unchanged on the apply-stage tree; 0 `--- FAIL` lines, 0 `DATA RACE` |
| new-test mutants | `command_queue.go` with the `byKey` guard (7a) / the `lanes` guard (7b) deleted, target test alone | both red — `panic: assignment to entry in nil map`, reverted; module hash `5e42f1d4…` before and after (section 5) |
| field-access gate | `grep -rc 'daemon\.\(commandQueue\|queuedCommands\|commandLanes\|commandRequests\|nextCommandRequest\)' daemon/internal/app/*.go` | no non-zero file (exit 1): 0 matches anywhere |
| module lock / purity / exported surface / nolint | the section 6 rows re-run | unchanged: no lock call, comments-only `daemon.`, no exported symbol, no `nolint` |
| whitespace / host-path / DLP | `git diff --check`; `git diff \| grep '^+' \| grep -i "users/"`; `git diff \| grep '^+' \| grep -P '\\[u]'` | exit 0; no added line matches (exit 1 each) |
| citation gate | not re-run | the apply stage adds/modifies no audited document (`docs/runbooks/**`, `docs/design/quality.md`, `docs/archive/CLOSED-TODO.md`); the five changed paths are listed above |

## 7. Contract clause mapping

1. Pass condition: the three loose fields are gone from `daemon` (grep gate row); `commandQueue` has
   exactly 8 methods (section 2); queue invariants are tested without `&daemon{}`
   (`command_queue_test.go` constructs only `newCommandQueue()`/`commandReceipts{}`); existing app
   tests green (focused + full rows) with 26 mechanical lines replaced (`git diff -w`); lock order
   identical (section 3); no file crosses 1500 L (section 1).
2. Judging artifact: this record — method list with callers (section 2), lock-site table (section 3),
   control-flow audit (section 4), red-mutant records (section 5), Windows + WSL outputs (section 6).
3. Failure control: no ordering, dedupe, receipt/ack or outbox-order change (sections 3-4 plus the
   unchanged app suite); no new mutex; no queue method performs I/O (section 2); no test builds
   `&daemon{}` to test queue invariants; no existing assertion changed.
4. Out of scope items untouched: `runningRun`/run lifecycle, goal admission, test doubles, Control
   client — `git status` lists only the eight paths of section 1 (six Go files, this plan, this record).
5. Evidence plan: section 6, with one environment-limited gate named there: the WSL full-suite race
   run cannot complete on this host because of a pre-existing `httptest` hang in an unrelated test
   (base-verified); the touched paths are race-clean (focused race run) and CI runs the full race
   suite.
6. Mergeable alone: the change is self-contained in `daemon/internal/app` plus this slice's plan and
   evidence record.

## 8. Residual risks and limits

- Windows cannot run `-race` on this host (no cgo toolchain), so the race oracle for this slice is
  the WSL run — and the WSL *full-suite* run cannot complete on this host for a pre-existing reason:
  `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart/…mixed_null_marker_survives_replay_and_restart`
  hangs on an `httptest` loopback dial under `-race`, identically at HEAD (`FAIL … 180.616s`) and at
  the base revision (`FAIL … 180.303s`, same stack). The touched paths are race-clean (52 tests,
  `ok … 10.789s`, 0 `DATA RACE`; apply-stage re-run of the same subset: 53 top-level PASS,
  `ok … 7.887s`, 0 `DATA RACE`, section 6), and the CI race oracle remains
  `.github/workflows/ci.yml:1528-1530`: `test -race -count=1 -timeout 600s ./...` (all daemon
  packages, `internal/app` included) behind the stdio-only caller boundary. Reported here rather
  than fixed: the hang is outside this slice's owned files and pre-dates it. The apply stage
  re-attempted the full suite with `-timeout 20m` and got the identical artifact
  (`FAIL … 1200.159s`, same two running tests, section 6), so this limit still stands.
- `dropRun` keeps the base tolerance for entries already in the arrival FIFO or a lane: their
  consumers drop them through `current`. The queue test pins that tolerance explicitly; fixing it is
  out of scope (the contract requires `dropRun` to release exactly what `releaseCommandReceiptsLocked`
  released).
- The receipt bookkeeping is a sibling value type inside the queue rather than queue methods: its two
  delivery-path mutations are ordered by `commandReceiptMu` across a journal write, a contract the
  module's lock-free methods cannot carry, and one type would then span two lock contracts. The
  superseded method-budget reading of this reason was retired by the apply stage (arch-decision-2 sets
  the budget at most 12 methods and the file counts 12 either way); the reason, the options and the
  chosen amendment are recorded as plan D1.
- `app_test.go` shows column realignment inside the 18 rewritten composite literals (the removed key
  was the widest); `git diff -w` shows exactly 26 replaced lines and no assertion change.

## 9. Scoped simplification review

The repository asks for a scoped simplification review before completion; this slice is itself a
simplification, and nothing further was removable without losing behaviour or clarity. Every field of
the module's five types is read (`pending`/`byKey`/`lanes`/`receipts`/`requests`/`nextRequest`/
`command`/`key`/`done`/`completed`/`delivered`/`running` each occur at least 3 times in
`command_queue.go`), and every `commandQueue` method has at least one caller outside the module
(`enqueue`/`next`/`lane`/`nextInLane`/`markDelivered` one each, `current`/`finish`/`dropRun` two
each), so the module has no unexercised surface. The two dispatch helpers a coarser interface could
have dropped (`current` folded into the lane put, `lane`'s start flag folded into its caller) each
carry a load-bearing race guard or a base-verbatim decision, so they stay.

Counts on the delivered tree: production files +23/−121 (`app.go` +4/−21, `commands.go` +17/−87,
`outbox.go` +2/−13); the module adds 251 lines and its tests 317; `app_test.go` is +129/−129 raw,
which `git diff -w` reduces to the 26 replaced lines (the rest is the column realignment above). The
two deleted daemon methods are the module's bodies, not behaviour.

Post-merge simplification review (slice range `fc8e95a..a4770ed`, 2026-11-21): its applicable findings
were applied in the apply stage — the D1 rationale correction (the superseded method-budget argument
retired, decision unchanged), the `dropRun` loop variable no longer shadowing the `commandKey` type,
the zero-value queue test with its two mutants recorded in section 5, the `SOURCE_PATHS` addition for
arch-decision-2 R9, and this record's count/hash refresh. Its "merge `commandQueue` and
`commandReceipts` into one 12-method type" variant was left unapplied (it changes decision D1 and
awaits an explicit conductor ruling), and the clause 1 `no file > 1500 L` disclosure in section 1
stays exactly as recorded.
