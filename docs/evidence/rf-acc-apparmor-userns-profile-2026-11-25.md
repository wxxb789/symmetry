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
| `scripts/install-linux-owner.sh` | `8d61c5ba7077808b20934aa42f6d44b6542adbf789fe5e622328a697c42367cf` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `4f14046a2351dfe069aacc417cd4985ec91e4fb54cce5e89784f68241cb16bd4` |
| `scripts/ci/linux-v2-native-rootfs-base.sh` | `c8e4972490151e03e6b6c9ea3fa5393a8c348ebf0c9912e1d16770842f7c5866` |
| `daemon/internal/linuxpreflight/preflight_linux.go` | `50f56f3f5b2190e186172fccd556c613037ae48617a3662f95fef6be9933aba9` |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `62a1bd7a9b37b3a48b91a3c5d5ddb938461407e86e1452e207a8cacb71276868` |
| `daemon/internal/linuxpreflight/preflight_unsupported.go` | `bf616df6e052fdbea37d1268d87970f39f3d2de05a9cc8c56952c34339441960` |
| `daemon/internal/linuxpreflight/preflight_unsupported_test.go` | `dfcb8084daa1edbcf68eee5800f78bb783bc4d6302327cff792398c68cf442ec` |
| `daemon/internal/linuxpreflight/apparmor_userns.go` | `e53a5a81f89c34e09ccf694556a4325d4a7f4ddc91dfb104daa06f0acb0aba6f` |
| `daemon/cmd/symmetry-daemon/main.go` | `ad8547f13c0c080eb674f08799ca881be142cb549e292c2fa2b685176af73890` |
| `docs/runbooks/control-plane-and-daemon.md` | `49a61eb7e93de4a1183ed11dc72887d6fadba825397ce653910b3588ba4f7d7f` |
| `docs/plans/2026-11-25-rf-acc-apparmor-userns-profile-plan.md` | `e02bd66774869cad0bffec854a4d04ccb09833f0e613f3ea8d6d1ab5fb5cac5a` (record revision; not run-bound) |

Run 3's head `ce3c30b9` (the amendment plus the round-2 hardening commits) is bound to the
run-3 code revisions by the §8 equality table; the apply-stage simplification (2026-11-26) and
the review-fix stage (2026-11-26) then changed the files above, so the table carries the
delivered post-fix revisions and the §8 delta tables record each change, its class and its
re-run gates. The plan and this record carry post-run, record-only edits and are not run-bound
code, and `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this record are deliberately not
hashed anywhere.

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
7. The policy inputs are the file **and its include closure**: `abi <abi/4.0>` and
   `include <tunables/global>` compile into the loaded policy, so the preflight verifies the
   two closure files and their directories exactly like the profile file (root-owned, singly
   linked, no group/world write, no untrusted ACL writer, readable by A) and the installer
   verifies their owner, mode and link count before it loads anything. A writable closure
   would otherwise let a writer widen the loaded policy while the profile file stays
   byte-identical.
8. Every unload passes only a definition file whose bytes equal the fixed text — the staged
   file, the verified published file, or a freshly printed declaration — so a foreign
   declaration placed at the published path is never unloaded (a pre-fix differential pins the
   old behaviour, §7). The installer's exit trap performs the full rollback (unload plus the
   verified-published reload) whenever the run's own load has not been superseded by a
   successful publication, so a failure inside the shared publish path — which names no remedy
   itself — still leaves the host as found.
9. `-remove-userns-profile` refuses when the removed state cannot be proven: a policy interface
   that is missing and could not be unloaded through the fixed declaration, or one that exists
   without being a readable directory, exits non-zero instead of printing the removed sentence.
   A userns re-run blocked on published bytes that differ from the fixed text names
   `-remove-userns-profile` as the remedy in its own message (the shared publish check's text
   names none).

## 3. Profile text, attachment set and load command

The text is not reproduced here (contract Change 5: cite, never copy). It is emitted by
`scripts/install-linux-owner.sh -print-userns-profile` (delivered sha256 in §1); the preflight's
byte-equal copy lives in `daemon/internal/linuxpreflight/preflight_linux.go` under the same
delivered digest. Five lines, one stanza, one rule, one exact attachment (measured set = census C2: `gate.c` is the
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
| C2 | `grep -rn "unshare(CLONE_NEWUSER)" daemon --include=*.c --include=*.go` | 1 | `gate.c` — the attachment target |
| C3 | `grep -rn "CLONE_NEWUSER" daemon --include=*.c --include=*.go` | 3 | the two named non-creation sites unchanged |
| C4 | `grep -rn "apparmor_restrict_unprivileged_userns" .` (minus `.git`, `.omo`, `node_modules`) | 16 hits in 11 files | readers: the installer, the preflight, `linux-v2-host-peer-probe.go`, `linux-v2-host-prerequisite-probe.sh`, `prepare-private-namespace-tests.sh`; the other six are records (`docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, the goal plan, this record, `linux-v2-host-probe-fix-2026-11-22.md`, the runbook); **no writer** (`sysctl -w`, `> /proc/sys` absent) |
| C5 | `grep -rln "install-linux-owner" scripts daemon .github` | 4 | the installer, the fixture, and the preflight/test references (comment + exec); no other caller |
| C6 | `grep -c "apparmor" scripts/ci/linux-v2-native-acceptance.sh` | 0 | the fixture authors no policy text: the case-sensitive census stays 0. The case-insensitive form (`grep -ci apparmor`) reads 13 because the new rows name the test family and the restriction; the policy-text census (`grep -c -e 'profile ' -e 'userns,' -e 'apparmor_parser'`) is 0, which is the property this row protects |
| C7 | `grep -rn -- "-verify-linux-owner-install" scripts daemon` | 18 in 8 files | every entry point known; the check lands on the existing path only |
| C8 | `grep -rn -- "unload_userns_profile" scripts/install-linux-owner.sh` | 3 (definition, rollback call, removal call) | every unload goes through the fixed-text gate; no other `-R` site exists |
| C9 | `grep -c -- '\$PARSER' scripts/install-linux-owner.sh` and `grep -c "exec.CommandContext" daemon/internal/linuxpreflight/preflight_linux.go` | 7 shell invocations, 1 Go invocation | every parser invocation uses the resolver-pinned absolute parser (shell) or the context-bounded `exec.CommandContext` (Go); no other process is started |
| C10 | `grep -c "linuxAppArmorInstallerUID" daemon/internal/linuxpreflight/preflight_linux.go` | 9 | every root-identity comparison in the AppArmor checks reads the single seam (production value 0) |
| C11 | `sed -n '1084,1571p' daemon/internal/linuxpreflight/preflight_linux_test.go \| grep -c 'Geteuid() == 0'` | 0 | no AppArmor row is gated on root any more; the four `os.Geteuid()` uses in the region feed the identity seam, so the rows run in the unprivileged `daemon` job and again as root in the fixture's guest |
| C12 | the branch-diff host-path scan (the conductor's command, described here in prose: the profile-name path segment and the `/mnt` and `/home` prefixes) | 0 | no host path containing a user profile name in the branch diff |

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
- The host precondition is asserted before anything is installed: the guest reads the
  restriction through a glob (`/proc/sys/kernel/*_restrict_unprivileged_userns`, kept out of
  this file's case-sensitive census) and the fixture refuses any value but `1`, so a
  sysctl-`0` runner reddens instead of passing with the mechanism skipped (review STD-A05).
- Both captured logs are asserted, not just their exit statuses: the in-guest installer log and
  the in-guest `-verify-linux-owner-install` log must each carry a line ending
  `userns=per_program`, which is also the sysctl-`1` proof for the window each run covers. The
  positive and negative arms of that grep were exercised locally (§6).
- The preflight package's AppArmor rows run as root inside the guest against the delivered
  sources (`$A_SOURCE`), through the repository's own JSON gate
  (`scripts/ci/assert-go-test-json.py`), which requires every one of the seven named tests to
  run and pass exactly once — a skip fails the fixture. This is the root-capable CI step review
  STD-A07 asked for; the same rows also run unprivileged in the `daemon` job through the
  identity seam (§6), and no workflow file changes.
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
(absent/`1`/`0`/invalid/unreadable), the profile-file verifier against a temp tree owned by the
invoking identity (absent, world-writable, symlink, foreign text, multiply linked; the delivered
bytes run the same positive rows unprivileged through the identity seam and again as root, §6
below), and one real `apparmor_parser` compilation of the profile text. `TestLinuxAppArmorLoadedProfileScan`: fixed name, another profile, stray file, duplicate
names. `TestLinuxAppArmorUsernsDecisionRows`: 21 rows over the closed vocabulary.

### Fix-stage rows (2026-11-26)

The same host, parser 4.0.1, one unprivileged run and one root run, plus a stub-parser driver for
the installer's shell arms (driver, stub and the built copies by sha256; the substitutions are
the apply stage's, extended with the two closure paths, the identity seam and one injected hook
call, all listed in §7):

- `go test -count=1 ./internal/linuxpreflight/ ./cmd/symmetry-daemon/` — `ok` both as the
  unprivileged user and as root. `go test -v -run '^TestLinuxAppArmor'`: 7/7 PASS in both
  identities, including `TestLinuxAppArmorCompileRows` (the compile-order pin), the new
  attachment-sensitivity row and the decision-reset row. On the pre-fix bytes the compile pin
  prints `preflight_linux_test.go:1333: the parser identity and directory checks require root`
  and `--- SKIP` for the unprivileged user, which is the before-state review STD-A07 named.
- Attachment sensitivity (SPEC-A01's local row, the same compile order the production code
  uses): `fixed` 2028 bytes `89fb4ee44077f70d233e07469eb3a7fefa8ecd3ec200cd15ddc2857d8cfcc6da`,
  attachment-changed 2124 bytes `98543524d41ddddf752087afb0c29b2cac8c8e4100541f76cd7415f5ca04bb73`
  (first differing byte 70), name-changed 2028 bytes
  `fdae8892f6acd050402f2e9fc158b4e7ec83cbc77c6220e2d34bb4893a0958fe`. Changing the attachment
  path or the profile name changes the compiled policy hash, which is what makes the readback's
  hash equality prove the exact installed attachment.
- The stub-parser driver: 18 rows, 0 FAIL, including the four before-state differentials (a
  foreign published declaration reports a remedy on the delivered bytes and none on the
  pre-fix bytes; a missing include file refuses on the delivered bytes and loads on the pre-fix
  bytes; an invisible policy interface refuses on the delivered bytes and prints the removed
  sentence on the pre-fix bytes; a foreign published declaration is never the `-R` argument on
  the delivered bytes and is the argument on the pre-fix bytes) and the publish-failure arm
  (a writer that makes the published path unusable between the pre-check and publication: the
  growth-run unloads what it loaded and publishes no file).
- The bounded-output pin caught a real defect in the shared predicate: `boundedLinuxOutput`
  embeds `bytes.Buffer`, whose promoted `ReadFrom` let `io.Copy` append unbounded output
  without ever calling the bounded `Write`. A standalone check reproduced it
  (`err=<nil> tooLarge=false len=4198400` against a `4 << 20` limit); the fix adds an explicit
  `ReadFrom` that routes through `Write`, and the pin now reddens when that method is removed
  (§7, M-A06b). The same predicate is used by the accounts and systemd callers.

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

### Fix-stage rows (2026-11-26)

The delivered bytes are the restored state for every row below; the pre-fix bytes are
`498bff21` (`scripts/install-linux-owner.sh` `ab0780fc…`, `preflight_linux.go` `da5b2caf…`).
Shell rows run a stub parser that records every invocation and materializes the loaded-profile
interface; the driver's substitutions are: the PATH line, the five fixed paths
(`USERSNS_SYSCTL`, `USERSNS_PROFILE_FILE`, `USERSNS_PROFILES_ROOT`, `USERSNS_ABI_FILE`,
`USERSNS_TUNABLES_FILE`), the `/etc` and `/etc/apparmor.d` directory checks, the two
`mktemp /etc/apparmor.d/` spellings, one call to a probe hook at the publication site (and a
second at the delivered bytes' post-readback hash check), and a probe dispatch that calls the
delivered functions under the delivered exit trap (the trap body is extracted verbatim from the
same source file). Driver `15fdf686…`, stub `4fe4e281…`, built copies `7e5d4bf1…` (delivered)
and `adc21ea1…` (pre-fix); all scratch paths under a disposable root. 20 rows, 0 FAIL.

| id | mutation or before-state | observed red (verbatim row) |
| --- | --- | --- |
| M-A01 | the pre-fix bytes over a published file whose bytes differ from the fixed text | `install-linux-owner: published AppArmor userns profile digest differs from the inspected stage` — no remedy named (R4); the delivered bytes print `... differs from the fixed text; re-run with -remove-userns-profile ...` and never load (R3) |
| M-A01b | an unusable published path (a directory in its place) | delivered: refused before the load, remedy named (R6c) |
| M-A02 | the pre-fix bytes with the published path made unusable only after the load | `published AppArmor userns profile is not a regular non-symlink file`, the run had loaded (`--replace` in the parser log) and no `-R` was issued (R6b); the delivered bytes unload what the run loaded and publish nothing (R6) |
| M-A02b | the delivered bytes with the staged text changed after the load | explicit rollback unloads through a freshly printed declaration and publishes nothing (R7); with a verified published file present the rollback reloads that file once (R8) |
| M-A03 | the pre-fix bytes with a missing include file | loads and publishes `apparmor_userns=per_program` (R5b); the delivered bytes refuse naming the include file (R5) |
| M-A04 | the pre-fix bytes with an invisible policy interface, no parser | prints the removed sentence and exits 0 (R11); the delivered bytes exit 1 with `... the removed state cannot be proven` |
| M-A04b | the delivered bytes with a policy interface that is a file, and with a profile that stays loaded | `is not a readable directory` (R12) and `is still loaded` with the published file kept (R13) |
| M-A10 | the pre-fix bytes with a foreign declaration at the published path | `apparmor_parser -R <published path>` is the unload argument (R14); the delivered bytes pass a freshly printed fixed declaration (its sha256 equals the fixed text) and never the published path, while a verified published file keeps the fast path (R15) |
| M-A05 | the fixture's log pin and the local sysctl read | the pin accepts a `userns=per_program` log and refuses a `not_needed` log (R16); this host has no restriction file, so the fixture's read arm fails closed here (R17) and only the runner exercises its accepting arm |
| M-A06a | `command.Env = []string{...}` deleted from the delivered source | `preflight_linux_test.go:1491: the parser environment leaked the canary: stat error = <nil>` (`TestLinuxAppArmorCompileRows` FAIL) |
| M-A06b | the `ReadFrom` override deleted from the delivered source | `preflight_linux_test.go:1511: a parser emitting more than the compile bound must be refused: unavailable = ""` (FAIL); the standalone check on the pre-fix predicate printed `err=<nil> tooLarge=false len=4198400` |
| M-A03-Go | the `verifyLinuxAppArmorIncludeClosure` call deleted from the delivered source | `preflight_linux_test.go:1349: missing include file: present = true, valid = true; want true, false` plus the writable-file and writable-directory rows (`TestLinuxAppArmorHostStateRows` FAIL) |
| M-A09 | the decision reset deleted from `VerifyLinuxOwnerInstallation` | `preflight_linux_test.go:1568: after a failed verify the decision = "per_program", want the reset "not_needed" token` (FAIL) |
| M-A07 | the pre-fix preflight package test run as the unprivileged user | `preflight_linux_test.go:1333: the parser identity and directory checks require root` → `--- SKIP: TestLinuxAppArmorCompileRows`; the delivered bytes PASS the same row |
| M-A08 | `LinuxAppArmorUsernsNotNeeded` renamed in the delivered source on the non-Linux build | `preflight_unsupported_test.go:30: LinuxOwnerAppArmorUsernsDecision() = "not_needed_v2", want the recorded not_needed token` (FAIL; in-place change restored to `e53a5a81…`) |

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

### Fix-stage deltas (post-apply; the review's STD/SPEC findings)

Every delivered file below is the §1 revision; the left column is the apply-stage revision this
fix round starts from. No profile-text byte, fence, credential readback, census count, gate byte
or existing test changes: the fixes strengthen checks or name remedies.

| file | apply-stage sha256 | delivered sha256 | delta |
| --- | --- | --- | --- |
| `scripts/install-linux-owner.sh` | `ab0780fc…` | `8d61c5ba…` | A01: the published-bytes refusal names `-remove-userns-profile` itself (the shared check's message names no remedy) and the usage text says so; A02: the staged-hash and publication arms roll back explicitly, the exit trap performs the full rollback (unload plus the verified-published reload) and the loaded flag now stays armed until publication succeeded; A03: `verify_userns_include_closure` checks both include files and their directories before any load; A04: removal refuses when the policy interface is invisible or not a directory and the fixed declaration could not be unloaded; A10: every unload candidate must byte-equal the fixed text |
| `scripts/ci/linux-v2-native-acceptance.sh` | `e85c1c32…` | `4f14046a…` | A05: the timeout-safe glob read of the restriction asserts `1` before anything is installed, and both captured logs must carry a line ending `userns=per_program`; A07: the seven AppArmor preflight tests run as root in the guest through `assert-go-test-json.py` (run-and-pass exactly once; a skip fails) |
| `daemon/internal/linuxpreflight/preflight_linux.go` | `da5b2caf…` | `50f56f3f…` | A03: `verifyLinuxAppArmorIncludeClosure` and `verifyLockedDirectory` cover the include closure; A06: `exec.CommandContext` with a 10s deadline, a sanitized environment and a bounded stdout/stderr, plus the `ReadFrom` override that makes the bound actually bind; A07: the identity seam (`linuxAppArmorInstallerUID`/`GID`, production `0:0`); A08: the vocabulary moved to the build-tag-free file; A09: the decision token resets at the entry point |
| `daemon/internal/linuxpreflight/preflight_linux_test.go` | `29dcfdb2…` | `62a1bd7a…` | A03 rows: missing/writable include file, writable include directory, restored closure; A06 pins: the environment canary (with its direct control) and the oversize output; A07: the rows run unprivileged through the seam (no `Geteuid` gate), the real parser is copied into `t.TempDir()`, and the parser identity plus its world-writable directory are pinned; A09 pin: a failed verify resets the decision; SPEC-A01 row: the compiled policy hash changes with the attachment path and with the profile name |
| `daemon/internal/linuxpreflight/preflight_unsupported.go` | `b7aaf81f…` | `bf616df6…` | A08: the stub returns the shared vocabulary token |
| `daemon/internal/linuxpreflight/preflight_unsupported_test.go` | `cb087240…` | `dfcb8084…` | A08: the non-Linux build pins the recorded token and the shared constant literally |
| `daemon/internal/linuxpreflight/apparmor_userns.go` | new | `e53a5a81…` | A08: the closed vocabulary lives outside the `linux && amd64` build tag, so both builds share it |
| `docs/runbooks/control-plane-and-daemon.md` | `06b6ae87…` | `49a61eb7…` | A01/A03/A04/A10 documentation: the remedy-naming refusal, the include-closure requirement, the fixed-text-only unload, and refusal instead of a removed claim when the interface cannot be read |

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

Fix-stage additions to the same oracle: the fixture now asserts the decision instead of only the
exit status, so the runner row is a grep (`userns=per_program$`) in both captured logs, and a
sysctl-`0` host can no longer pass the acceptance with the mechanism skipped. The refusal rows
that belong to the new guards are the shell probe's (evidence §7) plus the AppArmor decision
rows (21, §6); the closure rows are unit rows (§6/§7) because a runner cannot present a writable
`/etc/apparmor.d` closure without mutating the host.

## 10. Gates (commands, counts, exit codes)

Re-run at the delivered bytes (delivered-file digests in §1; the run-3 binding is §8):

- `bash -n` on the two changed bash scripts, `dash -n` and `sh -n` on the POSIX installer:
  exit 0 each.
- `shellcheck --severity=warning` base-vs-delivered finding-set equality: installer 2/2,
  fixture 16/16 by the apply stage's default-format invocation, base-rootfs 0/0 (no new
  findings; sets compared with `diff` after masking
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
- Anchors re-measured by content at the delivered bytes (each group cited by its §1 digest, all
  measured after the fix-stage edits): installer `8d61c5ba…`
  `USERSNS_PROFILE_FILE=/etc/apparmor.d/symmetry-linux-owner-userns` :80, `USERSNS_ATTACH` :81,
  `USERSNS_ABI_FILE`/`USERSNS_TUNABLES_FILE` :84-85, `print_userns_profile` :87, usage
  paragraph :46-57, `verify_published_file` :327, `verify_existing_or_absent` :345,
  `publish_or_verify` :382, `read_userns_restriction` :494, `verify_userns_include_closure`
  :510, `resolve_apparmor_parser` :537, `verify_userns_profile_load` :568,
  `unload_userns_profile` :602, `rollback_userns_profile` :621, `install_userns_profile` :640,
  `remove_userns_profile` :696, `cleanup` :883; preflight `50f56f3f…` boundary :45,
  `VerifyLinuxOwnerInstallation` :67 (reset :68), `verifyInstalledLinuxOwnerAssets` :141, the
  seam block :203-211, `linuxAppArmorProfileText` :223, `decideLinuxAppArmorUserns` :256,
  `readLinuxAppArmorUsernsRestriction` :312, `verifyLinuxOwnerAppArmorProfileFile` :378,
  `verifyLinuxAppArmorIncludeClosure` :418, `verifyLockedDirectory` :447,
  `verifyLinuxOwnerAppArmorUserns` :467, `compileLinuxAppArmorProfile` :500,
  `boundedLinuxOutput` :954, `ReadFrom` :970; `apparmor_userns.go` `e53a5a81…` vocabulary :5-17;
  preflight test `62a1bd7a…` rows :1084, :1146, :1181, :1237, :1401, :1517, :1560;
  `preflight_unsupported.go` `bf616df6…` stub :31 and its test `dfcb8084…` pins :29-34;
  `main.go` `ad8547f1…` `apparmor_userns=` :266; fixture `4f14046a…` capability :967, `shared`
  group guard :1556, restriction read :1647-1651, securityfs guard :1652-1655, installer run
  :1660, installer-log grep :1672, `-verify-linux-owner-install` :1687 with its grep :1693,
  root preflight rows :1700-1730; rootfs-base `c8e49724…` package :47, mask :70-72; runbook
  `49a61eb7…` section :264; `daemon/native/linux-gate/**` byte-identical to `61399984`
  (`git diff 61399984 -- daemon/native/linux-gate/` empty).
- Apply-stage re-runs (2026-11-26, post-apply bytes; §8 delta table): `bash -n`, `sh -n` and
  `dash -n` exit 0; shellcheck finding sets identical to the pre-apply bytes (installer 2,
  fixture 16 by the apply stage's default-format invocation); `gofmt -l .` empty; `GOOS=linux go vet` and `GOOS=windows go vet` exit 0; WSL
  Ubuntu-24.04 `go test -count=1 ./internal/linuxpreflight/ ./cmd/symmetry-daemon/` `ok` as the
  unprivileged user and as root (all five `LinuxAppArmor*` rows PASS as root, including the new
  literal stanza pin); C1-C7 identical (§4); the changed installer arms re-pinned by a local
  root probe, 18/18 rows, driver sha256
  `f0d40c7076fcee65a0e3973715f2cf6bf2c061064db54b56a2ceba1a2d72cdee` (row output and
  substitutions in the apply stage answer).
- Run 3's own gates are the two acceptance case sets and the in-guest install/verify rows.

### Fix-stage gates (2026-11-26, at the §1 digests)

- Shell syntax and shellcheck, each script compared against its own pre-fix bytes with the same
  command (`shellcheck --severity=warning -f gcc <script>`, finding text normalized by stripping
  the `file:line:col` prefix, then `sort -u` and `diff`): `sh -n` installer exit 0, `dash -n`
  installer exit 0, `bash -n` fixture exit 0, `bash -n` rootfs-base exit 0; finding sets
  installer 2 = 2, fixture 15 = 15, rootfs-base 0 = 0, delta 0 in all three (the delivered
  installer's two warnings are the pre-existing `SC2034` rows at `:282` and `:805`).
- Go: `gofmt -l daemon/internal/linuxpreflight daemon/cmd/symmetry-daemon` empty;
  `GOOS=linux GOARCH=amd64 go vet` and `GOOS=windows GOARCH=amd64 go vet` on both packages exit
  0; `go test -count=1 -v ./internal/linuxpreflight/` on the Windows host (the non-Linux build,
  which now compiles the shared vocabulary file) `ok`.
- WSL Ubuntu-24.04, parser 4.0.1, `go test -count=1 ./internal/linuxpreflight/
  ./cmd/symmetry-daemon/`: `ok` as `accept` and `ok` as root; `go test -v -run '^TestLinuxAppArmor'`:
  7 PASS / 0 SKIP in both identities (§6).
- The stub-parser driver: 20 rows, 0 FAIL, exit 0 (§7).
- Censuses: §4 counts re-run at the delivered bytes (C1 3, C2 1, C3 3, C4 16 in 11 files, C5 4,
  C6 0, C7 18 in 8 files, C8 3, C9 7/1, C10 9, C11 0, C12 0).
- `python3 scripts/ci/test-omo-control-native-evidence.py`: `Ran 15 tests`, `OK`, exit 0.
- A scratch copy committed at the delivered bytes (then discarded): `python3
  scripts/ci/check-record-cleanliness.py --base 4440c5c0 --head HEAD` exit 0 (`1 changed
  record(s) pass the root-only check`) and `python3 scripts/ci/check-citation-existence.py
  --base 4440c5c0 --head HEAD` exit 0 (`2 changed document(s) pass the path and symbol check
  (656 path citation(s))`; the notes name pre-existing gitignored citations).
- `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only` on every changed
  document (`docs/runbooks/control-plane-and-daemon.md`, `docs/TODO.md`,
  `docs/archive/CLOSED-TODO.md`, this record, the plan): exit 0 for `docs/TODO.md`, this record
  and the plan. The runbook and `docs/archive/CLOSED-TODO.md` refuse on host-path spellings that
  predate this change and lie outside its lines (a var-rooted example value in the runbook and
  quoted Windows/POSIX specimens in the archive); they are neither introduced nor
  rewritten here, and the enforceable gate — `check-record-cleanliness.py` over the range, which
  inspects the changed lines — passes (bullet above).
- Branch-diff hygiene: `git diff --check origin/main...HEAD` exit 0; the branch-diff host-path
  scan (§4 C12, described in prose) has no hits.
- The duplication gate (`python scripts/ci/jscpd-changed.py --base 4440c5c0 --head HEAD`) could
  not run here: the pinned devDependency cannot be installed in this environment. Two
  `pnpm install --frozen-lockfile` attempts (resolved 490, reused 490) both ended in
  `ERR_PNPM_RESOLVING_NPM_RESOLVER_NETWORK_ERROR` with `HandshakeFailure` after the supply-chain
  policy check rejected `24/592` entries — first on
  `https://registry.npmjs.org/@oxc-parser%2Fbinding-linux-arm-gnueabihf`, then on
  `https://registry.npmjs.org/@eslint%2Fobject-schema`. A threshold-matched replacement proof ran
  over the range's added lines (scratch script, described method: whitespace-free windows of 10
  consecutive added non-blank lines, searched across the delivered tree at the gate's
  `--min-lines`). First run: 1 duplicate pair (`preflight_linux_test.go:1364` and
  `:1513`, a parser-discovery block repeated in two new tests) — hoisted into
  `firstInstalledApparmorParser`; the final run over the delivered bytes reports 38 changed
  files and 0 findings (the repository's own `scripts/ci/test-jscpd-changed.py` cannot pass here
  either: 30 of its 32 pinned-dependency fixtures fail with the dependency absent). The real
  gate remains the CI contracts job's own run.

## 11. Reviews (rounds 1-3) and findings' dispositions

Round 3 (the review over `4440c5c0..498bff21`, run `20261126-021429-32253`; the conductor ruled
on its findings on 2026-11-26). STD axis dispositions:

| finding | disposition |
| --- | --- |
| STD-A01 (a mismatched published file refuses without naming a remedy) | fixed: the userns re-run checks the published bytes itself and fails with `re-run with -remove-userns-profile ...`; the runbook and the usage text match (probe R3/R4, differential R4) |
| STD-A02 (a failure between the load and publication leaves the policy loaded) | fixed: the staged-hash arm rolls back explicitly, the exit trap performs the full rollback and the loaded flag stays armed until publication succeeded (probe R6, R7, R8; differential R6b) |
| STD-A03 (the include closure is unchecked in both implementations) | fixed: `verifyLinuxAppArmorIncludeClosure` in the preflight (owner, mode, link, ACL, A-read, B-no-write, plus each include directory) and `verify_userns_include_closure` in the installer (owner, mode, link, plus each directory); pinned by the unit rows and probe R5/R5b. The installer has no ACL reader; the preflight's refusal is the ACL authority (residual §12) |
| STD-A04 (removal claims success while the state is undecidable) | fixed: an invisible interface that could not be unloaded through the fixed declaration, and an interface that is not a directory, both exit non-zero (probe R11-R13) |
| STD-A05 (the fixture asserts only exit status) | fixed: both logs must carry `userns=per_program` and the restriction must read `1` before anything is installed (probe R16/R17; runner arms in the next acceptance run) |
| STD-A06 (unbounded, environment-inheriting parser child) | fixed: `exec.CommandContext` with a 10s deadline, a sanitized environment and bounded stdout/stderr; the bound needed an explicit `ReadFrom` (the embedded `bytes.Buffer` promoted one that bypassed `Write`) and the pins for the leak and the oversize arms were added (§6/§7) |
| STD-A07 (the compile-order pin runs in no checked-in gate) | fixed: the identity seam lets every AppArmor row run unprivileged in the `daemon` job (7 PASS, no skip), and the same rows run as root in the fixture's guest through `assert-go-test-json.py` (a skip fails). No workflow change |
| STD-A08 (a free literal in the non-Linux stub) | fixed: the vocabulary moved to the build-tag-free `apparmor_userns.go`; the stub and the non-Linux test pin the token |
| STD-A09 (a failing verify can report a stale decision) | fixed: `VerifyLinuxOwnerInstallation` resets the token before its first check (mutant M-A09 red, test `TestLinuxAppArmorDecisionResetsPerVerify`) |
| STD-A10 (the unload trusts the published file's declaration) | fixed: every unload candidate must byte-equal the fixed text; the fast path survives for verified bytes (probe R14/R15) |

SPEC axis: SPEC-A01 was **ratified by the conductor with conditions** (`conductor ruling
2026-11-26`, recorded here as the plan's §11 also records it): the loaded policy's `raw_sha256`
equality with the compiled fixed text stands as the exact-attachment proof only together with
the attachment-sensitivity row, which now exists locally (§6: attachment- and name-changes both
change the compiled hash) and as a Go test row; its runner row belongs to the conductor's Bind
run. SPEC-A02: the TODO and CLOSED-TODO sentences now bind the wall rows to run 3's head
(`ce3c30b9`), not to the delivered bytes; the Bind run re-binds the delivered bytes. SPEC-A03:
the conductor ratified the three added files (`preflight_unsupported.go`, the rootfs-base mask
lines, the runbook). SPEC-A04: both embedded profile-text copies are deleted; the text is cited
through `-print-userns-profile`, the §1 digest and `TestLinuxAppArmorProfileTextMatchesTheInstaller`.

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
- The installer's include-closure check reads owner, mode and link count (POSIX sh, no ACL
  reader in the pinned toolchain); the ACL arm lives in the preflight, which refuses such a host
  at every daemon start, so a non-root writer who wins an ACL grant between the installer's
  check and its load cannot make the state persist. This extends the earlier ACL-blindness
  residual to the closure files rather than removing it.
- The parser deadline (10s) has no dedicated pin: producing a timeout costs at least the
  deadline plus the fallback attempt in test wall time, and the arm is exercised by
  construction (`exec.CommandContext` with `context.WithTimeout`) on the service and CLI paths
  alike. The output bound and the sanitized environment do have pins (mutants M-A06a/M-A06b).
- The fixture's read of the restriction (`R17`) can only fail closed on this host, which has no
  restriction file; its accepting arm runs on the runner, where the same value also has to
  satisfy the two log greps.
- SPEC-A01's runner row (the attachment-sensitivity probe) and SPEC-A02's re-binding of the
  delivered bytes belong to the conductor's Bind run on the PR head; until it lands, the
  delivered bytes are bound by the fix-stage gates above and the run-3 rows stay bound to
  `ce3c30b9`.
- `scripts/ci/omo-control-native-evidence.py`'s `SOURCE_PATHS` freeze does not list
  `daemon/internal/linuxpreflight/apparmor_userns.go`; adding the new file to that list is a
  candidate for the next record-hygiene change outside this goal's owned set.
- The duplication gate itself is unrun in this environment (the registry fetch above); the
  replacement proof is narrower than the gate's run-based rule, so the CI contracts job's run of
  `scripts/ci/jscpd-changed.py` stays the binding check for this range.
