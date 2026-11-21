# rf-w3-05 notification reconnect-error surfacing + fake-agent reader exit — implementation plan

Contract: `<HOST-PATH>/.local/state/gdd/contracts/goal-rf-w3-05-notification-fake-agent.md` (GDD,
2026-11-21) with its binding conductor decision (2026-11-21: D5 no `app.go` edit, DS A5, Opus
P1-1/P2-1/P3, D6 dispatch now) and the two review files it reconciles
(`refactor/w3-contract-review-ds-1.md`: GO/A5/N4; `refactor/w3-contract-review-opus-1.md`:
GO-WITH-AMENDMENTS/P0-1/P1-1/P2-1/P3). Program source: `refactor/program-plan.md` §6 W3-05;
finding `refactor/findings/daemon-protocol-rest.md` F-23:546-554.

Baseline for every coordinate below:
`36f9a887635ec89d49cce2cd13b3915a8a875a83` (`origin/main` after #155) = the worktree HEAD, clean
tree. The contract's coordinates were re-derived at `origin/main c87c9918`/`a1142d4e`; after #155
the notification references are unchanged (`client.go:150-178` `Run`, `:180-285` `runConnection`,
`client_test.go:429-460` `testOptions`/`newTestClient`) and the fake-agent call sites sit at
`main.go:48` (`bufio.NewReader`) and `main.go:68` (`runAutonomous(...)`). Branch:
`refactor/rf-w3-05-notification-fake-agent`. Area: `daemon/internal/notification/**` and
`daemon/cmd/symmetry-fake-agent/**` only — `daemon/internal/app/app.go` is deliberately outside the
owned set (D5, §8).

The increment makes two silent failures observable without changing either side's public
behaviour: `notification.Client.Run` discards every retried connection error (its caller's
`notification_client_stopped` warn is reachable only on cancellation), and the fake-agent
autonomous mode's input reader goroutine parks in `readLine` when the run returns, so it can never
observe the run's end.

## 1. Deliverable shape

| # | Surface | Change | File |
| --- | --- | --- | --- |
| 1 | notification option seam | `Option`, `WithConnectionErrorHandler(func(error)) Option`, `New(..., options ...Option)`, new `clientOptions.connectionErrorHandler` field | `daemon/internal/notification/client.go` |
| 2 | notification reporting | `Run` hands every dial error and every `runConnection` error to `reportConnectionError`, which reports only when `err != nil && ctx.Err() == nil` and the handler is set | same file |
| 3 | notification tests | `testOptions.connectionErrorHandler` passthrough, the three new tests (§5.1, §5.2, §5.5), a quiet frame reader for the server-side heartbeat loop | `daemon/internal/notification/client_test.go` |
| 4 | fake-agent reader exit | `runAutonomous(reader *bufio.Reader, input io.Reader, output io.Writer, envelope)` closes `done`, closes `input` when it is an `io.Closer`, and joins the reader goroutine through `readerDone`, bounded by `readerJoinTimeout` | `daemon/cmd/symmetry-fake-agent/autonomous.go` |
| 5 | fake-agent call site | the one `runAutonomous` call passes the input stream | `daemon/cmd/symmetry-fake-agent/main.go:68` |
| 6 | fake-agent test | per-test goleak helper with its recording-TB self-test, the io.Pipe exit test, and the bounded-join test (§5.5) | `daemon/cmd/symmetry-fake-agent/autonomous_test.go` |
| 7 | this plan | — | `docs/plans/2026-11-21-rf-w3-05-notification-fake-agent-plan.md` |
| 8 | docs reconciliation | re-point the one line citation this change shifts (`docs/TODO.md:539`, `client_test.go:283` → `:285`) | `docs/TODO.md` |

No production file outside those two packages is edited. `docs/TODO.md` gains no entry and closes
none: its single edited line is the coordinate re-point in row 8, which is caused by this diff's
own two import lines and is traceable to it (the contract's owned set admits `docs/TODO.md`; the
repo precedent for such a re-point is the W1-04 platform split's note in the same file). The
deferred wiring of §8 is recorded here, in the slice's own record, as the contract requires.
Test-only additions: `go.uber.org/goleak` is already a direct test-only require of `daemon/go.mod`
(W1-08), so no manifest changes.

## 2. Public / API shape

```go
// Option adjusts a client's connection policy. Only this package can construct
// one, so the option set stays known.
type Option func(*clientOptions)

// WithConnectionErrorHandler reports connection failures that Run retries: a
// failed dial or a dropped connection. Run's terminal error is the caller's
// context error and is never reported. The handler runs on Run's goroutine
// before the retry backoff, so it must not block the connection loop.
func WithConnectionErrorHandler(handler func(error)) Option

// New creates a notification client. ... Existing callers compile unchanged:
// the option list is variadic.
func New(baseURL, websocketPath, machineID, machineToken string, httpClient *http.Client, options ...Option) (*Client, error)
```

`clientOptions` gains `connectionErrorHandler func(error)`; `defaultClientOptions` leaves it nil;
`clientOptions.validate` does not constrain it (an unset handler is the pre-existing behaviour:
retried connection failures stay unobservable). `Option` is sealed by its unexported parameter type
— no external package can build one, which is what keeps the option set reviewable.

```go
// Fake-agent side: the reader keeps sharing the caller's buffered reader and
// now also receives the stream behind it.
func runAutonomous(reader *bufio.Reader, input io.Reader, output io.Writer, envelope protocol.AgentInputRecord) error
```

`runAutonomous` is package-private and has exactly one caller (`main.go:68`), which passes the same
`io.Reader` it wrapped in `bufio.NewReader` at `main.go:48`. Passing both is deliberate: the
buffered reader holds the initial input record's read-ahead bytes (a second `bufio.Reader` over the
same stream would lose them), and only the stream itself is closable.

## 3. Exact edge-case semantics

### 3.1 When the notification handler fires

The handler is invoked at exactly one place in `Run`, immediately after the existing
`if ctx.Err() != nil { return ctx.Err() }` early return and before the `connected` backoff reset:

```go
		if ctx.Err() != nil {
			return ctx.Err()
		}
		client.reportConnectionError(ctx, err)
```

`reportConnectionError` reports iff `err != nil && ctx.Err() == nil &&
client.options.connectionErrorHandler != nil`. Consequences, clause by clause:

- **Failed dial.** `websocket.Dial` errors (rejected handshake, refused connection, dial deadline
  expiring while the caller's context is live) reach the handler with the dial's own error.
- **Dropped connection.** `runConnection` errors reach the handler: the read error of a closed or
  reset connection, `Phoenix channel join was rejected`, `Phoenix channel phx_error`/`phx_close`,
  `Phoenix heartbeat reply timed out`, `Phoenix heartbeat was rejected`, and a frame-write failure.
- **Cancellation.** Every terminal return is `ctx.Err()` and none of them reports: the two
  `return ctx.Err()` arms, `waitForBackoff` returning false (`return ctx.Err()`), and the loop-top
  `if err := ctx.Err(); err != nil { return err }`. A `runConnection` that returns `ctx.Err()`
  because the caller cancelled is filtered by `ctx.Err() == nil` even if the call is moved earlier.
- **A live dial deadline is not cancellation.** `TestRunDialUsesDeadline`'s dial deadline is derived
  with `context.WithTimeout(ctx, dialTimeout)`, so `ctx.Err()` stays nil and the timeout is reported
  (it is a transient failure Run retries).
- **Unchanged:** the retry/backoff schedule (`reconnectInitial` → `nextBackoff` doubling to
  `reconnectMaximum`), the `connected` backoff reset (`connected` is true only after a successful
  join reply), `waitForBackoff`'s cancellation arm, and the terminal `ctx.Err()`-only contract. The
  handler call sits between the context check and the backoff reset and touches neither.

### 3.2 The fake-agent reader's exit path

Order inside the one deferred func (registration order is irrelevant; the two statements are
sequential):

1. `close(done)` — a reader parked in the send select (`select { case inputs <- ...; case <-done: }`)
   leaves instead of blocking on a channel nobody reads any more. This is what makes an early
   return (output failure, artifact failure) stop the reader promptly.
2. `input.(io.Closer)` — a non-closable reader (an in-process `strings.Reader`, a channel-backed
   test reader) returns here, so the join can never hang the return on a stream that cannot be
   closed. The contract's "join only when the input was closable" is implemented as exactly this
   type assertion.
3. `closer.Close()` then a join on `readerDone` bounded by `readerJoinTimeout` (50 ms).

The relied-on guarantee, corrected by measurement on this host (2026-11-21, probe program: an
`os.Pipe`/`io.Pipe` read parked in one goroutine, the read end closed after 50 ms; and a child
process whose stdin is a pipe the parent holds open, closing its own `os.Stdin` while blocked):

| carrier | read blocked when the read side closes | evidence |
| --- | --- | --- |
| `io.Pipe` (the test carrier) | returns `io.ErrClosedPipe` in 0 ms | Go runtime broadcasts the pipe's read wait; probe on Windows and Linux |
| `os.Pipe` created in-process | returns "file already closed" in 0 ms | both fds are registered with the runtime poller, so `Close` unblocks the polled read; probe on Windows and Linux |
| a process's inherited stdio pipe (a daemon-spawned agent's stdin) | Windows: returns in 0 ms; Linux: **stays blocked** | child-process probe: `read returned after 0s` on Windows, `read STILL BLOCKED after 1.501s` on Linux |
| Windows console stdin | no unblock guarantee | recorded, not probed (no console in this session) |

So the contract's stated guarantee ("`Close` unblocks a pending read for `io.Pipe`/`os.Pipe`, which
covers the test and the daemon-spawned pipe stdin") holds for the two Go-polled carriers and for
Windows stdio, but **not** for a Linux inherited stdio pipe, which is the daemon's own production
carrier. An unbounded join would therefore park the Linux agent forever at the normal end of every
autonomous run; the bound is not defensive polish, it is what keeps the Linux exit path correct.
This is a correction of a contract premise, recorded here rather than silently resolved; the
contract explicitly allows the bounded form ("bound the join or skip it for a console input and
record which was chosen"). Consequence on Linux: the goroutine stays parked in `readLine` until the
process exits (exactly today's behaviour), and the run returns within the bound instead of waiting.

`done` is closed before the input so the reader's own unblock cannot be mistaken for control-stream
end: whichever arm its select takes, the goroutine exits and closes `readerDone`.

Unchanged by this change: the reader loop reads a record, delivers it (line or error) through the
send select, and returns on the first error; a record read before the end of the run is still
delivered — dropping it would leave the run blocked in `state.boundary` (§5.3 pins that).

## 4. Evidence plan (exact commands, all foreground, from `daemon/` unless noted)

| Gate | Command |
| --- | --- |
| format | `gofmt -l internal/notification cmd/symmetry-fake-agent` (expect empty, exit 0) |
| vet | `go vet ./...` (host windows/amd64) |
| builds | `go build ./...`, `GOOS=linux go build ./...`, `GOOS=windows go build ./...` |
| targeted suites | `go test -count=1 ./internal/notification/... ./cmd/symmetry-fake-agent/...` |
| new notification tests | `go test -count=1 -run 'TestRunReportsDialAndConnectionFailuresToTheHandler|TestConnectionErrorHandlerOptionReportsFailedDial' -v ./internal/notification/` |
| unchanged budget tests | `go test -count=1 -run 'TestRunJoinsAuthenticatesSendsHeartbeatAndDeliversHints|TestRunReconnectBackoffDoesNotBusySpin|TestRunDialUsesDeadline' -v ./internal/notification/` |
| new fake-agent test | `go test -count=1 -run '^TestAutonomousReturnReleasesItsInputReader$' -v ./cmd/symmetry-fake-agent/` |
| extra Linux oracle | the same two packages under WSL Ubuntu-24.04 (Go 1.27.0, seeded module cache), run as host-cross-compiled test binaries (`GOOS=linux GOARCH=amd64 go test -c`) because the distro cannot fetch modules; outcome in §10 — the fake-agent package runs green on Linux, the notification package cannot run there at all (that VM's IPv4 loopback TCP is broken) |
| whitespace | `git diff --check` |
| no suppressions | `git diff -U0 \| grep -E '^\+.*(nolint\|credo:disable)'` (expect none) |
| no host path | `git diff \| grep -iE 'users[/\\]'` plus the same pattern over the untracked plan file (the contract's `git diff origin/main...HEAD` form assumes commits; this stage leaves the tree uncommitted, so the working-tree diff and the untracked file are the equivalent scope) |

`-race` is not run locally (no cgo on this host, the contract says so); the CI daemon race job and
the CI notification/unit steps stay the race oracles. No measurement or benchmark harness runs at
all in this slice, so the "never concurrently with the suite" rule is satisfied trivially.

Mutations (each applied, run, reverted; the mutated file is byte-identical afterwards):

| # | Mutation | Expected red |
| --- | --- | --- |
| a | delete the `client.reportConnectionError(ctx, err)` call | the new test's first `awaitConnectionError` (dial failure) times out |
| a2 | report only the dial half (move the report into the dial's `else` branch) | the new test's second `awaitConnectionError` (dropped connection) times out |
| c | remove `ctx.Err() == nil` from `reportConnectionError` **and** move the call above the early return | the post-cancel drain finds a cancellation report |
| b | delete the fake-agent close/join (keep `close(done)`) | the pipe-write assertion fails (`nil`, not `io.ErrClosedPipe`) and goleak reports the parked reader goroutine |

## 5. Test design

### 5.1 `TestRunReportsDialAndConnectionFailuresToTheHandler` (new, `client_test.go`)

One server, three attempts, no wall-clock classification of errors:

1. attempt 1 → `503` → the handshake fails → **dial failure** report.
2. attempt 2 → accepted, join replied, then no heartbeat reply → after the 20 ms heartbeat timeout
   `runConnection` returns `Phoenix heartbeat reply timed out` → **dropped connection** report. The
   test waits for `joined` (closed by the server after the join, bounded by `testHangGuard` —
   every wait in the test is bounded) before reading that report, and the only error that can
   follow an accepted + joined connection is a `runConnection` error, so the second report's class
   is pinned by server-observed state, not by timing.
3. attempt 3 → accepted and every heartbeat answered (quiet frame reader, §5.4) → the connection is
   provably live when the test cancels, so the only error the client can observe then is
   `ctx.Err()`; the post-`waitForRun` drain asserts the handler saw nothing more. `Run` must return
   `context.Canceled`.

Error classes are additionally pinned by content: the dial report must not mention the heartbeat
and the connection report must. The client is built through `testOptions`/`newTestClient`
(contract/Opus P1-1) with `connectionErrorHandler`, `heartbeatInterval` 1 ms,
`heartbeatTimeout` 20 ms, `reconnectInitial`/`reconnectMaximum` 20 ms, so the failing connection
is deterministic and fast.

### 5.2 `TestConnectionErrorHandlerOptionReportsFailedDial` (new, `client_test.go`)

`newTestClient` builds `clientOptions` directly, so without this test the exported
`WithConnectionErrorHandler`/`New` seam — the only surface production will use — would be
unpinned. A permanently `503` server plus `New(..., WithConnectionErrorHandler(handler))`: the test
waits for the first report (bounded by `testHangGuard`, so the verdict is event-driven), then
cancels and requires `Run` to return `context.Canceled`.

The first draft used a 2 s caller deadline and asserted `context.DeadlineExceeded`; the Linux
oracle measured it failing at 2.04 s with "handler did not receive the failed dial" on a VM whose
load average was ~10.6 on 16 vCPU (a sibling run's `state.test` held 321% CPU). That is the right
product behaviour — the deadline cancelled the dial, and cancellation is never reported — and a
wrong test verdict: a wall-clock budget is exactly the "statement about how fast the client should
be" the repo's own `testHangGuard` comment forbids. The event-driven form has no such coupling, so
a loaded host cannot redden it. This is the first of two additions beyond the contract's "one new
notification test" (§5.5 adds the second); the reason is that the option constructor is otherwise
untested (the contract's test harness bypasses it).

### 5.3 `TestAutonomousReturnReleasesItsInputReader` (new, `autonomous_test.go`)

`t.Chdir(t.TempDir())` (the run writes `progress.md`/`result.json` into `.`), an `io.Pipe` as the
input, `{"steps":1,"step_ms":0,"decision_at":1}`: the run reaches its decision and blocks in
`state.boundary` waiting for input, and the output hook answers the `waiting_for_input` record by
writing a `provide_input` line into the pipe. That makes the pipe delivery load-bearing — a reader
that drops an already-read record leaves the run blocked, and the test's `testHangGuard` fails it
(contract's "drops queued input once the read returns" control). After the run returns:

- `writer.Write` must fail with `io.ErrClosedPipe` — pins that the input was closed at exit;
- the output must contain the `summary` record — pins that the run finished through the pipe;
- `verifyNoLeakedGoroutines(t)` (per-test goleak, W1-08 pattern: `goleak.IgnoreCurrent()` baseline
  captured at test entry, `goleak.VerifyNone(tb, baseline)` in the first-registered cleanup, which
  therefore runs after every other cleanup) — pins that no reader goroutine is parked in `readLine`.

The pipe's **write end is deliberately never closed** by the test. A `defer writer.Close()` runs
before `t.Cleanup` functions, so it would close the write end while the product's reader is still
parked, wake that reader with `io.EOF`, and hide exactly the leak this test exists to catch: the
first mutation run (b) was measured that way — the write assertion failed but goleak stayed green —
before the defer was removed, and the same mutation then reported the parked goroutine. With the
write end open, only the product can release the reader. The pipe holds no goroutine, so leaving it
open costs nothing.

The helper is the W1-08 form with its recording-TB self-test, added by the apply pass (§5.5). The
mutation run (b) is not evidence of the oracle's own liveness: with the oracle neutered, mutation
(b) stays red on the `io.ErrClosedPipe` write assertion, which pins the input close, not the
oracle (measured in the simplify pass, 2026-11-21).

### 5.4 `readChannelFrameQuietly` (test helper refactor)

The live-connection loop must not call `t.Fatalf` from a server goroutine (the read and the write
legitimately fail when the client closes the connection — after the test body may already have
finished). `readChannelFrame` keeps its signature and its failure messages and now delegates to
`readChannelFrameQuietly(ctx, conn) (channelFrame, error)`, which returns the same errors instead of
failing the test.

The insertions all sit after every existing test in the file, but the two new imports (`fmt` for
the quiet decoder, `strings` for the two new assertions) sit above everything and shift every line
of `client_test.go` by two. The one citation affected is `docs/TODO.md:539`'s negative-reconnect
window (`time.Sleep(20 * time.Millisecond)` in `TestRunIgnoresStaleChannelCloseFrames`, unchanged):
it moves from `:283` to `:285`, and `docs/TODO.md` is re-pointed accordingly with a note naming
this diff. Rejected alternative: dropping the two content assertions and the `%w` decoding errors
so no import is needed and no line moves — that trades real assertion strength and error detail
for a docs coordinate, which is the wrong direction.

### 5.5 Apply-pass additions (post-simplify, 2026-11-21)

Three tests were added after the implementation stage, each with its own measured mutation red:

- `TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier` (`autonomous_test.go`) — pins the
  `readerJoinTimeout` arm of the exit join with an input whose `Close` cannot unblock a parked read
  (§3.2's Linux inherited-stdio carrier). Deleting the arm leaves the run parked in the join;
  measured red: exit 1 after 30.01 s, the test's `testHangGuard` ("runAutonomous did not return:
  the input cannot be unblocked and the join was not bounded").
- `TestConnectionErrorHandlerRunsBeforeTheRetryBackoff` (`client_test.go`) — pins the documented
  handler-before-backoff ordering with `reconnectInitial`/`reconnectMaximum` at 5 minutes. Moving
  the `reportConnectionError` call below `waitForBackoff` leaves the report unreachable; measured
  red: exit 1 after 30.02 s at `awaitConnectionError`'s guard.
- `TestLeakOracleDetectsLeakedGoroutine` (`autonomous_test.go`, the W1-08 recording-TB self-test
  with the `leakCheckTB` interface) — pins the per-test leak oracle's own liveness. Neuter the
  oracle body and this self-test fails ("the per-test leak oracle is inert", exit 1) while
  `TestAutonomousReturnReleasesItsInputReader` stays green — the gap the self-test closes.

## 6. Design trade-offs

1. **Bounded join instead of the contract's unconditional one.** Measured: an inherited stdio pipe
   read stays parked on Linux after the writer process closes its own stdin (probe table in §3.2),
   and the daemon's autonomous agent has exactly that stdin at the normal end of a run. Unbounded
   joining would hang every Linux autonomous run; skipping the join for stdio would leave the Linux
   goroutine's exit unpinned. The bounded join keeps the pin (the join completes in µs for polled
   carriers) and caps the cost at 50 ms on carriers that cannot unblock. Cost: on Linux, ≤50 ms of
   exit latency for a run whose reader is parked; the goroutine itself still exits only at process
   exit there (today's behaviour, unchanged).
2. **One reporting call site, not two.** `err` is assigned by the dial and by `runConnection`, and
   both reach the same post-dial code; adding a second call inside the dial failure branch would
   duplicate the guard and let the two branches drift. Mutations a and a2 (both red halves) prove
   the single site covers both classes.
3. **Handler on `clientOptions`, not on `Client`.** Opus P1-1's ruling: the option seam is what a
   caller configures, tests reuse it through `testOptions`, and `Client` keeps no mutable
   post-construction policy. A `(connected bool)` collapse (finding F-23's alternative) was
   rejected because it cannot distinguish "dial failed" from "channel dropped after a join" — the
   distinction the deferred wiring needs for its log message.
4. **The callback runs on `Run`'s goroutine.** No extra goroutine, no queue, no ordering
   ambiguity with the backoff; the doc comment states the non-blocking requirement.
5. **`readerJoinTimeout = 50 ms`.** Large enough that a polled read's wakeup is never truncated
   under load, small enough that the parked-carrier case costs a fraction of the smallest
   configurable step (`step_ms` default 250 ms). A truncated wait changes nothing observable: the
   join only decides whether the return waits a little longer.
6. **The fake-agent test drives the run through the pipe, not `executeAutonomous`.** The existing
   autonomy tests call `executeAutonomous` with a channel and therefore cannot see the reader's
   lifetime; the new test is the only one that pins the goroutine's exit.

## 7. Evidence records kept out of the repository

The stage's answer (outside the repository) carries: both mutation transcripts, the new tests'
`-v` output, the unchanged budget-test output, the `git grep -n` of the two `err` assignments
(`client.go` dial and `runConnection`) now consumed through `reportConnectionError`, and the probe
table's raw output. No `docs/evidence/**` file is created: the contract's owned set excludes that
directory, and the slice record is this plan.

## 8. Deferred: the production wiring of the seam (conductor D5)

`Run`'s caller `daemon/internal/app/app.go:430-436` (the `Run` goroutine, warn at `:433`) and the
construction at `:681-687` (`notification.New` at `:682`) stay untouched. Wiring the handler there
needs a new `notification_connection_failed` literal in `DAEMON_LOG_MESSAGES`
(`scripts/ci/export-linux-v2-redacted-evidence.py`), which is W3-16's file and outside this slice's
write surface. Reusing `notification_client_stopped` was rejected by the conductor: its meaning is
terminal, so a transient reconnect would be misreported as a stop. The deferred wiring therefore
consists of exactly two edits: add the literal to that allowlist, and pass
`notification.WithConnectionErrorHandler(...)` into `notification.New` at `app.go:682` (reporting
through the existing warn path). If a later slice re-adds it, it must serialize with **W3-08** (the
five option constructors at `app.go:241-259`, the other `app.go` owner) and extend the allowlist
first. Until then the daemon passes no handler and behaves exactly as before.

## 9. Out of scope (with reasons)

- `daemon/internal/app/app.go` and `scripts/ci/export-linux-v2-redacted-evidence.py` (D5, §8).
- Phoenix channel protocol/topic handling and `runConnection`'s frame/ref/dedup logic: unchanged
  byte-for-byte; the increment only consumes its already-returned error.
- Notification options unrelated to error surfacing (no new options, no re-typing of existing ones).
- Fake-agent mode selection and `--child` handling: `main.go` changes on its `runAutonomous` call
  line only.
- A package-wide goleak `TestMain` for `cmd/symmetry-fake-agent`: the W1-08 precedent has no
  `TestMain` in the package either, and a package-wide scan would have to ignore the fixture
  subprocess goroutines the tests create deliberately.
- `-race` (no cgo on this host) and every CI-only gate.

## 10. Residual risks and limits

- Linux exit latency: ≤50 ms per autonomous run whose reader is parked, on carriers that cannot
  unblock (measured for inherited stdio). Windows stdio and polled carriers pay µs.
- A Linux goroutine that stays parked in `readLine` until process exit is not verified by a Linux
  oracle on this host's test run alone; the probe above is the evidence, and the CI daemon job
  (Linux) plus any daemon e2e autonomous run exercise the real carrier.
- **Linux oracle, fake-agent package: green on the changed surface (re-measured by the apply pass).**
  `GOOS=linux GOARCH=amd64 go test -c` on this host, the binary run in WSL Ubuntu-24.04 with
  `-test.run '^TestAutonomous|^TestLeakOracleDetectsLeakedGoroutine'`: all ten `^TestAutonomous`
  tests pass, including `TestAutonomousReturnReleasesItsInputReader` (0.01 s, per-test goleak
  included) and `TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier` (0.06 s), plus
  `TestLeakOracleDetectsLeakedGoroutine` (0.46 s) — 11 pass, 0 fail, exit 0, 2 s wall. Running the
  whole package from its source directory instead: 31 pass, 1 fail —
  `TestProviderActionFlowUsesBrokerCapability` fails on the same VM's IPv4-loopback fault below
  ("Post http://127.0.0.1:46553: context deadline exceeded"), not on this change; it passes on the
  Windows host. This is the changed surface's Linux verdict and it needs no network.
- **Linux oracle, notification package: unobtainable on this host's WSL instance — environment fault,
  not a verdict.** IPv4 loopback TCP does not complete in that VM: a plain Python connect from one
  socket to a listener in the same process on `127.0.0.1` times out after 5 s at load average 0.09,
  while `lo` is UP with `127.0.0.1/8`, `ping -c1 127.0.0.1` succeeds (1.17 ms) and an IPv6 `::1` TCP
  connect succeeds in 5 ms (2026-11-21, same session). Every notification test dials an `httptest`
  server over IPv4 loopback, so all of them stall: the pre-existing, CI-Linux-green
  `TestRunJoinsAuthenticatesSendsHeartbeatAndDeliversHints` timed out at 45 s *and* at 240 s with
  `net/http.(*Transport).startDialConnForLocked` in the dump — a test whose notification path is
  unchanged by this slice (no handler is configured, so the added call is a nil check), and the same
  fault reddened the two new tests' first Linux run. An earlier WSL run of the same session also
  overlapped a sibling worktree's `internal/state` measurement loop (`state.test` at 168–321% CPU,
  load average 9.9–10.6), which is a second, independent reason not to treat that VM's timing as a
  verdict. The notification package's Linux/race verdict therefore remains the CI daemon job's, as the
  contract states ("no Linux-only suite here, so no WSL gate is required"); the contract's required
  gate is the Windows host run, which executes both packages fully.
- `-race` is not run locally: the handler adds no shared state (it is read-only after construction
  and called from one goroutine), and the reader exit adds two channels and one `sync`-free close
  ordering; the CI daemon race job remains the oracle.
- The Windows console-stdin case is recorded but not probed; the bound covers it.
- `goleak`'s window (~0.43 s of retries) means a reader that exits late still passes; the test's
  `io.ErrClosedPipe` assertion is the timing-independent half of the pin.
