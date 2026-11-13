# Codex 0.167.2 Windows native observation

Date: 2026-11-12. Platform: Windows 10.0.26300 x64. Scope: exact-binary
repository lifecycle, cancellation, artifact, usage counters and negative
permission evidence for the Symmetry Codex adapter.

## Exact binary

- Source: locally installed stable `codex-cli 0.167.2` (mise install; upstream
  tag `rust-v0.167.2`).
- `codex --version`: `codex-cli 0.167.2`
- `codex.exe` SHA-256:
  `6f65c42e3769209976cce097a400720b5d877e1d24ec8bb6272bf0f02d5e0fd7`
- Generated v2 schema bundle: `sha256:f2576ca5d46be4cfb159906264fbc173efcfcc917ea8fdc38323770e7b657b5a`
  (same digest as 0.167.1).

Tests ran from `daemon/` against the working tree based on
`7cb5f5811ff0b807b61e823c20a03471891d5f4c`. This observation is historical:
the 0.167.2 projection it promoted was superseded by the 0.168.0 pin and the
exact-blob gate, so no PR may cite this note as current-Subject evidence.

## Sandbox prerequisite

The prior 0.167.1 observation requested `workspace-write` and received
`readOnly`. That was not a Codex refusal of writes: an isolated `CODEX_HOME`
reports `windowsSandbox/readiness = notConfigured` and the server downgrades to
read-only. A host where the Codex Windows sandbox is configured reports
`ready`, grants `workspaceWrite`, and enforces it:

- `command/exec` with `workspaceWrite` wrote `inside.txt` in the workspace and
  failed with `Access is denied` writing outside it.
- The adapter still rejects any `thread/start` response whose sandbox type is
  not `workspaceWrite`; the isolated-home rejection is the expected negative
  case.

## Contained transport

```text
SYMMETRY_CODEX_NATIVE_SMOKE=1 \
SYMMETRY_CODEX_NATIVE_SMOKE_HOME=<sandbox-ready CODEX_HOME> \
SYMMETRY_CODEX_NATIVE_SMOKE_EXECUTABLE=<absolute>/codex.exe \
  go test ./internal/harness/codex -run '^TestNativeTransportOpenAndClose$' \
    -count=1 -v -timeout 180s
```

Result: PASS. `initialize`, `thread/start` and `Close` completed with
`terminated=true`, `containment_error=false`, `termination_error=false`,
`output_error=false`. After the confirmed close, a repeated `Open` and a new
`StartTurn` both failed closed (`errNativeSessionClosed`); a closed or stale
native session is never reused.

Without `SYMMETRY_CODEX_NATIVE_SMOKE_HOME` the isolated home returns
`sandbox.type=readOnly` and `Open` fails with:

```text
unsupported harness capability: Codex thread/start granted sandbox "readOnly", want workspaceWrite
```

That fail-closed negative case is required behavior, not a regression.

## Credentialed repository turn

```text
SYMMETRY_CODEX_LIVE_SMOKE=1 \
SYMMETRY_CODEX_LIVE_SMOKE_HOME=<local CODEX_HOME> \
SYMMETRY_CODEX_LIVE_SMOKE_EXECUTABLE=<absolute>/codex.exe \
SYMMETRY_CODEX_LIVE_SMOKE_MODEL=gpt-6.1-sol \
SYMMETRY_CODEX_LIVE_SMOKE_PROVIDER=custom \
  go test ./internal/harness/codex -run '^TestLocalCredentialedTurnSmoke$' \
    -count=1 -v -timeout 420s
```

Result: PASS (60.5s in the final run). The agent created the requested file, then returned a
canonical `candidate_completion` TaskResult that passed `protocol.ParseTaskResult`:

```text
artifact hello.txt = "hello from codex"
"Kind": "succeeded"
"Semantic.kind": "candidate_completion"
"Semantic.subject": {"resource_id":"11111111-1111-4111-8111-111111111111",
                     "commit":"40d671df...","tree_digest":"sha256:8d76ef07..."}
"Semantic.subject_hash": "sha256:838c9458..."
"Process": {"ExitCode":0,"Terminated":true,"ContainmentError":null,
            "TerminationError":null,"OutputError":null}
```

`thread/tokenUsage/updated` notifications normalized into
`EventUsageObserved` with cumulative `last`/`total` counters (for example
`total_tokens:30776` then `31261` and later updates). The TaskResult `Usage`
remains `unknown`: cumulative native counters are not promoted as billable
deltas.

## Cancellation

```text
go test ./internal/harness/codex -run '^TestLocalCredentialedCancelSmoke$' \
  -count=1 -v -timeout 300s
```

While a 180-second shell command was active, `Control(cancel)` returned
`outcome=applied`; `turn/interrupt` produced `turn/completed` with status
`interrupted`, `Wait` returned `kind=cancelled` with `reason=cancelled`, and the
process remained contained (`containment_error=false`).

## Attribute boundaries observed

- `turn/start` accepted the projected strict `outputSchema`; the unprojected
  canonical schema was rejected by the provider (`const` without `type`,
  `uniqueItems`, `oneOf`, required/additionalProperties strictness).
- Unknown 0.167.2 notifications (for example goal, queue and settings events)
  remained `unknown_native_event` diagnostics and never became success.
- No native resume, steer, pause, approval response, provider broker or hard
  cost limit is claimed.
