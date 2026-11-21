# rf-w2-13 harness shared session scaffolding 5a — implementation plan

Contract: `goal-rf-w2-13-harness-scaffolding-5a.md` (GDD state root) plus its binding review
amendments: `refactor/w2-contract-review-opus-1.md` P1-2, P2-3, P2-4, P2-5 and the P3 note on
estimates, and `refactor/w2-contract-review-ds-1.md` A8 and A10. Where the two reviews differ,
the stricter rule is applied (noted per item below).

Baseline for every coordinate in this plan: `4dd374bdcdbe09a05287e77ad90faaa949fcddcd`
(`origin/main` after #143), the worktree HEAD. Branch:
`refactor/rf-w2-13-harness-scaffolding-5a`. Area: `daemon/internal/harness/**` only.

The increment adds shared session scaffolding to the root `harness` package and makes the native
adapters delegate to it. No adapter behaviour, error string, retry rule or public interface
changes; the proof is the adapter suites plus the new root-package helper tests.

## 1. What is extracted (the amended six-concern list)

The contract's Change line also names "one bounded JSONL decoder"; review P1-2 moves that to
W4-15, so the binding extraction list is exactly:

| # | Concern | Shared helper | Consumers |
| --- | --- | --- | --- |
| 1 | `ProcessDetails`/`LeaseRenewalAvailable`/`RenewLease`/`LinuxV2LiveOwnerStatus` forwarding | `ProcessForwarder[P]` | `pi`, `codex`, `omo`, `omp`, claude candidate |
| 2 | single-flight close state machine | `CloseGate` | `pi`, `codex`, `omo`, `omp`, claude candidate, root `genericSession` |
| 3 | `Wait`/`WaitTurn` terminal barriers | `WaitResult`, `WaitTurnBarrier` | `Wait`: `pi`, `codex`, `omo`, `omp`, claude candidate. `WaitTurn`: `pi`, claude candidate |
| 4 | emit sink wrapper | `DeliverEvent` | `pi`, `codex`, `omo`, `omp`, claude candidate |
| 5 | reflection nil-interface helper | `IsNilInterface` | `pi`, `codex`, `omo`, claude candidate, root `GenericAdapter` |
| 6 | task-result clone | `CloneTaskResult`, `CloneTaskResultPointer` | `pi`, `codex`, `omo`, `omp` (new isolation), claude candidate |

`opencode` is read-only (P2-3/D1): its six duplicate copies stay and are listed in §10. No file
under `daemon/internal/harness/opencode/**` is touched, and no `daemon/internal/app/**` file is
touched (P2-4).

Measured duplication at the baseline (scout-verified ranges, `4dd374b`):

| Concern | pi | codex | omo | omp | claude | opencode (frozen) | generic |
| --- | --- | --- | --- | --- | --- | --- | --- |
| forwarding | 592-651 (57 ln) | 388-429 (39) | 306-345 (40) | 178-203 (26) | 350-387 (36) | 543-603 (61) | — |
| close gate | 916-967 + type (54) | 806-847 + type (42) | 628-660 + type (33) | 827-857 + type (31) | 678-740 + type (63) | 1063-1099 + type (37) | 264-298 + type (35) |
| wait barrier | 862-912 (51) | 756-804 (49) | 584-626 (43) | 718-759 (42) | 614-673 (60) | 960-998 (36) | 245-262 (18) |
| emit | 1314-1330 (17) | 2158-2192 (34) | 973-989 (17) | 285-293 (9) | 1166-1182 (17) | 1344-1371 (28) | — |
| nil helper | 501-512 (12) | 273-284 (12) | 241-247 (7) | — (no helper) | 269-280 (12) | 421-432 (12) | 161-172 (12) |
| clone | 1509-1552 (43 incl. summarize) | 2285-2327 (43) | 1078-1119 (41) | — (none) | 1147-1336 (97) | — | — |

## 2. Public / API shape

New file per concern in `daemon/internal/harness/`; every exported symbol is documented.

`nil_value.go`

```go
// IsNilInterface reports whether value is a nil interface or holds a typed-nil
// pointer, map, slice, func, chan or interface.
func IsNilInterface(value any) bool
```

`process_forwarding.go`

```go
// ProcessIdentity is the narrow process-identity seam every staged session
// forwards. The daemon persists this value before it admits native work.
type ProcessIdentity interface{ ProcessDetails() (int, string) }

// ProcessForwarder forwards a staged session's optional process capabilities.
// It holds the session's process field and the mutex that guards it, so the
// session keeps a single process authority and its existing lock discipline.
type ProcessForwarder[P ProcessIdentity] struct{ ... }

func NewProcessForwarder[P ProcessIdentity](mutex *sync.Mutex, process *P) ProcessForwarder[P]
func (forwarder ProcessForwarder[P]) Details() (int, string)          // locks; also handles a nil session field
func (forwarder ProcessForwarder[P]) DetailsLocked() (int, string)    // caller already holds the session lock
func (forwarder ProcessForwarder[P]) LeaseRenewalAvailable() bool     // execution.LeaseRenewer assertion
func (forwarder ProcessForwarder[P]) RenewLease(deadline time.Duration, sequence uint64) error
func (forwarder ProcessForwarder[P]) LiveOwnerStatus(ctx context.Context) (execution.LinuxV2LiveOwnerStatus, error)
```

`close_gate.go`

```go
// PermanentCloseFailure marks a close attempt whose failure must stay sticky.
type PermanentCloseFailure struct{ Err error }

// CloseGate owns a session's "at most one in-flight shutdown" protocol.
type CloseGate struct{ ... }

func (gate *CloseGate) Run(ctx context.Context, attempt func(context.Context) error) error
// RunDetached runs attempt on its own goroutine under
// context.WithTimeout(context.WithoutCancel(ctx), budget); the initiating
// caller may return on ctx while bounded cleanup continues.
func (gate *CloseGate) RunDetached(ctx context.Context, budget time.Duration, attempt func(context.Context) error) error
```

`wait_barrier.go`

```go
// WaitResult waits for a terminal barrier and returns the value read produces.
// When ctx ends first it returns cancelled plus ctx.Err().
func WaitResult(ctx context.Context, terminal <-chan struct{}, read func() (TaskResult, error), cancelled TaskResult) (TaskResult, error)

// WaitTurnBarrier waits for a turn terminal barrier. A settled turn wins over an
// ended session context; otherwise sessionErr reports the session's own error.
func WaitTurnBarrier(ctx context.Context, sessionContext context.Context, turnDone <-chan struct{}, turnErr func() error, sessionErr func() error) error
```

`event_delivery.go`

```go
// DeliverEvent stamps a missing event time on its copy of the event and
// forwards it to the session's sink, leaving the caller's copy untouched. A
// sink failure is returned unwrapped: each adapter keeps its own "persist
// <adapter> native event: %w" literal at the call site so the wire and log
// text is provably unchanged.
func DeliverEvent(ctx context.Context, sink EventSink, event Event) error
```

`result_clone.go`

```go
// CloneTaskResult returns a TaskResult whose semantic payload shares no
// pointer, slice or map with the input, preserving nil-vs-empty slices.
func CloneTaskResult(result TaskResult) TaskResult
func CloneTaskResultPointer(result *TaskResult) *TaskResult
```

No existing exported symbol is renamed, removed or re-typed. `Adapter`, `Session`,
`StagedSession` (`types.go:254-285`) are untouched; every adapter constructor, option and
exported adapter type is untouched (P2-4), so `internal/app` needs no edit.

## 3. Exact edge-case semantics

**`IsNilInterface`.** True for a nil interface value and for a typed-nil pointer, map, slice,
func, chan or interface; false for a non-nil value of any kind, including a non-nil pointer to a
zero struct. Identical to the four copies being replaced (`pi:501`, `codex:273`, `claude:269`,
`generic:161`) and *broader* than `omo:241` and `opencode:421`, which only test
`reflect.Pointer`; the two frozen copies keep their current behaviour.

**`ProcessForwarder`.**
- `Details`/`DetailsLocked`: nil process field → `(0, "")`; typed-nil process → `(0, "")`; a
  non-nil process → its `ProcessDetails()`. `Details` holds the session lock for the call
  (`pi`, `omo`, claude, codex already do); `DetailsLocked` is for callers that already hold it
  (`codex.partialProcessResultLocked`).
- `LeaseRenewalAvailable`/`RenewLease`: the process is read under the session lock, the lock is
  released, then the assertion runs. The assertion is `execution.LeaseRenewer` (both methods).
  Evidence that this equals the per-method assertions used by `pi`/`codex`/`omp`: every type in
  the tree that implements `LeaseRenewalAvailable() bool` also implements
  `RenewLease(time.Duration, uint64) error` — `execution.Process` (`runner.go:1599`/`:1609`),
  `execution.linuxV2Containment` (`linux_owner_v2_linux.go:128`/`:138`),
  `platform.jobContainment` (`process_windows.go:172`/`:205`),
  `platform.linuxSupervisor` (`supervisor_linux_client.go:1481`/`:1410`),
  `platform.processContainmentSupervisorLease` (`supervisor_windows_client.go:1197`/`:1201`),
  and the test doubles `leaseRenewalProcess` (`app/lease_renewal_test.go:275`/`:277`),
  `deadlineContainment` (`execution/runner_lifecycle_test.go:970`/`:972`),
  `blockingContainmentSupervisor` (`platform/process_windows_test.go:1333`/`:1337`). `omo` already
  used `execution.LeaseRenewer`; the change is stricter for the other three and unobservable for
  every existing implementer.
- `LeaseRenewalAvailable` returns false when the session field is nil, when the field is typed-nil
  or when the process cannot renew; `RenewLease` returns `errors.ErrUnsupported` in those cases.
  Both keep `errors.ErrUnsupported` exactly as today.
- `LiveOwnerStatus` returns `errors.ErrUnsupported` when the process is nil, typed-nil or does not
  implement `LinuxV2LiveOwnerStatus`. The per-adapter `session == nil || ctx == nil` guards and
  their error strings stay in `pi` and (frozen) `opencode`; `omo`/`omp` keep their guard-free arms.

**`CloseGate`.**
- Order of decisions: a successful close wins (`nil`, attempt not re-run); then a sticky
  permanent failure (its stored error, attempt not re-run); then an in-flight attempt is joined
  (`attempt.done` vs the caller's `ctx`, returning `ctx.Err()` if the caller leaves first); only
  otherwise does the caller run the attempt.
- An attempt that returns nil marks the gate permanently closed. An attempt that returns a plain
  error stays retryable. An attempt that returns `PermanentCloseFailure{Err: e}` makes `e` sticky:
  every later `Close` returns `e` without re-running, and the initiating caller also gets `e`
  (the marker is unwrapped, so no caller ever observes the marker type and `errors.Is`/`Is`+`%w`
  behaviour is unchanged).
- `Run` executes the attempt on the calling goroutine with the caller's context (`codex`, `omp`,
  claude-candidate-free `generic`; `omp` derives its own `WithTimeout(ctx, shutdownTimeout)`
  inside the attempt, exactly as today).
- `RunDetached` derives `context.WithTimeout(context.WithoutCancel(ctx), budget)` and runs the
  attempt on its own goroutine, so a caller that leaves on `ctx.Done()` (or `context.Canceled`)
  cannot abandon bounded cleanup; the budget expression stays in each adapter
  (`pi`: `cancelTimeout+processCleanupTimeout+processTerminationGrace`;
  `omo`: `cleanupTimeout`; claude candidate:
  `claudeCandidateCleanupTimeout+claudeCandidateTerminationGrace`).
- Attempt bookkeeping (`done` channel, stored error, clearing the in-flight slot only when it is
  still the same attempt) is exactly the current code, moved.

**`WaitResult`.** `terminal` closed first → `read()` (the adapter reads its own state under its own
lock and clones where it clones today). `ctx` done first → `(cancelled, ctx.Err())`. `cancelled` is
the adapter's own value: `TaskResult{Kind: ResultCancelled, Summary: "<adapter> wait cancelled"}`
for `pi`/`codex`/claude candidate, the zero `TaskResult` for `omo`/`omp` (unchanged). `generic`'s
`Wait` is not a consumer: it closes the session on cancellation and joins a close error
(`generic.go:245-262`), which no other adapter does.

**`WaitTurnBarrier`.** Three arms in the same order as `pi:862` and `claude:614`: turn done →
`turnErr()`; caller `ctx` done → `ctx.Err()`; session context done → a non-blocking re-check of
`turnDone` (a settled turn wins) → `turnErr()`, else `sessionErr()`. `pi`/claude's
`opened`/`turnStarted`/nil-argument guards and strings stay in the adapters. `codex` (`observed`
fast path plus a nil-barrier error) and `omo`/`omp` (different session-context precedence) keep
their own barriers, because adopting this helper would change which error a turn that settles
concurrently with its session context reports — that is per-adapter terminal semantics, which
clause 3 forbids changing.

**`DeliverEvent`.** The event is taken by value: a zero `Event.At` is stamped with
`time.Now().UTC()` on that copy, a non-zero `At` is preserved, and the caller's local event is
never mutated (the five call sites read only `event.Sequence` after delivery); the stamped copy
goes by value to `sink.Handle(ctx, event)`; the sink error is returned unwrapped so the adapter
keeps its own `fmt.Errorf("persist <adapter> native event: %w", err)` literal byte-identical.
Serialization (`emitMutex`/`emitMu`), the failure bookkeeping
(`codex.sinkErr`, frozen `opencode.sinkErr`) and the `eventCount`/`lastSequence` counters stay in
each adapter: they are guarded by that adapter's own session mutex, and folding them into the
helper would create a second state authority.

**`CloneTaskResult`.** The union of the four existing local clones (`pi:1539`, `codex:2316`,
`omo:1078`, claude:1274): `Reason` pointer, `Semantic` pointer, `EvidenceRefs`, `Blocker`
(+`WorkItemIDs`), `ProposedNextAction` (+ its `Blocker`), `Proposal`, `Reason`-in-semantic, and
`Diagnostics` with a rebuilt private parsed-field map. Because the union is a superset, no adapter
loses isolation: `pi`/`codex` gain the `Blocker`/`Proposal`/`Severity` copies they did not make,
`omo` gains the `present`-map rebuild, claude keeps it. `Diagnostics` keeps `nil` as `nil` and a
non-nil empty slice as non-nil empty, so `json.Marshal` of a clone is byte-equal to the original
(the `pi:25`/`codex:1389` wire-validity tests and the claude:872 deep-copy test stay green). The
private parsed-field map is only written by `protocol.Diagnostic` decode (`protocol/goal_results.go:270`)
and read afterwards (`:254`), which is why a struct copy would be harmless — the union keeps the
existing rebuild anyway, and the plan records that alternative as the rejected cheaper option.
`omp`'s `Wait` (`omp:730`) gains the clone: today it returns `session.result` with the session's
`Semantic` pointer attached.

## 4. Design trade-off: the forwarder holds pointers, not the field

Options for forwarding: (a) move the process field into a helper that owns its own mutex;
(b) pass the current process value into stateless helpers; (c) keep the field and pass pointers to
the field and its mutex.

(b) cannot shorten `omp`'s one-line `ProcessDetails` and moves the lock into every adapter, so the
shared part shrinks to a call. (a) changes lock granularity for ~10 other reads per adapter
(`closeOnce`, `watchProcess`, `call`, `StartTurn`, `usableLocked`), which is where the process
reference is read together with other session state; that is not a mechanical extraction and
re-races state the suites do not cover. (c) is chosen: the session keeps one process authority and
one lock, every other field read is untouched, and only the forwarding methods shrink. The cost is
one extra `NewProcessForwarder(&session.mutex, &session.process)` wiring line per session and the
rule that the forwarder must be built after the session struct (documented on the constructor).

## 5. Baseline and expected delta

Non-test Go lines under `daemon/internal/harness/**` at `4dd374b`: 19,807 (test: 31,855).
The contract's −500 was measured for the whole finding, which included the JSONL decoder now
moved to W4-15, and review P3 makes LOC deltas reported, not gated. Measured on the final tree
with `git diff --numstat -- daemon/internal/harness` plus `wc -l` on the new files: 761 non-test
lines removed from the six adapter files, 159 added to them, and 420 lines of new shared helper
(docs included) = **−182 net non-test**. The apply stage added the typed-nil lease guard
(`process_forwarding.go`) and the by-value `DeliverEvent` note (+11 helper lines over the 409 the
simplify report recorded); these numbers are re-derived from the final tree. The helpers are
larger than the plan's estimate because every exported helper carries the repo's doc-comment
style and the close gate encodes four adapter policies (join, retry, permanent, detached) in one
place; the duplication they replace is 761 lines across six files.

## 6. Evidence plan (all foreground, on the final tree)

| Gate | Command |
| --- | --- |
| format | `gofmt -l daemon/internal/harness` (expect empty) |
| vet | `cd daemon && go vet ./...` (host windows/amd64) |
| builds | `GOOS=linux go build ./...`, `GOOS=windows go build ./...` |
| targeted suites | `cd daemon && go test -count=1 ./internal/harness/...` |
| helper pins | `go test -count=1 -run <new root-package tests> ./internal/harness/` |
| whitespace | `git diff --check` |
| no suppressions | `git diff -U0 origin/main...HEAD ':!docs/plans/2026-11-21-rf-w2-13-harness-scaffolding-5a-plan.md' \| grep -E '^\+.*(nolint\|credo:disable)'` (expect none; the record file itself carries the pattern text, so it is excluded) |
| no host path | `git diff origin/main...HEAD ':!docs/plans/2026-11-21-rf-w2-13-harness-scaffolding-5a-plan.md' \| grep -i "users/"` (expect none) |
| LOC | `git diff --numstat -- daemon/internal/harness` for the changed files plus `wc -l` on the six new helper files |
| string preservation (A10) | sorted multiset of double-quoted literals per changed file, `git show HEAD:<file>` vs the working tree: the only removals are the import paths `"reflect"`/`"slices"`/`"sync"` and the empty-string literals of the deleted nil-process branches; every error string keeps its exact text and count |

The adapter suites named in the contract as per-adapter oracles run inside
`./internal/harness/...`: `pi/adapter_test.go`, `codex/adapter_test.go`, `omo/adapter_test.go`,
`omp/adapter_test.go`, `claude_candidate_*_test.go`, the fixture replays
(`pi/rpc_test.go`, `codex/framing_test.go`, `omo/transcript_test.go`, `claude/stream_test.go`,
`opencode/sse_test.go`, `omp` decoder tests) and `probeconformance`. Native acceptance suites that
`t.Skip` without credentials are reported as not run, with the skip conditions named (P2-4).
`-race` is CI-only on this host (no cgo). No measurement or benchmark harness runs concurrently
with the suite.

## 7. A10: which of the moved error strings are already asserted

Verified by scanning every `*_test.go` under `daemon/internal/harness/**` plus
`daemon/internal/app/harness_test.go` and `pi_provider_bridge_test.go` (reviewer-scout run, quoted
counts) and by reading the shared helper strings:

| String produced by the shared code | Existing assertion | Compensating action |
| --- | --- | --- |
| `"persist <adapter> native event: %w"` | 0 hits for all five literals; `codex:1129` asserts the sink error text is contained | The literal does not move: it stays in each adapter's `emit` (§3). The helper's own output is pinned in the new root test file. |
| `errors.ErrUnsupported` from lease/live-owner forwarding | 0 hits | Unchanged value and identity; the helper test asserts it for a process without the optional methods, and the four moved arms keep the identical return. |
| close-gate `ctx.Err()` / sticky error | close tests already pin them hard: `harness_test.go:342`, `omo:514`/`:682`, `pi:637`/`:741`, `codex:2416`/`:2453`, `omp:851`, claude `:256`/`:813`/`:844`/`:1040`, `opencode:492`/`:520`/`:565`/`:783` | The gate implementation is the union of those behaviours; the new root test pins join, retry, sticky and detached-cleanup semantics. |
| `WaitResult`/`WaitTurnBarrier` outputs | `omo:188`/`:216`, `omp:245`/`:289`/`:294`, `pi:514`/`:528`, `claude:300`, `codex:1340`, `opencode:261` | Callbacks keep each adapter's own value and error; the new root test pins arm preference. |
| `CloneTaskResult` | `pi:25`, `codex:1389`/`:1406`, `claude:872` | Per-field isolation + wire-byte equality added in the new root test; the three adapter tests are re-pointed to the shared helper without changing their assertions. |

The per-adapter `session == nil`/`ctx == nil` and "wait cancelled" strings are not moved and are
not re-pinned here: the diff leaves those literals byte-identical, which the report shows by
grepping the diff.

## 8. Out of scope (with reasons)

- **Bounded JSONL decoder** (`pi/rpc.go:204-283`, `omo/rpc.go:100-160`, `claude/stream.go:88-152`,
  `codex/framing.go:59-118`): review P1-2 moved it to W4-15; the four decoders differ in CR
  handling, blank-line policy, failure policy and output type.
- **`opencode/**`**: read-only (P2-3/D1). Its duplicate forwarding/clone-less result path stays.
- **`summarizeProcessFailure` / claude's `summarizeClaudeCandidateProcessFailure`**: not in the
  binding extraction list; `pi:1509` and `codex:2294` are byte-identical but claude's variant
  (`claude:1147`) has a different fallback string, so unifying it is a behaviour decision, not a
  move.
- **`codex`/`omo`/`omp` `WaitTurn` barriers**: adopting `WaitTurnBarrier` would change which error
  a race between turn settlement and session-context end reports (§3).
- **`generic`'s `Wait`**: cancellation closes the session and joins the close error
  (`generic.go:245-262`); no other adapter does that.
- **The duplicate UUID validator in `pi/provider_bridge.go:976`**: review P1-1 names the W2-13
  owner as the follow-up owner for that copy (with `state/goal_session.go:1988`), but this
  slice's binding extraction list does not include it. It stays where it is and is recorded here
  as a later-slice follow-up together with its second copy.
- **probe/registry dedup** (W3-04), **the W1-06 deferred deletions** (W2-12), **staged driver**
  (W4-15), **opencode capability work** (W6-04): other slices.
- **`docs/evidence/**`, `docs/evaluations/**`, merged plans, `docs/archive/CLOSED-TODO.md`**: bound
  to the revisions they audited; not re-pointed.

## 9. Ambiguities resolved

1. **Owned test files.** Review P2-5 (opus) gives W2-13 everything under `internal/harness/**`
   except `registry.go` and the four probe files; review A8 (ds) says root `harness_test.go` and
   the probe tests are W3-04's. Applying the stricter reading, root `harness_test.go` is not
   edited; the new helper tests go into a new file `session_scaffolding_test.go`. Adapter test
   files are edited only to re-point the three clone-helper call sites (§7).
2. **`PermanentCloseFailure` vs a classifier callback.** A marker type keeps every `Run` call site
   one line and survives wrapping; the callback alternative adds a parameter that `pi`/`omo` would
   always pass as nil. The gate unwraps the marker before storing or returning it, so callers see
   exactly today's error values.
3. **Clone depth.** The union (§3) rather than the cheapest common denominator; the alternative
   (drop claude's per-diagnostic JSON round trip because the private map is immutable after
   decode) is recorded and rejected because it would weaken one adapter's isolation.
4. **The `"persist … native event"` label parameter.** Rejected: passing the adapter name into
   `DeliverEvent` would move five already-uncovered literals into a parameter, and clause 3 makes
   any changed error string a failure. Keeping the literal at the call site makes the
   string-preservation check a pure diff.
5. **Closing the increment.** Contracts, the run ledger and the answer artifact live in the GDD
   state tree (`<GDD-STATE>/contracts`, `ledger.tsv`) and are operator-owned; this repository
   closes an increment with the plan file, the source/tests, and the TODO note. No index for the
   contract file exists in this repository, and moving or editing the contract file is not a
   repository edit this slice may make.
6. **The W2-12 plan file.** A8 requires the partition text in both plan files. W2-12's plan file
   does not exist in this tree (W2-12 has not dispatched). This plan carries the text verbatim in
   §10 so the W2-12 author can copy it; this slice does not create another slice's plan file.

## 10. W2-12 / W2-13 harness ownership split (A8, verbatim text for both plans)

> W2-12 owns no file under `daemon/internal/harness/**` or `daemon/internal/app/**`. W2-13 owns
> `daemon/internal/harness/**` except `registry.go`, `codex_probe.go`, `claude_probe.go`,
> `codex_probe_test.go` and `claude_probe_test.go` (W3-04's). Within its own set, W2-13 edits
> `pi/**`, `codex/**`, `omo/**`, `omp/**`, `claude_candidate_*_test.go`, root `harness` helper and
> test files, and re-points three clone-helper call sites in `pi/adapter_test.go:30`,
> `codex/adapter_test.go:1394` and `claude_candidate_adapter_test.go:893`. Root `harness_test.go`
> and the probe tests are not edited by either slice this wave.

## 11. Files this slice writes

Source: `daemon/internal/harness/nil_value.go`, `process_forwarding.go`, `close_gate.go`,
`wait_barrier.go`, `event_delivery.go`, `result_clone.go`, `generic.go` (close gate + nil helper),
`pi/adapter.go`, `codex/adapter.go`, `omo/adapter.go`, `omp/adapter.go`,
`claude_candidate_adapter.go`.
Tests: `daemon/internal/harness/session_scaffolding_test.go` (new), `pi/adapter_test.go`,
`codex/adapter_test.go`, `claude_candidate_adapter_test.go` (call-site re-points only).
Docs: this plan. `docs/TODO.md` only if an entry closes.
