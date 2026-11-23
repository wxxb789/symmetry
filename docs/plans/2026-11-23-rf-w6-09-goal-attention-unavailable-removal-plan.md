# rf-w6-09 goal-attention `unavailable` removal — plan

Contract: `goal-rf-w6-09-goal-attention-unavailable-removal.md` (GDD; binding conductor decisions:
D2 goal-member-only / report-only LOC, D1 frozen surfaces, lesson-32 control-flow audit, PR #182
review round 2 design-amendment duty P2-A, W6-reconcile P1 absence assertions, owner comment policy
2026-11-22). Baseline: this worktree's HEAD `55ecd76a` (`ci(browser): give the unwired browser specs
a CI verdict (#260)`, origin/main); every coordinate below was read at that revision. The contract's
own coordinates are from `0fa9b705` (2026-11-21) and are re-derived here as the contract requires.

Dependencies re-checked at `55ecd76a`: A4 (core work-item eligibility) is merged —
`control/lib/symmetry_control/workspaces/work_item_eligibility.ex` is in the tree and
`portal_json.ex`'s `attention/3` region is untouched by it — and W6-02 (browser wiring) is merged:
`portal-goal-attention.spec.mjs` runs in the render gate's 7-spec set (`ci.yml:526`) with its
rollout-off title asserted by the executed-set step (`ci.yml:550`).

## Member-site consumer table (re-derived; action per site)

`goal_attention.unavailable` at `55ecd76a` — every reference in control, contracts, generated,
frontend, browser and daemon, with its disposition:

| # | site | file:line | action |
| --- | --- | --- | --- |
| 1 | projection member | `control/lib/symmetry_control_web/portal_json.ex:123` | removed |
| 2 | projection doc comment | `portal_json.ex:105-113` | rewritten (goal feed cannot be unavailable) |
| 3 | controller doc comment | `control/lib/symmetry_control_web/controllers/portal_api_controller.ex:24-33` | kept (already states the workspace-refusal path; no member claim) |
| 4 | control test, served read | `control/test/.../portal_goal_attention_test.exs:68` | removed from the pattern (P1; absence proof: the served-document case at `:95/:108`) |
| 5 | control test, rollout-off | `:116` | removed from the pattern (P1; absence proof: the served-document case at `:95/:108`) |
| 6 | control test, project refusal | `:261` | removed from the pattern (P1; absence proof: the served-document case at `:95/:108`) |
| 7 | control test, unit-level `goal_result` literal | `:233` | removed (dead input member: `attention/3` reads only `.page`) |
| 8 | schema required list | `contracts/v1/attention-read.schema.json:20` | removed |
| 9 | schema property | `:27-29` | removed (`GoalAttentionFeed` keeps `additionalProperties: true`) |
| 10 | valid fixture (goal feed) | `contracts/fixtures/valid/attention-read.basic.json:3` | removed |
| 11 | valid fixture (project feed) | `:42` | kept (project member frozen) |
| 12-15 | invalid fixtures (3 files, 4 occurrences) | `invalid/attention-read.{malformed-uuid-goal:3,42,non-uuid-goal:3,75,missing-project-blocked:6}` | kept — not owned, additive under `additionalProperties: true`, each file's refusal reason unchanged; recorded, not edited |
| 16 | fixture manifest row | `contracts/fixtures/manifest.json:2486-2495` | unchanged (no member list exists) |
| 17 | generated TS DTO | `contracts/generated/ts/attention-read.ts:18` | regenerated (member gone) |
| 18 | generated Effect graph | `contracts/generated/effect/index.ts:531` | regenerated |
| 19 | generated Go DTO | `daemon/internal/contracts/generated.go:81` | regenerated |
| 20 | embedded schema bundle | `daemon/internal/contracts/schema_bundle.json:3739-` (attention-read) | regenerated |
| 21 | frontend derivation | `frontend/src/features/attention/attention.tsx:40` | removed |
| 22 | frontend prop pass | `:67` | removed |
| 23 | `GoalAttention` prop + notice branch | `:74-88` | removed (branch and prop together) |
| 24 | fixture builder option | `frontend/src/services/api.fixtures.ts:127` | removed |
| 25 | fixture builder assignment | `:158` | removed |
| 26 | frontend served fixture import | `api.fixtures.ts:6` | unchanged (reads the valid fixture, now without the member) |
| 27 | frontend goal-refusal case | `frontend/src/features/goals/goal-attention.test.tsx:63-82` | replaced by the project-notice case |
| 28 | browser member expectations | `browser/tests/portal-goal-attention.spec.mjs:127,136,487` | deleted (review round 1 F6 re-expresses one as the wire-absence assertion at head `:135`) |
| 29 | browser page-level absence check | `:494` base / `:492` head | kept (`toHaveCount(0)` for the Goal refusal heading) |
| 30 | browser case titles / capture captions | `:119,144,437,494,495` | kept byte-identical (the rollout-off title is CI-asserted at `ci.yml:550`) |
| 31 | contracts boundary suite | `contracts/scripts/boundary.test.mjs:313-317` | unchanged (consumes the fixture; asserts rename refusal for live members only) |
| 32 | `services/api.ts` type derivation | `frontend/src/services/api.ts:195-197` | unchanged (follows the regenerated DTO) |

Not a reference: `contracts/fixtures/shell-return-targets.json:157` and
`frontend/src/app/legacy.ts` name the unrelated `?unavailable=` query parameter of the shell
destination redirect (a different feature, out of scope).

## Public surface after the change

- `GoalAttentionFeed` carries exactly `entries` and `next_cursor` (both required); the member is not
  served, not decoded, not rendered and not typeable. `additionalProperties: true` stays, so the
  removal is a served-shape change, not a closed-schema change.
- `ProjectBlockedFeed` and its projection/rendering are byte-identical: `entries` + `unavailable`
  (error branch `%{code, message}` via `Protocol.error_details/1`).
- The controller's `with` chain, the 200-with-Project-refusal behaviour, the Goal entries on a
  failed workspace read and every error code/message stay unchanged (lesson-32 audit, evidence §6).

## Implementation choices the acceptance contract leaves open

1. **Each removed member assertion is dropped in place; the absence proof is the dedicated case.**
   The reconcile P1 requires absence proof (the schema's `additionalProperties: true` cannot): the
   three Elixir patterns drop the member and the served-document case proves it (`refute Map.has_key?`
   at `portal_goal_attention_test.exs:108`); the Go side gets a dedicated test file, the frontend gets
   one focused case — a silent deletion would read as the "weakened assertion" failure control.
2. **Go absence assertion lives in a new file `daemon/internal/contracts/attention_read_test.go`.**
   Binding P1 requires it and no owned file can hold it. The alternatives were to extend
   `schema_test.go` or create this file; the new file keeps the slice's edit surface free of a
   shared file another in-flight slice might touch. Recorded as an owned-set extension (precedent:
   W6-08's REV-01 amendment), and the file is asserted by the same `DecodeSchema` +
   `readContractFixture` helpers the package already uses.
   The assertion is two-legged: the updated fixture (member absent) must decode — which reddens
   while the schema still requires the member — and a re-marshal of the decoded DTO must carry no
   `unavailable` key under `goal_attention`.
3. **Mutation (a) is read per side, not cross-side.** The contract's frontend project-notice test
   forces the refusal through the client fixture builder (`readAttention({projectUnavailable})`), so
   an Elixir-side mutation cannot redden it; the honest pair is: Elixir `project_blocked` error
   branch → control case red; fixture-builder `projectUnavailable` assignment (and separately the
   `attention.tsx` project-notice condition) → frontend case red. Both mutants are run and recorded.
4. **The control test carries three goal-member assertions, not two.** The contract's owned-files
   clause counts two; at `55ecd76a` the sites are `:68`, `:116`, `:261`. All three drop the member
   (leaving one in a pattern would fail the gate); the absence proof is the dedicated case (choice 1).
5. **The unit test's `goal_result` literal drops its dead member** (`:233`): `attention/3` reads only
   `goal_result.page` and the controller passes `%{page: page}`; keeping a member no write path
   produces would leave the projection's input contract misstated.
6. **The three invalid fixtures and the manifest row stay unedited** (sites 12-16): not owned; their
   refusal reasons (missing `project_blocked`, malformed/non-UUID goal identity) are untouched by an
   additive-member removal, and `contracts:check` re-validates them in the same run.
7. **Design amendment placement** (P2-A): `docs/design/protocol.md`'s additive-within-v1 paragraph
   gains the amendment sentence; `docs/design/frontend.md`'s domain-JSON compatibility sentence
   gains its counterpart. Both name the 2026-11-21 owner mandate (no backward compatibility) and
   cite this contract revision's SHA-256, so the authorization is in the design text, not implied.

## Edge-case semantics

- A failed Goal read still answers the mapped protocol status before the projection runs (unchanged:
  `goal_attention/1` in the controller); no path can produce an unavailable Goal feed, so the
  removal deletes a branch no input reaches.
- A failed workspace read still serves HTTP 200 with the Goal entries and the Project refusal
  (`project_blocked.unavailable = %{code: code, message: message}`); the forced-error case takes the
  controller path (unknown project → `ReadModel.workspace/1` error), not a fixture.
- An empty Goal feed now always renders the empty state (the notice branch that used to pre-empt it
  is gone); the notice heading is asserted absent at page level in the frontend case and the browser
  spec.
- Extra members under `goal_attention` (an old server, a stale fixture) remain schema-valid and are
  ignored by the decoder — the reason the explicit absence assertions exist.

## Evidence plan (foreground, serialized; never a measurement harness beside the suite)

1. Baseline: targeted control test green at the frozen base (characterization before any edit).
2. Elixir: absence assertions added → red (member still served) → projection + doc comment edited →
   targeted test green → `mix format --check-formatted`, `MIX_ENV=test mix compile
   --warnings-as-errors`, the attention file, then the web tree.
3. Contracts: fixture member removed → `pnpm contracts:generate` → `pnpm contracts:check` (drift-free;
   21 schemas; 261 fixtures).
4. Go: new absence test → red on the pre-regeneration tree (both legs as reachable) → regenerated
   bundle/DTO → `GOOS=linux go build ./...`, `GOOS=windows go build ./...`,
   `go test -count=1 ./internal/contracts/... ./internal/protocol/...`.
5. Frontend: absence case + project-notice case added → red (fixture still carries the member) →
   `attention.tsx` / `api.fixtures.ts` edited → `typecheck`, `lint`, the file's vitest suite, then the
   full frontend suite.
6. Browser spec: the three member expectation lines deleted (review round 1 F6 re-expresses one as
   the wire-absence assertion); titles, the absence check and captions byte-identical; local render
   gate unavailable on this host (no Docker/release stack) → recorded with the exact CI step
   (`ci.yml` render gate, 7-spec set).
7. Mutants: (a) Elixir `project_blocked` error branch → control red; (a') fixture builder → frontend
   red; (b) schema member re-added without regenerating → `contracts:check` red (drift); (c) the
   frontend Goal notice re-rendered against the deleted member → typecheck red; (d) the member
   re-added to the valid fixture → Go + frontend absence assertions red while `contracts:check`
   stays green (the P1 proof).
8. `git diff --check`; report-only dLOC delta; DLP scan (no user-profile host path in the diff; no
   backslash-u escape sequence in committed text).

## Out of scope (with reason)

- `ProjectBlockedFeed` anywhere (schema, projection, generated DTOs, rendering, fixtures): D1 frozen.
- The default `:internal_error` flip and any `error_details` mapping change: W5-05's and a future
  approved slice (`protocol.ex` not owned).
- Other attention members, other schemas and fixtures, the generator scripts, `services/api.ts`
  internals, the browser wiring (W6-02), the 1.1 s sleeps in the attention pagination test (W6-12).
- The three invalid fixtures that carry the member: not owned; recorded in the consumer table.

## Residual risks and limits

- The render gate executes only in CI (this host has no Docker daemon, no staged release, no WSL
  browser carrier); the edited browser assertions (the F6 wire-absence assertion included) are
  typed/edited but not executed locally — M8 is their local equivalent; the CI render gate is the
  oracle, and the contract names it a conductor-verified slow oracle.
- Linux-only executions behind the Go gate are build-only on this host; the package suites run
  natively.
- `contracts:check` cannot prove member absence (schema `additionalProperties`), which is exactly
  why the three explicit absence assertions (Elixir, Go, frontend) carry that proof and why mutant
  M6 is run to show they are load-bearing.

## Outcome on the final tree (2026-11-23)

- Re-derivation corrections against `55ecd76a`, all carried into the change: the control test held
  **three** goal-member assertions (`:68`, `:116`, `:261`), not two; the four invalid-fixture
  occurrences stay unedited (not owned, additive, refusal reasons unchanged); the Go absence
  assertion needed a new file (`daemon/internal/contracts/attention_read_test.go`, 40 L) — the
  owned-set extension the reconcile P1 implies — and the two design files gained the P2-A amendment
  (contract SHA-256 cited in both).
- Mutation (a) is per side, as recorded in "Implementation choices" 3: M1 reddens the control
  cases, M2/M3 redden the frontend project-notice case. The contract's sentence that the Elixir
  branch mutation reddens the frontend test cannot hold by construction — the frontend test forces
  its refusal through the client fixture builder, not through the server.
- Gate results (final tree, re-run after every mutant was reverted; exact commands and exit codes in
  the evidence record §7): `pnpm contracts:generate` 0, `pnpm contracts:check` 0 (21 schemas,
  261 fixtures, drift-free), `mix format --check-formatted` 0, `MIX_ENV=test mix compile
  --warnings-as-errors` 0, the attention file 0 (`14 passed`), the web tree 0 (`291 passed,
  1 skipped` — the pre-existing Go-HTTP-recovery environment skip), frontend typecheck 0, lint 0
  (219 warnings under the pinned 229 cap), the attention file 0 (`30 passed`), the full frontend
  suite 0 (`27` files, `633 passed`), the frontend build 0, browser lint 0, `go test -count=1
  ./internal/contracts/... ./internal/protocol/...` 0, both GOOS builds 0, `git diff --check` 0.
- Mutants M1-M8 measured red exactly as §8 of the evidence records; M8 is the browser assertion's
  local equivalent (the member re-added → the spec's own matcher fails on the served projection);
  M6 additionally shows `contracts:check` and the Go test stay green when the member is re-added to
  the fixture only — the schema cannot prove absence, which is why the explicit assertions exist.
- Report-only LOC (D2): tracked diff +104/−81 (net +23) over 15 files; the member-removal subset is
  −39 dLOC (27/66), nine below the plan row's −30 (the review-round-1 doc compression and the added
  absence assertion); new files add 40 (Go test) + 196 (plan) + 288 (evidence) lines.
- Host deviation recorded and reverted: the four feed-unavailable dev pins were downgraded for the
  local install only; `frontend/package.json` and `pnpm-lock.yaml` are back to HEAD's bytes
  (`371e97c3…` / `b9575212…`) and absent from `git status`.
- Review round 1 (2026-11-23): F1 sizes recomputed at the delivered tree; F2 the `@doc` and the Go
  comment compressed to one line each; F5 the W4-11 `goalUnavailable` line reworded; F6 the browser
  wire-absence assertion added (`:135`, local equivalent mutant M8); F3/F4 ratifications recorded.
  Commands and counts: evidence record §8/§11.
