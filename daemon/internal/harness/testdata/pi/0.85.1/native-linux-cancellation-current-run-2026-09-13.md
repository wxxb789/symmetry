# Pi 0.85.1 Linux cancellation current-run evidence

Observed on 2026-09-13. This receipt records the Linux cancellation test at
daemon source revision `00a9e9b38075bfd7cfbb9fa684e75bdf7167f277`.

## Provenance and isolation

| Item | Value |
| --- | --- |
| Pi release archive | Pi `0.85.1` Linux archive |
| Pi archive SHA-256 | `494e498f47d74d21f40b3386f6a5e921a3d49531a169cab55bbdaca0ea1fe25a` |
| Container | Ephemeral `golang:1.27`, `linux/amd64` |
| Container flags | `--rm --init --network none` |
| Daemon source revision | `00a9e9b38075bfd7cfbb9fa684e75bdf7167f277` |
| Test source SHA-256 | `c32818c09867d085b7c613a3b944f5cb24d17dcdb5847dceb29c1107b059a944` |

The test used the real Pi 0.85.1 binary from the verified archive. The
loopback endpoint was local to the isolated container, no credentials were
provided, and no host or upstream network access was available.

## Passing test

Test: `TestNativeCancellationDrainsInFlightLoopbackRequest`.

```text
SYMMETRY_PI_NATIVE_CANCELLATION=1
SYMMETRY_PI_NATIVE_CANCELLATION_EXECUTABLE=/tmp/pi/pi
go test ./internal/harness/pi -run '^TestNativeCancellationDrainsInFlightLoopbackRequest$' -count=1 -timeout=150s -v
PASS (3.74s)

SYMMETRY_PI_NATIVE_CANCELLATION=1
SYMMETRY_PI_NATIVE_CANCELLATION_EXECUTABLE=/tmp/pi/pi
go test -race ./internal/harness/pi -run '^TestNativeCancellationDrainsInFlightLoopbackRequest$' -count=1 -timeout=150s -v
PASS (2.98s)
```

The Linux invocations ran from a disposable `golang:1.27` container after
extracting the pinned archive to `/tmp/pi`; the source tree, archive and module
cache were mounted read-only. The Windows rerun used
`C:\Users\lhan\AppData\Local\mise\installs\pi\0.85.1\pi.exe` with the same
two environment variables and the same focused test.

The test now supports Linux and Windows. On Linux it also verifies native PID
absence through `/proc` after cleanup. The observed cancellation semantics
include native abort through `clear_queue`, `ControlApplied`, a
`ResultCancelled` `WaitTurn`, no semantic result, and `UsageUnknown`.

## Limitations

This receipt does not prove real provider or model execution, usage
accounting, crash or power-loss recovery, handoff, or Control/PostgreSQL
durability. It does not complete Goal 0006 or promote any unverified
capability.
