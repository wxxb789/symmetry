# 0007d connection management — evidence receipt

Status: **produced and awaiting the owner's verdict**. Every number below was produced on the
working tree described here; no commit, push or history rewrite was performed, and the tree is left
for verification. Owner acceptance — the owner reviewing the rendered connection flows on real data
plus this boundary statement against the fixed design's connection page and truthful-status rules —
is explicitly after this run and not part of the goal's Done when, so this receipt states the result
as produced and does not rate its own rendering or sign for the owner.

Base: `4e60b9d4b7f113cfe0162fa8003a26a01e90e4c9`; the increment is committed as
`709570f25ebb65828e049a653969af3beef127cd` (branch `feat/0007d-connection-management`), and this fix
round leaves 10 modified paths on top of it (`git status --porcelain -uall`, counting this receipt);
`pnpm-lock.yaml` and `package.json` are byte-identical to the base (`pnpm-lock.yaml` sha256
`2a0c9ef07d94f250…`). No commit, push or history rewrite was performed in this round; the tree is
left for verification. `control/priv/static/portal/` is gitignored build output; the browser runs
need it staged (`pnpm --filter @symmetry/frontend build` does that, and the final build was run
before the final browser runs).

Plan: `docs/plans/2026-11-16-0007d-connection-management-plan.md` (§10 records every deviation and
finding taken during implementation).

## 1. What this increment contains

| Group | Paths |
| --- | --- |
| Backend | `control/lib/symmetry_control/integrations.ex` (provider list, bound-resource map, create replay), `integrations/schemas.ex`, `workspaces/read_model.ex` (`connections/0`, bound resources in the workspace projection), `symmetry_control_web/portal_json.ex` (`connections_read/1`, `connection/2`, `connection_resource/1`), `portal_api_controller.ex` (new read action, `disposition` receipts, check failure carries the connection), `router.ex` (`GET /portal/api/connections`, `GET /portal/connections` SPA route), `portal_session.ex` (return-path allowlist for the new route and `connectionId`), `goals/contract_validation.ex`. |
| Contracts | `contracts/v1/connections-read.schema.json`, five new fixture files — 3 valid (`basic`, `empty`, `unknown-values`) and 2 invalid (`missing-connections`, `invalid-version-type`) —, `fixtures/manifest.json` (195 fixtures), `scripts/{generate,check,check-effect,boundary.test}.mjs`, `ts/index.ts`, `generated/ts/connections-read.ts`, `generated/{ts,effect}/index.ts`, `daemon/internal/contracts/{schema.go,schema_test.go,generated.go,schema_bundle.json}`, `daemon/internal/protocol/goal_contract_fixture_test.go`, `contracts/README.md`. |
| Frontend | `frontend/src/services/api.ts` (connections read/mutations, `ServerRefusal`), `app/{application,legacy,router,shell}.tsx|ts`, new `app/session.tsx` (CSRF/operator session context), new `features/connections/{connections.tsx,connections.test.tsx}`, `styles.css`. |
| Browser | rewritten `browser/tests/portal-connections.spec.mjs` (real API + fixture transport), new `browser/tests/portal-classic-resources.spec.mjs` (moved classic resource coverage), new `browser/scripts/run-connection-fixture-tests.ps1`, new `scripts/ci/portal-connection-fixture.exs`, `browser/README.md`. |
| Docs | `docs/plans/2026-11-16-0007d-connection-management-plan.md`, `docs/TODO.md` (one dead-code candidate and three environment/intermittency entries; this fix round added a fourth for the load-sensitive legacy cases). |
| Control tests | `portal_integrations_test.exs`, `portal_controller_test.exs`, `portal_session_test.exs`, `contract_fixture_manifest_test.exs`. |

## 2. Done when → evidence

| Contract clause | Evidence in this run |
| --- | --- |
| create, inspect, update, test and remove for both providers, real API and browser flows, boundary stated per flow | `portal-connections.spec.mjs` test 1 drives create → row/detail inspect → provider check → capability update → removal for GitHub and asserts the removal against `GET /portal/api/connections`; test 5 drives the same five operations for Azure DevOps. Both run in the harness with the real Phoenix endpoint, the real PostgreSQL database and the canned provider transport; the provider boundary is this receipt's §4. |
| idempotent submission, stale updates and concurrent deletion, including what the UI shows when the server refuses a stale write | test 2: the same normalized submission answers `200` with `disposition: "replayed"` and a conflicting payload keeps `422` with `fields.provider = "already has a connection with this name"`, and the console renders the server's field error and `(validation_failed)`. test 3: an external `PATCH` makes the console's Save answer `(stale)`, the dialog pins the name field disabled and requires closing, and an external `DELETE` makes the console's confirm answer `(not_found)` plus "The server's current state is shown instead." with the row gone. The editor generation races are covered by `connections.test.tsx`: a late response for a closed editor never closes or navigates the open one, and a draft submits against the version it was edited from, so a background refresh still answers `(stale)` (both failed before the fix and pass after, `.tmp/evidence/red-frontend-editor-races.txt`). |
| a failed or expired authentication, and a test that fails, produce actionable redacted reasons | test 2 and test 5 check a connection whose account is marked unauthorized: the detail shows `Degraded` and "Provider authentication failed", the receipt shows `(provider_unauthorized)` with "Reauthenticate the provider session on this server…". The spec then asserts no fixture canary appears in any same-origin response body (an unreadable body fails the test; redirects are scanned through their `Location` header), recorded request headers through `allHeaders()` with explicit cookie/CSRF presence checks, request bodies, the DOM, `localStorage`/`sessionStorage`, `document.cookie` and the cookie jar including HttpOnly cookies, and the console and page errors; the harness additionally scans the control log, its error log and the recorded provider-call log and fails on a missing scan file or an empty provider-call log (§4). |
| a capability update visibly resets dependent resource health/sync state, observed through the real API and the database | browser test 4 (UI: receipt "Bound resource health and sync state was reset", the detail shows `Synced` gone and three `Unknown`) plus the direct API+database observation in §5, where the same transaction leaves the resource `unknown/unknown` with `last_synced_at` NULL and `lock_version` 2 → 3. |
| whatever the boundary leaves unverified is reported as unverified | §7 lists the unverified items (live provider credentials and authorization, provider-side mutation, hosted qualification). |
| pre-existing browser acceptance continues to pass on the new route, classic checks retired in the same PR | harness run `10 passed` (1.7 m, both Playwright projects) for the new route; `portal-classic-resources.spec.mjs` `8 passed` (41.4 s, both projects) for the retained resource coverage; the legacy shell specs `31 passed / 2 failed / 7 skipped` (6.2 m) where both failures are `portal.spec.mjs` desktop timeouts that pass alone on the same revision when the host is quiet (recorded as load-sensitive in `docs/TODO.md`); removed coverage in §6. |

## 3. Executed checks

All rows below were re-run in this fix round on the tree described at the top unless marked "not
re-run"; earlier numbers are superseded.

| Exact command (working directory) | Exit | Observed |
| --- | ---: | --- |
| `mix format --check-formatted` (`control/`) | 0 | Clean on the final revision; the round formatted `integrations.ex` and `portal_integrations_test.exs`. |
| `mix compile --force --warnings-as-errors` (`control/`) | 0 | Full recompile, zero warnings. |
| `mix test` (`control/`, `POSTGRES_DB=symmetry_control_test`, dedicated database) | 2 | `830/831 passed, 21 skipped`, `Failed: 1` — the pre-recorded environment-limited `CommandTest` "provider token commands can keep stderr diagnostics out of stdout" (`{:error, {:command_unavailable, "pwsh"}}`); unrelated to this diff. The two fix-round tests pass (831 = 829 + 2). |
| `mix test test/symmetry_control_web/controllers/portal_integrations_test.exs` (`control/`, same database) | 0 | `14 passed` on the formatted revision; the concurrent-create test repeated under seeds 0–3 (`14 passed` each). |
| `mix test` (`control/`, `POSTGRES_DB=symmetry_0007d_test`, the harness database) | not re-run | The harness recreates and migrates that database itself; the recorded pollution limitation is unchanged. |
| `node --test contracts/scripts/*.test.mjs` (root) | 0 | `tests 32, pass 32, fail 0, skipped 0`. |
| `node contracts/scripts/check.mjs` (root) | 0 | "Generated contract DTOs are up to date. Validated 16 Draft 7 schemas and 195 fixtures." |
| `node contracts/scripts/check-effect.mjs` (root) | 0 | "Effect decoded all 195 raw fixtures; 191 parsed documents reached Schema validation." |
| `pnpm --offline contracts:typecheck` (root) | 0 | `tsc --project contracts/tsconfig.json`, clean. |
| `pnpm --offline --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit`, clean. |
| `pnpm --offline --filter @symmetry/frontend lint` | 0 | `eslint . --max-warnings 0`; the round fixed a ref-read-during-render error in the first editor-identity cut. |
| `pnpm --offline --filter @symmetry/frontend test` | 0 | `Test Files 5 passed (5)`, `Tests 48 passed (48)` — was 42; the six migrated coverage tests and the two editor-race regression tests were red before the fix (`2 failed / 12 passed`, `.tmp/evidence/red-frontend-editor-races.txt`) and pass after. |
| `pnpm --offline --filter @symmetry/frontend build` | 0 | `vite build` + `scripts/stage-assets.mjs` into `control/priv/static/portal`; run before the final browser runs. |
| `go vet ./...` / `go test ./...` (`daemon/`) | not re-run | No daemon path is touched by this fix diff; the earlier green run stands for the unchanged packages. |
| `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1` | 0 | `10 passed (1.7 m)` = 5 tests × desktop-chromium + mobile-chromium on the new route with the real topology and the identity handshake; canary scan clean; its own process tree stopped and port 4021 free afterwards. Negative cases, each failing closed: the spec without the harness environment fails at collection (exit 1); a fabricated identity file plus a live foreign target refuses the login before any provider check (exit 1); a `-PortalUrl`/`-Port` mismatch refuses immediately (exit 1); an occupied Docker-forwarded port refuses immediately (exit 1). |
| `pnpm --offline --filter symmetry-browser-acceptance test tests/portal-classic-resources.spec.mjs` with `SYMMETRY_PORTAL_URL=http://127.0.0.1:4022` | 0 | `8 passed (41.4 s)` (4 tests × 2 projects) against a fixture server started from this revision. |
| `… test tests/portal.spec.mjs tests/workspace-shell.spec.mjs` with `SYMMETRY_PORTAL_URL=http://127.0.0.1:4022 SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4023` | 1 | `31 passed / 2 failed / 7 skipped` (6.2 m) against two fixture servers sharing the fixture database (the expiry URL makes the true-TTL cases run instead of skip). Both failures are `portal.spec.mjs` desktop timeouts: "detail output encodes untrusted statuses and rejects unsafe links" (its project-create toast never appeared within 10 s) and "two browser contexts reject a stale project update" (test timeout of 60 s). Neither surface is touched by this diff, and both pass alone on the same revision when the host is quiet (the stale-update case `ok` in 5.0 s under a longer budget; the detail case `ok` in its isolated retry), so they are recorded as load-sensitive in `docs/TODO.md`. |
| `… test --list` | 0 | `Total: 90 tests in 9 files` (with the harness environment present; without it the connection spec fails at collection by design). |

Two iterations of the browser run are worth naming because they were real defects, not tooling noise:
the harness's `set PORT=4021 && …` set `"4021 "` (cmd folds the space before `&&` into the value) and
the script's `Stop-Process` killed only the `cmd.exe` wrapper, leaving beam listening; both are fixed
and the fixed script leaves the port free. A mobile run also exposed a genuine narrow-viewport defect:
the refused-submission dialog's action row sat below the viewport because `.connection-dialog`'s height
budget ignored its own top offset, which made Cancel unclickable until the dialog was forced to fit
(`max-height: calc(100dvh - min(18vh, 120px) - 24px)`), now pinned by
`scrollIntoViewIfNeeded()` + `toBeInViewport({ ratio: 1 })` in the spec.

## 4. Provider evidence boundary

Boundary: **fixtures_only**. The control plane under test is the real application (real router,
controllers, integrations modules, migrations and PostgreSQL database). Only the two external
transports are replaced by `scripts/ci/portal-connection-fixture.exs`:

- `:integration_http_client` answers canned provider documents for `api.github.com` (`/user`,
  `/user/memberships/orgs/:account`, `/repos/:owner/:repository`) and `dev.azure.com`
  (`/:organization/…`), returning `401` for accounts in `SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS`;
- `:integration_command_runner` answers the local `gh`/`az` session commands with fixture tokens.

No provider host is contacted. The fixture keeps the application's real supervision topology: the
orchestration scheduler and reconciler stay up and provider access serves provider actions, so
`/healthz` reports the real database, scheduler, reconciler and provider access. Nothing stops or
replaces an application child, and the harness treats `/healthz` answering `ok` as a readiness
condition. The run fails closed before any browser flow:

- the harness refuses a `-PortalUrl` whose port is not the fixture port it starts, and refuses a
  port that already accepts connections, before it starts anything;
- the fixture publishes a per-run identity file naming the port it serves, and the harness requires
  that file plus the fixture's transport-install log marker before it proceeds;
- the spec requires `SYMMETRY_OPERATOR_TOKEN`, `SYMMETRY_FIXTURE_IDENTITY`,
  `SYMMETRY_FIXTURE_IDENTITY_FILE` and `SYMMETRY_PORTAL_URL` and fails at collection without them;
  every test re-reads the identity file, asserts it carries this run's identity for the portal port,
  and logs in with the per-run operator token before any provider check.

Negative evidence from this round: the spec without the harness environment fails at collection
(`SYMMETRY_OPERATOR_TOKEN is required`); pointed at a live Control instance on another port with a
fabricated identity file and a random token, the login is refused and the test fails before any
provider check; the harness with a mismatched `-PortalUrl`/`-Port` refuses immediately; the harness
pointed at an occupied port (a Docker-forwarded port this shell cannot enumerate as a listener)
refuses immediately before it starts anything, so the foreign server is never contacted.

The fixture only replaces the transport, so it logs at `warning` level to keep the record readable;
the boot-time SQL debug lines stop once the fixture configures the level.

With the real topology the harness run takes 1.3–1.7 min for the 10 connection tests (the original
receipt's 42 s was measured with the scheduler and reconciler stopped), and every flow still passes
inside the configured assertion budget: the earlier "multi-second reads under contention" observation
was not reproduced under this round's conditions.

The harness stops only the process tree it started plus the listener PID it recorded after the
identity handshake; it never kills an unowned process on the port. After the final run port 4021 was
free.

In the final harness run the recorded provider-call log
(`.tmp/evidence/harness-run-final/fixture-calls.jsonl`, copied from the run's `%TEMP%` directory)
contains **30 calls**: 10 to `api.github.com`, 4 to `dev.azure.com`, 12 `gh` and 4 `az` CLI
invocations; every entry records the call shape only (`"authorization":"bearer:github-fixture"`,
`"output":"fixture-canary"`), never a token value. The harness's canary scan of `control.log`,
`control.log.err` and the provider-call log reported "no fixture credential value in the control
logs or the provider-call log"; the control log had 37 lines and the error log 0 bytes. The browser
side is asserted by the spec itself: same-origin response bodies (an unreadable body fails the test;
redirects are scanned through their `Location` header), request headers through `allHeaders()` with
explicit presence checks for the cookie and CSRF headers of `/portal/api/` requests, request bodies,
the DOM, web storage, `document.cookie` and the cookie jar including HttpOnly cookies, console
messages and page errors. A missing scan file or an empty provider-call log fails the harness run
instead of passing the scan blind.

## 5. Capability reset observed through the real API and the database

Command: `.tmp/evidence-capability-reset.sh` against the running fixture control plane; raw output in
`.tmp/evidence/capability-reset-observation.txt`. The connection was created with the capabilities
`["repositories","ci"]`, a repository resource `acme-fixture/symmetry` was bound and synced, then
`PATCH /portal/api/connections/:id` changed the capabilities to `["repositories"]`.

| Observation | Before | After |
| --- | --- | --- |
| `GET /portal/api/connections` (resource entry) | `status: healthy`, `sync_status: synced`, `last_synced_at: 2026-11-15T17:26:32.737000Z` | `status: unknown`, `sync_status: unknown`, `last_synced_at: null` |
| `select … from project_resources …` | `healthy / synced / last_synced_at 17:26:32.737 / lock_version 2` | `unknown / unknown / NULL / lock_version 3` |
| `select … from external_connections …` | `capabilities {ci,repositories}`, `status unknown`, `last_checked_at NULL` | `capabilities {repositories}`, `status unknown`, `last_checked_at NULL` |
| PATCH response | — | `200 {"disposition":"updated","capabilities":["repositories"],"status":"unknown","resources":[{"name":"symmetry","status":"unknown","sync_status":"unknown","last_synced_at":null}]}` |

## 6. Retired classic coverage

`browser/tests/portal-connections.spec.mjs` in the base contained 5 tests and 10 references to the
classic `#connection-table`; the moved `browser/tests/portal-classic-resources.spec.mjs` keeps 4 tests
and **0** `#connection-table` references. The classic connection-surface tests retired with the classic
UI; their intent is migrated to the new route as follows, and no assertion was deleted without a
replacement:

| Retired classic assertion | New-route coverage |
| --- | --- |
| pending form generation ("form generations and connection deletion remain single-flight": a second editor opened while the first create is pending) | `frontend/src/features/connections/connections.test.tsx` "a late create response never closes or hijacks a newer editor" plus the existing "submits a new connection once while pending and reconciles to the server list" |
| connection check/update/delete mutual exclusion while one is pending | `connections.test.tsx` "keeps provider check, update and removal mutually exclusive while one is pending" (and the editor's blocked submit while saving) |
| editor conflict during refresh (a background refresh while the editor is open) | `connections.test.tsx` "submits a draft against the version it was edited from after a background refresh" (the server answers `(stale)` instead of a silent overwrite) |
| connection focus retention across a refresh | `connections.test.tsx` "keeps keyboard focus on the connection row across a background refresh" |
| refresh failure after a successful write | `connections.test.tsx` "keeps the success notice and surfaces the failure when the refresh after a write fails" |
| capability coupling (changes requires repositories) | `connections.test.tsx` "keeps the server's repositories-for-changes coupling observable in the editor" plus the existing server-side validation test in `portal_integrations_test.exs` |
| classic Azure DevOps Entra ID connection assertions incl. the `checkFailure` 502 case | `browser/tests/portal-connections.spec.mjs` test 5 (create/check/update/remove on the new route) and `connections.test.tsx` "keeps a failed provider check actionable, redacted and truthful" (a 502 `provider_unauthorized` check is rendered with the server's reason) |

The first six run as frontend component tests (vitest + jsdom against the real component tree and
the real fetch/response boundary; deferred response gates are not expressible against the real
fixture control plane without mocks); the Azure DevOps flows keep their real-API browser coverage in
the fixture harness. The resource-binding, synchronization, assignment,
project-switch and Azure resource-kind assertions moved with unchanged intent; one assertion that
depended on a deleted connection mock was corrected against the real server computation
(`ReadModel.connection_health/2` reports `degraded` for a degraded bound resource) and the
intentionally failed refresh's 503 console allowance was restored. A hidden `#resource-table`
mutation counter written during the move was removed: no contract clause claims that property for
that surface and the measurement showed the table does re-render behind a background refresh.

## 7. Unresolved limitations (not verified here)

- **Live provider credentials and authorization**: unverified. No real `gh` or `az` session and no
  real provider host was used; every provider answer came from the fixture transport. A live check on
  a real account is a separate authorized outcome.
- **Provider-side resource mutation**: out of scope by the contract; nothing in this run wrote to a
  provider.
- **Hosted qualification**: no CI run, deployment or hosted environment was exercised; all evidence is
  local to this workstation (Windows, containerized PostgreSQL on `127.0.0.1:55440`).
- **Suite state**: the fix-round `mix test` reports `830/831 passed` with the one environment-limited
  failure (needs a working `pwsh`); the harness-database pollution limitation from the original run
  was not re-measured (the harness recreates that database itself). Both are recorded in
  `docs/TODO.md`.
- **Intermittent browser cases**: the original run's `portal.spec.mjs` detach-toast failure did not
  recur this round; instead two other `portal.spec.mjs` desktop cases ("detail output encodes
  untrusted statuses…", "two browser contexts reject a stale project update") timed out under load in
  the 6.2 m legacy run and passed alone on the same revision on a quiet host. Recorded in
  `docs/TODO.md`, not explained.
- **Legacy-shell gate freshness**: the fix round re-ran `portal.spec.mjs` and
  `workspace-shell.spec.mjs` with the expiry URL (7 skipped, no expiry skips) but with the fixture
  server and fixture database instead of the original run's environment, so the two runs are not
  strictly comparable.
- **Daemon gate**: `go vet`/`go test` were not re-run in the fix round; no daemon path is touched by
  the fix diff.
- **Original defect red-steps**: the mobile viewport red-step and the port-sweep observations from
  the original receipt were not reproduced here; the port-sweep code they covered is replaced by
  owned-process cleanup, and the mobile project (including the viewport assertion) runs green in the
  fix-round harness.
- **pnpm supply-chain verification**: cannot complete on this host (the public registry is
  unreachable); `--offline` makes it fail in about a second and print its `node_modules` warning
  before the script runs. `node_modules` was installed for this exact lockfile
  (`pnpm-lock.yaml` sha256 `2a0c9ef07d94f250…`, unchanged from the base).
- **Owner acceptance**: not issued. The receipt states the result as produced and awaiting the
  owner's verdict; the model does not rate its own rendering and does not sign for the owner.
- **Measurement claims**: latency numbers quoted here are single observations on a loaded development
  host (the API answers were measured at 0.36–0.48 s per request over curl); they are context for the
  fixture changes, not a performance claim.
