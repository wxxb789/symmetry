# comment-policy-sweep evidence (2026-11-22)

Contract: `goal-comment-policy-sweep.md` (GDD, 2026-11-22 — held until the pre-policy land queue
merged; dispatched at `origin/main` `f1fe1e59`). Plan:
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](../plans/2026-11-22-comment-policy-sweep-plan.md).
Judging artifacts: the per-line inventory
[`comment-policy-sweep-2026-11-22-inventory.tsv`](comment-policy-sweep-2026-11-22-inventory.tsv)
(6,074 rows) and the moved-intent record
[`comment-policy-sweep-2026-11-22-moved-intent.md`](comment-policy-sweep-2026-11-22-moved-intent.md).

Delivered tree: `f1fe1e59` plus the uncommitted working-tree changes listed in §1; every delivered
state this record cites was measured on that tree. All commands ran foreground on 2026-11-22 in this
worktree (Windows/amd64, go1.27.4, portable Elixir 1.20.4/OTP 29, ruff 0.17.5, pnpm 12.17.0); exit
codes and outputs are verbatim.

## 0. Change inventory

| carrier | value |
|---|---|
| files changed | 195 (of the 212 candidate files; 17 candidate files had only `kept`/`excluded` rows and are byte-identical) |
| `git diff --shortstat` | see §6 gate 12; comment lines only, no code token, no string, no directive |
| candidate files | 212 |
| candidate lines | 6,074 inventory rows = 6,030 refined candidates + 44 classifier false positives (`excluded`) |
| dispositions | deleted 2,956 · compressed 2,836 · kept 238 · excluded 44 |
| rules | `K1` 2,316 · `D1` 1,479 · `D2` 415 · `D6` 406 · `K4` 393 · `M1` 353 · `K3` 255 · `K2` 224 · `D3` 71 · `K5` 63 · `M2` 30 · `D5` 25 · `CODELINE` 44 |
| new files | this record, the inventory TSV, the moved-intent record, the slice plan |
| source files outside the 212 | `docs/plans/*` notes (20 slice plans), `docs/TODO.md`, `docs/goals/0010-refactor-program.md` |

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
delivered tree and carry `disposition=excluded`, `rule=CODELINE`.

## 2. Dispositions

One row per candidate line in the inventory TSV (`file`, `line`, `kind`, `disposition`, `rule`,
`text`, `replacement`):

- `deleted` 2,956 — the line's text is gone from the delivered tree, its block is gone or shortened.
- `compressed` 2,836 — the line's text is gone while its block survives as a ≤2-line *why*; the
  `replacement` column carries the delivered block text.
- `kept` 238 — byte-identical survivors: 63 `K5` directives (`//go:build`, `#!`, `@moduledoc false`,
  `# noqa`) and 175 `K4`/`K1`–`K3` why-comments that were already ≤2 lines.
- `excluded` 44 — classifier false positives (§1).

Dispositions are derived from the delivered tree at block granularity (a verbatim survivor is `kept`,
a vanished line inside a surviving block is `compressed`, a vanished block is `deleted`); worker-stage
per-line labels were recomputed, their rules and replacement notes preserved. The ceiling rule — no
candidate block longer than two lines except published contracts — holds for every block except the
two named in the plan §10.5 (`daemon/internal/state/provider_action.go` 568-571 and
`scripts/ci/export-linux-v2-redacted-evidence.py` 535-560, the published-record allowlist a CI scan
enforces); the verifier reports 0 violations and 2 exempt blocks.

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

`python verify.py --base f1fe1e59 --artifact docs/evidence/comment-policy-sweep-2026-11-22-inventory.tsv`
checks: coverage (every inventory row dispositioned exactly once), vocabulary, per-block delivered
truth, the ≤2-line ceiling (with the published-contract exemption), directive byte-identity, code-line
(`excluded`) byte-identity, no unexplained new comment text, and the declared pre-program
deviations:

```
inventory rows: 6074; disposition rows: 6074; duplicate keys: 0
changed files: 195; candidate files: 212
stats: {'compressed': 2836, 'kept': 238, 'deleted': 2956, 'relocated_lines': 8, 'excluded': 44,
        'exempt_blocks_over_2': 2, 'pre_program_removed': 4}
--- verify: 0 problems
```

Declared deviations (also plan §10): 8 relocated why-lines (4 in `control/.credo.exs`, moved from the
deleted header to their config entries; 4 minimal completions of pre-program fragments in
`scripts/ci/model-routing-record-sanitize.py`), and 4 pre-program comment lines rewritten to close
sentences whose program-added continuation was deleted (same file; plus mid-sentence fragment fixes in
`control/test/support/integration_fixtures.ex`, `control/test/support/test_conn.ex`,
`scripts/ci/omo-native-evidence.py`, `scripts/ci/opencode-v2-support-witness.py`,
`scripts/ci/test-check-lockfile-registry.py`). The verifier counts these exactly
(`relocated_lines`, `pre_program_removed`); nothing else outside the candidate set changed.

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

- per-line inventory with dispositions: `comment-policy-sweep-2026-11-22-inventory.tsv` (6,074 rows);
- per-file counts: plan §9 (212 rows);
- strip-proof result: §3 (`207 PASS`), carrier validation `0 problems`;
- policy verifier: §4 (`0 problems`, with `exempt_blocks_over_2=2` named);
- gate table: §5;
- moved intent: `comment-policy-sweep-2026-11-22-moved-intent.md` plus the "Comment-policy sweep"
  notes appended to the owning slice plans.

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
