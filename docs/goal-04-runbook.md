# Goal 04: Autonomous Chat and supervisory control

## Chat workflows

Open `/portal#chat`. Choose Workspace, Project, or a particular Run. A run
conversation remains tied to its original attempt; it cannot quietly control a
replacement run after a retry.

The composer makes execution intent explicit:

- Discuss or Status persists a human message and an evidence-based reply using
  the same progress, findings, outcome, and PR/CI/review records as the run view.
  Asking a question does not send stdin to, pause, or cancel the working agent.
- Start work creates project work with the complete goal and launches it through
  the normal scheduler. Project defaults supply the agent profile and workspace;
  Workspace Chat also needs a target project. The work appears on the Kanban.
- Guidance persists an instruction for the current execution. Queued means
  durably saved; Applied means the worker has confirmed it at a safe boundary.
  Each instruction accepts at most 32,768 UTF-8 bytes, including multibyte text.
- Pause asks the worker to finish its atomic operation, then stop autonomous
  progress. Resume continues the same live process. Cancel terminates execution
  and preserves its history and useful workspace artifacts.

Run context shows concise progress and delivery state. A human decision presents
the question, context, reason, options and consequences, and a recommendation
when justified. Its answer is bound to that specific waiting transition. Raw
execution history remains available in run details for diagnosis.

The run lifecycle and the Kanban review workflow remain separate. A completed run
shows its completed execution and final result everywhere; humans can then move
the work item through Review and Done using the existing board workflow.

Replies to ordinary questions are grounded summaries of recorded evidence. The
control plane does not make an additional language-model call or invent an
unrecorded rationale. Coding and tool execution remain on the machine-local
agent adapter.

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

Chat-created work requires the `supervisory_control` capability. A legacy runtime
does not receive unsupported commands. The trusted-local Compose fake-agent
profile supports this capability; production adapters must implement the
contract before opting in.

The initial task envelope communicates high autonomy: handle ordinary
implementation choices, tool selection, temporary uncertainty, and recoverable
errors independently. Escalate genuine blockers or consequential, irreversible,
security, business/policy, unexpected-cost, or material product decisions.

The daemon sends `guidance`, `pause`, and `resume` records with durable
`command_id`, original `goal`, and an `input` payload. The adapter consumes them
at an appropriate safe execution boundary and emits `command_applied` with the
matching ID, kind, and `applied`, `rejected`, or `failed` outcome. Writing to stdin
does not establish application. See [Protocol v1](protocol-v1.md).

For a consequential decision, emit `waiting_for_input` with a concrete `question`
and `decision` containing `reason`, `context`, `options` (`id`, `label`,
`consequence`), and optional `recommended_option_id`. Tasks requiring supervisory
control must supply a valid packet. Legacy question-only waits remain compatible
for tasks without that requirement.

Pause retains the process, execution slot, lease, and workspace; it does not free
runtime capacity. A waiting human decision already stops progress, so pause is
offered while running. A paused process that is lost cannot silently resume on
another generation. The system records a recoverable failure instead. Daemon
restart does not reattach to an old process or replay uncertain stdin effects.
Review retained state before explicitly retrying failed work.

## Persistence and delivery

PostgreSQL owns scoped conversations, messages, work, commands, decisions and
execution history. Mutating Chat operations persist their message and domain
effect in one transaction. `action_id` provides replay protection; using it with
different content conflicts. Generation and waiting-transition checks reject
stale browser instructions.

Daemon journals retain supervisory delivery intent and outcome before draining
events, transitions, and command acknowledgements. PubSub and polling only wake
or transport work already stored durably. Cancellation and completion settle
pending instructions explicitly; a delayed pause or resume cannot resurrect a
terminal or cancelling execution.

## Deployment and recovery

Apply the control migrations and deploy the updated control plane before enabling
`supervisory_control` on daemon profiles. Existing profiles without the capability
retain the legacy task/input contract. A compatible runtime must be online before
Chat-created work can leave the scheduler queue.

The command-hash migration records the algorithm version for each command.
Existing hashes remain unchanged and retain their original replay contract;
new commands also bind the explicit generation and waiting-transition context.

The supervisory migration adds `paused` to state constraints and the
capacity-bearing run index. It refuses a downgrade once supervisory command or
paused-run history exists; deleting that history is not a rollback procedure.
Keep the upgraded database and use a forward fix if a deployed adapter violates
the contract. Disable new supervised work while diagnosing that adapter.

During rollout, verify that queued instructions become acknowledged only after
their safe-boundary receipt, paused tasks remain paused, and no replacement
generation starts without an explicit retry. Investigate commands that remain
pending beyond the adapter's normal atomic-operation duration using run details
and the daemon journal. A command that is durably pending is not evidence that
the agent has applied it.

## Verification

Run the regular control and daemon gates, then desktop/mobile browser acceptance:

```sh
cd control
mix compile --warnings-as-errors
mix test
cd ../daemon
go vet ./...
go test ./...
cd ../browser
npm test
```

With a control plane running, the isolated Windows fixture builds the real
daemon and deterministic cooperative agent and exercises the full Chat path:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-chat-daemon-tests.ps1
```

Use a freshly compiled control build. If an existing build copied `priv` assets,
run `mix compile --force --warnings-as-errors` in that control environment after
editing portal assets so the browser receives the current JavaScript.

For a dedicated disposable control container, pass `-ControlContainer <name>`
to additionally restart that container while the worker is paused. This verifies
that Chat history, paused state, and the same live worker survive control restart.

It checks questions without run mutation, durable guidance affecting subsequent
artifacts, pause with stable artifacts, same-run resume, structured decision
delivery, completion consistency, and cancellation with retained artifacts.
The fixture creates unique runtime bindings and reports its retained log and
artifact directory. The fake agent is test evidence for the integration
contract, not evidence of a production coding-agent adapter's compliance.

The live fixture also sends a required task a malformed question-only wait and
checks that it fails clearly while the next assignment can still complete.

### Recorded evidence

- Before implementation: existing control suite, **277 passed, 3 skipped**, in
  Docker with the isolated `symmetry_goal04_test` database.
- New live Chat acceptance initially failed at the expected boundary:
  `POST /portal/api/chat/messages` returned **404** against the original server.
- Windows daemon: `go vet` and the final full suite, **379 top-level tests passed,
  11 skipped**, including the real blocked-child and queued-cancel regressions.
- Linux daemon: `go vet` and full `go test -race`, **378 top-level tests passed,
  11 skipped**. The Unix termination test explicitly removes the race runtime's
  test-process exit delay; production termination and the exit assertions remain
  unchanged.
- Final control gate: formatting, strict compilation and **316 ExUnit tests
  passed, 3 skipped**, including the failure-display regression.
- Production strict compilation and `mix release --overwrite` passed; the release
  is generated at `control/_build/prod/rel/symmetry_control`.
- Full desktop/mobile browser suite: **45 passed, 17 skipped, 0 failed, 0 flaky**,
  including failure-display and pending-to-applied composer receipt regressions.
- Eleven independent review lenses and focused post-fix validators closed all
  **9 findings**, including guidance byte limits, disk-failure fencing, old-command
  replay, historical evidence, pagination, queued-cancel response validation,
  fatal decision-packet process termination, and stale composer receipts.
- Final real-daemon Chat acceptance: **1 passed** in approximately **1.2 minutes**.
  The visible composer creates work; the cooperative agent completes **120 steps**
  with one consequential decision. Questions leave commands unchanged, guidance
  changes subsequent artifacts, pause survives a control-container restart, and
  resume retains the same process identity. Chat, board and detail agree on the
  completed execution. Cancellation retains stable artifacts. A missing required
  decision packet fails the blocked child and the next assignment completes.
- The final live run also checks applied command receipts in the composer and
  saves paused, decision, completed, and mobile screenshots. Desktop and mobile
  output were visually checked; the mobile page has no horizontal overflow.
- Production release was regenerated after the final UI fix. Its `portal.js`
  SHA-256 matches the source asset. Final `git diff --check`, Go formatting and
  JavaScript syntax checks passed.

Evidence was recorded on September 6, 2026. The live fixture reports its temporary
directory containing daemon logs, retained artifacts and four screenshots.
Skipped suites require explicitly enabled live services or production agent
adapters; the dedicated cooperative Chat fixture above was enabled and passed.

### Cancellation review follow-up

Both cancellation review findings are fixed.
New cancellation acknowledgements retain a null `applied_at`; old incorrectly
stored timestamps are normalized in the wire response without rewriting history.
Generation-fenced cancellation rejects `payload` before creating a command.

The two HTTP regressions failed before the fix. Afterward, all **22 protocol
controller tests** and the full Control suite passed (**316 passed, 3 skipped**), along with formatting, strict compilation and the production
release. The complete real-daemon Chat fixture passed again and now requires the
cancel acknowledgement to be accepted and its local journal to disappear while
artifacts remain intact.

A separate recovery check replayed the previously stuck cancellation using the
unchanged original daemon binary and a copy of its saved state. The copied
journal retired, artifacts stayed unchanged, and the original failure evidence
was preserved. The permanent regressions are in
`control/test/symmetry_control_web/controllers/protocol_controller_test.exs`
and `browser/tests/portal-chat-live.spec.mjs`. Go and production UI sources did
not change in this follow-up, so their preceding full-suite results remain
applicable.
