# served-doc-gates plan — real-served-document gates for the four ungated portal reads

Contract: `goal-rf-w1-13-served-doc-gates.md` (GDD, 2026-11-21). Baseline for every coordinate
here: `a3039f0d17fc7c7905b63bdf247cac5bf339b213` (`a3039f0`, origin/main, this worktree's HEAD).
Touched packages: `control/test/**` (the three controller test files the contract owns), `docs/`
(this plan, the TODO/CLOSED-TODO move). No production code, no schema edit, no fixture, no wire
change: this increment adds no member, removes no member and relaxes no scalar constraint.

## Verified state at the baseline

Only three reads have a real-served-document gate today: `chat-read`
(`control/test/symmetry_control_web/controllers/chat_controller_test.exs:133`) and
`goal-read`/`attention-read`
(`control/test/symmetry_control_web/controllers/portal_goal_attention_test.exs:414-415`). The four
reads this increment gates have schemas and manifest fixtures but no served-response gate
(`contracts/fixtures/manifest.json`: 13 `workspace-read`, 9 `work-detail-read`, 9
`work-timeline-read`, 6 `connections-read` entries; `jq -r '.fixtures[].schema' | sort | uniq -c`).

| read | route | serving projection | shared body |
| --- | --- | --- | --- |
| `workspace-read` | `GET /portal/api/workspace` | `PortalJSON.workspace/1` (`portal_json.ex:10`) ← `ReadModel.workspace/1` (`read_model.ex:26`) | `PortalJSON.work_item/2` (`:221`), `project/2` (`:70`), `resource/1` (`:85`) |
| `work-detail-read` | `GET /portal/api/work-items/:work_item_id` | `PortalJSON.work_item_detail/3` (`:263`) | same `work_item/2` row + `Protocol.timeline_page/2` (`protocol.ex:384`) for `raw.timeline` |
| `work-timeline-read` | `GET /portal/api/work-items/:work_item_id/timeline` | `Protocol.timeline_page/2` (`:384`) via `Protocol.timeline_item/2` (`:548`) | `WorkItemTimelineItem` is one definition body across both reads |
| `connections-read` | `GET /portal/api/connections` | `PortalJSON.connections_read/1` (`:28`) ← `ReadModel.connections/0` (`:49`) | `connection/2` (`:35`), `connection_resource/1` (`:55`) |

## What the existing oracle cannot see (the pin design)

`validate_served_document!/2` (`control/test/support/served_document.ex`) validates a served
document against `daemon/internal/contracts/schema_bundle.json` (all four reads are in that bundle).
All four schemas are permissive: `additionalProperties: true` and zero `false`
(`jq '[.. | objects | select(has("additionalProperties")) | .additionalProperties] | length'`,
22/34/8/4 occurrences; none `false`). So the bundle comparison fails red only for a **required**
member:

- **`connections-read` needs no member pin.** Every declared member of `Connection` (12),
  `ConnectionResource` (17) and `ConnectionProvider` (3) is in its definition's `required` array, so
  the bundle comparison refuses a serving rename of every member this surface reads. The resource
  projection also emits one member its definition does not declare (`connection_id`, from
  `PortalJSON.resource/1` under `connection_resource/1`): no connections surface reads it, and the
  workspace gate pins the same serving member in its `WorkspaceResource` row. What this gate adds is
  the bundle comparison over the populated response the existing read test already serves — two
  provider rows, one connection and one bound resource — so a rename inside any declared member is
  exercised.
- **The other three reads need presence pins for their optional-but-consumed members.** The pin per
  read is (members the frontend reads) ∩ (members optional in the schema definition that serves
  them), with the parent object present in the gate's scenario. That set is exactly the class the
  `## Deferred review findings (0007h)` TODO entry names. (The PR #145 review amendment at the end
  of this plan adds the definition's declared members to every assertion's set, so a schema-side
  rename is caught as well.)

The workspace-read set below is the entry's own enumeration (66 members); this plan re-derived it
mechanically (`jq` over every `required` array of `workspace-read.schema.json`, intersected with the
frontend read sites the entry lists) and got the same 66, which is the check on the derivation
method used for the other two reads. Members whose definition declares them `required` are left to
the bundle comparison and are not repeated here.

`WorkspaceWorkItem` is one shared definition body across `workspace-read`, `work-detail-read` and
`chat-read` (`contracts/generated/effect/index.ts`), and `PortalJSON.work_item/2` is the one
projection that serves it. The row's nested optional members (`delivery.*.provider`,
`delivery.*.recorded_at`, `external.provider_owned_fields`) are therefore pinned once, in the
workspace gate, whose scenario serves provider-sourced delivery values and an external-bound row;
the detail gate pins the row's own member names plus the detail-only skins. A rename in the shared
body goes red in both gates that serve it.

### Pin 1 — `workspace-read` (`portal_resources_test.exs`, 66 members)

| served object | members (all optional in the schema) | frontend read sites |
| --- | --- | --- |
| root | `runtimes`, `registered_runtimes` | `features/connections/resources.tsx:136`, `features/runtimes/runtimes.tsx:24` area, `app/shell.tsx` project picker |
| `WorkspaceProject` | `version`, `default_agent_profile`, `default_workspace`, `resources` | `features/projects/projects.tsx:82`, `features/goals/detail.tsx:251`, `features/work/assignment.tsx:135` |
| `WorkspaceResource` | `id`, `project_id`, `connection_id`, `kind`, `name`, `provider`, `external_ref`, `url`, `status`, `sync_status`, `status_message`, `last_synced_at`, `version` | `features/connections/resources.tsx:102-116`, `features/planning/editors.tsx` resource rows, `features/work/presentation.ts:264` |
| `RegisteredRuntime` | `runtime_id`, `runtime_name`, `agent_profile`, `workspace` | `features/connections/resources.tsx:136-154` (`referenceOptions/2`) |
| `RuntimeSnapshot` | `capacity`, `reserved_capacity`, `connection_epoch`, `last_heartbeat_at`, `repository_resource_id`, `agent_profile`, `workspace` | `features/runtimes/runtimes.tsx`, `features/connections/resources.tsx:136`, `features/chat/chat.tsx:131` |
| `RuntimeSnapshot.capabilities` | `adapter`, `interactive`, `provider_access`, `structured_input`, `supervisory_control` | `features/runtimes/runtimes.tsx:24` and its adapter rendering |
| `WorkspaceWorkItem` | `project_id`, `goal_id`, `description`, `priority`, `assignee`, `workspace`, `repository_resource_id`, `repository`, `ci_resource_id`, `ci_resource`, `branch`, `pull_request_url`, `ci_status`, `review_status`, `delivery`, `external`, `goal_managed`, `version`, `can_start`, `execution`, `updated_at` | `features/work/control.tsx:304,444` (`can_start`), `features/work/assignment.tsx:152-155`, `features/work/detail.tsx:310-343,370`, `features/planning/write-policy.ts:28`, `features/projects/work-surface.tsx` |
| `WorkItemDeliveryValue` (each of `pull_request`/`ci`/`review`) | `provider`, `recorded_at` | `features/work/presentation.ts:339,347,355` and `:293,340,348,356` |
| `WorkItemExternal` | `provider_owned_fields` | `features/work/detail.tsx:320`, `features/planning/editors.tsx:263`, `features/projects/work-surface.tsx:209` |
| `WorkItemWaitingDecision` | `context`, `reason`, `options`, `recommended_option_id` | `features/work/presentation.ts:399-409` |
| `WorkItemWaitingDecisionOption` | `id`, `label`, `consequence` | `features/work/presentation.ts:401-404` |

`WorkspaceWorkItem.position` and `RuntimeSnapshot.active_runs` stay unpinned: no feature-code read
site exists for either (grep over `frontend/src`, excluding tests), which is the one residual the
TODO entry records and no gate can assert.

### Pin 2 — `work-detail-read` (`portal_workflow_test.exs`)

- the shared `WorkspaceWorkItem` 21-name row pin above (presence only), including its nested
  consumed members — `WorkItemDeliveryValue` `provider`/`recorded_at` and `WorkItemExternal`
  `provider_owned_fields`: this gate's own scenario syncs a provider delivery and imports a
  tracker row, so the nested members are populated and pinned here too, not only through Pin 1's
  scenario on the shared `PortalJSON.work_item/2` body (the detail read keeps its own row contract
  if the two reads stop sharing the body);
- `WorkItemTimelineItem`: `source`, `run_id`, `generation`, `recorded_at`, `data` — read at
  `features/work/presentation.ts:479-521`, `features/work/receipts.ts:155-178`,
  `features/work/history.tsx:60-62`. These five are required inside each `oneOf` variant of the
  definition, so the bundle comparison already refuses their renames; the pin restates them, which
  keeps the item contract visible at the gate and survives a future relaxation of those variants;
- the `WorkItemWaitingDecision`/`Option` 7 members above, read at `features/work/presentation.ts:389-409`.

`WorkItemOutcome` and `WorkItemTimelineItem`'s variant `data` bodies (`TimelineEventData`,
`TimelineTransitionData`, `TimelineCommandData`) are all `required`-complete, so the bundle
comparison covers their renames; `raw.task` (`TaskRead`/`TaskWork`/`TaskControls`) has no
feature-code read site at all and stays additive.

### Pin 3 — `work-timeline-read` (`portal_workflow_test.exs`)

The `WorkItemTimelineItem` five names above over the paginated page's items; the root
(`items`, `next_before`) and the variant `data` bodies are required-complete, and so are the five
item names inside each variant (the pin is a restatement there, see Pin 2).

### Pass-through boundary (the vocabulary is the writer's, the copy is still pinned)

Three member groups take their member vocabulary from an upstream writer, and the current serving
projections copy them verbatim, so the pin stops at the member name the projection emits — the
names themselves are the writer's. That is no waiver: the nested pins above are exactly what
catches a serving change that stops preserving the verbatim copy (the apply-stage throwaway rename
of `supervisory_control` inside `Protocol.runtime/1` went red at the `RuntimeSnapshot.capabilities`
pin):

- `RuntimeSnapshot.capabilities` — the registered map, copied by
  `Orchestration.register_runtimes/4` (`orchestration.ex:232`) and served by `Protocol.runtime/1`
  (`protocol.ex:328`); its shape is validated at registration against
  `contracts/v1/adapter-capabilities.schema.json` (`valid_runtime_capabilities_schema?/2`,
  `orchestration.ex:4290`). The gate registers a runtime carrying all five members so the
  pass-through is exercised with every consumed member present.
- `WorkItemWaiting.decision` — the agent's own packet; the read model serves the transition's
  payload when it carries a `decision` (`read_model.ex:221-232`), and `Protocol.waiting/1`
  (`protocol.ex:493`) copies it. The gate writes the packet in its waiting transition so the
  served packet carries every member the frontend reads.
- timeline `data` payloads and `execution.result`/`failure` — open bodies; their member names are
  the agent's, pinned by the daemon-side schemas.

## Increment

1. **`control/test/symmetry_control_web/controllers/portal_resources_test.exs`** — one new gate for
   `workspace-read`, its own scenario: a project with a repository resource (provider delivery
   steered and synced), a `ci` resource, a `work_tracking` resource synced to an external row, an
   adapter-backed runtime registered with all five capability members, and one work item bound to
   the repository and the `ci` resource, assigned to that runtime's binding, run, claimed and
   transitioned to `waiting_for_input` with a decision packet. The gate asserts the served rows,
   the pins above, and `validate_served_document!("workspace-read", served)`.
2. **`control/test/symmetry_control_web/controllers/portal_workflow_test.exs`** — one new gate
   serving `work-detail-read` and `work-timeline-read` from the same scenario (a provider-synced
   repository and an imported tracker row — so this scenario itself serves the shared row's
   `external` row and provider-sourced delivery values and this file pins them too — then that
   item's run + claim + `running` + `waiting_for_input` with the decision packet + one event, two
   transitions (running and waiting) and one command so `raw.timeline`/`items` carry all three
   sources), asserting both pins and both `validate_served_document!/2` calls.
3. **`control/test/symmetry_control_web/controllers/portal_integrations_test.exs`** — the existing
   `connection read lists provider policy, connections and bound resources` test captures the whole
   response, asserts it is populated by the three definitions' rows and calls
   `validate_served_document!("connections-read", response)`, with a comment recording that every
   member the surface reads is `required` (so the bundle comparison refuses its renames) and that
   the projection's undeclared `connection_id` extra is read by no connections surface and pinned
   by the workspace gate.
4. **`docs/TODO.md` / `docs/archive/CLOSED-TODO.md`** — the remaining `## Deferred review findings
   (0007h)` item (the workspace-read row) is closed by option (b) of its own text; the entry moves
   to `docs/archive/CLOSED-TODO.md` under that heading with the landed behaviour and the measured
   evidence, and `docs/TODO.md` loses it.

Each pinning file carries its own private `assert_served_members!/3` + `assert_served_rows!/3` (about
ten lines): `control/test/support/**` is outside this increment's owned files, and a module defined
in one test file would break a single-file run of the others. (`portal_integrations_test.exs` pins
no member — every declared member of its read is `required` — so it adds neither helper.) The
review amendment below adds the bundle-derived `gate_members/3`/`declared_members/2` to the same
private block; `docs/TODO.md` (PR #145 findings) records moving the block into
`SymmetryControlWeb.ServedDocument` once the increment that owns `control/test/support/**` (W1-12)
has landed.

## Exact edge-case semantics

- **Presence is key presence, not non-nullness.** `{"goal_id": null}`, `can_start: false` and
  `external: null` are served members; an omitted key is drift. The helper asserts
  `Map.has_key?/2`, never truthiness, so a legitimate `null` stays accepted while a rename or an
  omission fails red.
- **Non-vacuity is asserted, not assumed.** Every collection the pins walk must serve at least one
  row (`runtimes`, `registered_runtimes`, `resources`, `work_items`, `connections`, `providers`,
  timeline `items`): a gate that passed over an empty array would pin nothing.
- **Conditional members are reached by state, not by weakening the pin.** `delivery.*.provider` and
  `delivery.*.recorded_at` exist only in the provider-sourced branch of `effective_delivery/6`
  (`portal_json.ex:519-534`), and `external.*` only when `external_work_item_resource_id` is set
  (`:545`), so both gates' scenarios sync a provider delivery and import a tracker row instead of
  relaxing those names (the workspace gate through its steering provider, the detail/timeline gate
  through the support `ProviderStub`, whose tracker sync imports the external row and whose
  repository sync writes the provider-sourced delivery values).
- **The waiting packet is served from the transition when it carries a decision.** The gate's
  waiting transition carries `question` and `decision`, which is the authoritative-packet branch
  (`read_model.ex:221`); the served `execution.waiting.decision` is then the packet's own members.
- **Timeline items keep every source.** `Protocol.timeline_page/2` merges events, transitions and
  commands (`timeline_item/2`, `protocol.ex:548`); the gate's scenario produces one of each so the
  five item members are pinned on a real union member and not on one variant.

## Evidence plan

All gates foreground, in this order (never the full suite beside another suite or a harness on this
host):

1. `sh <HOST-PATH>/rf-w1-13-mix.sh format --check-formatted` (portable
   Elixir 1.20.4 / OTP 29 recipe adapted from `served-read-closeout-mix.sh` for this worktree,
   `MIX_DEPS_PATH` = the main checkout's `deps`, `MIX_TEST_PARTITION=rf-w1-13`,
   `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset).
2. `mix compile --warnings-as-errors`.
3. `mix test test/symmetry_control_web/controllers/portal_resources_test.exs
   test/symmetry_control_web/controllers/portal_workflow_test.exs
   test/symmetry_control_web/controllers/portal_integrations_test.exs` — the four gates plus the
   files' existing coverage.
4. **Mutation checks, each reverted** (throwaway serving-side renames, the recorded red is the
   judging artifact):
   - `PortalJSON.work_item/2` `goal_id` → `goal_id_renamed` (optional consumed, shared row): the
     workspace gate must fail on the presence assertion, not on the bundle comparison — the failure
     message names the missing member.
   - `Protocol.timeline_item/2` `recorded_at` → `recorded_at_renamed` (required in each `oneOf`
     variant — the bundle refuses it; the pin restates it): the detail gate must fail inside its
     bundle comparison at `#/raw/timeline/0-3`, and the standalone page carries the same items at
     `#/timeline/0-3`.
   - `PortalApiController.work_item_timeline/2` serving the page without `next_before` (the
     standalone route is the only serving surface a page-level rename can red alone — every page
     member is also embedded, and required, in the detail document): the `work-timeline-read`
     validator must fail on the page root while the detail comparison stays green.
   - `PortalJSON.connection/2` `auth_type` → `auth_type_renamed` (required): the connections gate
     must fail inside `validate_served_document!/2`.
   - `read_model.ex`'s authoritative-packet selection is *not* mutated: the packet members are the
     agent's, and the increment's mutation oracle is the serving projection.
5. `git diff --check` at every stage; `node --version`-free: `pnpm contracts:check` at the
   repository root (no `contracts/` path changes, so this is expected green and is recorded with
   its exit code).
6. Full `mix test` through the conductor gate-runner (separate step, never beside another suite).

## Design trade-offs and decisions

1. **Option (b) of the 0007h entry, not option (a).** Option (a) (dropping the tolerance and
   requiring every always-emitted consumed member) moves three schema bodies, their valid fixtures,
   the frontend fixture layer and the generated Go/TS output together, and it changes whether an
   older partial workspace projection is still renderable — a product decision, and out of this
   contract's scope ("schema/fixture changes beyond small additions"). Option (b) — pin the serving
   side on a real served document — closes the drift class with test-only changes and no renderable
   behaviour change. The pins are member-name lists, so a future *legitimate* removal of a consumed
   member is a deliberate edit of the pin rather than a silent pass.
2. **The pins live in the controller tests, not in `contracts/scripts/boundary.test.mjs`.** The
   boundary probe renames members on *fixture bytes*; the rename class here is a serving-side
   rename, which only a real response can catch, and `contracts/` is outside the owned files.
3. **One scenario per gate family, not four copies.** The detail and the timeline read are served
   from one scenario in `portal_workflow_test.exs` because both read the same durable rows, and
   splitting them would duplicate the claim/transition fixture without adding coverage. The two
   reads still have separate pins and separate `validate_served_document!/2` calls, so a failure
   names its read.
4. **`connections-read` gets the bundle comparison, not a decorative member list.** Every member is
   required in its schema; restating the names in the test would assert nothing the bundle does not
   already compare, and the contract's failure control is a rename that stays green — which the
   bundle comparison cannot do here.
5. **No production change.** No projection, read model, schema or fixture changes; the increment is
   test-only plus the TODO archive move.

## Review amendment (PR #145, P2-1): schema-derived member sets

The adversarial review of PR #145 found the gates went red only for a *serving-side* rename: every
pin was a hand-maintained member list, so renaming a consumed optional member in
`daemon/internal/contracts/schema_bundle.json` (`WorkspaceWorkItem.properties.goal_id` ->
`goal_ref` in the two gate reads' schemas, projection untouched) left both gates green — the bundle
comparison accepts the old name as an additional property and the new optional name as absent, and
the pin still finds the old name.

Each presence assertion now walks `gate_members(read, definition, frontend_members)`: the
definition's declared members read from the contract bundle at test time (`declared_members/2`,
`oneOf` variants included) plus the frontend-read list the call passes. `:root` names the read's own
schema, so the workspace root assertion now also walks `projects` and `selected_project_id`. The
schema side turns the bundle-side rename red; the frontend side keeps every member a `frontend/src`
read site consumes pinned by name; a serving-side rename or omission fails on either side, in the
same "stopped serving" message.

Evidence (`MIX_TEST_PARTITION=w113fix`, every probe reverted before the final run):

- pristine worktree, three gated files — `Result: 45 passed`, exit 0, with `mix format
  --check-formatted` and `mix compile --warnings-as-errors` green;
- scratch copy of the tree with the `goal_ref` bundle rename — both gates fail on the presence
  assertion: `workspace-read work row stopped serving ["goal_ref"]` and `work-detail-read work row
  stopped serving ["goal_ref"]`, exit 2, `Result: 0/2 passed, 28 excluded`;
- worktree with `PortalJSON.work_item/2` serving `goal_id_renamed` — both gates fail on the
  presence assertion: `stopped serving ["goal_id"]`, exit 2, `Result: 0/2 passed, 28 excluded`.

## Out of scope

- Production projections and any `PortalJSON`/`Protocol` change (W2-04 owns those).
- Schema requiredness (option (a)) and regeneration of the generated TS/Go output.
- Fixture or manifest changes (none is needed: the pins are asserted on live responses).
- The other ungated surfaces' reads (`/portal/api/attention` is already gated; `chat-read` and
  `goal-read` keep their existing gates).
- `WorkspaceWorkItem.position` and `RuntimeSnapshot.active_runs`, which have no feature-code read
  site (the P2-1 amendment's bundle-derived assertion walks them like every other declared member;
  no frontend-read list pins them).

## Comment-policy sweep (2026-11-22)

Module intent removed from
`control/test/symmetry_control_web/controllers/portal_integrations_test.exs` by the 2026-11-22
comment-policy sweep is recorded below (workstream record "ws9b-control-test").

Quotes are the load-bearing sentences of the deleted blocks; the TSV marks every line `deleted`/`M1`.
Authoring commits from the inventory's blame attribution:

2. `control/test/symmetry_control_web/controllers/portal_integrations_test.exs` 113-124
   (`857e763b8`, #145). "The connections-read gate: this real served document is compared with the
   canonical bundle. Every declared member sits in its definition's `required` array — `Connection`
   12, `ConnectionResource` 17, `ConnectionProvider` 3 — so the bundle comparison refuses a serving
   rename of every member the surface reads and no member needs a hand-written presence pin. The
   resource projection also emits one member the definition does not declare (`connection_id`, from
   `PortalJSON.resource/1` under `connection_resource/1`): no connections surface reads it, and the
   workspace gate pins the same serving member in its `WorkspaceResource` row."

The sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
