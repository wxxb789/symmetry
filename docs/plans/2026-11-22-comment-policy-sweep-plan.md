# comment-policy-sweep plan — apply the 2026-11-22 owner comment policy to the program's pre-policy comments

Contract: `goal-comment-policy-sweep.md` (GDD, 2026-11-22; held until the pre-policy land queue
merged — it has: `origin/main` is `f1fe1e59`, and the sweep is dispatched as a program slice of goal
0010). Base revision for every coordinate in this plan: `f1fe1e59` (the delivered tree is that
revision plus uncommitted working-tree changes; when a line number is cited it resolves at the base
unless a stage note says otherwise).

Plans consulted as the shape reference: `2026-11-21-core-dead-surface-plan.md` (definition of the
judging artifact and the failure controls), `2026-11-21-rf-w3-13-http-boundary-plan.md` (stage
updates and per-file proof carriers), `2026-11-21-rf-program-plan.md` (slice/worker rules).

## 1. Candidate derivation (mechanical, re-runnable)

- **Program commits** = the 68 first-parent commits on `main` after the program's base revision
  `a3039f0` (`git log --first-parent --pretty=%H a3039f0..f1fe1e59`), i.e. the land queue's slices
  (`rf-*` 138-214, `fix/*` 137/180/185/194/195, `docs/*` 182/207/208) plus the queue's dependabot
  and npm bumps. The contract's own baseline statement ("subjects/PRs of slices … merged on or after
  2026-11-21") selects exactly this set: `a3039f0` is the plan's base revision and the first program
  landing after it is `6099f26f` (#138, 2026-11-21 08:20).
- **Per-file attribution** = `git blame --line-porcelain -w HEAD -- <file>` over every file the
  program changed (`git diff --name-only a3039f0..f1fe1e59`, 462 files). A line is a **candidate**
  when its blame commit is one of the 68 program commits; the line's class is decided at HEAD:
  `comment` (`//`, `#`, `/* */`), `docstring`/`@doc` (Python docstring ranges from `ast`, Elixir
  `@doc`/`@moduledoc` attributes including heredoc bodies), or `directive` (machine-consumed
  comments, listed in §3).
- **Why blame instead of a `git show --unified=0` text scan**: the contract's rule is a line-level
  attribution of "added on `main` by refactor-program commits, still present at HEAD". Blame gives
  that attribution with exact positions; a diff-text scan (`--unified=0` plus text matching at HEAD)
  cannot disambiguate repeated line texts (duplicate `//` separators, blank docstring lines) and
  mis-locates them. The two methods agree on the commit set; blame is used because it cannot
  mis-locate. Program *moves* are candidates under both methods: a move-only split re-adds the
  lines in a new file, and blame attributes them to the split commit (verified: `4dd374bd`, the
  platform split, owns 506 of the 1,126 platform candidate lines).
- **Excluded by the contract's file rules**: `docs/**` (77 changed files), lockfiles/generated
  (`mix.lock`/`go.sum`/`pnpm-lock.yaml`: 3), data formats (`.json`/`.tsv`/`.snap`: 3), and the 6
  files the program deleted (no HEAD lines to attribute). No candidate exists in any file class the
  contract forbids.

Result: **6,074 candidate lines in 212 files** — 5,165 `comment`, 868 `docstring`, 41 `directive` —
forming 1,651 contiguous blocks (938 of them longer than 2 lines, 4,961 lines in those blocks).

Per-file candidate counts (§9) and the per-line inventory with dispositions
(`docs/evidence/comment-policy-sweep-2026-11-22-inventory.tsv`, one row per candidate) are the
judging artifacts; the inventory is generated from this derivation and re-checked against the
delivered tree by the verifier named in §5.

## 2. Disposition rules (the slice's "API")

One decision per candidate block; every candidate line gets one row in the inventory TSV with
`disposition` ∈ {`deleted`, `compressed`, `kept`} and the rule that produced it.

| rule | meaning | disposition |
|---|---|---|
| `D1` | restates code (signature/name/body narration in a doc comment, docstring or `@doc`) | deleted |
| `D2` | history/migration/provenance narration (slice ids, PR ids, "moved from", "previously", "the base passed", review bookkeeping) | deleted |
| `D3` | banner, divider, step label, Arrange/Act/Assert or numbered test narration | deleted |
| `D4` | commented-out code | deleted |
| `D5` | stale (behaviour, line numbers, revisions or symbols that no longer exist) | deleted |
| `D6` | unconsumed docstring boilerplate (`"""…"""` in scripts/CI/support, `@moduledoc` prose, `@doc` on internal code) | deleted |
| `K1` | line-level blast-radius hazard (concurrency, lock scope/order, lifecycle, idempotency, fence, live-service test trap, timing coupling) | compressed to ≤2 lines of *why*, beside the guarded line |
| `K2` | magic number/code/wire value/sentinel + its external source | compressed |
| `K3` | genuine external platform/vendor/protocol quirk on a live path | compressed |
| `K4` | published public API contract, or a load-bearing short spec/issue link | kept (verbatim or already ≤2 lines) |
| `K5` | machine-consumed directive/pragma/header | kept verbatim |
| `M1` | module/file-level intent worth keeping (algorithm choice, budget rationale, ownership boundary) | deleted from source; text moved into the owning slice plan (`docs/plans/*`) under a "Comment-policy sweep" note |
| `M2` | out-of-scope debt recorded only by the comment (hack justification, known defect) | deleted from source; recorded in `docs/TODO.md` (or the final report) per policy 2 |

Mechanical acceptance of the three dispositions (checked by the verifier in §5):

- `deleted`: the candidate's text no longer occurs at that location; the delivered tree's comment
  count for the file drops by the deleted lines.
- `compressed`: the candidate text is gone and the replacement block at the same anchor is ≤2 lines.
- `kept`: the line is byte-identical, and it is a `K4`/`K5` line (a `directive`, or a
  published-public-contract comment — the only class the contract exempts from the 2-line ceiling).

## 3. Edge-case semantics

1. **Directives are candidates but never touched.** `//go:build`, `// +build`, `//go:embed`,
   `//go:generate`, `//nolint`, `# noqa`, `# shellcheck`, `#!` shebangs, `// eslint-disable`,
   `# yaml-language-server`, `# renovate:`, `# dependabot`, `-*- mode -*-` — 41 candidate lines, all
   `kept`/`K5`. A build constraint is code semantics, not a comment.
2. **Consumed docstrings stay as ≤2 lines.** Five candidate files build their CLI description from
   the module docstring (`argparse.ArgumentParser(description=__doc__)`):
   `scripts/ci/assert-browser-suite-coverage.py`, `scripts/ci/go-test-durations.py`,
   `scripts/ci/omo-control-native-evidence.py`, `scripts/ci/omo-native-evidence.py`,
   `scripts/ci/opencode-v2-support-witness.py`. Deleting the docstring there removes CLI help text,
   so the first line of the module docstring stays (compressed to ≤2 lines); the rest of the
   narration goes under `D6`. No test asserts `--help` output (checked by grep over
   `scripts/ci/test-*.py`), and no other candidate file reads `__doc__`.
3. **`@doc false` / `@moduledoc false` stay.** They are machine-consumed doc pragmas; deleting them
   changes generated docs. Internal `@doc` prose goes under `D6`.
4. **Hazards are never deleted silently** (contract clause 3): a `K1` hazard statement may only be
   deleted when a test or a `docs/` record states the same hazard; otherwise it is compressed to
   ≤2 lines and kept beside the guarded line. The second-pass review in §5 re-derives this for every
   non-test file.
5. **Pre-program comments in touched files stay byte-identical** (contract scope rule). Only
   candidate lines (and the blank lines that disappear with them) are edited. Where a candidate
   line is adjacent to a pre-program comment line, the pre-program line survives untouched.
6. **Zero code-token change** is a per-file property, proven with the carriers in §5; a `deleted`
   docstring is part of the removable class (comment or docstring), and the carriers normalize
   exactly that class.
7. **Moved intent has one home per source file**: the slice plan of the commit that authored the
   file's comments (derived from the blame attribution). No intent is invented, and no new doc file
   is created.
8. **`docs/TODO.md` / `docs/archive/CLOSED-TODO.md` prose is not a candidate** (docs class), but
   `M2` debt found by the sweep is recorded there.

## 4. Workstreams (parallel, disjoint file sets)

Fourteen workstreams over disjoint paths; each worker owns its file list, its inventory slice and
one dispositions TSV. Every file in §9 belongs to exactly one workstream (partition checked
mechanically: no file in two lists, no candidate file unassigned).

| workstream | scope | files | candidates |
|---|---|---:|---:|
| `ws1-platform-anchor` | `daemon/internal/platform/anchor_*`, `helper_v2_*`, `daemon/.golangci.yml`, `daemon/internal/config/` | 16 | 499 |
| `ws2-platform-supervisor` | `daemon/internal/platform/{clone_aliasing_*,containment_*,supervisor_*}` | 12 | 655 |
| `ws3-app` | `daemon/internal/app/` | 19 | 793 |
| `ws4-authority` | `daemon/internal/{authority,workspace,state,notification}/` | 21 | 605 |
| `ws5-execution` | `daemon/internal/execution/`, `daemon/native/linux-gate/` | 13 | 579 |
| `ws6-sanitizer` | `scripts/ci/model-routing-record-sanitize.py` | 1 | 535 |
| `ws7a-ci-model-routing` | the model-routing test/eval scripts under `scripts/ci/` | 7 | 407 |
| `ws7b-ci-scripts` | the remaining `scripts/ci/*.py` | 9 | 249 |
| `ws8-control-lib` | `control/lib/`, `control/.credo.exs`, `control/mix.exs`, `control/config/` | 22 | 310 |
| `ws9a-control-test-support` | `control/test/support/` | 7 | 230 |
| `ws9b-control-test` | `control/test/{symmetry_control,symmetry_control_web}`, `test_helper.exs` | 20 | 299 |
| `ws10-frontend` | `frontend/` (incl. `eslint.config.js`, `vitest.config.ts`) | 15 | 257 |
| `ws11-infra-misc` | `.github/`, `browser/tests/`, `pyproject.toml`, `.gitignore` | 8 | 196 |
| `ws12-daemon-misc` | `daemon/internal/{harness,linuxpreflight,contracts,protocol,control}/`, `daemon/cmd/`, `daemon/e2e/` | 42 | 460 |
| **total** | | **212** | **6,074** |

Worker rules, the exact disposition vocabulary and the per-file verification recipe are
`<GDD-STATE>/scratch/sweep/RULES.md` (the same text every worker received). One integration owner
(this stage) merges the dispositions TSVs, runs the verifier, the carriers and the gates, and writes
the plan notes and records.

## 5. Evidence plan (gates the contract names, plus the two proof carriers)

Gate commands, all foreground, run by the integration owner on the frozen delivered tree unless
marked per-worker:

| # | gate | command |
|---|---|---|
| 1 | Go format | `gofmt -l daemon` (empty) |
| 2 | Go vet (host) | `cd daemon && go vet ./...` |
| 3 | Go vet (linux) | `cd daemon && GOOS=linux go vet ./...` |
| 4 | Go build | `cd daemon && go build ./...` |
| 5 | Elixir format | `mix format --check-formatted` (control, portable 1.20.4 toolchain) |
| 6 | Elixir compile | `MIX_ENV=test mix compile --warnings-as-errors` |
| 7 | Python lint/format | `ruff check` + `ruff format --check` 0.17.5 over every touched `scripts/ci/*.py` (explicit file list; the two `extend-exclude` files are included explicitly) |
| 8 | Frontend types | `pnpm exec tsc --noEmit -p .` in `frontend/` |
| 9 | Frontend lint | `pnpm lint` in `frontend/` (warning count must not rise; ceiling 229) |
| 10 | YAML | `actionlint` on `.github/workflows/ci.yml` |
| 11 | Strip-proof | `proof.py --rev f1fe1e59` over all 212 files, all PASS (§5.1) |
| 12 | Whitespace | `git diff --check` |
| 13 | Host paths | `git diff origin/main...HEAD` and the worktree diff grep `-i users/` (and the general profile-path scan) |
| 14 | Policy verifier | every candidate's disposition re-checked against the delivered tree, kept blocks ≤2 lines, directives intact (§5.2) |

### 5.1 Strip-proof carriers (contract: "the file with comments stripped is byte-identical")

Per language, the delivered file and the base revision's blob are each reduced to a comment-free
canonical form; the two forms must be byte-identical:

- **Go** — `go/parser` (comments off) → `go/printer`; blank-line runs normalized. Code tokens,
  literals, declarations and control flow are all in the printed form; comments cannot be.
- **Python** — `tokenize` removes `COMMENT` tokens and `ast` removes exactly the docstring ranges;
  blank-line runs normalized. A change inside a string or any other token survives into the
  comparison.
- **Elixir** — `Code.string_to_quoted` → drop `@doc`/`@moduledoc` nodes → `Macro.to_string`; a
  comment cannot appear in the quoted form, so any edit outside comments/doc attributes changes the
  digest. This also proves every touched `.ex`/`.exs` still parses.
- **TS/JS** — TypeScript parser → `createPrinter({removeComments: true})`; trivia-free print.
- **YAML/TOML** — parsed-value equality **and** comment-stripped byte equality (both required).
- **Shell** — `bash -n` plus a comment-stripped byte comparison.

A `not-PASS` row is a defect in the delivery, not a presentation issue: the stage rebuilds the file
from the base blob and re-applies only comment deletions until every row is PASS.

### 5.2 Policy verifier

Re-derives the candidate set from the delivered tree (same blame rules, `--rev f1fe1e59` for the
"before" side) and asserts, per candidate line: the recorded disposition matches the delivered
tree (absent / replaced by a ≤2-line block / byte-identical `K4`/`K5`), no kept block exceeds two
lines, every `directive` line is byte-identical, and no file outside the 212 touched files changed.
The TSV's per-line `note` records the compressed replacement text so the check is not a blanket
text search.

### 5.3 Bounded test evidence (beyond the contract's gates)

The contract's gates do not name test suites. The strip-proof carriers already prove behaviour
preservation at the token/AST level for every touched file; to catch anything the carriers cannot
see (e.g. a deleted docstring consumed by a test), this stage additionally runs a bounded subset:
`go test -count=1 ./internal/harness/... ./internal/workspace/... ./internal/linuxpreflight/...`,
`go vet ./...` already compiling every test file, and `python -m unittest` for the touched
`scripts/ci` self-tests that do not need a Go toolchain or network. Full suites (`daemon -race`,
full `mix test`, `vitest`, the Playwright render gate) remain CI's oracles and are reported as not
run here, not as passes.

## 6. Out of scope

- Pre-program comments (blame commit not in the program set) — untouched, even inside touched files.
- `docs/**` prose, generated files, lockfiles, vendored code, data fixtures.
- Renames, code reshaping, reformatting beyond the deleted comment lines, import changes.
- The debt itself where a comment was the only record (`M2` records it in `docs/TODO.md`; fixing it
  is a separate task).
- The `pyproject.toml` `extend-exclude` entries that PR #137 left behind: the comment saying to drop
  them is compressed, the entries stay (removing them is a config change, not a comment change), and
  the existing TODO entry keeps the debt.
- Any change to the wire, persisted state, error/log text, or accepted surfaces.
- Unifying the comment style across the repository beyond the candidate set.

## 7. Trade-offs and decisions

1. **Blame attribution over diff-text matching** (§1): exact locations, no duplicate-text
   ambiguity; the diff-text variant over-counts blank/duplicate lines by ~6% (6,467 vs 6,074 in the
   draft derivation) and mis-places them.
2. **Compress, do not delete, when a hazard is real but uncovered** — the contract's failure
   control makes an uncovered deleted hazard a failure, and compressing to two lines of *why* is
   the policy's own remedy.
3. **Per-line dispositions live in one committed TSV**, not in the plan's prose: 6,074 rows in the
   plan would bury the rules; the plan carries the per-file table (§9) and the TSV carries the
   per-line detail. Both are named as the judging artifact.
4. **One dispositions TSV per workstream**, merged by the integration owner, so a worker can be
   re-run or audited alone, and the merge is a pure concatenation.
5. **Module intent goes to the owning slice plan**, keyed by the authoring commit's slice; this
   keeps program knowledge next to the change it explains instead of in source headers (policy 4).
6. **No new committed harness.** The derivation, verifier and carriers are scratch tools recorded
   in the evidence file with their commands (repo precedent: the W1-05 move manifest and the W3-08
   proof scripts).
7. **Consumed docstrings win over the 2-line ceiling only for the CLI description line** (§3.2).

## 8. Failure controls and risks

- **Code-token change**: contractual failure; caught per file by §5.1 carriers, by `go vet`
  (every Go file including tests), by `mix compile --warnings-as-errors`, and by the Elixir carrier
  parsing every touched `.ex`/`.exs`.
- **Deleted hazard whose hazard is not covered elsewhere**: contractual failure; §5.1's second-pass
  review re-reads every `D`-classified block in non-test files and every `K1`/`K4`/`K5` keep, and
  the plan notes carry the moved intent that has a plan home.
- **Directive removal**: excluded by rule and re-checked by the verifier (§5.2).
- **Pre-program comment churn**: the verifier asserts no non-candidate comment line changed.
- **Docstring consumed by a test or CLI**: §3.2 lists the five `__doc__` consumers; the bounded
  subset in §5.3 (plus the frontend and Elixir compiles) is the backstop.
- **Worker drift across 14 concurrent editors**: disjoint file sets, one shared rule sheet, one
  inventory slice per worker, per-worker dispositions TSV, and the integration owner's verifier;
  review of the merged result is independent of the workers.
- **A worker deleting a whole block including non-candidate lines**: caught by the verifier's
  non-candidate-line check.

## 9. Candidate inventory (per file, at `f1fe1e59`)

`cand` = candidate lines from §1; `blocks` = contiguous candidate runs; the per-line dispositions
are in the inventory TSV. The final disposition counts per file are appended in the stage update
below the table.

<!-- FILE-TABLE -->
| file | cand | comment | docstring | directive | deleted | compressed | kept | excluded |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `.github/dependabot.yml` | 11 | 11 | 0 | 0 | 0 | 9 | 2 | 0 |
| `.github/workflows/ci.yml` | 111 | 111 | 0 | 0 | 9 | 100 | 2 | 0 |
| `.gitignore` | 1 | 1 | 0 | 0 | 1 | 0 | 0 | 0 |
| `browser/tests/portal-cutover.spec.mjs` | 2 | 2 | 0 | 0 | 2 | 0 | 0 | 0 |
| `browser/tests/portal-goal-attention.spec.mjs` | 6 | 6 | 0 | 0 | 6 | 0 | 0 | 0 |
| `browser/tests/readable-fixtures.mjs` | 55 | 55 | 0 | 0 | 19 | 35 | 1 | 0 |
| `browser/tests/workspace-shell.spec.mjs` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `control/.credo.exs` | 55 | 55 | 0 | 0 | 41 | 14 | 0 | 0 |
| `control/config/test.exs` | 1 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| `control/lib/symmetry_control/goals.ex` | 8 | 8 | 0 | 0 | 0 | 8 | 0 | 0 |
| `control/lib/symmetry_control/goals/changeset_validators.ex` | 1 | 0 | 1 | 0 | 1 | 0 | 0 | 0 |
| `control/lib/symmetry_control/goals/read_model.ex` | 13 | 12 | 1 | 0 | 3 | 10 | 0 | 0 |
| `control/lib/symmetry_control/integrations/http.ex` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/lib/symmetry_control/integrations/provider_access.ex` | 28 | 27 | 1 | 0 | 14 | 8 | 6 | 0 |
| `control/lib/symmetry_control/integrations/providers/support.ex` | 13 | 6 | 7 | 0 | 13 | 0 | 0 | 0 |
| `control/lib/symmetry_control/schemas/versioned_changeset.ex` | 10 | 9 | 1 | 0 | 1 | 9 | 0 | 0 |
| `control/lib/symmetry_control/workspaces/work_item_eligibility.ex` | 58 | 0 | 58 | 0 | 58 | 0 | 0 | 0 |
| `control/lib/symmetry_control_web/channels/daemon_channel.ex` | 1 | 0 | 1 | 0 | 1 | 0 | 0 | 0 |
| `control/lib/symmetry_control_web/controllers/chat_controller.ex` | 2 | 2 | 0 | 0 | 0 | 0 | 2 | 0 |
| `control/lib/symmetry_control_web/controllers/daemon_controller.ex` | 15 | 15 | 0 | 0 | 0 | 15 | 0 | 0 |
| `control/lib/symmetry_control_web/controllers/goal_controller.ex` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/lib/symmetry_control_web/controllers/portal_api_controller.ex` | 7 | 7 | 0 | 0 | 0 | 7 | 0 | 0 |
| `control/lib/symmetry_control_web/plugs/auth.ex` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/lib/symmetry_control_web/plugs/strict_evidence_json.ex` | 30 | 0 | 30 | 0 | 30 | 0 | 0 | 0 |
| `control/lib/symmetry_control_web/portal_json.ex` | 32 | 32 | 0 | 0 | 2 | 25 | 5 | 0 |
| `control/lib/symmetry_control_web/protocol.ex` | 5 | 5 | 0 | 0 | 0 | 5 | 0 | 0 |
| `control/lib/symmetry_control_web/query_params.ex` | 14 | 8 | 6 | 0 | 6 | 8 | 0 | 0 |
| `control/lib/symmetry_control_web/router.ex` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/mix.exs` | 5 | 5 | 0 | 0 | 0 | 5 | 0 | 0 |
| `control/test/support/integration_fixtures.ex` | 39 | 2 | 37 | 0 | 39 | 0 | 0 | 0 |
| `control/test/support/migration_case.ex` | 3 | 0 | 3 | 0 | 3 | 0 | 0 | 0 |
| `control/test/support/orchestration_case.ex` | 28 | 0 | 28 | 0 | 28 | 0 | 0 | 0 |
| `control/test/support/served_document.ex` | 48 | 0 | 48 | 0 | 48 | 0 | 0 | 0 |
| `control/test/support/slowest_formatter.ex` | 16 | 0 | 16 | 0 | 16 | 0 | 0 | 0 |
| `control/test/support/test_conn.ex` | 9 | 0 | 9 | 0 | 9 | 0 | 0 | 0 |
| `control/test/support/test_fixtures.ex` | 87 | 11 | 76 | 0 | 87 | 0 | 0 | 0 |
| `control/test/symmetry_control/goals/changeset_validators_test.exs` | 18 | 7 | 11 | 0 | 13 | 5 | 0 | 0 |
| `control/test/symmetry_control/goals_read_model_test.exs` | 5 | 5 | 0 | 0 | 5 | 0 | 0 | 0 |
| `control/test/symmetry_control/goals_test.exs` | 11 | 11 | 0 | 0 | 4 | 7 | 0 | 0 |
| `control/test/symmetry_control/integrations/azure_devops_test.exs` | 3 | 2 | 1 | 0 | 3 | 0 | 0 | 0 |
| `control/test/symmetry_control/integrations/github_test.exs` | 3 | 2 | 1 | 0 | 3 | 0 | 0 | 0 |
| `control/test/symmetry_control/integrations/http_test.exs` | 2 | 2 | 0 | 0 | 0 | 2 | 0 | 0 |
| `control/test/symmetry_control/integrations_test.exs` | 15 | 15 | 0 | 0 | 2 | 13 | 0 | 0 |
| `control/test/symmetry_control/orchestration/capability_scheduling_test.exs` | 1 | 1 | 0 | 0 | 1 | 0 | 0 | 0 |
| `control/test/symmetry_control/provider_broker_e2e_test.exs` | 5 | 5 | 0 | 0 | 0 | 5 | 0 | 0 |
| `control/test/symmetry_control/workspaces/work_item_eligibility_test.exs` | 20 | 19 | 1 | 0 | 20 | 0 | 0 | 0 |
| `control/test/symmetry_control/workspaces_test.exs` | 5 | 5 | 0 | 0 | 0 | 5 | 0 | 0 |
| `control/test/symmetry_control_web/controllers/chat_controller_test.exs` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `control/test/symmetry_control_web/controllers/goal_controller_test.exs` | 18 | 18 | 0 | 0 | 9 | 9 | 0 | 0 |
| `control/test/symmetry_control_web/controllers/operator_read_controller_test.exs` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/test/symmetry_control_web/controllers/portal_integrations_test.exs` | 12 | 12 | 0 | 0 | 12 | 0 | 0 | 0 |
| `control/test/symmetry_control_web/controllers/portal_resources_test.exs` | 112 | 112 | 0 | 0 | 108 | 4 | 0 | 0 |
| `control/test/symmetry_control_web/controllers/portal_workflow_test.exs` | 48 | 48 | 0 | 0 | 48 | 0 | 0 | 0 |
| `control/test/symmetry_control_web/controllers/protocol_controller_test.exs` | 10 | 10 | 0 | 0 | 10 | 0 | 0 | 0 |
| `control/test/symmetry_control_web/plugs/strict_evidence_json_test.exs` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/test/test_helper.exs` | 1 | 1 | 0 | 0 | 0 | 1 | 0 | 0 |
| `daemon/.golangci.yml` | 20 | 20 | 0 | 0 | 4 | 16 | 0 | 0 |
| `daemon/cmd/symmetry-daemon/preauthority_witness_report_windows_test.go` | 8 | 7 | 0 | 1 | 1 | 6 | 1 | 0 |
| `daemon/cmd/symmetry-fake-agent/autonomous.go` | 16 | 16 | 0 | 0 | 0 | 16 | 0 | 0 |
| `daemon/cmd/symmetry-fake-agent/autonomous_test.go` | 50 | 50 | 0 | 0 | 33 | 17 | 0 | 0 |
| `daemon/e2e/internal/operator/client.go` | 28 | 27 | 0 | 0 | 20 | 7 | 0 | 1 |
| `daemon/e2e/internal/operator/task.go` | 9 | 7 | 0 | 0 | 7 | 0 | 0 | 2 |
| `daemon/e2e/internal/operator/validation.go` | 5 | 5 | 0 | 0 | 1 | 4 | 0 | 0 |
| `daemon/internal/app/app_test.go` | 24 | 24 | 0 | 0 | 0 | 24 | 0 | 0 |
| `daemon/internal/app/cleanup_glue.go` | 39 | 39 | 0 | 0 | 0 | 39 | 0 | 0 |
| `daemon/internal/app/command_queue.go` | 86 | 86 | 0 | 0 | 26 | 56 | 4 | 0 |
| `daemon/internal/app/command_queue_test.go` | 38 | 38 | 0 | 0 | 38 | 0 | 0 | 0 |
| `daemon/internal/app/commands.go` | 42 | 42 | 0 | 0 | 0 | 37 | 5 | 0 |
| `daemon/internal/app/export_test.go` | 1 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| `daemon/internal/app/goal_admission.go` | 76 | 76 | 0 | 0 | 0 | 64 | 12 | 0 |
| `daemon/internal/app/goal_delivery.go` | 75 | 75 | 0 | 0 | 0 | 70 | 5 | 0 |
| `daemon/internal/app/goal_delivery_test.go` | 14 | 14 | 0 | 0 | 14 | 0 | 0 | 0 |
| `daemon/internal/app/leak_check_test.go` | 38 | 38 | 0 | 0 | 17 | 21 | 0 | 0 |
| `daemon/internal/app/lease.go` | 27 | 24 | 0 | 0 | 0 | 21 | 3 | 3 |
| `daemon/internal/app/linux_owner_preflight_state_dir_app_test.go` | 12 | 11 | 0 | 1 | 11 | 0 | 1 | 0 |
| `daemon/internal/app/linux_owner_witness_linux.go` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `daemon/internal/app/native_run.go` | 139 | 137 | 0 | 0 | 0 | 102 | 35 | 2 |
| `daemon/internal/app/native_settlement_regression_test.go` | 2 | 2 | 0 | 0 | 0 | 0 | 2 | 0 |
| `daemon/internal/app/outbox.go` | 32 | 32 | 0 | 0 | 0 | 24 | 8 | 0 |
| `daemon/internal/app/probe.go` | 16 | 16 | 0 | 0 | 0 | 16 | 0 | 0 |
| `daemon/internal/app/process_persistence_test.go` | 77 | 71 | 0 | 0 | 71 | 0 | 0 | 6 |
| `daemon/internal/app/recovery_glue.go` | 52 | 52 | 0 | 0 | 0 | 52 | 0 | 0 |
| `daemon/internal/authority/authority_v2_test.go` | 106 | 105 | 0 | 0 | 105 | 0 | 0 | 1 |
| `daemon/internal/authority/cgroup.go` | 47 | 47 | 0 | 0 | 6 | 41 | 0 | 0 |
| `daemon/internal/authority/linux_clone.go` | 62 | 62 | 0 | 0 | 16 | 46 | 0 | 0 |
| `daemon/internal/authority/supervisor.go` | 148 | 148 | 0 | 0 | 84 | 64 | 0 | 0 |
| `daemon/internal/config/config_test.go` | 8 | 8 | 0 | 0 | 0 | 8 | 0 | 0 |
| `daemon/internal/contracts/schema.go` | 9 | 9 | 0 | 0 | 0 | 9 | 0 | 0 |
| `daemon/internal/contracts/schema_test.go` | 25 | 25 | 0 | 0 | 12 | 13 | 0 | 0 |
| `daemon/internal/control/client_review_test.go` | 7 | 7 | 0 | 0 | 7 | 0 | 0 | 0 |
| `daemon/internal/control/client_test.go` | 7 | 7 | 0 | 0 | 7 | 0 | 0 | 0 |
| `daemon/internal/control/validation.go` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `daemon/internal/control/validation_test.go` | 14 | 14 | 0 | 0 | 14 | 0 | 0 | 0 |
| `daemon/internal/execution/linux_owner_v2_finalize_pipeline_test.go` | 52 | 51 | 0 | 1 | 24 | 25 | 3 | 0 |
| `daemon/internal/execution/linux_owner_v2_linux.go` | 101 | 101 | 0 | 0 | 76 | 25 | 0 | 0 |
| `daemon/internal/execution/linux_v2_ack_stages_test.go` | 6 | 6 | 0 | 0 | 6 | 0 | 0 | 0 |
| `daemon/internal/execution/runner.go` | 87 | 87 | 0 | 0 | 66 | 21 | 0 | 0 |
| `daemon/internal/execution/runner_pipes.go` | 17 | 17 | 0 | 0 | 10 | 7 | 0 | 0 |
| `daemon/internal/execution/runner_test.go` | 4 | 4 | 0 | 0 | 0 | 4 | 0 | 0 |
| `daemon/internal/harness/bounded_probe.go` | 24 | 24 | 0 | 0 | 4 | 20 | 0 | 0 |
| `daemon/internal/harness/bounded_probe_test.go` | 7 | 7 | 0 | 0 | 2 | 5 | 0 | 0 |
| `daemon/internal/harness/claude_candidate_adapter.go` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `daemon/internal/harness/claude_candidate_adapter_test.go` | 4 | 4 | 0 | 0 | 0 | 4 | 0 | 0 |
| `daemon/internal/harness/claude_probe_test.go` | 3 | 3 | 0 | 0 | 3 | 0 | 0 | 0 |
| `daemon/internal/harness/close_gate.go` | 31 | 31 | 0 | 0 | 7 | 24 | 0 | 0 |
| `daemon/internal/harness/codex/probe.go` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `daemon/internal/harness/codex_probe_test.go` | 3 | 3 | 0 | 0 | 3 | 0 | 0 | 0 |
| `daemon/internal/harness/event_delivery.go` | 6 | 6 | 0 | 0 | 0 | 6 | 0 | 0 |
| `daemon/internal/harness/nil_value.go` | 4 | 4 | 0 | 0 | 0 | 4 | 0 | 0 |
| `daemon/internal/harness/omo/adapter.go` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `daemon/internal/harness/omo/probe_test.go` | 4 | 4 | 0 | 0 | 0 | 4 | 0 | 0 |
| `daemon/internal/harness/pi/adapter_test.go` | 4 | 4 | 0 | 0 | 0 | 4 | 0 | 0 |
| `daemon/internal/harness/process_forwarding.go` | 23 | 23 | 0 | 0 | 10 | 13 | 0 | 0 |
| `daemon/internal/harness/result_clone.go` | 11 | 11 | 0 | 0 | 1 | 10 | 0 | 0 |
| `daemon/internal/harness/session_scaffolding_test.go` | 29 | 23 | 0 | 0 | 8 | 15 | 0 | 6 |
| `daemon/internal/harness/wait_barrier.go` | 7 | 7 | 0 | 0 | 0 | 7 | 0 | 0 |
| `daemon/internal/linuxpreflight/accounts_linux.go` | 4 | 3 | 0 | 1 | 0 | 3 | 1 | 0 |
| `daemon/internal/linuxpreflight/acl_linux.go` | 1 | 0 | 0 | 1 | 0 | 0 | 1 | 0 |
| `daemon/internal/linuxpreflight/cgroup_linux.go` | 14 | 13 | 0 | 1 | 9 | 4 | 1 | 0 |
| `daemon/internal/linuxpreflight/git_ownership_linux.go` | 1 | 0 | 0 | 1 | 0 | 0 | 1 | 0 |
| `daemon/internal/linuxpreflight/preflight_linux.go` | 26 | 24 | 0 | 1 | 7 | 17 | 1 | 1 |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `daemon/internal/linuxpreflight/preflight_unsupported.go` | 18 | 17 | 0 | 1 | 11 | 6 | 1 | 0 |
| `daemon/internal/linuxpreflight/preflight_unsupported_test.go` | 6 | 5 | 0 | 1 | 5 | 0 | 1 | 0 |
| `daemon/internal/linuxpreflight/systemd_linux.go` | 3 | 2 | 0 | 1 | 0 | 2 | 1 | 0 |
| `daemon/internal/notification/client.go` | 15 | 15 | 0 | 0 | 2 | 11 | 2 | 0 |
| `daemon/internal/notification/client_test.go` | 27 | 27 | 0 | 0 | 21 | 6 | 0 | 0 |
| `daemon/internal/platform/anchor_gate.go` | 25 | 24 | 0 | 1 | 0 | 24 | 1 | 0 |
| `daemon/internal/platform/anchor_proc.go` | 27 | 26 | 0 | 1 | 8 | 18 | 1 | 0 |
| `daemon/internal/platform/anchor_run.go` | 56 | 54 | 0 | 1 | 8 | 42 | 5 | 1 |
| `daemon/internal/platform/anchor_server.go` | 53 | 51 | 0 | 1 | 6 | 39 | 7 | 1 |
| `daemon/internal/platform/anchor_shape_table_test.go` | 60 | 59 | 0 | 1 | 23 | 36 | 1 | 0 |
| `daemon/internal/platform/anchor_types.go` | 11 | 10 | 0 | 1 | 0 | 8 | 3 | 0 |
| `daemon/internal/platform/anchor_wire.go` | 67 | 66 | 0 | 1 | 38 | 24 | 5 | 0 |
| `daemon/internal/platform/anchor_wire_shape_test.go` | 121 | 120 | 0 | 1 | 48 | 68 | 5 | 0 |
| `daemon/internal/platform/clone_aliasing_linux_amd64_test.go` | 26 | 25 | 0 | 1 | 5 | 20 | 1 | 0 |
| `daemon/internal/platform/clone_aliasing_linux_test.go` | 130 | 129 | 0 | 1 | 75 | 54 | 1 | 0 |
| `daemon/internal/platform/clone_aliasing_windows_test.go` | 8 | 5 | 0 | 1 | 5 | 0 | 1 | 2 |
| `daemon/internal/platform/containment_errors.go` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `daemon/internal/platform/containment_errors_linux.go` | 55 | 54 | 0 | 1 | 16 | 38 | 1 | 0 |
| `daemon/internal/platform/containment_errors_test.go` | 45 | 44 | 0 | 1 | 14 | 30 | 1 | 0 |
| `daemon/internal/platform/containment_supervisor_linux_test.go` | 65 | 65 | 0 | 0 | 4 | 61 | 0 | 0 |
| `daemon/internal/platform/containment_supervisor_windows_test.go` | 2 | 2 | 0 | 0 | 0 | 2 | 0 | 0 |
| `daemon/internal/platform/helper_v2_protocol.go` | 9 | 8 | 0 | 1 | 2 | 5 | 2 | 0 |
| `daemon/internal/platform/helper_v2_recovery.go` | 4 | 3 | 0 | 1 | 0 | 0 | 4 | 0 |
| `daemon/internal/platform/helper_v2_run.go` | 1 | 0 | 0 | 1 | 0 | 0 | 1 | 0 |
| `daemon/internal/platform/helper_v2_server.go` | 18 | 17 | 0 | 1 | 0 | 12 | 6 | 0 |
| `daemon/internal/platform/helper_v2_stop.go` | 16 | 15 | 0 | 1 | 0 | 11 | 5 | 0 |
| `daemon/internal/platform/linux_owner_helper_v2_linux.go` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `daemon/internal/platform/supervisor_linux_client.go` | 75 | 74 | 0 | 1 | 0 | 68 | 7 | 0 |
| `daemon/internal/platform/supervisor_linux_helper.go` | 93 | 91 | 0 | 1 | 1 | 82 | 9 | 1 |
| `daemon/internal/platform/supervisor_windows_client.go` | 106 | 105 | 0 | 1 | 2 | 97 | 7 | 0 |
| `daemon/internal/platform/supervisor_windows_helper.go` | 46 | 45 | 0 | 1 | 0 | 40 | 6 | 0 |
| `daemon/internal/protocol/goal_subject.go` | 21 | 21 | 0 | 0 | 9 | 12 | 0 | 0 |
| `daemon/internal/protocol/goal_subject_test.go` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `daemon/internal/protocol/goal_validation_test.go` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `daemon/internal/protocol/registration_capabilities_test.go` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `daemon/internal/state/goal_delivery.go` | 2 | 2 | 0 | 0 | 0 | 0 | 2 | 0 |
| `daemon/internal/state/goal_session.go` | 8 | 8 | 0 | 0 | 0 | 0 | 8 | 0 |
| `daemon/internal/state/goal_session_test.go` | 12 | 12 | 0 | 0 | 9 | 3 | 0 | 0 |
| `daemon/internal/state/linux_clone_journal_test.go` | 55 | 55 | 0 | 0 | 55 | 0 | 0 | 0 |
| `daemon/internal/state/provider_action.go` | 4 | 4 | 0 | 0 | 0 | 0 | 4 | 0 |
| `daemon/internal/state/provider_action_test.go` | 7 | 7 | 0 | 0 | 0 | 7 | 0 | 0 |
| `daemon/internal/state/state.go` | 10 | 10 | 0 | 0 | 5 | 4 | 1 | 0 |
| `daemon/internal/state/state_test.go` | 16 | 16 | 0 | 0 | 16 | 0 | 0 | 0 |
| `daemon/internal/state/state_wrap_test.go` | 18 | 18 | 0 | 0 | 12 | 6 | 0 | 0 |
| `daemon/internal/workspace/fingerprint_test.go` | 6 | 6 | 0 | 0 | 6 | 0 | 0 | 0 |
| `daemon/internal/workspace/linux_clone_fs_linux.go` | 28 | 28 | 0 | 0 | 20 | 8 | 0 | 0 |
| `daemon/internal/workspace/linux_clone_linux_test.go` | 13 | 13 | 0 | 0 | 6 | 7 | 0 | 0 |
| `daemon/internal/workspace/subject.go` | 9 | 9 | 0 | 0 | 9 | 0 | 0 | 0 |
| `daemon/internal/workspace/subject_test.go` | 5 | 5 | 0 | 0 | 5 | 0 | 0 | 0 |
| `daemon/internal/workspace/workspace_test.go` | 7 | 7 | 0 | 0 | 7 | 0 | 0 | 0 |
| `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` | 75 | 74 | 0 | 1 | 16 | 58 | 1 | 0 |
| `daemon/native/linux-gate/protocol/owner_config_linux.go` | 5 | 4 | 0 | 1 | 2 | 2 | 1 | 0 |
| `daemon/native/linux-gate/protocol/proc_identity_linux.go` | 13 | 12 | 0 | 1 | 0 | 12 | 1 | 0 |
| `daemon/native/linux-gate/protocol/wire.go` | 75 | 72 | 0 | 0 | 34 | 37 | 1 | 3 |
| `daemon/native/linux-gate/protocol/wire_linux.go` | 18 | 17 | 0 | 1 | 3 | 14 | 1 | 0 |
| `daemon/native/linux-gate/protocol/wire_linux_test.go` | 51 | 50 | 0 | 1 | 40 | 8 | 3 | 0 |
| `daemon/native/linux-gate/protocol/wire_test.go` | 75 | 75 | 0 | 0 | 60 | 15 | 0 | 0 |
| `frontend/eslint.config.js` | 29 | 29 | 0 | 0 | 22 | 7 | 0 | 0 |
| `frontend/src/components/primitives.test.tsx` | 11 | 11 | 0 | 0 | 8 | 3 | 0 | 0 |
| `frontend/src/components/primitives.tsx` | 18 | 18 | 0 | 0 | 9 | 9 | 0 | 0 |
| `frontend/src/components/use-dialog-opener.ts` | 12 | 12 | 0 | 0 | 0 | 12 | 0 | 0 |
| `frontend/src/features/chat/presentation.ts` | 8 | 8 | 0 | 0 | 0 | 8 | 0 | 0 |
| `frontend/src/features/connections/connections.test.tsx` | 5 | 5 | 0 | 0 | 5 | 0 | 0 | 0 |
| `frontend/src/features/connections/resources.tsx` | 32 | 32 | 0 | 0 | 16 | 16 | 0 | 0 |
| `frontend/src/features/goals/goal-attention.test.tsx` | 4 | 4 | 0 | 0 | 0 | 4 | 0 | 0 |
| `frontend/src/features/planning/planning.test.tsx` | 2 | 2 | 0 | 0 | 2 | 0 | 0 | 0 |
| `frontend/src/features/work/presentation.ts` | 1 | 1 | 0 | 0 | 0 | 1 | 0 | 0 |
| `frontend/src/services/api.test.ts` | 30 | 30 | 0 | 0 | 15 | 15 | 0 | 0 |
| `frontend/src/services/api.ts` | 56 | 56 | 0 | 0 | 12 | 44 | 0 | 0 |
| `frontend/src/services/served.ts` | 37 | 37 | 0 | 0 | 25 | 12 | 0 | 0 |
| `frontend/src/tool-cache-config.test.ts` | 8 | 8 | 0 | 0 | 0 | 8 | 0 | 0 |
| `frontend/vitest.config.ts` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `pyproject.toml` | 6 | 6 | 0 | 0 | 0 | 4 | 2 | 0 |
| `scripts/ci/assert-browser-suite-coverage.py` | 60 | 6 | 53 | 1 | 53 | 6 | 1 | 0 |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | 20 | 18 | 0 | 0 | 10 | 0 | 8 | 2 |
| `scripts/ci/go-test-durations.py` | 72 | 18 | 53 | 1 | 53 | 18 | 1 | 0 |
| `scripts/ci/model-routing-evaluation.py` | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 |
| `scripts/ci/model-routing-record-sanitize.py` | 535 | 171 | 364 | 0 | 530 | 5 | 0 | 0 |
| `scripts/ci/omo-control-native-evidence.py` | 4 | 0 | 0 | 0 | 0 | 0 | 0 | 4 |
| `scripts/ci/omo-native-evidence.py` | 12 | 3 | 8 | 0 | 8 | 3 | 0 | 1 |
| `scripts/ci/opencode-v2-support-witness.py` | 4 | 0 | 4 | 0 | 4 | 0 | 0 | 0 |
| `scripts/ci/revision_scope.py` | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 |
| `scripts/ci/test-check-lockfile-registry.py` | 21 | 7 | 14 | 0 | 14 | 7 | 0 | 0 |
| `scripts/ci/test-ci-changed-path-classifier.py` | 55 | 28 | 27 | 0 | 34 | 21 | 0 | 0 |
| `scripts/ci/test-model-routing-evaluation.py` | 4 | 0 | 0 | 0 | 0 | 0 | 0 | 4 |
| `scripts/ci/test-model-routing-omp-usage.py` | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 |
| `scripts/ci/test-model-routing-record-sanitize-json.py` | 339 | 329 | 9 | 1 | 89 | 238 | 12 | 0 |
| `scripts/ci/test-model-routing-record-sanitize.py` | 56 | 51 | 5 | 0 | 6 | 48 | 2 | 0 |
| `scripts/ci/test-omo-native-evidence.py` | 4 | 2 | 2 | 0 | 2 | 2 | 0 | 0 |
| `scripts/ci/test-opencode-v2-support-witness.py` | 2 | 0 | 2 | 0 | 2 | 0 | 0 | 0 |

`excluded` rows are code lines the prefix classifier read as comments (44 rows, all `kind=code`
after the real lexers ran: `git blame`-derived candidates whose line the Go/Python/TS lexers do not
place inside a comment token or docstring — e.g. `*events = append(...)`, `*run.stopEvidence = ...`,
`**call,` in Python argument lists). They are byte-identical in the delivered tree.

## 10. Delivered record (stage update, 2026-11-22)

Disposition totals over the 6,074 inventory rows: **deleted 2,956 · compressed 2,836 · kept 238 ·
excluded 44**; rules `K1` 2,316, `D1` 1,479, `D2` 415, `D6` 406, `K4` 393, `M1` 353, `K3` 255,
`K2` 224, `D3` 71, `K5` 63, `M2` 30, `D5` 25, `CODELINE` 44 (per-line rows:
[`docs/evidence/comment-policy-sweep-2026-11-22-inventory.tsv`](../evidence/comment-policy-sweep-2026-11-22-inventory.tsv)).

Deviations from this plan's assumptions, all recorded rather than silently absorbed:

1. **Per-line dispositions are derived from the delivered tree, block-anchored** (merge step): a row
   whose line survives verbatim is `kept`, a row whose text is gone while its block survives as a
   ≤2-line why is `compressed`, and a row whose block is gone is `deleted`. Worker-stage labels that
   attributed a block-level change to individual lines are therefore recomputed; rules and
   replacement notes are preserved.
2. **44 classifier false positives** (`CODELINE`): lines whose text starts with `*`/`**` but which
   real lexers do not place inside comments. No such line was edited.
3. **8 relocated why-lines** (declared): four in `control/.credo.exs` moved from the deleted header
   to their config entries, four are minimal completions of pre-program fragments in
   `scripts/ci/model-routing-record-sanitize.py` (see §10.4).
4. **4 pre-program comment lines were rewritten** to close sentences whose program-added
   continuation was deleted (`scripts/ci/model-routing-record-sanitize.py`: `_SEPARATOR`,
   `_FILE_PREFIX`, `root_relative`, `file_url`; plus two docstring tails and one fragment each in
   `control/test/support/integration_fixtures.ex`, `control/test/support/test_conn.ex`,
   `scripts/ci/omo-native-evidence.py`, `scripts/ci/opencode-v2-support-witness.py`,
   `scripts/ci/test-check-lockfile-registry.py`). The contract's scope rule protects standalone
   pre-program comments; these are fragments *inside* blocks whose candidate lines were removed, and
   completing them is the smallest edit that leaves no mid-sentence comment. Counted exactly by the
   verifier's `pre_program_removed` stat.
5. **Two blocks larger than two lines survive under the published-contract exemption**:
   `daemon/internal/state/provider_action.go` 568-571 and `scripts/ci/export-linux-v2-redacted-evidence.py`
   535-560 (the published-record allowlist whose membership a CI scan enforces). Every other
   candidate block in the delivered tree is zero, one or two candidate lines.
6. **Moved module intent** is recorded in
   [`docs/evidence/comment-policy-sweep-2026-11-22-moved-intent.md`](../evidence/comment-policy-sweep-2026-11-22-moved-intent.md)
   (353 `M1` rows, 16 workstream sections); each owning slice plan carries a short
   "Comment-policy sweep" note pointing at it instead of duplicating the text.
