# 0007g scoped engineering chat — receipt

Status: **produced and awaiting the owner's verdict**. This record pairs every
Done-when clause of `docs/goals/0007g-scoped-engineering-chat.md` with the
evidence that demonstrated it on the revision under test, states the exact
checks executed with their observed counts and exit codes, and lists unresolved
limitations. Numbers in this file are the single source for the run; every
measurement names the revision that produced it. The owner's review of the
rendered surface (the goal's own "Owner acceptance" paragraph) is **not**
supplied here: a model cannot rate its own rendering or sign for the owner.

## Revision binding

Revisions are identified by a content digest, because this branch may be
rebased before merge and a rebase rewrites SHAs. The binding is the **source
subject**: the SHA-256 over a sorted manifest of the delivered source paths with
every path under `docs/**` excluded, so editing this receipt cannot change the
value it records.

| Revision | Identity |
| --- | --- |
| Base | `dd2e9db0829d105ac58d1713d01092811cda86ff` — "feat(0007f): runtime visibility and trustworthy execution control (#38)" |
| This delivery, before the repair round (source) | `sha256:bb3d48212069eb571a76e68f7dbbd6c2bc7d29edbaecf050d1cdc84ba307d6af` over the delivered 43-path manifest (procedure below); rows 1–18 were produced on it unless a row says otherwise |
| This delivery, repaired (source) | `sha256:d52d250677296c9b914806a381920fc6a2dd31af60bb688995539e377737031a` over the repaired 45-path manifest (the pre-repair 43 paths plus `control/lib/symmetry_control/orchestration.ex` and `control/lib/symmetry_control/workspaces.ex`); every number recorded by the repair round below was produced on it |
| Rebase target | `706174ecda9504c6c1d991ddd4f862ff3bf47f0c` — "test(omp): remove the wakeup race from the sink-failure test (#45)"; the main-line revision the branch was rebased onto (main after #37, #39, #40, #42, #43, #44 and #45) |
| This delivery, rebased onto the current main (source) | `sha256:2a10b33e0ea248e178d2f000c012bb6c505123d4409e43560377a3ce122e1e58` over the same 45-path manifest: the path set is unchanged and 20 of the 45 paths differ in content from the repaired revision (`.github/workflows/ci.yml`, `browser/README.md`, `contracts/fixtures/manifest.json`, `contracts/generated/effect/index.ts`, `contracts/generated/ts/index.ts`, `contracts/scripts/check-effect.mjs`, `contracts/scripts/check.mjs`, `contracts/scripts/generate.mjs`, `contracts/ts/index.ts`, `control/lib/symmetry_control_web/portal_session.ex`, `control/lib/symmetry_control_web/router.ex`, `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs`, `control/test/symmetry_control_web/portal_session_test.exs`, `daemon/internal/contracts/generated.go`, `daemon/internal/contracts/schema_bundle.json`, `frontend/src/app/application.ts`, `frontend/src/app/router.test.tsx`, `frontend/src/app/router.tsx`, `frontend/src/services/api.ts`, `frontend/src/styles.css`); the rebase section below names every resolution and every row re-measured on it |
| Deliverable shape | one independently mergeable PR on top of the rebased base (`706174e`); the branch was rebased in place, so its local SHAs were rewritten (nothing pushed, no remote or other worktree touched), tree left for verification |

Reproduction (PowerShell 7, repository root):

```powershell
# the delivered source paths: this branch's own base to the working tree, plus
# untracked files, minus docs/**. The base is the branch's parent: `706174e`
# after the rebase, `dd2e9db` before it — the repaired-revision and rebased
# values in the table above were each taken with their own base.
git diff --name-only 706174e | Where-Object { $_ -notlike "docs/*" } > paths.txt
git ls-files --others --exclude-standard | Where-Object { $_ -notlike "docs/*" } >> paths.txt
# hash every listed file into "<sha256>  <path>`n" lines, sort the lines by
# [string]::CompareOrdinal over the path, then SHA-256 the UTF-8 manifest bytes
# (trailing newline).
```

The ordinal sort is deliberate: it is culture-independent, so the value does not
depend on the host's collation.

## What the increment implements

1. **The scoped conversation is the new shell surface.** `/portal/chat` is an
   authenticated shell document (`control/lib/symmetry_control_web/router.ex`),
   an allowed login return target with exactly its own search state
   (`portal_session.ex`: `scope=workspace|project|run`, `projectId=…`,
   `runId=…` and nothing else), a React route with the shell's project scope,
   navigation entry and command-palette destination, and a rendered surface in
   `frontend/src/features/chat/` that consumes the *existing* Chat API
   (`GET /portal/api/chat`, `POST /portal/api/chat/messages`) with no new route,
   permission or envelope.
2. **Per-scope drafts, selection and scroll.** One key per scope
   (`workspace`, `project:<id>`, `run:<id>`); the conversation query is keyed by
   the server-resolved scope, and background refetches cannot write into another
   scope's local state.
3. **Replay, late read/write and uncertain retry.** Mutations never retry
   implicitly; a lost response keeps the frozen payload (same `action_id`,
   byte-equivalent body) and the retry is answered by the server's `200` replay;
   a changed payload on the same identity is the server's `409
   idempotency_conflict`; a late read or a late write for one scope never lands
   in another.
4. **History, reconnect and durable receipts.** The served cursor chain, the
   retained older pages and the explicit unloaded-boundary note; a failed read
   recovers into the server's snapshot; every receipt is derived from the served
   command (`applied` only with `acknowledgement_outcome: "applied"`, a retained
   page the refreshed page does not carry renders "not refreshed", never
   applied).
5. **The design's outstanding Chat requirement** (`docs/design/harness.md`,
   "Existing chat compatibility"): the blanket `supervisory_control` requirement
   on the Chat start path is replaced **for native admissions only** by
   operation-specific requirements — `%{"adapter" => %{"operations" =>
   %{"start" => true, "events" => true, "cancel" => true, "pause" =>
   "unsupported"}}}` — while the legacy generic path keeps `%{"supervisory_control"
   => true}`. The split is derived from Control's own runtime registry for the
   work item's execution binding: native only when every registered runtime for
   that binding runs a native harness kind, which the derivation asks
   `Orchestration.native_harness_kind?/1` for rather than restating
   (`codex`, `claude_code`, `pi`, `opencode`, `omp`, `omo` — one list, also read
   by native Goal admission). An empty registry and any runtime without harness
   metadata keep today's behaviour exactly; `omp`/`omo` are still excluded from
   *claiming* a goal-less task by `next_assignable_legacy_task_and_runtime/1`,
   with `commit_new_claim!/3` rolling such a claim back, so the requirement never
   decides that.
   *Amended design text:* the fixed vocabulary has no `result` adapter operation
   (`contracts/v1/common.schema.json#/definitions/AdapterOperations`,
   `additionalProperties: false`), and the terminal result travels on the
   `events` stream the requirement already demands; the requirement therefore
   uses the same operation set the daemon's own native admission check enforces
   (`admissionLaunchFailure/3`: `start`, `events`, `cancel`) and that Control's
   pre-existing native Goal admission requires
   (`Orchestration.native_goal_adapter_requirement/0`). Adding a separate
   `result` member would duplicate `events` in every adapter record and needs its
   own contract change. `docs/design/harness.md` now states this beside the
   sentence it corrects (amendment of 2026-11-17) instead of leaving the design
   text contradicted.
6. **Guidance versus native steering, unsupported pause.** The run scope labels
   guidance delivery from the served runtime row (`native_steer` → "native
   steering … not dispatched from this conversation", `next_turn` → delivered on
   the worker's next turn, `unsupported` → unsupported by the adapter, no
   adapter record → the worker's next safe execution boundary, the server's own
   wording) and renders no pause control when the adapter says
   `pause: "unsupported"`, explaining instead that the run is stopped from the
   work detail. Cancel and retry stay on the work-detail control surface (0007f);
   Chat dispatches only its own intents.
7. **Safe rendering.** Message bodies, questions, options and failures render as
   inert text; every hyperlink goes through one helper that accepts only
   `http:`/`https:`; `runs[].raw.timeline` is never rendered, and the semantic
   evidence section renders only the served semantic entries.
8. **The read contract is additive.** A new Draft 7 root
   `contracts/v1/chat-read.schema.json` describes the consumed conversation read
   (scope enum, messages, command receipts, the reused work-detail projection,
   the opaque cursor) with four valid and two invalid fixtures; the generator's
   schema-count loss-detectors moved 18 → 19 and `chat-read` joined the
   presentation-read allowlists. No existing schema member, fixture or generated
   symbol changed meaning.

## Files written

The delivered manifest is the 41 source paths below (every changed or untracked
path outside `docs/**`).

**Frontend**

- `frontend/src/features/chat/chat.tsx` (the page: scope bar, run picker, served
  scope context with run facts/links/failure, decision packet, semantic
  execution evidence, retained history, composer, start-work fields),
  `frontend/src/features/chat/scopes.ts` (the per-scope store: draft, intent,
  start-work selections, send status, frozen request, retained pages and
  cursors, scroll offsets), `frontend/src/features/chat/presentation.ts`
  (intent vocabulary/filtering, scope label, delivery labels, receipt views
  reusing `features/work/receipts.ts`, send-status text, safe links, history
  merge/dedupe/gap, decision packet, bounded failure text, evidence rows), and
  their tests `frontend/src/features/chat/chat.test.tsx` (22) and
  `frontend/src/features/chat/presentation.test.ts` (29).
- `frontend/src/services/api.ts` (+ `api.test.ts`) — `chatConversation` and
  `chatMessage` through the shared request boundary, decoding the read with the
  generated `ChatRead` contract boundary and the POST answer with a hand-written
  Effect struct (HTTP 201/200 is the disposition).
- `frontend/src/app/application.ts` — `safeReturnTo` accepts `/portal/chat`,
  `chatConversationOptions`, `chatOlderPageOptions`, `chatMessageMutation`.
- `frontend/src/app/router.tsx` (+ `router.test.tsx`) — the `/chat` route and
  its search validation.
- `frontend/src/app/shell.tsx` — the Chat navigation link, command-palette
  destination and the project-scope clause.
- `frontend/src/styles.css` — the chat layout block.

**Control (Elixir)**

- `control/lib/symmetry_control_web/router.ex` — `/portal/chat` document route.
- `control/lib/symmetry_control_web/portal_session.ex` — shell path and the
  anchored chat search allowlist.
- `control/lib/symmetry_control/chat.ex` — the start-path admission derivation,
  which asks `Orchestration.native_harness_kind?/1` for the vocabulary and is
  evaluated by `Workspaces.launch_work_item/3` inside its transaction, on the
  locked work item.
- `control/lib/symmetry_control/orchestration.ex` — the exported
  `native_harness_kind?/1` predicate over the one native-kind list, so a caller
  cannot restate it.
- `control/lib/symmetry_control/workspaces.ex` — `launch_work_item/3` accepts a
  requirement *function* and evaluates it after the row lock
  (`required_capabilities_for/2`).
- `control/test/symmetry_control/chat_test.exs` — the admission tests: the
  native-operations case, the legacy-binding case, a native kind outside the old
  Chat list, and the lock-race case that holds the work item's row lock while a
  real launcher waits for it.
- `control/test/symmetry_control_web/portal_session_test.exs` — accept/reject
  return-target cases.
- `control/lib/symmetry_control/goals/contract_validation.ex` and
  `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs` — the
  new read schema joins the shared fixture corpus: `chat_read` in
  `@schema_files`, `validate_chat_read/2`, and the manifest test's validator
  map. (The first full-suite run after the contracts slice failed here with
  `key "chat-read" not found`; the fix is part of this delivery, and the
  re-run is in the checks table.)

**Contracts**

- `contracts/v1/chat-read.schema.json`, `contracts/fixtures/valid/chat-read.*.json`
  (4), `contracts/fixtures/invalid/chat-read.*.json` (2),
  `contracts/fixtures/manifest.json`, `contracts/scripts/{generate,check,check-effect}.mjs`,
  `contracts/ts/index.ts`, and the generated
  `contracts/generated/{ts/chat-read.ts,ts/index.ts,effect/index.ts}`,
  `daemon/internal/contracts/{generated.go,schema_bundle.json}`.

**Browser acceptance**

- `browser/tests/portal-chat.spec.mjs` — rewritten onto `/portal/chat`; its
  scope-switch case now reads a scope's restored scroll after that scope's own
  messages rendered, and its run-scope round trip asserts the `run:<id>` key's
  own draft, intent and offset through the in-page scope control
  (`setScrolledOffset/2` refuses an offset the pane cannot hold).
- `browser/tests/portal-chat-live.spec.mjs` — rewritten onto `/portal/chat`; the
  cancellation phase asserts the attempt's own `completed_steps` across the
  daemon's release of its local journal (never a `progress.md` substring the
  work item's goal already carries), and the keyboard walk waits for the real
  window-focus refetch's response instead of sleeping past it.
- `browser/scripts/run-chat-daemon-tests.ps1` — the isolated live chat runner,
  now with the operator-margin agent profile and a per-run Playwright output
  directory.
- `browser/tests/readable-fixtures.mjs` — additive `createProject` binding and
  load-sized post-login waits for the shared shell.
- `browser/playwright.config.mjs` — the suites' `expect` maximum is a load-sized
  wait (30 s), never a weaker observation.
- `browser/README.md`, `.github/workflows/ci.yml` — the migrated coverage and its
  CI gate.

**Docs**

- `docs/plans/2026-11-17-0007g-scoped-engineering-chat-plan.md` — the plan.
- this receipt. The 0007 series closes with the receipt: the goal file stays at
  `docs/goals/0007g-scoped-engineering-chat.md` byte for byte (no archive move)
  and `docs/goals/README.md` already lists it, so neither changed here.

## Retired classic Chat coverage (with successors)

The classic suite drove `/portal/classic?…#chat`; both chat spec files now drive
`/portal/chat`, so no acceptance checks the classic markup any more. Per classic
assertion:

| Classic check | Disposition |
| --- | --- |
| Chat delivery rejects unsafe pull request links | migrated — run-scope fixture case (`javascript:` renders no anchor, an `https:` link does) plus the live run's rejected script-scheme pull request |
| discussion separated from starting real project work; the complete goal preserved | migrated — a real question saved with no command, and the live `start_work` walk that carries the full goal into the created work item |
| run guidance and controls stay durable until a safe-boundary acknowledgement | migrated — live guidance → reconnect → `applied` receipt, pause, resume, decision |
| decision packet binds the selected consequence to the waiting transition | migrated — live decision send from the served snapshot; the packet's `option_id` + `waiting_transition_id` are asserted from the served projection in the fixture case |
| the packet's "collapsed diagnostics"/raw-execution expander | retired, not migrated — the conversation surface renders no raw rows at all (`runs[].raw.timeline` is blanked by the server and never rendered); the work detail remains the diagnostic surface (0007f) |
| autonomous semantic updates appear in the conversation without raw events | migrated as the run scope's execution-evidence section, which renders only the served semantic entries (asserted against the run's own served timeline) and never a raw tool row |
| late context reads/sends cannot overwrite another context or its draft | migrated — the held late read and held late write cases |
| uncertain send retries with the same action ID | migrated — the frozen-payload retry with the server's `200` replay and one durable message |
| project-switch failure keeps run context (503 toast) | retired, not migrated — the failure path of the classic project switcher; the shell's project picker owns that surface (workspace-shell suite) and the chat surface keeps its own scope state across a failed read (reconnect case) |
| workspace start-work selections (project/repository/CI) survive a scope switch | migrated — the selection-retention case |
| oversized guidance is refused with the 32,768-byte limit | migrated — live: the client refuses without a POST and the command list is unchanged |
| failed runs show bounded, escaped failure text in Chat | migrated — live: the failed run's scope renders the served failure text, bounded to 2,000 characters, with no serialized object |
| background refresh preserves editing, caret, focus and expanded raw history | the composer draft/caret/focus half migrated (unit tests, live keyboard walk); the classic raw-execution expander has no equivalent and is retired with the work detail as its successor |
| classic live walk's leftover-timer/Activity-row assertions | none here; the classic live walk's Chat-specific assertions all migrated (the only retired classic live assertion is the classic DOM walk itself) |

## Checks executed

Recorded as executed on this host, with the exit code and the count each
command printed, and with the revision each row belongs to. **Rows 1–11 and 18
name the pre-repair revision** (`sha256:bb3d4821…d6af`, labelled per row);
rows 12 and 15–17 carry their own revision labels, row 12's repaired measure
being the repair round's R2; **rows 13 and 14 were re-run on the repaired
revision** (`sha256:d52d2506…031a`) and their rows carry both measures, the
earlier one labelled as history. Postgres for the Elixir runs is the local
container on port 55462 (`symmetry_control_dev` for the browser gates,
`symmetry_control_test` for ExUnit). The pre-repair browser runs rendered
against this working tree's Control on port 4015 (port 4010 was held by another
session's server on this host); the repair round's browser runs used this
working tree on port 4025, so no number in this file borrows another tree's
server.

| # | Command | Observed |
| --- | --- | --- |
| 1 | `mise exec pnpm@12.17.0 -- pnpm contracts:check` (root) | exit 0 — `Generated contract DTOs are up to date. Validated 19 Draft 7 schemas and 232 fixtures. Effect decoded all 232 raw fixtures; 228 parsed documents reached Schema validation.` (the chain also runs `contracts:typecheck` and the generator/boundary unit tests) |
| 2 | `go vet ./internal/contracts/...` | exit 0, no diagnostics |
| 3 | `go test ./internal/contracts/...` | exit 0 |
| 4 | `go build ./...` (daemon) | exit 0 |
| 5 | `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend typecheck` | exit 0, no diagnostics |
| 6 | `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend lint` | exit 0 (`eslint . --max-warnings 0`) |
| 7 | `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend test` | `16 passed (16)` files, `281 passed (281)` tests, exit 0 |
| 8 | `mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build` | exit 0 (Vite build plus `stage-assets.mjs` into `control/priv/static/portal`) |
| 9 | Focused Elixir: `mix test test/symmetry_control/chat_test.exs test/symmetry_control_web/portal_session_test.exs test/symmetry_control_web/controllers/chat_controller_test.exs test/symmetry_control/goals/contract_fixture_manifest_test.exs` | `Result: 29 passed`, exit 0 |
| 10 | `mix compile --warnings-as-errors` | exit 0 |
| 11 | Whole Control suite: `mix test` | exit 2 — `Result: 864/865 passed, 21 skipped`, `Failed: 1 test` in 579.3 s. The one failure is the pre-existing host limit, not this delivery: `SymmetryControl.Integrations.CommandTest` "provider token commands can keep stderr diagnostics out of stdout" gets `{:command_unavailable, "pwsh"}` because this host cannot spawn `pwsh` through the WindowsApps app-execution alias (before this delivery's contract-validation fix the same suite reported `863/865`, `21 skipped`, `2 failed`, exit 2 — the second failure was the chat-read fixture manifest, fixed and re-run here) |
| 12 | Browser, deterministic scoped conversation (both Chromium projects): `SYMMETRY_PORTAL_URL=http://127.0.0.1:4015 pnpm --filter symmetry-browser-acceptance test tests/portal-chat.spec.mjs` | Pre-repair delivered tree: `10 passed`, exit 0, 2.2m — the five cases at desktop and Pixel 7 widths. The run that immediately preceded that fix **failed on this tree**: `9 passed`, `1 failed` in 4.0m, exit 1, the mobile project failing `expect(await scrollOffset(page)).toBe(120)` with `Received: 0` at `browser/tests/portal-chat.spec.mjs:175` (`.tmp/gate-chat-det-final.log`, 09:27); the fix that followed it (the render wait before that scroll observation) produced this row's `10 passed` run (`.tmp/gate-chat-det-final2.log`, 09:27). The `10 passed`, exit 0, 1.2m run at `.tmp/gate-chat-det3.log` (07:41) belongs to an **earlier** revision of this work, not to the delivered tree: it predates the 09:05–09:27 edits to `readable-fixtures.mjs`, `playwright.config.mjs` and the spec. Repaired tree (F1–F8): `12 passed`, exit 0, 2.5m on the final repair bytes (3.4 m on the repaired tree's preceding bytes) — repair check R2 below, which also names the red-before demonstration for the new F5 case (R6b) |
| 13 | Browser, pre-existing coverage, **repaired revision**: `SYMMETRY_PORTAL_URL=http://127.0.0.1:4025 … test tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs tests/portal-execution-control.spec.mjs` | `39 passed`, `3 skipped`, `5 failed`, exit 1, 21.9m (`.tmp/repair-pre132-2.log`) — every failure is in the mobile project, and **all five failing cases pass in isolation on the same bytes** (`5 passed`, exit 0, 45.5 s, `.tmp/repair-pre132-isolated.log`): the full run took 21.9 m against 7.9 m for the same set pre-repair, so these are host-load timeouts. Neither the repair's only shared frontend change (the chat page's own `.chat-messages` rule) nor the Chat start path is on any screen these cases render. The skips are the two true-TTL expiry cases with no `SYMMETRY_SHELL_EXPIRY_URL` instance and the desktop-only concurrency case. **History, labelled:** pre-repair `45 passed`, `3 skipped`, exit 0, 7.9m (and `45`/`3` in 4.7 m before the day's runs); the first attempt on the repaired bytes, with 132 accumulated project fixtures, 1,408 chat messages and 10 machines/runtimes in the dev database, reported `43 passed`, `3 skipped`, `2 failed`, exit 1, 10.9 m (`.tmp/repair-pre132-1.log`), after which those fixture rows were cleared (`TRUNCATE projects CASCADE`, `DELETE FROM machines`: 132 projects, 180 work items, 37 tasks, 20 runs, 1,408 messages, 10 machines and runtimes → 0). A third attempt, started while the host was again saturated (the Control instance answered `200` throughout), was abandoned by the operator after its first four cases failed under load; it is not a pass and no count from it is claimed |
| 14 | Live work-detail matrix, **repaired revision**: `pwsh -NoProfile -File browser/scripts/run-live-daemon-tests.ps1 -ControlUrl http://127.0.0.1:4025` | exit 0, 2.2m — phase 1 `1 passed (3.8s)`; phase 2 `6 passed`, `4 skipped`, `Live daemon browser tests passed` (`.tmp/repair-livematrix-1.log`). This is the migrated classic Chat target plus the 0007f live work-detail surface measured on the repaired bytes. Pre-repair: exit 0, 1.8m, phase 1 `1 passed`, phase 2 `6 passed`, `4 skipped` |
| 15 | Live scoped conversation (**pre-repair revision**): `pwsh -NoProfile -File browser/scripts/run-chat-daemon-tests.ps1 -ControlUrl http://127.0.0.1:4015` | exit 0 — `1 passed (3.9m)`, `Chat live acceptance passed`, log `.tmp/gate-chat-live22.log`. One case walks the whole surface against a real cooperative daemon: question with an unchanged command list, guidance observed pending and then applied with a `data-receipt-for` receipt, safe-boundary pause in run 1 (artifact already carrying the guidance and the same PID + process identity), a failed read with the loaded conversation retained, recovery through the surface's own control, in-scope `#chat-history-gap` count 0, same-run resume, the served decision packet with its recommended option, completion with the guidance in `result.json` and exactly one recorded decision, one action identity's `200` replay plus `409` `idempotency_conflict` with one durable row, per-scope drafts/intents/scroll across workspace-project-run in one session, a message carrying markup rendered inertly with no created element, the keyboard/focus walk (composer caret 4-9 and value kept across an announced background refresh, scopes reachable by keyboard, `Load older` keeping focus, capture `chat-keyboard-walk`), a stale-generation control refused by the server while run 3 stayed live, cancellation with a retained artifact and a removed local journal, the malformed wait's bounded escaped failure text in run 4, the server refusing a `javascript:` pull-request URL (`422 validation_failed`) while the `https:` one renders as exactly one anchor, and the recovery run reaching `completed`. Superseded for the repaired bytes by repair checks R3 (same walk, `1 passed (2.5m)`, exit 0) and R6c |
| 16 | Red before / green after (Control admission, **pre-repair revision**): the two new admission tests against the unmodified `chat.ex` | exit 2 — `Result: 1/2 passed, Failed: 1 test`; the failing case is the native-admission test (`left: %{"supervisory_control" => true}` / `right: %{"adapter" => …}`). With the change: `Result: 2 passed`, exit 0. On the repaired bytes the corresponding measures are repair checks R6a (`0/2 passed` with the repair reverted, `19 passed` restored) and R1 |
| 17 | Red before / green after (browser surface): the deterministic scope-switch case against the pre-change staged build | exit 1 — the failure is the **post-login shell assertion in `browser/tests/readable-fixtures.mjs:22`** (`getByRole("button", { name: "Refresh", exact: true })` absent on the returned page), not a `/portal/chat` assertion: the case never reaches a chat locator. The base router is the evidence — `git show dd2e9db:frontend/src/app/router.tsx:13-14,50` has no `/chat` route and its `notFoundComponent` renders no Refresh button and no Project combobox, so the pre-change build answers that URL with the shell's own "Page not found" fallback. The run predates the later wait/sequencing edits, which only change how long a wait lasts and not why the classic build fails. With the change: `10 passed`, exit 0 (pre-repair tree); `12 passed`, exit 0 (repaired tree, F5 row below) |
| 18 | Source identity (pre-repair revision): `pwsh .tmp/source-digest.ps1` | 43 delivered source paths (changed + untracked, `docs/**` excluded), `sha256:bb3d4821…d6af` — the pre-repair value in the revision table above; the repaired revision's value is repair check R7 |

## Repair round (independent audit findings F1–F8)

An independent audit of the delivered revision raised eight findings (F1–F8).
This section records what each repair changed and re-measured. Rows above that
name the pre-repair digest were produced on that tree; the rows here were
produced on the repaired tree whose digest is in the revision table, and each
one names the revision it measured.

| # | Finding | What changed |
| --- | --- | --- |
| F1 | Check 12's earlier run was attributed to the wrong revision | **Row 12 rewritten** (see above): the delivered tree's own pre-fix run appears with its fix, and the 1.2 m run is attributed to the revision that produced it (`.tmp/gate-chat-det-final.log`, `.tmp/gate-chat-det-final2.log`, `.tmp/gate-chat-det3.log`) |
| F2 | The design text promised a `result` adapter operation the requirement cannot demand | `docs/design/harness.md` "Existing chat compatibility" is corrected in place and carries an amendment (2026-11-17) with the contract, daemon and Control evidence; **the implementation's requirement is unchanged** |
| F3 | The cancellation assertion was satisfied by the test's own goal text, and two sleep windows stood in for observations | `browser/tests/portal-chat-live.spec.mjs`: the cancellation phase asserts the attempt's own `completed_steps` (read through a bounded poll on that field, compared across the daemon's release of its local journal), the `visibilitychange` wait is the real refetch response, and the post-cancellation byte-immutability sleep is dropped as superseded. The two remaining `waitForTimeout(500)` calls are the bounded retry backoffs in `setPullRequestUrl/3` and the unsafe-link loop, both entered only after an observed server `409` (see the limitations). Verified by R3 (the live walk, including its failed first attempt) and R6c (the red-before demonstration) |
| F4 | `chat.ex` restated Orchestration's native-harness vocabulary | `Orchestration.native_harness_kind?/1` is exported over the one list and the derivation asks it; a new admission test registers a native kind outside the old Chat list |
| F5 | The run scope was never switched in-page with any state to preserve | New deterministic case "the run scope keeps its own draft, intent and scroll across an in-page round trip", run at both widths, with `setScrolledOffset/2` refusing an offset the pane cannot hold |
| F6 | The admission requirement was derived before the launch's row lock | `Workspaces.launch_work_item/3` evaluates the requirement *function* after `lock_work_item_with_project/1`; a lock-race test holds that row lock while a real launcher waits, changes the binding, and asserts the stored requirement describes the locked row |
| F7 | `.chat-messages { min-height: calc(100% + 240px) }` manufactured the scrollability the retention assertions relied on | Removed from `frontend/src/styles.css`; the 40/120 px retention assertions now rest on real seeded message content, and the new run-scope case on its own window |
| F8 | The red-before browser row did not name which assertion failed | **Row 17 rewritten** (see above) to name the post-login shell assertion in `browser/tests/readable-fixtures.mjs:22` and the base-router evidence that makes it the pre-change build's failure |

### Repair checks (repaired revision)

| # | Command | Observed |
| --- | --- | --- |
| R1 | Focused Elixir over the files this repair touches: `mix test test/symmetry_control/chat_test.exs test/symmetry_control_web/portal_session_test.exs test/symmetry_control_web/controllers/chat_controller_test.exs test/symmetry_control/goals/contract_fixture_manifest_test.exs` with `POSTGRES_PORT=55462 POSTGRES_DB=symmetry_control_test` | `Result: 31 passed`, exit 0, 15.3 s (19 of them `chat_test.exs`, including the two new admission tests and the lock-race test; the pre-repair delivered tree reported `29 passed` for this set, check 9) |
| R2 | Browser, deterministic scoped conversation on the repaired revision: `SYMMETRY_PORTAL_URL=http://127.0.0.1:4025 mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test tests/portal-chat.spec.mjs` | `12 passed`, exit 0, 2.5m — six cases at desktop and Pixel 7 widths, including the new run-scope round trip (11.4 s desktop, 10.6 s mobile) and the 40/120 px retention assertions with the height hack removed (`.tmp/repair-det-2.log`, final repair bytes). An earlier run of the same command on the repaired tree's preceding bytes reported the same `12 passed`, exit 0, in 3.4 m (`.tmp/repair-det-1.log`, before the new case's render wait was added) |
| R3 | Live scoped conversation on the repaired revision: `pwsh -NoProfile -File browser/scripts/run-chat-daemon-tests.ps1 -ControlUrl http://127.0.0.1:4025` | Attempt 1 (exit 1, 3.2 m, `.tmp/repair-live-1.log`): `TimeoutError: page.waitForResponse: Timeout 30000ms exceeded` at `browser/tests/portal-chat-live.spec.mjs:399`. The new synchronization refused to pass because the walk dispatched `visibilitychange` on `document` while TanStack Query's focus manager listens on `window` (`@tanstack/query-core` `focusManager.ts`: `window.addEventListener("visibilitychange", …)`), so the previous sleep-only block could not distinguish a refresh from no refresh. Fix: dispatch the event on `window`. Attempt 2 (final repair bytes): `1 passed (2.5m)`, exit 0, `Chat live acceptance passed` (`.tmp/repair-live-2.log`); the walk observed the cancellation on the cancelled attempt's own `completed_steps` and the window-focus refetch's own response, and every pre-existing live assertion stayed green |
| R4 | `mix format --check-formatted lib/symmetry_control/chat.ex lib/symmetry_control/orchestration.ex lib/symmetry_control/workspaces.ex test/symmetry_control/chat_test.exs` (pinned toolchain) | exit 0 |
| R5 | Frontend on the repaired revision: typecheck, `eslint . --max-warnings 0`, `vitest`, `vite build` (`pnpm --filter @symmetry/frontend …`) | typecheck exit 0; lint exit 0; `16 passed (16)` files and `281 passed (281)` tests, exit 0; build exit 0 and the staged `control/priv/static/portal` assets carry the corrected `.chat-messages` rule |
| R6a | Red before / green after (F4 + F6, Control), reverted in place and restored | exit 2 — with `chat.ex` back on its own four-kind list and the pre-lock derivation (and `workspaces.ex` reading a literal map), `mix test test/symmetry_control/chat_test.exs:123 test/symmetry_control/chat_test.exs:172` reported `Result: 0/2 passed, 17 excluded`: the lock-race test failed `assert task.required_capabilities == %{"supervisory_control" => true}` with `left: %{"adapter" => %{"operations" => %{"cancel" => true, "events" => true, "pause" => "unsupported", "start" => true}}}` (the pre-lock read's stale native requirement), and the shared-vocabulary test failed the mirror image (`left: %{"supervisory_control" => true}`) for the `omo` binding. Restored: `Result: 19 passed`, exit 0, and then `Result: 31 passed`, exit 0, for the focused set (R1) |
| R6b | Red before / green after (F5, deterministic round trip), scratch copy of the case without the window it runs in | exit 1 — `expect(reachable, "the conversation pane scrolls at least 40px").toBeGreaterThanOrEqual(40)` failed with `Expected: >= 40`, `Received: 0` (`.tmp/f5-demo.log`): a pane shorter than its content is the state the case must reject, and it cannot pass by reporting a clamped 0. The delivered case (whose window is 260 px tall) passes, R2 |
| R6c | Red before / green after (F3, live cancellation), scratch copy that never sends the cancel and waits for the attempt's decision boundary instead | exit 1, 3.5 m (`.tmp/f3-demo2.log`) — printed `[f3-demo] pre-fix predicate (progress.md contains "cancelled") = true` and `[f3-demo] counters: 9 before, 150 after; total 200`, then failed the delivered assertion `no writer advanced the artifact after the cancel was acknowledged`: `Expected: 9`, `Received: 150`. The pre-fix assertion is satisfied by a run that was never cancelled; the repaired one fails for exactly that state. (One earlier attempt of this demonstration was cancelled by the operator about a minute in, because the scratch copy predated R3's `window`-dispatch fix and would have failed at the refetch wait instead; the copy was corrected and re-run.) |
| R7 | Source identity on the repaired tree: `pwsh .tmp/source-digest.ps1` | 45 delivered source paths (changed + untracked, `docs/**` excluded — the two Control files this repair adds to the manifest are the two extra paths), `sha256:d52d2506…031a` recorded in the revision table above. Run after the demonstration copies were deleted, so the recorded subject is the delivered tree alone |

### What the repair round did not verify

- **The whole Control suite was not re-run on the repaired tree.** Its number in
  check 11 names the pre-repair digest. The repaired Elixir delta is
  `chat.ex`/`orchestration.ex`/`workspaces.ex` plus `chat_test.exs`, and a scoped
  search for the new predicate and the changed launch option found no other
  consumer in `control/lib` or `control/test`; R1 measures the focused files,
  including the two admission paths and the lock race.
- **The pre-existing browser coverage suites and the live work-detail matrix
  were re-run on the repaired tree** (checks 13 and 14). The work-detail matrix
  is green (`6 passed`, `4 skipped`, exit 0). The coverage suites' repaired-tree
  run reports `39 passed`, `3 skipped`, `5 failed` on a host running ~3× slower
  than the pre-repair pass (21.9 m against 7.9 m); all five failing cases are in
  the mobile project and **all five pass in isolation on the same bytes**
  (`5 passed`, exit 0, 45.5 s). None of them renders the chat page or the Chat
  start path, so neither the F7 rule nor the F4/F6 derivation is on their
  screens; the failures are host-load timeouts. A fully green repaired-tree run
  of that suite is therefore **not** claimed: the row carries the failing run,
  the isolation pass and the labelled pre-repair pass.
- **Hosted CI has still not run for this revision**, and nothing was pushed,
  committed or opened as a PR.
- **The `omp`/`omo` admission change (F4) is observed at the Control level
  only**: no native `omp`/`omo` daemon exists on this host, so the claim query's
  own exclusion remains the daemon-side guarantee.
- **Owner acceptance is still not supplied** (unchanged from the pre-repair
  record).

## Rebase onto current main (2026-11-17)

The branch was rebased in place onto `706174e` (main after #37 and #39–#45) so it can open as one
mergeable PR against the current main. Nothing was pushed, `main` and every other worktree were left
untouched, and the rebased tree is what this section's rows were measured on. Seventeen paths
conflicted; each was resolved as the union of both sides, and only the schema-count constants and four
one-line alternations are files where both sides rewrote the same line (they take the value the union
requires: 18 base schemas + upstream's `attention-read`/`goal-read` + this delivery's `chat-read` = 21).
This section's rows are B1–B4; row 12, R2 and row 13 keep their own revision labels and their
re-measured counterparts are B2 and B3.

| Conflicted path | Resolution |
| --- | --- |
| `.github/workflows/ci.yml` | upstream's consolidated control job kept whole (the folded `pi-control-e2e-linux` steps and the credentialed-witness step) and this delivery's `tests/portal-chat.spec.mjs` added to the release render step's suite list |
| `contracts/scripts/generate.mjs`, `contracts/scripts/check.mjs` | the schema-count loss detector reads 21 — the 21 `contracts/v1/*.schema.json` roots on disk are the evidence; `check.mjs`'s presentation-read set keeps both sides' entries |
| `contracts/scripts/check-effect.mjs` | presentation-read set, decoder map and schema map are the union of both sides |
| `contracts/ts/index.ts` | `ChatReadSchema`/`ChatRead` added beside upstream's `AttentionRead*`/`GoalRead*` |
| `contracts/fixtures/manifest.json` | upstream's reformatting kept whole and this delivery's six `chat-read` fixtures appended verbatim: 240 entries = 226 base + 8 upstream + 6 here, every entry byte-equal to the side that authored it, every id resolvable and every referenced schema known |
| `contracts/generated/ts/index.ts`, `contracts/generated/effect/index.ts`, `daemon/internal/contracts/generated.go`, `daemon/internal/contracts/schema_bundle.json` | regenerated from the union schema set: `node contracts/scripts/generate.mjs --check` passes on this tree **and** on the unmodified `706174e` tree, so the artifacts carry no host or dependency drift, and `pnpm contracts:check` reports 21 schemas and 240 fixtures |
| `control/lib/symmetry_control_web/portal_session.ex` | one `@shell_path` carrying both sides' members: `attention|connections|chat|goals/[A-Za-z0-9_-]+|projects(?:/[A-Za-z0-9_-]+)?|runtimes|work/[A-Za-z0-9_-]+` |
| `control/test/symmetry_control_web/portal_session_test.exs` | the reject list carries upstream's six `/portal/goals…` cases and this delivery's four `/portal/chat…` cases |
| `frontend/src/app/application.ts` | `safeReturnTo`'s path alternation carries both sides' members (same list as the shell path) |
| `frontend/src/app/router.tsx` | route tree: upstream's `goalRoute` plus this delivery's `chatRoute` |
| `frontend/src/app/router.test.tsx` | import union (`readAttention` + `safeReturnTo`); both sides' cases retained |
| `frontend/src/services/api.ts` | import union (`AttentionRead as AttentionReadBoundary`, `ChatRead`, `type SymmetryAttentionReadV1`, `type SymmetryChatReadV1`); this delivery's chat types, decoders and methods unchanged |
| `docs/TODO.md` | every section both sides added is present: `(0007g)`, `(0007h)` and the CI consolidation decisions |
| auto-merged both-side paths: `browser/README.md`, `control/lib/symmetry_control_web/router.ex`, `control/test/symmetry_control/goals/contract_fixture_manifest_test.exs`, `frontend/src/styles.css` | merged coherently by git and audited: both document routes are in the router (`/portal/chat` and `/portal/goals/:goal_id`), the manifest test keeps upstream's `@presentation_reads` attribute and guard beside this delivery's `chat-read` validator entry, and the stylesheet keeps upstream's rules plus this delivery's chat block in the same layer |

Every path both sides changed (17 conflicted and 4 auto-merged, 21 in all) was then audited line by
line against both sides' additions: every line either side added relative to `dd2e9db` is present in
the rebased tree, and the only absent lines are the ones the two sides rewrote in place (the rows
above). Twenty-five of the 45 delivered source paths are byte-identical to the repaired revision —
including `browser/tests/portal-chat.spec.mjs`, `browser/tests/readable-fixtures.mjs`,
`frontend/src/features/chat/chat.tsx`, `scopes.ts`, `presentation.ts` and
`contracts/fixtures/valid/chat-read.run-waiting-decision.json`.

| # | Command | Observed |
| --- | --- | --- |
| B1 | Source identity: the reproduction procedure above with the rebased base (`pwsh -NoProfile -File .tmp/rebase/source-digest-rebased.ps1`) | 45 delivered source paths (`docs/**` excluded) and `sha256:2a10b33e0ea248e178d2f000c012bb6c505123d4409e43560377a3ce122e1e58` (revision table). The path set is identical to the repaired revision's and 20 paths differ in content, so the earlier digest values do not describe this tree; every row that names them names its own revision and none of them is restated here |
| B2 | Deterministic scoped conversation, rebased revision: `SYMMETRY_PORTAL_URL=http://127.0.0.1:4035 mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test tests/portal-chat.spec.mjs` | Five suite-context attempts, each exit 1 with exactly one failure and 11 of 12 cases green: `11 passed`, 1.4 m — desktop `…:469` read `Expected: 40` / `Received: 0` at its restored offset (`.tmp/rebase/rb-det-chat.log`); `5 passed` on the desktop project alone, 43.1 s (`…:469`, `.tmp/rebase/rb-det-chat-desktop.log`); `11 passed` on the desktop project doubled, 2.3 m — the run-scope render case `…:376` timed out at 33.6 s in its second pass (`.tmp/rebase/rb-det-chat-desktop2x.log`); `11 passed`, 1.6 m on a freshly created and migrated database (`…:469`, `.tmp/rebase/rb-det-chat-freshdb.log`); `11 passed`, 2.5 m on a fresh database with the server's debug output off the terminal — mobile `…:106` read `Expected: 120` / `Received: 0` (`.tmp/rebase/rb-det-chat-freshdb2.log`). On the same bytes the same cases pass alone and in sequence: `…:469` alone `1 passed`, 7.4 s (`.tmp/rebase/rb-det-chat-case1.log`); `…:376` + `…:469` `2 passed`, 10.7 s (`.tmp/rebase/rb-det-chat-pair.log`). The failing case, its helper and its fixture are unchanged from the repaired revision (the unchanged list above), and the failing case differs between attempts: this is the load class `docs/TODO.md` now records, not a rebase regression — but the suite is **not** claimed green on the rebased revision |
| B3 | Isolated five-case mobile run, rebased revision: `SYMMETRY_PORTAL_URL=http://127.0.0.1:4035 mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test tests/portal-execution-control.spec.mjs:232 tests/portal-execution-control.spec.mjs:293 tests/portal-work-views.spec.mjs:126 tests/portal-work-views.spec.mjs:613 tests/portal-work-views.spec.mjs:707 --project=mobile-chromium` | `5 passed`, exit 0, 38.5 s (`.tmp/rebase/rb-isolated-mobile.log`) — the five cases check 13's full run left red, pass on the rebased bytes in isolation |
| B4 | Elixir, frontend and contract gates on the rebased revision (pinned toolchains): `mix format --check-formatted` over the nine changed Elixir files; `mix compile --warnings-as-errors`; R1's focused suite; `pnpm --filter @symmetry/frontend typecheck`, `lint`, `test`, `build`; `pnpm contracts:check` | format exit 0; compile exit 0; `Result: 31 passed`, exit 0, 9.4 s (`.tmp/rebase/rb-focused-elixir.log`); typecheck exit 0; lint exit 0; `17 passed (17)` files, `295 passed (295)` tests, exit 0 (the count is the union of both sides' suites, `.tmp/rebase/rb-fe-test.log`); build exit 0 with the staged `control/priv/static/portal` assets being this tree's (`.tmp/rebase/fe-build.log`); `contracts:check` exit 0 — `Validated 21 Draft 7 schemas and 240 fixtures`, `Effect decoded all 240 raw fixtures`, `Generated contract DTOs are up to date.` (`.tmp/rebase/rb-contracts-check.log`) |

**Rows the rebase did not re-measure** (each keeps the revision label it already carries): checks 1–11
and 18, checks 15 and 16, check 12's pre-repair and repaired-tree runs (B2 is the rebased revision's
re-run), check 13's three-suite full run (only its isolation set was re-run, B3), check 14's live
work-detail matrix, R3's live scoped-conversation walk, R6a–R6c's red-before demonstrations, and R5's
frontend values (re-run in B4). None of them is restated here as a property of the rebased revision.

## Limitations

- **The rebased revision's rows are B1–B4; everything else keeps the revision it names.** The rebase
  re-measured the source identity (B1), the deterministic scoped-conversation suite (B2), the
  five-case mobile isolation set (B3) and the Elixir/frontend/contract gates (B4). The deterministic
  scoped-conversation suite is **not** claimed green on the rebased revision: five suite-context
  attempts each ended with one failure (11 of 12 cases green) while the same cases pass in isolation
  and in sequence on the same bytes, and the failing case differs between attempts. The whole Control
  suite, the three-suite browser full run, the live work-detail matrix and the live
  scoped-conversation walk were not re-run on the rebased revision, and the rebase section names each
  of them.
- **Owner acceptance is not supplied.** The goal's own Owner-acceptance
  paragraph asks the owner to review rendered workspace-, project- and
  run-scoped conversation with a work-item target on actual work; a model
  cannot rate its own rendering or sign for the owner. This record states the
  result as produced and awaiting the owner's verdict.
- **The native admission path is observed at the Control level only.** No native
  harness daemon is available on this host (the live fixtures are the generic
  cooperative fake agent), so the native branch's end-to-end execution — a
  native adapter claiming the task and passing the daemon's own per-operation
  admission checks — is **not** observed here. The observed admissions are the
  created task's operation-specific `required_capabilities` and the runtime the
  real scheduler assigns on both paths (check 16); the daemon's
  `admissionLaunchFailure/3` operation checks are pre-existing, untouched by
  this delivery, and covered by the daemon's own admission tests. The browser
  run-scope case that needs a native adapter row fulfils a committed,
  schema-valid chat-read contract fixture (labelled synthetic in the spec) and
  never counts as real run evidence; the real run-scope walk (decision packet,
  receipts, pause/resume, guidance delivery on a real runtime) is the live
  suite.
- **The design's "start/events/result" is amended, not reinterpreted**
  (`docs/design/harness.md`, "Existing chat compatibility"): the sentence is
  corrected in place and carries an amendment of 2026-11-17 beside it. The
  fixed adapter-operation vocabulary has no `result` member — the contract
  (`contracts/v1/common.schema.json` declares `AdapterOperations` with
  `additionalProperties: false` and no such property),
  `daemon/internal/harness/types.go:64-77` (no result capability) and
  `daemon/internal/app/app.go:4341` (exactly `start`/`events`/`cancel` at
  launch) are the evidence — and the terminal result already travels on the
  `events` stream. The requirement is therefore `start` + `events` + `cancel`
  with `pause: "unsupported"`, the same map Control's pre-existing native Goal
  admission requires (`orchestration.ex:1707-1717`), so Chat and Goal admission
  demand one vocabulary. Adding `result` needs its own contract change and was
  not done here; no test can observe a `result` operation because no contract
  or daemon surface can produce one.
- **A native harness kind outside the old Chat list is now admitted natively**
  (finding F4): `Chat` asks `Orchestration.native_harness_kind?/1` instead of
  keeping its own copy of the vocabulary, so a binding whose registered runtimes
  are all native harnesses demands the adapter operations even when one of them
  is `omp`/`omo`. This does not weaken the claim rule:
  `next_assignable_legacy_task_and_runtime/1` still excludes `omp`/`omo` from a
  goal-less claim and `commit_new_claim!/3` still rolls it back, so a pure
  `omp`/`omo` binding is still never claimed (it was not claimable before
  either, under the legacy flag). What changes is a mixed binding, which can now
  be claimed by its claimable native sibling instead of being stranded behind a
  requirement no native runtime advertises. Observed at the Control level only
  (see the admission limitation above).
- **The control-restart phase of the live walk was not exercised**:
  `SYMMETRY_CHAT_CONTROL_CONTAINER` was not set for these runs, so the walk's
  `docker restart` barrier block did not execute (the classic suite recorded the
  same guard).
- **The live scoped-conversation walk runs at desktop width only**; the
  deterministic suite covers both widths, and the runner's spec skips mobile by
  design. The walk is one case because the runner starts one daemon with one
  capacity slot (`existing_checkout` forbids a larger runtime capacity), so a
  second case would race the first case's capacity-bearing run.
- **Hosted CI has not run for this revision.** The render gate's file list now
  includes `tests/portal-chat.spec.mjs`, so the migrated coverage is enforced
  there; the hosted result itself is unverified here.
- **The live walk's waits are load-sized maximums, and two of its fixture
  constants exist for the operator's margin.** The runner's cooperative agent
  runs 200 steps at 400 ms with its decision at step 150, so the operator's
  pre-decision controls (question, guidance, pause) reach a safe boundary
  before the agent asks for its decision; with the fake agent's default profile
  the operator window is ~16 s and the agent answered a late pause with
  `rejected` because it was already waiting for its decision. The
  browser suites' `expect` timeout is 30 s because a real conversation read of
  a live run was measured at 9.9 s in a retained trace while the daemon
  streamed events on this host. Neither constant is an assertion: every
  observation in those suites is unchanged, and the walk is judged on
  observable behaviour, never on step counts or durations.
- **Scroll observations in both scoped-conversation suites now follow the
  render of the scope they read.** The scope-switch cases assert a restored
  `scrollTop` with a plain comparison, so they first wait for that scope's own
  messages to be visible; with a slow read the comparison could otherwise run
  against the loading pane. The asserted offsets are unchanged (120 px for the
  project scope, 40 px for the workspace scope; the repaired run-scope round
  trip adds 40 px for `run:<id>`, with the pane's reachable range asserted first
  so a clamped 0 can never be reported as a retained offset).
- **The live walk keeps two `waitForTimeout(500)` calls, both inside bounded
  retry loops rather than race windows.** `setPullRequestUrl/3` re-reads the
  served version and re-issues the write only after the server answered `409`
  (at most ten attempts, then a thrown error), and the unsafe-link loop does the
  same until the `422 validation_failed` arrives. Neither wait is the evidence:
  the evidence is the server's own answer, and the wait only spaces the attempts
  after an observed refusal. The two sleep *windows* the audit found — the
  visibilitychange block and the post-cancellation artifact comparison — are
  gone, replaced by the refetch's own response and by the attempt's step counter
  across the daemon's journal release.
- **Each live chat run keeps its own Playwright output directory.** One walk
  failed at teardown with `ENOENT … -recording1.trace` while the shared temp
  output directory was cleaned by a later Playwright start; the runner now
  passes `SYMMETRY_PLAYWRIGHT_OUTPUT` under its own run root (the convention
  `run-live-daemon-tests.ps1` already uses), which keeps traces, videos and
  screenshots of a run together with its daemon logs.
- **The classic Chat surface keeps its markup until the 0007i cutover**: no
  acceptance checks it any more (this receipt's retired-coverage table names
  each successor), the `#chat` hash still maps to `/portal/classic#chat`
  (`frontend/src/app/legacy.ts`), and the classic drawer's "Open Chat" /
  "Review decision in Chat" buttons remain the only work-detail entry point into
  a run's conversation. Both are recorded in `docs/TODO.md` under
  "Deferred review findings (0007g)".
- **The whole Control suite** keeps one environment-caused failure on this host
  (`integrations/command_test.exs` cannot spawn `pwsh` through the WindowsApps
  app-execution alias), unchanged by this delivery and not a pass. The number in
  check 11 is the observed result for this revision, not a claim of a green
  whole suite.
- **Port note**: the gate command's `SYMMETRY_PORTAL_URL` names port 4015 because
  another session's Control server held 4010 on this host; the served build is
  this working tree.
