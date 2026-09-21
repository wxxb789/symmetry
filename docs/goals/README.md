# Next goals for local Codex

Each active goal below is one independently meaningful outcome intended to fit
one agent run of at most 12 hours and one complete pull request. Every PR must be
safe to merge by itself: the repository remains runnable, existing supported
behavior stays correct, and no unmerged sibling is required for correctness.

Each series starts with its lettered goals and ends with a `-final` goal. The
`-final` file preserves the full-series acceptance contract and is the last
check/review goal, after every lettered goal in that series is complete. Read
its contract before implementation, but execute its review last.

| Series | Default serial order |
| --- | --- |
| 0006 | 0006a through 0006i, then 0006-final |
| 0007 | 0007a through 0007i, then 0007-final |
| 0008 | 0008a through 0008d, then 0008-final |

Independent lettered goals may run in parallel once their stated prerequisites
are met. Final reviews check integrated outcomes and reuse valid evidence from
the lettered goals; they do not repeat implementation or duplicate authorized
acceptance actions. Missing evidence or unresolved requirements block completion.

## Durable engineering work

| Goal | Desired outcome |
| --- | --- |
| [0006b](0006b-crash-safe-process-containment.md) | Crash-safe process containment and authority handoff |
| [0006c](0006c-retained-session-continuity.md) | Verified retained-session resume and handoff |
| [0006d](0006d-codex-native-capability.md) | Evidence-bounded Codex native capability |
| [0006e](0006e-claude-native-capability.md) | Evidence-bounded Claude Code native capability |
| [0006f](0006f-pi-native-capability.md) | Evidence-bounded pi native continuity |
| [0006g](0006g-opencode-support-decision.md) | Evidence-backed OpenCode support decision |
| [0006h](0006h-credentialed-provider-accounting.md) | Credentialed provider reconciliation and accounting |
| [0006i](0006i-exact-subject-release-acceptance.md) | Exact-subject release and authorized acceptance |
| [0006-final](0006-final.md) | Final review of the complete durable-work outcome and acceptance evidence |

## Modern workspace

| Goal | Desired outcome |
| --- | --- |
| [0007a](0007a-deployable-workspace-shell.md) | Deployable authenticated workspace shell |
| [0007b](0007b-readable-work-views.md) | Readable project, board and work-detail views |
| [0007c](0007c-safe-work-planning.md) | Safe project and work-item editing |
| [0007d](0007d-connection-management.md) | GitHub and Azure DevOps connection management |
| [0007e](0007e-resource-binding-and-sync.md) | Project resource binding and truthful synchronization |
| [0007f](0007f-runtime-control.md) | Runtime visibility and trustworthy execution control |
| [0007g](0007g-scoped-engineering-chat.md) | Scoped, recoverable engineering conversation |
| [0007h](0007h-goal-attention-workspace.md) | Goal, attention and decision workspace |
| [0007i](0007i-portal-cutover.md) | Single real-time Portal cutover |
| [0007-final](0007-final.md) | Final review of the integrated workspace and owner acceptance |

## Model-independent quality

| Goal | Desired outcome |
| --- | --- |
| [0008a](0008a-auditable-acceptance-workflow.md) | Auditable model-independent acceptance workflow |
| [0008b](0008b-go-model-routing.md) | Evidence-based Go model routing |
| [0008c](0008c-elixir-model-routing.md) | Evidence-based Elixir model routing |
| [0008d](0008d-typescript-model-routing.md) | Evidence-based TypeScript model routing |
| [0008-final](0008-final.md) | Final review of the combined evaluation evidence and accepted routing policy |

Read [fixed design](../design/README.md) and root `AGENTS.md` before executing.
Goal payloads define completion, not implementation plans. A dependency listed
inside a goal is a prerequisite for starting it, never permission to merge a
partial or knowingly broken state.

Native credentialed verification requires the named binary, credentials,
provider identity and permitted spend. Missing external inputs remain explicit
blockers; synthetic evidence must not be promoted. Historical goals 0001–0005
remain untouched under `archive/`. The next unsuffixed goal number is 0009.
