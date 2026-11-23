# rf-acc-delegated-cgroup-kill — plan

Contract: `goal-rf-acc-delegated-cgroup-kill.md` (GDD state root, external to the repository), with
its binding conductor decision (option b, retarget and relax). Base revision:
`b9d4b4a4cfff1467ce9bbd24c127107c884fd077` (#266). Branch:
`refactor/rf-acc-delegated-cgroup-kill`. Goal branch name: same.

Goal shape: retarget `verifyLinuxOwnerParentKillBoundary` (`daemon/internal/linuxpreflight/cgroup_linux.go:130`)
to the boundary the mandated systemd unit (`Delegate=yes` + `DelegateSubgroup=manager`) actually
keeps. The delegated `manager/cgroup.kill` becomes acceptable as root- or A-owned, unwritable by B;
the A `O_WRONLY` refusal it used to carry moves one level up to the owning unit cgroup's own
`cgroup.kill` (root:root, refused for an A `O_WRONLY` open, ENOENT is a failure there); the A-owned
delegated directory, the A-owned writable `cgroup.procs`/`cgroup.subtree_control`, the cgroup2
`statfs` identity and the `RESOLVE_BENEATH` openat2 discipline stay unchanged.

## 1. Owned-file re-derivation at dispatch

| Contract entry | HEAD reality | Action |
| --- | --- | --- |
| `daemon/internal/linuxpreflight/cgroup_linux.go:130` `verifyLinuxOwnerParentKillBoundary` | the base function; `:161` requires the manager dir A-owned; `:170-183` the control loop with `cgroup.kill ownerUID: 0`; `:183-193` the A `O_WRONLY` refusal; `:195-200` `validateLinuxOwnerManagerKillMetadata` root-only | rewrite the file's two kill checks, keep the loop's other four controls, delete the manager A-probe |
| `daemon/internal/linuxpreflight/preflight_linux_test.go:280-291` manager-kill rows | three rows inside `TestLinuxOwnerHostPrerequisiteChecksFailClosed` (`a`/`b` = 1001:1002 / 1003:1004) | replace with the contract's acceptance set in a new table-driven test in the same file; at most one new `*_test.go` is allowed and is not needed |
| `docs/evidence/goal-0006-linux-owner-design-2026-09-22.md:613-624` | states "the parent's `cgroup.kill` remained root-owned mode `0200`" and "The parent kill control must remain root-only" | append a dated amendment; no rewrite of the historic text |
| `docs/design/*` | `grep -rn "kill control\|parent kill\|root-only" docs/design/` finds nothing (the rule is stated only in the 0006 evidence record) | no edit; recorded as checked-absent |
| `docs/TODO.md` entry opened by rf-acc-native-hosted-run | not in the base tree: the entry lives on the unmerged branch `refactor/rf-acc-native-hosted-run` (its diff against `b9d4b4a4` adds it at `:311-335`, naming probes `45169655828`, `45169868354`, `45164286155` and the "production-side decision") | close it in `docs/archive/CLOSED-TODO.md` and add one resolution sentence to the pending acceptance entry's recorded status, which is where that branch's text lands |
| `docs/archive/CLOSED-TODO.md` | end of file | append the closure section |
| this plan + `docs/evidence/rf-acc-delegated-cgroup-kill-2026-11-23.md` | new | the goal's own artifacts |

Not owned and untouched: `daemon/internal/platform/**`, the anchor, `scripts/systemd/*`,
`scripts/install-linux-owner.sh`, `scripts/ci/linux-v2-native-acceptance.sh`.

## 2. Public/API shape

No exported symbol changes; `verifyLinuxOwnerParentKillBoundary` keeps its signature and both
internal helpers are package-private.

```go
func validateLinuxOwnerManagerKillMetadata(uid, gid, mode, ownerUID uint32, a, b ACLSubject) error // retargeted
func validateLinuxOwnerUnitKillMetadata(uid, gid, mode uint32, a, b ACLSubject) error              // new
func validateLinuxOwnerKillControlMode(mode uint32) error                                          // new, shared mode rule
func linuxOwnerUnitCgroupKillPath(delegatedPath string) string                                     // new, pure
func statLinuxOwnerCgroupControl(directoryFD int, relativePath string, how *unix.OpenHow) (unix.Stat_t, error) // new
```

- `validateLinuxOwnerManagerKillMetadata`: regular file, owner-write present, no group/other write;
  owner is root (gid 0) or A (gid = `a.GID`); A and B are distinct non-root subjects. A B-owned file
  and any file whose gid does not match its owner are refused.
- `validateLinuxOwnerUnitKillMetadata`: the same mode rule, plus owner root:root and distinct
  non-root A/B subjects.
- `linuxOwnerUnitCgroupKillPath(delegatedPath)` = `path.Dir(delegatedPath) + "/cgroup.kill"`; a
  single-component delegated path (`.`) means the cgroup2 mount root, so the result is plain
  `cgroup.kill`. `value.LinuxOwnerDelegatedPath` is already a validated canonical relative path
  (`daemon/internal/config/config.go:213-217`) and the live anchor's `ManagerPath` must equal it
  (`validateDiscoveredLinuxOwnerAnchor`), so no further canonicality check is needed here.

## 3. Edge-case semantics the contract demands

1. **Why B cannot write the manager file.** With the mode rule (no group/other write) and the owner
   restriction, B's only route to a write is being the owner (`b.UID == uid`), which the helper
   refuses. The group-write bit doubles as the POSIX ACL mask class, so an ACL entry granting B write
   cannot survive `mode&0022 != 0` being false. This is the metadata reading of "B must not be able to
   open it for writing"; the preflight process is A and cannot impersonate B read-only.
2. **Why the unit kill keeps a live probe.** Its metadata rule already refuses an A-owned file, but
   the `O_WRONLY` probe (`EACCES`/`EPERM` required) also refuses a root-owned file that A can still
   write through effective capability or an ACL that leaves the mask class intact. That is the
   contract's "refused for an A `O_WRONLY` open"; a probe that opens or fails with any other errno
   (including ENOENT) refuses.
3. **Retained gates.** Owner-write on both kill files is the base loop's `mustBeWritable` for
   `cgroup.kill`, kept; the manager directory, `cgroup.procs`, `cgroup.subtree_control`,
   `cgroup.events`, `cgroup.controllers`, the `CGROUP2_SUPER_MAGIC` check and every openat2
   `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS|RESOLVE_NO_XDEV` flag are unchanged.
   The manager kill's A `O_WRONLY` refusal and its root-only metadata rule are deleted, exactly as
   the decision states.
4. **One openat2 literal.** The control loop's inline `OpenHow` becomes one `metadataHow` value
   shared by the loop and both kill-file metadata reads (same flags; `Openat2` does not mutate it).
5. **Error surfaces.** Messages name the file and the boundary ("A can open the owning unit
   cgroup.kill for writing; the unit kill control must remain root-only"); the manager message no
   longer claims root-only. No consumer keys on these strings (only the stage's own runs read them).

## 4. Evidence plan

- **Change 1 measurement** (raw, before any code change) on a local WSL2 Ubuntu 24.04 systemd 255
  host carrying a real v2 owner install and the mandated unit: `systemctl --version`, `stat` of both
  kill files, and the A `O_WRONLY` open probe (open only, never write) after start, after
  `daemon-reload`, after restart. Expected: `manager/cgroup.kill` = A:A `0200` and A-writable; the
  unit's own `cgroup.kill` = `0:0 0200`, `EACCES` for A. This settles the record's `:613-617`
  anomaly.
- **Red first.** The retargeted manager metadata rows are added to the existing test file before the
  code change and observed red against the base helper (first red line recorded). The unit-kill rows
  cannot exist at base (no helper); their replacement proof is the hostile-state live row below.
- **Live rows** on the same host, the built daemon binary run as A:
  - base `b9d4b4a4` binary: exit 1 with the base refusal (captured before the change);
  - head binary: exit 0 (`-verify-linux-owner-install` passes as A);
  - head binary with the A capability that makes the root-owned unit kill writable (file capability
    `cap_dac_override`): exit 1 on the unit-kill refusal, proving the live probe;
  - hostile state, unit kill chowned to A while the unit runs: exit 1 on the unit metadata rule,
    then ownership restored and exit 0 again;
  - retarget mutant (unit check reading `manager/cgroup.kill`): exit 1 on the A-write refusal, since
    this host does chown the manager file.
- **Mutant rows, each red then restored** (unit tests unless stated):
  - M1 accept a B-owned manager kill → red;
  - M2 drop the unit-kill root check → red;
  - M3 accept an A-writable unit kill (drop the unit `O_WRONLY` refusal) → red on the capable-binary
    live row;
  - M4 accept a group-writable manager kill → red.
- **Gates** from `daemon/`: `gofmt -l .`, `go vet ./internal/linuxpreflight/...`,
  `GOOS=linux go build ./...`; WSL `go test -race -count=1 ./internal/linuxpreflight/...`; repo
  gates `python scripts/ci/model-routing-record-sanitize.py --check --root-only <each changed doc>`
  and `git diff --check`. No measurement harness runs concurrently with the test suite.

## 5. Out of scope (with reason)

- `daemon/internal/platform/**` runtime (`OpenLinuxCgroupV2Parent`, the anchor kill protocol): it
  already opens the manager kill `O_PATH` only (`verifyLinuxCgroupV2Controls`) and states the file
  "may remain root-owned"; no runtime assertion depends on root ownership.
- The run-scoped exactly-once kill/ACK protocol and the frozen OpenCode members.
- The unit templates, the installer, the acceptance script, and any credentialed acceptance run.
- An installer-side re-chown (contract option a, rejected: systemd undoes it on every start).

## 6. Risks

- **gid rule vs systemd's chown.** The manager file is accepted only with a gid matching its owner.
  The mandated unit's `User`/`Group` are pinned to A's account and A's primary group name
  (`systemd_linux.go` unit policy; measured A:A on this host), and the root-owned unit file is
  `0:0`, so both accepted shapes are the ones systemd produces.
- **Merge order with rf-acc-native-hosted-run.** Its `docs/TODO.md` paragraph will not contain the
  closure sentence this slice adds to the currently-pending acceptance entry; the archive entry and
  the evidence record state the merge-order fact so the resolution is unambiguous.
- **Probe authority.** Both `O_WRONLY` probes assume the preflight process is A, as the base code
  already did; `VerifyLinuxOwnerInstallation` refuses any other identity before this function runs.
- **Capable-binary row.** If the file-capability row turns out to fail earlier for an unrelated
  reason, the record states that and falls back to the chowned-host row plus the retarget mutant as
  the probe's live proof.

## 7. As-built note (2026-11-23, implement stage)

The plan's M3 row is superseded by measurement, and §4's capable-binary row was discarded as the
probe's proof:

- The file-capability row fails earlier than planned: an A client carrying `cap_dac_override=ep` is
  refused during anchor discovery (`sendmsg: broken pipe`), deterministically (2/2 capable
  failures against 2/2 plain successes alternating inside one anchor lifetime), before any cgroup
  check; the refusing check was not identified. Recorded in the evidence record §9/§11.
- M3 became the union mutant it actually needs to be (the unit metadata root check **and** the
  A `O_WRONLY` refusal removed), because either half alone still refuses the chowned file; its
  killer is the live hostile-state row (mutant exit 0 where the fixed code exits 1).
- The probe's live proof is the M2-nometa row instead: with the metadata root rule dropped, the
  probe alone refuses the chowned unit kill
  (`A can open the owning unit cgroup.kill for writing; the unit kill control must remain root-only`).
