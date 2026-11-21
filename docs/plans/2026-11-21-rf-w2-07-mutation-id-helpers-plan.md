# rf-w2-07 mutation-id + versioned-changeset helpers plan (CCS3)

Contract: `goal-rf-w2-07-mutation-id-helpers.md` (GDD, 2026-11-21) and its binding review
amendment (verified-dispatchable check, `<gdd-state>/refactor/plan-review-ds-1.md:228-230`).
Program-plan row: §5 W2-07 (`<gdd-state>/refactor/program-plan.md:463`, risk low, deps W1-14,
dLOC −120); evidence findings `<gdd-state>/refactor/findings/control-core.md` F4 (`:241-257`) and
F7 (`:298-314`). Baseline: this worktree's HEAD `b89e14d722758a53cb546b63e29e2e059df0957c` —
every coordinate below was read at that revision. W1-14 landed as `90465bfb` (PR #139,
`refactor(control): delete unreachable provider and contract helpers`), so the dependency and the
"rebase after W1-14" note are already satisfied in this history.

Two collapses in one increment: six copies of the settlement UUID string formatter in `goals.ex`,
and the optimistic-locking helper set duplicated between `Integrations` and `Workspaces`.
Acceptance: ids and 409 bodies byte-identical.

## Inventory at the baseline (the copies this slice collapses)

### A. `goals.ex` — six settlement mutation-id builders (`:10521-10630`)

| # | symbol | definition | prefix | parts joined with `:` |
| --- | --- | --- | --- | --- |
| 1 | `settlement_mutation_id/3` | `goals.ex:10521-10535` | `settlement` | task_id, run_id, `Integer.to_string(generation)` |
| 2 | `late_validation_outcome_mutation_id/3` | `:10537-10552` | `late-validation-outcome` | task_id, run_id, `Integer.to_string(generation)` |
| 3 | `unstarted_settlement_mutation_id/2` | `:10554-10568` | `unstarted-settlement` | task_id, reason |
| 4 | `automatic_admission_mutation_id/4` | `:10570-10591` | `automatic-admission` | `goal.id`, `Integer.to_string(goal.current_revision)`, `item.id`, purpose, source_identity |
| 5 | `automatic_admission_blocked_mutation_id/2` | `:10593-10610` | `automatic-admission-blocked` | `goal.id`, `Integer.to_string(goal.current_revision)`, candidate_identity |
| 6 | `automatic_reconciliation_deferred_mutation_id/3` | `:10612-10630` | `automatic-reconciliation-deferred` | `goal.id`, `Integer.to_string(goal.current_revision)`, deferred_identity, reason |

Each body is `:crypto.hash(:sha256, "<prefix>:" <> …) |> Base.encode16(case: :lower) |> then(fn
hex -> … end)`; the `then` block is the same 12 slices in all six (only line wrapping differs),
and it is the only `String.slice(hex, …)` site in the file (measured: 6 occurrences at `:10528`,
`:10545`, `:10558`, `:10581`, `:10601`, `:10622`). The formatter discards `hex` nibbles 12 and 16
and substitutes `5` (version) and `a` (variant), so every id is UUID v5-shaped but **not**
RFC-9562-computed. These ids are durable idempotency keys: `GoalEvent.id` for settlement events
and `admission_key` for automatic admissions (`payload["admission_key"]`,
`goals.ex:2787,2830,3983,4034,4205-4210,4792,5971-6017`).

Call sites of the six names: 24 in `goals.ex` (`:1609,2787,2934,2954,3537,3983,4205,4402,4426,
4453,4470,4491,4621,4792,5971,5976,6017,8051`). None outside `goals.ex` (all six are `defp`).

### B. `Integrations` ↔ `Workspaces` versioned-changeset helpers

| helper | integrations.ex | workspaces.ex | measured relation | disposition |
| --- | --- | --- | --- | --- |
| `expected_version/1` | `:1804-1809` | `:1130-1135` | same result; lookup spelling differs (`attr_value(attrs, :version)` = `Map.get(attrs, :version) \|\| Map.get(attrs, "version")` vs the explicit `\|\|`) | one shared definition |
| `matches_version/2` | `:1811-1812` (2 clauses) | `:1144-1149` (3 clauses) | **divergent tail** (see semantics) | one shared definition, verdicts keyed on `expected` |
| `update_with_stale_error/1` | `:1814-1817` | `:1151-1154` | byte-identical | shared |
| `delete_with_stale_error/1` | absent (inlined at `:210-211`) | `:1156-1159` | same call shape | shared; `Integrations.delete_connection/2` adopts it |
| `normalize_stale_result/1` | `:1819-1827` | `:1161-1169` | byte-identical (cond: `:lock_version` → `:stale`; `:provider_action_intents` → `:state_conflict`; else the changeset error; non-error passthrough) | shared |
| `put_attr/3` | `:1834-1838` | `:1282-1286` | byte-identical | shared |
| `drop_attr/2` | `:1840` | absent (inlined in `drop_attrs/2`) | same expression | shared |
| `drop_attrs/2` | `:1842-1844` | `:1270-1272` | same result (reduce of the per-key drop) | shared |

Measured call sites to migrate: `expected_version/1` integrations `:111`, workspaces `:224`;
`matches_version/2` integrations `:146,207,588,906,1534`, workspaces `:129,1139`;
`update_with_stale_error/1` integrations `:167,773,862,921,1338,1361,1492,1543`, workspaces
`:59,109,207`; `normalize_stale_result/1` integrations `:211`; `delete_with_stale_error/1`
workspaces `:132`; `put_attr/3` integrations `:86` + 1 more, workspaces 28 (incl. `:1036-1042,
1085-1097,1288-1290`); `drop_attrs/2` integrations `:85,151`, workspaces `:1075`; `drop_attr/2`
integrations `:150`.

### Staying where they are (measured, with reasons)

- `fetch/2` (integrations `:1794-1802` ≡ workspaces `:794-803`) is a row loader, not part of the
  409/stale contract, and the same helper has a **third** copy in `chat.ex:459` — a shared home
  for a three-way helper is a different slice with an unowned file. Excluded; recorded below.
- `expect_version/2` (workspaces `:1137-1142`) is a workspace-write composite over the shared
  primitives (guard + strip the version keys) with no integrations counterpart; it stays local and
  keeps calling the shared `expected_version/matches_version`.
- `attr_value/2` (integrations `:1832`) and `attr_value/3` + `attr_present?/2` (workspaces
  `:1268,1274-1280`) are different functions (nil-default vs `||` lookup), not copies.
- `delete_changeset/1` in `integrations/schemas.ex:83` / `workspaces/schemas.ex:202`
  (F7's 0.71-similar pair) lives in files this slice does not own.
- `unwrap_transaction/1` is byte-identical in both but is not part of the locking contract.

## Public / API shape

### 1. `goals.ex` — one `mutation_id/2`, six one-line builders

The six names, arities and call sites stay (they name intent at 24 call sites, and W3-10/W4-07
own the surrounding regions); only their bodies collapse:

```elixir
defp settlement_mutation_id(task_id, run_id, generation) do
  mutation_id("settlement", [task_id, run_id, Integer.to_string(generation)])
end
# … five more, same shape

# sha256 over "<prefix>:<part>:<part>…" rendered as the v5-shaped id these durable keys use.
defp mutation_id(prefix, parts) do
  hex =
    :crypto.hash(:sha256, prefix <> ":" <> Enum.join(parts, ":"))
    |> Base.encode16(case: :lower)

  String.slice(hex, 0, 8) <>
    "-" <>
    String.slice(hex, 8, 4) <>
    "-5" <>
    String.slice(hex, 13, 3) <>
    "-a" <> String.slice(hex, 17, 3) <> "-" <> String.slice(hex, 20, 12)
end
```

`Integer.to_string/1` stays at the call sites: the base bodies stringify exactly the integer
fields, and moving the conversion into the helper would make `mutation_id/2` type-polymorphic for
no gain. `parts :: [String.t()]`, `prefix :: String.t()`.

### 2. `SymmetryControl.Schemas.VersionedChangeset` (new file, new `schemas/` dir)

`@moduledoc false` (the established shape for plain-function helper modules in this repo:
`Goals.ChangesetValidators`, `Integrations.HTTP`), alias + qualified calls at both call sites
(the `Goals.ChangesetValidators` precedent):

```elixir
expected_version(attrs)        :: {:ok, pos_integer()} | {:error, :invalid_request}
matches_version(record, expected) :: :ok | {:error, :stale | :invalid_request}
update_with_stale_error(changeset) :: {:ok, struct()} | {:error, :stale | :state_conflict | Ecto.Changeset.t()}
delete_with_stale_error(changeset) :: {:ok, struct()} | {:error, :stale | :state_conflict | Ecto.Changeset.t()}
defp normalize_stale_result(result) :: result (the two clauses above, verbatim; private, no @spec)
put_attr(attrs, key, value)        :: map()
drop_attr(attrs, key)              :: map()
drop_attrs(attrs, keys)            :: map()
```

Bodies are the base bodies moved, not rewritten: `matches_version/2` keeps the workspaces 3-clause
shape with its stale clause keyed on the expected version alone (see semantics); `put_attr/3` is
the base text; `drop_attrs/2` reduces `drop_attr/2` (the integrations spelling, which produces the
same map as the workspaces inline `Map.drop(acc, [key, Atom.to_string(key)])`).

## Exact edge-case semantics

**Id byte-identity.** Base id = `UUID(hex(sha256(prefix <> ":" <> p1 <> ":" <> … <> ":" <> pn)))`
where `UUID` takes nibbles 0-7, 8-11, substitutes `5`, takes 13-15, substitutes `a` + 17-19,
takes 20-31. Head id = `UUID(hex(sha256(prefix <> ":" <> Enum.join([p1, …, pn], ":"))))` — the same
expression because `Enum.join/2` with `":"` is exactly the base `<> ":" <>` chain, including for
`n = 1` and for parts containing `:` or non-ASCII bytes (both are plain byte concatenation; the
hash sees the same byte string). Empty parts stay in place (no trimming). `Integer.to_string/1`
is applied at the same position as before. `Enum.join/2` is more permissive than the base `<>`
chain on a non-binary part: it renders through `String.Chars` (`nil` becomes an empty segment,
atoms and integers their text) where `<>` raised `ArgumentError`, so a `nil` part would silently
collide with `""`. No new reachable failure mode exists only because every reachable caller passes
binaries: `settle_task` (`goals.ex:2752`) and `reconcile_validation_outcome` (`:2901`) guard
`is_binary(run_id)`, `settle_unstarted_task` (`:3968`) guards `is_binary(reason)`, and the
admission identities are `<>`-built before the builder runs (`automatic_candidate_identity`
`:4656`, `external_wait_identity` `:10612`); a future caller must pass binaries itself.

**Cross-family uniqueness.** Distinct prefixes keep ids distinct for identical parts; the
evidence table asserts the six ids are pairwise distinct on one shared part vector (a formatter
that dropped the prefix would collide).

**`matches_version/2`, verdicts keyed on `expected`.** Base integrations returned `{:error,
:stale}` for any non-match — a stale row and a `nil` locked re-read alike — including a non-integer
`expected`; base workspaces returned `{:error, :invalid_request}` when the record has a
`lock_version` and `expected` is not an integer. The shared version keys the verdicts on `expected`
alone: `%{lock_version: version}, version` → `:ok`; any record with an integer `expected` →
`:stale`; any non-integer `expected` → `:invalid_request`, the verdict the same module's
`expected_version/1` gives the same malformed input (the HTTP gate
(`PortalApiController.version_param/1`, `:535-545`) normalizes an integer string to the integer
and answers 400 for anything else, so a non-integer version never reaches either context). The
stale clause deliberately does not require `%{lock_version: _}` on the record:
`mark_resource_syncing/1` (`:594`) and `record_resource_failure/3` (`:1544`) pass whatever the
locked re-read returned — `nil` when the resource was deleted mid-transaction — and base
integrations answered those `:stale` (409), not 400.

Observable delta: `Integrations.delete_connection(connection_id, expected)` with a non-integer
`expected` returns `{:error, :invalid_request}` instead of `{:error, :stale}`. That delta is not
reachable through production code: the only production callers are
`PortalApiController.delete_connection/2` (behind `version_param/1`) and — post-W1-14 — nothing
else (measured `grep -rn "delete_connection(\|delete_resource(" lib/`: two controller sites).
`Integrations.delete_connection/2` has no `@spec`; `Workspaces.delete_resource/2` is specced
`pos_integer()`. Of the five remaining `matches_version/2` call sites in `Integrations`,
`update_connection/2` (`:147`, `|| Repo.rollback(:not_found)`), `delete_connection/2` (`:214`,
`fetch/2`) and the work-item delivery lock (`:915`, behind its `match?/2` guard) always pass locked
structs; `mark_resource_syncing/1` (`:594`) and `record_resource_failure/3` (`:1544`) may pass
`nil` and keep the base `:stale` verdict. No test pins the base behaviour (all
`delete_connection`/`delete_resource` test calls pass integers). The delete-path delta is pinned by
one new test and both `nil`-record paths by two new tests (evidence plan, gate E7).

**`normalize_stale_result/1` order.** Unchanged cond order: `:lock_version` → `:stale` before
`:provider_action_intents` → `:state_conflict` before the changeset passthrough; a non-`{:error,
changeset}` result passes through (success tuples and non-changeset errors alike,
`normalize_stale_result(result), do: result`).

**`put_attr/3` key style.** One rule, unchanged: if the attrs map carries **any** binary key, the
put uses the binary form; else the atom form. `drop_attr/2`/`drop_attrs/2` drop both spellings and
return the map unchanged when neither is present.

**409 bodies.** The reason atoms returned by the contexts are untouched, so
`SymmetryControlWeb.Protocol.error/2` renders the same status/code/message bytes. Evidence:
real HTTP captures of the stale/state_conflict/invalid_request/success scenarios before and after
(gate E3).

## Evidence plan (all gates FOREGROUND; one full suite at a time on this host)

Recipe: `sh <gdd-state>/scratch/rf-w2-07-mix.sh <mix args>` — portable Elixir 1.20.4 / OTP 29,
`MIX_ENV=test`, `MIX_TEST_PARTITION=rf-w2-07` (own DB `symmetry_control_testrf-w2-07`),
`PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset, deps a read-only copy of the main
checkout's tree (identical `mix.lock` md5 `99cf712f…`) at `Q:/repos/wt-w2-07-mutation-id/.deps`.

| gate | command | expected |
| --- | --- | --- |
| E1 base byte-identity of the six ids | `mix run --no-start w207-mutation-ids.exs <base goals.ex> <head goals.ex>` | one table, 6 families × N rows, `base_id == head_id` per row, `mismatches 0`; the harness compiles both sides from the *source text* through the same path (probe module, `defp` → `def`), so the comparison is base expression vs head expression, not a reimplementation |
| E2 base 409/success bodies | capture test on the base tree, `mix test <capture>` | body TSV recorded at `<evidence>/bodies-base.txt` |
| E3 head 409/success bodies | same command after the change | `<evidence>/bodies-head.txt`, `cmp` against E2 → identical |
| E4 targeted suites (head) | `mix test test/symmetry_control/goals_test.exs test/symmetry_control/integrations_test.exs test/symmetry_control/workspaces_test.exs test/symmetry_control/orchestration_review_test.exs` | exit 0; counts recorded |
| E5 format + compile | `mix format --check-formatted`, `mix compile --warnings-as-errors` | exit 0, empty / 0 warnings |
| E6 duplicate-definition census | `sh rf-w2-07-count.sh <base|head>` | measured: base `TRACKED-TOTAL 25` (6 builders + 19 helper clauses across the two contexts, worst case 3 clauses for `matches_version/2`), head `18` (6 builders + `mutation_id/2` + 11 clauses — 8 helpers — all in the shared module); UUID formatter normalized blocks 1 form × 6 copies → 1 form × 1 copy with an unchanged form-text sha256; a planted local duplicate moves the count (M5) |
| E7 new pins | the four added tests plus the frozen durable-key pins (below) | green on head; the integrations delete pin is red on base for the non-integer version; the two sync-delete pins are red on d9c3e7d8 (clause 2 requires `%{lock_version: _}`) and green after the fix |
| E8 mutation checks | M1 drop the prefix from `mutation_id/2`; M2 change `-5` to `-4`; M3 keep the old 2-clause `matches_version/2` tail in the shared module; M4 delete the `:lock_version` branch from `normalize_stale_result/1`; M5 plant a duplicate `expected_version/1` in `workspaces.ex`; M6 make `put_attr/3` always use the atom spelling; M7 accept `version == 0` in `expected_version/1`; M8 swap the part order inside each of the four builders the family pins cover; M9 restore the `%{lock_version: _}` requirement on `matches_version/2` clause 2 | observed: M1/M2 → E1 red (1200/1200 mismatches; M1 also `collisions=1`); M3 → both delete pins red (integer versions keep E3 identical); M4 → the concurrent race test (`integrations_test.exs:97`) red, E3 canonically unchanged because a non-race stale write is pre-checked by `matches_version/2`; M5 → E6 count 1→2 and `compile --warnings-as-errors` fails on the unused function; M6 → the capture's first write raises in `Ecto.Changeset.cast`; M7 → the capture bytes differ; M8 → the four family pins red (`goals_test.exs:5343/5350/5599/7803`) while the settlement/late pins stay green; M9 → both sync-delete pins red (integer versions keep E3 identical). Each reverted, `md5sum -c` restore proof |
| E9 whitespace | `git diff --check` | exit 0, empty |
| E10 full suite | `mix test` (conductor gate-runner per contract; run here only if no other suite is concurrent) | exit 0; counts recorded |
| E11 profile-path grep | `git diff origin/main...HEAD \| grep -i "users/"` | no match |

**Transient harness files** (deleted before delivery, content + md5 recorded in the evidence dir):
`control/test/w207_capture_test.exs` (E2/E3: drives the real router with `TestConn.api_conn()` +
`PortalFixtures`, dumps `name\tstatus\tcontent-type\traw body` per scenario),
`control/w207-mutation-ids.exs` (E1), `<gdd-state>/scratch/rf-w2-07-count.sh` (E6).

**New pins.** `integrations_test.exs`: `delete_connection/2` with a non-integer version returns
`{:error, :invalid_request}` (the resolved unified rule; red on base); a resource deleted before
`mark_resource_syncing/1`'s `FOR UPDATE` re-read and one deleted during the provider call both keep
`Integrations.sync_resource/1` on the base `{:error, :stale}` (red while clause 2 requires
`%{lock_version: _}` on the record). `workspaces_test.exs`:
`delete_resource/2` with a non-integer version returns `{:error, :invalid_request}` (unchanged
behaviour, now pinned so the shared clause cannot drift). `goals_test.exs:3667`: the settlement
and late-validation-outcome receipts' `mutation_id`s are asserted against the frozen id expression
(`frozen_mutation_id/2`, the delivered bytes kept independently of `Goals.mutation_id/2`) next to
the existing inequality refutation, and the same frozen expression pins the remaining four
durable-key families at scenarios that already read those ids: the blocked admission event's own
id and the automatic-admission key in its payload (`:5270`), the deferred event ids (`:5554`), and
the `task_unstarted_settled` event id (`:7749`). All six families now have a standing byte pin.
The first two are new one-call tests; the rest are assertions inside existing scenarios; no
assertion is deleted, renamed or weakened.

## Design trade-offs

1. **Keep the six named builders, collapse only their bodies.** Deleting them and calling
   `mutation_id/2` at the 24 call sites would widen the diff into regions W3-10 (`goals.ex`
   dispatch) and W4-07 (facade split) own next, and would drop the intent names that make
   `admission_key` and settlement-event ids readable. Contract clause "one `mutation_id/2` for the
   6 settlement ids" is met by one formatter with six one-line callers.
2. **Shared module scope: locking core + attrs key helpers, not `fetch/2`.** `put_attr/drop_attr/
   drop_attrs` are on the same pipelines as the version guard (they normalize the attrs map that
   carries `:version`, and `drop_attr(:version)` is the strip step of the same transaction), so
   they belong to one "write a versioned row from a JSON attrs map" module; `fetch/2` is a read
   helper with a third copy in an unowned file, and moving it would rename nothing while touching
   16 read call sites. F7 lists both; the boundary is recorded here rather than glossed over.
3. **`matches_version/2` verdicts keyed on `expected`.** See semantics; the alternative (keep the
   base integrations 2-clause tail) would make `Workspaces.delete_resource/2` answer 409 for a
   malformed version argument, which contradicts the module's own `expected_version/1` verdict and
   the controller's 400 gate; requiring `%{lock_version: _}` on the record instead turns a row
   deleted mid-transaction into 400 where both bases answer 409. One atom changes on an
   HTTP-unreachable, typespec-violating call, and it is pinned; the missing-row verdict is pinned
   by the two sync-delete tests.
4. **Alias + qualified calls, no `import`.** Repo convention (`Goals.ChangesetValidators`,
   `Integrations.Providers.Support`); `import` would keep call-site text identical but hides the
   ownership of the 409/stale contract at 60 call sites.
5. **`@moduledoc false` + one-line `@spec`s.** Matches the two existing plain-function helper
   modules; the specs are the only documentation of the three-way `matches_version/2` result.
6. **No `fetch/2`, no `delete_changeset/1` merge.** Both are named in F7 but outside the
   contract's Change clause / owned set; recorded as boundaries, not silently dropped.

## Out of scope (contract clause 4, with reasons)

- Result dispatch flattening (W3-10) and the `goals.ex` facade split (W4-07): the mutation-id
  region stops at the six builders plus `mutation_id/2`.
- Goal schema validators (W2-05, landed as `bda93b11`) and provider access (W4-06): untouched.
- `goals_test.exs`: frozen mutation-id assertions only — the settlement and late-validation
  receipt equalities (`:3667`) and one byte freeze per remaining durable-key family at scenarios
  that already read those ids (`:5270`, `:5554`, `:7749`); no fixture or usage case is reworked
  (W1-12 (#149) owns the fixture usage rewrite there).
- `fetch/2` (three copies, one unowned) and `delete_changeset/1` (unowned files), as above.
- `chat.ex`, `integration_provider_stub.ex`, the portal controllers: no edits; the controllers are
  the *oracle* for the bodies, not part of the change.
- No new dependency, no `credo:disable`, no suppression, no `docs/TODO.md` entry closes (measured:
  no entry names this duplication).

## Residual risks

- The ids are opaque durable keys; the table in E1 covers 6 families × the input classes that reach
  them (uuid parts, integer generations incl. 0 and 2^63, parts containing `:` and non-ASCII), but a
  call site outside `goals.ex` cannot exist (all six are `defp`) and the 24 in-file call sites are
  untouched by the diff.
- The unified `matches_version/2` tail is a deliberate, documented micro-change (one atom on a
  typespec-violating direct call): a missing record keeps `:stale` and a non-integer `expected` is
  `:invalid_request`, both pinned by the delete and sync-delete tests. If a reviewer prefers byte
  parity there, the revert is the two integrations clauses — but that leaves a second definition,
  which the contract's failure control forbids.
- `deadcode`-style reachability tooling does not exist for Elixir here; the E6 census is the
  instrument, with the planted-duplicate check showing it bites.
- Full `mix test` and the CI `control` job remain the conductor's gate-runner steps per the
  contract; the local tree is left uncommitted for verification.
