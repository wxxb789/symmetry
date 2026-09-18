# Goal 0006 Current Validation Evidence - 2026-09-18

## Verdict

Goal 0006 remains **active** and **incomplete**. This receipt records bounded
local evidence at commit `97e7924f6085435df4ffa0fb3680c26b235ee067`.
The checkout is dirty, so this is current dirty-subject evidence, not a final
exact-Subject acceptance receipt. Tests, reviews, evidence documents, and ignore
rules do not grant repository, provider, hosted, or operator authority.

## Current Git snapshot

| Field | Value |
| --- | --- |
| Branch | `codex/goal-0006-durable-engineering-work` |
| `HEAD` | `97e7924f6085435df4ffa0fb3680c26b235ee067` |
| `HEAD^{tree}` | `260107cea5c3b4a6fdd67849d6fc4578e7538d30` |
| Index tree | `260107cea5c3b4a6fdd67849d6fc4578e7538d30` |
| Staged paths | `0` |
| Tracked dirty paths | `122` |
| Visible untracked paths | `108` |
| Status lines with all untracked files | `230` |
| Committed canonical tree digest | `sha256:b0ce798877e945a0441017d0bc073eae542fe7434865be72c5e36489b41e6e5c` |

The committed tree digest is not a complete Subject. No approved repository
`resource_id`, current complete Subject tuple, or current `subject_hash` exists,
and the dirty checkout cannot be the final accepted revision.

## Commit-specific evidence

| Commit | Evidence | Boundary |
| --- | --- | --- |
| `cbb9320` | Ignores Python caches, scratch files, local daemon executables, and the accidental root `-` archive. A Sol `xhigh` review confirmed that no tracked source or evidence path became ignored. | Repository hygiene only. |
| `de0d0f4` | Adds a locked copy snapshot for concurrent native-session call assertions. Linux Docker `go test -race -count=20` passed for both affected tests; Sol `xhigh` approved the exact two-file staged patch. | Test-fixture race repair only. |
| `97e7924` | Persists provider-action intent and completion state, preserves exact replay, rejects unsafe Control results, and reserves worst-case legal completion capacity. | Local durable behavior only; not credentialed provider proof. |

## Provider journal validation

The provider patch is bound to base
`de0d0f4eef740290facc10047e62621747cf6c0e` and patch SHA-256
`262C6377B2B33A0D574F8973709B48856250FE7770A3BC84AC17B4D788CDF665`.
Its closure is exactly 12 files with 3,009 insertions and 81 deletions.

Verified behavior includes:

- JSON credential scanning uses `UseNumber` and fails closed on malformed or
  trailing data. A Unicode-escaped capability token beside `1e10000` is not
  returned, persisted, or leaked by restart replay.
- Typed Control response fields reject non-exact case aliases before struct
  decoding while additive top-level fields and nested provider-owned JSON stay
  compatible.
- Dispatch and mutation capacity checks reserve the worst legal 256-byte
  `FailureCode` JSON representation. Tests distinguish the exact 1,280-byte
  difference from the former ASCII sentinel and verify maximum unknown
  completion persistence across restart.
- Windows focused tests, affected package tests, `go vet`, compile-only tests,
  and daemon build passed.
- Linux Docker `go test -race -count=1 ./internal/state ./internal/control
  ./internal/protocol ./internal/app` passed; the app package took `450.883s`
  in the independent final verification.

The final Sol `xhigh` integration receipt and Astra `xhigh` critical-review
receipt both returned `approve`, bind the same base and patch SHA-256, and are
not superseded. Their approval covers this provider-journal slice only.

## Evidence boundaries

No post-`97e7924` hosted CI run or complete clean-tree Linux/Windows gate is
claimed. Prior Codex, Claude Code, Pi, OpenCode, Control, Contracts, containment,
and recovery receipts remain bound to their named commits or dirty snapshots.

Completion still requires:

- a final committed complete Subject with approved repository `resource_id`,
  canonical Subject hash, and exact validator/reviewer receipts;
- hosted Linux and Windows validation for that exact Subject;
- credentialed Codex, Claude Code, Pi, and OpenCode identity, usage,
  billing/accounting, and provider readback evidence;
- verified native Codex and Claude Code lifecycle evidence within the claimed
  ranges;
- provider-owned OpenCode terminal success/error/interruption evidence and the
  exact Linux binary for any Linux capability claim; and
- authorized operator acceptance and `achieve` after every predicate is met.

Goal 0006 remains active.
