# rf-w1-11 scripts/ci ruff baseline plan — one pinned ruff config, one format pass, a lint-clean toolbelt

Contract: `goal-rf-w1-11-scripts-ruff.md` (GDD refactor wave 1, 2026-11-21). Baseline revision: this
worktree's HEAD `a3039f0d17fc7c7905b63bdf247cac5bf339b213` (`a3039f0`). Every count below was measured
at that revision with ruff 0.17.5 (`python -m ruff`, the version this host and `.ruff_cache/0.17.5`
carry; the plan's own `.ruff_cache` entry is the only ruff version evidence in the tree).

## Measured baseline (before)

| Command (from the repository root) | Result |
| --- | --- |
| `python -m ruff check scripts/ci` (ruff's own default rule set) | `Found 94 errors` / `42 fixable` |
| `python -m ruff check scripts/ci` under the target config (`line-length = 120`) | `Found 373 errors` / `43 fixable` — E501 220, B904 62, I001 28, SIM105 12, SIM117 6, C420 5, B905 4, SIM102 3, RUF005 3, B023 3, UP022 3, F841 3, SIM108 2, RUF021 2, UP031 2, C405 2, RUF007 2, RUF001/UP035/B020/SIM115/SIM905/UP012/RUF023/ARG001/F401/B007/RUF059 1 each |
| `python -m ruff format --check scripts/ci` | `46 files would be reformatted, 3 files already formatted` (49 linted; the two `#137` files are excluded) |
| `run-suites.sh` over `scripts/ci/test-*.py` (WSL Ubuntu-24.04, python3 3.12.3, the CI platform for these scripts) | 24 suites, 19 green, 5 environment-red — see "Suite baseline" below |
| the same loop natively on Windows (python3 3.12.10) | 14 suites reached: 12 exited 0 (one of them `test-linux-v2-control-receipt.py`'s documented self-skip), 2 red — `test-credentialed-provider-witness.py` (`FAILED (errors=2)`, both `FileNotFoundError` for `mix`, no Elixir toolchain on Windows) and `test-linux-v2-evidence.py` (`FAILED (failures=55, errors=5)`, a Linux-targeted suite); the run was then killed while `test-model-routing-evaluation.py` was still executing (~10 min there), so the Windows before-state is partial and the WSL pair is the regression oracle |

Mechanical pass, measured in this worktree:

| Step | Result |
| --- | --- |
| `ruff format scripts/ci` | `46 files reformatted, 3 files left unchanged` |
| `git diff -- scripts/ci` after that step | the recorded `format-only.patch` (5 868 insertions / 2 785 deletions across 46 files) |
| `ruff check scripts/ci --fix` | `Found 178 errors (44 fixed, 134 remaining)` |
| `ruff format scripts/ci` again (the fixer left three files unformatted) | `3 files reformatted, 46 files left unchanged` |
| `ruff check scripts/ci` / `ruff format --check scripts/ci` after the pass | `Found 133 errors` in 25 files / `49 files already formatted` — the 134 post-fix findings minus one `E501` the second format reflowed away (re-measured 2026-11-21 on a pristine `c8f20186` `scripts/ci` copy with the committed config) |

The 134 post-fix findings (133 still live when the workers started — the second format had already
reflowed one `E501` away) are partitioned across four workers with disjoint file sets (one file, one
writer): `model-routing-evaluation.py` + `test-model-routing-evaluation.py` (66); the Linux-v2 and
omo-control witnesses (33); the test surfaces `model-routing-omp-usage.py`,
`test-credentialed-provider-witness.py`, `test-ci-changed-path-classifier.py`,
`test-omo-*-evidence.py`, `test-check-citation-existence.py` (19); the small tools
`omo-native-evidence.py`, `check-citation-existence.py`, `assert-go-test-json.py`,
`credentialed-provider-witness.py`, `go-test-json-gate.py`, `check-record-cleanliness.py`,
`verify-linux-v2-helper-exit-checkpoint.py` (16).

### Suite baseline (environment matrix, before)

WSL Ubuntu-24.04, the platform `docs/design/quality.md` routes `scripts/ci/` changes to (`exit` is
the suite's own):

| Suite | Result | Note |
| --- | --- | --- |
| 19 of 24 | `OK` | includes `test-ci-changed-path-classifier.py` (18 tests), `test-check-citation-existence.py` (49), `test-linux-v2-evidence.py` (36) |
| `test-credentialed-provider-witness.py` | `FAILED (failures=1)` | `test_actual_elixir_http_policy_and_pending_intent_negative_controls`: `Could not find Hex` — this WSL image has no Hex archive, no `control/deps` and no network |
| `test-go-test-json-gate.py` | `FAILED (failures=1)` | `test_missing_command_executable_fails_closed` expects exit 127, got 1: the WSL PATH inherits Windows directories, and `execvp` treats an inaccessible PATH entry as fatal (`PermissionError` -> the gate's `OSError` arm). With a Linux-only PATH (`env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root`) the same suite is `OK, Ran 28 tests`, so this is an interop artifact of this image |
| `test-model-routing-evaluation.py` | `FAILED (errors=18, skipped=1)` | every error is `PermissionError: [Errno 13] Permission denied: 'node'` inside `install_fake_vitest`; this image has no `node` |
| `test-rootless-compose-owner-claim.py` | `FAILED (errors=1)` | `rootless Compose Control fixture failed: Control Portal login page was unavailable` — no rootless-Compose fixture running |
| `test-rootless-compose-transition-proxy.py` | `FAILED (errors=1)` | the proxy relay connection times out — same missing fixture |

The two rootless suites are green on Windows, and `test-go-test-json-gate.py` is green with the
Linux-only PATH; only the Credentialed-Provider witness needs an environment this host does not have
(Mix + Hex + compiled Control deps), so its pre-existing failure is recorded as an environment gap,
never as a pass.

The default-set 94/42 reproduces the program plan's F5 measurement exactly, so the plan's own numbers
are confirmed at this revision and this ruff version. The `line-length = 120` totals differ from
`findings/scripts-ci.md` §5's "398 at length 120" for one reason: that measurement was taken with a
ruff whose default select was `E4/E7/E9/F`, and it counted the residual *additional* findings of the
extended select. Both totals are the same tree; this plan uses the config-complete number.

## Deliverable shape

1. **`pyproject.toml` (new, repository root)** — the reconciled §10.4 config verbatim plus two
   additions this plan justifies below (`extend-exclude`, `required-version`):

   ```toml
   [tool.ruff]
   target-version = "py312"
   line-length = 120
   required-version = "==0.17.5"
   extend-exclude = [
       "scripts/ci/model-routing-record-sanitize.py",
       "scripts/ci/test-model-routing-record-sanitize.py",
       "scripts/ci/test-model-routing-record-sanitize-json.py",
   ]

   [tool.ruff.lint]
   select = ["E", "F", "W", "I", "B", "UP", "SIM", "C4", "PERF", "RUF", "ARG"]
   ignore = ["TRY003", "BLE001"]

   [tool.ruff.lint.per-file-ignores]
   "scripts/ci/test-*.py" = ["ARG001", "ARG005"]

   [tool.ruff.format]
   quote-style = "double"
   ```

   `required-version` and the exclude are the only deviations from §10.4's text; each is a documented
   decision below, not a silent edit.

2. **Mechanical pass, in this order** (one writer, the parent session):
   `ruff format scripts/ci` (directory form, so the exclude applies) → `ruff check scripts/ci --fix`
   (`Found 178 errors (44 fixed, 134 remaining)`) → re-measure. The format-only delta is saved as a patch
   (`format-only.patch` in this goal's scratch evidence) so the conductor can materialize the
   format-only commit the Opus review asks for without re-deriving it.

3. **Remaining findings fixed to zero**, file-partitioned across parallel workers (disjoint file
   sets, one integration owner). Rule-by-rule policy below; no `# noqa`, no per-file ignore beyond
   the two the config already carries.

4. **`docs/TODO.md`** gains the deferred-exclude entry (exclude + the ruff-version handshake with
   W1-01's `ci.yml` job).

## Exact semantics and edge cases

### What "0 under the config" measures

* The gate commands are the **directory forms** `ruff check scripts/ci` and
  `ruff format --check scripts/ci`. Ruff applies `extend-exclude` to discovered files only:
  `ruff check scripts/ci/model-routing-record-sanitize.py` (an explicitly passed path) bypasses the
  exclude and checks the file — measured. W1-01's job must therefore pass the directory (or
  `--force-exclude`); the plan states this so the two slices cannot disagree later.
* `required-version = "==0.17.5"` makes any other ruff refuse to load the config (measured: exit 2
  with `Required version ==9.9.9 does not match the running version 0.17.5`). The gate is
  reproducible or it does not run; it never passes under a version whose findings nobody measured.
* The `#137` files stay **outside the lint corpus entirely** (lint *and* format), per the
  binding Opus amendment: PR #137 rewrites them and a reformat here would conflict, and the third
  entry names the sibling test module #137 adds (`scripts/ci/test-model-routing-record-sanitize-json.py`,
  verified ADDED in `gh pr view 137 --json files`) ahead of its arrival, as the amendment's "any new
  sibling test module of it" clause requires, so the new file cannot red the gate at #137's merge.
  Consequence, recorded as a known limit: `scripts/ci` is lint-clean over 49 of its 51 Python files until #137
  lands and the exclude is dropped.

### Per-fix policy (behavior preservation is the default)

* **E501 (220)** — wrap the expression/statement; never change a string's bytes. Long message
  strings use implicit literal concatenation inside the existing parentheses, so the runtime string
  is identical character-for-character; long calls/comments are split at the natural boundaries of
  the surrounding style. `ruff format` is re-run after wrapping (idempotent check).
* **B904 (62)** — inside `except … as exc:` handlers, `raise X(...) from exc` when the message
  derives from the caught exception (the honest chain); `from None` for the refusal style that
  deliberately hides the cause (`raise SystemExit(msg)` in a handler that did not bind the
  exception). No handler's message text changes.
* **B905 (4)** — `strict=False` unless equal length is an invariant of the site's own construction,
  in which case `strict=True`. The choice is recorded per site in the increment's report; no suite
  may change its verdict because of it.
* **B023 (3)** — bind the loop variable eagerly. In `test-model-routing-evaluation.py:2800` the
  lambda is invoked by `exercise(...)` in the same iteration, so the eager binding is
  behavior-identical; `test-omo-native-evidence.py:118` builds a class in a loop whose method reads
  the iteration's `ready` event — the binding moves to the method's default argument. Both are
  later-reachable-defect fixes that the F10 entry (W3-16) also names; a lint-clean tree requires
  them here, and W3-16 keeps whatever regression coverage it planned.
* **F841 / RUF059 / B007 / B020 (6)** — delete the dead binding, or rename the loop/unpacked
  variable to the `_`-prefixed form when the value is genuinely unused. Test files: none of these
  touch an assertion.
* **SIM105 (12)** — `contextlib.suppress(<same exception tuple>)`; the suppressed body is exactly
  the `try: … except <E>: pass` body. `import contextlib` is added where missing.
* **SIM117 (6)** — merge the nested `with` statements; no statement sits between them (ruff only
  fires on the directly nested form).
* **SIM102/SIM108, RUF005, RUF021, UP012, UP022, UP031, UP035, C405, C420, E731, SIM115, SIM905,
  RUF007, RUF023 (≈30)** — apply the rule's own suggested rewrite; UP035 moves `Callable`/`Iterable`
  imports from `typing` to `collections.abc`; UP022 becomes `capture_output=True`; C405 rewrites
  `set((a, b, c))` as a set literal **inside the existing `tuple(...)`**, preserving both the type
  and the set's iteration order (the `Receipt.secret_forms` order feeds sequential
  `str.replace` calls); SIM905 replaces a literal `"a b c".split(" ")`-shape only where the split
  result is a constant tuple.
* **RUF001 (1)** — the en dash in `check-citation-existence.py`'s `LINE_SUFFIX` regex becomes the
  escape `\u2013`; the compiled pattern is byte-equivalent in behavior and the gate's fixture suite
  (which pins the grammar, including the en-dash range case) is the oracle.
* **Tests are never weakened**: the 24 `test-*.py` suites are the oracle, and every test-file edit in
  this increment is lint-shaped (wrapping, lambda/loop binding, `strict=`, dead binding removal).
  The report lists each test-file edit with its rule code, and the suites decide.

### Decisions with alternatives

* **`required-version = "==0.17.5"`** — alternatives: (a) no pin (any ruff; findings drift with the
  version and the "clean under the committed config" clause silently depends on whichever ruff CI
  installs), (b) a floor (`">=0.17.5"`; new rules can red the gate later). Chosen: the exact pin,
  matching the repo's existing tool-pin style (golangci-lint v2.15.0, §6 of the program plan). The
  handshake is explicit: W1-01's job must install ruff 0.17.5, recorded in `docs/TODO.md`.
* **`extend-exclude` for the `#137` files** — alternatives: (a) reformat them and accept the
  conflict (rejected by the binding amendment), (b) a `per-file-ignores` entry for all rules
  (silences lint but not format; the amendment says exclude both), (c) top-level `extend-exclude`
  (chosen: one entry covering lint and format; dropping it after #137 is a three-line deletion).
* **Fix instead of ignore for the F10-class findings** — the contract's clause 3 forbids new
  blanket ignores, and clause 4 keeps the *defect work* (regression coverage, the PyYAML oracle) in
  W3-16. The split is therefore: this increment makes the linter silent by fixing the shape; W3-16
  keeps the defect-level work. B023 is the only rule where the shape fix is also a small behavior
  fix; both sites are named by the W3-16 entry, and neither changes a suite's verdict.

## Evidence plan (all commands FOREGROUND, serialized; never a suite beside a benchmark)

### Post-change measurements (final tree)

| Check | Command | Result |
| --- | --- | --- |
| lint, Windows | `python -m ruff check scripts/ci` | `All checks passed!`, exit 0 |
| format, Windows | `python -m ruff format --check scripts/ci` | `49 files already formatted`, exit 0 |
| lint, Linux | `wsl … /tmp/rf-w1-11-ruff/ruff-x86_64-unknown-linux-gnu/ruff check scripts/ci` (ruff 0.17.5 built for `x86_64-unknown-linux-gnu`) | `All checks passed!`, exit 0 |
| format, Linux | the same binary, `ruff format --check scripts/ci` | `49 files already formatted`, exit 0 |
| diff hygiene | `git diff --check` | exit 0, no whitespace errors |
| scope | `git status --porcelain` | 47 tracked files (46 `scripts/ci/*.py` + `docs/TODO.md`) + 2 new files (`pyproject.toml`, this plan); the two `#137` files untouched |
| audited documents | selector check over the changed paths | 0 of `docs/runbooks/**`, `docs/design/quality.md`, `docs/archive/CLOSED-TODO.md` changed, so the citation gate has nothing to audit in this range |
| assertion inventory | AST audit of all 23 changed test files against `HEAD` | 0 files with a changed test-method set, assertion count or `subTest` count |
| assertion text | AST audit, `ast.unparse` canonical form | 21 of 23 files canonical-identical; `test-ci-changed-path-classifier.py` carries the two intended C420 rewrites `{key: True for key in OUTPUT_KEYS}` → `dict.fromkeys(OUTPUT_KEYS, True)` (both a fresh dict with immutable values) |
| string constants | AST multiset audit of every string constant in the 46 changed files | 5 intended deltas, all accounted for: the `LINE_SUFFIX` en dash → `\u2013` escape (regex-equivalent), the `CONNECTIVES` `str.split` → the same 23 words as separate literals, one `encode("utf-8")` → `encode()` (UP012 default), and two `%`-templates → f-strings whose evaluated results are byte-identical (`DECOY_SOURCE` re-evaluated: length 181, sha256 `1e10f544…`, equal on both sides; the URL template's assertion is green in the suite) |
| signatures | AST definition inventory of the 46 changed files | no function or class added or removed; exactly two signature changes, both intended: `require_resolved_dependency_tree` (the unused `run` parameter removed with its three call sites) and the test's in-loop `record` (`*, ready=ready` eager binding) |

Windows suite matrix after the change (24 suites, `run-suites-win.sh`): 19 exit 0 (including
`test-ci-changed-path-classifier.py` 18 tests, `test-check-citation-existence.py` 49, `test-go-test-json-gate.py`
28 with 5 skips, both rootless suites, `test-opencode-v2-support-witness.py` 11) and 5 non-zero, each
with its cause established:

| Suite | Result | Cause |
| --- | --- | --- |
| `test-credentialed-provider-witness.py` | `FAILED (errors=2)`, same count as the before-run | no `mix` on Windows (`FileNotFoundError` from `subprocess.run(["mix", …])`) |
| `test-linux-v2-evidence.py` | `FAILED (failures=55, errors=5)`, same counts as the before-run | Linux-targeted suite |
| `test-linux-v2-control-receipt.py` | exit 1 with its own message `receipt CLI regressions require Linux root…` | the suite refuses on non-Linux/non-root by construction (`raise SystemExit("…")`); CI runs it as root on Linux |
| `test-no-legacy-routes-audit.py` | `FAILED (failures=11)`, every one `/bin/bash: C:/…/scripts/audit-no-legacy-routes.sh: No such file or directory` (exit 127) | this MSYS `bash` cannot open a Windows-style path. **Proven pre-existing:** the same 6 tests / 11 failures / 127 assertion reproduce byte-for-byte in a pristine `git archive HEAD` checkout (`<HOST-PATH>`), untracked by this increment |
| `test-model-routing-evaluation.py` | the matrix run was killed by the runner's own 1200 s per-suite timeout with one `F` at test 60 (`test_review_record_requires_independent_accepted_evidence`); the dedicated verbose re-run of the same suite finished `Ran 80 tests in 1647.311s`, `OK (skipped=2)`, exit 0 | the suite needs ~28 min on this host. The single occurrence is a load-contention artifact, not a code verdict: it landed while a diagnostic single-test run of the same suite executed alongside the matrix run, and with the suite alone it is green. Test 60 also passes in isolation on the worktree (`Ran 1 test in 35.326s`, `OK`) and on the pristine `git archive HEAD` checkout (`Ran 1 test in 42.802s`, `OK`), and the same suite's Linux pair is identical (`80 tests`, `errors=18` from the missing `node`, `skipped=1`). None of this increment's edits introduces timing or ordering nondeterminism: the three SIM105 rewrites in that file are the same code path as the `try/except: pass` they replace (`process.kill()`, `process.wait(timeout=…)`, `stream.close()`), read in the diff |

WSL Ubuntu-24.04 matrix after the change (`run-suites.sh`, the same script and environment as the
before run): 24 suites, 19 `OK`, the same 5 environment-red as before, and **every one of the 24
suites reports the identical shape** (same `Ran N tests`, same status line, same sorted
failure/error target list) — the comparison is
`python3 compare-suite-logs.py suite-logs-wsl-before suite-logs-wsl-after` →
`SUMMARY suites-compared=24 changed=0`, exit 0. That includes `test-ci-changed-path-classifier.py`
(18 tests `OK`), `test-model-routing-evaluation.py` (`80 tests`, `FAILED (errors=18, skipped=1)`,
the same 18 `node` errors and the same 2 unreached refusal shapes — the line numbers behind that
summary shifted as expected: 1896/1925 → 2015/2044), and the two rootless suites' single identical
fixture errors.

Targeted supplement for `test-go-test-json-gate.py`, whose WSL failure is the PATH-interop artifact:
`env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root
python3 scripts/ci/test-go-test-json-gate.py` → `Ran 28 tests in 15.452s`, `OK`, exit 0 after the
change (the before-state was measured the same way: `Ran 28 tests in 21.096s`, `OK`).

* Before/after: `ruff check scripts/ci` (counts), `ruff format --check scripts/ci` (file counts).
* `for f in scripts/ci/test-*.py; do python3 "$f"; done` on Windows — all 24 suites, exit codes
  recorded per file (the contract's literal `python3 scripts/ci/test-*.py` form would expand the
  glob into argv and run only the first suite; the loop is the same corpus and is recorded as the
  gate command).
* The same loop in WSL Ubuntu-24.04, because `docs/design/quality.md`'s 2026-11-20 amendment routes
  `scripts/ci/` changes to a Linux run.
* `python3 scripts/ci/test-ci-changed-path-classifier.py` on both platforms (the Opus amendment's
  named test; `ci.yml` is untouched, so it must stay green with no edit beyond lint shape).
* `git diff --check`; `python -m ruff format --check scripts/ci` re-run as the last action.
* The citation gate (`check-citation-existence.py`) audits `docs/runbooks/**`,
  `docs/design/quality.md` and `docs/archive/CLOSED-TODO.md` only; this increment touches none of
  them (the plan lives in `docs/plans/`, the TODO entry in `docs/TODO.md`), so no citation repair is
  in scope — verified by running the gate's own selector over the change set.
* No benchmark/measurement harness runs in this increment (the contract has none).

## Out of scope (contract clause 4)

* F10's defect-level work: the PyYAML oracle in `test-check-citation-existence.py`-adjacent suites,
  regression tests for the loop-capture defects (W3-16).
* The `ci.yml` ruff job and its step wiring (W1-01, including the version handshake above).
* pytest/unittest conversion (D11), and any behavior change beyond the lint shape.
* The two `#137` files (excluded here; they must be lint-clean in #137 or in the follow-up that
  drops the exclude).

## Residual risks

* W1-01 must install ruff 0.17.5 (or the config refuses to run) and pass `scripts/ci` as a
  directory (or pass `--force-exclude`). Recorded in `docs/TODO.md` and the report.
* The format pass reflows 46 files, so blame noise is real; the Opus amendment's mitigation is a
  format-only commit plus `.git-blame-ignore-revs`. It stays unapplied: the branch carries the
  implementation as two fused commits — `ec918271` (49 files, +6395/−3000, config + semantic
  fixes) and `623052bd` (5 files, +37/−21) — with no format-only commit and no
  `.git-blame-ignore-revs` in any history, and no later stage of this goal may rewrite history.
  A `.git-blame-ignore-revs` entry cannot name the fused hash — `ec918271` carries the semantic
  fixes, so skipping it would blur them too.
  Recorded waiver (review finding R1, fix round 2026-11-21): the fused commits are accepted as
  the landed form, subject to the conductor repeating this waiver in the PR body before merge; a
  conductor-side rewrite as format-only + lint-fix commits, adding `.git-blame-ignore-revs` with
  the new hash, remains available from the recorded `format-only.patch` (numstat recorded in the
  evidence plan above), and the ruff gates must be re-run on the final tree if any byte changes.
* Two files (plus the sibling test module #137 adds, pre-excluded by name) stay outside the lint
  corpus until #137 lands (known limit, tracked).
* E501 wrapping is mechanical but touches long lines inside refusal messages some suites assert on;
  the string-byte rule plus the 24 suites are the guard.

## Comment-policy sweep (2026-11-22)

Module intent removed from `scripts/ci/omo-native-evidence.py` and
`scripts/ci/opencode-v2-support-witness.py` by the 2026-11-22 comment-policy sweep is recorded in
[`docs/evidence/comment-policy-sweep-2026-11-22-moved-intent.md`](../evidence/comment-policy-sweep-2026-11-22-moved-intent.md)
("ws7b-ci-scripts"); those two files' only candidate rows outside their docstrings are code-style
split lines. The sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
