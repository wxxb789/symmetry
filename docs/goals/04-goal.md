## Goal 4 — Autonomous Chat & Human Supervisory Control

/goal Build a **ChatGPT-like Chat experience for Symmetry** that lets humans communicate with and, when necessary, directly control working agents without turning human supervision into a requirement for normal execution.

Chat must be a first-class product surface with at least workspace/global, project, and individual run context. It should allow a user to express new work, discuss existing work, ask for concise status or rationale, give guidance to an active worker, make consequential decisions, and request pause, resume, or cancellation.

The default operating model must favor **high agent autonomy**. Human involvement should be minimized rather than maximized. Ordinary implementation choices, temporary uncertainty, tool selection, file-level decisions, and recoverable errors should not routinely escalate to a human. Human attention should primarily be requested when the agent is genuinely blocked or when a decision is consequential, irreversible, security-sensitive, business/policy-sensitive, unexpectedly expensive, or materially changes the intended product behavior.

Chat must distinguish conversational intent from control intent. Asking a question about a run should not silently mutate or interrupt that run. Guidance that changes execution intent should be explicit and durable. By default, non-emergency guidance to an active worker should be consumed at an appropriate safe execution boundary rather than destructively interrupting an atomic operation in progress. Pause should preserve useful state and stop further autonomous progress cleanly; Stop/Cancel should terminate the active execution while preserving its history and artifacts.

When a human decision is required, the system should present a concise **decision packet** rather than forcing the user to read the complete execution transcript. It should state the decision needed, relevant context, recommended choice when one can be justified, meaningful alternatives, and the consequence of each choice.

The normal Chat timeline must separate human communication from machine execution noise. Human messages, agent summaries, important findings, blockers, decisions, and final results belong in the primary conversation. Raw tool calls, shell commands, model event streams, verbose logs, and detailed execution traces should be collapsed behind progressive disclosure and remain available primarily for diagnosis.

Chat must operate on the same underlying project/work/run domain as the Kanban and run pages. Creating or assigning work through Chat must create or update the corresponding work/run state. A run that completes, becomes blocked, opens a pull request, fails CI, or requires review must be reflected consistently in Chat, Kanban, and run views rather than producing separate conversational state.

Chat messages and control instructions that materially affect execution must be durably persisted; delivery into an OTP mailbox or Phoenix Channel alone is not sufficient evidence of persistence.

Completion is demonstrated by scenarios in which:

* a user can start meaningful work from Chat;
* an agent can continue autonomously for an extended period without repetitive approval prompts;
* the user can ask what is happening without disturbing execution;
* the user can give durable guidance that changes subsequent execution;
* a consequential blocker produces a concise human decision request;
* the user can pause, resume, or cancel a live worker;
* Chat, Kanban, run state, PR/CI information, and durable history remain consistent;
* normal users can understand progress and outcome without reading raw execution traces.

The intended product quality is that successful work usually requires very little human attention: humans define goals, make the few decisions that genuinely require human judgment, and review outcomes; Symmetry keeps the work moving between those points.