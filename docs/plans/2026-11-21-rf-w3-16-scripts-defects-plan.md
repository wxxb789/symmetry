# rf-w3-16 scripts/ci defects plan — explicit loop rebinds, no dead handler, and a PyYAML oracle whose absence is visible and strict

Contract: `goal-rf-w3-16-scripts-defects.md` (GDD refactor wave 3, 2026-11-21). Baseline revision:
this worktree's HEAD `36f9a887635ec89d49cce2cd13b3915a8a875a83` (`36f9a887`, identical to
`origin/main` at dispatch). Every count below was measured at that revision: ruff 0.17.5 on the
Windows host (Python 3.12.10, PyYAML 6.0.3) and, for the Linux-targeted exporter suite, WSL
Ubuntu-24.04 (Python 3.12.3, PyYAML 6.0.1). Line citations bind that baseline as well — the
contract cites it too, so the numbers are deliberately not renumbered — and a citation at or
below the deleted `read_regular` handler in `export-linux-v2-redacted-evidence.py` is three
lines earlier in the post-change tree (post-change = baseline − 3).

## Measured baseline (before)

| Command (repository root) | Result |
| --- | --- |
| `python -m ruff check --no-cache --isolated --select B023,B020,F841,F401,RUF059,B007,TRY203,PLW2901 scripts/ci/` | `Found 3 errors.` — `PLW2901` ×2 (`export-linux-v2-redacted-evidence.py:1135` `item`, `:1150` `name`) and `TRY203` ×1 (`:1333`, `try: info = path.lstat() / except FileNotFoundError: raise`), statistics `2 PLW2901 redefined-loop-name`, `1 TRY203 useless-try-except` |
| `python -m ruff check scripts/ci/` (repo config) | `All checks passed!`, exit 0 |
| `python -m ruff format --check scripts/ci/` | `49 files already formatted`, exit 0 |
| re-measurement of the other six named files (`test-check-citation-existence.py`, `test-model-routing-evaluation.py`, `test-omo-native-evidence.py`, `opencode-v2-support-witness.py`, `verify-linux-v2-helper-exit-checkpoint.py`, `assert-go-test-json.py`) | 0 findings under the strict selects and 0 under the repo config: W1-11/#151 (`65869a78`) already fixed F10's unused locals/imports and closure captures, so per the contract **none of the six is edited** |
| `python scripts/ci/test-check-lockfile-registry.py` (PyYAML importable) | `Ran 23 tests in 4.081s`, `OK`, exit 0 — the differential method runs |
| `python -S scripts/ci/test-check-lockfile-registry.py` (site-packages off) | `Ran 23 tests in 3.973s`, `OK (skipped=1)`, exit 0 — **no notice and no assertion**: the oracle disappears invisibly |
| `python3 scripts/ci/test-linux-v2-evidence.py` in WSL Ubuntu-24.04 (`/tmp/rf-w3-16/symmetry`) | `Ran 36 tests in 18.877s`, `OK`, exit 0 — the suite that covers the exporter |

`scripts/ci/model-routing-record-sanitize.py` and `test-model-routing-record-sanitize.py` (open PR
#137's files) exist in the tree and are excluded by the repo config; the `--isolated` strict run
still lints them and reports no finding in them, so the directory-wide strict count is exactly the
three findings above.

## Public shape (what changes for a caller)

1. **`scripts/ci/export-linux-v2-redacted-evidence.py`** — three local edits inside
   `project_pid_reuse` and `read_regular`; no signature, no message text, no JSON field, no exit
   code and no manifest key changes.

   `project_pid_reuse`'s `crash_statuses` branch (`:1134-1141`) and `output_digests` branch
   (`:1148-1155`) stop rebinding their loop variables; the rebind target becomes a new local that
   the rest of the branch reads:

   ```python
   # before                                    # after
   for index, item in enumerate(child):         for index, item in enumerate(child):
       item = require_object(item, ...)             projected_item = require_object(item, ...)
       reject_unknown(item, ...)                    reject_unknown(projected_item, ...)
       crashes.append({                             crashes.append({
           "stage": safe_text(item["stage"], ...),      "stage": safe_text(projected_item["stage"], ...),
           "status": project_exit(item["status"], ...),  "status": project_exit(projected_item["status"], ...),
       })                                           })
   ```

   ```python
   # before                                    # after
   for name, digest in digests.items():         for name, digest in digests.items():
       name = safe_text(name, ...)                  safe_name = safe_text(name, ...)
       if not isinstance(digest, str) ...:          if not isinstance(digest, str) ...:
           fail(...)                                    fail(...)
       projected[name] = digest.lower()             projected[safe_name] = digest.lower()
   ```

   `read_regular` (`:1330-1335`) loses its `try/except FileNotFoundError: raise` wrapper;
   `info = path.lstat()` stands alone and keeps raising the same `FileNotFoundError`:

   ```python
   # before                                    # after
   try:                                         info = path.lstat()
       info = path.lstat()
   except FileNotFoundError:
       raise
   ```

2. **`scripts/ci/test-check-lockfile-registry.py`** — the differential oracle's availability
   becomes an asserted, printable fact with an opt-in strict mode. New module-level handles:

   * `REQUIRE_PYYAML_ENV = "SYMMETRY_REQUIRE_PYYAML"`
   * `ABSENCE_NOTICE = "PyYAML differential oracle skipped: module not importable"` (the
     contract's own example text, byte for byte)
   * `SKIP_REASON = "PyYAML is the optional cross-check oracle, not the contract"` (the existing
     skip text, kept)
   * `REQUIRE_NOTICE = "requires the PyYAML differential oracle"` (the phrase the strict failure
     carries; the `-S` subprocess test pins it)

   `load_optional_pyyaml()` prints `ABSENCE_NOTICE` to stderr on `ImportError` and returns `None`;
   the module-level probe is wrapped in `contextlib.redirect_stderr(RecordedStderr(sys.stderr))`,
   which forwards every byte to the real stderr and keeps the exact text for the suite's own
   assertion. The golden assertions and the differential method itself are unchanged.

3. **`docs/TODO.md`** — one new section gains the PyYAML-pin request for the wave-6 `ci.yml` owner
   (W6-01) and cites this contract; no existing entry is closed, so `docs/archive/CLOSED-TODO.md`
   is untouched.

4. **`docs/plans/2026-11-21-rf-w3-16-scripts-defects-plan.md`** — this file.

## Exact semantics and edge cases

### The renames are output-identical

Both branches read the rebound value after the rebind and nothing else references the loop
variables (`item` is not used after `crashes.append(...)`, `name` only in
`projected[name] = digest.lower()`), so the new locals carry exactly the same values in the same
order. The exporter suite does execute the `crash_statuses` branch: its legacy `literal-pid-reuse`
fixture carries the key (`test-linux-v2-evidence.py:292`) and the 16-fixture legacy export asserts
success (`:394-396`), but no assertion pins the branch's projected values — reading `pid_reuse`
(`:417-419`) checks only `assertions.same_pid_and_pgrp_reused` and `replacement.start_time`, and
`output_digests` appears only in the fixture at `:295`. Equivalence is therefore proven by a
throwaway behavioural driver instead of by a value assertion: import the pre-change module
(`git show HEAD:…` into a temporary copy) and the post-change module with `importlib`, call
`project_pid_reuse` on a fixture carrying `crash_statuses`, `output_digests` and the neighbouring
typed fields, and compare the returned dicts and the `SystemExit` messages of the refusal cases
(`crash_statuses` not a list, a crash entry that is not an object, an unknown crash key, a digest
that is not a byte-count + SHA-256, an unknown top-level key) byte for byte. The fixture lives
outside the repository; only its output is recorded in the increment report. No committed test is
added: `test-linux-v2-evidence.py` is outside the contract's owned set, so the missing value-level
`crash_statuses` assertion stays a recorded limit, not a silent gap.

### The dead handler: what the deletion must preserve

`path.lstat()` already raises `FileNotFoundError`; the wrapper only re-raised it. The exception
contract is load-bearing in two places inside the module, and both keep working:

* legacy and compose modes (`:1523` `read_json_regular`, `:1532`/`:1540`/`:1543` `read_regular`)
  run inside one `try` whose `except FileNotFoundError: missing.append(target) / continue`
  (`:1544-1546`) is what writes `manifest["missing_allowlisted_files"]`; the compose-diagnostic
  test asserts that key (`test-linux-v2-evidence.py:1255`, `missing_allowlisted_files`).
* the restarted-daemon log tails (`:1564` `read_daemon_log_tail`) have their own
  `except FileNotFoundError: continue` (`:1565`).

No other exception type changes: every other `fail()` path stays in `read_regular` after the
`lstat()` call, and `OSError` subclasses other than `FileNotFoundError` were never caught by the
deleted wrapper (it bound only `FileNotFoundError`).

### PyYAML availability: the four configurations

| PyYAML importable | `SYMMETRY_REQUIRE_PYYAML` | Behaviour |
| --- | --- | --- |
| yes | unset | no notice (the suite asserts `ABSENCE_NOTICE` is absent from the recorded stderr, so unrelated interpreter stderr chatter cannot red it), differential runs, suite green |
| yes | `1` | as above (the flag is a requirement, and it is met) |
| no | unset | `ABSENCE_NOTICE` on stderr and asserted; differential skipped with `SKIP_REASON`; golden assertions green |
| no | `1` | `@unittest.skipUnless(PYYAML is not None or REQUIRE_PYYAML, …)` no longer skips: the differential method runs, `assertIsNotNone(PYYAML, …)` fails with the requirement message, and the suite exits non-zero (`FAILED (failures=1)`) |

The suite's test count therefore moves 23 → 25: the two added `OracleVisibilityTests` methods are
the notice-visibility assertion above, and a `-S` subprocess test that observes the switched
interpreter's exit code (a suite's own non-zero exit is only observable from outside the process);
the contract's mutation (d) text quotes the pre-change 23.

The notice text and the env-var name are the machine-checkable handles: the notice is the line the
`ci.yml` step can grep for, the env var is the switch W6-01 sets once PyYAML is pinned.

## Decisions with alternatives

* **Rename the rebind target instead of restructuring the loop or suppressing the rule.** The
  contract forbids widening `pyproject.toml`'s `select` and forbids new suppressions, and the
  strict-selects run must read 0. Alternatives rejected: unpack in the loop header where the shape
  is not a tuple (`for index, item in enumerate(...)` cannot unpack `item` without a second
  assignment), or `# noqa: PLW2901` (a suppression the owner rules forbid).
* **Delete the wrapper rather than `# noqa: TRY203` or `try/except Exception`.** `TRY203`'s premise
  holds here: the handler re-raises the identical exception object, so it changes nothing except
  the traceback's frame list. Deleting the two lines is the smallest correct change; keeping the
  handler with a comment would preserve dead code the contract explicitly names as removable.
* **Assert the notice through a recording stderr wrapper, not by re-reading a variable or by
  spawning a probe process.** A `report = notice_text()` value would stay green when the `print`
  is deleted; a `python -E -S` subprocess probe would depend on `check-lockfile-registry.py` being
  importable side-effect free, on `-S` suppressing PyYAML in every environment, and would add a
  process spawn per run. The wrapper keeps the notice live on the real stderr *and* records the
  exact bytes, so deleting the emission reddens the suite's own assertion.
* **Enforce `SYMMETRY_REQUIRE_PYYAML=1` inside the differential method, not at module level.** A
  `raise SystemExit` at import would make the module hostile to any tool that imports it and would
  report the failure as a bare exit code; failing the test yields the standard `FAILED
  (failures=1)` line, keeps every golden assertion running in the same run, and is legible in the
  job log. The differential class' gate is therefore `PYYAML is not None or REQUIRE_PYYAML`: with
  the flag set, absence stops being a skip at all.
* **Notice text = the contract's example verbatim.** `PyYAML differential oracle skipped: module
  not importable` is already the contract's machine-checkable handle; inventing a different
  sentence would fork the handle.

## Evidence plan (all FOREGROUND, serialized; no benchmark runs beside a suite)

| Check | Command |
| --- | --- |
| strict-selects lint (3 → 0) | `python -m ruff check --no-cache --isolated --select B023,B020,F841,F401,RUF059,B007,TRY203,PLW2901 scripts/ci/` |
| repo-config lint / format | `python -m ruff check scripts/ci/`, `python -m ruff format --check scripts/ci/` |
| edited-file suite (hub platform) | `python3 scripts/ci/test-linux-v2-evidence.py` in WSL Ubuntu-24.04 |
| edited-file suite, oracle present | `python scripts/ci/test-check-lockfile-registry.py` (PyYAML 6.0.3) |
| edited-file suite, oracle absent | `python -S scripts/ci/test-check-lockfile-registry.py` (notice + `OK (skipped=1)`) and in WSL (`python3 -S …`, PyYAML 6.0.1 present otherwise) |
| strict flag without PyYAML | `SYMMETRY_REQUIRE_PYYAML=1 python -S scripts/ci/test-check-lockfile-registry.py` → non-zero |
| diff hygiene / scope | `git diff --check`; `git status --porcelain`; the branch-vs-main diff carries no user-profile directory path |
| mutation (a) | re-introduce one `item = require_object(...)` rebind → the strict-selects run reports `PLW2901` |
| mutation (b) | restore the `try/except FileNotFoundError: raise` → `TRY203` |
| mutation (c) | remove the `print(ABSENCE_NOTICE, …)` line → under `python -S` the visibility assertion fails |
| mutation (d) | oracle present → the differential method executes (no skip in the run's status line) |
| mutation (e) | drop `or REQUIRE_PYYAML` from the `DifferentialTests` gate → the `-S` subprocess test reddens on Windows and WSL while `SYMMETRY_REQUIRE_PYYAML=1 … -S` exits 0 and skips again |
| behavioural equivalence of the renames | throwaway driver importing the `HEAD` and edited modules, comparing `project_pid_reuse` outputs and refusal messages |
| other audit suites | `test-opencode-v2-support-witness.py`, `test-go-test-json-gate.py`, `test-check-citation-existence.py`, `test-omo-native-evidence.py` cover unedited files: CI oracles, re-run only if cheap |
| `test-model-routing-evaluation.py` | covers an unedited file; out of the foreground gate per the contract's budget note — run in the background if at all, and report its wall time (the 580 s vs §11 ~146 s conflict stays unresolved) |

## Out of scope (with reasons)

* `ci.yml` and every workflow file — W3-01/#157 own them this wave; the pinned PyYAML install and
  the `SYMMETRY_REQUIRE_PYYAML=1` export are recorded in `docs/TODO.md` for W6-01.
* `pyproject.toml` — W1-11's file; the contract forbids widening `select` instead of fixing sites.
* The other six named Python files — re-measured clean, and the contract says the slice must not
  re-fix or re-format files whose findings are gone.
* `test-linux-v2-evidence.py` — not in the owned set, so the `crash_statuses` branch keeps its
  execution-only coverage (no value assertion is added).
* The git-fixture/citest harness (W4-16), the source-subject/digest consolidation (W5-11),
  `model-routing-evaluation.py`'s split (W5-12), and the `test-*.py` files not named in the owned
  set.

## Residual risks and known limits

* `project_pid_reuse`'s `crash_statuses` branch is executed by the exporter suite (the legacy
  `literal-pid-reuse` fixture carries the key) but no committed assertion pins its projected
  values; the equivalence evidence is the throwaway driver, not a suite. A future edit that
  changes those values can regress without a red test — pre-existing, and outside this slice's
  owned set.
* The `item` → `projected_item` and `name` → `safe_name` rebinds change no output, so no golden
  assertion pins them; the strict-selects run and the equivalence driver are the evidence.
* PyYAML remains unpinned in the repository until W6-01 lands the install: on a runner without it
  the differential method is still skipped (now visibly and, with the flag, fatally). The oracle
  is honest but not yet enforced in CI by this slice.
* `RecordedStderr` only records writes that pass through the redirected stderr of the module-level
  probe, and it records *everything* written there (measured: `python -v … test-check-lockfile-registry.py`
  records the interpreter's import chatter on a PyYAML host). That is why the oracle-present branch
  asserts the notice's absence instead of an empty record, and why the oracle-absent branch's exact
  equality is measured only under plain `python -S` runs; a future second absence-notice site would
  not be recorded unless it goes through the same wrapper.
* `test-model-routing-evaluation.py`'s wall time stays unreconciled with plan §11 (580 s vs
  ~146 s); this slice does not touch the file and does not pre-conclude the number. Measured here
  as a background oracle on the unchanged file (2026-11-21, this Windows host, no other suite
  beside it): `Ran 80 tests in 1581.536s`, `OK (skipped=2)`, exit 0 — consistent with W1-11's
  1647.311 s on the same host, and a Windows-host reading, so it neither supports nor refutes the
  drafting host's 580 s or the CI step's ~146 s.

## Comment-policy sweep (2026-11-22)

Module intent removed from `scripts/ci/test-check-lockfile-registry.py` by the 2026-11-22
comment-policy sweep is recorded below (workstream record "ws7b-ci-scripts").

9. `test-check-lockfile-registry.py:13` — the suite's contract boundary: "the contract is the golden
   assertions, which always run", PyYAML being an extra oracle.
10. `test-check-lockfile-registry.py:131-134` — why the stderr stand-in exists: the suite has to
    assert what a run without PyYAML prints and a process cannot read back its own stderr, so the
    module-level probe writes through the wrapper (the notice still reaches the job log and the
    assertion reads the same bytes).

The sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
