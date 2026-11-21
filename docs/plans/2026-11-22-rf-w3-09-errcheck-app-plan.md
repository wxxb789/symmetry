# rf-w3-09-errcheck-app plan — errcheck triage part 1: app recovery paths (DA S7a)

Contract: `goal-rf-w3-09-errcheck-app.md` (GDD, 2026-11-21, including the conductor decision of
2026-11-21 and the comment policy of 2026-11-22). Base revision: `261e84a42313cf1580a06df11b6062305cdf53d5`
(`refactor(state): wrap state errors with their causes (#200)`, i.e. W3-07 merged, #197 W4-19 merged,
#199/#198/#211/#195/#191 merged). Dispatch gates satisfied: W3-07 merged (hard prerequisite for the
writer-seam `errors.Is` chain), W2-10 (#159) merged.

## Re-derivation at the base revision

The contract's coordinates were re-verified against `261e84a4` (they were drafted at `c87c9918` and
reconciled at `a1142d4e`):

- the 9 dropped `daemon.abandonGoalSession` calls are unchanged at `goal_admission.go:1088, 1099,
  1103, 1113, 1117, 1122, 1129, 1143, 1156`, each an expression statement immediately followed by the
  return the contract names;
- `abandonGoalSession/4` is defined at `:1370` and already joins its internal errors
  (`errors.Join(closeErr, retentionErr, uncertainErr)`) — untouched;
- `requireNativeSessionCloseRetry` is defined at `:1273`; its call sites at this head are `:1080`
  (the `markGoalSessionAttachDeliveryReady` error arm), `:1422`, `:1439` (inside
  `abandonGoalSession`), `:1478`, `:1494` (inside `abandonGoalSessionUncertain`). The contract's
  reconciled set (`:1080/:1406/:1423/:1462/:1478` at `a1142d4e`) is the same five call sites, four of
  them shifted by the intervening app commits; `:926/:1032/:1215` remain checked-abandon call sites,
  not arming sites;
- W4-19 (#197) moved the preflight Go files to `daemon/internal/linuxpreflight`, so the contract's
  arch-decision §G applies: the raw F11 ledger is retargeted to the new package path under the same
  errcheck rules; acceptance still binds only `goal_admission.go` (9 sites) + `terminate_linux.go`
  (1 site).

## Deliverable shape

| file | status | change |
|---|---|---|
| `daemon/internal/app/goal_admission.go` | edited | The 9 abandon calls are error-checked and folded into their returns with `errors.Join`: 8 capture into `abandonErr`; at `:1103` the shared literal is bound once as `cause` and used as both the call's cause and the joined primary. Nothing else in the file moves. |
| `daemon/internal/app/terminate_linux.go` | edited | `defer process.Release()` (pre-change `:50`) → stated-reason comment at `:50-51` + `defer func() { _ = process.Release() }()` at `:52` |
| `daemon/internal/app/abandon_error_join_test.go` | new | one test `TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn` driving site `:1088` with a fault inside the abandon path |
| `docs/plans/2026-11-22-rf-w3-09-errcheck-app-plan.md` | new | this plan |
| `docs/evidence/rf-w3-09-errcheck-app-2026-11-22.md` | new | evidence record (commands, raw output, mutants) |

No exported symbol is added, removed, renamed or moved. No new error type, no new sentinel, no new
suppression directive (`//nolint` count stays 0 for the touched files).

## The 9 sites and their exact post-change text

Every pre-change return keeps its message prefix and its wrapped primary cause; the change appends
only the join term. "prefix" below means the byte-identical leading text of `err.Error()`.

| line | primary return (unchanged bytes) | post-change return |
|---|---|---|
| 1088 | `fmt.Errorf("load native run fence: %w", err)` | `fmt.Errorf("load native run fence: %w", errors.Join(err, abandonErr))` |
| 1099 | `fmt.Errorf("attach native Goal session: %w", err)` | `fmt.Errorf("attach native Goal session: %w", errors.Join(err, abandonErr))` |
| 1103 | `errors.New("native Goal session attach receipt is unavailable")` | `errors.Join(cause, daemon.abandonGoalSession(sessionKey, session, attached, cause))` |
| 1113 | `fmt.Errorf("fetch Goal run context: %w", err)` | `fmt.Errorf("fetch Goal run context: %w", errors.Join(err, abandonErr))` |
| 1117 | `err` | `errors.Join(err, abandonErr)` |
| 1122 | `fmt.Errorf("encode canonical Goal context: %w", err)` | `fmt.Errorf("encode canonical Goal context: %w", errors.Join(err, abandonErr))` |
| 1129 | `fmt.Errorf("mark native Goal run running: %w", err)` | `fmt.Errorf("mark native Goal run running: %w", errors.Join(err, abandonErr))` |
| 1143 | `errors.New("Goal run ended before native turn start")` | `errors.Join(errors.New("Goal run ended before native turn start"), abandonErr)` |
| 1156 | `ctx.Err()` | `errors.Join(ctx.Err(), abandonErr)` |

Apply revision (2026-11-22, simplify finding 1): eight statements become `abandonErr :=
daemon.abandonGoalSession(sessionKey, session, attached, <same cause as today>)`; at `:1103` the
shared literal is bound once as `cause` and the call is folded into the return —
`errors.Join(cause, daemon.abandonGoalSession(sessionKey, session, attached, cause))` — so the
abandon cause and the returned primary are the same value and cannot drift. The other eight sites
keep their cause arguments byte-identical (`err`, `ctx.Err()`, the two literals), and the `:1143`
pair still differs intentionally: the abandon cause (`"Goal run was cancelled before native turn
start"`) and the returned literal (`"Goal run ended before native turn start"`).

Exact semantics of the joins, verified against `errors.Join`:

- two non-nil operands → `Unwrap() []error` holding both; the two `errors.Is`/`errors.As` consumers
  of this path (`taskResultFailureReason`'s `errors.As` and the terminal transition's `reason`
  derivation) see the same primary cause as before;
- one nil operand → the return is still a `*joinError` wrapping the single non-nil operand
  (`errors.Join` has no single-operand special case; go1.27.4 `errors/join.go`): `Error()` text and
  `errors.Is`/`errors.As` are preserved, but the operand's identity and dynamic type are not, and
  `errors.Unwrap` finds no single cause (no daemon production caller unwraps these returns), so a
  successful abandon adds nothing observable to the return;
- text: `err.Error()` of a joined pair is `primary.Error() + "\n" + abandon.Error()`; the primary's
  first line is therefore unchanged. This is the declared durable change (C-D1/P2-1): the joined
  tail now reaches `payload["error"]` (`app/events.go`), pinned exactly by the new test.

## Invariants that must not move

1. First line of every one of the 9 returned errors is byte-identical to the pre-change `err.Error()`.
   Check (the `-E` is required — as a BRE the `|` is literal and the pattern matches nothing):
   `git grep -n -E '"(load native run fence|attach native Goal session|native Goal session attach
   receipt|fetch Goal run context|encode canonical Goal context|mark native Goal run running|Goal run
   ended before native turn start)' <rev> -- daemon/internal/app/goal_admission.go` resolves to the
   same seven message strings before and after (eight matching lines at `261e84a4`, where the
   `:1103` literal is both the abandon argument and the return; seven at the delivered revision,
   where it is bound once as `cause`) — and the full diff.
2. `requireNativeSessionCloseRetry` call set unchanged: `:1080`, `:1422`, `:1439`, `:1478`, `:1494`
   (`git grep -n requireNativeSessionCloseRetry <rev> -- daemon/internal/app/goal_admission.go`
   before/after shows only the definition plus those five; the diff adds none — the unrestricted
   grep also matches `harness_test.go:6126`, `native_run.go:854` and docs text, an identical set
   before and after). The 9 sites neither gain nor lose arming: arming happens inside
   `abandonGoalSession`/`abandonGoalSessionUncertain`, whose bodies are untouched.
3. No hunk inside `abandonGoalSession` (`:1370-1444`) or `abandonGoalSessionUncertain`;
   `git diff -U0 -- daemon/internal/app/goal_admission.go` must show exactly the 9 two-line hunks.
4. Durable-write order and the internal join chain inside `abandonGoalSession` are untouched.
5. `terminate_linux.go:52` (pre-change `:50`) keeps the identity/stop-proof failures as returned
   errors; only the un-waited handle release becomes explicit-unactionable. `_ =` is the repo's
   existing convention (49 prod sites) and is not reported under the gate config's default
   `check-blank: false`, so no suppression directive is introduced.

## Test design (new file `abandon_error_join_test.go`)

One test, `TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn` (stable handle for `-run`), drives
site `:1088` (`load native run fence`) through `startGoalAdmission` with two seams composed in one
`store.SetAtomicWriterForTesting` writer:

1. **Ready-barrier write then corruption.** When the writer sees the run-journal write for the
   attach delivery marked ready (`"kind":"session_attach"` + `"ready":true`, i.e. the
   `markGoalSessionAttachDeliveryReady` write at `:1073`), it performs the real write and then
   replaces the journal bytes with `{`. The very next fence read is `store.LoadJournal` at `:1086`,
   so the corrupt journal deterministically selects the owned site; earlier reads/writes of the same
   file (claim, process marker) already happened.
2. **Abandon-internal sentinel.** When the writer sees the goal-session journal write carrying
   `"launch_state":"uncertain"` (the `MarkGoalSessionUncertain` write inside `abandonGoalSession`),
   it returns the injected sentinel. `writeJSONWithLimit` wraps it as
   `write goal session journal: %w` (W3-07), so `errors.Is` reaches it only if the site joins the
   abandon result.

Assertions (all exact, none is a bare `err != nil`):

- the admission deadline is overridden to `time.Now().UTC().Add(time.Hour)` before the fixture helper
  runs, so the test does not depend on the shared fixture's hardcoded `2026-12-31` deadline;
- `errors.Is(returnedErr, sentinel)` — the join (fails red at the pre-change tree, which drops the
  abandon result);
- `returnedErr.Error()` equals the composed exact text: first line `load native run fence: ` plus the
  re-read primary error, then the abandon join lines (`<primary>` from `recordStoppedGoalSession`,
  `persist workspace retention: <primary>`, `write goal session journal: <sentinel>`); the primary
  text is obtained by re-reading the still-corrupt journal after the call, so the assertion pins the
  prefix and the joined tail against observed values; it fails if any prefix or term moves;
- the returned error's first join frame carries exactly two terms and its first term still reaches
  the run-journal decode chain (`errors.As` to `*json.SyntaxError`, the type the re-read journal
  error is checked to have), so a joined-but-flattened primary fails (fix revision);
- the journal no longer decodes (`store.LoadJournal` after the call returns an error);
- the native session was closed and no turn started (`close` in `session.callsSnapshot()`,
  `start_turn` absent).

Not driven: the other 8 sites. The contract asks for at least one; the sites share one mechanical
shape (capture + join) and one op, and each of the 8 is guarded by errcheck 0 plus the prefix diff.
Driving every site would require eight unrelated fault injections (marshal, markRunning, context
fetch, receipt mismatch, …) whose extra coverage is the same three lines of joining logic.

## Errcheck accounting (measurement, not gate invention)

Two pinned commands per GOOS (raw, gate-config), from `daemon/`:

```
raw:  GOOS=<goos> golangci-lint run --no-config --default=none --enable=errcheck \
        --max-issues-per-linter=0 --max-same-issues=0 ./internal/app/... ./internal/state/...
gate: GOOS=<goos> golangci-lint run -c errcheck-gate.yml --default=none --enable=errcheck \
        --max-issues-per-linter=0 --max-same-issues=0 ./internal/app/... ./internal/state/...
```

with `errcheck-gate.yml` carrying exactly the contract's §10.2 exclusions
(`presets: [comments, common-false-positives, legacy, std-error-handling]`, `rules: [{path:
_test\.go, linters: [errcheck]}]`, `check-blank` left at its default `false`), and additionally the
retargeted `./internal/linuxpreflight/...` raw run (arch-decision §G). Acceptance: the gate-config
count for `goal_admission.go` + `terminate_linux.go` is 0 on linux and windows; the raw counts are
reported as the wider F11 ledger (they include the Close family outside this slice's owned set).

## Gates (all foreground)

| gate | command |
|---|---|
| format | `gofmt -l internal/app/goal_admission.go internal/app/terminate_linux.go internal/app/abandon_error_join_test.go` (0 files) |
| vet | `go vet ./...` (daemon) |
| builds | `GOOS=linux go build ./...`, `GOOS=windows go build ./...` |
| focused test | `go test -count=1 ./internal/app/ -run 'TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn\|TestFreshCodexPreNativeLaunchWriteFailuresReadBackAndCompensate'` |
| package suite | `go test -count=1 ./internal/app/...` (Windows) |
| linux lane | `wsl -d Ubuntu-24.04 -u accept -- bash -lc 'cd /mnt/q/repos/wt-w3-09-errcheck-app/symmetry/daemon && go test -count=1 ./internal/app/...'` (worktree path; the contract's `/mnt/q/repos/symmetry` is the main clone) |
| errcheck | the four pinned runs above, filtered to the owned files |
| whitespace | `git diff --check`, plus a host-path scan of the diff: `git diff origin/main...HEAD -- . ':(exclude)docs' \| grep -ci 'users/'` → 0 (the unrestricted scan counts 3 at `519b6bb9`, all its own quoted text in this plan and the evidence record; `grep -nE '[:][/\\]Users[/\\]\|[/]home[/]'` → 0 hits) |
| mutation | revert site `:1088` to the dropped call, show the new test red, restore |

Execution note (2026-11-22): the WSL lane could not run the literal command — this VM's shared
network namespace has IPv4 loopback broken (pre-existing: reproduced at the base revision
`261e84a4` and in the `symmetry-race` distro, `networkingMode=mirrored`), which hangs the
`httptest` dials the package suite depends on. The full `./internal/app/...` suite was therefore run
inside a fresh network namespace (`sudo unshare -n`, `lo` up, user `accept`, go1.27.0 linux/amd64):
rc 0, `ok … 132.853s`, plus a targeted run of the owned tests (`ok … 0.419s`). Details:
`docs/evidence/rf-w3-09-errcheck-app-2026-11-22.md` §6.1.

Sequencing: errcheck runs and the full app suite never overlap (host memory rule).

## Out of scope / decisions

- The Close-family raw findings (`internal/linuxpreflight`, `linux_owner_recovery_linux.go`,
  `linux_owner_witness_linux.go`, `internal/state/*`) stay: the gate preset excludes them, none
  carries recovery meaning for this slice, and W3-10/W3-11 own their triage rows.
- No wrap/reshape inside `internal/state` (W3-07 already delivered that), no `abandonGoalSession`
  body change, no `startGoalAdmission` split (W4-01 depends on this slice landing first), no `app.go`
  callback region (W2-10).
- `terminate_linux.go` keeps `defer` semantics (release at function exit); no retry/join is
  introduced for a released handle, matching the contract's stated reason.
- Decision recorded rather than asked: the contract's "gate config" is materialized as a standalone
  minimal config (`errcheck-gate.yml`) rather than the repo's `.golangci.yml`, because only the
  contract's §10.2 exclusions are reproduced (the repo file also relaxes test rules for other
  linters, which is a no-op under `--default=none --enable=errcheck`). Both runs are recorded.
