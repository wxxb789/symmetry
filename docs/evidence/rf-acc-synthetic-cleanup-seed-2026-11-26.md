# rf-acc-synthetic-cleanup-seed — evidence record

Contract: `goal-rf-acc-synthetic-cleanup-seed.md` (GDD state root, outside the repository).
Branch: `refactor/rf-acc-synthetic-cleanup-seed`. Dispatch base: PR #302, `8065b96e`. Round-one
fix base: PR #304, `6526dac9`, including main `07be175e`. Plan:
`docs/plans/2026-11-26-rf-acc-synthetic-cleanup-seed-plan.md`. Local verification is not the
credentialed acceptance. The conductor re-Binds the corrected PR head (§7).

## 1. Subject bytes (sha256)

| File | Before | Delivered |
| --- | --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `78b272754989625444dedd61f0ab7062ba4abdf2f59107533729135d7d71f8ea` at `6526dac9` | `aac479e5fb80bcd0cc222f1afe96a2e9b010081cc0d2903e84c940e8e61c516d` |
| `scripts/ci/test-linux-v2-evidence.py` | existing exporter and script tests | `d410828e7de0c5c90f8f7ef8f0559ce9a744ad12187840e0b64b2e822850075d` |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `1e7a5d3a05d444ccd1b73e52df943c19978eb7b4faf921ef2e49377a6d378dcb` at `8065b96e` | unchanged |
| `daemon/internal/clonebootstrap/filesystem_linux_amd64.go` | `86811678e6741923bfd64031a40e0c7e531e46adb0f6f585301900fe919442af` at `8065b96e` | unchanged |
| `daemon/internal/clonebootstrap/main_linux_amd64.go` | `3fdd0a28f79875c40073e861b2ee67b492d7526611a9a9d536fef1cef3695a70` at `8065b96e` | unchanged |

The dispatch fixture script (`70163ae16b80e81bec0aa3ee7e70299c3d739716a037f2d0ad4dd5a3ed57d143`)
matched the script that ran in #302's Bind (findings `302-binding-run.md` §2.1). Sections 2.1–2.4
refer to the historical diagnosis; historical script line references are not final delivery anchors.
No Go source changes in the PR relative to `07be175e`; the post-merge `daemon/` tree is
`22d5cc959d3b2980b23bd79e6947a31c19dbc728`, distinct from the pre-merge tree. The correction
changes the acceptance shell, its Python test, and four documents, not the workflow or case-set list.

## 2. Root cause (Stage 1)

### 2.1 The chain, end to end

The one-use RELEASE runs the fixed B clone bootstrap
(`daemon/internal/clonebootstrap/main_linux_amd64.go:124`): `openCloneSeed` opens the seed parent
chain with `openReadOnlyAPathDirectory`
(`daemon/internal/clonebootstrap/filesystem_linux_amd64.go:95`; `:309-345`), walking from `/` and
opening every component `O_RDONLY|O_DIRECTORY|O_CLOEXEC|O_NOFOLLOW` (`:326`). Opening a directory
`O_RDONLY` requires *read* on it (not just traverse), and B in the gate's user namespace is the
host B kuid with no supplementary groups (`daemon/native/linux-gate/c-gate/gate.c:1012`
`setresuid(1,1,1)`; `daemon/internal/platform/anchor_gate.go:528` pins the `1 <B_UID> 1` map with
`setgroups` denied).
The witness fixture publishes the case root `/opt/symmetry-owner-test/synthetic-goal-cleanup` as
`root:A_GID 0751` (`scripts/ci/linux-v2-native-acceptance.sh:589` at the dispatch head), so B is the
`other` class with `--x` only: the `openat(<case root>, O_RDONLY|O_DIRECTORY)` fails `EACCES`, and
every seed-stage step fails there. `openCloneSeed` wraps it as `open A-owned seed parent without
symlinks: %w` (`:95-97`); the bootstrap returns `fail(CloneFailureSeed, err)`
(`daemon/internal/clonebootstrap/main_linux_amd64.go:126`), its command main encodes that as a
clone-bootstrap failure frame (`daemon/cmd/symmetry-linux-clone-bootstrap/main.go:27`), and the
helper's checkout-receipt read (`daemon/native/linux-gate/protocol/wire_linux.go:600-613`) turns
it into the labelled detail
`stage=clone_checkout arm=checkout_receipt wait_class=other guest_stage=seed errno=13`
(`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:1108-1113`, `:761-790`, `:835-868`,
rendered by the release detail's `String()` (`daemon/internal/platform/helper_v2_protocol.go:486-505`)).
The journal rows the Bind recorded
(`has_clone_intent:true, has_seed_receipt:true`) are A-side receipts and are consistent: the seed
exists and is B-readable; only its chain is not.

### 2.2 Discrimination — the case root is the only failing component, and no other producer fits

- Every other component of the seed and clone-root chains is B-readable in the fixture: `/opt` and
  `/opt/symmetry-owner-test` are `0755` (`scripts/ci/linux-v2-native-acceptance.sh:588`), `seeds`
  carries the `u:B:r-x` ACL with defaults (`:596-597`), the binding/run directories inherit those
  defaults (created `Mkdirat(…, 0o750)` by
  `daemon/internal/workspace/linux_clone_fs_linux.go:576`), and the bundle itself is published
  B-readable by production (`daemon/internal/workspace/linux_clone_linux.go:2198`,
  `Fchmod(fd, 0o440)` "protect B-readable Linux clone seed"). The clone root `clones` is B-owned
  `0700` (`:595`, `:598-599`). The case root is the single
  B-unreadable component on both walks.
- Not a production pin or gate defect: the same `openCloneSeed`/`openAbsoluteDirectory` code is
  green on the #296 clone legs and on the production chain (§2.4), and the failing syscall needs
  only the fixture's access ACL to change.
- Not AppArmor: the installed per-program profile is exactly one `userns,` rule under
  `flags=(unconfined)` with no file rules and no globs
  (`daemon/internal/linuxpreflight/preflight_linux.go:224-229`), so it cannot produce a path
  `EACCES`; the Bind's seven AppArmor rows are green.
- Not the credential seed receipt or containment: `has_seed_receipt:true` and the A-side receipt
  checks passed (`daemon/internal/workspace/linux_clone_linux.go:142-160`), and the guest-side
  checks that remain are unchanged by this fix (§3).
- Not mount propagation: the walk uses the same namespace as the rest of the RELEASE, whose earlier
  stages (image pin, gate, seed creation) all passed at the same revision.

### 2.3 Reproduced on the resolute replica (the #296 S5 method)

`path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec` (Ubuntu 26.04 LTS userspace, the same userspace the fixture
debootstraps) with a scratch probe built
`CGO_ENABLED=0` from `git archive 8065b96e daemon`
(sha256 `18f38d94aa71f53229c2953f677e68a8deeb3ffd775326b004245b6b630c330a`) plus one scratch test
file (`3f723a8dad3903ac017ff887b21bb45f579704630add5c4b87e5bd983d03a904`) that calls the production
`openCloneSeed` + `verifyCloneSeed` and `openAbsoluteDirectory`; binary
`c09291124eb7b1694b7cffcc508e888380310de286d425dbb8e820ab046b39df`. Each shape is entered
`chroot path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec /usr/bin/setpriv --reuid=52001 --regid=52001
--clear-groups` (the guest's B kuid; no supplementary groups). Fixture shapes are built to the
script's own spec (`fixture-setup.sh`): red = `install -d -o root -g 52000 -m 0751 <case root>`;
green = red plus `setfacl -m "u:52001:r-x,m::r-x"` on the case root (the delivered fix's exact ACL
spec); baseline = the production shape (`0755 root:root` parents). Log
`path-sha256:2122c9399acfc230e8c3a2b6b0cb80e942aa29ddd4e60d325f2095b692c6dfb6` (sha256 `f56b9e653409430bcbc845c9b4f8fb8ad9d576f66e482a60eb38c20278850f82`),
verbatim rows:

```
FIXTURE stat path-sha256:f1318fde4f7a1ea3e06eba80d09db51da4925090fc9aa2fd83f47c8585fbf561 0:52000 751
FIXTURE acl caseroot user::rwx
FIXTURE acl caseroot group::r-x
FIXTURE acl caseroot other::--x
B_TEST red -x 0
B_TEST red -r 1
PROBE seed chain refused: errno=13 text="open A-owned seed parent without symlinks: permission denied"
PROBE clone root refused: errno=13 text="permission denied"
PROBE_MODE=red PROBE_RC=0
FIXTURE acl caseroot user:symmetry-b:r-x
B_TEST green -x 0
B_TEST green -r 0
PROBE seed chain accepted and verified: /opt/symmetry-owner-test/synthetic-goal-cleanup/seeds/binding-native/run-probe-run-000000000000/generation-1.bundle
PROBE clone root accepted: /opt/symmetry-owner-test/synthetic-goal-cleanup/clones mode=040750 uid=52001 gid=52001
PROBE_MODE=green PROBE_RC=0
B_TEST baseline -x 0
B_TEST baseline -r 0
PROBE seed chain accepted and verified: /opt/symmetry-owner-test/production/seeds/binding-native/run-probe-run-000000000000/generation-1.bundle
PROBE clone root accepted: /opt/symmetry-owner-test/production/clones mode=040750 uid=52001 gid=52001
PROBE_MODE=baseline PROBE_RC=0
```

The probe is written as an assertion of the observed capability, so each row's exit is 0 and the
observation text is the evidence: `red` asserts the refusal *is* `EACCES` (`errno=13`, matching the
recorded `errno=13`), `green` requires the full production `openCloneSeed` + `verifyCloneSeed`
(identity, digest, write-denied checks on every ancestor, path re-bind) and the production
`openAbsoluteDirectory` (`O_RDONLY` walk plus B-write check) to pass, and `baseline` shows the
unchanged production code accepts the production-shaped chain. In all three shapes the seed file
itself is `52000:52000 0440` with `user:symmetry-b:r-x #effective:r--`, `mask::r--`, so the
refusal is solely the case-root component.

### 2.4 The green comparison and why the fixture is the wrong side

The #296 clone legs (`daemon/native/linux-gate/protocol/clone_integration_linux_test.go:38`,
`TestInstalledLinuxOwnerCloneCheckoutScratch`) run the same `openCloneSeed` walk and are green in
every run: their seed root sits under `0755` parents with the seed-root ACL
(`scripts/ci/linux-v2-native-acceptance.sh:588`, `:875`) and the test publishes the bundle `0440`
next to an inherited `u:B` entry
(`daemon/native/linux-gate/protocol/clone_integration_linux_test.go:502-516`). The fixture's `0751`
case root is therefore not a hardening the guest supports: production's own publisher calls the
0440 seed "B-readable", so a B-unreadable ancestor contradicts the seed contract. The fix belongs
on the fixture side; a production change (e.g. walking ancestors `O_PATH`) would weaken the walk for
every deployment to accommodate one fixture mode.

## 3. Fixture correction

- `scripts/ci/linux-v2-native-acceptance.sh:590-594` grants B read on the case root only in
  `synthetic_goal_cleanup`. The shared installs and the child ACLs are unchanged. The source,
  config, and witness are still selected only by the synthetic set; other sets receive no new
  named case-root grant.
- `:1461-1471` uses Bash's ACL-aware builtin for B's case-root read, B's separate seed-root read,
  and A's case-root write denial. The case root must also match both `root:A_GID 0751` and the
  exact numeric ACL. The positive form of the denial check (`! test -w` inside the guest shell)
  fails closed if launching the check returns a nonzero status.
- A valid mode with an unrelated reader could otherwise pass all access rows. The exact ACL rejects
  that extra entry. The case-root and seed-root read rows require actual B access; their passing
  alone would not demonstrate how the read was granted, or prove that B can search the seed root.
- The measured refusals were removal of the case-root B access entry, removal of the seed-root B
  access entry, a widening that grants A **write** on the case root (`g::rwx`), chmod `0755`, and
  one extra named reader at the unchanged `0751` mode (§6). The config stays `root:A 0640` and
  B-unreadable, and the source stays A-private. Production seed receipt checks, ancestor
  write-denial checks, and RELEASE resolution are unchanged.

## 4. Before-state evidence

The credentialed #302 re-Bind (run `45803728792` on `5f744370`) reached RELEASE and observed
`guest_stage=seed errno=13`. The historical probe in §2.3 exercised the production paths before
and after granting B read. The separate #304 Bind below stopped earlier: the external ACL-blind
`/usr/bin/test` refused B on the correctly provisioned case root. Section 6 reproduces that setup
failure using the retained `6526dac9` guard, then proves the correction and individual refusals.

## 5. Post-merge local checks

All rows use `6526dac9` plus the two executable/test hashes in §1. The daemon input is exactly
`git archive 6526dac9 daemon` (sha256
`db4381493cfac6177ea2c21bac37d933a48caa32429f245e77bb9cbbd5e81472`). The full suites for
this head are CI's responsibility (the daemon job runs the full race suite) and the conductor's
acceptance record, not this local run. No local full-suite result is claimed.

| Platform and command | Exit | Observation |
| --- | ---: | --- |
| Host Bash and WSL Bash: `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 each | No syntax errors |
| WSL Python: `python3 -B scripts/ci/test-linux-v2-source-subject.py` and `test-linux-v2-go-events.py` (same invocation) | 0 each | 7 and 6 tests |
| WSL Python, same invocation: `test-linux-v2-evidence.py`, `test-linux-v2-owner-request-keys.py`, `test-linux-v2-host-prerequisite-probe.py` | 0 each | 57 (including W3), 10 and 17 tests |
| WSL Python, same invocation: `test-rootless-compose-owner-claim.py`, `test-ci-structure-extraction.py`, `test-ci-changed-path-classifier.py` | 0 each | 9, 72 and 37 tests |
| Windows, from `daemon/`: `go test -count=1 -v ./e2e -run '^TestRunJournalDiagnosticSnapshotRedactsExecutionCredentials$'` | 0 | 1 pass; log sha256 `5cda0bc3e78c88a8a165222858599dd3276f7824dce30b31224faf99546adddf` |
| WSL, from `daemon/`: `go test -count=1 -v ./e2e ./internal/clonebootstrap ./native/linux-gate/protocol -run "$focused"` (below) | 0 | 7 top-level passes. The two installed-owner rows compiled but skipped; log sha256 `4db934562551b3cbb2f051338157195dc46b481b5ec2e52632e9d4773adfb276` |

`focused='^Test(RunJournalDiagnosticSnapshotRedactsExecutionCredentials|LinuxV2CleanupApprovedWorkerProgram|ValidateBStatus.*|CloneBootstrapFailureStageNumbersArePinned|WireLinuxCloneBootstrapRefusals|ProductionLinuxV2CloneCleanupAuthenticatedScratch|ProductionLinuxV2PiNativeProviderAction)$'`

Earlier runs of syntax and Python fixture gates, advisory ShellCheck, format/vet, full Windows and
WSL Go suites, and committed doc/duplication/comment gates are **pre-merge characterization**.
The apply script `ce35bdf3…` was at `365828d3`, not implement `92e6d6d9` (`fb927785…`).
The pre-merge mode-pin delivery `483c947c` used `78b27275…`. Merging `07be175e` later changed
five non-doc blobs compared with `483c947c`: the native Go witness, the Control claim provisioner,
the machine registrar, the owner request writer, and its Python test. None of those old gate rows
binds the post-merge tree. Section 8 identifies the committed-blob gates for this delivery.

## 6. Resolute permissions and guard mutants

The probe (exit **0**, log sha256 `38ab8e22777931db6a0f79595b7ef8276bb0a878a87549f2c3b24ae1d072783a`)
ran the two delivered Bash blocks from §1 in a Resolute chroot on ext4. It only replaced
`machine_run_as` with `setpriv` at the pinned UID/GID and no supplementary groups, and executed
host fixture commands as the chroot's root. Those substitutions do not emulate permission results.
This is not a full nspawn, Control, or AppArmor reproduction. Inside the chroot,
`/usr/bin/grep '^VERSION_CODENAME=' /etc/os-release` returned `VERSION_CODENAME=resolute` and
`/usr/bin/dpkg-query -S /usr/bin/test` returned `coreutils-from-uutils: /usr/bin/test`, both exit 0.

The commands below omit only the private root paths: `resolute` is the chroot directory and
`work` is its private fixture parent. Use `root_for <label>` for each case root. Commands in the
next table ran via `c` (root) or `as` (the row's real credentials):

```bash
c() { /usr/sbin/chroot "$resolute" "$@"; }
as() {
  local id=$1; shift
  c /usr/bin/setpriv --reuid="$id" --regid="$id" --clear-groups "$@"
}
root_for() { printf '%s/%s/opt/symmetry-owner-test/synthetic-goal-cleanup' "$work" "$1"; }
```

In each row, `$p` is `root_for <label>`, or that path with `/seeds` where shown. Each shape's
provisioning completed with exit 0; each changed shape is a fresh fixture. The guard stderr is
prefixed by `Linux v2 native acceptance setup failed: ` for all failed rows.

| Shape and change (inside chroot) | Direct observation | Extracted guard |
| --- | --- | --- |
| `delivered`, original executable | `as 52001 /usr/bin/test -r "$p"` → 1 at the case root and 1 at `/seeds` | Original guard from `6526dac9` → 1: `native B cannot read the synthetic cleanup case root the clone seed path sits under` |
| `no-case-acl`: `c /usr/bin/setfacl -x u:52001 "$p"` → 0 | `as 52001 /usr/bin/bash -c 'test -r "$1"' bash "$p"` → 1; delivered → 0 | 1: same case-root read diagnostic |
| `no-seed-acl`: `c /usr/bin/setfacl -x u:52001 "$p/seeds"` → 0 | Same Bash command on `"$p/seeds"` → 1; delivered → 0 | 1: `native B cannot read the synthetic cleanup seed root` |
| `case-writable`: `c /usr/bin/setfacl -m g::rwx "$p"` → 0, granting A write | `as 52000 /usr/bin/bash -c '! test -w "$1"' bash "$p"` → 1; delivered → 0 | 1: `native A unexpectedly gained write access to the root-controlled synthetic cleanup case root` |
| `world-readable`: `c /usr/bin/chmod 0755 "$p"` → 0 | `c /usr/bin/stat -c '%u:%g:%a' "$p"` → 0, `0:52000:755`; delivered → 0, `0:52000:751` | 1: `synthetic cleanup case root must stay root:A mode 0751` |
| `extra-reader`: `c /usr/bin/setfacl -m u:52002:r-x,m::r-x "$p"` → 0 | The same `stat` still reports `0:52000:751` (0). `c /usr/bin/getfacl -cpn -- "$p"` → 0, ACL differs by `user:52002:r-x`; on delivered → 0 with no extra line | 1: `synthetic cleanup case root must have exactly the fixture B-read ACL` |
| `shared-old` → `shared`: original provisioning → delivered provisioning in non-synthetic set | `c /usr/bin/getfacl -cpn -- "$p"` → 0 both; original had `user:52001:r-x`, delivered had only `user::rwx`, `group::r-x`, and `other::--x`. B's Bash case-root read → 0 originally, 1 delivered | Delivered guard → 0 (not entered) |
| `delivered`, corrected guard | Case root ACL: `user::rwx`, `user:52001:r-x`, `group::r-x`, `mask::r-x`, `other::--x` | 0 with empty stderr |

**Actual case-root open (W1)** — command, with the same private-root aliases as above:

```bash
as 52001 /usr/bin/perl -MFcntl=:DEFAULT,O_DIRECTORY,O_NOFOLLOW \
  -e 'sysopen(my $fd, $ARGV[0], O_RDONLY|O_DIRECTORY|O_NOFOLLOW) or do { my $n=0+$!; print STDERR "open errno=$n: $!\n"; exit $n; };' \
  "$p"
```

The open exited **13** with `open errno=13: Permission denied` on `root_for no-case-acl` and
**0** with empty stderr on `root_for delivered`. This isolates the access-relevant flags of the
case-root open, not the full walk (the production walk also sets close-on-exec). W7's unchanged-mode
pair is `getfacl -cpn` on `extra-reader` versus `delivered`: getter 0 on both, exact-ACL guard
1 vs. 0. Both proof pairs use the Resolute chroot and the script hash in §1.

**Committed test.** `SyntheticCleanupGuardTest` in `scripts/ci/test-linux-v2-evidence.py` executes
the actual provisioning and guard blocks. It supplies command results to isolate Bash control flow,
then asserts the inner exit, complete diagnostic, and whether the grant ran. The same test evaluated
against the old script failed (5 assertion failures); unwrapping or inverting either case selector
failed as well. Removing each of the five permission checks, or replacing its `|| fail` with
`&& fail`, also failed. All 14 individual source mutations ran in memory with unittest verdict 1;
restoring the delivered bytes gave unittest verdict 0. The mutation driver exited 0 after asserting
all these expected failures; that outer exit is not the status of an individual guard.

## 7. Acceptance still pending

The conductor's re-Bind must reach RELEASE and show that the exact `:506 … guest_stage=seed errno=13`
stop is gone. A later stop can still use the same source line; record the next failing line verbatim
in the seed residual section of `docs/TODO.md`. If the set is green, confirm export of
`synthetic-goal-cleanup.json` and the manifest. The local probe cannot establish either result.

## 8. Committed-blob gates

The scratch clone holds `6526dac9` plus the six delivered paths; these checks compare its committed
blobs with `07be175e`. Use the receipt in the PR body for the resulting scratch commit and exit
codes (the checks read this record itself). No gate in the earlier pre-merge runs (§5), or in the
separate failed Bind below, substitutes for these checks.

- `python -B scripts/ci/jscpd-changed.py --repo "$scratch_clone" --base 07be175e --head HEAD`
- `python -B scripts/ci/check-record-cleanliness.py --repo "$scratch_clone" --base 07be175e --head HEAD`
- `python -B "$comment_cap" "$scratch_clone" 07be175e`

## Bind — FAILED before the Go witness on PR head `6526dac9`

[Run 45836765410](https://github.com/wxxb789/symmetry/actions/runs/45836765410), attempt 1,
ran only `synthetic_goal_cleanup` on `db89dc97f0d9109a7c15e14bc580a29df77cbb27`, the PR #304
head `6526dac9` plus the scratch workflow and shim. The run failed during fixture setup. One push
created one run (`total_count=1` by run head before and after branch deletion); the temporary
worktree was removed and the PR head did not change.

First failing line (raw log 2423, 14:16:40.9209399Z):

```text
Linux v2 native acceptance setup failed: native B cannot read the synthetic cleanup case root the clone seed path sits under
```

In all 2,563 log lines, `production_linux_v2_cleanup_witness_test.go:506`, `guest_stage=seed errno=13`,
`production_linux_v2_cleanup_witness_test.go:499`, and `stage=none` each occur **0** times. The
witness was not reached, so absence of the old frames does not establish that RELEASE now passes
the seed stage. The class pin was silent; B's case-root read assertion failed before the seed-root
read and A-write assertions ran. No AppArmor or cleanup test verification row was emitted.
The conductor re-Binds after this round; this setup abort is not acceptance.
`synthetic-goal-cleanup.json` did not export, and no manifest passed or was uploaded (log 2424,
2481). The acceptance-side artifact was the **host prerequisite probe**, not cleanup evidence:
`linux-v2-host-prerequisite-45836765410-1` (id `15127111028`), ZIP sha256
`0a6c1ba56155cb431d833102e10491043f9713d0ff93fa27665132ede622d307` (matched the API digest).

### Run-input equality

All 1,900 tracked PR-head entries matched the run head by path, mode, type, and Git object ID.
The table gives the case-set inputs whose sha256 changed since the
[PR #302 Bind 3](rf-acc-synthetic-cleanup-release-2026-11-26.md#bind-3); the other measured
PR-tracked rows, including the cleanup witness and exporter, kept their previous hashes.

| Input | Run head sha256 = PR head sha256 |
| --- | --- |
| `scripts/ci/linux-v2-native-acceptance.sh` | `78b272754989625444dedd61f0ab7062ba4abdf2f59107533729135d7d71f8ea` |
| `scripts/ci/provision-linux-v2-control-claims.exs` | `82668512928ade9ab7d52199d8edd9f62af3bce4e4c4b547f9365c8a5ad0a5e3` |
| `scripts/ci/register-linux-v2-claim-machine.py` | `f6029087c0acf8c42478018f0f9672fdf7a914a715d4e49d0eb3c3ee128ad773` |
| `scripts/ci/write-linux-v2-owner-request.py` | `a6d007374c7c4d64a96b0cc9a7fefc73d4120d4dba31f6ce796a30f5730d677a` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `d988ca3bf8a0364f7d2f1c9aabe4cef14b53216e501a6002c49950e18d508abf` |

The two additions are absent at the PR head. Their hashes match PR #302 Bind 3: workflow
`8beeadf6d00d73dac74a10a49722c9b767dab20010875938c8af5725e8fb389f` (only the plain `case_sets`
delta from `b0ba2694`) and shim `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`.
