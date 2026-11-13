# 0006e - Evidence-bounded Claude Code native capability

Establish the exact supported native capability range for Claude Code `2.1.323`
through real repository and session work. Terminal, cancellation, artifact, and
usage outcomes are authoritative only when the native protocol proves them;
unverified resume, pause, approvals, or provider operations remain unsupported.

Completion requires exact-binary lifecycle evidence for every promotion, bounded
failure and cancellation behavior, credential and provider identity, usage
classification, and independent review tied to the final commit Subject.
Transport-only or synthetic success is insufficient.

Claude Code `2.1.322` emits `system/init` only after the first user frame. The
adapter must not block `Open` on that record; it must reject every terminal
result until a matching init identity has been observed. `2.1.323` inherits that
rule until the run re-proves the init ordering for its own exact binary.

This goal depends on 0006a and 0006b and produces one independently mergeable PR.

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client rather than freezing an older release. `2.1.259`, `2.1.281`
and `2.1.322` observations remain historical evidence only.

## Current state (operator record, 2026-11-13)

The pin advances to the installed latest stable `2.1.323` on 2026-11-13 for the
same reason Codex advanced to `0.168.0`: a newer stable client supersedes the
captured fixtures, and older observations — including the 2.1.322 Windows
structured-output receipt — cannot satisfy the new pin.

What exists on `main`: a registered Claude adapter that probes `--version` and
`--help` and then keeps every native capability unsupported, plus an
unregistered lifecycle candidate
(`daemon/internal/harness/claude_candidate_adapter.go`) whose `Start`, decode,
structured-output `TaskResult` and process-cancel paths are exercised only
against loopback fixtures. `testedClaudeVersion` is `2.1.322`, and the Windows
receipt for it was captured against a loopback fake Messages API, not a real
repository turn.

The operator decision this goal records: promotion is bound to the exact
executable blob, exactly as the Codex promotion is. The resolved executable
SHA-256 must equal the tested blob in addition to the exact version string and
the committed fixture digests, so a different binary with the same version
string fails closed.

## Linux disposition (operator decision, 2026-11-13)

The Linux v2 owner path requires an installed A/B systemd runtime with a
delegated cgroup-v2 parent plus cross-UID peer inspection (`pidfd_getfd`,
`/proc/<pid>/fdinfo`). The hosts available for this goal do not provide that:
the recorded WSL2 observation denies exactly those inspections, and no other
Linux runner is reachable. Per the goal's own failure rule and the Codex
precedent, Linux admission therefore stays fail-closed and explicitly
unsupported, the blocking conflict is recorded with its evidence, and Windows
evidence is never promoted as Linux support. Linux evidence remains a
requirement of `0006-final` under that goal's full-series contract.

## Done when

- The pin is `2.1.323` and every promotion is bound to the exact installed
  executable: resolved executable SHA-256, exact `--version` string, and the
  committed fixture digests. The tested blob, its digest and the fixture digests
  are recorded under `daemon/internal/harness/testdata/claude/2.1.323/`, and the
  2.1.322 directory is marked historical without losing its files.
- Windows native evidence on a real repository proves each promoted operation —
  `start`, `events`, structured-output `result` with its artifact, and `cancel` —
  with the deferred `system/init` rule enforced: no terminal result is accepted
  before a matching init identity, and `Open` does not block on that record.
- Negative controls are recorded and fail closed: a version or blob mismatch, an
  executable without the print/stream-json transport, a terminal result without
  init identity, and a closed or superseded session.
- Only proved operations appear in the capability projection. Resume, handoff,
  guidance, pause, approval response, provider access, billable usage and hard
  cost limits stay explicitly unsupported unless this run proves them for the
  exact blob.
- Linux admission stays fail-closed with the recorded conflict; no Linux
  capability is advertised from Windows evidence.
- The evidence record binds the exact binary hashes, platform and the final
  commit Subject, and the goal, design and testdata documents are reconciled
  with the new pin. Existing supported behavior stays correct and the PR is
  independently mergeable without an unmerged sibling.

Stop and report instead of continuing if the pinned `2.1.323` binary, approved
credentials, provider identity, repository, or permitted spend is missing at the
start of the run, or if a promotion would require silently changing a fixed
design decision. After that, take and report reversible defaults; stop only for
irreversible or out-of-bounds decisions. End with a report that pairs each
claimed operation with its exact evidence and lists what remains unverified.
