# rf-w6-03 Go-test-binary fork removal — plan

Contract: `goal-rf-w6-03-gotest-binary-fork-removal.md` (GDD state root, external to the
repository), including the 2026-11-23 re-draft, the Conductor ruling of 2026-11-23 (Option A adopted,
`TestLinuxSupervisorDurableHandoffIsDisabledForGoTestBinary` relocated to
`execution/linux_owner_v2_test.go`), the binding D1/D2/D4 decisions, the lesson-32 control-flow audit
requirement, the program-wide conductor rules and the 2026-11-22 comment policy. Base revision:
`b9d4b4a4cfff1467ce9bbd24c127107c884fd077` (W4-22 merged as #266; dependency satisfied). Branch:
`refactor/rf-w6-03-gotest-binary-fork-removal`. Owned files are the contract's strict set, re-derived
in §1, plus this plan and `docs/evidence/rf-w6-03-gotest-binary-fork-removal-2026-11-23.md`.

Goal shape: the shipped containment route stops branching on the Go test runner. `isGoTestBinary`,
both `DurableSupervisorHandoffAvailable` probes and the noop take inside
`launchContainmentSupervisorProcess` are deleted; both launchers route on `RecorderCapabilities`
only; the in-process local route becomes one explicit test capability
(`platform.UseInProcessContainmentSupervisorForTesting` with a no-op twin off Windows) that every
affected test binary installs once. Production bytes are unchanged for every production invocation
shape: in a daemon binary the deleted probe always returned true and the app-built recorder always
requests handoff.

## 1. Owned-file re-derivation at dispatch (HEAD `b9d4b4a4`)

| Contract entry (@`e13450bc`) | HEAD reality | Action |
| --- | --- | --- |
| `platform/containment_supervisor_windows.go:352-358` `isGoTestBinary` + the file's only `flag` import | same text at `:352-358` (`containment_supervisor_windows.go` head unchanged by W4-22) | delete; move `noopContainmentSupervisorLease` (`:340-350`) into the new windows capability file |
| `platform/supervisor_windows_client.go:651-659` noop take in `launchContainmentSupervisorProcess` | noop take at `:655-659`, comment `:653-654` | delete the take and the comment |
| `platform/supervisor_windows_client.go:1073-1076` `DurableSupervisorHandoffAvailable` | `:1073-1076` | delete |
| `platform/supervisor_linux_client.go:27-34` inline probe | `:26-34` (comment + probe) | delete; drop the file's only `flag` import |
| `execution/process_launcher_linux.go:37` gate | gate at `:37`; durable body `:38-74` | drop the gate; extract the durable body to `launchLinuxSupervisor` (seam var) and the local body to `launchLocalLinuxProcess` |
| `execution/process_launcher_windows.go:51` gate | gate at `:51`; `attachSuspendedProcessWithHandoff` var at `:17` | drop the gate; the existing var doubles as the durable-route seam for the capability |
| `platform/containment_supervisor_linux_test.go:90-94` probe pin | `:90-94` | delete here; relocate in §3 |
| `platform/process_windows_test.go:877-946` stop-proof pin, fork precondition `:878-880` | `:870-939` (W4-22 did not touch it) | install a test-local non-durable stop lease through `restoreContainmentSupervisorLauncher`, drop the fork precondition, reword the two "test-binary supervisor" failure messages |
| `platform/native_launch_windows_test.go:226-259` attach pin | `:226-263` | install `installContainmentSupervisorTestLauncher` |
| `platform/containment_supervisor_production_windows_test.go:24-31,59` real-route witness | `:23-124` | restore the production launcher locally around the witness (the capability's TestMain install otherwise diverts it) |
| `execution/linux_owner_v2_test.go:335-353` v2 bypass pin | `:335-353` | untouched; re-run and record |
| implicit dependents: `execution/runner_test.go`, `probe_test.go`, `runner_windows_test.go`, `identity_windows_test.go`; `app/decision_liveness_test.go`, `output_settlement_test.go`; `harness/omp/native_acceptance_test.go`, `harness/omo/native_test.go` | census confirmed by the base-red experiment: with only the Windows gate dropped, `TestRequiredInvalidDecisionStopsRealBlockedChildBeforeAndAfterAttachment` (both subtests) and `TestLegacyQuestionOnlyDecisionKeepsRealChildLiveUntilInput` fail with `connect durable containment supervisor: containment supervisor connection timeout` | each test binary installs the capability once (`TestMain` per package, §2) |

The caller census re-measured at HEAD: `isGoTestBinary` had exactly two callers
(`supervisor_windows_client.go:657,:1075`); `DurableSupervisorHandoffAvailable` had four call
sites (two shipped: `process_launcher_linux.go:37`, `process_launcher_windows.go:51`; two tests:
`containment_supervisor_linux_test.go:92`, `process_windows_test.go:878`), plus its two
definitions; a non-test grep over `daemon/internal` for
`flag.CommandLine.Lookup` / `test.v` / `isGoTestBinary` returned exactly the two probe sites.

## 2. Public/API shape

```go
// platform (windows capability file; !windows twin returns a no-op restore)
func UseInProcessContainmentSupervisorForTesting() func()

// execution (cross-platform; the single entry point test binaries call)
func UseInProcessContainmentSupervisorForTesting() func()
```

Install semantics, per platform:

- Windows: `platform` installs the local noop lease into `launchContainmentSupervisor` (the
  existing seam). `execution` adds `installInProcessSupervisorRouteForTesting`, which replaces
  `attachSuspendedProcessWithHandoff` with a double that calls `platform.AttachSuspendedProcess`
  (local job containment) and returns an empty handoff. Prepare/Bind are never called, exactly like
  the base fork, which diverted such invocations before those callbacks.
- Linux: `execution` replaces `launchLinuxSupervisor` (the extracted durable dispatch) with
  `launchLocalLinuxProcess`. `platform`'s twin is a no-op because the local Linux route never spawns
  a helper.
- Other GOOS: no-op restore; the shipped route has no supervisor to divert.

New files: `platform/containment_supervisor_testing_windows.go`,
`platform/containment_supervisor_testing_other.go`,
`execution/inprocess_containment_testing.go`,
`execution/inprocess_containment_testing_unsupported.go`,
`platform/no_test_binary_branch_test.go`, and one install `TestMain` per affected test binary
(`execution/inprocess_containment_windows_test.go`, `app/inprocess_containment_test.go`,
`harness/omp/inprocess_containment_test.go`, `harness/omo/inprocess_containment_test.go`; the
platform binary's `TestMain` lives in the owned `containment_supervisor_windows_test.go`).

## 3. As-built amendment to step 3 (recorded, not silent)

The re-drafted step 3 lists one exported platform capability and a Linux no-op twin. The base-red
experiment shows the Linux twin cannot be a no-op: on Linux, app-built invocations request handoff
through the app recorder, so dropping the Linux gate sends in-process app/harness tests into
`platform.StartLinuxSupervisor`, which re-executes `os.Args[0]` with `-containment-supervisor`, a
mode a generated test main cannot dispatch (same failure the Windows experiment produced, per the
control-flow audit). The Linux durable route is execution-side, and a cross-package test binary can
only reach it through an exported execution function, so the delivered shape adds:

- a small action seam in `execution/process_launcher_linux.go`
  (`launchLinuxSupervisor = launchLinuxSupervisorProcess`, extracted from the inline durable body),
  which keeps the shipped decision literally `capabilities.SupervisorHandoffRequested()`; and
- `execution.UseInProcessContainmentSupervisorForTesting`, the one call test binaries use (it calls
  the platform capability and the per-OS route hook).

This satisfies clause 1 (both launchers route on `RecorderCapabilities` only; no self-inspection;
the noop route only behind the explicit capability; WSL green) and D4 ("an injectable test
capability") while keeping the platform capability exactly as the contract describes. The
alternative (Option B, production-binary exec for ≈30 launches) was rejected in the contract itself
on cost/flake grounds.

The Linux route pin (`TestDefaultLinuxLauncherUsesRecorderHandoffCapability`) runs without any
install and therefore still observes the shipped durable branch; the relocated
`TestLinuxSupervisorDurableHandoffIsDisabledForGoTestBinary` runs with the capability installed and
asserts a complete-capability invocation stays on the local route with no handoff.

One further as-built detail keeps the truth-table gate exact on both OSes: the Linux
incomplete-capability refusal (`durable Linux supervisor handoff callbacks are incomplete`) moved
from the launcher body to the top of the dispatch function the requested branch calls, so the
installed in-process route reproduces the base local diversion for every requested shape, partial
handoff included, while a production invocation keeps the refusal before any dispatch side effect.
The Windows refusal stays where it was (unguarded, before the durable branch), matching its base
test-binary behaviour, which already refused partial shapes.

## 4. Route and negative pins

- `execution/runner_windows_test.go`: `TestDefaultWindowsLauncherUsesRecorderHandoffCapability`
  installs a double on `attachSuspendedProcessWithHandoff` and asserts a complete recorder reaches it
  (sentinel error) while a capability-free invocation never does and completes. Base-red observed
  (the fork diverted the first invocation to the local route).
- `execution/linux_owner_v2_test.go`: `TestDefaultLinuxLauncherUsesRecorderHandoffCapability` drives
  `defaultProcessLauncher` with a complete recorder and non-file stdio; at HEAD the durable branch
  refuses with `Linux supervisor target stdio must be *os.File` before any helper spawn. Base-red
  observed (local route ran the process).
- `platform/no_test_binary_branch_test.go`: walks shipped (non-`_test.go`) files under
  `internal/platform` and `internal/execution` and fails on `flag.CommandLine.Lookup`,
  `flag.Lookup(`, `testing.Testing`, `test.v`, `isGoTestBinary`, or a `".test` suffix literal; a
  second pin walks the whole `daemon/` tree and allows the exported capability identifier only in
  its three defining files, and the unexported `installInProcessSupervisorRouteForTesting` only in
  its four caller/definition files. Both base-red observed; the fix stage added the two
  alternative self-inspection tokens, the installer-reference scan and the walk-sanity mutants
  (record §8).

## 5. Evidence plan

Contract gates with real output: `gofmt -l .`, `go vet ./...`, the three `GOOS` builds,
`git diff --check`; Windows suite `go test -count=1 -v` over `internal/platform/...`,
`internal/execution/...`, `internal/app/...`, `internal/harness/...`; WSL (Ubuntu-24.04, same
worktree) `-race` over the same set with SKIP lists; the frozen Windows witness
(`SYMMETRY_PRODUCTION_CONTAINMENT_WITNESS=1`, `-run '^TestProductionContainmentSupervisorReplaysReceiptAfterLostRecoverResponse$'`)
and `-run '^TestProductionLinuxContainment' ./e2e/` where the host can run it; the base-vs-HEAD truth
table of the handoff decision per recorder shape; the lesson-32 control-flow audit of every deleted
branch; the conversion table; mutants M1-M4; the dLOC line (D2, report-only). The CI `daemon` /
`daemon-windows` jobs remain the slow oracle.

## 6. Out of scope

The legacy generation deletion (W6-05, D3-blocked), the launcher/clone-bootstrap extraction (W6-06),
witness-config consolidation (W5-01, merged), platform dead-helper deletion (W5-09, merged), the
descendant-monitor state machine (W5-07, dropped), W4-22's recovery-seal write, `opencode/**`
(W6-04), `LinuxV2JournalCallbacks` (W4-23), the Windows pre-authority witness-hook consolidation
listed in `docs/TODO.md` under rf-w5-01 (not this slice's contract, although it names W6-03's files).
