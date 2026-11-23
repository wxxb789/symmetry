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
| `daemon/internal/execution/recorder.go` (new) | `ee54071db3eecee2c4af534d2d8594340876bddfd230de5007802ee0815cf973` |
| `daemon/internal/execution/runner.go` | `eb848fc8a0245460510c009419762c25fda6ab6f2240214ce0fbe5cbec67f667` |
| `daemon/internal/execution/probe.go` | `72f520aaaeda0bb7f1df8b8aeb0ed05651546d51cc96dde3aaa9d39ae8ef617c` |
| `daemon/internal/execution/probe_test.go` | `6e63515653a03893394f98b01e634929d3ea4f35e61a7bcae0b6073c71c6d334` |
| `daemon/internal/execution/process_launcher_linux.go` | `72c58a553a5c9c795199eea74400e2a40f198760630d17d595062822ddb5124d` |
| `daemon/internal/execution/process_launcher_windows.go` | `8985bdf01f8d639b0afe693a1bfc819f934407e02e802066132056c1528767c9` |
| `daemon/internal/execution/recorder_test.go` (new) | `b339162b49aa60ec11a01d75c4106b3cd7ecc880e4349037cf153682207a0ff0` |
| `daemon/internal/harness/types.go` | `9765d28b2922acaa39d30d6172d042d2791306e36a4e69b234b4966cf3e35144` |
| `daemon/internal/harness/staged_driver.go` | `7400de152de00cbf449036a65a35e69e712404875564896eaa35d92705041fdd` |
| `daemon/internal/harness/frozen_members_test.go` (new) | `6c77cb711ffb5acbbec92ed911303a74198180cd19f96489a0870d5e5118dae0` |
| `daemon/internal/app/process_recorder.go` (new) | `e79090c8701d9cc26daaf6c04834cb03ed5462ae15863952c8181baa7dedf739` |
| `daemon/internal/app/process_recorder_test.go` (new) | `9d462f5af6c33b20b88f7281e21e212c31403862e26b8e6f1b62da26678857c4` |
| `daemon/internal/app/app.go` | `406ef06b9bfe720cee64e6a39333d76668a1d5a3a70607d458dbb6360129e67b` |
| `daemon/internal/app/goal_admission_tx.go` | `54a9c0bca1c5c0af7dc17a763b543773875a0c25b72bca3442e6afcd349a9576` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `3383fe973e1a41b15ea699eb7fa153b7b84ede16b5e7d814a2564ff9ae021442` |
| `scripts/ci/omo-control-native-evidence.py` | `ba117e1236629ef233ed39fcf95c997671be3916a991a79eb5d067161eb881b1` |
| `docs/TODO.md` | `6c3712ff840a6a51510dd6f6a63e2e5879bff3892987e50acae9c6fe7616d1d5` |
| `docs/plans/2026-11-22-rf-w4-12c-process-recorder-port-plan.md` | `15962b2b6c9bfecc14012a2ced43fa7c784b1d1d449a4f6545c46b95aac1ae34` |
| this record | the file you are reading (no self-referential hash) |

## 2. The port: one interface, ten writes, one capability query

Definition grep (paths, not bodies):

```
$ rg -n 'type Recorder |type RecorderCapabilities |type RecorderFuncs |func NewRecorder|func invocationWrites|func recorderWrites|func \(invocation \*Invocation\) MirrorRecorderMembers|func PopulateOpencodeRecorderMembers' daemon/internal/execution/recorder.go daemon/internal/harness/staged_driver.go
daemon/internal/execution/recorder.go:13:type RecorderCapabilities struct {
daemon/internal/execution/recorder.go:47:type Recorder interface {
daemon/internal/execution/recorder.go:87:type RecorderFuncs struct {
daemon/internal/execution/recorder.go:117:func NewRecorder(writes RecorderFuncs) Recorder {
daemon/internal/execution/recorder.go:198:func invocationWrites(invocation Invocation) RecorderFuncs {
daemon/internal/execution/recorder.go:219:func recorderWrites(recorder Recorder) RecorderFuncs {
daemon/internal/execution/recorder.go:262:func (invocation *Invocation) MirrorRecorderMembers() {
daemon/internal/harness/staged_driver.go:39:func PopulateOpencodeRecorderMembers(request StartRequest) StartRequest {
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
execution/recorder.go:203:		PrepareSupervisorHandoff:           invocation.PrepareSupervisorHandoff,
execution/recorder.go:204:		BindSupervisorHandoff:              invocation.BindSupervisorHandoff,
execution/recorder.go:205:		CommitSupervisorHandoff:            invocation.CommitSupervisorHandoff,
execution/recorder.go:206:		RecordSupervisorHandoffStopReceipt: invocation.RecordSupervisorHandoffStopReceipt,
execution/recorder.go:207:		ClearSupervisorHandoff:             invocation.ClearSupervisorHandoff,
execution/recorder.go:208:		PersistProcess:                     invocation.PersistProcess,
execution/recorder.go:209:		PersistProcessWithAuthority:        invocation.PersistProcessWithAuthority,
execution/recorder.go:210:		PersistProcessAuthority:            invocation.PersistProcessAuthority,
execution/recorder.go:211:		PersistContainmentStopReceipt:      invocation.PersistContainmentStopReceipt,
execution/recorder.go:212:		PersistContainmentUnproven:         invocation.PersistContainmentUnproven,
execution/recorder.go:267:	invocation.PrepareSupervisorHandoff = writes.PrepareSupervisorHandoff
execution/recorder.go:268:	invocation.BindSupervisorHandoff = writes.BindSupervisorHandoff
execution/recorder.go:269:	invocation.CommitSupervisorHandoff = writes.CommitSupervisorHandoff
execution/recorder.go:270:	invocation.RecordSupervisorHandoffStopReceipt = writes.RecordSupervisorHandoffStopReceipt
execution/recorder.go:271:	invocation.ClearSupervisorHandoff = writes.ClearSupervisorHandoff
execution/recorder.go:272:	invocation.PersistProcess = writes.PersistProcess
execution/recorder.go:273:	invocation.PersistProcessWithAuthority = writes.PersistProcessWithAuthority
execution/recorder.go:274:	invocation.PersistProcessAuthority = writes.PersistProcessAuthority
execution/recorder.go:275:	invocation.PersistContainmentStopReceipt = writes.PersistContainmentStopReceipt
execution/recorder.go:276:	invocation.PersistContainmentUnproven = writes.PersistContainmentUnproven
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

### 7.1 Apply-stage re-checks (2026-11-23)

The apply stage changed `recorder.go`, `probe.go`, `runner.go` (one comment hunk) and the two
execution test files, so every mutant whose edit text or killer sits on those bytes was re-run
alone on the applied tree and then reverted (`sha256sum` of `recorder.go` and `probe.go` restored
to the §1 values). m1–m3 mutate statements in `persist()`/`persistLegacyIdentity()` and m5 mutates
`SupervisorHandoffRequested`; those functions are byte-identical to the implement-stage tree, and
m4b's subject `PopulateOpencodeRecorderMembers` was not touched by apply — those rows still bind
the same bytes.

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| mA1a recorder left attached | `ephemeralInvocation` drops `invocation.Recorder = noDurableWrites` | `TestRunBoundedCommandDoesNotInvokeRecorderWrites`, `TestEphemeralInvocationRemovesEveryDurableWrite` | `RunBoundedCommand() invoked 1 durable writes`; `ephemeral strip left durable writes: {Prepare…Commit:true, PersistProcess:true, PersistProcessWithAuthority:true, PersistContainmentStopReceipt:true, PersistContainmentUnproven:true}` |
| mA1b pre-D2 strip | `ephemeralInvocation` restores `Recorder = nil` plus the three legacy clears | `TestEphemeralInvocationRemovesEveryDurableWrite` (the recorder-run test passes, since the recorder itself is nil) | `ephemeral strip left durable writes: {Prepare:true Bind:true Commit:true RecordSupervisorHandoffStopReceipt:true Clear:true PersistProcessWithAuthority:true PersistContainmentUnproven:true}` (7 writes alive) |
| mA2 pre-D1 mirror | `MirrorRecorderMembers` restores the unconditional method-value body | `TestMirrorRecorderMembersKeepsAbsentWritesNil` (m4a's killer still passes) | `an absent write was mirrored as present` |
| mA3 wrong-method fallback | `recorderWrites`: `writes.PersistProcessAuthority = recorder.PersistProcessWithAuthority` | `TestRecorderWritesMapsEveryCapabilityToItsMethod` | `fallback write authority reached []string{"atomic"}, want its own method` |
| m4a re-expressed | `MirrorRecorderMembers`: `invocation.PersistProcessAuthority = writes.PersistProcessWithAuthority` | `TestMirrorRecorderMembersCallsTheSameRecorderMethods` | `frozen member authority reached []string{"atomic"}, want the same recorder method` |

## 8. Gates

Every row ran in the foreground on this host except the WSL and coverage rows, which ran alone (no
other suite on the host while either ran). The apply stage (2026-11-23, §12) re-ran every row
against the applied tree; the numbers below are the apply-stage observations (the three
callback-name greps are byte-identical, §3).

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
| `go test -count=1 -timeout 1800s ./internal/execution/... ./internal/harness/... ./internal/app/...` (Windows, `daemon/`) | 0 | 12 packages `ok`: execution 20.556s, harness 1.861s, claude 20.228s, codex 22.042s, linejson 1.271s, omo 12.609s, omp 12.535s, opencode 1.522s, pi 12.250s, probeconformance 9.962s, app 386.786s, apptest 11.118s |
| `python3 scripts/ci/test-omo-control-native-evidence.py` (R9, `git root`) | 0 | `Ran 15 tests in 25.490s / OK` |
| `git diff --name-only` (scope, `git root`) | 0 | §9 |
| `wsl -d Ubuntu-24.04 -u accept` → `go test -count=1 -race -timeout 3000s ./internal/execution/... ./internal/harness/... ./internal/app/... ./internal/platform/...` (`/mnt/q/repos/wt-w4-12c-process-recorder/symmetry/daemon`) | 0 | 13 packages `ok`: execution 26.470s, harness 1.163s, claude 27.453s, codex 39.375s, linejson 1.172s, omo 17.161s, omp 42.540s, opencode 5.676s, pi 12.642s, probeconformance 8.280s, app 741.095s, apptest 3.900s, platform 59.133s; no race report |
| coverage pass (`go test -covermode=count -coverprofile=…` for `./internal/execution/`, `./internal/harness/`, `./internal/app/`, Windows) | 0 | execution 54.1% (base 53.0%), harness 71.1% (base 70.7%), app 75.2% (base 75.2%); per-file deltas below |

Coverage delta for every touched file (statements covered/total; the base side comes from a
throwaway worktree at `f0448773`, so the base bytes are the code the contract was authored against;
that worktree was removed afterwards):

| File | Base | Implement stage | Applied (2026-11-23) |
| --- | --- | --- | --- |
| `daemon/internal/execution/runner.go` | 629/1169 (53.8%) | 635/1167 (54.4%) | 636/1167 (54.5%) |
| `daemon/internal/execution/probe.go` | 74/86 (86.0%) | 75/87 (86.2%) | 74/86 (86.0%) |
| `daemon/internal/execution/process_launcher_windows.go` | 16/102 (15.7%) | 18/104 (17.3%) | 18/104 (17.3%) |
| `daemon/internal/execution/recorder.go` (new) | no profile | 43/76 (56.6%) | 46/75 (61.3%) |
| `daemon/internal/harness/types.go` | 44/72 (61.1%) | 44/72 (61.1%) | 44/72 (61.1%) |
| `daemon/internal/harness/staged_driver.go` | 47/47 (100.0%) | 55/55 (100.0%) | 55/55 (100.0%) |
| `daemon/internal/app/app.go` | 829/1148 (72.2%) | 833/1149 (72.5%) | 831/1149 (72.3%) |
| `daemon/internal/app/goal_admission_tx.go` | 582/816 (71.3%) | 580/813 (71.3%) | 579/812 (71.3%) |
| `daemon/internal/app/process_recorder.go` (new) | no profile | 2/2 (100.0%) | 2/2 (100.0%) |

Apply-stage reading: `recorder.go`'s statement count drops by one (the deleted `recorderFuncs.Capabilities`
forwarder) and its covered count rises by three — the new apply tests cover the whole
`recorderWrites` fallback path (`go tool cover -func`: `recorderWrites` 8.0% at implement stage →
100.0% applied). `probe.go` loses one statement and one covered statement: the D2 strip rewrite
replaces eight assignments with the `ephemeralInvocation` call (`100.0%`) plus the helper's five
assignments and return. `app.go`'s bytes are unchanged from the implement-stage row (same §1
sha256); its 833→831 covered difference is run-to-run variation in timing-dependent paths, not a
code change.

`process_launcher_linux.go` and `linux_owner_witness_linux.go` are Linux-only files with no Windows
profile rows at base or HEAD (their behaviour is exercised by the WSL row above); the new test files
carry no profile rows by construction, and the applied-stage movements are the statement-shape
changes above, not uncovered additions.

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

Apply stage (2026-11-23, working tree against the committed slice; §12): the same owned set stays,
plus `daemon/internal/execution/probe_test.go` (the F6 test lives in the existing probe test file).

```
$ git diff --name-only
daemon/internal/app/goal_admission_tx.go
daemon/internal/app/linux_owner_witness_linux.go
daemon/internal/app/process_recorder.go
daemon/internal/execution/probe.go
daemon/internal/execution/probe_test.go
daemon/internal/execution/recorder.go
daemon/internal/execution/recorder_test.go
daemon/internal/execution/runner.go

$ git diff --numstat
0	1	daemon/internal/app/goal_admission_tx.go
0	1	daemon/internal/app/linux_owner_witness_linux.go
2	3	daemon/internal/app/process_recorder.go
12	8	daemon/internal/execution/probe.go
54	0	daemon/internal/execution/probe_test.go
15	19	daemon/internal/execution/recorder.go
145	0	daemon/internal/execution/recorder_test.go
3	3	daemon/internal/execution/runner.go
```

Post-apply line counts: `execution/recorder.go` 277, `execution/recorder_test.go` 535,
`execution/probe.go` 183, `execution/probe_test.go` 260, `app/process_recorder.go` 25,
`app/goal_admission_tx.go` 1454, `app/linux_owner_witness_linux.go` 3896, `execution/runner.go`
2672.

No path is under `daemon/internal/harness/opencode/`; `git diff origin/main...HEAD | grep -i "users/"`
matches only this record's own quoted command; no committed byte contains a backslash followed by `u` (checked with
`rg -n '\\u'` over every changed and new file; the only match is this record's own quoted command);
the new files are LF (`file` reports `ASCII text`).

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

## 12. Apply-stage amendments (2026-11-23)

The accepted findings of the simplify stage
(`state/gdd/runs/2026-11-23/symmetry-rf-w4-12c-process-recorder-port-simplif/answer.md`) were
applied to the merged slice (`51178508`, uncommitted working tree). Every finding was accepted;
only F8's option (b) was not applied, because the finding itself records it as conflicting with the
plan §8 decision (no method value per write) and requiring conductor sign-off — option (a), the
fallback-mapping test, is in.

- F1: the `Invocation` mirror comment names every reader (opencode, grant-less direct constructions,
  test doubles replacing the launch seam); same 4 lines, no code change.
- F2/F3: the `recorderFuncs.Capabilities` forwarder and the duplicated method comment are deleted;
  the promoted `RecorderFuncs.Capabilities` keeps `recorderFuncs` satisfying `Recorder`
  (`NewRecorder(writes).Capabilities()` in `recorder_test.go` still pins the value).
- F4: `processRecorder`'s doc comment is three lines.
- F5: the duplicate `MirrorRecorderMembers()` calls in `goal_admission_tx.go` and the Linux-only
  witness are deleted; between them and `PopulateOpencodeRecorderMembers` no reader exists, and the
  populate mirrors the request copy the adapters read.
- F6: `probe_test.go` adds `TestRunBoundedCommandDoesNotInvokeRecorderWrites` (an attached recorder
  must stay at zero calls) and `TestEphemeralInvocationRemovesEveryDurableWrite` (a fully mirrored
  invocation strips to zero capabilities).
- F7/D1: `MirrorRecorderMembers` mirrors `recorderWrites(recorder)`, so a false capability stays
  nil like the legacy absent callback and `recorderFuncs` mirrors allocate no method value;
  `TestMirrorRecorderMembersKeepsAbsentWritesNil` pins it.
- F8(a): `TestRecorderWritesMapsEveryCapabilityToItsMethod` pins the `recorderWrites` fallback
  mapping (all ten present → each field calls its own method; sparse capabilities → only the true
  ones are mapped).
- D2: `RunBoundedCommand` strips through `ephemeralInvocation` and the package-level
  `noDurableWrites = NewRecorder(RecorderFuncs{})`; the recorder is replaced, not merely nil'd, so a
  mirrored invocation can no longer keep a live write. The three legacy member clears are gone; the
  §4 grep shows `probe.go` no longer appears there.
- D3: the §2 grep output replaced the `PENDING` placeholder.

Refreshed at their single source by this stage: §1 hashes, §2 grep, §4 grep, §7.1 mutants, §8 gate
rows and coverage table, §9 scope and line counts. Not re-run: the base-side coverage worktree
(removed at implement stage; the base column is unchanged by construction). The advisory
`golangci-lint run ./internal/execution/... ./internal/harness/...` stays at the recorded 105
issues.
