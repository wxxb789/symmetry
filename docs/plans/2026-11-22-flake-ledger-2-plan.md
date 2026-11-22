# flake-ledger-2 plan — three observed flakes recorded in `docs/TODO.md`

Contract: `goal-flake-ledger-2.md` (GDD, conductor-authored 2026-11-22, docs-only). Baseline at
authorship: this worktree's HEAD `d5b14b8579b708150889c76e765d856b7f8c03af`; `git rev-parse
origin/main` and `git merge-base origin/main HEAD` both returned that revision, so every coordinate
was re-derived there. The increment is now committed (`9811a609`, merged as `393bb640` and again as
`e94c2902`), and the gates below are re-derived on the committed range (`origin/main...HEAD`, the
diff `9e360586..e94c2902`, which holds only this increment's two files).

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `docs/TODO.md` | committed | one new section at the end of the file, carrying three `- [ ]` entries (contract Change: "one `docs/TODO.md` entry per flake (or one section with three entries)") |
| `docs/plans/2026-11-22-flake-ledger-2-plan.md` | committed | this plan |

Nothing else changes: no source, test, workflow, doc or record file. The judging artifact is the
`docs/TODO.md` diff (contract clause 2).

Section title and entry lead, in the file's own style (appended the way the file's existing sections
are):

- section: `## Observed flakes, cause unknown: three observations (2026-11-22 CI runs and PR #220 review)`
- entries: one `- [ ]` bullet each, led by the test name (entry 3 leads with the flaked case and
  names its prefix-sharing sibling below), then package/file, observed failure text, where observed,
  reproduction attempts and outcomes, `Cause unknown`, and the next diagnostic step.

## Decisions kept from the entry derivation, re-derived at the baseline

The entry bodies live in `docs/TODO.md`'s three new bullets under the section above — they carry
the names, coordinates, recovered evidence text and reproduction outcomes; this plan keeps the
decisions and points there instead of restating the entries.

- Proximity, not duplication (entry 1): the comment-policy-sweep section's entry for
  `daemon/cmd/symmetry-fake-agent/autonomous.go` records the *unblockable* carrier (its reader is
  never joined); the flake entry records one race-step failure of the closable-carrier test —
  different item, cross-referenced in both so a later sweep does not merge or delete either.
- Not the closed class (entry 2): a fresh test group, not the closed `daemon/internal/platform`
  flake class (helper witness / descendant monitor, `docs/archive/CLOSED-TODO.md`); the file's own
  cgroup-`/` host trap is a distinct condition.
- No additional local attempt (entry 3): this worktree has no frontend dependency tree
  (`frontend/node_modules` absent), and installing one for a load-dependent case is neither the
  recorded reproduction nor cheap.
- The raw-scan suspect, not the bound (entry 1): the cited run's own record (`"Elapsed":0.01`
  against `readerJoinTimeout = 50 ms` at the same head) excludes the join's expiry arm, so the
  `runtime.Stack` scan that matched an exiting reader is what a reproduction should capture; the
  test-side probe change stays deferred (contract clause 3 forbids it here).
- Case name resolved (entry 3): the PR #220 review record names the red run, so the entry names
  `replays the plan approval's decision resolution byte for byte after a lost acknowledgement` and
  keeps the admission case only as the prefix-sharing sibling.
- Ambiguity that stands (entry 3): the "13 shuffled runs / 13-file subset" pairing is not
  recoverable from the repository (the PR #220 plan records shuffle runs over the full 21-file suite
  at seed `20261122`); the entry records the command shape the contract states and no invented file
  list.

## Duplicate and placement check

- `git grep -n -e TestAutonomousReturnReleasesItsInputReader -e TestLinuxOwnerAnchorWire -e
  'goal-attention.test.tsx' -- docs/TODO.md docs/archive/CLOSED-TODO.md` at the baseline → two hits,
  both `docs/archive/CLOSED-TODO.md`, both inside items that name the frontend file as the pin of
  the frozen-attempt release rule (closed 2026-11-20/21) — not flakes, and not in the live
  `docs/TODO.md`, which held no entry for any of the three observed flakes. So the contract's "extend it
  instead of duplicating" branch does not apply; three new entries are correct.
- Adjacent look-alikes checked and cross-referenced in the entries: the look-alike entries named in
  the decisions above (the comment-policy-sweep `autonomous.go` entry, the closed
  `daemon/internal/platform` flake class in `docs/archive/CLOSED-TODO.md`), plus the
  `admission_deadline` and `provider_bridge` flake sections (different packages, tests and
  mechanisms).
- Placement: appended as a new section at the end of `docs/TODO.md`, after the comment-policy-sweep
  section, matching how the existing sections were appended. The file's Rules block is
  untouched: the items are undone, so they stay in `docs/TODO.md`.

## Exact edge-case semantics this contract binds

1. No invented reproduction result: the three contract bullets are reproduced as stated; the two
   added local runs are recorded with their exact commands, counts and exit codes; where the
   contract's own evidence was thinner than the cited artifacts (entry 2's full failure line, entry
   3's case name), the entry carries the recovered text instead of a hedge.
2. No code or test change, no fix, no instrumentation (contract clauses 3-4): the failing surfaces
   keep their bytes, and the entries name the diagnostic step instead of performing it.

## Evidence plan (contract Gates, foreground)

| # | Command | Expectation |
| --- | --- | --- |
| 1 | `git diff --check` | exit 0, no output |
| 2 | the contract's committed-range gate: `git diff origin/main...HEAD` searched case-insensitively for the profile-directory segment the program rules name | empty (the increment is committed, so the committed range is the effective check; the same search is also run over the working diff while the apply-pass edits are uncommitted) |
| 3 | host-path scan over the increment (the committed range plus the working diff): the profile-directory segment, written `<profile-segment>/` in the convention `docs/plans/2026-11-22-rf-w3-01-classifier-extract-plan.md` uses, and any absolute user-profile directory path in any platform's spelling | empty |
| 4 | the contract's escape gate: no backslash escape immediately preceding the letter `u`, over the committed range and the working diff | empty |
| 5 | `python scripts/ci/check-record-cleanliness.py --base $(git merge-base HEAD origin/main) --head HEAD` | exit 0 (the range changes no file under `docs/evaluations/**` or `docs/evidence/**`) |
| 6 | `git diff --name-only origin/main...HEAD` and `git status --short` | the increment's two files, nothing else |
| 7 | census re-run from "Duplicate and placement check": every live `docs/TODO.md` hit belongs to the three new entries, `docs/archive/CLOSED-TODO.md` keeps its two, and no test name appears in two entries | counts below, in the measured table |

No CI-hosted oracle exists for this increment (docs-only, and the stage must not push); the local
test runs below are the only behavioral evidence and they are evidence about *reproduction*, not
about a fix.

## Evidence (measured 2026-11-22; gates re-derived in the apply pass on the committed range)

### Gates (contract clause 5, all foreground from the repository root)

| # | Command (as run) | Result |
| --- | --- | --- |
| 1 | `git diff --check` (working tree and committed range) | exit 0, no output |
| 2 | the contract's committed-range gate over `git diff origin/main...HEAD` | no match (the search exits 1) |
| 3 | the same search over the working diff | no match (the search exits 1) |
| 4 | the contract's escape gate over the committed range and the working diff | no match (the search exits 1) |
| 5 | `python scripts/ci/check-record-cleanliness.py --base $(git merge-base HEAD origin/main) --head HEAD` | exit 0 — `check-record-cleanliness: no record added or modified between 9e3605861956 and e94c29024c65; nothing to check` |
| 6 | `git diff --name-only origin/main...HEAD`, `git status --short` | the two increment files committed; the working tree holds only the apply-pass edits to them |
| 7 | the census grep from "Duplicate and placement check" | `docs/TODO.md:14`, `docs/archive/CLOSED-TODO.md:2` — the 14 are the three entries' own mentions (entry 1 twice: the lead and the `-run` pattern; entry 2 eleven times: the wildcard, its five names, the `-run` pattern and the five verbatim `--- FAIL` lines the entry records; entry 3 once), and the archive's 2 are pre-existing |

Python 3.12.10, as gate 5 ran it.

### Reproduction attempts recorded into the entries

| Surface | Command | Result |
| --- | --- | --- |
| fake-agent race, CI shape | WSL (Linux amd64, go1.27.0), from `daemon/`: `go test -race -count=20 -timeout 600s -v -run '^TestAutonomousReturnReleasesItsInputReader$' ./cmd/symmetry-fake-agent/` | 20/20 `--- PASS`, 0 `--- FAIL`, exit 0 |
| anchor-wire group, job shape | WSL Ubuntu-24.04 as an unprivileged user, go1.27.0, from `daemon/`: `go test -count=5 -timeout 1200s -cover -v -run '^TestLinuxOwnerAnchorWire' ./internal/platform/` | 25/25 `--- PASS`, 0 `--- FAIL`, exit 0, `coverage: 2.1% of statements` |
| anchor-wire group, root session | same command in the default (root) WSL session | 5 `--- FAIL` with `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0` — recorded as the host limit, not as a test failure |
| local `-race` on the Windows host | `go test -race …` (native) | not run: `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` and this host has no C toolchain; the WSL runs above stand in, and the limit is recorded |

Re-run in the apply pass on the committed tree: both WSL surfaces returned the counts recorded above
— 20/20 with 0 `--- FAIL`, exit 0, and 25/25 with 0 `--- FAIL`, `coverage: 2.1% of statements`,
exit 0; the fake-agent re-run had to set `GOTOOLCHAIN=go1.27.0` in the default WSL distro, whose
local toolchain is go1.26.8, and every other part of the two commands is the row's command.

## Design trade-offs and decisions

- One section with three entries, not three sections: the contract allows either, one section keeps
  the three 2026-11-22 observations together and leaves the file's thematic sections untouched.
- Entries are appended, not merged into the existing flake sections: each names a different package,
  test and mechanism, and merging would obscure which observation belongs to which PR/run.
- Cross-references to the nearest look-alikes are written into the entries (not only into this plan),
  because a future dedup sweep reads the entry first.
- No line-number coordinates, unlike older entries: the contract fixes that, and symbols survive the
  refactors this program performs.
- The plan records the one contract ambiguity the cited artifacts do not resolve (the "13 shuffled
  runs / 13-file subset" pairing); the case-name ambiguity it first recorded was resolved from the
  PR #220 review record, which names the red run, and the entry records that name.

## Out of scope, with reasons

| Item | Reason |
| --- | --- |
| Fixing any of the three flakes (instrumenting the join, raising `readerJoinTimeout`, changing the anchor-wire harness, adding a wait to the vitest cases) | Contract clause 4 ("fixing any flake") and clause 3 (any code/test edit = not achieved) |
| Reproducing the flakes on this host beyond the two runs above | Load-dependent failures need the hosted jobs' contention; the entries name the diagnostic step, and inventing a reproduction is explicitly a failure condition |
| Opening or commenting on the CI runs/PRs named in the evidence | Not part of the deliverable; the entries cite run id and PR head, which is the contract's requested form |
| The closed `daemon/internal/platform` flake class and the `admission_deadline`/`provider_bridge` sections | Different tests and mechanisms; each entry states its difference so no future sweep merges them |
| `docs/archive/CLOSED-TODO.md` | Nothing here is closed; the file's rule moves only done items |
| Installing `frontend/node_modules` to re-run the vitest subset | The subset's file list is unrecoverable, so a local run would not be the recorded reproduction; recorded as a limit instead |

## Risks and limits

- The hosted failure texts are re-read from the cited artifacts rather than trusted to the
  contract's summary: run `44976804508`'s race-step record (the reader test's `Elapsed: 0.01` and
  the goroutine dump), run `44973284689` attempt 1's `coverage-daemon` log (the five full failing
  lines) and the PR #220 review record (the failed case's name) were read through the authenticated
  `gh` CLI, and the entries carry the recovered text.
- Entry 3 names the case from the PR #220 review record; that record's own count — 1 failure in 13
  shuffled runs that include the case — is the check on that reading.
- The local passes above are single-session and unloaded for the anchor-wire group; they bound the
  reproduction, they do not disprove the flake.
- The root-session refusal is a WSL-default-user property on this host, not a repository condition;
  the entry records it beside the command so a revalidation does not read it as a test failure.
