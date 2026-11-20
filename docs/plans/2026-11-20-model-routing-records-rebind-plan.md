# model-routing-records-rebind plan — render the two remaining 0008b records and document receipt/tooling binding

Contract: `goal-model-routing-records-rebind.md` (GDD, 2026-11-20). Baseline for every coordinate in
this plan and in the delivered records: this worktree's HEAD
`1b9d0c46f789eae3fba2e1febe84ade69d8062f3` (PR #119's branch tip, the contract's stacked base
amendment; `origin/main` at `fdf332dcbcc8c8dc2e59f69703972ec3578c74de`), whose
`scripts/ci/model-routing-record-sanitize.py` is the widened tool that flags the two records.

## Deliverable shape

1. `docs/evaluations/model-routing/0008b-go-bounded-regression-strong.json` and its `.review.json`
   rendered in place with `scripts/ci/model-routing-record-sanitize.py --root-only` (the scope the
   record-cleanliness gate runs and the scope #101 rendered the same two files in). One substitution
   per file: the doubly escaped backslash-run UNC specimen the reviewer quoted while describing
   forbidden-backslash test cases becomes a `path-sha256:` token; nothing else in either file
   changes.
2. Every digest that binds a rendered file rebound, transitively: the receipt's
   `evidence.frozen_review_sha256` and `review.evidence_sha256`; `0008b-policy.json`'s strong-arm
   `frozen_input_digests.review_sha256` and `receipt_sha256`; and
   `docs/evidence/goal-0008b-go-model-routing-2026-11-17.md`'s quoted strong receipt digest and
   candidate-policy digest. A whole-tree scan shows no other full-length or truncated referrer.
3. `docs/evaluations/model-routing/README.md`: the two records leave the accepted-false-positive
   paragraph, and a new section states the rule "a receipt verifies only under the tooling revision
   it binds (`tooling_sources`); re-verify with that revision, not with current tooling" plus a table
   covering every published receipt (all fourteen) with the run store, the bound tooling revision and
   the observed `accept` outcome under the current tooling.
4. `docs/archive/CLOSED-TODO.md`: the ratchet-baseline entry's "(superseded 2026-11-20 …)" note
   becomes the closure pointer, and a new closed entry carries the old→new digest cascade and the
   gate evidence.
5. No source, test or CI change: the tool, the runner and the cleanliness gate are untouched by this
   increment (contract non-goal).

## Exact edge-case semantics the contract demands

- **Scope.** `--root-only`: it is the check the CI job runs, and it reproduces #101's committed end
  state for these files. The default scope would additionally tokenize three repo-relative prose
  fixtures in each record (`/definitions/Microusd`, `/etc/passwd`, `/definitions/CommitPath`) that
  are not host paths, corrupting the prose - the same reason the 0008c records keep their literal
  route fragments.
- **The matched span.** The specimen is spelled at two JSON quoting levels (a backslash-run UNC
  shape whose separators are doubled again in the stored bytes) and its final backslash is the
  escape of the closing quote, not a path byte; the renderer's odd-trailing-run rule keeps it
  outside the token, so the record stays valid JSON and parses to the same document with exactly
  one changed value.
- **Determinism.** The CLI render (stdout redirected) is byte-identical to the in-process
  `sanitize_text` render; re-rendering the rendered bytes changes nothing; the two files' new
  digests are stable over LF bytes (`eol=lf`).
- **Rebinding.** Digests are recomputed over the final bytes after each cascade step (the receipt's
  internal rebind changes the receipt's own digest), and every recorded value must equal
  `sha256(referenced file)`. The rendered review is referenced by the receipt (two fields) and the
  policy (strong arm); the rendered receipt by the policy (strong arm) and the evidence record; the
  changed policy by the evidence record.
- **Accept table.** The table's outcome column is an observed `accept` run per receipt, not a
  classification: each row records the run-store run directory, the command template and the exact
  refusal under the tooling of this revision. Fourteen runs, fourteen refusals, three causes: older
  attempt validation (0008a/0008b/0008c: stored gate results missing 30/24/13 currently required
  result keys), dependency provisioning (0008d: the run record predates the `environment.provisioning`
  block the current derivation requires for Node-tooling tasks), and the grown tooling-source set
  (the structural cause behind every row). The store roots are host-specific and coordinator
  private, so the committed README names them relative to their store (`<store>/runs/...`) and never
  publishes a root; the full transcripts (with the real roots) are retained outside Git and their
  outcomes recorded in `answer.md`.
- **A receipt that is refused is still evidence.** Refusal under current tooling is by design
  (owner decision Q2 A): the rule says re-verification uses the bound revision, and the table is the
  documented boundary, not a defect list.

## Evidence plan

- Before/after: the whole-corpus `--check --root-only` sweep over every tracked record (111 files;
  before: 2 dirty - the two 0008b-strong files; after: 0), and the cleanliness gate itself in the CI
  scope over the changed set (see "Gates" below).
- Digest resolution: `resolve-digests.py` (retained under the GDD task's `evidence/`, run from the
  repo root): resolves the eleven policy/receipt bindings of the 0008b class, scans every tracked file
  for the three pre-rebind digests (must be absent outside `docs/archive/CLOSED-TODO.md`, which quotes
  them as the cascade history) and the three rebound digests (referrer sets must match), and re-runs
  the corpus sweep. Output recorded in `answer.md`.
- Per-receipt `accept`: fourteen runs against the four series' run stores, one transcript each
  (command + exit code + message), summarized in the README table and recorded in `answer.md`.
- Gates: the six Python suites the records README lists under "Gates"
  (`test-model-routing-evaluation.py`, `test-model-routing-omp-usage.py`,
  `test-model-routing-record-sanitize.py`, `test-check-record-cleanliness.py`,
  `test-model-routing-source-subject.py`, `test-linux-v2-source-subject.py`),
  `check-record-cleanliness.py` in the CI scope emulated pre-commit with a `git stash create`
  commit as `--head` (the gate reads head blobs, so a commit object is needed and no ref is moved),
  the same gate at `--base origin/main`, and `git diff --check`.

  Resume-run note (2026-11-21): five of the six suites ran green at the frozen tree
  (`test-model-routing-record-sanitize.py` 54 tests, `test-check-record-cleanliness.py` 19,
  `test-model-routing-omp-usage.py` 40, `test-model-routing-source-subject.py` 6 with one skip,
  `test-linux-v2-source-subject.py` 4; exit 0 each). `test-model-routing-evaluation.py` (82 tests,
  most of them spawning real Go fixtures, ~1-1.5 min/test on this host) was started twice: the
  first run reached 31 tests inside its 30-minute job cap, the second was stopped at 27 test
  outcomes (26 passed, 1 skipped, 0 failures; partial output retained as evidence) so it would not
  contend with the verification stage. It is not a contract gate: this increment changes no
  `scripts/ci` tool, and the record-tool suites above cover the tools whose bytes this change
  depends on.

## Design trade-offs

- **Render in place vs. keep as produced.** Owner decision Q1 B overrides the earlier "left
  unedited" note: the flags are false positives, but the render-and-rebind mechanism is the repo's
  documented remedy (#83, #101) and the check stays un-narrowed. Rendering cost is one token per
  file plus the digest cascade.
- **Per-receipt table rows (14) vs. per-series rows (4).** Per receipt, because the contract says
  "cover every published receipt" and two rows of a series can differ (the two rendered 0008b-strong
  files diverge from their store copies in ways the sibling files do not).
- **Bound revision vs. bound `tooling_sources` digests.** The table names each series' revision/tag
  (`4644fd7`, `0008b-produced-tooling` = `253a701`, `0008c-produced-tooling` = `cab222b`,
  `73e083f`), verified by re-deriving the receipts' three recorded source digests at those
  revisions; naming the revision is the actionable form of the rule, the digests are the binding.
- **New README section placement.** After the 2026-11-19 rendering-exception section, before
  "Explicit local limits": the rule governs re-verification of any published receipt and the table
  is the corpus-wide boundary statement, not part of the one-time exception.

## Out of scope, with reasons

- Changing the sanitize tool's patterns or the runner's validation (contract non-goal; the change is
  a records pass, and narrowing the pattern would reopen the false-negative risk the README records).
- Re-accepting older receipts under current tooling (contract non-goal; a receipt is bound to its
  tooling revision, and re-executing a run is the documented alternative, not re-deriving bytes).
- Touching any other record, including the two 0008c records that still carry prose fixtures the
  default scope would tokenize (contract clause 4).
- Re-rendering the two records' store copies: the store copies are historical run state, and
  published receipts diverge from them by design after a render.
