# rf-w4-19 linuxpreflight package plan — extract the Linux host preflight verifiers out of package app

Contract: the GDD goal contract `goal-rf-w4-19-linuxpreflight-package.md` (2026-11-21, at the GDD
state root). Baseline for every coordinate in this plan: HEAD
`7954836892f39bc8a92abe11e5efcb5b3bac3476` (the dispatch base, the commit that landed W3-06/#166;
`origin/main` has since moved to `52bdffd2` — see "Recorded decisions"); the branch tree is that
commit plus this slice. Area: `daemon/internal/linuxpreflight/**` (new),
`daemon/internal/app/linux_owner_preflight*` (moved out of `app`), the call sites that consume the
verifiers (`daemon/internal/app/**`, `daemon/cmd/**`, one e2e test — see "Recorded decisions"), and
this plan file.

Root cause being removed (contract): `daemon/internal/app/linux_owner_preflight_linux.go` was 3380 L
of stateless Linux host/installation verifiers parked in `package app`. They do not belong to the
daemon run loop, they inflate `app`, and they are only reachable through `app`.

## Step 0 — premise audit (measured on the baseline tree)

**Inventory of `daemon/internal/app/linux_owner_preflight_linux.go` (3380 L).** 133 top-level
declarations: 119 functions, 1 method (`(*boundedLinuxOutput).Write`), `init`, 8 types
(`linuxOwnerAccounts`, `trustedLinuxAsset`, `linuxStatIdentity`, `boundedLinuxOutput`,
`linuxSystemdUnitPolicy`, `linuxSystemdExecProperty`, `linuxACLSubject`, `linuxACLEntry`), 3 const
blocks (bounds/names; `LinuxOwnerPreflightRuntimeBoundary`; the POSIX-ACL tag block) and 1 var block
(`errLinuxPreflightOutputTooLarge`). The remaining lines are the build tag, package clause and
import block.

**App-private declarations the file touches: three.**

| App-private declaration | Site | Consequence for the split |
| --- | --- | --- |
| `normalizeLinuxNativeStateDir` (var, `internal/app/lease.go:48`, default identity) | assigned by `init` at `linux_owner_preflight_linux.go:46`; called by `app.Run` at `internal/app/app.go:279` | the var is platform dispatch for one function; it is replaced by the package's exported `NormalizeOwnerStateDir`, which exists on every GOOS (linux+amd64: real implementation; elsewhere: identity passthrough, today's default) |
| `configuredHarnessKind(string) (harness.Kind, error)` (`internal/app/probe.go:123`) | called once, at `linux_owner_preflight_linux.go:359` inside `verifyLinuxNativeRuntimeSelection` | the configured-kind vocabulary; see "The runtime-selection vocabulary" below |
| `nativeGoalHarnessKind(harness.Kind) bool` (`internal/app/probe.go:156`) | called once, at `linux_owner_preflight_linux.go:363` inside `verifyLinuxNativeRuntimeSelection` | the native-goal harness classification; see below |

No reference to `*daemon`, `options`, `runningRun` or any other app-private type exists in the file
(`grep -n -w -e daemon -e options -e runningRun` over the baseline file matches only string literals,
comments and the `hasMountOption(options, expected string)` parameter).

**External callers of the file's declarations (baseline; the package interface must cover exactly
these).** Measured with `rg -n -w <decl>` over `daemon/**/*.go`:

| Caller | Sites | Declarations it calls |
| --- | --- | --- |
| `internal/app/linux_owner_v2_linux.go` | 17 | `VerifyLinuxOwnerInstallation`(90,211), `verifyLinuxOwnerIdentity`(56), `lstatCanonicalLinuxPath`(274,297), `linuxFileIdentity`(278,301), `linuxACLSubject`(282,283,305,310), `verifyLinuxPathTraversal`(284,311), `verifyEffectiveLinuxAccess`(287,290,306,314) |
| `internal/app/linux_owner_witness_linux.go` | 29 | `lstatCanonicalLinuxPath`(1232,1784,2206,2216,2555,2888), `linuxFileIdentity`(1192,1236,1356,1788,2210,2220,2559,2892), `linuxACLSubjectForAccount`(1201), `verifyEffectiveLinuxAccess`(1202,2573,2576,2912,2915), `verifyLinuxOwnerIdentity`(2312), `verifyLinuxOwnerStateDirBinding`(2563), `verifyLinuxOwnerAccounts`(2566,2905), `verifyLinuxTrustedPathAncestors`(2570,2909), `verifyLinuxDefaultLinuxAccess`(2579), `verifyLinuxNoUntrustedACLWrite`(2582,2918); plus `accounts.aSubject`/`accounts.bSubject` field reads on 7 lines (2570,2573,2576,2579,2909,2912,2915) |
| `internal/app/linux_owner_witness_linux_test.go` | 4 | `linuxStatIdentity`(592), `validateLinuxPiExtensionShareIdentity`(593,597,602) |
| `internal/app/linux_owner_recovery_linux.go` | 1 | `canonicalLinuxOwnerStateDir`(1576) |
| `internal/app/app.go` | 1 | `normalizeLinuxNativeStateDir`(279, the app var) |
| `cmd/symmetry-daemon/main.go` | 3 | `VerifyLegacyLinuxOwnersDrained`(255), `VerifyLinuxOwnerInstallation`(261), `LinuxOwnerPreflightRuntimeBoundary`(264) |
| `e2e/production_linux_v2_cleanup_witness_test.go` | 1 | `app.VerifyLinuxOwnerInstallation`(214) |

No other file in `daemon/**` references a declaration of this file. `internal/workspace/linux_clone_linux.go`
has its own unrelated `verifyLinuxCloneSource`; it is not a caller.

**CI pins: none.** None of the 24 test functions in `linux_owner_preflight_linux_test.go`, and no
mention of `linux_owner_preflight`, appears in `.github/workflows/ci.yml` or `scripts/**` (per-name
`grep -rl` over all 24 names, plus `grep -rn linux_owner_preflight .github scripts daemon/e2e`: no
match). No `--assert` list, `-run` alternation or `assert-*` script pins a moved test name, so no
escalation to the contract's "pinned test moved package" clause was needed; every test keeps its name
verbatim. The repository-level `go test ./...` race job and the ordinary package-scoped job both pick
the moved suite up unchanged (`./internal/linuxpreflight/...` is inside `./...`).

**Disjointness.** `gh pr view --json files` over the 20 open PRs: none touches
`daemon/internal/app/linux_owner_preflight*`, `daemon/internal/linuxpreflight`, or the call sites
(`daemon/internal/app/linux_owner_{v2,witness,recovery}*`, `daemon/cmd/symmetry-daemon/main.go`,
`daemon/internal/app/app.go`, `daemon/internal/app/lease.go`, the e2e cleanup witness). The last
change to the moved file on `origin/main` is `68176932` (#141, the landed W1-07 app/state hygiene
increment); the branch base is `79548368`.

## The runtime-selection vocabulary (the one app-state dependency, and the decision)

`verifyLinuxNativeRuntimeSelection` is the only declaration in the file that needs app state:

```go
kind, err := configuredHarnessKind(value.Runtime.HarnessKind)      // internal/app/probe.go:123
if err != nil { return fmt.Errorf("select configured Linux native harness: %w", err) }
if !nativeGoalHarnessKind(kind) || kind == "generic" { return fmt.Errorf("Linux owner preflight rejects generic/native-fallback harness selection %q", value.Runtime.HarnessKind) }
```

It runs at step 6 of `VerifyLinuxOwnerInstallation`, after `value.Validate()` (step 1), the StateDir
canonicalization (2) and the installed owner configuration/identity/accounts checks (3-5), so the
resolution cannot be hoisted into a caller without reordering error precedence.

Facts verified on the baseline tree before deciding:

- `internal/config` already carries the same vocabulary independently: `normalizeRuntimeAdapterMetadata`
  (`internal/config/config.go:305-323`) validates the identical 7-kind set and produces the identical
  `runtime.harness_kind %q is unsupported` text, and `Config.Validate()` calls it (`config.go:241`).
  A package-local copy is therefore consistent with an existing two-owner duplication, not a new
  convention.
- `internal/harness` (importable by the package) defines `type Kind` and the seven `Kind*` constants
  but has no string→kind parser and no native-goal classification.
- `linux_owner_preflight_linux_test.go:305-322` (moved with the code) drives
  `verifyLinuxNativeRuntimeSelection` directly, so the vocabulary must exist *inside the package*
  regardless of how production supplies it.

Decision: **copy the two pure helpers verbatim into `cgroup_linux.go` as unexported functions**, with
a doc comment naming both owners, and keep the entry-point signature `VerifyLinuxOwnerInstallation(config.Config) error`
unchanged. Consequences: cmd/e2e/app-internal call sites become plain import + call renames; no
app-side wrapper keeps the verifier's name; no package-level mutable state; the step-6 check runs
where it ran, with the same bytes.

Alternatives considered and rejected (recorded, with the evidence that decided them):

| Alternative | Why rejected |
| --- | --- |
| Eager resolution in an app-side thin caller passing the resolved values in (`VerifyLinuxOwnerInstallation(value, nativeHarness bool)`) | the resolution would run before step 1, so a config that fails both `Validate()` and the harness check returns the harness error first; measured against the contract's byte-identical check-order clause this is a behaviour change |
| Lazy resolver parameter (`VerifyLinuxOwnerInstallation(value, resolve func(string)(harness.Kind,bool,error))`) | keeps order without duplication, but changes the entry signature the contract fixes ("entry points taking a plain config value"), adds an exported type, and leaves `app.VerifyLinuxOwnerInstallation` as a wrapper — the entry point does not move for `cmd`/`e2e` |
| Injectable package-level vocabulary function installed by `app`'s `init` | entry signature and call sites survive, but it adds mutable exported state to the package interface and makes package correctness depend on an unrelated import keeping `app` linked; the package's own tests cannot import `app` (test import cycle) and would have to mutate the global |
| Move the vocabulary into `internal/harness` and rewire its consumers | correct long-term home, but `internal/harness`, `internal/app/probe.go` and `internal/app/goal_admission.go` are outside this slice's owned set |
| Keep the whole verifier in `app` | contradicts the contract's observable pass condition (the verifiers must be in the package behind its entry points) |

Drift is fail-closed (a kind the copy does not know is rejected at step 6, never admitted), and the
copy is pinned by the new table test `TestNativeRuntimeSelectionVocabularyMatchesTheConfiguredKinds`
(all seven configured kinds resolve; the unknown-kind message is byte-exact). The copied message is
byte-identical to `internal/app/probe.go:140` and `internal/config/config.go:323` (verified).

Step-6 detail preserved verbatim: the `|| kind == "generic"` disjunct is unreachable, because the
native-goal set already excludes generic; it is kept byte-for-byte and the M2 mutation below confirms
that dropping it is behaviour-preserving.

Residual risk and follow-up candidate (recorded, not owned here): the tree now carries three copies
of the kind table (`internal/config`, `internal/app/probe.go`, this package). The clean fix is one
`internal/harness` vocabulary (`ParseKind` plus the native-goal predicate) consumed by all three; that
touches `internal/harness`, `internal/app/probe.go` and `internal/app/goal_admission.go`, all outside
this slice's owned set, so it is left to a separate increment.

## Public / API shape

Three config-valued entry points, matching the contract's 2-4 target, plus exactly the primitives
that existing external callers use today (contract: "no exported per-check helpers unless an existing
external caller needs them"). Every exported symbol below is listed with its external call sites; no
other declaration of the moved file becomes exported.

| Exported symbol (package `linuxpreflight`) | Baseline name | External call sites |
| --- | --- | --- |
| `VerifyLegacyLinuxOwnersDrained(config.Config) error` | same | `cmd/symmetry-daemon/main.go:255` |
| `VerifyLinuxOwnerInstallation(config.Config) error` | same | `cmd/symmetry-daemon/main.go:261`, `internal/app/linux_owner_v2_linux.go:90,211`, `e2e/production_linux_v2_cleanup_witness_test.go:214` |
| `NormalizeOwnerStateDir(string) (string, error)` | `canonicalLinuxOwnerStateDir` (unexported) | `internal/app/app.go:280` (via the deleted var), `internal/app/linux_owner_recovery_linux.go:1577` |
| `LinuxOwnerPreflightRuntimeBoundary` (const) | same | `cmd/symmetry-daemon/main.go:264` |
| `VerifyOwnerIdentity(linuxgate.OwnerConfig, uid, euid, gid, egid uint32) error` | `verifyLinuxOwnerIdentity` | `internal/app/linux_owner_v2_linux.go:56`, `linux_owner_witness_linux.go:2312` |
| `VerifyOwnerAccounts(linuxgate.OwnerConfig) (OwnerAccounts, error)` | `verifyLinuxOwnerAccounts` | `linux_owner_witness_linux.go:2566,2905` |
| `OwnerAccounts` (type; fields `A`, `B *user.User`, `ASubject`, `BSubject ACLSubject`) | `linuxOwnerAccounts` | `linux_owner_witness_linux.go:2566,2570,2573,2576,2579,2905,2909,2912,2915` |
| `ACLSubjectForAccount(*user.User) (ACLSubject, error)` | `linuxACLSubjectForAccount` | `linux_owner_witness_linux.go:1201` |
| `ACLSubject` (type; fields `UID`, `GID uint32`, `Groups []uint32`) | `linuxACLSubject` | `linux_owner_v2_linux.go:282,283,305,310`, `linux_owner_witness_linux.go:1202,2570,2573,2576,2579,2909,2912,2915` |
| `StatIdentity` (type; fields `UID`, `GID`, `Mode uint32`, `Links uint64`) | `linuxStatIdentity` | `internal/app/linux_owner_witness_linux_test.go:593` |
| `LstatCanonicalPath(string) (os.FileInfo, error)` | `lstatCanonicalLinuxPath` | `linux_owner_v2_linux.go:274,297`, `linux_owner_witness_linux.go:1232,1784,2206,2216,2555,2888` |
| `FileIdentity(os.FileInfo) (StatIdentity, error)` | `linuxFileIdentity` | `linux_owner_v2_linux.go:278,301`, `linux_owner_witness_linux.go:1192,1236,1356,1788,2210,2220,2559,2892` |
| `VerifyEffectiveAccess(string, ACLSubject, required, forbidden uint16) error` | `verifyEffectiveLinuxAccess` | `linux_owner_v2_linux.go:287,290,306,314`, `linux_owner_witness_linux.go:1202,2573,2576,2912,2915` |
| `VerifyDefaultAccess(string, ACLSubject, required, forbidden uint16) error` | `verifyLinuxDefaultLinuxAccess` | `linux_owner_witness_linux.go:2579` |
| `VerifyNoUntrustedACLWrite(string, trustedUIDs, trustedGIDs []uint32, includeDefault bool) error` | `verifyLinuxNoUntrustedACLWrite` | `linux_owner_witness_linux.go:2582,2918` |
| `VerifyPathTraversal(string, ACLSubject) error` | `verifyLinuxPathTraversal` | `linux_owner_v2_linux.go:284,311` |
| `VerifyOwnerStateDirBinding(string, linuxgate.OwnerConfig) error` | `verifyLinuxOwnerStateDirBinding` | `linux_owner_witness_linux.go:2563` |
| `VerifyTrustedPathAncestors(string, ownerUID, ownerGID uint32, b *ACLSubject) error` | `verifyLinuxTrustedPathAncestors` | `linux_owner_witness_linux.go:2570,2909` |
| `ValidatePiExtensionShareIdentity(bool, StatIdentity, linuxgate.OwnerConfig) error` | `validateLinuxPiExtensionShareIdentity` | `linux_owner_witness_linux_test.go:593,597,602` |

Naming rule applied: the exported name drops the `linux` platform qualifier where the package name
already carries it (`linuxpreflight.VerifyEffectiveAccess`) and keeps the subsystem qualifier
(`VerifyOwnerStateDirBinding`, `ACLSubjectForAccount`). Types keep their noun; their fields are
exported because the external callers read them. `hasGroup`, `linuxACLEntry`, the ACL tag constants,
the copied vocabulary and every other declaration stay unexported.

The package builds on every GOOS the daemon builds on:

- `linux && amd64` (today's constraint, unchanged): the moved verifiers.
- `!linux || !amd64`: `preflight_unsupported.go`, carrying today's
  `internal/app/linux_owner_preflight_unsupported.go` text for the two verifiers and the boundary
  constant (string literals byte-identical, verified), plus `NormalizeOwnerStateDir` as the identity
  passthrough that is today's `normalizeLinuxNativeStateDir` default. Behaviour on those platforms is
  unchanged; the app-side var and its linux-only `init` disappear.

## The split (file table)

Declarations were assigned by concern along their own call graph; shared plumbing (bounded I/O,
identity stat, trusted-path/executable checks, ACL primitives used by several concerns) lives in the
entry-point file, and nothing was assigned twice. Line counts are `wc -l` on the final tree.

| File | Declarations | Lines | Concern |
| --- | --- | --- | --- |
| `preflight_linux.go` | 40 | 902 | entry points, boundary constant, bounds const block, bounded I/O, stat identity, trusted assets/executables/paths, state-dir canonicalization and drain scan, workspace/Pi-share checks |
| `accounts_linux.go` | 11 | 358 | UID/GID/group-db accounts: identity, process IDs, group membership, getent lookup, account HOME/credential path |
| `cgroup_linux.go` | 11 | 269 | parent-kill boundary, runtime cgroup selection (+ the copied harness vocabulary), owner anchor discovery, delegated parent identity, anchor process context |
| `acl_linux.go` | 17 | 335 | POSIX ACL parser, effective-permission evaluator, untrusted-writer checks, ACL shape validation |
| `git_ownership_linux.go` | 24 | 767 | Git/clone-seed/repository ownership, hooks, Git config parser, A-controlled Git path rules, path traversal |
| `systemd_linux.go` | 31 | 878 | systemd directive/drop-in parser, candidate/override validation, `systemctl show` effective-state verification, service-path checks |
| `preflight_unsupported.go` | 4 exports | 41 | non-linux/non-amd64 stub of the public surface |
| `preflight_linux_test.go` | 25 tests | 973 | the moved test file (`linux_owner_preflight_linux_test.go`) plus the vocabulary pin test |

Each of the six implementation files stays under 1000 L and no file in the result exceeds 1500 L
(largest: the test file at 973). `init` (4 L) is deleted: its only statement wired the app var that
no longer exists. The prelude (build tag, package clause, import block) is redistributed as per-file
headers; doc comments travel with the declaration they document.

Moved declarations that are *not* exported keep their baseline names byte-for-byte. Exported ones
appear in the table above; the declaration-set table in the evidence section accounts for all 133.

## Edge-case semantics the contract demands

- **Error strings byte-identical.** Every string literal of the moved file is compared base vs head as
  a multiset: **0 base literals removed or changed** (1093 base literals, 1098 head literals across
  the six implementation files). The 5 head-side additions are enumerated in the evidence section.
  The three stub literals and the copied vocabulary message are byte-identical to their owners.
- **Check order byte-identical.** A base-vs-HEAD comparison of every declaration's full text (doc
  comment, signature, body and statement skeleton) after the documented rename map: 132 of 132
  comparable declarations identical, 0 changed. This is the lesson-32 audit (dropped early returns /
  branches), run per extracted helper rather than only per entry point.
- **No app state in the package.** `go list -deps ./internal/linuxpreflight | grep internal/app` is
  empty; the package imports only `config`, `authority`, `harness`, `platform`, `state`, `linuxgate`
  protocol and `golang.org/x/sys/unix`.
- **No residue in `app`.** `linux_owner_preflight_linux.go`,
  `linux_owner_preflight_unsupported.go` and `linux_owner_preflight_linux_test.go` are deleted;
  `internal/app` declares no verifier, no stub for one and no wrapper with a verifier's name.
- **Build-tag parity.** The moved code keeps `linux && amd64`; all 24 moved tests keep their names; the
  moved suite runs `go test -count=1 ./internal/linuxpreflight/... ./internal/app/...` on linux/amd64
  with no new SKIPs against the baseline `./internal/app/...` run.
- **No weakened validation.** No predicate, permission mask, bound constant or ownership rule is
  edited; the only edits to moved text are identifier renames, the deleted `init`, and the new file
  headers. The two added helpers are copies, not reimplementations.
- **No new suppressions.** No `//nolint`, no lint-config change, no test deletion. The moved test file
  gains one pin test; nothing is removed.

## Design trade-offs

- **Exported primitives instead of app-side wrappers.** The witness/v2/recovery files reuse 13
  verifier primitives today. Keeping them in `app` as thin delegating wrappers would be a
  wrapper-only layer (repo contract: do not add wrapper-only layers; owner rule: no backward
  compatibility needed) and would leave the primitives untestable outside `app` — the exact defect
  this slice removes. Exporting exactly the 13 (each with a listed call site) keeps one
  implementation, zero wrappers, and the entry-point target intact.
- **`NormalizeOwnerStateDir` instead of the app var.** The var existed only to dispatch one function
  by platform; the package now provides both platforms, so `app.Run` and recovery call the package
  directly and the var (plus its `init`) is deleted.
- **Vocabulary copy over interface change** (section above): the copy is the only option that keeps
  both the entry signature and the step-6 position; it is pinned and fail-closed, and it mirrors a
  duplication the repository already carries between `app` and `config`.
- **Two drain tests move with a local fixture.** `TestLegacyOwnerDrainUsesExclusiveStoreAndDoesNotWriteJournals`
  drives the unexported `verifyDrainedLinuxStore` (a lock-conflict re-entry cannot reach the scan
  path), so it moves with the code. The two drain tests need a claimed-store journal fixture that
  `app_test.go` owns; the moved test file carries a package-local copy of that 22-line helper
  (`claimedStoreAt`) rather than keeping the tests in `app` or exporting a store-scan entry point.
  `app`'s own helper stays for the app suite; the fixture is test-only duplication, recorded here
  because two packages can no longer share test helpers.
- **`systemd` stays whole.** The systemd directive parser, candidate validator and effective-state
  verifier share the `linuxSystemdUnitPolicy` value and the directive map; splitting them further
  would serialize one concern across files for no size win (878 L < 1000 L).
- **Renames are mechanical.** No moved body is otherwise edited, so the diff is attributable: the
  declaration-set and control-flow audits compare renamed text, not eyeballed similarity.

## Recorded decisions where the contract was silent

- **The e2e call site is renamed.** `daemon/e2e/production_linux_v2_cleanup_witness_test.go:214`
  consumes `VerifyLinuxOwnerInstallation`; it is outside `daemon/internal/app/**` and
  `daemon/cmd/**`, but leaving it would break the module build and keeping an `app` wrapper would
  contradict "no backward compatibility" and the failure control. One import + call rename (and the
  now-unused `internal/app` import is dropped from that file).
- **`linux_owner_preflight_unsupported.go` is deleted.** The owned set names the `_linux.go` file and
  its tests; the unsupported stub defines the same exported API in `app` and cannot survive the move
  (it would shadow the package or keep the verifier surface in `app`). Its text is preserved in
  `linuxpreflight/preflight_unsupported.go` with `NormalizeOwnerStateDir` added.
- **Test file name.** `daemon/internal/linuxpreflight/preflight_linux_test.go` (the moved file); the
  contract fixes the implementation file names, not the test file name.
- **`origin/main` moved during implementation.** The dispatch base is `79548368`; `origin/main` is
  now `52bdffd2` (#165, #137). Neither landed commit touches any path this slice owns
  (`git log --oneline 79548368..origin/main -- <owned paths>` is empty), so the slice stays mergeable
  alone and every range gate above is run against the true base `79548368`, not the moved ref.
- **Snapshot artifact.** The temp-index commit drops the executable bit on
  `scripts/audit-no-legacy-routes.sh` (a Windows fresh-index artifact); the worktree and index keep
  `100755`, and no other file differs. It is named here so the snapshot diff is not misread.

- **Historical plans are not rewritten.** `docs/plans/2026-11-21-daemon-app-state-hygiene-plan.md`
  (landed as 68176932/#141) and the `docs/evidence/goal-0006b-*` / `goal-0009-source-identity-*`
  records name the old path; they are frozen records of the revisions they describe, and no script
  reads them for this change. `docs/TODO.md` and `docs/archive/CLOSED-TODO.md` carry no entry for
  this file, so nothing moves. W3-09's contract names the old path: retargeting it to
  `internal/linuxpreflight` is that slice's edit, as the contract's sequencing note records.

## Evidence plan and measured results (final tree)

Runnable from `daemon/` unless noted; the linux runs go through
`wsl -d Ubuntu-24.04 -u accept`.

| Gate | Command | Result |
| --- | --- | --- |
| format | `gofmt -l .` | empty, exit 0 |
| vet (windows host default) | `go vet ./...` | exit 0 |
| vet (linux target) | `GOOS=linux go vet ./...` | exit 0 |
| builds | `GOOS=linux go build ./...`; `GOOS=windows go build ./...`; `GOOS=linux GOARCH=arm64 go build ./...`; `GOOS=darwin go build ./...` | exit 0 each |
| moved suite (worktree path) | `go test -count=1 ./internal/linuxpreflight/...` via WSL in `daemon/` | PASS, exit 0 (0.30 s; 25 tests / 55 events, 0 skips) |
| affected app surface, differential | `go test -count=1 -timeout 840s -run '<linux-owner/v2/clone/systemd/trusted/ACL/recovery pattern>' ./internal/app/` on WSL ext4 copies of `79548368` and of the worktree | PASS on both sides, exit 0 each, 0 skips, 0 failures: 140 passing tests at the base and 87 at the head, the difference being exactly the 53 events that moved into `internal/linuxpreflight`; the skip sets are identical |
| full app suite, bounded attempts | `go test -count=1 -timeout 1440s ./internal/app/...` on the ext4 copy of the worktree, and `-timeout 900s` at the base on drvfs | environment-limited, not a code result: both stop in `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart/mixed_null_marker_survives_replay_and_restart` with a net/http transport goroutine dump (reproduced in isolation on ext4: FAIL after its 170 s test timeout, exit 1). That test is untouched by this slice and hangs identically at the base, so the ordinary full linux pass is recorded as pending the CI runner; the moved suite and the affected-surface differential above carry the evidence |
| skip list (all runs) | `-json` event scan over every linux run above | 0 skips everywhere (base and head, drvfs and ext4, moved suite and affected subset): no new skips and none removed |
| CLI differential smoke | base and head daemon binaries built from `/tmp` copies (`origin/main` and the worktree), then `-verify-legacy-owner-drained` and `-verify-linux-owner-install` against equivalent private StateDirs under `/var/lib` on WSL | identical output bytes on both commands: drain exit 0 with the same 68-byte success line on stdout, install exit 1 with the same 141-byte refusal on stderr. The install run reaches the moved entry point's step 4 (`VerifyOwnerIdentity`) through steps 1-3 (`value.Validate`, `NormalizeOwnerStateDir`, `ReadInstalledOwnerConfig`), so the moved dispatch, canonicalization and identity predicate are exercised end to end; the drvfs baseline attempt below is the same command class on the mount |
| baseline attempt on drvfs | `go test -count=1 -timeout 900s ./internal/app/...` at the base tree (WSL, `/mnt/q`) | host-limited: `-timeout` panic after 29 of the app tests (`.../TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart` still running, net/http goroutine dump). The same suite is re-run on an ext4 copy for a conclusive result; the moved suite above runs on drvfs in 0.3 s, so the limit is the app suite's I/O on the 9p/drvfs mount, not this slice |
| vocabulary pin sensitivity | `go test -overlay` against `cgroup_linux.go` (WSL, worktree path) | M1 (drop the `config.RuntimeHarnessOpenCode` arm) red: `configuredHarnessKind("opencode") = ("", runtime.harness_kind "opencode" is unsupported), want "opencode"`, exit 1; M3 (map codex to the generic kind) red: `configuredHarnessKind("codex") = ("generic", <nil>), want "codex"`, exit 1; M2 (drop the `|| kind == "generic"` disjunct) green, exit 0 — an equivalent mutant, as recorded under the step-6 note; clean-tree control exit 0 |
| no app import | `go list -deps ./internal/linuxpreflight \| grep internal/app` | empty (count 0), exit 1 as expected |
| file size | `wc -l daemon/internal/linuxpreflight/*.go` | max 973 (test file), max implementation file 902; all < 1000 |
| citations | `python3 scripts/ci/check-citation-existence.py --base 79548368 --head <snapshot>` (a temp-index commit of the working tree: the slice is deliberately uncommitted) | exit 0: no runbook, `docs/design/quality.md` or `docs/archive/CLOSED-TODO.md` entry added or modified. The fixture suite `python3 scripts/ci/test-check-citation-existence.py` passes (49 tests, exit 0) |
| host-path hygiene | `git diff 79548368 <snapshot> \| grep -icE "users[/][a-z]"` (bracket form, so the row cannot match itself) | 0 matches; no committed path carries a user-profile name |
| whitespace | `git diff --check 79548368 <snapshot>` | exit 0 (no whitespace errors across the slice, new files included) |
| declaration set | base file vs head package, per-declaration text + statement skeleton, after the rename map | 132/132 identical, 0 changed, 1 deleted (`init`), 2 added (vocabulary copies) |
| error strings | base file vs head package, string-literal multiset | 1093 base literals, 0 removed or changed; 5 enumerated additions |
| control flow | the same per-declaration skeleton comparison (see declaration set) | 0 differences |

Declaration-set accounting (`go run` audit tool, described below):

| Category | Count | Detail |
| --- | --- | --- |
| moved byte-identical, no rename | 64 | names and text identical |
| moved, name changed unexported→exported | 17 | the 16 renamed declarations of the interface table plus `func linuxACLSubject.hasGroup` → `func ACLSubject.hasGroup`, whose key carries the renamed receiver type |
| moved, body text updated only by those renames | 51 | normalized text identical (fields, selectors, composite-literal keys) |
| deleted with reason | 1 | `func init` (wired the deleted app var) |
| added unexported | 2 | `configuredHarnessKind`, `nativeGoalHarnessKind` (verbatim vocabulary copies) |
| total baseline declarations | 133 | 64 + 17 + 51 + 1 |

Error-string additions on the head side (all accounted for):

| Added literal | Why | Identity check |
| --- | --- | --- |
| `"runtime.harness_kind %q is unsupported"` | copied vocabulary | byte-identical to `internal/app/probe.go:140` and `internal/config/config.go:323` |
| `""` | operand of that copied error return | part of the copied function |
| the boundary const string | the non-linux stub carries its own copy, as the deleted app stub did | byte-identical to the base file and stub |
| two stub refusal strings | the non-linux stub | byte-identical to the deleted `internal/app/linux_owner_preflight_unsupported.go` |

Audit tooling (provenance, reviewer-side): a ~150-line stdlib-only Go program parses each side with
`go/parser`, prints every top-level declaration (doc comment included) plus a recursive statement
skeleton (statement kinds, `if`/`for`/`switch` conditions, `return` operands), and lists every string
literal per file. The head side is normalized through the inverse of the rename map
(`StatIdentity.UID`→`linuxStatIdentity.uid`, `NormalizeOwnerStateDir`→`canonicalLinuxOwnerStateDir`,
`accounts.BSubject`→`accounts.bSubject`, …) before comparing, so "identical" means identical apart
from the documented renames. It lives outside the repository (build-slice scratch, provenance like
the W2-14 splitter), and the plan records its algorithm and its outputs above.

The moved test file's literal delta is separately accounted: 1 changed literal
(`canonicalLinuxOwnerStateDir(%q)…` → `NormalizeOwnerStateDir(%q)…`, a test failure message), plus the
new pin test's messages and the copied `claimedStoreAt` fixture's 10 literals.

## Out of scope (with reasons)

- `linux_owner_witness_linux.go`, `linux_owner_witness_unsupported*.go` and their tests (deferred by
  the contract): only the call renames listed above touch the witness.
- `linux_owner_v2_linux.go`, `linux_owner_recovery_linux.go`, run lifecycle, `linux_owner_witness_linux.go`
  behaviour: call renames only.
- Any behaviour change: no predicate, bound, ordering or message edit; the contract forbids it.
- `internal/workspace`, `internal/platform`, `internal/authority`, `internal/state`, `internal/harness`,
  `internal/config`, `internal/app/probe.go`, `internal/app/goal_admission.go`: untouched.
- New packages: none beyond `internal/linuxpreflight` (a second shared-primitives package is not in
  the owned set, and the primitives have exactly one shared home now).
