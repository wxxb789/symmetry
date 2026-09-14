# Windows OpenCode CI Gate - 2026-09-14

## Source Subject

- Source commit: `09662c3`
- Scope: `.github/workflows/ci.yml`

This source adds `opencode-native-synthetic-windows` and allocates a verified
ephemeral IPv4 loopback PostgreSQL port to the Windows Pi job. The job records
the port, pinned binary provenance, and synthetic upstream scope.

## Local Verification

On Windows, the installed executable
`C:\Users\lhan\AppData\Local\mise\installs\opencode\1.18.30\opencode.exe`
reported version `1.18.30`. With
`SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL=1` and that executable set in
`SYMMETRY_OPENCODE_NATIVE_SYNTHETIC_TOOL_EXECUTABLE`, this command passed:

```text
go test -count=1 -timeout 180s -v ./internal/harness/opencode -run ^TestNativeSyntheticGatewayToolIntegration$
```

The test completed in `20.960s` (measured command duration `22.486s`). The
workflow YAML, all 11 PowerShell `run` blocks, static workflow assertions, and
`git diff --check` also passed before the source commit.

## Limits

The test uses a synthetic numeric IPv4 loopback provider without credentials.
It does not prove a hosted Windows runner, a real provider operation, provider
accounting, or Goal completion. Hosted Windows evidence requires an authorized
push and CI execution.
