# 0007d connection management — evidence receipt

Status: **produced and awaiting the owner's verdict**. Every number below names the subject it was
measured on. The increments are committed and pushed to the PR branch — pre-rebase `86b368a`,
`bbc9b6f`, `e67a925`, `43c1004`, `fc3cf8c`, `0b868fb`, `0f854fd`, rebased onto `7a92ca4` as
`0b2e16f`, `28997fb`, `4c0b0f0`, `381775c`, `3d10b91`, `1974260`, `1250976` (§9) — so CI and the PR bot review those bytes; earlier
revisions of this receipt described an uncommitted tree. Owner acceptance — the owner reviewing the rendered connection flows on real data
plus this boundary statement against the fixed design's connection page and truthful-status rules —
is explicitly after this run and not part of the goal's Done when, so this receipt states the result
as produced and does not rate its own rendering or sign for the owner.

Binding for round 5 (the rebase onto main, where the merged 0007b met this branch — see §9): base
`259ea7de7c2c040dd60507f511c03f26ed6452ef`; the increment is committed as `86b368a` (feature) with
fix rounds `bbc9b6fe876e145b5a434ebcad8ad783428d81a7`, `e67a925b030c470c1fa832b2553bef296136e56e`,
`43c10042261f12b6c9ad3706c4357b752bd0f9eb` (round 3), `fc3cf8cd65f6dee8da92599d113a17667aed80f2`
(the PR-bot round), `0b868fbcadfc32b5a7031a20041ffbefd8343c4f` and
`0f854fd720b3f1278944182687a08926831b46af` (rounds 4 and 4-b) on top, all rebased onto `7a92ca4`.
**Source subject for round 5**:
`sha256 f0681b1873616e2bb8bdb14a6c271342023e6037a74818e35a8cf01fc659d5e4`, computed over the same
kind of path set — 1049 tracked and untracked non-ignored repository paths with `docs/**` excluded
(the union adds 0007b's files to round 4's 1010; round 4 measured
`sha256 fbebda1e2106010966fd18190c0cd09d01fdd60dc7f7cc4d7bc0d15e773c3112` on 1010 paths and round 3
`0f09025f2cf6c1b84cfda41c95c4f11712e1a994202458515731116082afa4e3` on 1009, kept as historical), so a
receipt edit cannot invalidate its own binding:

```sh
git ls-files --cached --others --exclude-standard -z | grep -zv '^docs/' | sort -z | xargs -0 sha256sum | sha256sum
```

`control/priv/static/portal/` and `.tmp/**` are gitignored and therefore excluded by construction;
`pnpm-lock.yaml` and `package.json` are byte-identical between `259ea7d` and `bbc9b6f`
(`pnpm-lock.yaml` sha256 `2a0c9ef07d94f250…`). The original run's binding was base `4e60b9d…` /
increment `709570f…`; that commit was rebased onto `259ea7d` as `86b368a`, and numbers below that
were not re-measured keep the revision they were measured on, named with the number. The earlier
rounds' source subjects `4022d057a0e48a178ae70d78f6c732f824daec6f7a5faf3a78983fd684aaa909` (the
round-3 fix, committed as `43c1004`) and
`6976f472a4b3e61d707d19d25866ee01c5c392bd051ceb2fd7773e2f0e4c4e61` (the round-3 review tree) named
the same 1009 paths on their trees: each digest, like this round's, covers only the bytes of its own
round, so an observation made earlier keeps the revision it was measured on instead of being
re-bound by this round's bytes.
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
| idempotent submission, stale updates and concurrent deletion, including what the UI shows when the server refuses a stale write | test 2: the same normalized submission answers `200` with `disposition: "replayed"` and a conflicting payload keeps `422` with `fields.provider = "already has a connection with this name"`, and the console renders the server's field error and `(validation_failed)`. `portal_integrations_test.exs` adds the two replay edges this round: a submission that omits `capabilities` replays the applied row (the omitted field stores the schema default; it failed with `422` before the fix, `.tmp/evidence/red-control-replay-omitted-capabilities.txt`), and the same `[provider, name]` with a different `account_ref` keeps the `422`. test 3: an external `PATCH` makes the console's Save answer `(stale)`, the dialog pins the name field disabled and requires closing, and an external `DELETE` makes the console's confirm answer `(not_found)` plus "The server's current state is shown instead." with the row gone. The editor generation races are covered by `connections.test.tsx`: a late response for a closed editor never closes or navigates the open one, and a draft submits against the version it was edited from, so a background refresh still answers `(stale)` (both failed before the fix and pass after, `.tmp/evidence/red-frontend-editor-races.txt`); this round adds the removed-while-editing case (the dialog closes with a notice instead of losing the draft silently and reopening on the next selection) and the name-only edit notice (no capability reset is claimed when none happened). |
| a failed or expired authentication, and a test that fails, produce actionable redacted reasons | test 2 and test 5 check a connection whose account is marked unauthorized: the detail shows `Degraded` and "Provider authentication failed"; test 2 additionally asserts the receipt text `(provider_unauthorized)` with "Reauthenticate the provider session on this server…", test 5 asserts the detail projection only. The spec then asserts no fixture canary appears in any same-origin response body or response header (an unreadable body, an empty header list or an absent response record fails the test; redirects are scanned through their `Location` header; each `Set-Cookie` is scanned), recorded request headers through `allHeaders()` with explicit cookie/CSRF presence checks, request bodies, the DOM, `localStorage`/`sessionStorage`, `document.cookie`, the cookie jar sampled at every same-origin response and at the end (HttpOnly state is only visible in the jar and in the `Set-Cookie` headers), and the console and page errors; the harness additionally scans the control log, its error log and the recorded provider-call log and fails on a missing scan file or an empty provider-call log (§4). The canary-proof runs behind these claims are in §4. |
| a capability update visibly resets dependent resource health/sync state, observed through the real API and the database | browser test 4 (UI: receipt "Bound resource health and sync state was reset", the detail shows `Synced` gone and three `Unknown`) plus the direct API+database observation in §5 — measured on the original run's subject `709570f` and not re-run since, so this round's digest does not re-bind it — where the same transaction left the resource `unknown/unknown` with `last_synced_at` NULL and `lock_version` 2 → 3. |
| whatever the boundary leaves unverified is reported as unverified | §7 lists the unverified items (live provider credentials and authorization, provider-side mutation, hosted qualification). |
| pre-existing browser acceptance continues to pass on the new route, classic checks retired in the same PR | harness run `10 passed` (45.8 s this round, §3; 51.2 s on the round-3-fix tree and 1.7 m on the round-3 tree, both Playwright projects) for the new route; `portal-classic-resources.spec.mjs` `8 passed` (41.4 s, both projects) for the retained resource coverage; the legacy shell specs `31 passed / 2 failed / 7 skipped` (6.2 m) where both failures are `portal.spec.mjs` desktop timeouts that pass alone on the same revision when the host is quiet (recorded as load-sensitive in `docs/TODO.md`); removed coverage in §6. |

## 3. Executed checks

Rows marked *this round* were measured on the source subject above; rows marked *not re-run this
round* were not re-measured and keep the revision named in the row. Earlier numbers are superseded.

| Exact command (working directory) | Exit | Observed |
| --- | ---: | --- |
| `mix format --check-formatted` (`control/`) | 0 | *This round*: clean after formatting `integrations.ex` and `portal_integrations_test.exs`. |
| `mix compile --force --warnings-as-errors` (`control/`) | 0 | *This round*: full recompile (61 files), zero warnings. |
| `mix test` (`control/`, `POSTGRES_DB=symmetry_control_test`, dedicated database) | 2 | *This round*: `845/846 passed, 21 skipped`, `Failed: 1` — the same pre-recorded environment-limited `CommandTest` "provider token commands can keep stderr diagnostics out of stdout" (`test/symmetry_control/integrations/command_test.exs:25`, `{:error, {:command_unavailable, "pwsh"}}`, `2/3 passed` alone); unrelated to the 0007d diff. Log `.tmp/evidence/mix-test-fix.txt`. The `e67a925` tree measured `844/845`; the original receipt's `830/831` was the pre-rebase `709570f` subject. |
| `mix test test/symmetry_control_web/controllers/portal_integrations_test.exs` (`control/`, same database) | 0 | *This round*: `15 passed`; the omitted-capabilities replay test failed on the pre-fix `integrations.ex` (`14/15`, `422` instead of the replayed `200`, `.tmp/evidence/red-control-replay-omitted-capabilities.txt`). The concurrent-create test from the earlier round still passes inside the file. |
| `mix test` (`control/`, `POSTGRES_DB=symmetry_0007d_test`, the harness database) | not re-run | The harness recreates and migrates that database itself; the recorded pollution limitation is unchanged (not re-measured this round either). |
| `node --test contracts/scripts/*.test.mjs` (root) | 0 | *This round*: `tests 32, pass 32, fail 0, skipped 0` (`.tmp/evidence/contracts-node-test-fix2.txt`). The connections-read presentation-number assertion now decodes the fixture through `ConnectionsRead.decodeJson` instead of asserting the raw JSON literal (`.tmp/evidence/probe-a14.txt`). |
| `node contracts/scripts/check.mjs` (root) | 0 | *This round*: "Generated contract DTOs are up to date. Validated 16 Draft 7 schemas and 201 fixtures." (195 at `709570f`; see §1 for the inventory scope.) The connection `version` fields now `$ref` the common `Revision` (minimum 1, maximum 9007199254740991), and the regenerated DTOs/bundle carry it (`.tmp/evidence/contracts-check-fix2.txt`). |
| `node contracts/scripts/check-effect.mjs` (root) | 0 | *This round*: "Effect decoded all 201 raw fixtures; 197 parsed documents reached Schema validation." (195/191 at `709570f`; `.tmp/evidence/contracts-check-effect-fix2.txt`.) |
| `pnpm --offline contracts:typecheck` (root) | 0 | *This round*: `tsc --project contracts/tsconfig.json`, clean (regenerated `Revision` alias included; `.tmp/evidence/contracts-typecheck-fix2.txt`). |
| `pnpm --offline --filter @symmetry/frontend typecheck` | 0 | *This round*: `tsc --noEmit`, clean (`.tmp/evidence/frontend-typecheck-fix2.txt`). |
| `pnpm --offline --filter @symmetry/frontend lint` | 0 | *This round*: `eslint . --max-warnings 0`, clean (`.tmp/evidence/frontend-lint-fix2.txt`). |
| `pnpm --offline --filter @symmetry/frontend test` | 0 | *PR-bot round `fc3cf8c`; superseded by round 4 — see §8*: `Test Files 5 passed (5)`, `Tests 66 passed (66)` — was 61 at `43c1004`. Five tests were added; four of them failed on the pre-fix sources (`1 failed / 5 passed` for the mutation-401 expiry, `.tmp/evidence/red-p2a-mutation-expiry.txt`; `3 failed / 28 passed` for the catalog gate, the removed-while-editing editor and the name-only notice, `.tmp/evidence/red-frontend-still-open.txt`) and pass here (`.tmp/evidence/green-frontend-full-fix2.txt`); the fifth (the replayed-create notice) covers an existing branch and passes both sides. |
| `pnpm --offline --filter @symmetry/frontend build` | 0 | *This round*: `vite build` + `scripts/stage-assets.mjs` into `control/priv/static/portal`; run before the browser runs, and again after the regenerated contract graph. |
| `go vet ./...` / `go test ./...` (`daemon/`) | 0 / 0 | *This round*: all packages `ok`; `internal/contracts` adds the connections-read version-bound rejection (it failed with the pre-change schema, `.tmp/evidence/red-go-version-bound.txt`) and `internal/protocol` decodes the connections fixtures into the generated DTO and asserts the nullable fields (`.tmp/evidence/green-go-protocol-dto.txt`). Log `.tmp/evidence/go-gates-fix.txt`. |
| `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1` | 0 | *This round*: `10 passed (45.8 s)` = 5 tests × desktop-chromium + mobile-chromium on the new route with the real topology and the identity handshake; canary scan clean; the harness's own spec-summary assertion reported `10 passed` and no skips; the fixture started with `--no-start` (transports installed before the application boots); its own process tree stopped and port 4021 free afterwards; the recorded run has 30 provider calls (10 `api.github.com`, 4 `dev.azure.com`, 12 `gh`, 4 `az`), a 36-line control log and a 0-byte error log. Log `.tmp/evidence/harness-run-fix3.txt`, provider-call and spec logs `.tmp/evidence/harness-run-fix3/`. Negative evidence this round: the transport bearer/method sensitivity run (`4 passed` → exit 1, `.tmp/evidence/red-transport-bearer-sensitivity.txt`; the fixture-call log carries the `fixture_unexpected_request` rejections, `.tmp/evidence/harness-red-transport-sensitivity/fixture-calls.jsonl`), plus the collection and summary guards in §4. From the first fix round, each failing closed and not re-run this round: a fabricated identity file plus a live foreign target refuses the login before any provider check (exit 1); a `-PortalUrl`/`-Port` mismatch refuses immediately (exit 1); an occupied Docker-forwarded port refuses immediately (exit 1). The three canary-proof runs for the response-header and transient-HttpOnly-cookie scans are in §4. |
| `pnpm --offline --filter symmetry-browser-acceptance test tests/portal-classic-resources.spec.mjs` with `SYMMETRY_PORTAL_URL=http://127.0.0.1:4022` | 0 | *Not re-run this round*: `8 passed (41.4 s)` (4 tests × 2 projects) at `bbc9b6f` against a fixture server started from that revision. |
| `… test tests/portal.spec.mjs tests/workspace-shell.spec.mjs` with `SYMMETRY_PORTAL_URL=http://127.0.0.1:4022 SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4023` | 1 | *Not re-run this round* (first fix round's run at `bbc9b6f`, no legacy path touched): `31 passed / 2 failed / 7 skipped` (6.2 m) against two fixture servers sharing the fixture database (the expiry URL makes the true-TTL cases run instead of skip). Both failures are `portal.spec.mjs` desktop timeouts: "detail output encodes untrusted statuses and rejects unsafe links" (its project-create toast never appeared within 10 s) and "two browser contexts reject a stale project update" (test timeout of 60 s). Neither surface is touched by the 0007d diff, and both pass alone on the same revision when the host is quiet (the stale-update case `ok` in 5.0 s under a longer budget; the detail case `ok` in its isolated retry), so they are recorded as load-sensitive in `docs/TODO.md`. |
| `… test --list` | 0 | *This round*: `Total: 90 tests in 9 files` without the harness environment — the connection spec skips at collection (`.tmp/evidence/spec-list-without-env.txt`) and a partially configured environment still fails it (`.tmp/evidence/spec-list-partial-env.txt`). |

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

- `:integration_http_client` answers canned provider documents for a `GET` to `api.github.com`
  (`/user`, `/user/memberships/orgs/:account`, `/repos/:owner/:repository`) carrying that
  provider's fixture token, and for a `GET` to `dev.azure.com` (`/:organization/…`) carrying the
  Azure fixture token, returning `401` for accounts in `SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS`;
  any other host, method or bearer is refused as `fixture_unexpected_request` instead of being
  answered by a blanket canned document;
- `:integration_command_runner` answers the local `gh`/`az` session commands with fixture tokens.

No provider host is contacted. The fixture keeps the application's real supervision topology: the
orchestration scheduler and reconciler stay up and provider access serves provider actions, so
`/healthz` reports the real database, scheduler, reconciler and provider access. Nothing stops or
replaces an application child, and the harness treats `/healthz` answering `ok` as a readiness
condition. The harness starts the fixture with `mix run --no-start`: the script installs both
transports and only then starts the application (`Application.ensure_all_started(:symmetry_control)`),
so the boot-time provider recovery cannot reach a real HTTP client or CLI session. The run fails
closed before any browser flow:

- the harness refuses a `-PortalUrl` whose port is not the fixture port it starts, and refuses a
  port that already accepts connections, before it starts anything;
- the fixture publishes a per-run identity file naming the port it serves, and the harness requires
  that file plus the fixture's transport-install log marker before it proceeds;
- the spec requires `SYMMETRY_OPERATOR_TOKEN`, `SYMMETRY_FIXTURE_IDENTITY`,
  `SYMMETRY_FIXTURE_IDENTITY_FILE` and `SYMMETRY_PORTAL_URL` and skips at collection without them
  (a partial set still fails at collection, because a half-configured harness is a bug rather than
  an absent fixture); the harness asserts the spec summary — `10 passed` and no skips — so a
  skipped run fails the acceptance gate, and every test re-reads the identity file, asserts it
  carries this run's identity for the portal port, and logs in with the per-run operator token
  before any provider check.

This round's negative evidence: the spec without the harness environment skips at collection and
the repository-wide command still collects it (`--list` exit 0, `Total: 90 tests in 9 files`;
`.tmp/evidence/spec-list-without-env.txt`), while a partially configured environment still fails at
collection (`SYMMETRY_OPERATOR_TOKEN, SYMMETRY_FIXTURE_IDENTITY, SYMMETRY_FIXTURE_IDENTITY_FILE must
be set together`, exit 1; `.tmp/evidence/spec-list-partial-env.txt`); the harness fails a run whose
summary is not `10 passed` with no skips (the transport sensitivity run below reported `4 passed`
and exited 1); and the transport refusal for that run is recorded per call
(`.tmp/evidence/harness-red-transport-sensitivity/fixture-calls.jsonl` carries the matching
`fixture_unexpected_request` rejections). From the first fix round, not re-run this round: pointed
at a live Control instance on another port with a fabricated identity file and a random token, the
login is refused and the test fails before any provider check; the harness with a mismatched
`-PortalUrl`/`-Port` refuses immediately; the harness pointed at an occupied port (a
Docker-forwarded port this shell cannot enumerate as a listener) refuses immediately before it
starts anything, so the foreign server is never contacted.

Canary-proof runs from the round-3 fix (each through the same harness, real API and fixture transport):

- the pre-fix spec (`bbc9b6f`) with **both** injected canaries — an `x-canary-proof` response header
  on the session response and a transient HttpOnly `canary_probe` cookie set and cleared by two
  responses whose requests were already sent — reported `10 passed (58.1 s)`, exit 0
  (`.tmp/evidence/pre-fix-canary-injections-pass.txt`): the pre-fix scan missed both surfaces;
- the round-3 spec with the response-header canary: `2 failed / 8 passed`, exit 1, failing exactly
  at `response headers for /portal/api/session must not contain a provider credential`
  (`.tmp/evidence/red-canary-response-header.txt`);
- the round-3 spec with the transient HttpOnly cookie: `2 failed / 8 passed`, exit 1, failing at
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

With the real topology the harness run takes about 46 s to 1.7 min of Playwright time for the 10
connection tests (this round's re-run: 45.8 s; the two failed sensitivity runs measured 3.0 m and
3.9 m; the round-3 tree's runs: 51.2 s – 1.7 min under
varying host load; the first fix round measured 1.7 m; the original
receipt's 42 s was measured with the scheduler and reconciler stopped), and every flow still passes
inside the configured assertion budget: the earlier "multi-second reads under contention"
observation was not reproduced under this round's conditions.

The harness stops only the process tree it started plus the listener PID it recorded after the
identity handshake; it never kills an unowned process on the port. After the final run port 4021 was
free.

The round-3 run's recorded provider-call log
(`.tmp/evidence/harness-run-round3/fixture-calls.jsonl`, copied from the run's `%TEMP%` directory;
the first fix round's copy is `.tmp/evidence/harness-run-final/`) contains **30 calls**: 10 to
`api.github.com`, 4 to `dev.azure.com`, 12 `gh` and 4 `az` CLI invocations; every entry records the
call shape only (`"authorization":"bearer:github-fixture"`, `"output":"fixture-canary"`), never a
token value. The harness's canary scan of `control.log`, `control.log.err` and the provider-call log
reported "no fixture credential value in the control logs or the provider-call log"; that run's
control log had 37 lines and the error log 0 bytes. The browser side is asserted by the spec itself: same-origin
response bodies and every response header (each `Set-Cookie` included — the surface that introduces
an HttpOnly cookie; an unreadable body or an empty header list fails the test, and redirects are
scanned through their `Location` header), request headers through `allHeaders()` with explicit
presence checks for the cookie and CSRF headers of `/portal/api/` requests, request bodies, the DOM,
web storage, `document.cookie` (which cannot see HttpOnly state), the cookie jar sampled at every
same-origin response and once more at the end (an empty final jar or a run without jar samples
fails the test), console messages and page errors, and the scan fails closed when response records
are absent. A missing scan file or an empty provider-call log fails the harness run instead of
passing the scan blind.

This round re-ran the harness with the spec's new collection rule, the transport's bearer/method
check and the `--no-start` fixture boot: `10 passed (45.8 s)`, exit 0
(`.tmp/evidence/harness-run-fix3.txt`; provider-call and spec logs in
`.tmp/evidence/harness-run-fix3/`), with the same call shape — **30 calls**: 10 `api.github.com`,
4 `dev.azure.com`, 12 `gh`, 4 `az` — a 36-line control log, a 0-byte error log, a clean canary scan
and the failed-call sensitivity run as the negative direction.

## 5. Capability reset observed through the real API and the database

Command: `.tmp/evidence-capability-reset.sh` against the running fixture control plane; raw output in
`.tmp/evidence/capability-reset-observation.txt`. **Historical observation, not re-run**: the table
below was measured on the original run's pre-rebase subject `709570f` (the run whose sync timestamp
is `2026-11-15T17:26:32.737Z`), so it keeps that revision and neither this round's digest nor the
round-3 one re-binds it to a later tree; the same clause's readings on the current tree are §2's
browser test 4 assertions. The connection was created with the capabilities `["repositories","ci"]`,
a repository resource `acme-fixture/symmetry` was bound and synced, then
`PATCH /portal/api/connections/:id` changed the capabilities to `["repositories"]`.

| Observation (measured on `709570f`, not re-run) | Before | After |
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
| connection check/update/delete mutual exclusion while one is pending | `connections.test.tsx` "keeps provider check, update and removal mutually exclusive while one is pending" (check pending blocks edit and remove), "keeps check and removal blocked while an editor save is pending" (the update holds the surface after its dialog closes), "keeps check and editing blocked while a removal is pending" (the delete direction; it also passed on the pre-fix source, where `remove.isPending` already held it), and the remount directions "keeps a pending provider check blocked after the detail remounts" / "keeps a pending removal blocked after the detail remounts" (both failed while the remount forgot the pending request) |
| editor/removal conflict during refresh (a background refresh while the dialog is open) | `connections.test.tsx` "submits a draft against the version it was edited from after a background refresh" and "confirms a removal against the version the dialog opened with, even after a background refresh" (both answer with server truth instead of overwriting or deleting the newer revision) |
| late mutation callbacks act only on the subject the operator is looking at, on this page or after leaving it (the classic `ownsMutation` guard) | `connections.test.tsx` "drops a provider check receipt that arrives after the operator selected another connection", "does not clear the newly selected connection when a removal settles late", "does not navigate back when the operator selects another connection during the save's re-read" and "does not pull the operator back after they left the page while a save was pending" (all four failed before the fix and pass after), plus the same-subject direction "surfaces a check refusal that settles after the operator returned to the same connection" (failed before the fix, where the receipt died with the remounted detail) |
| connection focus retention across a refresh, extended to the new dialogs | `connections.test.tsx` "keeps keyboard focus on the connection row across a background refresh", "returns keyboard focus to the invoking control when either dialog closes" (Escape returns focus to "New connection" and to "Remove"), and the pending-close directions "keeps focus in the detail when the editor closes while its save is pending" / "keeps focus in the detail when the removal dialog closes while its removal is pending" (the invoking control is disabled by the pending write, so focus falls back to the detail's close control instead of the body; both failed before the fix) |
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
- **Suite state**: the original receipt's `mix test` reported `830/831 passed` at `709570f`; the
  round-3 tree measured `844/845 passed`; this round measured `845/846 passed` — all with the same
  one environment-limited failure (needs a working `pwsh`). The harness-database pollution
  limitation from the original run was not re-measured (the harness recreates that database
  itself). Both are recorded in `docs/TODO.md`.
- **Gates not re-run in this round**: the classic-resource spec and the legacy shell specs
  (`portal.spec.mjs`, `workspace-shell.spec.mjs`) were not re-measured — no classic or legacy path
  changed — so their numbers stay bound to the revisions named in §3. Every other gate the quality
  policy names for the touched surfaces (control format/compile/test, daemon vet/test, contracts
  check/effect/boundary/typecheck, frontend typecheck/lint/test/build, the connection-fixture harness
  and the browser `--list` collection) was re-run this round on the source subject above.
- **Intermittent browser cases**: the original run's `portal.spec.mjs` detach-toast failure did not
  recur in the round-3 run; instead two other `portal.spec.mjs` desktop cases ("detail output encodes
  untrusted statuses…", "two browser contexts reject a stale project update") timed out under load in
  the 6.2 m legacy run and passed alone on the same revision on a quiet host. Recorded in
  `docs/TODO.md`, not explained.
- **Legacy-shell gate freshness**: the fix round re-ran `portal.spec.mjs` and
  `workspace-shell.spec.mjs` with the expiry URL (7 skipped, no expiry skips) but with the fixture
  server and fixture database instead of the original run's environment, so the two runs are not
  strictly comparable.
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

## 8. Round 4 — the residuals from the scoped confirmation

Three residuals were fixed in `0b868fb` (seven committed paths: `browser/README.md`,
`browser/package.json`, `browser/scripts/connection-spec-env.test.mjs` (new),
`browser/tests/portal-connections.spec.mjs`, `docs/plans/2026-11-16-0007d-connection-management-plan.md`,
`frontend/src/features/connections/connections.tsx` and `…connections.test.tsx`).

| Residual (reviewer's finding) | Fix | Evidence |
| --- | --- | --- |
| the browser collection treated any partial environment as an error, so the README-documented `SYMMETRY_PORTAL_URL` + `SYMMETRY_OPERATOR_TOKEN` override — a normal custom target — failed the repository-wide suite at collection | the spec classifies its own environment: an absent fixture identity *or* the documented override collects the suite and skips the ten fixture tests, a half-configured fixture identity fails at collection, and the complete identity collects them | `node --test browser/scripts/connection-spec-env.test.mjs` → `tests 4, pass 4, fail 0`; it drives the real Playwright CLI and asserts `10 skipped` for both the override and the absent environment, status 1 with `SYMMETRY_PORTAL_URL must be set together` for the partial identity, and `Total: 10 tests in 1 file` for the complete one. Independent re-runs on `0b868fb`: `--list` with no fixture environment → rc 0, `Total: 90 tests in 9 files`; with the documented override → rc 0, `Total: 90 tests in 9 files`. |
| after a successful DELETE the focus fallback died with the detail: focus returned to `BODY` although the list's New connection button was available | the delete-success path restores focus to a control that survives the detail unmount | the new frontend test failed on the pre-fix `connections.tsx` (`.tmp/evidence/red-frontend-removal-focus.txt`) and passes here (`.tmp/evidence/green-frontend-removal-focus.txt`). |
| the plan's mapping table still promised `reset_resource_ids` while §3/§10.8 record the projection decision | the plan now matches the decision | `docs/plans/2026-11-16-0007d-connection-management-plan.md`; documentation only, no behaviour change. |

Re-measured on `0b868fb` by the verifying owner rather than the producing run: frontend
`Test Files 5 passed (5)` / `Tests 67 passed (67)` in 21.35 s (67 agrees with the run's own number;
66 was `fc3cf8c`), `tsc --noEmit` and `eslint . --max-warnings 0` exit 0, `node --test
browser/scripts/connection-spec-env.test.mjs` `4/4`, and the fixture harness in its round-4 row
`10 passed (1.1 m)` — that row records `45.8 s` for `fc3cf8c`, superseded by this measurement — with
the canary scan clean and rc 0. The frontend gates print `pnpm`'s "the install that runs before
scripts failed" warning because the public registry is unreachable on this host (`--offline`);
`node_modules` matches this lockfile, unchanged from the base. Control and contract packages are not
touched by the PR-bot round or round 4, so the `845/846` control number and the contract counts keep
the revisions named in their rows.

## 9. Round 5 — the rebase onto main (union with the merged 0007b)

Merging 0007b (`7a92ca4`) left this PR conflicted: GitHub reported `DIRTY/CONFLICTING` and ran no checks,
because the branch's own base (`259ea7d`) no longer fast-forwards. The seven commits were therefore
rebased onto `7a92ca4` (`86b368a→0b2e16f`, `bbc9b6f→28997fb`, `e67a925→4c0b0f0`, `43c1004→381775c`,
`fc3cf8c→3d10b91`, `0b868fb→1974260`, `0f854fd→1250976`; subjects unchanged, no squash, no reword).
Fourteen files conflicted and were resolved as the **union** of the two features — no entry, schema,
fixture, route, allowlist case or test from either side was dropped:

| File | Resolution |
| --- | --- |
| `browser/README.md` | both acceptance sections kept (0007b's read views, then 0007d's connections) |
| `contracts/README.md` | both prose blocks, with the read-projection sentence naming all four decoders and the numeric-lexeme sentence covering the union |
| `contracts/fixtures/manifest.json` | union corpus: 0007b's 219 entries plus the five `connections-read` fixtures |
| `contracts/scripts/check.mjs`, `check-effect.mjs`, `generate.mjs` | one four-member read-projection set (`workspace-read`, `work-detail-read`, `work-timeline-read`, `connections-read`); schema count 18 on both sides of the check |
| `control/lib/symmetry_control_web/portal_session.ex` | shell-path alternation union (`attention\|connections\|projects\|work`), 0007d's `connectionId` param rule, and a `shell_query?("/portal/connections", …)` clause inside 0007b's delegated allowlist |
| `daemon/internal/contracts/schema.go`, `schema_test.go` | `presentationEnvelopes` extended to all four read projections; the superseded single-projection helper deleted; compiled-schema expectation 18 |
| `daemon/internal/protocol/goal_contract_fixture_test.go` | union switch: 0007d's connections case plus 0007b's combined workspace/work-detail/work-timeline presentation case |
| `frontend/src/app/application.ts`, `router.tsx`, `shell.tsx` | allowlist union, both routes in one tree, and the union of both sides' imports and query-option helpers |
| `frontend/src/services/api.ts` | contract type union **plus the one non-marker repair**: the auto-merged `workDetail`/`workTimeline` calls still used the 3-argument `request(...)` against 0007d's 4-argument helper, so both now pass `{ method: "GET" }` — caught by `tsc --noEmit` |

That last row is the only place where a textually clean merge was not semantically clean; the clean
typecheck and the 116-test frontend suite are the evidence that nothing else of that kind survived.

Verified on the rebased head `1250976` (integrator-run, after rebuilding the frontend bundle that the
browser run needs):

| Command | Exit | Observed |
| --- | ---: | --- |
| `node contracts/scripts/check.mjs` | 0 | `Validated 18 Draft 7 schemas and 224 fixtures` (the union of 0007b's 17/219 and 0007d's connections schemas/fixtures, against ≥ both) |
| `node contracts/scripts/check-effect.mjs` | 0 | 224 raw fixtures decoded, 220 reached schema validation |
| `pnpm --offline --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit`, clean |
| `pnpm --offline --pm-on-fail=ignore --filter @symmetry/frontend test` | 0 | `Test Files 8 passed (8)`, `Tests 116 passed (116)` — the union of both sides' suites, nothing skipped or conditional |
| `go test ./internal/contracts ./internal/protocol` (`daemon/`) | 0 | `ok …/internal/contracts 3.973s`, `ok …/internal/protocol 8.168s` |
| `mix format --check-formatted` / `mix compile --warnings-as-errors` (`control/`) | 0 / 0 | run through `mise exec elixir@1.20.4-otp-29 erlang@29.1.2 --` because the bare shim reports `no version is set`; one file compiled, no warnings |
| `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1` (after `pnpm --filter @symmetry/frontend build`) | 0 | `10 passed (1.5 m)`, five tests × desktop-chromium and mobile-chromium on the new route through the real API, canary scan clean, port released |

Named limits after the union: the work-views browser suites (`portal-work-views`, `workspace-shell`,
both widths) were last run at 0007b's own merged revision and were **not** re-run after the union;
live-daemon, compose/restart and the legacy shell specs were not re-run; the Elixir side was exercised
through `mise exec` for the touched web layer (202 passed in `test/symmetry_control_web`) plus the two
focused files, not the whole control suite.

Two residual risks the resolving pass recorded and this receipt keeps open rather than smoothing over:
the `api.ts` arity break shows that a textually clean merge can still be semantically wrong, so any
later conflict should be re-verified by typecheck and tests rather than by markers; and the frontend
`safeReturnTo` allowlist is looser than the server's per-path query rules on `/portal/connections`
(the server stays the authority, so the worst case is a deep link falling back to `/portal`), now
tracked in `docs/TODO.md` rather than duplicated into the client.
