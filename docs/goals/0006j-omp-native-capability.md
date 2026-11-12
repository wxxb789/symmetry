# 0006j - Evidence-bounded Oh My Pi native capability

Establish the supported capability range for the pinned Oh My Pi (OMP) `18.18.7`
binary, including repository work, cancellation, retained resume, compatible
fresh handoff, provider access where separately verified, and exact
session-directory provenance. Every handoff creates a new native session and
every effect remains bound to its durable control identity.

OMP is a separate harness, not an alias for Pi. Its `omp --mode rpc` transport
is a custom newline-delimited JSON protocol with a `ready` frame, protocol
version negotiation, `prompt_result`, and `session_settled`; it must not inherit
Pi capability claims or decoder assumptions. Owner directive 2026-11-11: until
the first prototype and release, track the latest stable client rather than
freezing an older release.

Completion requires exact-binary Linux and Windows evidence for each promoted
operation, restart and lost-acknowledgement recovery, no duplicate effects,
artifact and Subject integrity, usage classification, and independent review of
the final revision. An unknown or incompatible protocol version remains
fail-closed and cannot advertise the affected operation.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, provider identity, repository, and permitted spend
are available; the 12-hour bound starts after all prerequisites are ready. It
depends on 0006a-0006c and produces one independently mergeable PR.
