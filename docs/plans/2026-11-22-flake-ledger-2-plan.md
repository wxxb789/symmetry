# flake-ledger-2 plan — three observed flakes recorded in `docs/TODO.md`

Contract: `goal-flake-ledger-2.md` (GDD, conductor-authored 2026-11-22, docs-only). Baseline: this
worktree's HEAD `d5b14b8579b708150889c76e765d856b7f8c03af`; `git rev-parse origin/main` and
`git merge-base origin/main HEAD` both return that revision, so every coordinate below is re-derived
there.

## Deliverable shape

| File | State | Content |
| --- | --- | --- |
| `docs/TODO.md` | modified | one new section at the end of the file, carrying three `- [ ]` entries (contract Change: "one `docs/TODO.md` entry per flake (or one section with three entries)") |
| `docs/plans/2026-11-22-flake-ledger-2-plan.md` | new | this plan |

Nothing else changes: no source, test, workflow, doc or record file. The judging artifact is the
`docs/TODO.md` diff (contract clause 2).

Section title and entry lead, in the file's own style (the three existing flake sections are
`## pi provider-bridge close-retry timing flake under host load (PR #155 review)`,
`## Daemon: the `admission_deadline` attach-ack subtest flakes on its wall-clock deadline (PR #217
review)` and `## Harness shared-scaffolding deferrals …`):

- section: `## Observed flakes, cause unknown: three tests (2026-11-22 CI runs and PR #220 review)`
- entries: one `- [ ]` bullet each, led by the test name (or the two candidate names, entry 3),
  then package/file, observed failure text, where observed, reproduction attempts and outcomes,
  `Cause unknown`, and the next diagnostic step.

## Entry inventory, re-derived at the baseline

### 1. `daemon/cmd/symmetry-fake-agent` — `TestAutonomousReturnReleasesItsInputReader`

- Test and package: `TestAutonomousReturnReleasesItsInputReader`,
  `daemon/cmd/symmetry-fake-agent/autonomous_test.go`. It installs the per-test goleak oracle
  (`verifyNoLeakedGoroutines`), drives `runAutonomous` on an `io.Pipe`, answers the decision from
  the output hook, and after the run scans `runtime.Stack(…, true)` for `runAutonomous.func1`
  (`autonomous.go`'s reader closure), failing with `reader goroutine is still alive when
  runAutonomous returned`; then asserts the write end now answers `io.ErrClosedPipe`.
- Observed failure text: that same `t.Fatalf` message plus the goroutine dump (the contract's
  evidence bullet 1).
- Where observed: hosted `daemon` job, race step (`go test -race -count=1 -timeout 600s -json
  ./...`, `.github/workflows/ci.yml` step "Run race tests with the same stdio-only caller
  boundary"), run `44976804508`, PR #225 head `d45dc45f`.
- The head is a Control-only code change: `git diff --name-only origin/main...d45dc45f` →
  `control/lib/symmetry_control_web/protocol.ex`,
  `control/test/symmetry_control_web/protocol_reason_coverage_test.exs` and two record files; the
  whole `daemon/` tree is untouched by the PR.
- Reproduction attempts recorded in the contract: the same test passed on the PR's previous and
  next heads. Added by this ledger (measured 2026-11-22, see "Evidence" below).
- Cause: unknown. Most probable mechanism, labelled inference: the input is a closable `io.Pipe`
  whose `Close` does unblock the parked `readLine`, so the reader exits through the `done` arm of
  its send `select`; the only arm that can return the run before the reader reaches its deferred
  `close(readerDone)` is the bounded join `runAutonomous` defers (`readerJoinTimeout = 50 ms`,
  introduced for the inherited-stdio carrier and pinned by
  `TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier`), and the failing message records no
  join-arm outcome, so the expiry is consistent with the observation but unproven.
- Next step: repeat `go test -race -count=N -run '^TestAutonomousReturnReleasesItsInputReader$'
  ./cmd/symmetry-fake-agent/` under CPU contention; on a reproduction capture which join arm won
  before touching the 50 ms bound or the assertion, because the bound is pinned by the sibling.
- Proximity, not duplication: the comment-policy-sweep section's entry for
  `daemon/cmd/symmetry-fake-agent/autonomous.go` records the *unblockable* carrier (its reader is
  never joined); this entry records one race-step failure of the closable-carrier test. Different
  item, cross-referenced in the entry so a later sweep does not merge or delete either.

### 2. `daemon/internal/platform` — `TestLinuxOwnerAnchorWire*` (five tests)

- Tests, in one file: `TestLinuxOwnerAnchorWireShapeRejectsWrongDescriptorCounts`,
  `…CloneCleanupDescriptorGate`, `…StoppingAnswerPrecedesShape`,
  `…ExclusiveFieldsRefusedBeforeStoppingAnswer`, `…CodecRejectsMalformedFrames`
  (`daemon/internal/platform/anchor_wire_shape_test.go`, `//go:build linux && amd64`). The contract's
  wildcard names exactly these five: `grep -rn "func TestLinuxOwnerAnchorWire" --include=*_test.go .`
  → 5, all in that file.
- Observed failure text: `recvmsg: connection reset by peer` (contract evidence bullet 2). The
  harness drives the real `serveConnection` over a real `unixpacket` socket
  (`linuxOwnerAnchorWirePair` → `linuxOwnerAnchorWireReadFrame` → `ReadMsgUnix`;
  `anchor_wire.go`'s `ReadMsgUnix` at the same socket), and the fragment is what Go renders for an
  `ECONNRESET` inside a `*net.OpError`'s syscall-op field — labelled inference: the full failing
  line and the failing assertion are not in hand, only the fragment.
- Where observed: hosted `coverage-daemon` job (advisory, `continue-on-error: true`;
  `python3 ../scripts/ci/stdio-only-caller-boundary.py "$(command -v go)" test -count=1
  -timeout 1200s -coverprofile=coverage.out ./...`), on PR #223 head `ca173870`.
- The head is records-only (`docs/evidence/comment-policy-sweep-2026-11-22.md` and its plan:
  `git show --stat ca173870` → those two files, 5 changed lines), and the PR's code hunks are
  comment-only, so no `daemon/` behavior changed in that revision range.
- Reproduction attempts recorded in the contract: main's last four `coverage-daemon` runs passed;
  five local WSL `-cover` runs at head and base passed. Added by this ledger (measured 2026-11-22):
  the group ran as an unprivileged WSL user with `-cover` at `-count=5` — 25/25 `--- PASS`, exit 0;
  the default WSL session here is root, where the same command refuses
  (`wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`), which is itself
  the host limit any revalidation has to respect and is recorded in the entry.
- Cause: unknown. This is a fresh test group, not the closed `daemon/internal/platform` flake class
  (helper witness / descendant monitor, `docs/archive/CLOSED-TODO.md`): the entry names that
  difference so a later reader does not treat one as the other.
- Next step: re-run the group above `-count=5` under CPU contention in the job's own shape
  (`-coverprofile` through `stdio-only-caller-boundary.py`), and on a reproduction capture the full
  failing line plus whether the peer side had already closed; the file's own host trap (a test
  process whose cgroup is `/` refuses clone cleanup) is a distinct recorded condition.

### 3. `frontend/src/features/goals/goal-attention.test.tsx` — the plan-approval replay case

- Test file: `frontend/src/features/goals/goal-attention.test.tsx` (vitest). The observation names the
  case as `replays the plan approval's …`, which is the shared prefix of exactly two cases in the
  file: `replays the plan approval's decision resolution byte for byte after a lost acknowledgement`
  and `replays the plan approval's admission byte for byte when its acknowledgement is lost`. The
  observation does not disambiguate; the entry keeps both names and says so, rather than picking one
  (a guess would be an invented reproduction result under contract clause 3).
- Where observed: PR #220 review, one failure among 13 shuffled vitest runs of a 13-file subset
  (`--sequence.shuffle --sequence.seed=3`); reruns with the same seed passed, so load-dependent, not
  order-dependent. The subset's file list is not recoverable from the repository (the PR #220 plan
  records shuffle runs at seed `20261122` over the full 21-file suite, not this subset), so the entry
  records the command shape the contract states and no invented file list.
- Both candidate cases end in bounded waits — `findByText("The server admitted the plan.", {},
  { timeout: 5_000 })` under a 15 s test timeout — so a starved bounded wait is a plausible
  mechanism; labelled inference, unproven, and the failure text is not in hand.
- Next step: repeat the shuffled 13-file subset with the same seed under CPU load and capture the
  assertion; the recorded rerun that passed is not described as loaded, so load remains the
  untested variable.
- No additional local attempt is made here: this worktree has no frontend dependency tree
  (`frontend/node_modules` absent), and installing one for a load-dependent case is neither the
  recorded reproduction nor cheap.

## Duplicate and placement check

- `git grep -n -e TestAutonomousReturnReleasesItsInputReader -e TestLinuxOwnerAnchorWire -e
  'goal-attention.test.tsx' -- docs/TODO.md docs/archive/CLOSED-TODO.md` at the baseline → two hits,
  both `docs/archive/CLOSED-TODO.md`, both inside items that name the frontend file as the pin of
  the frozen-attempt release rule (closed 2026-11-20/21) — not flakes, and not in the live
  `docs/TODO.md`, which holds no entry for any of the three tests. So the contract's "extend it
  instead of duplicating" branch does not apply; three new entries are correct.
- Adjacent look-alikes checked and cross-referenced in the entries: the two entries named in each
  flake's "proximity, not duplication" line above, plus the `admission_deadline` and
  `provider_bridge` flake sections (different packages, tests and mechanisms).
- Placement: appended as a new section at the end of `docs/TODO.md`, after the comment-policy-sweep
  section, matching how the three existing flake sections were appended. The file's Rules block is
  untouched: the items are undone, so they stay in `docs/TODO.md`.

## Exact edge-case semantics this contract binds

1. Three entries, one per flake, each carrying all seven demanded fields (name, package/file,
   observed text, where observed, reproduction attempts and outcomes, cause unknown, next step).
2. No line numbers in the entries (contract: "cite symbols and test names, not line numbers"); older
   flake entries do carry them, so this is a deliberate contract-driven deviation, not an oversight.
3. No invented reproduction result: the three contract bullets are reproduced as stated; the two
   added local runs are recorded with their exact commands, counts and exit codes; the entry that
   could be read as claiming more (entry 3's case name, entry 2's full failure line) states the
   ambiguity or the missing line explicitly.
4. `Cause unknown` is stated per entry; every mechanism is labelled inference and no entry asserts a
   proven cause.
5. No code or test change, no fix, no instrumentation (contract clauses 3-4): the failing surfaces
   keep their bytes, and the entries name the diagnostic step instead of performing it.
6. The increment stays mergeable alone (contract clause 6): docs only, no new dependency on any other
   in-flight slice, no file another slice owns.

## Evidence plan (contract Gates, foreground)

| # | Command | Expectation |
| --- | --- | --- |
| 1 | `git diff --check` | exit 0, no output |
| 2 | the contract's committed-range gate: `git diff origin/main...HEAD` searched case-insensitively for the profile-directory segment the program rules name | empty (the committed range is empty; the increment is uncommitted, so the same search is run over `git diff` plus the untracked plan as the effective check) |
| 3 | host-path scan over the whole increment (`git diff` + the untracked plan): the profile-directory segment, written `<profile-segment>/` in the convention `docs/plans/2026-11-22-rf-w3-01-classifier-extract-plan.md` uses, and any absolute user-profile directory path in any platform's spelling | empty |
| 4 | the contract's escape gate: no backslash escape immediately preceding the letter `u`, over the committed range and (effectively) the whole increment | empty |
| 5 | `python scripts/ci/check-record-cleanliness.py --base $(git merge-base HEAD origin/main) --head HEAD` | exit 0 (the range changes no file under `docs/evaluations/**` or `docs/evidence/**`) |
| 6 | `git diff --name-only` and `git status --short` | `docs/TODO.md` modified, the plan untracked, nothing else |
| 7 | census re-run from "Duplicate and placement check": every live `docs/TODO.md` hit belongs to the three new entries, `docs/archive/CLOSED-TODO.md` keeps its two, and no test name appears in two entries | counts below, in the measured table |

No CI-hosted oracle exists for this increment (docs-only, and the stage must not push); the local
test runs below are the only behavioral evidence and they are evidence about *reproduction*, not
about a fix.

## Evidence (measured 2026-11-22, on the uncommitted tree)

### Gates (contract clause 5, all foreground from the repository root)

| # | Command (as run) | Result |
| --- | --- | --- |
| 1 | `git diff --check` | exit 0, no output |
| 2 | the contract's committed-range gate over `git diff origin/main...HEAD` | no match (the search exits 1 — the committed range is empty) |
| 3 | the same search over `git diff` plus this untracked plan | no match (the search exits 1) |
| 4 | the contract's escape gate over the same bytes | no match (the search exits 1); the `docs/TODO.md` diff contains no backslash character at all |
| 5 | `python scripts/ci/check-record-cleanliness.py --base $(git merge-base HEAD origin/main) --head HEAD` | exit 0 — `check-record-cleanliness: no record added or modified between d5b14b8579b7 and d5b14b8579b7; nothing to check` |
| 6 | `git diff --name-only`, `git status --short` | `docs/TODO.md` modified; `docs/plans/2026-11-22-flake-ledger-2-plan.md` untracked; nothing else |
| 7 | `git grep -c -e TestAutonomousReturnReleasesItsInputReader -e TestLinuxOwnerAnchorWire -e goal-attention.test.tsx -- docs/TODO.md docs/archive/CLOSED-TODO.md` | `docs/TODO.md:9`, `docs/archive/CLOSED-TODO.md:2` — the 9 are the three entries' own mentions (entry 1 twice: lead and the `-run` pattern; entry 2 six times: the wildcard plus its five names; entry 3 once), and the archive's 2 are pre-existing |

Python 3.12.10, as gate 5 ran it.

### Reproduction attempts recorded into the entries

| Surface | Command | Result |
| --- | --- | --- |
| fake-agent race, CI shape | WSL (Linux amd64, go1.27.0), from `daemon/`: `go test -race -count=20 -timeout 600s -v -run '^TestAutonomousReturnReleasesItsInputReader$' ./cmd/symmetry-fake-agent/` | 20/20 `--- PASS`, 0 `--- FAIL`, exit 0 |
| anchor-wire group, job shape | WSL Ubuntu-24.04 as an unprivileged user, go1.27.0, from `daemon/`: `go test -count=5 -timeout 1200s -cover -v -run '^TestLinuxOwnerAnchorWire' ./internal/platform/` | 25/25 `--- PASS`, 0 `--- FAIL`, exit 0, `coverage: 2.1% of statements` |
| anchor-wire group, root session | same command in the default (root) WSL session | 5 `--- FAIL` with `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0` — recorded as the host limit, not as a test failure |
| local `-race` on the Windows host | `go test -race …` (native) | not run: `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` and this host has no C toolchain; the WSL runs above stand in, and the limit is recorded |

## Design trade-offs and decisions

- One section with three entries, not three sections: the contract allows either, one section keeps
  the three 2026-11-22 observations together and leaves the file's thematic sections untouched.
- Entries are appended, not merged into the existing flake sections: each names a different package,
  test and mechanism, and merging would obscure which observation belongs to which PR/run.
- Cross-references to the nearest look-alikes are written into the entries (not only into this plan),
  because a future dedup sweep reads the entry first.
- No line-number coordinates, unlike older entries: the contract fixes that, and symbols survive the
  refactors this program performs.
- The plan records the two contract ambiguities (entry 3's case name; the "13 shuffled runs / 13-file
  subset" pairing) instead of resolving them silently; the entry keeps the contract's own words.

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

- The hosted failure texts are quoted from the contract's evidence, not re-read from the CI logs on
  this host (no attempt is made to fetch them); if a log line differs, the entry's quotation is the
  contract's, and the plan records that provenance.
- Entry 3 names two candidate cases; a later reader who recovers the review log should narrow it to
  one and keep the other's text history only if it also flaked.
- The local passes above are single-session and unloaded for the anchor-wire group; they bound the
  reproduction, they do not disprove the flake.
- The root-session refusal is a WSL-default-user property on this host, not a repository condition;
  the entry records it beside the command so a revalidation does not read it as a test failure.
