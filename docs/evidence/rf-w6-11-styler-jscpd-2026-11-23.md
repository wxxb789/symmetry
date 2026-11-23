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
| `control/**` | the format pass at the frozen head: 188 files, `+5311 −6304` (5 `config`, 76 `lib`, 103 `test`, `.credo.exs`, `mix.exs`, `mix.lock`, `.formatter.exs`) |
| `package.json`, `pnpm-lock.yaml` | `jscpd@4.3.0` devDependency; the lockfile closure was generated in a scratch tree (this host's mirror cannot resolve four unrelated frontend dev pins) and spliced in; `check-lockfile-registry.py` green over 405 tarball URLs |
| `.jscpd.json` | the committed scope: corpus ignores, `mode: mild`, the explicit format table |
| `scripts/ci/jscpd-changed.py` | the changed-file gate (exit 0 tolerated / 1 refused / 2 unusable) |
| `scripts/ci/test-jscpd-changed.py` | the gate's suite, 32 tests over real throwaway Git repositories |
| `docs/plans/2026-11-23-rf-w6-11-styler-jscpd-plan.md`, this record | plan and evidence |
| `.github/workflows/ci.yml` | **not touched** — W6-12 inserts the step whose text §9 delivers |

## 3. The format pass: hunk-class audit (D12, lesson 32)

The first run was reviewed end to end, not sampled. Method: for each changed file the
base revision's and the head revision's ASTs were compared after metadata stripping and
named normalizations (pipe normalization; `assert`/collection/time normal forms;
negation/comparison/branch normal form; alias expansion; directive/attribute moves),
then the residue was read hunk by hunk.

The delivered bytes are the plugin's own **second-pass** fixpoint for two lib files,
not a single pass (Styler's line-break and alias-dedup decisions depend on the incoming
newline metadata): `lib/symmetry_control/integrations/provider_access.ex` (the
`Repo.update_all` line join at :727; the duplicate aliases inside `defmodule IntentContext`
removed at :1094) and `lib/symmetry_control/orchestration/transitions.ex` (:878, the
same line join). A base tree plus one `mix format` pass leaves those two files differing
from the head; the second pass matches it (16 → 14 differing files, the 14 being the §4
re-expressions and their pinning test). Both deltas are behaviour-neutral: the removed
aliases are all declared in the enclosing module (`provider_access.ex:8-22`, and Elixir
inherits outer aliases into a nested `defmodule`), and the rest is line breaking.

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

### 3.2 Rewrite classes the audit table missed (fix stage, REV-09)

Method: `git grep -o` occurrence counts over `control/` at both revisions, plus the
diff lines that carry each pattern; each class was re-read hunk by hunk.

| rewrite class | occurrences base → head | reviewed behaviour-identical |
| --- | --- | --- |
| `Map.merge/2` with one key → `Map.put/3` | 148 → 137 `Map.merge(`, 417 → 428 `Map.put(` | yes: the same receiver and the same single key |
| `DateTime.compare/2` against `:gt`/`:lt` → `DateTime.after?/2`/`DateTime.before?/2` | 36 → 18 `DateTime.compare(`, 0 → 14 `DateTime.after?`, 0 → 4 `DateTime.before?` | yes: the same operands in the same order, so the same boolean |
| `unless` → `if !` (condition negated, both bodies kept) | 212 → 16 `unless `, 0 → 149 `if !` | yes (§3's row, re-measured by occurrence) |
| `not x` → `!x` | 831 → 818 `not ` | yes: the diff carries 13 more `not `-shaped lines on the base side (76 → 63), each pair the same negation |
| `with` → `case` | 465 → 454 `with ` | yes, with one note: where a bare `with` became a `case`, an unmatched value raises `CaseClauseError` instead of `WithClauseError`. The suite's one `assert_raise WithClauseError` (`control/test/symmetry_control_web/controllers/protocol_controller_test.exs:1308`) pins a function the pass left as a `with` — the green suite run is the check — and no test asserts on a converted site |
| `if cond, do: nil, else: X` → `if !cond, do: X` (the review's example) | 11 → 11 `do: nil, else:` | not a rewrite class as stated: the two diff lines that carry the pattern are refolds of the same expressions, and the pattern occurs as often at both revisions |

## 4. Hunks rejected and re-expressed (behaviour-changing or fragile)

| # | file | what Styler did | disposition |
| --- | --- | --- | --- |
| 1 | `control/lib/mix/tasks/symmetry.check_test_db.ex` | the interpolation-aware pass **doubled an `@acceptance_databases` attribute** when the moduledoc interpolated module attributes (idempotence broke: a second pass grew the attribute again) | the moduledoc interpolations were re-expressed as literals; the rendered documentation is byte-identical (1957 characters, compared through `Code.fetch_docs`), and `mix format` is now a fixpoint (`--check-formatted` green after two passes). The apply stage pins the rendered moduledoc against the reserved values in `symmetry_check_test_db_test.exs`, so a literal edited apart from its attribute reddens |
| 2 | 12 `control/test/**` files | `use SymmetryControl.TestFixtures` was hoisted above the `import GoalsTestSupport`/`import TestFixtures` lines, so the `validation_profiles()` call the import provides stopped compiling | the calls were re-expressed fully qualified (`SymmetryControl.GoalsTestSupport.validation_profiles()` / `SymmetryControl.TestFixtures.validation_profiles()`); no `import` was removed and no stub was edited. Files: `goals/{commands_flow,commands,decisions_interface,goal_control_interface,harness_session_flow,reconciliation,recovery,run_fence_flow,settlement}_test.exs`, `goals_test.exs`, `web/controllers/{goal_machine,task_goal_guard}_controller_test.exs`. The apply stage drops the `validation_profiles: 0` entry the re-expression left dead in `goal_machine_controller_test.exs`'s `only:` list (the `import` line itself and every other entry unchanged) |

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
The apply-stage re-run (`mix credo --strict`, same checkout after the two control-test
edits) exits 30 with the same total: 564 (5 warnings / 326 refactoring / 160
readability / 73 design) — report-only.

## 6. The changed-file jscpd gate (D16)

### 6.1 Mechanics (fix stage: REV-01 to REV-07)

- Corpus: the `--head` revision's own tree, materialized with `git archive` (never the
  working tree, so a dirty checkout cannot change the verdict). A member tarfile
  refuses to unpack — an absolute-target symlink, say, which Git stores as a plain
  blob — is reported as an unusable corpus (exit 2), never as a traceback.
- Scan: the pinned `node_modules/jscpd` entry, `--min-lines 10 --min-tokens 50
  --max-lines 5000 --max-size 1mb` plus `--reporters json --silent --no-colors`, and
  `.jscpd.json` as the *comparison base* holds it (read from the base revision's blob;
  the head's only when the base holds none), so a range that rewrites the scope cannot
  exempt its own files — the run prints which revision governs, and the edit applies
  from the next range on.
- Verdict: a clone is refused when one of its two occurrences overlaps the range's
  added lines **and** holds a run of at least `--min-lines` (10) consecutive added
  lines the file did not already hold as often at the comparison base. "Held" counts
  line-boundary-aligned occurrences of the line's whitespace-free text in the same
  path's base revision (`TextIndex`): a refold or line join still matches, while a
  comment marker — or any other neighbouring token — does not. Uncommenting a block the
  base held only commented is refused, a second copy of a block the file already held is
  refused (a rename no longer launders it through the old path's deleted text), and a
  clone that only re-covers lines a reformat rewrote is tolerated. Renames follow their
  base path. Everything else that reaches added lines is printed as tolerated; the exit
  code stays 0.
- The diff that supplies the added-line ranges is read with `--no-ext-diff
  --no-textconv`: an inherited `GIT_EXTERNAL_DIFF`, a `diff.external` driver or a
  text-conversion filter can no longer replace the patch and hide every added line.
- Fail-closed edges: no report, no scanned source, a failed jscpd run, a missing pinned
  package, no config at base or head, an unresolvable base/tree, a quoted diff path, an
  unpackable archive — each exits 2. One exception: when jscpd writes no report because
  every changed file with added lines is shorter than `--min-lines`, no clone of that
  length can live in them, and the run reports `nothing to check` (exit 0) instead of
  reddening a legitimate small change. A changed file with added lines that jscpd did
  not scan is a notice naming the reason (shorter than the line threshold, or outside
  the configured scope); a binary carries no hunk, so it has no added lines to notice.

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
(§7). The 100 tolerated clones of the final run hold no run of 10+ added lines whose
content the file did not already hold (the longest such run is 4; the refused three ran
11-21). The fix-stage re-run of the gate over the frozen range (§7) reproduces the
boundary: 100 tolerated, 0 refusals.

### 6.3 Format table (jscpd 4.3.0 does not know Elixir)

`@jscpd/tokenizer`'s default `EXT_TO_FORMAT` maps no `ex`/`exs` extension, so a default
jscpd run scans none of `control/**` — the tree this slice formats. `formatsExts`
*replaces* that table (finder `getFormatByFile`), so `.jscpd.json` declares fifteen of
the sixteen default-mapped formats the tree uses plus `elixir: [ex, exs]`, with the
extensions the repository inventory produced. Pinned by test: dropping the elixir entry
turns the elixir clone fixture from refused into a notice/green, and a table that
recognizes nothing exits 2 instead of passing.

**Fix stage (REV-05):** the inventory had missed the repository's data and
configuration extensions, so a changed `.jsonl` file could only reach a notice.
`formatsExts` now also maps `json: [json, jsonl]`, `elixir: [ex, exs, lock]`,
`go: [go, mod]`, `ini: [ini, in]`, `markup: [html, eex]`, `nginx: [conf]`,
`txt: [txt, sum]` and `docker: [Dockerfile]` (the extension of a `*.Dockerfile` file is
the capitalised word, and `getFormatByFile` matches extensions case-sensitively).
Measured: two `.jsonl` files sharing a 15-line block are scanned and the clone is
refused (`test_a_changed_jsonl_file_carrying_a_clone_is_refused`), while a `.xyz` file
stays outside the table and is a notice naming the scope
(`test_a_changed_file_in_an_undeclared_format_is_a_notice`).

## 7. Gate table (foreground, serialized; exact commands and exit codes)

| gate | command (from the lane it names) | exit | counts / output |
| --- | --- | --- | --- |
| elixir format | `mix format --check-formatted` (`control/`) | 0 | no output |
| format mutation (a) | same, after reversing four `alias` lines in `goals.ex` | 1 | names the file and prints the sorted fix |
| format revert | same, after reverting the mutation | 0 | no output |
| elixir compile | `MIX_ENV=test mix compile --warnings-as-errors` (`control/`) | 0 | no output |
| control suite | `mix test` with the host's stale `PHX_SERVER`/`POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset and `POSTGRES_DB=symmetry_control_test_w611` | 0 | `1163 passed, 21 skipped` in 331 s, apply-stage re-run (the 4 portal-bundle cases included: the bundle was staged locally with `pnpm --filter @symmetry/frontend build`, gitignored build output). The fix stage touches no `control/**` byte (`git diff` over `control/` is empty), so this run still binds the frozen head |
| control compile (fix stage) | `MIX_ENV=test mix compile --warnings-as-errors` (`control/`) | 0 | no output; the tree is the stage head's |
| gate suite | `python scripts/ci/test-jscpd-changed.py` | 0 | `Ran 32 tests … OK` (fix-stage re-run; 23 tests before the fixes, §13) |
| gate, changed set (real new duplication) | `python scripts/ci/jscpd-changed.py --base HEAD --head <emulated head>` | 1 | 3 refusals: `check-citation-existence.py`/`check-record-cleanliness.py` ↔ the new gate script (21/17/15 lines, 185/151/135 tokens), 100 clones tolerated |
| gate, changed set (frozen stage head) | `python scripts/ci/jscpd-changed.py --base b9d4b4a4 --head 482a37f8` (the range exactly as frozen; the base holds no `.jscpd.json`, so the run reports that the head's scope governs) | 0 | `195 changed file(s), 1677 scanned source(s), 1272 clone(s) in the corpus, 100 tolerated among the added lines, none new` (92 s; 4 notices — `.formatter.exs` shorter than 10 lines, `mix.lock`, `pi_control_e2e_test.exs`, `pnpm-lock.yaml` outside the configured scope) |
| gate, changed set (fix-stage tree) | the fix-stage tree committed in a throwaway clone (`git clone --shared`, the working tree's `scripts/ci/jscpd-changed.py` sha256 `d0f285bb…`, `.jscpd.json` sha256 `43fed1bf…`, both copied in and committed; the command runs the worktree script with `--repo <clone>`) | 0 | `195 changed file(s), 1700 scanned source(s), 1273 clone(s) in the corpus, 100 tolerated among the added lines, none new` (140 s; the scope is the extended table, so 23 more sources are scanned than at the frozen head and `mix.lock` is no longer a notice) |
| injected-clone fixtures | inside the suite: a 12-line/≥60-token copy appended to a changed file, and the same copy inside a file that already held the block | 1 | fixture asserts the gate refuses both and names the file |
| large-file fixture (flag mutation (c)) | suite: jscpd's defaults over the same corpus, then the gate | 0 then 1 | the default report holds `duplicates: []` for the 1250-line file; the gate refuses it only because `--max-lines 5000 --max-size 1mb` are passed (`JSCPD_LIMITS` is pinned by its own test) |
| ruff | `python -m ruff check` + `python -m ruff format --check` on both new scripts | 0 | `All checks passed!` / `2 files already formatted` |
| classifier suite | `python scripts/ci/test-ci-changed-path-classifier.py` (unmodified) | 0 | `Ran 31 tests … OK` (28 s) |
| lockfile provenance | `python scripts/ci/check-lockfile-registry.py` | 0 | `verified every recorded tarball URL … 405 entries checked` |
| diff hygiene | `git diff --check` | 0 | clean |
| record cleanliness | `python scripts/ci/check-record-cleanliness.py --base HEAD --head <emulated head>` | 0 | `1 changed record(s) pass the root-only check` (the first run refused this record for naming the host-path *patterns* it scans; the wording was made generic, §12) |
| test names | 130 files under `control/test` (109 `_test.exs`), name multisets compared base ↔ head | — | 1173 names at the base, 1174 at the head; **no test renamed or removed**, one test name added (`names every reserved acceptance database in the task documentation`, §13) |

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
The apply stage adds: a copier whose path git C-quotes (a non-ASCII name, and a space
in the name) is refused by that name; a content line shaped like a diff header
(`++ b/decoy.py` added, `-- first` removed) is content and never a header; a self-copy
merged into an edit hunk is refused as copied-not-moved; and a dirty working-tree
config cannot change the verdict (a revision blob supplies it — the comparison base's
when it holds one, the fix stage's rule in §6.1).

The fix stage adds: a renamed file appending a verbatim copy of its own block is refused
(the same content appended without the rename already was); a block the base held only
commented is refused when uncommented; blank lines inside a cloned block are not new
content; a range whose only change is a 3-line file is green (`nothing to check`); a
range cannot exempt itself by rewriting the scope config; an external diff driver — in
the environment or in the repository config — cannot hide the added lines; an absolute
symlink in the corpus is a refusal (exit 2, no traceback); a `.jsonl` clone is refused
under the extended table; and a `.xyz` file stays a notice that names the scope.

**Apply-stage mutation checks.** Each mutant ran alone on the apply-stage tree and was
reverted before the next step; `scripts/ci/jscpd-changed.py` was restored to
`9ffee509…` and `scripts/ci/revision_scope.py` to `5faf04b3…` (sha256 verified), and no
other file carries a mutant byte.

| # | mutant | observed |
| --- | --- | --- |
| d | the pre-apply gate restored (the `4eb4682d` bytes) in place of the hardened script | 6 suite reds: both header-shaped-content fixtures (added: `0 != 1`, a false green; removed: a false refusal), both quoted/spaced-path copiers (`0 != 1`), the mixed-hunk self-copy (`0 != 1`), and the detection-mode test's dirty-config step (`1 != 0`: the working-tree config decided) |
| e | the reached-side test swapped from `edits.added` to `edits.inserted` | the reformat fixture reddens (`1 tolerated among the added lines` not found; the gate printed `0 tolerated … none new`) |
| f | the interval-overlap test dropped from the reached-side test | the clone-outside-added-lines fixture reddens (the gate printed `1 tolerated … none new` for a clone no added line reaches) |
| g | `D` added to `revision_scope.SELECTED_STATUSES` | the deleted-file fixture reddens (`nothing to check` missing; the gate took the non-empty path) |
| h | one reserved name replaced in the task moduledoc (`symmetry_0007e_` → `…f_`) | the new documentation assertion reddens (`assert moduledoc =~ value` for `symmetry_0007e_`) |

**Fix-stage mutation checks (the fix's own guards).** Each mutant ran alone on the
fix-stage tree, its named fixture was re-run, and the file was restored before the next
step (`scripts/ci/jscpd-changed.py` sha256 `d0f285bb…` verified after every step):

| # | mutant | observed |
| --- | --- | --- |
| i | the scope read from the head revision again (`at_base = ""`) | `test_a_range_cannot_exempt_itself_by_rewriting_the_scope_config` reddens (exit 0: the range's own `ignore` decides) |
| j | newness by absence only (`base_count == 0` instead of `< head_count`) | `test_a_renamed_file_appending_a_verbatim_copy_of_its_block_is_refused` reddens (exit 0: the copy's lines exist in the old path, so no line is absent) |
| k | occurrence counting without line boundaries (`if True:` in `TextIndex`) | `test_a_block_the_base_held_only_commented_is_refused_when_uncommented` reddens (exit 0: the uncommented lines match inside the base's comment text) |
| l | the missing-report branch always benign (`citable = []`) | `test_a_corpus_without_a_scannable_source_is_refused` reddens (exit 0 although a changed file could hold a clone) |
| m | the missing-report branch never benign (`citable = ["sentinel"]`) | `test_a_range_whose_changed_files_cannot_hold_a_clone_is_green` reddens (exit 2 on a legitimate small change) |
| n | the unpack moved outside its refusal (the `try`/`except` removed) | `test_an_absolute_symlink_in_the_corpus_is_refused` reddens (exit 1 with a `tarfile.AbsoluteLinkError` traceback instead of the refusal) |
| o | `--no-ext-diff --no-textconv` dropped from the added-line diff | `test_an_external_diff_driver_cannot_hide_the_added_lines` reddens (exit 0: the external driver's empty patch hides every added line) |
| p | `jsonl` dropped from the scope (`json: [json]`) | `test_a_changed_jsonl_file_carrying_a_clone_is_refused` reddens (exit 0 with a notice) |
| q | `too_short_to_clone` always false | `test_a_changed_file_jscpd_cannot_scan_is_a_notice_not_a_pass` reddens (the notice stops naming the line threshold) |

Two rules were checked and are not decision guards in the fixture space: the
whitespace-only early return in `TextIndex.line_occurrences` (removing it left
`test_blank_lines_inside_a_cloned_block_are_not_new_content` green — the empty needle
then counts collapsed line boundaries, and no fixture inserts a blank-only run into a
reached clone side of a *new* file), and the blank-line tolerance itself, which that
fixture pins instead (a 12-blank-line insertion inside a clone stays `1 tolerated`).

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

Both revisions scanned with the `.jscpd.json` this branch ships (the fix stage's table,
REV-05) and the audited thresholds (`--min-lines 10 --min-tokens 50 --max-lines 5000
--max-size 1mb`), from `git archive` trees, so the numbers are comparable to each other
and to the audit's:

| revision | clones | lines | duplicated lines | percentage | sources |
| --- | --- | --- | --- | --- | --- |
| base `cafd5488` | 1275 | 646 776 | 29 302 | 4.53 % | 1829 |
| head (this change) | 1273 | 645 830 | 29 250 | 4.53 % | 1848 |

Head per format (sources / duplicated lines / percentage): elixir 293 / 6050 / 5.14,
json 345 / 11 624 / 26.37 (32 `.jsonl` testdata files included), markdown 348 / 339 /
0.33, python 69 / 180 / 0.53, typescript 82 / 1615 / 9.86, tsx 39 / 342 / 2.29, go 500 /
8413 / 2.99, txt 9 / 335 / 23.02 (`go.sum` and the other plain-text files), yaml 24 /
46 / 1.52; the other formats stay below 2 % — including the newly declared `docker`
(3 sources), `ini` (2), `markup` (4) and `nginx` (1), which carry no duplicated lines.
The extended table is why the base's sources rise from the earlier measurement's 1806 to
1829 while the head's reach 1848.

**Correction to the audit's claim, not a regression of this change:** the audit
recorded "0 clones touching `control/**`", which was *vacuous* — jscpd 4.3.0's default
format table knows no Elixir extension, so no `control/**` file was scanned at all.
With the elixir format declared, the base revision already carries **244 clones with a
`control/` side (6235 cloned lines)** and the head 250 (6300 lines, +65); the clone
verdicts here are the elixir-namespaced ones §6.2 describes, and the extended table adds
sources in other namespaces without changing them. The repo-wide percentage stays 4.53 %
with the mechanical pass at this table (the earlier table's 4.56 % was the same pass
measured without the data-format extensions). Per D16 these numbers gate nothing.

## 11. Residual risks and limits

- **Formatter-created textual clones are tolerated by construction.** A clone whose
  added lines are rewrites of base lines passes (100 of them in the final gate run). A reviewer
  who wants stricter behaviour wants `mode: strict` **and** the base-content rule
  relaxed — a key change, not a limit change, and it would refuse the reformat pass.
- **A copy whose every line keeps its multiplicity passes.** The fix stage's rule is
  the occurrence count (a line is new when the file holds *more* copies of it than the
  base did); a range that copies a block and deletes an equal-content block elsewhere in
  the same file is therefore tolerated. The base rule's "inserted block not in the
  file's deletions" test caught some of those shapes and was replaced deliberately: its
  own false-positive class (a rename or reflow whose block text changed) was REV-01's
  cause. Every copy that grows a line's multiplicity is refused, which the rename
  fixture pins.
- **A whitespace-only line is always held** (the empty needle has no occurrences), so a
  blank-only run can never refuse a range;
  `test_blank_lines_inside_a_cloned_block_are_not_new_content` pins the tolerance.
- **The first range that introduces `.jscpd.json` is judged by the scope it writes** —
  there is no earlier scope to read. The window is one range wide, the config diff is
  visible, and every later range is judged by the base's table.
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
  three — the `--name-status -z` field reader this gate repeats from
  `revision_scope.changed_paths` included — is the follow-up and is outside this
  slice's owned set.

## 12. DLP and path hygiene

Scanned every path this change adds or modifies (the `git status --porcelain -uall`
inventory, 194 entries at the final tree) for a host-profile path (a user-profile
segment, an application-data segment, a drive-letter absolute path, a worktree root)
and for a backslash immediately before `u`:

- the two new scripts, `.jscpd.json`, `package.json`, the plan and this record: no
  hit (the only temporary-directory match is Python's `import tempfile`);
- the 187-file `control` diff: 0 host-path hits; 3 lines carry the escape sequence,
  all of them **context** lines in the diff (a `git diff HEAD -- control | grep -c` run
  whose pattern matches an added line holding a backslash immediately before the letter
  `u` returns 0), so the pass adds no such byte;
- the pre-existing occurrences in 8 `control` files are unchanged lines.

**Fix stage:** the same two scans over the working tree's diff (the gate script, its
suite, `.jscpd.json`, the plan and this record): no host-profile path, no drive-letter
absolute path, no mount/scratch/home prefix, and no added backslash-before-`u` line.
The machine check is `check-record-cleanliness.py` over the fix-stage tree (a throwaway
clone, `--base b9d4b4a4 --head <fix-stage head>`) — see §13.

## 13. Fix stage (rf-w6-11 review, 2026-11-23)

The review returned `MUST_FIX (7)` over the frozen range `b9d4b4a4..482a37f8`
(195 changed paths; `control` 188 files, `+5311 −6304`), with four evidence findings.
This stage fixes all of them inside the owned set (the gate script, its suite,
`.jscpd.json`, this record and the plan); no finding was judged invalid, and the only
recorded disagreement is the measured one in §3.2's last row. The gate's own verdict on
the frozen head was re-measured and is unchanged (195/1677/1272/100/0, §7).

Findings and their fixes — each behavioural one was reproduced against the pre-fix tree
first (a throwaway repository per case, the same command shape, exit codes observed):

| id | reproduced (before) | after | fix |
| --- | --- | --- | --- |
| REV-01 | a renamed file appending a verbatim copy of its own block: exit 0 (`1 tolerated … none new`); the same content appended without the rename: exit 1 | exit 1 | newness is the line's occurrence count in the same path's base revision (`TextIndex`, `Occurrence`), not absence from the old path's deleted text |
| REV-02 | a block the base held only commented, uncommented at the head: exit 0, `1 tolerated` | exit 1 (`new content`) | occurrences must be line-boundary-aligned: a match inside a comment's text is not content the file held |
| REV-03 | a range that commits `ignore: ["**/src/left.py"]`: exit 0, the changed file only a notice | exit 1 | the scope is read from the comparison base (`governing_scope`); the run reports which revision governs |
| REV-04 | `diff.external` or `GIT_EXTERNAL_DIFF` set to a no-op: exit 0, `none new`, every added line silently unmatched | exit 1 | the added-line diff is read with `--no-ext-diff --no-textconv` |
| REV-05 | a changed `.jsonl` whose added lines clone another `.jsonl`: exit 0, notice only | exit 1 | `formatsExts` extended to the repository's data/configuration extensions (§6.3) |
| REV-06 | a range whose only changed file is 3 lines: exit 2, `wrote no report` | exit 0, `nothing to check` | the missing-report refusal yields when no changed file with added lines can hold a `--min-lines` clone |
| REV-07 | a range adding an absolute-target symlink: `tarfile.AbsoluteLinkError` traceback, exit 1 | exit 2, `cannot materialize the corpus` | the unpack runs inside the corpus refusal |
| REV-08 | the record bound the gate row and the `control` counts to `4eb4682d`, not the frozen head | — | §2/§7 rows re-bound to `482a37f8` (195 paths, `188 files, +5311 −6304`), both gate runs recorded |
| REV-09 | §3's class table omitted `Map.merge`→`Map.put`, `DateTime.compare`→`after?/before?`, `not`→`!` and the `with`→`case` exception-class note | — | §3.2 states each class with its measured counts and the note; the review's `if …, do: nil, else:` example is measured as not-a-class |
| REV-10 | the 8 `assert x != nil` → `assert x` conversions had no per-point record | — | the per-point review below |
| REV-11 | the record claimed identical test-name multisets, while the head carries one added test name | — | §7's test-name row states **no test renamed or removed** and names the added test; the claim is corrected, not the test |

**REV-10 per-point review (the substitute for a mutation check, as the finding allows).**
The eight conversions pin values that cannot be `false`; `assert x` fails on `nil` (as
before) and additionally on `false`, so each conversion is strictly stronger and no
guarded behaviour was dropped:

| site | pinned value |
| --- | --- |
| `orchestration_goal_assignment_test.exs:541` | `runtime.id` — a runtime registry id |
| `workspaces_read_model_test.exs:144`, `:264` | `execution.timing.state_since` — an ISO-8601 timestamp |
| `portal_integrations_test.exs:329` | `checked["last_checked_at"]` — a rendered timestamp |
| `portal_resources_test.exs:322`, `:425`, `:475`, `:549` | `synced["last_synced_at"]` / `before.last_synced_at` / `resource_snapshot(...).last_synced_at` — rendered timestamps |

**REV-11 disclosure.** `control/test` holds 130 files, 109 of them `_test.exs`; the test
name multiset is 1173 at the base and 1174 at the head. The one added name
(`names every reserved acceptance database in the task documentation`, in
`symmetry_check_test_db_test.exs`) is the apply stage's documentation assertion, already
disclosed in §4 and the plan; no test was renamed, deleted or retagged (the 103 changed
test files' `assert`/`refute` line multisets were compared independently in the review).

**Fix-stage files and hashes** (working tree at the time of the runs in §7/§8):

| file | sha256 |
| --- | --- |
| `scripts/ci/jscpd-changed.py` | `d0f285bb…` |
| `scripts/ci/test-jscpd-changed.py` | `d865e44d…` |
| `.jscpd.json` | `43fed1bf…` |

The suite grew from 23 to 32 tests (9 added, 4 rewritten to the base-scope contract:
the detection-mode and elixir-format tests, the dependency-tree test, and the
no-scannable-source test whose scenario now names a changed file that *can* hold a
clone).
