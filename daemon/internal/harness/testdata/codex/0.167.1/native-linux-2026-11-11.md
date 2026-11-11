# Codex 0.167.1 Linux native observation

Date: 2026-11-11. Scope: Ubuntu 24.04 on WSL2 x86_64, transport gate only. This
is a fail-closed observation, not native capability evidence.

## Exact binary

- Release asset: `codex-x86_64-unknown-linux-musl.tar.gz` from
  `https://github.com/openai/codex/releases/download/rust-v0.167.1/`
- `codex-x86_64-unknown-linux-musl` SHA-256:
  `dd5bfdf19d794dcc6fb482d2d5a2d9fb31d11f6caa44b303bb72890d461e8a02`
- `codex --version`: `codex-cli 0.167.1`

## Result

Source revision `a04c4ab02d1a6a35aa682c5b845133c720d3be61` with the local
0.167.1 pin applied.

```text
SYMMETRY_CODEX_NATIVE_SMOKE=1 \
SYMMETRY_CODEX_NATIVE_SMOKE_EXECUTABLE=<absolute>/codex \
  go test ./internal/harness/codex -run '^TestNativeTransportOpenAndClose$' \
    -count=1 -v -timeout 120s

native Codex transport smoke: platform=linux version=0.167.1
close native Codex session failed
cleanup native Codex session failed
native Codex stop: terminated=true containment_error=true termination_error=true
output_truncated=true
```

Unlike Windows, Linux `thread/start` did not reject the requested
`workspace-write` grant before close. The process could not be closed with
containment evidence, so the session remains failed and no capability is
promoted. Repository work, approvals, resume and provider accounting remain
unsupported.
