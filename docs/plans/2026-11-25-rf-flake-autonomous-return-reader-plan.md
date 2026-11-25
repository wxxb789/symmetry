# rf-flake-autonomous-return-reader — plan

Base `01c0d4cf` (the dispatch head; the contract records the same revision as its head and base).
Contract: `goal-rf-flake-autonomous-return-reader.md` (GDD state root, external to the repository).
This plan implements the contract and records the choices it leaves to the implementation. Evidence
record: `docs/evidence/rf-flake-autonomous-return-reader-2026-11-25.md`. Shape references: the
rf-flake-omo-attach-ack-deadline plan/evidence pair (plan format, the P1-only `before` arm, the
pins/mutants table with restored hashes) and the rf-flake-stdout-drain-64kib pair (a sibling daemon
test flake closure).

Owned set (STRICT): `daemon/cmd/symmetry-fake-agent/autonomous_test.go`, this plan, the evidence
record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`. `daemon/cmd/symmetry-fake-agent/autonomous.go`
is read-only; the only product edits in this slice are temporary mutants in scratch copies, restored
byte-identical (product blob `d82b5991…` at base and after every restore).

Delegation decision (stage template §Optional delegation): skipped as ineligible before invoking —
the runtime's skill catalog offers `ce-work` without a `mode:return-to-caller` invocation contract,
and the dispatch supplies no gate command (`-`), so the "verbatim gate command" precondition cannot
hold. The loop runs inline.

## What lands

| file | change |
| --- | --- |
| `daemon/cmd/symmetry-fake-agent/autonomous_test.go` | P1 `runElapsed` in the failure detail; F1 settle-to-persistence scan (`readerExitSettle = 1s`); the 2-line comment by the const |
| `docs/plans/2026-11-25-rf-flake-autonomous-return-reader-plan.md` | this plan |
| `docs/evidence/rf-flake-autonomous-return-reader-2026-11-25.md` | the evidence record (commands, counts, dumps, pins, mutants, hashes) |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | the `:1495-1522` bullet moves to the archive with its closure statement; the section header count is reconciled |

No committed production change. No other assertion changes: the closed-pipe write check and the
summary check at `autonomous_test.go:323-328` and the sibling
`TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier` stay byte-identical.

## D1. F1: presence is not a leak; persistence is (contract ruling Q1(A))

`runAutonomous` joins its reader only after the reader ran `close(readerDone)` (`autonomous.go:98`,
`:121-124`); the join's `time.After(readerJoinTimeout)` arm cannot explain a sub-50 ms return, so
the recorded CI reds (0.00 s, 0.04 s) flagged a goroutine that had already been released and was in
its death tail (disassembly maps `+0x213` to the `racefuncexit` call after `deferreturn`). The fix
is test-only: after `runDone`, dump all stacks; pass immediately when no `runAutonomous.func1` frame
is present; otherwise re-dump every 1 ms until the frame vanishes or `readerExitSettle = 1 s`
passes; on persistence, fail with the dump. A released reader is dying and always vanishes; a
parked or never-exiting reader never does, so persistence still reds deterministically. This is not
a retry of the forbidden kind: nothing that leaked later passes; the accepted condition is "not
persistent". `readerJoinTimeout` stays 50 ms. The 1 s bound is ~20× the join bound and ~80× the
largest red `runElapsed` (12.4 ms; evidence record §4.1), so a starved host cannot
turn a dying reader into a red while any reader that truly persists does. Comment policy: exactly the two
lines beside `readerExitSettle` saying why persistence, not presence, is the condition.

## D2. P1: `runElapsed` makes any future red self-classifying

`runStart := time.Now()` immediately before the reader goroutine spawn; `runElapsed :=
time.Since(runStart)` immediately after `runDone` returns (before the settle scan); the persistence
failure message carries `runElapsed` and `readerExitSettle`. Classification: `runElapsed <
readerJoinTimeout` ⇒ the join returned through `<-readerDone` ⇒ the reader had been released and yet
persisted past the settle bound. The recorded CI reds carried 0.00 s / 0.04 s test elapsed.

## D3. `before`/green repeat recipe

`before` is a scratch copy carrying the P1-only arm (the old instant check plus the `runElapsed`
detail), the omo pair's precedent; `after` is the worktree's final bytes. Repeat command, as the
contract writes it, run once per arm:

```
GOMAXPROCS=4 timeout 30m go test -race -count=20000 -timeout 1500s -v -run '^TestAutonomousReturnReleasesItsInputReader$' ./cmd/symmetry-fake-agent/ > <log> 2>&1
```

beside one heavy carrier started in the same shell and reaped with `wait`: `timeout 25m go test
-count=8 -timeout 1400s -run '^TestOutputBackpressureRecoversWithOneTruncationMarker$'
./internal/app/` (the omo pair's carrier class; never the full suite beside a loaded repeat). Any
red carries its `runElapsed` and dump; none is worked around (the observed before outcome: 5 red /
20000, §4.1 of the evidence record).

Host toolchain note (recorded, not a workaround): native Windows cannot run `-race` on this host
(`go: -race requires cgo` and no C toolchain is installed), so the census and the loaded repeats run
in WSL Ubuntu-24.04 as the unprivileged `accept` user (go1.27.0 linux/amd64), which matches the CI
`daemon` job's OS; the WSL `-race` oracle gate uses the same distro and user. The CI-vs-local
discrepancy the contract records (two reds in ~20 hosted CI runs, 0/28 focused and 0/6000
path-replica iterations at diagnosis) was resolved by the loaded repeats: the recipe recovered 5
red / 20000 on the P1-only arm (§4.1), the same frame and offset as the CI dumps. F1 is designed not
to depend on the unnamed CI load agent.

## D4. Pins and mutants (scratch copies, byte-identical restores)

Layout: `<scratch>/before` (P1-only arm), `<scratch>/pins` (final test bytes; product mutations).
Every mutation below is applied to a scratch copy only; each restore is verified against the
recorded sha256 of the file it touched (product file at base):
`daemon/cmd/symmetry-fake-agent/autonomous.go` sha256 recorded in the evidence record.

| id | scratch edit | subject | expectation |
| --- | --- | --- | --- |
| P2a | product: reader goroutine gains a ~50 ms bounded `runtime.Gosched()` spin as its first defer, so it executes after `close(readerDone)` | `before` (old check) | red with the P1 detail (`runElapsed` ≈ the run, < `readerJoinTimeout`); goleak silent |
| P2a | same edit | `pins` (F1) | green: the frame vanishes at ~50 ms < `readerExitSettle` |
| P2b/M1 | product `autonomous.go:120`: `_ = closer.Close()` dropped | `before` (old check) | red: instant frame present (the reader is parked in `Read`) |
| P2b/M1 | same edit | `pins` (F1) | red on the settle deadline with a blocked state in the dump |
| P2c/M2 | product reader error branch (`:107-108`) becomes a forever loop | `before` (old check) | red: instant frame present |
| P2c/M2 | same edit | `pins` (F1) | red on the settle deadline (goleak cleanup would also red) |
| M3 | product join select (`:121-124`) loses its `time.After(readerJoinTimeout)` arm | `pins` (F1) | sibling `TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier` reds at its 30 s `testHangGuard` |

Mutant rows name the CI job that must redden: `daemon` (`.github/workflows/ci.yml:942`); the
`daemon-windows` twin is record-only (contract Q3(A)).

## D5. Closure handling

- The moved bullet keeps its text (with the two new run ids and the named cause added), gains the
  closure statement (branch, plan, evidence, counts, fix shape), and is appended to
  `docs/archive/CLOSED-TODO.md` under the section name as filed ("three observations").
- `docs/TODO.md` keeps the section with its other two observations; the header count is reconciled
  to "two observations" because the file holds only undone items (rules `docs/TODO.md:5-13`) and a
  stale count is a defect. The archive section keeps the original name so the move is traceable;
  the CLOSED-TODO pointer paragraph says which section the moved item went into.
- No decision note changes; the section's other bullets are untouched.

## D6. Out of scope

- Product behavior, other tests, other flakes, `ci.yml` (contract clause 4). No `readerJoinTimeout`
  raise, no skip, no `t.Parallel`, no wildcard acceptance, no removed assertion.
- The unnamed CI load agent (a hosted-runner property); F1 is designed not to require it.
- The external contract file and the GDD state root (conductor-owned).

## Results (commands, counts, hashes — full rows in the evidence record)

Subject: worktree `01c0d4cf` + `daemon/cmd/symmetry-fake-agent/autonomous_test.go` sha256
`fbdcce641b743435b2b0fdb18a8c22b4d79bd2c2d4e0cbc5d5a89394681e400b` (`git diff --numstat` 16/3;
`git diff --check` clean); product blob `d82b5991…` unchanged. `<scratch>` is the disposable
scratch root outside the worktree; no absolute paths.

| row | command | exit | result |
| --- | --- | ---: | --- |
| base census | `cd <worktree>/daemon && timeout 45m go test -race -count=1 -timeout 2400s ./...` (WSL `accept`, go1.27.0) | 0 | every package `ok` (31 packages plus 3 with no test files), `cmd/symmetry-fake-agent` `13.444s`, 1028 s wall; no red anywhere |
| calibration | `go test -race -count=200 -timeout 600s -run '^TestAutonomousReturnReleasesItsInputReader$' ./cmd/symmetry-fake-agent/` (WSL, worktree) | 0 | `ok … 3.140s` (~15.7 ms/iteration; 20000 ≈ 310 s unloaded) |
| package smoke | `go test -race -count=1 -timeout 600s ./cmd/symmetry-fake-agent/` (worktree) | 0 | `ok … 24.820s` (every test in the touched package) |
| disassembly witness | §3.1 of the evidence record (`GOTOOLCHAIN=go1.27.4 go test -race -c` + `go tool objdump`) | 0 | `+0x213` = `CALL runtime.racefuncexit(SB)` after `CALL runtime.deferreturn(SB)` in the base binary at the CI compiler |
| red-before repeat | the D3 command in `<scratch>/before` beside the carrier | 1 | 5 red / 20000 (19995 pass) in 209.464 s: every red `runElapsed` 2.85–12.38 ms (< `readerJoinTimeout`) with `[runnable]` at `autonomous.go:111 +0x213` — the CI dump shape, reproduced; carrier `ok … 165.866s` |
| green-after repeat | the D3 command in the worktree beside the carrier | 0 | 0 red / 20000 (20000 pass) in 182.690 s; carrier `ok … 148.994s` |
| P2a | §5 of the evidence record | 1 / 0 | old-check red 5/5 with `runElapsed 5.07 ms`; F1 green 50/50 |
| P2b/M1 | §5 | 1 / 1 | both arms red 3/3; F1 names the settle deadline (parked `io.PipeReader.Read`) |
| P2c/M2 | §5 | 1 / 1 | both arms red 3/3; F1 names the settle deadline (spinning `runtime.Gosched`) |
| M3 | §5 | 1 | sibling red at the 30 s guard (`30.04s`) |
| WSL `-race` oracle | `timeout 30m wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd <worktree>/daemon && go test -race -count=1 -timeout 1200s -run "^(TestAutonomousReturnReleasesItsInputReader\|TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier\|TestLeakOracleDetectsLeakedGoroutine)$" ./cmd/symmetry-fake-agent/'` (`GOTOOLCHAIN=go1.27.4`) | 0 | `ok … 1.610s` (all three tests), no race report |
| assertion census / hygiene | §6 of the evidence record; `git diff --check` | 0 | test-only diff plus the owned docs; product blob `d82b5991…`; 24/24 failure sites, 1/1 `runtime.Stack` site |
| citation existence | emulation repo of the final tree (`scripts/ci/check-citation-existence.py`) | 0 | `1 changed document(s) pass the path and symbol check (609 path citation(s))` |
