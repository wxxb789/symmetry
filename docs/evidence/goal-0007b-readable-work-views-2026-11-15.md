# 0007b readable work views — repair receipt

Status: **authorized blockers fixed; remaining limits recorded**. Round 1
resolved the descriptive-contract conflict and reported the goal blocked on two
verified predicates; round 2 applied exactly those two authorized changes (an
additive authenticated HTML route for the work-detail deep link, and an anchored
extension of the login `return_to` allowlist). No protocol, wire, shape,
pagination, permission or session-lifetime semantics changed, and no rejection
outside the authorized work-search surface was relaxed: the `/portal/work/:id`
document route and the work surface's own search encoding are accepted by design
(round 2), and every other previously rejected path, parameter and value still
maps to `/portal`. Non-browser gates, the focused release-shell browser gate, the
broad browser suite and the fresh-session deep links were re-run on the rebuilt
release. Owner visual acceptance is **awaiting the owner's verdict**; the model
does not supply that verdict.

Round 3 (review repair pass, 2026-11-16) fixes the DeepSeek review findings on
this branch: the three Spec MUST_FIX items, the ten Standards MUST_FIX items and
the listed real defects. It changes no wire contract, no permission and no
pagination semantics; the only allowlist widening accepts the tab escape
(`%09`) the shell itself writes into filter URLs. The repair pass only edits the
working tree: no commit, push, or history rewrite was performed.

Round 4 (fix pass on the round-2 Sol review, 2026-11-16) applies the three
findings in `.tmp/0007b-findings-sol-round2.md`: unrecognised structured
reported output renders as its JSON instead of reading as an absence (with
red-before/green-after assertions), the revision binding below is rewritten in
durable content-digest form, and the trailing blank line in
`frontend/src/components/workspace.tsx` is removed. Like round 3, that pass
edited only the working tree: it performed no commit, push, or history rewrite;
the branch's content was committed later (`01eb134` carries the round-4 source
edits together with the receipt update).

Round 5 (fix pass on the round-3 Sol review, 2026-11-16) is
documentation-only: it rebinds the evidence subject as a durable source digest
that excludes `docs/**`, names the revision behind every recorded digest and
count, keeps the pre-round-5 numbers as historical evidence bound to their own
revisions, states the current contract inventory with its scope, re-syncs the
retained `.tmp/0007b-tooling` contract scope to this revision, and re-runs the
contract and frontend gates there (section "Round 5 verification"). No source
file, test or assertion changed; like the earlier passes it edits the working
tree only (no commit, push, or history rewrite).

Round 6 (fix pass on the PR-bot P2 finding, 2026-11-16) fixes the
focus defect the review comment reports on this increment's work surface: the
shell keyed its post-navigation heading focus on the full URL, so every
search-parameter update — the filter input's every keystroke, the state and
owner selects, the blocked checkbox, the saved-views select, the list/board
view links and the Attention project picker — moved focus to
`#content-heading`. `WorkspaceRoot` now keys that reset on the route pathname,
so search-only updates keep focus in the control while real route changes
still move focus to the heading. Two unit tests and a strengthened browser
keyboard case cover it, both red before the fix (focused Vitest `1 failed /
25 passed`, exit 1; the release keyboard case `2 failed` on both widths, exit
1) and green after (`7 files, 73 passed`; focused release-shell gate `35
passed / 1 skipped / 0 failed`, exit 0). That pass edited the working tree
only: no commit, push, or history rewrite.

Round 7 (fix pass on the round-5 Sol review, 2026-11-16, committed as `45c0bc3`) applies the
two P3 findings in `.tmp/0007b-findings-sol-round5.md`. P3-1: round 6's
pathname-keyed heading reset stops the shell from moving focus on a
same-pathname update, but two controls are unmounted by their own such update
and focus fell to `body` — `Clear filters`, whose empty state and button
disappear once results return, and `Delete saved view`, removed together with
the active saved view. Both now hand focus to a control that stays mounted
(the filter input `#work-filter-input` and the saved-views select
`#work-saved-view-select`) before the removal commits. Class audit: the only
conditional button in the increment's surfaces was `Delete saved view`; the
refetch-driven `Try again` and `Retry history` buttons are query-state updates,
not same-pathname navigations, and never depended on the shell reset. Both
fixed controls carry assertions that fail on the pre-round-7 tree — focused
Vitest `2 failed / 26 passed`, exit 1; the release list case `2 failed` on both
widths at the Clear-filters assertion and, with that assertion probed out of a
scratch copy, `2 failed` at the delete assertion, exit 1 — and pass after the
fix (focused unit file `28 passed`; full frontend `7 files, 75 passed`;
focused release-shell gate `36 cases: 35 passed, 1 skipped, 0 failed`, exit 0).
P3-2: the binding paragraph below now names every changed source file (round 6
changed three, not two) and rests on the recomputed digests. Like the earlier
passes this one edits the working tree only: no commit, push, or history
rewrite.

Round 8 (fix pass on the PR-bot P1, 2026-11-16, committed as `747b19b`) keeps
keyboard focus on a board card when a workspace refresh re-parents it into
another column: the work item id, not the node, is the identity that survives
the regroup, so a `MutationObserver` restores focus unless another control took
it in the same commit (`useWorkFocusRestore` in
`frontend/src/features/projects/work-surface.tsx`). Its own numbers are in
section "Round 8 verification".

Round 9 (fix pass on the round-9 DeepSeek review, 2026-11-16, this pass) fixes
`.tmp/0007b-findings-deepseek.md`: STD-A20/SPEC-A08 (the consumed
`WorkspaceProject.resources[]` member was hand-written outside the generated
contract and the receipt claimed no such copy remained), STD-A22 (a detail
refresh silently discarded the expanded older history), SPEC-A06 (the
populated-history clause had no evidence on the reviewed revision, and the
"none of their surfaces changed" justification for not re-running the live
suite was false), and the five P3s (STD-A23 reworded in round 6's class audit,
STD-A24 JSON-shaped `q`, STD-A25 `Clear filters` dropping the view, STD-A26 the
missing CI-tone-to-class assertion, STD-A27 the detail CI chip's unstated
binding limitation). The contract change is additive documentation of a member
the server already serves inside the existing read schema; no server, gate,
permission, pagination or authority rule changed. This pass re-executed the
frontend unit/type/lint gates, the contract/Go/Elixir gates, the focused
release-shell browser subset and the live-daemon suite on this revision
(numbers in section "Round 9 verification") and edits the working tree only:
no commit, push, or history rewrite.

Explicit revision binding (durable, source-subject-addressed, not
commit-addressed): revisions here are identified by a content digest, because
this branch may be rebased before merge and a rebase rewrites SHAs. This
receipt's binding is the **source subject** of the revision it describes: the
digest of the source-file manifest over the digest scope below with every path
under `docs/**` excluded, so editing this receipt or any file under `docs/**`
cannot change the value it records. This guarantee does NOT cover documentation
outside `docs/`: `browser/README.md`, `contracts/README.md` and
`frontend/AGENTS.md` are in the digest scope, so editing those does change it.
For the current revision — branch HEAD
`747b19b4c6f118b02a7ee1b2bb76446efbcf1039` plus round 9's changed source files
(`contracts/v1/workspace-read.schema.json`,
`contracts/generated/ts/workspace-read.ts`,
`contracts/generated/effect/index.ts`,
`daemon/internal/contracts/generated.go`,
`daemon/internal/contracts/schema_bundle.json`,
`frontend/src/features/work/presentation.ts`,
`frontend/src/features/work/history.tsx`,
`frontend/src/features/work/detail.tsx`,
`frontend/src/features/projects/work-surface.tsx`,
`frontend/src/features/projects/filters.ts`,
`frontend/src/app/router.test.tsx`,
`frontend/src/features/projects/filters.test.ts` and
`browser/tests/portal-work-views.spec.mjs`; every other source byte is branch
HEAD's) — the source subject is

`sha256:c4002c27273e3f43d6de1fa7a6444383b9e1f069ad97836252621f2069510bd5`
(393 entries; recomputed on this host in the working tree with
`pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1`).

The fixed tree's full-tree digest — the source subject plus the three explicit
`docs/**` files the older rounds included — is
`sha256:68692b2cbb4bdad240dde6978ba6a9c37af844c76593f55ff894a4532a63a1b0`
(396 entries; `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1 -Scope
full`). Round 8 served its own binding inside section "Round 8 verification"
(source `sha256:d0ac0a48…`, full-tree `sha256:989330ee…`, 393/396 entries), and
round 7's tree — branch HEAD `49ca695` plus the four changed source files of
rounds 6–7 — keeps its historical binding (source
`sha256:851b154263961dbf3eeceae2b680ebf42e25a6f4fdee1c434a31b83eedacb259`,
393 entries; full-tree
`sha256:896ded3be3c28fc657af6e76ad02f93d35dedac1d8a5283ced2ae9b28e1769a3`,
396 entries). The pre-fix source content — branch HEAD `49ca695`, whose source
subject is round 5's `sha256:4019477f…` (393 entries) — and its full-tree
digest from `01eb134` (commit
`01eb1344e4415ed2ce978e71030739cbfed4a09e` →
`sha256:1e345643837bcaaf5f26e5218df52156965f564618257239100dceccbb02aca5`,
396 entries) are retained as historical evidence bound to their own revision,
as is round 6's fix tree (`sha256:5d86199e…`, 393 entries; full-tree
`sha256:a8c7fcf3…`, 396 entries). Round 6's description of that tree named
only two of its three changed files; the digest itself already covered
`browser/tests/portal-work-views.spec.mjs`, and recomputing it in this working
tree before round 7's edits reproduced it exactly (`pwsh -NoProfile -File
.tmp/0007b-subject-digest.ps1`, exit 0, 393 entries).
Commits that only touch `docs/**` after `01eb134` do not change the source
subject (documentation outside `docs/` does). The exact final source-subject
digest of the merge revision will be recorded in the PR description, not inside
this receipt.

Round 4's earlier binding `sha256:353b10fc…` (390 entries) was computed over
the round-4 uncommitted tree on the pre-rebase line (`6398caa` plus the five
round-4 source files) and is retained below only as historical evidence, not
as the current tree: it is not the digest of the revision this receipt
describes. The rebase onto the current base (`259ea7d`, the 0009 contract
amendments) brought in six additional `goal-command` fixtures, a `goal-command`
schema amendment and its regenerated artifacts, which is why the current
revision has 396 full-tree entries and 219 manifest fixtures. Round 1/2
evidence was produced on the pre-rebase working tree committed as `dae2388`
(base `343dea6a62484d73e7b577a0be2683ca233510f6`); its subject digest
reproduces exactly on that revision and not on the later trees, because the
rebase replaced `.github/workflows/ci.yml`, the only digest-scope path that
changed (`git diff --name-only dae2388..03646d1` lists four files; three are
outside the digest set).

## Findings and authorized boundary

| ID | File / surface | Change or disposition | Evidence |
| --- | --- | --- | --- |
| F1 | `.tmp/0007b-findings.md`; `contracts/v1/`, `contracts/ts/`, `frontend/src/` | Valid. Added descriptive work-detail and timeline schemas, generated DTOs and Effect boundaries, and consumed those boundaries in list/board/detail/history. The presentation readers derive their input shapes from the generated DTOs (`Loosen<>` views over the served projections; open-JSON members stay `unknown`). Correction (round 9): one consumed wire member, the project's bound `resources[]` read by the CI chip, was hand-written outside the generated contract when this row was written; round 9 moved it into the descriptive workspace-read schema, regenerated the DTOs and Effect graphs, and derives `CiResourceLike` from the generated `WorkspaceProject.resources`, so no consumed wire member is hand-written now. Implemented project filters, saved views, truthful cost/CI/acceptance labels, desktop drawer/narrow page, and cursor-on-demand history. This was the round-1 blocker; round 2 re-runs its surfaces on the round-2 revision, and round 3's review repairs are listed below. | Contract, frontend, Go, and Elixir results in this receipt; browser evidence is separate from schema proof. |
| F2 | Authorized resolution in `.tmp/0007b-findings.md` | Applied as a scope rule, not dismissed as an invalid finding. Regenerated `contracts/generated/**` and `daemon/internal/contracts/{generated.go,schema_bundle.json}`; added matching fixtures and passive read-schema dispatch. `control/lib/**` stayed untouched by round 1; round 2 then made only the two changes authorized below. | Generation/drift gate and cross-language fixture decoders; all production read responses keep their original wire contract. |

### Round 2 authorized blockers, fixed

| ID | Surface | Change | Evidence |
| --- | --- | --- | --- |
| F1 | `control/lib/symmetry_control_web/router.ex` | Added `get "/work/:work_item_id", PortalController, :index` to the existing authenticated `/portal` scope, next to the project routes. Additive SPA-shell document route only; no JSON/protocol/route-shape change. | `portal_controller_test.exs` serves the built entry with request CSRF at `/portal/work/work-123`, and still 404s `/portal/work` and `/portal/work/work-123/not-a-route`; live release answers `302` → `return_to=%2Fportal%2Fwork%2F<id>%3Fview%3Dboard`. |
| F2 | `control/lib/symmetry_control_web/portal_session.ex` | Extended the strict `return_to` allowlist in its anchored style: `@shell_path` gained `work/<id>`, and `shell_query?/1` accepts exactly the work surface's own search encoding (`view=list\|board`, `q`/`state`/`owner` text values in URLSearchParams form, `blocked=1\|%221%22`, `projectId=<id>` on the work route only). Any other path, parameter or value still maps to `/portal`. | `portal_session_test.exs` round-trips the work, filtered-project and blocked-flag URLs through login and invalid-credential retry (five new paths), and rejects 13 new work/project query cases while keeping every earlier rejection; live release `GET /portal/projects/<id>?view=board&q=keeper` answers `302` with the full path, `?view=evil` and `?q=//example.com` answer `return_to=%2Fportal`. |

Regression proof for both: with the unmodified `HEAD` copies of the two files
restored in the test image, the two focused suites report `18/20 passed` /
`Failed: 2 tests`; with the fixed files they report `20 passed`. The four
fresh-session browser cases that failed both widths in round 1 now pass (below).

A direct probe of `PortalSession.return_to/1` with the exact encodings the shell
writes returned each filtered URL unchanged
(`/portal/projects/p-1?view=board&q=Human+decision&state=in_progress&owner=agent%3Anix&blocked=%221%22`,
`/portal/work/w-1?view=list&q=%22x%22&projectId=p-1`, `/portal/work/w-1?blocked=1`)
and still rejected `blocked=%220%22`, an unknown parameter and `q=%0d%0aX`.

### Round 3 review findings (DeepSeek review of `4e60b9d..03646d1`)

Every finding is either fixed with a reproducing check or explicitly recorded
below; nothing was silently dropped.

| ID | Surface | Change or disposition | Evidence |
| --- | --- | --- | --- |
| SPEC-A04 / STD-A02 | `frontend/src/features/work/presentation.ts`, `detail.tsx`, `work-surface.tsx` | Open-JSON reported output is no longer judged as absent: `reportedText/1` renders non-empty structured `outcome.summary` / `result.summary` / evidence values as JSON text, `evidenceRows` accepts a single served value, and both list rows and the detail render them. | Unit + router tests red before (characterised summary rendered as null / `No reported result`) and green after; exit codes in the round-3 gate table. |
| SPEC-A04 / STD-A03 | `presentation.ts` (`ciLabel`), `work-surface.tsx` (`ciResourcesOf` threading) | `CI passed` green is now granted only to a provider source, a strict absolute `recorded_at` inside the 24 h window, and a bound CI resource the same snapshot reports as synced and not failed/offline/degraded; agent/manual sources render `CI passed · unverified source`, loose timestamps `freshness unknown`, stale/failed bound resources `CI passed · bound resource <signal>` with the stale tone, and missing bindings `binding not confirmed`. | Presentation unit tests red before (agent source and stale bound resource both rendered green) and green after; the browser list case's real stale-CI fixture row fails on both widths against the pre-fix release (in a `3 failed / 32 passed / 1 skipped` run) and passes after the rebuild. |
| SPEC-A04 / STD-A09 | `browser/tests/portal-work-views.spec.mjs`, `frontend/src/app/router.test.tsx` | Row-level truth labels are now asserted: real rows must carry `Acceptance not recorded` and `No reported result`, and the unit row/detail test asserts `Reported: {"message":"12 passed"}` for a structured result summary and the detail's JSON `outcome.summary`, with no `No reported execution output` fallback. | Unit test red before (row showed `No reported result`), green after; browser assertions bound to the served rows. |
| SPEC-A05 / STD-A06 | this receipt | The Compose/restart CI-coverage claim is rewritten truthfully (see the limits bullet below). | `ci.yml`: the `compose` job runs Compose smoke checks only and never installs Playwright or sets `SYMMETRY_COMPOSE*`; the only CI Playwright execution runs `workspace-shell` + `portal-work-views`. |
| SPEC-A05 / STD-A07 | `browser/tests/portal-compose-restart.spec.mjs` | The post-restart rendered-read block now targets the retried workflow, whose generation-2 `summary` event is already proved in the same spec, instead of the waiting workflow whose fake agent serves no summary. | Code chain: fake-agent `runWaitForInput` emits no `summary`; the retried timeline assertion already requires the gen-2 `summary` kind. The Compose suite itself remains unexecuted (limits below). |
| SPEC-A05 / STD-A10 | `browser/tests/portal-work-views.spec.mjs` keyboard case | The classic drawer Tab/Shift+Tab focus constraint is migrated to the new Radix drawer (desktop only): opening moves focus inside the dialog, Shift+Tab reaches the Close control, a second Shift+Tab stays inside, and Tab wraps back to Close. | Measured against the release with a throwaway probe (open focus H1 in dialog → Shift+Tab Close → Shift+Tab Refresh → Tab Close); the case passes before and after the frontend fix. |
| SPEC-A07 / STD-A08 | this receipt | Every digest is rebound and each binding names its revision (see the revision paragraph and the evidence-subject section). | `git archive` copies digested on this host: `03646d1` → `391930d0…` (390 entries); `dae2388` → `aa344b7d…` (390 entries, the round-2 number). |
| STD-A01 | `frontend/src/app/application.ts`, `router.test.tsx` | The served `before` boundary is part of the timeline query key, so a refreshed detail re-keys the disabled query instead of reusing pages from the old boundary. | Router test red before the key change: after a loaded older page (`cursor-1` → `cursor-1b`) and a detail refresh serving `cursor-2`, the old key paginated `cursor-1b` (`expected ['cursor-1','cursor-1b'] to deeply equal ['cursor-1','cursor-2']`); green after. |
| STD-A05 | `contracts/fixtures/invalid/{work-timeline-read.unknown-source,work-detail-read.unknown-timeline-source}.json` | Both fixtures now carry a complete valid event-branch `data`, so the only failing constraint is the unknown `source` discriminator. | Ajv probe: the corrected fixtures fail only at the source const; setting `source: "event"` or widening the source const makes them valid. Node/Go/Elixir fixture gates green. |
| STD-A11 | `control/lib/symmetry_control_web/portal_session.ex` | The `q`/`state`/`owner` text-param pattern accepts exactly `%09` (the one C0 escape the shell's URLSearchParams writes for filter text) and keeps rejecting every other `%0x`/`%1x` escape and injection case. | `portal_session_test.exs`: `q=a%09b` accepted on project and work routes; focus suite red 7/8 before (field degraded to `/portal`), green `20 passed` after; new `%0a`/`%1f`/`%00` rejections stay rejected. |
| STD-A12 | `control/test/symmetry_control_web/portal_session_test.exs` | Accepted coverage now exercises the shell's real encodings (`owner=agent%3Acodex`, `q=%22123%22`, tab and combined work-route filter), so the escape branches cannot be deleted silently. | Focused suites `20 passed`; the added cases fail against the pre-fix regex and pass after. |
| STD-A13 | `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs` | Recorded as a limitation, not silently accepted: the read-schema fixtures are validated through the generated bundle with a test-local ECMA-262→PCRE2 `\u0000` adaptation, mirroring `ContractValidation`'s private production copy. | Comparison shows zero semantic difference today; the private copies can drift if a daemon-only rule changes. |
| STD-A14 | `contracts/v1/work-timeline-read.schema.json`, `work-detail-read.schema.json` | Recorded as an explicit false-acceptance limitation: descriptive read schemas accept a half-open command identity pair and null `recorded_at`/`occurred_at` values the server does not serve; the database check constraint and `Protocol` remain the enforcement point. | Valid fixture `work-detail-read.active.json` serves `occurred_at: null`, so tightening the schema would reject a served shape; no schema change was made. |
| STD-A15 | `contracts/README.md` | The quicktype Go flattening of the timeline `oneOf` is now a documented known limitation. | One-sentence note in the Go Boundary API section; verified against `generated.go`. |
| STD-A16 | `frontend/src/features/work/history.tsx`, `router.test.tsx` | A 400 (aged-out cursor) on the older-history page now recovers: Retry resets the timeline query and re-reads the detail so the disabled query restarts from the newly served boundary; other errors keep the in-place refetch the live suite asserts. | Router test: Retry triggers a new detail read and the next older-page request uses `cursor-2` (red before: `cursor-1` loop). |
| STD-A17 | `frontend/src/features/work/presentation.ts` | The hand-written parallel interfaces are replaced by derived `Loosen<>` views over the generated DTOs (open-JSON members stay `unknown`); the receipt claim is now true as written. | `tsc --noEmit` green; renamed/removed generated members would fail the derivation at typecheck. |
| STD-A18 | `frontend/src/services/api.fixtures.ts`, `router.test.tsx` | Unit fixtures now accept served `execution`/`delivery`/`outcomeSummary` overrides, making structured reported output, the detail summary and delivery projection reachable in unit tests. | New router tests use them; the remaining waiting/narrow-unit gaps are recorded as limits (browser suites cover them). |
| STD-A19 | `browser/tests/portal-work-views.spec.mjs` | Board capture fields are assertion-backed (exact column list, card count equal to the served work-item count), the ready-column horizontal walk is unconditional, and the owner filter is driven end to end (URL, filtered rows, reload). | Assertions use the same served values the capture records; the previously conditional arrow branch is now always executed. |

## Maintained measurements

This receipt is the single source for the run's measured counts: the Round 9
column carries the current revision's re-executed evidence, and every earlier
column records its own revision. Older `test-ablation-*` receipts describe
their own historical revisions and are not live consumers of this run.

| Measurement | Baseline | Round 1 (`3ddb5e44…`) | Round 2 (`dae2388`) | Round 3 (`6398caa`) | Round 4 (`353b10fc…`, historical) | Round 5 (`01eb134`) | Round 6 (`49ca695`+fix6, historical) | Round 7 (`49ca695`+fix7, historical) | Round 8 (`45c0bc3`+fix8, historical) | Round 9 (`747b19b4`+fix9, current) | Measurement method |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| Frontend unit tests | 33 passed | 62 passed | 62 passed | 70 passed | 71 passed | 71 passed | 73 passed (re-run) | 75 passed (re-run) | 77 passed (re-run) | 81 passed (re-run) | Baseline archive Vitest run; final `pnpm --filter @symmetry/frontend test` (rounds 3–9 ran the package's pinned `vitest run` directly in the verified workspace: 7 files). |
| Contract runtime tests | 31 passed | 32 passed | 32 passed | 32 passed (unchanged) | not re-run in round 4 | 32 passed (re-run) | not re-run in round 6 | not re-run in round 7 | not re-run in round 8 | 32 passed (re-run) | Baseline archive and final `node --test contracts/scripts/*.test.mjs`. |
| Draft 7 root schemas | 15 | 17 | 17 | 17 (unchanged) | not re-run in round 4 | 17 counted, of 18 files (re-run) | not re-run in round 6 | not re-run in round 7 | not re-run in round 8 | 17 counted, of 18 files (re-run) | Contract generator/checker; `common.schema.json` is the excluded shared-definitions file (`contracts/scripts/check.mjs:114`), not a root. |
| Manifest fixtures | 190 | 213 | 213 | 213 (unchanged; two invalid fixtures corrected) | not re-run in round 4 (fixtures untouched) | 219 = 82 valid + 137 invalid (re-run) | not re-run in round 6 (contracts untouched) | not re-run in round 7 (contracts untouched) | not re-run in round 8 (contracts untouched) | 219 = 82 valid + 137 invalid (re-run; the schema gained a consumed member, no fixture added or removed) | Final schema/Effect gates exercise every manifest entry. |
| Browser declarations / spec files | 41 / 8 | 47 / 9 | 47 / 9 | 47 / 9 (unchanged) | not re-run in round 4 | 47 / 9 (re-listed) | 47 / 9 (re-listed) | 47 / 9 (re-listed) | 47 / 9 (re-listed) | 47 / 9 (re-listed) | Baseline and final Playwright discovery; declarations counted as `^\s*test\(` in `browser/tests/*.spec.mjs`. |
| Expanded desktop + mobile browser cases | 82 | 94 | 94 | 94 (unchanged) | 36 executed in round 4 (35 passed, 1 skipped); broad suite not re-run | 94 listed, not executed in round 5 | 36 executed in round 6 (35 passed, 1 skipped); broad suite not re-run | 36 executed in round 7 (35 passed, 1 skipped); broad suite not re-run | 36 executed in round 8 (35 passed, 1 skipped); broad suite not re-run | 36 executed in round 9 (35 passed, 1 skipped) + the live-daemon suite (3 passed, 1 skipped); broad suite not re-run | Executed `playwright test --list` in round 1; the round-3 broad run executed all 94 (79 passed + 15 skipped); the live-daemon suite is a separate 4-case spec run by its own runner. |
| Largest project snapshot in the completed measurement run | Not measured | 56,632 bytes / 15 work items | 74,388 bytes / 15 work items | 112,562 bytes / 15 work items | 144,421 bytes / 15 work items (focused run) | not re-measured in round 5 | 154,931 bytes / 15 work items (focused run) | 166,005 bytes / 15 work items (desktop 160,748; mobile 166,005; focused run) | 186,476 bytes / 15 work items (desktop 181,221; mobile 186,476; focused run) | 196,424 bytes / 15 work items (desktop 191,169; mobile 196,424; focused run) | `snapshotMetrics` in the final list/board cases; exact response bytes, not a string-length estimate. The value moves with the shared development database as fixtures accumulate, not with this change. |

Column revisions: Baseline is the archived pre-0007b workspace; Round 1 the
round-1 pre-rebase tree (`sha256:3ddb5e44…`); Round 2 the pre-rebase revision
`dae2388` (`sha256:aa344b7d…`); Round 3 the pre-rebase review-repair revision
`6398caa` (`sha256:193ddbb2…`); Round 4 the round-4 uncommitted tree
`sha256:353b10fc…` over `6398caa` (historical evidence, not the current tree);
Round 5 the revision `01eb134` (source subject `sha256:4019477f…`,
`docs/**` excluded); Round 6 the round-6 fix tree over branch HEAD `49ca695`
(source subject `sha256:5d86199e…`, `docs/**` excluded; historical, superseded
by round 7); Round 7 the round-7 fix tree over branch HEAD `49ca695` (source
subject `sha256:851b1542…`, `docs/**` excluded; historical, superseded by round
8); Round 8 the round-8 fix tree over branch HEAD `45c0bc3` (source subject
`sha256:d0ac0a48…`, `docs/**` excluded; historical, superseded by round 9);
Round 9 the current tree over branch HEAD `747b19b4` (source subject
`sha256:c4002c27…`, `docs/**` excluded). Full digests are listed under
"Measurement and evidence subject".

The Round 9 column records what round 9 re-executed on this tree — frontend
unit tests (77 → 81 with the JSON-shaped-query, Clear-filters-view,
history-retention and CI-chip-class tests), the typecheck and lint gates, the
contract generator/drift and Effect gates, the contract runtime tests, the Go
contract packages, the Elixir manifest fixture gate, browser discovery, the
focused release-shell subset (36 cases) and the live-daemon suite (4 cases,
3 passed with the intentional mobile lifecycle skip, both widths executing the
real-cursor history case: 100 → 121 entries). "Not re-run in round 8" keeps the
last executed value in its own revision's column; the Round 8 column keeps what
round 8 re-executed on its own, now historical, fix tree (75 → 77 unit tests,
the re-parent regression test and the focus-theft guard), the Round 7
column keeps what round 7 re-executed on its own, now historical, fix tree
(73 → 75 unit tests, the two focus-survivor regression tests), the Round 6
column what round 6 re-executed on its own historical tree (71 → 73 unit tests,
the strengthened keyboard case), and the round-5 column likewise holds only what
round 5 re-executed. Round 1–7 numbers are retained history, never restated as
new passes.

Current contract inventory with scope: `contracts/v1` holds 18 schema files, of
which the checker counts 17 Draft 7 root schemas — the excluded file is
`common.schema.json`, shared definitions rather than a root
(`contracts/scripts/check.mjs:114`) — and `contracts/fixtures/manifest.json`
lists 219 fixtures = 82 valid + 137 invalid. The corpus grew from 213 to 219
with the rebase onto `259ea7d` (0009 amendments: six `goal-command` achieve
fixtures plus regenerated `goal-command` artifacts). This round (Round 5) added
or removed no fixtures; the 0007b feature commit itself added 23 (manifest 196
at `259ea7d`, 219 at `714e33c`). The 17-root count is unchanged since round 1 added the
two read-schema roots (15 → 17). Round 9 added the consumed
`WorkspaceProject.resources` member to `workspace-read.schema.json` — a member
`/portal/api/workspace` already serves — regenerated the TypeScript, Effect and
Go artifacts and the schema bundle, and added or removed no fixture (the
existing `workspace-read.fractional-metadata` fixture already carried a
resource entry with additive metadata). Contract evidence on the round-5 revision: the Node
contract gates, the Go contract packages and the Elixir manifest gate were
re-run in round 5 against the re-synced tooling copy and container (exact
commands and exit codes under "Round 5 verification"); the focused/broad/live
browser suites, the Compose/restart specs, the Elixir full suite, the Go full
tree, and the frozen dependency installation were not re-run in round 5 and
keep the revision bindings recorded here. Round 6 re-ran the focused
release-shell subset (36 cases: 35 passed, 1 skipped) and left the broad,
live, Compose/restart, Elixir, Go and contract suites bound to their earlier
revisions. Round 7 re-ran the frontend unit, typecheck and lint gates and the
focused release-shell subset on its tree (36 cases: 35 passed, 1 skipped) and
likewise left the broad, live, Compose/restart, Elixir, Go and contract suites
bound to their earlier revisions. Round 8 re-ran the frontend unit, typecheck
and lint gates and the focused release-shell subset (36 cases: 35 passed, 1
skipped) on its tree, again leaving the broad, live, Compose/restart, Elixir,
Go and contract suites bound to earlier revisions. Round 9 re-ran the frontend
unit, typecheck and lint gates, the Node contract generator/drift and Effect
gates, the contract runtime tests, the Go contract packages, the Elixir
manifest fixture gate, the focused release-shell subset (36 cases: 35 passed, 1
skipped) and the live-daemon suite (4 cases: 3 passed, 1 intentionally
skipped) on this revision, so both browser suites that carry this increment's
acceptance now execute against it; the broad browser suite, the Compose/restart
specs and the frozen dependency installation stay bound to their earlier
revisions.

The baseline browser suite was **listed, not executed**. No before-run pass
count is claimed for Go or Elixir.

## Executed non-browser gates (round-1 tree, subject `sha256:3ddb5e44…`)

Node commands below ran in the disposable `symmetry-0007b-node` container at
`/isolated`, using pnpm 12.17.0, Node 24.22.3, the repository's package versions,
and the verified isolated dependency graph described below. They are not a pass
of the failed original frozen-install gate.

Every result in this section is bound to the round-1 pre-rebase tree
(`sha256:3ddb5e44551f2d498628e8456d87a40684d6a90f0b85b215a22148b5cdb008a6`);
its 17-schema / 213-fixture rows describe that tree, not the current revision.

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `docker exec -w /isolated symmetry-0007b-node pnpm contracts:generate` | 0 | Generated DTOs; artifacts copied back to the repository. |
| `docker exec -w /isolated symmetry-0007b-node pnpm contracts:check` | 0 | Typecheck, 32 runtime tests, 17 Draft 7 schemas, 213 fixtures, generated-file drift check. Effect checked all 213 raw fixtures; 209 parsed documents reached schema validation. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend typecheck` | 0 | Final decoder-sensitive and project-scope integration typecheck. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend lint` | 0 | Zero lint errors/warnings. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend test` | 0 | 7 files, 62 tests passed. jsdom reports its unimplemented `Window.scrollTo`; that warning is not browser verification. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend build` | 0 | Production assets staged; `index-D8XHWnI_.js` 656.77 kB / gzip 199.06 kB; CSS 20.92 kB / gzip 5.24 kB. Vite's greater-than-500-kB chunk warning remains. |
| `go vet ./...` in `daemon/` | 0 | Windows host vet passed. |
| `go test -json ./...` in `daemon/`, saved with `tee ../.tmp/0007b-go-all.jsonl` under `set -o pipefail` | 0 | 1,730 top-level passes, 55 top-level skips, 2,347 subtest passes; 21 packages passed, one had no tests. This run preceded the final fixture-only/nullability regeneration; the affected packages were repeated below. |
| `go test -json ./internal/contracts ./internal/protocol` in `daemon/`, saved with `tee ../.tmp/0007b-go-contracts-final.jsonl` under `set -o pipefail` | 0 | Final generated artifacts and complete fixture corpus: 73 top-level passes, 445 subtest passes, two packages passed, no failures/skips. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'mix format --check-formatted'` | 0 | Canonical formatter gate passed. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'mix compile --warnings-as-errors'` | 0 | Compile gate passed. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'mix test > /tmp/0007b-mix-full.log 2>&1; result=$?; cat /tmp/0007b-mix-full.log; exit "$result"'` | 0 | Full suite: 826 passed, 21 skipped, seed 900388; 297.85 s. Final contract-data changes were subsequently covered by the focused fixture run below. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'mix test test/symmetry_control/goals/contract_fixture_manifest_test.exs'` | 0 | Final 213-fixture manifest, one test passed, seed 768614. |

The table expands the component commands from the executed shell chains; each
successful component printed an explicit zero marker. Go event counts separate
top-level tests from subtests to avoid double-counting them.

### Round 2 gate re-runs (pre-rebase revision `dae2388`, subject `sha256:aa344b7d…`)

The same containers and toolchains were reused on the fixed tree. Round 2
changed only `control/lib/**` and `control/test/**`; the node gates were re-run
because the isolated workspace had been left at the earlier round-1 file set,
after its gate-relevant files were synced to that revision (306 files
verified byte-identical between `/isolated` and the working tree). Every result
in this section is bound to `dae2388` (`sha256:aa344b7d…`); its 17-schema /
213-fixture rows describe that revision's corpus.

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'cd /app/control && mix format --check-formatted && mix compile --warnings-as-errors && mix test test/symmetry_control_web/portal_session_test.exs test/symmetry_control_web/controllers/portal_controller_test.exs'` | 0 | Formatter and warnings-as-errors compile clean; the two focused suites report **20 passed**. |
| Same focused command with the unmodified `HEAD` copies of `router.ex` and `portal_session.ex` restored in the test image | 2 | Pre-fix red proof: **18/20 passed, Failed: 2 tests** — the work document returns 404 and the filtered project URL is dropped to `/portal`. |
| `docker exec -w /isolated symmetry-0007b-node pnpm contracts:check` | 0 | 32 runtime tests passed, 17 Draft 7 schemas, 213 fixtures, generated DTOs reported up to date, Effect decoded all 213 raw fixtures (209 reached schema validation). |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit` clean on the synced frontend sources. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend lint` | 0 | `eslint . --max-warnings 0` clean. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend test` | 0 | 7 files, 62 passed; 9.06 s. jsdom still reports its unimplemented `Window.scrollTo`. |
| `docker exec -w /isolated symmetry-0007b-node pnpm --filter @symmetry/frontend build` | 0 | `vite build && node scripts/stage-assets.mjs`; 2866 modules; `index-D8XHWnI_.js` 656.77 kB / gzip 199.06 kB, `index-D7KuRjr5.css` 20.92 kB / gzip 5.24 kB; the Vite greater-than-500-kB chunk warning remains. |
| `go vet ./...` in `daemon/` | 0 | Windows host vet passed on the round-2 revision. |
| `go test -json ./...` in `daemon/`, redirected to `.tmp/0007b-round2-go-all.jsonl` | 1 | **1,728 top-level passes, 55 top-level skips, 2,351 subtest passes, 20 packages passed, one package failed**; 311.9 s. Two `daemon/internal/app` tests failed on start deadlines: `TestGoalAdmissionLeavesProviderAccessNilForNativeHarness` ("native turn did not start", 1 s deadline) and `TestReconcilePendingRecoverySchedulesSnapshotBeforeBackoff` ("Control snapshot assignment was not started after pending recovery reconcile", 7.38 s). `daemon/**` is unchanged by both rounds except the round-1 contract artifacts, and neither failing file was edited. |
| `go test ./internal/app` in `daemon/`, alone after the full run | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/app 274.200s` — both previously failing tests pass when the host is not also running the browser suite, the live daemon fixtures and another agent's workloads. |
| `docker exec -d -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'cd /app/control && mix test > /tmp/0007b-round2-mix-full2.log 2>&1; echo "EXIT=$?" >> /tmp/0007b-round2-mix-full2.log'` | 0 | Clean full suite on the fixed tree: **826 passed, 21 skipped**, seed 357852, 423.0 s — the same totals as round 1. |

One earlier full-suite attempt, started while the broad browser suite and the
node gates were still running, reported `825/826 passed, 21 skipped, Failed: 1
test` (exit 2): `SymmetryControlWeb.ProviderActionControllerTest` "Goal provider
claim scope locks Project before Goal" failed in `setup` with
`:ets.new(:provider_action_test_state, [:named_table, :public, :set])` raising
"table name already exists"
(`provider_action_controller_test.exs:258`). That setup creates a fixed-name
named table with no `whereis` guard, and it is unrelated to the two changed
surfaces; the isolated re-run above passes. It is recorded as a contended-run
flake with its evidence, not as a passing gate.

The two `daemon/internal/app` failures follow the same contended-run pattern:
both wait on a goroutine start with a 1 s or 3 s deadline
(`harness_test.go:4601`, `recovery_isolation_test.go:625`), in files and a
package that neither round edited, and both pass in the isolated package re-run
above. The `./...` exit code stays `1` in this receipt; the isolated re-run is
recorded as a separate observation, not as retroactive green.

The freshly built assets are byte-identical to the ones the running release
serves: `index-D8XHWnI_.js`
`sha256:b45d41c17e269e0f213f06a9d8842e9a43c5ba2522fdbbcecb1a8ba39bda0f62` and
`index.html`
`sha256:331cf9b2943fc588cf148d410536b837e99ff6aadfa0824138f90aefd7b31613` in
`/isolated/control/priv/static/portal/`, `/app/control/priv/static/portal/` and
the release's own `priv`, so the release under test serves exactly the
round-2 revision's build (`dae2388`).

pnpm's pre-run dependency verification attempted the registry on every scripted
run and failed with `ERR_PNPM_RESOLVING_NPM_RESOLVER_NETWORK_ERROR` /
TLS `HandshakeFailure` (`ajv`, `quicktype-core`); pnpm printed its own remedy,
"Set verifyDepsBeforeRun: false to skip this install", and then ran the script
anyway. The gates above report their own exit codes from the tools that ran
(`tsc`, `eslint`, `vitest`, `vite`); pnpm's warning that `node_modules` may be
out of sync is a statement about its failed verification step, not about those
assertions, and two of the runs spent minutes waiting on the unreachable
registry before the script started. The frozen-install gate therefore remains
unpassed, exactly as in round 1 — no lockfile, registry policy or TLS validation
was weakened to make it pass.

### Regression proofs and bounded repairs

- Whole valid task-scoped-command workspace/detail/timeline fixtures failed
  against the old generated bundle (exit 1, three rejected projections). Actual
  queued cancel/retry commands may have `run_id: null` and `generation: null`.
  The descriptive schemas now allow the null pair only in command-owned
  identities; event/transition identities and task generation remain strict.
- The whole legal legacy-result detail fixture failed against the old bundle
  (exit 1) at `/outcome/summary` and `/outcome/tests`. The transition API accepts
  JSON-compatible result maps and PortalJSON forwards those fields verbatim.
  Their presence remains required, their values remain open JSON, and the
  frontend narrows them before presentation. No backend normalization was added.
- The final all-fixture gates pass both regressions across Ajv, native Effect,
  Go, and the Elixir fixture validator. These are contract regression fixtures,
  not substitutes for real browser data.
- The Elixir fixture test adapts canonical ECMA-262 `\\u0000` pattern escapes
  to PCRE2 `\\x{0}` in a resolved validator copy, exactly as the existing
  production validator does. Only the test helper changed. The generated
  canonical bundle and existing workspace production validator remain intact.
- Scoped simplification removed the unused summary-state computation and an
  unused export, retained board grouping/order, and removed the empty-string
  cursor sentinel. No registry abstraction, identifier pin set, or new scheduler
  was introduced.

## Toolchain failures and verification limits

The repository lockfile, manifests, TLS validation, and supply-chain policies
were not weakened to make a gate pass.

| Command / attempt | Exit / state | Actual outcome |
| --- | --- | --- |
| `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile` (two root attempts) | Cancelled; no exit code | Parent cancelled stalled supply-chain verification. No success and no invented cancellation exit code. |
| `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend test` at the original root | Timeout; no exit code | 180.02 s in supply-chain verification; zero tests executed. |
| `mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance exec playwright --version` at the original root | Timeout; no exit code | 45 s; runner version was not obtained by this probe. |
| `mise exec pnpm@12.17.0 -- pnpm install --offline --frozen-lockfile --reporter=ndjson` | 1 | Missing official-registry metadata for `@babel/core`: `ERR_PNPM_META_FETCH_FAIL` / `NO_OFFLINE_META`. |
| `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile --fetch-timeout=15000 --fetch-retries=1 --network-concurrency=16 --reporter=ndjson` | 2 | Unsupported `--fetch-retries`; no installation. |
| `mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile --fetch-timeout=15000 --network-concurrency=16 --reporter=ndjson` | 1 | Official `@babel%2Ftemplate` metadata fetch failed with TLS `HandshakeFailure`; 354.86 s. |
| Same frozen install with disposable `XDG_CONFIG_HOME=.tmp/pnpm-config` official-host-to-mirror routing | 1 | `ERR_PNPM_TARBALL_URL_MISMATCH`, 316 entries: mirror metadata URLs differed from the locked official URLs; temporary configuration removed. |
| Linux container original-lock `pnpm install --frozen-lockfile --fetch-timeout=15000 --network-concurrency=16` | 1 | Official `@exodus/bytes` metadata/tarball TLS failure; 589.43 s. |
| First Elixir read-fixture validation | 2 | PCRE2 rejected canonical `\\u0000` in the meta-schema; repaired by the test-only adaptation above. |
| First full Elixir suite with a 300 s command deadline | Timeout; no completion exit code | No final suite result; the orphaned test VM was stopped before the successful 900 s-deadline run. |
| Initial Linux browser run against `http://symmetry-0007b-control:4000` | 1 | 27 failed, one skipped; all failures were login 403 / missing Refresh, not UI assertions. Native `127.0.0.1` sessions work. The exact hostname/origin cause is unproven; backend CSRF was not changed. |
| Initial native live-daemon runner | 1 | One desktop lifecycle pass, two history failures from comparing independently signed cursors, one intentional mobile lifecycle skip. Fixed the oracle to bind the exact response consumed by the UI. |
| Old malicious-response probe | 0, invalid proof | Passed through typed-boundary rejection of a malformed injected `external` object. Replaced with schema-valid, positively rendered malicious text/URL assertions; the old pass is not security evidence. |
| Intermediate frontend checks | 1 | Initial optional-field/type errors, one unscoped text assertion, a later no-await test callback lint error, and three modal-background role locators. Corrected rather than suppressed; final gates are recorded above. |
| A command with `--filter symmetry-frontend` | No tests executed | The package is `@symmetry/frontend`; four zero-match messages were not treated as passing gates. Corrected commands appear above. |
| Round 2: `pnpm --filter @symmetry/frontend test`/`build` pre-run installs inside `symmetry-0007b-node` | Stalled, then warned | `ERR_PNPM_RESOLVING_NPM_RESOLVER_NETWORK_ERROR` / TLS `HandshakeFailure` for `ajv` and `quicktype-core`; the stalled installs were stopped instead of retried indefinitely, and the wrappers then ran the real scripts (row details in the round-2 gate subsection above). |

### Isolated dependency route

The normal-check-enabled mirror workspace preserved the four workspace manifests'
versions and pinned the four transitive versions to the original graph:
`@eslint-community/regexpp@4.12.2`, `parse5-parser-stream@8.1.0`, `parse5@8.1.0`,
and `seroval@1.7.9`. Both Linux and native isolated lockfiles were parsed and
compared with the original: all 378 package keys, integrity values, dependency
snapshots, and importer dependency records matched; only origin URLs differed.
No `trustLockfile`, TLS disabling, policy suppression, or repository lockfile
rewrite was used. Mirror checks are useful execution evidence but **do not turn
the original frozen installation into a passing gate**.

Linux/race Go qualification, native credentialed-provider qualification, and the
complete original CI/merge matrix are not established by the Windows Go and
fake-agent runs. Real daemon execution here means the production daemon/control
path with a deterministic fake-agent process, not an installed native provider.

The read API serves no acceptance identity or work-item usage/cost records.
Reported output is therefore never relabelled accepted, unknown cost is never
`$0`, and the absence label is not accepted-evidence proof. The real manual-CI
fixtures exercise unknown freshness, not the timestamp-stale provider branch;
the latter has deterministic presentation-unit coverage only.

## Executed browser gates

Native Playwright used Chromium 156.0.8078.4 with Playwright 1.64.1, the staged
production release at `http://127.0.0.1:4000`, and the verified non-junction
workspace `.tmp/0007b-tooling`. The expiry fixture was the same release at
`http://127.0.0.1:4001` with a two-second session lifetime. Desktop captures use
a 1280 by 720 viewport; narrow captures use 412 by 839. The latter's PNG pixel
dimensions are larger because of device scale factor.

| Executed command | Exit | Actual result / artifact |
| --- | ---: | --- |
| `playwright test --list` (workspace package runner) | 0 | 94 cases in nine files; discovery only. |
| `playwright test` (expanded package test script, `.tmp/0007b-tooling/browser`) | 1 | First complete native run: 70 passed, 15 skipped, nine failed; 307.13 s. `.tmp/0007b-browser-final-results` and `.tmp/0007b-browser-final-report`. All retained classic, Chat, connections, and shell cases that ran passed. |
| `mise exec pnpm@12.17.0 -- pnpm --dir .tmp/0007b-tooling --filter symmetry-browser-acceptance test tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs` | 1 | After the first test-body repairs: 29 passed, one skipped, six failed; 115.00 s. Four fresh-session failures are the backend boundary conflict; two list cases exposed the next incorrect URL-encoding assertion. `.tmp/0007b-browser-v3-results` and `.tmp/0007b-browser-v3-report`. All 16 shell cases passed, including real expiry recovery. |
| `mise exec pnpm@12.17.0 -- pnpm --dir .tmp/0007b-tooling --filter symmetry-browser-acceptance test tests/portal-work-views.spec.mjs --grep "list, board, filters and summary"` | 1 | First focused list repair: both cases reached rendered board ordering, then failed because the nested `has` locator searched for another board inside each column. 17.88 s; `.tmp/0007b-list-v4-results` and `.tmp/0007b-list-v4-report`. |
| Same exact focused command above, after correcting only that locator | 0 | Final desktop and narrow list/board/filter/saved-view/refresh/order checks: two passed, 17.55 s wall / 14.7 s Playwright. `.tmp/0007b-list-v5-results` and `.tmp/0007b-list-v5-report`. |
| `pwsh -NoProfile -File .tmp/0007b-tooling/browser/scripts/run-live-daemon-tests.ps1` | 0 | Clean native runner: three passed, one intentionally skipped mobile lifecycle case; 97.44 s. Both widths executed the real daemon history test. `.tmp/0007b-live-v3-results` and `.tmp/0007b-live-v3-report`. No dependency-preflight error in this run. |

The focused release invocation used these exact environment assignments before
the command in the table:

```powershell
$env:SYMMETRY_PORTAL_URL = "http://127.0.0.1:4000"
$env:SYMMETRY_SHELL_EXPIRY_URL = "http://127.0.0.1:4001"
$env:SYMMETRY_PLAYWRIGHT_OUTPUT = Join-Path $PWD ".tmp/0007b-browser-v3-results"
$env:PLAYWRIGHT_HTML_OUTPUT_DIR = Join-Path $PWD ".tmp/0007b-browser-v3-report"
```

The two final focused list invocations used the same portal URL and respectively
`.tmp/0007b-list-v4-results` / `.tmp/0007b-list-v4-report` and
`.tmp/0007b-list-v5-results` / `.tmp/0007b-list-v5-report` for those two output
variables. The clean live-runner invocation set its output variables to
`.tmp/0007b-live-v3-results` and `.tmp/0007b-live-v3-report`; its script starts
the real control/daemon fixture. Native live-runner logs are at
`C:\Users\lhan\AppData\Local\Temp\symmetry-goal-02-live-6ca824f6386f445d862bfaebe0566b1e`.
Each PowerShell wrapper returned `$LASTEXITCODE`.

These round-1 runs are separate observed runs, not an invented combined green
94-case run; the four fresh-session failures they recorded were the authorized
boundary conflict that round 2 fixed. The new desktop concurrency case passed;
its narrow counterpart is intentionally skipped. Other skips in the broad run
include environment-gated live/Compose suites and pre-existing mobile
concurrency exclusions; a skip is not proof.

### Round 2 runs (pre-rebase revision `dae2388`)

The release was rebuilt from the fixed tree (`mix release --overwrite` in
`/app/control`, then both instances restarted: primary on `:4000`, expiry on
`:4001` with `SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2`) and both suites were
re-run against it with Chromium 156.0.8078.4 / Playwright 1.64.1 from
`.tmp/0007b-tooling`.

| Executed command | Exit | Actual result / artifact |
| --- | ---: | --- |
| `mise exec pnpm@12.17.0 -- pnpm --dir .tmp/0007b-tooling --filter symmetry-browser-acceptance test tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs` | 0 | Focused release-shell gate: 36 cases, **35 passed, 1 skipped, 0 failed**; 2.9 min wall. Both fresh-session deep-link cases pass on both widths (cases 5, 6, 23, 24). `.tmp/0007b-round2-focused-results` and `.tmp/0007b-round2-focused-report`. |
| `mise exec pnpm@12.17.0 -- pnpm --dir .tmp/0007b-tooling --filter symmetry-browser-acceptance test` | 0 | Broad native run, nine spec files: 94 cases, **79 passed, 15 skipped, 0 failed**; 304.25 s wall / 5.0 min Playwright. Round 1 was 70/15/9; the nine previously failing cases all pass. `.tmp/0007b-round2-broad-results` and `.tmp/0007b-round2-broad-report`; `.last-run.json` is `{"status":"passed","failedTests":[]}`. |
| `pwsh -NoProfile -File .tmp/0007b-tooling/browser/scripts/run-live-daemon-tests.ps1` | 0 | Native live runner on the round-2 release: **3 passed, 1 intentionally skipped** mobile lifecycle case; 95.8 s wall / 1.4 min Playwright. Both widths executed the real-cursor history case (`firstPageEntries` 100 → `firstEntriesAfterOlderPage` 121, `currentEntriesAfterRetry` 121), the same counts as round 1. `.tmp/0007b-round2-live-results` and `.tmp/0007b-round2-live-report`; logs at `C:\Users\lhan\AppData\Local\Temp\symmetry-goal-02-live-c3d449fb4b71404980b99a52f7f447ab`. |

Round-2 environment assignments (identical to round 1 except for the output
directories; the focused run used `.tmp/0007b-round2-focused-results` and
`-focused-report`, the broad run `.tmp/0007b-round2-broad-results` and
`-broad-report`):

```powershell
$env:SYMMETRY_PORTAL_URL = "http://127.0.0.1:4000"
$env:SYMMETRY_SHELL_EXPIRY_URL = "http://127.0.0.1:4001"
$env:SYMMETRY_PLAYWRIGHT_OUTPUT = Join-Path $PWD ".tmp/0007b-round2-broad-results"
$env:PLAYWRIGHT_HTML_OUTPUT_DIR = Join-Path $PWD ".tmp/0007b-round2-broad-report"
```

The two previously failing expectations now hold with their original
assertions, unchanged: `deep-link-work-reentry.json` records
`/portal/work/<uuid>?view=board` after login and reload, and
`deep-link-project-view.json` records
`/portal/projects/<uuid>?view=board&q=keeper` after login and reload, for both
widths, under these round-2 focused result directories (`<project>` is
`desktop-chromium` or `mobile-chromium`):

- `.tmp/0007b-round2-focused-results/portal-work-views-Readable-c33c8-link-over-its-project-board-<project>/`
- `.tmp/0007b-round2-focused-results/portal-work-views-Readable-77c3c--board-and-filter-deep-link-<project>/`

The 15 skips are the same environment-gated live/Compose and pre-existing
mobile-concurrency cases as round 1; the daemon-free skips are covered by the
native live runner recorded in the round-1 row above only for its own revision.

### Browser repair trail and additional tooling failures

- Round 2 added no browser-test repair: it changed only the two authorized
  backend surfaces, rebuilt the release with `mix release --overwrite` in
  `/app/control`, restarted both instances, and re-ran the untouched suites.
  The four fresh-session cases pass with their original assertions, and the
  broad suite moved from 70/15/9 to 79/15/0 (exit 0).
- The first broad run's five non-backend failures were fixture/helper return
  misuse, an unscoped drawer heading assertion, an unscoped duplicate cost-label
  assertion, and trying to use the disabled pending project picker. Repairs
  retained the ordered sections and both cost consumers, and switched projects
  through the real, usable SPA directory while the prior response stayed held.
- The subsequent list failure compared raw URL text to `1`, although TanStack
  serializes that string as JSON. It now proves the URL has the filter and that
  a reload restores the checked control, the matching row, and exclusion of the
  nonmatching row. No production serialization was changed. The final board
  oracle selects exactly one ready column, waits for all served cards, and
  compares their rendered order with the real position-sorted snapshot; the
  deliberately nonalphabetical Zulu/Mike/Alpha order is retained.
- The first native live run after the cursor repair returned exit 1: one
  lifecycle pass, two history failures, one skip (108.01 s). A legitimate copy
  of reported output in the raw timeline made a whole-detail text locator
  ambiguous. The locator now targets the reported-output section in live,
  Compose, and restart checks; the served summary/source/href assertions remain.
- A temporary junction-based workspace's focused release invocation returned
  exit 1 before tests: `ERR_PNPM_UNSAFE_TASK_RUN_STATE_PATH` (3.00 s).
- `pwsh -NoProfile -File .tmp/0007b-verification-v2/browser/scripts/run-live-daemon-tests.ps1`
  returned exit 0 with three actual passes and one skip (101.76 s), **but** pnpm
  first emitted `ERR_PNPM_OUTDATED_LOCKFILE` for the junction's local
  `@symmetry/contracts` dependency and warned that its pre-script install failed.
  Those artifacts are retained at `.tmp/0007b-live-v2-results`; that run is not
  claimed as a clean dependency gate. The final runner above uses the original
  verified non-junction workspace and has no such preflight failure. No policy
  switch was disabled to bypass either error.
- Fresh-session contexts now inherit the running project's five device options
  but no authenticated storage. Browser-error assertions still run on successful
  paths; `finally` closes the context without overwriting the primary 404 or
  lost-return-path assertion. Neither expected backend defect is allowlisted.

## Required evidence map

Capture roots below are repository-relative. Each `.png` has a same-basename
`.json` recording the actual URL, viewport, and observations. These are local
run artifacts, not checked-in screenshots or claims that a missing CI job ran.

- **L** = `.tmp/0007b-list-v5-results` (round-1 tree `sha256:3ddb5e44…`)
- **V** = `.tmp/0007b-browser-v3-results` (round-1 tree `sha256:3ddb5e44…`)
- **H** = `.tmp/0007b-live-v3-results` (round-1 tree `sha256:3ddb5e44…`)
- **R** = `.tmp/0007b-round2-focused-results` (round-2 revision `dae2388` /
  `sha256:aa344b7d…`)
- **B** = `.tmp/0007b-round2-broad-results` (round-2 revision `dae2388` /
  `sha256:aa344b7d…`)
- **H2** = `.tmp/0007b-round2-live-results` (round-2 native live runner,
  `dae2388`)
- **R3** = `.tmp/0007b-round3-focused-results` (pre-rebase round-3 review-repair
  tree `6398caa` / `sha256:193ddbb2…`)
- **B3** = `.tmp/0007b-round3-broad-results` (pre-rebase round-3 review-repair
  tree `6398caa` / `sha256:193ddbb2…`)
- **H3** = `.tmp/0007b-round3-live-results` (pre-rebase round-3 native live
  runner, `6398caa`)
- **X** = `.tmp/0007b-round3-before-results` (pre-fix release on the round-3
  tree with the new assertions: 3 failed / 32 passed / 1 skipped)
- **R6** = `.tmp/0007b-round6-focused-results` (round-6 fix tree over `49ca695` /
  source subject `sha256:5d86199e…`); its pre-fix keyboard-case red run on the
  same release is `.tmp/0007b-round6-before-results` (2 failed, both widths)
- **R7** = `.tmp/0007b-round7-focused-results` (round-7 fix tree over `49ca695`
  / source subject `sha256:851b1542…`); its pre-fix red runs on the round-6
  release are `.tmp/0007b-round7-before-results` (Clear-filters assertion, 2
  failed, both widths) and `.tmp/0007b-round7-before-delete-results` (delete
  assertion, reached in a scratch copy with the Clear-filters assertions probed
  out, 2 failed, both widths)
- **R8** = `.tmp/0007b-round8-focused-results` (round-8 fix tree over `45c0bc3`
  / source subject `sha256:d0ac0a48…`); its pre-fix re-parent red run on the
  round-7 release is `.tmp/0007b-round8-before-results` (2 failed, both widths)
- **R9** = `.tmp/0007b-round9-focused-results` (round-9 tree over branch HEAD
  `747b19b4` / source subject `sha256:c4002c27…`)
- **H9** = `.tmp/0007b-round9-live-results` (round-9 native live runner on the
  same revision; the only executor of the real-cursor history case)
- **D** / **N** = `desktop-chromium` / `mobile-chromium` in the exact directory
  suffixes below; N is the narrow-screen project, not a second desktop context.

The L/V/H rows record the round-1 observations; round 2 re-produced the same
captures on the round-2 revision (`dae2388`) under R (focused gate), B (broad
suite) and H2
(native live runner, which is the only executor of the real-cursor history
case). The two fresh-session rows below were failures in V and are re-pointed to
R, where they pass; the two history rows are re-pointed to H2. Round 3
re-produced the focused, broad and live captures on the pre-rebase review-repair
tree (`6398caa` / `sha256:193ddbb2…`) under R3/B3/H3 (all green), with the
pre-fix red run under X. Round 6 re-produced the focused list/board and
keyboard captures on the fix tree over `49ca695` under R6 (all green); its
pre-fix keyboard-case red run is `.tmp/0007b-round6-before-results`. Round 7
re-produced the same captures on its own fix tree under R7 (all green; the
list capture now covers the two focus-survivor assertions). Round 8 re-produced
the same captures under R8 (all green; the list capture covers the re-parent
assertions). Round 9 re-produced them under R9 on this revision (all green; the
list capture covers the CI-chip-class assertions) and re-ran the live suite
under H9, so the rows below point at R9/H9 for the current revision and keep the
earlier roots as history.

| Required item | Desktop and narrow evidence | Observed result / limit |
| --- | --- | --- |
| Real project list | `R9/portal-work-views-Readable-add7e--work-with-stable-URL-state-{D,N}/work-list.png` | Both passed. Real API-created work shows owner/state/blocker, unrecorded acceptance, unknown cost, and honest CI labels. |
| Real project board | Same directory, `work-board.png` | Both passed, five columns, 15 cards, actual nonalphabetical ready-column ordering, view retained after reload. Narrow board scrolls within its own surface. |
| Detail | `R9/portal-work-views-Readable-3f6fb-on-and-history-on-real-data-{D,N}/work-detail.png` | Both passed: desktop modal drawer versus narrow full page; Outcome, Latest accepted evidence, Blocker, Next action, Execution history in order. |
| Populated history and reported result/delivery | `H9/portal-live-daemon-Goal-2--fb491-er-daemon-records-on-demand-{D,N}/work-history-pagination.png` | Both passed **in round 9 on this revision** (round 2 ran the same case but its later changes to the detail rendering have their own round-9 execution): served nonempty reported summary and delivery source/links asserted, plus rendered `debug-001`. Each item's first page has 100 entries and becomes 121 after the real older page (`firstPageEntries`, `firstEntriesAfterOlderPage`, `currentEntriesAfterRetry` in the same-basename JSON, `.tmp/0007b-round9-live-results`). Round 1's capture is under H, round 2's under H2. |
| Empty project and history | `R9/portal-work-views-Readable-6d00e-the-project-and-work-routes-{D,N}/work-list-empty.png`; detail directory above, `work-history-empty.png` | Both widths passed; empty states belong to real API-created projects/work with no history, not substituted DTOs. |
| Project/detail errors and recovery | Same `6d00e` directory, `work-list-error.png` and `work-detail-error.png` | Both passed. Deliberately abort real requests; no fabricated successful payload. Retry must render the real response afterward. |
| Stable filter/view state and keyboard navigation | `R9/portal-work-views-Readable-69464-focus-return-keep-URL-state-{D,N}/keyboard-walk.json` and `.png` (rounds 3–6's V/R6 captures remain the earlier revisions' records) | Both widths passed sequential filter typing (focus stays in the input after every keystroke, value `Keyboard`, URL `q=Keyboard`), text/state/blocked filtering, saved-view restoration, reload, list Enter, board arrow navigation, detail open and Escape/Back focus return. The recorded desktop sequence is unchanged since round 6 (Enter, Shift+Tab, Shift+Tab, Tab, Escape, ArrowDown, ArrowUp, ArrowRight, ArrowLeft, Enter, Escape). |
| Fresh signed-out work URL | `R9/portal-work-views-Readable-c33c8-link-over-its-project-board-{D,N}/deep-link-work-reentry.json` and `.png` | Both widths re-executed in round 9 (round 2 established the behaviour): the signed-out document load redirects to login with `/portal/work/<uuid>?view=board` as `return_to`, login returns to that URL, and the reload keeps the drawer over its project board. Round 1's failure artifacts (HTTP 404) remain under V. |
| Fresh signed-out filtered board URL | `R9/portal-work-views-Readable-77c3c--board-and-filter-deep-link-{D,N}/deep-link-project-view.json` and `.png` | Both widths re-executed in round 9 (round 2 established the behaviour): login `return_to` is the complete `/portal/projects/<uuid>?view=board&q=keeper`, the filtered board renders on return and after reload. Round 1's failure artifacts (received `/portal`) remain under V. |
| Project switch, detail switch, refresh, late responses | `R9/portal-work-views-Readable-af807-oject-s-data-or-newer-state-desktop-chromium/late-response-guard.json` and `.png` | Desktop passed in round 9 with both held real workspace and detail responses recorded as delivered; after release and the newer real refresh, no held project/work/detail data replaced current state. Narrow concurrency is not claimed (that case is skipped by design). |
| Foreign claimed project in work URL | `R9/portal-work-views-Readable-5094f--never-renders-foreign-work-{D,N}/work-url-canonicalization.json` and `.png` | Both passed; the actual served project owns the picker/background/navigation and foreign filters/data are cleared. |
| Older-history failure and Retry | Real-history JSON under H9 above (`faultAttemptOutcomes`, `retryCursorBoundTo`, `obsoleteFailureOutcomes`) | Both widths passed in round 9. An obsolete delivered 503 does not affect the newly selected work; a current failed older-page load renders the error and Retry, then requests the cursor from that navigation's consumed detail response and exhausts the real older page. Signed cursors from separate GETs are not compared. |
| Truth rules | List/board/detail captures above and `frontend/src/features/work/presentation.test.ts` | Unknown cost is not `$0`; missing acceptance is not accepted evidence; real manual passed-CI fixtures have unknown freshness and no green tone; the real stale-CI fixture row bound through `ci_resource_id` renders the stale `CI passed · bound resource stale` label. Round 9 additionally asserts the rendered `chip-*` class on those real rows (stale → `chip-stale`, manual/unknown → `chip-unknown`) and pins `chip-passed` for a confirmed fresh provider row in the unit suite, so a fixed green class cannot pass. Provider timestamp-stale handling has unit proof only, not a real-provider browser capture. |
| Unsafe read content | `R9/portal-work-views-Readable-7f7fe-er-as-markup-or-unsafe-URLs-{D,N}/malicious-response-injection.json` and `.png` | Both passed with schema-valid, positively rendered malicious strings and consumed unsafe URLs. Explicitly synthetic fault-injection/security evidence, **not** real-data surface proof. |
| Classic parity and retirement | Migration map below; broad-run report and the final scoped runs | Executed retained classic cases pass; replaced read assertions moved to new routes, not dropped. Compose/restart additions remain unexecuted. |

The coordinator also opened the actual release in a browser and inspected the
desktop drawer, narrow work page, and narrow board, then opened the produced
list/board/detail/history/empty/error captures. This establishes inspection,
not the owner's qualitative clarity/density verdict. Full-page narrow history
captures are long; their same-basename JSON separates work identities and
phase-bound counts instead of attributing one item's cursor/count to another.

### Same-change classic-check retirement

`browser/tests/portal.spec.mjs` goes from 12 to eight declarations. Four
classic read-only declarations are retired **with their predicates migrated**:

| Retired classic declaration | Replacement |
| --- | --- |
| `detail output encodes untrusted statuses and rejects unsafe links` | New typed detail/unknown-value checks and schema-valid malicious detail test; real live output/delivery checks remain separate. |
| `a delayed detail response cannot overwrite a newer drawer selection` | New held-real-response project/detail selection test and foreign-project URL test. |
| `an older history failure cannot notify or mutate a newer drawer` | Always-on, explicitly synthetic obsolete-history guard in the new CI-run spec **and** the real daemon older-page/503/Retry case on both widths. |
| `a delayed workspace response cannot replace the latest project` | New held-real-workspace project-switch/refresh case. |

The mixed CRUD/filter/order and keyboard/dialog tests retain classic mutation
coverage; read-only list/board/filter/order and keyboard predicates move to
`browser/tests/portal-work-views.spec.mjs` (10 declarations). Remaining classic
dirty-form refresh, project/work optimistic-concurrency, archive/restore,
resource, and session-security checks are retained. The broad run executed
12 retained classic cases successfully; its four classic mobile concurrency
cases kept their pre-existing skips.

The existing CI browser command only gains
`tests/portal-work-views.spec.mjs`. `portal-live-daemon.spec.mjs`,
`portal-compose-daemon.spec.mjs`, and `portal-compose-restart.spec.mjs` also
gain new-route read assertions without removing their original control/recovery
flows. This run exercised the native live suite, **not** the Compose/restart
gates or the native credentialed-provider matrix.

## Measurement and evidence subject

Round 1's measurement command read the two completed `work-list.json` files
under L with PowerShell `Get-ChildItem -Recurse -Filter work-list.json` and
`ConvertFrom-Json`, then applied `Measure-Object -Maximum` to
`observations.snapshotBytes` and `observations.workItemCount` (exit 0); the
desktop response measured 56,351 bytes and the narrow run's independently
created project 56,632 bytes. Round 2 re-ran the same list case in both the
focused and broad suites and re-read the same fields from those four files
(desktop 56,913 / 66,861 bytes, narrow 62,168 / 74,388 bytes). Each selected
project had 15 work items, and the maintained maximum for this revision is in
the measurements table above. Round 3 re-ran the same list case in both suites
and re-read the same fields from the four round-3 `work-list.json` files
(focused 95,087 / 100,342 bytes; broad 105,035 / 112,562 bytes), again all
15 work items; the maintained maximum is the round-3 value in the table above.
This is the largest observed **project-scoped fixture snapshot in this run**,
not a scalability benchmark or a maximum supported size. Snapshot size also
includes the directory metadata served by the unchanged workspace endpoint.
Server-side work-item pagination remains absent; no list endpoint or pagination
shape was invented. History uses only the existing opaque cursor API.

Evidence-subject digests, each stated as the content identity of the revision
it describes (commit SHAs below are informative only; a rebase rewrites them):

- round-1 pre-rebase tree —
  `sha256:3ddb5e44551f2d498628e8456d87a40684d6a90f0b85b215a22148b5cdb008a6`
  (full-tree scope);
- round-2 pre-rebase tree — the revision committed as `dae2388` (base
  `343dea6a62484d73e7b577a0be2683ca233510f6`) —
  `sha256:aa344b7ddd3f6f5682b0a7963cd294a7d918b44200796fd8d2174af1615c9527`
  (full-tree scope, 390 entries);
- post-rebase feature tree — the revision committed as `03646d1` (base
  `4e60b9d4b7f113cfe0162fa8003a26a01e90e4c9`) —
  `sha256:391930d0f0f098e023254ca0a8899b4884dfe410809f913e5bbbe168ddcb2f60`
  (full-tree scope, 390 entries);
- round-3 pre-rebase review-repair tree — the revision committed as `6398caa` —
  `sha256:193ddbb2e257230b2e61e30d9d3a0431d0dfcd6329ef725e6a242b6bc5796817`
  (full-tree scope, 390 entries);
- round-4 uncommitted tree over `6398caa` —
  `sha256:353b10fcec5b3aba29a331a406f2efea0ba65761ccaae71afc514d381d6c1c3f`
  (full-tree scope, 390 entries; **historical evidence only**, not the revision
  this receipt describes);
- round-5 revision `01eb134` (branch HEAD content before round 5's pass) —
  source subject
  `sha256:4019477f8ebf9b80294960cff305092cec22de070157cf830529ad16b6e1c323`
  (393 entries, `docs/**` excluded, `-Scope source`) and full-tree digest
  `sha256:1e345643837bcaaf5f26e5218df52156965f564618257239100dceccbb02aca5`
  (396 entries, the historical variant that includes the three explicit
  `docs/**` files, `-Scope full`);
- round-6 fix tree over branch HEAD `49ca695` (historical: superseded by the
  round-7 fix tree, which added the focus-survivor change and corrected this
  file list) — source subject
  `sha256:5d86199ee37b0a36f6471a224d4ba42a8a81cd755b98dbb1fd357bc1786ff919`
  (393 entries, `docs/**` excluded, `-Scope source`) and full-tree digest
  `sha256:a8c7fcf3fd0fde7733565112cdaaa69ac3d737b607c5c7a0c800e25a6a42714c`
  (396 entries, `-Scope full`);
- round-7 fix tree over branch HEAD `49ca695` (historical: superseded by the
  round-8 fix tree) — source subject
  `sha256:851b154263961dbf3eeceae2b680ebf42e25a6f4fdee1c434a31b83eedacb259`
  (393 entries, `docs/**` excluded, `-Scope source`) and full-tree digest
  `sha256:896ded3be3c28fc657af6e76ad02f93d35dedac1d8a5283ced2ae9b28e1769a3`
  (396 entries, `-Scope full`).
- current round-8 fix tree over branch HEAD `45c0bc3` — source subject
  `sha256:d0ac0a48e4bf90fa403bc9ab156753b9ef4c1506eb5b8363b60542664269b01e`
  (393 entries, `docs/**` excluded, `-Scope source`) and full-tree digest
  `sha256:989330ee48f044a1d04dddfcdf3df93b81251c9b4a1da88bb0f0a7c94042ec24`
  (396 entries, `-Scope full`).

Every value was recomputed on this host with `pwsh -NoProfile -File
.tmp/0007b-subject-digest.ps1` (exit 0) — `-Scope source` (default) for the
source subject, `-Scope full` for the historical full-tree values — in an
extracted `git archive` copy of the named revision, or in the clean working
tree at it (rounds 3–6; rounds 7–8's values were recomputed in the working tree
that holds their uncommitted fix files). These are source-manifest digests, not commit hashes
and not a claim that the original frozen-install gate passed. The receipt and
local output artifacts are excluded from both scopes to avoid self-reference;
the `docs/**` exclusion keeps the source subject insensitive to documentation
edits, including this receipt's own round-6 and round-8 edits (recomputed after them:
unchanged).

Reproduction algorithm (`-Scope source`, the durable binding): recursively
enumerate files under the following roots; add the direct files in `frontend/`,
`browser/`, and `contracts/`; add the explicit files below except every path
under `docs/`; **exclusion rule: no path under `docs/` is ever in scope**, so
editing this receipt or any file under `docs/` cannot change the value recorded
above (files under other documentation paths, such as `browser/README.md`, ARE
in scope and do change it); deduplicate full paths. For each file compute lowercase SHA-256 and a
repository-relative path with `/` separators. Sort by path using PowerShell
`Sort-Object -Property Path -Culture ""`. Hash UTF-8 without BOM of
`hash + two spaces + path + LF` for every entry, including the final LF.
`-Scope full` runs the identical algorithm with the `docs/**` exclusion
removed; it reproduces the historical full-tree values, including `01eb134` →
`sha256:1e345643…`.

```text
Recursive roots:
frontend/src
browser/tests
browser/scripts
contracts/v1
contracts/ts
contracts/scripts
contracts/fixtures
contracts/generated
daemon/internal/contracts
control/lib

Explicit files (`-Scope source` drops the last three `docs/**` lines):
package.json
pnpm-lock.yaml
pnpm-workspace.yaml
.github/workflows/ci.yml
daemon/internal/protocol/goal_contract_fixture_test.go
control/test/symmetry_control/goals/contract_fixture_manifest_test.exs
docs/goals/0007b-readable-work-views.md
docs/design/frontend.md
docs/design/quality.md
```

The production frontend and generated contracts did not change during the
last browser test-body repairs or in round 2. Round 2 changed only
`control/lib/symmetry_control_web/{router,portal_session}.ex` and their two
Elixir test files, plus one documentation paragraph in `browser/README.md`;
the browser, contract and frontend artifacts above are the same bytes as round
1's, and the round-2 runs re-executed them against the rebuilt release. Round 6
changed the production shell (`frontend/src/app/shell.tsx`) and the two test
surfaces above; the digests recorded for the round-6 fix tree name that change
and are not restated for any earlier revision.

### Round 3 gate re-runs (pre-rebase review-repair tree `6398caa`, subject `sha256:193ddbb2…`; historical)

Node gates ran against the verified workspace copy `.tmp/0007b-tooling` (the
isolated dependency graph; package versions identical to the repository
manifests). The frontend release build was additionally re-run in the
`symmetry-0007b-node` container at `/isolated` because the workspace copy nested
inside the repository changes Tailwind's automatic source detection (git-root
scope) and would otherwise emit a different stylesheet; the container build
reproduces the round-2 asset pipeline (`index-D7KuRjr5.css` byte-identical to
round 2). Elixir ran in `symmetry-0007b-control`; Go on the host. The release
was rebuilt with `mix release --overwrite` and both instances restarted with
`.tmp/0007b-restart-release.sh` (the script's own readiness loop reports
`wget: not found` inside this container; readiness was verified from the host
with `curl` `4000=200` / `4001=200`). Every row below is bound to that
pre-rebase revision (`6398caa` / `sha256:193ddbb2…`) and to the tooling-copy
state of that time; the tooling copy's contract scope was re-synced to the
revision `01eb134` in round 5 ("Round 5 verification"), and these rows are not
relabelled as current.

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `./node_modules/.bin/vitest run` in `.tmp/0007b-tooling/frontend` | 0 | 7 files, **70 passed** (was 62; the review repairs add the A02/A03/A16/A01/A09 assertions). |
| `./node_modules/.bin/tsc --noEmit` | 0 | Clean on the derived `Loosen<>` presentation types and tests. |
| `./node_modules/.bin/eslint . --max-warnings 0` | 0 | Clean. |
| `./node_modules/.bin/vite build && node scripts/stage-assets.mjs` in the container `/isolated/frontend` | 0 | `index-uvBWRZjr.js` 658,114 B / gzip 197,130 B; `index-D7KuRjr5.css` 20,927 B / gzip 5,279 B (byte-identical to round 2's CSS); `index.html` 525 B. sha256 `b2d67ac6…` (js) / `c9805a9b…` (css); the release bundle serves exactly these bytes. |
| `./node_modules/.bin/tsc --project contracts/tsconfig.json` + `node --test contracts/scripts/*.test.mjs` + `node contracts/scripts/check.mjs` + `node contracts/scripts/check-effect.mjs` in `.tmp/0007b-tooling` | 0 | Typecheck clean; **32 runtime tests**; 17 Draft 7 schemas; 213 fixtures; `Generated contract DTOs are up to date.`; Effect decoded all 213 raw fixtures (209 parsed reached Schema validation). |
| `go vet ./...` in `daemon/` | 0 | Passed. |
| `go test -count=1 ./internal/contracts ./internal/protocol` in `daemon/` | 0 | Both packages `ok` (3.0 s / 5.4 s) on the corrected fixtures. |
| `mix format --check-formatted && mix compile --warnings-as-errors && mix test test/symmetry_control_web/portal_session_test.exs test/symmetry_control_web/controllers/portal_controller_test.exs` in the container `/app/control` | 0 | Formatter/compile clean; **20 passed**. |
| `mix test test/symmetry_control/goals/contract_fixture_manifest_test.exs` | 0 | **1 passed** (all 213 manifest fixtures). |
| Focused browser run against the pre-fix release with the new assertions (`.tmp/0007b-round3-before-results`, exit 1) | 1 | **3 failed, 32 passed, 1 skipped** — the stale-CI bound-resource label on both widths, plus the first focus-order assertion variant that the measured drawer behavior replaced (the drawer does not focus Close on open; the migrated assertion now follows the measured order). |
| Focused release-shell run on the rebuilt release (`.tmp/0007b-round3-focused-results`) | 0 | **36 cases: 35 passed, 1 skipped, 0 failed**. |
| Broad browser run on the rebuilt release (`.tmp/0007b-round3-broad-results`) | 0 | **94 cases: 79 passed, 15 skipped, 0 failed** — the same skips as round 2. `.last-run.json` is `{"status":"passed","failedTests":[]}`. |
| `pwsh -NoProfile -File .tmp/0007b-tooling/browser/scripts/run-live-daemon-tests.ps1` (`.tmp/0007b-round3-live-results`) | 0 | **3 passed, 1 intentionally skipped**. Real-cursor history counts unchanged from round 2 (`firstPageEntries` 100 → `firstEntriesAfterOlderPage` 121; `currentEntriesAfterRetry` 121; three delivered faults; `retryCursorBoundTo` is the cursor of the consumed detail response). |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` | 0 | Round-3 review-repair tree (the revision committed as `6398caa`): `sha256:193ddbb2e257230b2e61e30d9d3a0431d0dfcd6329ef725e6a242b6bc5796817` (390 entries). |

Red-before-green proofs, per repaired surface: frontend `presentation.test.ts`
+ `router.test.tsx` ran red against the unfixed round-3 sources (`2 files
failed, 7 failed / 31 passed`) with the new assertions before the fixes and
green (`39 passed`, including the added boundary test) after, and the
superseded-boundary test was separately re-proved red against the old query key
(`['cursor-1','cursor-1b']` vs the expected `['cursor-1','cursor-2']`); the
Elixir session focus suite ran red `7/8 passed` against the unchanged allowlist
and green `20 passed` after; the browser focused suite ran red `3 failed` on the
pre-fix release and green after the rebuild; the fixture probe showed the old
failure reasons at `data` (not the enum) and the fixed fixtures failing only at
the `source` const. The Compose/restart suite and the native
credentialed-provider matrix remain unexecuted (limits below).

### Round 4 fix verification (historical: uncommitted tree over `6398caa`, subject `sha256:353b10fc…`)

Scope: the three round-2 Sol findings (unrecognised structured output, durable
revision binding, workspace trailing newline). Node gates ran in
`.tmp/0007b-tooling/frontend`; the release-shell gate ran on the release
rebuilt from the fix (rows below). Both copies are byte-identical to the
repository tree for the changed files (`diff -rq frontend/src
.tmp/0007b-tooling/frontend/src` exit 0; the five `docker cp`d files compare
`OK` against `/repo-src` by `sha256sum` inside the node container). Every row
below is historical evidence bound to `353b10fc…`; the same source content is
now part of the branch history (commit `01eb134`).

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `./node_modules/.bin/vitest run src/features/work/presentation.test.ts src/app/router.test.tsx` in `.tmp/0007b-tooling/frontend`, new assertions against the unfixed `presentation.ts` | 1 | **2 failed, 38 passed (40)** — exactly the new assertions: the served `outcome.tests={passed:12,failed:0}` was absent from the detail and `evidenceRows({passed:12,failed:0})` returned `[]`. |
| `./node_modules/.bin/vitest run` in `.tmp/0007b-tooling/frontend`, fixed tree | 0 | 7 files, **71 passed** (round 3: 70 passed; the new router case adds the difference). |
| `./node_modules/.bin/tsc --noEmit` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/eslint . --max-warnings 0` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` in the repository working tree | 0 | Round-4 fix tree: `sha256:353b10fcec5b3aba29a331a406f2efea0ba65761ccaae71afc514d381d6c1c3f` (390 entries). |
| `docker cp` of the five changed `frontend/src` files into the node container's `/isolated`, checked by `sha256sum` against `/repo-src` | 0 | All five compare `OK` (byte-identical). |
| `./node_modules/.bin/vite build && node scripts/stage-assets.mjs` in the container `/isolated/frontend` | 0 | `index-CBqVZxM1.js` 658,121 B / gzip 199.47 kB (round 3: 658,114 B); `index-D7KuRjr5.css` 20,927 B (byte-identical to round 3); sha256 `f1d3d9e6…` (js) / `c9805a9b…` (css); the >500 kB chunk warning remains. |
| `mix release --overwrite` in the container `/app/control` with `MIX_ENV=prod` | 0 | Release rebuilt over the round-4 assets. |
| `sh /repo-src/.tmp/0007b-restart-release.sh` in the control container, then host `curl` | 1 (script) | The script exits 1 at its `wget` readiness probe (`wget: not found`, the same limitation as round 3); host `curl` answers 4000=200 / 4001=200, and the served `/portal/assets/index-CBqVZxM1.js` answers 200 with 658,121 bytes. |
| Focused release-shell run on the rebuilt release (`SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4001`, `.tmp/0007b-round4-focused-results`) | 0 | **36 cases: 35 passed, 1 skipped, 0 failed** — the same set and result as round 3's focused gate (`portal-work-views` + `workspace-shell`, both widths). |

Not re-run for this tree: the broad browser suite, the live-daemon suite, the
Compose/restart specs, and the Elixir, Go and contract gates. The change is a
frontend presentation fallback plus a trailing newline, but it does change
detail rendering that the live suite asserts, so this tree carries no
live-suite evidence of its own: the round-3 results stay bound to the round-3
tree (`sha256:193ddbb2…`), and round 9 later re-executed the live suite on the
round-9 tree. Round 5 re-ran the contract and
frontend gates on revision `01eb134` (next section).

### Round 5 verification (revision `01eb134`, documentation-only pass)

The retained `.tmp/0007b-tooling` copy was re-synced to this revision before
these runs — `rsync -a --delete --exclude node_modules contracts/
.tmp/0007b-tooling/contracts/`, the same for `daemon/internal/contracts/`, then
verified byte-identical (`diff -rq .tmp/0007b-tooling/contracts contracts -x
node_modules` exit 0; `diff -rq .tmp/0007b-tooling/daemon/internal/contracts
daemon/internal/contracts` exit 0; `diff -rq .tmp/0007b-tooling/frontend/src
frontend/src` exit 0; `diff -rq .tmp/0007b-tooling/browser/tests
browser/tests` exit 0). The Elixir gate ran in `symmetry-0007b-control` after
`docker cp`-ing the current `contracts/v1`, `contracts/fixtures`,
`daemon/internal/contracts`, `control/lib` and `control/test` trees and
verifying `sha256sum` equality for the manifest, the schema bundle and the
contract validator.

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `./node_modules/.bin/tsc --project contracts/tsconfig.json` in `.tmp/0007b-tooling` | 0 | Contract typecheck clean. |
| `node --test contracts/scripts/*.test.mjs` in `.tmp/0007b-tooling` | 0 | 32 tests, 32 pass, 0 fail. |
| `node contracts/scripts/check.mjs` in `.tmp/0007b-tooling` | 0 | `Generated contract DTOs are up to date.`; `Validated 17 Draft 7 schemas and 219 fixtures.` |
| `node contracts/scripts/check-effect.mjs` in `.tmp/0007b-tooling` | 0 | `Effect decoded all 219 raw fixtures; 215 parsed documents reached Schema validation`. |
| `go test -count=1 ./internal/contracts ./internal/protocol` in `daemon/` (host) | 0 | Both packages `ok` (2.249 s / 2.402 s) on the 219-fixture corpus. |
| `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'cd /app/control && mix test test/symmetry_control/goals/contract_fixture_manifest_test.exs'` | 0 | **1 passed** (all 219 manifest fixtures; 4 files compiled on the synced sources). |
| `./node_modules/.bin/vitest run` in `.tmp/0007b-tooling/frontend` | 0 | 7 files, **71 passed**. jsdom still reports its unimplemented `Window.scrollTo`. |
| `./node_modules/.bin/tsc --noEmit` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/eslint . --max-warnings 0` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/playwright test --list` in `.tmp/0007b-tooling/browser` | 0 | **Total: 94 tests in 9 files**; declarations counted as `^\s*test\(`: 47 in 9 spec files. |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` in the repository working tree | 0 | Source subject (393 entries, `docs/**` excluded): `sha256:4019477f8ebf9b80294960cff305092cec22de070157cf830529ad16b6e1c323`. |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1 -Scope full` in the repository working tree | 0 | Full-tree digest of `01eb134` (396 entries): `sha256:1e345643837bcaaf5f26e5218df52156965f564618257239100dceccbb02aca5`. |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` (source scope), re-run after this receipt's edits | 0 | Same digest `sha256:4019477f…` (393 entries): the documentation edits did not change the source subject. |

Not re-run in round 5: the focused/broad/live browser suites, the
Compose/restart specs, the Elixir full suite, the Go full-tree run, and the
frozen dependency installation. Their last executed results remain bound to
their own revisions above.

### Round 6 verification (fix tree over `49ca695`, focus-reset repair)

Scope: the PR-bot P2 finding in `.tmp/0007b-findings-codex.md` — a
search-parameter-only navigation stole focus from the control being typed in.
`WorkspaceRoot`'s heading reset now keys on `state.location.pathname` instead
of `state.location.href`. Class audit: that effect is the only consumer of
`state.location.href` (the full navigation href; `location.href` itself also
appears in `application.ts` and in the router tests, and `useRouterState` also
in `shell.tsx`, but none of them reads the router's `state.location.href`), and every search-parameter-driven
control in the increment — filter input, state and owner selects, blocked
checkbox, saved-views select, list/board view links, Attention project picker
and the work-detail background filters — resolves to a same-pathname
`navigate`, so the one key change fixes the class. Changed files:
`frontend/src/app/shell.tsx`, `frontend/src/app/router.test.tsx` and
`browser/tests/portal-work-views.spec.mjs`. The changed shell is byte-identical
in the repository, `.tmp/0007b-tooling/frontend` and `/isolated/frontend`
(`sha256:a620128e7b0f84c7468cfff2cd8f3b600d676215e846a2e670c8a2929487babd`
at all three paths).

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `./node_modules/.bin/vitest run src/app/router.test.tsx` in `.tmp/0007b-tooling/frontend`, new tests against the unfixed `shell.tsx` | 1 | **1 failed, 25 passed (26)**: after typing "Subject" the filter held only `"S"` and `#content-heading` had focus. The route-change heading case passed on the unfixed tree too, as intended (it guards that real route changes still focus the heading). |
| Same focused command after syncing the fixed `shell.tsx` | 0 | **26 passed**. |
| `./node_modules/.bin/vitest run` in `.tmp/0007b-tooling/frontend` | 0 | 7 files, **73 passed** (round 5: 71; the two new tests). jsdom still reports its unimplemented `Window.scrollTo`. |
| `./node_modules/.bin/tsc --noEmit` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/eslint . --max-warnings 0` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/vite build && node scripts/stage-assets.mjs` in the container `/isolated/frontend` | 0 | `index-BEQ5CczN.js` 658.12 kB / gzip 199.47 kB (`sha256:f39c8bf84d2df733e392e75c8e81f43a733ba67a6b6c98e4cc9cf43ed37a3acc`); `index-D7KuRjr5.css` 20.92 kB unchanged; staged into `/isolated/control/priv/static/portal/`. |
| `docker cp` of the staged portal assets into `symmetry-0007b-control` + `mix release --overwrite` in `/app/control` (`MIX_ENV=prod`) | 0 | Release rebuilt; `/app/control/priv/static/portal` holds exactly the new asset pair; the served `index-BEQ5CczN.js` answers with the same sha256 `f39c8bf8…`. |
| `sh /repo-src/.tmp/0007b-restart-release.sh` in the control container | 1 (script) | Same known limitation as rounds 3–4: exits 1 at its `wget` readiness probe (`wget: not found`); host `curl` answers 4000=200 and 4001=200. |
| Focused browser run of the strengthened keyboard case against the **pre-fix release** (`SYMMETRY_PLAYWRIGHT_OUTPUT=.tmp/0007b-round6-before-results`) | 1 | **2 failed** (desktop and mobile), both at `expect(filter).toBeFocused()` after `pressSequentially("Keyboard")` — the real engine reproduced the finding on both widths before the rebuild. |
| Focused release-shell run on the rebuilt release (`SYMMETRY_PORTAL_URL=http://127.0.0.1:4000`, `SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4001`, `.tmp/0007b-round6-focused-results`) | 0 | **36 cases: 35 passed, 1 skipped, 0 failed** (2.2 min); `.last-run.json` is `{"status":"passed","failedTests":[]}`. The strengthened keyboard case passes on both widths; the measured snapshot is **154,931 bytes / 15 work items** (desktop 149,676; mobile 154,931), recorded in the measurements table. |
| `./node_modules/.bin/playwright test --list` in `.tmp/0007b-tooling/browser` | 0 | **94 tests in 9 files**; 47 declarations as `^\s*test\(` — unchanged (the keyboard case was strengthened, not added). |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` (`-Scope source`, then `-Scope full`) in the repository working tree | 0 | Source subject `sha256:5d86199e…` (393 entries); full-tree `sha256:a8c7fcf3…` (396 entries). |

Not re-run in round 6: the broad browser suite, the live-daemon suite, the
Compose/restart specs, the Elixir, Go and contract gates, and the frozen
dependency installation. The live suite asserts the detail surface this round's
shell change interacts with, so the round-6 tree carries no live-suite evidence;
their last executed results remain bound to their own revisions above, and
round 9 re-executed the live suite on the round-9 tree.

### Round 7 verification (fix tree over `49ca695`, focus-survivor repair)

Scope: the two P3 findings in `.tmp/0007b-findings-sol-round5.md`.
`frontend/src/features/projects/work-surface.tsx` gained the focus-survivor
helper and the two control ids, `frontend/src/app/router.test.tsx` the two
regression tests, and `browser/tests/portal-work-views.spec.mjs` the two
assertions inside the existing list/board case (no new declaration). Sync
before the runs: the three changed files copied from the repository into
`.tmp/0007b-tooling` and `/isolated/frontend`, verified byte-identical by
`sha256sum` (`router.test.tsx` `2100ada1…`, `work-surface.tsx` `6e9005a5…`);
`shell.tsx` stayed at the round-6 value `sha256:a620128e…` in the repository
and both copies. A concurrent external process (not this pass) then rewrote
the two retained frontend files back to their branch-HEAD content — file
mtimes `19:53:11`, with two unrelated runs left in the same copy
(`frontend/node_modules/.vite/vitest/…/results.json` `19:52:38`,
`browser/playwright-report/index.html` `19:53:57`) — so both files were
re-copied, `diff -rq` parity re-verified for `frontend/src` and `browser/tests`,
and the frontend gates were re-run on the restored content with identical
results (focused `28 passed`; full `7 files, 75 passed`; `tsc` and `eslint`
clean; `playwright test --list` `94 tests in 9 files`, all exit 0).

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `./node_modules/.bin/vitest run src/app/router.test.tsx` in `.tmp/0007b-tooling/frontend`, the two new tests against the unfixed `work-surface.tsx` | 1 | **2 failed, 26 passed (28)**: both at `toHaveFocus` — the filter input and the saved-views select were inactive (focus had nowhere to go once the clicked control was removed). |
| Same focused command after syncing the fixed `work-surface.tsx` | 0 | **28 passed**. |
| `./node_modules/.bin/vitest run` in `.tmp/0007b-tooling/frontend` | 0 | 7 files, **75 passed** (round 6: 73; the two new tests). jsdom still reports its unimplemented `Window.scrollTo`. |
| `./node_modules/.bin/tsc --noEmit` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/eslint . --max-warnings 0` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/vite build && node scripts/stage-assets.mjs` in the container `/isolated/frontend` | 0 | `index-BuYiGA6E.js` 658.28 kB / gzip 199.52 kB (`sha256:a5c6ece3dc638fd388ea9dbd40fc8ddaec87e0e1f3e0ef948d4dc6e6fcdcad29`); `index-D7KuRjr5.css` 20.92 kB unchanged (`sha256:c9805a9b…`); staged into `/isolated/control/priv/static/portal/`. |
| `tar` copy of the staged portal assets into `symmetry-0007b-control` + `mix release --overwrite` in `/app/control` (`MIX_ENV=prod`) | 0 | Release rebuilt; `/app/control/priv/static/portal` and the release priv hold exactly the new asset pair, and the release's `index.html` references `index-BuYiGA6E.js`. The served asset `GET /portal/assets/index-BuYiGA6E.js` answers with the identical sha256 `a5c6ece3…`, so the release under test serves exactly this revision's build. |
| `sh /repo-src/.tmp/0007b-restart-release.sh` in the control container | 1 (script) | Same known limitation as rounds 3–6: exits 1 at its `wget` readiness probe (`wget: not found`); host `curl` answers 4000=200 and 4001=200. |
| Focused browser run of the strengthened list/board case against the **pre-fix release** (the round-6 build), `.tmp/0007b-round7-before-results` | 1 | **2 failed** (desktop and mobile) at `expect(page.getByLabel(ui.filter)).toBeFocused()` after `Clear filters` — the real engine reproduced the reported gap on both widths (`Received: inactive` while the empty filter input was present). |
| Same pre-fix release with the Clear-filters assertions probed out of a scratch copy of the spec (restored byte-identical from the repository afterwards) to reach the delete path, `.tmp/0007b-round7-before-delete-results` | 1 | **2 failed** (desktop and mobile) at `expect(page.getByLabel(ui.savedViews)).toBeFocused()` after `Delete saved view` (`Received: inactive`), so the second assertion is also red on the unmodified behaviour. |
| Focused release-shell run on the rebuilt release (`SYMMETRY_PORTAL_URL=http://127.0.0.1:4000`, `SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4001`, `.tmp/0007b-round7-focused-results`) | 0 | **36 cases: 35 passed, 1 skipped, 0 failed** (2.6 min); `.last-run.json` is `{"status":"passed","failedTests":[]}`. Both new focus assertions pass on both widths; the measured snapshot is **166,005 bytes / 15 work items** (desktop 160,748; mobile 166,005), recorded in the measurements table. |
| `./node_modules/.bin/playwright test --list` in `.tmp/0007b-tooling/browser` | 0 | **94 tests in 9 files**; 47 declarations as `^\s*test\(` — unchanged (the case was strengthened, not added). |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` (`-Scope source`, then `-Scope full`) in the repository working tree | 0 | Source subject `sha256:851b1542…` (393 entries); full-tree `sha256:896ded3b…` (396 entries). |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` (source scope), re-run after this receipt's edits | 0 | Same digest `sha256:851b1542…` (393 entries): the round-7 documentation edits did not change the source subject. |

Not re-run in round 7: the broad browser suite, the live-daemon suite, the
Compose/restart specs, the Elixir, Go and contract gates, and the frozen
dependency installation. The live suite asserts the work surface this round
changed, so the round-7 tree carries no live-suite evidence; their last executed
results remain bound to their own revisions above, and round 9 re-executed the
live suite on the round-9 tree.

### Round 8 verification (fix tree over `45c0bc3`, re-parenting focus repair)

Scope: the PR-bot P1 in `.tmp/0007b-findings-codex-p1.md` — a workspace refresh
that changes the status of the board card a keyboard operator has focused
re-parents the card into another column, so the focused link is unmounted and a
new one is mounted for the same work item; focus fell to the document and Arrow
keys/Enter stopped acting on the work item while it stayed visible.

`frontend/src/features/projects/work-surface.tsx` gained `useWorkFocusRestore`:
the work item id, not the node, is the identity that survives a regroup. A
`focusin` listener records the work link that holds focus, and a
`MutationObserver` on the surface reports when that node is detached
(cross-column re-parenting, within-column reordering); the observer then
restores focus to the same work item's link unless another control (heading,
dialog, filter, saved views) holds focus in that commit. List and board links
carry `data-work-item` for that lookup. `frontend/src/app/router.test.tsx` gained
the re-parent regression test and a focus-theft guard, and
`browser/tests/portal-work-views.spec.mjs` gained three assertions inside the
existing list/board case (no new declaration). Changed-file parity before the
runs: `frontend/src` and `browser/tests` compare byte-identical between the
repository and `.tmp/0007b-tooling` (`diff -rq` exit 0), and the two built files
compare `sha256sum`-identical with `/isolated/frontend/src` in the node
container.

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `./node_modules/.bin/vitest run src/app/router.test.tsx` in `.tmp/0007b-tooling/frontend`, both new tests against the unfixed `work-surface.tsx` (`git show HEAD:…`) | 1 | **1 failed, 29 passed (30)**: `keeps keyboard focus on the board card when a refresh moves it to another column` at `toHaveFocus` on the re-mounted card in the `done` column. `.tmp/0007b-round8-red-unit.txt` |
| Same command after syncing the fixed `work-surface.tsx` | 0 | **30 passed** (round 7 ran 28 in this file). |
| Negative control: the observer's "another control holds focus" gate disabled in the tooling copy, same command | 1 | **1 failed, 29 passed**: only `does not move focus to a moved card while the operator is in the work filter` — the guard fails when the gate is removed. Fixed file re-copied afterwards (`diff -q` exit 0, then 30 passed). |
| `./node_modules/.bin/vitest run` in `.tmp/0007b-tooling/frontend` | 0 | 7 files, **77 passed** (round 7: 75). jsdom still reports its unimplemented `Window.scrollTo`. |
| `./node_modules/.bin/tsc --noEmit` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/eslint . --max-warnings 0` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/playwright test --list` in `.tmp/0007b-tooling/browser` | 0 | **94 tests in 9 files**; 47 declarations as `^\s*test\(` — unchanged (the case was strengthened, not added). |
| `./node_modules/.bin/vite build && node scripts/stage-assets.mjs` in the container `/isolated/frontend` | 0 | `index-DfxI5M9g.js` 659,147 B / gzip 199.77 kB (`sha256:a4cc373b1f79e1140603cbe8e5b2a5c5e3b572f0575a8c3db8f0a62f09ceb2f0`); `index-D7KuRjr5.css` 20,927 B **unchanged** (`sha256:c9805a9b…`, byte-identical to rounds 6–7; the emitted selector set is identical, so the fix adds no stylesheet bytes); staged into `/isolated/control/priv/static/portal/`. |
| `docker cp` of the staged pair into `symmetry-0007b-control` + `mix release --overwrite` in `/app/control` (`MIX_ENV=prod`) | 0 | Release priv holds exactly the new asset pair and `index.html`; the served `GET /portal/assets/index-DfxI5M9g.js` answers with the identical sha256 `a4cc373b…`. |
| `sh /repo-src/.tmp/0007b-restart-release.sh` in the control container | 1 (script) | Same known limitation as rounds 3–7: exits 1 at its `wget` readiness probe (`wget: not found`); host `curl` answers 4000=200 and 4001=200. |
| Focused browser run of the strengthened list/board case against the **pre-fix release** (the round-7 build `index-BuYiGA6E.js`), `.tmp/0007b-round8-before-results` | 1 | **2 failed, 0 passed** (desktop and mobile), both at `expect(movedCard).toBeFocused()` — `Received: inactive` while the same work item's card was rendered in the destination column. The real engine reproduced the finding on both widths before the rebuild. |
| Focused release-shell run on the rebuilt release (`SYMMETRY_PORTAL_URL=http://127.0.0.1:4000`, `SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4001`, `.tmp/0007b-round8-focused-results`) | 0 | **36 cases: 35 passed, 1 skipped, 0 failed** (3.1 min); `.last-run.json` is `{"status":"passed","failedTests":[]}`. The one skip is the unchanged mobile concurrency case. All three new assertions pass on both widths; the measured snapshot is **186,476 bytes / 15 work items** (desktop 181,221; mobile 186,476), recorded in the measurements table. |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` (`-Scope source`, then `-Scope full`) in the repository working tree | 0 | Source subject `sha256:d0ac0a48…` (393 entries, `docs/**` excluded); full-tree `sha256:989330ee…` (396 entries). |

Class audit of the re-parenting surfaces, with what was actually checked:

- **Board regroup across columns (the finding)**: fixed; red on the unfixed
  source (unit) and on the previous release (desktop + mobile), green after.
- **Within-column reordering and list re-sorting**: the list renders the served
  array order and the board renders served order inside each column, so a
  refresh that changes `position` makes React remove and re-insert the keyed
  node. The restore keys on the removed node itself, so the same rule covers
  that move; jsdom never loses focus on a remove/re-insert (it resets focus only
  at rendering updates), so this variant has no red-before unit proof — it is
  covered by the shared rule and asserted indirectly by the guard, and stays a
  stated verification limit.
- **List↔board switches**: the view links live in the toolbar that survives the
  switch, and the shell's heading reset keys on `state.location.pathname`
  (`shell.tsx`), so the same-pathname search update keeps focus on the activated
  link — asserted in the real engine on both widths.
- **Filter and search updates**: they change the filtered subset without
  re-sorting; the focused controls (filter input, state/owner selects, blocked
  checkbox, saved-views select) stay mounted, and the two controls that remove
  themselves keep focus through the round-6/round-7 repairs, whose tests remain
  green. The new guard proves a refresh that moves a card cannot pull focus out
  of the filter the operator is typing in.
- **Saved-view changes**: applying a saved view is the same-pathname search
  update on a persistent select; deleting one is covered by the round-7
  assertion.
- **Work-detail navigation**: the background surface stays mounted, so the
  clicked card is never detached and the route-change heading focus is
  preserved (the existing router case is still green).
- **A work item that leaves the view** (filtered out or no longer served): no
  card exists to focus, so nothing is restored and the record is cleared —
  deliberate, not a regression.

Deliberate trade, stated as a limit: if the operator's focus already fell to the
document itself (no control focused) and a refresh then detaches the last work
link that had focus, focus returns to that work item. The alternative — treating
every document-level focus state as "deliberate" — would miss the reported case.

Not re-run in round 8: the broad browser suite, the live-daemon suite, the
Compose/restart specs, the Elixir, Go and contract gates, and the frozen
dependency installation. The live suite asserts the work surface and detail this
round changed, so the round-8 tree carries no live-suite evidence; their last
executed results remain bound to their own revisions above, and round 9
re-executed the live suite on the round-9 tree.

### Round 9 verification (branch HEAD `747b19b4`, review repair)

Scope: the round-9 findings in `.tmp/0007b-findings-deepseek.md` — STD-A20 /
SPEC-A08 (a consumed wire member outside the generated contract, and a false
"no hand-written parallel wire DTO" claim), STD-A22 (a detail refresh silently
discarded the expanded older history), SPEC-A06 (the populated-history clause
had no evidence on the reviewed revision because the receipt's "none of their
surfaces changed" justification was false), and the five P3s STD-A23–A27.

Changes:

- `contracts/v1/workspace-read.schema.json` now documents the consumed
  `WorkspaceProject.resources` member the CI chip reads (`WorkspaceResource`:
  optional `id`, `status`, `sync_status`; every other served member additive,
  and every consumed member optional, following this file's rule that a served
  read keeps accepting older partial projections). The member is a member
  `/portal/api/workspace` already serves (`PortalJSON.resource/1`); the server,
  the gates, the permissions and the pagination rules did not change, and the
  fixture corpus is unchanged at 219. `contracts/generated/ts/workspace-read.ts`,
  `contracts/generated/effect/index.ts`,
  `daemon/internal/contracts/generated.go` and
  `daemon/internal/contracts/schema_bundle.json` were regenerated by
  `contracts:generate` and are drift-clean.
- `frontend/src/features/work/presentation.ts` derives `CiResourceLike` from the
  generated `SymmetryWorkspaceReadV1` DTO instead of declaring the wire members
  by hand.
- `frontend/src/features/work/history.tsx` keeps the older pages the reader
  expanded: each page fetched by the reader's own action (`Load older history`,
  `Retry history`) is merged into a retained list, so a detail refresh that
  re-issues the served cursor no longer collapses the history to the newest
  page. The continuation still starts from the refreshed boundary (the existing
  "superseded detail boundary" case stays green), duplicates keep the newest
  served copy, and an entry re-served by the refreshed first page is not
  rendered twice.
- `frontend/src/features/projects/work-surface.tsx`: `Clear filters` keeps the
  selected `view` and clears only `q`/`state`/`owner`/`blocked`.
- `frontend/src/features/projects/filters.ts`: JSON-shaped query text in a
  foreign URL (`?q=null`, `?q={"a":1}`) renders as the literal it encodes
  instead of being dropped while the URL keeps claiming the filter.
- `frontend/src/features/work/detail.tsx`: the detail CI chip's title states the
  limit that the bound resources are served by the project read, not the detail
  read (the findings allowed threading the resource map or stating the
  limitation in the label/title; this states it, so the detail surface does not
  fetch a project snapshot only for a chip).
- `browser/tests/portal-work-views.spec.mjs`: the CI chip's rendered class is
  asserted on the real stale-bound-resource and manual rows.
- Receipt corrections: the F1 row's wire-DTO claim, round 6's class-audit grep
  parenthetical (now "the only consumer of `state.location.href`"), and the
  false "none of their surfaces changed" justifications in rounds 4/6/7/8.

Disposition of every round-9 finding:

| ID | Surface | Change or why the finding is invalid | Evidence |
| --- | --- | --- | --- |
| STD-A20 / SPEC-A08 | `contracts/v1/workspace-read.schema.json`, generated TS/Effect/Go artifacts, `frontend/src/features/work/presentation.ts` | Fixed: the consumed `resources[]` member is inside the descriptive read contract, the artifacts are regenerated and drift-clean, and `CiResourceLike` is derived from the generated DTO. The false "no hand-written parallel wire DTO" claim in the F1 row is corrected in place. | `check.mjs` (drift + 219 fixtures), `check-effect.mjs`, Go contract packages and the Elixir manifest gate, all exit 0 on this revision. |
| STD-A22 | `frontend/src/features/work/history.tsx`, `router.test.tsx` | Fixed: the expanded older pages survive a detail refresh; the continuation re-anchors at the refreshed boundary and duplicates keep the newest served copy. | Red before (1 failed: `older entry` gone after the refresh), green after (81 unit tests). |
| SPEC-A06 | `browser/scripts/run-live-daemon-tests.ps1` evidence, receipt | Fixed by re-executing the live suite on this revision, and the false "none of their surfaces changed" justifications in rounds 4/6/7/8 are corrected. | Live run exit 0: 3 passed, 1 intentionally skipped; both widths 100 → 121; `.tmp/0007b-round9-live-results`. |
| STD-A23 | receipt, round-6 class audit | Fixed by rewording: "the only consumer of `state.location.href`", with the other `location.href`/`useRouterState` occurrences (`application.ts`, router tests, `shell.tsx`) named and distinguished. | Source grep over `frontend/src`. |
| STD-A24 | `frontend/src/features/projects/filters.ts`, `filters.test.ts` | Fixed: a JSON-shaped `q` renders as the literal text it encodes; the typed path already round-trips and stays a plain string. | New unit case red before (`{}` for `{q: null}`), green after. |
| STD-A25 | `frontend/src/features/projects/work-surface.tsx`, `router.test.tsx` | Fixed: `Clear filters` clears only `q`/`state`/`owner`/`blocked` and keeps `view`. | New router case red before (list rendered after clearing a board's filters), green after. |
| STD-A26 | `browser/tests/portal-work-views.spec.mjs`, `router.test.tsx` | Fixed: the rendered class is asserted on real rows (stale bound resource → `chip-stale`, manual/unknown → `chip-unknown`, and not `chip-passed`), plus a unit case that pins `chip-passed` for a confirmed fresh provider row. | Focused release-shell run (both widths) + unit case. |
| STD-A27 | `frontend/src/features/work/detail.tsx` | Fixed by the finding's second option: the chip's title states that the bound resources are served by the project read, not the detail read, instead of fetching the project snapshot for a chip. The list/detail labels still differ by that stated limit. | Detail chip title; no new request in the detail path. |

Red-before evidence for every behavioural change:

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `./node_modules/.bin/vitest run src/app/router.test.tsx src/features/projects/filters.test.ts` in `.tmp/0007b-tooling/frontend`, the four new cases against the unfixed sources | 1 | **3 failed, 37 passed (40)** — exactly the three behavioural cases: `workSearch({q: null})` returned `{}`; after `Clear filters` the board was gone and the list rendered; after the detail refresh the expanded `older entry` was gone. The CI-chip-class case passed on the unfixed tree (it guards existing behaviour). |
| `git show HEAD:frontend/src/features/projects/work-surface.tsx` restored in the tooling copy, the clear-filters case alone | 1 | **1 failed** at `findByRole("region", { name: "Work board" })` after the click — the view was dropped with the filters. |
| `git show HEAD:frontend/src/features/work/history.tsx` restored in the tooling copy, the history case alone | 1 | **1 failed** at `Unable to find an element with the text: older entry` — the refreshed boundary collapsed the expanded page. |

| Exact command / working directory | Exit | Observed result |
| --- | ---: | --- |
| `./node_modules/.bin/vitest run` in `.tmp/0007b-tooling/frontend`, fixed tree | 0 | 7 files, **81 passed** (round 8: 77; the four new cases). jsdom still reports its unimplemented `Window.scrollTo`. |
| `./node_modules/.bin/tsc --noEmit` in `.tmp/0007b-tooling/frontend` | 0 | Clean. |
| `./node_modules/.bin/eslint . --max-warnings 0` in `.tmp/0007b-tooling/frontend` | 0 | Clean (the first version of the history merge used `setState` in an effect and failed this gate with one error; the final version merges inside the reader's own fetch continuation instead). |
| `node contracts/scripts/check.mjs` in `.tmp/0007b-tooling` | 0 | `Generated contract DTOs are up to date.`; `Validated 17 Draft 7 schemas and 219 fixtures.` |
| `node contracts/scripts/check-effect.mjs` in `.tmp/0007b-tooling` | 0 | `Effect decoded all 219 raw fixtures; 215 parsed documents reached Schema validation`. |
| `./node_modules/.bin/tsc --project contracts/tsconfig.json` in `.tmp/0007b-tooling` | 0 | Contract typecheck clean. |
| `node --test contracts/scripts/*.test.mjs` in `.tmp/0007b-tooling` | 0 | 32 tests, 32 pass, 0 fail. |
| `go test -count=1 ./internal/contracts ./internal/protocol` in `daemon/` (host, repo tree) | 0 | Both packages `ok` (3.025 s / 3.543 s) on the 219-fixture corpus. |
| `docker cp` of `contracts/v1/workspace-read.schema.json` and `daemon/internal/contracts/schema_bundle.json` into `symmetry-0007b-control` (sha256 equality verified on both sides: `8c5a2b15…` / `a68ad09d…`), then `docker exec -e MIX_ENV=test -e POSTGRES_DB=symmetry_control_test symmetry-0007b-control sh -lc 'cd /app/control && mix test test/symmetry_control/goals/contract_fixture_manifest_test.exs'` | 0 | **1 passed** (all 219 manifest fixtures). |
| `./node_modules/.bin/vite build && node scripts/stage-assets.mjs` in the container `/isolated/frontend` | 0 | `index-Q36qYlaT.js` 660,108 B / gzip 200.02 kB (`sha256:9f1e164a593ae5c0a9e43d754940301d2d6b6bb7c3d77db8259a8bdfd99e2e26`); `index-Eweh798Z.css` 20,949 B (`sha256:bc50fa6a80aa7caf15877f0d4f6a11a6c4ad110090c793e6c02955466cc92810`); staged into `/isolated/control/priv/static/portal/`. |
| Rebuild after normalising the two files my earlier scripted edit had left with CRLF endings (`frontend/src/features/work/history.tsx`, `contracts/v1/workspace-read.schema.json`; content byte-identical modulo `\r`), same command | 0 | The rebuild emits the **same** asset pair (`sha256:9f1e164a…` / `sha256:bc50fa6a…`), so the release under test is exactly what this tree builds. |
| `docker cp` of the staged pair into `symmetry-0007b-control` + `mix release --overwrite` in `/app/control` (`MIX_ENV=prod`) | 0 | Release rebuilt; the release's `index.html` references `index-Q36qYlaT.js`, and the served `GET /portal/assets/index-Q36qYlaT.js` / `index-Eweh798Z.css` answer with the identical sha256 `9f1e164a…` / `bc50fa6a…`. |
| `sh /repo-src/.tmp/0007b-restart-release.sh` in the control container | 1 (script) | Same known limitation as rounds 3–8: exits 1 at its `wget` readiness probe (`wget: not found`); host `curl` answers 4000=200 and 4001=200. |
| Focused release-shell run on the rebuilt release (`SYMMETRY_PORTAL_URL=http://127.0.0.1:4000`, `SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4001`, `.tmp/0007b-round9-focused-results`) | 0 | **36 cases: 35 passed, 1 skipped, 0 failed** (2.9 min); `.last-run.json` is `{"status":"passed","failedTests":[]}`. The new chip-class assertions pass on both widths; the measured snapshot is **196,424 bytes / 15 work items** (desktop 191,169; mobile 196,424), recorded in the measurements table. |
| `./node_modules/.bin/playwright test --list` in `.tmp/0007b-tooling/browser` | 0 | **94 tests in 9 files**; 47 declarations as `^\s*test\(` — unchanged (assertions were added inside existing cases). |
| `pwsh -NoProfile -File .tmp/0007b-tooling/browser/scripts/run-live-daemon-tests.ps1` (`.tmp/0007b-round9-live-results`) | 0 | **3 passed, 1 intentionally skipped** (the mobile lifecycle case), 1.5 min; both widths executed the real-cursor history case: `firstPageEntries` 100 → `firstEntriesAfterOlderPage` 121, `currentEntriesAfterRetry` 121, three delivered fault attempts, `retryCursorBoundTo` is the consumed detail response's cursor. Logs: `C:\Users\lhan\AppData\Local\Temp\symmetry-goal-02-live-4ceae2cd4f1c424c9f4c28cb892c5406`. |
| `pwsh -NoProfile -File .tmp/0007b-subject-digest.ps1` (`-Scope source`, then `-Scope full`) in the repository working tree | 0 | Source subject `sha256:c4002c27273e3f43d6de1fa7a6444383b9e1f069ad97836252621f2069510bd5` (393 entries, `docs/**` excluded); full-tree `sha256:68692b2cbb4bdad240dde6978ba6a9c37af844c76593f55ff894a4532a63a1b0` (396 entries). |

Tooling-copy parity before the gates: `diff -rq` exit 0 for `frontend/src`,
`browser/tests`, `browser/scripts`, `contracts` (excluding `node_modules`) and
`daemon/internal/contracts`; the full `daemon/` tree was re-synced to this
revision before the live runner built the daemon binaries, because the retained
copy still held an older `internal/workspace` implementation (four files
differed). The live runner therefore built the daemon from this revision's
source. This revision's binding is the digest pair above; the round-7 and
round-8 digests named earlier are historical evidence for their own trees.

Not re-run in round 9: the broad browser suite (its last execution is the
round-3 broad run), the Compose/restart specs (skipped locally; no CI job
executes them), the Elixir full suite and the Go full tree (their changed
surfaces — the schema bundle and the two contract packages — were re-run
above), the frozen dependency installation (never available), and native
credentialed-provider qualification. Those stay bound to their own revisions
and are not claimed for this tree.

## Review disposition and remaining limitations

- Independent read-only state, schema, and acceptance reviews found real
  project-scope, modal-loading, nullable-command, legacy-result, unsafe-probe,
  cursor-binding, device-context, and negative-barrier defects. Those were
  repaired rather than exempted. The final scoped/live runs exercise their
  affected paths; schema regressions have old-bundle red and final-bundle green
  proofs. The CI spec-list addition was independently inspected.
- The final local simplification retained board grouping/order and existing
  state owners, removed only unused state-summary work/export and the cursor
  sentinel, and added no parallel cache or retry authority. Last test-only
  repairs strengthen reload, section, and rendered-order predicates rather
  than pinning serialization or selecting an arbitrary duplicate match.
- Round 9 closed three defects the bounded second-model review found on the
  reviewed revision: the consumed `resources[]` wire member left the generated
  contract, the detail refresh dropping the reader's expanded older history,
  and the live-suite justification that was simply untrue (the suite now
  executes on this revision, under H9). Two items stay deliberate trades, not
  waivers: the detail CI chip does not fetch the project snapshot to confirm a
  binding (its title states that the bound resources are served by the project
  read), and the `state`/`owner` selects still drop JSON-shaped foreign values
  while `q` renders them as the literal the URL carries.
- Two nonblocking review observations remain explicit: the pre-existing picker
  fallback may briefly label an available project unavailable while its query
  is pending; a scopeless narrow work navigation can fetch one additional
  project-scoped workspace directory after detail resolves. The shared detail
  query is deduplicated; the latter is not a second detail request.
- The four fresh-session failures are fixed and re-verified (above), and every
  Done-when predicate of `docs/goals/0007b-readable-work-views.md` has
  executed evidence on the round-3 repair tree (`sha256:193ddbb2…`): the
  list/board/detail/history/empty/error browser cases with their desktop and
  narrow captures, the fresh-session re-entry of the work and filtered-project
  deep links, the recorded keyboard walk, the truth labels, the retained
  classic coverage with its migration map, and the real-cursor history counts.
  The round-4 fix tree (`sha256:353b10fc…`) re-ran the frontend unit gates
  recorded above; the browser captures are not re-executed on it. Round 5
  re-executed the contract and frontend gates on its revision
  (`01eb134`, source subject `sha256:4019477f…`); the browser captures remain
  bound to the revisions recorded with them. Round 6 re-executed the frontend
  unit gates and the focused release-shell browser subset on the fix tree over
  `49ca695` (source subject `sha256:5d86199e…`): 73 unit tests, and 36 browser
  cases with 35 passed and 1 skipped; the broad, live and Compose suites remain
  bound to their earlier revisions. Round 7 re-executed the frontend unit,
  typecheck and lint gates and the focused release-shell subset on the
  round-7 fix tree (source subject `sha256:851b1542…`): 75 unit tests, and 36
  browser cases with 35 passed and 1 skipped, both focus assertions red on the
  previous revision first (2 failed per width, per control) and green here;
  the focused captures are re-pointed to R7. Round 8 re-executed the frontend
  unit, typecheck and lint gates and the focused release-shell subset on the
  round-8 fix tree (source subject `sha256:d0ac0a48…`): 77 unit tests, and 36
  browser cases with 35 passed and 1 skipped, with the re-parent focus
  assertion red on the previous release first (2 failed, one per width,
  `Received: inactive`) and green here; the focused captures are re-pointed to
  R8, and the broad, live and Compose suites remain bound to their earlier
  revisions. Round 9 re-executed the frontend unit, typecheck and lint gates,
  the Node contract generator/drift and Effect gates, the contract runtime
  tests, the Go contract packages, the Elixir manifest fixture gate, the
  focused release-shell subset and the live-daemon suite on this revision
  (source subject `sha256:c4002c27…`): 81 unit tests; 36 browser cases with 35
  passed and 1 skipped; and the 4-case live suite with 3 passed and 1
  intentional skip, both widths executing the real-cursor history case
  (`firstPageEntries` 100 → `firstEntriesAfterOlderPage` 121,
  `currentEntriesAfterRetry` 121), so the populated-history clause of the
  Done-when list now carries executed evidence on the revision under test; the
  focused captures are re-pointed to R9 and the live capture to H9.
- Still-unavailable evidence, recorded as limits rather than waived: the
  original frozen dependency installation; the Compose/restart browser specs
  (skipped locally, and **no CI job executes them** — the CI `compose` job runs
  Compose smoke checks and never installs Playwright or sets
  `SYMMETRY_COMPOSE*`; only the manual
  `browser/scripts/run-compose-daemon-tests.ps1` runner executes them); the
  provider stale-timestamp visual branch (unit proof only); native
  credentialed-provider qualification; and the complete CI/merge matrix for the
  eventual PR. Nothing above satisfies or waives those, and none was made to
  pass by weakening an assertion, a permission, a pagination rule or a wire
  contract.
- Round 3 records these additional explicit limits: the Elixir read-fixture
  validator keeps a test-local PCRE2 escape adaptation mirroring the production
  private copy (STD-A13); the descriptive read schemas keep their documented
  false-acceptance points (half-open command identity pair, null recorded
  timestamps) with the database check constraint and `Protocol` as the
  enforcement point (STD-A14); unit tests still cannot reach the waiting-state
  and narrow-width branches, which only the browser suites cover (STD-A18); and
  the provider timestamp-stale branch remains unit-only.
- Owner visual acceptance remains the owner's own verdict and is not supplied
  here; the run reports the result as produced and awaiting that verdict. No
  model-issued `achieve` is recorded, and rounds 3–5 edited only the working
  tree without committing (the stage's rules); the branch content is recorded
  in the branch history.
