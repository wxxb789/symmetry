# go-canonical-c0-case plan — align the C0 escape case in the canonical JSON both languages hash

Contract: `goal-go-canonical-c0-case.md` (GDD, 2026-11-20). Source: the open `docs/TODO.md`
entry "Align the control-character escape case in the canonical JSON bytes both languages
hash." Baseline for every coordinate in this plan: `origin/main` at
`fdf332dcbcc8c8dc2e59f69703972ec3578c74de` (`fdf332d`, this worktree's HEAD, branch
`fix/go-canonical-c0-case`).

## Deliverable shape

1. `daemon/internal/protocol/goal_subject.go` — the escape normalizer that already rewrites
   `U+2028`/`U+2029` to raw UTF-8 also emits every surviving C0 escape with uppercase hex
   digits; one scanner, one rule set, renamed and documented together.
2. `daemon/internal/protocol/goal_subject_test.go` — new regression pins
   `TestCanonicalizeJSONEscapesControlCharactersLikeTheControlPlane` and
   `TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest` (the reachable
   plan-proposal seam, anchored to the control-plane digest the frontend pin already records;
   both fail before, pass after).
3. `docs/design/protocol.md` — the normalization rule (currently silent on the C0 case) names the
   spelling.
4. `docs/TODO.md` → `docs/archive/CLOSED-TODO.md` — the entry moves with the closing branch, the
   decision and the pinning test names (AGENTS.md TODO rule).
5. This plan. No Elixir, TypeScript or contract-layer change; no new fixture file (reasons below).

## The decision the entry leaves open, and why it is settled

The entry names two options and does not pick a winner. Both readings of the second option
require changing what the control plane hashes, which this contract excludes. Evidence:

- **Option (a), the entry's first: Go adopts the spelling the other two implementations already
  share (uppercase `\u00XX`, i.e. Jason's bytes).**
- **Option (b), the entry's second: "a shortest-escape normalization written into the rule
  itself".** Read as "keep Go's current spelling and write it into the rule", it requires a
  contract-layer change and a `RequestHash` change (below), which clause 4 places out of scope;
  read as "write a rule both sides follow", it names no spelling and therefore cannot be
  implemented without making the same choice (a) makes.

The repository pins (a) three times over; (b) would contradict all three:

- `contracts/ts/semantics.ts:63-80` documents `encodeJsonString` as "exactly as Elixir `Jason`
  encodes it" and the boundary test "canonical JSON escapes control characters exactly as the
  control plane's Jason does" (`contracts/scripts/boundary.test.mjs:59-71`) pins the uppercase
  text `{"items":[{"description":"before\u000Bafter\u001Bend"}],"schema_version":"symmetry.plan.v1"}`
  (U+000B/U+001B inside a schema-valid LongText description). Changing that text is a contract
  change the entry does not require.
- `frontend/src/features/goals/goal-attention.test.tsx:782-790` pins the control plane's own
  digest for a control-bearing plan document —
  `sha256:6a41b487e2e260e048b11ea7e3221f53aa9705fc5a9d943a353e6b8c39d202e5` over the
  uppercase-escaped text — computed with this worktree's portable Elixir toolchain and
  `RequestHash.canonical/1`. Changing the Elixir bytes invalidates that pin.
- `docs/design/protocol.md:316` — "Preserve existing versioned RequestHash behavior for old
  commands; do not change it opportunistically to match snapshot hashing" — makes a `RequestHash`
  change a design-decision change, not a routine one.

Direction of the seam: Control stamps the snapshot `content_hash` with Jason
(`control/lib/symmetry_control/goals.ex:2602-2606` via `RequestHash.canonical/1`) and the daemon
recomputes it with Go and refuses on mismatch (`daemon/internal/control/validation.go:1744-1772`;
the same rule at `daemon/internal/protocol/goal_schema.go:48-70`). The daemon is the consumer;
the entry's own analysis states "the Go canonicalizer is the side that disagrees with the other
two, and it is unpinned". Aligning Go therefore retires an unpinned behavior, and no recorded
Go-side digest depends on the lowercase bytes (evidence below).

**Decision (recorded in the archive entry): Go adopts the control plane's uppercase spelling; the
Elixir and contracts encoders stay the reference; the normalization rule text names the case.**

## Exact semantics the contract demands

- **Change.** In the existing escape-token scanner, a C0 control's escape token (00-prefixed) that
  is not `U+2028`/`U+2029` is emitted with its four hex digits uppercased (`\u001f` → `\u001F`);
  every other four-digit token keeps its bytes, so the rewrite stays inside the rule's C0 scope.
  The comment records the rule: the control plane's `Jason` encoder spells C0 controls that way,
  and the canonical form is the pair's shared one.
- **Affected values.** Strings containing `U+000B`, `U+000E`, `U+000F`, `U+001A`-`U+001F` (the
  characters whose escape carries a hex letter). `U+0000`-`U+0007` and `U+0010`-`U+0019` carry
  digit-only escapes and are byte-identical before and after.
- **Unchanged.** Shorthand escapes (`\b` `\t` `\n` `\f` `\r`), `\"`, `\\`, `U+2028`/`U+2029` raw
  UTF-8, literal `\\u001f` escape text (the scanner's token rule), key order, arrays, numeric
  handling including negative zero.
- **Self-consistency.** Both input spellings (`\u001F`, `\u001f`) still canonicalize to one form;
  only the resulting form moves to Jason's bytes.
- **Reachable effect.** A context snapshot whose hashed text carries one of the affected
  characters now verifies against the control plane instead of being refused
  (`context.content_hash does not match canonical snapshot`).
- **Recorded digests.** No repository-asserted digest or fixture over affected bytes is
  invalidated: before this change, a case-insensitive search for an escape of one of the nine
  hex-letter C0 controls (U+000B, U+000E, U+000F, U+001A-U+001F) over `daemon/**`,
  `contracts/fixtures/**` and `contracts/scripts/**` matched only the boundary test's uppercase
  pin, and no fixture or testdata file carries a raw U+000B/U+000E/U+000F/U+001A-U+001F byte; the
  only control-bearing canonical artifacts in the tree are the uppercase pins above, and they
  stay valid. Two recorded digest classes outside that scope are superseded rather than reused:
  the pre-fix probe digest the archive entry quotes (its value canonicalizes to other bytes now),
  and persisted daemon-side digests over affected bytes such as
  `ProviderActionIntent.RequestDigest` (`daemon/internal/app/pi_provider_bridge.go`, replayed at
  `daemon/internal/state/provider_action.go`), where a pre-fix journal fails closed with
  `local_idempotency_conflict` and no duplicate external effect.

## Evidence plan (commands, output and exit codes recorded in answer.md)

1. Pre-fix, new tests red: `go test ./internal/protocol/ -run
   'TestCanonicalizeJSONEscapesControlCharactersLikeTheControlPlane|TestCanonicalizeJSONPlanProposalHashMatchesTheControlPlaneDigest'
   -count=1` over the reverted normalizer (the file restored to its HEAD bytes; the working-tree
   hash is compared before and after the revert).
2. Pre-fix, the entry's reproduction with scratch probes (deleted before the gates): a scratch Go
   test over `CanonicalizeJSON` and a Jason 1.4.5 probe over the same values, recording bytes and
   `sha256` digests for `{"value":"<U+001F>"}`, the full C0 range and the boundary document.
3. Implement (source, test, docs, archive).
4. Post-fix: the new tests green; both probes byte-equal (`{"value":"\u001F"}` /
   `sha256:520bd0e5...`; boundary text / `sha256:401bf74e...`; the plan-proposal text /
   `sha256:6a41b487...`, the frontend pin's digest).
5. Gates: `gofmt -l` over changed files (empty); `go vet ./...` and `go test ./...` in `daemon/`
   on Windows (go1.27.4); the same vet/test on WSL Linux (go1.27.0 from the cached toolchain,
   `GOTOOLCHAIN=auto`). Full suites run one at a time (no measurement harness in this increment).
6. `git diff --check`; no host path in the added lines.

## Out of scope (with reasons)

- **Elixir `RequestHash`, the contracts TypeScript encoder and their tests.** They are the
  reference the daemon must reproduce; changing them is the contract-layer change clause 4
  excludes and would break their own pins.
- **`daemon/internal/state/provider_action.go:445-495`,** which keeps its own copy of the
  line-separator normalizer: those bytes are used only for daemon-internal semantic equality and
  validation ("It is used only for semantic validation and equality; the original raw
  representation remains durable", `:270-276`), with no cross-language hash consumer.
- **A new shared fixture file under `contracts/fixtures/`.** It would drag the manifest, schema
  and `contracts:check` corpus for a two-line pin. The shared fixture this change needs is the
  identical document bytes plus recomputed digest, pinned in the Go test and already pinned by
  `contracts/scripts/boundary.test.mjs:59-71`.
- Other TODO entries, unrelated refactors, and backward compatibility (not required; first
  release not shipped, and no repository-asserted digest or fixture changes; superseded
  daemon-side journal digests fail closed, as "Recorded digests" above records).

## Trade-offs and residual risks

- One scanner with two escape rules (line separators, hex case) versus a second pass: one pass
  keeps the token-scanning invariant that literal `\\uXXXX` text survives in one place; the cost
  is a renamed function and a comment that must name both rules.
- The end-to-end refusal (a Control snapshot carrying U+001F in a LongText description now
  accepted by the daemon) stays source-traced; what is executed is the cross-language byte and
  digest equality on both sides over the same inputs.
- The Jason ground truth is Jason 1.4.5 code (the checkout pinned by `control/mix.lock`), run on
  WSL Elixir against beams built by this host's portable toolchain; the escape function is Jason's
  generated code, not Elixir-version behavior.
