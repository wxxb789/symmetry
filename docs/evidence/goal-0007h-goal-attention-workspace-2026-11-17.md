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
receipt cannot change it.

For this working tree the source subject is
`sha256:d4ad9505ebf5736e91a63017d79c66c5f03f923cd703b8cd5c6c420664a9a175`
(1089 entries: every tracked non-`docs/**` file with the round's content, minus
the one deleted fixture; recomputed with `node .tmp/subject-digest.mjs` in this
tree, exit 0). Two earlier values are kept as history, each with the revision it
was produced on: the first receipt recorded `sha256:822bf31b…` over 1055 entries
(pre-rebase working tree of this branch); the independent review re-ran the
algorithm at head `4674568` before this fix round and measured
`sha256:3987fcae…` over 1090 entries. The exact final digest of the merge
revision will be recorded in the PR description.

## What changed

### Fix round after the independent review (head `4674568`)

The review found four P1 defects on the new surfaces and a systemic cause: the
served-read contract fixtures were hand-written, so schema names that disagree
with the server passed every gate. This round adds the gate that cannot see it
again and fixes every FIX finding.

| Finding | Change |
| --- | --- |
| systemic | New real-document gate (below) and the stated derivation rule: a fixture for a served read is a sanitized capture of a real response, member names untouched. The `attention-read`/`goal-read` fixtures are re-derived from captures of a live server on this host; the frontend's served-read fixtures import the same documents. |
| P1-1 | `contracts/v1/goal-read.schema.json` now names the graph members the server sends: `satisfied?` (and `cycle_detected?`), with the edge `id` nullable because the durable dependency row has no single-column identity; the surface reads those names and no longer keys graph edges by a null id. Regenerated DTOs/bundle. |
| P1-2 | Goal work items serve `integration` (no suffix) and the schema/client now say `integration`; the achieve envelope carries `integration_work_item_id` from a multi-item plan. |
| P1-3 | The receipts panel checks react-query's `error` for truthiness (`null` is the initial value in v5, so `!== undefined` was always true); the empty "No receipts" state is reachable and asserted. |
| P1-4 | `accounting.usage_cost_unknown?` is the server spelling; unknown consumption renders as unknown, never as zero. |
| P2-1 | `ready_to_achieve?` (attention entries) and settlement `required?` are the server spellings, required by the schemas where the projection always serves them, and pinned by server-shaped cases. |
| P2-2 | The substituted browser response and the unreachable `attention-read.goal-unavailable` fixture are deleted. The browser now drives the real rollout-off release instance with a Goal present and asserts what the server actually serves; the control test asserts the same real behaviour (the attention read is not gated by admission). |
| P2-3 | The blanket `rescue` in `PortalApiController` is gone: caller-input errors keep their protocol status, the Goal feed's own admission refusal is the one refusal stated inline at 200, and any other error surfaces as `500 internal_error` (red-before: a crafted cursor answered 200 with `invalid_request`; green-after: 500). |
| P2-4 | The plan-approval control is gated on the served decision's own members (open plan decision whose served options carry `accept`) and presents the served option id; the exception is documented in the code and here, and pinned by cases for a decision without the option and a resolved decision. |
| P2-5 | The dead `goalContext` client read is removed. The Goal surface renders no context snapshot because the read serves no snapshot index; the authorization boundary stays browser-covered by the real 401/404 refusals (listed under Unresolved limits). |
| P2-6 | This receipt was re-run and rewritten on the current head: one current number per measurement, coverage claims matched to what was executed (see the clause table). |
| P2-7 | The subject digest is rebound to this tree; the earlier value is kept as history with its revision named. |
| P3-1 | The classic `#attention-list` keyboard walk is migrated, not dropped: the new spec's keyboard case focuses the attention Goal link, opens the Goal surface with Enter, asserts focus lands on the destination heading, and asserts the attention heading regains focus on history back. |
| P3-2 | Fixture/schema fidelity: the `projects` member the server never serves is gone from the schema and fixture; the manifest/self-assertion and the missing tests are fixed (see Tests and acceptance). |

### Contract (additive descriptive reads, no wire change)

| File | Change |
| --- | --- |
| `contracts/v1/attention-read.schema.json` | New Draft 7 root: the merged attention read (Goal attention feed + project blocked items + the per-feed `unavailable` statement). `additionalProperties: true`, unconsumed members stay additive. Member names are the server's own spellings (`ready_to_achieve?`, settlement `required?`), and the never-served `projects` member was removed. |
| `contracts/v1/goal-read.schema.json` | New Draft 7 root: the portal Goal read. Names match the durable projection: `required?`, `accepted?`, `integration`, `ready?`, `all_required_accepted?`, `current_revision?`, `no_open_decisions?`, `no_nonterminal_tasks?`, graph `satisfied?`/`cycle_detected?`, accounting `usage_cost_unknown?`, settlement `required?` (all required where the projection always serves them); the graph edge `id` is nullable. |
| `contracts/fixtures/valid/attention-read.basic.json`, `contracts/fixtures/valid/goal-read.{draft,active}.json` | Re-derived from captures of a live server's own responses (identifiers/labels/timestamps sanitized only). The active capture carries a dependency edge, an integration work item and the resolved plan decision. |
| `contracts/fixtures/invalid/attention-read.{missing-project-blocked,non-uuid-goal}.json`, `contracts/fixtures/invalid/goal-read.{missing-allowed-actions,invalid-graph-edge,invalid-accounting}.json` | Derived from the same captures with one injected defect each, so the invalid reason stays the tagged one. |
| `contracts/fixtures/valid/attention-read.goal-unavailable.json` | Deleted: no real request produces it (the attention read is not gated by Goal admission). |
| `contracts/fixtures/manifest.json` | 235 → 234 entries in this round (the unreachable `attention-read.goal-unavailable` fixture removed); the served-read captures were rewritten in place. The first receipt's 228 figure was the pre-rebase count. |
| `contracts/scripts/{check,check-effect,generate}.mjs` | The hardcoded schema count is 20 and both presentation-read sets contain the two new roots. |
| `contracts/README.md` | States the derivation rule and the two gates that keep it honest. |
| `contracts/generated/**`, `daemon/internal/contracts/{generated.go,schema_bundle.json}` | Regenerated; 20 roots. |

### Control

| File | Change |
| --- | --- |
| `control/lib/symmetry_control_web/router.ex` | `GET /portal/api/attention` is declared before `/goals/:goal_id` and `GET /portal/goals/:goal_id` serves the SPA document. Both additive. |
| `control/lib/symmetry_control_web/controllers/portal_api_controller.ex` | `attention/2`: validates `project_id`/`cursor`/`limit`, merges the durable Goal attention page with the workspace blocked items, states the Goal feed's admission refusal inline, and answers unexpected Goal-feed faults as `500 internal_error` instead of a 200 body with a client-error code. |
| `control/lib/symmetry_control_web/controllers/goal_controller.ex` | The portal Goal read is projected through `PortalJSON.goal/3`; `events` filters may be `after` and `limit` (capped at 100). |
| `control/lib/symmetry_control_web/portal_json.ex` | `attention/3` and `goal/3`: the portal projections. Every authority member is copied from the durable projection; `blocked_work_items/1` treats an unloaded association as "not read". |
| `control/lib/symmetry_control_web/portal_session.ex` | `@shell_path` accepts `/portal/goals/<id>` so a signed-out Goal deep link survives login. |
| `control/test/symmetry_control_web/controllers/portal_goal_attention_test.exs` | 13 tests, including the real-document gate: it builds a real Goal with a dependency edge, an integration work item, a settlement receipt and an unknown-cost usage leaf, serves it through `PortalJSON.goal/3`/`PortalJSON.attention/3`, and validates both documents against the schema bundle. |
| `control/test/symmetry_control_web/controllers/goal_controller_test.exs` | The operator events window (`after`/`limit`, the 100 cap and unknown-parameter refusal) is covered. |
| `control/test/symmetry_control_web/portal_session_test.exs` | `/portal/goals/<id>` is in both the accepted deep-link list and the rejected list. |
| `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs` | The manifest coverage test routes `attention-read` and `goal-read` through the canonical bundle schemas. |

### Frontend

| File | Change |
| --- | --- |
| `frontend/src/features/goals/detail.tsx` | New `/portal/goals/:goalId` surface. Reads the server's own member names (`integration`, `satisfied?`, `cycle_detected?`, `usage_cost_unknown?`, `ready?`); the receipts panel treats a null error as "no error"; plan approval is gated on the served open plan decision and its served acceptance option. |
| `frontend/src/features/attention/attention.tsx` | `/portal/attention` renders the merged feed with the server cursor and reads the served `ready_to_achieve?` member. |
| `frontend/src/services/api.ts` | `attention`, `goal`, `goalEvents` reads on the generated boundaries; `postGoalCommand`/`postPortalCommand`; the dead `goalContext` read was removed. |
| `frontend/src/services/api.fixtures.ts` | The served-read fixtures import the contract fixtures (real captures) and override values only, so a test cannot inject a spelling the server does not send. |
| `frontend/src/services/commands.ts` | Canonical JSON + sha256, UUIDv4 mutation identity, command envelope builder. |
| `frontend/src/app/{application,router}.tsx` | `/goals/$goalId` route, attention/goal query options, `fetcher` exposed for command POSTs, `safeReturnTo` accepts the Goal path. |

### Tests and acceptance

| File | Change |
| --- | --- |
| `frontend/src/features/goals/goal-attention.test.tsx` | 14 tests: the merged attention listing with the served readiness badge, a served Goal-feed refusal statement, cursor pagination, goal creation refusal, the Goal read render (accepted progress, advisory recommendation, budget truth incl. unknown, read-only graph incl. the served edge and cycle flag), stale conflict, per-attempt mutation identity, the multi-item achieve payload, action-list fidelity, transport failure, the empty receipts state, and the three plan-approval gate cases. |
| `frontend/src/app/application.test.ts` | `safeReturnTo` accepts `/portal/goals/<id>` and rejects a deeper or traversing Goal path. |
| `browser/tests/portal-goal-attention.spec.mjs` | 9 cases per width: merged attention from real reads, plan approval/activation/pause/resume/amendment, stale-version refusal and reload, mutation replay and identity conflict, read-only projection with receipts, refusal paths (context 404, unauthenticated 401), disconnect/retry recovery, the real rollout-off instance with a present Goal, and the migrated keyboard walk. |
| `browser/tests/portal.spec.mjs`, `browser/README.md` | The classic `#attention-list` keyboard walk is retired there and migrated to the new spec's keyboard case; the removed list is stated in the README, including the rollout-off instance requirement. |

## Contract clause → evidence

| Contract clause | Evidence |
| --- | --- |
| create, plan approval, decision resolution, activation, pause/resume, amendment on real Goal APIs → browser flows | `browser/tests/portal-goal-attention.spec.mjs` "plan approval, activation, pause, resume and amendment run through the delegated commands with server versions": a real Goal created through `POST /portal/api/projects/:id/goals`, a real plan decision, the surface resolving it and admitting the plan, then activate/pause/resume/amend, each accepted by the server and followed by a fresh read that reports revision 2. |
| achievement on real Goal APIs → the amended achieve semantics | The control suite's existing achievement coverage runs the whole loop against the real database and command path (`mix test test/symmetry_control/goals_test.exs`, includes "terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop" and the exactly-one-required-item bindings). The portal achieve *payload* is covered on the accepted subject by the frontend test; the browser does not drive the achieve form end to end (see Unresolved limits). |
| mutation replay → observed responses, never a silent overwrite | The browser replay case sends one envelope twice: both responses are HTTP 200 with the same `event.id` and `event.sequence`, a conflicting body for the same mutation identity answers `409 idempotency_conflict`, and the following read shows one revision advance. |
| stale revision/version conflict → observed response and UI recovery | Browser stale case: while the surface holds version *n*/revision *r*, a second operator cancels the goal; the surface's amendment answers `409 stale` with `current_version`, `current_revision` and `allowed_actions`, renders the conflict with that server state, and offers only "Reload the current state"; after the reload the cancelled state renders and the refused action is gone. The control test covers the same refusal at the API level. |
| dependency projection renders read-only (no editable canvas) + context reads respect authorization | Browser projection case asserts the rendered node/edge lists, the "Read-only projection" label and that no dependency-editing control exists; the refusal case asserts a real `404 not_found` for an unknown/foreign context snapshot and `401 unauthenticated` without a session. No context snapshot is *rendered* on this surface: the Goal read serves no snapshot index (see Unresolved limits). |
| attention aggregation lists Goal attention and the pre-existing project blockers together and works when Goal rollout is unavailable | Browser merged case asserts both lists on one page from real reads and the served document behind them; the rollout-off case runs against a real instance started with `SYMMETRY_GOALS_ROLLOUT_ENABLED=false`, asserts the served Goal entry with `unavailable: null`, and asserts the page renders the entry and the blockers without stating an unavailable feed. The control test asserts the same server behaviour. The refused-feed *statement* is a unit case in the frontend tests. |
| pagination, refresh and disconnect recovery on the Goal surfaces | Executed: the attention cursor and the receipts `after` boundary are covered at the control and frontend-unit level (page accumulation, no repeats, bounded `limit`); the browser renders the newest receipts page over the real `after=0` read. **Not executed**: no browser click loads an older attention page or an older receipts page (the spec has no `load more`/`Load older receipts` interaction), so the browser does not claim those paths. *(Amended 2026-11-20, PR #116: the served `after=0` page is the oldest slice and the browser list heads with it; the receipts boundary control loads newer receipts and is labelled "Load newer receipts". This amendment supersedes the "newest receipts page" direction wherever this record states it; the older-page limit stands.)* |
| pre-existing browser acceptance continues on the new routes; this surface's classic checks retired in the same PR, removed list reported | The focused release-shell gate (`workspace-shell.spec.mjs` + `portal-work-views.spec.mjs`) is re-run unchanged on this revision, including its Attention scope/lifecycle cases. The classic keyboard walk is retired from `portal.spec.mjs` and migrated to the new spec's keyboard case; the removed list is recorded in `browser/README.md`. |

## Verification on this revision

Gate table (all commands executed in this working tree; `pnpm` is the pinned
`pnpm@12.17.0` binary because this host's default pnpm (12.21.0) refuses the
repository's `packageManager` pin):

| Gate | Command | Result |
| --- | --- | --- |
| Contract gate (composite) | `pnpm contracts:check` (pinned pnpm) | exit 0: `Validated 20 Draft 7 schemas and 234 fixtures.`; `Effect decoded all 234 raw fixtures; 230 parsed documents reached Schema validation`; `pass 33 / fail 0`; `Generated contract DTOs are up to date.` |
| Contract fixture red-before | `node contracts/scripts/check.mjs` before the schema fix | exit 1: `fixture goal-read.active expected schema_valid=true, got false: /graph/edges/0 must have required property 'satisfied'; /graph/edges/0/id must be string` |
| Real-document gate | `mix test test/symmetry_control_web/controllers/portal_goal_attention_test.exs` | 13 passed, exit 0 (red-before captured before the schema fix: `Required property satisfied was not present` on the real edge) |
| Frontend typecheck | `pnpm --filter @symmetry/frontend typecheck` | exit 0 |
| Frontend lint | `pnpm --filter @symmetry/frontend lint` | exit 0 (0 warnings allowed) |
| Frontend unit/router | `pnpm --filter @symmetry/frontend test` | 11 files, 184 passed, exit 0 (red-before per finding captured by reconstructing the pre-fix spellings: 5 targeted cases failed, including the achieve payload, the unknown budget render, the empty receipts state, the readiness badge and the approval gate) |
| Frontend build | `pnpm --filter @symmetry/frontend build` | built + staged, exit 0 |
| Go vet | `go vet ./...` (daemon) | exit 0 |
| Go tests | `go test ./...` (daemon) | final run: every package `ok` (21 packages, including `internal/contracts`, `internal/protocol`, `cmd/symmetry-daemon`), exit 0 (2.4m). First run: exit 1 with `cmd/symmetry-fake-agent` `TestExitOnFileRejectsInvalidPath/empty` "timed out waiting for fixture"; that sub-case passes in isolation (`go test ./cmd/symmetry-fake-agent -run TestExitOnFileRejectsInvalidPath -count=1`, exit 0) and the full re-run is clean, so it was a load-induced timing flake on this host, not a code failure |
| Control focused | `mix test test/symmetry_control_web/controllers/portal_goal_attention_test.exs test/symmetry_control_web/controllers/goal_controller_test.exs test/symmetry_control_web/portal_session_test.exs test/symmetry_control/goals/contract_fixture_manifest_test.exs` | 31 passed, exit 0 |
| Control full | `mix test --warnings-as-errors` (control, `MIX_ENV=test`) | `874/875 passed, 21 skipped`, exit 3; the single failure is the pre-existing host environment failure `SymmetryControl.Integrations.CommandTest` "provider token commands can keep stderr diagnostics out of stdout" (`{:error, {:command_unavailable, "pwsh"}}` — the 0-byte WindowsApps alias `System.cmd` cannot spawn), an untouched file this increment does not modify |
| Browser (new spec, desktop) | `playwright test tests/portal-goal-attention.spec.mjs --project=desktop-chromium` with `SYMMETRY_PORTAL_URL=http://127.0.0.1:4100`, `SYMMETRY_GOALS_OFF_URL=http://127.0.0.1:4102` | 9 passed, exit 0 (2.0m) |
| Browser (new spec, mobile) | same, `--project=mobile-chromium` | 9 passed, exit 0 (1.2m) |
| Focused release-shell gate | `playwright test tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs` (both widths) with `SYMMETRY_PORTAL_URL=http://127.0.0.1:4100`, `SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4101` (a true-TTL instance) | 35 passed, 1 skipped, exit 0 (5.0m) |
| Classic suite | `playwright test tests/portal.spec.mjs --project=desktop-chromium --workers=1` | 4 passed, exit 0 (26.5s; the keyboard case now carries the migrated-walk comment) |

The rollout-off browser case requires the second instance; without
`SYMMETRY_GOALS_OFF_URL` it is skipped with that reason and never passed.

## Unresolved limits

- The owner's rendered review (attention and Goal detail on real Goals, blocker
  and receipt truth, no raw policy internals) is not part of this run; the
  receipt states the result as produced and awaiting the owner's verdict.
- **Context snapshots are not rendered on the Goal surface.** The Goal read
  serves no context-snapshot index, so the surface offers no context view; the
  dead client read was removed and only the authorization boundary is exercised
  (browser: real `404 not_found` and `401 unauthenticated` refusals against
  `/portal/api/goals/:goal_id/contexts/:snapshot_id`). Rendering a snapshot needs
  a server-served snapshot index, which this increment does not add.
- **Browser pagination depth.** The browser renders the newest receipts page; it
  does not click to older attention or receipts pages. Those boundaries are
  covered by the control suite and the frontend unit tests instead.
- The plan proposal is authored by an operator through the accepted
  `request_decision(plan)` command in the acceptance fixtures. A planning task
  executed by a real harness produces the same decision and the surface approves
  it the same way, but this run does not include a harness-executed planning
  task.
- The browser suite does not drive `achieve` end to end: reaching the server's
  achieve offer needs accepted work-item outcomes, which need a live daemon run.
  Achievement's real semantics are covered by the control suite against the real
  database and command path, and the portal achieve payload is covered on the
  accepted subject by the frontend tests.
- The rollout-off browser case ran against a locally started release-style
  instance (`mix phx.server`, `SYMMETRY_GOALS_ROLLOUT_ENABLED=false`, port 4102)
  sharing the same database; the CI render job still executes only the focused
  release-shell gate, so this case is skipped in CI unless
  `SYMMETRY_GOALS_OFF_URL` is provided.
- `mix test` still reports `SymmetryControl.Integrations.CommandTest` "provider
  token commands can keep stderr diagnostics out of stdout" as failing on this
  host: bare `pwsh` resolves to a 0-byte WindowsApps execution alias that
  `System.cmd` cannot spawn. It is an environment failure in an untouched file,
  not an assertion this increment changed.
- `ads`/provider effects, resource synchronization, runtime control, Chat and the
  Goal admission rule are untouched by this increment and their own evidence stays
  where it was.
- The classic suite still owns the classic editing/resource/control flows (0007i
  owns the Portal cutover); only the attention checks this surface replaces were
  retired (the keyboard walk as a migration).
- The classic suite's shared `#toast` helper reads that one element for the whole
  mutation sequence, so under a full-suite run a listing can report the read
  beating the text replacement rather than a wrong response; the previously
  observed failures passed when run alone and no classic assertion was weakened.
