# rf-acc-delegated-cgroup-kill — evidence record (2026-11-23, implement stage)

Contract: `goal-rf-acc-delegated-cgroup-kill.md` (GDD state root, external to the repository), with
its binding conductor decision (option b, retarget and relax). Plan:
`docs/plans/2026-11-23-rf-acc-delegated-cgroup-kill-plan.md`. Base revision:
`b9d4b4a4cfff1467ce9bbd24c127107c884fd077` (#266; `refactor/rf-acc-native-hosted-run` is unmerged).
Branch: `refactor/rf-acc-delegated-cgroup-kill`. Host: Windows, Go 1.27.4; the Linux rows ran on
WSL2 Ubuntu 24.04 (Go 1.27.0) over the same worktree through its mount point, and the live rows ran
on a second, disposable WSL2 Ubuntu 24.04 distro that carries a real v2 owner install (`systemd 255
(255.4-1ubuntu8.17)`, A = `syma` uid/gid `1002:1003`, B = `symb` uid/gid `1003:1004`, owner unit
`Delegate=yes`/`DelegateSubgroup=manager`, config `linux_owner_delegated_path`
`system.slice/symmetry-owner.service/manager`). The Windows and WSL suites never ran concurrently.

## 1. Subject bytes (sha256, final tree)

| File | sha256 |
| --- | --- |
| `daemon/internal/linuxpreflight/cgroup_linux.go` | `69b0e0f9ed5c76a2e2278fb50dee72b92065ef34f2989cbb3b40afcb858c9fd9` |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `d06bb55f2f8f4f1a30f0d16f12d22f0184e9aa715a78230e802cae8619f96443` |
| `docs/TODO.md` | `e00a0692942b9482624792a7fb6c2f25f1233ee76dbe88a22f2f4a8b5ace1a36` |
| `docs/archive/CLOSED-TODO.md` | `3a132eed66efdc7155042d46d440dd8a5d0b47a0b31e7e34ec6b7e02868427df` |
| `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md` | `3e62bfdbedf0c1c5eb90d73200eafab52836c69e03e6ca6fdb99983e13b1d831` |
| `docs/plans/2026-11-23-rf-acc-delegated-cgroup-kill-plan.md` | `914c306be9dabe8cdba16609149e655544ea784f45da7cf709a9b333382239a5` |
| this record | the file you are reading (no self-referential hash) |

The live rows bind the built bytes: the final tree builds reproducibly to
`ea0dccd0a2d08828e10c4c1d8220d78b2c6860cfd32ebf5d24e674f35e13be91`
(`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o … ./cmd/symmetry-daemon`), and that
exact hash was the binary executed in every live row, including a rebuild from the post-mutant
restored tree.

## 2. The retargeted boundary

`verifyLinuxOwnerParentKillBoundary` (`daemon/internal/linuxpreflight/cgroup_linux.go:130`) keeps
its signature, its mount/`RESOLVE_BENEATH` discipline and every gate the contract did not move:
the delegated directory (A-owned, no group/other write), `cgroup.procs` and
`cgroup.subtree_control` (A-owned, writable), `cgroup.events` and `cgroup.controllers`
(root- or A-owned), and the `CGROUP2_SUPER_MAGIC` `statfs` check.

- The delegated `manager/cgroup.kill` leaves the control loop and is judged by
  `validateLinuxOwnerManagerKillMetadata` alone: plain file, owner-write present, no group/other
  write, owner root (gid 0) or A (gid = A's primary group), distinct non-root A/B subjects. The
  A `O_WRONLY` refusal and the root-only metadata rule are deleted for this file.
- The owning unit cgroup's own control is opened as `dirname(delegatedPath)/cgroup.kill` from the
  pinned mount fd with the same `openat2` flags (`linuxOwnerUnitCgroupKillPath`; a
  single-component delegated path means the mount root, so the plain `cgroup.kill`).
  `validateLinuxOwnerUnitKillMetadata` requires root:root, and the retained A `O_WRONLY` probe
  requires `EACCES`/`EPERM` (an open that succeeds, or any other errno including ENOENT, refuses).
- `statLinuxOwnerCgroupControl` is the one metadata read used by both kill checks; the loop's
  inline `OpenHow` became the shared `metadataHow` value (identical flags).

## 3. Change 1 measurement: raw transcript

Host as in the header, real install, mandated unit. The probe opens
`O_WRONLY|O_CLOEXEC|O_NOFOLLOW|O_NONBLOCK` as the exact A uid/gid with no supplementary groups
(`setpriv --reuid=1002 --regid=1003 --clear-groups`) and never writes:

```
=== PHASE: after start ===
open-ok /sys/fs/cgroup/system.slice/symmetry-owner.service/manager/cgroup.kill
open-refused /sys/fs/cgroup/system.slice/symmetry-owner.service/cgroup.kill 13 EACCES
1002:1003 200 regular empty file /sys/fs/cgroup/system.slice/symmetry-owner.service/manager/cgroup.kill
0:0 200 regular empty file /sys/fs/cgroup/system.slice/symmetry-owner.service/cgroup.kill
=== PHASE: after daemon-reload ===
open-ok /sys/fs/cgroup/system.slice/symmetry-owner.service/manager/cgroup.kill
open-refused /sys/fs/cgroup/system.slice/symmetry-owner.service/cgroup.kill 13 EACCES
1002:1003 200 regular empty file /sys/fs/cgroup/system.slice/symmetry-owner.service/manager/cgroup.kill
0:0 200 regular empty file /sys/fs/cgroup/system.slice/symmetry-owner.service/cgroup.kill
=== PHASE: after restart ===
active
open-ok /sys/fs/cgroup/system.slice/symmetry-owner.service/manager/cgroup.kill
open-refused /sys/fs/cgroup/system.slice/symmetry-owner.service/cgroup.kill 13 EACCES
1002:1003 200 regular empty file /sys/fs/cgroup/system.slice/symmetry-owner.service/manager/cgroup.kill
0:0 200 regular empty file /sys/fs/cgroup/system.slice/symmetry-owner.service/cgroup.kill
=== systemctl --version ===
systemd 255 (255.4-1ubuntu8.17)
```

| Phase | `manager/cgroup.kill` | A `O_WRONLY` of the manager file | unit `cgroup.kill` | A `O_WRONLY` of the unit file |
| --- | --- | --- | --- | --- |
| after start | `1002:1003 0200` | open succeeds | `0:0 0200` | `EACCES` |
| after `daemon-reload` | `1002:1003 0200` | open succeeds | `0:0 0200` | `EACCES` |
| after restart | `1002:1003 0200` | open succeeds | `0:0 0200` | `EACCES` |

Reading: systemd 255 already hands the delegated manager `cgroup.kill` to A, at every phase, and
keeps the unit's own control root-only. This settles the 0006 record's `:613-617` anomaly (it is not
reproduced on systemd 255; the record now carries the dated amendment). The contract's decision
holds for both the v255 and v259 behaviours.

## 4. Live rows (base, head, hostile state, live mutants)

All rows: the installed host above, the owner unit started, the candidate binary run as A with
`-verify-linux-owner-install -config /etc/symmetry/daemon.json`; `run-as-a` prints the exact exit
status.

| Row | Command | Exit | Output |
| --- | --- | --- | --- |
| base `b9d4b4a4` binary | as A, unchanged host | 1 | stderr `-verify-linux-owner-install: manager control cgroup.kill is not owned/writable only by its trusted owner` |
| HEAD binary | as A, unchanged host | 0 | stdout `read-only preflight does not launch B, certify a future cgroup leaf, or attest a future per-run Git metadata ACL` |
| HEAD binary, unit kill chowned to A while active (root action, metadata now `1002:1003 0200`) | as A | 1 | stderr `-verify-linux-owner-install: unit cgroup.kill must remain root:root` |
| HEAD binary, ownership restored (`0:0 0200`) | as A | 0 | the same success line |
| M2-nometa binary (unit metadata root check removed; probe kept), unit kill chowned to A | as A | 1 | stderr `-verify-linux-owner-install: A can open the owning unit cgroup.kill for writing; the unit kill control must remain root-only` |
| M3 union mutant (unit metadata root check **and** A `O_WRONLY` refusal removed), unit kill chowned to A | as A | 0 | success line — accepts a state the contract requires to fail |
| retarget mutant (unit check reads `manager/cgroup.kill`) | as A, unchanged host | 1 | stderr `-verify-linux-owner-install: unit cgroup.kill must remain root:root` |
| HEAD binary carrying the file capability `cap_dac_override=ep` | as A, unchanged host | 1 | stderr `-verify-linux-owner-install: discover live Linux owner anchor: send Linux owner anchor identity request: write unixpacket @: sendmsg: broken pipe` |

Reading: clause 1's two directions are on the first four rows — the shipped host passes, and a host
where A can write the unit kill still fails. The M2-nometa row is the A `O_WRONLY` probe's live
proof: with the metadata root rule gone it is the probe alone that refuses the chowned file, i.e.
the probe is load-bearing on a reachable host state, and the M3 row shows the smallest edit that
accepts an A-writable unit kill (either half alone still refuses) turning that host green. The
retarget row matches the contract's Gates bullet: pointing the unit check at `manager/` fails on
this host, because this host chowns the manager file.

## 5. Red-first and mutant rows

Red first (test-level): the retargeted manager-kill rows were added to
`TestLinuxOwnerHostPrerequisiteChecksFailClosed` and run against the unchanged base helper before
any production edit; exit 1 with:

```
--- FAIL: TestLinuxOwnerHostPrerequisiteChecksFailClosed (0.00s)
    preflight_linux_test.go:284: A-owned parent cgroup.kill metadata was rejected: delegated manager cgroup.kill must remain root-owned and unavailable to A/B write access
FAIL	github.com/wxxb789/symmetry/daemon/internal/linuxpreflight	0.017s
```

The unit-kill rows cannot exist at base (no such check), so their replacement proof is the live
hostile-state pair in §4 plus the M2/M3 rows below.

Each mutant was applied alone to the final tree, run, and reverted; the two Go files hash back to
§1 (`sha256sum` verified after every revert) and the restored tree rebuilds to the §1 binary hash.

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| M1 accept a B-owned manager kill | the manager helper's owner-set and owner-gid rules removed | `go test -count=1 -run TestLinuxOwnerKillBoundaryMetadata ./internal/linuxpreflight/...` | `preflight_linux_test.go:308: manager cgroup.kill B-owned: err = <nil>, accepted = true, want accepted = false` (+ the two wrong-group rows) |
| M2 drop the unit-kill root check | `validateLinuxOwnerUnitKillMetadata` loses `uid != 0 || gid != 0` | the same command | `preflight_linux_test.go:327: unit cgroup.kill A-owned: err = <nil>, accepted = true, want accepted = false` (+ B-owned, foreign-group rows) |
| M3 accept an A-writable unit kill | the unit metadata root check **and** the A `O_WRONLY` refusal removed (the pair; either alone still refuses) | the live hostile-state row (§4) | mutant exit 0 where the fixed code exits 1 (`unit cgroup.kill must remain root:root`) |
| M4 accept a group-writable manager kill | the shared mode rule loses `mode&0022 != 0` | the unit command | `preflight_linux_test.go:308: manager cgroup.kill group-writable: err = <nil>, accepted = true, want accepted = false` (+ other-writable for both files) |
| extra: retarget the unit check | `linuxOwnerUnitCgroupKillPath(delegatedPath)` → `delegatedPath` | the live unchanged-host row (§4) | mutant exit 1 where the fixed code exits 0 (`unit cgroup.kill must remain root:root`) |

## 6. Test rows (`TestLinuxOwnerKillBoundaryMetadata`)

Accepted: root-owned `0:0 0200`; A-owned `1001:1002 0200` (manager) — the state systemd ≥254
produces. Rejected: B-owned `1003:1004 0200`; A-owned with the root group; root-owned with A's
group; group-writable `0220`; other-writable `0202`; owner-unwritable `0400`; a non-plain-file
`S_IFDIR|0700`; and, for the unit control, every non-root:root owner including a foreign group.
`TestLinuxOwnerUnitCgroupKillPathFollowsTheDelegatedParent` pins the derivation (`manager` →
`cgroup.kill`; `a/b/c/manager` → `a/b/c/cgroup.kill`), the off-by-one-level hazard the retarget
mutant exploits.

## 7. Gates

Working directory `daemon/` unless a row says otherwise; WSL rows run Ubuntu-24.04 over the same
worktree.

| Gate (working directory) | Exit | Output |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`, Windows) | 0 | no file listed |
| `go vet ./internal/linuxpreflight/...` (`daemon/`, Windows) | 0 | empty |
| `GOOS=linux go build ./...` (`daemon/`, Windows) | 0 | empty |
| `go vet ./internal/linuxpreflight/...` (`daemon/`, WSL) | 0 | empty |
| `go vet ./...` (`daemon/`, WSL) | 0 | empty log, 0 lines |
| `go test -race -count=1 ./internal/linuxpreflight/...` (`daemon/`, WSL) | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/linuxpreflight 1.174s` |
| `git diff --check` (git root) | 0 | no whitespace error |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on each changed doc (git root) | 0 for this record, the plan and the 0006 evidence doc; 1 pre-existing for `docs/TODO.md` and `docs/archive/CLOSED-TODO.md` (§10) | no host absolute path in this slice's added lines |
| `python scripts/ci/check-citation-existence.py --base b9d4b4a4 --head <scratch tree of the final bytes>` (git root) | 0 | `1 changed document(s) pass the path and symbol check (534 path citation(s))`; three pre-existing ignore-rule notices |
| `python scripts/ci/check-record-cleanliness.py --base b9d4b4a4 --head <scratch tree of the final bytes>` (git root) | 0 | `2 changed record(s) pass the root-only check` |
| live rows (§4) | 0 for the fixed binary on the unchanged and restored hosts; see §4 for the mutant rows | — |

## 8. Scope

```
$ git diff --name-status
M	daemon/internal/linuxpreflight/cgroup_linux.go
M	daemon/internal/linuxpreflight/preflight_linux_test.go
M	docs/TODO.md
M	docs/archive/CLOSED-TODO.md
M	docs/evidence/goal-0006-linux-owner-design-2026-09-22.md

$ git diff --numstat
72	13	daemon/internal/linuxpreflight/cgroup_linux.go
57	12	daemon/internal/linuxpreflight/preflight_linux_test.go
4	0	docs/TODO.md
45	0	docs/archive/CLOSED-TODO.md
37	0	docs/evidence/goal-0006-linux-owner-design-2026-09-22.md

$ git status --porcelain -uall --no-renames
 M daemon/internal/linuxpreflight/cgroup_linux.go
 M daemon/internal/linuxpreflight/preflight_linux_test.go
 M docs/TODO.md
 M docs/archive/CLOSED-TODO.md
 M docs/evidence/goal-0006-linux-owner-design-2026-09-22.md
?? docs/evidence/rf-acc-delegated-cgroup-kill-2026-11-23.md
?? docs/plans/2026-11-23-rf-acc-delegated-cgroup-kill-plan.md
```

Every path is in the contract's owned set; no `platform/**`, anchor, unit-template, installer or
acceptance-script byte changed, and the one new test file the contract allows was not needed.
`docs/design/*` states the old rule nowhere (`grep -rn "kill control\|parent kill\|root-only"
docs/design/` finds nothing), so no design page needed an edit — checked-absent, recorded here.

## 9. As-built decisions and limits

- The contract's TODO clause was executed against the merge reality: the entry lands on the
  unmerged branch `refactor/rf-acc-native-hosted-run` (its diff against `b9d4b4a4` adds it at
  `docs/TODO.md:311-335`), so `docs/TODO.md` gains one recorded-status sentence retiring it and
  `docs/archive/CLOSED-TODO.md` gains the closure section, which states the merge-order fact.
- The manager kill keeps the base loop's owner-write requirement (`mustBeWritable`), so the rule
  reads "writable only by its trusted owner"; the contract's acceptance set is a subset of it.
- The group rule for the accepted owners (`gid` 0 for root, A's primary gid for A) is the contract's
  "(gid matching that owner)". The mandated unit pins `User`/`Group` to A's account and A's primary
  group name (`systemd_linux.go` unit policy), and the measurement shows systemd chowns uid and gid
  together, so both accepted shapes are what systemd produces.
- ENOENT on the unit kill refuses by construction (the metadata read errors first); the probe never
  treats a missing file as root-only.
- The `cap_dac_override` row is a discarded attempt at demonstrating the A probe's independent
  discriminating power: an A client carrying a file capability is refused during anchor discovery,
  deterministically (2/2 capable failures against 2/2 plain successes alternating inside one anchor
  lifetime), before any cgroup check. `linuxOwnerAnchorPeerFromConn` reads `SO_PEERCRED` and
  process identity only, so the refusing check was **not identified**; the effect is fail-closed and
  the row is not used as evidence. The probe's live proof is the M2-nometa row instead.

## 10. Doc gates

`python scripts/ci/model-routing-record-sanitize.py --check --root-only` ran once per changed doc
with these results: exit 0 for this record, the plan and
`docs/evidence/goal-0006-linux-owner-design-2026-09-22.md`; exit 1 for `docs/TODO.md` and
`docs/archive/CLOSED-TODO.md`, **pre-existing and unchanged** — both files already exit 1 at
`b9d4b4a4` (historic entries quoting host-path specimens; first refusal lines `docs/TODO.md:1527`,
`docs/archive/CLOSED-TODO.md:373`), and the same tool over this slice's added lines only
(`git diff -U0`, `^+` filter, fed via stdin) exits 0 for both. `git diff --check` exit 0. The raw
transcripts of §3/§4 were captured through scratch scripts outside the repository and are quoted
here; no host path from them appears in any committed doc.

`check-citation-existence.py` and `check-record-cleanliness.py` read the `--head` revision's blobs,
so they audit a committed revision, not the worktree; the branch stays uncommitted for
verification. To run both on the final bytes without touching the branch, a scratch tree of those
bytes was staged under a temporary `GIT_INDEX_FILE` outside the repository and written as a
dangling `commit-tree` object (no ref, no index entry, `HEAD` and the worktree unchanged —
`git rev-parse HEAD` still reads `b9d4b4a4cfff1467ce9bbd24c127107c884fd077`); both gates exit 0
over it (§7), and the conductor's post-commit run rebinds the same audits to the committed
revision.

## 11. Residual risks and known limits

- The A/B separation of the manager kill rests on metadata (owner, group, no group/other write,
  owner-write) rather than a live B probe: the preflight is A and a read-only check cannot
  impersonate B. The group-write bit is also the POSIX ACL mask class, so an ACL entry granting B
  write cannot survive it.
- The A `O_WRONLY` probe's independent power (a root-owned file that A could still write through an
  effective capability or an LSM policy) is untested through the production path because the anchor
  refuses a capable client (§9); the probe stays as the contract's mandated reading of "refused for
  an A `O_WRONLY` open".
- A can still bulk-kill its own delegated subtree, including cross-UID run processes — the
  accepted, inherent consequence of `DelegateSubgroup=` delegation; run stops stay governed by the
  run-scoped exactly-once kill/ACK protocol.
- The package suite is the contract's WSL test gate; the full daemon suite was not part of the gate
  set and was not run (the change is one build-constrained package, and the live rows exercise the
  binary end to end).
- If `refactor/rf-acc-native-hosted-run` lands after this branch, its `docs/TODO.md` paragraph
  supersedes this branch's retirement sentence textually; the archive closure entry and this record
  carry the resolution.
