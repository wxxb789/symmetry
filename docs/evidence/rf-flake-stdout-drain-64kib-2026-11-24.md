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
whose clone-local HEAD was `15a27066`; `<clone-fb>` = the implement stage's `git clone --shared` of
the worktree, clone-local HEAD `4a45cda9` ("scratch: F1 strip + F-b handshake") whose four test-file
blobs equalled the pre-apply shipped tree's (`7955bf8b…` `runner_test.go`, `346bedd4…`
`probe_test.go`, `3e4d4a44…` `runner_unix_test.go`, `e265a770…` `runner_windows_test.go`; runner.go
at `bd677058…`); `<clone-apply>` = the apply stage's `git clone --shared`, clone-local HEAD
`e60375d4` ("scratch: apply-stage tree (S1 + S2)") whose blobs equal the current shipped tree
(`3a3177b3…` `runner_test.go`, `346bedd4…` `probe_test.go`, `3e4d4a44…` `runner_unix_test.go`,
`e265a770…` `runner_windows_test.go`; runner.go at `bd677058…`). Every command
ran foreground (bash tool `pty:true`, `timeout:0` — the harness reported pty unavailable and ran
without a terminal; the commands were still bounded with `timeout` and no job was left running) and
the two heavy configurations never ran concurrently with each other or with another suite. Windows
x64, `go1.27.4 windows/amd64`; WSL Ubuntu-24.04, `go1.27.0 linux/amd64`.

## 1. What changed

| file | numstat (add/del) | change |
| --- | --- | --- |
| `daemon/internal/execution/runner_test.go` | 194 / 4 | the strip (three documented lines, whole trailing block, helper-`GOCOVERDIR`-gated, every stripped line logged), the two pin tests (13 stripper rows after the fix stage's STD-A04 rows, §11), and the F-b readiness handshake (root waits for the grandchild's marker; `descendantHoldingStdoutInvocation`/`startHelperInvocation` plumbing); the apply stage starts the streams test through `startHelperInvocation` (§10); the fix stage rewords the prefix-table comment (STD-A01, §11) |
| `daemon/internal/execution/probe_test.go` | 1 / 1 | the bounded probe row supplies the F-b marker path |
| `daemon/internal/execution/runner_unix_test.go` | 1 / 1 | the Unix root-exit row supplies the F-b marker path |
| `daemon/internal/execution/runner_windows_test.go` | 1 / 1 | the Windows root-exit row supplies the F-b marker path |
| `docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md` | new | the plan (P1 capture, decisions D1–D8, verification plan, outcome note) |
| `docs/evidence/rf-flake-stdout-drain-64kib-2026-11-24.md` | new | this record |
| `docs/TODO.md` | 27 / 0 | one decision note: the resolved residual, the unapplied durable alternative |
| `docs/archive/CLOSED-TODO.md` | 36 / 0 | the (F-b) item the goal's gate runs found, recorded directly in the archive with its closure paragraph (never a `docs/TODO.md` item); the fix stage gives it its own section (STD-A02/A05, §11) |

No production file change. The only product-tree edits were the mutants in §6, applied in
`<clone>`/`<clone-fb>`/`<clone-apply>` and reverted by `git checkout` with blob hashes compared.
Shipped hashes at the post-apply gate runs (§10): `runner_test.go` blob
`3a3177b3daff09467138f783a2abcf4c2b94fc23`, sha256
`7194f81a34eb96cb8effd94b6416fc4a864101e93f6b0cd3a18bd5335c274971`; `probe_test.go` sha256
`e9bf53dda61343bc009c56122f8778b4d805f12ef97b12f6d78a11da2b74f2bf`; `runner_unix_test.go` sha256
`e965c169c02659c69eb243ddd67e5fa1ff1a9e09536f043102439c6ab8b657c9`; `runner_windows_test.go` sha256
`d457cef9e45d14deb83adae3b5a0a029077c39d7bdc3c26dc5602ad298f8f8dd`. The final fix-tree hashes
are in §11.

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

Apply-stage re-run (`<clone-apply>`, the post-apply tree, same mutation and command): **exit 0,
42.478 s, 0 red of 20 on an idle host** — the grandchild's spawn+exec then lands inside the 100 ms
window, so the mutant needs the load the comment names. With a bounded six-way CPU load
(`timeout 300s yes > /dev/null` ×6) the row reddens again: **exit 1, 92.502 s, 3 red iterations of
20** in the same pre-fix signature (2 × `probe_test.go:83` empty output, 1 ×
`runner_windows_test.go:218` 30 s hang guard); the spawn latency measured under that load is
116–278 ms for 10 spawns (`runner_test.go`'s own `-test.run='^$'` binary), i.e. at or above the
100 ms window. Reverted (`git checkout --`), blob `3a3177b3…` restored, clone status clean.

## 4. The strip, the pins, and the natural races

The acceptance run (§7 row 1, the post-apply tree, `-json` so passing tests' `t.Logf` lines are recorded):

| quantity | value |
| --- | ---: |
| subject-row `run` events / `pass` events | 20 / 20 |
| `fail` events, package-wide | 0 |
| `skip` events | 0 |
| `stripped coverage runtime diagnostic` log events, all | 29 |
| — on the shipped streams row (`runner_test.go:57`, the real diagnostic) | 9 |
| — on the boundary pin's gate row (`runner_test.go:124`, the synthesized line) | 20 |

So the goal's flake is fixed and *exercised*, not merely silenced: the race that produced §2's eight
reds occurred nine times in this run, the strip removed exactly the documented block each time, and
the exact-length assertions passed. Verbatim (one of the nine, paths elided):
`runner_test.go:57: stripped coverage runtime diagnostic (503 bytes): error: coverage meta-data emit
failed: writing <merge-dir>\covmeta.5bde9a34b0182dd9672696b24ef5f92c: rename from
<merge-dir>\tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c<nanotime> failed: rename <tmp> <covmeta>:
Access is denied.` Before → after on the subject row: **8/20 red → 0/20 red** over the gate runs
(§2, §3.1, §7 row 1), with every tolerated diagnostic logged.

## 5. Forced pins (contract Gates)

The F1 rows were run at the F1 tree by the prior stage (kept below); the after-F1 rows were re-run by
the apply stage at the post-apply tree in `<clone-apply>` with a forced child (one extra stderr write
appended to the `streams` mode — never in the shipped tree, which carries no forced mode).

| row | command (cwd) | exit | observed |
| --- | --- | ---: | --- |
| boundary pin + gate pin, post-apply tree | `go test -count=1 -timeout 120s -run '^TestCoverageRuntimeDiagnosticStrip' -v ./internal/execution/` (`daemon/`) | 0 | 11 boundary subtests and 3 gate subtests pass (fix tree: 13 + 3, §11) |
| forced documented line, **before F1** | `<scratch>/base-forced` (pre-F1 + the line appended to the `streams` mode): `GOMAXPROCS=8 timeout 10m go test -count=1 -timeout 300s -coverprofile=… -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` | 1 | `P1 stderr length = 196854, want 196731; first non-e byte index = 196732; over-length bytes = "error: coverage meta-data emit failed: rename <merge-dir>\\tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c: Access is denied.\n\n"; GOCOVERDIR nonempty = true` |
| forced documented line, after F1, coverage mode (post-apply tree) | `<clone-apply>` + the line: `GOMAXPROCS=8 timeout 10m go test -count=1 -timeout 300s -v -coverprofile=… -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` | 0 | PASS; `runner_test.go:57: stripped coverage runtime diagnostic (121 bytes): error: coverage meta-data emit failed: rename <merge-dir>/tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c: Access is denied.` |
| forced documented line, **`GOCOVERDIR` not forwarded**, plain mode (post-apply tree) | the same forced child without `-coverprofile` (shell `GOCOVERDIR` empty, `minimalEnvironment` forwards nothing) | 1 | `runner_test.go:58: stderr length = 196854, want 196731` — the strip is closed without a forwarded `GOCOVERDIR` (the conductor's gate) |
| forced **foreign** line, after F1, coverage mode (post-apply tree) | `<clone-apply>` + `error: unexpected forced diagnostic\n\n` appended | 1 | `runner_test.go:58: stderr length = 196768, want 196731` — a foreign line stays red |

## 6. Mutants (all in a clone, reverted by `git checkout`)

`<clone>`'s HEAD was the F1-stage clone-local commit `15a27066`; `<clone-fb>`'s is `4a45cda9` (§
header) — the implement stage's rows ran there. The apply stage re-ran M1, M2, M2b, M3 and M5 in
`<clone-apply>` (HEAD `e60375d4`, § header) so that every row is bound to the shipped bytes; the
observed cells below are those re-runs. Every row was applied, run, reverted with `git checkout --`,
and re-hashed to the listed blob before the next row.

| id | mutation | command | exit | observed |
| --- | --- | --- | ---: | --- |
| M1 | `runner.go` `readOutput` holds each chunk and never enqueues the final one (no drain of the tail) | `go test -count=1 -timeout 300s -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` (`<clone-apply>`) | 1 | `runner_test.go:55: stdout length = 196608, want 196731` — the lost tail chunk still reddens the exact-length assertion; the strip does not hide drain loss. Reverted: runner.go `bd677058…` |
| M2 | the strip replaced by "cut at the first non-`e` byte" (the contract's over-strip example; re-run as `strings.IndexFunc(output, r != 'e')`) | `go test -count=1 -timeout 200s -run '^TestCoverageRuntimeDiagnosticStrip' ./internal/execution/` (`<clone-apply>`) | 1 | 9 boundary rows red + the gate row red: the documented-line rows, both foreign-line rows, the no-terminator row, the leading-newline row and the shortened-payload row, e.g. the shortened-payload row `stripped stream is the payload = true, want false`, and the gate row `runner_test.go:124: stripped coverage runtime diagnostic (56 bytes): rror: coverage meta-data emit failed: Access is denied.` + `runner_test.go:125: drained = false, want true` |
| M2b | the prefix table widened to any `error: ` line (the conductor's dispatch row; re-run as `"error: "` prepended to the table) | the same | 1 | 2 boundary rows red: both foreign-line rows (`payload_plus_a_foreign_error_line_still_fails`, `payload_plus_a_documented_line_then_a_foreign_line_still_fails`) |
| M3 | `TestHelperProcess` `streams` writes `count-1` stderr bytes | `-run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$'` (`<clone-apply>`) | 1 | `runner_test.go:58: stderr length = 196730, want 196731` |
| M5 (F-b mutant) | the handshake replaced by the fixed `time.Sleep(100 * time.Millisecond)` | §3.3 | 1 | 6 red / 20 in the pre-fix signature at the implement tree (§3.3); apply-stage re-run on the post-apply tree: 0 red / 20 idle, 3 red / 20 under a six-way CPU load (§3.3) |
| M4 (diagnostic only, prior stage, **not** a proposed change) | the mode's fixed 100 ms window widened to 3 s | the focused descendant pair `-count=20` (plain, `<clone>`) | 0 | 20/20 green (168.636 s): the missing-descendant-output signature was the fixed window, not F1, coverage instrumentation or the drain |

Every mutation is restored: `<clone-fb>` `git status --porcelain` empty after the last implement-stage
revert (HEAD `4a45cda9`, its four test-file blobs = the pre-apply shipped tree's, runner.go blob
`bd677058…`), and `<clone-apply>` clean after the apply stage's last revert (HEAD `e60375d4`, its
four test-file blobs = the current shipped tree's (`3a3177b3…`, `346bedd4…`, `3e4d4a44…`,
`e265a770…`), runner.go blob `bd677058…`).

## 7. Gates

Every row foreground, bounded with `timeout`; no row ran concurrently with another suite.

| gate | command (cwd) | exit | counts / output |
| --- | --- | ---: | --- |
| red before / green after, the flake's own configuration | §2 (before), then `GOMAXPROCS=8 timeout 30m go test -json -count=20 -timeout 1200s -coverprofile=… ./internal/execution/` (`daemon/`; post-apply tree, §10) | 1 / 0 | before 8 red of 20 (all the subject row) with the verbatim capture; **after 0 fail events of 20 iterations** (377.013 s wall, `coverage: 60.0% of statements`), subject row 20 run / 20 pass, 29 strips logged (9 real at `runner_test.go:57`, 20 pin-gate at `runner_test.go:124`, §4). The implement-stage run at the pre-apply tree read 320 s / 60.1%; the profile carries no `_test.go` entries (`grep -c _test.go` on the profile = 0), so the 0.1% spread is product-code path coverage under load, not a test-file delta. The intermediate F1-tree runs red 8/20 and 3/20, every red (F-b) (§3.1). The fix-stage re-run at the final tree is §11 |
| plain loaded repeat (the CI shape) | `GOMAXPROCS=8 timeout 30m go test -count=200 -timeout 300s -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` (`daemon/`) | 0 | 57.816 s on the post-apply tree, 200 executions, 0 failures (implement tree: 34.473 s; before: 23.057 s, 0 failures) |
| plain package suite (local stand-in for `daemon`/`daemon-windows`) | `GOMAXPROCS=8 timeout 30m go test -count=1 -timeout 900s ./internal/execution/` (`daemon/`) | 0 | 12.637 s on the post-apply tree (implement tree: 26.480 s) — the pre-F-b local reds (`probe_test.go:83` empty output) are gone |
| boundary pin + gate pin, shipped tree | §5 row 1 | 0 | 13 + 3 subtests at the fix tree (§11); 11 + 3 at the post-apply tree (§10) |
| forced pins, post-apply tree | §5 rows 3–5 | 0 / 1 / 1 | as §5 |
| WSL `-race` (Linux oracle) | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd <worktree>/daemon && GOMAXPROCS=8 go test -race -count=1 -timeout 600s ./internal/execution/'` | 0 | `ok … 23.878s` on the post-apply tree (implement tree: 29.413 s); no race report (the detector fails the run on any race); `go1.27.0 linux/amd64` (the WSL distro's toolchain; the repo pins 1.27.4) |
| mutants | §6, six rows, re-run at the post-apply tree (M5 under a six-way load) | 1 (M1, M2, M2b, M3, M5 loaded), 0 (M4, M5 idle) | as §6, all reverted with blob hashes verified |
| assertion census | `git diff -U0 -- daemon/internal/execution/*.go` + `git diff --check` (`9d113eb6` → worktree) | 0 | `runner_test.go` 192 added / 4 deleted (the apply stage's 4→1 replacement at the streams start, §10), the 4 deletions named: the streams test's old `startHelper` call, the old `if got := len(sink.output(Stderr)); got != 196731 {` wrapper (replaced in place by the drained call), the old `startHelper` body line, and the mode's old `time.Sleep(100 * time.Millisecond)`; `probe_test.go`/`runner_unix_test.go`/`runner_windows_test.go` each 1/1 (their old call line). No assertion deleted or weakened: both payload assertions still require exactly 196731 with their original messages (`runner_test.go:55`, `:58`) and `requireStrictSequence` (`:60`) has 0 diff lines; `git diff --check` exits 0 |
| gofmt / go vet | `gofmt -l .` / `go vet ./...` (`daemon/`) | 0 / 0 | no output |
| duration ratchet (`scripts/ci/go-test-durations.py`, the w6-12 measurement harness) | `GOMAXPROCS=8 timeout 10m go test -json -count=1 -timeout 900s ./internal/execution/` → `python3 scripts/ci/go-test-durations.py <scratch>/exec-iter1.json` (`daemon/`, post-apply tree) | 0 | `tests=141 skipped=0 p95=0.210s baseline=0.260s ratio=0.81x limit=0.312s over_30s=0 adopted=0 failed=0` → PASS; slowest test 5.09 s (`TestContextCancellationTerminatesTheProcessTree`), the streams row not in the top 20. The committed baseline and ceilings are keyed to the GitHub ubuntu race run and no `internal/execution` test is on the adopted roster, so this row is a local floor check; the ratchet's verdict for the PR head is the CI row below. The fix-tree re-run and its load sensitivity are in §11 |
| shipped hashes at the gate runs | `git hash-object` + `sha256sum` on the four test files | 0 | §1, post-apply values; the Go tree did not change while the docs were written — every §7 gate ran on that tree (fix tree: §11) |
| CI `daemon` / `daemon-windows` on the PR head | not run here: this stage does not push | — | the operator's CI is the oracle for the acceptance row |

## 8. Scope

```
$ git status --porcelain -uall --no-renames
 M daemon/internal/execution/runner_test.go
 M docs/evidence/rf-flake-stdout-drain-64kib-2026-11-24.md
 M docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md
```

The implement stage's tree (the four test files — the contract's owned set plus the conductor's F-b
extension, call-site-only in `probe_test.go`/`runner_unix_test.go`/`runner_windows_test.go` — plus
the plan, this record, and the TODO/CLOSED-TODO closure) is committed as `2101921`; this block is the
apply stage's delta over it (§10): the S1 edit in `runner_test.go`, and the plan/record refreshes.
`runner.go`, `ci.yml`, `minimalEnvironment`, the payload assertions and every other path are
byte-identical to `9d113eb6` (runner.go blob `bd677058…`): the mutants lived only in
`<clone>`/`<clone-fb>`/`<clone-apply>` and were restored to the blobs above. The apply stage made no
commit (the contract leaves the tree to verification). All cover profiles, logs and clones live
outside the repository, in the host scratch directory (paths elided as `<scratch>`).

## 9. Findings and residual risks

1. **(F1) resolved and exercised**: the subject row's coverage-mode failure mode is the child-side
   covmeta exit-hook diagnostic; the strip tolerates exactly the three documented lines, only while
   the helper forwards `GOCOVERDIR`, only as a trailing block of lines matching those exact
   prefixes, each logged — 9 natural strikes in the post-apply acceptance run (§4), re-proved at the
   fix tree (§11), boundary/gate/forced pins green, over-strip and widening mutants red.
2. **(F-b) resolved**: the descendant helper mode now synchronizes on the grandchild's marker instead
   of a fixed 100 ms sleep; the package's coverage-mode `-count=20` pass is 0/20 red and the plain
   package suite is green (§7), and the sleep-restored mutant reddens 6/20 in the pre-fix signature
   (§3.3). The descendant-window item this goal's gate runs found is closed and archived
   (`docs/archive/CLOSED-TODO.md`), under its own section since the fix stage (STD-A02/A05, §11).
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

## 10. Apply stage (2026-11-24, simplify findings S1 + S2)

The simplify stage (`…-simplify/answer.md`) reported two findings; the conductor's brief — accept
everything inside the goal contract's owned set that does not weaken tests, types, validation or
permissions — admits both, and both are applied.

| finding | file | change | effect on this record |
| --- | --- | --- | --- |
| S1 | `daemon/internal/execution/runner_test.go:48` | the streams test's inline `NewRunner().Start` + `if err != nil` + `t.Fatalf` + `}` (4 lines) becomes `process := startHelperInvocation(t, sink, invocation)` — the helper this same increment extracted for the three F-b call sites, same call and the same `Start() error = %v` message | `runner_test.go` 195/4 → 192/4; blob/sha256 refreshed (§1); citations after the edit shift by −3 (`:60`→`:57` strip log, `:127`→`:124` pin gate row, `:58`→`:55` stdout assertion, `:61`→`:58` stderr assertion, `:63`→`:60` `requireStrictSequence`) |
| S2 | `docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md:93` | D4's residual pointer said `evidence §11`; the record has §1–§9 and the residual is §9 item 3 → `evidence §9` (plan D5 also records the S1 start shape) | none (doc-only) |

Re-run gates at the post-apply tree, each foreground and serialized (§7): coverage-mode `-count=20`
exit 0 with 0 fail events and the subject row 20/20 (9 natural strips logged at `runner_test.go:57`);
plain `-count=200` subject repeat 0 failures; plain package suite green; WSL `-race` green; boundary +
gate pins green (11 + 3 subtests); forced pins 0/1/1 in `<clone-apply>`; M1, M2, M2b, M3 and M5
re-run in `<clone-apply>` (M5 is load-driven: 0 red/20 idle, 3 red/20 under a six-way CPU load,
§3.3); assertion census 192/4 with no assertion deleted or weakened; `gofmt -l .`, `go vet ./...` and
`git diff --check` exit 0; the duration ratchet summarizer passes on a local single-iteration stream
(141 tests, p95 0.210 s vs the 0.260 s committed baseline, none over the 30 s cap, none on the
adopted roster, §7). Neither finding deleted or weakened a test, an evidence row or a
documented boundary, and nothing outside the contract's owned set plus the conductor's F-b extension
was touched.

## 11. Fix stage (2026-11-24, review findings STD-A01–STD-A05)

The review stage (`…-review/answer.md`, report-only after the reconciled dual-model audit panel)
returned `MUST_FIX (5)`: no behavioral defect, three P2 and two P3 comment/documentation-accuracy
findings, all inside the increment's own files. All five are applied here; no test, type, assertion
or contract clause was weakened, and nothing outside the owned set plus the F-b extension was
touched. The diff range of every gate below is `9d113eb6` → the working tree (this stage makes no
commit).

| finding | file | change | effect on this record |
| --- | --- | --- | --- |
| STD-A01 (P2) | `daemon/internal/execution/runner_test.go:624` | the prefix-table comment now names the three causes — "Coverage runtime exit-hook write failures (covmeta/covcounters) and the GOCOVERDIR-unset warning" — instead of attributing all three lines to the covmeta rename race | none (comment-only; the table, the gate and the strip are byte-identical) |
| STD-A02 (P2) | `docs/archive/CLOSED-TODO.md:4848`, this record, `docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md:29` | the (F-b) item was never a `docs/TODO.md` item to move: the range's `docs/TODO.md` diff is +27/−0 (the decision note only), so the block carries its own `## Descendant window under host load (rf-flake-stdout-drain-64kib)` heading and a header note records it as a direct record, and both table cells say "recorded directly in the archive (never a `docs/TODO.md` item)" | §1 rows for `docs/TODO.md`/`docs/archive/CLOSED-TODO.md`; the plan's "What lands" row |
| STD-A03 (P2) | `docs/plans/2026-11-24-rf-flake-stdout-drain-64kib-plan.md:107` | D6 now reads two `t.Parallel()` table tests — 13 stripper rows + 3 gate rows — matching the shipped tests (`runner_test.go:63-64` and `:107-108`) | plan D6; the §5/§7 pin counts |
| STD-A04 (P3) | `daemon/internal/execution/runner_test.go` (the pin table), `docs/TODO.md:1808` | the pin gains the mid-line-prefixed row (`payload + "x" + documented[0] + "\n"`: the documented line strips, the stray byte stays, so the length still fails) and the carriage-return row (`payload + documented[0] + "\r\n"`: strips to the payload); both pass against the unchanged stripper (their elapsed is ≤0.01 s), and the `docs/TODO.md` phrase now reads "a trailing block of documented lines matching those exact prefixes" | §1/§5/§7 counts; 11 → 13 stripper rows |
| STD-A05 (P3) | `docs/archive/CLOSED-TODO.md:4850` | the archived block's opening clause no longer reads "found by this goal's gate runs; not owned or fixed by it": it now reads "found by the goal's gate runs; brought into scope and fixed by the 2026-11-24 ruling", consistent with its own closure paragraph | archive text only |

No finding was judged invalid; all five are applied.

### 11.1 Fix-tree gates

| gate | command (cwd) | exit | counts / output |
| --- | --- | ---: | --- |
| coverage-mode acceptance, the flake's own configuration | `GOMAXPROCS=8 timeout 30m go test -json -count=20 -timeout 1200s -coverprofile=<scratch>/exec-cov-fix.out ./internal/execution/` (`daemon/`) | 0 | 0 `fail` and 0 `skip` events of 20 iterations; subject row 20 run / 20 pass; `coverage: 59.8% of statements` (package line `328.587s`); profile carries no `_test.go` entries; 22 strips logged (2 real at `runner_test.go:57`, 20 pin-gate at `runner_test.go:126`) |
| focused pin + subject + descendant rows | `GOMAXPROCS=8 timeout 10m go test -count=1 -timeout 400s -v -run '^(TestCoverageRuntimeDiagnosticStrip|TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB|TestRunBoundedCommandStopsDescendantHoldingOutputPipe|TestRootExitDoesNotLeaveADescendantHoldingStdout|TestTerminateStopsDescendantProcessOnWindows)' ./internal/execution/` (`daemon/`) | 0 | `ok 8.529s`; 13 boundary + 3 gate subtests pass; subject row 2.53 s; both descendant rows and the Windows terminate row pass |
| plain loaded repeat (the CI shape) | `GOMAXPROCS=8 timeout 30m go test -count=200 -timeout 300s -run '^TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB$' ./internal/execution/` (`daemon/`) | 0 | `ok 50.569s`, 200 executions, 0 failures |
| plain package suite | `GOMAXPROCS=8 timeout 30m go test -count=1 -timeout 900s ./internal/execution/` (`daemon/`) | 0 | `ok 15.073s` |
| WSL `-race` (Linux oracle) | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd <worktree>/daemon && GOMAXPROCS=8 go test -race -count=1 -timeout 600s ./internal/execution/'` | 0 | `ok 26.941s`, no race report |
| mutants M2 and M2b (the pin-coupled rows) | `<clone-fix>` = `git clone --shared` at `25dd63e9` + the final `runner_test.go` (blob `b420ef79…`), `go test -count=1 -timeout 200s -run '^TestCoverageRuntimeDiagnosticStrip' ./internal/execution/` | 1 / 1 | M2 (`strings.IndexFunc(output, r != 'e')` over-strip): 11 boundary rows red + the gate row red (`drained = false, want true` at `runner_test.go:127`), the two fix-stage rows both red (9 rows at the 11-row post-apply tree, §6); M2b (`"error: "` prepended to the table): the 2 foreign-line rows red, unchanged. Each reverted by restoring the backup, blob `b420ef79…` re-verified |
| assertion census | `git diff -U0 9d113eb6 -- daemon/internal/execution/*.go` and `git diff --check 9d113eb6` | 0 | `runner_test.go` 194 added / 4 deleted — the 4 are the apply stage's (§10); the comment reword adds no deletion because the whole block is new against the base; `probe_test.go`/`runner_unix_test.go`/`runner_windows_test.go` 1/1 each; no `196731` assertion and no `requireStrictSequence` line added or deleted; `git diff --check` clean |
| gofmt / go vet | `gofmt -l .` / `go vet ./...` (`daemon/`) | 0 / 0 | no output |
| local duration ratchet (floor check; load-sensitive, not a contract gate) | `GOMAXPROCS=8 timeout 10m go test -json -count=1 -timeout 900s ./internal/execution/` (`daemon/`) → `python3 scripts/ci/go-test-durations.py <scratch>/…` (repo root — the script lives at the root, not under `daemon/`) | 1 (loaded) / 0 (quiet) | see 11.2 |
| record hygiene scans | an added-line scan of the branch diff for the account name, drive-letter prefixes and the guest and temporary path prefixes (`/mnt`, `/home`, `/tmp`, `/var`), plus the branch diff filtered for profile-directory spellings; `git diff -U0 9d113eb6 -- daemon/internal/execution/` for added comment runs | 0 | 0 hits in added lines (the one `/mnt` hit, the §7 WSL row's original cwd, is replaced by `<worktree>`); 0 added comment runs above 2 lines |
| document-citation gate (the CI gate that covers `docs/archive/CLOSED-TODO.md`) | `python3 scripts/ci/check-citation-existence.py --base 9d113eb6 --head a20d8121` — the head is a no-ref `commit-tree` of the working tree through `<scratch>/cand.index`, since this stage makes no commit | 0 | 1 changed document passes the path and symbol check (573 path citations); the 3 notices are pre-existing ignore-covered citations (`control/priv/static/portal` ×2, `control/deps/ecto/...`) |

### 11.2 The local duration ratchet, and why the two rows cannot move the p95

The fix tree reds the local floor check under host load exactly as the committed pre-fix tree does in
the same windows. Three fix-tree runs in loaded windows read `p95` 0.450 / 0.540 / 1.940 (exit 1),
while a committed-tree run between them read 0.220 (exit 0). Three paired A/B runs then alternated
the committed and the final `runner_test.go` in `<clone-fix>` (summarizer over each stream): pre-fix
tree `p95` 0.480 / 0.890 / 0.340 (exit 1, 1, 1) versus fix tree 0.340 / 0.410 / 0.220 (exit 1, 1,
**0**). The inflated runs show broad multi-test inflation unrelated to the change (for example
`TestRunBoundedCommandTerminatesOnCallerCancellation` at 5.13 s against 0.16 s pre-fix; the
descendant rows and the sink rows rise and fall with it). The two added rows' own work is ≤0.01 s
and they are the smallest entries of the distribution: `nearest_rank` reads `ceil(0.95·n)−1` on the
ascending list, so 141 → 143 keys moves index 133 onto index 135 — the same order statistic once two
minimum values are inserted — and `fix-minus-new` p95 equals the fix-tree p95 in every pair (0.340,
0.410, 0.220). The blocking CI step reads the module-wide ubuntu race stream, and CI stays the
oracle for the PR head.

### 11.3 Citation shift and final hashes

The two added rows shift every line after the pin table by +2 against the post-apply tree, while the
verbatim log lines quoted in §4/§6 keep the numbers of the tree they ran on: pin gate row `:124` →
`:126`, its `t.Fatalf` `:125` → `:127`, the prefix-table comment `:622` → `:624`,
`stripCoverageRuntimeDiagnostics` `:655` → `:657`, its `LastIndex` line `:659` → `:661`, the
`streams` mode `:869` → `:871`, `root-exits-child-holding-stdout` `:921` → `:923`; lines above the
table are unchanged (`:54-55`, `:57`, `:58`, `:60`). Final-tree hashes (`git hash-object` +
`sha256sum`): `runner_test.go` blob `b420ef79dd02d35c14ae9116674ac449f6c0bb62`, sha256
`76bb28904277cdd967c1054b1476b1a56a7c408a939bd205bb28488d66a74123`; `probe_test.go`,
`runner_unix_test.go`, `runner_windows_test.go` and `runner.go` are byte-identical to the
post-apply tree (sha256 `e9bf53dd…`, `e965c169…`, `d457cef9…`; `runner.go` blob `bd677058…`).
Terminal `(package, test)` keys 141 → 143 (top-level tests unchanged at 99; the boundary subtests
11 → 13, the gate subtests 3 → 3); the two new subtests both pass in every mode above.
