# rf-w2-01-ci-test-observability plan — durations + browser-suite inventory

Contract: `goal-rf-w2-01-ci-test-observability.md` (GDD, 2026-11-21). Baseline for every coordinate in
this plan: this worktree's HEAD `6a815c0fc72393f9ccc1cdde5a516a2f8176eb9c` (`6a815c0f`,
`origin/main` at plan time), branch `refactor/rf-w2-01-ci-test-observability`. Binding inputs:
program plan §2 M1/M9/M10, §5 W2-01, §10.5 and decision D7; findings `tests-cross-cutting.md` T14
(per-test duration telemetry missing for two of three suites) and T4 (≈4,600 lines of browser/node
tests never run in CI); findings `scripts-ci.md` F4 (six of thirteen browser specs never run in CI)
and S6b (browser suite inventory test + manual-only label); `docs/design/quality.md` (gate policy);
and the contract's owned-file set (four files plus this plan, extended in review round 1 by the
Conductor decision with `control/test/support/slowest_formatter.ex`; every other path is read-only
here).

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `.github/workflows/ci.yml` | modified | control job's `mix test` step registers the owned test-support formatter (top-20 slowest at suite end; `--slowest 20` was reverted in fix round 1: trace mode, see "The duration plane"); race step emits `go test -json` through the shared gate and writes `$RUNNER_TEMP/daemon-go-race.json` (daemon job); one new advisory duration-summary step (daemon job); one new blocking inventory step (legacy-route-audit job) |
| `control/test/support/slowest_formatter.ex` | new | test-support ExUnit formatter: records every finished test's time and prints the twenty slowest at suite end, so the control job gets duration telemetry without trace mode |
| `scripts/ci/go-test-durations.py` | new | `go test -json` duration summarizer: prints the top-N test table and a package table, fails a test >30 s or p95 +20 % over the committed baseline |
| `scripts/ci/assert-browser-suite-coverage.py` | new | inventory gate: every `browser/tests/**/*.spec.mjs` / `browser/scripts/**/*.test.mjs` is named by a workflow or listed in `browser/README.md`'s manual matrix |
| `browser/README.md` | modified | new "Manual-only suite matrix" section (machine-parsed by the gate) listing the nine suites CI does not run, why, and how to run each today |
| `docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md` | new | this plan |

No new workflow job: the classifier fixture suite
`scripts/ci/test-ci-changed-path-classifier.py` pins the job model (every job must gate on an
existing classifier output, and the `changes` job must declare exactly the three outputs), and it is
explicitly not editable here (W1-01/W3-01 own it). Both new steps therefore grow inside jobs that
already exist, exactly as W1-01 grew the advisory quality plane.

## The duration plane

### ExUnit (control job)

**Implemented in review round 1 (Conductor decision 2026-11-21, escalation E1 option A);
`--slowest` stays forbidden.** The step was first changed to `- run: mix test --slowest 20`, and
the flag does print the slowest tests, but not as an output-only addition: `--slowest N` sets
ExUnit's `:slowest`, and `ExUnit.configure/1` turns that into `trace: true`
(`lib/ex_unit/lib/ex_unit.ex`, `put_slowest/1`), which drops `:max_cases` to `1` (`max_cases/1`)
and makes every test timeout `:infinity` (`ExUnit.Runner.get_timeout/2`). The runtime probe on the
pinned toolchain shows the consequence directly: an `async: true` test tagged `@tag timeout: 200`
that sleeps 600 ms fails with `** (ExUnit.TimeoutError)` under `mix test` (`max_cases: 32`, exit 2)
and passes under `mix test --slowest 1` (`max_cases: 1`, exit 0); the PR's CI log confirms trace
mode (2,024 `  * test ` lines versus 0 on the main reference run). A pre-existing gate changing
semantics is the contract's clause 3, so `--slowest` is out.

The ratified replacement is the owned test-support formatter
`control/test/support/slowest_formatter.ex` (`SymmetryControl.SlowestFormatter`), and the step now
runs:

```yaml
- run: mix test --formatter ExUnit.CLIFormatter --formatter SymmetryControl.SlowestFormatter
```

The formatter is a GenServer that records every finished test's `time` and prints the twenty
slowest at `{:suite_finished, _}`, in the same row shape ExUnit's own slowest list uses
(`* test <name> (<Module>) (<ms>ms) [file:line]`). It reads the same events `--slowest` would, so
`max_cases` and the per-test timeouts are untouched; `ExUnit.CLIFormatter` stays listed, so the
run's normal output and exit status are unchanged. No other `mix test` invocation (provider broker
e2e, the goal-receipt JSON-formatter run, the folded Pi E2E steps) is touched, so none of their
formatter contracts move.

### Go (daemon job): capture

The finding's complaint is that the full `go test -race ./...` pass — 449.5 s of the job on a
recent run, `internal/app` alone 385.7 s — reaches the log with no per-test durations, and that the
daemon job varies 7.5–11.3 min with no attribution. The stream that carries the attribution is the
race pass, so the race step starts emitting `-json`, captured through the repository's single
implementation of that plumbing (`scripts/ci/go-test-json-gate.py`), which keeps the stdio-only
caller boundary before Go and preserves the suite's exit status:

```yaml
- name: Run race tests with the same stdio-only caller boundary
  run: |
    python3 ../scripts/ci/go-test-json-gate.py \
      --events "$RUNNER_TEMP/daemon-go-race.json" \
      --stdio-boundary \
      --assert github.com/wxxb789/symmetry/daemon/internal/harness/opencode \
        TestOpenCodeV2VerifierRejectsReleasedPeer \
      -- "$(command -v go)" test -race -count=1 -timeout 600s -json ./...
```

Why not the alternatives:

* **Inline `| tee` + `PIPESTATUS`.** The pipeline shape the gate was written to eliminate ("one
  implementation of the `tee` the `-json` event stream, preserve the test exit status plumbing that
  six workflow steps used to spell out inline"); it would reintroduce a seventh spelling.
* **Feeding the existing scoped ordinary pass (`daemon-go-test.json`) instead.** Zero invocation
  change, but that pass is ~4 s of wall time over 529 sub-millisecond outcomes (measured below), so
  its p95 is 0.00 s and a p95 ratchet over it would be degenerate. It also leaves the finding's
  actual subject — the race pass — opaque.
* **Redirecting `-json` into a file and hiding the stream from the log.** Dead-element diagnostics
  would move out of the log and a 5–10 minute step would print nothing until it ends.

The gate requires at least one `--assert` group (its contract: every captured stream names a
witness). The asserted name is `TestOpenCodeV2VerifierRejectsReleasedPeer` — a plain test in
`internal/harness/opencode/peer_linux_test.go` (no subtests, no env gate, `//go:build linux`) that
the *same job* already requires to run and pass in its ordinary pass, so the job's requirements are
unchanged; the race configuration now enforces one of the names the ordinary pass pins. The race
step's own verdict stays "the race suite ran and exited with its own status". The log gains the JSON
event stream (the scoped ordinary step has flowed the same way for its whole life) and loses the
per-package `ok …s` lines; the summary step below is where a human reads the durations now.

### Go: summarizer (`scripts/ci/go-test-durations.py`)

One pure consumer: `EVENTS.json` (the `go test -json` stream), no command execution, no state.

* **Distribution.** Every terminal per-test event (`Action` ∈ {`pass`, `fail`} with a `Test` field),
  keyed by `(Package, Test)`; a duplicate key keeps its largest observed `Elapsed` so a rerun inside
  one stream cannot skew the sample. `skip` events are excluded from the distribution — a skipped
  test performed no work, and their share differs by host — but their count is printed so the
  exclusion is visible. Package-level summary events are excluded from the test table and used for
  the package table.
* **Table.** Top 20 test durations, descending (ties by package then name), printed as
  `seconds  package  Test`; then the ten longest package totals; then a summary line
  `go_test_durations: tests=N skipped=M packages=K p95=…s baseline=…s limit=…s over_30s=O`.
  A green run prints the table and exits 0 — that is the contract's observable pass condition.
* **p95.** Nearest-rank percentile over the ascending sample: index `ceil(0.95·N)−1`. Documented,
  deterministic, no interpolation.
* **Thresholds.** A test is too slow when `Elapsed > 30.0` (strictly greater; the contract's "a test
  >30 s"). The p95 arm fails when `p95 > P95_BASELINE_SECONDS × 1.2` (the contract's "+20 %"). Both
  arms report every crossing, not just the first.
* **Committed baseline.** `P95_BASELINE_SECONDS` is a module constant next to the provenance
  comment naming the exact stream it was measured from (see "Measured baselines"): the runner's own
  race stream of CI run 44892215975, the first run whose daemon job wrote
  `$RUNNER_TEMP/daemon-go-race.json` through the gate. Fix round 1 re-baselined it to that stream's
  `p95 = 0.260 s` (the earlier host-measured non-race proxy, p95 4.910 s, sat ~19x above the
  runner's p95, so the +20 % band could never fire). The step prints `p95=`, `baseline=` and
  `ratio=` side by side; W6-12 flips the thresholds to blocking, and any re-derivation of the band
  from more than one runner run is its call.
* **Exit codes.** `0` both arms pass; `1` a threshold crossed (the verdict, in the step's own exit
  status); `2` usage/missing-file/malformed-stream/no-outcomes (never silently green; a refused
  stream that carries per-test outcomes still prints the tables, labelled partial).

### Go: CI step and advisory framing

```yaml
- name: Summarize Go test durations
  if: always() && steps.daemon_race_tests.outcome != 'skipped'
  continue-on-error: true
  run: |
    python3 ../scripts/ci/go-test-durations.py "$RUNNER_TEMP/daemon-go-race.json"
```

`if: always()` makes the table print even when the race suite went red — the run that needs the
attribution most — and the `steps.daemon_race_tests.outcome != 'skipped'` guard (fix round 1, finding
P3-2) keeps it from erroring over the missing event file when an earlier step's failure skipped the
race run. `continue-on-error: true` follows the contract's evidence pointer (§10.5: "floors
stay advisory until W6-10/W6-12") and W6-12's own row ("flip duration thresholds/ratchets to
blocking"): the summarizer's exit status is the verdict and reddens the *step*, while the job keeps
its current conclusion semantics, the same advisory-plane contract W1-01 shipped for golangci-lint,
Credo, govulncheck, shellcheck and ruff. A planted 40 s test therefore fails the step with exit 1
(fixture evidence below); W6-12 flips the step to blocking, with the runner-seeded baseline already
in place.

## The browser-suite inventory gate

### Semantics (`scripts/ci/assert-browser-suite-coverage.py`)

Discoveries are the filesystem's: `<root>/browser/tests/**/*.spec.mjs` and
`<root>/browser/scripts/**/*.test.mjs` (16 files today: 13 specs, 3 node tests). Every discovered file
must be classified exactly once:

* **wired** — some `.github/workflows/*.yml`/`*.yaml` contains a path token whose suffix equals the
  file's browser-relative path (`tests/foo.spec.mjs`) or repository-relative path
  (`browser/tests/foo.spec.mjs`), at a `/` boundary, **outside its YAML comments** (fix round 1,
  finding P2-1: a comment is not a step and runs nothing). The render step's argument list
  (`tests/workspace-shell.spec.mjs …`) matches this way, as would a future compose-family step.
  What proves *execution* for the wired render family is `scripts/ci/assert-playwright-json.py`'s
  executed-set assertion, which this gate deliberately does not duplicate.
* **manual** — the file is named by a backticked path token in a markdown table row of the
  `## Manual-only suite matrix` section of `browser/README.md`. Only table rows in that one section
  count; the rest of the README's prose mentions spec files freely and is not consulted.

Verdicts (exit 1, every offending path named): an unclassified file; a file classified both wired
and manual (the matrix must reflect reality, so W6-02's wiring flips must also drop the row); a
matrix entry with no file on disk (matrix rot). The classification table (file, verdict, source) is
always printed; `--root` overrides the repository root for synthetic-fixture runs (the gate's
fixtures below run against a scratch tree, never the real one). Exit 2 for usage errors.

### Placement and gate strength

The step is **blocking** (no `continue-on-error`, no `if:`) and lives in `legacy-route-audit`,
the workflow's only unconditional job: the check is deterministic, reads only the tree, costs
milliseconds, and must also run for a documentation-only pull request — `browser/README.md` *is* a
Markdown file, so a matrix-only edit classifies as documentation-only and would skip every
`run-heavy` job. Adding a fourth classifier output is not possible (the classifier fixture suite
pins exactly the three existing outputs), and this is the one placement that closes the gap without
touching routing. Failure control from the contract's clause 3 is therefore direct: a pull request
that adds a spec file neither named by a workflow nor listed in the matrix reddens CI unconditionally.

## Failure-control mapping (contract clause 3)

| Failure mode named by the contract | State after this increment | Evidence |
| --- | --- | --- |
| A pre-existing job changing semantics/routing | No job's `needs:`/`if:` changes; every job still gates on the same classifier output. `test-ci-changed-path-classifier.py` is run as a gate, never edited. | classifier suite green (gate log below) |
| A new job missing `needs: changes` + `run-heavy` | No new job is added; both steps grow inside existing jobs. | `git diff` of ci.yml is steps-only |
| A gate that passes with an unlisted spec | The inventory gate fails on an unlisted spec (planted fixture, exit 1) and fails on matrix rot and double classification. | fixture runs below |

Pre-existing step semantics: the control job's `mix test` step gains two `--formatter` flags and one
test-support module (see "The duration plane / ExUnit") - `ExUnit.CLIFormatter` stays the first
formatter, the added one only prints a table at suite end, and the step's pass/fail meaning is
unchanged; the race step keeps its command, its stdio-only boundary, its 600 s budget and its
exit-status meaning, gains `-json` and the one already-required named assertion, and now writes the
event stream next to it; the new summary step is `continue-on-error`, so the daemon job's
conclusion cannot change because of it.

## Out of scope

* **Wiring browser specs into CI** (W6-02, decision D7): the gate classifies them; the fixture job /
  compose wiring / driver ports are W6-02's, per D7 and the contract's clause 4.
* **Coverage plane** (W2-17), **classifier extraction** (W3-01), **`ci.yml` program extraction**
  (W6-01): serialized owners of `ci.yml`/`test-ci-changed-path-classifier.py`.
* **Blocking duration thresholds** (W6-12) and the slow-test burn-down itself: this slice records
  the numbers and the runner-seeded baseline; W6-12 flips the thresholds to blocking.
* **Fixing the slow tests** the table will name, and any other change to `daemon/**`, `control/**`
  or `browser/tests/**`: read-only here (the Conductor decision's test-support formatter is the one
  control-side exception).
* **A committed fixture suite for the two new scripts** (`scripts/ci/test-*.py`): not in the owned
  set. The contract's gate is "both new scripts exercised on synthetic fixtures" — done in this
  plan's evidence runs, with `--root` and `EVENTS.json` as the seams; W2-17/W3-01 own the scripts'
  fixture files if they later want them in CI's helper-suite list.

## Decisions and their justification

1. **The race pass is the summarized stream** (not the 4 s scoped pass): it is the finding's
   subject, the job's critical path, and the only stream whose p95 is a real distribution (measured
   scoped pass: 529 outcomes, total 2.0 s of test time, p95 0.00 s).
2. **The shared gate, not inline tee**: one plumbing implementation; the mandatory named assertion
   is satisfied with a name the same job already requires, so no requirement is invented.
3. **`skip` events excluded from the distribution but counted**: host-dependent gating (Linux-only
   witnesses on the Windows half) must not move the percentile; the count keeps the exclusion
   auditable.
4. **Nearest-rank p95**: no interpolation, no float-order surprises; the definition is written in
   the tool's docstring and printed numbers can be re-derived by hand.
5. **Baseline committed in the script, not in `ci.yml`**: the provenance comment needs a home next
   to the comparison constant; a YAML literal would separate the number from its measurement record.
6. **Advisory summary step, blocking inventory step**: contract §10.5/W6-12 (advisory duration
   floors) versus the inventory gate's clause-3 text ("a gate that passes with an unlisted spec");
   the two planes have different failure semantics on purpose.
7. **No new classifier output**: the classifier fixture suite pins the `changes` job's outputs to
   exactly three; the README-matrix-only edit is closed by placing the inventory step in the
   unconditional job instead.
8. **Inventory matrix parsed from one marked section, table rows only**: prose mentions of spec
   files elsewhere in `browser/README.md` (there are many, and they are load-bearing history) must
   not satisfy the gate vacuously.
9. **`--root` on the inventory gate**: the contract asks for synthetic-fixture exercise; a root
   override is the smallest seam that lets the fixture run without planting files in the tree (the
   planted-file exercise is still done for the acceptance case and then removed).
10. **Expected advisory reds are recorded, not hidden** (W1-01 precedent): the runner's race stream
    already carries five tests over the 30 s cap (below), so the cap arm is expected red until
    W6-10/W6-12 adopt or burn down the threshold; the step prints the numbers and stays advisory
    until W6-12.

## Measured baselines

Recorded on this delivery host; every number below was produced by the command named with it, at
this revision unless stated.

**ExUnit `--slowest` (Elixir 1.20.4 / OTP 29, probe on a throwaway project; the fix-round-1
evidence)**: the flag exists on the pinned version and prints the slowest-test table (the original
shape, `Top 20 slowest (0.1s), 84.5% of total time:` …), but on an `async: true` test tagged
`@tag timeout: 200` that sleeps 600 ms it also changes the verdict: `mix test` prints
`max_cases: 32`, raises `** (ExUnit.TimeoutError)` and exits 2, while `mix test --slowest 1` prints
`max_cases: 1`, ignores the tag timeout and exits 0. That is why `--slowest` is not the duration
reader (see "The duration plane / ExUnit"); the probe exercises the flag's semantics, not the
control suite, which needs Postgres.

**ExUnit `SymmetryControl.SlowestFormatter` (Elixir 1.20.4 / OTP 29, control suite, review round
1)**: `mix test --formatter ExUnit.CLIFormatter --formatter SymmetryControl.SlowestFormatter
test/symmetry_control/request_hash_test.exs` (test database partition `fix188r1`, host Postgres)
printed `Running ExUnit with seed: 219747, max_cases: 32` (no trace), the table `Top 6 slowest
tests:` with rows in the CLI formatter's slowest shape (`* test <name> (<Module>) (<ms>ms)
[file:line]`), and `Result: 6 passed`, exit 0. Timeout enforcement stays ExUnit's own: it is
skipped only under `config.trace`, which this run does not set, and the round-1 prototype records
the `@tag timeout: 200` failure under `max_cases: 32` against the pass under `--slowest 1`.

**Scoped plain `go test -json` stream (the existing `$RUNNER_TEMP/daemon-go-test.json` step, from CI
run 44874926108's daemon job log)**: 529 terminal test outcomes, p95 0.00 s, max 1.51 s
(`TestLinuxLegacyOpenCodeRefusesBeforeHealthRequest`), 2.0 s of summed test time for a ~4 s step —
the measurement that rules this stream out as the summarized one.

**Committed baseline (race stream, CI run 44892215975)**: the daemon job's race step wrote
`$RUNNER_TEMP/daemon-go-race.json` through the gate for the first time, and the summarizer printed
`tests=5062 skipped=105 packages=25 p95=0.260s over_30s=5`. Fix round 1 committed that
`p95 = 0.260 s` as `P95_BASELINE_SECONDS` — the same stream shape the CI step feeds. It replaces
the initial host-measured non-race proxy (p95 4.910 s; method recorded in the paragraphs below),
which landed ~19x above the runner's p95, so a +20 % band anchored there could never fire. The same
run's `over_30s=5` is the expected advisory red recorded under "Residual risks".

**Why not a race-shaped stream from this host (three discarded attempts, recorded as method).**
Two attempts ran in the `symmetry-race` (Alpine) WSL distro — the first copied only `daemon/` (the
suite reads `../contracts/fixtures`) at 16-way parallelism, the second fixed both — and both
produced the same timeout-shaped failures: 105 then 55 failing outcomes and six 600 s package
timeouts. A direct single-test re-run isolated the cause: `TestClientHealthUsesExactEndpointAndBasicAuth`
fails in that distro with and without `-race` (`Get "http://127.0.0.1:44775/api/health": context
deadline exceeded`), and the whole opencode package times out at 300 s — the distro's loopback HTTP
to a test's own `httptest` server never completes (`/etc/hosts` and `getent hosts localhost` are
normal), an environment defect, not a code failure. A third attempt in the Ubuntu-24.04 distro
(glibc, the runner's base, `GOMAXPROCS=4`, full tree) reproduced the same loopback failures before
it was stopped; the host's WSL2 networking, not the distribution, is the common factor. Windows
native runs are healthy (the first loaded full-suite run had zero test-level failures), and the
discarded proxy measurement was taken there.

**The runner-side race reference** (run 44874926108's race step, logs; run 44892215975 supplies the
committed baseline): wall 313 s; per package `internal/app` 269.6 s, `platform` 52.1, `workspace`
42.3, `state` 33.0, `execution` 21.8, `harness/codex` 21.1, `harness` 15.6, `harness/omp` 13.0,
`probeconformance` 8.0, the rest ≤ 5.3 s. Run 44892215975's summarizer tables name the five
outcomes over the 30 s cap: `TestOutputBackpressureRecoversWithOneTruncationMarker` (170.05 s,
`internal/app`), `TestRawOutputBudgetDropsWithoutFailingSemanticEvents` (125.79 s) and three
`internal/state` tests at 31–32 s; the audit's earlier run measured `internal/app` 385.7 s on the
same step. That 1.4x run-to-run swing is why the +20 % band stays advisory until W6-12, which
should widen the sample before it flips the thresholds.

**Full plain `go test -count=1 -json ./...` stream (this host, Windows, go1.27.4, revision
6a815c0f)**: 4,007 tests over 22 packages, 17,811 event lines, exit 1. This run is **not** usable as
a baseline and is recorded as the host-load artifact it is: `internal/app` hit the 600 s package
timeout (a package-level failure with no named test failure — exactly the incomplete stream the
summarizer now refuses with exit 2) and the load-inflated p95 of 3.37 s is not a measurement of the
same work CI runs (no race detector, Windows test set, loaded host). Its value is the two things it
proves: the summarizer handles a real ~18k-line stream and reports the cap crossings (four tests
over 30 s on a loaded host), and the incomplete-stream rule fires on real data.

**The summarizer against the runner's own scoped stream** (extracted from run 44874926108's daemon
job log): the table prints and the verdict is exit 0 (`tests=498 skipped=31 packages=2 p95=0.000s`).

**Browser suite inventory today (real tree)**: 16 discovered suites; 7 wired (the render step's
list); 9 manual (six specs + three node tests); gate exit 0, and exit 1 with
`browser/tests/zz-planted-probe.spec.mjs` planted (the contract's acceptance exercise) and exit 0
again after its removal; a workflow comment naming an unlisted spec no longer classifies it
(fix round 1, finding P2-1; fixture below).

**actionlint, classifier, ruff**: actionlint 1.7.12 `-shellcheck= -pyflakes=` → 0 findings, exit 0,
with a broken-`if:` mutation exiting 1; `test-ci-changed-path-classifier.py` → 18 tests OK, exit 0;
ruff 0.17.5 (the version `pyproject.toml` requires): `ruff check scripts/ci` and
`ruff format --check scripts/ci` both exit 0, with the two new files inside that count. (The ExUnit
flag probe is recorded above, under its own heading.)

**The job's own helper suites on this host**: `test-assert-folded-gates.py` → 6 tests OK, exit 0;
`test-no-legacy-routes-audit.py` → 11 failures/4 errors, exit 1 with the default `PATH` (its
`shutil.which("bash")` resolves to the WSL launcher, which cannot read the Windows temporary paths
the suite passes — the pre-existing host resolution W1-01 recorded, identical on a pristine tree)
and 6 tests OK, exit 0 with Git's bash first on `PATH`; `scripts/audit-no-legacy-routes.sh` → exit 0
over 1,444 tracked files; the gate plumbing end to end (`go-test-json-gate.py --stdio-boundary
--assert …` then the summarizer) → assert verified, table printed, exit 0.

## Evidence plan (gates run, in this order, all foreground)

1. `python3 scripts/ci/test-ci-changed-path-classifier.py` — must stay green with the new steps.
2. `scripts/ci/go-test-durations.py` on synthetic fixtures: green stream (exit 0 + table), planted
   40 s test (exit 1, names the test), p95 over the limit (exit 1, names p95 vs baseline), a failed
   test's output replayed, an unexplained failed package (exit 2 + the package's own output),
   malformed stream (exit 2), empty stream (exit 2); then against the runner's race stream of run
   44892215975 (table + the committed baseline).
3. `scripts/ci/assert-browser-suite-coverage.py` on the real tree (exit 0, 16-row table) and on
   synthetic fixture roots: unlisted spec (exit 1), a comment-only mention of an unlisted spec
   (exit 1; fix round 1), matrix rot (exit 1), wired+manual overlap (exit 1), all-wired green
   (exit 0); plus the planted-file acceptance exercise in the real tree (add an unlisted spec, run,
   remove it).
4. `actionlint -shellcheck= -pyflakes= .github/workflows/*.yml` (pinned 1.7.12 shape; shellcheck
   integration off as in the CI step) — 0 findings, exit 0; plus one mutation on a scratch copy
   proving the oracle is not vacuous.
5. `ruff check scripts/ci` + `ruff format --check scripts/ci` with the pinned ruff 0.17.6 — the two
   new files must not add findings (the advisory step measures this).
6. `python3 -m py_compile` on both new scripts; `git diff --check`.
7. The runner's race stream from run 44892215975's daemon job (`p95=0.260s`) — the fix-round-1
   committed baseline; the WSL Linux race attempts are recorded above as discarded method.
8. `python3 scripts/ci/test-no-legacy-routes-audit.py` and `test-assert-folded-gates.py` where this
   host can run them (Git's bash first on `PATH`, per W1-01's host note).
9. The control-side formatter (review round 1): `MIX_ENV=test mix compile --warnings-as-errors` and
   `mix format --check-formatted` in `control/`, then one small test file under
   `--formatter ExUnit.CLIFormatter --formatter SymmetryControl.SlowestFormatter` (the table
   prints, `max_cases` is not 1).

Not runnable here (CI is their oracle, per the contract): the actual runner run — the control job's
`mix test` step, the race step's JSON capture, the summary step's table on the runner, and the
inventory step under `version`-shaped routing; the conductor verifies them on the draft PR.

## Residual risks and known limits

* **The baseline is one runner run** (run 44892215975's race stream, p95 0.260 s; fix round 1).
  The runner's own run-to-run swing (`internal/app` 269.6 → 385.7 s across runs) is larger than the
  +20 % band, so the p95 arm can swing across it; the step is advisory, prints `p95=`, `baseline=`
  and `ratio=`, and W6-12 should re-derive the band from several runs when it flips the thresholds
  to blocking.
* **Expected advisory reds** (W1-01 precedent): run 44892215975's race stream carries five outcomes
  over the 30 s cap (170.05 s `TestOutputBackpressureRecoversWithOneTruncationMarker`, 125.79 s
  `TestRawOutputBudgetDropsWithoutFailingSemanticEvents`, three `internal/state` tests at 31–32 s),
  so the cap arm is red on every current green run, and the fresh p95 band may be crossed by
  run-to-run swing. Both are recorded numbers, not failures of this increment; W6-10/W6-12 own the
  burn-down and the flip.
* **The race step's log becomes JSON** (7.87 MB on run 44892215975, following the scoped ordinary
  step's precedent) and the per-package `ok …s` lines disappear; the summary table replaces them,
  and fix round 1 makes the summary step replay every failed test's and unexplained failed
  package's output, so a red run's diagnostics are readable there.
* **The inventory gate counts any non-comment workflow token** naming a spec (YAML comments are
  stripped; fix round 1, finding P2-1); execution proof for the render family stays
  `assert-playwright-json.py`'s job, and W6-02 can tighten the wired predicate to step arguments if
  the remaining surface proves too loose.
* **`browser/README.md`'s matrix is manual data**: the gate proves coverage, not truthfulness of a
  row's stated driver.
* **Node-suite drivers are local-only today** (`test:connection-env`, `test:control-isolation`, and
  `goal-context-fixtures.test.mjs` with no script at all); the matrix records that, and W6-02 owns
  any wiring.
