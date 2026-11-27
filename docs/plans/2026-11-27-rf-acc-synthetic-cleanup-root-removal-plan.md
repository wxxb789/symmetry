# rf-acc-synthetic-cleanup-root-removal — implement plan

Contract: `goal-rf-acc-synthetic-cleanup-root-removal.md` (GDD state root, outside the repository).
Dispatch base: `d4e455f1` (main). Branch: this goal worktree. The accepted plan is the contract;
this file resolves the choices it leaves open (which layer the stop belongs to, the fixture
mechanism, the committed pins) and points at the measured evidence. Clause-by-clause results:
evidence record `docs/evidence/rf-acc-synthetic-cleanup-root-removal-2026-11-27.md`.

## 1. Clause census at the dispatch head

| Contract clause | Tree state at the dispatch head | This increment |
| --- | --- | --- |
| Change 1: root cause of the anchor's missing exact root removal proof, with file:line evidence and the named layer | Bind 2 (`45938637085` on `f3dbae66`) records only `production_linux_v2_cleanup_witness_test.go:882: … Linux owner anchor returned no exact root clone-removal proof`; the owner-service journal holds no worker detail (findings `304-binding-run-2.md` §2–3) | §2 of this plan + evidence §2–§3: the fixture's clone-root default ACL mask makes every B-created target/ancestor group-writable; the anchor's first target check refuses; the anchor maps the refusal to `clone_cleanup_unproven`, which the client renders as the generic line |
| Change 1: discriminate real removal / swallowed refusal / wrong subject / permission failure / proof-shape mismatch | The response status is not carried into the client error (`linux_owner_anchor_linux.go:81`), and the anchor logs no worker error | Evidence §3: reproduced on the resolute replica with the production checks — target shape and the RunKey walk refuse on the exact Bind-2 fixture shape; the State verifier's own preconditions (Begin readback, exclusive flock) are the witness's already-green readback; a real removal cannot have happened because the refusal precedes the launcher spawn |
| Change 1: never weaken containment, the exact-proof requirement or the RELEASE resolution | RELEASE and Begin are green; cleanup checks unchanged | §3 keeps every check; the fix is stricter (B-created entries lose write) |
| Change 2: fix on the named side | Fixture line `scripts/ci/linux-v2-native-acceptance.sh:600-601` (`d:u:$B_UID:rwx,d:m::rwx` defaults on the synthetic clone root) | §3: one scoped ACL change, one setup-time guard, one Python test extension, one witness-side named assertion |
| Acceptance: Bind on the PR head; the `:882` stop gone or the next stop recorded verbatim | Unmeasurable locally (needs the installed owner, guest and Control) | Evidence §8 states the expected next observation; the conductor's Bind supplies it |
| Owned files | — | The script's `synthetic_goal_cleanup` provisioning and guard blocks, `scripts/ci/test-linux-v2-evidence.py`, the witness file, this plan, the evidence record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` |
| Invariants: gates bind the handed-over bytes, comments ≤ 2 lines, a red mutant per new branch, ACL-aware permission assertions | — | Evidence §5–§7: subject hashes, five mutants, the guard run red/green in the resolute chroot, ACL reads through `getfacl`/`faccessat` |

## 2. Root cause (Stage 1, summary)

The B clone bootstrap creates the checkout layout with `Mkdirat(…, 0o770)` under `umask(0o077)`
(`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:216`, `:244`, `:365`). A default ACL
present on the clone root replaces that intent: the kernel derives each new entry's group bits from
the default mask, so the `d:m::rwx` mask at
`scripts/ci/linux-v2-native-acceptance.sh:600-601` (Bind 2 shape) yields `binding-…`, `run-…` and
`generation-1` at mode `0770`, and files opened with mode `0644` land at `0660`. The authenticated
root cleanup refuses exactly that shape: `verifyCleanupPathFD(targetFD, …)`
(`daemon/native/linux-gate/protocol/clone_cleanup_linux.go:320`, `:332`, first refusing call at
`:274`) and, behind it, the RunKey walk `openCleanupRelativeDirectory`
(`:358`, `:399`), the launcher core's `verifyCleanupDirectoryFD`/`cleanupTargetDescriptor`
(`daemon/internal/launchercore/main_linux_amd64.go:243`, `:253`, `:259`) and the removal walker's
`validateEntry` (`:1548`, `:1554`, `:1558`). The anchor turns any worker error into the status
string `clone_cleanup_unproven` (`daemon/internal/platform/anchor_server.go:1205`), and the client's
response check (`daemon/internal/platform/linux_owner_anchor_linux.go:81`) reports the status-blind
line the witness prints at `:882`. No removal happened: the refusal precedes the launcher spawn,
and the only journal write is the durable Begin the witness itself performed.

The wrong layer is the **fixture**. The installed preflight's clone-root contract
(`daemon/internal/linuxpreflight/git_ownership_linux.go:381`, `:438`) requires A `r-x` without write
and B `rwx` on access and inherited entries; B is the owner, so the contract reads B's access from
`user::`/`default:user::` (owner class wins in `permissionsFromLinuxACL`,
`daemon/internal/linuxpreflight/acl_linux.go:276`). A non-writable default mask therefore satisfies
the installed contract while making the bootstrap's B-only umask effective — which is why the
fixture, not the cleanup, is the side to correct. The cleanup's mode checks are load-bearing and
mirrored at the clone root (`requireOwnedBDirectory(…, true)` rejects group write) and in the seed
layout (`verifyLinuxCloneSeedDirectory` demands exact `0750`). The witness is green on everything
before the cleanup call and the RELEASE is untouched; the anchor/platform behavior is fail-closed
and is only harder to attribute than it should be.

## 3. The change (Stage 2, fixture + pins)

- `scripts/ci/linux-v2-native-acceptance.sh:600-603`: the synthetic clone root keeps its access ACL
  (`u:$A_UID:r-x,m::r-x`) and receives `d:u:$A_UID:r-x,d:m::r-x` defaults — B's `rwx` comes from
  ownership, A keeps traversal/read, and B-created entries are no longer writable by the mask.
- `:1475-1479`: setup-time guard pinning the synthetic clone root at `$B_UID:$B_GID:750` and at the
  exact access+default ACL, so a regression to a writable mask reddens in setup with a named
  message instead of at the next Bind.
- `scripts/ci/test-linux-v2-evidence.py` (`SyntheticCleanupGuardTest`): the harness stub answers the
  clones `stat`/`getfacl`, pins the delivered `setfacl` arguments, and adds `clone-mode` and
  `clone-acl` cases (widened mask; a second named default reader is the existing exact-ACL case).
- `daemon/e2e/production_linux_v2_cleanup_witness_test.go:574`: the witness asserts, before Begin,
  that the pinned root/target chain satisfies exactly the non-writable B-owned directory shape the
  authenticated root cleanup verifies (`linuxV2CleanupShapeRefusal`/`linuxV2CleanupRequireCleanupShape`,
  `:1683`, `:1702`), with `TestLinuxV2CleanupShapeRefusalRows` (`:1736`) pinning the predicate. A
  future shape refusal stops at the witness with the offending component instead of the generic
  anchor line.
- Rejected alternatives:
  - (a) Production bootstrap change (e.g. `Fchmod(0o750)` after each `Mkdirat`): a production
    behavior change outside the owned files, and unnecessary — the installed contract is satisfied
    by the fixture's non-writable mask (evidence §5).
  - (b) Production cleanup relaxation (mask-aware acceptance of group-write bits): weakens a
    load-bearing check on the root-removal path; refused.
  - (c) Dropping the named `d:u:$B_UID:rwx` default: kept as part of the change; B's inherited
    `rwx` is read from `default:user::` because B owns the root, and a masked-out named entry would
    only invite a later "restore" of the writable mask.
  - (d) Changing the production/`clone-root`/daemon-service fixture roots: out of the contract's
    scoped files; the latent mismatch is recorded as a residual (evidence §8).
- The change is strictly stricter: no check is relaxed, no proof requirement is weakened, the
  RELEASE resolution and the State verifier are untouched.

## 4. Evidence plan

1. Red first: the recorded Bind 2 stop and the resolute-replica probe of the production checks on
   the exact Bind-2 fixture shape (target `0770`, files `0660`; target-shape and walk refusals;
   launcher target-descriptor refusal; entry-policy rows unchanged).
2. Delivered shape: the same probe set accepts root, target and walk; the installed clone-root ACL
   contract still holds; the production walker removes the whole tree, leaves the sibling and the
   independent RunKey checkout intact, and the new predicate rows pass.
3. Guard and pins: the extracted guard block red/green inside the resolute chroot; five mutants
   (guard removed, guard inverted, fixture spec reverted, witness policy inverted, witness owner
   check dropped) redden their tests.
4. Gates: script syntax, the full Python fixture test file, `go vet` and the affected `./e2e` tests
   on Windows and in WSL, and the committed-blob checks (jscpd-changed, comment-cap, record
   cleanliness) on a scratch clone of the final bytes.

## 5. Out of scope

- The next stop after the cleanup is unmeasurable on this host (the case set needs the installed
  owner, the disposable guest and Control); the conductor's Bind on the PR head records it verbatim.
- Residual production observation (needs a conductor ruling, not fixed here): an install-accepted
  clone root whose default mask is writable produces a checkout the exact cleanup refuses — today
  fail-closed, but uncleanaable without operator intervention. Candidates: tighten the install
  preflight to reject a writable default mask, make the bootstrap set the mode explicitly, or make
  the cleanup's ancestor/entry checks ACL-aware. The same fixture shape remains on the production,
  daemon-service-workspace and clone-test roots; no cleanup leg touches them today.
- The anchor's status-blind client error stays as recorded; the witness-side shape assertion is the
  attribution available without a production protocol change.
