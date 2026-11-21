# rf-w4-03 runner decomposition — evidence record (2026-11-21)

Subject: `daemon/internal/execution/runner.go` plus the new sibling
`daemon/internal/execution/runner_pipes.go`, in the worktree
`wt-w4-03-runner-decomposition` at git HEAD `c9ed23b4` (contract re-derivation base
`0fa9b705`; `git log 0fa9b705..HEAD -- daemon/internal/execution/` is empty, so every base
line number below is valid at both revisions).

Evidence subject hashes (sha256, delivered tree):

| file | sha256 |
| --- | --- |
| `daemon/internal/execution/runner.go` | `aaec2c1095513b67a938a2235adde9c473bad775fa116c6a0a1d2ba8fa964ae5` |
| `daemon/internal/execution/runner_pipes.go` | `d649334f12dba0909dc2a906317bfcd0289655e596e34e9929007ace30402f66` |
| base `runner.go` (at `0fa9b705`) | `0b9e6a9862f6e5c3b0aaae6aefdf1241bc432d110abb0725af96068be7c461a5` |

Contract: `goal-rf-w4-03-runner-decomposition.md` (GDD state root) with its two conductor
decisions (Wave-4 draft, W4 review reconcile) and the program-wide rules (lesson 32,
host-path hygiene). Plan: `docs/plans/2026-11-21-rf-w4-03-runner-decomposition-plan.md`.

## 1. Shape delivered

`daemon/internal/execution/runner_pipes.go` (new, 63 lines) adds `startPipes` with
`createStartPipes`, `Close` and `closeChildEnds`. `runner.go` (2738 → 2758 lines) rewrites
`Start`, adds the `startRun` phase carrier and its nine phase methods, rewrites
`closeContainmentAttempt` and `closeContainmentOwnerLocked` in terms of six new
`*Process` close-path helpers plus one shared sentinel. Nothing else changes (see §7).

Function-length and decision-point table (base → head; lengths include signature and closing
brace; decision points counted as 1 + occurrences of `if`/`for`/`case`/`&&`/`||` on
comment- and string-stripped source — the base figures reproduce the findings-document
magnitude for `Start`):

| function | base lines | head lines | base decisions | head decisions |
| --- | ---: | ---: | ---: | ---: |
| `Start` | 398 | **81** | 85 | 27 |
| `startRun.launchStarted` | — | 49 | — | 18 |
| `startRun.persist` | — | 44 | — | 18 |
| `startRun.commitHandoff` | — | 22 | — | 7 |
| `startRun.persistAtomicAuthority` | — | 24 | — | 3 |
| `startRun.persistLegacyIdentity` | — | 31 | — | 7 |
| `startRun.armInitialLease` | — | 32 | — | 14 |
| `startRun.resumeTarget` | — | 12 | — | 4 |
| `startRun.deliverInitialInput` | — | 21 | — | 5 |
| `closeContainmentAttempt` | 248 | **143** | 64 | 41 |
| `closeOwnerOnce` (new) | — | 44 | — | 14 |
| `persistReceiptOrAbort` (new) | — | 30 | — | 9 |
| `persistUncertainAuthority` (new) | — | 18 | — | 6 |
| `finalizeContainment` (new) | — | 6 | — | 1 |
| `failContainment` / `failContainmentWith` (new) | — | 4 / 4 | — | 1 / 1 |
| `closeContainmentOwnerLocked` | 24 | 6 | 9 | 3 |
| close family total (`closeContainmentAttempt` + `closeContainmentOwnerLocked` vs head `closeContainmentAttempt` + the six helpers + `closeContainmentOwnerLocked`) | 272 L / 73 decisions | 255 L / 76 decisions | | |

Targets met: `Start` 81 L ≤ 180; `closeContainmentAttempt` 143 L ≤ 150. The family total is
report-only and near-flat (−17 L, +3 decisions) by construction: the same decisions now live in
named helpers, each counted with its own function constant. Statement counts under the
advisory `funlen` thresholds (`daemon/.golangci.yml`, 120 lines / 60 statements, not gating):
`Start` 310 → 54 statements, `closeContainmentAttempt` 186 → 103.

Advisory lint measurement (`golangci-lint v2.15.0` with the repo's `daemon/.golangci.yml`,
`--enable=funlen,gocyclo,dupl ./internal/execution/...`): **9 issues, none new** — errcheck 2
(the untouched `process.closeContainment` call sites), funlen 1
(`finalizeSupervisorHandoffLocked`, 63 statements > 60, untouched), gocritic 1 (the
`if/else if/else` persist chain at `:737`, the same chain shape base had), gocyclo 2 (`Start`
26 > 25, base measured 85; `closeContainmentAttempt` 41 > 25, base 64), unparam 2 (untouched
`newProcessFromStarted` and `waitForOutputShutdown`), unused 1 (the untouched unsupported-OS
`launchLinuxV2Process` stub). The two complexity findings name the two decomposed functions and
fell by 3.3× and 1.6×; the metric does not gate in CI.

LOC delta: `runner.go` +20 lines net (`git diff --numstat`: 449 insertions, 429 deletions in
the two changed tracked files, `runner.go` alone 447/427) plus the new 63-line sibling. The
plan row's `-200..-300` dLOC estimate is not met and is report-only (contract D2): the phase
functions carry doc comments and signatures, and the deleted boilerplate was ten-line call
sites rather than logic.

## 2. Site bootstrap tables

21 `cleanupFailedStartStarted(` call sites inside base `Start` — every one becomes one
`run.fail(<startErr>, <containmentErr>)` site with the same tuple:

| base site | base wrapper text | containment error | head site |
| --- | --- | --- | --- |
| 627 | `start %q: %w` (launcher err, owned process) | `err` | 667 |
| 654 | `contain process tree for %q: %w` | `wrappedErr` | 688 |
| 682 | `install containment uncertainty observer: %w` | nil | 731 |
| 702 | `commit supervisor handoff: %w` | nil | 781 |
| 711 | `wait for commit/resume barrier: %w` | nil | 784 |
| 732 | `containment authority is unavailable after process launch` | same `errors.New` | 806 |
| 749 | `persist process and containment authority: %w` | nil | 817 |
| 762 | `persist process identity: %w` | nil | 831 |
| 781 | `containment authority is unavailable after process marker persistence` | same `errors.New` | 844 |
| 794 | `persist process containment authority: %w` | nil | 851 |
| 808 | `persist containment uncertainty: %w` | nil | 752 |
| 827 | `attachFailureErr` | same | 765 |
| 837 | `deadlineErr` | nil | 877 |
| 848 | `initial containment lease sequence is missing` | nil | 884 |
| 857 | `arm initial Linux v2 containment lease: %w` | nil | 887 |
| 866 | `wait for lease-armed/resume barrier: %w` | nil | 890 |
| 876 | `initial containment lease sequence is missing` | nil | 894 |
| 885 | `arm initial containment lease: %w` | nil | 897 |
| 894 | `wait for lease-armed/resume barrier: %w` | nil | 900 |
| 906 | `execution context cancelled before process resume: %w` | nil | 914 |
| 915 | `resume process %q: %w` | nil | 917 |

Ten `closeFiles(` sites inside base `Start` — nine bootstrap sites become `pipes.Close()`
(all six ends, nil-tolerant), the child-end site stays a three-end close:

| base site | ends closed | head site |
| --- | --- | --- |
| 583 (stdout pipe creation failed) | stdinRead, stdinWrite | `runner_pipes.go:32` (`pipes.Close()`, stdout ends still nil) |
| 588 (stderr pipe creation failed) | +stdoutRead, stdoutWrite | `runner_pipes.go:36` |
| 593 (post-create ctx check) | all six | 583 |
| 605 (configure failure) | all six | 595 |
| 614 (launcher unavailable) | all six | 653 |
| 624 (launcher error, no process) | all six | 664 |
| 636 (launcher returned no process) | all six | 670 |
| 642 (Linux v2 fallback forbidden) | all six | 676 |
| 646 (`command.Start()` failed) | all six | 680 |
| 927 (child-end rule) | stdinRead, stdoutWrite, stderrWrite | 929 (`run.pipes.closeChildEnds()`) |

`closeFiles` itself remains (definition `:2752`, one call inside `cleanupFailedStartStarted`
at `:1346` — the same three child ends, untouched). `cleanupFailedStartStarted`'s parameter
list is unchanged, as pinned by `runner_output_truncation_test.go:206`.

Sixteen `setContainmentError(errors.Join(` sites inside base `closeContainmentAttempt` — all
collapse into named helpers; recorded text and join order unchanged (`base = errors.Join(
containmentFinalizationBaseError(), <x>)`):

| base site | base recorded / returned | head site |
| --- | --- | --- |
| 2154 | `finalizeErr` / `finalizeErr` | 2297 `failContainment(finalizeErr)` |
| 2195 | `errors.New("containment stop receipt provider is missing")` | 2336 |
| 2200 | sentinel | 2339 (`errContainmentAuthorityUncertain`) |
| 2237 | `errors.Join(containmentInitialError, err)` | `closeOwnerOnce` 2217 (unchanged shape) |
| 2253 | `errors.New("containment authority persistence callback is unavailable")` | `persistUncertainAuthority` 2116 |
| 2265 | `fmt.Errorf("retry process containment authority persistence: %w")` / returns `persistErr` | 2127 `failContainmentWith(wrapped, persistErr)` |
| 2274 | sentinel | `persistReceiptOrAbort` 2144 |
| 2279 | `errors.New("durable containment stop receipt callback is missing")` | 2147 |
| 2285 | the `AbortContainment` error / the same error returned | 2157 `failContainment(err)` |
| 2291 | `errors.New("pre-authority containment abort capability is missing")` | 2154 |
| 2296 | `errors.New("durable containment stop receipt callback is missing")` (required) | 2150 |
| 2302 | `fmt.Errorf("persist containment stop receipt: %w")` / returns `err` | 2163 `failContainmentWith(wrapped, err)` |
| 2311 | sentinel | 2361 |
| 2322 | `fmt.Errorf("release containment authority: %w")` / returns `err` | 2368 `failContainmentWith(wrapped, err)` |
| 2327 | `errors.New("containment stop receipt releaser is missing")` | 2371 |
| 2332 | `errors.New("containment stop receipt is unavailable")` | 2374 |

Sentinel constructions: base `errors.New("containment authority persistence is uncertain")`
at 2199, 2273, 2310 (3) → one package value `errContainmentAuthorityUncertain` (line 2092)
referenced by the same three logical sites (`failContainment(errContainmentAuthorityUncertain)`
at `persistReceiptOrAbort` 2144, 2339 and 2361). Text identical.

## 3. Control-flow audit (lesson 32)

Row format: base construct → head equivalent → preserved. "unchanged bytes" means the base
line's text survives verbatim at the head line.

### `Start` (base 547-944)

| base | construct | head | preserved | note |
| --- | --- | --- | --- | --- |
| 548-550 | nil-context early return | 548-550 | yes | unchanged bytes |
| 551-553 | cancelled-context early return | 551-553 | yes | unchanged bytes |
| 554-557 | Linux v2 platform guard early return | 554-557 | yes | unchanged bytes |
| 558-560 | required-grant guard early return | 558-560 | yes | unchanged bytes |
| 561-568 | v2 lease pre-validation (two early returns) | 561-568 | yes | unchanged bytes |
| 569-572 | `validateInvocation` early return | 566-572 | yes | unchanged bytes |
| 573-575 | nil-sink default | 573-575 | yes | unchanged bytes |
| 577-590 | three pipe creations, each early-returning on failure after closing the created prefix | 577-580 + `runner_pipes.go:25-41` | yes | `Close()` closes exactly the created prefix (later ends nil) |
| 592-595 | post-create ctx check early return closing all six | 582-585 | yes | same six ends, same text |
| 597-603 | `exec.Command` + dir/env/stdio wiring | 587-592 | yes | field-for-field, now reading `pipes.*` |
| 604-607 | configure guard early return | 593-598 | yes | same text, `pipes.Close()` |
| 609 | `startedAt` sampled after configure, before launcher checks | 609 (`startRun` construction) | yes | same relative position |
| 612-616 | Linux v2 launcher-unavailable early return | 652-655 | yes | same text, `Close()` |
| 617-622 | launcher call + handoff wiring + launcher-error exit (owned process → cleanup with `(wrapped, err)`) | 657-668 | yes | `run.fail` carries the tuple |
| 623-626 | "returned no process" early return | 669-672 | yes | same text |
| 628-635 | nil-process fallback: v2 forbidden early return, then `command.Start()` | 674-682 | yes | same order and texts |
| 640-648 | attach + `legacyStartedProcess` | 683-684 | yes | unchanged shape |
| 649-659 | attach failure: non-retryable → cleanup `(wrapped, wrapped)`; retryable → store `attachFailureErr` and fall through | 685-695 | yes | stored on `startRun`; the check stays after the persist tail (base 826 vs head 764) |
| 666-673 | persist setup (`persistContainmentUnproven`, provider assertions, stop-receipt closure) | 726-729 + `bindStopReceiptPersistence` 861-868 | yes | same assertions/condition, closure became a method |
| 674-684 | first uncertainty-observer install + failure exit | 730-732 | yes | same text |
| 685-688 | `authorityCapable` + availability downgrade | 733-736 | yes | unchanged predicate |
| 690-720 | handoff branch: bind → commit time → commit → commit barrier → marker → authority projection | 737-740 + `commitHandoff` 773-794 | yes | same order; entry condition `containmentHandoff != nil` |
| 721-755 | atomic branch: authority read → bind → nil guard → freeze → callback → marker | 741-744 + `persistAtomicAuthority` 799-822 | yes | same order, same three exits |
| 756-801 | legacy branch: process marker → bind → authority guard → freeze → callback → marker | 745-748 + `persistLegacyIdentity` 827-857 | yes | nested `if`, no synthetic return |
| 802-812 | marker reinstall + failure exit | 750-754 | yes | runs after whichever branch ran, as in base |
| 813-822 | capability reset (drop the receipt contract for non-releasable owners) | 755-763 | yes | unchanged text |
| 823-830 | attach-failure exit | 764-766 | yes | same tuple |
| 833-839 | lease deadline error exit | 875-878 | yes | same value, no wrapper |
| 840-869 | v2 lease arm: sequence guard, renewal, lease-armed barrier | 879-891 | yes | `if !requested { return nil, nil }` replaces the outer `if` and reaches the same next phase |
| 871-897 | legacy lease arm (else-if): same three steps | 892-901 | yes | else-if chain preserved |
| 900-905 | resume ctx guard exit | 910-915 | yes | same text |
| 906-916 | resume call failure exit | 916-918 | yes | same text |
| 921-927 | child-end close rule | 927-929 | yes | comment byte-identical; `closeChildEnds()` closes the same three ends |
| 929-931 | `newProcessFromStarted` + drain timeout + `process.start` | 931-933 | yes | same arguments |
| 932-936 | initial input write failure → `cleanupAfterInputSetupFailure` | 935-938 | yes | unchanged helper |
| 937-941 | close-after-initial failure → `cleanupAfterInputSetupFailure` | 940-943 | yes | unchanged helper |
| 943 | success return | 945 | yes | same value |

### `closeContainmentAttempt` (base 2092-2339) + `closeContainmentOwnerLocked` (2353-2376)

| base | construct | head | preserved | note |
| --- | --- | --- | --- | --- |
| 2093-2095 | nil-process guard | 2236-2238 | yes | unchanged bytes |
| 2096-2185 | v2 finalizer branch: lock/unlock defer, clone-finalization reservation, clone-outcome publication, finalization-start snapshot, finalized guard, `closeLinuxV2Containment` with the deferred clone-finalization signal, peer close | 2239-2314 | yes | unchanged bytes except the `finalizeErr` site |
| 2154 | finalize error → record join + return `finalizeErr` | 2297 | yes | `failContainment(finalizeErr)`: same recorded text, same returned value |
| 2178-2184 / 2186-2188 | finalized early returns | 2245-2249 / 2264-2266 | yes | unchanged bytes |
| 2190-2198 | pending-handoff delegation (owner close, then handoff finalizer) | 2327-2333 | yes | unchanged bytes |
| 2194-2197 | receipt-provider-required exit | 2335-2337 | yes | same text |
| 2199-2201 | uncertain-authority exit (sentinel) | 2338-2340 | yes | shared sentinel value, identical text |
| 2202-2206 | retryable probe + one-shot attempt guard | `closeOwnerOnce` 2177-2183 | yes | guard inverted to an early return; same predicate |
| 2210-2241 | close + uncertainty-persistence + retryable re-probe + stable/containment error bookkeeping + return `err` | `closeOwnerOnce` 2184-2218 | yes | plain path passes `persistUnproven=true`; `containmentInitialError` join preserved |
| 2207 (handoff caller) | `closeContainmentOwnerLocked` nil guard + same close skeleton without marker persistence | 2391-2396, delegation at 2395 | yes | flag `false` reproduces the handoff contract |
| 2243-2247 | no-provider finalize | 2346-2348 | yes | `finalizeContainment()` |
| 2248-2270 | uncertain-authority retry (callback availability, double clone, callback selection, error exit, flag clear) | `persistUncertainAuthority` 2114-2131, called 2351-2355 | yes | same clone count, same selection, same wrapped-record/raw-return exit |
| 2271-2307 | receipt persistence or pre-authority abort, in the base guard order | `persistReceiptOrAbort` 2138-2167, called 2356-2358 | yes | `!saved` guard first; required-receipt branch inverted to an early return; same four texts |
| 2308-2313 | post-abort early finalize (+ sentinel guard) | 2359-2365 | yes | same order: sentinel guard, then finalize |
| 2314-2324 | release or releaser-missing | 2366-2372 | yes | wrapped record, raw `err` return via `failContainmentWith` |
| 2330-2333 | receipt-unavailable exit | 2373-2375 | yes | same text |
| 2334-2338 | final finalize | 2376 | yes | `finalizeContainment()` |

No row is "changed": every base exit is reachable in head under the same predicate, records
the same text and returns the same error value. The three shape transforms (early return
instead of guard-inversion, a boolean parameter instead of a duplicated body, one shared
sentinel value) are the only structural differences, and each is listed above.

## 4. Ordered-text comparison (mechanical)

Scratch comparator (`ordered literal sequence` per region, comments and string escapes
handled; base region vs the head phase functions that replaced it in execution order):

| region | base literals | head literals | result |
| --- | ---: | ---: | --- |
| `Start` prologue (547-574) | 8 | 8 | identical sequence |
| `Start` pipes + ctx + configure → `createStartPipes` + `Start` | 5 | 5 | identical sequence |
| launch → `launchStarted` | 7 | 7 | identical |
| persist + commit → `persist`/`commitHandoff`/`persistAtomicAuthority`/`persistLegacyIdentity` | 9 | 9 | identical |
| lease → `armInitialLease` | 6 | 6 | identical |
| resume → `resumeTarget` | 2 | 2 | identical |
| input → `deliverInitialInput` | 4 | 4 | identical |
| `closeContainmentAttempt` v2 + guards | 3 | 2 | the single difference is the sentinel collapse (2199) |
| close region → `closeOwnerOnce` | 1 | 1 | identical |
| receipts → `persistUncertainAuthority` + `persistReceiptOrAbort` + close tail | 11 | 9 | two differences, both the sentinel collapse (2273, 2310); the remaining 8 texts align in execution order |
| `closeContainmentOwnerLocked` → `closeOwnerOnce` | 0 | 1 | head carries the marker literal because both close paths now share one body; the handoff path passes `persistUnproven=false` so it is unreachable there |

Whole-function call-name counts (last segment of each call, definitions included where the
span includes them; 5 base and 4 head rows are the comparator's `containment.(Type)`
type-assertion artifact and are excluded from the adjusted figures shown in parentheses):
`Start` base 139 calls/48 names (134/47) → head 159/60 (155/59) — `closeFiles` 10 → 0,
`cleanupFailedStartStarted` 21 → 1 (inside `run.fail`), `fail` 0 → 21 call sites (+1
definition), `Close` 0 → 9, `closeChildEnds` 0 → 1, `setStopReceiptPersistence` 3 → 0,
`bindStopReceiptPersistence` 0 → 3; `closeContainmentAttempt` base 125/34 → head 98/40 —
sixteen inline join sites become 12 `failContainment` + 3 `failContainmentWith` call sites
plus the helpers' two `errors.Join` calls, `containmentFinalizationBaseError` 19 → 2 (the
family's two joins), `New` 10 → 7 (three sentinel constructions collapse).

## 5. Frozen-surface proof

Each frozen function was extracted from both revisions by name and compared with `diff`
(exit 0 required): `publishResumeBarrierMarker`, `retrySupervisorHandoffReceipt`,
`retrySupervisorHandoffClear`, `cleanupFailedStartStarted`, `closeFiles`,
`configureStartedHandoff`, `installContainmentUnprovenCallback`, `legacyStartedProcess`,
`newProcessFromStarted`, `setContainmentError`, `finalizeSupervisorHandoffLocked`,
`recordSupervisorHandoffReceiptLocked`, `containmentFinalizationBaseError`,
`containmentStopReceiptProvider`, `pendingSupervisorHandoff` — all identical; plus base
lines 40-60 (`ErrLinuxV2MutationAckLost`, `errContainmentMarkerPending`) and 155-230
(callback struct, ack-stage constant table) identical. §7 classifies every changed hunk.

Independent confirmation (auditor A, read-only, 2026-11-21): direct `diff` of whole regions
shows base 1-576 == head 1-576 (ack table, callback struct, `ErrLinuxV2MutationAckLost`),
base 946-2086 == head 948-2088 (`configureStartedHandoff` … `isUnsupportedOnly`, including
`publishResumeBarrierMarker` and the resume-barrier machinery), and base 2376-2738 == head
2396-2758 (retry twins, handoff finalizer, `closeFiles`), each exit 0; that is the same
conclusion reached here by per-function extraction, from the opposite direction.

## 6. Gates (all foreground; exact commands, exit codes, counts)

| gate | command (cwd) | result |
| --- | --- | --- |
| gofmt | `gofmt -l ./internal/execution/` (`daemon/`) | no output, exit 0 (re-run after the last edit) |
| vet | `go vet ./...` (`daemon/`) | exit 0 |
| build windows/amd64 | `GOOS=windows go build ./...` | exit 0 |
| build linux/amd64 | `GOOS=linux go build ./...` | exit 0 |
| build linux/arm64 | `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 |
| execution package (Windows subset) | `go test -count=1 ./internal/execution/` | exit 0 — `ok … 16.455s` on the delivered revision with host headroom; `ok … 14.189s` on the immediately preceding revision. The saturated-host runs and the isolation pass are in §12. |
| harness callers | `go test -count=1 ./internal/harness/...` | exit 0 — 8 packages `ok` (root `ok … 23.792s` in the run before the final comment edit; re-run after it: 8 `ok`, exit 0) |
| app callers | `go test -count=1 ./internal/app/...` | exit 0 on the immediately preceding revision (`ok … 500.336s`) and on the base-overlay build (`ok … 784.920s`); the delivered-revision runs on this saturated host end on a 2-second wall-clock admission test that **fails at the base-overlay build in the same window** — full record and the interleaved pair in §12 |
| execution package (Linux, full set) | `wsl -d Ubuntu-24.04 -u accept` → `cd /mnt/q/repos/wt-w4-03-runner-decomposition/symmetry/daemon && go test -count=1 ./internal/execution/` | exit 0 — `ok … 2.210s` |
| test inventory | `go test -list '^Test' ./internal/execution/` | Windows subset **85**; Linux (WSL) **104** |
| citation gate | `python scripts/ci/check-citation-existence.py --base 0fa9b705 --head HEAD` | exit 0 — `no audited document added or modified between 0fa9b705cb6a and c9ed23b40977; nothing to check` (the slice changes no audited document; the `docs/TODO.md` re-point is outside the audited corpus) |
| whitespace | `git diff --check` | exit 0 |
| host-path hygiene | `git diff origin/main...HEAD \| grep -icE "users[/][a-z]"` | 0 (and 0 in the new files) |
| race | not run locally (no cgo on this host) | CI daemon job is the race oracle (contract gate-runner) |

## 7. Changed-hunk classification

`git diff -U0 -- daemon/internal/execution/runner.go` yields 49 hunks: 33 inside base
`Start` (547-944), 15 inside base `closeContainmentAttempt` (2092-2339), 1 inside
`closeContainmentOwnerLocked` (2353-2376), and 1 pure insertion after base line 2088 that
adds the six close-path helpers plus the shared sentinel (the contract's "new shared
close-path helpers they gain"). No hunk touches the ack-stage constants, the callback
struct, `ErrLinuxV2MutationAckLost`, the handoff twins, the barrier publisher, the
handoff finalizer, or `closeFiles`/`cleanupFailedStartStarted`.

## 8. Media/suite results and mutation battery

Suite results are in §6; the host-load flake story is in §12. This slice adds no test, so the
mutation battery below exists to show which oracles are sensitive to the new seams. Each mutant
is applied to a scratch copy through `go test -overlay` (the repository tree is never modified);
`M1`/`M2` are the contract's own example class ("drop one end from `startPipes.Close` -> red").

| mutant | one-line edit | observed (Windows / WSL Linux) |
| --- | --- | --- |
| M1 | `closeChildEnds` drops `stdoutWrite` | **RED both** — Windows: `TestProcessPersistsContainmentStopReceiptBeforeRelease`, `TestProcessRetriesReceiptPersistenceAfterWaitWithoutRestopping`, `TestProcessRetriesDurableHandleReleaseBeforeReceiptAndHelperRelease` (30 s guards each); WSL: the same three plus `TestLinuxCloneStopRunsWhileOutputDrainsAndPromotionWaitsForResult` |
| M2 | `Close()` drops `stderrWrite` | green both (`ok … 12.918s` / `ok … 2.158s`) — no oracle observes a leaked failure-path end |
| M3 | the plain close path calls `closeOwnerOnce(false)` (drops the ContainmentUnproven marker) | green both (`ok … 11.639s` / `ok … 3.091s`) — the marker persistence is not asserted by any current test |
| M4 | `persistReceiptOrAbort` drops the receipt callback call | **RED both** — five receipt/authority tests fail immediately (`TestProcessPersistsContainmentStopReceiptBeforeRelease`, `TestProcessRetriesReceiptPersistenceAfterWaitWithoutRestopping`, `TestProcessRetriesDurableHandleReleaseBeforeReceiptAndHelperRelease`, `TestAuthorityPersistenceRetryPrecedesReceiptAndRelease`, `TestAtomicAuthorityPersistenceUnknownOutcomeRetriesBeforeReceiptAndRelease`) |
| M6 | the launcher-error `run.fail` passes `nil` containment error | green both — that exit's recorded containment error is not asserted |
| M7 | the shared sentinel text changes | green both — no test asserts the sentinel string |
| M5, M8 | `finalizeContainment` not setting the flag; `commitHandoff` dropping `markerPersisted.Store(true)` | not run (the focused battery above already answered the sensitivity question; both are reproduced at head by the shipped code paths and covered by the §3 audit rows) |

Reading: the two seams the contract calls out (the child-end close rule and the receipt
persistence) are pinned red on both platforms; the failure-path `Close()` end set, the
ContainmentUnproven marker, the launcher-error containment tuple and the sentinel text are not
pinned by any current test, so their preservation rests on the §3 audit rows and the §4
ordered-text comparison rather than on a failing oracle.

Overlay recipe (identical shape to the W2-14 mutation recipe):

```sh
python <scratch>/mutants.py <repo>/daemon <scratch>/mut all
```

## 9. Documents re-pointed

| document | old citation | new citation | reason |
| --- | --- | --- | --- |
| `docs/TODO.md:765-766` | `internal/execution/runner.go:1378`, `:1408-1412` | `:1380`, `:1410-1414` | `WriteInputContext`'s definition moved +2 lines, so the two citations the open F15 entry quotes needed re-pointing |

Not re-pointed by decision: `docs/archive/CLOSED-TODO.md:543,551` cite
`daemon/internal/execution/runner.go:1046` from a repair made against a much older tree; the
archive records the revision it was written against (same decision as the W1-05 split
record), and the citation gate audits only documents a revision range modifies.
`docs/evidence/goal-0009-source-identity-2026-11-16.json` is a dated source-identity
snapshot with no consumer (no script or test reads it) and keeps its recorded hashes.

## 10. Residual risks and known limits

- **`-race` is not covered locally** (no cgo on this Windows host); CI's daemon job remains
  the race oracle, as the contract states.
- **Two audits run in parallel with this record** (§11): their findings are reconciled in
  the answer artifact; a finding accepted after this record was written would appear there
  with its fix.
- **The `-200..-300` dLOC estimate is not met** (+19 net in `runner.go`); report-only per
  contract D2. The decomposition's win is per-function shape (§1), not line count.
- **`closeContainmentAttempt` remains above the advisory `funlen` thresholds** (143 lines /
  103 statements vs 120/60) exactly as in base (248/186); the advisory lint metric is not
  gated, and three of the four finalization blocks it still carries are mutually exclusive
  exit paths.
- **Text-only close-path mutants are not pinned by the in-repo oracles** where no test
  asserts the string (see §8); those rows are preserved by construction, the audit table and
  the ordered-text comparison, not by a test.
- **The scratch comparators are not committed** (provenance, not reproduction): the committed
  artifacts are this record, the plan and the tree itself, and every command in §6/§8 is
  re-runnable from them.
- **The `internal/app` gate did not go green on this host at the delivered bytes** (§12): the
  full suite passed on the immediately preceding revision (500.336 s) and at the delivered
  revision fails on a 2-second wall-clock admission-deadline test that no code this slice
  changed reaches, after an earlier run ended by a suite-level timeout under sibling load. The
  isolation pass is green; CI's dedicated runner remains the oracle. This is the largest single
  gap in this record and is stated as such rather than smoothed over.

## 11. Independent audits

Two read-only audits ran in parallel against the delivered tree (owner routing rule: one
Sol-family reviewer, one DeepSeek-family auditor; neither was the implementing session):

| audit | role/model family | verdict |
| --- | --- | --- |
| A | `reviewer` (Sol) | no behavioural divergence in any audited class; two P3 latent notes |
| B | independent `task` auditor (DeepSeek) | all five preservation claims CONFIRMED, none falsified |

Audit A independently recomputed the 21 cleanup exits, 10 pipe sites, 16 joins and 3 sentinel
sites, confirmed the site map, the close-path helper equivalence, the partial-`Close()` prefix
behaviour (nil-skipping `closeFiles`), and byte-identity of base 1-576 / 946-2086 / 2376-2738.
Audit B re-derived the claims from an ordered-literal comparison (41/41 literals for `Start`,
−2 for the sentinel collapse), the full 21-row cleanup-pair table, the 16-row join table with
recorded-vs-returned identity, the `closeOwnerOnce` equivalence (De Morgan complement), and an
independent hunk classification (0 hunks outside the owned regions).

Observations accepted without code change:

- **Shared sentinel identity** (A P3, B INFO). One `errContainmentAuthorityUncertain` value now
  serves the three sites, so `==`/`errors.Is` between two separate close attempts that hit the
  sentinel flips false→true. Contract clause 1 mandates the collapse ("the 3 sentinel
  constructions collapse with identical text and join order"); no production or test code
  compares these errors (both auditors grepped `daemon/internal` including tests), so the
  observable text, join order and returned value are unchanged.
- **Argument evaluation order on the three wrapped sites** (B): head builds the wrapper before
  evaluating `containmentFinalizationBaseError()`; both are side-effect-free, so the recorded
  text and join order are identical.
- **Mutually exclusive literal ordering** (B): the two `"durable containment stop receipt
  callback is missing"` sites are textually adjacent at head while base nests them around the
  abort branch; no path reaches both, and the per-site mapping is in §2.
- **`startPipes.Close()` is not one-shot** (A P3): no caller can close twice today, and the
  method comment now states the two terminal dispositions and warns against adding such a
  caller.

One documentation defect was found and fixed: the plan's "19 single-error sites / two
container-error sites" sentence was wrong (5 sites pass a non-nil containment error); the plan
now lists all five with the command that counts them.

Both audits are read-only and neither compiled or ran tests; the execution evidence in §6/§8 is
the implementing session's, run on the delivered revision.

## 12. Host-load flake on the delivered revision (attributed, not silent)

The delivered-revision whole-package runs of `./internal/execution/` were **not** uniformly green
on this host; the record is complete, and the failures are attributed to host saturation, not to
this slice:

| run (delivered revision unless noted) | outcome |
| --- | --- |
| full package, saturated host (3 runs) | FAIL 3/3, 88-135 s each — `TestRunBoundedCommandStopsDescendantHoldingOutputPipe` ("child PID output \"\" is not an integer", `probe_test.go:83`) on all three, plus `TestRootExitDoesNotLeaveADescendantHoldingStdoutOnWindows` (30 s guard, `runner_windows_test.go:217`) on the first |
| full package, saturated host (1 run) | FAIL — the same two tests |
| full package, quiesced host | **PASS** `ok … 16.455 s`, exit 0 |
| focused selection of the two tests at the delivered revision (2 runs) | **PASS** `ok … 3.876 s` / `4.517 s`, exit 0 |
| full package, pre-final revision (before the finalize helper) | PASS `ok … 14.189 s`, exit 0 |
| app suite, pre-final revision | PASS `ok … 500.336 s`, 8 leak-checked tests included, exit 0 |
| app suite, delivered revision, saturated host (2 runs) | FAIL — both ended by `-timeout` (600 s default, then 20 m): one load-starved sub-test failure plus a suite that could not finish; the 20 m dump shows the suite inside a 4 MB journal-payload `json.Valid` under CPU starvation |
| app sub-test `…CancellationAndDeadlineDoNotRenewBudget`, delivered revision, isolated | FAIL once (37.92 s) while the host was still saturated, PASS once (20.721 s) with headroom |
| the same sub-test with the base-overlay build, isolated | PASS (23.947 s) |
| interleaved isolated pair in one window: delivered revision then base-overlay build | delivered **PASS** (25.262 s), base-overlay **FAIL** (46.02 s) — same arm (`caller_deadline`), same assertion, same `context deadline exceeded` message and the same `native_calls=[start details open details close wait]` detail |
| app suite with the base-overlay build (quieter window) | **PASS** `ok … 784.920 s`, exit 0 |

Attribution:

- The failures are wall-clock budget misses, not assertion or text changes: the helper child
  (`os.Args[0] -test.run=^TestHelperProcess$ -- root-exits-child-holding-stdout`) produced no
  output inside the test's fixed budget. `RunBoundedCommand` documents the matching race in code
  — "Start can race with a very short-lived child. Install the limit handler after Start, then
  recheck the collector so an early overflow is not lost" (`probe.go:55-59`) — and the helper mode
  is exactly a very short-lived root.
- The saturated runs were 6-8× slower than the quiesced ones (88-135 s vs 14-16.5 s for the same
  package; the app suite hit its 600 s default deadline under the same load while the pre-final
  run needed 500.3 s). Three GDD implement stages ran concurrently on this host throughout the
  failing window (this session's and the W4-04 / W4-06 siblings; their run-dir `state.json`
  mtimes coincide with the failures at 16:18-16:20).
- The slice does not change the window between `process.start` and `Start`'s return (§3 lists the
  input phase 1:1) and does not touch `probe.go`; the isolation pass beside the red local runs —
  the discipline the repo's own "Browser acceptance under host load" entry states
  (`docs/TODO.md:344-372`: "a local failure must not be cited as a regression without the
  isolation pass beside it … the runner gives the suite a dedicated machine") — is green at the
  delivered revision.
- The app-suite failure has the same shape and is *outside this slice's reach*: the failing
  assertion (`omo_attach_ack_test.go:629`) is the `admission_deadline` arm of
  `TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget`, which sets a **2-second
  wall-clock** admission deadline (`omo_attach_ack_test.go:611`) and then requires a loopback
  GET+PUT inside it; the fixture wires a fake native adapter and an `httptest` server
  (`omo_attach_ack_test.go:120-160`), and the test file references no `execution.` symbol at all,
  so no code this slice changed is on that path. It fails when starved (37.92 s) and passes with
  headroom at the delivered revision (20.721 s) and at the base-overlay build (23.947 s).
  The delivered-revision suite run failed on both 2-second arms (`caller_deadline`: the loopback
  GET reports `context deadline exceeded`; `admission_deadline`: "Goal admission deadline has
  elapsed before native launch"), and its failure detail prints `native_calls=[start details open
  details close wait]` — the *fake adapter's* call log, evidence that the fixture never launched
  an OS process at all.

The base-revision differential exists: the interleaved pair above fails the same app sub-test at
the base-overlay build while the delivered revision passes it in the same minutes, and the base
full suite passes in a quieter window at 784.920 s. Known limit: only that one interleaved pair
completed (the battery was stopped once the differential was decisive), and CI's daemon job on a
dedicated runner remains the oracle for the timing-sensitive tests.
