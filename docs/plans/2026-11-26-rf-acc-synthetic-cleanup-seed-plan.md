# rf-acc-synthetic-cleanup-seed — implement plan

Contract: `goal-rf-acc-synthetic-cleanup-seed.md` (GDD state root, external to the repository).
Dispatch gate: PR #302, head `8065b96e` (`main`, this branch's base); branch
`refactor/rf-acc-synthetic-cleanup-seed`. The accepted plan is the contract; this file resolves the
choices it leaves open (which side the fix belongs to, the fixture mechanism, the committed
assertion) and points at the measured evidence. Clause-by-clause results: evidence record
`docs/evidence/rf-acc-synthetic-cleanup-seed-2026-11-26.md`.

## 1. Clause census at the dispatch head

| Contract clause | Tree state at the dispatch head | This increment |
| --- | --- | --- |
| Change 1: root cause of `guest_stage=seed errno=13` with file:line evidence | Stop recorded by Bind302 (run `45791682543` on `90157ff4`; findings `302-binding-run.md` §3.1) | §2 of this plan + evidence §2: the fixture's case root is the only B-unreadable seed ancestor; reproduced in the resolute replica |
| Change 1: compare with the green #296 clone legs | `TestInstalledLinuxOwnerCloneCheckoutScratch` (`daemon/native/linux-gate/protocol/clone_integration_linux_test.go:38`) green in every run | Evidence §2.4: same walk code, B-readable ancestors; baseline probe row |
| Change 1: production defect / AppArmor change → DECISION_NEEDED | `openReadOnlyAPathDirectory` (`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:309`) requires B read on every ancestor; the installed profile carries one `userns,` rule, `flags=(unconfined)` (`daemon/internal/linuxpreflight/preflight_linux.go:224`) | Not a production defect (evidence §2.2); no DECISION_NEEDED |
| Change 2: fix on the wrong side; never weaken RELEASE resolution, the seed receipt, containment | Fixture `scripts/ci/linux-v2-native-acceptance.sh:589` publishes the case root `root:A_GID 0751` (B traverse-only) | §3: one ACL row + three committed assertion rows; no production byte, no assertion edited |
| Acceptance: Bind once on the PR head; the `:506 … errno=13` stop must be gone | The stop is the arm's first failing line | Unmeasurable locally (case set needs the installed owner, guest and Control); the Bind supplies it; evidence §7 states the expected next observation |
| Owned files | — | The script's `synthetic_goal_cleanup` arm (setup + its own checks), this plan, the evidence record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` |
| Invariants: gates bind the handed-over bytes, comments ≤ 2 lines, red mutant per new branch, full `go test ./...` Windows + WSL, comment-cap | — | Evidence §4-§6 |

## 2. Root cause (Stage 1, summary)

The one-use RELEASE runs the fixed B clone bootstrap
(`daemon/internal/clonebootstrap/main_linux_amd64.go:124` `openCloneSeed`); it opens the seed
parent chain with `openReadOnlyAPathDirectory`
(`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:95`, `:309-345`), which opens every
component `O_RDONLY|O_DIRECTORY|O_NOFOLLOW` (`:326`) and therefore needs read (not just traverse)
on each component for B (in the gate's user namespace B is uid/gid 1 over the host B uid/gid
52001, `daemon/native/linux-gate/c-gate/gate.c:1012`,
`daemon/internal/platform/anchor_gate.go:528`). The witness fixture publishes the case root
`/opt/symmetry-owner-test/synthetic-goal-cleanup` as `root:A_GID 0751`
(`scripts/ci/linux-v2-native-acceptance.sh:589`), so B is the `other` class with `--x`: the
`openat(<case root>, O_RDONLY|O_DIRECTORY)` fails `EACCES` and the bootstrap turns it into
`fail(CloneFailureSeed, …)` (`daemon/internal/clonebootstrap/main_linux_amd64.go:126`), which the
helper reads as the `clone_checkout`/`checkout_receipt` arm's `guest_stage=seed errno=13`
(`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:1108`, `:761-790`). The green #296
legs differ only there: their seed chains sit under `0755` roots (`:588`) with the seed-root ACLs
(`:875`) and the bundle is published B-readable by production
(`daemon/internal/workspace/linux_clone_linux.go:2198` "protect B-readable Linux clone seed"), so
the same walk succeeds. AppArmor cannot produce this denial: the installed profile is one `userns,`
rule with `flags=(unconfined)` and no file rules
(`daemon/internal/linuxpreflight/preflight_linux.go:224-229`).

## 3. The change (Stage 2, fixture only)

- `scripts/ci/linux-v2-native-acceptance.sh:592`: B read on the case root
  (`setfacl -m "u:$B_UID:r-x,m::r-x"`), next to the arm's existing seed/clone ACL rows.
- `:1459-1465`: three committed assertion rows in the arm's fixture checks — B must be able to read
  the case root and the seed root, and A must not write the case root — so a future "re-tightening"
  of either path, or a widening of the case root's class, reddens the set before the witness starts.
- Rejected alternatives:
  - (b) mode `0755` on the case root: same capability for B, but it also grants read to every other
    local user; the ACL keeps `other --x` and matches the arm's existing idiom.
  - (c) relocating the seed/clone roots out of the case root: a larger fixture/config change for the
    same property, and it would contradict the witness's fixture-shape assertion
    (`production_linux_v2_cleanup_witness_test.go:209-212`).
  - (d) production change (e.g. walking ancestors with `O_PATH`, or accepting non-readable
    ancestors): refused — the requirement is load-bearing and already stated by the publisher
    (`linux_clone_linux.go:2198`), and the contract routes production defects to DECISION_NEEDED
    while the tree shows none.
- Containment is untouched: the config stays `root:A 0640` and B-unreadable, the source stays
  A-private, the seed keeps its 0440 + ACL mask, and the guest's write-denied checks on every
  ancestor are unchanged; B gains only the directory read the bootstrap itself demands.

## 4. Evidence plan

1. Red first: the recorded Bind stop verbatim (findings §3.1) and the resolute-replica probe at the
   dispatch head (evidence §2.3): the production `openCloneSeed`/`verifyCloneSeed` and
   `openAbsoluteDirectory` refuse the current fixture chain with `errno=13`, accept it after the
   ACL, and accept the production-shaped chain unchanged.
2. Gates: `bash -n`; the Linux v2 fixture-contract Python tests from `daemon/`; shellcheck
   (advisory, before/after counts); `gofmt -l`; `GOOS=linux go vet ./...`; `go vet ./...` (Windows);
   the full `go test ./...` on Windows and in WSL; comment-cap on a scratch-clone commit of the
   delivered bytes.

## 5. Out of scope

- The next stop after the seed stage is unmeasurable on this host (the case set needs the installed
  owner, the disposable guest and Control); the conductor's Bind on the PR head records it verbatim,
  as the contract's acceptance allows.
- The `:495` termination-fallout line (`dial unixpacket: connect: connection refused`) is the same
  incident's second stop and is expected to disappear with a resolved RELEASE (#302 record §2.4).
- No production byte changes and no witness assertion is edited; the `shared` case-set stops and
  the other residuals stay tracked in `docs/TODO.md`.
