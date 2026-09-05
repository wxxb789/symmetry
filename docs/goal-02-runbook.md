# Goal 2 Portal Runbook

The Symmetry portal is the operator-facing engineering workspace built on top
of the durable orchestration plane from Goal 1. It is available at `/portal`
and uses the configured operator token to establish a signed browser session.

## Start the workspace

Run the migrated control plane, then open `http://127.0.0.1:4000/portal`.

```sh
cd control
mix setup
mix phx.server
```

For the default development configuration, sign in with
`development-operator-token`. Production uses `SYMMETRY_OPERATOR_TOKEN`; the
token is submitted once to the control plane and is not stored in browser
storage. The resulting session is carried by the signed, HTTP-only Phoenix
session cookie.

The trusted-local Docker topology also exposes the portal at
`http://127.0.0.1:4000/portal` after `docker compose up --build` completes.

## Engineering workspace model

A project owns its work-management state and may attach any number of resources
independently:

- repositories;
- work-tracking systems;
- CI systems;
- agent profiles;
- runtimes;
- provider connections.

Each resource reports `healthy`, `degraded`, `offline`, or `unknown`, with
connection state and synchronization state maintained separately. A project
also records the default agent profile and machine-local workspace binding used
when an agent-owned work item is started.

Work items use the portal workflow `backlog -> ready -> in_progress -> review ->
done`. Drag a card between columns or change its state in the detail drawer.
Priority, human or agent ownership, blockers, repository and branch context,
pull request, CI, and review state stay on the work item rather than being
encoded into low-level run events.

## Agent execution

Selecting agent ownership records the execution intent but does not launch it.
`Start run` creates one idempotent Goal 1 orchestration task for the work item
and moves the card to `in_progress`. Project defaults are used when the item
does not override `agent_profile` or `workspace`. The scheduler and daemon then
use the existing durable assignment, claim, lease, fencing, event, transition,
and command protocol without a separate portal execution path.

Execution-defining fields remain editable before launch, are locked while a
task is queued or active, and become editable again after failure or
cancellation. `Retry` refreshes the same durable task from the corrected work
item and queues its next generation; a completed task is not silently rerun.

The linked orchestration task may create multiple run generations during normal
recovery. The work item keeps one stable task association, so those generations
remain one execution history in the portal.

The detail drawer presents the normal operating view in this order:

1. current phase, owner, blocker, repository, branch, pull request, CI, and
   review state;
2. outcome summary, findings, changed artifacts, tests, and meaningful activity;
3. raw task data and the newest execution-history page in a collapsed
   disclosure, with older records loaded explicitly through `Load older`.

When a run is `waiting_for_input`, the drawer exposes the current question and
sends a durable `provide_input` command. An active run can be cancelled through
the same durable command path.

## Health interpretation

The right rail deliberately separates four signals:

- **Connections** derives from attached resource health.
- **Runtimes** is healthy only when at least one registered runtime is online.
- **Executions** distinguishes active work, human input waits, and execution
  faults.
- **Sync** highlights degraded or offline external resources.

Cards separately expose blocked work, agent activity, pull-request presence,
CI failure or success, and outstanding review. This keeps an offline runtime or
broken connection from looking like an agent that is merely working.

## Browser API

All portal mutation endpoints require the signed operator session and Phoenix
CSRF token.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/portal/api/workspace` | Projects, Kanban work, resources, runtimes, and health |
| `POST` | `/portal/api/projects` | Create a project |
| `PATCH` | `/portal/api/projects/:id` | Edit, archive, or restore a project with version checking |
| `POST` | `/portal/api/projects/:id/resources` | Attach an engineering resource |
| `PATCH` | `/portal/api/resources/:id` | Update connection or synchronization health |
| `DELETE` | `/portal/api/resources/:id` | Detach an unreferenced resource |
| `POST` | `/portal/api/projects/:id/work-items` | Create a work item |
| `GET` | `/portal/api/work-items/:id` | Read outcome-first detail and raw execution data |
| `PATCH` | `/portal/api/work-items/:id` | Update workflow, ownership, blockers, and delivery state |
| `PATCH` | `/portal/api/work-items/:id/move` | Move and order a work item with stale-write protection |
| `POST` | `/portal/api/work-items/:id/run` | Create or replay the linked orchestration task |
| `POST` | `/portal/api/work-items/:id/cancel` | Request durable cancellation |
| `POST` | `/portal/api/work-items/:id/retry` | Queue a later generation on a failed or cancelled task |
| `POST` | `/portal/api/work-items/:id/input` | Provide consequential human input |
| `GET` | `/portal/api/work-items/:id/timeline` | Load an older raw-history page |

The machine and external operator APIs under `/api/v1` are unchanged.

## Verification

Run the control-plane gates after applying the Goal 2 migration:

```sh
cd control
mix format --check-formatted
mix compile --warnings-as-errors
mix test
```

For browser verification, start `mix phx.server`, sign in, and verify the portal
at desktop and narrow mobile widths. Run the repeatable browser matrix from
`browser/`:

```sh
npm ci
npm test
```

Then run the real daemon lifecycle matrix from the repository root while the
control plane is still running:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-live-daemon-tests.ps1
```

When validating `compose.yaml`, also run the bundled container daemon path from
`browser/`:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-compose-daemon-tests.ps1
```

The feature-level end-to-end path is:

1. create a project and attach repository and CI resources;
2. create and prioritize a work item;
3. assign it to an agent, select `Start run`, and observe queued/running state;
4. inspect meaningful activity while raw execution data stays collapsed;
5. provide input, cancel, or retry when applicable;
6. load older raw records without flooding the normal detail view;
7. inspect PR/CI/review evidence and move the item through review to done.

Goal 1 daemon and protocol gates remain authoritative for dispatch, fencing,
recovery, and execution isolation. See [`goal-01-runbook.md`](goal-01-runbook.md)
for those commands.

## Feature verification matrix

Every row below requires a behavior assertion and persisted-state evidence;
service startup or a successful page load is not sufficient.

| Acceptance | Automated evidence |
| --- | --- |
| AE1 | Playwright `project, resource, work-item, filtering, ordering, and persistence`; controller empty-workspace and project API tests |
| AE2 | The same Playwright workflow plus `workspace projection joins compatible runtime, current execution evidence, and distinct health`; registered agent/runtime identities are validated without changing scheduling compatibility |
| AE3 | Playwright reload ordering plus `Kanban moves persist exact order within and across columns` |
| AE4 | Playwright moves a human-owned item through review to done and asserts `execution: null` |
| AE5 | Live daemon Playwright attaches registered agent/runtime identities, reloads them, rejects a forged identity visibly, and asserts one linked task, Activity runtime identity, and current generation |
| AE6 | `input actions are scoped to the current waiting request and replay after response loss` plus the live waiting/input workflow |
| AE7 | Live daemon cancellation asserts the durable command and terminal cancelled transition, then Activity offers Retry |
| AE8 | Live daemon failure/Retry asserts the same task ID, generation 2, and both generations in raw history |
| AE9 | Live semantic evidence assertions plus `raw execution history loads older daemon records on demand`; expanded/paginated raw history survives polling without hiding a newly available cursor |
| AE10 | Two-context Playwright work-item edit proves the stale save reloads the winning value |
| AE11 | Desktop/mobile Playwright operates navigation, dialogs, cards, and the drawer by keyboard, traps drawer focus, restores Board/Activity/attention triggers, renders elapsed timing, and checks page overflow |
| AE12 | Compose Playwright `the production Compose stack completes F1-F7 and persists each outcome`, followed by `the control restart preserves the complete F1-F7 workspace`, an unchanged database fingerprint, and a fresh authenticated browser read |
| AE13 | `active execution freezes intent fields and prevents project archive` plus live failed-edit-Retry with corrected intent |
| AE14 | Controller replay tests for cancel and input action IDs, including stale generation/wait rejection |
| AE15 | `retry hides prior-generation delivery evidence from the current projection` and queued-retry outcome isolation |
| AE16 | Domain archive-vs-launch locking tests plus Playwright archive/read-only/restore/reload |
| AE17 | Two-context stale project/work-item tests plus delayed detail/workspace ownership and background-refresh preservation of input, caret, focus, raw disclosure, and loaded history |

## Post-Deploy Monitoring & Validation

- Watch Phoenix request logs for `/portal`, `/portal/api/workspace`, work-item
  mutations, `401`, `403`, `409`, `422`, and `5xx` responses.
- Watch PostgreSQL connection errors, query latency, and scheduler/reconciler
  failures alongside the existing runtime heartbeat and lease signals.
- Healthy signals are successful sign-in, `200` workspace reads, stable runtime
  status, one orchestration task per launched work item, and expected CI/review
  badges after updates.
- Failure signals are repeated session rejection, portal API `5xx` responses,
  stale cards after successful mutations, duplicate linked tasks, or rising
  database latency during the five-second workspace refresh cadence.
- The release operator owns validation for the first 30 minutes after deploy.
  Roll back the application release on repeated `5xx` or broken task launch.
  Goal 2 migrations extend the Goal 1 `tasks` and `commands` tables as well as
  adding project/work-item data. A schema rollback is allowed only when no
  durable retry command exists and the workspace data is disposable or has
  been exported. When retry history exists, roll back the application while
  retaining the forward-compatible schema; the retry migration fails closed
  rather than deleting audit history.
