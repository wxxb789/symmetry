# rf-acc-synthetic-cleanup-release — evidence record

Contract: `goal-rf-acc-synthetic-cleanup-release.md` (GDD state root, external to the repository).
Base revision `ff0975a714d64e926190e3a937496a63e0d5ec86` (`main`, the contract's dispatch gate);
branch `refactor/rf-acc-synthetic-cleanup-release`. Plan:
`docs/plans/2026-11-26-rf-acc-synthetic-cleanup-release-plan.md`. Every number below is measured at
the delivered bytes unless a row names another revision; the delivered bytes are commit `90157ff4`
plus the PR #302 round-1 ruling fixes (V1-V10) and the standing-rules update (2026-11-26), applied
as the worktree state whose sha256 rows are in §1 (the conductor folds them into the branch).

## 1. Subject bytes (sha256)

| File | Dispatch head `ff0975a7` | Delivered | Change |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `d120677d8718d0ad346d0fdfc9ef6136234d1b48071d77e3506aac4c844f01e6` | `1e7a5d3a05d444ccd1b73e52df943c19978eb7b4faf921ef2e49377a6d378dcb` | +27/−1 |
| `daemon/e2e/production_linux_v2_cleanup_worker_shell_test.go` | absent | `e9e95d049df3dab8e6f5a78a7d28fd2fbed04cef1291994fb67bc6d4f5d194d9` | new, 94 lines |
| `daemon/native/linux-gate/protocol/native_image_pin_linux_test.go` | absent | `59fbbb43f27291fd618dc28f570dddf96c7353133aa231c267819e9932f1883e` | new, 128 lines (round-2: V5/V9 guards, no function doc comments) |
| `daemon/native/linux-gate/protocol/proc_identity_linux.go` | `b210f968bb5d0e52095e46142b37c5fdba43dd39b13e0fe8c9ed66999a0f2ade` | identical | untouched (production) |
| `daemon/internal/platform/helper_v2_protocol.go` | `4ac73de34f901c91a3cef10e3704e9530f520db321deb6af4e38669bc6eb9d75` | identical | untouched (production) |

No production byte, fixture script, workflow or `gate.c` byte changed; the worktree change set
(`git diff origin/main`, merge-base `b27ea691`) is exactly the three delivered files above plus the
plan, this record and the two TODO files.

## 2. Root cause (Stage 1)

### 2.1 The chain, end to end

The controlled B worker is released through the real launcher/anchor/helper at
`production_linux_v2_cleanup_witness_test.go:470` (dispatch head) as
`Program: "/bin/sh"`. The helper's one-use RELEASE runs `prepared.Release`
(`daemon/internal/platform/helper_v2_server.go:1147`), which pins the A-approved native image before
it builds the release packet: `pinNativeImage` (`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:928`
call; `daemon/native/linux-gate/protocol/proc_identity_linux.go:74`) walks every path component with
`O_PATH|O_DIRECTORY|O_NOFOLLOW` (`:128`) and opens the image itself `O_PATH|O_CLOEXEC|O_NOFOLLOW`
(`:79`), requiring regular/root-or-A-owned/non-set-ID/non-B-writable nodes
(`verifyTrustedImageDirectory:147`, `approvedNativeImageIdentity:159`). On the usrmerge guest this
refuses `/bin/sh` at the `/bin -> usr/bin` component. The refusal is a plain error, so
`NewReleaseSubStepError` never labels it, `linuxOwnerHelperV2ReleaseErrorDetailFor`
(`daemon/internal/platform/helper_v2_protocol.go:591`) leaves `Stage = "none"` (`:480`), `String()`
(`:486`) renders `stage=none`, the helper answers `release_outcome_unknown`
(`helper_v2_server.go:1200,:1211`), and the daemon wraps it
(`daemon/internal/execution/linux_owner_v2_linux.go:1066`) into the recorded stop.

The recorded stop (findings `main-measure-codex-synthetic.md` §4.2, run `45775969711` on
`ff0975a7`), verbatim:

```
production_linux_v2_cleanup_witness_test.go:499: start controlled B Git worker through the real Linux v2 launcher/anchor/helper: resume process "/bin/sh": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=none)
```

(the same leg stopped with the stage-less `(release_outcome_unknown)` detail in run `45573253632`,
recorded in `docs/evidence/rf-acc-guest-native-exec-2026-11-25.md` §9 and `docs/TODO.md`).

### 2.2 Discrimination (no other unlabelled producer fits)

Every other unlabelled pre-packet refusal in `PreparedGate.Release` is program-independent
(`command.Validate`, `validateReleaseOwnerPipe` `wire_linux.go:385`, `monotonicDeadlineNS`
`wire_linux.go:347`, the owner-pipe duplication, `encodeGuardedRelease` `wire.go:699`) and is green
on the legs that release other images; every post-packet gate failure is labelled with a sub-step
and arm (`stage=clone_bootstrap arm=bootstrap_attempt guest_stage=gate_drop_bounding_set`, runs
`45644664168`/`45601287283`, and `stage=native_exec`, run `45557621798`), which `stage=none`
excludes. The only leg-specific input is the leg's own image path: a repository census finds this
witness as the only e2e leg that passes an explicit `Program:` (`production_linux_v2_cleanup_witness_test.go:470`);
every other releasing leg takes its image from the fixture profiles
(`scripts/ci/linux-v2-native-acceptance.sh:1097` `/usr/local/lib/symmetry-pi/pi`,
`:1109` `/usr/local/bin/codex`, `:1111` `/usr/local/bin/claude`, `:462` `/usr/local/bin/opencode`),
all installed root-owned regular files whose staging is digest- and mode-checked.

### 2.3 Measured on the guest-identical userspace (red-first characterization)

`path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec` (the Ubuntu 26.04 LTS resolute replica kept by
`rf-acc-b-process-readback`; `/bin -> usr/bin`, `/bin/sh -> dash`, `dash 0.5.12-12ubuntu3`,
uutils `coreutils 9.5-1ubuntu2+0.0.0~ubuntu25`, `/usr/bin/dash` root:root 0755). The new protocol
probe run there as the guest's A shape (`setpriv --reuid=52000 --regid=52000 --clear-groups`,
`HOME=path-sha256:ec3459ebf2a5788e2ff90499117aae5439a79dc15090561bb8b2a50bf9cc5c83`), verbatim:

```
native image pin accepted the symlink-free image "path-sha256:4ab254c50f0019261517517a4446293eb79a3a18558d619c132065854ddebdda"
native image pin refusal for "path-sha256:f4c7b0b4eecb57e938c384b7ee7e217f424efb33638dc3701162fce054145ed2": open native-image parent component "linked" without following symlinks: not a directory
native image pin refusal for "path-sha256:7bba070e2043001d161cb3d24f44853fb0dfcfac5f06b23d7083ce656d9bff1a": native image "path-sha256:7bba070e2043001d161cb3d24f44853fb0dfcfac5f06b23d7083ce656d9bff1a" is not A-approved: image must be a root/A-owned regular executable without set-ID bits or B write access
native image pin accepted the resolved system shell "/usr/bin/dash"
native image pin refusal for "/bin/sh": open native-image parent component "bin" without following symlinks: not a directory
```

`REPLICA_RC=0` (the probe asserts the refusal, the detail and that no `ReleaseSubStepError` labels
it). The same probe on the WSL Ubuntu-24.04 host: `ok … 0.022s`, exit 0, with `resolved system
shell "/usr/bin/dash"`.

### 2.4 The `:488` stop is fallout, not a second defect

`production_linux_v2_cleanup_witness_test.go:488` (`terminate unfinished controlled B process …:
Linux v2 helper could not prove the exact run leaf stopped: dial unixpacket: connect: connection
refused`) is the test's second stop attempt after the failed release: the Start-failure path already
stops the containment (`daemon/internal/execution/runner.go:1280` `cleanupFailedStartStarted` →
`Process.Terminate`), the helper retires its endpoint afterwards, and the returned `startErr` carries
no joined termination error (the recorded `:499` line is the release error alone), so the daemon's
own stop succeeded. The line is expected to disappear with the release, not fixed by an assertion
change.

## 3. The change (Stage 2, test side only)

- `linuxV2CleanupWorkerShell = "/bin/sh"` (witness const, 2-line why comment);
  `linuxV2CleanupApprovedWorkerProgram` (`filepath.EvalSymlinks` + `Lstat`) returns the resolved,
  symlink-free `/usr/bin/dash` on the guest and refuses a non-canonical path, a missing/broken
  symlink, a non-regular node, a non-executable file, group/world write bits and set-ID/sticky bits
  with a named error; the invocation now releases `Program: workerProgram`.
- New probe `daemon/native/linux-gate/protocol/native_image_pin_linux_test.go`: a controlled trusted
  staging tree (positive pin, planted parent symlink, planted final symlink, each refusal asserted
  for its detail and for the absence of a `ReleaseSubStepError`), the `/bin/sh`-vs-resolved
  asymmetry row, and skip-with-reason guards for an unwritable-but-trusted staging base (V5) and a
  resolved shell whose parent chain is not a trustable pin chain (V9), both added in the round-2 fix.
- New probe `daemon/e2e/production_linux_v2_cleanup_worker_shell_test.go`: the fixture shell's own
  resolution (logged, so every run records the resolved image), the accepted symlink shapes, and the
  refused shapes.
- Round-2 standing comment rule (2026-11-26): the changed/new functions and tests carry no doc
  comments; the only added comment is the 2-line why comment beside the `linuxV2CleanupWorkerShell`
  fixture value.
- No assertion was weakened, deleted or renamed: the witness diff is `+27/−1`, and the single
  deletion is the `Program: "/bin/sh"` line replaced by `workerProgram`; no cleanup, release or
  containment assertion appears in any diff hunk.

## 4. Check before the change — replacement proof

The clause test is the `synthetic_goal_cleanup` case set (installed owner, disposable guest,
Control); it cannot run on this host, so the red-first evidence is (i) the recorded credentialed
stop verbatim (§2.1) and (ii) the replica characterization of the production pin on the same
userspace (§2.3), with (iii) the probe rows and mutants (§6) holding the rules after the change.

## 5. Local gates

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `(cd daemon && gofmt -l .)` (WSL) | 0 | no file listed |
| linux vet | `(cd daemon && GOOS=linux go vet ./...)` (WSL) | 0 | clean |
| windows vet | `(cd daemon && GOOS=windows go vet ./...)` (WSL) | 0 | clean |
| comment-cap | `python …/comment-cap.py <scratch clone> origin/main`, round-2 code bytes on a scratch-clone commit of `90157ff4` + the round-2 edits (pin-probe guards, comment removal) | 0 | no over-cap block; round 1 scanned the other two code files (3 files, 251 insertions, 1 deletion) and shortened the first draft's 4-line/3-line doc comments to 2 lines |
| protocol probe (WSL root) | `go test -count=1 -run '^TestNativeImagePin' ./native/linux-gate/protocol/` | 0 | both rows pass |
| e2e probe (WSL root) | `go test -count=1 -run '^TestLinuxV2CleanupApprovedWorkerProgram$' ./e2e/` | 0 | resolves `/bin/sh` to `/usr/bin/dash` |
| protocol probe (WSL `accept`) | same, as `sudo -u accept -i` | 0 | `ok … 0.024s` |
| e2e probe (WSL `accept`) | same, as `sudo -u accept -i` | 0 | `ok … 0.026s` |
| replica run, guest A shape | `chroot path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec setpriv --reuid=52000 … protocol-pin.test -test.run '^TestNativeImagePin'` | 0 | §2.3 rows |
| replica run, guest A shape | same replica, `e2e-probe.test -test.run '^TestLinuxV2CleanupApprovedWorkerProgram$'` | 0 | `controlled B worker shell /bin/sh resolves to the approved image /usr/bin/dash` |

Round-2 fix (V1-V10): the two protocol-probe rows and the e2e probe were re-run at the final bytes
(root and `accept`, compiled test binaries) with the same exit-0 results; the comment-cap row above
is the round-2 run; the two guard demonstrations are in §6.

### 5.1 Full-suite runs and environment attribution

Re-measured at the round-2 delivered bytes (after the comment strip); the base-vs-head rows run the
same packages at the dispatch head `ff0975a7`.

| Run | Command | Exit | Rows and attribution |
| --- | --- | --- | --- |
| Windows full suite | `(cd daemon && go test -count=1 ./...)` on the Windows host, Go 1.27.4 | 0 | 27 `ok` + 3 `[no test files]`, 0 failures (the linux/amd64 probe files are not built on Windows; no-test rows: `e2e/preauthority-sentinel`, `internal/linuxreadback`, `internal/linuxreadback/cmd/linux-v2-readback-reader`) |
| WSL full suite, `accept` umask 022 | `sudo -u accept -i bash -lc 'umask 022; cd daemon && go test -count=1 ./...'` | 0 | 31 `ok` + 3 `[no test files]`, 0 failures (no-test rows: `cmd/symmetry-linux-clone-bootstrap`, `e2e/preauthority-sentinel`, `internal/linuxreadback/cmd/linux-v2-readback-reader`) |
| WSL full suite, root | `(cd daemon && go test -count=1 ./...)` as root | 1 | 30 `ok` + 3 `[no test files]`; 13 failing rows, all `daemon/internal/platform` (`wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`, `anchor_shape_table_test.go:251` etc.). The rows require a non-root A; environment, not code |
| WSL full suite, `accept` umask 002 | same as `sudo -u accept -i` | 1 | 28 `ok` + 3 `[no test files]`; 53 `--- FAIL` lines (31 test rows) across `daemon/internal/linuxpreflight` (2 AppArmor host-state rows), `daemon/internal/platform` (8 supervisor-witness rows) and `daemon/internal/workspace` (21 clone rows). Measured cause for platform/workspace: Go 1.27's `t.TempDir()` creates its numbered directory with `os.Mkdir(dir, 0o777)` (`src/testing/testing.go:1634` at the `GOTOOLCHAIN=go1.27.0` toolchain's `GOROOT`) and `accept`'s umask is `0002`, so trees are `0775` and the production group/world-write rules refuse them (`containment_supervisor_linux_test.go:919` `witness marker parent is not a safe witness directory … group- or other-writable`; `linux_clone_linux_test.go:1465` etc.) |
| base-vs-head, `accept` umask 002 | the same three packages at the dispatch head `ff0975a7` (`git archive ff0975a7 daemon`, ext4 `/tmp`) as `accept`, umask 002 | 1 | the identical `internal/platform` (8 rows) and `internal/workspace` (21 rows) failures at base; `internal/linuxpreflight` fails there with 4 host-AppArmor-state rows (a superset of the head's 2, so the exact rows vary with the host's AppArmor state); the class is pre-existing and unrelated to this change |
| base-vs-head, root | `./internal/platform` at `ff0975a7` as root | 1 | the identical 13 uid-refusal rows at base |

## 6. Probes and mutants

Probe rows (delivered bytes): protocol — controlled positive pin, planted parent symlink
(refusal text `native-image parent component "linked"`), planted final symlink (`is not A-approved`),
each asserting no `ReleaseSubStepError`; system-shell asymmetry row (skip-with-reason when `/bin/sh`
is not a symlink or the resolved shell's parent chain is not a trustable pin chain). e2e — the
fixture shell resolves and logs `/usr/bin/dash`; three
accepted shapes (real file, symlinked directory component, symlinked file); eight refusals (missing,
broken symlink, non-canonical, `/`, non-executable, set-ID, group/world-writable, directory).

The first probe run caught a defect in the new helper itself: `info.Mode().Perm()&0o7000` is always
zero because `Perm()` carries only the permission bits, so the set-ID row stayed green
(`worker program for "…/setid-shell" = "…/setid-shell", err=<nil>, want a refusal`, `E2E_RC=1`);
the check now reads `os.ModeSetuid|os.ModeSetgid|os.ModeSticky` and M-P4 pins it.

Guard evidence (round 2, at the final bytes, with the pre-fix bytes reddening for the same
environment): V5 — HEAD bytes as `accept` with a trusted but unwritable `HOME=/root` →
`FAIL native_image_pin_linux_test.go:21: create a trusted native-image staging directory: mkdir
path-sha256:…: permission denied` (exit 1); final bytes →
`SKIP … :17: HOME "/root" is a trusted native-image pin base but not writable for staging: …`
(exit 0); final bytes with a writable `HOME` → both rows pass. V9 — a private mount namespace with
`/usr/bin` bind-mounted to a mode-1777 directory holding a copy of `dash` as `sh`: HEAD bytes →
`FAIL :66: resolved system shell "/usr/bin/sh" must pin: native-image parent component "bin" is
not A-controlled and non-B-writable` (exit 1); final bytes →
`SKIP :61: resolved system shell "/usr/bin/sh" is not A-approvable in this environment: /usr/bin
is group/world writable or not traversable` (exit 0). Both guards skip only where the environment
itself cannot satisfy the pin chain, and the assertions the rows carry on supported hosts are
unchanged (probe runs above).

Adversarial satisfaction (standing rule, round 2). The case-set observation cannot be satisfied by
an unrun or skipped witness: each case-set invocation is gated by `scripts/ci/assert-go-test-json.py`
on exactly one run and one pass of the named test (`run=1 pass=1 skip=0`, `-count=1`), the script
fails the set on any other event set, and the workflow additionally requires the set's exit 0 and a
sanitized manifest; the Bind's log shows the witness failing, not a vacuous green. Protocol-probe
refusal rows cannot be satisfied by an unrelated syscall error: each row asserts its own refusal
detail (the `O_NOFOLLOW` parent-open text, `is not A-approved`) and the absence of a
`ReleaseSubStepError`, so an `EACCES`/`ENOENT` or a labelled error fails the row; the `accepted`
rows cannot be satisfied by a pin that bypasses the guarded mechanism, because the same test's
planted parent/final symlink rows require those refusals — a pin that accepted everything fails
them (M-P1/M-P2 invert exactly that). The e2e helper's accepted rows cannot be satisfied by a
passthrough that returns its input, because the symlinked fixture shapes require the returned path
to equal the separately `EvalSymlinks`-resolved target; its refusal rows require their named detail
strings and an empty return. The V5/V9 skips cannot mask a pin regression: both guards decide from
environment facts (`trustedNativeImagePinChain` `Lstat`s, a staging `MkdirTemp`) before any pin
call, so they are insensitive to `pinNativeImage`/`gate_lifecycle_linux.go` changes.

Mutants (applied to a scratch copy of the final bytes, run, then restored and re-checked with `cmp`
against a byte copy; the two production mutants mutate `proc_identity_linux.go` `b210f968…`,
restored byte-identical after each row):

| id | Mutation | Red probe (verbatim) | Exit | Restored |
| --- | --- | --- | --- | --- |
| M-P1 | `proc_identity_linux.go:128`: drop `O_NOFOLLOW` from the parent-component open | `native_image_pin_linux_test.go:42: native image pin accepted "path-sha256:bf8148640fd69d8ab2d66ec30a052eeeff2160ebd5675f3b529f7881dbf70dc4", want a refusal` | 1 | byte-identical |
| M-P2 | `proc_identity_linux.go:79`: drop `O_NOFOLLOW` from the image open | `native_image_pin_linux_test.go:48: native image pin accepted "path-sha256:c2d71f89a1ad1fd6d9f253ca14adb6c38d6f0c783aef49c17667dd30b19c0069", want a refusal` | 1 | byte-identical |
| M-P3 | witness helper: drop the `Perm()&0o022 != 0` clause | `production_linux_v2_cleanup_worker_shell_test.go:88: worker program for "…/world-writable-shell" = …, err=<nil>, want a refusal` | 1 | `1e7a5d3a…` |
| M-P4 | witness helper: `os.ModeSetuid\|os.ModeSetgid\|os.ModeSticky` → `0` | same row for `"…/setid-shell"` | 1 | `1e7a5d3a…` |
| M-P5 | witness helper: drop `!info.Mode().IsRegular()` | same row for `"…/directory-shell"` | 1 | `1e7a5d3a…` |
| M-P6 | revert half of the fix: `Program: linuxV2CleanupWorkerShell` | not runnable on this host (guest-only clause): the red evidence is the recorded `:499` stop (§2.1) and the replica refusal (§2.3) | — | — |

The rows cover the mode-predicate clause mutants and both pin opens. The helper's canonical-path and
exec-bit clauses are discriminated by the e2e refusal rows (the non-canonical detail row and the
`…/not-executable` row), and the helper's `os.Lstat` error branch is unreachable after a successful
`EvalSymlinks`; no mutant row is claimed for those three branches (PR #302 round-1 ruling V7).

CI reach (the mutant→CI map at the delivered bytes): both probe files build only under
`//go:build linux && amd64` and carry no `-run` gating, so the PR `daemon` job's race pass
(`go test -race -count=1 -timeout 600s -json ./...` on ubuntu-latest) and `coverage-daemon`'s
coverage pass execute the probes that M-P1…M-P5 redden, under the runner's non-root account; the
pin probe's trusted-base guard skips with a reason (never a false red) where that account's `$HOME`
is not a trusted image base or is not writable for staging (V5), the asymmetry row skips where the
resolved shell's parent chain is not trustable (V9), and `daemon-windows` does not build these
files. M-P6's red is
guest-only: the dispatch-only `linux-native-v2-acceptance` workflow runs the cleanup case set
(`TestProductionLinuxV2CloneCleanupAuthenticatedScratch`, mode `synthetic-cleanup`, as the A user
`symmetry-a`), whose red shape is the recorded stop (§2.1); no mutant row reddens outside CI, so no
oracle gap is recorded.

## 7. Ruling-item checklist (contract clause → disposition + evidence)

| id | Clause | Disposition | Evidence |
| --- | --- | --- | --- |
| C1 | Stage 1 root cause, named verbatim with file:line, candidates discriminated | done | §2.1-§2.2: pin refusal at `proc_identity_linux.go:128` (`/bin -> usr/bin`), unlabelled → `stage=none`; the fast stop and the absent `guest_stage`/arm exclude helper death and protocol mismatch |
| C2 | Compared with the green legs that release B through the same launcher | done | §2.2 census: every green leg's image is an installed root-owned regular file; the only explicit `Program:` in e2e is this leg's |
| C3 | Production defect → DECISION_NEEDED | refused (not a production defect) | §2.3 + plan §2.3: the pin's symlink-free rule is the A-side approval; all production profiles satisfy it |
| C4 | Stage 2 fix on the wrong side | done | §3: witness image choice only; `+27/−1`, no assertion touched |
| C5 | Never weaken the RELEASE-outcome resolution or any cleanup assertion | done | §3 last bullet; the helper, the detail vocabulary and every cleanup assertion are byte-identical |
| C6 | Fences and idempotency preserved | done | `daemon/internal/**` (non-test) and `gate.c` have no diff (§1) |
| C7 | Comments ≤ 2 lines, `go vet` compile-check, never `-o /dev/null` | done | §5 rows (comment-cap 0; both vet rows 0); standing rule 2026-11-26: no doc comments on the changed/new functions or tests, the only added comment is the const's 2-line why comment |
| C8 | Records rule: red mutants or greps, a red row per new branch; "delivered, pending the conductor's Bind" | done | §6 mutants M-P1…M-P5 (the mode-predicate clause mutants and both pin rules; the canonical and exec-bit clauses are discriminated by the e2e refusal rows), §2.2 greps, this record's wording and the CLOSED-TODO entry |
| C9 | FULL `go test ./...` on Windows plus WSL; comment-cap before the final re-measure | done | §5.1 (re-run at the round-2 bytes after the comment strip): Windows exit 0 (27 `ok` + 3 no-test-files), WSL as `accept` umask 022 exit 0 (31 `ok` + 3 no-test-files), both 0 failures; the root-run and umask-002 failures are base-vs-head attributed at the dispatch head `ff0975a7`; comment-cap exit 0 ran on a scratch-clone commit before the re-measure |
| C10 | Acceptance: the conductor's Bind runs `synthetic_goal_cleanup` once on the PR head | done (Bind met, V10; re-Bind required on the final comment-stripped e2e bytes) | Run `45791682543` on `90157ff4` (one run, `total_count=1`): the `:499 … stage=none` stop is gone — the worker is released as `/usr/bin/dash` — and the next stop, the labelled `clone_checkout` leg, is recorded verbatim in `docs/TODO.md` under `## rf-acc-synthetic-cleanup-release: residuals (2026-11-26)`; see §Bind (run-input determination) and `docs/archive/CLOSED-TODO.md` |

## 8. Scope, residuals and limits

Owned files changed: the witness test, the two new probe files, the plan, this record,
`docs/TODO.md` (carve + one new residual) and `docs/archive/CLOSED-TODO.md` (the moved entry). The
plan §6 states and selects the reading that lets the pin probe live in
`daemon/native/linux-gate/protocol/*_test.go` (it exercises the shared production helper this leg
failed on); the conductor confirmed reading (i) in the PR #302 round-1 ruling (V6, 2026-11-26), and
the probe is test-only.

Residuals: (i) the pin's refusals stay unlabelled, so a future leg with an unapproved image path
still stops as `stage=none` — recorded as a new `docs/TODO.md` entry under this goal's own
`rf-acc-synthetic-cleanup-release: residuals` section: the witness's resolution check refuses a
non-canonical image before the release, the pin probe pins the pin's refusal shapes, and a labelling
change lands at the `PreparedGate.Release` call site
(`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:928-932`), which the pin probe does not
exercise, so it needs its own `Release`-level row (V1); (ii) `shared`'s three owner-leg stops stay tracked in `docs/TODO.md`
(not this goal's surface); (iii) the uutils `AT_EXECFN`/fd-exec product constraint is unchanged and
not touched by this fix (`dash` is a single-purpose ELF); (iv) the WSL full suite needs a non-root
user with umask 022 on this host (Go 1.27 `t.TempDir()` × umask 0002 would otherwise trip the
production A-ownership rule in `internal/workspace` — pre-existing, base-vs-head attributed in
§5.1).

## Bind

Conductor's Bind on the delivered bytes: case set `synthetic_goal_cleanup`, one scratch push, one
run (run unit `45791682543`, workflow `Scratch containment goal-admission probe`, event push,
attempt 1, conclusion `failure`). Run head `5891e98b227ed608f73134060921c30326b9d913` = the PR
head `90157ff4` plus exactly the two scratch-only files (workflow sha256
`8beeadf6d00d73dac74a10a49722c9b767dab20010875938c8af5725e8fb389f`; shim sha256
`507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`, byte-identical to the
ratified copy); `total_count=1` at every read of the runs list (creation, immediately before the
deletion, and after it). The scratch branch was deleted and the temporary worktree removed; both
PR refs stayed `90157ff4`.

**The `:499` stop is gone; the next stop is labelled; the set is not green.**

- The run log (2,930 lines, sha256
  `cdab78b483f3476634f0c3ee4993ddff15e2b4ae4ee2deaba653de246dfcc3da`) carries **0** occurrences of
  `production_linux_v2_cleanup_witness_test.go:499` and **0** of `stage=none`; the worker is
  released as `/usr/bin/dash`, i.e. the pin accepted the resolved image. The next stop, verbatim
  (the first failing line):
  ```
  production_linux_v2_cleanup_witness_test.go:506: start controlled B Git worker through the real Linux v2 launcher/anchor/helper: resume process "/usr/bin/dash": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=clone_checkout arm=checkout_receipt wait_class=other guest_stage=seed errno=13)
  ```
  The release now reaches `clone_checkout`/`checkout_receipt` (`errno=13`, `guest_stage=seed`)
  instead of stopping before the packet; the recorded RunJournal projection at the stop is
  `{"local_state":"running","has_claim_grant":true,"has_clone_intent":true,"has_seed_receipt":true,"recovery_required":true}`.
- Every verified line that ran (7 rows, each `run=1 pass=1 skip=0`): `TestLinuxAppArmorUsernsDecisionRows`,
  `TestLinuxAppArmorProfileTextMatchesTheInstaller`, `TestLinuxAppArmorLoadedProfileScan`,
  `TestLinuxAppArmorHostStateRows`, `TestLinuxAppArmorCompileRows`,
  `TestLinuxAppArmorAttachmentSensitivePolicyHash`, `TestLinuxAppArmorDecisionResetsPerVerify`.
  `TestProductionLinuxV2CloneCleanupAuthenticatedScratch` failed
  (`--- FAIL: TestProductionLinuxV2CloneCleanupAuthenticatedScratch (1.74s)`), so the set produced
  no cleanup verify row.
- `synthetic-goal-cleanup.json` **did not export** and there is **no manifest**: `redacted
  evidence export failed: pi durability witness is unavailable: synthetic-goal-cleanup.json`; the
  close path retained the private fixture, and the evidence upload found no files (`##[warning]No
  files were found with the provided path … No artifacts will be uploaded.`). The run carries no
  `linux-v2-native-evidence-*` artifact; `Case set synthetic_goal_cleanup exited 1` and `Failed
  case sets: synthetic_goal_cleanup`.
- Acceptance-side artifact: `linux-v2-host-prerequisite-45791682543-1` (id `15103910009`, 678 B,
  zip sha256 `253e610aeb966bc95e87da452fb105b17d3beb8c92048dff87d825f3f28ec396`, equal to the API
  digest; member `linux-v2-host-prerequisite-45791682543-1.json`, 822 B, sha256
  `14dbe10dc0e2ff434b171a272fcb4b515be37acb6592896f2f2f2bd73a9f38e3`, `status: "passed"`,
  `source_commit` = the run head).

### Run-input sha256 equality (run head `5891e98b` vs PR head `90157ff4`)

`git diff --name-status 90157ff4 5891e98b` is exactly the two scratch-only additions, so every
PR-head file executed by the run is byte-identical by construction; the table pins the principal
executed inputs (each row measured at both revisions with `git cat-file -p <rev>:<f> | sha256sum`,
equal).

| File | sha256 (both revs) |
| --- | --- |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `97c472b02fd7317da5697d5881b0fd1d6a8be6a891d27712deae5850018dc6d5` |
| `daemon/e2e/production_linux_v2_cleanup_worker_shell_test.go` | `4f3e484a8abba074c69b38d01b802134b9b68ab5b729e3040d5d361fbc48be54` |
| `daemon/native/linux-gate/protocol/native_image_pin_linux_test.go` | `1194b0fe23ba9f4510345aae7bd5885267067efccc6f74ff2bbcac1b815acf70` |
| `daemon/native/linux-gate/protocol/proc_identity_linux.go` | `b210f968bb5d0e52095e46142b37c5fdba43dd39b13e0fe8c9ed66999a0f2ade` |
| `daemon/internal/platform/helper_v2_protocol.go` | `4ac73de34f901c91a3cef10e3704e9530f520db321deb6af4e38669bc6eb9d75` |
| `daemon/internal/execution/linux_owner_v2_linux.go` | `c5da521dda3c0d7f05fb008f9a5761d33a4cdb8a917d354d3b6930b031a36099` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `1fd4c17f3f54d6b616097de1eb1832ee6d2b04ec67434e5e48fb607688ae0ce3` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `d57e01b59dae83bcf9cce789a398eeaba7628a6171023b667234c3e8cd1368a6` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `2403b2f4aa3584d2cfa0b83dfe12e3210a967c0fbf16e1172ba14f005329516b` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `70163ae16b80e81bec0aa3ee7e70299c3d739716a037f2d0ad4dd5a3ed57d143` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `bf53e78fa7fee74d98988c1a8c13cc910930aa08081b1adadd839973d09dd5c1` |
| `scripts/ci/linux-v2-host-prerequisite-probe.sh` | `b8b91f407dd1eb4f79c3a27ef03bb1c5ff2dad7ff5cc6911a1e1d2d7678e9fad` |
| `scripts/ci/linux-v2-native-rootfs-base.sh` | `c8e4972490151e03e6b6c9ea3fa5393a8c348ebf0c9912e1d16770842f7c5866` |
| `scripts/ci/linux-v2-source-subject.py` | `27fed36a71d46129ce2b713d55b6615573408a37c0dc8cceb9e5918dcaacd452` |
| `.github/workflows/linux-native-v2-acceptance.yml` | `2d5500ce4b411e979cf974c19028514dceb5a729b3c73e8971add50f55bc27d8` |
| `.github/workflows/ci.yml` | `f3e29fab798f010fb256dd0a49a78711abe232d84b9fc843461aa0d092fc73f7` |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `8beeadf6d00d73dac74a10a49722c9b767dab20010875938c8af5725e8fb389f` (scratch-only) |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` (scratch-only) |

The full verbatim log rows, poll chronology and deletion proof are in the conductor's Bind
findings for this goal.

Ruling (V10, PR #302 round 1, 2026-11-26): the conductor records this Bind as binding for the
acceptance — the `:499 … stage=none` stop is gone, the worker is released as `/usr/bin/dash`, the
next stop is the labelled `clone_checkout` leg, recorded verbatim in `docs/TODO.md` under
`## rf-acc-synthetic-cleanup-release: residuals (2026-11-26)` (new goal
`rf-acc-synthetic-cleanup-seed`, gated on this PR), and `docs/archive/CLOSED-TODO.md` cites run
`45791682543`.

Run-input determination for the round-2 edits (V5's conditional; the 2026-11-26 byte-binding
rule): the case set executes `go test ./e2e` filtered to
`TestProductionLinuxV2CloneCleanupAuthenticatedScratch`
(`scripts/ci/linux-v2-native-acceptance.sh:3219-3223`) plus the in-guest `go test
./internal/linuxpreflight` AppArmor rows (`:1722-1723`); it never builds
`daemon/native/linux-gate/protocol`'s test files. Therefore
`daemon/native/linux-gate/protocol/native_image_pin_linux_test.go` (V5/V9) is **not an executed
input** of this case set, while `daemon/e2e/production_linux_v2_cleanup_witness_test.go` **is**
(it supplies the executed test) and `daemon/e2e/production_linux_v2_cleanup_worker_shell_test.go`
is compiled into the same `./e2e` test binary. The round-2 comment-rule strip changes both e2e
files by removing their new functions' two doc-comment lines only — no code, assertion or fixture
delta. This Bind thus binds the observed acceptance for the run's bytes, and the final bytes
require a **re-Bind** on the comment-stripped e2e files (V5's conditional; the byte-binding rule);
the equality-table rows record the run-time byte equality that was true when the run was bound.
