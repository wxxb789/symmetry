# OpenCode 1.18.32 native Windows checks, 2026-09-24

Owner directive 2026-09-24: track latest client.

## Binary provenance

| Item | Value |
| --- | --- |
| Release | `v1.18.32`, published 2026-09-21T22:51:20Z, commit `545f51d26cc39a907d2867492d498d9607ea5fa4` |
| Archive | `opencode-windows-x64-baseline.zip` |
| Archive SHA-256 (local, equals GitHub release digest) | `cd852831bd094c2df2eb379eb98bed7a63db7f823a7caf277c732cdac33cbdb6` |
| Archive layout | single entry `opencode.exe`, 180133928 bytes |
| Executable SHA-256 | `da86eed515d91a7b2d7da9a8230a2bd095f68a89f0cf44eb6a9217bead81fffc` |
| `--version` (isolated HOME/XDG dirs) | `1.18.32` |
| Local path | `Q:\repos\symmetry\.tmp\harness-upgrade\opencode\win\opencode.exe` (gitignored scratch) |

Host: Windows 11 Enterprise 10.0.26300, go1.27.0 windows/amd64. The working
tree was uncommitted on top of `4fa39d8`. Rerun on the final commit before
citing these results as receipts.

## Outcomes

All runs used loopback gateways only. No provider credential was used and
nothing was spent.

| Test | Env gate | Outcome |
| --- | --- | --- |
| `TestNativeTransportOpenAndClose` | `SYMMETRY_OPENCODE_NATIVE_SMOKE=1`, `SYMMETRY_OPENCODE_NATIVE_SMOKE_EXECUTABLE` | PASS (7.99 s), `platform=windows version=1.18.32` |
| `TestNativeSyntheticGatewayToolIntegration` | `SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL=1`, `SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL_EXECUTABLE` | PASS (13.27 s) |
| `TestNativePromptAdmissionReplaysAfterPost` | `SYMMETRY_OPENCODE_NATIVE_ADMISSION_REPLAY=1`, `SYMMETRY_OPENCODE_NATIVE_ADMISSION_REPLAY_EXECUTABLE` | PASS (8.98 s) |
| `TestNativeOpenCodeTerminalCandidateWitness` | `SYMMETRY_OPENCODE_NATIVE_TERMINAL_CANDIDATE=1` (default pinned executable above) | FAIL, see below. Subtests `terminal_stop`, `provider_error`, `step_ended_continuation`, `step_ended_failure` and `truncated_provider_eof` PASS. |

### `interrupt` failure is not caused by 1.18.32

The `interrupt` subtest fails with `want exactly ResultCancelled without
semantic result`. The observed result is `Kind:unknown`, `Terminated:true`,
`OutputTruncated:false`, `ExitCode:1`.

A control run on the same host used the pre-upgrade commit `56f7139`, the
pinned 1.18.30 executable
(`c1bdbb18767048e1853af4238311b3f7e16ff2f91b68fb4c7ced3c5175347eea`) and a
temporary worktree. It failed the same subtest the same way, and the other
five subtests passed. The failure therefore predates the version move. The
Codex and pi native runs on this branch report the same `OutputTruncated`
difference. The shared execution runner owns this; it is not an OpenCode
version regression.

## Limits

These results do not prove any terminal result, usage, resume, permission or
artifact-recovery semantics. They were not run on Linux. No capability is
promoted: the probe still returns `ErrNativeUnverified`.
