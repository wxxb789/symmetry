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
| `scripts/ci/model-routing-evaluation.py` | rewritten entry | 4,199 → 761 | `4a4cff24bd316ab4326b7fc4b062679e337b23cb5eb8022a809c4acf74478fc3` |
| `scripts/ci/model_routing_records.py` | new | 1,239 | `b8b56a274abf7edd4be307d08c448b9b698fbdba604b479dd75c936514a054ff` |
| `scripts/ci/model_routing_subject.py` | new | 539 | `e18dadb062d7590d80ba040db4e05f3e0173d876652097f6319c2613017edce8` |
| `scripts/ci/model_routing_gates.py` | new | 1,119 | `f84c93b79039e15cdae8f3b48a87bd44a4a196b1dcb763e4b1c3f223aa22d20f` |
| `scripts/ci/model_routing_receipts.py` | new | 699 | `37d8b351212ec44db7819405b39ca8196ea97cf1bd9edd5a6a6dd41d54f825eb` |
| `scripts/ci/test-model-routing-evaluation.py` | edited (repoints + one pin test) | 3,748 → 3,795 | `0281de88f830167e5c48cd35575dc8d7da62a0f346d7f0f8618a0dd15a64b7d6` |
| `scripts/ci/test-model-routing-omp-usage.py` | edited (constant read repoint) | 738 → 738 | `fc9163f591d1532108cdce7427361dae7385bbffa8f49058f4c56e2615839150` |
| `scripts/ci/model-routing-source-subject.py` | edited (`DELIVERED_TOOL_PATHS` + docstring) | 71 → 75 | `6312f154a2ed97e9111ba9e56a0c46802ec4817a569e5c8881ae7c58c1ad6648` |
| `scripts/ci/test-model-routing-source-subject.py` | edited (new pin test) | 130 → 147 | `845fa4215d9a427d4fc675d2b57d13d923a312c092b51c4797f3bc0a9530383f` |
| `docs/plans/2026-11-22-rf-w5-12-model-routing-split-plan.md` | new | 264 | `d97c19f5ace7a595edfa1e1c91dc7eb9def90dd24b7890115804e823462affcf` |
| `docs/evidence/rf-w5-12-model-routing-split-2026-11-22.md` | new | this record | — |

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
  headers, the `ENTRY_SCRIPT`/`SPLIT_SCRIPTS` constants (checked by content) and the authored
  comment lines the plan records.
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

Base accounting: 4,199 lines = 3,851 migrated unit lines + 29 dropped comment lines + 122
imports/docstring + 364 blank lines. Dropped comments (each recorded in the plan): the eight
section-divider pairs (comment policy 4), the `:148-152` tool-run-state note that itself defers to
the module docstring, and the `:411-418` host-path note (trimmed to four why-lines beside
`SANITIZE_SCRIPT`). No dropped line carries code — the checker proves that by requiring every
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

Result: `cmp` per case × stdout/stderr/exit code — all 21 files identical; also the two raw logs
(`cli-before/`, `cli-after/`, scratch) are byte-identical. No fixture directory is created (the
`prepare` refusal precedes `mkdir`).

## 5. Loader / pin diffs (contract Change (1)-(3))

`test-model-routing-evaluation.py`:

- one loader `defining_module(name)` (`load_runner_module()` then `importlib.import_module(name)`)
  returns the exact module object the entry imported — the object a module-global patch must hit;
- monkeypatch repoints: `records.SANITIZE_SCRIPT`/`records._HOST_PATH_PATTERNS` +
  `records.host_path_patterns()`; `gates.terminate_owned_process_tree` (+ cleanup),
  `gates.subprocess` (+ cleanup), `gates.run_owned_command`, `gates.PROCESS_TREE_REAP_SECONDS`;
- rule-read repoints: `records.number_sum`, `records.validate_gate` (three tests),
  `records.is_vitest_entry`, `records.requires_dependency_provisioning` (two sites);
  `gates.vitest_report_detail`/`gates.vitest_report_argv`;
- the AST walk repoints to `pathlib.Path(gates.__file__).read_text(...)` so the inventory's lines
  and the traced code object come from one file;
- both tool-materialization copy lists gain the four module file names (the materialized entry
  imports its siblings from its own directory);
- new test `test_receipt_binds_every_tooling_source` (contract change (5));
- reads that stay on the loaded runner: `Refused`, `canonical_json`, `dependency_tree_digest`,
  `first_difference`, `redact_host_paths`, `_dependency_tree` (six names; the resolution check
  asserts the set is exposed by the entry);
- no test renamed, none removed; every needle, count, subTest and fixture byte stays; the
  coverage pin `(17, 15, 2)` stays.

`test-model-routing-omp-usage.py`: `load_records_module()` helper; the seven `load_runner_module()`
validator sites (`module.validate_usage`, `module.Refused`) and
`test_shared_constants_match_the_runner` (now `records.STAGES`, `records.SECRET_KEY_PATTERN`) read
the defining module; the extractor's copy comment names `model_routing_records.py`.

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

| gate | command | exit | result |
| --- | --- | ---: | --- |
| verbatim/resolution check | `python3 <HOST-PATH>/.local/state/gdd/scratch/w5-12/check.py` | 0 | 215 units, 214 identical + 1 patched; docstring kept; 6/6 runner reads exposed (re-run on the frozen tree) |
| main suite (final tree) | `python3 scripts/ci/test-model-routing-evaluation.py` | 0 | `Ran 81 tests in 1367.393s` → `OK (skipped=2)`; printed `17 returns, 15 reached …, 2 unreached` (log `logs/suite-final.log`) |
| extractor suite | `python3 scripts/ci/test-model-routing-omp-usage.py` | 0 | `Ran 40 tests in 13.669s` → `OK` |
| source-subject suite | `python3 scripts/ci/test-model-routing-source-subject.py` | 0 | `Ran 8 tests in 0.236s` → `OK (skipped=1)` (execute-bit skip) |
| CLI byte-diff | `bash <HOST-PATH>/.local/state/gdd/scratch/w5-12/cli-byte-diff.sh` ×2 + `cmp` | 0 | 21/21 files identical (re-run on the frozen tree) |
| ruff check | `python3 -m ruff check scripts/ci` | 0 | `All checks passed!` (ruff 0.17.5, pip module) |
| ruff format | `python3 -m ruff format --check scripts/ci` | 0 | `63 files already formatted` |
| whitespace | `git diff --check` | 0 | clean |
| materialized copy | entry + siblings copied to a directory, `--help` | 0 | imports resolve from the copy |

## 8. Mutation checks (judging)

Frozen tree = the delivered revision in §1; every mutation was reverted and the four mutated files
re-verified with `sha256sum -c` against their pre-mutation digests (`all mutants restored`).

| # | mutation | command | exit | observed red |
| --- | --- | --- | ---: | --- |
| a | `model_routing_gates.py:212` `return "the derived vitest run timed out"` → `pass` | `python3 scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_vitest_report_refusal_coverage_is_derived` | 1 | printed `16 returns … 1 unreached`; `AssertionError: Lists differ` on the uncovered-shape list — the AST walk reads the moved module |
| b | `model_routing_records.py:39` `re.IGNORECASE` → `re.IGNORECASE \| re.MULTILINE` | `python3 scripts/ci/test-model-routing-omp-usage.py OmpUsageExtractionTest.test_shared_constants_match_the_runner` | 1 | `AssertionError: 34 != 42` (flags) at `test-model-routing-omp-usage.py:329` |
| c | `model-routing-source-subject.py` `DELIVERED_TOOL_PATHS` loses `scripts/ci/model_routing_subject.py` | `python3 scripts/ci/test-model-routing-source-subject.py SourceSubjectTests.test_delivered_tool_paths_cover_the_whole_evaluation_tool_set` | 1 | tuple diff names the missing element |
| d | `model_routing_receipts.py` `SPLIT_SCRIPTS` loses `"model_routing_receipts.py"` | `python3 scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_receipt_binds_every_tooling_source` | 1 | `Items in the second set but not the first: 'model_routing_receipts.py'` |

Raw logs: `<HOST-PATH>/.local/state/gdd/scratch/w5-12/logs/mutant-{a,b,c,d}.log`.

The first full suite run on the repointed tree surfaced exactly one failure — the sanitize-patch
test (repointed to the shared records module) left `SANITIZE_SCRIPT` pointing into a deleted
temporary directory and the later `test_measured_redaction_keeps_urls_literal` refused; the fix
registers `addCleanup` restores of `SANITIZE_SCRIPT` and `_HOST_PATH_PATTERNS` (the patch target is
the module the runner executes, so the test must put it back). Both tests pass together after the
fix (2.348 s). This is the only suite failure the split introduced; the run's other numbers
(81 tests, 2 skips, 1,266 s) come from that run. The final run on the fixed tree is the gate:
`Ran 81 tests in 1367.393s` → `OK (skipped=2)`, and it printed the identical refusal-coverage line
(`17 returns, 15 reached …, 2 unreached`, gates lines 212/241).

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
its raw logs; `logs/suite-main.log` is the main suite's verbatim output. All scratch paths live
outside the repository under `<HOST-PATH>/.local/state/gdd/scratch/w5-12/`.

Residual limits: the main suite is the slow oracle (budgeted > 580 s locally, ≈146 s in CI); the
CI step itself is unchanged and remains the conductor-verified oracle. The refusal-coverage
figures printed after the split carry the gates module's line numbers; the base figures (2015,
2044) stay historical as the plan records. The entry now imports its four sibling modules, so an
incomplete copy of the tool set (one that carries the entry but not the modules) fails at import
time instead of running — both materialization lists in the suite carry the full set, CI runs the
files in place, and the `tooling_sources` pin binds the whole set.
