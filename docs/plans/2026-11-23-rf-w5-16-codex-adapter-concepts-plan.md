# rf-w5-16 codex adapter concepts — plan

Contract: `goal-rf-w5-16-codex-adapter-concepts.md` (GDD state root, external to the repository),
with the 2026-11-21 arch-decision-2 conductor rulings (R2/R11, O8) and the 2026-11-22 comment policy
binding. Base revision: `e13450bc03499a62e77b125ac02596cdb433ce05` (W4-12c Recorder port merged;
W4-15a decoder, W4-15b staged driver and W4-15c relocation already merged). Branch:
`refactor/rf-w5-16-codex-adapter-concepts`. Owned files are the contract's strict set plus this plan
and `docs/TODO.md` / `docs/archive/CLOSED-TODO.md`; clause 2's `answer.md` is delivered as this plan
plus the implement-stage report (no `docs/evidence/` record is written: that path is outside the
contract's strict owned set).

## 1. Owned-set re-derivation at dispatch

| Contract entry | HEAD reality | Action |
| --- | --- | --- |
| `codex/adapter.go` 2,619 L @ `79548368` | W4-15b's staged driver absorbed the generic launch shell: `adapter.go` is 2,617 L at `e13450bc`; four concerns remain (staged startup/session wiring, JSON-RPC correlation + notification staging, cancel-attempt machine, translation) | split into `adapter.go` + `cancel_state.go` + `rpc_session.go` + `translate.go` |
| `codex/adapter_test.go` 3,144 L | 3,206 L / 82 test functions + 60 helpers at `e13450bc` | split into `adapter_test.go` + `cancel_test.go` + `rpc_session_test.go` + `adapter_support_test.go` |
| cancel state six fields `:287,:309-313` | same six fields, span `:287-313`; 43 field-write rows in 18 functions (scan below) | owned by `cancelTracker` |
| "W4-13 also edits `codex/*_test.go`, one writer at a time" | no W4-13 branch open against `codex/`; this slice is the writer | record the order (this slice's split reasons are in section 6) |
| `gh pr list` re-verification | see the implement report (run at dispatch) | — |

## 2. Module layout

Every file stays ≤ 1,500 L. `wc -l` before/after is in the implement report.

| File | Owns |
| --- | --- |
| `codex/adapter.go` | wire constants + package error vars, `Adapter`, `Start`, process/transport lifecycle (`LaunchTransport`, `launchWatcher`, `retainStartFailure*`, `queuePreReadyOutput`, `failProcessOutputBeforeReady`, `failFraming`, `handleProcessOutput*`), session surface (`Open`, `OpenNativeSession`, `StartTurn`, `AdmitNativeTurn`, `Control`, `Wait`, `WaitTurn`, `Close`, `closeOnce`, `CloseNativeSession`, `watchProcess`), session state core (`emit*`, `complete*`, `signalTurnObserved*`, `currentResult`, `sessionError*`, `rememberNativeFailureReason`, `failureReasonOr`, `partialProcessResult*`, `ensureUsable`, `matches*`, `hasTerminalResult`, `markTerminalSeen`, `resultKindForTurn`) |
| `codex/cancel_state.go` | `cancelAttemptState`/`cancelAttempt`/`pendingInterruptedTerminal` types, `cancelTracker` (14 transitions), the cancel-flow session methods (`acquireCancelAttempt`, `beginCancelAttempt`, `markCancelAttemptWritten`, `failCancelAttempt`, `acceptCancelAttempt`, `rejectCancelAttempt`, `finishCancelAttempt`, `waitCancelAttempt`, `executeCancelAttempt`, `performCancelAttempt` — apply stage F4 — `isExplicitCancelRejection`, `deferInterruptedTerminal`, `failDeferredCancelOnProcessExit`) |
| `codex/rpc_session.go` | `retiredRPC`, `rpcCorrelator` + `rpcResolution`, `stagedNotification`, frame dispatch (`handleFrame`, `handleServerRequest`, `handleResponseWithOutputLock`, `handleNotification*`), notification staging/dedupe/replay/overflow (`stageTurnNotification`, `drainPendingTurnNotifications`, `closeReplayBarrierLocked`, `waitForTurnReplay`, `preparePendingTurnReplayAfterProcessExit`, `failPendingTurnNotificationOverflow`, `markNotification*`, `failNotificationOverflow`, `stageThreadStarted`), call plumbing (`call`, `callWithHooks`, `finishRPCResponse`, `notify`, `writeInput`, `removePending`, `retirePending`), terminal pipeline (`handleTurnCompleted`, `settlePendingInterruptedTerminal*`, `resolvePendingCallsAfterTerminal`, `publishTurnTerminal`) |
| `codex/translate.go` | pure mappings, no session state: `resultKindForTurn`, `nativeItemIsTool`, `nativeItemID`, `itemLifecycleEventKind`, `isTurnNotification`, `notificationDedupeKey`, `turnStartedDedupeKey`/`itemLifecycleDedupeKey`/`terminalDedupeKey` (apply stage F6), `notificationDigestKey`, `summarizeProcessFailure`, `capabilityForControl`, `validJSONObject`, `buildTurnPrompt`, `missingResultSummary`, `completedTerminalKind` |

Move mechanics: `nativeSession` keeps its name and all other fields; the six cancel fields become
`session.cancelTracker.<field>` (the field cannot be named `cancel` — `nativeSession.cancel` is the
context cancel func), the RPC maps/ids become `session.rpc.<field>`. Field paths that tests touch
are updated in the same commit (`native.cancelTracker.deferredTerminal`, `native.rpc.pending`,
`session.rpc.retiredIDs`, `session.rpc.retire(id, retiredRPC{})`).

## 3. `cancelTracker` — 14 transitions and their callers

All methods are lock-free; every caller holds `nativeSession.mutex` (one type-level note in the file
instead of 14 repeated ones, comment policy §4). One process-wide hook
`cancelViolationHook func(cancelTransition, uint64)` is nil in production; `cancelViolation` is a
single nil-compare jump, so production writes and order are byte-identical. The hook takes a typed
transition code, not a format string, so the non-test string-literal multiset does not change.

| # | Transition | Callers (after the split) |
| --- | --- | --- |
| 1 | `acquire() (id, owner, done)` | `session.acquireCancelAttempt` ← `performCancelAttempt` (`Control`/`closeOnce`; apply stage F4), `beginCancelAttempt` (tests) |
| 2 | `bindRPC(rpcID)` | `callWithHooks` (interrupt request registration) |
| 3 | `forgetRPC(rpcID) uint64` | `handleResponseWithOutputLock`, `settlePendingInterruptedTerminalLocked`, `resolvePendingCallsAfterTerminal`, `removePending`, `retirePending` |
| 4 | `observeResponse(attemptID)` | `handleResponseWithOutputLock` (found and retired branches) |
| 5 | `markWritten(id)` | `session.markCancelAttemptWritten` ← `executeCancelAttempt`'s `afterWrite` |
| 6 | `accept(id)` | `session.acceptCancelAttempt` ← `performCancelAttempt` (`Control`/`closeOnce`); `handleResponseWithOutputLock` |
| 7 | `reject(id)` | `session.rejectCancelAttempt` ← `performCancelAttempt` (`Control`/`closeOnce`); `handleResponseWithOutputLock` |
| 8 | `fail(id)` | `session.failCancelAttempt` ← `performCancelAttempt` (`Control`/`closeOnce`) |
| 9 | `finish(id, err)` | `session.finishCancelAttempt` ← `performCancelAttempt` (`Control`/`closeOnce`) |
| 10 | `deferTerminal(terminal)` | `session.deferInterruptedTerminal` ← `handleTurnCompleted` |
| 11 | `takeDeferredTerminal(id)` | `handleResponseWithOutputLock`, `failCancelAttempt`, `failDeferredCancelOnProcessExit` |
| 12 | `reset()` | `failProcessOutputBeforeReady`, `failFraming` |
| 13 | `requested()` | `closeOnce` (active-turn predicate), `session.resultKindForTurn` |
| 14 | `resolvePendingInterrupt(rpcID, status)` | `resolvePendingCallsAfterTerminal` |

Package-level helpers in the same file, not transitions: `hasWrittenCancelAttempt(tracker)` (the
derived scan behind every cache refresh — the reason a stale `cancelRequested` is impossible),
`newPendingInterruptedTerminal(...)` (keeps composite-literal field writes inside the owner file),
`cancelViolation`, `newCancelTracker`.

Edge-case semantics preserved exactly:

- `markWritten`/`accept`/`reject`/`fail`/`finish` keep every base guard and every base silent return;
  the hook fires only where base silently ignored a request that a correct flow cannot produce
  (`markWritten` without intent, `accept` without a written attempt, `reject` off intent/written,
  `fail` on a settled attempt, `finish` on an unknown attempt). `fail` on a `Written` attempt stays a
  documented no-op (that is how a written-but-unanswered interrupt keeps its classification), and
  `finish`'s `doneClosed` guard stays because the process-exit path closes the same channel.
- `requested()` returns the cached flag; `cancelRequested` is written only by refresh sites inside the
  tracker, and base's `cancelRequested || hasWrittenCancelAttemptLocked()` is equal to that cache at
  every lock-held point.
- `takeDeferredTerminal` keeps take-once and the Cancelled-vs-Failed rule (`hasWrittenCancelAttempt`).
- `reset()` is `pendingCancelAttempts = make(...); deferredTerminal = nil` — the framing and
  pre-ready failure paths still leave `cancelAttempts`, `cancelFlight` and `cancelRequested` intact.
- `resolvePendingInterrupt` folds `resolvePendingCallsAfterTerminal`'s two base branches verbatim
  (`interrupted && intent|written` → mark `terminalInterrupted`, keep pending; `interrupted &&
  written|accepted` → synthesize the empty success result, `Written` unreachable as in base).
- `failDeferredCancelOnProcessExit` and `waitCancelAttempt` read attempt fields directly and stay
  inside `cancel_state.go`, so the tracker keeps 14 methods and no accessor sprawl.
- The redundant lazy `make` in `acquire` is kept (contract option (a)): the base guard is
  byte-identical, and removing it is not provably unobservable for a directly constructed session.

## 4. `rpcCorrelator` and `rpcResolution`

`rpcCorrelator` is a lock-free value on the session (`session.rpc`), used only under
`session.mutex`; it owns `pending`, `pendingMethods`, `retiredIDs`, `retiredOrder` and the atomic
request-id counter (`nextRequestID()`). `resolve(id) rpcResolution` returns one typed answer:
`{waiter, method, cancelAttempt, found, retired}` — the found case deletes the pending entries, the
retired case carries the retired record's method and cancel-attempt id. `handleResponseWithOutputLock`
feeds that resolution to the tracker (`forgetRPC` → `observeResponse` → `reject|takeDeferredTerminal|
accept+takeDeferredTerminal`) inside one `session.mutex` critical section, exactly as today (R11/O8).
`forget`/`retire`/`reset` keep the base write positions; `retirePending` reads the bound attempt id
through `forgetRPC` before deleting, preserving base's write order per path.

## 5. Test plan

New files:

- `cancel_state_test.go`: tracker-only, no session, no process. Table-driven legal sequences
  (intent→written→accepted; intent→written→failed; intent→failed from a write failure; flight
  shared by two acquire calls; deferred terminal defer/take-once plus the Cancelled-vs-Failed rule;
  process-exit settlement of intent and written attempts; RPC bind/forget/observe; reset), plus
  illegal transitions (markWritten after accept, accept before write, reject/fail after settle,
  finish on an unknown attempt) asserting the typed hook code. One committed go/ast test
  (`TestCancelFieldsWrittenOnlyInOwnerFile`) proves 0 cancel-field writes outside `cancel_state.go`.
- `translate_test.go`: tables over recorded fixture items and turn statuses for `resultKindForTurn`,
  `completedTerminalKind`, `missingResultSummary`, `nativeItemIsTool`, `nativeItemID`,
  `itemLifecycleEventKind`, `isTurnNotification`, `notificationDedupeKey`, `notificationDigestKey`,
  `summarizeProcessFailure`, `capabilityForControl`, `validJSONObject`, `buildTurnPrompt`.
- Split of `adapter_test.go` (functions verbatim, names unchanged, every file ≤ 1,500 L):
  - `adapter_test.go`: startup/transport/open/turn-admission, pre-ready and framing failure, sink
    failure, terminal/process exit, `Wait`/`WaitTurn`/`Close` lifecycle, result cloning, schema
    projection, lifecycle fixture, thread-barrier text.
  - `cancel_test.go`: `Control` cancel receipts, cancel/interrupted classification, deferred
    interrupted terminals, shared-interrupt flight, blocked cancel write, late interrupt responses
    for retired ids, `WaitTurn` vs cancel acknowledgement.
  - `rpc_session_test.go`: server-request rejection, response-shape and null-error handling, retired
    rpc ids, notification dedupe/staging/replay/overflow, blocked RPC write, open-context deadline,
    token usage/native error decoding, terminal phase parsing, raw-protocol-identity redaction.
  - `adapter_support_test.go`: shared helpers, fake processes, sinks, fixtures (mirrors the claude
    adapter's `adapter_support_test.go` convention).

Red mutants: ≥ 1 per transition group (write-state, deferred-terminal, flight/RPC-glue,
reset/requested) and ≥ 1 for the translate table; each mutant is applied to the final tree, the
failing test named, then reverted. Deleting a superseded test requires its own recorded mutant; none
is planned.

## 6. Evidence plan (all foreground, working directory `daemon/`)

`gofmt -l internal/harness/codex`; `go vet ./...`; `GOOS=linux go build ./...`;
`GOOS=windows go build ./...`; `go test -list '.*' ./internal/harness/codex/` before/after (base name
set unchanged, new names listed); Windows `go test -count=1 ./internal/harness/...`; WSL
`wsl -d Ubuntu-24.04 -u accept bash -lc 'cd /mnt/q/repos/wt-w5-16-codex-adapter/symmetry/daemon &&
go test -count=1 -race ./internal/harness/... ./internal/app/...'` (SKIP list, no new skips);
write-site scan (committed go/ast test); lock-site table before/after; per-function write-sequence
table; error-string multiset (`rg -o '"[^"]*"'` over non-test `codex/*.go`, base vs HEAD);
base-vs-HEAD declaration token comparison for every moved declaration (gofmt-canonicalised Go/AST
equivalence, whitelisting the documented tracker/rpc call-site rewrites); `git diff --check`;
`wc -l` table; the slow CI race job is conductor-verified on the PR. Baseline captures (lock sites,
write sites, test-name list, string multiset) were taken at `e13450bc` before the first edit.

Never run the measurement harness concurrently with the full suite (host memory): the gate runs are
sequential.

## 7. Out of scope

Staged driver/launch grant (W4-15b), decoder (W4-15a), probe and its tests, adapter-owned policy
(A3), fixture pruning (W5-10), any protocol or behaviour change, the `codex.Probe` nil-context
re-pin TODO (probe tests are not owned by this contract — left in `docs/TODO.md` with a note),
`docs/evidence/**` (outside the strict owned set).

## 8. Ambiguities and calls made

1. `pendingCancelAttempts` ownership vs RPC correlation: the contract assigns the field to the
   tracker, so every read/write goes through `bindRPC`/`forgetRPC`/`resolvePendingInterrupt`; the
   correlator never touches it. Recorded because the two files meet here.
2. `publishTurnTerminal` and `settlePendingInterruptedTerminal*` are stateful emit paths, so they stay
   on the session (contract §3: "stateful emit/complete paths stay on the session and call them") and
   live in `rpc_session.go`; their pure parts (`completedTerminalKind`, `missingResultSummary`) moved
   to `translate.go`.
3. `resultKindForTurn` keeps base's early return (no lock for non-interrupted statuses) so its lock
   sites are unchanged.

## 9. Implement report — sizes and layout

`wc -l` at `e13450bc` → at HEAD (`daemon/internal/harness/codex/`):

| file | before | after |
| --- | --- | --- |
| `adapter.go` | 2,617 | 1,331 |
| `cancel_state.go` | — | 405 |
| `rpc_session.go` | — | 889 |
| `translate.go` | — | 167 |
| `adapter_test.go` | 3,206 | 1,296 |
| `cancel_test.go` | — | 493 |
| `rpc_session_test.go` | — | 848 |
| `adapter_support_test.go` | — | 609 |
| `cancel_state_test.go` | — | 682 |
| `translate_test.go` | — | 396 |

Every file is under 1,500 L; every other file in the package is untouched (`framing.go` 205,
`rpc.go` 562, `schema.go` 475, `probe.go` 315, and the native/live/sandbox tests).

Post-apply sizes (after the simplify apply of 2026-11-23) are in section 17.1; the numbers above are
the implement-stage record.

## 10. Lock-site table before/after

Script: per-function `.Lock()` sequence, base `adapter.go` vs HEAD's four source files
(`locks.awk`; tables captured pre-edit and post-edit). Result: **58 functions carry lock sites at
base, 58 at HEAD, and all 58 sequences are byte-equal** — no function gained, lost or reordered a
lock acquisition. Samples: `watchProcess` `mu outputMu mu mu outputMu mu mu`; `AdmitNativeTurn`
`mu mu mu`; `handleResponseWithOutputLock` `mu`; `publishTurnTerminal` `emitMu`;
`settlePendingInterruptedTerminal` `outputMu`; `settlePendingInterruptedTerminalLocked` `mu`;
`acquireCancelAttempt`/`markCancelAttemptWritten`/`rejectCancelAttempt`/`failCancelAttempt`/
`acceptCancelAttempt`/`finishCancelAttempt`/`deferInterruptedTerminal`/`waitCancelAttempt` `mu`
each. `Tracker` methods carry no lock site (they are lock-free by construction; the committed
`assertCancelTrackerShape` also fails if a lock field appears on `cancelTracker`).

## 11. Per-function cancel-field write sequence

18 functions wrote cancel state at base; the table is base write order → HEAD performer, all inside
one `session.mutex` critical section per path:

| base function | writes at base, in order | HEAD performer |
| --- | --- | --- |
| `newNativeSession` | `cancelAttempts=make`, `pendingCancelAttempts=make` | `newCancelTracker()` (map make order `pendingCancelAttempts` then `cancelAttempts`; no reader between) |
| `failProcessOutputBeforeReady` | `pendingCancelAttempts=make`, `deferredTerminal=nil` | `cancelTracker.reset()` |
| `failFraming` | `pendingCancelAttempts=make`, `deferredTerminal=nil` | `cancelTracker.reset()` |
| `handleResponseWithOutputLock` | `delete(pendingCancelAttempts,id)`, `attempt.responseObserved` (found, retired) | `forgetRPC(id)` at the same statement position, `observeResponse(...)` |
| `deferInterruptedTerminal` | `attempt.terminalInterrupted`, `deferredTerminal=terminal` | `deferTerminal(terminal)` |
| `settlePendingInterruptedTerminalLocked` | `delete(pendingCancelAttempts,attempt.rpcID)` after the two rpc-map deletes | `forgetRPC(...)` at the same position |
| `resolvePendingCallsAfterTerminal` | `attempt.terminalInterrupted` (suspend branch); `delete(pendingCancelAttempts,id)` last | `resolvePendingInterrupt(id, status)`; `forgetRPC(id)` after the switch |
| `callWithHooks` | `pendingCancelAttempts[id]=flight`, `attempt.rpcID=id` | `bindRPC(id)` after `rpc.register` |
| `removePending` | `delete(pendingCancelAttempts,id)` last | `forgetRPC(id)` last |
| `retirePending` | read binding, `delete` pending/pendingMethods/pendingCancelAttempts, retire metadata | `forgetRPC(id)` still deletes last; its binding read moved after the two rpc-map deletes (no reader between; documented deviation) |
| `markCancelAttemptWritten` | `attempt.state=written`, cache refresh | `markWritten(id)` |
| `failCancelAttempt` | `attempt.state=failed`, refresh, take deferred | `fail(id)` |
| `acceptCancelAttemptLocked` | `attempt.state=accepted`, refresh | `accept(id)` |
| `rejectCancelAttemptLocked` | `attempt.state=failed`, refresh, take deferred (refresh on both branches) | `reject(id)` (same two refresh positions) |
| `takeDeferredTerminalLocked` | `deferredTerminal=nil` after the read | `takeDeferredTerminal(id)` |
| `refreshCancelRequestedLocked` | `cancelRequested=<derived>` | inline `tracker.cancelRequested = hasWrittenCancelAttempt(tracker)` at each transition's base refresh point |
| `acquireCancelAttempt` | lazy `make`, `nextCancelAttempt++`, `cancelAttempts[id]=attempt`, `cancelFlight=attempt` | `acquire()` (lazy `make` kept) |
| `finishCancelAttempt` | `attempt.state` (intent only), refresh, `attempt.err`, `cancelFlight=nil`, `doneClosed=true`, `close(done)` | `finish(id, err)` |
| `failDeferredCancelOnProcessExit` | `attempt.state`/`attempt.err`, refresh, `cancelFlight=nil`, take deferred, `doneClosed`, `close(done)` | same function, now in `cancel_state.go` with `cancelTracker.` field paths |

Two order notes, both inside a single uninterrupted critical section with no intervening read of the
reordered field: the reset pair (`rpc.reset()` then `cancelTracker.reset()`) interleaves the rpc-map
clears with the cancel clears differently from base; `retirePending` reads the binding in
`forgetRPC` after the two rpc-map deletes instead of before them.

## 12. Error-string and literal multiset

Post-apply numbers are in section 17.1; the paragraph below is the implement-stage record.


`rg -o '"[^"]*"'` over the eight non-test `codex/*.go` files, base vs HEAD: the raw diff is 18 lines
and every one is an import-path literal whose count scales with the file count. Excluding import
blocks, the literal multiset is exactly equal: 682 literals, 391 distinct, zero differing counts
(`cancelViolation` takes a typed transition code precisely so the hook adds no literal).

Comment audit (comment policy §4): base `adapter.go` holds 80 `//` lines and HEAD's four source
files hold 92; the diff is additions only — the tracker's lock-discipline note, the
`cancelTransition`/hook note, `interruptRPCOutcome`, and the correlator/resolution lock-discipline
and purpose notes. No base comment was reworded or deleted. Test comments: base `adapter_test.go`
holds 10 `//` lines and the four split files hold 10 (0/5/2/3), so the split lost none.

## 13. Control-flow audit (base vs HEAD token diff, go/printer canonicalised)

The apply stage's delta to this audit (three functions) is in section 17.1; the table below is the
implement-stage record.


Declaration comparison over all non-test files: 0 base declarations missing except the nine
intentionally removed `*Locked`/`*IDLocked` helpers, 24 changed declarations (all in the plan's
whitelist), 34 new declarations (the tracker, correlator, resolution, transitions and pure helpers).
Token-level diffs for the eight named functions:

| function | differing regions | regions |
| --- | --- | --- |
| `Control` | 0 | identical |
| `watchProcess` | 0 | identical |
| `closeOnce` | 1 | `!session.cancelRequested` → `!session.cancelTracker.requested()` |
| `failFraming` | 1 | the six-line reset block → `rpc.reset()`, `cancelTracker.reset()` |
| `failProcessOutputBeforeReady` | 1 | the same reset block |
| `publishTurnTerminal` | 3 | `completedTerminalKind(...)`, `missingResultSummary(...)`, the folded `status == "completed"` kind override |
| `callWithHooks` | 4 | `rpc.nextRequestID()`, `rpc.register`, `cancelTracker.bindRPC(id)`, the dropped `cancelFlight != nil` guard |
| `handleResponseWithOutputLock` | 28 | `rpc.resolve(id)` for the four map reads/deletes, `forgetRPC`, `observeResponse`, and `accept|reject|takeDeferredTerminal` transition names; every branch condition, comparison and statement order is unchanged |

Test declarations: 0 missing, 5 changed — exactly the permitted field-path edits
(`native.cancelTracker.deferredTerminal` in two tests, `session.rpc.retire(id, retiredRPC{})` +
`session.rpc.retiredIDs` in `TestRetiredRPCIDsEvictOldestAndKeepLateResponsesClassifiable`,
`native.rpc.pending` in `TestNullRPCErrorCannotSatisfyOpenResponse` and
`TestUnmatchedRPCResponseFailsSessionAndReleasesPendingWaiters`).

## 14. Test names, skips and new tests

`go test -list '.*'`: 116 base names → 138; 0 base names lost, 0 duplicates, 22 new names
(`TestCancel*` × 9, `translate_test.go` × 13). No test file adds a `t.Skip`.

## 15. Red mutants (each applied to the final tree, observed failing, reverted)

| # | group | mutation | failing test and observed message |
| --- | --- | --- | --- |
| M1 | write-state | `cancelTracker.accept`'s written guard dropped (`if attempt != nil`) | `TestCancelViolationHookFiresOnIllegalTransitions/accept_before_write`: "state after violation = 3, want 1 unchanged" |
| M2 | deferred terminal | `takeDeferredTerminal` no longer clears `deferredTerminal` | `TestCancelDeferredTerminalTakeOnce/intent_defers_as_failed`: "second take returned …, want take-once" |
| M3 | flight/RPC glue | `resolvePendingInterrupt` no longer marks `terminalInterrupted` | `TestCancelResolvePendingInterrupt/written_suspends`: "terminalInterrupted = false, want true" |
| M4 | reset/requested | `reset` also clears `cancelRequested` | `TestCancelResetKeepsFlightState`: "reset dropped the cancellation evidence" |
| M5 | translate table | `itemLifecycleEventKind` always returns `EventNativeFrame` | `TestItemLifecycleEventKind`: `… (item/started, {"type":"functionCall"}) = "native_frame", want "tool_started"` |
| M6 | owner-file scan | a cancel-field write added to `rpc_session.go` | `TestCancelFieldsWrittenOnlyInOwnerFile`: "rpc_session.go:878 writes cancelTracker.deferredTerminal outside cancel_state.go" |
| M7 | frozen keys | `notificationDedupeKey` prefix `"turn-started:"` → `"turnStarted:"` | `TestNotificationDedupeKey`: `… = "turnStarted:thread-1:turn-1", want "turn-started:thread-1:turn-1"` |

No test was deleted, so no deletion needs a mutant.

## 16. Gates (all foreground, working directory `daemon/`)

This table is the implement-stage run; the apply-stage rerun is in section 17.2.


| gate | result |
| --- | --- |
| `gofmt -l internal/harness/codex` | empty (exit 0) |
| `go vet ./...` | exit 0 |
| `GOOS=linux go build ./...` | exit 0 |
| `GOOS=windows go build ./...` | exit 0 |
| `go test -list '.*' ./internal/harness/codex/` before/after | 116 → 138 names, no loss, no duplicates |
| `go test -count=1 ./internal/harness/...` (Windows) | 9/9 packages ok |
| WSL `go test -count=1 -race ./internal/harness/... ./internal/app/...` | harness 9/9 packages ok with `-race`; `internal/app` hit Go's default 10-minute package budget (600.124 s) inside `TestOutputBackpressureRecoversWithOneTruncationMarker` — the repository's documented slowest test (170.05 s in CI, 917.8 s here under `-race`, previously timed out the same way for W4-21) |
| WSL accepted rerun: `go test -count=1 -race -timeout 30m -skip TestOutputBackpressureRecoversWithOneTruncationMarker ./internal/harness/... ./internal/app/...` | **exit 0**, 11/11 packages ok (`codex` 45.685 s, `app` 644.017 s), wall 11m10s; the same two flags W4-21's evidence documents |
| WSL SKIP list | 44 skips, all pre-existing environment gates (live/native acceptance, Windows-only, hosted-runner cases); no test file in the diff contains a `t.Skip` |
| write-site scan | committed `TestCancelFieldsWrittenOnlyInOwnerFile` (green; red under M6) |
| lock-site table before/after | 58/58 identical sequences |
| write-sequence table | section 11 |
| error-string multiset | equal outside import blocks (682 literals) |
| fixture-replay suites | green inside the Windows harness run (`rpc_sandbox_test.go`, lifecycle fixture) |
| red-mutant records | 7, section 15 |
| control-flow audit | section 13 |
| `wc -l` table | section 9 |
| `git diff --check` | exit 0 |
| slow oracle (CI daemon race job) | conductor-verified on the PR |
| repo gate for the touched closure document: `python scripts/ci/check-citation-existence.py --base HEAD --head <scratch tree commit>` | exit 0, "1 changed document(s) pass the path and symbol check (524 path citation(s))"; run against a dangling scratch commit `ed724979` built from the working tree with a temporary `GIT_INDEX_FILE` (no branch, index or ref touched) |
| repo gate for records: `python scripts/ci/check-record-cleanliness.py --base HEAD --head ed724979` | exit 0, "no record added or modified … nothing to check" (`docs/evaluations/**`, `docs/evidence/**` untouched) |

## 17. Apply stage — accepted simplify findings (2026-11-23)

The simplify pass over `e13450bc..2571fd56` (external GDD artifact) produced 21 findings; this stage
dispositions them under the conductor brief's rule — accept every finding inside the owned set that
does not weaken tests, types, validation or permissions. `F` numbers are the simplify report's.

| # | finding | disposition | change |
| --- | --- | --- | --- |
| F1 | dead `blockingFakeNativeProcess.WriteInput` override | applied | override deleted from `adapter_support_test.go`; the embedded fake still satisfies the process seam |
| F2 | dead `wantAttempts` fixture field | applied | field deleted from `cancel_state_test.go` |
| F3 | `cancelAttempt.terminalInterrupted` write-only in production | applied as a debt record | `docs/TODO.md` entry naming the write and read sites; no code change (deleting the field would rewrite the frozen tracker assertions) |
| F4 | the 17-line cancel-attempt sequence duplicated in `Control` and `closeOnce` | applied | `session.performCancelAttempt(ctx, threadID, turnID)` in `cancel_state.go`; both callers keep their own context/timeout and `cancel()` handling |
| F5 | found/retired interrupt settlement duplicated in `handleResponseWithOutputLock` | applied | one settlement block entered per resolution; `forgetRPC` stays in the found branch and the retired branch keeps its `cancelAttempt != 0` guard |
| F6 | dedupe keys re-derived inline in the notification handlers | applied | `turnStartedDedupeKey`/`itemLifecycleDedupeKey`/`terminalDedupeKey` in `translate.go`, called by both `notificationDedupeKey` and the handlers, so the replay comparison has one source |
| F7 | overflow escalation repeated at five notification sites | applied | `markNotificationDeduplicated(frame, preDedupeKey, key)`; `rememberNativeFailureReason` stays after the seen check and before the emit |
| F8 | unreachable `Written` disjunct in `resolvePendingInterrupt` | not applied | the disjunct is a deliberately preserved base guard (section 3 records it as kept), and removing a state guard is a validation reduction; a conductor ruling is requested |
| F9–F11, F13, F14 | test-side duplication: local `controlResult` ×4, deferred-terminal introspection ×2, the -32001 envelope ×4 and its assertion ×3, the inline adapter literal ×2, the delta fixture ×4, repeated wire payloads | applied | shared `controlResult`, `assertDeferredIncomplete`, `interruptRejectionResponse`, `assertRejectedNonApplied`, `adapterFor`, `deltaNotification`, `initializeReply`, `turnStartedNotification`, `statusChangedNotification` |
| F12 note | the finding's literal replacement `fakeAdapter(process.fakeNativeProcess)` | applied with correction | that call returns the embedded fake, dropping the blocking override: tried at one site, `TestBlockedRPCWriteIsCancelledAndDoesNotLeak` failed after the 30 s guard ("Open() did not enter blocked native write"), then reverted; `adapterFor(process, process.fakeNativeProcess)` returns the wrapper and attaches the sink to the embedded fake |
| F14 note | the finding listed four initialize replies | corrected | six existed (two more in `rpc_session_test.go`); all six call `initializeReply()` |
| F15 | `Control` goroutine scaffolding in 11 tests | not applied | optional per the finding; one helper across the three shapes changes the channel structure of 11 CI-pinned cancel tests, whose goroutine/channel behaviour is frozen; conductor's call |
| F16 | overflow summary asserted through the code-under-test variable | applied | `rpc_session_test.go` asserts the literal `"Codex pending turn notification buffer overflow"` (the `adapter.go` text) |
| F17 | `TestFramingFailureSettlesWaitWithoutDiagnosticSink` asserted no diagnostic at all | applied | the sink is captured and the test now asserts no diagnostic event was recorded; the framing-failure path emits no sink event at all, so this is a negative pin on the absence of a diagnostic (no positive control) |
| F18 | owner-file scan used a hand-written field map | applied | field names derive from the parsed `cancelTracker` struct; an empty field set fails the test instead of scanning nothing |
| F19 | section 9 recorded `cancel_state_test.go` at 682 L | applied | the recorded number was wrong (697 L at `2571fd56`); post-apply sizes are in 17.1 |
| F20 | "no session" wording versus `TestCancelProcessExitSettlesDeferredTerminal` | rejected | the wording lives in the contract's clause 5, outside the owned set; a conductor wording ruling is requested |
| F21 | "each case with a recorded red mutant" versus seven group-level mutants | rejected | contract interpretation, outside the owned set; a conductor ruling is requested |

### 17.1 Post-apply measurements (single source — supersedes the counts in sections 9, 12 and 13)

`wc -l` after the apply (`daemon/internal/harness/codex/`):

| file | implement | apply |
| --- | --- | --- |
| `adapter.go` | 1,331 | 1,300 |
| `cancel_state.go` | 405 | 426 |
| `rpc_session.go` | 889 | 889 |
| `translate.go` | 167 | 179 |
| `adapter_test.go` | 1,296 | 1,283 |
| `cancel_test.go` | 493 | 472 |
| `rpc_session_test.go` | 848 | 834 |
| `adapter_support_test.go` | 609 | 624 |
| `cancel_state_test.go` | 697 (recorded as 682) | 704 |
| `translate_test.go` | 396 | 396 |

Every file stays under 1,500 L.

Literal multiset (`rg -o '"[^"]*"'`, import blocks excluded, the eight non-test `codex/*.go` files):
base 682 / 391 distinct → apply 674 / 391 distinct. The only differing keys are F6's single-sourcing:
`"turn-started:"` 2→1, `"item:"` 2→1, `"terminal:"` 2→1 and `":"` 13→8 (the five inline
concatenations removed). The error-string multiset (`errors.New` and `fmt.Errorf` first arguments) is
unchanged: 69 occurrences / 61 distinct, with `errors.New` 52 and `fmt.Errorf` 19 in both trees.

Declaration oracle (`go/parser` + `go/printer` canonicalised, per-name SHA-256 prefix; scratch `canon`
tool; "changed" = same name, different canonical text), non-test files: base `adapter.go` 159
declarations → apply 197; **9 missing** = the nine intentional `*Locked` helper removals, **26
changed**, **47 new** (`cancelTracker` and its 14 transitions, `rpcCorrelator`/`rpcResolution`, the
typed hook, the pure helpers, and the apply's `performCancelAttempt`, `markNotificationDeduplicated`,
`turnStartedDedupeKey`, `itemLifecycleDedupeKey`, `terminalDedupeKey`). Test declarations, base
`adapter_test.go` 137 → the apply's six test files 176: **1 missing** =
`(*blockingFakeNativeProcess).WriteInput` (F1), **25 changed** (the five recorded field-path edits plus
the apply's fixture and assertion substitutions), **40 new** (the 22 new tests, the tracker/translate
test helpers and the apply's nine shared test helpers).

Control-flow audit delta (section 13's rows re-derived with `canon func` and unified diff; this method
counts diff hunks, not the implement stage's token regions): `Control` was byte-identical to base and
now carries 1 hunk (the extracted sequence replaced by one call), `closeOnce` was 1 region and now
carries 2 hunks, `handleResponseWithOutputLock` now carries 3 hunks versus base (the merged settlement
block). Every other section 13 row is unchanged by the apply.

Lock-site table (section 10) re-derived (`locks.awk`: per-function `.Lock()`/`.RLock()` token sequence,
base `adapter.go` versus the apply's four source files): 58 functions at base, 58 at apply, all 58
sequences byte-equal; `performCancelAttempt` carries no lock site. Cancel-state write sequences
(section 11) are untouched by the apply — none of F4–F7 moves a cancel-field write — and the committed
scan still reports zero writes to the six tracker fields outside `cancel_state.go`.

Test names and skips (section 14) are unchanged by the apply: `go test -list '.*'` reports 138 names
with no duplicates, and all 84 `Test*` functions of base `adapter_test.go` are present.

Mutants re-checked on the applied tree, both reverted: M6 (a cancel-field write added at
`rpc_session.go:887`) turns `TestCancelFieldsWrittenOnlyInOwnerFile` red through the derived field set
("rpc_session.go:887 writes cancelTracker.deferredTerminal outside cancel_state.go"); M7
(`"turn-started:"` → `"turnStarted:"` in the new `turnStartedDedupeKey`) turns
`TestNotificationDedupeKey/turn_started` red. M1–M5 mutate declarations the apply does not touch.

### 17.2 Apply-stage gate results (working directory `daemon/`)

| gate | result |
| --- | --- |
| `gofmt -l internal/harness/codex` | empty (exit 0, 0 bytes of output) |
| `go vet ./...` | exit 0 |
| `GOOS=linux go build ./...` / `GOOS=windows go build ./...` | exit 0 / exit 0 |
| `go test -list '.*' ./internal/harness/codex/` | 138 names, 0 duplicates, 0 lost |
| `go test -count=1 ./internal/harness/codex/` | ok (16.132 s; 16.022 s inside the harness run) |
| `git diff --check` | exit 0 |
| `go test -count=1 ./internal/harness/...` (Windows) | exit 0, 9/9 packages ok (section 17.3) |
| WSL `-race` harness + app | exit 0, 11/11 packages ok (section 17.3) |
| repository gates (citation existence, record cleanliness) | exit 0 / exit 0, "nothing to check" (section 17.3) |

### 17.3 Apply-stage suite reruns

Windows `go test -count=1 ./internal/harness/...`: exit 0, 9/9 packages ok (`harness` 2.988 s,
`claude` 13.610 s, `codex` 16.022 s, `linejson` 1.094 s, `omo` 5.124 s, `omp` 5.795 s, `opencode`
0.594 s, `pi` 5.620 s, `probeconformance` 9.505 s). The diff adds no `t.Skip`.

WSL `go test -count=1 -race -timeout 30m -skip TestOutputBackpressureRecoversWithOneTruncationMarker
./internal/harness/... ./internal/app/...`: exit 0, 11/11 packages ok (`codex` 36.363 s, `app`
368.385 s, `app/apptest` 3.474 s; wall 428.53 s) — the same documented `-skip`/`-timeout` pair the
implement stage used to get past the repository's known slowest test under `-race` (that test is
untouched here). No verbose SKIP list was captured in this run; the diff adds no `t.Skip`.

Repository gates against a dangling scratch commit built from the applied working tree with a
temporary `GIT_INDEX_FILE` (`66db65e81050`, no branch, index or ref touched; `HEAD` stays
`2571fd56de44`): `python scripts/ci/check-citation-existence.py --base HEAD --head 66db65e81050` →
exit 0, "no audited document added or modified … nothing to check" (this stage changes only
`codex/**`, the plan and `docs/TODO.md`, none of them in the gate's audited corpus, unlike the
implement stage's `CLOSED-TODO.md` change); `python scripts/ci/check-record-cleanliness.py --base
HEAD --head 66db65e81050` → exit 0, "no record added or modified … nothing to check".
