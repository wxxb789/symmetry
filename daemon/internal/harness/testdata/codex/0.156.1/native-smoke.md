# Codex 0.156.1 native transport evidence

Owner directive 2026-09-24: track latest client. The tested pin moved from
Codex 0.153.4 to 0.156.1; the capability projection stays fail-closed.

Observed on 2026-09-24 at base revision
`ba9d7ee1175459dcbe9fb769170a659e3647da31` with an uncommitted working tree
(the version/schema constants and fixtures in this directory). Test:
`daemon/internal/harness/codex/adapter_native_test.go`,
`TestNativeTransportOpenAndClose`, unchanged from the base revision. SHA-256 of
the executed test-file bytes:
`f1b0cde9b098d358dd31b2df1e24a497d1f06bee64cec50860bf8d9efcf2f52f`.

| Environment | Result | Test duration |
| --- | --- | --- |
| Windows amd64 (10.0.26300), installed Codex CLI 0.156.1, Go 1.27.0 | FAIL: native `readOnly` grant | 1.74s-2.30s (4 runs) |
| Linux amd64 | not run for 0.156.1 | - |

Windows executable SHA-256:
`70bcb05f9bf1a4e7306edd0cd1b57d02af3267ad02a34b26f45c8c4bb20a3301`.
Generated v2 schema bundle:
`sha256:995fc3b8f8c469f6787e8fc5be4038c4f31359025edd8480b862e83355f3bf3b`,
computed with the production `SchemaDigest` code path and an isolated
`CODEX_HOME`.

## Reproduction

From `daemon/`, set `SYMMETRY_CODEX_NATIVE_SMOKE=1` and
`SYMMETRY_CODEX_NATIVE_SMOKE_EXECUTABLE` to the absolute pinned executable:

```text
go test ./internal/harness/codex -run '^TestNativeTransportOpenAndClose$' -count=1 -v -timeout 150s
```

The test isolates home/configuration and supplies no user credentials. It checks
the exact CLI version and generated protocol schema hash before opening a
session. It never submits a model turn.

## Observed Behavior

The version and schema gates passed. Process identity was persisted before
exposure. `Open` failed on all four runs with:

```text
unsupported harness capability: Codex thread/start granted sandbox "readOnly", want workspaceWrite
```

This matches the 0.153.4 Windows result. The raw frames in
`app-server-start-smoke.jsonl` show the same `jsonrpc` member omission.

The cleanup assertion also failed on all four runs:

```text
native Codex stop: terminated=true containment_error=false termination_error=false sink_error=false output_error=false output_truncated=false
```

The 0.153.4 record observed `OutputTruncated=true` at this barrier. This is not a
Codex version change. A scratch control that ran the same adapter `Start`, `Open`,
`Close` and `Wait` sequence against the official `rust-v0.153.4` Windows release
executable, at the same working tree, also observed `output_truncated=false`
after the same `readOnly` failure (2 runs each for 0.153.4 and 0.156.1). The
difference therefore comes from the shared runner or timing on this branch, not
from Codex. No Windows sandbox was automatically
enabled and no permission assertion was relaxed. The Windows test is a failure,
not a skip or an expected-pass test.

## Limits

Linux was not exercised for 0.156.1. No repository mutation, model response,
result/usage semantics, cancellation during work, retained resume, handoff or
Control durability was tested. Windows workspace write remains unavailable in
the tested isolated configuration. Goal 0006d remains incomplete.
