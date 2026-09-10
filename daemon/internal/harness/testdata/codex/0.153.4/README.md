# Codex 0.153.4 probe evidence

These files record local executable/help and transport-framing evidence only.
They do not claim that Codex app-server native session, control, approval or
usage semantics are supported by Symmetry.

`app-server-help.txt` is a sanitized copy of the locally observed
`codex app-server --help` output. `frames.jsonl` contains generic JSON-RPC
framing examples used to prove buffering and diagnostic handling; it does not
name or implement a Codex RPC method.

`app-server-lifecycle.jsonl` is a normalized strict-JSON-RPC fixture for
deterministic parser tests. It preserves the v2 method names and interleaving,
but is not a raw capture.

`app-server-start-smoke.jsonl` is a sanitized non-credentialed Windows
observation from 2026-09-09 for Codex CLI 0.153.4. The installed server
omitted the `jsonrpc` member from both responses and notifications, and it
returned `sandbox.type=readOnly` after a `workspace-write` request. The strict
adapter therefore rejects this wire as unverified and does not claim native
support. Paths, identifiers, and installation metadata were redacted.

`schema-manifest.json` records SHA-256 hashes from the locally generated
versioned schema bundle and the observed permission mismatch. These fixtures
are not credentialed Linux and Windows release evidence and do not claim any
native capability.
