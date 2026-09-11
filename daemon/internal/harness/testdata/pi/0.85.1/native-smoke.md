# Pi 0.85.1 native RPC transport evidence

Observed on 2026-09-11. Production code baseline: `d450cc7`.
Test: `daemon/internal/harness/pi/adapter_native_test.go`,
`TestNativeRPCOpenAndClose`. SHA-256 of the test's working-tree bytes used on
both platforms:
`939cb9899fc05fd8bfc9b7c8f04562ae3fd9670af1ba6f8582c21359f8fff8a3`.

| Environment | Result | Test duration |
| --- | --- | --- |
| Windows, installed mise Pi 0.85.1 standalone binary | PASS | 7.05s |
| Linux amd64, Docker `golang:1.27.0-bookworm`, upstream Pi 0.85.1 standalone binary | PASS | 1.22s |

Windows executable SHA-256:
`2d4d351da30bfe23a473032e66a571b238763565aa93754e74f4a939de13f195`.
Linux archive: upstream release `v0.85.1`, asset `pi-linux-x64.tar.gz`.
Its SHA-256 was checked against the release API's asset digest before extraction:
`494e498f47d74d21f40b3386f6a5e921a3d49531a169cab55bbdaca0ea1fe25a`.
Linux test image ID:
`sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452`.

## Reproduction

From `daemon/`, set `SYMMETRY_PI_NATIVE_SMOKE=1` and
`SYMMETRY_PI_NATIVE_SMOKE_EXECUTABLE` to the absolute path of the standalone
Pi executable. Then run:

```text
go test ./internal/harness/pi -run '^TestNativeRPCOpenAndClose$' -count=1 -timeout=120s -v
```

The Linux execution used the same working tree mounted read-only in the image
above, with the verified release archive extracted to `/tmp/pi` and the
executable variable set to `/tmp/pi/pi`. The ordinary test suite skips this
test unless explicitly enabled. Once enabled, missing or incompatible binaries
fail the test rather than skipping it.

## Proven Scope

The real binary is launched through the production adapter and execution runner.
The test checks the process-identity callback before session exposure, a decoded
native frame during `Open/get_state`, a complete local handle with a filename
under the isolated session directory, stable `Open` replay, and bounded
`Close/Wait` with no semantic success. No work, tool, approval or usage event is
accepted during this sequence.

Pi receives an isolated home/configuration/session environment, no inherited
provider credentials, and explicit offline/resource-disabling options. Model
selection initializes the native session; the test sends no prompt and never
calls `StartTurn`. The callback check proves invocation ordering, not fsync or
Control attachment durability. A pre-turn session file need not exist yet.

An independent review identified two test gaps: counting frames across the whole
lifecycle, and skipping `Wait` after a cleanup error. Both were corrected and
reviewed; the platform results above are the subsequent runs.

## Still Unverified

This evidence does not prove authentication, a model turn, repository changes,
structured task results, native abort during a turn, usage accounting, retained
resume, handoff, or daemon/Control/PostgreSQL recovery. Those requirements remain
open under Goal 0006. The adapter's release capabilities remain unverified;
this transport smoke does not enable `Start`, `Resume` or other operations.
