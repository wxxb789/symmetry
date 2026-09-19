# Goal 0006 Completion Matrix - 2026-09-18

## Overall classification

**Verdict: Goal 0006 remains active and incomplete.** Current `HEAD` contains
additional committed hygiene, test-fixture, and provider-journal work, but the
checkout is dirty and still lacks exact-Subject provenance, hosted validation,
credentialed provider evidence, native capability promotion, and authorized
operator acceptance.

Current Git evidence is recorded in
`goal-0006-current-validation-2026-09-18.md`: `HEAD` is `97e7924`, there are
zero staged paths and the index tree equals the `HEAD` tree, and the worktree
has 122 tracked dirty paths plus 108 visible untracked paths. The committed
tree digest is not a complete Subject.

## Requirement matrix

| Area | Status | Current evidence | Missing proof |
| --- | --- | --- | --- |
| Durable Goal, Task, Run, lease, decision, and recovery transitions | proved-current-dirty | Existing Control and daemon tests plus committed recovery hardening cover bounded retry, stale fences, terminal idempotency, interruption, and process authority. | Final exact-Subject and hosted validation. |
| Compatible native Codex, Claude Code, Pi, and OpenCode work | incomplete | Bounded local candidate and adapter evidence exists for named versions; unsupported operations remain explicit. | Credentialed real repository lifecycle evidence and capability promotion for each claimed range. |
| Linux and Windows native support | incomplete | Named local Windows checks and Linux Docker package/race checks pass for bounded slices. | Hosted checks on the final exact Subject and exact native binaries. |
| Preserved artifacts, versioned context, resume, and handoff | proved-current-dirty | Local tests cover retained session identity, fresh handoff identity, context binding, and unsupported cross-harness behavior. | Final cross-machine artifact and acceptance receipts. |
| Duplicate/lost acknowledgement and stale authority handling | proved-current-dirty | Journal, transition, lease, containment, and replay tests reject stale owners and reuse durable receipts. | Hosted restart matrix on the final Subject. |
| Unknown provider effects and durable readback state | external-blocked | `97e7924` adds fail-closed provider-action journal persistence, exact replay, redaction, and unresolved-liability recovery tests. | Credentialed provider readback, identity, billing, and accounting evidence. |
| Usage, liability, and budget semantics | proved-current-dirty | Local persistence retains unknown usage/liability and bounded completion data across recovery; Control tests cover reservations and limits. | Provider-owned usage and strict ceiling provenance. |
| Exact evidence, validation, and accepted work | external-blocked | Local schemas and tests separate run success, candidate completion, validation, accepted work, and achieved Goal. | Approved repository Subject, every predicate receipt, independent validator/reviewer binding, and integration acceptance. |
| Protocol v1 and goal-less compatibility | proved-current-dirty | Additive contracts, provider ownership, authentication boundaries, and goal-less paths remain covered by bounded tests. | Clean final-subject and hosted compatibility reruns. |
| PostgreSQL as sole durable control authority | proved-current-dirty | Additive migrations, transaction ownership, local backup/restore evidence, and daemon-local journal boundaries remain explicit. | Hosted disaster-recovery evidence on the final release subject. |
| Required quality and release gates | incomplete | `cbb9320`, `de0d0f4`, and `97e7924` have scoped Sol receipts; the provider slice also has an Astra critical-review approval and a full affected-package Linux race pass. | Complete clean-tree gates, hosted artifacts, exact-Subject receipts, and authorized release path. |
| Final Goal acceptance | external-blocked | Tests preserve the operator-required completion boundary and do not self-accept. | Authorized operator review and `achieve`. |

## Current commit provenance

- `cbb9320`: scoped ignore rules; no source or evidence path hidden.
- `de0d0f4`: synchronizes active native-session call assertions; targeted Linux
  race tests passed 20 iterations.
- `97e7924`: commits the reviewed 12-file provider-journal slice. Sol `xhigh`
  integration verification and Astra `xhigh` critical review both approved the
  exact base and SHA-256
  `262C6377B2B33A0D574F8973709B48856250FE7770A3BC84AC17B4D788CDF665`.

The 2026-09-17 matrix is superseded for current Git counts by this receipt, but
its test and provider receipts remain bound to their named commits or dirty
snapshots. Any later source or evidence commit requires fresh Subject,
predicate, and review binding.

The implementation has meaningful proved-current-dirty progress, but missing
live environments and acceptance evidence remain incomplete rather than being
replaced by synthetic or documentation success. Goal 0006 remains active, not
achieved.
