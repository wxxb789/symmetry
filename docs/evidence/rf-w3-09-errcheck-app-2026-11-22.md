# rf-w3-09-errcheck-app errcheck triage part 1: app recovery paths — evidence record (2026-11-22)

Subject: contract `goal-rf-w3-09-errcheck-app.md` (GDD, 2026-11-21 + conductor decision
2026-11-21 + comment policy 2026-11-22), base revision
`261e84a42313cf1580a06df11b6062305cdf53d5` (`refactor(state): wrap state errors with their causes
(#200)`; W3-07 merged, #197 W4-19 merged). Deliverable: the uncommitted working tree of
`<HOST-PATH>/wt-w3-09-errcheck-app/symmetry` described below.

Apply revision (2026-11-22, stage `apply`): simplify findings 1 and 2 applied, record corrections
D1, D2, D3 and one measured count fix applied in place, and the gates re-run — see §8. Fix revision
(2026-11-22, stage `fix`, review findings `W309-R1`/`R3`/`R4`): record statements restated against
the frozen revision `519b6bb9`, the new test made calendar-independent and its primary-cause pin
tightened, and the gates re-run — see §9. The sha256 table binds the fix revision; the
implement-revision run observations in §4–§6 stand as recorded where §8/§9 does not restate them.

Subject identity (sha256 of the delivered files at the fix revision; the two unchanged Go files carry
their apply-revision values):

| file | sha256 |
|---|---|
| `daemon/internal/app/goal_admission.go` | `e9e7494a507d9872d52316633f1411f4eafcfe345fe2d3ca6cb73c56f7a20ad9` (unchanged since apply) |
| `daemon/internal/app/terminate_linux.go` | `d3f853250cdd006e2f0c8b0bc4d648ee16deceb8de1157fc4b401f6f47c79947` (unchanged since apply) |
| `daemon/internal/app/abandon_error_join_test.go` | `9cdb84b942354e0fc5b4f5547fdfd92f369355c88e2822cd1e58ef243beb4624` (fix revision; apply revision `c809a2b1…`) |
| `docs/plans/2026-11-22-rf-w3-09-errcheck-app-plan.md` | `fda10bab272dd936828275fbbe879ed5e138398d1dcf0521d313839f77653b68` (fix revision; apply revision `3d2ed6ec…`) |
| `docs/evidence/rf-w3-09-errcheck-app-2026-11-22.md` | this record (hash not self-referential) |

## 1. Deliverable shape

| file | status | change |
|---|---|---|
| `daemon/internal/app/goal_admission.go` | edited | the 9 abandon calls are error-checked and folded into their returns via `errors.Join` (8 capture into `abandonErr`; `:1103` binds the shared literal once as `cause`). `git diff -U0` = exactly 9 hunks / 36 changed lines (apply revision). |
| `daemon/internal/app/terminate_linux.go` | edited | `defer process.Release()` (pre-change `:50`) → `defer func() { _ = process.Release() }()` at `:50` (no comment, per the 2026-11-22 comment policy; the released handle cannot change the stop-proof verdict) |
| `daemon/internal/app/abandon_error_join_test.go` | new (88 L) | `TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn` |
| `docs/plans/2026-11-22-rf-w3-09-errcheck-app-plan.md` | new | plan |
| `docs/evidence/rf-w3-09-errcheck-app-2026-11-22.md` | new | this record |

No exported symbol changed; no new `//nolint` (`grep -c nolint` = 0 in all three Go files).

## 2. The nine sites and the prefix identity

`git grep -n -E '^\s+daemon\.abandonGoalSession\(' <rev> -- daemon/internal/app/goal_admission.go`
matches exactly the nine dropped calls `:1088, :1099, :1103, :1113, :1117, :1122, :1129, :1143,
:1156` at the pre-change revision `261e84a4`; at the delivered revision `519b6bb9` (and on the
worktree) the same grep matches zero lines — eight sites capture into `abandonErr :=` and `:1103`
folds the call into `errors.Join(...)` inline.

| line | pre-change return | post-change return |
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

Prefix identity: `errors.Join(a, b)` with two non-nil operands renders `a.Error() + "\n" +
b.Error()`, so for every site the pre-change text is a byte-prefix of the post-change text (five
sites keep a `fmt.Errorf` wrapper and wrapped cause — `:1088`, `:1099`, `:1113`, `:1122`, `:1129`;
`:1117` keeps the primary `err`, `:1156` keeps `ctx.Err()`, `:1103` joins the shared literal bound
once as `cause`, and `:1143` joins its distinct literal — 5 + 4 = 9 site returns). With a nil
abandon result the return is still a `*joinError` wrapping the single non-nil operand — `Error()`
text and `errors.Is`/`errors.As` are preserved, identity and dynamic type are not (`errors.Join` has
no single-operand special case; go1.27.4 `errors/join.go`; simplify probe: identity=false, Is=true,
text-equal=true, `errors.Unwrap`=nil; no daemon production caller unwraps these returns) — so a
successful abandon adds nothing observable. The `:1143` pair of distinct literals is unchanged.
`git grep -n -E '"(load native run fence|attach native Goal session|native Goal session attach
receipt|fetch Goal run context|encode canonical Goal context|mark native Goal run running|Goal run
ended before native turn start)' <rev> -- daemon` (the `-E` is required: the same pattern as a
BRE, with `|` literal, matches nothing) matches nothing outside `goal_admission.go` on the
pre-change tree (`dc0aa35`, tests included), so no pre-existing test pinned the seven strings by
text; on the delivered tree it matches those seven in `goal_admission.go` (`:1089`, `:1100`,
`:1103`, `:1114`, `:1123`, `:1130`, `:1144` — seven matching lines, seven distinct strings), the new
test (`abandon_error_join_test.go:57`) and the plan/record tables that quote the sites; the same
file-scoped grep matches eight lines at `261e84a4` (the `:1103` literal appears both as the abandon
argument and in the return) and seven at the delivered revision, where it is bound once as `cause`.
The new test pins `:1088`'s rendered text exactly, and (fix revision) the join frame's first
operand's decode-error chain.

## 3. Invariants

- **Retry arming unchanged.** `git grep -n requireNativeSessionCloseRetry HEAD -- …/goal_admission.go`
  and the worktree grep are identical: definition `:1273` plus call sites `:1080`, `:1422`, `:1439`,
  `:1478`, `:1494` — the same five as the contract's reconciled set (`:1080/:1406/:1423/:1462/:1478`
  at `a1142d4e`, four shifted by the intervening app commits). The diff adds or removes none.
- **No edit inside `abandonGoalSession`.** `git diff -U0` shows 9 hunks, all at `:1087-1157`;
  `abandonGoalSession` (`:1370`) and `abandonGoalSessionUncertain` (`:1446`) bodies are untouched, so
  the durable-write order and the internal `errors.Join(closeErr, retentionErr, uncertainErr)` chain
  do not move.
- **Durable text is a declared change.** The joined tail reaches `payload["error"]` (D1/P2-1); the
  primary prefix and `reason` (`errors.As`) semantics are unchanged; `:1088`'s exact rendered text is
  pinned by the new test.

## 4. The new test and its mutant

`TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn` drives site `:1088` through
`startGoalAdmission` with two composed seams in one `store.SetAtomicWriterForTesting` writer:

1. run-journal write carrying `"kind":"session_attach"` + `"ready":true` (the
   `markGoalSessionAttachDeliveryReady` write at `:1073`) is committed and then the journal bytes are
   replaced with `{` → the next fence read is `store.LoadJournal` at `:1086`, the owned site;
2. goal-session write carrying `"launch_state":"uncertain"` (the `MarkGoalSessionUncertain` write
   inside `abandonGoalSession`) returns the injected sentinel, which W3-07's
   `writeJSONWithLimit` wraps as `write goal session journal: %w`.

Assertions: `errors.Is(returned, sentinel)`; the rendered text equals the composed exact string
(first line `load native run fence: ` + the re-read primary error, then `recordStoppedGoalSession`'s
primary, `persist workspace retention: ` + primary, and the sentinel write line); the journal no
longer decodes; the session was closed and no turn started. Fix revision: the admission deadline is
overridden to `time.Now().UTC().Add(time.Hour)` before the fixture helper runs, and the returned
error's first join frame must carry exactly two terms whose first still reaches the decode chain
(`errors.As` to `*json.SyntaxError`, with the same assertion first made on the re-read journal error
so the fixture stays honest).

- head, green: `go test -count=1 ./internal/app/ -run
  'TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn|TestFreshCodexPreNativeLaunchWriteFailuresReadBackAndCompensate'`
  → rc 0; `-v` shows `--- PASS: TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn (0.23s)` plus the
  4/4 subtests of the shape-reference test.
- mutant, red: site `:1088` reverted to the pre-change dropped call →
  `go test -count=1 ./internal/app/ -run '^TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn$' -v`
  exit 1, `--- FAIL: TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn (0.27s)`,
  `admission error = load native run fence: decode run journal: unexpected end of JSON input, want the
  abandon-internal sentinel joined` (`<run-dir>/gates/mutation-red.txt`). The join was restored:
  `sha256sum -c <run-dir>/gates/delivered-hashes.txt` → all three files OK, the focused gate re-ran
  green (rc 0, `<run-dir>/gates/focused-after-restore.txt`) and `gofmt -l` lists no file.

## 5. Errcheck measurements

Pinned commands, from `daemon/`, per GOOS; raw = `--no-config --default=none --enable=errcheck
--max-issues-per-linter=0 --max-same-issues=0 ./internal/app/... ./internal/state/...`; gate = the
same arguments with `--no-config` **replaced** by `-c <run-dir>/errcheck/errcheck-gate.yml` (the two
options cannot be combined: `golangci-lint` answers `can't combine option --config and --no-config`)
— the config is a minimal file carrying exactly the contract's §10.2 `linters.exclusions`:
`presets: [comments, common-false-positives, legacy, std-error-handling]` and
`rules: [{path: _test\.go, linters: [errcheck]}]`; `check-blank` default `false`. Raw outputs also
include `./internal/linuxpreflight/...` for the arch-decision §G retargeting. Before = a detached
pristine worktree at `261e84a4`; after = the delivered tree.
Raw totals include `_test.go` findings; the prod (non-test) counts are given separately.

```yaml
version: "2"
linters:
  exclusions:
    presets: [comments, common-false-positives, legacy, std-error-handling]
    rules:
      - path: _test\.go
        linters: [errcheck]
```

| run | before | after |
|---|---|---|
| raw linux, total | 467 | 458 |
| raw linux, prod | 23 = app 17 (abandon 9, recovery 3, witness 4, terminate 1) + state 6 | 13 = app 7 (recovery 3, witness 4) + state 6 |
| raw windows, total | 408 | 400 |
| raw windows, prod | 12 = abandon 9 + state 3 | 3 (state only) |
| raw linux `./internal/linuxpreflight/...` | 9 (7 prod + 2 test) | 9 (untouched) |
| **gate linux** | **10, every finding one of the owned sites** | **0** |
| **gate windows** | **9, every finding one of the owned sites** | **0** |

Raw deltas reconcile exactly: −9 abandon −1 `process.Release` +1 new test file's `defer store.Close()`
(test-file findings are raw-only; the gate config's `_test.go` errcheck rule excludes them).
The retargeted prod ledger at base is 23 (app+state) + 7 (linuxpreflight prod: `cgroup_linux.go` 2,
`preflight_linux.go` 5) = 30, reproducing the contract's F11 baseline with the preflight family at
its post-W4-19 path. Owned-file filter (`grep -E 'goal_admission\.go|terminate_linux\.go'`) on the
four after runs: 0 matches each.

Measurement hygiene: the first "before" attempt ran in the edited worktree and overlapped with the
implementation edits; its windows outputs are inconsistent with the pristine tree (e.g. gate-windows
1 finding quoting an already-checked line). It was discarded (`*-unreliable.txt`) and replaced by the
pristine-worktree run after `golangci-lint cache clean`; the before/after pairs above all come from
post-clean runs of the unmodified/graded tree respectively.

## 6. Gates

| gate | command | exit | observation |
|---|---|---:|---|
| format | `gofmt -l` on the three Go files | 0 | no files listed |
| vet | `go vet ./...` (daemon, windows-local) | 0 | no output |
| linux build | `GOOS=linux go build ./...` | 0 | no output |
| windows build | `GOOS=windows go build ./...` | 0 | no output |
| focused tests | `go test -count=1 ./internal/app/ -run 'TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn\|TestFreshCodexPreNativeLaunchWriteFailuresReadBackAndCompensate'` | 0 | `ok … 0.815s`; `-v`: 1 + 4/4 subtests PASS |
| package suite (Windows) | `go test -count=1 ./internal/app/...` | 0 | `ok … 119.454s` (`<run-dir>/gates/windows-app-suite.txt`) |
| linux lane (WSL Ubuntu-24.04) | full suite in a fresh network namespace, §6.1 | 0 | `ok … 132.853s` (`<run-dir>/gates/wsl-app-suite-netns.txt`); targeted run `ok … 0.419s` (`…/wsl-targeted-netns.txt`) |
| whitespace | `git diff --check` | 0 | clean |
| host paths | `git diff origin/main...HEAD -- . ':(exclude)docs' \| grep -ci 'users/'` | 0 | zero in the code/docs-independent scan; without `:(exclude)docs` the count is 3 at `519b6bb9` (`evidence:187`, `evidence:249`, `plan:177` — all three the scan's own quoted text in this record and the plan, which is where the restated counts live); `grep -nE '[:][/\\]Users[/\\]\|[/]home[/]'` over the same diff → 0 hits; worktree references are rendered as `<HOST-PATH>` |
| errcheck | §5 table | 1 before / 0 after (gate-config) | acceptance 10→0 linux, 9→0 windows |
| mutation | §4 entry | 1 (mutant) / 0 (restored) | red exactly at the driven site |

Suite logs: `<run-dir>/gates/windows-app-suite.txt`, `<run-dir>/gates/wsl-app-suite-netns.txt`
(plus the environment-hung first attempt `<run-dir>/gates/wsl-app-suite.txt`, §6.1); errcheck
logs: `<run-dir>/errcheck/{before,after}-{raw,gate}-{linux,windows}.txt` and
`…-raw-linux-preflight.txt` (run dir: `<HOST-PATH>/.local/state/gdd/runs/2026-11-22/
symmetry-rf-w3-09-errcheck-app-implement`).

### 6.1 Linux lane: pre-existing WSL IPv4-loopback defect and the netns workaround

The contract's literal WSL command hangs in this VM **before the change is involved**: in the
`Ubuntu-24.04` distro's shared network namespace, IPv4 loopback connects hang (`net.DialTimeout
"tcp" 127.0.0.1:port` → `i/o timeout`; `ss` shows the client stuck in `SYN-SENT` while the listener
stays `LISTEN`), while `::1` and the eth0 address work; `.wslconfig` sets
`networkingMode=mirrored` (WSL 2.x mirrored networking, VM uptime 10 days). Reproduced at the
pristine base `261e84a4` with the same single test under `-timeout 150s` (exit 1, `panic: test timed
out`, dump in `…/wsl-single-test-pristine.txt`) and in the `symmetry-race` distro (same dial
timeout), so it is environmental and not caused by this slice. The first full-suite attempt
(`…/wsl-app-suite.txt`) died at 600 s on
`TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart/mixed_null_marker_survives_replay_and_restart`,
whose `httptest` listener dials `127.0.0.1`.

The lane was therefore run in a **fresh network namespace** (`sudo unshare -n`, `ip link set lo up`,
as user `accept`) whose loopback is kernel-local and unaffected by the mirrored-networking proxy:
the full `go test -count=1 -timeout 2400s ./internal/app/...` passes there in 132.853 s with the same
Go toolchain (go1.27.0 linux/amd64) and the same tree, which also exercises the owned
`//go:build linux` `terminate_linux.go`. Wrapper scripts: `<HOST-PATH>/wt-w3-09-errcheck-app/lane/`
(`run-linux-lane.sh`, `run-linux-go-test.sh`). The CI Linux/race jobs remain the standing oracle;
repairing the VM's shared loopback (e.g. `wsl --shutdown`) was deliberately not attempted in this
run because it would terminate the user's other WSL sessions and processes.

## 7. Residual limits

- Eight of the nine sites are not individually driven: they share the one mechanical shape
  (capture + join) and are covered by the gate-config errcheck 0 plus the byte-prefix identity; each
  additional drive needs an unrelated fault injection (marshal, `markRunning`, context fetch, receipt
  mismatch) whose only new coverage is the same joining line.
- The raw Close-family ledgers stay as recorded: `linux_owner_recovery_linux.go` 3,
  `linux_owner_witness_linux.go` 4, `internal/state` 6 (linux) / 3 (windows), `internal/linuxpreflight`
  7 — out of scope here (the gate preset excludes them; no recovery meaning).
- The Linux lane ran in WSL `Ubuntu-24.04` (go1.27.0 linux/amd64) inside a fresh network namespace
  because the VM's shared IPv4 loopback is broken (pre-existing, §6.1); the CI daemon race job
  remains the race oracle (no `-race` in this lane). The preflight `internal/linuxpreflight` raw run
  is a linux-only measurement (its files are `//go:build linux`).
- The gate-config run uses a minimal standalone config file (run-dir path recorded above) rather than
  the repo `.golangci.yml`; the repo file's additional test relaxations are no-ops under
  `--default=none --enable=errcheck`, and both measurements are recorded.

## 8. Apply revision (2026-11-22)

Stage `apply` applied the accepted simplify findings. The contract file itself is not editable from
this stage (conductor-owned); its `terminate_linux.go:50` citation is the pre-change line — noted in
§1 and in the plan.

| finding | file | applied change |
|---|---|---|
| 1 | `daemon/internal/app/goal_admission.go:1103-1104` | the shared literal is bound once as `cause` and used as both the abandon cause and the joined primary (2-line form: no line added, no coordinate moved, `git diff -U0` stays 9 hunks / 36 changed lines; rendered text, `errors.Is`/`errors.As` and evaluation order unchanged) |
| 2 | `daemon/internal/app/terminate_linux.go:50-51` | the stated-reason comment reworded to "A released handle cannot change this helper's stop-proof verdict, so its release error is unactionable." (the previous "failures above" mis-scoped the stop-proof arm below the defer; line count unchanged) |
| D1 | record (§2, plan) | the single-nil-operand `errors.Join` invariant corrected: `*joinError` wrap, text and `errors.Is`/`errors.As` preserved, identity and dynamic type not, `errors.Unwrap` nil |
| D2 | record (§1, plan) | the contract's `:50` citation is the pre-change line; delivered comment `:50-51`, defer `:52` |
| D3 | record (§2, §6, plan gates row) | restated at the frozen revision `519b6bb9` (fix revision): the `users/` scan returns 3 — its own quoted text twice in this record plus once in the plan, 0 with `-- . ':(exclude)docs'` — and the seven-string `-E` grep returns the new test `:57`, the seven site literals in `goal_admission.go` and the tables quoting the sites; the original "2" was the count at the intermediate HEAD `2490c088`, the pre-change runs (0 matches each) stand as recorded |
| count fix | record (§2) | measured while republishing: the prefix-identity sentence said "six `fmt.Errorf` sites"; the nine delivered returns carry five (`:1088`, `:1099`, `:1113`, `:1122`, `:1129`), plus `:1117`, `:1103`, `:1143`, `:1156` — 5 + 4 = 9, matching the 9 hunks |

Re-runs at the apply revision (logs in
`<HOST-PATH>/.local/state/gdd/runs/2026-11-22/symmetry-rf-w3-09-errcheck-app-apply/`):

- errcheck after-runs (`errcheck/after-*.txt`, cache cleaned first): gate-linux 0 (rc 0),
  gate-windows 0 (rc 0), raw-linux 458, raw-windows 400, raw-linux-preflight 9; owned-file filter
  0 matches on both gates; raw prod 13 (linux: recovery 3, witness 4, state 6) / 3 (windows: state)
  — identical to §5. The before side was re-measured on a fresh detached worktree at `261e84a4`
  (`errcheck/before-*.txt`, worktree removed afterwards): gate-linux 10 / gate-windows 9, raw-linux
  467 / raw-windows 408, preflight 9, raw prod 23 / 12 — the same pair as §5, so the 10→0 / 9→0
  acceptance deltas are re-verified on both sides.
- suites: focused rc 0 (`gates/focused.txt`; the abandon test PASS 0.23s + 4/4 subtests of the
  shape-reference test); Windows `./internal/app/...` rc 0 (`gates/windows-app-suite.txt`,
  `ok … 105.803s`); WSL `Ubuntu-24.04` netns lane rc 0 (`gates/wsl-app-suite-netns.txt`,
  `ok … 133.157s`) and targeted rc 0 (`gates/wsl-targeted-netns.txt`, abandon test PASS 0.28s).
- mutation: site `:1088` reverted to the pre-change dropped call → `gates/mutation-red-apply.txt`
  exit 1, `--- FAIL: TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn (0.27s)` (`admission
  error = load native run fence: decode run journal: unexpected end of JSON input, want the
  abandon-internal sentinel joined`); restored from a byte backup — sha256 back to `e9e7494a…`,
  `gofmt -l` clean — and the focused gate re-ran rc 0 (`gates/focused-after-restore-apply.txt`).
- static: `gofmt -l` 0 files, `go vet ./...` rc 0, `GOOS=linux` and `GOOS=windows` builds rc 0,
  `git diff --check` clean including the uncommitted apply edits (`gates/gofmt.txt`,
  `gates/vet.txt`, `gates/build-linux.txt`, `gates/build-windows.txt`; `git diff --check` is in
  `gates/apply-measurements.txt`).
- re-measured record inputs (`gates/apply-measurements.txt`): `requireNativeSessionCloseRetry`
  worktree grep identical to `dc0aa35` (definition `:1273`, sites
  `:1080`/`:1422`/`:1439`/`:1478`/`:1494`); dropped-call grep on the worktree 0 lines; `git diff
  -U0` 9 hunks / 36 changed lines; no `//nolint` in the three Go files.

## 9. Fix revision (2026-11-22, stage `fix`)

Review findings fixed in this stage: `W309-R1` (record statements restated against the frozen
revision), `W309-R3` (the new test no longer inherits the fixture's hardcoded deadline) and `W309-R4`
(the join's primary-cause pin). `W309-R2` (the durable terminal-transition capacity interaction) needs
`events.go`/`outbox.go`/`state.go`, all outside this slice's owned set: it is recorded as debt in
`docs/TODO.md` ("Daemon: the durable terminal-transition payload has no capacity policy") and reported
as unmet rather than patched here.

Test changes (`daemon/internal/app/abandon_error_join_test.go`): the admission deadline is overridden
to `time.Now().UTC().Add(time.Hour)` before the fixture helper runs; after the exact-text assertion,
the returned error's outermost join frame must carry exactly two terms and `terms[0]` must still reach
the re-read journal's `*json.SyntaxError` chain (that assertion is first made on the re-read error, so
a fixture type change fails loudly). Mutation evidence (scratch copies + `go test -overlay`, saved in
`gates/mutants/` with `run-mutants.sh`):

| mutant | overlay | result |
|---|---|---|
| `:1089` joined primary flattened to `errors.New(err.Error())` | `mutants/overlay-A.json` | rc 1, FAIL `joined primary = decode run journal: unexpected end of JSON input, want the run-journal decode error chain` — the new pin, red (`gates/mutant-A.txt`) |
| fixture `deadline_at` moved to `2025-12-31` (expired-deadline calendar) | `mutants/overlay-B.json` | rc 0, `ok … 0.475s` — the test is calendar-independent now; the pre-fix tree failed red on this mutant (`gates/mutant-B.txt`) |
| site `:1088` reverted to the pre-change dropped call | `mutants/overlay-C.json` | rc 1, FAIL `admission error = load native run fence: …, want the abandon-internal sentinel joined` — red-on-flip preserved (`gates/mutant-C.txt`) |

Re-run gates at the fix revision (logs in
`<HOST-PATH>/.local/state/gdd/runs/2026-11-22/symmetry-rf-w3-09-errcheck-app-fix/gates/`):

| gate | command | exit | observation |
|---|---|---:|---|
| format | `gofmt -l` on the three Go files | 0 | no files listed (`gofmt.txt`) |
| vet | `go vet ./...` (daemon) | 0 | no output (`vet.txt`) |
| builds | `GOOS=linux` / `GOOS=windows go build ./...` | 0 / 0 | no output (`build-linux.txt`, `build-windows.txt`) |
| focused tests | `go test -count=1 -v ./internal/app/ -run 'TestAbandonGoalSessionErrorJoinsIntoAdmissionReturn\|TestFreshCodexPreNativeLaunchWriteFailuresReadBackAndCompensate'` | 0 | abandon PASS 0.25s + 4/4 subtests PASS, `ok … 0.663s` (`focused.txt`) |
| errcheck, gate config | §5 gate command, linux / windows | 0 / 0 | 0 findings each; owned-file filter 0 (`after-gate-linux.txt`, `after-gate-windows.txt`) |
| errcheck, raw | §5 raw command, linux / windows | 1 / 1 | 458 / 400; owned-file filter 0; prod 13 (recovery 3, witness 4, state 6) / 3 (state) — identical to §5 (`after-raw-linux.txt`, `after-raw-windows.txt`) |
| combine check | `golangci-lint run --no-config -c errcheck-gate.yml …` | 3 | `can't combine option --config and --no-config` — verifies §5's corrected wording (`errcheck-combine-check.txt`) |
| package suite (Windows) | `go test -count=1 ./internal/app/...` | 0 | `ok … 117.535s` (`windows-app-suite.txt`) |
| linux lane (WSL netns) | `run-linux-lane.sh` full / targeted | 0 / 0 | `ok … 143.582s` / `ok … 0.429s` (abandon PASS 0.22s, 4/4 subtests) (`wsl-app-suite-netns.txt`, `wsl-targeted-netns.txt`) |
| whitespace | `git diff --check` | 0 | clean |
| host paths | §6 scan row | 0 | 3 unrestricted, all the scan's own quoted text (`evidence:187`, `evidence:249`, `plan:177`); 0 with `:(exclude)docs`; 0 profile paths |

Record corrections (`W309-R1`): §2's dropped-call grep is revision-bound (`261e84a4` → nine,
`519b6bb9` → zero) and its seven-string grep carries `-E` and the measured 7/8 line counts; §5 states
that the gate run **replaces** `--no-config` with `-c` (rc 3 verified); §6 and the plan's gates row
name the discriminating host-path scan and the frozen-revision anchors; §8's D3 row and static-gate
list are restated against the actual files (`gates/gofmt.txt`, `vet.txt`, `build-*.txt`,
`apply-measurements.txt`). The plan's invariant 1/2 greps now carry `-E`, their file scope and the
measured counts, and the plan's gates row points at the corrected scan. Finding (f) — the implement
stage's claim that the prefix diff lives in its own `answer.md:36` — concerns a file outside this
repository (the implement run's record); it is reported as unmet with that exact path instead of being
edited, and the prefix table stays in the plan (`:48-58`) and §2 of this record.

The fix run dir keeps the gate config at `errcheck/errcheck-gate.yml` (copy in `gates/`), so §5's
`<run-dir>/errcheck/errcheck-gate.yml` path holds for this stage too.
