# rf-acc-synthetic-cleanup-release — implement plan

Contract: `goal-rf-acc-synthetic-cleanup-release.md` (GDD state root, external to the repository).
Base revision `ff0975a7` (the contract's dispatch gate, `main`); branch
`refactor/rf-acc-synthetic-cleanup-release`. The accepted plan is the contract; this file resolves
the implementation choices it leaves open (which side the fix belongs to, how the witness names its
B worker image, the probe rows, the owned-set reading of the protocol test file) and records the
measured evidence that closed them.

## 1. Clause census at the dispatch head

| Contract clause | Dispatch head | State |
| --- | --- | --- |
| Change 1: root cause of `release_outcome_unknown; stage=none` | `daemon/e2e/production_linux_v2_cleanup_witness_test.go:470` (`Program: "/bin/sh"`); pin call `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:928`; refusal sites `daemon/native/linux-gate/protocol/proc_identity_linux.go:79,:128,:147,:159` | Root cause ruled in §2: the leg's `/bin/sh` image cannot pass A's native-image pin on the usrmerge guest |
| Change 1: name it with file:line evidence | run `45775969711` stop (findings), resolute replica probe (§4) | §2 names the chain end to end |
| Change 1: if it is a production defect → DECISION_NEEDED | the pin's symlink-free rule is the A-side image-approval contract | Not a production defect: every production profile and every green leg already releases a symlink-free installed image (§2.3); the leg is on the wrong side |
| Change 2: fix on the wrong side, never weaken the RELEASE-outcome resolution or any cleanup assertion | the witness's own image choice | §3 changes the program choice only; `git diff` shows no assertion edit |
| Invariants: fences and idempotency preserved | `daemon/native/linux-gate/**` production, `gate.c` | Untouched; the probe only reads the pin |
| Invariants: comments ≤ 2 lines | added comment lines in the three delivered files | §5 gate row (comment-cap); round-2 standing rule: no doc comments on the new functions/tests, one 2-line why comment beside the const |
| Invariants: `go vet` compile-check, never `-o /dev/null` | `daemon/` | §5 rows (linux and windows) |
| Invariants: records rule (red mutants or greps), a red row per new branch | new helper branches + probe rows | §6 rows M-P1…M-P5; the greps in §2.3 |
| Invariants: FULL `go test ./...` on Windows plus WSL; comment-cap before the final re-measure | `daemon/` | §5 rows |
| Acceptance: the conductor's Bind runs `synthetic_goal_cleanup` once on the PR head | run `45791682543` on `90157ff4` | Bound (V10): the `:499 … stage=none` stop is gone; the next stop, the labelled `clone_checkout` leg, is recorded verbatim in `docs/TODO.md` under `## rf-acc-synthetic-cleanup-release: residuals (2026-11-26)` |
| Owned: the witness and its e2e helpers, a plan, an evidence record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` | — | This file, `docs/evidence/rf-acc-synthetic-cleanup-release-2026-11-26.md`, the TODO carve |

## 2. Root cause (Stage 1)

### 2.1 The unlabelled refusal (code path)

`helper_v2_server.go:1147` runs the one-use RELEASE in the helper process. Before the release packet
is built, `gate_lifecycle_linux.go:928` pins the A-approved native image, and `pinNativeImage`
(`proc_identity_linux.go:74`) opens every path component `O_NOFOLLOW` (`:128` parents via
`verifyTrustedImageDirectory` `:147`, `:79` the image via `approvedNativeImageIdentity` `:159`). The
refusals it returns are plain errors, so `NewReleaseSubStepError` never labels them,
`linuxOwnerHelperV2ReleaseErrorDetailFor` (`helper_v2_protocol.go:591`) leaves
`Stage = linuxOwnerHelperV2ReleaseStageNone` (`:480`), `String()` (`:486`) renders `stage=none`, the
helper answers `release_outcome_unknown` (`helper_v2_server.go:1200,:1211`), and the daemon wraps it
at `linux_owner_v2_linux.go:1066`. The observed stop line
(`production_linux_v2_cleanup_witness_test.go:499`, run `45775969711`) is exactly that chain.

Every other unlabelled pre-packet refusal in `PreparedGate.Release` is program-independent
(`command.Validate`, `validateReleaseOwnerPipe`, `monotonicDeadlineNS`, the owner-pipe duplication,
`encodeGuardedRelease`) and is exercised green by the legs that release `/usr/local/...` images, so
the leg's own `Program: "/bin/sh"` is the only differing input. All gate-side refusals after the
packet are labelled (`stage=clone_bootstrap`/`native_exec` with `arm`/`guest_stage`, e.g. the
AppArmor rows of run `45644664168`), which the observed `stage=none` excludes.

### 2.2 Measured on the guest-identical userspace

`path-sha256:dba63390a9da5f52d3a7ffab205e03c8a75c3c703da0942ac059b6dad88a28ec` (the resolute
replica kept by `rf-acc-b-process-readback`, `Ubuntu 26.04 LTS`, `dash 0.5.12-12ubuntu3`, uutils
`coreutils 9.5-1ubuntu2+0.0.0~ubuntu25`) is the same userspace the fixture debootstraps. The new
probe run there as the guest's A shape (`setpriv --reuid=52000`,
`HOME=path-sha256:ec3459ebf2a5788e2ff90499117aae5439a79dc15090561bb8b2a50bf9cc5c83`) prints:

```
native image pin refusal for "/bin/sh": open native-image parent component "bin" without following symlinks: not a directory
native image pin accepted the resolved system shell "/usr/bin/dash"
```

`/bin` is the usrmerge symlink (`bin -> usr/bin`) and `sh -> dash`, so the parent walk's
`O_PATH|O_DIRECTORY|O_NOFOLLOW` fails `ENOTDIR` before the final `sh` symlink is even reached; the
same refusal shape was reproduced for a planted parent symlink and a planted final symlink (§6).

### 2.3 Why this is the leg's defect, not production's

- Production profiles release installed, symlink-free images (`/usr/local/bin/codex`, the staged
  `/usr/local/lib/symmetry-pi/pi`, and the protocol legs' staged single-purpose ELFs); the pin's
  symlink-free rule is the A-side approval that keeps a B-writable or swapped image out.
- The repo already records both guest facts this depends on: the resolute/uutils `coreutils`
  symlink farm (`docs/TODO.md`, uutils entry) and usrmerge rewriting `/proc/<pid>/exe`
  (`readback_helpers_linux_test.go:44`).
- Grep census of the release inputs at the dispatch head (`grep -rn 'Program:' daemon/e2e
  daemon/internal/app` and the profile fixtures): the cleanup witness is the only leg whose native
  image is a distro path with symlinked components.

## 3. The one mechanism (Change 2)

The witness keeps the POSIX shell as its B worker and releases the shell's **resolved** path:
`linuxV2CleanupApprovedWorkerProgram` (`filepath.EvalSymlinks` + `Lstat`) returns the exact
symlink-free executable (on the guest `/usr/bin/dash`) and refuses any degenerate shape with a named
error, so a future guest cannot turn this into another `stage=none`. Rejected alternatives:

- (b) staging an A-owned copy of a shell: the clone bootstrap opens the image by path as B, so the
  copy would need an A-owned directory chain that B may traverse (ACLs) — more moving parts than the
  installed root-owned shell for no additional proof.
- (c) production change (teach the pin to follow symlinks): refused by the contract's
  owned-files rule and by the invariant; following symlinks would move the approval decision to
  whoever controls the path, which is the property the pin exists to deny.
- `bash`/`dash` hard-coded instead of resolved: layout-dependent (`/usr/bin` bound to real paths
  under usrmerge only); resolving the fixture's `/bin/sh` keeps the fixture contract and needs no
  case-set change.

## 4. Probe rows (check before the change; the clause test is guest-only)

The clause test is the `synthetic_goal_cleanup` case set, which needs the installed owner, the
disposable guest and Control; it cannot run on this host. Replacement proof, in order:

1. the recorded CI stop (`45775969711`) is the red observation;
2. the pin probe rows characterize the production behavior on the guest-identical userspace (§2.2)
   and on the local WSL host (both exit 0);
3. probes and mutants (§6) pin the refusal rules and the new helper branches.

## 5. Evidence plan (gates)

`gofmt -l`, `GOOS=linux go vet ./...` and `go vet ./...` (windows) from `daemon/`; the two focused
probe tests plus the replica runs; comment-cap before the final re-measure; the FULL
`go test ./...` on Windows and in WSL from `daemon/`, recorded with counts and exit codes in the
evidence record.

## 6. Owned-set reading (contract ambiguity, stated rather than guessed)

The contract owns `daemon/native/linux-gate/protocol/*_test.go` "only if they share the helper".
Readings: (i) a new probe that exercises the shared production helper the leg failed on
(`pinNativeImage`/`openTrustedImageParent`/`approvedNativeImageIdentity`) is inside the set;
(ii) only existing files that literally share a helper with the e2e leg are. Recommendation and
selection: (i). The probe is test-only, adds no production byte, and is the only way the root cause
stays pinned; the e2e leg itself cannot import those unexported helpers, so a probe in the owning
package is the only place the refusal rules can be pinned. Recorded here for the conductor;
confirmed in the PR #302 round-1 ruling (V6, 2026-11-26): reading (i) stands and the file is owned.
It stays test-only and reverts trivially under the stricter reading.

## 7. Out of scope

- The failed-release cleanup fallout (`production_linux_v2_cleanup_witness_test.go:488` dial
  refusal) is the second stop of the same incident, not an independent defect: the Start-failure
  path already stops the containment (`cleanupFailedStartStarted` → `Process.Terminate`), and the
  helper retires its endpoint afterwards; it is expected to disappear with the release.
- `shared`'s three owner-leg stops stay tracked in `docs/TODO.md` (not this goal's owned surface).
- The uutils `AT_EXECFN`/fd-exec product constraint stays as recorded; `dash` is a single-purpose
  ELF, so the fix does not interact with it.
- No production byte changes: `daemon/internal/**`, `gate.c` and the fixture scripts stay untouched.
