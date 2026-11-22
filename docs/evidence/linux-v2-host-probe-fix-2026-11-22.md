# Linux v2 host prerequisite probe: cgroup-v2 precondition and unit-stop proof fix — 2026-11-22

Status: **recording.** Two verified defects in `scripts/ci/linux-v2-host-prerequisite-probe.sh` are fixed on
the owner's G1=a ruling of 2026-11-22. This note records the host facts, the runs that established them, and
the before/after logic. It grants no acceptance: the probe's remaining `cross-uid-*` prerequisite is a
separate host property, and the fixed probe is still the gate that must pass on a qualifying host.

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
report was `/system.slice/<peer unit>`.

No-weakening check with `unit_stopped_proven` extracted verbatim from the fixed script and run against a live
transient unit `symmetry-ci-negtest.service` (recorded cgroup
`/system.slice/symmetry-ci-negtest.service`): a running unit, an empty recorded path, `/`, and a foreign
unit's path are each rejected; the same unit after `systemctl stop` is accepted.

`bash -n` exits 0; `shellcheck --severity=style` 0.11.0 reports nothing.

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

Production owner installation, native Pi/OpenCode execution, or WSL2 positive support; the fixed probe still
reports `status=failed` on hosts whose cross-UID prerequisite (fixed-B `CapEff=0`) does not hold.
