# Pi 0.85.1 Windows current-run evidence

Observed on 2026-09-12. This receipt records one Windows run of the real Pi
standalone executable against daemon source revision
`bab51bd4a918c563d526572590cb9aa4d5af64e5`. It is additional current-run
evidence; historical receipts remain unchanged.

## Binary provenance

| Item | Value |
| --- | --- |
| Pi version | `0.85.1` |
| Executable | `C:\Users\lhan\AppData\Local\mise\installs\pi\0.85.1\pi.exe` |
| SHA-256 | `2D4D351DA30BFE23A473032E66A571B238763565AA93754E74F4A939DE13F195` |
| Daemon source revision | `bab51bd4a918c563d526572590cb9aa4d5af64e5` |
| Platform | Windows |

The opt-in cancellation test file is additionally bound by SHA-256
`82BE529FD2290566365E8938FDD456A25C2B4126A6E9C4D24060B77CD796CC40`.

## Passing commands

The opt-in native tests used the executable above and the repository's local
loopback configuration. Each command completed successfully:

```text
go test ./internal/harness/pi -run '^TestNativeRPCOpenAndClose$' -count=1 -timeout=150s -v
PASS (6.53s)

go test ./internal/harness/pi -run '^TestNativeRepositoryTask$' -count=1 -timeout=150s -v
PASS (14.41s)

go test ./internal/harness/pi -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -timeout=5m -v
PASS (24.07s)

go test ./internal/harness/pi -run '^(TestNativeRPCOpenAndClose|TestNativeRepositoryTask|TestNativeRepositoryTaskRetainedResume)$' -count=1 -timeout=5m -v
PASS (49.297s combined run)

$env:SYMMETRY_PI_NATIVE_CANCELLATION='1'
$env:SYMMETRY_PI_NATIVE_CANCELLATION_EXECUTABLE='C:\Users\lhan\AppData\Local\mise\installs\pi\0.85.1\pi.exe'
go test ./internal/harness/pi -run '^TestNativeCancellationDrainsInFlightLoopbackRequest$' -count=3 -timeout=5m -v
PASS (7.42s / 7.59s / 7.67s; 24.386s combined)
```

The repository-work and retained-resume tests used the real binary, an
isolated temporary repository, and the deterministic loopback provider. The
loopback path is noncredentialed: it copies no user credential, reads no
global Pi configuration, and makes no upstream provider request.

## Scope and limitations

This receipt proves only the observed Windows native transport, one bounded
repository task, one retained-session restart, and one in-flight cancellation
drain for the exact binary and source revision above. The cancellation test's
`ControlCancel` call is accepted before `WaitTurn`; `WaitTurn` then drains the
native turn, while the subsequent `Wait` returns `ResultCancelled` with
`Semantic == nil` and unknown usage. It does not claim authenticated provider
execution, provider or usage accounting, crash or power-loss recovery,
handoff, or Control/PostgreSQL durability. It does not complete Goal 0006 or
promote any unverified capability.

## Cancellation evidence

The same Windows Pi 0.85.1 executable was also used for
`TestNativeCancellationDrainsInFlightLoopbackRequest`. The test ran against an
isolated, noncredentialed local `httptest` loopback endpoint and exercised the
native `clear_queue`/abort path while a request was in flight. Three runs
completed successfully:

```text
go test ./internal/harness/pi -run '^TestNativeCancellationDrainsInFlightLoopbackRequest$' -count=1 -timeout=5m -v
PASS (7.22s)
PASS (7.48s)
PASS (7.23s)
```

Each run observed `ControlApplied`, a `WaitTurn` `ResultCancelled`, no
semantic result, and `UsageUnknown`. The test also observed persisted native
PID and identity before cancellation and verified tasklist cleanup after the
native process terminated. The loopback endpoint was local and
noncredentialed; no user credential or upstream provider request was used.

This cancellation receipt does not prove provider or model usage accounting,
crash or power-loss recovery, handoff, or Control/PostgreSQL durability. It
does not complete Goal 0006 or promote any unverified capability.
