# rf-w2-11 typed error identities plan — daemon platform mismatch sentinels

Contract: the GDD goal contract `goal-rf-w2-11-typed-errors.md` (2026-11-21, at the GDD state
root) with its binding conductor decision of the same date (C1–C7, reconciled from the two
round-2 contract reviews). Baseline for every coordinate in this plan: `origin/main` at
`857e763b8b6c175b87e3f6413a4541d15bbb6e41` (the historical dispatch baseline and the revision
whose C2/C3 coordinates this plan quotes; every coordinate below was re-verified to resolve at
the reviewed head `10fed9cb`). Branch: `refactor/rf-w2-11-typed-errors`. Area:
`daemon/internal/platform` only.

The increment gives the repeated anonymous mismatch classes in the `errOr` fallbacks a typed
identity, so `errors.Is` distinguishes "the live process/namespace/image no longer matches its
recorded identity" from "the read itself failed", without changing one byte of produced error
text. It is the DP S-3 slice of the refactor program (finding P-04, `daemon-platform.md`).

## Owned set (re-derived over the post-split file names, per P2-8/C2)

`daemon/internal/platform/`:

- the ten post-W1-04 siblings that carry `errOr` call sites: `anchor_gate.go` 35,
  `anchor_proc.go` 5, `anchor_run.go` 14, `anchor_server.go` 8, `anchor_wire.go` 1,
  `helper_v2_protocol.go` 1, `helper_v2_run.go` 4, `helper_v2_server.go` 1,
  `helper_v2_stop.go` 9 (78 converted sites), and `helper_v2_recovery.go` 0;
- `containment_errors.go` — the sentinel home;
- the two client remainders whose `errOr` sites stay prose: `linux_owner_anchor_linux.go`
  (`errOr` definition `:1015`, sites `:115`, `:245`, `:989`), `linux_owner_helper_v2_linux.go`
  (`:785`, `:799`, `:820`, `:887`, `:1145`, `:1190`);
- `linux_owner_anchor_linux_test.go` — its two class-shaped test-helper sites (`:2049`
  `identity differs`, `:2132` `creation identity differs`) join the conversion, so no occurrence
  of a converted class literal remains as an anonymous fallback anywhere in the owned set; the
  other nine test-helper sites keep their one-off prose;
- `containment_errors_test.go` (new, this slice's test file, named after the sentinel home);
- the plan file itself.

Not touched: `execution/**` and `app/**` (C5), W2-16's `clone_aliasing_*_test.go`,
`native/linux-gate/**` (its `errOr` is a different package), every other platform file, all
`docs/` except `docs/plans/`.

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

Remaining count either way (measured, `refactor/tools/w2-11-typed-errors/remaining-after.md`):
**121 `errOr` sites in the owned production files = 78 converted + 43 kept** (34 anonymous
one-off literals, 7 `io.ErrShortWrite`, 2 `ErrLinuxCgroupV2Unresolved`), plus the 11 test-helper
sites in `linux_owner_anchor_linux_test.go` (2 converted, 9 kept as one-off prose), plus the
definition itself (133 occurrences at base, C2's number; 80 sites converted in total).

## Public / API shape

15 new exported sentinels in `containment_errors.go` (additive; the only exported-surface
change). No signature, struct, JSON, or protocol change. The 22 exported `Err*` values of C7
stay as they are; no name moves.

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
   text and *not* satisfy `errors.Is` (proving identity-change vs transport loss). The table is
   a base-green pin: the same 15 rows ran green on the base tree as
   `TestLinuxOwnerBaseMismatchTextPins` composing `errors.New("<literal>")` (evidence:
   `base-pin-wsl.txt`).
2. `TestLinuxOwnerTypedMismatchReachableSites` — three converted sites driven through
   production code (the other twelve classes are only reachable through the real anchor/helper
   state machine, which the contract does not authorize fabricating): `verifyLinuxOwnerAProcess`
   (`identity differs`), `openLinuxOwnerAnchorUserNamespaceFD` with a stale identity and
   `pidfd == -1` (`creation identity differs`), and
   `verifyLinuxOwnerAnchorStopEvidenceNamespacePin` against a real `/proc/self/ns/user` FD
   (`device or inode differs`). These fail if the site stops returning its sentinel; the
   privileged test reports by name (it skips when the process uid or its primary gid is 0, the
   incomplete-credentials precondition `verifyLinuxOwnerAProcess` checks before the identity
   comparison).

Mutation checks (Linux, WSL): the sentinel swap at each of the three reachable sites and one
sentinel-message swap must turn the suite red; each mutation is reverted and the suite re-run.

## Evidence plan (exact commands, final tree)

| Gate | Command | Where |
| --- | --- | --- |
| format | `gofmt -l daemon/internal/platform` | host |
| vet | `go vet ./...` | host (windows/amd64) |
| vet (owned files, real target) | `GOOS=linux GOARCH=amd64 go vet ./internal/platform/ ./internal/execution/ ./internal/app/` | host, C4 |
| build | `GOOS=linux go build ./...`, `GOOS=windows go build ./...` | host |
| package + callers | `go test -count=1 ./internal/platform/... ./internal/execution/... ./internal/app/...` | host (windows side) |
| Linux tests (C4) | `go test -count=1 ./internal/platform/... ./internal/execution/...` | WSL `Ubuntu-24.04` user `accept`, go1.27.0 |
| errorlint (C6) | `GOOS=linux GOARCH=amd64 golangci-lint run --no-config --default=none --enable=errorlint ./internal/platform/...`, filtered to the owned files | host, v2.15.0, base and head |
| sentinel/call-site table | `python verify.py <repo>` (this slice's tool) | host |
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
- No `//nolint`/`credo:disable` suppression is added; `errorlint` gains 0 new owned-file
  findings (base and head each report the same 2 pre-existing owned-file findings; see the C6
  amendment request under "Trade-offs and known limits").

## Trade-offs and known limits

- **errorlint baseline (measured, C6 command).** `GOOS=linux GOARCH=amd64 golangci-lint run --no-config
  --default=none --enable=errorlint ./internal/platform/...` reports **8 findings at base and the same
  8 at head**, i.e. zero new findings. Two of them are inside the owned set and pre-existing:
  `anchor_server.go:305` (`err.(net.Error)` on the direct `AcceptUnix` error) and
  `anchor_wire.go:346` (`sanitizeLinuxOwnerAnchorSocketError`'s type switch). C6's "= 0 for the owned
  files" is therefore unattainable as a baseline fact. This slice does not rewrite those two sites:
  the sanitizer's switch deliberately rebuilds only the *outermost* `*net.OpError`/`*os.PathError`/
  `*os.SyscallError` (a nested or joined error is returned untouched), and `errors.As` cannot express
  "outermost only" without an identity check that obscures the sanitizer; a `//nolint` is forbidden by
  the program rules. Smallest proposed amendment, awaiting the conductor's ruling: read C6 as
  "0 *new* owned-file findings" (measured, above) and defer the sanitizer's shape to a slice that
  owns error sanitation. `anchor_server.go:305`
  is safely convertible to `errors.As` (its only producer is `AcceptUnix`), but it would break this
  slice's "head = base + sentinel swaps" proof, so it is left for that follow-up too.
- 15 exported names for 15 repeated classes is more surface than the "8–10" the finding
  estimated; the count is the measured `≥2` rule over the owned files, stricter than C6's
  baseline enumeration (`≥4`), so no repeated class is left text-only.
- Twelve of the fifteen classes have no reachable-site test: their mismatch arm requires a real
  anchor/helper/gate state machine (a process that changes identity between two reads, an
  unsafe root path, a parked gate). The expression pins plus the mechanical site check
  (`verify.py`, which fails if a site does not carry its sentinel) cover wiring; runtime
  identity coverage is limited to the three reachable classes and is reported as such.
- `errOr` survives with 43 prose/typed sites; the decision and the exact remaining inventory
  are above, per C2.

## Handoff to later slices

- W3-02 owns the eight `writeResponse*` writers and the request validators in
  `anchor_server.go`/`anchor_wire.go`; the sentinels introduced here are stable and additive,
  so a response builder can wrap them unchanged.
- The two pre-existing `errorlint` findings this slice leaves in place belong to a follow-up
  error-sanitation slice: `anchor_server.go:305` (`err.(net.Error)` on the direct `AcceptUnix`
  error) and `anchor_wire.go:346` (`sanitizeLinuxOwnerAnchorSocketError`'s type switch).
- The sentinel table (`refactor/tools/w2-11-typed-errors/sentinel-sites.md`) is the authoritative
  class → site mapping at this revision; any later conversion of a one-off literal to a
  sentinel should extend it rather than re-derive counts.
