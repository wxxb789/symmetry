# comment-policy-sweep plan — apply the 2026-11-22 owner comment policy to the program's pre-policy comments

Contract: `goal-comment-policy-sweep.md` (GDD, 2026-11-22; held until the pre-policy land queue
merged — it has: `origin/main` is `f1fe1e59`, and the sweep is dispatched as a program slice of goal
0010). Base revision for every coordinate in this plan: `f1fe1e59`. Delivered revision: the shipped
head `465ffd1b` (`f1fe1e59` plus the sweep commits `16a7e7d3` and `0399d5f1` and the `origin/main`
merges, the last bringing `origin/main` to `cca92284`) plus the apply-stage and fix-stage
working-tree changes; when a line number is cited it resolves at the base unless a stage note says
otherwise.

Plans consulted as the shape reference: `2026-11-21-core-dead-surface-plan.md` (definition of the
judging artifact and the failure controls), `2026-11-21-rf-w3-13-http-boundary-plan.md` (stage
updates and per-file proof carriers), `2026-11-21-rf-program-plan.md` (slice/worker rules).

## 1. Candidate derivation (mechanical, re-runnable)

- **Program commits** = the 68 first-parent commits on `main` after the program's base revision
  `a3039f0` (`git log --first-parent --pretty=%H a3039f0..f1fe1e59`), i.e. the land queue's slices
  (`rf-*` 138-214, `fix/*` 137/180/185/194/195, `docs/*` 182/207/208) plus the queue's dependabot
  and npm bumps: `a3039f0` is the plan's base revision and the first program landing after it is
  `6099f26f` (#138, 2026-11-21 08:20).
- **Cutoff** (fix stage, review R2): the candidate set is frozen at the dispatch base `f1fe1e59` —
  the contract's "dispatch after the land queue has merged" premise. Eight further program slices
  landed on `main` after the freeze and reached the branch through its `origin/main` merges
  (`9f82fd8e` #210, `12de9428` #212, `3f73cc73` #213, `394859a9` #215, `e3864c1e` #216, `de526861`
  #217, `e0782105` #218, `cca92284` #219; 2026-11-22 05:39–08:03). Read without an upper bound, the
  contract's "still present at HEAD" rule also selects the comment/docstring lines these slices
  added: 88 lines still present at `465ffd1b` (5 directives; per slice `9f82fd8e` 21, `12de9428`
  33, `3f73cc73` 25, `cca92284` 9; re-computed at the fix stage as each slice's `git diff <slice>^
  <slice>` added lines ∩ present at HEAD, classified per language), of which 31 are in files §9
  lists and 57 in post-base files, including 4 contiguous runs longer than two lines (24 lines:
  `.github/workflows/ci.yml` 32–34, 92–96 and 1722–1724, `daemon/internal/app/run_lifecycle.go`
  76–88). Those slices are post-dispatch and post-policy and own their comments; the sweep owns
  only the frozen set. (An earlier revision of this bullet claimed the 68-commit set was selected
  "exactly"; that claim was false at the shipped head and this cutoff replaces it.)
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

Result: **6,074 candidate lines in 212 files** — 5,111 `comment`, 878 `docstring`, 41 `directive`;
the remaining 44 rows are code lines the prefix classifier read as comments but the real lexers do
not (`excluded`, §9). They form 1,651 contiguous blocks (938 of them longer than 2 lines, 4,961
lines in those blocks).

Per-file candidate counts (§9) and the per-line inventory with dispositions (one row per candidate;
kept off-repo per the conductor amendment, §11) are the judging artifacts; the inventory is
generated from this derivation and re-checked against the delivered revision by the verifier named
in §5 and §11.

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
- `kept`: the line's text is byte-identical in the delivered tree (survivors include `K4`/`K5`
  lines, `@moduledoc false` pragmas and `@doc`/`@moduledoc`/docstring boundary lines); only `K4`/`K5`
  blocks are exempt from the 2-line ceiling. Every kept block longer than one line is listed in §11.

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
   ≤2 lines and kept beside the guarded line. The controls that re-check this are named and real:
   the strip-proof carriers (§5.1) hold the code side fixed, the policy verifier (§5.2) re-derives
   every disposition against the delivered tree, and the fix stage's hazard re-review (§11.6)
   re-read every deleted hazard-flavoured row (112 rows, 0 without coverage). This paragraph
   previously cited a "second-pass review in §5" that no stage record described; the fix stage
   removed that claim (review R3).
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
| 13 | Host paths | `git diff origin/main...HEAD` and the worktree diff, scanned case-insensitively for user-profile path components (contract gate 13; the exact pattern lives in the contract file) |
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
3. **Per-line dispositions live in one TSV, kept off-repo** (conductor amendment, §11): 6,074 rows
   in the plan would bury the rules; the plan carries the per-file table (§9) plus every kept block
   longer than one line (§11), and the off-repo artifact carries the per-line detail. Both are the
   judging artifact.
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
- **Deleted hazard whose hazard is not covered elsewhere**: contractual failure; `K1`/`K2`/`K3`
  candidates are compressed rather than deleted (§2/§3.4), the policy verifier re-derives every
  disposition (§5.2), and the fix stage's hazard re-review re-read every deleted hazard-flavoured
  row — 112 rows (`K1` 64, `K2` 14, `K3` 7, `K4` 5, `M2` 22), 0 without a test or `docs/` record
  (§11.6); the moved intent lives in the owning slice plans (§10.6).
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

`cand` = candidate lines from §1; `blocks` = contiguous candidate runs. The counts are the
delivered-revision record, re-derived at the 2026-11-22 apply stage (§11); the per-line dispositions
are in the off-repo inventory artifact (§11). `kept` is kept-with-reason — every kept row carries its
rule — and every kept block longer than one line is listed in §11.

<!-- FILE-TABLE -->
| file | cand | comment | docstring | directive | deleted | compressed | kept | excluded |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `.github/dependabot.yml` | 11 | 11 | 0 | 0 | 6 | 3 | 2 | 0 |
| `.github/workflows/ci.yml` | 111 | 111 | 0 | 0 | 9 | 100 | 2 | 0 |
| `.gitignore` | 1 | 1 | 0 | 0 | 1 | 0 | 0 | 0 |
| `browser/tests/portal-cutover.spec.mjs` | 2 | 2 | 0 | 0 | 2 | 0 | 0 | 0 |
| `browser/tests/portal-goal-attention.spec.mjs` | 6 | 6 | 0 | 0 | 6 | 0 | 0 | 0 |
| `browser/tests/readable-fixtures.mjs` | 55 | 55 | 0 | 0 | 22 | 33 | 0 | 0 |
| `browser/tests/workspace-shell.spec.mjs` | 4 | 4 | 0 | 0 | 4 | 0 | 0 | 0 |
| `control/.credo.exs` | 55 | 55 | 0 | 0 | 41 | 14 | 0 | 0 |
| `control/config/test.exs` | 1 | 1 | 0 | 0 | 0 | 0 | 1 | 0 |
| `control/lib/symmetry_control/goals.ex` | 8 | 8 | 0 | 0 | 0 | 8 | 0 | 0 |
| `control/lib/symmetry_control/goals/changeset_validators.ex` | 1 | 0 | 1 | 0 | 0 | 0 | 1 | 0 |
| `control/lib/symmetry_control/goals/read_model.ex` | 13 | 12 | 1 | 0 | 2 | 10 | 1 | 0 |
| `control/lib/symmetry_control/integrations/http.ex` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/lib/symmetry_control/integrations/provider_access.ex` | 28 | 27 | 1 | 0 | 13 | 8 | 7 | 0 |
| `control/lib/symmetry_control/integrations/providers/support.ex` | 13 | 6 | 7 | 0 | 12 | 0 | 1 | 0 |
| `control/lib/symmetry_control/schemas/versioned_changeset.ex` | 10 | 9 | 1 | 0 | 0 | 9 | 1 | 0 |
| `control/lib/symmetry_control/workspaces/work_item_eligibility.ex` | 58 | 0 | 58 | 0 | 7 | 41 | 10 | 0 |
| `control/lib/symmetry_control_web/channels/daemon_channel.ex` | 1 | 0 | 1 | 0 | 0 | 0 | 1 | 0 |
| `control/lib/symmetry_control_web/controllers/chat_controller.ex` | 2 | 2 | 0 | 0 | 0 | 0 | 2 | 0 |
| `control/lib/symmetry_control_web/controllers/daemon_controller.ex` | 15 | 15 | 0 | 0 | 0 | 15 | 0 | 0 |
| `control/lib/symmetry_control_web/controllers/goal_controller.ex` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/lib/symmetry_control_web/controllers/portal_api_controller.ex` | 7 | 7 | 0 | 0 | 0 | 7 | 0 | 0 |
| `control/lib/symmetry_control_web/plugs/auth.ex` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/lib/symmetry_control_web/plugs/strict_evidence_json.ex` | 30 | 0 | 30 | 0 | 4 | 24 | 2 | 0 |
| `control/lib/symmetry_control_web/portal_json.ex` | 32 | 32 | 0 | 0 | 2 | 25 | 5 | 0 |
| `control/lib/symmetry_control_web/protocol.ex` | 5 | 5 | 0 | 0 | 0 | 5 | 0 | 0 |
| `control/lib/symmetry_control_web/query_params.ex` | 14 | 8 | 6 | 0 | 4 | 8 | 2 | 0 |
| `control/lib/symmetry_control_web/router.ex` | 3 | 3 | 0 | 0 | 0 | 3 | 0 | 0 |
| `control/mix.exs` | 5 | 5 | 0 | 0 | 0 | 5 | 0 | 0 |
| `control/test/support/integration_fixtures.ex` | 39 | 2 | 37 | 0 | 20 | 16 | 3 | 0 |
| `control/test/support/migration_case.ex` | 3 | 0 | 3 | 0 | 0 | 3 | 0 | 0 |
| `control/test/support/orchestration_case.ex` | 28 | 0 | 28 | 0 | 0 | 26 | 2 | 0 |
| `control/test/support/served_document.ex` | 48 | 0 | 48 | 0 | 37 | 7 | 4 | 0 |
| `control/test/support/slowest_formatter.ex` | 16 | 0 | 16 | 0 | 16 | 0 | 0 | 0 |
| `control/test/support/test_conn.ex` | 9 | 0 | 9 | 0 | 3 | 6 | 0 | 0 |
| `control/test/support/test_fixtures.ex` | 87 | 11 | 76 | 0 | 74 | 9 | 4 | 0 |
| `control/test/symmetry_control/goals/changeset_validators_test.exs` | 18 | 7 | 11 | 0 | 13 | 5 | 0 | 0 |
| `control/test/symmetry_control/goals_read_model_test.exs` | 5 | 5 | 0 | 0 | 5 | 0 | 0 | 0 |
| `control/test/symmetry_control/goals_test.exs` | 11 | 11 | 0 | 0 | 4 | 7 | 0 | 0 |
| `control/test/symmetry_control/integrations/azure_devops_test.exs` | 3 | 2 | 1 | 0 | 2 | 0 | 1 | 0 |
| `control/test/symmetry_control/integrations/github_test.exs` | 3 | 2 | 1 | 0 | 2 | 0 | 1 | 0 |
| `control/test/symmetry_control/integrations/http_test.exs` | 2 | 2 | 0 | 0 | 0 | 2 | 0 | 0 |
| `control/test/symmetry_control/integrations_test.exs` | 15 | 15 | 0 | 0 | 2 | 13 | 0 | 0 |
| `control/test/symmetry_control/orchestration/capability_scheduling_test.exs` | 1 | 1 | 0 | 0 | 1 | 0 | 0 | 0 |
| `control/test/symmetry_control/provider_broker_e2e_test.exs` | 5 | 5 | 0 | 0 | 0 | 5 | 0 | 0 |
| `control/test/symmetry_control/workspaces/work_item_eligibility_test.exs` | 20 | 19 | 1 | 0 | 19 | 0 | 1 | 0 |
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
| `daemon/internal/app/native_run.go` | 139 | 137 | 0 | 0 | 10 | 96 | 31 | 2 |
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
| `daemon/internal/platform/supervisor_linux_helper.go` | 93 | 91 | 0 | 1 | 52 | 35 | 5 | 1 |
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
| `daemon/internal/state/provider_action.go` | 4 | 4 | 0 | 0 | 2 | 2 | 0 | 0 |
| `daemon/internal/state/provider_action_test.go` | 7 | 7 | 0 | 0 | 0 | 7 | 0 | 0 |
| `daemon/internal/state/state.go` | 10 | 10 | 0 | 0 | 5 | 4 | 1 | 0 |
| `daemon/internal/state/state_test.go` | 16 | 16 | 0 | 0 | 16 | 0 | 0 | 0 |
| `daemon/internal/state/state_wrap_test.go` | 18 | 18 | 0 | 0 | 18 | 0 | 0 | 0 |
| `daemon/internal/workspace/fingerprint_test.go` | 6 | 6 | 0 | 0 | 6 | 0 | 0 | 0 |
| `daemon/internal/workspace/linux_clone_fs_linux.go` | 28 | 28 | 0 | 0 | 20 | 8 | 0 | 0 |
| `daemon/internal/workspace/linux_clone_linux_test.go` | 13 | 13 | 0 | 0 | 6 | 7 | 0 | 0 |
| `daemon/internal/workspace/subject.go` | 9 | 9 | 0 | 0 | 9 | 0 | 0 | 0 |
| `daemon/internal/workspace/subject_test.go` | 5 | 5 | 0 | 0 | 5 | 0 | 0 | 0 |
| `daemon/internal/workspace/workspace_test.go` | 7 | 7 | 0 | 0 | 7 | 0 | 0 | 0 |
| `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` | 75 | 74 | 0 | 1 | 16 | 58 | 1 | 0 |
| `daemon/native/linux-gate/protocol/owner_config_linux.go` | 5 | 4 | 0 | 1 | 2 | 2 | 1 | 0 |
| `daemon/native/linux-gate/protocol/proc_identity_linux.go` | 13 | 12 | 0 | 1 | 0 | 12 | 1 | 0 |
| `daemon/native/linux-gate/protocol/wire.go` | 75 | 72 | 0 | 0 | 35 | 36 | 1 | 3 |
| `daemon/native/linux-gate/protocol/wire_linux.go` | 18 | 17 | 0 | 1 | 3 | 14 | 1 | 0 |
| `daemon/native/linux-gate/protocol/wire_linux_test.go` | 51 | 50 | 0 | 1 | 40 | 8 | 3 | 0 |
| `daemon/native/linux-gate/protocol/wire_test.go` | 75 | 75 | 0 | 0 | 60 | 15 | 0 | 0 |
| `frontend/eslint.config.js` | 29 | 29 | 0 | 0 | 22 | 7 | 0 | 0 |
| `frontend/src/components/primitives.test.tsx` | 11 | 11 | 0 | 0 | 8 | 3 | 0 | 0 |
| `frontend/src/components/primitives.tsx` | 18 | 18 | 0 | 0 | 6 | 12 | 0 | 0 |
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
| `scripts/ci/assert-browser-suite-coverage.py` | 60 | 6 | 53 | 1 | 15 | 44 | 1 | 0 |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | 20 | 18 | 0 | 0 | 10 | 4 | 4 | 2 |
| `scripts/ci/go-test-durations.py` | 72 | 18 | 53 | 1 | 7 | 64 | 1 | 0 |
| `scripts/ci/model-routing-evaluation.py` | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 |
| `scripts/ci/model-routing-record-sanitize.py` | 535 | 171 | 364 | 0 | 369 | 141 | 25 | 0 |
| `scripts/ci/omo-control-native-evidence.py` | 4 | 0 | 0 | 0 | 0 | 0 | 0 | 4 |
| `scripts/ci/omo-native-evidence.py` | 12 | 3 | 8 | 0 | 5 | 6 | 0 | 1 |
| `scripts/ci/opencode-v2-support-witness.py` | 4 | 0 | 4 | 0 | 1 | 3 | 0 | 0 |
| `scripts/ci/revision_scope.py` | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 |
| `scripts/ci/test-check-lockfile-registry.py` | 21 | 7 | 14 | 0 | 2 | 18 | 1 | 0 |
| `scripts/ci/test-ci-changed-path-classifier.py` | 55 | 28 | 27 | 0 | 34 | 21 | 0 | 0 |
| `scripts/ci/test-model-routing-evaluation.py` | 4 | 0 | 0 | 0 | 0 | 0 | 0 | 4 |
| `scripts/ci/test-model-routing-omp-usage.py` | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 1 |
| `scripts/ci/test-model-routing-record-sanitize-json.py` | 339 | 329 | 9 | 1 | 96 | 230 | 13 | 0 |
| `scripts/ci/test-model-routing-record-sanitize.py` | 56 | 51 | 5 | 0 | 1 | 53 | 2 | 0 |
| `scripts/ci/test-omo-native-evidence.py` | 4 | 2 | 2 | 0 | 1 | 2 | 1 | 0 |
| `scripts/ci/test-opencode-v2-support-witness.py` | 2 | 0 | 2 | 0 | 1 | 0 | 1 | 0 |

`excluded` rows are code lines the prefix classifier read as comments (44 rows, all `kind=code`
after the real lexers ran: `git blame`-derived candidates whose line the Go/Python/TS lexers do not
place inside a comment token or docstring — e.g. `*events = append(...)`, `*run.stopEvidence = ...`,
`**call,` in Python argument lists). They are byte-identical in the delivered tree.

## 10. Delivered record (stage update, 2026-11-22)

Disposition totals over the 6,074 inventory rows: **deleted 2,609 · compressed 3,135 · kept 286 ·
excluded 44**; rules `K1` 2,318, `D1` 1,481, `D2` 415, `D6` 406, `K4` 385, `M1` 353, `K3` 255,
`K2` 228, `D3` 71, `K5` 63, `M2` 30, `D5` 25, `CODELINE` 44 (per-line rows: the off-repo inventory
artifact, §11; the fix stage moved 8 rows — the two blocks review R1 compressed — from `kept`,
§11.6).

Deviations from this plan's assumptions, all recorded rather than silently absorbed:

1. **Per-line dispositions are derived from the delivered tree, block-anchored** (merge step): a row
   whose line survives verbatim is `kept`, a row whose text is gone while its block survives as a
   ≤2-line why is `compressed`, and a row whose block is gone is `deleted`. Worker-stage labels that
   attributed a block-level change to individual lines are therefore recomputed; rules and
   replacement notes are preserved. Re-run at the apply stage against the delivered revision: 553
   rows moved or were re-replaced (376 `deleted`→`compressed`, 64 `deleted`→`kept`, 82
   `compressed`→`deleted`, 9 `kept`→`deleted`, 1 `compressed`→`kept`, 21 replacement-text
   refreshes), each with the delivered replacement text where a block survives (§11).
2. **44 classifier false positives** (`CODELINE`): lines whose text starts with `*`/`**` but which
   real lexers do not place inside comments. No such line was edited.
3. **8 relocated why-lines** (declared): four in `control/.credo.exs` moved from the deleted header
   to their config entries, four are minimal completions of pre-program fragments in
   `scripts/ci/model-routing-record-sanitize.py` (see §10.4).
4. **5 pre-program comment lines were rewritten** (4 at the apply stage, `provider_action.go`'s at
   the fix stage, §11.6) to close sentences whose program-added
   continuation was deleted (`scripts/ci/model-routing-record-sanitize.py`: `_SEPARATOR`,
   `_FILE_PREFIX`, `root_relative`, `file_url`; plus two docstring tails and one fragment each in
   `control/test/support/integration_fixtures.ex`, `control/test/support/test_conn.ex`,
   `scripts/ci/omo-native-evidence.py`, `scripts/ci/opencode-v2-support-witness.py`,
   `scripts/ci/test-check-lockfile-registry.py`). The contract's scope rule protects standalone
   pre-program comments; these are fragments *inside* blocks whose candidate lines were removed, and
   completing them is the smallest edit that leaves no mid-sentence comment. Counted by the
   verifier's `pre_program_removed` stat: 5 against the sweep base after the fix stage added
   `provider_action.go`'s mid-sentence tail (§11.6); 39 against the merged delivered revision,
   where 34 of the counted lines belong to main-side rewrites in `ci.yml`,
   `test-ci-changed-path-classifier.py` and `goal_session.go` (§11).
5. **No block exceeds the two-line ceiling in the fixed tree.** The apply stage left two blocks
   larger than two lines under a claimed exemption (`daemon/internal/state/provider_action.go`
   568-571 and `scripts/ci/export-linux-v2-redacted-evidence.py` 535-560, described there as a
   published-record allowlist a CI scan enforces). Review R1 showed neither qualifies: the
   provider_action block is the doc comment of an unexported helper with no scan or consumer, and
   the export script's scan (`scripts/ci/test-linux-v2-evidence.py`) enforces the
   `DAEMON_LOG_MESSAGES` code frozenset, not the comment text. The fix stage compressed both
   blocks to ≤2 lines (§11.6), so every candidate block in the fixed tree is zero, one or two
   lines. The docstring-aware ceiling check over all 1,651 blocks reports 0 blocks over two lines
   and no block using the exemption (§11.3).
6. **Moved module intent lives in the owning slice plans.** The 16 workstream sections of the sweep
   record (353 `M1` rows) were appended verbatim to the owning slice plans' "Comment-policy sweep"
   notes at the apply stage (conductor amendment, §11): 30 slice plans in all — the 22 that already
   carried a pointer note plus 8 that received one because their files' intent had no other in-repo
   home. The record file itself is kept off-repo (§11), and no slice-plan note references it.

## 11. Apply-stage record (2026-11-22) — re-bound to the delivered revision (conductor amendment)

The amendment "inventory size" is applied as this section records: the full per-line inventory and
the moved-intent record left the repository and are kept as off-repo run artifacts; the plan keeps
the compact per-file summary (§9), every kept-with-reason block longer than one line (§11.4) and
this record. The stage also applied the accepted simplify findings: the three residual
docstring/why trims (§11.2 item 5).

### 11.1 Off-repo judging artifacts

Run artifacts directory (`<HOST-PATH>` = the GDD state root, under this stage's run directory):

| artifact | sha256 | content |
|---|---|---|
| `comment-policy-sweep-2026-11-22-inventory.tsv` | `cb5ee753c206ae0b445a746fec7bdfd8fce59aa476ea86ba2d177e127014c583` | 6,074 rows, one per candidate; the per-line dispositions re-bound to the delivered revision, including the fix stage's 8 row moves (§11.6) (the judging artifact) |
| `comment-policy-sweep-2026-11-22-inventory-as-committed.tsv` | `3229361201f369f3a52e6ff6cd8396fdf685c20839428786b59f98400f6605e0` | the implement-stage record as committed, kept so the §11.2 diff is reproducible |
| `comment-policy-sweep-2026-11-22-moved-intent.md` | `45092a12744ab8da103b186a4e43edddc3c3f125ce87b08c25367c8de6f29c41` | the 492-line workstream record as committed; its content now lives in the owning slice plans (§10.6) |

Regeneration of the inventory from `git` history (scratch tools under `<HOST-PATH>/scratch/sweep/`;
`$ART` = the artifact path; the worker disposition sheets are the recorded decisions of the merged
14-workstream run):

```
python extract3.py                             # blame-derived candidates at f1fe1e59
python inventory.py                            # per-file inventory and block context
python refine2.py                              # real lexers -> inventory3-refined/-excluded.jsonl
python merge_dispositions.py --out-tsv $ART    # 14 worker sheets -> the merged rows
python redisp.py                               # recompute rows against the delivered tree
python build-tsv.py                            # write $ART ($ART is also the input; copy it aside first when re-running)
```

The derivation steps ran at implement (2026-11-22 05:29–06:12) and are unchanged; the last two ran
at the apply stage and reproduce the §11.2 diff on the delivered revision.

### 11.2 Record repairs against the delivered revision

553 rows differ from the implement record: 376 `deleted`→`compressed`, 64 `deleted`→`kept`, 82
`compressed`→`deleted`, 9 `kept`→`deleted`, 1 `compressed`→`kept`, and 21 replacement-text-only
refreshes. The defects they close:

1. **`@moduledoc` pragma and structural survivor rows** (`@moduledoc false`, `@moduledoc """`,
   `"""`, `@doc """`) that are byte-identical in the delivered tree are `kept`; the eleven pragma
   rows the review named now carry `kept`/`K5`. Fourteen candidate files are byte-identical to
   `f1fe1e59` (the apply stage fixed the two the implement record counted, `changeset_validators.ex`
   and `daemon_channel.ex`), so the sweep changed 195 of the 212 candidate files — 194 from the
   sweep commits, plus `provider_action.go` from the fix stage's compression (§11.6); three
   further candidate files differ from `f1fe1e59` only through content merged from `main` after the
   sweep base (`native_settlement_regression_test.go`, `goal_session.go`,
   `omo-control-native-evidence.py`).
2. **Surviving docstring/comment blocks** whose base text is gone are `compressed` with the
   delivered replacement text recorded — the delivered lines not present anywhere in the base
   revision — instead of the blanket `deleted` the implement record carried; the same recompute
   caught the block replacements the `main` merges removed (`compressed`→`deleted` rows).
3. **Nine kept rows the merged `main` removed** (`native_run.go` 139/140/1557/1558,
   `supervisor_linux_helper.go` 853/854/1166/1167, `readable-fixtures.mjs:6` — the last an
   occurrence-drift `//`) are restated against the delivered revision; the re-run's kept/removed
   checks are occurrence-keyed (§11.3).
4. **Counts and claims refreshed in one pass**: this plan (§1, §9, §10, §11), the evidence record
   (§0–§2, §4–§7) and the goal 0010 T1 entry.
5. **Accepted simplify findings applied** (from the stage's findings brief): residual restating
   text removed from `strict_evidence_json.ex` (two lines at :3-4 and one at :80),
   `work_item_eligibility.ex` (the four `@doc` restating clauses) and `go-test-durations.py` (the
   module docstring trimmed to its one consumed first line); the TSV replacement join rule is
   documented in the evidence record §2. The `--help`-pin and repo-hosted-harness findings stay
   unapplied: both would add committed test/harness files, which the contract's failure control
   ("any test edit beyond comments") and the plan §7.6 no-new-harness decision exclude.

### 11.3 Checks on the delivered revision

The three scratch checkers were re-bound to the delivered revision and extended where the merged
tree required it:

- **policy verifier** (`verify.py --base f1fe1e59 --also-base origin/main --artifact $ART`): 6,074
  rows (6,030 candidates + 44 `excluded`), 0 duplicate keys, `--- verify: 0 problems`.
  `--also-base` treats `origin/main` content as base text so main-side rewrites after the sweep
  base are not unexplained new comments; a bare-trivia arm (`//`, `///`, `////`, `#`, `/*`, `*/`,
  `/**`, `*`) keeps the removal check occurrence-wise for separator-only rows. On the fixed tree
  its stats read `deleted 2,609 / compressed 3,135 / kept 286 / excluded 44`,
  `pre_program_removed: 39`, `relocated_lines: 8`, `exempt_blocks_over_2: 1`; the one counted
  "exempt" entry is the verifier's anchor-walk artefact, not a delivered >2-line block — the walk
  above the two-line `K4` block `export-linux-v2-redacted-evidence.py` 559-560 also counts the
  fix-stage replacement line at 535 against it, and every candidate block in the tree is ≤2 lines
  (§10.5; ceiling check below). `pre_program_removed` reads 39: the 5 sweep-side mid-sentence
  repairs (the fix stage added `provider_action.go`'s, §11.6) plus 34 lines main-side PRs rewrote
  after the sweep base (`ci.yml` 19, `test-ci-changed-path-classifier.py` 11, `goal_session.go`
  4 — merged-main content, not sweep edits).
- **rows-vs-tree census** (occurrence-keyed, scratch `census-final.py`):
  `FALSE-DELETED: 0 (non-empty text: 0)`, `FALSE-KEPT: 0` — every row's disposition matches the
  delivered tree.
- **block-ceiling check** (docstring-aware, scratch `ceiling.py`; shebang and `"""` framing
  exempt): `--- ceiling: 0 blocks over two lines (non-exempt)` — no block uses the exemption
  (§10.5).

### 11.4 Kept blocks longer than one line

Every kept block longer than one line — 72 groups, all of two lines — from the delivered-revision
inventory, complete (the two four-line groups the apply stage listed here were compressed to ≤2
lines by the fix stage, §11.6):

- `.github/dependabot.yml` lines 7–8 (K1) — "# No update groups: each pull request stays one reviewable bump, and the weekly" / "# cadence with a small open-pull-requests ceiling bounds the noise."
- `.github/workflows/ci.yml` lines 167–168 (K1) — "# It resolves the locked versions against the registry, so it belongs" / "# beside the provenance check, before the install."
- `control/lib/symmetry_control/integrations/provider_access.ex` lines 1166–1167 (K1) — "# resource -> connection -> work_item -> task -> run -> runtime: the rows" / "# both scopes lock in this order and with `FOR UPDATE`."
- `control/lib/symmetry_control/integrations/provider_access.ex` lines 2085–2086 (K1) — "# The Goal authority a claim and an unstarted-intent recovery re-run against" / "# the rows they locked, in one order."
- `control/lib/symmetry_control_web/controllers/chat_controller.ex` lines 24–25 (K1) — "# A chat write can roll back a workspace changeset (`start_work`); those are" / "# served with the field errors, every other refusal with its protocol status."
- `control/lib/symmetry_control_web/portal_json.ex` lines 539–540 (K1) — "# An empty provider value means "never synchronized", so it falls through to" / "# the manual value exactly as an absent one does."
- `control/lib/symmetry_control_web/portal_json.ex` lines 554–555 (K1) — "# The manual branch serves neither `provider` nor `recorded_at`: those members" / "# exist only where a provider owns the value."
- `daemon/internal/app/command_queue.go` lines 84–85 (K3) — "// next pops the head of the arrival FIFO. The slot is cleared before the slice" / "// is re-headed so a popped entry is not retained by the backing array."
- `daemon/internal/app/command_queue.go` lines 231–232 (K1) — "// finish closes a window. Closing is how a response stops fencing: a delivery" / "// that happens afterwards is invisible to it."
- `daemon/internal/app/commands.go` lines 465–466 (K1) — "// The terminal owner is still responsible for completing its durable" / "// write. No cancellation acknowledgement is safe yet."
- `daemon/internal/app/commands.go` lines 605–606 (K1) — "// Do not acknowledge cancellation or allow terminal cleanup to" / "// proceed while a terminal native outcome has no durable accounting."
- `daemon/internal/app/goal_admission.go` lines 77–78 (K1) — "// Retained resume requires an implemented transport as well as a" / "// verified capability; other adapters cannot consume a reservation."
- `daemon/internal/app/goal_admission.go` lines 330–331 (K1) — "// Control derives validation_failed from persisted failed validator" / "// evidence; a local read/integrity failure has no such receipt."
- `daemon/internal/app/goal_admission.go` lines 497–498 (K1) — "// The new Run refers to the retained workspace but never owns its" / "// deletion. Persist that fact before consuming the predecessor receipt."
- `daemon/internal/app/goal_admission.go` lines 649–650 (K4) — "// Control owns a logical workspace key. The absolute local worktree path" / "// remains in the daemon journal and must not become control-plane identity."
- `daemon/internal/app/goal_admission.go` lines 831–832 (K3) — "// An adapter can return a typed-nil harness.Session through an interface." / "// Treat it as no transferred cleanup owner."
- `daemon/internal/app/goal_admission.go` lines 838–839 (K1) — "// Publish even a nil result so the Adapter.Start barrier cannot strand a" / "// cancellation when start fails without returning a session."
- `daemon/internal/app/goal_delivery.go` lines 200–201 (K1) — "// The stop outbox is the durable stopped witness. Write it before changing" / "// the local projection so recovery can repair either cross-file window."
- `daemon/internal/app/goal_delivery.go` lines 497–498 (K1) — "// deliverGoalDelivery sends one exact durable body and deletes it only after" / "// the control client has verified its typed receipt."
- `daemon/internal/app/lease.go` lines 271–272 (K1) — "// Do not persist the absolute field from a relative response: a" / "// replayed wall-clock expiry must not outlive this local authority."
- `daemon/internal/app/native_run.go` lines 27–28 (K1) — "// taskResultReasonError keeps a canonical terminal reason through local" / "// preflight failures without asking string matching to recover domain state."
- `daemon/internal/app/native_run.go` lines 96–97 (K1) — "// inputMu serializes stdin delivery with input-related lifecycle mutations." / "// Never wait for it while holding daemon.mu."
- `daemon/internal/app/native_run.go` lines 293–294 (K1) — "// withLinuxV2NativeAdmission keeps App admission snapshots ordered with the" / "// Store CAS while never holding daemon.mu across status RPCs or State access."
- `daemon/internal/app/native_run.go` lines 414–415 (K1) — "// Focused admission tests may call startGoalAdmission without a runningRun;" / "// production assignment paths always install one."
- `daemon/internal/app/native_run.go` lines 493–494 (K1) — "// releaseNativeTerminalOwner is idempotent because terminal delivery, stale" / "// recovery, and run release can converge on the same native completion."
- `daemon/internal/app/native_run.go` lines 694–695 (K1) — "// acknowledgeExpiredNativeStartCancellation preserves a cancellation command" / "// accepted before the lease watchdog won the native-start race."
- `daemon/internal/app/native_run.go` lines 852–853 (K1) — "// Publish retry only after all recovery evidence has been attempted. A" / "// concurrent final waiter then observes the actual close/accounting flags."
- `daemon/internal/app/native_run.go` lines 858–859 (K1) — "// retainUnknownGoalLaunchWorkspace prevents cleanup from deleting a worktree" / "// that an unconfirmed native transport may still be modifying."
- `daemon/internal/app/native_run.go` lines 1389–1390 (K1) — "// A retained app-server stays alive after turn/completed. WaitTurn is the" / "// close permission only; it must never expose an early TaskResult."
- `daemon/internal/app/native_run.go` lines 1425–1426 (K1) — "// Close joins the adapter watcher, but retain a bounded final Wait as a" / "// second barrier for deferred interrupt classification and process truth."
- `daemon/internal/app/native_run.go` lines 1529–1530 (K1) — "// A newer accounting retry owns terminal publication. An older completion" / "// must not clear its durable recovery barrier or publish stale close state."
- `daemon/internal/app/native_run.go` lines 1665–1666 (K1) — "// A queue conflict can mean another terminal writer won. Read back the" / "// durable terminal before retaining this owner forever."
- `daemon/internal/app/native_run.go` lines 2142–2143 (K1) — "// A sticky turn/protocol failure remains authoritative for the terminal" / "// result, but cannot negate a separately joined physical stop barrier."
- `daemon/internal/app/native_run.go` lines 2390–2391 (K1) — "// Failed Ready-barrier cleanup has no worker waiting on the native session." / "// Give it one bounded in-process retry before the store becomes unavailable."
- `daemon/internal/app/native_settlement_regression_test.go` lines 1391–1392 (K1) — "// A silent bound here would only delay the later backgroundWG.Wait" / "// hang; give the flush its verdict instead."
- `daemon/internal/app/outbox.go` lines 36–37 (K1) — "// restartOutboxRecoveryContextKey suppresses ordinary ownership-loss cleanup" / "// until restart recovery has converted the journal into a terminal fallback."
- `daemon/internal/app/outbox.go` lines 405–406 (K1) — "// A stale run cannot recreate an acknowledgement after cleanup. Keep the" / "// exact fenced body retryable until delivery or an explicit retirement."
- `daemon/internal/app/outbox.go` lines 603–604 (K1) — "// A nil return here defers rather than succeeds: the outbox retries on" / "// its wake signal and 1s interval; journal readers must wait for !isStarting."
- `daemon/internal/app/outbox.go` lines 1021–1022 (K1) — "// A delivered or retired acknowledgement no longer carries enough local" / "// data to prove an exact replay. Keep the authority barrier fail-closed."
- `daemon/internal/execution/linux_owner_v2_finalize_pipeline_test.go` lines 750–751 (K1) — "// The capability is not issued yet: a pipeline that claimed it before the" / "// durable clear would stop on the claim error instead of the clear failure."
- `daemon/internal/notification/client.go` lines 87–88 (K4) — "// Phoenix path, such as /socket/websocket?vsn=2.0.0. A client without a" / "// connection error handler discards the retried connection failures of Run."
- `daemon/internal/platform/anchor_run.go` lines 660–661 (K1) — "// These observations are ephemeral emergency-stop state only; none can" / "// authorize State receipt creation or journal clearing."
- `daemon/internal/platform/anchor_run.go` lines 674–675 (K1) — "// reaped retains exact responses only while this anchor process lives. It" / "// is not durable and cannot recover proof after correlated anchor loss."
- `daemon/internal/platform/anchor_server.go` lines 153–154 (K1) — "// Strict v1 owner configs remain usable for launch/recovery, but v2 clone" / "// cleanup stays closed because no fixed StateDir is present."
- `daemon/internal/platform/anchor_server.go` lines 328–329 (K1) — "// Keep the State reader pinned until every worker exits. Its own" / "// context and the owner unit's bounded stop timeout bound this wait."
- `daemon/internal/platform/anchor_server.go` lines 1522–1523 (K1) — "// This callback runs immediately before cmd.Start. Any later uncertain" / "// outcome keeps the tombstone; pre-fork validation failures stay retryable."
- `daemon/internal/platform/anchor_types.go` lines 48–49 (K2) — "// DefaultLinuxOwnerAnchorSocketPath is the systemd-provisioned A-private" / "// endpoint used by the daemon preflight and the per-run v2 helper."
- `daemon/internal/platform/anchor_wire.go` lines 214–215 (K1) — "// stoppingAnswer records whether a stopping anchor answers this operation" / "// with anchor_stopping before the per-operation shape gate runs."
- `daemon/internal/platform/anchor_wire.go` lines 222–223 (K1) — "// shape carries the relational rules a presence set cannot express; it may" / "// assume every required field is present."
- `daemon/internal/platform/anchor_wire_shape_test.go` lines 408–409 (K1) — "// A stopping anchor answers before the per-operation shape gate runs:" / "// a wrong descriptor count must still produce the same frame."
- `daemon/internal/platform/anchor_wire_shape_test.go` lines 524–525 (K1) — "// linuxOwnerAnchorShapeGoldenFrame resolves the one host-dependent value of a" / "// pinned frame: the harness anchor's A UID is the test process uid."
- `daemon/internal/platform/helper_v2_recovery.go` lines 263–264 (K1) — "// An unresolved helper cannot keep detached B alive by staying resident;" / "// its anchor owns the independent pidfd watcher and pinned-leaf fallback."
- `daemon/internal/platform/helper_v2_server.go` lines 251–252 (K1) — "// peekStatus reports lease liveness without latching expiry or waking the" / "// owner-loss watcher. Status is an observation; effectful paths own expiry."
- `daemon/internal/platform/helper_v2_server.go` lines 859–860 (K1) — "// A failed Prepare may retain an exact A child for emergency stop," / "// but must never establish gate-release transition authority."
- `daemon/internal/platform/helper_v2_stop.go` lines 26–27 (K1) — "// LinuxOwnerHelperV2StopEvidence contains only measured v2 stop observations;" / "// it is not a State receipt and cannot clear a journal or release a cgroup."
- `daemon/internal/platform/helper_v2_stop.go` lines 351–352 (K1) — "// The operation's measured state is already cached. Closing only this" / "// response channel creates a lost-ACK witness without forging evidence."
- `daemon/internal/platform/supervisor_linux_client.go` lines 1566–1567 (K1) — "// closeControlAfterResponseLossRequestLocked preserves pipe ordering: callers" / "// hold requestMu, while mu protects detaching the writer from other cleanup."
- `daemon/internal/platform/supervisor_linux_client.go` lines 1815–1816 (K1) — "// retireLinuxSupervisorEndpoint removes only a stale socket that no longer" / "// accepts connections. A live or replacement listener remains unresolved."
- `daemon/internal/platform/supervisor_linux_helper.go` lines 144–145 (K1) — "// renew accepts a strictly newer sequence, or an exact replay of the current" / "// sequence/deadline. The bool reports an idempotent replay."
- `daemon/internal/platform/supervisor_linux_helper.go` lines 517–518 (K1) — "// Keep the helper's ptrace owner on one OS thread from target launch through" / "// the later Resume/detach boundary."
- `daemon/internal/platform/supervisor_windows_client.go` lines 301–302 (K1) — "// RecoverPreparedSupervisor obtains the exact replayable empty-Job witness" / "// after owner loss or an uncertain commit boundary."
- `daemon/internal/platform/supervisor_windows_client.go` lines 341–342 (K1) — "// ReleasePreparedSupervisor releases the helper only after the exact stop" / "// receipt has been durably recorded by the caller."
- `daemon/internal/platform/supervisor_windows_helper.go` lines 607–608 (K1) — "// Keep the helper alive after a proven owner-loss stop so a restarted" / "// daemon can obtain and persist the exact stop receipt through recover."
- `daemon/internal/platform/supervisor_windows_helper.go` lines 820–821 (K1) — "// Keep a proven or unresolved Job authority alive for a retry. A" / "// lost response must not turn an unknown stop into a success."
- `daemon/native/linux-gate/protocol/wire_linux_test.go` lines 160–161 (K3) — "// The C gate zeroes the one-use token before it seals the request memfd" / "// (gate.c exec_as_b), so the bootstrap decodes the scrubbed frame."
- `pyproject.toml` lines 4–5 (K2) — "# Fail closed instead of reporting a different finding set: the baseline this" / "# config was fixed against was measured with ruff 0.17.5."
- `scripts/ci/export-linux-v2-redacted-evidence.py` lines 559–560 (K4) — "# assigned from a literal at native_run.go:1584-1620, 2180-2210; commands.go:721" / "# and supervisory.go:140) are covered by this rule, not by that scan."
- `scripts/ci/test-model-routing-record-sanitize-json.py` lines 765–766 (K2) — "# 8x the matches, the same number of rebuilds: one per matching pass (the pass" / "# sequence is at most the nine families plus two matchers per `--root`)."
- `scripts/ci/test-model-routing-record-sanitize-json.py` lines 840–841 (K2) — "# The scan finds the listing's first location and re-enters only its own last" / "# bytes: a handful of searches for the whole listing, not one per line."
- `scripts/ci/test-model-routing-record-sanitize-json.py` lines 884–885 (K1) — "# Boundary negative: a literal name character glued in front is still not" / "# this root."
- `scripts/ci/test-model-routing-record-sanitize-json.py` lines 923–924 (K1) — "# Boundary negative: the escape allowance does not weaken the name rejection, so" / "# a literal name character glued in front still passes."
- `scripts/ci/test-model-routing-record-sanitize.py` lines 508–509 (K1) — "# The escape allowance does not weaken the name rejection, so a literal name" / "# character glued in front is still not this location."

### 11.5 Gate re-runs (delivered revision)

The verbatim apply-stage gate table (rounds 1 and 2, exit codes and outputs) is in the evidence
record §5. Summary: Go/Elixir build, format, vet (native and `GOOS=linux`) and compile exit 0;
`ruff check`/`ruff format --check` exit 0 on the touched file and on `scripts/ci` under the
config; `tsc` and `pnpm lint` exit 0 (227 warnings, ceiling 229); `actionlint` exit 0 via its
direct binary; the strip-proof exits 0 both against `origin/main` `e0782105` (207 files, 207 PASS)
and for the apply-stage delta against `b942030e` (207 PASS); the verifier, census, ceiling,
`git diff --check` and the host scans exit 0. No result of the accepted findings' application is
outside the re-run gate set; the full Go/Elixir/frontend suites were not re-run (contract's
comment-only carrier proof; CI stays their oracle).

**Fix-stage re-runs (review r1, at the shipped head `465ffd1b` plus the fix-stage working-tree
changes).** Review R6: the shipped revision (`465ffd1b`, which merged `cca92284`/#219 over two
swept files and `docs/TODO.md`) had no recorded run bound to it; this stage re-ran the checks and
the results are part of the record now. Strip-proof `proof.py --rev cca92284 --files-from <212
candidates>` → `--- 212 files: 212 PASS, 0 not-PASS (none)`; policy verifier on the fixed
artifact → `--- verify: 0 problems` (stats in §11.3); `census-final.py` → `FALSE-DELETED: 0
(non-empty text: 0)`, `FALSE-KEPT: 0`; `ceiling.py --rev f1fe1e59` → `--- ceiling: 0 blocks over
two lines (non-exempt)`; `gofmt -l .` → 0 lines; `go vet ./...`, `GOOS=linux go vet ./...`, `go
build ./...` → exit 0, no output; `mix format --check-formatted`, `MIX_ENV=test mix compile
--warnings-as-errors` → exit 0; `ruff check scripts/ci` + `ruff format --check scripts/ci` →
`All checks passed!` / `53 files already formatted`; `tsc --noEmit` → exit 0; `pnpm lint` → `227
problems (0 errors, 227 warnings)` (ceiling 229, unchanged); `actionlint -shellcheck= -pyflakes=
.github/workflows/ci.yml` (direct 1.7.12 binary) → exit 0; `git diff --check` → 0 lines; host
scan (gate 13) → 0 added-side matches (below). Verbatim table: evidence record §8.

### 11.6 Fix-stage record (review r1, 2026-11-22)

All eight review findings are applied; the two source edits are comment-only (strip-proof passes,
§11.5) and nothing outside the candidate set changed.

1. **R1 (P1) — the last two >2-line blocks compressed.** `provider_action.go` 568-571 (4
   candidate lines `kept`/`K4`) becomes a 2-line why: the signature-restating lines are
   `deleted`/`D1`, the rationale lines `compressed`/`K1`
   (`// Completed provider data may already exceed the provider-only budget, so a` / `// mutation
   must leave space to classify every outstanding effect without loss.`);
   `export-linux-v2-redacted-evidence.py` 535-538 (the twelve-file list) becomes one
   `compressed`/`K2` line that closes the pre-program `msg` entry (`#           daemon/internal/app
   (the files that carry daemon.log call sites).`). Neither survived as a published-contract
   exemption (§10.5). The artifact moved 8 rows: `deleted` 2,607→2,609, `compressed` 3,129→3,135,
   `kept` 294→286; the rules moved `K4` 393→385, `K1` 2,316→2,318, `D1` 1,479→1,481, `K2`
   224→228; the artifact's new sha256 is in §11.1 and its rows re-check clean (§11.3). The
   `provider_action.go` rewrite removes one pre-program line (its sentence tail), counted as a
   declared `pre_program_removed` deviation (§10.4): the stat reads 39 (§11.3).
2. **R2 (P2) — candidate boundary stated.** §1 no longer claims the 68-commit set was selected
   "exactly": it carries the cutoff bullet (frozen at the dispatch base `f1fe1e59`; the eight
   post-freeze slices `9f82fd8e`–`cca92284` and their 88 still-present comment/docstring lines —
   5 directives, 31 in §9 files, 57 in post-base files, 4 runs >2 lines totalling 24 lines — are
   post-dispatch, post-policy and owned by their own slices).
3. **R3 (P2) — hazard control named, not invented.** §3.4/§8 no longer cite the nonexistent
   "second-pass review in §5"; they name the real controls (carriers, policy verifier) and this
   record. The hazard re-review actually performed is the review stage's row-by-row triage
   (off-repo review report, its "Hazard / test triage" item): every deleted hazard-flavoured row
   and sampled `D`-class blocks were re-read against tests and `docs/` records with 0 uncovered.
   The row set is re-derivable from the fixed artifact: disposition `deleted` with rule `K1` 64,
   `K2` 14, `K3` 7, `K4` 5, `M2` 22 — 112 rows.
4. **R4 (P2) — exemption wording removed.** §10.5, §11.3 and the evidence record no longer
   describe the two blocks as a "published-record allowlist whose membership a CI scan enforces":
   the provider_action block has no consumer or scan, and the export scan
   (`scripts/ci/test-linux-v2-evidence.py`) enforces the `DAEMON_LOG_MESSAGES` code frozenset, not
   the comment; both blocks are compressed, so no exemption is in use.
5. **R5 (P3) — byte-identical decomposition fixed.** §11.2 item 1, evidence §0 and the goal 0010
   entry now say 14 byte-identical to `f1fe1e59` + the main-merge-only files, with the counts the
   fix stage re-derived from the tree (14 identical; 195 sweep-changed; 3 main-merge-only).
6. **R6 (P3) — shipped-head re-runs folded in** (§11.5 above; evidence §0/§5/§8).
7. **R7 (P3) — TODO debt count fixed.** `docs/TODO.md` no longer says the three
   `model-routing-record-sanitize` files carry "18 `ruff format` hunks" as a set: it names the
   67-hunk three-file total (18/24/25, re-measured with ruff 0.17.5).
8. **R8 (P3) — host-path gate resolved.** The three added-side profile-path pattern matches the review
   replayed were the record's own gate rows quoting the command; they are reworded to describe
   the scan without embedding the pattern, so the contract gate's added side is empty (verified
   on the worktree diff: `git diff origin/main` added-side scan → 0 matches; the only remaining
   full-output matches are two context lines of base-revision placeholder fixtures in
   `scripts/ci/test-model-routing-record-sanitize-json.py` — a `Users`/`op` run path and its
   `file:` URL spelling, both already present at `origin/main` `cca92284` — plus
   `localHandleID`-style identifiers; no host path with a real profile name exists in the diff:
   case-sensitive profile-name scan → 0).
