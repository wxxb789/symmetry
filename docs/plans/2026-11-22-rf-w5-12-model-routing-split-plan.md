# rf-w5-12 plan — model-routing-evaluation.py module split, no rule change (SC S10)

Contract: `goal-rf-w5-12-model-routing-split.md` (GDD refactor wave 5, 2026-11-21, with the binding
conductor decisions D1/D2, the module-naming decision, lesson 32 and the owner comment policy of
2026-11-22). Base revision: this worktree's HEAD `b9eddc902bd21811b9162c05b25c7c27fcedb3da`
(`refactor(ci): one source-subject implementation with named variants (#238)`; W4-16 `#224`,
W3-16's `scripts/ci` repairs, W5-11 `#238` and W1-11's ruff configuration are ancestors), branch
`refactor/rf-w5-12-model-routing-split`, working tree clean at dispatch. All `<file>:<line>`
citations below resolve at the base revision.

## Re-derivation at the dispatch head (the contract asks for exactly this)

| Contract claim | Re-derived at `b9eddc90` |
| --- | --- |
| `model-routing-evaluation.py` = 4,199 L / 200,035 B | exact (unchanged since the draft; `wc -l -c`) |
| `Refused` `:407`, `strict_json` `:462`, `PathMatcher` `:756`, `execute_gate` `:2232`, `recompute_gate` `:2403`, `build_receipt` `:3064`, `derive_receipt` `:3302`, `_tree_entries` `:3486`, `dependency_tree_digest` `:3522`, `main` `:4127` | every one exact at head |
| test loader `:21` / `:24-30` (`spec_from_file_location` at `:27`) | exact (`SCRIPT` is `:22` at head: the file gained a shebang-adjacent docstring line; the loader `load_runner_module` is `:25-28` with the call at `:27`) |
| `refusal_returns` `:33-59`, called at `:2974` for `vitest_report_detail` | `refusal_returns` is `:35-61`; the call is `:2933` inside `test_vitest_report_refusal_coverage_is_derived` (`:2919`) |
| `test-model-routing-omp-usage.py` pins `SECRET_KEY_PATTERN` `:327-328` and `STAGES` `:325-326` | exact (`test_shared_constants_match_the_runner`, `:309`) |
| `DELIVERED_TOOL_PATHS` `:66-67` | `model-routing-source-subject.py:66-69` (W5-11 reformatted the tuple) |
| receipt `tooling_sources` `:3283-3291` binds `__file__`, `VALIDATOR_SCRIPT`, `SOURCE_SUBJECT_SCRIPT`, `SANITIZE_SCRIPT` | `:3284-3296`, four keys exactly as claimed |
| test file 3,795 L | 3,748 L / 191,313 B at head |
| CI step "Check auditable model-routing workflow", `ci.yml:1494-1499` | `.github/workflows/ci.yml:1078-1090`: the named step runs six suites by path (`test-model-routing-evaluation.py`, `test-model-routing-omp-usage.py`, `test-model-routing-record-sanitize.py`, `test-model-routing-record-sanitize-json.py`, `test-check-record-cleanliness.py`, `test-model-routing-source-subject.py`); unchanged by this slice |
| "the plan row lists only `test imports`" | the real coupling is four-way, not three: the two by-path loaders, the `refusal_returns` AST walk, the two module-global monkeypatch targets (`SANITIZE_SCRIPT`; `_HOST_PATH_PATTERNS`; `subprocess`; `terminate_owned_process_tree`), and the materialized-tool copies of two tests (`:1075-1085`, `:3700-3712`) that must now carry the new modules or the copies cannot import |
| "the refusal-coverage test prints line numbers that historical records quote" | confirmed: base run prints `17 returns, 15 reached …, 2 unreached: "the derived vitest run timed out" (line 2015), "the derived vitest report carries a malformed assertion" (line 2044)`; after the move the *counts* are the pin, the line numbers stay historical |

## Conductor decisions (D1/D2) and amendments

- **D1 (binding).** The `DELIVERED_TOOL_PATHS` extension (change (4)) and the `tooling_sources`
  extension (change (5)) are the sole authorized output-shape changes.
- **D2 (binding).** Module sizes are report-only; the gates are the module map, the refusal
  inventory, the CLI byte-diff and the two binding pins.

**Amendment to D1 (review fix r1, 2026-11-22).** The entry's startup tool-set binding is
authorized in addition: the own-directory `sys.path` insertion resolves the four sibling modules
beside the executing entry, and the guard refuses before argparse (tool stderr shape, exit 1) with
one of three messages — `the sibling modules did not resolve beside the entry: …`, `the entry is …
but the receipt binds …`, `<module> was executed from bytecode that does not match its source`.
Each fires only in a state the single-file base could not be in (a tool copy missing its siblings,
a renamed entry, a bytecode cache that no longer matches its source); the recorded 21-file CLI
byte-diff (evidence §4, §10, §11) is byte-identical, so no rule, refusal or output of the seven
subcommands changes. Details: "Review fix r1" below; behaviour evidence: the record's §10.

## Deliverable shape

| file | state | content |
| --- | --- | --- |
| `scripts/ci/model_routing_records.py` | new | the strict-typing and validation layer: `Refused`, the strict JSON/scalar/IO helpers, the `require_*` family, the declared schema constants and key sets, `PathMatcher`/test-path rules, the record `validate_*` family, the redaction/sanitize binding, the usage-row validators |
| `scripts/ci/model_routing_subject.py` | new | the git binding and every tree digest: worktree walk, base-tree object reads (`read_base_blob_digests`' `ls-tree` + `cat-file --batch` reader), index/object-substitution and filter-machinery refusals, `compute_subject`/`collect_changes`/`validate_changes`, `current_subject`/`require_current_subject`, `load_source_subject`, `SUBJECT_NAMESPACE`, `SOURCE_SUBJECT_SCRIPT` |
| `scripts/ci/model_routing_gates.py` | new | gate execution and stored evidence: the go/exunit/vitest derivations, `validator_argv`/`run_validator`, `terminate_owned_process_tree`/`run_owned_command`/`execute_gate`/`recompute_gate`, the dependency-tree walk and digest (`_tree_entries`/`dependency_tree_digest`/`verify_dependency_tree`), the resolved-tree checks, the process/trace constants |
| `scripts/ci/model_routing_receipts.py` | new | the run-record and receipt layer: `load_run`/`save_run`/`load_frozen_*`/`load_sealed`, attempt reading and verification, `provisioning_view`/`require_dependency_provisioning`, `build_receipt`/`derive_receipt`, `INCOMPLETE_CHECK_DETAIL`, the bound-tooling path constant |
| `scripts/ci/model-routing-evaluation.py` | edited | thin CLI entry: same path, same `prog="model-routing-evaluation.py"`, same seven subcommands, the module docstring unchanged, the `command_*` handlers and `finish_prepare`/`environment_record`/`first_difference`/`main` stay, plus imports of the four modules it uses (the suites read the defining modules — below) and the review-fix startup statements (own-directory `sys.path` insert, tool-set binding guard, bytecode-cache check — see "Review fix r1" below) |
| `scripts/ci/test-model-routing-evaluation.py` | edited | loader/AST-walk updates only (the four monkeypatch sites and the rule-read sites — `records.number_sum`/`validate_gate`/`is_vitest_entry`/`requires_dependency_provisioning`, `gates.vitest_report_detail`/`vitest_report_argv`/`PROCESS_TREE_REAP_SECONDS` — repointed to the defining modules, the AST walk with them; one `MATERIALIZED_TOOL_NAMES` tuple feeds both tool-materialization loops) plus the `tooling_sources` key-set pin of change (5) |
| `scripts/ci/model-routing-source-subject.py` | edited | `DELIVERED_TOOL_PATHS` extends to the full delivered set and its docstring sentence names it; W5-11 leaves this tuple untouched, so no rebase conflict |
| `scripts/ci/test-model-routing-source-subject.py` | edited | the new `DELIVERED_TOOL_PATHS` pin test (decision E) |
| `scripts/ci/model-routing-omp-usage.py` | edited | the `SECRET_KEY_PATTERN` copy comment names the new defining module (one comment line) |
| `docs/plans/2026-11-22-rf-w5-12-model-routing-split-plan.md` | new | this plan |
| `docs/evidence/rf-w5-12-model-routing-split-2026-11-22.md` | new | the evidence record (contract clause 2's judging artifact) |

No workflow, no `pyproject.toml`, no sanitizer, no `test-check-record-cleanliness.py` edit; no
`# noqa`/`//nolint`; no test renamed, added (beyond the two authorized pins and the review-fix
pins of "Review fix r1"), removed or retagged.

## Module map (def groups → module)

Every top-level unit of the base file moves to exactly one module; the evidence record carries the
machine-checked per-unit digest table (base text == delivered text per unit). Base order is kept
inside each module; the only deliberate re-ordering is `verify_dependency_tree` (`:1589`, which
reads `dependency_tree_digest`) placed directly after `dependency_tree_digest` in the gates module.

| module | def groups (base lines) | constants |
| --- | --- | --- |
| records | `host_path_patterns` `:423-451`; `sha256_hex`…`write_json` `:458-566`; `scan_no_secret_keys`; `require_*` family `:583-740`; `segments_match` `:743-753`; `PathMatcher` `:756-777`; `is_test_path`/`is_protected_asset` `:780-790`; `vitest_entry_index`/`is_vitest_entry` `:1939-1961` + `NODE_VALUE_FLAGS`; `validate_gate_env`…`validate_routes` `:1313-1455`; `redact_host_paths`/`sanitize_source_digest`/`require_recorded_redaction` `:1458-1504`; `validate_measured_mapping`…`validate_provisioning` `:1507-1564`; `validate_attempt_provisioning` `:1576-1586`; `requires_dependency_provisioning` `:1608-1620`; `validate_environment` `:1651-1674`; `validate_review_structure`/`review_semantic_blockers` `:1785-1888`; the usage validators `:2853-3061` | the declared schema/limits/patterns/key sets (`SCHEMA`, `STAGES`, `SEVERITIES`, `FINDING_STATUSES`, all `MAX_*_BYTES`, `COMMIT_PATTERN`…`SHA256_FIELD_PATTERN`, `TEST_SEGMENTS`/`TEST_FILE_PATTERN`/`PROTECTED_*`, the `*_KEYS` sets, `KNOWN_SUBTOTAL_*`, `PHASE_*`, `MODEL_ID_KEYS`, `PROMPT_HASH*`, `ATTEMPT_*`, `MEASURED_NAME_PATTERN`, `PROVISIONING_LOG_PATH`, `SANITIZE_SCRIPT`/`_HOST_PATH_PATTERNS`) |
| subject | `git_output`…`collect_changes` `:797-1257`; `validate_changes` `:1260-1286`; `current_subject`/`require_current_subject` `:1289-1306`; `load_source_subject` `:3391-3397` | `SUBJECT_NAMESPACE`, `SOURCE_SUBJECT_SCRIPT` |
| gates | `verify_dependency_tree` `:1589-1605`; `go_list_argv`…`recompute_gate` `:1895-2683`; `_is_junction`…`_tree_entries` `:3414-3519`; `dependency_tree_digest` `:3522-3544`; `_dependency_tree`…`require_resolved_dependency_tree` `:3678-3762` | process/trace constants: `PROCESS_TREE_REAP_SECONDS`, `DEFAULT_TIMEOUT`, `GO_LIST_FAILURE_PREFIX`, `EXUNIT_*` (patterns + prefixes), `VITEST_FAILURE_PREFIX`, `SOURCE_CHANGED_SUFFIX`, `TOOL_CACHE_*`, `TOOL_STATE_DIRNAME`, `DEPENDENCY_TREE_NAMESPACE`, `MAX_CAPTURE_BYTES`, `MAX_RESULT_BYTES`, `RESULT_KEYS`, `VALIDATOR_KEYS`, `VALIDATOR_SCRIPT` |
| receipts | `provisioning_view` `:1567-1573`; `require_dependency_provisioning` `:1623-1648`; `load_run`…`load_sealed` `:1677-1778`; `attempt_names`…`require_passing_attempt` `:2686-2846`; `build_receipt`/`derive_receipt` `:3064-3384` | `INCOMPLETE_CHECK_DETAIL` |
| entry | `environment_record` `:3400-3411`; `command_prepare` `:3547-3582`; `finish_prepare` `:3585-3675`; `command_bind_dependencies` `:3765-3856`; `command_seal` `:3859-3888`; `command_check` `:3891-3980`; `command_review` `:3983-4020`; `command_receipt` `:4023-4054`; `first_difference` `:4057-4091`; `command_accept` `:4094-4124`; `main` `:4127-4195`; the `__main__` guard | none of its own (imports) |

`validate_changes` sits in the subject module, not among the `validate_*` family, because it is part
of the changed-path rule and reads the subject module's own `refuse_filter_machinery` and
`utf8_path_key`; putting it in records would create a records → subject import cycle. Recorded as a
deliberate deviation from the contract's shorthand grouping ("records/validation modules hold …
the `validate_*` family"), which the module map supersedes as the judging artifact.

## Layering (import edges, acyclic)

```
records  ←─ subject ─┐
records  ←─ gates   ─┴─← receipts ←── entry
```
- records imports nothing local (leaf).
- subject imports records (`Refused`, `require_commit`, `PathMatcher`, `is_test_path`, `is_protected_asset`).
- gates imports records (`Refused`, `read_bytes`/`write_bytes`, `sha256_hex`, `strict_json`, `vitest_entry_index`, `require_bool`, `require_digest`, `require_integer`, `require_object`, `require_optional_integer`, `require_optional_text`).
- receipts imports gates, records, subject.
- the entry imports all four.

Every constant lives exactly once, in the module named above; the generator asserts that a
constant's users are all at or above its home module, so no cycle can hide. `pathlib.Path(__file__)`
constants (`SANITIZE_SCRIPT`, `VALIDATOR_SCRIPT`, `SOURCE_SUBJECT_SCRIPT`) resolve to the same
directory wherever they live; the receipt's own-tool path becomes an explicit
`ENTRY_SCRIPT = pathlib.Path(__file__).with_name("model-routing-evaluation.py").resolve()` in
`model_routing_receipts.py` so `tooling_sources` keeps binding the entry the coordinator runs (the
entry's path is frozen by D1). "Review fix r1" below makes the entry refuse whenever the four
modules did not resolve beside it and the entry is not the file `ENTRY_SCRIPT` names, so the
constant can only ever name files that executed.

## Public shape — the suites read the defining module

The entry imports exactly what its own code uses; it re-exports nothing it does not use. Contract
Change (1)'s recorded alternative is taken for every name the suites do not get from the entry's
own namespace: each `runner.<rule>` read and every module-global patch site is repointed to the
module that defines the rule (`importlib.import_module("model_routing_records" |
"model_routing_gates")`, the same object the entry imports). Reasons, recorded as the decision the
contract asked to state:

- a module-global patch only reaches code that executes in that module's namespace, so
  `SANITIZE_SCRIPT`/`_HOST_PATH_PATTERNS`, `subprocess` and `terminate_owned_process_tree` *must*
  be patched on the defining module;
- re-exporting the remaining reads would be a decorative import block kept alive only for the
  suite (ruff's isort splits every PEP 484 redundant alias into its own statement: 13 statements),
  and a second import surface invites drift. Repointing makes the suite state which module owns
  each rule, which is what the split is for;
- clause Change (3) explicitly allows it ("re-export or repoint, recorded"): the
  `SECRET_KEY_PATTERN`/`STAGES` equality pin now compares the extractor's copies with
  `model_routing_records`, the split home of both constants, and the extractor's copy comment names
  that file.

Reads that stay on the entry are exactly the ones the entry itself uses: `Refused`,
`canonical_json`, `dependency_tree_digest`, `first_difference`, `redact_host_paths` and
`_dependency_tree` (six names, machine-checked by the resolution check).

## Test repoints (the four-way coupling)

`test-model-routing-evaluation.py`, five write sites — a module-global patch only reaches code that
executes in that module's namespace, so these must name the defining module (contract Change (1):
"the suites are repointed in the same PR to the defining module — recorded"):

| base site | repoint |
| --- | --- |
| `:1120-1121` `runner.SANITIZE_SCRIPT = broken` / `runner._HOST_PATH_PATTERNS = None`, then `runner.host_path_patterns()` | records module attributes / call |
| `:1450` `runner.terminate_owned_process_tree = parent_only_kill` (+ cleanup) | gates module attribute |
| `:1462` `real_subprocess = runner.subprocess`, `:1492` `runner.subprocess = RecordingSubprocess()` (+ cleanup) | gates module attributes |
| `:2933` `refusal_returns(SCRIPT.read_text(…), "vitest_report_detail")` | the gates module file's text (AST walk is line-number-relative to the defining file; the counted set stays identical) |
| `:1075-1085` and `:3700-3712` tool-materialization loops | the one `MATERIALIZED_TOOL_NAMES` tuple gains the four new module file names, or the materialized entry cannot import |

The suites read the defining module with `importlib.import_module(name)`: it returns the
`sys.modules` instance the entry itself binds, so the suite reads and patches the defining module's
own globals, and it does not load the entry first. Every assertion, needle, count and fixture byte
stays; the re-exported `runner.*` reads stay as they are. (At r1 this was a named one-line helper,
`defining_module`; the P3 closeout below inlined it.)

`test-model-routing-omp-usage.py` loads `model_routing_records` at its six validator/constant sites
(`validate_usage`, `Refused`, the `STAGES`/`SECRET_KEY_PATTERN` pin) and keeps the
`load_usage_module()` loader for the extractor; it does not load the entry. Reads of rules the entry
keeps (`Refused`, `canonical_json`, `dependency_tree_digest`, `first_difference`,
`redact_host_paths`, `_dependency_tree`) stay spelled against the loaded runner.

## Delivered sizes (D2: report-only)

| file | base | delivered |
| --- | ---: | ---: |
| `scripts/ci/model-routing-evaluation.py` | 4,199 L / 200,035 B | 798 L / 36,608 B |
| `scripts/ci/model_routing_records.py` | — | 1,236 L / 53,672 B |
| `scripts/ci/model_routing_subject.py` | — | 539 L / 25,517 B |
| `scripts/ci/model_routing_gates.py` | — | 1,119 L / 55,535 B |
| `scripts/ci/model_routing_receipts.py` | — | 699 L / 32,272 B |
| total source | 4,199 L | 4,391 L (+192: four module import headers, the bound-tooling constants, the review-fix startup statements, the P3 closeout's `SPLIT_SCRIPTS` import, minus dropped comment lines) |

215 base top-level units: 214 delivered byte-identical, 1 (`build_receipt`) delivered with the
recorded `tooling_sources` patch and nothing else.

## `DELIVERED_TOOL_PATHS` (change (4), decision E)

`model-routing-source-subject.py` extends the tuple to the full delivered set — the entry, the four
modules, the suite — and its docstring sentence ("the two bound evaluation files") becomes the
truthful count. The output shape grows by four `delivered` lines; this tool is dispatch-only (not
CI-byte-pinned) and the growth is the authorized output change. The new pin test in
`test-model-routing-source-subject.py` asserts the exact tuple (mutation (c): drop a module → red)
and that every named path exists in the repository.

## Receipt `tooling_sources` (change (5), decision E)

The set grows from four to eight keys: `model-routing-evaluation.py` (`ENTRY_SCRIPT`),
`model_routing_records.py`, `model_routing_subject.py`, `model_routing_gates.py`,
`model_routing_receipts.py`, `assert-go-test-json.py`, `linux-v2-source-subject.py`,
`model-routing-record-sanitize.py`. The new test `test_receipt_binds_every_tooling_source` derives a
receipt through the accepted fixture, asserts the exact key set, and asserts each value is the
digest of the file beside the entry (mutation (d): drop a module → red). Receipts derived before the
split stay historical; the derivation necessarily refuses them, exactly as the evaluations README's
"tooling-source growth" paragraph describes for every earlier growth (see below).

## Comment policy applications (owner, binding for every hunk this slice touches)

- Section divider pairs (`:454-455`, `:717-718`, `:793-794`, `:1309-1310`, `:1781-1782`,
  `:1891-1892`, `:2849-2850`, `:3387-3388`) are dropped: each next module carries its own concern,
  and policy 4 names dividers. Zero code lines change with them.
- `:148-152` (tool-run-state comment) is dropped: it explicitly defers to the module docstring
  ("the module docstring's tool-run-state paragraph records…"), and the docstring moves with the
  entry, so the comment would cite a paragraph another module now owns; the docstring itself stays
  verbatim in the entry.
- `:411-418` (host-path definition) is trimmed to the one why-line beside `SANITIZE_SCRIPT`
  (recorded measured-surface values must not republish the operator profile or a disposable
  evaluation root); the lazy load from the sanitize tool and the refusal on a broken sibling live in
  `host_path_patterns()`'s docstring, and the base note's pattern-family enumeration duplicated that
  same docstring.
- Kept verbatim: the measured external shapes (`:171-178` ExUnit trace lines, `:182-185`
  `mix test` summaries), the launcher-flag note at `:1930-1935` (cites STD-A04), `:304-306`
  (extractor sync), `:340-341` (optional provisioning view).
- New module headers carry no docstrings: module intent, the map and the trade-offs live in this
  plan and the evidence record (policy 4).

## Method (why this split is machine-generated and machine-checked)

Moving 2,700+ lines verbatim cannot be done by hand-typing without transcription drift, and drift
in a security-boundary tool is the exact failure this slice must not have. The split is therefore
produced by a scratch generator (`<HOST-PATH>/.local/state/gdd/scratch/w5-12/split.py`, outside the
repository, recorded in the evidence record) that reads the base file, slices each top-level unit by
its AST span, and emits
the five files from an explicit unit→module table plus authored header/import text; the imports are
computed from each module's real name usage. Two independent checks run after generation and again
after any later edit:

1. **Verbatim check** — for every top-level unit, the delivered text's AST segment equals the base
   file's AST segment byte for byte; every base line is accounted for (unit → module, kept comment
   → module, dropped-with-reason, structural header/trailer, or blank) and `split.py` refuses on an
   unaccounted line; the printed accounting is a cover, not a partition. This is the
   machine-checkable form of lesson 32 for a move: a byte-identical body preserves every early
   return, refusal, `except` clause, default and exit-code path by construction.
2. **Resolution check** — every global name referenced by a module is defined in it, imported into
   it from a lower module, a stdlib import, or a builtin; and every name the two suites read
   through `runner` exists in the entry's namespace.

The generated files are format-clean and lint-clean as emitted (`ruff format --check scripts/ci`
reports `63 files already formatted`; no post-generation formatter pass was needed, and if one ever
wrote inside a migrated unit the verbatim check would fail), and `ruff check`/`format --check` are
the gates.

## Evidence plan

All gates foreground, sequenced (never a measurement harness concurrently with a suite):

| gate | command | expectation |
| --- | --- | --- |
| module split suites | `python3 scripts/ci/test-model-routing-evaluation.py` | exit 0, same test count, same printed refusal-coverage counts (17/15/2); budgeted > 580 s |
| extractor pin | `python3 scripts/ci/test-model-routing-omp-usage.py` | exit 0; `SECRET_KEY_PATTERN`/`STAGES` equality green |
| source-subject pin | `python3 scripts/ci/test-model-routing-source-subject.py` | exit 0; the new `DELIVERED_TOOL_PATHS` pin green |
| CLI byte-diff | `<HOST-PATH>/.local/state/gdd/scratch/w5-12/cli-byte-diff.sh` before/after, `cmp` per case | seven fixed fixture invocations (`--help`, an invalid operation, and the `accept`/`prepare`/`seal`/`check`/`receipt` refusals on missing inputs): stdout, stderr and exit code byte-identical |
| lint/format | `python3 -m ruff check scripts/ci` and `python3 -m ruff format --check scripts/ci` | exit 0 (ruff 0.17.5; the `ruff` mise shim resolves no version on this host, the pip-installed 0.17.5 module is used) |
| whitespace | `git diff --check` | exit 0 |
| mutants | (a) delete one refusal `return` in `vitest_report_detail` → the inventory test reddens; (b) change one `SECRET_KEY_PATTERN` copy → the equality pin reddens; (c) drop one module from `DELIVERED_TOOL_PATHS` → the new source-subject pin reddens; (d) drop one module from `tooling_sources` → the new receipt pin reddens | each red observed with exit code and message, then the file restored byte-identically |

Judging artifacts: the per-unit digest table, the module map, the refusal inventory before/after,
the byte-diff table, the loader/pin diffs, the mutant runs, the `DELIVERED_TOOL_PATHS` diff with
its pin, the `tooling_sources` key set, and the LOC delta (report-only, D2) — all in
`docs/evidence/rf-w5-12-model-routing-split-2026-11-22.md`.

## Out of scope (with reasons)

- The source-subject rules themselves (W5-11, now merged): the subject module moves its own
  functions verbatim, delegates to nothing and adds no second copy; `source_subject.py` is untouched.
- The sanitizer and its suites (#137's files, ruff-excluded today), `check-record-cleanliness.py`,
  `test-check-record-cleanliness.py`: not owned.
- Unifying `strict_json` with the other `scripts/ci` copies (contract clause 4: explicitly not here).
- `.github/workflows/**`: every suite path and CI step name stays; the CI step is the slow oracle.
- The entry's remaining size: module sizes are report-only (D2); the gates are the module map, the
  refusal inventory, the CLI byte-diff and the two binding pins. The entry keeps the CLI surface and
  the command handlers, which is what the contract asks it to keep.

## Known staleness this slice does not repair (out of the owned set)

- `docs/evaluations/model-routing/README.md`'s "Tooling-source growth" paragraph (dated
  verification section, 2026-11-20) states the current block has four entries; after this slice it
  has eight. The paragraph is a dated observation and the evaluations' `accept` rows there stay
  historical. The same section's re-verification procedure named a lone `git show` of the entry;
  the P3 closeout below refreshed it for post-split revisions (whole-set extraction), so only the
  four-entries paragraph remains stale.
- The contract's own `<file>:<line>` references re-derive above; the record carries the re-derived
  coordinates.

## Risks

- **Loader reachability**: the entry's `from model_routing_* import …` resolves through
  `sys.path[0]`, which is `scripts/ci` whenever a suite or the CLI runs a file from that directory
  (the `source_subject.py` convention W5-11 set). Both suite loaders and the two
  materialized-tool tests are covered; the CLI byte-diff exercises both the direct and the
  re-exported paths.
- **Line-number-relative pins**: the refusal-inventory walk must read the same file whose code
  object the coverage test traces; the repoint keeps that identity (gates module file ↔ gates
  module function). The printed line numbers change (2015/2044 → gates coordinates) and are
  recorded as historical.
- **Suite reads pinned to the entry**: the suites read six names through the loaded runner
  (`Refused`, `canonical_json`, `dependency_tree_digest`, `first_difference`, `redact_host_paths`,
  `_dependency_tree`); the entry uses each of them itself, and the resolution check enumerates the
  set, so an edit that drops one fails a machine check rather than a suite.

## Review fix r1 (2026-11-22)

The code review of `7f34d40d..8c295b5f` (verdict `MUST_FIX (4)`: F1-F4, plus advisory F5/F6)
produced six changes, all inside the owned set (no workflow, no `citest.py`, no new module):

- **F1 (binding).** The entry inserts its own resolved directory at `sys.path[0]` in the single
  `sys.path` statement between the stdlib imports and the four sibling imports — the form ruff's
  E402 exemption covers — so the tool set resolves beside the executing entry under
  `PYTHONSAFEPATH=1`/`-I`/`-P` instead of through an ambient module path. After the imports,
  `tooling_binding_problem()` refuses (tool stderr shape, exit 1, before any subcommand runs)
  unless all four modules resolved into the entry's directory and `Path(__file__).resolve()` is
  exactly `ENTRY_SCRIPT`. The reviewer's second option ("receipts refuses when the imported module
  directory differs from the executing entry's directory") is implemented in the entry because only
  the entry knows its own `__file__`; the refusal covers every subcommand, `--help` included.
  Regression pins: `test_an_ambient_module_path_cannot_rebind_a_tool_copy` (materialized copy under
  `PYTHONSAFEPATH=1` + `PYTHONPATH=<scripts/ci>`: pristine accepts; a mutated copy module refuses)
  and `test_an_incomplete_tool_copy_refuses_before_evaluating` (entry-only copy beside an ambient
  module path refuses with `did not resolve beside the entry`).
- **F2 (staleness).** The same guard proves source-vs-loaded identity: for every module with an
  existing bytecode cache file, `executed_matches_source()` requires the loaded code object to
  equal a fresh `compile` of the module's current bytes (with `dont_inherit=True`, as the loader
  compiles); a mismatch refuses before any command runs. Regression pin:
  `test_a_stale_bytecode_cache_cannot_supply_the_executed_rules` (a same-size edit with the mtime
  restored beside a valid `__pycache__` refuses naming the module; the mutated text never reaches
  the output).
- **F5 (advisory).** Both binding pins gain an ast-derived closure arm: `runner_module_closure()`
  walks every `model_routing_*` import from the entry transitively, and the closure must be inside
  the receipt key set / `DELIVERED_TOOL_PATHS`; the literal arms stay. At r1 the walk was duplicated
  in the two standalone suites because `citest.py` was outside the owned set; the P3 closeout moved
  the one copy both suites call into `citest.py` (`citest.runner_module_closure(script)`).
- **F6 (advisory).** The two-line comment on `test_receipt_binds_every_tooling_source` is removed;
  the invariant lives in the test name, the closure arm and the digest assertions.
- **F3/F4.** The layering list above now names the subject's and gates' actual `records` imports,
  and the evidence record's mutation (b) row cites `test-model-routing-omp-usage.py:323`.

`model_routing_records.py`, `model_routing_subject.py`, `model_routing_gates.py` and
`model_routing_receipts.py` keep their reviewed bytes; the refusal inventory, every existing
refusal message and the seven byte-diff invocations are unchanged, and the fix's new refusals fire
only where the tool set cannot be bound to what executes.

## P3 closeout (2026-11-22)

The PR review's four P3 findings (findings file outside the repository) are closed; `citest.py`
and the evaluations README join the changed set here:

- **P3-1 (derived tool set).** The entry no longer copies the four module names by hand:
  `_TOOL_MODULES = tuple(path.stem for path in SPLIT_SCRIPTS)` derives the guard's stale-bytecode
  list from the receipts module's one binding set. Mutation: drop `"model_routing_subject.py"` from
  `SPLIT_SCRIPTS` → `test_receipt_binds_every_tooling_source` reddens (`Items in the second set but
  not the first: 'model_routing_subject.py'`); the name is restored byte-identically (`sha256sum -c`
  OK, empty diff).
- **P3-2 (test hunks).** The name-restating docstrings are gone; `defining_module` and
  `load_records_module` are inlined as direct `importlib.import_module` calls; the duplicated
  `runner_module_closure` walk now lives once in `citest.py`.
- **P3-3 (records).** This closeout plus the D1 amendment above state the guard's refusal surface;
  the PR body replacement text (798-line entry, 84 tests, the r1 additions) is written to the
  review findings directory outside the repository.
- **P3-4 (README).** The "Receipt verification and the bound tooling revision" procedure now names
  the post-split whole-set extraction (`git archive <revision> scripts/ci | tar -x`); the
  "Tooling-source growth" paragraph stays a dated observation (see known staleness above).

Gate runs and re-measured figures: the evidence record's §11.
