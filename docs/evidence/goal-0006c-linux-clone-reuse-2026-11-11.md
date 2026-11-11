# Goal 0006c Linux v2 clone reuse evidence - 2026-11-11

## Status

**Implemented in the working tree; exact-commit and qualified-host native
witness remain pending.** This receipt supersedes only the Linux v2
retained-resume unsupported statement in
`goal-0006c-retained-session-continuity-2026-11-11.md`. It does not promote the
Pi capability: real-provider retained resume on a qualified A/B Linux v2 host is
still required by Goal 0006f.

Baseline `HEAD`: `ca4871b` (`feat(harness): track latest clients and bound
retained continuity`). The code and this receipt are not yet a clean exact
subject at the time of writing.

## Implemented contract

A resumed Linux v2 Goal admission may reuse the exact retained B checkout of a
stopped predecessor only through a new durable grant. The grant binds:

- the source clone intent, seed receipt and checkout receipt;
- the predecessor stop certificate (`RunID`, generation, session, local handle,
  binding, delivery digest, receipt ID);
- the new `RunKey` and the Goal session's workspace fingerprint.

`Store.BindLinuxCloneReuse` is a CAS mutation under the new RunKey fence. It
rejects a source that is not terminal, physically stopped, release-proven and
retained; a Goal session that is not stably stopped or differs from its clone
provenance; a target already reserved or consumed by another reuse owner; and a
non-empty target clone/workspace state. It records no clone intent, seed,
checkout, promotion, abort or cleanup authority in the consuming journal: the
source journal remains the deletion owner.

`Manager.RecoverLinuxCloneReuse` revalidates the source seed and the retained
target device/inode through the existing checkout receipt. It performs no Git,
checkout, promotion or deletion effect. The helper independently revalidates
the target device/inode and requires the target directory to be owned by the
configured fixed-B UID/GID before RELEASE. The native gate receives no clone
callback for a reuse launch; it directly executes in the retained B working
directory. A source cleanup begin is refused while any `LinuxCloneReuseGrant`
aliases its target.

## Changed paths

- `daemon/internal/authority/authority.go`: reuse stop/grant types and canonical
  validation.
- `daemon/internal/state/state.go`: consuming journal field, CAS binding,
  source/session/alias checks, cleanup alias refusal, and saved-journal
  immutability.
- `daemon/internal/workspace/linux_clone_linux.go`: read-only reuse recovery.
- `daemon/internal/app/app.go`: Linux resume recovery, rebind ordering,
  invocation grant, result-subject rejection for reuse candidates.
- `daemon/internal/execution/runner.go` and
  `daemon/internal/execution/linux_owner_v2_linux.go`: mutually exclusive clone
  and reuse invocation authority.
- `daemon/internal/platform/linux_owner_helper_v2_linux.go`: helper RELEASE
  accepts exactly one clone/reuse authority and revalidates the retained target.
- `daemon/native/linux-gate/protocol/launcher_linux.go`: reuse command validation
  and direct-exec (v2) release without checkout authority.

## Checks executed

| Check | Result |
| --- | --- |
| Windows `go test ./internal/authority ./internal/state ./internal/workspace ./internal/execution ./internal/platform ./internal/app` | Pass: authority 1.9s, state 38.5s, workspace 124.0s, execution 20.7s, platform 12.0s, app 129.3s |
| Linux WSL `go test ./internal/authority ./internal/state ./internal/workspace ./internal/platform ./native/linux-gate/protocol` | Pass: authority 0.02s, state 33.4s, workspace 29.8s, platform 30.5s, protocol 2.5s |
| Linux WSL full `go test ./internal/state ./internal/app` | Pass: state 39.6s, app 116.4s |
| Linux WSL `TestBindLinuxCloneReuseRequiresStoppedSourceAndStableSession` | Pass: exact source, stable Goal session, new RunKey CAS and retained target fields |
| Linux WSL `TestLinuxCloneReuseRecoversExactRetainedTargetWithoutEffects` | Pass: real B checkout recovery with unchanged worktree and seed |
| Linux WSL `TestLinuxOwnerHelperReuseGrantBindsNewRunAndSourceCheckout` | Pass: exact reuse RunKey/target binding and wrong-RunKey refusal |
| Linux WSL `TestLinuxOwnerHelperReuseTargetRequiresExactInodeAndBIdentity` | Pass: retained dev/inode and fixed-B UID/GID match; wrong-B refusal |
| Windows/Linux compile `go test -run '^$' ./...` | Pass; Linux run used Go 1.27.4 |

The newly added app test exercises the durable bind and its retained workspace
fields. The helper test exercises the grant-to-RunKey/target matcher. Neither
test creates a real A/B cgroup owner or a native Pi process.

## Not run or not proven

- **No qualified A/B host witness.** No installed fixed-UID/GID A/B systemd host
  or delegated cgroup-v2 environment was available in this session. The helper
  target check, gate direct-exec path and durable CAS are unit/integration
  tested, but the end-to-end helper/gate/native reuse path was not exercised by
  an installed owner.
- **No real-provider Pi 1.5.2 retained resume through daemon/Control.** The
  existing native loopback receipts cover the adapter, not this daemon path.
- **Target replacement after validation remains a point-in-time residual.** The
  helper checks the retained target immediately before RELEASE; it cannot stop a
  writer with B authority from replacing the target between that check and the
  native exec. This is the same local principal that owns the retained
  workspace, not a cross-principal read.
- **Exact commit subject.** The source-subject gate must be rerun after the
  final commit; this receipt intentionally records the pre-commit state only.
