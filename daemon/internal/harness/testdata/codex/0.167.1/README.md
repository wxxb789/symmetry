# Codex 0.167.1 probe evidence

Historical: superseded by `../0.167.2/` under the 2026-11-11 latest-stable
policy. The schema bundle is byte-identical between 0.167.1 and 0.167.2, but
this directory records no native session or control evidence.

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client. This directory superseded `../0.156.1/` as the tested
Codex version; 0.153.4 and 0.156.1 observations are historical only.

These files record local executable/help, schema and transport-framing evidence
only. They do not claim that Codex app-server native session, control, approval
or usage semantics are supported by Symmetry.

- `version.txt` is `codex-cli 0.167.1`.
- `app-server-help.txt` is the sanitized `codex app-server --help` output. It
  advertises `--stdio` / `stdio://` as the default transport.
- `schema-manifest.json` records SHA-256 hashes from the locally generated
  0.167.1 schema bundle and method schemas. The v2 bundle hash changed from
  0.156.1 and is the probe gate.
- `app-server-start-smoke.jsonl` is a sanitized non-credentialed Windows
  observation of `initialize`, `initialized` and `thread/start` on 0.167.1.
  The server again omitted `jsonrpc` and returned `sandbox.type=readOnly` after
  a `workspace-write` request; that permission mismatch remains unsupported.
- [Windows native observation](native-windows-2026-11-11.md) records the exact
  binary and the fail-closed `thread/start` rejection.
- [Linux native observation](native-linux-2026-11-11.md) records the exact
  binary and the fail-closed close/containment failure.
- `app-server-lifecycle.jsonl` and `frames.jsonl` are normalized synthetic
  fixtures moved from `../0.156.1/`. They model v2 method names and framing,
  not a raw 0.167.1 turn capture.

The adapter still projects native capabilities as unverified and does not claim
repository work, resume, approvals or provider accounting. Credentialed Linux
and Windows release evidence remains Goal 0006d work.
