# ci-classifier-gates plan — the backticked-citation existence check and the changed-path classifier fixture

Contract: `goal-ci-classifier-gates.md` (GDD, 2026-11-20). Baseline for every coordinate in this
plan: `origin/main` at `fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, this worktree's HEAD),
branch `fix/ci-classifier-gates`.

Two `docs/TODO.md` entries under `## Record and documentation hygiene`, in the order the contract
fixes: (1) the path-and-symbol existence check for backticked citations, (2) the fixture test for the
`changes` job's changed-path classifier. Entry 1 goes first because it adds a classifier output;
entry 2 then pins the post-change routing.

## Deliverable shape

1. `scripts/ci/check-citation-existence.py` — the gate: for a revision range, select the audited
   documents the range adds or modifies, then check every backticked repo-relative path citation
   against the head revision's tree and every bound symbol citation against the cited file's head
   blob.
2. `scripts/ci/test-check-citation-existence.py` — its fixture suite (scratch Git repositories).
3. `scripts/ci/revision_scope.py` — the revision-range machinery (base resolution with the
   `origin/main` → `HEAD^` fallback, merge-base narrowing, changed-path selection, head-blob read)
   extracted from `check-record-cleanliness.py` and shared by both gates, so the new gate does not
   fork a hardened, CI-event-shaped algorithm. `check-record-cleanliness.py` keeps its CLI, messages
   and exit codes and delegates to the module; its unchanged nineteen-test suite is the extraction's
   regression evidence.
4. `.github/workflows/ci.yml` — a third classifier output (`run-citations`), the
   `citation-existence` job gated on it (the `record-cleanliness` job's shape at `ci.yml:78,94`),
   the two new fixture suites wired where the existing ones run.
5. `scripts/ci/test-ci-changed-path-classifier.py` — the classifier fixture test: extracts the
   classifier program out of `ci.yml`, runs it over fixture diffs in scratch repositories, and pins
   the post-change routing.
6. Five stale citations in `docs/archive/CLOSED-TODO.md` repaired (the audited corpus must pass
   the new gate: entry 1's verification clause is "passes on the current tree").
7. `docs/TODO.md` loses the two entries (and its now-empty section heading);
   `docs/archive/CLOSED-TODO.md` gains both closures under its preserved
   `## Record and documentation hygiene` section.

## Audited corpus and scoping (entry 1's location clause)

`AUDITED_PATHS = ("docs/runbooks", "docs/design/quality.md", "docs/archive/CLOSED-TODO.md")` —
one constant in the gate, used both as the `git diff` pathspec and as the audit set. Selection
mirrors `check-record-cleanliness.py`: statuses `A`/`M`/`T` in `base..head` with `--no-renames`,
bytes and citations read from the **head revision's blob**, never the working tree. Unchanged
documents are deliberately not audited ("scoped to the changed files it audits"), and deleted
documents have nothing to check.

CI runs the scoped shape (`--base "$CITATION_BASE" --head HEAD`). The whole-corpus run that entry 1's
verification clause asks for is the same code path with the root commit as base:
`--base $(git rev-list --max-parents=0 HEAD) --head HEAD` (every audited document is then an `A`
entry). No second mode was added for it.

## Citation grammar (exact semantics)

A backticked token is a **path citation** when, after stripping a trailing line suffix
(`:` digits, ranges joined by `-`, `,` or an en dash) and a leading `./`:

| Shape | Rule |
| --- | --- |
| file | its last segment is file shaped (`name.ext`, 1-8 alphanumerics after the final dot) or the token carried a line suffix, and the directories above it resolve in the head tree; a single-segment token must itself be a head-tree top-level entry |
| directory | the token ends in `/` |
| glob/ellipsis | the token contains `**`, `...` or `…`; its anchor is the text before the first wildcard, with any trailing `/` removed |

Tokens with whitespace, a scheme (`://`), a leading `/`, `~`, `$`, `-`, or a placeholder (`<`, `>`,
`{`, `}`) are not citations: they are commands, URLs, host paths, or templates. An extensionless
multi-segment token with no trailing slash, no line suffix and no wildcard is deliberately **not**
audited: this corpus cites branches with path-shaped names (`docs/close-linux-platform-flakes`,
`docs/retro-followups`), which are indistinguishable from a directory citation by shape, and the
ambiguity is left to review rather than guessed (fixture:
`test_an_extensionless_token_is_not_a_path_citation`). A bare file name that is not a top-level
entry (`read_model.ex`, `runner.go` — the corpus cites these constantly) is not a path citation
either: a basename names no repo-relative path to resolve, and the entry's own example pairs such a
name with a line number that this gate deliberately does not validate.

A path citation **resolves** when the head revision's tree (`git ls-tree -r -t`) holds the path (or
the glob/ellipsis anchor), or when the checkout's ignore rules cover it as a file or as a directory
(`git check-ignore` on the path and on `path/`, both quiet). The second arm is the deliberate
boundary for generated output: `docs/runbooks/portal.md` documents the staged portal bundle at
`control/priv/static/portal/`, which no index contains *by the repository's own ignore rule*
(`/.gitignore`), and a gate that flagged those mentions would force accurate prose to be degraded.
`check-ignore` exits 128 on its own error, and that refuses the run (exit 2) instead of passing.

A **symbol citation** is a backticked `name/arity`, `Module.name/arity`, `name()` or
`Module.name()` token. It is bound to a path citation when the two are consecutive backticked tokens
in the same paragraph separated by at most 40 characters that contain no sentence terminator and at
most two words, each from a closed connective list (`of, in, at, and, or, is, the, its, from, to,
with, uses, used, calls, see, sees, runs, reads, writes, now, only, by, for, that, so`). A bound
symbol must occur in the cited file's head blob as a whole word. Unbound pairs are left to review —
this is a recall limit, not a passing claim, and it is fixture-pinned as such
(`test_a_symbol_the_prose_does_not_bind_is_not_checked`). The conservative rule is what keeps the
corpus green without a rewrite wave: the wider "any identifier-shaped token within N characters"
rule was measured over the corpus (survey, scratch) and produced false positives (`false`, `legacy`,
commit SHAs, and identifiers belonging to a different file in the same sentence).

Exit codes mirror the sibling gate: 0 when every selected document passes (also when the range
changes none), 1 when a citation is absent, a bound symbol is absent, or a selected document's head
entry is not a regular file blob, 2 when the repository, the revision range, or the ignore query
cannot be used.

## Measured corpus baseline (before → after)

The rule above, applied to the current corpus (ten audited documents), flags exactly five
citations, all in `docs/archive/CLOSED-TODO.md`, all genuine rot from a move or a deletion, and all
repaired in this increment with a revision-pinned form this repo already uses:

| Site | Citation | Repair |
| --- | --- | --- |
| `CLOSED-TODO.md` browser-flake closures (three sites) | `browser/tests/portal.spec.mjs` — deleted by the 0007i cutover (`ca1bdad`) | `git show 5c2e0ad^:` for the case 5c2e0ad replaced, `git show ca29a8e:` for the pre-cutover state |
| `CLOSED-TODO.md:762` | `control/priv/portal_assets/portal.js` — deleted by the same cutover | `git show ca29a8e:control/priv/portal_assets/portal.js` (verified: the blob exists at the cutover's parent) |
| `CLOSED-TODO.md:1776` | `docs/goals/0008d-typescript-model-routing.md:13` — moved to `docs/goals/archive/` | repoint to `docs/goals/archive/0008d-typescript-model-routing.md:13` (line 13 carries the quoted sentence) |

The five generated-output mentions (`control/priv/static/portal/`, `control/priv/static/portal/
index.html`) are explained by the ignore rules and stay as written. The before/after runs, exact
commands and outputs are recorded in the final report.

## Classifier and wiring (entry 2's pins)

`run-citations` joins `run-heavy`/`run-records`, folded with the same event arm
(`github.event_name != 'pull_request' || steps.paths.outputs.run-citations == 'true'`), computed from
the same `--no-renames` diff. Its predicate is the gate's corpus: `docs/runbooks/**`,
`docs/design/quality.md`, `docs/archive/CLOSED-TODO.md`. The predicate is duplicated in the workflow
because GitHub evaluates it there; the duplication is pinned by the classifier fixture test, which
imports the gate's `AUDITED_PATHS` and requires the classifier's own output to turn `true` for a
diff touching each entry — so extending the corpus without extending the classifier fails the test
instead of silently skipping the job.

Job placement: `citation-existence` sits next to `record-cleanliness`, `needs: changes`,
`if: needs.changes.outputs.run-citations == 'true'`, `fetch-depth: 0`, and the same event-fold base
env (`CITATION_BASE`). Fixture suites: `test-check-citation-existence.py` joins the "Check CI gate
helper scripts" step in the `daemon` job (where `test-check-lockfile-registry.py` runs, and where
`scripts/ci` changes already trigger `run-heavy`); `test-ci-changed-path-classifier.py` runs
ungated in the `legacy-route-audit` job, the job the entry names as the pattern and the one job in
`ci.yml` that runs unconditionally.

## The classifier fixture test (entry 2's verification clause)

1. **Execution**: the test extracts the classifier program from the `changes` job's
   `id: paths` step of `.github/workflows/ci.yml` (the `python3 - <<'PY' … PY` block, located by job
   block and heredoc delimiter) and runs it with `sys.executable` in a scratch two-commit Git
   repository, `GITHUB_OUTPUT` pointed at a temporary file. The outputs under test are the program's
   own, not a copy.
2. **Pins** (fixture diffs, one per routing): `docs/**`-only and `*.md`-only diffs report
   `run-heavy=false`; `docs/evaluations/**`, `docs/evidence/**`,
   `scripts/ci/model-routing-record-sanitize.py` and `scripts/ci/check-record-cleanliness.py` report
   `run-records=true`; each audited-corpus path reports `run-citations=true`; a non-`pull_request`
   event folds all three outputs to `true`; `legacy-route-audit` carries neither `needs:` nor `if:`
   and runs unconditionally; the `citation-existence` job runs exactly when `run-citations` is true;
   a rename across the documentation boundary reports both sides (`--no-renames`).
3. **Wiring model**: whitespace-normalized assertions over the job blocks are closed-world: every
   job except `changes` and the declared unconditional job must `needs: changes` and gate on a
   classifier output, so a new job that forgets to gate fails the test instead of passing silently.
4. **Mutation cases**: the routing assertions and the classifier-output assertions are functions of
   the workflow text; a mutation case per pin rewrites that text (drop a `run_records` clause,
   re-gate `citation-existence` on `run-heavy`, add an `if:` to `legacy-route-audit`, drop an output
   fold) and requires the assertion to fail. This is the entry's "fails when any of those routings
   changes", reproducible in the suite instead of by editing `ci.yml` by hand.

## Out of scope

- `docs/TODO.md`'s other entries, and every other open item in the repository.
- `.github/workflows/linux-native-v2-acceptance.yml` and `scripts/ci/model-routing-*.py` (open PR
  #119 owns them; the contract forbids touching them).
- No change to `check-record-cleanliness.py`'s behaviour, CLI, messages or exit codes — the
  extraction is behaviour-preserving by inspection and by its unchanged suite.
- No Elixir/Go/frontend/browser run: nothing under `control/`, `daemon/`, `frontend/`, `browser/`
  or `contracts/` is touched, so the browser render gate and the Elixir recipe are not triggered.
- Line numbers inside citations are not validated and are not repaired (entry 1: "do not validate
  raw `file:line` values"); `docs/design/quality.md`'s `ci.yml:107-110`/`:618` references therefore
  keep their pre-change numbers even though this increment shifts lines below them.
- No second audit mode, no per-file opt-out, no configuration surface: the gate's corpus and
  grammar are code and are pinned by fixtures.

## Evidence plan

Gates actually run, with exit codes, in this order (never a measurement/benchmark harness — this
increment has none):

1. `python3 scripts/ci/test-check-record-cleanliness.py` — before and after the shared-module
   extraction (19 tests, unchanged).
2. `python3 scripts/ci/test-check-citation-existence.py` — the new fixture suite.
3. `python3 scripts/ci/test-ci-changed-path-classifier.py` — the classifier fixture suite.
4. `python3 scripts/ci/check-citation-existence.py --base $(git rev-list --max-parents=0 HEAD)
   --head HEAD` — the corpus-wide run: red (five offenders) before the repairs; green after on a
   dangling snapshot commit built from a temporary index
   (`GIT_INDEX_FILE=<temp> git read-tree HEAD && git add -A && git write-tree &&
   git commit-tree <tree> -p HEAD`), because this stage leaves the tree uncommitted and the gate
   binds a revision: the snapshot reads the repaired documents and the increment's own new files
   with the same code path, and neither the real index nor the working tree is touched
   (observed: exit 1 with five refusals, then exit 0, 365 path citations).
5. `actionlint -color -shellcheck= -pyflakes= .github/workflows/ci.yml` (upstream 1.7.12, the
   documented substitute on this host) plus `shellcheck --shell=bash` over every changed `run:`
   block with each `${{ … }}` replaced by a placeholder word.
6. `git diff --check`.
7. WSL Ubuntu-24.04: the three Python suites above, on the platform CI's jobs use
   (`scripts/ci/` is a path-triggered Linux run per `docs/design/quality.md`'s 2026-11-20
   amendment).

## Decisions and their justification

1. **A classifier output rather than an unconditional job.** Entry 1 points at `ci.yml:78` (the
   `record-cleanliness` job's classifier gate) as the shape to copy; the cost is a duplicated
   corpus predicate in the workflow, which decision-pinned fixture agreement covers. Rejected: an
   unconditional job (no duplication, but ~2 minutes of runner time on every push and pull request
   for a gate that only matters when an audited document changed), and auditing the whole corpus on
   every pull request (it would redden unrelated pull requests for pre-existing rot, which is the
   reading entry 1 rejects by scoping the gate to changed files).
2. **Ignore rules explain absence; the tree decides otherwise.** The entry's harm example is a
   citation "that no index contains" (an ignored `control/deps/ecto/...` path). Under the chosen
   rule that citation is *explained* by the ignore rule and would not be flagged: the gate cannot
   distinguish a dependency path cited as a source location from a generated path cited as output
   without reading the sentence. The alternative — no ignore arm — was measured: it flags five
   further sites in the audited corpus that are accurate prose about gitignored build output
   (`docs/runbooks/portal.md` twice, `docs/runbooks/control-plane-and-daemon.md`, and the
   `:portal_assets` closure), and repairing them would either drop backticks from a path the reader
   must build or cite something else in its place. Recording this as a decision, not a silent
   choice: the gate covers every citation whose absence the repository does not itself declare.
3. **Shared revision scope instead of a copy.** The base/fallback/narrowing algorithm is subtle
   (hardened across `21cf326` and `4aebd05`) and identical for both gates; a second copy would be
   the kind of drift the repository's single-sourcing commits exist to avoid. The extraction is
   behaviour-preserving and its regression evidence is the sibling suite, run before and after.
4. **Conservative symbol binding.** See the grammar section: a mis-parse must not demand a rewrite.
5. **Extensionless multi-segment tokens are not audited.** See the grammar section: branch names in
   this corpus are path-shaped.

## Residual risk

- The classifier predicate and the gate's corpus can still drift for *new* corpus entries that a
  glob in the classifier happens to cover (`docs/runbooks/**` covers a new runbook automatically);
  a new *file* entry outside the globs is caught only by the imported-`AUDITED_PATHS` agreement
  case — which is exactly what that case is for.
- The symbol check's reach is small (ten bindings in the current corpus, all resolving). Its value
  is the fixture-pinned failure arm, not corpus coverage.
- The ignore arm can be satisfied by editing `.gitignore`; that is a deliberate, reviewable
  repository-level statement rather than a hole the gate can close.
