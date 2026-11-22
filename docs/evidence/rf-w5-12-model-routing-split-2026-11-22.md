# rf-w5-12 model-routing-evaluation.py module split — evidence record (2026-11-22)

Subject: `scripts/ci/model-routing-evaluation.py` (4,199 L) became a thin CLI entry over four new
sibling modules — `model_routing_records.py`, `model_routing_subject.py`, `model_routing_gates.py`,
`model_routing_receipts.py` — with no rule change: every top-level unit moves byte-identically
(215 units: 214 identical, 1 with the recorded `tooling_sources` patch), the seven subcommands,
every refusal message and every exit code stay, and the two pinning suites are repointed to the
defining modules and stay green. Contract: `goal-rf-w5-12-model-routing-split.md` (GDD, 2026-11-21,
binding conductor decisions D1/D2, the module-naming decision, lesson 32 and the owner comment
policy of 2026-11-22). Base revision `b9eddc902bd21811b9162c05b25c7c27fcedb3da` (this worktree's
dispatch HEAD: `refactor(ci): one source-subject implementation with named variants (#238)`; W4-16
`#224`, W3-16's repairs and W1-11's ruff config are ancestors). Plan:
`docs/plans/2026-11-22-rf-w5-12-model-routing-split-plan.md`. Judging artifact (contract clause 2,
"answer.md"): this record plus the implement-stage report.

## 1. Deliverable shape

| file | status | lines | sha256 |
| --- | --- | ---: | --- |
| `scripts/ci/model-routing-evaluation.py` | rewritten entry | 4,199 → 798 | `ac5b24f72c678cae512a7306739be0263e9582904291df9e08a7b4adb6b870c4` |
| `scripts/ci/model_routing_records.py` | new | 1,236 | `f80a835ce3d7e04d944d684d2b658a137ca6ffe2da078cc27bc7c36658d72642` |
| `scripts/ci/model_routing_subject.py` | new | 539 | `e18dadb062d7590d80ba040db4e05f3e0173d876652097f6319c2613017edce8` |
| `scripts/ci/model_routing_gates.py` | new | 1,119 | `f84c93b79039e15cdae8f3b48a87bd44a4a196b1dcb763e4b1c3f223aa22d20f` |
| `scripts/ci/model_routing_receipts.py` | new | 699 | `37d8b351212ec44db7819405b39ca8196ea97cf1bd9edd5a6a6dd41d54f825eb` |
| `scripts/ci/test-model-routing-evaluation.py` | edited (repoints + the tooling pins) | 3,748 → 3,851 | `589837525d7bbca860770e48cff7ce438374a0b62fb1c0db40fd60566a1c1a7c` |
| `scripts/ci/test-model-routing-omp-usage.py` | edited (constant reads repointed to the defining module) | 1,286 → 1,283 | `f92854f1f84d76c6132aff0049874e869785dd6f27d573a3f44a572d9333648a` |
| `scripts/ci/model-routing-omp-usage.py` | edited (copy comment names `model_routing_records.py`) | 738 → 738 | `eb331a2138e7d06cea92d97ba3049bb3602a323c0ecadf83e62776ac6afe60f5` |
| `scripts/ci/model-routing-source-subject.py` | edited (`DELIVERED_TOOL_PATHS` + docstring) | 71 → 75 | `6312f154a2ed97e9111ba9e56a0c46802ec4817a569e5c8881ae7c58c1ad6648` |
| `scripts/ci/test-model-routing-source-subject.py` | edited (new pin test + closure arm) | 130 → 150 | `9ebb5ea3aad1a948e9307b88290c55501884f46d9021fc95152f5e4744add73f` |
| `scripts/ci/citest.py` | edited (P3 closeout: the shared `runner_module_closure`) | 120 → 139 | `67bd8c3edd26c3c7a24c0def22d5506744a7a5a69efc9c4a6c8922a924ffb879` |
| `docs/evaluations/model-routing/README.md` | edited (P3 closeout: post-split re-verification note) | 369 → 369 | `c975fead5d8919dc5d3c475d54b9c334a852410fec6481daefa11239be32f2a5` |
| `docs/plans/2026-11-22-rf-w5-12-model-routing-split-plan.md` | new | 357 | `50acb748215c3e54c14fa0e9dc09651e17254654c8d741d2725d7b509de4cb2f` |
| `docs/evidence/rf-w5-12-model-routing-split-2026-11-22.md` | new | this record | — |

Counts and digests are measured on the delivered revision after the accepted simplify findings were
applied (S1-S3): `model_routing_records.py`, the two suites and the plan carry their
post-simplification figures, and `model-routing-omp-usage.py` gains the row the earlier table lost.
The rows Review fix r1 (§10) changed carry their post-fix figures (the entry, the evaluation suite,
the source-subject suite, the plan); every other row is unchanged by the fix. The rows the P3
closeout (§11) changed carry their post-closeout figures (the entry, the evaluation suite, the
extractor suite, the source-subject suite, `citest.py`, the README, the plan).

Untouched: `.github/workflows/**` (the "Check auditable model-routing workflow" step at
`.github/workflows/ci.yml:1078-1090` still runs the same six suite paths),
`model-routing-record-sanitize.py` and its three suites, `check-record-cleanliness.py`,
`test-check-record-cleanliness.py`, `pyproject.toml`, `source_subject.py`,
`linux-v2-source-subject.py`, `assert-go-test-json.py`. No new suppression of any kind.

## 2. Module map (def groups → module) and the machine check

Produced by `split.py` (scratch, below) from an explicit unit → module table; verified independently
by `check.py`, which re-derives every unit from the git-bound base revision and the delivered files:

- `base top-level units: 215`; `delivered units: identical 214, patched 1` (per-module:
  records 111, gates 50, subject 27, receipts 15 + 1 patched, entry 11).
- every base top-level unit is delivered exactly once; nothing added except the four import
  headers, the `ENTRY_SCRIPT`/`SPLIT_SCRIPTS` constants (checked by content), the authored
  comment lines the plan records, and — after Review fix r1 (§10) — the entry's startup statements
  (the `sys.path` insert, the binding guard and the cache check).
- every global name referenced by a delivered module resolves (own defs/constants, its imports,
  stdlib, `__file__`, builtins).
- the entry's module docstring is byte-identical to base; the `prog` string and the seven
  subcommands are inside the byte-identical `main` unit.

The one in-unit patch is contract change (5): in `build_receipt`'s `tooling_sources` tuple,

```
-                    pathlib.Path(__file__).resolve(),
+                    ENTRY_SCRIPT,
+                    *SPLIT_SCRIPTS,
```

with `ENTRY_SCRIPT = pathlib.Path(__file__).with_name("model-routing-evaluation.py").resolve()`
and `SPLIT_SCRIPTS` naming the four modules (receipts module). Everything else in `build_receipt`
is byte-identical. The unit table with base/delivered sha256 per unit is
`<HOST-PATH>/.local/state/gdd/scratch/w5-12/units.tsv` (215 rows, one `patched`).

Base accounting, as split.py prints it: 4,199 lines = 3,851 lines inside attributed spans (3,722
unit-span lines + 23 kept-comment lines + the 106-line raw header/trailer) + 29 comment lines in
dropped or replaced ranges + 122 docstring/import lines + 364 whitespace-only lines. The four
classes are a cover, not a partition: they overlap by 167 lines (103 module-docstring lines are in
both the raw header range and the docstring/import class, and 64 whitespace-only lines lie inside
unit spans with 5 more inside the docstring), and split.py fails the run only when a line is in no
class. Dropped comments (each recorded in the plan): the eight section-divider pairs (comment
policy 4), the `:148-152` tool-run-state note that itself defers to the module docstring, and the
`:411-418` host-path note, whose eight lines are replaced by the one-line why beside
`SANITIZE_SCRIPT`. No dropped line carries code — the checker proves that by requiring every
non-comment base line to be inside a migrated unit.

## 3. Refusal inventory before/after (the machine-checkable pin)

`test_vitest_report_refusal_coverage_is_derived` prints the recomputable figure. Before the split
(base revision, single-method run, 1.524 s):

```
vitest_report_detail refusal shapes: 17 returns, 15 reached by the suite's malformed-report cases, 2 unreached: "the derived vitest run timed out" (line 2015), "the derived vitest report carries a malformed assertion" (line 2044)
```

After the split the AST walk reads `model_routing_gates.py` (the file whose code object the same
test traces), the counted set is identical, and the printed line numbers are the historical
figures the records quote:

```
vitest_report_detail refusal shapes: 17 returns, 15 reached by the suite's malformed-report cases, 2 unreached: "the derived vitest run timed out" (line 212), "the derived vitest report carries a malformed assertion" (line 241)
```

The set is identical (`17 returns`, the same two unreached shapes); the pin
`(len(inventory), len(reached), len(uncovered)) == (17, 15, 2)` and the uncovered-shape list are
unchanged in the suite.

## 4. CLI byte-diff

Harness `<HOST-PATH>/.local/state/gdd/scratch/w5-12/cli-byte-diff.sh` (one script, one fixture
directory, run before and after the split; seven invocations that refuse without side effects,
`cwd` = `<HOST-PATH>/.local/state/gdd/scratch/w5-12/fixture`, relative arguments so no host path
enters the bytes):

| case | invocation | exit |
| --- | --- | ---: |
| 1 | `--help` | 0 |
| 2 | `zzz-nonsense` (argparse) | 2 |
| 3 | `accept --run-dir run-missing` | 1 |
| 4 | `prepare --run-dir run-prepare --task missing-task.json --routes missing-routes.json --source missing-source` | 1 |
| 5 | `seal --run-dir run-missing` | 1 |
| 6 | `check --run-dir run-missing` | 1 |
| 7 | `receipt --run-dir run-missing --usage missing-usage.json` | 1 |

Result: `cmp` per case × stdout/stderr/exit code — all 21 files identical before, after and on the
apply revision (`cli-apply/`); the raw logs (`cli-before/`, `cli-after/`, `cli-apply/`, scratch) are
byte-identical too. No fixture directory is created (the `prepare` refusal precedes `mkdir`).

## 5. Loader / pin diffs (contract Change (1)-(3))

`test-model-routing-evaluation.py`:

- the defining module is imported directly with `importlib.import_module(name)` — the module object
  the entry itself binds (the same `sys.modules` instance), the object a module-global patch must
  hit; it does not load the entry first (at r1 this was the one-line `defining_module` helper; the
  P3 closeout inlined it);
- monkeypatch repoints: `records.SANITIZE_SCRIPT`/`records._HOST_PATH_PATTERNS` +
  `records.host_path_patterns()`; `gates.terminate_owned_process_tree` (+ cleanup),
  `gates.subprocess` (+ cleanup), `gates.run_owned_command`, `gates.PROCESS_TREE_REAP_SECONDS`;
- rule-read repoints: `records.number_sum`, `records.validate_gate` (three tests),
  `records.is_vitest_entry`, `records.requires_dependency_provisioning` (two sites);
  `gates.vitest_report_detail`/`gates.vitest_report_argv`;
- the AST walk repoints to `pathlib.Path(gates.__file__).read_text(...)` so the inventory's lines
  and the traced code object come from one file;
- one module-level `MATERIALIZED_TOOL_NAMES` tuple gains the four module file names and feeds both
  tool-materialization loops (the materialized entry imports its siblings from its own directory);
- new test `test_receipt_binds_every_tooling_source` (contract change (5));
- reads that stay on the loaded runner: `Refused`, `canonical_json`, `dependency_tree_digest`,
  `first_difference`, `redact_host_paths`, `_dependency_tree` (six names; the resolution check
  asserts the set is exposed by the entry);
- no test renamed, none removed; every needle, count, subTest and fixture byte stays; the
  coverage pin `(17, 15, 2)` stays.

`test-model-routing-omp-usage.py`: the six records sites (`module.validate_usage`,
`module.Refused`, and `test_shared_constants_match_the_runner` reading
`records.STAGES`/`records.SECRET_KEY_PATTERN`) call `importlib.import_module("model_routing_records")`
directly (the P3 closeout inlined the r1 `load_records_module` helper), `load_usage_module()` stays
for the extractor, and the suite no longer loads the entry at all; the extractor's copy comment
names `model_routing_records.py`.

## 6. Binding extensions (contract changes (4) and (5))

- `DELIVERED_TOOL_PATHS` (model-routing-source-subject.py) is now the entry, the four modules and
  the suite (six paths); the docstring line count follows. The tool now prints one `delivered` line
  per path — observed six lines with the entry digest first. New pin test
  `test_delivered_tool_paths_cover_the_whole_evaluation_tool_set` asserts the exact tuple and that
  every path exists.
- `tooling_sources` is now eight keys: `model-routing-evaluation.py`, the four modules,
  `assert-go-test-json.py`, `linux-v2-source-subject.py`, `model-routing-record-sanitize.py`.
  Receipts derived before the split stay historical (the `accept` boundary refuses them, as the
  evaluations README's tooling-source-growth paragraph documents for earlier growths); the
  paragraph itself is now stale by this authorized change and is named in the plan and the report
  as a follow-up outside the owned set.

## 7. Gates

All rows were re-run on the apply-stage revision (the accepted simplify findings applied); the
implement-stage figures are superseded, and the log names below are the apply-stage runs.

| gate | command | exit | result |
| --- | --- | ---: | --- |
| verbatim/resolution check | `python3 <HOST-PATH>/.local/state/gdd/scratch/w5-12/check.py` | 0 | 215 units, 214 identical + 1 patched; docstring kept; 6/6 runner reads exposed (log `logs/check-apply.log`) |
| main suite | `python3 -u scripts/ci/test-model-routing-evaluation.py -v` | 0 | `Ran 81 tests in 3023.559s` → `OK (skipped=2)`, zero `FAIL:`/`ERROR:` lines; printed `17 returns, 15 reached …, 2 unreached` (gates lines 212/241; log `logs/suite-apply.log`) |
| extractor suite | `python3 scripts/ci/test-model-routing-omp-usage.py` | 0 | `Ran 40 tests in 25.499s` → `OK` |
| source-subject suite | `python3 scripts/ci/test-model-routing-source-subject.py` | 0 | `Ran 8 tests in 0.283s` → `OK (skipped=1)` (execute-bit skip) |
| CLI byte-diff | `bash <HOST-PATH>/.local/state/gdd/scratch/w5-12/cli-byte-diff.sh <HOST-PATH>/…/cli-apply` + `diff -r cli-before cli-apply` | 0 | 21/21 files identical, exit codes 0/2/1/1/1/1/1 (`cli-apply/`) |
| ruff check | `python3 -m ruff check scripts/ci` | 0 | `All checks passed!` (ruff 0.17.5, pip module) |
| ruff format | `python3 -m ruff format --check scripts/ci` | 0 | `63 files already formatted` |
| whitespace | `git diff --check` | 0 | clean |
| materialized copy | entry + siblings copied to a directory, `--help` | 0 | imports resolve from the copy, usage printed (log `logs/copysmoke-apply.log`) |

The apply-stage main suite ran at roughly twice the implement-stage ambient load (extractor suite
13.7 s → 25.5 s; main suite 1,367.4 s → 3,023.6 s on a host running other agents' workloads). A
first apply-stage attempt was killed at its 3,300 s deadline after 75 tests with one `F` at
`test_timed_out_gate_terminates_its_descendants`; that fixture is the load-sensitive one
`docs/archive/CLOSED-TODO.md:1343` records, and the verbose re-run above passed all 81 tests.

## 8. Mutation checks (judging)

Frozen tree = the delivered revision in §1; every mutation was reverted and each mutated file
re-hashed against its pre-mutation digest (`all mutants restored`). The four mutations were re-run
on the apply-stage revision and reproduced the same four reds; raw logs
`<HOST-PATH>/.local/state/gdd/scratch/w5-12/logs/mutant-{a,b,c,d}-apply.log` (implement stage:
`logs/mutant-{a,b,c,d}.log`).

| # | mutation | command | exit | observed red |
| --- | --- | --- | ---: | --- |
| a | `model_routing_gates.py:212` `return "the derived vitest run timed out"` → `pass` | `python3 scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_vitest_report_refusal_coverage_is_derived` | 1 | printed `16 returns … 1 unreached`; `AssertionError: Lists differ` on the uncovered-shape list — the AST walk reads the moved module |
| b | `model_routing_records.py:39` `re.IGNORECASE` → `re.IGNORECASE \| re.MULTILINE` | `python3 scripts/ci/test-model-routing-omp-usage.py OmpUsageExtractionTest.test_shared_constants_match_the_runner` | 1 | `AssertionError: 34 != 42` (flags) at `test-model-routing-omp-usage.py:323` |
| c | `model-routing-source-subject.py` `DELIVERED_TOOL_PATHS` loses `scripts/ci/model_routing_subject.py` | `python3 scripts/ci/test-model-routing-source-subject.py SourceSubjectTests.test_delivered_tool_paths_cover_the_whole_evaluation_tool_set` | 1 | tuple diff names the missing element |
| d | `model_routing_receipts.py` `SPLIT_SCRIPTS` loses `"model_routing_receipts.py"` | `python3 scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_receipt_binds_every_tooling_source` | 1 | `Items in the second set but not the first: 'model_routing_receipts.py'` |

The first full suite run on the repointed tree surfaced exactly one failure — the sanitize-patch
test (repointed to the shared records module) left `SANITIZE_SCRIPT` pointing into a deleted
temporary directory and the later `test_measured_redaction_keeps_urls_literal` refused; the fix
registers `addCleanup` restores of `SANITIZE_SCRIPT` and `_HOST_PATH_PATTERNS` (the patch target is
the module the runner executes, so the test must put it back). Both tests pass together after the
fix (2.348 s). This is the only suite failure the split introduced; the run's other numbers
(81 tests, 2 skips, 1,266 s) come from that run. The final run on the fixed tree was the
implement-stage gate: `Ran 81 tests in 1367.393s` → `OK (skipped=2)`, and it printed the identical
refusal-coverage line (`17 returns, 15 reached …, 2 unreached`, gates lines 212/241); the
apply-stage run in §7 is the current gate.

## 9. Method, tools and raw outputs

Bulk verbatim extraction was produced by a scratch generator, not by hand-typing 3,851 lines:
`<HOST-PATH>/.local/state/gdd/scratch/w5-12/split.py` reads the base file through
`git show HEAD:…` (so a rerun can never read
its own output), slices every top-level unit by its AST span from an explicit unit → module table,
computes each module's sibling/stdlib imports from real name usage, and prints the base-line
accounting. `<HOST-PATH>/.local/state/gdd/scratch/w5-12/check.py` is an independent verifier (it
derives its expectations from
the git-bound base and the delivered files, not from the generator's table). `units.tsv` carries
the per-unit digests. `cli-before/`, `cli-after/`, `cli-byte-diff.sh` are the byte-diff harness and
its raw logs; `logs/suite-main.log` is the implement-stage main suite's verbatim output and
`logs/suite-apply.log` the apply-stage one (`cli-apply/` is the apply-stage byte-diff output). All
scratch paths live outside the repository under `<HOST-PATH>/.local/state/gdd/scratch/w5-12/`.

Residual limits: the main suite is the slow oracle (budgeted > 580 s locally, ≈146 s in CI); the
CI step itself is unchanged and remains the conductor-verified oracle. The refusal-coverage
figures printed after the split carry the gates module's line numbers; the base figures (2015,
2044) stay historical as the plan records. The entry now imports its four sibling modules, so an
incomplete copy of the tool set (one that carries the entry but not the modules) fails at import
time instead of running — both materialization lists in the suite carry the full set, CI runs the
files in place, and the `tooling_sources` pin binds the whole set. After Review fix r1 (§10), such
an incomplete copy refuses with the tool's stderr shape whenever an ambient module path answers for
the missing siblings, and a bytecode cache that no longer matches its source refuses before any
command runs.

## 10. Review fix r1 (2026-11-22)

The code review of `7f34d40d..8c295b5f` returned `MUST_FIX (4)` (F1-F4; F5/F6 advisory) with four
reproduced findings. All six are fixed inside the owned set — the plan's "Review fix r1" section
names each change. Files changed vs the reviewed revision:
`scripts/ci/model-routing-evaluation.py` (F1/F2: the `sys.path` insert, the `tooling_binding_problem`
guard, `executed_matches_source`), `scripts/ci/test-model-routing-evaluation.py` (three regression
pins, the receipt pin's closure arm, the F6 comment removed),
`scripts/ci/test-model-routing-source-subject.py` (closure arm), the plan (F3 + the fix record) and
this record (F4 + the re-measured §1 rows). `model_routing_records.py`,
`model_routing_subject.py`, `model_routing_gates.py` and `model_routing_receipts.py` are
byte-identical to the reviewed revision (their §1 digests assert it). No workflow or `pyproject.toml`
edit at r1 (the P3 closeout §11 moves the shared closure helper into `citest.py`); no new
suppression.

Gates re-run in the foreground on the fix revision (host interpreter `python` 3.12.10; the
byte-diff harness invokes `python3`, the same build):

| gate | command | exit | result |
| --- | --- | ---: | --- |
| new/updated pins | `python scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_receipt_binds_every_tooling_source EvaluationTests.test_an_ambient_module_path_cannot_rebind_a_tool_copy EvaluationTests.test_an_incomplete_tool_copy_refuses_before_evaluating EvaluationTests.test_a_stale_bytecode_cache_cannot_supply_the_executed_rules` | 0 | `Ran 4 tests in 45.800s` → `OK` |
| regression pins | `python scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_tooling_revision_change_invalidates_an_earlier_receipt EvaluationTests.test_every_broken_sanitize_sibling_refuses_instead_of_raising EvaluationTests.test_measured_redaction_keeps_urls_literal EvaluationTests.test_redacted_values_bind_the_sanitize_tool_that_rendered_them EvaluationTests.test_positive_flow_accepts_exact_derived_receipt EvaluationTests.test_vitest_report_refusal_coverage_is_derived` | 0 | `Ran 6 tests in 167.121s` → `OK`; printed `17 returns, 15 reached …, 2 unreached` (gates lines 212/241) |
| source-subject suite | `python scripts/ci/test-model-routing-source-subject.py` | 0 | `Ran 8 tests in 1.490s` → `OK (skipped=1)` |
| extractor suite | `python scripts/ci/test-model-routing-omp-usage.py` | 0 | `Ran 40 tests in 49.984s` → `OK` |
| isolated-mode start | `python -I scripts/ci/model-routing-evaluation.py --help` | 0 | usage printed (reviewed revision: exit 1, `ModuleNotFoundError` — F1's reproduction) |
| mixed-set refusal | entry-only copy in a temporary directory, `PYTHONSAFEPATH=1 PYTHONPATH=<repo>/scripts/ci`, `--help` | 1 | `model-routing-evaluation refused: the sibling modules did not resolve beside the entry: <repo>\\scripts\\ci` (the guard; no command ran) |
| stale-cache refusal | materialized copy, `import model_routing_receipts` to build `__pycache__`, same-size `"run record"` → `"run ledger"` edit with the mtime restored (`st_mtime_ns`), then `receipt --run-dir missing-run --usage missing.json` | 1 | `model-routing-evaluation refused: model_routing_receipts.py was executed from bytecode that does not match its source`; the restored copy prints the pinned `cannot read run record at missing-run\\run.json` again |
| CLI byte-diff | `bash <HOST-PATH>/.local/state/gdd/scratch/w5-12/cli-byte-diff.sh …/cli-fix` + `diff -r cli-before cli-fix` + `diff -r cli-apply cli-fix` | 0 | 21/21 files identical against both earlier runs; exit codes 0/2/1/1/1/1/1 |
| ruff check | `python -m ruff check scripts/ci` | 0 | `All checks passed!` |
| ruff format | `python -m ruff format --check scripts/ci` | 0 | `63 files already formatted` |
| whitespace | `git diff --check` and `git diff --check origin/main` | 0 | clean |
| host path / DLP | the brief's user-profile path scan and the DLP filter sequence scan over `git diff origin/main` | no match | no user-profile path, no DLP filter sequence, no profile name in the whole diff |

Mutation checks (fix revision; every mutated file restored and its digest re-checked with
`sha256sum -c` against the pre-mutation digests — all `OK`):

| # | mutation | command | exit | observed red |
| --- | --- | --- | ---: | --- |
| a | `model_routing_gates.py:212` `return "the derived vitest run timed out"` → `pass` | `… test_vitest_report_refusal_coverage_is_derived` | 1 | `FAILED (failures=1)` (1.665 s) |
| b | `model_routing_records.py:39` `re.IGNORECASE` → `re.IGNORECASE \| re.MULTILINE` | `python scripts/ci/test-model-routing-omp-usage.py OmpUsageExtractionTest.test_shared_constants_match_the_runner` | 1 | `FAILED (failures=1)` (0.138 s) |
| c | `model-routing-source-subject.py` `DELIVERED_TOOL_PATHS` loses `scripts/ci/model_routing_subject.py` | `… test_delivered_tool_paths_cover_the_whole_evaluation_tool_set` | 1 | `FAILED (failures=1)`; the assertion diff names the missing element |
| d | `model_routing_receipts.py` `SPLIT_SCRIPTS` loses `"model_routing_receipts.py"` | `… test_receipt_binds_every_tooling_source` | 1 | `FAILED (failures=1)`; the assertion diff shows `'model_routing_receipts.py'` |
| e | the entry's `sys.path.insert` line deleted | `… test_an_ambient_module_path_cannot_rebind_a_tool_copy` | 1 | `FAILED (errors=1)`; the pristine copy run was refused by the guard instead of accepting (`JSONDecodeError` on the non-JSON stdout) |
| f | the entry's `_binding_problem` guard call deleted | `… test_an_incomplete_tool_copy_refuses_before_evaluating` | 1 | `FAILED (failures=1)`; the entry-only copy printed `--help` (exit 0) instead of refusing |
| g | the entry's cache-check loop deleted | `… test_a_stale_bytecode_cache_cannot_supply_the_executed_rules` | 1 | `FAILED (failures=1)`; `AssertionError: 'model_routing_receipts.py was executed from bytecode' not found in "model-routing-evaluation refused: cannot read run record at missing-run\\run.json…"` — the stale cache served the rules (F2's reproduction) |

The fix-revision gate numbers were taken before the mutation runs; every mutated file was restored
byte-identically afterwards (`sha256sum -c` shows all five files `OK`, and `git status` shows only
the five intended modifications), so the green rows above describe the same bytes as this record.

Not re-run here: the full main suite — the conductor-owned phase-5 gate (the apply-stage run took
3,023.6 s; the fix brief excludes conductor-owned full suites). The suite's test count moves
81 → 84 (three new pins), verified with `grep -c "    def test_"` on the delivered bytes and by the
`Ran 4`/`Ran 6` targeted runs above; both targeted runs cover every test the fix touches, and the
byte-diff plus the two other suites ran complete.

## 11. P3 closeout (2026-11-22)

The Opus review of `4f75e937` (findings file outside the repository: four P3 findings) is closed.
Files changed in this closeout beyond §10: `scripts/ci/model-routing-evaluation.py` (P3-1), the
three suite files (P3-2), `scripts/ci/citest.py` (P3-2, the shared closure helper; added to the §1
table), `docs/evaluations/model-routing/README.md` (P3-4), the plan (P3-3, the D1 amendment) and
this record. The four `model_routing_*.py` modules keep their bytes: the receipts module was
mutated for P3-1 and restored byte-identically (`sha256sum -c` `OK`, the file's `git diff` empty).

- **P3-1 (one source for the tool set).** `_TOOL_MODULES = tuple(path.stem for path in
  SPLIT_SCRIPTS)` replaces the hand-written four-name copy, so the guard's stale-bytecode list and
  the receipt's `tooling_sources` set derive from the same `SPLIT_SCRIPTS`. Mutation: remove
  `"model_routing_subject.py"` from `SPLIT_SCRIPTS` →
  `python scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_receipt_binds_every_tooling_source`
  → `FAILED (failures=1)`, `AssertionError: Items in the second set but not the first:
  'model_routing_subject.py'`; the baseline for the same command is `Ran 1 test in 28.012s` with
  `OK`.
- **P3-2 (test hunks).** The four name-restating docstrings are gone; `defining_module` and
  `load_records_module` are inlined as `importlib.import_module(...)` at their call sites; both
  suites call `citest.runner_module_closure(script)`, one copy in `citest.py`.
- **P3-3 (records).** The plan's "Conductor decisions (D1/D2) and amendments" section records the
  r1 startup guard — the own-directory `sys.path` insertion and the three refusals (siblings not
  beside the entry / entry not the receipt-bound file / bytecode not matching source) — as a
  documented amendment to D1. The entry is 798 lines (797 at r1 plus the `SPLIT_SCRIPTS` import
  line) and the evaluation suite stands at 84 tests (`grep -c "    def test_"`); the PR body
  replacement text is written outside the repository, to the review findings directory.
- **P3-4 (README).** "Receipt verification and the bound tooling revision" now states that a
  post-split revision needs the entry plus the four sibling `model_routing_*.py` modules from the
  same revision (`git archive <revision> scripts/ci | tar -x` into a temporary directory), not
  `git show` of the entry alone.

Gate runs on the closeout revision (from the worktree root; host interpreter `python` 3.12.10):

| gate | command | exit | result |
| --- | --- | ---: | --- |
| targeted pin, baseline | `python scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_receipt_binds_every_tooling_source` | 0 | `Ran 1 test in 28.012s` → `OK` |
| targeted pin, mutant | the same command with `SPLIT_SCRIPTS` minus `"model_routing_subject.py"` | 1 | `Ran 1 test in 35.588s` → `FAILED (failures=1)`; the assertion diff names `model_routing_subject.py` (restored after the run) |
| CLI byte-diff | `bash <scratch>/cli-byte-diff.sh <scratch>/cli-closeout`, then `diff -r cli-before cli-closeout` and `diff -r cli-fix cli-closeout` | 0 | 21/21 files identical against both the base-revision and the r1 runs (case exit codes 0/2/1/1/1/1/1) |
| isolated-mode start | `python -I scripts/ci/model-routing-evaluation.py --help` | 0 | usage printed |
| ruff check | `python -m ruff check scripts/ci` | 0 | `All checks passed!` |
| ruff format | `python -m ruff format --check scripts/ci` | 0 | `63 files already formatted` |
| main suite | `python scripts/ci/test-model-routing-evaluation.py` | 0 | `Ran 84 tests in 2172.133s` → `OK (skipped=2)`; printed `17 returns, 15 reached …, 2 unreached` (gates lines 212/241) |
| extractor suite | `python scripts/ci/test-model-routing-omp-usage.py` | 0 | `Ran 40 tests in 14.883s` → `OK` |
| source-subject suite | `python scripts/ci/test-model-routing-source-subject.py` | 0 | `Ran 8 tests in 0.311s` → `OK (skipped=1)` |
| literal acceptance discovery | `python -m unittest discover -s scripts/ci -p "test_model_routing*.py"` | 5 | `Ran 0 tests` → `NO TESTS RAN`: the pattern matches no file (every suite filename uses dashes); the acceptance fallback (the evidence-named suites) is the three suite rows in this table |
| whitespace | `git diff --check` | 0 | clean |
