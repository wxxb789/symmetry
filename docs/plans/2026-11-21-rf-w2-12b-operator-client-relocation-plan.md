# rf-w2-12b operator-client relocation plan — the operator driver moves into an e2e-owned helper

Contract: `goal-rf-w2-12b-operator-client-relocation.md` (GDD, 2026-11-21), the relocated half of
program-plan §5 W2-12 (`<gdd-state>/refactor/program-plan.md`), from findings F-09 and F-07
(`<gdd-state>/refactor/findings/daemon-protocol-rest.md:336-384`), reviewed at `4dd374b`
(`w2-contract-review-opus-1.md` P0-1/P0-2/P2-5, `w2-contract-review-ds-1.md` A5/A6/A8). Baseline:
this worktree's HEAD `a1142d4e939ba92de1aa8c5b047002a715e9e7d4` — every coordinate below was read at
that revision, and the tree was clean at dispatch.

Form: **relocation, not deletion** (opus P0-2; ds-1 A6's raw-HTTP alternative is not selected). The
operator task-control driver, its operator-exclusive validators and the v1 `Task`/`TaskWaiting`
decoders keep their exact text and move into a package owned by their only callers,
`daemon/e2e/internal/operator`. Production `control`/`protocol` keep every declaration their own
callers need; the shared substrate the driver also uses is *copied*, never removed from production.

## Inventory at the baseline

### Moved out of production (deleted there, text preserved in the new package)

| symbol | baseline definition | new home |
| --- | --- | --- |
| `OperatorClient` | `daemon/internal/control/client.go:125` | `e2e/internal/operator/client.go` |
| `NewOperatorClient` | `client.go:485` | same |
| `SubmitTask` | `client.go:987` | same |
| `GetTask` | `client.go:999` | same |
| `CreateTaskCommand` | `client.go:1015` | same |
| `operatorRequest` | `client.go:1121` | same |
| `operatorRequestWithStatus` | `client.go:1125` | same |
| `validateTaskResponse` | `daemon/internal/control/validation.go:209` | `e2e/internal/operator/validation.go` |
| `validateTaskCommandRequest` | `validation.go:281` | same |
| `validateTaskCommandResponse` | `validation.go:302` | same |
| `validateTaskWaiting` | `validation.go:334` | same |
| `sameCommandPayload` | `validation.go:441` | same |
| `isTaskState` | `validation.go:611` | same |
| `requiresTaskRun` | `validation.go:629` | same |
| `Task` + `UnmarshalJSON` + `HasField` | `daemon/internal/protocol/protocol.go:663/:677/:693` | `e2e/internal/operator/task.go` |
| `TaskWaiting` + `UnmarshalJSON` + `HasField` | `protocol.go:629/:640/:656` | same |

Removal is allowed only through the two-strategy proof (below); the contract's explicit list is a
candidate list, the deadcode intersection wins on disagreement (opus P0-1).

### Copied into the new package and retained in production

Driver substrate (`control/client.go`) — production's machine client, `AcknowledgeCommand`, the goal
family and `app` all keep using it, so nothing here is deleted:
`defaultMaxResponseBytes` (`:22`), `errRedirectNotAllowed` (`:24`), `ErrorCode` + its 14 constants
(`:27-44`), `APIError` (`:47`), `ResponseError` + `Error`/`Unwrap` (`:63/:67/:69`), `responseErrorf`
(`:71`), `(*APIError).Error` (`:75`), `IsOwnershipLost` (`:83`), `IsTerminalGraceExpired` (`:90`),
`Option`/`WithMaxResponseBytes` (`:96/:99`, A5), `transport` (`:107`), `newTransport` (`:496`),
`parseBaseURL` (`:521`), `RetryDelay` (`:1042`), `retryableTransportCause` (`:1073`),
`(*transport).request`/`requestWithStatus`/`perform` (`:1129/:1134/:1172`), `readBounded` (`:1218`),
`rejectDuplicateJSONMembersOnly` (`:1232`), `decodeJSON`/`decodeJSONUnchecked` (`:1240/:1247`),
`decodeAPIError` (`:1262`), `decodeEvidenceBatchConflictDetails` (`:1298`), `errorCodeForStatus`
(`:1317`), `retryAfter` (`:1342`), `validatePathID` (`:1839`).

Validators (`control/validation.go`) — retained for the production chain
`app.go → Client.AcknowledgeCommand (client.go:956, validate at :980) → validateAcknowledgementResponse`
(`:318`) → `validateTaskCommandResource` (`:352`) and for the machine snapshot path:
`invalidResponse` (`:638`), `validateTerminalPayloads` (`:265`, also the transition path),
`sameJSONObject`/`sameJSONValue`/`sameJSONNumber` (`:448/:460/:501`), `isSupervisoryCommand` (`:544`),
`ValidateSupervisoryPayload` (`:549`), `validateWork` (`:568`), `validateNullableJSONObject` (`:584`),
`validateJSONObject` (`:595`), `isJSONNull` (`:607`).

Decoders (`protocol/protocol.go`): `presentFields` (`:698`) stays — `Work`, `RuntimeRegistration`,
`ClaimResponse`, `LeaseHeartbeatResponse`, `Run` and `TaskCommand` all consume it.

`TaskSubmitRequest`, `TaskCommandRequest` and `TaskCommand` stay in `protocol`: `TaskCommand` is
production-live through the acknowledgement path, and the two request types are the wire contract of
the operator endpoints rather than the driver's private decode. They became driver-only in
production: under `daemon/internal`/`cmd` `TaskSubmitRequest` has no reference at all outside its
definition, and `TaskCommandRequest` is named only by the retained
`supervisory_validation_test.go` request-leg retarget (`:38`, a literal feeding the retained
validator); the e2e operator driver and witnesses are the callers everywhere else. (Contract
Change's conditional: recorded here and in the fix-stage report, not a deviation.)

## Deliverable shape

1. **New package `daemon/e2e/internal/operator`** (`package operator`), importable only from
   `daemon/e2e/**` by the `internal` placement:
   - `client.go` — the substrate copies plus the driver: `OperatorClient`, `NewOperatorClient`,
     `SubmitTask`, `GetTask`, `CreateTaskCommand`, `operatorRequest`, `operatorRequestWithStatus`,
     `RetryDelay` + `retryableTransportCause` (the driver's own tests classify the copied `APIError`),
     `IsOwnershipLost`/`IsTerminalGraceExpired` (the typed-error test's assertions) and the
     decode/error/transport helpers above.
   - `validation.go` — the seven moved validators plus the copies their bodies call.
   - `task.go` — `Task`, `TaskWaiting`, their presence-tracking decoders and a `presentFields` copy.
   - `client_test.go`, `client_review_test.go`, `validation_test.go` — the moved operator unit cases
     and the helper copies they need (see *Test split*).
2. **Production deletions**: exactly the 20 declarations of the first table, and nothing else.
3. **Production retargets (never deletions)**:
   - `internal/control/client_test.go` loses the four operator rows of `TestClientProtocolRequests`,
     the operator row of `TestClientRejectsIncompleteSuccessResponses`'s sibling file, the
     `TestClientTypedErrorsAndResponseValidation`/`TestClientUsesProtocolAPIPrefixForOriginBaseURL`
     bodies and the `NewOperatorClient` assertion of `TestNewClientRejectsInvalidConfiguration`
     (each re-homed byte-identically or with the documented mechanical adaptation below);
   - `internal/control/supervisory_validation_test.go` keeps its rows and re-expresses the parts that
     named moved validators against the retained ones (see *Supervisory retarget*);
   - `internal/control/client_review_test.go` keeps every machine case
     (`TestTransitionRequiresMatchingRunResource`, `TestAcknowledgeCommandRequiresMatchingCommandResource`,
     `TestAppendEventsRequiresNoContentWithoutBody`, `TestRuntimeSnapshotCommandsRejectInvalidKindsAndPayloads`,
     the three `RetryDelay`/`retryAfter` tests, the claim/lease/enroll/registration cases).
4. **e2e callers**: the five files the contract names (`core_test.go`,
   `production_daemon_crash_witness_test.go`, `production_linux_containment_witness_test.go`,
   `production_linux_containment_negative_witness_test.go`,
   `production_linux_containment_replay_witness_test.go`) change **qualifiers and imports only**:
   `control.OperatorClient`/`control.NewOperatorClient`/`control.APIError`/`control.ResponseError`/
   `control.StateConflict` and `protocol.Task` become the relocated package's symbols (`protocol.Task`
   → `operatorclient.Task`, …). No test body line, name, assertion or timeout changes; the CI
   `--assert` names are untouched.
5. **Docs**: this file; `docs/TODO.md` → `docs/archive/CLOSED-TODO.md` for the W2-12 follow-up part
   this slice closes, leaving W3-04's `harness.NewClaudeAdapterWithRunner` residual in place.

## Exact semantics

**Qualifier and import rule.** The new package keeps every moved/copied declaration's text verbatim;
the only sanctioned deviations are (a) the package clause and package doc of the new files, (b)
package qualifiers a moved declaration needs to name a *retained* type
(`protocol.Work`, `protocol.TaskCommand`, `protocol.TaskCommandRequest`, `protocol.TaskSubmitRequest`,
`contracts.SymmetryEvidenceBatchConflictDetailsV1`) and the reverse (a moved function's parameter
whose type moved with it loses the `protocol.` prefix: `validateTaskWaiting(task Task, waiting
TaskWaiting)`). The declaration-set proof scores both classes explicitly, so "zero logic edits" is
checked, not asserted.

**Import alias.** The five e2e files import the new package as
`operatorclient "github.com/wxxb789/symmetry/daemon/e2e/internal/operator"`. The default qualifier
`operator` is unusable there: those files declare local variables named `operator`
(`e2e/core_test.go:39`, `:96`, … at the baseline; `:40`, `:97`, … in the relocated tree) and the
same functions reference the moved `Task` type
(`waitForTask(…, func(task protocol.Task) bool {`), which a shadowed package name cannot resolve.
Renaming those locals would be a witness-body change; the alias is not. The relocation therefore
reads as a single, mechanical qualifier rewrite, which the witness-diff proof checks line by line.

**Test split (move vs. stay).** A case moves when its subject moved, and a case stays when its
subject stayed. For the three mixed tables this means:
- `TestClientProtocolRequests`: rows `submit task`, `get task`, `create cancel command`,
  `create provide input command` re-home as `TestOperatorClientProtocolRequests` in `client_test.go`
  (operator package). Row fields, handler assertions and the `invoke` closuress' bodies are
  byte-identical; the closure signature drops the unused machine parameter
  (`func(ctx context.Context, client *Client) error` → `func(ctx context.Context) error`) and the
  harness stops constructing the machine/enrollment clients. The ten machine rows keep their test.
- `TestClientRejectsIncompleteSuccessResponses`: the `task` row re-homes as a one-row
  `TestOperatorClientRejectsIncompleteSuccessResponses` with the same body, `wantError` string and
  assertion text; the seven machine/enrollment rows keep their table.
- `TestOperatorClientUsesDedicatedToken` moves whole (the operator token assertion and the
  machine-client guard, asserted from the same file through an import of `daemon/internal/control`).
  The guard's original interface named `protocol.Task`; after the move it could only name the
  relocated `Task`, which the machine client can never return, so it became unfalsifiable — the
  accepted simplify finding restored it as a name-level `reflect` check (mutation-checked red).
- `TestNewClientRejectsInvalidConfiguration` stays; its `NewOperatorClient` empty-token assertion
  re-homes as `TestNewOperatorClientRejectsInvalidConfiguration`, which additionally drives the six
  base-URL rejection rows through the moved `parseBaseURL` copy so the copy keeps a rejection arm on
  its own. The machine rows are unchanged.
- `TestPausedRunAndTaskValidation` splits: `TestPausedTransitionValidation` keeps the run/transition
  leg in `control` against the retained `validateTransitionResponse`; `TestPausedTaskValidation`
  moves the two task legs verbatim to the operator package.
- Cases whose subject is a moved validator move: all of `client_test.go`'s and
  `client_review_test.go`'s operator cases and the `supervisory_validation_test.go` cases listed
  below.

**Supervisory retarget (`supervisory_validation_test.go`, retained validators).**
`TestSupervisoryCommandRequestAndDispatchPayloads` keeps its 17 rows and its dispatch leg
(`validateCommands`, byte-identical), and the request leg becomes `ValidateSupervisoryPayload`
guarded by the retained `isSupervisoryCommand` (for every supervisory row the two verdicts agree —
checked row by row; the `teleport` row's rejection is carried by the dispatch leg, which is where
the retained validator rejects an unknown kind). The tail assertions are re-expressed on the
retained validator: the missing-generation refusal as a dispatch command with `Generation: 0`
(rejected by `validateCommands`'s identifiers/generation/kind/issued_at guard), and the two legacy
request shapes as their dispatch equivalents (`cancel` carries the empty object the dispatch
validator requires, `provide_input` carries its object). The moved request-leg rows re-home as
`TestSupervisoryCommandRequestValidatesKindPayloadAndGeneration` in the operator package, so the
moved `validateTaskCommandRequest` keeps exactly the coverage it had.
`TestSupervisoryCommandResourceStates` and `TestServerRejectedInputHistoryDoesNotProveAcknowledgement`
stay unchanged (both target `validateTaskCommandResource`, retained).
`TestSupervisoryResponseRequiresRequestedGeneration` moves whole with the `supervisoryResource`
helper copy (its subject, `validateTaskCommandResponse`, moved).

**Retained declarations that must not change.** `validateAcknowledgementResponse` and
`validateTaskCommandResource` stay in `control` byte-identical, together with every helper the
`app.go` → `client.go` → `validation.go` chain reaches; `Option`/`WithMaxResponseBytes` stay because
`internal/control/provider_action_client_test.go:425` and `internal/app/pi_provider_bridge_test.go:409`
use them (A5). The six base-URL rows of the moved configuration test exercise the *copy*; the
production `parseBaseURL` keeps its own coverage through `NewClient`'s rejection rows and the
`/api` default-path assertions of `TestOmOReviewRegressionClientRegistersNativeRuntime`.

**Review fix (P2-1/P3-1, 2026-11-21).** Opus round 1 found that the production
`decodeJSONUnchecked` trailing-value rejection (`daemon/internal/control/client.go:1172-1177`) lost
its only failing arm when the operator rows moved, and that the same sweep had not been run over
the rest of the moved tests. The fix adds the missing production-side cases and proves each one
red under a flipped guard (`-overlay` copies outside the worktree, repository untouched):

- `TestClientRejectsIncompleteSuccessResponses` gains the `trailing value` row (`{} {}` through
  `RegisterSession` → `decodeJSONUnchecked`); mutation `return errors.New(...)` → `return nil` at
  `client.go:1175` reddens `trailing_value`.
- `TestMachineClientRejectsOversizedSuccessResponse` (200 + body over `WithMaxResponseBytes`
  → `requestWithStatus` oversized arm); `if oversized` → `if oversized && false` at
  `client.go:1059` reddens it.
- `TestMachineClientTypedErrorPredicatesMatchCodes` (ownership-lost / terminal-grace / unrelated
  codes through `Dispatch` → `IsOwnershipLost`/`IsTerminalGraceExpired`); flipping either predicate
  to `RateLimited` reddens it.
- `TestMachineClientRetryDelayHonorsExplicitZeroRetryAfter` (`Retry-After: 0` → `RetryDelay` stays
  `(0s, true)`); `seconds >= 0` → `seconds > 0` at `client.go:1267` reddens it.
- `TestClaimRequiresWorkInputPresenceAndObjectShape` (`validateWork` input presence and object
  shape through `Claim`); dropping the `HasField("input")` guard reddens it.
- `TestAcknowledgeCommandRequiresMatchingCommandResource` gains the `unpaired run` row
  (`validateTaskCommandResource`'s paired run_id/generation check on the retained ack path);
  `return errors.New("run_id and generation must be null together")` → `return nil` reddens it.
- `TestValidateTaskCommandResourceRejectsMismatchedExpectedTask` (the `expectedTaskID` branch the
  relocated operator caller used to reach; production callers pass `""`); neutralising the branch
  reddens it.
- `TestSameJSONObjectPreservesNumericPrecision` (retained `sameJSONObject`/`sameJSONValue`/
  `sameJSONNumber` numeric spellings, large-integer separation, nested members); inverting the
  `big.Rat` comparison reddens it, and this closes the residual risk below.

Audit method for the sweep: per-moved-test `-covermode=count` profiles at the base revision versus
one profile of every remaining `control` test at head, keyed by declaration and offset, so a
production block covered only by moved tests is visible. That method found the seven declarations
behind the eight cases above; P2-1's trailing-value arm was still covered at head but no longer
asserted anywhere, so only the mutation exposed it. Every other moved test's production substrate
is covered and asserted by a retained `control` case (transport and redirect policy by
`TestClientExecuteProviderActionRedirectAndCancellationAreSingleAttempt` and
`TestOmOReviewRegressionClientRegistersNativeRuntime`, decode/limit/envelope by the machine and
provider-action tables, `ValidateSupervisoryPayload`/`validateCommands` by
`TestSupervisoryCommandRequestAndDispatchPayloads`, `validateTransitionResponse` by
`TestPausedTransitionValidation`).

**Review fix (P2-1, round 2).** Opus round 2 found the retained ack-path guard at
`validation.go:296` ("acknowledgement fields must be null together") still covered but no longer
asserted by any failing arm: with the guard off, a Control response carrying `acknowledgement_id`
and `acknowledgement_outcome` but `"acknowledged_at":null` reaches the next check and panics on the
nil `*time.Time`. `TestAcknowledgeCommandRequiresMatchingCommandResource` gains the
`partial acknowledgement` row; neutralising the guard (`cond` → `false && (cond)`) reddens it at
`validation.go:299` with that panic (full `control` + `protocol` suites: `ok` clean, `FAIL` under
the flip). The round-1 list above is unchanged; this is the tenth flip.

## Evidence plan

Every gate is foreground and sequential; no deadcode run or measurement runs beside a test suite on
this host.

1. **Base gates (before any edit)**: `gofmt -l .`, `go vet ./...`,
   `GOOS=linux go vet ./e2e/... ./internal/control/... ./internal/protocol/...`,
   `GOOS=linux|windows go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...`,
   `go test -count=1 ./internal/control/... ./internal/protocol/... ./e2e/...`, `git diff --check`.
2. **Two-strategy removal proof** for each of the 20 moved declarations:
   (a) `deadcode -test ./cmd/...` (v0.53.0) for `GOOS=windows GOARCH=amd64`,
   `GOOS=linux GOARCH=amd64`, `GOOS=linux GOARCH=arm64`, run at the baseline revision; the symbol
   must appear in all three findings; (b) `git grep -nw <symbol> -- daemon` at the head revision:
   no reference outside `daemon/e2e/internal/operator/**`, with the baseline reference inventory
   recorded to show where each reference went. Disagreements are reported, not smoothed.
3. **Declaration-set proof**: a Go program (kept outside the worktree) parses the baseline blobs
   (`git show <base>:<path>`) and the head files with `go/parser`, prints every top-level declaration
   with `go/printer`, and reports (i) every declaration placed in the new package byte-identical to
   its original, (ii) the two remaining declarations (if any) differing only by the sanctioned
   package qualifiers, (iii) every declaration retained in `control`/`protocol` byte-identical to
   base, (iv) the removed set equalling the intended 20.
   As implemented, the qualifier normalization covers the `protocol.`, `contracts.` and `control.`
   token prefixes (the third is the machine `Client` type a moved test helper must qualify), prints
   the go/printer diff of every declaration that is not byte-identical, and compares every
   base↔head counterpart of the six `control`/`protocol` files, `client_test.go` included:
   re-measured at the review-fix revision, of 315 retained declarations 310 are byte-identical and
   5 deliberately changed — the two mixed-table splits (`TestClientProtocolRequests`,
   `TestClientRejectsIncompleteSuccessResponses`) whose operator rows re-home, the two retargets
   (`TestNewClientRejectsInvalidConfiguration`,
   `TestSupervisoryCommandRequestAndDispatchPayloads`), and the two review-fix rows added to
   `TestAcknowledgeCommandRequiresMatchingCommandResource` (`unpaired run`, `partial
   acknowledgement`). That set gains 6 declarations
   (`TestMachineClientOversizedErrorResponseKeepsTypedFallback` and
   `TestPausedTransitionValidation` from the relocation, four from this fix), and this fix adds two
   more in `control/validation_test.go` (`TestSameJSONObjectPreservesNumericPrecision`,
   `TestValidateTaskCommandResourceRejectsMismatchedExpectedTask`).
4. **Witness-diff proof**: for the five e2e files, every changed line must reproduce the baseline
   line after the qualifier mapping is applied; imports may be added/removed only as the relocation
   requires. Reported as per-file counts of qualifier-only changed lines and the exact import deltas.
5. **Head gates** (same commands as 1, after the change) plus the targeted test command; the CI
   `integration` job and the race job are CI-only oracles on this host (no cgo → no `-race`).
6. **Mutation checks** in a throwaway copy outside the worktree for the assertions this slice
   rewrites (supervisory retarget rows): flip the guarded behaviour and show the retargeted test
   red, then restore; and one wiring check that a moved test still fails when its subject's guard is
   flipped (e.g. drop the `invalidResponse` prefix in the operator copy).

## Design trade-offs

1. **Relocation over deletion** (contract): the driver is unreachable from production but its e2e
   callers drive a live daemon; deleting it would force a raw-HTTP rewrite of five witnesses.
2. **Copies over a second implementation or a wrapper layer**: the copied substrate is byte-identical,
   so the operator package cannot silently drift from the machine client's transport semantics; the
   duplication is bounded to one e2e-owned package and disappears with the e2e surface.
   A thin "shared transport with a token parameter" refactor is *not* selected: it would edit the
   production machine client, which P0-1/§clause 4 freeze byte-identical.
3. **Tests move with their subject**: the operator cases exercise the moved code and cannot compile
   in `control` once the driver is gone; splitting mixed tables is the only shape that keeps the
   machine rows where they belong.
4. **Alias over local renames**: see *Exact semantics*; it keeps witnesses qualifier-only.
5. **`RetryDelay`/typed-error predicates copied**: their assertions are part of the operator cases
   (`Retry-After` classification of the copied `decodeAPIError`, `IsOwnershipLost` on a 409), so the
   copies are what the moved tests exercise; production keeps its own and its own tests.

## Out of scope (contract clause 4)

The validator family/unification, the `validateProviderActionUUID` fix and duplicate-member
rejection (12a, landed); `WithMaxResponseBytes`/`Option` removal (A5); `pi_provider_bridge_test.go`
(not retargeted, so `daemon/internal/app/**` is untouched); any other `internal/harness/**` or
`internal/app/**` file (P2-5); the `launchLinuxV2Process` stub and F-19's `linux && amd64` branch move
(`internal/execution`, untouched); the machine `Client`/`EnrollmentClient`/transport internals (stay
byte-identical); the goal-envelope family (D5/W6-08); ack stages (W3); W2-13's `internal/harness/**`.

## Residual risks

- `deadcode` reachability is RTA from `./cmd/...` with the tool's documented dynamic-call caveats; the
  `git grep` leg is the independent second strategy, and any disagreement keeps the symbol.
- The relocated package duplicates the transport. A future production transport change will not
  propagate into it; that is accepted because the package exists only for e2e and the copies are
  checked byte-identical at this revision.
- The five witness files cannot run on this Windows host (Linux-gated) and the live scenarios need
  `SYMMETRY_E2E=1` against a Control release; local evidence is compilation (`GOOS=linux go vet`)
  plus the qualifier-only diff proof. The CI `integration` job remains the acceptance oracle for the
  relocated call sites, and the `--assert` names are preserved unchanged.
- The retained `sameJSONNumber` (head `validation.go:383`) numeric branch is unreachable from
  production after the move: both retained call sites (head `validation.go:270`, `:408`) compare a
  payload against the literal `{}`, so no number-bearing value reaches the comparator, and the
  numeric-precision cases moved with `sameCommandPayload` into the e2e package. The helper stays
  because the contract's opus P0-1 requires the retained chain byte-identical. The review fix pins
  its semantics with `TestSameJSONObjectPreservesNumericPrecision`, a retained in-package case over
  `sameJSONObject` (number spellings, `-0`, large-integer separation, nested members), so a future
  change to the retained helper now fails in `control` even though the operator cases still cover
  the moved caller.
