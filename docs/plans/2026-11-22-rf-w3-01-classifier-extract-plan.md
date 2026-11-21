# rf-w3-01 plan — the changed-path classifier as a real file, one audited-corpus source

Contract: `goal-rf-w3-01-classifier-extract.md` (GDD refactor wave 3, 2026-11-21, conductor
reconciled). Baseline for every coordinate here: this worktree's HEAD
`5c4d59f5e91edf56f6f2c7f0d3dddcd45427027c` (`5c4d59f5`, `origin/main` at plan time, carrying the
contract's dispatch gates — #157/`6a815c0f` verified an ancestor, W2-01 in the `legacy-route-audit`
job, W2-17 as the HEAD commit), branch `refactor/rf-w3-01-classifier-extract`. Binding inputs: the
contract's Change (1)-(5) and Conductor decision (A1/P1-1, P2-1-P2-3, P3, D6); program plan §6
W3-01 and §11 (move slices run the citation gate over the full audited corpus); findings
`scripts-ci.md` F2 and §4 S2 (tool logic inside a data file; the corpus written twice, held together
only by the suite).

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `.github/workflows/ci.yml` | modified | the `changes` job's Classify step runs the extracted file (same `id: paths`, pull-request-only `if:`, `shell: bash`); the step/job comments reconciled; the `legacy-route-audit` fixture-suite comment reconciled. Nothing else in the file changes. |
| `scripts/ci/classify-changed-paths.py` | new | the heredoc program, moved verbatim except the two contract-mandated edits: the shared-corpus import and the family derivation. |
| `scripts/ci/audited_paths.py` | new | `AUDITED_PATHS`, the one definition in `scripts/ci`. |
| `scripts/ci/check-citation-existence.py` | modified | the local `AUDITED_PATHS` definition becomes `from audited_paths import AUDITED_PATHS`; the comment beside `PROGRAM` names the shared module; no other statement changes. |
| `scripts/ci/test-ci-changed-path-classifier.py` | modified | runs the real file (no `extract_classifier`), pins the single corpus definition and both consumptions, adds the two fixtures, keeps every existing pin and mutation with retargeted anchors. |
| `docs/plans/2026-11-22-rf-w3-01-classifier-extract-plan.md` | new | this plan. |

`docs/TODO.md` and `docs/archive/CLOSED-TODO.md` are untouched: no open TODO entry names this slice's
surface (`grep -n "AUDITED_PATHS" docs/TODO.md` → no hits; the `docs/TODO.md:1051` mention of W3-01
is the lockfile-registry follow-up's historical note about which slice owned `ci.yml`, not a closable
entry), so nothing closes and nothing moves.

## The moved program (contract Change 1, move-only proof)

The heredoc body at `5c4d59f5` (`ci.yml:55-95`, dedented past the step's block scalar) becomes
`scripts/ci/classify-changed-paths.py` with exactly these deltas, and nothing else:

1. the module docstring (`#!/usr/bin/env python3` + a docstring, the `scripts/ci` convention) and the
   `from audited_paths import AUDITED_PATHS` import;
2. the in-heredoc comment above `run_citations` repointed at the shared corpus and the fourth tool
   path;
3. the `run_citations` corpus clauses — `path.startswith(b"docs/runbooks/")`,
   `path == b"docs/design/quality.md"`, `path == b"docs/archive/CLOSED-TODO.md"` — replaced by
   `any(path == entry.encode() or path.startswith(entry.encode() + b"/") for entry in AUDITED_PATHS)`,
   and `or path == b"scripts/ci/audited_paths.py"` added to the tool clauses.
4. the two statements the pinned `ruff format` reflows now that the program is a linted file
   rather than YAML text (the `paths = …split(b"\0")` assignment and `run_heavy = any(…)` collapse
   onto one line each). The gate requires `ruff format --check scripts/ci` clean at 0.17.5, so the
   reflow is mandatory; the measured behaviour is unchanged (all sixteen pre-existing fixture
   triples are re-measured by the suite).

Every other line — the `git diff --name-only --no-renames -z HEAD^1 HEAD` invocation in the cwd, the
`run_records` predicate, the three `GITHUB_OUTPUT` writes — is byte-identical to the heredoc, and the
two reflowed statements differ only in whitespace. The proof command and its output are recorded in
the final report: a `diff` of the dedented heredoc from `git show 5c4d59f5:.github/workflows/ci.yml`
against the new file, which shows only the four deltas above.

The step becomes:

```yaml
      - name: Classify changed paths
        id: paths
        if: github.event_name == 'pull_request'
        shell: bash
        run: python3 scripts/ci/classify-changed-paths.py
```

`id: paths` and the pull-request-only `if:` stay because the outputs' fold reads
`steps.paths.outputs`, and a step GitHub skips leaves every output unset; `shell: bash` stays as
written. `python3 <path>` puts the script's own directory on `sys.path[0]`, which is how the
sibling imports (`audited_paths` here, `revision_scope` and `audited_paths` in the gate) resolve in
production. `grep -c "python3 - <<'PY'" .github/workflows/ci.yml` drops 1 → 0, and the one remaining
`<<'PY'` in the file (`ci.yml:448`, `python3 - ../scripts/cleanup-linux-pi-processes.sh <<'PY'`) is a
different program and stays (W6-01's set).

## One corpus source (contract Change 2)

`scripts/ci/audited_paths.py`:

```python
#!/usr/bin/env python3
"""The audited document corpus the citation existence gate and the classifier share."""

AUDITED_PATHS = ("docs/runbooks", "docs/design/quality.md", "docs/archive/CLOSED-TODO.md")
```

The gate imports the name and keeps using it as the `git diff` pathspec at its one call site
(`revision_scope.changed_paths(repository, base, head, AUDITED_PATHS)`) — same pathspec use, no
behaviour change. The classifier imports the name and derives the document family from the tuple,
`path == entry or path.startswith(entry + "/")` for every entry, against the diff's bytes.

Trade-off, accepted explicitly by the contract (P2-1): the derivation is a conservative superset of
the three literals it replaces — a commit that swaps a file and a directory of the same name would
now match where the literals did not. Nothing the heredoc matched is lost; the only extra matches are
a bare `docs/runbooks` file and children of the two file entries, i.e. a file↔directory swap. The
alternative — keeping the literals and importing the corpus only for a separate agreement check —
was rejected: it leaves the corpus written twice, which is the defect the slice removes.

The shared module exists rather than an import of the gate directly because
`check-citation-existence.py`'s hyphenated name cannot be imported by name; the findings' sanctioned
"tiny shared module" alternative. Both consumers use `from audited_paths import AUDITED_PATHS`
(never `import audited_paths` + attribute access) so the suite can pin the consumption with a bare
`Name` load, and the suite reads `getattr(module, "AUDITED_PATHS")` from the shared module. Every
mutated program copy runs from the suite's scratch root, which receives the real `audited_paths.py`,
so a mutation cannot pass by an import accident (P2-2/P2-3).

The run-citations tool literals become four: the gate, its fixture suite, `revision_scope.py` and
the corpus module itself — the corpus now lives in that module, so an edit to it must still gate the
citation job. `run-heavy` is a catch-all for `scripts/ci/audited_paths.py` (neither under `docs/`
nor `*.md`), so its fixture's pinned triple is `(True, False, True)` (the conductor's A1/P1-1
correction of the review's `(False, False, True)`).

## The suite retarget (contract Change 3/4/5)

Deleted: `extract_classifier`. Retargeted: `setUpClass` runs
`scripts/ci/classify-changed-paths.py` itself (the real file, not a copy), and
`classifier_step_keys`/`check_classifier_step` anchor on the step whose `run` invokes that file,
still pinning `id: paths`, the pull-request-only `if:` and `shell: bash`, and additionally pinning
that the workflow text carries no remaining `python3 - <<'PY'` heredoc.

`predicate_literals` stays for what is still literal-based: the records predicate and the
run-citations tool paths. The audited-family coverage check moves from "a trailing-slash literal is
present" to:

- `check_corpus_single_definition()` — an AST scan of every `scripts/ci/*.py` for a module-level
  assignment to `AUDITED_PATHS`: exactly one file defines it (`audited_paths.py`), and both consumers
  import the name (the gate also *loads* it, and the suite cross-checks the gate module's imported
  value against the shared tuple at runtime);
- `check_run_citations_consumes_corpus()` — the AST of the `run_citations` assignment must contain a
  `Name` load of `AUDITED_PATHS` and mention the four tool-path literals; an inlined copy has no such
  load and reddens;
- `check_family_fixture_coverage()` — every corpus entry that is a directory in this tree has a
  fixture adding a *new* path under it (its triple pinned with every other fixture's), so the
  derivation is exercised against a path the corpus literals never named;
- the per-entry "explained literal" loop keeps refusing a path literal in `run-citations` that no
  corpus entry or tool path explains. The loop filters its literals to path-shaped ones, because the
  derivation composes its comparisons with a bare `b"/"` separator constant, which names no path.

New fixtures (16 → 18):

| Fixture | Diff | Pinned triple | What it pins |
| --- | --- | --- | --- |
| `docs-runbooks-added` | add `docs/runbooks/new-runbook.md` | `(False, False, True)` | a *new* path under a directory corpus entry still gates the citation job (the family, not a literal) |
| `audited-paths-modified` | modify `scripts/ci/audited_paths.py` | `(True, False, True)` | the corpus module itself gates the citation job (its own fourth tool literal) and is heavy |

The four citation tool paths are registered in `CITATION_TOOL_FIXTURES` (the new one included) and
every tool path has stand-in bytes in `BASE_FILES` (the corpus module included), so the mutation that
drops the corpus literal from the predicate reddens the corpus fixture instead of only the un-run
draft PR.

Mutation cases (all run in-memory copies; the files under test are never touched):

| Mutation | Red witness |
| --- | --- |
| programs: drop the `docs/evaluations/` clause | `evaluations-record-added` flips `run-records` |
| programs: make the heavy predicate ignore `docs/`+`.md` | `docs-runbooks-modified` flips `run-heavy` |
| programs: drop the records `revision_scope.py` clause | `revision-scope-modified` flips `run-records` |
| programs: drop the citations `revision_scope.py` clause | `revision-scope-modified` flips `run-citations` |
| programs: excuse only `.md` for `docs/` | `docs-json-modified` flips `run-heavy` |
| programs: **make the corpus family exact-match only** (`path == entry.encode()`) | `docs-runbooks-added` flips `run-citations` (retargeted from the old prefix-narrowing case) |
| programs: **drop the corpus module's tool literal** | `audited-paths-modified` flips `run-citations` |
| programs: **inline a literal copy in place of the `AUDITED_PATHS` consumption** (import removed) | `check_run_citations_consumes_corpus` raises — and the mutation is *run* first to show it routes the added-runbook fixture identically, so the check refuses it for the inlined copy, not for a behaviour change |
| workflow: re-gate `citation-existence` on `run-heavy` | `check_job_gating` raises |
| workflow: re-gate `coverage-daemon` | `check_coverage_plane_routing` raises |
| workflow: drop a coverage job's advisory flag | `check_coverage_jobs_are_advisory` raises |
| workflow: drop a coverage upload's `if: always()` | `check_coverage_artifacts` raises |
| workflow: drop a coverage job's working directory | `check_coverage_artifacts` raises |
| workflow: condition `legacy-route-audit` | `check_job_gating` raises |
| workflow: drop the `run-records` fold | `check_output_folds` raises |
| workflow: run the classifier step off pull requests | `check_classifier_step` raises |

The suite prints each fixture's measured triple (`fixture <name>: run-heavy=… run-records=… run-citations=…`)
while it measures them, so the judging artifact's 18 triples are the suite's own output rather than a
re-run by hand.

## The 18 pinned triples (fixture order)

| # | Fixture | Triple (`run-heavy, run-records, run-citations`) |
| --- | --- | --- |
| 1 | `docs-runbooks-modified` | `(False, False, True)` |
| 2 | `docs-runbooks-added` *(new)* | `(False, False, True)` |
| 3 | `root-markdown-modified` | `(False, False, False)` |
| 4 | `docs-json-modified` | `(False, False, False)` |
| 5 | `evaluations-record-added` | `(False, True, False)` |
| 6 | `evidence-record-added` | `(False, True, False)` |
| 7 | `sanitize-tool-modified` | `(True, True, False)` |
| 8 | `cleanliness-tool-modified` | `(True, True, False)` |
| 9 | `quality-design-modified` | `(False, False, True)` |
| 10 | `closed-todo-modified` | `(False, False, True)` |
| 11 | `daemon-code-modified` | `(True, False, False)` |
| 12 | `mixed-docs-and-code` | `(True, False, True)` |
| 13 | `rename-across-docs-boundary` | `(True, False, False)` |
| 14 | `runbook-deleted` | `(False, False, True)` |
| 15 | `revision-scope-modified` | `(True, True, True)` |
| 16 | `citation-tool-modified` | `(True, False, True)` |
| 17 | `citation-tests-modified` | `(True, False, True)` |
| 18 | `audited-paths-modified` *(new)* | `(True, False, True)` |

No existing triple changes: the two new fixtures add paths whose triples are the pinned values above,
and the moved program's outputs for the other sixteen diffs are what the heredoc produced (the family
derivation only widens). The suite measures this rather than asserting it.

## Evidence plan (contract Gates; all foreground)

```
python3 scripts/ci/test-ci-changed-path-classifier.py
python3 scripts/ci/test-check-citation-existence.py
python3 scripts/ci/test-no-legacy-routes-audit.py
python3 scripts/ci/test-check-record-cleanliness.py
ruff check scripts/ci                     # ruff 0.17.5, the config's required-version
ruff format --check scripts/ci
git diff --check
python3 scripts/ci/check-citation-existence.py --base <base> --head <snapshot>
python3 scripts/ci/check-citation-existence.py --base $(git rev-list --max-parents=0 HEAD) --head <snapshot>
diff <(git show 5c4d59f5:.github/workflows/ci.yml | dedented heredoc) scripts/ci/classify-changed-paths.py
git grep -n AUDITED_PATHS
git diff HEAD | grep -iE '<profile-segment>/'   # the conductor's host-path rule
```

The citation gate binds revisions, and this stage leaves the tree uncommitted: both runs use a
dangling snapshot commit built from a temporary index (`GIT_INDEX_FILE=<temp>; git read-tree HEAD &&
git add -A && git write-tree && git commit-tree <tree> -p HEAD`), so the gate reads the increment's
own bytes and neither the real index nor the working tree is touched. The scoped run
(`--base HEAD --head <snapshot>`) audits the documents this change modifies; the full-corpus run
(root-commit base) audits every audited document at the snapshot head — the §11 requirement for a
move slice. Expected: exit 0 both runs; the full-corpus run reports 10 changed documents and the
path-citation count the baseline reports ± the increment's own new documents (this increment changes
none of the audited documents).

Actionlint on the edited workflow is run as extra, non-contract evidence if the pinned host binary is
available (the `legacy-route-audit` advisory step's oracle).

CI-only oracles stay with the operator: one real CI run on the PR (`gh run view --log`, the `changes`
job running the extracted file, `legacy-route-audit` running the retargeted suite) needs a pushed
branch, which this stage must not create. Recorded as the one residual gap in the report.

## Evidence (measured 2026-11-22 on the uncommitted tree)

| Command | Result |
| --- | --- |
| `python3 scripts/ci/test-ci-changed-path-classifier.py` | **exit 0**, `Ran 29 tests`, `OK`; 18 fixture report lines printed, e.g. `fixture docs-runbooks-added: run-heavy=false run-records=false run-citations=true` (all 18 in the report) |
| the same suite under WSL Ubuntu-24.04 | **exit 0**, `Ran 29 tests`, `OK` |
| `python3 scripts/ci/test-check-citation-existence.py` | **exit 0**, `Ran 49 tests`, `OK` (Windows host, 123 s; WSL: 25 s, `OK`) |
| `python3 scripts/ci/test-check-record-cleanliness.py` | **exit 0**, `Ran 19 tests`, `OK` (Windows host, 42 s; WSL: 11 s, `OK`) |
| `python3 scripts/ci/test-no-legacy-routes-audit.py` | Windows host: `FAILED (failures=11)`, every failure `/bin/bash: C:/…/scripts/audit-no-legacy-routes.sh: No such file or directory` — the suite plants into the Windows temp dir and invokes `/bin/bash` on a Windows path; identical 11 failures on the pre-change baseline run. WSL with this host's inherited `PATH`: `FAILED (failures=11)` on matcher status 127, because `command -v rg` finds the mounted Windows mise shim (`/mnt/c/.../mise/shims/rg`), whose `exec rg` fails. WSL with `PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`: **exit 0**, `Ran 6 tests`, `OK`. Unrelated to this change (no legacy-route surface touched); the environment note is recorded rather than the red. |
| `ruff check scripts/ci` (ruff 0.17.5) | **exit 0**, `All checks passed!`, 53 files |
| `ruff format --check scripts/ci` (ruff 0.17.5) | **exit 0**, `53 files already formatted` |
| `git diff --check` | **exit 0** |
| `actionlint -shellcheck= -pyflakes= .github/workflows/ci.yml` (1.7.12) | **exit 0**, no output |
| `check-citation-existence.py --base HEAD --head <snapshot>` | **exit 0**, `no audited document added or modified between 5c4d59f5e91e and d1337df9668b; nothing to check` (the expected pass: this change touches no audited document) |
| `check-citation-existence.py --base <root commit> --head <snapshot>` | **exit 0**, `10 changed document(s) pass the path and symbol check (536 path citation(s))` (the contract's `502` was the count at `a1142d4e`; the corpus has grown since) |
| move-only `diff` (dedented heredoc at `5c4d59f5` vs the new file) | only the four documented deltas |
| `grep -c "python3 - <<'PY'" .github/workflows/ci.yml` | `0` (was `1`) |
| `git grep -n AUDITED_PATHS` + grep of the untracked files | one assignment (`scripts/ci/audited_paths.py:4`), two `from audited_paths import AUDITED_PATHS` consumers, the gate's load at `check-citation-existence.py:779` |
| pre-change baseline (`git grep` before the edit) | the assignment was at `check-citation-existence.py:140` and the classifier literals lived in `ci.yml`; no `audited_paths.py` |

The snapshot is a dangling commit built from a temporary index
(`tree=1f9cb2d8ca8f927c94d960a2a4949de48a134ee1`, `commit=d1337df9668b21ac60b949f9fb0fa5bbbb609062`, parent `5c4d59f5`) — the real
index and working tree were not touched, and no branch or reference was created.

Mutation reds, measured on the shipped bytes (each mutation applied to
`scripts/ci/classify-changed-paths.py` with `apply_patch`, the suite run, the file restored and the
restoration verified by matching pre-mutation md5):

| Mutation | Observed |
| --- | --- |
| family derivation narrowed to `path == entry.encode()` | **exit 1**; `fixture docs-runbooks-added: … run-citations=false`; `test_every_directory_corpus_entry_covers_a_new_path` fails with `expected (False, False, True), got (False, False, False)` |
| `or path == b"scripts/ci/audited_paths.py"` dropped | **exit 1**; `fixture audited-paths-modified: … run-citations=false`; 4 failures incl. `test_the_run_citations_predicate_consumes_the_shared_corpus` |
| corpus consumption replaced by an inlined copy (import removed) | **exit 1**; `Ran 29 tests`, `FAILED (failures=3)`; `AssertionError: the run_citations predicate must load AUDITED_PATHS from the shared corpus module audited_paths.py; an inlined copy of the tuple is not the shared source`; the mutated program still routes the added-runbook fixture correctly, so the check refuses the copy, not a behaviour change |

Restored files: `md5sum -c` against the pre-mutation hashes → `OK` for both the program and the suite.

Not run here (operator stage): the real CI run on the PR — this stage must not commit or push.

## Decisions and trade-offs

1. **The family derivation lives in the predicate, not beside it.** The corpus tuple is loaded by
   name inside the `run_citations` assignment, so an inlined copy is mechanically detectable and the
   bytes conversion is visible where the comparison happens. The contract's own wording
   (`path == entry or path.startswith(entry + "/")`) is transliterated into the bytes domain with
   `entry.encode()`; the b"/" separator is the only composed constant, which the suite's path-shaped
   literal filter accounts for.
2. **The suite runs the real file; mutation copies run text.** Running the file is what makes the
   suite evidence for the extracted program (the heredoc-extraction step is gone); mutations still
   write their mutated text to the scratch root, with the real `audited_paths.py` beside it so the
   import resolves exactly as production does.
3. **Two new fixtures rather than a synthetic unit check.** The family and the corpus self-trigger are
   routing behaviour of the real program on a real diff; a fixture repository is the same oracle the
   other sixteen pins use, and each new pin reddens under its own mutation.
4. **`shell: bash` and the step keys stay.** GitHub's default shell for `run` on Linux is bash, but
   the keys are the routing: the suite pins them, and a skipped step must remain impossible for a
   pull request.
5. **The gate's docstring keeps its prose corpus description.** The single-*definition* pin is what
   the contract demands; restating the corpus in the gate's prose is not a second source, and the
   classifier suite's cross-check (gate module value == shared tuple) makes a diverging restatement
   impossible to ship silently.

## Out of scope

- The other `ci.yml` inline programs (`ci.yml:448` and the PowerShell blocks) and the contracts
  composite action — W6-01, one program per PR.
- Job/needs/output-structure changes: the `changes` job's `outputs:` folds, every job's gating and
  the three coverage jobs are pinned, not edited.
- `scripts/ci/revision_scope.py` behaviour.
- The other `scripts/ci` defect fixes (W3-16 owns eight other files in that directory).
- Ruff configuration changes (`pyproject.toml` is W1-11's).
- `docs/design/quality.md`: its citation-gate amendment describes the gate's semantics, not the
  corpus's location or the classifier's shape, so this change leaves no statement in it stale.
