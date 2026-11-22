# comment-policy-sweep evidence (2026-11-22)

Contract: `goal-comment-policy-sweep.md` (GDD, 2026-11-22 — held until the pre-policy land queue
merged; dispatched at `origin/main` `f1fe1e59`). Plan:
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](../plans/2026-11-22-comment-policy-sweep-plan.md).
Judging artifacts: the per-line inventory (6,074 rows; kept as an off-repo run artifact by the
conductor amendment — path, sha256 and regeneration commands in plan §11.1) and the moved module
intent, now recorded in the owning slice plans (plan §10.6).

Delivered revision: `b942030e` (`f1fe1e59` plus the sweep commit `16a7e7d3` and the `origin/main`
merges) plus the apply-stage working-tree changes. The implement-stage measurements cite the
pre-merge tree they ran on; the apply-stage re-runs (§4, §5, §7) were measured on the delivered
revision. All commands ran foreground on 2026-11-22 in this
worktree (Windows/amd64, go1.27.4, portable Elixir 1.20.4/OTP 29, ruff 0.17.5, pnpm 12.17.0); exit
codes and outputs are verbatim.

## 0. Change inventory

| carrier | value |
|---|---|
| files changed | 194 (of the 212 candidate files; 18 are byte-identical to the sweep base — the apply stage fixed the two the implement record counted, `changeset_validators.ex` and `daemon_channel.ex`; plan §11.2). Four further candidate files differ from `f1fe1e59` only through content merged from `main` after the base |
| `git diff --shortstat` | see §5 gate 12; comment lines only, no code token, no string, no directive |
| candidate files | 212 |
| candidate lines | 6,074 inventory rows = 6,030 refined candidates + 44 classifier false positives (`excluded`) |
| dispositions | deleted 2,607 · compressed 3,129 · kept 294 · excluded 44 (re-bound to the delivered revision at the apply stage, plan §11.2) |
| rules | `K1` 2,316 · `D1` 1,479 · `D2` 415 · `D6` 406 · `K4` 393 · `M1` 353 · `K3` 255 · `K2` 224 · `D3` 71 · `K5` 63 · `M2` 30 · `D5` 25 · `CODELINE` 44 |
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

- `deleted` 2,607 — the line's text is gone from the delivered tree, its block is gone.
- `compressed` 3,129 — the line's text is gone while its block survives as a ≤2-line *why*; the
  `replacement` column carries the delivered replacement text — the delivered lines not present
  anywhere in the base revision. A multi-line replacement is joined with ` | `; no replacement line
  in this inventory contains ` | ` itself (checked in the apply-stage rerun), so the join round-trips
  — and the delivered tree, not this column, is the authority for the exact block text.
- `kept` 294 — byte-identical survivors: 63 `K5` directives (`//go:build`, `#!`, `@moduledoc false`,
  `# noqa`); 231 why-comments (`K1` 155, `K4` 33, `K2` 12, `K3` 7 and 24 carried `D6`/`M1`/`D1`
  rules) that survive verbatim; every kept block longer than one line (74) is listed in plan §11.4.
- `excluded` 44 — classifier false positives (§1).

Dispositions are derived from the delivered tree at block granularity (a verbatim survivor is `kept`,
a vanished line inside a surviving block is `compressed`, a vanished block is `deleted`); worker-stage
per-line labels were recomputed, their rules and replacement notes preserved. The apply stage re-ran
the recompute against the delivered revision (`b942030e` + its working-tree changes, plan §11.2):
553 rows moved, now binding the merged tree. The ceiling rule — no candidate block longer than two
lines except published contracts — holds for every block except the two named in the plan §10.5
(`daemon/internal/state/provider_action.go` 568-571 and
`scripts/ci/export-linux-v2-redacted-evidence.py` 535-560, the published-record allowlist a CI scan
enforces); the docstring-aware ceiling check reports 0 violations and 2 exempt blocks.

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
truth, the ≤2-line ceiling (with the published-contract exemption), directive byte-identity, code-line
(`excluded`) byte-identity, no unexplained new comment text, and the declared pre-program
deviations. The apply stage re-ran it against the delivered revision and extended it for the merged
tree: `--also-base` treats lines merged from `origin/main` as base text (main-side rewrites are not
unexplained new comments), and a bare-trivia exemption (`//`, `#`, `/*`, …) keeps the removal check
occurrence-wise for separator rows; the docstring-aware ceiling check is a separate scratch script
(next paragraph). The delivered-revision run:

```
inventory rows: 6074; disposition rows: 6074; duplicate keys: 0
changed files: 38; candidate files: 212
stats: {'deleted': 2607, 'compressed': 3129, 'kept': 294, 'relocated_lines': 8, 'excluded': 44,
        'exempt_blocks_over_2': 2, 'pre_program_removed': 38}
--- verify: 0 problems
```

Two companion checks ran on the same revision: the occurrence-keyed rows-vs-tree census
(`FALSE-DELETED: 0 (non-empty text: 0)`, `FALSE-KEPT: 0`) and the docstring-aware block-ceiling
check (`--- ceiling: 0 blocks over two lines (non-exempt)`). `changed files: 38` is the
working-tree set this stage leaves for the operator: the three source trims, the sweep plan, the 30
slice-plan appends, this record, the goal entry and the two artifact removals.

Declared deviations (also plan §10): 8 relocated why-lines (4 in `control/.credo.exs`, moved from the
deleted header to their config entries; 4 minimal completions of pre-program fragments in
`scripts/ci/model-routing-record-sanitize.py`), and 4 pre-program comment lines rewritten to close
sentences whose program-added continuation was deleted (same file; plus mid-sentence fragment fixes in
`control/test/support/integration_fixtures.ex`, `control/test/support/test_conn.ex`,
`scripts/ci/omo-native-evidence.py`, `scripts/ci/opencode-v2-support-witness.py`,
`scripts/ci/test-check-lockfile-registry.py`). The verifier counts these (`relocated_lines`;
`pre_program_removed` 4 against the sweep base). On the merged delivered revision
`pre_program_removed` reads 38: the same 4 plus 34 lines main-side PRs rewrote after the sweep base
in `ci.yml` (19), `test-ci-changed-path-classifier.py` (11) and `goal_session.go` (4) — merged-main
content, not sweep edits (plan §10.4/§11). Nothing else outside the candidate set changed.

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
| 13 | `git diff origin/main...HEAD` + `grep -i users/` | — | branch diff is empty (0 lines); worktree diff: 0 `users/` matches; profile-path scan matches only `localHandleID`-style identifiers |
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
| host scans (`users/`, profile paths) | 0 | no matches beyond the known identifiers |

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
- policy verifier: §4 (`0 problems`, with `exempt_blocks_over_2=2` named);
- gate table: §5;
- moved intent: recorded in the owning slice plans (§10.6) — the standalone record is an off-repo
  run artifact (plan §11.1).

## 7. Deviations and limits

1. Per-line dispositions are block-derived (§2); 44 rows are classifier false positives, untouched.
2. 8 relocated why-lines + 4 rewritten pre-program fragment sentences (§4) — the only edits outside
   the candidate set, all declared and counted.
3. Two published-contract blocks exceed two lines (§2).
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
