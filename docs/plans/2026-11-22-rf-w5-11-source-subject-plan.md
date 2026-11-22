# rf-w5-11 plan — one source-subject implementation with named variants (SC S9 / F9b)

Contract: `goal-rf-w5-11-source-subject-single.md` (GDD refactor wave 5, 2026-11-21, draft plus the
2026-11-21 conductor decisions D1/D2/E and the lesson-32 control-flow decision). Baseline revision:
this worktree's HEAD `403142114adab9b58e45fc2809fc91f142680e1b` (`4031421`, identical to
`origin/main` at dispatch, carrying W4-16's #224 test harness, W3-16's merged `scripts/ci` repairs and
W1-11's ruff configuration), branch `refactor/rf-w5-11-source-subject-single`.

Binding inputs: the contract's Change (1)-(4), Gates, Six clauses; decision D1 (tool bytes frozen
except the recorded binding deltas), D2 (LOC delta is reported, never a gate), lesson 32
(control-flow preservation), the owner binding mandate (decision E), and the owner comment policy of
2026-11-22.

Program-plan claims re-derived at this revision (the contract asks for exactly this):

| Plan claim | Re-derived at `4031421` |
| --- | --- |
| "4 test suites" | **five** tool suites drive the five tools and their digests: `test-linux-v2-source-subject.py` (4 tests), `test-model-routing-source-subject.py` (6), `test-omo-control-native-evidence.py` (10), `test-credentialed-provider-witness.py` (5), `test-opencode-v2-support-witness.py` (16) |
| F9's `omo-control-native-evidence.py:497–542` line refs | stale; at HEAD `source_revision` is `:666`, `tree_digest` is `:709`, `SOURCE_PATHS` is `:69`, `BASELINE` is `:30-33`, the production self-check is `:1142` |
| CI step refs `ci.yml:2504/1481/367/1903` | at HEAD `:2581` (linux tool), `:1551` (omo suite), `:322` (credentialed suite), `:1980` (opencode suite), `:1540` (linux suite), `:1573` (model-routing suite) |
| `credentialed-provider-witness.py` "dispatch-only tool" | it is: `--allow-live-fixture` is required and the run needs a live fixture, PostgreSQL and `gh`; its CI oracle is its fixture suite at `ci.yml:322` |
| omo `SOURCE_PATHS` "33 entries" | **32** at this revision (`ast` count of the list literal at `HEAD:scripts/ci/omo-control-native-evidence.py`), 33 after D-1 |

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `scripts/ci/source_subject.py` | new | The single implementation of every source-subject rule: the five named variants (`index_mode_digest`, `working_tree_digest`, `committed_tree_digest`, `head_with_manifest`, `head_revision`), the shared primitives they compose (git argv builders, the two entry grammars, both sort keys, the exclusion sets, the execute-bit rule, the revision and cleanliness checks), and `Refused` with its rule codes. |
| `scripts/ci/linux-v2-source-subject.py` | thin CLI | keeps `main()` byte-identical and a module-level `source_subject(repository, expected_commit) -> SourceSubject` that delegates to `index_mode_digest` and maps the rule codes to its frozen messages. 83 -> 46 lines. |
| `scripts/ci/model-routing-source-subject.py` | thin CLI | keeps `main()` byte-identical; `source_paths`, `entry_bytes`, `source_subject`, `executable_paths` become module aliases (same names, same signatures, same semantics); the rule prose moves to the module. 138 -> 71 lines. |
| `scripts/ci/omo-control-native-evidence.py` | thin tool | `SOURCE_PATHS` gains the module; `source_revision`/`tree_digest` delegate to `committed_tree_digest`/`head_revision`/the declared-file-set verification and pass their receipt-observing git runner in; `file_identity`/`sha` become module aliases. 1815 -> 1813 lines. |
| `scripts/ci/credentialed-provider-witness.py` | thin tool | `source()` delegates to `head_with_manifest` and maps the rule codes to its frozen reason strings; the declared dirty scope becomes one `DirtyScope` constant that gains the module in all three spellings. 338 -> 319 lines. |
| `scripts/ci/opencode-v2-support-witness.py` | thin tool | `source_head` reads through `head_revision`; the single-file `witness_sha256` becomes `binding_sha256`, the map of the tool and the module. 523 -> 534 lines. |
| the five suites | modified | each tool's new binding test (decision E) plus the shared-rule pins the delegation needs (omo's two new refusal mappings, the non-mapping file-set refusal cases and the cross-variant digest agreement, credentialed's head/base rule, opencode's head rule); the fix pass adds the two approved-delta pins (linux's mocked malformed listing, credentialed's `--help` line); every existing assertion, message and fixture byte stays - the diffs are insertions only. |
| `docs/plans/2026-11-22-rf-w5-11-source-subject-plan.md` | new | this plan. |

`pyproject.toml`, `.github/workflows/**`, `scripts/ci/model-routing-evaluation.py` (+ its test),
`omo-native-evidence.py`, `model-routing-source-subject.py`'s `DELIVERED_TOOL_PATHS`,
`model-routing-record-sanitize.py` and `docs/TODO.md` are untouched (contract clause 3 and 4; no open
TODO entry names this surface, so neither TODO file changes).

## Variant table (contract judging artifact 1)

One row per tool: the base rule set it carried, the module entry point that now owns it, and the
parameters that reproduce the measured differences.

| Tool | Base rule set (measured at `4031421`) | Module variant + parameters |
| --- | --- | --- |
| `linux-v2-source-subject.py` | `:24-30` `rev-parse HEAD == expected`, clean `status --porcelain=v1 -uall`, `ls-tree --full-tree -r -z`, `cat-file --batch` streaming, reject non-blob, entry `mode\0kind\0path\0sha256:<hex>\0`, sorted by path, re-check `rev-parse`+clean before returning, `:62-65` | `index_mode_digest(repository, expected_commit)`: git source = committed objects read through one `cat-file --batch` process; exclusion sets = none; mode rule = `modes=None` (the kind decides); entry grammar = `tree_entry`; sort key = the committed-tree key; clean checks = exact head + clean before and after |
| `model-routing-source-subject.py` | `:54-64` declared exclusions (`EXCLUDED_DIRECTORIES`/`EXCLUDED_CACHE_NAMES`/`EXCLUDED_ROOTS`), `:78-86` working-tree walk skipping symlinked files, `:92` execute-bit rule (`0o111` -> `100755`), entry `mode\0sha256:<hex>\0<path-utf8>\0`, sorted by the UTF-8 bytes of the path | `working_tree_digest(root)` (+ `working_tree_paths`, `working_tree_entry`, `working_tree_executable_paths`): git source = the working tree; exclusion sets = the module's declared triples; mode rule = the execute bits; entry grammar = `working_entry`; sort key = UTF-8 path bytes; clean checks = none |
| `omo-control-native-evidence.py` | `:666-698` `source_revision` (local `rev-parse HEAD` through `command_bytes`, or a parent attestation verified per file by bytes/sha256 over `SOURCE_PATHS`), `:709-728` `tree_digest` (`ls-tree -r -z --full-tree` + one `cat-file blob` per entry, mode/kind admission, entry `mode\0kind\0path\0sha256:<hex>\0`, sorted by `(path, record)`) | `committed_tree_digest(records, read_object, modes=FILE_MODES)` + `head_revision(runner)` + `declared_file_identities`/`verify_declared_file_set`: git source = committed objects read one at a time by the caller's runner; mode rule = `100644`/`100755` only; entry grammar and sort key shared with the index variant; clean checks = none (its self-check at `:1142` keeps comparing the module's answer with the approved Subject) |
| `credentialed-provider-witness.py` | `:90-135` `source()`: `rev-parse HEAD == code_head_sha`, `code_base_sha == BASE`, per-entry sha256 over `changed_source_manifest`, scoped `status --porcelain=v1 -z -uall` with the rename skip, `relevant <= manifest paths`, `allow_manifest_bound_dirty` consent, canonical `manifest_sha256` | `head_with_manifest(root, config, git, base_sha=BASE, scope=SOURCE_SCOPE)`: git source = head + declared manifest; exclusion sets = the declared scope (pathspecs, prefixes, names); mode rule = n/a; entry grammar = the canonical-JSON manifest digest; clean checks = the scoped dirty check with its subset and consent rules |
| `opencode-v2-support-witness.py` | `:234` `source_head` = bare `git rev-parse HEAD`; `:235` `witness_sha256` = sha256 of the tool's own file | `head_revision(runner)`; `binding_sha256` = the module's file digests for the tool and the shared module: git source = bare head; the declared self-binding set |

Recorded variant names for the three shapes the plan did not name:
`committed_tree_digest` (variant C, the committed-tree digest read per object),
`head_with_manifest` (variant D, head plus a declared changed-source manifest),
`head_revision` (variant E, the bare head).

## Public shape

`scripts/ci/source_subject.py` is a library, imported as `import source_subject` from the importing
script's own directory (the convention `revision_scope.py` already sets): every repository entry point
runs a `scripts/ci` file as a script, so `sys.path[0]` is that directory - including
`model-routing-evaluation.py`'s by-path load of `linux-v2-source-subject.py`
(`citest.load_gate` keeps the same property for the suites). It does no work at import time.

`Refused(code, detail=None)` is the one refusal type; `code` names the rule, `detail` names the
offending path or record when the rule has one. The tools own the frozen message text and map codes
(language-local, unreachable from the module). Provenance and plumbing errors are *not* converted:
`OSError`, `subprocess.CalledProcessError`, `ValueError` and the callers' own `git`-runner refusals
escape unchanged, exactly as the base code let them. `UnicodeError` no longer applies where the base
read decoded strictly (omo's `.decode()`, opencode's `text=True`) and the read now goes through
`head_revision`, which decodes `ascii` with `replace`; the module's other revision reads
(`index_mode_digest`, `head_with_manifest`) stay strict. Unreachable with real hex output; recorded
rather than claimed preserved.

Primitives (each the single copy of exactly one rule):

| Name | Rule |
| --- | --- |
| `sha256_digest(data)` / `sha256_hex(data)` / `file_sha256_hex(path)` | the `sha256:<hex>` and bare-hex digest spellings, and the chunked file read |
| `file_identity(path)` | the observed `{path, bytes, sha256}` identity (omo's shape) |
| `WORKING_TREE_EXCLUDED_DIRECTORIES`, `WORKING_TREE_EXCLUDED_CACHE_NAMES`, `WORKING_TREE_EXCLUDED_ROOTS` | variant B's declared exclusion sets |
| `EXECUTE_BITS`, `MODE_REGULAR`, `MODE_EXECUTABLE`, `FILE_MODES` | the execute-bit test, the two mode literals and the modes a committed tree may hold |
| `tree_records(listing)`, `tree_entry(mode, kind, path, blob)`, `committed_tree_digest(records, read_object, *, modes)` | the ls-tree `-z` parse, the committed-tree entry grammar, the `(path, entry)` sort and the digest accumulation |
| `working_tree_entry(root, relative)`, `working_tree_paths(root)`, `working_tree_executable_paths(root)`, `working_tree_digest(root)` | the working-tree entry grammar (execute-bit mode), the declared walk, the execute-bit listing and the digest |
| `declared_file_identities(root, paths)`, `verify_declared_file_set(paths, declared, observed)` | the declared path set's observed identities and the never-trust-a-claimed-pass verification |
| `canonical_bytes(value)`, `manifest_digest(entries)` | the canonical-JSON bytes every witness payload is written as, and the digest the credentialed manifest binds |
| `DirtyScope(pathspecs, prefixes, names)` | one declared dirty surface in the three spellings its query and its filter use |
| `head_revision(runner)` | the bare head revision (variant E) |

The tools keep their own evidence plumbing and name it through module aliases where the rule moved:
`encoded = canonical_bytes` (credentialed), `sha = sha256_digest` and `file_identity =
file_identity` (omo), and the four `model-routing-source-subject.py` names (`source_paths`,
`entry_bytes`, `source_subject`, `executable_paths`), so every existing call site, signature and
value is unchanged.

Rule codes and the messages they map to (each tool's mapping is a dict in that tool; the *code* is
the module's, the *text* is the tool's):

| Code | `linux-v2-source-subject.py` | `omo-control-native-evidence.py` | `credentialed-provider-witness.py` |
| --- | --- | --- | --- |
| `head_mismatch` | "source checkout does not match the exact expected commit" | - | "code_head_mismatch" |
| `dirty_checkout` | "source checkout has staged, unstaged, or untracked changes" | - | - |
| `checkout_changed` | "source checkout changed while binding its subject" | - | - |
| `tree_entry_not_a_file` | "source tree contains a non-blob entry" | "fixture tree contains non-file effects" | - |
| `invalid_tree_record` | "Git returned an invalid source object" | "fixture tree listing is unreadable" (recorded delta, see decisions) | - |
| `invalid_source_object` | "Git returned an invalid source object" | - | - |
| `truncated_source_blob` | "Git returned a truncated source blob" | - | - |
| `batch_unavailable` | "Git batch pipes are unavailable" | - | - |
| `batch_failed` | "Git could not finish reading the source tree" | - | - |
| `base_mismatch` | - | - | "code_base_mismatch" |
| `manifest_required` | - | - | "source_manifest_required" |
| `manifest_path_invalid` | - | - | "invalid_source_manifest_path" |
| `manifest_digest_mismatch` | - | - | "changed_source_digest_mismatch" |
| `dirty_source_not_declared` | - | - | "dirty_source_missing_from_manifest" |
| `dirty_source_not_consented` | - | - | "dirty_source_requires_explicit_manifest_consent" |
| `declared_path_set_mismatch`, `declared_entry_shape`, `declared_content_mismatch` | - | "invalid parent source attestation identity or path set" / "parent source attestation differs from actual source blob: <path>" | - |

Recorded binding deltas (decision E; the only authorized changes to tool output):

| Id | Tool | Base | HEAD |
| --- | --- | --- | --- |
| D-1 | `omo-control-native-evidence.py` | `SOURCE_PATHS` has **32** entries (measured; the contract's "33" is off by one at this revision) and names no shared module; `receipt["source_files"]` and the parent attestation cover those 32 | `SOURCE_PATHS` gains `scripts/ci/source_subject.py`; `source_files` and the attestation cover 33 paths (a parent attestation declaring 32 is refused) |
| D-2 | `credentialed-provider-witness.py` | the dirty pathspecs and the name set name the two witness scripts; the manifest requirement covers them | both lists gain `scripts/ci/source_subject.py`, and the changed-source manifest must therefore cover an uncommitted edit to the module |
| D-3 | `opencode-v2-support-witness.py` | `"witness_sha256": <hex of the tool>` | `"binding_sha256": {"scripts/ci/opencode-v2-support-witness.py": <hex>, "scripts/ci/source_subject.py": <hex>}` |
| D-4 | `credentialed-provider-witness.py` | the argparse description says the manifest covers "these two scripts" | it names the witness scripts and the shared module (the usage text decision E makes stale) |

`test-model-routing-evaluation.py`'s/`test-model-routing-omp-usage.py`'s/`test-linux-v2-*`'s existing
assertions are the regression oracle for the two digest variants: the linux suite pins the known
synthetic subject's digest by an independently written formula, and the model-routing suite pins its
synthetic trees' equality/independence properties.

## Measured duplication baseline (before)

| Measure | Command (repository root, HEAD `4031421`) | Before |
| --- | --- | --- |
| lines in the five tools | `wc -l` on the five owned tools | 2897 (83 + 138 + 1815 + 338 + 523) |
| entry-grammar copies | `rg -n '\\0' <five tools>` | 3 rule sites (linux `:55`, model-routing `:94`, omo `:727`); the recorded command returns 9 matching lines at `d5b14b8` - the 3 rule sites plus omo's `ls-files`/`ls-tree`/`/proc` separators, model-routing's docstring `:19` and credentialed's `status -z` split |
| exclusion-set copies | `rg -n 'EXCLUDED_|__pycache__|node_modules' <five tools>` | 5 (all in model-routing) |
| execute-bit copies | `rg -n '0o111' <five tools>` | 2 (model-routing `:92`, `:108`) |
| `rev-parse` copies | `rg -n 'rev-parse' <five tools>` | 5 (linux `:24`, `:62`; omo `:674`; opencode `:234`; credentialed `:91`) |
| `ls-tree`/`cat-file` copies | `rg -n 'ls-tree\|cat-file' <five tools>` | 4 (linux `:30`, `:33`; omo `:715`, `:725`) |
| clean/dirty-check copies | `rg -n 'porcelain' <five tools>` | 3 rule sites (linux `:27`, `:63`; credentialed `:108`) plus omo's fixture-workspace status check at `:1677`, `:1682` (5 matching lines; that check is a different concern and stays) |
| file-digest copies | `rg -c 'sha256\(' <five tools>` | 14 |

## Control-flow preservation audit (contract lesson 32)

Every base check, refusal branch and sort/grammar decision, per variant, and the parameter that
reproduces it. "Preserved" is asserted by the byte-diff table and the suites, not by inspection
alone.

| Base check (tool:line) | Reproduced by | Preserved |
| --- | --- | --- |
| `rev-parse HEAD == expected_commit` (linux `:24-26`) | `index_mode_digest` -> `Refused("head_mismatch")` -> the tool's frozen message | yes |
| clean `status --porcelain=v1 -uall` before and after (`linux :27`, `:62-65`) | `index_mode_digest` (`dirty_checkout`, and `checkout_changed` when the re-check differs) | yes (both checks, same argv, same order) |
| non-blob rejection (`linux :44-45`) | `committed_tree_digest(modes=None)` -> `tree_entry_not_a_file` | yes (the kind decides; any mode is admitted, as base did) |
| batch reply validation (`linux :49-57`): 3-field header, `blob` kind, exact size, trailing newline, `wait()==0`, pipes present | the module's batch reader -> `invalid_source_object`, `truncated_source_blob`, `batch_failed`, `batch_unavailable` | yes (same reads, same order: one write per object, one header line, `size` bytes, one newline) |
| entry `mode\0kind\0path\0sha256:<hex>\0`, wrapped in a `sha256:` digest (`linux :59-60`, `:66`) | `tree_entry` + `committed_tree_digest` | yes |
| sort by path before streaming (`linux :41`) | `committed_tree_digest`'s `(path, entry)` sort; the read order stays the base's | yes (one entry per path makes the keys equivalent; recorded) |
| declared exclusions by parent directory (`model-routing :74-88`) | `working_tree_paths` (`parts[:-1]` test, root/`.git` skip, `is_file() and not is_symlink()`) | yes |
| execute-bit rule (`model-routing :92`) | `working_entry` (`stat().st_mode & 0o111`) | yes |
| UTF-8 path sort (`model-routing :88`) | `working_tree_paths`'s `encode("utf-8")` key | yes |
| `mode\0sha256:<hex>\0<path-utf8>\0` (`model-routing :91-93`) | `working_entry` | yes |
| `ls-tree -r -z --full-tree` parse and mode/kind admission (`omo :714-722`) | `tree_records` + `committed_tree_digest(modes=FILE_MODES)` | yes |
| per-object `cat-file blob` through the receipt-observing runner (`omo :723-726`) | `read_object` callback (the tool's runner, unchanged argv and event) | yes |
| `(path, record)` sort and `sha256:<hex>` digest (`omo :728`) | `committed_tree_digest` | yes |
| `rev-parse HEAD` through `command_bytes` (`omo :673-675`) | `head_revision(runner)` with the tool's runner | yes (the argv and its receipt event are the runner's) |
| attestation identity/path-set check (`omo :677-690`) | the tool's structural check plus `verify_declared_file_set` (`declared_path_set_mismatch`, `declared_entry_shape`) | yes |
| per-file bytes/sha256 verification (`omo :691-696`) | `verify_declared_file_set` (`declared_content_mismatch`) | yes |
| `rev-parse HEAD == code_head_sha` (`credentialed :91`) | `head_with_manifest` -> `head_mismatch` -> "code_head_mismatch" | yes |
| `code_base_sha == BASE` (`credentialed :92`) | `head_with_manifest(base_sha=BASE)` -> `base_mismatch` | yes |
| manifest path validity, file existence, no repeats (`credentialed :94-100`) | `manifest_path_invalid` | yes |
| per-entry sha256 (`credentialed :101-104`) | `manifest_digest_mismatch` | yes |
| scoped `status --porcelain=v1 -z -uall` with the rename/copy skip (`credentialed :105-124`) | `head_with_manifest`'s parse (`R`/`C` -> skip the next record) | yes |
| `relevant <= manifest paths` (`credentialed :125`) | `dirty_source_not_declared` | yes |
| `allow_manifest_bound_dirty` consent (`credentialed :126-129`) | `dirty_source_not_consented` | yes |
| canonical `manifest_sha256` (`credentialed :132-135`) | `manifest_digest` (same `ensure_ascii=False, sort_keys=True, indent=2` + newline bytes) | yes |
| bare head (`opencode :234`) | `head_revision(runner)` | yes |
| own-file digest (`opencode :235`) | `binding_sha256` (the tool's and the module's bytes) | yes, extended by D-3 |

The base behaviours this slice does *not* reproduce verbatim: a malformed `ls-tree -z` record
(no tab separator, or a header that is not three fields), which real git cannot emit. Base linux
raised `IndexError` for a tab-less record (it escaped `main`) and `ValueError` for a short header
(`main` printed "could not read the exact source checkout", exit 1); base omo's `ValueError` reached
`main`'s handler as `failures[].type = "ValueError"`, `classification: witness_failed`, exit 1. HEAD
refuses both as `invalid_tree_record`: linux -> `SystemExit("Git returned an invalid source
object")`, omo -> `failures[].detail = "fixture tree listing is unreadable"` (its one new message,
test-pinned; the linux mapping is unpinned). Measured in the apply pass, recorded in decisions
below, not hidden.

## Binding audit (decision E) and its mutation record

| Tool | Base self-binding scope | HEAD scope | Mutation that must redden the pinned test |
| --- | --- | --- | --- |
| `linux-v2-source-subject.py` | every blob of the committed tree (the whole tree is the binding) | unchanged; `scripts/ci/source_subject.py` is inside it by construction | change the module's bytes in the fixture tree -> the pinned digest moves (the index variant's entry list changes) |
| `model-routing-source-subject.py` | every declared working-tree path (minus the exclusions) | unchanged; the module is inside the declared scope | change the module's bytes -> the working-tree digest moves; the excluded paths keep not moving |
| `omo-control-native-evidence.py` | `SOURCE_PATHS` (32, measured) | `SOURCE_PATHS` (33, D-1) | drop the module from `SOURCE_PATHS` -> the attestation test accepts a stale module digest |
| `credentialed-provider-witness.py` | the two witness scripts (dirty scope, both spellings) and the manifest requirement | the two witness scripts plus the module (D-2) | drop the module from the scope -> the dirty-module test stops refusing |
| `opencode-v2-support-witness.py` | `witness_sha256` (the tool's bytes) | `binding_sha256` (the tool's and the module's bytes, D-3) | drop the module entry -> the binding test reddens |

## The byte-frozen evidence method (D1)

Every tool is exercised at the *same* fixed inputs on both sides of the change, and the records are
diffed byte for byte. The driver is a scratch script kept outside the commit (`.tmp/w5-11/`, which
`.gitignore` excludes and variant B's own exclusion set ignores); every rule it applies is stated
here:

1. the pre-change tools are extracted from this revision's tree (`git show HEAD:<path>`), so both
   sides run the same cases against the same fixtures;
2. CLI tools are invoked as `python3 <tool> <argv>` with `capture_output=True` (bytes, no decoding),
   and their stdout, stderr and exit code are recorded as-is;
3. in-process tools (omo, credentialed, opencode) are loaded through `citest.load_gate` and their
   *payload* functions are called with identical fixture inputs; the recorded value is the payload
   (`json.dumps(..., sort_keys=True, indent=2) + "\n"` for dicts, the message text for refusals),
   because those tools' stdout is a live-run receipt that needs the real daemon/Windows blob;
4. fixture repositories are built by one shared builder (a temporary directory, `git init`, a fixed
   identity, `core.autocrlf=false`), and every recorded string has the temporary root replaced with
   `<ROOT>` so random directory names cannot appear in the diff;
5. the `linux-v2-source-subject.py` cases include this checkout's own revision: a `--shared` clone
   at `4031421` (a clean checkout of the committed tree, the shape CI passes), the dirty worktree
   itself (the refusal case), and fixture repositories at a known commit;
6. the diff is `diff` over the two record files; the only allowed differences are D-1..D-4, each of
   which is applied to the pre-change record before the comparison and named in the table.

Children are not patched: the recorded invocations are the ones a plain run makes.

## Platform limits of the local gate run

| Suite | Windows host (this workstation) | WSL Ubuntu-24.04 (same mount) |
| --- | --- | --- |
| `test-linux-v2-source-subject.py` | exit 0, `Ran 6 tests`, `OK` | exit 0, `Ran 6 tests`, `OK` |
| `test-model-routing-source-subject.py` | exit 0, `Ran 7 tests`, `OK (skipped=1)` (no POSIX execute bit) | exit 0, `Ran 7 tests`, `OK` |
| `test-omo-control-native-evidence.py` | exit 0, `Ran 15 tests`, `OK` | exit 0, `Ran 15 tests`, `OK` |
| `test-opencode-v2-support-witness.py` | exit 0, `Ran 18 tests`, `OK` | exit 0, `Ran 18 tests`, `OK` |
| `test-credentialed-provider-witness.py` | exit 1, `Ran 8 tests`, `FAILED (errors=2)`: `mix` is a Windows `.cmd`/Bash shim that `subprocess` cannot execute (pre-existing, the same two failures as base) | exit 1, `Ran 8 tests`, `FAILED (failures=1)`: Hex is unavailable offline, so the Elixir policy test cannot build its dependencies (pre-existing, the same failure W4-16 recorded) |

The two mix-dependent launcher tests therefore run for real in WSL (where the fixture Mix project
compiles), and the Elixir-controls test's dependency refusal is pre-existing and identical on both
sides. The contract's slow oracle for the credentialed tool is its CI step `ci.yml:322`, which runs
it where Hex and the control dependencies are prepared; this stage names it as the oracle rather than
claiming a local pass.

`ruff` is not provisioned on this host (`mise` has no version set for its shim), so the pinned
version was installed for the gate run: `python3 -m pip install ruff==0.17.5`, which is the
`required-version` in `pyproject.toml`; the gate commands are recorded with the exact version.

## Decisions and trade-offs

1. **Codes in the module, texts in the tools.** Five tools phrase the same rule differently
   ("source tree contains a non-blob entry" against "fixture tree contains non-file effects"), so one
   message table in the module would change four tools' bytes. The module raises a code; each tool
   maps it. The mapped dict is a CLI surface, not a rule copy: it contains no exclusion, sort,
   grammar, mode or check.
2. **The declared scope stays data.** `DirtyScope` carries the pathspecs *and* the filter the base
   code applied a second time (`:130-135`). The filter is redundant for the declared surface, but
   collapsing it would be a rule change; it is preserved and documented instead.
3. **Git runners stay in the tools.** Each witness's runner is evidence plumbing (receipt events,
   `cwd` against `-C`, environment isolation, stderr policy, refusal text). The module receives a
   runner where the rule needs one (`head_revision`, `head_with_manifest`, `read_object`) and owns
   *what* is asked and *what* the answer must satisfy. The rule greps below show no rule left behind.
4. **`invalid_tree_record` is the one new message.** Base raised an uncaught `IndexError`/`ValueError`
   for a listing git cannot produce. The module refuses it; linux maps it to its existing
   invalid-object message, omo gets "fixture tree listing is unreadable". Named here because the
   contract freezes refusal text: no suite, fixture or real git invocation can reach it, and the new
   test pins it.
5. **`binding_sha256` replaces `witness_sha256`** (D-3): a field named for one file cannot honestly
   carry a two-file binding set, and the map makes "the set a receipt binds" readable. Nothing parses
   the field: `daemon/internal/harness/testdata/opencode/2.3.0/*.json` are recorded receipts, and no
   decoder reads them.
6. **Base-vs-HEAD history stays history.** The recorded 0008 subjects quote digests of *their*
   revisions; the tool edits necessarily change the working-tree variant's digest of the current
   revision (the tool bytes are themselves inside scope B). "Exact digests unchanged" therefore binds
   the algorithm's output on fixed inputs, which is what the suites and the byte-diff table check.
7. **`DELIVERED_TOOL_PATHS` is untouched** (contract clause 4): the 0008 records bind the two
   evaluation files, and extending that set is W5-12's after its split. The module is inside the
   working-tree scope regardless, so the subject digest covers it today.
8. **Comment policy.** The module's docstring documents the five variants, their rules and the
   import convention (policy class 3: a published contract); the tools keep only their usage/output
   contract plus the variant they use. No excuse comments, no banners; the base's rule prose moves
   rather than being copied.

## Rule greps after the change (contract clause 1's "definition grep per rule")

| Rule | Command | Required |
| --- | --- | --- |
| entry grammars | `rg -n '\\\\0'` (the `\0` separator literal) over the five tools | no source-subject rule site (omo's two remaining separators are the `ls-files -z` census and the `/proc/<pid>/cmdline` read; measured `rg -c` below) |
| working-tree exclusions | `rg -n 'EXCLUDED_|__pycache__|node_modules|playwright-report'` over the five tools | 0 matches |
| execute-bit / mode rule | `rg -n '0o111|100755'` over the five tools | 0 matches |
| path sort keys | `rg -n 'encode\("utf-8"\)|sorted\('` over the five tools | no source-subject rule site (omo's four remaining matches are `canonical()`'s encode, the receipt's secret sort and two environment-name sorts; measured `rg -c` below) |
| revision binding | `rg -n 'rev-parse'` over the five tools | 0 matches |
| clean/dirty source checks | `rg -n 'porcelain'` over the four binding tools (`omo`'s workspace-status check at `:1677` is a different concern and stays) | 0 matches outside that documented case |
| declared-content verification | `rg -n 'hashlib'` over the five tools | linux/credentialed 0; omo keeps its four domain `client_*_id` digests and opencode its pinned-blob `file_digest` check, none of them source-subject rules (measured `rg -c` below) |

## Evidence plan (contract Gates, all foreground and serialized)

```
python3 scripts/ci/test-linux-v2-source-subject.py            # Windows + WSL
python3 scripts/ci/test-model-routing-source-subject.py       # Windows + WSL
python3 scripts/ci/test-omo-control-native-evidence.py        # Windows + WSL
python3 scripts/ci/test-opencode-v2-support-witness.py        # Windows + WSL
MIX_ENV=test python3 scripts/ci/test-credentialed-provider-witness.py   # Windows + WSL
python3 scripts/ci/test-model-routing-evaluation.py <a prepare-using test>   # the by-path consumer
python3 -m ruff check scripts/ci                              # 0.17.5, the config's required-version
python3 -m ruff format --check scripts/ci
python3 scripts/ci/check-lockfile-registry.py                 # sanity
git diff --check
```

Plus, in the scratch driver: the byte-diff records, the pinned-BASELINE reproduction
(`/q/repos/fixture-0006h` is a local clone of the fixture at `6adc9ef1a2f704590ee4172cecb4e0bb6c70408c`,
so the module's committed-tree variant must return `sha256:1831626e62b934f63cf3abd59d3e5ed73e5a21d898eccd6e7d349ae116a39d92`),
and the five judging mutants ((a) drop an exclusion, (b) change the index sort key, (c) synthesize
the manifest revision, (d) drop the module from a tool's binding, (e) mutate the module's bytes).

## Out of scope

- `model-routing-evaluation.py` and `test-model-routing-evaluation.py` (W5-12; the sixth git-binding
  copy at `:965-1082` and the `tooling_sources` entry at `:3283-3291` stay and move verbatim there).
- `scripts/ci/omo-native-evidence.py`, `model-routing-omp-usage.py` and its test,
  `model-routing-record-sanitize.py` and its tests, `delivered DELIVERED_TOOL_PATHS`.
- `.github/workflows/**` (the suite steps keep their names and invocations), `pyproject.toml`.
- Production `strict_json`/`parse_json` copies and the other W4-16-identified duplication.
- The acceptance workflows' dispatch semantics, `BASELINE`'s value, the fixture repositories, and any
  behaviour of the witness tools other than revision binding.

## Evidence (measured 2026-11-22, on the uncommitted tree)

### Gate runs

| Gate (exact command) | Exit | Measured output |
| --- | --- | --- |
| `python3 scripts/ci/test-linux-v2-source-subject.py` | 0 | `Ran 6 tests in 19.398s`, `OK` (base `4031421`: `Ran 4 tests`, `OK`) |
| `python3 scripts/ci/test-model-routing-source-subject.py` | 0 | `Ran 7 tests in 0.453s`, `OK (skipped=1)` (base: `Ran 6 tests`, `OK (skipped=1)` - this Windows host reports no execute bit) |
| `python3 scripts/ci/test-omo-control-native-evidence.py` | 0 | `Ran 15 tests in 35.356s`, `OK` (base: `Ran 10 tests`, `OK`) |
| `python3 scripts/ci/test-opencode-v2-support-witness.py` | 0 | `Ran 18 tests in 9.012s`, `OK` (base: `Ran 16 tests`, `OK`) |
| `MIX_ENV=test python3 scripts/ci/test-credentialed-provider-witness.py` (Windows) | 1 | `Ran 8 tests in 90.954s`, `FAILED (errors=2)`: the same two pre-existing errors as base (`Ran 5 tests`, `FAILED (errors=2)`) - `mix` here is a `.cmd`/Bash shim `subprocess` cannot execute |
| the same five suites in WSL Ubuntu-24.04 | 0 / 0 / 0 / 0 / 1 | linux `Ran 6 tests`, `OK`; model-routing `Ran 7 tests`, `OK`; omo `Ran 15 tests`, `OK`; opencode `Ran 18 tests`, `OK`; credentialed `Ran 8 tests`, `FAILED (failures=1)` - the same pre-existing Hex-availability failure as base (`Ran 5 tests`, `FAILED (failures=1)`) |
| `python3 scripts/ci/test-model-routing-evaluation.py EvaluationTests.test_prepare_environment_json_records_the_measured_surface` | 0 | `Ran 1 test in 23.458s`, `OK` - a successful `prepare`/`accept` exercises `load_source_subject()` (`model-routing-evaluation.py:3392`): the by-path load of `linux-v2-source-subject.py` now resolves `import source_subject`, and the receipt's `tooling_sources` recompute against the new tool bytes |
| `python3 -m ruff check --no-cache scripts/ci` (ruff 0.17.5, the config's `required-version`; the plain form was also run first) | 0 | `All checks passed!` |
| `python3 -m ruff format --check --no-cache scripts/ci` | 0 | `58 files already formatted` |
| `python3 scripts/ci/check-lockfile-registry.py` | 0 | `verified every recorded tarball URL ... 303 entries checked` |
| `git diff --check` | 0 | no output |

### The tool-output byte-diff (D1)

The driver (`.tmp/w5-11/diff_tools.py`, scratch, outside the commit) records 49 Windows and 51 WSL
cases through the pre-change tools (`git show HEAD:...`) and the post-change tools on identical
fixtures, and `compare.py` fails on any difference that is not one of D-1..D-4. Windows:
**49 cases, 37 identical, 12 recorded deltas, 0 mismatches, exit 0**; WSL Ubuntu-24.04 (POSIX
execute bits and symlinks, two extra cases): **51 cases, 39 identical, 12 recorded deltas, 0
mismatches, exit 0**.

| Tool | Identical cases | Recorded deltas |
| --- | --- | --- |
| `linux-v2-source-subject.py` | 11 (clean fixture, wrong commit, staged/unstaged/untracked dirty, non-repository, three usage shapes, a `--shared` clone of this revision's real tree, this worktree's own refusal) | 0 |
| `model-routing-source-subject.py` | 5 Windows (+1 WSL execute-bit case): the suite's synthetic tree, a tree with the module's bytes changed, a tree missing the delivered files, a non-directory root, two-argument usage | 0 (+1 head-only re-derivation of the `342d1226` tree: Windows `source_paths: 1297`, `source_subject: sha256:f22c5d19…`; WSL `source_paths: 1297`, `source_subject: sha256:3cbd6f6e…`; the earlier `1299`/`f1945b78…`/`ded2fa82…` reading was the pre-merge `5b3a5a15` worktree, before the final merge deleted #231's two `daemon/internal/protocol` files) |
| `credentialed-provider-witness.py` | 11 (clean manifest, digest mismatch, empty manifest, path outside the root, head mismatch, base mismatch, unlisted dirty file, non-repository, declared-but-absent path, staged rename, usage-none) | D-2 x3, D-4 x1 |
| `omo-control-native-evidence.py` | 8 Windows (+1 WSL symlink case): the pinned baseline tree, a small committed tree, the ls-tree/cat-file read sequence, four attestation refusals (`content-flipped`, `entry-shape`, `path-added`, `path-dropped`), the local-head revision | D-1 x5 |
| `opencode-v2-support-witness.py` | 2 (the head revision through both the old expression and `head_revision`; the digest expression equals the module's `file_sha256_hex` for both binding files) | D-3 x2 |

The recorded deltas, verbatim from the comparison:

| Id | Case | Base | HEAD |
| --- | --- | --- | --- |
| D-1 | `omo/source-revision-attested` | 32 declared paths, no module key | 33 paths, `sha256:ddc888c6…` for `scripts/ci/source_subject.py` |
| D-1 | `omo/local-head-paths` | 32 | 33 |
| D-1 | `omo/attestation-module-flipped` | absent at base (the module is not in the declared set) | `Rejected: parent source attestation differs from actual source blob: scripts/ci/source_subject.py` |
| D-1 | `omo/attestation-module-dropped` | absent | `Rejected: invalid parent source attestation identity or path set` |
| D-1 | `omo/attestation-module-bytes-mutated` | `accepted` (base ignores the module's bytes) | `Rejected: parent source attestation differs from actual source blob: scripts/ci/source_subject.py` |
| D-2 | `credentialed/module-untracked-undeclared` | accepted (`manifest_bound_dirty_paths: []`) | `RuntimeError: dirty_source_missing_from_manifest` |
| D-2 | `credentialed/module-untracked-declared` | accepted, dirty list empty | accepted, `manifest_bound_dirty_paths: ["scripts/ci/source_subject.py"]` |
| D-2 | `credentialed/module-untracked-declared-consent-off` | accepted | `RuntimeError: dirty_source_requires_explicit_manifest_consent` |
| D-3 | `opencode/binding-keys` | absent (`witness_sha256` covered one file) | `["scripts/ci/opencode-v2-support-witness.py", "scripts/ci/source_subject.py"]` |
| D-3 | `opencode/binding-matches-expression` | absent | both entries equal `hashlib.sha256(path.read_bytes()).hexdigest()` |
| D-4 | `credentialed/usage-help` | "and these two scripts." | "and the witness scripts plus the shared scripts/ci/source_subject.py module." (the only `--help` difference; exit 0 both sides; the conductor's 2026-11-22 amendment to D1, pinned by `test_usage_declares_the_shared_module_in_the_manifest_scope`) |

The pinned-baseline gate reproduces exactly: `omo/pinned-baseline-tree` reads the local clone
`Q:/repos/fixture-0006h` at `6adc9ef1a2f704590ee4172cecb4e0bb6c70408c` and both the base tool and the
module return `sha256:1831626e62b934f63cf3abd59d3e5ed73e5a21d898eccd6e7d349ae116a39d92`, the
`BASELINE["tree_digest"]` recorded in `omo-control-native-evidence.py:30-33`.

### Mutation record

| Mutant | Applied to | Measured red |
| --- | --- | --- |
| (a) drop an exclusion | `WORKING_TREE_EXCLUDED_DIRECTORIES` loses `node_modules` | `test_declared_scope_selects_only_source_paths` `FAILED`: `['frontend/src/app.ts', 'node_modules/react/index.js', 'scripts/ci/tool.py'] != ['frontend/src/app.ts', 'scripts/ci/tool.py']`; `test_digest_ignores_the_tree_location_and_the_exclusions` `FAILED` (2 != 1 entries); suite exit 1 |
| (b) change the index variant's sort key | `committed_tree_digest` sorts `reverse=True` | `test_the_binding_covers_the_shared_module` `FAILED` for both subtests (`sha256:017b0fa0…` against the pinned `sha256:6cb2302d…`); suite exit 1. The single-file known-subject case is sort-invariant by construction, and the omo cross-variant test stays green because its two variants share the rule - the independent formula is the killer |
| (c) synthesize the revision | `head_with_manifest`'s head check becomes a tautology | `test_the_binding_requires_the_declared_head_and_base (field='code_head_sha')` `FAILED`: `RuntimeError not raised`; exit 1 |
| (d) drop the module from a binding | `SOURCE_PATHS` loses `scripts/ci/source_subject.py` | omo `test_the_declared_file_set_covers_the_shared_module` `FAILED`: `'scripts/ci/source_subject.py' not found in {...}`; exit 1 |
| (d) drop the module from a binding | `binding_sha256`'s path tuple loses the module | opencode `test_the_binding_covers_the_tool_and_the_shared_module` `FAILED`: `['scripts/ci/opencode-v2-support-witness.py'] != [... , 'scripts/ci/source_subject.py']`; exit 1 |
| (d) drop the module from a binding | `SOURCE_SCOPE` loses the module in both spellings | credentialed `test_the_declared_scope_covers_the_shared_module` `FAILED`: `RuntimeError not raised`; exit 1 |
| (e) mutate the shared module's bytes | one `# mutated` comment line appended to `scripts/ci/source_subject.py` | exactly three recorded observations moved (re-measured on `342d1226`): the model-routing delivered-tree subject (`source_paths` stays 1297, digest `sha256:f22c5d19… -> sha256:ec024844…`), the opencode binding module entry (`d16097a8… -> 429fb684…`) in `binding-keys` and `binding-matches-expression`; and the D-1/D-2 rows above are the omo/credentialed refusals a mutated module triggers where base accepted it. After reverting (the file's sha256 restored to `d16097a8…`) the binding value returned to `d16097a8…` |
| (f) return a bare-hex `file_identity` digest (S5's converted oracle) | `file_identity` drops `DIGEST_PREFIX` | omo `test_the_declared_file_set_covers_the_shared_module` `ERROR`: `Rejected: parent source attestation differs from actual source blob: scripts/ci/omo-control-native-evidence.py`; suite exit 1 - the independent declaration no longer moves with the function under test |
| (g) drop the declared-file-set mapping check (D1) | `verify_declared_file_set` loses `isinstance(declared, Mapping)` | omo `test_an_attestation_whose_file_set_is_not_a_mapping_is_refused` `ERROR`: `TypeError: list indices must be integers or slices, not str`; suite exit 1 |

`git diff --numstat` over the five suites is `33/0`, `38/0`, `10/0`, `91/0`, `26/0`
(insertions/deletions, alphabetical by path: credentialed-provider-witness, linux-v2-source-subject,
model-routing-source-subject, omo-control-native-evidence, opencode-v2-support-witness): every
removed line was added by this slice itself, so no assertion is deleted or re-pinned, and the apply
pass's two converted oracles carry the (f)/(g) mutation checks.

### Rule greps after the change (the definition grep per rule)

`rg -c` over the five tools (`linux-v2-source-subject.py`, `model-routing-source-subject.py`,
`omo-control-native-evidence.py`, `credentialed-provider-witness.py`,
`opencode-v2-support-witness.py`):

| Grep | Matches | Remaining matches are |
| --- | --- | --- |
| `EXCLUDED_\|__pycache__\|node_modules\|playwright-report` | 0 | - |
| `0o111\|100755` | 0 | - |
| `rev-parse` | 0 | - |
| `ls-tree\|cat-file` | 2 (omo) | the runner's argv inside `tree_digest`'s listing call and its `read_object` |
| `porcelain` | 2 (omo) | the fixture-workspace status check at `:1675-1680`, which is the native-turn artifact rule, not a source binding |
| `\\0` | 2 (omo) | the `ls-files -z` workspace census at `:148` and the `/proc/<pid>/cmdline` read at `:777` |
| `hashlib` | 5 (omo), 2 (opencode), 0 elsewhere | omo's `client_message_id`/`client_turn_id` domain digests (4) plus the import; opencode's pinned-blob `file_digest` check plus the import |
| `encode("utf-8")\|sorted(` | 4 (omo) | `canonical()`'s encode, the receipt's secret sort, two environment-name sorts |

### LOC delta (contract D2: reported, never a gate)

| Measure | Base `4031421` | HEAD | Delta |
| --- | --- | --- | --- |
| the five tools | 2897 (83 + 138 + 1815 + 338 + 523) | 2783 (46 + 71 + 1813 + 319 + 534) | **-114** |
| `scripts/ci/source_subject.py` | absent | 327 (44 docstring + 283 code) | **+327** |
| owned set, net | 2897 | 3110 | **+213** |
| the five suites | 1050 | 1248 | +198 (new tests only) |
| `docs/plans/2026-11-22-rf-w5-11-source-subject-plan.md` | absent | this file | new |

The plan's `-200..-300` figure came from F9's combined estimate, which counted the 4k-line
`model-routing-evaluation.py` split that the conductor decoupled into W5-12; this slice deletes the
four divergent copies (-114 lines in the tools) and pays for one implementation (+327). The
divergent-rule count is the change that matters and it is 5 -> 1 with 0 copies left in the tools.

## Residual risks and known limits

1. **The LOC target is not met** (reported above, D2).
2. **One new refusal message path**: `invalid_tree_record` -> omo's "fixture tree listing is
   unreadable" for a listing git cannot emit. Both mappings are test-pinned (omo's
   `test_a_listing_that_is_not_a_record_is_refused_without_reading_a_blob`, linux's
   `test_a_listing_that_is_not_a_record_refuses_as_an_invalid_source_object`); the linux path is an
   approved delta (the conductor's 2026-11-22 amendment): base's tab-less record raised `IndexError`
   (escaping `main`) and its short header reached `ValueError` -> "could not read the exact source
   checkout" (exit 1), while HEAD raises `SystemExit("Git returned an invalid source object")`
   (exit 1).
3. **The linux variant's header parse is now as strict as the omo one** (three fields): base's
   `split(b" ", 2)` tolerated a fourth field, which git never produces, and the module's shared
   parse refuses it. Recorded in the control-flow audit, unreachable from real git.
4. **D-4 changes `--help` prose** (the argparse description) because decision E makes the old
   sentence false; recorded as the conductor's 2026-11-22 amendment to D1 and pinned by
   `test_usage_declares_the_shared_module_in_the_manifest_scope`.
5. **Working-tree scope is exclusion-set bounded**: a ruff run leaves an ignored `.ruff_cache/`
   that the working-tree variant binds (it is not in the declared exclusion set), so a
   `delivered-tree` subject taken after running ruff differs from one taken before. This is
   pre-existing behaviour of the declared scope, not changed here; the numbers above were taken
   with the cache removed, and the final gate run used `--no-cache` for that reason.
6. **The witness tools' live runs are not locally reproducible**: `omo-control-native-evidence.py`
   needs the daemon, the fixture clone and the OmO binary; `opencode-v2-support-witness.py` needs
   Windows, the operator credential and the pinned 257553288-byte blob; the credentialed witness
   needs PostgreSQL, `gh` and a live fixture. Their CI steps (`ci.yml:1551`, `:1980`, `:322`) are the
   recorded oracles, and this slice's local evidence for them is their fixture suites plus the
   recorded `binding_sha256`/`source_files`/`source.json` payloads above.
7. **The credentialed suite's two `mix`-dependent tests cannot run on this Windows host**
   (pre-existing, unchanged set of failures) and its Elixir-controls test cannot build dependencies
   offline in WSL (pre-existing, unchanged).
8. **Historical evidence stays historical**: recorded 0008 subjects and the `daemon/.../testdata`
   receipts quote their own revisions; the tool bytes are inside variant B's scope by design, so a
   recorded row cannot be re-derived after any tool edit.
9. **The sibling import is the repository's own convention**: `import source_subject` resolves from
   the importing script's own directory (`sys.path[0]`), exactly as `import revision_scope` does in
   `check-citation-existence.py`; every entry point here satisfies it (script execution, a suite in
   `scripts/ci`, and `model-routing-evaluation.py`'s by-path load, which the consumer test above
   exercises). Loading a tool by path from a process whose first path entry is elsewhere needs
   `scripts/ci` on `sys.path`, as it already did for `revision_scope`.
