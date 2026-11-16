# 0007e resource binding and truthful synchronization — implementation plan

Goal contract: [docs/goals/0007e-resource-binding-and-sync.md](../goals/0007e-resource-binding-and-sync.md).
Base revision: `e6e4b03b35f2b1b1c884594c09adce20a75c4df8` (branch `feat/0007e-resource-binding-and-sync`).
Shape reference: 0007d (`e6e4b03`, connection management on the fixtures-only boundary) — one real-API
surface on `/portal/connections`, one controlled provider fixture whose calls are recorded and canary
scanned, one harness that owns its own process tree and fails a skipped run, one receipt that pairs
every Done-when clause with the check that demonstrated it.

This goal needs **no new wire surface**. Every operation it demands already exists on the real API
(§1); the missing part is the operator surface, the controlled provider documents for
resource/sync flows, and the evidence. Provider transport stays fixture-only: no provider host is
contacted, and provider-side resource mutation stays out of scope.

## 1. What already exists (measured at the base revision)

| Layer | State at `e6e4b03` |
| --- | --- |
| Resource wire | `POST /portal/api/projects/:project_id/resources`, `PATCH /portal/api/resources/:resource_id`, `DELETE /portal/api/resources/:resource_id?version=`, `POST /portal/api/resources/:resource_id/sync`, `POST /portal/api/projects/:project_id/sync`, `PATCH /portal/api/work-items/:work_item_id` (`router.ex:64-84`). |
| Resource policy | `Workspaces.{create,update,delete}_resource/2,3` (+ `ensure_active_project`, `validate_registered_reference` for `agent`/`runtime`, `validate_connected_reference` for external kinds, `ensure_resource_unreferenced` on identity change and delete, optimistic lock via `lock_version`); `Integrations.validate_resource_reference/3` delegates to the provider module (`github.ex:16-26`, `azure_devops.ex:24-43`); `ensure_connection_capability/2` refuses a connection that lacks the kind's capability (`workspaces.ex:915-929`). |
| Sync semantics | `Integrations.sync_resource/1` → `sync_connected_resource/1`: capability check, provider authenticate, `provider.sync_resource/3`, `delivery_updates/4`, `persist_sync/4`. Success writes `healthy`/`synced` + `last_synced_at`; a provider failure writes `degraded`/`failed` + the fixed message from `failure/1` and leaves `last_synced_at` untouched (`integrations.ex:663-688`, `1568-1620`). `sync_project/1` runs every connected resource of an active project and aggregates per-resource failures. |
| External work items | `persist_sync_work_items/4` upserts provider items by exact `external_id`, `mark_missing_work_items_unavailable/2` sets `external_available: false` for items absent from the provider answer, `provider_work_item_attrs/3` always sets `external_available: true` for items present (`integrations.ex:1197-1286`, `1506-1542`). |
| Provider-owned lock | `WorkItem.provider_owned_field?/1` = `[:title, :description, :priority]`; `provider_owned_fields_editable/2` refuses any change touching them with `:provider_owned` → `409 provider_owned` (`workspaces.ex:900-913`, `schemas.ex:291`, `protocol.ex:21`). |
| Execution entry | `PortalJSON.work_item/2` computes `can_start` only for a legacy, agent-assigned, backlog/ready item of an active project whose external item is available (`WorkItem.external_work_available?/1`); `retry_capability/2` also requires availability (`portal_json.ex:120-140`, `324-330`). |
| Reads | `GET /portal/api/workspace?project_id=` already serves the project directory, the selected project's `resources` and `work_items`, the connections with their bound resources, and `registered_runtimes` (`PortalJSON.workspace/1`). `GET /portal/api/connections` serves the provider catalog + connections with capabilities and bound resources. |
| New UI | `/portal/connections` renders the connection list/detail only (`frontend/src/features/connections/connections.tsx`); `/portal/work/:workItemId` renders a read-only detail (`features/work/detail.tsx`); there is no resource binding, sync or assignment control on any new route and no project scope on `/connections`. |
| Classic surface | `/portal/classic` keeps the classic resource dialog, project sync, per-resource sync and the work-item dialog (`control/priv/portal_assets/portal.js`, `portal_html/index.html.eex`). `browser/tests/portal-classic-resources.spec.mjs` is the only browser coverage of these flows (4 mocked tests, 8 runs). |
| Fixture | `scripts/ci/portal-connection-fixture.exs` answers only GET `api.github.com/user`, `/user/memberships/orgs/:account`, `/repos/:owner/:repo`, and any GET `dev.azure.com/<org>/…` with one projects document; it records every call and carries two canary tokens. |

## 2. Done-when → deliverable map

| Contract clause | Deliverable | Evidence |
| --- | --- | --- |
| bind / update / unbind / sync repository, work-tracking and CI resources from both provider kinds, real API + browser, boundary stated per flow | Project-resources section on `/portal/connections` (§4); fixture documents for every provider read these flows use (§5) | new-route spec tests 6–9 over the real API + fixture; ExUnit `portal_resources_test.exs`; receipt boundary table |
| provider failure and unknown outcomes stay explicit, never synchronized | failure and unknown rendering on the resource row + dialog (§4.3); sync reuse of the existing failure semantics (§3) | spec test 7 (injected HTTP 500 and an ambiguous transport failure); ExUnit failure/unknown tests; API+SQL readback |
| an unknown outcome resolved by readback shows the resolved truth | the sync endpoint **is** the provider readback: after an ambiguous outcome the row states the failure, and the next readback writes and renders exactly what the provider reported (metadata/url/state at readback time), never a carry-over assumption | spec test 7 second half: after the failure the fixture answers the changed repository document; the row shows the readback values and a new `last_synced_at`, the API/DB readback shows the same values |
| project switching and concurrent refresh never let a late response overwrite the newly selected project's state | project scope in the URL (`?projectId=`), resources read per project query key, continuations gated on the live project scope (§4.4) | spec test 8 (concurrent sync + late failure across a switch, two contexts); ExUnit project-sync concurrency; frontend component tests |
| synchronized external work items keep their exact external identity, provider-owned fields stay locked, an externally unavailable item renders unavailable with no execution entry point | external identity/provider facts on the work detail already exist; the new assignment editor adds no provider-owned control; availability is served, not guessed | spec test 9 (identity, `provider_owned` refusal, unavailability after a readback that omits the item, no run/retry control) + ExUnit |
| work assignment offers only the selected authorized resources | assignment editor on the work detail, options = the item's project's resources of the matching kind (§4.5) | spec test 9 option sets + a direct API cross-project refusal; frontend component tests; ExUnit `validate_work_item_resources` |
| pre-existing coverage passes on the new route, classic checks retired in the same PR | the four classic resource tests are retired; their intent is mapped to the new route (§6) | executed suites with counts/exit codes; the removed-spec list in the receipt |
| unverified reported | receipt limitations | receipt |

## 3. Server semantics the surface must mirror (no server change)

No endpoint, schema or policy is added or weakened. The surface mirrors these exact rules:

1. **Connection eligibility**: a connection is offered for a kind only when its `capabilities`
   include that kind's capability (`repository→repositories`, `work_tracking→work_items`, `ci→ci`,
   exactly `ensure_connection_capability/2`). The server re-checks; a client-side miss is a display
   bug, not authority.
2. **Exact external identity**: `external_ref` is typed by the operator and validated by the provider
   module — GitHub `owner/repository` (owner must equal the connection's `account_ref`), Azure DevOps
   `project/repository`, `project` (work tracking) and `project/pipeline/<definitionId>` (CI). Local
   kinds `agent`/`runtime` take a reference from the workspace's `registered_runtimes`.
3. **Reset on identity change**: changing connection, kind or reference resets the resource to
   `unknown/unknown` with no timestamps (`prepare_resource_attrs/2`); the UI re-reads instead of
   predicting the reset.
4. **Version fences**: `PATCH` and `DELETE` submit the `version` the operator saw; `409 stale`,
   `409 state_conflict` (referenced by a work item/runtime), `404 not_found` are rendered with the
   server's code and its current state is re-read.
5. **Sync outcome**: `200 {resource}` = provider read succeeded (status `healthy`, `sync_status`
   `synced`, `last_synced_at` set); `502 {resource, error}` = the provider read failed
   (`degraded`/`failed`, `last_synced_at` unchanged); `409` = capability/state refusal
   (`capability_not_granted`, `not_connected`, `state_conflict`). Project sync answers
   `200 {synced_resource_ids}` or `4xx/5xx {error, results[]}` with one entry per failed resource.
6. **Unknown outcome**: an ambiguous provider outcome (transport failure: the request left, no answer)
   is recorded as an explicit failure, never as a success: `sync_status: failed` with the fixed
   message (`"Provider transport failed"`). The readback is the same sync read: only what the provider
   answers at readback time is written, including a state that contradicts the pre-failure assumption.
7. **Assignment rules**: `repository_resource_id`/`ci_resource_id` must belong to the work item's
   project and match the kind (`validate_work_item_resources/2` → `validation_failed`); execution
   fields are refused while a run holds the item; provider-owned fields are refused with
   `provider_owned`.
8. **Availability**: `external.available === false` ⇒ `can_start: false`, retry capability false, and
   the surface renders unavailable and offers no execution control anywhere (this surface has none).

## 4. Frontend design

### 4.1 Placement and reads

`/portal/connections` gains a **Project resources** section beside the existing connection list and
detail. Reads stay the two the page/shell already use, each with one job:

- `workspaceOptions(api, projectId)` — the project directory, the selected project's resources and
  work items, and `registered_runtimes`. The shell's project picker already issues exactly this query
  (same key `["workspace", {projectId}]`), so the page adds no request.
- `connectionsOptions(api)` — connection list, capabilities and provider catalog.

Selected project = `search.projectId ?? workspace.selected_project_id` (the Attention precedent), and
the section states which project it is showing. An absent or unknown `projectId` renders the
Attention-style "Project not found" empty state instead of another project's rows.

### 4.2 Frozen control contract (labels the browser spec binds to)

| Control | Accessible name / label | Behavior |
| --- | --- | --- |
| Section | heading `Project resources` | count of the selected project's resources |
| Bind | button `Bind resource` | opens the bind dialog; disabled while a mutation is pending |
| Project sync | button `Synchronize project` | `POST /projects/:id/sync`; disabled for a pending mutation or an archived project |
| Row | list `Project resources` | name, kind, connection (or `Local resource`), exact reference, health badge, sync badge, `Last synced …`/`Never synced`, status detail |
| Row sync | button `Sync {name}` | `POST /resources/:id/sync` |
| Row update | button `Update {name}` | opens the update dialog |
| Row unbind | button `Unbind {name}` | opens the unbind confirmation |
| Bind dialog | title `Bind resource`; fields `Type`, `Provider connection`, `Name`, `Reference`; submit `Bind` | external kinds require a connection; local kinds take the reference from the registered list |
| Update dialog | title `Update resource`; read-only facts `Type`, `Provider`, `Connection`, `Health`, `Sync state`, `Last synced`, `Status detail`; editable `Name`, `Provider connection`, `Reference`; submit `Save` | submit carries the version the dialog opened with |
| Unbind dialog | title `Unbind {name}?`; buttons `Unbind resource`, `Cancel` | submit carries the version the dialog opened with |
| Work assignment | section heading `Assignment`; button `Edit assignment`; dialog title `Assignment` with `Owner type`, `Owner`, `Agent profile`, `Workspace`, `Repository`, `CI resource`; submit `Save assignment` | options come from the item's own project only |

Refusals render the server's message, its code in parentheses and the operator's next step, reusing
the 0007d `refusalNotice`/`refusalGuidance` helpers (extended with the resource codes
`not_connected`, `provider_owned`, `stale`, `state_conflict`, `capability_not_granted`).

### 4.3 Truthfulness rules on the resource surface

- A row is "synchronized" only from the served projection (`sync_status === "synced"` and a
  `last_synced_at`); a refused sync never mutates the row locally, it re-reads.
- A failed/unknown sync shows the sync badge from the server plus the server's `status_message`, and
  the `Last synced` value stays whatever the server holds (proof that freshness is not assumed).
- The unfiltered "Never synced" case (`last_synced_at === null`) stays distinct from "Failed".
- An archived project disables every mutation control; the note says why.
- A failed re-read of the workspace read — the reconciliation a settled write triggers or a manual
  refresh — renders the failed-refresh error state with the last loaded rows still shown and marked
  as such; a stale read is never presented as the current one.

### 4.4 Concurrency and project switching

- The resources rendered come from the query keyed by the live project id; a late response for the
  previous project cannot write into it (React Query cannot resolve a different key's data into the
  current one).
- Every continuation (sync receipt, mutation receipt, failure notice, refresh) is gated on the live
  project scope read through a ref (`projectRef`), the same pattern 0007d used for connection
  selection (`selectionRef`/`noticeFor`), so a switch discards it instead of acting on the new
  project.
- Single-flight: one page-level pending counter for resource mutations + pending counters keyed by
  subject (`resource:{id}`, `project:{id}`) so a check/sync/unbind keeps its exclusion across a
  re-read or a remount, and the project sync locks the rows while it runs (mirrors the classic
  single-flight contract).
- Editor generations: each dialog open gets an id; a late response only closes/navigates the dialog
  the operator still has open, otherwise it only invalidates the read.

### 4.5 Work assignment editor

`features/work/assignment.tsx`, rendered from the work detail (both the desktop drawer and the narrow
page). It reads the item's project resource list from the same `workspaceOptions(api, item.project_id)`
query the project surfaces use, and offers:

- `Repository` — options: `Not linked` plus the item's project resources with `kind === "repository"`.
- `CI resource` — options: `Use repository provider` plus the item's project resources with
  `kind === "ci"`.
- `Owner type`, `Owner`, `Agent profile`, `Workspace` — as in the classic dialog; profile/workspace
  default to the project's values server-side when empty.
- Option labels carry the exact reference and the connection/provider state so the operator selects an
  identity, not a guess; each option value is the resource id.

Rules: disabled while the served execution is `intent_locked`; no title/description/priority/status
control exists on this surface (provider-owned fields stay locked); a refusal is rendered with the
server's code (`provider_owned`, `stale`, `state_conflict`, `validation_failed`); a successful save
re-reads the detail and the project read, and a continuation checks the still-selected work item id
before touching the draft or the notice.

## 5. Fixture and harness extension (controlled provider documents)

`scripts/ci/portal-connection-fixture.exs` keeps every existing property (identity handshake, canary
tokens, call log with no token value, `--no-start` boot order, real supervision topology, strict
request acceptance) and gains:

1. **GitHub documents**: `/repos/:owner/:repository/issues?...` (issues list, `pull_request` entries
   filtered like the real API), `/repos/:owner/:repository/actions/runs?...` (one workflow run),
   `/repos/:owner/:repository/pulls/:number` (one pull request), `/repos/:owner/:repository/pulls?...`
   (list, for CI/review reads by `sync_delivery`/`sync_ci`). All are GETs answered only for the
   fixture bearer.
2. **Azure DevOps documents** per path: `/_apis/git/repositories/{repo}`,
   `/_apis/wit/wiql` and `/_apis/wit/workitemsbatch` (POST, with `System.*` fields and a positive
   `rev`), `/_apis/build/definitions/{id}`, `/_apis/build/builds`. The `dev.azure.com` clause stops
   answering every path with the projects document; unknown paths under the accepted hosts are
   refused as `fixture_no_route` again.
3. **Steering file**: `SYMMETRY_FIXTURE_STATE_FILE` (JSON, optional, default empty) read on every
   request:
   - `fail`: references whose provider read answers HTTP 500 (definitive provider failure);
   - `transport_fail`: references whose provider read returns `{:error, {:transport, …}}` — the
     request left, the answer never arrived (unknown outcome);
   - `issues`: per GitHub repository, the issue numbers to answer (an empty list makes a previously
     imported item absent ⇒ the readback marks it unavailable);
   - `repository`: optional overrides for the repository document (`archived`, `html_url`,
     `default_branch`, `visibility`) so a readback can show a state that contradicts the pre-failure
     assumption.
   The file path is passed by the harness/spec only; the browser cannot address the fixture directly.
4. **Rejected-call visibility**: a steered failure is still a *recorded* call (`status` 500 or
   `rejected` + reason), so the receipt can show which provider read failed.

`browser/scripts/run-connection-fixture-tests.ps1`: pass `SYMMETRY_FIXTURE_STATE_FILE`, keep the
canary scan and the owned-process cleanup, and read the expected pass count from the spec-file shape
instead of the hard-coded `10` (it grows with §6's tests); a wrong count still fails the run.

`browser/scripts/connection-spec-env.test.mjs` asserts the collection shape (`Total: N tests in 1
file`) and must be updated with the same count.

## 6. Browser acceptance on the new route and classic retirement

`browser/tests/portal-connections.spec.mjs` (real API + fixture transport, desktop + mobile projects)
keeps its five connection flows and adds:

| # | Flow | Boundary |
| --- | --- | --- |
| 6 | bind → inspect → update → unbind a GitHub repository resource through the console; resource row shows the served identity/state | real API, real DB; fixture repository document |
| 7 | sync a GitHub work-tracking resource (issues), then a repository resource: injected provider failure (500) and an ambiguous transport failure, then the readback that resolves the truth; the row never reads `Synced` while unresolved | real API, real DB, real failure mapping; fixture documents steered by the state file |
| 8 | project switching + concurrent operations: a sync and a project sync in flight across a switch, a late failure for the old project discarded, rows of the new project intact (two contexts) | real API, real DB; fixture documents |
| 9 | work assignment on the real API: external work item identity/provider-owned refusal/availability, assignment option sets limited to the item's project, save, and the cross-project refusal | real API, real DB; fixture issues document |
| 10 | Azure DevOps binding + sync for repository, work-tracking and CI references (`project/repository`, `project`, `project/pipeline/<id>`) with the reference normalization the classic test pinned | real API, real DB; fixture ADO documents |
| 11 | a real console write, then a refresh whose workspace re-read is aborted at the browser's network layer (a real fetch failure, no response substituted): the failed-refresh error state renders with the last loaded rows still shown, and the retry re-reads the real API and clears it | real API, real DB; one read aborted at the browser's network layer, every other request served by the real app |

Retired in the same PR: `browser/tests/portal-classic-resources.spec.mjs` in full (4 tests, 8 runs).
Its intent moves as follows — no assertion is dropped without either a replacement or an explicit,
recorded non-replacement (§6's ADO delivery row):

| Retired classic assertion (spec lines) | Replacement |
| --- | --- |
| binding create (`:52-76`, `:214-228`, `:826-849`, `:972-983`) and reference normalization (`:828`) | new spec tests 6 and 10 (console-driven create for both providers, all three kinds) |
| per-resource sync (`:78-127`, `:851-898`) and `#resource-table` "Synced" (`:231`, `:1011`) | tests 6, 7, 10 (row shows the served `synced` state; failure cases never do) |
| project sync (`:382-391`) and its failure mapping (`:488-491`) | test 8 (project sync + per-resource failure notice) and the ExUnit aggregate test |
| single-flight across rerenders/dialogs (`:404-466`) | frontend component tests (deferred gates) + test 8 for the observable mutual exclusion (buttons disabled while a sync/project sync is in flight) |
| late-response/dialog ownership (`:448-451`, `:733-734`) | frontend component tests (deferred gates) + test 8's discarded late failure |
| editor stale/draft/refresh rules (`:672-679`, `:692-712`) | frontend component tests + test 6's version-fenced update/unbind refusals |
| project switch/rollback/archived read-only (`:499-518`, `:736-738`, `:748-763`) | test 8 (switch + late response) and the archived-project controls test |
| external identity/provider attribution, GitHub arm (`:235-238`, `:988-993`) | test 9 (exact `external.id`/`url`/`available`) + test 7 (rendered `source: provider · github` on the pull-request, CI and review delivery rows) |
| ADO delivery rows (`:1016-1021`) | **not replaced** — no work item is bound in test 10 and the fixture serves no ADO pull/review documents; recorded as a limitation in the receipt §6 |
| provider-owned locking (`:241-243`, `:996-997`) | test 9 (the surface has no such control; the API refuses `title` with `provider_owned`) |
| unavailable item + no execution entry (`:270-271`, `:278-280`) | test 9 (rendered unavailable; the read-only detail's absent run/retry control plus the classic-surface check that `Start run` is shown while available and hidden after the readback omits the item; `can_start: false` in the readback) |
| assignment (`:129-149`, `:244-252`, `:900-916`) | test 9 (console-driven assignment with project-scoped option sets) |
| layout/no-overflow (`:286`) | every new test keeps the `capture()` overflow assertion |
| Azure resource kinds (`:784-1024`) | test 10 |

Retired assertions that depended on synthetic mocks only (held responses, injected workspace/JSON
failures, hand-written provider payloads) move to frontend component tests with deferred response
gates, exactly as 0007d did for the connection surface; `browser/README.md` is updated to state the
new split.

## 7. Real-API (ExUnit) tests

`control/test/symmetry_control_web/controllers/portal_resources_test.exs` (real endpoint + real
PostgreSQL + a steered provider module, the 0007d `AggregateProviderStub` pattern) covers what the
browser cannot: per-outcome provider documents, the aggregate project-sync failure shape, the
`provider_owned` refusal, cross-project assignment refusal, `state_conflict` on a referenced resource,
the readback write-through (metadata/url/`last_synced_at` after an unknown outcome), and the
unavailable-item projection (`can_start`/retry false). Each behavioural fix in this increment gets a
test that fails on the pre-fix revision.

## 8. Evidence plan

- Receipt: `docs/evidence/goal-0007e-resource-binding-and-sync-<date>.md`, following the
  0007b/0007d receipt pattern: subject digest (source-only path set), plan, §2-style
  Done-when→evidence table, executed checks with exact commands/counts/exit codes, the
  real-versus-fixture boundary per flow, retired-spec list, unverified limits.
- Red-before/green-after for every behavioural fix (fixture steering, UI truthfulness, assignment
  scoping, availability) with the failing output kept under `.tmp/evidence/`.
- One number per measurement, each naming its revision; single number source is the receipt.
- Unverified by construction and reported as such: live provider credentials/authorization, real
  provider-side state, hosted CI/qualification, pnpm supply-chain verification (registry
  unreachable), the environment-limited `CommandTest`/`pwsh` failure.

## 9. Gates (run by the integrator, never concurrently with each other)

`mix format --check-formatted`, `mix compile --warnings-as-errors`, `mix test` (control, dedicated
database), `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1` (the new-route
suite on both Playwright projects), `pnpm --offline --filter symmetry-browser-acceptance test --list`
(collection shape), `pnpm --offline --filter @symmetry/frontend {typecheck,lint,test,build}`,
`node --test browser/scripts/connection-spec-env.test.mjs`, `pnpm --offline contracts:check`,
`go vet ./...` and `go test ./...` (daemon; the fix round regenerates the contracts bundle, so these
run against it), the goals contract-fixture manifest test (the fix round's fixtures run through the
control validator), plus the work-detail spec file (`portal-work-views.spec.mjs`) for the surfaces
this increment touches.

## 10. Out of scope (with reasons)

- **Provider-side resource creation/mutation/deletion**: excluded by the contract (separate
  authorized outcome).
- **A provider catalogue or discovery API**: no such endpoint is invented; the connection list and
  the workspace's registered runtimes are the only sources of selectable values.
- **Runtime control (run/cancel/retry controls, 0007f)**: this surface renders no execution control;
  the availability clause is evidenced by the served projection (`external.available`, `can_start`)
  plus the classic surface's own execution control, shown while the item is available and hidden
  once the readback omits it (§6).
- **Full work-item planning editor (0007c)**: this increment adds the assignment editor only,
  because the contract's assignment clause names the editing surface and 0007c is not merged; no
  create/move/status/priority editing is added.
- **Classic workspace removal (0007i)**: classic keeps working; only this surface's classic *checks*
  are retired.
- **New wire schemas**: every operation is an existing endpoint. The fix round's contract edit only
  declares already-served workspace-read members as optional typed properties, with the two new
  fixtures and the regenerated DTO/Effect/Go artifacts; it adds no endpoint and changes no served
  answer. If implementation finds a clause that cannot be expressed with the existing endpoints, it
  stops and reports instead of inventing wire surface.

## 11. Implementation notes (recorded while executing this plan)

1. **Assignment surface is in scope because 0007c is not merged.** The contract names "the work
   detail and editing surfaces" as where assignment eligibility is observed, and its prerequisite
   lists the 0007c editing surfaces. At the base revision `feat/0007c-safe-work-planning` has no
   commits over `main` (verified with `git log main..feat/0007c-safe-work-planning`, empty), so no
   editing surface exists to carry the evidence. The repository requires this PR to be mergeable by
   itself and forbids depending on an unmerged sibling, so this increment adds the minimal
   assignment-only editor instead of the full 0007c planner. The alternative (leaving the clause
   unobserved) would fail "work assignment offers only the selected authorized resources → observed
   through the same tests on the work detail and editing surfaces"; the alternative of implementing
   all of 0007c would be a competing implementation of a sibling goal's contract.

2. **"Unknown outcome resolved by readback" is evidenced on the resource-sync path.** The
   provider-action intent/readback machinery (0006h) is reachable only through the daemon broker
   (`POST /api/v1/provider-actions` with fenced claims from a claimed run), which the
   fixtures-only browser boundary cannot drive without a daemon; the contract explicitly excludes
   runtime control. On this goal's surface the provider readback *is* the resource sync: a sync's
   provider read either returns the provider's state (written verbatim, including
   `last_synced_at`/`metadata`/`url`) or fails. The evidence therefore shows both directions of
   "never resolve by assumption": an ambiguous outcome (transport failure) records an explicit
   failure and leaves the previously synchronized state visibly unfresh, and the readback that
   follows writes and renders exactly what the provider answered (a repository document that
   contradicts the pre-failure state: `archived: true`, a changed `html_url`), verified by the API
   and by SQL against `project_resources`. The broker's own readback semantics are untouched and
   keep their existing coverage in `provider_action_controller_test.exs`.

3. **The increment adds no wire surface and changes no existing answer.** One candidate change was
   found and reverted: a connection-bound resource submitted with a blank `external_ref` is refused
   by the provider module *before* the changeset runs, so the server answers
   `400 invalid_request` rather than the changeset's `422 validation_failed` field envelope. The
   `ProjectResource` changeset declares the field error, but wiring it up would change the answer for
   an input this goal does not touch, so the test pins the pre-existing shape instead
   (`portal_resources_test.exs`, "…refuse stale versions, referenced identities and unknown ids").

4. **The project-resource surface steers from the workspace read.** The milestone review found the
   surface consuming workspace-read members no contract declared, so `workspace-read.schema.json`
   now declares every consumed member as an optional typed property: on `WorkspaceResource` the
   identity and binding members (`project_id`, `connection_id`, `kind`, `name`, `provider`,
   `external_ref`, `url`, `status_message`, `last_synced_at`, `version`), on `WorkspaceProject` the
   assignment defaults (`default_agent_profile`, `default_workspace`), and the root's
   `registered_runtimes` entries (`runtime_id`, `runtime_name`, `agent_profile`, `workspace`) that
   supply a local resource's references. All stay optional because a project row keeps accepting
   older partial projections, while a production server serves them all; a wrong type now fails the
   generated Effect decode instead of passing through `additionalProperties`. The surface still reads
   the served values through one local, defensive reader in `resources.tsx`/`assignment.tsx` that
   degrades on a missing member rather than inventing one. The alternative (reading the fully typed
   `connections-read` projection) stays rejected, but its reason is narrower: those rows can be
   filtered by their served `project_id`/`project_key`/`project_status`, and the rejection is only
   that they exist for connection-bound rows — a local resource (`connection_id == null`, the
   `agent`/`runtime` kinds) is served by the workspace read alone and by no connection.

5. **The fixture gained one primitive beyond the plan's §5 list: `hold`.** Proving "a sync in flight
   across a project switch" against the real API needs a provider read that stays unanswered while
   the browser switches projects; the alternative was browser-level response interception, which the
   real-API boundary forbids. A held read blocks in the fixture transport (re-reading the steering
   file every 100 ms, at most 300 polls = 30 s), is logged like every other visit (`steered: "hold"`
   / `"hold_timeout"`), and releases into whatever state the spec wrote — one rewrite can release a
   hold directly into `transport_fail`. Nothing else in the fixture contract changed, and the
   harness's canary scan, identity handshake and owned-process cleanup are untouched.

6. **Connection options in the bind/update dialogs are labelled by connection name** (not
   `name · provider`), because the row already names the provider and the accessibility name stays
   stable for the acceptance spec. The capability filter (kind → capability) is the server's rule
   (`ensure_connection_capability/2`) restated once in `resources.tsx` with a comment naming it.

7. **`connection`-kind project resources** (a pre-existing local kind the classic resource dialog
   offers) are rendered by the new surface when a project has one, but are not creatable from it:
   they carry no provider state to synchronize and no registered reference to select, and no clause
   of this goal names them. Classic keeps offering them until 0007i.

8. **The assignment section renders last on the work detail.** The 0007b acceptance
   (`browser/tests/portal-work-views.spec.mjs`) pins the first five `.detail-section` headings of
   the detail, so the new section is placed after the execution-history section instead of next to
   the project facts; the first legacy run caught the shifted order on both widths (§8 #7 of the
   receipt). §2's "project resources" section of this plan settles the binding surface, not the
   detail section order, so no planned behavior changed.

9. **The independent milestone review of this PR (head `38d19ce`) produced a fix round, and four
   plan-visible statements changed with it.** The consumed workspace-read members are now declared
   optional typed in the schema and the `connections-read` rejection reason is corrected (§11.4);
   the resource surface renders the failed-refresh error state with the last loaded rows when a
   re-read fails (§4.3); the browser acceptance adds flow 11 (the failed re-read after a write), the
   classic-surface execution check in flow 9, and the CI delivery row's provider attribution in
   flow 7; and the credential scan's aborted-read handling is a stated, `GET`/`HEAD`-narrowed
   reduction of the pre-existing readability assertion. The evidence, the unchanged limitations and
   the red-before/green-after records are in the receipt (§7/§8).
