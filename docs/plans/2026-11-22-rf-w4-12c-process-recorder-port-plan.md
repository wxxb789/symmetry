# rf-w4-12c process recorder port — plan

Contract: `goal-rf-w4-12c-process-recorder-port.md` (GDD state root, external to the repository),
with the 2026-11-21 arch-decision-2 rulings (R1, R12, O1, O2, O3, O5), the verification amendments
and the 2026-11-22 comment policy binding over the earlier text. Base revision:
`f04487736a30bccdc30ace6c40ecfd288fd45e05` (W4-15b/#243, W4-12b/#257 and W4-12a/#244 merged).
Branch: `refactor/rf-w4-12c-process-recorder-port`. Owned files are the contract's strict set plus
this plan, `docs/evidence/rf-w4-12c-process-recorder-port-2026-11-22.md` and
`docs/TODO.md`/`docs/archive/CLOSED-TODO.md`.

Goal shape: one `execution.Recorder` port per launch replaces the runner's direct reads of the ten
durable `Invocation` callbacks and the duplicate `StartRequest` Persist* triple; the app builds one
recorder per site from the existing `persist*Callback` factories; every conforming adapter keeps its
W4-15b grant path and names no durable callback; the frozen OpenCode adapter keeps reading the same
members, populated from the recorder as method values; every ordered write, argument, fence, replay,
branch and error string stays byte-identical; `opencode/**` is untouched.

## 1. Owned-file re-derivation at dispatch

| Contract entry | HEAD reality | Action |
| --- | --- | --- |
| `execution/runner.go` `:100-158` | `Invocation` carries the ten members at `:112-145`; the linux/windows launchers read five of them; `probe.go` clears three | retarget every read through the port |
| `harness/types.go` `StartRequest` `:315-343` | `Invocation`, `Grant` and the three Persist* members `:326`,`:335`,`:339`,`:342` | keep all four; the three become recorder method values; one populate entry |
| `harness/claude_candidate_adapter.go` | moved to `harness/claude/adapter.go` by W4-15c (#236); the five conforming adapters already launch through `LaunchGrant` and name no durable callback | no adapter edit needed (gate re-derived by grep) |
| `app/app.go:1561-1567`, `goal_admission.go:817-827` | sites are `app.go:1504-1517` (direct runner path) and `goal_admission_tx.go:746-760`+`:908-929` (adapter path); `goal_admission.go` is a facade | edit `app.go`, `goal_admission_tx.go` |
| `app/linux_owner_witness_linux.go:3180-3184` | site is `linux_owner_witness_linux.go:3106-3119` + `:3234-3246` | edit that site |
| `scripts/ci/omo-control-native-evidence.py` `SOURCE_PATHS` | app.go/goal_admission.go/omo adapter listed | add `daemon/internal/app/process_recorder.go` |

## 2. Public/API shape

New `daemon/internal/execution/recorder.go`:

```go
// RecorderCapabilities reports which durable writes one Recorder performs; a
// false field reproduces the legacy absent callback for that write.
type RecorderCapabilities struct {
    PrepareSupervisorHandoff, BindSupervisorHandoff, CommitSupervisorHandoff bool
    RecordSupervisorHandoffStopReceipt, ClearSupervisorHandoff               bool
    PersistProcess, PersistProcessWithAuthority, PersistProcessAuthority     bool
    PersistContainmentStopReceipt, PersistContainmentUnproven                bool
}
func (capabilities RecorderCapabilities) SupervisorHandoffRequested() bool  // base any-of-five gate
func (capabilities RecorderCapabilities) SupervisorHandoffComplete() bool   // base prepare&&bind&&commit

// Recorder is one run's durable-write port; App binds RunKey/fence at build time.
type Recorder interface {
    Capabilities() RecorderCapabilities
    PrepareSupervisorHandoff(authority.SupervisorHandoff) error
    BindSupervisorHandoff(authority.SupervisorHandoff, int, string) error
    CommitSupervisorHandoff(authority.SupervisorHandoff, time.Time) error
    RecordSupervisorHandoffStopReceipt(authority.SupervisorHandoff, authority.StopReceipt) error
    ClearSupervisorHandoff(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error
    PersistProcess(int, string) error
    PersistProcessWithAuthority(int, string, *authority.Supervisor) error
    PersistProcessAuthority(int, string, *authority.Supervisor) error
    PersistContainmentStopReceipt(int, string, authority.StopReceipt) error
    PersistContainmentUnproven(int, string) error
}

// RecorderFuncs is the one shared implementation over optional write functions;
// an absent function is an absent capability (calling one fails closed).
type RecorderFuncs struct { ... ten optional func fields ... }
```

`Invocation` gains `Recorder Recorder`; the ten legacy members stay as the frozen OpenCode surface
(O2) with their contract comments moved onto the port methods. New:

```go
func (invocation *Invocation) MirrorRecorderMembers()   // frozen members = recorder method values
func invocationWrites(invocation Invocation) RecorderFuncs
```

`invocationWrites` is the one rule the runner and both launchers use: the invocation's `Recorder`
writes when one is present, otherwise the frozen members (OpenCode rebuilds its invocation from
`request.Invocation` members and cannot carry the port — O2 keeps those members populated for it).
The runner's existing per-write plumbing (`startedProcess`/`Process` func fields, retry helpers,
fence bookkeeping) is unchanged: it is filled from `writes` instead of from `invocation`, so branch
order, argument order, retries and error wrapping are preserved verbatim.

The two presence gates become the port's capability query (O5):
`writes.Capabilities().SupervisorHandoffRequested()` / `.SupervisorHandoffComplete()`; the two
helpers `durableSupervisorHandoffRequested`/`durableSupervisorHandoffCallbacksComplete` are deleted.

`harness` gains one populate entry (the W6-04 deletion site):

```go
func PopulateOpencodeRecorderMembers(request StartRequest) StartRequest
```

It mirrors `request.Invocation` members from the recorder and copies the recorder's
`PersistProcess`/`PersistProcessAuthority`/`PersistContainmentStopReceipt` method values onto the
three frozen `StartRequest` fields OpenCode reads. The W4-15b `grant()` fallback stays as it is: it
now only folds the request-level triple onto the frozen invocation members, which the runner reads
only when no recorder is present (direct constructions and tests; production always passes a grant).

## 3. App recorder

New `daemon/internal/app/process_recorder.go`:

```go
func (daemon *daemon) processRecorder(key state.RunKey, persistProcess func(int, string) error) execution.RecorderFuncs
```

One constructor over the existing `persist*Callback` factories (`prepareSupervisorHandoffCallback`,
`bindSupervisorHandoffCallback`, `commitSupervisorHandoffCallback`,
`recordSupervisorHandoffStopReceiptCallback`, `clearSupervisorHandoffCallback`,
`persistProcessAuthorityCallback` for both authority writes,
`persistContainmentStopReceiptCallback`, `markContainmentUnprovenCallback`) plus the site-specific
process-marker write, so each method performs exactly the Store mutation, fence check, replay and
error wrapping its callback performed. The three sites:

| Site | Process-marker write | Recorder |
| --- | --- | --- |
| `app.go` direct runner | `persistProcessMarker(key, pid, identity, daemon.now())` | `daemon.processRecorder(key, ...)` |
| `goal_admission_tx.go` | `tx.persistNativeProcess` | `daemon.processRecorder(tx.key, tx.persistNativeProcess)` in `buildNativeInvocation` |
| `linux_owner_witness_linux.go` | `persistProcessMarker(key, pid, identity, time.Now().UTC())` | `daemon.processRecorder(key, ...)` |

Each site builds one recorder, sets `Recorder` on the invocation, calls `MirrorRecorderMembers()`,
and the two adapter sites call `harness.PopulateOpencodeRecorderMembers(request)` before
`adapter.Start`; the grant carries the same invocation, so conforming adapters reach the port
without naming a callback.

## 4. Frozen surface and the compatibility rule

- Opencode reads `request.Invocation`'s five handoff members, `PersistProcessWithAuthority`,
  `PersistContainmentUnproven` and the three `StartRequest` fields; all ten therefore stay
  populated (O2), as method values of the run's recorder.
- The runner prefers `Recorder`. It falls back to the frozen members only when an invocation has no
  recorder — the invocation OpenCode rebuilds from `request` members. Without that rule an OpenCode
  launch would perform no durable write at all.
- `RunBoundedCommand` keeps its ephemeral strip and additionally clears the recorder.

## 5. Tests and mutants

- `execution/recorder_test.go`: the handoff truth table for all 32 member combinations (base
  any-of-five/complete rules vs the capability query); capability/view agreement for
  `RecorderFuncs`; the recorder wins over the frozen members; a recorder-carrying invocation runs
  the ordered writes and its capability query decides the branches.
- `app/process_recorder_test.go`: each recorder method reaches the same Store method with the same
  arguments (FaultStore hooks), and the capability set is complete.
- `harness/opencode_members_test.go`: every populated member calls the same recorder method (O2 gate).
- Recorded red mutants: (m1) reorder two durable writes in the runner's legacy path; (m2) drop the
  `PersistContainmentUnproven` installation; (m3) take the two-callback path when
  `PersistProcessWithAuthority` exists; (m4) populate one frozen member from the wrong recorder
  method; (m5) drop `ClearSupervisorHandoff` from the handoff decision.

## 6. Evidence plan (gates, foreground)

Callback-name grep (harness, excluding opencode and tests); per-adapter effective-write table base
vs HEAD; nil-combination table; handoff truth table; mutant runs; `gofmt -l`; `go vet ./...`;
`GOOS=linux`/`windows`/`darwin` (amd64) and `GOOS=linux GOARCH=arm64` builds; Windows
`go test -count=1 ./internal/execution/... ./internal/harness/... ./internal/app/...`; WSL
Ubuntu-24.04 (worktree path) `go test -count=1 -race ./internal/execution/... ./internal/harness/...
./internal/app/... ./internal/platform/...`; `git diff --name-only` (no `opencode/**`); R9
`python3 scripts/ci/test-omo-control-native-evidence.py`; coverage base vs HEAD for the touched
files.

## 7. Out of scope

The #185 seal consumer (W4-22); `LinuxV2JournalCallbacks` and the Linux v2 grant path (W4-23);
opencode removal (W6-04); the staged driver's own semantics (W4-15b); `opencode/**`.

## 8. Decisions and ambiguities

- The port's writes are consumed by the runner as write functions derived once per launch, not as
  per-write method values, so the existing deferred/retry plumbing stays intact and no closure is
  allocated per write; the port remains the single source (capability query included).
- The app composes the shared `execution.RecorderFuncs` instead of a second 10-method type in
  `process_recorder.go`: a wrapper type would only forward the same callbacks.
- The populate is unconditional at the two adapter sites (the app cannot branch per adapter kind at
  the single construction point); only OpenCode's adapter reads the populated members, which is what
  "for opencode only" pins, and the dated TODO entry names the removal.
- `Invocation.PersistProcess`/`PersistProcessAuthority`/`PersistContainmentStopReceipt` also stay as
  mirror targets: OpenCode copies them from the request onto its own invocation literal, and the
  frozen adapter's tests read them from the invocation it forwards.

## 9. As-built amendments (implement stage)

The tree is authoritative for these; the sections above record the design intent.

- Final names: `execution.Recorder` (the port), `execution.RecorderCapabilities` with
  `SupervisorHandoffRequested`/`SupervisorHandoffComplete`, `execution.RecorderFuncs` (the functional
  form), `execution.NewRecorder` (adapts functions into the port) over an unexported `recorderFuncs`,
  `invocationWrites`/`recorderWrites`, `Invocation.MirrorRecorderMembers` and
  `harness.PopulateOpencodeRecorderMembers`. The definition grep is in evidence §2.
- The functional form and the port are two types: Go forbids a struct field and a method with the
  same name, and the port's method names must be the write names O2 asks the mirrors to reach, so
  `RecorderFuncs` carries the ten functions and `recorderFuncs` carries the methods.
- `durableSupervisorHandoffRequested`, `durableSupervisorHandoffCallbacksComplete` and
  `tx.persistNativeProcessAuthority` are deleted: the launchers ask
  `writes.Capabilities().SupervisorHandoffRequested()`/`.SupervisorHandoffComplete()` (O5), and the
  transaction's authority write now exists only as the recorder method.
- `probe.go` clears `Recorder` in addition to the three members it already cleared.
- The harness populate test is `daemon/internal/harness/frozen_members_test.go` (the file name keeps
  the `opencode/**` path check unambiguous).
- R9 adds two `SOURCE_PATHS` entries: `daemon/internal/app/process_recorder.go` (the new receiver)
  and `daemon/internal/app/goal_admission_tx.go` (the launch wiring the recorder is built in; the
  W4-01 split had left it out while its facade `goal_admission.go` was listed).
- `StartRequest.grant()`'s no-grant fallback stays (evidence §3 explains its new role: it folds the
  three request-level members onto the frozen mirrors a direct construction without a recorder
  still launches through).
