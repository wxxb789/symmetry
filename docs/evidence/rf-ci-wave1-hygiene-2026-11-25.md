# Evidence — rf-ci-wave1-hygiene

Goal: CI queue/timeout hygiene, the daemon advisory relocation, one frontend test run, and the Elixir
build cache, plus the appended `goal_0006` migration-test flake item. Contract: the goal file
`goal-rf-ci-wave1-hygiene.md` in the gdd contract store (owner ruling 2026-11-25: I1, items 1-5, plus the
appended flake item). Plan of record: `docs/plans/2026-11-25-rf-ci-wave1-hygiene-plan.md`.

Revision under test: `refactor/rf-ci-wave1-hygiene` at `61399984` (drafted against `origin/main`
`30ed5ada`; the three commits between them touch no owned file). Implementation date: 2026-11-25.

Stage boundary: this record was produced by the implementing stage, which may not push, commit or open a
pull request; every GitHub-side gate was handed over in §8 with its exact command. The runtime
acceptance of 2026-11-25 (UTC) then executed §8 items 1-3 and 5-6 against PR #292, and §8 records their
observed results; only item 4 (the after-window) remains open, conductor-owned post-merge. §1-§7 are
executed results from this host; §8 items 1-3 and 5-6 are executed results from the runtime session.

## 1. File inventory and hashes

Working tree at the time of this record (all paths in scope; `git status --porcelain -uall --no-renames`):

| path | pre-change sha256 (`HEAD`) | changed sha256 |
| --- | --- | --- |
| `.github/workflows/ci.yml` | `a7d2d138475fd6397ec970e14e5d2b1ef2f013be3c67acff6ecde4cf5623c95c` | `34357934e67a2cbd98fbab43ff801b77642883f5ba338bd36b41cb9a4f484f29` |
| `.github/actions/contracts-build/action.yml` | `2d36e663dc8b0f34eecfd44b0dc4e0758405c2797259f0d3671d3040dd505e55` | `5c49e6cea58e0b0516238378d9dcc0abd6b0382784e351d89a55f057d29f6767` |
| `docs/design/quality.md` | `f34eca086f43ae1ad4751e1e92c59b69b222202dd4e76aa50fc5e9deedf9be41` | `04adc7faa77acb7831f7638c7ca4a243375de11547f81f0ce53a18b172e58b75` |
| `scripts/ci/test-ci-structure-extraction.py` | `0f6ff3a0a459a3920cd93dcaef6f5a4b942201813f44ed4c2a2e1cabfe007bb5` | `485a7262dd67d595ce85907c8a640d720f6f99d4767933a7de1b80400b2b699c` |
| `control/test/symmetry_control/migrations/goal_0006_migration_test.exs` | `d1392647d9cc1afa0fd65382ead4cefca229dd5f5408fd10eb50cea5dd37f0b9` | `146c86cf0f8a477ab1ab31381e813ec60012dfd6d36929ff42a1ca82a4bdcbf7` |
| `docs/plans/2026-11-25-rf-ci-wave1-hygiene-plan.md` | new | `49783f6b97687d4e255ee1d009f5e419d2634cdfbcfdde2f7040ce4fb48699d4` |

`ci.yml` is 2215 lines after the change (2158 before).

Fix r1 (2026-11-26, the round whose review is recorded in §5) re-measured the two files whose bytes changed
again in this round; both hash equal in the working tree and in the scratch commit every fix-round gate ran
on — `scripts/ci/test-ci-structure-extraction.py` `3cd77159…`,
`control/test/symmetry_control/migrations/goal_0006_migration_test.exs` `fde9c590…`; §3 records the commands
and §7 binds the migration test's bytes at its own run.

Fix r2 (2026-11-26, the PR #292 CI-red round; conductor rulings R0-R7 in §5) changed four of the six
paths again — `.github/workflows/ci.yml` `34357934…`, `scripts/ci/test-ci-structure-extraction.py`
`485a7262…`, `control/test/symmetry_control/migrations/goal_0006_migration_test.exs` `146c86cf…` and
`docs/plans/2026-11-25-rf-ci-wave1-hygiene-plan.md` `49783f6b…`; `action.yml` and `quality.md` are
unchanged from the table above. §3 records every r2 gate with its exit code.

## 2. Censuses re-measured at `61399984`

| id | command | result |
| --- | --- | --- |
| C1 | `grep -c "continue-on-error: true" .github/workflows/ci.yml` | 8 (before and after; the two advisories move job, none is added or removed) |
| C2 | `grep -nE "^    timeout-minutes" .github/workflows/ci.yml` | before 5 (585, 637, 678, 705, 1353); after 16 — the five existing ceilings plus eleven job headers (`changes` 5, `record-cleanliness` 5, `citation-existence` 5, `contracts` 10, `control` 25, `daemon` 30, `daemon-audit` 20, `legacy-route-audit` 10, `integration` 10, `compose` 40, `browser-fixture-linux` 35; `control`/`legacy-route-audit` as amended by rulings R3/R4, fix round r2) |
| C3 | `grep -nE "^  [a-z][a-z0-9-]*:$" .github/workflows/ci.yml` | before 16 (15 jobs + `on.push`); after 16 job headers inside `jobs:` + `on.push` = 17 matches (16 jobs) |
| C4 | `grep -nE "^concurrency:" .github/workflows/ci.yml` | before 0; after 1 (line 15) |
| C5 | `grep -n "mix deps.get" .github/workflows/ci.yml .github/actions/contracts-build/action.yml` | before 5 (ci.yml 195, 619, 787, 2012; action 44); after 5, each preceded by its cache step (ci.yml 206/636/810/2060, action 37; the browser-fixture pair shifted by the fix-round r2 comment trim) |
| C6 | `grep -nE "vitest\|@symmetry/frontend test" …` | before 4 (action 26 plain run; ci.yml 579, 689 comments, 691 coverage run); after 3 (ci.yml comments and the coverage run; the action's plain step is gone) |
| C7 | `grep -rn "actions/cache" .github` | before 0; after 5 cache steps (`ci.yml` ×4, action ×1) |
| C8 | `git ls-tree -r --name-only HEAD control/deps \| wc -l` | 0 (deps are not vendored; `.gitignore` lines 43-44 ignore `/control/_build/` and `/control/deps/`) |
| C9 | `grep -nA1 "name: Check auditable model-routing workflow" .github/workflows/ci.yml` | the step is now in `daemon-audit` with exactly its `name` and `run: \|` keys (no `continue-on-error`, no narrowing `if:`) |
| C10 | `grep -nA3 "Measure golangci-lint findings (GOOS=linux, advisory)\|Measure govulncheck findings (advisory)" .github/workflows/ci.yml` | both blocks still carry `if: always()` + `continue-on-error: true`, now inside `daemon-audit` |
| C11 | `grep -rnE "pnpm[^\"]*\btest\b" .github` and a scan for `working-directory: frontend` / `cd frontend` (F4's "every spelling" census, on the fix-round tree) | 5 hits, every one `pnpm --filter symmetry-browser-acceptance test …` — the browser acceptance package, not `@symmetry/frontend` (exempt); 0 hits for the `frontend` work-directory forms; the two frontend spellings the review constructed are covered by the F4 witnesses (§4) |

Corrections the measurements force on the contract's own assumptions:

* `mix deps.get` is **1-3 s** at every site in the before-window (§6), so the cache's load-bearing part is
  the compile of the dependency tree (visible in the compile/test steps and in
  `browser-fixture-linux`'s combined build step), not the Hex download. The audits' "deps are vendored,
  1-3 s each" is still wrong about vendoring (`control/mix.lock` holds 26 entries, `control/deps` has no
  tracked files), but the observed deps.get duration is genuinely small.
* The composite action's body is **one** API step (`Run ./.github/actions/contracts-build`), so item 4's
  duplicate cost is not separately visible in run history; its before/after signal is that step's
  duration (1.67-2.10 min in the before-window).
* `daemon`'s moved content in the before-window is 2.56-3.67 min (golangci 0.57-0.95 + govulncheck
  0.10-0.15 + model-routing 1.88-2.57), i.e. 154-220 s, consistent with the contract's 216 s (run
  `45586585296`).
* The contract's §2 table records a 15-minute step cap for `coverage-control`; no such cap exists in
  `ci.yml` (the 15 is `control`'s Credo step). The job-timeout pins and the workflow agree with each
  other and with the table's *job timeout* column; only that step-cap cell is mis-attributed.
* Run `45630186081` (the run the appended flake item cites) has **two attempts**: attempt 1 failed on
  `coverage-control` (the `tasks_context_snapshot_ownership_fkey` report) and attempt 2 re-ran that job
  only. §6 reports both attempts; a per-attempt view is required for any honest runner-minute sum.

## 3. Local gates (executed, exit codes)

| gate | command (working directory: repository root) | result |
| --- | --- | --- |
| structure extraction | `python3 scripts/ci/test-ci-structure-extraction.py` | `Ran 70 tests`, `OK`, exit 0 (drafting baseline 40; this change adds 5 pins and 30 test methods — 8 in the fix round, all F2/F3/F4 witnesses; 52 of the 70 are mutation tests) |
| F2/F3/F4 pin-gap probe (not a repo gate; the fix round's mutation inputs) | `python3 <scratch>/pin_gap_probe.py scripts/ci/test-ci-structure-extraction.py` | pre-fix: all six rows green; post-fix: all six rows red — `if: false` on the owner step, the golangci `if: always()` / `timeout-minutes: 12` drops, the govulncheck `continue-on-error: true` drop, the `working-directory: frontend` + `pnpm test` spelling, and the backslash-continued `run: \|` body |
| goal_0006 migration test (scratch harnesses: Elixir 1.20.4 / OTP 29.0.6, PostgreSQL 18.6, run-local port; both match the CI control job) | `mix format --check-formatted`; `MIX_ENV=test mix compile --warnings-as-errors`; `mix test test/symmetry_control/migrations/goal_0006_migration_test.exs`; the same file at `:686` and with `--repeat-until-failure 50`; then the §7 drop-membership mutant | format exit 0 (silent); compile exit 0; the whole file `58 passed`, exit 0 (95.5 s); filtered `:686` run `1 passed, 57 excluded` exit 0; repeat mode 51 runs (countdown 50→0), every one `1 passed, 57 excluded`, exit 0; mutant: 1 failure at the new arm (`:709` on the r1 bytes, `:713` in the r2 mutant file) — all re-run on the fix-round r2 bytes |
| fix-round r2 Credo strict ratchet (rulings R0/R1; the step `control` runs, `ci.yml:213-225`) | from `control/` with its deps fetched: `mix credo --strict 2>&1 \| tee <log>` then `python3 ../scripts/ci/assert-credo-strict-baseline.py --log <log>` | `credo_strict_exit_status=30` (a severity bitmask, ignored by the gate); ratchet exit 0 — `564 issue(s)` over `225 file(s)` and `70 check(s)`: 5 warnings, 326 refactoring opportunities, 160 code readability issues, 73 software design suggestions — every category equal to `scripts/ci/credo-strict-baseline.json` |
| fix-round r2 python suites (re-run on the r2 bytes) | `python scripts/ci/test-ci-structure-extraction.py`; `python scripts/ci/test-ci-changed-path-classifier.py` | structure `Ran 70 tests`, `OK`, exit 0; classifier `Ran 37 tests`, `OK`, exit 0 |
| fix-round r2 actionlint (the CI-pinned `1.7.12`, the advisory step's command shape) | `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml` | exit 0, no findings |
| fix-round citation / record-cleanliness / sanitize gates (scratch clone holding the delivered bytes; `HEAD` = its scratch commit) | `python3 scripts/ci/check-citation-existence.py --repo . --base 613999840a4e4bc1d1169a37d3775b1814671015 --head HEAD`; `python3 scripts/ci/check-record-cleanliness.py --repo . --base 613999840a4e4bc1d1169a37d3775b1814671015 --head HEAD`; `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only <each changed doc>` | citation exit 0 — `1 changed document(s) pass the path and symbol check (19 path citation(s))`; cleanliness exit 0 — `1 changed record(s) pass the root-only check`; sanitize exit 0 on `docs/design/quality.md`, `docs/plans/2026-11-25-rf-ci-wave1-hygiene-plan.md` and `docs/evidence/rf-ci-wave1-hygiene-2026-11-25.md` |
| fix-round r2 citation / record-cleanliness / sanitize gates (scratch clone holding the r2 bytes; `HEAD` = its scratch commit) | `python3 scripts/ci/check-citation-existence.py --repo . --base 4440c5c0712040ad9d1b87cafdbe66403f3115f1 --head HEAD`; `python3 scripts/ci/check-record-cleanliness.py --repo . --base 4440c5c0712040ad9d1b87cafdbe66403f3115f1 --head HEAD`; `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only <each changed doc>` | citation exit 0 — `1 changed document(s) pass the path and symbol check (19 path citation(s))`; cleanliness exit 0 — `1 changed record(s) pass the root-only check`; sanitize exit 0 on the plan, the evidence record and `quality.md` |
| fix-round whitespace and host-path scan | `git diff --check origin/main...HEAD`; a case-insensitive scan of the same diff for the user-profile directory prefix | exit 0; 0 matches |
| fix-round r2 whitespace and host-path scan | `git diff --check 4440c5c0712040ad9d1b87cafdbe66403f3115f1`; the same case-insensitive scan over the merge-base diff (`origin/main` has advanced since `61399984`, so the merge base is used) | exit 0; 0 matches |
| fix-round jscpd gate (same scratch clone) | `python3 scripts/ci/jscpd-changed.py --base 613999840a4e4bc1d1169a37d3775b1814671015 --head HEAD` | exit 0 — `7 changed file(s), 1748 scanned source(s), 1263 clone(s) in the corpus, 1 tolerated among the added lines, none new`; the tolerated clone is the pre-existing `ci.yml` 603-636 / 169-202 pair, whose added lines held no content the file did not already hold |
| fix-round r2 jscpd gate (scratch clone holding the r2 bytes; `HEAD` = its scratch commit) | `python scripts/ci/jscpd-changed.py --base $(git merge-base HEAD origin/main)` — resolved to `--base 4440c5c0712040ad9d1b87cafdbe66403f3115f1 --head HEAD` | exit 0 — `7 changed file(s), 1750 scanned source(s), 1262 clone(s) in the corpus, 1 tolerated among the added lines, none new`; the tolerated clone is the same pre-existing `ci.yml` 603-636 / 169-202 pair |
| changed-path classifier | `python3 scripts/ci/test-ci-changed-path-classifier.py` | `Ran 37 tests`, `OK`, exit 0 (unmodified file) |
| citation existence (amended `quality.md`) | `python3 scripts/ci/check-citation-existence.py --base HEAD^1 --head HEAD` in a scratch repository holding the head tree plus the amended document | exit 0 — `1 changed document(s) pass the path and symbol check (19 path citation(s))` |
| record cleanliness (this new record) | `python3 scripts/ci/check-record-cleanliness.py --base HEAD^1 --head HEAD` in the same scratch repository | exit 0 — `1 changed record(s) pass the root-only check` |
| YAML shape probe (not a repo gate; independent of the structure suite, which parses text) | `python3` + `yaml.safe_load` over both changed YAML files | parses; 16 jobs; all 16 job timeouts equal the amended table (`control` 25 and `legacy-route-audit` 10 are the R3/R4 rulings recorded in §5; the rest of the §2 table) and the job sets match both ways; `concurrency` block equals the pinned three lines and cancels pull requests only; exactly one step runs `vitest` (`coverage-frontend`, pinned command); five `actions/cache@v6` steps, each with both paths and one key carrying `${{ runner.os }}`+`otp-29.0.6`+`elixir-1.20.4`+`${{ hashFiles('control/mix.lock') }}` and no `restore-keys`, each immediately preceding its `mix deps.get` step; `daemon-audit` steps exactly checkout, setup-go, golangci, govulncheck, model-routing |
| helper suites also run locally | `python3 scripts/ci/test-citest.py`, `test-go-test-durations.py`, `test-assert-go-coverage-floor.py`, `test-check-lockfile-registry.py`, `test-assert-folded-gates.py`, `test-assert-exunit-json.py`, `test-go-test-json-gate.py`, `test-assert-playwright-json.py`, `test-assert-runner-matches-target.py`, `assert-browser-suite-coverage.py` | all exit 0 (`OK`; `test-citest.py` and `test-go-test-json-gate.py` skip host-dependent cases) |
| `test-no-legacy-routes-audit.py` | `python3 scripts/ci/test-no-legacy-routes-audit.py` | 11 failures, **pre-existing on this host**: the same 11 failures reproduce on an unmodified copy of the head tree (probe scratch repository on the local disk), all with `/bin/bash: <scratch>/scripts/audit-no-legacy-routes.sh: No such file or directory` (exit 127) — an environment boundary of this host's bash, not a defect of the change. CI runs this suite in `legacy-route-audit`. |

## 4. Clause-by-clause evidence rows

| clause | green command (repository root) | exit | evidence row | expected failure / baseline observed first |
| --- | --- | --- | --- | --- |
| §1 concurrency pinned, PR-only | `python3 scripts/ci/test-ci-structure-extraction.py` (`WorkflowHygieneTests::test_only_superseded_pull_request_runs_are_cancelled`) | 0 | §3; C4 = 1; block sits between `permissions:` and `jobs:` | mutation tests `test_mutation_cancelling_every_event_reddens_the_concurrency_pin` and `test_mutation_a_constant_concurrency_group_reddens_the_concurrency_pin` redden the same pin before the fix exists (M1, M2) |
| §1 live cancel behaviour | two rapid pushes to the wave PR; `gh run list --repo wxxb789/symmetry --branch refactor/rf-ci-wave1-hygiene --event pull_request --limit 5` (§8 item 2) | `cancelled` / `success` | run `45679924982` (head `70685e63`) concluded `cancelled` 25 s after the probe run was created — 11 of its 16 jobs cancelled, 5 already `success` (`changes`, `citation-existence`, `record-cleanliness`, `legacy-route-audit`, `coverage-frontend`); run `45680179932` (head `40effe5f`; tree `f8ac8e53…` identical to `70685e63`'s) concluded `success`; push-event audit (`createdAt > 2026-11-25T18:50:00Z`) empty — zero cancelled `push`-event runs of `ci.yml` | the pre-change tree has no `concurrency:` key (C4 = 0): before this change a second push queued a full superseded run (§6 before-window: 0 cancelled runs) |
| §2 every job has its ceiling | `python3 scripts/ci/test-ci-structure-extraction.py::WorkflowHygieneTests::test_every_job_carries_its_pinned_ceiling_and_no_step_cap_meets_it` | 0 | §3; C2 = 16; YAML probe prints the table (`control` 25 and `legacy-route-audit` 10 are the R3/R4 amendments, §5) | mutations `test_mutation_deleting_the_daemon_ceiling_…` (M3), `test_mutation_raising_a_ceiling_…` (M4: 360), `test_mutation_a_ceiling_below_its_step_cap_…` (M4b: compose 15, now asserted on the cap message) and `test_mutation_a_step_cap_above_its_job_ceiling_…` (a 45-min step under a 40-min ceiling, the witness the first review's finding asked for) each redden the pin |
| R0 (conductor) — the changed bytes sit exactly at the Credo strict baseline | the `control` step's ratchet (§3, fix-round r2 row) | 0 | §3: 564 issues / 225 files / 70 checks — 160 readability, 326 refactor, 73 design, 5 warning, every category equal to `scripts/ci/credo-strict-baseline.json`; no added line exceeds 98 columns | PR run `45672530392` failed `control` at 570 vs 564 (readability 166 vs 160; the six added lines at `goal_0006_migration_test.exs:697/698/701/704/710/713`); the r2 reflow returns the file to its two pre-existing issues |
| §3 relocation, blocking step, no leftover | `python3 scripts/ci/test-ci-structure-extraction.py::…::test_the_daemon_audit_job_holds_the_moved_steps_and_the_blocking_model_routing_step` | 0 | §3; byte-identity check: the 72 removed lines of `daemon` (golangci 955-997 with its two comment lines, govulncheck 998-1018, model-routing 1061-1068 at `HEAD`) equal the 72 lines now in `daemon-audit` (`old == moved` → `True`; newline-joined span sha256 `589d57dcc0c92dae3646b8b50ea6d2cf94b723fe4e3ce178fc5c95ec95eb1eb1`); the reviewer's independent span digest is `5e8da0a309eac08db02690f068e0a558baff7fd4e25fc27749c25c595d655ac0`; C9/C10. The pin also now fixes the job's `uses:` prerequisites (`actions/checkout@v7`, `actions/setup-go@v7` with `go-version: 1.27.4` and `cache-dependency-path: daemon/go.sum`), its `defaults.run.working-directory: daemon`, and every defining command line of the two advisory bodies (version/digest/archive literals and the `GOOS=linux … run --config .golangci.yml` invocation for golangci-lint; `govulncheck_version='v1.10.0'` and the `./...` invocation for govulncheck) | mutations `test_mutation_marking_the_model_routing_step_advisory_…` (M5), `test_mutation_replacing_a_model_routing_invocation_with_exit_zero_…`, `test_mutation_emptying_a_moved_advisory_body_…` (golangci invocation → `exit 0`; the false-negative the second audit found), `test_mutation_pointing_the_moved_jobs_defaults_at_the_root_…` and `test_mutation_dropping_the_moved_jobs_setup_go_…` redden the pin; `daemon` no longer names any of the three steps |
| §3 daemon-audit green / M6 / M7 | plants run `45682191821` and the final-byte run `45680179932` (§8 items 1 and 3) | reds as planted; new home green | final bytes: `daemon-audit` `success`, 196 s ≤ `daemon` 421 s — the moved blocking model-routing step gates and passes in the new home (`python3 ../scripts/ci/test-model-routing-evaluation.py` green). Plants: `daemon-audit` red on M6 — `AssertionError: planted M6: forced model-routing suite failure`, `FAILED (failures=1, skipped=1)`; `daemon` red on M7 in the race step (the plant sits outside the two package-scoped ordinary packages, so only the race pass sees it) — `WARNING: DATA RACE` at `daemon/internal/execution/containment_receipt_test.go:24`, `TestPlantedM7RaceOnly`; every other job `success` in both runs; first failing lines verbatim in §8 item 3 | the moved commands' pre-change green runs are the before-window rows (§6); `daemon` carried the same steps and passed (12-run evidence in the contract) |
| §4 one vitest step | `python3 scripts/ci/test-ci-structure-extraction.py::…::test_one_step_owns_the_frontend_suite` | 0 | §3; C6: the action lost its plain test step (`ACTION_STEPS` pin), exactly one step in both files mentions `vitest` (ci.yml's coverage step) | mutations `test_mutation_restoring_the_plain_frontend_suite_reddens_the_owner_pin` (M9) reddens both the owner pin and `check_action`, and `test_mutation_re_adding_the_plain_frontend_suite_to_a_job_reddens_the_owner_pin` re-adds the suite inside a `ci.yml` job in the `run test` spelling (the second audit's counterexample to the first pattern); thresholds untouched (the classifier suite's `VITEST_COVERAGE_FLOORS` pin still passes, 37 tests OK) |
| §4 M8 / thresholds | plants run `45682191821`, job `coverage-frontend` (§8 item 3) | red as planted; thresholds untouched | M8 red — `FAIL  src/services/refusals.test.ts > planted M8 forced failure > fails deterministically`, `AssertionError: expected true to be false`, `Tests  1 failed \| 633 passed (634)`; the thresholds are untouched and the single frontend suite run is green on the final bytes (`coverage-frontend` `success`, 66 s = 3.7 % of its ceiling; the classifier suite's `VITEST_COVERAGE_FLOORS` pin still passes, 37 tests OK, §3) | before-window: the plain run executes inside `Run ./.github/actions/contracts-build` (1.67-2.10 min) and `coverage-frontend`'s suite step passes (0.60-0.82 min) |
| §5 five cache sites | `python3 scripts/ci/test-ci-structure-extraction.py::…::test_every_mix_deps_get_site_is_preceded_by_its_elixir_cache_step` | 0 | §3; C5 = 5 sites, C7 = 5 cache steps; each key carries `${{ runner.os }}`, `otp-29.0.6-elixir-1.20.4` and `${{ hashFiles('control/mix.lock') }}`, no `restore-keys`; env split test/dev/prod; the action's step keeps `if: inputs.build-control-release == 'true'` | mutations `test_mutation_deleting_a_cache_step_…` (M11), `test_mutation_stripping_the_lock_hash_from_a_cache_key_…` (M12) and `test_mutation_a_sixth_mix_deps_get_without_a_cache_step_…` each redden the pin; `actions/cache@v6` resolves (`git ls-remote` → `refs/tags/v6` = `55cc8345863c7cc4c66a329aec7e433d2d1c52a9`) |
| §5 restore lines / M13 | plants run `45682191821`, five `actions/cache@v6` sites (§8 item 3) | all five sites warm; red as planted | all five sites restored warm — verbatim `Cache restored from key:` values in §8 item 3 for `control`, `coverage-control` (plus the post-step `Cache hit occurred on the primary key …, not saving cache.`), `pi-control-e2e-windows`, `browser-fixture-linux` and the `contracts` action; no `Cache not found` line at any site. M13 red under the warm `control` restore at `Run MIX_ENV=test mix compile --warnings-as-errors` — `warning: variable "unused_m13" is unused`, `Compilation failed due to warnings while using the --warnings-as-errors option` (the Credo strict ratchet passed first: 564 issues) | before-window per-site baselines are in §6 |
| §6 before-window | `gh api` collection (§6 command) | 0 | §6 tables | — |
| §6 after-window | *deferred — conductor-owned post-merge* (§8 item 4) | — | seed row `45680179932` (final-byte green run): wall 573 s (9m33s), queue 0 s, 2738 job-seconds = 45.63 runner-min against the §6 mean 47.79 (−4.5 %); `daemon` 421 s (−161 s vs its 9.70-min mean) offset by the new `daemon-audit` job (196 s); the saving comes from the vitest de-duplication and the cache; no job above 40 % of its ceiling (max `integration` 30.8 %); the 5-run window is collected by the conductor after merge | — |
| flake item | ExUnit: `mix test test/symmetry_control/migrations/goal_0006_migration_test.exs:686` | 0 | §7: fixed row `1 test, 0 failures`; the same harness with the pre-fix row under the forced trigger order: `1 test, 1 failure` with `tasks_context_snapshot_ownership_fkey` | pre-fix red reproduced first (§7). **The contract's literal fix sentence is superseded by proof** (deviation ratified by conductor ruling 2026-11-26 (contract); see §7): "make the row violate only the asserted FK" is unsatisfiable at the schema this test builds — `AddPlanningTaskIdentityGuards` (`20260910000000`, which drops the 4-column ownership FK) is not in `MigrationModules.base_migrations()` (ends at `20260907000000`, `VersionRequestHashes`, `control/test/support/migration_modules.ex:65-87`), `migrate_goal_up!/0` runs only `AddGoal0006ControlPlane`, `tasks_goal_fields_all_or_none` requires `context_snapshot_id IS NOT NULL` (`20260909000000_add_goal_0006_control_plane.exs:1237-1245`) and `context_snapshots_work_item_ownership_fkey` (same file, `:425-430`) forces the referenced pair into `work_items`, which is exactly what the asserted work-item FK needs absent |
| branch protection | `gh api repos/wxxb789/symmetry/branches/main/protection` and `.../rulesets` | 404 "Branch not protected"; `[]` | no required-check contexts exist, so no job name is a protected context and nothing is renamed | re-read before the wave PR (§8) |
| citation gate for the amended document | see §3 | 0 | 19 path citations pass | — |
| reviewers | 3 dispatches (§5) plus the fix round's own probes (§3) | — | §5 | — |
| F2 (fix round) — the vitest owner cannot be silently disabled | `python3 scripts/ci/test-ci-structure-extraction.py::WorkflowHygieneTests::test_one_step_owns_the_frontend_suite` | 0 | the owner step's exact scalar map (`name`, `run`, no `if`) is asserted; `test_mutation_adding_an_if_false_to_the_owner_step_reddens_the_owner_pin` reddens the pin on `if: false`; the suite runs in `legacy-route-audit` (text parse, no user dimension) | pre-fix characterisation: `if: false` on the owner step left the pin green (§3 probe) |
| F3 (fix round) — the moved advisories keep their full step shape | `python3 scripts/ci/test-ci-structure-extraction.py::WorkflowHygieneTests::test_the_daemon_audit_job_holds_the_moved_steps_and_the_blocking_model_routing_step` | 0 | `ADVISORY_STEP_SCALARS` pins each moved advisory's scalar map (golangci: name, `if: always()`, `continue-on-error: true`, `timeout-minutes: 12`, `run: \|`; govulncheck the same minus the cap); one witness per key: renaming a moved advisory, dropping `if: always()`, dropping `continue-on-error: true`, dropping `timeout-minutes: 12`, inlining the run body | pre-fix characterisation: the `if: always()`, `continue-on-error: true` and `timeout-minutes: 12` drops each left the pin green (§3 probe) |
| F4 (fix round) — every plain-suite spelling is caught | `python3 scripts/ci/test-ci-structure-extraction.py::WorkflowHygieneTests::test_one_step_owns_the_frontend_suite` | 0 | the plain-suite matcher normalises run text (block bodies joined, backslash continuations folded) and treats `working-directory: frontend` (or `cd frontend`) plus a bare `pnpm test` as the plain suite; witnesses `test_mutation_a_working_directory_spelling_reddens_the_owner_pin` and `test_mutation_a_backslash_continued_spelling_reddens_the_owner_pin`; `\btest\b` keeps `vitest` legal | pre-fix characterisation: both spellings left the pin green (§3 probe) |
| F5 (fix round) — the membership FK is proven by exact name | `mix test test/symmetry_control/migrations/goal_0006_migration_test.exs:686` in the §3 scratch harness | 0 | a third arm inserts a task whose `work_item_id` is a goal-less work item inside the deferred-ownership transaction, so only `tasks_goal_work_item_membership_fkey` can report; the drop-membership mutant reds exactly that arm while the disjunctive arm stays green (§7); repeat mode: 51 green runs | pre-fix: dropping the membership constraint left the test green (the review's counterexample, reproduced in §7) |

## 5. Reviews of the diff

Two independent read-only reviews were dispatched against the working-tree diff (full review briefs: the
whole diff plus the contract; both instructed to make no change):

* `Wave1HygieneReview` (adversarial review; also asked for the independent verdict the contract requires
  on the `quality.md` gate-diff amendment).
* `Wave1HygieneAuditDS` (independent second audit: false-negative hunting in the new pins, mutation-test
  quality, Windows cache path resolution, evidence gaps).

**`Wave1HygieneReview`** (adversarial review, Sol high; verdict "fix first (the two medium items), then
ship" — no correctness, concurrency, cache or flake-fix defect found in the shipped code; byte-identity,
gating, ceiling and cache claims independently reproduced; **independent verdict on the `quality.md`
gate-diff amendment: accurate for the tree as changed, ship it**). Its findings and the response:

1. *(medium, confirmed)* the before-window row for run `45630186081` mixed attempts and hid its failed
   attempt → **fixed**: §6 is now attempt-aware (`attempts/<n>/jobs` per attempt), reports both attempts,
   counts the re-run, and defines `wall` as run-scoped `updated_at − created_at`. The row now shows
   attempt 1 `failure` with `coverage-control` red (the flake) and attempt 2 green after re-running that
   job; the window's measured runner-minutes rose from 47.07 to 47.79 per run as a result.
2. *(medium, confirmed)* the appended flake clause is a documented deviation presented as clause-green →
   **fixed**: the §4 flake row now marks the literal fix sentence "superseded by proof" with the schema
   citations, and §7/§9 record the fix's brittleness.
3. *(low, confirmed)* `linux-native-v2-acceptance.yml` now runs no frontend suite at all (its `contracts`
   job inherits item 4 and the workflow has no coverage job) → recorded as residual debt in §9; the
   contract de-scopes that workflow, so it is not edited here.
4. *(low, confirmed)* the new `daemon-audit` pin did not pin the job's `uses:` prerequisites (dropping
   `setup-go` would have left the pin green while the model-routing suites silently skip their Go-backed
   cases) → **fixed**: the pin now fixes `actions/checkout@v7` and `actions/setup-go@v7` with
   `go-version: 1.27.4` and `cache-dependency-path: daemon/go.sum`, plus a mutation witness.
5. *(low, confirmed)* the step-cap invariant is indentation-exact and `ci.yml`-only → recorded in §9
   (composite-action steps do not support `timeout-minutes`, so the practical gap is the raw-line read).
6. *(low, confirmed)* `legacy-route-audit`'s new 5-minute ceiling sits above three uncapped advisory
   steps → recorded in §9 as accepted residual risk (the value is contract-ratified; observed 0.40-0.52 min).
7. *(low, confirmed)* `docs/plans/2026-11-21-ci-quality-plane-plan.md` still lists the two advisory
   readings as `daemon`'s → recorded in §9; the contract's strict owned-file list excludes that plan.

**`Wave1HygieneAuditDS`** (independent second audit, Tencent DeepSeek V4.1 Flash Fast). Its findings and
the response:

* **A1** *(medium)* the frontend-owner pattern missed `pnpm --filter @symmetry/frontend run test` (and
  `-C`/`--dir` spellings) → **fixed**: `PLAIN_FRONTEND_SUITE` now matches any spelling whose line names
  the frontend package or directory and then a standalone `test` word (`\btest\b` cannot match inside
  `vitest`, so the coverage command stays legal), with a new `ci.yml`-side mutation.
* **A2** *(medium)* the step-cap invariant had no witness: the table equality fired first, so M4b
  reddened for the wrong reason → **fixed**: `check_job_timeouts` checks the cap invariant before the
  table equality, M4b now asserts the cap message, and a new mutation raises a step cap above its
  ceiling.
* **A3** *(low-medium)* the two advisory bodies and the job's `defaults` were unpinned (a body could be
  emptied while every pin stayed green) → **fixed**: both advisory bodies' defining command lines and the
  `defaults.run.working-directory` are pinned, with two new mutation witnesses.
* **A4** *(low)* job-set completeness inherits `JOB_HEADER = ^  [a-z][a-z0-9-]*:$`, so a `_`-leading job
  id would be invisible (the same regex is shared with the non-owned classifier suite) → recorded in §9.
* **A5** *(low)* cache-site detection is substring-bound, and `linux-native-v2-acceptance.yml:81-82` runs
  a sixth, uncached `MIX_ENV=prod mix deps.get` outside C5's scope → recorded in §9 (the contract lists
  that workflow out of scope).
* **A6** *(data defect)* the same impossible before-window row as the first review's finding 1 →
  **fixed** by the attempt-aware re-collection.
* **A7** the itemised handover gaps → all are §8; the auditor verified the four plant sha256s against the
  working tree itself.

Sound-by-both statements worth keeping: the relocation is genuine (byte-identical spans); no pin was
weakened or deleted; the cache keys/paths/env split are correct, including Windows path resolution for
`uses:` steps (precedent: `coverage-daemon`'s `path: daemon/coverage.out`); the flake mechanism and the
impossibility argument hold; the `quality.md` row keeps every citation token and stays citation-gate-safe.

### Fix round: ce-code-review (run `20261126-ci-wave1-31856`, verdict `MUST_FIX`)

`ce-code-review` (agent mode, depth full, six in-process reviewers plus an independent validator with
4/4 batched findings confirmed; the cross-model pass was not run under that invocation's rules) re-read
the 7 changed paths at the delivered revision plus the contract and returned five findings at P2-P3.
The conductor's 2026-11-26 ruling (contract store) accepted all five and amended the flake clause; the
dispositions, each with its witness in §3/§4/§7:

* **F1** — the record called the flake repair a "ratified deviation" while no ratification existed in
  the contract store. Fixed by the conductor's explicit ratification in the contract (which also
  amended the flake clause to "each arm pins its asserted constraint deterministically") and by this
  record's §4 flake row citing "deviation ratified by conductor ruling 2026-11-26 (contract)".
* **F2** — `check_single_frontend_suite_owner` compared only the owner step's run text, so an `if: false`
  could disable the suite while every pin stayed green. Fixed by the exact scalar map plus an `if: false`
  witness (§4 F2 row; pre-fix reproduction in §3).
* **F3** — the moved advisory steps were pinned by run-body digest only, so dropping `if: always()`,
  `continue-on-error: true` or `timeout-minutes: 12` reddened nothing. Fixed by the full scalar-map pin
  with one witness per key (§4 F3 row).
* **F4** — the plain-suite pattern missed a `working-directory: frontend` context and backslash-continued
  run bodies. Fixed by the normalising matcher plus two spelling witnesses (§4 F4 row).
* **F5** — the de-flaked assertion's disjunction (`tasks_(work_item_id|goal_work_item_membership)_fkey`)
  let the membership FK break silently. Fixed by the deterministic third arm; numbers, mutant and
  bytes in §7.

### Fix round r2: PR #292 CI red (Sol `findings/292-opus-review-1.md`, DS `findings/292-ds-audit-1.md`; conductor rulings R0-R7)

The wave PR's own run (`45672530392`, head `0283c9a8`) concluded `failure`: `control`'s step
`Enforce the committed Credo strict baseline` held 570 issues against the baseline's 564 (readability
166 vs 160), and every later `control` step was skipped. The DS audit's independent local repro agrees
(origin/main 564, head 570, the only per-file delta `goal_0006_migration_test.exs` 2 → 8); the six
added over-length lines are `697, 698, 701, 704, 710, 713`. The rulings and their dispositions:

* **R0/R1 — reflow, and record the ratchet as a gate.** The two `%{…}` maps are broken one key per
  line, the two comments rewrapped and the two-line "what" comment deleted (R5); no added line exceeds
  98 columns, so the file returns to its two pre-existing issues. The ratchet reproduces the baseline
  exactly — 564 issues over 225 files and 70 checks (160 readability, 326 refactor, 73 design,
  5 warning), exit 0 (§3). The plan now carries the ratchet as a recorded local gate for any slice
  touching `control/**`, with the Elixir gates' working directory stated.
* **R2 — no change.** The `linux-native-v2-acceptance.yml` frontend-suite debt stays disclosed in §9.
* **R3 — `legacy-route-audit` 5 → 10 minutes.** Re-derived with the contract's rule, counting the
  three advisory downloads: each is bounded by curl `--retry-max-time 120` (2 min), so the largest
  bounded step + 5 = 7, and the job's worst-case content (3 × 2 min of download budget plus its
  0.40-0.52 min of observed work ≈ 6.5 min) needs a ceiling at least that large; both reads give the
  rule's next 5-minute multiple, 10 (also ≥ 2.5 × the 0.4-min avg12 = 1 and ≥ the floor 5). The
  `JOB_TIMEOUTS` pin, C2 and the YAML-probe row are updated.
* **R4 — `control` 20 → 25 minutes.** The before-window maximum is 9.38 min, and the 40 %-of-ceiling
  rule needs ≥ 9.38 / 0.40 = 23.45, rounded up on the contract's 5-minute grid to 25 (also ≥ 2.5 ×
  the 8.0-min avg12 = 20 and ≥ the largest step cap 15 + 5 = 20). The two reportable before-window
  runs (9.38 and 9.07 min) sit at 37.5 % and 36.3 % of the raised ceiling. Pin and rows updated.
* **R5 — comment policy.** `daemon-audit`'s header is two lines; the test's two-line "what" comment is
  deleted.
* **R6 — stale reference.** The plan's flake-item heading cites `:686` (the test header) instead of
  `:697`. The historical `docs/plans/2026-11-21-ci-quality-plane-plan.md` stays unedited (§9).
* **R7 — the two residual FK alternations are order-robust.** `goal_0006_migration_test.exs:2190`
  (the `DELETE FROM goals …` arm) and `:2233` (the repository-resource `UPDATE` arm) list their full
  violated sets — three and two constraint names — so a report from any trigger-OID order still
  matches; no code change (§9; the r1 coordinates were `:2184`/`:2227`).

## 6. Measurement windows

Collection command (read-only, `wxxb789/symmetry`; a `per_page=20` scan of `event=pull_request` and
`event=push`, then per-run and per-attempt reads):

```
gh api 'repos/wxxb789/symmetry/actions/runs?per_page=20&event=pull_request'
gh api 'repos/wxxb789/symmetry/actions/runs?per_page=20&event=push'
gh api repos/wxxb789/symmetry/actions/runs/<id>            # created_at, run_started_at, updated_at, run_attempt
gh api 'repos/wxxb789/symmetry/actions/runs/<id>/attempts/<n>/jobs?per_page=100'
```

Measurement definitions (both reviews' finding on retried runs drove these): a row's **wall** is
run-scoped (`updated_at − created_at`, so a re-run's second attempt is inside it), **queue** is
`run_started_at − created_at` of the latest attempt, and **measured job-minutes** sums every attempt's
job durations, de-duplicating carried-over jobs of a partial re-run by counting only attempt-*N* jobs
whose `started_at` is at or after that attempt's `run_started_at`. Per-job columns come from the latest
attempt (job names are unique per attempt).

### Before window — last 5 completed `pull_request` runs (frozen at collection 2026-11-25T15:27:48Z)

Durations in minutes.

| run | head | attempts | wall | measured job-min | queue s | changes | record-cleanliness | citation-existence | contracts | control | coverage-control | coverage-daemon | coverage-frontend | pi-control-e2e-windows | daemon | legacy-route-audit | daemon-windows | integration | compose | browser-fixture-linux |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 45635133693 | `ac9d1736d` | 1 | 10.78 | 47.52 | 0 | 0.12 | 0.12 | 0.15 | 3.50 | 7.03 | 3.75 | 1.80 | 1.23 | 3.78 | 10.02 | 0.52 | 3.63 | 2.67 | 4.93 | 4.27 |
| 45632654810 | `acc20f7ae` | 1 | 12.50 | 46.58 | 0 | 0.10 | 0.18 | 0.18 | 2.88 | 9.38 | 2.97 | 1.72 | 1.12 | 2.90 | 8.78 | 0.45 | 4.27 | 2.80 | 4.97 | 3.88 |
| 45631571295 | `ad769b4e0` | 1 | 10.67 | 44.39 | 0 | 0.08 | 0.12 | 0.00 | 3.57 | 6.87 | 2.40 | 1.82 | 1.08 | 2.72 | 8.63 | 0.52 | 3.82 | 2.88 | 5.93 | 3.95 |
| 45630186081 | `3ad9a8194` | 2 | 14.03 | 48.19 | 668 | 0.12 | 0.12 | 0.20 | 3.43 | 6.87 | 2.80 | 1.68 | 0.85 | 2.55 | 8.60 | 0.48 | 4.37 | 2.90 | 5.95 | 3.70 |
| 45623398370 | `49a606dd3` | 1 | 12.63 | 52.26 | 0 | 0.10 | 0.13 | 0.00 | 3.30 | 9.07 | 3.60 | 1.45 | 1.12 | 3.77 | 12.45 | 0.40 | 4.65 | 3.25 | 5.22 | 3.75 |

Run `45630186081` in detail (the run the appended flake item cites, and the only retried run in the
window): attempt 1 `failure`, span 10.48 min, 45.39 job-minutes, **`coverage-control` red**
(3.57 min — its `mix test --cover` reported `tasks_context_snapshot_ownership_fkey`); attempt 2
`success`, the failed job re-run (2.80 min) while the other jobs were carried over, hence
45.39 + 2.80 = 48.19 measured job-minutes and a run-scoped wall of 14.03 min.

Window means: wall 12.12 min, measured 47.79 job-minutes, `daemon` 9.70 min, `contracts` 3.34 min,
`compose` 5.40 min, `browser-fixture-linux` 3.91 min, `coverage-frontend` 1.08 min, queue delay 134 s
(0 except the 668 s outlier).

Attempt census in the same 20-run scan: 6 attempts across the 5 runs (one re-run), 1 failed attempt
(`coverage-control`, the flake); 0 cancelled `pull_request` runs, 0 cancelled `push` runs. 20 completed
PR runs in the scan; `daemon-audit` does not exist before the change, hence its column is absent.

Per-site before-window baselines (minutes per run, oldest last):

| site | step | durations |
| --- | --- | --- |
| `control` | `Run mix deps.get` | 0.02, 0.02, 0.02, 0.02, 0.02 |
| `control` | `Run MIX_ENV=test mix compile --warnings-as-errors` | 0.40, 0.57, 0.40, 0.38, 0.55 |
| `coverage-control` | `Run mix deps.get` | 0.02, 0.03, 0.02, 0.02, 0.02 |
| `coverage-control` | `Run the Control suite with a coverage report` | 3.30, 2.48, 1.83, 2.33, 3.12 |
| `pi-control-e2e-windows` | `Run mix deps.get` | 0.05, 0.03, 0.02, 0.03, 0.05 |
| `browser-fixture-linux` | `Build the Control instance the fixture families serve from` | 0.62, 0.52, 0.48, 0.47, 0.42 |
| `contracts` (action, `MIX_ENV=prod` site) | `Run ./.github/actions/contracts-build` | 2.02, 1.67, 2.10, 1.97, 1.85 |
| `daemon` (item 3) | golangci / govulncheck / model-routing | 0.73+0.12+2.20, 0.58+0.10+1.88, 0.57+0.12+1.92, 0.63+0.12+2.07, 0.95+0.15+2.57 |
| `coverage-frontend` (item 4 owner) | `Run the frontend suite with a coverage report` | 0.67, 0.65, 0.67, 0.60, 0.82 |

### After window — first 5 completed `pull_request` runs whose head contains the wave merge

Not collected yet — conductor-owned post-merge (§8 item 4); the final-byte green run `45680179932` is
its seed row (wall 573 s / 9m33s, 0 s queue, 45.63 runner-min vs the 47.79 mean, −4.5 %; `daemon`
−161 s offset by `daemon-audit` 196 s). The comparison to apply when it is: the same attempt-aware
definitions above; wall (mean against 12.12 min), measured runner-minutes (mean against 47.79), per-job
durations as fractions of the amended ceilings (`control` 25, `legacy-route-audit` 10; report anything
above 40 %), cancelled-run count, queue delay; item 3 attribution `daemon` ≥150 s below its 9.70 min
before-window mean with `daemon-audit` ≤ `daemon`; item 4 attribution `contracts` below its 3.34 min
mean and the composite step below its 1.67-2.10 min band; item 5 `Cache restored from key: …` present
at all five sites; item 1 zero cancelled `push`-event runs. Before the wave PR opens, whichever 5 runs
immediately precede it are the window of record — if newer runs land first, re-collect with the
commands above rather than reusing this frozen set.

## 7. Flake item — probe and fix verification

### Mechanism (probe on PostgreSQL 17.11, a scratch cluster of the probe host)

A reduced but faithful copy of the `goal_0006` FK graph was built in the migration's creation order
(`work_items`, `goal_revisions`, `context_snapshots`, `tasks`; constraint names and shapes copied:
`tasks_work_item_id_fkey`, `tasks_goal_work_item_membership_fkey`, `tasks_goal_revision_fkey`,
`context_snapshots_work_item_ownership_fkey`, `tasks_goal_fields_all_or_none`,
`tasks_context_snapshot_ownership_fkey`). Trigger list for the four `tasks` FKs, in `pg_trigger` name
order (the firing and reporting order):

```
RI_ConstraintTrigger_c_16485/16486 tasks_work_item_id_fkey
RI_ConstraintTrigger_c_16490/16491 tasks_goal_work_item_membership_fkey
RI_ConstraintTrigger_c_16495/16496 tasks_goal_revision_fkey
RI_ConstraintTrigger_c_16518/16519 tasks_context_snapshot_ownership_fkey
```

Violated-constraint sets, from direct lookups (referenced row present = not violated):

| row shape | `tasks_work_item_id_fkey` | `tasks_goal_work_item_membership_fkey` | `tasks_context_snapshot_ownership_fkey` | reported |
| --- | --- | --- | --- | --- |
| random work item + random snapshot (the test's row) | violated | violated | violated | `tasks_work_item_id_fkey` |
| valid fixture work item + random snapshot | ok | ok | violated | `tasks_context_snapshot_ownership_fkey` |
| random work item + the fixture's valid snapshot | violated | violated | violated | `tasks_work_item_id_fkey` |
| valid work item + a sibling item's snapshot (same goal) | ok | ok | violated | `tasks_context_snapshot_ownership_fkey` |

Ordering flip (same row, after recreating `tasks_work_item_id_fkey` so its trigger OID is newer):

* recreated at `…528/…529` while the ownership trigger stayed `…523` → reported
  `tasks_goal_work_item_membership_fkey`;
* work-item and membership triggers both recreated after the OID counter was moved past a lexicographic
  boundary (triggers `31596/31597` and `31601/31602` vs ownership `16591/16592`) →
  **reported `tasks_context_snapshot_ownership_fkey`**, the failure observed once in CI run
  `45630186081`.

Constraint graph consequence (why "violate only the asserted FK" is not literally reachable): the
ownership FK's lookup needs a `context_snapshots` row whose `(goal_id, work_item_id)` equals the row's,
and `context_snapshots_work_item_ownership_fkey` requires that very pair to exist in `work_items` — the
same condition that satisfies `tasks_goal_work_item_membership_fkey`. A row whose work item is unknown
therefore always violates the ownership FK as well; the only single-violation shapes are the ownership
FK (rows 2 and 4 above) or the goal-revision FK.

Fix mechanics verified by the probe, then by the real harness:

* `ALTER TABLE tasks ALTER CONSTRAINT tasks_context_snapshot_ownership_fkey DEFERRABLE INITIALLY
  DEFERRED` inside the transaction makes the ownership check unreportable before the row fails on the
  immediate work-item FKs; the rolled-back transaction restores `condeferrable = false` /
  `condeferred = false` (probe) and leaves no dangling row.
* Privilege probes: `SET session_replication_role = replica` → `permission denied to set parameter`
  for a plain owner; `ALTER TABLE … DISABLE TRIGGER tasks_context_snapshot_ownership_fkey` → `trigger
  "…" for table "tasks" does not exist` (internal RI triggers are not addressable by constraint name);
  `ALTER TABLE … ALTER CONSTRAINT … DEFERRABLE INITIALLY DEFERRED` → `ALTER TABLE` for the table owner.
  The deferral is therefore the only mechanism available without superuser rights.

### Real harness (Alpine WSL guest, Elixir 1.19.6 / OTP 27, scratch PostgreSQL 17.11 cluster; the
repository's tracked files copied to a scratch tree, `POSTGRES_*` pointed at the scratch cluster)

| run | tree | command (in `control/`) | result |
| --- | --- | --- | --- |
| fixed row (as shipped) | the working tree | `mix test test/symmetry_control/migrations/goal_0006_migration_test.exs:686` | `1 test, 0 failures (57 excluded)` — both the deferred work-item assertion and the new sibling ownership assertion pass |
| pre-fix row, forced order (scratch copy: the row from `HEAD` plus one forced recreation of the two work-item FKs, printing the first violated trigger) | scratch copy of `HEAD` | same | `probe: first RI check trigger after the recreate: tasks_context_snapshot_ownership_fkey` → `1 test, 1 failure`: actual message `insert or update on table "tasks" violates foreign key constraint "tasks_context_snapshot_ownership_fkey"`, expected `~r/tasks_(work_item_id\|goal_work_item_membership)_fkey/i` |
| fixed row, forced order (same scratch instrumentation applied to the shipped fix) | scratch copy of the working tree | same | `probe: first RI check trigger after the recreate: tasks_context_snapshot_ownership_fkey` → `1 test, 0 failures (57 excluded)` |

The scratch instrumentation and the scratch trees are not part of the change; the shipped test file's
sha256 is in §1. The reviewer independently re-derived the schema facts the impossibility argument rests
on (constraint inventory at this test's migration version, the CHECK's non-null requirement, the FK
columns, the absent goal-revision FK) and confirmed the argument; §9 records the fix's brittleness and
§4 marks the contract sentence as superseded by proof.

### F5 fix-round arm, its repeat run, and the drop-membership mutant

Delivered bytes of `control/test/symmetry_control/migrations/goal_0006_migration_test.exs`:
`146c86cf0f8a477ab1ab31381e813ec60012dfd6d36929ff42a1ca82a4bdcbf7` (the fix-round r2 harness copied
exactly these bytes into its scratch tree; the fix-round r1 bytes were `fde9c590…`). Harness: Elixir
1.20.4 / OTP 29.0.6, PostgreSQL 18.6 on a run-local port — both match the CI `control` job — with
`POSTGRES_DB=symmetry_control_test` and the tree's migrations.

* The two work-item assertions now go through the helper `assert_deferred_ownership_insert_error!/2`,
  which inserts its row inside a transaction that defers `tasks_context_snapshot_ownership_fkey`.
  Three arms: the pre-existing disjunctive arm, the new membership arm, the sibling-snapshot ownership arm.
* The new arm's row takes `work_item_id` from a `work_items` row no goal owns (`goal_id` NULL), so
  `tasks_work_item_id_fkey` is satisfied and the deferred ownership FK cannot report before the insert
  fails; the only immediate FK left to report is `tasks_goal_work_item_membership_fkey`, independent of
  trigger OID order, and the assertion names it exactly (no alternation).
* Runs: `mix format --check-formatted` exit 0 (silent); `MIX_ENV=test mix compile --warnings-as-errors`
  exit 0; `mix test …:686` → `1 passed, 57 excluded`; the same command with `--repeat-until-failure 50`
  → its countdown ran 50 → 0, 51 runs, every one `1 passed, 57 excluded` (52 green runs including the
  single one). Re-run on the r2 bytes: the same commands report identical results (format and compile
  exit 0; 51 green repeat runs; §3).
* Mutant: one inserted line, immediately after the `migrate_goal_up!()` call that follows the `:686`
  test header —
  `Repo.query!("ALTER TABLE tasks DROP CONSTRAINT tasks_goal_work_item_membership_fkey")`. R1 built it
  from the r1 bytes as `<scratch>/goal_0006_mutant_drop_membership.exs`, sha256
  `00069c47c4596aa669810d17ac4f68338c84b12928da5a97e1d44171f0b7414b`; r2 rebuilt it from the r2
  bytes as `<scratch>/goal_0006_mutant_drop_membership_r2.exs`, sha256
  `a0fe0ecebab19c65aaafe72d556d61685711018602af377492b82e2c565cc9d8` (each scratched file re-hashed to
  its delivered value after the run; the r2 restore is `146c86cf…`). Result in both rounds: the
  disjunctive arm stays green — with the constraint dropped, that row's only reportable work-item
  violation is `tasks_work_item_id_fkey`, which its regex accepts — and the test reds at the new arm
  (`:709` in the r1 mutant file, `:713` in the r2 mutant file): expected
  `tasks_goal_work_item_membership_fkey`, actual `tasks_context_snapshot_ownership_fkey` (the insert
  otherwise succeeds and the deferred ownership check fires at commit). This is the review's
  counterexample, now red; the mutant is an oracle gap for CI — a schema mutation, not a suite pin.

## 8. Handover gates — observed runtime results

The implementing stage may not push, commit, open a pull request or dispatch workflows on the
repository, so these goal-contract gates were handed over with their commands. The runtime acceptance
(2026-11-25, UTC; the conductor's authority note allowed the empty probe commit, one scratch branch
and at most two dispatches) then executed items 1-3 and 5-6 against PR #292, and the observed results
are recorded in place below — every cited run attempt 1, no re-runs anywhere. Item 4 remains open,
conductor-owned post-merge.

1. **Wave PR and its own green run — done, green.** PR #292 head `40effe5f` (the empty probe commit;
   tree `f8ac8e53…` identical to `70685e63`'s, so the run validates the same bytes). Run
   `45680179932`: `success`, all 16 jobs green including `daemon-audit` (196 s) — the moved blocking
   model-routing step gates and passes in its new home. Numbers: wall 573 s (9m33s), queue delay 0 s,
   2738 job-seconds = 45.63 runner-minutes; `daemon` 421 s; `daemon-audit` 196 s ≤ `daemon`; no job
   above 40 % of its ceiling (max `integration` 30.8 %); per-job seconds: changes 8, record-cleanliness
   5, citation-existence 7, contracts 105, control 430, coverage-control 157, coverage-daemon 77,
   coverage-frontend 66, pi-control-e2e-windows 160, daemon 421, daemon-audit 196, legacy-route-audit
   31, daemon-windows 335, integration 185, compose 361, browser-fixture-linux 194 — the seed row set
   for §6's after-window. The earlier red is history: run `45672530392` (head `0283c9a8`) failed at
   `control`'s Credo step (ruling R0, §5); the green run on the final bytes is the acceptance evidence.
2. **Live concurrency check — done, cancel confirmed.** The empty probe commit `40effe5f` was pushed
   88 s after the prior push's run (`45679924982`, head `70685e63`) was created, while it was still in
   flight. `gh run list --repo wxxb789/symmetry --branch refactor/rf-ci-wave1-hygiene --event pull_request --limit 5` (the rows, verbatim): `45680179932` `success` 9m33s head `40effe5f`; `45679924982`
   `cancelled` 1m58s head `70685e63`. `45679924982` concluded `cancelled` 25 s after the probe run was
   created — 11 of its 16 jobs concluded `cancelled`, 5 had already finished `success` (`changes`,
   `citation-existence`, `record-cleanliness`, `legacy-route-audit`, `coverage-frontend`). Push-event
   audit (`createdAt > 2026-11-25T18:50:00Z`): empty — zero cancelled `push`-event runs of `ci.yml`
   (nothing was pushed to `main` or tags in the window).
3. **Plant dispatch — done; the four plants red, the one collateral explained.** Scratch branch
   `scratch/rf-ci-wave1-hygiene` = `40effe5f` + one plant commit `bcd041d9` ("scratch: plant M6 M7 M8
   M13 for the wave-1 hygiene acceptance dispatch"; 4 files, +34 lines). Both allowed dispatches were
   used: the warm-up `45680484186` (no plants, `success`) and the plants run `45682191821` (`failure`).
   The four expected reds, first failing line verbatim:
   * `daemon-audit` red on M6, step `Check auditable model-routing workflow`:
     `AssertionError: planted M6: forced model-routing suite failure` (`Ran 85 tests in 126.743s`,
     `FAILED (failures=1, skipped=1)`).
   * `daemon` red on M7 in the race step (the planted test is outside the two package-scoped ordinary
     packages, so only the race pass sees it): `WARNING: DATA RACE`, `TestPlantedM7RaceOnly` at
     `daemon/internal/execution/containment_receipt_test.go:24`,
     `FAIL github.com/wxxb789/symmetry/daemon/internal/execution`.
   * `coverage-frontend` red on M8: `FAIL  src/services/refusals.test.ts > planted M8 forced failure
     > fails deterministically`, `AssertionError: expected true to be false`, `Tests  1 failed | 633
     passed (634)`.
   * `control` red on M13 at `Run MIX_ENV=test mix compile --warnings-as-errors` under a warm cache
     restore (Credo strict ratchet green first): `warning: variable "unused_m13" is unused`,
     `Compilation failed due to warnings while using the --warnings-as-errors option`.
   Collateral: `pi-control-e2e-windows` red on the same M13 plant — its `Compile Control for Windows
   Pi E2E` step also runs `mix compile --warnings-as-errors`, so the same unused-variable warning
   reddens that job too; this is the same plant surfacing in a second compile gate, not a fifth
   planted target. Every other job concluded `success`. All five cache sites restored warm, no
   `Cache not found` line at any site — verbatim `Cache restored from key:` values:
   * `control` — `Cache restored from key: elixir-test-Linux-otp-29.0.6-elixir-1.20.4-895406ef5e6e2bb12533c3865b0a0a6f4c6696d7a5bb9d721715d30c3d088459`
   * `coverage-control` — `Cache restored from key: elixir-test-Linux-otp-29.0.6-elixir-1.20.4-895406ef5e6e2bb12533c3865b0a0a6f4c6696d7a5bb9d721715d30c3d088459`, plus `Cache hit occurred on the primary key elixir-test-Linux-otp-29.0.6-elixir-1.20.4-895406ef5e6e2bb12533c3865b0a0a6f4c6696d7a5bb9d721715d30c3d088459, not saving cache.`
   * `pi-control-e2e-windows` — `Cache restored from key: elixir-test-Windows-otp-29.0.6-elixir-1.20.4-9e9aa5a078eaee8ca3b64bde41da51bf7d3e778e3d5f17a9da8481e8c95c8d38`
   * `browser-fixture-linux` — `Cache restored from key: elixir-dev-Linux-otp-29.0.6-elixir-1.20.4-895406ef5e6e2bb12533c3865b0a0a6f4c6696d7a5bb9d721715d30c3d088459`
   * `contracts` (inside `Run ./.github/actions/contracts-build`) — `Cache restored from key: elixir-prod-Linux-otp-29.0.6-elixir-1.20.4-895406ef5e6e2bb12533c3865b0a0a6f4c6696d7a5bb9d721715d30c3d088459`
   Each site restored its own runner's exact warm key (the Windows `hashFiles` value `9e9aa5a0…`
   differs from the Linux `895406ef…`); on `control`, `mix deps.get` came off the download path
   (<1 s after the cache step).
   Deletion proof: `git push origin --delete scratch/rf-ci-wave1-hygiene` → `- [deleted] scratch/rf-ci-wave1-hygiene`; `git ls-remote origin scratch/rf-ci-wave1-hygiene` → exit 0, 0 lines / 0 bytes;
   `gh api repos/wxxb789/symmetry/git/refs/heads/scratch/rf-ci-wave1-hygiene` → 404
   `{"message":"Not Found"}`. The four pre-plant sha256s were verified on `40effe5f` before the plant
   and re-verified on the PR branch after (nothing leaked to it):
   * `scripts/ci/test-model-routing-evaluation.py` (M6 target) — `589837525d7bbca860770e48cff7ce438374a0b62fb1c0db40fd60566a1c1a7c`
   * `daemon/internal/execution/containment_receipt_test.go` (M7 target) — `caa8b530a175557f0a83d43148eab83e4f3709b78d62b32469f6a6139a9eb765`
   * `frontend/src/services/refusals.test.ts` (M8 target) — `201b88f81c74a1d351e56f971d7d1992b803fe5cd7d5fc43f2da7c0cd025e520`
   * `control/lib/symmetry_control/goals.ex` (M13 target) — `109eecd9dc91b78bc1423c1c0eef176bfdbcd4cd4c613386b659da9f0f4e4e92`
4. **After-window — still open, conductor-owned post-merge** (§6). The runtime acceptance did not
   collect the 5-run window (the wave PR had not merged). Seed row, the final-byte green run
   `45680179932`: wall 573 s (9m33s), queue 0 s, 2738 job-seconds = 45.63 runner-minutes against the
   §6 before-window mean of 47.79 (−4.5 %), with `daemon` 421 s (−161 s against its 9.70-min mean)
   offset by the new `daemon-audit` job (196 s). The saving against the before-window comes from the
   vitest de-duplication and the Elixir cache — the run adds `daemon-audit` and still lands below the
   mean. The conductor collects the first 5 completed `pull_request` runs whose head contains the wave
   merge after the merge and compares as specified in §6; this item stays open until then.
5. **Branch-protection re-read before the PR — done.** Re-read by the runtime acceptance on
   2026-11-25 before the wave PR: `gh api repos/wxxb789/symmetry/branches/main/protection` → HTTP 404,
   `{"message":"Branch not protected","documentation_url":"https://docs.github.com/rest/branches/branch-protection#get-branch-protection","status":"404"}`;
   `gh api repos/wxxb789/symmetry/rulesets` → `[]`. No required-check context exists, so no job name
   is a protected context; the changed diff adds `daemon-audit` and renames or removes no job.
6. **Advisory actionlint and the full CI matrix — done.** Run `45680179932`, job `legacy-route-audit`,
   step `Measure actionlint findings for the workflows (advisory)` (pinned actionlint 1.7.12):
   `actionlint_exit_status=0`, **`actionlint_findings=0`** — no findings at all, hence none on the
   changed `.github/workflows/ci.yml`; the plants run's same step reports the identical pair. The full
   CI matrix is green on the wave PR (run `45680179932`, 16/16 jobs, item 1), and the plant dispatch
   exercised the red paths (item 3). The two `X Process completed with exit code 1` annotations under
   `legacy-route-audit` belong to the advisory `ruff` and `shellcheck` steps (both
   `continue-on-error`), not to actionlint.
7. **`ce-work` delegation:** not used. The stage template's eligibility gate requires a `ce-work` skill
   invocation offering `mode:return-to-caller` as an explicit callable command; this runtime's skill
   discovery was not exercised for that route, so the loop ran here. No delegation record exists.

## 9. Known limits

* The appended flake item's literal sentence is superseded by proof (§4/§7): the shipped fix defers
  `tasks_context_snapshot_ownership_fkey` inside a rolled-back transaction instead of deleting that
  violation, because it cannot be deleted. The `ALTER … ALTER CONSTRAINT` hard-codes the constraint name:
  if a future slice advances this test's base schema past `20260910000000` (which drops that constraint),
  the statement fails loudly rather than silently skipping, and the fix must then be revisited.
* `JOB_HEADER` (`^  [a-z][a-z0-9-]*:$`) is shared in spirit with `scripts/ci/test-ci-changed-path-classifier.py`
  (its own copy). A hypothetical job id starting with `_` would be invisible to both suites' job-set
  equality; the classifier suite is not an owned file here, so widening the regex would leave the
  companion hole open and is left to a maintenance slice.
* `check_elixir_cache_sites` matches the literal `mix deps.get` (and step-level caps are read as
  `^        timeout-minutes: N$` lines), so a site respelled as `mix do deps.get, compile` or a cap at
  another indentation escapes the pin. Both are belt-level gaps around the ratified invariants.
* `.github/workflows/linux-native-v2-acceptance.yml` keeps its own `MIX_ENV=prod mix deps.get` with no
  cache step, and — inheriting item 4 through the shared action — it now runs **no** frontend suite at
  all. The contract lists that workflow out of scope (dispatch-only, and its native acceptance does not
  run the portal suites), so this is recorded debt, not an edited file: a dispatch acceptance run can go
  green with a failing frontend suite until a later wave gives it an owner.
* `daemon-audit` costs one extra runner's checkout/setup/job-startup per run-heavy PR; the contract
  accepted that trade, and the after-window's measured runner-minutes are the only place it shows.
* `legacy-route-audit`'s ceiling was raised to 10 minutes by ruling R3 (derivation in §5): the three
  advisory steps' downloads (`ruff`, `shellcheck`, `actionlint`) each carry a 120 s curl retry budget,
  so their 6-minute worst case plus the job's observed 0.40-0.52 min of work now fits inside the
  ceiling instead of being able to consume it. The three steps stay uncapped, but a stalled download is
  bounded by its own retry budget and no longer risks the workflow's only unconditional job.
* `docs/plans/2026-11-21-ci-quality-plane-plan.md:39-40` still names `daemon` as the home of the two
  advisory readings; the contract's strict owned-file list excludes that historical plan, so the stale
  row is left for a follow-up (no programmatic consumer reads it).
* The two residual FK-attribution alternations in the changed test file (ruling R7) —
  `goal_0006_migration_test.exs:2190`, the `DELETE FROM goals …` arm, and `:2233`, the
  repository-resource `UPDATE` arm (the r1 coordinates were `:2184`/`:2227`) — enumerate every
  constraint their statements can violate (three and two names), so any trigger-OID report order still
  matches; they are outcome-order-robust like the kept disjunctive arm (`:700`). No code change.
* The probe cluster is PostgreSQL 17.11 (Alpine guest); CI runs 18.6. The mechanism (first failing RI
  check trigger by trigger name, names encoding OIDs) is version-stable; the exact reported name is
  not, which is the defect being fixed.
* The fix-round r2 gates ran on the exact CI toolchain (Elixir 1.20.4 / OTP 29.0.6, PostgreSQL 18.6;
  §3); the earlier mechanism probe used Elixir 1.19.6 / OTP 27 against PostgreSQL 17.11. The delivered
  bytes' gates therefore carry no toolchain-version gap; the CI run remains the version-of-record proof.
* `mix deps.get` step durations in the after-window will be dominated by cache-restore and Mix startup
  time, not by the download; the per-site restore lines are the discriminating evidence (§6).
* The `daemon` after-window expectation (≥150 s lower) assumes the moved content stays off `daemon`;
  run-to-run noise in the before-window (`daemon` 8.60-12.45 min) means the comparison should use the
  window mean (9.70 min) with the per-run rows reported beside it.
* `docs/archive/CLOSED-TODO.md` and `docs/TODO.md` were not touched (contract §Acceptance 9); no
  done-item of this goal is tracked there.
