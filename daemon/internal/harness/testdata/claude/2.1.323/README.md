# Claude Code 2.1.323 evidence

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client. This directory supersedes `../2.1.322/`, which stays in the
tree as historical evidence; the 2.1.259, 2.1.281 and 2.1.322 observations are
historical only and cannot satisfy this pin.

## Files

- `version.txt` is the `--version` output of the installed Windows `claude.exe`:
  `2.1.323 (Claude Code)`, captured 2026-11-13 from the exact installed path.
- `help.txt` is the sanitized `--help` capture of the same executable (22494
  bytes). It advertises `--print`, `--verbose`, `--input-format stream-json`,
  `--output-format stream-json`, `--json-schema`, `--session-id`,
  `--permission-mode` and `--permission-prompts`. The capture contains no user
  paths.
- `stream-json.jsonl` is the synthetic, Multica-derived framing fixture carried
  unchanged from `../2.1.322/`. Identifiers, messages and usage values are
  invented; it proves envelope framing only.
- `native-repository-turn.jsonl` is a sanitized real 2.1.323 stream-json capture
  of the credentialed repository turn recorded in
  [the 2026-11-13 receipt](native-repository-turn-windows-2026-11-13.md). Six
  records: one `system/init`, one assistant `tool_use`, its `tool_result`, the
  `StructuredOutput` tool call, its result, and the terminal `result`. Absolute
  paths were replaced with `<elided-path>`, the `system/init` path and listing
  fields (`cwd`, `plugins`, `skills`, `agents`, `slash_commands`, `memory_paths`,
  `powershell_path`, `messaging_socket_path`, `additional_directories`,
  `original_cwd`) with `<elided>`, and session/UUID identifiers with fixed
  placeholders. Record order, types and every lifecycle-relevant field are the
  observed ones, including `"permissionMode":"acceptEdits"` and
  `"model":"claude-sonnet-5.5"`.
- `fixture-manifest.json` records the exact executable SHA-256 plus the SHA-256 of
  every committed fixture in this directory.
  `TestClaudeTestdataManifestBindsRecordedDigests` recomputes every digest and
  requires the manifest's executable digest to equal the pinned
  `ClaudeTestedExecutableHash`, so a fixture or pin drift fails a test instead of
  silently weakening the evidence.

## Exact-binary binding

Promotion requires the resolved executable bytes to match the tested blob in
addition to the exact version string and the transport help:

- Tested executable SHA-256:
  `sha256:5064e6e0572dca86257363ca04d48653eef20ef27f1bd04b504ed7199ed3fd13`
  (367634080 bytes) at
  `…/mise/installs/claude-code/2.1.323/claude.exe`.
- The probe hashes the resolved executable on every promotion attempt (the
  conservative registry projection never hashes, because it promotes nothing).
  A different blob with the same version string promotes nothing.
- Recorded blob negative control: the `mise` PATH shim reports the same
  `2.1.323 (Claude Code)` string but is a different file
  (`sha256:53ca4fc6…`, 298632 bytes), so it reaches the blob gate and promotes
  nothing. A third client, `~/.local/bin/claude.exe`
  (`sha256:85429000…`, 219715232 bytes), is rejected earlier by the version gate
  because it reports `2.1.259 (Claude Code)`; it therefore evidences version
  rejection, not same-version blob rejection.
- The version gate is anchored to the complete `X.Y.Z (Claude Code)` line, so a
  shim banner, a prerelease suffix or another client on the same stream fails
  closed instead of being parsed into a stable-looking prefix.

## Fixed permission policy

The fixed transport argv now carries exactly one permission value:

```text
claude --print --verbose --input-format stream-json --output-format stream-json \
  --json-schema <projected TaskResult schema> --session-id <uuid> \
  --permission-mode acceptEdits
```

Claude Code grants nothing without an approval path and this adapter has no
verified approval bridge, so a policy is unavoidable. `acceptEdits` is the
narrowest mode that keeps a real repository turn possible. Its bounds were
measured against the exact blob with a real model on 2026-11-13, one control at a
time; only the listed observations are claimed:

| Control | Observed result |
| --- | --- |
| `Write` tool, path inside the admitted workspace | allowed |
| `Write` tool, path outside the session working directories | denied (`permission_denied`) |
| `Bash` classified as an edit (for example `printf … > hello.txt`) | allowed |
| `Bash` `whoami` | allowed |
| `Bash` `curl` (network) | denied, "This command requires approval" |
| `Bash echo … > <path outside the workspace>` | command allowed; the shell wrote a mangled filename inside the workspace instead |
| repository-local `.claude/settings.json` with `permissions.deny: ["Write"]` | effective: the `Write` tool was removed from the session |
| repository-local settings with `allow: ["Bash"]`, `allow: ["Bash(*)"]`, `defaultMode: "bypassPermissions"`, or an outside-path `Write` allow rule | no widening observed: the tested operations stayed denied |

Two conclusions only: the mode is not `--dangerously-skip-permissions` or
`bypassPermissions`, and the native permission layer is not treated as a
containment boundary. The daemon's process owner (Windows Job object, Linux owner
path) remains the boundary; the admitted workspace is an execution starting point,
not a filesystem jail for the shell tool, and the edit/read classifier behind
`acceptEdits` is a product heuristic rather than a security boundary. Both
credentialed captures were taken with an isolated `CLAUDE_CONFIG_DIR`; an
operator's own user settings stay operator-owned and were not part of the
recorded measurement.

## Capability map for this pin

A daemon admits a Claude Code runtime only when the configured metadata matches
the probe: `runtime.harness_version` must be `2.1.323` (native kinds may not use
`legacy`), `runtime.adapter_version` must be `claude-stream-json-v1`, and
`runtime.adapter_protocol_version` must be `1`. An absent executable projects
`unavailable`; a resolved executable whose version, transport or bytes do not
match this pin projects no operation and stays fail-closed.

| Operation | Windows | Linux |
| --- | --- | --- |
| start / events / cancel / structured-output result / repository artifact | verified for the exact blob | unverified |
| resume, handoff | unsupported | unsupported |
| native steer, pause, approval response | unsupported | unsupported |
| billable usage, hard cost limit | unsupported (stream usage is not promoted) | unsupported |
| provider broker | unsupported | unsupported |

The Windows claim is carried by
[the 2026-11-13 receipt](native-repository-turn-windows-2026-11-13.md): exact-blob
promotion, a credentialed repository turn with its artifact and canonical
TaskResult, cancellation, and the recorded negative controls (blob mismatch,
missing transport, terminal result without a matching `system/init`, closed or
superseded session).

Linux admission stays fail-closed: the Linux v2 owner path needs an installed A/B
systemd runtime with a delegated cgroup-v2 parent plus cross-UID peer inspection,
which the hosts available for this goal do not provide. Windows evidence is never
promoted as Linux support.

## What this does not prove

No resume, handoff, native guidance, safe pause, approval response, provider
broker, billable usage or hard cost limit is proved here, and none is advertised.
The capture is one model (`claude-sonnet-5.5`) through one local provider
endpoint on one host; model-level tool compliance is not a Symmetry contract.
