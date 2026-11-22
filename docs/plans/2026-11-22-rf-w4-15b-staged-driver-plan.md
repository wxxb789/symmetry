# rf-w4-15b staged driver — plan

Contract: `goal-rf-w4-15b-staged-driver.md` (GDD state root, external to the repository), with the
2026-11-21 architecture reshape, the arch-decision-2 rulings and the 2026-11-22 comment policy
binding over the earlier text. Base revision: `adfe7d6ac75646f5ffb19ef33ca2c307e7ed3279`
(W2-13/#155 5a helpers, W4-15a/#222, W3-04/#187, W3-15, W4-15c/#236 and W4-01/#234 merged). Branch:
`refactor/rf-w4-15b-staged-driver`. Owned files are the contract's strict set plus this plan,
`docs/evidence/rf-w4-15b-staged-driver-2026-11-22.md`, `docs/TODO.md`/`docs/archive/CLOSED-TODO.md`.

Goal shape: one root `StagedDriver` owns the Start → persist → Open → StartTurn ordering and the
single-flight close that five adapters implement today, with **at most four hook methods on one
adapter-supplied interface**; each adapter's protocol state machine stays inside the adapter behind
those hooks; the app hands the harness one opaque launch grant so no conforming adapter names a
durable callback; every error string, persisted-write sequence, retry/terminal semantic and fixture
replay stays byte-identical; `opencode/**` is untouched.

## 1. Owned-file re-derivation at dispatch

The contract's file list predates two merged slices, so the owned set is re-derived here:

| Contract entry | HEAD reality | Action |
| --- | --- | --- |
| `claude_candidate_adapter.go` | moved to `harness/claude/adapter.go` by W4-15c (#236) | edit `harness/claude/adapter.go` |
| `claude_candidate_adapter_test.go` | split into `harness/claude/adapter_test.go` + `adapter_support_test.go` (+4) | edit the split files that pin the ordering |
| `app/goal_admission.go` StartRequest construction `:802-829` | W4-01 (#234) split it into `app/goal_admission_tx.go`; the construction is `:912-922`, the invocation is built at `:746` | edit the construction site in `goal_admission_tx.go` (the contract's "whichever lands second rebases and re-derives :802-829") |
| `app/linux_owner_witness_linux.go` `:3177-3185` | unchanged span | edit the `StartRequest` literal there |
| `harness/types.go` | unchanged | `StartRequest` + the new grant/description types |
| `generic.go` | unchanged | grant pass-through |

## 2. Public/API shape

New root file `daemon/internal/harness/staged_driver.go`:

```go
// LaunchGrant is one opaque durable launch authority. The app merges the
// invocation members and the three request-level persistence callbacks into it;
// adapters hand it back to the driver without reading it.
type LaunchGrant struct{ invocation execution.Invocation }

func NewLaunchGrant(invocation execution.Invocation) LaunchGrant

// ProcessDescription is everything an adapter may decide about the process.
type ProcessDescription struct {
    Program                string
    Args                   []string
    Dir                    string
    Env                    []string
    InitialInput           []byte
    CloseInputAfterInitial bool
}

// StagedHooks is the one adapter-supplied interface: exactly four methods.
type StagedHooks interface {
    LaunchTransport(ctx context.Context, invocation execution.Invocation) error
    OpenNativeSession(ctx context.Context) (NativeSessionHandle, error)
    AdmitNativeTurn(ctx context.Context, request TurnRequest) error
    CloseNativeSession(ctx context.Context) error
}

type StagedDriver struct { ... }            // unexported fields; constructed per session
func NewStagedDriver(hooks StagedHooks, mutex *sync.Mutex) *StagedDriver

func (driver *StagedDriver) Launch(ctx context.Context, request StartRequest, description ProcessDescription) error
func (driver *StagedDriver) Open(ctx context.Context) (NativeSessionHandle, error)
func (driver *StagedDriver) Admit(ctx context.Context, request TurnRequest) error
func (driver *StagedDriver) Close(ctx context.Context) error
func (driver *StagedDriver) CloseDetached(ctx context.Context, budget time.Duration, start func()) error
func (driver *StagedDriver) Opened() bool
```

Refusal sentinels the adapters map to their own frozen text (the guard lives in the driver, the
wording stays with the adapter):

```go
var ErrStagedSessionOpening  = errors.New("staged native session is already opening")
var ErrStagedSessionNotOpened = errors.New("staged native session has not opened its native handle")
```

`StartRequest` (in `types.go`) gains `Grant LaunchGrant`. The legacy members the frozen OpenCode
adapter still reads (`Invocation`, `PersistProcess`, `PersistProcessAuthority`,
`PersistContainmentStopReceipt`) stay, with a dated `docs/TODO.md` entry tying their removal to
W6-04 (contract Change 4 / R12). `StartRequest.grant()` returns `request.Grant` when set and
otherwise the legacy members merged nil-guarded, so direct-construction callers (the unowned root,
OpenCode and app test files) keep their exact behaviour; the five conforming adapters never read
the legacy members.

`ProcessDescription` replaces each adapter's `execution.Invocation` literal: the driver applies all
six members over the grant's invocation unconditionally, so every adapter supplies exactly the
expression its base literal used (a copy of `request.Invocation.Env` where the base copied it, the
grant's own environment where the base aliased it).

## 3. What the driver owns

| Phase | Driver step (the only path to it) | Adapter hook |
| --- | --- | --- |
| launch | `Launch`: merge grant + description, call `LaunchTransport` | start the transport, refuse a typed-nil owner, install it under this adapter's delivery lock, bind the provider bridge, drain pre-ready output, start the watcher |
| persist | inside the launch: the runner persists the process identity through the grant's callbacks before it exposes the process; no adapter callback construction remains | — |
| open | `Open`: single-flight latch; the handle is recorded **only after** `OpenNativeSession` returns success; an opened session returns the recorded handle | this adapter's native handshake (get_state / initialize+thread-start / protocol+state / latch) |
| admit | `Admit`: refuses any turn until the handle is recorded | this adapter's prompt write and turn latch/fence |
| close | `Close`/`CloseDetached`: one in-flight attempt through `CloseGate`; a bare `PermanentCloseFailure` is the only permanence interpreter | this adapter's bounded cleanup, returning the bare marker when the failure is sticky |

## 4. Per-adapter mapping (base → driver step + hook)

Base sequences are the scout-verified spans cited in the evidence record. "hook" = the adapter
method implementing one `StagedHooks` method; every hook keeps its base body minus the latch lines
the driver now owns.

### 4.1 launch (all five)

| Base | Driver / hook |
| --- | --- |
| `processContext, cancel := context.WithCancel(ctx)`; `newNativeSession(processContext, cancel, sink, ...)` | unchanged in the adapter; the constructor also calls `NewStagedDriver(session, &session.mutex)` |
| `request.Invocation` merge (24/24/22 literal lines in pi/codex/claude; copy+override in omo/omp) | `Launch(ctx, request, ProcessDescription{...})` → `request.grant().merged(description)`; the adapter passes the merged `execution.Invocation` to its launcher untouched |
| `request.StartProcess` preference (codex/omo/omp) | hook (adapter policy) |
| typed-nil owner (pi `errNativeProcessNil`, codex `failProcessOutputBeforeReady`, claude `errCandidateProcessNil`, omo `"OmO process launcher returned no process"`, omp `"OMP process launch returned no owner"`) | hook |
| install + `processReady`/`ready`/`launchReady` + pre-ready drain + `go watchProcess` | hook |

### 4.2 open

| Adapter | Base guard order | HEAD wrapper | HEAD hook |
| --- | --- | --- | --- |
| pi | `opened`→validator handle · `opening` · `usableLocked` · get_state · `usableLocked` · validator+resume | nil-session/nil-ctx text; map `ErrStagedSessionOpening` to `"pi native session is already opening"` | `usableLocked`, request id, `GetStateRequest`, call, re-check, validator state, resume validation; returns the handle (deletes `!opened`, `opening` lines) |
| codex | `ensureUsable(false)` · `opened`→threadID · `opening` · initialize/notify/thread-start · commit block | nil-session/nil-ctx text + `ensureUsable(false)` before the driver call (preserves base precedence) | handshake; commit block keeps `closed`/`sinkErr`/`terminalSeen` checks and the `threadID`/model fields (deletes `opened`, `opening`, `opening=false`, `cancel`-on-failure stays in the hook) |
| claude | `opened`→`{expectedSessionID}` · `usableLocked` | nil-session/nil-ctx text | `usableLocked` only |
| omo | `usableLocked` · `opened`→validator state · `opening` · protocol+get_state+validations | nil-ctx text + `usableLocked` before the driver call | protocol, get_state, `openError` classification, validations (deletes `opening` defer) |
| omp | `opened`→`session.handle` · `opening\|\|stopping\|\|failure` · ready wait · negotiate+get_state · `handle.ID` | nil-ctx text; map `ErrStagedSessionOpening` to `errors.Join(session.failure, "OMP session cannot open")` | ready wait, protocol calls, `handle` assignment (deletes `opened`, `opening` lines) |

### 4.3 admit

| Adapter | Driver gate | Hook (keeps its own machine) | Refusal mapping |
| --- | --- | --- | --- |
| pi | `opened` | usable, `turnStarted`, request id, emit, prompt RPC, `tryFinalizeTurn` | `usableLocked()` error, else `"pi native session has not opened"` |
| codex | `opened` | schema load, `turnStarted`/`turnStarting` + deferred rollback, replay branch | `"codex native session has not opened a thread"` (the `threadID == ""` check stays in the hook) |
| claude | `opened` | frame marshal, write fence, `turnStarted`, emit, write | `"Claude Code native adapter session has not opened"` |
| omo | `opened` | prompt build, `turnGate`, prepared/recovering machine | `"OmO native handle has not opened"` |
| omp | `opened` | `started`/`stopping`/`failure`, emit, prompt call | plain `"OMP session does not admit another prompt"` (the shared `errOMPPromptRefused`; only the `Open` refusal joins `session.failure`, §11) |

### 4.4 close

| Adapter | Driver call | Hook |
| --- | --- | --- |
| pi | `CloseDetached(ctx, cancelTimeout+processCleanupTimeout+processTerminationGrace, nil)` | base `closeOnce` unchanged (plain errors stay retryable) |
| codex | `Close(ctx)` | base `closeOnce` unchanged (plain errors stay retryable) |
| claude | `CloseDetached(ctx, cleanupTimeout+terminationGrace, session.requestClose)` | base `closeOnce` + the retryable switch: bare `PermanentCloseFailure` for the non-retryable arm |
| omo | `CloseDetached(ctx, cleanupTimeout, nil)` | base `closeOnce` unchanged |
| omp | `Close(ctx)` | `stopping = true` + bounded `stop` (base closure) |

## 5. Control-flow preservation (deviation list)

Everything else is preserved line-for-line. Deviations, each with its reason, all in unreachable or
effect-free paths:

1. **Repeat `Open` returns the driver's recorded handle** instead of re-reading the adapter's own
   source (`pi` validator state, `omo` validator state, `codex` `threadID`, `omp` `session.handle`,
   `claude` `expectedSessionID`). Values are identical: each is written once, at the success commit
   the driver now brackets. `pi`'s unreachable `ErrMissingSessionState` repeat arm and `omo`'s
   repeat-time `usableLocked` precedence are kept by the codex/omo wrapper pre-checks (codex, omo)
   and dropped only where the state cannot differ (`pi`).
2. **Guard evaluation moves from the adapter to the driver** for the open/admit latches; adapters
   map the two typed refusals to their frozen text, so every reachable error string is identical.
   Effects are unchanged: the driver sets no adapter state on a refusal.
3. **`StagedDriver.Launch` calls the hook after the merge**, so a launch never sees an invocation
   without the grant's durable members; the adapter's own validation order is untouched.
4. **`pi`/`codex`/`claude` pre-ready drain keeps its own lock discipline** (`outputMutex` +
   `mutex`) inside the launch hook; the driver does not take over the install, so the delivery race
   window is exactly the base one.
5. **`omo`'s deadline-composed cancel** stays in the adapter (it wraps `WithDeadline` around the
   process context before the session exists); the driver owns no context.
6. **`Admit` runs before prompt construction in `omo`/`codex`**, moving schema/prompt-load and
   `turnGate` acquisition behind the open gate. Both are effect-free reads that cannot fail for a
   valid turn; `omo`'s `turnGate` acquisition order is recorded here as the one real reorder.
7. **`Close` on an already-closed session** returns the gate's recorded outcome exactly as base
   (`CloseGate` semantics unchanged).

## 6. Edge-case semantics the contract demands

- **Ordering**: `Launch` → identity persisted by the runner inside the launch → `Open` records the
  handle only on hook success → `Admit` refuses until the handle exists. Root tests assert the
  sequence through fake hooks and a fake launcher that records the persist call.
- **Failure at each hook**: launch failure leaves the session unopened and the adapter's cleanup
  owner installed; open failure leaves `opened=false` (so `Admit` still refuses) and records no
  handle; admit failure surfaces the hook error and leaves the adapter's turn latch as the adapter
  left it; close failure stays retryable unless the hook returned the bare marker.
- **Cancellation before/after Open**: before Open the process context cancels through the adapter's
  own cancel (hook error path); after Open the close path is the only cleanup, and a joined close
  stays permanent only for a bare marker.
- **Close permanence**: through the exported `PermanentCloseFailure` constructor added by W4-15c;
  wrapped or joined markers stay retryable, `nil` Err stays retryable.
- **No public-interface change**: `Adapter`/`Session`/`StagedSession`/`StopProofSession` stay as
  they are; the new types are additive root helpers, exactly like the 5a helpers.
- **Opencode frozen**: `opencode/**` is not edited; the legacy `StartRequest` members stay populated
  for it, recorded in `docs/TODO.md` with the W6-04 tie.

## 7. Tests

`staged_driver_test.go` (root, fake hooks, no adapter package): the ordered phase trace; the launch
hook receives the merged invocation (grant durable members + description process members, and the
description wins); open is single-flight and idempotent; a failed open records no handle and keeps
`Admit` refused; `Admit` refuses before the handle and is released after; each hook error surfaces
unchanged; `Close`/`CloseDetached` join one attempt, a bare marker is permanent, a wrapped marker is
retryable. Each root test carries a recorded red mutant in the evidence record.

## 8. Evidence plan

Gates (foreground): `gofmt -l`, `go vet ./...`, `GOOS=windows go build ./...`, `GOOS=linux go
build ./...`, `GOOS=linux GOARCH=amd64 go vet ./internal/harness/...`, Windows `go test -count=1
./internal/harness/... ./internal/app/...`, WSL Ubuntu-24.04 `go test -count=1 -race
./internal/harness/... ./internal/app/...`, `git diff --check`, `git diff --name-only` (no
`opencode/**`), the hook-count grep, the callback-name grep, the per-adapter effective-callback
table base vs HEAD, the write-sequence and pinned-string tables, the per-adapter base-vs-HEAD
control-flow audit, the four contract mutants plus one mutant per hook, and the LOC delta
(report-only per D2).

## 9. Out of scope

The decoder leaf and its four files (W4-15a); the probe/registry layer (W3-04); the 5a helper
internals; `opencode/**`; `internal/platform/**`, `internal/execution/**`, `testdata/**`; the
app-side adapter/registry wiring beyond the two `StartRequest` sites; capture-fixture pruning
(W5-10); the codex adapter split (W5-16); A3/W4-12c/W4-13/W4-22/W6-04.

## 10. Ambiguities and decisions

- The contract's mutation (a) ("reorder persist-after-Open in the driver") is realised as
  "`Open` records the handle and marks the session opened before `OpenNativeSession` returns",
  the only reorder of that pair that exists in this design; its red test is the root ordering test
  plus the adapter suites' failed-open rows.
- The contract's mutation (c) is realised as disabling one adapter's hook stage (each hook stage
  gets its own mutant), since the hook stage is what the driver calls; the omp stopping latch is one
  of them.
- `StartRequest.StartProcess` stays a request field read by the launch hook: it is the owner
  witness seam, not a durable callback, and the witness path is unchanged.
- Recorded debt: the legacy `StartRequest` members and the exported `StagedDriver` accessors exist
  only because OpenCode stays frozen (W6-04) and the adapters live in other packages; both are
  recorded in `docs/TODO.md` rather than hidden behind a second interface.

## 11. As-built amendments (implement stage)

The tree is authoritative for these; the sections above record the design intent.

- Hook names as coded: `LaunchTransport`, `OpenNativeSession`, `AdmitNativeTurn`,
  `CloseNativeSession`; driver methods `Launch`, `Open`, `Admit`, `Close`, `CloseDetached`, with
  `OpenedLocked`/`OpeningLocked` as the only latch reads. The hook-count grep is in the evidence §2.
- `StagedDriver` is non-generic: no hook needs the adapter's process type, because each hook
  installs its own transport inside the adapter. `pi`/`codex`/`claude`/`omo`/`omp` keep their own
  `launchProcess` field so the packages' test seams (`adapter.startProcess`, `request.StartProcess`,
  `adapter.start`) keep working.
- Refusal mapping fixes found in review: omp's `StartTurn` refusal is the plain text (base's guard
  is one `||` chain), while its `Open` refusal joins `session.failure` (base joins there);
  claude's `Open` keeps the caller-`ctx.Err()` check in the wrapper and maps the concurrent-open
  refusal back to the recorded UUID (base had no opening latch); the `usableLocked`-first
  precedence of pi/claude/omo is reproduced in the wrapper mappings.
- The `codex` session lost its `opened`/`opening` fields entirely, which required deleting one
  inert line (`native.opened = true`) from `codex/adapter_test.go`; no assertion changed.
- Mutants grew to eleven (m1–m11) so every root test has a one-to-one red and every hook has one;
  the contract's four are m1, m2, m3 and m4 (the evidence §8 carries the exact edits and tails).
- One adapter test was added at implement (`omp/adapter_test.go`): the contract's mutant (c) names
  omp's stopping latch, and no base test pinned it. The apply stage added four
  `StartTurn`-before-open pins (`pi`, `codex`, `omo`, `omp`; evidence §7).
- LOC reality (report-only, D2): the adapters net +30 source lines (base→worktree; the implement
  revision was +29), the driver is +160 (implement +178) and the root test +381 (implement +380);
  the combined W4-15 target of −600…−800 is not met by either half, as the reshape's ≤4-hook
  constraint replaced absorption. Numbers in the evidence §11.
