# workspace walkers / subject guards + materialize dedup plan — one openat2 directory opener, one fd-stepping core, one materialize

Contract: `goal-rf-w4-04-workspace-walkers.md` (GDD, 2026-11-21) with its binding
conductor decisions (Wave-4 draft D1/D2, W4 review reconcile E4, lesson 32). Branch
`refactor/rf-w4-04-workspace-walkers`; frozen base `c9ed23b409771bd1c04691eeca31c51781da719b`
(`c9ed23b4`, the merge of #161) = `origin/main`. The contract re-derived its coordinates
against `b89e14d7` and `0fa9b705`; `git log --oneline b89e14d7..HEAD -- daemon/internal/workspace/`
is empty, so every coordinate below still resolves at the frozen base and the base
measurements bind either revision. Findings
F-16 (`daemon-protocol-rest.md:468-482`) and F-17 (`:482-493`), program plan §7 W4-04, DR S10.

Area: `daemon/internal/workspace/` only, plus this plan file. Owned source:
`linux_clone_fs_linux.go`, `subject.go`, `workspace.go`, `fingerprint.go`,
`linux_clone_linux.go` (only if a shared predicate is consumed), plus new pins in the
existing test files. No evidence file: as in the sibling control and daemon increments,
the judging artifact is the stage's `answer.md`; the tables clause 2 demands are also
reproduced here.

## Deliverable shape (three production pieces, four new pins)

1. **One `openat2` directory opener.** `openLinuxCloneDirAtFlags(parentFD, name, accessFlags)`
   replaces `openLinuxCloneOPathDirAt` (`:206-225`) and `openLinuxCloneDirAt`
   (`:1253-1272`): identical `Flags`/`Resolve`/`Fstat`/`S_IFMT` body, one caller-tunable
   `O_PATH`/`O_RDONLY` bit. The request literal moves into `linuxCloneDirOpenHow(accessFlags)`
   so the flag-level pin can compare the exact `OpenHow` the opener sends against the base
   literals (clause 2). Every at-call site passes its own access flag; the two path-level
   3-line callers `openLinuxCloneDirPath`/`openLinuxCloneOPathDirPath` (`:97-103`) stay as
   they are. `openLinuxCloneFileAt` (`:1274-1280`) is **not** unified (contract: caller-flags
   file opener).
2. **One fd-stepping core for the two configured-path walkers.** `openLinuxClonePinnedChild`
   (openat with the shared descriptor flags, parent released) and
   `readLinuxClonePinnedDirectory` (Fstat, descriptor released on failure) are the only
   mechanics the walks share. `openLinuxCloneTrustedPathDir` (`:109-184`) and
   `openLinuxCloneOwnedDirPath` (`:486-532`) stay two thin walkers that keep their own
   syscall order, mode/ownership policy, `S_IFMT` checks, refusal strings and error
   wrapping; a single post-open-stat callback cannot express the trusted order (contract
   change note, E4 walker shape), and no callback is introduced.
3. **One `materializeAdmittedSubject`.** `MaterializeSubject` (`subject.go:132-164`) stops
   inlining the `materializeAtRepository` body (`:321-339`); the shared function is renamed
   to the contract's name and called by `MaterializeSubject`, `PrepareSubject` (`:169-202`)
   and `RecoverSubject` (`:206-239`), and keeps serving `VerifySubject`/`DeriveSubject`.
   Every caller's `subject.Validate()` call, its `SubjectError` fields and its place in the
   control flow are preserved literally: the shared function derives only (identity
   validation, commit reachability, tree digest) and validates nothing.
   The rename migrates its sixth call site, `SubjectArtifact` (`artifact.go:62`), with a
   one-line call update: a rename forces a clean cutover and the repository rules forbid a
   compatibility shim, so that line is the only edit outside the contract's owned file
   list. It changes no behaviour, no string and no control flow in `artifact.go` (the
   before/after error-literal diff for that file is empty).
4. **Four new pins in existing test files**, one per refusal site touched and one per flag
   surface, each with a named red mutant:
   - `TestCleanupRefusesLinuxCloneWithoutStoreReadback` — `workspace.go`;
   - `TestFingerprintRefusesLinuxCloneIdentityMismatch` — `fingerprint.go` branch;
   - `TestVerifySubjectRefusesUnmanagedWorktree` — `subject.go` guard;
   - `TestLinuxCloneDirectoryOpenFlagsStayByteIdentical` — the `OpenHow` and walk-core flag
     sets, including `RESOLVE_NO_XDEV`.

No shared `requireManaged()` predicate is added: the three guard sites have different
predicates, outcomes and strings, and E4 drops that unification. `workspace.go` and
`fingerprint.go` therefore get pins only, no production edit; `linux_clone_linux.go` is
untouched (the clone `Prepared` literal needs no shared predicate once E4 lands).

## Public / API shape

Unchanged. No exported identifier is added, renamed, re-signed or deleted: all four pieces
are unexported, and the exported surface (`Manager.MaterializeSubject`,
`Manager.PrepareSubject`, `Manager.RecoverSubject`, `Manager.VerifySubject`,
`Manager.DeriveSubject`, `Manager.Cleanup`, `Fingerprint`, `Prepared`, `SubjectWorkspace`,
`SubjectError`) keeps its name, signature and error identity. `go doc -all
./internal/workspace` is not a useful diff for an unexported-only change; the in-repo
proof is the pinned before/after byte-compare of every touched refusal string plus the
unchanged-symbol grep over the removed definitions.

## Exact semantics the contract demands (byte-identical surfaces)

### Walkers: per-component syscall sequence (base to HEAD, per policy)

| Trusted walk (`openLinuxCloneTrustedPathDir`) | Owned walk (`openLinuxCloneOwnedDirPath`) |
| --- | --- |
| `canonicalLinuxClonePath` refusal, `/` refusal | `canonicalLinuxClonePath` refusal |
| `unix.Open("/", accessFlags\|O_DIRECTORY\|O_CLOEXEC)` | `unix.Open("/", O_RDONLY\|O_DIRECTORY\|O_CLOEXEC)` |
| per component: skip empty; `.`/`..` refusal | per component: skip empty |
| `Fstatat(parentFD, component, AT_SYMLINK_NOFOLLOW)` | `Openat(parentFD, component, accessFlags\|O_DIRECTORY\|O_CLOEXEC\|O_NOFOLLOW)` |
| `S_IFMT != S_IFDIR` refusal | parent descriptor released |
| last-component mode refusal / ancestor trust policy | `Fstat(child)` |
| `Openat(parentFD, component, accessFlags\|O_DIRECTORY\|O_CLOEXEC\|O_NOFOLLOW)` | `S_IFMT != S_IFDIR` refusal |
| parent descriptor released | last: A-owned + not group/world writable; ancestor: root/A-owned, non-sticky world-write refusal, foreign-group-write refusal |
| `Fstat(child)` | advance |
| identity/mode recheck (`sameLinuxCloneDirectoryIdentity`, uid/gid/mode bits) | final: `Fstat(fd)` |
| policy re-check on the pinned descriptor (root mode, ancestor trust), advance | |
| final: `Fstat(fd)`, `S_IFMT != S_IFDIR` refusal | |

Every row survives: the pre-stat, the pre-stat `S_IFMT` check, the parent-FD policy, the
open, the post-open identity recheck and the pinned-descriptor policy stay in the trusted
walker in that order; the owned walk keeps open-then-verify. Only the three mechanical
steps the two walks spell twice move into the core — `Openat` with the shared descriptor
flags plus parent release, `Fstat` plus release-on-failure, and the final descriptor read.

### Walk-core and `OpenHow` flag sets

| Surface | Base | HEAD |
| --- | --- | --- |
| root descriptor | `accessFlags\|O_DIRECTORY\|O_CLOEXEC` | `accessFlags\|linuxCloneConfiguredPathRootFlags` = same value |
| component descriptor | `accessFlags\|O_DIRECTORY\|O_CLOEXEC\|O_NOFOLLOW` | `accessFlags\|linuxCloneConfiguredPathComponentFlags` = same value |
| `openat2` request (dir) | `O_PATH`/`O_RDONLY` + `O_DIRECTORY\|O_CLOEXEC\|O_NOFOLLOW`, resolve `BENEATH\|NO_SYMLINKS\|NO_MAGICLINKS\|NO_XDEV` | `linuxCloneDirOpenHow(accessFlags)` = same bytes |
| `openat2` request (file, unchanged) | `flags\|O_CLOEXEC\|O_NOFOLLOW`, same resolve set | untouched |

### Refusal strings that must not move

| Site | String (or `SubjectError` code) |
| --- | --- |
| trusted walk, canonical | `directory path must be canonical and absolute` |
| trusted walk, root | `filesystem root cannot be configured as a B clone root` |
| trusted walk, invalid component | `configured directory path contains an invalid component` |
| trusted walk, pre-stat | `inspect configured directory component %q: %w` |
| trusted walk, pre-stat kind | `configured directory component %q is not a real directory` |
| trusted walk, root mode | `configured B root has group/world mode-bit write access` |
| trusted walk, ancestor policy | `configured root ancestor %q is not trusted: %w` |
| trusted walk, open | `pin configured root component %q: %w` |
| trusted walk, read | `read pinned configured root component %q: %w` |
| trusted walk, identity | `configured root component changed while pinning` |
| trusted walk, pinned root mode | `configured B root gained group/world mode-bit write access` |
| trusted walk, pinned ancestor | `pinned configured root ancestor %q is not trusted: %w` |
| trusted walk, final kind | `path is not a directory` |
| owned walk, canonical | `A-owned directory path must be canonical and absolute` |
| owned walk, kind | `A-owned path contains a non-directory component` |
| owned walk, last component | `final directory must be A-owned and not group/world writable` |
| owned walk, ancestor | `A-owned path has an unsafe ancestor` |
| dir opener | `entry is not a real directory` (unchanged; both callers wrap it) |
| Cleanup linuxClone refusal | `Linux clone cleanup requires explicit Store readback authorization` |
| Cleanup unmanaged no-op | `nil` (success, no work) |
| Fingerprint linuxClone identity | `Linux clone Prepared identity differs from its reserved grant` |
| Fingerprint linuxClone digest | `Linux clone fingerprint is invalid` |
| `verifyPreparedSubject` managed guard | `SubjectErrorWorkspaceBinding`, `subject verification requires a daemon-managed Git worktree` |

`openLinuxClonePinnedChild` and `readLinuxClonePinnedDirectory` return the raw syscall
error; each walker keeps its own wrapping, so no refusal text passes through the shared
core.

## Control-flow audit (lesson 32)

Every extraction carries a base-vs-HEAD row (each early return, fall-through,
error short-circuit and default clause in base mapped to its HEAD equivalent, with
preserved yes/no) in the "Per-extraction control-flow audit" table under
**Measured outcome** below, reproduced in the stage `answer.md`. The three rows that
matter most:

- **trusted walk** — 13 refusal points + 1 success return; all 14 preserved in order, with
  the two closed-descriptor error paths now closed by the shared reader instead of inline
  (same syscall, same release, same returned error).
- **owned walk** — 5 refusal points + 1 success return; preserved. The only shape change is
  `var info unix.Stat_t; Fstat` becoming `info, err := readLinuxClonePinnedDirectory(fd)`,
  which performs the identical `Fstat` and closes the descriptor on failure exactly where
  base does.
- **materialize** — 4 refusal points + 1 success return; preserved verbatim. `PrepareSubject`
  and `RecoverSubject` keep `admitted.Validate()` before the call and their
  `materialized != admitted` mismatch check after it; `VerifySubject` keeps
  `expected.Validate()` and its commit/digest compare; `DeriveSubject` keeps
  `validateSubjectResourceID`. No branch is added or removed anywhere: the shared function
  validates nothing, so no caller gains a refusal it did not have.

## New pins and their red mutants

| Pin | Covers | Named mutant (one-line edit, applied in place through the patch tool and reverted after the red run) |
| --- | --- | --- |
| `TestCleanupRefusesLinuxCloneWithoutStoreReadback` | `workspace.go` linuxClone refusal + `!managed` no-op | delete the `if prepared.linuxClone` block in `Cleanup` |
| `TestFingerprintRefusesLinuxCloneIdentityMismatch` | `fingerprint.go` identity branch, both strings | drop the `prepared.Path != prepared.linuxCloneTarget` term (identity case); `if !validLinuxCloneFingerprint` becomes `if false` (digest case) |
| `TestVerifySubjectRefusesUnmanagedWorktree` | `subject.go` managed/binding refusal, code + string | drop `!prepared.managed` from the guard predicate |
| `TestLinuxCloneDirectoryOpenFlagsStayByteIdentical` | `OpenHow` request + walk-core flag consts | drop `RESOLVE_NO_XDEV` from `linuxCloneDirOpenHow`; drop `O_NOFOLLOW` from `linuxCloneConfiguredPathComponentFlags` |

The flag pin is the required oracle for `RESOLVE_NO_XDEV`, because the pre-existing
bind-mount case `TestLinuxCloneOPathRootAllowsMountedRootButForbidsNestedMount`
(`linux_clone_linux_test.go:502`) can be skipped on a host without mount privileges and
cannot be the sole oracle for its path. The two call sites that test has
(`linux_clone_linux_test.go:496`, `:505`) are updated mechanically to
`openLinuxCloneDirAtFlags(rootFD, "nested", unix.O_PATH)`: same test name, same assertions.

## Evidence plan (exact commands, all foreground; never a measurement beside the full suite)

From `daemon/` unless a `cd` is shown; `<WT>` is the worktree root.

1. Focused red/green: `go test -count=1 ./internal/workspace/ -run 'TestLinuxClone|TestSubject|TestWorkspace|TestFingerprint'`
   (Windows, cross-platform subset) and the same under WSL (`cd /mnt/q/repos/wt-w4-04-workspace-walkers/symmetry/daemon`)
   for the `linux && amd64` files.
2. Windows-local package: `go test -count=1 ./internal/workspace/...`.
3. Linux package, verbose, with the SKIP list: `wsl -d Ubuntu-24.04 -u accept bash -lc 'cd /mnt/q/repos/wt-w4-04-workspace-walkers/symmetry/daemon && go test -count=1 -v ./internal/workspace/'`.
   The same command runs against an extracted base tree so the SKIP lists and PASS counts
   are compared, not assumed. The extraction is done from the host
   (`git archive c9ed23b4 daemon -o <tmp>.tar`) and untarred inside WSL, because this
   worktree's `.git` file points at a Windows path that WSL git cannot resolve; both
   suites are driven by a script file under `/mnt/c/...` so no nested quoting is involved.
4. Callers (lesson 32): `go test -count=1 ./internal/app/...` (Windows) and the Linux
   package under WSL when the `linux && amd64` files are involved; harness consumers
   `go test -count=1 ./internal/harness/pi/ ./internal/harness/omp/ ./internal/harness/omo/`.
5. Static: `gofmt -l` on the changed files, `go vet ./...`, `GOOS=windows go build ./...`,
   `GOOS=linux go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...`.
6. Hygiene: `git diff --check`; `git diff c9ed23b4...HEAD | grep -icE "users[/][a-z]"` = 0;
   `python scripts/ci/check-citation-existence.py --base c9ed23b4 --head HEAD`.
7. Mutants: one row per pin in the table below, each applied as a one-line patch to the
   production file, run with `go test -count=1 -run <pin> ./internal/workspace/`
   (Windows for the three cross-platform pins, WSL for the flag pin), then reverted through
   the same patch tool; every mutant is expected to redden exactly its pin and the reverted
   tree to leave the suites green.
8. Error-string byte-compare: `git show c9ed23b4:<file> | diff - <file>` restricted to the
   refusal `errors.New`/`fmt.Errorf` literals and `SubjectError` constructions, plus the
   before/after call-site grep for the two removed definitions.
9. CI race (Linux) remains the slow oracle; the conductor runs it.

## Measured outcome (delivered revision; base `c9ed23b4` for every comparison)

### Per-extraction control-flow audit (lesson 32)

The base-vs-HEAD function-body diffs of the two walkers are exactly three mechanical steps
each, with no other line touched:

| extraction | base branches | HEAD equivalent | preserved |
| --- | --- | --- | --- |
| trusted walk root open | 2 (open error, success) | `accessFlags\|linuxCloneConfiguredPathRootFlags` (same value) | yes |
| trusted walk component open (`Openat` + `Close(parent)`) | 2 (open error, success) | `openLinuxClonePinnedChild`: identical syscalls in the identical order; the walker keeps the `pin configured root component %q` wrapping | yes |
| trusted walk post-open read | 2 (stat error → close + wrap, success) | `readLinuxClonePinnedDirectory` closes on failure; the walker keeps the `read pinned configured root component %q` wrapping | yes |
| trusted walk final read + kind check | 3 (stat error, kind error, success) | `readLinuxClonePinnedDirectory` + the unchanged `path is not a directory` refusal | yes |
| trusted walk policy, identity recheck, pre-stat, `.`/`..` refusal | 12 | untouched lines | yes |
| owned walk root open | 2 | `unix.O_RDONLY\|linuxCloneConfiguredPathRootFlags` (same value) | yes |
| owned walk component open | 2 | `openLinuxClonePinnedChild` + raw error passthrough | yes |
| owned walk read (component and final) | 2 each | `readLinuxClonePinnedDirectory` + raw error passthrough | yes |
| owned walk kind check + ownership/mode policy | 4 | untouched lines | yes |
| directory opener pair → `openLinuxCloneDirAtFlags` + `linuxCloneDirOpenHow` | 4 (openat2 error, fstat error, kind error, success) | identical branch order, one access-flag parameter; both former call sites keep their access flag (`O_PATH`/`O_RDONLY`) | yes |
| `MaterializeSubject` inline materialization → `materializeAdmittedSubject` | 5 (identity, commit unreachable, digest, subject validate, success) | the shared function holds the first three verbatim; the caller keeps `subject.Validate()` and its exact `SubjectError` | yes |

No branch is added or removed anywhere: the shared materialization function validates
nothing, so no caller gains or loses a refusal.

### Mutations (one named red mutant per new pin, applied in place and reverted)

| mutant | pin | observed |
| --- | --- | --- |
| M1 delete the `Cleanup` linuxClone refusal | `TestCleanupRefusesLinuxCloneWithoutStoreReadback` | FAIL, exit 1: `Cleanup(linuxClone) error = <nil>, want the Store readback refusal` |
| M2 drop the `prepared.Path` term from the Fingerprint identity predicate | `TestFingerprintRefusesLinuxCloneIdentityMismatch` | FAIL, exit 1: `Fingerprint(path) = "sha256:0000…", <nil>; want the identity refusal` |
| M2b `if !validLinuxCloneFingerprint(...)` becomes `if false` | same pin | FAIL, exit 1: `Fingerprint(digest prefix) = "sha1:0000…", <nil>` |
| M3 drop `!prepared.managed` from the `verifyPreparedSubject` guard | `TestVerifySubjectRefusesUnmanagedWorktree` | FAIL, exit 1 on the "unmanaged with repository" case; the message becomes `workspace binding "primary" is not configured` |
| M4 drop `RESOLVE_NO_XDEV` from `linuxCloneDirOpenHow` | `TestLinuxCloneDirectoryOpenFlagsStayByteIdentical` (WSL) | FAIL: `resolve 0xe, want resolve 0xf` |
| M5 drop `O_NOFOLLOW` from `linuxCloneConfiguredPathComponentFlags` | same pin (WSL) | FAIL: `component flags = 0x90000, want 0xb0000` |

M3 is why the pin carries an "unmanaged with repository" case: without it the mutant stayed
green, because the first case had an empty repository and root as well. Every mutant was
reverted through the same patch tool; `grep -rn MUTANT` over the package finds nothing and
the green runs below are taken on the reverted tree.

### Byte-compare surfaces

Error literals (`errors.New(` / `fmt.Errorf(` / `SubjectError{` / `Error() string` lines,
literals extracted and sorted, base vs HEAD):

| file | base literals | HEAD literals | diff |
| --- | --- | --- | --- |
| `linux_clone_fs_linux.go` | 195 | 194 | only the multiplicity of `"entry is not a real directory"` (2 → 1): the single opener produces the string both former callers wrapped |
| `subject.go` | 98 | 98 | empty |
| `workspace.go` | 101 | 101 | empty |
| `fingerprint.go` | 57 | 57 | empty |
| `linux_clone_linux.go` | — | — | empty |
| `artifact.go` | — | — | empty |

Call-site grep for the removed and inlined definitions:

| name | base | HEAD |
| --- | --- | --- |
| `openLinuxCloneOPathDirAt` | 1 definition, 2 production call sites, 2 test call sites | 0 |
| `openLinuxCloneDirAt` | 1 definition, 19 call sites (16 fs, 3 lifecycle) | 0 |
| `unix.Openat2` in `internal/workspace` | 3 (2 directory openers, 1 file opener) | 2 (`openLinuxCloneDirAtFlags`, `openLinuxCloneFileAt`) |
| `openLinuxCloneDirAtFlags` | — | 1 definition, 23 call sites (18 fs, 3 lifecycle, 2 tests) |
| `openLinuxCloneDirPath` / `openLinuxCloneOPathDirPath` | 2 definitions, 1 + 3 call sites | unchanged 3-line callers of the trusted walk |
| `materializeAtRepository` | 1 definition, 6 call sites | 0 |
| `materializeAdmittedSubject` | — | 1 definition, 6 call sites |

Flag / kind / identity lines (every line matching
`S_IFMT|S_IFDIR|O_CLOEXEC|O_NOFOLLOW|AT_SYMLINK_NOFOLLOW|sameLinuxCloneDirectoryIdentity`):

| file | base | HEAD | base-only lines and their HEAD equivalents |
| --- | --- | --- | --- |
| `linux_clone_fs_linux.go` | 70 | 68 | 7 base-only, 5 HEAD-only: the two opener `Flags:` lines become the one `linuxCloneDirOpenHow` line plus the same access flags at the 23 call sites; the two root `Open` lines become the same lines with `linuxCloneConfiguredPathRootFlags`; one duplicated dir-opener `S_IFMT != S_IFDIR` becomes the single copy in the one opener; the two component `Openat` lines become the one `openLinuxClonePinnedChild` line plus the same access flags at its two call sites |
| `linux_clone_linux.go` | 23 | 23 | identical set, `diff` empty |

The effective values behind every one of those lines are pinned by
`TestLinuxCloneDirectoryOpenFlagsStayByteIdentical` (openat2 `Flags`/`Resolve` for both
access flags plus the two descriptor-flag constants), and the identity rechecks
(`sameLinuxCloneDirectoryIdentity` and the uid/gid/mode-bit comparisons) are unchanged lines.

### dLOC (report-only per D2)

Production lines (non-comment, non-blank) over the six touched files: base 5676 → HEAD
5656, **−20 net** (`linux_clone_fs_linux.go` 1633 → 1626, `subject.go` 835 → 822, the other
four unchanged). Raw line counts rise by 20 because the new core, the flag constants and
the extracted functions carry doc comments. The contract's plan-row projection (−130…−175)
assumed the pre-review shape — a walker driver with policy callbacks plus the single
`requireManaged()` guard — and the binding E4 and walker-shape decisions remove both from
scope, so the delivered reduction is the fd-stepping core plus the materialize helper.

### Gates

All commands ran on the delivered tree unless the row names the base tree; `daemon/` is the
working directory for Go commands.

| gate | command | result |
| --- | --- | --- |
| format | `gofmt -l internal/workspace/` | empty, exit 0 |
| vet (host, module) | `go vet ./...` | exit 0 (28.2s) |
| vet (host, package) | `go vet ./internal/workspace/` | exit 0 |
| vet (linux, module) | `GOOS=linux GOARCH=amd64 go vet ./...` | exit 0 |
| vet (linux, package) | `GOOS=linux GOARCH=amd64 go vet ./internal/workspace/` | exit 0 |
| build windows | `GOOS=windows go build ./...` | exit 0 |
| build linux | `GOOS=linux go build ./...` | exit 0 |
| build linux/arm64 | `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 |
| focused windows pins | `go test -count=1 -v -run 'TestCleanupRefusesLinuxCloneWithoutStoreReadback\|TestFingerprintRefusesLinuxCloneIdentityMismatch\|TestVerifySubjectRefusesUnmanagedWorktree' ./internal/workspace/` | 3 PASS, exit 0 |
| linux suite (WSL, base tree) | `wsl -d Ubuntu-24.04 -u accept` → `cd /tmp/rf-w4-04-base/daemon && go test -count=1 -v ./internal/workspace/` | `ok 49.106s`, exit 0; PASS 91, FAIL 0, SKIP 2 |
| linux suite (WSL, HEAD) | same command in the worktree | `ok 95.511s`, exit 0; PASS 95, FAIL 0, SKIP 2 |
| skip list (identical both sides) | `TestLinuxCloneOPathRootAllowsMountedRootButForbidsNestedMount` — `bind mounts unavailable; mounted root traversal was verified: operation not permitted`; `TestSubjectMeasurementWitness` — requires `SYMMETRY_SUBJECT_WITNESS_*` | no new skips |
| new pins under Linux | `-run 'TestLinuxCloneDirectoryOpenFlagsStayByteIdentical'` (WSL) | PASS 0.04s, exit 0 |
| citation gate | `python scripts/ci/check-citation-existence.py --base c9ed23b4 --head HEAD` (and `--base b89e14d7`) | `no audited document added or modified … nothing to check`, exit 0 both |
| whitespace | `git diff --check` | exit 0 |
| host-path hygiene | `git diff c9ed23b4 \| grep -icE "users[/][a-z]"` | 0 |
| backslash-u hygiene | grep the delivered diff for the program rule's DLP trigger (a backslash immediately before a `u`) | 0 |
| removed-symbol leftovers | `grep -rn 'openLinuxCloneDirAt(\|openLinuxCloneOPathDirAt('` over the module | 0 |
| advisory lint (windows) | `golangci-lint run -c .golangci.yml --max-issues-per-linter=0 --max-same-issues=0 ./internal/workspace/...` | HEAD 14 issues (errcheck 1, errorlint 9, exhaustive 1, gocritic 1, gocyclo 1, staticcheck 1), exit 1; base 15 issues with the same set plus one `unparam` finding; normalized finding sets identical apart from that removal |
| host workspace suite (baseline, pre-change) | `go test -count=1 ./internal/workspace/` on `c9ed23b4` | `ok 164.123s`, exit 0 |
| host workspace suite (attempt 1) | same command on the delivered tree | TIMEOUT after 10m0s in `TestRecoverSubjectRevalidatesCommitAfterJournalLoss` → `prepareWorktreeAt` → `git worktree add` child (`CreateProcess`/`WaitForSingleObject`), exit 1 |
| host workspace suite (attempt 2) | same command | TIMEOUT after 10m0s in `TestPrepareReleasesCreatedReservationAfterFailedAdd` → `newRepository` → `git init` (`CreateProcess` stalled in the syscall), exit 1 |
| stall suspect isolated | `go test -count=1 -v -run 'TestRecoverSubjectRevalidatesCommitAfterJournalLoss' ./internal/workspace/` | PASS 17.07s, exit 0 |
| focused red/green set (host) | `go test -count=1 ./internal/workspace/ -run 'TestLinuxClone\|TestSubject\|TestWorkspace\|TestFingerprint'` | `ok 73.905s`, exit 0 |
| host app suite (attempt 1) | `go test -count=1 ./internal/app/...` | TIMEOUT after 10m0s in `TestDefinitiveStopRejectionClosesRetainedSession` → `state.ensurePrivateDirectory` → `os.MkdirAll` stalled in `CreateDirectory`, exit 1 |
| harness consumers (host) | `go test -count=1 ./internal/harness/pi/ ./internal/harness/omp/ ./internal/harness/omo/` | `ok` 16.326s / 16.280s / 13.875s, exit 0 |
| host workspace suite (retry, 30m budget) | `go test -count=1 -timeout 30m ./internal/workspace/...` | `ok 304.939s`, exit 0, 0 `--- FAIL` |
| host app suite (retry, 30m budget) | `go test -count=1 -timeout 30m ./internal/app/...` | `ok 438.903s`, exit 0, 0 `--- FAIL` |
| advisory lint base re-run | `golangci-lint run … --timeout 20m` in the extracted base tree | 15 issues, exit 1; the complete base side of the row above |

Load note for the two host timeouts: throughout these runs, other sessions on the same host
ran `app.test`, `cli.test`, `cargo` and `cargo-clippy` processes. Both stalls are inside
Windows process creation (`CreateProcess`) or a filesystem syscall (`CreateDirectory`) in
tests that do not touch this slice's code paths — one of them
(`TestPrepareReleasesCreatedReservationAfterFailedAdd`) precedes any subject materialization.
The same package was green on this host before the change (baseline row) and green on Linux
after it (WSL rows), and each stalled test passes in isolation. The first base lint run also
hit the config's 10-minute `run.timeout` under that load; the 20-minute re-run above is the
complete base side.

`docs/TODO.md` / `docs/archive/CLOSED-TODO.md`: no entry names this slice, its functions or
its tests, so nothing is closed or moved.

## Out of scope (with reasons)

- `linux_clone_linux.go`'s lifecycle/cleanup machinery beyond call-site flag plumbing: the
  contract excludes it.
- `artifact.go`, the clone-lifecycle phase resolver (F-18), `Manager.Cleanup` policy logic,
  execution and app callers: contract clause 4.
- `openLinuxCloneFileAt`: the caller-flags file opener, explicitly outside the unification.
- A shared `requireManaged()` predicate: dropped by E4; the sites' predicates differ.
- The walkers' policy bodies (mode/ownership checks, mount-crossing allowance for trusted
  ancestors): not extracted, because the two policies genuinely differ and the contract
  freezes them.
- `docs/TODO.md` / `docs/archive/CLOSED-TODO.md`: no entry names this slice, these
  functions or these tests (checked by grep for the function names), so nothing is closed
  or moved.

## Residual risks and limits

- The fd-stepping core is a two-function mechanic, not a loop driver: a driver with policy
  hooks was rejected because it would share the walks' control flow and move the parent
  close after the policy checks, which changes the per-component syscall sequence the
  contract freezes. The cost is that the two loops stay textual duplicates apart from the
  mechanics.
- The trusted walk's pre-stat and the owned walk's post-open verify are pinned by
  behaviour (`linux_clone_linux_test.go`) and by the mutant-based pins only indirectly:
  removing the pre-stat would change the trusted order without changing any refusal string
  this slice pins. The contract's mitigation is the audit table above plus the unchanged
  test bodies.
- Mount-crossing behaviour (`RESOLVE_NO_XDEV`) is asserted flag-level on every host and
  behaviourally only where the pre-existing bind-mount test is not skipped.
