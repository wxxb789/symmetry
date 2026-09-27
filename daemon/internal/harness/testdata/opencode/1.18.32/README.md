# OpenCode 1.18.32 private protocol fixtures

Owner directive 2026-09-24: track latest client. These synthetic fixtures model
the OpenCode `serve` API at version `1.18.32` (upstream tag `v1.18.32`, commit
`545f51d26cc39a907d2867492d498d9607ea5fa4`). They moved unchanged from
[`../1.18.30/`](../1.18.30/README.md) after source verification:
`packages/sdk/openapi.json` is byte-identical at `v1.18.30` and `v1.18.32`, and
the `v1.18.30...v1.18.32` compare touches no session event definition, v2
session HTTP handler, or SDK schema (only ACP, TUI, console, stats, an
attachment-support check in `session/message-v2.ts`, and a config error
mapping). They exercise HTTP identity, SSE framing/cursor validation, and
strict source-derived durable event decoding only.

The lifecycle fixtures (`lifecycle-terminal.sse`, `lifecycle-tool-retry.sse`,
`unknown-then-terminal.sse`, and `missing-result.sse`) are synthetic sequences
derived from the pinned upstream event definitions. They are not captures from
a live model, do not prove terminal-result behavior, and must not promote any
OpenCode capability.

The fixture files do not capture provider credentials, model prompts, native
lifecycle, terminal results, usage, resume, permissions, artifact recovery, or
shutdown. They do not establish a supported Symmetry OpenCode adapter.

`version.txt` records the release tag version (`packages/opencode/package.json`
at `v1.18.32`). An isolated `opencode --version` run printed the same value;
see the native record below.

## Release provenance

Official release assets, SHA-256 checked against the GitHub release digest:

- `opencode-windows-x64-baseline.zip`:
  `cd852831bd094c2df2eb379eb98bed7a63db7f823a7caf277c732cdac33cbdb6`;
  contains only `opencode.exe`, SHA-256
  `da86eed515d91a7b2d7da9a8230a2bd095f68a89f0cf44eb6a9217bead81fffc`.
- `opencode-linux-x64-baseline.tar.gz`:
  `763af386ef88a8cab18df00fcf055690e5a55e31a7088beabe02307142a6adce`;
  contains only `opencode`, SHA-256
  `513f500a1a5ea1dc7d865547ac87b32a8936334e8d5abd5b3ff585c45a170080`.

## Native checks

Windows loopback-only native runs of 1.18.32 are recorded in
[`native-windows-2026-09-24.md`](native-windows-2026-09-24.md). No Linux run is
recorded yet. The opt-in
commands are unchanged. From `daemon/`, set `SYMMETRY_OPENCODE_NATIVE_SMOKE=1`
and `SYMMETRY_OPENCODE_NATIVE_SMOKE_EXECUTABLE` to the absolute binary path, then
run:

```text
go test -count=1 -timeout 120s -run '^TestNativeTransportOpenAndClose$' -v ./internal/harness/opencode
```
