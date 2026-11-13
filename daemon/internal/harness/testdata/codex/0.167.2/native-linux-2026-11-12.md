# Codex 0.167.2 Linux native observation

Date: 2026-11-12. Platform: Ubuntu 24.04 on WSL2 x86_64, uid 1001. Scope:
exact-binary transport and repository-turn attempt for the Symmetry Codex
adapter. This is not a promoted Linux capability.

## Exact binary

- Release asset: `codex-x86_64-unknown-linux-musl.tar.gz` from
  `https://github.com/openai/codex/releases/download/rust-v0.167.2/`
- Archive SHA-256:
  `8246e846c550ea21197fcc4067bc270dca895e94ea42ed6a282e6f4e0125b15f`
- Extracted binary SHA-256:
  `a9672d0fa0dd8fcf8cad136fa0d056b914bc046f5ac3d78f4a5cf390655af56b`
- `codex --version`: `codex-cli 0.167.2`

## Contained transport

```text
SYMMETRY_CODEX_NATIVE_SMOKE=1 \
SYMMETRY_CODEX_NATIVE_SMOKE_EXECUTABLE=<absolute>/codex-x86_64-unknown-linux-musl \
  go test ./internal/harness/codex -run '^TestNativeTransportOpenAndClose$' \
    -count=1 -v -timeout 180s
```

Result: PASS. `initialize` and `thread/start` granted `workspaceWrite`; `Close`
reported `terminated=true`, `containment_error=false`,
`termination_error=false`.

The adapter disables Codex's `shell_snapshot` feature for native sessions.
Without that, 0.167.2 starts its snapshot shell in a detached session and the
process-group owner observes an escape during the first close, even though the
snapshot exits. The daemon owns the child environment, so the snapshot is not
needed for admitted executions; disabling it keeps the process-group
containment proof valid without widening permissions.

## Credentialed repository turn

The same live smoke as Windows ran with a local Linux `CODEX_HOME` using the
configured HTTPS provider and `deepseek-v4.1-flash`. `thread/start`, `turn/start`
and the terminal `turn/completed` were observed, but the result was rejected at
close:

```text
Codex app-server process failed after the terminal turn event:
close started process: Linux descendant containment is unproven:
descendant observation was uncertain
```

`/proc` sampling of the actual turn showed the cause: Codex starts every shell
tool child in its own session. In this run the app-server leader was pid 548
(pgrp 548, sid 548) and the shell tool child was pid 635
(`codex-x86_64-un`, pgrp 635, sid 548), with grandchildren `lsb_release`, `cut`,
`tr` and `git`. Upstream 0.167.2 does this deliberately:
`codex-rs/core/src/spawn.rs` calls `detach_from_tty()` (which calls `setsid`)
for `StdioPolicy::RedirectForShellTool`. Symmetry's Linux process-group owner
cannot prove or guarantee termination of a child that leaves the group, so the
session fails closed instead of promoting a Linux capability.

## Capability decision

Linux stays explicitly unverified and the probe promotes no Linux operation.
This is a feasibility conflict with the current containment owner, not a
transport or permission failure.

Smallest proposed amendment: run Codex repository work under the existing
Linux v2 owner (per-run cgroup leaf, `cgroup.kill`) and collect the same
lifecycle, cancellation, artifact and negative-permission evidence there. Until
that evidence exists, a Linux daemon must reject Codex native admission rather
than inherit the Windows capability projection.
