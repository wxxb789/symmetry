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

## Prerequisite audit (operator record, 2026-11-14)

Two of the prerequisites this goal names are not available on the hosts this
series runs on, and neither appears by waiting:

- **Linux native runner.** The recorded conflict from 0006d/0006e still holds:
  no installed A/B systemd runtime, no delegated cgroup-v2 parent, and no
  cross-UID `pidfd_getfd`/`fdinfo` peer inspection. The "Linux v2 retained
  resume" prerequisite this goal names therefore stays explicitly unpaid — a
  fixture-workspace resume is not clone reuse.
- **Approved credentials, provider identity and permitted spend.** This
  environment has no approved non-production provider resource;
  `docs/goals/0006h-credentialed-provider-accounting.md` owns that work. The
  existing opt-in tests run against an operator-provided loopback gateway, and
  this repository ships no gateway starter, so loopback receipts remain
  transport-level observations — which is what this goal already says they are.

The unaffected authorized work is therefore the Windows exact-binary promotion,
the fail-closed negative controls, and the projection discipline. The credentialed
and Linux clauses are recorded as unpaid prerequisites owned by 0006h and
0006-final, and no claim in this increment may depend on them.

## Current state (operator record, 2026-11-14)

The pin stays at the installed latest stable `1.5.2` (upstream `v1.5.2`, released
2026-11-10); the previous `0.85.1` and `0.87.1` observations remain historical.

What exists on `main`: the Pi probe parses `pi --version`, requires exactly
`1.5.2`, requires the `--mode rpc` help surface, and then still returns
`ErrNativeUnverified` — no operation is promoted today. The opt-in native tests
(`SYMMETRY_PI_NATIVE_REPOSITORY_TASK`, `SYMMETRY_PI_NATIVE_CANCELLATION`, and
`TestNativeRepositoryTaskRetainedResume`) exercise the adapter-level lifecycle
against a loopback gateway the operator supplies (`…_BASE_URL` non-empty selects
it, and it is validated to require provider `symmetry-native-loopback`, model
`gpt-5.6-terra` and an empty credential env). The checked-in 1.5.2 receipts are
consequently transport, binding and retained-resume observations, not credentialed
provider work. The Control E2E covers compatible fresh handoff in source, with
Windows positive scenarios selected in CI and Linux running the installed-owner
refusal instead.

## Linux disposition (operator decision, 2026-11-14)

Same recorded conflict as 0006d/0006e. Linux admission stays fail-closed with its
evidence recorded; no Windows observation is promoted as Linux support.

## Done when

- Every promotion is bound to the exact resolved `pi` executable (SHA-256), the
  exact `1.5.2` version line, and the committed fixture digests recorded under
  `daemon/internal/harness/testdata/pi/1.5.2/`. The digest command must be
  reproducible from the committed revision — computed from the increment's base
  commit to its final revision, never from a working tree.
- Windows evidence on a real repository proves each promoted operation: a
  repository task with its artifact and structured result, cancellation that
  drains the in-flight turn, and retained resume across a process boundary —
  recorded against the exact binary and the final commit Subject. Each observation
  must compare against an independently derived expectation, not against a
  restatement of what the adapter was given.
- The credentialed turn uses a real provider identity through the existing
  allowlisted credential-variable path; a loopback gateway alone remains
  insufficient, and unavailable credentials are reported as an unverified
  operation rather than replaced by synthetic success.
- Negative controls fail closed and are recorded: version or blob mismatch, an
  executable without the RPC transport, a resume handle for an incompatible
  version, and a stale or superseded session. The probe's wrong-version gate
  currently has no dedicated rejection test — every fixture uses `TestedVersion` —
  so this increment adds one, in the shape of the Claude increment's
  `TestClaudeProbeUnknownVersionFailsClosed`.
- Only proved operations appear in the capability projection; guidance, pause,
  approval response, provider access, billable usage and hard cost limits stay
  explicitly unsupported unless this run proves them for the exact blob.
- Restart and lost-acknowledgement recovery, no duplicate effects, artifact and
  Subject integrity, and usage classification are each either proved or recorded
  as unverified with the exact blocker named.
- Existing supported behavior stays correct and the PR is independently mergeable.
