# Pi/Control E2E hardened current-run evidence

Observed on 2026-09-13 on Windows amd64. This receipt is bound to daemon
source revision `d1f95c40e9a51a246eb6a6163b5d56ca94b4ea25`.

## Invocation

```powershell
$env:SYMMETRY_PI_CONTROL_E2E = '1'
$env:SYMMETRY_PI_CONTROL_E2E_EXECUTABLE = 'C:\Users\lhan\AppData\Local\mise\installs\pi\0.85.1\pi.exe'
mise exec -- mix test test/symmetry_control/pi_control_e2e_test.exs --only pi_control_e2e
```

Result: `2 passed, 2 excluded` in `56.1s`.

## Provenance

| Item | Value |
| --- | --- |
| Pi version | `0.85.1` |
| Pi executable SHA-256 | `2d4d351da30bfe23a473032e66a571b238763565aa93754e74f4a939de13f195` |
| Pi E2E test SHA-256 | `efeef732a9d3aeeda4dfde9cc5add8ddf7f0f62f4fcb68718fd814a1b4a7421d` |
| DataCase SHA-256 | `0605b0e04762b7255cea8a867b7119e3c91ab29dabaeee7c1805557fe5413e19` |
| Source revision | `d1f95c40e9a51a246eb6a6163b5d56ca94b4ea25` |

## Scope

The run exercised the real Pi process through the production daemon and
Control/PostgreSQL path, including fresh admission, fenced claim state,
artifact delivery, terminal receipts, unknown usage accounting, non-accepting
settlement, fresh-daemon retained resume, runtime-epoch rotation, and session
lineage. The test uses a bounded SQL sandbox ownership timeout, raw journal
read handles that do not block Windows replacement renames, and identity-aware
idempotent daemon cleanup.

The upstream is deterministic numeric loopback. No model credentials or
external provider access were supplied. This evidence does not promote general
Pi capability, prove provider usage accounting, firewall or namespace
isolation, opaque-token portability, handoff, crash/power-loss recovery, or
completion of Goal 0006.
