# Goal 04 execution contract

Authority: `docs/goals/04-goal.md`. This document fixes integration seams for
parallel implementation. Verification results belong in `docs/goal-04-runbook.md`.

## Product behavior

- Chat is a first-class portal view with workspace, project, and individual run
  context. Explicit intent separates discussion/status from starting work,
  guidance, decisions, pause, resume, and cancellation. Message text never
  silently selects a mutating intent.
- Human messages and grounded context replies are persisted. Discussion/status
  replies use the same durable progress, findings, rationale, outcome, and delivery
  evidence as the work detail. They do not require interrupting the worker.
- Start work creates or launches a real project work item and its orchestration
  task in the same transaction as the message. The complete goal is retained.
- Live state and PR/CI/review are projections of existing domain records, not a
  second conversational state machine. Normal conversation excludes raw tool
  output; run details keep diagnostic history available.
- Mutating messages require an action ID and explicit current generation; stale
  run/decision context cannot control a later attempt. Repeated identical action
  IDs replay, changed payloads conflict. DB persistence precedes notification.

## Cooperative agent wire contract

An opt-in profile/runtime capability `supervisory_control: true` requires
interactive stdin, `input_mode: json`, and `event_format: jsonl`. Legacy agents
continue to work with their existing capabilities and input contract. Unsupported
controls are rejected explicitly, never silently delivered to an old daemon.

Durable command kinds and payloads:

```json
{"kind":"guidance","payload":{"message":"Use the existing adapter."}}
{"kind":"pause","payload":{}}
{"kind":"resume","payload":{}}
```

Guidance is allowed while running/paused, pause while running, resume while
paused. A waiting decision already stops autonomous progress and retains its
existing `waiting_transition_id` identity. Cancel remains available for paused
workers and wins over pending controls. Only one pending pause/resume is allowed.

Agent stdin adds `command_id` and types `guidance`, `pause`, `resume`, preserving
the original goal and putting the command payload into `input`:

```json
{"type":"guidance","command_id":"uuid","goal":"Original goal","input":{"message":"Use the existing adapter."}}
```

The initial supervised task envelope also describes high autonomy, escalation
reasons, and the safe-boundary/ack contract. Routine decisions, tool choices,
recoverable errors, and temporary uncertainty do not require approval.

An agent applies controls only at a safe boundary, then emits:

```json
{"type":"command_applied","command_id":"uuid","kind":"guidance","outcome":"applied"}
```

Outcomes are `applied`, `rejected`, or `failed`. Only a matching durable pending
intent can affect state. A successful stdin write is not evidence of application.
Applied pause transitions running -> paused; resume transitions paused -> running.
Transition payloads carry `command_id`; guidance does not change lifecycle state.
The daemon persists receipt/transition/ack outboxes before acknowledging upstream.

Paused processes retain their lease, capacity, process, and workspace. A lost
paused worker or pending pause must not automatically requeue and resume work.
Daemon restart cannot pretend to reattach: fail safely with retained history and
artifacts. Cancellation retains useful workspace artifacts even for an otherwise
automatic cleanup profile.

Decision packets extend the existing waiting event/transition:

```json
{
  "type":"waiting_for_input",
  "question":"Which migration strategy should be used?",
  "decision":{
    "reason":"irreversible",
    "context":"The migration removes the legacy column.",
    "recommended_option_id":"staged",
    "options":[
      {"id":"staged","label":"Stage migration","consequence":"Keeps rollback available."},
      {"id":"defer","label":"Defer","consequence":"Preserves current behavior and leaves this work blocked."}
    ]
  }
}
```

Allowed reasons: `blocked`, `consequential`, `irreversible`, `security`,
`business_policy`, `expensive`, `product_change`. Recommendation is optional and
must reference an option. Tasks requiring supervisory control must provide a
valid packet; legacy tasks retain question-only waits even on a capable runtime.
Decision replies reuse
`provide_input`, bind the waiting transition, and validate the selected option.

## Chat API integration

Use the existing authenticated/CSRF-protected portal API pipeline.

- `GET /portal/api/chat`: `scope=workspace|project|run`, with `project_id` for
  project scope or `run_id` for run scope. Optional `before` paginates messages.
- `POST /portal/api/chat/messages`: the same scope fields plus `action_id`,
  `intent`, and `content`. Mutations targeting existing work include
  `work_item_id` and `generation`. Run scope must match that work and generation.
- `start_work` accepts `work` containing optional `title`, `agent_profile`,
  `workspace`, `repository_resource_id`, and `ci_resource_id`; workspace scope
  also supplies `target_project_id`. `work_item_id` may select an existing ready
  agent-owned work item. Full content is the work description/goal, not truncated.
- `decision` also includes `waiting_transition_id` and `option_id` (legacy waits
  may use the content as their answer).
- GET returns `{scope, project_id, run_id, messages, runs, next_before}`.
  `messages` have `id`, `role`, `intent`, `content`, `inserted_at`, optional
  `work_item_id`, `command` (existing Protocol.command shape), and `metadata`.
  `runs` use existing `PortalJSON.work_item_detail` shape, with only human-facing
  semantic history required. Project/workspace context includes relevant work
  snapshots; the run scope pins one attempt and must never silently retarget.
- POST returns `{message, reply, work_item_id, command}` (nullable fields allowed).
  Client refreshes both Chat and the workspace after success.

## Verification

Verification covers transaction rollback and idempotency, questions without
execution mutation, capability gating, safe-boundary delivery, stale generation
and decision rejection, command/completion/cancel races, pause expiry/restart,
workspace artifact retention, meaningful timeline isolation, consistent PR/CI
projection, responsive desktop/mobile UI, and a live daemon lifecycle.
