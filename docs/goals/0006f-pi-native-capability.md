# 0006f - Evidence-bounded pi native continuity

Establish the supported capability range for the pinned pi `1.5.2` binary,
including repository work, cancellation, retained resume, compatible fresh
handoff, provider access, and exact session-directory provenance. Every handoff
creates a new native session and every effect remains bound to its durable
control identity.

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client rather than freezing an older release. Evidence recorded
for `0.85.1` or `0.87.1` does not count toward this goal's completion for
`1.5.2`. Pi `1.5.2` RPC is JSONL with `agent_settled` as the settled boundary;
the adapter must be revalidated rather than inheriting older protocol claims.
Oh My Pi and OmO are separate harness goals with their own pins.

Adapter-level retained resume has deterministic loopback Windows and Linux
receipts for 1.5.2 (`TestNativeRepositoryTaskRetainedResume`). They do not prove
real-provider authentication, daemon/Control durability, exactly-once effects,
or Linux v2 clone reuse; those remain completion requirements.

Completion requires exact-binary Linux and Windows evidence for each promoted
operation, restart and lost-acknowledgement recovery, no duplicate effects,
artifact and Subject integrity, usage classification, and independent review of
the final revision.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, provider identity, repository, and permitted spend
are available; the 12-hour bound starts after all prerequisites are ready. It
depends on 0006a-0006c and produces one independently mergeable PR.
