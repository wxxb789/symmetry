# 0007c safe work planning — evidence receipt

Status: **produced; awaiting the owner's verdict**. The work is one uncommitted
working tree over base `7a92ca498f60309b97920b9df834f2637b91cfbd`; the goal file
stays in `docs/goals/` (not archived) and this receipt is the closing evidence.
No commit, push or history rewrite was performed by the implementing agent.
Every number below was produced on the revision named in "Measurement and
evidence subject"; older numbers are not evidence for newer code.

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
- `tests/portal.spec.mjs`, `README.md` — classic retirement and documentation.
- `frontend/src/services/api.test.ts`, `frontend/src/features/planning/planning.test.tsx`
  — the unit cases for the cleared-field, ownership-flip, revision-loss and
  server-fault behaviours.

## Done-when → evidence

| Contract clause | Evidence |
| --- | --- |
| Successful project and work-item mutations on real data | `portal-work-planning.spec.mjs`: "creates, edits, archives and restores a project on real data" and "creates a work item with the project's resources, edits it and moves status through the board" — real HTTP against the running control plane and PostgreSQL, both Playwright projects (desktop + Pixel 7). |
| Duplicate submissions avoided, explicit retry reuses the original intent | "submits a project and a work item once on a double submit" (two `requestSubmit()` calls in one task, exactly one request captured per action) and "replays the failed intent verbatim on an explicit retry" (aborted request, then the retry carries the identical body). |
| Optimistic-lock conflict surfaces visibly and re-syncs the newest server state; background refresh never replaces another selected resource's draft | "re-syncs to the newest project after a stale update in two contexts", "re-syncs to the newest work item after a stale update in two contexts" (two browser contexts on the same row; the stale writer sees `stale` + newest state, never a silent overwrite) and "keeps an open draft across a background refresh and applies it against the loaded revision". |
| Cross-project races, refresh while editing, validation errors and recovery leave no corrupted visible state | "applies a late board move only to its own project" (the late move lands only on its own project; the selected project, its notices and its stored rows stay intact), "refuses a duplicate project key with the server's field error and keeps the draft", "refuses a whitespace title with the server's field error and keeps the draft" (server field errors in the open editor, drafts kept). |
| Keyboard board movement with focus restoration, Goal-managed and provider-owned items offer no writable path | "moves and reorders the focused board card with the keyboard" (column move, up/down reorder, unmoved-neighbour no-op, focus stays on the card, reload confirms the stored order), "a Goal-managed item offers no writable path and the server refuses legacy writes" (403 `goal_authority_required` for both legacy paths), "an external-tracker-owned item keeps its provider-owned fields and column unwritable" (409 `provider_owned`, disabled editor controls, no move request). Both run at both widths. |
| Goal-managed run/retry/move routes to Goal admission, never the legacy paths | Server: the legacy update and move paths answer 403 `goal_authority_required` (API probe above; re-proved in the browser run by calling both endpoints from the page). Presentation: `features/work/detail.tsx` states the routing on the item and offers no writable control, `features/projects/work-surface.tsx` refuses the board move with the Goal-admission message and sends no request, and the board card carries the `Goal-managed` chip. Goal admission itself is `0007h` and is out of scope. |
| Archived project read-only except restore | "an archived project's board offers no create, edit or move path" asserts the archived notice, no create/edit controls, a visible restore control, no move request and an unchanged stored column. |
| View preferences stay view preferences: versioned browser-local storage under a deployment scope, no credentials, evidence, session handles or authoritative task data | `frontend/src/features/projects/saved-views.ts` (pre-existing, unchanged by this increment): `savedViewStorageKey = "symmetry.portal.saved-views"`, `savedViewVersion = 1` inside the stored document, scope = deployment origin, per-project entries, and the stored shape is only `{name, search}` literals decoded with a schema; the module documents and enforces that no authoritative or credential data is kept there. This increment adds no other browser-local store, and its receipt-consuming paths keep the session token out of storage. |
| Pre-existing browser acceptance continues on the new routes and classic checks are retired in the same change | Retirement list below. Re-run on this revision: 44 cases, 40 passed, 2 failed, 2 skipped — both read-surface suites fully green (35 cases, 0 failures), and the two failures are one classic case (`portal.spec.mjs` resource detach) failing identically on the base revision's own copy of the file. |

## Maintained measurements

| What | Value | Revision |
| --- | --- | --- |
| Contract gate `pnpm contracts:check` | exit 0: typecheck, 32 tests passed / 0 failed, 17 schemas, 219 fixtures | this tree |
| Frontend unit tests | 8 files, 104 tests passed, exit 0 | this tree |
| Frontend build + stage | exit 0 (`vite build` + `scripts/stage-assets.mjs`) | this tree |
| Control format/compile | exit 0 | this tree |
| Control suite (`mix test`) | 839/840 passed, 21 skipped, 1 failed (`Integrations.CommandTest` "provider token commands can keep stderr diagnostics out of stdout" — the pre-existing Windows pwsh path, unchanged by this increment), exit 2 (778 s) | this tree |
| Planning acceptance, desktop-chromium | 14 passed, 0 failed, 0 skipped, exit 0 (3.4 min) | frozen tree, run `symmetry-0007c-planning-dce892cf` (its log records `harness_exit=0`) |
| Planning acceptance, mobile-chromium (Pixel 7) | 14 passed, 0 failed, 0 skipped, exit 0 (2.2 min) | frozen tree, run `symmetry-0007c-planning-ecf17b02` (its log records `harness_exit=0`) |
| Pre-existing browser suites (classic + read surfaces) | 44 cases, 40 passed, 2 failed (same pre-existing detach case at both widths), 2 pre-existing mobile skips | this tree |
| Evidence subject (source scope) | recorded below | this tree |

## Executed non-browser gates

Commands run from the worktree root unless a working directory is named. Every
row is a command actually executed in this run; a gate that was not executed is
listed under limitations instead.

| Gate | Command | Result |
| --- | --- | --- |
| Contract gate (the CI command) | `pnpm --offline contracts:check` from the repository root | exit 0: contract typecheck, 32 tests passed / 0 failed, "Generated contract DTOs are up to date", "Validated 17 Draft 7 schemas and 219 fixtures" |
| Effect contract surface | `node contracts/scripts/check-effect.mjs` (also inside the gate above) | exit 0 |
| Contract unit tests | `node --test contracts/scripts/*.test.mjs` (what the gate runs) | 32 passed, 0 failed, exit 0 |
| Frontend types | `pnpm --offline --filter @symmetry/frontend typecheck` | exit 0 |
| Frontend lint | `pnpm --offline --filter @symmetry/frontend lint` | exit 0 (`--max-warnings 0`) |
| Frontend unit tests | `pnpm --offline --filter @symmetry/frontend test` | 8 files, 104 tests passed, exit 0 |
| Frontend build + stage | `pnpm --offline --filter @symmetry/frontend build` | exit 0; `control/priv/static/portal` restaged |
| Control formatting | `mix format --check-formatted` in `control/` | exit 0 (re-run on the final tree) |
| Control compile (warnings as errors) | `mix compile --warnings-as-errors` in `control/` | exit 0 (re-run on the final tree) |
| Control suite | `mix test` in `control/` (database `symmetry_0007c_mixtest`) | 839/840 passed, 21 skipped, 1 failed, exit 2 (778 s). The failure is `SymmetryControl.Integrations.CommandTest` "provider token commands can keep stderr diagnostics out of stdout": it exercises the provider *command* path through pwsh, while the increment's only Control change in that area replaces a private Goal-ownership predicate with the public `WorkItem.goal_managed?/1` used by the work-item lookup; the same test failed on this tree before the browser work. |
| Daemon vet | `go vet ./...` in `daemon/` | exit 0 |
| Daemon app tests | `go test ./internal/app -count=1 -timeout 20m` in `daemon/` | exit 0 (about 7 min; the whole-tree `go test ./...` hits the 10 min default deadline on this host under load, see limitations) |

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
| desktop-chromium | `pwsh -NoProfile -File browser/scripts/run-planning-tests.ps1 -SpecArgs "--project=desktop-chromium"` | run `symmetry-0007c-planning-dce892cf`: 14 passed, 0 failed, 0 skipped, `harness_exit=0` (3.4 min) |
| mobile-chromium (Pixel 7) | `pwsh -NoProfile -File browser/scripts/run-planning-tests.ps1 -SpecArgs "--project=mobile-chromium"` | run `symmetry-0007c-planning-ecf17b02`: 14 passed, 0 failed, 0 skipped, `harness_exit=0` (2.2 min) |

Both runs executed the same frozen specification and the same staged bundle
(`control/priv/static/portal/assets/index-Nsodv3ma.js`), neither recorded a
control-plane restart, and each run log carries the exit code. 28 cases total,
0 skipped, every one over real HTTP against the running control
plane and PostgreSQL. The 14 cases are: project create/edit/archive/restore;
duplicate-key refusal with the server's field error; single submission on a
double submit; two-context stale project update; work-item create/edit with
resources and a board status move; whitespace-title refusal; explicit retry
after a transport abort (both captured bodies identical); a draft kept across a
background refresh; two-context stale work-item update; a late board move
landing only on its own project; keyboard column move, up/down reorder and
focus retention; Goal-managed item with server refusals; provider-owned item
with locked fields and no move request; archived project read-only with
restore. Network interception appears only to hold or abort a captured request
so a real request is replayed or released.

### Pre-existing browser acceptance on this revision

The classic and read-surface suites were run against the same revision on two
loopback servers started from this worktree (dev configuration, staged assets,
database `symmetry_0007c_shell`, one server with
`SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2` for the expiry case):

```
pnpm --offline --filter symmetry-browser-acceptance test \
  tests/portal.spec.mjs tests/portal-work-views.spec.mjs tests/workspace-shell.spec.mjs
```

Result: **44 cases collected, 40 passed, 2 failed, 2 skipped** (5.6 min):
`portal-work-views.spec.mjs` 20 cases — 19 passed, 1 skipped (its own
desktop-only held-response race); `workspace-shell.spec.mjs` 16 cases — 16
passed; `portal.spec.mjs` 8 cases — 5 passed, 2 failed, 1 skipped (its own
desktop-only background-refresh drawer case). So both read-surface suites are
fully green on this revision: 35 cases, 0 failures. The
two skips are the files' own pre-existing desktop-only concurrency skips
(`portal-work-views.spec.mjs` held-response race and `portal.spec.mjs`
background-refresh drawer case, both skipped on mobile). Both failures are the
same classic case, `portal.spec.mjs` "classic resource attach, update and detach
with truthful health", on desktop and mobile: the resource dialog's detach
leaves the toast on "Resource updated" instead of "Resource detached".

That failure is **pre-existing, not caused by this increment**: the unmodified
base-revision copy of the same file (`git show
7a92ca498f60309b97920b9df834f2637b91cfbd:browser/tests/portal.spec.mjs`, run as a
temporary probe file against the same servers) fails the identical assertion at
the identical step (`Received: "Resource updated"`), 10 passed / 2 failed / 4
skipped, and the second base-file failure is one of the cases this increment
retires (its replacement is asserted green in the planning suite). No
`portal.spec.mjs` or `portal-work-views.spec.mjs` assertion was made
conditional, skipped or weakened to reach that conclusion, and the temporary
probe file was deleted.

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
2. **A breakpoint crossing still drops an open draft** (see limitations): the
   detail presentation swaps trees at 1024 px, so the body remounts. Measured
   directly: with the editor open, `page.setViewportSize({width: 900})` left the
   editor open but its fields empty, and the pre-fix tree removed it entirely.
   The provider-owned editor capture therefore frames the viewport instead of
   the full page, and the contract's own refresh scenario (the case that must
   keep a draft) is asserted unchanged.
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

Independent API-level observation of the same boundary (performed on this tree
against a running control plane before the browser run; the browser suite
re-proves it through the UI):

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
columns only; project creation stays on the list; the editors hold a same-tick
single-flight ref in addition to the disabled control; and the two unwritable
item kinds are fixture-applied because neither is reachable through the portal
API. No plan decision was reversed during execution; the additions above are
the plan's own section 9.

Closure convention: the goal file stays in `docs/goals/` (unarchived) with its
text unchanged, matching the merged `0007b` precedent and the contract's
"awaiting the owner's verdict" status; `docs/goals/README.md` already lists
0007c in its goal table, so no index edit is required, and archival remains the
owner's closure step.

## Measurement and evidence subject

The subject is the content identity of the tree this receipt describes, not a
commit sha (a rebase rewrites shas). Algorithm (`.tmp/0007c-subject-digest.ps1`,
the 0007b algorithm): enumerate the listed roots recursively plus the direct
files of `frontend/`, `browser/`, `contracts/` and `scripts/` plus the explicit
files; hash every file's bytes; then hash the UTF-8 manifest of
`"<sha256>  <repo-relative path>\n"` lines sorted by path. Scope `source`
excludes `docs/**` so that editing this receipt cannot change the value it
records; scope `full` adds the three explicit `docs/**` files.

- source scope: `sha256:a0fde9904023360b711a251318a27e3ced6a321302a72c6c20946a894fd1b659`
  (530 entries)
- full scope: `sha256:79ffd1daacef47108d7ff7b350f77d30f5bf0e65cae82a5eb6ffd2b59d074784`
  (533 entries)

Both were recomputed on the closed tree after the final source edit, twice in a
row, with identical output, under `pwsh 7.6.9`. Two precision notes: the script
deduplicates full paths and sorts with `Sort-Object -Culture ""`, so a Windows
PowerShell 5.1 host (NLS rather than ICU collation) produces a different value
for the same tree — the recorded values are bound to the pwsh 7 host above — and
the digest does not cover `control/priv/static/portal` (a gitignored build
artifact the CI rebuilds) nor any `docs/**` file except the three named in the
full scope, so finishing this receipt cannot move the recorded value. Earlier
digest values in the review trail were superseded by each source edit and are
not evidence for this tree.

## Limitations and unresolved items

1. **A resize that crosses the 1024 px drawer breakpoint still drops an open
   draft.** `WorkDetailPage` swaps the detail's presentation tree at that
   breakpoint (0007b's design: drawer above, page below), so the body remounts
   and the editor closes. Measured on this tree: with the work-item editor open,
   `page.setViewportSize({width: 900, height: 900})` closed it on the pre-fix
   tree and left it open with empty fields after the mid-verification fix, and
   widening back did not restore it. Fixing this means lifting the editor's open
   state and draft above the presentation switch, which is a change to 0007b's
   detail surface; it is recorded here instead of being smuggled into this
   increment. The contract's own scenarios (background refresh, two contexts,
   cross-project races) are unaffected and asserted green.
2. **The host killed the control plane mid-run** (process gone, empty stderr,
   no crash dump, no `erl_crash.dump` newer than the run) in several attempts;
   the harness restarts a dead control plane with a logged line, and every case
   still has to pass. Runs that finished report 0 restarts
   (`432991e2`, `0cc1b516`). One earlier run (`39989eaf`) recorded one restart.
3. **Do not edit `control/lib` while an acceptance run is in flight.** The dev
   configuration runs the code reloader; a mid-edit compile error stopped the
   server in run `b6900180` and its cases then failed with
   `ERR_CONNECTION_REFUSED`. The acceptance runs above were executed on a
   frozen tree.
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
   (`go test ./internal/app -count=1 -timeout 20m`, exit 0, about 7 minutes;
   compiled assets unchanged since). No other daemon package was re-run.
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
10. **A board move with no rendered neighbouring column sends no request and
    leaves the live region silent** (the card keeps focus, the stored column and
    order are unchanged, and the plan records this as the intended behaviour).
    Moving into a state with no rendered column is the create form's `Status`
    select.
11. **Clearing a recorded pull request URL or a recorded CI/review status is not
    expressible through Control's update changeset** (its URL validator rejects
    nil and its status validators require an included value), so the form either
    does not offer the empty choice (CI/review, once a value is recorded) or
    reports that the server keeps the recorded value (pull request URL). The
    success notice carries that sentence; the field is never silently ignored.
12. **The control suite ran with the staged frontend bundle one build older than
    the tree** (it completed before the last editor/notice edits were rebuilt).
    The four portal asset cases assert that the staged document names a build and
    that the build is served, not its content, and the bundle was restaged
    afterwards for the acceptance runs; the control suite was not re-run against
    the final bundle.
