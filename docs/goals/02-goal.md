## Goal 2 — Engineering Workspace & Project Portal

/goal Build the **Symmetry web portal** into a polished, agent-native software engineering workspace rather than an administrative dashboard.

At completion, an engineering team must be able to use the portal as its daily control surface for projects, work items, Kanban, agents, runtimes, active runs, code changes, reviews, CI state, and external connections without needing to understand the internal orchestration machinery.

The portal must provide a first-class project/workspace model and a high-quality Kanban/work-management experience. Work items must be easy to create, inspect, prioritize, move between states, assign to a human or an agent, and associate with execution runs, repositories, pull requests, CI results, blockers, and review state.

The design should have the information density, responsiveness, hierarchy, consistency, and interaction quality expected from modern engineering products such as Linear, GitHub, Raycast, or similarly polished tools. This is a qualitative acceptance criterion: the portal should feel like a deliberately designed product for daily engineering work, not a collection of backend administration forms.

Agent execution must appear as a natural property of work rather than as a separate subsystem. A work-item card or detail view should make its meaningful execution state understandable at a glance: who or what owns it, whether an agent is working, whether it is blocked, whether a change/PR exists, whether CI passed, and whether human review is required.

Run details must use progressive disclosure. The default view should emphasize outcome-level information such as goal, current meaningful phase/status, important findings, changed artifacts, tests, PR/CI state, blockers, and completion result. Raw tool calls, shell output, verbose model events, and detailed execution traces must remain available for debugging but should not dominate the normal experience.

Projects must be able to attach multiple engineering resources rather than being hard-bound to a single repository. A project may reference work tracking, repositories, CI systems, agents, runtimes, and external provider connections independently.

The portal must expose connection health, runtime health, execution health, and synchronization problems clearly enough that a user can distinguish “agent is thinking/working” from “runtime offline”, “connection broken”, “CI failed”, “waiting for human decision”, or “orchestrator fault”.

Completion is demonstrated by a coherent end-to-end user experience in which a user can enter a project, manage its Kanban, assign work to an agent, observe execution without being flooded by low-level events, inspect the resulting code/PR/CI state, and move the work through review to completion entirely through the portal.

