# rf-flake-stdout-drain-64kib — evidence record

Base `9d113eb6` (`origin/main` at dispatch; branch `refactor/rf-flake-stdout-drain-64kib`, working
tree only — this slice makes no commit). Contract: `goal-rf-flake-stdout-drain-64kib.md` (GDD state
root, external to the repository) with the binding conductor dispatch note (2026-11-24) and the
conductor ruling of 2026-11-24 that brings the descendant window (F-b) into scope. Plan:
`docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md` (holds the P1 verbatim capture, the
decisions D1–D8, the verification plan). Shape references: the `rf-w6-12` plan/evidence pair
(`docs/plans/2026-11-24-rf-w6-12-wall-clock-ratchets-plan.md`,
`docs/evidence/rf-w6-12-wall-clock-ratchets-2026-11-24.md`) for the gates/mutants/scope sections;
`docs/plans/2026-11-21-rf-w2-17-coverage-plane-plan.md` ("Residual risks and known limits") for the
ratified `GOCOVERDIR` forwarding and the recorded covmeta rename race;
`docs/evidence/rf-w4-12c-process-recorder-port-2026-11-22.md` §8 for the superseded "skipped that one
test" workaround.

Scratch roots (never committed, all under one host scratch directory): `<scratch>` = a copy of
`daemon/` with a capture-only patch (`base/`, pre-F1); `<clone>` = the F1-stage `git clone --shared`
whose clone-local HEAD was `15a27066`; `<clone-fb>` = this stage's `git clone --shared` of the
worktree, clone-local HEAD `4a45cda9` ("scratch: F1 strip + F-b handshake") whose four test-file blobs
equal the shipped tree's (`7955bf8b…` `runner_test.go`, `346bedd4…` `probe_test.go`, `3e4d4a44…`
`runner_unix_test.go`, `e265a770…` `runner_windows_test.go`; runner.go at `bd677058…`). Every command
ran foreground (bash tool `pty:true`, `timeout:0` — the harness reported pty unavailable and ran
without a terminal; the commands were still bounded with `timeout` and no job was left running) and
the two heavy configurations never ran concurrently with each other or with another suite. Windows
x64, `go1.27.4 windows/amd64`; WSL Ubuntu-24.04, `go1.27.0 linux/amd64`.

## 1. What changed

| file | numstat (add/del) | change |
| --- | --- | --- |
| `daemon/internal/execution/runner_test.go` | 195 / 4 | the strip (three documented lines, whole trailing block, helper-`GOCOVERDIR`-gated, every stripped line logged), the two pin tests, and the F-b readiness handshake (root waits for the grandchild's marker; `descendantHoldingStdoutInvocation`/`startHelperInvocation` plumbing) |
| `daemon/internal/execution/probe_test.go` | 1 / 1 | the bounded probe row supplies the F-b marker path |
| `daemon/internal/execution/runner_unix_test.go` | 1 / 1 | the Unix root-exit row supplies the F-b marker path |
| `daemon/internal/execution/runner_windows_test.go` | 1 / 1 | the Windows root-exit row supplies the F-b marker path |
| `docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md` | new | the plan (P1 capture, decisions D1–D8, verification plan, outcome note) |
| `docs/evidence/rf-flake-stdout-drain-64kib-2026-11-24.md` | new | this record |
| `docs/TODO.md` | 27 / 0 | one decision note: the resolved residual, the unapplied durable alternative |
| `docs/archive/CLOSED-TODO.md` | 29 / 0 | the (F-b) tracked item this goal found, moved out of `docs/TODO.md` with its closure paragraph (the repo's done-items convention) |

No production file change. The only product-tree edits were the mutants in §6, applied in
`<clone>`/`<clone-fb>` and reverted by `git checkout` with blob hashes compared. Shipped hashes at
the gate runs: `runner_test.go` blob `7955bf8bafdb0ce79d5130f4a541394d6deec160`, sha256
`58f4b91f02d1434f132badd3e2002b499a2652e0f4c709100154b0fc6b5727c8`; `probe_test.go` sha256
`e9bf53dda61343bc009c56122f8778b4d805f12ef97b12f6d78a11da2b74f2bf`; `runner_unix_test.go` sha256
`e965c169c02659c69eb243ddd67e5fa1ff1a9e09536f043102439c6ab8b657c9`; `runner_windows_test.go` sha256
`d457cef9e45d14deb83adae3b5a0a029077c39d7bdc3c26dc5602ad298f8f8dd`.

## 2. P1 — the capture and its classification

`<scratch>/base` (pre-F1, capture-only patch to the streams assertion) under the flake's own
configuration:

`GOMAXPROCS=8 timeout 30m go test -count=20 -timeout 1200s -coverprofile=<scratch>/exec-cov-before.out ./internal/execution/`
(`daemon/`) → exit 1, 299.135 s, `coverage: 60.0% of statements`, **8 red iterations of 20**, all
eight `TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB`; the bounded `RunBoundedCommand` rows and
every other helper-output row were green in all 20 iterations.

| quantity | value |
| --- | --- |
| captured stderr lengths | 7 × 197240, 1 × 197302 (`want 196731`) |
| over-length bytes | 7 × 509, 1 × 571 |
| first non-`e` byte index | 196732 in all eight (byte 196731 is `e`: the extra text starts exactly at the payload end) |
| over-length content | one physical line, no inner newline, terminated `\n\n`; verbatim, paths elided: `error: coverage meta-data emit failed: writing <merge-dir>\covmeta.5bde9a34b0182dd9672696b24ef5f92c: rename from <merge-dir>\tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c1795525487246531800 failed: rename <tmp> <covmeta>: Access is denied.` |
| `GOCOVERDIR` in the test process (hence in the helper environment) | nonempty in all eight |
| stderr sha256 (the eight captures) | `4c6a4ffe…`, `77b3d28f…`, `8bb273c3…`, `8da2b784…`, `991807ad…`, `b64a2861…`, `c10046b3…`, `e22adf37…` (differ by the temp-name nanotime) |

Contract Change 1 classification: payload tail plus `error: coverage meta-data emit failed: `
starting at the payload end, every payload byte present and in order before it, no stdout over-length
stream, no non-diagnostic content → **child-side instrumentation diagnostic; continue to F1**. Not
drain loss or duplication: the first non-`e` index is the payload end, so the drain delivered
the whole payload, in order, before the child's own exit-hook diagnostic.

## 3. The second flake (F-b): found by the after-gate, ruled into scope, fixed

### 3.1 Found

The F1 tree's first gate runs (`<clone>`): `GOMAXPROCS=8 timeout 30m go test -json -count=20
-timeout 1200s -coverprofile=… ./internal/execution/` (`daemon/`) → exit 1 twice: **8 red of 20**
(532.168 s) and **3 red of 20** (434.744 s). Every red was one of two rows and **none** was the
subject row (0/20 in both, with 7 natural strips logged):

| red row | iterations | message |
| --- | ---: | --- |
| `TestRunBoundedCommandStopsDescendantHoldingOutputPipe` (`probe_test.go:83`) | 6 total | `child PID output "" is not an integer` — the descendant's stdout never arrived |
| `TestRootExitDoesNotLeaveADescendantHoldingStdoutOnWindows` (`runner_windows_test.go:218`) | 5 total | `helper child PID was not observed before the 30s hang guard expired` |

Both rows exercise the same helper mode, `root-exits-child-holding-stdout`
(`runner_test.go`): the root spawns a grandchild, **`time.Sleep(100 * time.Millisecond)`**, and exits;
the runner tears the containment tree down on root exit, so the grandchild's first write was
delivered only if it happened inside that fixed window. Not this goal's mechanism:

| observation | command | result |
| --- | --- | --- |
| red without F1 | `GOMAXPROCS=8 timeout 25m go test -count=20 -timeout 900s -coverprofile=… -run '^(TestRunBoundedCommandStopsDescendantHoldingOutputPipe\|TestRootExitDoesNotLeaveADescendantHoldingStdoutOnWindows)$' ./internal/execution/` (`<scratch>/base`, pre-F1) | exit 1, **7 red / 20** (3 × `probe_test.go:83` empty output, 4 × the 30 s hang guard) |
| red without coverage instrumentation | the same `-run` form **without** `-coverprofile` (shipped tree) | exit 1, **8 red / 20** (3 × empty output, 5 × hang guard) |
| the fixed window vs this host's child latency | `execution-plain.test -test.run='^$'` ×10 and `execution-cover.test -test.run='^$'` ×10, spawn+exit wall time (`<clone>`-built binaries) | plain 109–756 ms (median 131), instrumented 117–653 ms (median 134) — the grandchild's own startup already exceeds the 100 ms window on most spawns under this host's load |
| mechanism diagnostic (prior stage, M4) | the `<clone>` with the mode's fixed window widened to 3 s | the two rows pass 20/20 (168.636 s): the missing-output signature is the fixed window, not F1, coverage or the drain |

### 3.2 Fixed (conductor ruling, 2026-11-24; plan D8)

The helper mode now waits for a readiness marker instead of sleeping: the grandchild
(`tree-child` mode) creates the marker file right after its first stdout write
(`signalDescendantStdoutWritten`), and the root (`root-exits-child-holding-stdout`) polls for it
(1 ms interval, bounded by `testHangGuard`) and exits only then (`waitForDescendantMarker`). The
marker path travels in `GO_RUNNER_DESCENDANT_MARKER` (the `GO_RUNNER_START_MARKER` idiom); the mode
exits 2 when it is absent (fail closed); the path comes from the consumer test's `t.TempDir()`, so
`-count=N` runs cannot share a stale marker. All three consumers supply it via
`descendantHoldingStdoutInvocation(t)` (`probe_test.go:76`, `runner_unix_test.go:50`,
`runner_windows_test.go:211`); `runner_test.go`'s `startHelper` now delegates to
`startHelperInvocation`. No timeout was raised, no retry, no skip, `ci.yml` untouched.

Green at the final tree: focused pair plain 5 × (both descendant rows + the tree-parent row) =
`ok 59.287s`; focused pair with `-coverprofile` `-count=5` = `ok 25.161s`; the acceptance gate (4.1)
is 0/20 red package-wide.

### 3.3 The mutant that restores the window (conductor's row)

`<clone-fb>` with `waitForDescendantMarker(marker)` replaced by `time.Sleep(100 * time.Millisecond)`
(everything else at the final tree):

`GOMAXPROCS=8 timeout 30m go test -count=20 -timeout 1200s -coverprofile=<scratch>/m5-focused.out -run '^(TestRunBoundedCommandStopsDescendantHoldingOutputPipe|TestRootExitDoesNotLeaveADescendantHoldingStdoutOnWindows)$' ./internal/execution/`
(`<clone-fb>/daemon/`) → exit 1, 186 s, **6 red iterations of 20**: 4 ×
`probe_test.go:83: child PID output "" is not an integer: strconv.Atoi: parsing "": invalid syntax`
and 2 × `runner_windows_test.go:218: helper child PID was not observed before the 30s hang guard
expired` — the pre-fix signature (the prior stage recorded 7–8/20 for the same configuration; the
rate is load-driven). Reverted (`git checkout --`), blob `7955bf8b…` restored, clone status clean.

## 4. The strip, the pins, and the natural races

The acceptance run (§7 row 1, the final tree, `-json` so passing tests' `t.Logf` lines are recorded):

| quantity | value |
| --- | ---: |
| subject-row `run` events / `pass` events | 20 / 20 |
| `fail` events, package-wide | 0 |
| `skip` events | 0 |
| `stripped coverage runtime diagnostic` log events, all | 29 |
| — on the shipped streams row (`runner_test.go:60`, the real diagnostic) | 9 |
| — on the boundary pin's gate row (`runner_test.go:127`, the synthesized line) | 20 |

So the goal's flake is fixed and *exercised*, not merely silenced: the race that produced §2's eight
reds occurred nine times in this run, the strip removed exactly the documented block each time, and
the exact-length assertions passed. Verbatim (one of the nine, paths elided):
`runner_test.go:60: stripped coverage runtime diagnostic (507 bytes): error: coverage meta-data emit
failed: writing <merge-dir>\covmeta.5bde9a34b0182dd9672696b24ef5f92c: rename from
<merge-dir>\tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c<nanotime> failed: rename <tmp> <covmeta>:
Access is denied.` Before → after on the subject row: **8/20 red → 0/20 red** over the gate runs
(§2, §3.1, §7 row 1), with every tolerated diagnostic logged.

## 5. Forced pins (contract Gates)

The F1 rows were run at the F1 tree by the prior stage (kept below); the after-F1 rows were re-run at
the final tree in `<clone-fb>` with a forced child (one extra stderr write appended to the `streams`
mode — never in the shipped tree, which carries no forced mode).

| row | command (cwd) | exit | observed |
| --- | --- | ---: | --- |
| boundary pin + gate pin, shipped tree | `go test -count=1 -timeout 120s -run '^TestCoverageRuntimeDiagnosticStrip' -v ./internal/execution/` (`daemon/`) | 0 | 11 boundary subtests and 3 gate subtests pass |
| forced documented line, **before F1** | `<scratch>/base-forced` (pre-F1 + the line appended to the `streams` mode): `GOMAXPROCS=8 timeout 10m go test -count=1 -timeout 300s -coverprofile=… -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` | 1 | `P1 stderr length = 196854, want 196731; first non-e byte index = 196732; over-length bytes = "error: coverage meta-data emit failed: rename <merge-dir>\\tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c: Access is denied.\n\n"; GOCOVERDIR nonempty = true` |
| forced documented line, after F1, coverage mode (final tree) | `<clone-fb>` + the line: `GOMAXPROCS=8 timeout 10m go test -count=1 -timeout 300s -v -coverprofile=… -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` | 0 | PASS; `runner_test.go:60: stripped coverage runtime diagnostic (121 bytes): error: coverage meta-data emit failed: rename <merge-dir>\tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c: Access is denied.` |
| forced documented line, **`GOCOVERDIR` not forwarded**, plain mode (final tree) | the same forced child without `-coverprofile` (shell `GOCOVERDIR` empty, `minimalEnvironment` forwards nothing) | 1 | `runner_test.go:61: stderr length = 196854, want 196731` — the strip is closed without a forwarded `GOCOVERDIR` (the conductor's gate) |
| forced **foreign** line, after F1, coverage mode (final tree) | `<clone-fb>` + `error: unexpected forced diagnostic\n\n` appended | 1 | `runner_test.go:61: stderr length = 196768, want 196731` — a foreign line stays red |

## 6. Mutants (all in a clone, reverted by `git checkout`)

`<clone>`'s HEAD was the F1-stage clone-local commit `15a27066`; `<clone-fb>`'s is `4a45cda9` (§
header). Every row below was applied, run, reverted with `git checkout --`, and re-hashed to the
listed blob before the next row.

| id | mutation | command | exit | observed |
| --- | --- | --- | ---: | --- |
| M1 | `runner.go` `readOutput` holds each chunk and never enqueues the final one (no drain of the tail) | `go test -count=1 -timeout 300s -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` (`<clone-fb>`) | 1 | `stdout length = 196608, want 196731` — the lost tail chunk still reddens the exact-length assertion; the strip does not hide drain loss. Reverted: runner.go `bd677058…` |
| M2 | the strip replaced by "cut at the first non-`e` byte" (the contract's over-strip example) | `go test -count=1 -timeout 200s -run '^TestCoverageRuntimeDiagnosticStrip' ./internal/execution/` (`<clone-fb>`) | 1 | 9 boundary rows red + the gate row red: e.g. `stripped stream is the payload = false, want true; length = 196732, want 196731`, `stripped lines = 1, want 0: ["rror: surprising helper failure"]`, the log line `(55 bytes): rror: coverage meta-data emit failed: Access is denied.`, and the shortened-payload row `stripped stream is the payload = true, want false` |
| M2b | the prefix table widened to any `error: ` line (the conductor's dispatch row) | the same | 1 | 4 boundary rows red: both foreign-line rows (`stripped lines = 1/2, want 0`), the warning-line row and the two-documented-lines row (`stripped lines = 0, want 1/2`) |
| M3 | `TestHelperProcess` `streams` writes `count-1` stderr bytes | `-run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$'` | 1 | `stderr length = 196730, want 196731` |
| M5 (F-b mutant) | the handshake replaced by the fixed `time.Sleep(100 * time.Millisecond)` | §3.3 | 1 | 6 red / 20 in the pre-fix signature (§3.3) |
| M4 (diagnostic only, prior stage, **not** a proposed change) | the mode's fixed 100 ms window widened to 3 s | the focused descendant pair `-count=20` (plain, `<clone>`) | 0 | 20/20 green (168.636 s): the missing-descendant-output signature was the fixed window, not F1, coverage instrumentation or the drain |

Every mutation is restored: `<clone-fb>` `git status --porcelain` empty after the last revert (HEAD
`4a45cda9`, its four test-file blobs = the shipped tree's, runner.go blob `bd677058…`).

## 7. Gates

Every row foreground, bounded with `timeout`; no row ran concurrently with another suite.

| gate | command (cwd) | exit | counts / output |
| --- | --- | ---: | --- |
| red before / green after, the flake's own configuration | §2 (before), then `GOMAXPROCS=8 timeout 30m go test -json -count=20 -timeout 1200s -coverprofile=… ./internal/execution/` (`daemon/`, final tree) | 1 / 0 | before 8 red of 20 (all the subject row) with the verbatim capture; **after 0 fail events of 20 iterations** (320 s wall, `coverage: 60.1% of statements`), subject row 20 run / 20 pass, 29 strips logged (9 real, §4). The intermediate F1-tree runs red 8/20 and 3/20, every red (F-b) (§3.1) |
| plain loaded repeat (the CI shape) | `GOMAXPROCS=8 timeout 30m go test -count=200 -timeout 300s -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` (`daemon/`) | 0 | 34.473 s, 200 executions, 0 failures (before: 23.057 s, 0 failures) |
| plain package suite (local stand-in for `daemon`/`daemon-windows`) | `GOMAXPROCS=8 timeout 30m go test -count=1 -timeout 900s ./internal/execution/` (`daemon/`) | 0 | 26.480 s — the pre-F-b local reds (`probe_test.go:83` empty output) are gone |
| boundary pin + gate pin, shipped tree | §5 row 1 | 0 | 11 + 3 subtests |
| forced pins, final tree | §5 rows 3–5 | 0 / 1 / 1 | as §5 |
| WSL `-race` (Linux oracle) | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd /mnt/q/<WORKTREE>/daemon && GOMAXPROCS=8 go test -race -count=1 -timeout 600s ./internal/execution/'` | 0 | `ok … 29.413s`; 0 `DATA RACE` lines; `go1.27.0 linux/amd64` (the WSL distro's toolchain; the repo pins 1.27.4) |
| mutants | §6, six rows | 1 (M1, M2, M2b, M3, M5), 0 (M4) | as §6, all reverted with blob hashes verified |
| assertion census | `git diff -U0 -- daemon/internal/execution/*.go` + `git diff --check` (`9d113eb6` → worktree) | 0 | `runner_test.go` 195 added / 4 deleted, the 4 deletions named: the streams test's old `startHelper` call, the old `if got := len(sink.output(Stderr)); got != 196731 {` wrapper (replaced in place by the drained call), the old `startHelper` body line, and the mode's old `time.Sleep(100 * time.Millisecond)`; `probe_test.go`/`runner_unix_test.go`/`runner_windows_test.go` each 1/1 (their old call line). No assertion deleted or weakened: both payload assertions still require exactly 196731 with their original messages (`runner_test.go:58`, `:61`) and `requireStrictSequence` (`:63`) has 0 diff lines; `git diff --check` exits 0 |
| gofmt / go vet | `gofmt -l .` / `go vet ./...` (`daemon/`) | 0 / 0 | no output |
| shipped hashes at the gate runs | `git hash-object` + `sha256sum` on the four test files | 0 | §1; unchanged after the docs were written — the gates ran on the final Go tree |
| CI `daemon` / `daemon-windows` on the PR head | not run here: this stage does not push | — | the operator's CI is the oracle for the acceptance row |

## 8. Scope

```
$ git status --porcelain -uall --no-renames
 M daemon/internal/execution/probe_test.go
 M daemon/internal/execution/runner_test.go
 M daemon/internal/execution/runner_unix_test.go
 M daemon/internal/execution/runner_windows_test.go
 M docs/TODO.md
 M docs/archive/CLOSED-TODO.md
?? docs/evidence/rf-flake-stdout-drain-64kib-2026-11-24.md
?? docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md
```

The test files are exactly the contract's owned set plus the conductor's F-b extension
(`probe_test.go`, `runner_unix_test.go`, `runner_windows_test.go` — call-site-only changes for the
marker path); the docs are the plan, this record, and the TODO/CLOSED-TODO closure the ruling asked
for. `runner.go`, `ci.yml`, `minimalEnvironment`, the payload assertions and every other path are
byte-identical to `9d113eb6` (runner.go blob `bd677058…`): the mutants lived only in
`<clone>`/`<clone-fb>` and were restored to the blobs above. No commit was made (the contract leaves
the tree to verification). All cover profiles, logs and clones live outside the repository, in the
host scratch directory (paths elided as `<scratch>`).

## 9. Findings and residual risks

1. **(F1) resolved and exercised**: the subject row's coverage-mode failure mode is the child-side
   covmeta exit-hook diagnostic; the strip tolerates exactly the three documented lines, only while
   the helper forwards `GOCOVERDIR`, only as whole trailing lines, each logged — 9 natural strikes in
   the acceptance run (§4), boundary/gate/forced pins green, over-strip and widening mutants red.
2. **(F-b) resolved**: the descendant helper mode now synchronizes on the grandchild's marker instead
   of a fixed 100 ms sleep; the package's coverage-mode `-count=20` pass is 0/20 red and the plain
   package suite is green (§7), and the sleep-restored mutant reddens 6/20 in the pre-fix signature
   (§3.3). The TODO item this goal found is closed and archived
   (`docs/archive/CLOSED-TODO.md`).
3. Residual: the covmeta race itself is untouched (Windows `covmeta` rename TOCTOU between concurrent
   instrumented children sharing the tool's merge directory). Another helper-output row could in
   principle lose the race and redden its own exact assertion; 0/20 were observed for every such row
   here. The durable alternative — one warm-up instrumented child per package, or a private
   `GOCOVERDIR` per helper — stays unapplied because it changes the shared helper every test in the
   package uses (`docs/TODO.md` note, plan D7).
4. A fourth coverage-runtime message (for example `error: coverage meta-data prep failed: `), a
   diagnostic that does not start exactly at the payload end, or any foreign line still fails by
   design — that is the boundary, not an oversight.
5. `TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB`'s own coverage-mode failures are gone and the
   package's coverage-mode pass is green on this host; CI's `daemon`/`daemon-windows` rows (which do
   not instrument) remain the operator's oracle for the PR head.
