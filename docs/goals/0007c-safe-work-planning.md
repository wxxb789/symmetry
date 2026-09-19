# 0007c - Safe project and work planning

Let users create, edit, archive, and restore projects and create, edit, order,
and move work items without duplicate submissions, lost drafts, or silent
overwrite of concurrent changes. Scoped view preferences remain attached to the
correct project and view.

Completion evidence includes real-API browser tests for successful mutations,
idempotent retries, optimistic-lock conflicts, cross-project response races,
refresh while editing, keyboard board movement, focus restoration, validation
errors, and recovery without corrupting visible state.

This goal excludes provider connections, resource synchronization, runtime
control, Chat, and Goal-specific operations. It depends on 0007a and 0007b, is
one independently mergeable PR, and must fit one agent run of at most 12 hours.
