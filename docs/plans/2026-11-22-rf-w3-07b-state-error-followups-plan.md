# rf-w3-07b-state-error-followups plan — the W3-07-domain hunks split out of PR #214

Contract: `goal-rf-w3-07b-state-error-followups.md` (GDD, conductor decision 2026-11-22) — the
follow-up split out of `refactor/rf-w3-08-unexport-state-surface` (PR #214) with its own STRICT
owned set (the W3-07-domain hunks, `state_unix.go`/`state_windows.go` for the STD-04 remedy, the S2
deletion in `goal_session.go`, the tests and the records) and the merge order #214 first, keeping
the S2 deletion on catch-up. W3-08's contract owns the rename/move slice only, so the W3-07-domain
hunks the rf-w3-08 apply stage had left in that branch move here under the branch
`refactor/rf-w3-07b-state-error-followups`. Base: `origin/main` = `0132bd2b` (#203); the W3-07 work
itself is #200 (`261e84a4`).

## Why the split

The independent review of #214 (`P1-1`) found that branch shipping W3-07 follow-up behaviour it
does not own and cannot justify under its STRICT owned set: `provider_action.go` (S1 + S3), the
`Close`/`saveJournalLocked`/alias-decode hunks of `state.go` (STD-04, S3, S4), the
`EnsureGoalSessionLineageAvailable` deletion (S2), the S7 app test and the W3-07 evidence/plan
edits. The W3-08 evidence record itself calls these "the code this record does not own" (§8). The
conductor's split therefore keeps #214 to the rename/move plus the `linux_owner_preflight`
adaptation and the W3-08 records, and lands this set — the rf-w3-08 apply stage's accepted
simplifications of W3-07's own code — as this follow-up.

## What this branch carries

| finding | file(s) | change |
|---|---|---|
| S1 | `daemon/internal/state/provider_action.go` | collapse the two byte-identical trailing-data static arms to one; discard the token value |
| S2 | `daemon/internal/state/goal_session.go` | delete the callerless `Store.EnsureGoalSessionLineageAvailable` (W1-06 dead-code candidate, −18 lines) |
| S3 | `daemon/internal/state/provider_action.go`, `state.go` | extract `serializeRunJournalWithinLimit`; use it at the three former inline serialize/wrap/limit blocks |
| S4 | `daemon/internal/state/state.go` | propagate the alias-check cause (`fmt.Errorf("decode %s: %w", resource, err)`) instead of the static `errors.New("decode " + resource)` |
| S7 | `daemon/internal/app/goal_delivery_test.go` | `TestAbandonGoalSessionUncertainBoundsOversizedCloseReason` (2 rows) pinning the W307-R1 bound at the second composed-reason site (`goal_admission.go` `abandonGoalSessionUncertain`) |
| — | `daemon/internal/state/state_unix.go`, `state_windows.go` | STD-04 remedy: `releaseStoreLock` joins its unlock/close causes under the existing prefix; `Store.Close` passes the error through |
| records | `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, `docs/evidence/rf-w3-07-…md` §12, `docs/plans/2026-11-21-rf-w3-07-…-plan.md` | the moved apply-stage record, the S1/S2 closures and the citation refresh |

## STD-04 — the release-lock cause (decision)

#200 wrapped `Close`'s lock release (`fmt.Errorf("release state directory lock: %w", err)`) while
`releaseStoreLock` returned the same literal, so every release failure composed the duplicated
message `release state directory lock: release state directory lock` and dropped the underlying
`unlockErr`/`closeErr`. The reviewed hunk removed the wrap instead, which fixes the duplication by
losing the cause; the review's preferred remedy — applied here — keeps the wrap at the source:
`releaseStoreLock` returns `fmt.Errorf("release state directory lock: %w", errors.Join(unlockErr,
closeErr))` (`state_unix.go`; `state_windows.go` joins the Windows call error through the
file-local `windowsCallError` helper) and `Store.Close` returns `releaseStoreLock(lock)` unchanged.
Delivered message: `release state directory lock: The handle is invalid.` + the close cause.

## Tests and mutation evidence

- `state_wrap_test.go` (both appended after the last existing test — no earlier line moves):
  - `TestLoadJournalWrapsCaseInsensitiveAliasCause` (S4) — pins that a case-insensitive tag alias
    load keeps the `decode run journal: ` prefix and reports the aliased member/tag. Red under the
    reverted hunk: `LoadJournal() error = "decode run journal"`.
  - `TestStoreClosePreservesReleaseLockCauses` (STD-04) — closes the store's lock file first, then
    asserts the release prefix plus cause, exactly one prefix occurrence and
    `errors.Is(err, os.ErrClosed)`. Red on the pre-split forms: both hunks reverted →
    `"release state directory lock: release state directory lock"` (2 occurrences); `Close` reverted
    only → the same duplication with the causes present; platform helpers reverted only →
    `"release state directory lock"` with no cause.
- `state_windows_test.go` `TestWindowsReleaseStoreLockJoinsUnlockCause` (STD-04, review P3-2):
  releases an unlocked temp file's handle, so `UnlockFileEx` fails with `ERROR_NOT_LOCKED` while
  the close succeeds; asserts `errors.Is(err, syscall.Errno(158))` and the single prefix. Red when
  the Windows join drops `unlockErr`. Unix has no cheap equivalent without root: `LOCK_UN` on a
  valid fd succeeds whether or not a lock is held, and the only reachable `flock` failure (a
  closed/invalid fd) fails the close too, which the existing both-fail case already covers — this
  gap is recorded rather than papered over.
- `provider_action_test.go`'s semantic-normalization test gains the `{"a":1} {}` multiple-value
  rejection (review P3-3). Recorded limit: the rejection is produced by the leading validity gate —
  `json.Valid` is false and `RejectDuplicateJSONMembers` reports "JSON contains more than one value"
  — so the S1 static return behind it is unreachable for this input: deleting that return alone
  keeps the case rejected (green), while weakening the gate to `len(result) == 0` and deleting the
  return accepts the input with its trailing data ignored and reds the new row (`error = <nil>`).
  The review's suggested expectation (`provider action result has trailing data`) is not a message
  this input can produce; the row pins the rejection and its actual message.
- S7's mutation pair (re-recorded at this branch's head): green normally; red with
  `goal_admission.go`'s `boundedGoalSessionUncertainReason(reason)` → `reason`, both rows failing at
  `goal_delivery_test.go:806`.
- Comment policy (review P3-4): the `serializeRunJournalWithinLimit` docstring, the
  `goal_delivery_test.go`/`state_wrap_test.go` test docstrings and the rewritten budget-test comment
  are deleted; no linter requires them.
- The synthesized sweep tool (W3-07 evidence Appendix A) reproduces the apply-tip counts at this
  branch's head: `rows 28 (static-new 28, static-fmt-no-w 0, sentinel-return 0)`; `-all`: row 28 /
  pred 196 / other-branch 39 / no-branch 265, raw total 528; `-selfcheck` exit 0.

## Gates (foreground, cwd `daemon/`)

`gofmt -l .` (empty) · `go vet ./...` · `GOOS=linux go vet ./...` · `GOOS=darwin GOARCH=arm64 go vet
./internal/app/` · `go build ./...` · `go test -count=1 ./internal/state/... ./internal/app/...`.
All foreground on this worktree (Windows/amd64, go1.27.4), 2026-11-22; the commit carrying this
record adds no code change over the gated worktree.

| # | command | exit | observed |
|---|---:|---:|---|
| 1 | `gofmt -l .` | 0 | 0 lines |
| 2 | `go vet ./...` | 0 | no output |
| 3 | `GOOS=linux go vet ./...` | 0 | no output |
| 4 | `GOOS=darwin GOARCH=arm64 go vet ./internal/app/` | 0 | no output |
| 5 | `go build ./...` | 0 | no output |
| 6 | `go test -count=1 ./internal/state/... ./internal/app/...` | 0 | `ok …/internal/state 5.282s`; `ok …/internal/app 128.849s` |

`git diff --check` reports no whitespace errors and the host-path scan finds no absolute profile
paths in the branch diff.

## Record moves and citation base

The W3-07 evidence §12 and the plan's apply-stage paragraph moved here verbatim as the apply-stage
record; they name this branch's landing in a move note. Cells marked "apply tip" in that record
resolve at the pre-split apply tip — on this branch `state.go` cells at or below `Store.Close`
resolve 3 lines lower and `provider_action.go` cells at or below `serializeRunJournalWithinLimit` 4
lines lower, while the `goal_session.go` cells resolve as written; §12.3's assertion resolves at
`goal_delivery_test.go:806`.

## Out of scope

W3-08's type unexports and constructor move (#214); the rest of W3-07's wraps (#200); any further
error-cause site; new error types or verdict changes beyond the S3 serialize/size split #200
already pins.
