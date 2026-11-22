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
| Unit stop proof | after exit: `systemctl show --property=ControlGroup` must yield a non-root absolute path, whose directory is gone or reads `populated 0` | the unit payload records its live `/proc/self/cgroup` `0::` path (`cgroup_path=<absolute>` in the unit log; the peer Go probe already prints the same key in its report, the python payload now prints it). After exit the proof requires `LoadState=not-found` or `ActiveState=inactive|failed`, `MainPID=0`, the recorded path absolute, non-root, free of `..`, ending in the exact unit name under a `*.slice`, and the recorded directory either gone or reading `populated 0` in its `cgroup.events` |
| Evidence JSON | `version/case/source_commit/architecture/scope/not_evidence_of/systemd_is_pid1/unified_cgroup_v2_mounted/delegated_leaf_probe_passed/transient_unit_stop_proven/cross_uid_peer_probe_passed/cross_uid_capability/host_a_pidfd_getfd/peer_transient_unit_stop_proven/status/failure_stage` | identical fields and semantics |

A missing or root recorded path fails the proof. The peer unit uses the same proof against the path its own
report records; the payloads also record the path in unit-failure branches so cleanup can still prove the stop
and does not overwrite an earlier `failure_stage` with `transient_unit_cleanup`.

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
`PR_CAP_AMBIENT_CLEAR_ALL` before B's launch; the B-only maps and the `CapEff=0` check are unchanged.

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
  `apparmor_userns_exception=per_program|not_needed` as a log line and as an additive evidence JSON field;
  a missing parser or a failed load fails at `failure_stage=apparmor_userns_exception`.
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
`GITHUB_RUN_ATTEMPT=1`, `GITHUB_SHA=68b7dbb5a606603fe04bd6c8e79cfbb6f22b1377`. This host has no
`apparmor_restrict_unprivileged_userns`, so `apparmor_userns_exception=not_needed`:

```json
{"apparmor_userns_exception":"not_needed","architecture":"x86_64","case":"linux_v2_host_prerequisite_probe","cross_uid_capability":"fixed-b-child-userns-fdinfo-pidfd-getfd","cross_uid_peer_probe_passed":true,"delegated_leaf_probe_passed":true,"failure_stage":"","host_a_pidfd_getfd":"passed","not_evidence_of":["production owner installation","native Pi/OpenCode execution","WSL2 positive support"],"peer_transient_unit_stop_proven":true,"scope":"systemd, delegated cgroup-v2 leaf, and credential-free cross-UID userns/fdinfo/pidfd_getfd host prerequisites only","source_commit":"68b7dbb5a606603fe04bd6c8e79cfbb6f22b1377","status":"passed","systemd_is_pid1":true,"transient_unit_stop_proven":true,"unified_cgroup_v2_mounted":true,"version":1}
```

The probe now passes end to end on this host — including the B `CapEff=0` stage that previously failed —
with `bash -n` exit 0, `shellcheck --severity=style` 0.11.0 silent, `gofmt -l` empty, and
`GOOS=linux ... go build`/`go vet` clean on the probe file. The AppArmor branch cannot run on this WSL
kernel (no userns mediation), so it is exercised only by the hosted-runner run.

**Production consequence:** the AppArmor restriction is a real host property, not a CI artifact. A
production Linux v2 owner host with the restriction enabled and an unconfined launcher/gate would fail the
same way (`unshare(CLONE_NEWUSER)` followed by `drop_to_b`), so `scripts/install-linux-owner.sh` needs its
own per-program `userns` exception (or the sysctl must be relaxed), to be specified by the D3 `harness.md`
amendment / W5-02a; the native acceptance fixture (`scripts/ci/linux-v2-native-acceptance.sh`) needs the
same exception before its case sets can pass. Tracked in `docs/TODO.md`.

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
one host (the WSL round-2 run above, or a hosted runner with the AppArmor exception loaded) proves only the
listed host prerequisites; a host missing any of them still fails, and the AppArmor exception branch is
verified only where the kernel enforces the sysctl (the WSL host has none).
