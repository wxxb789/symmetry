# rf-acc-apparmor-userns-profile — evidence record

Goal `rf-acc-apparmor-userns-profile` (contract: the goal file under the GDD contracts root; owner
ruling 2026-11-25 = J1). Branch `refactor/rf-acc-apparmor-userns-profile`, base
`61399984` (main, PR #289 merged). Plan: `docs/plans/2026-11-25-rf-acc-apparmor-userns-profile-plan.md`.
Scratch branch: `scratch/rf-acc-apparmor-userns-profile` (throwaway workflow + probe only; deleted
at the end and its absence proven with `git ls-remote`). All paths in this record are relative to
the repository or to the GDD state root.

## 1. Delivered files and digests

| file | sha256 at the delivered bytes |
| --- | --- |
| `scripts/install-linux-owner.sh` | `ab0780fc94468991f03844f1d3cbdb67fe60a84e6d9aaed5261de3f3af580b4b` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `e85c1c321e140874f6f211e2e73d024a749d9972c27e4aadff7ded559f17b9be` |
| `scripts/ci/linux-v2-native-rootfs-base.sh` | `c8e4972490151e03e6b6c9ea3fa5393a8c348ebf0c9912e1d16770842f7c5866` |
| `daemon/internal/linuxpreflight/preflight_linux.go` | `da5b2caf3bfaa1fa4ef340e755cc9f1450346177ac07ce3d4dc203ce1f65f205` |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `29dcfdb2c8c73eea6cccc0d14701dd5289bff6a9b7faac1a217bd8ecb4f69c03` |
| `daemon/internal/linuxpreflight/preflight_unsupported.go` | `b7aaf81f43a53f39f6268c39f5b1be03753dd84783878f13387631962f095501` |
| `daemon/cmd/symmetry-daemon/main.go` | `ad8547f13c0c080eb674f08799ca881be142cb549e292c2fa2b685176af73890` |
| `docs/plans/2026-11-25-rf-acc-apparmor-userns-profile-plan.md` | `8fe8ef416dfcfdff77a3d899bc4de004cd041133e88032613939ff4a5f8ddbcb` |
| `docs/runbooks/control-plane-and-daemon.md` | `06b6ae8762734b53c93f3c2dc638797c2741d48bc380fd5dccdbd18dc59dfa8d` |

Run 3's head `ce3c30b9` (the amendment plus the round-2 hardening commits) is bound to the
run-3 code revisions by the §8 equality table; the apply-stage simplification (2026-11-26) then
changed six of the files above, so the table carries the delivered post-apply revisions and
§8's delta table records each change, its class and its re-run gates. The plan digest above is
its final record revision (the plan and the record files carry post-run, record-only edits and
are not run-bound code), and `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this record are
deliberately not hashed anywhere.

## 2. Readback rule (amended after run 2; §2 of the plan carries the source evidence)

The installer and the preflight use the same rule, both fail closed, and both never write the
sysctl:

1. `kernel.apparmor_restrict_unprivileged_userns` absent/`0`: nothing installed, nothing loaded;
   a published profile file or a loaded profile with the fixed name is refused
   (`unexpected_profile` / the installer's refusal naming `-remove-userns-profile`); an
   enumeration failure is a refusal, not an empty result.
2. `1`: the published file must be root:root 0644, singly linked, in a root-owned non-writable
   directory without an untrusted ACL writer, and byte-equal to the fixed text
   (`profile_file_mismatch`/`profile_absent`).
3. Exactly one loaded profile must carry the fixed name, in mode `unconfined`
   (`profile_not_loaded`, `profile_loaded_mismatch` for duplicates/foreign mode).
4. The kernel's recorded `policy/profiles/<name>.<id>/raw_sha256` must equal the digest of that
   text compiled with compression disabled (`apparmor_parser -c0 -Q -S -K -T --skip-bad-cache`,
   whole output or output from byte 9; a parser that rejects or empties `-c0` — parser < 5 —
   falls back to the plain compile, whose output is already uncompressed). Otherwise
   `profile_loaded_mismatch`. The kernel hashes the decompressed policy as received
   (`security/apparmor/policy_unpack.c` `aa_unpack` → `aa_calc_hash(udata->data, udata->size)`;
   `apparmorfs.c` `rawdata_open` decompresses the stored copy back to those bytes), which is why
   the first draft's digest of the default (compressed) `-S` output can never match.
5. `attach` is not read: `seq_profile_attach_show` prints `<unknown>` for the parser's DFA
   attachments on every tested parser (measurements in §8, run 2 rows), so it carries no
   attachment evidence; the first draft's `attach == exact path` shortcut was removed (it could
   never match on the runner and skipped the rule-set evidence — security review F2).
6. Installer ordering: stage the fixed text in `/etc/apparmor.d`, load that staged file with
   `--replace`, read the kernel state above, and only then publish it atomically; a failed load
   or readback unloads what the run loaded, removes the staged and compiled temporaries, and —
   when a verified published file already existed — reloads that text so the host is left as
   found. `-remove-userns-profile` unloads through the definition file that declares the
   profile (`apparmor_parser -R <file>`; a bare name is not a file and the parser skips it),
   proves the load is gone and removes the published file.

## 3. Profile text, attachment set and load command

```
abi <abi/4.0>,
include <tunables/global>
profile symmetry-linux-owner-userns /usr/libexec/symmetry/symmetry-linux-gate flags=(unconfined) {
  userns,
}
```

Five lines, one stanza, one rule, one exact attachment (measured set = census C2: `gate.c` is the
only user-namespace creator). Emitted by `install-linux-owner.sh -print-userns-profile`; the
preflight's byte-equal text is pinned by `TestLinuxAppArmorProfileTextMatchesTheInstaller`
(positive row: exact stanza/boilerplate/one `userns,`; negative rows: glob attachment, extra
flag/rule, second stanza). Load command: the resolver-pinned absolute `apparmor_parser --replace
<staged file>` as root; nothing else loads policy. Removal: `-remove-userns-profile` (root-only,
unload through a definition file, prove absence, remove the file, then report the intended
fail-closed state).

## 4. Census at the delivered bytes

| id | command | count | disposition |
| --- | --- | --- | --- |
| C1 | `grep -rln "flags=(unconfined)" scripts/` | 3 | the two cited CI exceptions plus the installer; the fixture stays absent |
| C2 | `grep -rn "unshare(CLONE_NEWUSER)" daemon --include=*.c --include=*.go` | 1 | `gate.c:1414` — the attachment target |
| C3 | `grep -rn "CLONE_NEWUSER" daemon --include=*.c --include=*.go` | 3 | the two named non-creation sites unchanged |
| C4 | `grep -rn "apparmor_restrict_unprivileged_userns" .` (minus `.git`, `.omo`, `node_modules`) | 16 hits in 11 files | readers: the installer, the preflight, `linux-v2-host-peer-probe.go`, `linux-v2-host-prerequisite-probe.sh`, `prepare-private-namespace-tests.sh`; the other six are records (`docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, the goal plan, this record, `linux-v2-host-probe-fix-2026-11-22.md`, the runbook); **no writer** (`sysctl -w`, `> /proc/sys` absent) |
| C5 | `grep -rln "install-linux-owner" scripts daemon .github` | 4 | the installer, the fixture, and the preflight/test references (comment + exec); no other caller |
| C6 | `grep -c "apparmor" scripts/ci/linux-v2-native-acceptance.sh` | 0 | the fixture authors no policy text (also 0 case-insensitively) |
| C7 | `grep -rn -- "-verify-linux-owner-install" scripts daemon` | 18 in 8 files | every entry point known; the check lands on the existing path only |

## 5. Fixture rows (Change 3)

- `scripts/ci/linux-v2-native-acceptance.sh:967`: `--capability=CAP_MAC_ADMIN` — the container
  manager's default retained set plus exactly the load's missing capability
  (`nspawn.c` `arg_caps_retain`: the default already carries `CAP_SYS_ADMIN`, omits
  `CAP_MAC_ADMIN`; `--capability=` adds). The first draft's `--capability=all` was a round-1
  finding (P2-6/F4) and is not shipped.
- `:1644-1649`: the securityfs guard uses the repository's own
  `findmnt --noheadings --output TARGET --target` pattern and then `mount -t securityfs` with a
  named failure; run 2 showed the first draft's `findmnt --quiet` is not a findmnt option, which
  made the guard dead and the mount unconditional (EBUSY on an already mounted guest).
- `scripts/ci/linux-v2-native-rootfs-base.sh:70-72`: the pinned package set gains `apparmor`
  (the in-guest parser the load and the readback need) and masks the guest's own policy service
  so its boot does not reload the distro profile set into the shared kernel policy.
- The installer's in-guest run and the in-guest `-verify-linux-owner-install` run remain the
  acceptance oracles; the fixture authors no policy text, names no parser, file or mode, and
  never writes the sysctl.

## 6. Local rows (WSL Ubuntu-24.04, no AppArmor kernel support)

The installer step's decision, failure, rollback and publication paths are driven by a stub
parser in a scratch layout (substitutions recorded: `check_root_directory /etc` → a scratch
`etc`; `/etc/apparmor.d` → a scratch directory; `USERSNS_SYSCTL`, `USERSNS_PROFILE_FILE`,
`USERSNS_ATTACH`, `USERSNS_PROFILES_ROOT`, `ROOT_STAGE` overridden; `apparmor_parser` replaced by
a stub that records every invocation and materializes the loaded-profile directory in its success
modes). Run as root; 42 rows with 27 PASS assertions and 0 FAIL (re-run at the delivered bytes):

- absent sysctl and sysctl `0`: `apparmor_userns=not_needed`, nothing published;
- `0` with a published file or a loaded profile: refused, naming `-remove-userns-profile`;
- load failure: refused at the load stage, no published file, no staged text, the unload
  attempted;
- a loaded profile whose recorded hash is another policy: refused at the readback (`differs from
  the staged userns profile text`) and rolled back (unload attempted, nothing published);
- successful load: exactly the fixed text published, `apparmor_userns=per_program`, only the
  published file left in the policy directory, the readback compiled the staged text;
- failed re-run over a verified published file: the published file survives and the rollback
  reloads it (the stub records the second `--replace`).
- a parser that rejects `-c0` with an empty output and exit 0 (the parser < 5 shape): the
  readback falls back to the plain compilation and still prints `apparmor_userns=per_program`
  (rows 32-35, with both parser invocations recorded).
- the success path issues no unload (the run's own load stays, and the exit trap's unload flag is
  cleared before the publication).
- the unload sites pass a definition file, never the bare profile name (round-2 Sol P1-1:
  `apparmor_parser -R <name>` is silently skipped because a name is not a file).

Go host-state rows (`TestLinuxAppArmorHostStateRows`): the sysctl reader against real files
(absent/`1`/`0`/invalid/unreadable), the profile-file verifier against a foreign-owned temp path
(absent, world-writable, symlink, foreign text; the root-owned positive rows are gated on
`Geteuid()==0` and observed as root), and one real `apparmor_parser` compilation of the profile
text. `TestLinuxAppArmorLoadedProfileScan`: fixed name, another profile, stray file, duplicate
names. `TestLinuxAppArmorUsernsDecisionRows`: 21 rows over the closed vocabulary.

## 7. Mutation rows (scratch copies and in-place runs, all restored byte-identically)

| id | mutation | observed red (verbatim row) |
| --- | --- | --- |
| M1 | attachment spec replaced by a glob | `TestLinuxAppArmorProfileTextMatchesTheInstaller` FAIL: `installer profile text differs from the preflight text`; mutant digest `3a9aeb0e…`, restore digest `605dd0ec…` verified |
| M2 | a second rule added (`capability,`) | same text row FAIL; mutant digest `8eafabea…`, restore verified |
| M3 | install on a sysctl `0` host | local probe `row 2 FAIL want: apparmor_userns=not_needed got: apparmor_userns=per_program` |
| M4 | publish the file but ignore a load failure | local probe `row 15 FAIL want: apparmor_parser could not load the installed userns profile got: install-linux-owner: the installed AppArmor userns profile is not loaded` (the failure moves to the readback) |
| M5 | skip the rollback's unload and staged-text removal (the unload call in `rollback_userns_profile`) | local probe `row 17 FAIL the rollback did not unload` |
| M6 | succeed when the sysctl is `1` and no profile is loaded | decision row FAIL: `restriction-one-without-load: decision = "profile_loaded_mismatch", want "profile_not_loaded"` (mutant digest `052eca9d…`) |
| M7 | accept any recorded hash (substituted for the removed attachment shortcut; substitution recorded) | decision row FAIL: `restriction-one-with-different-policy: decision = "per_program", want "profile_loaded_mismatch"` (mutant digest `d97c2b2a…`) |
| M8 | require the profile when the sysctl is `0` | decision rows FAIL: `restriction-absent-nothing-installed` and `restriction-zero-nothing-installed`: `decision = "profile_absent", want "not_needed"` |
| M9, M11 | gate self-check mutants | N/A: `gate.c` is byte-identical by the owner ruling (Q-C; no self-check mode exists) |
| M10 | the fixture authors its own exception | `delivered_fixture_apparmor_hits=0 mutant_fixture_apparmor_hits=1` |

M1/M2 mutate the delivered installer in place for one Go test run and restore it (verified by
sha256); M3-M5 mutate the probe's scratch copy of the installer; M6-M8 mutate a scratch copy of
the whole daemon module under `/tmp`; M10 uses a scratch copy of the fixture. Every scratch copy
and in-place mutation was restored, and the restores are asserted (M1/M2 restore digest, M6-M8
`copy_restored=yes`).

## 8. Runs (foreground, watched; artifacts by id and sha256)

| run | head | sets | outcome | first failing check (verbatim) |
| --- | --- | --- | --- | --- |
| 1 `45644664168` | `7db44f4e` (base bytes) | migration, shared | both exit 1 — the base red the goal exists for | `migration`: `release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other guest_stage=gate_drop_bounding_set errno=1`; `shared`: `clone_integration_linux_test.go:240 … Linux B clone bootstrap failed at stage 13 (errno=1)` and `launcher_integration_linux_test.go:439 … native exec failed after RELEASE: errno=1` |
| 2 `45651150146` | `af5804b9` (round-1 fix) | migration, shared | both exit 1 — fail-closed refusal at the readback; the probe measured the amendment's evidence | both sets: `install-linux-owner: the loaded AppArmor userns profile differs from the installed userns profile text` (log `gdd/native/run-45651150146.log`) |
| 3 `45659523339` | `ce3c30b9` (the amended, review-hardened bytes) | migration, shared | both exit 1 — the installer and the preflight passed end-to-end (`apparmor_userns=per_program`) and the wall's named rows are cleared; the next stops are named in the last column | `migration`: `production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness command did not reach its requested durable stage`, with the owner-witness command exiting 1 on `-linux-v2-native-owner-witness: wait for authentic Pi owner witness turn` (run 1's transport-start row `start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: ... guest_stage=gate_drop_bounding_set errno=1` is cleared; the RunJournal grew 5988 → 6559 bytes with the same projection flags); `shared`: `clone_integration_linux_test.go:252: same-PID clone-native process is not exact B: open /proc/7171/environ: permission denied` and `launcher_integration_linux_test.go:451: native process did not retain the exact B-only boundary: open /proc/7197/environ: permission denied` (run 1's :240/:439 rows are cleared) |

Artifacts (id, name, sha256 of each file that is goal evidence; `portal-assets` is the build
artifact and carries no goal evidence):

| run | artifact id | name | file sha256 |
| --- | --- | --- | --- |
| 1 | 15036338998 | `linux-v2-host-prerequisite-45644664168-1` | `a922cf0aa86afb1aa5886a8f7415fc2cebcebb270c2952364b1342eaf2c18c34` |
| 1 | 15037498105 | `linux-v2-native-evidence-45644664168-1` | `migration/manifest.json` `c2616c5462d0eec00499e5ab55603e18489d0dc2aac21d992a2ac8c454bf15d1`; `migration/migration-denial.json` `fb5fbe466270111b829c50bb2f4346f04006df453c4ecf6092e12afbd75a7c44`; `shared/unprovisioned-owner-refusal.json` `943dd4dfe597034d69b6d04be6c0b22e362005142fb761870aad382b8580aa14` |
| 2 | 15040596346 | `linux-v2-host-prerequisite-45651150146-1` | `2b72665e124ac12b3706f1a767598f5603b5c1a46d482b18132733c4a54d9eed` |
| 2 | 15041106371 | `linux-v2-native-evidence-45651150146-1` | `shared/unprovisioned-owner-refusal.json` `77a91886acfe136a63be05ad6c6ce664cad4ded7df3ef61c46c8ec72bb73131a` |
| 3 | 15043414262 | `linux-v2-host-prerequisite-45659523339-1` | `linux-v2-host-prerequisite-45659523339-1.json` `3540a6fa57c85104ef8fe0611c850a09b2ab88eae5c06219c095e27abc1f921e`; upload digest `7b85d553bfe3e3d501e13af5c88942ba43610f87c1cebf7a05d7cd5a90bc004f` |
| 3 | 15045665320 | `linux-v2-native-evidence-45659523339-1` | `migration/manifest.json` `52957a3019e6503d75e9697e1db52334449f5692b86bf60273e1da330d5145b9`; `migration/migration-denial.json` `74f585d5ad61ee6d6527443e3a82fb381cadde22fb722efffc9b07ad9d0f3547`; `shared/unprovisioned-owner-refusal.json` `78c63cdc5494863c67ab7ff87753b1c0ce9f312642078c9b6095dad29aca5727`; upload digest `2807f1824e32e4f5e93b0eebe14213a318c7a7d22201a930263391e8b5b532a6` |

Run 2 probe rows (log `gdd/native/run-45651150146.log`, step "Scratch-only AppArmor userns
readback and nspawn attribution probe"): host `raw_sha256=c9f1a3…` vs `-S` whole `b60bd1…` /
payload `a27c99…` (588 bytes); guest ingest `raw_sha256=fdd39e…` vs whole `4b7a71…` /
payload `579348…` (427 bytes); `attach_file=<unknown>` in both; `raw_data` unreadable as the
runner and 3186 bytes as guest root; `rerun_byte_identical=yes`; the guest baseline reproduces
the wall (`probe label post_unshare: unprivileged_userns (enforce)`,
`probe capbset_drop: errno=1`) while a profile attached to the probe's path passes
(`probe capbset_drop: ok`). Run 3's probe adds the amendment's positive rows: `-c0` whole/byte-9
digests against the kernel's recorded hash for a profile loaded the production way,
`sha256(raw_data)` coherence, compile path-independence, and the in-guest load under the fixture's
narrowed capability set.

Run 3 probe rows (log `gdd/native/run-45659523339.log`, same scratch step; host kernel
`7.0.0-1020-azure`, sysctl `1`, `AppArmor parser version 5.0.2`): the baseline red mechanism
replicated (`probe label post_unshare: unprivileged_userns (enforce)`, `probe capbset_drop:
errno=1`, exit=4); the amendment's decisive row for a profile loaded the production way
(`--replace`) — kernel `raw_sha256=fceb72acfc73c779740b3fcc3425666c09cbc5dbb9c5d04d8bdf82bcf2c4c237`,
`attach_file=<unknown>`, `raw_data_readable_bytes=0`, and `blob host-c0: size=3906
whole=fceb72ac… payload=37a7b156b0708743077f725625706942b9b73756b386c9d829d652f1b98673c3
equals_kernel_whole=yes equals_kernel_payload=no`. The probe then aborted inside its own scratch
helper (`blob.sh:10: $2: unbound variable`, swallowed by the step's `|| true`), so its guest
rows, the attachment-sensitivity row and the in-guest load row were not captured — residual, §12.

### Run 3's head vs the delivered files

`git diff ce3c30b9 -- <path>` is empty for each file and both digests are equal (the worktree
stayed uncommitted by contract):

| file | sha256 (worktree = `ce3c30b9`) |
| --- | --- |
| `scripts/install-linux-owner.sh` | `605dd0ec6a1e44aa42389641c6c599ddacb705da94cadcc8a3e5b3521d94a975` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `cd71800daa842c05b6fec78be8856f64b4880cfc5001e6517095dde0f484c166` |
| `scripts/ci/linux-v2-native-rootfs-base.sh` | `c8e4972490151e03e6b6c9ea3fa5393a8c348ebf0c9912e1d16770842f7c5866` |
| `daemon/internal/linuxpreflight/preflight_linux.go` | `50d800f0d10f7830719ca68ecad905bef4f4e3feee5f3e082dd9aed9c1130d83` |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `93c2c2191d6e53dfa90068f9baba8a0c9c0b48a18646ac80a22fde39c4180c9e` |
| `daemon/internal/linuxpreflight/preflight_unsupported.go` | `b7aaf81f43a53f39f6268c39f5b1be03753dd84783878f13387631962f095501` |
| `daemon/cmd/symmetry-daemon/main.go` | `ad8547f13c0c080eb674f08799ca881be142cb549e292c2fa2b685176af73890` |
| `docs/runbooks/control-plane-and-daemon.md` | `9930b98bdbb4a4b6c88dc5b08c933722fc656f1ba5032bab26db9b8088c3b532` |

The scratch branch is deleted and its absence proven: `git ls-remote origin
scratch/rf-acc-apparmor-userns-profile` prints nothing. `daemon/native/linux-gate/**` is
byte-identical to `61399984` (`git diff 61399984 -- daemon/native/linux-gate/` is empty — no
`gate.c` byte, no protocol byte). The plan and the record files carry post-run, record-only
edits (the plan: §1 anchors re-measured at the delivered bytes plus the run-3 outcome paragraph;
this record: these run-3 rows and the apply-stage delta table below), which is why the plan's
digest in §1 is its record revision rather than a run-bound byte.

### Apply-stage deltas (post-run-3)

The simplify review over `4440c5c0..9bf6ddc6` produced findings S1-S7 (under the GDD state
root); the apply stage accepted and applied all seven. No accepted decision, profile-text byte,
fence, credential readback or census count changes: `-print-userns-profile` still emits the
fixed five lines, C1-C7 are identical (§4), and the gates are in §10.

| file | run-3 sha256 | delivered sha256 (§1) | delta |
| --- | --- | --- | --- |
| `scripts/install-linux-owner.sh` | `605dd0ec6a1e44aa42389641c6c599ddacb705da94cadcc8a3e5b3521d94a975` | `ab0780fc94468991f03844f1d3cbdb67fe60a84e6d9aaed5261de3f3af580b4b` | S2: the removal path resolves the parser through `resolve_apparmor_parser` (optional-missing mode) and now carries its absolute-path and root-directory checks; S6: the readback strips the whole sysctl file's whitespace and requires exactly `0`/`1`; S3: two comment blocks compressed — behavior-strengthening, 18/18 local probe rows re-pinned |
| `scripts/ci/linux-v2-native-acceptance.sh` | `cd71800daa842c05b6fec78be8856f64b4880cfc5001e6517095dde0f484c166` | `e85c1c321e140874f6f211e2e73d024a749d9972c27e4aadff7ded559f17b9be` | S3: two comment blocks compressed — non-behavioral; the fixture still authors no policy text (C6 = 0) |
| `daemon/internal/linuxpreflight/preflight_linux.go` | `50d800f0d10f7830719ca68ecad905bef4f4e3feee5f3e082dd9aed9c1130d83` | `da5b2caf3bfaa1fa4ef340e755cc9f1450346177ac07ce3d4dc203ce1f65f205` | S1: the write-only `linuxAppArmorLoadedProfile.directory` field removed; S3: five comment blocks compressed — non-behavioral, `verifyLinuxOwnerAppArmorUserns` now :441-470 |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `93c2c2191d6e53dfa90068f9baba8a0c9c0b48a18646ac80a22fde39c4180c9e` | `29dcfdb2c8c73eea6cccc0d14701dd5289bff6a9b7faac1a217bd8ecb4f69c03` | S4: a literal stanza pin added to `TestLinuxAppArmorProfileTextMatchesTheInstaller`; S3: one comment block compressed — coverage-only, root WSL run 5/5 `LinuxAppArmor*` PASS |
| `docs/runbooks/control-plane-and-daemon.md` | `9930b98bdbb4a4b6c88dc5b08c933722fc656f1ba5032bab26db9b8088c3b532` | `06b6ae8762734b53c93f3c2dc638797c2741d48bc380fd5dccdbd18dc59dfa8d` | S5: removal is documented as unloading through its definition file, matching `unload_userns_profile` — documentation-only |
| `docs/plans/2026-11-25-rf-acc-apparmor-userns-profile-plan.md` | `b9b524ff3bacea567aea6930a0c2e03117e39649c62ba17e201b4b66c685782e` | `8fe8ef416dfcfdff77a3d899bc4de004cd041133e88032613939ff4a5f8ddbcb` | S7: census corrections (C5 = 4, censuses cited to evidence §4) plus the §1 anchors re-measured — record-only |

## 9. Preflight rows on the runner

In-guest `-verify-linux-owner-install` (the fixture's second oracle) prints
`apparmor_userns=<token>` beside `LinuxOwnerPreflightRuntimeBoundary`; the run-3 log row is the
end-to-end evidence for the decision line. Refusal rows and their tokens are the 21 decision
rows (§6) plus the installer's refusal rows (§7). Run 3, both sets, from the in-guest install and
the installed daemon's own ExecStartPre verify (`daemon_verify_log`, fixture :1674-1681):
`Installing production owner artifacts inside the disposable systemd guest.` →
`apparmor_userns=per_program` → `legacy Linux owners are drained; pending Goal deliveries may
remain` → `Verified fixed Linux owner assets for A=symmetry-a(52000:52000)
B=symmetry-b(52001:52001).` → the units are left stopped with the drain proof durable; the
daemon's `-verify-linux-owner-install` line (`read-only preflight does not launch B, certify a
future cgroup leaf, or attest a future per-run Git metadata ACL`) is followed by
`apparmor_userns=per_program` and the daemon.service smoke starts. The per-config probe rows in
the failure context likewise print `apparmor_userns=per_program` for every witness config, and
the bounded refusal example is `-verify-linux-owner-install: configured delegated parent identity
differs from the live owner anchor's exact manager subgroup` for
`negative-root/alternate_mount/config.json` (a host-identity refusal, unrelated to the userns
decision).

## 10. Gates (commands, counts, exit codes)

Re-run at the delivered bytes (delivered-file digests in §1; the run-3 binding is §8):

- `bash -n` on the two changed bash scripts, `dash -n` and `sh -n` on the POSIX installer:
  exit 0 each.
- `shellcheck --severity=warning` base-vs-delivered finding-set equality: installer 2/2,
  fixture 16/16, base-rootfs 0/0 (no new findings; sets compared with `diff` after masking
  paths/line numbers; the delivered run's shellcheck exit is 1 from the pre-existing findings
  alone, identical to the base set).
- `gofmt -l .` in `daemon/`: empty. `GOOS=linux go vet ./internal/linuxpreflight/
  ./cmd/symmetry-daemon/`: exit 0. `GOOS=windows go vet` (same packages): exit 0.
- WSL Ubuntu-24.04 `-u accept`: `go test -count=1 ./internal/linuxpreflight/
  ./cmd/symmetry-daemon/`: `ok` both (the root-gated host-state and compile-order rows skip
  there); the same host as root runs every `LinuxAppArmor*` test and they pass, including
  `TestLinuxAppArmorCompileRows` (compression disabled first, one plain compile after a rejected
  `-c0`, one `-c0` compile when accepted, the stub's digest). Both exits 0.
- The stub probe (§6): 42 rows, 27 PASS assertions, 0 FAIL, exit 0 (run at the run-3 bytes; the
  apply probe in the last bullet re-pins the arms the apply stage changed).
- Censuses: §4 counts, re-run at the delivered bytes (identical).
- Anchors re-measured by content at the delivered bytes (each group cited by its §1 digest):
  installer `ab0780fc…`
  `USERSNS_PROFILE_FILE=/etc/apparmor.d/symmetry-linux-owner-userns` :79, `USERSNS_ATTACH`
  :80, `verify_published_file` :324, `verify_existing_or_absent` :342, `publish_or_verify` :379,
  `read_userns_restriction` :491, `resolve_apparmor_parser` :516, `unload_userns_profile` :581,
  `install_userns_profile` :617, `remove_userns_profile` :660, usage :59-60; preflight
  `da5b2caf…` boundary :43, `verifyInstalledLinuxOwnerAssets` :138-181,
  `verifyLinuxOwnerAppArmorUserns` :441-470; `main.go` `ad8547f1…` `apparmor_userns=` :266;
  fixture `e85c1c32…` capability :967, securityfs guard :1644-1649, installer run :1654-1665,
  verify :1674-1681, scratch group :2997-3001; rootfs-base `c8e49724…` package :47, mask :70-72;
  `daemon/native/linux-gate/**` byte-identical to `61399984`.
- Apply-stage re-runs (2026-11-26, post-apply bytes; §8 delta table): `bash -n`, `sh -n` and
  `dash -n` exit 0; shellcheck finding sets identical to the pre-apply bytes (installer 2,
  fixture 16); `gofmt -l .` empty; `GOOS=linux go vet` and `GOOS=windows go vet` exit 0; WSL
  Ubuntu-24.04 `go test -count=1 ./internal/linuxpreflight/ ./cmd/symmetry-daemon/` `ok` as the
  unprivileged user and as root (all five `LinuxAppArmor*` rows PASS as root, including the new
  literal stanza pin); C1-C7 identical (§4); the changed installer arms re-pinned by a local
  root probe, 18/18 rows, driver sha256
  `f0d40c7076fcee65a0e3973715f2cf6bf2c061064db54b56a2ceba1a2d72cdee` (row output and
  substitutions in the apply stage answer).
- Run 3's own gates are the two acceptance case sets and the in-guest install/verify rows.

## 11. Reviews (rounds 1 and 2) and findings' dispositions

Round 1 (before run 2's readback failure was understood): a Sol review, a DeepSeek audit and a
`security-reviewer` pass over the `61399984..af5804b9` diff; findings under the GDD state root.
Dispositions:

| finding | disposition |
| --- | --- |
| Sol P0-1 / security F5 (the digest premise is refuted by the run-1/2 rows) | fixed: `-c0` compilation with the plain-compile fallback for parser < 5, whole/byte-9 digests, staged-bytes load; the measurements are §2/§8 |
| Sol P1-2 / security F4-part (dead `findmnt --quiet` guard) | fixed: the repository's `findmnt --noheadings --output TARGET --target` pattern + named mount failure |
| Sol P1-3 / security F1-part (Go refused `0` + absent interface while the installer accepted) | fixed: the `0`+absent case is `not_needed` in both; the file is now verified on every host, so a leftover profile is refused |
| Sol P2-4 (unreachable rollback; a failed re-run could destroy a working install) | fixed: `verify_userns_profile_load` returns instead of exiting, publication moved after the readback, the rollback only touches this run's artifacts and reloads a verified pre-existing text |
| Sol P2-5 (fail-open `[ -z "$(scan …)" ]`) | fixed: every scan call captures the status (`x=$(scan …) || fail/report …`), in the installer's install, removal and rollback paths |
| Sol P2-6 / security F2-part (`--capability=all`; attach shortcut) | fixed: `--capability=CAP_MAC_ADMIN` + the base rootfs mask; the attach shortcut is removed |
| Sol P3-8 (`preflight_unsupported.go` outside the owned list) | kept, flagged for owner ratification (build requirement of `main.go` on every GOOS; plan §5) |
| Sol P3-9 / security F2 (no rows binding the IO; the decision line untested by unit rows) | fixed: `TestLinuxAppArmorHostStateRows` (sysctl/files/compile); the decision line is exercised end-to-end by the fixture's in-guest verify (run 3) |
| Sol P3-10 (parser directory not checked) | fixed: `verifyLockedRootDirectory` on the parser's directory |
| Sol P3-11 (pinned package postinst in debootstrap) | confirmed benign: run 2's base build step succeeded |
| security F3 (installer bound the readback to the current file; ACL-blind directory check) | mitigated: the readback now proves the *staged* bytes this run loaded and the publication is atomic after it; the directory ACL checks stay in the preflight (the installer's `check_root_directory` is owner/mode only) — recorded residual |
| DeepSeek audit C5 = 4 files, C7 = 18 in 8 files, shellcheck sets identical | adopted into §4/§10 (the contract's plan numbers 2/16 were corrected) |

Round 2 (over the amended diff, before run 3): a second Sol review, DeepSeek audit and
`security-reviewer` pass. `security-reviewer` round 2:

| finding | disposition |
| --- | --- |
| F-1 (the exact-attachment invariant needs a negative row: nothing showed the accepted digest is attachment-sensitive) | programmed into run 3's probe (two texts differing only in the attachment path, `-c0 -S` digests compared) but **not captured**: the probe aborted at `blob.sh:10` before that row (§8, §12). The loaded-identity argument therefore rests on the exact-attachment text plus the kernel's recorded hash of the compiled policy; the attachment-sensitivity measurement stays open (§12) |
| F-2 (the installer's reference hash was the staged file's own hash, so a POSIX-ACL writer on `/etc/apparmor.d` could make the run verify against its own bytes) | fixed: `PROFILE_HASH` is the hash of the fixed printed text, and the staged file is checked against it before and after the load; a mismatch is a named refusal |
| F-3 (an interrupt after `--replace` left a loaded, unpublished profile; the exit trap never unloaded) | fixed: an unload flag is set around the load and cleared on every rollback and on the success path; the exit trap unloads what the run loaded, and the success path is measured to issue no unload |
| verified negative (the `-c0`/fallback pair cannot widen acceptance; decision lines carry no path/errno/free text; the fixture census and the no-sysctl-writer greps hold) | accepted as stated |

Sol round 2: confirmed the mechanism from the parser source (`-c0` really disables compression;
the `-S` stdout and the kernel write are the same raw bytes; the kernel hashes what it receives,
or the decompressed equivalent), and found:

| finding | disposition |
| --- | --- |
| P1-1 (all three unload sites passed a bare profile name to `-R`, which requires a definition file, so nothing was ever unloaded) | fixed: `unload_userns_profile` passes the staged file, then the published file, then a freshly printed temporary declaration, and the probe now asserts that the unload's argument is a file path (row 18) |
| P1-2 (the records still described `--capability=all` and "the loaded, attached policy") | fixed in `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this record |
| P2-1 (the installer's `/etc/apparmor.d` check is ACL-blind, so a hostile ACL writer can win the load window) | documented residual: the fixed-text hash check refuses publication and, with P1-1 fixed, the rollback unloads the swapped policy again; root is out of the threat model |
| P2-2 (the Go `-c0`/fallback order had no unit row) | fixed: `TestLinuxAppArmorCompileRows` (root-gated) asserts `-c0` first, exactly one plain compile after a rejected `-c0`, exactly one `-c0` compile when accepted, and the digest of the stub's bytes |
| P3-1 (mixed hashing discipline on the staged file) | accepted as a residual: the staged checks are by path, the published check is FD-pinned |
| P3-2 (Go followed a symlinked sysctl; the installer refuses it) | fixed: the reader lstats first and reports `unreadable` for a non-regular path (row added) |
| P3-3 (an unsafe parser or parser directory is labelled `parser_absent`) | accepted: the label is imprecise, the refusal is the same |
| P3-4 (candidate-set divergence for a <8-byte compile; a misleading usage line) | fixed: the shell adds the byte-9 candidate only above 8 bytes and the usage line now says parser < 5 is accepted on the whole output |
| P3-5 (a post-publish verification failure leaves the suspect file) | accepted as a residual: the next run and the preflight refuse it |
| P3-6 (a SIGKILL can leave a stray `/etc/apparmor.d/.linux-owner-userns.*`) | accepted as a residual: normal failures remove the temporaries |
| P3-7/P3-8 (`preflight_unsupported.go` outside the owned list; the probe's determinism row compared the wrong pair) | P3-7 stays for owner ratification; P3-8 fixed (each probe compile has its own output file) |

DeepSeek round 2: re-ran the whole gate battery at these bytes (all pass, §10), re-measured the
censuses (C1 3, C2 1, C3 3, C5 4 files, C6 0, C7 18 in 8 files; C4 re-measured below), confirmed
clause verdicts for Change 1/2/3 and the decision tables, and flagged: the record's decision-row
count (21, not 23 — fixed), the `docs/TODO.md` capability line (fixed), the scope notes for
`preflight_unsupported.go`, `CAP_MAC_ADMIN`, the base-rootfs mask and the runbook section (all
four kept and recorded for owner ratification), and that run 3 remains the go/no-go for the
runtime equality. Run 3 closed that question positively: the in-guest installer and preflight
both printed `apparmor_userns=per_program`, the probe's `-c0` host row equals the kernel's
recorded `raw_sha256`, and the acceptance then executed the gate legs (§8) — the readback is
pinned on the runner.

## 12. Limits and residuals

- `CONFIG_SECURITY_APPARMOR_HASH` off, an unreadable policy interface, a missing/rejected parser
  or an unreadable recorded hash are refusals (`apparmor_state_unreadable`, `parser_absent`,
  `parser_failed`), never weakened checks.
- The `-c0` option requires parser >= 5; older parsers are accepted only through their already
  uncompressed `-S` output, and a parser that rejects `-c0` but compresses would be refused.
- `attach` never carries the path for these parser/DFA attachments (measured), so the loaded
  identity rests on the kernel's recorded hash; a same-named, same-mode profile attached
  elsewhere cannot satisfy it without a sha256 preimage or root.
- The installer's directory check is owner/mode only (no ACL inspection); a non-root writer with
  an ACL grant on `/etc/apparmor.d` could win the load window; the fixed-text hash check refuses
  publication and the rollback unloads the swapped policy again, so nothing persists, and the
  preflight's own file check refuses as well — root is out of the threat model either way.
- The staged-file identity checks hash by path while the published-file check pins an FD; a
  SIGKILL can leave a stray `/etc/apparmor.d/.linux-owner-userns.*` temporary, and a
  post-publication verification failure leaves the suspect file for the next run to refuse.
- The in-guest load mutates the runner host's kernel policy (policy is not namespaced); the guest
  is disposable and loads only the installer's own profile, and the guest's distro set is no
  longer reloaded (the mask).
- Owner ratification is requested for four bounded additions outside the contract's literal
  owned list: `daemon/internal/linuxpreflight/preflight_unsupported.go` (the cross-platform
  build stub), the fixture's `--capability=CAP_MAC_ADMIN` (exactly the load's missing
  capability, strictly narrower than the round-1 draft), the base rootfs' single mask line (it
  cannot move into the fixture without breaking the C6 census), and the runbook section that
  documents the removal procedure Q-E asked for.
- Run-1's probe defect rows (void attribution) are recorded in plan §2; run 2's corrected probe
  supplies the measurements used above.
- Run 3's probe aborted after its host rows inside its own scratch helper
  (`/usr/.../shared/blob.sh:10: $2: unbound variable`, swallowed by the step's `|| true`), so its
  guest rows, the F-1 attachment-sensitivity row and the in-guest load row were not captured. Run
  3 was the last code-affecting action (three-run budget) and the scratch branch is deleted, so
  those rows stay unmeasured on the runner; the substitute evidence is the run itself — the
  in-guest installer and preflight both printed `apparmor_userns=per_program` for the
  exact-attachment text and the acceptance then executed the gate legs.
- The migration next stop (`-linux-v2-native-owner-witness: wait for authentic Pi owner witness
  turn`, the Pi turn wait at `daemon/internal/app/linux_owner_witness_linux.go`; the transport
  start beforehand returned no error) has no determined cause in this evidence. Recorded
  mechanics for the next goal's check: the release-stage marker writes sit in the `daemon.start`
  closure (`linux_owner_witness_linux.go` `witness.stage(linuxV2NativeWitnessReleaseStage …)`) and
  the run-3 barrier timeout (`native_release.json` not observed within `Barrier.TimeoutSeconds`)
  while the witness itself progressed to the turn stage. The discriminating checks are the same
  witness in a real-credential environment (the scratch workflow runs the ratified dummy pins)
  and the Pi turn's own bounded error.
- The two `shared` next-stop rows (`clone_integration_linux_test.go:252` and
  `launcher_integration_linux_test.go:451`, both `open /proc/<pid>/environ: permission denied`
  on the exec'd B process, plus the cleanup row `clone_integration_linux_test.go:113 …
  cgroup.kill: bad file descriptor`) are new territory first reached in run 3 and are the
  `shared` set's other rows, out of scope by the contract's Acceptance §4; they are named
  verbatim here because Acceptance §1 requires the next stop to be named.
  `TestInstalledLinuxOwnerCloneCleanupParentDeathScratch` (PR #287's row) passed in run 3.
- The contract's Change 4 (the D3 `harness.md` / `0010-refactor-program.md` amendment) is
  proposed, not applied; the text is in plan §9.
- The apply-stage deltas (§8) are not re-exercised by a runner acceptance: the run-3 binding
  covers the pre-apply revisions, and the next acceptance run is the oracle for the
  removal-path and readback deltas; the apply probe's 18/18 local rows stand in meanwhile.
- The shell reader strips the whole sysctl file's ASCII whitespace while the Go reader's
  `TrimSpace` also accepts Unicode-whitespace edges, so the shell side is strictly narrower; a
  divergent input can only refuse an install, never accept one.
