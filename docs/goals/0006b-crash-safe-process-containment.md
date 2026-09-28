# 0006b - Crash-safe process containment

Make process authority survive daemon or owner failure before and after native
launch without orphaning a child, resuming an unauthorized child, duplicating
ownership, or clearing containment before durable stop proof exists. Windows
supervisor handoff and Linux descendant cleanup remain platform-correct.

Completion evidence covers prepare, bind, commit, lease arm, resume, stop
receipt, release, and clear boundaries, including lost-response replay and hard
crashes at each externally visible point. Production-binary witnesses and
native platform tests show bounded cleanup and preserve unresolved state when
physical stop cannot be proven.

The Windows production witness also exercises a structurally valid wrong-secret
request as the first named-pipe client after the inherited bootstrap. The helper
must reject it without changing the Job or target, and the legitimate hello
must then complete within the same absolute deadline.

For **new Linux native executions**, the verified boundary includes the native
CLI and every descendant, including detached and reparented Pi/OpenCode
children that leave the original Unix process group. A delegated cgroup-v2
run leaf is an exact, durable ownership subject: no native instruction executes
before the existing handoff/lease authorization, and release/clear require a
version-matched durable physical-stop receipt covering that leaf and its
descendants. An unknown kill, migration, owner identity, reaping or receipt
outcome remains durable and fail closed; neither PID absence nor an old
process-group scan is a cgroup stop receipt. Existing Linux v1 group journals
must recover under their own rules, never forged into v1.2 receipts. Windows
ownership and protocol v1 behavior remain unchanged. This goal adds no 32-bit
target; Windows execution remains x86-64/amd64.

The supported Linux native deployment is **x86-64/amd64** on an
operator-provisioned systemd host, including WSL2 only when it provides the
same verified systemd delegation and service lifecycle **and** permits UID A,
without host ptrace privileges, to verify real namespace identities, read
B's fdinfo and duplicate the exact connected B socket using `pidfd_getfd`
after B's credential drop. A WSL2 kernel denying these checks fails closed;
systemd alone does not certify native support. It requires exclusive
cgroup delegation to daemon/helper UID A, an installed
root-owned transient launcher restricted to one distinct native UID B,
operator-provisioned B HOME/credentials and approved workspace/Git access.
A and B do not share host credentials or cgroup control permissions. The
fixed B UID is a shared native-run filesystem trust domain, **not** per-run
workspace confidentiality. For new Linux native runs, A owns the source Git
repository and durable workspace intent; after the existing handoff and lease
authorization, B creates its own per-run standalone clone from an A-controlled
seed. A never runs Git against B-owned configuration or treats B's Git metadata
as ownership proof; a proposed commit is validated and promoted in A-owned
storage before acceptance. Existing Windows and Linux v1 linked-worktree
behavior remains unchanged. The existing rootless Compose daemon has no such
delegation or launcher: new native execution there fails closed, without a
group-only or same-UID fallback; legacy journal recovery remains required.
The delegated helper must survive an ordinary daemon-process crash under the
supported service policy. Manager termination of both owners needs a separate
bounded recovery/stop proof and cannot invent a receipt. Pi and OpenCode
positive native tests must run under the actual distinct-UID boundary, not
only a same-UID capability probe. Linux needs real native launch,
descendant-stop and privilege-boundary evidence. WSL2 needs the same positive
evidence only on a kernel passing the exact A/B peer checks; this observed
denying WSL2 kernel instead needs a real fail-closed witness and must not be
reported as natively supported. An unprovisioned WSL2 instance also fails
closed without reverting to the old process group.

This goal excludes harness-specific terminal semantics and capability promotion.
It remains one independently mergeable PR and preserves journal compatibility;
the former 12-hour single-agent-run limit does not apply to this authorized
Linux scope expansion. The goal stays active until the exact-subject native,
crash/replay and independently reviewed security evidence passes.
