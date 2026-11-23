# rf-w4-13 wall-clock burn-down part 1 (Go deadlines, TC-F) — plan

Contract: `goal-rf-w4-13-go-wall-clock.md` (GDD state root, external to the repository), with the
program-wide conductor rules, the 2026-11-21 lesson-32 and E5 rulings, the 2026-11-22 comment
policy and the 2026-11-23 lessons 55-56 amendment binding over it. Base revision:
`b9d4b4a4cfff1467ce9bbd24c127107c884fd077`. Branch: `refactor/rf-w4-13-go-wall-clock`. Host:
Windows x64, `go1.27.4`; the Linux rows run Ubuntu-24.04 over the same worktree through its mount
point (`wsl -d Ubuntu-24.04 -u accept`, `go1.27.0`). Windows and WSL suites never run concurrently.

## 1. Scope

Owned (STRICT): `daemon/internal/harness/claude/probe_test.go`,
`daemon/internal/harness/codex/probe_test.go`, `daemon/internal/harness/codex/rpc_session_test.go`,
`daemon/internal/harness/probeconformance/cancellation_test.go`, this plan. No production file, no
`daemon/internal/app/**`, no other harness test file. `docs/TODO.md` stays untouched: the one entry
that names W4-13 (`docs/TODO.md:1095-1106`, containment close-path pins in `internal/execution`) is
not closed by this slice — it needs execution-package coverage this slice does not own.

## 2. What the contract leaves unresolved, and the decisions taken

D1. **Where the tables live.** Clause 2 names `answer.md`; the strict owned set carries no
`docs/evidence/` path. Resolution: the full tables live in this plan file (owned, in-repo,
verifiable from the tree), `answer.md` (the stage report) mirrors the tables as the judging
artifact the contract names, and neither writes a path outside the owned set.

D2. **Import clean-up.** None: the converted `claude/probe_test.go` keeps its `time` import. The
anticipated drop did not happen — the two `time.After(testHangGuard)` hang guards of the converted
test stay in place (`:140`/`:152`), now reading the synctest bubble's virtual clock. The other
three files likewise keep live `time.` uses (`time.After` in unconverted rows, `probeTimeout`
duration constants, `time.Now()` event stamps).

D3. **`synctest` callback shape.** `synctest.Test(t, func(t *testing.T) { … })`: the callback
parameter shadows the outer `t`, so every assertion inside keeps its exact text. The wrapper sits
*inside* the innermost `t.Run` body (never around the outer loop), so names, subtest names and the
loop structure are unchanged.

D4. **Close-row shapes.** Both codex rows use the `adapter_test.go:996` pattern: `Close` in a
goroutine, `process.nextRequest(t)` for the interrupt, the new `Method`/`ThreadID`/`TurnID`
assertions, `process.reply`, then the nil close result. The `_ = ` discard of the close error is
replaced by the assertion the contract demands; the tolerated-interrupt-timeout behaviour stays
pinned by `adapter_test.go:972`.

## 3. Conversions (one at a time, in this order)

| # | test | file:line (base) | conversion |
| --- | --- | --- | --- |
| C1 | `TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation` | `claude/probe_test.go:112` | `synctest.Test` inside each of the two `t.Run` bodies |
| C2 | `TestClaudeProbeRejectsSuccessAfterProbeDeadline` | `claude/probe_test.go:157` | whole body in `synctest.Test` |
| C3 | `TestProbeBoundsHangingRunnerAndPreservesCancellation` | `probeconformance/cancellation_test.go:88` | `synctest.Test` inside the innermost `t.Run` body (3 providers × 3 rows) |
| C4 | `TestProbeBoundsHangingSchemaRunner` | `codex/probe_test.go:252` | whole body in `synctest.Test` |
| C5 | `TestDuplicatePreTurnNotificationsDeduplicateBeforeStageCapacity` | `codex/rpc_session_test.go:265` | serve the close interrupt, assert method/ids, assert nil close |
| C6 | `TestNativeSinkEventsDoNotExposeRawProtocolIdentityOrFrames` | `codex/rpc_session_test.go:701` | same as C5 |

(C5/C6 are one disposition class — "serve the interrupt" — applied to two tests.)

## 4. Control-flow preservation (lesson 32)

Per test: what the base waited on, the early-return/short-circuit paths around it, and the HEAD
equivalent.

- C1: base waits (a) `runner.started` (a real channel close by the injected runner, no timer) and
  (b) `probeCapabilities` return, bounded by the caller's 5 s `probeTimeout` or the caller's
  cancellation; the `time.After(testHangGuard)` arms only turn a hang into a failure. No early
  return sits between them: the harness computes `--version` first, so the runner start always
  precedes the bound. HEAD: both waits keep their identity; the 5 s bound becomes the bubble's
  virtual timer; cancellation still comes from the test goroutine. Preserved: yes. The failed arm
  (`t.Fatalf` on a guard expiry) stays.
- C2: base waits for `successAfterDeadlineRunner` to answer after `<-ctx.Done()`, i.e. the 5 s
  virtual bound elapses; the assertion is the late-success rejection, not the duration. HEAD:
  identical control flow, virtual bound. Preserved: yes.
- C3: rows are (i) local timeout — provider bound (1 s codex, 1 s opencode, 5 s pi) — (ii) caller
  earlier deadline — the custom context reports a past deadline, so the derived bound fires
  immediately and the runner is called before it — (iii) caller cancellation — `cancel()` after
  `runner.started`. Same two waits as C1, per provider; HEAD keeps all three rows, all three bounds
  become virtual. Preserved: yes.
- C4: base waits for the codex schema bound (1 s) to end the hanging schema runner; guard arms
  unchanged. HEAD: virtual. Preserved: yes.
- C5/C6: base closes the session at teardown with an unanswered `turn/interrupt`, spending the real
  5 s `nativeInterruptTimeout` inside `closeOnce`, and discards the close error. HEAD: the same
  `closeOnce` path runs; the interrupt is read, its method and both IDs asserted (neither was
  asserted before), answered, and the close result asserted nil. The tolerant
  terminate-despite-interrupt-timeout branch is not deleted — `adapter_test.go:972` still pins it.
  Preserved: yes (the test now asserts more, not less).

## 5. Baseline re-measurement (dispatch head `b9d4b4a4`, list of record)

One foreground run, `go test -count=1 -json ./internal/harness/ ./internal/harness/claude/
./internal/harness/codex/ ./internal/harness/probeconformance/ ./internal/app/` in `daemon/`,
2026-11-23T11:13:02Z → 11:17:45Z (4 m 42.8 s), Go 1.27.4 Windows x64. Package totals:
`internal/app` 269.27 s (fail: the pre-existing flake below), `internal/harness/codex` 16.46 s,
`internal/harness/claude` 15.58 s, `internal/harness/probeconformance` 9.94 s,
`internal/harness` 3.14 s.

### 5.1 The five converted rows

| s (base) | test | file:line at `b9d4b4a4` | cause | bounding constant/seam | disposition |
| --- | --- | --- | --- | --- | --- |
| 5.00 (+5.00 `local_timeout`) | `TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation` | `claude/probe_test.go:112` | the caller-context row waits out the provider bound for a hanging in-process runner | `claude/probe.go:29` `probeTimeout = 5s` via `harness.RunBoundedProbe` (`bounded_probe.go:27`) | convert (synctest) |
| 5.00 | `TestClaudeProbeRejectsSuccessAfterProbeDeadline` | `claude/probe_test.go:157` | same bound; the late success must be discarded | same | convert (synctest) |
| 7.00 (5.00 `pi/local_timeout`, 1.00 `codex/local_timeout`, 1.00 `opencode/local_timeout`) | `TestProbeBoundsHangingRunnerAndPreservesCancellation` | `probeconformance/cancellation_test.go:88` | three provider bounds waited for real, one per provider row | `pi/probe.go:50` 5 s, `codex/probe.go:23` 1 s, `opencode/adapter.go:31` 1 s, each via `RunBoundedProbe` | convert (synctest) |
| 5.02 | `TestDuplicatePreTurnNotificationsDeduplicateBeforeStageCapacity` | `codex/rpc_session_test.go:265` | teardown leaves `turn/interrupt` unanswered → real 5 s `nativeInterruptTimeout`; the close error is discarded | `codex/adapter.go:38` `nativeInterruptTimeout = 5s` at `:716` | convert (serve the interrupt) |
| 5.01 | `TestNativeSinkEventsDoNotExposeRawProtocolIdentityOrFrames` | `codex/rpc_session_test.go:701` | same | same | convert (serve the interrupt) |
| 1.00 | `TestProbeBoundsHangingSchemaRunner` | `codex/probe_test.go:252` | codex schema bound waited for real | `codex/probe.go:23` 1 s via `RunBoundedProbe` | convert (synctest) |

### 5.2 Out-of-scope / recorded rows

| s (base) | test | file:line at `b9d4b4a4` | cause | bounding constant/seam | disposition |
| --- | --- | --- | --- | --- | --- |
| 21.61 (15.41 `request_deadline`, 2.33 `caller_deadline`, 2.14 `admission_deadline`) | `TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget` | `app/omo_attach_ack_test.go:602` (`:611` deadline, `:626` `afterPut`, real `httptest.NewServer` `:138`) | request mode bounded by the production control limit | `app/app.go:38` `controlRequestLimit = 15s` via `controlContext` | out of scope (production-bound) per E5 |
| 15.56 | `TestOutputBackpressureRecoversWithOneTruncationMarker` | `app/app_test.go:4680` | 5 MiB queue+journal throughput; no timer in the body | — | record only; file outside the owned set |
| 14.15 | `TestRawOutputBudgetDropsWithoutFailingSemanticEvents` | `app/app_test.go:4641` | same | — | record only; file outside the owned set |
| 2.31 | `TestClaudeLiveSmokeSubjectDigestComesFromTheCommit` | `claude/` | credential-gated live smoke (E8: census row, never an oracle) | — | record only |
| 0.37-0.40 | `TestAdapterStagesNativeTransportAndRequiresMatchingStructuredCompletion`, `TestClaudeCandidateProcessExitAfterTerminalBindsFinalOutcome` | `codex/adapter_test.go`, `claude/adapter_test.go` | below the ≥4.9 s class target | — | record only, left to W6-12 |

### 5.3 Pre-existing flake found in the baseline (not this slice's file)

The baseline co-run is red in `internal/harness` (exit 1):
`TestStagedDriverCloseDetachedRunsStartOnTheCaller` fails at `staged_driver_test.go:449` with
`a departed caller reports its own cancellation: <nil>`; package elapsed 3.144 s. The row is in
`daemon/internal/harness/staged_driver_test.go` (W4-15b, `b6c7c383`), which this slice does not own
and does not edit. Cause, from the code: `CloseGate.RunDetached` (`close_gate.go:78`) ends in
`waitCloseAttempt`, a two-arm `select` over `entry.done` and `ctx.Done()` (`close_gate.go:120-125`).
The test cancels the caller context before the call, so `ctx.Done()` is already closed; on a loaded
host the detached goroutine can close `entry.done` before the caller reaches the `select`, both arms
become ready, and Go picks one at random — a nil close result instead of `context.Canceled`.
Evidence: pristine tree at `b9d4b4a4` (no slice edit), failing in the co-run, and 8/8 green when the
same test runs alone (`go test -count=1 -run 'TestStagedDriverCloseDetachedRunsStartOnTheCaller$'
./internal/harness/`, 2.6-3.3 s each). W4-15b's own evidence records only a different,
deterministic mutant for this test (`docs/evidence/rf-w4-15b-staged-driver-2026-11-22.md:270`, red at
its `:367`). Reported as a finding; not fixed here (not-owned file, and the gate may not be weakened
by exclusion).

## 6. Measured results at HEAD

### 6.1 Converted rows, base → HEAD (same command shape, `-count=1`)

| test | package | base s | HEAD s |
| --- | --- | --- | --- |
| `TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation` (`local_timeout`) | claude | 5.00 | 0.00 |
| `TestClaudeProbeRejectsSuccessAfterProbeDeadline` | claude | 5.00 | 0.00 |
| `TestProbeBoundsHangingRunnerAndPreservesCancellation` (`codex`/`opencode`/`pi` `local_timeout`) | probeconformance | 7.00 (1+1+5) | 0.00 |
| `TestDuplicatePreTurnNotificationsDeduplicateBeforeStageCapacity` | codex | 5.02 | 0.04 |
| `TestNativeSinkEventsDoNotExposeRawProtocolIdentityOrFrames` | codex | 5.01 | 0.02 |
| `TestProbeBoundsHangingSchemaRunner` | codex | 1.00 | 0.00 |

Every name, subtest name and assertion survives: the `-v` runs show the same 2 claude rows, the 12
probeconformance subtests (`codex`/`opencode`/`pi` × `local_timeout`/`caller_earlier_deadline`/
`caller_cancellation`) and the two codex close rows.

### 6.2 Package totals

| package | base s | HEAD s |
| --- | --- | --- |
| `internal/app` | 269.27 | 236.84 |
| `internal/harness/codex` | 16.46 | 4.33 |
| `internal/harness/claude` | 15.58 | 4.20 |
| `internal/harness/probeconformance` | 9.94 | 3.08 |
| `internal/harness` | 3.14 (red row, §5.3) | 2.12 |

Whole co-run: base 4 m 42.8 s → HEAD 4 m 00.5 s (both including the unchanged `internal/app`
suite). The four harness package totals drop by 26.7 s; the difference between that and the
co-run's 42.3 s comes from the shorter tail of the parallel co-run.

### 6.3 Co-run gate result

- Run 1 (`after.json`): exit 1 — `internal/harness` green, `internal/app` red on the pre-existing
  load-sensitive row `TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline`
  (5.28 s, `omo_attach_ack_test.go:629: attachment requests=[], want [GET PUT]`, the GET failing
  with `context deadline exceeded` before the recorded attachment list was populated). Not-owned
  file; the same row was green in the baseline co-run and in the targeted re-run (§6.4).
- Run 2 (recorded re-run on the settled tree, `after2.json`): exit 0, 4 m 00.5 s; this is the
  after-table used above.

### 6.4 Targeted gates

- E5 row re-run: `go test -count=1 -v -run
  'TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget' ./internal/app/` → exit 0,
  21.49 s (`request_deadline` 15.86 s, `caller_deadline` 2.16 s, `admission_deadline` 2.57 s,
  `caller_cancel` 0.90 s) — untouched, still bounded by the production 15 s limit.
- Loaded repeat: `GOMAXPROCS=8 go test -count=200 -run '<the six names>'
  ./internal/harness/claude/ ./internal/harness/codex/ ./internal/harness/probeconformance/` →
  exit 0, 19.68 s (`claude` 3.143 s, `codex` 14.963 s, `probeconformance` 3.290 s); 1 200 test
  executions, all subtests included.
- WSL `-race` (`wsl -d Ubuntu-24.04 -u accept`, Go 1.27.0, the four packages, `-count=1`):
  HEAD exit 0 — `harness` 1.186 s, `claude` 16.817 s, `codex` 23.781 s, `probeconformance`
  1.146 s; pristine base export (`git archive b9d4b4a4 daemon` into a scratch dir outside the
  repo, `synctest` count 0) exit 0 — `harness` 1.132 s, `claude` 22.954 s, `codex` 31.893 s,
  `probeconformance` 8.091 s. Report-only delta: −21.14 s of package time (−33 %), same direction
  as the contract's scratch (−33 s on three packages); absolute values differ by host load, both
  runs measured back-to-back with the identical command.
- Windows platform gate: no file in the owned set carries a build tag (`grep -l "go:build\|+build"`
  empty), so the Windows co-run is the platform gate.

## 7. Mutants (red, then restored green)

Every row: mutant applied with `apply_patch` on the real tree, targeted tests run, mutant reverted,
and the file's blob hash compared with `git rev-parse b9d4b4a4:<path>`. `bounded_probe.go` restored
hash `ad3bdd9488da1e16d37ebb72b184ca0759dc3322`, `adapter.go` `0d0d85856a9210fdccc60b4e774430d42b783846`
— both equal to the base blobs; the final tree has 0 diff lines in every production file.

| id | mutation | rows expected | observed | restore |
| --- | --- | --- | --- | --- |
| M1 | `bounded_probe.go:27` `context.WithTimeout` → `context.WithCancel` | claude `local_timeout`, codex schema, probeconformance `local_timeout` | red in 0.00 s: claude `local_timeout` FAIL + `panic: deadlock: main bubble goroutine has exited but blocked goroutines remain`; codex `probe_test.go:275: Probe() did not return after its own 1s bound` + panic; probeconformance `cancellation_test.go:127` same + panic | byte-identical |
| M2 | delete `bounded_probe.go:33-35` (`probeContext.Err()` rejection) | claude `TestClaudeProbeRejectsSuccessAfterProbeDeadline` | red in 0.00 s: `probe_test.go:164: probeCapabilities() error = native harness behavior is unverified: … want context.DeadlineExceeded` | byte-identical |
| M5 (2 hunks, one behaviour: the probe ignores the caller) | `bounded_probe.go:27` → `context.WithTimeout(context.WithoutCancel(ctx), timeout)` **and** delete the parent `ctx.Err()` post-check (`:30-32`) | the four `caller_cancellation` arms | red in 0.00 s: claude `probe_test.go:150` and probeconformance `cancellation_test.go:130` × codex/opencode/pi: `probe error = context deadline exceeded, want context canceled` | byte-identical |
| M3 | `adapter.go:717` interrupt turn ID → `"turn-0"` | both close rows | red in 0.04 s / 0.02 s on `Close() native interrupt params = … TurnID:turn-0, want thread-1/turn-1` | byte-identical |
| M4 | `adapter.go:705` `active := false` (no interrupt attempt) | both close rows | red in 30.02 s each: `timed out waiting for native request` (slow red at the 30 s guard, as the contract predicted) | byte-identical |
| M6 | `adapter.go:743` `return nil` → `return errors.New("mutant: refused a clean stop")` | both close rows (the assertion the conversion adds) | red in 0.03 s / 0.01 s: `Close() error = mutant: refused a clean stop, want known stopped after the served interrupt` | byte-identical |
| M7 | `cancel_state.go:333` `appServerMethodTurnInterrupt` → `appServerMethodTurnStart` | both close rows | red in 0.02 s / 0.02 s at the helper call sites (`rpc_session_test.go:301`/`:742`): `Close() native request = {…Method:turn/start…}, want turn/interrupt`; same production path also red fast: `adapter_test.go:982` `TestCloseTimesOutNativeInterruptThenTerminatesProcess` 0.02 s, `adapter_test.go:1004` `TestCloseAttemptsNativeInterruptThenUsesProcessTermination` 0.01 s, `cancel_test.go:60` `TestControlCancelAwaitsTerminalInterruptedTurn` 0.01 s | byte-identical |

M5 exists because M1 does not red the `caller_cancellation` arms: `context.WithCancel(ctx)` still
propagates the caller's cancellation, and the parent `ctx.Err()` post-check reports it either way.
M6 exists because the nil-close assertion is the branch this slice adds (lessons 55-56). M7
completes the added-guard rows for C5/C6 — the `Method` comparison had none of its own — and the
helper's `t.Helper()` keeps its failures reported at the call site (`rpc_session_test.go:301`/`:742`).
The recorded trap stands: dropping only the interrupt reply does not red the close rows, because
`closeOnce` tolerates the interrupt timeout (`adapter.go:740-744`, pinned by
`adapter_test.go:972`) — the request assertion is the guard, and M3/M4/M6/M7 each exercise it.

## 8. Report-only rows (D2)

- Test-LOC delta: +28 net lines over the four owned test files (`numstat`: claude +36/−31,
  codex probe +28/−25, codex rpc_session +19/−2, probeconformance +29/−26). No test, name or
  assertion is deleted: the conversions add wrapper indentation and the close-row assertions, and
  the close-row orchestration is shared through one helper (`closeServingInterrupt`); the −150
  target belongs to W6-12's poll conversions, and this slice reports the deviation instead of
  hiding it.
- CI race-job delta: not measurable in this stage (the stage must not push). The conductor
  measures it on the PR with the `daemon_race_tests` step (`go test -race -count=1 -timeout 600s
  -json ./...` plus `scripts/ci/go-test-durations.py`, `.github/workflows/ci.yml:1114-1129`). The
  local proxy above is −21.1 s on the four touched packages, inside the plan's −40…−90 s band per
  package-set direction; the full-suite PR figure is the CI measurement of record.

## 9. Findings (pre-existing, not fixed here)

1. `daemon/internal/harness/staged_driver_test.go:449` (`TestStagedDriverCloseDetachedRunsStartOnTheCaller`,
   W4-15b `b6c7c383`): load-dependent nondeterminism, §5.3. Not-owned file.
2. `daemon/internal/app/omo_attach_ack_test.go:629`
   (`TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget/admission_deadline`): red in
   the first HEAD co-run under parallel-package load, green in the baseline co-run, the targeted
   run and the re-run. Not-owned file; the fixture's deadline tolerance is what moves.
3. `daemon/internal/harness/claude/probe_test.go:26` carries a `C:\Users\runneradmin\…` fixture
   string (a fictional Windows-runner path, present at `b9d4b4a4`). The diff adds no
   backslash-before-`u` line and no `users/` path; the pre-existing line is reported, not edited.

## 10. Evidence plan

1. Re-measured table at the dispatch head (`go test -count=1 -json` over the four harness packages
   plus `./internal/app/`), per test: name, package, duration, cause, bounding constant/seam,
   disposition; the E5 rows recorded with their production bound and never converted.
2. After table (same command, HEAD tree) with per-test and per-package rows.
3. Loaded repeat: `GOMAXPROCS=8 go test -count=200 -run '<the six names>'` over
   `claude`, `codex`, `probeconformance`.
4. WSL `-race` on the four touched packages, base vs HEAD.
5. Mutants (red, then restored green, each byte-verified restored):
   M1 `bounded_probe.go:27` `context.WithTimeout` → `context.WithCancel`;
   M2 delete the `probeContext.Err()` rejection (`:33-35`);
   M3 mutate the close interrupt's turn ID (`adapter.go:717`);
   M4 short-circuit the close interrupt attempt (`adapter.go:705` `active := false`, slow red at the
   30 s guard).
   Plus one row per branch the change relies on (lessons 55-56): the `cancelAfterStart` arms are the
   rows the M1 deadlock shows.
6. Foreground gates: `gofmt -l`, `go vet ./...`, `go build ./...`, the five-package co-run,
   the E5 targeted re-run, `git diff --check`, DLP grep.
7. Report-only, D2: test-LOC delta; the CI race-job delta is measured on the PR by the conductor —
   this stage records the local WSL race figures (base vs HEAD) as the proxy and does not push.

## 11. Risks and out-of-scope

- `synctest` around a real subprocess/socket/filesystem wait is a failure-control condition; all
  five converted bodies wait only on in-process doubles (verified per test above) — C3's row (ii)
  uses a hand-written context type, not a timer.
- The `nativeInterruptTimeout` tolerance itself is not converted or weakened; `adapter_test.go:972`
  keeps that contract.
- Out of scope: `internal/app/**` (E5 rows), the two `output_settlement` throughput rows (22.5 s /
  19.1 s, file outside the owned set — recorded only), e2e sleeps (dispatch-only, W6-12), the
  opencode/native-acceptance tool budgets, every production sleep/timeout, any fake-clock framework.
