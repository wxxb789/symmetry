# 0006d - Evidence-bounded Codex native capability

Establish the exact supported native capability range for the pinned Codex
`0.153.4` binary and expose only operations proved against a real repository.
Unsupported permission, session, terminal, provider, or usage behavior remains
explicitly unavailable rather than inferred from process exit or transport.

Completion requires exact-binary Linux and Windows evidence for the claimed
repository lifecycle, cancellation, result, artifact, and usage behavior,
including negative permission and stale-session cases. Registry promotion is
limited to those operations and is reviewed against the exact commit Subject.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, provider identity, repository, and permitted spend
are available; the 12-hour bound starts after all prerequisites are ready. It
depends on 0006a-0006c and produces one independently mergeable PR.
