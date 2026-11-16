# 0007d connection management — evidence receipt

Status: **produced and awaiting the owner's verdict**. Every number below was produced on the
working tree described here; no commit, push or history rewrite was performed, and the tree is left
for verification. Owner acceptance — the owner reviewing the rendered connection flows on real data
plus this boundary statement against the fixed design's connection page and truthful-status rules —
is explicitly after this run and not part of the goal's Done when, so this receipt states the result
as produced and does not rate its own rendering or sign for the owner.

Base: `4e60b9d4b7f113cfe0162fa8003a26a01e90e4c9` (branch `feat/0007d-connection-management`).
Working tree: 36 modified and 15 new paths (`git status --porcelain -uall`, counting this receipt);
`pnpm-lock.yaml` and `package.json` are byte-identical to the base (`pnpm-lock.yaml` sha256
`2a0c9ef07d94f250…`).
`control/priv/static/portal/` is gitignored build output; the browser run needs it staged
(`pnpm --filter @symmetry/frontend build` does that, and the final build was run before the last
browser run).

Plan: `docs/plans/2026-11-16-0007d-connection-management-plan.md` (§10 records every deviation and
finding taken during implementation).

## 1. What this increment contains

| Group | Paths |
| --- | --- |
| Backend | `control/lib/symmetry_control/integrations.ex` (provider list, bound-resource map, create replay), `integrations/schemas.ex`, `workspaces/read_model.ex` (`connections/0`, bound resources in the workspace projection), `symmetry_control_web/portal_json.ex` (`connections_read/1`, `connection/2`, `connection_resource/1`), `portal_api_controller.ex` (new read action, `disposition` receipts, check failure carries the connection), `router.ex` (`GET /portal/api/connections`, `GET /portal/connections` SPA route), `portal_session.ex` (return-path allowlist for the new route and `connectionId`), `goals/contract_validation.ex`. |
| Contracts | `contracts/v1/connections-read.schema.json`, five new fixtures plus two invalid ones, `fixtures/manifest.json` (195 fixtures), `scripts/{generate,check,check-effect,boundary.test}.mjs`, `ts/index.ts`, `generated/ts/connections-read.ts`, `generated/{ts,effect}/index.ts`, `daemon/internal/contracts/{schema.go,schema_test.go,generated.go,schema_bundle.json}`, `daemon/internal/protocol/goal_contract_fixture_test.go`, `contracts/README.md`. |
| Frontend | `frontend/src/services/api.ts` (connections read/mutations, `ServerRefusal`), `app/{application,legacy,router,shell}.tsx|ts`, new `app/session.tsx` (CSRF/operator session context), new `features/connections/{connections.tsx,connections.test.tsx}`, `styles.css`. |
| Browser | rewritten `browser/tests/portal-connections.spec.mjs` (real API + fixture transport), new `browser/tests/portal-classic-resources.spec.mjs` (moved classic resource coverage), new `browser/scripts/run-connection-fixture-tests.ps1`, new `scripts/ci/portal-connection-fixture.exs`, `browser/README.md`. |
| Docs | `docs/plans/2026-11-16-0007d-connection-management-plan.md`, `docs/TODO.md` (one dead-code candidate and three environment/intermittency entries). |
| Control tests | `portal_integrations_test.exs`, `portal_controller_test.exs`, `portal_session_test.exs`, `contract_fixture_manifest_test.exs`. |

## 2. Done when → evidence

| Contract clause | Evidence in this run |
| --- | --- |
| create, inspect, update, test and remove for both providers, real API and browser flows, boundary stated per flow | `portal-connections.spec.mjs` test 1 drives create → row/detail inspect → provider check → capability update → removal for GitHub and asserts the removal against `GET /portal/api/connections`; test 5 drives the same five operations for Azure DevOps. Both run in the harness with the real Phoenix endpoint, the real PostgreSQL database and the canned provider transport; the provider boundary is this receipt's §4. |
| idempotent submission, stale updates and concurrent deletion, including what the UI shows when the server refuses a stale write | test 2: the same normalized submission answers `200` with `disposition: "replayed"` and a conflicting payload keeps `422` with `fields.provider = "already has a connection with this name"`, and the console renders the server's field error and `(validation_failed)`. test 3: an external `PATCH` makes the console's Save answer `(stale)`, the dialog pins the name field disabled and requires closing, and an external `DELETE` makes the console's confirm answer `(not_found)` plus "The server's current state is shown instead." with the row gone. |
| a failed or expired authentication, and a test that fails, produce actionable redacted reasons | test 2 and test 5 check a connection whose account is marked unauthorized: the detail shows `Degraded` and "Provider authentication failed", the receipt shows `(provider_unauthorized)` with "Reauthenticate the provider session on this server…". The spec then asserts no fixture canary appears in any same-origin response body, recorded request header/body, the DOM, `localStorage`/`sessionStorage` or `document.cookie`; the harness additionally scans the control log, its error log and the recorded provider-call log (§4). |
| a capability update visibly resets dependent resource health/sync state, observed through the real API and the database | browser test 4 (UI: receipt "Bound resource health and sync state was reset", the detail shows `Synced` gone and three `Unknown`) plus the direct API+database observation in §5, where the same transaction leaves the resource `unknown/unknown` with `last_synced_at` NULL and `lock_version` 2 → 3. |
| whatever the boundary leaves unverified is reported as unverified | §7 lists the unverified items (live provider credentials and authorization, provider-side mutation, hosted qualification). |
| pre-existing browser acceptance continues to pass on the new route, classic checks retired in the same PR | harness run `10 passed` (both Playwright projects) for the new route; `portal-classic-resources.spec.mjs` `8 passed` (both projects) for the retained resource coverage; the legacy shell specs `30 passed / 1 failed / 9 skipped` with that one failure reproduced as intermittent (two clean re-runs) and recorded in `docs/TODO.md`; removed coverage in §6. |

## 3. Executed checks

| Exact command (working directory) | Exit | Observed |
| --- | ---: | --- |
| `mix format --check-formatted` (`control/`) | 0 | Clean after formatting `test/symmetry_control_web/portal_session_test.exs`; no other file needed changes. |
| `mix compile --force --warnings-as-errors` (`control/`) | 0 | Full recompile of the current revision, zero warnings. |
| `mix test` (`control/`, `POSTGRES_DB=symmetry_control_test`, dedicated database) | 2 | `828/829 passed, 21 skipped`, `Failed: 1 test` — `CommandTest` "provider token commands can keep stderr diagnostics out of stdout": `{:error, {:command_unavailable, "pwsh"}}`. Host has no working `pwsh` (`System.find_executable("pwsh")` → `nil`, `powershell.exe` resolves); the file's other two cases pass. Unrelated to this diff and recorded in `docs/TODO.md`. |
| `mix test` (`control/`, `POSTGRES_DB=symmetry_0007d_test`, the harness database) | 2 | `819/829 passed`, `Failed: 10 tests` — suite-level aggregates see the acceptance rows committed by earlier browser runs (`sync_all_resources/0`, workspace and portal reads). Recorded in `docs/TODO.md`; the suite's own database is the default one. |
| `node --test contracts/scripts/*.test.mjs` (root) | 0 | `tests 32, pass 32, fail 0`. One assertion added by this change pointed at the wrong field (`resources[0].metadata.ratio` instead of the fixture's actual fractional field `connections[0].metadata.latency_ratio`) and was corrected rather than weakened. |
| `node contracts/scripts/check.mjs` (root) | 0 | "Generated contract DTOs are up to date. Validated 16 Draft 7 schemas and 195 fixtures." |
| `node contracts/scripts/check-effect.mjs` (root) | 0 | "Effect decoded all 195 raw fixtures; 191 parsed documents reached Schema validation." |
| `pnpm --offline contracts:typecheck` (root) | 0 | `tsc --project contracts/tsconfig.json`, clean. |
| `pnpm --offline contracts:test` (root) | 0 | 32/32. (`pnpm contracts:check` itself cannot run here: its nested `pnpm` calls try the public registry and fail with `ERR_PNPM_META_FETCH_FAIL`; the four steps it expands to were run directly.) |
| `pnpm --offline --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit`, clean. |
| `pnpm --offline --filter @symmetry/frontend lint` | 0 | `eslint . --max-warnings 0`, zero warnings. |
| `pnpm --offline --filter @symmetry/frontend test` | 0 | `Test Files 5 passed (5)`, `Tests 42 passed (42)` (includes the new `legacy.test.ts` cases for the retired `#connections` anchor). |
| `pnpm --offline --filter @symmetry/frontend build` | 0 | `vite build` 2862 modules + `scripts/stage-assets.mjs` into `control/priv/static/portal`. |
| `go vet ./...` (`daemon/`) | 0 | No findings. |
| `go test ./...` (`daemon/`) | 0 | Every package `ok` (`internal/app` 184.5 s, `internal/workspace` 126.2 s, …); contract and protocol packages unchanged since their earlier green run. |
| `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1` | 0 | `10 passed (42.0 s)` = 5 tests × desktop-chromium + mobile-chromium on the new route; canary scan clean; the script stopped its control plane (port 4021 free afterwards). |
| `node …/@playwright/test/cli.js test browser/tests/portal-classic-resources.spec.mjs --config browser/playwright.config.mjs` | 0 | `8 passed (37.2 s)` (4 tests × 2 projects). |
| `node …/@playwright/test/cli.js test browser/tests/portal.spec.mjs browser/tests/workspace-shell.spec.mjs --config browser/playwright.config.mjs` | 1 | `30 passed / 1 failed / 9 skipped` (3.4 m). The failure is `portal.spec.mjs` "project, resource, work-item, filtering, ordering, and persistence" (desktop): after the detach confirmation the toast still read "Resource updated". The same test alone on the same revision passed twice (`1 passed` 18.2 s and `1 passed` 17.3 s, exit 0 each), so it is recorded as intermittent in `docs/TODO.md`; the classic bundle `control/priv/portal_assets/portal.js` and the resource routes are untouched by this change. |
| `node …/@playwright/test/cli.js test --list --config browser/playwright.config.mjs` | 0 | `Total: 90 tests in 9 files` (declaration count for this revision). |

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

No provider host is contacted. In the final harness run the recorded provider-call log
(`%TEMP%\symmetry-0007d-connections-f4779f97\fixture-calls.jsonl`) contains **30 calls**: 10 to
`api.github.com`, 4 to `dev.azure.com`, 12 `gh` and 4 `az` CLI invocations; every entry records the
call shape only (`"authorization":"bearer:github-fixture"`, `"output":"fixture-canary"`), never a
token value. The harness's canary scan of `control.log`, `control.log.err` and the provider-call log
reported "no fixture credential value in the control logs or the provider-call log"; the control log
had 39 lines and the error log 0 bytes. Independently, the manual fixture server's log (119 lines,
`.tmp/fixture-server.log`) and call log (`.tmp/fixture-calls.jsonl`) contain neither canary. The
browser side is asserted by the spec itself (same-origin response bodies, request headers and bodies,
DOM, web storage, cookies).

The fixture is deliberately quieter than a development server: it logs at `warning` level, and it
terminates `Orchestration.Scheduler` and `Orchestration.Reconciler` after boot (provider access stays
up) because their per-second database traffic contended with the flows under test — a `GET
/portal/api/connections` that answers in 0.48 s over curl stayed unanswered for over 10 s in a browser
under that contention. As a consequence `/healthz` reports those two checks down in the fixture, so
the harness's readiness probe is `GET /portal/login`.

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
and **0** `#connection-table` references. Removed and replaced by the new-route checks: the whole
"form generations and connection deletion remain single-flight" test, the connection form/check
single-flight, focus and partial-failure phases of "provider operations remain single-flight…", and
the classic Azure DevOps Entra ID connection assertions (including its `checkFailure` 502 case). The
resource-binding, synchronization, assignment, project-switch and Azure resource-kind assertions moved
with unchanged intent; one assertion that depended on a deleted connection mock was corrected against
the real server computation (`ReadModel.connection_health/2` reports `degraded` for a degraded bound
resource) and the intentionally failed refresh's 503 console allowance was restored. A hidden
`#resource-table` mutation counter written during the move was removed: no contract clause claims that
property for that surface and the measurement showed the table does re-render behind a background
refresh.

## 7. Unresolved limitations (not verified here)

- **Live provider credentials and authorization**: unverified. No real `gh` or `az` session and no
  real provider host was used; every provider answer came from the fixture transport. A live check on
  a real account is a separate authorized outcome.
- **Provider-side resource mutation**: out of scope by the contract; nothing in this run wrote to a
  provider.
- **Hosted qualification**: no CI run, deployment or hosted environment was exercised; all evidence is
  local to this workstation (Windows, containerized PostgreSQL on `127.0.0.1:55440`).
- **Suite state**: `mix test` reports one environment-limited failure (needs a working `pwsh`) and,
  when pointed at the harness database, ten pollution-caused failures; both are recorded in
  `docs/TODO.md` with their observed counts.
- **Intermittent browser case**: `portal.spec.mjs`'s detach-toast assertion failed once under host
  load and passed twice alone; recorded, not explained.
- **pnpm supply-chain verification**: cannot complete on this host (the public registry is
  unreachable); `--offline` makes it fail in about a second and print its `node_modules` warning
  before the script runs. `node_modules` was installed for this exact lockfile
  (`pnpm-lock.yaml` sha256 `2a0c9ef07d94f250…`, unchanged from the base).
- **Owner acceptance**: not issued. The receipt states the result as produced and awaiting the
  owner's verdict; the model does not rate its own rendering and does not sign for the owner.
- **Measurement claims**: latency numbers quoted here are single observations on a loaded development
  host (the API answers were measured at 0.36–0.48 s per request over curl); they are context for the
  fixture changes, not a performance claim.
