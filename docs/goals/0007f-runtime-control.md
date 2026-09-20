# 0007f - Runtime visibility and trustworthy execution control

Let users inspect machines and runtimes with their verified capabilities and
control work from the detail view through start, cancel, retry, guidance, and
input flows. Requested, queued, applied, rejected, failed, and unknown receipts
remain visibly distinct across generations.

Completion evidence includes live-daemon and browser tests for assignment,
waiting input, cancellation/completion races, retry, stale commands, unsupported
capabilities, history pagination, refresh, disconnect, and reconnect. UI state
must reconcile from server snapshots without inventing transition authority.

This goal excludes native capability promotion, provider connection management,
Chat, and Goal-specific screens. It depends on 0007a and 0007b, is one
independently mergeable PR, and must fit one agent run of at most 12 hours.
