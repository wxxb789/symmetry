# rf-w6-11 Styler adoption + changed-file jscpd gate — evidence

Contract: `goal-rf-w6-11-styler-jscpd.md` (D12, D16; plan W6-11). Base
`cafd5488` (`origin/main` at dispatch, the contract's dependency list merged). Plan:
`docs/plans/2026-11-23-rf-w6-11-styler-jscpd-plan.md`. Every command below ran in this
worktree (`control` work runs from `control/`, script work from the repository root),
foreground and serialized (the one suite that could collide with the jscpd scans was
run after them).

## 1. Method and toolchain

- Elixir through the portable toolchain: `mise x elixir@1.20.4-otp-29 erlang@29.1.2 -- mix …`
  (Elixir 1.20.4 / OTP 29.1.2), PostgreSQL on the host's local instance.
- Host environment deviation: the shell exports `PHX_SERVER`, `POSTGRES_PORT` and a
  `SYMMETRY_CONTRACTS_DIR` pointing at another checkout; the suite runs with those
  unset (`env -u PHX_SERVER -u POSTGRES_PORT -u SYMMETRY_CONTRACTS_DIR`) and
  `POSTGRES_DB=symmetry_control_test_w611`, so no other slice's database or contracts
  tree is touched. Recorded here because a reader reproducing the run needs it.
- Node through the pinned pnpm 12.17.0 (`mise x pnpm@12.17.0 -- pnpm …`); jscpd is the
  repository's devDependency (`jscpd@4.3.0`, `node_modules/jscpd`), never `npx`.
- Python 3.12; `ruff` via `python -m ruff` (the repository pins 0.17.5, line 120).
- Environment note: on this host the `mise x … -- mix …` shim occasionally leaves an
  `erl_crash.dump` at the repository root (a boot-time `io.put_chars` encoding failure
  of a probe process; the recorded `mix` commands themselves exit with the codes
  below). Each dump was deleted before the tree was handed over; none is part of the
  change.

## 2. What landed

| file | change |
| --- | --- |
| `control/mix.exs` | `{:styler, "~> 1.12", only: [:dev, :test], runtime: false}` |
| `control/mix.lock` | `styler 1.12.2` (`1aa29422…`, hex) |
| `control/.formatter.exs` | `plugins: [Styler]` (the existing keys untouched) |
| `control/**` | the mechanical format pass: 187 files, `+5301 −6303` (5 `config`, 76 `lib`, 102 `test`, `.credo.exs`, `mix.exs`, `mix.lock`, `.formatter.exs`) |
| `package.json`, `pnpm-lock.yaml` | `jscpd@4.3.0` devDependency; the lockfile closure was generated in a scratch tree (this host's mirror cannot resolve four unrelated frontend dev pins) and spliced in; `check-lockfile-registry.py` green over 405 tarball URLs |
| `.jscpd.json` | the committed scope: corpus ignores, `mode: mild`, the explicit format table |
| `scripts/ci/jscpd-changed.py` | the changed-file gate (exit 0 tolerated / 1 refused / 2 unusable) |
| `scripts/ci/test-jscpd-changed.py` | the gate's suite, 17 tests over real throwaway Git repositories |
| `docs/plans/2026-11-23-rf-w6-11-styler-jscpd-plan.md`, this record | plan and evidence |
| `.github/workflows/ci.yml` | **not touched** — W6-12 inserts the step whose text §9 delivers |

## 3. The format pass: hunk-class audit (D12, lesson 32)

The first run was reviewed end to end, not sampled. Method: for each changed file the
base revision's and the head revision's ASTs were compared after metadata stripping and
named normalizations (pipe normalization; `assert`/collection/time normal forms;
negation/comparison/branch normal form; alias expansion; directive/attribute moves),
then the residue was read hunk by hunk.

| class | files |
| --- | --- |
| layout only (whitespace, line joins, indentation) | 28 |
| pipe normalization | 39 |
| `assert`/collection/`DateTime`/`:timer` normal forms | 37 |
| alias expansion | 16 |
| negation/comparison/branch normal form | 15 |
| directive/attribute moves | 7 |
| control-flow shapes (`cond`→`if`, `with` bare clauses) | 3 |
| residue (reviewed individually, table below) | 33 |
| **analysed `control/lib` + `control/test` files** | **178** |

The remaining 9 changed files are not Elixir-AST-carriers (`control/config/*.exs`,
`control/.credo.exs`, `control/mix.exs`, `control/mix.lock`,
`control/.formatter.exs`); the five config scripts and `.credo.exs` were audited by
statement multiset instead (§3.1).

Residue classes and their dispositions. The counts are diff lines carrying the
pattern (removed → added, measured over the 27 419-line `control` diff) — they are the
reproducible part of this table; the per-hunk review is what the dispositions record.

| rewrite class | diff lines | reviewed behaviour-identical |
| --- | --- | --- |
| `unless` → `if !(…)` (condition negated, both bodies kept) | 197 → 1 `unless` | yes |
| `DateTime.add(amount, unit)` → `DateTime.shift/2` (1.20 port) | 162 → 159 `DateTime.shift` | yes: same amounts and units |
| `:timer.seconds(n)` → `to_timeout(...)` | 6 → 8 `to_timeout` | yes: same durations |
| `try do` → implicit `try` in a function body | 9 removed | yes: rescued clauses and `after` unchanged |
| `assert x != nil` → `assert x` (test files) | 8 → 9 bare `assert …` | yes: the pinned values are non-nil; the assertion still fails on `nil` |
| `Map.drop/2` → `Map.delete/2` | 5 → 14 `Map.delete` | yes: same keys, same receiver |
| `cond` → `if` | 6 → 1 `cond` | yes: the guarded clauses and their fall-through are unchanged |
| `with` clause re-shaping (`case`/body moves) | 26 → 15 `with` | yes: every clause keeps its pattern; `else`/error branches reach the same returns |
| `@moduledoc false` added | 34 added | yes: a doc-visibility attribute |
| sigil rewrites (`~r`/`~s` with the same content) | 8 ↔ 8 `~r` | yes: byte-equal after unescaping |

The audit's AST rungs (unchanged-but-reviewed files) additionally prove 28 files are
layout-only, 39 pipe normalization, 37 `assert`/collection/time normal forms, 16 alias
expansion, 15 negation/comparison/branch normal form, 7 directive/attribute moves and
3 control-flow shapes; those files' diff was re-read as well (the rung only routes the
review, it does not replace it).

### 3.1 Config scripts

Method: `Code.string_to_quoted!` per file, each top-level statement stripped of `line`
metadata and compared as a sorted multiset (base revision vs head revision), with
`Macro.to_string` used to print any difference. Result:

- `control/config/config.exs` (10 statements), `dev.exs` (7), `prod.exs` (3),
  `test.exs` (10) and `control/.credo.exs` (1): **identical ASTs** modulo line metadata
  and statement order. Styler reorders top-level `config` calls; every reordered key
  path appears exactly once in its file, so no override order changed. The comment
  blocks travel with their calls, and `import_config` stays last in `config.exs`.
- `control/config/runtime.exs` (12 statements): one statement differs — inside the
  `if config_env() == :prod do` branch the plain assignments (`database_url`,
  `maybe_ipv6`, `secret_key_base`, `host`) were moved **before** the `config` calls.
  Reviewed: every referenced value is still assigned before its use (the `config
  SymmetryControlWeb.Endpoint, secret_key_base: secret_key_base` call still follows the
  assignment), the three reordered `config` calls set distinct key paths
  (`… Repo`, `… :dns_cluster_query`, `… Web.Endpoint`), and the branch's `end`/blank
  structure is intact — behaviour-identical.

## 4. Hunks rejected and re-expressed (behaviour-changing or fragile)

| # | file | what Styler did | disposition |
| --- | --- | --- | --- |
| 1 | `control/lib/mix/tasks/symmetry.check_test_db.ex` | the interpolation-aware pass **doubled an `@acceptance_databases` attribute** when the moduledoc interpolated module attributes (idempotence broke: a second pass grew the attribute again) | the moduledoc interpolations were re-expressed as literals; the rendered documentation is byte-identical (1957 characters, compared through `Code.fetch_docs`), and `mix format` is now a fixpoint (`--check-formatted` green after two passes) |
| 2 | 12 `control/test/**` files | `use SymmetryControl.TestFixtures` was hoisted above the `import GoalsTestSupport`/`import TestFixtures` lines, so the `validation_profiles()` call the import provides stopped compiling | the calls were re-expressed fully qualified (`SymmetryControl.GoalsTestSupport.validation_profiles()` / `SymmetryControl.TestFixtures.validation_profiles()`); no `import` was removed and no stub was edited. Files: `goals/{commands_flow,commands,decisions_interface,goal_control_interface,harness_session_flow,reconciliation,recovery,run_fence_flow,settlement}_test.exs`, `goals_test.exs`, `web/controllers/{goal_machine,task_goal_guard}_controller_test.exs` |

Both are the D12 caveat class ("test stub modules where a fully-qualified reference is
deliberate"); neither weakens an assertion (the calls return the same fixture maps).

## 5. Credo per-rule before/after (alias families)

`mix credo --strict --format json` in the Control project (dev env, the pinned Credo,
the repository's `.credo.exs`) on the head tree, and on the base tree extracted from
`HEAD:control` with this checkout's `deps` shared (`MIX_DEPS_PATH`) and a private
`MIX_BUILD_PATH`; both runs exit 30 (Credo's strict-findings code) and their JSON
bodies were counted per `check`. Before → after:

| rule | base | head | delta |
| --- | --- | --- | --- |
| `Readability.AliasOrder` | 26 | **0** | −26 |
| `Design.AliasUsage` | 33 | **13** | −20 |
| `Readability.WithSingleClause` | 11 | 0 | −11 |
| `Refactor.NegatedConditionsWithElse` | 17 | 0 | −17 |
| `Refactor.RedundantWithClauseResult` | 12 | 0 | −12 |
| `Readability.PreferImplicitTry` | 9 | 0 | −9 |
| `Readability.StringSigils` | 5 | 0 | −5 |
| `Refactor.CondStatements` | 5 | 0 | −5 |
| `Readability.ModuleDoc` | 3 | 0 | −3 |
| `Refactor.NegatedConditionsInUnless` | 2 | 0 | −2 |
| `Refactor.UnlessWithElse` | 1 | 0 | −1 |
| `Readability.MaxLineLength` | 39 | 159 | +120 |
| `Refactor.CyclomaticComplexity` | 153 | 154 | +1 |
| `Refactor.Nesting` 155, `Design.DuplicatedCode` 60, `Refactor.{Apply,FilterFilter,FunctionArity}` 17, `Warning.*` 5 | 237 | 237 | 0 |
| **total** | **554** | **564** | **+10** |

The alias class the contract names drops by 46 (26 `AliasOrder` + 20 `AliasUsage`),
against the audit's ≈55 estimate; the 13 remaining `AliasUsage` findings are the ones
Styler cannot fix (nested deeper than the configured two, or called more than zero
times), and the ten web-lib files #147 cleaned were already at zero. The `+120`
`MaxLineLength` is the formatter's own trade: Styler joins `def`/call lines, so strict
line-length findings rise — the D12 caveat's "config sorting / with→case" class, here as
line joining. The single `CyclomaticComplexity` +1 is one `with`→`case` rewrite. The
total (+10 of 554) is report-only per D2 and is the baseline W6-10's zero-count flip
will measure; the CI Credo step is advisory (`continue-on-error: true`), so no gate
changes colour, and this slice touched no check, threshold or suppression (the only
`.credo.exs` diff is `parse_timeout: 30000` → `30_000` and one unwrapped line, §3.1).

## 6. The changed-file jscpd gate (D16)

### 6.1 Mechanics

- Corpus: the `--head` revision's own tree, materialized with `git archive` (never the
  working tree, so a dirty checkout cannot change the verdict).
- Scan: the pinned `node_modules/jscpd` entry, `--min-lines 10 --min-tokens 50
  --max-lines 5000 --max-size 1mb` plus `--reporters json --silent --no-colors`, and
  the scanned repository's committed `.jscpd.json`.
- Verdict: a clone is refused when one of its two occurrences overlaps the range's
  added lines **and** the occurrence holds a run of at least `--min-lines` (10)
  consecutive added lines whose content the file did not already hold at the comparison
  base (whitespace-insensitive, so refolds and line joins are not new), **or** a run of
  that length whose lines were inserted into the file while the same file deleted that
  block nowhere else (a copy, not a move). Renames follow their base path. Everything
  else that reaches added lines is printed as tolerated; the exit code stays 0.
- Fail-closed edges: no report, no scanned source, a failed jscpd run, a missing pinned
  package, a missing config, an unresolvable base/tree — each exits 2. A changed file
  with added lines that jscpd did not scan (unknown format, binary, over the size
  ceiling) is a notice.

### 6.2 Why the added-line test alone is not the criterion

Measured on this PR's own changed set (187 reformatted files + the new files; whole
tree corpus, 105 clones reach an added line): the mechanical pass re-touches every line
of the files it rewrites, so an added-line-only gate refuses the pass itself — 105
clones, including 59 self-clones the formatter created by normalizing two previously
different blocks into identical text and one 103-line/838-token block between two
orchestration test files. The measured disambiguation (same corpus, per the rule above):
**3 refusals**, all blocks the new gate script shared verbatim with
`check-citation-existence.py` / `check-record-cleanliness.py` (21/17/15 lines,
185/151/135 tokens) — genuine new duplication, which the script then restructured away
(§7). The 100 tolerated clones of the final run all carry new-content runs of ≤ 4 lines
(next longest run: 4; the refused three ran 11–21).

### 6.3 Format table (jscpd 4.3.0 does not know Elixir)

`@jscpd/tokenizer`'s default `EXT_TO_FORMAT` maps no `ex`/`exs` extension, so a default
jscpd run scans none of `control/**` — the tree this slice formats. `formatsExts`
*replaces* that table (finder `getFormatByFile`), so `.jscpd.json` declares the
sixteen default-mapped formats the tree uses plus `elixir: [ex, exs]`, with the
extensions the repository inventory produced. Pinned by test: dropping the elixir entry
turns the elixir clone fixture from refused into a notice/green, and a table that
recognizes nothing exits 2 instead of passing.

## 7. Gate table (foreground, serialized; exact commands and exit codes)

| gate | command (from the lane it names) | exit | counts / output |
| --- | --- | --- | --- |
| elixir format | `mix format --check-formatted` (`control/`) | 0 | no output |
| format mutation (a) | same, after reversing four `alias` lines in `goals.ex` | 1 | names the file and prints the sorted fix |
| format revert | same, after reverting the mutation | 0 | no output |
| elixir compile | `MIX_ENV=test mix compile --warnings-as-errors` (`control/`) | 0 | no output |
| control suite | `mix test` with the host's stale `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset and `POSTGRES_DB=symmetry_control_test_w611` | 0 | `1162 passed, 21 skipped` in 272 s (the 4 portal-bundle cases included: the bundle was staged locally with `pnpm --filter @symmetry/frontend build`, gitignored build output) |
| gate suite | `python scripts/ci/test-jscpd-changed.py` | 0 | `Ran 17 tests … OK` (54 s) |
| gate, changed set (real new duplication) | `python scripts/ci/jscpd-changed.py --base HEAD --head <emulated head>` | 1 | 3 refusals: `check-citation-existence.py`/`check-record-cleanliness.py` ↔ the new gate script (21/17/15 lines, 185/151/135 tokens), 100 clones tolerated |
| gate, changed set (final tree) | same, after restructuring the preamble; re-run on the frozen tree | 0 | `194 changed file(s), 1665 scanned source(s), 1275 clone(s) in the corpus, 100 tolerated among the added lines, none new` (115 s) |
| injected-clone fixtures | inside the suite: a 12-line/≥60-token copy appended to a changed file, and the same copy inside a file that already held the block | 1 | fixture asserts the gate refuses both and names the file |
| large-file fixture (flag mutation (c)) | suite: jscpd's defaults over the same corpus, then the gate | 0 then 1 | the default report holds `duplicates: []` for the 1250-line file; the gate refuses it only because `--max-lines 5000 --max-size 1mb` are passed (`JSCPD_LIMITS` is pinned by its own test) |
| ruff | `python -m ruff check` + `python -m ruff format --check` on both new scripts | 0 | `All checks passed!` / `2 files already formatted` |
| classifier suite | `python scripts/ci/test-ci-changed-path-classifier.py` (unmodified) | 0 | `Ran 31 tests … OK` (28 s) |
| lockfile provenance | `python scripts/ci/check-lockfile-registry.py` | 0 | `verified every recorded tarball URL … 405 entries checked` |
| diff hygiene | `git diff --check` | 0 | clean |
| record cleanliness | `python scripts/ci/check-record-cleanliness.py --base HEAD --head <emulated head>` | 0 | `1 changed record(s) pass the root-only check` (the first run refused this record for naming the host-path *patterns* it scans; the wording was made generic, §12) |
| test names | 130 test files, name multisets compared base ↔ head | — | 1173 names each side, identical multisets |

## 8. Mutation checks

The three mutations the contract names, each observed as a red run before the tree was
restored:

| # | mutation | observed |
| --- | --- | --- |
| a | four `alias` lines reversed in `control/lib/symmetry_control/goals.ex` | `mix format --check-formatted` exits 1 and prints the file with Styler's sorted fix; the revert restores exit 0 (the file's only remaining diff is the pass's pipe normalization) |
| b | a 12-line/≥60-token clone introduced into a changed file | two independent reds: the suite's `test_a_changed_file_carrying_a_new_clone_is_refused` (fixture) and the real changed set before the new script's preamble was restructured (3 refusals, §7). The gate names the file, the counterpart, the sizes and the offending line run. |
| c | remove `--max-lines 5000 --max-size 1mb` from the gate | the suite's large-file test runs jscpd's defaults first and records `duplicates: []` for a 1262-line file that holds the clone (the silent skip the audit named), then asserts the gate refuses that file — with the flags gone the assertion fails; `test_the_gate_carries_the_explicit_limits` pins `JSCPD_LIMITS` verbatim |

The suite additionally pins the verdict boundary in both directions: a clean changed
file is green, a clone between two untouched files is green, a clone outside the added
lines is green, a reformat that only rewrites a clone is tolerated, a block moved
inside its file is tolerated, a block *copied* inside its file is refused, a third copy
of a pre-existing duplication is refused, a renamed file is compared against its base
path, and a changed file jscpd did not scan is a notice rather than a silent skip.

## 9. CI step text for W6-12 (handoff, D16)

The contract's handoff: W6-12 owns `.github/workflows/ci.yml` and inserts this single
step. It belongs in the `contracts` job (that job has node + pnpm and installs the
lockfile through `./.github/actions/contracts-build`); the step runs after that install
and needs the job's checkout to hold the comparison base, so the same job's checkout
gains `fetch-depth: 0`:

```yaml
      - uses: actions/checkout@v7
        with:
          fetch-depth: 0
          persist-credentials: false
      # …and, after `./.github/actions/contracts-build`:
      - name: Refuse code clones this change adds to the files it touches
        env:
          JSCPD_BASE: ${{ github.event_name == 'pull_request' && 'HEAD^1' || github.event.before }}
        run: |
          python3 scripts/ci/test-jscpd-changed.py
          python3 scripts/ci/jscpd-changed.py --base "$JSCPD_BASE" --head HEAD
```

The gate's suite and the gate itself both run in the step (the suite's fixtures are
throwaway repositories, so its cost is seconds). The `contracts` job already gates on
`needs.changes.outputs.run-heavy == 'true'`; no classifier change is required for the
step to fire on a change to the gate's own program only — W6-12 decides whether to add
the two new paths to the classifier's contract family, and this record flags it as the
one open wiring question.

## 10. Repo-wide jscpd numbers (trend-only per D16)

Both revisions scanned with the committed `.jscpd.json` and the audited thresholds
(`--min-lines 10 --min-tokens 50 --max-lines 5000 --max-size 1mb`), from `git archive`
trees, so the numbers are comparable to each other and to the audit's:

| revision | clones | lines | duplicated lines | percentage | sources |
| --- | --- | --- | --- | --- | --- |
| base `cafd5488` | 1274 | 646 277 | 29 292 | 4.53 % | 1806 |
| head (this change) | 1275 | 641 810 | 29 291 | 4.56 % | 1812 |

Head per format (sources / duplicated lines / percentage): elixir 292 / 6050 / 5.14,
json 332 / 11 614 / 26.44, markdown 345 / 339 / 0.34, python 69 / 180 / 0.53,
typescript 82 / 1615 / 9.86, tsx 39 / 342 / 2.29, go 489 / 8464 / 3.03; the other
formats stay below 2 %.

**Correction to the audit's claim, not a regression of this change:** the audit
recorded "0 clones touching `control/**`", which was *vacuous* — jscpd 4.3.0's default
format table knows no Elixir extension, so no `control/**` file was scanned at all.
With the elixir format declared, the base revision already carries **244 clones with a
`control/` side (6235 cloned lines)** and the head 250 (6300 lines, +65). The repo-wide
percentage moves 4.53 % → 4.56 % with the mechanical pass, and the new clones are the
tolerated ones §6.2 describes. Per D16 these numbers gate nothing.

## 11. Residual risks and limits

- **Formatter-created textual clones are tolerated by construction.** A clone whose
  added lines are rewrites of base lines passes (100 of them in the final gate run). A reviewer
  who wants stricter behaviour wants `mode: strict` **and** the base-content rule
  relaxed — a key change, not a limit change, and it would refuse the reformat pass.
- **A same-file copy whose block the range also deletes elsewhere passes** (the "move"
  branch). A copy that deletes an unrelated identical block at the same time is
  therefore tolerated; the content-new branch still refuses copies of text the file did
  not hold.
- jscpd's clone intervals are token-anchored and, for chained clones, occasionally
  inverted (`start > end` in 85 of 2548 sides over the whole corpus). Attribution uses
  the reported interval, so such a side is only treated as reached when the interval
  itself overlaps added lines; the tolerated/refused decision never depends on the
  fragment text.
- The corpus is the head *tree*, so a clone that exists only in the diff context (base
  carried a block the head moved) is judged on head's bytes.
- The three changed-file gates (`check-citation-existence`, `check-record-cleanliness`,
  `jscpd-changed`) repeat the same ~20-line CLI/range preamble. This gate's copy was
  restructured to the minimum (its own help texts, a local refusal helper), which is
  what the gate's own run demanded; extracting `revision_scope.parse_range` for all
  three is the follow-up and is outside this slice's owned set.

## 12. DLP and path hygiene

Scanned every path this change adds or modifies (the `git status --porcelain -uall`
inventory, 194 entries at the final tree) for a host-profile path (a user-profile
segment, an application-data segment, a drive-letter absolute path, a worktree root)
and for a backslash immediately before `u`:

- the two new scripts, `.jscpd.json`, `package.json`, the plan and this record: no
  hit (the only temporary-directory match is Python's `import tempfile`);
- the 187-file `control` diff: 0 host-path hits; 3 lines carry the escape sequence,
  all of them **context** lines in the diff (`git diff HEAD -- control | grep -c '^+.*\\u'`
  = 0), so the pass adds no such byte;
- the pre-existing occurrences in 8 `control` files are unchanged lines.
