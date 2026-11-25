# rf-flake-autonomous-return-reader — evidence record

Base `01c0d4cf` (the dispatch head; the contract records the same revision as its head and base).
Contract: `goal-rf-flake-autonomous-return-reader.md` (GDD state root, external to the repository).
Plan: `docs/plans/2026-11-25-rf-flake-autonomous-return-reader-plan.md`. Shape references: the
rf-flake-omo-attach-ack-deadline plan/evidence pair (record format, the P1-only `before` arm, the
pins/mutants rows with restored hashes) and the rf-flake-stdout-drain-64kib pair.

**Subject revision (binding):** the worktree at `01c0d4cf` with
`daemon/cmd/symmetry-fake-agent/autonomous_test.go` sha256
`fbdcce641b743435b2b0fdb18a8c22b4d79bd2c2d4e0cbc5d5a89394681e400b` (`git diff --numstat` 16/3).
The product file is untouched at base: `daemon/cmd/symmetry-fake-agent/autonomous.go` sha256
`d041a4cb4cb02aa4f5d7dbb1b8c5b61e9ad1971b326a8659938571f053439773`, git blob
`d82b59919541c7fdf460f0b0485f5dd32c90828a`. `<worktree>` is the goal worktree, `<scratch>` the
disposable scratch root outside it; no absolute host paths are recorded.

## 1. Method and toolchain

- Windows x64 host, 16 cores. Native `-race` is unavailable on this host (`go: -race requires cgo`
  and no C toolchain is installed), so every `-race` run below executes in WSL Ubuntu-24.04 as the
  unprivileged `accept` user (go1.27.0 linux/amd64 by default; `GOTOOLCHAIN=go1.27.4`, the CI
  compiler, for the disassembly witness and the `-race` oracle), matching the CI `daemon` job's OS
  (`ubuntu-latest`, go toolchain at `/opt/hostedtoolcache/go/1.27.4/x64` in the fetched logs).
- Scratch layout under `<scratch>`: `before` (the P1-only arm: base test file plus the `runElapsed`
  detail, sha256 `411429383902e12bea08ad192dca5e53cf2b80409c648f833fc8fab0111af1ee`), `pins` (the
  final test bytes `fbdcce64…` plus temporary product mutations), `ci` (the refetched CI logs and
  excerpts). Every scratch product edit was restored and verified against the base sha256 above.
- Every command ran bounded with `timeout`; the loaded repeats ran beside one bounded heavy carrier
  started in the same shell and reaped with `wait`, never beside the full suite. No job was left
  pending.

## 2. What changed

| file | numstat (add/del) | change |
| --- | --- | --- |
| `daemon/cmd/symmetry-fake-agent/autonomous_test.go` | 16 / 3 | P1 `runElapsed`; F1 settle-to-persistence scan; the 2-line comment on `readerExitSettle` |
| `docs/plans/2026-11-25-rf-flake-autonomous-return-reader-plan.md` | new | the plan, decisions and the command/count table |
| `docs/evidence/rf-flake-autonomous-return-reader-2026-11-25.md` | new | this record |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | 1 / 30, 64 / 0 | the tracked bullet moves to the archive with its closure statement; the section header count is reconciled 3→2 |

No committed production change; the only product edits were temporary mutants in `<scratch>/pins`,
restored byte-identical (§5).

## 3. The recorded CI reds, verbatim (refetched)

`gh run view <id> --log-failed` on `wxxb789/symmetry` (logs kept at `<scratch>/ci`). Both reds are
the `daemon` job's race step (`go test -race -count=1 -timeout 600s -json ./...` through
`scripts/ci/stdio-only-caller-boundary.py`); the only failing test of each run
(`failed=1 package_fail=1`; the duration summary in both logs reads
`PASS: no unadopted test over the cap …`).

Run `45510612412`, head `01c0d4cf` (workflow run created 2026-11-24T23:31:02Z, failure), verbatim:

```
    autonomous_test.go:308: reader goroutine is still alive when runAutonomous returned:
        goroutine 46 [running]:
        github.com/wxxb789/symmetry/daemon/cmd/symmetry-fake-agent.TestAutonomousReturnReleasesItsInputReader(0xc00017c488)
	/home/runner/work/symmetry/symmetry/daemon/cmd/symmetry-fake-agent/autonomous_test.go:306 +0x771
        testing.tRunner(0xc00017c488, 0x11e9a50)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2195 +0x21d
        created by testing.(*T).Run in goroutine 1
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2260 +0xb13

        goroutine 1 [chan receive]:
        testing.(*T).Run(0xc0001aab48, {0xc82885, 0x2a}, 0x11e9a50)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2268 +0xb3e
        testing.runTests.func1(0xc0001aab48)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2744 +0x85
        testing.tRunner(0xc0001aab48, 0xc0002319f0)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2195 +0x21d
        testing.runTests({0xc7b4c0, 0x22}, {0xc8e9ee, 0x3a}, 0xc0001a43d8, {0x124f050, 0x20, 0x20}, {0xc2b9a64bce9f3708, 0x8bb4442d73, ...})
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2742 +0x9ea
        testing.(*M).Run(0xc0001f4320)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2602 +0xf45
        github.com/wxxb789/symmetry/daemon/cmd/symmetry-fake-agent.TestMain(0xc0001f4320)
	/home/runner/work/symmetry/symmetry/daemon/cmd/symmetry-fake-agent/main_test.go:42 +0x39
        main.main()
	_testmain.go:110 +0x172

        goroutine 47 [runnable]:
        github.com/wxxb789/symmetry/daemon/cmd/symmetry-fake-agent.runAutonomous.func1()
	/home/runner/work/symmetry/symmetry/daemon/cmd/symmetry-fake-agent/autonomous.go:111 +0x213
        created by github.com/wxxb789/symmetry/daemon/cmd/symmetry-fake-agent.runAutonomous in goroutine 50
	/home/runner/work/symmetry/symmetry/daemon/cmd/symmetry-fake-agent/autonomous.go:97 +0x233
--- FAIL: TestAutonomousReturnReleasesItsInputReader (0.00s)
```

Run `45430210878`, head `5c297c25`, verbatim (same shape, `goroutine 46 [runnable]`,
`+0x213`, elapsed `0.04s`):

```
    autonomous_test.go:308: reader goroutine is still alive when runAutonomous returned:
        goroutine 44 [running]:
        github.com/wxxb789/symmetry/daemon/cmd/symmetry-fake-agent.TestAutonomousReturnReleasesItsInputReader(0xc00028e008)
	/home/runner/work/symmetry/symmetry/daemon/cmd/symmetry-fake-agent/autonomous_test.go:306 +0x771
        testing.tRunner(0xc00028e008, 0x11e9a50)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2195 +0x21d
        created by testing.(*T).Run in goroutine 1
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2260 +0xb13

        goroutine 1 [chan receive]:
        testing.(*T).Run(0xc00028e248, {0xc82885, 0x2a}, 0x11e9a50)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2268 +0xb3e
        testing.runTests.func1(0xc00028e248)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2744 +0x85
        testing.tRunner(0xc00028e248, 0xc0001b19f0)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2195 +0x21d
        testing.runTests({0xc7b4c0, 0x22}, {0xc8e9ee, 0x3a}, 0xc000296108, {0x124f050, 0x20, 0x20}, {0xc2b98b42e863c4d9, 0x8bb3bd2e57, ...})
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2742 +0x9ea
        testing.(*M).Run(0xc0002d40a0)
	/opt/hostedtoolcache/go/1.27.4/x64/src/testing/testing.go:2602 +0xf45
        github.com/wxxb789/symmetry/daemon/cmd/symmetry-fake-agent.TestMain(0xc0002d40a0)
	/home/runner/work/symmetry/symmetry/daemon/cmd/symmetry-fake-agent/main_test.go:42 +0x39
        main.main()
	_testmain.go:110 +0x172

        goroutine 46 [runnable]:
        github.com/wxxb789/symmetry/daemon/cmd/symmetry-fake-agent.runAutonomous.func1()
	/home/runner/work/symmetry/symmetry/daemon/cmd/symmetry-fake-agent/autonomous.go:111 +0x213
        created by github.com/wxxb789/symmetry/daemon/cmd/symmetry-fake-agent.runAutonomous in goroutine 45
	/home/runner/work/symmetry/symmetry/daemon/cmd/symmetry-fake-agent/autonomous.go:97 +0x233
--- FAIL: TestAutonomousReturnReleasesItsInputReader (0.04s)
```

Reading: the flagged goroutine sits at the reader closure's tail (`autonomous.go:111`, no `readLine`
or select frame), `[runnable]` under the `runtime.Stack(_, true)` stop-the-world dump; both runs
returned in under `readerJoinTimeout = 50 ms` (0.00 s / 0.04 s), so the join's `time.After` arm did
not fire and the reader had already run `close(readerDone)` (`autonomous.go:98`) — it was in its
death transition when the scan read it. The sibling
`TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier` passed in the same runs (0.05 s). The
goleak cleanup reported nothing in either log (no `found unexpected goroutines:` line for this
package), i.e. the goroutine was gone by cleanup time.

### 3.1 Disassembly witness (`+0x213`)

Built in WSL from the base tree (`<scratch>/witness`, `git archive HEAD daemon`) with the CI's
compiler and flags class: `GOTOOLCHAIN=go1.27.4 go test -race -c -o /tmp/fake-agent-witness.test
./cmd/symmetry-fake-agent/` (`go version` → `go1.27.4 linux/amd64`), then `go tool objdump -s
'runAutonomous\.func1'` (133 instructions). Verbatim around the offset both CI dumps print:

```
+0x20c	autonomous.go:111	0xbb382c		5d			POPQ BP
+0x20d	autonomous.go:111	0xbb382d		c3			RET
+0x20e	autonomous.go:111	0xbb382e		e80d188dff		CALL runtime.deferreturn(SB)
+0x213	autonomous.go:105	0xbb3833		e8a8bd91ff		CALL runtime.racefuncexit(SB)   <<< +0x213
+0x218	autonomous.go:111	0xbb3838		4881c438010000		ADDQ $0x138, SP
+0x21f	autonomous.go:111	0xbb383f		5d			POPQ BP
+0x220	autonomous.go:111	0xbb3840		c3			RET
```

`+0x213` is the `CALL runtime.racefuncexit(SB)` immediately after `CALL runtime.deferreturn(SB)`;
the reader closure's only defers are `defer close(readerDone)` (`autonomous.go:98`, registered
first, LIFO-last) and `defer close(inputs)` (`:99`), so a frame stopped at that instruction is in
the return epilogue with the deferred closes run — and both CI dumps print exactly
`autonomous.go:111 +0x213` on `[runnable]`. The other return exits of the closure carry the same
`deferreturn`/`racefuncexit` pairs (`+0x111`/`:98`, `+0x1f8`/`:108`, `+0x26f`/`:99`,
`+0x2cf`/`:98`), so every exit path runs the deferred closes first. Line-table note:
`objdump`/`addr2line` label the `+0x213` call `autonomous.go:105` (the `<-done` arm's `return`)
while the runtime printed `:111` (the closure's closing line) in both dumps; the reading rests on
the instruction, not the printed line.

## 4. Loaded repeats (red-before / green-after)

### 4.1 Red-before (`<scratch>/before`, the P1-only arm)

The contract's command, run from `<scratch>/before/daemon` beside the carrier
`timeout 25m go test -count=8 -timeout 1400s -run '^TestOutputBackpressureRecoversWithOneTruncationMarker$' ./internal/app/`
(same shell, reaped with `wait`; carrier `ok … 165.866s`, exit 0):

```
GOMAXPROCS=4 timeout 30m go test -race -count=20000 -timeout 1500s -v -run '^TestAutonomousReturnReleasesItsInputReader$' ./cmd/symmetry-fake-agent/ > <scratch>/repeat-before.log 2>&1
```

exit 1, `FAIL … 209.464s` (216 s wall): **5 red / 20000**, 19995 `--- PASS` — the flake reproduced
locally under load, and every red is the CI shape:

| red | log line | `runElapsed` | flagged goroutine |
| --- | ---: | --- | --- |
| 1 | 124 | `9.942423ms` | `goroutine 214 [runnable]` at `autonomous.go:111 +0x213` |
| 2 | 204 | `2.849707ms` | `goroutine 276 [runnable]` at `autonomous.go:111 +0x213` |
| 3 | 846 | `12.381629ms` | `goroutine 1186 [runnable]` at `autonomous.go:111 +0x213` |
| 4 | 8082 | `3.874209ms` | `goroutine 11914 [runnable]` at `autonomous.go:111 +0x213` |
| 5 | 19230 | `3.433408ms` | `goroutine 28638 [runnable]` at `autonomous.go:111 +0x213` |

Every `runElapsed` is far under `readerJoinTimeout = 50 ms`, so the P1 detail self-classifies all
five as released readers — identical to the two CI reds (same frame, same offset, same
`[runnable]`). This also resolves the contract's CI-vs-local discrepancy: the window needs the
loaded `-race -count=20000` shape (~0.025 % of iterations) locally, while the hosted runner opens
it in ~2 of 20 full-suite runs.

### 4.2 Green-after (the worktree, final bytes)

The same command (D3 recipe) on the final `fbdcce64…` bytes, beside the same carrier class
(`ok … 148.994s`, exit 0):

```
GOMAXPROCS=4 timeout 30m go test -race -count=20000 -timeout 1500s -v -run '^TestAutonomousReturnReleasesItsInputReader$' ./cmd/symmetry-fake-agent/ > <scratch>/repeat-after.log 2>&1
```

exit 0, `ok … 182.690s` (186 s wall): **0 red / 20000**, 20000 `--- PASS` — the same recipe, host
class and carrier under which the P1-only arm red 5/20000. The accepted condition is unimpaired: a
persistent reader still reds (§5 P2b/P2c/M1/M2) and every other assertion of the test is unchanged.

## 5. Pins and mutants (scratch copies, restored byte-identical)

Product file at base: `daemon/cmd/symmetry-fake-agent/autonomous.go` sha256
`d041a4cb4cb02aa4f5d7dbb1b8c5b61e9ad1971b326a8659938571f053439773` (git blob `d82b5991…`). Every
restore below was verified against that value (and the clones' test files stayed at their recorded
sha256s: `before` `41142938…`, `pins` `fbdcce64…`). All rows ran with `GOTOOLCHAIN=go1.27.4` in
WSL; logs are named per row under `<scratch>`.

| id | scratch edit | command (clone) | observed | restore |
| --- | --- | --- | --- | --- |
| P2a-before | product: the reader closure's first defer is a bounded ~50 ms `runtime.Gosched()` loop, so it runs after `close(readerDone)` | `go test -race -count=5 -timeout 600s -v -run '^TestAutonomousReturnReleasesItsInputReader$' ./cmd/symmetry-fake-agent/` (`before`) | exit 1, 5/5 red: `autonomous_test.go:310: reader goroutine is still alive when runAutonomous returned (runElapsed 5.067212ms)` and a dump frame at `autonomous.go:118 +0x218`; goleak silent (the tail ends inside its 50 ms) — the P1 detail classifies the reader as released (`5.07 ms < readerJoinTimeout`) | d041a4cb… |
| P2a-after | same edit | same command, `-count=50` (`pins`) | exit 0, 50/50 `--- PASS` (`ok … 4.281s`): the frame vanishes at ~50 ms, inside `readerExitSettle` | d041a4cb… |
| P2b/M1-before | product: `_ = closer.Close()` dropped (the whole `input.(io.Closer)` block) | same command, `-count=3` (`before`) | exit 1, 3/3 red: `(runElapsed 54.31483ms)` — ≥ `readerJoinTimeout`, i.e. the join timed out on a reader that was never released | d041a4cb… |
| P2b/M1-after | same edit | same command, `-count=3` (`pins`) | exit 1, 3/3 red: `autonomous_test.go:319: reader goroutine is still alive 1s after runAutonomous returned (runElapsed 54.895633ms)`; the dump shows the parked reader `io.(*PipeReader).Read` → `bufio.(*Reader).fill` → `readLine` → `runAutonomous.func1`; the goleak cleanup also reports it (`autonomous_test.go:215: found unexpected goroutines:`) | d041a4cb… |
| P2c/M2-before | product: the reader's read-error branch becomes `for { runtime.Gosched() }`, hoisted above the send select (the in-place edit would be absorbed by the ready `<-done` arm in ~50 % of runs) | same command, `-count=3` (`before`) | exit 1, 3/3 red: `(runElapsed 53.394628ms)` — the join timed out | d041a4cb… |
| P2c/M2-after | same edit | same command, `-count=3` (`pins`) | exit 1, 3/3 red: `1s after runAutonomous returned (runElapsed 53.402728ms)`; dump `goroutine 21 [runnable]: runtime.Gosched() … runAutonomous.func1()`; the goleak cleanup also reports it | d041a4cb… |
| M3 | product: the join select collapses to `<-readerDone` (the `time.After(readerJoinTimeout)` arm removed) | `go test -race -count=1 -timeout 240s -v -run '^TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier$' ./cmd/symmetry-fake-agent/` (`pins`) | exit 1: `--- FAIL: TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier (30.04s)`, `autonomous_test.go:355: runAutonomous did not return: the input cannot be unblocked and the join was not bounded` | d041a4cb… |

M1/M2's CI oracle is the `daemon` job (`.github/workflows/ci.yml:942`); `daemon-windows` is
record-only (contract Q3(A)). The mutant edits are temporary scratch bytes only; the worktree's
product file is untouched at base.

## 6. Assertion census and hygiene

| metric | base `01c0d4cf` | HEAD |
| --- | ---: | ---: |
| failure sites (`t.Fatalf`/`t.Errorf`) | 24 | 24 |
| `runtime.Stack` call sites | 1 | 1 |
| containment checks (`runAutonomous.func1` substring sites) | 1 | 1 |

No assertion was removed: the one containment check keeps its needle and its failure site and is
reshaped into the settle loop; the closed-pipe write check and the summary check
(`autonomous_test.go:323-328` in the final file) and the sibling
`TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier` are byte-identical. The diff against
`01c0d4cf` touches only the owned files; `git hash-object
daemon/cmd/symmetry-fake-agent/autonomous.go` is `d82b5991…`; `git diff --check 01c0d4cf` (frozen base → final bytes) exits 0.

## 7. Gates

| gate | command (cwd) | exit | result |
| --- | --- | ---: | --- |
| base census | `cd <worktree>/daemon && timeout 45m go test -race -count=1 -timeout 2400s ./...` (WSL `accept`, go1.27.0) | 0 | every package `ok` (31 plus three `[no test files]`; `cmd/symmetry-fake-agent` `13.444s`), 1028 s wall, no red anywhere |
| red-before repeat | the §4.1 command from `<scratch>/before/daemon` beside the carrier | 1 | 5 red / 20000 (§4.1) |
| green-after repeat | the §4.2 command from `<worktree>/daemon` beside the carrier (`ok … 148.994s`) | 0 | 0 red / 20000 (§4.2) |
| WSL `-race` oracle | `cd <worktree>/daemon && GOTOOLCHAIN=go1.27.4 go test -race -count=1 -timeout 1200s -run '^(TestAutonomousReturnReleasesItsInputReader\|TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier\|TestLeakOracleDetectsLeakedGoroutine)$' ./cmd/symmetry-fake-agent/` (WSL `accept`) | 0 | `ok … 1.610s`, no race report |
| pins and mutants | §5 (P2a 1/0, P2b/M1 1/1, P2c/M2 1/1, M3 1) | — | every expectation observed; product restored byte-identical at every row |
| package smoke | `go test -race -count=1 -timeout 600s ./cmd/symmetry-fake-agent/` (worktree, WSL) | 0 | `ok … 24.820s` |
| calibration | `go test -race -count=200 -timeout 600s -run '^TestAutonomousReturnReleasesItsInputReader$' ./cmd/symmetry-fake-agent/` (worktree, WSL) | 0 | `ok … 3.140s` |
| assertion census | §6 | — | 24/24 failure sites, 1/1 `runtime.Stack` site; product blob `d82b5991…` |
| diff hygiene | `git diff --check 01c0d4cf` (worktree root; frozen range base → final bytes; the implement-stage plain form ran while this record and the plan were untracked); `gofmt -l daemon/cmd/symmetry-fake-agent/autonomous_test.go` | 0 | clean; empty (the `space before tab in indent` lines the range form flags are re-indented) |
| citation existence | emulation repo of the final tree: `python3 scripts/ci/check-citation-existence.py --repo . --base <BASE> --head HEAD` | 0 | `1 changed document(s) pass the path and symbol check (609 path citation(s))`; three pre-existing ignore-covered notices on `docs/archive/CLOSED-TODO.md:737,752,2512` |

Not run here: CI `daemon` / `daemon-windows` on a PR head (the stage must not push; conductor-owned),
the Windows-native suite (native `-race` is unavailable on this host; the WSL census covers the
daemon module on the CI platform), and any `daemon-windows`-specific mutant execution (the contract
records `daemon-windows` as record-only, Q3(A)).

## 8. Residual risks and known limits

- The diagnosis-stage local non-reproduction (0/28 focused runs, 0/6000 path-replica iterations)
  was overturned by the loaded repeat: 5 red / 20000 on the P1-only arm under the D3 recipe, each
  red the CI frame and offset (§4.1). F1 does not depend on the unnamed CI load agent.
- The seven `[runnable]` reds (two CI, five local) returned in 0.00–12.4 ms wall, all under
  `readerJoinTimeout`; the settle bound (`readerExitSettle = 1 s`) is 20× the join bound and ~80×
  the largest red `runElapsed` (12.4 ms), so a dying reader cannot red while a reader that persists
  (parked, spinning, or never exiting) still does.
- A released reader that takes longer than 1 s to die would red; no such shape is recorded in any
  CI run or probe, and a goroutine past its final defer is microseconds from `racefuncexit` even
  under load.
- A persistent reader reds twice (the settle-deadline failure, then the goleak cleanup); that is
  intended — the contract's P2c says "the goleak cleanup would too".
- The mutants' oracle is the CI `daemon` job (contract Q3(A)); `daemon-windows` is record-only. The
  M3 sibling red at the 30 s `testHangGuard` is a local execution, not a CI observation.
- The census runs under WSL; the CI-only steps around the race step (the stdio-only boundary
  wrapper, the duration-summary gate) are not reproduced locally.
