# OpenCode 1.18.30 synthetic tool current-run evidence

Observed on 2026-09-13. This receipt records bounded native-process evidence
for daemon source revision
`00a9e9b38075bfd7cfbb9fa684e75bdf7167f277`. The interaction used a
deterministic local loopback gateway and synthetic upstream responses; it is
not evidence of a real provider or model execution.

## Provenance and isolation

| Item | Value |
| --- | --- |
| OpenCode version | `1.18.30` |
| Windows executable | `C:\Users\lhan\AppData\Local\mise\installs\opencode\1.18.30\opencode.exe` |
| Windows platform | Windows amd64 |
| Linux archive | `opencode-linux-x64-baseline.tar.gz` |
| Linux archive SHA-256 | `60c92147d0d86ca606dda8a77260d3c87e0ef959eb2d8dbffb34df6d8a64e063` |
| Linux container | `golang:1.27`, Go `1.27.1`, `linux/amd64`, `--rm --init --network none` |
| Source revision | `00a9e9b38075bfd7cfbb9fa684e75bdf7167f277` |

The Windows run used the real installed OpenCode executable with isolated V2
`OPENCODE_CONFIG` catalog data and a temporary repository. The gateway was a
local loopback Chat Completions endpoint. The Linux run used a clean Git
archive in an ephemeral container with no network and no published host port.
Neither run supplied user credentials or allowed provider egress.

## Passing commands

The opt-in test used the production adapter and completed the synthetic tool
exchange, wrote the expected artifact, and stopped the native process within
the bounded test lifecycle:

```text
go test ./internal/harness/opencode -run '^TestNativeSyntheticGatewayToolIntegration$' -count=1 -timeout=120s -v
PASS (19.34s test; 19.661s package, Windows)

go test -race ./internal/harness/opencode -run '^TestNativeSyntheticGatewayToolIntegration$' -count=1 -timeout=120s -v
PASS (7.44s test; 8.491s package, Linux)
```

The Windows command used the actual executable at the path above. The Linux
command ran against the verified OpenCode release archive and the clean source
archive described above.

## Scope and limitations

This receipt proves only the observed native OpenCode process, local synthetic
Chat Completions tool exchange, artifact write, and bounded clean stop for the
exact source revision and binaries listed above. The upstream response was
synthetic and deterministic. The test deliberately remains fail-closed and
reports `ResultUnknown`; it does not establish a terminal success or failure
mapping.

It does not prove real-model terminal or result semantics, provider or model
usage, authenticated provider execution, Symmetry resume or crash recovery,
handoff, permissions, Control or PostgreSQL durability, or Goal acceptance.
It must not be used to claim supported OpenCode capability or completion of
Goal 0006.
