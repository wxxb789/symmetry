# Goal 0006b acceptance-environment audit

Date: 2026-09-29
Disposition: **NO QUALIFIED ACCEPTANCE TARGET IS CURRENTLY RUNNABLE**
Goal status: **ACTIVE / acceptance incomplete**

## Scope and evidence boundary

This read-only audit covers
`docs/goals/0006b-crash-safe-process-containment.md`, the surface QA and
convergence receipt dated 2026-09-29, documented installation/native witness
scripts, local Windows/WSL/Docker surfaces, and GitHub metadata for
`wxxb789/symmetry`. No heavy witness was recreated. No service, Docker workload,
credential, runtime, permission, kernel setting or external resource was
changed. No credential value or credential-file content was read. GitHub queries
were limited to names, presence and runner/artifact metadata.

The checkout is branch `codex/goal-0006b-crash-safe-process-containment` at
`d14debbf6ae3ded09e5013ef90952d347c01474d`. Before this report it had 67
tracked-change entries and 54 untracked entries. The tree is concurrent and not
frozen; the convergence receipt remains **INCOMPLETE** and binds no exact tested
commit. Actual audit route: provider `ghc`, model `gpt-5.6-sol-fast`, reasoning
level `medium`.

Terms used below: **unavailable** means the runtime/input is absent;
**inaccessible** means a facility may exist but this caller cannot qualify it;
**unconfigured** means the mechanism exists without its required installation or
inputs; **unqualified** means necessary properties exist but the exact boundary
has not passed. Systemd or cgroup v2 alone is never a support claim.

## Route decision

### Recommended positive route: protected GitHub disposable systemd guest

The intended route is the working-tree `linux-native-v2-acceptance` job. On
explicit `workflow_dispatch`, an amd64 `ubuntu-latest` host builds the exact
subject, creates a private Ubuntu rootfs, boots a throwaway systemd guest with
`systemd-nspawn` in a delegated transient unit, installs fixed A/B identities,
and exercises the production owner/launcher and real Pi/OpenCode adapters. The
script checks systemd PID 1, unified cgroup v2, ACL/tool availability, source
identity, the installed A/B boundary, exact peer verification and bounded guest
stop before success.

This is the smallest documented isolated route and is recommended, but it is
currently **unconfigured and unavailable**, not failed:

- The dispatch, host-prerequisite and native-acceptance jobs exist only in the
  dirty local workflow. Recent remote runs contain none of them.
- GitHub returned zero repository or Environment secret names and zero repository
  or Environment variables.
- Environment `linux-v2-native-acceptance` exists, admits only the Goal 0006b
  branch, requires reviewer `wxxb789`, and has `prevent_self_review=false`.
  This matches the current one-writer workflow comment but qualifies no host.
- No repository self-hosted runners are registered. An older remote
  `cgroup-v2-delegation-probe` failed, but predates the new dirty-tree exact peer
  probe and does not decide that probe.
- No native-acceptance or host-prerequisite artifact metadata exists remotely.

Next action: freeze and publish one reviewed exact commit, configure the four
protected inputs below and a durable raw archive, then manually dispatch this
Environment-protected job. Every preflight and case set must pass; an image label
is not acceptance evidence.

### WSL surfaces and denial-witness defect

`Ubuntu-24.04` is **unconfigured and known-unqualified for positive support**.
Read-only metadata showed Linux x86-64, kernel
`6.18.40.1-microsoft-standard-WSL2`, systemd PID 1 in degraded state, and
unified cgroup-v2 files. `/etc/symmetry/linux-owner.conf` is a root-owned 0644
regular file; its contents were not read. `/etc/symmetry/daemon.json` is absent.
Both Symmetry units report `LoadState=not-found` and `ActiveState=inactive`.

There is also a concrete script defect. At
`scripts/ci/wsl2-v2-denial-witness.sh:20`, the provisioned-host guard is:

```bash
[[ -d /run/systemd/system && -d /sys/fs/cgroup/cgroup.controllers ]]
```

On available Ubuntu WSL, `cgroup.controllers` is a root-owned, mode-0444,
zero-byte **regular file**. `[ -f ... ]` passed and `[ -d ... ]` failed, while
`/run/systemd/system` was present. Line 20 therefore emits `the provisioned WSL2
instance must run systemd on unified cgroup v2` and exits before checking owner
service, config, Control claim or exact peer denial. Native acceptance correctly
uses `-f`. This is a witness-script bug, not evidence that cgroup v2 or the
fixture is absent. The script was not edited. Next code action is a focused `-d`
to `-f` correction with separate prerequisite verification.

Fixing the guard is necessary but insufficient. Independent blockers remain:
the owner unit is not installed/active, `daemon.json` is absent, and no root-owned
A-readable OpenCode config, authentic Control Claim request under the owner-test
request root, or A-readable/B-writable marker root was supplied. The prior exact
peer observation remains binding: this WSL kernel denied required cross-UID
`pidfd_getfd`. Ubuntu WSL may only provide the authentic fail-closed witness.

`symmetry-race` is **unavailable** as an installed target: it is amd64 on the
same WSL2 kernel, PID 1 is WSL init, `/run/systemd/system` and both Symmetry
configs are absent. Its cgroup files alone do not qualify it.

### Local Hyper-V and Docker

Windows reports an active hypervisor; `Get-VM` exists and the Hyper-V management
service runs. VM/switch inventory was **inaccessible** because this caller lacks
Hyper-V authorization; feature inspection required elevation. No VM can be
claimed. An operator-provided amd64 Linux VM is permissible only if it passes the
same systemd, delegation, fixed A/B, peer, lifecycle and physical-stop checks.
No ready local VM fixture is documented, and Control provisioning requires
GitHub Actions. Use the GitHub route unless an operator supplies an authorized VM
and equivalent fixture contract.

Docker client 29.8.0 and Compose exist, but the Desktop Linux engine named pipe
is absent. Rootless runtime refusal is **unavailable**, not failed. Even with an
engine it proves rejection only; it cannot qualify positive native support.

## Exact credentials and authentic Control inputs

| Protected input | Exact contract | Current disposition / next action |
|---|---|---|
| `SYMMETRY_LINUX_V2_TEST_GITHUB_OAUTH_TOKEN` -> `GH_TOKEN` | Secret-backed `gho_` GitHub OAuth user token accepted by Control, with access for tested `resource.sync` on `wxxb789/symmetry`. Hosted `GITHUB_TOKEN` is rejected. It belongs to host Control, not B HOME. | Unconfigured. Add a dedicated test token to the protected Environment after approving non-publishing repository sync. |
| `SYMMETRY_LINUX_V2_TEST_OPENAI_API_KEY` -> `SYMMETRY_OPENAI_API_KEY` | Isolated paid/test OpenAI credential used by real Pi and OpenCode. It is installed only in B-owned mode-0600 auth files; A may traverse known paths but cannot read/write them. | Unconfigured. Add a budget-limited key and authorize real provider calls. |
| `SYMMETRY_LINUX_V2_TEST_PI_MODEL` -> `SYMMETRY_PI_MODEL` | Operator-approved Pi model token matching `^[A-Za-z0-9._-]+$`. | Unconfigured. Supply one available model ID. |
| `SYMMETRY_LINUX_V2_TEST_OPENCODE_MODEL` -> `SYMMETRY_OPENCODE_MODEL` | Operator-approved OpenCode model token with the same grammar; B config uses `openai/<model>`. | Unconfigured. Supply one available model ID. |

The workflow generates a per-run operator token and supplies the isolated
enrollment token; neither substitutes for provider credentials. Its production
Control and PostgreSQL are run-isolated. The fixture creates a unique Project,
GitHub Integration connection, Pi repository resource, separate OpenCode
resource and 44 exact-subject Work Items. It enrolls a distinct daemon Machine,
schedules one Run and claims it through Control.

The owner request must preserve the authentic ClaimResponse RunKey, generation,
runtime ID/epoch, ClaimID, lease token, Work binding and exact Subject. Pi also
requires Control-issued claim-scoped `provider_access` granting GitHub repository
`resource.sync` for that exact resource, followed by one matching succeeded
`ProviderActionIntent` readback. OpenCode must omit provider access. Synthetic
success, a hand-written request, a generic machine token or tool execution alone
does not qualify.

Local presence checks found none of these provider/model/Control/witness names.
GitHub's repository and Environment secret-list calls succeeded and returned
zero names. That is evidence for those scopes at query time. In general, a
permission failure while listing secret metadata would not prove absence; no
such inference is made here.

## Host and native witness prerequisites

The positive route must prove all of the following:

1. Linux amd64, systemd PID 1, unified cgroup v2, delegated transient units,
   `cgroup.kill`, systemd tooling, ACLs and documented build/fixture tools.
2. A credential-free peer probe creating fixed B in a child user namespace,
   proving namespace identity, B fdinfo, the established socket and exact
   `pidfd_getfd` duplication while A has no effective host capabilities; plus
   delegated-leaf kill and bounded unit cleanup. WSL EPERM/EACCES is denial.
3. Distinct non-root A/B UIDs/GIDs, dedicated A group, A-owned source, seed,
   StateDir and App RunJournal, and B-owned private HOME/credentials and clone.
   A must never consume B's Git config.
4. Root-owned static amd64 owner artifacts and reviewed units, canonical matching
   daemon/owner StateDir, durable install intent and separately operator-activated
   services. Installation or systemd presence alone is insufficient.
5. Pinned Pi 0.87.1 and OpenCode 1.18.32 binaries at documented hashes. Tool
   provenance alone is insufficient.
6. Every isolated case set, stage/ACK-loss boundary, detached crash, stale
   identity/journal, exact receipt/release/clear, authentic Pi action, OpenCode
   health, unprovisioned refusal and cleanup result bound to one exact commit.
   Unknown stop or a missing path can never become proof.

## Raw evidence and journal archival

There is **no configured durable protected raw-evidence destination**. The native
script retains private raw logs, credential-bearing request context, RunJournals,
checkouts and recovery material on the ephemeral runner, and exports only
schema-allowlisted, credential-scanned JSON. The local workflow would upload
sanitized JSON for 14 days. A public-repository artifact, Actions cache,
ephemeral runner disk, WSL path, this checkout or unspecified workstation is not
a protected durable raw archive.

Credential locality and retention remain binding: Control OAuth stays in host
Control config; Pi/OpenCode secrets stay B-only; only sanitized summaries may
leave after strict allowlisting and exact-source recheck. Raw journals, requests,
logs, B checkout and A seed/base remain protected. Unknown stop, cleanup
mismatch, exporter failure, absent exact root-removal proof, or a stopped non-Goal
Run without an exactly delivered Goal candidate forbids deletion. Guest teardown
is not a containment StopReceipt.

Before dispatch, the operator must name a private destination and provide a
write-only run credential. It must be encrypted, access-controlled to recovery
operators, versioned/object-locked, keyed by repository/commit/run/attempt/case,
and governed by explicit retention and authorized disposition. Upload must
precede runner teardown and return a durable hash-bound acknowledgement; failure
must preserve journals and fail closed. Destination identifier, credential
delivery name, retention period and recovery owner are currently missing.

## Concrete dispositions and minimal operator facts

| Gap | Disposition | Next action |
|---|---|---|
| Exact source | Unavailable | Freeze, review and publish one exact SHA/tree. |
| Positive A/B host | Unavailable | Use protected disposable GitHub guest; require every preflight. |
| WSL positive | Disqualified | Never promote; use Ubuntu only for denial. |
| WSL denial script | Defective before fixture checks | Separately fix line 20 from `-d` to `-f`; then run negative witness on exact subject. |
| WSL fixture | Unconfigured independently of bug | Install/activate fixed owner and supply config, authentic Claim request and marker root. |
| Hyper-V VM | Inaccessible/unconfigured | Ignore unless operator supplies authorized VM and equivalent contract. |
| Rootless refusal | Engine unavailable | Run later on isolated CI; classify as refusal only. |
| Credentials/models | Unconfigured | Add the two Environment secrets and two variables above. |
| Authentic Control/Integration | Documented, not instantiated | Let protected run create ephemeral DB/Control/Integration and verify exact claim/receipt. |
| Sanitized export | Implemented locally, not remote | Publish exact workflow; require every manifest. |
| Durable raw archive | Unavailable | Supply destination, write credential, retention/access policy and recovery owner. |
| Unresolved journals | Retention mandatory | Preserve until exact physical stop and separately authorized disposition. |

Safe decision-free work is exhausted: source/script inspection, presence checks,
WSL and Docker metadata, GitHub metadata and virtualization capability queries
are complete. Installing units, starting Docker, creating a VM, changing
kernel/permissions, downloading tools, supplying paid credentials, dispatching
provider work, choosing archival policy, publishing source or deleting journals
all cross explicit operator boundaries. Repeating captured heavy tests would not
qualify the environment.

The minimal missing user facts are: (1) approval of the GitHub disposable-guest
route and real GitHub/OpenAI calls; (2) the two protected credential values and
two model IDs; (3) the exact approved commit; and (4) the raw archive destination,
credential delivery name,
retention period, authorized readers/recovery owner and unresolved-journal
disposition authority. Until those facts exist, the line-20 bug is fixed
separately, and all exact-subject witnesses pass, Goal 0006b stays active.

## Resumption addendum - 2026-10-01

The historical observations above remain unchanged. Two pending source defects
are now corrected in the working tree: the WSL prerequisite at
`scripts/ci/wsl2-v2-denial-witness.sh:20` and the disposable guest prerequisite
at `scripts/ci/linux-v2-native-acceptance.sh:771` use `-f`, not `-d`, for
`/sys/fs/cgroup/cgroup.controllers`. The host and alternate-mount checks already
used `-f`. No peer, ownership, claim, physical-stop or witness acceptance gate
was removed or changed.

Current script SHA-256 values:

| Path | SHA-256 |
| --- | --- |
| `scripts/ci/wsl2-v2-denial-witness.sh` | `af8ee1ea686e47b166431525aec27b7b978102f8241dfff3297a41f81b37d458` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `6fa540b9b66cd0aee8893ca01b50dcf54932230b020cce81e571f5659bbb7052` |

Executed checks:

- `wsl.exe -d Ubuntu-24.04 --exec bash -n /mnt/q/repos/symmetry/scripts/ci/wsl2-v2-denial-witness.sh`
  and the same command for `linux-v2-native-acceptance.sh`: both exit 0.
- `wsl.exe -d Ubuntu-24.04 --exec env SYMMETRY_PRODUCTION_LINUX_V2_WSL2_DENIAL_WITNESS=1 bash /mnt/q/repos/symmetry/scripts/ci/wsl2-v2-denial-witness.sh`:
  exit 1 with `the isolated A-owned symmetry-owner.service must already be active`.
  The real script passed the corrected systemd/cgroup prerequisite and rejected
  the unavailable installed owner. This is prerequisite smoke evidence only,
  not a successful authentic WSL denial witness.
- The current native-script guest predicate was parsed into its actual command
  arguments and executed on real Ubuntu WSL cgroupfs:
  `wsl.exe -d Ubuntu-24.04 --exec /usr/bin/test -f /sys/fs/cgroup/cgroup.controllers`,
  exit 0. No disposable native guest was provisioned or qualified by this check.
- Protected Environment metadata queries
  `gh api repos/wxxb789/symmetry/environments/linux-v2-native-acceptance/secrets --jq .total_count`
  and the equivalent `/variables` query both returned 0.
- `docker version --format 'client={{.Client.Version}} server={{if .Server}}{{.Server.Version}}{{else}}unavailable{{end}}'`:
  exit 1, client `29.8.1`, server unavailable because the Desktop Linux engine
  named pipe was absent. No Docker service or workload was started.
- Presence-only local checks found neither the four protected provider/model
  inputs nor the WSL config, authentic request and marker-root input variables.
  No credential value was read or exported.

Native independent `quick-consultant` review (Sol medium) found no defect in
either one-token correction and no unnecessary abstraction. It inspected the
downstream fail-closed guards; it did not execute checks or approve Goal 0006b.
The parent executed the boundary smoke separately.

These checks concern the original shared checkout and the WSL host, not the
separate installed guest or current acceptance candidate. Subsequent continuity
recovery found a clean `Q:/repos/symmetry-0006b-acceptance` at
`3f233f4d7394d5d0ffb21201612dc604595410c1`, tree
`2cd69626a502b5e9e70c76b020de4509c35b68ef`. Its newer deployment evidence locates
the earlier peer denial in nspawn's syscall filter, not a blanket WSL kernel
restriction; see candidate commit `b2ef6e3354fafa6cf77973a86ac1da5c3a7fd3e9`.
The original September 29 host interpretation is historical, not current truth.

The latest machine-local checkpoint is
`current-checkpoint-20261001-dedup-l2-current-seven-pins-parser-passed-same-review-pending-v35.json`
under the operator's protected evidence root. It supersedes the older handoff
for continuation and records native/runtime holds, user-deferred remote work,
and the pending approved Go-only dedup review/consumer. The protected root's
presence does not itself prove durable recovery archival. Installed-native,
authentic-provider, archive/readback, full-matrix and exact-source acceptance
have not been established by the fresh checks above. Those checks replace no
original acceptance item. Goal 0006b remains **ACTIVE / INCOMPLETE**. This
session performed no commit, push, workflow dispatch, credential provisioning
or service mutation.
