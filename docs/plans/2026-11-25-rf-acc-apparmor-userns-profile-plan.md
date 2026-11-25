# rf-acc-apparmor-userns-profile — implement plan

Contract: `goal-rf-acc-apparmor-userns-profile.md` (GDD state root, external to the repository).
Base revision `61399984` (PR #289 merge, the ruling's dispatch gate); branch
`refactor/rf-acc-apparmor-userns-profile`. Conductor pre-rulings carried: Q-A (read-only kernel
check; `gate.c` stays byte-identical, no self-check mode), Q-B (in-guest load through the
installer), Q-C (no c-gate change), Q-D (one side-effect-free installer emit mode is the single
source of the profile text), Q-E (a bounded installer removal mode plus the documented
procedure). The accepted plan is the contract; this file resolves only the implementation
choices it leaves open and records the probe evidence that closed them.

## 1. Clause census at the dispatch head (re-located by content)

| Contract clause | Dispatch head | Content |
| --- | --- | --- |
| Change 1 installer | `scripts/install-linux-owner.sh` | 9-argument install, root-only, marker/resume, `verify_published_file`/`publish_or_verify`/`verify_existing_or_absent` (delivered :324, :379, :342), fixed publish list, no service start/stop |
| Change 2 preflight | `daemon/internal/linuxpreflight/preflight_linux.go` | `LinuxOwnerPreflightRuntimeBoundary` (delivered :43), `verifyInstalledLinuxOwnerAssets` (delivered :138-181), `verifyLinuxOwnerAppArmorUserns` (delivered :447-476) |
| Change 2 decision line | `daemon/cmd/symmetry-daemon/main.go` | `-verify-linux-owner-install` prints the boundary line and `apparmor_userns=` (delivered :266) |
| Change 3 fixture | `scripts/ci/linux-v2-native-acceptance.sh` | nspawn boot with the narrowed capability (delivered :968), securityfs guard (delivered :1648-1652), in-guest installer run (delivered :1656-1667), in-guest `-verify-linux-owner-install` (delivered :1676-1683), installed-owner scratch group (delivered :3001) |
| Change 3 base package | `scripts/ci/linux-v2-native-rootfs-base.sh` | pinned `--include=` set (delivered :47) and the guest policy-service mask (delivered :70-72) |
| Change 4 design text | `docs/design/harness.md`, `docs/goals/0010-refactor-program.md` | not owned; the amendment text is proposed in §9 and applied by the D3 amendment |

Recorded censuses at the delivered bytes (all re-run; evidence record §6): C1 `grep -rln
"flags=(unconfined)" scripts/` = 3 files (the two cited CI exceptions plus the installer's emit
mode); C2 `grep -rn "unshare(CLONE_NEWUSER)" daemon --include=*.c --include=*.go` = 1
(`gate.c`); C3 `CLONE_NEWUSER` = 3 (the named non-creation sites unchanged); C4 the sysctl
readers = the previous set plus the installer and the preflight, no writer; C5
`install-linux-owner` = 2; C6 `grep -c apparmor scripts/ci/linux-v2-native-acceptance.sh` = 0
(the fixture authors no policy text and never names the parser, file or mode: it only uses the
installer's own modes and kernel interfaces); C7 `-verify-linux-owner-install` entry points plus
the new decision accessor.

## 2. Probe evidence that closed the open choices (scratch run 1, `45644664168`, head `7db44f4e`)

Runner: `ubuntu-latest` = Ubuntu 26.04, kernel `7.0.0-1020-azure`, `apparmor_parser 5.0.2`,
`kernel.apparmor_restrict_unprivileged_userns=1`. Probe rows (log `native/run-45644664168.log`,
scratch step "Scratch-only AppArmor userns readback and nspawn attribution probe"):

- `attach` reads `<unknown>` for every path-attached profile (the userspace parser never writes
  the kernel's optional attachment string), so the attachment-path readback is not directly
  available; `mode` reads `unconfined`; `name` reads the profile name; all three are
  0444 but only as world-readable under `/sys/kernel/security/apparmor/policy/profiles`.
- `raw_data` (the loaded policy blob) is **not** readable by the unprivileged user, but
  `raw_sha256` (the kernel's recorded load-data hash, an ordinary 0444 file) **is** readable, and
  `apparmor_parser -Q -S -K -T --skip-bad-cache` compiles unprivileged and deterministically
  (two runs byte-identical; 597 bytes = an 8-byte `compr_user_header` plus a 589-byte zstd
  frame). `parser_interface.c` writes that same compressed buffer to the kernel, so the kernel's
  `raw_sha256` is the digest of the compiled bytes with the 8-byte cache header stripped; the
  check therefore accepts the digest of the whole output or of the output from byte 9 on.
- The guest root cannot load policy with the container manager's default capability set
  (`arg_caps_retain` in nspawn omits `CAP_MAC_ADMIN`; the row shows "Access denied. You need
  policy admin privileges" from the in-guest postinst). The fixture therefore boots its guest
  with `--capability=all`, which is the only change that lets the *installer* exercise its
  production install/load/verify path in the guest (Q-B(a)); a host-side load would put the
  profile outside the installer's own command and the in-guest verification could not compile
  with the loading parser.
- The guest's systemd mounts `securityfs` at `/sys/kernel/security` at boot (`mount-setup.c`),
  and `systemd-nspawn` switches root with `pivot_root` (`mount_switch_root_pivot`), so a guest
  exec of `/usr/libexec/symmetry/symmetry-linux-gate` computes exactly that namespace-absolute
  path and matches the installed attachment.
- Base red confirmed verbatim on the dispatch bytes with both sets: `migration`
  `release_outcome_unknown; stage=clone_bootstrap arm=bootstrap_attempt wait_class=other
  guest_stage=gate_drop_bounding_set errno=1`; `shared`
  `clone_integration_linux_test.go:240 … Linux B clone bootstrap failed at stage 13 (errno=1)`
  and `launcher_integration_linux_test.go:439 … native exec failed after RELEASE: errno=1`
  (both sets exited 1).

Probe defects in run 1 are recorded as a residual: the probe unloaded its profiles before the
nspawn rows and ran the guest payload as root, so its attribution rows are void; run 2 carries a
corrected probe (profiles stay loaded; the guest payload runs through `setpriv` as an
unprivileged user; the digest rows compare the recorded `raw_sha256` content) whose rows confirm
the mechanism before the acceptance step consumes it.

### Run 2 measurements (scratch run 2, `45651150146`, head `af5804b9`) — the readback amendment

The corrected probe refuted §2's hash premise on the runner (both host rows and the guest row):
the kernel's `raw_sha256` equals **neither** the whole `-Q -S` output nor its byte-9 payload
(`blob host: size=588 whole=b60bd1… payload=a27c99…` against `raw_sha256=c9f1a3…`;
`blob guest-ingest: size=427 whole=4b7a71… payload=579348…` against `fdd39e…`), while `-S`
output stays byte-identical across runs. Two further rows explain it:

- The parser help captured in run 1 carries `-c[n], --zstd-compress-level[=n]  Compress profile
  at level n (0 disables compression, default: 10)`, and the kernel source (AppArmor 6.15/7.0
  `security/apparmor/policy_unpack.c`, `aa_unpack`) hashes `udata->data, udata->size` — the
  **decompressed** policy as received — while `apparmorfs.c`/`aafs`' `rawdata_open` decompresses
  the kernel's stored copy back to exactly those bytes and gates the read on
  `aa_current_policy_view_capable()`. A parser >= 5 `-S` output is compressed (588 bytes
  compressed against a 3186-byte decompressed `raw_data` read in the guest as root), so the
  digest rule can never match a compressed compilation. Parser 4.0.1 writes `-S` uncompressed
  (measured locally: 1428 bytes, and it rejects `-c`) — there the whole-output digest is the
  kernel's recorded one.
- `cat profiles/<name>.<id>/raw_sha256` (a symlink into `policy/raw_data/…`) reads `<unknown>`
  for the attachment in every tested parser: `seq_profile_attach_show` prints the path only when
  the parser supplied `xmatch_str` (never for these DFAs; 4.0.1 emits `aadfa`/`notflex`), so the
  attachment-string route is dead on the target host and the kernel's recorded policy hash is
  the only usable evidence.

Run 2's acceptance case sets therefore failed closed at the installer's readback
(`install-linux-owner: the loaded AppArmor userns profile differs from the installed userns
profile text`; both sets `exit 1`), which is the verbatim refusal the contract's failure control
asks to record rather than shipping a check that cannot pass. **Amendment (smallest change
keeping the ruled readback's intent):** compile with compression disabled (`-c0`, accepted by
parser >= 5; on a parser that rejects it the installer and the preflight fall back to the plain
uncompressed `-S`, accepting only those digests), and load the profile from the *staged* bytes
this run verified. The `attach`-equality shortcut is removed: it could never match on the target
host and skipped the rule-set evidence (security review F2). Run 3's probe adds the positive
row this amendment rests on: `-c0 -Q -S` whole/byte-9 digests against the kernel's `raw_sha256`
for a profile loaded the production way (`--replace`, compressed).

## 3. Profile text, attachment set and load command (the installer's, byte-fixed)

```
abi <abi/4.0>,
include <tunables/global>
profile symmetry-linux-owner-userns /usr/libexec/symmetry/symmetry-linux-gate flags=(unconfined) {
  userns,
}
```

- One `profile` stanza per exact installed binary path; the measured set is the census C2 set
  (`/usr/libexec/symmetry/symmetry-linux-gate`); the launcher core, wrapper, daemon and clone
  bootstrap create no user namespace and are not attached. No glob, directory, regex or prefix.
- The only rule is `userns,`; the only other bytes are the probe's two boilerplate lines; the
  attachment is unquoted, matching Ubuntu's shipped per-program `flags=(unconfined)` pattern.
- File `/etc/apparmor.d/symmetry-linux-owner-userns`, root:root mode 0644, singly linked,
  published through the installer's existing machinery (hash-pinned temp file beside the final
  path + `publish_or_verify` + `verify_published_file`), so a re-run is idempotent and a
  mismatch fails closed.
- Load command: the resolved absolute `apparmor_parser --replace <file>` as root; nothing else
  is invoked for loading. The installer emits the profile text through
  `-print-userns-profile` (the single source shared by the installer, the acceptance fixture and
  the text rows) and removes it through `-remove-userns-profile`, which unloads the profile,
  proves it is gone and removes the file; the usage text documents that removal leaves a
  restricted host refused by the preflight (the intended fail-closed state).

## 4. Installer decision table (Change 1) and readback

`install_userns_profile`, called after the markerless-partial-install refusals and before the
install-intent marker can become durable:

| host state | action |
| --- | --- |
| sysctl absent or `0` | nothing installed and no load; refuses when the profile file or a loaded profile with the fixed name exists, with the removal mode named; an enumeration failure is a refusal, never an empty result |
| sysctl present and neither `0` nor `1` | fail closed (bounded stage) |
| sysctl `1`, no `apparmor_parser` | fail closed at the named AppArmor stage |
| sysctl `1` | stage the fixed text, load that staged file with `--replace`, verify the loaded fact, then publish it atomically and print `apparmor_userns=per_program` |

Readback (identical rules to the preflight): the loaded profile directory is resolved by the
exact `name` (duplicates and enumeration failures are refusals); `mode` must read `unconfined`;
the kernel's recorded `raw_sha256` must equal the digest of the staged
bytes compiled with compression disabled (`-c0 -Q -S -K -T --skip-bad-cache`, whole output or
output from byte 9 on; a parser that rejects `-c0` falls back to the plain uncompressed `-S`).
The `attach` interface cannot carry this evidence on the target host (run 2 measurements), so it
is not read. Publication happens only after the readback, so a failure never leaves a published
artifact; a failed load or readback unloads what this run loaded, discards the staged file and
its compilation, and — when a verified published file already existed — reloads that text so the
host is left as found. The failure text names the failing stage only.

## 5. Preflight (Change 2)

`verifyLinuxOwnerAppArmorUserns` runs inside `verifyInstalledLinuxOwnerAssets` and records the
decision in `linuxOwnerAppArmorUsernsDecision`; `main.go` prints `apparmor_userns=<decision>`
beside `LinuxOwnerPreflightRuntimeBoundary`. Closed vocabulary: `not_needed`, `per_program`,
`sysctl_invalid`, `apparmor_state_unreadable`, `unexpected_profile`, `profile_absent`,
`profile_file_mismatch`, `profile_not_loaded`, `profile_loaded_mismatch`, `parser_absent`,
`parser_failed`; refusals carry `apparmor_userns=<token>: <fixed reason>` and no path, errno or
free text. The decision is a pure function of an evidence struct, so the positive/negative rows
are unit rows; the interface read, file identity/text, and the unprivileged compilation are the
only IO. `/etc/apparmor.d` and the profile file are checked through the existing asset checks
(root:root, mode, single link, no untrusted ACL writer, A must read and A/B must not write), and
the same helper checks the parser's own directory. The file is verified on **every** host, so a
profile left behind while the sysctl is `0`/absent is refused (`unexpected_profile`) instead of
silently accepted, and a `0` host whose loaded-profile interface is absent (a container without
securityfs) stays `not_needed` — the installer and the preflight agree on both.
The cross-platform build needs one `LinuxOwnerAppArmorUsernsDecision` stub in
`preflight_unsupported.go` (the only non-Linux-amd64 home in the package); its value is the
vocabulary's `not_needed` token and it is unreachable, because the unsupported verify always
fails first. That file is outside the contract's owned list and is the one addition the plan
justifies for the build.

## 6. Fixture (Change 3)

No policy text, load command or attachment of its own. Three bounded additions: the guest boots
with the container manager's default capability set plus exactly `CAP_MAC_ADMIN` (the load's
only missing capability; `--capability=` adds to the retained default in `nspawn.c`, and the
default already carries `CAP_SYS_ADMIN`); before the installer run the fixture ensures
`/sys/kernel/security` is a securityfs mount (the repository's own
`findmnt --noheadings --output TARGET --target` pattern, then `mount -t securityfs`, with a named
failure — `findmnt --quiet` from the first draft is not a findmnt option and made the guard
dead); the pinned rootfs package set gains `apparmor` so the in-guest parser the load and the
readback need exists, and the base rootfs masks `apparmor.service` so the guest's boot does not
reload the distro profile set into the shared kernel policy. The installer's in-guest run and
the in-guest `-verify-linux-owner-install` run remain the acceptance oracles.
Deliberately *no* host-side load: the host-side invocation would move the load command out of
the installer, and a guest parser other than the one that loaded cannot reproduce the compiled
identity.

## 7. Rows and mutants

- Text rows (`daemon/internal/linuxpreflight/preflight_linux_test.go`,
  `TestLinuxAppArmorProfileTextMatchesTheInstaller`): run the installer's emit mode and require
  byte equality with the preflight's text, exactly five lines, the exact stanza line, exactly
  one `userns,` rule, and refuse glob metacharacters, extra flags and every confinement
  alternative (positive and negative rows in one test).
- Decision rows (`TestLinuxAppArmorUsernsDecisionRows`): 21 positive/negative rows over the
  closed vocabulary, including the 0-host `not_needed` rows (with and without the securityfs
  interface), the compression-disabled whole and byte-9 `per_program` rows, and every refusal.
- Interface rows (`TestLinuxAppArmorLoadedProfileScan`): a real fixture tree under `t.TempDir()`
  with the fixed name, another profile and a stray file; duplicate names reported.
- Host-state rows (`TestLinuxAppArmorHostStateRows`): the sysctl reader against real files, the
  profile-file verifier against a foreign-owned temp path (absent, world-writable, symlink,
  foreign text; the root-owned positive rows are gated on `Geteuid()==0`), and one real
  `apparmor_parser` compilation of the profile text.
- The installer step's own decision/failure/rollback rows are a bounded local probe (run as root
  in WSL Ubuntu-24.04 with the fixed paths substituted to a scratch layout and `apparmor_parser`
  replaced by a stub that records every invocation and materializes the loaded-profile
  directory; substitutions recorded in the evidence): absent sysctl and sysctl `0` install
  nothing and print `not_needed`; a `0` host carrying the file or a loaded profile is refused;
  a failing load is named at the load stage, leaves no published file and no staged text, and
  attempts the unload; a loaded profile whose recorded hash is another policy is refused at the
  readback and rolled back; a successful load publishes exactly the fixed text and prints
  `per_program`; a failed re-run over a verified published file reloads that text.
- Mutants: M1/M2 (glob attachment, second rule) redden the text rows with the mutated installer
  in place and restored byte-identically; M3/M4/M5 (install on a `0` host, ignore the load
  failure, skip the rollback's unload and staged-text removal) redden the local probe's rows on
  scratch copies; M6/M8 (accept a missing load, require the profile on a `0` host) redden the
  decision rows; M7 (accept any recorded hash) is the substituted analogue of the removed
  attachment shortcut — its original mutation no longer applies, and the substitution is
  recorded. M6/M7/M8 mutate a scratch copy of the whole daemon module under `/tmp`, restored;
  M10 reddens the C6 census on a scratch copy of the fixture; M9/M11 are not applicable because
  no `gate.c` byte changes.

## 8. Runs, reviews and budget

At most three scratch runs (branch `scratch/rf-acc-apparmor-userns-profile`, throwaway workflow
copy of the real workflow without the environment block and without secrets/vars, dummy pins,
scratch `gh` shim sha256 `507340af…`, `case_sets=(migration shared)`): run 1 the base red with
both sets (done, `45644664168`); run 2 the fix with both sets and the corrected probe (done,
`45651150146`: the probe confirmed the mechanism and the corrected attachment rows, and both
sets failed closed at the readback — §2's amendment); run 3 the final literal run on the
amended, reviewed bytes (the fix commit below `af5804b9` on the same scratch branch). Review
routing before any byte-binding conclusion: one Sol review, one DeepSeek audit and one
`security-reviewer` pass over the diff, findings under the GDD state root; round 2 re-runs all
three over the amended diff before run 3, the run that binds the delivered bytes. The scratch
branch is deleted at the end and its absence proven with `git ls-remote`.
The round-2 reviews added three bounded hardenings before run 3: `PROFILE_HASH` is the hash of
the fixed printed text (not the staged file's own hash) with the staged file checked against it
before and after the load (security round-2 F-2); the exit trap unloads what the run loaded when
a failure escapes the explicit rollback, with the flag cleared on every rollback and on success
(F-3); and run 3's probe measures the digest's attachment sensitivity with two texts differing
only in the attachment path (F-1).
The Sol round-2 review then found that `apparmor_parser -R` takes a definition file rather than
a profile name, so every unload site was a silent no-op (P1-1): `unload_userns_profile` now
passes the staged file, the published file or a freshly printed temporary declaration, the probe
asserts the unload's argument is a file path, and the Go compile order gained a root-gated unit
row (P2-2, `TestLinuxAppArmorCompileRows`).

### Run 3 outcome (final literal run; the evidence record §8 carries the full table)

Run 3 `45659523339` (head `ce3c30b9`, the amended, review-hardened bytes) ran both sets
foreground to completion; both exited 1. The installer and the preflight passed end-to-end on
the runner: the in-guest install and `-verify-linux-owner-install` runs printed
`apparmor_userns=per_program`, and the acceptance proceeded past the wall the goal exists for —
the migration witness's transport start resolved its one-use RELEASE (run 1's `... resume
process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: ...
guest_stage=gate_drop_bounding_set errno=1` is cleared; the RunJournal grew from 5988 to 6559
bytes) and the shared set's `clone_integration_linux_test.go:240` /
`launcher_integration_linux_test.go:439` rows are cleared. The next stops, named verbatim in the
record §8: `migration` — the test row `wait for exact native_release boundary: owner-witness
command did not reach its requested durable stage` with the owner-witness command's own last
stderr row `-linux-v2-native-owner-witness: wait for authentic Pi owner witness turn`; `shared` —
`clone_integration_linux_test.go:252: same-PID clone-native process is not exact B: open
/proc/7171/environ: permission denied` and `launcher_integration_linux_test.go:451: native
process did not retain the exact B-only boundary: open /proc/7197/environ: permission denied`.
The run-3 probe captured the amendment's decisive host row (`blob host-c0: whole=fceb72ac… equals
kernel_whole=yes` against the kernel's recorded `raw_sha256`) and then aborted at its scratch
helper (`blob.sh:10: $2: unbound variable`); its later rows (guest ingest, attachment
sensitivity, in-guest load) are recorded as a probe residual — run 3 is the last code-affecting
action and the scratch branch is deleted (`git ls-remote origin
scratch/rf-acc-apparmor-userns-profile` prints nothing).

## 9. Records and the proposed D3 amendment text

The evidence record `docs/evidence/rf-acc-apparmor-userns-profile-2026-11-25.md` carries the
per-run table, the profile digest and attachment set, the preflight decision/refusal rows, the
mutant rows with restored digests, and the sha256 equality table of run 3's head against the
delivered files. `docs/TODO.md`:1639-1649 is consumed (its "to be specified by the D3
amendment" statement is replaced by the delivered facts and by the amendment pointer) and
`docs/archive/CLOSED-TODO.md` gets the entry under this goal's heading only for what closes.
Proposed amendment text for `docs/design/harness.md` (anchor: after the Pi paragraph, before
`OmO promotion`) and for the D3 row and W5-02 unblock list in
`docs/goals/0010-refactor-program.md`, as drafted by the contract (Change 4), with one measured
correction: the installed set is the gate path `/usr/libexec/symmetry/symmetry-linux-gate`
(the launcher core does not create a user namespace; census C2), so the amendment should read
"for the exact installed gate path" rather than "launcher/gate paths".

## 10. Out of scope (with reason)

The sysctl policy decision itself; the two existing CI exceptions; `daemon/e2e/**` and the other
packages the contract excludes; `shared`'s other rows; `synthetic_goal_cleanup`; non-Ubuntu
hosts and WSL2; `control/**`; any `gate.c` byte. Residuals recorded in the evidence record: run
1's probe-defect rows; the round-1 findings' dispositions (F1/P1-3, F2, F3, P1-2, P2-4, P2-5,
P2-6, P3-7, P3-8, P3-9, P3-10, P3-11); the local-only negative installer rows (the acceptance
runs exercise their positive paths); hosts with `CONFIG_SECURITY_APPARMOR_HASH` off or an
unwritable policy interface are refused (`apparmor_state_unreadable`) rather than weakened; a
guest whose systemd already mounted securityfs needs no fixture mount; the `--capability=all`
draft was narrowed to `CAP_MAC_ADMIN` and the base rootfs masks the guest's policy service, so
the fixture widens nothing beyond the load's own capability.
