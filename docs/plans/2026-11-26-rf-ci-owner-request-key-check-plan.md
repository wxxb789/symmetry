# rf-ci-owner-request-key-check — implementation plan (pin the writer/fixture Goal-admission key agreement)

Contract: `goal-rf-ci-owner-request-key-check.md` (GDD state root, external to the repository).
Dispatch head `8453da12` (main). The accepted design is the contract and the conductor ruling it
cites (residual S3 of `rf-acc-containment-goal-admission`); this file resolves only the choices the
contract leaves open: the parse, the comparison axis, the comment edits, the `ci.yml` line, the
evidence plan, and what stays out of scope.

## 1. Clause census at the dispatch head

| Contract clause | Dispatch head (`8453da12`) | Disposition |
| --- | --- | --- |
| Change 1 new self-test | `scripts/ci/test-linux-v2-owner-request-keys.py` absent; the writer's `GOAL_ADMISSION_CASES` (`write-linux-v2-owner-request.py:32-38`) and the fixture's rows/roles (`provision-linux-v2-control-claims.exs:40-128,150-153,225-239`) exist and agree | implement here (§2.1-2.4) |
| Change 2 mutation witnesses | no fixture of this agreement exists; `test-ci-structure-extraction.py`'s mutation tests are the in-repo pattern | implement here (§2.5) |
| Change 3 `ci.yml` wiring | daemon job's `Check Linux v2 fixture contracts` step at `.github/workflows/ci.yml:993-995` already runs four `test-linux-v2-*.py` suites; no pin in `test-ci-structure-extraction.py` names that step or its body (`grep`) | implement here (§2.6) |
| Change 4 comment glue | writer comment at `:30-31` (2 lines); fixture comment at `:184-185` (2 lines) | implement here (§2.7) |
| Owned set | the new test, `ci.yml` (one line), `test-ci-structure-extraction.py` pins only if needed, the two scripts (comment lines only), `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, plan + evidence | §2, §3 |
| Acceptance | new test green at head with three mutants red; `test-ci-structure-extraction.py` and `test-ci-changed-path-classifier.py` green; actionlint 0 findings; jscpd-changed exit 0; the PR's CI is the conductor's stage | §4 |

Both facts the test pins arrived in `550f96a3` (#294): `git log -S GOAL_ADMISSION_CASES` on the
writer and `-S opencode_goal` on the fixture both report that one commit, so the pre-fix pair is the
available before-state (§4.1).

## 2. Implementation choices the contract leaves open

1. **Writer parse: `ast`, not a regex.** `ast.parse` + `ast.literal_eval` over the module-level
   `CASES` and `GOAL_ADMISSION_CASES` assignments reads exactly the literal, refuses any computed
   or renamed shape with a message naming the symbol, and cannot match a neighbouring line. The
   assignment must be a dict of string keys and a non-empty set of string constants; every goal
   name must be a key of `CASES`, because a goal key that names no case can never be dispatched
   (so it is a typo or a half-done drop, the writer-side mutant's shape).
2. **Fixture parse: bounded regexes plus a line census over a bounded region, refusing on
   anything else.** The region runs from the `  @cases [` line to the next top-level `  def `, so
   the census sees the table and nothing else. Inside it every non-blank line must be the table
   opener, a structural line of the concatenation (`] ++`, `Enum.map(@attr, fn v ->`, `end) ++`,
   `[`, `]`), or a row: a start line `{"<name>"|"<prefix>#{var}", "<profile>", :<harness>,
   :<role>,` completed by `"<description>"}` lines, with interpolated names expanded from the
   referenced attribute's own string list; an element line there that is neither blank, a comment,
   nor a bare quoted string refuses too. An unparseable line refuses with its text, so a shape
   the parser cannot read can never be silently skipped into a smaller set. The two routing facts
   are parsed the same way: the goal-role list from
   `Enum.split_with(@cases, fn … repository_role not in [:pi_durability, :opencode_goal] end)` and
   the fixture's own `request_case` cond (the fixed-name list, the prefix, the collapse target).
   The `@cases` table, `goal_case_specs`' `case repository_role do` arms and the fixture's
   `@expected_fixture_runs` raise remain runtime facts of the fixture; the test does not restate
   them (the arms and the count are loud failures inside a hosted run, which is not the drift the
   contract names).
3. **Comparison axis: the writer case name, with the fixture mapped by its own `request_case`
   cond.** The writer keys on its argv case (`stage_replay` plus the three fixed names), while the
   fixture's `stage_replay_*` family rows are per-hold/per-stage names. The fixture already maps
   each row to the case name its run metadata carries (`schedule/5`'s `request_case` cond), and
   the writer requires `run.get("case") == case` of its argv, so that cond *is* the fixture's
   statement of which writer case a row is dispatched under. The test parses that cond (fixed
   list → identity, prefix → collapse target, else identity) and compares
   `{mapped(row) : row.role ∈ goal roles}` with `GOAL_ADMISSION_CASES` as sets. Both sets are
   printed on failure. One addition beyond plain equality: no *legacy* row may map into
   `GOAL_ADMISSION_CASES`, because such a row's dispatch would hand the writer a legacy
   ClaimResponse under a goal-keyed case (`stage_replay_unprovisioned_refusal` is the existing
   legacy row that survives the prefix rule only through the fixed list, and it must stay out).
4. **Failure shape.** A refusal is a local `Refused` exception whose message names the file and
   the unparseable symbol/shape (bounded: one line plus the set dump for the disagreement case).
   The committed-pair test fails the same way.
5. **Mutation witnesses (inside the test, over mutated copies of the committed bytes).**
   (a) drop `"codex_containment"` from the writer's set → red; (b) add a goal-routed
   `pi_durability` row named for the probe to the fixture's table → red; (c) unparseable shapes →
   red with a bounded reason: the split predicate's role list replaced by a variable, the
   `request_case` cond's prefix arm rewritten, one row's role atom replaced, one
   `@stage_replay_holds` element line replaced by an atom, a second `  @cases [` opener appended,
   and the writer's set assignment starred (`{*CASES,`); (d) remove the
   `stage_replay_unprovisioned_refusal` exemption from the cond's fixed list → red (the
   legacy-side disjointness witness). Each mutation asserts its anchor matched, so a mutated-away
   anchor fails the witness instead of passing vacuously.
6. **`ci.yml`: exactly one line, in the daemon job's existing step.** The new suite joins
   `Check Linux v2 fixture contracts` after `test-linux-v2-host-prerequisite-probe.py`
   (`.github/workflows/ci.yml:998`). No structure pin needs an update: `check_job_timeouts` pins
   the daemon ceiling (30) and step caps against it, and no pin names this step or hashes its
   body (`grep -rn "fixture contracts" scripts/ci/ .github/` → only
   `.github/workflows/ci.yml:993`; the `scripts/ci/`-only invocation prints nothing); the job's
   timeout derivation is therefore untouched. No new job.
7. **Comment glue: point at the test, keep ≤2 lines.** Writer `:30-31` keeps its first line
   (the fact) and its second becomes the pointer to the new suite; the fixture's `:184-185`
   comment above `goal_case_specs` gets the same pointer. Nothing else in either file changes.

## 3. Edge-case semantics

- An empty or missing goal set, a goal name outside `CASES`, a duplicate fixture row name, an
  interpolated row naming an attribute with no string list, and a second `@cases`-shaped opener
  line after the first are all refusals, not guesses.
- The check is insensitive to whitespace, comments, quoting style and argument order inside the
  parsed constructs (both parsers read structure, not byte offsets), and to the number of rows:
  adding or dropping a `stage_replay_*` goal row leaves the mapped set unchanged, because the
  writer dispatches the whole family as `stage_replay`; adding a goal row whose name is not in
  the fixed list and does not carry the prefix changes the mapped set and reddens until the
  writer's key changes with it.
- The `stage_replay_unprovisioned_refusal` carve-out is pinned by the legacy-side disjointness:
  if that name loses its fixed-list exemption, its mapped name becomes `stage_replay`, which is
  a writer goal case, and the check reddens.

## 4. Evidence plan

1. **Before-state.** (a) Run the new check against the pre-#294 writer
   (`git show 550f96a3^:scripts/ci/write-linux-v2-owner-request.py`): it refuses (no
   `GOAL_ADMISSION_CASES`), so the check reddens on the state that predates the keyed branch.
   (b) The three mutation witnesses listed in §2.5 run inside the committed suite; their
   transcripts are the observed reds (the suite passes only because each refusal is expected).
   Gate results (exit codes, counts) go to
   `docs/evidence/rf-ci-owner-request-key-check-2026-11-26.md`: the new suite, the two named
   structure/classifier suites, actionlint (0 findings), jscpd-changed over the change range
   (temp commit built with a scratch index; the pinned jscpd 4.3.0 from the existing scratch
   install), ruff check/format for `scripts/ci`.
2. **No measurement or comparison harness** is part of this contract; the fixtures are static
   parses and their mutants.
3. **Hosted CI**: the PR's own run and its log are the conductor's PR stage; this stage records
   the local gates only.

## 5. Out of scope (with reason)

- `daemon/e2e/production_linux_v2_native_witness_test.go`'s
  `TestLinuxV2WitnessRequiresGoalAdmissionRows` already pins the request-case side of the same
  agreement in the e2e package; this goal's contract names only the writer/fixture pair.
- `register-linux-v2-claim-machine.py`'s goal-assigned runtime specs key on harness metadata, not
  on the case set; a drift there fails loudly in the fixture's runtime assertions.
- The fixture's `@expected_fixture_runs` count and `case repository_role do` arms: loud runtime
  refusals, outside the silent-drift class the contract closes.
- No change to `linux-v2-native-acceptance.sh` (its `prepare_owner_request` pairs are the
  dispatch the mapped comparison models; parsing it is not required by the contract and would add
  a third side to keep in sync).
