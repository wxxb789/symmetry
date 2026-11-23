# rf-w4-12c process recorder port — evidence record (2026-11-22, implement stage)

Contract: `goal-rf-w4-12c-process-recorder-port.md` (GDD state root, external to the repository),
with the 2026-11-21 arch-decision-2 rulings (R1, R12, O1, O2, O3, O5), the verification amendments
(including the R2 supersession) and the 2026-11-22 comment policy binding over the earlier text.
Plan: `docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md`. Base revision:
`f04487736a30bccdc30ace6c40ecfd288fd45e05` (W4-15b/#243, W4-12a/#244, W4-12b/#257 merged). Branch:
`refactor/rf-w4-12c-process-recorder-port`. Every command ran in `daemon/` unless its row says
otherwise, on the Windows host with the Go toolchain of this checkout; the WSL row runs
Ubuntu-24.04 over the same worktree (`/mnt/q/repos/wt-w4-12c-process-recorder/symmetry/daemon`).
Nothing is committed.

## 1. Subject bytes (sha256, final tree)

| File | sha256 |
| --- | --- |
| `daemon/internal/execution/recorder.go` (new) | `adeec17a57b6e58c9f08662ede88497302ff5da6bc9a9fcbb24fcc67172b87ca` |
| `daemon/internal/execution/runner.go` | `42cf94a0ed2429c46bce4445bab8cd97eb38e98eda3ae082309a1089557b97d7` |
| `daemon/internal/execution/probe.go` | `cccf4cd590ce921f1a9a53a6cfc11762f0efeaac287b8c31bf1819dac111ba7f` |
| `daemon/internal/execution/process_launcher_linux.go` | `72c58a553a5c9c795199eea74400e2a40f198760630d17d595062822ddb5124d` |
| `daemon/internal/execution/process_launcher_windows.go` | `8985bdf01f8d639b0afe693a1bfc819f934407e02e802066132056c1528767c9` |
| `daemon/internal/execution/recorder_test.go` (new) | `f43a383be2b33c453f5838bc4428c6b2a90ac21619859ecb4dc47404260ec7ea` |
| `daemon/internal/harness/types.go` | `9765d28b2922acaa39d30d6172d042d2791306e36a4e69b234b4966cf3e35144` |
| `daemon/internal/harness/staged_driver.go` | `7400de152de00cbf449036a65a35e69e712404875564896eaa35d92705041fdd` |
| `daemon/internal/harness/frozen_members_test.go` (new) | `6c77cb711ffb5acbbec92ed911303a74198180cd19f96489a0870d5e5118dae0` |
| `daemon/internal/app/process_recorder.go` (new) | `22f848ffc478aaf2e65ee5d5307a85df9246adb6e96af067ff90631dc10a23b3` |
| `daemon/internal/app/process_recorder_test.go` (new) | `9d462f5af6c33b20b88f7281e21e212c31403862e26b8e6f1b62da26678857c4` |
| `daemon/internal/app/app.go` | `406ef06b9bfe720cee64e6a39333d76668a1d5a3a70607d458dbb6360129e67b` |
| `daemon/internal/app/goal_admission_tx.go` | `c69281c5879a550bdc39b375fa154a289e0bf13ce00786e5859b25f87b38904f` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `1d9d1b3253636e83dfa780c3d9e26f3e4f52a3a8073c076cbe2aaf1c39f89799` |
| `scripts/ci/omo-control-native-evidence.py` | `ba117e1236629ef233ed39fcf95c997671be3916a991a79eb5d067161eb881b1` |
| `docs/TODO.md` | `941c929d72f6e2fbe88b5c553683dd1e6cb337c316ef6031ac27a1ab43bc527c` |
| `docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md` | `15962b2b6c9bfecc14012a2ced43fa7c784b1d1d449a4f6545c46b95aac1ae34` |
| this record | the file you are reading (no self-referential hash) |

## 2. The port: one interface, ten writes, one capability query

Definition grep (paths, not bodies):

```
$ rg -n 'type Recorder |type RecorderCapabilities |type RecorderFuncs |func NewRecorder|func invocationWrites|func recorderWrites|func \(invocation \*Invocation\) MirrorRecorderMembers|func PopulateOpencodeRecorderMembers' daemon/internal/execution/recorder.go daemon/internal/harness/staged_driver.go
PENDING
```

`Recorder` carries one method per durable write the runner performed at base
(`PrepareSupervisorHandoff`, `BindSupervisorHandoff`, `CommitSupervisorHandoff`,
`RecordSupervisorHandoffStopReceipt`, `ClearSupervisorHandoff`, `PersistProcess`,
`PersistProcessWithAuthority`, `PersistProcessAuthority`, `PersistContainmentStopReceipt`,
`PersistContainmentUnproven`) plus `Capabilities()`. `RecorderFuncs` is the functional form (ten
optional functions); `NewRecorder` adapts it into the port through the unexported `recorderFuncs`,
whose absent writes fail closed (`errRecorderWriteUnavailable`).

Two rules tie the port to the base surface:

- `invocationWrites(invocation)` returns the invocation's recorder writes when it has a recorder,
  otherwise the ten frozen `Invocation` mirrors. OpenCode rebuilds an invocation from
  `request.Invocation` members (`daemon/internal/harness/opencode/adapter.go:343-362`) and cannot
  carry a recorder, so its launch reaches the same recorder through the mirrors; every other path
  carries the recorder itself.
- `Invocation.MirrorRecorderMembers` sets the mirrors from the recorder's method values, and
  `harness.PopulateOpencodeRecorderMembers` does the same for the three request-level members the
  frozen adapter reads; both are the single W6-04 deletion sites.

## 3. Callback-name gate (harness, excluding OpenCode and tests)

Verbatim contract command; note that ripgrep matches `--glob '!opencode/**'` against the paths as
printed (`daemon/internal/harness/...`), so the exclusion only bites with `!**/opencode/**`; both
outputs are pasted.

```
$ rg -n 'Persist(Process|ProcessAuthority|ContainmentStopReceipt|ProcessWithAuthority|ContainmentUnproven)|(Prepare|Bind|Commit|Clear)SupervisorHandoff|RecordSupervisorHandoffStopReceipt' daemon/internal/harness --glob '!opencode/**' --glob '!*_test.go'
daemon/internal/harness/opencode/adapter.go:351:		PrepareSupervisorHandoff:           request.Invocation.PrepareSupervisorHandoff,
daemon/internal/harness/opencode/adapter.go:352:		BindSupervisorHandoff:              request.Invocation.BindSupervisorHandoff,
daemon/internal/harness/opencode/adapter.go:353:		CommitSupervisorHandoff:            request.Invocation.CommitSupervisorHandoff,
daemon/internal/harness/opencode/adapter.go:354:		RecordSupervisorHandoffStopReceipt: request.Invocation.RecordSupervisorHandoffStopReceipt,
daemon/internal/harness/opencode/adapter.go:355:		ClearSupervisorHandoff:             request.Invocation.ClearSupervisorHandoff,
daemon/internal/harness/opencode/adapter.go:356:		PersistProcessWithAuthority:        request.Invocation.PersistProcessWithAuthority,
daemon/internal/harness/opencode/adapter.go:357:		PersistProcess:                     request.PersistProcess,
daemon/internal/harness/opencode/adapter.go:358:		PersistProcessAuthority:            request.PersistProcessAuthority,
daemon/internal/harness/opencode/adapter.go:359:		PersistContainmentStopReceipt:      request.PersistContainmentStopReceipt,
daemon/internal/harness/opencode/adapter.go:360:		PersistContainmentUnproven:         request.Invocation.PersistContainmentUnproven,
daemon/internal/harness/staged_driver.go:29:	invocation.PersistProcess = request.PersistProcess
daemon/internal/harness/staged_driver.go:30:	invocation.PersistProcessAuthority = request.PersistProcessAuthority
daemon/internal/harness/staged_driver.go:31:	invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
daemon/internal/harness/staged_driver.go:45:	request.PersistProcess = recorder.PersistProcess
daemon/internal/harness/staged_driver.go:46:	request.PersistProcessAuthority = recorder.PersistProcessAuthority
daemon/internal/harness/staged_driver.go:47:	request.PersistContainmentStopReceipt = recorder.PersistContainmentStopReceipt
daemon/internal/harness/types.go:335:	PersistProcess func(pid int, identity string) error
daemon/internal/harness/types.go:338:	PersistProcessAuthority       func(pid int, identity string, value *authority.Supervisor) error
daemon/internal/harness/types.go:339:	PersistContainmentStopReceipt func(pid int, identity string, receipt authority.StopReceipt) error

$ rg -n '<the same ten names>' daemon/internal/harness --glob '!**/opencode/**' --glob '!*_test.go'
daemon/internal/harness/staged_driver.go:29:	invocation.PersistProcess = request.PersistProcess
daemon/internal/harness/staged_driver.go:30:	invocation.PersistProcessAuthority = request.PersistProcessAuthority
daemon/internal/harness/staged_driver.go:31:	invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
daemon/internal/harness/staged_driver.go:45:	request.PersistProcess = recorder.PersistProcess
daemon/internal/harness/staged_driver.go:46:	request.PersistProcessAuthority = recorder.PersistProcessAuthority
daemon/internal/harness/staged_driver.go:47:	request.PersistContainmentStopReceipt = recorder.PersistContainmentStopReceipt
daemon/internal/harness/types.go:335:	PersistProcess func(pid int, identity string) error
daemon/internal/harness/types.go:338:	PersistProcessAuthority       func(pid int, identity string, value *authority.Supervisor) error
daemon/internal/harness/types.go:339:	PersistContainmentStopReceipt func(pid int, identity string, receipt authority.StopReceipt) error
```

Reading: the ten handoff/authority names appear in `daemon/internal/harness` production code only
inside `opencode/adapter.go` (frozen reader). The three request-level names appear only as the
`StartRequest` declarations (types.go) and at the two population sites: `PopulateOpencodeRecorderMembers`
(staged_driver.go:45-47) and `grant()`'s no-grant fold (staged_driver.go:29-31, see §5.3). No
conforming adapter names any durable callback:

```
$ rg -n '<the same ten names>' daemon/internal/harness/claude daemon/internal/harness/codex daemon/internal/harness/omo daemon/internal/harness/omp daemon/internal/harness/pi daemon/internal/harness/generic.go --glob '!*_test.go'
(no output, exit 1)
```

## 4. Per-adapter effective writes, base vs HEAD

Completeness check for clause 1 (no production reader of the ten members outside the port surface
and its two write sites; `opencode/**` excluded because it is the frozen reader):

```
$ rg -n 'invocation\.(Persist|PrepareSupervisorHandoff|BindSupervisorHandoff|CommitSupervisorHandoff|RecordSupervisorHandoffStopReceipt|ClearSupervisorHandoff)' daemon/internal --glob '!*_test.go' --glob '!**/opencode/**' | sed 's|daemon/internal/||'
execution/probe.go:37:	invocation.PersistProcess = nil
execution/probe.go:38:	invocation.PersistProcessAuthority = nil
execution/probe.go:39:	invocation.PersistContainmentStopReceipt = nil
execution/recorder.go:208:		PrepareSupervisorHandoff:           invocation.PrepareSupervisorHandoff,
execution/recorder.go:209:		BindSupervisorHandoff:              invocation.BindSupervisorHandoff,
execution/recorder.go:210:		CommitSupervisorHandoff:            invocation.CommitSupervisorHandoff,
execution/recorder.go:211:		RecordSupervisorHandoffStopReceipt: invocation.RecordSupervisorHandoffStopReceipt,
execution/recorder.go:212:		ClearSupervisorHandoff:             invocation.ClearSupervisorHandoff,
execution/recorder.go:213:		PersistProcess:                     invocation.PersistProcess,
execution/recorder.go:214:		PersistProcessWithAuthority:        invocation.PersistProcessWithAuthority,
execution/recorder.go:215:		PersistProcessAuthority:            invocation.PersistProcessAuthority,
execution/recorder.go:216:		PersistContainmentStopReceipt:      invocation.PersistContainmentStopReceipt,
execution/recorder.go:217:		PersistContainmentUnproven:         invocation.PersistContainmentUnproven,
execution/recorder.go:271:	invocation.PrepareSupervisorHandoff = recorder.PrepareSupervisorHandoff
execution/recorder.go:272:	invocation.BindSupervisorHandoff = recorder.BindSupervisorHandoff
execution/recorder.go:273:	invocation.CommitSupervisorHandoff = recorder.CommitSupervisorHandoff
execution/recorder.go:274:	invocation.RecordSupervisorHandoffStopReceipt = recorder.RecordSupervisorHandoffStopReceipt
execution/recorder.go:275:	invocation.ClearSupervisorHandoff = recorder.ClearSupervisorHandoff
execution/recorder.go:276:	invocation.PersistProcess = recorder.PersistProcess
execution/recorder.go:277:	invocation.PersistProcessWithAuthority = recorder.PersistProcessWithAuthority
execution/recorder.go:278:	invocation.PersistProcessAuthority = recorder.PersistProcessAuthority
execution/recorder.go:279:	invocation.PersistContainmentStopReceipt = recorder.PersistContainmentStopReceipt
execution/recorder.go:280:	invocation.PersistContainmentUnproven = recorder.PersistContainmentUnproven
harness/staged_driver.go:29:	invocation.PersistProcess = request.PersistProcess
harness/staged_driver.go:30:	invocation.PersistProcessAuthority = request.PersistProcessAuthority
harness/staged_driver.go:31:	invocation.PersistContainmentStopReceipt = request.PersistContainmentStopReceipt
```

| Adapter / path | Base write source | HEAD write source | Ordered writes + arguments |
| --- | --- | --- | --- |
| claude (`claude/adapter.go:167`) | grant invocation's ten members | `request.grant().merged(...)` (`staged_driver.go:102`) → the grant's `Recorder` | identical |
| codex (`codex/adapter.go:192`) | same | same | identical |
| pi (`pi/adapter.go:198`) | same | same | identical |
| omo (`omo/adapter.go:202`) | same | same | identical |
| omp (`omp/adapter.go:107`) | same | same | identical |
| generic (`generic.go:132-133`) | `request.grant().invocation`'s members | the same invocation's `Recorder` | identical |
| opencode (`opencode/adapter.go:343-362`, `:73`) | rebuilt invocation's ten members | rebuilt invocation (no recorder) → `invocationWrites` falls back to the same ten mirrors | identical |
| app direct (`app.go:1494-1509`) | ten inline callbacks | `daemon.processRecorder` (`process_recorder.go`) + mirrors | identical |
| goal admission (`goal_admission_tx.go:746-756`, `:900-919`) | seven invocation members + three grant members + three request members | one recorder in the invocation (`:754`) + mirrors + `PopulateOpencodeRecorderMembers` (`:918`) | identical |
| Linux v2 owner witness (`linux_owner_witness_linux.go:3109-3117`, `:3227-3232`) | seven members + three grant/request members each | one recorder (`:3113`) + mirrors (`:3117`) + populate (`:3232`) | identical |

The ordered writes themselves are unchanged because the runner's plumbing is unchanged: the same
store methods run in the same order with the same arguments, and the parity test
(`TestRecorderWritesLaunchExactlyLikeTheFrozenMembers`) pins the sequences (§7).

## 5. Nil-combination branches and the handoff truth table

### 5.1 Branches

| Base nil combination | Base branch | HEAD branch |
| --- | --- | --- |
| `PersistProcessWithAuthority` nil | legacy two-write path | `writes.PersistProcessWithAuthority == nil` → same |
| `PersistProcessWithAuthority` set + authority-capable containment | atomic write, two legacy writes skipped | same |
| `PersistProcess` nil | no marker write | same (`writes.PersistProcess == nil`) |
| `PersistProcessAuthority` nil | no authority write | same |
| `PersistContainmentStopReceipt` nil | no receipt write, `stopReceiptRequired=false` | same |
| `PersistContainmentUnproven` nil | no uncertainty observer installed | same |
| handoff members nil | `configureStartedHandoff` leaves the launcher's funcs | same (`writes.*`) |

The parity test's five shapes (marker+authority, atomic, atomic failure, marker without authority,
no durable write) each assert the exact write sequence for the frozen-member form and the
recorder form, plus the start error, the containment error and the containment cleanup counters.

### 5.2 Handoff truth table

`TestRecorderHandoffCapabilityMatchesPresenceGates` walks all 32 combinations of the five handoff
members and asserts `SupervisorHandoffRequested`/`SupervisorHandoffComplete` equal the base
any-of-five/prepare-and-bind-and-commit rules for the frozen members and for the recorder form:

```
$ go test -count=1 -v -run TestRecorderHandoffCapabilityMatchesPresenceGates ./internal/execution/
exit=0
mask 00000 requested=false complete=false
mask 00001 requested=true complete=false
mask 00010 requested=true complete=false
mask 00011 requested=true complete=false
mask 00100 requested=true complete=false
mask 00101 requested=true complete=false
mask 00110 requested=true complete=false
mask 00111 requested=true complete=true
mask 01000 requested=true complete=false
mask 01001 requested=true complete=false
mask 01010 requested=true complete=false
mask 01011 requested=true complete=false
mask 01100 requested=true complete=false
mask 01101 requested=true complete=false
mask 01110 requested=true complete=false
mask 01111 requested=true complete=true
mask 10000 requested=true complete=false
mask 10001 requested=true complete=false
mask 10010 requested=true complete=false
mask 10011 requested=true complete=false
mask 10100 requested=true complete=false
mask 10101 requested=true complete=false
mask 10110 requested=true complete=false
mask 10111 requested=true complete=true
mask 11000 requested=true complete=false
mask 11001 requested=true complete=false
mask 11010 requested=true complete=false
mask 11011 requested=true complete=false
mask 11100 requested=true complete=false
mask 11101 requested=true complete=false
mask 11110 requested=true complete=false
mask 11111 requested=true complete=true
```

### 5.3 The no-grant fold

`StartRequest.grant()` still folds `request.PersistProcess`/`PersistProcessAuthority`/
`PersistContainmentStopReceipt` onto the invocation's mirrors when no grant was passed. Production
always passes a grant (`goal_admission_tx.go:915`, `linux_owner_witness_linux.go:3230`), so the fold
only serves direct constructions (adapter tests, `staged_driver_test.go`), and it feeds exactly the
mirrors a recorder-less invocation already launches through — it is not a second path for a
conforming production adapter.

## 6. Port-path unit tests and app dispatch

`daemon/internal/app/process_recorder_test.go` drives one recorder through a real store wrapped in
`apptest.FaultStore` with one recording hook per write: the store dispatch sequence is asserted
(`SetProcessDetails`, `SetProcessDetailsWithAuthority` ×2, `RecordContainmentStopReceipt`,
`MarkContainmentUnproven`, `PrepareSupervisorHandoff`, `BindSupervisorHandoff`,
`RecordSupervisorHandoffStopReceipt`, `ClearSupervisorHandoff`, then the commit route's
`PrepareSupervisorHandoff`, `BindSupervisorHandoff`, `CommitSupervisorHandoff`) and the resulting
journal state is asserted after each write (marker, authority, receipt, unproven flag, bound
handoff, cleared handoff, promoted marker).

## 7. Red mutants

Five edits, each applied alone to the final tree, run against the named tests, then reverted
(`sha256sum -c` over the pre-mutant hashes of `recorder.go` and `runner.go` says `OK`).

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| m1 reorder two writes | `persistLegacyIdentity`: authority write before the marker write | new parity subtest `process_marker_and_legacy_authority` and pre-existing `TestAuthorityProviderWithoutAtomicCallbackPreservesLegacyCallbacks` | `write sequence = ["persist-process-authority(4242,fake:4242)","persist-process(4242,fake:4242)",...], want ["persist-process(...)","persist-process-authority(...)",...]`; `legacy persistence callbacks = [authority marker], want [marker authority]` |
| m2 drop `PersistContainmentUnproven` | `persist()` sets `started.persistContainmentUnproven = nil` | pre-existing `TestRunnerPersistsObservedContainmentUncertaintyAfterMarkerBeforeClose`, `TestStartPersistsMarkerBeforeCleaningRetainedInitialScanFailure` | `containment uncertainty callback was not installed before marker persistence` |
| m3 two-callback path while `PersistProcessWithAuthority` is supplied | `persist()` branch drops the write-presence check | new parity subtest `process_marker_and_legacy_authority` | `panic: runtime error: invalid memory address or nil pointer dereference` (nil atomic write reached) |
| m4a wrong-method populate (invocation) | `MirrorRecorderMembers`: `PersistProcessAuthority = recorder.PersistProcessWithAuthority` | new `TestMirrorRecorderMembersCallsTheSameRecorderMethods` | `frozen member authority reached ["atomic"], want the same recorder method` |
| m4b wrong-method populate (request) | `PopulateOpencodeRecorderMembers`: `PersistProcessAuthority = recorder.PersistProcessWithAuthority` | new `TestPopulateOpencodeRecorderMembersReachesTheSameRecorderMethod` | `frozen member PersistProcessAuthority reached ["PersistProcessWithAuthority"], want the same recorder method` |
| m5 drop `ClearSupervisorHandoff` from the handoff decision | `SupervisorHandoffRequested` loses the `Clear` term | new `TestRecorderHandoffCapabilityMatchesPresenceGates` | `mask 10000: recorder decided requested=false complete=false, want requested=true complete=false` |

m1, m2 and m3 are killed by tests that predate the slice as well, satisfying the R2 amendment.

## 8. Gates

Every row ran in the foreground on this host except the WSL and coverage rows, which ran alone (no
other suite on the host while either ran).

| Gate (working directory) | Exit | Output |
| --- | --- | --- |
| callback-name grep (contract command, `git root`) | 0 | §3 |
| callback-name grep with `!**/opencode/**` (`git root`) | 0 | §3 |
| conforming-adapter grep (`git root`) | 1 | no output: no durable callback named |
| `gofmt -l .` (`daemon/`) | 0 | no file listed |
| `git diff --check` (`git root`) | 0 | no whitespace error |
| `go vet ./...` (`daemon/`) | 0 | empty log (`Q:/tmp/gate-vet.log`) |
| `GOOS=linux go build ./...` (`daemon/`) | 0 | — |
| `GOOS=windows go build ./...` (`daemon/`) | 0 | — |
| `GOOS=darwin go build ./...` (`daemon/`) | 0 | — |
| `GOOS=linux GOARCH=arm64 go build ./...` (`daemon/`) | 0 | — |
| `go test -count=1 -timeout 1800s ./internal/execution/... ./internal/harness/... ./internal/app/...` (Windows, `daemon/`) | 0 | 12 packages `ok`: execution 13.091s, harness 3.606s, claude 16.702s, codex 18.275s, linejson 1.213s, omo 9.531s, omp 9.463s, opencode 0.779s, pi 9.721s, probeconformance 11.025s, app 371.886s, apptest 18.868s |
| `python3 scripts/ci/test-omo-control-native-evidence.py` (R9, `git root`) | 0 | `Ran 15 tests in 22.890s / OK` |
| `git diff --name-only` (scope, `git root`) | 0 | §9 |
| `wsl -d Ubuntu-24.04 -u accept` → `go test -count=1 -race -timeout 3000s ./internal/execution/... ./internal/harness/... ./internal/app/... ./internal/platform/...` (`/mnt/q/repos/wt-w4-12c-process-recorder/symmetry/daemon`) | 0 | 13 packages `ok`: execution 24.411s, harness 1.382s, claude 26.595s, codex 37.039s, linejson 1.095s, omo 15.042s, omp 40.733s, opencode 4.024s, pi 9.142s, probeconformance 8.187s, app 801.501s, apptest 3.629s, platform 61.889s; no race report |
| coverage pass (`go test -covermode=count -coverprofile=…` for `./internal/execution/`, `./internal/harness/`, `./internal/app/`, Windows) | 0 | execution 53.8% (base 53.0%), harness 71.1% (base 70.7%), app 75.3% (base 75.2%); per-file deltas below |

Coverage delta for every touched file (statements covered/total; the base side comes from a
throwaway worktree at `f0448773`, so the base bytes are the code the contract was authored against;
that worktree was removed afterwards):

| File | Base | HEAD |
| --- | --- | --- |
| `daemon/internal/execution/runner.go` | 629/1169 (53.8%) | 635/1167 (54.4%) |
| `daemon/internal/execution/probe.go` | 74/86 (86.0%) | 75/87 (86.2%) |
| `daemon/internal/execution/process_launcher_windows.go` | 16/102 (15.7%) | 18/104 (17.3%) |
| `daemon/internal/execution/recorder.go` (new) | no profile | 43/76 (56.6%) |
| `daemon/internal/harness/types.go` | 44/72 (61.1%) | 44/72 (61.1%) |
| `daemon/internal/harness/staged_driver.go` | 47/47 (100.0%) | 55/55 (100.0%) |
| `daemon/internal/app/app.go` | 829/1148 (72.2%) | 833/1149 (72.5%) |
| `daemon/internal/app/goal_admission_tx.go` | 582/816 (71.3%) | 580/813 (71.3%) |
| `daemon/internal/app/process_recorder.go` (new) | no profile | 2/2 (100.0%) |

`process_launcher_linux.go` and `linux_owner_witness_linux.go` are Linux-only files with no Windows
profile rows at base or HEAD (their behaviour is exercised by the WSL row above); the new test files
carry no profile rows by construction, and no touched file's coverage dropped.

## 9. Scope

```
$ git diff --name-only
daemon/internal/app/app.go
daemon/internal/app/goal_admission_tx.go
daemon/internal/app/linux_owner_witness_linux.go
daemon/internal/execution/probe.go
daemon/internal/execution/process_launcher_linux.go
daemon/internal/execution/process_launcher_windows.go
daemon/internal/execution/runner.go
daemon/internal/harness/staged_driver.go
daemon/internal/harness/types.go
docs/TODO.md
scripts/ci/omo-control-native-evidence.py

$ git status --porcelain -uall --no-renames
 M daemon/internal/app/app.go
 M daemon/internal/app/goal_admission_tx.go
 M daemon/internal/app/linux_owner_witness_linux.go
 M daemon/internal/execution/probe.go
 M daemon/internal/execution/process_launcher_linux.go
 M daemon/internal/execution/process_launcher_windows.go
 M daemon/internal/execution/runner.go
 M daemon/internal/harness/staged_driver.go
 M daemon/internal/harness/types.go
 M docs/TODO.md
 M scripts/ci/omo-control-native-evidence.py
?? daemon/internal/app/process_recorder.go
?? daemon/internal/app/process_recorder_test.go
?? daemon/internal/execution/recorder.go
?? daemon/internal/execution/recorder_test.go
?? daemon/internal/harness/frozen_members_test.go
?? docs/evidence/rf-w4-12c-process-recorder-port-2026-11-22.md
?? docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md
```

Line counts (`git diff --numstat` for the modified files, `wc -l` for the new ones): modified
`app.go` +15/−24, `goal_admission_tx.go` +17/−32, `linux_owner_witness_linux.go` +6/−20, `probe.go`
+1/−0, `process_launcher_linux.go` +8/−6, `process_launcher_windows.go` +10/−8, `runner.go`
+35/−62, `staged_driver.go` +19/−0, `types.go` +6/−10, `docs/TODO.md` +17/−16, the evidence script
+2/−0 (net −53 source lines over the ten modified files); new `execution/recorder.go` 281,
`app/process_recorder.go` 26 (production, +307), `execution/recorder_test.go` 390,
`harness/frozen_members_test.go` 112, `app/process_recorder_test.go` 209 (tests, +711),
`docs/plans/...` 201, this record.

No path is under `daemon/internal/harness/opencode/`; `git diff origin/main...HEAD | grep -i "users/"`
is empty; no committed byte contains a backslash followed by `u` (checked with
`rg -n '\\u'` over every changed and new file); the new files are LF (`file` reports `ASCII text`).

## 10. As-built deviations from the plan

- The port's functional form and the port itself are two types (`RecorderFuncs` fields +
  unexported `recorderFuncs` methods) because Go forbids a field and a method with the same name;
  plan §9 records it.
- The harness populate test file is `frozen_members_test.go` (not `opencode_members_test.go`) so the
  `opencode/**` scope check cannot be confused by a file name.
- R9 adds `daemon/internal/app/goal_admission_tx.go` to `SOURCE_PATHS` besides the new
  `process_recorder.go`: the W4-01 split had left the file out while its facade was listed.
- The Windows launcher evaluates the handoff capability once instead of twice: the invocation cannot
  change between the two base checks, and the reused capability set is the same value (the Linux
  launcher's condition order — query first, `DurableSupervisorHandoffAvailable` second — is
  unchanged).

## 11. Residual risks and known limits

- The recorder-less fallback stays for the frozen adapter; it is the only way an OpenCode launch can
  reach its durable writes without editing `opencode/**` (O2). W6-04 deletes it with the members.
- `Recorder` writes on a non-`RecorderFuncs` implementations are taken as method values once per
  launch (`recorderWrites`); the app and both construction paths use `NewRecorder`, which avoids it.
- The base coverage profile was taken from a throwaway worktree at `f0448773` (removed after the
  measurement); no repository file depends on it.
