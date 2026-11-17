# 0007g scoped engineering chat — implementation plan

Revision: branch HEAD `dd2e9db0829d105ac58d1713d01092811cda86ff` (merged 0007f).
Rebase note (2026-11-17): the branch was rebased onto
`706174ecda9504c6c1d991ddd4f862ff3bf47f0c` (#37, #39, #40, #42, #43, #44 and #45) before its PR
opened; the plan's steps are unchanged by that rebase, and the receipt's own rebase section records
the per-file resolutions and the rows re-measured on the rebased revision.
Contract: `docs/goals/0007g-scoped-engineering-chat.md`. Shape reference: 0007f
(`docs/plans/2026-11-16-0007f-runtime-control-plan.md`,
`docs/evidence/goal-0007f-runtime-control-2026-11-16.md`).

## Public/API shape

No new control route, no new command envelope, no permission change. The two
existing Chat routes are consumed unchanged and the shell serves one new
document route:

| Operation | Route | Body/answer (existing contract) |
| --- | --- | --- |
| conversation read | `GET /portal/api/chat?scope=workspace\|project\|run[&project_id=][&run_id=][&before=]` | `{scope, project_id, run_id, messages[], runs[], next_before}` (`SymmetryControl.Chat.conversation/1`) |
| message | `POST /portal/api/chat/messages` | request `{scope, project_id\|run_id, intent, content, action_id, work_item_id?, generation?, waiting_transition_id?, option_id?, target_project_id?, work?}`; answer `201` (created) / `200` (replayed identical action) with `{message, reply, work_item_id, command}` |
| document route | `GET /portal/chat` | the authenticated shell document (`PortalController.index`), plus its login `return_to` allowlist entry with the shell's own `scope`/`projectId`/`runId` search state |

The server owns the intent vocabulary (`discuss, status, start_work, guidance,
decision, pause, resume, cancel`), the scope resolution, the replay identity
(`action_id` + request hash, `409 idempotency_conflict` on a changed replay) and
the structured waiting-for-input decision package. The surface derives every
proposal from those served members; it invents no intent, scope or plan.

Additive **read** contract only, in the same class as 0007b's and 0007f's
consumed read schemas:

1. `contracts/v1/chat-read.schema.json` (new Draft 7 root) describes the
   consumed members of the conversation read: scope enum, nullable project/run
   identity, `ChatMessage` (`message_json/1`), `ChatCommand`
   (`Protocol.command/1`) and the reused work-detail projection for `runs`
   (`Chat.ReadModel.runs/2`, whose `raw.timeline` is blanked). Fixtures and
   regenerated DTOs follow the existing manifest convention; nothing existing
   changes.
2. The POST answer stays decoded by a hand-written Effect struct inside
   `frontend/src/services/api.ts`, exactly like the existing control responses
   (`StartResponse`, `CancelResponse`, …), because it is a narrower receipt
   projection than the read.

Server-side policy change (the design's outstanding Chat requirement,
`docs/design/harness.md` "Existing chat compatibility"):

3. `SymmetryControl.Chat` start path admission. Today every Chat start submits
   `required_capabilities: %{"supervisory_control" => true}`. That blanket
   requirement is kept for the legacy generic path and replaced, for native
   admissions, by operation-specific capability requirements:

   | Path | Requirement submitted with the task |
   | --- | --- |
   | legacy generic | `%{"supervisory_control" => true}` (unchanged) |
   | native admission | `%{"adapter" => %{"operations" => %{"start" => true, "events" => true, "cancel" => true, "pause" => "unsupported"}}}` |

   "Native admission" is server-derived, never client-named: the runtime rows
   Control itself has registered for the work item's execution binding
   (`agent_profile` + `workspace`, resolved exactly as
   `Workspaces.execution_attrs/3` resolves them) must exist and every one of
   them must be a native harness kind that can be claimed by a goal-less task
   (`codex`, `claude_code`, `pi`, `opencode`). `omp`/`omo` are excluded because
   `Orchestration.next_assignable_legacy_task_and_runtime/1` filters them out of
   the legacy path and `Orchestration.commit_new_claim!/3` rolls a goal-less
   claim on them back (`:ownership_lost`); such bindings therefore keep the
   legacy requirement rather than stranding the task under an impossible one.
   An empty runtime set (no daemon has registered yet) also keeps the legacy
   requirement, so every existing behaviour and test is preserved.

   The native requirement reuses the exact nested shape Control already
   validates and matches (`native_goal_adapter_requirement/0` in
   `orchestration.ex`, `server_admission_budget!/3` in `goals.ex`) and the exact
   operation set the daemon itself requires before it starts a native admission
   (`admissionLaunchFailure/3`: `start`, `events`, `cancel`, with `pause`
   reserved for the legacy supervisory protocol). Containment matching in
   `next_assignable_legacy_task_and_runtime/1` and the daemon's per-operation
   checks stay the only admission authorities; this change adds no second
   scheduler and no new wire member.

## Exact edge-case semantics the contract demands

| Contract clause | Semantics implemented |
| --- | --- |
| workspace/project/run scopes are real | Scope is a URL member (`/portal/chat?scope=…&projectId=…&runId=…`), the read is keyed by the *server-resolved* scope, and the rendered scope label comes from the read's own `scope`/`project_id`/`run_id`, never from the URL alone. |
| each scope keeps draft, selection and scroll | One key per scope (`workspace`, `project:<id>`, `run:<id>`) holds the composer draft, the selected intent, the start-work selections and the message-list scroll offset. Switching restores that key's values; a background refetch of another scope cannot write into it (per-scope query keys + per-scope local state, asserted by tests). |
| background updates never overwrite another scope's draft | Query keys are per scope, so a late read for scope A can only update A's messages; drafts live outside the query cache entirely. The surface asserts both directions in tests and in the browser walk. |
| changed action's replay | A replayed `action_id` with a changed payload is the server's `409 idempotency_conflict`; the surface renders the server's refusal and never a second message. |
| late read / late write | A late read for A never overwrites B's messages or draft; a late write for A lands only in A's history and leaves B's draft/status untouched. |
| uncertain retry never double-applies | Mutations never retry automatically. A lost response keeps the draft and the frozen payload (same `action_id`, byte-equivalent body); the retry reuses that object and the server answers the original message with `200 replayed`. |
| history gaps | Older pages stay retained across refreshes; ids are deduped; `Load older` continues from the oldest retained cursor. A refresh that breaks contiguity renders an explicit gap note instead of presenting a gapped history as continuous. |
| reconnect | A failed read keeps the loaded conversation visible as stale with the failure surfaced; a later successful read reconciles with the server's snapshot, and receipt outcomes update from durable evidence only. 401 still expires the session. |
| durable control receipts | A receipt is derived from the served command: `applied` requires `acknowledgement_outcome == "applied"`; a pending command renders "Saved · awaiting agent acknowledgement"; a retained page the refreshed newest page does not carry renders "Acknowledgement not refreshed with this page" and never applied. |
| read-only questions never steer a worker | `discuss`/`status` create no command server-side (existing behaviour, re-proved live) and the surface offers them as questions, with the live suite observing the command list unchanged. |
| a control request stays pending until the server confirms | The send status is `Sending…` until the POST answers; a confirmed command shows the served acknowledgement state only; a refusal shows the server's message; an unconfirmed send stays unconfirmed. |
| native admissions admit through required capabilities | See the table above; the observed admissions are the created task's `required_capabilities` and the runtime the real scheduler assigns, on both paths. |
| legacy generic path keeps its supervisory-control requirement | Same observation, plus every existing acknowledgement test unchanged. |
| unsafe output rendering | Message content, refusals, waiting questions/options and titles render as inert text (no HTML, no raw tool events); every hyperlink goes through one helper that accepts only `http:`/`https:`. |
| keyboard and focus through a long conversation | Native form controls, composer focus/caret retained across background refreshes, `Load older` keeps focus at the boundary, route changes focus the heading through the shell's existing rule. |
| pre-existing acceptance migrates, classic checks retire | `browser/tests/portal-chat.spec.mjs` and `portal-chat-live.spec.mjs` are rewritten onto `/portal/chat` in the same change, each classic assertion either migrated or listed retired with a named successor. |

## Evidence plan

Per-clause red-before/green-after where a new assertion exists: new frontend
unit tests and the rewritten browser suites are run against the pre-change tree
first where that is possible (the pre-change tree has no `/portal/chat` route at
all, which is the red class (a)), and the new server assertions run against the
unmodified `chat.ex` first (red class (b): the blanket requirement persists).

| Done-when item | Evidence |
| --- | --- |
| scope switching keeps draft/selection/scroll; background updates never cross scopes | browser: one session switching workspace→project→run→workspace at both Chromium widths with a held (late) read and a held (late) write; live-daemon: the same walk on a real run scope with a real daemon record |
| changed replay, late read, late write, uncertain retry | browser (deterministic, held responses) + live: real `409` on a changed replay, real `200 replayed` on an identical retry with one durable message row, real late read/write isolation |
| history gaps, reconnect, durable receipts | browser: paged history through the real cursor semantics (synthetic daemon-free where the backend cannot serve records) + live: real cursor load, a real reconnect against the daemon, and the receipt history re-read after reconnection |
| questions never steer; controls stay pending | live: command list before/after a question identical; guidance command observed `pending`→`applied` with the receipt rendered from the served outcome |
| native vs legacy admission | Elixir: real runtime registrations and the real scheduler's assignment on both paths, plus the retained acknowledgement tests |
| unsafe rendering | browser + live: a malicious message body served by the real API renders inert; a `javascript:` pull-request URL renders no anchor while an `https:` one does |
| keyboard/focus walk | live: long session (many messages), scope switches and a reconnect, recorded as an observable-behaviour walk |
| pre-existing coverage migrates | the executed browser suites on the new routes and the removed-classic-check list in the receipt |

## Out of scope (with reason)

- Goal-specific screens, Goal admission and any Goal route — 0007h owns them;
  `Chat` does not create Goal admissions and the `omp`/`omo` bindings stay on
  the legacy requirement rather than inventing one.
- Work-detail controls, cancel/retry dispatch and the shared receipt
  presentation — 0007f owns them; Chat dispatches only its own intents
  (`discuss`, `status`, `start_work`, `guidance`, `pause`, `resume`, `decision`)
  and reuses the shared derivation.
- Provider connection management and resource binding/sync — 0007d/0007e.
- Streaming/real-time Chat over the socket, or a second message transport — the
  existing read + refetch is the contract; real-time is 0007i's cutover.
- Pagination changes, new intents or new capability vocabulary — each would be
  a wire-contract addition needing an amendment.

## Design trade-offs

- **Admission derivation from the registry, not from the request**: a client-
  named runtime class would be a second authority and could select a path the
  deployment cannot serve. Deriving from Control's own runtime rows keeps the
  server the only authority and degrades to the unchanged legacy behaviour when
  no runtime is registered.
- **`result` in the design's "start/events/result"**: the fixed adapter-operation
  vocabulary has no `result` member (`contracts/v1/common.schema.json`
  `AdapterOperations`); the terminal result is delivered on the `events` stream
  that the requirement already demands, and the daemon's own native-admission
  check requires `start`/`events`/`cancel`. Adding a `result` key would duplicate
  `events` in every adapter record for no new information, so the requirement
  uses `start` + `events` (+ `cancel`, matching the daemon's admission check and
  keeping a launched run cancellable). Recorded here as the chosen default for
  an ambiguous design phrase.
- **Chat does not dispatch cancel/retry**: `0007f` owns those controls and their
  receipt presentation; dispatching them from two surfaces would create two
  writers for one authority. The retired classic Chat cancel check is listed
  with that successor.
- **Receipt rendering reused, not re-implemented**: `features/work/receipts.ts`
  already encodes "applied only from durable evidence"; the Chat surface
  consumes it and adds only the page-provenance rule for retained history.
- **Raw execution data never rendered in Chat**: the conversation is a
  semantic surface; the work detail remains the diagnostic one (0007f), so no
  raw tool output can leak as unexpanded noise.

## Ambiguities and chosen defaults (reported, reversible)

1. **`result` capability** — see the trade-off above: implemented as
   `start` + `events` (+ `cancel`) with the daemon's operation set; the
   alternative (a new `result` operation member) is rejected as a duplicated
   wire vocabulary needing a design amendment.
2. **Run selection source** — the surface lists runs from the served workspace
   read (`project.work_items[].execution.run_id`) rather than adding a run-list
   route; the deep link (`?scope=run&runId=…`) is the contract, and an unknown
   run id renders as its own option rather than falling back to another run.
3. **`scope`/`projectId`/`runId` in the URL** — kept so a session expiry
   returns to the same scope; the login allowlist accepts exactly those
   anchored parameters and nothing else.
4. **Delivery label for guidance on a legacy runtime** — "the worker's next safe
   execution boundary", which is the server's own reply wording; native steering
   and next-turn adapters are labelled from the adapter record and no control is
   offered for `unsupported`.
5. **Chat start of an existing work item** — the surface keeps the classic
   behaviour of proposing new work (title + goal); launching an existing
   backlog/ready item stays on the work detail's start control (0007f), because
   the Chat read does not carry the `can_start` predicate per item.
