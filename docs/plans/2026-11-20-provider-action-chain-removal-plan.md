# provider-action-chain-removal plan — delete the callerless projected provider-action chain

Contract: `goal-provider-action-chain-removal.md` (GDD, 2026-11-20). Baseline for every coordinate in
this plan and in the delivered change: `origin/main` at
`fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, the worktree's HEAD). Source entry:
`docs/TODO.md` "Revalidate and remove the callerless projected provider-action chain" under
`## Dead-code candidates` (the line numbers its prose cites were written against an earlier
revision and are treated as claims to re-derive, not coordinates to trust).

## Deliverable shape

1. `control/lib/symmetry_control/integrations.ex`: delete the projected provider-action chain —
   `execute_provider_action/5` and its catch-all clause, the private
   `execute_authenticated_provider_action/7` and `persist_provider_delivery/5` — together with the
   unreachable `:capability_not_granted` member of the disjunct inside it. Nothing else changes:
   every helper the chain used is still referenced by a live path, so no helper is deleted.
2. `control/test/symmetry_control/integrations_test.exs`: one regression test that pins the module
   surface (the projected variant is absent, the unprojected sibling is present). It fails at
   `fdf332d`, passes after the deletion; both runs are recorded in `answer.md`.
3. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md`: the entry moves verbatim with a closure
   paragraph naming the branch, the decision and the pinning test, per `AGENTS.md`.
4. No other source, test, CI or contract file is edited. In particular
   `control/test/symmetry_control_web/controllers/provider_action_controller_test.exs` stays
   untouched (it is owned by the open flake entry, and the deletion forces no edit there: nothing
   in it names the projected chain).

## Revalidation record (zero callers, tree-wide, at `fdf332d`)

Run against all tracked files, with the entry's own citation excluded so it cannot mask or
manufacture a hit (`docs/TODO.md` is the entry itself):

```
$ git grep -n "execute_provider_action" -- . ':!docs/TODO.md'
control/lib/symmetry_control/integrations.ex:282:  def execute_provider_action(
control/lib/symmetry_control/integrations.ex:310:  def execute_provider_action(_, _, _, _, _), do: {:error, :invalid_request}

$ git grep -n "execute_authenticated_provider_action" -- . ':!docs/TODO.md'
control/lib/symmetry_control/integrations.ex:296:      execute_authenticated_provider_action(
control/lib/symmetry_control/integrations.ex:561:  defp execute_authenticated_provider_action(

$ git grep -n "persist_provider_delivery" -- . ':!docs/TODO.md'
control/lib/symmetry_control/integrations.ex:574:        persist_provider_delivery(connection, resource, work_item, operation, delivery)
control/lib/symmetry_control/integrations.ex:581:  defp persist_provider_delivery(connection, resource, work_item, operation, delivery) do
```

Interpretation: `execute_provider_action` has no referent at all (only its two definition clauses);
the two private helpers each have exactly one caller, and that caller is the chain itself
(`:296` inside `execute_provider_action/5`, `:574` inside
`execute_authenticated_provider_action/7`). Escapes checked so the search cannot miss a dynamic
user: no `apply(Integrations, :execute_provider_action, …)`, no captured remote call
(`&Integrations.…`), no `function_exported?(Integrations, …)` anywhere in the repository
(`git grep -n -e "apply(Integrations" -e "&Integrations\\." -e "function_exported?(Integrations"`:
no hits), and no string/atom spelling of the name outside the two clauses above. The one non-code
mention is this TODO entry (and this plan), both of which the deletion updates.

The entry's decision fork — "dead surface (delete the chain and the disjunct together)" versus
"missing its caller (the disjunct needs a producer)" — is settled by this search plus the
contract's authority line ("no backward compatibility is required; first release not shipped"):
zero callers tree-wide means dead surface, so the deletion branch applies. No product decision
remains open. (Had a live caller existed, the fork would have needed the owner; it does not.)

## Deletion set, and why nothing else becomes dead

Deleted in `integrations.ex`:

| Deleted | Evidence it is callerless |
| --- | --- |
| `execute_provider_action/5` (:282-308) + catch-all (:310), public | zero referents tree-wide (search above) |
| `execute_authenticated_provider_action/7` (:561), private | sole call `:296`, inside the deleted clause |
| `persist_provider_delivery/5` (:581), private | sole call `:574`, inside the deleted clause |
| the `:capability_not_granted` member of `{:error, reason} when reason in [:stale, :state_conflict, :capability_not_granted]` (:596-597) | dies with its only containing clause; on that path the value has no producer (see semantics below) |

Kept, with the surviving reference that keeps each one alive:

- `execute_unprojected_provider_action/5` and `/6` (:313-352) — called from
  `control/lib/symmetry_control/integrations/provider_access.ex:1578`; contract says keep the live
  path and its tests.
- `persist_provider_action/4` — also called from `project_provider_action_delivery/5`
  (`integrations.ex:458`), which `provider_access.ex:2243` uses.
- `project_provider_action_delivery/5`, `unprojected_provider_action_result/4` — called from
  `provider_access.ex:2243`, `:2254`, `:2270`.
- `delivery_attrs/2`, `provider_action_result/5`, `provider_action_delivery/1`,
  `valid_provider_action_delivery?/1`, `record_provider_action_failure/3`,
  `mutation_failure_outcome/1`, `provider_action_error/1`, `require_capabilities/2` — each still
  has at least one caller outside the deleted chain (`delivery_attrs/2` at `:432` and `:963`;
  `provider_action_result/5` at `:461`, `:471`, `:492`; `record_provider_action_failure/3` at
  `:656`, `:659`; `mutation_failure_outcome/1` via `unprojected_provider_action_failure/1` at
  `:399`; `provider_action_error/1` at `:1196`; `require_capabilities/2` at `:331` and the
  `require_capability/2` wrappers).

Apply-stage note (2026-11-21, findings S1/S4): the `record_provider_action_failure/3` row above —
and the matching sentence in `Deliverable shape` item 1 ("every helper the chain used is still
referenced by a live path") — overstate liveness: both surviving call sites sit inside
`execute_accepted_resource_sync/2`, a function with zero callers tree-wide (its two definition
clauses only; `provider_access.ex` calls the `_unprojected` sibling), and its `outcome` input was
consequently dead; the parameter (`/3` -> `/2`) was removed in the apply stage. The callerless
function and `provider_access.ex`'s producerless `provider_failure/1` 3-tuple clause are recorded
in `docs/TODO.md` (`## Dead-code candidates`) for a separate maintenance change.

The deletion is therefore exactly the entry's fix direction: the chain and the disjunct inside it,
nothing more. `mix compile --warnings-as-errors` is the mechanical backstop: any of the kept
helpers that had silently depended on the chain would surface as an unused-private warning.

## Exact edge-case semantics the entry states

- **`:capability_not_granted` in the deleted disjunct.** The `with` at `:293` runs
  `require_capabilities/2` before the provider call, and `persist_provider_action/4`'s transaction
  body never calls it; the clause after `persist_provider_action/4` could therefore only see
  `:stale`, `:state_conflict`, `:not_found`, a passed-through changeset error, or a
  `Repo.rollback` reason. The member was unreachable *on that clause's only input*, which is one
  more reason the clause belongs to the dead chain rather than to the live path — and it is
  deleted together with the clause, never "fixed" by adding a producer to a chain with no caller.
- **The `:provider_action_intents` disjunct of `normalize_stale_result` stays.** The entry notes
  it "cannot fire from this path" — the projected path's only changeset
  (`WorkItem.delivery_changeset/2`) declares no such error key — but it is *not* dead globally:
  `delete_connection/2` (`integrations.ex:205-212`) pipes `Connection.delete_changeset/1` through
  `normalize_stale_result/1`, and that changeset declares
  `no_assoc_constraint(:provider_action_intents)` (`integrations/schemas.ex:87`), which is exactly
  the error key the disjunct maps to `:state_conflict`. Deleting it would remove a live refusal
  path; the entry's "delete the chain and the disjunct together" therefore binds only the disjunct
  *inside the chain* (the `:capability_not_granted` clause above). The reading is recorded here
  because the entry's compressed sentence can be read against this disjunct too.
- **Failure classification on the deleted chain is not re-pinned.** `record_provider_action_failure/3`
  is called with `:definite` in the deleted clause and `:ambiguous`/`mutation_failure_outcome/1`
  in `persist_provider_delivery/5`; no test asserts the deleted clause's classification (the
  integration test at `integrations_test.exs:976` that pins "a pre-dispatch capability refusal is
  classified definite" drives `execute_unprojected_provider_action/5`, whose own `with` produces
  the same definite classification through `unprojected_provider_action_failure/1`), so deletion
  changes no pinned outcome. The surviving test's comment names "projected guards"; the guards it
  describes are the unprojected path's own shared guards (`:330-333`), which are unchanged, so the
  comment is not rewritten by this change (noted in the report).
- **The controller surface is untouched.** The public HTTP behavior for provider actions flows
  through `ProviderAccess` (`provider_access.ex:1578`, `:2243-2270`) and is unchanged; the
  provider-action controller tests reference `ProviderAccess`, not `Integrations.execute_provider_action`,
  so the deletion forces no edit in the flake-owned file.

## Regression test design (contract's before/after requirement)

The deletion's observable surface is the module's exported API: before, `execute_provider_action/5`
resolves; after, it must not, while the live sibling must. The test pins exactly that:

```
refute function_exported?(Integrations, :execute_provider_action, 5)
assert function_exported?(Integrations, :execute_unprojected_provider_action, 5)
assert function_exported?(Integrations, :execute_unprojected_provider_action, 6)
```

- It **fails at `fdf332d`** (the projected variant is exported) and **passes after** the deletion;
  both runs are recorded with exact commands and outputs in `answer.md`.
- It is not a tautology over the diff: `function_exported?/3` reads the compiled module, not the
  source; it is the same shape the repository already uses to pin a dead surface
  (`portal_realtime_test.exs:114` refutes `PortalChannel.handle_in/3`).
- It pins both directions: reviving a callerless second variant (the recorded risk) and
  accidentally dropping the live unprojected entry points both redden it.
- Placement: `integrations_test.exs`, the module that owns the surviving provider-action tests,
  after the last provider-action case; the flake-owned controller test is not touched.

## Evidence plan (all commands at `fdf332d`+working tree, Elixir via the scratch recipe)

1. Pre-fix regression run: `sh <scratch>/provchain-mix.sh test test/symmetry_control/integrations_test.exs:<line>`
   — expected: 1 failure on the `refute`.
2. Deletion, then the same command — expected: green.
3. `sh <scratch>/provchain-mix.sh format --check-formatted`; `MIX_ENV=test … compile --warnings-as-errors`
   (the CI control-job order); both expected exit 0.
4. Full control suite: `sh <scratch>/provchain-mix.sh test` with
   `MIX_TEST_PARTITION=provider-action-chain-removal` (unique to this goal), after staging the
   frontend bundle for the four `:portal_assets` cases
   (`mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile` at the repo root, then
   `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build`), matching CI's control job.
5. Provider broker end-to-end suite, this area's own E2E in CI:
   `SYMMETRY_PROVIDER_BROKER_E2E=1 sh … test test/symmetry_control/provider_broker_e2e_test.exs`.
6. `git diff --check`; docs-only edits afterwards re-checked for whitespace.
   No frontend/browser/daemon/scripts path is touched, so no other path-triggered check applies;
   the measurement/benchmark-vs-full-suite sequencing rule has no harness in this increment.

## Out of scope (contract clause 4)

- The other open `docs/TODO.md` entries (the two markerless notes in the same section, the `conn`
  parameter entry, the provider-action flake entry, and the retro follow-ups).
- Any widening of the deletion beyond the three functions and the disjunct they contain; any
  refactor of the surviving unprojected path; any change to `normalize_stale_result` (live, see
  above).
- The entry's decision fork is settled by evidence (zero callers) and the contract's authority
  line; no product question is handed back.

## Design trade-offs and decisions

1. **Delete the chain rather than annotate or `@doc false` it.** The if `first release not shipped`
   boundary plus zero referents makes annotation dead weight; a public `def` that hides a missing
   referent from the compiler is exactly the failure mode the entry records.
2. **A surface pin as the regression test rather than a behavior test.** A deletion has no runtime
   behavior to exercise; inventing an artificial caller to assert against would recreate the dead
   surface the change removes. The surface assertion is deterministic, isolated, and matches the
   repository's existing dead-surface pin.
3. **Keep the disjunct reading explicit in the closure record.** Two disjuncts appear in the
   entry's prose; the closure states which one the deletion binds and why the other stays, so a
   later reader does not "finish the job" by deleting a live refusal path.
4. **No new helper or script committed.** The evidence runs are one-off commands; a committed
   harness would be new dead weight.

## Residual risks

- The surface pin proves absence of the export, not that no caller was silently reaching it (only
  the tree-wide search and the compiler can show that). Both are recorded.
- Line numbers cited above are at `fdf332d` and drift; the closure entry names symbols, not just
  lines.
- The full-suite run is a snapshot of a parallel-agent host; a failure outside `control/`-scoped
  reach would be reported as such, not silently attributed to this change.
