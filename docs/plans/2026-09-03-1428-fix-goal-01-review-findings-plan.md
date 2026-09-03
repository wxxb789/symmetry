---
title: Goal 01 Review Findings Remediation - Plan
type: fix
date: 2026-09-03
topic: goal-01-review-findings-remediation
artifact_contract: ce-unified-plan/v1
artifact_readiness: requirements-only
product_contract_source: ce-brainstorm
execution: code
---

# Goal 01 Review Findings Remediation - Plan

## Goal Capsule

- **Objective:** Close the 15 validated Goal 01 review findings without expanding into unproven residual risks.
- **Outcome:** The orchestration protocol, daemon liveness model, operator read surfaces, deployment boundary, and cross-language contracts are internally consistent and covered by deterministic tests.
- **Execution:** Code changes on `main`, with one verified commit for each meaningful work unit and no pull request.
- **Product authority:** `docs/goals/01-goal.md` defines the Goal 01 outcome; the session-settled decisions in this artifact govern this remediation when they narrow or clarify that goal.
- **Compatibility boundary:** Breaking protocol changes are allowed before `1.0.0`; compatibility becomes a release constraint at `1.0.0`.
- **Open blockers:** None. All product and contract decisions required for planning are resolved.
- **Stop condition:** All requirements below pass focused and full release validation, and a follow-up review reports no remaining actionable finding in this scope.

---

## Product Contract

### Summary

Remediate the validated Goal 01 reliability, protocol, deployment, and observability gaps while the project is still in rapid development. Replace the protocol v1 contract atomically across control, daemon, documentation, tests, and deployment assets rather than preserving legacy aliases.

### Problem Frame

Goal 01 has a working durable control plane and execution daemon, but a second full review found failure sequences that can block run progress, retain capacity indefinitely, or make the documented Compose path unusable. The control plane also persists runtime and execution history that operators cannot read through the supported API. Because the project is pre-`1.0.0`, the correct response is one coherent contract correction rather than compatibility scaffolding around behavior that is not yet released as stable.

### Requirements

**Deployment and transport**

- R1. Externally reachable production traffic must be HTTPS-only. TLS terminates at a trusted reverse proxy or load balancer, while explicitly trusted private networks such as the local Docker network may use HTTP. The control service must not trust client-supplied forwarded-protocol headers unless the request crossed the trusted proxy boundary.
- R2. The local Compose stack must use the trusted-private-network HTTP path and must prove that the daemon enrolls, receives work, and completes a task.

**REST protocol v1**

- R3. Protocol v1 must be replaced atomically with resource-oriented routes and purpose-appropriate HTTP methods. Read operations use GET, resource creation uses POST, idempotent resource writes use PUT, partial updates use PATCH, and DELETE is used only for actual deletion. No legacy route aliases or deprecation bridge is required before `1.0.0`.
- R4. Cancellation and consequential human input must create durable command resources with `Idempotency-Key`; cancellation must not delete the task or mutate task state outside the fenced transition model.
- R5. A successful bulk event append must return a bounded acknowledgement, preferably `204 No Content`, so an already-committed append cannot exceed the daemon response limit and enter a permanent replay loop.

**Operator observability**

- R6. Operator-authenticated runtime collection and detail resources must expose machine identity, online/offline state, last heartbeat, connection epoch, capacity, reserved capacity, and active runs from PostgreSQL-backed state.
- R7. Operators must be able to read both a unified task timeline and separate run event, transition, and command collections. The unified timeline is newest-first with an opaque `before` cursor. Separate collections are oldest-first with an opaque `after` cursor. Pagination defaults to 100 items and rejects limits above 500.
- R8. The task read model must expose the current waiting-for-input question and relevant command state without requiring direct database access.

**Daemon liveness and recovery**

- R9. Once a process exits and its terminal transition is durable locally, the daemon may retain its execution slot for at most eight minutes while delivery is retried. Control acknowledgement releases the slot earlier. At the deadline the slot is released while the journal remains durable, and a later cancellation may still replace an unconfirmed completion.
- R10. Lease maintenance must run independently from polling, reconciliation, outbox delivery, and workspace cleanup. The default lease is two minutes, renewal begins with about 90 seconds remaining, and an individual control request may wait up to 15 seconds.
- R11. Workspace cleanup attempts default to a two-minute timeout configurable from 10 seconds through 10 minutes. Timeout or failure retains the journal and workspace reservation for retry, does not reacquire an execution slot, and cannot block daemon shutdown indefinitely.
- R12. Repeated `waiting_for_input` agent records remain durable events but coalesce to one pending lifecycle transition. A later input must be able to resume the run and drain its terminal transition.

**Cross-language contract integrity**

- R13. An omitted task `input` is represented as `null`; an explicitly empty input remains `{}`. PostgreSQL, control DTOs, Go protocol types, validators, and agent JSON framing must preserve that distinction.
- R14. Interactive profiles require `input_mode: "json"`. The configuration must reject `interactive: true` with `input_mode: "goal"` until a plain-text follow-up contract is deliberately designed. Accepted interactive profiles must use the same newline-delimited JSON record framing for the initial input and every later `provide_input` command.
- R15. API clients must reject missing required fields, invalid types, and state-dependent invariant violations while accepting unknown additive fields. Existing field semantics may change before `1.0.0` only through an atomic control/daemon/docs/tests update.
- R16. Unknown or invalid operator credentials return `401 unauthenticated`; a valid credential used against a resource or credential class it does not own returns `403 forbidden`.
- R17. Startup control operations, including terminal-journal delivery, enrollment, and session registration, must fail fast for permanent client errors, retry transport failures and retryable server responses, and honor `Retry-After` where supplied.
- R18. Every untrusted value destined for PostgreSQL JSONB must reject `U+0000` before entering a transaction and return the protocol's invalid-request response rather than a database exception.

### Key Decisions

- **HTTPS boundary (Governs R1, R2):** Production is HTTPS-only at the external boundary; trusted private networks may use HTTP. (session-settled: user-approved — chosen over direct Phoenix TLS everywhere: proxy termination preserves production security without making local Compose carry certificates.)
- **REST replacement (Governs R3):** Replace protocol v1 atomically with purpose-appropriate REST methods and resource routes. (session-settled: user-directed — chosen over retaining GET/POST action routes: the greenfield protocol should express resource semantics directly.)
- **Durable commands (Governs R4):** Cancellation and human input create command resources. (session-settled: user-approved — chosen over deleting tasks or directly patching lifecycle state: commands preserve audit and fenced transition authority.)
- **Bounded event acknowledgement (Governs R5):** Event append returns a bounded success response because callers already own event identities and need reliable acknowledgement.
- **History surfaces (Governs R6, R7, R8):** Provide both a unified timeline and separate resource collections with view-specific ordering. (session-settled: user-directed — chosen over a single history representation: inspection and replay have different ordering needs.)
- **Terminal slot grace (Governs R9):** Hold a locally terminal execution slot for at most eight minutes, then release it while retaining durable delivery state. (session-settled: user-directed — chosen over immediate release: tolerate meaningful control-plane outages before returning capacity.)
- **Lease tolerance (Governs R10):** Use independent lease maintenance with a two-minute lease and 15-second request tolerance. (session-settled: user-approved — chosen over short single-reactor budgets: coding-agent runs should tolerate temporary connection failure.)
- **Cleanup tolerance (Governs R11):** Use a configurable two-minute cleanup timeout and durable retry. (session-settled: user-approved — chosen over unbounded cleanup: a local Git operation must not stop daemon progress.)
- **Waiting transition coalescing (Governs R12):** Preserve every waiting event but enqueue only one pending waiting lifecycle transition so repeated context cannot block progress.
- **Input absence (Governs R13):** Preserve the distinction between omitted input and an explicitly empty object. (session-settled: user-directed — chosen over canonicalizing both to an empty object: callers need to retain intent.)
- **Interactive framing (Governs R14):** Interactive execution uses one JSON record contract only for now. (session-settled: user-approved — chosen over mixed plain-text and JSON framing: one stable stream contract prevents mid-run parser changes.)
- **Strict required contract (Governs R15):** Reject missing required fields and invalid invariants while accepting unknown additive fields. (session-settled: user-approved — chosen over either sparse acceptance or fail-on-unknown parsing: required semantics stay strict without adding low-value parser rigidity.)
- **Authentication semantics (Governs R16):** Invalid credentials are unauthenticated; valid credentials with insufficient authority are forbidden so status codes retain their standard meaning.
- **Retry classification (Governs R17):** Retry only failures that can recover without configuration or credential changes so permanent failures remain diagnosable.
- **JSONB compatibility (Governs R18):** Reject JSONB-incompatible input at the protocol boundary so callers receive deterministic invalid-request errors instead of database exceptions.
- **Compatibility (Governs R3, R15):** Do not preserve pre-`1.0.0` protocol compatibility. (session-settled: user-directed — chosen over aliases and deprecation bridges: rapid development benefits from one corrected contract.)
- **Scope (Governs R1-R18):** Fix the 15 validated findings and their required tests only. (session-settled: user-approved — chosen over absorbing residual risks: unmeasured performance and future lifecycle work remain outside this remediation.)

### Acceptance Examples

- AE1. **Covers R1, R2.** Given an external production deployment, direct HTTP is unavailable and HTTPS succeeds through the trusted TLS boundary. Given the local Compose network, the daemon uses HTTP internally and completes a real fake-agent task.
- AE2. **Covers R5.** Given an event backlog whose serialized stored events exceed one MiB, the append commits once, returns a bounded success response, and the daemon removes the delivered events from its journal.
- AE3. **Covers R9.** Given a completed local process and an unavailable control plane, acknowledgement within eight minutes releases the slot immediately; continued outage past eight minutes releases the slot while retaining terminal delivery and cancellation authority.
- AE4. **Covers R10.** Given a polling or reconciliation request that stalls for 15 seconds, lease renewal continues independently and the live run remains authoritative until the two-minute lease genuinely expires.
- AE5. **Covers R11.** Given a hanging `git worktree remove`, cleanup times out, the journal and reservation remain, daemon shutdown completes, and later cleanup can retry.
- AE6. **Covers R12.** Given two `waiting_for_input` records before operator input, both events remain visible, only one waiting transition is sent, and the run resumes and completes.
- AE7. **Covers R13.** Omitting `input` produces `null` end to end, while sending `{}` remains an explicit empty object end to end.
- AE8. **Covers R14.** A profile with `interactive: true` and `input_mode: "goal"` fails configuration validation before enrollment or process launch. An accepted interactive JSON profile receives the same newline-delimited JSON record shape at startup and for later input.
- AE9. **Covers R15, R16, R18.** Sparse success responses, invalid credential classification, and JSONB-incompatible payloads fail with the documented protocol errors rather than being accepted or causing database exceptions.

### Success Criteria

- Every validated review finding has a deterministic regression test at its responsible layer.
- Cross-service contract tests cover the REST routes, methods, status codes, pagination, nullable input semantics, and bounded append response.
- Live E2E proves Compose enrollment/task completion, repeated waiting input recovery, outage-after-exit capacity release, reconnect, control restart, and stale fencing.
- Control and daemon full test gates, formatter checks, static analysis, production release build, Linux build, and deployment configuration checks pass from a clean tree.
- A follow-up full review reports no remaining actionable finding in this remediation scope.

### Scope Boundaries

- Do not refactor large files solely because they exceed a line-count threshold.
- Do not redesign per-event journal persistence without benchmark evidence.
- Do not add reconnect jitter, enrollment-token rotation, retention policy, or new rate-limit behavior unless a validated finding requires it to complete safely.
- Do not implement the Goal 02 portal, Goal 03 provider integrations, or Goal 04 chat experience.
- Do not add compatibility aliases, migration shims, or deprecation behavior before `1.0.0`.

### Dependencies and Assumptions

- The TLS-terminating proxy or load balancer is trusted to set and strip forwarded headers at the external production boundary.
- PostgreSQL remains authoritative for control state and execution history; daemon journals remain authoritative only for undelivered local work.
- Operator authentication grants access to all Goal 01 runtime and execution history until a later multi-tenant authorization model is defined.
- Unknown additive response fields remain acceptable even though backward compatibility is not a pre-`1.0.0` priority.

### Sources

- `docs/goals/01-goal.md`
- `docs/protocol-v1.md`
- `docs/goal-01-runbook.md`
- `control/lib/symmetry_control/orchestration.ex`
- `control/lib/symmetry_control_web/router.ex`
- `control/config/prod.exs`
- `daemon/internal/app/app.go`
- `daemon/internal/control/validation.go`
- `compose.yaml`
- `.github/workflows/ci.yml`
