# Linux v2 host prerequisite probe: cgroup-v2 precondition and unit-stop proof fix — 2026-11-22

Status: **recording.** Two verified defects in `scripts/ci/linux-v2-host-prerequisite-probe.sh` are fixed on
the owner's G1=a ruling of 2026-11-22. This note records the host facts, the runs that established them, and
the before/after logic. It grants no acceptance: the probe's remaining `cross-uid-*` prerequisite is a
separate host property, and the fixed probe is still the gate that must pass on a qualifying host. Round 2
(below) reclassifies the reported B `CapEff=0` peer failure as a probe defect, records the Ubuntu AppArmor
userns restriction as a real host property with a production consequence, and records the round-2 fixes.

## Verified host facts

Hosted GitHub runner evidence from runs `45007136962`, `45007333006`, `45010498354` (`ubuntu-latest` =
Ubuntu 26.04, systemd 259, kernel 7.0; plus `ubuntu-24.04`):

- systemd is PID 1 and `/sys/fs/cgroup` is `cgroup2fs`, controllers
  `cpuset cpu io memory hugetlb pids rdma misc dmem`.
- The root cgroup has no `cgroup.events`. The kernel creates that file only in non-root cgroups
  (`Documentation/admin-guide/cgroup-v2.rst`), so the old precondition's
  `-f /sys/fs/cgroup/cgroup.events` test could never succeed there: observed `ROOT_EVENTS_ABSENT`,
  `unified_cgroup_v2_mounted=false`, `failure_stage=host_systemd_cgroup_v2_precondition`.
- After a `systemd-run --wait` transient unit exits, systemd 259 reports
  `LoadState=not-found ActiveState=inactive MainPID=0 ControlGroup=` (empty), also with
  `--property=CollectMode=inactive-or-failed`; with `--remain-after-exit` the unit stays `active` with an
  empty `ControlGroup` once no process remains. The old proof required a non-root absolute `ControlGroup`
  *after* exit, so `transient_unit_cleanup` failed on every host — including the WSL2 attempt recorded in
  `goal-0006i-exact-subject-release-acceptance-2026-11-15.md` (line 237).

## Change

| Check | Before | After |
| --- | --- | --- |
| Unified cgroup v2 precondition | `-d /run/systemd/system`, `-f /sys/fs/cgroup/cgroup.controllers`, **and** `-f /sys/fs/cgroup/cgroup.events` | `-d /run/systemd/system`, `-f /sys/fs/cgroup/cgroup.controllers`, and `stat -fc %T /sys/fs/cgroup` = `cgroup2fs`; root `cgroup.events` is no longer required |
| Unit stop proof | after exit: `systemctl show --property=ControlGroup` must yield a non-root absolute path, whose directory is gone or reads `populated 0` | the unit payload records its live `/proc/self/cgroup` `0::` path (`cgroup_path=<absolute>` in the unit log; both payloads print it before their first strict check, so failure branches still carry it). After exit the proof requires `LoadState=not-found` or `ActiveState=inactive|failed`, `MainPID=0`, the recorded path absolute, non-root, free of `..`, ending in the exact unit name under a `*.slice`, and the recorded directory either gone or reading `populated 0` in its `cgroup.events` |
| Evidence JSON | the round-1 field set (`version/case/source_commit/architecture/scope/not_evidence_of/systemd_is_pid1/unified_cgroup_v2_mounted/delegated_leaf_probe_passed/transient_unit_stop_proven/cross_uid_peer_probe_passed/cross_uid_capability/host_a_pidfd_getfd/peer_transient_unit_stop_proven/status/failure_stage`) | those fields plus `apparmor_userns_exception` (`not_needed` when the host has no restriction, `required` from entering the restriction branch, `per_program` only after a successful load); `not_evidence_of` gains "production launcher/gate under the AppArmor unprivileged-userns restriction" whenever the exception was loaded |

A missing or root recorded path fails the proof. The peer unit uses the same proof against the path its own
report records. The python payload and the Go peer probe each print `cgroup_path` before their first strict
check (round 3 moved the Go probe's print from after `checkHostPrerequisites` to before it), so a unit-failure
branch still proves the stop instead of overwriting the earlier `failure_stage` with `transient_unit_cleanup`;
only a host whose `/proc/self/cgroup` carries no single `0::` path has nothing to record.

## Local verification (WSL2 Ubuntu-24.04, systemd 255, kernel 6.18.58.1-microsoft-standard-WSL2)

Clean worktree at `333c572c1cd4ea50972a914ade043a657dd7d7c6`, user `accept`, invoked as
`bash scripts/ci/linux-v2-host-prerequisite-probe.sh` with `GITHUB_RUN_ID=1`, `GITHUB_RUN_ATTEMPT=1`,
`GITHUB_SHA` = that commit, `GITHUB_WORKSPACE` = the worktree, `RUNNER_TEMP` = a private temporary directory.

Unmodified HEAD script (defect 2 reproduced; this host's root cgroup does expose `cgroup.events`, so defect 1
is only observable on the hosted kernels above):

```
{"architecture":"x86_64","case":"linux_v2_host_prerequisite_probe","cross_uid_capability":"not-proven","cross_uid_peer_probe_passed":false,"delegated_leaf_probe_passed":false,"failure_stage":"transient_unit_cleanup","host_a_pidfd_getfd":"not-proven","not_evidence_of":["production owner installation","native Pi/OpenCode execution","WSL2 positive support"],"peer_transient_unit_stop_proven":false,"scope":"systemd, delegated cgroup-v2 leaf, and credential-free cross-UID userns/fdinfo/pidfd_getfd host prerequisites only","source_commit":"333c572c1cd4ea50972a914ade043a657dd7d7c6","status":"failed","systemd_is_pid1":true,"transient_unit_stop_proven":false,"unified_cgroup_v2_mounted":true,"version":1}
```

Fixed script (both stop proofs now pass; the remaining failure is this host's unrelated cross-UID property and
is no longer masked by the cleanup stage):

```
{"architecture":"x86_64","case":"linux_v2_host_prerequisite_probe","cross_uid_capability":"not-proven","cross_uid_peer_probe_passed":false,"delegated_leaf_probe_passed":true,"failure_stage":"cross_uid_userns_fdinfo_pidfd_getfd","host_a_pidfd_getfd":"not-proven","not_evidence_of":["production owner installation","native Pi/OpenCode execution","WSL2 positive support"],"peer_transient_unit_stop_proven":true,"scope":"systemd, delegated cgroup-v2 leaf, and credential-free cross-UID userns/fdinfo/pidfd_getfd host prerequisites only","source_commit":"333c572c1cd4ea50972a914ade043a657dd7d7c6","status":"failed","systemd_is_pid1":true,"transient_unit_stop_proven":true,"unified_cgroup_v2_mounted":true,"version":1}
```

That host's peer probe ends at `status=failed stage=cross-uid-b-capabilities-nonzero detail="B must have
CapEff=0"`; post-exit `systemctl show` for both units reports `LoadState=not-found ActiveState=inactive
MainPID=0` with no `ControlGroup`, which is exactly the defect-2 input. The peer unit's recorded path in its
report was `/system.slice/<peer unit>`. Round 2 identifies that stage as a probe defect (ambient-capability
inheritance), not a host property; see below.

No-weakening check with `unit_stopped_proven` extracted verbatim from the fixed script and run against a live
transient unit `symmetry-ci-negtest.service` (recorded cgroup
`/system.slice/symmetry-ci-negtest.service`): a running unit, an empty recorded path, `/`, and a foreign
unit's path are each rejected; the same unit after `systemctl stop` is accepted.

`bash -n` exits 0; `shellcheck --severity=style` 0.11.0 reports nothing.

## Round 2 (2026-11-22): CapEff reclassification, AppArmor userns restriction, and the peer-log proof echo

The round-1 note above left the `cross-uid-b-capabilities-nonzero` peer failure unexplained as "a separate
host property". Round 2 (owner ruling G1=a) established that it was a **probe defect**, and that hosted
Ubuntu runners additionally carry an AppArmor userns restriction the probe did not handle.

### `cross-uid-b-capabilities-nonzero` was a probe defect

`runCrossUIDProbe` raised ambient capabilities `{CAP_SETGID, CAP_SETUID, CAP_SYS_PTRACE}` on the namespace
gate (`SysProcAttr.AmbientCaps`), and the gate's exec passes the ambient set to the namespace worker. The
fixed B-only map (`1 65534 1`) leaves in-namespace ID 0 unmapped, so the worker's launch of B with
`Credential{Uid:1,Gid:1}` performs `setuid(1)` from a non-root in-namespace UID: the kernel's
`cap_emulate_setxuid()` rule (`capabilities(7)`: capabilities are cleared only when a process that is root
*in its user namespace* changes to a nonzero UID) never fires, B's exec keeps `P'(ambient)`, and B reports
`CapEff=0x800c0` (bits 6, 7, 19). The fix clears the worker's ambient set with
`PR_CAP_AMBIENT_CLEAR_ALL` before B's launch; the B-only maps and the `CapEff=0` check are unchanged. The
ambient clear alone still left B at `CapInh=0x800c0`; round 3 below closes that gap.

Production never depended on kernel clearing here: `daemon/native/linux-gate/c-gate/gate.c` `drop_to_b`
(line 998) clears the sets explicitly and `read_self_status` (line 932) verifies zeros, so this stage said
nothing about the production gate or the host.

### Ubuntu AppArmor restriction on unprivileged user namespaces

Hosted `ubuntu-latest`/`ubuntu-24.04` runs after the round-1 fix proceed through the cgroup and unit checks
and then run with `kernel.apparmor_restrict_unprivileged_userns=1` (Ubuntu default). When an *unconfined*
process creates a user namespace, the kernel transitions it into AppArmor's `unprivileged_userns` profile,
which denies capability changes; the namespace worker's `Credential{Uid:1,Gid:1}` launch of B then fails
with `fork/exec /proc/self/exe: operation not permitted`. Two fixes, in the wrapper and in the probe:

- `scripts/ci/linux-v2-host-prerequisite-probe.sh` loads a per-program exception for the exact fresh peer
  binary path when the sysctl is 1: a run-unique profile
  `profile symmetry-ci-v2-host-probe-userns-<run>-<attempt> "<peer binary>" flags=(unconfined) { userns, }`
  through `apparmor_parser --replace`, unloaded in `cleanup()` with `apparmor_parser -R`. The global sysctl
  is never changed, and the profile grants only `userns` on one ephemeral path (the same pattern as
  `scripts/ci/prepare-private-namespace-tests.sh`). The run records
  `apparmor_userns_exception=not_needed|per_program` as a log line and as an additive evidence JSON field;
  a missing parser or a failed load fails at `failure_stage=apparmor_userns_exception`. Round 3 adds the
  intermediate `required` state, makes a failed unload set `cleanup_failed`, and names the production
  launcher/gate caveat in `not_evidence_of` whenever the exception was loaded.
- The Go probe fails early at `apparmor-unprivileged-userns-restricted` when the sysctl is 1 and
  `/proc/self/attr/current` (or `/proc/self/attr/apparmor/current`) still reads exactly `unconfined`,
  instead of the misleading B-launch EPERM that follows the transition.

### Peer-log proof echo (latent defect surfaced once the probe could pass)

The wrapper's peer-log marker check requires `namespace-proof=passed` and `tcp_state=ESTABLISHED`, but the
namespace worker prints its proof to A's private pipe, so the peer unit log never carried those two markers;
the check had never been reached because the peer unit always failed earlier. A now re-echoes the proof line
it has already validated in-process (`runCrossUIDProbe`), and the required-marker list is unchanged.

### Round-2 local verification (WSL2 Ubuntu-24.04, systemd 255, kernel 6.18.58.1-microsoft-standard-WSL2)

Script run from `<worktree>` as user `accept` with `RUNNER_TEMP=<scratch>`, `GITHUB_RUN_ID=1`,
`GITHUB_RUN_ATTEMPT=1`. This host has no `apparmor_restrict_unprivileged_userns`, so
`apparmor_userns_exception=not_needed`. The JSON below is reproduced with one correction: the run recorded
`source_commit=68b7dbb5a606603fe04bd6c8e79cfbb6f22b1377`, but that revision carried no AppArmor code at all
(`git show 68b7dbb5:scripts/ci/linux-v2-host-prerequisite-probe.sh` contains no `apparmor`), so the value was
a stale label. The probe files that produced the JSON are those of
`67619892b26f758d91cd1fc832d580c9da2ea7de`, which is what it binds here:

```json
{"apparmor_userns_exception":"not_needed","architecture":"x86_64","case":"linux_v2_host_prerequisite_probe","cross_uid_capability":"fixed-b-child-userns-fdinfo-pidfd-getfd","cross_uid_peer_probe_passed":true,"delegated_leaf_probe_passed":true,"failure_stage":"","host_a_pidfd_getfd":"passed","not_evidence_of":["production owner installation","native Pi/OpenCode execution","WSL2 positive support"],"peer_transient_unit_stop_proven":true,"scope":"systemd, delegated cgroup-v2 leaf, and credential-free cross-UID userns/fdinfo/pidfd_getfd host prerequisites only","source_commit":"67619892b26f758d91cd1fc832d580c9da2ea7de","status":"passed","systemd_is_pid1":true,"transient_unit_stop_proven":true,"unified_cgroup_v2_mounted":true,"version":1}
```

The probe now passes end to end on this host — including the B `CapEff=0` stage that previously failed —
with `bash -n` exit 0, `shellcheck --severity=style` 0.11.0 silent, `gofmt -l` empty, and
`GOOS=linux ... go build`/`go vet` clean on the probe file. The AppArmor branch cannot run on this WSL
kernel (no userns mediation), so it is exercised only by the hosted-runner run.

### Hosted-runner verification (round 2)

GitHub Actions run `45030128797` on branch `scratch/linux-v2-host-probe` ran this branch's probe at commit
`67619892b26f758d91cd1fc832d580c9da2ea7de` unchanged on `ubuntu-latest` (Ubuntu 26.04, systemd 259) and
`ubuntu-24.04`. Both jobs logged `apparmor_userns_exception=per_program` and wrote evidence with
`status=passed`, `failure_stage` empty, `unified_cgroup_v2_mounted`, `delegated_leaf_probe_passed`,
`transient_unit_stop_proven`, `cross_uid_peer_probe_passed`, and `peer_transient_unit_stop_proven` all true,
`cross_uid_capability=fixed-b-child-userns-fdinfo-pidfd-getfd`, and `host_a_pidfd_getfd=passed`.

The scratch branch adds only a workflow file that runs the probe on push (its head,
`61900f54cd5fc4aea5fd73414e196fa96e785271`, is recorded as the evidence `source_commit`); it is not part of
this change. Earlier diagnostic runs on the same scratch branch: `45007136962`, `45007333006`,
`45010498354`, `45027997457`, `45028149850`.

**Production consequence:** the AppArmor restriction is a real host property, not a CI artifact. A
production Linux v2 owner host with the restriction enabled and an unconfined launcher/gate would fail the
same way (`unshare(CLONE_NEWUSER)` followed by `drop_to_b`), so `scripts/install-linux-owner.sh` needs its
own per-program `userns` exception (or the sysctl must be relaxed), to be specified by the D3 `harness.md`
amendment / W5-02a; the native acceptance fixture (`scripts/ci/linux-v2-native-acceptance.sh`) needs the
same exception before its case sets can pass. Tracked in `docs/TODO.md`.

## Round 3 (2026-11-22): fix round for the two independent reviews of this branch

One review approved the branch with P3 items; the other required changes. Round 3 answers them in the probe
files and in this note's records; every gate keeps its round-2 direction (fail-closed), and none is widened:

- The Go peer probe prints `cgroup_path` right after reading its own `/proc/self/cgroup`, before every
  `checkHostPrerequisites` branch, and the python payload prints it before its first strict check. An early
  prerequisite failure therefore still lets `cleanup()` prove the unit stop and keeps the peer phase's
  `failure_stage` instead of overwriting it with `transient_unit_cleanup`.
- The namespace worker clears its inheritable capability set (`capset`, effective/permitted preserved, on the
  thread that forks B) and B must show `CapEff=CapPrm=CapInh=CapAmb=0`; the ambient clear alone left
  `CapInh=0x800c0`, because `execve` preserves the inheritable set. The root group-preparation helper's
  credential-drop check now also requires `CapInh=0`.
- The AppArmor branch records `apparmor_exception=required` from the moment it is entered (`per_program` only
  after a successful load), a failed unload in `cleanup()` sets `cleanup_failed`, and a run that loaded the
  exception adds "production launcher/gate under the AppArmor unprivileged-userns restriction" to
  `not_evidence_of`.
- `scripts/ci/test-linux-v2-host-prerequisite-probe.py` pins `unit_stopped_proven`'s path validation (empty,
  relative, `/`, `..`, foreign unit name, non-`.slice` parent, running/activating unit, non-zero `MainPID`,
  failing `systemctl show`) and the two accepted stopped shapes, with the systemd calls stubbed in-process.
  It runs beside the other `test-linux-v2-*.py` fixtures in the `daemon` job's "Check Linux v2 fixture
  contracts" step.

### Round-3 local verification (same WSL2 host)

`bash -n` exits 0; `shellcheck --severity=warning` and `--severity=style` (0.9.0) report nothing; `gofmt -l`
is empty; `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOFLAGS=-mod=readonly go vet` on the probe exits 0. The new
suite passes 13/13; reverting its basename guard (`"${recorded_cgroup##*/}" == "$unit_name"`) reddens
`test_foreign_unit_name_is_rejected` — the foreign path is then accepted — and the guard-extraction
assertion, after which the guard is restored and the suite passes again.

Full probe run from the final worktree as user `accept` via a temporary copy of the wrapper
(`wsl.exe -d Ubuntu-24.04 -u accept bash <scratch copy>`) with `RUNNER_TEMP=<scratch>`, `GITHUB_RUN_ID=1`,
`GITHUB_RUN_ATTEMPT=1` and `GITHUB_SHA=3013ede0fd83b8f5382f2f5cd343065ac018f908`; it used the final probe
files (`sha256` `2d661805652548c957720ce4cd2e92f8673e25d6052c12f44830793365e540aa` wrapper,
`fde1238995fcadd4de22964c8bac9f21eea8282c2e2e4a35704617c96ebb772e` peer probe) from a worktree whose HEAD is
that commit plus this round-3 fix set, which was uncommitted when the run was taken:

```json
{"apparmor_userns_exception":"not_needed","architecture":"x86_64","case":"linux_v2_host_prerequisite_probe","cross_uid_capability":"fixed-b-child-userns-fdinfo-pidfd-getfd","cross_uid_peer_probe_passed":true,"delegated_leaf_probe_passed":true,"failure_stage":"","host_a_pidfd_getfd":"passed","not_evidence_of":["production owner installation","native Pi/OpenCode execution","WSL2 positive support"],"peer_transient_unit_stop_proven":true,"scope":"systemd, delegated cgroup-v2 leaf, and credential-free cross-UID userns/fdinfo/pidfd_getfd host prerequisites only","source_commit":"3013ede0fd83b8f5382f2f5cd343065ac018f908","status":"passed","systemd_is_pid1":true,"transient_unit_stop_proven":true,"unified_cgroup_v2_mounted":true,"version":1}
```

Disabling only the inheritable clear and re-running the same wrapper reddens the new assertion exactly as the
review measured: `status=failed stage=cross-uid-b-capabilities-nonzero detail="B must have CapInh=0"` (peer
log) with `failure_stage=cross_uid_userns_fdinfo_pidfd_getfd` and `peer_transient_unit_stop_proven=true` in
the JSON — the ambient clear alone leaves B inheritable-nonzero, and the early `cgroup_path` line is what
keeps the stop proof and the real stage. The clear is then restored (peer-probe `sha256` back to
`fde1238995fcadd4de22964c8bac9f21eea8282c2e2e4a35704617c96ebb772e`), and the final run passes again,
producing the JSON above.

Forcing the probe's first prerequisite branch (`user.max_user_namespaces=0`, restored after the run) shows
the same stop-proof behaviour on a real early prerequisite failure: the peer log carries
`status=failed stage=user-namespaces-disabled detail="user.max_user_namespaces must be positive"` after the
early `cgroup_path` line, and the JSON reports `status=failed` with
`failure_stage=cross_uid_userns_fdinfo_pidfd_getfd` (the peer phase, *not* `transient_unit_cleanup`) plus
`transient_unit_stop_proven` and `peer_transient_unit_stop_proven` true.

The AppArmor branch cannot run on this WSL kernel (no userns mediation); its round-3 edits are exercised only
by code reading and by the hosted runner's next run on this branch.

## Adjacent code check (no changes made)

- `daemon/internal/linuxpreflight/systemd_linux.go` `verifyEffectiveLinuxSystemdUnit` (line 123) requires
  `ControlGroup=/system.slice/symmetry-owner.service` only together with `ActiveState=active` and
  `SubState=running` (lines 238-243) — a *running* persistent unit, queried from the running daemon
  (`cmd/symmetry-daemon/main.go:262`, `daemon/internal/app/linux_owner_v2_linux.go:91,212`). No post-exit
  `ControlGroup` dependency.
- `daemon/internal/platform/linux_cgroup_v2_linux.go` `OpenLinuxCgroupV2Parent` (line 181, control list
  lines 1200-1214) and `daemon/internal/linuxpreflight/cgroup_linux.go`
  `verifyLinuxOwnerParentKillBoundary` (line 130, control list line 162) require a readable `cgroup.events`
  only on the delegated, non-root manager subgroup. `verifyLinuxNativeRuntimeSelection`
  (`cgroup_linux.go:55`, guard at line 79) rejects an empty `LinuxOwnerDelegatedPath` before either can reach
  the mount root; `canonicalDelegatedCgroupPath` (`linux_cgroup_v2_linux.go:1324`) alone would accept `""`
  (latent, unreachable through configuration).
- No code under `daemon/internal/execution/` or `daemon/internal/app/linux_owner_v2_linux.go` reads
  `ControlGroup`, `ExecMainPID`, or `cgroup.events`; `systemctl` appears only under
  `daemon/internal/linuxpreflight/`. Leaf-level `cgroup.events` reads
  (`platform/linux_cgroup_v2_linux.go:718,1008`, `native/linux-gate/protocol/launcher_integration_linux_test.go`,
  `e2e/production_linux_v2_owner_cli_test.go:4037`) address delegated non-root leaves.
- `scripts/ci/linux-v2-native-acceptance.sh` already learns the guest cgroup while the unit is active and
  proves the stop against that pinned path (`pin_guest_cgroup_while_active` line 109,
  `guest_stopped_proven` line 90); its host check (lines 53-54) never required root `cgroup.events`. No change
  needed for these two defects, and no normative document states root `cgroup.events` as a host prerequisite.

## Not evidence of

Production owner installation, native Pi/OpenCode execution, or WSL2 positive support. A passing probe on
one host (the WSL round-3 run above, or a hosted runner with the AppArmor exception loaded) proves only the
listed host prerequisites; a host missing any of them still fails, and the AppArmor exception branch is
verified only where the kernel enforces the sysctl (the WSL host has none). When that exception was loaded,
the JSON's `not_evidence_of` names the production launcher/gate under the restriction explicitly, so a
JSON-only consumer cannot read the pass as production evidence.

## Residual risk

- `unit_stopped_proven` still takes each payload's self-reported cgroup path and shape-checks it without
  cross-checking the live unit's systemd `ControlGroup`/cgroup identity (the `linux-v2-native-acceptance.sh`
  `guest_stopped_proven` pattern), so a payload that printed a well-shaped but wrong path would fall back to
  the systemd-state proof alone. Accepted for this change (review P3-4); narrowing it needs an injection
  experiment with a misreporting payload.
