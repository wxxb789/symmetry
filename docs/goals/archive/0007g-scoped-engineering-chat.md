# 0007g - Scoped, recoverable engineering conversation

Provide real workspace-, project- and run-scoped engineering conversation, with
work items reachable as targets of those scopes, that preserves each scope's
draft, selection, scroll position, and history. Questions, proposed commands,
authorization, execution, and delivery receipts remain distinct so conversation
cannot silently mutate work.

Prerequisite: the accepted 0007f runtime, generation and receipt surfaces.

Command proposals carry the operator's explicit intent and the server-derived
scope, and are never invented client-side and never require a language model:
the intent vocabulary and the structured waiting-for-input decision package are
the server's. This goal additionally completes the fixed design's outstanding
Chat requirement: for native admissions only, the blanket generic
supervisory-control check on the Chat start path is replaced by required
start/events/result capabilities with operation-specific checks; the legacy
generic path keeps its supervisory-control requirement and its existing
acknowledgement tests, with no weakening. Guidance distinguishes next-turn
delivery from native steering, unsupported pause controls are hidden or
explained, and the UI never renders raw agent HTML or unexpanded raw tool noise.

## Done when

Scoped conversation is real, recoverable and safe, and the run shows each part
itself:

- browser and live-daemon tests for scope switching between workspace, project
  and run: each scope's draft, selection and scroll position survive, and
  background updates never overwrite another scope's draft → observed in one
  session;
- browser and live-daemon tests for a changed action's replay, a late read, a
  late write and an uncertain retry → the observed outcomes, with the retry
  reusing the original action identity and never double-applying;
- browser and live-daemon tests for history gaps, reconnect and durable control
  receipts → the observed receipt history after a reconnect;
- read-only questions never steer a worker, and a control request stays pending
  until the server confirms it → the observed request/acknowledgement sequence;
- for native admissions, the Chat start path admits through required
  start/events/result capabilities with operation-specific checks, while the
  legacy generic path keeps its supervisory-control requirement and its existing
  acknowledgement tests pass → the observed admissions on both paths and the
  retained acknowledgement tests;
- browser and live-daemon tests for unsafe output rendering → output rendered
  inertly with links restricted, observed for malicious content;
- keyboard and focus behavior stays usable through a long conversation (long
  session, scope switches, reconnect) → the recorded walk, judged on observable
  behavior, not on message counts or durations;
- this surface's pre-existing browser acceptance coverage continues to pass on
  the new routes and its classic checks are retired in the same PR → the
  executed browser suites and the removed spec list.

If, at the start of the run, any input that only the user can supply is
missing, stop and report at once with the exact missing item: do not start
partially, do not substitute synthetic, mock or fixture-only
evidence for a required real surface, and do not narrow the scope. After that,
take and report a reversible default for every remaining open detail; stop only
for an irreversible or out-of-bounds decision: adding a conversation scope the
server does not implement, removing the legacy generic Chat path, or deleting
acceptance coverage instead of migrating it. End with a report that pairs each
required item with the evidence that demonstrated it, states the exact checks
executed and their observed results, including counts and exit codes, and lists
unresolved limitations explicitly.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission,
authority or recovery rule is weakened, removed or reinterpreted to make an
implementation pass; a fixed design change would need a documented amendment
with evidence, not a worker choice (root AGENTS.md, the fixed-design amendment
rule); evidence binds the revision it was produced on and older evidence never
validates newer code (root AGENTS.md, the evidence-binds-its-revision rule).
This goal renders no raw agent HTML and gives read-only conversation no implicit
worker authority. It excludes provider connection management and Goal-specific
screens. It owns the existing Chat guidance/pause/resume dispatch and their
receipts; 0007f owns the work-detail controls and receipt presentation, and this
goal adds no new runtime control routes.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline, not
an automatic stop. Waiting, observation and other parties' decisions do not
count toward this window and are neither a completion argument nor a reason for
an extra goal; elapsed time never establishes success.

This goal is one independently mergeable PR (repository convention,
docs/goals/README.md).

## Owner acceptance

After the run (not part of this Done when): the owner reviews rendered
workspace-, project- and run-scoped conversation with a work-item target on
actual work against the fixed design's chat scope, intent and receipt rules
(docs/design/frontend.md). Record that review as the owner's own verdict in the
receipt; the model never rates its own rendering, never signs for the owner and
never treats owner acceptance as a condition this run can satisfy. Until that
review arrives, the receipt states the result as produced and awaiting the
owner's verdict.
