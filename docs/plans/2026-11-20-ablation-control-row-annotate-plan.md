# ablation-control-row-annotate plan — annotate the test-ablation Control row as the as-reviewed artifact

Contract: `goal-ablation-control-row-annotate.md` (GDD, 2026-11-20). Base for every coordinate in
this plan: `origin/main` at `fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (= worktree HEAD,
`fdf332d`). Branch `fix/ablation-control-row-annotate`. Docs-only increment:
`docs/evidence/test-ablation-final-review-2026-09-29.md`, `docs/TODO.md`,
`docs/archive/CLOSED-TODO.md` and this plan file.

## Decision and delivered shape

Owner decision (human owner, 2026-11-20): option A — keep every digest and number in the record as
recorded and annotate the Control row of `docs/evidence/test-ablation-final-review-2026-09-29.md`
as binding the **as-reviewed artifact**, not the committed bytes. No rebinding.

1. `docs/evidence/test-ablation-final-review-2026-09-29.md`: one annotation paragraph inserted
   directly after the six-row "Evidence-report SHA-256 values reviewed" table (currently ends at
   `:152`). The Control row itself stays byte-identical at `:148`, so the archived entry's `:148`
   citation keeps pointing at it.
2. `docs/TODO.md`: the entry and its now-empty `## Model-routing evaluation records` heading are
   removed (the file holds only undone items; five earlier closures removed an emptied heading the
   same way — `git log --all -p -- docs/TODO.md | grep '^-## '` lists them).
3. `docs/archive/CLOSED-TODO.md`: one preamble line naming the moved block and branch, then the
   entry text verbatim under the existing `## Model-routing evaluation records` section with a
   closure statement naming the owner decision, the branch, the annotation's location and the pin.
4. No source, test, CI or tooling edit. CI's path classifier marks this pull request docs-only and
   record-touching, so the record-cleanliness job is the only hosted gate it triggers.

## Identification of the report the row names (evidence, not inference)

Row digests against the committed LF blob of each report at `fdf332d`:

| Row | Report | Row digest | Committed HEAD digest | Match |
| --- | --- | --- | --- | --- |
| app startup | `docs/evidence/test-ablation-app-startup-experiment-2026-09-29.md` | `92a66cbd…` | `92a66cbd…` | yes |
| Control | `docs/evidence/test-ablation-control-experiment-2026-09-29.md` | `e2672276…` | `d8d9ca80…` | **no** |
| contracts | `docs/evidence/test-ablation-contract-experiment-2026-09-29.md` | `73b76e1e…` | `73b76e1e…` | yes |
| Go probe | `docs/evidence/test-ablation-go-probe-experiment-2026-09-29.md` | `c983ee0a…` | `c983ee0a…` | yes |
| CI peer | `docs/evidence/test-ablation-ci-peer-experiment-2026-09-29.md` | `2df04968…` | `2df04968…` | yes |
| inline parser | `docs/evidence/test-ablation-inline-parser-experiment-2026-09-29.md` | `8e81e32f…` | `8e81e32f…` | yes |

Five of six rows name the committed bytes; the Control row is the only exception, and the report it
names is the experiment report by contents, not by label guess: the review's own Control evidence
(`596086…` snapshot, the 800-test run, Elixir 1.19.6/OTP 28/PostgreSQL 17.11) is that file's
content (`test-ablation-control-experiment-2026-09-29.md:13,26,52`). The Control *inventory*
(`test-ablation-control-2026-09-29.md`, `b3f333cd…`) is a different track's record and is not the
row's referent. That path has exactly two committed revisions — `72bcf8e` → `1a7ef5e8…`, and
`0e59d8e` (the #101 render) → `d8d9ca80…` — and neither matches the row.

## Blob walk (contract clause 1)

All-refs walk over every reachable object, then the spelling variants the maintenance entry named:

- `git rev-list --all --objects` → 5,776 objects (3,326 blobs, 2,450 trees/commits);
- every reachable blob hashed as-is and under the eight LF/CRLF × BOM × no-final-newline
  spellings → 26,542 hashes, **0 hits** for `e2672276…`;
- the fourteen `test-ablation*` paths the entry named → 19 distinct blob revisions, 152 spellings,
  **0 hits** — reproducing the entry's own counts.

So the digest matches no committed blob anywhere in this repository at `fdf332d`, which is the
strongest form of the result the contract asks the annotation to record. The walk is re-run at the
end from a saved script, and its command and output are recorded in `answer.md`.

## Exact semantics the annotation states

1. The row binds the Control experiment report as reviewed (the report text delivered to this
   review), not the committed bytes of its path.
2. The digest resolves against no committed revision: 0 hits over the all-refs blob walk at
   `fdf332d`, and 0 hits over the fourteen `test-ablation*` report paths' 19 blob revisions.
3. The other five rows in that table do bind the committed bytes of their reports (verified above),
   so a reader must not generalize the exception to the table.
4. The row and every digest and number in the record stay as recorded (option A); the maintenance
   entry is closed in `docs/archive/CLOSED-TODO.md` on branch `fix/ablation-control-row-annotate`.

Edge-case semantics the contract forces:

- **No new digest in the annotation.** A corrected value (the file's current or pre-render bytes)
  would read as a rebinding, and the row's neighbours were rebound in #101 exactly because their
  digest was meant to name committed bytes. The annotation therefore carries counts and the walk's
  revision, not a replacement digest.
- **Revision-bounded claim.** The 0-hit result is a statement about `fdf332d`; a later commit that
  ever produced the digest would not falsify the annotation as written.
- **No host path, no rendered token.** The record is under `docs/evidence/`, so CI's
  record-cleanliness job runs the sanitize tool's root-only check over it; the added prose is
  host-path free by construction and is checked.
- **Row line number preserved.** The paragraph is inserted after the table, so `:148` still names
  the Control row and the archived entry's citation stays accurate.

## Evidence plan (commands recorded in `answer.md`)

1. Blob walk: the saved script (`--root-only`-clean, no repository file added), command and output,
   including the per-path case and the whole-repository case.
2. `git diff --check` → exit code and output.
3. `python scripts/ci/check-record-cleanliness.py --base origin/main --head HEAD` → the prescribed
   command; locally vacuous because `HEAD == origin/main`, recorded as such.
4. Non-vacuous equivalent of the same gate over the worktree content: a temporary index
   (`GIT_INDEX_FILE`, the real index untouched) plus an **unreferenced** `git commit-tree` at the
   worktree bytes (`-p HEAD`, no ref moved, nothing committed) so the gate sees exactly the one
   changed record; command, record count and exit code recorded.
5. `python scripts/ci/model-routing-record-sanitize.py --check --root-only
   docs/evidence/test-ablation-final-review-2026-09-29.md` → exit code.
6. `git status --porcelain` at the end, showing the four changed paths and nothing else.

No measurement/benchmark harness is involved, so the host's harness/suite sequencing rule is not
engaged.

## Out of scope (contract clause 4)

Rebinding the row to committed bytes (option B, explicitly not chosen); any other `docs/TODO.md`
entry; the 0008c records and the sibling record-rebinding contract; a new repository gate or
checker for record digest bindings (a separate open item tracks that idea in
`docs/archive/CLOSED-TODO.md`).

## Design trade-offs and decisions

1. **"Pinning test names" in the contract's change line versus a docs-only increment.** No
   behavior changes, so there is no failing-then-passing regression test to add, and clause 3 makes
   a test that passes on pre-fix code a defect. Chosen: the archived closure names the pin as the
   checks that actually decide this change — the record-cleanliness gate the path classifier
   triggers for `docs/evidence/` changes and the sanitize tool's root-only check over the record —
   plus the reproducible blob walk. Rejected: building a digest-resolution checker here (that is a
   new gate, out of clause 4's scope, already tracked as an open archive item, and would put
   tooling bytes into a docs-only increment).
2. **Annotation as a paragraph under the table, not a row change.** The row is the recorded
   artifact; the annotation's job is to explain it. Editing the row's cell (adding a qualifier
   inside the digest cell) would alter the recorded table and make the `:148` cell unquotable.
3. **Archive placement inside the existing section.** `docs/archive/CLOSED-TODO.md` already has a
   `## Model-routing evaluation records` section whose items keep their original text and marker
   and append a closure statement; this closure follows that in-section shape rather than appending
   a new section at the end.
4. **Plan file in the repository.** `docs/plans/` is the repository's designated plan location and
   the most recent docs-only increment (`2026-11-20-docs-followups-2-plan.md`) shipped its plan in
   the same place.

## Residual risks

- The "as-reviewed artifact" text itself is not recoverable from this repository (by the walk's own
  result); the annotation can only state that the recorded digest names it and resolve nothing.
- The walk's counts are point-in-time at `fdf332d`.
- The record's own committed digest is referenced by no other file (`91ffbc6c…` before this change,
  grepped across the tree), so this edit breaks no digest binding.
