# 0006e - Evidence-bounded Claude Code native capability

Establish the exact supported native capability range for Claude Code `2.1.322`
through real repository and session work. Terminal, cancellation, artifact, and
usage outcomes are authoritative only when the native protocol proves them;
unverified resume, pause, approvals, or provider operations remain unsupported.

Completion requires exact-binary Linux and Windows lifecycle evidence, bounded
failure and cancellation behavior, credential and provider identity, usage
classification, and independent review tied to the final commit Subject.
Transport-only or synthetic success is insufficient.

Claude Code 2.1.322 emits `system/init` only after the first user frame. The
adapter must not block `Open` on that record; it must reject every terminal
result until a matching init identity has been observed.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, repository, and permitted spend are available;
the 12-hour bound starts after all prerequisites are ready. It depends on 0006a
and 0006b and produces one independently mergeable PR.

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client rather than freezing an older release. `2.1.259` and
`2.1.281` observations remain historical evidence only.
