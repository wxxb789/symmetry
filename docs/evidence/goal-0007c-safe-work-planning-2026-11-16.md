# 0007c safe work planning — evidence receipt

Status: **produced; awaiting the owner's verdict**. The increment's committed
head is `724b0c87e037b9493127512a6eaee7085486b0d3` (PR #35) on main's
`e6e4b03`/`d781ba7`; the PR-bot repair round of 2026-11-17 — the client-minted
creation identity with its replay, the rendered empty board destinations, the
work-item editor and its draft owner lifted above the responsive branches, and
this receipt update — is one uncommitted working tree over that head, and every
number below was re-measured on that working tree on 2026-11-17. The union
review's repairs (receipt rebinding, the restored failed-GET guard, the CSS
declaration cleanup and the README correction) are committed in `724b0c8`. The
goal file stays in `docs/goals/` (not archived) and this receipt is the closing
evidence. No commit, push or history rewrite was performed by the implementing
agent. Every number below names the revision it was measured on; the earlier
rounds' values kept as history (`af7f3bc`'s repaired tree, the pre-union
`2525a7e`) are not evidence for this tree.

## What this increment contains

Control (`control/`):

- `lib/symmetry_control/workspaces/schemas.ex` — public
  `WorkItem.goal_managed?/1` (goal id + admitted revision + acceptance
  contract, the all-or-nothing predicate the database enforces).
- `lib/symmetry_control/workspaces.ex` — workspace read delegates goal
  membership to that predicate.
- `lib/symmetry_control_web/portal_json.ex` — the work-item projection serves
  `goal_managed` and the existing `external.provider_owned_fields`.
- `lib/symmetry_control/integrations/provider_access.ex` — the Goal-owned work
  item lookup uses the public predicate instead of a private duplicate.
- Tests: `test/symmetry_control/workspaces_test.exs`,
  `test/symmetry_control_web/controllers/portal_controller_test.exs`.

Contracts (`contracts/`, regenerated, no wire break):

- `v1/workspace-read.schema.json`, `v1/work-detail-read.schema.json` — optional
  `goal_managed`, `provider_owned_fields`, `version`, `default_agent_profile`,
  `default_workspace`, `kind`, `name` members the surfaces already served.
- Generated: `generated/ts/*.ts`, `generated/effect/index.ts`,
  `daemon/internal/contracts/generated.go`,
  `daemon/internal/contracts/schema_bundle.json`.

Frontend (`frontend/src/`):

- `services/api.ts` — mutation calls, typed `ServerRefusal`/`Network`/
  `UnexpectedResponse` failures, receipt-consuming calls.
- `features/planning/write-policy.ts` — served-statement rules: Goal
  membership is served never inferred, provider-owned controls come from the
  served `provider_owned_fields` list, `hasServedRevision` guards every write,
  move policy (Goal-managed and provider-owned `state` columns have no status
  change), failure modes (`fields` / `retryable` / `authoritative`).
- `features/planning/editors.tsx` — project and work-item editors with
  field-level refusals, provider-owned disabled controls, frozen-intent retry.
- `app/session.tsx`, `app/application.ts` — session token provider, mutation
  cache with `retry: false`, `SessionTokenProvider`.
- `features/projects/projects.tsx`, `features/projects/work-surface.tsx`,
  `features/work/detail.tsx`, `app/shell.tsx`, `app/router.tsx`,
  `features/attention/attention.tsx`, `styles.css` — create/edit/archive/
  restore entry points, board keyboard move and reorder with focus
  restoration, ownership chips and read-only notices.
- `features/planning/planning.test.tsx` — unit coverage on the fake control
  plane.

Browser acceptance (`browser/`, `scripts/ci/`):

- `tests/portal-work-planning.spec.mjs` — the real-API planning acceptance.
- `tests/planning-fixtures.mjs` — applies the two unwritable item kinds.
- `tests/readable-fixtures.mjs` — one additive option on the shared `capture`
  helper (viewport framing for a capture taken with a modal open).
- `scripts/run-planning-tests.ps1` — its harness (real control plane, dedicated
  database, per-run operator token, skips become failures).
- `scripts/ci/portal-planning-fixture.exs` — the fixture itself.
- `scripts/ci/0007c-subject-digest.ps1` — the evidence-subject digest algorithm
  of record (committed in this repair round; see "Measurement and evidence
  subject").
- `tests/portal.spec.mjs`, `README.md` — classic retirement and documentation.
- `frontend/src/services/api.test.ts`, `frontend/src/features/planning/planning.test.tsx`
  — the unit cases for the cleared-field, ownership-flip, revision-loss and
  server-fault behaviours, plus the failed-GET status-surfacing case added in
  this repair round.

Added by the PR-bot repair round (2026-11-17, working tree over `724b0c8`):

- `control/priv/repo/migrations/20261117000000_add_work_item_creation_identity.exs`
  — nullable `work_items.creation_id` plus the partial unique index
  `work_items_project_creation_id_index` on `(project_id, creation_id) WHERE
  creation_id IS NOT NULL`; explicit up/down.
- `control/lib/symmetry_control/workspaces/schemas.ex`,
  `control/lib/symmetry_control/workspaces.ex`,
  `control/lib/symmetry_control_web/controllers/portal_api_controller.ex` — the
  optional creation identity, the replay that returns the original row as
  `{:replayed, row}`, and the 201/200 `created`/`replayed` receipts.
- `control/test/symmetry_control/workspaces_test.exs`,
  `control/test/symmetry_control/migrations/add_work_item_creation_identity_test.exs`,
  `control/test/symmetry_control_web/controllers/portal_controller_test.exs` —
  the replay/identity/idempotency control tests and the migration up/down test.
- `frontend/src/services/api.ts`, `frontend/src/services/api.test.ts` —
  optional `WorkItemDraft.creation_id`, the `WorkItemReceipt` disposition and
  its decode pins.
- `frontend/src/features/planning/editors.tsx` — the draft owner split from the
  dialog shell (`presentation`/`drawerHost` placement), the per-draft creation
  identity and the replayed-creation notice.
- `frontend/src/features/work/detail.tsx` — the editor, notices, pending-write
  count and drawer host lifted out of the responsive branches.
- `frontend/src/features/projects/filters.ts`,
  `frontend/src/features/projects/work-surface.tsx`,
  `frontend/src/features/projects/filters.test.ts` — every supported state
  renders a board column, empty columns are destinations, and the empty-column
  contract is pinned.
- `browser/tests/portal-work-planning.spec.mjs`, `browser/README.md` — the four
  new real-API cases (empty-column destinations ×2, lost-response create
  replay, draft across the breakpoint) and the documentation.
- `docs/design/data.md`, `docs/design/frontend.md`,
  `docs/plans/2026-11-16-0007c-safe-work-planning-plan.md` — the additive
  contract documentation for the creation identity and the corrected board/
  editor notes.
- `frontend/src/features/planning/write-policy.ts` — the pending creation
  identity per project (`workItemCreationId` / `acknowledgeWorkItemCreation`),
  released only by an acknowledged create.
- `control/priv/portal_assets/portal.js` — the retained classic create dialog
  mints and replays a per-draft creation identity too (audit-driven; see the
  independent-audit section).

## Done-when → evidence

| Contract clause | Evidence |
| --- | --- |
| Successful project and work-item mutations on real data | `portal-work-planning.spec.mjs`: "creates, edits, archives and restores a project on real data" and "creates a work item with the project's resources, edits it and moves status through the board" — real HTTP against the running control plane and PostgreSQL, both Playwright projects (desktop + Pixel 7). |
| Duplicate submissions avoided, explicit retry reuses the original intent | "submits a project and a work item once on a double submit" (two `requestSubmit()` calls in one task, exactly one request captured per action), "replays the failed intent verbatim on an explicit retry" (aborted request, then the retry carries the identical body), and — for the case where the response is lost *after* the server committed — "replays a lost create response without duplicating the work item": the first attempt reaches the control plane through `route.fetch()` (asserted 201), its response is destroyed, and the retry replays the frozen intent (identical body, same client-minted `creation_id`) while the workspace read holds exactly one item, whose receipt is the server's 200 `disposition: "replayed"`. |
| Optimistic-lock conflict surfaces visibly and re-syncs the newest server state; background refresh never replaces another selected resource's draft | "re-syncs to the newest project after a stale update in two contexts", "re-syncs to the newest work item after a stale update in two contexts" (two browser contexts on the same row; the stale writer sees `stale` + newest state, never a silent overwrite), "keeps an open draft across a background refresh and applies it against the loaded revision", and "keeps an open editor draft across the responsive breakpoint" (with the editor open, the viewport crosses 1280 → 900 → 1280: the drawer unmounts and remounts, the editor and its typed value survive both crossings — recorded per crossing in the case's observation JSON — and the save after the crossings applies the draft, which also proves the editor stayed the topmost modal because the click is not intercepted). |
| Cross-project races, refresh while editing, validation errors and recovery leave no corrupted visible state | "applies a late board move only to its own project" (the late move lands only on its own project; the selected project, its notices and its stored rows stay intact), "refuses a duplicate project key with the server's field error and keeps the draft", "refuses a whitespace title with the server's field error and keeps the draft" (server field errors in the open editor, drafts kept). |
| Keyboard board movement with focus restoration, Goal-managed and provider-owned items offer no writable path | "moves and reorders the focused board card with the keyboard" (column move, up/down reorder, unmoved-neighbour no-op, focus stays on the card, reload confirms the stored order), "moves work into a ready column that has no cards on the board" and "keeps the ready destination while the board is filtered to backlog" (every supported state renders a column, so a one-state board or a one-state filter still has a move destination: the card moves, the notice lands and the workspace read shows the new status), "a Goal-managed item offers no writable path and the server refuses legacy writes" (403 `goal_authority_required` for both legacy paths), "an external-tracker-owned item keeps its provider-owned fields and column unwritable" (409 `provider_owned`, disabled editor controls, no move request). All run at both widths. |
| Goal-managed run/retry/move routes to Goal admission, never the legacy paths | Server: the legacy update and move paths answer 403 `goal_authority_required` (API probe above; re-proved in the browser run by calling both endpoints from the page). Presentation: `features/work/detail.tsx` states the routing on the item and offers no writable control, `features/projects/work-surface.tsx` refuses the board move with the Goal-admission message and sends no request, and the board card carries the `Goal-managed` chip. Goal admission itself is `0007h` and is out of scope. |
| Archived project read-only except restore | "an archived project's board offers no create, edit or move path" asserts the archived notice, no create/edit controls, a visible restore control, no move request and an unchanged stored column. |
| View preferences stay view preferences: versioned browser-local storage under a deployment scope, no credentials, evidence, session handles or authoritative task data | `frontend/src/features/projects/saved-views.ts` (pre-existing, unchanged by this increment): `savedViewStorageKey = "symmetry.portal.saved-views"`, `savedViewVersion = 1` inside the stored document, scope = deployment origin, per-project entries, and the stored shape is only `{name, search}` literals decoded with a schema; the module documents and enforces that no authoritative or credential data is kept there. This increment adds no other browser-local store, and its receipt-consuming paths keep the session token out of storage. |
| Pre-existing browser acceptance continues on the new routes and classic checks are retired in the same change | Retirement list below. Re-run on the working tree over `724b0c8` (2026-11-17, two loopback servers, `symmetry_0007c_shell`): 44 cases, 42 passed, 2 skipped, 0 failed — both read-surface suites fully green. The two skips are the files' own pre-existing desktop-only cases on mobile. The classic resource-detach case that failed on the pre-union tree (`2525a7e`) passed at both widths again; it fails identically on the base revision's own copy of the file when it does fire, so the mechanism remains the documented timing-dependent classic-JS toast race, not a repaired assertion. |

## Maintained measurements

| What | Value | Revision |
| --- | --- | --- |
| Contract gate `pnpm contracts:check` | exit 0: typecheck, 33 tests passed / 0 failed, 18 schemas, 224 fixtures | working tree over `724b0c8` (2026-11-17) |
| Frontend unit tests | 9 files, 144 tests passed, exit 0 | working tree over `724b0c8` (2026-11-17) |
| Frontend build + stage | exit 0 (`vite build` + `scripts/stage-assets.mjs`), bundle `index-BctcJ1zX.js` (`sha256:2c3def7f…`) | working tree over `724b0c8` (2026-11-17) |
| Control format/compile | exit 0 | working tree over `724b0c8` (2026-11-17) |
| Control suite (`mix test`) | 850/851 passed, 21 skipped, 1 failed (`Integrations.CommandTest` "provider token commands can keep stderr diagnostics out of stdout" — `{:error, {:command_unavailable, "pwsh"}}`, the same environment-limited failure 0007d recorded at its own head, unchanged by this increment), exit 2 (452.7 s); the round's later `control/priv/portal_assets/portal.js` edit (static asset, not compiled) is bound by a re-run of its only suite assertion, 13 passed / exit 0 | working tree over `724b0c8` (2026-11-17) |
| Planning acceptance, red baseline (pre-fix bundle) | 14 passed / 4 failed — exactly the four new cases — 0 skipped (2.2 min; that invocation's exit code was masked by the capturing pipe, see the browser section) | pre-fix bundle `index-BNMC_fP3.js` (built from `724b0c8`) with the new spec, run `symmetry-0007c-planning-95171f7e` |
| Planning acceptance, desktop-chromium | 18 passed, 0 failed, 0 skipped, exit 0 (1.5 min) | working tree over `724b0c8`, run `symmetry-0007c-planning-ff2595d2` (the artifact carries `HARNESS_EXIT=0`) |
| Planning acceptance, mobile-chromium (Pixel 7) | 18 passed, 0 failed, 0 skipped, exit 0 (1.5 min) | working tree over `724b0c8`, run `symmetry-0007c-planning-1319913a` (the artifact carries `HARNESS_EXIT=0`) |
| Pre-existing browser suites (classic + read surfaces) | 44 cases, 42 passed, 2 skipped, 0 failed (4.0 min), exit 0 | working tree over `724b0c8` (2026-11-17, two loopback servers on `symmetry_0007c_shell`) |
| Connections fixture harness (the 0007d surface) | 10 passed (5 flows × desktop + mobile), 0 skipped, exit 0 (47.7 s), canary scan clean | working tree over `724b0c8`, run `symmetry-0007d-connections-6afa5d77` |
| Evidence subject | source `sha256:2bab2d22…` (545 entries), full `sha256:49f5d12d…` (548 entries) | working tree over `724b0c8`, recomputed after the last source edit (2026-11-17) |

## Executed non-browser gates

Commands run from the worktree root unless a working directory is named. Every
row is a command actually executed — in the 2026-11-17 PR-bot repair round,
unless the row names an earlier round; a gate that was not executed is listed
under limitations instead.

| Gate | Command | Result |
| --- | --- | --- |
| Contract gate (the CI command) | `pnpm --offline contracts:check` from the repository root | exit 0: contract typecheck, 33 tests passed / 0 failed, "Generated contract DTOs are up to date", "Validated 18 Draft 7 schemas and 224 fixtures", Effect decoded all 224 raw fixtures (2026-11-17, this round; `.tmp/0007c-evidence/green-contracts-check.txt`) |
| Effect contract surface | `node contracts/scripts/check-effect.mjs` (also inside the gate above) | exit 0 (covered by the gate run above; not invoked separately in this round) |
| Contract unit tests | `node --test contracts/scripts/*.test.mjs` (what the gate runs) | 33 passed, 0 failed, exit 0 (2026-11-17, this round, inside the gate) |
| Frontend types | `pnpm --offline --filter @symmetry/frontend typecheck` | exit 0 (2026-11-17, this round, after the last source edit) |
| Frontend lint | `pnpm --offline --filter @symmetry/frontend lint` | exit 0 (`--max-warnings 0`; 2026-11-17, this round) |
| Frontend unit tests | `pnpm --offline --filter @symmetry/frontend test` | 9 files, 144 tests passed, exit 0 (2026-11-17, this round; the pre-round tree held 139 — this round adds two creation-identity cases in `api.test.ts`, two create-receipt cases in `planning.test.tsx` and one reopened-draft case, and rewrites three create cases' expected notice text to the new wording) |
| Frontend build + stage | `pnpm --offline --filter @symmetry/frontend build` | exit 0; `control/priv/static/portal` restaged as `assets/index-BctcJ1zX.js` (2026-11-17, this round, as the last source edit, before the green browser runs; the artifact carries `BUILD_EXIT=0`) |
| Control formatting | `mix format --check-formatted` in `control/` | exit 0 (2026-11-17, this round) |
| Control compile (warnings as errors) | `mix compile --warnings-as-errors` in `control/` | exit 0 (2026-11-17, this round) |
| Control suite | `mix test` in `control/` (database `symmetry_0007c_mixtest`) | 850/851 passed, 21 skipped, 1 failed, exit 2 (452.7 s), 2026-11-17, this round. The failure is `SymmetryControl.Integrations.CommandTest` "provider token commands can keep stderr diagnostics out of stdout": `{:error, {:command_unavailable, "pwsh"}}` at `test/symmetry_control/integrations/command_test.exs:25` — the same environment-limited failure 0007d recorded at its own head, unchanged by this round. This run's exit code was captured in the session echo (stdout read `Result: 1 failed, 21 skipped`); since it finished, the only control-tree edit is `control/priv/portal_assets/portal.js`, a static asset no compilation or test covers byte-wise, and the one suite assertion that reads it (served at `/assets/portal.js`, contains `loadWorkspace`) was re-run on the final tree: `mix test test/symmetry_control_web/controllers/portal_controller_test.exs --seed 0` — 13 passed, `MIX_TEST_EXIT=0` (`.tmp/0007c-evidence/green-portal-controller-focused.txt`). |
| Daemon vet | `go vet ./...` in `daemon/` | exit 0 (2026-11-17, this round) |
| Daemon contract packages | `go test ./internal/contracts ./internal/protocol -count=1` in `daemon/` | exit 0: `ok internal/contracts 3.385s`, `ok internal/protocol 3.105s` (2026-11-17, this round; `daemon/` is byte-identical to `724b0c8`) |
| Daemon app tests | `go test ./internal/app -count=1 -timeout 20m` in `daemon/` | exit 0 (`ok … 172.152s`, 2026-11-17, this round; the whole-tree `go test ./...` still hits the 10 min default deadline on this host under load, see limitations) |
| Failed-GET guard regression | `pnpm --offline --filter @symmetry/frontend test api.test.ts` with `frontend/src/services/api.ts` pre-fix vs restored guard | earlier round (2026-11-17 rebinding), kept as history: red 1 failed / 14 passed, exit 1 (`Network` for a 404 whose body read throws); green 15 passed, exit 0. The guard is unchanged this round and its case runs inside the frontend suite above |
| Evidence-subject digest | `pwsh -NoProfile -File scripts/ci/0007c-subject-digest.ps1` (then `-Scope full`), twice each | exit 0; source `sha256:2bab2d22ee262202456f595a9ccaa6bf6e09000f15d315d8af2d9be7eb82ce44` (545 entries), full `sha256:49f5d12d02ea68f6ba971f9a71044eb1bf62da6d3f705f8e9f4cccc102ae3b96` (548 entries), identical across repetitions (pwsh 7.6.9; the four artifacts carry `DIGEST_*_EXIT=0`) |

## Browser acceptance

`browser/scripts/run-planning-tests.ps1` is the executor. Each invocation
recreates the database `symmetry_0007c_test` on `127.0.0.1:55441`, starts the
repository's dev control plane on a loopback port it owns with a per-run
operator token, verifies the staged frontend build is served, runs the real-API
spec, and turns a skipped or empty summary into a failure. The two Playwright
projects are run as two invocations of the same revision so each width gets a
fresh database, token and fixtures:

| Run | Command | Result |
| --- | --- | --- |
| red baseline, desktop-chromium (pre-fix bundle) | `pwsh -NoProfile -File browser/scripts/run-planning-tests.ps1 -SpecArgs "--project=desktop-chromium"` with the pre-fix staged bundle `index-BNMC_fP3.js` (built from `724b0c8`) | run `symmetry-0007c-planning-95171f7e`: 14 passed, 4 failed — exactly the four new cases — 0 skipped (2.2 min); the existing 14 cases stayed green on the pre-fix bundle, so the four new cases are the discriminator. This red invocation's own exit code was masked by the capturing pipe (`tee | tail` reported the pipe's status); its log records Playwright's `4 failed` and the harness's `[ELIFECYCLE] Test failed` line. The two green runs below captured the harness exit code explicitly (`HARNESS_EXIT=0`). |
| desktop-chromium | `pwsh -NoProfile -File browser/scripts/run-planning-tests.ps1 -SpecArgs "--project=desktop-chromium"` | run `symmetry-0007c-planning-ff2595d2`: 18 passed, 0 failed, 0 skipped, `HARNESS_EXIT=0` (1.5 min) |
| mobile-chromium (Pixel 7) | `pwsh -NoProfile -File browser/scripts/run-planning-tests.ps1 -SpecArgs "--project=mobile-chromium"` | run `symmetry-0007c-planning-1319913a`: 18 passed, 0 failed, 0 skipped, `HARNESS_EXIT=0` (1.5 min) |

All three runs executed the same 18-case specification; the red run predates two
disclosed specification edits — the locator adjustment inside the breakpoint
case and the notice-text matcher in the replay case — and its four failures land
as follows: the two empty-destination cases failed on the missing `ready` column
(`expect(ready.getByRole("heading", { name: /^ready$/i })).toBeVisible()` at
lines 599 and 637), the replay case failed on the absent replay notice (line
690, under the pre-change matcher text), and the breakpoint case failed on the
editor being absent after resizing back (line 750). The first three discriminate
their defects directly; the breakpoint case's red failure predates its locator
adjustment, so its discrimination rests on the green pair and on its own
post-crossing assertions (editor visible, `Branch` value intact) rather than on
the red element-not-found line alone. The two green runs
served the same staged bundle (`control/priv/static/portal/assets/index-BctcJ1zX.js`,
built from this working tree before them), neither green run recorded a
control-plane restart, and each run artifact carries its exit line. 36 cases total,
0 skipped, every one over real HTTP against the running control plane and
PostgreSQL. The 18 cases are: project create/edit/archive/restore;
duplicate-key refusal with the server's field error; single submission on a
double submit; two-context stale project update; work-item create/edit with
resources and a board status move; whitespace-title refusal; explicit retry
after a transport abort (both captured bodies identical); a draft kept across a
background refresh; two-context stale work-item update; a late board move
landing only on its own project; keyboard column move, up/down reorder and
focus retention; Goal-managed item with server refusals; provider-owned item
with locked fields and no move request; archived project read-only with
restore; a create whose committed response is destroyed and whose retry is
replayed without a second row; a move into an empty `ready` column from a
backlog-only board; a move out of a board filtered to `state=backlog`; and a
work-item draft kept across the responsive breakpoint (1280 → 900 → 1280, with
the crossing observations recorded). Network interception appears only to hold,
abort or replay a captured request so a real request is replayed or released.

### Pre-existing browser acceptance on this revision

The classic and read-surface suites were run against the same revision on two
loopback servers started from this worktree (dev configuration, staged assets,
database `symmetry_0007c_shell`, one server with
`SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2` for the expiry case):

```
pnpm --offline --filter symmetry-browser-acceptance test \
  tests/portal.spec.mjs tests/portal-work-views.spec.mjs tests/workspace-shell.spec.mjs
```

Result on this working tree over `724b0c8` (2026-11-17, 4.0 min, exit 0):
**44 cases collected, 42 passed, 2 skipped, 0 failed**:
`portal-work-views.spec.mjs` 20 cases — 19 passed, 1 skipped (its own
desktop-only held-response race); `workspace-shell.spec.mjs` 16 cases — 16
passed; `portal.spec.mjs` 8 cases — 7 passed, 1 skipped (its own desktop-only
background-refresh drawer case). Both read-surface suites are fully green:
35 cases, 0 failures. The two skips are the files' own pre-existing
desktop-only concurrency skips (`portal-work-views.spec.mjs` held-response race
and `portal.spec.mjs` background-refresh drawer case, both skipped on mobile).
This round changes exactly the surfaces those suites read: the board now renders
every supported state (the read suite's exact five-column assertion still holds
because its fixture populates all five) and the detail's editor/draft owner
moved above the presentation switch (the drawer's own keyboard walk and focus
trap cases stay green).

The classic resource-detach case, `portal.spec.mjs` "classic resource attach,
update and detach with truthful health", failed at both widths on the pre-union
tree (`2525a7e`) and passed at both widths this round. When it fires it is
**pre-existing, not caused by this increment**: the unmodified base-revision
copy of the same file (`git show
7a92ca498f60309b97920b9df834f2637b91cfbd:browser/tests/portal.spec.mjs`, run as a
temporary probe file against the same servers back then) fails the identical
assertion at the identical step (`Received: "Resource updated"`), the mechanism
is the unchanged classic JS toast race
(`control/priv/portal_assets/portal.js:2408-2412` skips the toast when a newer
workspace load supersedes it), and no `portal.spec.mjs` or
`portal-work-views.spec.mjs` assertion was made conditional, skipped or
weakened. Nothing in this repair round touches that mechanism, so this round's
pass is the same timing-dependent case not firing.

The connection-management surface carried by the same bundle was re-run with
its own fixture harness on this working tree (the 0007d suite; this round
touches no CSS and no connection code, so the run re-confirms that the shared
bundle kept that surface intact):

| Run | Command | Result |
| --- | --- | --- |
| connections fixture harness, both widths | `pwsh -NoProfile -File browser/scripts/run-connection-fixture-tests.ps1` | run `symmetry-0007d-connections-6afa5d77`: 10 passed (5 flows × desktop-chromium + mobile-chromium), 0 skipped, exit 0 (47.7 s); canary scan clean |

The harness's refused-submission case keeps its viewport pin
(`scrollIntoViewIfNeeded()` + `toBeInViewport({ ratio: 1 })` on the dialog's
Cancel control) and passed at both widths.

## Repairs made while verifying (red before, green after)

1. **A window resize could navigate out from under an open editor.** Radix's
   layer stack asks the work-detail drawer to close on an unattributable
   dismissal (a resize, which a full-page capture triggers); the drawer ran its
   "close" navigation, the detail body remounted and the open work-item form
   disappeared mid-edit. Red: runs `symmetry-0007c-planning-41082af0` and
   `4858939a` failed "an external-tracker-owned item keeps its provider-owned
   fields and column unwritable" at `expect(editor).toBeVisible()` right after
   the capture (`element(s) not found`); `39989eaf` (which also recorded one
   control-plane restart) failed the same assertion in the refresh-draft case.
   Fix: the drawer and both planning editors only close on the
   operator's own dismissals (Escape, the overlay, the close control, Cancel);
   other layer requests are ignored. Green: the same case passes
   (`-g keeps`: 4 passed) and both full-width suites are 14/14.
2. **A breakpoint crossing dropped an open draft** (fixed in the PR-bot repair
   round below; the former limitation entry is removed): the detail
   presentation swaps trees at 1024 px, so the body remounted. Measured
   directly then: with the editor open, `page.setViewportSize({width: 900})`
   left the editor open but its fields empty, and the pre-fix tree removed it
   entirely. The provider-owned editor capture therefore frames the viewport
   instead of the full page, and the contract's own refresh scenario (the case
   that must keep a draft) is asserted unchanged.
3. **Harness robustness.** This host killed the BEAM mid-run more than once
   (process gone, empty stderr, no crash dump): run
   `symmetry-0007c-planning-39989eaf` reports "control plane restarted 1 time(s)
   during this run" and recovered to 11 passed / 3 failed where the same tree
   previously lost 8 cases to `ERR_CONNECTION_REFUSED`. The launcher now loops
   with a logged restart, stdin comes from an empty file, and the control
   plane's lifetime is printed with the run.
4. **Planner spec defects found by these runs and fixed**: the fixture argv
   separator (`mix run file.exs -- args` puts `--` into `System.argv/0`); the
   two unwritable-kind tests missing `testInfo`; the archived-state locator
   matching both the notice and the board hint; the two-context cases never
   navigating to their surface (and using a width-dependent locator); the
   whitespace-title expectation (the server trims, so the message is
   `can't be blank`); the refresh case clicking a control behind an open modal
   and asserting a blocker reason without the blocked flag; width skips removed
   so the two-context cases run at both widths.
5. **A mid-verification mobile run (`symmetry-0007c-planning-c62524ca`,
   13 passed / 1 failed) timed out in the two-context work-item case**, which
   still reached the desktop drawer instead of the narrow detail surface. The
   fix makes that case width-aware; that edit is the one line that separates the
   first-cited desktop run from the mobile one, and both widths were re-run on
   the frozen specification afterwards.

### Union-review repairs (2026-11-17, uncommitted over `af7f3bc`)

The union review of PR #35 (findings file
`.tmp/0007c-findings-union-review.md`, review scratch) raised one evidence
blocker and four smaller findings; all five were fixed in the working tree the
numbers above are bound to:

1. **The acceptance receipt did not bind the shipped revision.** The recorded
   530/533-entry digests were measured on the pre-rebase tree (`2525a7e`); the
   union head's frontend build had changed under the receipt. Fixed by
   recomputing both digests on this head with the same `pwsh 7.6.9` host (see
   "Measurement and evidence subject"; the old values stay as history with
   their revision named), committing the algorithm as
   `scripts/ci/0007c-subject-digest.ps1` (it existed only as untracked `.tmp`
   scratch, so until now the algorithm reproduced only from this receipt's
   prose) and re-running the suites whose subject changed: planning acceptance
   at both widths, the classic + read-surface + workspace-shell run, and the
   connections fixture harness. All numbers above are the re-run values.
2. **Dead CSS declarations silently re-tuned 0007d's connection dialogs.**
   `styles.css` had two `.field-error` and two `.dialog-actions` declarations
   (same `@layer`, source order decided), and the later pair — authored for the
   planning dialogs — changed the connection dialogs' actions row from main's
   accepted `margin-top: 12px` to `20px`. Fixed: one declaration per rule
   (`.field-error`, `.dialog-actions`), the planning dialogs' spacing scoped to
   `.planning-form .field-error` and `.planning-dialog .dialog-actions`, so the
   connection dialogs are back to main's tuned value and the planning dialogs
   keep the exact cascade they were accepted with. Verified by the connections
   fixture harness (its in-viewport pin on the refused-submission dialog's
   Cancel, both widths) and by the planning acceptance re-run.
3. **`.field-error`'s `-2px` margin bled into 0007d's forms.** The same merge
   left the margin on the shared class, where `connections.tsx` renders it
   inside main's forms that were styled without it. Fixed with the same scoping
   (the margin lives only under `.planning-form`).
4. **`api.ts` read a failed GET's body unconditionally** (main's line; the
   0007c guard was dropped by the union), so a failed GET whose body could not
   be read rejected as `Network` — which `readRetry` retries — instead of
   surfacing the status-derived error. Fixed by restoring `response.ok ||
   init.method !== "GET"`-gated body reads (a failed command still reads the
   body for its refusal JSON; a failed GET never parses it) and pinned with a
   new `api.test.ts` case: red under the merged line (`Network: Cannot reach
   the server` for a 404 with an errored body stream, `.tmp/0007c-evidence/red-api-get-body.txt`,
   exit 1), green with the guard (`UnexpectedResponse{status: 404,
   transient: false}`, `readRetry` false; `.tmp/0007c-evidence/green-api-get-body.txt`,
   exit 0). The full frontend suite then held 139 tests (138 before this case),
   0 failed; it stands at 144 after the PR-bot repair round.
5. **`browser/README.md` misstated the shell's mutation surface.** The
   connection-only sentence is replaced by the two current surfaces
   (connections and project/work-item planning), both of which the README
   covers below.

### PR-bot repair round (2026-11-17, uncommitted over `724b0c8`)

The PR-bot findings on PR #35 (head `724b0c8`; findings file
`.tmp/0007c-findings-pr-bot.md`) raised one P1 and two P2 defects; all three
were fixed as classes in this working tree. The red evidence is one desktop
run of the same frozen spec against the *pre-fix* staged bundle
(`index-BNMC_fP3.js`, built from `724b0c8`) and the new control plane: run
`symmetry-0007c-planning-95171f7e`, 14 passed / 4 failed — exactly the four new
cases, each failing at its defect (`ready` column absent twice, the replayed
notice absent after a retry that duplicated the row, and the editor missing
after the 900 px crossing). The green evidence is the same spec against the
final rebuilt bundle (`index-BctcJ1zX.js`, the last source edit of the round):
desktop run `symmetry-0007c-planning-ff2595d2` and mobile run
`symmetry-0007c-planning-1319913a`, 18 passed / 0 failed at each width, after
the audit round's frontend changes (the pending creation identity and the
replayed-notice wording); the pre-change pair (`-d7997be3`, `-8e0d0762`) and the
intermediate `-85ca970a` remain history.

1. **P1 — work-item creation was not idempotent, so a lost response plus the
   operator's retry duplicated the row.** `Workspaces.create_work_item/2`
   inserted unconditionally. Fixed additively: an optional client-minted
   `creation_id` (1..128 bytes, no NUL — the action-id rule) persists on
   `work_items.creation_id` under the partial unique index
   `work_items_project_creation_id_index` (`(project_id, creation_id) WHERE
   creation_id IS NOT NULL`); the project row lock that already serializes
   same-project creates makes the lookup-then-insert race-free; a replay of an
   existing identity returns that original row as `{:replayed, row}` with no
   write, and the API answers 201 `disposition: "created"` / 200
   `disposition: "replayed"`. The SPA mints the identity once per draft, keeps
   it in the frozen intent and replays it on every attempt of that draft; the
   success notice then reports the server's own statement ("Work item already
   existed; the retry returned the original row."). Contract: no request without
   the field changes behaviour (legacy callers and provider sync keep inserting
   with `creation_id` NULL), and the field is documented in
   `docs/design/data.md`, `docs/design/frontend.md` and the plan's §3. Project
   creation does **not** need the mechanism: `projects.key` is already unique
   (migration `20260904000000`), so a replayed identical create is refused with
   the field error ("has already been taken") instead of duplicating a row —
   the existing browser case asserts that refusal and the single stored project.
   Evidence: control tests — service replay/identity/scoping/invalid-identity
   (`workspaces_test.exs`), HTTP 201/200 receipts with the same row id and a
   different-identity second row (`portal_controller_test.exs`), and the
   migration's index scope plus up/down
   (`add_work_item_creation_identity_test.exs`); the real-API browser case
   "replays a lost create response without duplicating the work item"; and the
   frontend unit pins for the stable per-draft identity, the replayed notice and
   the receipt decode.
   Deviation from the repo's other idempotency precedents, stated explicitly:
   machine enrollment stores a request hash and answers a same-key/different
   request with `idempotency_conflict`, and the Goal command surface compares
   mutation ids plus request hashes. Work-item creation does **not** store or
   compare a request hash: the finding prescribes "a replay that returns the
   originally created row with a receipt marking it as a replay rather than a
   new creation", the client always replays a frozen intent verbatim, and even a
   changed draft retried under the same identity must not become a second row —
   the operator's truthful signal is the `replayed` disposition and the notice
   that names it, not a hash comparison that would dead-end the draft. Nothing
   in the Goal command contract or its request-hash rule is changed.
   The identity's lifecycle (audit-driven): the SPA keeps one pending identity
   per project in memory, not per editor instance, and releases it only when the
   server acknowledges a create (`created` or `replayed`). Closing and reopening
   the draft — or navigating away and returning — therefore still replays the
   original attempt, while a draft started after an acknowledged create mints a
   fresh identity. The notice for a replayed answer says the submission was not
   applied, so a changed retry cannot be mistaken for a second creation. The
   retained classic dialog mints its identity when the create form opens and
   replays it for every submission of that form (probe below); it keeps the
   pre-fix reopen behaviour until the classic surface is removed, listed under
   limitations. A replay against a project archived since the original attempt
   refuses with the ordinary `state_conflict` (the archived gate runs first, and
   no row is written or returned) — the archived read-only rule is not weakened
   by the identity.
   Project creation does not share this shape and keeps its existing guard: a
   project *is* identified by the operator-typed `key`, and
   `SymmetryControl.Workspaces.Schemas.project/1` carries
   `unique_constraint(:key)`, so a retry after a lost response is refused with
   the server's field error instead of inserting a second project — the visible
   refusal the existing real-API case "refuses a duplicate project key with the
   server's field error and keeps the draft" pins. A work item has no natural
   key (two items may share a title), which is exactly why its retry needs the
   minted identity.
2. **P2 — board movement could not reach an empty state.**
   `boardColumns(items)` returned only populated states, so a project whose
   items all sit in backlog (or a board filtered to one state) had no
   destination and `boardMoveRequest` returned null without a request. Fixed in
   the rendering, not by weakening the move rule: every supported state always
   renders a column (empty ones included, with their count and a placeholder),
   any served state outside the supported set keeps its own column and sorts
   last, and the keyboard move targets the neighbouring rendered column as
   before. The plan's §9.4 note and the former limitation are corrected
   accordingly; the create form's Status select is no longer the only way into
   an empty state. Evidence: the two new real-API cases (only-backlog project,
   board filtered to `state=backlog`), both red before the fix (no `ready`
   column) and green after; `filters.test.ts` pins the always-rendered column
   set and the empty-column move intent.
3. **P2 — the work-item editor unmounted across the responsive breakpoint and
   lost the draft.** The page owned `editing` inside the branch that swaps
   trees at 1024 px, so a crossing remounted the owner. Fixed by lifting the
   editor, its draft owner, the notices and the pending-write count above both
   presentations (`WorkDetailPage`), rendering the editor once in a stable
   position; the draft and frozen intent live above both presentations, the
   creation identity lives in the module-scope pending store (P1), and only the
   Radix dialog shell follows the presentation. On the
   desktop presentation the shell is portaled into a node inside the drawer's
   own dialog (`drawerHost`), so the drawer is an ancestor of the open editor
   and Radix's aria-hidden bookkeeping keeps the editor visible whichever
   dialog mounted first; the shell waits for that node to exist before it
   mounts, which also registers its layer above the drawer's. Evidence: the new
   real-API case crosses 1280 → 900 → 1280 with an open draft, records the
   drawer count and the typed value at each crossing, and saves after the
   crossings; it failed before the fix (editor gone at 900) and passes at both
   widths after. The receipt's former limitation entry is removed. One
   test-only adjustment inside that new case: the narrow page's heading is
   asserted as rendered content rather than through the accessibility tree,
   because an open modal editor correctly makes everything behind it inert
   (`aria-hidden`) — the case still proves the narrow presentation rendered
   (its own heading is visible, the drawer is gone) and that the editor stays
   usable after the crossing (the save click is not intercepted).

### Independent audit of this round (2026-11-17, two read-only auditors in parallel)

A Sol-high adversarial review and a DeepSeek mechanism-level audit both ran
read-only on this working tree (diff over `724b0c8` plus the receipt and the
`.tmp/0007c-evidence` artifacts). Both verified the three mechanisms
independently — the partial index and its project scope, the no-write replay
(no changeset, no position bump, no `lock_version` touch), the 201/200 receipt
split, the always-rendered columns against the suites' selectors, and the
editor's mount order, aria-hidden ownership and layer routing — and both
dispositions are recorded here. Findings and what changed because of them:

1. **The retained classic create dialog was still non-idempotent (Sol, P2).**
   `portal.js` posted the create form without an identity, so a lost response
   plus a resubmission duplicated the row there. Fixed: the classic dialog mints
   a 32-hex identity when the create form opens (`crypto.getRandomValues`, no
   secure-context requirement) and replays it on every submission of that form.
   Evidence: a real-browser probe of the classic dialog (throwaway script,
   deleted afterwards; `.tmp/0007c-evidence/red-classic-create-probe.txt` vs
   `green-classic-create-probe.txt`) — red was two stored rows, two `201
   created` attempts and no identity; green is one stored row, attempt 1 `201
   created`, attempt 2 `200 replayed`, both carrying the same `creation_id`.
   The classic dialog's *reopen* path keeps the pre-fix behaviour and is listed
   under limitations.
2. **The replayed notice did not say a changed submission was not applied
   (Sol, P2).** Wording fixed to "Work item already existed; the server
   returned the original row, so this submission was not applied." (unit and
   browser cases updated). The semantics stay as designed and documented above.
3. **The identity was scoped to one editor instance, so Cancel + reopen after a
   lost response could duplicate (DeepSeek, F3).** Fixed by the pending
   identity store (see the P1 entry); red/green unit evidence in
   `.tmp/0007c-evidence/red-reopened-draft.txt` (1 failed / 23 skipped, exit 1)
   vs `green-reopened-draft.txt` (1 passed, exit 0), and the full frontend suite
   is 144 tests after the case. The page-reload residual is listed under
   limitations.
4. **The receipt overstated its own provenance twice (both auditors).** (a) The
   red run predates the one disclosed locator adjustment inside the breakpoint
   case, so "the same frozen specification" is reworded to say so; the red
   discrimination is unchanged (all four failures land on their defect
   assertions). (b) "its log records `harness_exit=0`" was true of the session
   echo, not the artifact; every re-run gate below now appends its exit line to
   its artifact, and the receipt cites the artifact.
5. **A replay against a project archived since the first attempt refuses
   `state_conflict` (DeepSeek, F4, minor).** Disposition: intended and now
   documented in the P1 entry — the archived gate keeps precedence and no row is
   written or returned.

## Dual independent audit and dispositions

Two independent read-only audits ran on this revision in parallel: a Sol-high
adversarial review of the increment against the contract, and a DeepSeek
mechanism-level audit that re-read each mechanism's code and re-derived three
recorded numbers from their artifacts. Findings and their dispositions:

| Finding | Disposition |
| --- | --- |
| The recorded subject digests did not reproduce and the desktop run used a one-line-older spec (blocker, both auditors) | Accepted. Both widths were re-run on the frozen specification after the last source edit, the digests were recomputed on the closed tree with the host and PowerShell edition recorded, and the one-line difference (the width-aware fix in the mobile two-context case) is documented above. |
| A cleared optional field was silently ignored on update; provider/delivery gating was re-derived live mid-edit (major) | Fixed. `workItemUpdatePayload` now sends `null` for the fields Control can clear (description, branch, owner name, agent profile, workspace, blocker) so an emptied control really clears; the CI/review "Not recorded" choice is rendered only when the served value is already empty (Control's changeset refuses nil there); an emptied pull request URL is kept by the server and the success notice says so; and a field that becomes provider-owned mid-edit is named in the success notice instead of being dropped silently. The reviewer's alternative (freeze the gating at open time so the write is attempted and refused) was considered and not taken: live gating keeps the served statement authoritative for what is presented as writable, and the notice closes the silence. Evidence: red `2 failed / 17 passed` → green `19 passed` for the focused file, later `8 files / 103 passed`; the cleared-field and fault-classification cases are new. |
| Two classic assertions (owner reassignment, manual delivery value) were retired without a migrated replacement (major) | Fixed. The planning suite's work-item edit case now reassigns the owner and sets a manual pull request URL, asserting the stored `assignee`, `pull_request_url` and `delivery.pull_request.value`. |
| A bare HTTP 500 was classified as authoritative, closing the editor and losing the draft (minor) | Fixed. Every 5xx is now transient, so a command keeps its frozen intent with a visible retry; pinned by a new `api.test.ts` case. |
| Receipt cited `39989eaf` for the provider-owned symptom (minor) | Corrected above. |
| The "pre-existing acceptance" clause was documented but not green (minor) | Accepted and re-worded; the detach failure is reproduced on the base revision's own file, and the mechanism is in unchanged classic JS (`control/priv/portal_assets/portal.js:2408-2412` skips the toast when a newer workspace load supersedes it). |
| A cold narrow deep link offers "Edit work item" on an archived project's item until the click is refused (minor) | Accepted as a limitation; the write path is refused by the client and the server, and the refusal is visible. See limitations. |
| Board move with no rendered neighbouring column leaves the live region silent (note) | Accepted as a limitation: the plan records the no-request behaviour, the card keeps focus and the stored order is unchanged. |
| `hasServedRevision`'s no-write branch had no browser case; one unit pin for the ownership mapping (note) | Partially addressed: the new revision-loss case covers the editor side; the mapping pin stands. Recorded as low risk. |
| Fixture precision: the fixture's `acceptance_contract` shape is not the shape Goal admission itself would accept (note) | Accepted and documented in the fixture boundary section: every predicate this increment reads (the three-column all-or-none membership and `goal_managed?/1`) is identical, and the Goal plan/approve flow is out of scope. |

A later union review of the committed head (`af7f3bc`, PR #35, 2026-11-17)
raised the evidence-currency blocker and the four smaller findings disposed of
in "Union-review repairs (2026-11-17)" above; the table in this section is the
pre-union review history.

## Unwritable item kinds: the fixture boundary

Two item kinds cannot be produced through the portal API on this revision: a
Goal-managed row comes from Goal admission (`0007h`) and an
external-tracker-owned row from provider synchronization (`0007e`). The suite
therefore applies them with `scripts/ci/portal-planning-fixture.exs`, which uses
Control's own changesets against the same PostgreSQL database the running
control plane serves:

- `--goal-managed <work_item_id> <repository_resource_id>` writes the goal row,
  its first revision and the work item's all-or-nothing membership
  (`goal_id`, `admitted_revision`, `integration`, `acceptance_contract`) plus the
  repository baseline the first admission requires, in one transaction, because
  `goals.current_revision` carries a deferrable foreign key into
  `goal_revisions` and the database asserts an integrated item per admitted
  revision. The Goal plan/approve flow itself is not exercised.
- `--provider-owned <work_item_id> <resource_id> <provider> <external_id>`
  applies `WorkItem.provider_changeset/2`, the changeset the provider-sync path
  writes.

Independent API-level observation of the same boundary (performed on the
pre-union tree against a running control plane before the browser run; the
browser suite re-proves it through the UI):

- Goal-managed item: served `goal_managed: true`; `PATCH
  /portal/api/work-items/:id` → 403 `goal_authority_required`; `PATCH .../move`
  → 403 `goal_authority_required`.
- Provider-owned item: served
  `external.provider_owned_fields = ["title","description","priority","state","labels","assignee"]`;
  `PATCH` with a provider-owned field → 409 `provider_owned`; `PATCH` with a
  non-owned field (`branch`) → 200.

## Classic-check retirement

`browser/tests/portal.spec.mjs` keeps four cases: classic resource
attach/update/detach, the browser session boundary, the dialog/drawer keyboard
and overflow walk, and background refresh retaining drawer input, focus and
expanded raw history. Retired from it in this change (each retirement is
recorded at its former location in the file):

- the classic project and work-item planning dialogs: create, edit,
  archive/restore, board order/move, the two stale-update conflicts and the
  delayed-mutation race — replaced by `portal-work-planning.spec.mjs`;
- the classic detail-security injection probe — replaced by the same probe on
  `/portal/work/:workItemId` in `portal-work-views.spec.mjs`;
- the classic work-card → drawer keyboard walk — replaced by the list/board/
  detail keyboard walk in `portal-work-views.spec.mjs` (the dialog and control
  paths in the classic file stay);
- the owner-reassignment edit and its no-Retry / "Execution stopped"
  assertions — retired with the classic planning dialogs (planning suite and
  `0007f`);
- the classic "a delayed detail response cannot overwrite a newer drawer
  selection" race — replaced by the captured-response race in
  `portal-work-views.spec.mjs`;
- the classic "an older history failure cannot notify or mutate a newer drawer"
  race — replaced by the fault-injection guard and the real-cursor case in
  `portal-work-views.spec.mjs`;
- the classic "a delayed workspace response cannot replace the latest project"
  race — replaced by the captured workspace-snapshot race in
  `portal-work-views.spec.mjs`.

Nothing was deleted without a replacement on the newer route, and no assertion
was made conditional or skipped to make a gate pass.

## Recorded decisions, deviations and closure convention

The written plan is `docs/plans/2026-11-16-0007c-safe-work-planning-plan.md`.
Its section 8 records the reversible defaults taken (no new route or query
parameter; mutation receipts consumed as identity and disposition, never merged
into the read cache, so a partial write projection cannot empty a real work
item list; provider-owned edits follow the served `provider_owned_fields`, with
the server refusal as the enforcement point; status and order go through the
move endpoint; `goal_managed` is served, not inferred; preference scope stays
the deployment origin). Its section 9 records what execution added: the new
consumed read members stay **optional** because the fixture corpus deliberately
serves older partial projections, and a row without a served revision offers no
write path (`hasServedRevision`); an update omits provider-owned fields
entirely rather than echoing them; delivery values stay editable only while
their served source is `none` or `manual`; board movement targets rendered
columns (the PR-bot repair round makes every supported state render a column and
corrects the §9.4 note that had recorded the empty-state gap as a limitation);
project creation stays on the list; the editors hold a same-tick single-flight
ref in addition to the disabled control; the two unwritable item kinds are
fixture-applied because neither is reachable through the portal API; and the
work-item editor's draft owner lives above the responsive presentation branches
(§9.11). No plan decision was reversed during execution; the additions above are
the plan's own section 9 (plus §3.5 and §9.11 of the PR-bot repair round).

Closure convention: the goal file stays in `docs/goals/` (unarchived) with its
text unchanged, matching the merged `0007b` precedent and the contract's
"awaiting the owner's verdict" status; `docs/goals/README.md` already lists
0007c in its goal table, so no index edit is required, and archival remains the
owner's closure step.

## Measurement and evidence subject

The subject is the content identity of the tree this receipt describes, not a
commit sha (a rebase rewrites shas). Algorithm of record
`scripts/ci/0007c-subject-digest.ps1` (committed in this repair round; until
then it existed only as the untracked `.tmp/0007c-subject-digest.ps1`, the
0007b algorithm): enumerate the listed roots recursively plus the direct files
of `frontend/`, `browser/`, `contracts/` and `scripts/` plus the explicit
files; hash every file's bytes; then hash the UTF-8 manifest of
`"<sha256>  <repo-relative path>\n"` lines sorted by path. Scope `source`
excludes `docs/**` so that editing this receipt cannot change the value it
records; scope `full` adds the three explicit `docs/**` files.

Current values, recomputed on the working tree over `724b0c8` after the last
source edit of the PR-bot repair round (including the audit-driven pass — the
pending creation identity, the replayed-notice wording and their tests), twice
in a row with identical output under `pwsh 7.6.9`:

- source scope: `sha256:2bab2d22ee262202456f595a9ccaa6bf6e09000f15d315d8af2d9be7eb82ce44`
  (545 entries)
- full scope: `sha256:49f5d12d02ea68f6ba971f9a71044eb1bf62da6d3f705f8e9f4cccc102ae3b96`
  (548 entries)

The delta from the values the earlier pass of this round recorded
(`e5a33d0e…` / `9ce36bcc…`, now superseded) is exactly the audit-driven pass:
`frontend/src/features/planning/write-policy.ts` (new), `editors.tsx`,
`planning.test.tsx` and `browser/tests/portal-work-planning.spec.mjs` — the
throwaway classic-dialog probe lived and was deleted inside this pass, so no
digest run ever saw it and the entry counts stay 545/548. Relative to
the earlier rebinding round's repaired tree (`af7f3bc` + repairs) the round
adds `control/test/symmetry_control/migrations/add_work_item_creation_identity_test.exs`
plus the files listed above (the new `control/priv/repo/migrations/**` file is
outside the algorithm's listed roots, which cover `control/lib` and
`control/test`, not `control/priv`).

Historical values from the earlier rebinding round, kept as history only. They
were measured on the repaired tree over `af7f3bc` after that round's last source
edit, twice each with identical output under `pwsh 7.6.9`:

- source scope: `sha256:ed15849d1cc0645047823ff9abbcde56e78d73a5bb2b854b76f78f0f5727287e`
  (544 entries; superseded)
- full scope: `sha256:29148952b89a690835814b1406e01cd982f7dd3c477e4c7ae050aef36d8d17af`
  (547 entries; superseded)

Historical values from the pre-union tree, kept as history only. They were
recorded for revision `2525a7e6fee4d28818c4e6e2da7a33274341ebbe`, the union
review reproduced them exactly with the same script on a `git archive 2525a7e`
extraction, and entry-level attribution of the 530→543 delta (this head before
this round's script commit) showed every changed or added entry belongs to
main's `0007d` work (`d781ba7`, `e6e4b03`) with zero unexplained entries:

- source scope: `sha256:a0fde9904023360b711a251318a27e3ced6a321302a72c6c20946a894fd1b659`
  (530 entries; superseded)
- full scope: `sha256:79ffd1daacef47108d7ff7b350f77d30f5bf0e65cae82a5eb6ffd2b59d074784`
  (533 entries; superseded)

Two precision notes: the script
deduplicates full paths and sorts with `Sort-Object -Culture ""`, so a Windows
PowerShell 5.1 host (NLS rather than ICU collation) produces a different value
for the same tree — the recorded values are bound to the pwsh 7 host above — and
the digest does not cover `control/priv/static/portal` (a gitignored build
artifact the CI rebuilds) nor any `docs/**` file except the three named in the
full scope, so finishing this receipt cannot move the recorded value. Earlier
digest values in the review trail were superseded by each source edit and are
not evidence for this tree.

## Limitations and unresolved items

1. *(Removed in the PR-bot repair round: a resize that crosses the 1024 px
   drawer breakpoint no longer drops an open draft — the editor, its draft
   owner, the notices and the pending-write count now live above the
   presentation switch, and the real-API case "keeps an open editor draft
   across the responsive breakpoint" crosses 1280 → 900 → 1280 and saves the
   surviving draft at both widths.)* The list below keeps its original
   numbering for the entries that remain.
2. **The host killed the control plane mid-run** (process gone, empty stderr,
   no crash dump, no `erl_crash.dump` newer than the run) in several attempts;
   the harness restarts a dead control plane with a logged line, and every case
   still has to pass. Runs that finished report 0 restarts
   (`432991e2`, `0cc1b516`). One earlier run (`39989eaf`) recorded one restart.
   The 2026-11-17 re-runs recorded one control-plane start each (0 restarts):
   `symmetry-0007c-planning-8816f399`, `symmetry-0007c-planning-77c7d50a`,
   `symmetry-0007c-shell-cee825a9` (both servers),
   `symmetry-0007d-connections-b6806a0e`.
   The PR-bot repair round's runs recorded no restart line either — the final
   pair `symmetry-0007c-planning-ff2595d2`, `-1319913a`, the pre-change pair
   `-d7997be3`, `-8e0d0762`, the intermediate `-85ca970a`, the red baseline
   `-95171f7e`, and `symmetry-0007d-connections-aa6cd456` /
   `-6afa5d77` — and the two shell servers ran one instance each for the whole
   44-case run.
3. **Do not edit `control/lib` while an acceptance run is in flight.** The dev
   configuration runs the code reloader; a mid-edit compile error stopped the
   server in run `b6900180` and its cases then failed with
   `ERR_CONNECTION_REFUSED`. The pre-union acceptance runs were executed on a
   frozen tree; every later round kept the tree unchanged from its frontend
   build to its last case (no source edit in flight) — this repair round froze
   `control/` before the red run and rebuilt/staged the frontend exactly once,
   before the green runs.
4. **The planning suite runs with Playwright `trace: "off"`.** On this host the
   on-failure trace flush fails at context close
   (`ENOENT …/traces/*.trace`), which Playwright reports as the test's own
   error; run `048933e1` lost five passing cases that way. Screenshot, error
   context and video stay on, and the suite's own `capture` artifacts are the
   acceptance images.
5. **The pre-existing browser suites run here against the repository's dev
   configuration**, with staged assets, a dedicated database and two servers
   (one with a two-second session lifetime for the expiry case), not against the
   production release the Linux CI browser job builds; the release path itself
   was not exercised on this host (the CI browser job is its gate).
6. **Daemon coverage on this host**: `go vet ./...` exits 0; the whole-tree
   `go test ./...` hit the 10-minute default deadline under this host's load, so
   the app package was run explicitly
   (`go test ./internal/app -count=1 -timeout 20m`, exit 0, 172.2 s this round;
   compiled assets unchanged since). No other daemon package was re-run, and
   the whole-tree run still has not been executed. The 2026-11-17 rebinding
   added `go test ./internal/contracts ./internal/protocol -count=1` (the two
   packages the union changed) on top of the app-package run, and this round
   re-ran the same three gates on the byte-identical `daemon/`; see the gate
   table.
7. **Node toolchain**: the public pnpm registry is unreachable from this host,
   so every Node command runs `pnpm --offline` against the lockfile-matched
   `node_modules` installed for this revision; no dependency was added or
   upgraded, and no fresh install was verified.
8. **A bare `node --test` inside `contracts/` fails on a pre-existing file the
   repository's own gate does not select.** `contracts/ts/decoder.type-test.ts`
   raises `ReferenceError: usageForExactnessTest is not defined` (line 243)
   because that symbol is never declared; `node --test` collects it while
   `contracts:test` selects only `contracts/scripts/*.test.mjs`. The file is
   untouched by this increment and the documented gate
   (`pnpm contracts:check`, the CI command) is green. Recorded as a
   pre-existing dead-code/defect candidate for a maintenance task, not fixed
   here.
9. **A cold narrow deep link to a work item in an archived project offers
   "Edit work item" until the operator clicks it**, because nothing fetches the
   project snapshot on the narrow path (the desktop background is the only
   reader) and the archived gate then fires in `openEditor`. No write is
   reachable — the editor refuses with the archived notice and the server's
   `ensure_active_project` refuses too — but the presentation is later than the
   server's truth. The browser case walks in from the warm board, so the receipt
   claims only what that case asserts.
10. **Every supported state now renders a column, so the remaining no-destination
    case is the last rendered column**: a `Shift+←/→` move from `done` (or from
    a served unknown state, which sorts last) has no neighbour to the right, so
    no request is sent and the live region stays silent — the card keeps focus
    and the stored column and order are unchanged. This is the narrower residue
    of the former "a move into a state with no rendered column" finding, which
    the PR-bot repair round fixed for the reverse direction: `boardColumns`
    renders all five supported states plus any served state outside that set,
    empty ones included, and the two new real-API cases move into a
    previously-empty column from a backlog-only project and from a board
    filtered to `state=backlog`. The plan's §9.4 note is corrected accordingly;
    the create form's `Status` select remains the way to create directly into a
    state.
11. **Clearing a recorded pull request URL or a recorded CI/review status is not
    expressible through Control's update changeset** (its URL validator rejects
    nil and its status validators require an included value), so the form either
    does not offer the empty choice (CI/review, once a value is recorded) or
    reports that the server keeps the recorded value (pull request URL). The
    success notice carries that sentence; the field is never silently ignored.
12. **The green browser runs and the receipt's final numbers bind this round's
    last build** (`assets/index-BctcJ1zX.js`, staged as the round's final
    source edit, built from the working tree over
    `724b0c8`). Two earlier measurements of the same round ran the round's
    previous build (`assets/index-DD-MatYX.js` before the audit-driven frontend
    change): the full `mix test` suite and the first green browser pair
    (`-d7997be3`, `-8e0d0762`), both superseded above except where they are the
    only evidence for a path (the control suite's static-asset assertion, whose
    subject was re-run on the final tree). The classic-dialog probe involved
    only `control/priv/portal_assets/portal.js`, which no build step touches,
    so it binds the same classic source every other classic check serves. The
    four portal asset cases assert that the staged document names a build and
    that the build is served, not its content. The pre-union control suite had
    run one build older, and the earlier rebinding round used
    `assets/index-BNMC_fP3.js`; both remain history, not evidence for this
    tree.
13. **Two duplicate paths remain by scope, both visible rather than silent.**
    (a) A page reload loses the pending creation identity together with the
    draft, so an operator who reloads after a lost response, retypes the item
    and submits can still create a second row; the identity lives in memory only
    because this surface keeps no browser-local store (view preferences stay the
    only one, per the contract). (b) The retained classic dialog re-mints its
    identity when a create form is reopened, so its Cancel-and-reopen path keeps
    the pre-fix behaviour until `0007i` removes the classic surface; its
    same-form resubmission path is fixed and probe-verified. Neither path
    weakens the database index or the replay contract: the replay answer names
    the original row, and every other answer is the row the operator typed.
