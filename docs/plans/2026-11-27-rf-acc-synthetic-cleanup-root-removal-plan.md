# rf-acc-synthetic-cleanup-root-removal — implement plan

Contract: `goal-rf-acc-synthetic-cleanup-root-removal.md` (GDD state root, outside the repository).
Dispatch base: `d4e455f1` (main). Branch: this goal worktree. The accepted plan is the contract;
this file resolves the choices it leaves open (which layer the stop belongs to, the fixture
mechanism, the committed pins) and points at the measured evidence. Clause-by-clause results:
evidence record `docs/evidence/rf-acc-synthetic-cleanup-root-removal-2026-11-27.md`.

## 1. Clause census at the dispatch head

| Contract clause | Tree state at the dispatch head | This increment |
| --- | --- | --- |
| Change 1: root cause of the anchor's missing exact root removal proof, with file:line evidence and the named layer | Historical #304 Bind 2 (`45938637085`, run head `2119226c`, subject `f3dbae66`) records `production_linux_v2_cleanup_witness_test.go:882: … Linux owner anchor returned no exact root clone-removal proof`; the owner-service journal holds no worker detail | §2 of this plan and evidence §2–§3: the fixture's default ACL makes the exact B target group-writable, which the first worker-side target check refuses before launcher spawn |
| Change 1: discriminate real removal / swallowed refusal / wrong subject / permission failure / proof-shape mismatch | The client drops the response status (`linux_owner_anchor_linux.go:81`) | Evidence §3: the Resolute replica proves the old ACL shape independently blocks removal at the worker boundary. The historical log does not distinguish an earlier State-authority or request refusal from the reproduced execution refusal |
| Change 1: never weaken containment, the exact-proof requirement or the RELEASE resolution | RELEASE and Begin are green; cleanup checks unchanged | §3 keeps every check; the fix is stricter (B-created entries lose write) |
| Change 2: fix on the named side | Fixture line `scripts/ci/linux-v2-native-acceptance.sh:600-601` (`d:u:$B_UID:rwx,d:m::rwx` defaults on the synthetic clone root) | §3: one scoped ACL change, one setup-time guard, one Python test extension, one witness-side named assertion |
| Acceptance: Bind on the PR head; the `:882` stop gone or the next stop recorded verbatim | Unmeasurable locally (needs the installed owner, guest and Control) | Evidence §9 states the pending exact-head requirement; `## Bind` records the green result on the prior head only |
| Owned files | — | The script's `synthetic_goal_cleanup` provisioning and guard blocks, `scripts/ci/test-linux-v2-evidence.py` (owned under the conductor's 2026-11-27 scope amendment, review STD-A02: its guard-test harness for the owned script arm — the delivered script against the base harness reddens `SyntheticCleanupGuardTest`, so the edit is coupled), the witness file, this plan, the evidence record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` |
| Invariants: gates bind the handed-over bytes, comments ≤ 2 lines, a red mutant per new branch, ACL-aware permission assertions | — | Evidence §1 and §5–§10: subject hashes, Resolute guard results, named mutant rows and the scoped local gates |

## 2. Root cause (Stage 1, summary)

The B clone bootstrap creates the checkout layout with `Mkdirat(…, 0o770)` under `umask(0o077)`
(`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:216`, `:244`, `:365`). A default ACL
present on the clone root replaces that intent: the kernel derives each new entry's group bits from
the default mask, so the `d:m::rwx` mask at
`scripts/ci/linux-v2-native-acceptance.sh:600-601` (Bind 2 shape) yields `binding-…`, `run-…` and
`generation-1` at mode `0770`, and files created with mode `0666` (Git, the setup shell) land at
`0660`. The authenticated root cleanup refuses exactly that shape:
`verifyCleanupPathFD(targetFD, …)`
(`daemon/native/linux-gate/protocol/clone_cleanup_linux.go:320`, `:332`, first refusing call at
`:274`) and, behind it, the RunKey walk `openCleanupRelativeDirectory`
(`:358`, `:399`), the launcher core's `verifyCleanupDirectoryFD`/`cleanupTargetDescriptor`
(`daemon/internal/launchercore/main_linux_amd64.go:243`, `:253`, `:259`) and the removal walker's
`validateEntry` (`:1548`, `:1554`, `:1558`). The cleanup execution refusal maps to
`clone_cleanup_unproven`; the earlier State-authority failure maps to `cleanup_authority_unproven`
(`daemon/internal/platform/anchor_server.go:1089–1094`, `:1202–1209`). The client reports the same
no-exact-proof message for both (`daemon/internal/platform/linux_owner_anchor_linux.go:75–81`), so
the historical `:882` line cannot identify where the Anchor refused. The reproduced shape refusal
precedes launcher spawn; the historical RunJournal had no removal proof.

The wrong layer is the **fixture**. The installed preflight's clone-root contract
(`daemon/internal/linuxpreflight/git_ownership_linux.go:381`, `:438`) requires A `r-x` without write
and B `rwx` on access and inherited entries; B is the owner, so the contract reads B's access from
`user::`/`default:user::` (owner class wins in `permissionsFromLinuxACL`,
`daemon/internal/linuxpreflight/acl_linux.go:276`). The inherited ACL still governs creation, bounded
by the requested mode. In Resolute, `mkdir(0770)` under `umask 077` gave `0770` with `rwx`, `0750`
with `r-x`, and `0700` only with no default ACL (evidence §5.1). Masking out group write still
satisfies the installed contract, so the fixture is the side to correct. The mode checks are
mirrored at the clone root (`requireOwnedBDirectory(…, true)` rejects group write) and in the seed
layout (`verifyLinuxCloneSeedDirectory` demands exact `0750`). The witness is green on everything
before the cleanup call and the RELEASE is untouched; the anchor/platform behavior is fail-closed
and is only harder to attribute than it should be.

## 3. The change (Stage 2, fixture + pins)

- `scripts/ci/linux-v2-native-acceptance.sh:600-603`: the synthetic clone root keeps its access ACL
  (`u:$A_UID:r-x,m::r-x`) and receives `d:u:$A_UID:r-x,d:m::r-x` defaults — B's `rwx` comes from
  ownership, A keeps traversal/read, and B-created entries are no longer writable by the mask.
- `:1474-1478`: setup-time guard pinning `$B_UID:$B_GID:750` and the exact access+default ACL.
  The measured refused inputs and diagnostics are in evidence §6.
- `scripts/ci/test-linux-v2-evidence.py` (`SyntheticCleanupGuardTest`): the harness stub answers the
  clone root's `stat`/`getfacl` and pins the provisioned `setfacl` arguments. `clone-acl` widens only
  the default mask; `clone-default-reader` adds a named default reader without changing mask or
  mode. `clone-mode`, `clone-uid` and `clone-gid` isolate the three stat columns.
- `daemon/e2e/production_linux_v2_cleanup_witness_test.go:574`: the witness asserts, before Begin,
  that the pinned root/target chain satisfies exactly the non-writable B-owned directory shape the
  authenticated root cleanup verifies (`linuxV2CleanupShapeRefusal`/`linuxV2CleanupRequireCleanupShape`,
  `:1683`, `:1702`), with `TestLinuxV2CleanupShapeRefusalRows` (`:1736`) pinning the predicate and
  `TestLinuxV2CleanupShapeWalkRows` (`:1981`) pinning root, intermediate ancestor, exact target and
  open-error decisions. The walk tests require the borrowed root FD to survive and acquired fixture
  FDs not to survive. The witness reports the offending component before calling the Anchor.
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
    scoped files; the latent mismatch is recorded as a residual (evidence §11).
- The change is strictly stricter: no check is relaxed, no proof requirement is weakened, the
  RELEASE resolution and the State verifier are untouched.

## 4. Evidence plan

1. Red first: the recorded Bind 2 stop and the resolute-replica probe of the production checks on
   the exact Bind-2 fixture shape (target `0770`, files `0660`; target-shape and walk refusals;
   launcher target-descriptor refusal; entry-policy rows unchanged).
2. Delivered shape: the same probe set accepts root, target and walk; the installed clone-root ACL
   contract still holds; the production walker removes the whole tree, leaves the sibling and the
   independent RunKey checkout intact, and the new predicate rows pass.
3. Guard and pins: run the extracted guard on real ACLs in the Resolute chroot and run each named
   mutant on the matching committed row (evidence §6–§7).
4. Gates: script syntax, the full Python fixture test file, `go vet` and the affected `./e2e` tests
   on Windows and in WSL, and the committed-blob checks (jscpd-changed, comment-cap, record
   cleanliness) on a scratch clone of the final bytes.

## 5. Out of scope

- The updated execution inputs need a fresh credentialed Bind on the fixed PR head. Local probes
  do not replace the installed owner, disposable guest or Control.
- Residual production observation (needs a conductor ruling, not fixed here): an install-accepted
  clone root whose default mask is writable produces a checkout the exact cleanup refuses — today
  fail-closed, but not removable by the exact cleanup path without intervention. Candidates: reject
  a writable default mask at preflight, have the bootstrap set the mode explicitly, or make
  the cleanup's ancestor/entry checks ACL-aware. The writable mask remains on the production,
  daemon-service-workspace and clone-test roots. The shared legs use clone-test targets provisioned
  before that mask is installed; see evidence §11 for their callers and ordering.
- The anchor's status-blind client error stays as recorded; the witness-side shape assertion is the
  attribution available without a production protocol change.
