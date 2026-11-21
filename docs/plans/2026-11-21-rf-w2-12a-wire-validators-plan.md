# rf-w2-12a-wire-validators plan — one wire validator family + strict-goal decode parity

Contract: `goal-rf-w2-12a-wire-validators.md` (GDD, 2026-11-21), the dispatched half of program-plan
§5 W2-12 (`<gdd-state>/refactor/program-plan.md:468`, risk low-med, deps W1-06) from findings F-04
(`<gdd-state>/refactor/findings/daemon-protocol-rest.md:297-310`) and F-05 (`:311-335`), reviewed at
`4dd374b` (`w2-contract-review-opus-1.md` P0-1/P0-2/P1-1/P2-5, `w2-contract-review-ds-1.md`
A5-A9). Baseline: this worktree's HEAD `4dd374bdcdbe09a05287e77ad90faaa949fcddcd` — every coordinate
below was read at that revision.

## Inventory at the baseline (the copies this slice collapses)

Five UUID bodies with three different message policies, three short-identifier bodies with one:

| # | symbol | definition | scan | rejection messages (before the field name) | disposition |
| --- | --- | --- | --- | --- | --- |
| 1 | `validateUUID` | `daemon/internal/protocol/goal_subject.go:291` | rune | length/dash/non-hex `must be a canonical UUID`; uppercase `must use lowercase hexadecimal UUID digits`; version `must use a supported UUID version`; variant `must use a supported UUID variant` | keeps its callers; delegates to the primitive with policy **A** |
| 2 | `validateRegistrationUUID` | `daemon/internal/protocol/protocol.go:136` | rune | every class `must be a canonical UUID` | delegates, policy **B** |
| 3 | `validateCanonicalUUID` | `daemon/internal/config/config.go:317` | rune | every class `must be a canonical UUID` | delegates, policy **B** |
| 4 | `validateGoalUUID` | `daemon/internal/control/validation.go:1123` | byte | length/dash `must be a canonical UUID`; uppercase **and** other non-hex `must use lowercase hexadecimal UUID digits`; version+variant (one condition) `must use a supported UUID version and variant` | delegates, policy **C** |
| 5 | `validateProviderActionUUID` | `daemon/internal/control/client.go:1454` | byte | length/dash `must be a canonical UUID`; uppercase and other non-hex `must use lowercase hexadecimal UUID digits`; **no version/variant check** (F-04's gap) | delegates, policy **C** → the one behaviour change |
| 6 | `validateShortIdentifier` | `daemon/internal/protocol/goal_validation.go:48` | rune | `must contain 1..128 characters`; `has an invalid identifier` | canonical body; new exported wrapper delegates to it |
| 7 | `validateNativeModel` | `daemon/internal/config/config.go:295` | rune | byte-identical pair to #6 | delegates to #6's wrapper |
| 8 | `validateGoalShortIdentifier` | `daemon/internal/control/validation.go:1164` | rune | byte-identical pair to #6 | delegates to #6's wrapper |

Bodies #2/#3 are textually identical; #6/#7/#8 are textually identical modulo the local
`isASCIIAlphaNumeric` spelling. #4 and #5 differ only in #5's missing version/variant tail.

Class → message mapping the unification must preserve, per policy (message = `"<field> " + body`):

| policy | malformed (length, dash position) | uppercase `[A-F]` | other non-hex | bad version | bad variant |
| --- | --- | --- | --- | --- | --- |
| A (protocol DTO fields) | canonical UUID | lowercase hexadecimal UUID digits | canonical UUID | supported UUID version | supported UUID variant |
| B (registration, config) | canonical UUID | canonical UUID | canonical UUID | canonical UUID | canonical UUID |
| C (control goal machine, provider actions) | canonical UUID | lowercase hexadecimal UUID digits | lowercase hexadecimal UUID digits | supported UUID version and variant | supported UUID version and variant |

Policy C's version/variant share one message because #4 tests both nibbles in one condition; A's are
separate because #1 tests them separately.

## Deliverable shape

1. `daemon/internal/protocol/goal_subject.go` — the single implementation:
   - exported `UUIDMessagePolicy` (five message **bodies**: `Malformed`, `Uppercase`, `NonHex`,
     `Version`, `Variant`; the caller's field name is always the `"%s "` prefix, so a policy cannot
     change it), exported `CanonicalUUIDPolicy` (B) and `GoalMachineUUIDPolicy` (C), unexported
     `protocolUUIDPolicy` (A) with a doc comment naming its call-site family;
   - exported `ValidateUUID(value, field string, policy UUIDMessagePolicy) error` — one byte-based
     scan: length 36, `-` at 8/13/18/23, hex digits elsewhere, version nibble `1..5`, variant nibble
     in `89ab` — the P1-1/D-s1-A7 exported primitive;
   - `validateUUID(value, field string)` keeps its two-argument signature and all in-package callers
     (goal DTO validators) unchanged, delegating with policy A;
   - exported `ValidateShortIdentifier(value, field string) error`, a wrapper delegating to the
     canonical `validateShortIdentifier` body (`goal_validation.go`, untouched) — **no message
     policy parameter**: the three copies pin byte-identical message pairs, so a policy would be
     dead configuration (deviation from the A7 sentence, recorded here; the pinned strings are what
     the clause protects).
2. `daemon/internal/protocol/protocol.go` — validator region only: `validateRegistrationUUID`
   delegates to `ValidateUUID` with `CanonicalUUIDPolicy`.
3. `daemon/internal/config/config.go` — `validateCanonicalUUID` delegates with `CanonicalUUIDPolicy`;
   `validateNativeModel` delegates to `ValidateShortIdentifier`; new `protocol` import (no cycle:
   `protocol` imports only `contracts`); `isASCIIAlphaNumeric` (`:313`) is deleted after the
   two-strategy proof (strategy b below).
4. `daemon/internal/control/validation.go` — `validateGoalUUID` delegates with
   `GoalMachineUUIDPolicy`; `validateGoalShortIdentifier` delegates to `ValidateShortIdentifier`;
   `goalASCIIAlphaNumeric` (`:1228`) deleted after the two-strategy proof; `decodeStrictJSON`
   (`:658`) rejects duplicate members (semantics below).
5. `daemon/internal/control/client.go` — `validateProviderActionUUID` delegates with
   `GoalMachineUUIDPolicy` (all existing call sites unchanged: `:1398`, `:1401`, `:1434`, `:1488`,
   `:1567`); `isHex` (`protocol/goal_subject.go:342`) deleted after the same proof.

Tests (owned set; pin tables first, then the new rejections):

| file | test | shape |
| --- | --- | --- |
| `protocol/goal_subject_test.go` | `TestValidateUUIDPinsRejectionMessages` | table over #1: length, dash position, uppercase, non-hex (lowercase and out-of-range uppercase), version, variant, version+variant precedence, accept |
| `protocol/registration_capabilities_test.go` | `TestValidateRegistrationUUIDPinsRejectionMessages` | same class table over #2 (all-canonical policy) |
| `protocol/goal_validation_test.go` | `TestValidateShortIdentifierPinsRejectionMessages` | length (empty, 129), first-rune, mid-rune, accepted `._:-` set, 128-rune accept |
| `config/config_test.go` | `TestValidateCanonicalUUIDPinsRejectionMessages`, `TestValidateNativeModelPinsRejectionMessages` | same tables over #3 and #7 |
| `control/validation_test.go` | `TestValidateGoalUUIDPinsRejectionMessages`, `TestValidateGoalShortIdentifierPinsRejectionMessages` | same tables over #4 and #8 |
| `control/client_test.go` | `TestValidateProviderActionUUIDPinsRejectionMessages` | the **pre-existing** classes over #5 only (length, dash, uppercase, non-hex, accept); version/variant rows cannot be pinned here because the base accepts them |
| `control/client_review_test.go` (new-rejection tests only) | `TestValidateProviderActionUUIDRejectsBadVersionAndVariant`, `TestRequestGoalWithStatusRejectsDuplicateMembers`, `TestDecodeStrictObjectJSONRejectsDuplicateMembers` | A9's (a) and (b) names |

Pin tables assert the exact `Error()` string per row, so a flipped policy string or a rewired call
site turns the owning table red. They call the unexported validators directly (in-package tests), so
they compile and must pass **on the base source** before the unification — that is the byte-identity
proof, not the source diff.

Docs: this file (`docs/plans/` is the established plan location — #127, #132, W1-09). No
`docs/TODO.md` entry closes: the two open "revalidate and remove the daemon surfaces" entries name
the W2-12 **follow-up** surfaces (`daemon/e2e/**`, the legacy `validation.go:209-629` task validators,
the operator client, `harness.NewClaudeAdapterWithRunner`), which clause 4 assigns to 12b/W3-04, and
the `live — do not remove` bullet is a decision note. Nothing moves to
`docs/archive/CLOSED-TODO.md`. No in-repo document goes stale: `docs/design/protocol.md` documents no
UUID validator and no duplicate-member rule, so the duplicate-member rejection is a local-protocol
strictness change to record in the PR body, not a doc edit.

## Exact edge-case semantics

**Byte scan vs rune scans (policies A/B).** #1/#2/#3 iterate runes but `len(value)`/`value[14]`/
`value[19]` are bytes, so acceptance already implies ASCII; the unified byte scan differs only in
which *index* sees a multi-byte rune, and the first divergence is at the same index: an ASCII prefix
aligns indices, and the first non-ASCII byte/rune fails at the same position with the same class
(`-` position → malformed; otherwise not `A-F`, not hex → non-hex). A string that passes one scan is
pure ASCII, so it passes both. Result: identical accept sets and identical first-error messages.

**Reorderings.** Uppercase is tested before non-hex in every copy and stays first; version precedes
variant (A's distinct messages make that visible, C's shared message is order-insensitive); malformed
beats hex classes (the dash check is inside the loop, left to right).

**`validateProviderActionUUID` (the one behaviour change).** Policy C adds the version `1..5` and
variant `89ab` rejection with the existing combined message `must use a supported UUID version and
variant` — byte-identical text already emitted by #4, so no new string enters the wire vocabulary.
Accepted set change: version/variant-bad UUIDs that the base accepted are now rejected; suffix
classes are unchanged. Safety evidence for durable replay: `deriveProviderBridgeActionID`
(`daemon/internal/harness/pi/provider_bridge.go:1102-1120`) stamps every bridge-minted action ID as
version 5 (`digest[6] = digest[6]&0x0f | 0x50`) with variant `10xx`
(`digest[8] = digest[8]&0x3f | 0x80`), and that stamping is present since the durable-workflow
commit `8e22b42` — action IDs persisted in the durable replay path were always shape-valid.

**Duplicate-member rejection (F-05 parity).** `decodeStrictObjectJSON`
(`control/validation.go:674`) → `decodeStrictJSON` (`:658`) is the strict Goal decode path;
`requestGoalWithStatus` (`client.go:1090-1104`) is its response entry point, while the tolerant
legacy path (`decodeJSON`, `:1240`) already rejects duplicates via `rejectDuplicateJSONMembersOnly`
(`:1232`). Deliverable: after `decodeStrictJSON`'s successful single-value parse (and after its
existing trailing/unknown-field checks), run `protocol.RejectDuplicateJSONMembers` over the same
bytes and return its error unwrapped — the message is
`duplicate JSON object member "<name>" at <path>` (top level `$`, nested `$.item[0]`), exactly the
daemon-side `decodeStrictObject` wording (`protocol/goal_subject.go:266-291`), and
`requestGoalWithStatus` wraps it as `decode strict goal response: …` as it does today.
Ordering is deliberately **after** the decode, not before: every body the base rejected keeps its
exact message and precedence (`json: unknown field …`, `response must contain one JSON value`,
`missing required field …`), and the only verdict change is accepted → rejected for
valid-JSON-with-duplicates. A pre-decode scan would have moved the duplicate message ahead of those
rejections on already-rejected bodies, which clause 3's "changing any pre-existing rejection message"
failure control forbids. The scan is recursive, so nested duplicates are rejected at every depth,
matching `decodeStrictObject`; it runs only on the strict path, so `decodeStrictJSON`'s two callers
(`decodeStrictObjectJSON:688`, `requestGoalWithStatus:1101`) are covered and no other decode changes.
Correct control emits plain Jason maps (`Jason.OrderedObject` only in `request_hash.ex:35`, which
hashes and does not encode responses) [INFERENCE: not exercised end-to-end] — record as a
local-protocol strictness note in the PR body, per P1-1.

## Evidence plan (all gates FOREGROUND; no suite beside a deadcode run on this host)

1. Pins before unification, on the base source: `go test -count=1 -run PinsRejectionMessages
   ./internal/protocol/ ./internal/config/ ./internal/control/` → exit 0 (byte-identity proof, base).
2. New rejection tests on the base source, captured before any source edit:
   `go test -count=1 -run 'TestValidateProviderActionUUIDRejectsBadVersionAndVariant|TestRequestGoalWithStatusRejectsDuplicateMembers|TestDecodeStrictObjectJSONRejectsDuplicateMembers'
   ./internal/control/` → exit 1 with the three named failures (red-on-base for the version/variant
   tightening and both duplicate-member rejections).
3. Head gates, in order (from `daemon/`): `gofmt -l internal/protocol internal/config internal/control`;
   `go vet ./...`; `GOOS=linux go build ./...`; `GOOS=windows go build ./...`;
   `GOOS=linux GOARCH=arm64 go build ./...`; `GOOS=linux go vet ./e2e/...`;
   `go test -count=1 ./internal/control/... ./internal/protocol/... ./internal/config/...`;
   `git diff --check`.
4. Mutation checks in a throwaway copy outside the worktree (deleted afterwards): M1 flips
   `GoalMachineUUIDPolicy.Uppercase` to the canonical body → the #4 pin table goes red while the
   other tables stay green; M2 deletes the duplicate scan from `decodeStrictJSON` → both duplicate
   tests red; M3 restores the base `validateProviderActionUUID` body (no version/variant tail) →
   `TestValidateProviderActionUUIDRejectsBadVersionAndVariant` red. Each red recorded with its exact
   failure line.
5. Two-strategy deletion proof for `isHex`, `isASCIIAlphaNumeric` (config), `goalASCIIAlphaNumeric`
   (control): (a) intersection of `deadcode -test ./cmd/...` for windows/amd64 (host),
   `GOOS=linux GOARCH=amd64` and `GOOS=linux GOARCH=arm64` (toolchain-installed `deadcode.exe`);
   (b) `git grep -nw <name> -- daemon` showing only the definition. A candidate that fails either
   strategy is kept and reported as a disagreement.
6. CI-only oracle (contract): the daemon race job (`go test -race -count=1 ./...`) — not runnable on
   this Windows host (no cgo); the conductor runs it on the PR.

## Design trade-offs

1. **One policy-carrying primitive + thin delegates, not per-call-site exported wrappers.** The
   contract fixes one exported primitive taking the message policy; the five call-site functions stay
   (P0-1 forbids deleting the production-path validators, and their callers are out of the owned
   set), so the diff is a body replacement per copy, not a call-site migration.
2. **Byte-based scan.** It is the shape #4/#5 already use, and it makes multi-byte input fail at the
   same index/class as the rune scans (proof above); storing policies as message *bodies* keeps the
   `"<field> "` prefix out of policy reach.
3. **`ValidateShortIdentifier` without a policy.** All three short-identifier copies pin the same two
   strings; a policy parameter would be unreachable configuration. The byte-identity requirement is
   carried by the pins.
4. **Duplicate scan after the decode.** Chosen over pre-decode parity with `decodeStrictObject` to
   keep every pre-existing rejection message and precedence intact (see semantics); the cost is one
   extra token pass over an already-validated body on the strict path only.
5. **Delete the three helpers, keep every validator.** `isHex`, config's `isASCIIAlphaNumeric` and
   control's `goalASCIIAlphaNumeric` become unreferenced once the bodies collapse; they are the
   slice's only deletion candidates and are deleted only on the two-strategy proof, with both proofs
   recorded. No production-path validator, guard or call site is deleted.

## Out of scope (contract clause 4)

- The operator client, the legacy task validators (`control/validation.go:209-629`) and the v1
  `Task`/`TaskWaiting` decoders — 12b.
- The two remaining UUID copies `harness/pi/provider_bridge.go:976` (`validateProviderBridgeUUID`,
  own wording) and `state/goal_session.go:1988` — W2-13/W2-09 follow-ups, not edited here.
- Root probe delegation and `NewClaudeAdapterWithRunner` (W3-04); the goal-envelope family (D5/W6-08);
  ack stages (W3); harness shared scaffolding (W2-13); `TaskSubmitRequest`/`TaskCommandRequest`/
  `TaskCommand` (stay in `protocol`).
- `decodeStrictJSON`'s sibling tolerant path `decodeJSON`: already rejects duplicates; no change.

## Residual risks

- The version 1..5 bound rejects RFC 9562 v6/v7/v8 UUIDs on the provider-action path. That is the
  contract's fail-closed tightening (base accepted them); the v5-stamping citation above is the
  durable-replay safety argument, not a proof for foreign-minted IDs — no caller in the tree mints a
  non-1..5 provider-action UUID (the only minter is `deriveProviderBridgeActionID`).
- `deadcode` reachability is RTA from `./cmd/...` with the tool's documented dynamic-call caveats;
  the `git grep` leg is the independent second strategy, and a disagreement keeps the symbol.
- The duplicate-member rejection is not exercised end-to-end against a live control plane on this
  host (no Control stack started); the parity claim rests on the two named tests plus the Jason
  emission audit cited above.
