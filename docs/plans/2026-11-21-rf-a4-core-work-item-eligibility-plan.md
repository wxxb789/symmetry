# rf-a4 core work-item eligibility plan (A4)

Contract: `goal-rf-a4-core-work-item-eligibility.md` (GDD, 2026-11-21; conductor decision
2026-11-21 arch-decision-2 O14/R10 binding). Program row: arch-decision-1 "Work-item eligibility
re-derived in web" — A4 lands first, before W6-09 (both edit `portal_json.ex`) and before W5-15's
control half. Baseline for every coordinate here: this worktree's HEAD
`c07849521e6bdfdf608b414ca6734a4ae0041cbb` (`c0784952`, the #190 merge; `origin/main` at the same
commit). Hard dependencies satisfied in this history: #184 (`94d04e42`, `workspaces.ex`) and #190
(`c0784952`, the oracle test files).

Owned files (contract): new `control/lib/symmetry_control/workspaces/work_item_eligibility.ex` and
its test; `control/lib/symmetry_control/workspaces.ex` (launch, cancel, retry, `launch_locked_`
`work_item` clause 2, the `ensure_*` helpers); `control/lib/symmetry_control_web/portal_json.ex`
(`work_item/2`, `execution/2` + `work_item_retry_runtime_eligible?/2`, `execution_summary/2` +
`legacy_capabilities/3`; not `attention/3`); this plan; `docs/TODO.md` /
`docs/archive/CLOSED-TODO.md`. No edit to `orchestration.ex`, `chat/**`, `goals.ex`,
`integrations/provider_access.ex`, schemas/fixtures/generated contracts, the frontend, or any
existing test file.

The increment: the work-item predicates of start/cancel/retry are written twice — once as the
enforcement gates in `Workspaces`, once as the projection's conjuncts in `PortalJSON` — and kept in
agreement by comments and mirrored pins. There is no core verdict both sides consume, and the drift
has already shipped once (`docs/plans/2026-11-17-0007e-resource-binding-and-sync-plan.md:24`: a
goal-managed item served `can_start: true` while the run endpoint refused `403
goal_authority_required`). This slice puts the verdicts in one core module and makes both sides call
it, with the served members, refusal atoms, precedence, transactions and lock order byte-identical.

## Verified state at the baseline

### The web restatements

| site | coordinates | predicate |
| --- | --- | --- |
| served `can_start` | `portal_json.ex:254-257` (member at `:254`, `goal_managed` binding at `:225`) | `not goal_managed and is_nil(orchestration_task_id) and assignee_type == "agent" and status in ["backlog", "ready"] and project_active?(project) and WorkItem.external_work_available?/1` |
| served `can_cancel` work-item half | `portal_json.ex:420` (`legacy_capabilities/3`) | `and not goal_managed` |
| served `can_retry` work-item half | `portal_json.ex:421-424` | `and assignee_type == "agent" and WorkItem.external_work_available?/1 and project_active?(project)` |
| its private project test | `portal_json.ex:713-714` | `%{status: "active"} -> true`, else `false` |

Both served halves reach the wire through `work_item/2` (workspace read, activity feed) and through
`execution/2` (command responses), which composes `execution_summary/2` with the same
`legacy_capabilities/3`.

### The enforcement sites

| command | coordinates (HEAD) | gate |
| --- | --- | --- |
| `launch_work_item/3` | `workspaces.ex:316-347`; pre-transaction `:321`; in-transaction `:325-326` | `ensure_legacy_work_item/1` twice (unlocked read, then the locked re-read), `ensure_active_project/1` |
| `launch_locked_work_item/2` clause 1 | `:579-596` | the already-launched replay (`:585` `:idempotency_conflict` on a required-capability mismatch) |
| `launch_locked_work_item/2` clause 2 | `:598-643` (check `:599-603`) | `project.status != "active" or assignee_type != "agent" or status not in ["backlog", "ready"] or not WorkItem.external_work_available?/1` → `Repo.rollback(:state_conflict)` |
| `cancel_work_item/3` | `:349-374` (gate `:353`) | `ensure_legacy_work_item/1` only; task-state validity is `Orchestration.create_command/5`'s |
| `provide_work_item_input/4` | `:378-409` (gate `:382`) | `ensure_legacy_work_item/1` (same gate; not otherwise in the contract's list) |
| `retry_work_item/3` | `:413-478` (pre-transaction `:418`, in-transaction `:422-424`) | `ensure_legacy_work_item/1` twice, `ensure_active_project/1`, `ensure_external_work_available/1`, the inline `assignee_type != "agent"` rollback |
| `update_work_item/2`, `move_work_item/2` | `:193/:199`, `:234/:249` | `ensure_legacy_work_item/1` (callers of the shared gate; untouched in shape) |
| the helpers | `:858-859` `ensure_active_project/1`, `:861-866` `ensure_external_work_available/1`, `:869-873` `ensure_legacy_work_item/1` | the predicates above |

`ensure_active_project/1` has eight other callers (project/resource/work-item mutation paths at
`:77, :99, :128, :156, :196, :238`) and stays. `ensure_external_work_available/1` has exactly one
caller (`retry_work_item/3`) and becomes obsolete when the retry gate moves. `ensure_legacy_work_item/1`
has ten callers and stays as the named shared gate.

### The oracles this slice must not edit

- `portal_workflow_test.exs` capability pins (`:48` member list; `:398-444` the goal fence with
  `managed["can_start"] == false` / sibling `== true` and the endpoints' 403/202; `:674-825` the
  archived-project, re-bound-binding and human-reassign retry pins — read − modified-route
  agreement).
- `portal_resources_test.exs:696/:721/:732/:734` (external availability drives `can_start` and
  `can_retry` in both directions).
- `workspaces_read_model_test.exs` (26 capability lines `:329-581`), `workspaces_test.exs`
  (`:228` `{:ok, second, :replayed}`; `:250-303` the goal-owned refusals incl.
  `{:error, :goal_authority_required}` from launch/cancel/input; `:350/:409` `{:replayed, …}`).
- `chat_test.exs:837-838`, `orchestration_supervision_test.exs:255,317`.
- The W1-13 served-document gates (`test/support/served_document.ex:14`, called from
  `portal_resources_test.exs:115,1457` and `portal_workflow_test.exs:7,1424,1478`) and the schemas
  carrying `can_start` (`contracts/v1/workspace-read.schema.json:193`,
  `work-detail-read.schema.json:54`, `chat-read.schema.json:136`).

## Public / API shape

`control/lib/symmetry_control/workspaces/work_item_eligibility.ex` — six public functions
(interface budget ≤ 6), pure over an already-loaded `%WorkItem{}`, no `Repo`/transaction/process
call, no `@doc false` policy. Every refusal atom and every precedence is the enforcing order at the
baseline.

| function | returns | head callers |
| --- | --- | --- |
| `start/1` | `:ok \| {:error, :goal_authority_required \| :state_conflict}` | `Workspaces.launch_locked_work_item/2` clause 2 |
| `start_allowed?/1` | `boolean()` — the served `can_start` | `PortalJSON.work_item/2` |
| `cancel_gate/1` | `:ok \| {:error, :goal_authority_required}` — the Goal gate | `Workspaces.ensure_legacy_work_item/1` (launch, cancel, provide-input, retry, update, move) |
| `cancel_allowed?/1` | `boolean()` — the work-item half of the served `can_cancel` | `PortalJSON.legacy_capabilities/2` |
| `retry_gate/1` | `:ok \| {:error, :goal_authority_required \| :state_conflict}` | `Workspaces.retry_work_item/3` (in-transaction) |
| `retry_allowed?/1` | `boolean()` — the work-item half of the served `can_retry` | `PortalJSON.legacy_capabilities/2` |

Three private helpers carry the shared clauses (`start_project/1` = the active-project clause, the
same token set as the base `ensure_active_project/1`; `start_state/1` = assignee/status/external;
`retry_external_work/1` = the external clause, the same token set as the base
`ensure_external_work_available/1`; `retry_gate/1` keeps the agent clause inline beside them).
`cancel_gate/1` is deliberately the *Goal* clause of all three verdicts:
cancel's own work-item gate is exactly this clause, and `ensure_legacy_work_item/1` — the one name
every legacy command path shares — is its caller. The name is the contract's.

Decomposition rule applied at every call site: the verdict with the refusal atom replaces the
predicate **at the exact place the base spelled it**, so the base's sequence survives:

1. `workspaces.ex:316-347` `launch_work_item/3`: unchanged. The pre-transaction and in-transaction
   `ensure_legacy_work_item/1` calls now reach `cancel_gate/1`; the in-transaction
   `ensure_active_project/1` stays (it precedes the replay clause, so a launched item in an archived
   project is still refused, as at the baseline).
2. `workspaces.ex:598-643` `launch_locked_work_item/2` clause 2: the four-condition `if` becomes
   `WorkItemEligibility.start(work_item) |> require_ok!()`. The clause still only runs with no
   attempt launched (clause 1 matches first), and `start/1` evaluates the project condition first,
   exactly where the base `or` chain did.
3. `workspaces.ex:413-478` `retry_work_item/3`: the pre-transaction `ensure_legacy_work_item/1`
   stays (the fetched row's project association is unloaded; only the Goal clause is answerable
   there). The in-transaction four gates (`ensure_legacy_work_item/1` re-check,
   `ensure_active_project/1`, `ensure_external_work_available/1`, the inline agent rollback) become
   one `WorkItemEligibility.retry_gate(work_item) |> require_ok!()` — the same four checks in the
   same order, one reason. `ensure_external_work_available/1` is deleted with its only caller.
4. `workspaces.ex:869-873` `ensure_legacy_work_item/1` becomes `cancel_gate/1` (one call, same
   `{:error, :goal_authority_required}`), so all ten callers keep their call site and atom.
5. `portal_json.ex:254-257` `can_start` becomes `WorkItemEligibility.start_allowed?(work_item)` in
   the same map position; `portal_json.ex:416-426` `legacy_capabilities/3` becomes
   `legacy_capabilities/2` applying `cancel_allowed?/1` and `retry_allowed?/1` to the execution map;
   `project_active?/2` (`:713-714`) is deleted with its two callers. The `goal_managed` member keeps
   its own derivation (`WorkItem.goal_managed?(work_item)`, `:225`) because it is a served *value*,
   not a gate: the module's `cancel_allowed?/1` is its negation.

After the slice the only work-item predicate spelling left in `portal_json.ex` is that served
member's derivation; the grep gate below is the check.

## Exact edge-case semantics the truth table pins

Inputs (one dimension each, all read from the loaded struct): `goal_managed`
(`WorkItem.goal_managed?/1` — all three membership columns set or not), `launched`
(`is_binary(orchestration_task_id)`, the enforcement spelling of clause 1; `is_nil/1`, the
projection spelling, for `start_allowed?/1`), `assignee_type == "agent"`, `status in
["backlog", "ready"]`, `project.status == "active"` (any other value, `Ecto.Association.NotLoaded`
included, is not active), `WorkItem.external_work_available?/1` (`external_work_item_resource_id
== nil` → true, else `external_available`).

The table's dimensions are the predicate classes, not the raw enum spellings: the assignee column
is the agent/not-agent class (the schema's `"human"` and `"unassigned"` take the same branch of
`assignee_type == "agent"`), and the status column carries the three literals so that both
startable values (`"backlog"`, `"ready"`) are exercised beside a non-startable one
(`"in_progress"`). The unloaded-project and no-external-binding spellings have their own pins
beside the table.

**Start.** Base enforcement order: Goal gate → active project → (no attempt launched ? the three
fresh-start conditions : the replay). `start/1` returns
`:goal_authority_required` for the first, `:state_conflict` for any of the next four, `:ok` for a
launched (and non-Goal, active-project) item — the launch's own answer is the replay, so the
request is acceptable. `start_allowed?/1` adds the `is_nil(orchestration_task_id)` conjunct: the
served `can_start` is the fresh-start question, and a launched item is served `false` beside the
same launch answering `{:ok, snapshot, :replayed}` (`workspaces_test.exs:228`).

**Cancel.** Base `cancel_gate/1` is the Goal clause alone: `:goal_authority_required` or `:ok`.
The served `can_cancel` work-item half is `cancel_allowed?/1` = the verdict is `:ok`; the
task-state lists and `Orchestration.adapter_operation_supported?/2` stay in `execution_summary/2`
(the task levels are core already and `orchestration.ex` is not edited). A missing
`orchestration_task_id` stays a `:state_conflict` in `cancel_work_item/3`'s `with` (the `nil`
branch), as at the baseline.

**Retry.** Base enforcement order: Goal gate → active project → external work available → agent
assignee, then the missing-task `:state_conflict`. `retry_gate/1` reports the same atoms in that
order; the three `:state_conflict` conditions are not separable — one atom, pure checks, so their
relative order is unobservable and the table asserts the atom, not a sub-order. The served
`can_retry` work-item half is `retry_allowed?/1` = agent and external-available and active-project,
the baseline conjuncts in the baseline spelling order — deliberately **without** the Goal term,
because the baseline has none there: a Goal-owned *attempt* is already excluded by
`Orchestration.retryable_attempt?/1` (`orchestration.ex:656-660`, `goal_id: nil` only), and the
module does not silently add a conjunct the served bytes never had. (A goal-managed *work item*
whose task is not Goal-owned is the one shape where the served `can_retry` can be true while
`retry_gate/1` refuses `:goal_authority_required`; that asymmetry is the baseline's, kept, and
pinned by the truth table's goal-managed rows.)

**Value spellings.** `WorkItem.external_work_available?/1` is the schema predicate, called, never
restated (it has two clauses). Its answer is a boolean on every stored row (`external_available` is
`null: false, default: true`,
`control/priv/repo/migrations/20260905000000_add_engineering_connections.exs:56`), so the served
boolean chains that end in it cannot become `null` at either revision. `start_allowed?/1` returns
`is_nil(...) and match?(:ok, start(work_item))` and `retry_allowed?/1` returns the three-conjunct
`and` chain of booleans: the same `true | false` values the baseline chains produce, including the
short-circuit answer `false` for every failing conjunct.

**Served bytes.** The map literals of `work_item/2`, `execution_summary/2` and
`legacy_capabilities/2` keep their member names, member order and value types; only the value
expressions for `can_start`, `can_cancel` and `can_retry` change (the `Map.update!` pipeline shape is
kept). `execution/2` and `work_item_retry_runtime_eligible?/2` are untouched except for the arity
call into `legacy_capabilities/2`; `attention/3`, `delivery/2`, `outcome/*` and the `@evidence_keys`
getter are untouched.

## Increment

1. New `control/lib/symmetry_control/workspaces/work_item_eligibility.ex` as specified above
   (`@moduledoc` paragraph, `@spec` per function, `@startable_statuses ["backlog", "ready"]`, three
   private helpers).
2. `control/lib/symmetry_control/workspaces.ex`: the five edits of the decomposition rule, plus the
   deletion of `ensure_external_work_available/1`.
3. `control/lib/symmetry_control_web/portal_json.ex`: alias the module; `can_start` from
   `start_allowed?/1`; `legacy_capabilities/2` from the two boolean helpers; delete
   `project_active?/2`; refresh the two comments that name the old derivation
   (`:410-415` `legacy_capabilities`' comment and `:371-379` `execution_summary`'s closing sentence)
   so no prose names a call that no longer exists.
4. New `control/test/symmetry_control/workspaces/work_item_eligibility_test.exs`: `async: true`,
   pure, one literal truth table (96 rows: 2 goal × 2 launched × 2 assignee × 3 status × 2 project ×
   2 external — the third status class `"in_progress"` is shared by every other value) asserting per
   row the six module answers plus the enforcement atom/outcome the three commands answer with on
   that row. Expected values are literals (the baseline semantics frozen), not calls back into the
   module; the table also pins the exact refusal *atom* for each row so a changed atom fails even
   when the boolean class would not.
5. `docs/TODO.md`: the two dated entries the conductor decision requires (allowed-actions
   duplication with its owning slices; the chat projection inversion with W5-15), each citing the
   baseline lines.

## Evidence plan (all gates FOREGROUND; one full suite at a time on this host)

Recipe: `bash <HOST-PATH>/gdd/scratch/rf-a4-mix.sh <mix args>` — portable Elixir 1.20.4 / OTP 29,
`MIX_ENV=test`, `MIX_TEST_PARTITION=rf-a4` (own DB `symmetry_control_testrf-a4`),
`PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR`/`MIX_DEPS_PATH`/`MIX_BUILD_PATH` unset,
worktree-local `control/deps` refreshed with `mix deps.get` (the copied tree carried the bandit/
telemetry lock mismatch the w3-14 runner comment records). Full-suite runs go through
`<HOST-PATH>/gdd/scratch/mix-full-queue.sh` (serialized, isolation reruns).

| gate | command | expected |
| --- | --- | --- |
| E1 format | `mix format --check-formatted` | exit 0, empty |
| E2 compile | `MIX_ENV=test mix compile --warnings-as-errors` | exit 0, no warnings |
| E3 xref | `mix xref graph --format cycles --label compile-connected` and `mix xref callers SymmetryControlWeb.PortalJSON` | no new cycle; the caller list identical to base (no new core → web edge) |
| E4 targeted suites | `mix test test/symmetry_control/workspaces/work_item_eligibility_test.exs test/symmetry_control_web/controllers/portal_workflow_test.exs test/symmetry_control_web/controllers/portal_resources_test.exs test/symmetry_control/workspaces_read_model_test.exs test/symmetry_control/workspaces_test.exs test/symmetry_control/chat_test.exs test/symmetry_control/orchestration_supervision_test.exs test/symmetry_control_web/controllers/chat_controller_test.exs` | exit 0; counts recorded; the base run of the same seven oracle files (without the new file) is the control |
| E5 full suite | `mix-full-queue.sh "rf-a4|Q:/repos/wt-a4-core-work-item-eligibility/symmetry"` | `MIXQ-rf-a4 PASS rc=0`, counts recorded (isolation rerun on any env noise, per its own contract) |
| E6 contracts | `pnpm contracts:check` (root; `mise exec pnpm@12.17.0`) | exit 0 — typecheck, fixture tests, schemas/fixtures, DTOs current; no `contracts/` change |
| E7 grep gate | `rg -n 'assignee_type == "agent"|status in \["backlog", "ready"\]|project_active\?\(|external_work_available\?\(|goal_managed\?\(|not goal_managed' control/lib/symmetry_control_web/portal_json.ex` | exactly one match: `:225`'s served-`goal_managed` derivation (a served value, not a gate); every other predicate spelling is gone |
| E8 token byte-compare | `rg -o ':[a-z_]+|"[^"]*"'` over the named functions at base and head (script `<HOST-PATH>/gdd/scratch/rf-a4-tokens.py`, which also extracts the member keys) | served member keys identical as multisets per function; the union over the touched functions plus the new module equals base's token for token; every delta is a moved predicate spelling, listed row by row |
| E9 control-flow audit (lesson 32) | per-function base-vs-head table for `launch_work_item/3`, `launch_locked_work_item/2` (both clauses), `cancel_work_item/3`, `retry_work_item/3`, `work_item/2`, `execution/2`, `legacy_capabilities/N` — every early return, `with` else, `Repo.rollback`, default clause | each base exit has a head equivalent with the same atom/status; the two collapses (`clause 2`'s `if` → `start/1`; retry's four gates → `retry_gate/1`) are same-atom, same-order, and the clause-2 Goal branch is recorded unreachable (the in-transaction `ensure_legacy_work_item/1` ran first on the same locked row) |
| E10 base-vs-head differential | throwaway ExUnit file (deleted before delivery) that drives the real command paths and the real read model: 96 launch jobs over all six inputs (command result + served `can_start`), and 64 served + 64 cancel + 64 retry jobs over `goal × project × external × assignee × attempt shape` (served `can_start`/`can_cancel`/`can_retry` and the two command results); the dump carries only the observed class (no ids or timestamps), and the same throwaway harness runs on the base tree, twice (stability control), and on the head tree | base run 1 = base run 2 = head, 288 lines each; the harness is live (non-refusals recorded for both commands and both refusal atoms) and the crosscheck script agrees with the truth-table rules on every row |
| E11 red mutants | M1 drop the Goal clause from `start/1`; M2 accept any status; M3 make `start_allowed?/1` ignore the launched conjunct; M4 make `cancel_allowed?/1` constant true; M5 add the Goal term to `retry_allowed?/1`; M6 return `:ok` for a non-agent retry; M7 swap `retry_gate/1`'s project and external order (expected green — the atom class is unchanged — recorded as the unobservability claim), M8 make `retry_gate/1` refuse an archived project with `:goal_authority_required` | M1–M6, M8 red with the failing row group named; M7 green, recorded as expected |
| E12 credo | `mix credo --strict` over the touched files | count on the touched files does not rise above base (recorded per file) |
| E13 citation gate | `python scripts/ci/check-citation-existence.py --base origin/main --head HEAD` | exit 0; the audited corpus is untouched by this slice (no `docs/runbooks/**`, `docs/design/quality.md` or `docs/archive/CLOSED-TODO.md` edit) except for the refreshed citations this slice writes, if any |
| E14 whitespace / host paths | `git diff --check`; `git diff origin/main...HEAD \| grep -i "users/"` | exit 0, empty; no host profile path; no backslash-u sequence |

Transient harness files (outside the repository, or reverted before delivery): the token script and
the differential/runner scripts under `<HOST-PATH>/gdd/scratch/rf-a4-*`; the throwaway differential
test file inside `control/test/` is deleted after its head run (content recorded in the plan's
evidence record).

## Design trade-offs

1. **`start/1` answers the launch *request*, not the fresh-start question.** A launched item's
   launch is answered by clause 1's replay (`{:ok, _, :replayed}`, pinned at
   `workspaces_test.exs:228`), so a verdict that refused `:state_conflict` there would invent a
   refusal the enforcement never produces. The six-input enumeration is still complete: `start/1`
   branches on `launched` (skip the fresh-start conditions) and `start_allowed?/1` states the
   `is_nil/1` conjunct the read serves. The truth table's enforcement column is then equal to the
   verdict for every row, which is the property the contract's clause 3 asks for.
2. **`cancel_gate/1` is the shared Goal clause; `ensure_legacy_work_item/1` keeps its name and
   callers.** The gate is applied at ten call sites across six functions, two of them outside this
   slice's contract list (`provide_work_item_input/4`, `update_work_item/2`/`move_work_item/2`).
   Delegating keeps every atom and the re-lock-then-recheck shape; inlining the module call at ten
   sites would rewrite regions this slice does not own.
3. **The retry in-transaction gates collapse to `retry_gate/1`, the launch ones do not collapse
   further.** In retry the four base gates are one contiguous block, all inside the transaction, so
   one verdict call is the same sequence with one reason. In launch the Goal/project gates sit
   *before* the replay clause (a launched item in an archived project is refused — the replay
   branch cannot own that refusal), so `launch_work_item/3` keeps its two `ensure_*` calls and
   `launch_locked_work_item/2` clause 2 keeps its check — the same split the base has, now with the
   verdict as the check. `start/1`'s Goal/project clauses are redundant there on purpose: the base
   clause 2 re-checked the project the same way, and the Goal re-check is unreachable rather than
   wrong.
4. **The pre-transaction gate stays Goal-only.** The fetched row's project association is not
   loaded, and an unloaded project is never `"active"`; a full `start/1`/`retry_gate/1` at the
   pre-transaction site would turn every launch and retry into `:state_conflict`. This is a
   correctness reason, not style.
5. **Boolean helpers reproduce the baseline conjuncts exactly, including the Goal asymmetry.**
   `retry_allowed?/1` has no Goal term because the served `can_retry` never had one (the Goal-owned
   attempt is excluded by `Orchestration.retryable_attempt?/1`); adding one would change served
   bytes for the goal-managed-work-item/goal-less-task shape, which this increment may not do. The
   asymmetry (verdict refuses, the read of that shape can still say true) is recorded, pinned by the
   truth table's goal-managed rows, and left for the owner of the retry-family policy.
6. **`WorkItem.external_work_available?/1` is called, never restated.** The module owns the
   *work-item* gates; the membership and availability facts stay schema predicates, as they are
   today, so the two-clause availability semantics and the `goal_managed?/1` conjunction are not
   copied.
7. **The served `goal_managed` member keeps its own derivation.** It is a served value (the
   frontend decodes it, `frontend/src/features/work/control.tsx`), not a gate; deriving it as
   `not cancel_allowed?/1` would make a schema fact depend on a gate that may gain conjuncts.
8. **`legacy_capabilities/3` becomes `/2`.** Its third parameter was the caller-computed
   `goal_managed` boolean, now unused; both call sites are in the owned region.
9. **The truth table's expected values are literals.** A table computed by the module would be
   tautological; a table recomputing the base chains at runtime would duplicate the logic under
   test. Literal rows generated once (by a script that transcribes the *baseline* expressions) and
   reviewed by hand, plus E10's live differential, give both the falsifiability (E11) and the base
   equality (E10).

## Out of scope (contract clause 4, with the conductor ruling)

- The chat read-model inversion and `decision.lock_version` (W5-15). `chat/read_model.ex:10,46`
  still calls `SymmetryControlWeb.PortalJSON.work_item_detail/2` and overrides five capability
  members (`:51-65`); this slice leaves both untouched and records a dated `docs/TODO.md` entry
  naming W5-15 as the owner (conductor O14).
- The allowed-actions predicate unification (`goals.ex:9982-9985` `provider_operation_allowed?`
  against `integrations/provider_access.ex:607-609` `goal_action_allowed?`): owned by the
  W4-07/W5-14 and W4-06 chains; one dated `docs/TODO.md` entry with the cited lines (conductor
  O14).
- Task-state cancel lists and the retry attempt predicate inside `Orchestration`
  (`orchestration.ex` is W4-18's; not edited here — the facade calls `PortalJSON` makes are
  unchanged, so a W4-18 landing before this one needs no re-derivation of the call names).
- W6-09's `attention/3` region (`portal_json.ex:105-140` and `:115`): untouched, and this slice
  lands before it (its contract records "A4 lands first").
- The goal-task eligibility host question (conductor R10: W5-18 may host it here or in
  `Goals.Eligibility`): no interface change is needed for that now; the module's shape does not
  preclude it.
- Every existing test file: the oracles stay as the independent check. No mirrored pin is deleted
  (the contract permits one only with a recorded red mutant and only in an owned file; the default
  is none).
- `docs/archive/CLOSED-TODO.md`: not touched. The archived can-start/goal-fence decision note
  (`:3079-3101`) already records the serving decision this slice re-implements, and the citation
  gate audits the whole file on any edit, so an untouched archive is also the safer close.

## Residual risks

- The truth table's enforcement column is a frozen claim about `Workspaces`; E4's oracle suites and
  E10's live differential are what verify it against the running command paths. If E10 cannot
  construct one of the attempt shapes for a row group (e.g. a Goal-owned *task* for a non-Goal work
  item), that row group is recorded as truth-table-only, with the reason, rather than silently
  reported as covered.
- `git diff origin/main...HEAD` must stay free of host profile paths: the plan, the test and the
  module use `<HOST-PATH>` in prose and no absolute paths in code.
- The full control suite and the CI `control` job remain the conductor's gate-runner steps per the
  contract; this tree is left uncommitted for verification.

## Evidence as run (2026-11-21/22, base `c0784952`)

All gates ran foreground through `<HOST-PATH>/gdd/scratch/rf-a4-mix.sh` (portable Elixir 1.20.4 /
OTP 29, `MIX_ENV=test`, `MIX_TEST_PARTITION=rf-a4`, DB `symmetry_control_testrf-a4`,
`PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR`/`MIX_DEPS_PATH`/`MIX_BUILD_PATH` unset;
worktree-local `control/deps` refreshed with `mix deps.get` because the copy taken from the main
checkout carried the bandit/telemetry lock mismatch the w3-14 runner comment records), one mix run
at a time, and the full suite through `<HOST-PATH>/gdd/scratch/mix-full-queue.sh`.

| gate | command | result |
| --- | --- | --- |
| E1 format | `mix format --check-formatted` | exit 0, empty |
| E2 compile | `MIX_ENV=test mix compile --warnings-as-errors` (after `rm -rf _build/test/lib/symmetry_control`, so the delivered bytes are compiled, not a cached no-op) | exit 0, no warnings, `Compiling 89 files (.ex)` |
| E3 xref cycles | `mix xref graph --format cycles --label compile-connected` | `No cycles found`, exit 0 |
| E3 xref callers | `mix xref callers SymmetryControlWeb.PortalJSON` | head: 4 callers — `chat/read_model.ex`, `controllers/goal_controller.ex`, `controllers/portal_api_controller.ex`, `protocol.ex` (all `runtime`); the base revision measured in a throwaway worktree at `c0784952` (`Q:/repos/wt-a4-core-work-item-eligibility/wt-a4-base-xref`, removed afterwards): the same 4; no new core → web edge |
| E4 targeted (base control) | `mix test --exclude portal_assets` over the 7 oracle files | exit 0, `102 passed` in 21.8s |
| E4 targeted (head) | the 7 oracle files + the new test | exit 0, `105 passed` in 26.8s |
| E5 full suite | `mix-full-queue.sh "rf-a4\|Q:/repos/wt-a4-core-work-item-eligibility/symmetry"`, then the same suite with `--formatter ExUnit.CLIFormatter --formatter SymmetryControl.ExUnitJsonFormatter` (`SYMMETRY_EXUNIT_EVENTS_FILE=<HOST-PATH>/gdd/scratch/rf-a4-full-events.ndjson`) | both `MIXQ-rf-a4 PASS rc=0` / `rc=0`, `Result: 984 passed, 21 skipped, 4 excluded --exclude portal_assets` (462.4s and 759.5s); the JSON stream reports `{"total":1009,"passed":984,"failed":0,"invalid":0,"skipped":21,"excluded":4}` and names the skips: 10 `Goals.DispatchNotificationTest` (`SYMMETRY_GOAL_DISPATCH_E2E` gate), 6 `PiControlE2ETest` (`@moduletag skip: true`), 4 `ProviderBrokerE2ETest`, 1 `GoalMachineControllerTest`; the 4 excluded are `PortalControllerTest`'s `portal_assets` cases (the queue's own rule for a branch that does not change `frontend`/`contracts`). The same stream contains the delivered module's three tests, all `passed` |
| E6 contracts | `pnpm install --frozen-lockfile` then `pnpm contracts:check` (`mise exec pnpm@12.17.0`) | exit 0 — `Generated contract DTOs are up to date`, `Validated 21 Draft 7 schemas and 248 fixtures`, `tests 35 / pass 35 / fail 0` |
| E7 grep gate | `rg -n 'assignee_type == "agent"\|status in \["backlog", "ready"\]\|project_active\?\\(\|external_work_available\?\\(\|goal_managed\?\\(\|not goal_managed' control/lib/symmetry_control_web/portal_json.ex` | one match, `:225` `goal_managed = WorkItem.goal_managed?(work_item)` — the served member's own derivation; every gate spelling is gone |
| E8 token compare | `python <HOST-PATH>/gdd/scratch/rf-a4-tokens.py <rev\|WORKTREE> <path> <fn…>`, plus the union over the touched functions ∪ the new module | per-function deltas below; union: base-only ∅, head-only ∅, 89 tokens both |
| E9 control-flow audit | per-function table below | every base exit has a head equivalent with the same atom |
| E10 differential | `RF_A4_DUMP=… mix test --exclude portal_assets test/symmetry_control/workspaces/rf_a4_differential_test.exs` (throwaway, deleted after the head run) | base run 1 = base run 2 = head, 288 lines each; `rf-a4-crosscheck.py` on both dumps: `launch=96 served=64 command=128 mismatches=0` |
| E11 mutants | `mix test test/symmetry_control/workspaces/work_item_eligibility_test.exs` under each mutant (reverted after; module re-hashed) | M1–M6, M8, M9 red; M7 green as predicted (below) |
| E12 credo | `mix credo suggest "lib/symmetry_control/workspaces.ex" "lib/symmetry_control_web/portal_json.ex" --strict --format json` | base 15 issues (all `workspaces.ex`), head 14 (all `workspaces.ex`); `portal_json.ex`, the new module and the new test: 0 |
| E13 citation gate | `python scripts/ci/check-citation-existence.py --base c0784952 --head HEAD` | exit 0 — `no audited document added or modified between c07849521e6b and c07849521e6b` (this slice touches neither `docs/runbooks/**`, `docs/design/quality.md` nor `docs/archive/CLOSED-TODO.md`; the gate compares commits, and this tree is uncommitted, so the run is a "nothing to check" pass, not a per-citation verification — the slice's own citations were checked by hand against the head tree) |
| E14 whitespace / paths | `git diff --check`; `git diff origin/main...HEAD \| grep -i "users/"`; host-path/backslash-u scan over the new files | clean; 0 matches; 0 matches |

### E8 per-function token deltas

| function | base | head | delta |
| --- | --- | --- | --- |
| `portal_json.ex:work_item` | 4 tokens, 28 member keys (`msha1 ad03c42ba2f0`) | 1 token, 28 member keys (`msha1 ad03c42ba2f0`) | `"agent"`, `"backlog"`, `"ready"` moved to the module; the served member keys are unchanged (identical multiset hash) |
| `portal_json.ex:execution` | 1 token (`:can_retry`), sha1 `4585414a` | identical | — |
| `portal_json.ex:execution_summary` | 11 tokens, 17 member keys, sha1 `8ebce661` | identical | — |
| `portal_json.ex:legacy_capabilities` | 3 tokens (`"agent"`, `:can_cancel`, `:can_retry`), sha1 `86235a46` | 2 tokens (the two atoms), sha1 `6d6b21b6` | `"agent"` moved to the module |
| `portal_json.ex:work_item_retry_runtime_eligible?` | 0 tokens | identical | — |
| `workspaces.ex:launch_work_item` | 8 tokens, sha1 `683594b0` | identical | — |
| `workspaces.ex:launch_locked_work_item` | 14 tokens, 7 member keys, sha1 `27253216` | 10 tokens, 7 member keys (same `msha1 8b70872931ce`), sha1 `2eaf4c6e` | `"active"`, `"backlog"`, `"ready"`, `:state_conflict` moved to the module |
| `workspaces.ex:cancel_work_item` | 7 tokens, sha1 `2a06e0b0` | identical | — |
| `workspaces.ex:retry_work_item` | 10 tokens, 6 member keys, sha1 `8a1dc8a2` | 9 tokens, 6 member keys (same `msha1 951b0f9e8458`), sha1 `e3c3a4c7` | `"agent"` moved; `:state_conflict` stays for the missing-attempt refusal |
| `workspaces.ex:ensure_active_project` | 4 tokens, sha1 `989471c9` | identical | — |
| `workspaces.ex:ensure_external_work_available` | 3 tokens, sha1 `792d9bb0` | ABSENT | deleted with its only caller; the module's `retry_external_work/1` carries the identical token set (`sha1 792d9bb0`) |
| `workspaces.ex:ensure_legacy_work_item` | 3 tokens, sha1 `31c44c8e` | 0 tokens | delegated; the module's `cancel_gate/1` carries the identical token set (`sha1 31c44c8e`) |
| `workspaces.ex:provide_work_item_input` | 7 tokens, sha1 `5afb003b` | identical | — |

Module side (head): `start` 1 token; `start_allowed?` 3 (`sha1 31c44c8e`); `cancel_gate` 3
(`31c44c8e`); `cancel_allowed?` 4; `retry_gate` 4; `retry_allowed?` 2; `start_project` 4
(`989471c9` = base `ensure_active_project/1`); `start_state` 4; `retry_external_work` 3
(`792d9bb0` = base `ensure_external_work_available/1`).

### E9 control-flow audit (base `c0784952` vs head)

Every base exit — early return, `with` else clause, `Repo.rollback`, default clause — is matched
with its head equivalent (lesson 32: a move must not drop one). w = `control/lib/symmetry_control/`
`workspaces.ex`, p = `control/lib/symmetry_control_web/portal_json.ex`; base ranges are the ones
inventoried above, head ranges are in the head tree.

| function | base exit | head equivalent | preserved |
| --- | --- | --- | --- |
| `launch_work_item/3` (w:316-347, unchanged text) | `validate_action_id` → else `{:error, reason}`; `valid_uuid?` false → `false -> {:error, :not_found}`; `fetch` → `{:error, :not_found}` | same | yes |
| | pre-transaction and in-transaction `ensure_legacy_work_item` → `:goal_authority_required` | `ensure_legacy_work_item/1` delegates to `cancel_gate/1`, same atom | yes |
| | in-transaction `ensure_active_project` → `:state_conflict` | unchanged | yes |
| | clause 1 caps mismatch → `:idempotency_conflict` | unchanged | yes |
| | `submit_task` / `Repo.update` / `task_snapshot` errors → `rollback(reason)` | unchanged | yes |
| | `{:ok, {snapshot, disposition, should_wake?}}` / `{:error, reason}` | unchanged | yes |
| `launch_locked_work_item/2` clause 2 (w:598-643 → 598-631) | `if project.status != "active" or assignee != "agent" or status not in ["backlog","ready"] or not external_work_available? -> Repo.rollback(:state_conflict)` | `start/1 |> require_ok!()`: project → `:state_conflict`; assignee/status/external → `:state_conflict`; the Goal branch → `:goal_authority_required` is **unreachable** (the in-transaction `ensure_legacy_work_item` ran first on the same locked row) | yes, atoms identical |
| `cancel_work_item/3` (w:349-374, unchanged text) | action-id / `fetch` errors; `ensure_legacy_work_item` → `:goal_authority_required`; missing attempt (`nil ->`) → `{:error, :state_conflict}`; command/snapshot errors | same (the Goal gate delegated) | yes |
| `retry_work_item/3` (w:413-478) | action-id / uuid / `fetch` / pre-transaction Goal gate | unchanged | yes |
| | in-transaction Goal gate → `:goal_authority_required` | `retry_gate/1` clause 1, same atom | yes |
| | in-transaction `ensure_active_project` → `:state_conflict` | `retry_gate/1` clause 2, same atom | yes |
| | in-transaction `ensure_external_work_available` → `:state_conflict` | `retry_gate/1` clause 3, same atom | yes |
| | inline `assignee_type != "agent"` → `:state_conflict` | `retry_gate/1` clause 4, same atom | yes |
| | missing attempt → `:state_conflict`; `retry_task` `:replayed` / `:created` / `{:error, reason} -> rollback(reason)`; dispatch | unchanged (the missing-attempt refusal stays in the function — it is not a work-item gate input) | yes |
| `work_item/2` (p:221-261 → 221-254) | no early return; map literal order unchanged | `can_start` value expression → `start_allowed?/1` | yes |
| `execution/2` (p:288-313 → 285-310) | the three clauses unchanged | only the final `legacy_capabilities/2` arity | yes |
| `execution_summary/2` (p:361-407) | the four clauses unchanged | unchanged | yes |
| `legacy_capabilities/3` → `/2` (p:416-426 → 414-420) | `nil` clause → `nil` | `nil, _work_item` | yes |
| | `&1 and not goal_managed` | `&1 and cancel_allowed?(work_item)` (`cancel_gate == :ok`), same value | yes |
| | `&1 and assignee == "agent" and external_work_available? and project_active?` | `&1 and retry_allowed?(work_item)`, same conjuncts in the same order | yes |
| `ensure_legacy_work_item/1` (w:869-873 → 858) | `{:error, :goal_authority_required}` / `:ok` | `cancel_gate/1` returns the same | yes |
| `ensure_external_work_available/1` (w:861-866) | `{:error, :state_conflict}` / `:ok` | deleted; its only caller collapsed into `retry_gate/1`, same atom | yes |
| `ensure_active_project/1` (w:858-859) | `:ok` / `{:error, :state_conflict}` | unchanged | yes |

### E10 differential

Harness: `test/symmetry_control/workspaces/rf_a4_differential_test.exs` (throwaway, deleted after
the head run; final revision archived at `<HOST-PATH>/gdd/scratch/rf-a4-differential-test-final.exs`).
It builds two projects (the archived one is archived only after its items exist: the base code
refuses to create a work item in an archived project and refuses to archive a project that already
holds a queued attempt), one repository resource and one adapter-backed runtime per project, a real
Goal + revision per project, and fabricates the attempt rows (legacy/goal-owned, queued or
cancelled, with the context snapshot a Goal task requires) through `Repo.insert!`, linking them
with a direct `orchestration_task_id` update because `execution_changeset/2` requires an agent
profile a human-assigned row does not carry. Each job records only the observed class:
`ok:created` / `ok:replayed` / `error:<atom>` for commands and the three served booleans for reads.
Jobs: 96 launch jobs (all six inputs), and for the cancel/retry matrix 64 served + 64 cancel + 64
retry jobs over `goal × project × external × assignee × attempt shape` (a goal-owned attempt exists
only for a goal-managed item — the composite foreign key refuses one on a legacy item).

Results: base run 1 → 288 lines, `1 passed`, exit 0; base run 2 (stability control) → identical;
head → identical; both diffs empty. The harness is live, not blind: it records 96 launch
non-refusals (`ok:created` 48 + `ok:replayed` 48), 8 cancel and 1 retry non-refusals, and both
refusal atoms. `rf-a4-crosscheck.py` re-derives every row's expected class from the base rules and
reports `mismatches=0` for both dumps.

### E11 mutants (each applied to `work_item_eligibility.ex`, test run, then reverted; module
re-hashed at `546efed75669…af923` afterwards)

| mutant | change | result |
| --- | --- | --- |
| M1 | drop the Goal clause from `start/1` | red, `2/3 passed`, row `{true, false, :human, :backlog, :archived, false}` |
| M2 | drop the startable-status conjunct | red, row `{false, false, :agent, :in_progress, :active, true}` |
| M3 | `start_allowed?/1` ignores the launched conjunct | red, row `{false, true, :human, :backlog, :active, false}` |
| M4 | `cancel_allowed?/1` constant `true` | red, row `{true, false, :human, :backlog, :archived, false}` |
| M5 | add the Goal term to `retry_allowed?/1` | red, row `{true, false, :agent, :backlog, :active, true}` |
| M6 | `retry_gate/1` accepts a non-agent assignee | red, row `{false, false, :human, :backlog, :active, true}` |
| M7 | swap `retry_gate/1`'s project and external clauses | green — expected: the three state conditions share `:state_conflict`, so the swap is unobservable |
| M8 | `retry_gate/1` refuses an archived project with `:goal_authority_required` | red, `1/3 passed` (row `{false, false, :human, :backlog, :archived, false}` plus the unloaded-project pin) |
| M9 | check the project before the Goal gate in `start/1` | red, row `{true, false, :human, :backlog, :archived, false}` (the precedence pin) |

### Delivered hashes

`work_item_eligibility.ex` `sha256 546efed75669…af923`; `work_item_eligibility_test.exs`
`8bca6503b82b…`; `workspaces.ex` `d0e9899bd53e…`; `portal_json.ex` `df7663bfb354…`; this plan and
`docs/TODO.md` as committed by the conductor. The differential/probe harnesses and the base
worktree are gone from the tree; `git status` shows the three modified tracked files and the three
new files only.
