# rf-acc-release-recovery-diagnostics — evidence record

Goal: `rf-acc-release-recovery-diagnostics` (contract `goal-rf-acc-release-recovery-diagnostics.md`,
GDD state root). Branch `refactor/rf-acc-release-recovery-diagnostics`, base revision `48ef0982`
(#284 `rf-acc-witness-unresolved-exit`; #281 supplies the case sets). Rulings Q-A(a) (one staged
goal), Q-B(a) (typed sub-step errors + one additive bounded detail; `error_code` unchanged),
Q-C(a) (minimal redacted refusal document), Q-D(a) (the four `stale_journal_*` rows hand off to
`rf-acc-owner-loss-semantics`) and the 2026-11-25 ruling on the implement `DECISION_NEEDED` (stage 1,
the `stale_leaf` stage-2 fix and the refusal document are the accepted base; the bounded detail gains
a closed arm/wait-class/guest-stage sub-code and one `migration`-only rerun on the final bytes —
§2.8, §6, §7.5, §9), plus the 2026-11-25 fix-stage ruling on the review `MUST_FIX` (F1-F10: the
release wrap can no longer mask a labelled sub-step, the native exec arm splits, the code-driven
sentinels stay in the chain, the detail is validated before the write, the checkout rejection carries
the terminal detail, and the record's census re-anchors to the delivered bytes — §2.9, §5, §6, §10).
Plan: `docs/plans/2026-11-25-rf-acc-release-recovery-diagnostics-plan.md`.

Line numbers below are at the delivered bytes unless a section says otherwise; the run tables name
their own head revisions, and every number carrying a run id belongs to that run's head.

## 1. Clause census at the delivered bytes (re-located by content)

| Contract (origin/main `33ad88a9`) | Delivered bytes | Disposition |
| --- | --- | --- |
| helper `:1184-1203` | `helper_v2_server.go:1196-1219` | converted: the unresolved block computes the bounded detail and writes it via `writeResponseWithDetail` |
| helper `:1126-1139` | `helper_v2_server.go:1129-1140` | converted: `gate_authorize` typed wraps around the missing-identity error and `AuthorizeGateRelease` |
| helper `:1169-1173` | `helper_v2_server.go:1178-1191` | converted: `register` typed wraps around `RegisterProcess` and `ensureGateWait` |
| helper `:1073-1099` | `helper_v2_server.go:1073-1099` | untouched: pre-attempt rejections, never the unresolved path |
| `linux_owner_helper_v2_linux.go:301-317` | `:302-326` | extended: `Detail` field + `(<code>; <detail>)` rendering; code-only rendering byte-identical |
| `helper_v2_protocol.go:516-540` | `:647-668` (writer) and `:640` (wire read) | converted: typed for every `release`/`unresolved` response; detail validated on the wire |
| `linux_owner_v2_linux.go:1066` | unchanged | `%w` carries the new text |
| `runner.go:864` | unchanged | `%w` carries the new text |
| app `:3279` | `:3510` | unchanged |
| app `:531-534` | `:532-539` | converted: guard + reason + recovery class named, `recoveryErr` chain joined, refusal document written for the stage-replay arm |
| app `:2801-2811` | `:2916-2926` | predicate unchanged; companion reason function (`:2989`) + refusal builder added |
| app `:737-742` | `:715-732` (`linuxV2NativeWitnessUnresolvedSummaryTripped`), validated at `:765-767` | converted: names each tripped predicate |
| app `:3211` | `:3410,:3424,:3442` through `stage` (`:2158`), hold at `:2214` | the `native_release` hold (unchanged: `migration` never reaches it — §9) |

## 2. Stage-1 changes (behavior-preserving diagnostics)

1. **Release sub-step type** `linuxgate.ReleaseSubStepError` (`gate_lifecycle_linux.go:700-710`) with
   the closed vocabulary `gate_authorize | clone_bootstrap | clone_checkout | native_exec |
   register`; `Error()` returns the wrapped message verbatim, so every current line
   (`native exec failed after RELEASE: errno=1` at `:993`) stays byte-identical. Wrap sites
   (delivered bytes): `:872` (clone bootstrap verify), `:898` (pre-RELEASE gate identity),
   `:979,:992,:999,:1006` (native exec result; errno on `:992`; the `:1006` site picks
   `native_process`/`exec_readback` — §2.8), `:1056,:1060,:1072,:1076` (clone bootstrap),
   `:1065,:1069,:1087,:1098,:1120` (clone checkout), `:1125` (clone native exec proof); platform:
   `helper_v2_server.go:1122` (checkout receipt), `:1131,:1137` (gate authorize),
   `:1181,:1185` (register).
2. **Detail wire type** `LinuxOwnerHelperV2ReleaseErrorDetail`
   (`helper_v2_protocol.go:469-565`): `stage` always set (`none` for an untyped error), `errno` only
   when the sub-step carries one, `reason` only when `err == nil`
   (`owner_lost | lease_expired | stop_requested`). `Validate()` enforces both closed vocabularies
   and rejects a reason mixed with a sub-step; `readLinuxOwnerHelperV2Response` runs it after decode
   (`:640`), so a free-form field from the wire is refused, never carried.
3. **Additive response field** `release_error_detail` (`:126`), written only by
   `writeResponseWithDetail` (`helper_v2_stop.go:302-326`), which validates the detail before
   encoding (`:303`) and returns the validation error instead of writing an invalid frame;
   `writeResponse` (`:296`) delegates with nil, so the 60 call sites, all in the two owned files,
   keep their signature.
4. **Carry-through**: `LinuxOwnerHelperV2ReleaseUnresolvedError.Detail`
   (`linux_owner_helper_v2_linux.go:302-326`); `linuxOwnerHelperV2ResponseError` builds the typed
   error for every `release`/`unresolved` response (`helper_v2_protocol.go:647-668`) and joins the
   code-driven sentinel (`ErrLinuxOwnerHelperV2OwnerLost`, the generic lease-expiry rejection) beside
   it. New stderr tail: `… release outcome is unresolved (release_outcome_unknown; stage=native_exec errno=1)`.
5. **Refusal naming** (`linux_owner_witness_linux.go:2916-3042`): guards
   `case_not_stage_replay` / `journal_without_unresolved_authority`; authority reasons
   `release_proof_present | clone_abort_proof_present | clone_promotion_receipt_present |
   no_unresolved_term`; recovery classes `cancelled | timeout | owner_lost | helper_recovery_only |
   outcome_unknown | gate_never_produced | cgroup_unresolved | recovery_failed`.
6. **Refusal document** (`:2069-2143`): identity fields + `recovery_refusal` only; the writer
   validates the encoded bytes against the closed key set before writing, and the validator rejects
   any authority key by name. The e2e decoder ignores the unknown key, so no e2e byte changes.
7. **Summary diagnostics** (`:713-732`): the ten predicates are named, not silently ORed.

### 2.8 Arm, wait class and guest stage (stage 3, conductor ruling 2026-11-25)

`stage=native_exec` with no errno named four sites, so it could not route the guest-side fix. The
bounded detail now carries a closed **arm** code for every wrap site of every multi-arm sub-step the
migration path can reach (`daemon/native/linux-gate/protocol/gate_lifecycle_linux.go:634-689`
vocabulary, `:700-710` type, `:711-737` shape table, `:739-753` wait-class and `:755-773`
guest-stage classifiers, `:798-819` constructor), plus the closed **wait class** of the arm's
exec-status read and the closed **guest stage** (+ the guest's errno) of a decoded guest
clone-bootstrap failure. Wrap sites at the delivered bytes, all owned:

| Sub-step | Arms (closed) | Wrap sites (delivered bytes) |
| --- | --- | --- |
| `gate_authorize` | `missing_gate_identity`, `anchor_authorize`, `gate_identity` | `helper_v2_server.go:1131`, `:1137`; `gate_lifecycle_linux.go:898` |
| `clone_bootstrap` | `installed_bootstrap`, `bootstrap_attempt`*, `bootstrap_image`, `bootstrap_process`, `leaf_membership` | `gate_lifecycle_linux.go:872`, `:1056`*, `:1060`, `:1072`, `:1076` |
| `clone_checkout` | `checkout_receipt`*, `checkout_receipt_mismatch`, `checkout_callback`, `checkout_ack_encode`, `checkout_ack_send` | `gate_lifecycle_linux.go:1065`*, `:1069`, `:1087`, `:1098`, `:1120`; `helper_v2_server.go:1122` |
| `native_exec` | `await_result`*, `exec_errno`, `no_exec_attempt`, `native_process`, `exec_readback`, `clone_exec_proof`* | `gate_lifecycle_linux.go:979`*, `:992`, `:999`, `:1006`, `:1125`* |
| `register` | `native_register`, `gate_waiter` | `helper_v2_server.go:1181`, `:1185` |

`*` = reads the exec-status channel: its detail also carries the wait class (`deadline | peer_closed |
other`, computed from that arm's own error by `ReleaseWaitErrorClass`, so a joined abort error cannot
reclassify it) and, for the three clone-status arms, `guest_stage`
(`request | seed | target | git | checkout | native_image | exec`) with the guest-reported errno from
`CloneBootstrapFailureError` (`wire.go:204`, sent by `daemon/native/linux-gate/c-gate/gate.c:1140-1204`
at `CLONE_FAILURE_EXEC_STAGE`; the frame rejects `errno == 0` and the C gate falls back to `EPERM`, so
the guest errno is the only errno the clone arms can carry). `LookupReleaseArmShape` is the single closed table
binding each arm to its sub-step and to those two flags; `helper_v2_protocol.go:508-565` validates it
on the wire (unknown arm, cross-stage arm, missing/extra wait class, guest stage on a reading-less
arm, errno without a sub-step all refused). `error_code` values and every pre-existing rendered line
are unchanged; the arm rendering is additive (`… stage=native_exec arm=clone_exec_proof wait_class=other
guest_stage=exec errno=1`, `String()` at `helper_v2_protocol.go:485-506`). The `native_exec` split is
stage-3's F8 fix (§2.9): `native_process` names the process-side verify failures (pidfd, exit,
identity, credentials, namespace, boot) and `exec_readback` the image readbacks (attempt identity,
pinned-file readback, `/proc` stat/readback), chosen by `NativeExecVerifyArm`
(`gate_lifecycle_linux.go:788-794`) from the process-side marker the verify (`:1337,:1340,:1347,:1351,:1356,:1369,:1374`)
attaches; the clone sibling keeps its single `bootstrap_process` arm.

### 2.9 Fix-stage changes (review `MUST_FIX` F1-F10, conductor ruling 2026-11-25)

`NewReleaseSubStepError` (`gate_lifecycle_linux.go:798-819`) now returns an already-labelled
`*ReleaseSubStepError` unchanged (`:799-802`), so the `await_result` wrap at `:979` can no longer mask
a labelled `awaitCloneRelease` arm and the `checkout_callback` wrap at `:1087` can no longer mask the
helper's `checkout_receipt_mismatch`; an unlabelled wait error still renders `await_result`. The
`native_exec` readback/process split is §2.8. `linuxOwnerHelperV2ResponseError`
(`helper_v2_protocol.go:647-668`) joins the code-driven sentinel beside the typed unresolved error
(`:661,:663`), `writeResponseWithDetail` validates its detail (`helper_v2_stop.go:303`), and the
checkout-rejection branch joins the terminal response error (`linux_owner_helper_v2_linux.go:659`).
The summary predicate's version-0 exemption is pinned by the "absent stop receipt version stays
untripped" row and the "unknown stop receipt version" row (F3); the record's census is re-anchored to
the delivered bytes (F4), and the `migration` arm reading is restated as pending the post-F1 run
(F2, §7.5/§9).

## 3. Change-6 grep census

Command (repository root): `grep -rn "<pattern>" daemon/ scripts/ --include=*.go --include=*.sh`.

| Pattern | Sites | Disposition |
| --- | --- | --- |
| `release_outcome_unknown` | `helper_v2_server.go:1200` (writer) | converted: the response now carries the bounded detail beside the unchanged code |
| | `linux_owner_helper_v2_linux_test.go:703,709` (evidence pins), `:1149,1152,1156,1157,1194,1232,1251,1273,1290,1295,1376` (detail pins; `:1273` is the writer probe, `:1290` the fix-stage writer-refusal probe) | tests; the code-only rendering assertion pins byte-identical backward compatibility |
| `recover existing owner witness RunJournal` | `linux_owner_witness_linux.go:538` | converted: `refusal=<guard> reason=<term> recovery_error_class=<class>: <recoveryErr chain>`; the document names the same codes |
| `validateFinalSummary` refusals | `:765-767` (`unresolved` case) | converted: names every tripped predicate |
| | `:748,:752` (release tri-state / nativeReleased) | exempt: single named property each, already in the message |
| | `:738,:743` (containment) and `:760` (helper-drop) | exempt: single named property each |
| | `:755` → `validateStageReplayResponseLostSummary` | exempt: delegated sub-validator whose own messages name the failed exact boundary |
| | `:773,:780,:785,:789` (`aborted_no_target`, `no_native_effects`, `released`, unknown state) | exempt: each message names its property group; outside this goal's clause |
| | `:787` → `validateStageReplayPhysicalStopSummary` | exempt: delegated sub-validator, messages name the missing physical proof |
| | `:797,:803` (WSL2 denial) | exempt: names the exact observation class |
| | `:808,:811` (peer denial, Pi resource.sync) | exempt: single named property each |
| | `:814,:819` (OpenCode health) | exempt: names the exact boundary |
| | `:827,:833,:836` (Codex, detached child, helper loss) | exempt: single named property each |

No site was rewritten to hide a failure; every exempt message already names the property it rejects.

Reader/validator census for the additive helper-response field (ruling Q-B(a): "update every reader
and validator of the witness response"); command
`grep -rn "linuxOwnerHelperV2Response\b" daemon/ --include=*.go`:

| Reader / validator | Disposition |
| --- | --- |
| `readLinuxOwnerHelperV2Response` (`helper_v2_protocol.go:609-646`) | updated: runs `ReleaseErrorDetail.Validate()` after decode (`:640`), so an unknown sub-step/reason or a mixed shape is refused on the wire |
| `linuxOwnerHelperV2ResponseError` (`:647-668`) | updated: builds the typed unresolved error with the detail for every `release`/`unresolved` response, joined with its code-driven sentinel |
| `linuxOwnerHelperV2Response.matches` (`:129-132`) | unchanged: identity-only (version/RunKey/fence/launch token/helper); the additive field cannot satisfy or break it |
| literal response constructors (`helper_v2_server.go:935,:960`; `helper_v2_stop.go:289,:415`) | unchanged: no detail (nil, omitted) — only the unresolved release writer sets it |
| test literals (`linux_owner_helper_v2_linux_test.go`, the wire/read probes and the fix-stage writer probes) | unchanged protocol shape; the fix-stage probes add the checkout-rejection exchange and the writer-refusal rows |
| `ErrLinuxOwnerHelperV2ReleaseUnresolved` consumers | tests only (`containment_errors_test.go:252`, `linux_owner_helper_v2_linux_test.go:708,1182`); no production caller keys off the type, so the extra typed case (evidence-less unresolved responses) changes no decision. `ErrLinuxOwnerHelperV2OwnerLost` keeps its production consumers: the app recovery class (`linux_owner_witness_linux.go:3013`) reads `errors.Is`, which the fix-stage join (`helper_v2_protocol.go:661`) preserves |
| App-side witness summary readers (`linuxV2OwnerWitnessResponse` in `daemon/e2e/**`, the App's `writeLinuxV2NativeWitnessSummary`) | different protocol, untouched; the helper field never crosses it |

Stage-3 addition to the same census (ruling: "update every reader and validator of the witness
response, and grep-census them"): the arm/wait/guest fields are validated on the read path by
`readLinuxOwnerHelperV2Response` → `ReleaseErrorDetail.Validate()`
(`helper_v2_protocol.go:508-565`); the helper's write path now runs the same `Validate()` before
encoding (`helper_v2_stop.go:303`), and the probe file covers both directions. `LookupReleaseArmShape`
has exactly one non-test consumer (`helper_v2_protocol.go:538`), `ReleaseWaitErrorClass` and
`ReleaseGuestStageName` exactly one each
(`NewReleaseSubStepError`, `gate_lifecycle_linux.go:809,:814`), `NewReleaseSubStepError` 21 call
sites — 16 in `gate_lifecycle_linux.go`, 5 in `helper_v2_server.go`, all owned — and the fix-stage
split has one consumer (`NativeExecVerifyArm` at `gate_lifecycle_linux.go:1006`) fed by seven
`NativeExecProcessSide` sites inside `verifyNativeImage`
(`gate_lifecycle_linux.go:1337,:1340,:1347,:1351,:1356,:1369,:1374`); command
`grep -rn "NewReleaseSubStepError(\|LookupReleaseArmShape(\|ReleaseWaitErrorClass(\|ReleaseGuestStageName(\|NativeExecVerifyArm(\|NativeExecProcessSide(" daemon/ --include=*.go`.

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

Fix-stage re-run of the whole gate on the delivered bytes (WSL Ubuntu-24.04 `-u accept`, from
`daemon/`, after the F1-F10 changes and the probes), all exit 0:

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no file listed (the five over-cap comment blocks listed by the fix brief are 2 lines each) |
| linux vet + e2e compile | `GOOS=linux go vet ./...` and `GOOS=linux go vet ./e2e/` | 0 / 0 | clean |
| focused suites (race, contract gate) | `go test -race -count=1 -timeout 1800s ./internal/platform/ ./internal/execution/` then `go test -race -count=1 -timeout 1800s ./internal/app/` | 0 / 0 | `ok platform 66.426s`, `ok execution 22.322s`, `ok app 582.126s` (under the command's own 600s default, so the gate as written passes without the raised cap) |
| new probes under load | `GOMAXPROCS=8 go test -race -count=200 -timeout 1200s -run '<the five fix-stage probe tests>' ./internal/platform/` | 0 | `ok internal/platform 28.580s` (200 iterations of the F1/F5/F6/F7/F8 probes incl. the socket/goroutine checkout-rejection exchange) |
| host vet | `go vet ./...` (Windows host, Go 1.27.4, from `daemon/`) | 0 | clean |
| scripts | `git diff --stat -- scripts/ .github/` (WSL) | — | empty: no script or workflow byte changed by this goal, so `bash -n`/`shellcheck` delta is 0 by identity |

## 5. Probes (lesson 74)

Every new guard has a table test with a positive and a negative case; run in WSL from `daemon/`:

| Guard | Command | Exit | Result |
| --- | --- | --- | --- |
| release-error → sub-step classifier (20 rows: 15 classified incl. errno, reasons, join/wrap/nesting survival, and 5 untyped/reason rows — §2.9) | `go test -count=1 -run 'TestLinuxOwnerHelperV2ReleaseErrorDetail' ./internal/platform/` | 0 | ok |
| closed-vocabulary validator (18 rows) | same file, `TestLinuxOwnerHelperV2ReleaseErrorDetailValidatesClosedVocabulary` | 0 | ok |
| free-form redaction (synthetic path/PID/token string) | `TestLinuxOwnerHelperV2ReleaseErrorDetailRedactsFreeFormText` | 0 | ok |
| bounded rendering + sentinel chain (5 rows) | `TestLinuxOwnerHelperV2ReleaseUnresolvedErrorCarriesTheBoundedDetail` | 0 | ok |
| wire carry + wire refusal of a free-form detail | `TestLinuxOwnerHelperV2ResponseErrorCarriesTheReleaseDetail`, `TestLinuxOwnerHelperV2ResponseRejectsAFreeFormReleaseDetail` | 0 | ok |
| refusal-guard formatter (8 rows + rendering pin) | `go test -count=1 -run 'TestLinuxV2NativeWitnessRecoveryRefusalNamesTheGuard' ./internal/app/` | 0 | ok |
| recovery error class (10 rows, one per arm) | `TestLinuxV2NativeWitnessRecoveryErrorClassClosesTheVocabulary` | 0 | ok |
| refusal-document authority shape (9 authority rows + missing code + free-form reason) | `TestLinuxV2NativeWitnessRefusalDocumentIsIdentityOnly` | 0 | ok |
| summary diagnostic predicate (16 rows: one per predicate, the exact-versioned acceptance, the F3 unknown/absent version rows, and the ordered multi-trip) | `TestLinuxV2NativeWitnessUnresolvedSummaryTrippedNamesPredicates` | 0 | ok |
| validator error names the tripped predicate | `TestLinuxV2NativeWitnessUnresolvedSummaryErrorNamesTheTrippedPredicate` | 0 | ok |

Stage-3 rows (all `go test -count=1 -run '<name>' ./internal/platform/` in WSL `-u accept` from
`daemon/`, exit 0 each; re-run together as `-run 'TestLinuxOwnerHelperV2Release'`):

| Guard | Probe | Rows |
| --- | --- | --- |
| every closed arm renders its literal code, is bound to its sub-step and carries exactly the shape's wait class | `TestLinuxOwnerHelperV2ReleaseErrorDetailNamesEveryArm` | 21 positive rows + the alias/duplicate negative |
| closed wait class (nil, `context.DeadlineExceeded`, `os.ErrDeadlineExceeded`, wrapped deadline, `io.EOF`, wrapped `io.EOF`, `io.ErrUnexpectedEOF`, `ECONNRESET`, `ENOTCONN`, `EPIPE`, `context.Canceled`, the raw channel-closed text) | `TestLinuxOwnerHelperV2ReleaseWaitErrorClassClosesTheVocabulary` | 12 |
| closed guest stage (`request`…`exec`) with the guest errno + negatives (unknown enum, an arm that cannot read the channel, the errno arm keeping its own errno) | `TestLinuxOwnerHelperV2ReleaseGuestFailureDetailClosesTheVocabulary` | 10 |
| the full detail survives the helper wire and re-validates | `TestLinuxOwnerHelperV2ReleaseDetailSurvivesTheWire` | 1 |
| closed-vocabulary validator: 5 accepted shapes and 13 refused shapes (free-form/cross-stage arm, arm without a sub-step, missing wait class, wait class on a non-waiting arm, free-form wait class, guest stage on a non-reading arm, free-form guest stage, errno without a sub-step, …) | `TestLinuxOwnerHelperV2ReleaseErrorDetailValidatesClosedVocabulary` | 18 |
| the wire refuses a free-form sub-step, a free-form arm and a cross-stage arm | `TestLinuxOwnerHelperV2ResponseRejectsAFreeFormReleaseDetail` | 3 |
| the classifier still names the arm on every classified row (15 rows) and never labels a checkout failure `native_exec` | `TestLinuxOwnerHelperV2ReleaseErrorDetailClassifiesSubSteps` | 20 |
| (apply stage) the writer helper's field assignment reaches the wire | `TestLinuxOwnerHelperV2ReleaseDetailIsWrittenThroughTheResponseWriter` | 1 |

Fix-stage rows (F1, F3, F5-F8; all `go test -count=1 -run '<name>' ./internal/platform/` or
`./internal/app/` in WSL `-u accept` from `daemon/`, exit 0 each, re-run together with the stage-3
rows; each guard was also executed in isolation before the fix, one positive and one negative case):

| Guard | Probe | Rows |
| --- | --- | --- |
| the release wrap never re-labels a labelled error (F1) | `TestLinuxOwnerHelperV2ReleaseErrorDetailClassifiesSubSteps` (the three fix-stage rows) | 3 (positive: the inner clone arm plus guest stage/errno wins; positive: the helper's `checkout_receipt_mismatch` survives the `checkout_callback` wrap; negative: an unlabelled wait error still renders `await_result`) |
| the native-exec verify split (F8) | `TestLinuxOwnerHelperV2NativeExecVerifyArmSplitsProcessFromReadback` | 4 assertions (process-side marker arm, its verbatim `Error()`, a wrapped marker, the readback negative) |
| the code-driven sentinel stays in the unresolved chain (F5) | `TestLinuxOwnerHelperV2ResponseErrorKeepsTheReleaseCodeSentinel` | 2 (`owner_lost_during_release` keeps `ErrLinuxOwnerHelperV2OwnerLost`; `lease_expired_during_release` keeps the generic rejection text) |
| the writer validates its detail before encoding (F6) | `TestLinuxOwnerHelperV2ResponseWriterRefusesAnInvalidReleaseDetail` | 2 (negative: an invalid detail is refused and nothing is written; positive: the next valid frame decodes) |
| the checkout rejection carries the terminal detail (F7) | `TestLinuxOwnerHelperV2ReleaseCheckoutRejectionCarriesTheTerminalDetail` | 2 (unresolved: the detail joins the typed error; released: the rejection guard fires and no detail is claimed) |
| the stop-receipt version rows (F3) | `TestLinuxV2NativeWitnessUnresolvedSummaryTrippedNamesPredicates` (`unknown stop receipt version`, `absent stop receipt version stays untripped`) | 2 |

The characterization baseline for each changed check is the base tree's own behaviour, captured before
any edit: the base six-set run (§7.1) holds the release line without a sub-step
(`… release outcome is unresolved (release_outcome_unknown)`), the refusal line without a guard
(`recover existing owner witness RunJournal without launch`) and the summary refusal without predicate
names (`stage replay unresolved outcome lacks retained journal/owner uncertainty`). The pre-existing
pin of the untouched shape is `TestLinuxV2NativeWitnessStageReplayRequiresExactUnresolvedCategory`.

## 6. Mutants (M1-M13)

Each mutant is applied to a scratch copy of the delivered bytes outside the goal worktree (the
delivered files are never mutated) and restored byte-identical (`cp` + `cmp`, digest re-verified after
every row; every row printed `restore=EQ`). Rows M1-M7 were re-derived on the fix-stage bytes; M8-M13
are the fix-stage guards. The CI job that must redden is `daemon`
(`daemon/`, `go test -race -count=1 -timeout 600s -json ./...` with the stdio-only caller boundary)
on `ubuntu-latest`, which runs as the non-root `runner` user; the local focused run is the pinned red.
The restored digest is the file's delivered digest (§7.4).

| id | mutation | expected red | observed red (verbatim) | restored digest |
| --- | --- | --- | --- | --- |
| M1 | `linuxOwnerHelperV2ReleaseErrorDetailFor` drops the `errors.As` classification (no sub-step, arm or errno) | classifier table test, positive rows | `linux_owner_helper_v2_linux_test.go:848: release detail = {Stage:none Arm: WaitClass: GuestStage: Errno:0 Reason:}, want stage="gate_authorize" arm="missing_gate_identity" wait_class="" guest_stage="" errno=0 reason=""` (all 15 classified rows) | `2720cf55…` (`helper_v2_protocol.go`) |
| M2 | classifier mislabels a `clone_checkout` failure as `native_exec` | classifier table test, negative case | `:848: release detail = {Stage:native_exec Arm:checkout_receipt WaitClass:other GuestStage:exec Errno:1 Reason:}, want stage="clone_checkout" arm="checkout_receipt" wait_class="other" guest_stage="exec" errno=1 reason=""`; the `checkout_receipt_mismatch` row red the same way | `2720cf55…` |
| M3 | refusal formatter reverted to the single generic string (guard, reason and class dropped) | refusal-probe test, every refusing row | `linux_owner_witness_linux_test.go:682: refusal rendering = "refusal=", want the named guard, reason and class` | `5cbf4c74…` (`linux_owner_witness_linux.go`) |
| M4 | accept an unknown/inexact stop receipt in the `unresolved` branch (the stage-2 predicate dropped) | stale-leaf validator negative cases | `linux_owner_witness_linux_test.go:910: tripped predicates = [], want [stop.receipt_version]` ×3 rows (`versioned_receipt_without_physical_proof`, `unknown_stop_receipt_version`, `versioned_receipt_without_its_physical_flags`); `:927: unresolved summary refusal = <nil>, want the named tripped predicate` | `5cbf4c74…` |
| M5 | writer emits `Outcome` in the refusal document | refusal-authority-shape test | `linux_owner_witness_linux_test.go:733: write owner witness refusal document: owner witness refusal document exposed a non-identity field "outcome"` | `5cbf4c74…` |
| M6 | bounded arm replaced by free-form `err.Error()` | classifier, arm-literal and redaction probes | `linux_owner_helper_v2_linux_test.go:1032: release detail rendered the raw fixture text (a disposable-home path, a pid and a lease token) as its arm, want the closed sub-step, arm and errno only` | `2720cf55…` |
| M7 | two arm codes swapped (`ReleaseArmNativeNoAttempt` ↔ `ReleaseArmNativeReadback` values, same sub-step and shape, so the shape table stays valid) | arm-literal test | `linux_owner_helper_v2_linux_test.go:903: arm "exec_readback" rendered "stage=native_exec arm=exec_readback", want the literal "stage=native_exec arm=no_exec_attempt"` | `7761233f…` (`gate_lifecycle_linux.go`) |
| M8 | `NewReleaseSubStepError` re-wraps a labelled error (the F1 guard removed) | classifier fix-stage positive rows | `linux_owner_helper_v2_linux_test.go:848: release detail = {Stage:native_exec Arm:await_result WaitClass:other GuestStage: Errno:0 Reason:}, want stage="clone_bootstrap" arm="bootstrap_attempt" wait_class="other" guest_stage="exec" errno=1 reason=""` (both positive rows) | `7761233f…` |
| M9 | `NativeExecVerifyArm` returns the opposite arm (F8 split swapped) | verify-split probe | `linux_owner_helper_v2_linux_test.go:950: process-side verify arm = "exec_readback", want native_process` | `7761233f…` |
| M10 | `linuxOwnerHelperV2ResponseError` drops the code-driven sentinel joins (F5) | sentinel probe | `linux_owner_helper_v2_linux_test.go:1334: unresolved release lost its code sentinel: Linux owner helper release outcome is unresolved (owner_lost_during_release; …)`; `:1337: … lost the generic rejection text: … (lease_expired_during_release; …)` | `2720cf55…` |
| M11 | `writeResponseWithDetail` skips `Validate()` (F6) | writer-refusal probe | `linux_owner_helper_v2_linux_test.go:1292: writeResponseWithDetail accepted an invalid release detail: <nil>` | `f8d5b77f…` (`helper_v2_stop.go`) |
| M12 | checkout rejection returns `callbackErr` alone (F7) | checkout-rejection probe | `linux_owner_helper_v2_linux_test.go:1441: checkout rejection error = gate refused the checkout callback, want "stage=clone_checkout arm=checkout_callback"` | `b2018753…` (`linux_owner_helper_v2_linux.go`) |
| M13 | `NativeExecProcessSide` returns the cause unwrapped (the process-side marker dropped) | verify-split probe | `linux_owner_helper_v2_linux_test.go:950: process-side verify arm = "exec_readback", want native_process` | `7761233f…` (`gate_lifecycle_linux.go`) |

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
| `migration` | `production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness command did not reach its requested durable stage` (owner-witness command exit status 1) | `-linux-v2-native-owner-witness: start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=native_exec)` — the sub-step is named; **no errno** is emitted, so the failing arm carries none (the four non-errno `native_exec` sites, delivered bytes `:979` await-guarded-result, `:999` no-exec-attempt, `:1006` same-PID verify, `:1125` clone native-exec proof). The App then cannot prove the stop: `Linux v2 helper could not prove the exact run leaf stopped: dial unixpacket: connect: connection refused` (four times in this run; §9 attributes it there and notes it is intermittent) and the RunJournal projection stays `{"local_state":"claimed", …, "recovery_required":true}` — the release never succeeded, so `native_release` is never stamped |
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
(call sites `linux_owner_witness_linux.go:871,:878,:885`, the predicate at `:904`, pinned by
`TestLinuxV2NativeWitnessAllowsReceiptBackedLostClearAck`
and `TestLinuxV2NativeWitnessAllowsExactStopReceiptForLostReceiptAck`), and it is at least as strict as
the e2e's own acceptance at `production_linux_v2_owner_cli_test.go:1276-1284` (version V2, status
`stopped`, a non-empty proof kind, non-nil issuer/reaper, the five physical flags; the App predicate
additionally requires the launcher/gate exit identities). Inexact receipts — versioned but missing a physical flag — stay tripped
(M4); a version-0 (unversioned) receipt is intentionally untripped, matching the pre-existing
predicate and the e2e's own acceptance, and is pinned by the "absent stop receipt version stays
untripped" row (the fix stage renamed the stale mutant row). The `migration` sub-step cause is
pending the post-F1 run (§9).

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

### 7.4 sha256 equality table (run heads vs delivered bytes)

Run `45557621798` was head `116114d3` (stage-2 bytes) and run `45564071868` was head `f32d47d9` (the
stage-3 bytes); both heads are the eight code files plus the two scratch-only files
(`git diff 48ef0982 <head>` = 10 paths). The fix stage then changed seven of the eight code files
(F1/F5-F8/F10 and their probes), so both earlier equality tables are historical: the post-F1 binding
run on the delivered bytes is the conductor's Bind-worker dispatch (one `migration` plus one
`stale_leaf`), and its equality table is filled there (§9). Delivered digests (sha256, `sha256sum`
at the fix stage):

| File | Run head `f32d47d9` (historical) | Delivered (fix stage) | Eq vs run head |
| --- | --- | --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `91e83974c871bac0675619fff58afe6a56d8df963d57ee307c202f4140f6c79d` | `5cbf4c74ebb96f64256c6beb62e79c9eaa0cf06b9cee5540ee2fa55149da3ca4` | Δ |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `aef98fd56448157b69a45090d3e15816377c7e4595756428fd127cd47ee37a90` | `f148a377532c7f4ab0382c2355f93cdca6387bf0ceb422cc380b7299fc324378` | Δ |
| `daemon/internal/platform/helper_v2_protocol.go` | `a9e255e045c78b2b985085a15c9d85bd722137e06d33568e8371290d741c02f1` | `2720cf5579c6a52ef49f7fc36ac2042feab3457a9afc8a319c0ea45b3e10575c` | Δ |
| `daemon/internal/platform/helper_v2_server.go` | `a00c6606f9da60b12322d93835f6d3f9d549cbce6b662dd5aefa1b8982c2da6b` | `a00c6606f9da60b12322d93835f6d3f9d549cbce6b662dd5aefa1b8982c2da6b` | EQ |
| `daemon/internal/platform/helper_v2_stop.go` | `d216fd1d3ed719a78e4ef9ebd83c33a671601e687669ffec3c3639b037e3773e` | `f8d5b77f706177418f3309f51dd61aecd78c2c61910903de5e7352cdb068ab87` | Δ |
| `daemon/internal/platform/linux_owner_helper_v2_linux.go` | `b55b1768bc413517b0aab719087498139ea4ffbba9495326273709e512da1831` | `b2018753088d48aab5cc0ef40d1569f6985b899d983f0687647d33a6f2602745` | Δ |
| `daemon/internal/platform/linux_owner_helper_v2_linux_test.go` | `7dd9b57e2678339823abb3668e0fd919a0d040484c949044d9985eadeede4c19` | `4255c5ca22464982247e9b102b1f8418460c9aa0e288df30dc950bd77a396868` | Δ |
| `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` | `cd803b8aa004fae929dd0a6e290555a78ea71ccfb983bd89f275694b0b3c6075` | `7761233ffeb044da36d20fc574c5a7a094dad067b264144dbac554b7b4a45488` | Δ |

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

### 7.4a Apply-stage delta (test-only)

The apply stage applied accepted simplify findings 1-3 (2026-11-25) and changed only the two
unit-test files after every run; the six product files and both scripts stay byte-identical to the
run bytes. Neither run set compiles unit-test files (the harness builds `./cmd/...`, and the guest
runs `go test ./e2e` / `./native/linux-gate/protocol` with `-run` patterns), so the executed subject
of every run is unchanged; the local gates and probes were re-run on the delivered bytes with the
commands of §4 and §5, and the M2/M6/M7 rows were re-derived on them (§6).

| File | Run-bytes sha256 | Delivered sha256 | Delta (`git diff --numstat`) |
| --- | --- | --- | --- |
| `daemon/internal/platform/linux_owner_helper_v2_linux_test.go` | `7dd9b57e2678339823abb3668e0fd919a0d040484c949044d9985eadeede4c19` | `3ba872ca0ee1e63453d0950b8429620a6467f79373a49fb7db3dc069da7344c4` | 17 added / 6 removed: two unreachable assertions removed from the classifier loop (finding 1); the `writeResponseWithDetail` wire probe added (finding 3) |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `aef98fd56448157b69a45090d3e15816377c7e4595756428fd127cd47ee37a90` | `8968341e0fcab1855af3abe60c260e3cbce381b1a59cdae49b86aad335d93ed3` | 6 added / 6 removed: three `bytes`-shadowing locals renamed in `TestLinuxV2NativeWitnessRefusalDocumentIsIdentityOnly` (finding 2) |

The fix stage then changed seven of the eight code files again (F1/F5-F8/F10 and the probes, §2.9,
§6); the apply-stage delta above is history, and §7.4's delivered column carries the fix-stage bytes.

### 7.5 Stage-3 run `45564071868` (migration only — the pre-F1 arm)

Head `f32d47d9` (the stage-3 bytes + the two scratch-only files, one case set `migration`),
2026-11-25T06:54:24Z → 06:59:15Z; jobs `contracts` success (`139034466893`),
`linux-native-v2-acceptance` failure (`139035057733`); `Case set migration exited 1` /
`Failed case sets: migration`. The test ran `31.11s` and stopped at the same first check as §7.1-§7.3
(`production_linux_v2_native_witness_test.go:613: wait for exact native_release boundary: owner-witness
command did not reach its requested durable stage`), but the release detail now names the failing arm
verbatim in the App stderr:

```
-linux-v2-native-owner-witness: start native owner witness transport: resume process "/usr/local/lib/symmetry-pi/pi": one-use Linux v2 RELEASE result is unresolved: Linux owner helper release outcome is unresolved (release_outcome_unknown; stage=native_exec arm=await_result wait_class=other)
```

Reading (pre-F1, superseded by the fix stage): this run's bytes carry the stage-3 wrap, so
`arm=await_result` is that wrap's own code for an error the wrap could not classify; per the
fix-stage F1 ruling a labelled `awaitCloneRelease` arm (or the helper's checkout-receipt mismatch)
inside the await failed with exactly this rewriting, so the observation does not establish which arm
failed — it is **pending the post-F1 binding run** (§9). `wait_class=other` means the read failed on
its own terms — neither the lease/deadline wait nor a peer EOF/`ENOTCONN`/`EPIPE`/`ECONNRESET`, which
render `deadline`/`peer_closed`; the class stays honest for an unlabelled failure. No producer is
named here: the candidates inside that read are the gate C code's exec-status producer
(`daemon/native/linux-gate/c-gate/gate.c`, `exec_as_b`, which sends `EXEC_FAILURE(errno)` on every
internal failure and the `FRAME_EXEC_ATTEMPT` at `:1276` before the exec) and the reused guest, none
of them owned by this goal — so per contract Change 7 and the conductor ruling this clause stops with
the definitive arm still pending in `docs/TODO.md` (§9).

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
four candidate sites at the delivered bytes (`gate_lifecycle_linux.go:979,:999,:1006,:1125`), not the
errno-reporting `:992` that the tracked `launcher_integration_linux_test.go:439` stop (`native exec
failed after RELEASE: errno=1`) names. The stage-3 rerun (run `45564071868`, §7.5) reported
`stage=native_exec arm=await_result wait_class=other`, but F1 proved those bytes re-wrapped every
labelled clone arm into `await_result`, so that observation cannot establish the failing arm; the
definitive arm is **pending the conductor's post-F1 binding run** (one `migration` plus one
`stale_leaf` on the PR head), and no guest producer is named before it is observed. One named fact
stands independent of the arm: the witness vehicle sets a `CloneGrant`
(`linux_owner_witness_linux.go:3353`), so this release path is not a reuse-only site; the
`awaitCloneRelease` arms are distinguishable only after the F1 guard. Per contract Change 7 and
the conductor ruling this clause stops with `DECISION_NEEDED`: the residual pointer in `docs/TODO.md`
routes to `rf-acc-guest-native-exec` only once the arm is observed; if the post-F1 arm is inside the
owned Go set and the fix is a one-site change, the conductor decides it in a later round.
The guest-side staging of the executed binary is already correct and verified
(`/usr/local/lib/symmetry-pi/pi` `regular file root:root 755 1`, `test -x` as A; run `45555828634`
log, "guest view of the staged native tools"). After the aborted release the App cannot even measure
a stop — `Linux v2 helper could not prove the exact run leaf stopped: dial unixpacket: connect:
connection refused`, observed four times in run `45555828634` and zero times in runs `45557621798`
and `45564071868` (an intermittent measurement, not a consequence of the final-bytes run; measured
with `gh run view --log` over those three runs) — which is why `native_release` is never stamped and
the wait times out. Stage 1, the `stale_leaf` stage-2 fix, the stage-3 diagnostics and the fix-stage
F1-F10 changes still merge.

The four `stale_journal_*` sets' refusal is the owner-loss consult's candidate (B): the verbatim App
stderr of run `45557621798` is
`recover existing owner witness RunJournal without launch: refusal=journal_without_unresolved_authority
reason=release_proof_present recovery_error_class=recovery_failed: Linux clone cleanup requires its
exact workspace adapter and complete durable receipts` — a post-tombstone failure in the forced clone
cleanup path, not in `anchor.RetireHelper`. It is handed to `rf-acc-owner-loss-semantics` (Q-D(a)); no
green row is claimed here.

## 10. Residual risks and limits

- `migration`'s arm stays pending the post-F1 run (§9): `stage=native_exec arm=await_result
  wait_class=other` was the stage-3 observation and may be a masked arm; the definitive arm is
  pending the post-F1 binding run, and its producer (the gate C code / reused guest, or an owned
  Go site) is not named before it is observed. The `native_release`, `turn`, `stop_receipt_durable`,
  `leaf_removed` and `journal_cleared` boundaries of every pi leg strictly after `native_release`
  remain unreachable until it is fixed.
- Wait-class granularity is the ruling's three-class vocabulary, so every non-deadline, non-EOF
  failure inside `await_result` renders `other` — including the conflated
  `exec-status channel closed or carried ancillary data before a frame` (`receiveStatusPacket`,
  `wire_linux.go:685`, `n == 0` in practice: the gate closed the FD before a frame) and the
  frame-level rejections of the same read. Splitting those needs a sentinel in the unowned
  `wire_linux.go`; they are the follow-up goal's first discriminating probes, and the class is honest
  as delivered (`other` = neither `deadline` nor `peer_closed`, both of which have their own pins).
- The arm literals, the arm/sub-step binding and the wait/guest shape are pinned by owned probe rows
  (§5) and by mutants M7/M9 (§6); the *wrap-site* assignment itself (which site constructs which arm)
  has no owned unit test, because every test file in `daemon/native/linux-gate/protocol/` is outside
  the owned set. Two parts are now pinned outside it: the F1 guard (the wrap never re-labels; M8) and
  the verify-split classifier (`NativeExecVerifyArm`; M9); the remaining assignment — which
  `verifyNativeImage` returns are process-side — is pinned behaviorally by the post-F1 binding run,
  whose `migration` arm either matches an owned site's code or the residual pointer names the exact
  arm for `rf-acc-guest-native-exec`.
- The four `stale_journal_*` green rows stay `rf-acc-owner-loss-semantics`'s (Q-D); the refusal
  document now decodes, so that goal starts from a named guard, not from a decode failure. The
  `stale_journal_run_key` fresh-request precondition shows three variants of its pidfd message across
  runs — `process creation identity changed across pidfd open` (`45554251351`), `stage owner does not
  have exact A credentials` (`45555828634`) and `A anchor/helper was not live before fresh request:
  exact native process pidfd reports an exited target` (`45557621798`) — all F4-class, and none
  satisfiable by an owned file.
- The credentialed `linux-native-v2-acceptance` dispatch (owner-approved Environment, after merge) is
  conductor-owned; `stale_leaf`'s real pi turn is its proof of record.
