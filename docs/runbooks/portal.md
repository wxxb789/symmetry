# Portal operations

Status: **operator runbook** for the engineering workspace. The workspace is one
authenticated React SPA document served at `/portal`; the 0007i cutover deleted
the classic EEx renderer, its `portal.js`/`portal.css` and the
`/portal/classic` route (now a real 404), and existing `/portal#...` hash links
are repaired once by the SPA bootstrap to their new routes. The wire contract is
[protocol v1](../protocol-v1.md); the frontend contract is
[frontend.md](../design/frontend.md); browser suites and their fixtures are
documented in [`browser/README.md`](../../browser/README.md); gates are in
[quality.md](../design/quality.md).

## Build and start

The SPA must be built before Phoenix serves it. From the repository root:

```sh
mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile
mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build
```

The build stages hashed assets and `index.html` in
`control/priv/static/portal/`; `mix release` and the Docker build include those
files. Missing build output returns `503` rather than an alternate workspace.
For Vite development, start Phoenix first, then
`mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend dev` and open
`http://127.0.0.1:5173/portal/` (session and socket requests proxy to Phoenix).

Run the migrated control plane, then open `http://127.0.0.1:4000/portal`:

```sh
cd control
mix setup
mix phx.server
```

For the default development configuration, sign in with
`development-operator-token`. Production uses `SYMMETRY_OPERATOR_TOKEN`; the token
is submitted once to the control plane, compared in constant time, and is not
stored in browser storage. The resulting session is carried by the signed,
HTTP-only Phoenix session cookie, which holds only a session fingerprint.

The trusted-local Docker topology also exposes the portal at
`http://127.0.0.1:4000/portal` after `docker compose up --build` completes. See
[Control plane and daemon operations](control-plane-and-daemon.md) for the
Compose stack and credentials.

## Workspace model

A project owns its work-management state and may attach any number of resources
independently: repositories, work-tracking systems, CI systems, agent profiles,
runtimes and provider connections. Each resource reports `healthy`, `degraded`,
`offline` or `unknown`, with connection state and synchronization state
maintained separately. A project also records the default agent profile and
machine-local workspace binding used when an agent-owned work item is started.

Work items move through `backlog -> ready -> in_progress -> review -> done`: on
the board, **Shift + ←/→** moves the focused card between columns and
**Shift + ↑/↓** reorders it inside its column, and the work detail edits state
directly. A Goal-managed item has no writable path here — the board refuses its
move and the work detail records its current state only — and
`/portal/api/work-items/:id/move` answers `403 goal_authority_required`. Priority,
human or agent ownership, blockers, repository and branch context, pull request,
CI and review state stay on the work item rather than being encoded into low-level
run events.

The work detail is outcome-first: it leads with current state chips, the outcome,
the latest accepted evidence and any blocker or decision, then the next action,
execution history and assignment. Older raw execution records load explicitly
through **Load older history** instead of flooding the normal view.

### Agent execution

Selecting agent ownership records the execution intent but does not launch it.
**Start run** creates one idempotent orchestration task for the work item and
moves the card to `in_progress`; project defaults are used when the item does not
override `agent_profile` or `workspace`. The scheduler and daemon then use the
existing durable assignment, claim, lease, fencing, event, transition and command
protocol without a separate portal execution path.

**Start run**, **Cancel run** and **Retry** belong to a legacy work item. A
Goal-managed item renders a **Goal-managed work** panel instead — its run, retry
and move paths go through Goal admission — and the `run`, `cancel` and `retry`
endpoints answer `403 goal_authority_required`.

Execution-defining fields remain editable before launch, are locked while a task
is queued or active, and become editable again after failure or cancellation. The
server refuses an edit that would change locked intent fields.
**Retry** refreshes the same durable task from the corrected work item and queues
its next generation; a completed task is not silently rerun. The work item keeps
one stable task association, so generations remain one execution history in the
portal.

When a run is `waiting_for_input`, the detail view exposes the current question
and sends a durable `provide_input` command. An active run can be cancelled
through the same durable command path.

### Health signals

The workspace read serves four health signals for the selected project:
**connections** (derived from attached resource health), **runtimes** (healthy
only when at least one *compatible* runtime is online), **executions** (active
work, human input waits, execution faults) and **synchronization** (degraded or
offline external resources; failed or stale synchronization reports attention).
The classic health rail no longer renders in the SPA — read these signals from
the project, connections and runtimes surfaces or from the API. This keeps an
offline runtime or broken connection from looking like an agent that is merely
working.

## Portal HTTP API

All portal routes require the signed operator session. Mutating requests also
carry the Phoenix CSRF token; Plug exempts `HEAD`, `GET` and `OPTIONS` from token
verification, so the reads below need the session alone. The work-item write
entries (`run`, `cancel`, `retry`, `move`) are the legacy path: a Goal-managed item
is refused with `403 goal_authority_required` and is driven through the Goal
surface below. Project and work-item endpoints:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/portal/api/workspace` | Projects, Kanban work, resources, runtimes and health |
| `POST` | `/portal/api/projects` | Create a project |
| `PATCH` | `/portal/api/projects/:id` | Edit, archive or restore a project with version checking |
| `POST` | `/portal/api/projects/:id/resources` | Attach an engineering resource |
| `PATCH` | `/portal/api/resources/:id` | Update connection or synchronization health |
| `DELETE` | `/portal/api/resources/:id` | Detach an unreferenced resource |
| `POST` | `/portal/api/projects/:id/work-items` | Create a work item |
| `GET` | `/portal/api/work-items/:id` | Read outcome-first detail and raw execution data |
| `PATCH` | `/portal/api/work-items/:id` | Update workflow, ownership, blockers and delivery state |
| `PATCH` | `/portal/api/work-items/:id/move` | Move and order a work item with stale-write protection |
| `POST` | `/portal/api/work-items/:id/run` | Create or replay the linked orchestration task |
| `POST` | `/portal/api/work-items/:id/cancel` | Request durable cancellation |
| `POST` | `/portal/api/work-items/:id/retry` | Queue a later generation on a failed or cancelled task |
| `POST` | `/portal/api/work-items/:id/input` | Provide consequential human input |
| `GET` | `/portal/api/work-items/:id/timeline` | Load an older raw-history page |

The same scope also exposes the session read, the connection surface
(`/portal/api/connections`, `.../:id/check`, `/portal/api/connections/:id`),
resource and project synchronization (`/portal/api/resources/:id/sync`,
`/portal/api/projects/:id/sync`), the attention read, the goal surface
(`/portal/api/projects/:id/goals`, `/portal/api/goals/:id` and its
events/graph/context/command routes) and chat (`/portal/api/chat`,
`/portal/api/chat/messages`). `control/lib/symmetry_control_web/router.ex` is the
authority for the full list. The machine and external operator APIs under
`/api/v1` are unchanged.

## Post-deploy signals

- Watch Phoenix request logs for `/portal`, `/portal/api/workspace`, work-item
  mutations, and `401`, `403`, `409`, `422` and `5xx` responses, alongside
  PostgreSQL connection errors, query latency, and scheduler/reconciler failures
  next to the existing runtime heartbeat and lease signals.
- Healthy signals are successful sign-in, `200` workspace reads, stable runtime
  status, one orchestration task per launched work item, and expected CI/review
  badges after updates.
- Failure signals are repeated session rejection, portal API `5xx` responses,
  stale cards after successful mutations, duplicate linked tasks, or rising
  database latency. Connected documents refresh through the operator invalidation
  channel; a disconnected surface falls back to a five-second poll while the
  document is visible.
- The release operator owns validation for the first 30 minutes after deploy.
  Roll back the application release on repeated `5xx` or a broken task launch.
  Portal migrations extend the orchestration `tasks` and `commands` tables and add
  project/work-item data. A schema rollback is allowed only when no durable retry
  command exists and the workspace data is disposable or has been exported; the
  retry migration fails closed rather than deleting audit history, so with retry
  history present roll back the application and keep the forward-compatible
  schema.

## Verification

Run the control-plane gates listed in
[Control plane and daemon operations](control-plane-and-daemon.md#verification)
and *Commands and gates* in [quality.md](../design/quality.md), then the browser
suites. Browser setup, the suite inventory and each fixture harness are in
[`browser/README.md`](../../browser/README.md); the canonical commands are:

```sh
mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile
mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance exec playwright install chromium
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test
```

The suite runs desktop and mobile Chromium projects against
`http://127.0.0.1:4000` with the development operator token; failure screenshots,
videos and traces are written under the system temporary directory unless
`SYMMETRY_PLAYWRIGHT_OUTPUT` overrides the directory, and the HTML report is
written under `playwright-report/`. Then run the real daemon
lifecycle matrix from the repository root while the control plane is still
running:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-live-daemon-tests.ps1
```

For the daemon bundled by the trusted-local Compose stack, including a control
restart with a database fingerprint and browser readback:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-compose-daemon-tests.ps1
```

The feature-level end-to-end path is: create a project and attach repository and
CI resources; create and prioritize a work item; assign it to an agent, select
**Start run**, and observe queued/running state; inspect outcome-first detail
while raw execution data stays behind the history read; provide input, cancel or
retry when applicable; load older raw records without flooding the detail view;
and inspect PR/CI/review evidence and move the item through review to done.
Dispatch, fencing, recovery and execution isolation remain governed by
[protocol v1](../protocol-v1.md) and covered by the daemon checks in
[Control plane and daemon operations](control-plane-and-daemon.md#live-checks-opt-in).
