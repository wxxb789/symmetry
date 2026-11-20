# 0007f runtime visibility and execution control — receipt

Status: **produced and awaiting the owner's verdict**. This record pairs every
Done-when clause with the evidence that demonstrated it on the revision under
test, states the exact checks executed with their observed counts and exit
codes, and lists unresolved limitations. Numbers in this file are the single
source for the run; every measurement names the revision that produced it. This
delivery is the review-fix round for PR #38 (2026-11-17): it re-ran the checks
on the union head with the review fixes applied and re-bound every number below
to that tree. The previous round's digest is kept as a historical value with its
own revision named, and is explicitly not valid for the tree this file binds.

## Revision binding

Revisions are identified by a content digest, because this branch may be
rebased before merge and a rebase rewrites SHAs. The binding is the **source
subject**: the SHA-256 over a sorted manifest of the delivered source paths with
every path under `docs/**` excluded, so editing this receipt cannot change the
value it records.

The recorded digest covers the 43 changed and untracked source paths of the
delivery (the union head's changes, this round's changes, and the two new test
files; every path outside `docs/**`), each hashed and listed as
`<sha256>  path` in a manifest sorted by the path's ordinal (byte) order, then
hashed again:

| Revision | Identity |
| --- | --- |
| Union head | `6996740` — "feat(0007f): runtime visibility and trustworthy execution control from the work detail" |
| This delivery (source) | `sha256:cafbdc5495cf6ac2de4347339b0e0e104107609213e2bc9de29d1019fc2d32c6` — the delivered commit `1d9ed56` plus the integration delta it was merged with (one added resource-form field-error case and these receipt corrections), computed over the current 44-path manifest with the procedure below |
| Historical, superseded | `sha256:a5160334d61f3d3536cbaaab827ab2924f46d19860e3d8beaf2cec213f1fc8e8` — recorded by the fix round for `1d9ed56` over a 43-path manifest, and independently reproduced by the reviewer against that manifest; the current manifest carries one more path, so the value is kept as a historical fact rather than the current binding |
| Historical, superseded | `sha256:569875d4e8c751d76c14b6a4ee34dce7a5a11cbfd069d7e921f8510df564305e` — reproduces only at the pre-rebase commit `3e353ea` over that revision's 40-path manifest; it is **not** valid for the union head, and no check in this file was run against it |
| This delivery (tree) | delivered as the commit `1d9ed56` plus this integration delta; the current source digest is computed from the tree's content over the 44-path manifest, so a receipt edit (docs are excluded from the manifest) cannot invalidate it |

Reproduction (PowerShell 7, repository root):

```powershell
# the delivered source paths: the union base (ad1c117) to the delivered tree,
# plus untracked files, minus docs/**
git diff --name-only ad1c117 | Where-Object { $_ -notlike "docs/*" } > paths.txt
git ls-files --others --exclude-standard | Where-Object { $_ -notlike "docs/*" } >> paths.txt
# hash every listed file into "<sha256>  <path>`n" lines, sort the lines by
# [string]::CompareOrdinal over the path, then SHA-256 the UTF-8 manifest bytes
# (trailing newline). The manifest was 43 lines and printed the digest above;
# the same procedure at 3e353ea over that revision's 40-path manifest reproduces
# the historical value byte-for-byte, which is how the convention is verified.
```

The ordinal sort is deliberate: it is culture-independent, so the value does
not depend on the host's collation.

## Files written

**Frontend (this round)**
- `frontend/src/services/api.ts` — the four control mutations now run through
  the shared `request()` boundary: a control-plane refusal arrives as the
  server's typed `ServerRefusal` (code/message/fields), an undescribed 403
  keeps the session-protection message, and the caller supplies the session's
  live CSRF token; the old `mutate` helper and its meta-tag token reader are
  gone.
- `frontend/src/app/application.ts` — the four control mutation options carry
  `csrf` in their variables; `createApplication` no longer takes a
  token-reading option.
- `frontend/src/features/work/control.tsx` — controls send
  `useSessionToken()`; a definitive refusal (including a 4xx `ServerRefusal`)
  renders as `refused`, and a lost response or unproven fault stays `unknown`
  instead of `failed`; the control surface owns the reader's expanded timeline
  pages and shares them with the receipt list, so a refresh cannot drop them.
- `frontend/src/features/work/history.tsx` — reads and retains those shared
  pages instead of keeping a private copy.
- `frontend/src/features/work/receipts.ts` — new `refused` outcome
  ("Refused by the server", distinct from the runtime's "Rejected by the
  runtime") and a pre-runtime headline ("Awaiting a runtime") for a recorded
  run no runtime has claimed, distinct from `requested` and from a runtime
  acknowledgement.
- `frontend/src/features/runtimes/{presentation.ts,runtimes.tsx}` — the empty
  state names the compatibility filter and cites the served registered count
  ("No compatible runtimes" / "No project selected"), so the page never claims
  an empty registry from a filtered read.
- `frontend/src/features/work/detail.tsx` — passes the served cursor into the
  control surface.
- Tests: `frontend/src/features/work/control.test.tsx` (new),
  `frontend/src/features/runtimes/runtimes.test.tsx` (new), and additions to
  `services/api.test.ts`, `features/work/receipts.test.ts`,
  `features/runtimes/presentation.test.ts`.

**Control (Elixir)**
- `control/lib/symmetry_control_web/portal_session.ex` — `/portal/runtimes`
  accepts `projectId=…` as a return target (reusing the work-surface project
  parameter), so a session expiry no longer collapses a project-scoped runtimes
  page to `/portal`.
- `control/test/symmetry_control_web/portal_session_test.exs` — the accept list
  gains `/portal/runtimes?projectId=project-123`; the reject list gains
  `/portal/runtimes?projectId=//example.com`,
  `/portal/runtimes?projectId=project-123%0d%0alocation%3ahttps://example.com`
  and `/portal/runtimes?view=board`.

**Browser acceptance**
- `browser/tests/portal-goal-managed.spec.mjs` — rewritten onto a real
  Goal-managed row: the case creates the project, repository resource and two
  work items through the authenticated API, writes the Goal membership with
  `scripts/ci/portal-planning-fixture.exs` (Control's own changesets and
  database constraints), then asserts the served `goal_id`, no run/cancel/retry
  control, the four 403 `goal_authority_required` refusals of the legacy paths,
  and a sibling legacy item that keeps its start control. The fixture needs
  `POSTGRES_*` naming the database the release serves; without them the case
  skips with its missing environment named instead of substituting a captured
  response.
- `browser/tests/portal-execution-control.spec.mjs` — the queued-run receipt
  assertion uses the new pre-runtime headline; the retired live-timer check is
  replaced by the served control-bar state chip; a new Runtime visibility case
  covers the scoped and unscoped `/portal/runtimes` empty states against the
  served read.
- `browser/tests/portal-live-daemon.spec.mjs` — restores the corrected-intent
  copy assertion (the retry re-admits the task from the current work-item text)
  and records the retired classic assertions with their successors.
- `browser/README.md` — the retired/coverage paragraph corrected (retired live
  timer and Activity-row assertion; the live-daemon losses listed with
  successors), the focused-gate command gains the fixture environment, and the
  Goal-managed description now names the real fixture.

**Docs**
- `docs/TODO.md` — deferred review findings: the CI render job's missing
  fixture environment, the pre-existing `/portal/connections?projectId=…`
  return-target gap, and the pre-existing capability-vs-state cancel/retry
  policy.

## What this round changed, by review finding

| Finding | Change |
| --- | --- |
| P2-1 control mutations discarded the typed refusal | All four run through `request()`; a 4xx `ServerRefusal` renders as "Refused by the server" with the server's message, never "Rejected by the runtime" |
| P2-2 controls sent the static document token | Controls take the session's live token from `useSessionToken()`; the rotated-session case is covered by unit tests |
| P2-3 Goal-managed clause rested on a replay | The case asserts on a real Goal-managed row written by the in-tree fixture, plus the server's own 403 refusals |
| P2-4 receipt bound a non-head revision | Every check below re-ran on the union head with these fixes; the pre-rebase digest is historical and the union's own digest is the single current number |
| P3-1 live-daemon suite asserted less | The corrected-intent assertion is restored; the forged-reference walk and the obsolete-response history phases are explicitly retired with named successors, and the README sentence is corrected |
| P3-2 "live timer" replacement was not real | The migrated check asserts the served control-bar state chip; the live-timer and Activity-row assertions are recorded retired |
| P3-3 pairing row outran its evidence | Reworded below; the durable guidance/pause/resume rendering on `/portal/work/:id` stays an explicit limitation |
| P3-4 receipts dropped older pages on refresh | The control surface owns the retained pages and both mount points render them |
| P3-5 queued run rendered an acknowledgement | A pre-runtime run has its own "Awaiting a runtime" headline |
| P3-6 runtimes empty state from a filtered read | The empty state names the filter and cites the served registered count; both URL scopes are covered |
| P3-7 scoped runtimes return target rejected | `portal_session.ex` accepts `projectId=…` for `/portal/runtimes`, with accept/reject tests |
| P3-8 capability-vs-state cancel/retry | Named limitation below and in `docs/TODO.md`; out of this diff's server-policy scope |
| P3-9 lost response rendered "Failed" | A lost response or unproven fault maps to the unknown outcome |

## Done-when pairing

| Contract clause | Evidence |
| --- | --- |
| live-daemon and browser tests for assignment and waiting-for-input flows | `portal-live-daemon.spec.mjs` "waiting input and failed retry complete on the readable work detail" — real daemon, real fake-agent `wait_input` run; asserted the served `waiting_for_input` state, the form, the `provide_input` command with `acknowledgement_outcome: applied`, the `completed` transition, the rendered state text, and the corrected intent copied into generation 2. Both widths. |
| cancellation/completion race, failure-repair retry, stale command, unsupported capability | Same suite: the race case (cancel against a fast-completing run, rendered state asserted equal to the served state), the retry case (generation 1 failed → corrected intent → generation 2 completed with delivery evidence, both generations distinguishable), and the unsupported-capability lines on the runtime page. The stale-generation/stale-transition refusals are answered 409 by the real server and asserted at the API level in `portal-execution-control.spec.mjs`; their typed consumption and rendering are asserted in `api.test.ts` and `control.test.tsx`. The live suite does not itself render a stale-generation refusal. |
| receipts across generations stay distinguishable | `receipts.ts` derivation plus the rendered history: applied requires `acknowledgement_outcome: applied`; a control-plane-applied command with no runtime acknowledgement renders `Queued for the runtime`; a command awaiting an acknowledgement renders `Awaiting agent acknowledgement`; a pre-runtime run renders `Awaiting a runtime`; a control-plane refusal renders `Refused by the server`; an unconfirmed attempt renders `Outcome unknown`. Asserted in `receipts.test.ts`. The live runs render start/cancel/retry/waiting-input receipts from real records; the guidance/pause/resume receipts exercised here are the synthesised command projections of `receipts.test.ts:92-103`, because the Chat live suite dispatches real guidance/pause/resume commands but never opens `/portal/work/:id` — that rendering stays a stated limitation, not a live observation. *(Amended 2026-11-20, PR #111: a `cancel` the control plane applied with no runtime acknowledgement renders `Applied by the control plane`; a `pause`/`resume`/`provide_input` the plane recorded applied from the runtime's own transition report without an acknowledgement renders `Applied by the runtime; acknowledgement not recorded`; only the re-queued `retry` renders `Queued for the runtime`.)* |
| history pagination, refresh, disconnect, reconnect | `portal-live-daemon.spec.mjs` real-cursor case (Load older history against real daemon records, both widths, with capture and observation JSON), `portal-work-views.spec.mjs` "a disconnected read recovers and reconciles with the server's own snapshot", and the receipt list's retained older pages asserted in `control.test.tsx`. |
| runtime/capability rows match the advertised record | `portal-live-daemon.spec.mjs` "runtime capability rows match the daemon's own registration record": the rendered card's `data-adapter`/`data-capabilities` bytes, per-operation rows and value text compared with the daemon's own served record, including the explicit `pause: unsupported` and `guidance: unsupported` entries; the page's empty states assert against the served read in `portal-execution-control.spec.mjs` and `runtimes.test.tsx`. |
| a Goal-managed work item presents no legacy path | `portal-goal-managed.spec.mjs` on a real Goal-managed row (written by `scripts/ci/portal-planning-fixture.exs`): the served `goal_id`, no start/cancel/retry control, the Goal-managed section naming the Goal, the four 403 `goal_authority_required` refusals, and the sibling legacy item keeping its control; plus the Elixir cases in `portal_workflow_test.exs`. |
| pre-existing coverage migrates, classic checks retire | The classic work-detail control walk is retired with its assertions migrated or recorded retired (live timer and Activity-row assertion retired, not migrated); the classic drawer draft/focus case's migratable half moved; the retired live-daemon assertions and their successors are listed below and in `browser/README.md`. |

## Retired / changed acceptance coverage

Removed from `browser/tests/portal.spec.mjs` (classic `/portal/classic`):
- "keyboard navigation, dialogs, and drawers remain usable without overflow" —
  the work-detail control walk: `Start run` → queued state → `Cancel run` →
  `Cancelled` → `Retry` visible → reassigned to a human → `Retry` count 0. The
  classic walk's live-timer assertion and its Activity-row `Execution stopped`
  assertion are **retired, not migrated**: the new surface renders the served
  execution state and no client-side elapsed time, and the migrated check
  asserts the served control-bar state chip instead
  (`portal-execution-control.spec.mjs`). The dialog, keyboard and overflow
  assertions of that case stay; the reassignment edit path stays classic-owned
  (goal 0007c).
- "background refresh preserves drawer input, focus, and expanded raw history" —
  the migratable half (waiting-input draft, caret and focus surviving a
  background refresh) moved to `portal-execution-control.spec.mjs`; the classic
  raw-execution expander has no equivalent on the new surface and is recorded
  as retired, while history pagination is covered by
  `portal-work-views.spec.mjs` and the live daemon cursor case.

Removed from `browser/tests/portal-live-daemon.spec.mjs` by its 0007f rewrite,
each with its successor named (also in `browser/README.md`):
- the classic forged-runtime-reference refusal walk — server enforcement stays
  in `portal_workflow_test.exs` "resource APIs expose and enforce registered
  runtime identities" (422 `validation_failed` with the `external_ref` field
  error), and the new resource form's field-error rendering is covered by
  `connections.test.tsx`;
- the obsolete-response phases of the classic cursored history case (the
  in-flight failing older-history page that must not mutate a newer detail, and
  the `Retry history` cursor-reissue phase) — the obsolete older response has
  the daemon-free synthetic guard in `portal-work-views.spec.mjs` "an obsolete
  older-history failure never mutates a newer real detail"; the real cursor
  load and exhaustion stay in the live suite. The failing-page/`Retry history`
  phases are retired with that guard as the only successor.

## Checks executed

Recorded as executed on this host, with the exit code and count each command
printed. Revision: the delivered commit `1d9ed56`. Postgres for the Elixir runs is the local container on port 55442
(`symmetry_control_dev` for the browser gates, `symmetry_control_test` for the
ExUnit runs); the Control dev server for browser work is the working tree on
port 4010.

| # | Command | Observed |
| --- | --- | --- |
| 1 | `pnpm contracts:check` (root) | exit 0 — *Generated contract DTOs are up to date. Validated 18 Draft 7 schemas and 226 fixtures. Effect decoded all 226 raw fixtures; 222 parsed documents reached Schema validation.* |
| 2 | `pnpm --filter @symmetry/frontend typecheck` | exit 0, no diagnostics |
| 3 | `pnpm --filter @symmetry/frontend lint` | exit 0 (`eslint . --max-warnings 0`) |
| 4 | `pnpm --filter @symmetry/frontend test` | `14 passed (14)` files, `220 passed (220)` tests, exit 0 |
| 5 | `pnpm --filter @symmetry/frontend build` | exit 0 (Vite build plus `stage-assets.mjs` into `control/priv/static/portal`) |
| 6 | `go vet ./internal/contracts/...` | exit 0 |
| 7 | `go test ./internal/contracts/...` | `ok github.com/wxxb789/symmetry/daemon/internal/contracts 2.756s`, exit 0 |
| 8 | `mix test portal_session_test.exs portal_controller_test.exs portal_workflow_test.exs` | `Result: 33 passed`, exit 0 |
| 8b | `mix compile --warnings-as-errors` | exit 0 |
| 9 | Focused release-shell gate, both widths (see below) | `47 passed`, `3 skipped`, exit 0, 5.7m |
| 10 | `run-live-daemon-tests.ps1` (real daemon, see below) | `7 ok` cases, `4 skipped` across both phases, exit 0, 165s |
| 11 | `mix test` (whole Control suite) | `862/863 passed, 21 skipped`, `Failed: 1 test`, exit 2 (564s) — the one failure is `integrations/command_test.exs` "provider token commands can keep stderr diagnostics out of stdout", an unmodified file, failing on this host because `pwsh` resolves only to the WindowsApps app-execution alias, which the Elixir port cannot spawn (`{:error, {:command_unavailable, "pwsh"}}`): the same environment-limited failure every previous round recorded. Not a pass, not attributed to this change. |

### Focused release-shell browser gate (desktop-chromium + mobile-chromium)

Command (with the fixture environment the Goal-managed case needs):

```sh
SYMMETRY_PORTAL_URL=http://127.0.0.1:4010 \
POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55442 POSTGRES_USER=postgres \
POSTGRES_PASSWORD=postgres POSTGRES_DB=symmetry_control_dev \
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test \
  tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs \
  tests/portal-execution-control.spec.mjs tests/portal-goal-managed.spec.mjs
```

`tests/workspace-shell.spec.mjs`, `portal-work-views.spec.mjs`,
`portal-execution-control.spec.mjs`, `portal-goal-managed.spec.mjs`:
`47 passed`, `3 skipped`, exit 0, 5.7m. The three skips are the two true-TTL
expiry cases (no expiry instance at `SYMMETRY_SHELL_EXPIRY_URL` for this run)
and the mobile-width concurrency case (desktop-only by design) — the same skips
the previous round recorded, not hidden failures. The Goal-managed case
executed against the real fixture database in this run, at both widths, and the
new Runtime visibility case ran at both widths.

### Live daemon acceptance (real daemon, real fake-agent harness)

`pwsh -NoProfile -File browser/scripts/run-live-daemon-tests.ps1 -ControlUrl
http://127.0.0.1:4010` against the same revision: exit 0, 165s. Phase 1
(harness provisioning, desktop) `1 passed`. Phase 2 `6 passed`, `4 skipped`,
2.2m. Per case, read from the reporter's own lines:

| Case | desktop | mobile |
| --- | --- | --- |
| provisions the repository resource the real-harness runtime will serve | ok (phase 1) | skipped by design (single provisioning run) |
| waiting input and failed retry complete on the readable work detail | ok | ok |
| a cancel racing a fast completion stays consistent and is never reported as applied when unconfirmed | ok | skipped by design (race runs at desktop width) |
| runtime capability rows match the daemon's own registration record | ok | skipped by design (single registration run) |
| raw execution history loads older daemon records on demand | ok | ok |

### Red before / green after

- **Frontend behaviour (P2-1, P2-2, P3-4, P3-5, P3-6, P3-9)**: the new and
  modified tests were executed against the unmodified sources first —
  `5 failed (5)` files, `22 failed | 45 passed (67)` tests, exit 1
  (`.tmp/red-frontend.log`). After the fixes: the same five files
  `67 passed (67)`, and the whole frontend suite `220 passed (220)`, exit 0.
  The red failures were the assertions under test: the control refusal did not
  render as a server refusal (the old path attributed it to the runtime), the
  control POST carried no session token, the queued run rendered "Awaiting
  agent acknowledgement", the lost response rendered "Failed", the refreshed
  receipt list dropped the older page's row, and the runtimes empty state
  claimed an empty registry.
- **Control (P3-7)**: with the pre-change `portal_session.ex` restored, the
  extended test file ran `Result: 7/8 passed`, exit 2 — the failing case was
  the new accept path `/portal/runtimes?projectId=project-123` (the added
  reject cases already passed, because an unknown query was rejected by
  falling through). After the change the trio is `33 passed`, exit 0.
- **P2-3 (evidence surface, not product behaviour)**: the previous case could
  pass with no Goal-managed row in the database (it folded `goal_id` into a
  captured response). The new case's green run includes the fixture's own
  output, the served `goal_id` read back from the API, and the server's
  403 `goal_authority_required` refusals — none of which the replay could
  produce.
- **P3-1**: the restored corrected-intent assertion passed in the live run
  above; the retired phases are listed with successors instead of being
  silently dropped.
- **P3-2**: the replaced check asserts the control bar's served state chip and
  passed in the gate; the retired timer assertion is recorded above.

## Limitations

- Owner visual and interaction acceptance is **not** supplied here; the model
  records evidence, not the verdict.
- **Named limitation (pre-existing, P3-8)**: `can_cancel` and `can_retry` in
  `control/lib/symmetry_control_web/portal_json.ex` are derived from task state
  only, while adapters advertise per-operation support. A runtime advertising
  `cancel: false` therefore still renders a live Cancel control until the
  server refuses the command. Fixing it is server-policy work outside this
  diff; it is recorded in `docs/TODO.md`.
- **Named limitation (pre-existing, the P3-7 sibling)**: the shell writes
  `/portal/connections?projectId=…` but `portal_session.ex` accepts only
  `connectionId=…` for that path, so a session expiry collapses a
  project-scoped connections page to `/portal`. The identical runtimes gap is
  fixed here; the connections gap is recorded in `docs/TODO.md`.
- **CI render job**: the focused gate's Goal-managed case needs the fixture
  environment (`POSTGRES_*` plus the Elixir toolchain); hosted CI's render job
  has neither, so the case would report an environment-named skip there until
  the job gains them (`docs/TODO.md`). Hosted CI for this revision has not run.
- `Chat`-dispatched guidance/pause/resume commands are dispatched by the Chat
  live suite but never rendered on `/portal/work/:id` in a live run; their
  durable receipt rendering is asserted from synthesised command projections
  (`receipts.test.ts`). Provider connections, resource binding/sync and
  Goal-specific operations remain outside this contract.
- The live suite's race and capability cases run at desktop width only, and
  provisioning runs once; the waiting-input/retry and history cases run at both
  widths. Mobile-width skips are by design, not conditionals hiding failures.
- The focused gate's two expiry cases skip unless an expiry instance serves
  `SYMMETRY_SHELL_EXPIRY_URL` with
  `SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2`; this run did not provide one.
- The whole Control suite has one environment-caused failure on this host
  (`pwsh` app-execution alias, unmodified `integrations/command_test.exs`,
  `862/863 passed, 21 skipped`, exit 2). It is not a pass and not attributed to
  this change; the focused trio above (check 8) is green. Hosted CI for this
  revision has not run.
