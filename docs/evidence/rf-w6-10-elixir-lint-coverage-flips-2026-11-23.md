# rf-w6-10 Elixir lint/coverage flips — evidence

Contract: `goal-rf-w6-10-elixir-lint-coverage-flips.md` (plan W6-10; binding text as amended by the
2026-11-21 reconcile: RATCHET against a committed baseline, not flip-to-zero). Base
`95d1df45` (`origin/main` at dispatch). Plan:
[`docs/plans/2026-11-23-rf-w6-10-elixir-lint-coverage-flips-plan.md`](../plans/2026-11-23-rf-w6-10-elixir-lint-coverage-flips-plan.md).
Every command below ran in this worktree (`control` work from `control/`, script work from the
repository root), foreground and serialized: the full-suite runs and the coverage runs never
overlapped another harness.

## 1. Method and toolchain

- Elixir through the portable toolchain (`erl.exe` started directly with the switches `elixir.bat`
  uses: Elixir 1.20.4 / OTP 29.0.6); the CI control job's environment is `dev`, so the Credo step's
  own body was reproduced with `MIX_ENV=dev`, and the coverage runs with `MIX_ENV=test`.
- PostgreSQL 18.6 on the host's local instance, addressed as `POSTGRES_DB=symmetry_control_test_w610`
  with `MIX_TEST_PARTITION` unset, so no other slice's database is touched; the host's stale
  `PHX_SERVER` / `POSTGRES_PORT` / `SYMMETRY_CONTRACTS_DIR` exports are unset for every run.
- Portal bundle: `control/priv/static/portal` holds the frontend's production build, so the four
  `:portal_assets` cases run — the same four CI's `coverage-control` stages the artifact for. Host
  deviation: pnpm 12.17.0 cannot re-link the workspace here (its local package mirror lacks
  `@colors/colors`, and `registry.npmjs.org` metadata fetches fail with a TLS handshake error, both
  reproduced), so the bundle was produced by the two commands the `build` script runs —
  `node node_modules/vite/bin/vite.js build` and `node scripts/stage-assets.mjs` from `frontend/` —
  over the sibling rf-w5-08 worktree's frontend link tree, whose `pnpm-lock.yaml` frontend closure is
  byte-identical to this revision's (the only lock delta is rf-w6-11's `jscpd` devDependency at the
  workspace root; `vite` resolves to the same `vite@8.3.8_@types+node@26.6.6_jiti@2.7.0_yaml@2.9.1`).
  `frontend/`, `contracts/` and `control/` sources are this revision's own bytes.
- Python 3.12 with the repository-pinned ruff 0.17.5; `actionlint` 1.7.12 without shellcheck (the
  host limit recorded in `docs/TODO.md`).

## 2. What landed

| file | change |
| --- | --- |
| `.github/workflows/ci.yml` | the Credo step is the blocking ratchet gate (`continue-on-error` and the three `exit 0` skip branches gone, renamed, ending in the gate program); `coverage-control` drops its job-level `continue-on-error`, its step loses the `(advisory)` label and its comment names the mix threshold |
| `scripts/ci/assert-credo-strict-baseline.py` | new: the fail-closed report parser and ratchet (exit 0 / 1 / 2) |
| `scripts/ci/test-assert-credo-strict-baseline.py` | new: 42 tests |
| `scripts/ci/credo-strict-baseline.json` | new: the committed baseline (564 issues, 225 files, 70 checks, split 5/326/160/73 at `95d1df45`) |
| `scripts/ci/test-ci-changed-path-classifier.py` | the advisory pin split per job + one mutation case (32 tests) |
| `scripts/ci/test-ci-structure-extraction.py` | `ADVISORY_STEPS` loses the Credo step, `ADVISORY_JOBS` loses `coverage-control` + one mutation case (24 tests) |
| `control/mix.exs` | the mix coverage threshold (arithmetic below) and its comment |
| `docs/plans/2026-11-23-rf-w6-10-elixir-lint-coverage-flips-plan.md`, this record | plan and evidence |
| `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | the count-0 residual (new) and the closed Credo half of the actionlint item |

`control/.credo.exs` and `control/mix.lock` are **unchanged**; no `control/lib` or `control/test`
byte changes, so no burn-down finding was touched.

Line delta (D2, report-only), `git diff --numstat` plus the new files' line counts at hand-over:
`ci.yml` **+9 / −39** (the Credo step body shrinks from 37 to 9 lines; the coverage job loses one line
and gains two comment lines), `control/mix.exs` **+3 / −3** (the threshold and its comment), the
classifier suite **+37 / −1**, the structure suite **+11 / −2**, `docs/TODO.md` +14 / −3,
`docs/archive/CLOSED-TODO.md` +15 / −0, and the new files: the gate **326** lines, its suite **405**,
the baseline artefact **13**, the plan **194** and this record **224**. No `control/lib` or
`control/test` line changes.

## 3. The base→head control flow (conductor lesson 32)

| item | base | head |
| --- | --- | --- |
| Credo step name | `Measure Credo strict findings (advisory)` | `Enforce the committed Credo strict baseline` |
| Credo step `continue-on-error` | `true` | absent |
| Credo step `if: always()` | present | present (unchanged) |
| Credo step `timeout-minutes` | 15 | 15 (unchanged) |
| skip branches (`exit 0`) | 3 (missing `:credo` dep, missing `.credo.exs`, unfetched deps) | 0 |
| verdict | `exit "$credo_status"` (Credo's bitmask, swallowed by `continue-on-error`) | the gate's exit code; the bitmask is echoed as a metric |
| metrics echoed | `credo_strict_summary`, `credo_strict_issue_total`, `credo_strict_unparsed_files` | `credo_strict_exit_status`, `credo_strict_issue_total`, `credo_strict_files_analyzed`, `credo_strict_checks_run`, `credo_strict_scopes`, `credo_strict_<category>` ×4 |
| Credo step stderr | dropped (only stdout was teed) | `2>&1` into the log, so a skipped-file complaint is parsed |
| control job's other steps | — | byte-identical (`git diff` touches no other line of the job) |
| `coverage-control` job `continue-on-error` | `true` | absent |
| `coverage-control` `needs` / `if` / timeout / services / env / steps / artifact upload | — | byte-identical except the step's label and its comment |
| `coverage-daemon`, `coverage-frontend` | advisory | advisory (unchanged; W6-12's flips) |
| control job's folded-gate assertion (`assert-folded-gates.py --job control`) | 8 Linux Pi gates | 8 Linux Pi gates (the Credo step was never in the array — re-derived at this revision) |
| classifier routing pins (`check_coverage_plane_routing`) | — | unmodified; the edited pins are the advisory-flag ones |

## 4. Credo: the measured baseline (revision, per-rule breakdown, unparsed files)

`mix credo --strict` at the base revision, whole output, in the CI step's `dev` environment and in
`test` (the two agree byte-for-byte on every count):

| run | exit | `mods/funs` | issues | files | checks |
| --- | ---: | ---: | ---: | ---: | ---: |
| dev (CI's env) | 30 | 4527 | 564 | 225 | 70 |
| test | 30 | 4527 | 564 | 225 | 70 |

Per-category split (the summary line's own wording): 5 warnings, 326 refactoring opportunities, 160
code readability issues, 73 software design suggestions. The per-issue accounting is exact: 564
issue blocks parse out of the log (73 `[D]`, 326 `[F]`, 160 `[R]`, 5 `[W]`) and each section equals
the summary's count for its category. Skipped/unparsable source files: **0** (no complaint block, and
the gate would refuse the run if one appeared). The `checks: %{}` trap recorded in `control/.credo.exs`
was not touched: the explicit check list is byte-identical.

Per-rule breakdown from the same revision with `mix credo --strict --format json` (exit 30, one JSON
object; its `issues` array cross-checks the text total at 564 and the same category split:
refactor 326, readability 160, design 73, warning 5), counted per `check`:

| rule | issues |
| --- | ---: |
| `Readability.MaxLineLength` | 159 |
| `Refactor.Nesting` | 155 |
| `Refactor.CyclomaticComplexity` | 154 |
| `Design.DuplicatedCode` | 60 |
| `Design.AliasUsage` | 13 |
| `Refactor.FunctionArity` | 11 |
| `Refactor.FilterFilter` | 3 |
| `Warning.RaiseInsideRescue` | 3 |
| `Refactor.Apply` | 3 |
| `Warning.ExpensiveEmptyEnumCheck` | 1 |
| `Readability.PredicateFunctionNames` | 1 |
| `Warning.MissedMetadataKeyInLoggerConfig` | 1 |
| **total** | **564** |

The 564 issues sit in 98 files of the 225 analyzed; `Readability.AliasOrder` is at 0 (rf-w6-11's
Styler pass), and the `MaxLineLength` count is the formatter's own line joining, per that slice's
record.

## 5. The gate: what the CI step now runs

`scripts/ci/assert-credo-strict-baseline.py` reads the step's log and refuses unless every line from
Credo's `Checking N source files ...` banner on is accounted for (banner and closing report agree on
the analyzed count; each section's issue blocks equal the summary's count for its category; the
`[D]`/`[R]`/`[F]`/`[W]`/`[C]` tags match the section title; the sections keep Credo's fixed order;
the closing report is complete), the analyzed-file count and the check count are at least the
baseline's, and the per-category issue counts equal the baseline's exactly — a rise refuses as a
regression, a fall refuses as a stale ceiling that must be recorded in the same change as its fix.
Credo's own exit status is reported, never compared (30 is a bitmask of severities: `2|4|8|16`).

The base→head effect on the step, measured with the **extracted step body** run verbatim:

| run | command (from `control/`) | exit |
| --- | --- | ---: |
| base body | the `95d1df45` step body, verbatim, same shim (`credo_strict_summary=4527 mods/funs, found 5 warnings, 326 refactoring opportunities, 160 code readability issues, 73 software design suggestions.`, `credo_strict_issue_total=564`, `credo_strict_unparsed_files=0`) | 30 (swallowed by the job's `continue-on-error` in CI) |
| head body, green | the `ci.yml` step body, verbatim, `RUNNER_TEMP` set, portable `mix` shim | 0 |
| head body, mutant (a) | same body with one `Readability.AliasOrder` violation introduced in `control/lib/symmetry_control/orchestration.ex` (two adjacent `alias` lines swapped) | 1 |
| head body, restored | same body after reverting the mutation | 0 |

The mutant run reports `credo_strict_issue_total=565` (files and checks unchanged at 225/70) and
fails with `Code Readability: 161 issue(s) against the baseline's 160 (the strict count rose; fix
the finding, never raise the baseline)` — the gate names the category and the direction, and its
exit status, not Credo's bitmask, is the step's verdict.

## 6. Coverage: three runs, the arithmetic, and the floor's mutant

`mix test --cover` in `control/` (the CI job's command), each run a full suite with coverage:

| run | exit | tests | `Total` | wall |
| --- | ---: | --- | ---: | ---: |
| 1 | 0 | 1163 passed, 21 skipped | 87.64 % | 481 s |
| 2 | 0 | 1163 passed, 21 skipped | 87.62 % | 464 s |
| 3 | 0 | 1163 passed, 21 skipped | 87.58 % | 349 s |

Threshold arithmetic: `min(87.64, 87.62, 87.58) − 1.00 point = 86.58` → `control/mix.exs` carries
`test_coverage: [summary: [threshold: 86.58]]`. `Mix.Tasks.Test.Coverage` compares the **total**
(`totals < threshold` → `System.at_exit(fn _ -> exit({:shutdown, 3}) end)`), so the floor is one point
under the weakest of the three measured runs, never a hand-picked round number. The three totals span
0.06 points, so the point of headroom is over sixteen times the spread observed across the three runs.

| floor check | command | exit | output |
| --- | --- | ---: | --- |
| the delivered threshold | `MIX_ENV=test mix test --cover` with the shipped `control/mix.exs` | 0 | `1163 passed, 21 skipped`, `Total` **87.65 %** ≥ 86.58 (389 s) |
| the below-floor mutant | `MIX_ENV=test mix test --cover test/symmetry_control_web/` — a suite subset under the *delivered* threshold, i.e. a run whose `Total` sits below the floor | 3 | `291 passed, 1 skipped`, then `Coverage test failed, threshold not met:` / `Coverage: 61.21%` / `Threshold: 86.58%` (57 s) |

The CI job's own `Total` is the confirming oracle (Linux runner): if it lands more than a point below
the local minimum, the fix stage re-runs the arithmetic on the CI-observed number — the same one-line
`control/mix.exs` edit, never a hand pick.

## 7. Gate table (exact commands, exit codes, counts)

| gate | command | exit | counts / output |
| --- | --- | ---: | --- |
| the gate's suite | `python scripts/ci/test-assert-credo-strict-baseline.py` | 0 | `Ran 42 tests … OK` |
| classifier suite | `python scripts/ci/test-ci-changed-path-classifier.py` | 0 | `Ran 32 tests … OK` (routing/artifact pins unmodified; the advisory pin split, one mutation case added) |
| structure suite | `python scripts/ci/test-ci-structure-extraction.py` | 0 | `Ran 24 tests … OK` (both advisory allowlists updated, one mutation case added) |
| folded-gate suite | `python scripts/ci/test-assert-folded-gates.py` | 0 | `Ran 6 tests … OK` |
| elixir format | `mix format --check-formatted` (`control/`) | 0 | no output |
| elixir compile | `MIX_ENV=test mix compile --warnings-as-errors` | 0 | no output |
| targeted web suite | `mix test test/symmetry_control_web/` | 0 | `291 passed, 1 skipped` in 39.3 s (quiet-host run; the first run under load hit the recorded 100 ms `assert_receive` flake at `portal_integrations_test.exs:699`, 290/291, and the re-run is the row) |
| full suite | `mix test` (`control/`, `POSTGRES_DB=symmetry_control_test_w610`) | 0 | `1163 passed, 21 skipped` in 392 s |
| coverage ×3 | `mix test --cover` ×3 | 0 | §6 |
| citation gate (over this change's emulated range) | `python scripts/ci/check-citation-existence.py --repo <throwaway clone> --base 95d1df45 --head 1b961c4a (the emulated commit)` | 0 | 1 changed document (CLOSED-TODO.md), 535 path citations, 3 ignore-covered notices |
| comment cap (conductor amendment) | `python scratch/comment-cap.py <throwaway clone> 95d1df45` | 0 | no comment block longer than 2 lines added by the branch |
| record cleanliness | `python scripts/ci/check-record-cleanliness.py --repo <clone> --base 95d1df45 --head 1b961c4a` | 0 | `1 changed record(s) pass the root-only check` |
| workflow shape | `actionlint -color -shellcheck= -pyflakes= .github/workflows/ci.yml` | 0 | 0 findings |
| changed run block | `shellcheck --shell=bash` over the extracted Credo step body | 0 | 0 findings |
| python hygiene | `python -m ruff check` + `--format --check` on the new/changed Python | 0 | `All checks passed!`, all files formatted |
| diff hygiene | `git diff --check` | 0 | clean |
| DLP | added lines and the new files scanned for host-profile paths and backslash-`u` (`grep -Eic`) | 1¹ | 0 hits |

¹ `grep` exits 1 for "no line selected", which is the passing state; the zero-valued count is the
evidence.

## 8. Mutation checks

The contract's three mutation reds, each observed before the tree was restored (temporary diffs,
never committed), plus one row per guard the gate's suite pins:

| # | mutation | observed |
| --- | --- | --- |
| a | two adjacent `alias` lines swapped in `control/lib/symmetry_control/orchestration.ex` (one `Readability.AliasOrder` violation) | the **extracted CI step body** exits 1 with `Code Readability: 161 issue(s) against the baseline's 160 (the strict count rose; …)`; the revert restores exit 0 (§5) |
| b | a coverage run whose `Total` sits below the floor (the web-tree subset under the delivered 86.58) | `mix test --cover test/symmetry_control_web/` exits 3 with `Coverage test failed, threshold not met: Coverage: 61.21% / Threshold: 86.58%`, while the full run under the same threshold is green at 87.65 % (§6) |
| c | the classifier/structure suites | re-gating `coverage-daemon` on `run-citations`, dropping its advisory line, dropping its `if: always()`, dropping its `defaults.run`, restoring `continue-on-error: true` to `coverage-control`, and marking `coverage-control` advisory again each redden the matching check (the suite cases themselves) |
| d | the gate's own guards | each refusal has its own test in §2's suite: a rise, a fall, a new category, files below the floor, checks below the floor, a skipped source file, an unparsable-file preamble, a missing banner, a truncated report, disagreeing counts, an out-of-order category, an out-of-order summary fragment, a crossed tag, a section with no issue, an unaccounted line inside a section, an unaccounted line after the report, a coloured line, a `--all`-style truncation, a missing summary, a missing analysis line, a missing or non-strict closing line, a malformed/missing/empty/non-integer baseline and a non-positive category count |

## 9. Residual risks and limits

- The floor is derived from local Windows runs; the CI `Total` is the oracle (rule 4 of the plan). The
  delivered `Total` (87.65 %) sits one point above it, so a Linux delta would have to exceed a point.
- The web tree carries one load-sensitive case (`portal_integrations_test.exs:699`, a 100 ms
  `assert_receive`); it failed once under concurrent host load and passed in every quiet run recorded
  here (two full-suite runs and the two coverage runs included).
- The ratchet's artefact is a committed count, not a history check: a manual raise of
  `credo-strict-baseline.json` is not caught by a shallow CI checkout (review owns that direction).
- The parser's grammar is pinned to Credo 1.7.19's rendering; a Credo upgrade refuses until the
  grammar and the baseline are re-measured — intended, and recorded in the plan.
- Credo prints one numbered list for a timeout and for an unparsable file; the gate refuses either,
  so the distinction is a reporting limit, not a deflation path.
- `control/.credo.exs`'s record comment still carries its #157 measurement (516/153 at `c3a1725a`);
  it is history, and the live number lives in the baseline artefact.
- The changed-file duplication gate (rf-w6-11's `jscpd-changed.py`, wired by rf-w6-12) judges the
  range's *added* lines: an approximation check over this change (every 10-consecutive-non-blank-line
  window of the new and changed files against an index of the tracked `.py`/`.sh`/`.ps1`/`.go`/`.exs`/
  `.yml`/`.json`/`.md` files) found three duplicate windows, all inside `ci.yml` blocks whose lines all
  exist at `95d1df45` (the pnpm/Node setup block and the SQL fingerprint block), and none over an added
  line; the exact token-level gate remains rf-w6-12's to run.
