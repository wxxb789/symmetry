# rf-w2-01-ci-test-observability plan — durations + browser-suite inventory

Contract: `goal-rf-w2-01-ci-test-observability.md` (GDD, 2026-11-21). Baseline for every coordinate in
this plan: this worktree's HEAD `6a815c0fc72393f9ccc1cdde5a516a2f8176eb9c` (`6a815c0f`,
`origin/main` at plan time), branch `refactor/rf-w2-01-ci-test-observability`. Binding inputs:
program plan §2 M1/M9/M10, §5 W2-01, §10.5 and decision D7; findings `tests-cross-cutting.md` T14
(per-test duration telemetry missing for two of three suites) and T4 (≈4,600 lines of browser/node
tests never run in CI); findings `scripts-ci.md` F4 (six of thirteen browser specs never run in CI)
and S6b (browser suite inventory test + manual-only label); `docs/design/quality.md` (gate policy);
and the contract's owned-file set (four files plus this plan; every other path is read-only here).

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `.github/workflows/ci.yml` | modified | `mix test` step → `mix test --slowest 20` (control job); race step emits `go test -json` through the shared gate and writes `$RUNNER_TEMP/daemon-go-race.json` (daemon job); one new advisory duration-summary step (daemon job); one new blocking inventory step (legacy-route-audit job) |
| `scripts/ci/go-test-durations.py` | new | `go test -json` duration summarizer: prints the top-N test table and a package table, fails a test >30 s or p95 +20 % over the committed baseline |
| `scripts/ci/assert-browser-suite-coverage.py` | new | inventory gate: every `browser/tests/*.spec.mjs` / `browser/scripts/*.test.mjs` is named by a workflow or listed in `browser/README.md`'s manual matrix |
| `browser/README.md` | modified | new "Manual-only suite matrix" section (machine-parsed by the gate) listing the nine suites CI does not run, why, and how to run each today |
| `docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md` | new | this plan |

No new workflow job: the classifier fixture suite
`scripts/ci/test-ci-changed-path-classifier.py` pins the job model (every job must gate on an
existing classifier output, and the `changes` job must declare exactly the three outputs), and it is
explicitly not editable here (W1-01/W3-01 own it). Both new steps therefore grow inside jobs that
already exist, exactly as W1-01 grew the advisory quality plane.

## The duration plane

### ExUnit (control job)

`- run: mix test` becomes `- run: mix test --slowest 20`. The control job's `test` alias
(`control/mix.exs`: `test: ["symmetry.check_test_db", "ecto.create --quiet", "ecto.migrate --quiet", "test"]`)
forwards CLI arguments to the real task, and `--slowest N` makes ExUnit's own CLI formatter print
the N slowest tests after the run summary. Verified against the CI-pinned Elixir (1.20.4, the version
`mise` holds on this host) on a synthetic two-test project; measured shape:

```
Top 20 slowest (0.1s), 84.5% of total time:

  * test slow case (SlowestProbeTest) (156.6ms) [test/slowest_probe_test.exs:8]
  * test fast case (SlowestProbeTest) (0.00ms) [test/slowest_probe_test.exs:4]
```

Pass/fail semantics are untouched: the step already fails on a red `mix test`, and `--slowest` only
adds output. No other `mix test` invocation (provider broker e2e, the goal-receipt JSON-formatter
run, the folded Pi E2E steps) is touched, so none of their formatter contracts move.

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
      --stderr "$RUNNER_TEMP/daemon-go-race.stderr" \
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
  comment naming the exact measuring command, host class, toolchain and revision (see "Measured
  baselines"). The CI stream is the race pass while the baseline is measured with the same
  `-json` command on this host without `-race` (a race-shaped stream cannot be produced here: the
  WSL environments' loopback HTTP stalls, below); the p95 falls in the fixed-deadline slow tail,
  whose per-test durations do not scale with host or detector, which is what makes the proxy
  usable as an advisory ratchet seed. The step prints `p95=`, `baseline=` and `ratio=` side by
  side; W6-12 owns the move to a runner-measured baseline when the thresholds flip to blocking.
* **Exit codes.** `0` both arms pass; `1` a threshold crossed (the verdict, in the step's own exit
  status); `2` usage/missing-file/malformed-stream/no-outcomes (never silently green).

### Go: CI step and advisory framing

```yaml
- name: Summarize Go test durations
  if: always()
  continue-on-error: true
  run: |
    python3 ../scripts/ci/go-test-durations.py "$RUNNER_TEMP/daemon-go-race.json"
```

`if: always()` makes the table print even when the race suite went red — the run that needs the
attribution most. `continue-on-error: true` follows the contract's evidence pointer (§10.5: "floors
stay advisory until W6-10/W6-12") and W6-12's own row ("flip duration thresholds/ratchets to
blocking"): the summarizer's exit status is the verdict and reddens the *step*, while the job keeps
its current conclusion semantics, the same advisory-plane contract W1-01 shipped for golangci-lint,
Credo, govulncheck, shellcheck and ruff. A planted 40 s test therefore fails the step with exit 1
(fixture evidence below); W6-12 flips the step to blocking after re-baselining against a runner run.

## The browser-suite inventory gate

### Semantics (`scripts/ci/assert-browser-suite-coverage.py`)

Discoveries are the filesystem's: `<root>/browser/tests/*.spec.mjs` and
`<root>/browser/scripts/*.test.mjs` (16 files today: 13 specs, 3 node tests). Every discovered file
must be classified exactly once:

* **wired** — some `.github/workflows/*.yml`/`*.yaml` contains a path token whose suffix equals the
  file's browser-relative path (`tests/foo.spec.mjs`) or repository-relative path
  (`browser/tests/foo.spec.mjs`), at a `/` boundary. The render step's argument list
  (`tests/workspace-shell.spec.mjs …`) matches this way, as would a future compose-family step. A
  mention in a comment counts as naming; what proves *execution* for the wired render family is
  `scripts/ci/assert-playwright-json.py`'s executed-set assertion, which this gate deliberately does
  not duplicate.
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

Pre-existing step semantics: `mix test --slowest 20` adds output only; the race step keeps its
command, its stdio-only boundary, its 600 s budget and its exit-status meaning, gains `-json` and
the one already-required named assertion, and now writes the event stream next to it; the new
summary step is `continue-on-error`, so the daemon job's conclusion cannot change because of it.

## Out of scope

* **Wiring browser specs into CI** (W6-02, decision D7): the gate classifies them; the fixture job /
  compose wiring / driver ports are W6-02's, per D7 and the contract's clause 4.
* **Coverage plane** (W2-17), **classifier extraction** (W3-01), **`ci.yml` program extraction**
  (W6-01): serialized owners of `ci.yml`/`test-ci-changed-path-classifier.py`.
* **Blocking duration thresholds** (W6-12) and the slow-test burn-down itself: this slice records
  the numbers; W6-12 flips and re-baselines.
* **Fixing the slow tests** the table will name, and any change to `daemon/**`, `control/**` or
  `browser/tests/**`: read-only here.
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
10. **Expected advisory reds are recorded, not hidden** (W1-01 precedent): the first runner run may
    exceed the host-measured baseline or cross 30 s on a pre-existing test; the step prints both
    numbers and stays advisory until W6-12.

## Measured baselines

Recorded on this delivery host; every number below was produced by the command named with it, at
this revision unless stated.

**ExUnit `--slowest` (Elixir 1.20.4 / OTP 29, synthetic probe)**: the flag exists on the pinned
version and prints the table quoted above (`Top 20 slowest (0.1s), 84.5% of total time:` …), exit 0.
The probe project is throwaway (`mise`-held Elixir 1.20.4-otp-29); it exercises the flag, not the
control suite, which needs Postgres and is CI's to run.

**Scoped plain `go test -json` stream (the existing `$RUNNER_TEMP/daemon-go-test.json` step, from CI
run 44874926108's daemon job log)**: 529 terminal test outcomes, p95 0.00 s, max 1.51 s
(`TestLinuxLegacyOpenCodeRefusesBeforeHealthRequest`), 2.0 s of summed test time for a ~4 s step —
the measurement that rules this stream out as the summarized one.

**Committed baseline: the full plain `go test -count=1 -json ./...` stream on this host (Windows,
go1.27.4, revision 6a815c0f)**, the same command CI runs minus `-race`, with the per-package
timeout raised to 1500s: **p95 = 4.910 s** (the constant in the script). The stream is complete —
4,340 outcomes, 56 skips, 22 packages, 3 Windows-host flakes (`internal/execution` pipe tests and
one `harness/pi` fixture test) outside the slow tail — and the raise is what makes it complete: two
earlier runs with the repository's 600s cap truncated `internal/app` (and once `internal/workspace`)
into package timeouts on this contended host, and both are recorded as discarded, not silently
averaged. The p95 is deadline-anchored rather than CPU-anchored: 23 outcomes sit inside
[4.9 s, 5.2 s] (`claudeProbeTimeout = 5 * time.Second`, `daemon/internal/harness/registry.go:29`)
and the p95 index lands on the cluster's lower edge — p50 0.010 s, p90 1.880 s (the 2 s attach
deadline plateau), p95 4.910 s, p99 20.160 s, max 155.26 s, 220 outcomes ≥ 4.9 s, 23 over the 30s
cap. The cluster's fixed waits are host- and detector-independent; the percentile only stays
anchored while that cluster still defines it, which is the part W6-12 must re-measure on the runner.

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
native runs are healthy (the first loaded full-suite run had zero test-level failures; the complete
run's three host flakes are listed above), so the proxy baseline is measured there.

**The runner-side reference for W6-12** (run 44874926108's race step, logs): wall 313 s; per package
`internal/app` 269.6 s, `platform` 52.1, `workspace` 42.3, `state` 33.0, `execution` 21.8,
`harness/codex` 21.1, `harness` 15.6, `harness/omp` 13.0, `probeconformance` 8.0, the rest ≤ 5.3 s.
The audit's earlier run measured `internal/app` 385.7 s on the same step — a 1.4x run-to-run swing
on the runner itself, which is why the +20 % band stays advisory until W6-12 re-baselines there.

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
again after its removal.

**ExUnit, actionlint, classifier, ruff**: `mix test --slowest 20` verified on the CI-pinned Elixir
1.20.4 through the alias shape (probe above); actionlint 1.7.12 `-shellcheck= -pyflakes=` → 0
findings, exit 0, with a broken-`if:` mutation exiting 1; `test-ci-changed-path-classifier.py` → 18
tests OK, exit 0; ruff 0.17.5 (the version `pyproject.toml` requires): `ruff check scripts/ci` and
`ruff format --check scripts/ci` both exit 0, with the two new files inside that count.

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
   40 s test (exit 1, names the test), p95 over the limit (exit 1, names p95 vs baseline), malformed
   stream (exit 2), empty stream (exit 2); then on the real full race stream (table + verdict).
3. `scripts/ci/assert-browser-suite-coverage.py` on the real tree (exit 0, 16-row table) and on
   synthetic fixture roots: unlisted spec (exit 1), matrix rot (exit 1), wired+manual overlap
   (exit 1), all-wired green (exit 0); plus the planted-file acceptance exercise in the real tree
   (add an unlisted spec, run, remove it).
4. `actionlint -shellcheck= -pyflakes= .github/workflows/*.yml` (pinned 1.7.12 shape; shellcheck
   integration off as in the CI step) — 0 findings, exit 0; plus one mutation on a scratch copy
   proving the oracle is not vacuous.
5. `ruff check scripts/ci` + `ruff format --check scripts/ci` with the pinned ruff 0.17.6 — the two
   new files must not add findings (the advisory step measures this).
6. `python3 -m py_compile` on both new scripts; `git diff --check`.
7. The WSL Linux race run (`/opt/go/bin/go` 1.27.0, `-race`, same command) that produces the
   committed baseline and validates the summarizer on a real ~4,000-outcome stream.
8. `python3 scripts/ci/test-no-legacy-routes-audit.py` and `test-assert-folded-gates.py` where this
   host can run them (Git's bash first on `PATH`, per W1-01's host note).

Not runnable here (CI is their oracle, per the contract): the actual runner run — `mix test
--slowest 20` inside the control job, the race step's JSON capture, the summary step's table on the
runner, and the inventory step under `version`-shaped routing; the conductor verifies them on the
draft PR.

## Residual risks and known limits

* **The baseline is a proxy** (same command, this host, no `-race`; see "Measured baselines" for
  why no race-shaped stream was obtainable). The p95 falls in the fixed-deadline tail (5.00 s probe
  bounds, 2 s attach deadlines) whose per-test durations do not scale with host or detector, but the
  runner's race stream can still land outside the +20 % band, and the runner's own run-to-run swing
  (`internal/app` 269.6 → 385.7 s) is larger than that band. The step is advisory, prints `p95=`,
  `baseline=` and `ratio=`, and W6-12 re-baselines on the runner when it flips to blocking.
* **Expected advisory reds on the first runner run** (W1-01 precedent): a pre-existing test over
  30 s under `-race` on the runner and/or a p95 over the band. Both are recorded numbers, not
  failures of this increment; W6-12 owns the flip.
* **The race step's log becomes JSON** (≈4–6 MB for the full suite, following the scoped ordinary
  step's precedent) and the per-package `ok …s` lines disappear; the summary table replaces them.
* **The inventory gate can be satisfied by a workflow comment** naming a spec; execution proof for
  the render family stays `assert-playwright-json.py`'s job. W6-02 can tighten the wired predicate
  to step arguments if the comment surface proves too loose.
* **`browser/README.md`'s matrix is manual data**: the gate proves coverage, not truthfulness of a
  row's stated driver.
* **Node-suite drivers are local-only today** (`test:connection-env`, `test:control-isolation`, and
  `goal-context-fixtures.test.mjs` with no script at all); the matrix records that, and W6-02 owns
  any wiring.
