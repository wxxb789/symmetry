# 0006d - Evidence-bounded Codex native capability

Establish the exact supported native capability range for the pinned Codex
`0.168.0` binary and expose only operations proved against a real repository.
Unsupported permission, session, terminal, provider, or usage behavior remains
explicitly unavailable rather than inferred from process exit or transport.

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client rather than freezing an older release. The pin advanced to
`0.168.0` on 2026-11-13. Evidence for `0.153.4`, `0.156.1`, `0.167.1` or
`0.167.2` — including the 0.167.2 Windows lifecycle, cancellation, artifact and
usage observations — is historical and does not count toward this goal's
completion.

Current state: probe and promotion are bound to the exact executable bytes.
Native capability promotion now requires the resolved executable SHA-256 to
equal the tested blob as well as the exact version string and schema digest, so
a different binary with the same version/schema fails closed. The tested
`0.168.0` executable blob (`sha256:ad8296f7…`) and its changed v2 schema bundle
(`sha256:893cc237…`) are recorded under `testdata/codex/0.168.0/`. Windows
start/events/cancel are re-proved for that exact blob: transport containment,
a credentialed repository turn with canonical TaskResult and artifact,
cancellation, cumulative-counter usage observations, and the isolated-home
read-only negative case are recorded in
[the 2026-11-13 observation](../../daemon/internal/harness/testdata/codex/0.168.0/native-windows-2026-11-13.md).
The probe additionally tolerates the benign leading `WARNING:` alias line that
Codex 0.168.0 writes to stderr when it refuses its own PATH helper, without
weakening the anchored version gate. Linux stays blocked by the
containment-owner feasibility conflict: shell tool children detach into their
own session, so the Linux process-group owner cannot prove containment; the
smallest recorded amendment is the existing Linux v2 cgroup owner (per-run
cgroup leaf, `cgroup.kill`). A Linux daemon rejects Codex native admission.

Completion requires exact-binary Linux and Windows evidence for the claimed
repository lifecycle, cancellation, result, artifact, and usage behavior,
including negative permission and stale-session cases. Registry promotion is
limited to those operations and is reviewed against the exact commit Subject.

This goal starts only when both Linux and Windows native runners, the pinned
binary, approved credentials, provider identity, repository, and permitted spend
are available; the 12-hour bound starts after all prerequisites are ready. It
depends on 0006a-0006c and produces one independently mergeable PR.

## Done when

- Windows promotion stays re-proved at the `0.168.0` pin for the exact tested
  executable blob: `start`, `events`, `cancel`, result, artifact, and usage
  classification behave on a real repository, and the negative cases
  (isolated-home `readOnly` rejection, permission downgrade refusal,
  closed/stale session rejection) still fail closed.
- Linux carries exact-binary evidence under the Linux v2 cgroup owner: the same
  repository lifecycle, cancellation, result, artifact, and usage behavior runs
  against a real repository with proven containment, and the same negative
  permission and stale-session cases fail closed. If containment for an
  operation cannot be proven, exclude the operation, keep it explicitly
  unsupported, and report the exact conflict with its evidence instead of
  promoting it.
- Only proved operations are promoted, the promotion stays bound to the exact
  tested executable blob, every unverified operation (resume, handoff, guidance,
  pause, approval response, provider access, billable usage, hard cost limits)
  stays explicitly unsupported, and the evidence binds the exact binary hashes,
  platform, and revision.
- Existing supported behavior remains correct and the PR is independently
  mergeable without an unmerged sibling.

Stop and report instead of continuing if, at the start of the run, the pinned
`0.168.0` binary, approved credentials, provider identity, repository, or
permitted spend is missing, or if a Linux containment proof would require
silently changing a fixed design decision. After that, take and report
reversible defaults; stop only for irreversible or out-of-bounds decisions.
End with a report that pairs each claimed operation with its exact evidence and
lists what remains unverified.
