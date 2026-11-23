# rf-w6-03 Go-test-binary fork removal — evidence record (2026-11-23, implement stage)

Contract: `goal-rf-w6-03-gotest-binary-fork-removal.md` (GDD state root, external to the
repository), with the 2026-11-23 re-draft and Conductor ruling (Option A; the Linux probe pin
relocated), the binding D1/D2/D4 decisions, the lesson-32 control-flow audit requirement, the
program-wide conductor rules and the 2026-11-22 comment policy. Plan:
`docs/plans/2026-11-23-rf-w6-03-gotest-binary-fork-removal-plan.md`. Base revision:
`b9d4b4a4cfff1467ce9bbd24c127107c884fd077` (W4-22 merged as #266; dependency satisfied). Branch:
`refactor/rf-w6-03-gotest-binary-fork-removal`. Host: Windows, Go 1.27.4; the Linux rows run
Ubuntu-24.04 over the same worktree through its mount point (`<worktree>/daemon`, Go 1.27.0) as
`wsl -d Ubuntu-24.04 -u accept`. The Windows and WSL suites never ran concurrently.

## 1. Subject bytes (sha256, final tree)

| File | sha256 |
| --- | --- |
| `daemon/internal/platform/containment_supervisor_windows.go` | `a092fedd3685fe8d25f2a82b4da19e8348ab5a3b71e905c7c94e41f907a502cf` |
| `daemon/internal/platform/supervisor_windows_client.go` | `f9b83c29710e492317327157dd21934132974600f89fe4ca740da68afb89cb3f` |
| `daemon/internal/platform/supervisor_linux_client.go` | `3fc7626317c705f8219d11f8bb9f12addccac551f1b508f37a89d54b4bfd4bb7` |
| `daemon/internal/platform/containment_supervisor_testing_windows.go` (new) | `163ac56c660cb5d10f5e9a5302dca2f2beacba651cabc4f8271d1bb9b7b3f339` |
| `daemon/internal/platform/containment_supervisor_testing_other.go` (new) | `e2563ff774f4ade7155d292df3f2f1c708d4cb0c37fbbb2ad9225072559eaea2` |
| `daemon/internal/execution/process_launcher_linux.go` | `61395b4eab4ec15e80d10113eb97a5c379855d0bbe8b34704af45813a8264d06` |
| `daemon/internal/execution/process_launcher_windows.go` | `09416475476bb8c11e3fcb846481f61ffa057c4acdd49a9d49546f92ac8f7761` |
| `daemon/internal/execution/inprocess_containment_testing.go` (new) | `198c75f4af5a13d5881f8a5163b0af1d946a4d6b683aa79e31cecb434d380e29` |
| `daemon/internal/execution/inprocess_containment_testing_unsupported.go` (new) | `2dcf24f2aaf85342115a456f388fbdae5be06eb362462f238779b2ce313f8ec7` |
| `daemon/internal/platform/no_test_binary_branch_test.go` (new) | `d1a5084c2eee35d427a421bd910fad9b98d46b2384938fe6019aa8619876c14a` |
| `daemon/internal/execution/runner_windows_test.go` | `0161fccc0d7800dc78cfa1776f6fb82d510676d47f8c8af16819f35c619233a3` |
| `daemon/internal/execution/linux_owner_v2_test.go` | `596d7237297a93a6c59b4dbce3dfb6ea4a1e8b9248bae224372f71a4c338ae29` |
| `daemon/internal/platform/containment_supervisor_windows_test.go` | `4a0e65a37df6f6e9c6158b7bb951a89cf2b97e3e1cecb026d2de776afa633d4b` |
| `daemon/internal/platform/containment_supervisor_linux_test.go` | `9b5bcd657d184b55e1a44c69aeb4dae8e37ad3808a59c0644577383531aa531f` |
| `daemon/internal/platform/process_windows_test.go` | `3918e8e817e63e8e71d29e0cf0b691bd9285973ece344daa803f3defa165bd5f` |
| `daemon/internal/platform/native_launch_windows_test.go` | `d5bd9e44acf81a800b9d4068b4fadbb0348a4282fb3bab87fa8479e8afeb9bb7` |
| `daemon/internal/platform/containment_supervisor_production_windows_test.go` | `5ee21802d2f751de915119718ce277f6a140a26d7a2c557dedb318347b64cbfb` |
| `daemon/internal/execution/inprocess_containment_windows_test.go` (new) | `981fe15ac98ab7b1312d2a6caf45eabfca025f4f780b26be71787315020deda5` |
| `daemon/internal/app/inprocess_containment_test.go` (new) | `f89720befa058d873158ac1f7182a33145569584351a9efe397cc8f448d3c274` |
| `daemon/internal/harness/omp/inprocess_containment_test.go` (new) | `5accfb3aeba8ab405beff950d10f579feaac17c2697e535a4a31d3fa33be85c6` |
| `daemon/internal/harness/omo/inprocess_containment_test.go` (new) | `050375be1782ff56d26b94f18dab347150208b8aeec7a7e24eef3cc017acecc8` |
| `docs/plans/2026-11-23-rf-w6-03-gotest-binary-fork-removal-plan.md` | `20ddaf29398cb7a466dc77596ffa7e44790174c3143f709028b8c41728b3b328` |
| this record | the file you are reading (no self-referential hash) |

## 2. Deleted fork sites and the replacement capability

Deleted from shipped code (exact HEAD positions before deletion):

- `platform/containment_supervisor_windows.go:352-358` `isGoTestBinary` (`flag.CommandLine.Lookup("test.v")`
  plus a `.test.exe`/`.test` suffix check) and the file's only `flag` import.
- `platform/containment_supervisor_windows.go:340-350` `noopContainmentSupervisorLease` (moved into the
  new capability file; not deleted behaviour).
- `platform/supervisor_windows_client.go:653-659` the noop take in `launchContainmentSupervisorProcess`,
  including its comment; :1073-1076 `DurableSupervisorHandoffAvailable`.
- `platform/supervisor_linux_client.go:26-34` the inline probe and the file's only `flag` import.
- `execution/process_launcher_linux.go:37` and `execution/process_launcher_windows.go:51` both drop
  `&& platform.DurableSupervisorHandoffAvailable()`; the durable decision is
  `capabilities.SupervisorHandoffRequested()` (Linux) and
  `capabilities.SupervisorHandoffRequested()` after the unguarded completeness refusal (Windows).

Added capability:

- `platform.UseInProcessContainmentSupervisorForTesting() func()`:
  - windows (`containment_supervisor_testing_windows.go`): installs `launchContainmentSupervisor`
    (the existing seam) to return `noopContainmentSupervisorLease` + the exact target identity;
  - other GOOS (`containment_supervisor_testing_other.go`): no-op restore.
- `execution.UseInProcessContainmentSupervisorForTesting() func()`, the one call test binaries make:
  calls the platform capability and the per-OS route hook —
  - windows (`process_launcher_windows.go`): replaces `attachSuspendedProcessWithHandoff` with a
    double that runs `platform.AttachSuspendedProcess` (local job containment) and returns an empty
    handoff, so `callbacks.Prepare`/`Bind` are never called, exactly like the base diversion;
  - linux (`process_launcher_linux.go`): replaces `launchLinuxSupervisor` (the extracted durable
    dispatch) with `launchLocalLinuxProcess`;
  - other GOOS (`inprocess_containment_testing_unsupported.go`): no-op.
- Production callers: none. `platform/no_test_binary_branch_test.go` walks every shipped (non-test)
  `.go` file under `daemon/` and allows the identifier only in the three defining files.

Before/after site grep (`daemon/`, shipped files only; base output captured before the deletions):

```
$ rg -n 'isGoTestBinary|DurableSupervisorHandoffAvailable|launchContainmentSupervisorProcess|noopContainmentSupervisorLease|flag\.CommandLine|test\.v' --glob '!**/*.md' internal
internal/execution/process_launcher_linux.go:37:	if capabilities.SupervisorHandoffRequested() && platform.DurableSupervisorHandoffAvailable() {
internal/execution/process_launcher_windows.go:51:	if capabilities.SupervisorHandoffRequested() && platform.DurableSupervisorHandoffAvailable() {
internal/platform/containment_supervisor_linux_test.go:92:	if DurableSupervisorHandoffAvailable() {
internal/platform/containment_supervisor_windows.go:175:	launchContainmentSupervisor         = launchContainmentSupervisorProcess
internal/platform/containment_supervisor_windows.go:352:func isGoTestBinary(path string) bool {
internal/platform/containment_supervisor_windows.go:353:	if flag.CommandLine.Lookup("test.v") == nil {
internal/platform/supervisor_linux_client.go:28:func DurableSupervisorHandoffAvailable() bool {
internal/platform/supervisor_linux_client.go:29:	if flag.CommandLine.Lookup("test.v") == nil {
internal/platform/supervisor_linux_client.go:32:	name := strings.ToLower(os.Args[0])
internal/platform/supervisor_windows_client.go:657:	if isGoTestBinary(os.Args[0]) {
internal/platform/supervisor_windows_client.go:1075:	return !isGoTestBinary(os.Args[0])

$ rg -n 'flag\.CommandLine\.Lookup|test\.v|isGoTestBinary|"\.test' internal/platform internal/execution --glob '!*_test.go'
(no output)
```

## 3. Base-vs-HEAD truth table of the handoff decision

Decision inputs: `capabilities.SupervisorHandoffRequested()` (any of the five handoff writes) and
`capabilities.SupervisorHandoffComplete()` (prepare+bind+commit). Shapes: (1) app-built Recorder,
all ten writes; (2) legacy members, all five handoff writes; (3) prepare-only (incomplete);
(4) persist-process only (no handoff write); (5) empty invocation.

| Shape | Base, production binary | Base, test binary | HEAD, production binary | HEAD, test binary (capability installed) |
| --- | --- | --- | --- | --- |
| (1) all-ten recorder | Windows: durable; Linux: durable | Windows: local (probe false); Linux: local (probe false) | durable (deleted probe always true in a daemon binary) | local (both OSes, through the installed route hook) |
| (2) five legacy members | durable | local | durable | local |
| (3) prepare-only | Windows: refusal before the gate; Linux: refusal inside the gate | Windows: refusal (unguarded); Linux: local (gate false) | Windows: refusal (unchanged position); Linux: refusal | Windows: refusal (base); Linux: local (base) |
| (4) persist-process only | local | local | local | local |
| (5) empty invocation | local | local | local | local |

The recorded base asymmetry is shape (3): the Windows refusal sat before the availability gate
(`process_launcher_windows.go:33`), so a test binary refused a partial handoff, while the Linux
refusal sat inside the gate, so a test binary stayed local. HEAD keeps each OS's base in-process
behaviour exactly: Windows keeps the refusal where it was; Linux routes every requested shape
through the dispatch seam, which the installed capability replaces with the local route. The Linux
refusal therefore moved to the top of `launchLinuxSupervisorProcess` (still inside the requested
branch, before any dispatch side effect) — recorded as the as-built detail in plan §3. No shipped
caller depended on the removed default: the only production consumers were the two launcher gates
(where the deleted call was always true in a daemon binary) and the noop take (where
`flag.CommandLine.Lookup("test.v") == nil` always holds in a daemon binary).

## 4. Control-flow audit of the deleted branch family (lesson 32)

| Base branch | Condition | HEAD equivalent |
| --- | --- | --- |
| `isGoTestBinary` → false | `test.v` absent (production) | deleted; no shipped code reads the flag |
| `isGoTestBinary` → true | `test.v` present and path ends `.test.exe`/`.test` | explicit capability install per test binary (recorded install files) |
| `isGoTestBinary` → false | `test.v` present without the suffix (e.g. a custom test runner) | deleted; such a process now takes the production route like any other binary |
| Linux probe → true | `test.v` absent | deleted; the durable branch is unconditional on the capability |
| Linux probe → false | `test.v` present with `.test` suffix | explicit capability (installed route) |
| Linux probe → true | `test.v` present without the suffix | deleted; production route |
| Windows noop early return | `isGoTestBinary(os.Args[0])` in `launchContainmentSupervisorProcess` | `platform.UseInProcessContainmentSupervisorForTesting` installed launcher returns the noop lease |
| `launchContainmentSupervisor` seam | unchanged | unchanged (the installed capability swaps its value) |

Caller census at HEAD before deletion: `isGoTestBinary` had exactly two callers
(`supervisor_windows_client.go:657` noop take, `:1075` probe); `DurableSupervisorHandoffAvailable`
exactly three (`process_launcher_linux.go:37`, `process_launcher_windows.go:51`,
`containment_supervisor_linux_test.go:92`). All shipped callers were removed with their condition;
no other shipped site read `flag.CommandLine.Lookup`, `os.Args[0]` or `os.Executable()` to pick a
containment route (the remaining `flag.` uses in the platform package are `flag.NewFlagSet` in the
helper/anchor parsers, unrelated to routing).

## 5. Conversion table (fork branch → HEAD replacement)

| Dependent | Base dependency | HEAD replacement |
| --- | --- | --- |
| ≈30 in-process launch sites in `execution`, `app`, `harness/omp`, `harness/omo` (census confirmed by the base-red experiment) | noop take inside `launchContainmentSupervisorProcess` | per-binary `TestMain` install: `execution/inprocess_containment_windows_test.go` (Windows), `app/inprocess_containment_test.go`, `harness/omp/inprocess_containment_test.go`, `harness/omo/inprocess_containment_test.go` (both OSes) |
| `platform` package tests | noop take | `TestMain` in `containment_supervisor_windows_test.go` |
| `TestNonDurableStopLeaseCloseRetainsFailedStopProof` | fork precondition (`DurableSupervisorHandoffAvailable()` false) | explicit `restoreContainmentSupervisorLauncher` install of the non-durable stop lease; the capability and stop/release assertions are unchanged |
| `TestAttachSuspendedProcessTransfersJobOwnershipToContainment` | noop take | explicit `installContainmentSupervisorTestLauncher` |
| `TestLinuxSupervisorDurableHandoffIsDisabledForGoTestBinary` | asserted the platform probe | relocated to `execution/linux_owner_v2_test.go`; installs the capability and asserts a complete-capability invocation stays on the local route with `containmentHandoff == nil` |
| `TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse` | n/a (ran in a real test binary; probe kept it on the local route) | restores the production launcher locally so the witness still drives `launchContainmentSupervisorProcess` (real helper dispatch of the built daemon) |
| `TestDefaultLinuxLauncherDoesNotUseTestBinaryV1BypassForV2` | v2 precedes the gate | untouched; re-run green |
| `execution` package launches without handoff capabilities (helper-invocation census) | probe false → local | unchanged route; the Windows local route needs the installed noop lease |

## 6. Route pins and the negative pin (red at base, green at HEAD)

Commands (each run in the worktree, `cd daemon`):

```
base: go test -count=1 -run '^TestDefaultWindowsLauncherUsesRecorderHandoffCapability$' ./internal/execution/
base: go test -count=1 -run 'TestShippedContainmentCodeHasNoTestBinarySelfInspection|TestInProcessContainmentCapabilityStaysTestOnly' ./internal/platform/
base: wsl -d Ubuntu-24.04 -u accept -- ... go test -count=1 -run '^TestDefaultLinuxLauncherUsesRecorderHandoffCapability$' ./internal/execution/
```

Observed at base (tree = base revision plus the new pins; the Windows gate restored for the base
measurement, as it is part of the change):

- Windows route pin: `--- FAIL: TestDefaultWindowsLauncherUsesRecorderHandoffCapability` —
  `complete handoff recorder did not reach the durable route: process=<non-nil> err=<nil>`; the
  fork diverted the invocation to the local route, exactly as the contract predicts.
- Platform pins: `--- FAIL` for both — `containment_supervisor_windows.go decides containment from
  the test runner via "flag.CommandLine.Lookup"`, `"test.v"`, `"isGoTestBinary"`, `'".test"'`,
  `supervisor_linux_client.go` (three tokens), `supervisor_windows_client.go` (`isGoTestBinary`);
  and `capability references found = 0, want the 3 defining files`.
- Linux route pin: `--- FAIL: TestDefaultLinuxLauncherUsesRecorderHandoffCapability` —
  `complete handoff recorder did not enter the durable supervisor branch: contain Linux process tree:
  capture process group anchor: process <pid> is in process group <pgrp>, want <pid>` (the local
  route ran the process, so the expected supervisor-branch error was absent).

At HEAD (final tree) all three pins report `ok`:

```
go test -count=1 -run '^TestDefaultWindowsLauncherUsesRecorderHandoffCapability$' ./internal/execution/   # ok, exit 0
go test -count=1 -run 'TestShippedContainmentCodeHasNoTestBinarySelfInspection|TestInProcessContainmentCapabilityStaysTestOnly' ./internal/platform/   # ok, exit 0
wsl -d Ubuntu-24.04 -u accept -- ... go test -count=1 -run 'TestDefaultLinuxLauncherUsesRecorderHandoffCapability|TestLinuxSupervisorDurableHandoffIsDisabledForGoTestBinary' ./internal/execution/   # ok
```

## 7. Gates (final tree, real output)

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| gofmt | `cd daemon && gofmt -l .` | 0 | no files listed |
| vet | `go vet ./...` | 0 | no diagnostics |
| build windows | `GOOS=windows go build ./...` | 0 | clean |
| build linux | `GOOS=linux go build ./...` | 0 | clean |
| build darwin | `GOOS=darwin go build ./...` | 0 | clean |
| whitespace | `git diff --check` | 0 | clean |
| Windows suite | `go test -count=1 -timeout 1800s -v ./internal/platform/... ./internal/execution/... ./internal/app/... ./internal/harness/...` | 0 | 13 packages `ok`; 1365 `--- PASS`, 0 `--- FAIL`, 35 `--- SKIP` |
| WSL suite | `go test -count=1 -race -timeout 2400s -v ./internal/platform/... ./internal/execution/... ./internal/app/... ./internal/harness/...` under `wsl -d Ubuntu-24.04 -u accept` over the same worktree | 0 | 13 packages `ok`; 1627 `--- PASS`, 0 `--- FAIL`, 49 `--- SKIP` (46 distinct names) |
| Windows witness | `SYMMETRY_PRODUCTION_CONTAINMENT_WITNESS=1 go test -count=1 -timeout 180s -v ./internal/platform -run '^TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse$'` | 0 | `--- PASS: TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse (8.37s)`, `ok ... internal/platform 9.878s` — real helper dispatch through the step-4 launcher restore |
| Linux e2e witness | anchored CI expression `go test -count=1 -timeout 300s -v -run '^TestProductionLinuxContainment(WitnessSpine|NegativeWitness|ReplayWitness)$' ./e2e/` in WSL | 0 | package compiles; all three witnesses skip without the runner-provisioned control plane (`SYMMETRY_E2E=1` + live `http://127.0.0.1:4000`): `--- SKIP: TestProductionLinuxContainment{NegativeWitness,ReplayWitness,WitnessSpine}`. The contract's prefix command and the real run stay on the CI `daemon` job (`.github/workflows/ci.yml` step with `SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_*` and `go-test-json-gate.py --assert '*' ...`) — recorded CI-only |

Windows SKIP names (all pre-existing env gates; the diff adds no `t.Skip`):
`TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse`,
`TestPreparePiProviderBridgeFailsClosedWithoutLinuxShare`, `TestPiProbeLiveRejectsDifferentBlob`,
`TestPiProbeLivePromotesTheTestedBlobOnlyOnWindows`,
`TestOmOReviewRegressionAbsoluteResolutionFailureHasOnlyAvailabilityLeaf`,
`TestNativeSyntheticGatewayToolIntegration`, `TestNativeResumeEventCapture`,
`TestNativeRepositoryTaskRetainedResume`, `TestNativeRPCOpenAndClose`,
`TestNativePromptAdmissionReplaysAfterPost`, `TestNativePiProviderBridgeLifecycle`,
`TestNativePiProviderBridgeAction`, `TestNativeOpenCodeTerminalCandidateWitness`,
`TestNativeClaudeCandidateStructuredOutput`,
`TestNativeCancellationDrainsInFlightLoopbackRequest`, `TestNativeAcceptanceLostAckHelper`,
`TestNativeAcceptanceCancelToolHelper`, `TestNativeAcceptanceAuthenticationFailure`,
`TestMaterializeProviderBridgeExtensionRejectsExistingSymlink`,
`TestMaterializeProviderBridgeExtensionForShareRequiresProvisionedSafePath`,
`TestMaterializeProviderBridgeExtensionForShareRejectsSymlinkAndDigestConflict`,
`TestMaterializeProviderBridgeExtensionForShareProducesAOwnedBReadableImmutableFile`,
`TestLocalCredentialedTurnSmoke`, `TestLocalCredentialedCancelSmoke`,
`TestGeneratedProviderBridgeExtensionLoadsInNativePi`,
`TestClaudeCandidateLiveRepositoryTurnSmoke`, `TestClaudeCandidateLiveProbeRejectsDifferentBlob`,
`TestClaudeCandidateLiveProbePromotesTheTestedBlob`, `TestClaudeCandidateLiveCancelSmoke`,
`TestNativeTransportOpenAndClose` (×2 packages), `TestNativeRepositoryTask` (×2),
`TestNativeAcceptance` (×2).

WSL SKIP names (46 distinct; all pre-existing env/platform gates):

```
TestAdapterBindsAndRevokesDaemonOwnedProviderBridge
TestAdapterProviderBridgeBindFailureReturnsOwnedSessionForCleanup
TestClaudeCandidateLiveCancelSmoke
TestClaudeCandidateLiveProbePromotesTheTestedBlob
TestClaudeCandidateLiveProbeRejectsDifferentBlob
TestClaudeCandidateLiveRepositoryTurnSmoke
TestGeneratedProviderBridgeExtensionLoadsInNativePi
TestGoalAdmissionTxClosesUnownedProviderBridgeForReal
TestGoalAdmissionTxHandsTheProviderBridgeToTheNativeOwner
TestLegacyProviderBridgeExtensionMaterializationRemainsAvailableOnWindows
TestLegacyWindowsPeerVerifierAllowsOwnedHTTPConnection
TestLegacyWindowsPeerVerifierRejectsWrongIdentityBeforeWriting
TestLinuxLegacyOpenCodeRefusesBeforeHealthRequest
TestLinuxLegacyPiProviderBridgeRefusesBeforeProviderAction
TestLinuxOwnerAnchorRunReaperRejectsForeignChildBeforeAdoptingIt
TestLocalCredentialedCancelSmoke
TestLocalCredentialedTurnSmoke
TestNativeAcceptance
TestNativeAcceptanceAuthenticationFailure
TestNativeAcceptanceCancelToolHelper
TestNativeAcceptanceLostAckHelper
TestNativeCancellationDrainsInFlightLoopbackRequest
TestNativeClaudeCandidateStructuredOutput
TestNativeOpenCodeTerminalCandidateWitness
TestNativePiProviderBridgeAction
TestNativePiProviderBridgeLifecycle
TestNativePromptAdmissionReplaysAfterPost
TestNativeRPCOpenAndClose
TestNativeRepositoryTask
TestNativeRepositoryTaskRetainedResume
TestNativeRepositoryTaskWindowsEnvironmentDerivesMiseDataDirectory
TestNativeRepositoryTaskWindowsEnvironmentPreservesExecutableResolution
TestNativeResumeEventCapture
TestNativeSyntheticGatewayToolIntegration
TestNativeTransportOpenAndClose
TestOpenCodeKernelPeerServerProcess
TestOpenCodeQualifiedNonDumpablePeerDeniedBeforeHTTP
TestOpenCodeQualifiedStartNamespacePeerDenials
TestPiProbeLivePromotesTheTestedBlobOnlyOnWindows
TestPiProbeLiveRejectsDifferentBlob
TestPiResumeDefinitiveAttachRejectionAfterKnownCloseQuarantinesSession
TestPiResumeUnknownStartRetainsReadyAttachmentBarrier
TestProviderBridgeKernelPeerClientProcess
TestProviderBridgeQualifiedNonDumpablePeerFailsClosedBeforeProviderAction
TestWindowsLegacyProviderBridgeRejectsDifferentProcessIdentity
TestWindowsLegacyProviderBridgeRequiresExactPeerIdentity
```

Compile-out subset (`GOOS=windows` vs `GOOS=linux` `go list` over the four trees; 146 test files
compiled on Windows, 161 on Linux):

- Windows-only (19): `internal/app/{linux_owner_witness_unsupported,pi_provider_bridge_peer_nonlinux,terminate_windows}_test.go`;
  `internal/execution/{identity_windows,inprocess_containment_windows,linux_owner_v2_unsupported,probe_process_exists_windows,runner_termination_order_windows,runner_windows}_test.go`;
  `internal/harness/claude/adapter_cmdline_windows_test.go`;
  `internal/harness/omp/adapter_windows_test.go`;
  `internal/platform/{clone_aliasing_windows,containment_supervisor_production_windows,containment_supervisor_receipt_windows,containment_supervisor_windows,directory_path_windows,native_launch_windows,process_windows,tcp_peer_windows}_test.go`.
- Linux-only (34): `internal/app/{journal_store_linux,linux_owner_preflight_state_dir_app,linux_owner_v2_app,linux_owner_witness_linux,pi_provider_bridge_peer_linux,recovery_linux,terminate_linux}_test.go`;
  `internal/execution/{identity_linux,linux_owner_v2_finalize_pipeline,linux_owner_v2,probe_process_exists_unix,process_launcher_linux,runner_unix}_test.go`;
  `internal/harness/opencode/{adapter_native_work_linux,peer_linux}_test.go`;
  `internal/harness/pi/{provider_bridge_peer_linux,provider_extension_native_linux}_test.go`;
  `internal/platform/{anchor_shape_table,anchor_wire_shape,clone_aliasing_linux_amd64,clone_aliasing_linux,containment_errors,containment_supervisor_linux,directory_path_other,identity_linux,linux_cgroup_v2_linux,linux_owner_anchor_linux,linux_owner_helper_v2_linux,linux_ptrace_launch_linux,linux_supervisor_protocol_linux,process_unix_linux,supervisor_linux_witness,tcp_peer_linux,tcp_peer_linux_v2}_test.go`.

The new Windows route pin lives in `runner_windows_test.go` and the relocated Linux pin plus the
Linux route pin live in `linux_owner_v2_test.go`, so each runs on its own OS half; the negative
pins are cross-platform and run in both suites.

## 8. Mutants (each: mutant red, restored green)

| Mutant | Patch | Oracle and observed result |
| --- | --- | --- |
| M1 | re-added `isGoTestBinaryMutation` (`flag.CommandLine.Lookup("test.v")` + `.test.exe`/`.test` suffix) to `platform/containment_supervisor_windows.go` and its call in `launchContainmentSupervisorProcess` | `go test -count=1 -run '^TestShippedContainmentCodeHasNoTestBinarySelfInspection$' ./internal/platform/` → exit 1, `--- FAIL` with five token reports (four in `containment_supervisor_windows.go`, one in `supervisor_windows_client.go`); restored → exit 0 `ok` |
| M2 | re-added a test-binary short-circuit to both launcher gates: `&& !strings.Contains(os.Args[0], ".test")` plus the `strings` import in `process_launcher_linux.go` / `process_launcher_windows.go` | Windows: `go test -count=1 -run '^TestDefaultWindowsLauncherUsesRecorderHandoffCapability$' ./internal/execution/` → exit 1, `complete handoff recorder did not reach the durable route` (local route ran); Linux (WSL): `go test -count=1 -run '^TestDefaultLinuxLauncherUsesRecorderHandoffCapability$' ./internal/execution/` → exit 1, `did not enter the durable supervisor branch`; restored → both `ok` (exit 0) |
| M3 | removed the execution-side route install (`restoreRoute := installInProcessSupervisorRouteForTesting()` dropped from `UseInProcessContainmentSupervisorForTesting`) | Windows: `go test -count=1 -run '<the two decision-liveness tests>' ./internal/app/` → exit 1 (`start ... connect durable containment supervisor: containment supervisor connection timeout`); Linux (WSL): the relocated `TestLinuxSupervisorDurableHandoffIsDisabledForGoTestBinary` → exit 1, `receive Linux supervisor pidfd: Linux supervisor fd payload is incomplete`; restored → app tests `ok` (exit 0), relocated test `ok` (exit 0) |
| M4 | made the converted stop-lease install expose durable authority (`mutantDurableAuthorityLease` embedding `noopContainmentSupervisorLease` plus `containmentAuthority()`) | `go test -count=1 -run '^TestNonDurableStopLeaseCloseRetainsFailedStopProof$' ./internal/platform/` → exit 1, `in-process supervisor platform.mutantDurableAuthorityLease exposes durable authority; the non-durable stop lease route is not exercised`; restored → exit 0 `ok` |

## 9. dLOC (D2: report-only)

`git diff --numstat` over the strictly owned shipped files
(`containment_supervisor_windows.go`, `supervisor_windows_client.go`, `supervisor_linux_client.go`,
`process_launcher_linux.go`, `process_launcher_windows.go`): +71/−80, net −9 (the deleted fork family — `isGoTestBinary`, both probes, the noop take — is 31 of the deleted lines); the four new shipped capability files add 64 lines (35/9/15/5). Base plan estimated −40 dLOC;
the gate is the grep + the pins + the mutations).

## 10. Residual risks and limits

- The capability's Linux route hook is a package variable whose shipped default is the Recorder
  capability query; the install is test-named, never called by production, and pinned by the
  shipped-source walk in `no_test_binary_branch_test.go`. A reviewer must accept it as the
  "injectable test capability" the binding D4 decision allows (recorded as plan §3).
- The Windows durable diversion returns an empty handoff, so a launch that requests handoff in an
  instrumented test binary wraps attach failures with the durable route's message
  (`contain durable process tree for ...`) instead of the local route's. No assertion depends on
  that message.
- The Linux e2e witness needs the GitHub-runner environment (live control plane at
  `http://127.0.0.1:4000`, fake agent, `/tmp` workspace); it is recorded CI-only with its exact
  command and the freezing CI step. No skip is counted as coverage of the removed branch.
