# rf-w2-11 typed error identities plan — daemon platform mismatch sentinels

Goal authority (external to this repository, not evidence): the GDD goal contract
`goal-rf-w2-11-typed-errors.md` (2026-11-21) with its binding conductor decisions of the same
date (C1–C7, reconciled from the two round-2 contract reviews, plus the PR #160 review
follow-ups recorded below). Every code fact in this plan is restated with an in-repo
reproduction: coordinates are read from this tree, counts are `git grep`/`grep` commands, and
the full class-to-site inventory is inlined under "Public / API shape". Baseline for every base
coordinate: `origin/main` at `857e763b8b6c175b87e3f6413a4541d15bbb6e41` (the historical
dispatch baseline; every coordinate below was re-verified to resolve at the reviewed head
`10fed9cb`). Branch: `refactor/rf-w2-11-typed-errors`. Area: `daemon/internal/platform` plus
this plan and two `docs/TODO.md` follow-up entries.

The increment gives the repeated anonymous mismatch classes in the `errOr` fallbacks a typed
identity, so `errors.Is` distinguishes "the live process/namespace/image no longer matches its
recorded identity" from "the read itself failed", without changing one byte of produced error
text. It is the DP S-3 slice of the refactor program; the goal contract and conductor
decisions that bound it are named above, and every code fact below is reproduced in-repo.

## Owned set (re-derived over the post-split file names, per P2-8/C2)

`daemon/internal/platform/`:

- the ten post-W1-04 siblings that carry `errOr` call sites: `anchor_gate.go` 35,
  `anchor_proc.go` 5, `anchor_run.go` 14, `anchor_server.go` 8, `anchor_wire.go` 1,
  `helper_v2_protocol.go` 1, `helper_v2_run.go` 4, `helper_v2_server.go` 1,
  `helper_v2_stop.go` 9 (78 converted sites), and `helper_v2_recovery.go` 0;
- `containment_errors.go` — the sentinel home for the cross-platform
  `ErrLinuxSupervisorResponseLost`, plus the new `containment_errors_linux.go` that holds the
  15 `ErrLinuxOwner*` mismatch sentinels: all 78 producing sites are in `linux && amd64`
  files, so the values carry the same constraint instead of being exported dead API on
  windows/darwin (P3-3);
- the two client remainders whose `errOr` sites stay prose: `linux_owner_anchor_linux.go`
  (`errOr` definition `:1015`, sites `:115`, `:245`, `:989`), `linux_owner_helper_v2_linux.go`
  (`:785`, `:799`, `:820`, `:887`, `:1145`, `:1190`);
- `linux_owner_anchor_linux_test.go` — its two class-shaped test-helper sites (`:2049`
  `identity differs`, `:2132` `creation identity differs`) join the conversion, so no occurrence
  of a converted class literal remains as an anonymous fallback anywhere in the owned set; the
  other nine test-helper sites keep their one-off prose;
- `containment_errors_test.go` (this slice's test file, named after the sentinel home; it pins
  all 37 exported `Err*` texts and drives five converted sites through production);
- the plan file itself.

Not touched: `execution/**` and `app/**` (C5), W2-16's `clone_aliasing_*_test.go`,
`native/linux-gate/**` (its `errOr` is a different package), every other platform file, and
every other `docs/` file (`docs/plans/` plus the two `docs/TODO.md` entries are the only doc
changes).

## Decision on the C2 scope fork

C2 requires a choice in this plan: **(a) all production `errOr` sites, delete `errOr`, or
(b) mismatch-class sites only, keep `errOr`.** This plan chooses **(b)**.

Definition used: an *identification site* is an `errOr` call whose fallback literal states one
of the repeated mismatch classes — a class that occurs two or more times across the owned
production files, so callers can branch on it. Those sites are exactly the 78 converted below.

Why (b):

- The defect P-04 names is *erased identity*: the fallback `errors.New("<class>")` means the
  only handle on the class is its text. With sentinels as the fallbacks the identity exists at
  every repeated-class site, and `errOr`'s five lines are then identity-preserving.
- The 34 remaining anonymous fallbacks are single-occurrence prose (`PID differs`,
  `run leaf is absent`, `credentials unavailable`, …). Converting them to sentinels would add
  34 exported names that nothing can branch on — the opposite of finding P-04's own remedy
  (b), "keep arbitrary prose only where nothing can branch on it". Deleting `errOr` at those
  sites costs either a duplicated `fmt.Errorf` return per site (measured +2 lines/site) or a
  stdlib substitute (`cmp.Or`) that reduces clarity for zero identity gain; both are worse
  than keeping a five-line helper for one-off prose.
- 9 of the remaining sites already carry typed identities: `io.ErrShortWrite` (7) and the
  in-package `ErrLinuxCgroupV2Unresolved` (2).

Remaining count, reproducible in-repo (run from the repository root):

```sh
# owned production files at this revision: 122 occurrences = 121 call sites + the definition
git grep -n 'errOr(' -- 'daemon/internal/platform/anchor_*.go' \
  'daemon/internal/platform/helper_v2_*.go' \
  'daemon/internal/platform/linux_owner_*_linux.go' \
  ':(exclude,glob)**/*_test.go' | wc -l
# pre-split base (C2's number): 133 occurrences
git grep -n 'errOr(' c87c9918 -- 'daemon/internal/platform/*.go' | wc -l
```

**121 `errOr` sites in the owned production files = 78 converted + 43 kept** (34 anonymous
one-off literals, 7 `io.ErrShortWrite`, 2 `ErrLinuxCgroupV2Unresolved`), plus the 11 test-helper
sites in `linux_owner_anchor_linux_test.go` (2 converted, 9 kept as one-off prose), plus the
definition itself (122 occurrences at head, 133 at base; 80 sites converted in total).

## Public / API shape

15 new exported sentinels in `containment_errors_linux.go` (`//go:build linux && amd64`;
additive; the only exported-surface change). No signature, struct, JSON, or protocol change.
The 22 exported `Err*` values of C7 stay as they are: same names, same declaration files, same
`Error()` texts, each now pinned by `TestPlatformSentinelErrorTexts` (C7's stricter ruling:
every exported sentinel's `Error()` text is pinned, not only this slice's 15).

| sentinel | message (byte-identical to the base literal) | sites |
| --- | --- | --- |
| `ErrLinuxOwnerCgroupPathChanged` | `cgroup path changed` | 4 |
| `ErrLinuxOwnerCreationIdentityChanged` | `creation identity changed` | 8 |
| `ErrLinuxOwnerCreationIdentityDiffers` | `creation identity differs` | 25 |
| `ErrLinuxOwnerDeviceOrInodeChanged` | `device or inode changed` | 4 |
| `ErrLinuxOwnerDeviceOrInodeDiffers` | `device or inode differs` | 6 |
| `ErrLinuxOwnerDirectoryOwnershipUnsafe` | `directory ownership or mode is unsafe` | 2 |
| `ErrLinuxOwnerGateImageIdentityDiffers` | `root-owned image identity differs` | 2 |
| `ErrLinuxOwnerIdentityChanged` | `identity changed` | 5 |
| `ErrLinuxOwnerIdentityDiffers` | `identity differs` | 3 |
| `ErrLinuxOwnerLauncherPIDFDNotReady` | `launcher pidfd is not ready` | 3 |
| `ErrLinuxOwnerParentDiffersFromHelper` | `parent differs from helper` | 2 |
| `ErrLinuxOwnerParentNamespaceDiffers` | `parent namespace differs` | 2 |
| `ErrLinuxOwnerParentPIDDiffers` | `parent PID differs` | 5 |
| `ErrLinuxOwnerPIDFDNotReady` | `pidfd is not ready` | 5 |
| `ErrLinuxOwnerThreadSnapshotDiffers` | `thread snapshot differs` | 2 |

The four near-synonym process-identity sentinels (changed/differs × creation/plain) exist
because C3 requires every site's `Error()` text to stay byte-identical; they are documented as
the two phrasings of the same two checks. Unifying the texts would change durable and served
strings and is out of scope. C3's sink coordinates are pre-#144 split (`app.go:5235`,
`:8618-8626`, `:7980`, `:8963`, `:8998-9003`); after the W1-05 split those sinks are the
persisted reasons passed to `markGoalSessionUncertain` (`goal_admission.go:889`) and
`publishNativeCloseRetryAfterEvidence` (`goal_delivery.go:134`, `:138`, `:142`), the
terminal-transition payloads (`native_run.go:1628`, `:2204`, `goal_delivery.go:377-382`), the
supervisor helper's `response.Error`, and the two client echoes.

### Class-to-site inventory (the 78 converted `errOr` sites at this revision)

Reproduce from the repository root (the `errOr` fallback at each coordinate is the named
sentinel):

```sh
git grep -n 'errOr(.*ErrLinuxOwner' -- 'daemon/internal/platform/anchor_*.go' \
  'daemon/internal/platform/helper_v2_*.go' 'daemon/internal/platform/linux_owner_*_linux.go' \
  ':(exclude,glob)**/*_test.go' | wc -l                              # 78 sites
```

| sentinel | sites |
| --- | --- |
| `ErrLinuxOwnerCgroupPathChanged` | `anchor_gate.go:995`; `anchor_run.go:1287`; `anchor_server.go:497`, `:680` |
| `ErrLinuxOwnerCreationIdentityChanged` | `anchor_gate.go:376`; `anchor_proc.go:613`; `anchor_run.go:335`, `:1447`; `helper_v2_run.go:284`; `helper_v2_stop.go:589`, `:633`, `:1094` |
| `ErrLinuxOwnerCreationIdentityDiffers` | `anchor_gate.go:172`, `:554`, `:621`, `:677`, `:695`, `:718`, `:864`, `:940`, `:991`; `anchor_proc.go:165`, `:330`; `anchor_run.go:434`, `:447`, `:800`, `:879`, `:977`, `:1280`; `anchor_server.go:930`; `anchor_wire.go:165`; `helper_v2_protocol.go:418`; `helper_v2_run.go:253`; `helper_v2_server.go:98`; `helper_v2_stop.go:502`, `:556`, `:582` |
| `ErrLinuxOwnerDeviceOrInodeChanged` | `anchor_gate.go:321`, `:329`, `:537`, `:868` |
| `ErrLinuxOwnerDeviceOrInodeDiffers` | `anchor_gate.go:339`, `:500`, `:517`, `:714`, `:882`; `anchor_run.go:202` |
| `ErrLinuxOwnerDirectoryOwnershipUnsafe` | `anchor_gate.go:81`, `:97` |
| `ErrLinuxOwnerGateImageIdentityDiffers` | `anchor_gate.go:506`; `anchor_server.go:837` |
| `ErrLinuxOwnerIdentityChanged` | `anchor_gate.go:318`, `:413`, `:440`, `:462`, `:488` |
| `ErrLinuxOwnerIdentityDiffers` | `anchor_proc.go:307`, `:528`; `helper_v2_stop.go:512` |
| `ErrLinuxOwnerLauncherPIDFDNotReady` | `anchor_gate.go:196`, `:968`; `anchor_run.go:302` |
| `ErrLinuxOwnerPIDFDNotReady` | `anchor_gate.go:433`, `:481`; `anchor_run.go:1469`; `anchor_server.go:605`, `:764` |
| `ErrLinuxOwnerParentDiffersFromHelper` | `helper_v2_stop.go:1049`, `:1099` |
| `ErrLinuxOwnerParentNamespaceDiffers` | `anchor_gate.go:299`; `anchor_server.go:831` |
| `ErrLinuxOwnerParentPIDDiffers` | `anchor_gate.go:176`, `:944`; `anchor_run.go:418`, `:805`; `anchor_server.go:559` |
| `ErrLinuxOwnerThreadSnapshotDiffers` | `helper_v2_run.go:276`, `:288` |

All coordinates are relative to `daemon/internal/platform/`; the class counts in the table
above sum to 78 and match this inventory at the revision that added the production-driven site
tests. Any later conversion of a one-off literal to a sentinel should extend this inventory in
the same change rather than re-derive counts from a scratch tool.

## Exact edge-case semantics

- **Conversion shape.** Each site's single edit is `errOr(err, errors.New("<literal>"))` →
  `errOr(err, <Sentinel>)`: same statement, same `fmt.Errorf` prefix, same `%w` position, same
  argument order. Every other `fmt.Errorf` argument is untouched, so the composed error text is
  byte-identical for both arms.
- **Identity only for the mismatch arm.** `errOr` returns the inner error whenever the read
  failed; the sentinel is wrapped only when the read succeeded and the predicate mismatched.
  So `errors.Is(err, <Sentinel>)` is true exactly for the identity-change case and false for
  transport loss, which keeps the acceptance restated in C5 honest inside the owned package.
- **Text sinks.** The wire/served text is the sentinel message, identical to the literal, and
  all wrapping prefixes are unchanged; no journal, reason, or `response.Error` string changes.
- **Two-`%w` sites.** `helper_v2_run.go:276`, `:288` wrap the outer
  `errLinuxOwnerProcessChildrenChanged` and the new class marker; both verbs are preserved.

## Tests

`containment_errors_test.go` (`//go:build linux && amd64`):

1. `TestLinuxOwnerTypedMismatchSentinelTable` — one pin per sentinel class, generated from the
   site inventory: the representative site's `fmt.Errorf` expression verbatim with `err == nil`
   must produce the base-pinned full text (proving byte-identical wire text) and must satisfy
   `errors.Is`; with a non-nil inner error the same expression must carry the inner error's
   text and *not* satisfy `errors.Is` (proving identity-change vs transport loss). The 15 rows
   reproduce the base literals, so the pin is checkable against any base revision in-repo:
   `git show c87c9918:daemon/internal/platform/<file>` shows the replaced literal at each
   row's site coordinate (the inventory in "Public / API shape").
2. `TestPlatformSentinelErrorTexts` — one row per exported platform sentinel (C7's 22
   pre-existing `Err*` values plus this slice's 15), pinning the exact `Error()` text of each
   declaration. `errors.Is` identity cannot detect a reworded message, so this is the C7 /
   C3 text pin for the declarations themselves.
3. `TestLinuxOwnerTypedMismatchReachableSites` — five converted sites driven through production
   code (the other ten classes are only reachable through the real anchor/helper state machine,
   which the contract does not authorize fabricating):
   - `verifyLinuxOwnerAProcess` (`identity differs`, `anchor_proc.go:307`);
   - `openLinuxOwnerAnchorUserNamespaceFD` with a stale identity and `pidfd == -1`
     (`creation identity differs`, `anchor_gate.go:695`);
   - `verifyLinuxOwnerAnchorStopEvidenceNamespacePin` against a real `/proc/self/ns/user` FD
     (`device or inode differs`, `anchor_run.go:202`);
   - `verifyLinuxOwnerAnchorPIDFDIdentity` with the process's own pidfd and a stale expected
     identity (`identity differs`, `anchor_proc.go:528`);
   - `classifyUnregisteredPreReleaseGate` (`cgroup path changed`, `anchor_gate.go:995`): the
     test starts a real gate child in its own nested user namespace (whose parent is the test
     process's namespace, so the production ancestry check verifies) and a real registered
     launcher that is killed and reaped (so the production pidfd-readiness check observes an
     exited launcher); the child's live cgroup path then differs from the leaf path the
     classifier is asked to confirm, which is exactly the site's predicate.

   Each subtest asserts the exact outer `Error()` text and `errors.Is` on its sentinel, so a
   site that stops returning its sentinel fails. The user-namespace fixture skips by name when
   the kernel refuses unprivileged user namespaces, and the two credential-sensitive subtests
   skip when the process uid or primary gid is 0.

Mutation checks (Linux, WSL, `go test -overlay`, run on 2026-11-21): the identity overlay copy
of `anchor_gate.go` passes; swapping `anchor_gate.go:995`'s sentinel to
`ErrLinuxOwnerPIDFDNotReady` fails `classifyUnregisteredPreReleaseGate` with the swapped wire
text; reverting that site to `errOr(err, errors.New("cgroup path changed"))` fails the same
subtest with the byte-identical text but no `errors.Is` match. The repo tree is never modified
by these runs; each mutant is a one-line `sed` copy in a scratch directory referenced by an
overlay JSON.

## Evidence plan (exact commands, final tree)

| Gate | Command | Where |
| --- | --- | --- |
| format | `gofmt -l daemon/internal/platform` | host |
| vet | `go vet ./...` | host (windows/amd64) |
| vet (owned files, real target) | `GOOS=linux GOARCH=amd64 go vet ./internal/platform/ ./internal/execution/ ./internal/app/` | host, C4 |
| build | `GOOS=linux go build ./...`, `GOOS=windows go build ./...` | host |
| package + callers | `go test -count=1 ./internal/platform/... ./internal/execution/... ./internal/app/...` | host (windows side) |
| Linux tests (C4) | `go test -count=1 -skip 'Loopback\|TCPPeer' ./internal/platform/... ./internal/execution/...` | WSL `Ubuntu-24.04` user `accept`, go1.27.0; the skip excludes this host's environmental loopback/TCP-peer pathologies |
| errorlint (C6) | `GOOS=linux GOARCH=amd64 golangci-lint run --no-config --default=none --enable=errorlint ./internal/platform/...`, filtered to the owned files | host, v2.15.0: 8 findings at base, 7 at head (only `anchor_wire.go:346` owned; see "Trade-offs") |
| sentinel/call-site inventory | `git grep -n 'errOr(.*ErrLinuxOwner' ... \| wc -l` over the owned production files (78), and `git grep -n 'errOr(' ... \| wc -l` (122 with the definition) — see the two blocks above | this tree |
| whitespace | `git diff --check` | host |
| host-path leak | the program's profile-directory grep over the branch diff and both new files | host: 0 hits |

CI-only oracle (per contract): the daemon race job. No `-race` locally (no cgo on this Windows
host). Measurement harnesses are not part of this slice, so the sequencing rule does not bind.

## Out of scope

- W3-02 (response builder + request-shape table), W4-02 (session objects), W5-01 (witness
  scaffolding) — untouched.
- Caller migration in `execution/**`/`app/**` (C5): no caller edits and no caller tests; the
  sentinels are consumed by `errors.Is` only inside the owned package.
- One-off prose classes (34 sites) and the pre-existing 1188 anonymous `errors.New` sites
  elsewhere in the package.
- Unifying the near-synonym texts (would change wire text; not authorized).
- No `//nolint`/`credo:disable` suppression is added; `errorlint` gains 0 new findings and one
  pre-existing owned-file finding is fixed where behaviour-neutral
  (`anchor_server.go:305` converted to `errors.As`). The remaining owned-file finding
  (`anchor_wire.go:346`) is recorded in `docs/TODO.md`; see "Trade-offs and known limits".

## Trade-offs and known limits

- **errorlint (measured, C6 command).** `GOOS=linux GOARCH=amd64 golangci-lint run --no-config
  --default=none --enable=errorlint ./internal/platform/...` reports **8 findings at base and 7 at
  head**, i.e. zero new findings and one fixed. The fixed one is `anchor_server.go:305`
  (`err.(net.Error)` on the direct `AcceptUnix` error) → `errors.As`; this is behaviour-neutral
  because `net.(*UnixListener).AcceptUnix` can return only a bare `syscall.EINVAL` (the `!l.ok()`
  branch; `syscall.Errno.Timeout()` is true only for `EAGAIN`/`EWOULDBLOCK`/`ETIMEDOUT`, so this
  branch never takes the timeout arm) or an `&net.OpError{...}` that already *is* the outermost
  `net.Error`, so both forms select the same error. The one remaining owned-file finding is
  `anchor_wire.go:346` (`sanitizeLinuxOwnerAnchorSocketError`'s type switch): the sanitizer
  deliberately rebuilds only the *outermost* `*net.OpError`/`*os.PathError`/`*os.SyscallError` and
  returns a nested or joined error untouched, which `errors.As` cannot express without an identity
  check that obscures the sanitizer, and a `//nolint` is forbidden by the program rules. Per the
  conductor ruling on the PR #160 review, C6 is read as "0 *new* findings; owned-file findings
  fixed where behaviour-neutral", and this remaining finding is recorded with its reason in
  `docs/TODO.md` ("Daemon platform: anchor socket-error sanitation keeps its outermost-only type
  switch").
- 15 exported names for 15 repeated classes is more surface than the "8–10" the finding
  estimated; the count is the measured `≥2` rule over the owned files, stricter than C6's
  baseline enumeration (`≥4`), so no repeated class is left text-only.
- Ten of the fifteen classes have no reachable-site test: their mismatch arm requires a real
  anchor/helper/gate state machine (a process that changes identity between two reads, an
  unsafe root path, a parked gate). The expression pins cover those classes' composition, and
  five sites are driven through production, including the representative sites of the
  `cgroup path changed` and `identity differs` rows; runtime identity coverage of the other ten
  classes is reported as a limit, not implied by the pins.
- `errOr` survives with 43 prose/typed sites; the decision and the exact remaining inventory
  are above, per C2.

## Handoff to later slices

- W3-02 owns the eight `writeResponse*` writers and the request validators in
  `anchor_server.go`/`anchor_wire.go`; the sentinels introduced here are stable and additive,
  so a response builder can wrap them unchanged.
- One pre-existing `errorlint` finding this slice leaves in place belongs to a follow-up
  error-sanitation slice: `anchor_wire.go:346`
  (`sanitizeLinuxOwnerAnchorSocketError`'s type switch), recorded in `docs/TODO.md` with its
  reason and the measured base/head counts.
- The class-to-site inventory in "Public / API shape" is the in-repo mapping at this revision
  (its `git grep` command reproduces the 78 rows); any later conversion of a one-off literal to
  a sentinel should extend it in the same change rather than re-derive counts from a scratch
  tool.

## Comment-policy sweep (2026-11-22)

Module intent removed from this slice's files (`daemon/internal/platform/containment_errors.go`,
`containment_errors_linux.go`) by the 2026-11-22 comment-policy sweep is recorded below (workstream record "ws2b-platform-containment").

- `daemon/internal/platform/containment_errors.go` (head lines 10–12): "This value stays in an
  unconstrained file because execution/runner.go consumes it on every platform; the ErrLinuxOwner*
  mismatch sentinels live in containment_errors_linux.go next to their linux && amd64 producing
  sites." (the pre-program contract paragraph at lines 5–8 was kept byte-identical).
- `daemon/internal/platform/containment_errors_linux.go` (head lines 14–17): "All 78 producing
  sites are in linux && amd64 files, so the values live in this linux && amd64 file rather than in an
  unconstrained one, where they would be exported dead API on windows and darwin. The cross-platform
  ErrLinuxSupervisorResponseLost stays in containment_errors.go."

The sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
