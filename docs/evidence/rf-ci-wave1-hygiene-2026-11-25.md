# Evidence — rf-ci-wave1-hygiene

Goal: CI queue/timeout hygiene, the daemon advisory relocation, one frontend test run, and the Elixir
build cache, plus the appended `goal_0006` migration-test flake item. Contract: the goal file
`goal-rf-ci-wave1-hygiene.md` in the gdd contract store (owner ruling 2026-11-25: I1, items 1-5, plus the
appended flake item). Plan of record: `docs/plans/2026-11-25-rf-ci-wave1-hygiene-plan.md`.

Revision under test: `refactor/rf-ci-wave1-hygiene` at `61399984` (drafted against `origin/main`
`30ed5ada`; the three commits between them touch no owned file). Implementation date: 2026-11-25.

Stage boundary: this record was produced by the implementing stage, which may not push, commit or open a
pull request. Everything a GitHub-side gate needs is therefore handed over in §8 with its exact command;
nothing in §8 is claimed as passed. §1-§7 are executed results from this host.

## 1. File inventory and hashes

Working tree at the time of this record (all paths in scope; `git status --porcelain -uall --no-renames`):

| path | pre-change sha256 (`HEAD`) | changed sha256 |
| --- | --- | --- |
| `.github/workflows/ci.yml` | `a7d2d138475fd6397ec970e14e5d2b1ef2f013be3c67acff6ecde4cf5623c95c` | `5997f368ce8464db54f37c1bcfe03cf27ac8235c114c9b77d2af96ab86244197` |
| `.github/actions/contracts-build/action.yml` | `2d36e663dc8b0f34eecfd44b0dc4e0758405c2797259f0d3671d3040dd505e55` | `5c49e6cea58e0b0516238378d9dcc0abd6b0382784e351d89a55f057d29f6767` |
| `docs/design/quality.md` | `f34eca086f43ae1ad4751e1e92c59b69b222202dd4e76aa50fc5e9deedf9be41` | `04adc7faa77acb7831f7638c7ca4a243375de11547f81f0ce53a18b172e58b75` |
| `scripts/ci/test-ci-structure-extraction.py` | `0f6ff3a0a459a3920cd93dcaef6f5a4b942201813f44ed4c2a2e1cabfe007bb5` | `f4b1a7e0a686340277f03feb1be2b9e9ec9f1088d79559df2669efd196890438` |
| `control/test/symmetry_control/migrations/goal_0006_migration_test.exs` | `d1392647d9cc1afa0fd65382ead4cefca229dd5f5408fd10eb50cea5dd37f0b9` | `87df1c4713ca5195441345ca30e0671d24aad28ae05de87d1488566aab06a0b8` |
| `docs/plans/2026-11-25-rf-ci-wave1-hygiene-plan.md` | new | `63b5626a25b6232cd90fdce430e53258f6322e7ee4669c6cea6f80b10373abf9` |

`ci.yml` is 2216 lines after the change (2158 before).

## 2. Censuses re-measured at `61399984`

| id | command | result |
| --- | --- | --- |
| C1 | `grep -c "continue-on-error: true" .github/workflows/ci.yml` | 8 (before and after; the two advisories move job, none is added or removed) |
| C2 | `grep -nE "^    timeout-minutes" .github/workflows/ci.yml` | before 5 (585, 637, 678, 705, 1353); after 16 — the five existing ceilings plus eleven job headers (`changes` 5, `record-cleanliness` 5, `citation-existence` 5, `contracts` 10, `control` 20, `daemon` 30, `daemon-audit` 20, `legacy-route-audit` 5, `integration` 10, `compose` 40, `browser-fixture-linux` 35) |
| C3 | `grep -nE "^  [a-z][a-z0-9-]*:$" .github/workflows/ci.yml` | before 16 (15 jobs + `on.push`); after 16 job headers inside `jobs:` + `on.push` = 17 matches (16 jobs) |
| C4 | `grep -nE "^concurrency:" .github/workflows/ci.yml` | before 0; after 1 (line 15) |
| C5 | `grep -n "mix deps.get" .github/workflows/ci.yml .github/actions/contracts-build/action.yml` | before 5 (ci.yml 195, 619, 787, 2012; action 44); after 5, each preceded by its cache step (ci.yml 206/636/810/2061, action 37) |
| C6 | `grep -nE "vitest\|@symmetry/frontend test" …` | before 4 (action 26 plain run; ci.yml 579, 689 comments, 691 coverage run); after 3 (ci.yml comments and the coverage run; the action's plain step is gone) |
| C7 | `grep -rn "actions/cache" .github` | before 0; after 5 cache steps (`ci.yml` ×4, action ×1) |
| C8 | `git ls-tree -r --name-only HEAD control/deps \| wc -l` | 0 (deps are not vendored; `.gitignore` lines 43-44 ignore `/control/_build/` and `/control/deps/`) |
| C9 | `grep -nA1 "name: Check auditable model-routing workflow" .github/workflows/ci.yml` | the step is now in `daemon-audit` with exactly its `name` and `run: \|` keys (no `continue-on-error`, no narrowing `if:`) |
| C10 | `grep -nA3 "Measure golangci-lint findings (GOOS=linux, advisory)\|Measure govulncheck findings (advisory)" .github/workflows/ci.yml` | both blocks still carry `if: always()` + `continue-on-error: true`, now inside `daemon-audit` |

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
| structure extraction | `python3 scripts/ci/test-ci-structure-extraction.py` | `Ran 62 tests`, `OK`, exit 0 (baseline at drafting: 40 tests OK; +5 pins, +17 mutations, of which 5 were added in response to the two reviews below and 1 to the simplify pass) |
| changed-path classifier | `python3 scripts/ci/test-ci-changed-path-classifier.py` | `Ran 37 tests`, `OK`, exit 0 (unmodified file) |
| citation existence (amended `quality.md`) | `python3 scripts/ci/check-citation-existence.py --base HEAD^1 --head HEAD` in a scratch repository holding the head tree plus the amended document | exit 0 — `1 changed document(s) pass the path and symbol check (19 path citation(s))` |
| record cleanliness (this new record) | `python3 scripts/ci/check-record-cleanliness.py --base HEAD^1 --head HEAD` in the same scratch repository | exit 0 — `1 changed record(s) pass the root-only check` |
| YAML shape probe (not a repo gate; independent of the structure suite, which parses text) | `python3` + `yaml.safe_load` over both changed YAML files | parses; 16 jobs; all 16 job timeouts equal the contract's §2 table and the job sets match both ways; `concurrency` block equals the pinned three lines and cancels pull requests only; exactly one step runs `vitest` (`coverage-frontend`, pinned command); five `actions/cache@v6` steps, each with both paths and one key carrying `${{ runner.os }}`+`otp-29.0.6`+`elixir-1.20.4`+`${{ hashFiles('control/mix.lock') }}` and no `restore-keys`, each immediately preceding its `mix deps.get` step; `daemon-audit` steps exactly checkout, setup-go, golangci, govulncheck, model-routing |
| helper suites also run locally | `python3 scripts/ci/test-citest.py`, `test-go-test-durations.py`, `test-assert-go-coverage-floor.py`, `test-check-lockfile-registry.py`, `test-assert-folded-gates.py`, `test-assert-exunit-json.py`, `test-go-test-json-gate.py`, `test-assert-playwright-json.py`, `test-assert-runner-matches-target.py`, `assert-browser-suite-coverage.py` | all exit 0 (`OK`; `test-citest.py` and `test-go-test-json-gate.py` skip host-dependent cases) |
| `test-no-legacy-routes-audit.py` | `python3 scripts/ci/test-no-legacy-routes-audit.py` | 11 failures, **pre-existing on this host**: the same 11 failures reproduce on an unmodified copy of the head tree (probe scratch repository on the local disk), all with `/bin/bash: <scratch>/scripts/audit-no-legacy-routes.sh: No such file or directory` (exit 127) — an environment boundary of this host's bash, not a defect of the change. CI runs this suite in `legacy-route-audit`. |

## 4. Clause-by-clause evidence rows

| clause | green command (repository root) | exit | evidence row | expected failure / baseline observed first |
| --- | --- | --- | --- | --- |
| §1 concurrency pinned, PR-only | `python3 scripts/ci/test-ci-structure-extraction.py` (`WorkflowHygieneTests::test_only_superseded_pull_request_runs_are_cancelled`) | 0 | §3; C4 = 1; block sits between `permissions:` and `jobs:` | mutation tests `test_mutation_cancelling_every_event_reddens_the_concurrency_pin` and `test_mutation_a_constant_concurrency_group_reddens_the_concurrency_pin` redden the same pin before the fix exists (M1, M2) |
| §1 live cancel behaviour | *not run in this stage* (§8) | — | — | the pre-change tree has no `concurrency:` key (C4 = 0), so a second push to the same PR queues a full superseded run |
| §2 every job has its ceiling | `python3 scripts/ci/test-ci-structure-extraction.py::WorkflowHygieneTests::test_every_job_carries_its_pinned_ceiling_and_no_step_cap_meets_it` | 0 | §3; C2 = 16; YAML probe prints the table | mutations `test_mutation_deleting_the_daemon_ceiling_…` (M3), `test_mutation_raising_a_ceiling_…` (M4: 360), `test_mutation_a_ceiling_below_its_step_cap_…` (M4b: compose 15, now asserted on the cap message) and `test_mutation_a_step_cap_above_its_job_ceiling_…` (a 45-min step under a 40-min ceiling, the witness the first review's finding asked for) each redden the pin |
| §3 relocation, blocking step, no leftover | `python3 scripts/ci/test-ci-structure-extraction.py::…::test_the_daemon_audit_job_holds_the_moved_steps_and_the_blocking_model_routing_step` | 0 | §3; byte-identity check: the 72 removed lines of `daemon` (golangci 955-997 with its two comment lines, govulncheck 998-1018, model-routing 1061-1068 at `HEAD`) equal the 72 lines now in `daemon-audit` (`old == moved` → `True`; newline-joined span sha256 `589d57dcc0c92dae3646b8b50ea6d2cf94b723fe4e3ce178fc5c95ec95eb1eb1`); the reviewer's independent span digest is `5e8da0a309eac08db02690f068e0a558baff7fd4e25fc27749c25c595d655ac0`; C9/C10. The pin also now fixes the job's `uses:` prerequisites (`actions/checkout@v7`, `actions/setup-go@v7` with `go-version: 1.27.4` and `cache-dependency-path: daemon/go.sum`), its `defaults.run.working-directory: daemon`, and every defining command line of the two advisory bodies (version/digest/archive literals and the `GOOS=linux … run --config .golangci.yml` invocation for golangci-lint; `govulncheck_version='v1.10.0'` and the `./...` invocation for govulncheck) | mutations `test_mutation_marking_the_model_routing_step_advisory_…` (M5), `test_mutation_replacing_a_model_routing_invocation_with_exit_zero_…`, `test_mutation_emptying_a_moved_advisory_body_…` (golangci invocation → `exit 0`; the false-negative the second audit found), `test_mutation_pointing_the_moved_jobs_defaults_at_the_root_…` and `test_mutation_dropping_the_moved_jobs_setup_go_…` redden the pin; `daemon` no longer names any of the three steps |
| §3 daemon-audit green / M6 / M7 | *not run in this stage* (§8) | — | — | the moved commands' pre-change green runs are the before-window rows (§6); `daemon` carried the same steps and passed (12-run evidence in the contract) |
| §4 one vitest step | `python3 scripts/ci/test-ci-structure-extraction.py::…::test_one_step_owns_the_frontend_suite` | 0 | §3; C6: the action lost its plain test step (`ACTION_STEPS` pin), exactly one step in both files mentions `vitest` (ci.yml's coverage step) | mutations `test_mutation_restoring_the_plain_frontend_suite_reddens_the_owner_pin` (M9) reddens both the owner pin and `check_action`, and `test_mutation_re_adding_the_plain_frontend_suite_to_a_job_reddens_the_owner_pin` re-adds the suite inside a `ci.yml` job in the `run test` spelling (the second audit's counterexample to the first pattern); thresholds untouched (the classifier suite's `VITEST_COVERAGE_FLOORS` pin still passes, 37 tests OK) |
| §4 M8 / thresholds | *not run in this stage* (§8) | — | — | before-window: the plain run executes inside `Run ./.github/actions/contracts-build` (1.67-2.10 min) and `coverage-frontend`'s suite step passes (0.60-0.82 min) |
| §5 five cache sites | `python3 scripts/ci/test-ci-structure-extraction.py::…::test_every_mix_deps_get_site_is_preceded_by_its_elixir_cache_step` | 0 | §3; C5 = 5 sites, C7 = 5 cache steps; each key carries `${{ runner.os }}`, `otp-29.0.6-elixir-1.20.4` and `${{ hashFiles('control/mix.lock') }}`, no `restore-keys`; env split test/dev/prod; the action's step keeps `if: inputs.build-control-release == 'true'` | mutations `test_mutation_deleting_a_cache_step_…` (M11), `test_mutation_stripping_the_lock_hash_from_a_cache_key_…` (M12) and `test_mutation_a_sixth_mix_deps_get_without_a_cache_step_…` each redden the pin; `actions/cache@v6` resolves (`git ls-remote` → `refs/tags/v6` = `55cc8345863c7cc4c66a329aec7e433d2d1c52a9`) |
| §5 restore lines / M13 | *not run in this stage* (§8) | — | — | before-window per-site baselines are in §6 |
| §6 before-window | `gh api` collection (§6 command) | 0 | §6 tables | — |
| §6 after-window | *not run in this stage* (§8) | — | — | — |
| flake item | ExUnit: `mix test test/symmetry_control/migrations/goal_0006_migration_test.exs:686` | 0 | §7: fixed row `1 test, 0 failures`; the same harness with the pre-fix row under the forced trigger order: `1 test, 1 failure` with `tasks_context_snapshot_ownership_fkey` | pre-fix red reproduced first (§7). **The contract's literal fix sentence is superseded by proof** (ratified deviation, see §7): "make the row violate only the asserted FK" is unsatisfiable at the schema this test builds — `AddPlanningTaskIdentityGuards` (`20260910000000`, which drops the 4-column ownership FK) is not in `MigrationModules.base_migrations()` (ends at `20260907000000`, `VersionRequestHashes`, `control/test/support/migration_modules.ex:65-87`), `migrate_goal_up!/0` runs only `AddGoal0006ControlPlane`, `tasks_goal_fields_all_or_none` requires `context_snapshot_id IS NOT NULL` (`20260909000000_add_goal_0006_control_plane.exs:1237-1245`) and `context_snapshots_work_item_ownership_fkey` (same file, `:425-430`) forces the referenced pair into `work_items`, which is exactly what the asserted work-item FK needs absent |
| branch protection | `gh api repos/wxxb789/symmetry/branches/main/protection` and `.../rulesets` | 404 "Branch not protected"; `[]` | no required-check contexts exist, so no job name is a protected context and nothing is renamed | re-read before the wave PR (§8) |
| citation gate for the amended document | see §3 | 0 | 19 path citations pass | — |
| reviewers | 2 read-only dispatches (§5) | — | §5 | — |

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

Not collected in this stage (§8). The comparison to apply when it is: the same
attempt-aware definitions above; wall (mean against 12.12 min), measured runner-minutes (mean against
47.79), per-job durations as fractions of the §2 ceilings (report anything above 40 %), cancelled-run
count, queue delay; item 3 attribution `daemon` ≥150 s below its 9.70 min before-window mean with
`daemon-audit` ≤ `daemon`; item 4 attribution `contracts` below its 3.34 min mean and the composite step
below its 1.67-2.10 min band; item 5 `Cache restored from key: …` present at all five sites; item 1 zero
cancelled `push`-event runs. Before the wave PR opens, whichever 5 runs immediately precede it are the
window of record — if newer runs land first, re-collect with the commands above rather than reusing this
frozen set.

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

## 8. Not run in this stage (handover)

The implementing stage may not push, commit, open a pull request or dispatch workflows on the
repository. These goal-contract gates therefore remain open; each is handed over with its command.

1. **Wave PR and its own green run.** Push `refactor/rf-ci-wave1-hygiene`, open a normal PR, and require
   the run on the final bytes to be green with 16 jobs and `daemon-audit` green (proves the moved
   commands run in the new home). Read the advisory actionlint log for new findings on the changed files
   (the diff must add none).
2. **Live concurrency check.** Two rapid pushes to the wave PR: the first run must conclude `cancelled`,
   the second green; then verify zero cancelled `push`-event runs of `ci.yml` in the after-window.
3. **Plant dispatch (budget: 1).** Push `scratch/rf-ci-wave1-hygiene` = wave branch + one plant commit
   carrying M6, M7, M8, M13, then `gh workflow run ci.yml --repo wxxb789/symmetry --ref
   scratch/rf-ci-wave1-hygiene`. Expected: `daemon-audit` red (M6), `daemon` red (M7, race step),
   `coverage-frontend` red (M8), `control` red under the warm cache (M13). Delete the branch afterwards
   and prove it with an empty `git ls-remote origin scratch/rf-ci-wave1-hygiene`.
   Pre-plant sha256s for restoration (the plant must be removed or the branch deleted; these are the
   bytes a restore has to reproduce):
   * `scripts/ci/test-model-routing-evaluation.py` (M6 target) — `589837525d7bbca860770e48cff7ce438374a0b62fb1c0db40fd60566a1c1a7c`
   * `daemon/internal/execution/containment_receipt_test.go` (M7 target) — `caa8b530a175557f0a83d43148eab83e4f3709b78d62b32469f6a6139a9eb765`
   * `frontend/src/services/refusals.test.ts` (M8 target) — `201b88f81c74a1d351e56f971d7d1992b803fe5cd7d5fc43f2da7c0cd025e520`
   * `control/lib/symmetry_control/goals.ex` (M13 target) — `109eecd9dc91b78bc1423c1c0eef176bfdbcd4cd4c613386b659da9f0f4e4e92`
4. **After-window** (§6): collect the first 5 completed `pull_request` runs whose head contains the wave
   merge with the commands in §6 and compare as specified there.
5. **Branch-protection re-read before the PR.** Authenticated
   `gh api repos/wxxb789/symmetry/branches/main/protection` and `.../rulesets`. Read on 2026-11-25
   during implementation: `{"message":"Branch not protected", … "status":404}` and `[]`, so no
   required-check context exists and no job name is protected; re-reading before the PR is still
   required, and if a context appears, no job may be renamed (the changed diff adds one job,
   `daemon-audit`, without removing any).
6. **Advisory actionlint and the full CI matrix** on the wave PR (the local gates cover the two suites
   the contract names; the matrix's other suites are CI-side).
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
* `legacy-route-audit`'s new 5-minute ceiling sits above three uncapped advisory steps (ruff, shellcheck,
  actionlint downloads): a stalled advisory download can now fail the workflow's only unconditional job.
  Observed 0.40-0.52 min; the value is contract-ratified (Q-D), so it is recorded rather than changed.
* `docs/plans/2026-11-21-ci-quality-plane-plan.md:39-40` still names `daemon` as the home of the two
  advisory readings; the contract's strict owned-file list excludes that historical plan, so the stale
  row is left for a follow-up (no programmatic consumer reads it).
* The probe cluster is PostgreSQL 17.11 (Alpine guest); CI runs 18.6. The mechanism (first failing RI
  check trigger by trigger name, names encoding OIDs) is version-stable; the exact reported name is
  not, which is the defect being fixed.
* The WSL Elixir toolchain is 1.19.6/OTP 27 against CI's 1.20.4/OTP 29.0.6; `control/mix.exs` requires
  `elixir: "~> 1.17"`, and the single migrated test is schema-level, so toolchain-version risk for the
  fix is low, but the CI run is the version-of-record proof.
* `mix deps.get` step durations in the after-window will be dominated by cache-restore and Mix startup
  time, not by the download; the per-site restore lines are the discriminating evidence (§6).
* The `daemon` after-window expectation (≥150 s lower) assumes the moved content stays off `daemon`;
  run-to-run noise in the before-window (`daemon` 8.60-12.45 min) means the comparison should use the
  window mean (9.70 min) with the per-run rows reported beside it.
* `docs/archive/CLOSED-TODO.md` and `docs/TODO.md` were not touched (contract §Acceptance 9); no
  done-item of this goal is tracked there.
