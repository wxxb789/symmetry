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
omitted the `jsonrpc` member from both responses and notifications. The parser
now accepts that omission for JSON-RPC-like framing, but this historical
observation does not verify native method semantics. The server also returned
`sandbox.type=readOnly` after a `workspace-write` request; that permission
mismatch remains unsupported. The adapter therefore keeps the native
capability projection unverified and does not claim native support. Paths,
identifiers, and installation metadata were redacted.

`schema-manifest.json` records SHA-256 hashes from the locally generated
versioned schema bundle and the observed permission mismatch. These fixtures
are not credentialed Linux and Windows release evidence and do not claim any
native capability.

## Workspace-only permission contract

In the pinned `rust-v0.153.4` protocol, `writableRoots` lists additional roots;
the verified `cwd` is implicitly writable. An explicit empty array is therefore
valid, while missing/null roots remain invalid. Both response CWD fields must
match the requested workspace, every additional/runtime root must stay within
it, and network access must explicitly remain disabled.

`thread/start` requests `sandbox: "workspace-write"` and four dotted `config`
overrides, without modifying global settings:

```json
{
  "sandbox_workspace_write.writable_roots": [],
  "sandbox_workspace_write.network_access": false,
  "sandbox_workspace_write.exclude_tmpdir_env_var": true,
  "sandbox_workspace_write.exclude_slash_tmp": true
}
```

The response must explicitly confirm `excludeTmpdirEnvVar: true` and
`excludeSlashTmp: true`; otherwise writable temporary directories could extend
the requested scope. Missing, null, false, and wrongly typed flags are rejected.
The adapter still rejects `readOnly` and every non-`workspaceWrite` grant. It does
not automatically choose or initialize a Windows sandbox implementation.

Workspace identity and containment use existing directory entities, not lexical
path normalization. Both CWD fields and every returned writable/runtime root
must be absolute existing directories; invalid UTF-8, NUL, and standalone `.`
or `..` components are rejected. Linux resolves symlinks and rejects resolution
errors. Windows opens the directory and obtains its final normalized DOS/UNC
path with `GetFinalPathNameByHandle`, resolving junctions as well as symlinks.
The adapter compares these resolved paths exactly, with a directory-separator
boundary for descendants; it does not infer identity through case folding.

Windows junction behavior has local regression coverage. Native UNC and
case-sensitive SMB behavior remain unverified. Resolution is a validation-time
snapshot, not protection against a later filesystem replacement (TOCTOU).

The exact upstream contracts are in
[core SandboxPolicy](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/protocol/src/protocol.rs),
[v2 permission projection](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server-protocol/src/protocol/v2/permissions.rs),
[thread request](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server-protocol/src/protocol/v2/thread.rs),
and [request config loading](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server/src/config_manager.rs).

## Opt-in native transport check

Set `SYMMETRY_CODEX_NATIVE_SMOKE=1` and
`SYMMETRY_CODEX_NATIVE_SMOKE_EXECUTABLE` to the absolute pinned executable, then
run from `daemon/`:

```text
go test ./internal/harness/codex -run '^TestNativeTransportOpenAndClose$' -count=1 -v -timeout 150s
```

The test isolates native configuration and credentials, verifies the exact
version/schema, persists process identity before exposure, and exercises
`Open`, repeated `Open`, `Close`, `Wait`, and repeated `Close`. It submits no
model turn. Closing before a turn produces `cancelled/missing_result`, never
execution success or accepted work. The process must be terminated with no
sink, output, containment or termination error.

The shared runner deliberately sets `OutputTruncated=true` when termination
closes output delivery to unblock sinks. The test checks this termination
barrier, not a complete output drain. An enabled test that receives a read-only
grant fails; it is not converted to a skip or a support claim.
