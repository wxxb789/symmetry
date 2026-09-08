# Symmetry contributor contract

Symmetry is an engineering-work control plane. Keep its core small, explicit,
and recoverable. PostgreSQL owns durable control state; Elixir owns transition
policy; Go owns machine-local execution; the frontend owns presentation.

## Read only what the change needs

- Start with [design decisions](docs/design/README.md) and the selected file in
  [docs/goals](docs/goals/README.md). These describe the approved target, not
  features already implemented. Existing protocol v1 remains binding until its
  documented additive migration is implemented.
- Read the applicable directory AGENTS.md. Skills in `.agents/skills/` apply
  by task: `symmetry-durable-transition`, `symmetry-harness-adapter`,
  `symmetry-frontend-contract`, `symmetry-simplification-review`.
- Historical goals under `docs/goals/archive/` are evidence, not competing
  instructions. Do not load all historical plans into every task.

## Non-negotiable boundaries

- A model may propose work or summarize evidence; it cannot grant itself
  authority, change an acceptance contract, or declare unverified completion.
- Do not change a fixed design decision silently. If code or upstream evidence
  disproves feasibility, document the exact conflict and smallest proposed
  amendment. Continue unaffected authorized work. Routine implementation
  choices within the design do not need another approval.
- Preserve fences, idempotency, transaction boundaries, credential locality,
  and existing provider-owned fields. A notification is never business truth.
- Run success, accepted work, and achieved goal are distinct. Evidence binds
  the exact subject revision; old evidence cannot validate new code.
- Do not reduce tests, types, validation, permissions, or acceptance conditions
  to make an implementation pass. Necessary contract changes require explicit
  rationale and review independent of the implementing model.
- Reuse an existing library or local pattern when it reduces total owned
  complexity. Do not add wrapper-only layers, speculative extension systems,
  duplicate caches, or a second scheduler/state authority.

## Working and verifying

Use bounded changes with a complete behavior contract. Read relevant code and
tests before editing. Preserve unrelated changes. Model choice does not change
acceptance standards. Follow [quality policy](docs/design/quality.md) for risk
routing, actual commands, evidence, and escalation; missing checks remain
missing, never assumed passed. Do not require every expensive suite for a
documentation-only or unrelated local change.

Before completion, perform a scoped simplification review: remove an abstraction
only when required behavior and clarity survive; repeat affected checks.
Report what changed, the exact checks executed, their outcomes, and unresolved
limitations. A green test suite is necessary evidence, not proof of all quality.
