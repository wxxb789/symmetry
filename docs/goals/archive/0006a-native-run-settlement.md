# 0006a - Single-owner native run settlement

Make each native Run have one durable settlement owner across process start
publication, terminal observation, cancellation, lease expiry, usage capture,
output delivery, and cleanup. A late actor cannot overwrite an observed result,
double-complete a Run, lose usage, or hide discarded output.

Completion is demonstrated by deterministic race and fault tests for terminal
versus cancel, lease expiry during start, usage persistence failure, output
truncation, repeated completion, and recovery after an unknown write. Applicable
Go tests, race checks, and Windows-native checks pass on the exact revision.

This goal does not change harness capability declarations, retained-session
handoff, or provider policy. It is one independently mergeable PR and must fit
one agent run of at most 12 hours while leaving unsupported paths fail-closed.
