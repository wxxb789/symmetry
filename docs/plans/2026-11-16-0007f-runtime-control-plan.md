# 0007f runtime visibility and execution control — implementation plan

Revision: branch HEAD `7a92ca498f60309b97920b9df834f2637b91cfbd` (merged 0007b).
Contract: `docs/goals/0007f-runtime-control.md`. Shape reference: 0007b
(`docs/evidence/goal-0007b-readable-work-views-2026-11-15.md`).

## Public/API shape

No new wire control route, no new command envelope, no permission change. The
four execution controls already exist on the Portal API and are consumed
unchanged:

| Operation | Route | Body (existing contract) |
| --- | --- | --- |
| start | `POST /portal/api/work-items/:id/run` | `{action_id}` |
| cancel | `POST /portal/api/work-items/:id/cancel` | `{action_id, generation}` |
| retry | `POST /portal/api/work-items/:id/retry` | `{action_id, generation}` |
| waiting input | `POST /portal/api/work-items/:id/input` | `{input, action_id, waiting_transition_id}` |

Additive **read** members only, in the same class as 0007b's read-schema
entries (the servers already serve them; the schemas describe what is consumed):

1. `contracts/v1/workspace-read.schema.json` gains a consumed `runtimes` array
   and, on its row, a consumed `capabilities` object that reuses
   `common.schema.json#/definitions/Adapter` plus the four existing boolean
   capability members. The daemon/protocol side already validates this exact
   shape (`valid_runtime_capabilities_schema?` →
   `ContractValidation.validate_adapter_capabilities`), and Control persists the
   map verbatim; the read schema describes it so the frontend can decode
   `hosting_model`, harness kind/version and the ten per-operation entries
   through generated DTOs instead of hand-written types.
2. `contracts/v1/workspace-read.schema.json` + `work-detail-read.schema.json`
   gain a consumed `goal_id` (nullable UUID) on `WorkspaceWorkItem` so the
   surface can tell Goal-managed work from legacy work without re-deriving the
   server's relational triple. `PortalJSON.work_item/2` starts serving it; the
   field is optional in the schema because the row keeps accepting older partial
   projections.

Everything else — `can_start`, `can_cancel`, `can_retry`, `can_guide`,
`can_pause`, `can_resume`, `waiting.transition_id`, `latest_command`,
`timeline[].data.acknowledgement_outcome` — is already served and already
described by the 0007b schemas.

Frontend routes (`frontend/src/app/router.tsx`, basepath `/portal`):

| Route | Content |
| --- | --- |
| `/portal/runtimes` | machine status and verified adapter capabilities |

`/portal/runtimes` is added to the authenticated shell route list in
`control/lib/symmetry_control_web/router.ex` and to the anchored login
`return_to` allowlist in `portal_session.ex`
(`@shell_path` gains `runtimes`, no query parameters accepted), with the
existing rejection behaviour preserved for every other path.

## Exact edge-case semantics the contract demands

| Contract clause | Semantics implemented |
| --- | --- |
| queued request never shown as applied | Client-side receipt states are derived from durable evidence only: `applied` requires `acknowledgement_outcome == "applied"`; while a POST is in flight the receipt is `requesting`/`queued`; a command whose state is non-terminal and whose outcome is null renders `awaiting agent acknowledgement`; a receipt the refreshed snapshot does not carry renders `outcome unknown` (never applied) with a link to durable history. |
| unknown outcome never rendered as success | Same rule; `unknown` is a distinct tone and text (`Outcome unknown`), and no success tone is used for it. Success tones are reserved for `applied` and for terminal task states read from the server. |
| requested/queued/applied/rejected/failed/unknown distinct across generations | Receipt cards carry the served `generation` (null for task-scoped cancel/retry commands) and the served `kind`; history renders one card per timeline `command` entry, so generation 1 and generation 2 receipts coexist. Chat-dispatched `guidance`/`pause`/`resume` receipts are displayed from the same timeline data with no control route invented for them. |
| unsupported operations hidden or explained | Buttons render only when the server's `can_*` flag is true. When a capability is advertised unsupported (`capabilities.adapter.operations.pause`/`guidance`), the surface renders an explicit `Unsupported · <reason class>` line and never a disabled live control; absent adapter metadata renders `not advertised`, not `supported`. |
| runtime rows match the advertised record | Every rendered value is the served value: machine name, runtime name/key, status, capacity, agent profile, workspace, repository resource affinity, connection epoch, harness kind, native version, implementation version, protocol version and all ten operation entries, including explicit `false`/`unsupported`/`unknown`. No default is substituted for a missing member. |
| Goal-managed work routes to Goal admission | A row is Goal-managed when the server serves a non-null `goal_id`. Such a work item renders no start/cancel/retry/input control and no legacy move path; it renders a `Goal-managed` note pointing at the Goal's admission (its Goal-origin item admits work through the Goal), and the surface never issues a legacy control request for it. Legacy paths already refuse server-side with `goal_authority_required`. |

Receipt generation binding: cancel and retry send the generation read from the
snapshot that rendered the control; a server `state_conflict` is a definitive
rejection receipt, not a silent refetch. Input sends the waiting
`transition_id` from the same snapshot; a stale transition is a definitive
rejection. The same `action_id` is reused for a retry of an unconfirmed
request (session storage keyed by kind + work item + scope), matching the
existing classic surface.

## Evidence plan

Per-clause red-before/green-after: the implementation lands with assertions
that fail on the pre-change tree and pass after. Two red classes exist and both
are recorded: (a) tests that cannot pass without the new route/surface, run
against the pre-change release; (b) tests that assert a specific new behaviour
(receipt-state wording, capability text, Goal routing, focus/draft retention)
run against the pre-change build.

| Done-when item | Evidence |
| --- | --- |
| assignment + waiting-for-input flows on a real daemon | live-daemon browser suite (fake-agent profile `wait_input`) on the new route, both widths where applicable |
| cancel/completion race, failure-repair retry, stale command, unsupported capability | live-daemon browser: slow-mode cancel of a running process; `fail_once_then_evidence_success` retry across generations; stale-generation and stale-transition rejections rendered as rejection receipts; capability line for a runtime whose adapter advertises `pause: unsupported` |
| receipts across generations | rendered receipt history assertions over the served timeline (command entries for generations 1 and 2, plus guidance/pause receipts preserved as display-only) |
| history pagination, refresh, disconnect, reconnect | existing live-daemon cursor case on the new route plus the synthetic obsolete-failure guard; a new real-API case that takes the control plane's session cookie away, asserts the surface's recovery path, and reconciles the refreshed server snapshot with in-flight local state |
| runtime/capability rows match the advertised record | a live daemon with real harness kinds (`pi` 1.5.2 and `omo` 5.1.69, both reproduced against their repository pins on this host) renders rows beside the daemon's own registration payload read back from Control, plus the browser comparison on the same revision |
| Goal-managed work item | fixture created through Goal plan admission with exactly one required item; detail view assertion plus a direct legacy-path refusal probe |
| pre-existing coverage migrated, classic checks retired | the classic control assertions move to the new route in the same PR; the retired spec/test list is in the evidence record with the executed suites and counts |

## Out of scope (with reason)

- Guidance/pause/resume dispatch from Chat — 0007g owns that route; this
  increment displays their receipts only.
- Provider connections, resource binding/sync — 0007d/0007e.
- Goal-specific screens and the Goal admission UI — the Goal workspace is its
  own surface; this increment only refuses the legacy path and states the
  correct authority.
- Native capability promotion of any harness — excluded by the contract; this
  increment reads whatever the runtime advertises.
- Server-side pagination or a second list endpoint — would be a wire-contract
  addition needing an amendment (0007b precedent).

## Design trade-offs

- **Capability data source**: served `capabilities` map rather than a new
  operator read route. The map is already validated at registration time and
  already served; a new route would duplicate an authority for no gain.
- **Runtime scoping**: `/portal/runtimes` renders the workspace read's `runtimes`
  array (all registered runtimes) — the same projection the work surface uses,
  reached through `GET /portal/api/workspace?project_id=…` and therefore the same
  query key the shell already holds, so the page adds no new request when the
  project is already loaded.
- **Receipts**: durable-evidence-only derivation (no optimistic "applied"), so
  the local pending state can never be mistaken for a server acknowledgement.
- **Goal routing**: rely on the served `goal_id`, not on the
  `integration`/`admitted_revision`/`acceptance_contract` triple, because the
  server's own legacy guard keys on task goal ownership and the Goal workspace
  is the authority for further detail.

## Ambiguities and chosen defaults (reported, reversible)

1. Should `/portal/runtimes` show only runtimes compatible with the selected
   project? Default: show all registered runtimes, since the contract asks what
   the runtime advertises, and label nothing as project-scoped.
2. Should the work-detail surface render a control route for Goal admission?
   Default: no — state the authority and name the Goal; adding a route would be
   a new wire control route, which the contract fences.
3. Should a waiting-input decision packet be answerable from the detail view?
   Default: yes for the plain form the classic surface already used (`input:
   {answer}`), and the advisory decision packet is displayed with Chat as the
   decision route (0007g).
