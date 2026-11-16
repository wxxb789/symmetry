# 0007h Goal attention workspace — implementation receipt

Status: **produced and awaiting the owner's verdict**. The attention entry point
and the Goal detail surface are implemented on the accepted Goal APIs, checked by
contract, control, frontend, Go, browser and live-server runs on the revision this
receipt binds, and the classic attention checks this surface replaces are retired
in the same change. The owner's rendering review is not part of this run and is
not claimed.

Revision binding: this branch (`feat/0007h-goal-attention-workspace`, base
`7a92ca498f60309b97920b9df834f2637b91cfbd` — the merged 0007b increment on the
accepted 0009 amendments) plus this change set, listed below. Because the branch
may be rebased before merge, the binding is the **source subject**: the digest of
the source-file manifest with every `docs/**` path excluded, so editing this
receipt cannot change it. The exact final digest is recorded in the PR
description; the per-gate evidence below names the files it was produced on.

For this working tree the source subject is
`sha256:822bf31bac0a375567203248160e5d0d582b05175d88841bb25863a717abce7e`
(1055 entries: every tracked non-`docs/**` file plus 20 untracked new source
files, each hashed and listed as `sha256 <path> <digest>` in path order before the
final hash; recomputed with `node .tmp/subject-digest.mjs` in this tree, exit 0).
The exact final digest of the merge revision will be recorded in the PR
description; the per-gate evidence below names the tree it was produced on.

## What changed

### Contract (additive descriptive reads, no wire change)

| File | Change |
| --- | --- |
| `contracts/v1/attention-read.schema.json` | New Draft 7 root: the merged attention read (Goal attention feed + project blocked items + the per-feed `unavailable` statement). `additionalProperties: true`, unconsumed members stay additive. |
| `contracts/v1/goal-read.schema.json` | New Draft 7 root: the portal Goal read (objective, accepted progress, blockers, server `allowed_actions`, settlement attention, decisions with their served `version`, accounting, waiting-external boundaries, the read-only dependency projection). Every boolean the Elixir projection serves is described with the name the server actually sends (`required?`, `accepted?`, `integration?`, `ready?`, `all_required_accepted?`, `current_revision?`, `no_open_decisions?`, `no_nonterminal_tasks?`); the first browser run caught the descriptive schema naming them without the suffix, and the surface read `completion.ready` where the server sends `ready?`. |
| `contracts/fixtures/valid/attention-read.{basic,goal-unavailable}.json`, `contracts/fixtures/invalid/attention-read.{missing-project-blocked,non-uuid-goal}.json` | 2 valid + 2 invalid fixtures for the merged feed, including the Goal-feed-refused page that still serves project blockers. |
| `contracts/fixtures/valid/goal-read.{draft,active}.json`, `contracts/fixtures/invalid/goal-read.{missing-allowed-actions,invalid-graph-edge,invalid-accounting}.json` | 2 valid + 3 invalid fixtures for the Goal read (draft plan offer; active goal with dependency edges, settlement attention, unknown consumption and an external wait). |
| `contracts/fixtures/manifest.json` | 219 → 228 entries; the first 219 are byte-identical (`jq -S` comparison of `.[:219]` before/after). |
| `contracts/scripts/{check,check-effect,generate}.mjs` | The hardcoded schema count 17 → 19 and both presentation-read sets gained the two new roots. |
| `contracts/ts/index.ts` | `AttentionRead`/`GoalRead` boundaries exported on the presentation-read parser. |
| `contracts/generated/**`, `daemon/internal/contracts/{generated.go,schema_bundle.json}` | Regenerated; 19 roots. |

No existing schema, fixture, error code, status code or permission rule changed.

### Control

| File | Change |
| --- | --- |
| `control/lib/symmetry_control_web/router.ex` | `GET /portal/api/attention` is declared before `/goals/:goal_id` (Phoenix matches in declaration order, so `attention` was previously captured as a goal id) and `GET /portal/goals/:goal_id` serves the SPA document. Both additive. |
| `control/lib/symmetry_control_web/controllers/portal_api_controller.ex` | `attention/2`: validates `project_id`/`cursor`/`limit`, merges the durable Goal attention page with the workspace blocked items, and answers HTTP 200 with `goal_attention.unavailable` when the Goal feed refuses. Caller-input errors (malformed cursor, unknown parameter) stay 400. |
| `control/lib/symmetry_control_web/controllers/goal_controller.ex` | The portal Goal read is projected through `PortalJSON.goal/3`; the operator `/api/v1` projection is untouched. `events` filters may be `after` and `limit` (capped at 100). |
| `control/lib/symmetry_control_web/portal_json.ex` | `attention/3` and `goal/3`: the portal projections. Every authority member is copied from the durable projection; the only additions are the project label and the decision lock version the resolver must present. `blocked_work_items/1` treats an unloaded association as "not read", never as "no blocked work". |
| `control/lib/symmetry_control_web/portal_session.ex` | `@shell_path` accepts `/portal/goals/<id>` so a signed-out Goal deep link survives login, in the same anchored style as `/portal/work/<id>`. No other path, query or value is accepted. |

No migration, no permission widening, no Goal admission rule changed.

### Frontend

| File | Change |
| --- | --- |
| `frontend/src/features/goals/detail.tsx` | New `/portal/goals/:goalId` surface: objective, accepted progress and per-item acceptance, blockers and settlement attention (recommendations labelled advisory), decisions with their served version, the server's `allowed_actions`, budget reservation/consumption with explicit unknown, waiting-external boundaries, the read-only dependency projection and paginated receipts. The Objective section renders the served lifecycle state, and the accepted-progress banner reads the served `ready?` member; both were found missing when the first real browser run reported the version the page still showed after a server-admitted plan. |
| `frontend/src/features/goals/served-text.ts` | Scalar rendering for open-JSON members, so an object can never render as `[object Object]`. |
| `frontend/src/features/attention/attention.tsx` | `/portal/attention` now renders the merged feed with the server cursor, states an unavailable Goal feed while keeping the project blockers, and offers goal creation through the accepted creation command. The workspace read it uses for the project scope is presented as this surface's own loading and error state, so the pre-existing shell acceptance for a held/aborted workspace read keeps passing on the new route. |
| `frontend/src/services/api.ts` | `attention`, `goal`, `goalEvents`, `goalContext` reads on the generated boundaries; `postGoalCommand`/`postPortalCommand` transport returning `MutationRefused` with exactly the members the server sent. |
| `frontend/src/services/commands.ts` | Canonical JSON + sha256 (the server's `RequestHash.canonical/1` encoding), UUIDv4 mutation identity, command envelope builder. |
| `frontend/src/app/{application,router}.tsx` | `/goals/$goalId` route, attention/goal query options, `fetcher` exposed for command POSTs, `safeReturnTo` accepts the Goal path. |
| `frontend/src/styles.css` | Goal surface styles only. |

### Tests and acceptance

| File | Change |
| --- | --- |
| `control/test/symmetry_control_web/controllers/portal_goal_attention_test.exs` | 9 new tests: the merge, the rollout-disabled page, cursor pagination with limit bounds and malformed input, the portal Goal read (project label, served decision version, operator projection untouched), conflict/replay on portal commands, context ownership refusal, portal authentication, and the unloaded-association regression. |
| `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs` | The manifest coverage test routes `attention-read` and `goal-read` through the canonical bundle schemas, as the 0007b presentation reads already were; without this the new roots have no Elixir-side fixture expectation. |
| `frontend/src/features/goals/goal-attention.test.tsx` | 10 new tests: merged listing, unavailable Goal feed with blockers kept, cursor pagination without repeats, goal creation refusal, the Goal read render (accepted progress, advisory recommendation, budget truth, read-only graph, receipts pagination), stale conflict with no silent overwrite, per-attempt mutation identity, achieve payload from the accepted subject, action-list fidelity, and command transport failure. |
| `frontend/src/app/test-harness.tsx`, `frontend/src/services/api.fixtures.ts` | Test-only render harness and served-shape fixtures for the new reads. |
| `frontend/src/app/router.test.tsx` | The attention-package route now answers the merged read path, so the existing 0007a/0007b attention scope cases keep asserting the real surface. |
| `browser/tests/portal-goal-attention.spec.mjs` | New acceptance spec (7 cases) on both widths: merged attention + Goal-feed refusal page, plan approval/activation/pause/resume/amendment with server versions, stale-version refusal and reload, mutation replay and identity conflict, read-only dependency projection with receipts, refusal paths (context 404, unauthenticated 401), and disconnect/retry recovery. |
| `browser/tests/portal.spec.mjs`, `browser/README.md` | Classic attention checks retired: the `#attention-list` keyboard walk (the classic rail walk that the Goal attention lists replace) and the stale "Attention filtering is read on the new routes" comment. Nothing else changed in the classic suite; the removed list is stated in the README. |

## Contract clause → evidence

| Contract clause | Evidence |
| --- | --- |
| create, plan approval, decision resolution, activation, pause/resume, amendment on real Goal APIs → browser flows | `browser/tests/portal-goal-attention.spec.mjs` "plan approval, activation, pause, resume and amendment run through the delegated commands with server versions": a real Goal created through `POST /portal/api/projects/:id/goals`, a real plan decision, the surface resolving it (decision resolution) and admitting the plan, then activate/pause/resume/amend, each accepted by the server and followed by a fresh read that reports revision 2. |
| achievement on real Goal APIs → the amended achieve semantics | The control suite's existing achievement coverage runs the whole loop against the real database and command path: `mix test test/symmetry_control/goals_test.exs` includes "terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop", "a Goal-level operator acceptance predicate is satisfied by the operator achieve", "a single-required plan admitted without an integration designation achieves without the payload field" (the amended exactly-one-required-item binding), "a conflicting stored optional integration marker refuses achieve for a single-required plan" and "a legacy unmarked single-required plan achieves without the payload field"; all pass inside the full-suite result below. The portal surface's achieve form is covered on the accepted subject and the served action list by `frontend/src/features/goals/goal-attention.test.tsx` "offers only the served actions and shows the achieve command with the accepted subject when the server allows it". A browser-driven achieve that first needs a completed task and an accepted outcome is not in this run — producing one requires a live daemon run, and it is listed under unresolved limits. |
| mutation replay → observed responses, never a silent overwrite | The browser replay case sends one envelope twice: both responses are HTTP 200 with the same `event.id` and the same `event.sequence`, a conflicting body for the same mutation identity answers `409 idempotency_conflict`, and the following read shows one revision advance and the version of the first receipt. |
| stale revision/version conflict → observed response and UI recovery | Browser stale case: while the surface holds version *n*/revision *r*, a second operator cancels the goal; the surface's amendment answers `409 stale` with `current_version`, `current_revision` and `allowed_actions`, renders "The goal changed since this page loaded" with that server state, and offers only "Reload the current state"; after the reload the cancelled state renders and the refused action is gone. The control test also covers the same refusal at the API level. |
| dependency projection renders read-only (no editable canvas) + context reads respect authorization | Browser projection case asserts the rendered node/edge lists, the "Read-only projection" label and that no dependency-editing control exists; the refusal case asserts a real `404 not_found` for an unknown/foreign context snapshot and `401 unauthenticated` without a session. `goal-read`'s graph members have no write affordance anywhere in the surface. |
| attention aggregation lists Goal attention and the pre-existing project blockers together and works when Goal rollout is unavailable | Browser merged case asserts both lists on one page from real reads; the same case then serves a Goal feed whose `unavailable` is `goal_admission_disabled` and asserts the refusal text plus the still-rendered project blockers. Control tests cover the server side of both. |
| pagination, refresh and disconnect recovery on the Goal surfaces | Browser: the attention cursor (`load more`), the receipts `after` pagination (bounded `limit`, page accumulation, no repeats) and the aborted-read case that renders the retryable error and recovers through "Try again" with the admitted work item present after reload. Frontend tests cover the cursor page accumulation and the receipt boundaries. |
| pre-existing browser acceptance continues on the new routes; this surface's classic checks retired in the same PR, removed list reported | The focused release-shell gate (`workspace-shell.spec.mjs` + `portal-work-views.spec.mjs`) is re-run unchanged on this revision, including its Attention scope/lifecycle cases. Retired in `browser/tests/portal.spec.mjs`: the `#attention-list` keyboard walk in "keyboard navigation, dialogs, and drawer remain usable without overflow" and the stale "Attention filtering are read surfaces covered on the new routes" comment; recorded in `browser/README.md`. |

## Verification on this revision

| Gate | Command | Result |
| --- | --- | --- |
| Contract generation | `pnpm contracts:generate` | generated, exit 0 |
| Contract check | `pnpm contracts:check` | `Validated 19 Draft 7 schemas and 228 fixtures`, `Effect decoded all 228 raw fixtures; 224 parsed documents reached Schema validation`, `Generated contract DTOs are up to date`, exit 0 |
| Contract runtime tests | `node --test contracts/scripts/*.test.mjs` (inside `contracts:check`) | 32 passed, 0 failed, exit 0 |
| Frontend typecheck | `pnpm --filter @symmetry/frontend typecheck` | exit 0 |
| Frontend lint | `pnpm --filter @symmetry/frontend lint` | exit 0 (0 warnings allowed) |
| Frontend unit/router | `pnpm --filter @symmetry/frontend test` | 8 files, 92 passed, exit 0 |
| Frontend build | `pnpm --filter @symmetry/frontend build` | built, exit 0 |
| Go vet | `go vet ./...` (daemon) | exit 0 |
| Go tests | `go test ./...` (daemon) | every package `ok` (including `internal/contracts`, `internal/protocol`, `internal/state`, `internal/workspace`, `cmd/symmetry-daemon`), exit 0 (9.6m) |
| Control focused | `mix test test/symmetry_control_web/controllers/portal_goal_attention_test.exs` | 9 passed, exit 0 |
| Control full | `mix test --warnings-as-errors` (control, `MIX_ENV=test`) | `848/849 passed, 21 skipped`, 1 failed: `SymmetryControl.Integrations.CommandTest` "provider token commands can keep stderr diagnostics out of stdout" — this host has no real PowerShell 7 install (only the 0-byte `%LOCALAPPDATA%\Microsoft\WindowsApps\pwsh.exe` App Execution Alias, which `System.cmd` cannot spawn: `{:error, {:command_unavailable, "pwsh"}}`), so the test is red on this host with or without this increment; it is the only remaining failure. The `contract_fixture_manifest_test` failure the first full run reported (new roots missing from the Elixir validator map) is fixed in this change set and passes: 1 passed. |
| Browser (new spec, desktop) | `playwright test tests/portal-goal-attention.spec.mjs --project=desktop-chromium` | 7 passed, exit 0 |
| Browser (new spec, mobile) | `playwright test tests/portal-goal-attention.spec.mjs --project=mobile-chromium` | 7 passed, exit 0 |
| Focused release-shell gate | `playwright test tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs` (both widths) | 33 passed, 3 skipped, exit 0 (7.3m) |
| Classic suite | `playwright test tests/portal.spec.mjs --project=desktop-chromium --workers=1` | 6 passed, 2 failed in the full-suite run; both failures are the classic page's shared `#toast` element read before its text is replaced, and each failing case passes on its own (see "Unresolved limits") |

## Unresolved limits

- The owner's rendered review (attention and Goal detail on real Goals, blocker and
  receipt truth, no raw policy internals) is not part of this run; the receipt
  states the result as produced and awaiting the owner's verdict.
- The plan proposal is authored by an operator through the accepted
  `request_decision(plan)` command in the acceptance fixtures. A planning task
  executed by a real harness produces the same decision and the surface approves it
  the same way, but this run does not include a harness-executed planning task, so
  that path is not measured here.
- The browser suite does not drive `achieve` end to end: reaching the server's
  achieve offer needs accepted work-item outcomes, which need a live daemon run
  (task claim → terminal result → validation). Achievement's real semantics are
  covered by the control suite against the real database and command path (see the
  clause table), and the portal achieve form is covered on the accepted subject by
  the frontend tests; the browser-driven form submission is the unmeasured part.
- `mix test` still reports `SymmetryControl.Integrations.CommandTest` "provider
  token commands can keep stderr diagnostics out of stdout" as failing on this
  host: bare `pwsh` resolves to a 0-byte WindowsApps execution alias that
  `System.cmd` cannot spawn. It is an environment failure in an untouched file,
  not an assertion this increment changed; the remaining 848 tests pass.
- `ads`/provider effects, resource synchronization, runtime control, Chat and the
  Goal admission rule are untouched by this increment and their own evidence stays
  where it was.
- The classic suite still owns the classic editing/resource/control flows (0007i
  owns the Portal cutover); only the attention checks this surface replaces were
  retired.
- The classic suite's shared `#toast` helper reads that one element for the whole
  mutation sequence, so under the full-suite run a listing such as
  `Expected: "Resource updated" / Received: "Resource attached"` reports the read
  beating the text replacement, not a wrong response. Two different pairs failed
  across two full runs, and each named case passed when run alone:
  `-g "project archive is read-only|two browser contexts reject a stale project
  update"` → 2 passed; the case names in the two full runs were
  "project, resource, work-item, filtering, ordering, and persistence" +
  "two browser contexts reject a stale work-item update" (2 failed, 6 passed,
  exit 1) and "project archive is read-only and restore keeps workspace state" +
  "two browser contexts reject a stale project update" (2 failed, 6 passed,
  exit 1). No classic assertion was changed; the classic page and its asset bundle
  are untouched by this increment.
