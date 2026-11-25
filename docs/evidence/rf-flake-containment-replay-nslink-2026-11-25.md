# rf-flake-containment-replay-nslink — evidence record

Goal: classify the procfs reap-race `EACCES` in the containment identity-gone wait.
Contract: `goal-rf-flake-containment-replay-nslink.md` (GDD state root, external to the
repository). Plan: `docs/plans/2026-11-25-rf-flake-containment-replay-nslink-plan.md`.
Base revision: `3b23dead` (dispatch HEAD). Subject: `daemon/e2e/production_linux_containment_witness_test.go`,
sha256 `00cdad06fd7d020dcef380f8267c7a2bb928452fb0bf61b4f0b4d8bb0324af76`
(`git diff --numstat 3b23dead` `535 20`, of which the 2026-11-25 apply pass is `103 60` on top of
`f5e9ea7d` and the fix round `64 0` on top of `919a0902`; `git diff --check` clean).

Environment: WSL `Ubuntu-24.04`, user `accept` (uid 1000, non-root, 16 CPUs), Go `go1.27.0
linux/amd64`, kernel `6.18.58.1-microsoft-standard-WSL2`. Scratch root `<scratch>` (`base/` = base
tree + K1 only, `head/`/`apply/`/`fix/` = stage copies of the module, `logs/` and `logs-fix/`,
`scripts/` and `scripts-fix/`, plus a native ext4 copy of the fix tree for the mutant harness and
the load repeat); worktree `<worktree>`. Every run is foreground and sequential.

## 1. The classification rule (contract clause 5)

What changed (one test file): the F1 decision in the shared wait; the P1 probe record on the
unexpected-error return; the F2 `waitForLinuxWitnessIdentityGoneWith` seam with a one-line wrapper;
the single `/proc/<pid>/stat` parse shared by the probe and `linuxWitnessProcessSnapshotAt`; pins K1
and K2 with the re-exec child mode. No production file changed. The 2026-11-25 apply pass then
simplified the helper and pins, closed the probe-parse gap (plan D10) and added two probe-failure
rows plus a read counter; the fix round then pinned the gone arm's ESRCH half
(`probe_esrch_is_gone`), split the payload parse into the pure `parseLinuxWitnessProcessSnapshot`
and pinned it (`TestLinuxWitnessProcessSnapshotPayloadDecisions`). The decision table below is
unchanged throughout.

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

Product semantics untouched: `ProcessIdentity` (`daemon/internal/platform/identity_linux.go:75/:113`)
returns the wrapped syscall error for a denied ns-link read, the helper's denial classification is
only `errors.Is(err, syscall.EACCES)`, and the `ErrPersistedProcessGroupUnproven` sentinel is
produced by `ProvePersistedProcessGroupAbsent` (`:259`), which never reads the target ns link. No
product file was edited (the mutants below live only in the scratch copies and are restored
byte-identical).

## 2. Base census (lesson 73): K1 red on base

`<scratch>/base` is the base tree with only pin K1 and its child-mode scaffolding added
(`linuxWitnessProcessSnapshotAt` builds the canonical identity, so the base copy compiles without
any part of the fix). Command (literal):

```
cd <scratch>/base/daemon && go test -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v
```

`exit=1`, `red_rows=1` (`<scratch>/logs-fix/final-base-census.log`), exactly K1:

```
=== RUN   TestLinuxWitnessIdentityGoneDeniedChildProcess
    production_linux_containment_witness_test.go:3684: child mode only: … spawn this test binary with SYMMETRY_LINUX_WITNESS_DENIED_IDENTITY_CHILD=1
--- SKIP: TestLinuxWitnessIdentityGoneDeniedChildProcess (0.00s)
=== RUN   TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed
    production_linux_containment_witness_test.go:3844: unreadable live process 153283 classified after 0s: read process 153283 PID namespace: readlink /proc/153283/ns/pid: permission denied
    production_linux_containment_witness_test.go:3853: classified unreadable-identity error "read process 153283 PID namespace: readlink /proc/153283/ns/pid: permission denied" does not name "is unreadable after a denied identity read"
--- FAIL: TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed (0.06s)
FAIL	github.com/wxxb789/symmetry/daemon/e2e	0.089s
```

The red also proves the denial leg: the real `PR_SET_DUMPABLE 0` denial fails the **readlink** leg
(unwrapped by the product), which is why M3 (matching `stat namespace link` text) reds K1.

## 3. Pins green on the shipping bytes

Command (literal, post-fix worktree; the fix-round re-run of the contract's pin command):

```
cd <worktree>/daemon && go test -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v
```

`exit=0`, `red_rows=0`, `k2sub=10` (`<scratch>/logs-fix/final-pins.log`):

```
--- SKIP: TestLinuxWitnessIdentityGoneDeniedChildProcess (0.00s)
=== RUN   TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed
    production_linux_containment_witness_test.go:3977: unreadable live process 153480 classified after 0s: process 153480 identity "linux:v2:3a843c9a-0118-4f5f-95b7-0d7a3d8b805c:4026532213:153480:9036504" is unreadable after a denied identity read: /proc/153480/stat shows state=S starttime=9036504
--- PASS: TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed (0.02s)
=== RUN   TestLinuxWitnessIdentityGoneDeniedReadDecisions
    production_linux_containment_witness_test.go:4083: identity read failure record: read process 153486 start time: input/output error (identity read failure: leg=read process start time errno=5(input/output error) poll=1 elapsed=0s; /proc/153486/stat probe: exists=true state=S starttime=9036506 err=<nil>)
    --- PASS: …/reaped_pid_probe_missing_is_gone (0.02s)
    --- PASS: …/probe_esrch_is_gone (0.00s)
    --- PASS: …/zombie_state_is_gone (0.03s)
    --- PASS: …/doctored_starttime_is_gone (0.00s)
    --- PASS: …/live_pid_real_identity_stat_namespace_link_fails_closed (0.00s)
    --- PASS: …/live_pid_real_identity_readlink_fails_closed (0.00s)
    --- PASS: …/non_denial_error_carries_the_probe_record (0.00s)
    --- PASS: …/denied_read_probe_failure_fails_closed (0.00s)
    --- PASS: …/non_denial_error_records_probe_failure (0.00s)
    --- PASS: …/empty_identity_keeps_polling (0.51s)
ok  	github.com/wxxb789/symmetry/daemon/e2e	0.624s
```

The fix round's unit pin runs in the same package:

```
cd <worktree>/daemon && go test -count=1 -timeout 120s ./e2e -run "^TestLinuxWitnessProcessSnapshotPayloadDecisions$" -v
```

`exit=0`, `red_rows=0`, `rows=4` (`<scratch>/logs-fix/final-parse.log`):
`complete_payload_parses`, `short_payload_is_rejected`, `missing_closing_parenthesis_is_rejected`,
`non_decimal_starttime_is_rejected` — all PASS (`ok … 0.043s`).

### 3a. Implement-stage observation: one over-specified pin assertion, fixed before sealing

The first full-suite run on the pre-fix pin bytes (`<scratch>/logs/head-suite.log`, subject hash
`8c87ba74…`) reddened `non_denial_error_carries_the_probe_record` under package-parallel load: the
row pinned the child's `/proc/<pid>/stat` *state letter* captured at row entry (`state=S`) while the
probe inside the P1 record read the child mid-wake (`state=R`). The child is a blocked Go test
process; its transient scheduler state is not part of the contract. The row now pins the record's
field presence and the stable values (`exists=true state=` … ` starttime=<N> err=<nil>`), exactly as
the classified-error rows already did. All runs in §3, §4, §7 and §8 are on the shipping bytes
(implement-stage sha256 `55fd6949…`; the apply pass then rewrote the helper and pins, and the fix
round rewrote them again — §3/§4/§7/§8 below were re-run on the fix-round bytes `00cdad06…`); the
failing run is kept in the log as the reproduction.

## 4. Mutants (scratch copies; rows name the CI job that must redden: `daemon`, non-root runner)

Each mutation is applied to the fix scratch copy
(`<scratch>/fix/daemon/e2e/production_linux_containment_witness_test.go` or its native ext4 twin)
with `patch` (`scripts-fix/mutants/M<id>.patch`; the applied diff is captured verbatim as
`<scratch>/logs-fix/mutant-M<id>.patch.diff`), the pins and the payload pin run
(`run-mutants-native.sh`, one id per row), then the pristine blob is restored from
`production_linux_containment_witness_test.go.pristine`; the sha256 after every restore equals the
pristine value `00cdad06fd7d020dcef380f8267c7a2bb928452fb0bf61b4f0b4d8bb0324af76`. M1-M5 are the
apply-pass mutations re-applied to the fix bytes (same mutation text); M6-M8 are the fix round's.

| id | mutation (diff vs pristine) | command | exit | observed reds |
| --- | --- | ---: | --- | --- |
| M1 | delete the EACCES decision (+ its 2 comment lines); EACCES falls into the unexpected-error return | `run-mutants-native.sh M1` | 1 | 10 rows: K1, K2, `reaped_pid_probe_missing_is_gone`, `probe_esrch_is_gone`, `zombie_state_is_gone`, `doctored_starttime_is_gone`, both real-identity rows, `denied_read_probe_failure_fails_closed`, `empty_identity_keeps_polling` |
| M2 | `return nil` on EACCES, no probe | `run-mutants-native.sh M2` | 1 | 6 rows: K1, K2, both real-identity rows, `denied_read_probe_failure_fails_closed`, `empty_identity_keeps_polling` |
| M3 | decision condition `err != nil && strings.Contains(err.Error(), "stat namespace link")` | `run-mutants-native.sh M3` | 1 | 4 rows: K1, K2, `live_pid_real_identity_readlink_fails_closed`, `doctored_starttime_is_gone` |
| M4 | decision case negated: starttime **equal** → `nil` (`!linuxWitnessIdentityStartTimeDiffers(...)`) | `run-mutants-native.sh M4` | 1 | 5 rows: K1, K2, both real-identity rows, `doctored_starttime_is_gone` |
| M5 | `identity == ""` + readable live + EACCES → `return nil` | `run-mutants-native.sh M5` | 1 | 2 rows: K2, `empty_identity_keeps_polling` |
| M6 | gone arm: drop `|| errors.Is(probeErr, syscall.ESRCH)` | `run-mutants-native.sh M6` | 1 | 2 rows: K2, `probe_esrch_is_gone` |
| M7 | parse: the starttime `ParseUint` failure branch sets `startTime = 0` and falls through (no error) | `run-mutants-native.sh M7` | 1 | 2 rows: `TestLinuxWitnessProcessSnapshotPayloadDecisions`, `non_decimal_starttime_is_rejected` |
| M8 | parse: `short /proc stat` returns `nil` instead of the error | `run-mutants-native.sh M8` | 1 | 2 rows: `TestLinuxWitnessProcessSnapshotPayloadDecisions`, `short_payload_is_rejected` |

Every contract-listed row reddens; the extra reds are the same mutant truncating other branches
(the contract's cells name the deciding rows, not an upper bound; M6-M8 each red exactly their
deciding row plus its parent). Logs: `<scratch>/logs-fix/mutant-M{1..8}.log`. The
`non_denial_error_carries_the_probe_record` row stays green under M1-M8 by construction (it injects
EIO, not EACCES); the four payload rows stay green under M1-M6 (the parse is untouched).

Mutation patches, verbatim (against the pristine blob
`sha256 00cdad06fd7d020dcef380f8267c7a2bb928452fb0bf61b4f0b4d8bb0324af76`; M1 removes the block,
M2 replaces it with the two-line `return nil` form, M3-M5 are the single-line edits shown; the
fix-round diffs exactly as applied are `<scratch>/logs-fix/mutant-M{1..8}.patch.diff`):

```
M1 (delete):
		if errors.Is(err, syscall.EACCES) {
			// The kernel returns one EACCES for a reap-window detach and for a ptrace denial on a
			// live process, so absence is decided by the recorded starttime, never by the errno.
			snapshot, probeErr := probe(pid)
			switch {
			case errors.Is(probeErr, os.ErrNotExist) || errors.Is(probeErr, syscall.ESRCH):
				return nil
			case probeErr != nil:
				return linuxWitnessDeniedIdentityError(pid, identity, snapshot, probeErr)
			case snapshot.State == "Z":
				return nil
			case identity == "":
				time.Sleep(25 * time.Millisecond)
				continue
			case linuxWitnessIdentityStartTimeDiffers(identity, snapshot.StartTime):
				return nil
			default:
				return linuxWitnessDeniedIdentityError(pid, identity, snapshot, nil)
			}
		}

M2 (replacement):
		if errors.Is(err, syscall.EACCES) {
			return nil
		}

M3: `if errors.Is(err, syscall.EACCES) {` → `if err != nil && strings.Contains(err.Error(), "stat namespace link") {`
M4: `case linuxWitnessIdentityStartTimeDiffers(identity, snapshot.StartTime):` → `case !linuxWitnessIdentityStartTimeDiffers(identity, snapshot.StartTime):`
M5: `case identity == "":` body `time.Sleep(25 * time.Millisecond); continue` → `return nil`

M6: `case errors.Is(probeErr, os.ErrNotExist) || errors.Is(probeErr, syscall.ESRCH):` → `case errors.Is(probeErr, os.ErrNotExist):`
M7: `if err != nil {` body `return linuxWitnessProcessSnapshot{}, errors.New("malformed /proc stat")` → `startTime = 0`
M8: `if len(fields) <= 19 {` body `return linuxWitnessProcessSnapshot{}, errors.New("short /proc stat")` → `return linuxWitnessProcessSnapshot{}, nil`
```

### 4a. Apply-pass scratch unit proof of the starttime predicate (not shipped)

Finding 3 replaced the hand-rolled marker parse with `parseLinuxNegativePIDReuseIdentity`; the pins
only exercise canonical markers, so the partial-malformed behavior was proved with a scratch pin in
`<scratch>/apply/daemon/e2e/apply_scratch_probe_test.go`
(`sha256 c648c3abaebef79567ce9baa8a1fd7392dd754c325a6e567ca564da779023d97`), verbatim:

```go
//go:build linux

package e2e_test

import "testing"

func TestApplyScratchStartTimeDiffers(t *testing.T) {
	cases := []struct {
		identity  string
		startTime uint64
		want      bool
	}{
		{"linux:v2:boot:7:123:5", 5, false},
		{"linux:v2:boot:7:123:5", 6, true},
		{"linux:v2::7:123:5", 6, false},
		{"linux:v2:boot:0:123:5", 6, false},
		{"linux:v2:boot:7:123:", 6, false},
		{"linux:v2:boot:7:0:5", 6, false},
		{"not-an-identity", 6, false},
	}
	for _, c := range cases {
		if got := linuxWitnessIdentityStartTimeDiffers(c.identity, c.startTime); got != c.want {
			t.Errorf("linuxWitnessIdentityStartTimeDiffers(%q, %d) = %v, want %v", c.identity, c.startTime, got, c.want)
		}
	}
}
```

`cd <scratch>/apply/daemon && go test -count=1 -timeout 120s ./e2e -run
"^TestApplyScratchStartTimeDiffers$" -v` → `exit=0`, `--- PASS (0.00s)`, so a canonical marker with
a differing starttime is still `gone`, and every partial-malformed marker (empty boot ID, zero
namespace, missing starttime, zero pid, wrong shape) is fail-closed.

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
carries the recorded identity). No other input flips. The apply pass narrowed "a differing
starttime" to canonical `linux:v2:` markers (`parseLinuxNegativePIDReuseIdentity`): a
partial-malformed marker's differing trailing field no longer takes the gone branch (plan D9) and
takes the classified fail-closed error instead, so no red→green transition is added.

## 6. Class census (conductor ruling Q2, lesson 85)

All `58` `platform.ProcessIdentity(` call sites in `daemon/**/*_test.go` (`38` in `e2e`, `20` in
`daemon/internal`; plus the two value passes `witness_test:3668`, `core_test:1359`) were classified
by whether the read can repeatedly straddle a reap. Full table with per-site reasons: plan D5.
Result: the only repeated-read disappearance wait is `waitForLinuxWitnessIdentityGone` itself
(`38` direct call sites); of the `25` `waitForLinuxWitnessProcessStopped` call sites, the `5` with
an empty identity route into it, while the other `20` prove absence through
`ProvePersistedProcessGroupAbsent` and never read the ns link. Every other site is a one-shot read
whose vantage point is before the reap (live assertion/audit: fail-closed is correct) or after it
(post-`release_task`: ENOENT is deterministic), or a one-shot absence assertion with its own
classification (`retiredProcessIdentityError` — `core_test.go:1359` value pass into `:1364`, whose
predicate is `os.ErrNotExist || syscall.Errno(87)`, not the contract's "ENOENT/ESRCH" wording;
`assertPreAuthorityProcessStopped`, `production_pre_authority_crash_witness_test.go:417/445`). No
file outside the owned set needed an edit, so the ruling's routing is satisfied by construction.

## 7. Gates

| gate | command | exit | result |
| --- | --- | ---: | --- |
| base census | `<scratch>/base/daemon: go test -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` | 1 | exactly K1 red (§2) |
| pins | `<worktree>/daemon` (post-fix): same command | 0 | §3, K2 `10` rows |
| payload pin | `<worktree>/daemon: go test -count=1 -timeout 120s ./e2e -run "^TestLinuxWitnessProcessSnapshotPayloadDecisions$" -v` | 0 | §3, `4` rows |
| load repeat | fix scratch copy: `GOMAXPROCS=8 go test -race -count=200 -timeout 3600s ./e2e -run "^(TestLinuxWitnessIdentityGone\|TestLinuxWitnessProcessSnapshotPayloadDecisions)" -v` | 0 | `3400` PASS rows, `0` FAIL, `0` `DATA RACE`, `ok … 179.399s` |
| format | `gofmt -l .` (`<worktree>/daemon`) | 0 | 0 files listed |
| vet | `go vet ./...` (`<worktree>/daemon`) | 0 | 0 findings |
| suite | `go test -count=1 -timeout 900s ./...` (`<worktree>/daemon`) | 0 | 30 `ok`, 0 `FAIL`; pins + payload pin included |
| race, changed package | `go test -race -count=1 -timeout 600s ./e2e` (`<worktree>/daemon`) | 0 | `ok … 3.996s` |
| race, pins | `go test -race -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` | 0 | `ok … 2.129s`, `12` PASS rows, `red_rows=0`, 0 stray child processes |
| race, full module | `go test -race -count=1 -timeout 900s ./...` (implement-stage `<scratch>/head/daemon`) | 1 | per-binary budget expired in the untouched `internal/app` (panic after 15m0s while the running test had 42s); not re-run in the apply or fix rounds; see §8 |
| diff hygiene | `git diff --check` (worktree) | 0 | clean; `git diff --numstat 3b23dead` = `535 20` |
| mutants | §4 | 1 × 8 | every contract-listed row red; M6-M8 red exactly their deciding row |
| `daemon` CI job | CI-owned on the PR head (pins run in `go test -race … ./...`, `ci.yml:1096-1105`) | — | not runnable here (no push); local equivalents above |
| `integration` witness matrix | CI-owned on the PR head (`ci.yml:1659`) | — | not runnable here; the flake cannot be forced to red (plan D6), the pins are the mutant oracle |

## 8. Suite and race runs

Plain suite on the shipping bytes (fix-round final gate batch, `<scratch>/logs-fix/final-suite.log`):
`go test -count=1 -timeout 900s ./...` → `suite exit=0`; 30 `ok` lines and 0 `FAIL` lines
(`e2e` `1.089s`, `internal/app` `198.144s`, `internal/platform` `45.574s`). This is the local
equivalent of the `daemon` job's aggregate (that job runs the same tree under `-race`, which CI owns).

Race runs (`<scratch>/logs-fix/final-race-e2e.log`, `final-race-pins.log`; the full-module row below
is the implement-stage `final-race.log`, not re-run in the apply or fix rounds):

- changed package: `go test -race -count=1 -timeout 600s ./e2e` → exit 0, `ok … 3.996s`;
- pins: `go test -race -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` →
  exit 0, `ok … 2.129s`, `red_rows=0`; `pgrep -f e2e.test` = 0, so the re-exec children do not
  leak;
- full module: `go test -race -count=1 -timeout 900s ./...` → exit 1, `panic: test timed out after
  15m0s` in `internal/app` (900.262s) with `TestDurablePiProviderBridgeExecutorReservesCompletionCapacityBeforeDispatch`
  at 42s — the package was still progressing when the per-binary budget expired. `internal/app` is
  untouched by this diff and is green in the plain suite (see above); the race overhead is ~5-9x
  here (`protocol` 47.9s, `state` 94.9s, `workspace` 138.4s under race). Recorded as a
  local-host budget limit, not a red of the change; the blocking `daemon` job owns the full
  `-race ./...` verdict on the PR head.

## 9. Residual risks and known limits

1. The reap straddle cannot be forced to red on demand, so the mutant rows are the oracle (contract
   Gate "Witness oracle"); the `integration` witness matrix and the `daemon` job remain CI-owned — a
   locally green run is not those jobs' evidence.
2. The decision classifies the reap-window EACCES source only; a ptrace-denied **live** process
   still reds by design (the contract's fail-closed requirement, not a residual).
3. The test-side starttime parse (`linuxWitnessIdentityStartTimeDiffers`, via
   `parseLinuxNegativePIDReuseIdentity` since the apply pass) mirrors the `linux:v2:` marker's
   trailing field. If the product ever changes the marker, the pins build the canonical identity
   independently and cross-check it against `platform.ProcessIdentity`, so the pins red instead of
   silently weakening.
4. A non-empty identity that is not a parseable marker takes the fail-closed branch (plan D9): it
   cannot prove absence and cannot mask a live process.
5. The P1 record wraps the unexpected error (message suffix, `errors.Is` preserved). No call site
   matched the raw message text.
6. `retiredProcessIdentityError` (`core_test.go:1364`; `platform.ProcessIdentity` value pass at
   `:1359`) keeps its one-shot `os.ErrNotExist || syscall.Errno(87)` classification (not the
   contract's "ENOENT/ESRCH" wording); it is an assertion, not a wait (§6). No red of record exists
   for it; if that assertion ever reds, it opens its own goal rather than a silent widening here.
7. `linuxWitnessProcessSnapshotAt`'s error strings are unchanged for real inputs (`malformed /proc
   stat`, `short /proc stat`); the apply pass reuses the same `malformed /proc stat` text when the
   starttime field does not parse, so the evidence JSON written by the witnesses is unaffected.
8. Since the apply pass (PD-1), a structurally complete `/proc/<pid>/stat` whose starttime field
   does not parse returns the `malformed /proc stat` probe error, and a probe error takes the
   classified `probe failed:` path — never `nil` (this was the one latent fail-closed gap against
   contract F1). The branch is unreachable from kernel-generated stat files (always decimal), so it
   is documented as unreachable there; since the fix round the parse is the pure
   `parseLinuxWitnessProcessSnapshot` and the branch is pinned by the table-driven
   `TestLinuxWitnessProcessSnapshotPayloadDecisions` (`non_decimal_starttime_is_rejected`, with
   `short_payload_is_rejected` / `missing_closing_parenthesis_is_rejected` on the sibling guards;
   mutant rows M7/M8). The `denied_read_probe_failure_fails_closed` /
   `non_denial_error_records_probe_failure` pins exercise the decision layer's probe-error handling,
   not this parse path.
9. The P1 leg classifier matches test-authored copies of the product wrapper messages
   (`daemon/internal/platform/identity_linux.go:75/:113`); a product reword would degrade the record
   to `leg=unknown` with nothing red. Diagnostic only; the product files are outside the owned set
   (contract Q1(a)).

## 10. Fix stage (2026-11-25, review findings STD-A01–STD-A03)

Review receipt: `ce-code-review` `mode:agent`, focused path, `status: complete`, verdict "Ready with
fixes" (run `20261125-114841-`), range `a9e97747..919a0902`, scope `3` changed files, no peer (the
invocation was declared local-only; the adversarial lens was a separately dispatched read-only
in-process reviewer). All three findings were applied inside the owned set; none invalid. The fix
round re-ran the mutant oracle on the apply bytes and put every figure above on the fix bytes
(sha256 `00cdad06…`).

| finding | severity | file | change |
| --- | --- | --- | --- |
| STD-A01 | P2 | `daemon/e2e/production_linux_containment_witness_test.go` | the gone arm's ESRCH half had no oracle — a baseline mutant dropping `\|\| errors.Is(probeErr, syscall.ESRCH)` left K1 + all `9` K2 rows green on the apply bytes (`exit=0`, `red_rows=0`, `<scratch>/logs/prefix-esrch-drop-baseline.log`). K2 gained `probe_esrch_is_gone` (stub probe → `syscall.ESRCH`, expects `nil`), the oracle gained M6 (removing that clause reds exactly this row), and the row is in the load repeat |
| STD-A02 | P2 | `docs/plans/2026-11-25-rf-flake-containment-replay-nslink-plan.md` (§ D1) | the paragraph claimed `ProcessIdentity` reports EACCES as `ErrPersistedProcessGroupUnproven`; it now names the real mechanism, sourced from `daemon/internal/platform/identity_linux.go:75/:113` (`ProcessIdentity` returns the wrapped syscall error; the helper's denial classification is only `errors.Is(err, syscall.EACCES)`) and `:259` (the sentinel belongs to `ProvePersistedProcessGroupAbsent`) |
| STD-A03 | P3 | this record (§9.8) | the claim that the two probe-failure pins covered the non-decimal starttime parse branch overstated their reach (they inject a failing probe function and exercise the decision layer, not the parse). Taken at the stronger arm: the parse became the pure `parseLinuxWitnessProcessSnapshot`, pinned by the table-driven `TestLinuxWitnessProcessSnapshotPayloadDecisions`, with mutant rows M7/M8 |

### 10.1 The new pins and their oracle

- `probe_esrch_is_gone` (K2 row): injected stat-leg EACCES + a probe stub returning `syscall.ESRCH`
  → `nil` inside `1 s`. Before the fix, with the clause dropped, the baseline above shows the arm
  was unobserved; after the fix it is green in §3 and red under M6 only.
- `TestLinuxWitnessProcessSnapshotPayloadDecisions` (unit pin, `4` rows): green in §3 and in the
  suite; red under M7 (`non_decimal_starttime_is_rejected`) and M8 (`short_payload_is_rejected`).

### 10.2 Fix-round gates

Every row in §7/§8 is the fix-round re-run on the fix bytes: the worktree gates ran as one
foreground script (`scripts-fix/final-gates.sh`, `<scratch>/logs-fix/`), the mutants as §4, the
load repeat with `GOMAXPROCS=8 -race -count=200`, and the base census against the unchanged
`<scratch>/base` copy. The blocking CI rows stay CI-owned.

### 10.3 Hashes, run-head equality, citation shifts

- Delivered `daemon/e2e/production_linux_containment_witness_test.go`: sha256
  `00cdad06fd7d020dcef380f8267c7a2bb928452fb0bf61b4f0b4d8bb0324af76`; `git diff --numstat
  3b23dead` `535 20`, `git diff --numstat 919a0902` `64 0`.
- Run-head equality: the gate batch printed the file's sha256 before the runs and after them — both
  `00cdad06…` — and equals the delivered bytes; no code edit followed the batch. The mutant scratch
  copies were restored byte-identical after every run (M1-M8, `00cdad06…` after each).
- Citation shift: the parse split (`+4` lines) and the two pin insertions (`+12`, `+46`) move every
  witness-file line reference below `:3527`; plan D5's census cites were updated to the fix bytes
  (`3668`, `3653`, `4221/4270`, `3906`, `3963`), and the quoted logs keep the line numbers of the
  tree they ran on (the base copy in §2, the fix tree in §3).

### 10.4 Record hygiene

- Each changed record is clean under
  `python scripts/ci/model-routing-record-sanitize.py --check --root-only <record>` → exit 0; the
  committed `record-cleanliness` gate (`ci.yml` job `record-cleanliness`, which guards
  `docs/evidence/**`) passes against a synthetic commit carrying the fix bytes.
- Added-line scan of the diff against `origin/main` for the account name, drive-letter prefixes and
  the guest/temporary path prefixes (the `/mnt`, `/home`, `/tmp`, `/var` spellings), plus the
  profile-directory spelling filter: `0` hits; `git diff --check` clean; `0` added comment lines.
