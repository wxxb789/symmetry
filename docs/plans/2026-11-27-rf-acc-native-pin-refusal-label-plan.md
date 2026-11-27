# rf-acc-native-pin-refusal-label — implement plan

Contract: `goal-rf-acc-native-pin-refusal-label.md` (GDD state root, outside the repository).
Dispatch base `d45e3298` (`main`), branch `refactor/rf-acc-native-pin-refusal-label`. Unless
specified otherwise, source anchors refer to the dispatch base. The accepted plan is the contract
plus the residual it closes; this file resolves the open choices (the closed vocabulary code, the
new assertion, and the evidence boundary). The stage subjects and exact PR Bind are recorded in
`docs/evidence/rf-acc-native-pin-refusal-label-2026-11-27.md`.

## 1. Clause census at the dispatch head

| Contract clause | Tree state at `d45e3298` | This increment |
| --- | --- | --- |
| The refusal stays exactly as it is (pin, predicates, refusal) | `pinNativeImage` (`daemon/native/linux-gate/protocol/proc_identity_linux.go:74`) refuses symlinked components, non-canonical paths and non-approvable nodes; untouched | No production predicate, message or wrap inside the pin changes |
| The label is supplied at the `PreparedGate.Release` call site (`gate_lifecycle_linux.go:928-932`) | The site returns a bare `fmt.Errorf("pin A-approved native executable before RELEASE: %w", err)`; `linuxOwnerHelperV2ReleaseErrorDetailFor` (`daemon/internal/platform/helper_v2_protocol.go:591`) finds no `ReleaseSubStepError` and leaves `Stage = "none"` (`:592`) | §3: one `NewReleaseSubStepError` wrap at that site; the message text is preserved verbatim by `ReleaseSubStepError.Error()` |
| Go-side change only; no C helper or wire-format change | `daemon/native/linux-gate/**` untouched; the detail JSON is Go-to-Go (daemon ↔ helper) | §3 touches only owned Go files |
| Every currently-labelled stop renders byte-identically; no existing assertion relaxed | Pinned by the literal renderings in `TestLinuxOwnerHelperV2ReleaseErrorDetailNamesEveryArm` (`daemon/internal/platform/linux_owner_helper_v2_linux_test.go:901`) and the classifier table (`:715`) | §3 adds rows and one `Validate()` accepted value; no existing row, literal or check changes |
| The new row is a `PreparedGate.Release`-level row that reddens when the labelling is removed or dropped to `stage=none` | The pin probe (`native_image_pin_linux_test.go`) calls `pinNativeImage` directly and asserts the opposite (no `ReleaseSubStepError`) — it cannot cover the call site | §3: `TestPreparedGateReleaseLabelsTheNativeImagePinRefusal`; §4 mutants |
| Owned files | — | The two protocol production files, the protocol package's tests, the helper protocol file and its tests, this plan, the evidence record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` |
| Acceptance: the conductor's Bind keeps `synthetic_goal_cleanup` green and the record names the new row and its mutant | Not locally runnable (installed owner, guest, Control) | §4 and the record name the rows and mutants; the Bind stays the conductor's |

## 2. Stage 1 — where the label is lost (trace)

1. The C gate sends nothing for this failure class: `PreparedGate.Release` pins `command.Path`
   before it builds or sends the release packet (`gate_lifecycle_linux.go:928`, before
   `encodeGuardedRelease` at `:963` and `sendPacketWithRights` at `:984`). No wire or C helper
   vocabulary is involved.
2. The Go renderer cannot invent a stage: `linuxOwnerHelperV2ReleaseErrorDetailFor`
   (`daemon/internal/platform/helper_v2_protocol.go:591`) classifies only through
   `errors.As(err, &subStep)` (`:593-600`), and its default is `linuxOwnerHelperV2ReleaseErrorDetail{Stage: "none"}`
   (`:592`). An untyped error renders `stage=none` (`String()`, `:486-509`).
3. The other labelled release stops already label at their own call site with the closed
   `gate_lifecycle_linux.go:635-639` sub-step vocabulary and `:645-669` arm vocabulary: the clone
   bootstrap verify at `:918`, the pre-send gate identity at `:944`, the post-send wait and exec
   arms at `:1025-1052`, and the clone-release arms at `:1102-1171`, plus
   `helper_v2_server.go:1121-1185`. Three other outcome-unknown returns in the same RELEASE path
   stay unlabelled (`:975`, `:998`, `:1032`) and are listed out of scope in §5.
4. This goal targets the unlabelled image-approval refusal at `gate_lifecycle_linux.go:928-932`.
   It makes no claim that other pre-packet checks ignore leg inputs: `command.Validate` can reject
   the executable path, for example (`wire_linux.go:416-425`).

Wrong layer: **`PreparedGate.Release`'s pin call site**, which knows the release context but returns
the pin's error unwrapped. `pinNativeImage` is the wrong place to label: it is also called for the
fixed clone bootstrap (`proc_identity_linux.go:227` inside `verifyInstalledCloneBootstrap`), whose
failure is already labelled `clone_bootstrap`/`installed_bootstrap` at `gate_lifecycle_linux.go:918`
with an outer wrap, and `NewReleaseSubStepError` never re-labels an already-labelled error
(`:833-857`), so an inner label would misname either caller.

## 3. The change

### 3.1 Label choice (the ruling's open choice)

The pre-packet pin refusal occurs before anything is sent and before the gate identity check
(`:944`). Candidate labels, all expressible in the existing closed vocabularies:

- **(a) chosen — a new sub-step `native_image` with one arm `image_pin`.**
  `stage=native_image arm=image_pin` names the failing stage truthfully: the release never started,
  and the step that failed is the pinning/approval of the native image. It mirrors the C side's own
  name for the same logical step (`ReleaseGuestStageNativeImage = "native_image"`,
  `gate_lifecycle_linux.go:687`), so an operator sees one step name for image preparation on both
  sides. Cost: one new sub-step token in the Go-side closed vocabulary, so
  `LinuxOwnerHelperV2ReleaseErrorDetail.Validate()` (`helper_v2_protocol.go:516-519`) accepts it.
  Additive only; existing tokens, arms and rendered strings are unchanged, so no currently-labelled
  stop can change bytes.
- **(b) rejected — reuse `native_exec` with a new arm.** `stage=native_exec arm=image_pin` needs no
  `Validate()` change but names a stage that never ran: the exec-status channel arms of
  `native_exec` (`await_result`, `exec_errno`, `no_exec_attempt`, `native_process`, `exec_readback`,
  `clone_exec_proof`) all describe post-send observations, and the pin refusal is pre-send.
- **(c) rejected — reuse `gate_authorize`.** Its arms are the gate/anchor identity checks
  (`missing_gate_identity`, `anchor_authorize`, `gate_identity`); the image pin is not a gate
  identity check, and routing it there would misdirect the next leg.
- **(d) rejected — return `DECISION_NEEDED` and change the C helper or the wire format.** The ruling
  forbids it and it is unnecessary: the detail vocabulary is Go-side.

Errno stays `0` because the wrapper passes zero deliberately for this bounded pre-packet detail
(the same choice the existing pre-packet wraps `installed_bootstrap` and `gate_identity` make), not
because the failure cannot be a syscall error: the class includes `ENOTDIR` from the
parent-component open and `ENOENT` from the image open, each kept inside the pin's own message
text. The arm/wait/read/guest shapes of the new arm are all false, so the detail carries only the
stage and the arm.

### 3.2 Edits

- `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go`
  - sub-step vocabulary (`:635-639`): `ReleaseSubStepNativeImage = "native_image"`;
  - arm vocabulary (`:645-669`): `ReleaseArmNativeImagePin = "image_pin"`;
  - shape table (`LookupReleaseArmShape`, `:726-748`): the arm binds to `ReleaseSubStepNativeImage`
    with no wait class, no clone read and no guest stage;
  - the pin call site (`:928-932`): wrap the failure in
    `NewReleaseSubStepError(ReleaseSubStepNativeImage, ReleaseArmNativeImagePin, 0, fmt.Errorf(...))`,
    keeping the existing message and `%w`.
- `daemon/internal/platform/helper_v2_protocol.go`: `Validate()`'s stage switch (`:516-519`) accepts
  `linuxgate.ReleaseSubStepNativeImage`.
- `daemon/native/linux-gate/protocol/release_pin_label_linux_test.go` (new):
  `TestPreparedGateReleaseLabelsTheNativeImagePinRefusal` builds a `PreparedGate{state: gatePrepared,
  config: …}` with the package's own scratch pipes (`newScratchNativePipes`) and owner-pipe guard
  (`newScratchReleaseGuard`), calls the real `Release`, and asserts the returned error carries
  `stage=native_image arm=image_pin` with the pin's own detail intact. Two inputs: a planted
  symlinked parent component in a trusted staging tree (the recorded stop's class; skip-guarded base
  by the existing `trustedNativeImagePinBase` idiom) and an image with no A-controlled parent
  (environment-independent). Expectations are literals, never derived from the constants, so a
  swapped constant value reddens here too.
- `daemon/internal/platform/linux_owner_helper_v2_linux_test.go`: the arm census
  (`TestLinuxOwnerHelperV2ReleaseErrorDetailNamesEveryArm`) gains the literal rendering
  `stage=native_image arm=image_pin`; the closed-vocabulary validator table gains the accepted shape
  `{Stage: "native_image", Arm: "image_pin"}`. Both tables keep every existing row unchanged.

### 3.3 Simplification review

No abstraction is added: one closed-vocabulary constant pair, one table row and one wrap at the
site that owns the context. The alternative (b)/(c) reuses make the diagnostic less truthful, not
smaller in any load-bearing way. Nothing is deleted: the trace shows no dead code this change
obsoletes.

## 4. Evidence plan

1. Red first: run the new protocol row with the dispatch-base lifecycle and helper protocol files
   (unlabelled refusal).
2. Green on the reviewed code/test snapshot `76cb92b1` (parent `07452d16`): both protocol subtests
   and the platform detail tables, including the new census and validator rows. The existing
   rendering expectations in those tables stay unchanged.
3. Use a disposable scratch copy for each mutant. M1 (wrap removed) and M2 (stage changed to
   `native_exec`) fail the protocol row; M2 instead fails to **compile** the platform package at
   `helper_v2_protocol.go:519` (duplicate switch case). M3 (arm renamed) fails the platform census;
   M4 (stage omitted from `Validate()`) and M5 (arm rebound) fail the census and validator. See
   the evidence record §4 for mutant digests and observed diagnostics.
4. Local red/green reproduction in WSL Ubuntu-24.04 (`VERSION_CODENAME=noble`): both protocol
   subtests ran on each side. No Resolute run was performed; this is not CI runner evidence. In
   the base override the two undefined names come from the census row alone
   (`linux_owner_helper_v2_linux_test.go:922`, reviewed snapshot); the validator uses literals
   (`:1225`) and does not execute on that build failure. M3–M5 supply the runtime platform reds.
5. Evidence record §6 binds the three local gate subjects and digests. The implement-stage full
   suites are historical; apply and fix pass C covered the affected Linux packages, not a repeat
   of the full suites. Windows gates checked its own build, excluding all five Linux/amd64 files.
   The Linux `daemon` CI job owns head-wide unit coverage, separately from the credentialed Bind.
6. Closure: the residual moves from `docs/TODO.md` to `docs/archive/CLOSED-TODO.md` with the
   record's pointer.

## 5. Out of scope

- The C helper (`daemon/native/linux-gate/c-gate/**`, `native/linux-gate/**`) and any wire-format or
  packet change: the ruling forbids them and the stage is known on the Go side.
- Pre-packet failures other than image approval are outside the ruling, including command
  validation, guard/owner-pipe handling, and encoding (which can reject an oversized command or
  grant, `wire.go:715-730`).
- The three other unlabelled outcome-unknown returns in the RELEASE path
  (`gate_lifecycle_linux.go:975`, `:998`, `:1032`; §2 item 3): pre-existing, recorded as residual
  in the evidence record (§7); unchanged here.
- `daemon/e2e/**` assertion changes: after #302 the `synthetic_goal_cleanup` leg releases a resolved
  symlink-free image, so no stop exists in the green set to assert a label on; the e2e bytes stay
  untouched and the Bind re-run is the conductor's.
- The pin's own predicates, messages and the `assertUnlabelledNativeImagePinRefusal` rows: the label
  is added at the call site, so the pin-level probe's "unlabelled at the pin" assertion stays true.
