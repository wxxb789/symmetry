# Symmetry next-stage design

Status: **normative implementation target**, proposed for acceptance through this
PR. Baseline: `36da0e253c1796c1a041e319c849fe68f23780ae`, inspected 2026-09-08.
This PR adds design and contributor material only; no runtime capability,
migration, adapter compatibility, or model-quality result is claimed delivered.

The owner requested fixed core design before local Codex implementation. The
following choices are fixed; implementation can choose local names/algorithms
only where they preserve these contracts. A material change needs a documented
design amendment with evidence; it is not silently delegated to a worker.

| Decision | Fixed choice |
| --- | --- |
| Product | Outcome-first engineering workspace with durable goal continuity |
| Authority | PostgreSQL; Elixir transition policy; Go local execution |
| Existing lifecycle | Preserve Task/Run/generation/fence semantics |
| Goal loop | Bounded task execution, evidence validation, transactional settlement |
| Reasoning | Configured harness tasks propose; deterministic kernel accepts |
| LoopX | Adopt concepts; no embedded second writable control plane |
| Graph | Same-goal WorkItem dependency DAG; derived read-only graph UI |
| Context | Immutable versioned snapshots and source pointers; no vector DB initially |
| Backend | Existing Phoenix/Ecto/PostgreSQL stack; Req; Oban short jobs |
| Frontend | React, TypeScript, Effect 3.x stable, Vite, pnpm, TanStack Router/Query |
| UI | Radix primitives, Tailwind CSS, Lucide; attention/list/board/detail/chat |
| Contract | JSON Schema Draft 7 as wire authority, generated TS/Go types |
| Deployment | Phoenix release serves built frontend; no Node production server |
| Scope | Existing single-operator deployment; Linux/Windows execution |
| Explicitly deferred | Native macOS daemon, multi-tenant RBAC, mobile, workflow DSL, skill marketplace, embeddings, autonomous cross-goal work |

No major upgrade of existing Go/Elixir/OTP/PostgreSQL is required by this design.
Honor the repository's CI/runtime versions. For newly added libraries, resolve
compatible stable patch releases and commit lockfiles. Effect stays on major 3
for this implementation even though the upstream site advertises v4 RC; moving
to major 4 is a separate evidence-backed design amendment.

## Documents

- [Core boundaries and loop](core.md)
- [Relational model, constraints, transactions and migration](data.md)
- [Wire protocol, commands, events and context](protocol.md)
- [Typed policy and result structures](contracts.md)
- [Native harness adapter contract](harness.md)
- [Frontend architecture and interaction design](frontend.md)
- [Quality policy, model routing and task prompts](quality.md)
- [Goals and local handoff](../goals/README.md)

## Evidence and design provenance

The existing [protocol v1](../protocol-v1.md) supplies the execution fence,
command acknowledgement and recovery foundation. The design extends it rather
than replacing it. Existing `work_items.orchestration_task_id` is a current-task
pointer; the migration below preserves it while adding durable task membership.

LoopX references (inspiration, not imported code):
[Turn receipts](https://github.com/huangruiteng/loopx/blob/f0375cc621cf8201ea457dfafeda5c938f1663a0/loopx/control_plane/turn_driver/transaction.py),
[recovery cases](https://github.com/huangruiteng/loopx/blob/f0375cc621cf8201ea457dfafeda5c938f1663a0/tests/test_loopx_turn_executor.py),
[versioned context](https://github.com/huangruiteng/loopx/blob/f0375cc621cf8201ea457dfafeda5c938f1663a0/loopx/capabilities/decision_context/assembler.py),
[host projection boundary](https://github.com/huangruiteng/loopx/blob/f0375cc621cf8201ea457dfafeda5c938f1663a0/docs/integrations/session-runtime-control-plane-adapter.md).
The latter is an architecture target/read-only v0 contract, not a proven
drop-in writable integration.

Multica references:
[native pi adapter](https://github.com/multica-ai/multica/blob/b5a7ee1e0e75347bed5fb4590e2fb9a92b046353/server/pkg/agent/pi.go),
[run/issue distinction](https://github.com/multica-ai/multica/blob/b5a7ee1e0e75347bed5fb4590e2fb9a92b046353/apps/docs/content/docs/tasks.mdx).
Its [license](https://github.com/multica-ai/multica/blob/b5a7ee1e0e75347bed5fb4590e2fb9a92b046353/LICENSE)
contains additional hosted-service, commercial-embedding and branding conditions.
Do not copy its implementation as if it were an unmodified Apache-2.0 library.
