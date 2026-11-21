# Plan: Elixir fixture extraction batch 1 (control tests) — rf-w1-12

Contract: `goal-rf-w1-12-elixir-fixtures.md` (program plan §4 W1-12, findings T1, reviews
`plan-review-ds-1.md` P3-4/P3-5 and `plan-review-opus-1.md` P2-3). The opus amendment replaces the
contract's original owned set and acceptance wording where they conflict; this plan follows the
amendment.

## Deliverable

One shared data-builder module `SymmetryControl.TestFixtures` and one case template
`SymmetryControl.OrchestrationCase`, with the cross-file copies of five helpers converted to them:

| helper | owned-set definitions before | after | converted files |
| --- | --- | --- | --- |
| `register_runtime` | 9 | 1 (`TestFixtures`) | 8 non-web files |
| `claim` | 7 | 1 (`TestFixtures`) | 7 non-web files |
| `with_schema` | 7 (`/1` wrappers) + 1 (`MigrationCase` `/2`) | 1 (`MigrationCase.with_schema/2`) | 7 migration files |
| `command_current` | 3 | 1 (`TestFixtures`) | 3 goals files |
| `task_attrs/1` | 4 | 1 (`TestFixtures`) | 4 orchestration files |

Measured before/after counts come from the two owned paths combined, with `defp? <name>(` — the
`foo(args)` / `foo(args \\ [])` declaration shape the converted copies used; a same-named definition
written without parentheses is outside this pattern's scope:

    git grep -nE "defp? (register_runtime|claim|with_schema|command_current|task_attrs)\(" <rev> \
      -- control/test/symmetry_control        # base cb88bf8e: 9/7/7/5/4; converted tree: 0/0/0/2/0
    (same pattern) <rev> -- control/test/support   # base: 0/0/1/0/0; converted tree: 1/1/1/1/1

`command_current`'s base 5 and converted-tree 2 are the two e2e copies, which are out of scope, so
the owned-set pair is 3 / 1; `with_schema`'s support hit is the single `MigrationCase.with_schema/2`.
The `task_attrs` row is arity-scoped (`task_attrs/1`): the two same-named arity-0 helpers that were
never copies are listed under Out of scope. Web files stay outside the count (their copies are
listed for W3-03 batch 2 at the end of this plan).

## Public shape

`SymmetryControl.TestFixtures` (new, `control/test/support/test_fixtures.ex`):

```elixir
register_runtime(machine, now, opts \\ [])   # arities 2, 3
claim(run, runtime, now, opts \\ [])         # arities 3, 4
task_attrs(overrides \\ [])                  # arities 0, 1
provider_task_attrs(overrides)               # arity 1
command_current(goal_id, kind, payload, command_opts, opts \\ [])  # arities 4, 5
goal_command(goal, kind, payload, mutation_id \\ Ecto.UUID.generate())
```

Semantics that keep every converted call site byte-equivalent:

- `register_runtime/3` registers exactly one runtime through `Orchestration.register_runtimes/4`,
  at `now`, and returns it. Resource defaults are the values the majority of the copy-pasted bodies
  used: `runtime_key: "default"`, `name: "Local Codex"`, `capacity: 1`, `agent_profile: "codex"`,
  `workspace: "primary"`, `capabilities: %{}`, `daemon_instance_id: "00000000-0000-0000-0000-000000000001"`.
  Every knob the converted copies set differently is an explicit option at the call site:
  `:runtime_key`, `:name`, `:capacity`, `:agent_profile`, `:workspace`, `:capabilities`,
  `:daemon_instance_id` (accepts a literal; the files that generated a fresh id pass
  `Ecto.UUID.generate()` explicitly — laziness of the original `Keyword.get_lazy` is preserved),
  `:extra` (a map merged over the base spec, for `heartbeat_interval_ms`, `harness_*`,
  `adapter_*`, `repository_resource_id`).
- `claim/4` claims through `via.claim/3` (`:via` defaults to `Orchestration`; the goal-admission
  suite passes `Goals`) with `now:` positional and returns the same five-key fence map. `:claim_id`
  defaults to the deterministic fixture id `"00000000-0000-0000-0000-000000000010"` — the value 40
  of the converted call sites already pinned (three copies used that literal or `uuid(10)`, which
  renders the same string). Call sites that used a fresh idempotency key pass
  `claim_id: Ecto.UUID.generate()`; the read-model suite keeps its deterministic
  `claim_id: uuid(n)`.
- `task_attrs/1` merges overrides over the four-key base the majority used
  (`%{goal: "Run tests", agent_profile: "codex", workspace: "primary", input: %{}}`).
  `provider_task_attrs/1` is the same builder over the capability suite's `"Use provider"` base;
  the telemetry suite passes `task_attrs(goal: "Observe events")`. Merging (not keyword-putting)
  keeps key-override behaviour identical.
- `command_current/5` fetches the goal, builds the canonical command map
  (`expected_version: goal.version`, `expected_revision: goal.current_revision`,
  `payload: canonical_command_payload(kind, payload)`) and submits it as `"operator:test"` with
  `Keyword.merge(command_opts, opts)`. `command_opts` is the file's own defaults function: the
  clock/rollout/profiles each file's local helper used (`goals_test`: `[now: @now,
  validation_profiles: validation_profiles()]`; `dispatch_notification`: the existing
  `command_opts/0`; `workers`: `[validation_profiles: validation_profiles()]`). The merge keeps the
  original precedence (`opts` win; `put_new` on profiles is equivalent because the defaults only
  supply the key when the caller's list did not).
- `goal_command/3..4` is the canonicalising command builder; `goals_test`'s 19 and
  `dispatch_notification`'s 2 local command-building call sites call it directly, so the two files
  and the two canonical-compliance suites keep one implementation and no second name for the same
  function (their former `command/4` delegations are gone; the earlier "~70 direct call sites"
  figure counted every command-submission token, not the 21 wrapper call sites). The
  canonicalisation is only observable for `"request_decision"` payloads, and every
  `request_decision` payload passed from `dispatch_notification`/`workers` is already exactly the
  canonical key set (`%{kind:, work_item_id:, subject_hash:, proposal:}`) — re-verified by reading
  each of those 14 call sites.

`SymmetryControl.OrchestrationCase` (new, `control/test/support/orchestration_case.ex`) is the case
template the converted files `use` in place of `SymmetryControl.DataCase`:

```elixir
use SymmetryControl.OrchestrationCase, async: false
```

Its `using` block reproduces DataCase's alias/import surface and imports the fixture module; its
`setup` calls `SymmetryControl.DataCase.setup_sandbox(tags)` exactly once. This mirrors
`SymmetryControlWeb.ConnCase`, which is the repository's existing pattern for a case template over
DataCase.

Rejected alternative: nesting `use SymmetryControl.DataCase` inside `OrchestrationCase`'s `using`
block (or `use`-ing both templates in one file). ExUnit injects `use ExUnit.Case` per template and
`DataCase`'s sandbox setup then runs twice; measured on this tree with a throwaway probe
(`use SymmetryControl.DataCase, async: false` twice): exit 2, `Ecto.Adapters.SQL.Sandbox.start_owner!/2`
failure, `0/1 passed`. A template that only imports fixtures (not a case) was also rejected: every
converted file would then need two lines where ConnCase's pattern needs one, with no extra safety.

## Edge-case semantics the contract pins

1. Test names are unchanged. Measured by running the suite with
   `--formatter SymmetryControl.ExUnitJsonFormatter` (one `test_finished` line per test) on the
   baseline tree and on the converted tree; the sorted `module<TAB>name` lists must be identical,
   and so must the per-test states.
2. Converted helpers are pure fixtures: the acceptance bar is identical SQL/HTTP traffic. Every
   value a converted body passed stays identical — argument values, literal ids, clocks, option
   precedence, and the `Orchestration`/`Goals` entry points are preserved at the call sites. The
   full suite (which asserts stored payloads, notification bodies and projections) is the traffic
   oracle.
3. No converted helper loses coverage: no test is deleted, renamed, retagged or narrowed; the diff
   adds no skip/exclude.
4. `mix test` stays green on the converted tree with the same include/exclude filters as the
   baseline.

## Evidence plan (all foreground, serially; never a suite next to a measurement harness)

Portable-toolchain recipe `<HOST-PATH>/gdd/scratch/rf-w1-12-mix.sh` (adapted from `retro-g2-mix.sh`
for this worktree): Elixir 1.20.4 / OTP 29.0.6 via `erl.exe`, `MIX_TEST_PARTITION=rf-w1-12`,
`POSTGRES_PORT`/`PHX_SERVER`/`SYMMETRY_CONTRACTS_DIR` unset, deps read from the main checkout. This
worktree's `control/mix.lock` is unmodified. A sibling worktree later ran `deps.get` into that shared
deps directory (`plug` 1.20.4 vs the locked 1.20.3), so the tail of the gate run used the local-deps
variant `rf-w1-12-mix-local.sh` (`MIX_DEPS_PATH=<worktree>/control/.deps-local`), which fetches the
locked versions; see the answer's residual risks.

1. Baseline: `ERL_SCHEDULERS=4 mix test --seed 0 --exclude portal_assets` on HEAD (`a3039f0`) with
   the JSON formatter → test-name list + states.
2. Per converted file, a targeted `mix test <file>` as it converts.
3. Converted tree: the same full-suite command as the baseline; compare name lists and states.
4. `mix format --check-formatted`; `MIX_ENV=test mix compile --warnings-as-errors` (forced
   recompile after the last edit); `git diff --check`.
5. Duplicate-helper counts before/after (command + output) in the answer.
6. CI-only oracle: the control job (conductor verifies on the PR).

`--exclude portal_assets` is the documented host path for a machine without a staged frontend
bundle (`test/support/portal_bundle.ex`); the four excluded cases live in
`portal_controller_test.exs` (web controller, not in the owned set) and the same exclusion applies
to both runs, so the name comparison is unaffected. Postgres: the shared 5432 cluster (contract
recipe), `ERL_SCHEDULERS=4` to match CI's 4-CPU pool arithmetic.

## Out of scope (with reasons)

- **Web controller test copies** of `register_runtime`/`claim`/`command_current`
  (`symmetry_control_web/controllers/{goal_machine_controller,operator_read_controller,
  protocol_controller,task_goal_guard_controller}_test.exs`) — W1-13/W3-03 batch 2, explicitly
  excluded by the contract.
- **E2E copies** (`pi_control_e2e_test.exs`, `provider_broker_e2e_test.exs`) — the opus amendment
  keeps `provider_broker_e2e_test.exs` out (W1-14's oracle) and `pi_control_e2e_test.exs` is not in
  the owned set; both are listed for the batch-2 slice.
- `register_runtime_on_machine/3` in `orchestration_goal_admission_test.exs` — a single-definition
  local spec builder (native adapter capabilities, repository-resource lookup); not a cross-file
  duplicate. Its caller `register_runtime/2` (the label composite that also enrolls a machine) is
  renamed `enroll_and_register_runtime/2` so the shared name has one definition; renaming is not a
  behavioural change and test names are unaffected.
- Other duplicated helper names inside the owned set (`uuid/1`, `enroll_machine/0..2`, `transition/4`,
  `migrate_up!`, `migrate_all_up!`, `enforce_goal_phase`, …) — not among the contract's converted
  names; converting them widens the diff past this slice's acceptance bar. Recorded for the
  follow-up extraction slice.
- Two same-named arity-0 `task_attrs` helpers in owned files are not copies of the converted
  `task_attrs/1` builder and stay: `goals_schema_test.exs:587` (a 9-key insert attrs map) and
  `orchestration_supervision_test.exs:693` (`"Build the adapter"` / `"supervised"` base). They are
  why the deliverable table's `task_attrs` row is arity-scoped.
- ExUnit `async` conversion (W3-14) and production code (none touched). `MigrationCase` is reused,
  not rewritten; the one edit the conversion forced is failure-path parity: `with_schema/2` now
  starts the private-schema Repo inside its `try` so a Repo-start failure still drops the schema
  (the converted `goal_0006_migration_test.exs` inherited the harness leak; success path unchanged).

## Design trade-offs

- Passing per-file constants (clock, defaults, schema base) at each call site rather than keeping a
  thin local wrapper: matches the merged #114 idiom (`with_schema("add_history_lookup_indexes", …)`,
  `command_opts()`); keeps one definition per name, which the contract's acceptance measures.
  Cost: mechanically larger diffs in `goals_test.exs` (~200 call sites gain `command_opts()`) and
  `orchestration_goal_admission_test.exs` (24 claim sites gain `claim_id:`/`via:`).
- Migration files whose local helper was a pure delegation pass the schema base at the call site
  (`with_schema("add_omo_harness_kind_migration", fn -> … end)`), matching the #114 idiom. The two
  files whose helper bootstrapped module loading (`add_retry_commands_test.exs`, whose helper also
  ran `migrate_up!()` inside the schema, and `version_request_hashes_test.exs`, whose helper only
  loaded the modules and left `migrate_up!()` to the test body) inline that bootstrap at their two
  call sites each. `goal_0006_migration_test.exs` keeps a renamed composer
  (`with_base_schema/1`) because inlining `migrate_base_up!()`/`load_migration_modules!()` into its
  60 call sites moves more lines than it removes and weakens reviewability; the composer is not a
  copy of `MigrationCase.with_schema/2`, it calls it.
- Defaults for `daemon_instance_id`/`claim_id` were chosen to be the deterministic values the
  majority of copies already pinned, so the common call site stays terse and the files that need a
  fresh id say so explicitly — the safe direction, because a fresh id is the deliberate choice in
  the tests that use it (idempotency/replay semantics).

## Verification owner

Sol (this stage) runs every gate listed above in the foreground and records real exit codes and
counts; an independent read-only review follows in the review stage.
