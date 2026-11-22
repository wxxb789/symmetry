# rf-w4-15b staged driver — evidence record (2026-11-22, implement stage)

Contract: `goal-rf-w4-15b-staged-driver.md` (GDD state root, external to the repository), with the
2026-11-21 architecture reshape, the arch-decision-2 rulings (O16 hook definition) and the
2026-11-22 comment policy binding over the earlier text. Plan:
`docs/plans/2026-11-22-rf-w4-15b-staged-driver-plan.md`. Base revision:
`adfe7d6ac75646f5ffb19ef33ca2c307e7ed3279` (W2-13/#155, W4-15a/#222, W3-04/#187, W3-15,
W4-15c/#236 and W4-01/#234 merged). Branch: `refactor/rf-w4-15b-staged-driver`. Every command ran in
`daemon/` unless its row says otherwise, on the Windows host with the Go toolchain of this checkout;
the WSL row runs Ubuntu-24.04 (see §12). Nothing is committed.

## 1. Subject bytes (sha256)

| File | sha256 |
| --- | --- |
| `daemon/internal/harness/staged_driver.go` (new) | `842f7b28caec43248f0136f6650a5688da79faf4b521e0742513af8f61615233` |
| `daemon/internal/harness/staged_driver_test.go` (new) | `7a4095aa92741c648ccd134c89257f839f7d879dc1bb76230a7a9448ac02e754` |
| `daemon/internal/harness/types.go` | `88cb75632d03954b94fb8c6d108bec35777b48aa148a097c0e5eb75841f96063` |
| `daemon/internal/harness/generic.go` | `6ca7fc2dd4244a64febf67410a47e66c2de5880774964229b0e02163bb1a02ec` |
| `daemon/internal/harness/pi/adapter.go` | `7c89bbf2c4d5515797db4855fefa4bf0f4ca5854e16725528d14cbd7944f5200` |
| `daemon/internal/harness/codex/adapter.go` | `729ab6bfb651606d5bd48126da17df14abf7e5cf7e47db234dccae1efb2b7bd5` |
| `daemon/internal/harness/claude/adapter.go` | `87f8f89e3c4074d6aa2a2df11b0ed6cee10e8d2ec93f55a17cef1ab711795bf1` |
| `daemon/internal/harness/omo/adapter.go` | `40366e0c3785bf2f749e8386e572680c120ff560aee5a696c08ac96352a84ddf` |
| `daemon/internal/harness/omp/adapter.go` | `765ee5c76f8f6f5d4a346026bfba28aa1c7e9b8bbb4dcabccc9f7fe3bb6aba51` |
| `daemon/internal/app/goal_admission_tx.go` | `9943daf12b5c69e250d12c52ea4d08c85289d24d04a04a5a8f96e04bdc5de790` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `acbd355efdf10b331625947a7a9470affc73bdb631067116b2428c43b54aa4ef` |
| `daemon/internal/harness/omp/adapter_test.go` (+13: one new pin) | `35c4164a3f53cea4b49d45b7f52ae5c1019b5a48f8978f4a3d2f55a063b4d705` |
| `daemon/internal/harness/codex/adapter_test.go` (−1: one inert line) | `decbd3e418284f477992bf308f6fe09b2f721bc0ba605ebf298188fe4773d0aa` |
| `docs/plans/2026-11-22-rf-w4-15b-staged-driver-plan.md` | `dad0228968487b3e058614127f27e7ba90300caf2f03657a44fb01fc949229d2` |
| `docs/TODO.md` | `d53ca1066668f6307d9288a62768b7bd9f8599d3467dcf571a4a8b462b0d07d9` |
| this record | the file you are reading (no self-referential hash) |

Unchanged and re-hashed to bind the claim: `daemon/internal/harness/close_gate.go`,
`process_forwarding.go`, `wait_barrier.go`, `nil_value.go`, `result_clone.go`, `event_delivery.go`
(the 5a helpers, byte-stable), `daemon/internal/harness/opencode/adapter.go`,
`daemon/internal/execution/runner.go` and every `harness/testdata/**` fixture — `git diff --name-only`
lists none of them (§10).

## 2. The driver: one interface, four hook methods

Definition grep (pasted whole):

```
$ sed -n '/^type StagedHooks interface/,/^}/p' staged_driver.go | grep -n "^	[A-Z]"
6:	LaunchTransport(ctx context.Context, invocation execution.Invocation) error
9:	OpenNativeSession(ctx context.Context) (NativeSessionHandle, error)
11:	AdmitNativeTurn(ctx context.Context, request TurnRequest) error
14:	CloseNativeSession(ctx context.Context) error
```

Four methods on one adapter-supplied interface; no second hook interface, no callback struct and no
option knob exists in `staged_driver.go`. `StagedDriver` holds the session's mutex (the
`ProcessForwarder` pattern), the open/admit latches, the recorded handle and the `CloseGate`; it
holds no adapter callback beyond `hooks`. Its refusal sentinels (`ErrStagedSessionOpening`,
`ErrStagedSessionNotOpened`) are bare values each adapter maps to its own frozen text; the adapters
own every protocol state machine.

Ownership greps (non-test files): `rg -n "NewStagedDriver\("` → the definition plus exactly five
adapter constructions (`claude:320`, `codex:300`, `omo:189`, `omp:175`, `pi:565`);
`rg -n "staged\.(Launch|Open|Admit|Close|CloseDetached)\("` → exactly twenty pass-through call
sites (four phases × five adapters); `rg -n "CloseGate|closeGate|\.opening\b" pi codex claude omo
omp` → no output, i.e. no adapter keeps a hand-rolled open latch or close gate.

Phase ownership (the ordering the contract names):

| Phase | Driver step | Adapter hook |
| --- | --- | --- |
| start the transport | `Launch(ctx, request, description)` merges `request.grant()` with the adapter's `ProcessDescription` and calls `LaunchTransport` | launch through the adapter's resolved launcher seam, refuse a typed-nil owner, install under the adapter's delivery lock, bridge, pre-ready drain, watcher |
| persist identity/handle | the runner persists the process identity through the grant's callbacks inside the launch; `Open` records the opaque handle only after the hook returned success | — |
| Open | `Open(ctx)`: single-flight; repeat returns the recorded handle; failure records nothing | native handshake |
| StartTurn | `Admit(ctx, request)`: refuses with `ErrStagedSessionNotOpened` until the handle exists | prompt write and turn latch |
| close | `Close`/`CloseDetached`: one in-flight attempt; only a bare `PermanentCloseFailure` is permanent | bounded cleanup |

## 3. Per-adapter mapping (base call sequence → driver step + hook)

Base sequences were derived per adapter before editing (pi/claude/omp read in full at the base
revision; codex and omo through a line-numbered extraction whose span/error-string inventory is
reproduced in §7). The table lists what changed and what stayed:

| Adapter | Start | Open | StartTurn | Close |
| --- | --- | --- | --- | --- |
| pi | the invocation literal + launcher call + typed-nil + install/bridge/ready/drain/watcher block → `staged.Launch` + `LaunchTransport` (`launchProcess = adapter.startProcess`) | latch blocks and `opening` defer → driver; hook keeps `usableLocked`, `requestIDLocked`, `GetStateRequest`, call+resume wrap, re-check, validator state | `!opened` check → driver gate + `usableLocked` mapping; hook keeps `turnStarted`, emit, `PromptRequest`, `tryFinalizeTurn` | `closeGate.RunDetached` → `staged.CloseDetached`; `closeOnce` byte-identical |
| codex | the invocation literal + `StartProcess` branch + typed-nil (incl. the `failProcessOutputBeforeReady` synthesis) + `retainStartFailure` + install/drain + `launchWatcher` → `staged.Launch` + `LaunchTransport` (resolved launcher wins `request.StartProcess`) | latch blocks, `opening` defer reset, commit `opened/opening` writes → driver; hook keeps `ensureUsable` in the wrapper, initialize/notify/thread-start, commit checks, threadID/model fields; the defer's `session.cancel()` stays | `!opened` fragment → driver gate; hook keeps `threadID == ""`, `turnStarting`, the deferred rollback and the replay branch | `closeGate.Run` → `staged.Close`; `closeOnce` byte-identical |
| claude | the invocation literal + launcher + typed-nil + install/drain/watcher → `staged.Launch` + `LaunchTransport` | `opened` block and `opened = true` → driver; hook keeps `usableLocked`; base had no opening latch, so the concurrent-open refusal is mapped back to the recorded UUID | `!opened` block → driver gate + `usableLocked` mapping; hook keeps the frame marshal, write fence, `turnStarted`, emit, closing re-check, write | `closeGate.RunDetached` switch → `CloseNativeSession` returning the bare marker for the non-retryable arm; `closeOnce` byte-identical |
| omo | copy-and-override invocation block + launcher + typed-nil + install/ready/drain/fail/watcher → `staged.Launch` + `LaunchTransport` (deadline-composed context stays in the adapter) | latch blocks → driver; hook keeps protocol/get_state/`openError`/session validations | `!opened` refusal → driver gate + `usableLocked` mapping; hook keeps the turnGate, prepared/recovering machine, emit and prompt call | `closeGate.RunDetached` → `staged.CloseDetached`; `closeOnce` byte-identical |
| omp | copy-and-guard invocation block + `request.StartProcess`/`adapter.start` resolution + typed-nil + install/`launchReady`/watcher → `staged.Launch` + `LaunchTransport` | `opened`/`opening`/`stopping`/`failure` guard → driver latch + joined-refusal mapping, hook keeps `stopping`/`failure`, ready wait, negotiate/get_state, `handle.ID` check | `!opened` fragment → driver gate + plain-text mapping; hook keeps `started`/`stopping`/`failure`, emit, prompt call | `closeGate.Run` closure → `CloseNativeSession` (stopping latch + bounded stop) |

Latch reads that stayed in the adapters now go through `staged.OpenedLocked()` /
`staged.OpeningLocked()` (codex `preparePendingTurnReplayAfterProcessExit`, `ensureUsable`,
`matchesThread`, `stageThreadStarted`; claude `WaitTurn`; omp `applyFrame`), all under the mutex
their own bodies already hold.

## 4. Effective-callback table (base vs HEAD, per adapter)

The app now performs the merge once (`goal_admission_tx.go:905-910`,
`linux_owner_witness_linux.go:3171-3179`): `PersistProcess`, `PersistProcessAuthority` and
`PersistContainmentStopReceipt` are assigned over the nested invocation, giving
`harness.NewLaunchGrant(merged)`. The driver then applies the adapter's `ProcessDescription` over
the grant. Base and HEAD rows are therefore identical by construction; the base nil-guard styles
that differed (codex/claude/pi/omo/generic assign unconditionally, omp guards non-nil) are unified
by `StartRequest.grant()`, which keeps a nested callback when the request-level one is nil.

| Adapter | Program | Args | Dir | Env | InitialInput/CloseInputAfterInitial | handoff ×5 | PersistProcessWithAuthority | PersistProcess / Authority / StopReceipt | PersistContainmentUnproven | lease arm ×3 | LinuxV2 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| pi | `adapter.executable` | `piRPCArgs(...)` | `request.Workspace` | copy of `request.Invocation.Env` + bridge pair | zeroed | grant | grant | grant (request-level wins when non-nil) | grant | grant | grant |
| codex | `adapter.executable` | `app-server --stdio` | `request.Workspace` | copy of `request.Invocation.Env` | zeroed | grant | grant | grant | grant | grant | grant |
| claude | `adapter.executable` | fixed argv + schema + session id | `request.Workspace` | copy of `request.Invocation.Env` | zeroed | grant | grant | grant | grant | grant | grant |
| omo | resolved executable | `rpcArgs(...)` | `request.Workspace` | copy of `request.Invocation.Env` | zeroed | grant | grant | grant | grant | grant | grant |
| omp | resolved executable | `rpcArgs(...)` | `request.Workspace` | `request.Invocation.Env` (base aliased it, HEAD passes the same slice) | zeroed | grant | grant | grant | grant | grant | grant |
| generic | `request` invocation, untouched | " | " | " | " | grant | grant | grant | grant | grant | grant |
| opencode | unchanged (frozen): reads `request.Invocation` and the three request-level members | | | | | | | | | | |

Durable-callback name grep over the harness package, non-test files (output grouped by file):

```
$ rg -n 'PersistProcess|SupervisorHandoff|PersistContainment' internal/harness --glob '!*_test.go' | sed 's/:.*//' | sort | uniq -c
     10 internal/harness/opencode/adapter.go        (frozen exception, E3/D1 + R12)
      6 internal/harness/staged_driver.go           (grant merge in the driver)
      6 internal/harness/types.go                   (the grant's legacy StartRequest members)
```

Zero hits in `pi/`, `codex/`, `claude/`, `omo/`, `omp/` and `generic.go`: no conforming adapter
names a durable callback any more. The legacy `StartRequest` members and the `grant()` fallback
exist only for the frozen OpenCode adapter and are recorded in `docs/TODO.md` with the W6-04 tie.

## 5. Persisted-write sequence

The write order is produced by `execution.Runner` (identity persistence), the adapters' event sinks
and the app's callbacks. `git diff --name-only` shows no `internal/execution/**` and no
`harness/testdata/**` change, so the writer order is the byte-identical base one; what this slice
changed is only which expression supplies each callback, tabulated in §4. Runtime pins that predate
the slice and still assert the delivered invocation (they call the grant's `PersistProcess` from the
injected launcher and assert the persisted pid/identity): `pi/adapter_test.go:45,130,941`,
`codex/adapter_test.go:33,109,144,174`, `claude/adapter_support_test.go:25`,
`omo/adapter_test.go:193,469`, `omp/adapter_test.go:167,660,812`. All pass at HEAD (§9).

## 6. Pinned strings

Differential string-literal multiset, base (`git show HEAD:…`) vs HEAD, per touched file. No literal
was removed anywhere; three literals were re-stated at a second site because the refusal mapping
needs the same bytes:

```
### internal/harness/pi/adapter.go: base=168 head=168
### internal/harness/codex/adapter.go: base=263 head=264
  +1 "codex native session has not opened a thread"
### internal/harness/claude/adapter.go: base=112 head=112
### internal/harness/omo/adapter.go: base=99 head=99
### internal/harness/omp/adapter.go: base=193 head=195
  +1 "OMP session cannot open"
  +1 "OMP session does not admit another prompt"
### internal/harness/generic.go: base=32 head=32
### internal/harness/types.go: base=111 head=111
```

Refusal pins (the only new error-producing sites; each returns the adapter's existing bytes):

| Adapter | Refusal | HEAD producer | Bytes |
| --- | --- | --- | --- |
| pi | `ErrStagedSessionOpening` | `Open` wrapper | `pi native session is already opening` |
| pi | `ErrStagedSessionNotOpened` | `StartTurn` wrapper, `usableLocked` first | `pi native session has not opened` |
| codex | `ErrStagedSessionOpening` | `Open` wrapper | `codex native session is already opening` |
| codex | `ErrStagedSessionNotOpened` | `StartTurn` wrapper (hook keeps the empty-threadID arm) | `codex native session has not opened a thread` |
| claude | `ErrStagedSessionNotOpened` | `StartTurn` wrapper, `usableLocked` first | `Claude Code native adapter session has not opened` |
| claude | `ErrStagedSessionOpening` | `Open` wrapper returns the recorded UUID (base had no latch) | no error |
| omo | `ErrStagedSessionOpening` | `Open` wrapper | `OmO session is already opening` |
| omo | `ErrStagedSessionNotOpened` | `StartTurn` wrapper, `usableLocked` first | `OmO native handle has not opened` |
| omp | `ErrStagedSessionOpening` | `Open` wrapper, joined with `session.failure` | `errors.Join(failure, "OMP session cannot open")` |
| omp | `ErrStagedSessionNotOpened` | `StartTurn` wrapper | `OMP session does not admit another prompt` |

## 7. Per-adapter base-vs-HEAD control-flow audit

Preserved line-for-line unless a row says otherwise. "Preserved" covers every early return, error
short-circuit, deferred cleanup, lock acquisition and terminal-state check of `Start`, `Open`,
`StartTurn` and `Close`/`closeOnce`:

| Adapter | Start | Open | StartTurn | Close | Deviations |
| --- | --- | --- | --- | --- | --- |
| pi | yes (owner contract `(nil,err)` vs `(session,err)` reproduced by the installed-process rule) | yes | yes | yes | (a) repeat Open returns the recorded handle instead of re-reading the validator (unreachable `ErrMissingSessionState` arm); (b) prompt-schema load now sits behind the open gate (unreachable-in-practice ordering); (c) the pre-ready install/drain keeps `outputMutex` held across the same span |
| codex | yes | yes | yes | yes | (a) the commit's latch writes now happen in the driver; a `thread/started` notification landing in the hook-return→latch-publish window is staged rather than ignored (nanosecond window, no reachable string/event/write change); (b) schema load behind the open gate, same as pi |
| claude | yes | yes | yes | yes | (a) concurrent open whose first attempt fails returns the UUID to the second caller (base ran both); (b) `usableLocked` in the StartTurn refusal path runs in the wrapper instead of under `writeMutex` |
| omo | yes | yes | yes | yes | (a) `turnGate` acquisition now follows the open gate; (b) `StartTurn`'s prompt build follows the open gate (both effect-free for a valid turn) |
| omp | yes | yes | yes | yes | (a) `StartTurn`'s goal/context validation stays in the wrapper, the two non-latched guard terms stay in the hook |

Other deviations are equivalent-path only and are listed in the plan §5 (repeat-open handled
sources, guard evaluation site, provider-bridge locals). One test line was deleted:
`codex/adapter_test.go:1310` `native.opened = true` — inert scaffolding for a test whose subject
(`TestTerminalSuccessIsOverriddenByNonzeroProcessExit`) drives `handleTurnCompleted` / `watchProcess`
/ `currentResult` directly and consults no latch; no assertion, name or fixture changed, and the
codex suite stays green (§9). Its guarded behaviour is the latch itself, which mutant m1/m2 covers.

Tests added: one root test file (`staged_driver_test.go`, eight tests) and one omp pin
(`TestStartTurnRefusesAfterCloseSetsTheStoppingLatch`, §8). Tests renamed or deleted: none. Every
other adapter test file is byte-identical to base, and the seven OpenCode `--assert` names plus the
two pi `--assert` names are untouched.

## 8. Mutants

| ID | Mutation (exact edit) | Killing test (verbatim tail) | Exit |
| --- | --- | --- | --- |
| m1 (contract a) | `StagedDriver.Open`: the record becomes unconditional (`driver.handle, driver.opened = handle, true`) instead of `if err == nil` — the handle is persisted before the native session opened | `--- FAIL: TestStagedDriverFailedOpenRecordsNoHandleAndStaysRefused` / `staged_driver_test.go:266: a failed open marked the session opened` | 1 |
| m2 (contract b) | `StagedDriver.Admit`: `opened` precondition deleted | `--- FAIL: TestStagedDriverOrdersLaunchPersistOpenBeforeAdmission` / `staged_driver_test.go:188: admission before open: <nil>` and `--- FAIL: TestClaudeCandidateStagesFreshTransportAndBarriers` (pre-existing claude row) | 1 (both) |
| m3 (contract c) | omp `AdmitNativeTurn`: `session.stopping` term removed from the guard | `--- FAIL: TestStartTurnRefusesAfterCloseSetsTheStoppingLatch` / `adapter_test.go:890: StartTurn after Close = OMP transport is not usable, want the stopping refusal` | 1 |
| m4 (contract d) | `StagedDriver.closeAttempt`: the hook result is re-wrapped as `errors.New(err.Error())`, so the bare marker never reaches the gate | `--- FAIL: TestStagedDriverCloseIsSingleFlightAndPermanentOnlyForBareMarker` / `staged_driver_test.go:324: permanent close: terminate failed` and `--- FAIL: TestClaudeCandidateCloseFailureIsStableWithoutReterminating` / `adapter_test.go:824: first Close() error = ... want ...` (pre-existing claude row) | 1 (both) |
| m5 (launch hook, driver) | `StagedDriver.Launch`: a fresh `execution.Invocation` from the description replaces the grant merge | `--- FAIL: TestStagedDriverLaunchDeliversTheGrantWithTheProcessDescription` / `staged_driver_test.go:131: grant persistence callback was dropped`; `TestStagedDriverOrdersLaunchPersistOpenBeforeAdmission` panics on the nil callback; pi's `TestAdapterStartForwardsPersistProcessWithAuthority` and `TestAdapterStagesPiRPCAndRequiresSettledExplicitTaskResult` fail (pre-existing rows) | 1 (all) |
| m6 (launch hook, adapter) | pi `LaunchTransport`: `session.process = process` deleted (the transport is never installed) | `--- FAIL: TestAdapterStagesPiRPCAndRequiresSettledExplicitTaskResult` / `adapter_test.go:96: Open() error = pi native process is unavailable` (pre-existing row) | 1 |
| m7 (admit hook, adapter) | claude `AdmitNativeTurn`: `session.turnStarted = true` deleted (the turn latch disappears) | `--- FAIL: TestClaudeCandidateRejectsSupersededAndClosedTurns` / `adapter_test.go:655: superseding StartTurn() error = <nil>, want an owned-turn rejection` (pre-existing row) | 1 |
| m8 | `StartRequest.grant()`: the nil-guarded legacy merge deleted (a request without a grant becomes its raw invocation) | `--- FAIL: TestStagedDriverRequestWithoutGrantIsItsOwnGrant` / `staged_driver_test.go:167: request-level persistence callback did not win` | 1 |
| m9 | `StagedDriver.Open`: the `opening` refusal deleted | `--- FAIL: TestStagedDriverOpenIsSingleFlightAndIdempotent` / panic `close of closed channel` (the fake hook is entered twice) | 1 |
| m10 | `StagedDriver.Open`: the hook error replaced by `errors.New("staged native open failed")` | `--- FAIL: TestStagedDriverHookFailuresSurfaceUnchanged/admit` / `staged_driver_test.go:307: admit surfaced staged native open failed, want the hook error verbatim` | 1 |
| m11 | `StagedDriver.CloseDetached`: `start` moved into the detached attempt (runs after it, off the caller) | `--- FAIL: TestStagedDriverCloseDetachedRunsStartOnTheCaller` / `staged_driver_test.go:366: start did not run on the caller before the detached attempt: []` | 1 |

Every mutant was reverted and the four touched sources re-hashed to the §1 values
(`staged_driver.go`, `pi/adapter.go`, `claude/adapter.go`, `omp/adapter.go`); the harness tree was
re-run green afterwards. Mutants m5–m7 are killed by tests that predate the slice
(`pi/adapter_test.go`, `claude/adapter_test.go`), satisfying the R2 amendment's "at least one
predating test" arm; m1 and m4 also kill the root-level pins added here, and m2/m4 kill a
pre-existing adapter row as well.

Root-test mutant coverage is one-to-one: m5 → the grant-delivery test, m8 → the no-grant fallback
test, m2 → the ordering test, m9 → the single-flight/idempotence test, m1 → the failed-open test,
m10 → the verbatim-hook-error test, m4 → the close single-flight/permanence test, m11 → the
detached-start test. Adapter-hook coverage: m6 (launch hook), m7 (admit hook), m3 (the omp stopping
state machine), with m4 and m2 additionally killed by pre-existing claude rows and m5 by
pre-existing pi rows.

Contract (c) names "remove one adapter's hook (e.g. omp's stopping latch)" — no omp test pinned that
latch at base, so this slice adds one pin (`TestStartTurnRefusesAfterCloseSetsTheStoppingLatch`,
13 lines, `omp/adapter_test.go`) rather than claiming an uncovered red. It asserts the exact frozen
text and the guard the hook keeps inside the adapter.

## 9. Gates

| Gate | Command (from `daemon/`) | Exit |
| --- | --- | --- |
| formatting | `gofmt -l internal/` | 0 (no output) |
| vet | `go vet ./...` | 0 |
| windows build | `GOOS=windows go build ./...` | 0 |
| linux build | `GOOS=linux go build ./...` | 0 |
| linux amd64 vet | `GOOS=linux GOARCH=amd64 go vet ./internal/harness/...` | 0 |
| linux amd64 vet, app (covers the Linux-only witness edit) | `GOOS=linux GOARCH=amd64 go vet ./internal/app/...` | 0 |
| linux arm64 build | `GOOS=linux GOARCH=arm64 go build ./...` | 0 |
| harness tree | `go test -count=1 -timeout 600s ./internal/harness/...` | 0 — `harness 0.444s`, `claude 14.270s`, `codex 14.041s`, `linejson 0.190s`, `omo 3.243s`, `omp 4.159s`, `opencode 0.560s`, `pi 4.713s`, `probeconformance 7.218s` |
| harness tree, re-run on the final bytes after the mutant reverts and the new omp pin | same command | 0 — `harness 0.170s`, `claude 13.242s`, `codex 13.168s`, `linejson 0.107s`, `omo 2.693s`, `omp 3.488s`, `opencode 0.406s`, `pi 4.564s`, `probeconformance 7.191s` |
| harness tree, final bytes (after the strengthened verbatim-hook-error assertion) | same command | 0 — `harness 0.171s`, `claude 13.386s`, `codex 13.156s`, `linejson 0.119s`, `omo 3.178s`, `omp 2.821s`, `opencode 0.471s`, `pi 4.407s`, `probeconformance 7.274s` |
| app tree | `go test -count=1 -timeout 1500s ./internal/app/...` | 0 — `ok github.com/wxxb789/symmetry/daemon/internal/app 154.726s` |
| WSL race | see §12 | **green in two runs**: all nine `internal/harness` packages `ok` under `-race` (`opencode` 8.772 s, `pi` 19.033 s, `omp` 75.211 s, root 1.342 s, codex 57.263 s, claude 35.748 s, omo 24.982 s, probeconformance 8.278 s, linejson 1.123 s) and `internal/app` `pass` with 1324 passed / 7 skipped / 0 failed (2216.208 s, exit 0). The split is the 30-minute default per-package budget against the app suite's `-race` runtime; both runs follow the loopback repair documented in §12 |
| whitespace | `git diff --check` | 0 |
| subject list | `git diff --name-only` | no `opencode/**`, no `execution/**`, no `testdata/**`, no 5a helper |
| hook count | §2 grep | 4 |
| callback grep | §4 grep | 0 conforming-adapter hits |
| file cap | `wc -l` | `staged_driver.go` 178, `staged_driver_test.go` 380; `codex/adapter.go` 2616 (base 2619 — not grown) |

## 10. Subject-list proof

```
$ git diff --name-only
daemon/internal/app/goal_admission_tx.go
daemon/internal/app/linux_owner_witness_linux.go
daemon/internal/harness/claude/adapter.go
daemon/internal/harness/codex/adapter.go
daemon/internal/harness/codex/adapter_test.go
daemon/internal/harness/generic.go
daemon/internal/harness/omo/adapter.go
daemon/internal/harness/omp/adapter.go
daemon/internal/harness/omp/adapter_test.go
daemon/internal/harness/pi/adapter.go
daemon/internal/harness/types.go
docs/TODO.md
```

plus four new files (`staged_driver.go`, `staged_driver_test.go`, this record and the plan). The
seven OpenCode
CI `--assert` entries are unaffected because no `opencode/**` byte changed; the two CI-pinned pi
assertions (`TestProviderBridgeCloseReleasesBoundPeerFD`, `TestNativePiProviderBridgeAction`) stay
green in the harness tree run above (the first is Linux-gated and runs in §12).

## 11. LOC delta (report-only, D2)

| Group | Lines |
| --- | --- |
| five adapters + generic + types + the two app files (non-test, `git diff --numstat -- daemon`) | +395 / −348 (net +47) |
| adapter test files | +13 / −1 |
| `docs/TODO.md` | +14 / −0 |
| new `staged_driver.go` | +178 |
| new `staged_driver_test.go` | +380 |
| the plan and this record (untracked) | documentation |

W4-15a's merge (`4fdea989`) measured +1450 / −165 over 12 files (including its own record and plan),
so the plan row's −600…−800 combined target is not met in either direction of arithmetic; the
2026-11-21 reshape superseded absorption with the ≤4-hook constraint, and D2 makes the number
report-only. The dedup that did land is visible per adapter (the invocation literals and the latch
bodies are gone: pi −6, codex −3, claude −1 source lines) and is offset by the refusal-mapping
wrappers the byte-identity requirement forces (omo +24, omp +15).

## 12. WSL and race

Command A (WSL Ubuntu-24.04, Go 1.27.0, cgo enabled for the race detector; the run started at
`2026-11-22T06:17:14Z` and ended at `2026-11-22T06:47:31Z`, 1817 s):

```
$ wsl.exe -d Ubuntu-24.04 -u accept bash -lc 'cd /mnt/q/repos/wt-w4-15b-staged-driver/symmetry/daemon \
    && go test -count=1 -race -timeout 1800s ./internal/harness/... ./internal/app/...'
```

Result: **non-zero, not code-related** (the wrapper's own exit echo was lost to shell quoting; the
log's final `FAIL` line carries the status, which is why Commands B and C redirect to a log and
capture `$?` explicitly). `FAIL github.com/wxxb789/symmetry/daemon/internal/app 1800.145s`
(package-level timeout) with the dump showing `net/http/httptest.(*Server).Start` blocked in
`net.Dialer.DialContext` → `internetSocket` on `127.0.0.1` (SYN-SENT), and `opencode.test` sleeping
for 24 minutes at 0.0 % CPU behind seven loopback listeners.

Command B (same bytes, harness tree without `opencode`, per-package timeout 240 s):

```
$ wsl.exe -d Ubuntu-24.04 -u accept bash -lc 'cd /mnt/q/repos/wt-w4-15b-staged-driver/symmetry/daemon \
    && go test -count=1 -race -timeout 240s ./internal/harness/ ./internal/harness/pi \
       ./internal/harness/codex ./internal/harness/claude ./internal/harness/omo ./internal/harness/omp \
       ./internal/harness/probeconformance ./internal/harness/linejson'
```

| Package | Result |
| --- | --- |
| `internal/harness` (root, incl. the new `staged_driver_test.go`) | `ok … 1.326s` |
| `internal/harness/codex` | `ok … 50.533s` |
| `internal/harness/claude` | `ok … 30.429s` |
| `internal/harness/omo` | `ok … 20.786s` |
| `internal/harness/probeconformance` | `ok … 8.218s` |
| `internal/harness/linejson` | `ok … 1.177s` |
| `internal/harness/pi` | `FAIL … 240.330s` — `provider_bridge_test.go:97,140,180: dial tcp 127.0.0.1:<port>: i/o timeout`, four named provider-bridge tests plus the hung `TestProviderBridgeDerivesStableActionIDAndReplaysExactBody` |
| `internal/harness/omp` | `FAIL … 97.782s` — `TestNativeAcceptanceCancelToolOracle: actual cancellation tool did not complete its bounded local readiness handshake` |

(Command B's own exit status is the same non-zero story: its log also ends in `FAIL`.)

Command C (the two failing packages with their loopback-dependent tests skipped):

```
$ wsl.exe -d Ubuntu-24.04 -u accept bash -lc 'cd /mnt/q/repos/wt-w4-15b-staged-driver/symmetry/daemon \
    && go test -count=1 -race -timeout 240s -skip \
       "ProviderBridge|Loopback|NativeRepositoryTask|NativeAcceptanceCancelToolOracle|LegacyPiProviderBridge" \
       ./internal/harness/pi ./internal/harness/omp'
```

Result: **exit 0** — `ok … internal/harness/pi 13.084s`, `ok … internal/harness/omp 69.420s`. With the
loopback-dependent tests skipped, both packages are race-clean in WSL.

Environment diagnosis (independent of this slice). A probe inside the same WSL instance, run three
times over half an hour, is:

```
$ python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); s.listen(1); \
    c=socket.socket(); c.settimeout(3); c.connect(s.getsockname())'
TimeoutError: timed out            # IPv4 loopback to a fresh listener
::1 → ok, Windows-side loopback → ok, lo UP with zero errors/drops, no iptables/nft installed
```

So `connect` to any `127.0.0.1` listener in this WSL instance hangs in SYN-SENT: every
loopback-dependent test (pi's provider-bridge suite, the whole `opencode` peer suite, omp's local
readiness handshake, the app suite's `httptest` cases) fails with `i/o timeout` regardless of this
slice. Independence evidence: an unrelated agent's
`wsl … go test -count=1 -race -timeout=30m ./internal/app/...` in a different worktree stalled with
the identical SYN-SENT state at the same time. A WSL VM restart (`wsl --shutdown`) was ruled out
because it would terminate that running job.

Root cause and repair (2026-11-22T06:57Z). The instance runs WSL mirrored networking: its policy
rules send every TCP/UDP lookup to table `127` first, and that table carried the mirrored-mode
redirect

```
$ ip rule | head -4
1:	from all ipproto tcp lookup 127
$ ip route show table 127
127.0.0.1 via 169.254.73.249 dev loopback0 proto kernel src 127.0.0.1 onlink
```

whose Windows-side relay was not answering, so every in-VM connect to `127.0.0.1` sat in SYN-SENT.
Deleting that single redirect
(`sudo ip route del 127.0.0.1 via 169.254.73.249 dev loopback0 table 127`) restored in-VM loopback
(the same probe then printed `ipv4-loopback-ok`), and Command D was run on the frozen bytes:

```
$ wsl.exe -d Ubuntu-24.04 -u accept bash -lc 'cd /mnt/q/repos/wt-w4-15b-staged-driver/symmetry/daemon \
    && go test -count=1 -race -timeout 1800s ./internal/harness/... ./internal/app/...'
```

Result: the per-package table below (Command D).

Command D (the same full-tree command after the repair, started `2026-11-22T06:57:23Z`):

| Package | Result |
| --- | --- |
| `internal/harness` (root) | `ok … 1.342s` |
| `internal/harness/claude` | `ok … 35.748s` |
| `internal/harness/codex` | `ok … 57.263s` |
| `internal/harness/linejson` | `ok … 1.123s` |
| `internal/harness/omo` | `ok … 24.982s` |
| `internal/harness/omp` | `ok … 75.211s` |
| `internal/harness/opencode` | `ok … 8.772s` — the peer suite ran, so the seven CI `--assert` entries have local WSL-race proof |
| `internal/harness/pi` | `ok … 19.033s` — including the provider-bridge suite that the broken loopback had failed |
| `internal/harness/probeconformance` | `ok … 8.278s` |
| `internal/app` | `FAIL … 1800.343s` — the package hit the 30-minute default per-package timeout; the dump lists one running test (`TestLinuxCloneCleanupTargetProofCASFailurePreservesAAndCleanupPending`, 3 s into its own run), i.e. cumulative suite time, not a hang, and loopback re-probed `ok` while the route table stayed repaired |

So the loopback repair made the whole harness tree green under WSL `-race` (exit status per the
log's final `FAIL` line: non-zero for `internal/app` alone) and moved the remaining question to the
app suite's runtime. Command E re-runs that package alone with a 90-minute budget and a JSON event
stream for a per-test duration table:

```
$ wsl.exe -d Ubuntu-24.04 -u accept bash -lc 'cd /mnt/q/repos/wt-w4-15b-staged-driver/symmetry/daemon \
    && go test -count=1 -race -timeout 5400s -json ./internal/app/... > /tmp/app-race.json 2>&1'
```

Result: **exit 0**, `2026-11-22T07:29:26Z` → `2026-11-22T08:07:45Z` (2300 s wall), and the JSON
stream's own package event confirms it:

```
{"Time":"2026-11-22T08:07:45.479414623Z","Action":"pass","Package":"github.com/wxxb789/symmetry/daemon/internal/app","Elapsed":2216.208}
1324 tests passed, 7 skipped, 0 failed
```

The app package is slow but sound under `-race` in this environment; the three slowest tests are
`TestOutputBackpressureRecoversWithOneTruncationMarker` 917.8 s,
`TestRawOutputBudgetDropsWithoutFailingSemanticEvents` 476.6 s and
`TestDurablePiProviderBridgeExecutorReservesCompletionCapacityBeforeDispatch` 143.6 s. The first two
are output/backpressure tests whose timing is dominated by this host under `-race`; the same
`TestRawOutputBudgetDropsWithoutFailingSemanticEvents` is the test the unrelated agent's WSL
`-race ./internal/app/...` timed out in.

Control: both are recorded as pre-existing slow cases in
`docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md` (§ "outcomes over the 30 s cap"):
`TestOutputBackpressureRecoversWithOneTruncationMarker` at 170.05 s and
`TestRawOutputBudgetDropsWithoutFailingSemanticEvents` at 125.79 s in CI, i.e. a race-detector
multiplier on tests this slice does not touch explains the WSL `-race` wall clock. Command D's
`internal/app` timeout was exactly the default 30-minute per-package budget against that runtime;
Command E's 90-minute budget is the same test set and passes.

The Windows `internal/app` run above stays bound to the delivered revision: no `internal/app` source
or test byte and no `internal/harness` production byte changed after it ran (the later edits were
`staged_driver_test.go`, `omp/adapter_test.go`, `codex/adapter_test.go`, this record and the plan;
a package's test files do not enter another package's build).

The CI `daemon_race_tests` job (`go test -race -count=1 -timeout 600s -json ./...` with the seven
OpenCode `--assert` entries and the pi `TestProviderBridgeCloseReleasesBoundPeerFD` assertion in the
ordinary pass) is the PR-side oracle. This record does not claim its result: it is
conductor-verified on the PR, and the two pinned pi assertions are additionally exercised in the
local harness tree runs in §9.

## 13. Documentation reconciliation

- `docs/design/harness.md` names the `Adapter`/`Session` sketch (unchanged) and summarises
  `StartRequest` as "admission identity, local workspace, decoded context, model profile, limits and
  optional validated local resume handle" — a non-exhaustive admission summary that already omitted
  `Invocation`, `ProviderBridge` and `StartProcess`; the added `Grant` does not make any sentence
  false, and the file (not owned by this slice) names no hook, latch or driver. Grep:
  `grep -n "StartRequest\|StagedSession\|Invocation\|staged\|hook" docs/design/harness.md` → lines
  152 and 162 only.
- `docs/protocol-v1.md`, `contracts/**`: zero matches for `StartRequest|StagedSession|harness
  adapter|native handle`; no wire or persisted shape changed.
- `docs/TODO.md`: one dated entry added (§4). No existing entry is closed by this slice.

## 14. Verification limits

- The credentialed native rows and the `linux-native-v2-acceptance` job are not oracles (E8): they
  skip without env and the workflow is dispatch-only. The gate is the harness tree, `internal/app`,
  the WSL race run and the CI daemon race job, whose result this record does not claim.
- `go test -race` is unavailable on this host for the Windows run (no cgo); the WSL run carries it.
- The adapter packages' live tests stay env-gated and were skipped exactly as at base.
