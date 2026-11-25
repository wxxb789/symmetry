# rf-flake-containment-replay-nslink — evidence record

Goal: classify the procfs reap-race `EACCES` in the containment identity-gone wait.
Contract: `goal-rf-flake-containment-replay-nslink.md` (GDD state root, external to the
repository). Plan: `docs/plans/2026-11-25-rf-flake-containment-replay-nslink-plan.md`.
Base revision: `3b23dead` (dispatch HEAD). Subject: `daemon/e2e/production_linux_containment_witness_test.go`,
sha256 `8db3a21e6993d34a7889b994de1b082229e20a43d73c182cecd266f468cdbed7`
(`git diff --numstat 3b23dead` `471 20`, of which the 2026-11-25 apply pass is `103 60` on top of
`f5e9ea7d`; `git diff --check` clean).

Environment: WSL `Ubuntu-24.04`, user `accept` (uid 1000, non-root, 16 CPUs), Go `go1.27.0
linux/amd64`, kernel `6.18.58.1-microsoft-standard-WSL2`. Scratch root `<scratch>` (`base/` = base
tree + K1 only, `head/` = head tree + pins, `logs/`, `scripts/`); worktree `<worktree>`. Every run
is foreground and sequential.

## 1. The classification rule (contract clause 5)

What changed (one test file): the F1 decision in the shared wait; the P1 probe record on the
unexpected-error return; the F2 `waitForLinuxWitnessIdentityGoneWith` seam with a one-line wrapper;
the single `/proc/<pid>/stat` parse shared by the probe and `linuxWitnessProcessSnapshotAt`; pins K1
and K2 with the re-exec child mode. No production file changed. The 2026-11-25 apply pass then
simplified the helper and pins, closed the probe-parse gap (plan D10) and added two probe-failure
rows plus a read counter; the decision table below is unchanged.

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

Command (literal, post-apply worktree; the same command ran in `<scratch>/apply/daemon`):

```
cd <worktree>/daemon && go test -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v
```

`exit=0`, `red_rows=0` (`<scratch>/logs/apply-pins.log`):

```
--- SKIP: TestLinuxWitnessIdentityGoneDeniedChildProcess (0.00s)
=== RUN   TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed
    production_linux_containment_witness_test.go:3973: unreadable live process 124460 classified after 0s: process 124460 identity "linux:v2:3a843c9a-0118-4f5f-95b7-0d7a3d8b805c:4026532213:124460:8781707" is unreadable after a denied identity read: /proc/124460/stat shows state=S starttime=8781707
--- PASS: TestLinuxWitnessIdentityGoneUnreadableLiveProcessFailsClosed (0.02s)
=== RUN   TestLinuxWitnessIdentityGoneDeniedReadDecisions
    production_linux_containment_witness_test.go:4066: identity read failure record: read process 124467 start time: input/output error (identity read failure: leg=read process start time errno=5(input/output error) poll=1 elapsed=0s; /proc/124467/stat probe: exists=true state=S starttime=8781709 err=<nil>)
    --- PASS: …/reaped_pid_probe_missing_is_gone (0.02s)
    --- PASS: …/zombie_state_is_gone (0.03s)
    --- PASS: …/doctored_starttime_is_gone (0.00s)
    --- PASS: …/live_pid_real_identity_stat_namespace_link_fails_closed (0.00s)
    --- PASS: …/live_pid_real_identity_readlink_fails_closed (0.00s)
    --- PASS: …/non_denial_error_carries_the_probe_record (0.00s)
    --- PASS: …/denied_read_probe_failure_fails_closed (0.00s)
    --- PASS: …/non_denial_error_records_probe_failure (0.00s)
    --- PASS: …/empty_identity_keeps_polling (0.53s)
ok  	github.com/wxxb789/symmetry/daemon/e2e	0.647s
```

`wsl -d Ubuntu-24.04 -u accept -- bash <scratch>/scripts/apply-gates.sh` → `pins exit=0 red=0
subtests=9`.

### 3a. Implement-stage observation: one over-specified pin assertion, fixed before sealing

The first full-suite run on the pre-fix pin bytes (`<scratch>/logs/head-suite.log`, subject hash
`8c87ba74…`) reddened `non_denial_error_carries_the_probe_record` under package-parallel load: the
row pinned the child's `/proc/<pid>/stat` *state letter* captured at row entry (`state=S`) while the
probe inside the P1 record read the child mid-wake (`state=R`). The child is a blocked Go test
process; its transient scheduler state is not part of the contract. The row now pins the record's
field presence and the stable values (`exists=true state=` … ` starttime=<N> err=<nil>`), exactly as
the classified-error rows already did. All runs in §3, §4, §7 and §8 are on the fixed bytes
(implement-stage sha256 `55fd6949…`; the apply pass then rewrote the helper and pins, and
§3/§4/§7/§8 below were re-run on the apply bytes `8db3a21e…`); the failing run is kept in the log
as the reproduction.

## 4. Mutants (scratch copy; rows name the CI job that must redden: `daemon`)

Each mutation is applied to `<scratch>/apply/daemon/e2e/production_linux_containment_witness_test.go`,
the pins run (`apply-mutant.sh <id>`), then the pristine blob is restored from
`production_linux_containment_witness_test.go.pristine`; the sha256 after every restore equals the
pristine value `8db3a21e6993d34a7889b994de1b082229e20a43d73c182cecd266f468cdbed7`.

| id | mutation (diff vs pristine) | command | exit | observed reds |
| --- | --- | ---: | --- | --- |
| M1 | delete the EACCES decision (+ its 2 comment lines); EACCES falls into the unexpected-error return | `apply-mutant.sh M1` | 1 | 9 rows: K1, K2, `reaped_pid_probe_missing_is_gone`, `zombie_state_is_gone`, `doctored_starttime_is_gone`, both real-identity rows, `denied_read_probe_failure_fails_closed`, `empty_identity_keeps_polling` |
| M2 | `return nil` on EACCES, no probe | `apply-mutant.sh M2` | 1 | 6 rows: K1, K2, both real-identity rows, `denied_read_probe_failure_fails_closed`, `empty_identity_keeps_polling` |
| M3 | decision condition `err != nil && strings.Contains(err.Error(), "stat namespace link")` | `apply-mutant.sh M3` | 1 | 4 rows: K1, K2, `live_pid_real_identity_readlink_fails_closed`, `doctored_starttime_is_gone` |
| M4 | decision case negated: starttime **equal** → `nil` (`!linuxWitnessIdentityStartTimeDiffers(...)`) | `apply-mutant.sh M4` | 1 | 5 rows: K1, K2, both real-identity rows, `doctored_starttime_is_gone` |
| M5 | `identity == ""` + readable live + EACCES → `return nil` | `apply-mutant.sh M5` | 1 | 2 rows: K2, `empty_identity_keeps_polling` |

Every contract-listed row reddens; the extra reds are the same mutant truncating other branches
(the contract's cells name the deciding rows, not an upper bound). Logs:
`<scratch>/logs/apply-mutant-{M1,M2,M3,M4,M5}.log`. The `non_denial_error_carries_the_probe_record` row
stays green under M1-M5 by construction (it injects EIO, not EACCES).

Mutation patches, verbatim (against the pristine blob
`sha256 8db3a21e6993d34a7889b994de1b082229e20a43d73c182cecd266f468cdbed7`; M1 removes the block,
M2 replaces it with the two-line `return nil` form, M3-M5 are the single-line edits shown):

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
`daemon/internal`; plus the two value passes `witness_test:3664`, `core_test:1359`) were classified
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
| pins | `<worktree>/daemon` (post-apply): same command | 0 | §3 |
| format | `gofmt -l .` (`<worktree>/daemon`) | 0 | 0 files listed |
| vet | `go vet ./...` (`<worktree>/daemon`) | 0 | 0 findings |
| suite | `go test -count=1 -timeout 900s ./...` (`<worktree>/daemon`) | 0 | 30 `ok`, 0 `FAIL`; pins included |
| race, changed package | `go test -race -count=1 -timeout 600s ./e2e` (`<worktree>/daemon`) | 0 | `ok … 1.944s` |
| race, pins | `go test -race -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` | 0 | `ok … 1.784s`, K1 `0.05s`, K2 `0.68s`, 0 red rows, 0 stray child processes |
| race, full module | `go test -race -count=1 -timeout 900s ./...` (implement-stage `<scratch>/head/daemon`) | 1 | per-binary budget expired in the untouched `internal/app` (panic after 15m0s while the running test had 42s); not re-run in the apply pass; see §8 |
| diff hygiene | `git diff --check` (worktree) | 0 | clean; `git diff --numstat 3b23dead` = `471 20` |
| mutants | §4 | 1 × 5 | every contract-listed row red |
| `daemon` CI job | CI-owned on the PR head (pins run in `go test -race … ./...`, `ci.yml:1096-1105`) | — | not runnable here (no push); local equivalents above |
| `integration` witness matrix | CI-owned on the PR head (`ci.yml:1659`) | — | not runnable here; the flake cannot be forced to red (plan D6), the pins are the mutant oracle |

## 8. Suite and race runs

Plain suite on the shipping bytes (`scripts/apply-gates.sh`, `<scratch>/logs/apply-suite.log`):
`go test -count=1 -timeout 900s ./...` → `suite exit=0`; 30 `ok` lines and 0 `FAIL` lines
(`e2e` `1.628s`, `internal/app` `241.007s`, `internal/platform` `74.034s`). This is the local
equivalent of the `daemon` job's aggregate (that job runs the same tree under `-race`, which CI owns).

Race runs (`<scratch>/logs/apply-race-e2e.log`, `apply-race-pins.log`; the full-module row below is
the implement-stage `final-race.log`, not re-run in the apply pass):

- changed package: `go test -race -count=1 -timeout 600s ./e2e` → exit 0, `ok … 1.944s`;
- pins: `go test -race -count=1 -timeout 300s ./e2e -run "^TestLinuxWitnessIdentityGone" -v` →
  exit 0, `ok … 1.784s`, K1 `0.05s`, K2 `0.68s`, `red_rows=0`; `pgrep -f e2e.test` = 0, so the
  re-exec children do not leak;
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
   is covered by inspection plus the `denied_read_probe_failure_fails_closed` /
   `non_denial_error_records_probe_failure` pins, not by a real-input pin.
9. The P1 leg classifier matches test-authored copies of the product wrapper messages
   (`daemon/internal/platform/identity_linux.go:75/:113`); a product reword would degrade the record
   to `leg=unknown` with nothing red. Diagnostic only; the product files are outside the owned set
   (contract Q1(a)).
