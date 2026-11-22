# rf-w4-06 provider_access intent-context unification plan (CCS8)

Contract: the GDD goal contract `goal-rf-w4-06-provider-access-intent.md` (2026-11-21, GDD state
root) including its binding conductor decisions (D1 byte-identical surfaces; D2 dLOC report-only;
foreground-oracle rule E8; control-flow-audit rule from lesson 32). Baseline for every coordinate
in this plan: `origin/main` `c9ed23b4` (= this worktree's HEAD). Area:
`control/lib/symmetry_control/integrations/provider_access.ex` (2,846 L) plus its four named Elixir
oracles; the foreground oracle is the web controller suite.

The increment collapses the five intent-lock variants and the three claim dispatches into one
`IntentContext` builder (a nested module in the same file) plus two scope bodies, keeps the
goal-vs-legacy selection rules and every refusal atom where they are, and removes the second
copy of the goal-dispatch validation that `recover_dispatch_context/1` carries.

The clause-2 judging artifact (variant -> builder mapping, lock order per path, refusal-atom diff,
per-path control-flow audit) is the appendix at the end of this file; it landed there at the fix
stage.

## Derivation: the five variants as they exist at the base revision

All spans verified against the base blob (line numbers are base-revision numbers; the contract's
re-derivation against `0fa9b705` reproduces them exactly, and #161 does not touch this file).

| Variant | Base span | Scope | Id source | Lock sequence (mode) | Refusal on missing row |
| --- | --- | --- | --- | --- | --- |
| `lock_intent_context/1` | :1123-1128 | dispatcher | `intent_goal_id/1` | selects goal vs legacy | — |
| `lock_legacy_intent_context/1` | :1130-1148 | legacy | intent struct | Project -> Resource -> Connection -> WorkItem -> Task -> Run -> Runtime (all `FOR UPDATE`) | Project/WorkItem/Task/Run/Runtime `:ownership_lost`, Resource/Connection `:forbidden` |
| `lock_new_intent_context/2` | :1150-1160 | dispatcher | `locate_intent_context/1` | selects goal vs legacy | `:ownership_lost` when locate fails |
| `lock_legacy_new_intent_context/3` | :1162-1186 | legacy | claims + resource id | Project -> Resource -> Connection (from the locked resource row) -> WorkItem -> Task -> Run -> Runtime (all `FOR UPDATE`) | Project/WorkItem/Task/Run/Runtime `:ownership_lost`, Resource/Connection `:forbidden` |
| `lock_goal_intent_context/2` | :1190-1216 | goal | intent struct | Project `FOR SHARE` -> Goal -> Resource -> Connection -> WorkItem -> Task -> Run -> Runtime (all `FOR UPDATE` except Project) -> GoalRevision (unlocked read) | Project/Goal/WorkItem/Task/Run/Runtime/Revision `:ownership_lost`, Resource/Connection `:forbidden` |
| `lock_goal_new_intent_context/3` | :1218-1250 | goal | claims + resource id | same sequence, same modes | same atoms |

Supporting definitions in the collapse census: `validate_goal_intent_context_links!/3` :1252-1264,
`validate_goal_new_intent_context_links!/4` :1266-1276, `goal_context_links?/2` :1278-1289,
`lock_goal_work_item/1` :1323-1326 (goal bodies only), `lock_project_for_goal/1` :2815-2817 and
`lock_record/2` :2819-2821 (shared primitives). `locate_intent_context/1` :1291-1321 and
`goal_project_id/1` :645-647 stay outside the census.

The only real differences between the two legacy variants are the **connection id source** (intent
field vs the locked resource row) and the ids' provider (intent struct vs claims). The goal variants
add the Goal row + revision, the `FOR SHARE` project mode, and a link re-verification of the locked
rows. All three differences are data, not control flow, so they become builder inputs.

| Claim dispatch | Base span | Distinct behaviour |
| --- | --- | --- |
| `claim_dispatch/2` | :2084-2092 | pre-read + `goal_managed_intent?/1` selection |
| `claim_goal_dispatch/2` | :2094-2126 | context lock -> intent row -> hash -> 4 goal validations -> state machine; returns `authorized_context/2` |
| `claim_legacy_dispatch/2` | :2128-2154 | intent row -> hash -> state machine; returns the bare `executing` intent |

The state machine itself (dispatch token, `dispatch_changeset/2` CAS, `{:replay, result}`,
`{:failed, stored_failure(...)}`, `:state_conflict`) is byte-identical in both bodies and is the
shared claim core. `recover_dispatch_context/1` :1940-2023 repeats the same four goal validations
inside its `:authorized` branch.

## Public / API shape

No public function of `SymmetryControl.Integrations.ProviderAccess` changes name, arity or
behaviour. The builder is one nested module in the same file:

```elixir
defmodule SymmetryControl.Integrations.ProviderAccess do
  defmodule IntentContext do
    @moduledoc false
    # docs/design/data.md:347 order encoded once.
    def legacy_lock(ids)                 # 7 locks, all FOR UPDATE
    def goal_lock(ids, expected)         # FOR SHARE Project -> Goal -> ... -> revision, then links
    def lock_record(schema, id)          # the two primitives the rest of the file also uses
    def lock_project_for_goal(project_id)
    def lock_goal_revision(goal)
  end
end
```

Builder inputs (`ids`) are one map shape for all four variants:
`%{goal_id:, project_id:, resource_id:, connection_id:, work_item_id:, task_id:, run_id:, runtime_id:}`
with two documented sentinels, each replacing exactly one base variant difference:

- `connection_id: :from_resource` — the claim source derives the connection from the locked resource
  row and refuses `:forbidden` when it has none (base `lock_legacy_new_intent_context/3` :1166-1170,
  `lock_goal_new_intent_context/3` :1224-1228).
- `project_id: nil` (goal scope only) — refused `:ownership_lost` before the Project lock, exactly
  as base `lock_goal_new_intent_context/3` :1219. The intent source passes
  `goal_project_id(goal_id)` (the Goal row's current project), as base `lock_goal_intent_context/2`
  :1191 did.

`goal_lock/2`'s second argument is the **expected rows** map: every key present in it is re-verified
against its locked context row, in the base order (project, work_item, task, run, runtime, resource,
connection). The intent source passes all seven (base `validate_goal_intent_context_links!/3`), the
claim source passes the five it did not derive itself (base
`validate_goal_new_intent_context_links!/4`); the resource/connection comparisons the claim source
omits were tautological at base (`lock_record(Schema, id).id == id`), which is why that variant never
wrote them.

The two dispatchers keep their base selection rule verbatim and shrink to the id extraction plus one
builder call:

```elixir
defp lock_intent_context(intent) do
  ids = intent_ids(intent)

  case intent_goal_id(intent) do
    goal_id when is_binary(goal_id) ->
      goal_ids = %{ids | goal_id: goal_id, project_id: goal_project_id(goal_id)}
      IntentContext.goal_lock(goal_ids, ids)

    _legacy ->
      IntentContext.legacy_lock(ids)
  end
end

defp lock_new_intent_context(claims, resource_id) do
  ids = locate_intent_context(claims) || Repo.rollback(:ownership_lost)
  expected = claim_ids(ids, claims, resource_id)

  case ids do
    %{goal_id: goal_id} when is_binary(goal_id) ->
      IntentContext.goal_lock(
        %{expected | goal_id: goal_id},
        Map.drop(expected, [:resource_id, :connection_id])
      )

    _legacy ->
      IntentContext.legacy_lock(expected)
  end
end
```

The three claim dispatches become one `claim_dispatch/2`: the pre-read and
`goal_managed_intent?/1` selection stay, the transaction body keeps the base order (context lock,
intent row, request-hash compare, goal validations), and only the `:execute` payload differs by
scope (`authorized_context/2` for goal, the bare `executing` intent for legacy — the two clauses
`claim_and_execute/2` already distinguishes). `recover_dispatch_context/1` calls the same
`validate_goal_dispatch!/2` helper its `:authorized` branch used to spell out inline.

## Edge-case semantics the contract demands

- **Lock order and modes unchanged.** Legacy: 7 rows `FOR UPDATE` in Project -> Resource ->
  Connection -> WorkItem -> Task -> Run -> Runtime order (both sources). Goal: Project `FOR SHARE`
  (the documented serialization against archival) -> Goal `FOR UPDATE` -> Resource -> Connection ->
  WorkItem -> Task -> Run -> Runtime -> revision read, exactly as base. `lock_project_for_goal/1`
  keeps its literal `lock: "FOR SHARE"` query, which the controller suite's telemetry assertion
  (:418-455) matches on.
- **Refusal atoms byte-identical, at the same step.** Each `|| Repo.rollback(atom)` keeps its
  position in its sequence: resource/connection refuse `:forbidden`, every other missing row refuses
  `:ownership_lost`. `lock_goal_work_item/1`'s non-binary guard (base :1323-1326) is kept in the
  shared work-item lock. For a nil id the legacy variants instead raised `ArgumentError` from
  Ecto's nil-comparison guard (`Ecto.Query.Builder.not_nil!/2`, Ecto 3.14.3), where HEAD refuses
  `:ownership_lost`. The case is unreachable: the column is `null: false` and `intent.work_item_id`
  passes `validate_required(@accept_fields)` (`schemas.ex:166-189`), so every reachable row keeps
  the same refusal.
- **Selection rules unchanged.** `intent_goal_id/1` (persisted intent) and
  `locate_intent_context/1` (fresh claim) decide scope in the dispatchers, not in the builder.
- **Claim core semantics unchanged.** `request_hash` compare position, `dispatch_changeset/2` CAS,
  `Ecto.UUID.generate()` token, `{:replay, ...}`/`{:failed, ...}`/`:state_conflict` results and the
  absence of a default state clause are preserved verbatim in the one core.
- **Transaction boundaries unchanged.** Every builder call happens inside the same
  `Repo.transaction/1` body it was called from; `Repo.rollback/1` keeps unwinding to the same
  boundary. No new transaction, no new round trip outside a transaction.
- **Byte-identical kept regions.** GenServer plumbing :1-300, dispatch execution :1568-1920, the
  accept-intent family :875-1043, the claim-scope family :303-512, `finalize_*`/`mark_*`
  :2156-2380 and the tail helpers :2380-2846 keep their text except the three delegating private
  forwards for the moved primitives (`lock_record/2`, `lock_project_for_goal/1`,
  `lock_goal_revision/1`) and the two call-site-free edits described above.

## Evidence plan (exact commands, run on this worktree, all foreground)

Runner: `bash <GDD-STATE>/scratch/rf-w4-06-mix.sh <mix args>` (portable Elixir 1.20.4 / OTP 29.0.6
per the `rf-w1-14-mix.sh` / `rf-w3-11-mix.sh` recipe, `MIX_TEST_PARTITION=rf-w4-06`, deps read from
the main checkout, local PostgreSQL on 5432).

| Gate / artifact | Command | Expected |
| --- | --- | --- |
| derivation (before/after) | declaration census of the file via `grep -n "^  defp"` + `git diff --stat` | 5 variants + 2 validators + `lock_goal_work_item/1` gone; builder present |
| targeted suites (before/after) | `mix test --seed 0 --formatter SymmetryControl.ExUnitJsonFormatter <4 files>` | 13 tests, same names/states both revisions (4 broker tests skipped unless env-gated) |
| controller suite (before/after) | `mix test --seed 0 --formatter ... test/symmetry_control_web/controllers/provider_action_controller_test.exs` | 79 tests, same names/states both revisions |
| broker e2e (CI way) | `SYMMETRY_PROVIDER_BROKER_E2E=1 mix test test/symmetry_control/provider_broker_e2e_test.exs` | 4 tests; `go build` of the daemon/agent from `daemon/` must succeed, else recorded CI-only |
| lock-mode assertions | the controller suite's `:400` case (`:418-455`) plus its `:2092` advisory-lock case | green; `FOR SHARE` on projects, `FOR UPDATE NOWAIT` on goals |
| format | `mix format --check-formatted` | exit 0, empty |
| compile | `MIX_ENV=test mix compile --warnings-as-errors` | exit 0 |
| whitespace | `git diff --check` | exit 0 |
| credo | `mix credo --strict` | file's issue count not above the recorded base count |
| citations | `python scripts/ci/check-citation-existence.py --head HEAD` | exit 0 (no citation into the owned regions) |
| mutation checks | flip the shared `FOR SHARE` to `FOR UPDATE` (red: 'Goal provider claim scope locks Project before Goal'); flip the claim conflict atom (red: 'action IDs conflict on changed input and replay a stored success') | observed red for each, then reverted; no assertion changes, so both are the recorded mutants for the *preserved* assertions |

The full `mix test` run is the conductor's gate-runner (one suite at a time, shared 100-connection
Postgres cap); no measurement harness runs concurrently with it.

## Design trade-offs

1. **The builder owns the lock primitives; the outer module keeps three one-line forwards.**
   A nested module cannot call its parent's `defp`s, and the primitives are used ~15 times outside
   the owned spans (`lock_record/2` in the accept, dispatch and finalize families; both in
   `lock_goal_claim_scope/1`). Alternatives: (a) rewrite every outer call site to
   `IntentContext.lock_record(...)` — touches byte-identical regions for no behavioural gain;
   (b) duplicate the two query texts in the builder — two conventions for one primitive. The
   chosen shape keeps one implementation of each primitive, keeps the lock *policy* single-sourced
   in the builder, and leaves every non-owned call site byte-identical. It also keeps
   `lock_project_for_goal/1`'s `FOR SHARE` query text in one place, which the controller suite's
   telemetry assertion matches literally.
2. **Expected-rows map instead of two validators.** The two link validators differ only in which
   rows they re-verify. A key->context-field table plus "keys present are verified" keeps the base
   order and the base atom, drops the two tautological claim-source comparisons (which never wrote
   a check), and keeps all seven intent-source comparisons.
3. **`map.put` sentinels vs four more bodies.** `:from_resource` and a nil goal project encode the
   two genuine source differences in data, so the builder has two bodies instead of four; both
   sentinel semantics are base behaviour, cited above.
4. **One `claim_dispatch/2` with a `goal_managed?` flag.** The alternative — a shared core called by
   two thin scope wrappers — keeps three functions for one state machine and was rejected: the
   contract asks for one core, and the two scope-dependent expressions are already distinguished by
   the existing `%ProviderActionIntent{}` clause split in `claim_and_execute/2`.
5. **`recover_dispatch_context/1` keeps its state machine.** Its 84 lines are a distinct recovery
   policy (pause/revoke/fail-unstarted, interrupted, readback), not a context reassembly; only the
   duplicated four-line validation block and the context construction (already the dispatcher call)
   are shared. Rewriting more would be an unrequested behaviour change.

## Out of scope (with reasons)

- `lock_claim_scope/1` and `lock_goal_claim_scope/1` (:303-460): the claim-scope family is a
  different owner in the findings census and returns a different shape; it is the *foreground
  oracle's* surface, so touching it would invalidate the characterization the contract relies on.
  It does share the goal order, and that residual third encoding is reported, not fixed here.
- `integrations.ex` (W2-07), `goals.ex` (W3-10/W4-07), provider modules (merged W2-06),
  `Integrations.HTTP` rescue narrowing (W3-11): other owners, other files.
- GenServer job/dispatch-timeout plumbing and the accept-intent family: byte-identical by contract.
- No new test file and no assertion change: the four named Elixir files stay oracles; the mutation
  checks above are recorded for the preserved assertions.

## Ambiguities resolved (options and the chosen reading)

1. **"Five variants collapse to builder + two scope bodies" vs the dispatchers.** The five are the
   variants F8 names (:1130, :1150, :1162, :1190, :1218 minus the selection dispatcher); the
   dispatchers `lock_intent_context/1` and `lock_new_intent_context/2` keep the selection rule and
   stay as the only call-site-visible entry points, so no caller text changes. Recommendation:
   adopted.
2. **`recover_dispatch_context/1`'s "84-line reassembly becomes a builder call + the same
   validation".** Read as: the context construction goes through the builder (it already does via
   the dispatcher, which now *is* the builder call) and the duplicated validation becomes the shared
   helper; the recovery state machine stays. Recommendation: adopted; the fuller reading (rewriting
   the recovery policy) would change behaviour the contract forbids changing.
3. **Where the builder lives.** A `defmodule` nested inside `ProviderAccess` (precedent:
   `SymmetryControlWeb.StrictJSONDecoder.DuplicateJSONKeyError`,
   `lib/symmetry_control_web/plugs/strict_evidence_json.ex:11`) keeps the owned set exactly one
   source file, as the contract requires. Recommendation: adopted.

## Apply-stage results (2026-11-21, implement stage, uncommitted tree)

- **Gates on the head bytes** (sha256
  `031c579c0b314ad4af5b432d41bbffba57e40d343cb35daff5d1a6111b6fec7f`): `format --check-formatted` 0,
  `MIX_ENV=test compile --warnings-as-errors` 0, targeted 13 tests (9 passed + 4 env-gated skips),
  controller 79 passed, test-name/state sets identical to base (`diff` empty), credo `--strict`
  file count 42 → 40 (project 517 → 515), `git diff --check` 0, citation gate 0. The base side was
  measured in a pristine detached `c9ed23b4` worktree with its own test partition, not by reverting
  this tree.
- **Mutants measured** (all reverted; the delivered hash was re-verified after each revert):
  `FOR SHARE` → `FOR UPDATE` reddens exactly "Goal provider claim scope locks Project before Goal";
  `existing_intent_decision/4`'s `:idempotency_conflict` → `:state_conflict` reddens
  "action IDs conflict on changed input and replay a stored success" plus one goal-readback sibling;
  the collapsed claim core's `request_hash` atom flip stays green — that branch is unreachable from
  the public flow (`accept_intent/7` passes the intent row's own hash), recorded in the stage answer
  as a finding, not a defect of this collapse; the `claim_ids/3` `task_id` mapping flip reddens the
  named conflict test, which is the collapse-specific mutant for the new id extraction.
- **Lock-path coverage probe** (temporary, `RF_W4_06_PROBE`, reverted; hash re-verified): all four
  paths are reached by the oracles — goal/claims 10, goal/intent 44, legacy/claims 45,
  legacy/intent 41 — so no lock path needs reporting as unreached.
- **dLOC**: −18 net on the file (+201/−219). The two trade-off constraints above (the claim-scope
  family and the recovery policy stay out) bound the collapse well above the program plan's
  −150…−250 estimate; per D2 that number is report-only.
- The env-gated broker e2e and its environmental analysis are recorded in the stage answer.

### Apply-stage hygiene + gate refresh (2026-11-21, apply stage; conductor-accepted simplify F1–F3)

- Tree change on top of the bytes above: `@claim_expected` deleted (its five keys are exactly the
  first five `IntentContext.@link_fields` entries, so the subset it expressed was unchecked; the
  fresh-claim goal branch now passes `Map.drop(expected, [:resource_id, :connection_id])`, which
  skips the two rows the lock derived itself), the `validate_goal_dispatch!/2` comment now names its
  two call sites, and the API sketch above was aligned with the shipped names. No test, assertion,
  evidence row or documented boundary changed.
- Equivalence evidence for the deleted attribute (no assertion changed, so per the gates clause no
  mutation run was needed): the comparison sequence `validate_links!/3` derives from the two maps is
  identical for the `claim_ids/3` shape, that same map after `%{expected | goal_id: goal_id}`, the
  `intent_ids/1` shape, an all-nil-links map and a sparse map — project/work_item/task/run/runtime
  compared, resource/connection skipped, nil values still compared; the dropped map's extra
  `goal_id` key is never read (`@link_fields` drives the loop). No lock, branch or comparison value
  changed, so the lock-path coverage probe counts above still stand.
- Superseding numbers for the hygiene bytes: sha256
  `342634d6ecfa9a8e9ba5fd5b01c6ea1022c1e8e4b5dbbeccd4d6e5bac4650990`; the file is 2,825 L; the
  file's slice diff vs `3ccc62f5` is +198/−219 (−21 net). The `031c579c…` hash, 2,828 L and −18 net
  above describe the pre-hygiene bytes.
- Gates re-run on the hygiene bytes: `format --check-formatted` 0 (0 bytes), `MIX_ENV=test compile
  --warnings-as-errors` 0, targeted 13 (9 passed + 4 env-gated skips) with the test-name/state set
  identical to the pre-hygiene run (`diff` empty), controller 79 passed (same `diff` empty), credo
  `--strict` file count 40 (pre-hygiene 40, base 42 — no rise; the project totals moved with the
  merged #162 `read_model.ex` rewrite and are not comparable across the merge), `git diff --check`
  0, citation gate 0 ("nothing to check").
- Broker e2e (CI way, prebuilt daemon/agent): the first run gave 3 passed + 1 failed on "Azure
  Boards work item reaches Azure Repos, review, and Pipelines projection" (3.1 s versus 7.8–22.1 s
  for its siblings, in the same loaded window where the first credo run lost three files to parse
  timeouts); re-run alone on the same bytes: 4 passed (34.6 s). Classified a load flake, not a
  regression.
- Not applied (recorded, not silently dropped): the proposed controller case pinning
  `IntentContext` lock order. It needs
  `control/test/symmetry_control_web/controllers/provider_action_controller_test.exs`, outside the
  owned set (a new focused case lands in one of the four named oracle files; E8's disposition for a
  lock path the oracles do not reach is "reported, not fixed here"). The gap stays a reported
  finding.

## Review-round-1 fixes (2026-11-21)

- **P3-1 (record accuracy).** The three appendix rows and the edge-case bullet that claimed the
  legacy sites issued `WHERE id = NULL` and refused are corrected: a nil work-item id raised
  `ArgumentError` from Ecto's nil-comparison guard on those sites (the goal guard refused), HEAD
  refuses `:ownership_lost` in both, and the input is unreachable (`null: false`,
  `validate_required`).
- **P3-2 (stale comments).** The three `IntentContext` comments describing pre-refactor history now
  state the current invariants: the link-check order, the primitives vs the file's other lock
  queries, and the goal-vs-legacy selection rule. Comment-only, line counts unchanged.
- **P3-3 (coordinate shift).** The builder block moved from the file top (`:26-155`) to just above
  the dispatchers (`:1118-1246`), so the GenServer plumbing and the live `docs/TODO.md:115-117`
  citations (`:95`, `:232`, `:238`, `:113`, `:116`, `:135`) keep their base coordinates. The first
  reference (`lock_goal_revision/1`'s one-line forward, `:638`) reads
  `__MODULE__.IntentContext.lock_goal_revision(goal)`: with the module below its first reference,
  the bare nested name warns under `--warnings-as-errors`.
- **P3-5 (PR body).** The body now carries the current hash, the W4-06 wave pointer and the
  appendix pointer.
- Numbers for this tree after the round: sha256
  `9e1f5c1f2a5690e28cd03b1aca58fa3c5dce12dbd0898c1204cfbed64dafa083`; 2,825 L; the slice diff vs
  `3ccc62f5` is +199/−220 (−21 net).
- Gates re-run on these bytes: `format --check-formatted` 0, `MIX_ENV=test compile
  --warnings-as-errors` 0, targeted 13 (9 passed + 4 env-gated skips), controller 79 passed, broker
  e2e the CI way 4 passed, credo `--strict` file count 40 with the issue set identical modulo
  coordinates.
- **P3-4 (live tracker gap), added under the conductor's 2026-11-21 authorization.** A new
  "Deferred review findings (PR #189, provider access lock order)" section in `docs/TODO.md` tracks
  both open follow-ups: pinning `IntentContext`'s lock order with an oracle (review STD-A02,
  report-only per E8) and folding `lock_goal_claim_scope/1`'s third goal-order encoding into
  `IntentContext`.

## Appendix — clause-2 judging artifact (fix stage, 2026-11-21)

Clause 2 names four tables as the judging artifact and the implement-stage answer listed them
without shipping them; this appendix is the durable copy of all four, re-derived at the fix stage
from the shipped bytes against the base
blob `c6b2c352`, which is also the revision clause 1's base-vs-HEAD audit is read against. It
supersedes the answer's self-description of where the tables live. The review round then moved the
builder block to `:1118-1246` and refreshed three stale comments; the shipped-bytes hash is
`9e1f5c1f2a5690e28cd03b1aca58fa3c5dce12dbd0898c1204cfbed64dafa083` (2,825 L) — the fix-stage
`342634d6…` and pre-hygiene `031c579c…` (2,828 L) hashes describe the pre-review bytes — and the
HEAD spans below are the post-move ones. Coordinates below:
`base` = `git show c6b2c352:control/lib/symmetry_control/integrations/provider_access.ex` (2,846 L),
`HEAD` = the shipped file.

### 1. Variant -> builder mapping

| Base symbol (base span) | Shipped replacement (HEAD span) | Difference |
| --- | --- | --- |
| `lock_intent_context/1` :1123-1128 | `lock_intent_context/1` :1250-1262 | same `case intent_goal_id(intent)`, same `goal_id when is_binary` / `_legacy` branches; the Goal branch now reads `goal_project_id(goal_id)` itself and passes the persisted intent's seven ids as the expected map, and `intent_ids/1` :1265-1276 hoists the struct field reads (no queries) |
| `lock_legacy_intent_context/1` :1130-1148 | `IntentContext.legacy_lock/1` :1147-1150, called from :1260 with `intent_ids/1` | same seven locks, modes, atoms and seven-key context map |
| `lock_new_intent_context/2` :1150-1160 | `lock_new_intent_context/2` :1278-1292 | same `locate_intent_context(claims) \|\| Repo.rollback(:ownership_lost)` then the same `case ids` selection; `claim_ids/3` :1296-1304 builds the expected map |
| `lock_legacy_new_intent_context/3` :1162-1186 | `IntentContext.legacy_lock/1` called at :1290 over `claim_ids/3` with `connection_id: :from_resource` | same order and atoms; the connection probe `is_binary(resource.connection_id)` moved verbatim into `lock_connection(:from_resource, _)` :1188-1190 |
| `lock_goal_intent_context/2` :1190-1216 | `IntentContext.goal_lock(goal_ids, ids)` :1257 with `goal_ids = %{ids \| goal_id: goal_id, project_id: goal_project_id(goal_id)}` | same sequence and atoms; expected map = all seven intent ids |
| `lock_goal_new_intent_context/3` :1218-1250 | `IntentContext.goal_lock(%{expected \| goal_id: goal_id}, Map.drop(expected, [:resource_id, :connection_id]))` :1284-1287 | same sequence and atoms; the expected map drops the two rows the lock derives itself (the two comparisons base's claim variant never wrote) |
| `validate_goal_intent_context_links!/3` :1252-1264 | `IntentContext.validate_links!/3` :1204-1215 with the seven-key expected map | `goal_context_links?` first, then the seven comparisons in the base order via `@link_fields` :1137-1145 |
| `validate_goal_new_intent_context_links!/4` :1266-1276 | the same helper with the five surviving keys | the same five comparisons in the same order |
| `goal_context_links?/2` :1278-1289 | `IntentContext.goal_context_links?/2` :1217-1228 | text unchanged |
| `lock_goal_work_item/1` :1323-1326 | `IntentContext.lock_work_item/1` :1194-1197 plus the `\|\| Repo.rollback(:ownership_lost)` at :1171 | binary id: same lock and atom; nil id: base's goal guard refused the same atom while the legacy sites raised Ecto's nil-comparison `ArgumentError`, HEAD refuses the same atom (unreachable; audit 9) |
| `lock_project_for_goal/1` :2815-2817 | `IntentContext.lock_project_for_goal/1` :1232-1234 plus a one-line outer forward :2798 | text unchanged (`FOR SHARE`) |
| `lock_record/2` :2819-2821 | `IntentContext.lock_record/2` :1236-1238 plus a one-line outer forward :2800 | text unchanged (`FOR UPDATE`) |
| `lock_goal_revision/1` :638-643 | `IntentContext.lock_goal_revision/1` :1240-1245 plus a one-line outer forward :638 | text unchanged (unlocked read) |
| `claim_dispatch/2` :2084-2092 + `claim_goal_dispatch/2` :2094-2126 + `claim_legacy_dispatch/2` :2128-2154 | one `claim_dispatch/2` :2096-2135 | one transaction; `goal_managed?` decides the context lock, the validation call and the `:execute` payload only; the state machine is shared verbatim |
| recover `:authorized` inline block :1962-1974 | `validate_goal_dispatch!/2` :2087-2092, called from :1975 | the same four validations in the same order behind the same guard |
| (new) `lock_scope_rows/2` :1168-1184 | — | the six-row body the four variants inlined; resource -> connection -> work_item -> task -> run -> runtime, all `FOR UPDATE` |
| (new) `lock_goal_revision_row/1` :1199-1202 | — | the revision read plus `Map.put(context, :revision, _)`; base inlined both |
| (new) `intent_ids/1` :1265-1276, `claim_ids/3` :1296-1304 | — | the two id-extraction shapes (the persisted intent's fields; the fresh claim's ids), replacing the inline field reads |

### 2. Lock order (row -> mode) per path, base vs HEAD

`U` = `FOR UPDATE`, `S` = `FOR SHARE`.

| Path | Base sequence | HEAD sequence | Result |
| --- | --- | --- | --- |
| legacy/intent | :1131-1137 Project U -> ProjectResource U -> Connection U -> WorkItem U -> Task U -> Run U -> Runtime U | `legacy_lock/1` :1148 Project U -> `lock_scope_rows/2` :1169-1174 the other six, all U | same order, same modes |
| legacy/claims | :1163-1175 same order; the connection comes from the locked resource row | `legacy_lock/1` :1290 over `claim_ids/3` with `connection_id: :from_resource`; `lock_connection(:from_resource, resource)` :1188-1190 locks `resource.connection_id`, in the same second position | same order, same modes |
| goal/intent | :1191-1200 `goal_project_id` read (unlocked) -> Project S -> Goal U -> ProjectResource U -> Connection U -> WorkItem U (guarded, :1323-1326) -> Task U -> Run U -> Runtime U -> revision read (unlocked) | `goal_lock/2` :1153 `ids.project_id` read -> :1154 Project S -> :1155 Goal U -> `lock_scope_rows/2` (resource, connection, work_item, task, run, runtime) -> `lock_goal_revision_row/1` :1199-1202 | same order, same modes |
| goal/claims | :1219-1234 the same sequence and modes | the same `goal_lock/2` body | same order, same modes |
| shared primitives | `lock_project_for_goal/1` :2815-2817 (literal `FOR SHARE`), `lock_record/2` :2819-2821 (literal `FOR UPDATE`), `lock_goal_revision/1` :638-643 (no lock) | the same three texts in `IntentContext` :1232-1245 | byte-identical |

### 3. Refusal-atom diff (expect empty)

Every emission site below keeps both its atom and its step; the diff is empty on every reachable
input (the nil-id row is unreachable, audit 9). Sites outside the collapsed family
(accept/finalize/`mark_dispatch_unknown`/claim-scope) are byte-identical by the diff hunks.
(`base` spans are one per variant that shared the step; the two goal variants emit through
`lock_goal_work_item/1` :1323-1326, recorded once.)

| Refusal and emitting step | Base sites | HEAD site | Same |
| --- | --- | --- | --- |
| `:ownership_lost`, missing Goal project id before the Project lock | :1191, :1219 | `goal_lock/2` :1153 | yes |
| `:ownership_lost`, Project row absent | :1131, :1163, :1192, :1220 | `legacy_lock/1` :1148; `goal_lock/2` :1154 | yes |
| `:ownership_lost`, Goal row absent | :1193, :1221 | `goal_lock/2` :1155 | yes |
| `:forbidden`, ProjectResource row absent (before the connection lock) | :1132, :1164, :1194, :1222 | `lock_scope_rows/2` :1169 | yes |
| `:forbidden`, Connection row absent (persisted id) | :1133, :1195 | :1170 via `lock_connection/2` :1192 | yes |
| `:forbidden`, connection not derivable from the locked resource row | :1171, :1229 | :1170 via `lock_connection(:from_resource, _)` :1188-1190 | yes |
| `:ownership_lost`, WorkItem row absent | :1134, :1172, :1230, :1326 | :1171 via `lock_work_item/1` :1194-1197 | yes |
| `:ownership_lost`, nil WorkItem id (unreachable) | :1326 (goal guard); legacy :1134/:1172 raised Ecto's nil-comparison `ArgumentError` | :1171 via `lock_work_item/1` :1194-1197 | unreachable; base goal sites refused the same atom, base legacy sites raised instead (audit 9) |
| `:ownership_lost`, Task / Run / Runtime row absent | :1135-1137, :1173-1175, :1197-1199, :1231-1233 | :1172-1174 | yes |
| `:ownership_lost`, revision row absent | :1200, :1234 | `lock_goal_revision_row/1` :1200 | yes |
| `:ownership_lost`, link re-verification mismatch or `goal_context_links?` false | :1263, :1275 | `validate_links!/3` :1214 | yes |
| `:ownership_lost`, `locate_intent_context/1` miss | :1151 | :1279 | yes |
| `:not_found`, intent row absent during a claim | :2097, :2130 | :2101 | yes |
| `:idempotency_conflict`, request-hash mismatch | :2098, :2131 | :2102 | yes |
| `:state_conflict`, state `executing` / `unknown` | :2123, :2151 | :2132 | yes |
| `Repo.rollback(reason)`, dispatch-changeset error | :2113, :2141 | :2122 | yes |

### 4. Per-path control-flow audit (lesson 32)

| # | Path | Base control flow | HEAD equivalent | Preserved |
| --- | --- | --- | --- | --- |
| 1 | legacy/intent | `lock_legacy_intent_context/1` :1130-1148: seven `\|\| Repo.rollback` short-circuits in Project -> ... -> Runtime order, no `case`, no default clause, seven-key map returned; the dispatcher decides the path from `intent_goal_id/1` | `legacy_lock/1` :1147-1150 (Project short-circuit) then `lock_scope_rows/2` :1168-1184 (six short-circuits in the same order), seven-key merged map; same dispatcher decision | yes |
| 2 | legacy/claims | :1162-1186: `locate_intent_context \|\| Repo.rollback` first, then the same seven short-circuits; the connection is `if is_binary(resource.connection_id), do: lock_record(...), else: nil` followed by `\|\| Repo.rollback(:forbidden)` | `lock_new_intent_context/2` :1278-1292 keeps `locate \|\| rollback` then the same `case ids`; `lock_connection/2` :1188-1190 keeps the `is_binary` probe; `\|\| Repo.rollback(:forbidden)` at :1170 | yes |
| 3 | goal/intent | :1190-1216: `goal_project_id` read then `\|\| Repo.rollback` before any lock -> Project S -> Goal -> six rows -> revision -> nine-key map -> validator with `unless valid?, do: Repo.rollback` | dispatcher :1253-1258 reads the project id before the call; `goal_lock/2` :1152-1164 keeps the same statement order; `validate_links!/3` :1204-1215 keeps the `unless` | yes |
| 4 | goal/claims | :1218-1250: `ids.project_id \|\| Repo.rollback` -> the same sequence; validator over the five non-tautological keys | same via `goal_lock/2` plus the `Map.drop` expected map (five keys) | yes |
| 5 | link verification | :1252-1276 and :1278-1289: `goal_context_links?` first, then an `and` chain that short-circuits on the first false; comparison order project, work_item, task, run, runtime (resource, connection additionally on the intent source) | `validate_links!/3` :1204-1215 plus `@link_fields` :1137-1145: same first check, same order, `Enum.all?` short-circuits like the `and` chain, same `unless` | yes |
| 6 | claim/goal | `claim_dispatch/2` :2084-2092 selects before the transaction; `claim_goal_dispatch/2` :2094-2126: context lock -> intent row (`intent.id`) -> hash compare -> four validations -> `case` with no default clause -> `:execute` payload `authorized_context(context, executing)` | one `claim_dispatch/2` :2096-2135 with `goal_managed?` read before the transaction: `context = if goal_managed?, do: lock_intent_context(intent)`, intent row via `intent_id` (equal to `intent.id` on every reachable path), hash compare, `if goal_managed?, do: validate_goal_dispatch!`, same `case` without default, same payload | yes |
| 7 | claim/legacy | :2128-2154: intent row -> hash compare -> `case` with no default clause -> `:execute` payload is the bare `executing` intent | same single core with `goal_managed? == false`: no context lock, no validations, bare-intent payload | yes |
| 8 | recover `:authorized` | :1962-1974: `if goal_managed_intent?(intent) do` four validations in the base claim order `end` | `if goal_managed_intent?(intent), do: validate_goal_dispatch!(intent, context)` :1975 with the same four calls in the same order | yes |
| 9 | WorkItem lock | legacy: :1134/:1172 `lock_record(WorkItem, nil)` raised `ArgumentError` from Ecto's nil-comparison guard (`not_nil!/2`), no query reached Postgres; goal bodies: :1326 rolled back `:ownership_lost` for a nil id without a query | `lock_work_item/1` :1194-1197 returns nil for a non-binary id, :1171 rolls back `:ownership_lost` without the query | yes on reachable rows; the nil id is unreachable (`null: false` + `validate_required`), where base legacy raised and HEAD refuses |
| 10 | primitives | `lock_project_for_goal/1` :2815-2817, `lock_record/2` :2819-2821, `lock_goal_revision/1` :638-643 | moved verbatim into `IntentContext` :1232-1245; the outer one-line forwards :638 (`__MODULE__.`-qualified, see the round-1 section), :2798, :2800 keep every other call site byte-identical | yes |
| 11 | transaction boundary | every path above runs inside the caller's `Repo.transaction/1`; `Repo.rollback/1` unwinds to that boundary | the same callers, one transaction per claim/recover/finalize call, no new transaction and no lock taken outside a transaction | yes |

## Comment-policy sweep (2026-11-22)

Module intent removed from `control/lib/symmetry_control/integrations/provider_access.ex` by the
2026-11-22 comment-policy sweep is recorded below (workstream record "ws8-control-lib").

- `control/.credo.exs` (header, lines 1-23): *Credo configuration for the Control
  application, added with the advisory `mix credo --strict` CI step (refactor slice
  W1-01); the step does not gate — the strict issue count is a metric and the count
  recorded on the PR is the ratchet baseline the burn-down slices measure against. The
  check list is `mix credo gen.config` output (Credo 1.7.19) with two contract changes;
  measured 459 strict issues with `checks: %{}` at 1.7.19 vs 403 from a Credo that dropped
  AliasOrder/AliasUsage/PreferImplicitTry/StringSigils/Apply (their findings sum to 67).*
  (The `%{}`-fallback why is retained in-file as a 2-line K3 note above `checks: %{`; the
  two per-check whys are retained beside their entries — see below.)
- `control/lib/symmetry_control/integrations/provider_access.ex` (1230-1231): ownership
  boundary — *The intent-context lock primitives; other locks in this file keep their own
  queries.*

The sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
