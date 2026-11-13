# Codex 0.168.0 probe evidence

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client. This directory supersedes `../0.167.2/` and is the tested
Codex version. The earlier `0.153.4`, `0.156.1`, `0.167.1` and `0.167.2`
observations remain historical only.

Scope: version/help capture, generated v2 schema digests, and the exact
executable blob binding. Windows start/events/cancel are verified for the exact
tested blob; Linux stays fail-closed.

- `version.txt` is `codex-cli 0.168.0`.
- `app-server-help.txt` is the sanitized `codex app-server --help` output. It
  advertises `--stdio` / `stdio://` as the default transport.
- `schema-manifest.json` records SHA-256 hashes from the locally generated
  0.168.0 schema bundle, method schemas, the exact executable blob, and the
  observed wire constraints. The v2 bundle changed from 0.167.2; the
  `InitializeParams`, `ThreadStartParams`, `TurnStartParams` and
  `TurnInterruptParams` method schemas are byte-identical to 0.167.1/0.167.2.
- `app-server-start-smoke.jsonl`, `app-server-lifecycle.jsonl` and
  `frames.jsonl` are normalized synthetic framing fixtures moved from
  `../0.167.2/`. The adapter request/response shapes they model are unchanged
  because the method schemas are byte-identical to 0.167.2; they are not raw
  0.168.0 captures.

## Exact-binary binding

Promotion now requires the resolved executable bytes to match the tested blob,
not only the version string and schema digest:

- Tested executable SHA-256:
  `sha256:ad8296f7d95eea56bc4bab33cef025eb000340a7252ddb83dd41041ebcc65d0b`
- The probe hashes the resolved executable file on every probe. A different
  blob with the same version string and schema bundle fails closed with an
  explicit unsupported reason and promotes nothing.
- 0.167.2 promoted Windows start/events/cancel against executable
  `sha256:6f65c42e3769209976cce097a400720b5d877e1d24ec8bb6272bf0f02d5e0fd7`.
  That projection does not carry over to 0.168.0: re-promotion requires the
  same lifecycle, cancellation, artifact, usage and negative-permission evidence
  for this exact blob.

## Capability map for this pin

| Operation | Windows | Linux |
| --- | --- | --- |
| start / events / cancel / result / artifact | verified | unverified |
| resume, handoff | unsupported | unsupported |
| native steer, pause, approval response | unsupported | unsupported |
| billable usage accounting | unsupported (cumulative counters only) | unsupported |
| provider broker, hard cost limit | unsupported | unsupported |

The Windows claim is carried by
[the 2026-11-13 native observation](native-windows-2026-11-13.md): exact-blob
transport containment, a credentialed repository turn with canonical TaskResult
and artifact, cancellation, cumulative-counter observations, and the
isolated-home read-only negative case. It does not carry over to any other
executable blob: a daemon must reject Codex native admission when the resolved
executable hash differs, and on Linux the adapter stays fail-closed until Linux
v2 cgroup owner evidence exists.
