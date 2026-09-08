# Frontend implementation and interaction contract

Normative target. Replace the current portal UI completely while retaining its
behavior and existing domain APIs. No production Node server and no LiveView
component tree wrapping a second React application.

## Stack and module ownership

- `frontend/`: React + TypeScript, Vite, pnpm, TanStack Router, TanStack Query,
  Effect 3 stable, Tailwind CSS, Radix primitives, Lucide icons.
- Vitest + Testing Library for behavior/unit tests; existing Playwright for
  cross-service browser acceptance. React Flow only for the goal dependency
  projection; do not introduce an editable workflow canvas.
- Root pnpm workspace includes `frontend/` and `browser/`, one pnpm lockfile.
  Convert browser dependency installation from npm only when CI is migrated
  together. Preserve existing tests and their fixture semantics.
- `frontend/src/app/`: providers, routing, bootstrap and one Effect runtime.
  `features/{attention,projects,work,chat,connections,runtimes}/`: views and hooks.
  `services/`: API, event subscriptions and typed error mapping.
  `components/`: reused UI compositions. `generated/`: generated wire DTOs.
  Do not create an empty directory for every possible future feature.

TanStack Router owns URLs, TanStack Query owns server cache, Effect owns IO and
resource lifetime, React owns local view state. No Redux/Zustand/Effect cache
duplicating Task/Goal records. Use TanStack Table for list sorting/columns when
the product needs it, not a handmade generic data-grid framework.

Effect `Schema` decodes unknown API/event data; typed tagged errors distinguish
Unauthenticated, Forbidden, Conflict, Validation, Unsupported, Network and
UnexpectedResponse. The query adapter returns a Promise and forwards AbortSignal
into Effect/runtime cancellation and the underlying fetch. Read retry belongs to
Query, capped and limited to transient faults. Effect performs one HTTP attempt.
Mutation retries default off; an explicit retry reuses the original mutation ID
and byte-equivalent normalized payload. No retry on 401/403/409/422.

Query keys include project/goal/work IDs and filters. Session expiry cancels
in-flight work and clears private cache. Background invalidation cannot overwrite
drafts or an optimistic edit belonging to another selected resource.

## Build and deployment

Development: Vite serves the SPA and proxies `/portal/api`, `/portal/login`,
`/portal/logout` and `/socket` to Phoenix, including WebSocket proxy. Bootstrap
loads same-origin session/CSRF data from Phoenix. Proxy cookie/origin settings
must match the configured dev origin; do not disable CSRF to fix development.
Keep the server-rendered login page and redirect unauthenticated navigation to it.

Production: Vite builds index.html and hashed assets into a staging directory,
then the release build copies them into `control/priv/static/portal/`.
Phoenix serves assets and authenticated SPA HTML. Cache hashed assets immutably;
HTML/bootstrap are no-store. Asset URLs use the `/portal/` base. No runtime npm.
Place the SPA catch-all after `/portal/api`, login/logout and static asset routes;
unknown API or asset paths remain errors, not index.html. Test direct deep links.

Old `/portal#chat` and other documented hash locations redirect once to the new
equivalent URL. Domain JSON endpoints retain compatibility; new goal projections
are additive. Remove old portal.js/CSS/EEx workspace renderer after parity;
retain the login template. A cutover must not leave two writable UI state models.

Target scripts (implement these exact entrypoints):
`pnpm --filter @symmetry/frontend typecheck`, `lint`, `test`, `build`;
`pnpm --filter symmetry-browser-acceptance test`;
root `pnpm contracts:generate` and `pnpm contracts:check`.
typecheck uses `tsc --noEmit`; build does not substitute for typecheck.
Lint uses typescript-eslint type-aware rules plus React hooks rules.

## Information architecture

| Route | Required first-screen content |
| --- | --- |
| `/portal/attention` | decisions, blockers and reviewable results; default landing |
| `/portal/projects/:projectId` | saved list/board view, filters, work summary |
| `/portal/goals/:goalId` | objective, accepted progress, blocker, next authorized action |
| `/portal/work/:workItemId` | outcome, artifact/check evidence, context and decision links |
| `/portal/chat` | workspace/project/run conversation with explicit scope |
| `/portal/runtimes` | machine status and verified adapter capabilities |
| `/portal/connections` | GitHub/ADO resource bindings, health and sync state |

Runs are linked from work detail; their transcript is drill-down detail rather
than the default product surface. A goal's dependency graph is a secondary view.
Saved filters/columns are view preferences, not an additional workflow state.
Initially keep them in versioned localStorage under a user/deployment scope;
never store credentials, evidence, session handles or authoritative task data.

## Visual and interaction decisions

Use one restrained neutral palette with a single action accent and semantic
status colors. Central tokens define spacing (4px base), typography, border,
surface and focus styles. Default desktop shell: 224px navigation, flexible
content, optional 440px detail drawer. At narrow widths the drawer becomes a
full page, navigation collapses and controls remain keyboard reachable. These
are initial design tokens, not measured performance claims.

Default work list favors compact rows; board is a selectable projection. Work
rows show title, owner, state, last accepted result, blocking reason and cost
availability. No decorative metric cards that displace actionable work.
Use explicit unknown/stale labels; never show unknown usage as $0 or stale CI as
fresh green. Dates and durations are readable with exact values available.

Detail priority: desired outcome -> latest accepted evidence -> blocker/decision
-> next action -> execution history. Decision cards include question, options,
recommendation when available, consequences, exact scope and evidence links.
The recommendation is advisory and visually separate from authority.

Command palette: navigate/create/search applicable actions using Ctrl/Cmd+K.
Support focus return, Escape, visible focus, keyboard board movement, form error
association and reduced motion. Use Radix focus/overlay primitives instead of
porting handwritten focus traps. Do not use color alone for state.

Chat displays intent and delivery receipt for commands. Drafts are keyed by
scope; background updates preserve draft, scroll and selection. Read-only
questions do not steer a worker. Natural-language command proposals show the
interpreted scope; already authorized safe actions may proceed with a receipt.
Consequential new authorization uses the same Decision UI as other entrypoints.

## Acceptance and cutover

Preserve all current project/resource/connection/board/chat/control flows,
including external provider ownership and optimistic lock conflicts. Capture
behavior before deleting the old UI. Test duplicate submissions, stale response
arrival after navigation, network loss/reconnect, expired login, API 404,
refresh with an open dialog, history pagination, drafts and keyboard operations.

Goal pages may be integrated after 0006 provides their APIs; frontend completion
does not require fabricating those APIs. Existing functionality must work without
goal rollout, and new pages must use real responses when available. No fake
production data. Owner reviews rendered list/board/detail/chat/attention on actual
work for clarity and polish; model self-rating does not complete visual acceptance.

References: [Vite backend integration](https://vite.dev/guide/backend-integration),
[TanStack Query](https://tanstack.com/query/latest/docs/framework/react/overview),
[Router type safety](https://tanstack.com/router/latest/docs/guide/type-safety),
[Effect v3 docs](https://effect.website/docs/),
[Radix](https://www.radix-ui.com/primitives),
[Linear](https://linear.app/features),
[Attio views](https://attio.com/help/reference/managing-your-data/views/create-and-manage-table-views).
These inform independently authored UI; do not copy product assets or UI code.
