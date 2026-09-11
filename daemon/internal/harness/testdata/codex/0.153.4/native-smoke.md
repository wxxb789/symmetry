# Codex 0.153.4 native transport evidence

Observed on 2026-09-11 at source revision
`09b5eb32e99d8ff9e74c9874dfb400339db8a929`. The tracked source was clean during
these native runs. Test: `daemon/internal/harness/codex/adapter_native_test.go`,
`TestNativeTransportOpenAndClose`. SHA-256 of the executed test-file bytes:
`923f414312104174f9e081b20d579c429258ff19430ff3177fe44506c78375df`.

| Environment | Result | Test duration |
| --- | --- | --- |
| Windows amd64, installed Codex CLI 0.153.4 | FAIL: native `readOnly` grant | 7.78s |
| Linux amd64, Docker `golang:1.27.0-bookworm`, Codex CLI 0.153.4 | PASS | 0.50s |

Windows executable SHA-256:
`ccdc9eb9dd71fbcfb03ad42c4eca2b0d6ff6fbd32ebe9416550e6244561e559b`.
Linux release archive SHA-256:
`f479424eca092484dc40d87ae28c44f4cc40234a60045d6131e493800d814a30`.
Linux image ID:
`sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452`.

## Reproduction

From `daemon/`, set `SYMMETRY_CODEX_NATIVE_SMOKE=1` and
`SYMMETRY_CODEX_NATIVE_SMOKE_EXECUTABLE` to the absolute pinned executable:

```text
go test ./internal/harness/codex -run '^TestNativeTransportOpenAndClose$' -count=1 -v -timeout 150s
```

Linux used the repository mounted at `/repo`, working directory `/repo/daemon`,
and the release executable extracted into the disposable container's `/tmp`.
The test isolates home/configuration and supplies no user credentials. It checks
the exact CLI version and generated protocol schema hash before opening a
session. It never submits a model turn.

## Observed Behavior

Linux passed process-identity persistence ordering, `Open` and replay identity,
workspace-only permission readback, bounded `Close/Wait`, and repeated `Close`.
The no-turn result was `cancelled/missing_result` without a semantic result.

Windows reached `Open` and failed with:

```text
unsupported harness capability: Codex thread/start granted sandbox "readOnly", want workspaceWrite
```

The Windows cleanup assertion passed. Both platforms observed
`OutputTruncated=true` at the explicit termination barrier, with no sink,
output, containment or termination error. This is not a complete output drain.
No Windows sandbox was automatically enabled and no permission assertion was
relaxed. The Windows test is a failure, not a skip or an expected-pass test.

## Limits

No repository mutation, model response, result/usage semantics, cancellation
during work, retained resume, handoff or Control durability was tested. Linux
transport success does not advertise native work support. Windows workspace
write remains unavailable in the tested isolated configuration. Windows symbolic
links, native UNC and case-sensitive SMB paths were not exercised; directory
resolution remains a validation-time snapshot, not a TOCTOU guarantee. Goal 0006
remains incomplete.
