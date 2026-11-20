# served-read-closeout plan — revalidate and close the optional-but-consumed served-read hardening

Contract: `goal-served-read-closeout.md` (GDD, 2026-11-20). Baseline for every coordinate here:
`fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, origin/main, the worktree's HEAD).
Touched packages: `contracts/` (pnpm `contracts:check`), `control/` (Elixir served-document
gate), `docs/`. No wire change: this increment adds no member, removes no member and relaxes no
scalar constraint.

## Verified state at `fdf332d` (contract clause: confirm per member + client read anchor)

The TODO entry ("Harden the optional-but-consumed members of the served reads …") names eleven
members. Each one is already inside a `required` array of the schema that describes it, and each
one is read by the frontend after the boundary decode:

| Member (schema pointer) | `required`? | Client read anchor | Server emit site |
| --- | --- | --- | --- |
| `goal-read#/definitions/GoalReadAccounting` `known_usage_cost_microusd` | yes | `frontend/src/features/goals/detail.tsx:610` | `control/lib/symmetry_control/goals/read_model.ex:2555` (`accounting_projection/4`, defined `:2539`) |
| `goal-read#/definitions/SettlementAttention` `proposed_next_action` | yes | `detail.tsx:316` | `read_model.ex:1562` (`settlement_entry/8`, defined `:1536`, which `task_settlement_entry/9` delegates to) |
| `…/SettlementAttention` `proposed_next_action_status` | yes | `detail.tsx:316` | `read_model.ex:1563` |
| `goal-read#/definitions/GoalReadExternalWait` `external_ref` | yes | `detail.tsx:626` | `read_model.ex:2623` (`external_wait_projection/1`, defined `:2610`) |
| `…/GoalReadExternalWait` `next_check_at` | yes | `detail.tsx:627` | `read_model.ex:2626` |
| `…/GoalReadExternalWait` `check_seq` | yes | `detail.tsx:627` | `read_model.ex:2627` |
| `goal-read#/definitions/GoalReadOutcome` `candidate_subject` | yes | `detail.tsx:534` | `read_model.ex:2265` (`outcome_projection/1`, defined `:2255`) |
| `…/GoalReadOutcome` `subject_hash` | yes | `detail.tsx:272,531,537` | `read_model.ex:2266` |
| `attention-read#/definitions/GoalAttentionEntry` `attention_reasons` | yes | `frontend/src/features/attention/attention.tsx:92` | `read_model.ex:2657` (`attention_entry/1`, defined `:2647`) |
| `…/GoalAttentionEntry` `active_tasks` | yes | `attention.tsx:90` | `read_model.ex:2658` (`execution.active_task_count`) |
| `…/GoalAttentionEntry` `nonterminal_tasks` | yes | `attention.tsx:90` | `read_model.ex:2659` (`execution.nonterminal_task_count`) |

Two further confirmations, both mechanical (optional sets extracted from the three read schemas
with `jq`, then cross-read against `frontend/src`):

- `goal-read.schema.json` and `attention-read.schema.json` have **zero** optional-and-consumed
  members. Every member their surfaces read is pinned; the optional ones (`goal-read` root
  `project_name`/`updated_at`/`lifecycle`/`history`/`execution`/`planning`/`attention_reasons`,
  the goal work item's planning/assignee fields, the attention entry's `blockers`/
  `settlement_attention`/`updated_at`, the optional `SettlementAttention` members) are
  unconsumed and stay additive.
- `workspace-read.schema.json` leaves 68 members optional (a `jq` walk of every `required` array in
  the file); 66 of them are read by `frontend/src` feature code, and only two are not
  (`RuntimeSnapshot.active_runs` and `WorkspaceWorkItem.position`, each with no feature-code read
  site). The four `RuntimeCapabilities` booleans (`interactive`, `provider_access`, `structured_input`
  and `supervisory_control`) are consumed only as the whole map (`runtimes.tsx:24` serializes
  `runtime.capabilities` into `data-capabilities`, which
  `browser/tests/portal-live-daemon.spec.mjs:433` asserts equals the registration-side map), so they
  have no member-named read site. That is the item's first residual and the only surviving instance
  of the class.

## What remains, and the shape of the increment

The class-level fix direction ("marking them `required` … would close the class", `3bc87b0`) is
implemented for both goal surfaces. The item's residual text names two things left: (1) the
workspace-read row's consumed members stay optional, so its surfaces keep rendering their own
fallbacks; (2) the goal-read fixtures carry `accepted_outcomes`, `settlement_attention` and
`external_waits` as empty arrays, so those members are pinned by the boundary test and the
real-served controller gate "rather than by a fixture-level rename probe".

Increment:

1. **Close residual (2) at the server.** The real-served controller test
   (`control/test/symmetry_control_web/controllers/portal_goal_attention_test.exs`, "the real
   served attention and goal reads validate against the contract bundle") currently serves a
   Goal whose `accepted_outcomes` and `external_waits` are empty, so the bundle gate never
   exercises those member shapes on a real response. Extend its durable-row fixture with one
   accepted `work_outcomes` row (over a completed producer run plus a bound evidence row, carrying
   the columns `Goals.insert_validation_outcome!/13` writes; the acceptance pipeline's separate
   validate task and its run are not built here, and the producing result id and the evidence row
   are minted by the gate) and one `goal_external_waits` row over a
   third plan item whose completed run reported a blocked-external result, which is the shape
   `Goals.create_external_wait!/8` writes and the only shape the `goal_external_waits` identity
   trigger admits (completed task and run, the run's `task_result` equal to the wait's `result`,
   a terminal `state` with no schedule). Assert the served values and keep
   `validate_served_document!("goal-read", read)`. A serving-side rename of `candidate_subject`,
   `subject_hash`, `external_ref`, `next_check_at` or `check_seq` now fails the bundle gate
   instead of decoding into a silent fallback.
2. **Close residual (2) at the fixture.** Capture the served document as a new positive fixture
   `contracts/fixtures/valid/goal-read.accepted-and-external.json` (identifiers, labels and timestamps
   replaced; member names and shapes untouched) plus its manifest row, and run the boundary
   rename probe for the seven members of the three collections over that fixture's own bytes —
   deleting the three inline synthetic documents the probe needs today. The probe's
   `renameMember/2` helper asserts the member exists, so the probe fails on the pre-increment
   fixture set and passes after: the recorded before/after evidence for this half.
3. **Record the residual that stays open.** The workspace-read row keeps its members optional:
   closing it is a product decision (see trade-offs) and per-surface work, and the entry itself
   scopes it out of this class. It stays in `docs/TODO.md` as its own item, with a pointer to the
   archived closure.
4. **Archive.** The class closure, the eleven-member confirmation and the residual-(2) closure
   narrative move to `docs/archive/CLOSED-TODO.md` under the existing "Deferred review findings
   (0007h)" heading.

## Exact edge-case semantics the contract demands

- **Requiredness is key presence, not non-nullness.** Every newly confirmed member stays
  nullable where the projection can serve a `null` (`next_check_at`, `reason`,
  `candidate_subject`): `{"state": "unsupported", "next_check_at": null}` is a real served wait,
  and the gate must keep accepting it while refusing an omitted key.
- **`external_waits` has one producible live shape.** `Goals` writes `goal_external_waits` rows
  through `GoalExternalWait.changeset/2`, whose states are `satisfied|failed|cancelled|
  unsupported` with `next_check_at` required absent (`control/lib/symmetry_control/goals/
  schemas.ex:1435,1595-1601`); the served document therefore carries
  `state: "unsupported"`, `reason: "unsupported_external_check"`, `check_seq: 0`,
  `next_check_at: null`. The DB check constraint admits the same four states
  (`20260909100000_add_goal_external_wait_unsupported_state` replaced the create-table
  `waiting|checking|unknown` set), so the gate fixture uses the writer's shape and does not
  invent a state the server cannot serve; the embedded task result it copies verbatim keeps the
  future blocker schedule the settle path validates (`validate_external_blocker/3` refuses a
  `null` schedule), so the result bytes are a shape the writer admits.
- **An accepted outcome's `candidate_subject` is the canonical Subject object** (`resource_id`,
  `commit`, `tree_digest`) and `subject_hash` is served as `sha256:<hex>`
  (`read_model.ex:2828`); the fixture carries both, so the rename probe pins the exact spelling
  the client reads at `detail.tsx:534,537`.
- **The boundary probe refuses with a `ParseError`**, the same assertion the existing goal and
  attention probes make; renamed *unconsumed* members must still decode (the existing additive
  control is kept).

## Evidence plan

1. `mise x pnpm@12.17.0 -- pnpm install --frozen-lockfile` (repository root).
2. `mise x pnpm@12.17.0 -- pnpm contracts:check` — typecheck, `contracts/scripts/*.test.mjs`
   (boundary + generator tests), fixture/manifest + generated drift check (`check.mjs`),
   Effect-graph check (`check-effect.mjs`).
3. Before/after for the probe: the probe's fixture-byte assertions run against the pre-increment
   fixture set (recorded failure) and against the increment (recorded pass). The pre-increment
   run uses the increment's probe code with the pre-increment fixtures, so the recorded failure
   is the missing capture, not a missing test.
4. Elixir: `mix format --check-formatted`, `mix compile --warnings-as-errors`, and the full
   suite through the `retro-g2` recipe with `MIX_TEST_PARTITION=servedreadcloseout` and
   `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset (deps reused from the main
   checkout, identical `mix.lock`).
5. Go: the manifest fixtures are decoded by `daemon/internal/protocol` and
   `daemon/internal/contracts`, so run those packages' tests on Windows after adding the
   fixture (the changed path is `contracts/`, not `daemon/`; the Linux trigger does not fire).
6. `git diff --check`; no `docs/evaluations/**` or `docs/evidence/**` path changes, so the
   record-cleanliness gate stays a vacuous run and is reported as such.

## Design trade-offs and decisions

1. **Residual (2) is closed with a real capture, not by editing the expected names.** Both the
   gate half and the fixture half take the server's own spelling: the Elixir test asserts the
   members on a served response, and the fixture is that response sanitized. The alternative
   (declaring the boundary test's inline documents sufficient) leaves a serving-side rename of
   `external_ref`/`check_seq`/`candidate_subject` passing every gate, because no gate compares a
   populated server response against the schema.
2. **Residual (1) stays open; the options, not a silent choice.** Option (a): drop the tolerance and
   require every always-emitted consumed member; because `WorkspaceWorkItem` is one shared definition
   body across `workspace-read`, `work-detail-read` and `chat-read`
   (`contracts/generated/effect/index.ts:633`), that single requiredness edit moves the required
   arrays, the valid fixtures of all three schemas, the frontend fixture layer
   (`frontend/src/services/api.fixtures.ts`) and the generated Go DTOs together, while the
   `RuntimeCapabilities` members and the `WorkItemWaitingDecision` packet stay optional either way
   because the projection passes the stored registration map and the agent's own waiting payload
   through verbatim. Option (b): keep the tolerance and pin the serving side instead, by validating
   a real served `workspace-read` document against the contract bundle with the consumed members
   asserted on that output (`validate_served_document!` today pins `chat-read`, `goal-read` and
   `attention-read` only, so no gate compares a served workspace read with the contract at all).
   Recommendation, pending the owner: (b) is the smaller, reversible step and keeps older
   projections renderable; (a) is the class-consistent one. This increment records neither option;
   the choice decides whether an older partial workspace projection is still renderable
   (`contracts/README.md:22-23` records the tolerance as "keeps accepting older partial
   projections").
3. **The increment adds no second mechanism.** The gate reuses `ServedDocument`
   (`control/test/support/served_document.ex`), the fixture reuses the manifest, and the probe
   reuses `renameMember/2`; nothing new is introduced beside one fixture file and one manifest
   row.
4. **No frontend or browser change.** The fixture is not imported by the frontend fixture layer
   (`frontend/src/services/api.fixtures.ts` imports `goal-read.active.json`), so no surface
   behaviour moves and the browser render gate's trigger does not fire.

## Out of scope (contract clause 4)

Other TODO entries; the workspace-read hardening (trade-off 2); the surfaces' own fallback
copy; any schema requiredness change or regeneration (none is needed — the named members are
already required); unrelated refactors.
