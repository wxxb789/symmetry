# 0006k - Evidence-bounded OmO native capability

Establish the supported capability range for the pinned OmO
(`oh-my-openagent`) `5.1.68` native binary, including repository work,
cancellation, retained resume, compatible fresh handoff, provider access where
separately verified, and exact session-directory provenance. Every handoff
creates a new native session and every effect remains bound to its durable
control identity.

OmO Native runs on the senpi engine, a Pi-derived fork. Its `omo --mode rpc`
surface is the primary integration target; the `omo app-server` Codex protocol
and the OpenCode-plugin edition are not assumed to be equivalent. The adapter
must probe the exact installed binary and protocol rather than inheriting Pi
capability claims. Owner directive 2026-11-11: until the first prototype and
release, track the latest stable client rather than freezing an older release.

Completion requires exact-binary Linux and Windows evidence for each promoted
operation, restart and lost-acknowledgement recovery, no duplicate effects,
artifact and Subject integrity, usage classification, and independent review of
the final revision. An unknown or incompatible protocol version remains
fail-closed and cannot advertise the affected operation.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, provider identity, repository, and permitted spend
are available; the 12-hour bound starts after all prerequisites are ready. It
depends on 0006a-0006c and produces one independently mergeable PR.
