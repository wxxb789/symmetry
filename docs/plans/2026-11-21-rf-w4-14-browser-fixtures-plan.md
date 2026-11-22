# rf-w4-14-browser-fixtures plan — one browser fixture module for the CI-run spec set

Contract: `goal-rf-w4-14-browser-fixtures.md` (GDD, 2026-11-21), binding with its
conductor decisions E6 (narrowing to the CI-run families), E8 (runnable oracles),
D1 (frozen bytes), D2 (LOC report-only), D7 and the lesson-32 control-flow audit.
Base for every coordinate below: this worktree's HEAD `7654f32a`
(`refactor/rf-w4-14-browser-fixtures`, W2-01's tip; the contract's dispatch
predecessor). The tree was clean at that revision and every line reference below
was re-measured there.

Sources: `program-plan.md` §7 W4-14; `findings/tests-cross-cutting.md` T5:352-375
(the clone windows) and `findings/scripts-ci.md` F8:278-296 (the four specs that
redefine already-shared helpers); `browser/README.md`'s manual matrix and
`scripts/ci/assert-browser-suite-coverage.py` (W2-01's inventory, read before
touching the spec set — it classifies suite files, so this slice renames nothing).
Shape references: `docs/plans/2026-11-21-rf-w2-15-served-value-module-plan.md`
(#164 — same kind of increment: declaration copies move into one shared module,
extraction diffs prove the move, mutations are named) and
`docs/plans/2026-11-21-rf-w2-04-portal-json-plan.md` (#161).

## Measured basis (base revision, `7654f32a`)

Definition census over the in-scope set (the four CI-run specs that carry copies +
the shared module). Command, run from `browser/`:

```sh
grep -nE "^(export )?(async )?function (login|api|capture|createProject|recordHistoryWrites|executionPolicy|acceptance|authorityPolicy|contextManifest|deviceContextOptions|historyWrites)\b|^(export )?const (acceptance|acceptanceContract|authorityPolicy|executionPolicy|contextManifest|planProposal|historyWrites|FALLBACK_READ_GUARD_MS|deviceContextOptions|login|api|capture|createProject)\b" \
  tests/workspace-shell.spec.mjs tests/portal-goal-attention.spec.mjs tests/portal-cutover.spec.mjs tests/portal-work-views.spec.mjs tests/readable-fixtures.mjs
```

The `const` branch names the payload builders explicitly: `acceptance` is the
base's const-form definition, `acceptanceContract` its final-tree name, and
`authorityPolicy`/`executionPolicy`/`contextManifest`/`planProposal` are const
both before and after. This returns **24** lines at `7654f32a` and **13** at the
final tree (all in the shared module; the per-helper table below splits them).

| helper | copies at base | classification |
| --- | --- | --- |
| `recordHistoryWrites` | `workspace-shell.spec.mjs:30`, `portal-cutover.spec.mjs:40` (+ shared: none) | **byte-identical** (extraction diff, empty) |
| `historyWrites` | `workspace-shell:41`, `portal-cutover:51` | **identical** one-liner |
| `login` | `workspace-shell:43`, `portal-goal-attention:25`, shared `readable-fixtures.mjs:25` | three variants: workspace-shell adds a third readiness marker; goal-attention adds `base`; shared carries the guard |
| `api` | `workspace-shell:52` (2-arg POST), `portal-goal-attention:32` (3-arg + `null` body on parse failure), shared `:33` | three variants; the shared signature is the superset |
| `capture` | `workspace-shell:86`, shared `:191` | workspace-shell's copy is the shared body with `fullPage: true` fixed |
| `createProject` | `portal-goal-attention:76` (`(page, suffix, {blocked})`), shared `:77` (options object) | variant adds a key-minting rule, goal-workspace identity and an optional blocked work item |
| `executionPolicy` | `portal-goal-attention:58`, `portal-cutover:61` | **byte-identical** |
| `authorityPolicy` | `portal-goal-attention:51`, `portal-cutover:27` | **identical** (same four keys/values) |
| `acceptance` | `portal-goal-attention:45`, `portal-cutover:21` | differs in the frozen `description` text only |
| `contextManifest` | `portal-goal-attention:74`, `portal-cutover:77` | identical object, different line wrapping |
| `deviceContextOptions` | `portal-work-views:91`, `portal-connections:233` (E6-excluded) | work-views spreads `baseURL` at the call site, connections bakes its own origin in |
| `FALLBACK_READ_GUARD_MS` | `portal-cutover:60` only | single copy, contract-named move (bounds a poll over the shared `readsStatus` record) |
| the `symmetry.plan.v1` proposal literal | `portal-cutover:115-142`, `portal-goal-attention:156-177` | identical field set/order; JSON formatting only |

Excluded remainder recorded by E6 (untouched in this slice, owned by W6-02):
`portal-connections.spec.mjs` (`login:109`, `api(page, path, {method, body}):117`,
`createProject:132`, `deviceContextOptions:233`), `portal-work-planning.spec.mjs`
(`columnOf:92`, `moveByKeyboard:98`), `portal-compose-daemon.spec.mjs`
(`uniqueProject:20`, `fixtureDescription:27`, `projectUrl:31` (a byte-identical
copy of `readable-fixtures.mjs:74-77` modulo `export`), `workUrl:35`, `columnOf:40`,
`servedColumnTitles:51`, `moveByKeyboard:60`, `itemDetail:65`, `openWork:70`),
`portal-compose-restart.spec.mjs` (`workUrl:8`, `columnOf:9`,
`workItemDetail:14`), plus the two live-daemon specs.

Non-owned importers reached only through the shared module's signatures (they are
not edited, so their call shapes are the compatibility constraint):
`portal-chat.spec.mjs` (six `login(page, "/portal/chat")`, `api`, `createProject`
options object, `attachResource`, `capture`, `uniqueKey`),
`portal-chat-live`, `portal-execution-control`, `portal-goal-managed`,
`portal-live-daemon`, `portal-work-planning`, `portal-work-views`,
`portal-compose-{daemon,restart}`, `portal-connections` (`capture` only). Verified
call shapes at base: no caller of the shared `login` passes a third positional
argument (the only three-argument call is the in-scope `goal-attention:512`,
against that suite's own local copy, migrated here to the `base` option); every
caller of the shared `createProject` passes the options object
(`portal-goal-attention` and `portal-connections` call their own local copies —
`(page, suffix, {blocked})` and a single argument); `capture`'s fifth `options`
argument is already in use outside this slice's set (`work-planning:821` passes
`{ fullPage: false }`), so the shared `options.fullPage ?? true` default is a live
contract, not dead code.

CI pins (frozen): the render gate (`.github/workflows/ci.yml:752-825`) runs seven
specs, `--expect-file` names exactly those seven, and three test titles are pinned
by name. This slice renames no file, adds no file, and changes no title; the
executed-set assertion is untouched.

## Deliverable shape

One shared home: the existing `browser/tests/readable-fixtures.mjs` (imported by
12 specs, and already the module that holds `login`/`api`/`capture`/`createProject`
plus the cutover suite's socket harness `controlSocket`/`readsStatus`). No new
`goal-fixtures.mjs`: contract clause (1) asks for *one* module holding the moved
helpers, and four of the seven helper families already live here.

### 1. `browser/tests/readable-fixtures.mjs` (extended)

- **header** — one paragraph added: the module now also carries the Goal/plan
  revision payload vocabulary and the browser-harness helpers the cutover and
  Goal suites share; no behaviour change.
- **`login(page, path = "/portal/attention", { base = "", assert = [] } = {})`** —
  `base` reproduces `portal-goal-attention`'s origin argument (its rollout-off
  instance); `assert` reproduces `workspace-shell`'s third readiness marker (an
  array of locators, each required visible under the same `LOGIN_GUARD_MS`
  backstop as the two built-in markers). Existing callers keep their shapes:
  `login(page)`, `login(page, "/portal/chat")`.
- **`api(page, method, path, body)`** — unchanged, including the `{status, body}`
  return contract and `body: {}` for an empty body.
- **`capture(page, testInfo, name, observations, options = {})`** — unchanged.
- **`deviceContextOptions(testInfo, { baseURL } = {})`** — new export: the five
  device fields `portal-work-views` copied (viewport, isMobile, hasTouch,
  deviceScaleFactor, userAgent) plus an optional `baseURL` for the caller that
  addresses another origin; work-views' call sites stay byte-identical and keep
  spreading `baseURL` themselves.
- **`recordHistoryWrites()`** — new export, byte-identical to both copies, usable
  as `page.addInitScript(recordHistoryWrites)`.
- **`historyWrites(page)`** — new export, identical.
- **`createProject(page, {key, name, description, defaultAgentProfile, defaultWorkspace, blocked = false})`** —
  additive `blocked`: after the 201 assertion, creates the blocked work item the
  Goal suites assert (`title: \`Blocked work ${key}\``, `status: "ready"`,
  `blocked: true`, `blocker: "Operator review required"`) through the shared
  `createWorkItem` (its own 201 assertion). Return value unchanged (`{id, key, name}`).
- **Goal revision and plan payloads** (new section): `acceptanceContract(description)`
  (the `symmetry.acceptance.v1` literal with the frozen per-suite description as its
  argument), `authorityPolicy()`, `executionPolicy()` (both byte-identical moves),
  `contextManifest()`, `planProposal({ goalId, title, repositoryId, acceptanceDescription })`
  (the `symmetry.plan.v1` proposal: one `implement` item, `change_target: null`,
  the subject baseline `"a".repeat(40)`/`sha256:${"b".repeat(64)}`, the suite's
  acceptance contract inside the item) and `FALLBACK_READ_GUARD_MS` (moved with its
  comment; it bounds the poll over the shared `readsStatus` record).

### 2. `browser/tests/workspace-shell.spec.mjs`

- Delete the local `recordHistoryWrites`, `historyWrites`, `login`, `api`,
  `capture` (`:30-94` minus the `project` helper); import all five from
  `./readable-fixtures.mjs`; drop the now-unused `node:fs/promises` import.
- Add one local preset, `shellLogin(page, path)`: the shared `login` plus this
  suite's readiness marker (`assert: [palette trigger]`) — the marker the deleted
  copy asserted for every authenticated entry. Eight call sites become
  `await shellLogin(page)`. No local function is named `login`.
- Keep the local `project(page, suffix, withWork)` fixture identity
  (`:65-84`), migrating its two `api(page, path, body)` calls to
  `api(page, "POST", path, body)`; its assertions are unchanged.
- `capture` call sites (11) unchanged.

### 3. `browser/tests/portal-goal-attention.spec.mjs`

- Delete the local `login`, `api`, `acceptance`, `authorityPolicy`,
  `executionPolicy`, `contextManifest`, `createProject`.
- Keep the suite's fixture identity as a local preset over the shared helper:
  `goalProject(page, suffix, { blocked = false })` mints the suite's key
  (`H` + five date chars + suffix, upper-cased, 8 chars), passes
  `name: \`Goal workspace ${key}\``, `description: "Real Phoenix fixture"`,
  `defaultAgentProfile: "codex"`, `defaultWorkspace: "primary"` and `blocked` to the
  shared `createProject`. No local function is named `createProject`; the preset
  contains no HTTP call and no assertion. This mirrors the existing precedent in
  `portal-work-planning.spec.mjs:73-78` (a local identity preset over the shared seeder).
- Add `acceptanceDescription = "Operator verifies the goal workspace boundary."`
  (frozen bytes) used at the three `acceptance_contract` sites; `createGoalWithPlan`
  builds its proposal with the shared `planProposal`.
- `attachRepository`, `goalCommand`, `createGoal`, `createGoalWithPlan` stay local
  (different names/payloads/return shapes across the two Goal specs — see the
  not-consolidated table) with their `api` calls moved to the shared signature.
- `login(page, "/portal/attention", goalsOffUrl)` → `login(page, "/portal/attention", { base: goalsOffUrl })`.

### 4. `browser/tests/portal-cutover.spec.mjs`

- Delete the local `acceptance`, `authorityPolicy`, `recordHistoryWrites`,
  `historyWrites`, `FALLBACK_READ_GUARD_MS`, `executionPolicy`, `contextManifest`;
  import the shared helpers and `planProposal`.
- Add `acceptanceDescription = "Operator verifies the cutover boundary."`.
- `createGoalWithPlan` and `goalCommand` stay local; their payloads use the shared
  builders and their `api` calls keep the shared signature (already imported).
- `login`/`api`/`capture`/`createProject` call sites are unchanged.
- `RECONNECT_GUARD_MS` stays local (see decisions).

### 5. `browser/tests/portal-work-views.spec.mjs`

- Delete the local `deviceContextOptions` (`:91-100`); import it from
  `./readable-fixtures.mjs`. Both call sites (`:707`, `:777`) stay byte-identical.

No other file changes. `docs/TODO.md`/`docs/archive/CLOSED-TODO.md` do not move:
the two TODO mentions of this area (`:393`, `:556-560`) sit inside entries of
other goals that are themselves still open at the base revision (`- [ ]`: the
browser-suite re-measurement under host load and the live-daemon pagination
residual) and their text documents past inventories/budgets, not open items about
the helper copies — checked with
`grep -n "browser/tests\|readable-fixtures\|T5:" docs/TODO.md`. The evidence record
is written into this plan file and the run's `answer.md` because the contract's
STRICT owned set names neither `docs/evidence/**` nor any other new file.

## API shape

```js
// browser/tests/readable-fixtures.mjs (new/changed signatures only)
export async function login(page, path = "/portal/attention", { base = "", assert = [] } = {});
export async function createProject(page, { key, name, description = "...", defaultAgentProfile, defaultWorkspace, blocked = false });
export const deviceContextOptions = (testInfo, { baseURL } = {}) => ({ ... });
export function recordHistoryWrites();                    // page.addInitScript target
export const historyWrites = (page) => page.evaluate(...);
export const acceptanceContract = (description) => ({ ... });   // symmetry.acceptance.v1
export const authorityPolicy = () => ({ ... });
export const executionPolicy = () => ({ ... });
export const contextManifest = () => ({ ... });
export const planProposal = ({ goalId, title, repositoryId, acceptanceDescription }) => ({ ... }); // symmetry.plan.v1
export const FALLBACK_READ_GUARD_MS = 20_000;
```

Unchanged: `api`, `capture`, `operatorToken`, `uniqueKey`, `projectUrl`, `workUrl`,
`detailSurface`, `closeDetail`, `createWorkItem`, `updateWorkItem`, `moveWorkItem`,
`attachResource`, `workspace`, `projectItem`, `seedProject`, `servedColumnTitles`,
`snapshotMetrics`, `recordKeys`, `readsStatus`, `controlSocket`.

## Exact edge-case semantics the contract demands

- **The three CI-pinned titles and the seven spec file names are byte-identical**
  (D1). Evidence: the Playwright collection listing before/after, diffed with line
  numbers stripped.
- **Shared `login` keeps accepting a bare path string** and keeps its
  `LOGIN_GUARD_MS` backstop on the Refresh and Project markers, so
  `portal-chat.spec.mjs`'s six `login(page, "/portal/chat")` calls and the five
  other non-owned importers are unaffected. `base`/`assert` are additive options.
- **Shared `api` keeps its `{status, body}` contract with `body: {}` for an empty
  body** and its method-conditional headers; every in-scope call site passes the
  same method/path/body it passed to its deleted copy.
- **`createProject`'s new `blocked` branch asserts exactly what the deleted copy
  asserted** (a 201 from the work-item POST) and returns the same `{id, key, name}`.
  `portal-goal-attention`'s blocked title `Blocked work ${key}` is what two of its
  cases render and assert.
- **The payload bytes are frozen**: `executionPolicy`, `authorityPolicy` and
  `contextManifest` move verbatim; `acceptanceContract` reproduces the two
  copies' objects with the per-suite `description`; `planProposal` reproduces the
  field set, order and values of both proposal literals (`expected_revision: 1`,
  `change_target: null`, `depends_on_keys: []`, the 40-hex/`sha256:`-prefixed
  baseline, `model_profile: "codex"`).
- **`deviceContextOptions` keeps exactly the five device fields and only them**,
  plus `baseURL` when the caller passes one; the caller that spreads its own
  `baseURL` keeps doing so.

## Decisions

- **P1 — one shared home, the existing `readable-fixtures.mjs`.** Options: (A)
  extend the module 12 specs already import (which already holds four of the seven
  families and the cutover socket harness); (B) a new `goal-fixtures.mjs` for the
  Goal/plan payloads. (A) is what clause (1) asks for ("one shared module holds the
  in-scope moved helpers") and keeps every in-scope spec's import list pointing at
  one module; (B) would split *the same* helper families (`createProject`,
  `login`) across two homes or create a module whose consumers all already import
  the other one. Chosen: **(A)**.
- **P2 — `login`'s variants become `{base, assert}` options.** `base` is
  goal-attention's origin; `assert` is workspace-shell's marker list. Alternatives:
  (i) strengthen the shared `login` with the palette assertion for everyone —
  rejected: it changes a helper 12 non-owned specs call, and E8 says an
  unverifiable strengthening is not claimed; (ii) leave workspace-shell's marker
  out — rejected: "a helper may not assert less than the copy it replaced".
  Chosen: the option, used by a one-line preset in the spec that owns the marker.
- **P3 — `api` keeps its signature; the deleted copies' deltas are audited, not
  parameterized.** The deltas (`content-type`/`csrf` on GET, `JSON.parse` vs
  `response.json()` vs `.catch(() => null)` on an unparsable body) change only the
  error class on a *failing* response; every in-scope call site asserts `status`
  before reading a body member, or reads a body the server serves as JSON on that
  path (the 404/409/401 refusals the specs assert include a JSON error document).
  The per-call-site audit is in the control-flow table below; no option is added
  because no call site observably needs one (adding one would be speculative).
- **P4 — `createProject` gains `blocked`; the Goal suite keeps its identity in a
  delegating preset.** Alternatives: (i) make the shared helper mint the
  suffix-keyed/`Goal workspace` identity — rejected: that is one suite's fixture
  identity, and `seedProject` already establishes the opposite convention (the
  caller templates identity over the minted key); (ii) rename the preset
  `createProject` to keep the call sites identical — rejected: it would make the
  contract's definition-count grep read 2 for `createProject`. Chosen: `blocked`
  as the option (the deleted copy's only *mechanic* difference) plus a
  `goalProject` preset that owns the identity.
- **P5 — the policy literals take the frozen description as an argument.**
  `acceptance(description)` is the only variant difference between the two copies;
  passing the description keeps the two frozen strings at their own suite's call
  sites instead of forking the literal. `authorityPolicy`/`executionPolicy`/
  `contextManifest` are parameterless.
- **P6 — `FALLBACK_READ_GUARD_MS` moves; `RECONNECT_GUARD_MS` stays.**
  `FALLBACK_READ_GUARD_MS` is the contract's named move and bounds a poll over the
  shared `readsStatus` record — the same harness `controlSocket`/`readsStatus`
  come from. `RECONNECT_GUARD_MS` bounds this suite's own `page.waitForRequest`
  inside a case body, is a single copy, and is not named by the contract; moving it
  would exceed the named set.
- **P7 — `contextManifest` joins the moved policy-literal family.** It is the
  fourth literal in the same `initial_revision` block, duplicated with identical
  bytes-in-effect across exactly the two specs the contract names; leaving one of
  four siblings behind would keep a copy the slice exists to remove. Recorded as an
  addition to the contract's named set (its bytes are frozen by D1).

## Deliberately not consolidated (recorded, with reasons)

| local helper | where | why it stays |
| --- | --- | --- |
| `project(page, suffix, withWork)` | `workspace-shell.spec.mjs:65` | this suite's own fixture identity (its own key prefix, work-item shape, return value); not a copy of `createProject` |
| `attachRepository` | both Goal specs (`goal-attention:99`, `cutover:83`) | different resource name/`external_ref` identity per suite; not named by the contract |
| `goalCommand` | both Goal specs (`goal-attention:113`, `cutover:158`) | different return shapes (`{read, response}` vs the response) and different call sites; not named by the contract |
| `createGoal` / `createGoalWithPlan` | both Goal specs | different composition (throw-vs-assert, shared `goalCommand` vs inline); only their payload literals move |
| `RECONNECT_GUARD_MS`, `EXPIRY_GUARD_MS`, `EXPIRY_POLL_INTERVALS`, `JOIN_EPOCH_GUARD_MS` | per suite | single-copy suite-local guards, not duplicates |
| `detailPanel`, `focusedCard`, `ui`, `statusNotice`, … | per suite | suite-local vocabulary, not duplicates |

## Evidence plan (what will be run, and what proves what)

All gates foreground. The release-shell harness is not available on this host
(the render gate needs a built release, three instances and PostgreSQL), so the
render-gate run + its executed-set assertion + the render-gate mutations are
**CI-owned** (contract gates row); what is runnable here is run here.

| what | command (from repo root unless noted) | proves |
| --- | --- | --- |
| syntax | `node --check` on every touched `.mjs` | no syntax error in any touched file |
| collection (before/after) | `pnpm --pm-on-fail=ignore --filter symmetry-browser-acceptance exec playwright test --list tests/{workspace-shell,portal-work-views,portal-goal-attention,portal-cutover}.spec.mjs` | the four spec files link through the real Playwright ESM graph with the shared module's new export set; the collected files/titles are unchanged (diff after stripping line numbers); exit code recorded |
| definition census (before/after) | the `grep -nE` command above | each consolidated helper has exactly one definition in the in-scope set |
| import/export consistency | throwaway `node` script (no repo file): every name imported from `readable-fixtures.mjs` is exported, and used | no misspelled/missing/leftover import (CI's render gate is the runtime proof) |
| moved bytes | extraction + `diff` of each verbatim block against `git show HEAD:<file>` | `recordHistoryWrites`, `historyWrites`, `executionPolicy`, `authorityPolicy`, `contextManifest`, `capture` (vs work-views? no: vs workspace-shell), `deviceContextOptions` (five fields) move byte-identically |
| payload equality | a throwaway `node` script that builds each suite's proposal/acceptance/policy objects at HEAD (extracted literals) and with the shared builders, and deep-compares them | the shared builders reproduce the deleted copies' observable payloads exactly |
| proposal contract check | throwaway `node` script: `planProposal(...)` validated against `contracts/v1/plan-proposal.schema.json` (when the offline dependency install succeeds), then one field mutated | the builder's payload is schema-valid and a single-field mutation is refused |
| citation gate | `python3 scripts/ci/check-citation-existence.py` | no citation into `browser/**` breaks (§11) |
| diff hygiene | `git diff --check`; case-insensitive profile-directory grep over the diff | no whitespace damage, no user-profile host path |
| LOC delta (D2, reported) | `git diff --stat`/`--numstat` over the owned set | the `−200…−250` prediction is reported, never gated |

Mutation checks (contract gates row): one per consolidated helper *family whose
removal is observable by a consuming case*, each a CI render-gate run on a
throwaway commit, named here so the conductor can run them:

| family | mutant | expected red source |
| --- | --- | --- |
| `login` | drop the `base` prefix | `portal-goal-attention`'s rollout-off case: without the prefix the entry lands on the goals-on instance and `expect(refused.response.status).toBe(409)` / `error.code === "goal_admission_disabled"` (`:486-487`) fails. The render gate sets `SYMMETRY_GOALS_OFF_URL` and `SYMMETRY_RENDER_GATE_REQUIRED=1`, so the case executes rather than skipping green. Dropping the `assert` loop is not a red mutant: it removes a satisfied wait that no case observes |
| `api` | return `body: null` instead of `{}` for an empty body, or drop the `x-csrf-token` header | every POST case in the four specs (403/parse failure) |
| `capture` | — no deterministic mutant | the deleted `workspace-shell:86` copy was the shared body with `fullPage: true` fixed: it carried no assertion the shared copy lacks. The `expect.poll(scrollWidth <= innerWidth)` guard came *from* the shared copy, and removing a satisfied read-only assertion is observed by no case, so this family's cutover has nothing the render gate can redden on. Recorded as unmutable, not claimed as a passing pin |
| `recordHistoryWrites`/`historyWrites` | record `String(args[0])` or stop filtering the empty URL | `workspace-shell`'s legacy-hash-repair case, `portal-cutover`'s hash-route case |
| `createProject`/`blocked` | skip the blocked work item | `portal-goal-attention`'s attention-merge and rollout-off cases (they assert `Blocked work <key>`) |
| policy literals + `planProposal` | change one field of the built proposal (e.g. `expected_revision: 2`) | `portal-goal-attention`'s plan-approval case / `portal-cutover`'s reconnect case (server refuses the command) |
| `deviceContextOptions` | drop `isMobile`/`hasTouch` | `portal-work-views`' fresh-context deep-link cases at the mobile project |
| `acceptanceContract` | drop a field or alter `description` | the plan-admission cases (server-side contract validation) |

Seven runs, not eight: `capture`'s deleted copy differs from the shared body only
in the `fullPage` default, which changes an attached screenshot's bytes and
nothing any case asserts.

## Out of scope (with reasons)

- Wiring the six unwired families into CI (W6-02, D7); their helper copies stay
  and are the recorded remainder above.
- The jsdom duplicates (W5-08); the ps1 driver scripts; the browser ESLint block
  (W6-01); `playwright.config.mjs`; `browser/scripts/**` node tests; any new spec
  coverage; `ci.yml`/driver edits.
- `frontend/**`, `control/**`, `contracts/**` — untouched (the contract's failure
  controls name a `frontend/**` edit as "not achieved").

## Residual risks / known limits

- The render gate cannot run on this host, so "the seven specs still pass" is
  carried by the PR's CI run (conductor-verified) plus this slice's local
  collection/static evidence; it is not claimed as locally observed.
- The shared `login` now asserts the Project combobox on the rollout-off entry
  (`portal-goal-attention`'s deleted copy asserted only Refresh there): a strictly
  stronger assertion on an instance this host cannot start; CI's render gate covers it.
- `portal-goal-attention`'s key minting moves from inside its deleted `createProject`
  into the `goalProject` preset; the rule is preserved byte-for-byte, but the
  fixture keys are per-run random, so "same key" is not directly observable — the
  preserved *rule* is what the extraction diff pins.
- The `api` deltas (P3) are audited statically per call site; the runtime proof is
  CI's render gate.
- Evidence lives in this plan and the run's `answer.md`: the contract's STRICT
  owned set names no `docs/evidence/**` path.

## Control-flow audit (lesson 32: base bytes -> HEAD)

Every deleted copy's branches, guards, defaults and conditional assertions, and
where each one went. "Carrier checks" are the executed old-vs-new comparisons in
the harness below (`C1`…`C8`), where the deleted bytes and the shared helper are
driven through the identical recording carrier.

| deleted copy (base) | control flow it carried | where it went | preserved |
| --- | --- | --- | --- |
| `workspace-shell:43` `login` | default `path`; three `toBeVisible` waits (Refresh, Project, palette) with the project's default timeout | `login(page, path = "/portal/attention", { assert })` + the suite's `shellLogin` preset; the palette marker is the `assert` entry, the waits keep the shared `LOGIN_GUARD_MS` = the project's 30 s expect timeout | yes: carrier C1a/C1b show the same 3 waits in the same order; the only recorded delta is the explicit `{timeout: 30000}` argument |
| `goal-attention:25` `login` | default `path`, default `base = ""`, one `toBeVisible` wait | `login(page, path, { base })` | yes for `base` (C1c: same URL, same Refresh wait); the shared helper adds the Project wait — strictly more, never less |
| `workspace-shell:52` `api` | fixed `POST`; unconditional `content-type`/`x-csrf-token`; unconditional `JSON.stringify(body)`; `response.json()` uncaught (throw on an empty or non-JSON body) | `api(page, "POST", path, body)`: CSRF for non-GET, `content-type` and body only when a body is passed, `text ? JSON.parse(text) : {}` | yes at every call site (C2a: same method/path/body, same returned `{status, body}`); the delta is an added `Accept`, the case-insensitive header spelling, and the failing-response branch (C2c, audited: every call site asserts `status` first) |
| `goal-attention:32` `api` | conditional body stringify (same as shared); `.catch(() => null)` on `response.json()`; CSRF/content-type on GET too | shared `api` | yes (C2b: identical method/path/body; the GET loses the CSRF/content-type headers, which no portal read enforces); C2c records the failing-response branch |
| `workspace-shell:86` `capture` | overflow poll guard, `fullPage: true`, attach, observation JSON | shared `capture` + `options.fullPage ?? true` | yes (C3: identical poll/screenshot/attach sequence and identical JSON bytes) |
| `workspace-shell:30` / `cutover:40` `recordHistoryWrites` | `args[2] ?? ""` | shared, byte-identical body | yes (C4a/C4b: identical source and identical installed wrapper) |
| `workspace-shell:41` / `cutover:51` `historyWrites` | none | shared, byte-identical | yes (C4c/C4d) |
| `goal-attention:76` `createProject` | default `blocked = false`; 201 assertion; conditional work-item POST + its own 201 assertion; returns `{id, key, name}` under `Goal workspace <key>` | shared `createProject` + `blocked` option; the key minting, name, description and codex/primary bindings stay in `goalProject` | yes (C5 for `blocked` × suffix A/J: identical request sequences and identical return value) |
| `goal-attention:45,51,58,74` policy literals | none (pure literals) | `acceptanceContract(description)`, `authorityPolicy`, `executionPolicy`, `contextManifest` | yes (C6a/c/e/g: identical JSON including key order) |
| `cutover:21,27,61,77` policy literals, `cutover:60` `FALLBACK_READ_GUARD_MS` | none (pure literals / one constant) | same shared literals with the cutover description; the guard moves verbatim | yes (C6b/d/f/h; extraction diff empty except `export`) |
| `cutover:115-142` / `goal-attention:156-177` proposal literals | `crypto.randomUUID()` for `proposal_id`; `expected_revision: 1`; `change_target: null`; the `a`×40 / `sha256:`+`b`×64 baseline | `planProposal({goalId, title, repositoryId, acceptanceDescription})` | yes (C7a/C7b: identical request bytes and identical `proposalJson` modulo the random uuid) |
| `portal-work-views:91` `deviceContextOptions` | five conditional device spreads; no `baseURL` | shared `deviceContextOptions(testInfo, { baseURL })` | yes (C8a shapes 0-2 identical; C8b reproduces `portal-connections`' baked-origin composition for W6-02) |

## Per-helper table (deleted copy -> shared helper + parameters -> call sites)

Definition census: at the base revision the in-scope set (the four CI-run specs +
the shared module, the command's own file set) held **24 definitions** of the
consolidated names — 3 `login`, 3 `api`, 2 `capture`, 2 `createProject`, 2
`recordHistoryWrites`, 2 `historyWrites`, 2 `executionPolicy`, 2 `authorityPolicy`,
2 `acceptance`, 2 `contextManifest`, 1 `deviceContextOptions` (the in-scope
`portal-work-views:91` copy; the `portal-connections:233` copy is E6-excluded and
outside the command's file set), 1 `FALLBACK_READ_GUARD_MS` — with 4 of them
already in `readable-fixtures.mjs`. The two `symmetry.plan.v1` proposal literals
(`cutover:115`, `goal-attention:156`) are inline object literals at the base, so
the census command has no definition line to match; the table below counts them.
At the final tree the same grep finds **13 definitions, all in
`browser/tests/readable-fixtures.mjs` (lines 43, 52, 96, 223, 229, 236,
252, 266, 294, 303, 321, 349, 360)**, and **zero** in the four CI-run specs. The
E6-excluded `portal-connections.spec.mjs` still carries its own `login:109`,
`api:117`, `createProject:132` and `deviceContextOptions:233` — the recorded
remainder for W6-02.

| helper | base copies deleted here | shared definition | call sites at the final tree | assertions moved |
| --- | --- | --- | --- | --- |
| `login` | `workspace-shell:43`, `goal-attention:25` | `readable-fixtures.mjs:43` (`{base, assert}`) | `shellLogin` 8 (workspace-shell), `login` 9 (goal-attention, one with `base`), 8 (cutover, unchanged), 11 (work-views, unchanged) | the palette wait became the `assert` entry |
| `api` | `workspace-shell:52`, `goal-attention:32` | `:52` (signature unchanged) | 3 migrated in workspace-shell to the 3-argument form — 2 in its `project` helper (`:34`, `:38`), 1 in the Attention-scope case (`:177`); goal-attention 25, cutover 7, work-views 2 unchanged | none |
| `capture` | `workspace-shell:86` | `:321` (unchanged) | 11 + 10 + 6 + 13, unchanged | the overflow poll guard came from the shared copy |
| `recordHistoryWrites` / `historyWrites` | `workspace-shell:30/41`, `cutover:40/51` | `:349` / `:360` | 1 + 1 each, unchanged | none |
| `createProject` | `goal-attention:76` | `:96` (`blocked`) | `goalProject` 9 (goal-attention), cutover 5 unchanged, other importers unchanged | the blocked work item's 201 assertion |
| `acceptanceContract` | `goal-attention:45`, `cutover:21` | `:223` (`description`) | 3 (goal-attention), 1 (cutover) | none |
| `authorityPolicy` / `executionPolicy` / `contextManifest` | `goal-attention:51/58/74`, `cutover:27/61/77` | `:229` / `:236` / `:252` | 9 policy calls (goal-attention), 3 (cutover) | none |
| `planProposal` | `cutover:115-142`, `goal-attention:156-177` | `:266` | 1 per Goal spec | none |
| `deviceContextOptions` | `portal-work-views:91` | `:303` (`baseURL`) | 2, unchanged | none |
| `FALLBACK_READ_GUARD_MS` | `cutover:60` | `:294` | 1, unchanged | none |

## Gate results (implement stage + fix r1, 2026-11-21, final tree, foreground)

| gate | command (from the repo root unless noted) | exit | counts / output |
| --- | --- | ---: | --- |
| syntax, five files | `node --check` on `readable-fixtures.mjs`, `workspace-shell.spec.mjs`, `portal-goal-attention.spec.mjs`, `portal-cutover.spec.mjs`, `portal-work-views.spec.mjs` | 0 | one `0` per file |
| collection (before) | `pnpm --pm-on-fail=ignore --filter symmetry-browser-acceptance exec playwright test --list tests/{workspace-shell,portal-work-views,portal-goal-attention,portal-cutover}.spec.mjs` at HEAD | 0 | `Total: 70 tests in 4 files` (both projects) |
| collection (after) | same command on the final tree | 0 | `Total: 70 tests in 4 files`; diff of the two listings after stripping `:line:col` is **empty** — same files, same titles, same order |
| every importer still links | `pnpm --pm-on-fail=ignore --filter symmetry-browser-acceptance exec playwright test --list` (all 13 specs) | 0 | `Total: 168 tests in 13 files` — all nine non-owned importers of `readable-fixtures.mjs` (13 importers at this tree, 12 at base) collect against the new module |
| importer signature compatibility | scripted check: every name each spec imported from `readable-fixtures.mjs` at HEAD is still exported | 0 | 13 importers, 31 exported names at the final tree, no missing name (the aliased `seedProject as seedFixtureProject` form included); no imported name is left unreferenced |
| definition census | the `grep -nE` command in "Measured basis" | 0 | 13 definitions, all shared; 0 in the four specs |
| migration completeness | greps for a 2-argument `api`, a positional `createProject`, a local definition outside the shared module | 0/1 | no 2-argument `api`; no positional `createProject`; the only remaining local definitions are `portal-connections`' four (E6 remainder); the two 3-argument `login` lines at the final tree are the migrated `{ base: goalsOffUrl }` call (`portal-goal-attention:445`) and the `{ assert }` option inside `workspace-shell`'s local `shellLogin` preset (`:29`) |
| browser suite inventory | `python3 scripts/ci/assert-browser-suite-coverage.py` | 0 | `suites=16 wired=7 manual=9 unclassified=0`; `PASS` |
| citation existence | `python3 scripts/ci/check-citation-existence.py` | 0 | `no audited document added or modified between 4d465119546d and 6d0b2c8fabb9; nothing to check` (fix-stage run on the final tree; the audited set is `docs/runbooks`, `docs/design/quality.md`, `docs/archive/CLOSED-TODO.md`, so this plan file is not audited either way) |
| diff hygiene | `git diff --check` | 0 | no whitespace errors |
| DLP | case-insensitive grep over the diff for the profile-directory fragment, a backslash immediately before the letter u, and the profile name (patterns described, never embedded); `git diff origin/main` for the branch's own commits | 1 (no match) | the working diff carries no user-profile path, no JSON-escape sequence, no profile name; the `origin/main` hits are other slices' committed fixture text |
| moved bytes | `diff` of each verbatim block against `git show HEAD:<file>` | 0 | `executionPolicy` (both copies), `authorityPolicy`, `contextManifest`, `recordHistoryWrites`, `historyWrites` differ only by the added `export`; `capture` differs only in `options.fullPage ?? true`; `deviceContextOptions` only in the `baseURL` parameter |
| old-vs-new behaviour | `node --import ./hook.mjs compare.mjs` (throwaway harness outside the repo, `<HOST-PATH>/_w4-14-evidence/harness`) | 0 | **17 PASS / 11 DIFF**, every DIFF a recorded accepted delta (backstop argument, extra Project marker, header spelling/`Accept`/GET headers, harness file names) — `compare.txt` |
| mutation sensitivity | `node --import ./hook.mjs mutation-probe.mjs` | 0 | **9 mutants, 9 detected, 0 survived** — one per consolidated family (`login` assert loop, `login` base, `blocked` branch, proposal field, history-write URL, empty-body branch, `isMobile`, `fullPage`, acceptance description) |
| LOC delta (D2, reported) | `git diff --numstat`/`--shortstat -- browser/tests`, and the `-U0` awk split | — | raw **+225 / −271 = −46**; executable **+168 / −237 = −69**; comment/blank +57 / −34 (apply stage: one dead local deleted) |

Not run on this host (recorded as missing, not passed):

- the render gate itself (7 specs + the executed-set assertion): it needs a built
  release, three release instances and PostgreSQL; this host has no release build
  and the contract's gates row makes the PR's CI run the primary oracle. The local
  collection evidence above proves the four specs still link and register the same
  70 cases; it does not execute a case.
- the seven render-gate mutations named in the Evidence plan (`capture` has no
  observable removal to mutate — recorded there): each is a CI run on a throwaway
  commit (the contract limits them to one per helper family). The local
  carrier-level probe (9/9 detected) proves the comparison evidence is mutant
  sensitive; it is not a substitute for the spec-level red.
- the six E6-excluded manual families: untouched, no oracle (E8); their helper
  copies are the recorded remainder for W6-02.

## Mutation records

Local (throwaway carrier, `<HOST-PATH>/_w4-14-evidence/mutation-probe.txt`): each
mutant is one line of the shared module; "detected" means the old-vs-new
comparison above reports it. A carrier-level detection is not a spec-level red:
the `login` `assert`-loop mutant is reported here while no consuming case observes
that wait, which is why the CI table names only the `base` prefix for that family.

| family | mutant | result |
| --- | --- | --- |
| `login` | `assert` loop dropped | detected (3 waits -> 2) |
| `login` | `base` prefix dropped | detected (rollout-off URL -> local URL) |
| `createProject`/`blocked` | blocked work item skipped | detected (2 requests -> 1) |
| `planProposal` | `expected_revision` 1 -> 2 | detected (payload differs) |
| `recordHistoryWrites` | URL read from `args[0]` | detected (`/portal/projects/x` -> `state`) |
| `api` | empty body `{}` -> `null` | detected |
| `deviceContextOptions` | `isMobile` dropped | detected |
| `capture` | `fullPage` forced false | detected |
| `acceptanceContract` | `description` dropped | detected |

CI-owned (spec level, one per family that has an observable removal, throwaway
commits): the seven mutations in the Evidence plan table, each expected to redden
the named consuming spec in the render gate. The conductor runs them on the PR;
this slice does not commit.

## Comment-policy sweep (2026-11-22)

Module intent removed from `browser/tests/readable-fixtures.mjs` by the 2026-11-22 comment-policy sweep is recorded below (workstream record "ws11-infra-misc").

- `browser/tests/readable-fixtures.mjs` module header (head 6-12): "The payload literals live here
  once; each suite's own fixture identity (its keys, names, acceptance descriptions and plan titles)
  stays at its call sites as parameters" — shared-fixture module design choice.

The sweep's plan is [`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
