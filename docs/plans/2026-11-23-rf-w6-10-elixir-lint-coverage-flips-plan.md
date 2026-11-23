# rf-w6-10 Elixir lint/coverage flips — plan

Base: `95d1df45` (`origin/main` at dispatch, the contract's dependency chain merged: W1-01 #157 →
W2-01 → W2-17 → W3-01 → W6-01 → W6-02 → W6-11 #268). Contract:
`goal-rf-w6-10-elixir-lint-coverage-flips.md`, binding text as amended by the 2026-11-21 reconcile
(RATCHET, not flip-to-zero; `arch-decision-2-rulings.md` R13, `arch-decision-audit-deepseek-1.md`
I4) and the 2026-11-22 comment policy. Shape reference: the `rf-w6-11` plan/evidence pair (the
wave-6 `ci.yml`-adjacent slice that landed immediately before this one).

## What lands

| file | change |
| --- | --- |
| `.github/workflows/ci.yml` | the Credo step becomes the blocking ratchet gate: the three `exit 0` skip branches and the job-level `continue-on-error: true` are gone, the step is renamed, and it ends in `assert-credo-strict-baseline.py` instead of `exit "$credo_status"`; `coverage-control` loses its job-level `continue-on-error: true` (its `needs`/`if`/artifact upload and the two other coverage jobs are untouched) |
| `scripts/ci/assert-credo-strict-baseline.py` | new: the fail-closed report parser and the ratchet verdict (exit 0 green / 1 breach or unusable report / 2 unreadable log or missing or malformed baseline) |
| `scripts/ci/test-assert-credo-strict-baseline.py` | new: the gate's suite (44 tests: the shipped baseline's own report, a verbatim Credo 1.7.19 capture, every refusal the parser and the ratchet can raise, and both baseline-validation failures) |
| `scripts/ci/credo-strict-baseline.json` | new: the committed baseline artefact (revision, issue total, per-category split, analyzed-file floor, check floor) |
| `scripts/ci/credo-strict-capture-fixture.log` | new: the verbatim capture of a real Credo 1.7.19 `--strict` run (three `control/lib` files) the gate's suite parses green |
| `scripts/ci/test-ci-changed-path-classifier.py` | the coverage-advisory pin becomes per job: `coverage-control` must *not* carry `continue-on-error`, `coverage-daemon`/`coverage-frontend` must; one mutation case restores the flag to prove the new check is not vacuous; the `control/mix.exs` coverage threshold is pinned above zero, with mutations for `threshold: 0` and for the key's removal |
| `scripts/ci/test-ci-structure-extraction.py` | the two advisory allowlists the flip makes stale: the Credo step leaves `ADVISORY_STEPS` and `coverage-control` leaves `ADVISORY_JOBS`; one mutation case marks the Control coverage job advisory again to prove the job-set equality is not vacuous, and the Credo gate step gets its own pin (present, no advisory flag, no `exit 0` skip path, body ending in the gate program) with three mutations |
| `control/mix.exs` | `test_coverage: [summary: [threshold: <baseline − 1 point>]]` and the one comment that names where the number comes from |
| `control/.credo.exs` | **unchanged**: its check list is the instrument the ratchet measures; the head measurement and revision live in `scripts/ci/credo-strict-baseline.json`, so restating them in the config comment would be a second copy |
| `docs/plans/2026-11-23-rf-w6-10-elixir-lint-coverage-flips-plan.md` | this plan |
| `docs/evidence/rf-w6-10-elixir-lint-coverage-flips-2026-11-23.md` | the evidence record |
| `docs/TODO.md` | the count-0 residual (the ruling keeps it open) and the amended `parse_timeout` clause of the existing Credo/actionlint item |

## The binding decision this implements

The 2026-11-21 reconcile supersedes the drafted "flip at count zero": `mix credo --strict` is
blocking **against a committed baseline count**, it fails closed when Credo did not run, when the
report holds a line the parser cannot account for, or when the analyzed-file count is zero or below
the committed floor; the baseline may only decrease, in the same change as the fix. The measured
strict count at the base is **564** (5 warnings / 326 refactoring / 160 readability / 73 design) over
**225 files** and **70 checks** (`4527 mods/funs`), identical in the `dev` environment the CI step
runs in and in `test`; zero is not reachable inside W6 (the ≈55-finding alias share W6-11's Styler
delivered, plus the formatter's own +120 `MaxLineLength`, leave the total above the #157 record of
516), which is why the ruling replaced the zero target.

## Implementation choices the contract leaves to this plan

1. **The ratchet is per category, and a stale ceiling fails too.** `compare` requires the measured
   split to equal the baseline's per category (`design`, `readability`, `refactor`, `warning`,
   `consistency`), not merely the total: a rise names the category and refuses; a fall refuses as
   well, because the ruling's "the baseline may only decrease, in the same change as the fix" makes
   the recorded number the single source of truth — a ceiling nobody lowered would let a later
   regression back into the gap. The analyzed-file and check counts are **floors** (`measured <
   recorded` refuses), so adding files or checks stays green while a narrowed `.credo.exs` scope or
   a disabled/skipped check refuses. The contract's clause 3 ("a count above baseline accepted", "a
   disabled/removed check") is enforced by the same comparison.

2. **Two new `scripts/ci/` artefacts and a suite, which the drafted owned-file list does not name.**
   The owned list predates the reconcile that introduced the ratchet: it still describes a flip at
   zero and lists only `ci.yml`, `control/.credo.exs`, `control/mix.exs` (plus the classifier suite
   "only if the flip requires a fixture update" and the plan file). The ruling's mechanism needs a
   committed baseline *artefact* and a fail-closed *parser*; both live beside the gate conventions
   the repository already uses (`assert-*.py` + `test-assert-*.py` + `citest`, the shape
   `assert-folded-gates.py`, `assert-exunit-json.py` and `jscpd-changed.py` use). The alternative —
   inlining a 1500-line-report parser as shell inside `ci.yml` — was rejected as untestable and
   against the repo's own gate convention. Nothing under `control/` moves, no job is added or
   removed, and the classifier's routing pins are untouched, so the permission breach is three new
   files in the CI-gate directory, recorded here rather than guessed silently.
   The two structural suites are the contract's own "only if the flip requires a fixture update"
   case: they pin the advisory *sets* the flip changes (`Measure Credo strict findings (advisory)`
   in `ADVISORY_STEPS`, `coverage-control` in `ADVISORY_JOBS`, and the per-job advisory pin in the
   classifier suite), so the flip cannot land green without them — each edit is the fixture, and
   each carries its mutation case.

3. **The parser is anchored to Credo 1.7.19's own rendering, and everything it cannot account for
   refuses.** `control/.credo.exs` sets `strict: true`, so Credo renders *every* issue (`all: true`)
   under four fixed category sections; the banner `Checking N source files ...` carries the analyzed
   count, the closing `Analysis took ... running C checks on N files` repeats it, and the summary
   line carries the per-category wording. The gate requires: both counts agree, every section's
   parsed issue blocks equal the summary's count for that category, the section's `[D]`/`[R]`/`[F]`/
   `[W]`/`[C]` tags match the section title, the sections keep Credo's fixed order, and every line
   from the banner to the end of the log is one of the accounted shapes. Anything else — a
   `--all`-style truncation note, a coloured line, a `(N more)` line, an added warning block —
   refuses instead of under-reporting. Lines *before* the banner are `mix`'s own compiler output and
   are tolerated, with one exception that must never be tolerated: Credo's
   "Some source files could not be parsed correctly / were not parsed in the time allotted" block
   (printed before the banner) refuses outright, because a skipped source file is exactly the
   deflation the ruling forbids.
   Credo's exit status is a **bitmask** of issue severities (30 for this tree: `2|4|8|16`), not an
   issue count, so the step records it and the gate never compares it; the gate's own verdict is the
   step's exit status.

4. **Coverage: the threshold is arithmetic, and the runs are the contract's ≥3.** `mix test --cover`
   prints a per-module table and a `Total` row; `Mix.Tasks.Test.Coverage` fails the run
   (`exit({:shutdown, 3})`) when the **total** is below `test_coverage[:summary][:threshold]`
   (`mix/lib/mix/tasks/test.coverage.ex`, the portable toolchain). The threshold is
   `min(three measured totals) − 1.00 point`, never a round number: the floor sits one point under
   the weakest of three full-suite runs at the rebased head. The runs stage the portal bundle
   (`pnpm --filter @symmetry/frontend build` copies it into `control/priv/static/portal`), i.e. the
   same four `:portal_assets` cases CI's `coverage-control` runs. On this host pnpm cannot re-link the
   workspace (its local package mirror lacks `@colors/colors` and npmjs metadata fetches fail), so the
   bundle came from the two commands the `build` script runs, over the sibling rf-w5-08 worktree's
   frontend link tree (its frontend lock closure is byte-identical; evidence record §1). The runner's environment is the
   host's local PostgreSQL with a slice-owned database (`POSTGRES_DB=symmetry_control_test_w610`);
   `MIX_TEST_PARTITION` is unset so no other slice's database is touched. The CI job's printed
   `Total` is the confirming oracle: it runs on Linux, and if its total is more than a point below
   the local minimum the fix stage re-runs the arithmetic on the CI-observed number (never a hand
   pick), which is the same one-line edit in `control/mix.exs`.

5. **Step renames are free.** No script references the old step names: the classifier suite pins the
   coverage jobs' *commands*, uploads and working directories, and the control job's folded-gate
   assertion (`assert-folded-gates.py --job control`) names the Linux Pi folded steps only. The
   renamed steps are "Enforce the committed Credo strict baseline" and "Run the Control suite with a
   coverage report"; the latter's "(advisory)" label would otherwise be false.

## Edge-case semantics

| case | verdict |
| --- | --- |
| the strict count rises in one category, the total unchanged | exit 1, the category named |
| the strict count falls (a fix landed without the baseline edit) | exit 1, the new counts and the artefact path named |
| a category the baseline does not record appears | exit 1, that category against a recorded 0 |
| fewer analyzed files / fewer checks than the baseline records | exit 1, both named |
| zero analyzed files (`No files found!`) | exit 1 |
| a source file skipped (invalid or over `parse_timeout`) | exit 1, before the report is even parsed |
| the `Checking ...` banner or the closing `Analysis took ...` line is missing | exit 1 (Credo did not run to completion) |
| banner and closing line disagree on the analyzed count | exit 1 |
| a truncated section (`all: false`) against a full summary | exit 1 (the accounting rule) |
| a coloured log (`color: true`, or an ANSI-forcing wrapper) | exit 1; the parser strips nothing |
| a CRLF log (a Windows developer's redirect) | parsed; `\r\n` is normalized before the line scan |
| the log file is unreadable, or the baseline is missing or malformed | exit 2 (`cannot judge`), never a green verdict |
| a baseline recording 0 files, 0 checks, 0 issues, an unknown category, or a split that does not sum | exit 2 |
| `mix credo --strict` exits 30 (issues present) with a complete report | green when the counts match; the status is recorded, not compared |

## Evidence plan (all foreground, serialized; the suite never runs beside a harness)

1. `mix credo --strict` at the base in `dev` (the CI step's environment) and `test`: the baseline
   numbers, the per-rule breakdown, the analyzed-file and check counts and the exit status.
2. The **extracted step body** from `ci.yml` run verbatim through the portable toolchain (a `mix`
   shim): green with the ratchet's metrics; the mutant (an alias-order violation in a `control/lib`
   file) reddens the same body, and the revert restores green — the control-flow evidence the
   conductor's 2026-11-21 lesson 32 asks for.
3. `python scripts/ci/test-assert-credo-strict-baseline.py` (44 tests) and
   `python scripts/ci/test-ci-changed-path-classifier.py` (34 tests, unmodified routing pins);
   `python scripts/ci/test-assert-folded-gates.py`.
4. `mix format --check-formatted`, `MIX_ENV=test mix compile --warnings-as-errors`, the targeted
   `mix test test/symmetry_control_web/`, the full `mix test`, then `mix test --cover` ×3 (locally)
   plus the threshold mutant (`threshold: 100` → exit 3) and the restored run.
5. Workflow shape: `actionlint` (no shellcheck, the host limit recorded in `docs/TODO.md`) plus a
   `shellcheck --shell=bash` pass over the changed `run:` block.
6. Hygiene: `python -m ruff check`/`format --check` on the new and changed Python, `git diff
   --check`, the DLP scans (no user-profile host path, no backslash-before-`u`), and the record
   cleanliness gate over this change's own records.

## Out of scope (with reasons)

- **The burn-down itself.** The wave-1..6 slices carry their own findings; this slice measures and
  flips. `control/.credo.exs`'s check list stays byte-identical, and no `credo:disable` is added.
- **The Go and vitest floors** (W6-12b, the wave-6 last `ci.yml` owner) and the duration ratchets.
- **The coverage job shapes** (W2-17): `needs`, `if`, the artifact upload, the report path and the
  commands are untouched; only the job-level advisory flag and the step's label change.
- **`ci.yml` structural extraction / browser wiring** (W6-01, W6-02) and the classifier's routing
  semantics (W3-01): the suite edit is the advisory pin only.
- **`control/mix.lock`**: no dependency changes, so the lock stays byte-identical (checked).
- **Zero-count blocking**: the ruling keeps it an open `docs/TODO.md` entry with the measured
  residual; it is not reachable inside W6 and no check may be disabled to approach it.
- **`control/.formatter.exs` / Styler** (W6-11, merged).

## Residual risks and limits

- **Local (Windows) vs CI (Linux) coverage totals.** The floor is derived from local runs; the CI
  `Total` is the oracle. The measured local minimum is the weakest of three runs, and the floor sits
  a further point below it, so a platform delta has to exceed a point before it bites — and if it
  does, the CI number re-runs the arithmetic (rule 4).
- **The ratchet's count is a committed artefact, not a history check**: a *manual* raise of
  `credo-strict-baseline.json` is not caught by the gate (no shallow CI checkout holds the old
  value). Review and the "only decreases" rule own that direction; the same limit applies to every
  committed count in this repository.
- **The grammar is pinned to Credo 1.7.19's rendering.** A `mix.lock` bump that changes the summary
  wording, the section titles or the issue-line shape refuses until the grammar and the baseline are
  re-measured — the intended fail-closed direction, at the cost of a re-measure in that change.
- **`parse_timeout` (30 s) stays a hang guard**: a source file above it is now a *refusal* instead
  of a printed guard, which closes the deflation path the `docs/TODO.md` item names but does not
  teach the gate to tell a timeout from a syntax error (Credo prints one list for both).
- **A rise on the merge ref that this branch did not cause** (another control change landing on
  `main`) refuses the run: that is the ratchet biting, and the fix is the finding's fix plus the
  recorded baseline — never a raise.
- **`coverage-control` is now load-bearing for every pull request** that classifies as `run-heavy`:
  its suite is the full Control suite (~9 min with instrumentation), so the floor's failure mode is
  a red job, not a red advisory step. That is the contract's "the mix coverage job is blocking".

## Outcome on the delivered tree (2026-11-23)

| what | result |
| --- | --- |
| Credo, base and head | 564 issues over 225 files and 70 checks (identical in `dev` and `test`), unchanged by this slice's own files; 98 of the 225 files carry an issue |
| the ratchet gate | the extracted `ci.yml` step body green (exit 0); the alias mutant red (exit 1, `Code Readability: 161 issue(s) against the baseline's 160`); the revert green |
| coverage | three runs 87.64 / 87.62 / 87.58 % → `test_coverage: [summary: [threshold: 86.58]]`; the delivered threshold green at 87.65 %; a below-floor subset run red at 61.21 % (exit 3) |
| suites | the gate's 44, the classifier's 34, the structure suite's 28, the folded-gate suite's 6 — all OK |
| the fix round's new pins | the Credo gate-step pin reddens on an `exit 0` body swap, on an inserted skip branch and on a restored advisory flag; the threshold pin reddens on `threshold: 0` and on the key's removal; the real-capture test reddens on a perturbed capture — each restored green |
| tree | `mix format --check-formatted` 0, `MIX_ENV=test mix compile --warnings-as-errors` 0, `mix test` 1163 passed / 21 skipped, the web tree 291 passed / 1 skipped |
| `ci.yml` | +9 / −39; the control job's other steps and `coverage-daemon`/`coverage-frontend` byte-identical |
| record | `docs/evidence/rf-w6-10-elixir-lint-coverage-flips-2026-11-23.md` (every row with its command, exit code and counts) |
