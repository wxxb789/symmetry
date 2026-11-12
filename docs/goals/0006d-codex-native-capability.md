# 0006d - Evidence-bounded Codex native capability

Establish the exact supported native capability range for the pinned Codex
`0.167.1` binary and expose only operations proved against a real repository.
Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client rather than freezing an older release. The earlier
`0.153.4` and `0.156.1` observations remain historical evidence only.
Unsupported permission, session, terminal, provider, or usage behavior remains
explicitly unavailable rather than inferred from process exit or transport.
The 0.167.1 schema digest and initialize/thread-start transport are observed;
`thread/start` still returns `readOnly` for a requested `workspace-write`, so
the adapter rejects the session and promotes no native capability.
The Linux smoke reached the same transport but failed closed without
containment evidence during close; that failure is not a promoted capability.

Completion requires exact-binary Linux and Windows evidence for the claimed
repository lifecycle, cancellation, result, artifact, and usage behavior,
including negative permission and stale-session cases. Registry promotion is
limited to those operations and is reviewed against the exact commit Subject.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, provider identity, repository, and permitted spend
are available; the 12-hour bound starts after all prerequisites are ready. It
depends on 0006a-0006c and produces one independently mergeable PR.
