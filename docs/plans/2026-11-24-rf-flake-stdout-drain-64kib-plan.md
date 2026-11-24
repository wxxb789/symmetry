# rf-flake-stdout-drain-64kib — make the >64 KiB drain test deterministic under coverage instrumentation — plan

Base: `9d113eb6` (`origin/main` at dispatch, branch `refactor/rf-flake-stdout-drain-64kib`; this slice
ships the working tree, no commit of its own). Contract: `goal-rf-flake-stdout-drain-64kib.md` (GDD
state root, external to the repository), with the binding conductor dispatch note (2026-11-24: the
strip is allowed only while `GOCOVERDIR` is set in the helper's environment and only for the three
documented coverage-runtime lines, matched as whole lines; widening the match to any `error:` line
must redden the boundary pin) and the 2026-11-22 comment policy over it.

Shape references: the `rf-w6-12` plan/evidence pair
(`docs/plans/2026-11-24-rf-w6-12-wall-clock-ratchets-plan.md`,
`docs/evidence/rf-w6-12-wall-clock-ratchets-2026-11-24.md`) for the plan/evidence sections, the
mutant protocol and the scope proof; `docs/plans/2026-11-21-rf-w2-17-coverage-plane-plan.md`
("Residual risks and known limits") for the ratified `GOCOVERDIR` forwarding and the recorded covmeta
rename race; `docs/evidence/rf-w4-12c-process-recorder-port-2026-11-22.md` §8 for the superseded
"skipped that one test" workaround.

## What lands

| file | change |
| --- | --- |
| `daemon/internal/execution/runner_test.go` | the strip (the three documented lines, whole trailing block, `GOCOVERDIR`-gated, every stripped line logged) and its boundary pin test; the `root-exits-child-holding-stdout` helper mode's readiness handshake (F-b) with `descendantHoldingStdoutInvocation`/`startHelperInvocation` plumbing |
| `daemon/internal/execution/probe_test.go` | the bounded probe row's call site supplies the F-b marker path |
| `daemon/internal/execution/runner_unix_test.go` | the Unix root-exit row's call site supplies the F-b marker path |
| `daemon/internal/execution/runner_windows_test.go` | the Windows root-exit row's call site supplies the F-b marker path |
| `docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md` | this plan |
| `docs/evidence/rf-flake-stdout-drain-64kib-2026-11-24.md` | the evidence record |
| `docs/TODO.md` | one decision note: the resolved residual and the unapplied durable alternative |
| `docs/archive/CLOSED-TODO.md` | the (F-b) tracked item this goal found, moved out of `docs/TODO.md` with its closure paragraph (the repo's done-items convention) |

No production file changes. The mutants below run in a scratch clone, each restored byte-identical
and verified by blob hash against the clone's committed base.

## P1 — verbatim capture and classification (before any change)

Scratch copy of `daemon/` with a capture-only patch to the streams assertion (dump the over-length
bytes, the first non-`e` index, the stderr sha256 and the `GOCOVERDIR` state), then the flake's own
configuration:

`GOMAXPROCS=8 timeout 30m go test -count=20 -timeout 1200s -coverprofile=<scratch>/exec-cov-before.out ./internal/execution/`
(`daemon/`) → exit 1, `coverage: 60.0% of statements`, **8 red iterations of 20**, all eight
`TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB`; the bounded `RunBoundedCommand` rows
(`probe_test.go:21-33`, limits 256/128/1024) did not red in any iteration.

| quantity | value |
| --- | --- |
| captured stderr lengths | 7 × 197240, 1 × 197302 (`want 196731`) |
| over-length bytes | 7 × 509, 1 × 571 |
| first non-`e` byte index | 196732 in all eight (byte 196731 is `e`; the extra text starts exactly at the payload end) |
| over-length content | one physical line, no inner newline, terminated `\n\n`; verbatim with paths elided: `error: coverage meta-data emit failed: writing <merge-dir>\covmeta.5bde9a34b0182dd9672696b24ef5f92c: rename from <merge-dir>\tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c1795525487246531800 failed: rename <tmp> <covmeta>: Access is denied.` |
| `GOCOVERDIR` in the test process (hence forwarded to the helper) | nonempty in all eight |
| stderr sha256 (the eight runs) | `4c6a4ffe…`, `77b3d28f…`, `8bb273c3…`, `8da2b784…`, `991807ad…`, `b64a2861…`, `c10046b3…`, `e22adf37…` (the temp-name nanotime differs per capture) |

Classification (contract Change 1): payload tail + `error: coverage meta-data emit failed: ` starting
at the payload end, every payload byte present and in order before it, no stdout over-length stream,
no other content → **child-side instrumentation diagnostic; continue to F1**. Not drain loss or
duplication: the first non-`e` index is the payload end, not earlier, so the drain delivered the whole
payload in order.

## Decisions the contract leaves open

### D1. Block grammar: a documented line plus its own newline terminators

The runtime prints `fmt.Fprintf(os.Stderr, "error: …: %v\n", err)` where the wrapped error's text can
already end in `\n` (`internal/coverage/cfile/emit.go`: `emitMetaDataFile`'s rename error ends with
`\n`), so the observed block is `<line>\n\n`. The strip therefore accepts a trailing block iff the
stream ends with a newline and every physical line in the block starts with one of the three
documented prefixes; anything else in the block refuses the whole strip (nothing that is not one of
the three lines is ever swallowed). Consequences pinned by the boundary test: `payload + documented
line` with one or two newlines strips to the payload; a documented line with no terminating newline
is not a whole line and stays; a foreign line after a documented line refuses the strip; a shortened
payload stays short.

### D2. Only the stderr stream is drained

The coverage runtime writes its exit-hook diagnostics to `os.Stderr` (toolchain source and P1's
capture). A stdout over-length stream stays a hard failure — the contract's P1 classification names
it as drain-implication evidence.

### D3. The gate reads `GOCOVERDIR` from the helper's `Invocation`

The ruling is "only when `GOCOVERDIR` is set in the helper's environment"; that environment is exactly
`helperInvocation(...).Env` (`minimalEnvironment()` forwards the parent's `GOCOVERDIR` only when set).
Reading it from the invocation makes the gate a pure function of data the test already holds and lets
the pin test it (absent / empty / set) without mutating the process environment; an in-process
`os.Setenv("GOCOVERDIR", …)` would be visible to the package's other parallel tests and could itself
perturb their instrumented children.

### D4. `probe_test.go` stays out of the owned set

The contract owns it only if P1 shows the bounded rows also see the line. The 20-run capture shows
0/20 for those rows (and for every other helper-output row) and 8/20 for the streams row, so the
change stays in `runner_test.go`. Residual recorded (evidence §9 and `docs/TODO.md`): the diagnostic
can in principle land on any instrumented helper child, so a future loss on another row would redden
that row; the durable alternative below is the fix if that ever happens.

### D5. The streams test builds its invocation inline, then starts it through the helper

The strip needs the invocation that produced the child, so the test uses
`invocation := helperInvocation("streams", "196731")` and starts it with
`startHelperInvocation(t, sink, invocation)` — the same `NewRunner().Start` plus `Start() error = %v`
failure the F-b call sites use, so the call is behavior-identical. The start path, payload size, both
exact-length assertions and `requireStrictSequence` are unchanged.

### D6. Boundary pin shape

One non-parallel table test over the pure stripper plus the gate: payload alone; payload plus each of
the three documented lines (one and two newlines); payload plus two documented lines; payload plus a
foreign `error:`-prefixed line; payload plus a documented line then a foreign line; payload plus a
documented line without a terminator; shortened payload plus a documented line; and the gate rows
(helper environment without `GOCOVERDIR`, with it empty, and with it set). "Still fails" is expressed
as "the post-strip length is not 196731", which is what the exact-length assertion checks.

### D7. `docs/TODO.md` gets one decision note, not a tracked item

No entry exists to close (contract Acceptance 6). The note records: the rf-w4-12c §8 "skipped that one
test" workaround is superseded; the race itself (Windows `covmeta` rename TOCTOU between concurrent
instrumented children sharing the tool's merge directory) is untouched; the durable alternative — one
warm-up instrumented child per package, or a private `GOCOVERDIR` per helper — stays unapplied because
it changes the shared helper every test in the package uses (the contract's recorded,
rejected-as-larger alternative). The historical plans/evidence are frozen records and are not edited
(they are outside the owned set).

### D8. (F-b) readiness marker after the write; the conductor's in-scope ruling

The conductor ruling of 2026-11-24 brings the descendant window into scope for this goal: the
`root-exits-child-holding-stdout` mode's fixed 100 ms sleep races the grandchild's first stdout
write against the teardown that starts on root exit. Fix: the grandchild creates a marker file right
after that write, the root polls for it (1 ms interval, bounded by `testHangGuard`) and exits only
then. Rejected: `exec.Cmd.ExtraFiles` for a dedicated signal pipe (documented unsupported on
Windows); a wider or retried sleep (same race, narrower window, and the contract's failure control);
an unbounded root wait (a wedged grandchild would hang instead of failing). The marker path travels
in `GO_RUNNER_DESCENDANT_MARKER` (the `GO_RUNNER_START_MARKER` idiom), the mode exits 2 when it is
absent (fail closed), and the path comes from the consumer test's `t.TempDir()`, so `-count=N` runs
never share a stale marker. The root's wait expiry exits 6 after a stderr note — bounded failure
path, not a widened window. Consumers: `probe_test.go:76`, `runner_unix_test.go:50`,
`runner_windows_test.go:211`, all through `descendantHoldingStdoutInvocation(t)`.

## Verification plan (foreground, bounded, never two heavy suites at once)

1. Red before / green after, the flake's own configuration: the `-count=20 -coverprofile` run above
   (before: 8 red, verbatim capture recorded) and again on the final tree (target 0 red).
2. Plain loaded repeat, the CI shape: the `-count=200 -run '^TestStartDrains…$'` form before and after
   → 0 failures (the plain shape is the `daemon`/`daemon-windows` regression gate).
3. Forced pin in a scratch clone: a helper mode that appends one documented line to the streams
   payload — (a) with the helper's `GOCOVERDIR` forwarded: red before F1, green after F1 with the line
   logged; (b) with `GOCOVERDIR` absent from the helper environment: red before *and* after (the gate);
   (c) a foreign-line child stays red after F1.
4. WSL `-race` on `./internal/execution/` → exit 0, no race report (the Linux oracle).
5. Assertion census: the `runner_test.go` diff deletes no assertion, the payload length (`196731`) and
   `requireStrictSequence` are unchanged, `git diff --check` exits 0.
6. Mutants in a scratch clone committed at the final state, reverted by `git checkout` and verified by
   blob hash: M1 (drop the last read chunk in `runner.go:1661-1683` → streams length short), M2 (the
   strip over-strips from the first non-`e` byte → the pin's foreign-line and short-payload rows red),
   M2b (widen the match to any `error:` line → the pin reddens, the conductor's row), M3 (`streams`
   writes `count-1` stderr bytes → streams length short).
7. (F-b) red-without / green-with, the conductor's row: the two descendant rows and the coverage-mode
   package pass red 7–8/20 (focused) and 8/20, 3/20 (package) on the pre-F-b tree (§3 of the evidence
   record), and the mutant that restores the fixed 100 ms sleep in the scratch clone reddens again;
   the fixed tree is 0/20 in the coverage-mode package pass.
8. `gofmt -l`, `go vet ./...`, `go test -count=1 ./internal/execution/` (plain) on the final tree.

## Risks and out of scope

- The after-gate is a probabilistic claim (8/20 before). A documented line landing on another
  helper-output row would still redden that row (D4 residual); only the streams row was observed.
- A *fourth* coverage-runtime message (for example `error: coverage meta-data prep failed: `) stays
  red by design; the tolerance is exactly the three documented lines.
- Out of scope: `runner.go` and every product file (DECISION_NEEDED if P1 had implicated the drain —
  it did not), `probe_test.go`'s diagnostic-class ownership (D4 — the conductor's F-b ruling makes
  its call site in scope for the marker path only), other tests and packages, `ci.yml` and the
  coverage job, `minimalEnvironment`'s `GOCOVERDIR` forwarding, the warm-up / private-`GOCOVERDIR`
  alternative, timeouts, retries, skips and `-run` exclusions.

## Outcome note (implement stage, 2026-11-24)

- F1 landed as designed: the strip plus its pins; the boundary pin (11 rows), the gate pin (3 rows),
  the four forced pins, mutants M1/M2/M2b/M3 and WSL `-race` all behave as the contract demands; the
  assertion census shows no assertion removed, both payload assertions still exact and
  `requireStrictSequence` untouched.
- The first gate run (F1 tree) exposed the pre-existing descendant window (F-b): the package's
  coverage-mode `-count=20` pass red 8/20 and 3/20 iterations in
  `TestRunBoundedCommandStopsDescendantHoldingOutputPipe`/`TestRootExitDoesNotLeaveADescendantHoldingStdoutOnWindows`,
  while the subject row was 0/20 with seven natural strips logged. The conductor ruling of 2026-11-24
  brought F-b into scope and it is fixed here as D8 (readiness marker instead of the fixed sleep;
  call sites in the three consumer test files).
- Final acceptance on this host: the coverage-mode `-count=20` full-package pass is **exit 0, 0 fail
  events of 20 iterations** (320 s, `coverage: 60.1%`), subject row 20 run / 20 pass with 9 natural
  strips logged; the plain `-count=200` subject repeat is 0 failures (34.473 s); the plain package
  suite is green (26.480 s); WSL `-race` green (29.413 s, 0 `DATA RACE`); gofmt/`go vet`/`git diff
  --check` exit 0. The sleep-restored mutant reddens 6/20 in the pre-fix signature; M1/M2/M2b/M3
  redden in the contract's rows; `<clone-fb>` is clean after the reverts. Full record:
  `docs/evidence/rf-flake-stdout-drain-64kib-2026-11-24.md` §3–§7.
- Apply stage (2026-11-24, simplify findings S1 + S2): the streams test now starts through
  `startHelperInvocation` (S1 — `runner_test.go` 195/4 → 192/4 added/deleted, blob `3a3177b3…`,
  sha256 `7194f81a…`) and D4's pointer reads `evidence §9` (S2). Citations after the edit shift by −3
  (strip log `:60` → `:57`, pin gate row `:127` → `:124`, stdout assertion `:58` → `:55`, stderr
  assertion `:61` → `:58`, `requireStrictSequence` `:63` → `:60`); the gates were re-run on the
  post-apply tree (evidence §7, §10): coverage-mode `-count=20` exit 0 with 9 natural strips, plain
  `-count=200` green, WSL `-race` green, pins green, M1–M3/M2b re-run in the apply clone and M5
  load-driven (0/20 idle, 3/20 loaded), census 192/4 with no assertion weakened, and the duration
  ratchet summarizer PASS on a local single-iteration stream (p95 0.210 s vs the 0.260 s baseline,
  141 tests, none over the cap, none adopted).
- Residual: the covmeta race itself is untouched by design, and CI's `daemon`/`daemon-windows` rows
  remain the operator's oracle for the PR head (this stage does not push).
- Contract-file decision: the goal file in the GDD state root is left in place unchanged (Status
  `queued`), because it is the conductor's control-plane artifact for the following verify stage and
  the dispatch gate refuses `implement` runs against an archived contract. The repo-side close-out —
  plan, evidence record, `docs/TODO.md` note and the `docs/archive/CLOSED-TODO.md` move — is done
  here, and no repo-grammar or stability document is left stale by this change (`ci.yml`, the
  coverage job and the design docs are untouched).
