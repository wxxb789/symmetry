---
name: symmetry-frontend-contract
description: Implement or review Symmetry portal changes affecting API decoding, asynchronous state, command receipts, routing or accessible user interactions.
---

# Frontend contract

Read [frontend](../../../docs/design/frontend.md), applicable
[protocol](../../../docs/design/protocol.md) sections and frontend/AGENTS.md.

Identify whether each value is URL state, server cache or local draft/interaction
state. Keep its designated owner; do not add a second cache or transition policy.
Decode external JSON and keep the generated DTO and Effect boundary compatible.

For a changed asynchronous flow, verify the relevant adversarial sequence:
navigation before response; refresh during editing; duplicate submission;
conflict after optimistic display; connection loss; session expiry. Reuse mutation
identity for intentional retries. Show authoritative pending/applied/failed states.

Render changed states, including loading/empty/error/unknown. Exercise keyboard
operation, focus return and draft/scroll preservation where affected. Use shared
tokens and accessible primitives. Do not self-certify owner-level visual acceptance.

Return actual type/behavior/browser checks, rendered states inspected and any
missing backend or visual acceptance. Do not add production mocks for missing APIs.
