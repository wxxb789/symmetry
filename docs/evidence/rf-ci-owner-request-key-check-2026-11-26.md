# rf-ci-owner-request-key-check — evidence record

Contract: `goal-rf-ci-owner-request-key-check.md` (GDD state root, external to the repository).
Base revision: `8453da1203f4c109824545b8caee41f9e9696557` (`main` at dispatch; the branch head's
parent). Branch: `refactor/rf-ci-owner-request-key-check`. Plan:
`docs/plans/2026-11-26-rf-ci-owner-request-key-check-plan.md`.

## 1. Subject bytes

| File | Base sha256 | Delivered sha256 |
| --- | --- | --- |
| `scripts/ci/test-linux-v2-owner-request-keys.py` | absent | `61e8c013d946103d985051e5ff2215143d930f9ae138176385ebb8cd53616b01` |
| `.github/workflows/ci.yml` | `34357934e67a2cbd98fbab43ff801b77642883f5ba338bd36b41cb9a4f484f29` | `fcf1aa34be77f97bcf62eb5cd7a8dcee728e4eb20762a77de132a6b7ebb98d43` |
| `scripts/ci/write-linux-v2-owner-request.py` | `5b9074374fc55119e8c8254b1a61f83585c4d745e8ec801caf67252e009e4196` (= the #294 delivered sha) | `bb2577d8273ccbba844700bde4c5fbe91e306659e2a0e538e2e4fc6f84af0906` |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `f982ef7a59642fd4cce738bb59c4cf1ea6a4eaf79491385933b2eec494f34012` | `a8e20af6c0dc87b47b115c0ce40f856308cd2b5ea1ecfb0ffd02ee4a93143e5e` |
| `docs/plans/2026-11-26-rf-ci-owner-request-key-check-plan.md` | absent | `5353c6fa02d097b85a0b473d5a7035e1ea879ab27e6993820b14a315c3983483` |

## 2. The change

- **New suite** `scripts/ci/test-linux-v2-owner-request-keys.py` (5 tests): the agreement check
  and its mutation witnesses. Writer side parsed with `ast` (`CASES` dict keys, and
  `GOAL_ADMISSION_CASES` via `literal_eval`; a non-literal shape, an empty set, a non-string
  member and a member that names no `CASES` key are refusals). Fixture side parsed by a line
  census over the region from its `@cases` opener to the next top-level `def`: every non-blank
  line must be the opener, a structural line (`] ++`, `Enum.map(@attr, fn v ->`, `end) ++`, `[`,
  `]`), or a row (`{"name"|"prefix#{var}", "profile", :harness, :role,` plus description lines,
  interpolated names expanded from the attribute the enclosing `Enum.map` binds; every element
  line of that attribute must be blank, a comment, or a bare quoted string, and any other refuses
  with its text). A second `@cases [` opener inside the region is refused as unparseable, not
  skipped. Goal routing
  parsed from `Enum.split_with(@cases, … repository_role not in [:pi_durability, :opencode_goal]
  end)`; the fixture-case → writer-case map from the fixture's own `request_case` cond (fixed-name
  list, prefix, collapse target), which is the case name its run metadata carries and the writer
  re-checks (`run.get("case") == case`). Set equality between the mapped goal-routed rows and
  `GOAL_ADMISSION_CASES` prints both sets on failure; a legacy row mapping into the writer's goal
  set is refused too (the `stage_replay_unprovisioned_refusal` carve-out's shape).
- **`ci.yml`**: one line, `python3 ../scripts/ci/test-linux-v2-owner-request-keys.py`, in the
  daemon job's existing `Check Linux v2 fixture contracts` step (after the
  `test-linux-v2-host-prerequisite-probe.py` line). No new job; no step body is pinned in
  `test-ci-structure-extraction.py` (`grep -rn "fixture contracts" scripts/ci/ .github/` → the
  step name lives only in `.github/workflows/ci.yml`; the `scripts/ci/`-limited invocation prints
  nothing), and the job's ceiling (30) and step caps are untouched, so the derivation
  `check_job_timeouts` pins stays valid.
- **Comment glue** (≤2 lines each, pointing at the suite):
  `write-linux-v2-owner-request.py:30-31` above `GOAL_ADMISSION_CASES`, and
  `provision-linux-v2-control-claims.exs:184-185` above `goal_case_specs`. Both files changed
  comment bytes only; the fixture's diff is 2 lines replaced by 2 lines (line count unchanged).

## 3. Before-state and mutation witnesses

The check reddens on the pre-#294 writer (the state that predates the keyed branch):

| Writer bytes | Fixture bytes | Verdict |
| --- | --- | --- |
| `git show 550f96a3^:scripts/ci/write-linux-v2-owner-request.py` | head | RED — `write-linux-v2-owner-request.py: no module-level GOAL_ADMISSION_CASES assignment` |
| `git show 550f96a3:scripts/ci/write-linux-v2-owner-request.py` | head | green — both sides the 5 writer cases |

The committed pair parses as 48 `@cases` rows (43 goal-routed: 39 `:pi_durability` + 4
`:opencode_goal`; 5 legacy), which equals the fixture's own `@expected_fixture_runs 48` runtime
pin; the mapped goal set is `{stage_replay, codex_containment, claude_containment,
stage_replay_native_release_codex, stage_replay_native_release_claude}`, equal to
`GOAL_ADMISSION_CASES`.

Mutation witnesses (inside the suite — three standalone refusal tests plus the six spellings
of the tuple loop; each asserts its anchor matched first, so a
mutated-away anchor fails the witness instead of passing vacuously; every row reddens inside the
`daemon` job's `Check Linux v2 fixture contracts` step, non-root runner user, or the same suite
locally):

| Mutation | Verdict and refusal |
| --- | --- |
| drop `"codex_containment",` from the writer's set | RED — both sets printed: `writer (4): [...] / fixture (5): [...]` |
| add `{"goal_case_drift_probe", … :pi_durability,` to the fixture's table | RED — `writer (5) / fixture (6)` with `goal_case_drift_probe` in the fixture set |
| remove `"stage_replay_unprovisioned_refusal"` from the cond's fixed list (legacy row maps to `stage_replay`) | RED — `legacy rows route to a writer case keyed as a goal admission: ['stage_replay']` |
| replace the split predicate's role list with a variable | RED — `no Enum.split_with(@cases, … repository_role not in [:role, :role] end) goal-route predicate` |
| rewrite the cond's prefix arm | RED — `no request_case = cond … rule of the pinned shape` |
| replace one row's role atom with a variable | RED — `unparseable @cases line '… :claude_code, goal_role,'` |
| replace one `@stage_replay_holds` element with an atom | RED — `unparseable @stage_replay_holds [ element line '    :handoff_committed,'` |
| star the writer's set assignment (`{*CASES,`) | RED — `GOAL_ADMISSION_CASES is not a literal set of strings (malformed node or string …)` |
| add a second `@cases [` opener after the first `  ] ++` | RED — `unparseable @cases line '  @cases ['` |

## 4. Gates (all on the frozen tree)

| Gate | Command (cwd) | Exit | Output row |
| --- | --- | --- | --- |
| New suite | `python scripts/ci/test-linux-v2-owner-request-keys.py` (repo root, Python 3.12.10, Windows) | 0 | `Ran 5 tests in 0.055s … OK` (the nine mutation witnesses are inside and each expects its refusal) |
| Path classifier suite | `python scripts/ci/test-ci-changed-path-classifier.py` (repo root) | 0 | `Ran 37 tests in 31.282s … OK` |
| CI structure suite | `python scripts/ci/test-ci-structure-extraction.py` (repo root) | 0 | `Ran 70 tests in 9.513s … OK` |
| actionlint | `actionlint -shellcheck= -pyflakes= .github/workflows/*.yml` (v1.7.12, the version and flags `ci.yml` pins) | 0 | `actionlint_findings=0` |
| ruff check | `python -m ruff check scripts/ci/test-linux-v2-owner-request-keys.py` (0.17.5, the version `pyproject.toml` requires) | 0 | `All checks passed!`; the directory-wide run's 9 findings sit in `opencode-v2-support-witness.py`, `test-ci-structure-extraction.py`, `test-linux-v2-evidence.py`, `test-opencode-v2-support-witness.py` — none in the new file |
| ruff format | `python -m ruff format --check scripts/ci/test-linux-v2-owner-request-keys.py` | 0 | `1 file already formatted` |
| Changed-path clone gate | `python scripts/ci/jscpd-changed.py --base dcfb167544aab12b80d96079342ea25c986da94b --head 558cf940f88c478caaf49a597f47d8af7795c1ff` (the changed bytes committed in a scratch clone only, this worktree's refs untouched; jscpd 4.3.0 from the existing scratch install, the `package.json` pin) | 0 | `8 changed file(s), 1763 scanned source(s), 1262 clone(s) in the corpus, 0 tolerated among the added lines, none new`; a notice records that `docs/archive/CLOSED-TODO.md` carries added lines but was not scanned (its format, path or size is outside the configured scope), so clones inside it are not checked |
| Record cleanliness (the classifier routes a `docs/evidence/**` change to it) | `python scripts/ci/check-record-cleanliness.py --base dcfb167544aab12b80d96079342ea25c986da94b --head 558cf940f88c478caaf49a597f47d8af7795c1ff` | 0 | `1 changed record(s) pass the root-only check` |
| Citation existence (the classifier routes a `docs/archive/CLOSED-TODO.md` change to it) | `python scripts/ci/check-citation-existence.py --base dcfb167544aab12b80d96079342ea25c986da94b --head 558cf940f88c478caaf49a597f47d8af7795c1ff` | 0 | `1 changed document(s) pass the path and symbol check (667 path citation(s))`; three notes name pre-existing ignore-rule-covered citations |

## 5. Limits

- The contract's hosted facts — the PR's own CI green and its log showing the new step — are the
  PR stage's; this stage proves the step exists and parses (actionlint, the structure suite) and
  the suite green locally.
- The Elixir formatter check was not run on the fixture: the file is outside `control/.formatter.exs`
  inputs, so the `control` job's `mix format --check-formatted` does not cover it, and the
  pi-durability record measured it red at base and delivered alike (pre-existing Styler drift).
  The change there is comment-only with an unchanged line count, so no drift line can move onto it
  (§2; `git diff` shows the two comment lines). `mix format` here would need `control/deps` and a
  compiled Styler, absent in this worktree.
- Writer cases with no row in this fixture (`wsl2_denial`, driven by the rootless-Compose fixture)
  are outside this agreement by construction; the check compares the fixture's rows only.
- The clone-gate row was run on each successive snapshot of this change, every run printing the same
  row (exit 0, `8 changed file(s), 1262 clone(s) in the corpus, 0 tolerated among the added lines,
  none new`) apart from the scanned-source count, which the corpus's size moves: the snapshot with
  this row's placeholder unfilled (`fdc10076`), `1283fb68` (the row as quoted then), `3d5f9761`, the
  apply stage's `d8dcff061d3d5c6731eaa5edd8d8b2f86951bd37` (the pre-record bytes, the row's cited run
  then) with its delivered bytes, and the pre-merge branch head
  `b0952feba15c9cb6022319baa94df9d11657111a` each printed `1760 scanned source(s)`; the same gate at
  the merge head `e4fe6835089abfec32eec3fe50ffb261ae9fd9d3` prints `1763` (`origin/main`'s merge
  brought the three sources, and the fix-stage edits move no source count), and the fix stage's
  `558cf940f88c478caaf49a597f47d8af7795c1ff` (the pre-record bytes, this row's cited run) with its
  delivered bytes print `1763 scanned source(s)`.
- `test-jscpd-changed.py` (the clone gate's own fixture suite) was not run: no byte of that gate
  or its suite changed.
