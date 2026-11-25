# rf-ci-owner-request-key-check — evidence record

Contract: `goal-rf-ci-owner-request-key-check.md` (GDD state root, external to the repository).
Base revision: `8453da1203f4c109824545b8caee41f9e9696557` (`main` at dispatch; the branch head's
parent). Branch: `refactor/rf-ci-owner-request-key-check`. Plan:
`docs/plans/2026-11-26-rf-ci-owner-request-key-check-plan.md`.

## 1. Subject bytes

| File | Base sha256 | Delivered sha256 |
| --- | --- | --- |
| `scripts/ci/test-linux-v2-owner-request-keys.py` | absent | `b40427ac66bba326bd1bcee9e541696d05a3fad07b4cba1ac731a2f12b6c68c7` |
| `.github/workflows/ci.yml` | `34357934e67a2cbd98fbab43ff801b77642883f5ba338bd36b41cb9a4f484f29` | `fcf1aa34be77f97bcf62eb5cd7a8dcee728e4eb20762a77de132a6b7ebb98d43` |
| `scripts/ci/write-linux-v2-owner-request.py` | `5b9074374fc55119e8c8254b1a61f83585c4d745e8ec801caf67252e009e4196` (= the #294 delivered sha) | `bb2577d8273ccbba844700bde4c5fbe91e306659e2a0e538e2e4fc6f84af0906` |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `f982ef7a59642fd4cce738bb59c4cf1ea6a4eaf79491385933b2eec494f34012` | `a8e20af6c0dc87b47b115c0ce40f856308cd2b5ea1ecfb0ffd02ee4a93143e5e` |
| `docs/plans/2026-11-26-rf-ci-owner-request-key-check-plan.md` | absent | `fdcc216cb64d24c083718dc55b1e14b38ae7557cb7e8f14c53088a8747c6596f` |

## 2. The change

- **New suite** `scripts/ci/test-linux-v2-owner-request-keys.py` (5 tests): the agreement check
  and its mutation witnesses. Writer side parsed with `ast` (`CASES` dict keys, and
  `GOAL_ADMISSION_CASES` via `literal_eval`; a non-literal shape, an empty set, a non-string
  member and a member that names no `CASES` key are refusals). Fixture side parsed by a line
  census over the region from its `@cases` opener to the next top-level `def`: every non-blank
  line must be the opener, a structural line (`] ++`, `Enum.map(@attr, fn v ->`, `end) ++`, `[`,
  `]`), or a row (`{"name"|"prefix#{var}", "profile", :harness, :role,` plus description lines,
  interpolated names expanded from the attribute the enclosing `Enum.map` binds). A second
  `@cases [` opener inside the region is refused as unparseable, not skipped. Goal routing
  parsed from `Enum.split_with(@cases, … repository_role not in [:pi_durability, :opencode_goal]
  end)`; the fixture-case → writer-case map from the fixture's own `request_case` cond (fixed-name
  list, prefix, collapse target), which is the case name its run metadata carries and the writer
  re-checks (`run.get("case") == case`). Set equality between the mapped goal-routed rows and
  `GOAL_ADMISSION_CASES` prints both sets on failure; a legacy row mapping into the writer's goal
  set is refused too (the `stage_replay_unprovisioned_refusal` carve-out's shape).
- **`ci.yml`**: one line, `python3 ../scripts/ci/test-linux-v2-owner-request-keys.py`, in the
  daemon job's existing `Check Linux v2 fixture contracts` step (after the
  `test-linux-v2-host-prerequisite-probe.py` line). No new job; no step body is pinned in
  `test-ci-structure-extraction.py` (`grep -rn "fixture contracts" scripts/ci/` → the step name
  lives only in `ci.yml`), and the job's ceiling (30) and step caps are untouched, so the
  derivation `check_job_timeouts` pins stays valid.
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

Mutation witnesses (inside the suite — three standalone refusal tests plus the five spellings
of the tuple loop; each asserts its anchor matched first, so a
mutated-away anchor fails the witness instead of passing vacuously):

| Mutation | Verdict and refusal |
| --- | --- |
| drop `"codex_containment",` from the writer's set | RED — both sets printed: `writer (4): [...] / fixture (5): [...]` |
| add `{"goal_case_drift_probe", … :pi_durability,` to the fixture's table | RED — `writer (5) / fixture (6)` with `goal_case_drift_probe` in the fixture set |
| remove `"stage_replay_unprovisioned_refusal"` from the cond's fixed list (legacy row maps to `stage_replay`) | RED — `legacy rows route to a writer case keyed as a goal admission: ['stage_replay']` |
| replace the split predicate's role list with a variable | RED — `no Enum.split_with(@cases, … repository_role not in [:role, :role] end) goal-route predicate` |
| rewrite the cond's prefix arm | RED — `no request_case = cond … rule of the pinned shape` |
| replace one row's role atom with a variable | RED — `unparseable @cases line '… :opencode_goal,'` |
| star the writer's set assignment (`{*CASES,`) | RED — `GOAL_ADMISSION_CASES is not a literal set of strings (malformed node or string …)` |
| add a second `@cases [` opener after the first `  ] ++` | RED — `unparseable @cases line '  @cases ['` |

## 4. Gates (all on the frozen tree)

| Gate | Command (cwd) | Exit | Output row |
| --- | --- | --- | --- |
| New suite | `python scripts/ci/test-linux-v2-owner-request-keys.py` (repo root, Python 3.12.10, Windows) | 0 | `Ran 5 tests … OK` (the eight mutation witnesses are inside and each expects its refusal) |
| Path classifier suite | `python scripts/ci/test-ci-changed-path-classifier.py` (repo root) | 0 | `Ran 37 tests in 65.741s … OK` |
| CI structure suite | `python scripts/ci/test-ci-structure-extraction.py` (repo root) | 0 | `Ran 70 tests in 23.233s … OK` |
| actionlint | `actionlint -shellcheck= -pyflakes= .github/workflows/*.yml` (v1.7.12, the version and flags `ci.yml` pins) | 0 | `actionlint_findings=0` |
| ruff check | `python -m ruff check scripts/ci/test-linux-v2-owner-request-keys.py` (0.17.5, the version `pyproject.toml` requires) | 0 | `All checks passed!`; the directory-wide run's 9 findings sit in `opencode-v2-support-witness.py`, `test-ci-structure-extraction.py`, `test-linux-v2-evidence.py`, `test-opencode-v2-support-witness.py` — none in the new file |
| ruff format | `python -m ruff format --check scripts/ci/test-linux-v2-owner-request-keys.py` | 0 | `1 file already formatted` |
| Changed-path clone gate | `python scripts/ci/jscpd-changed.py --repo . --base 8453da12 --head d8dcff061d3d5c6731eaa5edd8d8b2f86951bd37` (the changed bytes committed in a scratch index only, no ref moved; jscpd 4.3.0 from the existing scratch install, the `package.json` pin) | 0 | `8 changed file(s), 1760 scanned source(s), 1262 clone(s) in the corpus, 0 tolerated among the added lines, none new`; a notice records that `docs/archive/CLOSED-TODO.md` carries added lines but exceeds the pinned scan limits (`--max-lines 5000`), so clones inside it are not checked |
| Record cleanliness (the classifier routes a `docs/evidence/**` change to it) | `python scripts/ci/check-record-cleanliness.py --base 8453da12 --head <snapshot>` | 0 | `1 changed record(s) pass the root-only check` |
| Citation existence (the classifier routes a `docs/archive/CLOSED-TODO.md` change to it) | `python scripts/ci/check-citation-existence.py --base 8453da12 --head <snapshot>` | 0 | `1 changed document(s) pass the path and symbol check (661 path citation(s))`; three notes name pre-existing ignore-rule-covered citations |

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
  row (exit 0, `8 changed file(s), 1760 scanned source(s), 1262 clone(s) in the corpus, 0 tolerated
  among the added lines, none new`): the snapshot with this row's placeholder unfilled (`fdc10076`),
  `1283fb68` (the row as quoted then), `3d5f9761`, and the delivered bytes; the apply stage's
  `d8dcff061d3d5c6731eaa5edd8d8b2f86951bd37` (the pre-record bytes, this row's cited run) and its
  delivered bytes.
- `test-jscpd-changed.py` (the clone gate's own fixture suite) was not run: no byte of that gate
  or its suite changed.
