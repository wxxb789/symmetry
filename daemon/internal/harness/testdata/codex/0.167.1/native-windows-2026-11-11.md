# Codex 0.167.1 Windows native observation

Date: 2026-11-11. Scope: Windows x64 transport gate only. This is a fail-closed
observation, not native capability evidence.

## Exact binary

- Release asset: `codex-x86_64-pc-windows-msvc.exe.zip` from
  `https://github.com/openai/codex/releases/download/rust-v0.167.1/`
- `codex-x86_64-pc-windows-msvc.exe` SHA-256:
  `c12aca02de615a9fcb76f955bf005a33f4b10b577f8a1730161953f57a1155b3`
- `codex --version`: `codex-cli 0.167.1`

## Result

Source revision `a04c4ab02d1a6a35aa682c5b845133c720d3be61` with the local
0.167.1 pin applied.

```text
SYMMETRY_CODEX_NATIVE_SMOKE=1 \
SYMMETRY_CODEX_NATIVE_SMOKE_EXECUTABLE=<absolute>/codex.exe \
  go test ./internal/harness/codex -run '^TestNativeTransportOpenAndClose$' \
    -count=1 -v -timeout 120s

native Codex transport smoke: platform=windows version=0.167.1
open isolated native Codex session failed: unsupported harness capability:
Codex thread/start granted sandbox "readOnly", want workspaceWrite
```

`initialize` and the 0.167.1 schema digest passed. `thread/start` returned
`sandbox.type=readOnly` for the requested `workspace-write`, so the adapter
rejected the session. The raw start capture is
`app-server-start-smoke.jsonl`. Repository work, approvals, resume and
credentialed provider evidence remain unsupported.
