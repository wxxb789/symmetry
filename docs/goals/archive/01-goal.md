## Goal 1 — Core Orchestration & Execution Plane

/goal Build the core of **Symmetry** as a reliable agent orchestration and execution platform.

At completion, Symmetry must provide a durable control plane that can register execution machines and agent runtimes, accept work, dispatch that work to an appropriate runtime, supervise its execution, recover from normal failures, and preserve an authoritative history of what happened.

The server/control plane must use **Elixir/OTP/Phoenix**. The execution daemon must be a separate **Go** program and must not require BEAM. **PostgreSQL** is the authoritative durable store for control state, task/run state, leases, execution history, session metadata, and other business truth. **Ecto** is the primary persistence layer. **Oban** may own durable background work such as cleanup, reconciliation, asynchronous synchronization, scheduled maintenance, and retries that do not constitute the live agent-execution ownership model.

The daemon communicates with the control plane through a language-neutral network protocol over a persistent connection. Normal dispatch should have low latency through server notification, while task claiming and a polling/reconciliation fallback must make the protocol self-healing after missed messages or connection interruptions.

A runtime must have a clearly observable lifecycle including online/offline state, heartbeat, available capacity, active runs, disconnect, reconnect, and reclaim behavior. A task/run must have a durable lifecycle that distinguishes queued work, assigned/claimed work, active execution, completion, failure, cancellation, and cases that are waiting for consequential human input.

Live runtime and run coordination may use OTP processes, supervisors, PubSub, Channels, or Presence, but live BEAM state must never be treated as durable truth. Restarting a server process or the entire BEAM node must allow useful live state to be reconstructed from PostgreSQL and connected daemons without silently losing durable work.

Execution ownership must be robust against duplicate delivery, reconnects, retries, delayed messages, stale workers, and multiple server instances. A stale execution must not be able to overwrite the result of a newer execution. The system must have an explicit durable ownership/version mechanism such as leases, execution generations or fencing semantics, together with idempotent state transitions.

A daemon must be able to launch a configured coding-agent CLI on its own machine, prepare and isolate the task workspace as required by the selected workspace policy, stream meaningful execution events to the control plane, terminate or cancel work safely, and report final results. Coding-agent credentials and machine-local repository credentials should remain on the execution machine unless a specific integration explicitly requires otherwise.

Completion is demonstrated by end-to-end scenarios showing that:

* a daemon can connect, register its available runtime(s), and become schedulable;
* work created in Symmetry is dispatched and claimed without human babysitting;
* the daemon executes a real configured coding agent and reports progress and final outcome;
* capacity limits prevent over-dispatch;
* temporary network loss does not permanently lose queued work;
* daemon reconnect can re-register and reconcile unfinished work;
* server process and full server restarts reconstruct durable task/run state correctly;
* retries or delayed messages cannot cause two different execution generations to both become authoritative;
* cancellation terminates the live execution while preserving the durable audit/history needed to understand what occurred;
* normal failures of one runtime/run do not crash or corrupt unrelated runtime/run state.

The core should remain lean: do not reproduce a general workflow engine, distributed database, or message broker where PostgreSQL plus OTP primitives already provide the required semantics.
