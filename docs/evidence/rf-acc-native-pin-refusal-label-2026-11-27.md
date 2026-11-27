# rf-acc-native-pin-refusal-label — evidence record

Contract: `goal-rf-acc-native-pin-refusal-label.md` (GDD state root, outside the repository).
Dispatch base and delivered parent revision: `d45e3298` (`main`). Branch:
`refactor/rf-acc-native-pin-refusal-label`. Plan:
`docs/plans/2026-11-27-rf-acc-native-pin-refusal-label-plan.md`. Every row below ran on the
delivered bytes whose digests are in §1 unless the row names another revision; the conductor's Bind
on the PR head is the acceptance and is not claimed here (§8).

## 1. Subject bytes (sha256)

| File | Dispatch head `d45e3298` | Delivered | Diff |
| --- | --- | --- | --- |
| `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` | `76f8a0ff803af08a359bbc18e1275d0363308328044209cf8e91e346c8ac91d8` | `c2adf63fae19559cce34c77937443f6da16736f940a195e2cca040ead0dd6a2e` | +6/−1 |
| `daemon/native/linux-gate/protocol/release_pin_label_linux_test.go` | absent | `3d3a814056f29804496279aca99a7d12f3450004152cdfb43e1414cf00e264a9` | new, 56 lines |
| `daemon/native/linux-gate/protocol/native_image_pin_linux_test.go` | `2c46a46ec66de8a8a62f5dded2724826bd0a008822530dd31efcb20edabd52f0` | `b34b09d38f5f1b4c6e2250a5d119f5dca04ea081a4caee4e13ad1619ba7bda03` | +19/−15 |
| `daemon/internal/platform/helper_v2_protocol.go` | `4ac73de34f901c91a3cef10e3704e9530f520db321deb6af4e38669bc6eb9d75` | `29c2251101903bdd8d3ef7c4b3696c198c3c66ee81356d607e514e20e251d358` | +2/−1 |
| `daemon/internal/platform/linux_owner_helper_v2_linux_test.go` | `17c7e40ec0e551debfaf7accfb3c7f48f72e822deff4fdfd9058348a71616fa7` | `e3f904b61529397c4b81fb979bd54d30aa0e38c27557d0ed72ac2cb7541fe396` | +5/−0 |

Documents: `docs/plans/2026-11-27-rf-acc-native-pin-refusal-label-plan.md` (new,
`82024fbc6aaf662fe0429c0f7f130b800d7bca6656b921c1e8f84f63d69830c5`), `docs/TODO.md`
(`6af9c33f94c461217bf1e69a1ee49060798b9b51c78fc6e19cf66c10cb5f7212`),
`docs/archive/CLOSED-TODO.md` (`ec561213e57d336dcf7bafb3c912cbb06c817c698c17530082ea27c2a6aac468`),
this record (see §6 for the committed-blob gates that bind it; a record cannot contain its own
digest).

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
3. **Every other release failure site labels at its own call site** with the closed vocabulary
   (`gate_lifecycle_linux.go:635-639` sub-steps, `:645-669` arms, `:726-748` shapes): the clone
   bootstrap verify at `:918`, the pre-send gate identity at `:944`, the post-send exec-status arms
   at `:1025-1052` and `:1102-1171`, plus `helper_v2_server.go:1121-1185`.
4. **The pin site was the one unlabelled, leg-specific pre-packet refusal.** Its input is the leg's
   own image path; the other pre-packet refusals (`command.Validate`, `validateReleaseOwnerPipe`,
   `monotonicDeadlineNS`, owner-pipe duplication, `encodeGuardedRelease`) are program-independent
   and stay out of scope.

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
`gate_authorize` because its arms are the gate/anchor identity checks. Errno stays `0`; the arm's
wait-class, clone-read and guest-stage shapes are all false, so the detail carries only the stage
and arm.

| Delivered site | Change |
| --- | --- |
| `gate_lifecycle_linux.go:638` | `ReleaseSubStepNativeImage = "native_image"` |
| `gate_lifecycle_linux.go:662` | `ReleaseArmNativeImagePin = "image_pin"` |
| `gate_lifecycle_linux.go:741-742` | shape table binds the arm to the new sub-step with no wait class, clone read or guest stage |
| `gate_lifecycle_linux.go:936-937` | the pin failure returns `NewReleaseSubStepError(ReleaseSubStepNativeImage, ReleaseArmNativeImagePin, 0, fmt.Errorf("pin A-approved native executable before RELEASE: %w", err))`; `ReleaseSubStepError.Error()` renders the inner message verbatim, so the refusal text is unchanged |
| `helper_v2_protocol.go:516-519` | `Validate()`'s closed stage switch accepts `linuxgate.ReleaseSubStepNativeImage` |
| `linux_owner_helper_v2_linux_test.go:922` | arm census row, literal `stage=native_image arm=image_pin` |
| `linux_owner_helper_v2_linux_test.go:1223-1226` | validator accepted-shape row `{Stage: "native_image", Arm: "image_pin"}` |

Census (`grep -rn "native_image\|image_pin" daemon/ --include=*.go`, delivered bytes, 9 hits): the
two new token sites above, the pre-existing `ReleaseGuestStageNativeImage = "native_image"`
guest-stage constant and its `CloneFailureNativeImage` row, and the new/updated test rows (8 token
sites) — no other token site; the ninth hit is an incidental `open_native_image` substring inside a
pre-existing message literal (`gate_census_linux_test.go:1214`). The vocabulary gained exactly one
sub-step and one arm.

Existing strings unchanged: the two production diffs are the added vocabulary/shape/wrap lines and
the replacement of the one unwrapped return (`git diff --numstat d45e3298`: `gate_lifecycle_linux.go`
+6/−1, `helper_v2_protocol.go` +2/−1); the census table's existing literal rows are unedited (+1
row, 0 changes), so every previously rendered `stage=… arm=…` literal is preserved, and the
classifier, wire and redaction tests pass unchanged on the delivered bytes (§6, §7).

## 4. Red first, the new rows, and the mutants

Red first (scratch tree, base production bytes plus the two final test files, WSL Ubuntu-24.04 as
the unprivileged `accept` user, `daemon/`):
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

Mutants (each applied to a copy of the delivered tree in the scratch area, never to the worktree;
the delivered files re-hashed after the pass: `gate_lifecycle_linux.go` `c2adf63fae19559c…`,
`helper_v2_protocol.go` `29c2251101903bdd…` — unchanged). Rows ran in the runner-mirroring resolute
chroot, non-root A shape (`setpriv --reuid=52000 --clear-groups`, `HOME` under the trusted probe
home), on the mutant's own binary:

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

Runner-mirroring red→green on the same rows (resolute chroot, `VERSION_CODENAME=resolute`, A shape,
test binaries built from the scratch trees; re-run by the apply stage on the final bytes): base
production bytes + the two final test files → exit 1 (verbatim above, red); the final bytes → exit
0 for the new row and for every `TestLinuxOwnerHelperV2ReleaseErrorDetail*` row (classifier,
census, read class, redaction, closed-vocabulary validator).

## 5. New-assertion attribution (Bind versus CI)

The conductor's Bind executes the credentialed `synthetic_goal_cleanup` case set, not Go unit
tests; on the green head that leg releases a resolved symlink-free image, so the new label adds no
stop to it and the Bind's role for this clause is regression: the set stays green and every
previously-labelled stop in that log renders byte-identically (contract Acceptance). The new rows
execute in the CI `daemon` job (`.github/workflows/ci.yml`: `go test -race -count=1 -timeout 600s
-json ./...` on `ubuntu-latest`, non-root runner) and locally in WSL, the resolute chroot and
Windows compilation. They are not executed by the Bind; the Bind does not become the first place
they run.

## 6. Gates on the delivered bytes (§1 digests)

The apply stage's accepted simplifications edit the two protocol test files (the subtest rename and
the shared staging helper in `native_image_pin_linux_test.go`) and the plan, this record and
`CLOSED-TODO` — no Go source. The implement-stage WSL rows whose package is `native/linux-gate/protocol`
are PARTIAL and SUPERSEDED there by the apply-stage rows below on the final bytes
(`release_pin_label_linux_test.go` `3d3a8140…`); the Windows rows are unaffected
(`//go:build linux && amd64`), and the head-wide full suites are CI's
(`.github/workflows/ci.yml` daemon job) and the conductor's.

Implement-stage receipts (WSL full suite and WSL race superseded for the protocol package):

| Gate | Exit | Result |
| --- | ---: | --- |
| WSL Ubuntu-24.04, from `daemon/`: `gofmt -l .` | 0 | no file listed |
| WSL: `GOOS=linux go vet ./...` and `GOOS=linux go vet ./e2e/` | 0 / 0 | clean |
| Host (Windows, Go 1.27.4), from `daemon/`: `go vet ./...` | 0 | clean |
| WSL: `go test -count=1 -timeout 1800s ./...` | 0 | 31 packages `ok`, 0 fail, incl. `internal/app` 257.0s, `internal/platform` 40.2s, `native/linux-gate/protocol` 14.2s, `e2e` 1.5s |
| Host (Go 1.27.4), from `daemon/`: `go test -count=1 -timeout 1200s ./...` | 0 | 27 packages `ok`, 0 fail, incl. `internal/app` 201.4s, `cmd/symmetry-daemon` 0.7s |
| WSL: `go test -race -count=1 -timeout 1800s ./internal/platform/ ./native/linux-gate/protocol/` | 0 | `ok platform 73.671s`, `ok protocol 9.696s` (the two changed packages; the CI daemon job runs the same race shape over `./...`) |

Apply-stage receipts on the final bytes (the two changed packages; WSL runs as the unprivileged
`accept` user, which the platform wire harness requires — a root process is refused):

| Gate | Exit | Result |
| --- | ---: | --- |
| WSL, from `daemon/`: `gofmt -l .` | 0 | no file listed |
| WSL: `GOOS=linux go vet ./...` and `GOOS=linux go vet ./e2e/` | 0 / 0 | clean |
| Host (Windows, Go 1.27.4), from `daemon/`: `go vet ./...` | 0 | clean |
| WSL: `go test -count=1 ./native/linux-gate/protocol/ ./internal/platform/` | 0 | `ok protocol 6.958s`, `ok platform 35.473s` (both changed packages; a head-wide `./...` re-run is the CI daemon job's and the conductor's) |
| WSL: `go test -race -count=1 ./internal/platform/ ./native/linux-gate/protocol/` | 0 | `ok platform 64.887s`, `ok protocol 8.032s` |
| `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record, the plan and `docs/TODO.md` | 0 / 0 / 0 | no host absolute path anywhere in the three; `docs/archive/CLOSED-TODO.md` exits 1 for spans already present at `d45e3298` (the base and current renders differ only by the added closure block, which carries none) |

Committed-blob gates ran on a scratch clone of `d45e3298` plus exactly the §1 files. They ran twice
— on the intermediate clone revision `1c468ff7192f02d5fee76e2cd3cdb3f4ac71a4c8` (these subject
bytes, this section not yet written) and on the final revision, which differs only in this section
(the hand-off report names that revision; a record cannot carry its own commit). Both runs exited 0
with the outcomes below:

| Gate | Exit | Result |
| --- | ---: | --- |
| `python -B scripts/ci/jscpd-changed.py --base d45e3298 --head HEAD` (clone root) | 0 | 8 changed file(s), 1795 scanned source(s), 1245 clone(s) in the corpus, 0 tolerated among the added lines, none new; the notice records that `docs/archive/CLOSED-TODO.md` is outside the configured scan scope |
| the owner comment-cap probe on the clone commit, base `d45e3298` | 0 | no comment block over the 2-line cap among the added lines |
| `python -B scripts/ci/check-record-cleanliness.py --base d45e3298 --head HEAD` | 0 | 1 changed record passes the root-only check (no host absolute path in this record) |
| `python -B scripts/ci/check-citation-existence.py --base d45e3298 --head HEAD` | 0 | 1 changed document passes the path and symbol check (722 path citations, with three ignore-rule notes for pre-existing portal and deps citations) |

The apply stage re-ran the same four gates on its own scratch clone of `d45e3298` plus exactly the
§1 files: on the intermediate revision `3b604c5c2b89f1fdbdc3873729466c3174c31f2a` (this section not
yet written) and on the final revision, which differs only in this section (the stage report names
it; a record cannot carry its own commit). Both runs exited 0 with the outcomes below:

| Gate | Exit | Result |
| --- | ---: | --- |
| `python -B scripts/ci/jscpd-changed.py --base d45e3298 --head HEAD` (clone root; `git merge-base HEAD origin/main` = `d45e3298`) | 0 | 9 changed file(s), 1795 scanned source(s), 1245 clone(s) in the corpus, 0 tolerated among the added lines, none new; the notice records that `docs/archive/CLOSED-TODO.md` is outside the configured scan scope |
| the owner comment-cap probe on the clone commit, base `d45e3298` | 0 | no comment block over the 2-line cap among the added lines |
| `python -B scripts/ci/check-record-cleanliness.py --base d45e3298 --head HEAD` | 0 | 1 changed record passes the root-only check |
| `python -B scripts/ci/check-citation-existence.py --base d45e3298 --head HEAD` | 0 | 1 changed document passes the path and symbol check (722 path citations, with three ignore-rule notes for pre-existing portal and deps citations) |

## 7. Residual risks and limits

- The symlinked-component subtest stages under a trusted writable `HOME` (the #302 V5 idiom); where
  the environment cannot stage it, only that subtest skips. The environment-free subtest always
  runs, and M1-M3 red both subtests, so the label has a running guard everywhere.
- Scope: only the pin call site is labelled. The program-independent pre-packet refusals stay
  unlabelled by the ruling (plan §5); the e2e bytes are untouched because the green
  `synthetic_goal_cleanup` leg has no stop to assert on.
- `native_image` now names both the A-side pin stage and the guest clone-bootstrap stage
  (`ReleaseGuestStageNativeImage`). They are separate closed fields validated independently; the
  existing validator rows refuse a guest stage on a non-reading arm and an arm bound to another
  sub-step, so the two vocabularies cannot be mixed on the wire.
- The label's operator-facing value is proven at the row level (label + literal rendering); the
  credentialed stop that would display it (`release_outcome_unknown; stage=native_image arm=image_pin`)
  needs a future leg whose release image is not symlink-free and cannot be produced by the green
  case set.

## 8. Acceptance still pending

Delivered, pending the conductor's Bind of `synthetic_goal_cleanup` once on the PR head: the set
stays green (run `45959717137` on `897363bb` is the green prior-head record) and every
previously-labelled stop in that log renders byte-identically. Neither the local gates nor the
resolute-chroot rows count as that acceptance.
