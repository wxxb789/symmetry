# rf-w4-19 linuxpreflight package plan — extract the Linux host preflight verifiers out of package app

Contract: the GDD goal contract `goal-rf-w4-19-linuxpreflight-package.md` (2026-11-21, at the GDD
state root), sha256 `b5a2176e7ca6a20b9424303c2ca026ea653cbe1daf84391d6d6f4ca97cbaafba` (the BOUND
binding; recorded here per round-1 review P3-1; `sha256sum` matches the `in-flight` row of the GDD
state root's `refactor/arch-decision-2-contracts.tsv`). Baseline for every coordinate in this plan: HEAD
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
declarations: 118 functions, 2 methods (`(*boundedLinuxOutput).Write`,
`(linuxACLSubject).hasGroup`), `init`, 8 types
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

New kinds fail closed (a kind the copy does not know is rejected at step 6, never admitted); the
reverse direction is not guarded — if `app` dropped a kind from its native-goal set, preflight would
still accept it, though the runtime would still refuse it. The copy is pinned by the new table test
`TestNativeRuntimeSelectionVocabularyMatchesTheConfiguredKinds` to a hard-coded seven-kind literal
table, **not** to `app`'s copy, so drift between the two copies goes undetected (round-1 review
P3-2 corrected this paragraph's earlier "drift fail-closed and pinned" wording). The copied message
is byte-identical to `internal/app/probe.go:140` and `internal/config/config.go:323` (verified).

Step-6 detail preserved verbatim: the `|| kind == "generic"` disjunct is unreachable, because the
native-goal set already excludes generic; it is kept byte-for-byte and the M2 mutation below confirms
that dropping it is behaviour-preserving.

Residual risk and follow-up candidate (recorded, not owned here): the tree now carries three copies
of the kind table (`internal/config`, `internal/app/probe.go`, this package). The clean fix is one
`internal/harness` vocabulary (`ParseKind` plus the native-goal predicate) consumed by all three; that
touches `internal/harness`, `internal/app/probe.go` and `internal/app/goal_admission.go`, all outside
this slice's owned set, so it is left to a separate increment. The conductor recorded this follow-up
in `docs/TODO.md` ("Deferred review findings (PR #197)") per round-1 review P3-2.

## Public / API shape

Three entry points — the two config-valued verifiers plus the `NormalizeOwnerStateDir` state-dir
normalizer — matching the contract's 2-4 target, plus exactly the primitives
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
| `OwnerAccounts` (type; exported fields `ASubject`, `BSubject ACLSubject`; `a`, `b *user.User` unexported) | `linuxOwnerAccounts` | `linux_owner_witness_linux.go:2566,2570,2573,2576,2579,2905,2909,2912,2915` |
| `ACLSubjectForAccount(*user.User) (ACLSubject, error)` | `linuxACLSubjectForAccount` | `linux_owner_witness_linux.go:1201` |
| `ACLSubject` (type; exported fields `UID`, `GID uint32`; `groups []uint32` unexported) | `linuxACLSubject` | `linux_owner_v2_linux.go:282,283,305,310`, `linux_owner_witness_linux.go:1202,2570,2573,2576,2579,2909,2912,2915` |
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
(`VerifyOwnerStateDirBinding`, `ACLSubjectForAccount`). Types keep their noun, and only the fields
external callers use are exported (`OwnerAccounts.ASubject`/`BSubject`, `ACLSubject.UID`/`GID`,
`StatIdentity`'s four): `OwnerAccounts.a`/`b`, `ACLSubject.groups`, `hasGroup`, `linuxACLEntry`, the
ACL tag constants, the copied vocabulary and every other declaration stay unexported.

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
| `preflight_unsupported_test.go` | 2 tests | 34 | apply-pass pins for the non-linux/non-amd64 stub: identity passthrough and both refusal strings (finding 5) |

Each of the six implementation files stays under 1000 L and no file in the result exceeds 1500 L
(largest: the test file at 973). `init` (4 L) is deleted: its only statement wired the app var that
no longer exists. The prelude (build tag, package clause, import block) is redistributed as per-file
headers; doc comments travel with the declaration they document.

Moved declarations that are *not* exported keep their baseline names byte-for-byte. Exported ones
appear in the table above; the declaration-set table in the evidence section accounts for all 133.

## Edge-case semantics the contract demands

- **Error strings byte-identical.** Every string literal of the moved file is compared base vs head as
  a multiset: **0 base literals removed or changed** (1093 base literals, 1095 head literals across
  the six implementation files — 1098 including the stub's three). The 5 head-side additions (2 in
  the implementation files, 3 in the stub) are enumerated in the evidence section.
  The three stub literals and the copied vocabulary message are byte-identical to their owners.
- **Check order byte-identical.** A base-vs-HEAD comparison of every declaration's full text (doc
  comment, signature, body and statement skeleton) after the documented rename map: 132 of 132
  comparable declarations identical, 0 changed. This is the lesson-32 audit (dropped early returns /
  branches), run per extracted helper rather than only per entry point. The apply pass drops one unused
  parameter (`owner`, finding 1): the reconstruction check in the apply section shows the two touched
  declarations differ only in the signature line and the rewritten call argument, with every payload
  line and the statement skeleton byte-identical.
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
  edited; the only edits to moved text are identifier renames, the deleted `init`, the dropped unused
  parameter of the apply pass (finding 1), and the new file headers. The two added helpers are copies,
  not reimplementations.
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
  `app_test.go` owns; the moved test file carries a package-local copy of that 20-line helper
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
  now-unused `internal/app` import is dropped from that file). The conductor ACCEPTS this
  owned-files scope extension because the alternative is a module that does not compile:
  **conductor-accepted scope extension (review P3-3)**.
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

## Evidence plan and measured results (move-only revision; the apply pass below re-derives the rows it touched)

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
| citations | `python3 scripts/ci/check-citation-existence.py --base 79548368 --head <snapshot>` (a temp-index commit of the working tree: the slice is deliberately uncommitted) | exit 0 on the apply snapshot: the only changed document in the range is `docs/archive/CLOSED-TODO.md` (landed by the merged `f4e4ace6`, #137 — not this slice), passing with 453 path citations and 3 ignore-rule notes. The fixture suite `python3 scripts/ci/test-check-citation-existence.py` re-run on the apply snapshot: 49 tests, exit 0 |
| host-path hygiene | `git diff a4be4e26 <snapshot> \| grep -icE "users[/][a-z]"` (bracket form, so the row cannot match itself; `a4be4e26` is `origin/main` at the apply dispatch, so the range holds this slice's own bytes) | 0 matches; no committed path carries a user-profile name. The wider `79548368` range also traverses later-merged main commits whose own escape-fixture files spell host-shaped test data under a generic two-letter user (`f4e4ace6`, #137); those bytes are not this slice's |
| whitespace | `git diff --check 79548368 <snapshot>` | exit 0 (no whitespace errors across the slice, new files included) |
| declaration set | base file vs head package, per-declaration text + statement skeleton, after the rename map; re-derived after the apply pass for the one touched declaration | 132/132 identical after the rename map, 0 changed, 1 deleted (`init`), 2 added (vocabulary copies); the apply pass adds one signature delta (`verifyTrustedLinuxConfig` drops the unused `owner` parameter, finding 1) plus its rewritten call argument in `verifyInstalledLinuxOwnerAssets` — body and statement skeleton byte-identical, reconstruction proof in the apply section |
| error strings | base file vs head package, string-literal multiset | 1093 base literals, 0 removed or changed; 1095 head literals across the six implementation files (1098 including the stub's three); 5 enumerated additions (2 in-implementation, 3 stub) |
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

Error-string additions on the head side (all accounted for; two in the implementation files, three
in the stub):

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

## Apply pass (2026-11-21, GDD apply stage — findings 1, 4, 5 and D2 applied; findings 2, 3 retained)

The simplify review recommended findings 1 + 4 (+5) and the conductor's apply brief accepts every finding
inside the owned set or covered by a conductor amendment, rejecting only for weakened tests/types/validation/
permissions or out-of-set files. D2 is applied under the BOUND arch-decision-2 R9 instruction
(`refactor/arch-decision-2.md` §R9 applies to in-flight W4-19 and directs SOURCE_PATHS updates per slice).

| Finding | Decision | Change / reason | Gate |
| --- | --- | --- | --- |
| 1 `verifyTrustedLinuxConfig` unused `owner` (`unparam`) | applied | parameter and its call argument dropped (`preflight_linux.go:175,195`); no other text touched | reconstruction check below; advisory lint 33→32 with the default caps and `unparam` 1→0; the CI-flag run reports 0 `unparam` |
| 2 `claimedStoreAt` fixture copy | retained (SKIP) | removal needs a shared test-helper package outside the owned set and would replace test support that pins the drain contract | unchanged |
| 3 configured-kind vocabulary copy | retained (SKIP) | the one-vocabulary fix needs `internal/harness`, `internal/app/probe.go` and `internal/app/goal_admission.go` (outside the owned set); recorded follow-up, drift fail-closed and pinned | unchanged |
| 4 no test binds the rewired `app.Run` normalization (amendment 1, D1) | applied | new `daemon/internal/app/linux_owner_preflight_state_dir_app_test.go` (`linux && amd64`, 65 L): four subtests pin the branch, the exact wrapped refusals of `app.go:282`, the normalize-before-validate order and the non-branch for other kinds | WSL `-run` exit 0, 4/4 subtests pass, 0.067 s |
| 5 non-linux/non-amd64 stub unpinned | applied | new `daemon/internal/linuxpreflight/preflight_unsupported_test.go` (`!linux \|\| !amd64`, 34 L): identity passthrough for five paths and both refusal strings byte-pinned | Windows host `go test ./internal/linuxpreflight/...` exit 0, 2/2 pass |
| D1 (amendment-1 test missing) | closed | by findings 4 + 5: the linux/amd64 branch wiring and the non-linux no-op are both pinned now | the two rows above |
| D2 (R9 SOURCE_PATHS) | applied, R9 branch 1 | `daemon/internal/linuxpreflight/preflight_linux.go` and `preflight_unsupported.go` added to `SOURCE_PATHS` (`scripts/ci/omo-control-native-evidence.py:69-100`, entries at `:95-96`): the `init()` rewiring moved the `normalizeLinuxNativeStateDir` identity default out of the listed `daemon/internal/app/lease.go` into the package, so the freeze must cover the receiving pair (linux implementation plus the cross-GOOS stub) | `python3 scripts/ci/test-omo-control-native-evidence.py` exit 0, `Ran 10 tests in 9.5s`, `OK` |

Re-derived parity evidence for finding 1 (reproducible: extract the two declarations from
`79548368:daemon/internal/app/linux_owner_preflight_linux.go` and from the head package file, apply the
documented inverse rename map — `ACLSubject`, the exported verifier names, the `StatIdentity` field
selectors — then the two documented deltas): `verifyTrustedLinuxConfig` (21 L) and
`verifyInstalledLinuxOwnerAssets` (41 L) both reconstruct byte-identical, so the apply-pass delta is
exactly the dropped parameter and its argument; the other 130 comparable declarations are untouched and
statement skeletons are unchanged, which is why the control-flow row above still reads 0 differences.

Re-measured numbers (single source: this plan; the implement-stage answer is stage history for the
move-only revision, and this section rebinds the rows the apply pass touched):

| Gate | Command | Result |
| --- | --- | --- |
| format / vet / builds | `gofmt -l .`; `go vet ./...`; `GOOS=linux go vet ./...`; `GOOS=linux\|windows\|darwin go build ./...`; `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 each, `gofmt -l` empty |
| moved suite | WSL `go test -count=1 -json ./internal/linuxpreflight/...` | exit 0; 25 test functions, 55/55 test events, 0 skips — unchanged from the implement measurement |
| stub pins | Windows host `go test -count=1 -v ./internal/linuxpreflight/...` | exit 0; 2/2 pass |
| `app.Run` wiring (amendment 1) | WSL `go test -count=1 -run TestRunNormalizesLinuxV2NativeOwnerStateDir ./internal/app/` | exit 0; 4/4 subtests pass |
| advisory lint | `GOOS=linux golangci-lint run --config .golangci.yml ./internal/linuxpreflight/...`, default caps and with `--max-issues-per-linter=0 --max-same-issues=0` (CI flags) | exit 1 both (advisory, as before): default caps 33→32 issues and `unparam` 1→0; CI flags 37 issues, 0 `unparam` |
| no app import | `GOOS=linux go list -deps ./internal/linuxpreflight \| grep internal/app` | 0 matches, exit 1 as expected |
| evidence freeze | `python3 scripts/ci/test-omo-control-native-evidence.py` | exit 0, `Ran 10 tests in 9.5s`, `OK` |
| citations / records / hygiene | `check-citation-existence.py --base 79548368 --head <snapshot>`, `check-record-cleanliness.py` same range, `git diff --check`, host-path and backslash-u greps over the apply snapshot (ranges `a4be4e26..` and `dee3f4ae..`) | exit 0 (1 changed document, 453 path citations, 3 ignore-rule notes) / exit 0 (2 changed records) / exit 0 / 0 matches each |

Package totals after the apply pass: 9 files, 4557 L (was 8 files / 4523 L; +34 the stub test file) and 27
test functions across the two test files (25 linux, 2 elsewhere). `wc -l` per file is in the file table;
max file is still the moved test file at 973 L, max implementation file 902 L. The 65-L app test lives in
`internal/app`. The affected-surface differential row above was measured on the move-only revision; the
apply pass edits no app production file, so its production result stands, while a re-run of its head-side
count would now include the four new subtest events.

## Fix pass (2026-11-21, GDD fix stage — review findings STD-A01..A05 applied)

The review of `fc8e95a2..1352b156` (dual-axis, dual-model, spec axis APPROVE) returned five P3
standards findings, all against this plan's counts/wording; all five are applied — four as text
corrections, one (STD-A03) also as a code change.

| Finding | Decision | Change | Evidence |
| --- | --- | --- | --- |
| STD-A01 literal totals (`:200`, `:305`) | applied | the six implementation files carry 1095 head literals against 1093 base (1098 including the stub's three); the additions table now marks the 2-implementation/3-stub split | per-file `go/parser` literal count: 177+85+72+26+241+494 = 1095, stub 3, base 1093 |
| STD-A02 step-0 inventory (`:19`) | applied | corrected to 118 functions, 2 methods (`(*boundedLinuxOutput).Write`, `(linuxACLSubject).hasGroup`), `init`, 8 types, 3 const blocks, 1 var block = 133 | baseline AST recount on `79548368`: funcs 119 (including `init`), methods 2, types 8, consts 3, vars 1 |
| STD-A03 exported fields (`:159`) | applied | `OwnerAccounts.A`/`.B` and `ACLSubject.Groups` unexported, back to their baseline names `a`/`b`/`groups`; external reads are zero — callers outside the package read only `ASubject`/`BSubject` and construct `ACLSubject{UID, GID}`; the exported-fields sentence and the two interface rows now state the actual rule | selector grep over `daemon/internal/**`, `daemon/cmd/**`, `daemon/e2e/**` outside the package: no reference to the moved structs' `A`/`B`/`Groups` fields; WSL package suite 55/55 pass |
| STD-A04 entry-point wording (`:129`) | applied | restated as three entry points: the two config-valued verifiers plus the `NormalizeOwnerStateDir` state-dir normalizer | signatures: `VerifyLegacyLinuxOwnersDrained(config.Config)`, `VerifyLinuxOwnerInstallation(config.Config)`, `NormalizeOwnerStateDir(string)` |
| STD-A05 helper size (`:243`) | applied | `claimedStoreAt` restated as a 20-line helper (24 including the package copy's 4-line doc comment) | both copies measured `func` line to closing brace: `preflight_linux_test.go:954-973`, `app_test.go:7373-7392` |

The field unexports are the only code change; no line counts move (the file table, 4557 L total, and
the 19-symbol surface above are unchanged) and the declaration-set partition still holds: every
declaration that used `a`/`b`/`groups` also carries other documented renames
(`accounts.ASubject`/`BSubject` etc.), so none moves between the 64/51 rows. Re-run gates on the
fix-pass tree: `gofmt -l` empty; `go vet ./...` and `GOOS=linux go vet ./...` exit 0;
linux/windows/darwin/linux-arm64 builds exit 0; WSL `go test -count=1 -json
./internal/linuxpreflight/...` exit 0 with 55/55 test events, 0 skips; Windows host
`go test -count=1 ./internal/linuxpreflight/...` exit 0 (stub pins); WSL
`-run TestRunNormalizesLinuxV2NativeOwnerStateDir ./internal/app/` exit 0; `go list -deps`
app-import count 0; `python3 scripts/ci/test-omo-control-native-evidence.py` exit 0 (10 tests,
`OK`); `check-citation-existence.py --base 79548368 --head HEAD` exit 0 (453 path citations, 3
ignore-rule notes); `check-record-cleanliness.py` same range exit 0; fixture suites
`test-check-citation-existence.py` (49 tests) and `test-check-record-cleanliness.py` (19 tests)
exit 0 each; `git diff --check` exit 0;
host-path and backslash-u greps over `origin/main...HEAD` and the worktree diff 0 matches.

## Round-1 review fix pass (2026-11-21 — `197-opus-review-1.md`, Opus, head `1bd89ac9`)

The round-1 review returned three findings (P2-1 required, P3-1 required, P3-2 and P3-3 conductor
records) and confirmed the refactor itself (declarations, literals, call sites, builds, R9, test
move). Its verdict sentence: "Amendment 1's required proof is incomplete: the test does not detect a
dropped `value.StateDir = stateDir`, as mutant M4 survives (P2-1)."

### P2-1 — the pi state-dir subtest now observes the normalized value

The old third subtest ("pi relative path normalizes before validation") expected the
config-validation refusal for missing adapter metadata, which is the same error whether or not the
assignment ran, so it pinned the branch gate, the refusal wrapping and the check order but not the
applied value. It is replaced by
`pi relative path reaches the Store at the normalized absolute path`
(`daemon/internal/app/linux_owner_preflight_state_dir_app_test.go:63-105`; the file is 106 L): the
subtest `t.Chdir`s to a scratch working directory, sets `StateDir` to the absolute temporary path
with its leading separator stripped (so the normalized path is that same writable temporary
directory while the unnormalized path resolves under the working directory), gives the config the
complete non-legacy pi metadata plus the native `git_worktree` workspace policy `Validate` demands,
omits `WithStore`, and asserts the first-enrollment refusal (which can only fire after `state.New`
succeeded) plus that the Store directory exists at the normalized absolute path and not under the
working directory. No production seam was added: the observation is `state.New`'s directory effect
at `app.go:552`, and no privilege is needed because the routed path is a writable temporary
directory, not the filesystem root. The three refusal/branch subtests of the table are unchanged.

Mutation evidence (WSL Ubuntu-24.04, `GOFLAGS=-buildvcs=false`, applied in place and reverted after
each run; command `go test -count=1 -v -run TestRunNormalizesLinuxV2NativeOwnerStateDir
./internal/app/`):

| Mutant | Edit | Result |
| --- | --- | --- |
| M4 (the review's surviving mutant) | `value.StateDir = stateDir` → `_ = stateDir` (`app.go:283`) | exit 1: the new subtest fails with `Store was not opened at the normalized absolute StateDir "/tmp/TestRunNormalizes…/002/state": stat …: no such file or directory`; the three table subtests pass (0.05 s total). Re-run against the final test text after the setup guard was added: same result |
| M1 (normalizer identity) | `NormalizeOwnerStateDir` body → `return path, nil` (`preflight_linux.go:770-782`) | exit 1: all three pi/opencode subtests fail, the new one with the same "Store was not opened" message (0.04 s total) |

Reverted: same command, exit 0, 4/4 subtests pass.

### P3-1 — the contract SHA-256 is recorded

`b5a2176e7ca6a20b9424303c2ca026ea653cbe1daf84391d6d6f4ca97cbaafba` is in this plan's header
(`:4`), with the `sha256sum` check against the GDD state root's
`refactor/arch-decision-2-contracts.tsv` (`in-flight` row), and in the PR #197 body under
"Contract".

### P3-2 — the vocabulary follow-up is recorded and the over-stated pin claim is corrected

`docs/TODO.md` now carries `## Deferred review findings (PR #197)` with the one-vocabulary
follow-up (`internal/harness` `ParseKind` plus the native-goal predicate), the three copy sites, the
one-direction fail-closed analysis and the literal-table pin gap. The "The runtime-selection
vocabulary" section above is reworded to "new kinds fail closed; the copy is pinned to a literal
table, not to app's copy", and the same correction is applied to the `configuredHarnessKind` doc
comment (`daemon/internal/linuxpreflight/cgroup_linux.go:28-31`); that comment is the only code
change of this pass.

### P3-3 — the e2e scope extension is accepted

Recorded in "Recorded decisions where the contract was silent" as
**conductor-accepted scope extension (review P3-3)**.

### Gates re-run on the round-1 tree

`gofmt -l` is empty for every Go file this pass touched (the two in the commit, plus the
mutation-reverted `preflight_linux.go`); WSL
`go test -count=1 -v -run TestRunNormalizesLinuxV2NativeOwnerStateDir ./internal/app/` exit 0 with
4/4 subtests pass (0.23 s); WSL `go test -count=1 ./internal/linuxpreflight/...` exit 0; WSL
`go vet ./internal/linuxpreflight/... ./internal/app/...` and
`GOOS=windows go vet ./internal/linuxpreflight/... ./internal/app/...` exit 0; `git diff --check`
exit 0; host-path and backslash-u greps over the worktree diff 0 matches. The review's own
verified-clean list (builds for linux/amd64, linux/arm64, windows/amd64, darwin/amd64, no `app`
import, export surface, literal/declaration comparison, moved suite 55/55) is unchanged by this
pass because no production statement other than the comment above moved.

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
