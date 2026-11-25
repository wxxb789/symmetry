# Plan — rf-ci-wave1-hygiene (CI queue/timeout hygiene, daemon audit relocation, one frontend suite run, Elixir build cache, goal_0006 flake)

Contract: the goal file `goal-rf-ci-wave1-hygiene.md` in the gdd contract store (owner ruling
2026-11-25: I1, items 1-5, plus the appended `goal_0006_migration_test.exs` flake item). Drafted
against `origin/main` `30ed5ada`; work happens at `61399984` (the three commits since touched no owned
file — the censuses below were re-run here and match the contract's appendix).

Status of the accepted plan: executed as written. This file records only the choices the contract left
open, the one place where the contract's literal fix instruction is not satisfiable (the appended flake
item) and what was done instead, and the evidence plan.

## Censuses re-measured at `61399984` (contract appendix → local)

| id | command | hits |
| --- | --- | --- |
| C1 | `grep -c "continue-on-error: true" .github/workflows/ci.yml` | 8 |
| C2 | `grep -nE "^    timeout-minutes" .github/workflows/ci.yml` | 5 (585, 637, 678, 705, 1353) |
| C3 | `grep -nE "^  [a-z][a-z0-9-]*:$" .github/workflows/ci.yml` | 16 (15 jobs + `on.push`) |
| C4 | `grep -nE "^concurrency:" .github/workflows/ci.yml` | 0 |
| C5 | `grep -n "mix deps.get" .github/workflows/ci.yml .github/actions/contracts-build/action.yml` | 5 (195, 619, 787, 2012; action 44) |
| C6 | `grep -nE "vitest\|@symmetry/frontend test" …` | 4 (action 26 plain run; ci.yml 579, 689 comments, 691 coverage run) |
| C7 | `grep -rn "actions/cache" .github` | 0 |
| C8 | `git ls-tree -r --name-only HEAD control/deps \| wc -l` | 0 (deps not vendored) |
| C9 | `grep -nA1 "name: Check auditable model-routing workflow" .github/workflows/ci.yml` | 1061 name, 1062 `run: \|`, no `continue-on-error` |
| C10 | `grep -nA3 "Measure golangci-lint findings (GOOS=linux, advisory)\|Measure govulncheck findings (advisory)" …` | both blocks `if: always()` + `continue-on-error: true` |

## Decisions the contract left open

1. **`daemon-audit` step list.** `actions/checkout@v7`, `actions/setup-go@v7` (same
   `cache-dependency-path: daemon/go.sum`), then the three moved step blocks in their `daemon` order
   (golangci-lint, govulncheck, model-routing), with `defaults.run.working-directory: daemon` so the
   `../scripts/ci/...` invocations and `go` commands resolve exactly as they did in `daemon`. The two
   comment lines above the golangci step (`# Build tags change …`) move with it; `daemon` keeps every
   other step byte-identical. Job inserted between `daemon`'s last step and `legacy-route-audit`.
2. **Job-timeout placement.** `    timeout-minutes: N` immediately after `runs-on:`, matching the five
   existing ceilings (`coverage-control`, `coverage-daemon`, `coverage-frontend`,
   `pi-control-e2e-windows`, `daemon-windows`), none of which is touched. The PR #292 CI-red rulings
   R3/R4 revised two of this goal's new ceilings — `control` 25 and `legacy-route-audit` 10, derived
   in the evidence record — and lowered no pre-existing ceiling.
3. **Cache step shape.** Unnamed `- uses: actions/cache@v6` (the current `actions/cache` major;
   `refs/tags/v6` = `55cc8345863c7cc4c66a329aec7e433d2d1c52a9` resolved with `git ls-remote` at
   implementation time), `with.path` a two-line block (`control/deps`, `control/_build`),
   `with.key` one line, no `restore-keys`. Keys:
   `elixir-test-${{ runner.os }}-otp-29.0.6-elixir-1.20.4-${{ hashFiles('control/mix.lock') }}` at the
   three test-env sites (`control`, `coverage-control`, `pi-control-e2e-windows`), `elixir-dev-…` at
   `browser-fixture-linux` (its `mix compile` runs in the default dev env), `elixir-prod-…` in the
   `contracts-build` action with `if: inputs.build-control-release == 'true'` (the action's production
   release step runs `MIX_ENV=prod`).
4. **`quality.md` TS row wording.** Keeps every existing citation token and adds only: typecheck/lint/
   build in the `contracts` job's `contracts-build` action; the frontend suite runs once, with coverage,
   in `coverage-frontend` (`pnpm --filter @symmetry/frontend exec vitest run --coverage`, thresholds in
   `frontend/vitest.config.ts`). No backticked symbol is written adjacent to a backticked path, so the
   citation-existence gate's symbol-binding rule has nothing new to resolve; the row's tokens are
   otherwise unchanged. `docs/design/frontend.md` carries no CI-ownership claim (checked), so no other
   document goes stale.
5. **New structure pins** (`scripts/ci/test-ci-structure-extraction.py`): `check_workflow_concurrency`,
   `check_job_timeouts` (`JOB_TIMEOUTS` table, two-way completeness against the workflow's job set, the
   step-cap invariant), `check_daemon_audit_relocation` (the contract's pin name
   `check_daemon_audit_model_routing_step` shipped inside it, together with the `DAEMON_AUDIT_STEPS`
   step-name pin, the moved-step absence, `defaults`, the advisory bodies' sha256 pins and the `uses:`
   prerequisites), `check_single_frontend_suite_owner`, `check_elixir_cache_sites`. Mutation rows M1,
   M2, M3, M4, M4b, M5, M9, M11, M12, plus M6/M7/M8/M13's CI-side plants, are validated by the suite's
   own mutation tests here and by `legacy-route-audit` in CI. `ACTION_STEPS`/`ACTION_CONDITIONS` are
   updated for the deleted test step and the new cache step; `ADVISORY_STEPS` moves the two rows to
   `daemon-audit`.

## Appended flake item (`control/test/symmetry_control/migrations/goal_0006_migration_test.exs:686`)

The instruction "make the row violate only the asserted FK" is **not satisfiable literally** under the
`goal_0006` schema this test migrates to. Proved by probe on PostgreSQL 17.11 (reduced but faithful
constraint graph, same constraint names and creation order; full transcript in the evidence record):

* The row's `work_item_id` is random, so `tasks_work_item_id_fkey` (`(work_item_id) → work_items(id)`)
  and `tasks_goal_work_item_membership_fkey` (`(work_item_id, goal_id) → work_items(id, goal_id)`) are
  both violated — a `work_item_id` that is absent from `work_items` cannot satisfy the composite pair.
  Both are named by the asserted regex.
* The row's `context_snapshot_id` is random, so `tasks_context_snapshot_ownership_fkey`
  (`(context_snapshot_id, goal_id, goal_revision, work_item_id) → context_snapshots(id, goal_id,
  goal_revision, work_item_id)`) is violated too — and it is **not** named by the regex.
* That third violation cannot be removed while the work-item violation stands: satisfying the ownership
  FK's lookup requires a `context_snapshots` row whose `(goal_id, work_item_id)` equals the task's, and
  `context_snapshots_work_item_ownership_fkey` requires that very pair to exist in `work_items` — which
  is exactly the condition that satisfies the membership FK. (Probe rows: valid item + random snapshot →
  ownership FK alone; random item + valid snapshot → all three.)
* PostgreSQL reports the first failing RI check trigger, and those triggers are named
  `RI_ConstraintTrigger_c_<oid>` and fire in name order. Probe: the same row reported
  `tasks_work_item_id_fkey`, then `tasks_goal_work_item_membership_fkey` after that constraint was
  recreated with a fresh OID — the mechanism behind the observed `tasks_context_snapshot_ownership_fkey`
  report on run 45630186081.

Fix applied (transaction-local, no schema or assertion change, regex untouched):

* Wrap the offending insert in `Repo.transaction/1` and, inside it, `ALTER TABLE tasks ALTER CONSTRAINT
  tasks_context_snapshot_ownership_fkey DEFERRABLE INITIALLY DEFERRED`. The deferred check cannot report
  before the statement fails, so the row reports only whichever of the two regex-named FKs fires first;
  the rollback removes both the planted deferral and the row. Probe: the reported constraint is a
  regex-named one under the recreated (flipped) trigger order, and `condeferrable`/`condeferred` are back
  to `false`/`false` afterwards. Alternatives measured and rejected: `SET session_replication_role`
  (superuser-only: `permission denied to set parameter "session_replication_role"` on a plain owner) and
  `ALTER TABLE … DISABLE TRIGGER <constraint>` (internal RI triggers are not addressable by constraint
  name: `trigger "…" for table "tasks" does not exist`). The deferrable ALTER succeeds for the table
  owner, which every test schema already is.
* Add the ownership FK its own deterministic, single-violation assertion in the same test (valid fixture
  work item + a context snapshot of a sibling work item in the same goal): that row violates only
  `tasks_context_snapshot_ownership_fkey`. This keeps the enforcement the deferred arm would otherwise
  stop proving, with no widening of the existing regex.

## Evidence plan (contract §Gates, §Acceptance 6-7)

Run here: both local suites (structure extraction, changed-path classifier) plus each mutation row by
direct invocation; the flake probe transcript; the branch-protection read (done: 404 "Branch not
protected", `rulesets` = `[]`); the citation gate over the amended `quality.md` in a scratch clone.
Added by the PR #292 CI-red conductor rulings (R1): the Credo strict ratchet is a recorded local gate
for any slice touching `control/**`, run from `control/` with its deps fetched —
`cd control && mix credo --strict 2>&1 | tee <log> && python3 ../scripts/ci/assert-credo-strict-baseline.py --log <log>`
(the ratchet is exact: the per-category counts must equal `scripts/ci/credo-strict-baseline.json`,
never merely stay below it). Every Elixir gate states its working directory: `mix format --check-formatted`
and the targeted `mix test` also run from `control/`, never the repository root, where `mix format`
has no `.formatter.exs` inputs and exits 0 vacuously.
Also run here, and added after the reviews landed: the record-cleanliness gate over the new record, and
two independent read-only reviews (an adversarial Sol review and a second audit) whose findings forced
five more mutation witnesses and three pin-strengthening changes — the evidence record's §5 lists every
finding and its disposition. The measurement windows are attempt-aware (a retried PR run counts both
attempts' job minutes and is reported per attempt).

Executed by the runtime acceptance of 2026-11-25 (UTC), because the implementing stage may not push,
commit or open a PR: the wave PR's own green run on the final bytes (PR #292, run `45680179932`, 16
jobs, `daemon-audit` green), the two-rapid-push cancel check (`45679924982` `cancelled` then
`45680179932` `success`), the scratch dispatch carrying the M6/M7/M8/M13 plants (a no-plant warm-up
used the first of the two allowed dispatches, `45680484186`; the plants run `45682191821` concluded
`failure` with the four expected reds plus the M13 collateral in `pi-control-e2e-windows`), and the
scratch-branch deletion proof. Still open: the after-window of 5 PR runs — conductor-owned post-merge,
seeded by `45680179932` (45.63 runner-min vs the 47.79 before-window mean, −4.5 %). The evidence
record §8 records each of these results in place; the before-window (last 5 completed PR runs) is
collected in the evidence record from the GitHub API.

## Out of scope (with reason)

* `docs/TODO.md` / `docs/archive/CLOSED-TODO.md`: contract §Acceptance 9 lists them out of scope, and no
  done-item belonging to this goal is tracked there.
* Race-pass sharding, the control-chain/render split, the `daemon-windows` advisory lint, the
  coverage-control/daemon duplicate suite, classifier path filters, `linux-native-v2-acceptance.yml`:
  contract §Acceptance 9.
* `frontend/vitest.config.ts`, `scripts/ci/test-ci-changed-path-classifier.py`, `daemon/**`, `control/**`
  (except the appended flake item's test row): contract §Owned files, verified no update needed.
* Reducing the pre-existing `compose` 35-minute rootless-witness step cap or `browser-fixture-linux`'s
  30-minute acceptance caps: the contract forbids lowering existing ceilings and sets the job timeouts
  from those caps.
