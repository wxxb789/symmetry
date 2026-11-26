# rf-ci-owner-request-key-check — evidence record

Contract: `goal-rf-ci-owner-request-key-check.md` (GDD state root, external to the repository).
Base revision: `8453da1203f4c109824545b8caee41f9e9696557` (`main` at dispatch; the branch head's
parent), comparison base for the range gates `dcfb167544aab12b80d96079342ea25c986da94b`
(`origin/main` at the round-1 fix). Branch: `refactor/rf-ci-owner-request-key-check`. Plan:
`docs/plans/2026-11-26-rf-ci-owner-request-key-check-plan.md`.

## 1. Subject bytes

| File | Base sha256 | Delivered sha256 |
| --- | --- | --- |
| `scripts/ci/test-linux-v2-owner-request-keys.py` | absent | `00bc16f8023e9baa659324f137d9a0ca757b7a3df5e1671d30a78232bb06fd7e` |
| `.github/workflows/ci.yml` | `34357934e67a2cbd98fbab43ff801b77642883f5ba338bd36b41cb9a4f484f29` | `fcf1aa34be77f97bcf62eb5cd7a8dcee728e4eb20762a77de132a6b7ebb98d43` |
| `scripts/ci/write-linux-v2-owner-request.py` | `5b9074374fc55119e8c8254b1a61f83585c4d745e8ec801caf67252e009e4196` (= the #294 delivered sha) | `bb2577d8273ccbba844700bde4c5fbe91e306659e2a0e538e2e4fc6f84af0906` |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `f982ef7a59642fd4cce738bb59c4cf1ea6a4eaf79491385933b2eec494f34012` | `a8e20af6c0dc87b47b115c0ce40f856308cd2b5ea1ecfb0ffd02ee4a93143e5e` |
| `docs/plans/2026-11-26-rf-ci-owner-request-key-check-plan.md` | absent | `a90245a2889291f76e8bcd19b439a4d4972be25e3373ca21c0b18a76821a4fdc` |

The round-1 fix (conductor K1-K4 plus the DS-audit K5-K7; `299-opus-review-1`, `299-ds-audit-1`)
changed the suite and the plan only; the other three rows are the round-0 delivered bytes,
re-measured and unchanged at the fix head. `docs/archive/CLOSED-TODO.md` (K4 closure wording)
hashes to `dd87e08287fe830a0a2d087716e5842bdc0c2ae557d3111fdae965e493595780`; this record is not
self-hashed.

## 2. The change

- **New suite** `scripts/ci/test-linux-v2-owner-request-keys.py` (5 tests): the agreement check
  and its mutation witnesses. Writer side parsed with `ast` (`CASES` dict keys, and
  `GOAL_ADMISSION_CASES` via `literal_eval`; a non-literal shape, an empty set, a non-string
  member and a member that names no `CASES` key are refusals). Fixture side parsed by a line
  census over the region from its `@cases` opener to the next top-level `def`: every non-blank
  line must be the opener, a structural line (`] ++`, `Enum.map(@attr, fn v ->`, `end) ++`, `[`,
  `]`), or a row (`{"name"|"prefix#{var}", "profile", :harness, :role,` plus description lines,
  interpolated names expanded from the attribute the enclosing `Enum.map` binds; that attribute
  must be declared exactly once and every element must be blank, a comment, or a bare quoted
  string — an atom, variable, concatenated or interpolated element line, or a second declaration,
  refuses with its text). A second `@cases [` opener inside the region is refused as unparseable,
  not skipped. Goal routing parsed from `Enum.split_with(@cases, … repository_role not in
  [:pi_durability, :opencode_goal] end)`, whose five-element `fn` head must bind `repository_role`
  as the 4th element — the `@cases` role column the census reads — refusing any other arity or
  position; the fixture-case → writer-case map comes from the fixture's own `request_case` cond
  (fixed-name list, prefix, collapse target), and every fixed-list element must be a bare string
  (`#{` refuses, naming the element). That cond is the case name its run metadata carries and the
  writer re-checks (`run.get("case") == case`). Set equality between the mapped goal-routed rows
  and `GOAL_ADMISSION_CASES` prints both sets on failure; a legacy row mapping into the writer's
  goal set is refused too (the `stage_replay_unprovisioned_refusal` carve-out's shape).
- **Round-1 fixes.** K1 pins the goal-route predicate's 4th binding position and its arity (head
  captured and split, not substring-matched). K2 refuses an interpolated element in a parsed list
  attribute and in the cond's fixed list, naming the element. K3 deletes the two class docstrings
  (zero-comment default). K4 restates the closure (§5). K5 drops the nonexistent `schedule/5`
  citation from the plan and narrows the plan's robustness claim to what the probes show. K6 makes
  `mutate()` assert its anchor occurs exactly once and makes every mutation row assert the
  refusal's reason, not only the file name. K7 refuses a parsed attribute declared more than once.
- **`ci.yml`**: one line, `python3 ../scripts/ci/test-linux-v2-owner-request-keys.py`, in the
  daemon job's existing `Check Linux v2 fixture contracts` step (after the
  `test-linux-v2-host-prerequisite-probe.py` line). No new job; no step body is pinned in
  `test-ci-structure-extraction.py` (`grep -rn "fixture contracts" scripts/ci/ .github/` → the
  step name lives only in `.github/workflows/ci.yml:993`; the `scripts/ci/`-limited invocation
  prints nothing), and the job's ceiling (30) and step caps are untouched, so the derivation
  `check_job_timeouts` pins stays valid.
- **Comment glue** (≤2 lines each, pointing at the suite):
  `write-linux-v2-owner-request.py:30-31` above `GOAL_ADMISSION_CASES`, and
  `provision-linux-v2-control-claims.exs:184-185` above `goal_case_specs`. Both files changed
  comment bytes only; the fixture's diff is 2 lines replaced by 2 lines (line count unchanged).

## 3. Before-state and mutation witnesses

The check reddens on the pre-#294 writer (the state that predates the keyed branch):

| Writer bytes | Fixture bytes | Verdict |
| --- | --- | --- |
| `git show 550f96a3^:scripts/ci/write-linux-v2-owner-request.py` | head | RED — ``write-linux-v2-owner-request.py: no module-level `GOAL_ADMISSION_CASES` assignment`` |
| `git show 550f96a3:scripts/ci/write-linux-v2-owner-request.py` | head | green — both sides the 5 writer cases |

The committed pair parses as 48 `@cases` rows (43 goal-routed: 39 `:pi_durability` + 4
`:opencode_goal`; 5 legacy), which equals the fixture's own `@expected_fixture_runs 48` runtime
pin; the mapped goal set is `{stage_replay, codex_containment, claude_containment,
stage_replay_native_release_codex, stage_replay_native_release_claude}`, equal to
`GOAL_ADMISSION_CASES`.

Mutation witnesses: three standalone tests (a dropped writer case, a fixture-only goal row, a
legacy fixed-list exemption loss) assert a distinctive refusal fragment, and an eleven-row table
in `test_unparseable_shapes_are_refused_with_their_location` asserts, per row, that its anchor
occurs exactly once in the committed bytes and that the refusal names the file and the row's
reason. Every message below was measured on the delivered bytes with a driver that imports the
delivered suite and replays its own mutation table (the fix-stage scratch directory, outside the
repository); the writer's `literal_eval` message carries a nondeterministic `<ast.Starred object
at 0x…>` suffix.

| # | Mutation | Observed refusal |
| --- | --- | --- |
| 1 | split predicate's role list → variable | ``provision-linux-v2-control-claims.exs: no `Enum.split_with(@cases, … repository_role not in [:role, :role] end)` goal-route predicate`` |
| 2 | cond prefix arm rewritten | ``provision-linux-v2-control-claims.exs: no `request_case = cond …` rule of the pinned shape`` |
| 3 | row role atom → variable | ``provision-linux-v2-control-claims.exs: unparseable `@cases` line '    {"claude_containment", "claude-native", :claude_code, goal_role,'`` |
| 4 | writer set starred (`{*CASES,`) | ``write-linux-v2-owner-request.py: `GOAL_ADMISSION_CASES` is not a literal set of strings (malformed node or string on line 32: <ast.Starred object at 0x…>)`` |
| 5 | second `  @cases [` opener | ``provision-linux-v2-control-claims.exs: unparseable `@cases` line '  @cases ['`` |
| 6 | `handoff_committed` element → atom | ``provision-linux-v2-control-claims.exs: unparseable `@stage_replay_holds [` element line '    :handoff_committed,'`` |
| 7 | predicate head: 4th binding moved | ``provision-linux-v2-control-claims.exs: the goal-route predicate must bind repository_role as the 4th of 5 tuple elements (the `@cases` role column), got '_name, _profile, repository_role, _harness, _description'`` |
| 8 | predicate head shortened to 4 bindings | ``provision-linux-v2-control-claims.exs: the goal-route predicate must bind repository_role as the 4th of 5 tuple elements (the `@cases` role column), got '_name, repository_role, _harness, _description'`` |
| 9 | `leaf_bound` element → `"leaf_#{stage}"` | ``provision-linux-v2-control-claims.exs: interpolated `@stage_replay_holds [` element line '    "leaf_#{stage}",'`` |
| 10 | cond fixed element → `"stage_replay_#{codex}"` | ``provision-linux-v2-control-claims.exs: interpolated `case_name in [` element 'stage_replay_#{codex}'`` |
| 11 | second `@stage_replay_holds` declaration | ``provision-linux-v2-control-claims.exs: `@stage_replay_holds [` is declared 2 times, not once`` |

Standalone rows (the two set disagreements print both sets on separate lines):

| Mutation | Verdict and refusal |
| --- | --- |
| drop `"codex_containment",` from the writer's set | RED — both sets printed: `writer (4): [...] / fixture (5): [...]` |
| add `{"goal_case_drift_probe", … :pi_durability,` to the fixture's table | RED — `writer (5) / fixture (6)` with `goal_case_drift_probe` in the fixture set |
| remove `"stage_replay_unprovisioned_refusal"` from the cond's fixed list (legacy row maps to `stage_replay`) | RED — `legacy rows route to a writer case keyed as a goal admission: ['stage_replay']` |

## 4. Gates (all on the frozen tree, in a scratch clone outside the repository)

The range gates ran in a scratch clone outside the repository (head
`21f747f44959a184fe9bf230688f0c7ab4c02ef4`, the pre-record snapshot carrying every delivered
file except this record's final text; the worktree's refs were untouched; the clone is deleted
after the runs, so the cited head identifies the tree). Their quoted invocations reproduce from
this repository against that head; the final record bytes were re-checked at the same clone's
next head after this file was written (see the closing sentence).

| Gate | Command (cwd = the clone) | Exit | Output row |
| --- | --- | --- | --- |
| New suite | `python scripts/ci/test-linux-v2-owner-request-keys.py` | 0 | `Ran 5 tests in 0.069s … OK` (Python 3.12.10, Windows; the eleven mutation rows are inside and each expects its refusal) |
| Path classifier suite | `python scripts/ci/test-ci-changed-path-classifier.py` | 0 | `Ran 37 tests in 103.481s … OK` |
| CI structure suite | `python scripts/ci/test-ci-structure-extraction.py` | 0 | `Ran 70 tests in 33.393s … OK` |
| actionlint | `actionlint -shellcheck= -pyflakes= .github/workflows/*.yml` (v1.7.12, the version and flags `ci.yml` pins) | 0 | no output (`actionlint_findings=0`) |
| ruff check | `python -m ruff check scripts/ci/test-linux-v2-owner-request-keys.py` (0.17.5, the version `pyproject.toml` requires) | 0 | `All checks passed!` |
| ruff format | `python -m ruff format --check scripts/ci/test-linux-v2-owner-request-keys.py` | 0 | `1 file already formatted` |
| Changed-path clone gate | `python scripts/ci/jscpd-changed.py --base dcfb1675… --head 21f747f4…` (jscpd 4.3.0 from the existing scratch install, the `package.json` pin) | 0 | `8 changed file(s), 1763 scanned source(s), 1262 clone(s) in the corpus, 0 tolerated among the added lines, none new`; a notice records that `docs/archive/CLOSED-TODO.md` carries added lines but was not scanned (its format, path or size is outside the configured scope), so clones inside it are not checked |
| Record cleanliness (the classifier routes a `docs/evidence/**` change to it) | `python scripts/ci/check-record-cleanliness.py --base dcfb1675… --head 21f747f4…` | 0 | `1 changed record(s) pass the root-only check` |
| Citation existence (the classifier routes a `docs/archive/CLOSED-TODO.md` change to it) | `python scripts/ci/check-citation-existence.py --base dcfb1675… --head 21f747f4…` | 0 | `1 changed document(s) pass the path and symbol check (667 path citation(s))`; three notes name pre-existing ignore-rule-covered citations |
| Sanitizer, per record | `python scripts/ci/model-routing-record-sanitize.py --check --root-only <file>` for this record, the plan and `docs/TODO.md` | 0 | no output |
| Sanitizer, `CLOSED-TODO` | same command on `docs/archive/CLOSED-TODO.md` | 1 | `model-routing-record-sanitize refused: a host absolute path is present` — identical on `dcfb1675`'s bytes, so pre-existing and outside this change's added lines |
| Comment cap | `python <HOST-PATH>/.local/state/gdd/scratch/comment-cap.py <clone> dcfb1675` | 0 | no output (no added comment block over the cap) |

Round-0 gate rows that the fix round re-ran unchanged: the new suite, the two suites, actionlint,
ruff, jscpd and the record gates all printed the rows above at the fix head; the earlier
snapshots' scanned-source count differences (1760 before the `origin/main` merge, 1763 after)
were the corpus size moving, not the gate.

## 5. Limits and closure

- Closure: **delivered; acceptance is this PR's CI log on its final head showing the step and its
  5+ tests (conductor confirms at final acceptance).** This goal has no conductor Bind; the
  hosted run is this record's bound acceptance witness. This stage proves the step exists and
  parses (actionlint, the structure suite) and the suite green locally.
- The range gates' rows above are the pre-record snapshot `21f747f4`; after this record was
  written into the same clone, `check-record-cleanliness.py`, `check-citation-existence.py`, the
  per-record sanitize check on this file and `comment-cap.py` were re-run at the resulting head
  and printed the same exit 0 rows, so the delivered record's own bytes are covered.
- The Elixir formatter check was not run on the fixture: the file is outside `control/.formatter.exs`
  inputs, so the `control` job's `mix format --check-formatted` does not cover it, and the
  pi-durability record measured it red at base and delivered alike (pre-existing Styler drift).
  The change there is comment-only with an unchanged line count, so no drift line can move onto it
  (§2; `git diff` shows the two comment lines). `mix format` here would need `control/deps` and a
  compiled Styler, absent in this worktree.
- Writer cases with no row in this fixture (`wsl2_denial`, driven by the rootless-Compose fixture)
  are outside this agreement by construction; the check compares the fixture's rows only.
- `test-jscpd-changed.py` (the clone gate's own fixture suite) was not run: no byte of that gate
  or its suite changed.
