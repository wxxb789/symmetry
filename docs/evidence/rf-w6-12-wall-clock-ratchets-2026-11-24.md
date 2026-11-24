# rf-w6-12 wall-clock burn-down part 2 + duration ratchets blocking — evidence record

Base `f731e04e` (`origin/main` at dispatch). Contract:
`goal-rf-w6-12-wall-clock-ratchets.md` (GDD state root, external to the repository), binding
conductor decisions included. Plan: `docs/plans/2026-11-24-rf-w6-12-wall-clock-ratchets-plan.md`.
Shape references: the rf-w4-13 plan (conversions, census, mutants), the rf-w6-10 plan/evidence pair
(the flip with a committed baseline artefact, a fail-closed parser and its suite) and
`docs/evidence/rf-w6-11-styler-jscpd-2026-11-23.md` §9 (the jscpd step text).

## 1. Method and toolchain

- Windows x64, `go1.27.4`; WSL Ubuntu-24.04 for the linux-only rows (never concurrently with a
  Windows suite). Elixir through `mise x elixir@1.20.4-otp-29 erlang@29.1.2 -- mix`, PostgreSQL 18.6
  on the host's local instance with the slice-owned database `symmetry_control_test_w612`
  (`MIX_TEST_PARTITION` unset; stale `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset).
- Frontend through `mise x pnpm@12.17.0 -- pnpm --trust-lockfile --config.verify-deps-before-run=false`.
  Host deviation: the local package mirror lacks `knip@6.46.1`, `@vitest/coverage-v8@5.1.1` and
  `msw@3.0.7`, so one install ran with a temporary `pnpm-workspace.yaml` overrides block
  (`6.45.1`/`5.1.0`/`3.0.5`, the rf-w5-04 precedent); both `pnpm-workspace.yaml` and `pnpm-lock.yaml`
  were restored byte-identically immediately after (sha256 checked: workspace
  `c745d33e…`, lock `4df25bc4…`). The CI job installs the locked versions, so the provider-version
  deviation is a local limit, not a change to the gate.
- Every command was run foreground and bounded with `timeout`; no background job was left pending.

## 2. What changed

| file | numstat (add/del) | change |
| --- | --- | --- |
| `.github/workflows/ci.yml` | 22 / 9 | the three flips, the Go floor step, the jscpd step, the two new suite wire-ups |
| `scripts/ci/go-test-durations.py` | 90 / 25 | adoption roster with measured ceilings, ceiling verdict, partial-run detection |
| `scripts/ci/test-go-test-durations.py` | new 252 lines | the duration gate's suite (16 tests) |
| `scripts/ci/assert-go-coverage-floor.py` | new 125 lines | the fail-closed floor parser and verdict |
| `scripts/ci/go-coverage-baseline.json` | new 15 lines | the four measured runs and the 54.664 floor |
| `scripts/ci/go-coverage-capture-fixture.txt` | new 19 lines | a verbatim `go tool cover -func` capture |
| `scripts/ci/test-assert-go-coverage-floor.py` | new 163 lines | the floor gate's suite (17 tests) |
| `scripts/ci/test-ci-changed-path-classifier.py` | 72 / 26 | the blocking check, the Go-floor and vitest pins, four new mutations |
| `scripts/ci/test-ci-structure-extraction.py` | 61 / 11 | allowlists, the duration-gate pin, three mutations |
| `frontend/vitest.config.ts` | 7 / 0 | the coverage thresholds |
| `daemon/internal/notification/client_test.go` | 19 / 5 | the conversion (§6.1) |
| `control/test/…/portal_goal_attention_test.exs` | 20 / 5 | the conversion (§6.2) |
| `frontend/src/app/router.test.tsx` | 2 / 2 | two converted waits (§6.3) |
| `frontend/src/app/selected-project.test.tsx` | 2 / 2 | two converted waits (§6.3) |
| `frontend/src/features/chat/chat.test.tsx` | 1 / 1 | the `openChat` wait (§6.3) |
| `frontend/src/features/connections/connections.test.tsx` | 2 / 2 | two converted waits (§6.3) |
| `frontend/src/features/goals/goal-attention.test.tsx` | 1 / 1 | one converted wait (§6.3) |
| `frontend/src/features/work/assignment.test.tsx` | 1 / 1 | the `openAssignmentEditor` wait (§6.3) |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | 19 / 10, 24 / 0 | the closed partial-run entry and the adopted-rows debt |
| `docs/plans/2026-11-24-rf-w6-12-wall-clock-ratchets-plan.md` | new 280 lines | the plan |
| `docs/evidence/rf-w6-12-wall-clock-ratchets-2026-11-24.md` | new 340 lines | this record |

No production file change: the only production edits were temporary mutants, each restored and
verified (§7).

## 3. The duration flip

`scripts/ci/go-test-durations.py` keeps `MAX_TEST_SECONDS = 30.0`, `P95_LIMIT_FACTOR = 1.2` and
`P95_BASELINE_SECONDS = 0.26`; the cap verdict gains the committed adoption roster, whose ceiling per
row is `ceil(measured max × 1.2)` over the ten recorded runs:

| package | test | measured max (s) | ceiling (s) | runs over cap |
| --- | --- | ---: | ---: | ---: |
| `internal/app` | `TestOutputBackpressureRecoversWithOneTruncationMarker` | 167.20 | 201 | 10 / 10 |
| `internal/app` | `TestRawOutputBudgetDropsWithoutFailingSemanticEvents` | 124.96 | 150 | 10 / 10 |
| `internal/state` | `TestProviderActionCompletionRejectsOverLimitWorstCaseUnknownWithoutMutation` | 34.58 | 42 | 7 / 10 |
| `internal/state` | `TestProviderActionCapacityFailsBeforeDispatchWhenJournalByteBudgetIsExceeded` | 32.91 | 40 | 4 / 10 |
| `internal/state` | `TestProviderActionCapacityReservesWorstCaseFailureCodeBeforeDispatch` | 33.88 | 41 | 8 / 10 |

Ten green `main` runs (2026-11-22/23), per-run p95 and over-cap rows:

| run | tests | p95 (s) | over-cap rows (s) |
| --- | ---: | ---: | --- |
| 45047053979 | 5714 | 0.25 | app 167.20/124.96; state 34.58/33.78/32.33 |
| 45087709518 | 5722 | 0.25 | app 162.48/123.65; state 33.15/32.69 |
| 45098932836 | 5872 | 0.21 | app 135.42/112.51; state 31.37/31.60 |
| 45101489043 | 5872 | 0.22 | app 163.59/124.57; state 33.78/31.29 |
| 45109921118 | 5872 | 0.23 | app 161.41/122.93; state 33.04/32.74 |
| 45119101867 | 5872 | 0.24 | app 159.14/120.36; state 32.70/30.68 |
| 45120528766 | 5872 | 0.24 | app 163.90/124.76; state 33.88/33.69/31.60 |
| 45154249296 | 6072 | 0.17 | app 125.00/93.64 |
| 45186872699 | 6079 | 0.16 | app 124.62/94.06 |
| 45200375602 | 6086 | 0.21 | app 163.79/124.77; state 34.42/32.91/30.24 |

p95 min 0.16 / max 0.25 / mean 0.218 against the committed 0.26 baseline and the 0.312 limit: the
variance stays inside the band (max observed ratio 0.96× of the baseline, 0.80× of the limit), so no
widening was needed and the gate flips with the measured data (O26). The step's `continue-on-error`
is gone; the step and the race run both fail the job.

Rows and mutants:

| row | command (cwd) | exit | output |
| --- | --- | ---: | --- |
| suite | `python scripts/ci/test-go-test-durations.py` | 0 | `Ran 16 tests … OK` (includes the ten measured runs replayed green through the gate) |
| real-data replay (in-suite) | the five over-cap rows per recorded run + 200 fast rows | 0 | `over_30s=N adopted=5 … PASS` for each of the ten runs |
| planted 40 s test | synthetic stream with one unrostered 40 s row | 1 | `FAIL: … TestPlanted took 40.00s, over the 30s cap` |
| mutant M3 (cap) | `MAX_TEST_SECONDS = 30.0` → `1.0` | 1 | the suite's cap row reddens (`took 40.00s, over the 1s cap`) |
| mutant M4 (p95) | `P95_LIMIT_FACTOR = 1.2` → `1.01` | 1 | the suite's p95 row reddens (`limit 0.263s`) |
| mutant M5 (partial run) | revert the unfinished-test refusal | 1 | the suite's cut-off-stream row reddens (the stream no longer refuses) |

The partial-run fix (the closed STD-A09 entry): a test whose `run` event has no terminal
`pass`/`fail`/`skip` event makes the stream a refusal (exit 2) with the unfinished tests named, which
covers the package that carries a named failed test *and* was cut off.

## 4. The coverage flips

### 4.1 Go (`coverage-daemon`)

Four green `main` runs' `daemon-coverage` artifacts, measured with `go tool cover -func` (the same
command the job now runs):

| run | hit / total | total |
| --- | --- | ---: |
| 45120528766 | 32790 / 58907 | 55.6640 % |
| 45154249296 | 32961 / 59003 | 55.8633 % |
| 45186872699 | 32988 / 59032 | 55.8816 % |
| 45200375602 | 33027 / 59077 | 55.9050 % |

Floor = `min − 1.00` point = **54.6640 %**, committed in `scripts/ci/go-coverage-baseline.json`
together with the runs, their hit/total pairs and the method; the job's new
`Enforce the committed Go coverage floor` step pipes the profile through `go tool cover -func` and
the gate refuses a missing, empty, duplicated-total or unparsable summary, a baseline whose floor is
not `baseline − 1.00` or whose baseline is not the runs' minimum, and any run whose recorded percent
does not match its hit/total pair.

| row | command | exit | output |
| --- | --- | ---: | --- |
| suite | `python scripts/ci/test-assert-go-coverage-floor.py` | 0 | `Ran 17 tests … OK` |
| real capture | `python scripts/ci/assert-go-coverage-floor.py --summary scripts/ci/go-coverage-capture-fixture.txt` | 0 | `measured 86.20% >= floor 54.66% (baseline 55.66%, the minimum of 4 recorded runs)` |
| planted coverage drop (M6) | a summary one point below the floor (`54.65%`) | 1 | `the Go coverage total 54.66% is below the committed floor 54.66%`-shaped refusal naming both |
| floor tamper | floor raised without the baseline | 2 | `the floor must be baseline_percent - 1.00` |

### 4.2 vitest (`coverage-frontend`)

The same four runs' `frontend-coverage` artifacts (`lcov.info` totals):

| run | lines | functions | branches |
| --- | ---: | ---: | ---: |
| 45120528766 | 3004/3177 = 94.5546 % | 1130/1240 = 91.1290 % | 3498/4157 = 84.1472 % |
| 45154249296 | 3003/3176 = 94.5529 % | 91.1290 % | 3491/4150 = 84.1205 % |
| 45186872699 | 94.5529 % | 91.1290 % | 84.1205 % |
| 45200375602 | 94.5529 % | 91.1290 % | 84.1205 % |

Thresholds (min − 1.00 point, floored to two decimals): lines **93.55**, functions **90.12**,
branches **83.12**; `statements` stays unset because the four baseline runs were measured from
`lcov.info` totals, which carry lines/functions/branches only.
The job drops `continue-on-error` and `frontend/vitest.config.ts` carries the thresholds; the
classifier suite pins them at or above these numbers.

## 5. W6-11's jscpd step

Inserted verbatim from `docs/evidence/rf-w6-11-styler-jscpd-2026-11-23.md` §9 into the `contracts`
job after `./.github/actions/contracts-build`, with the job's checkout gaining `fetch-depth: 0` and
`persist-credentials: false`. W6-11's open wiring question resolves without a classifier edit:
`run_heavy` classifies every non-documentation path, so a change to the gate's own program already
runs the job the step lives in (pinned by the classifier suite's `daemon-code-modified` and
tool-fixture rows). The step runs the gate's own suite then the gate.

The gate was also run over this branch's own emulated range (a `git clone --shared` of the worktree
with the 22 changed/new paths committed on top of `f731e04e`, `--repo <clone> --base f731e04e
--head HEAD`): its first run **refused** the change for a 14-line / 275-token clone — the ten-run
duration table verbatim in both the plan and this record (`docs/evidence/…md [70-83] duplicates
docs/plans/…-plan.md [171-184]`) — which is the step's own planted red on real content; the table
was deduplicated (the plan points at this record's §3) and the range re-run green: `jscpd-changed: 22
changed file(s), 1708 scanned source(s), 1266 clone(s) in the corpus, 0 tolerated among the added
lines, none new` (exit 0).

## 6. Conversions

### 6.1 `daemon/internal/notification/client_test.go`

Base: `<-staleFramesSent`, `time.Sleep(20 ms)`, `attempts == 1`. HEAD: the server writes one
`work_available` frame on the same connection after the stale frames; the test loops past the join's
`connected` hint until the `work_available` hint (asserting its `RuntimeID`) under a single
`testHangGuard`, then keeps `attempts == 1`. The removed settle channel is subsumed: the witness
frame is written after the stale frames on the connection the hint must travel, so receiving it
proves the stale refs were ignored. Duration (`go test -count=1 -json -run
'^TestRunIgnoresStaleChannelCloseFrames$'`): base 0.04 s → HEAD 0.02 s; the package run is 4.81 s
(`go test -count=1 ./internal/notification/`, exit 0).

Mutant M1 (`client.go`'s close case drops the `JoinRef` check): the test reddens at
`client_test.go:296` after the 30 s guard (`client did not deliver a work_available hint after the
stale frames`), exit 1; restored byte-identical (blob hash equals the base's), green again.

### 6.2 `control/test/…/portal_goal_attention_test.exs`

Base: three goals created with `Process.sleep(1_100)` between them (2.2 s of real sleep) so the
cursor's `updated_at` order is deterministic. HEAD: each goal's `updated_at` is stamped explicitly
through `Repo.update_all` with ascending microsecond timestamps
(`~U[2026-01-01 00:00:01/02/03.000000Z]`). Control flow: both shapes read the pages only after all
three goals exist, and creation is synchronous through the API, so the sleeps had no physical side
beyond timestamp separation; the stamps make the same ordering property deterministic.

File run (`POSTGRES_DB=symmetry_control_test_w612 mix test test/symmetry_control_web/controllers/portal_goal_attention_test.exs`):
exit 0, `Result: 14 passed` in 5.7 s before / 3.2 s after (final post-format run 4.2 s; the residual
spread is the shared database, not the code). `mix format --check-formatted` exits 0 tree-wide.

Mutant M2 (`goals/read_model.ex:321` attention order `desc` → `asc`): the file exits 2 with
`Result: 13/14 passed`, failing `attention paginates the Goal feed by the server cursor and bounds
the limit` at `portal_goal_attention_test.exs:171` (`left: ["ef2c32ef…"] right: ["c845548a…"]`);
`read_model.ex` restored (`sha256 2bacfdac…` equal to the pre-mutation hash, `git diff` empty) and
the file green again.

### 6.3 frontend

Re-measurement: a full-suite run under host load (`mise x pnpm@12.17.0 -- … --filter
@symmetry/frontend exec vitest run`) failed 10 rows across six files — every failure was a wait
without an explicit budget expiring at testing-library's implicit 1000 ms default while the app's
server-driven state was still pending (`Unable to find role="heading"…` against an empty or
still-loading body), plus worker-start timeouts on two later files. The quiet-tree runs are green;
the load run is the flake evidence the re-derivation rests on.

Conversions (9 wait sites in the six flaked files; the 10 rows are covered because
`chat.test.tsx`'s `openChat` and `assignment.test.tsx`'s `openAssignmentEditor` are helper waits):

| file:line | wait | new argument |
| --- | --- | --- |
| `router.test.tsx:53` | `findByRole("heading", { level: 2, name: "Project Two" })` | `{ timeout: 5_000 }` |
| `router.test.tsx:399` | `findByRole("dialog", { name: "Work item" })` | `{ timeout: 5_000 }` |
| `selected-project.test.tsx:59` | `findByRole("link", { name: "Runtimes" })` | `{ timeout: 5_000 }` |
| `selected-project.test.tsx:83` | `findByRole("dialog", { name: subjectTitle })` | `{ timeout: 5_000 }` |
| `chat.test.tsx:199` (`openChat`) | `findByRole("heading", { level: 1, name: "Chat" })` | `{ timeout: 5_000 }` |
| `connections.test.tsx:176` | `findByRole("heading", { level: 1, name: "Connections" })` | `{ timeout: 5_000 }` |
| `connections.test.tsx:188` | `findByRole("button", { name: "Refresh" })` | `{ timeout: 5_000 }` |
| `goal-attention.test.tsx:243` | `findByRole("heading", { level: 1, name: "Deliver the attention entry point" })` | `{ timeout: 5_000 }` |
| `assignment.test.tsx:136` (`openAssignmentEditor`) | `findByRole("heading", { level: 1, name: "Subject work item" })` | `{ timeout: 5_000 }` |

Six-file run: exit 0, `Test Files 6 passed`, `Tests 194 passed (194)` in 31.9 s (42 + 6 + 47 + 59 +
30 + 10). The conversions keep every name and assertion byte-identical; the twelve rf-w5-04 budget
sites are untouched. Mutant: `goal-attention.test.tsx:243`'s budget at `{ timeout: 1 }` reddens
that row (`Tests 1 failed | 29 passed (30)`, exit 1, `Unable to find role="heading"…` at
`:243:25`); restored to `5_000`, the file is green.

The local coverage run over the full suite reports lines 94.55 % (3003/3176), functions 91.12 %
(1130/1240), branches 84.12 % (3491/4150), statements 91.65 % (3637/3968) — all three configured
thresholds clear with the gate green (§9).

## 7. Mutants (all restored; blob-hash verified)

Every mutant was applied to the real tree, the named check run red, the file restored and compared
byte-for-byte with its pre-mutation bytes, and the check green again. The CI job column names the
job that reddens on the PR; every job runs as the runner's non-root user, so no row depends on root.

| id | mutation (file) | check that reddens | CI job | observed |
| --- | --- | --- | --- | --- |
| M1 | `daemon/internal/notification/client.go`: close case drops the `JoinRef` check | the converted close-frames test | `daemon` (race), `daemon-windows` | red at `client_test.go:296` after the 30 s guard, exit 1 (§6.1) |
| M2 | `control/lib/symmetry_control/goals/read_model.ex`: attention order `desc` → `asc` | the paginated attention test | `control` | `13/14 passed`, exit 2, at `portal_goal_attention_test.exs:171` (§6.2) |
| M3 | `scripts/ci/go-test-durations.py`: `MAX_TEST_SECONDS` 30.0 → 1.0 | the duration suite's cap row *and* the real gate on the WSL `-race` planted-40 s stream | `legacy-route-audit` (suite), `daemon` (step) | suite red; real gate exit 1 (`over the 1s cap` + the p95 arm); restored byte-identical (§9) |
| M4 | `scripts/ci/go-test-durations.py`: `P95_LIMIT_FACTOR` 1.2 → 1.01 | the duration suite's p95 row | `legacy-route-audit` | red in-suite (limit 0.263 s) |
| M5 | `scripts/ci/go-test-durations.py`: revert the unfinished-test refusal | the duration suite's cut-off-stream row | `legacy-route-audit` | red in-suite (the stream no longer refuses) |
| M6 | a copy of the real capture with the total at 54.65 %; the floor raised without the baseline | the floor gate | `coverage-daemon` | exit 1 (`54.65% is below the committed floor 54.66%`); the floor tamper case exits 2 in-suite |
| M7 | `frontend/vitest.config.ts`: a threshold at 0, or the thresholds block dropped | the classifier suite's floor pin | `legacy-route-audit` | red in-suite |
| M8 | `.github/workflows/ci.yml`: `continue-on-error: true` restored on the durations step | the structure suite (and its advisory-set equality) | `legacy-route-audit` | 3 failures; flag removed, suite green (33 tests) |
| M9 | `frontend/src/features/goals/goal-attention.test.tsx:243`: budget `5_000` → `1` | the converted `goal-attention` row | `contracts` (frontend test step) | `Tests 1 failed | 29 passed (30)`, exit 1 (§6.3) |
| M10 | the branch's own first emulated range (a real table duplicated between two docs) | the changed-file jscpd gate | `contracts` | refused: `1 new clone(s) among the lines this range adds` (14 lines, 275 tokens); after deduplication the range is green (§5) |
| M11 | `scripts/ci/go-test-durations.py`: the adopted classification loses its `elapsed <= ceiling` guard (the pre-fix shape) | the duration suite's over-ceiling row (`test_a_rostered_test_over_its_ceiling_fails_with_the_ceiling_named`) | `legacy-route-audit` | red (`AssertionError: 0 != 1`; the row also prints the contradictory `within its committed ceiling` line with `adopted=1`); restored byte-identical (sha256 `932c984b…`) |

## 8. Re-measured census and dispositions

The full table lives in the plan §4. Summary by class at `f731e04e`: **73 Go `time.Sleep(` test
sites in 23 files** (`git grep -c 'time.Sleep(' f731e04e -- daemon` → 73; 72 sites in 22 files after
this slice's conversion), 11 ExUnit `Process.sleep(` sites in 4 files, the twelve vitest `{ timeout: }`
sites inside W5-04's files (untouched; ten more live outside this slice's set), and the re-measured
implicit-default rows. Converts: the notification settle (§6.1), the
attention pair (§6.2), the frontend rows (§6.3). Everything else is recorded as an
already-bounded-poll site, a subprocess-helper lifetime, a real-process/durability witness or a
not-owned row (W4-13's harness leftovers, the live-tagged smokes, `internal/app`'s helpers).

## 9. Gates

All foreground, each bounded with `timeout`; Windows and WSL suites never ran concurrently.

| gate | command (cwd) | exit | counts / output |
| --- | --- | ---: | --- |
| gofmt | `gofmt -l .` (`daemon/`) | 0 | no output |
| go vet | `go vet ./...` (`daemon/`) | 0 | no output |
| Windows build | `go build ./...` (`daemon/`) | 0 | |
| Linux build | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` (`daemon/`) | 0 | |
| touched packages (Windows) | `go test -count=1 -timeout 900s ./internal/notification/ ./internal/execution/ ./internal/workspace/ ./internal/platform/` (`daemon/`) | 0 | notification 3.09 s, execution 12.77 s, workspace 164.76 s, platform 15.04 s |
| loaded repeat | `GOMAXPROCS=8 go test -count=50 -run '^TestRunIgnoresStaleChannelCloseFrames$' ./internal/notification/` (`daemon/`) | 0 | 3.58 s, 50 executions |
| WSL linux-only | `wsl -d Ubuntu-24.04 -u accept -- go test -count=1 ./internal/platform/ ./internal/execution/ ./internal/workspace/` (mount) | 0 | platform 42.82 s, execution 2.09 s, workspace 48.91 s |
| e2e (Windows) | `go test -count=1 -timeout 1200s ./e2e/` (`daemon/`) | 0 | 0.70 s; the env-gated witnesses skip and the linux-tagged witness files are the dispatch-only subset |
| duration suite | `python scripts/ci/test-go-test-durations.py` | 0 | `Ran 16 tests … OK` |
| coverage floor suite | `python scripts/ci/test-assert-go-coverage-floor.py` | 0 | `Ran 17 tests … OK` |
| floor gate on the real capture | `python scripts/ci/assert-go-coverage-floor.py --summary scripts/ci/go-coverage-capture-fixture.txt` | 0 | `measured 86.20% >= floor 54.66% (baseline 55.66%, the minimum of 4 recorded runs)` |
| planted 40 s test (real stream) | a temporary `daemon/internal/notification/zz_planted_duration_test.go` run under WSL `-race -json`, through the gate | 1 | `TestPlantedFortySecondDuration took 40.01s, over the 30s cap` + the p95 arm; the file was removed and the package re-run green (2.09 s) |
| classifier suite | `python scripts/ci/test-ci-changed-path-classifier.py` | 0 | `Ran 37 tests … OK` |
| structure suite | `python scripts/ci/test-ci-structure-extraction.py` | 0 | `Ran 33 tests … OK` |
| folded-gate suite | `python scripts/ci/test-assert-folded-gates.py` | 0 | `Ran 6 tests … OK` |
| jscpd suite | `python scripts/ci/test-jscpd-changed.py` | 0 | `Ran 32 tests … OK` |
| actionlint (pinned 1.7.12) | `actionlint -color -shellcheck= -pyflakes= .github/workflows/ci.yml` | 0 | 0 findings |
| shellcheck (changed bodies) | `shellcheck --shell=bash --severity=warning` over the three changed step bodies | 0 | 0 findings |
| ruff (changed Python) | `uvx ruff@0.17.5 check` + `format --check` over the six changed/new scripts | 0 | `All checks passed!`, all files formatted |
| comment cap | `comment-cap.py <emulated range>` over the branch's added lines | 0 | no comment block over 2 lines |
| DLP | added lines and the new files scanned for profile paths and for a backslash before `u` | 0 hits | none |
| diff hygiene | `git diff --check` | 0 | clean |
| Elixir format | `mix format --check-formatted` (`control/`) | 0 | no output |
| Elixir compile | `MIX_ENV=test mix compile --warnings-as-errors` (`control/`) | 0 | no output |
| attention file | `POSTGRES_DB=symmetry_control_test_w612 mix test test/symmetry_control_web/controllers/portal_goal_attention_test.exs` (`control/`) | 0 | `Result: 14 passed` in 4.2 s (final) |
| full control suite | `POSTGRES_DB=symmetry_control_test_w612 mix test` (`control/`) | 0 | `Result: 1163 passed, 21 skipped`, `Finished in 364.7 seconds` (168.2 s async) on this shared host |
| frontend typecheck | `pnpm --filter @symmetry/frontend typecheck` | 0 | no output |
| frontend lint | `pnpm --filter @symmetry/frontend lint` | 0 | 219 warnings, 0 errors (the pinned cap is 229) |
| six converted files | `pnpm … exec vitest run <six paths>` | 0 | `Tests 194 passed (194)` in 31.9 s |
| full frontend suite + coverage | `pnpm … exec vitest run --coverage` | 0 | `Test Files 27 passed (27)`, `Tests 633 passed (633)`, 59.19 s; coverage lines 94.55 %, functions 91.12 %, branches 84.12 % — all three thresholds clear |
| per-file durations | `pnpm … exec vitest run --reporter=json --outputFile=…` | 0 | 27 files, 633 tests; slowest files: connections 26.8 s/59, chat 19.4 s/47, goal-attention 15.8 s/30, planning 12.4 s/35, goal-settle 12.3 s/8, router 8.7 s/42, then assignment 3.7 s and every other file under 2.8 s |

## 10. D2 report-only rows

- Test-LOC delta: `daemon/internal/notification/client_test.go` +19/−5,
  `control/test/…/portal_goal_attention_test.exs` +20/−5, the frontend rows (§6.3). The plan's
  −150 estimate is a plan figure, not a gate.
- Race-suite wall times (CI, main, sampled from the same ten runs): the race step spans 348 s
  (45186872699), 350 s (45154249296) and 450 s (45200375602) on the ubuntu runner. The slice's own
  delta against W4-13's −40…−90 s band is report-only: the conversions here remove one 20 ms settle
  and 2.2 s of ExUnit sleep, and the local package figures are in §9; the PR's CI race job is the
  measurement of record.
- `mix test` wall times (CI, main): `Finished in` 114.9 s (45200375602), 128.1 s (45186872699) and
  144.9 s (45154249296) — all above W3-14's "median ≤65 s / no seed >75 s" target at this head.
  Reported, not gated (D2); the slice's own change to the suite is the 2.2 s the attention pair
  stops sleeping (local `Result: 14 passed` 5.7 s → 3.2 s) and it does not explain the gap.

## 11. Residual risks and limits

- The adopted ceilings sit 1.2× above the ten-run maxima; the `internal/app` rows' observed swing
  across the ten runs is 1.34× (124.62 → 167.20 for the backpressure row), so a runner beyond 1.2×
  the recorded worst reddens the gate. The burn-down debt is in `docs/TODO.md`.
- The frontend host runs `@vitest/coverage-v8@5.1.0`/`msw@3.0.5` (mirror limits, restored lockfile);
  CI runs the locked 5.1.1/3.0.7.
- The Go floor's `go tool cover -func` reads the tree's sources in the same job that wrote the
  profile; a later source/tree mismatch inside one job is not modelled.
- The `internal/state` rows straddle the 30 s cap, so their ceiling (40–42 s) is the coarse
  ratchet; the fine signal for them is the p95 arm and the burn-down debt.

## 12. Findings (reported, not fixed here)

1. `mix test`'s CI wall time (114.9/128.1/144.9 s on the three sampled runs) is above W3-14's
   "median ≤65 s / no seed >75 s" target at this head; this slice's ExUnit change removes 2.2 s of
   sleep and does not explain the gap. Owner: the control-suite wall-time target (§10).
2. A local `vitest run` (no `SYMMETRY_TOOL_CACHE_DIR`) writes an untracked `frontend/.vitest/`
   cache directory that `.gitignore` does not cover (`.gitignore:79` covers
   `/frontend/coverage/` only). Found by this slice's runs; the directory was deleted from the
   working tree and no repo file was created for the finding. Owner: frontend tooling.
