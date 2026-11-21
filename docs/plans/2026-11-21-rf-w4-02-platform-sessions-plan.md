# rf-w4-02 platform sessions plan — session objects for the supervisor/anchor loops (DP S-5)

Goal authority (external to this repository, not evidence): the GDD goal contract
`goal-rf-w4-02-platform-sessions.md` (2026-11-21) plus its conductor decisions (D1, D2, E8,
lesson-32 control-flow audit, program-wide path rules). Every code fact below is restated with an
in-repo coordinate; counts are `grep`/`rg` reproductions from this tree.

Baseline: slice base = `origin/main` = HEAD = `59da44d383b080159bbf670717748f17f732a1b7` (#196,
W3-02 response table, merged — its dispatch gate and this slice's HOLD condition). The contract's
pre-W3-02 measurement base `0fa9b705` is used only where the contract's own numbers came from it.
Branch `refactor/rf-w4-02-platform-sessions`. Area: `daemon/internal/platform` plus this plan.

## Dispatch gates re-derived at the slice head

- #160 (W2-11 typed sentinels) merged; #196 (W3-02 response table) merged as the slice base.
- W3-02's wire tests are on the slice base: `anchor_wire_shape_test.go` (582 L,
  `TestLinuxOwnerAnchorWireShapeRejectsWrongDescriptorCounts`,
  `...WireCloneCleanupDescriptorGate`, `...WireStoppingAnswerPrecedesShape`,
  `...WireExclusiveFieldsRefusedBeforeStoppingAnswer`, `...WireCodecRejectsMalformedFrames`) and
  `anchor_shape_table_test.go` (844 L, 12 `TestLinuxOwnerAnchorShapeTable*` plus the two writer
  tests). `anchor_wire_shape_test.go:291` drives the real `anchor.serveConnection` over a
  `unixpacket` socketpair, so `serveConnection`'s pre-dispatch shape/descriptor gates and the
  dispatch `switch` have an executable oracle. The 16 extracted handler bodies and
  `authorizeRunRequest` are not covered by it: the review's M2 mutant (the `authorizeRunRequest`
  gate removed from `handleRunOperation`) and M3 mutant (`Identity` routed to `handleRunOperation`)
  both survived `-run TestLinuxOwnerAnchor` (measured 2026-11-22), so their equivalence rests on
  the normalized text comparison (contract E8) and the control-flow audit below. **No HOLD.**
- `supervisor_linux_helper.go` is absent from #160's and #196's diffs; the owned loop regions
  re-derived below are the current ones.

## Owned set (re-derived at the slice base)

- `daemon/internal/platform/anchor_server.go` — `serveConnection` (`:1020-1514`, 495 L, 70 bare
  `return` statements; the only non-test caller is `anchor.serve`:314, which discards the detached
  result). The writer block (`writeResponse` :1707, `closeLinuxOwnerAnchorFDs` :1757) and
  `serveCloneCleanup` (:1569) are W3-02's landed code and are **not** re-edited; they keep the two
  remaining bare returns in this file.
- `daemon/internal/platform/supervisor_linux_helper.go` — `runLinuxSupervisorHelperLoop`
  (`:796-1106`, 311 L, 12 parameters), `runLinuxSupervisorRecoveryLoop` (`:1108-1228`, 121 L, 11
  parameters) and `RunContainmentSupervisor`'s call sites (`:703/:711/:718/:725` pre-hello recovery,
  `:734` helper loop) — wiring only, because the session constructor replaces those parameter
  lists.
- New same-package siblings with the exact source tags: `daemon/internal/platform/anchor_session.go`
  (`//go:build linux && amd64`, source `anchor_server.go`) and
  `daemon/internal/platform/supervisor_session.go` (`//go:build linux`, source
  `supervisor_linux_helper.go`). The helper-side file is pure-Linux because the two loops live in
  the `//go:build linux` file (`containment_supervisor_windows.go` has its own supervisor).
- This plan file.
- `docs/TODO.md`: **no W4-02 entry exists** (`rg -n 'W4-02|w4-02|serveConnection|naked|493'
  docs/TODO.md` = one unrelated hit), so no TODO bookkeeping is in scope.

Explicitly NOT owned (contract clause 4): `anchor_wire.go` (the request-shape table stays the
per-operation contract this slice preserves), `anchor_types.go`, `containment_supervisor_*.go`,
`supervisor_linux_client.go`, `helper_v2_*.go`, `linux_owner_anchor_linux.go`, all test files.

## Public / API shape

Two package-private session values, one per half; no exported surface changes.

```go
// anchor_session.go, //go:build linux && amd64
type anchorConnection struct {
    anchor      *linuxOwnerAnchor
    connection  *net.UnixConn
    peer        linuxOwnerAnchorPeer
    request     linuxOwnerAnchorRequest
    row         linuxOwnerAnchorOperationShape
    receivedFDs []int
    helperPeer  bool
}
```

- `serveConnection` keeps its signature `(connection *net.UnixConn) (detached bool)`: the named
  result is the detached-clone-cleanup signal, read by the same deferred close closure as today.
  It becomes the handshake preamble (deadline, peer, frame read, version/identity, shape lookup,
  exclusive-field and stopping gates, `refuses`) plus one 16-way dispatch switch. Every exit is an
  explicit `return false`/`return detached`; zero bare `return` statements.
- Handlers, one per operation, all in `anchor_session.go`:
  `handleIdentity`, `handleRetireRequest` (the cleared/aborted pair — the base already selects
  between `handleRetireClearedRun`/`handleRetireAbortedHelper` inside the shared block),
  `handleCloneCleanup` (returns the detached flag), and `handleRunOperation`, which owns the
  grant-gated group: the base's pre-switch grant/peer/overlap/deadline/membership/parent-pin block
  becomes `authorizeRunRequest() bool`, and the base's 12-case switch becomes a 12-way dispatch to
  `handleStart`, `handleAttach`, `handleLookup`, `handleRegisterProcess`, `handleClassifyChild`,
  `handleRegisterGate`, `handleAuthorizeGate`, `handleStopLeaf`, `handleWaitReap`, `handleReapRun`,
  `handleWaitHelper`, and the `retire-helper-v2` case to the existing anchor method
  `handleRetireHelper`.
- Every handler body is the base case body **textually unchanged** except the selector
  substitutions `anchor.` → `session.anchor.`, `connection` → `session.connection`, `request` →
  `session.request`, `row` → `session.row`, `receivedFDs` → `session.receivedFDs`, `helperPeer` →
  `session.helperPeer` (and `helperPeer :=` → `session.helperPeer =`). Comments move with their
  statements. This is the move-only proof's basis, not a style preference.

```go
// supervisor_session.go, //go:build linux
type linuxSupervisorSession struct {
    // constructor parameters (the helper loop's 12)
    launch, listener, target, group, ownerFile, controlFile, responseFile, statusFile,
    stdinFile, stdoutFile, stderrFile, ownerRecoveryMarker   // fields of those types
    // the helper loop's mutable locals (:805-817) and its closures' state
    bound, committed, resumed bool
    receipt *authority.StopReceipt
    boundSecret string
    ownerLostLatched bool
    ownerLossProofBarrier linuxSupervisorOwnerLossProofBarrier
    ownerLossProofBarrierEnabled, ownerLossProofBarrierStarted, ownerLossProofBarrierReleased bool
    ownerLossProofBarrierErr error
    ownerLossProofResultWritten bool
    ownerLossProofResultCategory string
    waitDone chan struct{}
    waitOnce sync.Once
    leaseState *linuxSupervisorLeaseState
    watchdog *linuxSupervisorOwnerWatchdog
    // the recovery loop's 11 parameters (the last four are what the pre-hello callers vary)
    ownerLost <-chan struct{}
    stop func() error
    allowAbort bool
    scanSealSequence func() uint64
}
```

- `newLinuxSupervisorSession(...)` takes the 12 parameters and returns the session with
  `stop = session.runStop`; `newLinuxSupervisorPreHelloRecoverySession(listener, launch, target,
  stop, ownerRecoveryMarker)` reproduces the four pre-hello call sites' argument tuple
  (`receipt=nil`, `boundSecret=""`, `ownerLost=nil`, `allowAbort=true`, `scanSealSequence=nil`,
  `category=""`).
- Methods replace the parameter lists: `runHelperLoop()`, `runRecoveryLoop()`, `runStop()`
  (assigned to the `stop` field, called as `session.stop()` exactly where the base called the
  closure), `startWait()`, `enterRecovery()`. `group.descendantScanSealSequence` and
  `watchdog.ownerLost()` are assigned into `scanSealSequence`/`ownerLost` at the same point the
  base evaluated them (`enterRecovery`, and the helper loop's `ownerLost` local).
- `newLinuxSupervisorSession` does no work beyond field assignment; the base loop's first three
  statements (`linuxSupervisorOwnerLossProofBarrierFromEnv` lookup with its error return,
  `newLinuxSupervisorLeaseState` + `defer leaseState.stop()`, `newLinuxSupervisorOwnerWatchdog` +
  `defer watchdog.disarm()`) stay as the first statements of `runHelperLoop`, so the error path and
  the defer (LIFO) order are unchanged.

### Parameter-count table (12/11 → session fields)

| Base | Base kind | Session member |
| --- | --- | --- |
| `launch` | helper param + recovery param | `launch` |
| `listener` | both | `listener` |
| `target` | both | `target` |
| `group` | helper param | `group` |
| `ownerFile, controlFile, responseFile, statusFile` | helper params | same names |
| `stdinFile, stdoutFile, stderrFile` | helper params | same names |
| `ownerRecoveryMarker` | both | `ownerRecoveryMarker` |
| `receipt **authority.StopReceipt` | recovery param | `receipt *authority.StopReceipt` |
| `boundSecret` | recovery param | `boundSecret` |
| `ownerLost` | recovery param | `ownerLost` |
| `stop func() error` | recovery param | `stop` |
| `allowAbort` | recovery param | `allowAbort` |
| `scanSealSequence` | recovery param | `scanSealSequence` |
| `ownerLossProofResultCategory` | recovery param | `ownerLossProofResultCategory` (field; frozen at loop entry, see the liveness table) |
| `bound, committed, resumed` | mutable locals | same names |
| `ownerLostLatched` | mutable local | same |
| `ownerLossProofBarrier{Started,Released,Err}, ownerLossProofBarrierEnabled` | mutable locals | `ownerLossProofBarrier`, `{Enabled,Started,Released}`, `Err` |
| `ownerLossProofResult{Written,Category}` | mutable locals | `ownerLossProofResultWritten`, `ownerLossProofResultCategory` |
| `waitDone, waitOnce` | mutable locals + `startWait` closure | same names + `startWait` method |

Parameter counts after the change: `runHelperLoop()` 0, `runRecoveryLoop()` 0; the 12-tuple now
appears once (the constructor) and the 11-tuple once (the two constructors together).

### Handover liveness (base value semantics; added with the F-A1 fix)

The move-only text comparison and the control-flow audit are both blind to a by-value parameter or
local turning into a live shared field, so every base value that crosses the handover is recorded
with its base passing mode and its head write/read sites. Read site = the recovery loop unless
noted; line numbers are `supervisor_session.go` lines at the current head (post-merge, after the
2026-11-22 comment compression).

| Base name | Base passing mode | Head writes | Freeze / equivalence argument |
| --- | --- | --- | --- |
| `launch`, `listener`, `target`, `ownerRecoveryMarker` | by value (struct / pointers) | constructor only | never reassigned; pointer identity kept |
| `receipt **authority.StopReceipt` | by value (pointer to the helper loop's `receipt` local) | `runStop` sets `session.receipt` (`:389`) | the base's `&receipt` aliased that same storage, so the field is the same alias, not a copy |
| `boundSecret` | by value (string) | helper-loop hello only (`session.boundSecret = request.Secret`, `:173`) | no write after handover |
| `ownerLost` | by value (channel) | `enterRecovery` (`watchdog.ownerLost()`, the base argument expression, `:398`) | the helper loop's `= nil` disarm (`:128`) is local to it and precedes the handover; no write after |
| `stop func() error` | by value (closure) | constructors | injection point preserved; never reassigned |
| `allowAbort` | by value (bool) | `enterRecovery` (`!committed && !resumed`, the base argument expression, `:399`); pre-hello `true` | no write after handover |
| `scanSealSequence` | by value (func) | `enterRecovery` (`group.descendantScanSealSequence`, the base argument expression, `:400`); pre-hello nil | no write after handover |
| `ownerLossProofResultCategory` | by value (string), frozen at the handover call (`supervisor_linux_helper.go:928` → read `:1157`) | `runStop` (write-once latch, `:364`; may run from inside the recovery loop) | `runRecoveryLoop`'s first statement copies the field into a local (`:405-408`) and the marker write reads the local (`:457`): a stop latched from inside the loop cannot change the reported category |

The helper loop's mutable locals (the `:805-817` block) were closure-captured in base — shared by
reference between the loop, `stop`, `startWait` and `enterRecovery` — so the fields preserve that
sharing one for one (`bound`, `committed`, `resumed`, `ownerLostLatched`, the
`ownerLossProofBarrier{Enabled,Started,Released,Err}` group, `ownerLossProofResultWritten`,
`waitDone`/`waitOnce`). `ownerLossProofResultCategory` is the one member of that block whose base
consumer across the handover took a copy, which is why only its read is frozen.

## Exact edge-case semantics

1. **Byte identity (D1).** No response field, JSON tag, status string, error string or log line
   changes. The anchor half is additionally pinned by W3-02's socketpair tests, which read the real
   frames `serveConnection` writes: frame bytes for the reachable statuses, the `anchor_stopping`
   frames with zero `SCM_RIGHTS`, the two-phase gate ordering, and the descriptor-count rejections.
2. **Validation order.** The handshake preamble keeps its exact statement order: socket deadline
   (`linuxOwnerAnchorRequestTimeout`) → `linuxOwnerAnchorPeerFromConn` (self-PID refusal) →
   `readLinuxOwnerAnchorRequest` (FDs closed on error) → `defer closeLinuxOwnerAnchorFDs(receivedFDs)`
   → version/client-PID/client-identity → `linuxOwnerAnchorOperationShapeFor` (`!known`) →
   `exclusiveFieldRejected` → stopping answer (`anchor.stopping && row.stoppingAnswer`) →
   `refuses`. Within the grant-gated group the order is grant validation → `helperPeer` computation →
   cgroup-overlap refusal → first `row.timeoutClass` deadline → manager membership → parent pin →
   operation dispatch. Identity keeps its own order (overlap → membership → parent pin → `ready`);
   retire keeps overlap → `row.timeoutClass` deadline → membership → parent pin → cleared/aborted
   delegation; clone-cleanup delegates immediately to `serveCloneCleanup`.
3. **FD ownership.** `closeLinuxOwnerAnchorFDs(receivedFDs)` stays a `defer` in the only function
   that owns the connection's lifetime, `serveConnection`, so received descriptors are closed after
   the handler ran, exactly as today; the read-error path still closes them inline. The session
   never closes them itself and never duplicates them.
4. **One connection = one session.** `anchorConnection` is constructed after the gates and never
   escapes the call; no field aliases state between connections. `receivedFDs` is the same slice
   the base passed to the handlers (`[*]int` from the frame read), not a copy.
5. **Detached semantics.** `handleCloneCleanup` returns `serveCloneCleanup`'s boolean; the named
   result and the deferred `connection.Close()` suppression behave exactly as before, including the
   clone-cleanup worker's use of the connection after `serveConnection` returned.
6. **Stop/recovery equivalence.** `session.receipt` replaces the base's `receipt` local plus the
   `&receipt` pointer indirection: `runStop` assigns it and `runRecoveryLoop` reads it; the four
   pre-hello sessions start with `receipt == nil`, which the loop treats exactly like the base's nil
   `**authority.StopReceipt`. `allowAbort` is computed in `enterRecovery` after `stop()` — the same
   expression, at the same point, as the base argument — and stays `true` for the pre-hello
   sessions. The base's `ownerLost = nil` select-disarm becomes `session.ownerLost = nil`.
   `ownerLossProofResultCategory` is the one recovery value the base passed by value into a loop:
   `runRecoveryLoop` copies the field into a loop-entry local (`:405-408`) and the owner-recovery
   marker write reads that local (`:457`), so a stop latched from inside the recovery loop cannot
   change the reported category.
7. **Failure control (clause 3).** No accept/reject set, timeout class, FD count, response field,
   status, error text or test name/assertion changes; a session field that is not equivalent to the
   local it replaces, or any bare return left in `serveConnection`, is a defect.

## Tests

No new test is required by the contract, and clause 3 makes changed test files a failure. The
existing oracles are exactly:

- `anchor_wire_shape_test.go` — real `serveConnection` over a `unixpacket` socketpair: descriptor
  rejection for the 16 operations, the `anchor_stopping` answer set with zero `SCM_RIGHTS`, the
  exclusive-field-before-stopping ordering, malformed-frame rejection, clone-cleanup descriptor
  gate.
- `anchor_shape_table_test.go` — the table this slice preserves (accept/reject per row, field sets,
  literals, timeout classes, writer goldens).
- `linux_owner_anchor_linux_test.go` (37 tests), `containment_supervisor_linux_test.go`,
  `process_unix_linux_test.go` — the supervisor and process oracles.
- Mutation check (contract clause 3 / owner rules): the control-flow audit below is a reading, and
  the extraction is proven move-only by mechanical text comparison of every moved body
  (whitespace-normalized, after the documented substitutions). No assertion is deleted or
  converted, so no red-mutation record is owed. Measured residual (review finding P3-4): the
  W3-02 wire tests stop at `serveConnection`'s pre-dispatch gates and the dispatch `switch`, and
  the review's M2 (`authorizeRunRequest` gate dropped) and M3 (`Identity` routed to
  `handleRunOperation`) mutants both survived `-run TestLinuxOwnerAnchor` (2026-11-22), so the 16
  extracted handler bodies and `authorizeRunRequest` rest on the move-only proof and the
  control-flow audit (contract E8).

## Evidence plan

All foreground, final tree, counts and exit codes recorded; the measurement harness (WSL suites) is
**never** run concurrently with another suite on this host.

| Gate | Command | Where |
| --- | --- | --- |
| format | `gofmt -l .` (daemon module; must print nothing) | host |
| vet | `go vet ./...` | host (windows/amd64) |
| build windows | `GOOS=windows go build ./...` | host |
| build linux | `GOOS=linux go build ./...`, `GOOS=linux GOARCH=amd64 go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...` | host |
| platform suite (Windows subset) | `go test -count=1 ./internal/platform/...` | host |
| platform suite (Linux, real) | `go test -count=1 -v ./internal/platform/` + the `--- SKIP` list | WSL `Ubuntu-24.04` user `accept`, `/mnt/q/repos/wt-w4-02-platform-sessions/symmetry/daemon` |
| focused wire oracle | `go test -count=1 ./internal/platform/ -run 'TestLinuxOwnerAnchor|TestLinuxSupervisor'` | WSL |
| callers' packages (lesson 32) | `go test -count=1 ./internal/execution/` and `go test -count=1 ./internal/app/...` | WSL (Linux) and host where it compiles |
| naked-return proof | `rg -c '^\s*return\s*(//.*)?$'` over `anchor_server.go` + the new session files, before/after (≥83 drop against the contract's `0fa9b705` base; against the slice base `59da44d3`: `anchor_server.go` 72 → 2 = 70, or 72 → 3 = 69 when the head count adds `supervisor_session.go`'s inherited survivor, `anchor_session.go` 0) | host |
| move-only proof | text comparison of every moved body, base vs head, after the documented substitutions | host (scratch, outside the tree) |
| control-flow audit | per-handler base→head rows (early return, fall-through, error short-circuit, default) | this file, filled at implementation |
| citation gate | `scripts/ci/check-citation-existence.py` at the slice head | host |
| whitespace | `git diff --check` | host |
| host-path leak | case-insensitive profile-directory grep over the tracked diff plus a scan of both new files and this plan (all clean) | host |

CI-only oracles (not gates): the daemon race job (slow oracle) and the dispatch-only
`linux-native-v2-acceptance`/witness jobs (zero dispatches; E8).

### Linux verification environment (deviation, measured)

On this host the WSL distro's mirrored-mode loopback path is broken for `127.0.0.1` only: an
in-process `connect(127.0.0.1)` to a fresh listener times out, `connect(127.0.0.53)` (the resolved
stub) succeeds, and the first full-suite run therefore failed four untouched tcp-peer tests with
`dial tcp4 127.0.0.1:NNNNN: connect: connection timed out` (135 s each) before the package hit the
10 m limit. In a fresh network namespace the same four tests pass in 11.7 s, and the full package
suite passes with zero failures, so the Linux suite commands above were run as
`sudo unshare -n` (root brings `lo` up in the new namespace) with the tests themselves still
executed as the `accept` user via `sudo -u accept -H` — the only environment delta is the network
namespace, and the uid/gid the tests observe is unchanged (a userns-only wrapper was rejected
because W3-02's wire harness refuses to run as uid 0). No product or test file is affected.

## Trade-offs and known limits

- **`serveConnection` keeps the handshake preamble.** A constructor returning
  `(session, ok)` would shorten `serveConnection` further but would have to move the
  `defer closeLinuxOwnerAnchorFDs` into a two-phase ownership handoff (the constructor must close on
  its own failure paths and register the defer on success). Keeping the preamble in place makes FD
  ownership single-function and the diff provably move-only; the function still drops 495 → 49
  lines (`:1020-1068` at the head).
- **`handleRunOperation` nesting.** The 12 grant-gated operations keep one shared gate and one
  dispatch point (the base's structure), instead of repeating `authorizeRunRequest()` as the first
  line of 12 handlers. Observable behaviour is identical because only one operation is handled per
  connection; the audit records the shared gate as one row plus one row per operation.
- **`stop` field vs method.** The base passes `stop func() error` as a recovery parameter, and the
  pre-hello call sites pass a different closure. Keeping it a field (`stop`, implemented for the
  helper session by the `runStop` method) preserves that injection point; a method-only design would
  need a second stop path.
- **`allowAbort` and `scanSealSequence` stay fields.** They are the contract's "four pure-plumbing
  params" category; computing them inside `runRecoveryLoop` from `committed/resumed/group` would
  change the pre-hello sessions' semantics (they must stay `true`/nil).
- **dLOC is report-only (D2).** Extracting handlers to a sibling file adds type/method boilerplate;
  the expected net delta is small and reported, never gated.
- **Unreachable-status coverage** stays as W3-02 recorded it (statuses needing a real A/B cgroup
  topology or a spawned helper are pinned at the writer level, not end to end).

## Out of scope

The response builder / request-shape table (W3-02, merged), typed error sentinels (W2-11, merged),
`helper_v2_*` handler decomposition, `containment_supervisor_*` witness/test scaffolding (W5-01/
W5-09), `process_unix.go` monitor state machine (W5-07), the anchor client
(`linux_owner_anchor_linux.go`), `supervisor_linux_client.go`, `anchor_wire.go`,
`anchor_types.go`, every test file, and any wire/JSON/error-string/log-line change (D1).

## Control-flow audit (executed)

Mechanism: the base slice of every path is put through the same documented transformation as the
head (selector substitutions, bare `return` → explicit `return false`, appended trailing return,
dropped `helperPeer := false` declaration, `if`-wrapper removal) and the ordered control-flow event
chain of both sides is compared; the chains below are the audit's rows. `guard` = `if` condition
(the rejecting gates), `return` = an exit, `loop`/`continue`/`break` = iteration control, `else` = an
`else`/`} else` branch, `case`/`switch` = dispatch. Base line numbers come from
`supervisor_linux_helper.go` / `anchor_server.go` at the slice base (transformed-base coordinates —
see below); head numbers are those of
`anchor_session.go`, `supervisor_session.go` or the new `anchor_server.go` `serveConnection` span,
as the row states, and are at the current head: the #185 merge inserted 25 lines into
`enterRecovery` and the 2026-11-22 comment compression removed them again, so post-:393
coordinates were re-derived by grep at both heads and match the pre-merge frame.

Coordinate provenance (added while applying review finding F5/PD-1): a **base** line number is a
*transformed-base* coordinate — the line of that event in the extracted base slice *after* the
documented transformation — so a line-removing step moves later events up (`authorizeRunRequest`'s
`guard@1098` is the dropped `helperPeer := false` line; the guard itself is `:1099`), and each run
case's appended trailing `return false` lands on the following `case` label's number
(`handleStart`'s `return@1188` is `case linuxOwnerAnchorOperationAttach:`); where the transformation
removes and appends nothing, base numbers equal the raw `supervisor_linux_helper.go` /
`anchor_server.go` lines at the slice base. **Head** numbers are raw lines of `anchor_session.go`,
`supervisor_session.go` or the new `anchor_server.go` span, except the hand-written
`handleCloneCleanup` row, whose `return@60` is the function's signature line (the `return` is `:61`).

### Naked-return proof

| revision | bare `return` lines in `anchor_server.go` | inside the `serveConnection` span | new session files |
| --- | --- | --- | --- |
| `0fa9b705` (contract's measurement base, pre-W3-02) | 91 | 83 | — (no session file) |
| slice base `59da44d3` (post-W3-02, post-#160) | 72 | 70 | — |
| worktree HEAD | 2 | **0** | `anchor_session.go` 0, `supervisor_session.go` 1 (a `return` in the void `requestEvents` reader closure, inherited verbatim) |

Drop over the contract's own base: 91 → 3 = **88 ≥ 83**. Drop over the slice base: `anchor_server.go`
72 → 2 = 70 (or 72 → 3 = 69 when the head count adds `supervisor_session.go`'s inherited survivor;
`anchor_session.go` 0), of which 70 → 0 inside `serveConnection` (W3-02 had already named 19 of the
91, 13 of them inside the `serveConnection` span). The two survivors in `anchor_server.go` are
`serveCloneCleanup` (:1230) and `writeResponse` (:1265) — both W3-02's landed functions, outside
this slice's owned region. All terminal `return`s in the moved paths are explicit `return false` /
`return detached`, and every handler result is named (`detached bool`).

### Base → head control-flow rows

#### anchor_server.go -> anchor_session.go

- **`serveConnection handshake preamble`** — events 17 -> 17, preserved: yes
  - base: guard@1022; guard@1026; return@1027; guard@1030; return@1031; guard@1034; return@1036; guard@1039; return@1040; guard@1043; return@1044; guard@1046; return@1047; guard@1049; return@1051; guard@1053; return@1054
  - head: guard@1022; guard@1026; return@1027; guard@1030; return@1031; guard@1034; return@1036; guard@1039; return@1040; guard@1043; return@1044; guard@1046; return@1047; guard@1049; return@1051; guard@1053; return@1054
  - note: all bare `return` -> `return false`; dispatch switch + `return detached` appended
- **`handleIdentity`** — events 7 -> 7, preserved: yes
  - base: guard@1057; return@1058; guard@1060; return@1061; guard@1063; return@1064; return@1067
  - head: guard@26; return@27; guard@29; return@30; guard@32; return@33; return@36
  - note: wrapper `if request.Operation == identity` replaced by the dispatch; trailing `return` -> `return false`
- **`handleRetireRequest`** — events 11 -> 11, preserved: yes
  - base: guard@1070; return@1071; guard@1073; return@1074; guard@1076; return@1077; guard@1079; return@1080; guard@1082; else@1084; return@1087
  - head: guard@40; return@41; guard@43; return@44; guard@46; return@47; guard@49; return@50; guard@52; else@54; return@57
  - note: wrapper replaced by the dispatch; cleared/aborted selection unchanged
- **`authorizeRunRequest`** — events 12 -> 12, preserved: yes
  - base: guard@1093; return@1096; guard@1098; guard@1103; return@1104; guard@1106; return@1107; guard@1109; return@1110; guard@1112; return@1113; return@1115
  - head: guard@65; return@68; guard@70; guard@75; return@76; guard@78; return@79; guard@81; return@82; guard@84; return@85; return@87
  - note: `helperPeer := false` declaration dropped (struct zero value); trailing `return true` added
- **`handleCloneCleanup`** — events 3 -> 1, preserved: yes (collapse)
  - base: guard@1089 (wrapper) ; other@1090 (`detached = anchor.serveCloneCleanup(...)`) ; return@1091
  - head: return@60
  - note: the dispatch replaces the wrapper guard; the assignment into the named result plus the bare `return` collapse into one `return session.anchor.serveCloneCleanup(...)` with identical arguments

`handleRunOperation` is the new dispatch surface: the base pre-switch gate is `authorizeRunRequest`, and every base case below is routed 1:1 (`retire-helper-v2` to the existing anchor method, no `default` clause).

- **`handleStart`** — events 20 -> 20, preserved: yes
  - base: guard@1118; return@1120; guard@1122; return@1124; guard@1126; guard@1127; return@1129; return@1134; guard@1137; guard@1147; guard@1150; guard@1152; return@1156; guard@1159; return@1163; guard@1166; return@1171; guard@1174; return@1180; return@1188
  - head: guard@124; return@126; guard@128; return@130; guard@132; guard@133; return@135; return@140; guard@143; guard@153; guard@156; guard@158; return@162; guard@165; return@169; guard@172; return@177; guard@180; return@186; return@194
  - note: case `linuxOwnerAnchorOperationStart`
- **`handleAttach`** — events 13 -> 13, preserved: yes
  - base: guard@1190; return@1192; guard@1194; return@1198; guard@1200; return@1204; guard@1207; return@1211; guard@1214; return@1219; guard@1222; return@1228; return@1236
  - head: guard@199; return@201; guard@203; return@207; guard@209; return@213; guard@216; return@220; guard@223; return@228; guard@231; return@237; return@245
  - note: case `linuxOwnerAnchorOperationAttach`
- **`handleLookup`** — events 17 -> 17, preserved: yes
  - base: guard@1238; return@1240; guard@1242; return@1246; guard@1249; guard@1250; guard@1254; return@1258; guard@1264; return@1268; guard@1271; return@1276; guard@1278; else@1283; guard@1285; return@1291; return@1300
  - head: guard@250; return@252; guard@254; return@258; guard@261; guard@262; guard@266; return@270; guard@276; return@280; guard@283; return@288; guard@290; else@295; guard@297; return@303; return@312
  - note: case `linuxOwnerAnchorOperationLookup`
- **`handleRegisterProcess`** — events 5 -> 5, preserved: yes
  - base: guard@1302; return@1304; guard@1306; return@1308; return@1311
  - head: guard@317; return@319; guard@321; return@323; return@326
  - note: case `linuxOwnerAnchorOperationRegisterProcess`
- **`handleClassifyChild`** — events 11 -> 11, preserved: yes
  - base: guard@1312; return@1313; guard@1316; return@1318; guard@1320; return@1326; guard@1329; return@1336; guard@1342; return@1349; return@1355
  - head: guard@330; return@331; guard@334; return@336; guard@338; return@344; guard@347; return@354; guard@360; return@367; return@373
  - note: case `linuxOwnerAnchorOperationClassifyChild`
- **`handleRegisterGate`** — events 10 -> 10, preserved: yes
  - base: guard@1357; return@1359; loop@1361; guard@1362; continue@1363; guard@1366; return@1368; guard@1371; return@1373; return@1376
  - head: guard@378; return@380; loop@382; guard@383; continue@384; guard@387; return@389; guard@392; return@394; return@397
  - note: case `linuxOwnerAnchorOperationRegisterGate`
- **`handleAuthorizeGate`** — events 5 -> 5, preserved: yes
  - base: guard@1378; return@1380; guard@1382; return@1384; return@1387
  - head: guard@402; return@404; guard@406; return@408; return@411
  - note: case `linuxOwnerAnchorOperationAuthorizeGate`
- **`handleStopLeaf`** — events 5 -> 5, preserved: yes
  - base: guard@1389; return@1391; guard@1396; return@1398; return@1403
  - head: guard@416; return@418; guard@423; return@425; return@430
  - note: case `linuxOwnerAnchorOperationStopLeaf`
- **`handleWaitReap`** — events 12 -> 12, preserved: yes
  - base: guard@1405; return@1407; guard@1410; return@1412; guard@1415; guard@1416; return@1419; guard@1422; return@1425; guard@1429; return@1431; return@1436
  - head: guard@435; return@437; guard@440; return@442; guard@445; guard@446; return@449; guard@452; return@455; guard@459; return@461; return@466
  - note: case `linuxOwnerAnchorOperationWaitReap`
- **`handleReapRun`** — events 18 -> 18, preserved: yes
  - base: guard@1438; return@1440; guard@1443; guard@1444; return@1447; guard@1450; return@1453; guard@1456; return@1459; guard@1463; return@1465; loop@1468; guard@1469; guard@1475; loop@1477; guard@1478; break@1480; return@1490
  - head: guard@471; return@473; guard@476; guard@477; return@480; guard@483; return@486; guard@489; return@492; guard@496; return@498; loop@501; guard@502; guard@508; loop@510; guard@511; break@513; return@523
  - note: case `linuxOwnerAnchorOperationReapRun`
- **`handleWaitHelper`** — events 6 -> 6, preserved: yes
  - base: guard@1492; return@1494; guard@1497; guard@1498; return@1501; return@1509
  - head: guard@528; return@530; guard@533; guard@534; return@537; return@545
  - note: case `linuxOwnerAnchorOperationWaitHelper`
- **`handleRunOperation retire delegate`** — events 0 -> 0, preserved: yes
  - base: -
  - head: -
  - note: case `retire-helper-v2` -> existing anchor method

#### supervisor_linux_helper.go -> supervisor_session.go

- **`runStop <- base `stop` closure`** — events 33 -> 33, preserved: yes
  - base: guard@835; return@836; guard@841; guard@845; guard@847; guard@848; return@849; guard@855; return@856; guard@862; return@863; guard@865; return@866; guard@869; guard@870; guard@875; return@876; guard@884; else@886; guard@889; guard@892; else@894; guard@898; return@899; guard@901; return@902; guard@904; return@908; guard@911; else@913; guard@916; return@917; return@921
  - head: guard@304; return@305; guard@310; guard@314; guard@316; guard@317; return@318; guard@324; return@325; guard@331; return@332; guard@334; return@335; guard@338; guard@339; guard@344; return@345; guard@353; else@355; guard@358; guard@361; else@363; guard@367; return@368; guard@370; return@371; guard@373; return@377; guard@380; else@382; guard@385; return@386; return@390
  - note: closure body moved verbatim; `group`/`target`/`launch`/`leaseState`/`watchdog`/receipt/flag locals -> session fields
- **`startWait <- base `startWait` closure`** — events 1 -> 1, preserved: yes
  - base: guard@824
  - head: guard@292
  - note: `waitOnce`/`waitDone`/status+stdio files -> session fields
- **`enterRecovery <- base `enterRecovery` closure`** — events 1 -> 1, preserved: yes
  - base: return@928
  - head: return@401
  - note: base `return runLinuxSupervisorRecoveryLoop(...)` replaced by 3 field assignments + `return session.runRecoveryLoop()` (same argument order, after `stop()`); the by-value `ownerLossProofResultCategory` argument stays a session-field read, frozen in `runRecoveryLoop`'s first statement (F-A1 fix). Post-#185 merge the head additionally carries the latch comment and unconditional `session.ownerLostLatched = true` (`:394-396`) in place of the base's conditional latch expression — still one return on each side
- **`runRecoveryLoop <- base recovery loop`** — events 44 -> 44, preserved: yes
  - base: loop@1111; guard@1112; guard@1114; guard@1118; guard@1122; guard@1124; continue@1126; return@1128; guard@1131; continue@1133; guard@1137; continue@1139; guard@1146; else@1148; guard@1150; else@1152; guard@1156; guard@1157; return@1159; guard@1164; else@1166; guard@1167; guard@1168; else@1170; else@1172; else@1175; else@1178; guard@1179; else@1181; else@1183; else@1186; else@1188; guard@1189
    else@1191; else@1193; else@1196; else@1198; guard@1203; guard@1216; continue@1218; guard@1221; return@1222; guard@1224; return@1225
  - head: loop@411; guard@412; guard@414; guard@418; guard@422; guard@424; continue@426; return@428; guard@431; continue@433; guard@437; continue@439; guard@446; else@448; guard@450; else@452; guard@456; guard@457; return@459; guard@464; else@466; guard@467; guard@468; else@470; else@472; else@475; else@478; guard@479; else@481; else@483; else@486; else@488; guard@489; else@491; else@493; else@496; else@498
    guard@503; guard@516; continue@518; guard@521; return@522; guard@524; return@525
  - note: `receipt == nil || *receipt == nil` -> `session.receipt == nil` (parameter indirection removed); `ownerLossProofResultCategory` is frozen into a loop-entry local (the loop's first statement), the base's by-value parameter semantics, so a stop latched from inside the loop cannot change the marker category; locals stay locals
- **`runHelperLoop`** — events 117 -> 82, preserved: yes (container reshape)
  - base: 117 control-flow lines in the base loop body (local declarations plus the three closures)
  - head: 82 control-flow lines in the method body
  - note: the base body minus the declaration lines that became session fields and the three closures that became methods; `leaseState`/`watchdog`/`waitDone` declarations became field assignments, `ownerLost :=` became a field assignment, and the `select` loop body is verbatim (normalized diff in the move-only proof)

### Move-only proof (executed)

A scratch verifier (outside this tree, `verify_sessions.py`) reconstructs every base slice, applies
the documented transformation, reverse-substitutes the head selectors and compares normalized text:
22 comparisons — `serveConnection` preamble, session literal, dispatch switch, `handleIdentity`,
`handleRetireRequest`, `handleCloneCleanup`, `authorizeRunRequest`, the 12 run cases, `runStop`,
`startWait`, `enterRecovery`, `runRecoveryLoop` — **PASS**; the two container reshapes
(`runHelperLoop`) are printed as a normalized diff whose only deltas are the removed declaration
lines, the three extracted closures and `:=` → `=` on the fields that became session state.

Base revision (added while applying review finding F4/C-2): both scripts read their base text from
an explicit revision — `python verify_sessions.py --base <rev>` / `python audit_rows.py --base <rev>`
(`BASE_REV` also works; default = the slice base `59da44d383b080159bbf670717748f17f732a1b7`) —
because the original `git show HEAD:` pin only held while the slice was uncommitted and exits 1 at
the committed head. Re-run at the committed head with that base, both print the revision, `RESULT:
PASS` (22/22) and `AUDIT RESULT: PASS` (22 rows) with output identical to the rows recorded above.
The scripts are scratch (`<HOST-PATH>/.local/state/gdd/tmp/w4-02/verify_sessions.py`, `…/audit_rows.py`),
outside this tree.

### Executed gates (final tree, results in the stage evidence record)

| gate | result |
| --- | --- |
| `gofmt -l .`, `go vet ./...` | clean, exit 0 |
| `GOOS=windows/linux/linux+amd64/linux+arm64 go build ./...` | exit 0 (all four) |
| Windows `go test -count=1 ./internal/platform/...` | `ok` 6.436s, exit 0 |
| Linux `go test -count=1 -v ./internal/platform/` (netns-isolated, see above) | 425 run / 0 fail / 1 pre-existing skip (`TestLinuxOwnerAnchorRunReaperRejectsForeignChildBeforeAdoptingIt`, "requires the test process to run as the installed A UID/GID"), `ok` 33.054s, exit 0 |
| Linux focused `-run 'TestLinuxOwnerAnchor\|TestLinuxSupervisor'` | 152 run / 151 pass / 1 pre-existing skip / 0 fail, `ok` 3.790s; all 17 W3-02 wire/shape tests PASS |
| Linux/Windows `./internal/execution/` | `ok` 1.548s (Linux) / 10.332s (Windows), exit 0 |
| Linux `./internal/app/...` | `ok` 239.067s, exit 0 |
| moves | the 22 move-only comparisons above PASS; literal multiset (go/parser `ast.BasicLit`, both literal kinds): base 508 occurrences (438 interpreted + 70 raw) / 304 distinct → head 513 / 304, zero distinct lost; head adds only the two new files' import paths (15 occurrences, 9 distinct) and folds 10 `""` arguments/declarations into session zero values |

### Advisory lint delta (not a gate — review finding F3/C-1)

The implement stage ran no advisory lint (its answer's "Not run" list), so the numbers below were
re-measured while applying the finding: `golangci-lint` **v2.15.0** (the `.github/workflows/ci.yml`
pin), `GOOS=linux`, `--config .golangci.yml --max-issues-per-linter=0 --max-same-issues=0`,
`./internal/platform/...` (the slice's package; the CI step lints the whole module) → slice base
`59da44d3` **234** issues, worktree head **236**: `unparam` 8 → 11 (the new `serveConnection` named
result plus the always-false `handleIdentity`/`handleRetireRequest` results), `ineffassign` 6 → 5,
and `gocyclo` 70 / `gocritic` 17 / `errorlint` 58 / `funlen` 4 / `staticcheck` 29 unchanged, with
finding attribution moved to `supervisor_session.go`.

Boundary: do **not** silence the two handler `unparam` findings by dropping their named results —
that adds 9 bare returns and moves the union bare-return drop from 88 to 79, below clause 1's ≥83.

### Apply-stage amendment record (2026-11-21, simplify → apply)

Accepted simplify findings applied in this file: F3/C-1 (lint delta above), F4/C-2 (base revision
named in the move-only proof), F5/PD-1 (coordinate provenance in the audit header), F6/PD-2
(`handleRetireRequest` / `handleRetireHelper`), F7/PD-3 (counts re-derived at the head, never
arithmetically: import-path literals 15 occurrences / 9 distinct, the slice-base bare-return drops
scoped, **33** `linuxSupervisorSession` fields, **22** audit rows). Not applied: F1 (retained — the
two tautological field assignments at `supervisor_session.go:101-102` are the documented reshape
artifact; changing them is count-refreshing for one line), F2 (declined — the `detached == true`
clone-cleanup path has no reachable oracle; inherited gap, disclosed here and under E8). The
implement-stage run answer keeps its superseded figures ("12 import paths", "35 fields", "21 rows
preserved"); this file is the slice's in-repo record of the corrected values.

Gate re-run at the apply head (the tree's code and tests are byte-identical to the implement head;
this file is the diff): all green — host `gofmt`/`go vet` and all four `GOOS` builds exit 0, Windows
platform `ok` 6.833s, execution `ok` 11.026s, app `ok` 153.959s; Linux (netns) platform `ok` 33.502s
with 425 run / 0 fail / 1 pre-existing skip, focused `152 run / 151 pass / 1 skip` with all 17
W3-02 wire/shape tests PASS, execution `ok` 1.660s, app `ok` 186.582s. One host-load artifact: the
first Windows `./internal/app/...` run (which overlapped the advisory lint) failed the 2 s-deadline
test `TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline` on a
loopback HTTP `context deadline exceeded`; that class is the one
`docs/plans/2026-11-21-rf-w2-10-callback-dedup-plan.md:168-173` and
`docs/evidence/goal-0009-achieve-contract-amendments-2026-11-15.md:202` record — the focused test
passes idle (22.14s, 4/4 subtests) and the idle full run exits 0.

### Parameter counts (summary)

See the parameter-count table in the design section above: 12 constructor parameters → 12 fields; the
loop's 15 mutable locals/closures → fields/methods; recovery's 11 parameters → 11 fields. `runHelperLoop()`
and `runRecoveryLoop()` take 0 parameters.

### Fix-stage record (2026-11-22, review findings F-A1/F-A2/F-A3)

- **F-A1 (applied, code).** `ownerLossProofResultCategory` was read live from the session field at
  the owner-recovery marker write, but the base passed it **by value** into
  `runLinuxSupervisorRecoveryLoop` (`supervisor_linux_helper.go:928` → read `:1157`): a stop
  latched from inside the recovery loop could change the category the marker reported, which the
  base could not. `runRecoveryLoop` now copies the field into a loop-entry local
  (`supervisor_session.go:405-408`) and the marker write reads that local (`:457`) — the base's
  handover-time freeze, restored without adding session state or changing the parameter counts
  (clause 1). The move-only script models the copy as one documented stripped statement and
  re-runs `RESULT: PASS` (22/22); the regenerated audit row shows the same 44-event chain with the
  post-fix head coordinates (`audit_rows.py`, `AUDIT RESULT: PASS`).
- **F-A2 (applied, method).** The "Handover liveness" table above records every base
  parameter/local that became a field with its base passing mode and head write/read sites, so the
  by-value → live-field class that the move-only proof and the control-flow audit are both blind to
  is now checked explicitly.
- **F-A3 (judged invalid at `:457`; applied at `:260`).** The review's "508 double-quoted + 70
  backtick = 578 occurrences" double-counts the raw literals: the go/parser `ast.BasicLit` count
  over the slice base's two files is 438 interpreted + 70 raw = **508 total** (the label's own
  number; independently reproduced: the files hold 1016 `"` characters — 876 delimiting the 438
  interpreted literals plus 140 inside raw literals) and 513 at the head (= 508 + 15 import
  occurrences − 10 `""`), 304 distinct values on both sides. The suggested "508 double-quoted"
  relabel would have made the record wrong, so the row keeps 508 and now states the method and both
  totals; the load-bearing deltas (15 import occurrences / 9 distinct, ten `""` folded, zero
  distinct lost) reproduce. At `:260`, "495 → ~75 lines" was wrong and is now "495 → 49"
  (`serveConnection` `:1020-1068` at the head).

Gate re-run at the fix tree (foreground, `daemon` module unless noted): host `gofmt -l .` and
`go vet ./...` exit 0, all four `GOOS` builds (`windows`, `linux`, `linux/amd64`, `linux/arm64`)
exit 0, Windows platform `ok` 6.338s, Windows execution `ok` 8.784s, Windows app `ok` 119.032s;
Linux (netns, `sudo unshare -n` + `sudo -u accept -H`) platform `ok` 31.882s with **425**
`--- PASS` / 0 `--- FAIL` / 1 pre-existing `--- SKIP`
(`TestLinuxOwnerAnchorRunReaperRejectsForeignChildBeforeAdoptingIt`), focused `-v`
`-run 'TestLinuxOwnerAnchor|TestLinuxSupervisor'` `ok` 3.952s with 151 pass / 0 fail / 1 skip and
**17/17** W3-02 wire/shape tests PASS, execution `ok` 1.486s, app `ok` 141.646s. Naked-return count
re-measured: `anchor_server.go` 2 (both outside `serveConnection`, whose span is 0),
`supervisor_session.go` 1 (inherited closure `return`), `anchor_session.go` 0 — the contract base
drop stays 91 → 3 = 88 ≥ 83. Advisory `golangci-lint` v2.15.0 `GOOS=linux`
`./internal/platform/...`: **236** issues, unchanged from the apply head (no new finding, no new
suppression). `python scripts/ci/check-citation-existence.py --base origin/main --head HEAD` exit 0
(1 changed document, 465 path citations — the script reads committed revisions, so the fix-stage
edits are not yet visible to it; the added citations are in-file line references, each verified by
grep). `git diff --check` clean; no host path and no backslash-`u` in the range or in the
uncommitted diff.

### Post-merge record (2026-11-22, merge `3c711bce`)

The slice merged `origin/main` `f70553bf` (which carries #185 `4b034f50`) into `e07b575f` as
`3c711bce`. Of the owned files only `supervisor_session.go` changed, and only in one hunk:
`git diff e07b575f 3c711bce -- daemon/internal/platform/supervisor_session.go` is the single
`@@ -393,7 +393,32 @@`, the #185 port into `(*linuxSupervisorSession).enterRecovery`.

- **The port.** #185's comment and unconditional `session.ownerLostLatched = true` (`:394-396`)
  replace the pre-#185 conditional latch
  `session.ownerLostLatched = session.ownerLostLatched || channelClosedV2(session.watchdog.ownerLost())`;
  `_ = session.stop()`, the three field assignments and `return session.runRecoveryLoop()` are
  unchanged. All 9 `enterRecovery` call sites route through the method, and the owner-lost select
  arm keeps its own latch before the call (`:128-130`).
- **Body identity.** At the merge, `enterRecovery` (then `:394-426`) equaled `origin/main`'s closure
  `supervisor_linux_helper.go:924-952` after stripping `session.` (comment, latch, `stop()`, the
  three argument assignments and the recovery call), so no other #185 behavior was lost; only the
  2026-11-22 comment compression has touched it since. The delta is the upstream fix itself, not a
  move artifact; the 22/22 move-only comparisons and the audit chains above were established at the
  pre-merge head and cover every other moved body, whose bytes the merge did not touch.
- **Red/green.** Both #185 tests — `TestLinuxSupervisorControlEOFWithDeadOwnerMetadataUsesOwnerLossSeal`
  and `TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed` — pass at the merge head
  (`go test -count=3 -run '<the two names>'` → `ok`), and both fail with the latch reverted via
  `-overlay` to the pre-#185 expression (M1), so the port is load-bearing.
- **WSL platform suite.** Green as non-root uid/gid 1001 (`setpriv --reuid=1001 --regid=1001
  --clear-groups`):
  `go test -count=1 -timeout 1800s -v -skip '<the six loopback TCP-peer tests, regex from the
  #185 plan :238-239>' ./internal/platform/` → exit 0, 229 top-level `--- PASS`, 0 FAIL, the
  pre-existing `TestLinuxOwnerAnchorRunReaperRejectsForeignChildBeforeAdoptingIt` skip the only
  skip. The six name-skipped tests are the documented WSL mirrored-loopback limitation, not slice
  regressions.
- **Coordinates.** The #185 merge inserted 25 lines into `enterRecovery` at `:393`, pushing every
  later citation +25; the 2026-11-22 comment compression removed exactly those 25 lines again, so
  the citations in this plan were re-derived by grep a second time and match the pre-merge frame
  (handover reads `:398`/`:399`/`:400`, loop-entry category copy `:405-408`, marker read `:457`,
  audit heads `return@401`, `loop@411`…`return@525`). `origin/main` has since moved to `9a01686d`
  (#179, #181), neither touching `daemon/internal/platform/`, so the PR range (merge-base
  `f70553bf`) is unchanged.
- Comment compressed to two why-lines per the 2026-11-22 owner comment policy; the test name that
  pins the live-owner case remains `TestLinuxSupervisorLiveOwnerControlCloseInProcessStopFailsClosed`.
