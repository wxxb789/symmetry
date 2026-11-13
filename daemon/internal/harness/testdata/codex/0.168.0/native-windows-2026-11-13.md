# Codex 0.168.0 Windows native observation

Date: 2026-11-13. Platform: Windows 10.0.26300 x64. Scope: exact-binary
repository lifecycle, cancellation, artifact, usage counters and negative
permission evidence for the Symmetry Codex adapter.

## Exact binary

- Source: locally installed stable `codex-cli 0.168.0` (mise install; upstream
  tag `rust-v0.168.0`).
- `codex --version`: `codex-cli 0.168.0`
- `codex.exe` SHA-256:
  `ad8296f7d95eea56bc4bab33cef025eb000340a7252ddb83dd41041ebcc65d0b`
  (372737328 bytes)
- Generated v2 schema bundle: `sha256:893cc2379d96a7f72394b69abc2b40a5a7da039854b211c1137dcbd26f757afe`

Tests ran from `daemon/` at commit
`0ae6a94e2a1e9b05c0f917621dd4f4bb1a605f1e` (clean worktree, `git status` empty;
the 0.168.0 pin, the exact-blob promotion gate and this testdata are committed
there). The transport, repository-turn and cancellation smokes below were
re-executed at that Subject, so this observation binds both the exact Codex
binary and the reviewed adapter revision.

## Sandbox prerequisite

The host Codex Windows sandbox is configured (`~/.codex/.sandbox/setup_marker.json`,
version 5, offline/online sandbox users). With that sandbox-ready `CODEX_HOME`,
`thread/start` returns `sandbox.type=workspaceWrite` for the requested
`workspace-write` policy. Without it, an isolated home returns
`sandbox.type=readOnly` and `Open` fails with
`unsupported harness capability: Codex thread/start granted sandbox "readOnly",
want workspaceWrite`; that fail-closed negative case is required behavior, not a
regression.

## Version output warning

On this host `codex --version` prints one benign `WARNING:` line on **stderr**
before the clean stdout version. The bounded executor used by the probe reads
both streams, so the merged capture is:

```text
WARNING: proceeding, even though we could not create PATH aliases: Refusing to create helper binaries under temporary dir "<profile root>" (codex_home: AbsolutePathBuf("<profile root>\.codex"))
codex-cli 0.168.0
```

The alias helper is Codex's own PATH convenience binary; the daemon does not
rely on it, and the warning is not a compatibility signal. The version gate now
tolerates a bounded number (4) of exactly `WARNING:`-prefixed leading lines and
still requires the complete anchored `codex-cli X.Y.Z` line; any other leading
line, an excess of warnings, or a non-matching version line still fails the
parse. The same stderr warning appears on `app-server --help` and does not
affect the transport-help or schema gates.

Environment for every invocation below: `CODEX_HOME` = `SYMMETRY_CODEX_*_HOME`
= `SYMMETRY_CODEX_*`/`HOME`/`USERPROFILE` = `C:\Users\lhan\.codex`,
`SYMMETRY_CODEX_*_EXECUTABLE` =
`C:\Users\lhan\AppData\Local\mise\installs\codex\0.168.0\bin\codex.exe`.
Interpolation is by the isolated smoke environment; no secret is recorded here.

## Contained transport

```text
SYMMETRY_CODEX_NATIVE_SMOKE=1 go test ./internal/harness/codex \
  -run '^TestNativeTransportOpenAndClose$' -count=1 -v -timeout 240s
```

Result: PASS. `initialize`, `thread/start` and `Close` completed with
`terminated=true`, `containment_error=false`, `termination_error=false`,
and no dropped buffered output requirement. The exact 0.168.0 binary
regenerated the schema bundle and matched the pinned digest before transport
started.

## Credentialed repository turn

```text
SYMMETRY_CODEX_LIVE_SMOKE=1 \
SYMMETRY_CODEX_LIVE_SMOKE_MODEL=gpt-6.1-sol \
SYMMETRY_CODEX_LIVE_SMOKE_PROVIDER=custom \
  go test ./internal/harness/codex -run '^TestLocalCredentialedTurnSmoke$' \
    -count=1 -v -timeout 420s
```

Result: PASS at commit `0ae6a94` (43.64 s). The agent created the requested file and returned a
canonical `candidate_completion` TaskResult that passed `protocol.ParseTaskResult`:

```text
artifact hello.txt = "hello from codex"
"Kind": "succeeded"
"Semantic.kind": "candidate_completion"
"Semantic.subject": {"resource_id":"11111111-1111-4111-8111-111111111111",
                     "commit":"342b6d4c...","tree_digest":"sha256:8d76ef07..."}
"Semantic.subject_hash": "sha256:52df1f05..."
"Process": {"ExitCode":0,"Terminated":true,"ContainmentError":null,
            "TerminationError":null,"OutputError":null,"OutputTruncated":false}
"EventCount": 534
```

`thread/tokenUsage/updated` notifications normalized into `EventUsageObserved`
with cumulative `last`/`total` counters (final `last.total_tokens:30707`,
cumulative `total.input_tokens:206537`). The TaskResult `Usage` remains
`unknown`: cumulative native counters are not promoted as billable deltas.
Unknown 0.168.0 notifications (goal, queue, settings) remained
`unknown_native_event` diagnostics and never became success. The projected
strict `outputSchema` was accepted; the unprojected canonical schema was not
sent and the provider reported no schema rejection.

## Cancellation

```text
SYMMETRY_CODEX_LIVE_SMOKE=1 \
SYMMETRY_CODEX_LIVE_SMOKE_MODEL=gpt-6.1-sol \
SYMMETRY_CODEX_LIVE_SMOKE_PROVIDER=custom \
  go test ./internal/harness/codex -run '^TestLocalCredentialedCancelSmoke$' \
    -count=1 -v -timeout 300s
```

Result: PASS at commit `0ae6a94` (51.03 s). While a 180-second shell command was active, the
`cancel-1` control returned `outcome=applied` with
`"Codex turn interruption accepted; awaiting turn/completed"`; `Wait` returned
`kind=cancelled` with `reason=cancelled`, and the process remained contained
(`terminated=true`, `containment_error=false`, `termination_error=false`). The
cancelled run's buffered output was truncated (`output_truncated=true`); a
contained stop does not require dropping buffered output.

## Provider and model rehearsal (tencent-litellm)

A config-mirror home with `model_provider="tencent-litellm"`, `model` and
`review_model` set to `deepseek-v4.1-flash` (sandbox markers and provider
credential copied from the live home) ran the same turn smoke in 11.52 s
(PASS): `hello.txt` = `"hello from codex"`, canonical `candidate_completion`,
`containment_error=null`, `output_truncated=false`, schema accepted.
`model_context_window` was reported as 258400 by this provider.

The same home's cancel smoke failed before cancellation: the 20-second
cancellation boundary arrived after the turn had already reached a terminal
state (`Codex turn reached a terminal state before the pending RPC completed`,
`outcome=failed`), because this model did not execute the instructed 180-second
shell command. That is model compliance on the fixture prompt, not an adapter
defect; the cancel capability is evidenced by the `custom` provider run above,
which did execute the sleep. A faster model is usable for the turn smoke; the
cancel smoke still requires an instruction-following model that keeps the turn
running past the cancellation boundary.

## Capability decision

Exact-blob lifecycle evidence now exists at 0.168.0 for start, events, cancel,
result, artifact and cumulative-counter observation, so the Windows projection
promotes start/events/cancel for the resolved executable
`sha256:ad8296f7d95eea56bc4bab33cef025eb000340a7252ddb83dd41041ebcc65d0b`.
Any other blob stays fail-closed regardless of version or schema agreement.
Linux stays explicitly unverified: shell tool children detach into their own
session, so the Linux process-group owner cannot prove containment. The smallest
recorded amendment remains the existing Linux v2 cgroup owner (per-run cgroup
leaf, `cgroup.kill`).
