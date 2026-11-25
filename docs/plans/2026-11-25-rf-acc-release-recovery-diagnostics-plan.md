# rf-acc-release-recovery-diagnostics — implement plan

Contract: `goal-rf-acc-release-recovery-diagnostics.md` (GDD state root, external to the repository).
Base revision `48ef0982` (#284 `rf-acc-witness-unresolved-exit`, this goal's dispatch gate; #281
`rf-acc-pi-durability-matrices` supplies the case sets). Branch
`refactor/rf-acc-release-recovery-diagnostics`. Conductor rulings Q-A(a) (one staged goal), Q-B(a)
(typed sub-step errors + additive bounded detail, `error_code` unchanged), Q-C(a) (minimal redacted
refusal document), Q-D(a) (the four `stale_journal_*` rows hand off to `rf-acc-owner-loss-semantics`).

The accepted plan is the contract; this file resolves only the implementation choices it leaves
open, the clause census at the dispatch head, the stage-1 → stage-2 sequencing, and what is out of
scope.

## 1. Clause census at the dispatch head (re-located by content)

| Contract | Head `48ef0982` | Content | Disposition |
| --- | --- | --- | --- |
| helper `:1184-1203` | `helper_v2_server.go:1184-1203` | unresolved release block; `err` in scope, only `release_outcome_unknown`/`owner_lost_during_release`/`lease_expired_during_release` written | converted (detail field) |
| helper `:1126-1139` | `helper_v2_server.go:1126-1141` | gate authorization, lease recheck, `prepared.Release` | typed `gate_authorize`; lease recheck stays untyped (its code already names `lease_expired_during_release`) |
| helper `:1073-1099` | `helper_v2_server.go:1073-1099` | clone grant / native command / reuse guards | untouched (pre-attempt rejections, not the unresolved path) |
| helper `:1169-1173` | `helper_v2_server.go:1169-1175` | register + gate waiter | typed `register` |
| `linux_owner_helper_v2_linux.go:301-317` | `:297-318` | `LinuxOwnerHelperV2ReleaseUnresolvedError` | extended with `Detail` |
| `helper_v2_protocol.go:516-540` | `helper_v2_protocol.go:516-540` | `linuxOwnerHelperV2ResponseError` | carries the detail; typed for every release/unresolved response |
| `linux_owner_v2_linux.go:1066` | `linux_owner_v2_linux.go:1066` | `one-use Linux v2 RELEASE result is unresolved: %w` | unchanged; `%w` carries the message |
| `runner.go:864` | `runner.go:864` | `resume process %q: %w` | unchanged |
| app `:3279` | `:3279` | `start native owner witness transport: %w` | unchanged |
| app `:531-534` | `:531-534` | refusal collapses `recoveryErr` + authority into one string | converted (guard + reason + class + document) |
| app `:2801-2811` | `:2815-2826` | `linuxV2NativeWitnessJournalHasUnresolvedAuthority` | unchanged predicate; companion reason function added |
| app `:737-742` | `:737-742` | `unresolved` OR of ten predicates, names none | converted (tripped predicate names) |
| app `:3211` | `:3412,:3426,:3444` | the `native_release` hold after a successful release | stage-2 target for `migration` |

`git grep` census (Change 6) is recorded in the evidence record: `release_outcome_unknown` (helper
`:1188`, its only writer), `recover existing owner witness RunJournal without launch` (app `:533`,
its only site), every `validateFinalSummary` refusal.

## 2. Public/API shape (choices the contract leaves open)

1. **Sub-step type** lives in `daemon/native/linux-gate/protocol/gate_lifecycle_linux.go` (the one
   owned file in that package; platform already imports it):

   ```go
   type ReleaseSubStepError struct { SubStep string; Errno int; Err error }
   ```

   `Error()` returns the wrapped error's message verbatim (so `native exec failed after RELEASE:
   errno=1` and every other current line stays byte-identical); `Unwrap()` exposes `Err`. Closed
   sub-step constants: `gate_authorize`, `clone_bootstrap`, `clone_checkout`, `native_exec`,
   `register`. Wrap sites: the `clone_bootstrap` sites (`:664,:842,:845,:855,:857`), the
   `clone_checkout` sites (`:849,:851,:869,:901`; the helper's own checkout-receipt mismatch
   `helper_v2_server.go:1121`), the `native_exec` sites (`:762,:769,:781` — this one carries
   `status.errno` — `:787,:793,:905`), `gate_authorize` (`:689` and
   `helper_v2_server.go:1127-1131`), `register` (`helper_v2_server.go:1172,1174`). The remaining
   guards (lease recheck, release-closed states, witness action) stay untyped `none`; their
   information is already the response `error_code` or the surrounding branch.
2. **Detail wire type** in `helper_v2_protocol.go`:
   `LinuxOwnerHelperV2ReleaseErrorDetail{Stage string json:"stage"; Errno int json:"errno,omitempty";
   Reason string json:"reason,omitempty"}`. `Stage` is always set (`none` for an untyped error);
   `Errno` only when the sub-step carries one (`native_exec`); `Reason` only when `err == nil`
   (`owner_lost | lease_expired | stop_requested`, resolved in that precedence). `String()` renders
   `stage=<closed>[ errno=<n>][ reason=<closed>]`; `Validate()` enforces both closed vocabularies,
   and `readLinuxOwnerHelperV2Response` validates the field after decode (a free-form field from the
   wire is rejected, not carried).
3. **Additive response field**: `ReleaseErrorDetail *… json:"release_error_detail,omitempty"`. The
   write path is a new `writeResponseWithDetail` in `helper_v2_stop.go`; `writeResponse` delegates to
   it with `nil`, so the 160+ existing call sites in the unowned files keep their signature.
4. **Carry-through**: `LinuxOwnerHelperV2ReleaseUnresolvedError` gains `Detail`; `Error()` renders
   the existing text plus `(<code>; <detail>)`; `linuxOwnerHelperV2ResponseError` builds it for every
   `release`/`unresolved` response (evidence pointer stays nil when the helper could not measure it).
   `errors.Is(err, ErrLinuxOwnerHelperV2ReleaseUnresolved)` is unchanged; no production caller keys
   off the typed error today (census in the record).
5. **Refusal naming** (app): `linuxV2NativeWitnessRecoveryRefusal(caseName, journal, recoveryErr)
   (refusal, refused)`, guard codes `case_not_stage_replay` /
   `journal_without_unresolved_authority`; for the authority arm a bounded reason derived from the
   present journal terms (`release_proof_present`, `clone_abort_proof_present`,
   `clone_promotion_receipt_present`, `no_unresolved_term`); plus a closed recovery error class
   (`cancelled`, `timeout`, `owner_lost`, `helper_recovery_only`, `outcome_unknown`,
   `gate_never_produced`, `cgroup_unresolved`, `recovery_failed`), each arm pinned by its sentinel.
   The returned error keeps the verbatim prefix and joins the `recoveryErr` chain with `%w`.
6. **Refusal document** (app): `linuxV2NativeWitnessRefusalDocument` carries only
   `version/case/harness/native_version/installed_owner_verified/run_key/fence/subject` and
   `recovery_refusal{guard,reason,recovery_error_class}` — no `Outcome`, `FailureCategory`,
   `RecoveryState`, `Stages`, `Release` or `Stop`. `writeLinuxV2NativeWitnessRefusal` encodes with
   the same escaping as the summary writer, validates the encoded bytes against the closed key set
   (and the refusal vocabulary) and writes only when valid. The e2e decoder ignores the unknown
   `recovery_refusal` key, so no e2e edit is needed (Q-C). It is written only for the
   `stage_replay` authority arm; the case arm keeps the error-only behavior.
7. **Summary diagnostics** (app): `linuxV2NativeWitnessUnresolvedSummaryTripped(summary) []string`
   projects the `:737-742` OR into predicate property names (`outcome`, `failure_category`,
   `journal_unproven`, `no_target_abort`, `release.anchor_retired`,
   `release.opaque_platform_release`, `release.clear_persisted`, `release.leaf_removed`,
   `stop.receipt_version`, `stages.journal_cleared`); the error becomes
   `stage replay unresolved outcome lacks retained journal/owner uncertainty: tripped <names>`.

8. **Arm, wait class and guest stage** (conductor ruling 2026-11-25 on the implement
   `DECISION_NEEDED`; §2.3's accepted base is kept and extended). `stage=native_exec` without an
   errno named four sites, so the bounded detail gains a closed **arm** code, one per wrap site of
   every multi-arm sub-step the migration path can reach: `gate_authorize`
   (`missing_gate_identity | anchor_authorize | gate_identity`), `clone_bootstrap`
   (`installed_bootstrap | bootstrap_attempt | bootstrap_image | bootstrap_process |
   leaf_membership`), `clone_checkout` (`checkout_receipt | checkout_receipt_mismatch |
   checkout_callback | checkout_ack_encode | checkout_ack_send`), `native_exec` (`await_result |
   exec_errno | no_exec_attempt | image_mismatch | clone_exec_proof`) and `register`
   (`native_register | gate_waiter`); 21 wrap sites, all in owned files
   (`gate_lifecycle_linux.go` ×16, `helper_v2_server.go` ×5). One table
   (`LookupReleaseArmShape`) binds each arm to its sub-step and declares whether it reads the
   exec-status channel (wait class required) and whether a decoded guest clone-bootstrap failure can
   be its cause (guest stage/errno allowed). The wait class is closed (`deadline | peer_closed |
   other`, computed from the arm's own error by `ReleaseWaitErrorClass`, so an abort sibling's errno
   cannot reclassify it) and the guest stage is closed (`request | seed | target | git | checkout |
   native_image | exec`, mapped from `CloneBootstrapFailureError`). The guest's errno joins the same
   field, since it is the only errno the clone arms have (`wire.go:204` rejects `errno == 0`, the C
   gate falls back to `EPERM`); carried verbatim, so `migration` names either the wait class or the
   guest stage + errno. `error_code` and every existing rendered line stay unchanged.

## 3. Stage sequencing (contract Change 7 rides on stage-1 evidence)

1. Stage 1 = Changes 1-6 (behavior-preserving diagnostics) with the probes.
2. Scratch run on stage-1 bytes: the six sets; the record reads the named release sub-step + errno
   for `migration`, the refusal guard + class for the four denial legs, the tripped predicate for
   `stale_leaf` (the `stop.receipt_version` predicate is the contract's expectation; the observed
   name is what stage 2 fixes).
3. Stage 2: `stale_leaf` — the observed term is `stop.receipt_version` (run `45555828634`), so the
   predicate becomes `ReceiptVersion != 0 && !linuxV2NativeWitnessStopSummaryHasPhysicalProof(stop)`
   — the file's existing exact-proof predicate, which the response-loss branch already pins; every
   inexact/unversioned receipt stays tripped (M4). No second convention is added.
   `migration` — fix the named sub-step cause only if it is staging in an owned script; a cause in
   the gate C code, `control/**` or the guest outside the owned scripts stops with `DECISION_NEEDED`
   and the verbatim detail. The observed detail is `stage=native_exec` without an errno, i.e. one of
   the four non-errno `native_exec` sites inside the guest gate/bootstrap protocol
   (`gate_lifecycle_linux.go:803,:823,:830,:949`) — the guest-side class the contract routes to
   `DECISION_NEEDED`.
4. Stage 3 (conductor ruling on the implement `DECISION_NEEDED`): §2.8's arm/wait/guest extension
   plus one literal `migration`-only scratch run on the final bytes — the stage-2 six-set run
   (`45557621798`) stays the proof of record for `stale_leaf` (green) and the four denial legs
   (named guard, handed to `rf-acc-owner-loss-semantics`), so no second six-set run is spent. The
   run's verbatim detail goes to evidence §9; `migration` stays `DECISION_NEEDED` when the named arm
   is outside the owned set, and the residual pointer names the exact arm.
5. Scratch budget: three runs total in the goal (base confirmation, stage-1, stage-2), plus the
   stage-3 `migration`-only run; mutants M1-M7 are local (`daemon` job) and need no scratch run. If
   a run clears no new class twice, stop.

## 4. Local probes (lesson 74, each with ≥1 positive and ≥1 negative case)

- `linux_owner_helper_v2_linux_test.go` (owned): the classifier table — every closed sub-step, the
  unt typed `none`, the `native_exec` errno arm, each reason arm, the M2 negative case (a checkout
  failure is `clone_checkout`, never `native_exec`), and a free-form-bearing error whose rendered
  detail contains no fragment of the raw text (M6). Plus the `ReleaseSubStepError` message and the
  extended `Error()` rendering.

  Stage 3 rows: one row per arm literal (`TestLinuxOwnerHelperV2ReleaseErrorDetailNamesEveryArm`, 20
  arms — exact rendered string per arm, so a swapped constant value reddens; the arm→sub-step
  binding; no two arms render alike), the wait-class table (nil, context/os deadline, EOF, wrapped
  EOF, `ErrUnexpectedEOF`, `ENOTCONN`/`EPIPE`/`ECONNRESET`, canceled context, the raw channel-closed
  text — `TestLinuxOwnerHelperV2ReleaseWaitErrorClassClosesTheVocabulary`), the guest-stage table
  (one row per stage, its errno, and the negative rows: unknown stage enum, a guest failure on an arm
  that cannot read it, the errno arm keeping its own errno), a wire round-trip of the full detail,
  and the validator/wire negatives (free-form and cross-stage arms, missing wait class, wait class on
  a non-waiting arm, guest stage on a non-reading arm, errno without a sub-step).
- `linux_owner_witness_linux_test.go` (owned): the refusal formatter (both guards, the reason arms,
  the not-refused case), the recovery error class table (one row per sentinel arm), the
  refusal-document validator (positive bytes; negative rows for `outcome`, `failure_category`,
  `release`, `stop`, `stages`, `recovery_state` — M5), the writer's exact bytes, and the summary
  predicate names (one row per tripped predicate plus a clean summary) with the versioned-stop arm
  pinned in stage 2 (M4).

## 5. Lesson-83 assertion trace (what the changed checks expose)

- `stale_leaf` recovery leg: the checks after the changed one have never executed in any run
  (`45484692731`, `45489336388`): `requireLinuxV2RecoveryExitStatus` (now satisfied by #284),
  `linuxV2ValidateStageReplayResponseState("stop_receipt_durable", …, "unresolved")` (its Stop arm
  is the e2e-accepted shape this fix mirrors), `linuxV2ValidateStageReplayJournal`, and the two
  leaf-identity checks. Stage 2's run is their first execution; a failure there is a new named wall,
  reported, not hidden.
- four denial legs: after the refusal document the decode check passes; the next assertions
  (`requireLinuxV2RecoveryExitStatus` with an empty outcome, then `ValidateStageReplayResponse`,
  then the variant assertions/`linuxV2ValidateNegativeBarrier`) have never executed on any run and
  stay assigned to Q-D/F4; no green row is claimed.
- `migration`: after the release fix the `native_release` wait passes and the migration/denial legs
  (capture, alternate leaf, fail-closed recovery, journal retention) execute; the recovery-failure
  assertions have never executed in any run.

## 6. Evidence plan

`docs/evidence/rf-acc-release-recovery-diagnostics-2026-11-25.md`: the clause census, the per-set
table (before line from the parent runs; after: run id, case set, boundary, exit status, first
failing check, the verbatim named cause, artifact/manifest sha256), probe outputs, the M1-M7 rows
with restored blob hashes and the CI job that reddens, the sha256 equality table of the tested
bytes, the greps of Change 6, and the refusal-document bytes verbatim. Stage 3 adds §2.8 (the
arm/wait/guest vocabulary with the wrap-site table), the matching probe and mutant rows, §7.5 (the
`migration`-only run with the verbatim named arm) and the rewritten §9 residual pointer. Every
record stays free of a host path carrying a user profile name; no sha256 of
`docs/TODO.md`/`CLOSED-TODO.md`.
`docs/TODO.md`: the parent residual's product-class entry is split — the F1 bullet keeps its closure
statement, the `rf-acc-release-recovery-diagnostics` bullet gains this goal's evidence and is closed
where this goal closes it (moving to `docs/archive/CLOSED-TODO.md` only when nothing of it remains).

## 7. Out of scope (with reason)

- F1 exit contract (#284, closed), F4 semantics and the four denial rows' green state
  (`rf-acc-owner-loss-semantics`), `shared`/`synthetic_goal_cleanup`, `helper_v2_recovery.go`, the
  gate C code, `daemon/e2e/**`, `runner.go`, `control/**`, `docs/design/**`, the credentialed
  dispatch (conductor-owned after merge).
- The e2e-side `stale_journal_run_key` fresh-request pidfd precondition defect (`.agents` §4c of the
  parent record) belongs to F4; this goal records it, never edits `daemon/e2e/**`.
