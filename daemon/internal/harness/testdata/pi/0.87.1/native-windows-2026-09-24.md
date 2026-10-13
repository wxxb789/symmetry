# Pi 0.87.1 Windows native opt-in evidence

Observed on 2026-09-24 on Windows 11 with `go1.27.0 windows/amd64`. The source
was an uncommitted working tree on top of
`ba9d7ee1175459dcbe9fb769170a659e3647da31`, with the 0.87.1 pin change and other
harness workers' uncommitted edits present. This record is not bound to a commit.
Rerun the tests against the final commit before citing them as receipts.

## Binary provenance

| Item | Value |
| --- | --- |
| Pi version | `0.87.1` (`pi --version`) |
| Executable | `C:\Users\<user>\AppData\Local\mise\installs\pi\0.87.1\pi.exe` |
| Executable SHA-256 | `dd5fdf61bdd10e3fa3fb3d7dbca1ce9f21475d4a7a410f2e0524fd19ae35651f` |
| Release asset | `v0.87.1` `pi-windows-x64.zip`, SHA-256 `aab2ba67baf8ff97a52d05b62d88e9e65a840c6ea8fa1029a28d62d210d4e5fc` (matches the release API digest) |

The `pi.exe` extracted from the release zip has the same SHA-256 as the
installed executable.

## Commands and outcomes

All commands ran from `daemon/`. None used a provider credential or made an
upstream request.

```text
SYMMETRY_PI_NATIVE_SMOKE=1
SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE=<executable above>
go test ./internal/harness/pi -run '^TestNativeRPCOpenAndClose$' -count=1 -timeout=150s -v
PASS (1.48s); logged platform=windows version=0.87.1

SYMMETRY_PI_PROVIDER_ACTION_SMOKE=1
SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE=<executable above>
go test -run '^TestNativePiProviderBridgeAction$' -count=1 -timeout=90s -v ./internal/harness/pi
PASS (1.29s)

SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE=1
SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE=<executable above>
(HOME, USERPROFILE and PI_CODING_AGENT_DIR pointed at a temporary directory; PI_OFFLINE=1)
go test ./internal/harness/pi -run '^(TestGeneratedProviderBridgeExtensionLoadsInNativePi|TestNativePiProviderBridgeLifecycle)$' -count=1 -timeout=150s -v
PASS (1.11s / 1.04s)

SYMMETRY_PI_NATIVE_CANCELLATION=1
SYMMETRY_PI_NATIVE_CANCELLATION_EXECUTABLE=<executable above>
go test ./internal/harness/pi -run '^TestNativeCancellationDrainsInFlightLoopbackRequest$' -count=3 -timeout=5m -v
PASS (1.88s / 1.93s / 1.90s)
```

The repository-task tests used the deterministic gateway from
`.symmetry/native-linux-tools/loopback-responses-gateway.go`, built locally and
bound to `127.0.0.1:41419` because port 4141 was held by an unrelated process.
The gateway has no upstream forwarding.

```text
SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_EXECUTABLE=<executable above>
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_PROVIDER=symmetry-native-loopback
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_MODEL=gpt-5.6-terra
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL=http://127.0.0.1:41419/v1
go test ./internal/harness/pi -run '^TestNativeRepositoryTask$' -count=1 -timeout=150s -v
FAIL (2.61s): "native Pi repository task process did not complete a bounded termination barrier"

go test ./internal/harness/pi -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -timeout=5m -v
FAIL (2.85s): "retained native Pi process did not complete a bounded termination barrier"
```

Both failures happen after the gateway served the tool call and the final
answer, and after `WaitTurn` and the semantic-result comparison passed. A
temporary diagnostic line, reverted afterwards, showed
`Terminated=true OutputTruncated=false` with nil sink, output, termination and
containment errors. The test requires `OutputTruncated=true`.

The same failure occurred in two runs with the 0.85.1 executable (the
`TestedVersion` constant was set back to `0.85.1` only for that run). It is
therefore not a 0.87.1 regression. It concerns the `OutputTruncated` barrier in
the shared execution runner on this branch, which this change does not touch.

## What this does not prove

- No Linux run against 0.87.1 is recorded.
- The repository-task and retained-resume paths are not re-established for
  0.87.1 until the `OutputTruncated` barrier failure is resolved.
- No authenticated provider, served model or effort, usage accounting, crash or
  power-loss recovery, handoff, or Control/PostgreSQL durability.
- The Control E2E (`pi_control_e2e_test.exs`) was not run.
- No capability is promoted: `Probe` still returns `ErrNativeUnverified`.

## Decoder change made for 0.87.1

pi 0.87.1 emits `entry_appended` from `_omitRecoveryAttempt` on every
auto-retry. The adapter decoder treated that as an unknown event and failed. The
decoder now allowlists the three documented state events (`entry_appended`,
`session_info_changed`, `thinking_level_changed`) as non-terminal records, and
`TestValidatorTreatsStateEventsAsNonTerminal` covers them. They play no part in
settlement. No native run in this record exercised a provider retry.
