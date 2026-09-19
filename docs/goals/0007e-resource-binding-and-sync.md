# 0007e - Project resource binding and truthful synchronization

Let users bind repositories, work tracking, and CI resources from configured
GitHub or Azure DevOps connections to a project, inspect their capabilities, and
synchronize provider-owned state without presenting stale or unknown outcomes as
success. Work assignment uses only the selected authorized resources.

Completion evidence includes real-API and browser tests for resource discovery,
creation, update, deletion, synchronization, project switching, concurrent
refresh, provider failure, unknown outcome, and assignment eligibility. The UI
must preserve provider-owned fields and exact resource identity.

This goal depends on 0007a and 0007d. It excludes provider policy duplication,
runtime control, Chat, and Goal-specific UI, is one independently mergeable PR,
and must fit one agent run of at most 12 hours.
