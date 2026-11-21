# portal-json plan — `PortalJSON` consolidation: keyed evidence getter (CW-8a) and one delivery resolver (CW-8b)

Contract: `goal-rf-w2-04-portal-json.md` (GDD, 2026-11-21; conductor decision "reconciled contract
review round 2" binding). Baseline for every coordinate here: `857e763b8b6c175b87e3f6413a4541d15bbb6e41`
(`857e763b`, the W1-13 merge, this worktree's HEAD = origin/main). Touched packages: `control/lib/**`
(one module), `control/test/**` (one test file), `docs/` (this plan, one TODO decision note). No
schema, fixture, generated-output or frontend change; the wire document is not supposed to change at
all — that is the increment's acceptance condition.

Owned files (contract): `control/lib/symmetry_control_web/portal_json.ex`; the four portal/attention
controller test files; `docs/TODO.md` / `docs/archive/CLOSED-TODO.md`; this plan. Test-file partition
with W2-03 (controller consolidation): **non-portal** controller tests stay with W2-03;
`portal_resources_test.exs`, `portal_workflow_test.exs`, `portal_integrations_test.exs` and
`portal_goal_attention_test.exs` are this slice's (declared in the PR).

## Verified state at the baseline

`PortalJSON` (`control/lib/symmetry_control_web/portal_json.ex`, 727 lines, `@moduledoc false`)
projects eight wire documents. Two of its pieces are the increment:

**CW-8a — the hand-rolled key getter.** `value/2` is eight near-identical clauses,
`:698-717`: seven `defp value(map, "<key>") when is_map(map), do: Map.get(map, "<key>") || Map.get(map, :<key>)`
(`summary`, `message`, `question`, `findings`, `changed_artifacts`, `changed_files`, `tests`) plus
`defp value(_map, _key), do: nil`. Callers: `outcome/5` (`:470`, `:476`, `:478`) — the agent's
result JSON (`result` is `task.result`/`run.result`, string keys) and, through `list_value/3`
(`:719`), the same three sites. Every new shipped result field costs another clause, and the two
spellings must stay in step by hand.

**CW-8b — the 6-argument delivery resolver.** `delivery/2` (`:489-520`) resolves three members
(`pull_request`, `ci`, `review`) through `effective_delivery/6` (`:522`, `:535`, `:567`, `:574`,
`:584`, `:587`) — six heads, two call arities (`/6` with a `delivery_context/3` (`:607-611`) map and
a `/4` continuation), a `field` atom switch (`:url` for `pull_request`, `:status` for `ci`/`review`)
that decides both where the resolved value is placed and which `status` fallback applies
(`nil`/`"unknown"`/`"none"`), `normalize_delivery/2` (`:590-595`) adding the members a branch did not
write, and `provider_pull_request_state/2` (`:613-616`) overriding `status` afterwards. The agent
head (`:574-581`) serves the **whole** evidence map it is handed.

Wire contract: `WorkItemDelivery` / `WorkItemDeliveryValue` in `contracts/v1/workspace-read.schema.json`
(shared definition body also served by `work-detail-read` and `chat-read`; `chat-read.schema.json:155-165`
is the same body), `required = value, source, generation, url, status`, `additionalProperties: true`.
`delivery` is served from `PortalJSON.work_item/2` (workspace read, activity feed), `outcome/5`
(work detail, chat outcome) and `work_item_detail/3` — one projection body, three reads.

What the branches actually serve today (derived from the code and pinned by the new matrix test):

| branch | precondition | served member set | values |
| --- | --- | --- | --- |
| provider | `external_*` is a binary and `!= ""` | `value`, `source`, `provider`, `generation`, `recorded_at`, field, `url`, `status` | `value = <provider value>`; `source = "provider"`; `provider = context.provider`; `generation = nil`; `recorded_at = iso8601(context.recorded_at)`; the member's field carries the value; `status` = the value for `ci`/`review`, and for `pull_request` `external_pull_request_state \|\| "unknown"` after the override |
| manual | no provider value, `pull_request_url`/`ci_status`/`review_status` binary and `!= ""` | `value`, `source`, `generation`, field, `url`, `status` | `value = <manual value>`; `source = "manual"`; `generation = nil`; the field carries the value; `url` = `nil` (except the `pull_request` field itself); `status` = fallback where the field is not `status` |
| agent | no provider and no manual value, evidence entry is a map | **every member the evidence map carries** + `value` + the member's field + `status`/`url` when absent | `value = Map.get(agent, field) \|\| fallback`; `source` = the map's own `source`, else `"agent"`; the field is filled only when the map lacks it |
| none | none of the three | `value`, `url`, `status`, `source`, `generation` | `pull_request`: all `nil`, `source = "none"`; `ci`: `value = status = "unknown"`; `review`: `value = status = "none"` |

The agent map is built by `ReadModel` (`control/lib/symmetry_control/workspaces/read_model.ex`):
`source/1` (`:430-437`) contributes `source`, `generation`, `event_id`, `recorded_at`, and
`apply_event/2` adds per kind `url`/`state` (`pull_request`, `:373-385`), `status`/`url`/`summary`
(`ci`, `:387-400`) and the same trio for `review` (`:402-415`) — `summary`/`url` only when the event
payload carries them (`maybe_put/3`). `provider_pull_request_state/2` exists because the provider
delivery's `status` is the change's *state* while the `ci`/`review` `status` members are the
provider's verdict strings.

Coverage at the baseline (the gap the contract's C2 closes): the four owned files assert
`delivery.*.source` only for `provider` (`portal_resources_test.exs:726, :736, :745`, `:1227`;
`portal_workflow_test.exs:1436`), `manual` (`:713`, `:868`) and `none` (`:873-874`). `"agent"` is
asserted nowhere, and no test serves the agent branch at all: the only semantic delivery events
appended anywhere in the four files are non-delivery kinds, so the read-model → resolver plumbing
for `pull_request`/`ci`/`review` evidence has never been served by these files
(`chat_test.exs:741-855` serves it through the chat outcome, asserting `generation` only). The
W1-13 gates assert delivery member *presence* against permissive schemas
(`additionalProperties: true`), so a dropped extra member passes them.

## Increment

1. **CW-8a — one keyed getter.** Replace the eight `value/2` clauses with one table and one head,
   keeping the `is_map/2` guard, the string-then-atom lookup order and the `nil` answer for an
   unknown key or a non-map input:

   ```elixir
   @evidence_keys ~w[summary message question findings changed_artifacts changed_files tests]a
   @evidence_key_strings Map.new(@evidence_keys, &{Atom.to_string(&1), &1})

   defp value(map, key) when is_map(map) do
     case Map.fetch(@evidence_key_strings, key) do
       {:ok, atom_key} -> Map.get(map, key) || Map.get(map, atom_key)
       :error -> nil
     end
   end

   defp value(_map, _key), do: nil
   ```

   The atom list is a compile-time literal of the atoms themselves (no `String.to_atom` and no
   `String.to_existing_atom` — the latter would depend on an *unloaded* module having created
   `:changed_files` first), and the string→atom table is built at compile time, so the getter is one
   map fetch plus the two `Map.get` calls it always did. A new evidence field is one more word in the
   list.

2. **CW-8b — three descriptors and one resolver.** `delivery/2` keeps its literal three-member map
   and asks one resolver per member; the descriptors carry the member's three competing inputs, the
   field they compete for, the fallback, and the provider metadata the provider branch serves
   (including the member's own recorded-at source, which is `external_change_updated_at` for
   `pull_request`/`review` and `external_ci_updated_at` for `ci` — the deleted
   `delivery_context/3` map, now inline):

   ```elixir
   defp delivery(work_item, evidence) do
     %{
       pull_request:
         work_item
         |> delivery_descriptor(:pull_request, evidence)
         |> resolve_delivery()
         |> provider_pull_request_state(work_item),
       ci: work_item |> delivery_descriptor(:ci, evidence) |> resolve_delivery(),
       review: work_item |> delivery_descriptor(:review, evidence) |> resolve_delivery()
     }
   end
   ```

   `resolve_delivery/1` keeps the precedence as clause order (provider → manual → agent → none) and
   reproduces each branch's member set exactly, including the provider branch's
   `provider`/`recorded_at`, the manual branch's absence of both, the agent map's whole pass-through
   (`Map.put`/`Map.put_new` sequence unchanged), the empty-string fall-through and the
   `"unknown"`/`"none"` fallbacks. `normalize_delivery/2` and `provider_pull_request_state/2` stay as
   named steps; the none branch keeps its `:url`/`:status` split (a `:url` member claims neither a
   value nor a status; a `:status` member serves its fallback in both). The `field` atom still
   exists, but only inside the descriptor and the resolver — no caller passes it.

3. **`control/test/symmetry_control_web/controllers/portal_resources_test.exs`** — the C2 delivery
   precedence matrix test (one table, one loop, exact map equality of the served member per branch
   per member, so it is green on the base revision and stays unchanged on head) plus one end-to-end
   case that serves the agent branch over HTTP (agent attempt → the three semantic delivery events →
   workspace read), because no test in the owned files serves that branch today.

4. **`docs/TODO.md`** — one decision note (no checkbox) recording CW-15 as tolerated with its
   coordinate and the condition that would justify the table (`## Tolerated duplication (refactor
   findings, 2026-11-21)`). No tracked item is closed by this increment, so
   `docs/archive/CLOSED-TODO.md` is not touched.

## Exact edge-case semantics the matrix pins

- **The member sets are the contract.** Provider and agent branches serve members the manual and
  none branches do not (`provider`/`recorded_at`; the evidence's `event_id`/`state`/`summary`), so
  every matrix row asserts `==` on the **whole map**, not member presence: an added member, a dropped
  member or a changed value fails the row.
- **Empty string is "not set", `nil` is "not set".** `""` for a provider value and for a manual value
  falls through to the next source (rows "provider empty" and "manual empty"); a `nil` does the same
  thing through the guard's `is_binary/1` half. Both spellings are covered per member.
- **An agent map decides its own source and its own field.** A map without the member's field serves
  the fallback in that field (`value` and the field both `nil` for `pull_request`, `"unknown"`/
  `"none"` for `ci`/`review`, together with a `status` member); a map carrying its own `source`
  keeps it (`"agent"` is only the default) — the pass-through is `Map.put_new`, not an overwrite.
- **The provider override is `pull_request`-only.** `provider_pull_request_state/2` patches `status`
  to `external_pull_request_state || "unknown"` when (and only when) `source == "provider"`; `ci`
  and `review` provider rows serve the provider's own verdict string as `status`, and manual/agent/
  none rows are never patched.
- **Recorded-at comes from the member's own provider timestamp.** The matrix gives the change
  timestamp and the CI timestamp different values, so a resolver that feeds the wrong context to a
  member fails two rows.
- **`generation` is `nil` except on the agent branch**, where it is the evidence's own generation
  (the read model's `source/1`); `normalize_delivery/2`'s `Map.put_new` means an agent map that
  carries `generation: nil` stays `nil` rather than being filled.

## Evidence plan

All gates foreground, in this order (never the full suite beside another suite or a harness on this
host — shared PostgreSQL 100-connection cap; the portable-toolchain runner is
`<HOST-PATH>/rf-w2-04-mix.sh`, Elixir 1.20.4 / OTP 29, `MIX_TEST_PARTITION=rf-w2-04`,
`PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset, `+S 2` because the shared server refuses
connections once several worktree suites hold pools):

1. **Base, with the new tests only** (source untouched): `mix test` over the four owned files. The
   matrix and the agent case must be green *before* `portal_json.ex` changes — that is what makes
   them characterization tests rather than head-written assertions. The one table + one loop shape
   is what keeps them unchanged on head (`git diff` of the test file between the two runs shows no
   edit inside the new test).
2. **CW-8a/8b implementation** in `portal_json.ex` (nothing else).
3. `mix format --check-formatted`; `mix compile --warnings-as-errors`; the four-file `mix test`.
4. **Base-vs-head served-document differential (C3).** A throwaway hook — one temporary line in
   `control/test/support/test_conn.ex`'s `api_conn/0` (`Plug.Conn.register_before_send/2`) plus a
   throwaway recorder module compiled from a scratch file outside the repository — records every
   response issued by the four files as one normalized `Jason.encode!` line (UUIDs, ISO-8601
   timestamps, numbers and `next_cursor`-like opaque tokens masked; the mask list is the artifact's
   own claim) plus one line of the response's sorted key paths. Every request in the four files goes
   through `api_conn/0`/`operator_conn/1` (39/85/64/27 and the operator reads in the attention file),
   so the hook sees the whole corpus. The differential is run as **base, base again (stability
   control), head**: the base-vs-base diff proves the normalization removes the per-run volatility
   (ids, timestamps), and the base-vs-head diff is the wire-identity artifact. The hook is reverted
   before the final gates and never committed.
5. **C5 member-by-member table.** A throwaway script (outside the repository) that calls
   `PortalJSON.work_item/2` over the same precedence matrix and prints each member's served map; run
   at base and at head, diffed. It is the projection-level counterpart of the C3 differential and the
   table the answer attaches.
6. `pnpm contracts:check` (root; schema/fixture/DTO side — expected green, no `contracts/` change; it
   is not evidence for CW-8b, because it never executes `portal_json.ex`).
7. `git diff --check`; `git diff origin/main...HEAD -- . | grep -i "users/"` empty. Full `mix test`:
   conductor gate-runner (separate step).
8. **Mutation check on the new pin** (throwaway, reverted): flip the agent branch's pass-through to a
   fixed member set (drop `:state`) and confirm the matrix test fails red before reverting — the C2
   test must be able to fail, not merely exist. Same for the getter: drop `changed_files` from
   `@evidence_keys` and confirm the outcome path that reads it goes red (or record that no test
   reaches that key, whichever the run shows).

## Design trade-offs and decisions

1. **A compile-time atom table, not `String.to_existing_atom/1`.** The finding's sketch converted the
   served key with `String.to_existing_atom/1`. That atom must already exist in the running system:
   `:changed_files` lives only in `PortalJSON.value/2`'s own clause today, so after the rewrite the
   conversion would depend on some other module having been loaded first — a latent
   `ArgumentError` on the agent-result path in any deployment where it has not. The
   `~w[...]a` list plus `Map.new/2` keeps the atoms in *this* module's literal (so loading
   `PortalJSON` creates them) and is one map fetch per lookup.
2. **`is_map/2` guard kept, unknown key still `nil`.** `Map.fetch/2` on the table reproduces the old
   catch-all exactly, including the answer for a key no branch serves (`nil`, never a `nil`-keyed
   lookup on the evidence map, which the simpler `Map.get(map, Map.get(@table, key))` form would
   silently perform).
3. **Descriptors, not a `resolve(provider, manual, agent, fallback)` tuple.** The contract's C1
   requires every branch member to survive, and the branches serve three distinct member sets —
   manual and none share `value source generation url status`, only the provider branch adds
   `provider`/`recorded_at`, and only the agent branch carries the evidence map's own members; a
   tuple-returning resolver would have to re-derive `provider`/`recorded_at`/field/status at the
   call site, which is where the original's 6 arguments came from. The descriptor makes the
   per-member inputs data and leaves the member sets in one place.
4. **`provider_pull_request_state/2` stays a named, separately-tested step.** It is the one
   `pull_request`-only patch, and folding it into the resolver's provider clause would need a
   member-specific key in the descriptor and a second `status` write; keeping it as a wrapped step
   preserves the exact write order (`normalize_delivery/2` first, override second) that the matrix
   pins.
5. **No `field`-less descriptor.** The `field` atom survives inside the resolver because the `:url`
   member's none branch and the `:status` members' none branches serve different values
   (`nil` vs the fallback). Expressing that as "the `:url` member has fallback `nil`" would be
   less explicit and would break the moment a `:url` member gains a fallback.
6. **The matrix is projection-level; the agent case is wire-level.** The branches the review names as
   unpinned ("an agent map with and without the field", "a map carrying its own source") are not
   reachable through events (`ReadModel` only writes a `ci`/`review` entry with a valid `status` and
   a `pull_request` entry with a valid URL, so the "without the field" case cannot be produced from
   the database), so the exhaustive half has to call `PortalJSON.work_item/2` directly — the same
   function all three reads serve from. The one wire-level agent case covers what the projection-level
   table cannot: the read model's own map shape (including its `DateTime` `recorded_at`) travelling
   through Jason to the response body.
7. **CW-15 is out of scope (C4).** `control/lib/symmetry_control_web/portal_session.ex:5-79` is the
   `safeReturnTo` open-redirect allowlist, outside the owned set and unrelated to the delivery
   projection; the contract's decision records it as tolerated, and this increment records that note
   in `docs/TODO.md` instead of editing the module. The historical title keeps the id.
8. **No `control/test/support/**` change.** Like the W1-13 gates, the new helpers stay private to the
   file that uses them: the support directory is outside the owned set, and defining a module in one
   test file would break a single-file run of the others. The throwaway hook is the one temporary
   exception and is reverted before the final gates.

## Out of scope

- `attention.unavailable` removal (D2/W6-09) and any other wire-member removal: the schema bodies,
  fixtures, generated TS/Go output and frontend decoders are untouched.
- Controller consolidation (W2-03) and the shared delivery/authority module W2-15 owns.
- Test-only seam removal (W5-06) and the `value/2` callers outside `outcome/5`.
- The `chat-read` outcome path's own delivery coverage (`chat_test.exs` is outside the owned set;
  its `generation` assertion is intentionally left as it is).
- CW-15's allowlist table and every `portal_session.ex` change.
- Moving the duplicated W1-13 gate helpers into `SymmetryControlWeb.ServedDocument` (the open PR #145
  TODO entry; `control/test/support/**` is not owned here).

## Evidence as run (2026-11-21, base `857e763b`)

All gates ran foreground through `<HOST-PATH>/rf-w2-04-mix.sh` (Elixir 1.20.4 / OTP 29, `+S 2`,
`MIX_TEST_PARTITION=rf-w2-04`, `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset). Two
deltas from the plan above, recorded here rather than left stale: the C5 table was derived from the
differential's own delivery dump instead of a second script (same matrix, one artifact), and the
temporary hook sits in `test_conn.ex`'s `api_conn/0` **and** `operator_conn/1` (the attention file
also reads the operator API). Its final mask list, the canonical form and the control runs:

| run | result | body diff | member-path diff | delivery diff | responses |
| --- | --- | --- | --- | --- | --- |
| new tests only, unmodified `portal_json.ex` | `14 passed`, exit 0 | — | — | — | — |
| base `h1` vs base `h2` (stability control) | `59 passed` ×2, exit 0 | 0 | 0 | 0 | 380 (`/portal/api/**`), 375 delivery lines |
| base `h1` vs head `i1` | `59 passed`, exit 0 | 0 | 0 | 0 | 380, 375 |

The canonical form is part of the artifact's claim: object members name-sorted, arrays sorted by
their **masked** encoding (an unmasked key lets a per-run row id decide the order — the first
control iteration showed exactly that, and the fix is one `mask/1` call in the sort key), and values
masked for UUIDs, ISO-8601 timestamps, numbers (as `0`, so the text stays decodable JSON),
`next_cursor`/`next_before`/`cursor=`/`before=` cursor values, `sha256:` digests, the digit runs the
`System.unique_integer/1` fixtures embed in `key`/`name`/`title`/`project_key`/`project_name`/
`work_item_key`, and the pagination fixture's `raw event <n>` label (110 events written under one
`now:`, so page membership follows a tie-break that differs between two runs of the *unmodified*
code). `.delivery` lines are the delivery member maps themselves, so the CW-8b subject is compared
member by member and value by value, not only as part of a body.

The C5 table per member × branch, from the 375 delivery lines of both runs (base and head identical):

| member | branch | count | served member set |
| --- | --- | --- | --- |
| `pull_request` | provider | 13 | `value source generation url status provider recorded_at` |
| `pull_request` | manual | 14 | `value source generation url status` |
| `pull_request` | agent | 2 | `value source generation url status event_id recorded_at state` |
| `pull_request` | none | 96 | `value source generation url status` |
| `ci` / `review` | provider | 13 each | `value source generation url status provider recorded_at` |
| `ci` / `review` | manual | 4 each | `value source generation url status` |
| `ci` | agent | 2 | `value source generation url status event_id recorded_at summary` |
| `review` | agent | 2 | `value source generation url status event_id recorded_at summary` |
| `ci` / `review` | none | 106 each | `value source generation url status` |

Representative served values (masked only for UUID/timestamp/number): provider `pull_request`
`%{source: "provider", provider: "github", url: "https://github.com/acme/symmetry/pull/42", value:
..., status: "open", generation: nil, recorded_at: "<ts>"}`; agent `pull_request` `%{source:
"agent", event_id: "<uuid>", state: "open", url: "https://agent.example/acme/symmetry/pull/77",
value: ..., status: nil, generation: 1, recorded_at: "<ts>"}`; none `ci` `%{source: "none", value:
"unknown", status: "unknown", url: nil, generation: nil}`.

Mutations (each reverted; `git diff` of `portal_json.ex` empty afterwards): the agent branch's
pass-through dropped `state` → the matrix and the wire agent case both red (`Result: 0/2 passed`,
exit 2); the provider guard's `and provider_value != ""` removed → red on “provider empty falls
through to manual”, exit 2; the provider clause made unreachable → red on “provider wins over
manual and agent”, exit 2. The getter's key table reduced to one key stayed **green** across the
four files: no served `result` payload in these suites carries those keys, so CW-8a's equivalence
rests on a dedicated old-vs-new oracle (the eight old clauses verbatim against the new clause over
994 inputs — 7 keys × 9 map shapes × string/atom/nil/unknown queries plus non-map inputs — **0
mismatches**) and on the differential, which would have caught any *served* difference.

The differential's own sensitivity was measured the same way: with the `state` mutation applied, the
same hook over the same four files reports body 2, member paths 2 and delivery 4 differing lines
against the base dump (`57/59 passed`, the two new tests red), while the unmutated base-vs-head
comparison is empty. The empty diff is therefore a sensitive measurement, not a blind one. The final
`portal_json.ex` is byte-identical to the file the head dump measured
(`sha256 c6c97f7c…9529af`), and the base revision's file is `sha256 4afea623…594b9696` at `857e763b`.

`mix format --check-formatted`, `mix compile --warnings-as-errors` (1 file) and the four-file
`mix test` (`59 passed`) are exit 0 on the reverted tree; `pnpm contracts:check` exit 0 (typecheck,
35/35 fixture tests, 21 schemas / 248 fixtures, DTOs current); `git diff --check` clean; the full
`mix test` and the CI jobs belong to the conductor's gate-runner per the contract.

## Apply-stage re-record (2026-11-21, rf-w2-04-portal-json apply, findings 1-3)

The apply stage landed three deltas on the `i1` revision above: the `delivery/2` comment
(finding 1 — prose only, no code path, no line-count change), the pin `the served outcome reads the
attempt result's string-keyed members` in `portal_resources_test.exs` (finding 2 — the keyed
getter's string spelling and key table had no committed test, which is why the one-key mutation row
above stayed green; the pin now kills it) and the trade-off-3 sentence above (finding 3). The C3
differential was re-run on the delivered revision with the same throwaway hook and canonical form
(recovered from the implement run's event log; `<HOST-PATH>/gdd/scratch/w204-apply-*`):

| run | result | body diff | member-path diff | delivery diff | responses |
| --- | --- | --- | --- | --- | --- |
| delivered revision, run 1 vs run 2 (stability control) | `60 passed` ×2, exit 0 | 0 | 0 | 0 | 384 (`/portal/api/**`), 387 delivery lines |
| base `4afea623…594b9696` vs delivered revision, same corpus | `60 passed` + `60 passed`, exit 0 | 0 | 0 | 0 | 384, 387 |

The corpus grew by the new pin's own reads (380 → 384 responses, 375 → 387 delivery lines). The
delivery member sets per member × branch are unchanged (the base and delivered delivery tables are
identical apart from their source filename); only the `none` counts move — `pull_request` 96 → 100,
`ci` 106 → 110, `review` 106 → 110 — while provider (13), manual (14 / 4 / 4) and agent (2) counts
stand. Delivered byte-identity: `portal_json.ex` is now `sha256 abaee844…7806e24` (superseding the
`c6c97f7c…9529af` recorded for `i1` above; the delta is the finding-1 comment sentence) and
`portal_resources_test.exs` is `sha256 ec5ba5a5…04df984f`. The delivered four-file `mix test` is
`60 passed` exit 0, and the same suite on the base source with the same test file is `60 passed`.
Pin mutation: `@evidence_keys` reduced to one key → the pin alone red (`0/1 passed`, exit 2,
`findings` served `[]`), reverted and re-verified at the same sha. `mix format --check-formatted`,
`mix compile --warnings-as-errors`, `pnpm contracts:check` (typecheck, 35/35 fixture tests,
21 schemas / 248 fixtures, DTOs current) and `git diff --check` are exit 0; the full `mix test` and
the CI jobs remain with the conductor's gate-runner.
