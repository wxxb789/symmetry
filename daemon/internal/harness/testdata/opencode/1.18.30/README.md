# OpenCode 1.18.30 private protocol fixtures

These synthetic fixtures model the locally inspected OpenCode `serve` API at
version `1.18.30` (upstream commit `3104c1428ec91f809e5ab86631300de41eb6952e`).
They exercise HTTP identity and SSE framing/cursor validation only.

The fixture files do not capture provider credentials, model prompts, native
lifecycle, terminal results, usage, resume, permissions, artifact recovery, or
shutdown. They do not establish a supported Symmetry OpenCode adapter.

## Native Transport Check

On 2026-09-11, `TestNativeTransportOpenAndClose` passed with the actual OpenCode
1.18.30 binary on Windows amd64 and Linux amd64 (glibc baseline build, Docker
with `--init`, Go 1.27). This opt-in test uses the production adapter, isolated
home/config/data directories, no provider credential variables copied into the
child environment, and no prompt or `StartTurn` call. It checks process-identity persistence, verified TCP peers,
HTTP health/session creation, repeated Open returning the same native identity,
and bounded Close/Wait with an unknown task result.

Binary provenance:

- Windows installed `opencode.exe` SHA-256:
  `c1bdbb18767048e1853af4238311b3f7e16ff2f91b68fb4c7ced3c5175347eea`.
- Official `opencode-linux-x64-baseline.tar.gz` release archive SHA-256, checked
  before extraction:
  `60c92147d0d86ca606dda8a77260d3c87e0ef959eb2d8dbffb34df6d8a64e063`.

From `daemon/`, set `SYMMETRY_OPENCODE_NATIVE_SMOKE=1` and set
`SYMMETRY_OPENCODE_NATIVE_SMOKE_EXECUTABLE` to the absolute binary path, then run:

```text
go test -count=1 -timeout 120s -run '^TestNativeTransportOpenAndClose$' -v ./internal/harness/opencode
```

The Linux check initially failed because Bun's HTTP listener uses
`TCP_DEFER_ACCEPT=1`: the client can be established while the server still has
only a `SYN_RECV` request and no accepted socket inode. The verifier now allows
a five-second observation deadline on the same connection, without sending
application bytes. A real deferred-accept socket test verifies this behavior;
SYN_RECV, listener ownership, and inode zero still cannot authorize HTTP bytes.
OpenCode's pinned source uses Bun 1.3.14; see the upstream
[HTTP listener](https://github.com/oven-sh/bun/blob/bun-v1.3.14/packages/bun-uws/src/HttpContext.h)
and [deferred accept option](https://github.com/oven-sh/bun/blob/bun-v1.3.14/packages/bun-usockets/src/bsd.c).

No Symmetry model turn was submitted. This test does not instrument every
internal network operation of the native executable, verify provider usage or
terminal semantics, or promote any capability. Credentialed repository work,
native stream replay, permissions, resume, and artifact recovery remain pending.
