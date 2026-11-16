# 0007c safe work planning — implementation plan

Goal contract: [docs/goals/0007c-safe-work-planning.md](../goals/0007c-safe-work-planning.md).
Base revision: `7a92ca498f60309b97920b9df834f2637b91cfbd` (branch `feat/0007c-safe-work-planning`,
the merged 0007b read surfaces). Shape reference: 0007b (`7a92ca4`, read surfaces and their
browser gate) and the sibling mutation increment 0007d (`86b368a` + fix rounds in its own
worktree: contract root, `ServerRefusal`, real-API browser flows, per-mutation single-flight,
receipt with source-subject binding).

Prerequisite honoured: the 0007b list, board, detail and history surfaces are the surfaces this
goal makes writable. No new route is introduced: mutations live on `/portal/projects`,
`/portal/projects/:projectId` and `/portal/work/:workItemId`, whose search parsing, login
`return_to` allowlist and document routes already exist.

## 1. What already exists (measured, not assumed)

| Layer | State at base |
| --- | --- |
| Elixir API | `POST /portal/api/projects`, `PATCH /portal/api/projects/:id` (versioned, archived projects accept only `{status: "active"}`, archive refused while any execution is active), `POST /projects/:id/work-items`, `PATCH /work-items/:id` (versioned, `:provider_owned` for title/description/priority when an external tracker resource is bound, `:state_conflict` for execution-defining fields while a run is active), `PATCH /work-items/:id/move` (versioned; `status` + optional `before_id`; the server owns positions), and the control paths (`run`/`cancel`/`retry`/`input`, action-id idempotent). Every refusal is `{error: {code, message, fields?}}` through `Protocol.error/2` (`stale` 409, `state_conflict` 409, `provider_owned` 409, `goal_authority_required` 403, `validation_failed` 422). |
| Read surface | `PortalJSON.project` serves `version`; `PortalJSON.work_item` serves `version`, `position`, `status`, `external.provider_owned_fields`, `can_start`, `execution`. Workspace rows are ordered by `position` and carry every project. |
| Goal authority | `Workspaces.ensure_legacy_work_item/1` refuses every legacy mutation/control path for a work item whose `goal_id`, `admitted_revision` and `acceptance_contract` are all set (all-or-nothing relational invariant; `work_items_goal_fields_all_or_none`). **The read projection does not expose that state**, so the UI cannot present "no writable path" without an additive member. |
| Frontend | Read-only by construction: `services/api.ts` has GETs only; `features/projects/{projects,work-surface,filters,saved-views}.ts(x)` render list/board; `features/work/{detail,history,presentation}.tsx` render the detail; `app/shell.tsx` owns navigation. `features/connections` does not exist at this base. |
| Browser | `browser/tests/portal.spec.mjs` (classic `/portal/classic`) still holds this surface's editing checks: project/work-item create+edit, board ordering, archive/restore read-only, the two-context stale project and work-item updates, the delayed-mutation ownership race and the refresh-preserves-drawer case. `browser/tests/portal-work-views.spec.mjs` covers the 0007b read surfaces on the new routes. |

## 2. Done-when → deliverable map

| Contract clause | Deliverable | Evidence |
| --- | --- | --- |
| create/edit/archive/restore projects; create/edit/order/move work items, real API and browser | New SPA mutation surfaces (project dialog, work-item dialog, board keyboard movement) over the existing versioned endpoints | new `browser/tests/portal-work-planning.spec.mjs` against real Phoenix + PostgreSQL, both Playwright projects |
| duplicate submissions avoided; explicit retries reuse the original intent | per-form single-flight (control disabled + handler guard); a failed mutation keeps its frozen intent (payload + expected version) and Retry re-sends it verbatim | browser: double-submitted create (one POST, one row) and a retry after a transport abort whose second request body equals the first |
| optimistic-lock conflict surfaces visibly and re-syncs the newest server state | 409 `stale` renders the server refusal, closes the editor and invalidates the authoritative read | browser: two contexts edit one project and one work item; the loser sees the refusal and the winner's value |
| background refresh never replaces another selected resource's draft | editor drafts are local state snapshotted at open; queries are keyed per resource; late responses apply only to their own scope | browser: refresh while editing; cross-project race with a held response |
| cross-project response races, refresh while editing, validation errors and recovery leave no corrupted visible state | scope-guarded mutation application; validation `fields` rendered next to the editor; recovery by re-read | the same browser set |
| keyboard board movement and focus restoration | `Shift+←/→` moves the focused card across columns, `Shift+↑/↓` reorders within the column, focus returns to the same work item after the regroup, a live region announces the move | browser: keyboard move on both widths, persisted order asserted through the workspace read |
| Goal-managed items and provider-owned fields present no writable path (run/retry/move route to Goal admission) | additive `goal_managed` read member; no editor/move path for such items; provider-owned fields (served list) are not editable; the server's 403/409 refusal is surfaced verbatim if a stale snapshot offers an edit | browser: fixture-created goal-managed and provider-owned items; direct API refusal asserted alongside |
| archived project read-only except restore | archived projects render the notice, no create/edit/move controls, only Restore project | browser: archive → read-only assertions → restore |
| pre-existing browser acceptance continues on the new routes; classic checks retired in the same PR | migrated assertions in the new spec; classic planning checks removed from `portal.spec.mjs`; resource/control coverage untouched | executed suites and the removed/changed spec list in the receipt |

## 3. Wire and contract deltas (additive only)

1. `SymmetryControl.Workspaces.WorkItem.goal_managed?/1` — the all-or-nothing predicate moved into
   the schema module; `Workspaces.ensure_legacy_work_item/1` delegates to it so the served marker
   and the refusal cannot drift.
2. `PortalJSON.work_item` gains `goal_managed: WorkItem.goal_managed?(item)`.
3. `contracts/v1/workspace-read.schema.json` and `contracts/v1/work-detail-read.schema.json`:
   add optional `goal_managed` (boolean) to the shared `WorkspaceWorkItem` definition and optional
   `provider_owned_fields` (array of strings) to `WorkItemExternal`; both definition bodies stay
   byte-identical across the two files (the effect generator merges definitions by name and fails on
   conflict). Regenerate `contracts/generated/**`, `daemon/internal/contracts/**` with
   `pnpm contracts:generate`; `pnpm contracts:check` must stay green. No existing field changes
   meaning; both members are optional because a row keeps accepting older partial projections.
4. Everything else the increment consumes is already served: `version`, `position`, `status`,
   `external`, `delivery.*.source` and the locked error envelope; the mutation *receipts* are
   consumed as identity + disposition only (the authoritative projection is re-read), the same
   documented treatment 0007d gave `{disposition, connection.id}`.

No migration, no new endpoint, no permission, no session or pagination change.

## 4. Frontend design

- `services/api.ts` gains the mutation path of 0007d: `ServerRefusal` (verbatim code/message/fields),
  `request` with method/body/csrf, no retry for commands, 401 routed to the existing expiry handler,
  and `createProject/updateProject/createWorkItem/updateWorkItem/moveWorkItem` returning identity
  receipts. Reads keep their current decoders.
- `app/session.tsx` (as in 0007d): the shell hands the bootstrap CSRF token to pages through
  context instead of every page re-fetching `/portal/api/session`.
- `app/planning.ts` (new): one module owning the write-surface policy so pages cannot restate it —
  `usePlanningMutations()` (per-subject single-flight + scope guard + frozen intent + notice state),
  `projectWriteAllowed(project, item)`, field-level provider-owned mapping from the *served*
  `external.provider_owned_fields`, and `readOnlyReason` (archived project, goal-managed item).
- `features/projects/projects.tsx`: project cards keep their read markup; selection state is URL
  (`?project_id=` on `/portal/projects`, matching the classic switcher semantics) so a project's
  work loads without a new route — implemented as a search parameter on the existing route with the
  `legacy`/`safeReturnTo` rules kept. "New project" dialog and (active) "Project settings" dialog
  with name/key/defaults/description/status; archived projects render the notice and a single
  "Restore project" action. Success and refusal notices are inline `role="status"`/`role="alert"`
  regions, not toasts, so they are assertable and screen-reader visible.
- `features/projects/work-surface.tsx`: "New work item" toolbar action (active projects only),
  board keyboard movement (`Shift+Arrow`), a visible hint and a live announcement region; the
  existing focus-restore observer keeps working across the regroup the move causes.
- `features/work/detail.tsx`: "Edit work item" action for items whose fields are not all
  provider- or Goal-owned; the editor dialog maps exactly the fields the server accepts
  (`update_changeset` cast list) minus provider-owned ones; status is never edited through the
  update path (the server rejects it) — status changes go through board movement, which is the
  surface's move path.
- One shared `WorkItemDialog` component serves create and edit so create-only (status) and
  edit-only (version) differences are explicit in one place.
- Draft safety: the dialog snapshots its initial values when it opens and never re-derives them from
  a refresh; unmount (navigation) disposes the draft with the registration; a background refresh
  cannot re-key or remount an open dialog.
- Retry: a refused or failed mutation keeps `{payload, version}` frozen; "Retry" re-sends exactly
  that intent. A 409 never offers Retry as a rebase: it offers "Reload current state", which
  invalidates the read and shows the server's current values.
- Saved view preferences stay versioned localStorage in the `symmetry.portal.saved-views` document
  (`version: 1`, deployment-origin scope). The session bootstrap serves no user identity in this
  single-operator deployment, so the deployment origin is the "user/deployment" scope; the document
  schema admits only view + filter fields and is validated on read, so credentials, evidence,
  session handles and task data cannot round-trip through it. New unit assertions pin the version,
  the scope and the discard-on-mismatch behaviour.

## 5. Browser acceptance and classic retirement

- New `browser/tests/portal-work-planning.spec.mjs` (real API + database, seeding through the
  authenticated Portal API like `readable-fixtures.mjs`, screenshots + observation JSON per test):
  project create + validation refusal; project edit; archive read-only + restore; conflict on the
  project with two contexts; work-item create + validation refusal; edit from the detail; conflict
  on the work item with two contexts; board keyboard move + reorder with persistence and focus;
  refresh-while-editing and cross-project held-response races; double submit; explicit retry;
  goal-managed and provider-owned no-writable-path (with the direct API refusal).
- Fixture for the two unwritable item kinds: a small script executed through Control's own changeset
  path (`scripts/ci/portal-planning-fixture.exs`, invoked with `mix run` from `control/`), applying
  `WorkItem.goal_membership_changeset/2` with a real `goals` row for one item and
  `WorkItem.provider_changeset/2` with a work-tracking resource for another. The browser flows stay
  real HTTP/DB; the receipt names this fixture boundary (the Goal *admission* flow itself is
  0007h's surface and is not exercised).
- Retirement: the classic planning assertions in `browser/tests/portal.spec.mjs` are removed and
  their intent migrated to the new spec (that file keeps login/CSRF/logout, keyboard drawer and the
  non-planning cases). Classic resource/control coverage (`portal-classic-resources.spec.mjs`,
  compose/live daemon runners) is untouched: it belongs to 0007e/0007f fixtures.
- `browser/README.md` records the new suite and what the classic file still owns.

## 6. Out of scope (with reason)

- Provider connections (0007d), resource binding/sync (0007e): not touched.
- Runtime control (run/cancel/retry/input) and Chat (0007f/0007g): this increment renders no control
  affordance for them; the Goal-admission clause is honoured by presenting *no* path rather than a
  re-routed control, and by surfacing the server's `goal_authority_required` refusal when a stale
  snapshot offers an edit.
- Goal-specific operations (0007h): no Goal workspace, no admission UI; the fixture writes the
  admitted state through the real changeset only to prove the read/refusal behaviour.
- Classic UI removal (0007i): the classic surface keeps its own editing flows; only the planning
  *acceptance* moves.

## 7. Gates

`mix format --check-formatted`, `mix compile --warnings-as-errors`, `mix test` (control, dedicated
database), `pnpm contracts:check` + `pnpm contracts:typecheck` (regenerated DTOs), `go vet ./...` +
`go test ./...` (daemon; generated contract artifacts changed), frontend `typecheck`, `lint`,
`test`, `build`, and the browser suites (`portal-work-planning.spec.mjs` focused, then
`portal.spec.mjs` + `workspace-shell.spec.mjs` + `portal-work-views.spec.mjs`). No measurement
harness runs concurrently with the full suite on this host.

## 8. Open decisions taken (reversible defaults)

1. **No new route or query parameter for selection/editing.** The project switcher uses the
   `?project_id=` search parameter on `/portal/projects` (already allowed by the login allowlist:
   the classic switcher wrote the same parameter); editors are modal local state. Reason: the login
   `return_to` allowlist is an anchored security boundary, and nothing in the contract needs a
   deep-linkable editor.
2. **Mutation receipts are identity + disposition only.** The server's write responses repeat a
   partial project projection (unloaded associations render as empty arrays); merging them into the
   read cache would replace real work items with `[]`. Every mutation therefore invalidates the
   authoritative read, exactly as 0007d documented for connections.
3. **Provider-owned edits follow the served list** (`external.provider_owned_fields`) rather than the
   server's narrower refusal set, so the UI never offers an edit that the provider owns; the server
   refusal remains the enforcement point for the three fields it refuses.
4. **Status/order changes use the move endpoint** (`PATCH /work-items/:id/move`) because
   `update_changeset` deliberately rejects move fields; the board is the only status editor.
5. **`goal_managed` is served, not inferred.** Both the marker and the refusal come from one
   predicate in the schema module.
6. **Preference scope stays the deployment origin** (no user identity is served in this
   single-operator deployment); versioned document, discard on version/scope mismatch.

## 9. Implementation notes (recorded while executing this plan)

1. **Consumed read members are additive *optional* members, and the surface refuses a write without
   a served revision.** 0007d's write-capable read model makes its consumed members required,
   because that schema root is new. These members extend `WorkspaceProject`/`WorkspaceWorkItem`,
   whose fixture corpus deliberately covers older partial projections
   (`contracts/fixtures/valid/workspace-read.basic.json` serves a project without `version` and
   resources without `kind`). Requiring them would reject that corpus, so they stay optional and the
   client treats an absent revision as "no write path" (`hasServedRevision`) instead of inventing
   one; the server's changesets remain the enforcement point.
2. **An update never carries a provider-owned field, even unchanged.** Control refuses only a
   *changed* provider-owned field (`changeset.changes`), but the editor renders those fields
   read-only, so the payload omits them entirely (`withoutProviderOwned`): what is sent equals what
   the operator could edit. The unit double in `planning.test.tsx` refuses *any* provider-owned key
   in a body, which pins that property.
3. **Delivery values are gated by their served source.** `pull_request_url`, `ci_status` and
   `review_status` stay editable only while the served `delivery.*.source` is `none` or `manual`; a
   `provider` or `agent` value renders read-only with its source. This keeps the classic surface's
   manual-delivery intent without offering a write over provider- or evidence-owned data.
4. **Board keyboard movement targets rendered columns only.** The board renders the states the
   served rows actually use (0007b's rule: a served state is never dropped), so a state with no
   item has no column to move into; creating the first item of a new state is the create form's
   `Status` select. Recorded as a UX limitation, not a defect: no request is sent when there is no
   neighbouring column, and the card stays focused.
5. **Project creation stays on the list.** The notice and the created row are both on
   `/portal/projects` (the re-read is what makes them truthful); the operator opens the new project
   from its row instead of being navigated away before the notice can be read.
6. **Same-tick single-flight.** The editors hold an `inFlightRef` in addition to the disabled
   control, because two submits dispatched in one tick would both read the rendered pending flag as
   false and the duplicate would reach the server.
7. **The two unwritable item kinds need a fixture.** A Goal-managed row comes from Goal admission
   (0007h's surface) and an external-tracker binding from provider sync (0007e's surface); neither
   is reachable through the portal API. `scripts/ci/portal-planning-fixture.exs` applies Control's
   own `goal_membership_changeset/2` / `provider_changeset/2` with `mix run` against the same
   database, and the suite then asserts the real API refusals (`goal_authority_required` 403,
   `provider_owned` 409) alongside the rendered no-writable-path state.
8. **Environment.** Node gates run against the retained tooling workspace `.tmp/0007c-tooling`
   (node_modules installed for the pinned lockfile `2a0c9ef07d94f250…`; the public pnpm registry is
   unreachable, so installs are `--offline` or unnecessary). The browser harness starts the real
   control plane in dev configuration on loopback with `PORT`, a per-run operator token and the
   dedicated database `symmetry_0007c_test` on 127.0.0.1:55441; the built frontend assets must be
   staged into `control/priv/static/portal` first (four control portal tests also serve that build).
9. **Classic retirement.** `browser/tests/portal.spec.mjs` keeps the classic resource
   (0007e), control/Activity (0007f), session-boundary and background-refresh-drawer coverage and
   stops driving the classic project/work-item planning dialogs; its seeding moves to the
   authenticated Portal API. The removed/changed spec list is recorded in the receipt.

10. **Audit-driven additions (recorded after the dual independent audit, before close).** The
    increment additionally (a) sends `null` for the optional fields an operator clears on the update
    path, because Ecto reads an empty string as "no change" and the cleared value was previously
    kept; (b) reports, in the success notice, the fields a submission could not carry (a field the
    server now reports as owned elsewhere, or a recorded pull request URL Control cannot clear) and
    does not offer the CI/review "not recorded" choice once a value is recorded, because Control's
    changeset refuses nil there; (c) treats every 5xx as transient so a command keeps its frozen
    intent with a visible retry; (d) tells the operator when an open form closes because the row
    stopped serving a revision; and (e) migrates the two classic assertions the review found
    unreplaced (owner reassignment and a manual delivery value) into the planning edit case. The
    drawer and both planning editors close only on the operator's own dismissals, so an
    unattributable layer request (a window resize) cannot navigate out from under an open draft. A
    viewport-framed capture replaces the full-page one for the provider-owned editor, because a
    full-page capture resizes the emulated viewport. Evidence, red-before/green-after values and the
    residual limitations are in the receipt.
