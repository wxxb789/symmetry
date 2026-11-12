# Goal 0006 Go state audit - 2026-11-11

Scope: `daemon/internal/{app,state,workspace,execution,protocol,authority,harness}`,
`daemon/cmd`, and `docs/design/harness.md`. Read-only source audit plus the
focused checks recorded below. This is not an exact-Subject acceptance receipt.

## Synchronized

- No `TODO`, `FIXME`, `XXX`, `HACK`, placeholder return, or panic placeholder
  remains in the active non-test Go files under the audited packages. Intentional
  fail-closed branches are explicit errors (for example
  `daemon/internal/app/app.go:4347`).
- Pi is pinned to `1.5.2` (`daemon/internal/harness/pi/probe.go:17`); Claude Code
  to `2.1.322` (`daemon/internal/harness/registry.go:22`); Codex to `0.167.1`
  with its generated-schema digest (`daemon/internal/harness/codex_probe.go:12`).
- Codex, Claude, Pi and the current OpenCode adapter still reject unverified
  native lifecycle capabilities rather than inferring them from transport.
- Linux v2 admission, clone intent/checkout/promotion, retained-session
  compatibility, stop receipts and exact-subject checks remain fail-closed and
  consistently ordered.

## Open mismatches

| Severity | Finding | Evidence |
| --- | --- | --- |
| High | OpenCode code and CI still pin V1 `1.18.32`, while the approved target is V2 `2.2.7`; V1 is explicitly skipped. The adapter rejects `2.2.7` before any V2 probe. | `daemon/internal/harness/opencode/adapter.go:28,281-283`; `.github/workflows/ci.yml:1386-1422,1481-1558`; `docs/goals/0006g-opencode-support-decision.md:3,18-26` |
| High | A retained Linux v2 resume still requires a native gate/bootstrap reuse mode, not only a daemon-local grant. The bootstrap always creates and checks out a fresh target; the helper/gate require the new RunKey's clone grant. | `daemon/cmd/symmetry-linux-clone-bootstrap/main_linux_amd64.go:167-200`; `daemon/native/linux-gate/c-gate/gate.c:1125-1216`; `daemon/internal/platform/linux_owner_helper_v2_linux.go:470-480,1441-1444`; `daemon/internal/app/app.go:4573-4575` |
| Medium | The current Claude lifecycle candidate is deliberately unregistered and rejects resume/guidance, while the design row previously advertised them as current. The row now marks resume/guidance as 0006e targets. | `daemon/internal/harness/claude_candidate_adapter.go:66-69,115-116,534-535`; `docs/design/harness.md:11` |
| Medium | OMP and OmO have goals and design rows but no `harness.Kind`, registry constructor or adapter. This is still future-goal work, not a current capability. | `daemon/internal/harness/types.go:25-27`; `docs/goals/0006j-omp-native-capability.md`; `docs/goals/0006k-omo-native-capability.md` |
| Low | The default registry installs `UnavailableAdapter` entries for Codex, Claude, Pi and OpenCode, then daemon composition overwrites all four. The fallback messages are not the production support statement. | `daemon/internal/harness/registry.go:141-147`; `daemon/internal/app/app.go:1163-1170` |

## Decision

Do not bump OpenCode to `2.2.7` or claim V2 support without exact-binary
help/API/event evidence. Do not enable Linux v2 retained resume without the
native reuse mode. Keep both paths fail-closed; the design amendment in
`docs/design/harness.md:110-118` records the required grant and native
prerequisite.
