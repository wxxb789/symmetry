# rf-w3-17 migration-tests async conversion — plan

Contract: `goal-rf-w3-17-migration-tests-async.md` (GDD, 2026-11-22), including the conductor
decision on Conflicts 1/2 (the slice id is `rf-w3-17`; the ≤65 s program target is not this slice's
pass condition). Owner comment policy (2026-11-22) applies to every hunk this slice writes.
Binding inherited rules: W3-14's `goal-rf-w3-14-exunit-async.md` Change (1) and its conductor
amendment (no test renamed/deleted/skipped/retagged; name/state-set equality through
`SYMMETRY_EXUNIT_EVENTS_FILE` + `SymmetryControl.ExUnitJsonFormatter`; the flake rule; the
`max_cases`/`pool_size` coupling; lessons F2 — a probe must be shown red — and F4 — host wall clock
is not an oracle).

Base revision: `66bc9dcb5bc5a2012f16480dbb4c6e13e4f34283` = `origin/main` at dispatch = this
worktree's HEAD before the slice's first edit; working tree clean. #217 (`de526861`) is an ancestor
of the base, so the W3-15 fold (which created `MigrationCase.migrations_path/0` +
`migrate_all_up!/0`) is in the base exactly as the contract assumes. Every `<file>:<line>` citation
below resolves at the base unless it names a slice file.

## Re-derivation at the dispatch head

| measure | base value | command |
| --- | --- | --- |
| the nine files | 6,642 L, 78 tests | `wc -l`, `grep -c 'test "'` over `control/test/symmetry_control/migrations/*_test.exs` |
| migration files | 43 `.exs` | `ls control/priv/repo/migrations/*.exs \| wc -l` |
| `async: true` declarations | 34 files / 35 matching lines | `grep -rl/-rh "async: true" control/test` |
| `async: false` declarations | 45 files / 48 matching lines | same, `"async: false"` |
| after conversion (delivered) | 43 files / 44 `async: true` lines (one is an assertion argument, `pi_control_e2e_test.exs:5162`); 36 files / 39 `async: false` lines — +9 / −9 files and lines | same commands on the slice tree |
| `max_cases` / `pool_size` | 28 / `min(max(schedulers*2, 32), 90)` = 32 on this 16-scheduler host | `control/test/test_helper.exs:4`, `control/config/test.exs:18` |

Three ad-hoc schema-name constructions existed at the base
(`goal_0006_migration_test.exs:3834`, `add_history_lookup_indexes_test.exs:28`,
`add_task_command_ownership_test.exs:11`); all three now go through the one helper. Five files
carried a lazy `Code.compile_file` loader (`add_history_lookup_indexes_test.exs:132-147`,
`add_retry_commands_test.exs:137-152`, `add_task_command_ownership_test.exs:219-233`,
`goal_0006_migration_test.exs:4928-4984`, `version_request_hashes_test.exs:90-104`); all five
loaders and their seven call sites are deleted.

## Environment and command (identical on both sides)

* Toolchain: portable Elixir 1.20.4 / OTP 29.0.6 through `erl.exe`
  (`gdd/refactor/scratch/w3-17-mix.sh`, derived from the W3-14/W3-15 recipe); `MIX_ENV=test`;
  worktree-local gitignored `control/deps` (25/25 ok on both sides); `MIX_TEST_PARTITION=rf-w3-17`
  on both sides, so both sides use the same partition database (`symmetry_control_testrf-w3-17`) —
  sequentially, never concurrently: two suites on one database would race on the shared `public`
  tables and on the deterministic parts of the private-schema names.
* Partitions: every run pins `MIX_TEST_PARTITION=rf-w3-17` (its own database on the slice's
  cluster); probes P6a/P6b additionally created the disposable partitions `rfw317fresh` and
  `rfw317fresh2` to show the fresh-database boot behaviour. The host had 0 foreign `erl.exe`
  processes during the probe window, against 2-3 during the seed window.
* PostgreSQL: the shared instance on port 5432 was in crash recovery at dispatch (its
  `postmaster.pid` reported status `starting` and every connection answered
  `FATAL: the database system is not yet accepting connections — Consistent recovery state has not
  been yet reached` for the whole setup window), and the shell's inherited `POSTGRES_PORT` pointed
  at a port with no server. Both sides therefore ran against a disposable slice-owned cluster
  (`gdd/scratch/w3-17-pg`, port 55417) created for this slice with `initdb -U postgres -A trust
  --encoding=UTF8 --locale=C` and `-c timezone=Asia/Shanghai`, i.e. the same `max_connections` (100,
  the CI image's default too), the same locale and the same cluster timezone as the shared instance.
  The runner pins `POSTGRES_PORT=55417` instead of defaulting it, so the inherited stale value
  cannot leak in.
* Timed command, identical on both sides, always foreground and strictly serial (never two
  `mix test` runs at once):

```
SYMMETRY_EXUNIT_EVENTS_FILE=<run>/<label>.events.json mix test --seed <0..4> \
  --exclude portal_assets --formatter ExUnit.CLIFormatter --formatter SymmetryControl.ExUnitJsonFormatter
```

`--exclude portal_assets` is the documented host path for a machine without the staged frontend
bundle; the flag is identical on both sides and CI covers those four cases.

## Deliverable shape

| file | change |
| --- | --- |
| `control/test/support/migration_modules.ex` | new: boot-time `{version, module}` registry over `priv/repo/migrations` (`compile_all!/0`, `migrations/0`); recursive `.exs` glob with an empty-glob raise, compile under `ignore_module_conflict`, named error when the registry was never built. Fix revision adds `verified_pairs!/1` (the registry binding every migrator source routes through) and the one shared `base_migrations/0` (review #3/#6) |
| `control/test/test_helper.exs` | `+ SymmetryControl.MigrationModules.compile_all!()` before `ExUnit.start/1` (plus the existing blank line) |
| `control/test/support/migration_case.ex` | `+ schema_name/1` (one naming authority: lowercase-identifier check, node-unique plus wall-clock suffix, 63-byte guard); `create_schema!/1`/`drop_schema!/1` run their DDL on a private single-connection non-sandbox Repo and register an ExUnit `on_exit` drop; `migrate_all_up!/0` runs `MigrationModules.migrations()` instead of the path source; `private_repo_config/0` shared by `start_schema_repo/1` and the DDL Repo, with `migration_lock: false`, `timeout: 30_000`, `queue_target: 5_000`, `queue_interval: 10_000`. Fix revision: `with_schema/2`'s teardown reduced to a non-raising dynamic-repo restore (review #5), the execution recipe factored into `run_private_query!/2` so `on_exit` cleanup can use it, and the case template aliases `MigrationModules` into every user |
| the nine `control/test/symmetry_control/migrations/*_test.exs` | `async: false` → `async: true` (nine declarations); five `load_migration_modules!/0` definitions and seven call sites deleted; three schema-name sites routed through `schema_name/1`; `goal_0006_migration_test.exs` loses its two Oban tests and their now-unused helpers to the F-8 move. Fix revision: all 53 `Ecto.Migrator.run` sources route through `MigrationModules.verified_pairs!/1`, and the duplicated 20-pair base list plus its now-unused aliases are deleted (review #3/#6) |
| `control/test/symmetry_control/migrations/add_oban_jobs_table_test.exs` | new (F-8 ruling): `async: false` module holding the two moved Oban tests, names and bodies byte-identical, with the local helper set the directory's convention gives every file. Fix revision: extends the shared `MigrationModules.base_migrations/0`, drops its 20 duplicated aliases, and registers the marker-row `DELETE` as an `on_exit` (PRE-A01) |
| `control/test/symmetry_control/native_harness_kind_binding_test.exs` | one change (F-7 ruling): the live-constraint read is scoped to the test's own schema (`AND connamespace = current_schema()::regnamespace`) |
| `docs/plans/2026-11-22-rf-w3-17-migration-tests-async-plan.md` | this plan |
| `docs/TODO.md` | the wall-time entry gains this slice's numbers and the disposition request |

No production file, no `priv/repo/migrations/**` file, no other test file, no CI workflow, no
`mix.exs`, no `config/test.exs` change (the envelope is kept, see below).

## Design decisions the contract left open

1. **Boot registry, derived in one pass.** `compile_all!/0` takes
   `Path.wildcard(priv/repo/migrations/*.exs)`, sorts it, and per file derives the version with
   Ecto's own rule (`Integer.parse/1` of the rootname, requiring a `"_" <> name` suffix — the same
   parse as `Ecto.Migrator.extract_migration_info/1`) and the module from `Code.compile_file/1`'s
   return value filtered by `function_exported?(module, :__migration__, 0)` — the same filter as
   `Ecto.Migrator.migration?/1`. A file with no version prefix or no migration module raises at
   boot. The compile happens once per VM at boot, before any test module loads, and the test path
   never compiles again (`migrate_all_up!/0` passes module tuples), so no run redefines a migration
   module: zero `redefining module` lines across all 21 recorded run logs (post seeds, probe runs,
   gate sweeps) checked for this plan. The pair list is stored in `:persistent_term` (written once, read many times, never a
   message round trip) and read by `MigrationCase.migrate_all_up!/0`. Drift is impossible by
   construction because the wildcard is the only source; version/name uniqueness is re-checked by
   `Ecto.Migrator.ensure_no_duplication!/1` on every `run/4` over the 43 pairs.
2. **DDL placement.** `create_schema!/1`/`drop_schema!/1` start a fresh private single-connection
   Repo (`pool: DBConnection.ConnectionPool`, `pool_size: 1`, `name: nil`, default search path),
   run one statement through `Ecto.Adapters.SQL.query!/4` (whose first argument is documented as
   `pid() | Ecto.Repo.t()`) and stop it. Rejected alternative: running the DDL on the test's own
   `start_schema_repo/1` Repo, which the contract's parenthetical allows — it would force
   `with_schema/2` to start the Repo before the schema exists, move `drop_schema!` before
   `GenServer.stop(repo)` for the two manual call sites, and put `DROP SCHEMA … CASCADE` on a pool
   that may still hold a test's transaction. The chosen shape leaves create/start/stop/drop order
   byte-identical to the base, is off every test-owned pool, and runs outside every transaction
   (the `concurrently: true` history-index migration needs that). One extra connection per
   statement is opened and closed; it is not held for the test's duration.
   Both private pools (the schema Repo and the DDL Repo) carry `timeout: 30_000`,
   `queue_target: 5_000, queue_interval: 10_000`: a request waiting on a connection that is still
   being established on a saturated host waits instead of being dropped at DBConnection's ~4 s
   default (the remedy the library itself documents for that error,
   `db_connection/lib/db_connection/connection_pool.ex:382-390`), and a first statement in flight
   gets a 30 s budget instead of 15 s. Findings F-1/F-4/F-5 are why; ExUnit's 60 s per-test timeout
   still bounds a genuine hang.
3. **Consequence for the manual call sites.** They keep their exact `create → start → put_dynamic
   → after: restore/stop/drop` order; only the name construction changes. `with_schema/2` now owns
   the name for all six `with_schema` bases it is given and for `goal_0006`'s base
   (`"goal_0006_migration"`), replacing the former `System.system_time(:microsecond)` name.
   Superseded in part by review #5 (fix revision): `with_schema/2` no longer stops its Repo or drops
   its schema — the Repo is link-managed and the drop lives in `create_schema!/1`'s `on_exit`; see
   "Fix-stage verification" for the probe that established both.
4. **`schema_name/1` guard and uniqueness.** PostgreSQL silently truncates identifiers past
   `NAMEDATALEN - 1 = 63` bytes, which would make two schemas collide on a long base; the helper
   raises instead, and it validates the base against `\A[a-z_][a-z0-9_]*\z` so a non-literal caller
   gets a clear error instead of raw SQL. The longest base in play is 31 characters
   (`add_work_item_creation_identity`); with `_`, a 10-digit `System.unique_integer([:positive])`
   maximum and a 17-digit `System.system_time(:microsecond)` the worst case is 60 bytes. The
   wall-clock component is deliberate: the base's goal_0006 name was cross-VM unique, and the first
   revision of this helper dropped that property (the review caught it — finding R5); the suffix
   keeps node and cross-VM uniqueness.
5. **Envelope.** 43 async declarations against `max_cases: 28` at the base's `pool_size: 32`. The
   measured choice is recorded in the envelope section; the invariant `max_cases + 2 ≤ pool_size ≤
   min(PG max_connections − other clients, 90)` is checked against both hosts (16-scheduler local,
   4-scheduler CI).

## Attempt-2 conductor rulings (F-7, F-8, measurement scope) and their delivery

Binding rulings of 2026-11-22 after attempt 1's findings — what the tree now carries:

1. **F-7** — the unkeyed `pg_constraint` reader is in the owned set for one change: scope it to the
   test's own schema (`AND connamespace = current_schema()::regnamespace` or the equivalent already
   used nearby), show it red under the concurrent conversion and green after. Delivered at
   `control/test/symmetry_control/native_harness_kind_binding_test.exs:219-222`; the red is attempt
   1's concurrent observation (3 of 5 converted-revision full-suite runs, 0 of 6 baseline runs) plus
   the deterministic re-run and the green half in the Probes section (P4b re-run).
2. **F-8 (third reading)** — move only the two Oban-rollback tests (the `public.oban_jobs`
   `LOCK TABLE … IN SHARE MODE` paths) into a separate `async: false` module in a new file beside
   `goal_0006_migration_test.exs`, test names byte-identical, the rest of goal_0006 `async: true`;
   record the reason here. Delivered: `add_oban_jobs_table_test.exs`. Reason: ExUnit starts
   `async: false` modules only after every async module has finished, so the refusal test's SHARE
   lock can no longer queue behind the async phase's Oban producers (finding F-8's producer chain,
   `task_goal_guard_controller_test.exs:2` → `Goals.command/4` → `Oban.insert!` inside the peer's
   sandbox transaction); the peer modules' helper set is duplicated locally because every file in
   this directory owns its migration list, and the census confirms the union of the donor and the
   new file is byte-identical in its assertion lines and test names.
3. **Measurement scope** — the full control suite runs in the FOREGROUND three times (seeds 0, 1, 2)
   on the final tree, sequentially, plus one nine-file subset pre/post A/B; no background jobs; wall
   per phase and the two Oban tests' durations recorded. This narrows the contract's five-seed sweep
   to three seeds on the post side; the pre side keeps its five recorded seeds (`w3-17-runs-pre/`)
   for the equality comparison, and the A/B is the paired same-workload measurement.
4. Attempt-1 evidence from the earlier revision stays valid where its files are unchanged:
   the pre-side five seeds, the `g4small` gate-4 sweep, probes P1/P2/P3/P5/P6, and the envelope A/B.
   The gate table below marks which runs are attempt-2 reruns; the two rulings changed exactly the
   files listed in the deliverable shape above.

## Review-stage conductor rulings (2026-11-22 review `DECISION_NEEDED`) and their delivery

An independent review of the reviewed head (`f6b5b176`) returned `DECISION_NEEDED`: six findings
(#1-#6), three plan-accuracy defects on the spec axis (SPEC-A01..A03) and one pre-existing finding
(PRE-A01, outside the verdict). The conductor ruled on every one before this fix revision; the
rulings are binding dispositions, not re-opened design. What the tree and this record carry now:

1. **#1 (repeat counts) — conductor-accepted, recorded.** Gates 4/5 ran at 10 and 5 repetitions
   against the contract's 25 and 10, with cost reasons but no ruling at the time. The conductor
   accepts the reduced counts against the compensating evidence (five pre-side seeds, three
   final-tree seeds, five converted-set seeds, and CI's `coverage-control` running the converted
   suite on every PR), so no re-run at the contract counts is required. The deviation is recorded
   verbatim in the gate table; the stale cross-reference in the envelope section was reconciled.
2. **#2 (`schema_name/1` wall-clock suffix) — recipe amended.** The suffix stays: it keeps the
   cross-VM uniqueness the base's `goal_0006` name had (R5), which a name built from
   `System.unique_integer/1` alone does not (every VM starts low). Contract Change 2 / clause 1 are
   amended to "a per-module private schema name with a uniqueness suffix"; the amended reading and
   its reason are recorded here, replacing the contract-literal "no `System.system_time`" recipe
   that the plan's own design decision 4 had already diverged from.
3. **#3 (version<->module binding) — restored in code.** `MigrationModules.verified_pairs!/1`
   returns a `{version, module}` list unchanged after checking every pair against the boot
   registry's file-derived entry for its module (version mismatch or a module with no file raises).
   All 53 `Ecto.Migrator.run` sources under `control/test/symmetry_control/migrations` (43 literal
   lists, 7 helper calls, 3 `helper() ++ [literal]` concatenations) route through it, so a
   re-versioned migration file turns the suite red instead of leaving it green with the old version.
   The guard is named `verified_pairs!` and not `assert_pairs!` (the review's example name) because
   the assertion-preservation census classifies every line containing an `assert*`/`refute*`
   identifier: a guard call is not an assertion, and the 53 new lines would otherwise land in that
   class and make the census's IDENTICAL verdict unreadable. Its red/green pair is mutant M1 in
   "Fix-stage verification".
4. **#4 (`public.oban_jobs` premise) — rewritten from the measurement.** The shared-state inventory
   and the Oban audit now state the measured mechanism — `task_goal_guard_controller_test.exs`
   (`async: true`) admits a Goal task whose `Goals.command/4` path runs `Oban.insert!` inside the
   test's sandbox transaction, so the refusal test's `LOCK TABLE public.oban_jobs IN SHARE MODE`
   could queue behind an async writer — the async `migrate_all_up!()` runs that drive
   `AddObanJobsTable` (and therefore `Oban.Migrations` with its default `public` prefix) as the
   remaining coupling with the version-not-14 trigger, and the delivered remedy: the two
   Oban-rollback tests moved to the `async: false` module, which removes the refusal test from the
   async phase.
   No further escalation is required (the ruling); the audit's older reading is kept as history.
5. **#5 (teardown masking) — corrected, with the suggested mechanism adapted to measurement.**
   The review's scenario is real and was reproduced (mutant M2): with the old `after`-block
   teardown, a body failure plus a failing `GenServer.stop` reports the stop's exit instead of the
   assertion. The review's suggested mechanism — move restore/stop/drop into `on_exit` — cannot be
   taken literally: probes X1/X2 show ExUnit terminates the test process with `:shutdown` before
   `on_exit` runs, so the linked private Repo is already terminating and a stop in `on_exit` races
   it; and the suite Repo cannot run in `on_exit` at all (no sandbox ownership). The delivered fix
   therefore removes the stop (the Repo is link-managed and dies with the test process, killed or
   not) and keeps the schema drop in `create_schema!/1`'s `on_exit` (R4), which is the teardown step
   that must survive a kill; the only `after` left in `with_schema/2` restores the dynamic repo,
   which cannot raise.
6. **#6 (duplicated 20-pair base list) — collapsed.** One definition,
   `MigrationModules.base_migrations/0`, guarded by `verified_pairs!/1`; `goal_0006_migration_test.exs`
   and `add_oban_jobs_table_test.exs` both extend it (the donor's copy and the F-8 copy, 20 pairs
   each, are gone along with the now-unused aliases). Drift between the two files is no longer
   possible; a drift between the shared list and the files is mutant M3.
7. **PRE-A01 (pre-existing, outside the verdict) — fixed alongside.** The refusal test now registers
   the idempotent, id-keyed `DELETE FROM public.oban_jobs` as an `on_exit` right after the marker
   INSERT, so a failing assertion no longer leaves the synthetic row behind. The callback cannot use
   `Repo` (X1: no sandbox ownership in `on_exit`) or the test's dynamic Repo (X2: already
   terminating), so it goes through the new `MigrationCase.run_private_query!/2`, which runs the
   statement on a fresh private single-connection Repo — the shape `create_schema!/1` and
   `drop_schema!/1` now share too. Its red/green pair is mutant M10.

SPEC-A01 (the A/B table and its statements), SPEC-A02 (the gate-4 evidence citation) and SPEC-A03
(the stale R6 row) are corrected in place below. Each mechanism claim in this section names the
probe or mutant that proved it at the fix revision; the probes are in "Fix-stage verification".

## Shared-state inventory (before → after)

| shared surface at the base | why shared | after |
| --- | --- | --- |
| migration modules in the global code server: the base's one path-source helper (`MigrationCase.migrate_all_up!/0`, 11 call sites across four files, each call `Code.compile_file`-ing all 43 files) + 5 lazy loaders under a `function_exported?` check-then-act guard | redefinition warnings: 516 lines in the baseline seed-0 log (473 in seed 1), 0 in every delivered-side log; under `async: true` a redefinition can purge/swap a module another process executes | compiled once at boot under `ignore_module_conflict` (the `test` alias may have loaded the same files first); test time passes `{version, module}` tuples only; no `Code.compile_file` under `control/test` on this path |
| `CREATE SCHEMA`/`DROP SCHEMA` through `Sandbox.unboxed_run(Repo, …)` — a checkout against the suite-wide sandbox pool whose mode is one global value | DDL under the shared Repo while other tests use it | DDL on a fresh private single-connection non-sandbox Repo; the shared Repo is untouched |
| schema names built ad hoc at three sites; one from `System.system_time(:microsecond)` | not unique under concurrency | one helper: module base + node-unique `System.unique_integer([:positive])` + wall-clock microseconds, 63-byte guard, lowercase-identifier check |
| `public.oban_jobs` (the two Oban-rollback tests, in the F-8 `async: false` module) | the production migration hard-codes `public.` and the refusal test takes `LOCK TABLE public.oban_jobs IN SHARE MODE`; the async `migrate_all_up!()` runs drive `AddObanJobsTable` → `Oban.Migrations` with the same default `public` prefix, and the async phase enqueues through `task_goal_guard_controller_test.exs` → `Goals.command/4` → `Oban.insert!` | the refusal tests now run in the serial phase (F-8 move), so their SHARE lock cannot queue behind an async producer; the `AddObanJobsTable`-under-`migrate_all_up!` coupling stays, is bounded by the private pools' 30 s budget, and is audited not edited (see the Oban audit) |
| `public.schema_migrations` | never touched by these tests: every `Ecto.Migrator.run` here targets the private schema through `put_dynamic_repo` + `search_path` | unchanged |

## Assertion-preservation census (base vs delivered)

`gdd/refactor/scratch/w3-17-census.py <base-root> <cand-root>` compares, per file, the line count of
`assert*`/`refute*` lines, the whitespace-normalized multiset of those lines (so an edited expected
value shows up), and the sorted `test "…"` name set.

Attempt 2 reruns the census with `gdd/refactor/scratch/w3-17-r2-census.py`, which knows the F-8 move:
the donor (`goal_0006_migration_test.exs`) is compared against the **union** of the candidate donor
and `add_oban_jobs_table_test.exs`, and the F-7 reader file (outside `migrations/`) is compared too.

| unit | assert/refute-family lines base/cand | tests base/cand | verdict |
| --- | --- | --- | --- |
| `add_goal_operator_version_test.exs` | 15 / 15 | 2 / 2 | IDENTICAL |
| `add_history_lookup_indexes_test.exs` | 15 / 15 | 1 / 1 | IDENTICAL |
| `add_omo_harness_kind_migration_test.exs` | 7 / 7 | 3 / 3 | IDENTICAL |
| `add_retry_commands_test.exs` | 10 / 10 | 2 / 2 | IDENTICAL |
| `add_task_command_ownership_test.exs` | 9 / 9 | 1 / 1 | IDENTICAL |
| `add_work_item_creation_identity_test.exs` | 8 / 8 | 3 / 3 | IDENTICAL |
| `goal_0006_migration_test.exs` + `add_oban_jobs_table_test.exs` (union) | 274 / 274 | 60 / 60 | IDENTICAL |
| `goal_external_wait_migration_test.exs` | 36 / 36 | 4 / 4 | IDENTICAL |
| `version_request_hashes_test.exs` | 3 / 3 | 2 / 2 | IDENTICAL |
| `native_harness_kind_binding_test.exs` (F-7 reader, outside `migrations/`) | 14 / 14 | 3 / 3 | IDENTICAL |
| **total** | **377 / 377** | **78 / 78** | **IDENTICAL** |

The comparison is not a line count alone: the whitespace-normalized multiset of the assert lines is
equal per file, so an edited expected value inside an assertion would show as a difference. `RESULT:
IDENTICAL`, exit 0. Every row is one run of `w3-17-r2-census.py` over the two worktrees (attempt-1's
table carried the same 377/78 totals with different per-file splits from an earlier tool revision;
this table is the authoritative one). The reader's 14 assert-family lines are identical on both
sides, so the F-7 predicate change did not touch an assertion. The same tool and roots were rerun
on the fix revision (`66bc9dcb` baseline worktree vs. this worktree) with the same verdict: IDENTICAL,
377/377 assert-family lines and 78/78 test names, so the guard routing, the shared base list and the
teardown change touched no assertion. The guard is named `verified_pairs!` rather than the review's
example `assert_pairs!` for this reason: the census's class is every line carrying an `assert*`/
`refute*` identifier, a guard call is not an assertion, and 53 new lines in that class would have
made the verdict unreadable (see the review-stage rulings).

## Diff line classification (the slice's own hunks)

`git diff --no-renames -U0` over the nine files, every line classified as declaration /
naming plumbing / loader call site / loader block / blank, with zero unclassified lines:

| class | lines | content |
| --- | --- | --- |
| declaration | 18 | nine `-  use SymmetryControl.MigrationCase, async: false` / `+ … async: true` pairs |
| naming plumbing | 6 | `-  schema = "history_lookup_indexes_#{System.unique_integer([:positive])}"` → `+  schema = schema_name("history_lookup_indexes")`; the same for `u1_command_ownership`; `-  with_schema("goal_0006_migration_#{System.system_time(:microsecond)}", fn ->` → `+  with_schema("goal_0006_migration", fn ->` |
| loader call sites | 7 | the seven `-    load_migration_modules!()` lines |
| loader blocks | 111 | the five `defp load_migration_modules!/0` definitions: 55 lines in `goal_0006_migration_test.exs`, 15 in `add_history_lookup_indexes_test.exs`, 14 in `add_retry_commands_test.exs`, 14 in `version_request_hashes_test.exs`, 13 in `add_task_command_ownership_test.exs` (wrapped entries included; blank lines counted separately) |
| blank lines | 17 | blank lines released by the above deletions |

No assertion, expected value, ordering, alias or helper body outside these classes changed. The
census above is the independent content check (identical multisets), this table the structural one.

Attempt 2 adds two hunk classes on top of the above, both ordered by a conductor ruling:

| class | where | content |
| --- | --- | --- |
| query predicate | `native_harness_kind_binding_test.exs` (F-7) | `- "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1"` → `+` the same query as a heredoc with `AND connamespace = current_schema()::regnamespace`; plus two `#`-comment lines stating the concurrency reason. The changed text is the **argument** of the same `assert %{rows: [[definition]]} =` — no assertion, expected value or order changed, and the census shows the reader's 14 assert-family lines byte-identical |
| F-8 move | `goal_0006_migration_test.exs` (donor) and `add_oban_jobs_table_test.exs` (new) | the two test bodies, their two helper definitions, the `@oban_migration_version` attribute and the `AddObanJobsTable` alias move out of the donor into the new module byte-for-byte; the new module additionally carries `with_base_schema/1`, `migrate_base_up!/0`, `migrate_goal_up!/0`, `base_migrations/0` (20 literal pairs), their aliases and `@migration_version`, copied from the donor because every file in this directory owns its migration list |

`git diff --stat` over the final tree (attempt 1 + 2, uncommitted): the nine files, the two support
files, `test_helper.exs`, the reader file, the new module, the plan and (after this stage's other
deliverable) `docs/TODO.md`.

The fix revision (review corrections, this stage) adds four more classes, all mechanical:

| class | where | content |
| --- | --- | --- |
| guard routing | the ten migration test files, 53 sites | every `Ecto.Migrator.run` source wrapped in `MigrationModules.verified_pairs!(…)` (one line added, or the file's own list helper call wrapped); no assertion, expected value or test name touched — the census below is still IDENTICAL |
| guard + shared base | `migration_modules.ex` | `verified_pairs!/1` and one `base_migrations/0` (the moved 20-pair definition plus its 20 aliases) |
| base-list deletion | `goal_0006_migration_test.exs`, `add_oban_jobs_table_test.exs` | each file's `defp base_migrations/0` (20 pairs) and its copy of the 20 aliases deleted; the four call sites read `MigrationModules.base_migrations()` |
| teardown + cleanup | `migration_case.ex`, `add_oban_jobs_table_test.exs` | `with_schema/2`'s nested `after` blocks (stop/drop) replaced by one non-raising restore plus the existing `on_exit` drop; the private-Repo execution recipe factored into `run_private_query!/2`; the refusal test gains the `on_exit` marker-row delete |

Tag hygiene, same diff scope: zero added or removed lines matching `@moduletag`, `@tag`, `:skip`,
`:only`, `ExUnit.configure` or `portal_assets` (`git diff -U0 -- control/test | grep -c …` → 0 for
each), so nothing was retagged, skipped or re-excluded.

## Static isolation audit (contract gate 6)

Commands run from the repository root on the delivered tree, all three recorded with their exit
status:

```
grep -rn "Code.compile_file\|Sandbox.unboxed_run\|Sandbox.mode" \
  control/test/symmetry_control/migrations control/test/support/migration_case.ex
  → no matches, rc=1
grep -rn "migrations_path\|migration_source" \
  control/test/symmetry_control/migrations control/test/support/migration_case.ex
  → no matches, rc=1
grep -rn "Ecto.Migrator.run" control/test/symmetry_control/migrations | wc -l
  → 53 call sites, every source argument a `{version, module}` list
python w3-17-r3-migrator-sources.py control/test/symmetry_control/migrations control/test/support
  → sites=53 wrapped=53 literal=43 helper=7 helper_plus_literal=3 unclassified=0, rc=0
```

The 53 sources were classified mechanically (each call's second argument parsed out and checked):
43 are literal `{version, Module}` lists; 7 call the files' own `migrations/0` / `base_migrations/0`
helpers, which hold literal pairs only; 3 are `helper() ++ [{…}]` concatenations that add one
literal pair (all three print in the classifier's `concat` class). The count moved 51 → 53 with the
F-8 move: `goal_0006_migration_test.exs` lost the two Oban helpers and
`add_oban_jobs_table_test.exs` brings four sites (one `base_migrations()` helper, three literals).
At the fix revision every one of the 53 sources is additionally wrapped in
`MigrationModules.verified_pairs!/1` (review #3); the classifier unwraps the guard and counts it
(`wrapped=53`), so the inner expressions above are the classified ones and still no source is a
path, a directory or a variable.
The four files that migrate the whole directory call
`migrate_all_up!/0` (11 sites), whose implementation
(`control/test/support/migration_case.ex`, `migrate_all_up!/0`) passes
`MigrationModules.migrations()`. No path,
directory or string source remains. The suite-wide `Sandbox.mode(Repo, :manual)` stays in
`test_helper.exs:5` (it is the suite's global mode, outside the audited path set, and the audit's
scope is the migration harness).

Boot registry equality (`MIX_ENV=test mix run --no-start w3-17-r2-registry-check.exs`, compiled test
support, no app start; rerun on the fix revision):

```
registry_pairs=43 wildcard_files=43 version_sets_equal=true unique_pairs=true all_loaded=true
```

The version set is derived from the same `Path.wildcard/1` listing in the same pass, so equality is
structural; the recorded line shows the boot-time registry equals the directory listing (43 files,
43 unique versions), and every pair's module answers `__migration__/0`. A stricter
module-name-vs-file-suffix check (`w3-17-registry-pairs.exs`) reports one pre-existing mismatch:
`20260909000000_add_goal_0006_control_plane.exs` defines `AddGoal0006ControlPlane`, whose
snake-cased name is `add_goal0006_control_plane`. That is production naming, unchanged by this
slice, and a `{version, module}` source does not depend on the file/module name agreeing — the
registry takes the module `Code.compile_file/1` returns — so it is recorded, not fixed.

Two fidelity checks read out of the pinned dependencies rather than assumed:

* `MigrationModules.version!/1` parses the version exactly the way `ecto_sql` parses a directory
  source — `Integer.parse(Path.rootname(Path.basename(file)))` against `"_" <> name`
  (`control/deps/ecto_sql/lib/ecto/migrator.ex:676-698`), so the registry cannot admit a file the
  migrator would reject or reject one it would accept.
* A `{version, module}` list source is re-sorted by the migrator itself
  (`control/deps/ecto_sql/lib/ecto/migrator.ex:660-672`, `|> Enum.sort()`), so the registry's own
  filename order has no semantic weight; and `Ecto.Repo.put_dynamic_repo/1` returns the previous
  repo (`control/deps/ecto/lib/ecto/repo.ex:314-316`), which is what `with_schema/2`'s
  `after` clause restores.

## Oban audit (contract Change 3)

Audit at the delivered tree (commands and outputs recorded):

* `grep -rn "public\.oban_jobs" control/test --include="*_test.exs"` at the fix revision → 12
  lines, all in `add_oban_jobs_table_test.exs` (`:2` the pin comment, `:18-79` the two tests plus
  the refusal test's `on_exit` cleanup): the tests read `to_regclass`, insert one unique marker row,
  assert the refusal, and delete the row (inline and again idempotently in `on_exit`). No other test
  file names the table.
* `Oban`/`oban_jobs` references per test file, with the file's declaration (fix revision): every
  file that names Oban is `async: false` — the eight `goals*` files, the four
  `orchestration_*_test.exs` files, and this slice's `add_oban_jobs_table_test.exs`. So no
  `async: true` file reads or writes `public.oban_jobs` *by name*. That is not the whole coupling:
  every async `migrate_all_up!()` run also drives `AddObanJobsTable`, whose `up` delegates to
  `Oban.Migrations.up/1` with the default `public` prefix
  (`control/deps/oban/lib/oban/migrations/postgres.ex:10,19-34`) — at the pinned version 14 that is
  a no-op read of the version comment, at any other recorded version it alters and comments the
  shared `public.oban_jobs` (the version-not-14 trigger; production path, recorded not edited) —
  and the async phase does enqueue Goal jobs without naming Oban
  (`task_goal_guard_controller_test.exs:2` → `admitted_goal_task/0` → `Goals.command/4` →
  `Oban.insert!`). Both halves are what the refusal test's `LOCK TABLE … IN SHARE MODE` can wait
  behind; the F-8 move removes the refusal test from the async phase, which is the half the
  ruling ordered fixed.
* The refusal test's `LOCK TABLE public.oban_jobs IN SHARE MODE` is taken by the production
  migration's `down` inside Ecto's per-migration transaction, which then raises (the DO block) and
  rolls back: the lock is held for the duration of a failing statement sequence, milliseconds, not
  for a test.
* Conflicting holders are lock modes, not files: `SHARE` conflicts with the `ROW EXCLUSIVE` that any
  `INSERT`/`UPDATE`/`DELETE` on `public.oban_jobs` takes. The four `async: false` Oban files above
  (and any file whose test drives app code that enqueues) are writers, and ExUnit starts `async:
  false` modules only after every async module has finished, so they cannot overlap goal_0006's
  refusal test at all. A hypothetical async writer would only block (Postgrex's client-side timeout
  is 15 s, three orders of magnitude above the lock's hold time). **Correction (finding F-8, found by
  the independent review, verified in the tree): the async phase does enqueue Goal jobs, just not
  through any file that names Oban.** `task_goal_guard_controller_test.exs:2` is `async: true` and
  its test admits a Goal Task (`admitted_goal_task/0`, `:59` → `command_current(…, "admit_task", …)`,
  `:117-131` → `test_fixtures.ex:14-23` → `Goals.command/4`, `goals.ex:293-307`), whose
  `execute_command` inserts a `WakeupWorker` (`goals.ex:4491-4495`: `Oban.insert!`) inside the
  test's sandbox transaction; `config/test.exs:51-55` sets no Oban `prefix:`, so the insert lands in
  `public.oban_jobs` and its `ROW EXCLUSIVE` lock lives until that test's rollback
  (`data_case.ex:50`, `shared: not tags[:async]`). The refusal test's `LOCK TABLE … IN SHARE MODE`
  therefore waits for such a peer; the wait is bounded by the peer's remaining test time, and only a
  hold beyond the harness's 30 s budget (or an accumulated wait past ExUnit's 60 s per-test timeout)
  turns it into a failure. Probes P3a/P3b/P3c measure that residual. The marker row itself cannot
  conflict: it is unique, inserted and deleted inside the
  same test, and no other test writes that table concurrently.
* **Resolution (attempt 2, conductor ruling F-8, third reading).** The two Oban tests moved out of
  `goal_0006_migration_test.exs` into a new `async: false` module —
  `control/test/symmetry_control/migrations/add_oban_jobs_table_test.exs`,
  `SymmetryControl.Migrations.AddObanJobsTableTest` — with names and bodies byte-identical (the
  census's union comparison is IDENTICAL to the base donor), so ExUnit runs them after every async
  module and the refusal test can no longer wait on an async Oban producer. The new module carries
  the helpers it needs locally (`with_base_schema/1`, `migrate_base_up!/0`, `migrate_goal_up!/0`,
  `migrate_oban_up!/0`, `migrate_oban_down!/0`, `base_migrations/0`, their aliases and the two
  version attributes) — this directory's convention is one self-contained file per migration — and
  the census shows no assertion line was duplicated or lost (377 on both sides). Evidence:
  `w3-17-r2-gates/smoke-obanfile.log` reports `7.5 s (0.00s async, 7.5s sync)`, i.e. the module is
  in the serial phase; the paired run `w3-17-r2-probes/f8-pairing.*` (the real async producer
  `task_goal_guard_controller_test.exs` beside the moved module) is green, `4 passed`, `7.6 s
  (2.1s async, 5.5s sync)`, with the two Oban tests at 2.66 s and 2.53 s — against 9.9 s / 15.9 s
  and 6.3 s / 36.3 s in attempt 1's pre-move full runs. The audit's earlier readings and P3a/P3b/P3c
  probe measurements are kept above as the record the ruling was made from. The destructive branch
  (`is_nil(oban_jobs_before)`) is unreachable on a migrated suite database, so no data could be
  dropped by the coupling; only time was lost, and the move removes the wait.

## Gates (all foreground; the timed runs strictly serial)

Every gate ran foreground, one run at a time, against the slice's disposable cluster. The runners
are `gdd/refactor/scratch/w3-17-*.sh` (attempt 1) and `w3-17-r2-*.sh` (attempt 2); each run's log,
`.events.json` and rc are on disk under `gdd/refactor/scratch/w3-17-{runs-pre,runs-post,gates}` and
`w3-17-r2-{runs,gates}`.

| # | gate | command | status |
| --- | --- | --- | --- |
| 1 | format | `mix format --check-formatted` | rc=0 on the final tree (attempt-2 rerun after the F-7/F-8 edits); fix-revision rerun rc=0 over the whole project |
| 2 | compile | `MIX_ENV=test mix compile --warnings-as-errors --force` | rc=0 on the final tree (attempt-2 rerun), `Compiling 95 files (.ex)`, `Generated symmetry_control app`, no warnings; fix-revision rerun rc=0, `Compiling 114 files (.ex)`, no warnings |
| 3 | seeded full runs | `SYMMETRY_EXUNIT_EVENTS_FILE=… mix test --seed <n> --exclude portal_assets --formatter ExUnit.CLIFormatter --formatter SymmetryControl.ExUnitJsonFormatter` | pre side (attempt 1, `w3-17-runs-pre/`): five seeds, rc 0, 0, 2, 0, 0 — seed 2 is F-2's two pre-existing flakes in untouched files. Post side (attempt-2 tree, seeds 0/1/2 per the conductor's amended scope): all rc=0, counts `1032 = 1007 + 21 + 4` (`w3-17-r2-runs/full-seed{0,1,2}`); name/state equality vs the matching pre seed in the Equality section below. Fix revision (`w3-17-r3-gates/full-seed0`): rc=0, `1181 = 1156 + 21 + 4` — the tree now carries origin/main's post-merge tests, so the fix-stage comparator is a reviewed-head (`f6b5b176`) export run reporting the same counts and an IDENTICAL exact comparison (table in "Fix-stage verification"; the comparator change is explained in the Equality section) |
| 4 | per converted file | `mix test <file> --seed 0 --repeat-until-failure 25` and `--seed <0..4>` | attempt-2 sweep (the 25-rep logs live in `w3-17-gates/g4r2-*`: the seven small files, 25 reps each **rc=0**, 40-334 s; `g4r2-big-goal_external_wait_migration_test-*` 25 reps **rc=0** (357 s); `g4r2-big-add_oban_jobs_table_test-*` 25 reps **rc=0** (143 s)), while `w3-17-r2-gates/` holds the `goal_0006` **10-rep** rc=0 (2430 s, `58 passed` per repetition) — the citation is corrected here per review SPEC-A02; the deviation from 25 is recorded below. Fix-revision rerun: all nine 25-rep sweeps rc=0, 31-259 s each (table in "Fix-stage verification") |
| 5 | the set together | `mix test <ten paths> --seed <0..4>` and `--seed 0 --repeat-until-failure N` | attempt-2: seeds 0..4 all **rc=0** (138-236 s, `g5r2-set-seed*`); repeat loop **5** instead of 10: rc=0, 833 s, `78 passed` per repetition (`g5r2-set-seed0-repeat5`) — deviation recorded below. Fix-revision reruns: `w3-17-r3-gates/set-seed{0..4}` all rc=0 (193-289 s, `78 passed` each), `set-seed0-repeat5` rc=0 (1294 s, `78 passed` per repetition) |
| 6 | static isolation audit + registry equality | the greps above, `w3-17-r2-migrator-sources.py`, `w3-17-r2-registry-check.exs` | rc=1 / rc=1 (no matches); 53 sites: 43 literal, 7 helper, 3 helper+literal, **0 unclassified**; registry `43/43, version_sets_equal=true, unique_pairs=true, all_loaded=true`. Fix-revision rerun with the guard-unwrapping classifier: `sites=53 wrapped=53`, same inner classes, rc=0; registry rerun identical |
| 7 | assertion census + diff classification | `w3-17-r2-census.py <base> <cand>`, diff classes above | **IDENTICAL**, rc=0: 377 assert-family lines and 78 test names on both sides, the F-8 union included, the F-7 reader byte-identical; fix-revision rerun identical (see the census section) |
| 8 | probes | P1/P2/P3a-c/P5/P6 (attempt 1) + P4c (F-7 red/green on the final tree) + P7 (F-8 pairing) | recorded; the F-7 revert restored the tree to `RESTORE PASS` over the 15-file snapshot |
| 9 | wall table + CI oracle + async-phase attribution | `w3-17-r2-table.py`, `w3-17-modules.py`, `w3-17-oban-durations.py` | the Wall time section: three final-tree seeds, the five same-workload A/B runs (corrected per SPEC-A01), the Oban durations, and the CI expectation (the CI number itself needs a pushed PR, forbidden here) |
| 10 | diff hygiene | `git diff --check`; the program's profile-path scan over the diff and the new files | rc=0; 0 matches. Fix-revision rerun: the committed branch diff's and the uncommitted fix diff's added lines hold no profile-name path and no drive-letter, /mnt, /tmp or /home prefix (0 matches each, the scans exit 1); the plan record itself matches none. `docs/TODO.md`'s pre-existing temporary-directory strings (an earlier entry quoting another slice's Go test output) are verbatim quotes and are left as they are |

Recorded deviations, all with their reason:

* **post-side seeds 5 → 3** (conductor ruling, binding): the foreground-only scope plus the
  attempt-1 background job's failure mode; the pre side keeps five seeds for the equality check and
  the same-workload A/B bounds the host movement.
* **gate 4 `goal_0006`: 25 → 10 repetitions**: one repetition costs ~243 s wall (154.7 s of tests +
  ~88 s of boot: registry compile + the alias' `ecto.create`/`ecto.migrate`), so 25 reps ≈ 100 min;
  the file's 58 tests already ran in every timed run of this window's record (three full runs, five
  A/B runs, two smokes) and once per repetition here, and the set-level repeat
  covers it inside the ten-module overlap. The loop stopped on no failure.
* **gate 5 repeat 10 → 5**: same reason at set scale (one repetition ~150 s + boot); attempt 1's
  `p2` probe already ran the set `--repeat-until-failure 5` green (1151 s) on the review-hardened
  revision.
* Both repeat-count deviations are **conductor-accepted** by the review-stage ruling #1 (recorded in
  the rulings section above) against the compensating evidence: five pre-side seeds, three
  final-tree seeds, the five converted-set seeds, and CI's `coverage-control` running the converted
  suite on every PR.
* **the A/B was run five times, in both orders** (corrected per review SPEC-A01): once per side as
  ordered in the conductor's scope (`ab-pre-nine-seed0` then `ab-post-nine-seed0`, the nine-path
  pair), once more at ten paths (`ab-post-ten-seed0`), then the reverse-order pair
  (`ab2-post-ten-seed0` then `ab2-pre-nine-seed0`). The first ordered pair is path-identical and
  showed the post side 16 s wall below the pre side, while the ten-path post run sat 34 s above the
  nine-path pre run and the reversed pair put the pre side 16 s above the preceding post run — the
  direction is not stable, which is what makes the report's claim honest about the variance.

`--only` was not usable for gate 4 — the nine files carry no tags, so the file path is the selector
(the contract's own note). `mix test --repeat-until-failure N` stopped at the first failure in the
one run that went red (F-1) and reported the repeat index.

## Fix-stage verification (review corrections)

Every guard, branch or public entry this slice adds or relies on has a mutant row: the mutant is the
guard removed (or, for the teardown, the pre-fix shape restored), red/green are observed on this
revision's tree, and the tree was restored from a sha256 snapshot between rows
(`gdd/refactor/scratch/w3-17-r3-snapshot.sh check` → `CHECK PASS` over the 12 harness/test files
after every probe round). Recipes and logs live under `gdd/refactor/scratch/w3-17-r3-probes/` and
`gdd/refactor/scratch/w3-17-r3-gates/`/`w3-17-r3-runs/` (the reviewed-head export and its wrapper
live under `w3-17-r3-head/` and `w3-17-r3-head-mix.sh`): the guard rows ran as
`mix run --no-start w3-17-r3-probes/guards.exs <mode>` (compiled test support, no app start, no
database) and the test-level rows as `mix test <file> --seed 0`.

| row | guard / branch | mutant | observed mutant (red) | restored (green) |
| --- | --- | --- | --- | --- |
| X1 | `on_exit` context facts (witness) | – | `on_exit` callbacks run in reverse registration order (`first`/`second` log), and the suite `Repo` cannot run there: the callback got `cannot find ownership process … (ExUnit.OnExitHandler) … using mode :manual` | n/a (witness for the #5 shape) |
| X2 | linked-process lifetime at test end (witness) | – | ExUnit terminates the test process with `:shutdown` (`{:watcher_test_down, :shutdown}`), so a process linked to it is already dead when `on_exit` runs (`sleeper_alive: false`) | n/a (witness for the #5 shape) |
| M1 | `MigrationModules.verified_pairs!/1` (version<->file binding) | guard body dropped; one literal tampered (`…902_000_000` → `…902_000_099`) in a test's own list | `add_task_command_ownership_test.exs` **passes** (1 passed) while recording a version no file carries | guard back, same tampered literal: **red** — `CreateOrchestrationTables is recorded at version 20260902000099 but its migration file carries 20260902000000`; literal back: 1 passed |
| M2 | `with_schema/2` no longer runs a teardown call over the test's own error (#5) | the old nested `after GenServer.stop(repo, :normal, 30_000)` restored; probe body stops its own Repo and then `assert false` | the report is the stop's exit (`exited in: GenServer.stop(…) ** (EXIT) no process …`); the assertion's message is lost — the review's masking scenario reproduced | delivered shape: the report is the body's own `assert false` message |
| M3 | `MigrationModules.base_migrations/0` shared definition (#6) | guard body dropped; one shared version drifted | `add_oban_jobs_table_test.exs` **passes** (2 passed) on a base version no file carries | guard back, same drift: **red** — both tests raise the mismatch on the shared pair; drift reverted: 2 passed |
| M4 | `schema_name/1` lowercase-identifier check | `unless` block removed | `schema_name("Bad-Name")` → `{:ok, 30}` (silently accepted into DDL) | delivered: raises `not a lowercase SQL identifier`; `schema_name("good_name")` → 31 bytes |
| M5 | `schema_name/1` 63-byte check | `if byte_size…` block removed | a 60-char base → `{:ok, 82}` (PostgreSQL would truncate silently) | delivered: raises `exceeds PostgreSQL's 63-byte identifier limit` |
| M6 | `compile_all!/0` empty-glob raise | raise removed, registry pointed at an empty scratch directory | `compile_all!` → `{:ok, [], 0}` | raise back on the same directory: `no migration files found in <probe dir>`; directory back: 43 pairs |
| M7 | `migrations/0` pre-boot raise | `:missing -> []` | `migrations/0` in a fresh VM → `{:ok, []}` | delivered: raises `compile_all!/0 has not run in this VM` |
| M8 | `version!/1` version-prefix check | `_ -> 0` | an unversioned probe file → `{:ok, [{0, W317Probe.NoVersionMigration}], 1}` | delivered: raises `does not start with a version` |
| M9 | `migration_module!/1` module check | `module || nil` | a versioned non-migration file → `{:ok, [{20260101000000, nil}], 1}` | delivered: raises `defines no Ecto migration` |
| M10 | the refusal test's marker-row `on_exit` cleanup (PRE-A01) | registration removed; a probe assertion forces the failure after the INSERT | the marker row survives: `rows_with_id=1`, `goal_0006_test_markers` 1 → 2 | delivered: `rows_with_id=0`, count back at the 1 pre-existing row; probe lines removed: 2 passed |
| P5-rerun | `create_schema!/1`'s `on_exit` drop (R4) | registration removed; a probe test kills itself inside `with_schema` | the schema survives the kill: `schemas_matching=1` (sample `w3_17_kill_probe_…`) | delivered: `schemas_matching=0`; the leaked probe schema dropped afterwards |
| P6-rerun | `compile_all!/0`'s `ignore_module_conflict` guard (R3) | guard removed; fresh `MIX_TEST_PARTITION` | 43 `redefining module` lines on the empty database | delivered: 0 on a fresh partition (1 passed) |

The two witness rows (X1/X2) are what selected the #5 fix's shape: the review's suggested "move the
stop into `on_exit`" cannot work (X2: the linked Repo is already terminating when `on_exit` runs) and
the suite Repo cannot substitute (X1: no sandbox ownership in the callback), so the delivered fix
removes the stop instead of moving it, and keeps the drop in `create_schema!/1`'s `on_exit`, which is
the step that must survive a kill. M2 shows the review's masking scenario is real and that the
delivered shape reports the body's error instead. Every other row is a fail-loud guard whose mutant
lets the tree pass on data the guard exists to refuse. M1 tampers the test's own version literal
rather than a migration filename (the guard compares both directions, and `priv/repo/migrations/**`
is outside the owned set); M3 drifts the shared base list, which is the drift #6 was about.

### Fix-stage gates (all foreground, strictly one run at a time, slice partition)

| gate | command | rc | counts |
| --- | --- | --- | --- |
| format | `mix format --check-formatted` | 0 | whole project |
| compile | `MIX_ENV=test mix compile --warnings-as-errors --force` | 0 | `Compiling 114 files (.ex)`, `Generated symmetry_control app`, no warnings |
| the ten-module set, seeds 0..4 | `SYMMETRY_EXUNIT_EVENTS_FILE=… mix test <ten paths> --seed <n> …` | 0, 0, 0, 0, 0 | `78 passed` each; walls 246 / 289 / 205 / 193 / 277 s (`w3-17-r3-gates/set-seed{0..4}`) |
| the set, repeated | `… --seed 0 --repeat-until-failure 5` | 0 | `78 passed` per repetition, 1294 s (`set-seed0-repeat5`) |
| full suite, seed 0 | `… mix test --seed 0 --exclude portal_assets …` | 0 | `1181 = 1156 passed + 21 skipped + 4 excluded`; run 334.1 s = 214.8 s async + 119.4 s sync, work 455.8 s (`full-seed0`) |
| reviewed-head baseline | `git archive f6b5b176` export, `compile --force`, same command on partition `rfw317head` | 0 | `1181 = 1156 passed + 21 skipped + 4 excluded`, wall 316 s (`head-full-seed0`) |
| equality, fix vs head | `w3-17-r3-equality-exact.py head-full-seed0.events.json full-seed0.events.json` | 0 | counts equal, `rows 1181 = 1181`, `only_left=0 only_right=0 state_diff=0`, `RESULT: IDENTICAL` |
| witnesses X1/X2 | `mix test <probe file> --seed 0` from the scratch probes | 0 / 0 | `x1-order.log` (LIFO order + the suite-Repo ownership error), `x2-link-death.log` (`:shutdown`, `sleeper_alive: false`) |
| census / classifier / registry | the three tools above | 0 / 0 / 0 | `IDENTICAL` 377/78; `sites=53 wrapped=53 … unclassified=0`; `43/43` + three `true`s |
| diff hygiene | `git diff --check`; the two added-line scans over the branch and fix diffs | 0 / 1 / 1 | no whitespace errors; 0 matches each |

The full-suite count moved from the attempt-2 record's `1032` to `1181` because the branch merged
origin/main after attempt 2's runs; the 149 extra tests live in files this slice never touches, and
the fix-stage name/state comparator is therefore the reviewed head: a `git archive f6b5b176` export
(recompiled with `compile --force`, its own partition `rfw317head`) ran the same command to
`1181 = 1156 + 21 + 4` in 316 s, and
`w3-17-r3-equality-exact.py head-full-seed0.events.json full-seed0.events.json` reports the counts
equal, `rows 1181 = 1181`, `only_left=0 only_right=0 state_diff=0`, `RESULT: IDENTICAL` — so the fix
revision renames, deletes, skips or retags nothing against the reviewed head either.

Per-file sweeps at the fix revision (`w3-17-r3-gates/`, `mix test <file> --seed 0
--repeat-until-failure 25`, every run rc=0 with 26 `Result:` lines and 0 `Failed:` lines, i.e. 25
green repetitions plus the summary):

| file | wall_s | file | wall_s |
| --- | --- | --- | --- |
| `add_goal_operator_version_test.exs` | 192 | `version_request_hashes_test.exs` | 39 |
| `add_history_lookup_indexes_test.exs` | 41 | `add_oban_jobs_table_test.exs` | 108 |
| `add_omo_harness_kind_migration_test.exs` | 244 | `goal_external_wait_migration_test.exs` | 259 |
| `add_retry_commands_test.exs` | 52 | `add_task_command_ownership_test.exs` | 31 |
| `add_work_item_creation_identity_test.exs` | 214 | | |

`goal_0006_migration_test.exs` at the conductor-accepted 10 repetitions: rc=0, 1633 s, 11 `Result:`
lines and 0 `Failed:` lines (`58 passed` per repetition, the summary line included).

## Wall time and the CI oracle

Five seeds per side, same command, same host, same partition, `--exclude portal_assets`. `run_s` is
ExUnit's own `run_us`; `wall_s` is the runner's measurement including the alias' `ecto.create` /
`ecto.migrate` steps. `work_s` is the sum of per-test durations; `nine_s` is that sum over the nine
converted files; `largest_s` is the largest single module's work (the async phase's floor once it is
converted).

Pre-slice, `66bc9dcb` (`w3-17-runs-pre/`):

| seed | wall_s | run_s | async_s | sync_s | work_s | nine_s | largest_s |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | 673.4 | 656.6 | 31.2 | 625.4 | 771.0 | 479.1 | 279.6 (Goal0006) |
| 1 | 614.3 | 605.2 | 36.9 | 568.3 | 743.0 | 343.5 | 254.6 (Goal0006) |
| 2 | 818.2 | 803.3 | 36.9 | 766.4 | 948.8 | 526.0 | 372.7 (Goal0006) — rc=2, F-2 |
| 3 | 690.0 | 684.7 | 28.4 | 656.3 | 756.3 | 469.2 | 359.2 (Goal0006) |
| 4 | 525.7 | 516.1 | 35.9 | 480.2 | 632.7 | 326.9 | 238.5 (Goal0006) |
| median (green seeds) | 652.2 | — | — | — | — | 469.2 | 279.6 |

Pre-side facts the conversion has to move: the nine files' work is 326.9-526.0 s per seed (62.1 % of
the suite's work in seed 0), and the pre-existing async phase carries only 31.2-36.9 s of *phase*
(its 33 modules' work is 103.2-184.7 s, largest module 12.8-22.2 s, so that phase is bound by
`ContractFixtureManifestTest`/`ProtocolControllerTest`/`TaskGoalGuardControllerTest`, not by
`max_cases`).

Post-slice, final tree (`w3-17-r2-runs/`): three seeds — the conductor's amended measurement scope —
same command, same host, same partition, all `rc=0`:

| seed | wall_s | run_s | async_s | sync_s | work_s | mig10_s | largest_s |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | 348 | 342.6 | 219.5 | 123.1 | 537.9 | 307.3 | 202.0 (Goal0006) |
| 1 | 342 | 335.9 | 198.1 | 137.8 | 536.9 | 279.2 | 178.1 (Goal0006) |
| 2 | 305 | 294.0 | 173.9 | 120.1 | 443.6 | 245.8 | 154.9 (Goal0006) |

Counts are `1032 = 1007 passed + 21 skipped + 4 excluded` on every run, identical to the baseline.
`mig10_s` is the ten migration modules' work (nine + the F-8 module); `largest_s` is the async
phase's floor, and the async phase's wall tracks it within ~20 s (`async_s − largest_s` =
17.5 / 19.8 / 19.0 s): the phase ends when the largest module does.

Paired per-seed movement (`pre` from `w3-17-runs-pre/`, `post` above):

| seed | run_s pre → post | sync_s pre → post | async_s pre → post |
| --- | --- | --- | --- |
| 0 | 656.6 → 342.7 (−313.9, −47.8 %) | 625.4 → 123.1 (−502.3) | 31.2 → 219.5 (+188.3) |
| 1 | 605.2 → 336.0 (−269.2, −44.5 %) | 568.3 → 137.8 (−430.5) | 36.9 → 198.1 (+161.2) |
| 2 | 803.3 → 294.0 (−509.3, −63.4 %) | 766.4 → 120.1 (−646.3) | 36.9 → 173.9 (+137.0) |

The pre-side level was measured while two foreign BEAMs were resident (attempt 1's own record); these
local numbers are report-only (lesson F4) and their absolute level moves with host state — the four
same-workload A/B runs below (five, after the SPEC-A01 correction) bound that movement. Attempt-1's
post-side rows
(`w3-17-runs-post/`, pre-review revision; `w3-17-runs-post-prereview/`'s seeds 0-3 at
538.0 / 574.5 / 715.5 / 650.3 s, rc 2, 2, 2, 0) are kept as F-7's rate sample and are superseded by
the three rows above.

Paired A/B on the same host and partition, **run in both orders** because the level drifts inside a
ten-minute window. Five runs exist, corrected here per review SPEC-A01 (the earlier table printed
four and paired non-adjacent runs): the conductor-ordered pair `w3-17-r2-ab.sh` ran (pre nine paths,
then the same nine paths post-conversion), one more post run at ten paths, then the reverse-order
pair `w3-17-r2-ab2.sh` ran (ten-path post, then nine-path pre). Pre = the nine serial files (78
tests); post-nine = the nine converted paths (76 tests — the two moved Oban tests are not on the nine
paths); post-ten = the nine converted paths plus the F-8 `async: false` module (78 tests).

| run (time order, label) | side (paths) | tests | wall_s | run_s | async_s | sync_s | work_s | largest_s |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 `ab-pre-nine-seed0` | pre (nine) | 78 | 178 | 175.1 | – (all serial) | 175.1 | 172.1 | 123.9 |
| 2 `ab-post-nine-seed0` | post (nine) | 76 | 162 | 157.4 | 157.4 | 0.0 | 199.8 | 144.3 |
| 3 `ab-post-ten-seed0` | post (ten) | 78 | 218 | 209.3 | 204.9 | 4.4 | 270.3 | 188.3 |
| 4 `ab2-post-ten-seed0` | post (ten) | 78 | 224 | 216.9 | 210.3 | 6.6 | 285.6 | 195.3 |
| 5 `ab2-pre-nine-seed0` | pre (nine) | 78 | 240 | 227.3 | – (all serial) | 227.3 | 223.6 | 136.9 |

Paired differences, each pair named by its paths: the path-identical adjacent ordered pair (rows
1→2, nine paths each) is `−17.7 s` run / `−16 s` wall on the post side; the 78-vs-78 pair is rows
1→3 (nine-path pre against the later ten-path post), `+34.2 s` run / `+40 s` wall, and it mixes
workloads — the ten-path post leg also runs the F-8 module serially; the reversed pair (rows 4→5)
puts the pre side `+10.4 s` run / `+16 s` wall above the preceding post run. The direction is not
stable, and the same table shows why: the largest module's own work (60 of the pre side's tests, 58
of the post side's) moves 123.9 → 144.3 → 188.3 → 195.3 → 136.9 s across the five runs, and the post
side inflates every module's own work by running nine heavy-DDL modules concurrently — the nine
converted modules' summed work is 172.1 / 223.6 s in the two serial runs against 199.8 s (the
nine-path run) and 264.1 / 279.4 s in the two ten-path runs (net of the moved module's 6.15 s). At
seed 0 on this host the structural gain (the serial phase loses the eight non-largest modules' work,
48.2-86.7 s in the two pre-side runs) sits inside that variance, so no single ordered pair
adjudicates the ≤65 s target locally; the CI number is the oracle (report-only locally by lesson F4).

The robust local facts are the phase structure and the attribution, not the wall pair:

* the post side's async phase is the largest module plus a 17.5-19.8 s tail in the attempt-2 full
  runs (60.2 s on the fix revision's merged corpus), and the sync phase is 120.1-137.8 s against
  the baseline's 568.3-766.4 s;
* the two Oban tests' full-run durations after the F-8 move are 2.29 / 2.81 s (seed 0),
  2.22 / 2.28 s (seed 1) and 1.67 / 1.72 s (seed 2), against 9.9 / 15.9 s and 6.3 / 36.3 s in
  attempt 1's pre-move full runs (`w3-17-oban-durations.py` over the event streams).

CI oracle: main's `control` job spans 73.0-159.4 s sync against 14.7-22.9 s async, and W3-14's
branch measured 140.2 s = 18.1 s async + 122.0 s sync on the 4-scheduler runner
(`docs/TODO.md:1146-1150`). The post-change CI number needs a pushed PR, which this dispatch
forbids; the derived expectation from these streams is that the nine files leave the serial phase,
the async phase becomes `Goal0006MigrationTest`-bound (on this 16-scheduler host it already is:
`async_s − largest_s` = 17.5-19.8 s across the three full runs), the two Oban tests stay serial at
1.7-2.8 s, and the CI movement is the moved modules' serial sum minus the largest of them — not
140 s → 65 s. The local release is 48.2-86.7 s at seed 0 (the eight non-largest modules' work); on
the 4-scheduler runner the same fraction of the nine files' serial share would be released, which
cannot be derived without the CI event stream.

Fix-revision full run (review corrections), same command and partition, seed 0 (`full-seed0` in
`w3-17-r3-gates/`): rc=0, `1181 = 1156 passed + 21 skipped + 4 excluded` — the merged tree's newer
tests, see the Equality section — run 334.1 s = 214.8 s async + 119.4 s sync, per-test work 455.8 s,
`Goal0006MigrationTest`'s own work 154.6 s (async tail 60.2 s). The nine converted modules' summed
work is 247.8 s against the attempt-2 seeds' 245.8-307.3 s and the pre-side 326.9-526.0 s; the sync
phase stays at the post-side level (119.4 s against 120.1-137.8 s), so the phase structure the
conversion produced reproduces on the merged corpus (the run's host carried 1 foreign BEAM, one of
the quieter windows).

## Name/state equality across the pre/post streams

`w3-17-r2-equality.py` compares each final-tree seed against the matching pre-side stream
(`w3-17-runs-pre/10-seed<n>.events.json`): suite counts, the sorted `(module, name)` set and every
test's state. The F-8 move remaps the two moved tests' rows from
`SymmetryControl.Migrations.AddObanJobsTableTest` onto the donor module before comparing, and
requires exactly two remapped rows — a third moved test would fail the check.

That comparison holds for the attempt-2 tree, whose main content matched the pre-side baseline
(`1032` names on both sides; seeds 0/1/2 `IDENTICAL`, the two state differences being F-2/F-3's
pre-existing flakes). At the fix revision the comparator changes, because the branch merged
origin/main after attempt 2 and the delivered tree legitimately carries 149 tests the pre-side
baseline never had: the fix-revision stream is compared against a `git archive f6b5b176` export of
the reviewed head (same origin/main content, the slice head without the fix), recompiled with
`compile --force` and run with the same command on its own partition (`rfw317head`). The result is
the fix-stage gate table's equality row: `w3-17-r3-equality-exact.py` reports the counts equal and
`only_left=0 only_right=0 state_diff=0`, `RESULT: IDENTICAL` — no rename, delete, skip or retag is
introduced by the fix revision, and no test changed state between the two trees.

| seed | counts | rows | only_pre | only_post | state_diff | result |
| --- | --- | --- | --- | --- | --- | --- |
| 0 | 1032 = 1032 | 1032 = 1032 | 0 | 0 | 0 | **IDENTICAL** |
| 1 | 1032 = 1032 | 1032 = 1032 | 0 | 0 | 0 | **IDENTICAL** |
| 2 | pre 1005 passed + **2 failed** vs post 1007 passed | 1032 = 1032 | 0 | 0 | 2 | names IDENTICAL; the two differing states are the pre-side flake failures of F-2/F-3 (`DaemonChannelTest`'s hint window and `PortalIntegrationsTest`'s stale-resource case, both `async: false`, untouched), `failed` → `passed` |

No test name is missing or added on either side; the only state movement is the pre side's two
recorded flake failures passing in the final tree's run. No rename, delete, skip or retag: the tag
scan in the diff-classification section is zero for every tag class.

## Findings (flakes and incidents; full log in `gdd/refactor/scratch/w3-17-findings.md`)

| id | run | failure | file touched by this slice? | action |
| --- | --- | --- | --- | --- |
| F-1 | gate-4 25-repeat of `add_work_item_creation_identity_test.exs`, repeat 13 | `tcp recv: closed` in the `CREATE SCHEMA` statement while the pre-slice suite and a third BEAM shared the host; the cluster's checkpoints were writing for 270 s and 218 s in that window | the harness is (the DDL Repo); the test file only got the declaration flip | recorded; one same-seed re-run alone on a quiet host → **rc=0, 25/25 green** |
| F-2 | pre-slice seed 2 | two untouched `async: false` files: `DaemonChannelTest`'s hint test (`no matching message after 100ms`, the window W3-14 already recorded) and `PortalIntegrationsTest` "project sync preserves stale resource conflicts" | no (pre-slice tree) | recorded; one same-seed re-run taken (F-3) |
| F-3 | pre-slice seed-2 re-run | different file, `ProviderActionControllerTest` `:DOWN` window, `no matching message after 2000ms`, `async: false` and untouched | no (pre-slice tree) | recorded; no further re-run (the rule allows one; repeating until green would hide it) |
| F-4 | post-side seed 1, revision before the queue-tolerance line | 4 × `connection not available and request was dropped from queue after 4000ms` at the start of migration tests (the fresh private pools could not establish a connection fast enough while two foreign BEAMs oversubscribed the CPU) + 1 × the F-2 `DaemonChannelTest` window | the harness is (the fresh pools); no test file since its start-of-test `with_schema` call | recorded; `queue_target: 5_000, queue_interval: 10_000` added to `private_repo_config/0`, and the whole post-side evidence re-run on the hardened revision (the seed-1 re-run the rule allows is the hardened seed 1) |
| F-5 | post-side seed 0, queue-tolerance revision | `CREATE SCHEMA` still in flight at DBConnection's 15 s client budget in `Goal0006MigrationTest`, and one instance of what F-7 then identified while migration tests ran `DROP SCHEMA … CASCADE` concurrently (per-test work 55 % above the same seed's earlier value; two foreign BEAMs) | the timeout is the harness's; the catalog race belongs to the unkeyed reader (F-7) and is only reachable because the DDL moved into the async phase | recorded; `timeout: 30_000` added to the private pools; the catalog reader itself is escalated under F-7 |
| F-6 | post-side seeds 0 and 1 on the final revision | two load-sensitive assertions in untouched, non-migration files: seed 0 `integrations_test.exs:1081` `assert_receive {:integration_provider_waiting, …}` — "no matching message after 100ms", W3-14's F-2/F-3 class; seed 1 `provider_action_controller_test.exs:1925` `assert Process.read_timer(timeout_ref) == false` (left `59848`, the 60 s timer still armed) — with three and two foreign BEAMs resident | no | recorded; not hidden |
| F-7 | post-side seeds 1 and 2 on the final revision, plus the queue-tolerant revision's seed 0 (observed there as `ERROR XX000`); **absent in all six baseline runs** (18 executions of the same test passed) | `NativeHarnessKindBindingTest.constraint_kinds!/1` (`native_harness_kind_binding_test.exs:214-220`, `async: true`, W3-14's file, outside this slice's owned set) asserts one row from `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1` and receives **two identical definitions** while a concurrent migration test's private schema carries the same `runtimes_harness_kind_check` name; the `ERROR XX000 could not open relation with OID` variant is the same unfiltered catalog access racing `DROP SCHEMA … CASCADE` | no — the nine are keyed (their queries carry `conrelid = '…'::regclass` or a `pg_namespace.nspname = current_schema()` join); the conversion only makes the unkeyed reader reachable | **resolved by the F-7 ruling, one change in the reader file**: `AND connamespace = current_schema()::regnamespace` on the query (keeps both call sites at `:36`/`:37` working). Red/green pair on the final tree in the Probes section (P4c); see also the F-7 section below |
| F-8 | found by the independent review (`ReviewSol`), not by a failing run | `goal_0006_migration_test.exs`'s two Oban tests now run while other `async: true` tests hold `ROW EXCLUSIVE` on the shared `public.oban_jobs`: `task_goal_guard_controller_test.exs:2` (`async: true`) → `admitted_goal_task/0` → `command_current(…, "admit_task", …)` → `Goals.command/4` → `Oban.insert!` of a `WakeupWorker` (`goals.ex:4491-4495`) inside the test's sandbox transaction; `AddObanJobsTable`'s down path takes `LOCK TABLE public.oban_jobs IN SHARE MODE`, which waits for those holders | the migration file is in the owned set, but every test-side remedy changes test semantics (a tag or a serialize mechanism) | recorded with its measurement plan (probes P3a/P3b/P3c) and escalated with both readings of the contract (answer §12.3); exposure did not materialize in the five post-side runs (0 `oban_jobs`/`lock_timeout`/`canceling statement` lines). **Resolved by the F-8 ruling**: the two tests moved to `add_oban_jobs_table_test.exs` (`async: false`) with names byte-identical; the move's evidence is in the Oban audit's resolution |
| F-9 | attempt-2 A/B (five same-workload runs, both orders — count corrected per SPEC-A01) | converting the nine files puts nine heavy-DDL modules in one async phase: the same nine modules' summed work is 172.1 / 223.6 s in the two serial runs against 199.8 s (the nine-path run) and 264.1 / 279.4 s (the two ten-path runs, net of the moved module's 6.15 s), i.e. each module's own work inflates under the concurrency; the largest module's own work moves 123.9 → 144.3 → 188.3 → 195.3 → 136.9 s across the five runs | the concurrency is the deliverable's own shape; the inflation is a host/database property (catalog and I/O contention between nine concurrent 43-migration runs, bounded by the harness's 30 s budgets) | recorded; it is why the local A/B cannot adjudicate the ≤65 s target and why the CI number is named as the oracle |
| F-10 | same five runs (order control) | the path-identical ordered pair showed the post side `−16 s` wall (nine paths each) while the nine-pre→ten-post pair showed `+40 s` and the reversed pair put the pre side `+16 s` wall above the preceding post run; the level drifted inside fifteen minutes (pre 175.1 → 227.3 s, post 157.4 → 209.3 → 216.9 s) | no | recorded; the reversed pair is the control and the report states the variance instead of one number |

F-7 and F-8 were resolved by the conductor's rulings of 2026-11-22 (see the attempt-2 rulings
section): the reader's predicate is schema-scoped and the two Oban tests run in the serial phase;
both were verified on the final tree (P4c, P7) and by the three seeded full runs and the gate sweeps
above. F-6's load-sensitive windows in untouched files were not observed in the three final-tree
runs.

F-2/F-3 mean the pre side has no five-green-seed window on this host; both red seeds are reported and
the equality comparison names them. F-1's single re-run is green, so it is recorded as an incident,
not as a reproducible defect of the delivered harness. F-4 changed the deliverable: post-side
streams from before the queue-tolerance line (`w3-17-runs-post/10-seed0.*` green and
`10-seed1.*` red, both overwritten by the re-run) are reported in the findings log only.

### F-7 — the slice's one blocking interactor

Converting the nine files moves `CREATE SCHEMA` + 43 migrations + `DROP SCHEMA … CASCADE` from the
serial phase into the async phase. An `async: true` file from W3-14 reads the catalog by constraint
*name* only; every private schema the migration tests migrate adds a second match for the same name,
so its `assert %{rows: [[definition]]} =` fails with two identical rows. Measured rate on this host:
the final revision's seeds 1 and 2 plus the queue-tolerant revision's seed 0 — 3 of the 5 full-suite
runs on converted revisions, against 0 of 6 baseline runs (18 executions of that test).
The nine converted files cannot cause or suffer this: every catalog query they contain is keyed
(`add_goal_operator_version_test.exs:124-127`, `add_retry_commands_test.exs:47-55`,
`goal_0006_migration_test.exs:999-1004` and `:3421-3436`, `goal_external_wait_migration_test.exs:65-69`,
`:108-112`, `:139-143`). The one-line amendment is
`AND connamespace = current_schema()::regnamespace` on the reader's query — the same
`current_schema()` keying `add_retry_commands_test.exs:47-55` uses, and it keeps the helper's
`harness_sessions` call site working (a `conrelid` key would not). The probe P4 below reproduces the
collision.

**Resolution (attempt 2, conductor ruling F-7).** The reader file was added to the owned set for
exactly this one change; the predicate now reads

    WHERE conname = $1 AND connamespace = current_schema()::regnamespace

at `native_harness_kind_binding_test.exs:219-222`. Red on the final tree (probe P4b re-run,
`w3-17-r2-probes/p4b-unkeyed.*`): with a peer schema carrying the same constraint names present and
the predicate removed, the read returns `num_rows: 2` on two identical `CHECK (…)` definitions —
rc=2, `2/3 passed`. Green with the predicate, same construction, same peer schema present
(`w3-17-r2-probes/p4b-fixed.*`): rc=0, `3 passed`. The concurrent observation that drove the ruling
is attempt 1's: 2 of the 4 final-revision post-side runs and the queue-tolerant revision's seed 0 —
3 of 5 converted-revision full runs — died in this test
(`w3-17-runs-post-prereview/10-seed1.log:7-25`, `10-seed2.log:9-27`), against 0 of 6 baseline runs
in 18 executions. The final tree's three seeded full runs are the green-under-concurrency half.

## Independent review of the delivered tree (two reviewers, dispatched in parallel)

`ReviewSol` (ghc/gpt-6.2-sol, high effort) and `ReviewTencent`
(tencent-litellm/deepseek-v4.1-flash-fast, max effort) reviewed the uncommitted tree read-only, with
`mix`/`psql`/BEAM commands off-limits so the measurement runs stayed valid. Both found the F-7
blocker independently, with the same mechanism and the same fix family. Every other finding was
re-verified in the tree before being accepted or rejected:

| # | finding (source) | verified how | disposition |
| --- | --- | --- | --- |
| R1 | F-7: the async flip makes the unkeyed reader fail (both) | mechanism read in the tree; database checked afterwards — the name-only predicate returned exactly 2 rows, both in `public` | escalated; one-line amendment (answer §12.4) |
| R2 | Oban audit premise false: `task_goal_guard_controller_test.exs` (`async: true`) admits a task → `Oban.insert!` inside its sandbox transaction (ReviewSol; ReviewTencent asserted the opposite and missed this producer) | every link read: `test_fixtures.ex:14-23`, `goals.ex:293-307`, `goals.ex:4491-4495`, `config/test.exs:51-55` (no `prefix:`), `data_case.ex:50` (`shared: not tags[:async]`) | recorded as F-8 with probes P3a/P3b/P3c and both readings of the contract (answer §12.3) |
| R3 | `compile_all!/0` recompiles alias-loaded modules, so a *fresh* database prints ~43 redefinition warnings (both; also a plan/code divergence) | mechanism read in `mix.exs:82` + `migrator.ex:733-741`; this host's logs showed 0 warnings only because its database was already migrated | fixed: compile under `ignore_module_conflict` (previous value restored); probe P6 shows 0 warnings on a fresh partition |
| R4 | a killed/timed-out test leaks its private schema (ExUnit `:kill` skips every `after`), and each leak makes F-7 permanent (ReviewTencent) | confirmed, and a stale pre-fix leak (`history_lookup_indexes_2824`) was found in this host's test database | fixed: `create_schema!/1` registers an ExUnit `on_exit` drop; probe P5 shows the leak without the fix and no new leak with it |
| R5 | `schema_name/1` was node-unique only, dropping the cross-VM uniqueness the old `goal_0006` name had (ReviewTencent) | `System.unique_integer([:positive])` starts low in every VM | fixed: `_<unique_integer>_<system_time(:microsecond)>` suffix, worst case 60 bytes ≤ 63 |
| R6 | unvalidated base interpolated into DDL; `create_schema!` outside the `try`; the two manual call sites stop their Repo before dropping the schema, so a raising stop skips the drop (both) | latent, no live caller | fixed for the first two: lowercase-identifier check on the base, create moved inside the `try` (`DROP … IF EXISTS` makes the extra drop harmless). Restated at the fix revision (review SPEC-A03): `create_schema!/1` registers its ExUnit `on_exit` drop before it runs the DDL, so a raising `GenServer.stop` at the two manual call sites no longer skips the drop — the runner still drops the schema (probe P5-rerun: a killed test leaves `schemas_matching=0`, and with the registration removed it leaves one). The surviving residual is the narrower one: an `on_exit` drop that itself fails or is interrupted, with nothing sweeping leaks and no test asserting their absence |
| R7 | `pool_size: 1` plus the inherited `migration_lock: :pg_advisory_lock` raises "pool size less than 2" for any caller that forgets `migration_lock: false` (ReviewTencent) | read in `postgres.ex:305-313`; all 51 sites pass the option today | fixed: `migration_lock: false` in `private_repo_config/0` |
| R8 | registry glob is top-level-only while Ecto recurses; an empty glob is silent (ReviewSol) | read in `migrator.ex:660-680` | fixed: `**/*.exs` and a boot raise when the glob is empty |
| R9 | `migrations/0`'s bare `:persistent_term` error; `GenServer.stop/1`'s 5 s default; the queue comment named the wrong defaults (both) | read | fixed: named error, `GenServer.stop(repo, :normal, 30_000)` at both sites, comment names the observed 4000 ms message |
| R10 | three fresh connections per test instead of the base's one; suggested one shared DDL Repo per VM (ReviewTencent) | churn is real; the peak is measured in the envelope section (52 nominal ≤ 100) | **rejected with reasons**: a small shared DDL pool would serialize DDL that F-1/F-4/F-5 show can take tens of seconds under host saturation — the failure class the per-statement design exists to avoid |
| R11 | `version!/1` raises where Ecto silently skips an unparseable file (both) | read | kept deliberately: Change 1 asks for drift caught before the first test |

Both reviewers separately confirmed: the nine files' assertion and test-name multisets are unchanged;
every `{version, module}` reference resolves to the right migration module (76 checked by one
reviewer, 43 registry pairs by the other); every catalog read inside the nine is keyed; no test-time
`Code.compile_file`, path-source migrator call or `Sandbox.unboxed_run` remains; private Repos are
genuinely private (`name: nil`) and `put_dynamic_repo` restores exactly; a missing `search_path`
schema fails loudly instead of falling back to `public`; and no database-global advisory lock is
reachable from the migration path.

## Probes (contract Change 4)

Recipes and the revert procedure: `gdd/refactor/scratch/w3-17-probes.md`; every probe edit was
temporary and the delivered tree was restored from the sha256-verified snapshot
(`gdd/refactor/scratch/w3-17-restore.sh`, `RESTORE PASS: all 13 files match the snapshot sums`)
before the next step.

**P1 — two converted files on one fixed schema name.** `schema_name/1` returned its base unchanged,
and `add_retry_commands_test.exs` + `version_request_hashes_test.exs` were both pointed at
`"w3_17_probe_shared"`.

| side | command | outcome |
| --- | --- | --- |
| `async: true` (delivered declaration) | `mix test <add_retry_commands_test.exs> <version_request_hashes_test.exs> --seed 0 --repeat-until-failure 10` | **RED**, rc=2, `2/4 passed`: `** (Postgrex.Error) ERROR 42P06 (duplicate_schema) schema "w3_17_probe_shared" already exists`, raised from `MigrationCase.with_schema/2` → `run_schema_ddl!/1`, attributed to the peer module holding the name |
| `async: false` control (same edits plus the declaration flip) | same command | **GREEN**, rc=0, `4 passed`, `Finished in 6.2 seconds (0.00s async, 6.2s sync)`, zero `42P06` occurrences |
| reverted tree | `w3-17-restore.sh` then the file runs of gate 4 | `RESTORE PASS`; green |

Re-run on the review-hardened tree (`w3-17-probes/p1-async.log`, `p1-sync.log`, `p1-reverted.log`):
the pair under `async: true` is **RED**, rc=2, `0/4 passed` — two tests on
`ERROR 42P06 (duplicate_schema) schema "w3_17_probe_shared" already exists`, the other two on
`ERROR 42P01 (undefined_table) relation "schema_migrations" does not exist`, i.e. the peer's drop of
the shared name — while the `async: false` control is **GREEN**, rc=0, 83 s, ten repetitions each
`4 passed`, and the reverted tree is green again (rc=0, `4 passed`).

The probe therefore shows the shared name *is* shared state: red under `async: true`, green when the
two modules cannot overlap. It is also the only probe construction that needed a red that the
delivered code cannot produce — the delivered `schema_name/1` suffix is what the probe removes.

**P4 — the F-7 collision, three forms (contract Change 4's P-class; review finding R1).** The
pair-run form holds a fully migrated schema open for 30 s in
`add_omo_harness_kind_migration_test.exs` while the unkeyed reader runs beside it; the deterministic
form seeds the same kind of schema *at boot*, before `ExUnit.start/1`, through the harness's own
path (private Repo with `search_path`, `Ecto.Migrator.run/4` over the registry).

| form | command | outcome |
| --- | --- | --- |
| pair run | `mix test <add_omo_harness_kind_migration_test.exs> test/symmetry_control/native_harness_kind_binding_test.exs --seed 0 --repeat-until-failure 3` (`w3-17-probe-run.sh p4`) | **GREEN**, rc=0, `7 passed` three times (232 s) — insensitive as a reproducer: the reader's three tests take ~2 s while the peer needs ~15 s to migrate, and every repetition restarts both modules |
| boot-seeded peer schema (`p4b` probe block in `test_helper.exs`, schema `w3_17_probe_preseed`) | `mix test test/symmetry_control/native_harness_kind_binding_test.exs --seed 0` | **RED**, rc=2, `2/3 passed`: `assert %{rows: [[definition]]}` matched a result holding **two identical** definitions — `CHECK (((harness_kind IS NULL) OR (harness_kind = ANY (ARRAY['codex'::text, …, 'omo'::text]))))` twice (`w3-17-probes/p4b-red.log`) |
| control (delivered `test_helper.exs`, seeded schema dropped) | same command | **GREEN**, rc=0, `3 passed` (0.6 s) |

The pair form's insensitivity is itself the measurement that explains the full-suite rate: the
collision needs a private schema to exist *while* the reader queries, which in a full suite happens
for minutes at a time (9 migration modules' schemas overlapping the reader's single slot) and in a
two-module run for ~2 s at a time. The 3-in-5 full-suite rate and this DB check are the concurrency
evidence:

```
SELECT nspname, conname FROM pg_constraint JOIN pg_namespace … WHERE conname IN
  ('runtimes_harness_kind_check','harness_sessions_harness_kind_check')
→ rows=2, both in `public`   (after the seed runs: no leftover schema carries the names, so the
                              observed failures came from live concurrent schemas)
```

**P4c — the F-7 fix, red and green on the final tree (attempt 2, conductor ruling F-7).** Same
construction as P4b: the boot block in `test_helper.exs` seeds `w3_17_probe_preseed` through the
harness's own path (private search-path Repo, `Ecto.Migrator.run/4` over the boot registry) before
`ExUnit.start/1`, so the peer schema with the same constraint names exists when the reader's real
tests query. The reader's query is the only thing that differs between the two runs.

| side | command | outcome |
| --- | --- | --- |
| predicate removed (probe edit) | `mix test test/symmetry_control/native_harness_kind_binding_test.exs --seed 0` | **RED**, rc=2, `2/3 passed`: `match (=) failed`, `num_rows: 2`, two identical `CHECK (((harness_kind IS NULL) OR (harness_kind = ANY (…))))` definitions (`w3-17-r2-probes/p4b-unkeyed.log:7-18`) |
| delivered predicate (`AND connamespace = current_schema()::regnamespace`) | same command, same seeded peer schema | **GREEN**, rc=0, `3 passed`, 0.4 s (`w3-17-r2-probes/p4b-fixed.log`) |
| reverted tree | `w3-17-r2-snapshot.sh restore` then `check` | `RESTORE PASS`, all 15 files match the snapshot sums; probe schema dropped, zero schemas match the probe prefix |

**P7 — F-8: the moved Oban module beside the real async producer (attempt 2, conductor ruling
F-8).** No probe edit: the delivered tree's `add_oban_jobs_table_test.exs` (`async: false`) next to
`task_goal_guard_controller_test.exs` (`async: true`, the file whose `Goals.command/4` path inserts
a `WakeupWorker` inside its sandbox transaction).

| command | outcome |
| --- | --- |
| `mix test test/symmetry_control_web/controllers/task_goal_guard_controller_test.exs test/symmetry_control/migrations/add_oban_jobs_table_test.exs --seed 0` | **GREEN**, rc=0, `4 passed`, `Finished in 7.6 seconds (2.1s async, 5.5s sync)` (`w3-17-r2-probes/f8-pairing.log`): the producer is in the async phase, the moved module in the serial phase. The two Oban tests take 2.66 s and 2.53 s (`f8-pairing.events.json`) against 9.9 s / 15.9 s (attempt 1 seed 1) and 6.3 s / 36.3 s (seed 2) while they were async |
| `mix test test/symmetry_control/migrations/add_oban_jobs_table_test.exs --seed 0` | **GREEN**, rc=0, `2 passed`, `7.5 s (0.00s async, 7.5s sync)` (`w3-17-r2-gates/smoke-obanfile.log`): the module runs in the serial phase |

**P5 — a killed test leaks its schema unless `on_exit` catches it (review finding R4).** The probe
adds `test "probe: a killed test leaves no private schema behind"` to
`add_history_lookup_indexes_test.exs` (`with_schema("w3_17_probe_kill", fn -> Process.exit(self(),
:kill) end)`) and counts schemas matching the prefix afterwards with
`w3-17-schema-count.exs` over a raw Postgrex connection.

| side | command | outcome |
| --- | --- | --- |
| `on_exit` registration removed (one-line probe edit) | `mix test add_history_lookup_indexes_test.exs --seed 0` | **RED**, rc=2, `1/2 passed`, and the schema survived: `schemas_matching=1 sample=["w3_17_probe_kill_11650_1795366081747774"]` — the `:kill` skips every `after` block, which is the leak |
| delivered code (the edit reverted, the leak dropped first) | same command | **RED**, rc=2, `1/2 passed` — the probe test still dies as intended — but `schemas_matching=0`: the ExUnit `on_exit` drop ran in the runner process after the kill |

**P6 — boot warnings on a fresh database (review finding R3).** A fresh `MIX_TEST_PARTITION` makes
`mix test`'s alias create and migrate a new database, so `ecto.migrate` loads all 43 migration
modules before `test_helper.exs` runs. Runs the same small file on a fresh partition and counts
`redefining module` lines, once with the delivered `ignore_module_conflict` guard and once with the
guard removed (one-line probe edit, reverted afterwards).

| side | command | outcome |
| --- | --- | --- |
| delivered code, fresh partition `rfw317fresh` | `MIX_TEST_PARTITION=rfw317fresh mix test add_history_lookup_indexes_test.exs --seed 0` | rc=0, `1 passed`, **0** `redefining module` lines |
| guard removed (probe edit), fresh partition `rfw317fresh2` | same command | rc=0, `1 passed`, **43** `redefining module` lines — one per migration module, e.g. `redefining module SymmetryControl.Repo.Migrations.CreateOrchestrationTables (current version defined in memory)` |

The pre-fix count is what CI would have printed on every job (its Postgres service starts empty);
the delivered count is 0 on a fresh database as well as on this host's pre-migrated one.

**P3 — the `public.oban_jobs` construction under concurrency (contract Change 4; finding F-8).** The
carrier is a converted file (`version_request_hashes_test.exs`) writing into the shared app table
while `goal_0006`'s two Oban tests run beside it.

| form | command | outcome |
| --- | --- | --- |
| P3a: a synthetic writer inside a converted file, one INSERT+DELETE cycle every 25 ms for 90 s | `mix test <version_request_hashes_test.exs> <goal_0006_migration_test.exs> --seed 0` (`w3-17-probe-run.sh p3a`) | **GREEN**, rc=0, `63 passed`, 196 s; the probe printed `probe_oban_writes slowest_statement_ms=117` — the writer did meet the peer's SHARE lock and paid ~0.1 s, three orders of magnitude inside the harness's 30 s budget |
| P3b attempt 1: the same write held in an open transaction on the test's *private pool* | same command | rc=2, `62/63 passed`: only the **probe itself** failed — DBConnection killed its connection after ~60 s (`connection is closed because of an error, disconnect or timeout`, client's location `Process.sleep/1`) — so the hold ended before goal_0006's Oban tests and no attributed red appeared. Recorded as a construction lesson: a checked-out connection cannot outlive its pool's client budget |
| P3b attempt 2: the hold moved to a raw Postgrex connection (no pool watchdog), 200 s | same command | **RED, precisely attributed**: rc=2, `62/63 passed`, 526 s — the probe itself passed (its `probe_oban_hold_started`/`released` pair spans exactly 200 s) and `goal_0006_migration_test.exs:1255` "refuses Oban rollback while an incomplete Goal worker remains queued" died with `** (ExUnit.TimeoutError) test timed out after 60000ms` — the migration's `LOCK TABLE public.oban_jobs IN SHARE MODE` waiting behind the held write. So the F-8 coupling can turn the refusal test red; the real producers (P3c) hold for a fraction of that |
| P3c: no edits — ExUnit's own async producer (`task_goal_guard_controller_test.exs`) beside goal_0006 | `mix test test/symmetry_control_web/controllers/task_goal_guard_controller_test.exs <goal_0006_migration_test.exs> --seed 0` | GREEN, rc=0, `62 passed`, 257 s; the two Oban tests took 5.2 s and 3.1 s in that pairing |

The decisive P3 number is not the probe but the **real suite**: the two Oban tests' own durations
across the streams (`w3-17-oban-durations.py`) are 2.9 s / 2.8 s (seed 0) and 3.1 s / 3.1 s
(seed 3) in the quiet seeds, but **9.9 s / 15.9 s** (seed 1) and **6.3 s / 36.3 s** (seed 2) in the
full suite — an order-of-magnitude inflation of the refusal test in exactly the runs where the real
producer is scheduled beside it (seed 1's total per-test work, 827.7 s, is comparable to seed 0's
803.6 s, so load alone does not explain it). Seed 2's 36.3 s is one lock-retry away from the 30 s
budget that P3b attempt 2 shows failing, which is the measured case for F-8's escalation.

**P2 — a lazy `Code.compile_file` at test time under `async: true` (contract Change 4).** Both tests
in `add_retry_commands_test.exs` reload the retry migration module before `with_schema`, so a
`Code.compile_file` redefines it in the global code server while the rest of the set runs.

| command | outcome |
| --- | --- |
| `mix test <nine paths> --seed 0 --repeat-until-failure 5` (`w3-17-probe-run.sh p2 5`) | **GREEN**, rc=0, 1151 s: five repetitions, each `78 passed`; the log shows the mutation really happened — `redefining module SymmetryControl.Repo.Migrations.AddRetryCommands (current version defined in memory)`, 12 lines over the five repetitions |

Recorded as insensitive, with the reason: the file compiled is the same source the boot registry
already compiled, BEAM keeps the executing version alive until a purge, and no converted test reads
module identity across the window. The hazard W3-14 recorded was the per-`run/4` recompilation of a
*path* source (all 43 files, in every test), which the boot registry removes; what remains here is a
test-time reload of one identical file, and it is not shown red. Deviation from the recipe's
`--repeat-until-failure 10` to 5 is recorded: the loop stops at the first failure and the gate sweep
that follows is the cost bound. Reverted afterwards (`RESTORE PASS`, all 13 files) and the probe's
own file is green on the delivered tree (`2 passed`, 1.9 s).

## Envelope measurement (`max_cases` / `pool_size`)

Re-derived at the slice head: 34 files / 35 `async: true` lines at the base become 43 files / 44
lines after the nine flips (one of the 44 is an assertion argument), against `max_cases: 28` and
`pool_size: 32` (16-scheduler host;
CI has 4 schedulers, and `min(max(schedulers*2, 32), 90)` pins 32 there too).

Connection arithmetic under `max_cases: 28`: the migration tests never check out the shared sandbox
pool (`with_schema/2` points the *dynamic* repo at their own single-connection pool), so the peak
is ≈ 28 sandbox connections (other async tests, upper bound) + ≤ 9 dedicated migration-test pools +
≤ 28 transient DDL connections + the app's own pool headroom, comfortably inside
`max_connections` = 100 (local cluster and the CI `postgres:18.6-alpine` default alike).
Both private pools tolerate ~10 s of connection establishment before a queued request is dropped
(`queue_target`/`queue_interval`, design decision 2), so a starved host delays a migration test
instead of failing it at DBConnection's ~4 s default.

Queueing: ExUnit's async phase is module-granular, so with 43 async modules on 28 slots the extra
14 wait for a free slot. The measured choice and its numbers are in the table below.

Three hazards considered before measuring:

* `add_history_lookup_indexes_test.exs`'s `concurrently: true` index build runs on its own
  connection outside any transaction (unchanged from the base), but `CREATE INDEX CONCURRENTLY`
  waits for in-progress transactions with older snapshots. In the base it ran in the serial phase,
  where it could not overlap a peer's transaction; as an async module it can, at worst behind
  goal_0006's two lock tests (bounded by their `lock_timeout = '1s'` / `… 15_000` limits). The
  converted-set runs (gate 5, five seeds plus ten repeats as delivered — five set-wide repetitions,
  which the review-stage ruling #1 accepts, plus `goal_0006`'s ten; attempt-2 wording reconciled
  here with the recorded deviation) are the coverage for this: any red there
  is a finding and would move that one file back to `async: false` with its call site recorded.
* The two Oban tests' `LOCK TABLE public.oban_jobs IN SHARE MODE`: the audit was wrong about the
  producers (finding F-8) and Change 4's P3 measured the residual — a 117 ms worst statement in the
  synthetic writer, a precisely attributed red when a writer is held across the migration, and an
  order-of-magnitude inflation of the refusal test's own duration (15.9 s / 36.3 s against ~3 s) in
  the two full-suite runs where the real producer is scheduled beside it. The disposition is the
  conductor's (answer §12.3).
* Fresh-connect exposure under a saturated server. The base harness opens one fresh connection per
  migration test (its schema Repo); the delivered harness opens four (schema Repo + one DDL Repo
  per statement: CREATE, the `after` DROP and the `on_exit` DROP). While this slice's gate sweep ran
  beside the pre-slice baseline suite, one repeat of
  `add_work_item_creation_identity_test.exs` failed with Postgrex's async socket close
  (`tcp recv: closed`) inside the `CREATE SCHEMA` statement, with the cluster's own log showing
  checkpoints whose writes took 270 s and 218 s in that window (the host had three BEAMs and ~70 of
  the cluster's 100 connections in use). Recorded as finding F-1
  (`gdd/refactor/scratch/w3-17-findings.md`); the one recorded same-seed re-run of that 25-repeat
  gate, with only the pre-slice suite on the host, was green (rc=0, 382 s, 25/25 repeats, 3 passed
  each), so the finding is recorded and not reproduced; F-4 then hit the same class in a post-side
  seed and drove the queue-tolerance hardening in design decision 2. Verified alternative if a
  *smaller* connection count is wanted rather than more tolerance:
  `create_schema!/2`/`drop_schema!/2` on the test's own Repo (a Repo accepts
  `parameters: [search_path: <missing schema>]`, and `CREATE SCHEMA`/`current_schema()`/`DROP SCHEMA`
  all work on it — `mix run --no-start` probe, 2026-11-22 22:46), which cuts fresh connects per test
  from four to one but forces the two manual call sites to drop before stopping their Repo, and
  does not remove the failure class (a stalled connect can still fail a migration test's first query
  on either tree). The review's variant — one reusable DDL Repo per VM with a small pool — was
  rejected for the same class in the other direction (R10): a small shared pool serializes the DDL
  that F-1/F-4/F-5 show can take tens of seconds under host saturation, so the per-statement Repo
  keeps the DDL parallel at the cost of three extra connects per test.

Both configurations measured back to back in the same window on an idle host (0 foreign `erl.exe`),
full-suite seed 0, same partition and command; only full-suite runs are compared because the
nine-file set (9 async modules) never reaches either slot count:

| run | config | result | run_s | async_s | sync_s |
| --- | --- | --- | --- | --- | --- |
| `w3-17-envelope/fullA-seed0.*` | A: `max_cases: 28`, `pool_size: 32` (committed) | rc=0, 1007 passed + 21 skipped + 4 excluded | 284.0 | 176.9 | 107.1 |
| `w3-17-envelope/fullB-seed0.*` | B: `max_cases: 40`, `pool_size: 48` | rc=0, same counts | 465.0 | 261.8 | 203.2 |

**Recorded choice: config A (28/32), unchanged** — `config/test.exs` keeps its committed line and
`test_helper.exs` keeps `max_cases: 28`. Config B is *worse*, uniformly: run +181 s, async +85 s,
sync +96 s, summed per-test work 663.0 s against 444.3 s, and even `Goal0006MigrationTest`'s own
work 240.3 s against 158.6 s. More slots on a 16-scheduler host means more modules interleaving, and
the inflation shows in every module including the async phase's floor. One caveat, recorded: this is
a single A/B pair on a shared host; the difference is ~+45 % and in the direction opposite to what
raising the cap could buy, so no further repeats were spent. Note that this window's
config-A number is far below the pre-review seeds' 538-715 s because the host was idle here (0
foreign BEAMs against 2-3 then) — the comparison that carries weight is A against B in this same
window, which is exactly why the pair was measured adjacently rather than read out of the sweep.

## Out of scope (with reasons)

* Production code and `control/priv/repo/migrations/**`: the two Oban tests assert the production
  migration's own `public.` behaviour; making them schema-scoped is a production change and is
  escalated, not taken (the audit now records the real async producers — finding F-8 — and the
  measured residual, with the disposition in answer §12.3).
* Test-content changes (splitting, renaming, fixture extraction): W3-14's F3 residue; the
  contract's failure control forbids renames and assertion changes.
* `goals_test.exs` and every other sync file, CI workflow edits, coverage/`--slowest`
  instrumentation, `goal_0006`'s internal per-test cost, and the ≤65 s target itself (measured and
  reported; the disposition belongs to the conductor).
* The `add_rt`/`System.system_time` schema-name base strings themselves: only their construction
  moved to the helper, the base literals stay as the files chose them.
