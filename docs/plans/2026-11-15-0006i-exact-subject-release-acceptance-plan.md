# 0006i implementation plan — exact-subject release and authorized acceptance

Contract: [docs/goals/archive/0006i-exact-subject-release-acceptance.md](../goals/archive/0006i-exact-subject-release-acceptance.md)
(read in full). Operator brief: `.tmp/0006i-brief.md` (binding; deliverable = documentation
only). Shape reference: the completion-matrix format of
[goal-0006-completion-matrix-2026-09-18.md](../evidence/goal-0006-completion-matrix-2026-09-18.md)
(Area | Status | Current evidence | Missing proof) mirrored against the receipt structure of
[goal-0006j-omp-native-capability-2026-11-14.md](../evidence/goal-0006j-omp-native-capability-2026-11-14.md)
(exact subjects, commands with outcomes, clause-to-evidence boundary table).

## Subject freeze (measured facts, 2026-11-15)

| Fact | Value | Source |
| --- | --- | --- |
| Subject commit | `8ded77150ec0ae49d52355670fdad78a348bf1d5` (squash merge of PR #22, merged 2026-11-14T19:46:57Z, parent `27dc3e4`) | `git rev-parse HEAD`, `gh pr view 22` |
| Subject tree digest | `3a1dec551b0e2c630f6720d8a542343648270793` | `git rev-parse HEAD^{tree}` |
| Reviewed/CI head | `109f6037a34ad340219fa6c07c3db4145e2377a1` — same tree `3a1dec551b0e2c630f6720d8a542343648270793` | `git rev-parse 109f6037a34ad340219fa6c07c3db4145e2377a1^{tree}` |
| Tree equivalence | byte-identical: PR #22 was squash-merged, so the PR head and the merge commit carry the identical tree object | both `^{tree}` resolves equal |
| Clean checkout | `git status --porcelain` empty; `git write-tree` = `git rev-parse HEAD^{tree}` = `3a1dec55…` | run locally on this worktree |
| CI on the identical tree | run `43633014138` (PR head `109f6037…`, 11 jobs success, 1 skipped-by-design) and run `43633946120` (push `8ded771` itself, 11 success + 1 skipped) | `gh run list --commit`, `gh api …/check-runs` |
| Independent review | Codex code review on `109f6037…`: completed, no findings, no unresolved findings (PR #22 comment `6883104913`) | `gh pr view 22 --json comments` |
| Release/version marker | none — repo is pre-0.0.1, no tags, no CHANGELOG/VERSION convention exists; the README status banner already carries it | `git tag -l` empty, `gh release list` empty |

## What this increment is and is not

It is a **recording** increment: one new evidence receipt that freezes the Subject and maps
every configured acceptance predicate to evidence bound to that exact tree, with the hosted
and operator predicates recorded precisely as pending. It is **not** a release, a capability
promotion, an `achieve`, or an acceptance declaration:

- The repo has no release/version convention (`git tag -l` empty, no releases, README
  pre-0.0.1 banner already present), so per the brief's "only if the repository already has a
  convention" clause, **no release marker is invented**. The receipt records this.
- `linux-native-v2-acceptance` is `workflow_dispatch`-only (`.github/workflows/ci.yml:14-16`)
  and is skipped in both CI runs on the Subject tree. It must be dispatched by the operator
  on the RC head and is recorded as **pending operator dispatch** with the exact workflow and
  ref — never as passed.
- Operator acceptance and `achieve` are pending by construction; the receipt states the exact
  remaining predicates (the operator-only `achieve` command path in
  `control/lib/symmetry_control/goals.ex` requires final-acceptance authority, accepted
  outcomes for every required item, an integration outcome matching the exact Subject, and
  required evidence bound to the same subject hash).

## Deliverable shape

1. `docs/evidence/goal-0006i-exact-subject-release-acceptance-2026-11-15.md` — the final
   completion matrix + Subject record + per-area evidence. Statuses: proved (cited to the
   Subject tree), pending-operator (hosted/native), and explicitly-incomplete where a
   capability class stays unverified. No rounding up.
2. `docs/goals/README.md` — one closure/index entry for 0006i in the established form,
   stating the pending predicates so no reader mistakes the receipt for an acceptance.
3. `docs/goals/archive/0006i-exact-subject-release-acceptance.md` — the goal file moved
   unchanged (git mv), matching how the 0006d/0006h closings move the file with history
   preserved.

No source, test, workflow, or acceptance-condition changes. No commit (leave the tree for
verification per the stage rules).

## Area-by-area evidence mapping (what the receipt cites)

| Area | Status the receipt records | Evidence bound to the Subject tree |
| --- | --- | --- |
| Subject identity + clean checkout | proved | `git rev-parse` outputs, tree equivalence, `git status --porcelain` empty, `git write-tree` == HEAD tree |
| Contract/wire contracts | proved | CI `contracts` job on run 43633014138: `pnpm contracts:check` 31/31 pass, frontend typecheck/lint/test/build green |
| Control (`mix`) | proved | CI `control` job: `mix format --check-formatted` clean, `mix compile --warnings-as-errors` exit 0, `mix test` **826 passed, 21 skipped**, provider-broker E2E 4 passed, goal-receipt selective gate 1 passed/10 excluded, `MIX_ENV=prod mix release` succeeded |
| Go (`daemon`) | proved | CI `daemon` job: `gofmt -l` clean, `go vet ./...` exit 0, all 24 test packages `ok` (plus the no-test `e2e/preauthority-sentinel`), pidfd/OpenCode peer-negative selective witnesses verified run=1 pass=1 |
| Race | proved | CI `daemon` job race step: `go test -race -count=1 -timeout 600s ./...` all 24 test packages ok (app 382.168s, state 74.441s, …) |
| Platform Windows | proved | CI `daemon-windows` job: `go test ./...` all 22 packages ok (20 test packages + `e2e` + the no-test `e2e/preauthority-sentinel`), `go build` both cmds, production containment witness + pre-authority crash matrix (prepare/bind/…/clear) all PASS |
| Platform Linux hosted | proved for ordinary hosted gates; native v2 acceptance **pending operator dispatch** | `daemon`, `integration`, `compose`, `pi-control-e2e-linux` jobs green; `linux-native-v2-acceptance` skipped (dispatch-only) |
| Native capability: Codex 0.168.0 | proved within recorded bounds — Windows start/events/cancel; Linux explicitly unsupported (containment conflict) | committed testdata/promotion at the Subject tree; historical receipts are context, the promotion claims are part of the tree's own tests |
| Native capability: Claude Code 2.1.323 | proved within recorded bounds — Windows; Linux unsupported | as above |
| Native capability: Pi 1.5.2 | proved within recorded bounds — Windows; Linux fail-closed | CI `pi-control-e2e-windows` (5 passed) + `pi-control-e2e-linux` refusal witness (run=1 pass=1) |
| Native capability: OMP 18.20.0 | explicitly incomplete — no capability promoted (zero-spend blocker); transport/decoder evidence recorded | `docs/evidence/goal-0006j-omp-native-capability-2026-11-14.md` bound to the same tree via commit `e4bfbfe`…; tree-identical, so cited, not re-run |
| Native capability: OmO 5.1.69 | proved within recorded bounds — Windows + WSL Ubuntu-24.04 start/events/cancel/retained resume/same-machine fresh handoff for the fixed `read,write` profile | committed `daemon/internal/harness/testdata/omo/5.1.69/` receipts at the Subject tree; migration/rollback tests in `control/test/symmetry_control/migrations/add_omo_harness_kind_migration_test.exs` ran in the control job |
| Native capability: OpenCode V2 2.3.0 | explicitly unsupported by decision (0006g) — both jobs verify the refusal, not a promotion | CI `opencode-native-synthetic` (1.18.32 Linux legacy refusal before health request, gate passed) + `opencode-production-unsupported-windows` (legacy assignment fails before workspace/process, both blobs PASS) |
| Migration | proved | OmO/OMP kind-constraint migrations covered by committed migration tests inside the Subject tree's own control suite (826-pass run); additive-migration family also in `goal_0006_migration_test.exs` |
| Backup/restore, PostgreSQL authority | proved at the recorded local/CI level; hosted disaster recovery **not** proved | integration job: real Control release + `Release.migrate()`, restart recovery (`TestDaemonSurvivesControlRestart` PASS), daemon crash response-loss witness PASS; no pg_dump/pg_restore hosted DR job exists — recorded as an explicit gap, not claimed |
| Release gate (deployable artifacts) | proved for the artifacts CI builds | control job `MIX_ENV=prod mix release` → "Release created at _build/prod/rel/symmetry_control"; daemon job `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...`; compose job local HTTP + production HTTPS stacks incl. 308/421/SNI refusals and 101 websocket upgrade |
| Protocol v1 / goal-less compatibility | proved | `legacy-route-audit` job green; goal-less paths covered by the Subject tree's own suites (generic adapter/fake-agent tests in `daemon`/`integration` jobs; rollout-scoped integration job with `SYMMETRY_GOALS_ROLLOUT_ENABLED=true` + Oban smoke `goal_oban_smoke=completed`) |
| Hosted Linux/Windows native acceptance | **pending operator dispatch** | `workflow_dispatch` on `main` @ `8ded771` → workflow "CI", job `linux-native-v2-acceptance`, environment `linux-v2-native-acceptance`; skipped in both completed runs (never recorded as passed) |
| Operator acceptance + `achieve` | **pending by construction** | operator-only command path; requires final-acceptance authority decision + accepted required outcomes + integration outcome on this Subject + evidence bound to the same subject hash |
| Independent semantic review | proved for the implementation head | Codex review on `109f6037…`: completed, 0 findings, 0 unresolved; tree-identical to the Subject |

Known-defect disclosures carried into the receipt (recorded, not fixed — documentation-only
goal): two pre-existing Control test-compile warnings at the Subject tree
(`replay_task` at `orchestration_goal_admission_test.exs:1351`, `task` at 2450/2541 —
already tracked in `docs/TODO.md` for the maintenance track); no test was weakened.

## Evidence rules followed

- Everything cited from runs `43633014138`/`43633946120` is on commits whose trees are
  byte-identical to the Subject; the receipt states the equivalence proof, then cites the
  runs instead of re-running the same jobs (per the brief's cite-don't-rerun rule).
- Local commands actually run for this receipt: the git provenance set
  (`rev-parse HEAD`, `rev-parse HEAD^{tree}`, `rev-parse <109f6037>^{tree}`, `cat-file -t`,
  `status --porcelain`, `write-tree`), `gh run list/view/api` queries, and `git tag -l`.
  Each with its exit status recorded in the receipt.
- The hosted dispatch job, operator acceptance and `achieve` are recorded **pending** with
  exact instructions; a pending predicate is never presented as satisfied.

## Design trade-offs

1. **Cite rather than re-run CI.** The brief explicitly authorizes citing the green CI runs
   on the tree-identical head with the equivalence proof. Re-running the identical matrix
   would produce no new Subject and burn the operator's runner budget.
2. **One receipt, not two.** The matrix format and the per-goal receipt structure are
   combined in one document (the brief demands both formats; the 0006j receipt shows the
   per-area clause table is compatible with the matrix columns).
3. **No release marker.** No convention exists (no tags, no releases, no CHANGELOG, no
   VERSION file; README already carries pre-0.0.1 status). The brief's conditional clause
   resolves to "invent nothing".
4. **Goal file archived unchanged.** Matching the 0006d/0006h closure commits: `git mv`
   the goal file to `archive/`, update the README index. The 0006i entry records the pending
   hosted/operator predicates as the boundary of this closure — the lettered goal's
   recording work is done; the release acceptance itself is not claimable until the operator
   predicates complete on an unchanged tree.

## Out of scope (with reasons)

- Any source/test/workflow change: forbidden by the brief (documentation-only goal).
- Dispatching `linux-native-v2-acceptance`: operator-only by design (environment-gated
  workflow_dispatch); the receipt records the exact dispatch instruction instead.
- Operator acceptance/`achieve`: operator-only command path; a model cannot grant it.
- Fixing the two recorded test-compile warnings or the `docs/TODO.md` items: unrelated
  maintenance; touching control test files would change the tree and invalidate the Subject.
- Re-running the full local suites: the tree is identical to the CI-green head; the brief
  says cite, and re-running adds no Subject-bound value (and `mix` is unavailable locally
  per the 0006j receipt's environment note).
- New baseline/manifest updates: no measurement harness is part of this goal; the only
  manifest-like artifact is the receipt itself.

## Verification of this increment

Markdown-only deliverable. Checks actually run and recorded in the final report:

1. `git mv docs/goals/0006i-exact-subject-release-acceptance.md docs/goals/archive/` (file
   unchanged — verified by `git diff --cached -M` showing a pure rename).
2. Receipt/README/plan files exist with the intended content (`git status --porcelain`
   shows exactly the intended new files plus the rename).
3. Markdown lint/format: the repo has no markdown formatter or lint gate (no config in
   package.json, no CI step); consistency with existing docs is the applied standard.
4. No formatter/linter is defined for docs in CI (`contracts`, `control`, `daemon` jobs own
   code formatting; none touch `docs/`), so no doc-format gate is claimed as run.
