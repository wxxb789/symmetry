# rf-acc-synthetic-cleanup-release — evidence record

Contract: `goal-rf-acc-synthetic-cleanup-release.md` (GDD state root, external to the repository).
Base revision `ff0975a714d64e926190e3a937496a63e0d5ec86` (`main`, the contract's dispatch gate);
branch `refactor/rf-acc-synthetic-cleanup-release`. Plan:
`docs/plans/2026-11-26-rf-acc-synthetic-cleanup-release-plan.md`. Every number below is measured at
the delivered bytes unless a row names another revision; the tree is left uncommitted for the
conductor.

## 1. Subject bytes (sha256)

| File | Dispatch head `ff0975a7` | Delivered | Change |
| --- | --- | --- | --- |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `d120677d8718d0ad346d0fdfc9ef6136234d1b48071d77e3506aac4c844f01e6` | `97c472b02fd7317da5697d5881b0fd1d6a8be6a891d27712deae5850018dc6d5` | +29/−1 |
| `daemon/e2e/production_linux_v2_cleanup_worker_shell_test.go` | absent | `4f3e484a8abba074c69b38d01b802134b9b68ab5b729e3040d5d361fbc48be54` | new, 96 lines |
| `daemon/native/linux-gate/protocol/native_image_pin_linux_test.go` | absent | `1194b0fe23ba9f4510345aae7bd5885267067efccc6f74ff2bbcac1b815acf70` | new, 126 lines |
| `daemon/native/linux-gate/protocol/proc_identity_linux.go` | `b210f968bb5d0e52095e46142b37c5fdba43dd39b13e0fe8c9ed66999a0f2ade` | identical | untouched (production) |
| `daemon/internal/platform/helper_v2_protocol.go` | `4ac73de34f901c91a3cef10e3704e9530f520db321deb6af4e38669bc6eb9d75` | identical | untouched (production) |

No production byte, fixture script, workflow or `gate.c` byte changed; `git status --porcelain
-uall --no-renames` lists exactly the three delivered files above plus the plan, this record and the
two TODO files.

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
production_linux_v2_cleanup_witness_test.go:499: start controlled B Git worker through the real Linux v2 launcher/anchor/helper: resume process "/bin/sh": one-use Linux v2 release result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=none)
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
  asymmetry row, and the skip-with-reason when no trusted base exists.
- New probe `daemon/e2e/production_linux_v2_cleanup_worker_shell_test.go`: the fixture shell's own
  resolution (logged, so every run records the resolved image), the accepted symlink shapes, and the
  refused shapes.
- No assertion was weakened, deleted or renamed: the witness diff is `+29/−1`, and the single
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
| comment-cap | `python3 …/comment-cap.py <scratch repo> HEAD~1` (the delivered diff, 3 files, 251 insertions, 1 deletion) | 0 | no over-cap block; the first draft's 4-line/3-line doc comments were shortened to 2 lines and re-measured |
| protocol probe (WSL root) | `go test -count=1 -run '^TestNativeImagePin' ./native/linux-gate/protocol/` | 0 | both rows pass |
| e2e probe (WSL root) | `go test -count=1 -run '^TestLinuxV2CleanupApprovedWorkerProgram$' ./e2e/` | 0 | resolves `/bin/sh` to `/usr/bin/dash` |
| protocol probe (WSL `accept`) | same, as `sudo -u accept -i` | 0 | `ok … 0.024s` |
| e2e probe (WSL `accept`) | same, as `sudo -u accept -i` | 0 | `ok … 0.026s` |
| replica run, guest A shape | `chroot path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec setpriv --reuid=52000 … protocol-pin.test -test.run '^TestNativeImagePin'` | 0 | §2.3 rows |
| replica run, guest A shape | same replica, `e2e-probe.test -test.run '^TestLinuxV2CleanupApprovedWorkerProgram$'` | 0 | `controlled B worker shell /bin/sh resolves to the approved image /usr/bin/dash` |

### 5.1 Full-suite runs and environment attribution

| Run | Command | Exit | Attribution |
| --- | --- | --- | --- |
| WSL full suite, root | `(cd daemon && go test -count=1 ./...)` as root | 1 | 13 rows of `daemon/internal/platform` refuse with `wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0` (`anchor_shape_table_test.go:251` etc.); every other package `ok`. The rows require a non-root A; environment, not code |
| WSL full suite, `accept` (umask 002) | same as `sudo -u accept -i` | 1 | `daemon/internal/workspace` clone-seed rows refuse (`linux_clone_linux_test.go:1465: validate A-owned Git source: final directory must be A-owned and not group/world writable`). Measured cause: Go 1.27's `t.TempDir()` creates its numbered directory with `os.Mkdir(dir, 0o777)` (`/usr/local/lib/go1.27.0/src/testing/testing.go:1635`) and `accept`'s umask is `0002`, so the tree is `0775` and the production A-ownership rule (`mode&0o022 != 0`) refuses it; a scratch copy of the production validator prints `path=path-sha256:7760009c20ff3b77cbf0e7cd071fd4bebe07e85f7491ec740d9c936ae97a8690 uid=1000 euid=1000 mode=040775`. All other packages `ok` |
| base-vs-head, `internal/workspace` | the same single test at the base bytes (`git archive HEAD daemon` in `/tmp`, ext4) as `accept`, umask 002 | 1 | identical failure `validate A-owned Git source: final directory must be A-owned and not group/world writable` at base, so the row is pre-existing and unrelated to this change |
| WSL full suite, `accept` umask 022 | `sudo -u accept -i bash -lc 'umask 022; cd daemon && go test -count=1 ./...'` | 0 | 32 package rows, every one `ok` (no skips attributed, no failures), including `e2e 1.417s`, `internal/platform 39.600s`, `internal/workspace 49.919s`, `native/linux-gate/protocol 5.661s` |
| Windows full suite | `(cd daemon && go test -count=1 ./...)` on the Windows host, Go 1.27.4 | 0 | 30 package rows, every one `ok` (the linux-only probe files are not built on Windows), including `e2e 0.301s`, `internal/app 149.775s`, `internal/workspace 140.467s` |

## 6. Probes and mutants

Probe rows (delivered bytes): protocol — controlled positive pin, planted parent symlink
(refusal text `native-image parent component "linked"`), planted final symlink (`is not A-approved`),
each asserting no `ReleaseSubStepError`; system-shell asymmetry row (skip-with-reason only when
`/bin/sh` is not a symlink). e2e — the fixture shell resolves and logs `/usr/bin/dash`; three
accepted shapes (real file, symlinked directory component, symlinked file); eight refusals (missing,
broken symlink, non-canonical, `/`, non-executable, set-ID, group/world-writable, directory).

The first probe run caught a defect in the new helper itself: `info.Mode().Perm()&0o7000` is always
zero because `Perm()` carries only the permission bits, so the set-ID row stayed green
(`worker program for "…/setid-shell" = "…/setid-shell", err=<nil>, want a refusal`, `E2E_RC=1`);
the check now reads `os.ModeSetuid|os.ModeSetgid|os.ModeSticky` and M-P4 pins it.

Mutants (each applied to the delivered bytes, run, then restored and re-checked with `cmp` against
a byte copy; the two production mutants mutate `proc_identity_linux.go` `b210f968…`, restored
byte-identical after each row):

| id | Mutation | Red probe (verbatim) | Exit | Restored |
| --- | --- | --- | --- | --- |
| M-P1 | `proc_identity_linux.go:128`: drop `O_NOFOLLOW` from the parent-component open | `native_image_pin_linux_test.go:44: native image pin accepted "path-sha256:bf8148640fd69d8ab2d66ec30a052eeeff2160ebd5675f3b529f7881dbf70dc4", want a refusal` | 1 | byte-identical |
| M-P2 | `proc_identity_linux.go:79`: drop `O_NOFOLLOW` from the image open | `native_image_pin_linux_test.go:50: native image pin accepted "path-sha256:c2d71f89a1ad1fd6d9f253ca14adb6c38d6f0c783aef49c17667dd30b19c0069", want a refusal` | 1 | byte-identical |
| M-P3 | witness helper: drop the `Perm()&0o022 != 0` clause | `production_linux_v2_cleanup_worker_shell_test.go:90: worker program for "…/world-writable-shell" = …, err=<nil>, want a refusal` | 1 | `97c472b0…` |
| M-P4 | witness helper: `os.ModeSetuid\|os.ModeSetgid\|os.ModeSticky` → `0` | same row for `"…/setid-shell"` | 1 | `97c472b0…` |
| M-P5 | witness helper: drop `!info.Mode().IsRegular()` | same row for `"…/directory-shell"` | 1 | `97c472b0…` |
| M-P6 | revert half of the fix: `Program: linuxV2CleanupWorkerShell` | not runnable on this host (guest-only clause): the red evidence is the recorded `:499` stop (§2.1) and the replica refusal (§2.3) | — | — |

## 7. Ruling-item checklist (contract clause → disposition + evidence)

| id | Clause | Disposition | Evidence |
| --- | --- | --- | --- |
| C1 | Stage 1 root cause, named verbatim with file:line, candidates discriminated | done | §2.1-§2.2: pin refusal at `proc_identity_linux.go:128` (`/bin -> usr/bin`), unlabelled → `stage=none`; the fast stop and the absent `guest_stage`/arm exclude helper death and protocol mismatch |
| C2 | Compared with the green legs that release B through the same launcher | done | §2.2 census: every green leg's image is an installed root-owned regular file; the only explicit `Program:` in e2e is this leg's |
| C3 | Production defect → DECISION_NEEDED | refused (not a production defect) | §2.3 + plan §2.3: the pin's symlink-free rule is the A-side approval; all production profiles satisfy it |
| C4 | Stage 2 fix on the wrong side | done | §3: witness image choice only; `+29/−1`, no assertion touched |
| C5 | Never weaken the RELEASE-outcome resolution or any cleanup assertion | done | §3 last bullet; the helper, the detail vocabulary and every cleanup assertion are byte-identical |
| C6 | Fences and idempotency preserved | done | `daemon/internal/**` (non-test) and `gate.c` have no diff (§1) |
| C7 | Comments ≤ 2 lines, `go vet` compile-check, never `-o /dev/null` | done | §5 rows (comment-cap 0; both vet rows 0) |
| C8 | Records rule: red mutants or greps, a red row per new branch; "delivered, pending the conductor's Bind" | done | §6 mutants M-P1…M-P5 (each new helper branch and both pin rules), §2.2 greps, this record's wording and the CLOSED-TODO entry |
| C9 | FULL `go test ./...` on Windows plus WSL; comment-cap before the final re-measure | done | §5.1: Windows exit 0 (30 rows), WSL as `accept` umask 022 exit 0 (32 rows), both at the delivered bytes, with the root-run and umask-002 artifacts base-vs-head attributed; comment-cap exit 0 ran before them |
| C10 | Acceptance: the conductor's Bind runs `synthetic_goal_cleanup` once on the PR head | pending | Delivered, pending the conductor's Bind: the `:499` stop is gone; the next stop is recorded verbatim, or green |

## 8. Scope, residuals and limits

Owned files changed: the witness test, the two new probe files, the plan, this record,
`docs/TODO.md` (carve + one new residual) and `docs/archive/CLOSED-TODO.md` (the moved entry). The
plan §6 states and selects the reading that lets the pin probe live in
`daemon/native/linux-gate/protocol/*_test.go` (it exercises the shared production helper this leg
failed on); it is test-only and reverts trivially under the stricter reading.

Residuals: (i) the pin's refusals stay unlabelled, so a future leg with an unapproved image path
still stops as `stage=none` — recorded as a new `docs/TODO.md` entry with the witness check and this
probe as the current guards; (ii) `shared`'s three owner-leg stops stay tracked in `docs/TODO.md`
(not this goal's surface); (iii) the uutils `AT_EXECFN`/fd-exec product constraint is unchanged and
not touched by this fix (`dash` is a single-purpose ELF); (iv) the WSL full suite needs a non-root
user with umask 022 on this host (Go 1.27 `t.TempDir()` × umask 0002 would otherwise trip the
production A-ownership rule in `internal/workspace` — pre-existing, base-vs-head attributed in
§5.1).
