# rf-w6-12 wall-clock burn-down part 2 + duration ratchets blocking — plan

Base: `f731e04e` (`origin/main` at dispatch; every dependency merged — W4-13 #267, W2-01 #188,
W2-17 #186, W5-04 #251, W3-14 #211, W3-15 #217, W6-09 #264, W6-11 #268, W6-10 #274; W5-07 dropped
2026-11-21). Contract: `goal-rf-w6-12-wall-clock-ratchets.md` (GDD state root, external to the
repository), with the binding conductor decisions (D14 split, D1 frozen names, the W6-11 jscpd
handoff, D2 report-only, the O26 p95 rule, the 2026-11-23 lessons 55-56 amendment and the
2026-11-24 dispatch note) and the 2026-11-22 comment policy over it.

Shape references: `docs/plans/2026-11-23-rf-w4-13-go-wall-clock-plan.md` (the part-1 conversion
techniques, the census/disposition table and the mutant protocol), the `rf-w6-10` plan/evidence pair
(the `ci.yml` flip with a committed baseline artefact, a fail-closed parser, its suite and the two
structural suites' pin updates), `docs/evidence/rf-w6-11-styler-jscpd-2026-11-23.md` §9 (the exact
jscpd step text this slice inserts).

## What lands

| file | change |
| --- | --- |
| `.github/workflows/ci.yml` | wave-6 last `ci.yml` owner: `coverage-daemon` and `coverage-frontend` lose `continue-on-error` and gain their floor steps/checks; the Go duration step loses `continue-on-error`; the `contracts` job's checkout gains `fetch-depth: 0` and W6-11's single jscpd step verbatim; `legacy-route-audit` gains the two new gate suites beside their siblings |
| `scripts/ci/go-test-durations.py` | the per-test cap gains the committed adoption roster with measured ceilings; the partial-run detection is fixed (the open `docs/TODO.md` entry) |
| `scripts/ci/test-go-test-durations.py` | new: the duration gate's suite (synthetic `go test -json` streams; the shipped constants pinned against the measured runs table) |
| `scripts/ci/assert-go-coverage-floor.py` | new: the fail-closed `go tool cover -func` summary parser and the Go floor verdict |
| `scripts/ci/go-coverage-baseline.json` | new: the committed Go coverage baseline (the four measured CI runs; floor = min − 1.00 point) |
| `scripts/ci/test-assert-go-coverage-floor.py` | new: the Go floor gate's suite (a real `go tool cover -func` capture, every refusal, the baseline arithmetic) |
| `frontend/vitest.config.ts` | the coverage thresholds at the measured floor (min of four CI runs − 1.00 point) |
| `scripts/ci/test-ci-changed-path-classifier.py` | the coverage-advisory split becomes the all-three blocking check with the Go floor step and the vitest thresholds pinned; mutations for each |
| `scripts/ci/test-ci-structure-extraction.py` | both advisory allowlists lose the flipped entries; the duration gate step gets its own pin plus mutations |
| `daemon/internal/notification/client_test.go` | conversion (in-process settle → predicate) |
| `control/test/symmetry_control_web/controllers/portal_goal_attention_test.exs` | conversion (the `Process.sleep(1_100)` pair → stamped `updated_at`) |
| `frontend/src/**/*.test.*` | the re-measured wait conversions (post-W4-11/W5-04/W5-08/W6-07) |
| `docs/plans/2026-11-24-rf-w6-12-wall-clock-ratchets-plan.md`, `docs/evidence/rf-w6-12-wall-clock-ratchets-2026-11-24.md` | this plan and the evidence record |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | close the duration partial-run entry; record the adopted over-cap rows' burn-down |

No production file changes: the only production edits are temporary mutants, each restored
byte-identical and verified by blob hash against `f731e04e`.

## Decisions the contract leaves open

### D1. The duration cap: adopt the measured stream as per-test ceilings (ratchet, not a raised cap)

The step's verdict is the summarizer's exit status; flipping the step blocking is only green if the
race stream passes. Re-measured at this head over ten green main runs (per-run table: evidence
record §3), the stream carries five structural rows over the 30 s cap: the two `internal/app` rows
(93.6–167.2 s) are over cap in every run and the three `internal/state` journal rows (30.2–34.6 s)
straddle it in four to eight of the ten runs. The two
app rows do real work proportional to the 5 MiB fixture (no timer, no sleeping goroutine; W4-13
recorded them "record only; file outside the owned set"), so they are not convertible inside this
slice's owned set, and no run of the stream can be green with the cap at 30 s
(`docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md` already named this: "the cap arm is
expected red until W6-10/W6-12 adopt or burn down the threshold").

Options considered:

- (a) Raise `MAX_TEST_SECONDS` to a measured constant (≈ 201 s). Green, but the cap then stops
  catching anything below a 3-minute hang, and the contract's planted-40 s red cannot fire.
- (b) Burn the five rows down. The app/state files are outside the owned set; the rows are
  throughput/fsync-bound, not sleep-bound.
- (c) **Adopted ceilings (chosen).** `MAX_TEST_SECONDS = 30.0` stays the rule for every test the
  committed roster does not name; the roster names exactly the measured over-cap rows with a
  per-test ceiling derived from the measured table: `ceiling = ceil(measured max × 1.2)` (the same
  +20 % factor the p95 band uses). A roster entry over its own ceiling fails; any unlisted test
  over 30 s fails. This is the only shape that is both green and keeps the 30 s cap meaningful for
  the whole tree (a new 40 s test reddens anywhere, including a scoped stream), and it is a
  *tightening* over a raised constant: every adopted row is ratcheted at 1.2× its measured worst.
  This is "adopt ... the threshold" read as the measured baseline data W2-01's plan anticipated.

The adopted roster (max over the ten recorded runs → ceiling):

| package | test | measured max (s) | ceiling (s) |
| --- | --- | ---: | ---: |
| `internal/app` | `TestOutputBackpressureRecoversWithOneTruncationMarker` | 167.20 | 201 |
| `internal/app` | `TestRawOutputBudgetDropsWithoutFailingSemanticEvents` | 124.96 | 150 |
| `internal/state` | `TestProviderActionCompletionRejectsOverLimitWorstCaseUnknownWithoutMutation` | 34.58 | 42 |
| `internal/state` | `TestProviderActionCapacityFailsBeforeDispatchWhenJournalByteBudgetIsExceeded` | 32.91 | 40 |
| `internal/state` | `TestProviderActionCapacityReservesWorstCaseFailureCodeBeforeDispatch` | 33.88 | 41 |

The roster is a ceiling ratchet, not a liveness list: an entry whose test no longer runs is inert
(a skipped test publishes no elapsed row, so the script cannot and does not refuse on absence). The
five rows' burn-down (proportional work or a smaller fixture at the same semantics) stays open in
`docs/TODO.md` for the owning slices. The p95 arm keeps its committed baseline (0.26 s) and its
1.2× band: the ten measured p95 values are 0.16–0.25 s (max ratio 0.96× of the baseline, limit
1.2×), so no band widening is needed and the measured variance is recorded (O26).

### D2. The ExUnit duration equivalent: nothing to flip, per the review amendment

W2-01 landed no ExUnit threshold gate: the ExUnit duration machinery is
`control/test/support/slowest_formatter.ex`, a formatter that prints the twenty slowest tests at
suite end (telemetry only, no verdict, no `continue-on-error` step). The binding w6 review
amendment is explicit: "W6-12b flips only what W2-01 landed advisory and records 'nothing to flip'
otherwise. No vitest/ExUnit duration gate is invented here." Recorded as such; the formatter's
table is reported.

### D3. Go coverage floor: artefact-derived baseline, `go tool cover -func` as the measuring tool

The CI `coverage-daemon` job uploads `daemon/coverage.out`; four green main runs' artifacts were
downloaded and measured (table in §5). Floor = `min(measured totals) − 1.00 point` = **54.66 %**
(exact: 55.6640 − 1.00 = 54.6640). The verdict compares the run's own `go tool cover -func` total
(the tool that owns the arithmetic) against the committed baseline; the parser refuses a missing,
empty, duplicated-total or unparsable summary, and refuses a baseline whose declared floor is not
exactly `baseline_percent − 1.00`. The job drops `continue-on-error`, so a below-floor total fails
the job.

### D4. vitest floor: the coverage thresholds at the same arithmetic

`coverage-frontend` drops `continue-on-error`; `frontend/vitest.config.ts` gains
`coverage.thresholds` for the three metrics the committed `lcov` baseline carries, at
`min(four measured runs) − 1.00 point`, floored to two decimals:
lines **93.55**, functions **90.12**, branches **83.12**. `statements` stays unset: the four
baseline runs were measured from `lcov.info` totals, which carry lines/functions/branches only.
The thresholds are pinned in the classifier suite at ≥ the committed floors.

### D5. W6-11's jscpd step: inserted verbatim, wiring question resolved without a classifier edit

The step text is W6-11's evidence §9, byte-for-byte, in the `contracts` job after the shared build
action (checkout gains `fetch-depth: 0` and `persist-credentials: false`). W6-11's one open wiring
question — whether the two new paths need a classifier family — resolves to "no edit needed":
`run_heavy` classifies every non-documentation path, so a change to the gate's own program already
runs the job the step lives in. Recorded; the classifier suite's `run_heavy` fixture pins that
behaviour.

### D6. The partial-run detection TODO entry is closed here

`docs/TODO.md`'s STD-A09 entry requires the flip to fix the partial-run detection first: a package
whose stream is cut off by a timeout but which also carries a named failed test currently counts as
"explained". The fix extends the unexplained computation: any test with a `run` event and no
terminal `pass`/`fail`/`skip` event makes its package's stream incomplete, and the verdict is the
same refusal (exit 2) with the unfinished tests named. The entry moves to
`docs/archive/CLOSED-TODO.md`.

### D7. Suite wiring

The two new suites run in `legacy-route-audit` beside `test-ci-changed-path-classifier.py` and
`test-assert-credo-strict-baseline.py` (W6-10's precedent for a gate's own suite); the jscpd suite
runs inside W6-11's step; the duration suite's fixtures are synthetic streams, so it costs seconds.

## 4. Re-measured census and dispositions at `f731e04e`

Go `time.Sleep(` in test files: **23 files / 73 sites at `f731e04e`** (`git grep -c 'time.Sleep(' f731e04e
-- daemon` → 73; 72 sites / 22 files after this slice's conversion; W4-13's part-1 subset already converted;
the production sites stay untouched by rule). ExUnit `Process.sleep(`: 11 sites / 4 files. vitest: the twelve
explicit `{ timeout: }` sites inside W5-04's files are untouched (ten more live in `goal-settle.test.tsx`
and `tool-cache-config.test.ts`, outside this slice's set); the remainder is re-measured below.

| # | site | cause / bound | disposition |
| --- | --- | --- | --- |
| G1 | `execution/probe_test.go:87` | process-existence poll, `testHangGuard` deadline | keep: already a bounded poll-with-predicate on a real process |
| G2 | `execution/probe_test.go:258` | filesystem marker written by a subprocess, `testHangGuard` | keep: `waitForProbeMarker` is the existing bounded-poll helper |
| G3 | `execution/runner_test.go:779` | helper subprocess lifetime (root exits while its child holds stdout) | keep: subprocess helper; a new fd-handshake protocol is a new seam, out of scope |
| G4 | `execution/runner_test.go:817` | `waitForever()` sentinel | keep: helper lifetime sentinel, no deadline |
| G5 | `execution/runner_unix_test.go:71` | process-existence poll, `testHangGuard` | keep: already bounded; linux-only, WSL gate |
| G6 | `workspace/subject_test.go:608`, `:1350` | scripted helper children held alive | keep: subprocess helper lifetimes |
| G7 | `workspace/subject_test.go:1051`/`:1059` | owned Git children settled after cancellation | keep: real-process absence double-check (quiescence window) |
| G8 | `workspace/subject_test.go:1366` | helper readiness marker, deadline | keep: existing `waitForWorkspaceBatchReady` helper |
| G9 | `notification/client_test.go:285` | in-process settle: no reconnect within a window after stale frames | **convert** (predicate: the current join delivers a hint) |
| G10 | `platform/containment_supervisor_linux_test.go:916`, `:1010` | supervisor stop retry during cleanup (100 × 10 ms attempts) | keep: retry-with-backoff on a live supervisor; attempt-count bound |
| G11 | `platform/containment_supervisor_linux_test.go:1224`, `:1522`, `:3500` | marker/durability/child-progress polls with deadlines | keep: already bounded polls |
| G12 | `platform/process_unix_linux_test.go:3074`, `:3126` | process-state and reap polls with deadlines | keep: already bounded polls |
| G13 | `platform/process_unix_linux_test.go:3474` | separates two process start-time identities by 100 ms | keep: real-clock identity separator (documented in place) |
| G14 | `platform/process_windows_test.go:1132` | owned-child observation poll, `containmentCloseDeadline` | keep: already bounded |
| G15 | `e2e/core_test.go` ×12 | inside `waitForPathsAbsent`/`waitForJournal*/waitForTask`/`waitForControl*` etc. | keep: every site is the body of an existing bounded-poll helper |
| G16 | e2e files ×45 (core 12, containment witness 11, v2 owner cli 10, negative 5, rootless 3, helper-exit 2, pre-authority 1, replay 1) | real process/socket/durability waits, mostly inside `waitFor*` helpers, in the dispatch-only acceptance witnesses | keep: each is a bounded poll helper body, a subprocess-helper lifetime or a documented durability settle; none is a deadline on in-process state |
| G17 | `harness/{staged_driver_test.go:456, claude/adapter_support_test.go:88,:128}` | W4-13's owned subset leftovers: bounded polls | not-owned-by-slice (W4-13 part 1); recorded |
| G18 | `harness/claude/adapter_live_test.go:554`, `harness/codex/adapter_live_cancel_test.go:92` | credential-gated live smokes (15 s / 20 s real sleeps) | not-owned-by-slice: live-tagged, census rows only |
| G19 | `app/{app_test.go:7339, cleanup_test.go:657, harness_test.go:4786}` | existing bounded poll helpers | not-owned-by-slice; recorded |
| X1 | `portal_goal_attention_test.exs:144`/`:146` | three goals need distinct `updated_at` for cursor order | **convert** (stamped `updated_at`) |
| X2 | `pi_control_e2e_test.exs` ×4, `provider_broker_e2e_test.exs` ×4, `portal_integrations_test.exs` ×1 | bounded poll loops with backoff | keep: already bounded polls (census rows) |
| F1 | frontend `waitFor(` sites | see §6.3 | **convert** the re-measured rows only |

## 5. Measured baselines

### 5.1 Duration stream (ten green `main` runs, `f731e04e` window, 2026-11-22/23)

p95: min 0.16, max 0.25, mean 0.218 over the ten runs; committed baseline 0.26; limit 0.312 (1.2×);
max observed ratio 0.96×. The per-run table with every over-cap value and the tests-per-run counts
lives in the evidence record §3. The committed baseline's provenance (run 44892215975, the first
run whose daemon job wrote the stream through the gate) is unchanged.

### 5.2 Go coverage (four green `main` runs, `daemon-coverage` artifacts)

| run | statements hit/total | total |
| --- | --- | ---: |
| 45120528766 | 32790 / 58907 | 55.6640 % |
| 45154249296 | 32961 / 59003 | 55.8633 % |
| 45186872699 | 32988 / 59032 | 55.8816 % |
| 45200375602 | 33027 / 59077 | 55.9050 % |

Floor = min − 1.00 point = **54.6640 %**.

### 5.3 vitest coverage (the same four runs, `lcov.info` totals)

| run | lines | functions | branches |
| --- | ---: | ---: | ---: |
| 45120528766 | 3004/3177 = 94.5546 % | 1130/1240 = 91.1290 % | 3498/4157 = 84.1472 % |
| 45154249296 | 3003/3176 = 94.5529 % | 91.1290 % | 3491/4150 = 84.1205 % |
| 45186872699 | 94.5529 % | 91.1290 % | 84.1205 % |
| 45200375602 | 94.5529 % | 91.1290 % | 84.1205 % |

Floors = min − 1.00 point: lines **93.55**, functions **90.12**, branches **83.12**.

## 6. Conversions and keeps

### 6.1 `notification/client_test.go` (G9)

Base: create the client, `<-staleFramesSent`, sleep 20 ms, assert `attempts == 1` (a sample of the
absence of a reconnect). HEAD: the server writes one `work_available` frame on the same connection
after the stale frames; the test awaits the resulting `Hint` (looping past the join's `connected`
hint) under `testHangGuard`, then asserts `attempts == 1`. Control flow (lesson 32): the base's
single wait is "20 ms elapsed"; the HEAD's is "the current join delivered a hint on the same
connection" — the connection that carries the hint is the one the stale frames must not have
closed, so the wait's physical side (the websocket) is preserved and the assertion is stronger: a
client that closed on the stale refs cannot deliver the hint. No early return sat between the
sleep and the assertion; none sits between the predicate and it now.

### 6.2 `portal_goal_attention_test.exs` (X1)

Base: create goal → sleep 1.1 s → create goal → sleep 1.1 s → create goal (two real seconds so the
`updated_at` values order the cursor pages). HEAD: create goal → stamp its `updated_at` explicitly
through `Repo.update_all` → … with ascending microsecond timestamps. Control flow: both shapes read
the pages only after all three goals exist; the sleeps had no physical side beyond timestamp
separation (creation is synchronous through the API), and the stamped values make the same ordering
property deterministic instead of second-boundary luck.

### 6.3 frontend waits

Re-measured: a load run failed 10 rows across six files, every failure a wait without an explicit
budget expiring at testing-library's implicit 1000 ms default while the app's server-driven state
was pending. Converted: nine wait sites in those six files (`router.test.tsx:53`, `:399`,
`selected-project.test.tsx:59`, `:83`, `chat.test.tsx:199` (`openChat`), `connections.test.tsx:176`,
`:188`, `goal-attention.test.tsx:243`, `assignment.test.tsx:136` (`openAssignmentEditor`)), each
given the files' own `{ timeout: 5_000 }` convention. The twelve rf-w5-04 budget sites are
byte-identical; the six-file run is 194 passed, and the `{ timeout: 1 }` mutant reddens the
`goal-attention` row. Full table and numbers: the evidence record §6.3.

## 7. Mutants (red, then restored)

Every mutant is applied with a temporary diff, the named test run red, the file restored
byte-identical (blob hash compared against `f731e04e`), and the test green again.

| id | mutation | rows expected red | CI job that reddens |
| --- | --- | --- | --- |
| M1 | `notification/client.go`: drop the `JoinRef` check in the close case | the converted close-frames test (guard timeout) | daemon (race, non-root); daemon-windows |
| M2 | `goals/read_model.ex`: flip the attention order `desc:` → `asc:` | the paginated attention test | control |
| M3 | `go-test-durations.py`: `MAX_TEST_SECONDS` → 1.0 (or plant a 40 s test) | the duration gate (cap) | daemon (race) |
| M4 | `go-test-durations.py`: `P95_LIMIT_FACTOR` → 1.01 | the duration gate (p95 arm) | daemon (race) |
| M5 | `go-test-durations.py`: revert the partial-run fix | the duration suite's truncated-stream fixture | legacy-route-audit |
| M6 | `go-coverage-baseline.json`: floor + 1.00 point | the Go floor step | coverage-daemon |
| M7 | `vitest.config.ts`: raise one threshold above the floor | the vitest coverage run | coverage-frontend |
| M8 | `ci.yml`: re-add `continue-on-error` to a flipped step/job | classifier + structure suites | legacy-route-audit |

Planted reds (temporary, not committed): a 40 s test's stream through the flipped gate (exit 1,
cap named); a coverage drop below the Go floor (job red) and below a vitest threshold.

## 8. Evidence plan (foreground, serialized)

1. Go: `gofmt -l`, `go vet ./...`, both GOOS builds, `go test -count=1` for the touched packages
   (Windows), the WSL run for the linux-only rows, the duration suite, `git diff --check`.
2. Control: `mix format --check-formatted`, `MIX_ENV=test mix compile --warnings-as-errors`, the
   touched file, the full `mix test` (wall time against W3-14's ≤65 s median target).
3. Frontend: typecheck, lint (pinned cap 229, not raised), touched suites, the full suite with
   per-file durations, then `--coverage` against the new thresholds.
4. Scripts: the classifier suite, the structure suite, the jscpd suite, the two new gate suites,
   the changed-path classifier over this slice's own diff shape where cheap.
5. The ten-run and four-run tables above; the race-job and mix wall times from the same runs; the
   test-name byte-identity check (the daemon job's 7 `--assert` names and `-run` patterns, the five
   pi_control ExUnit names).
6. D2 report-only: test-LOC delta; the CI race/mix deltas are the conductor's PR measurements.

## 9. Risks and out of scope

- The adopted ceilings are green by construction at the measured maxima; a loaded runner beyond
  1.2× the worst measured row reddens the gate. The observed app-row swing (1.34× within the ten
  runs) is below the 1.2× headroom *over the max* but not far below it; the evidence records the
  swing, and the burn-down TODO is the durable fix.
- `go tool cover -func` reads the tree's sources; the CI step runs it in the same job that
  produced the profile, so the pairing is the job's own.
- Out of scope: production sleeps; W5-04's twelve vitest sites and its `testTimeout`; W5-07's
  monitor state machine; the opencode/native acceptance budgets; W6-11's gate implementation;
  the coverage-artifact generation (W2-17); new production seams; duration thresholds on the
  dispatch-only acceptance workflows.
