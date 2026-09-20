# 0006b - Crash-safe process containment

Make process authority survive daemon or owner failure before and after native
launch without orphaning a child, resuming an unauthorized child, duplicating
ownership, or clearing containment before durable stop proof exists. Windows
supervisor handoff and Linux descendant cleanup remain platform-correct.

Completion evidence covers prepare, bind, commit, lease arm, resume, stop
receipt, release, and clear boundaries, including lost-response replay and hard
crashes at each externally visible point. Production-binary witnesses and
native platform tests show bounded cleanup and preserve unresolved state when
physical stop cannot be proven.

This goal excludes harness-specific terminal semantics and capability promotion.
It is one independently mergeable PR, preserves journal compatibility, and must
fit one agent run of at most 12 hours.
