# rf-w4-16 plan — one Python CI-test harness: `citest` + `FixtureRepo`

Contract: `goal-rf-w4-16-python-test-harness.md` (GDD refactor wave 4, 2026-11-21). Baseline revision:
this worktree's HEAD `12de9428a4db5cbe67461ed8511c08978ea3364e` (`12de9428`, identical to
`origin/main` at dispatch, carrying W3-01's #212, W3-16's #180 (`fc8e95a2`, the merged
`fix(scripts): repair CI script defects` PR) and W1-11's #151 ruff
configuration). Branch `refactor/rf-w4-16-python-test-harness`. Binding inputs: the contract's
Change (1)-(6), Gates, Six clauses; conductor decisions D1 (frozen bytes), D2 (LOC report-only),
D11 (unittest stays), E7 (a suite that is not wired is not created), lesson 32 (control-flow
preservation audit), and the owner comment policy of 2026-11-22.

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `scripts/ci/citest.py` | new | `load_gate`, `run_gate`, `git`, `git_text`, `write_bytes`, `write_json`, `strict_json`, `GateImportRefused`, `StrictJsonRefused`. |
| `scripts/ci/testsupport/gitfixture.py` | new | `FixtureRepo`: the `setUp`-mixable throwaway repository (`root`/`repo`/`git`/`git_text`/`write`/`commit`) with the repo-local identity, `commit.gpgsign=false`, `core.autocrlf=false` and the `GIT_DIR`-family environment isolation. A namespace package directory (no `__init__.py`; `import testsupport.gitfixture` resolves through the suite's own directory on `sys.path[0]`). |
| `scripts/ci/test-citest.py` | new | `citest`'s own suite: 29 tests, one POSIX-only skip; the mutant killers for `run_gate` (synthesized success), `write_json` (non-canonical bytes) and `strict_json` (duplicate keys, non-finite numbers), plus the six-variable `GIT_REPOSITORY_VARIABLES` isolation table and the `GIT_CONFIG_GLOBAL` / `run_gate(timeout=)` pins (apply stage, findings F6/F7) and the fix stage's pins: empty stdin by default, the stderr echo on failure, the config redirect/injection drop and the umask-controlled mode assertion. |
| `scripts/ci/test-gitfixture.py` | new | `FixtureRepo`'s own suite: 12 tests; the mutant killer for the identity/signing/eol isolation (runs a fixture commit under `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` pointed at `os.devnull` plus `GIT_CONFIG_NOSYSTEM=1`), plus the fix stage's `GIT_CONFIG`-redirect probe (the fixture identity survives a redirected config file). The vacuous auto-collected `_Probe.runTest` case is gone: the probe class lives inside its only test (apply stage, finding F2). |
| `.github/workflows/ci.yml` | modified | the daemon job's "Check CI gate helper scripts" step gains the two new suite invocations (E7). Nothing else in the file. |
| 22 test suites under `scripts/ci/` | modified | harness plumbing only: loader blocks, `run_gate`/`run_check` bodies, git-fixture `setUp` bodies, `write_json`/`write_log` bodies. Every assertion, message and fixture byte stays. |
| `docs/plans/2026-11-22-rf-w4-16-python-test-harness-plan.md` | new | this plan. |

`pyproject.toml` is untouched: the new modules need no per-file ignore (measured: `ruff check
scripts/ci` and `ruff format --check scripts/ci` are clean with the W1-11 config as written).
`docs/TODO.md` and `docs/archive/CLOSED-TODO.md` are untouched: no open TODO entry names this
slice's surface, and the deferred production `strict_json` unification belongs to W5-11/W5-12, not
to a new TODO entry.

## Public shape

`citest.load_gate(path, name)` is the one loader shape: `spec_from_file_location` +
`module_from_spec` + `exec_module`, returning the module, and raising `GateImportRefused` when the
path cannot be imported as a module at all (a non-`.py` suffix) instead of returning a default or
crashing with `AttributeError` on a `None` spec. A missing file keeps raising `FileNotFoundError`
from `exec_module` - the helper does not convert one refusal into another.

`citest.run_gate(script, *arguments, cwd=None, env=None)` is the one gate-CLI shape:
`[sys.executable, str(script), *arguments]`, `capture_output=True`, `text=True`, `check=False`. The
exit code and both streams are the child's, unmodified; a non-zero exit is returned, never raised
and never synthesized.

Two keyword-only parameters were added to that signature because the measured consumers needed
them and no other shape was left in the tree: `timeout=None` (the receipt publisher suite bounds its
CLI at 10 s, `test-linux-v2-control-receipt.py:76`) and `encoding=None` (the OMP usage extractor
suite pins UTF-8 decoding while its CLI also emits non-ASCII rows, `test-model-routing-omp-usage.py:94`;
`text=encoding is None` keeps the default decoding byte-identical for every other caller).
Both defaults reproduce the untouched `subprocess.run` defaults, so no consumer's bytes change.

`citest.git(repository, *arguments, input=None)` runs `git -C <repository>` with
`capture_output=True`, `check=True` (a failing command raises `subprocess.CalledProcessError`, the
same refusal class the replaced `subprocess.check_output` calls raised), `b""` as the default stdin
(the semantics `check_output(input=None)` had) and the
`GIT_DIR`/`GIT_WORK_TREE`/`GIT_INDEX_FILE`/`GIT_OBJECT_DIRECTORY`/`GIT_COMMON_DIR`/
`GIT_ALTERNATE_OBJECT_DIRECTORIES` variables dropped, mirroring `revision_scope.run_git`; the
config-redirect family (`GIT_CONFIG`, `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_*`,
`GIT_CONFIG_VALUE_*`) is dropped as well, while `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM`/
`GIT_CONFIG_NOSYSTEM` are deliberately kept because suites pin configuration through them (fix
stage, findings STD-A03/STD-A04). A failing command's captured stderr is written to the suite's
stderr before the exception is re-raised, restoring the diagnostics the converted `check_call`
sites used to stream.

`citest.git_text` is that call plus `decode("ascii").strip()`. `citest.write_bytes(path, data, *,
mode=None)` creates the parent, writes the bytes and returns them, applying `mode` after the write;
`citest.write_json(path, value, *, sort_keys=False, mode=None)` is `json.dumps(..., indent=2,
sort_keys=...) + "\n"` through it. The two parameterizations are exactly what the two measured
test-side copies needed (unsorted + `0o600` + bytes in the exporter suite; `sort_keys=True` + a
returned `Path` in the evaluation suite), so neither consumer's bytes change.

`citest.strict_json(text, source)` parses one document under an `object_pairs_hook` that refuses a
repeated key, `parse_float` refusing a non-finite number and `parse_constant` refusing `NaN`/
`Infinity`, raising `StrictJsonRefused` with the source named. It is the single *test-side* strict
reader; the three production copies (`model-routing-omp-usage.py:84` `UsageRefusal`,
`omo-native-evidence.py:65` `WitnessError`, `omo-control-native-evidence.py:243` `Rejected` with
redacted text) and the canonical `model-routing-evaluation.py:462` remain untouched - unifying them
changes evidence-reader behaviour owned by W5-11/W5-12 (contract clause 4). Consequence recorded
rather than hidden: inside this slice `strict_json` has no consumer besides its own suite, because
every suite that reads tool-written JSON today reads it through `json.loads`; wiring it into
`test-model-routing-evaluation.py`'s readers would change that frozen suite's refusal class and is
W5-12's file.

`FixtureRepo` is the one fixture-repository shape. `REPOSITORY_PREFIX` names the temporary root
(each consumer keeps its own prefix) and `REPOSITORY_DIRECTORY` selects between the two measured
layouts (`root/repository` for the citation, record-cleanliness and evaluation suites; the root
itself for the source-subject and no-legacy suites). `git`/`git_text` take an optional `cwd` so the
evaluation suite's clone-directory reads go through the same isolation. `write` accepts a relative
path (resolved under the repository) or an absolute one, creates the parents, writes UTF-8 bytes
without newline translation and returns the path; `commit(message)` stages everything, commits
quietly and returns the new head. The repository-local `user.name`/`user.email`/`commit.gpgsign`/
`core.autocrlf` configuration is what the mutation (b) run removes.

Reviewable trade-off: `FixtureRepo` has no `run` method. The contract lists `run` among the methods
the helper "owns", and the two readings are (a) a gate-CLI runner and (b) the repository's own git
runner. (a) is `citest.run_gate`, which every migrated suite already calls; a `FixtureRepo.run`
delegate would be a wrapper-only layer that adds no behaviour (the repository's AGENTS.md forbids
those). (b) is `FixtureRepo.commit`/`git`. The reading is recorded here instead of guessed silently.

## Measured duplication baseline (before)

| Measure | Command (repository root, HEAD `12de9428`) | Before |
| --- | --- | --- |
| loader blocks in `scripts/ci` | `grep -h spec_from_file_location scripts/ci/*.py \| wc -l` | 22 across 19 files (16 in the 15 owned suites, 2 in the unowned evaluation tool, 2 in W3-01's `test-ci-changed-path-classifier.py`, and 1 in each of the two #137-excluded files) |
| `def run_gate`/`def run_check` in the owned suites | `grep -c "def run_gate\|def run_check" scripts/ci/test-*.py` | 10 definitions in 8 files |
| git-fixture `setUp` bodies | `grep -l "def git(" scripts/ci/test-*.py` | 8 (the six migrated consumers: citation, record-cleanliness, evaluation, source-subject, credentialed provider witness, no-legacy routes; plus W3-01's `test-ci-changed-path-classifier.py` and `test-omo-control-native-evidence.py`'s recorded exclusion) |
| `write_json`/`write_log` bodies | `grep -n "def write_json\|def write_log" scripts/ci/*.py` | 3 (evaluation suite, exporter suite's `write_json` and `write_log`) |
| suite files | `ls scripts/ci/test-*.py \| wc -l` | 25 at this HEAD (24 at the contract's `0fa9b705`; W3-01 added `test-ci-changed-path-classifier.py`), becoming 27 with this slice's two new suites |
| CI python-suite steps | `grep -cE "(sudo )?python3? (\.\./)?scripts/ci/test-" .github/workflows/ci.yml` | 25 steps at this HEAD (the contract re-measured the plan's "37 suites" as steps, not files; no comment in `ci.yml` claims a count this slice changes) |

## Migration table (deleted block -> helper call)

Line ranges are the HEAD ranges of the deleted block. "shape" names the reason a one-line local
delegate stayed; every other wrapper was deleted and its call sites now call `citest` directly.

| Suite | Deleted at HEAD | Helper call | shape |
| --- | --- | --- | --- |
| `test-assert-exunit-json.py` | `import importlib.util` L4, `import subprocess`/`sys` L7-8; loader L13-15; `run_gate_file` body L57-62 | `GATE = citest.load_gate(SCRIPT, "assert_exunit_json")`; `run_gate_file` -> `citest.run_gate(SCRIPT, str(events), *arguments)`; `run_gate` (temp-file write) and `run_stream` kept | events path |
| `test-assert-folded-gates.py` | L4/L6-7 imports; loader L11-13; `run_check` L26-33 (pure passthrough) | `FOLDED = citest.load_gate(SCRIPT, "assert_folded_gates")`; `run_check` deleted, 4 call sites -> `citest.run_gate(SCRIPT, *arguments)` | - |
| `test-assert-playwright-json.py` | L6-7 imports; `gate_command` L60-66 prefix; `run_gate`/`run_gate_path` bodies L75-80/L83-88 | `gate_arguments(path, files)` builds the flags only; both wrappers -> `citest.run_gate(SCRIPT, *self.gate_arguments(path, files), *arguments)` | flags builder |
| `test-assert-runner-matches-target.py` | L4/L8-9 imports; loader L13-15; `run_guard` body L27-33 | `GUARD = citest.load_gate(SCRIPT, "assert_runner_matches_target")`; `citest.run_gate(SCRIPT, "--target", target, *arguments, env=environment)` (environment construction kept) | `env=` |
| `test-check-citation-existence.py` | `import tempfile` L19; fixture `setUp` L34-42; base commit L52-54; `git`/`git_text`/`write`/`commit` L58-73; `run_gate` body L76-81; `# --` dividers L55-57/L87-88 | `FixtureRepo` mixin (`REPOSITORY_PREFIX = "citation-existence-"`); `self.base = self.commit("fixture base")`; `run_gate` -> `citest.run_gate(SCRIPT, "--repo", str(self.repo), *arguments)`; `cite_in_runbook` kept | `--repo` |
| `test-check-lockfile-registry.py` | `import importlib.util` L20; loader L29-31; `run_check` L194-201 (pure passthrough) | `CHECK = citest.load_gate(SCRIPT, "check_lockfile_registry")`; `run_check` deleted, 19 call sites -> `citest.run_gate(SCRIPT, ...)` | - |
| `test-check-record-cleanliness.py` | `import tempfile` L15; `setUp` L24-32; base commit L38-40; `git`/`git_text`/`write`/`commit` L44-59; `run_gate` body L62-67; dividers L41-43/L69 | `FixtureRepo` mixin (`REPOSITORY_PREFIX = "record-cleanliness-"`); `self.base = self.commit("fixture base")`; `run_gate` -> `citest.run_gate(SCRIPT, "--repo", str(self.repo), *arguments)` | `--repo` |
| `test-go-test-json-gate.py` | `import importlib.util` L4; loader L14-16; `run_gate` L88-93; `run_boundary` L353-358 | `GATE = citest.load_gate(SCRIPT, "go_test_json_gate")`; `citest.run_gate(SCRIPT, *options, "--", *command)`; `citest.run_gate(GATE.BOUNDARY_SCRIPT, *command)` | `--` separator; boundary script |
| `test-linux-v2-go-events.py` | L6-7 imports; `run_gate` L19-24; raw invocation L89-94 | `citest.run_gate(SCRIPT, str(path), *arguments)` (temp jsonl kept); raw call -> `citest.run_gate(SCRIPT, str(path), "TestWitness")` | temp jsonl |
| `test-linux-v2-source-subject.py` | class bases L15; `setUp` L17-24; source write + commit L25-28; `git` L30-31; `invoke` body L33-39 | `FixtureRepo` mixin (`REPOSITORY_PREFIX = "symmetry-subject-"`, `REPOSITORY_DIRECTORY = None`); `self.commit_sha = self.commit("fixture")`; `invoke` -> `citest.run_gate(SCRIPT, str(self.repo), commit or self.commit_sha)`; `self.repository` -> `self.repo` (5 sites), the old `self.commit` attribute -> `self.commit_sha` (4 sites, so the attribute cannot shadow `FixtureRepo.commit`) | renamed attribute |
| `test-model-routing-evaluation.py` | `import importlib.util` L8, `import tempfile` L16; `load_runner_module` L26-30; class L146; `setUp` L147-162; `git`/`git_text`/`write` L166-174; `write_json` L177-178; `cli` L185-191; copied-runner runs L1119-1124, L1134-1139; `run_copy` L3752-3758 + 3 call sites | `citest.load_gate(path, "model_routing_evaluation")`; `FixtureRepo` mixin (`"model-routing-eval-"`); `write_json` adapter (`citest.write_json(path, value, sort_keys=True)`, returns the `Path`); `cli` -> `citest.run_gate(SCRIPT, *arguments, cwd=self.root)`; `run_copy` deleted as a pure passthrough | `cwd=`; sorted + returned `Path` |
| `test-model-routing-omp-usage.py` | `import importlib.util` L11; `load_runner_module` L76-82 and `load_usage_module` L85-90 bodies; `run_arguments` L94 | both loaders -> `citest.load_gate`; `run_arguments` -> `citest.run_gate(SCRIPT, *arguments, encoding="utf-8")` | `encoding=` |
| `test-model-routing-source-subject.py` | `import importlib.util` L10; `load_tool` body L21-24 | `return citest.load_gate(SCRIPT, "model_routing_source_subject")` | wrapper name kept |
| `test-omo-control-native-evidence.py` | `import importlib.util` L12; loader L22-24 | `EVIDENCE = citest.load_gate(SCRIPT, "omo_control_native_evidence")` | - |
| `test-omo-native-evidence.py` | `import importlib.util` L4; loader L17-19 | `WITNESS = citest.load_gate(SCRIPT, "omo_native_evidence")` | - |
| `test-opencode-v2-support-witness.py` | `import importlib.util` L7; loader L19-23 | `witness = citest.load_gate(pathlib.Path(__file__).with_name("opencode-v2-support-witness.py"), "witness")` | lowercase binding kept |
| `test-rootless-compose-owner-claim.py` | `import importlib.util` L5; loader L15-17 | `claim = citest.load_gate(SCRIPT, "rootless_claim")` | - |
| `test-rootless-compose-transition-proxy.py` | `import importlib.util` L7; in-test loader L19-21 | `proxy = citest.load_gate(SCRIPT, "transition_proxy_gzip_test")` | in-test loader |
| `test-linux-v2-evidence.py` | `load_exporter` L52-55; `write_json` L363-368; `write_log` L370-375; `run_export` L377-378 | `load_exporter` -> `citest.load_gate(SCRIPT, "export_linux_v2_redacted_evidence")`; `write_json`/`write_log` -> `citest.write_json(..., mode=0o600)` / `citest.write_bytes(..., mode=0o600)`; `run_export` -> `citest.run_gate(SCRIPT, *arguments)` | `0o600` mode + returned bytes |
| `test-no-legacy-routes-audit.py` | `setUp` L62-72; `tearDown` L74-75; `git` L77-83; `write` L85-89 | `FixtureRepo` mixin (`REPOSITORY_PREFIX = "legacy-route-audit-"`, `REPOSITORY_DIRECTORY = None`); `plant`/`copy_from_repo`/`track_everything`/`audit` kept | audit is a `bash` invocation |
| `test-linux-v2-control-receipt.py` | `publish` def L70 and its body L71-77 | `citest.run_gate(SCRIPT, str(self.source), str(self.directory), self.name, str(uid), str(gid), timeout=10)` | `timeout=` |
| `test-credentialed-provider-witness.py` | loader L23-27; `setUp` L31-58 (fixture repo and the inline `-c` commit); `git` L73-74 | loader -> `citest.load_gate(ROOT / "scripts/ci/credentialed-provider-witness.py", "credentialed_provider_witness")`; `FixtureRepo` mixin (`"symmetry-witness-negative-"`, `REPOSITORY_DIRECTORY = None`); the commit -> `self.commit("local fixture")` | identity config is repo-local now, with the values changing from the inline `-c` `Witness <witness@example.invalid>` to `Fixture <fixture@example.invalid>` |

## Control-flow preservation audit (contract lesson 32)

Every `check=True/False`, `returncode` comparison, `assertIn`/`assertNotIn` on a gate's stderr/stdout,
`except` clause and default inside a deleted block, per suite:

| Suite | Deleted-block control flow | Preserved by the helper |
| --- | --- | --- |
| the loader-only suites (`test-model-routing-source-subject.py`, `test-omo-native-evidence.py`, `test-omo-control-native-evidence.py`, `test-opencode-v2-support-witness.py`, `test-rootless-compose-owner-claim.py`, `test-rootless-compose-transition-proxy.py`, `test-model-routing-omp-usage.py`'s two loaders) | nothing but the `spec_from_file_location`/`module_from_spec`/`exec_module` sequence and the module name | same name, same module object; the only new path is `GateImportRefused` where the old code would have raised `AttributeError` on a `None` spec (unreachable for a `.py` path) |
| the gate-CLI bodies (`assert-exunit-json`, `assert-folded-gates`, `assert-playwright-json`, `assert-runner-matches-target`, `go-test-json-gate`, `linux-v2-go-events`, `lockfile-registry`, `record-cleanliness`, `citation-existence`, and `model-routing-evaluation`'s `cli`/`run_copy`/copied-runner runs) | `check=False` in every deleted body; `env=environment` in `run_guard`; `cwd=str(self.root)` in `cli`; `--`/`--target`/`--repo`/events-path argument order; `capture_output=True, text=True` | `run_gate` hard-codes `capture_output=True, text=True, check=False` and takes `cwd`/`env`/`timeout`/`encoding` through; argument order is rebuilt identically (`[sys.executable, str(script), *arguments]`) |
| the git-fixture bodies (`citation`, `record-cleanliness`, `evaluation`, `source-subject`, `no-legacy`, witness) | `subprocess.check_output(..., stderr=subprocess.PIPE)` (raises on failure, keeps stderr out of the suite's stream; citation and record-cleanliness forward an explicit `input=None`); per-consumer commit sequences (`git add -A` + `commit --quiet -m` + `rev-parse HEAD` in citation, record-cleanliness and evaluation; `git add source.txt` + commit + `rev-parse` in source-subject; `git add .` plus an inline `-c user.name/... commit` in the witness suite; no commit in no-legacy, which stages through `track_everything`'s `add --all --force`); `write_text` parent creation; `tearDown` cleanup | `citest.git` uses `capture_output=True, check=True` (same `CalledProcessError`), an empty-stdin default and a stderr echo on failure; `FixtureRepo.commit` stages `-A`, commits quietly and returns the head (equivalent for the single-file fixtures - source-subject's `add source.txt` becomes `add -A`); `write` creates parents; `addCleanup` replaces `tearDown`. Known, plan-recorded deltas: fixture bytes are UTF-8/LF instead of host-translated, the repository-selecting and config-redirect environment variables are dropped, and the witness identity values change from `Witness <witness@example.invalid>` to `Fixture <fixture@example.invalid>` |
| `test-model-routing-omp-usage.py` (`run_arguments`) | `encoding="utf-8"` with `check` omitted | `run_gate(..., encoding="utf-8")` sets `text=False, encoding="utf-8"` (subprocess enables text mode), the same object the original produced |
| `test-linux-v2-control-receipt.py` (`publish`) | `text/capture/check=False` plus `timeout=10` | `run_gate(..., timeout=10)` |

## The byte-frozen evidence method (D1)

D1 freezes every gate's `stdout`/`stderr`/exit code, so the acceptance evidence is a byte diff of
the *same* child invocations before and after the harness move. Method (the driver is a scratch
script kept out of the repository; every rule it applies is stated here so the diff is
reproducible):

1. a scratch driver runs one suite in-process (`runpy.run_path(..., run_name="__main__")` with the
   suite's directory inserted at `sys.path[0]`, exactly as `python3 scripts/ci/<suite>.py` does),
   patching `subprocess.run`/`check_output`/`check_call`/`Popen` at the outermost call only;
2. every child invocation is recorded as `{kind, argv, cwd, env-markers, text, input, pass_fds,
   shell, returncode, stdout, stderr, raised}`; the suite's own exit code, stdout and stderr are the
   driver's return value and the shell's redirected files;
3. temporary roots are tracked through `tempfile.mkdtemp` and every recorded string has them
   replaced with `<ROOT n>` (both separators), so the fixtures' random directory names do not
   appear in the diff;
4. the same pass runs on the pre-change tree (captured before the first edit) and on the post-change
   tree; `diff` over the normalized records is the D1 table.

Comparison scope: the recorded streams are decoded text (`text=True`, universal newlines), so the
diff binds equality of decoded stdout/stderr, exit code, argv, `cwd` and the recorded fields - not
of raw bytes; `env` is compared through the recorded markers, and `check`/`capture_output`/
`encoding`/`timeout` are not among the compared fields. The one class this leaves open - CRLF vs LF
in fixture bytes - is decision 1's recorded trade-off, and the gate oracle is Linux.

Children are *not* patched (the driver never sets `PYTHONPATH`), so a gate's own environment and
argv are what a plain run would give it. The driver's only behavioural difference from
`python3 <suite>` was fixed during the baseline: a `SystemExit("message")` also prints its message
to stderr, as CPython does. `PYTHONDONTWRITEBYTECODE=1` keeps `__pycache__` out of the tree.

## Platform limits of the local gate run

Measured on this Windows host at the baseline (pre-change), the same three suites the repository's
previous slices recorded:

| Suite | Windows host | Linux (WSL Ubuntu-24.04) |
| --- | --- | --- |
| `test-linux-v2-control-receipt.py` | exit 1 in 0 s: the suite's own guard refuses (it requires Linux root: `sys.platform != "linux" or os.geteuid() != 0`) | the real gate (run as root) |
| `test-linux-v2-evidence.py` | `Ran 36 tests`, `FAILED (failures=55, errors=5)`: the exporter calls `os.getuid()` | the real gate |
| `test-no-legacy-routes-audit.py` | `Ran 6 tests`, `FAILED (failures=11)`: `/bin/bash` is WSL's bash and the planted path is a Windows path | the real gate (with `rg` absent from `PATH`, the audit's `grep` fallback) |
| `test-credentialed-provider-witness.py` | `Ran 5 tests`, `FAILED (errors=2)`: `mix` is a Windows `.cmd` shim that `subprocess` cannot execute | the real gate needs the control job's `MIX_ENV=test` dependency preparation |

These four suites therefore have their real gate runs in WSL, and their pre/post byte diff is taken
in the same environment on both sides; the Windows-host results above are recorded as the
environment, not as the contract's gate.

The WSL side of that table was measured before the migration (as root, `python3 scripts/ci/<suite>.py`
from the repository mount): `test-linux-v2-evidence.py` exit 0, `Ran 36 tests in 52.225s`, `OK`;
`test-no-legacy-routes-audit.py` exit 0 with the clean `PATH`, `Ran 6 tests in 2.826s`, `OK`;
`test-linux-v2-control-receipt.py` exit 0, `Ran 9 tests in 2.819s`, `OK`;
`test-credentialed-provider-witness.py` `Ran 5 tests in 13.747s`, `FAILED (failures=1)` - the one
failure is the real-control test that needs Hex and the control dependencies, which this
environment cannot install: `mix local.hex --force` fails with
`httpc request failed with: {:failed_connect, ... repo.hex.pm ...}` / `Could not install Hex because
Mix could not download metadata at https://repo.hex.pm/installs/hex-1.x.csv` (no network path to
hex.pm). The other four tests pass. That refusal is pre-existing and identical before and after the
migration; the post-change run repeats it with the same count, so it is recorded as an environment
limit, not as the contract's gate.

## Decisions and trade-offs

1. **Fixture bytes are written without newline translation.** `FixtureRepo.write` uses
   `write_bytes(content.encode("utf-8"))`. Four of the six git-fixture consumers already had to be
   byte-exact (the evaluation suite's `write`, the source-subject suite's blob hash), and a fixture
   whose bytes depend on the host's `os.linesep` is exactly the class of defect this consolidation
   removes: the CI runs on Linux, so LF is what the gate oracle sees. The measured cost is the D1
   table; a byte that changes with this decision is visible there and none did.
2. **Thin local delegates stay where the shape differs.** Deleted: every wrapper whose body was the
   same call with the same arguments (`test-assert-folded-gates.py`, `test-check-lockfile-registry.py`,
   `test-linux-v2-go-events.py`'s call sites). Kept, one line each: the `--repo`-prefixed gate call
   (citation, record-cleanliness), the temp-file-then-run shapes (exunit, playwright, go-test-json), the
   `env=` shape (runner-matches-target), the `--` separator (go-test-json), the `cwd=self.root` shape
   (evaluation suite) and the `--repo`/`0o600`-mode adapters of the two largest suites. Each is named
   in the migration table; none of them contains the replaced plumbing.
3. **`write_json` adapters instead of call-site rewrites.** The evaluation suite's `write_json`
   returned a `Path` (8 call sites, four of which pass the result straight into a CLI argument) and
   the exporter suite's returned the bytes it wrote (30 call sites, all `original = self.write_json(...)`).
   Both keep a two-line adapter whose body is the helper call plus the return the call site expects;
   the encoding, indentation, newline, parent creation, `sort_keys` and `chmod` all live in the
   helper now. Rewriting 38 call sites inside frozen suites buys nothing and risks the bytes D1
   freezes.
4. **`strict_json` lands test-side only** (public shape above), with its own duplicate-key test;
   the production unification is W5-11/W5-12's, per clause 4.
5. **`receipt` is not a helper.** The three measured `receipt()` copies are unrelated payloads that
   share a name: the exporter suite's fixed stop-receipt dict (`legacy_v1_no_v2_proof`,
   `owner_kind: linux_helper`), the evaluation suite's CLI driver for the runner's `receipt`
   subcommand, and the opencode witness suite's builder for a witness record. None is harness
   plumbing; all three stay, and their remaining similarity is the subject fixtures' own vocabulary.
6. **`test-omo-control-native-evidence.py`'s inline git fixture stays.** Its module-level
   `git(directory, *arguments)` is `text=True` + `strip()` + `AssertionError`, used by one test that
   builds a Go-toolchain fixture inside the test body; converting it would change that refusal class
   and give a class that needs no repository a `setUp`. It is not one of the six measured
   git-fixture variants and the contract's own re-derivation does not name it.
7. **Comment policy.** The new modules carry no docstrings, no banners and no what-comments; the
   only comment added by this slice is the two-line why beside `FixtureRepo`'s configuration block
   (a host's global identity/signing/eol configuration must not decide fixture bytes) plus the
   two-line why beside the dropped `GIT_*` variables in `citest.git` - both are the "line-level
   blast-radius hazard" class the policy allows. In the migrated files the `# -- helpers ---`
   dividers go with the sections they label.
   The migrated files lose the dividers that bracketed deleted blocks: the helpers/tests pairs in
   the citation and record-cleanliness suites and the environment-helpers divider inside the
   evaluation suite's migrated hunk (fix stage, finding STD-A06); the evaluation suite's
   fixture-builders and tests dividers sit outside the touched hunks and stay. No docstring is
   removed: the two that survive on the loader shims (`load_runner_module`, `load_usage_module`)
   state why the module is imported in-process rather than restating the call, which the policy
   keeps; `test-model-routing-source-subject.py`'s `load_tool` shim has no docstring and had none.

## Evidence plan (contract Gates, all foreground and serialized)

```
python3 scripts/ci/test-<each owned suite>.py        # 18 on this host, 4 in WSL Ubuntu-24.04 as root
python3 scripts/ci/test-citest.py                    # + the two new suites (E7-wired)
python3 scripts/ci/test-gitfixture.py
python3 scripts/ci/check-citation-existence.py ...   # at the slice head, recorded in the evidence section
python3 scripts/ci/check-lockfile-registry.py        # unchanged sanity
ruff check scripts/ci                                # 0.17.5, the config's required-version
ruff format --check scripts/ci
git diff --check
```

The citation gate binds revisions and this stage leaves the tree uncommitted, so its slice-head run
uses the same dangling-snapshot technique W3-01 recorded: a temporary index
(`GIT_INDEX_FILE=<temp>`; `git read-tree HEAD && git add -A && git write-tree && git commit-tree
<tree> -p HEAD`) builds a commit of the working tree, and the gate then runs `--base HEAD --head
<snapshot>` (the increment's own documents) and `--base <root commit> --head <snapshot>` (the full
audited corpus, the §11 requirement for a moving slice). Neither the real index nor a branch is
touched.

Platform routing: `test-linux-v2-control-receipt.py`, `test-linux-v2-evidence.py`,
`test-no-legacy-routes-audit.py` and `test-credentialed-provider-witness.py` run in WSL (see
"Platform limits"), every other suite on this Windows host; the two long suites
(`test-model-routing-evaluation.py` >580 s, `test-check-record-cleanliness.py` ≈48 s, plus
`test-check-citation-existence.py` ≈132 s) run alone, foreground, with a 3000 s budget.

## Out of scope

- The production `strict_json`/`parse_json`/`require_object` copies and the source-subject
  implementations (W5-11/W5-12).
- `scripts/ci/test-ci-changed-path-classifier.py`, `scripts/ci/check-citation-existence.py`
  (W3-01's) and `scripts/ci/test-model-routing-record-sanitize{,-json}.py` (#137's): untouched.
- The three dispatch-only tools' CLI fixtures; pytest migration (D11 keeps unittest); `ci.yml`
  restructuring (W6-01); the gate tools' own logic and messages; `scripts/ci/*.exs` formatting;
  anything under `daemon/**`, `control/**`, `frontend/**`, `browser/**`.

## Evidence (measured 2026-11-22, on the uncommitted tree)

### Duplication after

| Measure | Command | Before | After |
| --- | --- | --- | --- |
| loader blocks, whole directory | `grep -h spec_from_file_location scripts/ci/*.py \| wc -l` | 22 | 7 - one inside `citest.load_gate` itself, two in the unowned evaluation tool, two in W3-01's `test-ci-changed-path-classifier.py`, one in each of the two #137-excluded files. **Zero in the 22 owned suites.** |
| `def run_gate`/`def run_check` in the owned suites | `grep -c ... scripts/ci/test-*.py` | 10 definitions in 8 files | 8 thin local delegates in 6 files (`test-assert-exunit-json.py` two: temp file + events path; `test-assert-playwright-json.py` two: temp report + explicit path; `test-check-citation-existence.py` and `test-check-record-cleanliness.py`: `--repo`; `test-go-test-json-gate.py`: `--` separator; `test-linux-v2-go-events.py`: temp jsonl; the three temp-file wrappers are multi-line, the other five are one-line calls). No delegate body contains `subprocess.run` (`grep -A4 "def run_gate" ... \| grep -c subprocess.run` -> 0). `test-linux-v2-evidence.py` has no local delegate left: its 33 `run_export` call sites call `citest.run_gate(SCRIPT, ...)` directly (apply stage, finding F3). |
| raw `subprocess.run` gate invocations | `rg -U 'subprocess\.run\(\s*\[[^]]*str\((SCRIPT\|GATE\.BOUNDARY_SCRIPT)\)' scripts/ci/test-*.py` | 24 matching lines in 13 of the 22 owned suites at `b25e34ae` (every deleted delegate body plus every raw call site; 30 lines in 15 files including the W3-01/#137 files). The apply stage's F4 subset is 8 raw call sites - citation 3, record-cleanliness 2, go-test-json-gate 3 - seven converted and the `pass_fds` probe kept | 1 - `test-go-test-json-gate.py`'s `pass_fds` descriptor-boundary probe, which must stay raw (apply stage, finding F4); the two #137-excluded suites keep their own runs and are untouched. |
| files defining a git fixture | `grep -l "def git(" scripts/ci/test-*.py` | 8 | 2 - `test-ci-changed-path-classifier.py` (W3-01's) and `test-omo-control-native-evidence.py` (recorded exclusion); the six migrated consumers use `FixtureRepo` (`grep -l FixtureRepo scripts/ci/test-*.py` -> citation, record-cleanliness, evaluation, source-subject, no-legacy, witness, test-gitfixture). |
| `write_json`/`write_log` bodies | `grep -n "def write_json\|def write_log" scripts/ci/test-*.py` | 3 bodies (evaluation, exporter x2) with their own mkdir/dumps/encode/chmod | 3 one-line adapters: `citest.write_json(path, value, sort_keys=True)` (returns the `Path`), `citest.write_json(path, value, mode=0o600)`, `citest.write_bytes(path, ..., mode=0o600)`. |
| suites | `ls scripts/ci/test-*.py \| wc -l` | 25 at this HEAD | 27 (+`test-citest.py`, `test-gitfixture.py`, both wired into `ci.yml`'s daemon step next to the other suite invocations) |
| CI python-suite steps | `grep -cE "(sudo )?python3? (\.\./)?scripts/ci/test-" .github/workflows/ci.yml` | 25 | 27 (`ci.yml:1609-1610` are the two new suite invocations) |
| LOC | `wc -l` over the 22 suites the migration table names, `e0782105` vs the delivered tree | 11,185 | 10,901 (-284) on the fix-stage tree; the two new modules and their two suites add 601 (`citest.py` 122, `testsupport/gitfixture.py` 45, `test-citest.py` 316, `test-gitfixture.py` 118), so `scripts/ci` nets +317 (`git diff --numstat b25e34ae -- scripts/ci` -> +843/-526). At the reviewed revision `89c68e05` the same measures were 10,903 (-282), 509 and +227 (`git diff --numstat b25e34ae..89c68e05 -- scripts/ci` -> +751/-524); naming `e0782105` as the numstat base does not reproduce the slice delta because it also contains #221's two `scripts/ci` files (fix stage, finding SPEC-A04). The figures first recorded here (10,747 -> 10,548, net +211) were taken on an intermediate working tree and do not reproduce on a committed revision (`10b03cc0` measures 11,185 -> 10,929 over the same set, `+567/-413` plus the 410 new-file lines). D2 makes LOC report-only; the -284 is the duplication the contract asked to remove plus the apply-stage plumbing removals and the fix-stage test hardening, and the +601 is the helper plus the suites that kill its mutants. |

### Byte-frozen gate outputs (D1)

Every gate invocation a migrated suite drives was captured before and after with the method above and
compared after normalizing only run-varying tokens (the random temporary-root name, 40- and 12-hex
git identifiers of freshly created fixture repositories, and unittest's elapsed-time line). "gates"
counts the recorded `python … scripts/ci/<gate>.py` children; the diff compares argv, `cwd`, argv
carried `env` markers, `text`, `input`, `pass_fds`, `shell`, exit code, `stdout` and `stderr` field
by field.

| Suite | exit | gate invocations | gate differences | other children (git/bash/etc.) | new fixture config lines |
| --- | --- | --- | --- | --- | --- |
| `test-assert-exunit-json.py` | 0 = 0 | 23 -> 23 | 0 | 0 -> 0 | 0 |
| `test-assert-folded-gates.py` | 0 = 0 | 4 -> 4 | 0 | 0 -> 0 | 0 |
| `test-assert-playwright-json.py` | 0 = 0 | 17 -> 17 | 0 | 0 -> 0 | 0 |
| `test-assert-runner-matches-target.py` | 0 = 0 | 8 -> 8 | 0 | 0 -> 0 | 0 |
| `test-check-citation-existence.py` | 0 = 0 | 54 -> 54 | 0 | 538 -> 587 | 196 |
| `test-check-lockfile-registry.py` | 0 = 0 | 19 -> 19 | 0 | 1 -> 1 | 0 |
| `test-check-record-cleanliness.py` | 0 = 0 | 21 -> 21 | 0 | 210 -> 229 | 76 |
| `test-credentialed-provider-witness.py` (Windows, environment failure) | 1 = 1 | 0 -> 0 | 0 | 27 -> 43 | 16 |
| `test-go-test-json-gate.py` | 0 = 0 | 20 -> 20 | 0 | 0 -> 0 | 0 |
| `test-linux-v2-control-receipt.py` (Windows, suite guard) | 1 = 1 | 0 -> 0 | 0 | 0 -> 0 | 0 |
| `test-linux-v2-evidence.py` (Windows, exporter `os.getuid`) | 1 = 1 | 112 -> 112 | 0 | 0 -> 0 | 0 |
| `test-linux-v2-go-events.py` | 0 = 0 | 15 -> 15 | 0 | 0 -> 0 | 0 |
| `test-linux-v2-source-subject.py` | 0 = 0 | 6 -> 6 | 0 | 35 -> 35 | 16 |
| `test-model-routing-omp-usage.py` | 0 = 0 | 91 -> 91 | 0 | 0 -> 0 | 0 |
| `test-model-routing-source-subject.py` | 0 = 0 | 0 -> 0 | 0 | 0 -> 0 | 0 |
| `test-no-legacy-routes-audit.py` (Windows, `/bin/bash` path) | 1 = 1 | 0 -> 0 | 0 | 28 -> 52 | 24 |
| `test-omo-control-native-evidence.py` | 0 = 0 | 0 -> 0 | 0 | 8 -> 8 | 0 |
| `test-omo-native-evidence.py` | 0 = 0 | 0 -> 0 | 0 | 6 -> 6 | 0 |
| `test-opencode-v2-support-witness.py` | 0 = 0 | 0 -> 0 | 0 | 1 -> 1 | 0 |
| `test-rootless-compose-owner-claim.py` | 0 = 0 | 0 -> 0 | 0 | 1 -> 1 | 0 |
| `test-rootless-compose-transition-proxy.py` | 0 = 0 | 0 -> 0 | 0 | 0 -> 0 | 0 |
| `test-model-routing-evaluation.py` | 0 = 0 | 686 -> 686 | 0 | 718 -> 718 | 320 |

The "other children" deltas are the fixture configuration `FixtureRepo` now pins: one
`git config core.autocrlf false` line per created fixture repository (+49 for 49 tests, +19, +24 for
the six-test audit suite), the witness suite's four repo-local config lines per test in place of its
inline `git -c user.name=… commit` (whose recorded identity values change from `Witness
<witness@example.invalid>` to `Fixture <fixture@example.invalid>`), and the three commands per
fixture repository that moved inside the helper. The apply stage's F5 conversions also changed
three git argv shapes without touching any gate child: the source-subject setUp's `git add
source.txt` became `FixtureRepo.commit`'s `git add -A` (equivalent on that single-file fixture),
citation's `git clone` gained the helper's `-C <root>` prefix, and the orphan/other-repository
setups moved from `check_call([...], cwd=X)` to `git -C X …` (the child's cwd is inherited instead
of `X`, equivalent because every one of those commands is repository-relative or given absolute
arguments). Apart from those shapes and the fixture-configuration lines, no child invocation
changed; gate children are identical by construction, and the fix stage's `citest.git` changes
(empty stdin by default, the config-redirect family dropped, captured stderr echoed on failure)
reach git children only.
The two new suites have no pre-change counterpart - they did not exist before this slice - so they
are evidence through their own green runs and the mutants they kill, not through a byte diff.

Two normalizations need naming because they touch real bytes: (i) 677 of the evaluation suite's 686
gate invocations pass a `cwd` that the pre-change call stringified (`cwd=str(self.root)`) and the
helper passes as a `pathlib.Path`; the differ treats the two recorded forms as equal, and the
equivalence of `subprocess.run(cwd=Path)` and `subprocess.run(cwd=str(Path))` is Python's documented
`os.fspath` handling of a path-like. Everything else about those invocations (argv, streams, exit
code) compared field by field. (ii) The gate streams of every suite that creates a fixture
repository carry identifiers of that freshly created repository (its commit ids, and sha256 digests
computed over documents that embed them), so those tokens are normalized; that they vary per run
independently of this change is measured: two runs of one evaluation test on the *same* tree differ
in the same invocation set, and 0 invocations differ after the same normalization.

The four suites whose real gate is Linux were captured in WSL on both sides (root, repository
mount, `test-no-legacy-routes-audit.py` with the clean `PATH`):

| Suite (WSL) | exit | gate invocations | gate differences | other children | new fixture config lines |
| --- | --- | --- | --- | --- | --- |
| `test-linux-v2-evidence.py` | 0 = 0 | 119 -> 119 | 0 | 0 -> 0 | 0 |
| `test-no-legacy-routes-audit.py` | 0 = 0 | 0 -> 0 | 0 | 34 -> 58 | 24 |
| `test-linux-v2-control-receipt.py` | 0 = 0 | 10 -> 10 | 0 | 0 -> 0 | 0 |
| `test-credentialed-provider-witness.py` | 1 = 1 | 0 -> 0 | 0 | 56 -> 72 | 16 |

### Mutation reds (measured on the shipped bytes: each mutant applied with `apply_patch`, the
suites run foreground, the file restored from a byte-identical backup and verified with `md5sum`:
`citest.py` `65e11bfc389eaa2f297a270bac55c640`, `testsupport/gitfixture.py`
`2b35736d5360c6bf8bfe7b821983d087`, both `OK` after every restore. Re-run at the apply stage against
copies of `scripts/ci` whose bytes equal the delivered files (`md5sum` equal, so the delivered files
were never mutated): production `citest.py` `59819427474a22dae5e2d03c4da57abc`,
`testsupport/gitfixture.py` `2b35736d5360c6bf8bfe7b821983d087`; every red below reproduced, and the
one count that moved is noted in its row. Fix stage: every red below was re-run against copies of
the fixed tree - (a), (c) and (d) reproduce, (b) moves to `failures=3, errors=1` because the new
redirect probe fails too, and the fix-stage pins' own mutants are recorded in the fix-stage section
below)

| Mutation | Target suite(s) | Observed |
| --- | --- | --- |
| (a) `run_gate` returns `subprocess.CompletedProcess(argv, 0, "", "")` instead of the child's result | `test-citest.py`, `test-assert-folded-gates.py`, `test-linux-v2-go-events.py`, `test-assert-exunit-json.py` | **exit 1 / 1 / 1 / 1**: `test-citest.py` `FAILED (failures=2, errors=2, skipped=1)` (`test_the_child_status_and_streams_are_returned_unchanged`, `test_arguments_reach_the_child_in_order`, `test_cwd_and_env_replace_the_inherited_ones`, and the apply stage's `test_a_timeout_raises_instead_of_returning_a_default` - the added pin also kills this mutant); `test-assert-folded-gates.py` `FAILED (failures=4)`; `test-linux-v2-go-events.py` `FAILED (failures=13)`, failing every refusal sub-test; `test-assert-exunit-json.py` `FAILED (failures=21, errors=1)`, failing its refusal set |
| (b) `FixtureRepo` drops the repo-local `user.name`/`user.email`/`commit.gpgsign`/`core.autocrlf` block, run with `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` pointed at an empty file and `GIT_CONFIG_NOSYSTEM=1` (`git config --global --get user.email` -> exit 1, so the host identity was masked) | `test-gitfixture.py`, `test-linux-v2-source-subject.py`, `test-check-record-cleanliness.py` | **`test-gitfixture.py` exit 1, `FAILED (failures=3, errors=1)`** (fix stage; was `failures=2, errors=1` at 11 tests): `test_the_identity_signing_and_eol_configuration_is_the_fixture_s` errors (`git config --get user.name` fails), `test_commit_stages_every_change_and_returns_the_head` and `test_a_commit_needs_no_host_identity_or_signing_configuration` fail on the recorded author email, and the fix-stage `test_the_fixture_identity_survives_a_redirected_config_file` fails on the missing fixture identity. The two consumer suites stay green and are recorded: they make no identity claim, and git still commits with its *implicit* identity derived from the host user name when `user.email` is unset - so the mutant's red is carried by the fixture suite's explicit identity pin, not by every consumer. |
| (c) `strict_json`'s `object_pairs` hook stops refusing a repeated key | `test-citest.py` | **exit 1, `FAILED (failures=2, skipped=1)`**: `test_a_repeated_key_is_refused` and `test_a_repeated_key_inside_a_nested_object_is_refused` (`AssertionError: StrictJsonRefused not raised`) |
| (d) `write_json` writes `separators=(",", ":")` instead of `indent=2` | `test-citest.py` | **exit 1, `FAILED (failures=2, skipped=1)`**: `test_the_written_bytes_are_canonical_and_returned` shows `b'{"b":1,"a":[2,{"c":3}]}\n' != b'{\n  "b": 1,\n  "a": [\n    2,\n    {\n      "c": 3\n    }\n  ]\n}\n'`; `test_sort_keys_orders_the_members_by_key` fails on the same reflow |

### Plain gate runs (post-change, foreground, one suite at a time)

Every count below is the suite's own summary line; the "pre" count is the same suite's summary in
the baseline capture, so a changed count or a new failure would be visible here.
The batches were re-run once with `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM`/`GIT_CONFIG_NOSYSTEM`
unset, because the mutant-(b) shell had exported them; the results were identical in every row, so
the numbers below are the clean-environment runs.

| Command | exit | counts |
| --- | --- | --- |
| `python3 scripts/ci/test-assert-folded-gates.py` | 0 | `Ran 6 tests`, `OK` (pre 6) |
| `python3 scripts/ci/test-assert-exunit-json.py` | 0 | `Ran 26 tests`, `OK` (pre 26) |
| `python3 scripts/ci/test-assert-playwright-json.py` | 0 | `Ran 17 tests`, `OK` (pre 17) |
| `python3 scripts/ci/test-assert-runner-matches-target.py` | 0 | `Ran 14 tests`, `OK` (pre 14) |
| `python3 scripts/ci/test-linux-v2-go-events.py` | 0 | `Ran 6 tests`, `OK` (pre 6) |
| `python3 scripts/ci/test-model-routing-source-subject.py` | 0 | `Ran 6 tests`, `OK (skipped=1)` (pre 6, same skip) |
| `python3 scripts/ci/test-omo-native-evidence.py` | 0 | `Ran 15 tests`, `OK` (pre 15) |
| `python3 scripts/ci/test-omo-control-native-evidence.py` | 0 | `Ran 10 tests`, `OK` (pre 10) |
| `python3 scripts/ci/test-rootless-compose-owner-claim.py` | 0 | `Ran 9 tests`, `OK` (pre 9) |
| `python3 scripts/ci/test-rootless-compose-transition-proxy.py` | 0 | `Ran 1 test`, `OK` (pre 1) |
| `python3 scripts/ci/test-opencode-v2-support-witness.py` | 0 | `Ran 16 tests`, `OK` (pre 16) |
| `python3 scripts/ci/test-check-lockfile-registry.py` | 0 | `Ran 25 tests`, `OK` (pre 25) |
| `python3 scripts/ci/test-go-test-json-gate.py` | 0 | `Ran 28 tests`, `OK (skipped=5)` (pre 28, same 5 POSIX skips) |
| `python3 scripts/ci/test-citest.py` | 0 | `Ran 29 tests`, `OK (skipped=1)` (new suite; the skip is the POSIX-only `0o600` assertion; three of the added tests are the apply stage's F6/F7 pins, and the four fix-stage pins are the empty-stdin default, the stderr echo, the config redirect and the config injection) |
| `python3 scripts/ci/test-gitfixture.py` | 0 | `Ran 12 tests`, `OK` (new suite; the apply stage removed the vacuous auto-collected `_Probe.runTest` case, finding F2; the fix stage added the redirected-config identity probe) |
| `python3 scripts/ci/test-check-record-cleanliness.py` | 0 | `Ran 19 tests`, `OK` (pre 19) |
| `python3 scripts/ci/test-check-citation-existence.py` | 0 | `Ran 49 tests`, `OK` (pre 49) |
| `python3 scripts/ci/test-linux-v2-source-subject.py` | 0 | `Ran 4 tests`, `OK` (pre 4) |
| `python3 scripts/ci/test-model-routing-omp-usage.py` | 0 | `Ran 40 tests`, `OK` (pre 40) |
| `python3 scripts/ci/test-model-routing-evaluation.py` | 0 | `Ran 80 tests in 1558.578s`, `OK (skipped=2)` (pre 80, same 2 skips). This one run inherited the mutant-(b) shell's empty `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM`; the suite's fixtures pin identity, signing and eol configuration repo-locally, and both byte captures of the same suite ran without that masking with identical results, so the run is stricter, not weaker, than the CI environment |
| `python3 scripts/ci/test-linux-v2-control-receipt.py` (Windows host) | 1 | the suite's own guard: `receipt CLI regressions require Linux root for new disposable ownership fixtures` (identical to the baseline) |
| `python3 scripts/ci/test-no-legacy-routes-audit.py` (Windows host) | 1 | `Ran 6 tests`, `FAILED (failures=11)` - the pre-existing `/bin/bash` path failures, identical to the baseline |
| `python3 scripts/ci/test-linux-v2-evidence.py` (Windows host) | 1 | `Ran 36 tests`, `FAILED (failures=55, errors=5)` - the pre-existing `os.getuid` exporter failures, identical to the baseline |
| `python3 scripts/ci/test-credentialed-provider-witness.py` (Windows host) | 1 | `Ran 5 tests`, `FAILED (errors=2)` - the pre-existing unlaunchable `mix.cmd`, identical to the baseline |

The four Linux-only suites, run in WSL Ubuntu-24.04 as root from the repository mount:

| Command (WSL) | exit | counts |
| --- | --- | --- |
| `python3 scripts/ci/test-linux-v2-evidence.py` | 0 | `Ran 36 tests`, `OK` (pre 36, `OK`) |
| `env PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin python3 scripts/ci/test-no-legacy-routes-audit.py` | 0 | `Ran 6 tests`, `OK` (pre 6, `OK`) |
| `python3 scripts/ci/test-linux-v2-control-receipt.py` | 0 | `Ran 9 tests`, `OK` (pre 9, `OK`) |
| `python3 scripts/ci/test-credentialed-provider-witness.py` | 1 | `Ran 5 tests`, `FAILED (failures=1)` - the pre-existing `Could not find Hex` failure, same test, same count as the baseline |

### Toolchain and repository checks

| Command | Result |
| --- | --- |
| `ruff check scripts/ci` (0.17.5, the config's `required-version`) | **exit 0**, `All checks passed!` |
| `ruff format --check scripts/ci` (0.17.5) | **exit 0**, `57 files already formatted` |
| `python3 scripts/ci/check-lockfile-registry.py` | **exit 0**, `verified every recorded tarball URL in pnpm-lock.yaml sits on registry.npmjs.org: 263 entries checked` |
| `git diff --check` | **exit 0**, no output |
| scan of the working-tree diff and the new files for a user-profile path (case-insensitive scan for the Windows profile directory segment, and for a backslash immediately before the letter u) | no matches - the scan pattern is described instead of embedded, so the contract-mandated host-path check over `git diff origin/main...HEAD` is clean (fix stage, findings STD-A06/SPEC-A06) |
| `pyproject.toml` | unchanged: the new modules need no per-file ignore, and the two new suites' POSIX-only skip is a `unittest.skipUnless`, not a lint suppression |
| the CI step's invocation form, run from `daemon/` (`python3 ../scripts/ci/test-citest.py`, `… test-gitfixture.py`, `… test-check-citation-existence.py`, `… test-model-routing-evaluation.py <one test>`) | the two new suites report `Ran 29 tests`/`OK (skipped=1)` and `Ran 12 tests`/`OK`; `import citest` and `import testsupport.gitfixture` resolve through the script's own directory on `sys.path[0]`, which is what the daemon job's `../scripts/ci/…` form relies on |

### Citation gate at the slice head

Run exactly as §11 requires for a slice that moves files, against a dangling snapshot of the
uncommitted tree (`GIT_INDEX_FILE` in a scratch path, `git read-tree HEAD`, `git add -A`,
`git write-tree`, `git commit-tree <tree> -p HEAD`; the real index and every branch untouched):
`tree=5a78ec4a203f40eef700c945680b8a7d6463ec49`, `commit=31ef7f9405607ae5a9fdbff0af6368a271ee7524`,
parent/HEAD `12de9428a4db`, root commit `e309077c4c4ea815387e6dc2a667b10a1524f522`.

| Command | Result |
| --- | --- |
| `python3 scripts/ci/check-citation-existence.py --repo . --base HEAD --head 31ef7f940560` | **exit 0**, `no audited document added or modified between 12de9428a4db and 31ef7f940560; nothing to check` (expected: this slice changes no audited document) |
| `python3 scripts/ci/check-citation-existence.py --repo . --base e309077c4c4e --head 31ef7f940560` | **exit 0**, `10 changed document(s) pass the path and symbol check (544 path citation(s))` |

This slice touches none of the audited documents (`docs/runbooks/*`, `docs/design/quality.md`,
`docs/archive/CLOSED-TODO.md`), which is why the scoped run reports nothing to check; the `note:`
lines the full-corpus run prints about `control/priv/static/portal` and `control/deps/...` are the
pre-existing ignore-rule notices for those documents, and the run still exits 0.

## Apply stage (2026-11-22, accepted simplify findings F1-F8)

The apply stage carried the accepted simplify findings on this tree: F1 (`citest.git`'s unused `text=`
parameter removed), F2 (`_Probe` moved into its only test, so the vacuous auto-collected `runTest` is
gone), F3 (`run_export` inlined at its 33 call sites, its two-line delegate and the then-unused
`import subprocess` deleted), F4 (7 raw gate invocations -> `citest.run_gate`; `test-go-test-json-gate.py`'s
`ArgumentTests.run_script` inlined at its 3 call sites and deleted; the `pass_fds` boundary probe stays
raw), F5 (3 inline orphan/other-repository setups -> `citest.git`, which adds the `GIT_*` isolation),
F6 + F7 (`test-citest.py` gains the six-variable `GIT_REPOSITORY_VARIABLES` isolation table with one
verified probe per variable, the `GIT_CONFIG_GLOBAL` keep-policy pin and the `run_gate(timeout=)` pin),
F8 (the LOC row above).

Gate-CLI evidence for the converted call sites (the D1 method applied to the four suites the applied
findings touch): a scratch recorder wrapped `subprocess.run` and recorded every gate child - argv, cwd,
`env`, `text`/`encoding`, `capture_output`, `check`, `pass_fds`, `shell`, `input`, exit code, `stdout`,
`stderr` - for each suite on `10b03cc0` (extracted with `git archive`; the Linux-only suite ran in WSL
on both sides) and on the delivered tree, normalizing the tree root, every temporary root and the hex
identifiers of freshly created fixture repositories. 214 invocations compared, 0 field differences,
suite exit codes unchanged:

| Suite | Gate invocations | Field differences |
| --- | --- | --- |
| `test-check-citation-existence.py` | 54 = 54 | 0 |
| `test-check-record-cleanliness.py` | 21 = 21 | 0 |
| `test-go-test-json-gate.py` | 20 = 20 | 0 |
| `test-linux-v2-evidence.py` (WSL, root) | 119 = 119 | 0 |

Every plain gate run at the delivered revision is in the tables above (Windows host and WSL), and the
platform limits, decisions and residual risks below are unchanged by the apply stage.

## Fix stage (2026-11-22, review r1: STD-A01-A06, SPEC-A01-A10)

The dual-axis review of `b25e34ae..89c68e05` reported 16 findings and no production-behaviour
defect. The fix stage changed the helpers, the two new suites and this plan; every migrated suite's
logic, assertions, messages and gate invocations are unchanged, and the D1 gate children are
untouched by construction (`citest.run_gate` is not edited; the evaluation suite loses one divider
comment).

- STD-A03: `citest.git` defaults to `input=b""` (the semantics the replaced
  `check_output(input=None)` calls had) and writes a failing command's captured stderr to the
  suite's stderr before re-raising. New pins:
  `test_an_absent_input_runs_the_child_with_empty_stdin` and
  `test_a_failing_command_prints_the_captured_stderr`.
- STD-A04: `git_environment` drops `GIT_CONFIG` and the
  `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_*`/`GIT_CONFIG_VALUE_*` family and keeps
  `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM`/`GIT_CONFIG_NOSYSTEM` for the callers that pin
  configuration. The reviewer's `git config --local` suggestion does not work on this host: with
  `GIT_CONFIG` set, git 2.56.0 answers `error: only one config file at a time`; dropping the
  redirect fixes the write and the read side at once. New pins:
  `test_an_inherited_config_file_redirect_is_dropped`,
  `test_an_inherited_config_injection_is_dropped`, and the fixture-level
  `test_the_fixture_identity_survives_a_redirected_config_file`.
- STD-A01/A02/A05: the input assertion pins the `b"blob 8\0content\n"` digest; the three `GIT_DIR`
  isolation tests assert `rev-parse --absolute-git-dir` against the expected `.git` directory; the
  env-replacement test scrubs a patched `GATE_SCRUBBED` variable from the passed environment; the
  child reports its full `cwd`; the POSIX mode test sets `os.umask(0o022)` for its duration.
- STD-A06: the evaluation suite's `# -- environment helpers` divider (inside the migrated hunk) is
  removed; decision 7 records the corrected count.
- SPEC-A01-A10: corrections applied above - the baseline counts (22 loaders across 19 files; 8
  test-side `def git(` files), the raw-gate baseline (24 matching lines in 13 owned suites at
  `b25e34ae`, F4 subset named), the LOC row's numstat base, the falsified "no other child
  invocation changed" sentence, the DLP row, the witness identity values, the `#180` reference,
  decision 7's docstring list and the D1 comparison scope. SPEC-A08 needs a conductor-side
  contract amendment (two migrated files outside the STRICT list) and changes no code; it is
  recorded in the fix-stage answer.

Fix-stage re-run (foreground, fixed tree; every host suite except the conductor-owned full
`test-model-routing-evaluation.py` run, plus both toolchain checks, the citation gate and WSL):

- Host suites: every summary line equals the apply-stage table above except the two new suites (29
  and 12 tests); the four Windows platform-limit suites repeat their recorded outcomes; the daemon
  form re-run reports `Ran 29 tests`/`OK (skipped=1)`, `Ran 12 tests`/`OK` and `Ran 1 test`/`OK`
  for one evaluation test. WSL: evidence 36 `OK`, no-legacy 6 `OK`, control-receipt 9 `OK`,
  witness 5 `FAILED (failures=1)` (the pre-existing Hex refusal).
- Toolchain: `ruff check`/`ruff format --check` exit 0; `git diff --check` exit 0; the lockfile
  gate exit 0; the contract-mandated host-path check over the working-tree diff is clean.
- Citation gate at the fix-stage snapshot (tree `0ad7ed5770727d8c78f92f979ef60673c2e07a26`, commit
  `f8838085c6f485fa86b5bc071018e9e921115b45`, parent `89c68e05`; a dangling commit from the
  scratch `GIT_INDEX_FILE` index, no ref moved): `--base HEAD` exit 0, nothing to check; `--base
  e309077c4c4e` exit 0, `10 changed document(s) pass the path and symbol check (561 path
  citation(s))`.
- Mutants (copies of the fixed `scripts/ci`): (a) reproduces (`failures=2, errors=2, skipped=1` in
  `test-citest.py`; folded 4; go-events 13; exunit `failures=21, errors=1`); (b) `test-gitfixture.py`
  `FAILED (failures=3, errors=1)` with both consumers green; (c) and (d) `FAILED (failures=2,
  skipped=1)` each. The new pins' mutants: always-empty input `failures=2` (the digest pin plus the
  alternates probe), inherited-stdin default `failures=1` (the empty-stdin pin), config family kept
  `errors=1, failures=1` in `test-citest.py` plus `failures=1` in `test-gitfixture.py`, only
  `GIT_CONFIG` dropped `failures=1` (the injection pin alone), stderr echo removed `failures=1`,
  env merged instead of replaced `failures=1`, chmod removed (WSL) `failures=1` at `420 != 384`
  with the unmutated control green.

## Residual risks and known limits

- `citest.strict_json` has no consumer besides its own suite in this increment: every suite that
  reads tool-written JSON today reads it through `json.loads`, and wiring the strict reader into
  `test-model-routing-evaluation.py` would change that frozen suite's refusal class (W5-12's file).
  The helper lands with the test that kills its mutant; the production unification stays with
  W5-11/W5-12.
- The three measured `receipt()` definitions are unrelated payloads sharing a name (a stop-receipt
  dict, a CLI driver, a witness-record builder); they are recorded as measured-and-not-a-copy rather
  than folded into the helper.
- `test-omo-control-native-evidence.py`'s module-level `git(directory, *arguments)` and its inline
  Go-toolchain fixture stay as they are: `text=True` + `strip()` + `AssertionError`, one test, and a
  class that needs no repository in `setUp`.
- `test-credentialed-provider-witness.py`'s fifth test cannot pass in this environment: it needs Hex
  and the control dependencies, and `mix local.hex --force` fails offline
  (`repo.hex.pm` is unreachable). Four of its five tests pass before and after with the same counts
  and the same failure name; the CI control job carries the dependency preparation that this host
  lacks.
- `test-linux-v2-control-receipt.py`, `test-linux-v2-evidence.py` and `test-no-legacy-routes-audit.py`
  cannot run on the Windows host (root, `os.getuid`, `bash`); their real gates and their byte diffs
  are in WSL, and the Windows-host results are recorded as the environment, unchanged by this slice.
- Mutation (b)'s red is carried by `test-gitfixture.py`'s explicit identity pin. Git falls back to
  an implicit identity derived from the host user name when `user.email` is unset, so the consumer
  suites keep committing and stay green under that mutant; that is a property of git, not of the
  helper, and it is why the fixture suite asserts the recorded author email rather than only that the
  commit succeeded.
- The evaluation suite's `cli` delegate passes `cwd` as a `pathlib.Path` where the pre-change call
  passed `str(...)`: behaviourally identical for `subprocess` (`os.fspath`), and the only recorded
  field whose *form* changed.
- `docs/TODO.md`'s "scripts/ci ruff baseline" entry (the W1-11 follow-up that removes the `#137`
  `extend-exclude`) states "51 today, 52 once #137 lands". This slice adds four Python files
  (`scripts/ci` now has 59 with the `*.py` glob and 60 recursively), so that count is stale by +4.
  The entry belongs to the #137-exclude follow-up and is not closed by this slice, so it is recorded
  here rather than edited.
- The CI wiring is in the diff, but a real CI run needs a pushed branch: the run that executes the
  two new suites inside the daemon job is the operator's stage, not this one.
