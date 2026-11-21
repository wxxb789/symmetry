# Plan: ExUnit async conversion + migration scaffolding fold (TC-E) — rf-w3-14

Contract: `goal-rf-w3-14-exunit-async.md`. Re-derived at dispatch head `d0761739` (`W3-03` / `#192`,
which carries `#149`; `origin/main` == `d0761739`, working tree clean at the start).

Re-derivation result: `control/test` holds **70 `*_test.exs`** files; every one declares `async:`
explicitly — **56 `async: false` / 14 `async: true`** by each file's declaration (matches the
contract's re-derivation at `c87c9918`; the counts were re-run at `d0761739` and are unchanged).
`test_helper.exs` is two lines (`ExUnit.start()` + `Sandbox.mode(Repo, :manual)`), no `max_cases`
anywhere in the tree, and `config/test.exs` sets `pool: Ecto.Adapters.SQL.Sandbox`,
`pool_size: System.schedulers_online() * 2`. The run prints its effective ceiling itself:
`Running ExUnit with seed: 0, max_cases: 32` on this 16-scheduler host (ExUnit's default is
`System.schedulers_online() * 2`; `max_cases` bounds concurrent **modules**, `ExUnit.Runner` line
:105 compares it against `map_size(running)` of spawned modules).

## Deliverable

(1) the DB-only subset of the 56 sync files converted to `async: true` with the per-file
classification recorded (converted and excluded-with-reason); (2) `MigrationCase` owning the two
byte-identical migration-scaffolding helpers, with the four private copies deleted; (3) the pinned
`max_cases` in `test_helper.exs` and the `pool_size` it is coupled to in `config/test.exs`.

### The classification rule, applied

> DB-only = every case touches the database solely through `DataCase`/`Repo` and mutates no
> application env (`Application.put_env`/`compile_env`/`TestEnv.put!`/`System.put_env`), no named
> global registry, no shared on-disk path, no `Endpoint` server, no ExUnit global state, and no
> writes outside its own sandbox transaction.

Two ExUnit properties make the rule the right boundary, both read from the toolchain's runner:

- **`ExUnit.Runner` :105-132** — async modules are spawned up to `max_cases`, and sync modules are
  run **only after every async module has finished** (`# Wait for all async modules`). A sync module
  therefore never overlaps an async one, before or after this slice. That is why an app-env mutator
  is harmless today and unsafe the moment it is declared async, and it is why a file whose cases
  depend on "nothing else is running" (global PubSub topic subscribers, global `:telemetry` event
  names) must stay sync: converting it *creates* the overlap.
- The per-test sandbox owner mode already keys off the tag (`DataCase.setup_sandbox/1` →
  `sandbox_options(tags)` = `[shared: not tags[:async]]`), and every case template used by the
  converted files (`DataCase`, `ConnCase`, `OrchestrationCase`) routes through it. Conversion is the
  declaration alone; no support-module behaviour changes.

### Converted (11 files, `async: false` → `async: true`)

| file (all under `control/test/`) | template | what the rule sees |
| --- | --- | --- |
| `symmetry_control/native_harness_kind_binding_test.exs` | `DataCase` | `Repo` inserts + one keyed `Repo.query!` against `pg_get_constraintdef` for a named constraint (`:214-220`); no globals. Migration suites that also define that constraint name create it in a private schema and run sync (later phase), so the catalog read cannot race them. |
| `symmetry_control/orchestration/capability_scheduling_test.exs` | `OrchestrationCase` | `Integrations`/`Workspaces`/`Orchestration` calls in-process through `Repo`; unique project keys per fixture. |
| `symmetry_control/orchestration_retry_test.exs` | `OrchestrationCase` | same shape (submit/assign/claim/transition through `Orchestration`). |
| `symmetry_control/workspaces_read_model_test.exs` | `OrchestrationCase` | same shape, plus a `:telemetry` handler whose **id is keyed per test** (`{__MODULE__, :snapshot_read, project.id}`, `:605-615`) and whose body forwards only reads matching this test's own project id (`TelemetryReads.read?(metadata.params, forms)`), so concurrent suites' query events are filtered out. |
| `symmetry_control/goals/contract_fixture_manifest_test.exs` | `ExUnit.Case` | no database at all: reads the committed, immutable `contracts/fixtures/manifest.json`, `contracts/v1`, `daemon/internal/contracts/schema_bundle.json` (`:7-10`). Read-only, no globals. |
| `symmetry_control/goals/contract_validation_test.exs` | `ExUnit.Case` | no database at all: reads the same committed fixtures; its one write path is a **per-test unique** temp root (`System.tmp_dir!() <> "symmetry-contracts-cache-#{System.unique_integer([:positive])}"`, `:360-372`, removed `on_exit`). |
| `symmetry_control_web/controllers/chat_controller_test.exs` | `ConnCase` | `ConnTest` dispatch in-process (endpoint `server: false`), `Workspaces`/`Chat` through `Repo`; every assertion is scoped by this test's fresh project id. |
| `symmetry_control_web/controllers/health_controller_test.exs` | `ConnCase` | one read-only `GET /healthz`. |
| `symmetry_control_web/controllers/operator_read_controller_test.exs` | `ConnCase` | operator read routes in-process + `Repo` fixtures; no aggregates, no globals. |
| `symmetry_control_web/controllers/protocol_controller_test.exs` | `ConnCase` | protocol routes in-process + `Repo`; `Scheduler.drain()` (`:1314`) is an in-process function call, not a started process. |
| `symmetry_control_web/controllers/task_goal_guard_controller_test.exs` | `ConnCase` | guard routes in-process + `Repo`; shares `TestFixtures` builders only. |

The unique keys each converted file's fixtures pin were checked against the 14 pre-existing async
modules (the set they now share a phase with): `projects.key` literals `SYM`/`CAP`/`API`/`REFUSE`/
`CONTRACT` and the connection name `Scoped GitHub` each occur in exactly one converted file and in no
async file; machine names and `(machine_id, runtime_key, …)` keys are machine-scoped with a fresh
machine per test, and every enrollment/claim/daemon-instance key is a fresh UUID or
`System.unique_integer`-suffixed. A literal-scan of all 657 string literals in the converted files
against the async set shows no shared value in a unique-index position.

### Excluded (45 files, with the one-line reason and call site)

`Application.put_env`/`compile_env`/`System.put_env` mutators — D9 keeps app-env files `async: false`
(a concurrent mutation is visible VM-wide to whichever module runs beside it):

| file | call site |
| --- | --- |
| `mix/tasks/symmetry_check_test_db_test.exs` | `:119`, `:125` `Application.put_env(:symmetry_control, SymmetryControl.Repo, …)` |
| `symmetry_control/config/runtime_test.exs` | `:32`, `:38-39`, `:70` `System.put_env/2` |
| `symmetry_control/goals/dispatch_notification_test.exs` | `:32`, `:38`, `:48-49` (and `Sandbox.mode(Repo, :auto)` `:30`) |
| `symmetry_control/goals/workers_test.exs` | `:12`, `:14` `Application.put_env(:symmetry_control, :goals, …)` |
| `symmetry_control/goals_test.exs` | `:34`, `:40`, `:3639` `Application.put_env(:symmetry_control, :goals, …)` |
| `symmetry_control/integrations/auth_test.exs` | `:8`, `:14`, `:16` `:integration_command_runner` |
| `symmetry_control/integrations/azure_devops_test.exs` | `:10`, `:12`, `:22` `:integration_http_client` (+ `:integration_providers`) |
| `symmetry_control/integrations/github_test.exs` | `:10`, `:12`, `:22` same pair |
| `symmetry_control/integrations/syncer_test.exs` | `TestEnv.put!(:integration_providers, …)` `:12` |
| `symmetry_control/integrations_test.exs` | `:27`, `:34`, `:36` `:integration_providers` |
| `symmetry_control/orchestration/scheduler_test.exs` | `:43`, `:57` `:orchestration` |
| `symmetry_control/orchestration_goal_admission_test.exs` | `:224`, `:226`, `:233-234` `:contracts` + `:goals` |
| `symmetry_control/orchestration_request_hash_test.exs` | `:12`, `:232` `:orchestration` |
| `symmetry_control/pi_control_e2e_test.exs` | `:124`, `:135`, `:2829` `Application.put_env`/`System.put_env` (plus the e2e daemon/build surface; W3-15 owns the file) |
| `symmetry_control/provider_broker_e2e_test.exs` | `:52-61` `TestEnv.put!` |
| `symmetry_control/resource_sync_identity_loss_test.exs` | `:58` `TestEnv.put!(:integration_providers, …)` |
| `symmetry_control/resource_sync_inflight_marker_test.exs` | `:58`, `:60` `TestEnv.put!` |
| `symmetry_control_web/controllers/goal_controller_test.exs` | `:675`, `:681` `:goals` |
| `symmetry_control_web/controllers/goal_machine_controller_test.exs` | `:599`, `:616`, `:628` `:contracts` |
| `symmetry_control_web/controllers/portal_goal_attention_test.exs` | `:40`, `:46`, `:99` `:goals` |
| `symmetry_control_web/controllers/portal_integrations_test.exs` | `:57`, `:665`, `:668`, `:719` `:integration_providers` + `:integration_provider_test_controller` |
| `symmetry_control_web/controllers/portal_resource_identity_test.exs` | `:10` `TestEnv.put!(:integration_providers, …)` |
| `symmetry_control_web/controllers/portal_resources_test.exs` | `:289`, `:294`, `:1731` `:integration_providers`, `:portal_resources_steering` |
| `symmetry_control_web/controllers/portal_workflow_test.exs` | `:1293`, `:1561`, `:1569` `:integration_providers`, `:goals` |
| `symmetry_control_web/controllers/provider_action_controller_test.exs` | `:253`, `:258`, `:906` `:integration_providers`, `:provider_action_test_controller`, `:goals` |
| `symmetry_control_web/portal_session_test.exs` | `:89`, `:120` `:orchestration` |

Writes outside the case's own sandbox transaction (an un-checked-out writer process or an unboxed
run cannot see, or be rolled back by, the per-test owner connection):

| file | call site |
| --- | --- |
| `symmetry_control/chat_test.exs` | `Sandbox.unboxed_run(Repo, …)` `:234`, `:265`, `:1128`; `Task.async` `:264` |
| `symmetry_control/orchestration_review_test.exs` | `Sandbox.unboxed_run` `:496`, `:564`, `:590`; `Task.async` `:613`, `:762` |
| `symmetry_control/workspaces_completion_test.exs` | `Sandbox.unboxed_run` `:546`, `:582`, `:595`; `Task.async` `:690` |
| `symmetry_control/goals/dispatch_notification_test.exs` | `Sandbox.mode(Repo, :auto)` `:30-50` (listed above for its env mutation too) |
| `symmetry_control/integrations_test.exs` | `Sandbox.unboxed_run` `:1249`, `:1254`, `:1397`; `Task.async` writing through `Integrations` `:54`, `:88`, `:1080` |
| `symmetry_control/resource_sync_inflight_marker_test.exs` | `Sandbox.unboxed_run` `:66`, `:88`, `:122`; `start_supervised!` writer `:165` (listed above for its env mutation too) |
| `symmetry_control/resource_sync_identity_loss_test.exs` | `Task.async(fn -> Integrations.sync_resource(resource.id) end)` `:71-73` |
| `symmetry_control_web/controllers/goal_machine_controller_test.exs` | `Sandbox.unboxed_run` `:1353`; `start_supervised!` `:1371` |
| `symmetry_control_web/controllers/portal_integrations_test.exs` | `Sandbox.unboxed_run` `:260`, `:270`, `:286`; `Task.async` `:269`, `:285`, `:677` |
| `symmetry_control_web/controllers/provider_action_controller_test.exs` | named ETS table `:provider_action_test_state` `:16`, `:103`, `:179` |
| `symmetry_control/integrations/syncer_test.exs` | `start_supervised!({Syncer, …})` `:31` (a process outside the test's checkout) |

Assert an unkeyed whole-table aggregate while a converted file can commit rows into that table:

| file | call site |
| --- | --- |
| `symmetry_control/workspaces_test.exs` | `Repo.aggregate(WorkItem, :count)` `:346`, `:359`, `:369`, `:423` |
| `symmetry_control/orchestration_test.exs` | `Repo.aggregate(Runtime/Task/Command/RunTransition, :count)` `:1160`, `:1169`, `:1208`, `:1222` |
| `symmetry_control/chat_test.exs` | `Repo.aggregate(Message/Action/WorkItem/Task/Run, :count)` `:16-17`, `:36-39`, `:151`, `:329-332` |

Global state that a converted neighbour would newly overlap:

| file | call site |
| --- | --- |
| `symmetry_control/orchestration_telemetry_test.exs` | `:telemetry_test.attach_event_handlers/2` on the orchestration event names `:17`, `:75`; `assert_event/3` (`:88`) selects by **event name + its own ref** (`assert_receive {^event, ^ref, %{count: 1}, metadata}`) and only then asserts this test's ids, so any concurrently emitted event of the same name is delivered by this test's own handler and fails the id assertions |
| `symmetry_control_web/portal_realtime_test.exs` | `Invalidations.subscribe()` `:133`, `subscribe_and_join(…, Invalidations.topic(), …)` `:110` — one process-wide topic (`SymmetryControl.Portal.Invalidations` `@topic "portal:invalidations"`) that production paths (`Chat`, the goal workers, `Syncer`, `Reconciler`) publish to, with unkeyed assertions (`assert_receive {:portal_invalidation, %{kind: "project"}}` `:146`) |
| `symmetry_control_web/router_invalidation_coverage_test.exs` | `Invalidations.subscribe()` in `setup` `:40` — same process-wide topic |

Cross-process delivery pinned to the shared-connection mode's scheduling (measured, see the answer):

| file | call site |
| --- | --- |
| `symmetry_control_web/channels/daemon_channel_test.exs` | `assert_push "work_available", …` `:28` — the 100 ms window `assert_push/3` installs. The hint broadcast reaches the test process in 20.6 ms in the pre-slice sync phase (pre-slice stream, seed 0) and in 162.7 ms once the file runs in per-test owner mode (single-file run, same host; the pass/fail boundary is the 100 ms window: red at 100 ms twice, green with the window widened to 5 s). The delay is delivery scheduling, not database work — `UserSocket.connect/2`'s one read runs in the test process and `DaemonChannel` touches no database — so the file stays `async: false` under the amendment-pinned **F1** reason: pre-existing load-marginal 100 ms `assert_push` window (`:28`); widening it is outside this slice's owned set. Its one post-slice seed-0 failure is disclosed with the same-seed re-run (green, name/state sets identical to the pre-slice run); the file is byte-identical to `d0761739`, so it is a recorded flake, not clause-3 evidence. |

Migration files (D9: `create_schema!`/`drop_schema!` are `Sandbox.unboxed_run` DDL on the shared
Repo — `migration_case.ex:58-70` — and each suite migrates its own private schema through a
dedicated Repo). All 9 stay `async: false`; the four carrying the scaffolding residue are folded
(Deliverable 2) with identical test names:

| file | why it stays sync |
| --- | --- |
| `symmetry_control/migrations/add_goal_operator_version_test.exs` | private-schema migration run (`with_schema/2` → `create_schema!`/`drop_schema!` unboxed DDL); folded |
| `symmetry_control/migrations/add_history_lookup_indexes_test.exs` | same, with an explicit `create_schema!`/`start_schema_repo`/`Repo.put_dynamic_repo` sequence `:29-32` |
| `symmetry_control/migrations/add_omo_harness_kind_migration_test.exs` | private-schema migration run; folded |
| `symmetry_control/migrations/add_retry_commands_test.exs` | `with_schema/2` `:14`, `:39` |
| `symmetry_control/migrations/add_task_command_ownership_test.exs` | same explicit schema sequence `:12-15` |
| `symmetry_control/migrations/add_work_item_creation_identity_test.exs` | private-schema migration run; folded |
| `symmetry_control/migrations/goal_0006_migration_test.exs` | the largest private-schema suite (60 tests, W3-15 owns the split) |
| `symmetry_control/migrations/goal_external_wait_migration_test.exs` | private-schema migration run; folded |
| `symmetry_control/migrations/version_request_hashes_test.exs` | `with_schema/2` `:14`, `:40`, plus `load_migration_modules!/0` |

Shared, mutable on-disk path:

| file | call site |
| --- | --- |
| `symmetry_control_web/controllers/portal_controller_test.exs` | `File.rename!(index_path, unavailable_path)` `:161` and the restore at `:171` move the shared staged bundle document (`control/priv/static/portal/`, `PortalAssets.index_path()`); the file also carries the four `:portal_assets` cases that need the staged build |

### Public shape

`SymmetryControl.MigrationCase` (extends the existing template; no behaviour change):

```elixir
migrations_path()  # module-scoped (defp): Path.expand("../../priv/repo/migrations", "control/test/support")
migrate_all_up!()  # Ecto.Migrator.run(Repo, migrations_path(), :up, all: true, log: false,
                   #   migration_lock: false, dynamic_repo: Repo.get_dynamic_repo())
```

Both bodies are taken from the four private copies; each hashes to one body across all four
donors (the dedented-body hashes are `057ba8c2d809` and `2c55e6bee54c`). Relative to the donors,
`migrate_all_up!/0` differs only in being declared public (the suites import it) and
`migrations_path/0` only in the relative-path literal re-expressed below. The `using do … import
SymmetryControl.MigrationCase,
only: […]` line gains `migrate_all_up!: 0` (wrapped over lines so `mix format --check-formatted`
stays green at 98 columns); `migrations_path/0` stays module-scoped (nothing else calls it —
verified: each of the four files references it exactly once, from inside its `migrate_all_up!/0`).
The path is re-expressed for the support directory's depth: `__DIR__` moves from
`control/test/symmetry_control/migrations` to `control/test/support`, so `"../../../priv/repo/migrations"`
becomes `"../../priv/repo/migrations"`; both expand to the same `control/priv/repo/migrations`.

`control/test/test_helper.exs`:

```elixir
ExUnit.start(max_cases: 28)                       # sandbox mode line unchanged below
Ecto.Adapters.SQL.Sandbox.mode(SymmetryControl.Repo, :manual)
```

`28` is pinned rather than left at ExUnit's default because 27 modules are async after the
conversion: the default on this host (32) already exceeds the pool coupling below, and on the
4-scheduler CI runner it is 8, below the module count. The effective ceiling each gate run printed
(`Running ExUnit with seed: N, max_cases: 28`) is recorded in the answer, together with the
measured phase shift it produces.

`control/config/test.exs` — only if the pinned ceiling needs it, and then together with it:
`pool_size: min(max(System.schedulers_online() * 2, 32), 90)`. Coupling invariant: each concurrent
async module holds one sandbox owner connection, so `max_cases + 2 ≤ pool_size ≤
min(PG max_connections − other clients, 90)` must hold on the dev host **and** on the 4-scheduler CI
runner (the +2 is the sync-module connection plus the app's own pool user). ExUnit's default ceiling
(`schedulers_online() * 2`) already violates the first inequality on this host (32 + 2 > 32) and
leaves only 8 concurrent module slots on the 4-scheduler CI runner; pinning 28 is what makes the
coupling explicit.

### Edge-case semantics this slice pins

1. **Test names and states are unchanged.** Measured with `SYMMETRY_EXUNIT_EVENTS_FILE` +
   `--formatter SymmetryControl.ExUnitJsonFormatter` (one `test_finished` line per test) on the
   pre-slice head and on the converted tree: the sorted `module<TAB>name` list and the per-test
   states must be identical, and the `suite_finished` counts equal. No test is renamed, deleted,
   skipped or retagged; the diff adds no `--exclude`/`--only` beyond the host's parity-matched
   `--exclude portal_assets`.
2. **The `:portal_assets` exclusion is parity-matched, not a scope reduction.** `control/priv/static/portal/`
   is gitignored build output; this host has no staged frontend bundle, so the four tagged cases in
   `portal_controller_test.exs` fail here by design (`PortalBundle.require_staged!/1`). The same flag
   is applied to the pre-slice and post-slice runs; CI stages the artifact in the `portal-assets`
   download step of the `control` job and runs the bare `mix test`.
3. **`max_cases` bounds modules.** With 16 existing + 11 converted = 27 async modules, a ceiling
   below 27 serializes the pool; the pinned value must be ≥ 27 and satisfy the pool coupling, and the
   pre/post wall time is reported with the effective ceiling each run printed.
4. **The mutation check is the lock-contention probe** (amended F2): two converted files take the
   same two advisory keys (41, 42) in opposite order, each waiting for the peer's first key, and the
   run fails red with `40P01 (deadlock_detected)`; the same edits with both files declared
   `async: false` run green. The earlier fixed-durable-key construction is recorded as insensitive
   (PostgreSQL's speculative-insert wait serializes it — green, with ~100× latency inflation) and
   is not the probe. An inverted assertion is not sensitive to sandbox isolation and is not the
   probe either.
5. **Fold-only files keep their exact test-name set** (events-file comparison, alone — not just as
   part of the full suite), and the four folded files' bodies change only by deleting the two private
   helpers.
6. **A flake is a finding.** Any failure is re-run once with the same seed; a failure that does not
   reproduce is recorded in the answer as a finding, never re-run until green.

### Evidence plan (all foreground, strictly serial; never a suite beside a measurement)

Recipe `gdd/refactor/scratch/w3-14-mix.sh` (portable Elixir 1.20.4 / OTP 29.0.6 via `erl.exe`,
`MIX_ENV=test`, `MIX_TEST_PARTITION=rf-w3-14`, `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR`
unset, deps from the worktree-local gitignored `control/deps`) and runner
`gdd/refactor/scratch/w3-14-baseline.sh <pre|post>`:

1. Pre-slice baseline at `d0761739`: `mix deps.get`, `mix compile --warnings-as-errors`,
   `mix ecto.create`, then `mix test --seed {0,1,2} --exclude portal_assets --formatter
   ExUnit.CLIFormatter --formatter SymmetryControl.ExUnitJsonFormatter`, wall time per seed.
2. Target checks after each conversion: `mix test <file>` for the converted file.
3. Post-slice: the same four commands plus the three seeded runs; compare name sets, states and
   counts against the baseline. Record the full-suite wall times and medians with **no threshold**
   (F4: this host cannot judge wall-time movement of this size), and run the amended clause-1
   acceptance: the paired-workload A/B on the 11 converted files, `async: false` vs `async: true`,
   three runs each back-to-back, async median ≤ 0.7 × sync median.
4. `mix format --check-formatted`, `MIX_ENV=test mix compile --warnings-as-errors` (after the last
   edit), `git diff --check`.
5. Probe: the lock-contention construction — two converted files taking the same two advisory keys
   in opposite order → red with `40P01`; the same edits with both files `async: false` → green. The
   fixed-durable-key construction is recorded as insensitive, not as evidence.
6. CI-only oracle: the `control` job's `MIX_ENV=test mix compile --warnings-as-errors` step and its
   bare `mix test` step (cited by name; the conductor verifies on the PR).

### Out of scope (with reasons)

- Splitting the >300-line test units (`goal_0006_migration_test.exs`, `pi_control_e2e_test.exs`,
  `goals_test.exs`) — W3-15 owns them; `pi_control_e2e_test.exs` and `goals_test.exs` are excluded
  here for their app-env mutations, `goal_0006_migration_test.exs` keeps `async: false` per D9.
- Fixture extraction: the remaining byte-identical clone pair inside the migrations tests
  (`execution_policy_json/0`, `add_goal_operator_version_test.exs` + `goal_external_wait_migration_test.exs`,
  one body hash) is a *fixture* clone, not scaffolding, and belongs to the fixture slices
  (W1-12/W3-03, both merged); folding it here would widen this slice's diff into that program item.
  Recorded for the follow-up extraction slice.
- New coverage, new tests, `mix.exs`/`mix.lock`, CI workflow files, `control/config/{dev,runtime,prod}.exs`,
  `--slowest`/coverage instrumentation (W2-17).
- Changing the sandbox to `shared: true` anywhere: forbidden by the contract's failure control.

### Residual risks

- The host is shared: other worktrees' suites share the same PostgreSQL cluster and the same CPU, so
  a single wall-time sample is not evidence. Mitigation: three seeds, median + max, and the same
  recipe for the baseline.
- New async overlap can surface flakes that never appeared in the serialized sync phase; the flake
  rule above records them as findings rather than hiding them by re-running.

### Conductor amendment (2026-11-22): amended clause 1 met — paired-workload A/B

The contract's original timing acceptance (`mix test` median ≤ 65 s and no seed > 75 s, or all
three ≤ 0.7 × the re-measured baseline) is **not reachable by any implementation that obeys this
contract's own classification rule and D9**; the conductor replaced it, binding (source: the
implement run's answer). The pre-slice stream (`w3-14-runs-pre/10-seed0.events.json`, the same
command the gate uses) measures why:

- The 11 DB-only files this slice converts carry **17.7 s of 482.6 s of per-test work (3.7%)**
  (including the ~0.05 s of `daemon_channel_test.exs`, which the measurement below sends back to
  `async: false`).
  The wall time each seed actually spends is `async_us + sync_us` (ExUnit runs sync modules only
  after every async module finished), so the deliverable can remove at most that share from the
  serial phase — a reduction of order 3% of the suite.
- The serial phase is dominated by modules the contract itself pins:
  `Goal0006MigrationTest` 180.8 s, `GoalsTest` 67.8 s, `ProviderActionControllerTest` 29.8 s,
  `AddWorkItemCreationIdentityMigrationTest` 27.7 s, `AddGoalOperatorVersionMigrationTest` 27.2 s,
  `GoalExternalWaitMigrationTest` 21.8 s, `AddOmoHarnessKindMigrationTest` 19.3 s,
  `OrchestrationTest` 17.7 s. The 9 migration files total 282.0 s (58.4%) and D9 keeps every one of
  them `async: false`; `goals_test.exs` (67.8 s) mutates `:symmetry_control, :goals` and belongs to
  W3-15; `provider_action_controller_test.exs` mutates app env, uses a named ETS table and asserts
  unkeyed aggregates; `orchestration_test.exs` asserts unkeyed whole-table aggregates.
- ExUnit's concurrency is **module-granular** (`ExUnit.Runner` :105-132 spawns modules, and a
  module's tests run serially inside their slot), so even converting the migration files would not
  help before W3-15 splits `goal_0006_migration_test.exs`: a 180.8 s module bounds the async phase
  at ≥ 180.8 s on its own.

All clauses of the amendment are met on the delivered tree:

- **Amended clause 1 (met):** the 11 converted files' 79 tests — the exact changed workload — run
  back-to-back on this host (2026-11-22), `async: false` vs the delivered `async: true`, three runs
  each; `run_us` is the run's own `suite_finished` field, command shape
  `mix test <11 files> --seed 0 --formatter …` (script `gdd/refactor/scratch/w3-14-ab-fix1.sh`,
  which asserts the declaration count it runs with — 0/11 sync, 11/11 async):

  | run | sync (`async: false`) `run_us` / wall | async (`async: true`) `run_us` / wall |
  | --- | --- | --- |
  | 1 | 7,535,820 µs / 9.79 s | 3,478,732 µs / 5.91 s |
  | 2 | 9,134,284 µs / 12.07 s | 3,659,878 µs / 6.76 s |
  | 3 | 9,911,500 µs / 13.07 s | 3,745,382 µs / 7.11 s |
  | median | 9.13 s | 3.66 s |

  async median / sync median = **0.40 ≤ 0.7 ✓** (wall-time medians 6.76 / 12.07 = 0.56 ≤ 0.7 ✓).
  All six runs 79/79 passed with identical `counts`, and every async run's `async_us == run_us`. This
  table is the single source for the amended clause-1 numbers; the implement stage's single-sample
  pair and its `max_cases` ceiling probe are superseded by it.
- **Full-suite walls recorded, no threshold:** pre-slice `d0761739` 496.7 / 877.4 / 545.4 s (median
  545.4), post-slice 580.3 / 387.6 / 833.6 s (median 580.3), post seed-0 re-run 665.3 s; every green
  run's test-name and state sets equal the pre-slice run's (1006 rows, three pairwise comparisons).
  Host wall clock spans 1.77× within one side under one identical command (F4), so no local
  full-suite wall-time criterion is claimed or met.
- **Probe replaced (F2):** the lock-contention construction (two converted files, same two advisory
  keys 41/42 in opposite order, each waiting for the peer's first key) → **red**: pair run 11/12
  with `ERROR 40P01 (deadlock_detected)`; the same edits with both files `async: false` → **green**:
  12/12, `0.00s async`, no `40P01`. The original fixed-durable-key construction is recorded as
  insensitive (speculative-insert serialization; green 12/12 with ~100× latency inflation), not as
  evidence.
- **F1 recorded:** `daemon_channel_test.exs` stays `async: false` with the reason and call site in
  the excluded table above; its one post-slice seed-0 failure is disclosed with the same-seed
  re-run, not hidden.
- **F3/F4 follow-ups recorded:** `execution_policy_json/0`'s fixture clone moves to the
  fixture-extraction slice; host wall time is not a gate oracle (paired workload or CI-side
  measurement instead).
- **Full-suite wall-time goal moved out of this slice:** to W3-15 (`goals_test.exs` split) and a D9
  re-evaluation of the migration-file pin (its DDL is already private-schema) — recorded as a
  follow-up, not claimed here.
