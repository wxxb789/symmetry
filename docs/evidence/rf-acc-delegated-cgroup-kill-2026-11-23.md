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
exact hash was the binary executed in every live row of the implement tree, the capable-binary row
included, the rebuild from the post-mutant restored tree reproducing it; the base `b9d4b4a4` row
ran the base revision's own build and each mutant row ran its mutant build (`4ca24d41…` and
`fd8026a7…` for the M3 and retarget mutants, §12); §13 re-runs the live rows on the fix-stage
binary.

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

## 12. Apply-stage amendment (2026-11-23): accepted simplify findings

Applied exactly the accepted findings of the apply-stage brief: simplify findings 1-5, D1 as its
non-weakening variant, D2. Findings 1-4 are behaviour-preserving edits inside
`daemon/internal/linuxpreflight/cgroup_linux.go`; finding 5 adds the unit table's
`owner-unwritable` row; D1 keeps the unit kill's owner-write requirement (the shared mode rule) and
discloses it here and in the CLOSED-TODO closure - the contract's stated unit-file set is a subset
of the enforced set, the same relationship §9 records for the manager file; D2 appends the sentence
naming the systemd-nspawn guest instance of the retired rule in the 0006 record. The plan file is
unchanged: no accepted finding named it, and its §4 evidence plan points at this record.
**§1's subject hashes and binary hash, §5's first-red line numbers and §8's numstat bind the
implement-stage bytes; the apply-stage bytes are bound here.** §6's row list gains the unit
`owner-unwritable` row. §4's live rows and every acceptance decision stay valid: no gate was
relaxed, no test row removed, and the loop's refusal semantics are unchanged (any open, stat or
close error still refuses; only the helper's wording changed).

| Finding | Change |
| --- | --- |
| 1 (delete) | `unitKillPath` no longer wraps `linuxOwnerUnitCgroupKillPath` in `filepath.ToSlash`; the helper owns the conversion, which is identity in this linux/amd64 file. |
| 2 (refactor) | The control loop calls `statLinuxOwnerCgroupControl`; only its owner/mode conditions stay inline, so one open/stat/close path serves the four controls and both kill metadata reads. |
| 3 (refactor) | The helper's error text drops "delegated" (`open cgroup control %s metadata read-only`), which misnamed the unit cgroup file and the manager controls. |
| 4 (refactor) | `linuxOwnerKillSubjectsDistinctAndNonRoot` is the one distinct-non-root A/B predicate; the manager keeps its `ownerUID` coupling checks and the unit keeps its root:root check. |
| 5 (test) | Unit table gains the `owner-unwritable` row (mode `0400`), rejected - parity with the code, and the row `docs/archive/CLOSED-TODO.md` claims for both checks. |
| D1 | Unit kill keeps the strict mode rule; disclosed here and in the closure text. Contract-literal relaxation not applied: it would remove a validation requirement the contract does not require removing, and no measured host produces `0400` (the unit kill is kernel/systemd-made `0200`). |
| D2 | 0006 amendment gains the sentence naming the `:710-714` instance and its unmeasured guest environment; the historic text is untouched. |

Subject bytes after apply (sha256):

| File | sha256 |
| --- | --- |
| `daemon/internal/linuxpreflight/cgroup_linux.go` | `cb82e1e1d61c63e70542397a28629e3c321bcd841899c6090d8eb20628b90f05` |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `552a9576b6a855fb73664f257b2df8aad1fd7e8ddc103053b9a428689065e42f` |
| `docs/TODO.md` (unchanged; matches §1) | `e00a0692942b9482624792a7fb6c2f25f1233ee76dbe88a22f2f4a8b5ace1a36` |
| `docs/archive/CLOSED-TODO.md` | `bcd27881439f891a96184e62b2b31370db5bb50f23f03134d5c6c4927bf6529d` |
| `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md` | `4fa086ca51cf9401d299a1d17030fcc3df95f4f65337c3b5e977078e76c7fbe6` |
| `docs/plans/2026-11-23-rf-acc-delegated-cgroup-kill-plan.md` (unchanged; matches §1) | `914c306be9dabe8cdba16609149e655544ea784f45da7cf709a9b333382239a5` |

The apply bytes build reproducibly to
`8d49dc0b1798dc2bdd350049d5a357bd7ddc052849cc61e078ebeff7b11c8275`
(`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o … ./cmd/symmetry-daemon` from
`daemon/`), and that exact binary ran every live apply row below.

| Gate (working directory) | Exit | Output |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`, Windows) | 0 | no file listed |
| `go vet ./internal/linuxpreflight/...` (`daemon/`, Windows) | 0 | empty |
| `GOOS=linux go build ./...` (`daemon/`, Windows) | 0 | empty |
| `go vet ./internal/linuxpreflight/...` (`daemon/`, WSL) | 0 | empty |
| `go test -race -count=1 ./internal/linuxpreflight/...` (`daemon/`, WSL) | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/linuxpreflight 1.188s` |
| `git diff --check` (git root) | 0 | no whitespace error |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` (git root; per changed doc) | 0 for `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md`; 1 pre-existing for `docs/archive/CLOSED-TODO.md` and `docs/TODO.md`, unchanged from their `b9d4b4a4` blobs (first rendered host-path token: CLOSED-TODO line 247 at both revisions; TODO line 730 at base, 734 now) | this record: see the closing line of this section |

Mutants, each applied alone to the apply bytes, run, and reverted; both Go files hash back to this
section after every revert (`sha256sum` verified):

| Mutant | Killer (exit) | First red line |
| --- | --- | --- |
| M1 accept a B-owned manager kill | unit test, exit 1 | `preflight_linux_test.go:308` manager B-owned (+ the two wrong-group rows) |
| M2 drop the unit-kill root check | unit test, exit 1 | `preflight_linux_test.go:328` unit A-owned (+ B-owned, foreign-group) - the finding 5 row moved this line from §5's 327 |
| M4 accept a group-writable manager kill | unit test, exit 1 | `preflight_linux_test.go:308` manager group/other-writable, then the same unit rows at `:328` |
| M3 union mutant (unit root check and A `O_WRONLY` refusal removed) | live hostile-state row, mutant exit 0 where the fixed binary exits 1 | mutant binary `4ca24d41fdc750037d8d54e802069a6ce8ebb296b6697dd3f251ca9f315f96d0` accepts the chowned host |
| extra: retarget the unit check | live unchanged-host row, mutant exit 1 where the fixed binary exits 0 | mutant binary `fd8026a7e8833f57efbaefc165f08a14557e8741d6cdb5828850bf5a27013ebe` refuses with `unit cgroup.kill must remain root:root` |

Live apply rows (install host from the header; the owner unit started; the apply binary run as A
with `-verify-linux-owner-install -config /etc/symmetry/daemon.json`):

| Row | Exit | Output |
| --- | --- | --- |
| unchanged host | 0 | the §4 success line |
| unit kill chowned to A (`1002:1003 0200`, root action) | 1 | `unit cgroup.kill must remain root:root` |
| ownership restored (`0:0 0200`) | 0 | the §4 success line |
| M3 union mutant on the same chowned state | 0 | the §4 success line - accepts the state the contract requires to fail |
| retarget mutant on the unchanged host | 1 | `unit cgroup.kill must remain root:root` |

The Change 1 measurement was re-run read-only (probe script from the same scratch set; open only,
never write) because the live rows consume the host state it describes: all three phases reproduce
§3 exactly - `manager/cgroup.kill` `1002:1003 0200` with the A open succeeding, unit `cgroup.kill`
`0:0 0200` with `EACCES` for A, `systemctl --version` `systemd 255 (255.4-1ubuntu8.17)`. The probe
never ran concurrently with the suite. The host was left as found: owner unit `inactive`, both kill
files `0:0 0200` / `1002:1003 0200` restored.

Numstat: apply delta (`git diff --numstat`, worktree against `e19999b7`) is `14 13`
`daemon/internal/linuxpreflight/cgroup_linux.go`, `1 0`
`daemon/internal/linuxpreflight/preflight_linux_test.go`, `4 4` `docs/archive/CLOSED-TODO.md`,
`6 0` `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md`. Cumulative against the recorded
range's base `2c8c419e`: `80 20` and `58 12` for the two Go files, `4 0` `docs/TODO.md`, `45 0`
`docs/archive/CLOSED-TODO.md`, `43 0` `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md`.
§8's `72 13`/`57 12` counted the implement-stage uncommitted diff at `b9d4b4a4`; per-file numstat
is not additive across revisions, so the cumulative row is the current count.

Doc gates for this record, observed on this appendix tree (scratch commit of these bytes,
`7474eb06fa81ad85efff6881dc008aa5ba2ca289`, dangling, no ref; `HEAD` and the worktree unchanged):
`model-routing-record-sanitize.py --check --root-only` exit 0; `check-citation-existence.py --base
e19999b7 --head 7474eb06` exit 0 (`1 changed document(s) pass the path and symbol check (534 path
citation(s))`, with the same three pre-existing ignore-rule notes as §7);
`check-record-cleanliness.py --base e19999b7 --head 7474eb06` exit 0 (`2 changed record(s) pass the
root-only check`). This closing paragraph was appended after that audit; the conductor's
post-commit run rebinds all three gates to the committed revision.

## 13. Fix-stage amendment (2026-11-23): review findings STD-A01..A05, SPEC-A01, SPEC-A02

Applied exactly the fix brief's findings; nothing else changed. No test row was removed and no gate
relaxed. §1's subject and binary hashes and §12's amend hashes bind the implement/apply bytes; the
fix-stage bytes are bound here.

Subject bytes after the fix (sha256):

| File | sha256 |
| --- | --- |
| `daemon/internal/linuxpreflight/cgroup_linux.go` | `a474b6206f79d129ab5b7c20880c7fca63abbfd5d82c502ae77ba4a224c6c85a` |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `3a885793af323c6bc4be72786201f40575ca7607a1890af9af0711141e8544cb` |
| `docs/archive/CLOSED-TODO.md` | `7476b1f3038ad3b452f26e7872d20632cd6e5e9dcd39cba9fd0e83d4561a7da8` |
| `docs/plans/2026-11-23-rf-acc-delegated-cgroup-kill-plan.md` | `4eaacfd171789391750b34028c54a553eb1989729620141090f8b4ab94c63add` |

`docs/TODO.md` and `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md` are unchanged from
§12 (`e00a0692…`, `4fa086ca…`); this record carries no self-referential hash.

Structural change (STD-A03, SPEC-A01): the unit-kill metadata read, its root:root judgement and the
A `O_WRONLY` probe move out of `verifyLinuxOwnerParentKillBoundary`'s inline tail into
`verifyLinuxOwnerUnitKillControl(rootFD, delegatedPath, metadataHow, a, b)`, which derives
`linuxOwnerUnitCgroupKillPath(delegatedPath)` and performs both the metadata read and the probe
under the caller's `mountFD`; the probe outcome is the pure
`validateLinuxOwnerUnitKillWriteProbe(writeErr)` - nil open and every errno but `EACCES`/`EPERM`
refuse, `EACCES`/`EPERM` accept bare or wrapped. Every error string and the decision sequence are
unchanged; the live rows below re-run on the fix-stage binary and reproduce §4.

New committed rows: `TestLinuxOwnerUnitKillWriteProbeDecisions` table-tests nil, bare and wrapped
`EACCES`, `EPERM`, `ENOENT` and `EINVAL`; `TestLinuxOwnerUnitKillControlReadsTheDerivedUnitPath`
drives the control helper against a real directory tree under one directory fd - an absent
`unit/cgroup.kill` refuses with `ENOENT` at the derived path (not the delegated `unit/manager`), and
a present root:root `0200` file is refused through the live `O_WRONLY` probe (the gate environment
runs the suite as root, so the probe really opens; a non-root run asserts the root:root metadata
refusal instead). Package count: 27 `func Test` / 25 top-level PASS before, 29 / 27 after (2 SKIP
at the fix bytes).

Mutants, each applied alone to the fix bytes, run, and reverted (`git cat-file blob` restore,
sha256 checked; commands from `daemon/`):

| Mutant | Edit | Killer (exit 1) | First red line |
| --- | --- | --- | --- |
| F1 probe accepts an opened fd | probe `writeErr == nil` branch returns nil | `go test -count=1 -v -run 'TestLinuxOwnerUnitKillWriteProbeDecisions\|TestLinuxOwnerUnitKillControlReadsTheDerivedUnitPath' ./internal/linuxpreflight/...` | `preflight_linux_test.go:363: unit kill write probe opened for writing: err = <nil>, accepted = true, want accepted = false`; `:395: unit cgroup.kill as root: err = <nil>, want the write-probe refusal` |
| F2 any errno accepted | probe default branch returns nil | the same command | `preflight_linux_test.go:363: unit kill write probe missing file: err = <nil>, accepted = true, want accepted = false` (+ wrapped missing file, invalid flags, wrapped invalid flags; the wiring rows stay green because the metadata read refuses first) |
| F3 `EACCES`/`EPERM` refused | probe accept branch deleted | `go test -count=1 -v -run TestLinuxOwnerUnitKillWriteProbeDecisions ./internal/linuxpreflight/...` | `preflight_linux_test.go:363: unit kill write probe refused with EACCES: err = cannot establish root-only unit cgroup.kill permissions: permission denied, accepted = false, want accepted = true` (+ EPERM, wrapped EACCES, wrapped EPERM) |
| F4 unit check retargeted at the delegated path | `linuxOwnerUnitCgroupKillPath(delegatedPath)` to `delegatedPath` | `go test -count=1 -v -run TestLinuxOwnerUnitKillControlReadsTheDerivedUnitPath ./internal/linuxpreflight/...` | `preflight_linux_test.go:383: absent unit cgroup.kill: err = cgroup.kill must be a plain file writable only by its owner, want an ENOENT refusal at the derived path` |
| M3 union, re-recorded | unit metadata root check and the A `O_WRONLY` refusal removed | the new probe/control rows and the live chowned-host row below | `preflight_linux_test.go:329: unit cgroup.kill A-owned: err = <nil>, accepted = true, want accepted = false`; `:363: unit kill write probe opened for writing: err = <nil>, accepted = true, want accepted = false`; `:395: unit cgroup.kill as root: err = <nil>, want the write-probe refusal`; live: mutant exit 0 on the chowned host where the fixed binary exits 1 |

F4 replaces the previous live-only kill of the retarget mutation, and M3's killer column gains the
hermetic probe rows alongside the live row (STD-A03, SPEC-A01).

Live rows (fix-stage binary, install host from the header, owner unit started, run as A with
`-verify-linux-owner-install -config /etc/symmetry/daemon.json`):

| Row | Exit | Output |
| --- | --- | --- |
| fixed binary, unchanged host | 0 | the §4 success line |
| retarget mutant binary `02d01eeefd1456a6455cfb283f9edfd842f1da4aef48f4cde312396c7098dd88`, unchanged host | 1 | `unit cgroup.kill must remain root:root` |
| fixed binary, unit kill chowned to A (`1002:1003 0200`) | 1 | `unit cgroup.kill must remain root:root` |
| M3 union mutant binary `e75517e28393636580427fee7bcd005df66d0c1a57abeefa78f075923464d8f4`, chowned host | 0 | the §4 success line - accepts the state the contract requires to fail |
| fixed binary, ownership restored (`0:0 0200`) | 0 | the §4 success line |

The fix-stage binary is
`52b441e045d223aa32f354ce4a3a837802f7335d9b546e1c88599255ae41d511`
(`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o … ./cmd/symmetry-daemon`; a rebuild
reproduced it byte for byte); the host was left as found (unit stopped, no leftover cgroup).

Doc corrections: STD-A01 - the closure's residual no longer attributes the capable-client refusal
to "the anchor's own client gate (all-zero capability sets)"; it states the record's finding
(refused during anchor discovery, `sendmsg: broken pipe`, refusing check not identified) and the new
hermetic probe coverage. STD-A02 - the closure's acceptance-set sentence now states acceptance per
check (root-owned for both, A-owned manager-only, unit stays root:root) per §6 and the unit table.
STD-A04 - the plan's §1 base-coordinate cell now cites `:150-152` and `:153-182` (the `:161` entry),
verified against `b9d4b4a4`. STD-A05 - §1's binary sentence scopes `ea0dccd0…` to the implement-tree
rows. SPEC-A02 - all three corrections applied in one pass.

Fix-stage gates (all from `daemon/` unless stated; Windows and WSL):

| Gate | Exit | Output |
| --- | --- | --- |
| `gofmt -l .` (Windows) | 0 | no file listed |
| `go vet ./internal/linuxpreflight/...` (Windows) | 0 | empty |
| `GOOS=linux GOARCH=amd64 go build ./...` (Windows) | 0 | empty |
| `go vet ./internal/linuxpreflight/...` (WSL) | 0 | empty |
| `go test -count=1 ./internal/linuxpreflight/...` (WSL) | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/linuxpreflight 0.085s` |
| `go test -race -count=1 ./internal/linuxpreflight/...` (WSL) | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/linuxpreflight 1.117s` |
| `git diff --check` (git root) | 0 | no whitespace error |
| `python scripts/ci/check-citation-existence.py --repo . --base 2c8c419e --head <scratch>` | 0 | `1 changed document(s) pass the path and symbol check (534 path citation(s))`; the 3 pre-existing ignore-rule notes |
| `python scripts/ci/check-record-cleanliness.py --repo . --base 2c8c419e --head <scratch>` | 0 | `2 changed record(s) pass the root-only check` |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` per changed doc | 0 for this record, the plan and the 0006 evidence doc; 1 pre-existing for `docs/TODO.md` and `docs/archive/CLOSED-TODO.md`; the added lines of every changed doc exit 0 through the same tool (stdin) | no host absolute path in the slice's lines |

The three revision gates above ran over scratch commit
`a564b42314b3bc58554bf44398b88d947fb04480` (dangling, no ref; `HEAD` and the worktree unchanged) of
these bytes, which differ from the final record only in this sentence naming that commit; the
sanitize gates ran on the worktree files. The conductor's post-commit run rebinds all revision
gates to the committed revision.
