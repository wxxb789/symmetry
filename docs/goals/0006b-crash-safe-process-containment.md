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

The Windows production witness also exercises a structurally valid wrong-secret
request as the first named-pipe client after the inherited bootstrap. The helper
must reject it without changing the Job or target, and the legitimate hello
must then complete within the same absolute deadline.

The Linux guarantee is bounded to the original pidfd-owned process group.
Observed group escapes, scan failures, and other unresolved containment remain
durable and fail closed; descendants that escape the original Unix group
without being observed are outside this goal's verified boundary.

This goal excludes harness-specific terminal semantics and capability promotion.
It is one independently mergeable PR, preserves journal compatibility, and must
fit one agent run of at most 12 hours.
