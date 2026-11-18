# Chat and supervisory control

Status: **operator runbook** for the scoped conversation surface at
`/portal/chat` and the supervisory control contract behind it. The wire records
(`guidance`, `pause`, `resume`, `command_applied`, `waiting_for_input`,
`decision`) are normative in [protocol v1](../protocol-v1.md) under *Cooperative
Supervision*; native adapter capability status is in
[harness.md](../design/harness.md); gates are listed in
[quality.md](../design/quality.md). A run conversation remains tied to its
original attempt: it cannot quietly control a replacement run after a retry.

## Chat actions

Open `/portal/chat` and choose Workspace, Project or a particular Run. Legacy
`/portal#chat` links are repaired once by the SPA bootstrap to the same surface.
The composer makes execution intent explicit:

- **Discuss / Status** persists a human message and an evidence-based reply using
  the same progress, findings, outcome and PR/CI/review records as the run view.
  Asking a question does not send stdin to, pause or cancel the working agent, and
  the control plane makes no extra language-model call.
- **Start work** creates project work with the complete goal and launches it
  through the normal scheduler. Project defaults supply the agent profile and
  workspace; Workspace Chat also needs a target project. The work appears on the
  Kanban.
- **Guidance** persists an instruction for the current execution. *Queued* means
  durably saved; *Applied* means the worker confirmed it at a safe boundary. Each
  instruction accepts at most 32,768 UTF-8 bytes, including multibyte text.
- **Pause** asks the worker to finish its atomic operation and then stop
  autonomous progress; **Resume** continues the same live process. Cancelling and
  retrying work stay on the work-detail surface, not in the composer.

Guidance, pause and resume are supervisory control, and the composer offers them
only while the run serves `can_guide`/`can_pause`/`can_resume` as true: those
flags are true only for a runtime that advertises the `supervisory_control`
capability, so a native-harness run serves them as `false` (and `pause` is
`unsupported` in the operations a native admission advertises) and the composer
offers no guidance, pause or resume. Its start, event stream and cancellation
come from the native adapter instead.

Run context shows concise progress and delivery state. A human decision presents
the question, context, reason, options and consequences, and a recommendation
when justified; its answer is bound to that specific waiting transition. Raw
execution history remains in run details for diagnosis.

The run lifecycle and the Kanban review workflow remain separate: a completed run
shows its completed execution and final result everywhere, and humans then move
the work item through Review and Done with the existing board workflow. Replies
to ordinary questions are grounded summaries of recorded evidence — the control
plane does not invent an unrecorded rationale — and coding plus tool execution
stay on the machine-local agent adapter.

## Agent adapter contract

Profiles supporting safe-boundary controls opt in:

```json
{
  "input_mode": "json",
  "interactive": true,
  "event_format": "jsonl",
  "supervisory_control": true
}
```

The daemon validates that this capability requires interactive JSON input and
JSONL events. Chat-created work requires `supervisory_control`, except when every
runtime registered for the target project binding is a native harness kind, in
which case the native adapter-operations requirement applies instead. A legacy
runtime does not receive unsupported commands: the daemon settles them as
`rejected` without writing to stdin. The trusted-local Compose fake-agent profile
supports the capability; production adapters must implement the contract before
opting in.

A profile that opts in with `supervisory_control` receives an initial task
envelope that communicates high autonomy: handle ordinary implementation choices,
tool selection, temporary uncertainty and recoverable errors independently, and
escalate genuine blockers or consequential, irreversible, security,
business/policy, unexpected-cost or material product decisions. A profile without
the capability keeps the legacy task/input envelope.

The daemon sends `guidance`, `pause` and `resume` records with a durable
`command_id`, the original `goal` and an `input` payload. The adapter consumes
them at an appropriate safe execution boundary and emits `command_applied` with
the matching ID, kind and an `applied`, `rejected` or `failed` outcome; a receipt
that does not match the command's ID and kind is refused. Writing to stdin does
not establish application.

For a consequential decision, the task emits `waiting_for_input` with a concrete
`question` and a `decision` containing `reason`, `context`, `options` (`id`,
`label`, `consequence`) and an optional `recommended_option_id`; the answer must
name an existing option. Tasks that require supervisory control must supply a
valid packet, while legacy question-only waits remain compatible for tasks
without that requirement. Pause retains the process, execution slot, lease and
workspace, and does not free runtime capacity; a waiting human decision already
stops progress, so pause is offered while running. A paused process that is lost
cannot silently resume on another generation — the system records a recoverable
failure instead. Daemon restart does not reattach to an old process or replay
uncertain stdin effects; review retained state before explicitly retrying failed
work.

## Persistence and delivery

PostgreSQL owns scoped conversations, messages, work, commands, decisions and
execution history. Mutating Chat operations persist their message and domain
effect in one transaction. `action_id` provides replay protection; using it with
different content conflicts. Generation and waiting-transition checks reject
stale browser instructions, and a historical attempt's projection strips live
controls.

Daemon journals retain supervisory delivery intent and outcome before draining
events, transitions and command acknowledgements. PubSub and polling only wake or
transport work already stored durably. Cancellation and completion settle pending
instructions explicitly; a delayed pause or resume cannot resurrect a terminal or
cancelling execution.

## Deployment and recovery

Apply the control migrations and deploy the updated control plane before enabling
`supervisory_control` on daemon profiles. Existing profiles without the
capability retain the legacy task/input contract. A compatible runtime must be
online before Chat-created work can leave the scheduler queue.

The command-hash migration records the algorithm version for each command:
existing hashes keep their original replay contract, and new commands also bind
the explicit generation and waiting-transition context (see
[Request-hash write mode](control-plane-and-daemon.md#request-hash-write-mode)).

The supervisory migration adds `paused` to the task/run state constraints,
`guidance`/`pause`/`resume` to the command kind constraint, and `paused` to the
capacity-bearing run index. It refuses a downgrade once supervisory command or
paused-run history exists; deleting that history is not a rollback procedure.
Keep the upgraded database and use a forward fix if a deployed adapter violates
the contract, disabling new supervised work while diagnosing that adapter.

During rollout, verify that queued instructions become acknowledged only after
their safe-boundary receipt, that paused tasks remain paused, and that no
replacement generation starts without an explicit retry. Investigate commands
that remain pending beyond the adapter's normal atomic-operation duration using
run details and the daemon journal: a command that is durably pending is not
evidence that the agent has applied it.

## Verification

Run the regular control and daemon gates (see
[Control plane and daemon operations](control-plane-and-daemon.md#verification)
and *Commands and gates* in [quality.md](../design/quality.md)), then the browser
suites at desktop and mobile widths. Browser setup and the full fixture inventory
are documented in [`browser/README.md`](../../browser/README.md); the
conversation-specific pieces are:

The pinned package manager is `package.json`'s `packageManager` field; the
commands below show its current value.

```sh
mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile
mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test
```

With a control plane running, the isolated Windows fixture builds the real daemon
and deterministic cooperative agent and exercises the full Chat path:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-chat-daemon-tests.ps1
```

Pass `-ControlContainer <name>` to additionally restart that dedicated disposable
control container while the worker is paused, verifying that Chat history, paused
state and the same live worker survive a control restart. Use a freshly compiled
control build: edit-independent portal assets are staged by the frontend build
above, and a missing bundle returns 503 rather than an alternate workspace.

The fixture checks questions without run mutation, durable guidance affecting
subsequent artifacts after a real reconnect, pause with stable artifacts, same-run
resume, structured decision delivery, completion consistency, and cancellation
with retained artifacts. It also sends a required task a malformed question-only
wait and checks that it fails clearly while the next assignment can still
complete. The fake agent is test evidence for the integration contract, not
evidence of a production coding-agent adapter's compliance.
