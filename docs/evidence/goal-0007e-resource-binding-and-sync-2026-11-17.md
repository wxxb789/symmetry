# 0007e resource binding and truthful synchronization — evidence receipt

Status: **produced and awaiting the owner's verdict**. Every number below names the subject it was
measured on. Owner acceptance — the owner reviewing the rendered resource-binding, synchronization
and assignment flows on real data plus this boundary statement against the fixed design — is
explicitly after this run and not part of the goal's Done when, so this receipt states the result as
produced and does not rate its own rendering or sign for the owner.

Binding for this round: base `e6e4b03b35f2b1b1c884594c09adce20a75c4df8`; the increment is the
working tree this receipt lives in (the run leaves it uncommitted for verification, per the
increment instructions). **Source subject**: `sha256
bb9372daf9074e6ee39d7fca811072b144e5dac28c2b21ae566b8ccad2f743d4` over **1052** paths, computed
after the last source edit (the `detail.tsx` section move) and before the final harness run, over
the same kind of path set as 0007d — every
tracked and untracked non-ignored repository path with `docs/**` excluded, so a receipt edit cannot
invalidate its own binding:

```sh
git ls-files --cached --others --exclude-standard -z | grep -zv '^docs/' | sort -z | xargs -0 sha256sum | sha256sum
```

`control/priv/static/portal/` and `.tmp/**` are gitignored and therefore excluded by construction;
the frontend build was staged into `control/priv/static/portal` before the browser runs.

Plan: `docs/plans/2026-11-17-0007e-resource-binding-and-sync-plan.md` (§11 records every deviation
and finding taken during implementation).

## 1. What this increment contains

| Group | Paths |
| --- | --- |
| Frontend | `frontend/src/features/connections/resources.tsx` (new: project-resource surface), `features/connections/connections.tsx` (project scope, project read, per-subject pending counts, section placement), `features/connections/connections.test.tsx`, `features/work/assignment.tsx` (new: assignment editor), `features/work/assignment.test.tsx` (new), `features/work/detail.tsx` (renders the assignment section), `services/api.ts` (resource/assignment API surface), `app/router.tsx` + `app/shell.tsx` (`?projectId=` scope on `/connections`, project picker branch), `styles.css`. |
| Fixture + harness | `scripts/ci/portal-connection-fixture.exs` (GitHub issues/actions/pulls/reviews documents, ADO repositories/wiql/workitemsbatch/build documents, `SYMMETRY_FIXTURE_STATE_FILE` steering with `fail` / `transport_fail` / `hold` / `issues` / `work_items` / `repository`, strict per-path refusals), `browser/scripts/run-connection-fixture-tests.ps1` (state file, this worktree's database, pinned sweep interval, 20-run expectation), `browser/scripts/connection-spec-env.test.mjs`, `browser/README.md`. |
| Browser acceptance | `browser/tests/portal-connections.spec.mjs` (10 flows), `browser/tests/portal-classic-resources.spec.mjs` **deleted** (4 tests / 8 runs). |
| Control API tests | `control/test/symmetry_control_web/controllers/portal_resources_test.exs` (new, 10 tests). |
| Docs | this receipt, `docs/plans/2026-11-17-0007e-resource-binding-and-sync-plan.md`, `docs/TODO.md` (findings recorded below). |

No wire contract changed: every operation is an existing endpoint, and the contracts package
(schemas, fixtures, manifest, generated DTOs) is byte-identical to the base revision.

## 2. Done when → evidence

| Contract clause | Evidence in this run |
| --- | --- |
| binding, updating, unbinding and synchronizing repository, work-tracking and CI resources from both provider kinds, real API and browser, boundary stated per flow | new-route spec test 6 (GitHub repository: bind → served identity → sync → version-fenced update refusal → identity-change reset → unbind) and test 10 (Azure DevOps `project/repository`, `project`, `project/pipeline/<id>` bound and synchronized); spec test 7 (GitHub work-tracking issues import + repository sync). Boundary: §4. `portal_resources_test.exs` covers the same operations through the routed API for 3 kinds × 2 providers, including the capability refusal and the identity reset. |
| provider failure and unknown outcomes stay explicit, never synchronized or fresh | spec test 7: injected HTTP 500 → row `Failed` + `Provider returned HTTP 500`, `getByText("Synced")` count 0, `last_synced_at` unchanged from the last success; ambiguous transport failure → `Provider transport failed`, same held timestamp. `portal_resources_test.exs` "a definitive provider failure…" and "an ambiguous transport outcome stays failed until a readback writes exactly the provider answer" assert the same through the API plus direct `project_resources` reads. |
| an unknown outcome resolved by readback shows the resolved truth, not success by assumption | spec test 7 second half: after the ambiguous outcome the fixture answers a repository document that contradicts the pre-failure state (`archived: true`, a different `html_url`); the readback writes and renders exactly those values with a strictly newer `last_synced_at`, verified through `GET /portal/api/connections` and the rendered row. ExUnit asserts the same rows through SQL. Interpretation recorded in plan §11.2. |
| project switching and concurrent refresh never let a late response overwrite the newly selected project's state | spec test 8 (a real sync held in flight by the fixture's `hold` steering while the project switches; the late continuation is discarded, the new project's rows are intact; the second half asserts the single-flight exclusion, with a second browser context for the cross-operator direction). Frontend component tests cover the same switch/late-response rules with deferred gates, and `portal_resources_test.exs` covers the server-side concurrency guard. |
| synchronized external work items keep their exact external identity, provider-owned fields stay locked, an externally unavailable item renders unavailable with no execution entry point | spec test 9: imported issues carry `external.id` `101`/`102` and the exact provider URLs; the detail renders `unavailable` for the item the readback omitted and shows no run/retry control (`toHaveCount(0)`); `can_start` is false for it and true for the still-available sibling; a `title` PATCH is refused `409 provider_owned`. `portal_resources_test.exs` asserts the same projections and the DB `external_available` flip. |
| work assignment offers only the selected authorized resources | spec test 9: the assignment dialog's `Repository`/`CI resource` option sets contain exactly the work item's project's matching-kind resources (a sibling project's resource is absent), the save is confirmed through the API readback, and the API refuses a resource of another project with `422 validation_failed`. `assignment.test.tsx` asserts the option sets and the payload; `portal_resources_test.exs` "work item assignment is scoped to the work item's project and resource kind" covers own-project/wrong-kind/unknown-id. |
| this surface's pre-existing browser acceptance continues to pass on the new routes and its classic checks are retired in the same PR | the four classic resource tests are deleted in this change; §5 maps every retired assertion to its replacement, and §6 lists the removed spec. The retained classic and shell suites were re-run (§3). |
| unverified reported | §6 (unresolved limitations) and §7 (red-before/green-after records). |

## 3. Executed checks

All rows were measured on this worktree's revision (the working tree this receipt binds):

| Exact command (working directory) | Exit | Observed |
| --- | ---: | --- |
| `mise exec pnpm@12.17.0 -- pnpm --offline contracts:typecheck` (root) | 0 | `tsc --project contracts/tsconfig.json`, clean. |
| `mise exec pnpm@12.17.0 -- pnpm --offline contracts:test` (root) | 0 | `node --test contracts/scripts/*.test.mjs`: `tests 33`, `pass 33`, `fail 0`, `skipped 0`. |
| `node contracts/scripts/check.mjs` (root) | 0 | "Generated contract DTOs are up to date. Validated 18 Draft 7 schemas and 224 fixtures." |
| `node contracts/scripts/check-effect.mjs` (root) | 0 | "Effect decoded all 224 raw fixtures; 220 parsed documents reached Schema validation." |
| `pnpm --offline --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit`, clean. |
| `pnpm --offline --filter @symmetry/frontend lint` | 0 | `eslint . --max-warnings 0`, clean. |
| `pnpm --offline --filter @symmetry/frontend test` | 0 | `Test Files 9 passed (9)`, `Tests 142 passed (142)`. |
| `pnpm --offline --filter @symmetry/frontend build` | 0 | `vite build` + `scripts/stage-assets.mjs`; assets staged into `control/priv/static/portal` (run before the browser gates). |
| `go vet ./...` (`daemon/`) | 0 | clean. |
| `go test ./...` (`daemon/`) | 1 | 20 packages `ok`; `internal/harness/omp` failed one test under load (`TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock`, "context deadline exceeded" against that test's own 1 s deadline) while the full Elixir suite ran concurrently. Re-run alone on the same revision: `ok github.com/wxxb789/symmetry/daemon/internal/harness/omp 10.609s`, exit 0 (`.tmp/evidence/go-test.txt`, `go-test-omp-alone.txt`). No daemon code is touched by this increment. |
| `mix test test/symmetry_control_web/controllers/portal_resources_test.exs` (`control/`, `POSTGRES_DB=symmetry_control_test`, port 55444) | 0 | `10 passed`. |
| `mix format --check-formatted` (`control/`) | 0 | clean over the repository's format set, including the new `portal_resources_test.exs`. |
| `mix compile --warnings-as-errors` (`control/`) | 0 | clean; the only control source change is the added test file, so this re-compiles the same library the suite ran against. |
| `mix test` (`control/`, same dedicated database) | 2 | `855/856 passed, 21 skipped`, `Failed: 1` — the environment-limited `CommandTest` "provider token commands can keep stderr diagnostics out of stdout" (`{:error, {:command_unavailable, "pwsh"}}` at `test/symmetry_control/integrations/command_test.exs:25`), the same single failure 0007d recorded, on a tree where the only control file this increment adds is `portal_resources_test.exs`. An earlier run of the same suite started before the frontend build staged `control/priv/static/portal` and reported `851/856` with four `PortalControllerTest` asset failures; the staged-assets re-run is the recorded number (log `mix-test.txt`, earlier run `mix-test-before-assets.txt`). |
| `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1` | 0 | **Final run on the frozen tree** (the last source edit before it is the `detail.tsx` section move): `20 passed (2.6m)` — the 10 flows on desktop-chromium and mobile-chromium — with the harness's own summary gate (`20 passed`, no skips) satisfied, and `canary scan: no fixture credential value in the control logs or the provider-call log`. The run recreated and migrated `symmetry_0007e_test` on `127.0.0.1:55444`, kept the real supervision topology (`/healthz` ready), and stopped only its own process tree. Recorded fixture calls: **144** (`62` HTTP, `82` command) — `api.github.com` 48 (36× `200`, 2× `401`, 2× `500` steered `fail`, 8 aborted steered `transport_fail`) / `dev.azure.com` 14 (12× `200`, 2× `401`); commands `gh config get git_protocol` ×36, `gh auth token` ×36, `az account get-access-token` ×10; every HTTP record carries only a bearer shape, never a token value (`bearer:github-fixture` ×48, `bearer:azure-fixture` ×14). Control log 38 lines / 3,865 bytes, error log 0 bytes, fixture state file back to `{}`. Artifacts: `harness-run-final.txt`, `harness-run-final-calls.jsonl`, `harness-run-final-control.log`, `harness-run-final-spec.log`. The earlier run of the same harness on the pre-move tree reported `20 passed (2.5m)` with 142 calls (`harness-run.txt`); the final run supersedes it. |
| `pnpm --offline --filter symmetry-browser-acceptance test --list tests/portal-connections.spec.mjs` | 0 | `Total: 20 tests in 1 file` (10 flows × desktop-chromium and mobile-chromium). |
| `mise exec pnpm@12.17.0 -- pnpm --offline --filter symmetry-browser-acceptance test:connection-env` | 0 | `browser/scripts/connection-spec-env.test.mjs`: `tests 4`, `pass 4`, `fail 0` (absent / documented-override / partial-identity / complete-fixture environments, the last one asserting `Total: 20 tests in 1 file`). |
| retained legacy suites — `portal-work-views.spec.mjs`, `portal.spec.mjs`, `workspace-shell.spec.mjs` on both widths against two staged fixture control planes (`browser/scripts/portal.spec.mjs` contract: real API, no route interception of the portal's own reads) | 1 | `46 passed`, `1 failed`, `5 skipped` (52 cases, 5.5m) in the final run; the single failure is the desktop case `portal.spec.mjs:483` "two browser contexts reject a stale project update" hitting the 60 s test timeout — the load-sensitive case the 0007d receipt already recorded, which passes alone on this revision (`ok`, 4.4 s, `.tmp/evidence/classic-flakes-alone.txt`). The first run of the same suites reported `43 passed`, `4 failed`, `5 skipped`: two of those four were the section-order regression (§8 #7, fixed, green in the final run), the third was the mobile classic toast case below, the fourth the same desktop timeout. Logs: `legacy-browser-run.txt` (run 1), `legacy-browser-run2.txt` (final), `work-views-run.txt` (the fixed suite alone: `19 passed`, `1 skipped`, exit 0 — the skip is the desktop-only concurrency case on mobile). |

## 4. Provider evidence boundary (fixtures_only), per flow

Boundary: **fixtures_only**. The control plane under test is the real application — real router,
controllers, `Integrations`/`Workspaces` modules, provider modules, migrations and PostgreSQL
database. Only the two external provider transports are replaced by
`scripts/ci/portal-connection-fixture.exs` (`:integration_http_client`, and
`:integration_command_runner` for the local `gh`/`az` session probes). No provider host is contacted,
and provider-side resource creation, mutation or deletion is out of scope by the contract.

| Flow | Real | Fixture |
| --- | --- | --- |
| Binding a resource (both providers, all three external kinds) | `POST /portal/api/projects/:id/resources` → `Workspaces.create_resource/2` → provider-module reference validation → `project_resources` row | none — reference validation is local (`owner` must equal the connection's `account_ref` for GitHub; ADO validates the reference shape) |
| Capability refusal / kind rules | `ensure_connection_capability/2` against the connection's stored capabilities | none |
| Resource update / unbind (version fences, identity reset, referenced-resource refusal) | `PATCH`/`DELETE /portal/api/resources/:id`, `prepare_resource_attrs/2` reset, `ensure_resource_unreferenced/1` | none |
| GitHub repository sync | `sync_connected_resource/1` → `Providers.GitHub.sync_resource/3` → `persist_sync/4` | `GET https://api.github.com/repos/{owner}/{repo}` document (overridable) |
| GitHub work-tracking sync | same path → issue import → `persist_sync_work_items/4`, `mark_missing_work_items_unavailable/2` | `GET …/issues` list (default issues 101 open / 102 closed) |
| GitHub CI sync | same path → `…/actions/runs` document read | one completed/succeeded workflow run |
| Azure DevOps repository / work-tracking / CI sync | same path → `Providers.AzureDevOps.sync_resource/3` | `…/_apis/git/repositories/{repo}`, `POST …/wit/wiql` + `workitemsbatch`, `…/build/definitions/{id}` + `…/build/builds` documents |
| Provider failure | `failure/1` mapping → `record_resource_failure/3` (`degraded`/`failed` + fixed message, `last_synced_at` untouched) | steering `fail` answers HTTP 500 |
| Unknown outcome and readback | the same sync path writes only what the provider read returned; the readback is that read | steering `transport_fail` drops the answer; a later document (steered override) is the readback truth |
| Project sync | `Integrations.sync_project/1` + the controller's per-resource aggregate | the same provider documents |
| Work assignment | `PATCH /portal/api/work-items/:id` → `validate_work_item_resources/2`, `provider_owned_fields_editable/2`, `execution_fields_editable/2` | none |
| External item availability | `mark_missing_work_items_unavailable/2` after a readback that omits the item | steering `issues` shrinks the answered set |
| Provider-owned delivery | `sync_delivery` → `persist_provider_action/…` writes `external_*`; the work detail renders `source: "provider"` | pull-request and review documents (`/pulls/42`, `/pulls/42/reviews`) |

The harness keeps the application's real supervision topology (scheduler, reconciler, provider
access; `/healthz` is a readiness condition) and now pins
`SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS=3600000` so the periodic sweep cannot re-synchronize the
acceptance rows mid-run; the syncer child stays up and nothing else about the application is
reconfigured. Fixture CLI tokens remain canaries: the harness scans the control log, its error log
and the provider-call log, and the spec scans same-origin response bodies and every response header,
the DOM, web storage, the cookie jar (sampled during the run), request headers and bodies, console
messages and page errors.

Harness run observation (final run on the frozen tree): **144** recorded fixture calls — `62` HTTP
(`api.github.com` 48, `dev.azure.com` 14) and `82` commands (`gh config get git_protocol` ×36,
`gh auth token` ×36, `az account get-access-token` ×10). HTTP statuses: `200` ×48, `401` ×4 (the
`expired-fixture` account), `500` ×2 (`steered: fail`), 8 aborted (`steered: transport_fail`); the
four `hold` visits of test 8 are released and logged like any other read. The provider-call log
records only the bearer *shape* (`"authorization":"bearer:github-fixture"` ×48 /
`"bearer:azure-fixture"` ×14), never a token value; the harness's canary scan over the control log,
its error log and the provider-call log reported "no fixture credential value", with a 38-line
control log and a 0-byte error log. Artifacts: `.tmp/evidence/harness-run-final.txt` (Playwright +
canary summary), `harness-run-final-calls.jsonl`, `harness-run-final-control.log`,
`harness-run-final-spec.log`.

**Credential-scan scope, stated exactly.** The spec scans same-origin response bodies and every
response header (each `Set-Cookie` included), the DOM, web storage, `document.cookie` and the cookie
jar (sampled at every same-origin response and once more at the end), request bodies, console
messages and page errors. Two records are provably unreadable and are handled explicitly rather than
silently: a request the page cancelled (its observer unmounted on a project switch or a document
navigation) has `request.failure() != null`, carries no cookie because nothing was sent, and must be
a `GET`/`HEAD` — a write can never take this branch; a response whose body the protocol can no longer
serve is allowed only when its request failed. Every other same-origin `/portal/api/` request must
prove its cookie header (and its CSRF header when it is a write), every completed response is
scanned, and header/DOM/storage/console scans are unchanged.

## 5. Retired classic coverage

`browser/tests/portal-classic-resources.spec.mjs` (4 tests, 8 runs across the desktop and mobile
projects) no longer exists in this revision. Every assertion it pinned has a replacement on the new
route or in the component/API layer:

| Retired classic assertion (spec lines) | Replacement |
| --- | --- |
| binding create (`:52-76`, `:214-228`, `:826-849`, `:972-983`) and ADO reference normalization (`:828`) | spec test 6 (GitHub console bind) and test 10 (ADO `project/repository`, `project`, `project/pipeline/<id>`) |
| per-resource sync (`:78-127`, `:851-898`) and the "Synced" row (`:231`, `:1011`) | spec tests 6/7/10 (`Synced` only from the served projection, with its timestamp) |
| project sync (`:382-391`) and its failure mapping (`:488-491`) | spec test 8 (held project sync + late failure) and `portal_resources_test.exs` (aggregate failure shape with per-resource results) |
| single-flight across rerenders/dialogs (`:404-466`) | spec test 8 (observable mutual exclusion) + `connections.test.tsx` deferred-gate tests |
| late response / dialog ownership (`:448-451`, `:733-734`) | spec test 8 (discarded late continuation, two contexts) + component tests |
| editor stale/draft/refresh rules (`:672-679`, `:692-712`) | spec test 6 (version-fenced update and unbind refusals) + component tests |
| project switch, rollback, archived read-only (`:499-518`, `:736-738`, `:748-763`) | spec test 8 + `connections.test.tsx` archived-project and switch tests |
| external identity / provider attribution (`:235-238`, `:988-993`, `:1016-1021`) | spec test 9 (exact `external.id`/`url`/`state`/`available`/assignee) and spec test 7 (provider-sourced delivery rows: the rendered `source: provider · github` attribution with the exact provider URL, plus the served `delivery.*.provider`) |
| provider-owned locking (`:241-243`, `:996-997`) | spec test 9 (no title/description/priority/state control on the surface; API `409 provider_owned`); `assignment.test.tsx` asserts the dialog's control set |
| unavailable item and absent execution entry (`:270-271`, `:278-280`) | spec test 9 (rendered `unavailable`, no run/retry control, `can_start: false` in the readback, sibling still `true`) |
| assignment (`:129-149`, `:244-252`, `:900-916`) | spec test 9 (console assignment, project-scoped option sets, API readback) |
| layout / no horizontal overflow (`:286`) | every new test keeps the `capture()` overflow assertion |
| Azure DevOps resource kinds (`:784-1024`) | spec test 10 |

Assertions that existed only because the classic spec mocked the browser (`page.route` held
responses, injected workspace/JSON failures, hand-written provider payloads) moved to frontend
component tests with deferred response gates, the same treatment 0007d used for the connection
surface. Removed spec: **`browser/tests/portal-classic-resources.spec.mjs`** (4 tests, 8 runs
across the desktop and mobile projects) — "a bound provider resource can be synchronized and
assigned" (`:14`), "provider operations remain single-flight across rerenders and respect dialog
ownership" (`:289`), "resource mutations, refreshes, and project switches preserve one coherent UI
owner" (`:521`), "Azure DevOps resource kinds normalize Boards, Repos, and Pipelines" (`:784`).

## 6. Unresolved limitations (not verified here)

- **Live provider credentials and authorization**: unverified. No real `gh` or `az` session and no
  real provider host was used; every provider answer came from the fixture transport. A live check on
  a real account is a separate authorized outcome.
- **Provider-side resource mutation**: out of scope by the contract; nothing in this run wrote to a
  provider.
- **Hosted qualification**: no CI run, deployment or hosted environment was exercised; every number
  here is local to this workstation (Windows, containerized PostgreSQL on `127.0.0.1:55444`).
- **Provider delivery for Azure DevOps**: only the GitHub arm of the provider-owned delivery
  write-through is exercised (spec test 7 and the ExUnit delivery test). The ADO delivery arm shares
  the code path but has no test in this increment.
- **`connection`-kind project resources**: rendered with their served kind, but not creatable from
  the new surface (plan §11.7); the classic dialog still offers them until 0007i.
- **pnpm supply-chain verification**: cannot complete on this host (the public registry is
  unreachable); `--offline` makes the lockfile check fail in about a second and print its
  `node_modules` warning. `pnpm-lock.yaml` is byte-identical to the base revision.
- **Control suite state**: `855/856 passed, 21 skipped` with one environment-limited failure —
  `CommandTest` "provider token commands can keep stderr diagnostics out of stdout" needs a working
  `pwsh` (`{:error, {:command_unavailable, "pwsh"}}`), the same failure 0007d recorded. A first run
  of the same suite failed four `PortalControllerTest` cases because it started before the frontend
  build staged `control/priv/static/portal`; the re-run after staging is the recorded number. Both
  findings are in `docs/TODO.md`.
- **Go suite state**: `go test ./...` reports 20 packages `ok` and one load-induced failure in
  `internal/harness/omp` (`TestFinalAssistantMessageAcceptsReasoningBlocksBesideSingleTextBlock`,
  "context deadline exceeded" against that test's own 1 s deadline) while two other heavy suites ran
  concurrently; the package passes alone on the same revision (`ok … 10.609s`, exit 0). No daemon
  code is touched by this increment.
- **`hold` steering bound**: a held provider read polls for at most 300 × 100 ms = 30 s and then
  answers `{:error, {:transport, "fixture hold timed out"}}` (logged `steered: "hold_timeout"`); a
  leaked hold therefore degrades into an explicit transport failure instead of hanging a run.
- **Windows-only evidence**: the workstation is Windows; no Linux or macOS run was made.
- **Restart/recovery of the fixture control plane**: not exercised; the same boundary 0007d recorded.
- **Retained legacy suites are not fully green on this host**: the final run reports `46 passed,
  1 failed, 5 skipped` with the failure in `portal.spec.mjs:483` "two browser contexts reject a
  stale project update" (desktop, 60 s test timeout) — the load-sensitive case 0007d already
  recorded in `docs/TODO.md`, which passes alone on this revision (4.4 s). A second case,
  `portal.spec.mjs:113` (mobile), failed in the first run and passes in the final one: the classic
  work-item save persists while `#toast` keeps the previous text, in files this increment does not
  touch (`control/lib`, `control/priv/portal_assets`, `portal_html`, `portal_controller.ex` are
  byte-identical to the base revision). Both are recorded in `docs/TODO.md`; neither is a change of
  the pre-existing classic behavior by this increment, and no assertion was modified.

## 7. Red-before/green-after records

This increment adds a surface rather than repairing a released behavior, so "red before" is mostly
the absence of the surface. What was actually executed:

| Change | Red evidence | Green evidence |
| --- | --- | --- |
| Assignment section on the work detail | executed: with the `<AssignmentSection …>` render removed from `detail.tsx`, all 9 `assignment.test.tsx` tests fail (worker-run mutation check) | the same 9 tests pass in the 142-test frontend suite |
| Project-resource surface on `/portal/connections` | static: at the base revision the string set `Bind resource` / `Synchronize project` / `Project resources` / `Unbind ` has 0 matches in that file and `resources.tsx` does not exist | 17 component tests plus spec tests 6–10 |
| Browser coverage of binding/sync/assignment on the new route | structural: the base revision's `/portal/connections` renders no such controls and the base fixture answers no `…/issues`, `…/actions/runs`, `…/pulls`, ADO `wiql`/`workitemsbatch`/`build` reads (`fixture_no_route`), so the new flows cannot pass on the base revision | spec tests 6–10 in the harness run |
| Provider-owned delivery write-through | structural: a repository sync for a work item bound to a resource was previously covered only by the classic mocked spec (now retired) | ExUnit delivery test + spec test 7 delivery assertions |
| Blank reference answer (candidate change) | — | reverted; the pre-existing `400 invalid_request` is pinned by an ExUnit assertion (plan §11.3) |
| Work-detail section order (regression caught by the legacy gate) | executed: the 0007b work-detail acceptance failed on both widths with `Assignment` as the second `.detail-section` (§8 #7) | the same case passes once the section renders last — 19 passed / 1 skipped, exit 0 |

One wording change reached an existing surface: the shared refusal guidance for `not_found` now reads
"Reload the page to see the server's current state." instead of "…current connections." because the
map is shared with the resource surface. The 0007d connection assertions (code + "The server's
current state is shown instead.") pass unchanged in the harness run.

## 8. Defects found by the gates and their fixes

The harness was run repeatedly while the flow assertions were hardened; each entry below is a real
failure observed in a run, its cause, its fix, and the run that closed it. Logs:
`.tmp/evidence/harness-run.txt` (final, 20 passed), `harness-subset-1..7.txt` (iterations).

| # | Failure observed | Cause | Fix | Closed by |
| --- | --- | --- | --- | --- |
| 1 | test 8 both projects: `expect(getByLabel('Project')).selectOption` strict-mode violation (3 elements) | the new section's heading/list labels contain "Project" | spec uses `getByLabel("Project", { exact: true })` (the shell picker) | subset-4 |
| 2 | test 10 both projects: `external.state` expected `"ready"`, received `"Active"` | the spec asserted the normalized work state onto the provider's raw state | spec asserts `external.state === "Active"` **and** `status === "ready"` | subset-5 |
| 3 | tests 6/7/10: `…the cookie header of /portal/api/workspace must be visible…` | the first `reconcile()` implementation refetched twice (invalidate + observer `refetch()`), whose `cancelRefetch` cancelled the first request | `reconcile()` now invalidates the workspace reads once (`invalidateQueries` never cancels an in-flight fetch) and marks other projects stale | subset-6 (8/9 desktop green) |
| 4 | test 7 delivery rows: expected `source: github`, received `source: provider` | the surface renders the delivery's provenance and, until this fix, not the provider name | `DeliveryRow.provider` added (`presentation.ts`) and rendered as `source: provider · github`; the spec asserts that plus the served `delivery.*.provider` | subset-6 |
| 5 | test 8: `response /portal/api/workspace must be readable…navigated away from` | post-login document reloads and a second operator document load stranded in-flight response bodies; switching scope while the refresh's read was in flight cancelled it | the out-of-band-project cases use `Refresh` + `networkidle` + in-app scope selection; the operator selects both scopes in-app; the delivery-negative case keeps a `networkidle`-guarded reload; the scan scope was then stated exactly for provably aborted reads (above) | bg_93 (20 passed) |
| 6 | test 8 mobile: `EPERM … fixture-state.json.…tmp -> fixture-state.json` | the fixture re-reads the steering file (every 100 ms while a read is held), so an atomic rename can briefly hit an open handle on Windows | `writeFixtureState` retries the same rename (≤20 × 25 ms) instead of weakening it to an in-place write | bg_93 |
| 7 | legacy run 1, desktop **and** mobile: `portal-work-views.spec.mjs:507` "work detail renders outcome, evidence, blocker, next action and history on real data" — received `["Outcome", "Assignment", "Latest accepted evidence", …]`, the pinned value is `["Outcome", "Latest accepted evidence", "Blocker", "Next action", "Execution history"]` | the assignment section was rendered between the project-fact list and the evidence section, shifting the first five `.detail-section` headings that the 0007b acceptance pins | `<AssignmentSection …>` renders after the history section, with a comment naming the pin; no 0007b assertion was edited | work-views re-run alone on both widths: `19 passed`, `1 skipped` (desktop-only concurrency case), exit 0 (`.tmp/evidence/work-views-run.txt`); full legacy re-run `legacy-browser-run2.txt` |

The `hold` steering primitive itself (plan §11.5) is what makes test 8's in-flight evidence real: the
fixture blocks the provider read server-side, the browser switches project against a genuinely
pending sync, and the spec releases the hold by rewriting the state document.
