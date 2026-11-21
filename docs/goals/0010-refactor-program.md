# 0010 - Refactor program

Status: **active** — started 2026-11-21 under the owner's mandate, recorded mid-program (wave W1
complete; W2 mostly merged, with five PRs in review and three slices in flight).
**Owner-authorized amendable** under the three-tier policy below: T1 status refreshes are
conductor-only and logged; T2 scope, slice, Done-when and decision changes take effect only after
an independent review recorded in the [Amendment log](#amendment-log); and T3 closes only on an
independently reviewed closure record plus the owner's verdict — no model declares completion.

## Goal

Deliver the owner's 2026-11-21 mandate: a thorough refactor and quality uplift of the whole
repository, run as one program of independently mergeable slices. Anything is allowed — protocol
and schema changes without backward compatibility, framework swaps, new packages — when it
measurably improves robustness, performance or engineering quality, or greatly reduces LOC.
The 2026-11-21 mandate (no backward compatibility) is itself the owner-approved amendment of the
additive-only v1 rule (`docs/design/protocol.md:34-37`, with the frontend compatibility note in
`docs/design/frontend.md`), and it permits non-additive protocol changes for the two named
slices only — W6-08 and W6-09 — each of which amends `docs/design/protocol.md` (and
`frontend.md` where relevant) in the same PR. The
program plan is the synthesis snapshot at base `a3039f0`
([docs/plans/2026-11-21-refactor-program-plan.md](../plans/2026-11-21-refactor-program-plan.md));
per-slice contracts live outside the repository under the GDD state directory and supersede the
plan's numbers, line references and owned-file sets wherever they differ. This goal completes —
and archives — only when its Done-when closes.

## Non-negotiable invariants

- PostgreSQL owns durable control state; Elixir owns transition policy; Go owns machine-local
  execution; the frontend owns presentation. These ownership rules are never traded for a refactor
  win, however large.
- Fences, idempotency, transaction boundaries, credential locality and provider-owned fields stay.
- Tests, types, validation, permissions and acceptance conditions are never weakened to make a
  refactor pass. A deleted or converted assertion requires a mutation check: flip the guarded
  behaviour and watch the converted test fail red.
- No new `//nolint` or `credo:disable` suppressions to make a lint gate pass; a gate flips to
  blocking only when its area's count reaches zero.
- Evidence binds its revision; older evidence never validates newer code. A fixed-design change
  needs a documented amendment, not a worker choice (root `AGENTS.md`); the mandate amendment
  covers the two named non-additive slices (W6-08, W6-09) only.

## Program shape

One slice = one PR = one GDD increment: single purpose, explicit owned file set, observable
acceptance, named slow oracles, independently mergeable. Up to 16 parallel workers run at once.
Hot files (`.github/workflows/ci.yml`, `control/mix.exs`, `daemon/go.mod`,
`frontend/package.json`, `daemon/internal/app/app.go`, plus the quasi-hot `goals.ex`, `state.go`,
`runner.go`, `portal_json.ex`, `goals/read_model.ex`) get at most one writer per wave; the
high-risk W4-W6 programs carry one integration owner each. Lint and coverage gates land advisory
and flip blocking per area when that area's count reaches zero (plan §10).

- **W1 — low-risk enablers (14 slices, complete)**: lint baselines and advisory gates, vulnerability
  scanning and dependabot, the verified plug CVE bump and multipart-parser drop, dead-code
  deletion, move-only god-file splits, Elixir fixture extraction, served-document gates.
- **W2 — abstraction/dedup I (17 planned slices; W2-12 split into 12a/12b at contract review)**: CI
  test observability and the coverage plane, strict single-pass JSON decode, controller plumbing
  and PortalJSON consolidation, shared validators/support/mutation-id/read-model/state/wire
  helpers, typed errors, harness session scaffolding, authority split, served-value helpers,
  clone-aliasing test.
- **W3 — abstraction/dedup II + medium-risk (16 slices)**: changed-path classifier extraction,
  response/request-shape tables, Elixir fixture batch 2, probe/registry dedup, notification and
  witness-writer fixes, error-cause wrapping, errcheck triage, result dispatch flattening, HTTP
  rescue narrowing, UI primitives, HTTP boundary unify, ExUnit async conversion, giant-test split,
  scripts/ci defect repairs.
- **W4 — structural refactors (16 planned slices; W4-15 split into 15a/15b at the W4 reconcile)**:
  `startGoalAdmission` phase split, platform session objects, execution runner decomposition,
  workspace walkers, Linux v2 finalize unification, provider-access intent context, `goals.ex`
  facade phase 1, ChatPage decomposition, Api reads/writes split, refusal classification,
  frontend test-harness consolidation, Go test doubles, wall-clock burn-down part 1, browser
  fixtures, harness JSONL decoder leaf and staged driver, Python test harness.
- **W5 — high-risk programs (13 planned slices; W5-07 and W5-13 dropped at the W5 reconcile)**:
  witness scaffolding converge-in-place, containment convergence phase 1 (blocked under D3),
  selected-project hook, settle-poll removal, error-reason coverage, test-only seam removal,
  jsdom/browser split, platform dead helpers, capture-fixture consolidation, source-subject
  single implementation, model-routing split.
- **W6 — tail micro-waves (12 slices)**: CI structure finalization, browser-spec wiring,
  `isGoTestBinary` fork removal, opencode promote-or-drop (parked under D1), containment
  convergence phase 2 (blocked under D3), cmd extraction, frontend route splitting, dead protocol
  API deletion, `GoalAttentionFeed.unavailable` removal, Elixir lint/coverage flips, Styler
  adoption, wall-clock burn-down part 2. Rows touching `ci.yml` serialize in the order shown, at
  most one `ci.yml` writer per wave.

## Working method

GDD increments with DeepSeek worker stages and independent Opus review:

- DeepSeek V4.1 Flash workers run the implement, simplify, apply and fix stages in isolated
  worktrees; each slice's own plan file (`docs/plans/<date>-<slice>-plan.md`) is part of its owned
  set.
- Every PR gets an independent Opus review bound to its head; findings are reconciled against both
  reviewer reports and fixed in bounded rounds until the review returns no findings.
- Contracts carry six acceptance clauses — observable pass condition, judging artifact, failure
  control, out of scope, evidence plan, mergeable alone — plus a strict owned-file set. From W3
  onward, every contract is reviewed independently by DeepSeek and Opus before dispatch, and a
  reconciler re-measures every contested fact at the live revision and folds the outcome in as a
  binding `## Conductor decision` section, so contracts carry the review amendments from the
  start (lesson 21). W1 was reviewed at plan level and W2's review amendments were folded
  without a separate reconcile report.
- Binding conductor rules per slice: **C1 — byte-identical surfaces** (no wire, persisted, error
  or log text and no CI-gated job/test name changes, except where a contract explicitly authorizes
  one and pins it with a test); **C2 — LOC report-only** (the plan's LOC estimates are never a
  gate; deltas are re-derived and reported); **C3 — control-flow audit (lesson 32)** (every
  decomposition or consolidation records a base-vs-HEAD map of each early return, fall-through,
  error short-circuit and default clause, with a gate covering every package whose tests exercise
  the moved paths, not only the edited package). C1-C3 are conductor rules; D1-D16 below remain
  the owner decisions.
- Landing: one serialized landing lane at a time; Control slices land only after the full
  `mix test`; after each landing the other ready PRs catch up to `main` with merges whose
  merge-only deltas are verified mechanically. Slow oracles (daemon `-race`, the frontend suite,
  the Playwright render gate, the dispatch-only acceptance workflows) are named per slice, run in
  the foreground, and are never left in background jobs.
- Records: each PR body carries its plan id, owned file set, acceptance result, oracle output,
  risk/protocol/LOC lines and the SHA-256 of the contract revision it dispatched against;
  committed records pass the record-cleanliness and DLP rules; a review stage that dies or
  truncates is rerun, never treated as clean.

The routing above is the current working method, not an acceptance condition: the contracts'
acceptance clauses bind whatever model or harness runs them.

## Owner decisions

Plan §9's decisions were adopted at synthesis and re-taken where marked at the wave gates; each is
implemented by the slices named. The per-slice `## Conductor decision` sections carry the binding
detail.

| # | Decision | Adopted default |
| --- | --- | --- |
| D1 | opencode adapter fate (11.7k LOC, half-wired) | **Keep and freeze**: retain `harness/opencode/**`, its six CI assertions and the release-acceptance case; invest nothing further (W6-04 parked). Any drop needs an owner-approved design amendment editing `docs/design/harness.md`, the opencode rows of `docs/goals/README.md`, the acceptance script/inputs and `linux_owner_witness_linux.go` together. |
| D2 | always-null `attention.unavailable` wire member | Remove `GoalAttentionFeed.unavailable` only, end to end (schema, fixtures, projection, DTOs, frontend) in W6-09; `ProjectBlockedFeed.unavailable` stays — it is live on workspace-read failure. The W6-09 PR carries the same-PR `docs/design/protocol.md` and `docs/design/frontend.md` amendment under the mandate. |
| D3 | dual Linux containment generations (legacy supervisor + cgroup-v2 owner) | **Deferred / amendment-gated**: W5-02 and W6-05 are blocked until an owner-approved `harness.md` design amendment covers the OmO/OMP/generic/workspace policy and host prerequisites, and a dispatched `linux-native-v2-acceptance` run is green for each kind's exact binary on a qualifying host. |
| D4 | test-only witness authority in production binaries; `isGoTestBinary` fork | **Consolidate in place** (W5-01): one witness-config struct parsed once behind the single `SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS` gate, privileged writes confined to a witness-owned directory; **no build tag** — witness evidence binds the shipped binary's SHA-256. Delete the self-inspection fork in W6-03 (independent of D3). |
| D5 | dead goal-envelope decode/validate family in `internal/protocol` | **Delete**; retarget the fixture suites to the production entry points (W6-08). |
| D6 | Linux v2 ack stages (10 declared, 2 emitted) | **Shrink the table to the emitted stages** inside W4-05, after proving no persisted journal/receipt can carry a removed stage name — otherwise keep decode tolerance for removed names. |
| D7 | browser specs never run in CI | **Keep and wire by fixture family** (W6-02): compose specs into the existing compose job; connections/planning into a new Linux fixture job; live-daemon/chat-live as an explicit manual matrix; the W2-01 inventory gate prevents regression. |
| D8 | ST1005 (584 capitalized Go error strings) | **Exclude ST1005 from the golangci gate**; the optional mechanical slice for the ~134 genuine cases (W5-13) was dropped at the W5 reconcile (decision G — the genuine-case premise was disputed and both measurements are recorded). |
| D9 | ExUnit serialization | **Convert DB-only files to `async: true`**, keep app-env/migration files serial; new flakes are findings, not normalized (W3-14). |
| D10 | harness capture fixture versions (114 files) | **One canonical capture per harness + per-version deltas**; retire versions older than the two most recent after the runbook check (W5-10). |
| D11 | pytest vs unittest for `scripts/ci` | **Keep unittest**; revisit only if the parametrization need grows. |
| D12 | Styler as a `mix format` plugin | **Adopt** in a standalone mechanical PR with a reviewed first run (W6-11). |
| D13 | MSW for frontend test doubles | **Adopt** in the test-harness slice (W4-11). |
| D14 | coverage ratchet | Advisory artifacts from W2-17; floors become blocking at W6-10/W6-12 once baselines are stable. |
| D15 | jsdom vs browser invariant split | **Adopt the split** (jsdom = state/contracts, browser = layout/scroll/focus); deletions land one red/green pair at a time (W5-08). |
| D16 | duplication gate | **Changed-file jscpd with explicit `--max-lines/--max-size`**; repo-wide numbers stay trend-only (W6-11). |

Plan §9 additionally records D17 (a bounded `os.Root` trial for the Windows private-path check,
backlog) and D18 (defer happy-dom / react-hook-form, no measured win); neither carries a slice in
this program.

## Status

State re-derived on 2026-11-21 from `gh`, the contracts and the live branches at `origin/main`
`a503eab7` (the T1 refresh logged below). "Pipeline" = dispatched work with no PR yet: W2-01,
W4-03, W4-04 and W4-06 have work in their worktrees; W2-07, W2-17 and W3-04 have pushed branches
without a PR; W3-03 has a prepared worktree at `main`. "Gated on X" lists the merge-first set at
this snapshot; the contracts are the authority and every gate is re-derived at dispatch. The W4
and W5 reconciles under independent two-model review are recorded (reconcile reports, parts a
and b): W4 split W4-15 into 15a/15b and W5 dropped W5-07 and W5-13. The W6 contracts were
batch-drafted in two parts and reviewed before dispatch.

### Wave 1 — low-risk enablers (14 slices) — complete

| Slice | Purpose | PR | State |
| --- | --- | --- | --- |
| W1-01 | CI quality-instrumentation plane: advisory golangci (both GOOS), Credo, ruff, shellcheck/actionlint steps + vulnerability scanning + dependabot | #157 | merged |
| W1-02 | Control dependency hygiene: Credo dep, inert telemetry deps removed, plug 1.20.4 (CVE fix), `:multipart` parser dropped | #138 | merged |
| W1-03 | Control-web strict-Credo clean-up (lib subtree) | #147 | merged |
| W1-04 | Daemon-platform god-file split, move-only (anchor/helper/supervisor) | #143 | merged |
| W1-05 | `app.go` split into concern files, move-only | #144 | merged |
| W1-06 | Daemon dead-code sweep (caller-free surfaces only) | #142 | merged |
| W1-07 | App/state hygiene: dead stores + preflight mount-path unescape fix | #141 | merged |
| W1-08 | Daemon test enablers: per-test goleak, state `t.Parallel()`, short-wait folding | #148 | merged |
| W1-09 | Contract envelopes decoded in a single pass | #140 | merged |
| W1-10 | Frontend strict type-checked ESLint baseline + dead-export trim + coverage config | #146 | merged |
| W1-11 | `scripts/ci` ruff baseline (config + format + autofix) | #151 | merged |
| W1-12 | Elixir fixture extraction batch 1 | #149 | merged |
| W1-13 | Control-web served-document gates for the four ungated reads | #145 | merged |
| W1-14 | Control-core dead-surface deletion (F11) | #139 | merged |

### Wave 2 — abstraction/dedup I (17 slices, W2-12 split) — in flight

| Slice | Purpose | PR | State |
| --- | --- | --- | --- |
| W2-01 | CI test observability: durations + browser-suite inventory gate | — | pipeline |
| W2-02 | Strict JSON decoded in one ordered pass (CW-1) | #156 | merged |
| W2-03 | Controller plumbing consolidation (CW-2/3/4/5/6/7/10) | #163 | merged |
| W2-04 | `PortalJSON` consolidation (CW-8a/8b, CW-15) | #161 | merged |
| W2-05 | Goal schema validators (CCS1) | #150 | merged |
| W2-06 | Provider shared support (CCS2) | #154 | merged |
| W2-07 | Mutation-id + versioned-changeset helpers (CCS3) | — | pipeline (branch pushed, no PR) |
| W2-08 | Read-model projection context (CCS4) | #162 | in review |
| W2-09 | State validator section helpers (DA S2) | #165 | in review |
| W2-10 | Callback factory dedup (DA S3) | #159 | merged |
| W2-11 | Typed error identities (DP S-3) | #160 | in review |
| W2-12 | Wire primitives + strict-decode parity (DR S3) | — | superseded by W2-12a/W2-12b (split at contract review) |
| W2-12a | Wire validator family (split of W2-12) | #153 | merged |
| W2-12b | Operator client relocated to its test-only callers (split of W2-12) | #178 | in review |
| W2-13 | Harness shared session scaffolding 5a (DR S5a) | #155 | merged |
| W2-14 | `authority.go` split + clone/equal unification (DR S8) | #158 | merged |
| W2-15 | Served-value helper module (FE S2) | #164 | in review |
| W2-16 | Clone-aliasing reflection test (DP S-9) | #152 | merged |
| W2-17 | Coverage plane (D14; moved from W1-01) | — | pipeline (branch pushed, no PR; serialized after W2-01 on `ci.yml`) |

### Wave 3 — abstraction/dedup II + medium-risk (16 slices)

| Slice | Purpose | PR | State |
| --- | --- | --- | --- |
| W3-01 | Changed-path classifier extraction + corpus single source (SC S2) | — | gated on W2-01 + W2-17 (`ci.yml` serialization) |
| W3-02 | Response builder + per-operation request-shape table (DP S-4) | — | gated on W2-11 (#160) |
| W3-03 | Elixir fixture extraction batch 2 — web + integrations (T1 cont.) | — | queued — gates merged (#163, #161); worktree staged |
| W3-04 | Probe/registry dedup, cycle-free (DR S6) | — | pipeline (branch pushed; #155 merged) |
| W3-05 | Notification reconnect seam + fake-agent exit path (DR S11) | #181 | in review |
| W3-06 | Witness-writer error propagation (DR S12) | #166 | in review |
| W3-07 | Error-cause wrapping + capacity split (DA S4) | — | gated on W2-09 (#165) |
| W3-08 | Unexport unused state surface + test-only options (DA S8) | — | gated on W2-09 + W3-07 |
| W3-09 | Errcheck triage part 1 — app recovery paths (DA S7a) | — | gated on W3-07; must land before W4-01 |
| W3-10 | Result dispatch flattening (CCS5) | — | gated on W2-07 |
| W3-11 | Narrow HTTP rescue (CCS9b) | #179 | in review |
| W3-12 | UI primitives + resource-dialog collapse (FE S4+S9) | — | gated on W2-15 (#164) + W3-03 |
| W3-13 | HTTP boundary unify (FE S5) | — | gated on W2-15 (#164) + W3-12 |
| W3-14 | ExUnit async conversion (TC-E) | — | gated on W3-03 |
| W3-15 | Giant-test split (TC-H) | — | gated on W2-09, W3-08, W3-10, W3-14 |
| W3-16 | `scripts/ci` defect repairs + visible PyYAML oracle (SC S8) | #180 | in review |

### Wave 4 — structural refactors (16 planned; W4-15 split)

| Slice | Purpose | PR | State |
| --- | --- | --- | --- |
| W4-01 | `startGoalAdmission` phase split (DA S13) | — | gated on W3-09; W3-08 co-writes `app.go` |
| W4-02 | Session objects for supervisor/anchor loops (DP S-5) | — | gated on W3-02; before W5-01 |
| W4-03 | Execution `Start`/`closeContainmentAttempt` decomposition (DR S7) | — | pipeline (work in its worktree; no open-PR gate) |
| W4-04 | Workspace walkers + subject guards (DR S10) | — | pipeline (work in its worktree; no open-PR gate) |
| W4-05 | Linux v2 finalize pipeline unification (DR S9, D6) | — | gated on W4-03 |
| W4-06 | `provider_access` intent-context unification (CCS8) | — | pipeline (work in its worktree; no open-PR gate) |
| W4-07 | `goals.ex` facade split phase 1 (CCS7) | — | gated on W2-07, W3-10, #162 |
| W4-08 | ChatPage decomposition (FE S7) | — | gated on W4-11 (frontend serial order) |
| W4-09 | Api reads/writes split (FE S6) | — | gated on W4-08 |
| W4-10 | Refusal/outcome classification module (FE S3) | — | gated on W4-09 |
| W4-11 | Frontend test-harness consolidation + MSW (FE S10, D13) | — | gated on W2-15 (#164) + W3-12 + W3-13 + the consolidated npm bump (#183, replaces the #173-#177 family); head of the frontend serial order |
| W4-12 | Go test-double extraction part 1 (TC-G) | — | gated on W3-08 → W3-15 |
| W4-13 | Wall-clock burn-down part 1 — Go deadlines (TC-F) | — | gated on W4-12, W4-15b (after W4-15a), W3-04, W3-15, #178 |
| W4-14 | Browser fixture consolidation (TC-I) | — | gated on W2-01 |
| W4-15 | Harness staged driver 5b — split at the W4 reconcile | — | superseded by W4-15a/W4-15b |
| W4-15a | Harness JSONL decoder leaf (`internal/harness/linejson`) | — | gated on W3-04 + W3-15 |
| W4-15b | Harness staged driver 5b (DR S5b) | — | gated on W4-15a |
| W4-16 | Python test harness: git fixture + citest (SC S5 + T8) | — | gated on W3-16, W3-01 |

### Wave 5 — high-risk programs (13 planned; W5-07 and W5-13 dropped)

| Slice | Purpose | PR | State |
| --- | --- | --- | --- |
| W5-01 | Witness scaffolding converge-in-place: one gate, no build tag (DP S-7, D4) | — | queued — see contract (gates: #160 → W5-09; W3-02 → W4-02; W4-13, #137, #178, dependabot #167-#172; W3-01 before any `ci.yml` edit) |
| W5-02 | Containment convergence phase 1: v2 mandatory on Linux (DP S-11a, D3) | — | blocked (D3) |
| W5-03 | ChatPage/scope-hook follow-ups (FE S7b + S8) | — | gated on #164 → W3-12 → W4-11 → W4-08 → W4-09 → W4-10 |
| W5-04 | Goal-settle poll removal + wait budgets (FE S11 + TC-J) | — | gated on #164 → W3-12 → W4-11 → W4-08 → W4-09 → W4-10 |
| W5-05 | Error-reason coverage + fallback counter (CW-9) | — | gated on W3-03 → W3-14 (#163 merged) |
| W5-06 | Test-only seam removal (CW-12 remainder) | — | queued — #163 merged; rebase at dispatch (W3-03/W3-14 touch the oracle test only) |
| W5-07 | Descendant-monitor state machine (DP S-10) | — | dropped (W5 reconcile, decision D) |
| W5-08 | jsdom/browser layer split (TC-K, D15) | — | gated on the frontend chain (#164 → W3-12 → W4-11 → W4-08 → W4-09 → W4-10), W4-14, W5-03 + W5-04 |
| W5-09 | Platform dead test-only helpers (DP S-8) | — | gated on #160 (lands before W5-01) |
| W5-10 | Harness capture fixture consolidation (TC-M, D10) | — | gated on W3-04 → W3-15 → W4-15a → W4-15b → W4-13 (W2-13 merged); runbook check at dispatch |
| W5-11 | Source-subject single implementation (SC S9) | — | gated on W3-16 (#180) + W4-16 |
| W5-12 | model-routing module split (SC S10) | — | gated on W4-16 + W3-16 (#180); W5-11 no longer a dependency (decision E) |
| W5-13 | ST1005 genuine-case mechanical slice (D8) | — | dropped (W5 reconcile, decision G) |

### Tail W6 — micro-waves (12 slices)

| Slice | Purpose | PR | State |
| --- | --- | --- | --- |
| W6-01 | CI structure finalization: inline program extraction + contracts composite action | — | queued (W1-01 merged; `ci.yml` micro-wave) |
| W6-02 | Browser spec wiring per D7 | — | gated on W2-01 |
| W6-03 | `isGoTestBinary` fork removal (DP P-05, D4) | — | gated on W5-01 → W5-09; W5-07 dropped (its monitor code stays) |
| W6-04 | opencode promote-or-drop | — | parked (D1) |
| W6-05 | Containment convergence phase 2: delete the legacy generation (DP S-11b, D3) | — | blocked (D3) |
| W6-06 | cmd extraction: launcher handshake + clone-bootstrap (DR S13) | — | gated on W6-05 |
| W6-07 | Frontend route code splitting (FE S13) | — | gated on W4-08 |
| W6-08 | Dead protocol API deletion: goal-envelope family (DR F-07, D5) | — | queued (W1-06 merged; micro-wave); mandate slice — amends `docs/design/protocol.md` in-PR |
| W6-09 | Remove always-null `GoalAttentionFeed.unavailable` (CW S12, D2) | — | gated on W5-05; mandate slice — carries the `protocol.md` + `frontend.md` amendment in-PR |
| W6-10 | Elixir lint/coverage flips | — | gated on W6-01 + area counts at zero |
| W6-11 | Styler adoption + duplication burn-down (D12, D16) | — | gated on the control-slice tail |
| W6-12 | Wall-clock burn-down part 2 + duration ratchet blocking (TC-F/TC-B cont.) | — | gated on W4-13, W2-01 |

### Blocked, parked and dropped items

- **W5-02 — blocked (D3).** Unblock: (1) an owner-approved design amendment updating
  `docs/design/harness.md` for the OmO/OMP/generic/workspace Linux-owner policy and the host
  prerequisites, and (2) a dispatched `linux-native-v2-acceptance` run green for each kind's exact
  binary on a qualifying host. One dispatch exists (2026-11-19): the protected acceptance job
  never completed — cancelled while awaiting approval — so there are zero green runs. Do not
  dispatch before both conditions exist.
- **W6-05 — blocked (D3).** Unblock: W5-02's amendment and green per-kind acceptance, plus the
  phase-1 soak; the plan's W5-07 dependency is moot — W5-07 was dropped and its monitor code
  stays the deferred live path until a future convergence. W6-06 (cmd extraction) rides on
  W6-05 and stays blocked with it.
- **W6-04 — parked (D1).** Keep and freeze; invest nothing further. Unblock (only for a drop): an
  owner-approved design amendment editing `docs/design/harness.md`, the opencode rows of
  `docs/goals/README.md`, the acceptance script/inputs and `linux_owner_witness_linux.go`
  together.
- **W5-07 — dropped (W5 reconcile, decision D).** The single terminal-predicate acceptance already
  holds at base (`endsMonitor()`), and the re-scope could not compile inside the owned set; no
  branch, no reservation; the existing monitor code stays the deferred live path.
- **W5-13 — dropped (W5 reconcile, decision G).** Optional under D8; the reviews disputed whether
  the genuine-case set is non-empty and both measurements are recorded in the contract; the churn
  PR is not worth it. No branch, no config change — the ST1005 exclusion stands.

## Done when

- Every non-blocked, non-parked, non-optional slice is merged, or dropped with its reason recorded
  in the Amendment log and its contract's disposition; W5-07 and W5-13 are the recorded drops.
- Every lint and coverage gate is live per its staging, each carrying its owning slice or an
  explicit carry recorded either way:
  - Credo strict — W6-10 flips it blocking at count zero;
  - golangci-lint (both GOOS), strict ESLint, ruff and shellcheck/actionlint — no owning slice
    today: each is carried to `docs/TODO.md` with its current count;
  - the changed-file jscpd gate — W6-11; coverage floors — W6-10/W6-12 (after W2-17's advisory
    plane); duration ratchets — W6-12.
- The blocked and parked items (W5-02, W6-04, W6-05/W6-06) are carried into `docs/TODO.md` with
  their unblock conditions, so nothing silently disappears when this goal archives.
- A closure evidence record under `docs/evidence/` maps every slice — merged, dropped, blocked,
  parked or optional-taken — to its PR and merge commit, carries the SHA-256 of each slice's
  binding contract revision, and is independently reviewed; this goal archives only after the
  owner's verdict is recorded. No model declares completion.

## Amendment policy

The owner's 2026-11-21 authorization lets the conductor amend this goal while the program runs:
the design evolves with implementation and this file tracks it instead of freezing contract bytes.
The per-slice contracts (outside the repository) remain the binding acceptance text for each
slice; this goal records the program's shape, decisions and status. Nothing changes silently:
amendments fall into three tiers, and every one is appended to the [Amendment log](#amendment-log)
— append-only, never edited retroactively — with its date, what changed and why.

- **T1 — status and fact refresh.** The conductor corrects status rows and facts in place and logs
  the refresh with the date and the `origin/main` sha the rows were derived from at the `gh` and
  branch snapshot time. No review gate.
- **T2 — scope, slice add/drop/split, Done-when, conductor rules (C1-C3), or any design-decision
  change.** The conductor may amend under the owner's standing authorization (2026-11-21), but
  the amendment takes effect only after an independent review by a different model than the
  writer; the log entry records the authority, the review by reference, the date, and the revision
  the amendment's evidence binds. Scope and acceptance are never rewritten to fit delivered work.
- **T3 — closure and archive.** The closure evidence record is independently reviewed and the
  owner's verdict is recorded before this goal archives; no model declares completion, and the
  conductor cannot close this goal on its own authority.

Working rules: each slice PR body carries the SHA-256 of the contract revision it dispatched
against — the contracts are mutable files outside version control, so the digest pins the binding
acceptance text — and the closure evidence record carries the same digests, so a reviewer can
reproduce which acceptance text a merged slice met. Status rows are edited in place; T2
amendments also edit the body in place and log date/what/why/authority/review. The plan snapshot
stays verbatim apart from its provenance header and one placeholder normalization.

### Amendment log

- **2026-11-21 — goal recorded mid-program.** The refactor program is recorded as goal 0010 after
  W1 (14 slices) fully landed: W2 is largely in review or pipeline, four W3 slices are in review
  and one is in pipeline, and W4-W6 are queued behind their contracts. No earlier amendment
  applies.
- **2026-11-21 — T1 status refresh.** Status rows and facts re-derived from `gh` and the live
  branches at `origin/main` `a503eab7`: #161 (W2-04) and #163 (W2-03) merged; W2-07, W2-17 and
  W3-04 have pushed branches without a PR; W2-01, W4-03, W4-04 and W4-06 have work in their
  worktrees; #183 (`build/frontend-dev-deps`) is the consolidated npm bump; the W4/W5 reconcile
  reports and the W6 review pair are recorded. Gate rows and slice counts were corrected in the
  same pass.
- **2026-11-21 — T2: the independent review reconciliation.** The independent Opus review of
  PR #182 (review 1, `182-opus-review-1.md`, 2026-11-21, bound to `b2850ebd`; findings P1-1,
  P2-1-P2-5 and P3-1-P3-5) is applied: the amendment policy above is tiered and this log becomes
  the append-only audit trail; the mandate is recorded as the owner-approved amendment permitting
  non-additive changes for W6-08/W6-09 only; the conductor rules are renamed C1-C3; the Done-when
  lint bullet names each gate's owner or carry; closure now requires an independent review and the
  owner's verdict. Authority: the owner's standing authorization (2026-11-21); review by
  reference as cited.
- **2026-11-21 — T2: W4-15 split; W5-07 and W5-13 dropped.** Recorded from the W4/W5 contract
  reconciles under independent two-model review: W4 reconcile decision E3 splits W4-15 into
  W4-15a (JSONL decoder leaf) and W4-15b (staged driver); W5 reconcile decision D drops W5-07
  (its single-predicate acceptance already holds at base) and decision G drops W5-13 (optional;
  premise disputed, both measurements recorded). Authority: the conductor's reconcile decisions
  under the owner's standing authorization; reviews by reference: the W4/W5 contract reviews and
  reconcile reports (parts a and b, 2026-11-21).

## Known follow-ups

Carried alongside the program, not slices in it:

- **Witness-spine flake.** `TestProductionLinuxContainmentWitnessSpine/daemon_sigkill_after_lease_arm_before_resume`
  stalls at the stop-receipt barrier, tracked in `docs/TODO.md` ("Production Linux containment
  witness: stop-receipt barrier stall awaiting a diagnosable occurrence"); diagnose the next
  occurrence from the restarted daemon's exported log tail and do not raise the 45 s bound.
- **Dependabot npm lockfiles vs `check-lockfile-registry.py`.** The eleven dependabot PRs
  #167-#177 came from W1-01; the npm family (#173-#177, to be superseded by the consolidated bump
  #183 on `build/frontend-dev-deps`) must not land a lockfile whose tarballs point at a mirror
  registry — the open canonical-registry work is tracked in `docs/TODO.md`
  ("Lockfiles must carry canonical registry URLs").
- **Go stdlib advisories.** The advisory `govulncheck` step reports 12 Go standard-library
  vulnerabilities because CI pins `go-version: 1.27.0`; all are fixed in go1.27.2-go1.27.4.
  Tracked in `docs/TODO.md` ("CI Go toolchain: bump past the stdlib advisories").
- **Pre-existing username paths in two scripts.** `scripts/ci/omo-native-evidence.py` and
  `scripts/ci/opencode-v2-support-witness.py` still carry the operator's username in committed
  default binary paths. Pre-existing DLP debt, named here by repository path only; no slice or
  gate owns it today.
