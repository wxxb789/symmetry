# 0007d connection management — evidence receipt

Status: **produced and awaiting the owner's verdict**. Every number below names the subject it was
measured on; no commit, push or history rewrite was performed, and the tree is left for
verification. Owner acceptance — the owner reviewing the rendered connection flows on real data
plus this boundary statement against the fixed design's connection page and truthful-status rules —
is explicitly after this run and not part of the goal's Done when, so this receipt states the result
as produced and does not rate its own rendering or sign for the owner.

Binding for this round (fixing the round-2 review): base `259ea7de7c2c040dd60507f511c03f26ed6452ef`;
the increment is committed as `86b368a` (feature) with the first fix round
`bbc9b6fe876e145b5a434ebcad8ad783428d81a7` on top; this round's changes are the working tree on top
of `bbc9b6f` — 6 modified paths (`git status --porcelain -uall`, counting this receipt; no untracked
files). **Source subject for this round**:
`sha256 6976f472a4b3e61d707d19d25866ee01c5c392bd051ceb2fd7773e2f0e4c4e61`, computed over the 1009
tracked and untracked non-ignored repository paths with `docs/**` excluded, so a receipt edit cannot
invalidate its own binding:

```sh
git ls-files --cached --others --exclude-standard -z | grep -zv '^docs/' | sort -z | xargs -0 sha256sum | sha256sum
```

`control/priv/static/portal/` and `.tmp/**` are gitignored and therefore excluded by construction;
`pnpm-lock.yaml` and `package.json` are byte-identical between `259ea7d` and `bbc9b6f`
(`pnpm-lock.yaml` sha256 `2a0c9ef07d94f250…`). The original run's binding was base `4e60b9d…` /
increment `709570f…`; that commit was rebased onto `259ea7d` as `86b368a`, and numbers below that
were not re-measured keep the revision they were measured on, named with the number.
`control/priv/static/portal/` is gitignored build output; the browser runs need it staged
(`pnpm --filter @symmetry/frontend build` does that, and the build was run before the browser runs).

Plan: `docs/plans/2026-11-16-0007d-connection-management-plan.md` (§10 records every deviation and
finding taken during implementation).

## 1. What this increment contains

| Group | Paths |
| --- | --- |
| Backend | `control/lib/symmetry_control/integrations.ex` (provider list, bound-resource map, create replay), `integrations/schemas.ex`, `workspaces/read_model.ex` (`connections/0`, bound resources in the workspace projection), `symmetry_control_web/portal_json.ex` (`connections_read/1`, `connection/2`, `connection_resource/1`), `portal_api_controller.ex` (new read action, `disposition` receipts, check failure carries the connection), `router.ex` (`GET /portal/api/connections`, `GET /portal/connections` SPA route), `portal_session.ex` (return-path allowlist for the new route and `connectionId`), `goals/contract_validation.ex`. |
| Contracts | `contracts/v1/connections-read.schema.json`, five new fixture files — 3 valid (`basic`, `empty`, `unknown-values`) and 2 invalid (`missing-connections`, `invalid-version-type`) —, `fixtures/manifest.json`, `scripts/{generate,check,check-effect,boundary.test}.mjs`, `ts/index.ts`, `generated/ts/connections-read.ts`, `generated/{ts,effect}/index.ts`, `daemon/internal/contracts/{schema.go,schema_test.go,generated.go,schema_bundle.json}`, `daemon/internal/protocol/goal_contract_fixture_test.go`, `contracts/README.md`. The manifest's current inventory is **201 entries** (129 invalid, 72 valid) at this round's subject; the original run measured 195 at `709570f`, and the six entries added since are the `goal-command` fixtures that arrived with the 0009 contract amendments when this work was rebased onto `259ea7d` (this increment itself adds the five connection fixtures listed here). |
| Frontend | `frontend/src/services/api.ts` (connections read/mutations, `ServerRefusal`), `app/{application,legacy,router,shell}.tsx|ts`, new `app/session.tsx` (CSRF/operator session context), new `features/connections/{connections.tsx,connections.test.tsx}`, `styles.css`. |
| Browser | rewritten `browser/tests/portal-connections.spec.mjs` (real API + fixture transport), new `browser/tests/portal-classic-resources.spec.mjs` (moved classic resource coverage), new `browser/scripts/run-connection-fixture-tests.ps1`, new `scripts/ci/portal-connection-fixture.exs`, `browser/README.md`. |
| Docs | `docs/plans/2026-11-16-0007d-connection-management-plan.md`, `docs/TODO.md` (two dead-code candidates — this round recorded the `Connection.statuses/0` accessor from this increment — and three environment/intermittency entries; the first fix round added a fourth for the load-sensitive legacy cases). |
| Control tests | `portal_integrations_test.exs`, `portal_controller_test.exs`, `portal_session_test.exs`, `contract_fixture_manifest_test.exs`. |

## 2. Done when → evidence

| Contract clause | Evidence in this run |
| --- | --- |
| create, inspect, update, test and remove for both providers, real API and browser flows, boundary stated per flow | `portal-connections.spec.mjs` test 1 drives create → row/detail inspect → provider check → capability update → removal for GitHub and asserts the removal against `GET /portal/api/connections`; test 5 drives the same five operations for Azure DevOps. Both run in the harness with the real Phoenix endpoint, the real PostgreSQL database and the canned provider transport; the provider boundary is this receipt's §4. |
| idempotent submission, stale updates and concurrent deletion, including what the UI shows when the server refuses a stale write | test 2: the same normalized submission answers `200` with `disposition: "replayed"` and a conflicting payload keeps `422` with `fields.provider = "already has a connection with this name"`, and the console renders the server's field error and `(validation_failed)`. test 3: an external `PATCH` makes the console's Save answer `(stale)`, the dialog pins the name field disabled and requires closing, and an external `DELETE` makes the console's confirm answer `(not_found)` plus "The server's current state is shown instead." with the row gone. The editor generation races are covered by `connections.test.tsx`: a late response for a closed editor never closes or navigates the open one, and a draft submits against the version it was edited from, so a background refresh still answers `(stale)` (both failed before the fix and pass after, `.tmp/evidence/red-frontend-editor-races.txt`). |
| a failed or expired authentication, and a test that fails, produce actionable redacted reasons | test 2 and test 5 check a connection whose account is marked unauthorized: the detail shows `Degraded` and "Provider authentication failed"; test 2 additionally asserts the receipt text `(provider_unauthorized)` with "Reauthenticate the provider session on this server…", test 5 asserts the detail projection only. The spec then asserts no fixture canary appears in any same-origin response body or response header (an unreadable body, an empty header list or an absent response record fails the test; redirects are scanned through their `Location` header; each `Set-Cookie` is scanned), recorded request headers through `allHeaders()` with explicit cookie/CSRF presence checks, request bodies, the DOM, `localStorage`/`sessionStorage`, `document.cookie`, the cookie jar sampled at every same-origin response and at the end (HttpOnly state is only visible in the jar and in the `Set-Cookie` headers), and the console and page errors; the harness additionally scans the control log, its error log and the recorded provider-call log and fails on a missing scan file or an empty provider-call log (§4). The canary-proof runs behind these claims are in §4. |
| a capability update visibly resets dependent resource health/sync state, observed through the real API and the database | browser test 4 (UI: receipt "Bound resource health and sync state was reset", the detail shows `Synced` gone and three `Unknown`) plus the direct API+database observation in §5, where the same transaction leaves the resource `unknown/unknown` with `last_synced_at` NULL and `lock_version` 2 → 3. |
| whatever the boundary leaves unverified is reported as unverified | §7 lists the unverified items (live provider credentials and authorization, provider-side mutation, hosted qualification). |
| pre-existing browser acceptance continues to pass on the new route, classic checks retired in the same PR | harness run `10 passed` (1.7 m, both Playwright projects) for the new route; `portal-classic-resources.spec.mjs` `8 passed` (41.4 s, both projects) for the retained resource coverage; the legacy shell specs `31 passed / 2 failed / 7 skipped` (6.2 m) where both failures are `portal.spec.mjs` desktop timeouts that pass alone on the same revision when the host is quiet (recorded as load-sensitive in `docs/TODO.md`); removed coverage in §6. |

## 3. Executed checks

Rows marked *this round* were measured on the source subject above; rows marked *not re-run this
round* were not re-measured and keep the revision named in the row. Earlier numbers are superseded.

| Exact command (working directory) | Exit | Observed |
| --- | ---: | --- |
| `mix format --check-formatted` (`control/`) | 0 | *This round*: clean on the current tree. |
| `mix compile --force --warnings-as-errors` (`control/`) | 0 | *This round*: full recompile (61 files), zero warnings. |
| `mix test` (`control/`, `POSTGRES_DB=symmetry_control_test`, dedicated database) | 2 | *This round*: `844/845 passed, 21 skipped`, `Failed: 1` — the pre-recorded environment-limited `CommandTest` "provider token commands can keep stderr diagnostics out of stdout" (`test/symmetry_control/integrations/command_test.exs:25`, `{:error, {:command_unavailable, "pwsh"}}`, `2/3 passed` alone); unrelated to the 0007d diff. The original receipt's `830/831` was the pre-rebase `709570f` subject. |
| `mix test test/symmetry_control_web/controllers/portal_integrations_test.exs` (`control/`, same database) | 0 | *Not re-run this round* (no control path changed; last at `bbc9b6f`): `14 passed`; the concurrent-create test repeated under seeds 0–3 (`14 passed` each). |
| `mix test` (`control/`, `POSTGRES_DB=symmetry_0007d_test`, the harness database) | not re-run | The harness recreates and migrates that database itself; the recorded pollution limitation is unchanged (not re-measured this round either). |
| `node --test contracts/scripts/*.test.mjs` (root) | 0 | *This round*: `tests 32, pass 32, fail 0, skipped 0`. |
| `node contracts/scripts/check.mjs` (root) | 0 | *This round*: "Generated contract DTOs are up to date. Validated 16 Draft 7 schemas and 201 fixtures." (195 at `709570f`; see §1 for the inventory scope.) |
| `node contracts/scripts/check-effect.mjs` (root) | 0 | *This round*: "Effect decoded all 201 raw fixtures; 197 parsed documents reached Schema validation." (195/191 at `709570f`.) |
| `pnpm --offline contracts:typecheck` (root) | 0 | *This round*: `tsc --project contracts/tsconfig.json`, clean. |
| `pnpm --offline --filter @symmetry/frontend typecheck` | 0 | *This round*: `tsc --noEmit`, clean. |
| `pnpm --offline --filter @symmetry/frontend lint` | 0 | *This round*: `eslint . --max-warnings 0`, clean (the first fix round had removed a ref-read-during-render error in the first editor-identity cut). |
| `pnpm --offline --filter @symmetry/frontend test` | 0 | *This round*: `Test Files 5 passed (5)`, `Tests 55 passed (55)` — was 48. The seven direction tests added or strengthened this round failed on the pre-fix `connections.tsx` (`7 failed / 48 passed`, `.tmp/evidence/red-round3-component-tests.txt`) and pass here (`.tmp/evidence/green-round3-component-tests.txt`): the removal version fence, the exclusion direction that changed, the four late-callback/late-navigation ownership cases (including leaving the page mid-save) and both dialog focus restorations. The eighth added test (the delete-pending exclusion direction) also passes on the pre-fix source because `remove.isPending` already held that direction, and now guards it. |
| `pnpm --offline --filter @symmetry/frontend build` | 0 | *This round*: `vite build` + `scripts/stage-assets.mjs` into `control/priv/static/portal`; run before this round's browser runs. |
| `go vet ./...` / `go test ./...` (`daemon/`) | not re-run | No daemon path is touched by this diff; the earlier green run stands for the unchanged packages. *Not re-run this round either.* |
| `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1` | 0 | *This round*: `10 passed (1.7 m)` = 5 tests × desktop-chromium + mobile-chromium on the new route with the real topology and the identity handshake; canary scan clean; its own process tree stopped and port 4021 free afterwards (the first fix round's run also measured 1.7 m; this round's five runs reported 58.1 s – 1.7 min of Playwright time under varying host load). Negative cases from the first fix round, each failing closed, not re-run this round: the spec without the harness environment fails at collection (exit 1); a fabricated identity file plus a live foreign target refuses the login before any provider check (exit 1); a `-PortalUrl`/`-Port` mismatch refuses immediately (exit 1); an occupied Docker-forwarded port refuses immediately (exit 1). The three canary-proof runs for the new response-header and transient-HttpOnly-cookie scans are in §4. |
| `pnpm --offline --filter symmetry-browser-acceptance test tests/portal-classic-resources.spec.mjs` with `SYMMETRY_PORTAL_URL=http://127.0.0.1:4022` | 0 | *Not re-run this round*: `8 passed (41.4 s)` (4 tests × 2 projects) at `bbc9b6f` against a fixture server started from that revision. |
| `… test tests/portal.spec.mjs tests/workspace-shell.spec.mjs` with `SYMMETRY_PORTAL_URL=http://127.0.0.1:4022 SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4023` | 1 | *Not re-run this round* (first fix round's run at `bbc9b6f`, no legacy path touched): `31 passed / 2 failed / 7 skipped` (6.2 m) against two fixture servers sharing the fixture database (the expiry URL makes the true-TTL cases run instead of skip). Both failures are `portal.spec.mjs` desktop timeouts: "detail output encodes untrusted statuses and rejects unsafe links" (its project-create toast never appeared within 10 s) and "two browser contexts reject a stale project update" (test timeout of 60 s). Neither surface is touched by the 0007d diff, and both pass alone on the same revision when the host is quiet (the stale-update case `ok` in 5.0 s under a longer budget; the detail case `ok` in its isolated retry), so they are recorded as load-sensitive in `docs/TODO.md`. |
| `… test --list` | 0 | *This round*: `Total: 90 tests in 9 files` (with the harness environment present; without it the connection spec fails at collection by design). |

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

Negative evidence from the first fix round: the spec without the harness environment fails at
collection (`SYMMETRY_OPERATOR_TOKEN is required`); pointed at a live Control instance on another
port with a fabricated identity file and a random token, the login is refused and the test fails
before any provider check; the harness with a mismatched `-PortalUrl`/`-Port` refuses immediately;
the harness pointed at an occupied port (a Docker-forwarded port this shell cannot enumerate as a
listener) refuses immediately before it starts anything, so the foreign server is never contacted.

Canary-proof runs from this round (each through the same harness, real API and fixture transport):

- the pre-fix spec (`bbc9b6f`) with **both** injected canaries — an `x-canary-proof` response header
  on the session response and a transient HttpOnly `canary_probe` cookie set and cleared by two
  responses whose requests were already sent — reported `10 passed (58.1 s)`, exit 0
  (`.tmp/evidence/pre-fix-canary-injections-pass.txt`): the pre-fix scan missed both surfaces;
- this round's spec with the response-header canary: `2 failed / 8 passed`, exit 1, failing exactly
  at `response headers for /portal/api/session must not contain a provider credential`
  (`.tmp/evidence/red-canary-response-header.txt`);
- this round's spec with the transient HttpOnly cookie: `2 failed / 8 passed`, exit 1, failing at
  `cookie canary_probe from a jar sample must not contain a provider credential`
  (`.tmp/evidence/red-canary-transient-cookie.txt`);
- the clean spec: `10 passed (1.7 m)`, exit 0 (`.tmp/evidence/harness-run-round3.txt`), the run
  whose artifacts §4's numbers below are taken from.

The pre-fix comparison run carried the `bbc9b6f` spec text plus the removal-dialog wait fix
described below; that fix only changes when the row is counted, not what the canary scan sees. The
canary work also exposed that fix as a real assertion defect: after "Remove connection" the row
count could pass while the removal dialog was still open, because an open Radix modal marks the
rest of the page `aria-hidden` and a role-based locator cannot see the row; the assertion then
raced the delete still in flight (observed as a mobile failure when the DELETE took 346 ms). The
spec now waits for the removal dialog to close before counting the row in both successful-removal
flows.

The fixture only replaces the transport, so it logs at `warning` level to keep the record readable;
the boot-time SQL debug lines stop once the fixture configures the level.

With the real topology the harness run takes about 1.0–1.7 min of Playwright time for the 10
connection tests (this round's runs: 58.1 s to 1.7 min under varying host load; the first fix round measured 1.7 m; the original
receipt's 42 s was measured with the scheduler and reconciler stopped), and every flow still passes
inside the configured assertion budget: the earlier "multi-second reads under contention"
observation was not reproduced under this round's conditions.

The harness stops only the process tree it started plus the listener PID it recorded after the
identity handshake; it never kills an unowned process on the port. After the final run port 4021 was
free.

In this round's final harness run the recorded provider-call log
(`.tmp/evidence/harness-run-round3/fixture-calls.jsonl`, copied from the run's `%TEMP%` directory;
the first fix round's copy is `.tmp/evidence/harness-run-final/`) contains **30 calls**: 10 to
`api.github.com`, 4 to `dev.azure.com`, 12 `gh` and 4 `az` CLI invocations; every entry records the
call shape only (`"authorization":"bearer:github-fixture"`, `"output":"fixture-canary"`), never a
token value. The harness's canary scan of `control.log`, `control.log.err` and the provider-call log
reported "no fixture credential value in the control logs or the provider-call log"; the control log
had 37 lines and the error log 0 bytes. The browser side is asserted by the spec itself: same-origin
response bodies and every response header (each `Set-Cookie` included — the surface that introduces
an HttpOnly cookie; an unreadable body or an empty header list fails the test, and redirects are
scanned through their `Location` header), request headers through `allHeaders()` with explicit
presence checks for the cookie and CSRF headers of `/portal/api/` requests, request bodies, the DOM,
web storage, `document.cookie` (which cannot see HttpOnly state), the cookie jar sampled at every
same-origin response and once more at the end (an empty final jar or a run without jar samples
fails the test), console messages and page errors, and the scan fails closed when response records
are absent. A missing scan file or an empty provider-call log fails the harness run instead of
passing the scan blind.

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
| PATCH response | — | `200`; the observation script's jq projection of the wire body shows `disposition: "updated"` and the reset resource `unknown / unknown / last_synced_at null` (the wire body is `{disposition, connection: {…}}`, and the script reads `.connection.capabilities`) |

## 6. Retired classic coverage

`browser/tests/portal-connections.spec.mjs` in the base contained 5 tests and 10 references to the
classic `#connection-table`; the moved `browser/tests/portal-classic-resources.spec.mjs` keeps 4 tests
and **0** `#connection-table` references. The classic connection-surface tests retired with the classic
UI; their intent is migrated to the new route as follows, and no assertion was deleted without a
replacement:

| Retired classic assertion | New-route coverage |
| --- | --- |
| pending form generation ("form generations and connection deletion remain single-flight": a second editor opened while the first create is pending) | `frontend/src/features/connections/connections.test.tsx` "a late create response never closes or hijacks a newer editor" plus the existing "submits a new connection once while pending and reconciles to the server list" |
| connection check/update/delete mutual exclusion while one is pending | `connections.test.tsx` "keeps provider check, update and removal mutually exclusive while one is pending" (check pending blocks edit and remove), "keeps check and removal blocked while an editor save is pending" (the update holds the surface after its dialog closes) and "keeps check and editing blocked while a removal is pending" (the delete direction; it also passed on the pre-fix source, where `remove.isPending` already held it) |
| editor/removal conflict during refresh (a background refresh while the dialog is open) | `connections.test.tsx` "submits a draft against the version it was edited from after a background refresh" and "confirms a removal against the version the dialog opened with, even after a background refresh" (both answer with server truth instead of overwriting or deleting the newer revision) |
| late mutation callbacks act only on the subject the operator is looking at, on this page or after leaving it (the classic `ownsMutation` guard) | `connections.test.tsx` "drops a provider check receipt that arrives after the operator selected another connection", "does not clear the newly selected connection when a removal settles late", "does not navigate back when the operator selects another connection during the save's re-read" and "does not pull the operator back after they left the page while a save was pending" (all four failed before the fix and pass after) |
| connection focus retention across a refresh, extended to the new dialogs | `connections.test.tsx` "keeps keyboard focus on the connection row across a background refresh" and "returns keyboard focus to the invoking control when either dialog closes" (Escape returns focus to "New connection" and to "Remove") |
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
- **Suite state**: the original receipt's `mix test` reported `830/831 passed` at `709570f`; this
  round measures `844/845 passed` with the same one environment-limited failure (needs a working
  `pwsh`); the harness-database pollution limitation from the original run was not re-measured (the
  harness recreates that database itself). Both are recorded in `docs/TODO.md`.
- **Gates not re-run in this round**: the daemon `go vet`/`go test` gates and the classic/legacy
  browser specs were not re-measured because no daemon or legacy path changed (the round-3 diff is
  `connections.tsx`, `connections.test.tsx`, `portal-connections.spec.mjs` and docs). Their numbers
  stay bound to `bbc9b6f` as named in §3; the control gates and the browser harness ran the real
  application end to end in this round.
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
