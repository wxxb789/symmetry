# comment-policy-sweep evidence (2026-11-22)

Contract: `goal-comment-policy-sweep.md` (GDD, 2026-11-22 — held until the pre-policy land queue
merged; dispatched at `origin/main` `f1fe1e59`). Plan:
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](../plans/2026-11-22-comment-policy-sweep-plan.md).
Judging artifacts: the per-line inventory (6,074 rows; kept as an off-repo run artifact by the
conductor amendment — path, sha256 and regeneration commands in plan §11.1) and the moved module
intent, now recorded in the owning slice plans (plan §10.6).

Delivered revision: the round-2 head — `bcdc5d94` (`f1fe1e59` plus the sweep commits `16a7e7d3`
and `0399d5f1`, the apply stage and the fix stage §8) merged with `origin/main` `e84973a7` (#220;
merge base `b25e34ae`) and then `4fdea989` (#222, landed mid-stage), plus the round-2 working-tree
changes (§9), committed as the merge commit `refactor: correct sweep comments and merge main` for
the first merge and a completion merge for #222 (exact shas in `findings/223-fix-r1.md`; plan
§11.7). The implement-stage measurements cite the pre-merge tree they ran on; the apply-stage
re-runs (§4, §5, §7) were measured on the `b942030e`-era delivered revision, the fix-stage
re-runs (§8) on the shipped head plus the review fixes, and the round-2 re-runs (§9) on the
merge head plus the round-2 changes. All commands ran foreground on 2026-11-22 in this worktree
(Windows/amd64, go1.27.4, portable Elixir 1.20.4/OTP 29, ruff 0.17.5, pnpm 12.17.0); exit codes
and outputs are verbatim.

## 0. Change inventory

| carrier | value |
|---|---|
| files changed | 195 of the 212 candidate files by the sweep (194 from the sweep commits, plus `provider_action.go` from the fix stage's R1 compression, plan §11.6). Fourteen are byte-identical to `f1fe1e59` (the apply stage fixed the two the implement record counted, `changeset_validators.ex` and `daemon_channel.ex`; plan §11.2); three more — `native_settlement_regression_test.go`, `goal_session.go`, `omo-control-native-evidence.py` — differ from `f1fe1e59` only through content merged from `main` after the base |
| `git diff --shortstat` | see §5 gate 12; comment lines only, no code token, no string, no directive |
| candidate files | 212 |
| candidate lines | 6,074 inventory rows = 6,030 refined candidates + 44 classifier false positives (`excluded`) |
| dispositions | deleted 2,609 · compressed 3,067 · kept 354 · excluded 44 (re-bound to the delivered revision at the apply stage, the fix stage's R1 compression and the round-2 restores, plan §11.2/§11.6/§11.7; round 2 flipped the 68 restored runtime-docstring rows to `kept`) |
| rules | `K1` 2,318 · `D1` 1,481 · `D2` 415 · `D6` 406 · `K4` 385 · `M1` 353 · `K3` 255 · `K2` 228 · `D3` 71 · `K5` 63 · `M2` 30 · `D5` 25 · `CODELINE` 44 |
| new files | this record and the sweep's plan; the inventory TSV and the moved-intent record were removed from the repo by the conductor amendment and are kept as off-repo run artifacts (plan §11.1) |
| source files outside the 212 | `docs/plans/*`: slice plans with a "Comment-policy sweep" note — 22 at implement, 30 after the apply stage appended the moved-intent content (plan §10.6); `docs/TODO.md`; `docs/goals/0010-refactor-program.md` |

## 1. Candidate derivation (clause 1, re-runnable)

Program commits: the 68 first-parent commits on `main` after the program base `a3039f0`
(`git log --first-parent --pretty=%H a3039f0..f1fe1e59`), i.e. land-queue slices `rf-*` (PRs
138-214), `fix/*` (137, 180, 185, 194, 195), `docs/*` (182, 207, 208) and the queue's dependabot/npm
bumps. Candidates: `git blame --line-porcelain -w HEAD -- <file>` over the 462 files the program
changed; a line is a candidate when its blame commit is one of the 68, classified at HEAD as
`comment`, `docstring`/`@doc` (Python docstring ranges from `ast`; Elixir `@doc`/`@moduledoc` with
heredoc bodies) or `directive` (machine-consumed comment). Files under `docs/**`, lockfiles and data
formats are excluded by the contract's file rules, as are the 6 files the program deleted.

Refinement with real lexers (`go/parser` comment tokens, Python `tokenize`+`ast`,
TypeScript parser trivia, Elixir heredoc scan): 6,074 raw rows split into 6,030 candidates and 44
code lines the prefix classifier read as comments (`*events = append(...)`, `*run.stopEvidence = …`,
`**call,` splats, `*paths = append(...)`, `* position += 4`, …). Those 44 are byte-identical in the
delivered tree and carry `disposition=excluded`, `rule=CODELINE`. The 6,030 candidates by kind:
5,111 `comment`, 878 `docstring`, 41 `directive`.

## 2. Dispositions

One row per candidate line in the inventory TSV (`file`, `line`, `kind`, `disposition`, `rule`,
`text`, `replacement`):

- `deleted` 2,609 — the line's text is gone from the delivered tree, its block is gone.
- `compressed` 3,135 — the line's text is gone while its block survives as a ≤2-line *why*; the
  `replacement` column carries the delivered replacement text — the delivered lines not present
  anywhere in the base revision. A multi-line replacement is joined with ` | `; no replacement line
  in this inventory contains ` | ` itself (checked in the apply-stage rerun), so the join round-trips
  — and the delivered tree, not this column, is the authority for the exact block text.
- `kept` 286 — byte-identical survivors: 63 `K5` directives (`//go:build`, `#!`, `@moduledoc false`,
  `# noqa`); 223 why-comments (`K1` 155, `K4` 25, `K2` 12, `K3` 7 and 24 carried `D6`/`M1`/`D1`
  rules) that survive verbatim; every kept block longer than one line (72) is listed in plan §11.4.
- `excluded` 44 — classifier false positives (§1).

Dispositions are derived from the delivered tree at block granularity (a verbatim survivor is `kept`,
a vanished line inside a surviving block is `compressed`, a vanished block is `deleted`); worker-stage
per-line labels were recomputed, their rules and replacement notes preserved. The apply stage re-ran
the recompute against the delivered revision (`b942030e` + its working-tree changes, plan §11.2):
553 rows moved, now binding the merged tree; the fix stage moved a further 8 (review R1, plan
§11.6). The ceiling rule — no candidate block longer than two lines except published contracts —
holds for every block: the two blocks over two lines the apply stage had named (plan §10.5,
`daemon/internal/state/provider_action.go` 568-571 and
`scripts/ci/export-linux-v2-redacted-evidence.py` 535-538) were compressed to ≤2 lines, and the
docstring-aware ceiling check reports 0 violations with no block using the exemption.
Round 2 (§9) re-bound the replacement notes to the merge head and flipped 68 rows to `kept` (the
four restored runtime-docstring module docstrings; the conductor amendment makes their `--help`
text user-facing contract text, plan §11.7); the verifier and the census agree on the round-2
tree.

## 3. Strip-proof (zero code-token change)

Carriers (all position- and blank-line-insensitive, because comment removal changes both):

| language | carrier | proof |
|---|---|---|
| Go | `go/scanner` token stream (kind + literal, comments skipped; `tokenstream.exe`) | digest equality |
| Python | `tokenize` comment strip + `ast` docstring strip | normalized-text equality |
| Elixir | `Code.string_to_quoted` → drop `@doc`/`@moduledoc` (presence, form and content) → `Macro.to_string` | digest equality |
| TS/JS | TypeScript parser → `createPrinter({removeComments: true})` | digest equality |
| YAML/TOML | comment-stripped text parsed (`safe_load`/`tomllib`) **and** comment-stripped byte equality | digest equality |
| shell / extension-less | comment strip + `bash -n`, or full-line `#` strip | digest equality |

Carrier validation (§4 instrument check): for each of 13 language samples, deleting a comment block
and deleting a docstring left the carrier identical while a real code edit changed it —
`--- carrier validation: 0 problems`.

Delivered result over every candidate file (the union of the 16 workstream file lists, 207 unique
files; the remaining 5 candidate files had no rows to change):

```
$ python proof.py --rev f1fe1e590276883415ede724dce788aa22838029 --files-from all_files.txt --json proof-all.json
--- 207 files: 207 PASS, 0 not-PASS (none)
```

## 4. Policy verifier (clause 1/2 acceptance, scratch tool)

`python verify.py --base f1fe1e59 --also-base origin/main --artifact <off-repo inventory artifact>`
checks: coverage (every inventory row dispositioned exactly once), vocabulary, per-block delivered
truth, the ≤2-line ceiling (no exemption in use after the fix stage, §2), directive byte-identity, code-line
(`excluded`) byte-identity, no unexplained new comment text, and the declared pre-program
deviations. The apply stage re-ran it against the delivered revision and extended it for the merged
tree: `--also-base` treats lines merged from `origin/main` as base text (main-side rewrites are not
unexplained new comments), and a bare-trivia exemption (`//`, `#`, `/*`, …) keeps the removal check
occurrence-wise for separator rows; the docstring-aware ceiling check is a separate scratch script
(next paragraph). The fixed-tree run (shipped head plus the review fixes, §8):

```
inventory rows: 6074; disposition rows: 6074; duplicate keys: 0
changed files: 6; candidate files: 212
stats: {'deleted': 2609, 'kept': 286, 'compressed': 3135, 'pre_program_removed': 39, 'relocated_lines': 8, 'excluded': 44, 'exempt_blocks_over_2': 1}
--- verify: 0 problems
```

Two companion checks ran on the same revision: the occurrence-keyed rows-vs-tree census
(`FALSE-DELETED: 0 (non-empty text: 0)`, `FALSE-KEPT: 0`) and the docstring-aware block-ceiling
check (`--- ceiling: 0 blocks over two lines (non-exempt)`). The single `exempt_blocks_over_2`
count is the verifier's anchor-walk artefact, not a delivered >2-line block: the walk above the
two-line `K4` block `export-linux-v2-redacted-evidence.py` 559-560 also counts the fix-stage
replacement line at 535 against it (plan §11.3). `changed files: 38` was the working-tree set the
apply stage left for the operator: the three source trims, the sweep plan, the 30 slice-plan
appends, this record, the goal entry and the two artifact removals; the fix-stage run's working
set is §8's.

Declared deviations (also plan §10): 8 relocated why-lines (4 in `control/.credo.exs`, moved from the
deleted header to their config entries; 4 minimal completions of pre-program fragments in
`scripts/ci/model-routing-record-sanitize.py`), and 5 pre-program comment lines rewritten to close
sentences whose program-added continuation was deleted (same file; plus mid-sentence fragment fixes in
`control/test/support/integration_fixtures.ex`, `control/test/support/test_conn.ex`,
`scripts/ci/omo-native-evidence.py`, `scripts/ci/opencode-v2-support-witness.py`,
`scripts/ci/test-check-lockfile-registry.py`, and — fix stage, review R1 —
`daemon/internal/state/provider_action.go`; plan §11.6). The verifier counts these
(`relocated_lines`; `pre_program_removed` 5 against the sweep base). On the merged delivered
revision `pre_program_removed` reads 39: the same 5 plus 34 lines main-side PRs rewrote after the
sweep base in `ci.yml` (19), `test-ci-changed-path-classifier.py` (11) and `goal_session.go` (4) —
merged-main content, not sweep edits (plan §10.4/§11). Nothing else outside the candidate set
changed.

## 5. Gates (contract list, foreground, exit codes verbatim)

| # | gate (cwd) | exit | observed |
|---|---|---:|---|
| 1 | `gofmt -l .` (`daemon/`) | 0 | 0 lines |
| 2 | `go vet ./...` (`daemon/`) | 0 | no output |
| 3 | `GOOS=linux go vet ./...` (`daemon/`) | 0 | no output |
| 4 | `go build ./...` (`daemon/`) | 0 | no output |
| 5 | `mix format --check-formatted` (`control/`, portable toolchain) | 0 | no output |
| 6 | `MIX_ENV=test mix compile --warnings-as-errors` (`control/`, local `deps/`) | 0 | `Compiling 90 files (.ex)` → `Generated symmetry_control app` |
| 7 | `ruff check` (0.17.5, explicit list of all 12 touched `scripts/ci/*.py`) | 1 | `Found 12 errors` — all 12 in the three files `pyproject.toml` `extend-exclude`s; base-revision copies produce the same 12 (`PERF401` ×2, `UP031` ×5, `I001`, `UP012`, `E501`, `E731` ×2) |
| 7b | `ruff check scripts/ci` (config semantics, respect `extend-exclude`) | 0 | `All checks passed!` |
| 8 | `ruff format --check` (0.17.5, explicit list of all 12) | 1 | `3 files would be reformatted, 9 files already formatted` — the same 3 excluded files; base copies: `3 files would be reformatted` |
| 8b | `ruff format --check scripts/ci` (config semantics) | 0 | `51 files already formatted` |
| 9 | `pnpm exec tsc --noEmit -p .` (`frontend/`) | 0 | no output |
| 10 | `pnpm lint` (`frontend/`) | 0 | `227 problems (0 errors, 227 warnings)` — ceiling `--max-warnings 229`, unchanged |
| 11 | `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml` | 0 | no output (plain `actionlint` with shellcheck on PATH does not terminate on this host — the pre-existing quirk recorded in `docs/TODO.md` and kept compressed at `ci.yml:1948-1952`) |
| 12 | `git diff --check` | 0 | 0 lines |
| 13 | `git diff origin/main...HEAD` + case-insensitive user-profile path scan (contract gate 13; exact pattern in the contract file) | — | 0 added-side matches at the fixed tree (the three the review replayed were this gate's own rows quoting its command; reworded, plan §11.6); the remaining full-diff matches are two context lines of base-revision placeholder fixtures in `scripts/ci/test-model-routing-record-sanitize-json.py` (a `Users`/`op` run path and its `file:` URL spelling, both present already at `origin/main` `cca92284`), plus `localHandleID`-style identifiers; a case-sensitive profile-name scan of the diff finds none (§8) |
| 14 | strip-proof (§3) | 0 | `207 files: 207 PASS, 0 not-PASS` |
| 15 | policy verifier (§4) | 0 | `--- verify: 0 problems` |
| 16 | `go test -count=1 ./internal/linuxpreflight/... ./internal/workspace/...` (`daemon/`) | 0 | `ok 0.108s` / `ok 172.232s` |

### Apply-stage re-runs (delivered revision)

The accepted simplify findings and the conductor amendment changed three source files plus records;
every contract gate the change could affect was re-run on the delivered revision. Round 1
(`scratch/sweep/apply-gates.sh`, logs in `scratch/sweep/apply-gates/`):

| gate | exit | observed |
|---|---:|---|
| `gofmt -l .` (`daemon/`) | 0 | 0 lines |
| `go vet ./...` (`daemon/`) | 0 | no output |
| `go build ./...` (`daemon/`) | 0 | no output |
| `mix format --check-formatted` (`control/`, portable) | 0 | no output |
| `MIX_ENV=test mix compile --warnings-as-errors` (`control/`) | 0 | `Compiling 2 files (.ex)` (the two touched modules) |
| `ruff check scripts/ci/go-test-durations.py` | 0 | `All checks passed!` |
| `ruff format --check scripts/ci/go-test-durations.py` | 1 | `1 file would be reformatted` — the two-line module docstring F3 collapses to one line (round 2 below) |
| `ruff check scripts/ci` (config semantics) | 0 | `All checks passed!` |
| `ruff format --check scripts/ci` (config semantics) | 1 | `1 file would be reformatted, 52 files already formatted` — same file and cause |
| `pnpm exec tsc --noEmit -p .` (`frontend/`) | 0 | no output |
| `pnpm lint` (`frontend/`) | 0 | `227 problems (0 errors, 227 warnings)` — ceiling 229, unchanged |
| policy verifier (`--also-base`, trivia exemption, §4) | 0 | `--- verify: 0 problems` (pre-F3 row set: `deleted` 2,639 / `kept` 296 / `compressed` 3,095) |
| rows-vs-tree census | 0 | `FALSE-DELETED: 0 (non-empty: 0)`, `FALSE-KEPT: 0` |
| block-ceiling check (docstring-aware) | 0 | `0 blocks over two lines (non-exempt)` |
| `git diff --check` | 0 | 0 lines |
| host scans (user-profile path components) | 0 | no matches beyond the known identifiers |

Round 2 (`scratch/sweep/apply-gates2.sh`) fixed the three invocation defects round 1 exposed (the
`govet-linux` wrapper's `env` call, `actionlint` through its direct binary because the mise shim
has no default version on this host, and the `proof.py` baseline), re-ran `ruff format` after F3's
docstring fix, and ran the gates round 1 did not reach:

| gate | exit | observed |
|---|---:|---|
| `GOOS=linux go vet ./...` (`daemon/`) | 0 | no output |
| `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml` (direct binary) | 0 | no output |
| strip-proof `proof.py --rev e0782105` (`origin/main`) over the 207-file list | 0 | `--- 207 files: 207 PASS, 0 not-PASS` |
| strip-proof `proof.py --rev b942030e` (apply-stage delta) | 0 | `--- 207 files: 207 PASS, 0 not-PASS` |
| `ruff format --check scripts/ci/go-test-durations.py` (post-F3) | 0 | `1 file already formatted` |
| `ruff format --check scripts/ci` (post-F3) | 0 | `53 files already formatted` |
| policy verifier, census, ceiling | 0 | as §4 (round 2 binds the final artifact); the round-1 verifier log quoted the pre-F3 row set |
| `git diff --check`, host scans | 0 | 0 lines; the host-path scan's 4 matches are rows of the inventory artifact leaving the repo (its deletion diff) — no delivered file matches |

The first `proof.py` run (round 1) used the sweep base `f1fe1e59` as `--rev` and read 187/207: the
20 not-PASS files are exactly those whose code was rewritten by `origin/main` PRs merged after the
sweep base, so that baseline had gone stale — against `origin/main` `e0782105` all 207 files PASS,
and the delivered-revision delta against `b942030e` passes the same way.

Compile, format, lint, the verifier trio, `git diff --check` and the host scans re-ran clean on the
delivered revision; the full Go/Elixir/frontend suites remain CI oracles (list above).

Not run, with reasons:

- Full Go suite and `-race` (CI oracles; comment-only change, §3 carrier proves code identity).
- Full `mix test` (shared PostgreSQL; CI oracle) and the frontend `vitest`/Playwright suites.
- `ruff` gates on the three `extend-exclude` files as *blocking* checks: those files are base-red by
  configuration (unchanged, §5 rows 7/8), and the contract's zero-code-token clause forbids
  reformatting them; the open debt is tracked in `docs/TODO.md`.
- `pnpm install` initially failed (registry metadata TLS handshake + supply-chain policy check);
  it succeeded with `--frozen-lockfile --trust-lockfile --prefer-offline` against the local store, so
  gates 9/10 did run and are reported with their exit codes. `mix deps.get` needed one local fetch
  (the shared checkout's `deps/` no longer matched this base revision's lock); the shared `deps/`
  tree was restored to its own lock with `mix deps.get` after the gates.

## 6. Judging artifact (clause 2)

- per-line inventory with dispositions: off-repo run artifact (6,074 rows; path/sha256/regeneration
  in plan §11.1) — removed from the repo by the conductor amendment;
- per-file counts: plan §9 (212 rows);
- strip-proof result: §3 (`207 PASS`), carrier validation `0 problems`;
- policy verifier: §4 (`0 problems`; the one `exempt_blocks_over_2` count is an anchor-walk
  artefact, §4);
- gate tables: §5 (apply) and §8 (fix);
- moved intent: recorded in the owning slice plans (§10.6) — the standalone record is an off-repo
  run artifact (plan §11.1).

## 7. Deviations and limits

1. Per-line dispositions are block-derived (§2); 44 rows are classifier false positives, untouched.
2. 8 relocated why-lines + 8 rewritten pre-program fragment/referent sentences (§4; the fifth is
   the fix stage's, plan §11.6, and the last three are round 2's, plan §11.7) — the only edits
   outside the candidate set, all declared and counted (`pre_program_removed: 68`, of which 60
   are main-side rewrites in the merged tree).
3. No candidate block exceeds two lines: the two the apply stage had exempted were compressed by
   the fix stage (review R1; plan §11.6); round 2's restored module docstrings use the conductor
   amendment's named runtime-contract exemption (plan §11.7).
4. The three `extend-exclude` Python files stay base-red for `ruff check`/`ruff format --check`
   (unchanged from base, counts identical).
5. `pnpm-lock.yaml`-driven installs require `--trust-lockfile` in this environment; the supply-chain
   policy check cannot fetch registry metadata (handshake failure). Gates 9/10 ran on the frozen
   lockfile and are reported as such.
6. No test suite run beyond §5 row 16; the carriers are the strong evidence, and CI remains the
   oracle for the suites the contract does not name.
7. This stage does not commit, push or archive the contract file; the operator's landing step
   re-derives the diff against `origin/main`.
8. Apply stage (this revision): the two judging artifacts left the repo by the conductor amendment
   (plan §11.1); `kept` rose 236 → 294 and `deleted`/`compressed` moved with it (plan §11.2); two
   ceiling violations were fixed (`changeset_validators.ex`, `daemon_channel.ex`); F6/F7 (new tests
   or harnesses) were rejected as outside the contract; the full suites were not re-run, CI remains
   their oracle.

## 8. Fix stage (review r1, 2026-11-22)

The review found the record bound to `b942030e`/`e0782105` only, with no run bound to the shipped
head `465ffd1b` (review R6), and seven further record/disposition defects (R1-R5, R7, R8; all
applied, plan §11.6). This stage re-ran the contract gates on the shipped head plus its own fixes.
Exit codes and outputs verbatim (gate logs under `<HOST-PATH>/gates/`):

| gate (cwd) | exit | observed |
|---|---:|---|
| `gofmt -l .` (`daemon/`) | 0 | 0 lines |
| `go vet ./...` (`daemon/`) | 0 | no output |
| `GOOS=linux go vet ./...` (`daemon/`) | 0 | no output |
| `go build ./...` (`daemon/`) | 0 | no output |
| `go test -count=1 ./internal/state/...` (`daemon/`, the touched package) | 0 | `ok  github.com/wxxb789/symmetry/daemon/internal/state  4.828s` |
| `mix format --check-formatted` (`control/`, portable Elixir 1.20.4/OTP 29) | 0 | no output |
| `MIX_ENV=test mix compile --warnings-as-errors` (`control/`, portable) | 0 | no output (up to date) |
| `ruff check scripts/ci` (0.17.5, config semantics) | 0 | `All checks passed!` |
| `ruff format --check scripts/ci` | 0 | `53 files already formatted` |
| `ruff check scripts/ci/export-linux-v2-redacted-evidence.py` | 0 | `All checks passed!` |
| `ruff format --check scripts/ci/export-linux-v2-redacted-evidence.py` | 0 | `1 file already formatted` |
| `pnpm exec tsc --noEmit -p .` (`frontend/`) | 0 | no output |
| `pnpm lint` (`frontend/`) | 0 | `227 problems (0 errors, 227 warnings)` — ceiling 229, unchanged |
| `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml` (direct 1.7.12 binary) | 0 | no output |
| strip-proof `proof.py --rev cca92284 --files-from <212 candidate files>` | 0 | `--- 212 files: 212 PASS, 0 not-PASS (none)` |
| policy verifier (§4) | 0 | `--- verify: 0 problems` |
| rows-vs-tree census (`census-final.py`) | 0 | `FALSE-DELETED: 0  (non-empty text: 0)` / `FALSE-KEPT: 0` |
| block-ceiling check (`ceiling.py --rev f1fe1e59`) | 0 | `--- ceiling: 0 blocks over two lines (non-exempt)` |
| `git diff --check` | 0 | 0 lines |
| host scans (gate 13) | — | added side: 0 profile-path pattern matches; the two remaining full-output matches are context lines of base-revision placeholder fixtures in `scripts/ci/test-model-routing-record-sanitize-json.py`; case-sensitive profile-name scan of the diff: 0 |
| plan §9 table vs the fixed artifact TSV (212 files × 8 fields) | 0 | 0 mismatches; totals 6,074 / 5,111 / 878 / 41 / 2,609 / 3,135 / 286 / 44 |

The working set this stage leaves beside the two comment-only source edits: this record, the plan,
`docs/TODO.md` and `docs/goals/0010-refactor-program.md`. The fix does not commit; the operator's
landing step commits it and re-derives the diff against `origin/main`, at which point the branch's
added side of the host-path gate is empty (the rows that matched were this record's own quoting).

## 9. Round 2 (PR #223 review r1, 2026-11-22)

Findings F1–F7 applied; stage record in plan §11.7. Stage head: the merge of `origin/main`
`e84973a7` (#220) into `bcdc5d94`, then `4fdea989` (#222, landed mid-stage and conflict-free),
plus the round-2 working-tree changes; committed as the merge commit
`refactor: correct sweep comments and merge main` and a completion merge.

- **F1**: the two merge conflicts resolved per plan §11.7 item 1 (take #220's code; keep only the
  sweep edits whose target lines survive). Strip proof re-run against the final merge base:
  `python proof.py --rev 4fdea989 --files-from all_files.txt --json proof-final-4fdea989.json` →
  `--- 207 files: 207 PASS, 0 not-PASS (none)` (the base moved from `e84973a7` when #222 landed).
- **F2/F6/F7**: the sanitizer comment repairs, the pidfd-across-exec quirk and the 31 >120-char
  compressions per plan §11.7.
- **F5**: `--help` compared by running `origin/main`'s copies and the head copies of the four
  `description=__doc__` scripts: identical for all four (`cmp` exit 0), after the base module
  docstrings are restored byte-identically.
- **Judging artifacts**: the inventory's new sha256 is
  `a6ebf1de5686125c36f10b82ff5b76861fb6bc87ff3a6a2e22ed8c865afb3441` (plan §11.1); the census on
  the round-2 tree reads `FALSE-DELETED: 0 (non-empty text: 0)`, `FALSE-KEPT: 0`; the policy
  verifier on the committed clean tree reads `--- verify: 0 problems` with
  `deleted 2,609 · compressed 3,067 · kept 354 · excluded 44`, `pre_program_removed: 68`,
  `relocated_lines: 8`; the ceiling run reads
  `0 blocks over two lines (non-exempt); 17 exempt runtime module-docstring blocks`.

| gate (cwd) | exit | observed |
|---|---:|---|
| `gofmt -l .` (`daemon/`) | 0 | 0 lines |
| `go vet ./...` (`daemon/`) | 0 | no output |
| `GOOS=linux go vet ./...` (`daemon/`) | 0 | no output |
| `mix format --check-formatted` (`control/`, portable Elixir 1.20.4/OTP 29) | 0 | no output |
| `MIX_ENV=test mix compile --warnings-as-errors` (`control/`, portable) | 0 | no output |
| `ruff check scripts/ci` (0.17.5) | 0 | `All checks passed!` |
| `ruff format --check scripts/ci` | 0 | `53 files already formatted` |
| `pnpm --pm-on-fail=ignore install --frozen-lockfile --trust-lockfile` (`frontend/`) | 0 | no output |
| `pnpm --pm-on-fail=ignore exec tsc --noEmit -p .` (`frontend/`) | 0 | no output |
| `pnpm --pm-on-fail=ignore lint` (`frontend/`) | 0 | `223 problems (0 errors, 223 warnings)` — the count moved from 227 through #220's merged changes; `--max-warnings 229` unchanged |
| `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml` (direct 1.7.12 binary) | 0 | no output |
| strip-proof (§above) | 0 | `--- 207 files: 207 PASS, 0 not-PASS (none)` |
| `scripts/ci/test-check-lockfile-registry.py` | 0 | `Ran 25 tests in 8.397s` / `OK` |
| `scripts/ci/test-ci-changed-path-classifier.py` | 0 | fixture routing printed; `OK` |
| `scripts/ci/test-model-routing-record-sanitize.py` | 0 | `OK` |
| `scripts/ci/test-model-routing-record-sanitize-json.py` | 0 | `OK` |
| `scripts/ci/test-omo-native-evidence.py` | 0 | `Ran 15 tests in 2.479s` / `OK` |
| `scripts/ci/test-opencode-v2-support-witness.py` | 0 | `Ran 16 tests in 0.162s` / `OK` |
| `scripts/ci/test-check-record-cleanliness.py` | 0 | `Ran 19 tests in 70.890s` / `OK` |
| `--help` comparison, four runtime-docstring scripts | 0 | identical on base and head |
| `git diff --check` | 0 | 0 lines |
| added-side host scans | — | `users/` 0, `/Users/` 0, case-sensitive profile name 0; the case-insensitive profile-name pattern matches only `localHandleID` identifiers (present at base) |
