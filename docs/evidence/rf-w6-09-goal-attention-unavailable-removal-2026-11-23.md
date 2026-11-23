# rf-w6-09 goal-attention `unavailable` removal — evidence

Contract: `goal-rf-w6-09-goal-attention-unavailable-removal.md` (GDD; binding decisions D1/D2, the
lesson-32 control-flow decision, the PR #182 review round 2 design-amendment duty P2-A, the
W6-reconcile P1 absence assertions, owner comment policy 2026-11-22). Contract revision SHA-256:
`b39af1298b29d94392ba7ac0686a7d00973524da07694ea08a9a4d8829ded929`. Plan:
`docs/plans/2026-11-23-rf-w6-09-goal-attention-unavailable-removal-plan.md`. Dispatch baseline:
`55ecd76a61c26092b2bf0e97cbcb36d410cf5f22` (this worktree's HEAD, origin/main at dispatch).
Delivered base: `e13450bc03499a62e77b125ac02596cdb433ce05` (origin/main; merged by the branch at
`f9ca993d`). A commit cannot name its own sha, so the delivered head sha is recorded outside the
tree (GDD ledger); the delivered tree is bound by the six artifact hashes in section 9. Every
coordinate below was read at the dispatch baseline or at the change recorded beside it.

## 1. Method and toolchain

Elixir gates run through the portable toolchain (`<HOST-PATH>/.codex/tools/symmetry/erlang-29.0.6` +
`elixir-1.20.4`, invoked as `mix.bat` from a `cmd.exe /d /c` shim with that PATH, because the host's
mise shims win `erl`/`mix` resolution otherwise) with a dedicated test database
(`POSTGRES_DB=symmetry_control_test_w609`) and `SYMMETRY_CONTRACTS_DIR` / `POSTGRES_PORT` /
`PHX_SERVER` unset (the harness environment carried stale values for a different worktree). The
repository-default `symmetry_control_test` database carried stale migration state (`42P07
duplicate_table` on `project_resources_connected_identity`, the quality.md recovery case) and was
left untouched; the dedicated database is created and migrated by the `test` alias's own steps.

Node gates run as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile --config.verify-deps-before-run=false
...`. The host's reachable registry (`packagefeedproxy.microsoft.io`) does not serve four dev pins
HEAD carries (`msw 3.0.7`, `@vitest/coverage-v8 5.1.1`, `knip 6.46.1`, `typescript-eslint 8.73.0`;
feed latest 3.0.6 / 5.1.0 / 6.45.1 / 8.72.2), so `--frozen-lockfile` fails with
`ERR_PNPM_META_FETCH_FAIL` — the host deviation W6-07 recorded. `frontend/package.json` and
`pnpm-lock.yaml` were restored to HEAD's bytes afterwards (§9); the install linked in 9.1 s from the
content-addressable store, and `frontend/node_modules/vitest` and
`@testing-library/jest-dom`'s resolution both land on the single `vitest@5.1.0 …d22a4b15…` variant
(the two-variant hazard W6-07 recorded did not recur).

Go gates run with the host's Go 1.27.4, the CI pin (`go.mod` `go 1.27`).

## 2. Consumer table (member site → file:line → action)

`goal_attention.unavailable` at `55ecd76a`:

| # | site | file:line | action |
| --- | --- | --- | --- |
| 1 | projection member | `control/lib/symmetry_control_web/portal_json.ex:123` | removed |
| 2 | projection doc comment | `portal_json.ex:109-113` | rewritten (the feed cannot be unavailable) |
| 3 | controller doc comment | `control/lib/symmetry_control_web/controllers/portal_api_controller.ex:24-33` | kept, file untouched (it names the workspace-refusal path, no member) |
| 4 | control test, merged read | `portal_goal_attention_test.exs:68` | removed from the pattern |
| 5 | control test, rollout-off | `:116` | removed from the pattern |
| 6 | control test, project refusal | `:261` | removed from the pattern |
| 7 | control test, `goal_result` literal | `:233` | removed (dead input member: `attention/3` reads only `.page`) |
| 8 | schema required list | `contracts/v1/attention-read.schema.json:20` | removed |
| 9 | schema property | `:27-29` | removed (`GoalAttentionFeed` keeps `additionalProperties: true`) |
| 10 | valid fixture, goal feed | `contracts/fixtures/valid/attention-read.basic.json:3` | removed |
| 11 | valid fixture, project feed | `:42` | kept (project member frozen) |
| 12-15 | invalid fixtures (3 files, 4 occurrences) | `invalid/attention-read.{malformed-uuid-goal:3,42, non-uuid-goal:3,75, missing-project-blocked:6}` | kept, not owned — additive under `additionalProperties: true`; each file's refusal reason (missing `project_blocked`, malformed/non-UUID identity) is untouched and `contracts:check` re-validated the corpus in the same run (261 fixtures) |
| 16 | fixture manifest row | `contracts/fixtures/manifest.json:2486` | unchanged (no member list exists) |
| 17 | generated TS DTO | `contracts/generated/ts/attention-read.ts:18` | regenerated |
| 18 | generated Effect graph | `contracts/generated/effect/index.ts:531` | regenerated |
| 19 | generated Go DTO | `daemon/internal/contracts/generated.go:81` | regenerated |
| 20 | embedded schema bundle | `daemon/internal/contracts/schema_bundle.json:3739` region | regenerated |
| 21 | frontend derivation | `frontend/src/features/attention/attention.tsx:40` | removed |
| 22 | frontend prop pass | `:67` | removed |
| 23 | `GoalAttention` prop type + notice branch + its empty-state pre-emption | `:74-88` | removed |
| 24 | fixture builder option | `frontend/src/services/api.fixtures.ts:127` | removed |
| 25 | fixture builder assignment | `:158` | removed |
| 26 | frontend served-fixture import | `api.fixtures.ts:6` | unchanged (reads the valid fixture, now without the member) |
| 27 | frontend goal-refusal case | `frontend/src/features/goals/goal-attention.test.tsx:63` | replaced by the project-notice case at `:72`; absence pinned by the new case at `:63` |
| 28 | browser member expectations | `browser/tests/portal-goal-attention.spec.mjs:127,136,487` base | the three `.toBe(null)` expectations deleted; one wire-level absence assertion added at head `:135` (`not.toHaveProperty("unavailable")`, review round 1 F6) |
| 29 | browser page-level absence check | `:494` base / `:492` head | kept (`toHaveCount(0)` on the Goal refusal heading) |
| 30 | browser case titles + capture captions | `:119,144,437,495` base | byte-identical base→head (`:119`→`:119`, `:144`→`:143`, `:437`→`:436`, `:495`→`:493`); the rollout-off title stays CI-asserted at `ci.yml:550` |
| 31 | contracts boundary suite | `contracts/scripts/boundary.test.mjs:313-317` | unchanged (consumes the fixture; renames only live members) |
| 32 | `services/api.ts` type derivation | `frontend/src/services/api.ts:195-197` | unchanged (follows the regenerated DTO) |

The only production reference to the member was the projection itself (site 1). The compile-side
proofs: the frontend typecheck covers sites 21-23, and `contracts:check`'s generator drift check
covers sites 17-20. The three invalid-fixture occurrences (sites 12-15) are recorded, not edited:
their failure reasons are elsewhere and the schema stays open to additional members.

## 3. Red observations before the change (per side)

1. **Go, DTO shape leg.** New test only, nothing else edited:
   `go test ./internal/contracts/ -run TestAttentionReadServesNoGoalUnavailableMember -count=1`
   → **FAIL, exit 1**: `attention_read_test.go:37: the Goal attention feed carries an unavailable
   member: {"goal_attention":{…,"next_cursor":null,"unavailable":null},…}`.
2. **Go, fixture leg.** With the valid fixture edited (member removed) and the schema/bundle not yet
   regenerated: same command → **FAIL, exit 1**: `attention_read_test.go:18: the served attention
   read did not decode: validate attention-read schema: jsonschema:
   '/goal_attention' does not validate with …#/properties/goal_attention/$ref/required: missing
   properties: 'unavailable'`. The member-less served fixture — exactly what the wire change
   requires — was rejected by the embedded bundle before regeneration.
3. **Elixir.** With the three pattern lines dropped, the dead literal dropped, and the dedicated
   absence case added: the targeted file → **13/14 passed, 1 failed** (`exit 2`), the served document
   printing `"unavailable" => nil` under `goal_attention` at `portal_goal_attention_test.exs:108`.
4. **Frontend.** With the absence case + project-notice case added and the builder/projection
   untouched: `vitest run src/features/goals/goal-attention.test.tsx` → **1 failed | 29 passed (30)**
   (`exit 1`), `AssertionError: expected true to be false` at `goal-attention.test.tsx:68` — the goal
   feed carried the member. The re-expressed project-notice case was **green on the same run**
   before any source edit: the surviving project branch is unchanged behaviour, characterized before
   the retarget.
5. **Baseline (frozen base, before any edit):** the targeted control file → **12 passed**, exit 0.

## 4. The change

- Projection (`portal_json.ex`): the goal member and its doc-comment claim removed (+2/−12 vs base;
  the `@doc` is one line, review round 1 F2); both `project_blocked/1` clauses byte-identical
  (measured: base 129-140 == head 119-130).
- Control test (+38/−5): 3 pattern lines + 1 dead literal dropped; two cases added —
  `attention serves no unavailable member on the Goal feed` (controller path, real project + Goal,
  `refute Map.has_key?(response["goal_attention"], "unavailable")`) and
  `attention serves the Goal entries when the workspace read is refused` (unit path: non-empty Goal
  page + `{:error, :not_found}` → entries and cursor preserved, `project_blocked.unavailable ==
  %{code: "not_found", message: "resource was not found"}`).
- Schema (+2/−5) and valid fixture (−1): member removed only; the `ProjectBlockedFeed` required list,
  its property, `ReadUnavailable` and the fixture's project feed are byte-identical (measured by JSON
  comparison of both definitions against HEAD).
- Regenerated, no hand edits (`pnpm contracts:generate`, exit 0): `contracts/generated/ts/attention-read.ts`
  (+8/−9), `contracts/generated/effect/index.ts` (+1/−2), `daemon/internal/contracts/generated.go`
  (+9/−10), `daemon/internal/contracts/schema_bundle.json` (+1/−12). The generator's only other
  movement is the deterministic relocation of the `ReadUnavailable` definition/interface, now
  referenced only by `ProjectBlockedFeed`.
- Frontend (`attention.tsx` +3/−10, `api.fixtures.ts` −2, test file +17/−9): `attention.tsx` loses the
  derivation, the prop pass, `GoalAttention`'s `unavailable` prop, the Goal notice branch and the
  notice's pre-emption of the empty state; `api.fixtures.ts` loses the `goalUnavailable`
  option/assignment; the goal-refusal case becomes the project-notice case (notice heading, mapped
  message and code, the still-served Goal entry, no Goal refusal heading) plus the new served-document
  absence case.
- Go: new `daemon/internal/contracts/attention_read_test.go` (40 lines; one comment line after
  review round 1 F2) — the owned-set extension the reconcile P1 requires (ratified in review round 1
  F3; plan §"Implementation choices" 2).
- Design amendments: `docs/design/protocol.md:44-54` (+12) and `docs/design/frontend.md:64-71` (+9)
  record the member removal as the program's one non-additive v1 change under the 2026-11-21 owner
  mandate; both cite the contract revision SHA-256.
- Browser spec (+1/−3): the three member expectations deleted and one wire-level absence assertion
  added at `:135` (`not.toHaveProperty("unavailable")` on the served `goal_attention`, review round
  1 F6); titles, the page-level absence check and the capture captions byte-identical; `node --check`
  and the browser lint pass.

## 5. Control-flow preservation audit (base → HEAD; lesson 32)

| surface | base | HEAD | preserved |
| --- | --- | --- | --- |
| `project_blocked({:error, reason})` | `portal_json.ex:137-140`, `unavailable: %{code, message}` from `Protocol.error_details/1` | head `127-130`, byte-identical | yes (measured) |
| `project_blocked({:ok, model})` | `:129-135` | head `119-125`, byte-identical | yes (measured) |
| `ProjectBlocked` notice condition + body | `attention.tsx:103-110` | byte-identical function (extracted base vs head, `diff` empty) | yes |
| `GoalAttention` notice condition + body | `attention.tsx:74-88` | removed with the member (no input reaches it) | removed by design |
| controller `with` chain + `goal_attention/1` mapping | `portal_api_controller.ex:34-72` | file not modified (`git status` lists it not at all) | yes |
| 200-with-Project-refusal on a failed workspace read | controller test `:276` + new unit case | controller test asserts 200 + the `%{code, message}` map; the new unit case adds the non-empty Goal entries beside the refusal | yes, strengthened |

## 6. Served-document evidence

- **Absent goal member, controller path:** `attention serves no unavailable member on the Goal feed`
  reads `/portal/api/attention?project_id=<real project>` (200, the real Goal entry served) and
  asserts the served `goal_attention` object has no `unavailable` key. Green in §7.
- **Project member forced error, controller path:** `attention states an unavailable project feed
  while the Goal feed stays served` reads the endpoint with an unknown project UUID → 200 with
  `project_blocked.unavailable == %{code: "not_found", message: <mapped>}` and
  `goal_attention.entries == []`; the follow-up read of the known project serves its blocked item.
  Unchanged case, still green.
- **Project member forced error, unit path (non-empty Goal feed):** `attention serves the Goal
  entries when the workspace read is refused` composes `PortalJSON.attention(project_id,
  %{page: %{entries: [entry], next_cursor: "cursor"}}, {:error, :not_found})` and asserts the same
  map shape plus the preserved entry — the live refusal path with the Goal half non-vacuous.
- **Frontend:** the project-notice case forces `readAttention({ projectUnavailable: {code:
  "not_found", message: "resource was not found"} })` and asserts the notice heading, the message,
  the code, the served Goal entry beside it and no Goal refusal heading; the absence case asserts the
  served document's `goal_attention` has no `unavailable` key while `project_blocked` keeps it.
- **Go:** `TestAttentionReadServesNoGoalUnavailableMember` decodes the served fixture against the
  embedded bundle and re-marshals the DTO: no `unavailable` key under `goal_attention`, `entries`
  present.
- **Browser:** the three member expectations are deleted and one wire-level absence assertion is
  added on the served `goal_attention` (`served.body.goal_attention`
  `not.toHaveProperty("unavailable")`, spec `:135`, the primary case of the render gate's executed
  set); the page-level absence check (`Goal attention is unavailable on this server` → count 0)
  stays in the rollout-off case that the render gate's executed-set assertion names (`ci.yml:550`).
  The spec runs only under the CI render gate; its local equivalent is M8 (§8).

## 7. Gate table (foreground, serialized; exact commands and exit codes)

Every gate was run at least twice: once during implementation and again on the frozen final tree
(§9) after the mutation runs were reverted; the values below are the final-tree runs unless noted.

| gate | command | exit | counts / output |
| --- | --- | --- | --- |
| contracts generate | `pnpm contracts:generate` | 0 | `Generated contract DTOs.` |
| contracts check | `pnpm contracts:check` | 0 | 35 node tests pass; `Generated contract DTOs are up to date.`; `Validated 21 Draft 7 schemas and 261 fixtures.`; `Effect decoded all 261 raw fixtures; 246 parsed documents reached Schema validation` |
| elixir format | `mix format --check-formatted` | 0 | no output |
| elixir compile | `MIX_ENV=test mix compile --warnings-as-errors` | 0 | clean (1 file recompiled at the change) |
| elixir attention file | `mix test test/symmetry_control_web/controllers/portal_goal_attention_test.exs` | 0 | `14 passed` (12 base + 2 new), 4.7 s; red baseline inside, see §3.3 |
| elixir web tree | `mix test test/symmetry_control_web/` | 0 | `291 passed, 1 skipped`, 33.8 s. The skip is the environment-gated `committed harness session receipts recover through real Go HTTP after listener restarts` (pre-existing, unrelated) |
| frontend typecheck | `pnpm --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit`, no output |
| frontend lint | `pnpm --filter @symmetry/frontend lint` | 0 | `219 problems (0 errors, 219 warnings)` — under the pinned `--max-warnings 229` cap, not raised |
| frontend attention file | `vitest run src/features/goals/goal-attention.test.tsx` | 0 | `30 passed` (29 base cases + the new absence case; the replaced case keeps its slot), 18-20 s |
| frontend full | `pnpm --filter @symmetry/frontend test` | 0 | `27 passed` files, `633 passed` tests, 32.9 s |
| frontend build (staging for the web tree) | `pnpm --filter @symmetry/frontend build` | 0 | built in 1.10 s; assets staged (gitignored) |
| browser lint | `pnpm --filter symmetry-browser-acceptance lint` | 0 | `eslint . --max-warnings 0`, clean |
| browser spec syntax | `node --check browser/tests/portal-goal-attention.spec.mjs` | 0 | syntax ok |
| go test | `go test -count=1 ./internal/contracts/... ./internal/protocol/...` | 0 | `ok internal/contracts 1.694s`, `ok internal/protocol 2.538s` |
| go format | `gofmt -l .` (daemon/) | 0 | no files listed |
| go vet | `go vet ./...` (daemon/, host windows/amd64) | 0 | no output |
| go build linux | `GOOS=linux go build ./...` | 0 | clean (regenerated `generated.go` compiles) |
| go build windows | `GOOS=windows go build ./...` | 0 | clean |
| whitespace | `git diff --check` | 0 | clean |

## 8. Mutation runs (judging)

All mutants were applied with the patch tool, measured, and reverted; the reverts are byte-exact
(§9). "Red" below means the named case failed with the named assertion. The contract gate's
mutation (a) is read per side (review round 1 F4): M1 is the control side, M2/M3 the frontend side;
the frontend project-notice case forces its refusal through the client fixture builder, so no
single server-side mutation reddens both sides by construction.

| # | mutation | target case(s) | measured |
| --- | --- | --- | --- |
| M1 | `portal_json.ex` `project_blocked({:error, reason})` → `%{entries: [], unavailable: nil}` | control project-refusal case + the new unit case | `mix test` attention file → **exit 2, `12/14 passed`**: `1) test attention states an unavailable project feed while the Goal feed stays served`, `2) test attention serves the Goal entries when the workspace read is refused` |
| M2 | `api.fixtures.ts` project assignment → `unavailable: null` (the forced refusal dropped) | frontend project-notice case | `vitest … -t "states the project read refusal"` → **exit 1, `1 failed | 29 skipped (30)`**: `Unable to find role="heading" and name "The project read is unavailable"` |
| M3 | `attention.tsx` ProjectBlocked notice condition → `{false && …}` | frontend project-notice case | same command → **exit 1, `1 failed | 29 skipped (30)`**, the same missing-heading assertion: the surviving branch is pinned by a test, not by prose |
| M4 | schema member re-added, no regeneration | `pnpm contracts:check` | **exit 1**: `fixture attention-read.basic expected schema_valid=true, got false: /goal_attention must have required property 'unavailable'` (the schema/fixture leg fires first) |
| M4b | schema member re-added **and** fixture member re-added, no regeneration | `pnpm contracts:check` | **exit 1**: `generated file is out of date: contracts\generated\ts\attention-read.ts`, `generator check exited with 1` — the drift backstop |
| M5 | the frontend Goal notice restored (derivation + prop + notice branch) against the deleted member | `pnpm --filter @symmetry/frontend typecheck` | **exit 1**: `src/features/attention/attention.tsx(67,44): error TS2322: Type '{} | null' is not assignable to type '{ code: string; message: string; } | null'` — the deleted member no longer yields the typed refusal, so a retained notice cannot compile (the "no live consumer" proof) |
| M6 | member re-added to the valid fixture only | frontend absence case + `contracts:check` + Go test | frontend `vitest … -t "serves the Goal feed with no unavailable member"` → **exit 1**, `AssertionError: expected true to be false`; `pnpm contracts:check` → **exit 0** (`Generated contract DTOs are up to date.`); `go test -run TestAttentionRead…` → **exit 0**. The schema's `additionalProperties` hides the member from every validator — the explicit absence assertions are the load-bearing pins (the P1 demonstration) |
| M7 | member restored in the schema **and regenerated** (the DTO regains the field) | Go absence test | `go test -run TestAttentionReadServesNoGoalUnavailableMember` → **exit 1**: `attention_read_test.go:18: … missing properties: 'unavailable'` — the committed member-less fixture rejects the regenerated contract, so the wire change cannot be reverted silently |
| M8 | the removed goal member re-added to the projection (`portal_json.ex` goal feed → `unavailable: nil`, review round 1 F6) | the browser spec's new wire-level absence assertion, exercised locally as its own equivalent (the spec itself runs only under the CI render gate) | **exit 1, red**: the Elixir served-document case `attention serves no unavailable member on the Goal feed` → `mix test` attention file **exit 2, `13/14 passed`**; the projected `goal_attention` document on the mutant tree (`{"unavailable":null,"entries":[],"next_cursor":null}`, `mix run --no-start` probe) fails the spec's exact expression `expect(document).not.toHaveProperty("unavailable")` run with `@playwright/test`'s `expect` (**exit 1**); after the byte-exact revert the probe yields `{"entries":[],"next_cursor":null}` and the same matcher holds (**exit 0**), the file 14/14 |

The mutation is not applied by editing a generated file by hand (the failure control forbids that);
M7 takes the real regression path (schema edit → regenerate), and the drift backstop (M4b) covers the
un-regenerated variant. M7's `:18` line reference predates review round 1 F2 (removing two comment
lines shifts the same failure to `:16`); the recorded output is the stage's raw quote.

## 9. Scope, DLP, restore and report-only figures

- Diff scope: 15 tracked files (+104/−81, net +23) and 3 new files — `git status --porcelain` lists
  exactly the owned set plus `docs/design/{protocol,frontend}.md`, `daemon/internal/contracts/attention_read_test.go`
  (owned-set extension, ratified in review round 1 F3),
  `docs/plans/2026-11-22-rf-w4-11-frontend-test-harness-plan.md` (one line, ratified in review round
  1 F5), this slice's plan and this evidence record. The member-removal subset (projection, schema,
  fixture, 4 generated files, frontend fix, browser spec) is **−39 dLOC** (27 insertions / 66
  deletions), nine lines below the plan row's −30 because review round 1 compressed the projection
  `@doc` to one line (F2, −9 net) and added the wire-absence assertion (F6, +1); the positive total
  comes from the added clause tests (+38/−5 control, +17/−9 frontend) and the two design amendments
  (+21).
- New-file sizes (`wc -l` at the delivered tree): Go test 40 L, plan 196 L, evidence 288 L.
- Host dependency tree: `frontend/package.json` and `pnpm-lock.yaml` restored to HEAD's bytes
  (sha256 `371e97c389f5fe61…` and `b9575212d79edd4f…`, re-measured after the restore) and absent
  from `git status`; the temporary feed-version lockfile is host-local and CI installs from HEAD's
  lockfile.
- Delivered artifact hashes (sha256 prefixes, for the review): `contracts/generated/effect/index.ts`
  `583d3efa7cf1e8a7`, `contracts/generated/ts/attention-read.ts` `f88be21395285eb3`,
  `daemon/internal/contracts/generated.go` `f6d48bcf7851f1db`,
  `daemon/internal/contracts/schema_bundle.json` `a86650aad7624f7d`,
  `contracts/v1/attention-read.schema.json` `f6fe461abea7c7be`,
  `contracts/fixtures/valid/attention-read.basic.json` `2792b059f0395869`.
- Revert exactness: after M7's regenerate-and-revert the four generated files diff at exactly the
  same shape (effect +1/−2, ts +8/−9, go +9/−10, bundle +1/−12) as the delivered state, and the
  whole gate set in §7 re-ran green on the frozen tree; `attention.tsx`, `portal_json.ex`, the
  schema and the fixture each show exactly their intended hunks.
- DLP: no user-profile host path and no backslash-u escape sequence in the diff or the three new
  files (both scans return zero matches); no gdd-state path in the diff or the Go test.

## 10. Residual risks and limits

- The CI render gate is the only execution of the edited browser spec (this host cannot run the
  release-shell harness: no Docker daemon and no three-instance release/fixture stack — the
  conductor-verified slow oracle the contract names). Local surrogates: `node --check` on the spec,
  the browser lint (`--max-warnings 0`), the jsdom project-notice case that asserts the same
  page-level absence of the Goal refusal heading, and M8 (the served projection document fed to the
  spec's own matcher, §8).
- The host dependency-tree deviation (§1) is host-local; CI installs from HEAD's lockfile.
- `contracts:check` cannot prove member absence (schema `additionalProperties`); M6 is the measured
  demonstration that the explicit absence assertions are load-bearing, and M7 that the regenerated
  Go DTO is pinned by a test rather than only by the drift check.
- The frontend ~1.1 s pagination sleeps and the other W6-12 targets in the same attention test file
  are untouched (out of scope by contract).

## 11. Review round 1 fixes (2026-11-23, PR #264)

- F1: the record sizes are recomputed at the delivered tree (`wc -l`): Go test 40, plan 196,
  evidence 288; the plan's figures read the same numbers.
- F2: the `attention/3` `@doc` and the Go test comment are one line each (≤2); nothing else moved.
- F3: `daemon/internal/contracts/attention_read_test.go` joins the owned set.
- F4: the contract's mutation (a) is recorded per side (M1 control, M2/M3 frontend), see §8.
- F5: the W4-11 plan line now reads the project-refusal case (`projectUnavailable`) instead of the
  removed `goalUnavailable` refusal.
- F6: the browser spec's wire-level absence assertion at `:135` — see §6 and mutant M8 (§8).
- Gates re-run on the fixed tree: `mix format --check-formatted` 0; `mix test
  test/symmetry_control_web/controllers/portal_goal_attention_test.exs` 0 (14 passed);
  `node contracts/scripts/generate.mjs --check` 0; `tsc --noEmit` 0; `vitest run
  src/features/goals/goal-attention.test.tsx` 0 (30 passed); `go test -count=1
  ./internal/contracts/...` 0; `gofmt -l .` clean; `node --check` on the spec 0; browser `eslint .
  --max-warnings 0` 0; `git diff --check` clean.
