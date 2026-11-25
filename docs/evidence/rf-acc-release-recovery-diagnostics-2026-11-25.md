# rf-acc-release-recovery-diagnostics — evidence record

Goal: `rf-acc-release-recovery-diagnostics` (contract `goal-rf-acc-release-recovery-diagnostics.md`,
GDD state root). Branch `refactor/rf-acc-release-recovery-diagnostics`, base revision `48ef0982`
(#284 `rf-acc-witness-unresolved-exit`; #281 supplies the case sets). Rulings Q-A(a) (one staged
goal), Q-B(a) (typed sub-step errors + one additive bounded detail; `error_code` unchanged),
Q-C(a) (minimal redacted refusal document), Q-D(a) (the four `stale_journal_*` rows hand off to
`rf-acc-owner-loss-semantics`) and the 2026-11-25 ruling on the implement `DECISION_NEEDED` (stage 1,
the `stale_leaf` stage-2 fix and the refusal document are the accepted base; the bounded detail gains
a closed arm/wait-class/guest-stage sub-code and one `migration`-only rerun on the final bytes —
§2.8, §6, §7.5, §9). Plan: `docs/plans/2026-11-25-rf-acc-release-recovery-diagnostics-plan.md`.

Line numbers below are the dispatch head `48ef0982` unless a section says otherwise.

## 1. Clause census at the dispatch head (re-located by content)

| Contract | Head `48ef0982` | Disposition |
| --- | --- | --- |
| helper `:1184-1203` | `helper_v2_server.go:1184-1210` | converted: the unresolved block computes the bounded detail and writes it via `writeResponseWithDetail` |
| helper `:1126-1139` | `helper_v2_server.go:1127-1137` | converted: `gate_authorize` typed wraps around the missing-identity error and `AuthorizeGateRelease` |
| helper `:1169-1173` | `helper_v2_server.go:1172-1183` | converted: `register` typed wraps around `RegisterProcess` and `ensureGateWait` |
| helper `:1073-1099` | `helper_v2_server.go:1073-1099` | untouched: pre-attempt rejections, never the unresolved path |
| `linux_owner_helper_v2_linux.go:301-317` | `:301-322` | extended: `Detail` field + `(<code>; <detail>)` rendering; code-only rendering byte-identical |
| `helper_v2_protocol.go:516-540` | `:591,:598-609` | converted: typed for every `release`/`unresolved` response; detail validated on the wire |
| `linux_owner_v2_linux.go:1066` | unchanged | `%w` carries the new text |
| `runner.go:864` | unchanged | `%w` carries the new text |
| app `:3279` | `:3457` | unchanged |
| app `:531-534` | `:531-539` | converted: guard + reason + recovery class named, `recoveryErr` chain joined, refusal document written for the stage-replay arm |
| app `:2801-2811` | `:2817-2827` | predicate unchanged; companion reason function + refusal builder added |
| app `:737-742` | `:741-746` | converted: `linuxV2NativeWitnessUnresolvedSummaryTripped` names each tripped predicate |
| app `:3211` | `:3287` | the `native_release` hold (unchanged: `migration` never reaches it — §9) |

## 2. Stage-1 changes (behavior-preserving diagnostics)

1. **Release sub-step type** `linuxgate.ReleaseSubStepError` (`gate_lifecycle_linux.go:633-663`) with
   the closed vocabulary `gate_authorize | clone_bootstrap | clone_checkout | native_exec |
   register`; `Error()` returns the wrapped message verbatim, so every current line
   (`native exec failed after RELEASE: errno=1` at `:817`) stays byte-identical. Wrap sites:
   `:696` (clone bootstrap verify), `:722` (pre-RELEASE gate identity), `:803,:816,:823,:830`
   (native exec result, errno on `:816`), `:880,:884,:896,:900` (clone bootstrap),
   `:889,:893,:911,:922,:944` (clone checkout), `:949` (clone native exec proof); platform:
   `helper_v2_server.go:1121` (checkout receipt), `:1129,:1134` (gate authorize),
   `:1177,:1180` (register).
2. **Detail wire type** `LinuxOwnerHelperV2ReleaseErrorDetail`
   (`helper_v2_protocol.go:470-540`): `stage` always set (`none` for an untyped error), `errno` only
   when the sub-step carries one, `reason` only when `err == nil`
   (`owner_lost | lease_expired | stop_requested`). `Validate()` enforces both closed vocabularies
   and rejects a reason mixed with a sub-step; `readLinuxOwnerHelperV2Response` runs it after decode
   (`:591`), so a free-form field from the wire is refused, never carried.
3. **Additive response field** `release_error_detail` (`:126`), written only by
   `writeResponseWithDetail` (`helper_v2_stop.go:296-323`); `writeResponse` delegates with nil, so
   the 160+ call sites in unowned files keep their signature.
4. **Carry-through**: `LinuxOwnerHelperV2ReleaseUnresolvedError.Detail`
   (`linux_owner_helper_v2_linux.go:301-322`); `linuxOwnerHelperV2ResponseError` builds the typed
   error for every `release`/`unresolved` response (`helper_v2_protocol.go:598-609`). New stderr
   tail: `… release outcome is unresolved (release_outcome_unknown; stage=native_exec errno=1)`.
5. **Refusal naming** (`linux_owner_witness_linux.go:2929-3045`): guards
   `case_not_stage_replay` / `journal_without_unresolved_authority`; authority reasons
   `release_proof_present | clone_abort_proof_present | clone_promotion_receipt_present |
   no_unresolved_term`; recovery classes `cancelled | timeout | owner_lost | helper_recovery_only |
   outcome_unknown | gate_never_produced | cgroup_unresolved | recovery_failed`.
6. **Refusal document** (`:2070-2143`): identity fields + `recovery_refusal` only; the writer
   validates the encoded bytes against the closed key set before writing, and the validator rejects
   any authority key by name. The e2e decoder ignores the unknown key, so no e2e byte changes.
7. **Summary diagnostics** (`:716-734`): the ten predicates are named, not silently ORed.

### 2.8 Arm, wait class and guest stage (stage 3, conductor ruling 2026-11-25)

`stage=native_exec` with no errno named four sites, so it could not route the guest-side fix. The
bounded detail now carries a closed **arm** code for every wrap site of every multi-arm sub-step the
migration path can reach (`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:642-669`
vocabulary, `:691-708` types, `:710-801` shape table, wait-class and guest-stage classifiers and the
constructor), plus the closed **wait class** of the arm's exec-status read and the closed **guest
stage** (+ the guest's errno) of a decoded guest clone-bootstrap failure:

| Sub-step | Arms (closed) | Wrap sites (base `48ef0982`, all owned) |
| --- | --- | --- |
| `gate_authorize` | `missing_gate_identity`, `anchor_authorize`, `gate_identity` | `helper_v2_server.go:1129`, `:1134`; `gate_lifecycle_linux.go:722` |
| `clone_bootstrap` | `installed_bootstrap`, `bootstrap_attempt`*, `bootstrap_image`, `bootstrap_process`, `leaf_membership` | `gate_lifecycle_linux.go:696`, `:880`*, `:884`, `:896`, `:900` |
| `clone_checkout` | `checkout_receipt`*, `checkout_receipt_mismatch`, `checkout_callback`, `checkout_ack_encode`, `checkout_ack_send` | `gate_lifecycle_linux.go:889`*, `:893`, `:911`, `:922`, `:944`; `helper_v2_server.go:1121` |
| `native_exec` | `await_result`*, `exec_errno`, `no_exec_attempt`, `image_mismatch`, `clone_exec_proof`* | `gate_lifecycle_linux.go:803`*, `:816`, `:823`, `:830`, `:949`* |
| `register` | `native_register`, `gate_waiter` | `helper_v2_server.go:1177`, `:1180` |

`*` = reads the exec-status channel: its detail also carries the wait class (`deadline | peer_closed |
other`, computed from that arm's own error by `ReleaseWaitErrorClass`, so a joined abort error cannot
reclassify it) and, for the three clone-status arms, `guest_stage`
(`request | seed | target | git | checkout | native_image | exec`) with the guest-reported errno from
`CloneBootstrapFailureError` (`wire.go:204`, sent by `daemon/native/linux-gate/c-gate/gate.c:1140-1204`
at `CLONE_FAILURE_EXEC_STAGE`; the frame rejects `errno == 0` and the C gate falls back to `EPERM`, so
the guest errno is the only errno the clone arms can carry). `LookupReleaseArmShape` is the single closed table
binding each arm to its sub-step and to those two flags; `helper_v2_protocol.go:510-568` validates it
on the wire (unknown arm, cross-stage arm, missing/extra wait class, guest stage on a reading-less
arm, errno without a sub-step all refused). `error_code` values and every pre-existing rendered line
are unchanged; the arm rendering is additive (`… stage=native_exec arm=clone_exec_proof wait_class=other
guest_stage=exec errno=1`, `String()` at `helper_v2_protocol.go:487-508`).

## 3. Change-6 grep census

Command (repository root): `grep -rn "<pattern>" daemon/ scripts/ --include=*.go --include=*.sh`.

| Pattern | Sites | Disposition |
| --- | --- | --- |
| `release_outcome_unknown` | `helper_v2_server.go:1195` (writer) | converted: the response now carries the bounded detail beside the unchanged code |
| | `linux_owner_helper_v2_linux_test.go:702,708` (evidence pin), `:846,849,853,854,889,904` (new detail pins) | tests; the code-only rendering assertion pins byte-identical backward compatibility |
| `recover existing owner witness RunJournal` | `linux_owner_witness_linux.go:538` | converted: `refusal=<guard> reason=<term> recovery_error_class=<class>: <recoveryErr chain>`; the document names the same codes |
| `validateFinalSummary` refusals | `:741-746` (`unresolved` case) | converted: names every tripped predicate |
| | `:739,:744` (release tri-state / nativeReleased) | exempt: single named property each, already in the message |
| | `:752,:755` (containment / helper-drop) | exempt: single named property each |
| | `:755` → `validateStageReplayResponseLostSummary` | exempt: delegated sub-validator whose own messages name the failed exact boundary |
| | `:763,:770,:774,:778` (`aborted_no_target`, `no_native_effects`, `released`, unknown state) | exempt: each message names its property group; outside this goal's clause |
| | `:782` → `validateStageReplayPhysicalStopSummary` | exempt: delegated sub-validator, messages name the missing physical proof |
| | `:786,:789` (WSL2 denial) | exempt: names the exact observation class |
| | `:791,:794` (peer denial, Pi resource.sync) | exempt: single named property each |
| | `:797,:800,:803` (OpenCode health) | exempt: names the exact boundary |
| | `:816,:823,:826` (Codex, detached child, helper loss) | exempt: single named property each |

No site was rewritten to hide a failure; every exempt message already names the property it rejects.

Reader/validator census for the additive helper-response field (ruling Q-B(a): "update every reader
and validator of the witness response"); command
`grep -rn "linuxOwnerHelperV2Response\b" daemon/ --include=*.go`:

| Reader / validator | Disposition |
| --- | --- |
| `readLinuxOwnerHelperV2Response` (`helper_v2_protocol.go:557-593`) | updated: runs `ReleaseErrorDetail.Validate()` after decode, so an unknown sub-step/reason or a mixed shape is refused on the wire |
| `linuxOwnerHelperV2ResponseError` (`:598-609`) | updated: builds the typed unresolved error with the detail for every `release`/`unresolved` response |
| `linuxOwnerHelperV2Response.matches` (`:128-131`) | unchanged: identity-only (version/RunKey/fence/launch token/helper); the additive field cannot satisfy or break it |
| literal response constructors (`helper_v2_server.go:935,:960`; `helper_v2_stop.go:288-323`) | unchanged: no detail (nil, omitted) — only the unresolved release writer sets it |
| test literals (`linux_owner_helper_v2_linux_test.go` × 4) | unchanged |
| `ErrLinuxOwnerHelperV2ReleaseUnresolved` consumers | tests only (`containment_errors_test.go:252`, `linux_owner_helper_v2_linux_test.go:701`); no production caller keys off the type, so the extra typed case (evidence-less unresolved responses) changes no decision |
| App-side witness summary readers (`linuxV2OwnerWitnessResponse` in `daemon/e2e/**`, the App's `writeLinuxV2NativeWitnessSummary`) | different protocol, untouched; the helper field never crosses it |

Stage-3 addition to the same census (ruling: "update every reader and validator of the witness
response, and grep-census them"): the arm/wait/guest fields are validated on the read path by
`readLinuxOwnerHelperV2Response` → `ReleaseErrorDetail.Validate()`
(`helper_v2_protocol.go:510-568`), which is the *only* validator of the release detail (the helper's
own write path re-validates through the same method via `writeResponseWithDetail`, and the probe file
exercises both directions). `LookupReleaseArmShape` has exactly one non-test consumer
(`helper_v2_protocol.go:540`), `ReleaseWaitErrorClass` and `ReleaseGuestStageName` exactly one each
(`NewReleaseSubStepError`, `gate_lifecycle_linux.go:786,:791`), and `NewReleaseSubStepError` 21 call
sites — 16 in `gate_lifecycle_linux.go`, 5 in `helper_v2_server.go`, all owned; command
`grep -rn "NewReleaseSubStepError(\|LookupReleaseArmShape(\|ReleaseWaitErrorClass(\|ReleaseGuestStageName(" daemon/ --include=*.go`.

## 4. Local gates

WSL Ubuntu-24.04 `-u accept`, working directory `daemon/`, Go 1.27.0 (2026-11-25):

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no file listed |
| linux build | `GOOS=linux go build ./...` | 0 | clean |
| linux vet | `GOOS=linux go vet ./...` and `GOOS=linux go vet ./e2e/` (the e2e package compiles) | 0 | clean |
| focused suites (non-race) | `go test -count=1 -timeout 900s ./internal/platform/ ./internal/app/ ./internal/execution/` | 0 | `ok platform 36.412s`, `ok app 146.658s`, `ok execution 1.379s` |
| focused suites (race, contract gate) | `go test -race -count=1 ./internal/platform/ ./internal/app/ ./internal/execution/` | 0 | stage-1/2 bytes: `ok platform 77.142s`, `ok execution 22.433s`, `ok app 808.717s` under `-timeout 1800s`; the app package needs ~9–14 min in this WSL environment, so the app row is run with a raised timeout (the hosted `daemon` job runs the same suite under its own 600s cap) |
| host vet | `go vet ./...` (Windows host, Go 1.27.4) | 0 | clean |
| scripts | `bash -n` / `shellcheck --severity=warning` (WSL) | 0 / 0 | no script byte changed in stage 1 (`git diff --stat -- scripts/` empty), so the delta is 0 by identity; base-state measurements: `linux-v2-native-acceptance.sh` findings 64 lines (pre-existing), `linux-v2-native-tools.sh` clean |

Stage-3 re-run of the whole gate on the final bytes (after the arm/wait/guest extension, WSL
Ubuntu-24.04 `-u accept`, from `daemon/`), all exit 0:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no file listed |
| linux vet + e2e compile | `GOOS=linux go vet ./...` and `GOOS=linux go vet ./e2e/` | 0 / 0 | clean |
| focused suites (non-race) | `go test -count=1 -timeout 900s ./internal/platform/ ./internal/execution/ ./internal/app/` | 0 | `ok platform 38.575s`, `ok execution 1.625s`, `ok app 147.744s` |
| focused suites (race, contract gate) | `go test -race -count=1 -timeout 1800s ./internal/platform/ ./internal/execution/` then `go test -race -count=1 -timeout 1800s ./internal/app/` | 0 / 0 | `ok platform 67.117s`, `ok execution 22.862s`; `ok app 576.900s` (under the command's own 600s default, so the gate as written passes without a raised cap) |
| host vet | `go vet ./...` (Windows host, Go 1.27.4) | 0 | clean |
| scripts | `git diff --stat -- scripts/` (WSL) | — | empty: no script byte changed by this goal, so `bash -n`/`shellcheck` delta is 0 by identity |

## 5. Probes (lesson 74)

Every new guard has a table test with a positive and a negative case; run in WSL from `daemon/`:

| Guard | Command | Exit | Result |
| --- | --- | --- | --- |
| release-error → sub-step classifier (12 rows incl. errno, reasons, join/wrap survival) | `go test -count=1 -run 'TestLinuxOwnerHelperV2ReleaseErrorDetail' ./internal/platform/` | 0 | ok |
| closed-vocabulary validator (7 rows) | same file, `TestLinuxOwnerHelperV2ReleaseErrorDetailValidatesClosedVocabulary` | 0 | ok |
| free-form redaction (synthetic path/PID/token string) | `TestLinuxOwnerHelperV2ReleaseErrorDetailRedactsFreeFormText` | 0 | ok |
| bounded rendering + sentinel chain (5 rows) | `TestLinuxOwnerHelperV2ReleaseUnresolvedErrorCarriesTheBoundedDetail` | 0 | ok |
| wire carry + wire refusal of a free-form detail | `TestLinuxOwnerHelperV2ResponseErrorCarriesTheReleaseDetail`, `TestLinuxOwnerHelperV2ResponseRejectsAFreeFormReleaseDetail` | 0 | ok |
| refusal-guard formatter (8 rows + rendering pin) | `go test -count=1 -run 'TestLinuxV2NativeWitnessRecoveryRefusalNamesTheGuard' ./internal/app/` | 0 | ok |
| recovery error class (10 rows, one per arm) | `TestLinuxV2NativeWitnessRecoveryErrorClassClosesTheVocabulary` | 0 | ok |
| refusal-document authority shape (9 authority rows + missing code + free-form reason) | `TestLinuxV2NativeWitnessRefusalDocumentIsIdentityOnly` | 0 | ok |
| summary diagnostic predicate (11 rows + ordered multi-trip) | `TestLinuxV2NativeWitnessUnresolvedSummaryTrippedNamesPredicates` | 0 | ok |
| validator error names the tripped predicate | `TestLinuxV2NativeWitnessUnresolvedSummaryErrorNamesTheTrippedPredicate` | 0 | ok |

Stage-3 rows (all `go test -count=1 -run '<name>' ./internal/platform/` in WSL `-u accept` from
`daemon/`, exit 0 each; re-run together as `-run 'TestLinuxOwnerHelperV2Release'`):

| Guard | Probe | Rows |
| --- | --- | --- |
| every closed arm renders its literal code, is bound to its sub-step and carries exactly the shape's wait class | `TestLinuxOwnerHelperV2ReleaseErrorDetailNamesEveryArm` | 20 positive rows + the alias/duplicate negative |
| closed wait class (nil, `context.DeadlineExceeded`, `os.ErrDeadlineExceeded`, wrapped deadline, `io.EOF`, wrapped `io.EOF`, `io.ErrUnexpectedEOF`, `ECONNRESET`, `ENOTCONN`, `EPIPE`, `context.Canceled`, the raw channel-closed text) | `TestLinuxOwnerHelperV2ReleaseWaitErrorClassClosesTheVocabulary` | 12 |
| closed guest stage (`request`…`exec`) with the guest errno + negatives (unknown enum, an arm that cannot read the channel, the errno arm keeping its own errno) | `TestLinuxOwnerHelperV2ReleaseGuestFailureDetailClosesTheVocabulary` | 10 |
| the full detail survives the helper wire and re-validates | `TestLinuxOwnerHelperV2ReleaseDetailSurvivesTheWire` | 1 |
| closed-vocabulary validator: 5 accepted shapes and 13 refused shapes (free-form/cross-stage arm, arm without a sub-step, missing wait class, wait class on a non-waiting arm, free-form wait class, guest stage on a non-reading arm, free-form guest stage, errno without a sub-step, …) | `TestLinuxOwnerHelperV2ReleaseErrorDetailValidatesClosedVocabulary` | 18 |
| the wire refuses a free-form sub-step, a free-form arm and a cross-stage arm | `TestLinuxOwnerHelperV2ResponseRejectsAFreeFormReleaseDetail` | 3 |
| the classifier still names the arm on every classified row (10 rows) and never labels a checkout failure `native_exec` | `TestLinuxOwnerHelperV2ReleaseErrorDetailClassifiesSubSteps` | 16 |

The characterization baseline for each changed check is the base tree's own behaviour, captured before
any edit: the base six-set run (§7.1) holds the release line without a sub-step
(`… release outcome is unresolved (release_outcome_unknown)`), the refusal line without a guard
(`recover existing owner witness RunJournal without launch`) and the summary refusal without predicate
names (`stage replay unresolved outcome lacks retained journal/owner uncertainty`). The pre-existing
pin of the untouched shape is `TestLinuxV2NativeWitnessStageReplayRequiresExactUnresolvedCategory`.

## 6. Mutants (M1-M7)

Each mutant is applied inside a detached worktree at `48ef0982` carrying the final bytes (outside the
goal worktree, so the delivered files are never mutated) and restored byte-identical (`cp` + `cmp`,
digest re-verified after every row). The restored digest is the file's delivered digest (§7.4). All
seven rows below were re-run on the final bytes after the stage-3 extension; the `daemon` CI job
(`daemon/`, `go test -race -count=1 -timeout 600s -json ./...` with the stdio-only caller boundary) is
the job that must redden; the local focused run is the pinned red.

| id | mutation | expected red | observed red (verbatim) | restored digest |
| --- | --- | --- | --- | --- |
| M1 | `linuxOwnerHelperV2ReleaseErrorDetailFor` drops the `errors.As` classification (no sub-step, arm or errno) | classifier table test, positive rows | `linux_owner_helper_v2_linux_test.go:816: release detail = {Stage:none Arm: WaitClass: GuestStage: Errno:0 Reason:}, want stage="gate_authorize" arm="missing_gate_identity" wait_class="" guest_stage="" errno=0 reason=""` (all 10 classified rows) | `a9e255e0…` (`helper_v2_protocol.go`) |
| M2 | classifier mislabels a `clone_checkout` failure as `native_exec` | classifier table test, negative case | `:816: release detail = {Stage:native_exec Arm:checkout_receipt WaitClass:other GuestStage:exec Errno:1 Reason:}, want stage="clone_checkout" …`; `:870: arm "checkout_receipt" classified as stage="native_exec" arm="checkout_receipt"` | `a9e255e0…` |
| M3 | refusal formatter reverted to the single generic string (guard, reason and class dropped) | refusal-probe test, every refusing row | `linux_owner_witness_linux_test.go:668: refusal = refusal=, want guard="journal_without_unresolved_authority" reason="release_proof_present"` | `91e83974…` (`linux_owner_witness_linux.go`) |
| M4 | accept an unversioned/inexact stop receipt in the `unresolved` branch (the stage-2 predicate dropped) | stale-leaf validator negative cases | `:904: tripped predicates = [], want [stop.receipt_version]` ×3 rows (`versioned_receipt_without_physical_proof`, `unversioned_stop_receipt`, `versioned_receipt_without_its_physical_flags`); `:921: unresolved summary refusal = <nil>, want the named tripped predicate` | `91e83974…` |
| M5 | writer emits `Outcome` in the refusal document | refusal-authority-shape test | `:733: write owner witness refusal document: owner witness refusal document exposed a non-identity field "outcome"` | `91e83974…` |
| M6 | bounded arm replaced by free-form `err.Error()` | closed-vocabulary test + the record host-path gate | `:987: release detail rendered "stage=native_exec arm=/home/some-profile-user/secret\npid=4242 exec --lease-token=abcd1234 errno=1", want the closed sub-step, arm and errno only`; `:938: … unknown arm "receive same-PID native exec proof: Linux B clone bootstrap failed at stage 1 (errno=13)"` | `a9e255e0…` |
| M7 | two arm codes swapped (`ReleaseArmNativeNoAttempt` ↔ `ReleaseArmNativeImage` values, same sub-step, so the shape table stays valid) | classifier table test, literal wire values | `:876: arm "image_mismatch" rendered "stage=native_exec arm=image_mismatch", want the literal "stage=native_exec arm=no_exec_attempt"` (both swapped rows; `:816` also red) | `cd803b8a…` (`gate_lifecycle_linux.go`) |

## 7. Scratch runs

Throwaway workflow `.github/workflows/scratch-rf-acc-release-recovery-diagnostics.yml` (sha256
`2a57866548a3fcb3e3d1919a8ab0d7adc1bde08787f22e50986264d3d25c9fde`; copy of
`linux-native-v2-acceptance.yml` without the `environment:` block, with the ratified dummy pins and
the scratch-only `gh` shim step, six case sets) plus `scripts/ci/scratch-gh-shim` (sha256
`507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`, byte-identical to the ratified
burndown copy). Both live only on `scratch/rf-acc-release-recovery-diagnostics` and are deleted with
it. No run was voided; no run repeated a class.

| # | purpose | head | case sets | acceptance job | run | result |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | base re-confirmation (`48ef0982` + the two scratch files) | `a4293de4` | six sets | `139003046926` | `45554251351` | all six exit 1 — the parent reds reproduce (§7.1) |
| 2 | stage-1 diagnostics | `17ac8690` | six sets | `139008016043` | `45555828634` | all six exit 1, every class now named — §7.2 |
| 3 | final bytes (stage 2: one predicate) | `116114d3` | six sets | `139013873581` | `45557621798` | `stale_leaf` **exit 0**; five sets keep the named causes — §7.3 |

### 7.1 Base run `45554251351` (before reds, verbatim)

`Case set <name> exited 1` for all six; `Failed case sets: stale_leaf migration
stale_journal_run_key stale_journal_run_key_legacy stale_journal_fence stale_journal_subject`.

| Set | First failing check (verbatim) | App stderr (verbatim) |
| --- | --- | --- |
| `stale_leaf` | `production_linux_v2_native_witness_test.go:627: read stale-leaf recovery response: decode redacted production owner-witness response` | `-linux-v2-native-owner-witness: stage replay unresolved outcome lacks retained journal/owner uncertainty` |
| `migration` | `production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness command did not reach its requested durable stage` (exit status 1 logged just above) | `-linux-v2-native-owner-witness: start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown)` |
| `stale_journal_run_key` | `:531: pin exact live A anchor/helper PIDFD before fresh request: process creation identity changed across pidfd open` (fresh-request subtest) and `:531: read stage-replay recovery response: decode redacted production owner-witness response` | `-linux-v2-native-owner-witness: recover existing owner witness RunJournal without launch` |
| `stale_journal_run_key_legacy` | `production_linux_v2_native_witness_test.go:269: read stage-replay recovery response: decode redacted production owner-witness response` | same refusal line |
| `stale_journal_fence` | same `:269` line | same refusal line |
| `stale_journal_subject` | same `:269` line | same refusal line |

Red source not named in the contract: `stale_journal_run_key`'s fresh-request precondition reports
`process creation identity changed across pidfd open` on this base tree — a third variant of the same
F4-class precondition (run 11: `stage owner does not have exact A credentials`; run 13: `exact native
process pidfd reports an exited target`). The class stays F4's; no owned file can satisfy that pidfd
precondition.

Artifact `linux-v2-native-evidence-45554251351-1` (id `14995662195`, 10501 bytes, digest
`sha256:4d965900f35abe266f2264663b20564296ca565c3e4cca88c32045eb4946a474`):

| Set | `manifest.json` sha256 | Witness document sha256 |
| --- | --- | --- |
| `stale_leaf` | `b1863bf6229de834b46f1cc06cee709a090ab8439ec72ef0df17a5e704620825` | `df70b8400e0fe8f4fb4f9696d105f6cde6f60feb3815381769466857e18daf44` |
| `migration` | `ba039c686d1a730241d52b800dd965c3211092f5badde0a22c3dab40742962ed` | `7010fd2c216b5ad8b6f52f667f84132cf7cdca008f046e0787337b73b2ce15bc` |
| `stale_journal_run_key` | `32ea8d52df28b875247a87cab977e4c96f97aa862ce3b654f953cc0715bad119` | `b832fb428c200d9d794a034e2f4c26b355f7e4eb3d8123731ea8e9035f94d51f` |
| `stale_journal_run_key_legacy` | `fb9c1cf3b85f9801ad5f00ac70e489b8899542f515948e5a28dd50f5ddab9561` | `1bd20b507f29b525fe1a075349778e6fc83777e0efdf95fe8ec618e263cb14f6` |
| `stale_journal_fence` | `82839df1b738b212e32babd22d25df49f90b07583cca6ae6eaf49dbf5a333aa6` | `98eb60b7028236238bcc873e8a23392986efd9f1bef8bf01f28845c84be223f3` |
| `stale_journal_subject` | `d2c7b8931b562f34887b04a7b77439b1cff396a8854f729f4225ac8499a01067` | `82579a2f213c0a457b304768b3974702df81db348feb39e06d835011c96dca11` |

### 7.2 Stage-1 run `45555828634` (the named causes, verbatim)

Head `17ac8690`, 2026-11-25T05:45:57Z → 05:58:24Z, conclusion failure; jobs `contracts` success
(`139007508635`), `linux-native-v2-acceptance` failure (`139008016043`). All six sets exit 1, but every
class now names its cause; `Failed case sets:` unchanged.

| Set | First failing check (verbatim) | Named cause (App stderr, verbatim) |
| --- | --- | --- |
| `migration` | `production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness command did not reach its requested durable stage` (owner-witness command exit status 1) | `-linux-v2-native-owner-witness: start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=native_exec)` — the sub-step is named; **no errno** is emitted, so the failing arm carries none (the four non-errno `native_exec` sites: `gate_lifecycle_linux.go:803` await-guarded-result, `:823` no-exec-attempt, `:830` same-PID image, `:949` clone native-exec proof). The App then cannot prove the stop: `Linux v2 helper could not prove the exact run leaf stopped: dial unixpacket: connect: connection refused` (twice) and the RunJournal projection stays `{"local_state":"claimed", …, "recovery_required":true}` — the release never succeeded, so `native_release` is never stamped |
| `stale_leaf` | `production_linux_v2_native_witness_test.go:627: read stale-leaf recovery response: decode redacted production owner-witness response` (the summary is still refused, so no response is written) | `-linux-v2-native-owner-witness: stage replay unresolved outcome lacks retained journal/owner uncertainty: tripped stop.receipt_version` — the tripped predicate is named |
| `stale_journal_run_key` | fresh-request subtest `:531: pin exact live A anchor/helper PIDFD before fresh request: stage owner does not have exact A credentials`; parent leg `:531:  production recovery exit = exit status 1, want exit 0` (the refusal document decoded — the old decode failure is cleared) | `-linux-v2-native-owner-witness: recover existing owner witness RunJournal without launch: refusal=journal_without_unresolved_authority reason=release_proof_present recovery_error_class=recovery_failed: Linux clone cleanup requires its exact workspace adapter and complete durable receipts` |
| `stale_journal_run_key_legacy` | `production_linux_v2_native_witness_test.go:269:  production recovery exit = exit status 1, want exit 0` (decode cleared) | same refusal line, same guard/reason/class and same recovery chain |
| `stale_journal_fence` | same `:269` line | same refusal line |
| `stale_journal_subject` | same `:269` line | same refusal line |

So acceptance 1(a) holds with the honest detail (sub-step `native_exec`, no errno because the failing
arm carries none), 1(b) holds (guard `journal_without_unresolved_authority` + journal term
`release_proof_present` + recovery class `recovery_failed` in the refusal document; the structured read
now succeeds — the old `decode …` first failing check is gone), and 1(c) holds (`stop.receipt_version`,
the term stage 2 fixes). Two run-level facts pin the refusal document's *content* as well: the e2e's
reader returned a response with an **empty** `Outcome` (the failure line renders as
` production recovery exit = …`, not `unresolved production recovery exit = …`, which a fabricated
`outcome:"unresolved"` would have produced), and its identity-field checks passed. The document's own
bytes are pinned by the writer probe (§5, `TestLinuxV2NativeWitnessRefusalDocumentIsIdentityOnly`) and
by the authority-shape validator the writer runs before writing.

Artifact `linux-v2-native-evidence-45555828634-1` (id `14995928751`, 10499 bytes, digest
`sha256:130054d6f172302aedc2785c0a15f2404998dc3672f3cb7aabd48eefc8da08e3`):

| Set | `manifest.json` sha256 | Witness document sha256 |
| --- | --- | --- |
| `stale_leaf` | `306ee5cee363f49f86b9ce3b0c9c102ff504795483ba4a0d65ffe6e946be497b` | `350c2b99644380c979828e954a3f55d7eea5bde89389f0271850357b79849824` |
| `migration` | `0c7b4a48a8d2c8b76bc42a7cdf3e49179e36e079804f23d0bd07f6d42b49931b` | `761079d6728b257c4fb8623de99b06ba075ce8a38e587d8f36fe05f0a5691ffb` |
| `stale_journal_run_key` | `2ecc33e668658879115bdd041f7c066f52b4fab945cf5c291b08f52edafc9653` | `e41330934a194069de260f88c45a02e6565f4fda83161024b2798fd0eb8c4700` |
| `stale_journal_run_key_legacy` | `2e7112196153c4b4dc6a591af7936f36ec9d77f4d7e4bba0a2bcc615ab461513` | `05d4a7acde48380c504d441e6203bef018adf65d461b72cd67b25f3003404dc8` |
| `stale_journal_fence` | `bc16495f18c8c32f612a5eb2b1f76269b98cc4adc8a767cd43d6374e498bae3e` | `be058615185d163bccbeaa00df451e0f0b28687423812f96ee2e9e29c37e692f` |
| `stale_journal_subject` | `9e3578531a02ddad45b1ddd119431e4d96c2a3bb6f5571fa5899b593e4e6e8cd` | `a6d79cbaf9c8d30b8d8eefbe955f6a1bc09af2af0957708f62d90858c5e32585` |

### 7.2a Stage 2 (formed from §7.2) — one predicate, no new vocabulary

The observed term is `stop.receipt_version`; the fix reuses the file's existing exact-proof predicate
instead of adding a second convention:

`add(summary.Stop.ReceiptVersion != 0 && !linuxV2NativeWitnessStopSummaryHasPhysicalProof(summary.Stop), "stop.receipt_version")`

That predicate is the same one the response-loss branch already uses for its accepted receipt
(`linux_owner_witness_linux.go:905-911`, pinned by `TestLinuxV2NativeWitnessAllowsReceiptBackedLostClearAck`
and `TestLinuxV2NativeWitnessAllowsExactStopReceiptForLostReceiptAck`), and it is at least as strict as
the e2e's own acceptance at `production_linux_v2_owner_cli_test.go:1238-1245` (version V2, status
`stopped`, proof kind, issuer/reaper, the five physical flags, plus the launcher/gate exit proofs the
e2e leaves to `Ready`-ness). Unversioned or partial receipts stay tripped (M4). The `migration` sub-step
cause is not in the owned set (§9).

### 7.3 Stage-2 six-set run `45557621798` (`stale_leaf` green, the named causes held)

Head `116114d3` (= the goal worktree's stage-2 bytes + the two scratch-only files), 2026-11-25T06:01:51Z →
06:14:18Z, conclusion failure; jobs `contracts` success (`139013382238`), `linux-native-v2-acceptance`
failure (`139013873581`). Per-set rows: `stale_leaf` **exit 0** — the first green row this goal
delivers; the five remaining sets keep the named causes of §7.2 unchanged.

| Set | Exit | First failing check / pass line (verbatim) | Named cause (verbatim) |
| --- | --- | --- | --- |
| `stale_leaf` | 0 | `verified TestProductionLinuxV2StaleLeafIdentityRetainsJournal: run=1 pass=1 skip=0`; `Linux amd64 installed A/B case set stale_leaf passed after verified guest/Control stop and allowlisted evidence export …`; `Case set stale_leaf exited 0` | the recovery writes the expected `unresolved` summary (witness document `owner_witness_outcome=unresolved`, `recovery_state=unresolved`, `journal_unproven=true`, `failure_category=containment_unproven`); every post-decode assertion executed for the first time and passed |
| `migration` | 1 | `production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness command did not reach its requested durable stage` | `… (release_outcome_unknown; stage=native_exec)` — unchanged from §7.2 (the guest-side arm) |
| `stale_journal_run_key` | 1 | `:531:  production recovery exit = exit status 1, want exit 0` | `refusal=journal_without_unresolved_authority reason=release_proof_present recovery_error_class=recovery_failed: Linux clone cleanup requires its exact workspace adapter and complete durable receipts` |
| `stale_journal_run_key_legacy` | 1 | `:269` same exit line | same refusal line |
| `stale_journal_fence` | 1 | `:269` same exit line | same refusal line |
| `stale_journal_subject` | 1 | `:269` same exit line | same refusal line |

`Failed case sets: migration stale_journal_run_key stale_journal_run_key_legacy stale_journal_fence
stale_journal_subject`.

Artifact `linux-v2-native-evidence-45557621798-1` (id `14997077305`, 11671 bytes, digest
`sha256:9fa01e6bd834395a5f3e3c8375f91b36096da52fe21d0aeb1b2063b720dddf81`):

| Set | `manifest.json` sha256 | Witness document sha256 |
| --- | --- | --- |
| `stale_leaf` (status `passed`) | `69ceeba06ff515783c58da40c25441c156a561dfdf1e37415e76794547e663ce` | `35e8eb3d85e34f6512b3c240e57cf12cb674fef79df9bc547ab9ab98287a8e8a` |
| `migration` | `b4913812a0a7f3f1d12e43c74c269b29abc41c38e9d0b219a0f80b59fceab581` | `940d586e904269fc2098474c3b4f3ffe21fa87464d5e3e53da214125b7ee6996` |
| `stale_journal_run_key` | `ffaaf908b288f4e90f4e6ddf046a5af6279a3d742bf0c2a0dfb23f9a2e5fcd95` | `1feb71ecb51ab22b8849f1174926f3580e065c1b32982d04d32b7d0ea08d8849` |
| `stale_journal_run_key_legacy` | `e0612ebac3a2daa63e8c5a2565ec296ba85d95cad3e89f2482fba4a4eeea274d` | `090a4a18f03552454a5955cb53ee7c2088439286ee08418530eb9d886e253cb9` |
| `stale_journal_fence` | `64090e5a1a5159e0a409eafd9921bf8ae3bfb267312ef4c49848c0d401433daf` | `3758b209ac5b3f1ca11d29d08f72ce816a9db3a6ef144eae729ee56d5d43bc28` |
| `stale_journal_subject` | `744fc5bd71dba2c6836846e697ef1451d075fa1c869c8a7106aaa83dc679e7be` | `da30d16812620b8abaf97112ce96aefef8535c5ce2519c7779d5f2009043f911` |

The `stale_leaf` manifest carries `status=passed`, `source_commit=116114d327580fe8793f4bafaeb6cc00b62e1183`,
`missing_allowlisted_files=[]`, `credentials_included=false`, `mode=manual`.

### 7.4 sha256 equality table (run head vs delivered bytes)

Run `45557621798` was head `116114d3` (stage-2 bytes) and run `45564071868` is head `f32d47d9` (the
stage-3 final bytes); both heads are the eight code files plus the two scratch-only files
(`git diff a4293de4 <head>` = 10 paths), and the delivered bytes are the goal worktree's,
byte-for-byte. Final-byte table (the run-`f32d47d9` column is the binding one):

| File | Run head `f32d47d9` / delivered (goal worktree, `cmp` + sha256) | Eq |
| --- | --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `91e83974c871bac0675619fff58afe6a56d8df963d57ee307c202f4140f6c79d` | EQ |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `aef98fd56448157b69a45090d3e15816377c7e4595756428fd127cd47ee37a90` | EQ |
| `daemon/internal/platform/helper_v2_protocol.go` | `a9e255e045c78b2b985085a15c9d85bd722137e06d33568e8371290d741c02f1` | EQ |
| `daemon/internal/platform/helper_v2_server.go` | `a00c6606f9da60b12322d93835f6d3f9d549cbce6b662dd5aefa1b8982c2da6b` | EQ |
| `daemon/internal/platform/helper_v2_stop.go` | `d216fd1d3ed719a78e4ef9ebd83c33a671601e687669ffec3c3639b037e3773e` | EQ |
| `daemon/internal/platform/linux_owner_helper_v2_linux.go` | `b55b1768bc413517b0aab719087498139ea4ffbba9495326273709e512da1831` | EQ |
| `daemon/internal/platform/linux_owner_helper_v2_linux_test.go` | `7dd9b57e2678339823abb3668e0fd919a0d040484c949044d9985eadeede4c19` | EQ |
| `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` | `cd803b8aa004fae929dd0a6e290555a78ea71ccfb983bd89f275694b0b3c6075` | EQ |

The stage-3 scratch workflow bytes: `scripts/ci/scratch-gh-shim`
`507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` (byte-identical to the ratified
burndown copy), `.github/workflows/scratch-rf-acc-release-recovery-diagnostics.yml`
`24883507a3fd5c257ab49f0defec6d61a6b3fa2434dbfeb7b0bfab6837071bea` (a copy of
`linux-native-v2-acceptance.yml` without the `environment:` block, with the ratified dummy pins, the
scratch-only `gh` shim step and `case_sets=(migration)`). The plan/record/TODO bytes are not part of any run's subject (docs only, and
the scratch tree carried the base copies, so no doc row is claimed). The scratch branch was deleted
after the stage-3 run (`git push origin --delete scratch/rf-acc-release-recovery-diagnostics`) and
`git ls-remote origin scratch/rf-acc-release-recovery-diagnostics` prints nothing (exit 0), with
`git ls-remote origin 'refs/heads/scratch/*'` printing nothing too.

### 7.5 Stage-3 run `45564071868` (migration only — the arm is named)

Head `f32d47d9` (the delivered bytes + the two scratch-only files, one case set `migration`),
2026-11-25T06:54:24Z → 06:59:15Z; jobs `contracts` success (`139034466893`),
`linux-native-v2-acceptance` failure (`139035057733`); `Case set migration exited 1` /
`Failed case sets: migration`. The test ran `31.11s` and stopped at the same first check as §7.1-§7.3
(`production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness
command did not reach its requested durable stage`), but the release detail now names the failing arm
verbatim in the App stderr:

```
-linux-v2-native-owner-witness: start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=native_exec arm=await_result wait_class=other)
```

Reading: `arm=await_result` is the non-clone (reuse) await site of `PreparedGate.Release`
(`gate_lifecycle_linux.go:803` at the head) — a fresh-clone release would report
`clone_exec_proof`/`bootstrap_attempt`/`checkout_receipt`, so the migration release replays
already-established clone authority and fails while awaiting the gate's exec-status result;
`wait_class=other` means that read failed on its own terms — neither the lease/deadline wait nor a peer
EOF/`ENOTCONN`/`EPIPE`/`ECONNRESET`, which render `deadline`/`peer_closed`. The remaining arms inside
that read are the gate C code's exec-status producer (`daemon/native/linux-gate/c-gate/gate.c`
`exec_as_b`, which sends `EXEC_FAILURE(errno)` on every internal failure and the `FRAME_EXEC_ATTEMPT`
at `:1276` before the exec) and the reused guest, none of them owned by this goal — so per contract
Change 7 and the conductor ruling this clause stops with the exact arm in `docs/TODO.md` (§9).

Artifact `linux-v2-native-evidence-45564071868-1` (id `14998998279`, 1570 bytes, digest
`sha256:a129c0394ce62cfb57537746483d613fc3c90d83a6210fb545931384173c8644`):

| Set | `manifest.json` sha256 | Witness document sha256 |
| --- | --- | --- |
| `migration` (status `failed`) | `39fbbed98249aca75df541c8f293fe139f7d09fce5ecbdbe4fa2b0c85c7bb957` | `4a4bb45c23e02086502e484d5e93ae9f728c35265c113b4a56cbac9f3f54d81c` |

The manifest carries `status=failed`, `source_commit=f32d47d96672be019a0c7d2688a8821851993c42`,
`missing_allowlisted_files=[]`, `credentials_included=false`, `mode=manual`; the witness document
carries `owner_witness_outcome=null`, `failure_category=null`, `installed_owner_verified=true`
(`fail_events=2`).

## 8. Lesson-83 assertion trace (what the changed checks expose)

- `stale_leaf`: before this goal the leg never got past `read stale-leaf recovery response` (runs
  `45484692731`, `45489336388`, `45554251351`). On the final bytes every later assertion executed for
  the first time — `requireLinuxV2RecoveryExitStatus`,
  `linuxV2ValidateStageReplayResponseState("stop_receipt_durable", …, "unresolved")` (including its
  Stop-receipt arm), `linuxV2ValidateStageReplayJournal` and the two leaf-identity checks — and passed
  (run `45557621798`, `run=1 pass=1 skip=0`).
- The four denial legs: the refusal document clears the decode check; the next assertions
  (`requireLinuxV2RecoveryExitStatus` with an empty outcome, then `ValidateStageReplayResponse`, then
  the variant assertions and `linuxV2ValidateNegativeBarrier`) execute on the final bytes and stop at
  the exit-status line — a Q-D/F4 row, never recorded green here.
- `migration`: the `native_release` wait is still the first failing check; the migration/denial legs
  after it remain unexecuted in every run of this goal and of the parent.

## 9. `DECISION_NEEDED`: the `migration` release arm is not in the owned set

Stage 2 could only say `stage=native_exec` with **no errno** (runs `45555828634`, `45557621798`) —
four candidate sites (`gate_lifecycle_linux.go:803,:823,:830,:949`), not the errno-reporting `:816`
that the tracked `launcher_integration_linux_test.go:439` stop (`native exec failed after RELEASE:
errno=1`) names. The stage-3 rerun on the final bytes (run `45564071868`, §7.5) names it:
`stage=native_exec arm=await_result wait_class=other` — the **non-clone (reuse) exec-status await**,
the helper waiting for the gate's `FRAME_EXEC_ATTEMPT`/`EXEC_FAILURE` result, failing with a class that
is neither a deadline nor a peer EOF. The remaining producers of that state are the gate C code and
the reused guest (`daemon/native/linux-gate/c-gate/**`, outside the owned set; the contract's own
run-path flag anticipated this), so per contract Change 7 and the conductor ruling this clause stops
with `DECISION_NEEDED` and the exact arm in `docs/TODO.md` (residual pointer → `rf-acc-guest-native-exec`).
The guest-side staging of the executed binary is already correct and verified
(`/usr/local/lib/symmetry-pi/pi` `regular file root:root 755 1`, `test -x` as A; run `45555828634`
log, "guest view of the staged native tools"), and after the aborted release the App cannot even
measure a stop — `Linux v2 helper could not prove the exact run leaf stopped: dial unixpacket:
connect: connection refused` — which is why `native_release` is never stamped and the wait times out.
Stage 1, the `stale_leaf` stage-2 fix and the stage-3 diagnostics still merge.

The four `stale_journal_*` sets' refusal is the owner-loss consult's candidate (B): the verbatim App
stderr of run `45557621798` is
`recover existing owner witness RunJournal without launch: refusal=journal_without_unresolved_authority
reason=release_proof_present recovery_error_class=recovery_failed: Linux clone cleanup requires its
exact workspace adapter and complete durable receipts` — a post-tombstone failure in the forced clone
cleanup path, not in `anchor.RetireHelper`. It is handed to `rf-acc-owner-loss-semantics` (Q-D(a)); no
green row is claimed here.

## 10. Residual risks and limits

- `migration`'s arm is named but its cause stays open (§9): `stage=native_exec arm=await_result
  wait_class=other` — the non-clone exec-status await, produced from the gate C code / reused guest.
  The `native_release`, `turn`, `stop_receipt_durable`, `leaf_removed` and `journal_cleared` boundaries
  of every pi leg strictly after `native_release` remain unreachable until it is fixed.
- Wait-class granularity is the ruling's three-class vocabulary, so every non-deadline, non-EOF
  failure inside `await_result` renders `other` — including the conflated
  `exec-status channel closed or carried ancillary data before a frame` (`receiveStatusPacket`,
  `wire_linux.go:685`, `n == 0` in practice: the gate closed the FD before a frame) and the
  frame-level rejections of the same read. Splitting those needs a sentinel in the unowned
  `wire_linux.go`; they are the follow-up goal's first discriminating probes, and the class is honest
  as delivered (`other` = neither `deadline` nor `peer_closed`, both of which have their own pins).
- The arm literals, the arm/sub-step binding and the wait/guest shape are pinned by owned probe rows
  (§5) and by mutant M7 (§6); the *wrap-site* assignment itself (which site constructs which arm) has
  no owned unit test, because every test file in `daemon/native/linux-gate/protocol/` is outside the
  owned set. Its behavioral pin is the stage-3 run: the reached site renders `arm=await_result`
  verbatim, and a mislabel there would have printed a sibling code.
- The four `stale_journal_*` green rows stay `rf-acc-owner-loss-semantics`'s (Q-D); the refusal
  document now decodes, so that goal starts from a named guard, not from a decode failure. The
  `stale_journal_run_key` fresh-request precondition shows three variants of its pidfd message across
  runs — `process creation identity changed across pidfd open` (`45554251351`), `stage owner does not
  have exact A credentials` (`45555828634`) and `A anchor/helper was not live before fresh request:
  exact native process pidfd reports an exited target` (`45557621798`) — all F4-class, and none
  satisfiable by an owned file.
- The credentialed `linux-native-v2-acceptance` dispatch (owner-approved Environment, after merge) is
  conductor-owned; `stale_leaf`'s real pi turn is its proof of record.
