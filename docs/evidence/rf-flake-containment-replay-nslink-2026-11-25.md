# rf-flake-containment-replay-nslink — evidence record

Goal: classify the procfs reap-race `EACCES` in the containment identity-gone wait.
Contract: `goal-rf-flake-containment-replay-nslink.md` (GDD state root, external to the
repository). Plan: `docs/plans/2026-11-25-rf-flake-containment-replay-nslink-plan.md`.
Base revision: `3b23dead` (dispatch HEAD). Subject: `daemon/e2e/production_linux_containment_witness_test.go`,
sha256 `55fd6949af66bd00c4f67d2e2bbadbd764507b12f70808dd1e09bad2aac61278`
(`git diff --numstat` `420 12`; `git diff --check` clean).

Environment: WSL `Ubuntu-24.04`, user `accept` (uid 1000, non-root, 16 CPUs), Go `go1.27.0
linux/amd64`, kernel `6.18.58.1-microsoft-standard-WSL2`. Scratch root `<scratch>` (`base/` = base
tree + K1 only, `head/` = head tree + pins, `logs/`, `scripts/`); worktree `<worktree>`. Every run
is foreground and sequential.

## 1. The classification rule (contract clause 5)

What changed (one test file): the F1 decision in the shared wait; the P1 probe record on the
unexpected-error return; the F2 `waitForLinuxWitnessIdentityGoneWith` seam with a one-line wrapper;
the single `/proc/<pid>/stat` parse shared by the probe and `linuxWitnessProcessSnapshotAt`; pins K1
and K2 with the re-exec child mode. No production file changed.

`waitForLinuxWitnessIdentityGoneWith(pid, identity, timeout, read, probe)` — the shared
identity-gone wait — decides absence on `errors.Is(err, syscall.EACCES)` from the identity read
with exactly one `/proc/<pid>/stat` probe:

| probe result | outcome |
| --- | --- |
| read error ENOENT/ESRCH | `nil` — the dentry is retired |
| any other read error, or an unparseable payload | classified fail-closed error |
| readable, state `Z` | `nil` — unreaped zombie |
| readable, `identity == ""` | keep polling; deadline error unchanged |
| readable, recorded starttime differs (`linux:v2:` marker's trailing field) | `nil` — the pid no longer carries the recorded identity |
| readable, recorded starttime equal, or the marker is not parseable | classified fail-closed error |

Classified error text (observed in K1):
`process 93251 identity "linux:v2:…:93251:8396518" is unreadable after a denied identity read: /proc/93251/stat shows state=S starttime=8396518`;
on a probe failure the `shows …` clause is `probe failed: <err>`. It never returns `nil`, never
repeats the raw syscall error, never claims the process `remained live`.

Product semantics untouched: `ProcessIdentity` keeps EACCES as unproven and
`ProvePersistedProcessGroupAbsent` never reads the target ns link. No product file was edited
(the mutants below live only in `<scratch>/head` and are restored byte-identical).

## 2. Base census (lesson 73): K1 red on base

`<scratch>/base` is the base tree with only pin K1 and its child-mode scaffolding added
(`linuxWitnessProcessSnapshotAt` builds the canonical identity, so the base copy compiles without
any part of the fix). Command (literal):

```
cd <scratch>/base/daemon && go test -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v
```

`exit=1`, `red_rows=1` (`<scratch>/logs/base-census.log`), exactly K1:

```
=== RUN   TestLinuxWitnessIdentityGoneDeniedChildProcess
    production_linux_containment_witness_test.go:3684: child mode only: … spawn this test binary with SYMMETRY_LINUX_WITNESS_DENIED_IDENTITY_CHILD=1
--- SKIP: TestLinuxWitnessIdentityGoneDeniedChildProcess (0.00s)
=== RUN   TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed
    production_linux_containment_witness_test.go:3844: unreadable live process 94044 classified after 0s: read process 94044 PID namespace: readlink /proc/94044/ns/pid: permission denied
    production_linux_containment_witness_test.go:3853: classified unreadable-identity error "read process 94044 PID namespace: readlink /proc/94044/ns/pid: permission denied" does not name "is unreadable after a denied identity read"
--- FAIL: TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed (0.05s)
FAIL	github.com/wxxb789/symmetry/daemon/e2e	0.092s
```

The red also proves the denial leg: the real `PR_SET_DUMPABLE 0` denial fails the **readlink** leg
(unwrapped by the product), which is why M3 (matching `stat namespace link` text) reds K1.

## 3. Pins green on the shipping bytes

Command (same literal shape, head tree; also run in the worktree — identical bytes):

```
cd <scratch>/head/daemon && go test -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v
```

`exit=0`, `red_rows=0` (`<scratch>/logs/head-pins.log`; worktree in `logs/worktree-pins.log`):

```
--- SKIP: TestLinuxWitnessIdentityGoneDeniedChildProcess (0.00s)
=== RUN   TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed
    production_linux_containment_witness_test.go:3951: unreadable live process 93251 classified after 0s: process 93251 identity "linux:v2:3a843c9a-0118-4f5f-95b7-0d7a3d8b805c:4026532213:93251:8396518" is unreadable after a denied identity read: /proc/93251/stat shows state=S starttime=8396518
--- PASS: TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed (0.02s)
=== RUN   TestLinuxWitnessIdentityGoneDeniedReadDecisions
    --- PASS: …/reaped_pid_probe_missing_is_gone (0.04s)
    --- PASS: …/zombie_state_is_gone (0.12s)
    --- PASS: …/doctored_starttime_is_gone (0.00s)
    --- PASS: …/live_pid_real_identity_stat_namespace_link_fails_closed (0.00s)
    --- PASS: …/live_pid_real_identity_readlink_fails_closed (0.00s)
    --- PASS: …/non_denial_error_carries_the_probe_record (0.00s)
    --- PASS: …/empty_identity_keeps_polling (0.52s)
ok  	github.com/wxxb789/symmetry/daemon/e2e	0.794s
```

`wsl -d Ubuntu-24.04 -u accept -- bash <scratch>/scripts/head-pins.sh` → `head-pins exit=0
red_rows=0`; `worktree-pins exit=0 red_rows=0`.

### 3a. Implement-stage observation: one over-specified pin assertion, fixed before sealing

The first full-suite run on the pre-fix pin bytes (`<scratch>/logs/head-suite.log`, subject hash
`8c87ba74…`) reddened `non_denial_error_carries_the_probe_record` under package-parallel load: the
row pinned the child's `/proc/<pid>/stat` *state letter* captured at row entry (`state=S`) while the
probe inside the P1 record read the child mid-wake (`state=R`). The child is a blocked Go test
process; its transient scheduler state is not part of the contract. The row now pins the record's
field presence and the stable values (`exists=true state=` … ` starttime=<N> err=<nil>`), exactly as
the classified-error rows already did. All runs in §3, §4, §7 and §8 are on the fixed bytes
(sha256 `55fd6949…`); the failing run is kept in the log as the reproduction.

## 4. Mutants (scratch copy; rows name the CI job that must redden: `daemon`)

Each mutation is applied to `<scratch>/head/…/production_linux_containment_witness_test.go`, the
pins run, then the pristine blob is restored from
`production_linux_containment_witness_test.go.pristine`; the sha256 after every restore equals the
pristine value `55fd6949af66bd00c4f67d2e2bbadbd764507b12f70808dd1e09bad2aac61278`.

| id | mutation (diff vs pristine) | command | exit | observed reds |
| --- | --- | ---: | --- | --- |
| M1 | delete the EACCES decision (+ its 2 comment lines); EACCES falls into the unexpected-error return | `mutant-run.sh M1` | 1 | 8 rows: K1, K2, `reaped_pid_probe_missing_is_gone`, `zombie_state_is_gone`, `doctored_starttime_is_gone`, both real-identity rows, `empty_identity_keeps_polling` |
| M2 | `return nil` on EACCES, no probe | `mutant-run.sh M2` | 1 | 5 rows: K1, K2, both real-identity rows, `empty_identity_keeps_polling` |
| M3 | decision condition `err != nil && strings.Contains(err.Error(), "stat namespace link")` | `mutant-run.sh M3` | 1 | 4 rows: K1, K2, `live_pid_real_identity_readlink_fails_closed`, `doctored_starttime_is_gone` |
| M4 | decision case negated: starttime **equal** → `nil` | `mutant-run.sh M4` | 1 | 5 rows: K1, K2, both real-identity rows, `doctored_starttime_is_gone` |
| M5 | `identity == ""` + readable live + EACCES → `return nil` | `mutant-run.sh M5` | 1 | 2 rows: K2, `empty_identity_keeps_polling` |

Every contract-listed row reddens; the extra reds are the same mutant truncating other branches
(the contract's cells name the deciding rows, not an upper bound). Logs:
`<scratch>/logs/mutant-{M1,M2,M3,M4,M5}.log`. The `non_denial_error_carries_the_probe_record` row
stays green under M1-M5 by construction (it injects EIO, not EACCES).

## 5. Base-vs-head branch census of the helper

| input class | base outcome | head outcome | previous red made green? |
| --- | --- | --- | --- |
| `pid <= 0` | `nil` | `nil` | no |
| read ENOENT | `nil` | `nil` | no |
| read ESRCH + `/proc/<pid>/stat` ENOENT | `nil` | `nil` | no |
| read ESRCH + stat error | wrapped error | unchanged | no |
| read ESRCH + stat exists | sleep 25 ms, continue | unchanged | no |
| read EACCES + probe ENOENT/ESRCH | **error (red)** | `nil` | **yes — reap straddle class** |
| read EACCES + probe readable `Z` | **error (red)** | `nil` | **yes — reap straddle class** |
| read EACCES + probe readable, starttime differs | **error (red)** | `nil` | **yes — pid-reuse/other-process class** |
| read EACCES + probe readable, starttime equal | error | classified error | no (still red, classified text) |
| read EACCES + probe other error / unparseable | error | classified error | no (still red, classified text) |
| read EACCES + `identity == ""` + readable live | error | continue polling → deadline error | no (still red) |
| read other (EIO/EPERM/…) | error | error + probe record (wrapped, `errors.Is` kept) | no |
| read ok, identity mismatch | `nil` | `nil` | no |
| read ok, live non-`Z` | sleep 25 ms, continue | unchanged | no |
| read ok, `Z` | `nil` | `nil` | no |
| deadline | `process %d with identity %q remained live` | byte-identical | no |

No assertion and no branch of the base helper was removed; the only red→green transitions are the
three EACCES absence classes, i.e. exactly the classes the contract's F1 mandates as gone (probe
ENOENT/ESRCH and probe `Z` are the reap straddle; a differing starttime is the pid that no longer
carries the recorded identity). No other input flips.

## 6. Class census (conductor ruling Q2, lesson 85)

All `58` `platform.ProcessIdentity` call sites in `daemon/**/*_test.go` were classified by whether
the read can repeatedly straddle a reap. Full table with per-site reasons: plan D5. Result: the
only repeated-read disappearance wait is `waitForLinuxWitnessIdentityGone` itself (`38` direct call
sites, plus `25` through `waitForLinuxWitnessProcessStopped`), now carrying the decision; every
other site is a one-shot read whose vantage point is before the reap (live assertion/audit:
fail-closed is correct) or after it (post-`release_task`: ENOENT is deterministic), or a one-shot
absence assertion with its own classification (`retiredProcessIdentityError`, `core_test.go:1441`;
`assertPreAuthorityProcessStopped`, `production_pre_authority_crash_witness_test.go:417/445`). No
file outside the owned set needed an edit, so the ruling's routing is satisfied by construction.

## 7. Gates

| gate | command | exit | result |
| --- | --- | ---: | --- |
| base census | `<scratch>/base/daemon: go test -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` | 1 | exactly K1 red (§2) |
| pins | `<scratch>/head/daemon` and `<worktree>/daemon`: same command | 0 / 0 | §3 |
| format | `gofmt -l .` (`<scratch>/head/daemon`) | 0 | 0 files listed |
| vet | `go vet ./...` (`<scratch>/head/daemon`) | 0 | clean |
| suite | `go test -count=1 -timeout 900s ./...` (`<scratch>/head/daemon`) | 0 | green on the shipping bytes, pins included |
| race, changed package | `go test -race -count=1 -timeout 600s ./e2e` (`<scratch>/head/daemon`) | 0 | `ok … 2.161s` |
| race, pins | `go test -race -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` | 0 | K1 `0.12s`, K2 `0.89s`, 0 red rows, 0 stray child processes before/after |
| race, full module | `go test -race -count=1 -timeout 900s ./...` (`<scratch>/head/daemon`) | 1 | per-binary budget expired in the untouched `internal/app` (panic after 15m0s while the running test had 42s); see §8 |
| diff hygiene | `git diff --check` (worktree) | 0 | clean |
| mutants | §4 | 1 × 5 | every contract-listed row red |
| `daemon` CI job | CI-owned on the PR head (pins run in `go test -race … ./...`, `ci.yml:1096-1105`) | — | not runnable here (no push); local equivalents above |
| `integration` witness matrix | CI-owned on the PR head (`ci.yml:1659`) | — | not runnable here; the flake cannot be forced to red (plan D6), the pins are the mutant oracle |

## 8. Suite and race runs

Plain suite on the shipping bytes (`scripts/final-gates.sh`, `<scratch>/logs/final-suite.log`):
`go test -count=1 -timeout 900s ./...` → `final-suite exit=0`; 30 `ok` lines and 0 `FAIL` lines
(`e2e` `0.977s`, `internal/app` `249.126s`, `internal/platform` `69.562s`). This is the local
equivalent of the `daemon` job's aggregate (that job runs the same tree under `-race`, which CI owns).

Race runs (`<scratch>/logs/final-race-e2e.log`, `final-race-pins.log`, `final-race.log`):

- changed package: `go test -race -count=1 -timeout 600s ./e2e` → exit 0, `ok … 2.161s`;
- pins: `go test -race -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` →
  exit 0, K1 `0.12s`, K2 `0.89s`, `red_rows=0`; `pgrep -f e2e.test` = 0 before and after, so the
  re-exec children do not leak;
- full module: `go test -race -count=1 -timeout 900s ./...` → exit 1, `panic: test timed out after
  15m0s` in `internal/app` (900.262s) with `TestDurablePiProviderBridgeExecutorReservesCompletionCapacityBeforeDispatch`
  at 42s — the package was still progressing when the per-binary budget expired. `internal/app` is
  untouched by this diff and is green in the plain suite (`249.126s` on this host); the race overhead
  is ~5-9x here (`protocol` 47.9s, `state` 94.9s, `workspace` 138.4s under race). Recorded as a
  local-host budget limit, not a red of the change; the blocking `daemon` job owns the full
  `-race ./...` verdict on the PR head.

## 9. Residual risks and known limits

1. The reap straddle cannot be forced to red on demand, so the mutant rows are the oracle (contract
   Gate "Witness oracle"); the `integration` witness matrix and the `daemon` job remain CI-owned — a
   locally green run is not those jobs' evidence.
2. The decision classifies the reap-window EACCES source only; a ptrace-denied **live** process
   still reds by design (the contract's fail-closed requirement, not a residual).
3. The test-side starttime parse (`linuxWitnessIdentityStartTimeDiffers`) mirrors the `linux:v2:`
   marker's trailing field. If the product ever changes the marker, the pins build the canonical
   identity independently and cross-check it against `platform.ProcessIdentity`, so the pins red
   instead of silently weakening.
4. A non-empty identity that is not a parseable marker takes the fail-closed branch (plan D9): it
   cannot prove absence and cannot mask a live process.
5. The P1 record wraps the unexpected error (message suffix, `errors.Is` preserved). No call site
   matched the raw message text.
6. `retiredProcessIdentityError` (`core_test.go:1441`) keeps its one-shot ENOENT/ESRCH-only
   classification; it is an assertion, not a wait (§6). No red of record exists for it; if that
   assertion ever reds, it opens its own goal rather than a silent widening here.
7. `linuxWitnessProcessSnapshotAt`'s error strings are unchanged (`malformed /proc stat`,
   `short /proc stat`), so the evidence JSON written by the witnesses is unaffected.
