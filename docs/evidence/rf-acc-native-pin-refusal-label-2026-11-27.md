# rf-acc-native-pin-refusal-label — evidence record

Contract: `goal-rf-acc-native-pin-refusal-label.md` (GDD state root, outside the repository).
Dispatch base `d45e3298` (`main`), branch `refactor/rf-acc-native-pin-refusal-label`. Plan:
`docs/plans/2026-11-27-rf-acc-native-pin-refusal-label-plan.md`. The receipt blocks name their
own subjects (§6); the credentialed acceptance and its comparator are separate (§8 and Bind).

## 1. Changed Go files (SHA-256)

The reviewed code/test snapshot `76cb92b1` (parent `07452d16`) has the five Go blobs below.
They are unchanged in fix pass C (`97e86f75`). Except for §2's dispatch-base trace,
unqualified source anchors refer to those Go blobs.

| File | Dispatch base `d45e3298` | Reviewed snapshot | Diff |
| --- | --- | --- | --- |
| `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` | `76f8a0ff803af08a359bbc18e1275d0363308328044209cf8e91e346c8ac91d8` | `c2adf63fae19559cce34c77937443f6da16736f940a195e2cca040ead0dd6a2e` | +6/−1 |
| `daemon/native/linux-gate/protocol/release_pin_label_linux_test.go` | absent | `3d3a814056f29804496279aca99a7d12f3450004152cdfb43e1414cf00e264a9` | new, 56 lines |
| `daemon/native/linux-gate/protocol/native_image_pin_linux_test.go` | `2c46a46ec66de8a8a62f5dded2724826bd0a008822530dd31efcb20edabd52f0` | `b34b09d38f5f1b4c6e2250a5d119f5dca04ea081a4caee4e13ad1619ba7bda03` | +19/−15 |
| `daemon/internal/platform/helper_v2_protocol.go` | `4ac73de34f901c91a3cef10e3704e9530f520db321deb6af4e38669bc6eb9d75` | `29c2251101903bdd8d3ef7c4b3696c198c3c66ee81356d607e514e20e251d358` | +2/−1 |
| `daemon/internal/platform/linux_owner_helper_v2_linux_test.go` | `17c7e40ec0e551debfaf7accfb3c7f48f72e822deff4fdfd9058348a71616fa7` | `e3f904b61529397c4b81fb979bd54d30aa0e38c27557d0ed72ac2cb7541fe396` | +5/−0 |

The four documentation paths in the stage manifests (§6) are the plan, this evidence record,
`docs/TODO.md` (residual section renamed, closed item moved out, Re-Bind kept open) and
`docs/archive/CLOSED-TODO.md` (closure block added). The shared TODO files are cited by content;
the immutable Git trees pin their historical blobs as well as each historical copy of this record.

No production byte outside `daemon/native/linux-gate/protocol/` and
`daemon/internal/platform/helper_v2_protocol.go` changed; no C helper, `gate.c`, script, workflow,
fixture, e2e or frontend byte changed. The pin's own predicates, messages and
`assertUnlabelledNativeImagePinRefusal` rows are untouched; `native_image_pin_linux_test.go`
changes only by hosting `stageSymlinkedNativeImageParent`, the trusted-staging helper both symlink
fixtures share.

## 2. Stage 1 — where the label is lost (trace; the line anchors below are dispatch-head `d45e3298` anchors)

1. **The C gate sends nothing for this class.** `PreparedGate.Release` pins `command.Path` before
   it builds the release packet: the pin call at `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:928`
   precedes `encodeGuardedRelease` (`:963`) and `sendPacketWithRights` (`:984`). No packet, frame
   or C-side vocabulary is involved, which is why the ruling's wire-change branch does not apply.
2. **The Go renderer cannot invent a stage.** `linuxOwnerHelperV2ReleaseErrorDetailFor`
   (`daemon/internal/platform/helper_v2_protocol.go:591`) classifies only through
   `errors.As(err, &subStep)` (`:593-600`) and defaults to `Stage: "none"` (`:592`); an untyped
   error renders `stage=none` (`String()`, `:486-509`). The helper feeds it the exact `Release`
   return (`daemon/internal/platform/helper_v2_server.go:1211`) beside the unchanged
   `release_outcome_unknown` code (`:1200`).
3. **The other labelled release stops label at their own call site** with the closed vocabulary
   (`gate_lifecycle_linux.go:635-639` sub-steps, `:645-669` arms, `:726-748` shapes): the clone
   bootstrap verify at `:918`, the pre-send gate identity at `:944`, the post-send wait and exec
   arms at `:1025-1052`, and the clone-release arms at `:1102-1171`, plus
   `helper_v2_server.go:1121-1185`. Three other `ErrOutcomeUnknown` returns in the same RELEASE
   path stay unlabelled at these bytes and still render `stage=none` (`gate_lifecycle_linux.go:975`
   release or exec-status channel unavailable, `:998` one-use send/close result uncertain, `:1032`
   aborted before the exec acknowledgement settled; delivered `:980`, `:1003-1004`, `:1037`):
   pre-existing, outside this goal's clause, carried as residual (§7).
4. **Only the pre-packet image-approval refusal is in scope.** `command.Validate` can reject the
   leg's supplied command, including its executable path (`wire_linux.go:416-471`), and
   `encodeGuardedRelease` can reject an over-bound command or grant (`wire.go:715-730`). Neither
   is the pin refusal. Other pre-send failures, including owner-pipe/guard failures, are not
   relabelled by this goal.

**Wrong layer: the `PreparedGate.Release` pin call site**, which knows the release context but
returned the pin's error unwrapped. `pinNativeImage` is the wrong layer: `verifyInstalledCloneBootstrap`
calls it for the fixed clone bootstrap (`proc_identity_linux.go:227`), and that failure is already
labelled `clone_bootstrap`/`installed_bootstrap` at `gate_lifecycle_linux.go:918` with an outer
wrap, while `NewReleaseSubStepError` never re-labels an already-labelled error (`:833-857`) — an
inner label would have misnamed that caller.

## 3. The change and the string census

Label choice (plan §3.1): a new sub-step `native_image` with one arm `image_pin` — the step that
failed is the pinning of the native image, and the C side already calls the same logical step
`native_image` (`ReleaseGuestStageNativeImage`, `gate_lifecycle_linux.go:689`); reusing
`native_exec` was rejected because its arms all describe post-send observations, and
`gate_authorize` because its arms are the gate/anchor identity checks. Errno stays `0` because the
call-site wrapper passes zero deliberately for this bounded pre-packet detail (the same choice the
neighbouring pre-packet wraps `installed_bootstrap` and `gate_identity` make), not because the
failure cannot be a syscall error — the class includes `ENOTDIR` from the parent-component open and
`ENOENT` from the image open, each kept inside the pin's own message text. The arm's wait-class,
clone-read and guest-stage shapes are all false, so the detail carries only the stage and arm.

| Reviewed snapshot site | Change |
| --- | --- |
| `gate_lifecycle_linux.go:638` | `ReleaseSubStepNativeImage = "native_image"` |
| `gate_lifecycle_linux.go:662` | `ReleaseArmNativeImagePin = "image_pin"` |
| `gate_lifecycle_linux.go:741-742` | shape table binds the arm to the new sub-step with no wait class, clone read or guest stage |
| `gate_lifecycle_linux.go:936-937` | the pin failure returns `NewReleaseSubStepError(ReleaseSubStepNativeImage, ReleaseArmNativeImagePin, 0, fmt.Errorf("pin A-approved native executable before RELEASE: %w", err))`; `ReleaseSubStepError.Error()` renders the inner message verbatim, so the refusal text is unchanged |
| `helper_v2_protocol.go:516-519` | `Validate()`'s closed stage switch accepts `linuxgate.ReleaseSubStepNativeImage` |
| `linux_owner_helper_v2_linux_test.go:922` | arm census row, literal `stage=native_image arm=image_pin` |
| `linux_owner_helper_v2_linux_test.go:1223-1226` | validator accepted-shape row `{Stage: "native_image", Arm: "image_pin"}` |

Census (`grep -rn "native_image\|image_pin" daemon/ --include=*.go`, §1 Go blobs, 9 hits): the
two new production token sites above (`:638`, `:662`), the four new/updated test rows (the
arm-census literal at `linux_owner_helper_v2_linux_test.go:922`, the validator shape at `:1225`,
and the new row's two literal assertions at `release_pin_label_linux_test.go:45-46`), and two
pre-existing sites (`ReleaseGuestStageNativeImage = "native_image"` at `gate_lifecycle_linux.go:689`
and its `CloneFailureNativeImage` row at `linux_owner_helper_v2_linux_test.go:1084`); the ninth hit
is an incidental `open_native_image` substring inside a pre-existing message literal
(`gate_census_linux_test.go:1214`). The vocabulary gained exactly one sub-step and one arm.

Existing strings unchanged: the two production diffs are the added vocabulary/shape/wrap lines and
the replacement of the one unwrapped return (`git diff --numstat d45e3298`: `gate_lifecycle_linux.go`
+6/−1, `helper_v2_protocol.go` +2/−1); the census table's existing literal rows are unedited (+1
row, 0 changes), so every previously rendered `stage=… arm=…` literal is preserved, and the
classifier, wire and redaction tests passed unchanged on the apply and fix-pass-C subjects (§6).

## 4. Red first, the new rows, and the mutants

Base red (fix pass C Go inputs, with both changed production files restored to their dispatch-base
SHA-256 values in §1; WSL Ubuntu-24.04 as unprivileged `accept`, `daemon/`):
`go test -count=1 -run '^TestPreparedGateReleaseLabelsTheNativeImagePinRefusal$' ./native/linux-gate/protocol/`
exited 1 with both subtests red, verbatim (staging path elided):

```
release_pin_label_linux_test.go:19: the pin refusal for … is unlabelled, so the helper renders stage=none: pin A-approved native executable before RELEASE: approved native image must have an A-controlled parent directory
release_pin_label_linux_test.go:24: the pin refusal for …/linked/image is unlabelled, so the helper renders stage=none: pin A-approved native executable before RELEASE: open native-image parent component "linked" without following symlinks: not a directory
```

The new row is `TestPreparedGateReleaseLabelsTheNativeImagePinRefusal`
(`daemon/native/linux-gate/protocol/release_pin_label_linux_test.go`): a `PreparedGate` in
`gatePrepared` state with the package's own scratch pipes and owner-pipe guard drives the real
`Release` for two unapprovable images — a top-level image path, refused by the path-shape check
before any parent is stat'ed (`proc_identity_linux.go:111-113`, environment-free), and a planted
symlinked parent component in a trusted staging tree (the recorded class; both symlink subtests
stage through the shared `stageSymlinkedNativeImageParent` helper) — and asserts the returned error
carries `stage=native_image arm=image_pin` as literals, no errno/wait/read/guest fields, and the
pin's own refusal detail.

Mutants below use apply R2's Go snapshot (§1 and §6), with one production blob replaced in each
scratch subject by the SHA-256 listed after the table. They ran in Noble userland as a non-root A
user (`setpriv --reuid=52000 --clear-groups`, trusted `HOME`), where a mutant binary built. The
fix-stage probe found that even the rootfs named for Resolute reported `VERSION_CODENAME=noble`;
independent review reproduced the refusal-label discrimination in Noble. No Resolute run was
performed.

| id | mutation | red row and observed output |
| --- | --- | --- |
| M1 | the wrap deleted (the pre-fix return restored) | new row: `the pin refusal for … is unlabelled, so the helper renders stage=none: …` (both subtests) |
| M2 | `ReleaseSubStepNativeImage` value swapped to `"native_exec"` | new row: `pin refusal for … labelled stage="native_exec" arm="image_pin", want stage=native_image arm=image_pin`; the platform test binary additionally fails to build, `helper_v2_protocol.go:519:3: duplicate case linuxgate.ReleaseSubStepNativeExec (constant "native_exec" of type string) in expression switch` |
| M3 | `ReleaseArmNativeImagePin` value swapped to `"native_pin"` | new row: `… labelled stage="native_image" arm="native_pin", want stage=native_image arm=image_pin`; census row: `linux_owner_helper_v2_linux_test.go:947: arm "native_pin" rendered "stage=native_image arm=native_pin", want the literal "stage=native_image arm=image_pin"` |
| M4 | `Validate()`'s stage switch drops the new sub-step | census row: `:944: arm "image_pin" is not closed-vocabulary: … unknown sub-step "native_image"`; validator row: `:1331: detail {Stage:native_image Arm:image_pin …} was rejected: … unknown sub-step "native_image"` |
| M5 | the shape entry rebound to `ReleaseSubStepNativeExec` | census row: `:936: arm "image_pin" is not bound to sub-step "native_image" in the closed vocabulary`; validator row: `:1331: … arm "image_pin" does not belong to sub-step "native_image"` |

Each mutant copy's mutated production file (full sha256): M1
`19411ec02864a22d42fc50674f443b6608c65c535cdf89c71551e12dc839b33e`, M2
`3c31caa8bf4e61a672381b861743c44feae08c39970c7eac071365a58d663703`, M3
`af4c37608ff8a5cbdac590570ee831cdc4d0f68ec3364e082995588b830b879c`, M5
`c94e53474472b612f5a5c859acfec8c1e821129e02f4723d6c9153f3f4359c7e` (`gate_lifecycle_linux.go`); M4
`c6d97ae3fbd4b512f613d66a1cd99b26cb4debde17524b7ce6480c861024540d`
(`helper_v2_protocol.go`).

Local Noble result: the base-override protocol binary ran both subtests and failed as quoted
above. Without the overrides, fix pass C passed both subtests and the changed packages, including
the platform classifier, census, read-class, redaction and closed-vocabulary rows (§6). The
platform base override did **not** build; M3–M5, not that compile failure, are the executed
platform red assertions.

## 5. New-assertion attribution (Bind versus CI)

The conductor's credentialed `synthetic_goal_cleanup` Bind did not execute the three new unit
assertions. Its leg releases a resolved symlink-free image, not one that triggers the pin refusal.
The assertion sites are `release_pin_label_linux_test.go` (both inputs) and
`linux_owner_helper_v2_linux_test.go:922` and `:1225` (census and validator). Their CI home is the
non-root Linux `daemon` race step (`.github/workflows/ci.yml:971,1055-1063`); §4 and §6 record the
local executions, not a Linux CI verdict.

The first line of each changed Go file is `//go:build linux && amd64`:
`daemon/internal/platform/helper_v2_protocol.go:1`,
`daemon/internal/platform/linux_owner_helper_v2_linux_test.go:1`,
`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:1`,
`daemon/native/linux-gate/protocol/native_image_pin_linux_test.go:1` and
`daemon/native/linux-gate/protocol/release_pin_label_linux_test.go:1`. Windows compiled none of
these files and ran none of the new rows. Both the historical local Windows gates and the
`daemon-windows` job (`ci.yml:1399-1402,1461`) cover the Windows build and its selected tests,
not the Linux-only changes.

## 6. Historical gate receipts by gated subject

Each Git tree ID is the digest manifest for its commit (paths, modes and blob IDs). The stage
checks used the matching worktree bytes for source and sanitizer gates and the named scratch
commits for committed-blob gates. File sets are relative to dispatch base `d45e3298`. §1 gives
SHA-256 for the five Go files and names the four document paths; the table below lists only
exceptions to the fix-pass-C SHA-256 values for the Go files, plan and record.

| Gated scratch commit | Tree digest and matched worktree snapshot | Changed path set |
| --- | --- | --- |
| Implement R2 `cf4ae2cf26863db37ad9d26552b41fd84e20ba4b` | `e68b33c19f5d4bc0565445033f5dfed1a022466a` = `07452d16` | 8: the five Go paths minus `native_image_pin_linux_test.go`, plus the four documents |
| Apply R2 `18bf015814bc13fddb60622b71b0e2de8ab77c8d` | `b1ca95cf52cc0ecade1d51c0cbcb0faf7e63e770` = `76cb92b1` | 9: all five Go paths and four documents |
| Fix pass C `97e86f752b1ba314100062a27ab2583dc0343aa4` | `c6c97c615b91a9a12b4b7b4e79c5ef7e3757d393` = the PR snapshot in Bind | The same 9 paths as apply R2 |

Fix-pass-C document SHA-256: plan `6c147ee680e63ac5adddc30037cac9f37d4778972c0aa550b08a7571c8515366`,
record `d2a47466d40f20a710b93cf157f3565482aaf255c8a2871a7e9898023a573f82`. Earlier deltas:

| Path | Implement R2 SHA-256 or scope | Apply R2 SHA-256 if different from fix pass C |
| --- | --- | --- |
| `native_image_pin_linux_test.go` | not changed from dispatch base (§1) | same as fix pass C |
| `release_pin_label_linux_test.go` | `a35137e577abc9668a87a086760f41f7bb59e3c3e7b16ccf67863af33eb259c8` (67 lines) | same as fix pass C |
| Plan | `4150aef7eff0482958aff9c4e137e1f18d3958cd30a6c37c346572171eb6804e` | `82024fbc6aaf662fe0429c0f7f130b800d7bca6656b921c1e8f84f63d69830c5` |
| Evidence record | `8aa8b67ba67b97daae83d43a8845163931e798918b604f6fdce6f3639bed4d40` | `bc3948ac38f7a7af00e18881695950a9c9af2dacce8fb9581050ea01d8a46d0e` |

The `CLOSED-TODO` closure wording also changed from implement R2 to apply R2 (which now names the
pre-rename heading); the tree manifests pin that change without a separate shared-doc SHA-256.

**Implement R2 (`cf4ae2cf`) — PARTIAL; SUPERSEDED by apply R2 (`18bf0158`)** for the changed
protocol test blobs and documents. The implement-only full suites below are historical, not
full-suite evidence for either later subject.

| Gate | Exit | Result |
| --- | ---: | --- |
| WSL Ubuntu-24.04, from `daemon/`: `gofmt -l .` | 0 | no file listed |
| WSL: `GOOS=linux go vet ./...` and `GOOS=linux go vet ./e2e/` | 0 / 0 | clean |
| Host (Windows, Go 1.27.4), from `daemon/`: `go vet ./...` | 0 | clean |
| WSL: `go test -count=1 -timeout 1800s ./...` | 0 | 31 packages `ok`, 0 fail, incl. `internal/app` 257.0s, `internal/platform` 40.2s, `native/linux-gate/protocol` 14.2s, `e2e` 1.5s |
| Host (Go 1.27.4), from `daemon/`: `go test -count=1 -timeout 1200s ./...` | 0 | 27 packages `ok`, 0 fail, incl. `internal/app` 201.4s, `cmd/symmetry-daemon` 0.7s |
| WSL: `go test -race -count=1 -timeout 1800s ./internal/platform/ ./native/linux-gate/protocol/` | 0 | `ok platform 73.671s`, `ok protocol 9.696s` (the two changed packages; the CI daemon job runs the same race shape over `./...`) |

**Apply R2 (`18bf0158`) — PARTIAL; SUPERSEDED by fix pass C (`97e86f75`)** for the re-edited plan
and record. Its changed-package tests remain evidence for the same Go blobs; WSL ran as
unprivileged `accept` because the platform wire harness refuses root.

| Gate | Exit | Result |
| --- | ---: | --- |
| WSL, from `daemon/`: `gofmt -l .` | 0 | no file listed |
| WSL: `GOOS=linux go vet ./...` and `GOOS=linux go vet ./e2e/` | 0 / 0 | clean |
| Host (Windows, Go 1.27.4), from `daemon/`: `go vet ./...` | 0 | clean |
| WSL: `go test -count=1 ./native/linux-gate/protocol/ ./internal/platform/` | 0 | `ok protocol 6.958s`, `ok platform 35.473s` (both changed packages; a head-wide `./...` re-run is the CI daemon job's and the conductor's) |
| WSL: `go test -race -count=1 ./internal/platform/ ./native/linux-gate/protocol/` | 0 | `ok platform 64.887s`, `ok protocol 8.032s` |

**Fix pass C (`97e86f75`)** — focused tests from `daemon/` in WSL Ubuntu-24.04 and vet on Windows
with Go 1.27.4. This receipt does not cover subsequent documentation edits or a head-wide rerun.

| Gate | Exit | Result |
| --- | ---: | --- |
| WSL, from `daemon/`: `gofmt -l .` | 0 | no file listed |
| WSL: `GOOS=linux go vet ./...` and `GOOS=linux go vet ./e2e/` | 0 / 0 | clean |
| Host (Windows, Go 1.27.4), from `daemon/`: `go vet ./...` | 0 | clean |
| WSL: `go test -count=1 -v -run '^TestPreparedGateReleaseLabelsTheNativeImagePinRefusal$' ./native/linux-gate/protocol/` | 0 | both subtests `--- PASS` (no skip) |
| WSL: `go test -count=1 ./native/linux-gate/protocol/ ./internal/platform/` | 0 | `ok protocol`, `ok platform` (both changed packages; a head-wide `./...` re-run is the CI daemon job's and the conductor's) |
| WSL: `go test -race -count=1 ./internal/platform/ ./native/linux-gate/protocol/` | 0 | both packages `ok` under the race detector |
| WSL, base override specified in §4: `go test -count=1 -run '^TestPreparedGateReleaseLabelsTheNativeImagePinRefusal$' ./native/linux-gate/protocol/` | 1 | both protocol subtests ran red as quoted in §4 |
| Same base override, restoring **both** `gate_lifecycle_linux.go` and `helper_v2_protocol.go`: `go test -count=1 -run '^TestLinuxOwnerHelperV2ReleaseErrorDetail' ./internal/platform/` | 1 | `basered-platform.txt:2-3`: undefined `linuxgate.ReleaseSubStepNativeImage` and `linuxgate.ReleaseArmNativeImagePin` at `linux_owner_helper_v2_linux_test.go:922:14` and `:922:51` (census row); no platform assertions ran. The validator at `:1225` uses literals. §4 M2 is a separate compile failure (`build-platform-M2.test.log:2`, duplicate case at `helper_v2_protocol.go:519:3`). |

Record and committed-blob gates, bound to the same three manifests above. `HEAD` in each quoted
command is that row's gated scratch revision, not the current branch tip.

| Gate | Exit | Result |
| --- | ---: | --- |
| `python -B scripts/ci/jscpd-changed.py --base d45e3298 --head HEAD` | 0 each | Fix C `97e86f75` and apply R2 `18bf0158`: 9 changed files; implement R2 `cf4ae2cf`: 8. Each scanned 1,795 sources, found 1,245 corpus clones and no new clone; `docs/archive/CLOSED-TODO.md` is outside scan scope. Fix C reported zero tolerated. |
| `comment-cap.py <clone> d45e3298` | 0 each | No added comment block over two lines on `cf4ae2cf`, `18bf0158` or `97e86f75`. |
| `python -B scripts/ci/check-record-cleanliness.py --base d45e3298 --head HEAD` | 0 each | One changed record passes at each of `cf4ae2cf`, `18bf0158` and `97e86f75`. |
| `python -B scripts/ci/check-citation-existence.py --base d45e3298 --head HEAD` | 0 each | One record passes with 722 path citations and three pre-existing ignore-rule notes on `cf4ae2cf`, `18bf0158` and `97e86f75`. |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` | 0 / 0 / 0 / 1 each | `18bf0158` and `97e86f75` worktree equivalents: record, plan and `docs/TODO.md` passed; `docs/archive/CLOSED-TODO.md` failed for unchanged spans from `d45e3298`. No implement-stage sanitizer receipt. |

## 7. Residual risks and limits

- The symlinked-component subtest stages under a trusted writable `HOME` (the #302 V5 idiom); where
  the environment cannot stage it, only that subtest skips. The environment-free subtest always
  runs, and M1-M3 red both subtests, so the label has a running guard everywhere.
- Scope: only the pin call-site refusal gained a label. Other pre-packet failures are outside the
  ruling (plan §5); the three unlabelled RELEASE outcome-unknown returns in §2 item 3 are still
  residuals. The green `synthetic_goal_cleanup` leg has no refusal stop to assert on, so its e2e
  assertions were not changed.
- Environment: no Resolute (ubuntu-26.04) userland exists on this host — the staged probe rootfs
  named for Resolute reports `VERSION_CODENAME=noble`, as does the host WSL distro — so this record
  claims no Resolute run; every red/green reproduction here is in Noble, and the runner userland
  remains CI's `ubuntu-latest` daemon job's to prove.
- `native_image` now names both the A-side pin stage and the guest clone-bootstrap stage
  (`ReleaseGuestStageNativeImage`). They are separate closed fields validated independently; the
  existing validator rows refuse a guest stage on a non-reading arm and an arm bound to another
  sub-step, so the two vocabularies cannot be mixed on the wire.
- The label's operator-facing value is proven at the row level (label + literal rendering); the
  credentialed stop that would display it (`release_outcome_unknown; stage=native_image arm=image_pin`)
  needs a future leg whose release image is not symlink-free and cannot be produced by the green
  case set.

## 8. Acceptance — BINDING run

The conductor's Bind below is green, its manifest passed, and the three exported label/stop
strings matched the historical green comparator. Neither run emitted a labelled RELEASE refusal;
that path is covered by the unit evidence, not by this comparison.

The historical comparator [run 45959717137](https://github.com/wxxb789/symmetry/actions/runs/45959717137)
checked out `5d27bcb0a792cd05b18ce520bd5df5e50fb2ae30` (tree
`d4d34e8b96bd58f0718d80ac4844aed77cea1e38`), not `897363bb` directly. The short SHA in the
preserved comparison below identifies its PR #305 product parent. The checkout is that parent
plus `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` and
`scripts/ci/scratch-gh-shim`, absent from the parent and byte-identical to the two scratch inputs
of the binding run. It is not PR #307 acceptance.

Round 2 corrects the records only. Under the conductor's churn bound, new findings confined to
record wording go to the conductor's TODO rather than a third revision round; any change to code,
the refusal, assertion meaning or acceptance still blocks.

## Bind

[Run 45991474220](https://github.com/wxxb789/symmetry/actions/runs/45991474220), attempt 1, resolves
§8 on PR #307 head `228415a52376548f6a54e6e09764455a6912a7e6`. Setup passed and the sole case set
`synthetic_goal_cleanup` stayed green. `synthetic-goal-cleanup.json` exported and the manifest
passed. The evidence ZIP (artifact `15194291171`) matched the API SHA-256
`dd3aced78268acdcb7162edebe120d5865179dc567a5a2d60c7aae9f58d573d3`.

Against [run 45959717137](https://github.com/wxxb789/symmetry/actions/runs/45959717137) on `897363bb`,
all three exported label/stop string members (`evidence_label`, `physical_stop.proof_kind`, and
`physical_stop.receipt_status`) matched byte-for-byte; none differed. Neither JSON member nor log
contains a labelled RELEASE refusal. This compares the green case, not the new refusal path.

Run head `2027cab23ed827de777b5901eea7bdfba4d9f169` matched all 1,905 PR entries by tree metadata and
blob SHA-256. Only the two scratch files were added; their bytes match PR #304 Bind 2. Compared with
that Bind (`f3dbae66`), 1,890 PR entries are unchanged. Only case-set input deltas are listed; the
other changes are docs and standalone test files outside this case set.

| Case-set input changed since PR #304 Bind 2 | Run `2027cab23` SHA-256 = PR `228415a5` SHA-256 |
| --- | --- |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `7c486e5415aa24c841016d0a9019d4a46309099f76358adb9af41e021ad4b7d9` |
| `daemon/internal/platform/helper_v2_protocol.go` | `29c2251101903bdd8d3ef7c4b3696c198c3c66ee81356d607e514e20e251d358` |
| `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` | `c2adf63fae19559cce34c77937443f6da16736f940a195e2cca040ead0dd6a2e` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `a7c453cb75958c218bd7a09dac4e56a1f749aef2f008e22886b23ac46307c360` |

**Binding provenance:** The PR head named above has parent `76cb92b12c7b8eac76b24bc75d691c7b2dc7a5f4`
and the fix-pass-C tree digest in §6. The actual run checkout's parent is that PR head and its
tree digest is `a9a3ada472dc94352e74c53e97496573b3c0d6a5`. Its file set is the complete PR tree
plus the two scratch-only paths named in §8. Their SHA-256 values, in workflow/shim order, are
`8beeadf6d00d73dac74a10a49722c9b767dab20010875938c8af5725e8fb389f` and
`507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`.
