# rf-w3-07-state-error-causes evidence (2026-11-21)

Contract: `goal-rf-w3-07-state-error-causes.md` (GDD, 2026-11-21) with the conductor reconciliation
(A6/P2-1–P2-3/P3, D1–D6) and the program-wide "lesson 32" control-flow rule.
Plan: `docs/plans/2026-11-21-rf-w3-07-state-error-causes-plan.md`.
Base revision: `52bdffd25e46753136cf892a70cfa05e43bc01c8` (`refactor(state): decompose the journal
validators (#165)` — W2-09 merged, so the contract's dispatch gate is met by the branch point).
Delivered tree: the base commit plus the uncommitted working-tree changes listed below; every
`<file>:<line>` in this record resolves in that tree unless marked "base".

**2026-11-21 — rf-w3-08 apply stage.** The accepted simplify findings S1/S3/S4 (and S7, S9)
modified `state.go`, `provider_action.go` and the app side on top of this record's delivered tree;
§12 is the apply-stage record (applied changes, the re-run sweep, the touched-row cell refresh, the
S7 mutation pair and the apply-tip gates). Sections above keep the fix-round numbers with in-place
notes where the apply stage moved them (§0, §1, §2.1, §2.3, §2.4, §3.1).

## 0. Change inventory

| file | state | lines |
|---|---|---|
| `daemon/internal/state/state.go` | modified | +40 / −19 at the delivered tip `6d10da97` (21 of 46 swept rows); the round-1 tree `01aada59` measured +34 / −19 — the 6-line const block of §10 is the difference (`git diff --numstat 52bdffd2 <rev> -- daemon/internal/state/state.go`) |
| `daemon/internal/state/provider_action.go` | modified | +47 / −33 (11 of 11 swept rows) |
| `daemon/internal/state/state_wrap_test.go` | new | 98 |
| `daemon/internal/state/provider_action_test.go` | modified | +86 (one new test) |
| `docs/plans/2026-11-21-rf-w3-07-state-error-causes-plan.md` | new | — |
| `docs/evidence/rf-w3-07-state-error-causes-2026-11-21.md` | new | this record |
| `docs/TODO.md` | modified | +19 (one dead-code candidate) |

No exported symbol was added, removed, renamed or moved. No new `//nolint` or
`credo:disable` suppression. No condition order, limit value or verdict changed except the two
contract-prescribed splits (serialize arm out of `ErrProviderActionCapacity`; shared conditions
separated into an error arm plus a byte-identical predicate arm).

## 1. Sweep — recorded counting rule, command, before/after output

**Rule.** A *row* is a `return` of a static error inside the body (any depth) of an `if` whose
condition or init tests an *error value* for failure:

- `<err-ish ident> != nil`, `<err-ish ident> != io.EOF`, or `errors.Is(<err-ish ident>, …)` /
  `errors.As(…)`; "err-ish" = identifier contains `err` case-insensitively (`err`, `readErr`,
  `tokenErr`, `refErr`);
- returned expression: `errors.New(…)`, `fmt.Errorf(…)` **without** `%w`, or a bare `Err*`
  sentinel value (conflation candidate).

Excluded by construction: `err == nil` / `err == io.EOF` success branches; predicates with no
error variable (`X.Validate() != nil`, `protocol.RejectDuplicateJSONMembers(v) != nil`,
pointer/field presence); typed classification returns (`&NotFoundError{…}`); bare propagation
(`return err`). Function-literal bodies are swept as fresh scopes. Switch
`case errors.Is(err, …)` arms are not error branches; `grep -nE 'case .*errors\.(Is|As)\('
state.go provider_action.go goal_session.go` returns nothing; the `-all` accounting below
classifies every other static return by class (counts only), and the rows listings in Appendix B
carry each row's condition and returned expression.

**Command** (tool source verbatim in Appendix A; `-selfcheck` validates the tool against a fixture
with a known classification and exits non-zero if the rule drifts):

```
cd daemon/internal/state
sweep state.go provider_action.go                # rows
sweep -predicates state.go provider_action.go    # residual no-cause predicate class
sweep -all state.go provider_action.go           # complete accounting of static error returns
sweep -selfcheck                                 # {"other-branch":2,"pred":1,"row":2}, exit 0
```

**Before (base `52bdffd2`):** `TOTAL reported 57 rows` — `rows 57 (static-new 51,
static-fmt-no-w 0, sentinel-return 6)`: 46 rows in `state.go` (45 `static-new` + 1 conflation at
`:5305`), 11 in `provider_action.go` (6 `static-new` + 5 sentinel conflations at `:267`, `:541`,
`:571`, `:592`, `:613` — all five split in §2.3, none left).

**After (fix-round tree `01aada59`/`6d10da97`):** `TOTAL reported 29 rows` — `rows 29
(static-new 29, static-fmt-no-w 0, sentinel-return 0)`: `sentinel-return` is zero, i.e. every
sentinel conflation is split and no `Err*` value is returned from an error branch any more. The
29 residual rows are listed in §2.4. Verbatim listings with each row's condition and returned
expression: Appendix B.

**After (rf-w3-08 apply stage, 2026-11-21):** `TOTAL reported 28 rows` — `rows 28 (static-new 28,
static-fmt-no-w 0, sentinel-return 0)`. The accepted simplify findings restructure the same three
seams: S1 collapses the two byte-identical trailing-data arms to one (`−1` `row` listing), S3 folds
the three `serialize → %w → size-sentinel` blocks into `serializeRunJournalWithinLimit` (three
`sentinel-return` listings become the helper's one, `−2` `no-branch`), and S4 propagates the
alias-check cause (the `decodeJSONWithLimitAndTags` `pred` listing becomes a `%w` arm, `−1` `pred`).
Raw listing total 532 → 528. The tool was rebuilt from Appendix A and re-run on the post-apply
tree (`-selfcheck` exit 0); exact stdout and the touched-row cell refresh are in §12, and
Appendix B's After block carries the apply-tip listing.

**Accounting (`-all`).** Every static error return in both files is classified:

| class | base | fix rounds | rf-w3-08 apply tip | meaning |
|---|---:|---:|---:|---|
| `row` | 57 | 29 | 28 | err-ish identifier in the branch condition; the only class the rule sweeps |
| `pred` | 197 | 197 | 196 | condition tests calls/fields against nil and mentions no err-ish identifier |
| `other-branch` | 36 | 39 | 39 | inside `for`/`switch`/`select` or a non-error `if`; no error value |
| `no-branch` | 259 | 267 | 265 | no enclosing branch (scalar/state verdicts) |

The counts move only where the split narrowed a branch condition: `pred` is unchanged (197), `row`
57 → 29 (−28), `other-branch` 36 → 39 (+3) and `no-branch` 259 → 267 (+8), i.e. 11 new listing
entries (the size arms that now stand alone, and the split predicate arms whose condition is no
longer shared with an error test); raw total 549 → 532 = −17, which is 28 removed from `row` minus
the 11 added entries. Statement level, of the 28 base rows that left the `row` class: 16 have no
static error return at their site any more — the arm is now a `%w` wrap (the 16 `state.go` WRAP rows
of §2.1 minus the two SPLIT rows `:5479`/`:6373`) — and 12 still return the same message under a
narrowed condition (the 6 capacity sentinels + the 6 SPLIT predicate arms). Two of those 12, the
save-capacity sentinels at base `:571`/`:592`, land on the single shared
`validateProviderActionJournalSize` size arm (head `:600`) because the dedup collapses both loops'
checks, which is why the listing gains 11 entries, not 12. The contract's drafting-time
"39 static + 1 / 7 + 5" does not reproduce at this head: 39/45 differ by the counting rule (the
independent review's same-rule sweep found 45 `state.go` rows, which this rule reproduces exactly
at base), and `provider_action.go`'s drafting-time 7th row is the rule-excluded predicate at
base `:278` (`!json.Valid(result) || protocol.RejectDuplicateJSONMembers(result) != nil`).
Contract Change 4 makes the recorded rule govern; both counts and both listings are in
Appendices A/B, so a reviewer can re-run and re-derive them.

The apply tip changes exactly three listings against the fix-round tree (a normalized
file/class/kind/function/condition/expression multiset diff of the two `-all` outputs): the two
byte-identical trailing-data `row` listings collapse to one (`provider_action.go:298`/`:300` →
`:297`, S1), the `pred` listing at `state.go:5364` becomes the propagated
`fmt.Errorf("decode %s: %w", …)` arm (S4, no longer a static return), and the three
`ErrProviderActionCapacity` sentinel listings of `saveJournalLocked` /
`validateJournalSaveCapacity` / `validateProviderActionJournalSize` become the one
`serializeRunJournalWithinLimit` arm (S3) — `row` 29 → 28, `pred` 197 → 196, `no-branch`
267 → 265, raw listing total 532 → 528.

## 2. Classification of the 57 base rows

### 2.1 WRAP — 20 rows (prefix preserved, cause appended)

| base | head | operation whose cause is appended |
|---|---|---|
| `state.go:913` | `:913` | `json.Marshal` — `encode Linux clone cleanup fence` |
| `state.go:1202` | `:1202` | `releaseStoreLock` — `release state directory lock` |
| `state.go:1212` | `:1212` | `io.ReadFull(rand.Reader)` — `generate daemon instance ID` |
| `state.go:1224` | `:1224` | `io.ReadFull(rand.Reader)` — `generate machine token` |
| `state.go:1270` | `:1270` | `os.Remove` — `remove enrollment intent` |
| `state.go:2316` | `:2316` | `os.Remove` — `delete run journal` |
| `state.go:2335` | `:2335` | `os.ReadDir` — `list run journal directory` |
| `state.go:2365` | `:2365` | `os.Open` — `list run journal directory` |
| `state.go:2386` | `:2386` | `directory.ReadDir` (`readErr`) — `list run journal directory` |
| `state.go:5312` | `:5315` | `store.atomicWrite`/`writeAtomic` — `write run journal` |
| `state.go:5342` | `:5345` | `readLimited` — `read %s` |
| `state.go:5352` | `:5355` | `protocol.RejectDuplicateJSONMembers` — `decode %s` |
| `state.go:5360` | `:5363` | `decoder.Decode` — `decode %s` |
| `state.go:5364` | `:5368` | `decoder.Decode != io.EOF` (guarded arm) — `decode %s` |
| `state.go:5384` | `:5391` | `decoder.Decode != io.EOF` (guarded arm) — `trailing JSON data` |
| `state.go:5486` | `:5498` | `store.atomicWrite`/`writeAtomic` — `write %s` |
| `state.go:5514` | `:5526` | `os.Remove` — `remove run journal temporary file` |
| `state.go:6363` | `:6375` | `json.Unmarshal` — `completed terminal payload is invalid` |
| `provider_action.go:288` | `:295` | `decoder.Token` — `provider action result has trailing data` |
| `provider_action.go:290` | `:298`/`:300`, collapsed to `:297` by the rf-w3-08 apply stage (§12) | `decoder.Token` — `provider action result has trailing data` |

Resolution note (rf-w3-08 apply stage, 2026-11-21). The `base` column resolves at `52bdffd2`.
The `head` column was captured while the slice's fix rounds ran: for the `state.go` rows it
resolves at the round-1 tree `01aada59` (the round-2 const block of §10 adds 6 lines above all of
them, so the delivered tip `6d10da97`/`b1caba11` is +6 — e.g. `release state directory lock` is
`:1208`); the two cells previously recorded as `:5366`/`:5389` did not resolve at that tree and are
corrected here to `:5368`/`:5391`. The `provider_action.go` cells resolve at the same round-1 tree
and were unchanged by round 2. The apply stage republishes the apply-tip cell of every row it
touched in §12 (and rewrote two of them: the trailing-data collapse and the shared serializer).

### 2.2 SPLIT — 6 rows (error arm wraps; predicate arm keeps the byte-identical message)

| base | head | wrapped arm | unchanged arm |
|---|---|---|---|
| `state.go:5479` | `:5488`/`:5491` | `encode %s: %w` | `errors.New("encode " + resource)` |
| `state.go:6373` | `:6385`/`:6388` | `completed terminal task result kind is invalid: %w` | same message, no cause |
| `provider_action.go:284` | `:288`/`:291` | `provider action result must be a JSON object: %w` | same message, no cause |
| `provider_action.go:358` | `:368`/`:371` | `provider action result array is invalid: %w` | same message, no cause |
| `provider_action.go:368` | `:381`/`:384` | `provider action result object key is invalid: %w` | same message, no cause |
| `provider_action.go:382` | `:398`/`:401` | `provider action result object is invalid: %w` | same message, no cause |

### 2.3 SPLIT-CAPACITY — 6 rows (contract Change 2)

Head cells cite the `return` statements (serialize arm / size arm), the same convention as §2.1,
§2.2 and the rows listings in the plan and Appendix B.

| base | head | serialize arm | size arm |
|---|---|---|---|
| `state.go:5305` | `:5305`/`:5308` — apply tip: the shared `serializeRunJournalWithinLimit` (`provider_action.go:594`/`:597`, §12) | `serialize run journal: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:267` | `:268`/`:271` | `serialize provider action result: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:541` | `:560`/`:563` — apply tip `:557`/`:560` (§12) | `serialize provider action intents: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:571` | `:597`/`:600` — apply tip: the shared `serializeRunJournalWithinLimit` (`provider_action.go:594`/`:597`, §12) | `serialize run journal: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:592` | `:605` → `:627` (shared `validateProviderActionJournalSize` on the reserved copy; serialize arm `:624`) | `serialize run journal: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:613` | `:624`/`:627` — apply tip: the shared `serializeRunJournalWithinLimit` (`provider_action.go:594`/`:597`, §12) | `serialize run journal: %w` | `ErrProviderActionCapacity` |

Second verdict consequence, stated and pinned: `errors.Is(err, ErrProviderActionCapacity)` is now
false for a serialize failure, so `daemon/internal/app/pi_provider_bridge.go:67` classifies it
`local_persistence_unknown` and calls `RecoverProviderAction` instead of returning
`local_action_rejected`. Genuine size/limit cases still return the sentinel *value*
(`TestProviderActionCapacitySplitSeparatesSerializationFromSize` asserts `err ==
ErrProviderActionCapacity`, not merely `errors.Is`), so the out-of-package `errors.Is` consumer is
unchanged for capacity.

### 2.4 Residual — the 29 delivered rows (excluded by the recorded counting rule)

The heading states the rule's boundary, not a claim that no cause exists anywhere in the branch:
the rule keys on an *err-ish identifier* in the condition, so call-shaped predicates whose own
failure is dropped (`protocol.RejectDuplicateJSONMembers(result) != nil`) sit in the `pred` class,
and the 25 verdict cascades below do carry a cause from the `X.Validate()`/`parseX` call in their
condition — they are residual because the returned message is that stage's verdict, not an
operation failure. The other named call-shaped predicate,
`rejectCaseInsensitiveJSONTagAliases(data, rootType) != nil`, *did* drop a real cause; the
rf-w3-08 apply stage (finding S4) rewrote that arm to `if err :=
rejectCaseInsensitiveJSONTagAliases(data, rootType); err != nil { return fmt.Errorf("decode %s:
%w", resource, err) }`, so it no longer sits in this class and the `state.go:5384`/`:5391` wrap of
§2.1 is now reachable through the production read path (§3.1 audit, §12).

Verdict cascades (25, `state.go`) — the error value comes from a validation/derivation call over
control state and the message is that stage's verdict: `:1241`, `:1289`, `:1422`, `:1464`,
`:1539`, `:1707`, `:1796`, `:1800`, `:1960`, `:2014`, `:2045`, `:2397`, `:2666`, `:3035`, `:3046`,
`:4860`, `:4904`, `:5030`, `:5052`, `:5157`, `:5181`, `:5196`, `:5294`, `:6073`, `:6096`. Four of
them are additionally pinned byte-exactly by W2-09's first-error pins, with the pin's case
reaching the error arm: `:6073`/`:6096` (containment-authority wants `state_test.go:2532`, `:2540`,
`:2548` — `&authority.Supervisor{}` at `:2530`, then `testSupervisorAuthority(...)` with a
TargetPID / target-identity mismatch at `:2538`/`:2546`; containment-handoff want `:2573` —
`&authority.SupervisorHandoff{}` at `:2571` — whose case is the one reaching `:6096`, while the
want at `:2585` takes the byte-identical `:6093` sibling; the pins compare with
`err.Error() != test.want` at `:2914`) and `:5030`/`:5196`
(`linux_clone_journal_test.go:2043`, cases "reuse subject mismatch" / "cleanup intent invalid").
Wrapping those four would change a byte-pinned verdict, which clause 1 forbids; the other 21 have
the same shape (validation/derivation call + state predicates → stage verdict).

Nil-cause arms (4) — arms that run only when no cause exists, so a `%w` verb is impossible without
inventing one: `state.go:5370` and `:5393` (the `err == nil` arms of the guarded
`Decode(&extra) != io.EOF` splits: a *second successfully decoded value* is trailing data with no
error; apply tip `:5375`/`:5398`), `provider_action.go:298` and `:300` (the `tokenErr == nil` arms
of the trailing-data block: a trailing token with no decoder error; the apply stage collapses the
two byte-identical arms into the single `:297` return, §12).

## 3. Control-flow preservation audit (base → head, lesson 32)

Every early return, fall-through and error short-circuit touched by this slice, with its head
equivalent. "Preserved" means: the same condition is evaluated in the same order, and every base
path maps to a head path with the same outcome and the same message prefix.

| condition (head lines) | base | head | preserved |
|---|---|---|---|
| `state.go:5303-5309` | `err != nil \|\| len(data) > maxJournalFileBytes` → sentinel | `err != nil` → wrapped; `len(data) > …` → sentinel | yes — `err` first, as in `\|\|`; size arm byte-identical |
| `state.go:5365-5371` | `Decode(&extra) != io.EOF` → static | same condition; `err != nil` → wrapped, else static | yes — condition unchanged, one nested arm added |
| `state.go:5388-5394` | `Decode(&extra) != io.EOF` → static | same condition; `err != nil` → wrapped, else static | yes |
| `state.go:5486-5492` | `err != nil \|\| len(data) > limit` → static | `err != nil` → wrapped; `len(data) > limit` → static | yes — limit arm byte-identical |
| `state.go:6384-6389` | `err != nil \|\| !validTaskResultKind(kind)` → static | `err != nil` → wrapped; `!valid(kind)` → static | yes — kind arm byte-identical |
| `provider_action.go:267-272` | `err != nil \|\| len(persisted) > maxProviderActionResultBytes` → sentinel | `err != nil` → wrapped; size → sentinel | yes |
| `provider_action.go:287-292` | `err != nil \|\| value.kind != object` → static | `err != nil` → wrapped; kind → static | yes |
| `provider_action.go:294-301` | `tokenErr != io.EOF` → {`tokenErr == nil \|\| token != nil` → static; static} | `tokenErr != io.EOF` → {`tokenErr != nil` → wrapped; `token != nil` → static; static} | yes — every base path keeps an error with the same prefix; only the `tokenErr != nil` case gains the cause |
| `provider_action.go:366-372` | `err != nil \|\| end != ']'` → static | `err != nil` → wrapped; delimiter → static | yes |
| `provider_action.go:379-385` | `err != nil \|\| !ok` → static | `err != nil` → wrapped; `!ok` → static | yes |
| `provider_action.go:396-402` | `err != nil \|\| end != '}'` → static | `err != nil` → wrapped; delimiter → static | yes |
| `provider_action.go:557-566` | provider pre-check → inline reserve loop → `validateProviderActionJournalSize(reserved)` | provider pre-check → `validateProviderActionJournalSize(reserveWorstCaseCompletions(journal))` | yes — pre-check stays an explicit extra step; the loop body is moved verbatim (`:573-588`); the final call is unchanged |
| `provider_action.go:594-606` | serialize → sentinel; `!hasUnresolvedProviderActions` → nil; inline reserve loop; `reservedData` serialize → sentinel; nil | serialize → wrapped; size → sentinel; `!hasUnresolvedProviderActions` → nil; shared size check on the reserved copy | yes — early return, limit and sentinel unchanged; only serialize failures leave the sentinel |
| `provider_action.go:621-630` | `err != nil \|\| len(data) > maxJournalFileBytes` → sentinel | `err != nil` → wrapped; size → sentinel | yes |

No `if` condition outside these rows and the 21 wraps is edited; the 25 residual rows and their
conditions are byte-identical to base (`git diff` shows no hunk touching them).

### 3.1 Apply-stage control-flow audit (rf-w3-08 apply, 2026-11-21)

The three accepted findings that restructure code rather than delete it, base (`52bdffd2`) → apply
tip, with every early return / fall-through / error short-circuit mapped:

| condition (apply-tip lines) | base | apply tip | preserved |
|---|---|---|---|
| `state.go:5360-5364` (S4, alias-check arm) | `rootType != nil && rejectCaseInsensitiveJSONTagAliases(data, rootType) != nil` → static `errors.New("decode " + resource)` | `rootType != nil` → `if err := rejectCaseInsensitiveJSONTagAliases(data, rootType); err != nil` → `fmt.Errorf("decode %s: %w", resource, err)` | yes — same two conditions in the same order, prefix `decode <resource>` unchanged, cause appended; the only observable change is the cause suffix, which is the contract's prescribed change class (D1). The call's own wrap (`:5396`/`trailing JSON data`) is now reachable through this path |
| `provider_action.go:293-298` (S1, trailing-data block) | `tokenErr != io.EOF` → {`tokenErr == nil \|\| token != nil` → static; static} | `tokenErr != io.EOF` → {`tokenErr != nil` → wrapped `:295`; static `:297`} | yes — the two base static arms return byte-identical errors, so merging them cannot change any outcome; the token value was only read by the removed disjunct and is now discarded; the `tokenErr != nil` wrap is unchanged |
| `state.go:5309` → `provider_action.go:591-600` (S3, `serializeRunJournalWithinLimit`) | three inline blocks, each `serialize → fmt.Errorf("serialize run journal: %w") → len(data) > maxJournalFileBytes → ErrProviderActionCapacity`: `saveJournalLocked` (state.go), `validateJournalSaveCapacity` and `validateProviderActionJournalSize` (provider_action.go) | one helper (`provider_action.go:591-600`): serialize error → wrapped (`:594`), over-limit → sentinel value (`:597`); `saveJournalLocked` → `data, err := helper; if err != nil { return err }` (`state.go:5309-5312`) and still writes `data`; `validateJournalSaveCapacity` → `if _, err := helper; err != nil { return err }` then the `hasUnresolvedProviderActions` / reserved-copy branches unchanged; `validateProviderActionJournalSize` → `_, err := helper; return err` | yes — per site the order (serialize before size), the prefix, the sentinel *value* (still pinned by `err == ErrProviderActionCapacity` in `provider_action_test.go`) and every site-specific follow-up are unchanged; the difference is only that the bytes are produced once per call by one implementation |
| `provider_action.go:606-614` (S3 follow-up) | `!hasUnresolvedProviderActions` → nil; inline reserve loop; `reservedData` serialize → sentinel; nil | `!hasUnresolvedProviderActions` → nil; `validateProviderActionJournalSize(reserveWorstCaseCompletions(journal))` (`:613`, now via the helper `:629-632`) | yes — same early return and limit; the reserved-copy check keeps its own call |

## 4. Durable text (declared change, contract Change 2/D1 and clause 3)

- Wrapped suffixes reach the outbox payload: `daemon/internal/app/events.go:530-541`
  `queueFailure` sets `payload["error"] = cause.Error()` (`:535`) and the payload is persisted into
  a `protocol.StateTransitionRequest` for Control.
- Wrapped suffixes reach the durable `UncertainReason`:
  `daemon/internal/app/goal_admission.go:1410-1415` builds the reason from `cause.Error()` /
  `closeErr.Error()`; the field is written at `daemon/internal/state/goal_session.go:1086`.
- **W307-R1 (fix round 1): the length check is not text-neutral — it can reject.** No code *parses*
  either string (the only consumer of `UncertainReason` is the length check
  `len(journal.UncertainReason) > 4096` at `goal_session.go:1688` — verified by listing every
  occurrence of the field: `:223` declaration, assignments/clears, `:1688`, `:1720` emptiness
  check — and `payload["error"]` is opaque to the daemon and to Control), but the write-side guard
  `validRequiredString(reason, 4096)` at `goal_session.go:1052-1053` rejects a composed reason over
  the budget with `uncertain launch reason is invalid` *before* the mutation. Measured arithmetic at
  the state boundary (`state_wrap_test.go:TestMarkGoalSessionUncertainReasonBudget` for the budget
  itself, and a scratch measurement for the composition): the abandon composition is
  `"native Goal session did not reach a known stopped state" + ": " + cause.Error()`, the prefix is
  55 bytes and the base cause text `write goal session journal` is 26 bytes, so the base composed
  reason for a goal-session write failure is 83 bytes and is recorded; the head text appends
  `": " + <inner writer cause>`, so the composed reason is `85 + N` bytes for an `N`-byte inner
  cause and flips to rejection at `N ≥ 4012` (measured: `N = 4011` → 4096 bytes accepted, `N = 4012`
  → 4097 bytes rejected; 4096 accepted / 4097 rejected is also pinned by the new budget test). The
  affected compositions are the error-text-fed sites listed in the plan's fix-round note; the
  remedy is a bounded composition in `daemon/internal/app`, which is outside this slice's strict
  owned set (W4-01's file), so the behavior fix is recorded as unmet with the exact paths there.
- Every prefix and every other message stays byte-identical; the two test-visible Contains
  assertions (`daemon/internal/state/goal_session_test.go:281` `decode goal session journal`,
  `daemon/internal/app/harness_test.go:3830` `write run journal`) stay green because only a
  `: <cause>` suffix is appended. A repo-wide literal scan of all 21 wrapped messages against the
  pre-increment test corpus found no test asserting any of them by equality (the only pre-existing
  test literals are those two `strings.Contains` calls); the increment's own `state_wrap_test.go`
  does assert `write run journal: <cause>` and `write goal session journal: <cause>` by equality.
- `state_wrap_test.go` pins the exact wrapped strings for the deterministic lanes:
  `write run journal: <cause>` and `write goal session journal: <cause>` (injected sentinel), the
  decode lane as `strings.HasPrefix(err.Error(), "decode run journal: ")` plus
  `errors.As(err, *json.UnmarshalTypeError)` with `Value == "array"` (hardened in fix round 1: the
  earlier `strings.Contains` + `errors.Unwrap != nil` form passed under a wrong-prefix mutation and
  could not see a wrong cause — §9 mutations), and the serialize lanes as prefix +
  `errors.As(err, *json.SyntaxError)` (the `%w` chain and the cause identity, since the cause text
  belongs to `encoding/json`).

## 5. Gates (all foreground, exact commands)

| # | command (cwd `daemon/`) | exit | observed |
|---|---|---:|---|
| 1 | `gofmt -l .` | 0 | no output |
| 2 | `go vet ./...` | 0 | no output |
| 3 | `GOOS=windows go build ./...` | 0 | — |
| 4 | `GOOS=linux go build ./...` | 0 | — |
| 5 | `GOOS=linux GOARCH=amd64 go vet ./internal/state/` | 0 | — |
| 6 | `go test -count=1 ./internal/state/...` (Windows) | 0 | `ok … 6.485s` at the final tree (pre-mutation run: `ok … 23.630s`) |
| 7 | WSL `Ubuntu-24.04` `accept`: `cd …/daemon && go test -count=1 ./internal/state/...` (go1.27.0 linux/amd64) | 0 | `ok … 13.344s` at the final tree (`ok … 17.381s` pre-mutation) — compiles and runs the `//go:build linux` clone/cgroup suites, including W2-09's equality pins |
| 8 | `go test -count=1 ./internal/app/ -run '^TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure$'` | 0 | `ok … 0.458s` at the final tree (`0.694s` pre-mutation; contract's targeted oracle: Contains + `errors.Is(err, abortFailure)`) |
| 9 | `go test -count=1 ./internal/app/ -run 'PiProviderBridge\|DurablePiProviderBridge\|ProviderAction'` | 0 | `ok … 5.787s` at the final tree (`3.016s` pre-mutation; caller-package subset for the capacity paths) |
| 10 | `go test -count=1 ./internal/app/` (full caller package, run on the fully reverted tree) | 0 | `ok … 196.930s` |
| 11 | `git diff --check` | 0 | no whitespace errors |
| 12 | host-path scan of the diff and the new files (case-insensitive search for the Windows user-profile directory segment and the account name) | 1 | no matches (grep exit 1 = no match); the only hit was this record's own description of the scan, which is now pattern-free |

No `-race` locally: this Windows host has no cgo toolchain, as the contract records; the CI race
job is the oracle for that lane. No measurement harness ran concurrently with the suites.

**Apply-stage re-run (the delivered tree plus the apply-stage simplification edits: the decode
lane's `errors.Unwrap` assertion in `state_wrap_test.go`, 98 lines, and record-only
wording/citation fixes — no production file changed).** All contract gates re-run sequentially at
that revision, no harness concurrent with a suite:

| command (cwd `daemon/`) | exit | observed |
|---|---:|---|
| `gofmt -l .` | 0 | no output |
| `go vet ./...` | 0 | no output |
| `GOOS=windows go build ./...` | 0 | — |
| `GOOS=linux go build ./...` | 0 | — |
| `GOOS=linux GOARCH=amd64 go vet ./internal/state/` | 0 | — |
| `go test -count=1 ./internal/state/...` (Windows) | 0 | `ok … 34.362s` |
| WSL `Ubuntu-24.04` `accept` — `go test -count=1 ./internal/state/...` in the worktree `daemon/` directory (WSL mount path elided) (go1.27.0 linux/amd64) | 0 | `ok … 20.386s` (the `//go:build linux` clone/cgroup suites run) |
| `go test -count=1 ./internal/app/ -run '^TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure$'` | 0 | `ok … 0.563s` |
| `go test -count=1 ./internal/app/ -run 'PiProviderBridge\|DurablePiProviderBridge\|ProviderAction'` | 0 | `ok … 4.678s` |
| `git diff --check` | 0 | no whitespace errors |
| host-path scan of the apply diff | 1 | no matches (grep exit 1 = no match) |
| sweep rebuilt from Appendix A and re-run at this revision (`-selfcheck`, rows, `-predicates`, `-all`) | 0 | `{"other-branch":2,"pred":1,"row":2}`; `rows 29 (static-new 29, sentinel-return 0)`; `pred-class 197`; `row 29 / pred 197 / other-branch 39 / no-branch 267` (532) — every number in §0/§1/§2.4/Appendix B reproduces, and `git diff --numstat` vs `52bdffd2` is still `state.go` +34/−19, `provider_action.go` +47/−33, `provider_action_test.go` +86, `docs/TODO.md` +19 |

## 6. Mutation checks (judging)

### 6.1 (a) wrap test red on base

Detached worktree of the base commit `52bdffd2` with this slice's two test files copied in
(`state_wrap_test.go` new, `provider_action_test.go` = base + the one new test):

```
go test -count=1 -run 'TestSaveJournalWrapsAtomicWriteCause' ./internal/state/
--- FAIL: TestSaveJournalWrapsAtomicWriteCause/run_journal
    state_wrap_test.go:32: SaveJournal() error = write run journal, want the injected writer cause in its chain
--- FAIL: TestSaveJournalWrapsAtomicWriteCause/goal_session_journal
    state_wrap_test.go:61: MarkGoalSessionUncertain() error = write goal session journal, want the injected writer cause in its chain
FAIL (exit 1)

go test -count=1 -run 'TestLoadJournalWrapsDecodeCause|TestProviderActionCapacitySplitSeparatesSerializationFromSize' ./internal/state/
--- FAIL: TestLoadJournalWrapsDecodeCause
    state_wrap_test.go:90: LoadJournal() error = "decode run journal", want the decode prefix plus cause
--- FAIL: …/serialize_failure_leaves_capacity/{dispatch_capacity_guard,journal_size_guard,lossless_mutation_guard}
    provider_action_test.go:1435: guard error = provider action capacity is exhausted, want a serialize failure outside the capacity verdict
FAIL (exit 1)
```

The base run also shows the split's other half *passing* at base
(`size arm returns the sentinel value`), which is what "no verdict changed" means for capacity.

### 6.2 (a) green on head, red under the `%v` flip

```
head:  go test … -run 'TestSaveJournalWrapsAtomicWriteCause'   → ok (exit 0)
flip:  state.go `write run journal: %w` → `%v`
       --- FAIL: TestSaveJournalWrapsAtomicWriteCause/run_journal
           SaveJournal() error = write run journal: injected atomic writer failure,
           want the injected writer cause in its chain          (exit 1)
revert: %w restored → ok (exit 0); `grep -n '%v", err)' state.go provider_action.go` finds nothing
```

Decode seam (`state_wrap_test.go` decode lane, added at apply): the same flip at the other lane's
site, `state.go:5363` (`decode %s: %w`), leaves the message byte-identical, so only the new chain
assertion catches it:

```
flip:  state.go:5363 `decode %s: %w` → `%v`
       --- FAIL: TestLoadJournalWrapsDecodeCause
           state_wrap_test.go:93: LoadJournal() error = "decode run journal: json: cannot unmarshal
           array into Go value of type state.RunJournal", want the decoder cause in its error chain
                                                                                              (exit 1)
revert: %w restored → ok (exit 0); `grep -n '%v", err)' state.go provider_action.go` finds nothing
```

### 6.3 (b) reservation sensitivity — dispatch use

Mutation: `validateProviderActionDispatchCapacity`'s call becomes
`validateProviderActionJournalSize(journal)` (reservation dropped):

```
--- FAIL: TestProviderActionCapacityReservesWorstCaseFailureCodeBeforeDispatch (2.23s)
        PASS: TestProviderActionCapacityFailsBeforeDispatchWhenJournalByteBudgetIsExceeded
        PASS: TestProviderActionCapacityRejectsProviderBytesBeforeDispatch
        PASS: TestProviderActionFailureCodeCapacityUsesWorstCaseJSONEncoding
        PASS: TestProviderActionMutationPreservesUnresolvedCompletionCapacity
FAIL (exit 1)                                                        → reverted, all green
```

**Conductor-ratified (fix round 1, 2026-11-21): only `:615`/`:1257` are reservation-sensitive;
`:479` is a size assertion.** Change 2's original gate list expected the dispatch-use removal to
redden `provider_action_test.go:479` *and* `:615`; measured at this head, only `:615` reddens
(`PASS` lines above). `:479` builds its near-limit fixture by binary search for the smallest payload
at which `journal + one bare intent` already exceeds `maxJournalFileBytes`, so the plain size check
catches that case with or without the reservation; `:479` is therefore a size/budget assertion for
the dispatch path, not a reservation assertion. Both reservation uses are still proven load-bearing
(§6.3 + §6.4), and no test was changed to fit the claim.

### 6.4 (b) reservation sensitivity — save use

Mutation: `validateJournalSaveCapacity`'s call becomes `validateProviderActionJournalSize(journal)`:

```
--- FAIL: TestProviderActionMutationPreservesUnresolvedCompletionCapacity (1.75s)
        PASS: the other five capacity tests + TestProviderActionCapacitySplitSeparatesSerializationFromSize
FAIL (exit 1)                                                        → reverted, all green
```

Measured reservation-sensitive set at the delivered head: `:615` (dispatch use), `:1257` (save use).

### 6.5 Final post-revert verification

Re-run after every mutation was reverted, all foreground and sequential (no harness concurrent
with a suite):

```
gofmt -l .                                                            exit 0
go vet ./...                                                          exit 0
GOOS=windows go build ./...                                           exit 0
GOOS=linux go build ./...                                             exit 0
GOOS=linux GOARCH=amd64 go vet ./internal/state/                      exit 0
go test -count=1 ./internal/state/...            ok …  6.485s          exit 0
go test -count=1 ./internal/app/ -run '^TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure$'
                                                 ok …  0.458s          exit 0
go test -count=1 ./internal/app/ -run 'PiProviderBridge|DurablePiProviderBridge|ProviderAction'
                                                 ok …  5.787s          exit 0
go test -count=1 ./internal/app/                 ok …196.930s          exit 0
wsl Ubuntu-24.04: go test -count=1 ./internal/state/...  ok …13.344s   exit 0
git diff --check                                                      exit 0
```

`git diff --numstat` after the reverts is identical to the pre-mutation numbers in §0
(`state.go` +34/−19, `provider_action.go` +47/−33), and `grep -n '%v", err)'` over both files
finds nothing, so no mutation residue remains.

## 7. Residual risks and limits

- **The verdict-cascade boundary is a judgment call**, defined by the source of the error value
  (validation/derivation call over control state → stage verdict) and corroborated by four
  byte-exact W2-09 pins whose cases reach the error arm. It is mechanical and re-derivable from the
  `-all` class counts (§1) and the rows listings (Appendix B), but a reviewer who prefers wrapping
  `X.Validate()` cascades would have to relax clause 1's "no message … changed" for those pins.
- **The `pred` class (197 static returns) is untouched by contract decision** (Change 1 declares
  "proven no-cause predicates (`X.Validate() != nil` cascades)" residual). The class contains the
  drafting-time `provider_action.go:278` row; all of them are enumerated by `sweep -predicates`.
- **`provider_action.go:267`'s serialize arm is unreachable in production** (its guard requires a
  valid JSON result that re-encodes), so no test drives it; the reachable serialize arms of the
  other two guards are pinned instead. The split is still the correct classification.
- **No `-race` evidence locally** (no cgo on this host). The Linux lane ran in WSL, not on a CI
  runner; CI's race job remains the oracle for that lane.
- **The sweep tool lives outside the repository** (stdlib-only Go, source verbatim in Appendix A)
  because the contract's owned-file set admits no tooling path; the rule, the command and both
  listings are in this record and in the plan, so a reviewer can rebuild the tool from Appendix A
  and re-run it byte-for-byte against the delivered `state.go`/`provider_action.go`.

## 8. Failure boundaries (symmetry-durable-transition) and simplification review

**Enforced invariants, unchanged by this slice.** No external provider dispatch without a durably
prepared journal record (`PrepareProviderAction` persists before returning `dispatch=true`); every
run-journal and state-file write is atomic (`writeAtomic`: exclusive temp file, `Sync`, `Rename`);
a failed write before the rename leaves the previous durable state, a failure or crash after the
rename but before the directory sync leaves the *new* file committed with an error returned (see the
table below), and the caller's error now carries the concrete cause; the 4 MiB journal guard and the
provider-only half-budget keep their exact conditions, limits and sentinel value; capacity verdicts
still come only from size/limit arms.

**Changed boundaries, crash/replay/replay-status review.**

| boundary | crash before commit | after commit, before acknowledgement | replay / unresolved status |
|---|---|---|---|
| `saveJournalLocked` write (`state.go:5315`), before the rename | previous journal remains; error carries the temp-write/fsync cause | n/a | `mutateJournal` recomputes from durable state; unchanged |
| `saveJournalLocked` write (`state.go:5315`), after `renameStateFile` (`writeAtomic`, `state.go:5573`: rename at `:5616`) | n/a — the new journal file is already committed | `writeAtomic` returns `syncDirectory(directory)` (`:5620`; `syncDirectory` = open + `Sync`, `state_unix.go:65-72`) *after* the rename, so a directory-sync failure or a crash between the two steps surfaces as a write error with the new file committed: the caller must treat it as committed-but-unacknowledged and decide by read-back, not by the error | unchanged: `mutateJournal` recomputes from durable state, so the read-back is the truth |
| capacity guard serialize failure (wrap returns: `state.go:5305`; `provider_action.go:268` — unreachable by construction, §7 — `:560`, `:597`, `:624`; the last also through the shared calls at `:565`/`:605`) | no write, no dispatch, no external effect — the guard runs before both | n/a | classification changes: not `ErrProviderActionCapacity` any more, so the bridge reports `local_persistence_unknown` and calls `RecoverProviderAction`, which settles an existing unresolved record and is a no-op when the prepare never committed; the caller receives explicit unresolved status, never an unconditional retry |
| capacity guard size arm (same sites) | no write, no dispatch | n/a | unchanged: the sentinel value itself, so `pi_provider_bridge.go:67` still returns `local_action_rejected` |
| `writeJSONWithLimit` (`state.go:5498`) | previous state file remains (same two-phase window as the row above) | n/a | unchanged atomicity; the app's `UncertainReason`/`payload["error"]` text gains the declared suffix (§4) — and the goal-session journal's own 4096-byte reason budget now interacts with that suffix (§4, W307-R1) |
| read/decode (`state.go:5345`, `:5355`, `:5363`, `:5366`, `:5389`) | read-only | n/a | load-failure classification unchanged; only the field-diagnosability of the message changes |

Fences, leases, revision/claim grants, cancel/completion ordering and provider-owned fields are
not touched (`git diff` contains no fence/lease/claim-grant line); no external effect is retried
anywhere in the changed paths.

**Simplification review (symmetry-simplification-review, scoped to this diff).**

| candidate | decision | reason |
|---|---|---|
| `reserveWorstCaseCompletions` | retained | removes the duplicated ~25-line stamping loop (F9); owns the worst-case completion reservation for every unresolved intent in one place, on the same allocation profile as the inlined loops |
| `validateProviderActionJournalSize` as the shared size check | retained, no wrapper added | each call site needs exactly "serialize + 4 MiB compare"; it is called (not wrapped) by the dispatch loop, the save loop's reserved copy and the completion/recovery seams |
| 11 SPLIT arms (serialize/shape vs predicate) | retained | prescribed by the contract; each adds ~2 lines and removes nothing else; deleting them would either lose the cause or change a verdict |
| guarded `Decode(&extra) != io.EOF` arms (`state.go:5366`, `:5389`) | retained nested | a flatter rewrite (`err != nil && !errors.Is(err, io.EOF)`) would replace the base condition text, which the program-wide control-flow rule keeps byte-identical |
| trailing-data three-arm block (`provider_action.go:294-301`) | retained as split | collapsing the two static arms is a real simplification but changes the base control flow; recorded in `docs/TODO.md` as a follow-up candidate instead |
| removed | the duplicated inline loop body in `validateJournalSaveCapacity` only | no test, type, validation, permission or acceptance condition was reduced; no `//nolint`/`credo:disable` added |

Affected checks after the review and after every revert: §6.5 (all exit 0).

## 9. Fix round 1 (2026-11-21) — W307-R1…W307-R6

Review verdict on `fc8e95a2..fd460635`: `MUST_FIX (5)` (R1 P2 + R2–R5 P3, R6 P3 optional
hardening). This round changed `daemon/internal/state/state_wrap_test.go` (+52/−5) and
`daemon/internal/state/provider_action_test.go` (+3/−2) — the only two non-documentary paths
(measured `git diff --numstat`) — plus these two records, which are updated in place. **No
production file changed**, so §0's prod line counts, §1's before/after counts, §2's classification, §3's
control-flow audit and §6's mutation runs still hold at this head.

**Gates re-run (all foreground, sequential, no harness concurrent with a suite).**

| command (cwd `daemon/`) | exit | observed |
|---|---:|---|
| `gofmt -l .` | 0 | no output |
| `go vet ./...` | 0 | no output |
| `GOOS=windows go build ./...` | 0 | — |
| `GOOS=linux go build ./...` | 0 | — |
| `GOOS=linux GOARCH=amd64 go vet ./internal/state/` | 0 | — |
| `go test -count=1 ./internal/state/...` (Windows, go1.27.4) | 0 | `ok … 35.085s` |
| WSL `Ubuntu-24.04`: `go test -count=1 ./internal/state/...` | 0 | `ok … 29.890s` (the `//go:build linux` clone/cgroup suites run) |
| `go test -count=1 ./internal/app/ -run '^TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure$'` | 0 | `ok … 0.950s` |
| `go test -count=1 ./internal/app/ -run 'PiProviderBridge\|DurablePiProviderBridge\|ProviderAction'` | 0 | `ok … 6.130s` |
| `git diff --check` | 0 | clean |
| host-path scan (`git diff origin/main...HEAD`, working tree diff and both records) | 1 | no matches (grep exit 1 = no match) |
| sweep rebuilt from Appendix A: `-selfcheck`, rows, `-all` at base and delivered | 0 | `{"other-branch":2,"pred":1,"row":2}`; base `rows 57 (static-new 51, static-fmt-no-w 0, sentinel-return 6)` + `row 57 pred 197 other-branch 36 no-branch 259`; delivered `rows 29 (static-new 29, static-fmt-no-w 0, sentinel-return 0)` + `row 29 pred 197 other-branch 39 no-branch 267` — every number in §0/§1/§2.4 reproduces |
| test-case count, `./internal/state` with `-v` (`=== RUN` lines): HEAD `fd460635` (scratch copy from `git archive HEAD`) vs delivered worktree | 0 / 0 | 457 → 458 (the new `TestMarkGoalSessionUncertainReasonBudget`); 390 PASS lines, 0 FAIL in the delivered run |

Not re-run in this round: the full `./internal/app/` package (conductor-owned full suite) — no
production file changed, so its behavior is untouched; the targeted oracle and the provider subset
above re-ran green. No `-race` locally (no cgo on this host; CI race job remains the oracle).

**W307-R1 — behaviour fix unmet inside the strict owned set (measured arithmetic; ready change
set).** The suffix this slice adds reaches the app's composed `UncertainReason` and the store's
4096-byte budget can reject a composed reason the base revision recorded. Measured at the state
boundary with the app's exact composition (scratch measurement in a copy of the package, repo
untouched): the abandon prefix `native Goal session did not reach a known stopped state` is 55 B,
the base cause text `write goal session journal` is 26 B, so the base composed reason is 83 B and is
recorded; the head text appends `": <inner writer cause>"`, the composed reason is `85 + N` B for an
`N`-byte inner cause, and the verdict flips at `N ≥ 4012`: `N = 4011` → 4096 B accepted,
`N = 4012` → 4097 B rejected with `uncertain launch reason is invalid`. The app compositions that can
receive store-grown text: `goal_admission.go:889`, `:950`, `:1410-1416`, `:1468-1474`,
`native_run.go:611`, and `goal_delivery.go:134`/`:138`/`:142` via `native_run.go:846` → store call
`:850`. Both remedies the conductor named
need files outside the strict owned set — the app compositions (`goal_admission.go`, `native_run.go`,
`goal_delivery.go`, W4-01's owned files, plus an app test file) or a clamp at the store sink
(`goal_session.go`) — so the behaviour fix is recorded **unmet with those exact paths**, with the
ready change set: a rune-safe bound applied only to the string passed to
`MarkGoalSessionUncertain` (never to the returned/joined error chains), budget single-sourced from
`state`, plus an app test that drives `abandonGoalSession` with a ~4 KB writer cause and asserts the
session reaches `uncertain` rather than the requeue path. In-set artifact added:
`state_wrap_test.go:TestMarkGoalSessionUncertainReasonBudget` — exactly 4096 B accepted, persisted
and loadable; 4097 B rejected before the mutation with the journal file byte-unchanged; error text
`uncertain launch reason is invalid`. It pins the budget and the no-side-effect property; it cannot
observe the app's composition.

**W307-R6 — hardened assertions and their mutations** (scratch copies of the package, repo
untouched, each mutation reverted):

```
mutation 1: state.go:5363 `decode %s: %w` → `wrong prefix: decode %s: %w`
  --- FAIL: TestLoadJournalWrapsDecodeCause
      state_wrap_test.go:91: LoadJournal() error = "wrong prefix: decode run journal: json: cannot
      unmarshal array into Go value of type state.RunJournal", want the decode prefix plus cause
      (the pre-fix strings.Contains form passed this mutation)
mutation 2: state.go:5363 cause → errors.New("decoder rejected the payload")
  --- FAIL: TestLoadJournalWrapsDecodeCause
      state_wrap_test.go:95: LoadJournal() error = decode run journal: decoder rejected the payload,
      want the array-root *json.UnmarshalTypeError in its chain
      (the pre-fix errors.Unwrap != nil form passed this mutation)
mutation 3: provider_action.go:624 cause → errors.New("journal is not encodable")
  --- FAIL: TestProviderActionCapacitySplitSeparatesSerializationFromSize/serialize_failure_leaves_capacity/journal_size_guard
      provider_action_test.go:1442: guard error = serialize run journal: journal is not encodable,
      want the encoding/json syntax cause in its chain
      (the pre-fix errors.Unwrap != nil form passed this mutation)
```

**Record corrections verified.** (R2) §1's accounting sentence is now the measured one. (R3) the
rule-boundary wording replaces "all proven no-cause"/"only class"; `pred`'s meaning column states
the err-ish test. (R4) both plan output blocks are the tool's exact stdout: the fenced blocks were
re-extracted from the plan and `diff`ed against the recorded run's stdout — identical; the two
`provider_action.go` WRAP rows name `normalizeProviderActionResult`; §2.3's head cells cite return
statements. (R5) §8's `saveJournalLocked` row is split before/after `renameStateFile`
(`writeAtomic` returns `syncDirectory(directory)` at `state.go:5620`), and §4's invariants sentence
no longer claims a failed write always leaves the previous durable state. (P3) conductor
ratification recorded in §6.3 and in the plan's mutation paragraph.

## 10. Fix round 2 (2026-11-21) — W307-R1 remedy (conductor owned-set extension)

Conductor decision 2026-11-21 ("review W307-R1 — owned-set extension, binding"): the composed
uncertain reason is bounded at the app composition sites, rune-safely, with the budget single-sourced
from `state`; the store sink is unchanged and returned/joined error chains stay untouched.
Implemented files and lines (this round's tree):

| file:line | change |
|---|---|
| `daemon/internal/state/state.go:42-46` | `const GoalSessionUncertainReasonBytes = 4096` (single source) |
| `daemon/internal/state/goal_session.go:1052`, `:1688` | write and read enforcement reference the constant (pure constant references) |
| `daemon/internal/app/goal_admission.go:1308-1327` | `boundedGoalSessionUncertainReason`: budget check, then rune-boundary walk (`utf8.DecodeLastRuneInString`, keeping a genuine U+FFFD by its `size > 1`) |
| `daemon/internal/app/goal_admission.go:890`, `:951` | start-failure compositions bounded before `markGoalSessionUncertain` |
| `daemon/internal/app/goal_admission.go:1443`, `:1498` | `abandonGoalSession` / `abandonGoalSessionUncertain` bound `reason` at the store call |
| `daemon/internal/app/native_run.go:611` | `resolveCancelledNativeStartAbortFailure` composition bounded |
| `daemon/internal/app/native_run.go:850` | `publishNativeCloseRetryAfterEvidence` bounds the handoff shared with `goal_delivery.go:134`/`:138`/`:142` |
| `daemon/internal/app/goal_delivery_test.go:640-678` | `TestAbandonGoalSessionBoundsOversizedUncertainReason` |

**Superseded in fix round 3 (PR #200 review, §11):** the helper row's `:1308-1327` span and its
`utf8.DecodeLastRuneInString` walk description, the `:1443`/`:1498` call-site refs (delivered:
`:1437`/`:1492`), and the `goal_delivery_test.go:640-678` test span describe fix round 2's bytes
only. The delivered helper spans `goal_admission.go:1307-1321` and normalizes the over-budget
prefix with `strings.ToValidUTF8`; the delivered tests are `goal_delivery_test.go:640-714`
(`TestBoundedGoalSessionUncertainReason`) and `:716-774`
(`TestAbandonGoalSessionBoundsOversizedUncertainReason`). The "Rune-boundary arithmetic"
paragraph below is likewise fix-round-2 history.

The bound applies only to the string handed to `MarkGoalSessionUncertain`; every returned/joined
error chain, every message prefix and every verdict is unchanged, and the store-side enforcement is
the same check with the same value (now the constant). The helper is a no-op for reasons within
budget (it returns the input string unchanged), so every shorter composed reason stays byte-identical.

**Rune-boundary arithmetic.** The test's composed reason is 4,313 bytes: prefix
`native Goal session did not reach a known stopped state` 55 B + `: ` 2 B + writer cause
`write goal session journal: ` 28 B + 2,100 `é` 4,200 B + `; close: ` 9 B + `native close failed`
19 B. A byte cut at 4,096 lands one byte inside an `é` (the kept prefix ends on its lead byte), so
the boundary walk yields 4,095 bytes and `utf8.ValidString` holds; a naive byte cut would store a
partial rune, which the store's JSON round-trip would corrupt and this round's test would catch.
(Superseded in fix round 3: the delivered bound normalizes the whole over-budget prefix with
`strings.ToValidUTF8` instead of walking back from the cut — see §11.)

**Red run — bound disabled** (temporarily: `boundedGoalSessionUncertainReason` reduced to `return
reason`, its now-unused `unicode/utf8` import removed; repo restored immediately after and the green
run below re-verified the restore):

```
$ cd daemon && go test -count=1 ./internal/app/ -run '^TestAbandonGoalSessionBoundsOversizedUncertainReason$'
--- FAIL: TestAbandonGoalSessionBoundsOversizedUncertainReason (2.33s)
    goal_delivery_test.go:660: oversized uncertain reason left the session off the uncertain path: state.GoalSessionJournal{… LaunchState:"attached" … UncertainReason:"" …}
FAIL
FAIL	github.com/wxxb789/symmetry/daemon/internal/app	2.691s
FAIL
exit 1
```

The journal dump is elided; `LaunchState:"attached"` and `UncertainReason:""` are exhaustive grep
extracts from the raw line. The unbound 4,313-byte reason is rejected by `MarkGoalSessionUncertain`
from the store's budget check, leaving the session off the uncertain path — exactly the W307-R1
regression the remedy closes.

**Green run — bound restored**:

```
$ cd daemon && go test -count=1 ./internal/app/ -run '^TestAbandonGoalSessionBoundsOversizedUncertainReason$' -v
=== RUN   TestAbandonGoalSessionBoundsOversizedUncertainReason
--- PASS: TestAbandonGoalSessionBoundsOversizedUncertainReason (3.10s)
PASS
ok  	github.com/wxxb789/symmetry/daemon/internal/app	3.551s
exit 0
```

The durable reason is 4,095 B, valid UTF-8, keeps the composed prefix and the writer-cause head, and
the session is durably `uncertain` (`LaunchState:"uncertain"`, `SessionState:"unavailable"`,
`NeedsReconciliation()` true).

**Targeted tests and gates** (exact commands, exit codes after each output):

```
$ cd daemon && go test -count=1 ./internal/app/ -run '^TestAbandonGoalSession'
ok  	github.com/wxxb789/symmetry/daemon/internal/app	0.981s
exit 0

$ cd daemon && go test -count=1 ./internal/app/ -run '^(TestAbandonGoalSession.*|TestCancelledNativeStartCloseFailureSettlesBeforeCleanupRetry|TestNativeCloseRetrySuccessDoesNotReleaseObservedTerminalOwner|TestNativeTurnCloseFailureEntersSameDaemonRetry|TestNativeCloseDurableWriteFailuresRetryWithoutLeakingRecoveryBarrier|TestNativeCloseRetrySuccessDoesNotEraseUsageRecoveryBarrier|TestNativeCloseRetrySuccessDoesNotLeaveStaleCloseErrorAsCleanupBarrier|TestFreshCodexNativeStartFailureReportsUncertainCompensationFailure|TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure|TestCancelledNativeStartDiscardAndCloseFailuresJoinAndRetainRetry)$' -v
--- PASS: TestCancelledNativeStartCloseFailureSettlesBeforeCleanupRetry (0.51s)
--- PASS: TestNativeCloseRetrySuccessDoesNotReleaseObservedTerminalOwner (0.00s)
--- PASS: TestAbandonGoalSessionClearsPersistedProcessAfterVerifiedClose (0.19s)
--- PASS: TestAbandonGoalSessionAfterStartTurnResponseLossQueuesStopWhenCloseSucceeds (0.29s)
--- PASS: TestAbandonGoalSessionBoundsOversizedUncertainReason (0.24s)
--- PASS: TestNativeTurnCloseFailureEntersSameDaemonRetry (0.17s)
--- PASS: TestNativeCloseDurableWriteFailuresRetryWithoutLeakingRecoveryBarrier (0.96s)
--- PASS: TestNativeCloseRetrySuccessDoesNotEraseUsageRecoveryBarrier (0.03s)
--- PASS: TestNativeCloseRetrySuccessDoesNotLeaveStaleCloseErrorAsCleanupBarrier (0.33s)
--- PASS: TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure (0.29s)
--- PASS: TestFreshCodexNativeStartFailureReportsUncertainCompensationFailure (0.41s)
--- PASS: TestCancelledNativeStartDiscardAndCloseFailuresJoinAndRetainRetry (0.29s)
PASS
ok  	github.com/wxxb789/symmetry/daemon/internal/app	3.986s
exit 0

$ cd daemon && go test -count=1 ./internal/state/
ok  	github.com/wxxb789/symmetry/daemon/internal/state	15.372s
exit 0

$ wsl -d Ubuntu-24.04 -u accept   (cd into the worktree's daemon directory)
$ go test -count=1 ./internal/state/...
ok  	github.com/wxxb789/symmetry/daemon/internal/state	18.385s
wsl-exit=0

$ cd daemon && gofmt -l internal/app internal/state
(no output) exit 0

$ cd daemon && go vet ./...
(no output) exit 0

$ cd daemon && GOOS=linux go vet ./...
(no output) exit 0    (~117 s)

$ cd <worktree root> && git diff --check
(no output) exit 0
```

**Record cleanliness.** This round replaces §5's Linux-row WSL mount path with a description.
`python scripts/ci/check-record-cleanliness.py --base $(git merge-base HEAD origin/main) --head HEAD`
on the committed head exits 0 (raw output and exit code are recorded in the fix-round findings file
`w3-07-fix-r2.md`, which lives outside this repository's records).

## 11. Fix round 3 (2026-11-21) — review #200 fix record (docs-only round)

PR #200 review round 2 (`200-opus-review-2.md`) verified the P2-1 fix on `f150ed2a` (parent
`848dee6c`) and raised one P3: §10 above still describes fix round 2's helper bytes and test spans.
This section records the delivered bytes; this round changes records only, and the line refs here
resolve at the fix-round-3 head.

- **Helper change** (`daemon/internal/app/goal_admission.go:1307-1321`): the over-budget branch
  returns `strings.ToValidUTF8(reason[:state.GoalSessionUncertainReasonBytes], "")`, dropping every
  invalid or partial-rune byte in the bounded prefix, so the result is valid UTF-8 and cannot
  lengthen in the store's JSON round trip (an invalid byte becomes U+FFFD, 3 bytes, and could push
  the reloaded reason past the read validation at `goal_session.go:1688`). The
  `utf8.DecodeLastRuneInString` tail-only walk and the `unicode/utf8` import are removed; the
  within-budget branch is unchanged (byte-identical), which is the W307-R1 decision's boundary.
- **Tests** (`daemon/internal/app/goal_delivery_test.go`): `TestBoundedGoalSessionUncertainReason`
  (`:640-714`) is a 10-case table with exact string equality (within-budget identity; within-budget
  invalid byte untouched; exact-budget identity ending with a genuine U+FFFD; budget+1 ASCII; cuts
  inside 2-/3-/4-byte runes; interior invalid byte; interior invalid run; invalid bytes past the
  cut), and every over-budget case also asserts `utf8.ValidString`. The extended
  `TestAbandonGoalSessionBoundsOversizedUncertainReason` (`:716-774`) tables the multibyte cause
  and an interior-`\xff` cause, and asserts the session is durably `uncertain` **and**
  `LoadGoalSession` reads the reason back.
- **Red/green** (transcripts in `200-fix-r1.md` §2): against the `848dee6c` tail-only helper the
  new cases fail — table cases at `goal_delivery_test.go:707` (exact-equality, the interior
  invalid byte survived) and the abandon interior case at `:751` (`LoadGoalSession() error =
  invalid goal session journal`) — exit 1; with the `ToValidUTF8` bound both pass, exit 0. Review
  round 2 re-ran the round-1 reproduction on the delivered head: bounded len 4,095, mark accepted,
  load succeeds, session `uncertain`.
- **Deferred store-side hazard** (`docs/TODO.md`, "Deferred review findings (PR #200)"): a
  within-budget reason containing invalid UTF-8 still passes the length-only write check and can
  expand past 4,096 bytes in the JSON round trip, making the journal unreadable; owner: state
  store; base `52bdffd2` has the same issue, and the W307-R1 decision forbids changing the sink in
  this slice.
- **Gates for the unchanged code bytes** (fix-round-3 head): `gofmt -l internal/app internal/state`
  0 · `go vet ./internal/app/ ./internal/state/` (windows) 0 · `GOOS=linux go vet
  ./internal/app/ ./internal/state/` 0 · targeted app subset 13/13 PASS, exit 0 ·
  `git diff --check` 0 · `check-record-cleanliness.py` on the committed head 0.

## 12. rf-w3-08 apply stage (2026-11-21) — accepted simplifications

Apply stage of goal `rf-w3-08-unexport-state-surface` over the range `d0761739..aa43c2f2` (this
record's W3-07 work, W3-08 and the two `origin/main` merges). The finding ids are the simplify
report's; the accepted/not-applied split and the stage gate table are the stage result. No test,
evidence row or documented boundary was deleted — the rows above were refreshed where the apply
stage moved them, and this section republishes every affected number.

Moved (2026-11-22). The split of PR #214 moved this apply-stage set — S1, S2, S3, S4, S7 and the S9
citation refresh — to the W3-07 follow-up branch `refactor/rf-w3-07b-state-error-followups`, which
is where the code now lands; the W3-08 branch keeps only its rename/move slice (the split, the
delivered tests and the branch's own gates are recorded in
[`docs/plans/2026-11-22-rf-w3-07b-state-error-followups-plan.md`](../plans/2026-11-22-rf-w3-07b-state-error-followups-plan.md)).
Cells marked "apply tip" resolve at the pre-split apply tip: on this branch every `state.go` cell
at or below `Store.Close` resolves 3 lines lower and every `provider_action.go` cell at or below
`serializeRunJournalWithinLimit` 2 lines lower (`Close` returns `releaseStoreLock`'s error directly
and the helper's doc comment is two lines), while the `goal_session.go` cells resolve as written;
§12.3's failing assertion resolves at `goal_delivery_test.go:808`. The sweep totals reproduce
unchanged at this branch's head: `rows 28 (static-new 28, static-fmt-no-w 0, sentinel-return 0)`,
`-all`: row 28 / pred 196 / other-branch 39 / no-branch 265, raw listing total 528, `-selfcheck`
exit 0.

### 12.1 Applied changes (all inside the combined W3-07/W3-08 owned sets)

| finding | file(s) | change | apply-tip site |
|---|---|---|---|
| S1 | `daemon/internal/state/provider_action.go` | collapse the two byte-identical trailing-data static arms to one; the token value is discarded | `:293-298` (wrap `:295`, static `:297`), audit §3.1 |
| S2 | `daemon/internal/state/goal_session.go` | delete the callerless `Store.EnsureGoalSessionLineageAvailable` (W1-06 dead-code candidate; −18 lines; TODO entry closed into `docs/archive/CLOSED-TODO.md`) | `CheckGoalSessionLineage` stands alone at `:576-586` |
| S3 | `daemon/internal/state/provider_action.go`, `state.go` | extract `serializeRunJournalWithinLimit` and use it at the three former inline serialize/wrap/limit blocks | helper `provider_action.go:591-600`; callers `state.go:5309-5312`, `provider_action.go:607-609`, `:629-632` |
| S4 | `daemon/internal/state/state.go` | propagate the alias-check cause (`fmt.Errorf("decode %s: %w", resource, err)`) instead of the static `errors.New("decode " + resource)` | `:5360-5364` |
| S7 | `daemon/internal/app/goal_delivery_test.go` | new `TestAbandonGoalSessionUncertainBoundsOversizedCloseReason` (2 table rows) pinning the W307-R1 bound at `goal_admission.go:1492` | `:776-828` |
| S9 | this record §0/§1/§2/Appendix B + the W3-07 plan's pointer | citation refresh, no code change | §12.2 |
| — | `docs/TODO.md` | S1 closed its entry; S6 landed a `ControlProjection` dead-surface entry; the W1-06 entry keeps its two open surfaces with a residual line | S6 has no code change |

Not applied in this stage (unchanged; reasons in the stage result): S5 (unexporting
`QueueLateGoalUsage`/`CheckGoalSessionLineage` needs a decision-record amendment — the W3-08 plan
records those methods as retained); S6's deletion (would retire the contract-pinning
`TestGoalSessionControlProjectionOmitsRawNativeIdentity` — only the ledger entry landed); S8 was
the gate re-run itself.

### 12.2 Sweep and citation refresh (S1/S3/S4/S9)

The tool was rebuilt from Appendix A in a scratch directory and re-run at the apply tip:
`-selfcheck` exit 0 (`{"other-branch":2,"pred":1,"row":2}`); `rows 28 (static-new 28,
static-fmt-no-w 0, sentinel-return 0)`; `-all`: row 28 / pred 196 / other-branch 39 / no-branch
265, raw listing total 528. The base side re-derives unchanged (57 rows; row 57 / pred 197 /
other-branch 36 / no-branch 259 at `52bdffd2`). §1's After paragraphs and accounting table, and
Appendix B's apply-tip block, carry these numbers.

Touched-row cell refresh (`base` row identity from §2 → fix-round cell → apply tip; all apply-tip
lines verified by `rg -n` in the worktree):

| §2 row key | fix-round cell | apply tip |
|---|---|---|
| §2.1 `state.go:5312` write run journal | `:5315` | `:5318` |
| §2.1 `state.go:5342` read %s | `:5345` | `:5348` |
| §2.1 `state.go:5352` RejectDuplicateJSONMembers | `:5355` | `:5358` |
| §2.1 `state.go:5360` decoder.Decode | `:5363` | `:5368` |
| §2.1 `state.go:5364` guarded arm | `:5368` | `:5373` |
| §2.1 `state.go:5384` inner trailing JSON | `:5391` | `:5396` |
| §2.1 `state.go:5486` write %s | `:5498` | `:5503` |
| §2.1 `state.go:5514` remove journal temp | `:5526` | `:5531` |
| §2.1 `state.go:6363` terminal payload | `:6375` | `:6380` |
| §2.1 `provider_action.go:290` trailing data | `:298`/`:300` | `:297` (collapsed, S1) |
| §2.2 `provider_action.go:358` array | `:368`/`:371` | `:365`/`:368` |
| §2.2 `provider_action.go:368` object key | `:381`/`:384` | `:378`/`:381` |
| §2.2 `provider_action.go:382` object | `:398`/`:401` | `:395`/`:398` |
| §2.3 `state.go:5305` save capacity | `:5305`/`:5308` | helper `provider_action.go:594`/`:597` (S3) |
| §2.3 `provider_action.go:541` intents capacity | `:560`/`:563` | `:557`/`:560` |
| §2.3 `provider_action.go:571` save capacity | `:597`/`:600` | helper `provider_action.go:594`/`:597` (S3) |
| §2.3 `provider_action.go:592` reserved copy | `:605` → `:627` | `:613` → `:629-632` → helper `:594`/`:597` |
| §2.3 `provider_action.go:613` journal size | `:624`/`:627` | helper `provider_action.go:594`/`:597` (S3) |
| §2.4 nil-cause arms `state.go` | `:5370`/`:5393` | `:5375`/`:5398` |
| §2.4 nil-cause arms `provider_action.go` | `:298`/`:300` | `:297` (S1) |

Cells not listed here were not touched by the apply stage; they keep their fix-round resolution
(+6 for the `state.go` rows above the edit zone — e.g. §2.1's `:913` → `:919`, `:1202` → `:1208`,
`:1212` → `:1218`, `:1224` → `:1230`, `:1270` → `:1276`, `:2316` → `:2322`, `:2335` → `:2341`,
`:2365` → `:2371`, `:2386` → `:2392`, all verified). S4 additionally makes the §2.1
`state.go:5384`/`:5391` wrap reachable through the production read path (§2.4, §3.1).

### 12.3 S7 mutation pair (pinning the second composed-reason site)

Green at the apply tip: `go test -count=1 ./internal/app/ -run
'TestAbandonGoalSessionUncertainBoundsOversizedCloseReason'` → exit 0, both table rows PASS. Red
under the mutation `goal_admission.go:1492` `boundedGoalSessionUncertainReason(reason)` → `reason`:
exit 1, both rows FAIL at `goal_delivery_test.go:811` (`oversized uncertain reason left the session
off the uncertain path`: the store rejected the over-budget reason, so the session stayed attached
and took the `requireNativeSessionCloseRetry` path). The mutation was reverted; `git diff --stat` at
the apply tip lists no `goal_admission.go` hunk, which is the byte-level proof of restoration.

### 12.4 Case counts (§9 sync)

`go test -count=1 -v ./internal/state/...` at the apply tip: exit 0, 458 `=== RUN` lines, 0 FAIL —
the §9 count (457 → 458) is unchanged by the apply stage, which adds no state test (S7's test is in
`./internal/app`). §9's companion "390 PASS lines" does not reproduce at the apply tip under either
convention (458 `--- PASS` lines including subtests, 225 top-level); that row stays as the
fix-round capture.

### 12.5 Gates at the apply tip (all foreground, exit codes verbatim)

| # | command (cwd `daemon/` unless noted) | exit | observed |
|---|---:|---|
| 1 | `gofmt -l .` | 0 | 0 lines |
| 2 | `go vet ./...` | 0 | no output |
| 3 | `GOOS=windows go build ./...` | 0 | no output |
| 4 | `GOOS=linux go build ./...` | 0 | no output |
| 5 | `go build ./...` | 0 | no output |
| 6 | `GOOS=linux GOARCH=amd64 go vet ./e2e/` | 0 | no output |
| 7 | `GOOS=darwin GOARCH=arm64 go vet ./internal/app/` | 0 | no output; the `!linux && !windows` construction site and `export_test.go` are in the darwin `TestGoFiles` set |
| 8 | `go test -count=1 -v ./internal/state/...` (Windows) | 0 | `ok … 7.218s`; 458 `=== RUN`, 0 FAIL |
| 9 | WSL `Ubuntu-24.04`: `go test -count=1 ./internal/state/...` (worktree path) | 0 | `ok … 15.547s` (go1.27.0 linux/amd64; the linux-gated clone/cgroup suites run) |
| 10 | `go test -count=1 ./internal/app/` (slow oracle) | 0 | `ok … 235.644s` (host was busier than the fix-round 170.6s; green) |
| 11 | `go test -count=1 ./internal/app/ -run '^TestAbandonGoalSessionUncertainBoundsOversizedCloseReason$'` | 0 / 1 | green; 1 under the reverted `goal_admission.go:1492` mutation (§12.3) |
| 12 | planted-reference pair, 15 scratch packages + `go vet ./zzapplyprobe/...`: pre-rename base `6d10da97` (scratch `git archive` copy) vs apply tip | 0 / 1 | base: exit 0, all 15 references compile; apply tip: 15 `undefined: state.<Name>` errors — one per name; probes deleted |
| 13 | clause-1 grep, Go-scoped: `git grep --untracked -n -E 'state\.(…)' -- '*.go' ':!daemon/internal/state'` | 1 | no matches (the whole-tree form matches only the quoted probe output in this record and the W3-08 one, 22 lines — W3-08 evidence §1b) |
| 14 | `git diff --check` | 0 | clean (one `new blank line at EOF` in `docs/archive/CLOSED-TODO.md` was introduced and fixed during the stage) |
| 15 | host-path scan: `git diff origin/main...HEAD` and the worktree diff (0 matches; grep exit 1) + `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on the two evidence records and the W3-07 plan | 0 | no host absolute paths in the records |
| 16 | sweep tool rebuilt from Appendix A: `-selfcheck`, rows, `-predicates`, `-all` (apply tip and base copy) | 0 | §12.2 numbers; the base side reproduces 57 rows and row 57 / pred 197 / other-branch 36 / no-branch 259 |

Not re-run, with reasons: `-race` (no cgo on this Windows host; CI remains the race oracle, and the
apply set adds no concurrency — S7's test is sequential); the advisory `golangci-lint` set (CI-only,
non-gating, unchanged by the apply set: no new exported symbol, the extracted helper is used three
times); e2e and `cmd/` test suites (no e2e or `cmd` file is touched; their compile surface is
covered by rows 6 and 7).

## Appendix A — sweep tool source (verbatim, stdlib-only)

```go
// Sweep tool for rf-w3-07 (error-cause wrapping + capacity split).
//
// Recorded counting rule. A *row* is a `return` of a static error inside the
// body (any depth) of an `if` whose condition or init tests an *error value*
// for failure:
//
//	<err-ish ident> != nil          err != io.EOF
//	errors.Is(<err-ish ident>, ...) / errors.As(...)
//
// where "err-ish" means the identifier contains "err" (case-insensitive), so
// `err`, `readErr`, `tokenErr`, `refErr` are covered. Reported return kinds:
//
//	static-new         errors.New(...)
//	static-fmt-no-w    fmt.Errorf(...) whose format string has no %w
//	sentinel-return    a bare Err* sentinel value (possible conflation)
//
// Excluded by construction: `err == nil` / `err == io.EOF` success branches;
// predicates with no error variable (`X.Validate() != nil`,
// `protocol.RejectDuplicateJSONMembers(v) != nil`, pointer/field presence
// checks); typed classification returns (`&NotFoundError{...}`); bare error
// propagation (`return err`).
//
// Modes:
//
//	(none)        rows only (the rule above) + per-kind totals
//	-predicates   the residual predicate class: static returns inside a branch
//	              that tests only calls/fields/selectors against nil, with no
//	              error variable failure test anywhere above it
//	-all          complete accounting: every static error return classified as
//	              row / pred / other-branch / no-branch
//
// Usage: sweep [-predicates|-all] <files...> | -selfcheck
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strings"
)

const (
	kindErr   = "err-branch"
	kindPred  = "pred-branch"
	kindOther = "other-branch"
)

type ctx struct {
	kind string
	line int
	cond string
}

type row struct {
	file  string
	line  int
	fn    string
	kind  string
	class string
	cond  string
	expr  string
}

var (
	fset = token.NewFileSet()
	mode = "rows"
	rows []row
)

func render(n ast.Node) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, n); err != nil {
		return "<error>"
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

func isErrName(name string) bool {
	return strings.Contains(strings.ToLower(name), "err")
}

// mentionsErrIdent reports whether the expression mentions an error-ish identifier.
func mentionsErrIdent(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok && isErrName(id.Name) {
			found = true
		}
		return !found
	})
	return found
}

func isNilOrEOF(n ast.Node) bool {
	switch v := n.(type) {
	case *ast.Ident:
		return v.Name == "nil"
	case *ast.SelectorExpr:
		return v.Sel.Name == "EOF"
	}
	return false
}

func isErrorsCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "errors" && (sel.Sel.Name == "Is" || sel.Sel.Name == "As")
}

// errorBranchTest reports whether the node contains a failure test on an
// error-ish identifier.
func errorBranchTest(n ast.Node) bool {
	if n == nil {
		return false
	}
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		switch v := node.(type) {
		case *ast.BinaryExpr:
			if v.Op != token.NEQ {
				return true
			}
			if isNilOrEOF(v.Y) && mentionsErrIdent(v.X) {
				found = true
			}
			if isNilOrEOF(v.X) && mentionsErrIdent(v.Y) {
				found = true
			}
		case *ast.CallExpr:
			if isErrorsCall(v) && len(v.Args) > 0 && mentionsErrIdent(v.Args[0]) {
				found = true
			}
		}
		return !found
	})
	return found
}

// predicateBranchTest reports whether the node tests a call/selector against
// nil while containing no error-variable failure test.
func predicateBranchTest(n ast.Node) bool {
	if n == nil || errorBranchTest(n) {
		return false
	}
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		v, ok := node.(*ast.BinaryExpr)
		if !ok || v.Op != token.NEQ {
			return !found
		}
		x, y := v.X, v.Y
		if isNilOrEOF(x) {
			x, y = y, x
		}
		if !isNilOrEOF(y) {
			return !found
		}
		switch x.(type) {
		case *ast.CallExpr, *ast.SelectorExpr:
			found = true
		}
		return !found
	})
	return found
}

func branchKind(s *ast.IfStmt) string {
	if errorBranchTest(s.Cond) || errorBranchTest(s.Init) {
		return kindErr
	}
	if predicateBranchTest(s.Cond) || predicateBranchTest(s.Init) {
		return kindPred
	}
	return ""
}

func fnName(decl *ast.FuncDecl) string {
	if decl.Recv == nil {
		return decl.Name.Name
	}
	var buf bytes.Buffer
	for _, field := range decl.Recv.List {
		if buf.Len() > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(render(field.Type))
	}
	return "(" + buf.String() + ")." + decl.Name.Name
}

func walkStmts(fn string, stmts []ast.Stmt, stack []ctx) {
	for _, stmt := range stmts {
		walkStmt(fn, stmt, stack)
	}
}

// walkFuncLits recurses into function-literal bodies reachable from an
// expression position (closure arguments, callbacks, `go`/`defer` calls). A
// function literal opens a new scope: its body starts from an empty branch
// stack, so an outer error branch never classifies a closure-local return.
func walkFuncLits(fn string, n ast.Node) {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return
	}
	ast.Inspect(n, func(node ast.Node) bool {
		lit, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		walkStmts(fn, lit.Body.List, nil)
		return false
	})
}

func walkStmt(fn string, stmt ast.Stmt, stack []ctx) {
	switch s := stmt.(type) {
	case nil:
		return
	case *ast.BlockStmt:
		walkStmts(fn, s.List, stack)
	case *ast.AssignStmt, *ast.DeclStmt, *ast.SendStmt, *ast.IncDecStmt, *ast.BranchStmt, *ast.EmptyStmt:
		walkFuncLits(fn, stmt)
	case *ast.IfStmt:
		inner := stack
		if kind := branchKind(s); kind != "" {
			inner = append(append([]ctx{}, stack...), ctx{kind: kind, line: fset.Position(s.Pos()).Line, cond: render(s.Cond)})
		}
		if s.Init != nil {
			walkStmt(fn, s.Init, inner)
		}
		walkFuncLits(fn, s.Cond)
		walkStmts(fn, s.Body.List, inner)
		if s.Else != nil {
			walkStmt(fn, s.Else, inner)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			walkStmt(fn, s.Init, stack)
		}
		walkFuncLits(fn, s.Cond)
		walkStmts(fn, s.Body.List, append(stack, ctx{kind: kindOther, line: fset.Position(s.Pos()).Line}))
	case *ast.RangeStmt:
		walkFuncLits(fn, s.X)
		walkStmts(fn, s.Body.List, append(stack, ctx{kind: kindOther, line: fset.Position(s.Pos()).Line}))
	case *ast.SwitchStmt:
		if s.Init != nil {
			walkStmt(fn, s.Init, stack)
		}
		walkFuncLits(fn, s.Tag)
		walkStmt(fn, s.Body, append(stack, ctx{kind: kindOther, line: fset.Position(s.Pos()).Line}))
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			walkStmt(fn, s.Init, stack)
		}
		walkFuncLits(fn, s.Assign)
		walkStmt(fn, s.Body, append(stack, ctx{kind: kindOther, line: fset.Position(s.Pos()).Line}))
	case *ast.SelectStmt:
		walkStmt(fn, s.Body, append(stack, ctx{kind: kindOther, line: fset.Position(s.Pos()).Line}))
	case *ast.CaseClause:
		for _, expr := range s.List {
			walkFuncLits(fn, expr)
		}
		walkStmts(fn, s.Body, stack)
	case *ast.CommClause:
		if s.Comm != nil {
			walkStmt(fn, s.Comm, stack)
		}
		walkStmts(fn, s.Body, stack)
	case *ast.LabeledStmt:
		walkStmt(fn, s.Stmt, stack)
	case *ast.GoStmt, *ast.DeferStmt, *ast.ExprStmt:
		walkFuncLits(fn, stmt)
	case *ast.ReturnStmt:
		walkFuncLits(fn, stmt)
		classifyReturn(fn, s, stack)
	default:
		walkFuncLits(fn, stmt)
	}
}

func classifyReturn(fn string, s *ast.ReturnStmt, stack []ctx) {
	for _, result := range s.Results {
		kind := ""
		switch v := result.(type) {
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				continue
			}
			switch {
			case pkg.Name == "errors" && sel.Sel.Name == "New":
				kind = "static-new"
			case pkg.Name == "fmt" && sel.Sel.Name == "Errorf":
				if strings.Contains(render(v.Args[0]), "%w") {
					continue
				}
				kind = "static-fmt-no-w"
			default:
				continue
			}
		case *ast.Ident:
			if !strings.HasPrefix(v.Name, "Err") {
				continue
			}
			kind = "sentinel-return"
		default:
			continue
		}
		class := "no-branch"
		innermost := ""
		for index := len(stack) - 1; index >= 0; index-- {
			if innermost == "" {
				innermost = stack[index].cond
			}
			if stack[index].kind == kindErr {
				class = "row"
				break
			}
			if stack[index].kind == kindPred {
				class = "pred"
			} else if class == "no-branch" {
				class = "other-branch"
			}
		}
		rows = append(rows, row{
			file: fset.Position(s.Pos()).Filename, line: fset.Position(s.Pos()).Line,
			fn: fn, kind: kind, class: class, cond: innermost, expr: render(result),
		})
	}
}

func selfCheck() {
	// The mode must be able to reproduce the recorded rule on a fixture whose
	// expected classification is known, so a reviewer can validate the tool.
	const source = `package fixture

import "errors"

func cases(flag bool, err error, value error) error {
	if err != nil {
		return errors.New("row")
	}
	if err == nil {
		return errors.New("success-branch")
	}
	if !flag && value.Validate() != nil {
		return errors.New("pred")
	}
	if err := value.Validate(); err != nil {
		return errors.New("row")
	}
	switch {
	case errors.Is(err, errors.New("sentinel")):
		return errors.New("row")
	default:
		return errors.New("other")
	}
}
`
	file, err := parser.ParseFile(fset, "fixture.go", source, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "selfcheck parse:", err)
		os.Exit(2)
	}
	for _, decl := range file.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok {
			walkStmts(fnName(fnDecl), fnDecl.Body.List, nil)
		}
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.class]++
	}
	report := map[string]int{"row": counts["row"], "pred": counts["pred"], "other-branch": counts["other-branch"]}
	encoded, _ := json.Marshal(report)
	fmt.Println(string(encoded))
	if report["row"] != 2 || report["pred"] != 1 || report["other-branch"] != 2 {
		fmt.Fprintln(os.Stderr, "selfcheck FAILED")
		os.Exit(1)
	}
}

func main() {
	for len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "-predicates":
			mode = "predicates"
		case "-all":
			mode = "all"
		case "-selfcheck":
			selfCheck()
			return
		default:
			fmt.Fprintln(os.Stderr, "unknown flag:", os.Args[1])
			os.Exit(2)
		}
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	for _, path := range os.Args[1:] {
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse:", err)
			os.Exit(2)
		}
		for _, decl := range file.Decls {
			fnDecl, ok := decl.(*ast.FuncDecl)
			if !ok || fnDecl.Body == nil {
				continue
			}
			walkStmts(fnName(fnDecl), fnDecl.Body.List, nil)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].file != rows[j].file {
			return rows[i].file < rows[j].file
		}
		return rows[i].line < rows[j].line
	})
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.kind]++
	}
	classes := map[string]int{}
	for _, r := range rows {
		switch mode {
		case "rows":
			if r.class == "row" {
				printRow(r, "row")
				classes["row"]++
			}
		case "predicates":
			if r.class == "pred" {
				printRow(r, "pred")
				classes["pred"]++
			}
		case "all":
			printRow(r, r.class)
			classes[r.class]++
		}
	}
	fmt.Printf("TOTAL reported %d rows\n", classes["row"]+classes["pred"]+classes["other-branch"]+classes["no-branch"])
	switch mode {
	case "rows":
		fmt.Printf("  rows %d", classes["row"])
		fmt.Printf(" (static-new %d, static-fmt-no-w %d, sentinel-return %d)\n",
			classKind(rows, "row", "static-new"), classKind(rows, "row", "static-fmt-no-w"), classKind(rows, "row", "sentinel-return"))
	case "predicates":
		fmt.Printf("  pred-class %d\n", classes["pred"])
	default:
		fmt.Printf("  row %d  pred %d  other-branch %d  no-branch %d\n",
			classes["row"], classes["pred"], classes["other-branch"], classes["no-branch"])
	}
}

func printRow(r row, class string) {
	fmt.Printf("%s:%d\t%s\t%s\t%s\t%s\t%s\n", r.file, r.line, class, r.kind, r.fn, r.cond, r.expr)
}

func classKind(rows []row, class, kind string) int {
	count := 0
	for _, r := range rows {
		if r.class == class && r.kind == kind {
			count++
		}
	}
	return count
}
```

## Appendix B — sweep listings (rows mode; the constant `row` class column omitted)

The plan's "Before output"/"Sweep after" blocks carry this same run's full stdout, including the
constant `row` class column and the two summary lines the tool prints after the rows. Here the
columns are: file:line, kind, function, condition, returned expression, i.e. the stdout minus the
class column (constant in rows mode).

Before (base 52bdffd2):

```
provider_action.go:267	sentinel-return	ValidateNewProviderActionResult	err != nil || len(persisted) > maxProviderActionResultBytes	ErrProviderActionCapacity
provider_action.go:284	static-new	normalizeProviderActionResult	err != nil || value.kind != providerActionJSONObject	errors.New("provider action result must be a JSON object")
provider_action.go:288	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
provider_action.go:290	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
provider_action.go:358	static-new	decodeProviderActionJSONValue	err != nil || end != json.Delim(']')	errors.New("provider action result array is invalid")
provider_action.go:368	static-new	decodeProviderActionJSONValue	err != nil || !ok	errors.New("provider action result object key is invalid")
provider_action.go:382	static-new	decodeProviderActionJSONValue	err != nil || end != json.Delim('}')	errors.New("provider action result object is invalid")
provider_action.go:541	sentinel-return	validateProviderActionDispatchCapacity	err != nil || len(providerData) > maxProviderActionBytes	ErrProviderActionCapacity
provider_action.go:571	sentinel-return	validateJournalSaveCapacity	err != nil || len(data) > maxJournalFileBytes	ErrProviderActionCapacity
provider_action.go:592	sentinel-return	validateJournalSaveCapacity	err != nil || len(reservedData) > maxJournalFileBytes	ErrProviderActionCapacity
provider_action.go:613	sentinel-return	validateProviderActionJournalSize	err != nil || len(data) > maxJournalFileBytes	ErrProviderActionCapacity
state.go:913	static-new	linuxCloneCleanupIntentFromJournal	err != nil	errors.New("encode Linux clone cleanup fence")
state.go:1202	static-new	(*Store).Close	err != nil	errors.New("release state directory lock")
state.go:1212	static-new	NewDaemonInstanceID	err != nil	errors.New("generate daemon instance ID")
state.go:1224	static-new	NewMachineToken	err != nil	errors.New("generate machine token")
state.go:1241	static-new	(*Store).LoadEnrollmentIntent	err != nil	errors.New("invalid enrollment intent")
state.go:1270	static-new	(*Store).DeleteEnrollmentIntent	err != nil	errors.New("remove enrollment intent")
state.go:1289	static-new	(*Store).LoadIdentity	err != nil	errors.New("invalid identity record")
state.go:1422	static-new	(*Store).ReserveLinuxCloneIntent	err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation	errors.New("Linux clone intent does not match RunKey")
state.go:1464	static-new	(*Store).BindLinuxCloneSeed	err != nil || seed.BaseCommit != intent.BaseCommit	errors.New("Linux clone seed receipt does not match its intent")
state.go:1539	static-new	(*Store).BindLinuxCloneReuse	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse grant does not match the new RunKey")
state.go:1707	static-new	(*Store).AbortLinuxCloneReuseBeforeNativeStart	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse abort grant does not match the RunKey")
state.go:1796	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Validate() != nil || candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) || receipt.Version != candidate.Version || receipt.Commit != candidate.Commit || receipt.TreeDigest != candidate.TreeDigest	errors.New("Linux clone promotion receipt does not match candidate intent")
state.go:1800	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Ref != ref	errors.New("Linux clone promotion ref does not match RunKey")
state.go:1960	static-new	(*Store).BeginLinuxCloneCleanup	err != nil || !journal.LinuxCloneCleanupIntent.Equal(intent)	errors.New("Linux clone cleanup intent readback is invalid")
state.go:2014	static-new	(*Store).RecordLinuxCloneTargetRemoval	err != nil || proof.Validate() != nil || proof.Disposition != authority.LinuxCloneTargetDispositionRemoved || !proof.TargetNlinkZero || !proof.Intent.Equal(expected)	errors.New("Linux clone target-removal proof does not match cleanup intent")
state.go:2045	static-new	(*Store).CompleteLinuxCloneCleanup	err != nil || proof.Validate() != nil || !proof.Intent.Equal(expected)	errors.New("Linux clone cleanup proof does not match cleanup intent")
state.go:2316	static-new	(*Store).DeleteJournal	err != nil	errors.New("delete run journal")
state.go:2335	static-new	(*Store).listJournalsLocked	err != nil	errors.New("list run journal directory")
state.go:2365	static-new	(*Store).forEachJournalLocked	err != nil	errors.New("list run journal directory")
state.go:2386	static-new	(*Store).forEachJournalLocked	readErr != nil	errors.New("list run journal directory")
state.go:2397	static-new	(*Store).readJournalEntryLocked	err != nil	errors.New("invalid run journal")
state.go:2666	static-new	(*Store).CheckLinuxV2NativeAdmission	err != nil || !validLinuxV2AdmissionFence(key, fence) || now.IsZero()	errors.New("Linux v2 native admission RunKey, fence, or time is invalid")
state.go:3035	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || expected.CreateStarted || expected.LeafIdentity != nil || expected.Helper != nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil || expected.HelperExit != nil || expected.AbortProof != nil || expected.PreGateStopReceipt != nil || expected.LauncherGrantPossible || expected.Mirror == nil	errors.New("Linux cgroup reservation is not a never-created state")
state.go:3046	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || !linuxCgroupReservationAbortMatches(expected, proof)	errors.New("Linux cgroup never-created tombstone is invalid")
state.go:4860	static-new	(*Store).loadJournalLocked	err != nil	errors.New("invalid run journal")
state.go:4904	static-new	requireLinuxCloneJournalFence	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("Linux clone intent does not match the current admitted Subject")
state.go:5030	static-new	validateLinuxCloneReuseRetainedTarget	err != nil	errors.New("run journal Linux clone reuse does not match admitted Subject")
state.go:5052	static-new	validateLinuxCloneIntent	err != nil	errors.New("run journal Linux clone intent does not match admitted Subject")
state.go:5157	static-new	validateLinuxClonePromotion	promotion.Validate() != nil || err != nil || promotion.Ref != ref || promotion.Commit != promotionIntent.Commit || promotion.TreeDigest != promotionIntent.TreeDigest || !linuxCloneStopProven(journal)	errors.New("run journal Linux clone promotion receipt is invalid")
state.go:5181	static-new	validateLinuxClonePromotionWithoutIntent	promotion.Validate() != nil || refErr != nil || promotion.Ref != legacyRef || len(promotion.Commit) != len(intent.BaseCommit) || !linuxCloneStopProven(journal) || journal.WorkspacePath != intent.Target	errors.New("run journal legacy Linux clone promotion receipt is invalid")
state.go:5196	static-new	validateLinuxCloneCleanup	err != nil || !cleanupIntent.Equal(*journal.LinuxCloneCleanupIntent)	errors.New("run journal Linux clone cleanup intent is invalid")
state.go:5294	static-new	requireLinuxCloneJournalFenceSubject	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("admitted Subject does not match Linux clone intent")
state.go:5305	sentinel-return	(*Store).saveJournalLocked	err != nil || len(data) > maxJournalFileBytes	ErrProviderActionCapacity
state.go:5312	static-new	(*Store).saveJournalLocked	err != nil	errors.New("write run journal")
state.go:5342	static-new	(*Store).readJSONWithLimitAndTags	err != nil	errors.New("read " + resource)
state.go:5352	static-new	decodeJSONWithLimitAndTags	err != nil	errors.New("decode " + resource)
state.go:5360	static-new	decodeJSONWithLimitAndTags	err != nil	errors.New("decode " + resource)
state.go:5364	static-new	decodeJSONWithLimitAndTags	err != io.EOF	errors.New("decode " + resource)
state.go:5384	static-new	rejectCaseInsensitiveJSONTagAliases	err != io.EOF	errors.New("trailing JSON data")
state.go:5479	static-new	(*Store).writeJSONWithLimit	err != nil || len(data) > limit	errors.New("encode " + resource)
state.go:5486	static-new	(*Store).writeJSONWithLimit	err != nil	errors.New("write " + resource)
state.go:5514	static-new	(*Store).removeOwnedTempsLocked	err != nil && !errors.Is(err, os.ErrNotExist)	errors.New("remove run journal temporary file")
state.go:6061	static-new	validateContainmentAuthority	err != nil || journal.ContainmentAuthority.TargetPID != journal.PID || journal.ContainmentAuthority.TargetIdentity != journal.ProcessIdentity	errors.New("run journal containment authority is invalid")
state.go:6084	static-new	validateContainmentHandoff	err != nil	errors.New("run journal containment handoff is invalid")
state.go:6363	static-new	terminalTaskResultKind	err != nil	errors.New("completed terminal payload is invalid")
state.go:6373	static-new	terminalTaskResultKind	err != nil || !validTaskResultKind(result.Kind)	errors.New("completed terminal task result kind is invalid")
```

After (fix-round tree `01aada59`/`6d10da97`):

```
provider_action.go:298	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
provider_action.go:300	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
state.go:1241	static-new	(*Store).LoadEnrollmentIntent	err != nil	errors.New("invalid enrollment intent")
state.go:1289	static-new	(*Store).LoadIdentity	err != nil	errors.New("invalid identity record")
state.go:1422	static-new	(*Store).ReserveLinuxCloneIntent	err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation	errors.New("Linux clone intent does not match RunKey")
state.go:1464	static-new	(*Store).BindLinuxCloneSeed	err != nil || seed.BaseCommit != intent.BaseCommit	errors.New("Linux clone seed receipt does not match its intent")
state.go:1539	static-new	(*Store).BindLinuxCloneReuse	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse grant does not match the new RunKey")
state.go:1707	static-new	(*Store).AbortLinuxCloneReuseBeforeNativeStart	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse abort grant does not match the RunKey")
state.go:1796	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Validate() != nil || candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) || receipt.Version != candidate.Version || receipt.Commit != candidate.Commit || receipt.TreeDigest != candidate.TreeDigest	errors.New("Linux clone promotion receipt does not match candidate intent")
state.go:1800	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Ref != ref	errors.New("Linux clone promotion ref does not match RunKey")
state.go:1960	static-new	(*Store).BeginLinuxCloneCleanup	err != nil || !journal.LinuxCloneCleanupIntent.Equal(intent)	errors.New("Linux clone cleanup intent readback is invalid")
state.go:2014	static-new	(*Store).RecordLinuxCloneTargetRemoval	err != nil || proof.Validate() != nil || proof.Disposition != authority.LinuxCloneTargetDispositionRemoved || !proof.TargetNlinkZero || !proof.Intent.Equal(expected)	errors.New("Linux clone target-removal proof does not match cleanup intent")
state.go:2045	static-new	(*Store).CompleteLinuxCloneCleanup	err != nil || proof.Validate() != nil || !proof.Intent.Equal(expected)	errors.New("Linux clone cleanup proof does not match cleanup intent")
state.go:2397	static-new	(*Store).readJournalEntryLocked	err != nil	errors.New("invalid run journal")
state.go:2666	static-new	(*Store).CheckLinuxV2NativeAdmission	err != nil || !validLinuxV2AdmissionFence(key, fence) || now.IsZero()	errors.New("Linux v2 native admission RunKey, fence, or time is invalid")
state.go:3035	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || expected.CreateStarted || expected.LeafIdentity != nil || expected.Helper != nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil || expected.HelperExit != nil || expected.AbortProof != nil || expected.PreGateStopReceipt != nil || expected.LauncherGrantPossible || expected.Mirror == nil	errors.New("Linux cgroup reservation is not a never-created state")
state.go:3046	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || !linuxCgroupReservationAbortMatches(expected, proof)	errors.New("Linux cgroup never-created tombstone is invalid")
state.go:4860	static-new	(*Store).loadJournalLocked	err != nil	errors.New("invalid run journal")
state.go:4904	static-new	requireLinuxCloneJournalFence	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("Linux clone intent does not match the current admitted Subject")
state.go:5030	static-new	validateLinuxCloneReuseRetainedTarget	err != nil	errors.New("run journal Linux clone reuse does not match admitted Subject")
state.go:5052	static-new	validateLinuxCloneIntent	err != nil	errors.New("run journal Linux clone intent does not match admitted Subject")
state.go:5157	static-new	validateLinuxClonePromotion	promotion.Validate() != nil || err != nil || promotion.Ref != ref || promotion.Commit != promotionIntent.Commit || promotion.TreeDigest != promotionIntent.TreeDigest || !linuxCloneStopProven(journal)	errors.New("run journal Linux clone promotion receipt is invalid")
state.go:5181	static-new	validateLinuxClonePromotionWithoutIntent	promotion.Validate() != nil || refErr != nil || promotion.Ref != legacyRef || len(promotion.Commit) != len(intent.BaseCommit) || !linuxCloneStopProven(journal) || journal.WorkspacePath != intent.Target	errors.New("run journal legacy Linux clone promotion receipt is invalid")
state.go:5196	static-new	validateLinuxCloneCleanup	err != nil || !cleanupIntent.Equal(*journal.LinuxCloneCleanupIntent)	errors.New("run journal Linux clone cleanup intent is invalid")
state.go:5294	static-new	requireLinuxCloneJournalFenceSubject	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("admitted Subject does not match Linux clone intent")
state.go:5370	static-new	decodeJSONWithLimitAndTags	err != io.EOF	errors.New("decode " + resource)
state.go:5393	static-new	rejectCaseInsensitiveJSONTagAliases	err != io.EOF	errors.New("trailing JSON data")
state.go:6073	static-new	validateContainmentAuthority	err != nil || journal.ContainmentAuthority.TargetPID != journal.PID || journal.ContainmentAuthority.TargetIdentity != journal.ProcessIdentity	errors.New("run journal containment authority is invalid")
state.go:6096	static-new	validateContainmentHandoff	err != nil	errors.New("run journal containment handoff is invalid")
```

Predicate class counts: base 197, delivered 197 (`sweep -predicates`); full accounting: base row 57 / pred 197 / other-branch 36 / no-branch 259, delivered row 29 / pred 197 / other-branch 39 / no-branch 267 (`sweep -all`).

After (rf-w3-08 apply tip, 2026-11-21) — exact stdout of the same tool rebuilt from Appendix A
(minus the class column and the two summary lines, as above):

```
provider_action.go:297	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
state.go:1247	static-new	(*Store).LoadEnrollmentIntent	err != nil	errors.New("invalid enrollment intent")
state.go:1295	static-new	(*Store).LoadIdentity	err != nil	errors.New("invalid identity record")
state.go:1428	static-new	(*Store).ReserveLinuxCloneIntent	err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation	errors.New("Linux clone intent does not match RunKey")
state.go:1470	static-new	(*Store).BindLinuxCloneSeed	err != nil || seed.BaseCommit != intent.BaseCommit	errors.New("Linux clone seed receipt does not match its intent")
state.go:1545	static-new	(*Store).BindLinuxCloneReuse	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse grant does not match the new RunKey")
state.go:1713	static-new	(*Store).AbortLinuxCloneReuseBeforeNativeStart	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse abort grant does not match the RunKey")
state.go:1802	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Validate() != nil || candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) || receipt.Version != candidate.Version || receipt.Commit != candidate.Commit || receipt.TreeDigest != candidate.TreeDigest	errors.New("Linux clone promotion receipt does not match candidate intent")
state.go:1806	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Ref != ref	errors.New("Linux clone promotion ref does not match RunKey")
state.go:1966	static-new	(*Store).BeginLinuxCloneCleanup	err != nil || !journal.LinuxCloneCleanupIntent.Equal(intent)	errors.New("Linux clone cleanup intent readback is invalid")
state.go:2020	static-new	(*Store).RecordLinuxCloneTargetRemoval	err != nil || proof.Validate() != nil || proof.Disposition != authority.LinuxCloneTargetDispositionRemoved || !proof.TargetNlinkZero || !proof.Intent.Equal(expected)	errors.New("Linux clone target-removal proof does not match cleanup intent")
state.go:2051	static-new	(*Store).CompleteLinuxCloneCleanup	err != nil || proof.Validate() != nil || !proof.Intent.Equal(expected)	errors.New("Linux clone cleanup proof does not match cleanup intent")
state.go:2403	static-new	(*Store).readJournalEntryLocked	err != nil	errors.New("invalid run journal")
state.go:2672	static-new	(*Store).CheckLinuxV2NativeAdmission	err != nil || !validLinuxV2AdmissionFence(key, fence) || now.IsZero()	errors.New("Linux v2 native admission RunKey, fence, or time is invalid")
state.go:3041	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || expected.CreateStarted || expected.LeafIdentity != nil || expected.Helper != nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil || expected.HelperExit != nil || expected.AbortProof != nil || expected.PreGateStopReceipt != nil || expected.LauncherGrantPossible || expected.Mirror == nil	errors.New("Linux cgroup reservation is not a never-created state")
state.go:3052	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || !linuxCgroupReservationAbortMatches(expected, proof)	errors.New("Linux cgroup never-created tombstone is invalid")
state.go:4866	static-new	(*Store).loadJournalLocked	err != nil	errors.New("invalid run journal")
state.go:4910	static-new	requireLinuxCloneJournalFence	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("Linux clone intent does not match the current admitted Subject")
state.go:5036	static-new	validateLinuxCloneReuseRetainedTarget	err != nil	errors.New("run journal Linux clone reuse does not match admitted Subject")
state.go:5058	static-new	validateLinuxCloneIntent	err != nil	errors.New("run journal Linux clone intent does not match admitted Subject")
state.go:5163	static-new	validateLinuxClonePromotion	promotion.Validate() != nil || err != nil || promotion.Ref != ref || promotion.Commit != promotionIntent.Commit || promotion.TreeDigest != promotionIntent.TreeDigest || !linuxCloneStopProven(journal)	errors.New("run journal Linux clone promotion receipt is invalid")
state.go:5187	static-new	validateLinuxClonePromotionWithoutIntent	promotion.Validate() != nil || refErr != nil || promotion.Ref != legacyRef || len(promotion.Commit) != len(intent.BaseCommit) || !linuxCloneStopProven(journal) || journal.WorkspacePath != intent.Target	errors.New("run journal legacy Linux clone promotion receipt is invalid")
state.go:5202	static-new	validateLinuxCloneCleanup	err != nil || !cleanupIntent.Equal(*journal.LinuxCloneCleanupIntent)	errors.New("run journal Linux clone cleanup intent is invalid")
state.go:5300	static-new	requireLinuxCloneJournalFenceSubject	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("admitted Subject does not match Linux clone intent")
state.go:5375	static-new	decodeJSONWithLimitAndTags	err != io.EOF	errors.New("decode " + resource)
state.go:5398	static-new	rejectCaseInsensitiveJSONTagAliases	err != io.EOF	errors.New("trailing JSON data")
state.go:6078	static-new	validateContainmentAuthority	err != nil || journal.ContainmentAuthority.TargetPID != journal.PID || journal.ContainmentAuthority.TargetIdentity != journal.ProcessIdentity	errors.New("run journal containment authority is invalid")
state.go:6101	static-new	validateContainmentHandoff	err != nil	errors.New("run journal containment handoff is invalid")
```

Apply-tip counts: `rows 28 (static-new 28, static-fmt-no-w 0, sentinel-return 0)`; the
`-predicates` run: pred 196; `sweep -all`: row 28 / pred 196 / other-branch 39 / no-branch 265
(raw listing total 528); `sweep -selfcheck` exits 0.
