# Symmetry v1 Contracts

`v1/*.schema.json` is the canonical JSON Schema Draft 7 source for the new
control envelopes and operator requests described by `docs/design/protocol.md` and
`docs/design/contracts.md`. `common.schema.json` contains shared definitions;
the envelope schemas reference it locally and reject unknown control fields.
`ContextSnapshot.content_hash` is the SHA-256 of canonical UTF-8 JSON for the
schema-valid snapshot with its top-level `content_hash` omitted; keys sort
recursively and arrays retain order.

The fixture manifest at `fixtures/manifest.json` is the shared positive and
negative corpus. It deliberately covers complete subjects, missing and extra
fields, wrong identity, unknown enums, nullable values, version mismatch and
safe-integer boundaries. Evidence receipts carry a `predicate_id`, complete
`Subject` and `subject_hash`; `contracts:check` verifies the canonical sorted
JSON Subject hash for TaskResults and evidence, plus kind-specific `source_ref`
identity. A TaskResult always carries its complete result Subject; a
`candidate_completion` therefore names the candidate artifact rather than only
claiming an unbound hash. Monetary strings are bounded to signed `int64`
`microusd`, including values below MaxInt64 such as `9200000000000000000`, the
exact MaxInt64 boundary, and rejection of MaxInt64+1. UUIDs use the daemon's
canonical lowercase form with version `1`-`5` and RFC 4122 variant `8|9|a|b`.
The fixture checker rejects fractional/exponent JSON number lexemes and raw
integers outside the JSON safe-integer range before `JSON.parse` or Ajv can
round them.

`goal-create.schema.json`, `goal-command.schema.json`, and
`plan-proposal.schema.json` are the canonical input authority for the Goal
operator API. `GoalCommand` is a strict discriminated union. In particular,
public `admit_task` selects only an admitted work item, purpose, model profile,
and session; it cannot supply the server-derived Subject, execution limits,
reservation, admission key, or provider scope. `request_plan` is the draft-Goal
exception: it selects only the planning model, approved repository resource and
complete Subject, plus a discriminated fresh/resume/handoff session selection.
The server derives the planning Admission, limits, reservation, context and
provider scope; the command cannot carry those fields. A planning Admission and
its ContextSnapshot have `purpose: "plan"` and `work_item_id: null`.
`request_decision` currently supports plan, dependency-scope, review, and
completion requests with fully typed intended effects. Budget and external-action
requests remain unavailable until their effects have equally strict types.

TaskResult includes a required nullable `proposal`: only `kind: "plan_proposed"`
may carry a schema-valid PlanProposal; every other result kind must carry
`proposal: null`.

`PlanItem.integration` may be omitted on the wire and means `false`. Canonical
proposal hashing materializes that default before hashing, so omission and an
explicit `false` have the same approved authority and replay identity.

An Admission includes a required nullable `provider_scope`. A non-null scope
freezes `resource_ids`, independently bounded `operations_by_resource`, and a
server-derived change target without connection or credential material. The Go
boundary checks that the operation-map keys equal `resource_ids`; Control must
enforce the same cross-field invariant before issuing an Admission. Execution
policy carries the server-owned `per_run_cost_limit_microusd` and
`hard_cost_limit_required`. Automatic execution requires a finite total
`budget_limit_microusd`; strict policy additionally requires a non-null per-run
ceiling and the hard-limit requirement, while Control verifies the selected
runtime's advertised enforcement capability.

Admission session selection is also discriminated: `fresh` and `handoff` require
`requested_session_id: null`, while `resume` requires the exact retained native
session UUID. Handoff is a request to reconstruct a new native session from the
immutable snapshot and reachable artifact; it never carries or transfers a raw
native session handle.

`authority_policy.operator_required_for_completion` is a stricter fence than
`execution_policy.final_acceptance`. When it is `true`, final acceptance is
operator-authorized even if `final_acceptance` is `"deterministic"`; review and
operator-acceptance predicates remain valid in that case. Machine-only
predicates are required only when the effective final-acceptance authority is
deterministic.

Generated TypeScript DTOs live under `generated/ts`; generated Go DTOs and the
embedded schema bundle live under `daemon/internal/contracts`. They are not
runtime validators and must not be edited by hand. Generated Go discriminated
union DTOs are decode-only transport views; do not marshal them back to wire
JSON because `quicktype` flattens branch-specific nullable fields. The daemon compiles the
bundle with `santhosh-tekuri/jsonschema` before decoding a Goal envelope, then
applies its bounded ECMAScript `CommitPath` check because Go's regexp engine
does not support the source schema's negative lookaheads. Recreate them with:

```text
pnpm contracts:generate
```

Validate every schema, fixture expectation and generated-file drift with:

```text
pnpm contracts:check
```

The root `pnpm-lock.yaml` pins `ajv`, `ajv-formats`,
`json-schema-to-typescript` and `quicktype-core`. These files describe only
the additive v1 envelopes; legacy protocol-v1 wire behavior remains owned by
its existing clients and schemas.

## Elixir Boundary API

`SymmetryControl.Goals.ContractValidation` is the narrow Elixir boundary
adapter. Goals code must pass the absolute configured `contracts/v1` path on
every call; there is no CWD or repository-root fallback:

```elixir
opts = [schema_root: configured_contracts_v1_path]
:ok = ContractValidation.validate_goal_revision(value, opts)
:ok = ContractValidation.validate_admission(value, opts)
:ok = ContractValidation.validate_context_snapshot(value, opts)
:ok = ContractValidation.validate_evidence(value, opts)
:ok = ContractValidation.validate_usage(value, opts)
:ok = ContractValidation.validate_adapter_capabilities(value, opts)
```

To consume the operator request roots, Control must add matching
`validate_goal_create/2`, `validate_goal_command/2`, and
`validate_plan_proposal/2` entry points and call them before command parsing.
That integration must retain the existing semantic checks for state,
authorization, proposal graph integrity, replay identity, and runtime
capability eligibility; JSON Schema deliberately does not duplicate them.

Validation failures return `{:error, reason}`. Atom map keys are normalized to
JSON string keys recursively, while values are left unchanged. The adapter
loads only the canonical Draft 7 documents from the explicit root, merges the
local shared definitions for ExJsonSchema resolution, rejects external schema
references and caches resolved immutable documents by root and envelope.
