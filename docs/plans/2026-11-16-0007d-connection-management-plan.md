# 0007d connection management — implementation plan

Goal contract: [docs/goals/0007d-connection-management.md](../goals/0007d-connection-management.md).
Base revision: `4e60b9d4b7f113cfe0162fa8003a26a01e90e4c9` (branch `feat/0007d-connection-management`).
Shape reference: 0007a (`27dc3e4`, deployable workspace shell) — one read schema, real-API
browser flows, focused browser gate, docs closure.

## 1. What already exists (measured, not assumed)

| Layer | State at base |
| --- | --- |
| DB | `external_connections` (unique `[provider, name]`, provider/auth_type/status check constraints, `lock_version`), `project_resources` (`status`, `sync_status`, `lock_version`, partial unique connected identity). No credential column. |
| Server | `POST/PATCH/DELETE /portal/api/connections[...]`, `POST /portal/api/connections/:id/check`; `Integrations.{list,create,update,delete,check}_connection`; optimistic locking (`:stale` → 409); `update_connection/2` already resets connection + bound-resource health in one transaction when `capabilities` changes; provider responses with secret-looking keys are rejected (`validate_provider_map/1` → `:invalid_provider_response`); every failure reason is a fixed string from `failure/1` (HTTP bodies discarded). |
| Missing | no `GET /portal/api/connections`; no replay/idempotency signal on create; no UI on `/portal/connections` (classic `#connections` view is the only connection UI); legacy `#connections` hash still points at classic. |
| Browser | `browser/tests/portal-connections.spec.mjs` (1447 lines) drives `/portal/classic` with `page.route("**/portal/api/**")` JSON mocks — no DB, no provider. |

## 2. Done-when → deliverable map

| Contract clause | Deliverable | Evidence |
| --- | --- | --- |
| create/inspect/update/test/remove, both providers, real Portal API + browser | `GET /portal/api/connections`; existing mutation endpoints; SPA feature `frontend/src/features/connections/` | new `browser/tests/portal-connections.spec.mjs` against a real Phoenix + Postgres server; per-flow real/fixture boundary in the receipt |
| idempotent submission | create replay: identical `[provider, name, account_ref, capabilities]` returns the applied row with `disposition: "replayed"` (200); different payload keeps today's 422 | ExUnit + browser flow (double submit, retry after timeout) + DB row count |
| stale updates | existing optimistic lock → 409 `stale`; SPA renders the server's current row | browser flow: mutate behind the page, then submit stale PATCH |
| concurrent deletion | existing ver→409/404; SPA shows removed/refused state | browser flow: delete twice (two contexts) |
| actionable redacted reasons | `status_message` + `error.message` from `failure/1`; `secret_data?` rejection; fixture tokens are canaries | rendered text + recorded page responses/DOM/storage + server log scan for the canary |
| capability update resets dependent health/sync | existing transactional reset + `reset_resource_ids` in the update receipt | API response + SQL observation of `project_resources` after the update |
| unverified reported | receipt limitations list | receipt |
| pre-existing coverage passes on the new route, classic checks retired | new-route spec replaces classic connection checks | executed suites + removed check list with counts/exit codes |

## 3. Wire design (additive; no existing field changes meaning)

`GET /portal/api/connections` (new, `portal_api` pipeline):

```json
{ "providers": [ { "id": "github", "auth_type": "gh_cli",
                   "capabilities": ["repositories","work_items","changes","ci"] } ],
  "connections": [ { "id": "...", "provider": "github", "name": "...", "account_ref": "...",
                     "auth_type": "gh_cli", "capabilities": [...], "status": "unknown",
                     "status_message": null, "metadata": {}, "last_checked_at": null, "version": 1,
                     "resources": [ { "id": "...", "project_id": "...", "project_key": "EXT",
                                      "project_name": "...", "kind": "repository", "name": "...",
                                      "external_ref": "acme/symmetry", "url": null,
                                      "status": "unknown", "sync_status": "unknown",
                                      "status_message": null, "last_checked_at": null,
                                      "last_synced_at": null, "version": 1 } ] } ] }
```

- Provider catalog (allowed capability ids, allowed auth type per provider) is server-owned:
  `Integrations.connection_providers/0` reads `Connection`'s existing schema rules — the UI never
  restates provider/capability policy. Presentation labels stay in the UI with an explicit
  unknown-value fallback (the `StatusBadge` precedent).
- Read model: `Workspaces.ReadModel.connections/0` (same shape as `workspace/1`) + `PortalJSON.connections/1`.
- Mutations gain an additive `disposition`: `"created" | "replayed" | "updated" | "deleted" | "checked"`.
  `create` replay rule: after normalization (trim, capability uniq+sort) an existing row with the same
  `provider` + `name` and the same `account_ref` + `capabilities` is returned with 200/`replayed`
  (identical submission converged); any other payload keeps the current path and error.
  A concurrent identical insert that loses the unique-index race is re-read and replayed the same way.
- `update` receipt repeats the connection read projection instead of adding the
  `reset_resource_ids` list this plan originally promised: the projection's resource entries
  already carry the reset `status`/`sync_status`/`last_synced_at`, and nothing in the contract
  consumed an id list. The unimplemented promise and its replacement are recorded as the
  deviation in §10.8.
- Locked error envelopes keep their existing shape (`{error: {code, message, fields?}}`); the
  connections page decodes them (including the projection the check endpoint already returns).
- `GET /portal/api/connections` is additive to the workspace read: `PortalJSON.workspace/1` keeps
  its `connections` list entries compatible — each entry gains the additive `resources` array and
  the projection runs one all-connections resource query — and classic `portal.js` reads only the
  named scalars it consumed, so it and existing clients are untouched.

## 4. Frontend design

- `features/connections/connections.tsx`: list + detail for `/portal/connections`; selection is URL
  state (`?connectionId=`), server state is one TanStack Query (`["connections"]`), drafts/pending
  state is React-local. No second cache; after every mutation the list query is invalidated and the
  authoritative payload is re-rendered.
- Create/edit dialog (Radix `Dialog`, existing tokens): provider, name, account_ref, capability
  checkboxes from the server catalog; the capability coupling (`changes` requires `repositories`) is
  enforced by the server changeset and surfaced as a field error, not re-implemented as policy.
- Test/check button: pending until the server answers; renders the new status, `last_checked_at` and
  the redacted reason; a refused check renders the server's refusal envelope and re-reads the list
  for the authoritative state (the projection in the 502 body is not consumed directly).
- Update: submits the loaded `version`; a 409 renders "This connection changed elsewhere…" plus the
  server's current values (refetch), never a silent success.
- Delete: confirm dialog; 409/404 renders "already changed or removed" and reconciles to server truth.
- Session/mutation wiring: `services/api.ts` gains a mutation path (same-origin, `x-csrf-token`,
  `Accept: application/json`, `cache: no-store`, no retries except the existing read policy), typed
  errors (`ServerRefusal` carrying `code`/`message`/`fields`), and effect-schema decoding of the new
  read plus the connection projection. The CSRF token comes from the session the shell already
  loaded (React context from `shell.tsx`), so no extra session round trip; 401 from a mutation goes
  through the same sign-out recovery the query cache already implements.
- Routing: `/portal/connections` route with validated search; navigation link; command palette entry;
  `legacyDestination` now sends `#connections` to `/portal/connections`; `safeReturnTo` admits the
  new path so a deep link survives sign-in.

## 5. Contracts

One new Draft 7 schema `contracts/v1/connections-read.schema.json` (root: providers + connections;
definitions: `ConnectionProvider`, `Connection`, `ConnectionResource`), fixtures (valid
basic/empty/unknown-values, invalid missing-connections/invalid-version-type), manifest entries,
regenerated TS/Go/Effect DTOs (`pnpm contracts:generate`; schema counts 15 → 16 in `generate.mjs`,
`check.mjs`, `daemon/internal/contracts/schema*.go`), and the existing fixture-manifest
registrations for the Go and Elixir checks. Legacy envelopes (`{connection}`, `{disposition}`,
`{deleted_connection_id}`, `{error:{code,message,fields}}`) keep their hand-written Effect decoders
in `services/api.ts`, the same treatment `/portal/api/session` already has.

## 6. Browser acceptance and classic retirement

- New: `browser/tests/portal-connections.spec.mjs` rewritten for `/portal/connections` against the
  real Portal API and Postgres, seeding through the real API (the `workspace-shell.spec.mjs` pattern:
  CSRF POST from the page, screenshots + observation JSON, no route interception).
- Fixture provider transport: `scripts/ci/portal-connection-fixture.exs` starts the real control app
  with a controlled `:integration_http_client` (canned GitHub/ADO responses, 401 for the marked
  account) and `:integration_command_runner` (fixture CLI tokens). Real provider modules, real HTTP
  handler plumbing, real DB; the transport never touches the network and records every call. Fixture
  tokens are canaries: the spec asserts none of them appears in any same-origin response body, the
  DOM, `localStorage`/`sessionStorage`, or the server log.
- Retired in this PR (replaced by the new-route checks): the connection-management assertions and the
  connection form/delete single-flight test in `browser/tests/portal-connections.spec.mjs`, plus the
  connection check single-flight/focus/partial-failure case and the classic ADO auth-type assertions.
  The resource-binding/sync/assignment checks of the same file belong to 0007e's surface and move to
  `browser/tests/portal-classic-resources.spec.mjs` with unchanged behavior assertions.

## 7. Out of scope (with reason)

- Resource binding/sync editing UI (`/portal/connections` resource mutations) — 0007e owns it; this
  goal reads bound resources for connection health/reset visibility only.
- Any provider-side resource mutation, live provider credentials, `gh`/`az` sessions — boundary
  `fixtures_only`.
- Runtime control, work views, chat surfaces (sibling goals).
- Removing the classic UI/renderer — 0007i cutover.
- A per-provider capability matrix or new DB constraints — no evidence demands them; the existing
  app-level rules stay the single authority.

## 8. Gates

`mix format --check-formatted`, `mix compile --warnings-as-errors`, `mix test` (control),
`pnpm contracts:check`, `pnpm --filter @symmetry/frontend {typecheck,lint,test,build}`,
`pnpm --filter symmetry-browser-acceptance test tests/portal-connections.spec.mjs` and
`tests/portal-classic-resources.spec.mjs` against the fixture server, plus a SQL observation of the
capability reset. Benchmark harnesses are not part of this goal; no run happens concurrently with a
full suite.

## 9. Open decisions taken (reversible defaults)

1. Idempotency key = the natural key `(provider, name)` + normalized payload equality, not a stored
   client `mutation_id`. Reason: the unique index already defines identity; no migration, no new
   field, no wire change for classic clients, and the observable property (a repeat of the same
   submission converges on one row with the applied state) holds while the applied row keeps its
   name: renaming or removing that row before the retry makes the retry a new submission, because
   the natural key no longer resolves to it. Alternative rejected: storing `mutation_id` (schema
   churn, no additional guarantee for this surface).
2. Selection is a search param on the single design route instead of a new child route; the fixed
   design's table lists first-screen content, and attention already scopes by `?projectId=`.
3. The fixture transport lives in `scripts/ci/` (matching `credentialed-provider-witness.exs`), not
   in `control/lib` + runtime env: no fake transport ships inside the control application.

## 10. Implementation notes (recorded while executing this plan)

Measured during the browser gate; each note names the evidence, not a preference.

1. **Fixture control plane load.** The dev-config control plane logs every SQL statement at debug
   level and runs the orchestration scheduler and reconciler every second. Under a browser run the
   log volume blocked the endpoint's output pipe and the scheduler tick contended with the page's
   reads: a `GET /portal/api/connections` that the server answers in 0.48 s over curl was still
   unanswered after 10 s in the browser (Playwright trace `status: -1`), and shell bootstrap took
   ~12 s. `scripts/ci/portal-connection-fixture.exs` sets the logger to `:warning` and keeps the
   application's real supervision topology: an early cut also terminated the
   `Orchestration.Scheduler`/`Reconciler` children after boot, and the first fix round (`bbc9b6f`) removed that
   termination (the receipt's `/healthz` readiness now covers the real database, scheduler,
   reconciler and provider access; no production behaviour changes). The spec also gets a 30 s
   assertion budget and a 120 s per-test
   timeout instead of the shared 10 s/60 s, and
   `browser/scripts/run-connection-fixture-tests.ps1` clears the acceptance database's portal rows
   before starting, so the run does not inherit a growing connection list.
2. **Account ownership of a bound resource.** `POST /portal/api/projects/:id/resources` with a
   `connection_id` is validated by the provider module
   (`Integrations.Providers.GitHub.validate_resource_reference/3` → `require_account/2`): a reference
   whose owner is not the connection's `account_ref` is refused with `forbidden`. The acceptance
   spec therefore binds `acme-fixture/symmetry` to the `acme-fixture` connection; this is existing
   behaviour, not a new rule.
3. **Capability reset scope.** The existing transactional reset (`Integrations.update_connection/3`)
   resets the connection's own check state (status, metadata, `last_checked_at`) and every bound
   resource in an active project (status, sync status, timestamps, `lock_version + 1`) whenever
   `capabilities` changes. The browser assertion counts three `Unknown` labels (connection health,
   resource health, resource sync); the DB observation in the receipt shows the same rows.
4. **Retired classic coverage.** The move to `browser/tests/portal-classic-resources.spec.mjs` keeps
   the resource-binding/sync/assignment assertions and drops the connection ones (connection table,
   connection form/delete single-flight, check single-flight/focus/partial failure, classic ADO
   auth-type). Two assertions that depended on deleted connection mocks were corrected against the
   real server computation instead of being kept vacuously: the health grid now receives
   `connections: "degraded"` for a degraded bound resource
   (`ReadModel.connection_health/2` returns `degraded`), and the 503 console allowance the
   intentionally failed refresh needs was restored. A hidden-`#resource-table` mutation counter
   written during the move was removed: no contract clause claims that property for that surface,
   and the measurement showed the table does re-render behind a background refresh.
5. **Goal file stays in place.** 0007a/0007b/0007c remain in `docs/goals/` and their rows stay in the
   README table while owner acceptance is pending; owner acceptance is explicitly after this run and
   not part of this goal's Done when, so this increment does not archive the contract or rewrite the
   index. The receipt states the result as produced and awaiting the owner's verdict.
6. **Narrow-viewport dialog.** A mobile (Pixel 7, 412×915 CSS px) run failed intermittently at the
   refused-submission step: `Cancel` stayed unclickable because the dialog's height budget
   (`calc(100dvh - 64px)`) ignored its own top offset, so once the field error and refusal notice
   rendered, the action row sat below the viewport and the fixed-position dialog could not be
   scrolled further. `.connection-dialog` now subtracts `min(18vh, 120px)` and a 24 px bottom margin,
   and the spec pins the property with `toBeInViewport()` on `Cancel` after a refusal instead of
   relying on the click timing out or not.
7. **Control suite database separation.** Running the control suite with `POSTGRES_DB` pointing at
   the browser harness's database makes 10 tests fail (suite-level aggregates see committed fixture
   rows: `sync_all_resources/0`, workspace and portal reads). The suite belongs on its own database
   (the repository default `symmetry_control_test`), where it reports 828/829 passed, 21 skipped and
   one environment-limited failure that needs a working `pwsh`. Both findings are recorded in
   `docs/TODO.md`.
8. **Update-receipt deviation.** §3 promised that the `update` receipt would add
   `reset_resource_ids` (the active-project bound resources the transaction reset). The
   implementation never returned that list: the PATCH receipt repeats the connection read
   projection, whose resource entries carry the reset `status`/`sync_status`/`last_synced_at`
   (observed in the receipt's §5), and no contract clause or client consumed an id list. The
   promise is corrected in §3 instead of implemented; an id list would add wire surface with no
   consumer.
