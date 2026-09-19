# Pi/Control E2E current-run evidence

Observed on 2026-09-13 on Windows amd64. This receipt is bound to daemon
source revision `89f52cb4b77448b5c53ee76858445508a45d09be` and records the
opt-in Pi/Control integration test after that revision was committed.

## Invocation

```powershell
$env:SYMMETRY_PI_CONTROL_E2E = '1'
$env:SYMMETRY_PI_CONTROL_E2E_EXECUTABLE = 'C:\Users\lhan\AppData\Local\mise\installs\pi\0.85.1\pi.exe'
mise exec -- mix test test/symmetry_control/pi_control_e2e_test.exs --only pi_control_e2e
```

Result: `2 passed` in `113.4s`.

## Provenance

| Item | Value |
| --- | --- |
| Pi version | `0.85.1` |
| Pi executable SHA-256 | `2d4d351da30bfe23a473032e66a571b238763565aa93754e74f4a939de13f195` |
| Control E2E test SHA-256 | `27690f8134edaefca05da7c02f22ebea3b06f8ba2bb08121e771cf90b0e9ef58` |
| Witness SHA-256 | `5c153854767e16387db081d38a623eb84a2683a989b829546487a9e1df88b566` |
| Daemon app SHA-256 | `c3c1a6e175d62e2b97d7d0f5aa6dc49267ccc2185a56269f9e6ed55530ce7102` |
| Source revision | `89f52cb4b77448b5c53ee76858445508a45d09be` |

## Observed scope

The test exercised a real Pi 0.85.1 process through the production daemon
execution path and Control/PostgreSQL receipts. It checked fresh admission,
claim and fenced run state, durable process/journal identity, artifact write,
terminal attach/stop receipts, `UsageUnknown`, settlement without Goal
achievement, and a fresh daemon retained-resume run with a rotated runtime
epoch and binding lineage.

The upstream endpoint was a deterministic numeric loopback fixture. No model
credentials or external provider access were supplied. The test-only witness
is enabled only by the `symmetry_pi_control_e2e` build tag and the explicit
opt-in environment variable.

This is integration evidence, not credentialed provider evidence. It does not
promote general Pi native capability, prove provider usage accounting, firewall
or network-namespace isolation, native opaque-token portability, handoff, crash
or power-loss recovery, or completion of Goal 0006.
