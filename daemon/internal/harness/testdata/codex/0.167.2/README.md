# Codex 0.167.2 probe evidence

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client. This directory supersedes `../0.167.1/` and is the tested
Codex version. The earlier 0.153.4, 0.156.1 and 0.167.1 observations remain
historical only.

- `version.txt` is `codex-cli 0.167.2`.
- `app-server-help.txt` is the sanitized `codex app-server --help` output. It
  advertises `--stdio` / `stdio://` as the default transport.
- `schema-manifest.json` records SHA-256 hashes from the locally generated
  0.167.2 schema bundle and method schemas. The v2 bundle is byte-identical to
  0.167.1 (`sha256:f2576ca5...`) and remains the probe gate.
- `app-server-start-smoke.jsonl` is a sanitized Windows observation of
  `initialize`, `initialized` and `thread/start` with the exact adapter
  parameters. With a sandbox-ready `CODEX_HOME`, `thread/start` returns
  `sandbox.type=workspaceWrite`, `networkAccess=false` and no extra writable
  roots. Paths, identifiers and installation metadata were redacted.
- `app-server-lifecycle.jsonl` and `frames.jsonl` are normalized synthetic
  framing fixtures moved from `../0.167.1/`. The generated v2 schema bundle is
  byte-identical, so the method names and DTO shapes they model are unchanged.
  They are not a raw 0.167.2 turn capture.
- [Windows native observation](native-windows-2026-11-12.md) records the exact
  binary, the credentialed repository turn, cancellation, artifact, cumulative
  token counters, and the isolated-home read-only rejection.
- [Linux native observation](native-linux-2026-11-12.md) records the exact
  binary, the contained transport open/close, and the documented reason Linux
  repository work remains unverified.

## Native output schema projection

Codex 0.167.2 forwards `outputSchema` to strict provider structured outputs,
which reject the canonical contract schema unchanged: untyped `const`,
`uniqueItems`, `oneOf`, unreachable definitions and schema-valued
`additionalProperties` are not accepted. The adapter now projects the canonical
TaskResult schema into the strict subset (reachable `$defs`, typed `const`/`enum`,
optional properties required and nullable) and prunes strict-mode null
placeholders for optional fields before canonical validation. The canonical
envelope still validates every result; the projection never weakens admission.

## Capability map for this pin

| Operation | Windows | Linux |
| --- | --- | --- |
| start / events / cancel / result / artifact | verified | unverified |
| resume, handoff | unsupported | unsupported |
| native steer, pause, approval response | unsupported | unsupported |
| billable usage accounting | unsupported (cumulative counters only) | unsupported |
| provider broker, hard cost limit | unsupported | unsupported |

The probe promotes verified capabilities only on `windows`. Linux stays
fail-closed until Codex runs under the Linux v2 cgroup owner; see the Linux
observation for the exact conflict.
