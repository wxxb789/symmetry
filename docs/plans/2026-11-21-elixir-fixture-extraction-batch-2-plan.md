# Plan: Elixir fixture extraction batch 2 (web + integrations) — rf-w3-03

Contract: `goal-rf-w3-03-elixir-fixtures-2.md` (W3-03, continuing `docs/plans/2026-11-21-elixir-fixture-extraction-plan.md`,
batch 1 = #149). Every count and every path in this plan was re-derived at HEAD `a503eab7` with
`#149` (`b89e14d7`), `#161` (`c9ed23b4`) and `#163` (`a503eab7`) merged, because the contract's
re-derivation predates them (`task_attrs`/`command_current` line numbers and the `uuid` counts all
moved). Re-derivation method: extract every `def`/`defp` clause of `control/test/**/*.exs` and
`control/test/support/*.ex`, dedent the clause text by the definition's indentation, and group by
(name, clause text) — a group with ≥2 members is a byte-identical clone; grouping is always
reported per name **and** arity.

## Deliverable

(1) one shared definition per interchangeable duplicated builder in `SymmetryControl.TestFixtures` /
`SymmetryControlWeb.TestConn`, imported by the converting files; (2) the served-document gate block
in one home, `SymmetryControlWeb.ServedDocument`, with the two web suites importing it and their
`@schema_bundle` attributes gone; (3) the integration suite's local sync fixtures in
`SymmetryControl.IntegrationFixtures`; (4) `docs/TODO.md`'s served-document item closed into
`docs/archive/CLOSED-TODO.md` with its bindings repointed.

### Converted helpers (before → after)

"before" = definitions in the owned files at `a503eab7`; "after" = definitions in owned files on the
converted tree. Counts are name+arity scoped.

| helper | before (owned defs) | after | converted files | identical group |
| --- | --- | --- | --- | --- |
| `assert_error/3` | 5 | 1 (`TestConn`, pre-existing) | goal_machine, operator_read, protocol, provider_action, task_goal_guard | 5 of 5 web copies byte-identical to `TestConn.assert_error/3`'s body |
| `bearer/2` | 4 | 1 (`TestConn`, new) | goal_controller, goal_machine, protocol, provider_action | all 4 byte-identical |
| `bearer/1` → `TestConn.operator_conn/1` | 1 | 0 (`operator_conn/1` is the pre-existing operator path) | operator_read | body identical to task_goal_guard's local `operator_conn/1` |
| `operator_conn/1` | 1 (task_goal_guard, header-only) | 0 (`TestConn.operator_conn/1`) | task_goal_guard | task_goal_guard's copy is body-identical to operator_read's `bearer/1` |
| `enforce_csrf/1` | 1 | 1 (`TestConn`, new) | portal_workflow | byte-identical to `portal_session_test.exs:415` (non-owned, keeps its copy) |
| `create_resource/5` | 2 | 1 (`TestConn`, new) | portal_resources, portal_workflow | the two copies byte-identical (round-1 fix; `api_conn/0` + `POST …/resources` + `json_response(201)`) |
| `unix_signal/1` (2 clauses each) | 4 clauses | 2 clauses (`TestFixtures`, new) | pi_control, provider_broker | round-2 fix; both clause pairs byte-identical (the two source copies and the new home share `91f42c84…` / `0f298da3…`) |
| `command_current/4` | 2 (+1 support definition) | 1 (`TestFixtures.__using__/1`) | goal_machine, task_goal_guard | the two copies are a byte-identical 19-line clone |
| `subject/1` | 2 | 1 (`TestFixtures`, new) | goal_machine, task_goal_guard | the two copies byte-identical |
| `validation_profiles/0` | 3 (`/0`; one of them a default-arg clause) | 2 (`TestFixtures`, new + `portal_workflow`'s pinned copy — Left local — reason) | task_goal_guard (imports `/0`), goal_machine (its clause becomes the arity-1 variant, which imports the shared `/0` for its default path) | task_goal_guard's copy byte-identical to `goals/workers_test.exs:178` (non-owned, keeps its copy); `portal_workflow`'s copy pins its own `allowed_runtime_ids` and is not interchangeable |
| `check_contract/0` | 1 | 1 (`TestFixtures`, new) | goal_machine | byte-identical to `goals_test.exs:9480` and `goals/dispatch_notification_test.exs:699` (non-owned, keep their copies) |
| `repository_fixture/1` | 1 | 1 (`TestFixtures`, new) | goal_machine | byte-identical to `goals_test.exs:9591` (non-owned, keeps its copy) |
| `uuid/0` | 3 | 1 (`TestFixtures`, new) | operator_read, protocol, provider_action | all 3 byte-identical (`defp uuid, do: Ecto.UUID.generate()`) |
| `register_runtime/2` | 1 (operator_read's own spec builder) | 1 (`TestFixtures.register_runtime/2,3`, pre-existing) | operator_read | value-identical rewrite; see Edge cases §2 |
| `claim/2` | 1 (operator_read's own) | 1 (`TestFixtures.claim/3,4`, pre-existing) | operator_read | value-identical rewrite; see Edge cases §2 |
| `task_attrs/1` | 1 (operator_read's copy) | 1 (`TestFixtures.task_attrs/0,1`, pre-existing) | operator_read | same 4-key base merge; text differs (`@task_base` attribute) |
| `uuid/1` | 2 | 1 (`ServedDocument`, new) | portal_resources, portal_workflow | the two copies byte-identical |
| `waiting_decision_payload/0` | 2 | 1 (`ServedDocument`, new) | portal_resources, portal_workflow | the two copies byte-identical |
| `assert_served_members!/3` | 2 | 1 (`ServedDocument`, new) | portal_resources, portal_workflow | the two copies byte-identical |
| `assert_served_rows!/3` | 2 | 1 (`ServedDocument`, new) | portal_resources, portal_workflow | the two copies byte-identical |
| `gate_members/3` | 2 | 1 (`ServedDocument`, new) | portal_resources, portal_workflow | the two copies byte-identical |
| `declared_members/2` | 2 | 1 (`ServedDocument`, new) | portal_resources, portal_workflow | the two copies byte-identical |
| `definition_variants/2` (2 clauses) | 4 clauses | 2 clauses (`ServedDocument`, new) | portal_resources, portal_workflow | both clause pairs byte-identical |
| `contract_bundle/0` | 2 | 1 (`ServedDocument`, new) | portal_resources, portal_workflow | the two copies byte-identical |
| integrations sync fixtures (9 helpers) | 9 `defp` (all in `integrations_test.exs`) | 9 (`IntegrationFixtures`: 7 public + 2 module-private `defp`) | integrations_test | single-source move, bodies unchanged; the stub module moves with them and the two samplers stay private; see Edge cases §6 |

The served-document move also deletes both `@schema_bundle` attributes
(`portal_resources_test.exs:220`, `portal_workflow_test.exs:23`) — each was referenced only by
`contract_bundle/0` (`grep` shows one use per file) — and `ServedDocument`'s existing
`@schema_bundle` (path `../../../daemon/internal/contracts/schema_bundle.json` from
`control/test/support/`) resolves to the same file the web path
`../../../../daemon/internal/contracts/schema_bundle.json` resolved to, so the attribute's path is
re-based by construction, not by a new literal.

### Left local — reason

Every same-name/arity family the converted files still define, and every cloned family the other
owned files define, is listed here. "cloned" = byte-identical copies exist.

Change (1) scopes conversion to byte-identical bodies and requires the same-name families left
local to be recorded here, so a family listed here is not a converted helper left duplicated:
the contract's Failure control reads against the converted families.

| family | owned definitions (files) | reason left local |
| --- | --- | --- |
| `project_fixture/0` | integrations_test:1986, goal_machine:1293, provider_action:3244 (+4 non-owned) | 8 distinct bodies in 9 files (each pins its own project name/key literal); the only byte-identical pair is `workspaces_read_model_test.exs:635` = `workspaces_test.exs:551`, both non-owned. No owned pair is interchangeable, and converting one copy while the others stay file-local would leave a two-definition family in the owned set (contract Failure control). |
| `project_fixture/1` | none (goals_test:9579, workspaces_completion:642, non-owned) | default-arg variant, non-owned files |
| `repository_fixture/1` | goal_machine converted; goals_test:9591 and goals/dispatch_notification_test.exs:647 (different body) | non-owned survivors |
| `enroll_machine/0` | operator_read:513 (+4 non-owned) | 12 distinct bodies in 12 files (different names, tokens, clocks; `orchestration_test.exs:2236` differs from `orchestration_review_test.exs:724` only by option order, still not byte-identical); no interchangeable pair with an owned member |
| `enroll_machine/1` | none (5 non-owned) | all bodies differ (`name \\ "retry-machine"` vs `name \\ "portal-machine"` vs required arg, three tokens, two clocks) |
| `enroll_machine/2` | chat_test:1345 (+ goal_admission:3858) | two distinct bodies (`enrollment_token: "enroll"` vs `"test-enrollment-token"`, different names) |
| `goal_attrs/0` | none (workers_test:189) | non-owned, single body |
| `goal_attrs/1` | goal_machine:1146, task_goal_guard:166 (+ goals/dispatch_notification_test.exs:657) | 3 distinct bodies (different titles, objectives, `allowed_model_profiles`/`allowed_resource_ids`); the arity-1 family is not interchangeable |
| `goal_attrs/3` | pi_control:2241 (+ goals_test:9420, which is a default-arg variant) | two distinct bodies; goals_test's clause is a 3-default-arg variant (batch-1 plan's "default-arg variant" case) |
| `acceptance_contract/0` | portal_workflow:1631, task_goal_guard:230 (+ workers_test:233) | conductor decision P1-2: `PortalFixtures.acceptance_contract/1` already owns the acceptance-contract surface with a different pinned shape; the arity-0 copies stay file-local. `task_goal_guard`'s copy is a third distinct body ("A separate check is required.") |
| `execution_policy/0` | provider_action:3593 (+ goals_schema:628, workspaces_test:598) | conductor decision P1-2 (`PortalFixtures.execution_policy/0` clash); the three test-file copies are byte-identical but the fence is explicit |
| `session_attrs/1` | native_harness_kind_binding:176, goal_machine:1218 | two distinct bodies (one derives `repository_resource_id` from the item, one calls `get_in/2`); arity-2 copy in goals_test |
| `create_project/1` | portal_integrations:940, portal_resource_identity:127, portal_resources:1555, portal_workflow:1496 (+ `PortalFixtures.create_project/2`) | HTTP-layer helper: each file's adapter binds its own name/key prefix (`"Resource project #{key}"`, `"Project #{key}"`, …) and journals through `POST /portal/api/projects`; the natural home `SymmetryControlWeb.PortalFixtures` is explicitly not owned (contract P1-3: not converted — different layer) |
| `create_project/0` | goal_controller:623, portal_controller:437, portal_goal_attention:587, portal_integrations:940 (default-arg clause) | per-file HTTP adapters over `PortalFixtures.create_context_project/2` (`"Goal API project"`/`"G"`, `"SYM"`/`"Symmetry"`, …); same fence |
| `register_runtime/3` | goal_machine:1327 | HTTP-layer helper (`conn`, `machine_id`, `token` → `POST /api/v1/machines/:id/sessions/:sid`); same name+arity as the shared fence builder but a different layer |
| `claim/3` | protocol_controller:1316 | HTTP-layer helper (`conn`, `machine_token`, `runtime_id` → dispatch + `POST /runs/:id/claims/:claim_id`); same name+arity as `TestFixtures.claim/3` but a different layer |
| `create_resource/2`, `create_work_item/3`, `create_work_item/1` | portal_resource_identity:129, portal_resources:1575, portal_workflow:1521, portal_controller:439 | HTTP-layer helpers; the `create_work_item` bodies differ (the workflow copy injects `status`/`priority`) |
| `enforce_csrf/1` | portal_workflow converted; `portal_session_test.exs:415` keeps its byte-identical copy | non-owned survivor. Re-derivation of the contract clause's paths: `portal_session_controller_test.exs` and `channels/portal_realtime_test.exs` do not exist in the tree — the actual files are `portal_session_test.exs` and `portal_realtime_test.exs` (its `operator_conn/0` at `:17` matches no converted helper by name+arity) — so the conditional extension does not reach the byte-identical sibling, which stays outside the re-derived owned set |
| `validation_profiles/0` | portal_workflow:1634 (kept) | the kept body pins `allowed_runtime_ids: ["00000000-0000-4000-8000-000000000001"]` for the `:goals` app-config registry write in `enable_goal_rollout/0`; it is not byte-identical to the shared fresh-id builder, so Change (1)'s conversion scope leaves it local — converting it would change the registered profile value |
| `sync_ci/4`, `sync_delivery/4`, `sync_resource/3`, `check/2`, `authenticate/1`, `validate_resource_reference/3`, `execute/6`, `github_headers/1`, `azure_devops_headers/1`, `request/4`, `respond_with/2` | inline stub modules in integrations_test, portal_integrations_test, portal_resources_test, provider_action_controller_test, resource_sync_*_test, router_invalidation_coverage_test | module-level `def` behaviour implementations (provider stubs), not per-suite fixtures; the shared `IntegrationProviderStub`/`IntegrationAuthStub`/`IntegrationHTTPStub` support modules are the existing mechanism for the common path, and each stub exists to inject a file-local failure that the shared stub does not model |
| `goal_command/4` | goal_controller:643 | HTTP-layer envelope reader (GET the goal, POST the command); same name as `TestFixtures.goal_command/3,4` but a different layer and different arity semantics (batch-1 plan already records the name collision) |
| `goal_fixture/1`, `goal_fixture/2`, `work_item_fixture/3`, `history_fixture/0`, `runtime_fixture/0,1`, `transition/4,5`, `baseline_subject/1`, `evidence_attrs/2`, `plan_proposal/2`, `usage_attrs/1`, `task_result/3`, `errors_on/1`, … | the owned files above | every cross-file pair either differs in body (checked by the same grouping) or has no owned support home; they were already recorded as out of scope by the batch-1 plan ("other duplicated helper names inside the owned set … not among the contract's converted names") and stay recorded for the follow-up extraction slice |
| e2e host plumbing: `git_output_bytes!/2` | pi_control, provider_broker (byte-identical pair) | cannot move alone (it delegates to the file-local `git_output!/2`, which the standalone support-tree compile under `--warnings-as-errors` cannot call from `.exs` files) and its family is not interchangeable (pi_control routes `git_output!/2` through `system_cmd_options/1`'s Windows `use_stdio` path; provider_broker does not), so a shared body changes one file's `System.cmd/3` options; review round 2 accepted the reason. `unix_signal/1` was moved into `TestFixtures` in round 2 (the conductor allowed no exception) — see the Round-2 review fix subsection |
| remaining e2e helpers (`await!/4`, `await_loop!/5`, `command_current/3,4` in the two e2e files, `daemon_command/2`, `start_daemon!/*`, `git_output!/2`, `run_git!/2`, `subject_hash!/1`, `digest!/1`, `os_process_alive?/1`, …) | pi_control, provider_broker | the two files' copies are **not** byte-identical (different ports, build dirs, operator refs, option lists, clocks), so they are not interchangeable; the batch-1 plan already lists the e2e copies for this slice and the grouping shows only the plumbing pair above is a clone |

## Public shape

`SymmetryControl.TestFixtures` (extended; each new function's doc states the values it pins):

```elixir
subject(resource_id)            # {"resource_id", commit: 40×"a", tree_digest: "sha256:" <> 64×"4"}
validation_profiles()           # [test: [kind: :check, profile_digest: "sha256:" <> 64×"c",
                                #         enabled: true, allowed_runtime_ids: [fresh uuid]]]
check_contract()                # the "An independent check must pass." acceptance contract
repository_fixture(project)     # Workspaces.create_resource(kind: "repository", unique name)
uuid()                          # Ecto.UUID.generate/0 — the converted suites' fresh-id one-liner
unix_signal(:term | :kill)      # "-TERM" / "-KILL", round-2 fix: the flag the e2e suites pass to System.cmd("kill", …)
```

`SymmetryControlWeb.TestConn` (extended):

```elixir
bearer(conn, token)   # header-only: authorization "Bearer <caller's own token>"
enforce_csrf(conn)    # drops the CSRF skip api_conn/0 installs
create_resource(project, connection, kind, name, external_ref)  # POST …/resources + json_response(201)
```

`SymmetryControlWeb.ServedDocument` (extended; the module keeps `validate_served_document!/2`):
`uuid/1`, `waiting_decision_payload/0`, `assert_served_members!/3`, `assert_served_rows!/3`,
`gate_members/3`, `declared_members/2`, `definition_variants/2`, `contract_bundle/0`. All eight were
`defp`; they become `def` with the comments they carried as `@doc`s. `definition_variants/2` keeps
both clauses.

`SymmetryControl.IntegrationFixtures` (extended; seven public functions, the two samplers stay private):

```elixir
sync_lock_order_task(resource, provider_items, owner, ref)   # unboxed sandbox Task at the prelock barrier
work_item_row_locked?(id)                                    # FOR UPDATE SKIP LOCKED probe
assert_sync_waiting_on_work_item(backend, blocker, deadline)  # pg_stat_activity/pg_locks sampler
use_credential_leak_provider()
use_blocking_connection_check_provider()
provider_work_item(provider_data)
provider_snapshot(provider, updated_at, title, external_state, provider_data)
```

`sync_wait_sample/2` and `format_sync_wait_sample/1` are `defp`: their only caller,
`assert_sync_waiting_on_work_item/3`, is in the same module. The file also defines
`SymmetryControl.Integrations.CredentialLeakProviderStub` (moved out of `integrations_test.exs`).

`tracked_resource!/1` stays untouched (three other suites import it).

## Edge-case semantics the contract pins

1. **Byte-identical conversion.** Every converted body is moved text-for-text; only the indentation
   (dedent/re-indent), the `defp`→`def` keyword, and comments-turned-`@doc`s differ. The answer
   records per-function sha256 of the dedented body text before and after (equal iff the body
   moved), plus the sha256 of the two byte-identical 48-line source regions
   (`portal_resources_test.exs:1665-1712` = `portal_workflow_test.exs:1896-1943`, `diff` empty).
2. **operator_read's converted builders keep their fresh ids.** The file's `register_runtime/2`
   minted `uuid()` for `daemon_instance_id` and its `claim/2` minted `uuid()` for `claim_id`; after
   conversion the call sites pass them explicitly (`register_runtime(machine, @now,
   daemon_instance_id: uuid())`, `claim(run, runtime, @now, claim_id: uuid())`), so the SQL traffic
   is unchanged: same `Orchestration.register_runtimes/4` entry, same spec map, same fresh ids
   (contract P1-3: "operator_read call sites pass explicit ids").
3. **`command_current/4` canonicalisation is a no-op for every converted call site.** The injected
   `command_current/3,4` canonicalises only `"request_decision"` payloads. goal_machine's single
   `request_decision` site passes `%{kind: "plan", work_item_id: nil, subject_hash: nil,
   proposal: proposal}` (canonical already) and its other four sites use `resolve_decision` /
   `accept_plan` / `activate` / `admit_task`, which pass through; task_goal_guard's single
   `request_decision` site has the same canonical shape and its four others pass through. Option
   precedence is preserved: local `Keyword.put_new(opts, :validation_profiles, …)` ≡ injected
   `Keyword.merge(command_opts(), opts)` when `command_opts/0` supplies exactly that default, and
   goal_machine's explicit `validation_profiles: validation_profiles(runtime.id)` override still
   wins (the option list order changes; `Goals.command/4` reads options by key).
4. **`operator_conn/1` adds an `accept` header; no controller reads it.** The contract makes
   `TestConn.operator_conn/1` the operator-token path for the arity-1 copies
   (operator_read `bearer/1`, task_goal_guard's header-only `operator_conn/1`). The added
   `accept: application/json` header is the header every portal request already carries;
   `git grep -n "get_format" control/lib` finds no format negotiation, so the header cannot change
   any response the suites assert. Recorded as the one intentional request-header change.
5. **Test names unchanged.** No test is renamed, retagged, moved or skipped; no `async` flag
   changes; no test body is edited except the operator_read call sites in §2 and the
   import/attribute lines. The JSON-formatter name list is compared before/after.
6. **Move-only for the integration fixtures.** The nine helpers move with their bodies and
   comments; their `alias`/`import` surface (`Sandbox`, `Repo`, `Integrations`,
   `CredentialLeakProviderStub`, `ExUnit.Callbacks.on_exit/1`) is added to the support module. The
   `CredentialLeakProviderStub` module moves into the same support file (round-1 fix), so
   `sync_lock_order_task/4` calls it statically and the support tree compiles the stub it drives —
   no `apply/3`, and no `respond_with/2 is undefined` warning. `sync_wait_sample/2` and
   `format_sync_wait_sample/1` stay module-private: their only caller,
   `assert_sync_waiting_on_work_item/3`, moved with them.
7. **`docs/TODO.md` closure.** The PR #145 item moves into `docs/archive/CLOSED-TODO.md` (an
   audited document) with a closure statement; its symbol bindings are repointed to
   `control/test/support/served_document.ex` (both old web files would make the binding false), and
   the citation gate is run on the resulting revision.

## Evidence plan

All foreground, serially; never a suite next to a measurement harness. Portable-toolchain recipe
(pattern of `rf-w3-11-mix.sh`, this worktree): Elixir 1.20.4 / OTP 29.0.6 via `erl.exe`,
`MIX_TEST_PARTITION=rf-w3-03`, `MIX_DEPS_PATH` pointed at the main checkout's `deps/`,
`PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset, local PostgreSQL required.

1. Before: named-definition table (`grep`/clause counts per converted helper, owned files), the
   byte-identity groups, and the two 48-line region hashes.
2. Per converted file, a targeted `mix test <file>` as it converts.
3. `mix format --check-formatted`; `MIX_ENV=test mix compile --warnings-as-errors`;
   `mix test test/symmetry_control_web/` and the two e2e files.
4. Mutation red run: flip `TestFixtures.validation_profiles/0`'s `enabled` field (`true` → `false`),
   run `mix test test/symmetry_control_web/controllers/task_goal_guard_controller_test.exs` and show
   the red, then restore byte-identically and re-run.
5. After: the same named-definition table; move-only hashes; the full `mix test` run with the
   JSON formatter, compared to the pre-change name list (`--exclude portal_assets` where the
   frontend bundle is not staged, the documented host path).
6. `git diff --check`; `python3 scripts/ci/check-citation-existence.py --base <merge-base> --head <tree>`
   (a `git stash create` revision of the worktree so the uncommitted closure is visible);
   the branch diff (`git diff origin/main...HEAD`) contains no user-profile path prefix (program rule).

CI-only oracles (not runnable on this host): the control job's format + warnings-as-errors + full
suite + provider-broker e2e + goal-receipt e2e steps.

## Out of scope (with reasons)

- Batch-1 files (`goals_test`, `workspaces_*`, `orchestration*/**`, `migrations/*`) and `test_helper.exs`
  / `config/test.exs` (W3-14) — owned by other slices.
- `control/test/support/portal_fixtures.ex` — explicitly not owned; the `PortalFixtures` clashes
  (`acceptance_contract`, `execution_policy`, the HTTP builders) stay there per P1-2/P1-3.
- Production code, new fixtures for behaviour not already asserted, giant-test splitting (W3-15),
  and `async` conversion.
- The e2e host-plumbing helper `git_output_bytes!/2` — the round-1 review fix and review round 2
  both accept keeping it local: it cannot move without its non-interchangeable `git_output!/2`
  family (standalone support-tree compile boundary, and a shared body changes one file's
  `System.cmd/3` options on Windows). Recorded in "Left local — reason". `unix_signal/1`, left
  local in round 1, moved into `TestFixtures` in round 2 (the conductor allowed no exception) —
  see the Round-2 review fix subsection.
- Pre-existing dead code found while re-deriving: none added to `docs/TODO.md` because the
  contract fences `docs/TODO.md` edits to the closure; anything found is reported in the answer.

## Design trade-offs and documented contract deviations

- **`project_fixture`, `enroll_machine`, `goal_attrs` are not added to `TestFixtures`.** The
  contract names them as extension candidates, but Change (1) scopes conversion to byte-identical
  bodies and the measured grouping shows no interchangeable pair with an owned member (8/12/6
  distinct bodies). Adding a shared definition whose other copies keep different bodies would
  leave a two-definition family in the owned set. Recorded instead in "Left local — reason".
- **`acceptance_contract`/`execution_policy` are not added** (P1-2, unchanged).
- **9 of the 11 integration helpers move.** `project_fixture/0` and `connection_fixture/2` stay
  local in `integrations_test.exs`: each pins file-specific values and each has another file-local
  copy in the owned set (`goal_machine`, `provider_action`), so moving them would create the
  two-definition family the Failure control refuses. The other nine are single-source in that file
  and move cleanly.
- **`uuid/0` is converted** (three byte-identical owned copies) rather than left local: it is an
  interchangeable duplicated builder, and `TestFixtures` is the only owned fixtures home.
- **`create_resource/5` is consolidated into `TestConn`** (round-1 fix). The two owned copies were
  byte-identical HTTP clones and the conductor decision requires a single home; `TestConn` is the
  owned module that already carries the connector mechanics, and it takes the
  `@endpoint SymmetryControlWeb.Endpoint` dispatch attribute the `Phoenix.ConnTest` request macro
  needs, exactly as `PortalFixtures` does. Call sites keep their shape through the import.
- **`operator_conn/1` is converted in task_goal_guard** although the contract's Change (4) names
  only the operator_read arity-1 copy: it is the same name+arity as `TestConn.operator_conn/1` and
  the operator-token path the contract designates; the `accept` header is inert (§4).
- **`enforce_csrf/1` is converted into `TestConn`**: it is the session helper Change (4) routes to
  `TestConn`, and its one owner pair is byte-identical (the non-owned `portal_session_test.exs`
  copy stays).
- **Goal-machine's `validation_profiles` clause loses its default argument** (`runtime_id \`
  `Ecto.UUID.generate()` → `runtime_id`), so the arity-0 path resolves to the shared
  `TestFixtures.validation_profiles/0` (same fresh id, same profile body) and the arity-1 variant
  stays local; among the converted copies this leaves one definition per name+arity, while
  portal_workflow's distinct-body `/0` copy stays recorded in "Left local — reason".

## Verification owner

Sol (this stage) runs every gate in the foreground and records real exit codes and counts; an
independent read-only review follows in the review stage.

## Implementation record (as converted)

The converted tree holds one shared definition per name+arity for every helper the deliverable
table converts; families left local keep their file-local copies, recorded in "Left local —
reason". The definitions live at:

* `control/test/support/test_conn.ex` — `bearer/2` at `:43`, `enforce_csrf/1` at `:46`,
  `create_resource/5` at `:50` (round-1 fix; it dispatches through the `@endpoint
  SymmetryControlWeb.Endpoint` attribute at `:20`), and `assert_error/3` at `:65` is the
  pre-existing body the five web copies carried.
* `control/test/support/test_fixtures.ex` — `uuid/0` at `:76`, `subject/1` at `:171`,
  `check_contract/0` at `:180`, `validation_profiles/0` at `:193`, `repository_fixture/1` at
  `:205` (the injected command surface stays in `__using__/1`), and `unix_signal/1` at `:232`
  (round-2 fix).
* `control/test/support/served_document.ex` — `uuid/1` at `:51`, `waiting_decision_payload/0` at
  `:60`, `assert_served_members!/3` at `:87`, `assert_served_rows!/3` at `:95`, `gate_members/3` at
  `:108`, `declared_members/2` at `:122`, `definition_variants/2` at `:132`, `contract_bundle/0` at
  `:140`.
* `control/test/support/integration_fixtures.ex` — `sync_lock_order_task/4` at `:59`,
  `work_item_row_locked?/1` at `:84`, `assert_sync_waiting_on_work_item/3` at `:102`,
  `sync_wait_sample/2` at `:120` (`defp`), `format_sync_wait_sample/1` at `:172` (`defp`),
  `use_credential_leak_provider/0` at `:189`, `use_blocking_connection_check_provider/0` at `:200`,
  `provider_work_item/1` at `:215`, `provider_snapshot/5` at `:232`, plus the moved
  `CredentialLeakProviderStub` module at `:260`.

Move-only proof: per-function body sha256 (dedented body, header line excluded) before and after —
recorded in the answer — shows every moved body byte-identical in its new home, with exactly two
documented exceptions, all text-only and value-identical:

1. `command_current/3,4` in goal_machine and task_goal_guard: the shared definition is the
   pre-existing injected surface (Edge cases §3); the deleted copies' body text differs by
   construction.
2. `task_attrs/1` in operator_read: the shared definition merges over the `@task_base` attribute
   instead of restating the same four keys.

The first revision's third exception is gone with the round-1 fix: the `CredentialLeakProviderStub`
module now compiles in the same support file, `sync_lock_order_task/4` calls
`CredentialLeakProviderStub.respond_with/2` statically again, and its normalized-clause sha256
matches its source copy (`6480dc27…`).

Two ordering details the converted files had to respect: the `use SymmetryControl.TestFixtures,
command_opts: [...]` option list is expanded where the `use` sits, so the
`import SymmetryControl.TestFixtures, only: [..., validation_profiles: 0]` line precedes it in both
converted suites (a local `defp` would not: batch-1's files carry their own `validation_profiles/0`);
and `subject/1`'s parameter keeps the source name `repository_id`.

### Per-function move-only hashes

sha256 of each clause's normalized text (dedented, `defp` → `def`), computed over the owned files at
`a503eab7` and on the converted tree. "shared sha" = the hash the before-side copies and the new
home share; a row whose counts changed keeps the hash of the copy that stayed unchanged.

| helper (name/arity) | home after | before defs (owned) | after defs (owned) | shared normalized-clause sha256 |
| --- | --- | --- | --- | --- |
| `assert_served_members!` | ServedDocument | 1 | 1 | `18c8c96b9099f85d7a5cc4b9f2a319e3baf093b83a976a1f5eaa7d4303e840fb` |
| `assert_served_rows!` | ServedDocument | 1 | 1 | `5e2a3d0dc9ac22ddbbbdce76c0ad75b570c202637a970ec5d2743200bcba3286` |
| `gate_members` | ServedDocument | 1 | 1 | `b9cb9e4a64f2438623951cb9826762c123d63dca529880fefa0302ad7121d403` |
| `declared_members` | ServedDocument | 1 | 1 | `8dcb556aa18b1a6117691a3f6a80b92a9bab78dabd451dd011bb3b9d288e663d` |
| `definition_variants` | ServedDocument | 2 | 2 | `4a534088915008ec8b73c37f022b5bc6702f71f36a467e4d51f533d904553edd` |
| `contract_bundle` | ServedDocument | 1 | 1 | `77c198431ef1c6d8dfb0f1ee41d21a1119c8fb32dc4f617e9f4a11f67e2da0f9` |
| `waiting_decision_payload` | ServedDocument | 1 | 1 | `828248899e3c65f6436de01798f7a7e4ff955ae851a384aa714a46c121e130ca` |
| `assert_error` | TestConn | 1 | 1 | `55ab7565a6d006a3cf787ad35ae2c215e44b73a364439887821d42eba78b2021` |
| `bearer` | TestConn | 2 | 1 | `efab688ead7687fde9d98739a6476b46182e384e4881001e087349f972c26a52` (the `/2` one-liner; the `/1` copy is the operator-token path, Edge cases §4) |
| `enforce_csrf` | TestConn | 1 | 1 | `d9a18eb25fab2e3b64e7b5a76fed86f7c77f755c84850f984fda48c912aa39d6` |
| `create_resource` | TestConn | 2 | 1 | `c1a41089412ed1ec26bc72b8275a1e90f3a175328fe75f3f57081adf5b2a4ac6` (round-1 fix; both source copies byte-identical to the new home) |
| `unix_signal` | TestFixtures | 4 clauses | 2 clauses | `91f42c845e72437798e95dad347459e67785142b6aab95db858623d117fdefc8` (`:term` clause; the `:kill` clause is `0f298da3510e3a2635eea7cd924375bfbb0b449e931f40b613b4a81c1d99ed31`; round-2 fix, both source pairs byte-identical to the new home) |
| `subject` | TestFixtures | 1 | 1 | `151c2069c6967ba7bd7faf6c53cc8bf3c6b84f709fd408e645acb9208d71c456` |
| `check_contract` | TestFixtures | 1 | 1 | `cad03c430dda8ae70c1d6a7428751a256841e152948608c285202ea72a4fd458` |
| `repository_fixture` | TestFixtures | 1 | 1 | `44618a9ecc4a2bce8a17e4a360fb9d5ece145656c3cbbfa6c24cb4f1260c931b` |
| `validation_profiles` | TestFixtures | 1 | 1 | `5402eb80fd29e54ae713bf6a453bc4cec8a2ec9fcb86402c9e87ad9e6700c3a1` |
| `uuid` | ServedDocument (`/1`) + TestFixtures (`/0`) | 2 | 2 | `b0cd259a37f0a5dbe81250472c14204f3041bceb73d6ddc120abdc82b96f3e10` (`/1`; three `/0` copies share the empty-body hash of the one-liner `def uuid, do: Ecto.UUID.generate()`) |
| `sync_lock_order_task` | IntegrationFixtures | 1 | 1 | `6480dc2750194adfcb4b90e4be1b7b6eae5ad59ba4d945fb8a9acd4336af5c36` (round-1 fix: static stub call restored; the earlier `apply/3` revision had no shared hash) |
| `work_item_row_locked?` | IntegrationFixtures | 1 | 1 | `ab92dfa6c43d781feafd82f964789040003d10bc463a4fd30663cb9cd941386e` |
| `assert_sync_waiting_on_work_item` | IntegrationFixtures | 1 | 1 | `fde389e2d60cb14209313218e19629316d04e56359ce670b7fb46145b3cc24ad` |
| `sync_wait_sample` | IntegrationFixtures | 1 | 1 | `6524c963377c1f472cf70d4af80c8b21989db04c3014d4605a010eb120b835a8` |
| `format_sync_wait_sample` | IntegrationFixtures | 1 | 1 | `99d756f915bc1c005be94c10d2448623426bac83e029e3dbc653bc33095a5aac` |
| `use_credential_leak_provider` | IntegrationFixtures | 1 | 1 | `03a3f9855cd6f82b9207693d3fe778249d5127026c3043778a8b3a839dd8c72c` |
| `use_blocking_connection_check_provider` | IntegrationFixtures | 1 | 1 | `87969f645a9b5f8d667b59e7f9958ee33eb3a8918c2393a6544788ed2347cf92` |
| `provider_work_item` | IntegrationFixtures | 1 | 1 | `284c7b837930412c3c4e197d6a423297f3a14a3b5c592418b3d5bd45af0e176f` |
| `provider_snapshot` | IntegrationFixtures | 1 | 1 | `33c4d64196584c0709af2ed9b27e01f58709b428781fd392dbdaecdbacd6fde9` |
| `command_current` | TestFixtures (`__using__/1`) | 2 | 1 | `a9076ca290a44e5eecdb55b9935d34bd0124a27e2dd70e2f462eb646aaf59060` (the pre-existing injected clause; exception 1) |
| `task_attrs` | TestFixtures | 2 | 1 | `a13ae60cfd9b05e2b85fb49d28adfc1f0294a97e43b5c1e773c7395e416966be` (the pre-existing clause; exception 2) |

`definition_variants/2`'s two clauses hash separately and both survive
(`4a534088915008ec8b73c37f022b5bc6702f71f36a467e4d51f533d904553edd` covers the pair; the
`:root` one-liner's clause text is byte-identical too).

### Gate results (measured)

Host recipe: portable Elixir 1.20.4 / OTP 29.0.6, `ERL_SCHEDULERS=4`,
`MIX_TEST_PARTITION=rf-w3-03`, local PostgreSQL, `--exclude portal_assets` (this worktree has no
staged frontend bundle; the four excluded cases are `portal_controller_test.exs`'s bundle gates,
unaffected by this change and covered by the CI control job).

| gate | command | result |
| --- | --- | --- |
| format | `mix format --check-formatted` | exit 0 |
| compile | `MIX_ENV=test mix compile --warnings-as-errors` | exit 0 |
| web | `mix test test/symmetry_control_web/ --exclude portal_assets` | 275 passed, 1 skipped, 4 excluded, 0 failed, exit 0 |
| e2e | `mix test test/symmetry_control/pi_control_e2e_test.exs test/symmetry_control/provider_broker_e2e_test.exs` | 2 passed, 10 skipped (host/environment-tagged), exit 0 |
| full suite | `mix test --exclude portal_assets` | 976 passed, 21 skipped, 4 excluded, 0 failed, exit 0 |
| mutation | flip `enabled: true` → `false` in `TestFixtures.validation_profiles/0`, run `task_goal_guard_controller_test.exs` | red: `{:error, :invalid_validation_profile}`, 1/2 passed, exit 2; restored byte-identically (`sha256:171c5cce4c2b89a1ff1752974cea19a45ac82474e8687f350fa371feb98ac3db`) and green again (2 passed, exit 0) |
| test names | static declaration set (`test "..."` heads) HEAD vs converted tree; runtime `(module, name)` sets of both full runs | 936 literal declarations identical (every loop-generated name sits in untouched generator code); runtime sets identical: 1001 names each, 0 missing, 0 added, same 21 skipped / 4 excluded. The 7 state differences are baseline-side host load: one 100 ms mailbox window in the pre-change `integrations_test.exs` run and six `goal_0006_migration_test.exs` cases that hit a 4000 ms `DBConnection` queue drop — all seven pass on the converted tree, and migrations are outside this change. |
| diff hygiene | `git diff --check`; branch-diff scan for a user-profile path prefix | exit 0; 0 matches |
| citation gate | `python3 scripts/ci/check-citation-existence.py --base a503eab7 --head <worktree snapshot>` | exit 0 (after repointing the archived item's bindings; the first run refused one stale symbol binding, recorded in the answer) |

### Round-1 review fix (opus review round 1)

The round-1 review's P2-1 (two dead imports and their compiler warnings), P3-1 (runtime `apply/3`
dispatch to a stub defined in a test file) and P3-2 (stale shipping-revision note) are fixed on
revision `61f8980526` (named in `docs/archive/CLOSED-TODO.md`'s revision note):

- **P2-1:** `format_sync_wait_sample: 1` and `sync_wait_sample: 2` were dead entries in
  `integrations_test.exs`'s `only:` import list (their only caller,
  `assert_sync_waiting_on_work_item/3`, moved into the support module with them); the entries are
  removed and both helpers are `defp` in `IntegrationFixtures` again, byte-identical to their
  source text.
- **P3-1:** `CredentialLeakProviderStub` moved from `integrations_test.exs` into
  `control/test/support/integration_fixtures.ex` (`:260`) and `sync_lock_order_task/4` calls it
  statically again (no `apply/3`); the support tree compiles the stub, so the static call cannot
  warn.
- **`create_resource/5`:** consolidated into `TestConn` (`:50`) per the conductor decision for the
  round-1 duplicates; both source copies and the new home share the normalized-clause sha256 in the
  per-function table above.

Escalations carried from the fix report:

- `git_output_bytes!/2` and `unix_signal/1` were left local in round 1 — the reasons recorded in
  "Left local — reason". Review round 2 accepted the `git_output_bytes!/2` reason (compile boundary
  plus non-interchangeable `git_output!/2` family) and rejected the `unix_signal/1` one (a
  placement preference, not a compile boundary or behaviour difference); the conductor required its
  consolidation, done in the Round-2 review fix subsection.

Reruns on the round-1 fix revision `61f8980526` (portable toolchain recipe,
`MIX_TEST_PARTITION=rf-w3-03`, `--exclude portal_assets`): format exit 0; `MIX_ENV=test mix compile
--warnings-as-errors --force` exit 0; `mix test test/symmetry_control/integrations_test.exs` 32
passed, 0 `warning:` lines; `mix test test/symmetry_control_web/` 275 passed, 1 skipped, 4 excluded,
exit 0, 0 `warning:` lines; the two e2e files 2 passed, 10 skipped, exit 0; the other three
`IntegrationFixtures` importers (`integrations/syncer_test.exs`,
`resource_sync_identity_loss_test.exs`, `resource_sync_inflight_marker_test.exs`) load with 0
`warning:` lines. One earlier web run on the same bytes hit a host-load flake at
`provider_action_controller_test.exs:1960` (`{:DOWN, …, :noproc}` where the test pins `:killed`);
the file passed alone (79 passed) and the rerun above is green. The full suite stays with the
conductor gate-runner.

### Round-2 review fix (opus review round 2)

Review round 2 accepted every round-1 fix and left one item: `unix_signal/1` (its P2-1) had to be
consolidated. The conductor allowed no exception, so it is fixed on revision `d556d26c04` (named
in `docs/archive/CLOSED-TODO.md`'s revision note):

- `SymmetryControl.TestFixtures` gains `unix_signal/1` (`:232`) with a one-line `@doc` and a
  moduledoc that now says "data builders and host-signal helpers"; the `:term`/`:kill` clause pair
  is byte-identical to both deleted copies (`91f42c84…` / `0f298da3…`).
- `pi_control_e2e_test.exs` and `provider_broker_e2e_test.exs` each `import
  SymmetryControl.TestFixtures, only: [unix_signal: 1]` and drop their local `defp` pairs; both
  callers keep their own `System.cmd` invocation.
- `git_output_bytes!/2` stays local with the reason review round 2 accepted.

Reruns on the round-2 fix revision `d556d26c04` (portable toolchain recipe,
`MIX_TEST_PARTITION=rf-w3-03`): format exit 0; `MIX_ENV=test mix compile --warnings-as-errors
--force` exit 0; the two e2e files 2 passed, 10 skipped, exit 0, 0 `warning:` lines. The full
suite stays with the conductor gate-runner.
