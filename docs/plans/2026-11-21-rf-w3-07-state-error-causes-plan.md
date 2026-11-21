# rf-w3-07-state-error-causes plan — error-cause wrapping + capacity split (DA S4)

Contract: `goal-rf-w3-07-state-error-causes.md` (GDD, 2026-11-21) including the conductor
reconciliation (A6/P2-1–P2-3/P3, D1–D6) and program-wide rule "lesson 32" (control-flow
preservation). Base revision: `52bdffd25e46753136cf892a70cfa05e43bc01c8` (`refactor(state):
decompose the journal validators (#165)`, i.e. W2-09 merged — the contract's dispatch gate).
All `<file>:<line>` citations resolve at that revision.

## Deliverable shape

Two prod files changed; one new test file; one existing test file gains one test:

| file | change |
|---|---|
| `daemon/internal/state/state.go` | 21 of 46 swept rows carry the treatment (18 wraps, 2 condition splits, 1 capacity split); 25 rows stay untouched as verdict cascades |
| `daemon/internal/state/provider_action.go` | all 11 swept rows: 6 static rows (2 wraps, 4 condition splits) and 5 `ErrProviderActionCapacity` conflations split; the two ~25-line reservation loops become one `reserveWorstCaseCompletions` helper |
| `daemon/internal/state/state_wrap_test.go` | **new**: pins the exact wrapped strings, the `%w` chain (`errors.Is` through the writer seam; `errors.Unwrap` for the decode lane) and the decode wrap |
| `daemon/internal/state/provider_action_test.go` | **+1 test** (contract Change 2): the serialize-failure reclassification out of `ErrProviderActionCapacity`, and that the size arm still returns the sentinel value itself |

No exported symbol is added, removed, renamed or moved. `reserveWorstCaseCompletions` is
unexported (package `state`), takes and returns `RunJournal`, and allocates exactly what the two
inlined loops allocated today (one intent-slice copy, one max-result copy per unresolved intent).

## The sweep (contract Change 4 / clause 1)

**Counting rule.** A *row* is a `return` of a static error inside the body, at any depth, of an `if`
whose condition or init tests an *error value* for failure:

- `<err-ish ident> != nil`, `<err-ish ident> != io.EOF`, or `errors.Is(<err-ish ident>, …)` /
  `errors.As(…)`; "err-ish" = the identifier contains `err` case-insensitively (covers `err`,
  `readErr`, `tokenErr`, `refErr`);
- the returned expression is `errors.New(…)`, `fmt.Errorf(…)` **without** `%w`, or a bare `Err*`
  sentinel value (a conflation candidate).

Excluded by construction (each class enumerated, see "Accounting" below): `err == nil` /
`err == io.EOF` success branches; predicates with no error variable (`X.Validate() != nil`,
`protocol.RejectDuplicateJSONMembers(v) != nil`, pointer/field presence checks); typed
classification returns (`&NotFoundError{…}`); bare propagation (`return err`). Sweeping runs over
`*ast.FuncDecl` bodies including function-literal bodies (closures start a fresh branch stack, so an
outer error branch never classifies a closure-local return). Switch `case errors.Is(err, …)` arms
are *not* error branches under this rule; the `-all` accounting lists every such row, and `grep -nE
'case .*errors\.(Is|As)\(' state.go provider_action.go goal_session.go` finds none in the package.

**Command** (tool source: evidence record Appendix A; `-selfcheck` proves the tool reproduces the
rule on a fixture with a known classification):

```
cd daemon/internal/state
sweep state.go provider_action.go           # rows (the rule above)
sweep -predicates state.go provider_action.go   # the predicate class (no err-ish identifier)
sweep -all state.go provider_action.go          # complete accounting of every static error return
```

**Before output at the base revision** — exact stdout of `sweep state.go provider_action.go`
(columns: `file:line`, class, kind, function, condition, returned expression; the tool prints the
two summary lines *after* the rows, and rows mode prints the constant `row` class):

```
provider_action.go:267	row	sentinel-return	ValidateNewProviderActionResult	err != nil || len(persisted) > maxProviderActionResultBytes	ErrProviderActionCapacity
provider_action.go:284	row	static-new	normalizeProviderActionResult	err != nil || value.kind != providerActionJSONObject	errors.New("provider action result must be a JSON object")
provider_action.go:288	row	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
provider_action.go:290	row	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
provider_action.go:358	row	static-new	decodeProviderActionJSONValue	err != nil || end != json.Delim(']')	errors.New("provider action result array is invalid")
provider_action.go:368	row	static-new	decodeProviderActionJSONValue	err != nil || !ok	errors.New("provider action result object key is invalid")
provider_action.go:382	row	static-new	decodeProviderActionJSONValue	err != nil || end != json.Delim('}')	errors.New("provider action result object is invalid")
provider_action.go:541	row	sentinel-return	validateProviderActionDispatchCapacity	err != nil || len(providerData) > maxProviderActionBytes	ErrProviderActionCapacity
provider_action.go:571	row	sentinel-return	validateJournalSaveCapacity	err != nil || len(data) > maxJournalFileBytes	ErrProviderActionCapacity
provider_action.go:592	row	sentinel-return	validateJournalSaveCapacity	err != nil || len(reservedData) > maxJournalFileBytes	ErrProviderActionCapacity
provider_action.go:613	row	sentinel-return	validateProviderActionJournalSize	err != nil || len(data) > maxJournalFileBytes	ErrProviderActionCapacity
state.go:913	row	static-new	linuxCloneCleanupIntentFromJournal	err != nil	errors.New("encode Linux clone cleanup fence")
state.go:1202	row	static-new	(*Store).Close	err != nil	errors.New("release state directory lock")
state.go:1212	row	static-new	NewDaemonInstanceID	err != nil	errors.New("generate daemon instance ID")
state.go:1224	row	static-new	NewMachineToken	err != nil	errors.New("generate machine token")
state.go:1241	row	static-new	(*Store).LoadEnrollmentIntent	err != nil	errors.New("invalid enrollment intent")
state.go:1270	row	static-new	(*Store).DeleteEnrollmentIntent	err != nil	errors.New("remove enrollment intent")
state.go:1289	row	static-new	(*Store).LoadIdentity	err != nil	errors.New("invalid identity record")
state.go:1422	row	static-new	(*Store).ReserveLinuxCloneIntent	err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation	errors.New("Linux clone intent does not match RunKey")
state.go:1464	row	static-new	(*Store).BindLinuxCloneSeed	err != nil || seed.BaseCommit != intent.BaseCommit	errors.New("Linux clone seed receipt does not match its intent")
state.go:1539	row	static-new	(*Store).BindLinuxCloneReuse	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse grant does not match the new RunKey")
state.go:1707	row	static-new	(*Store).AbortLinuxCloneReuseBeforeNativeStart	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse abort grant does not match the RunKey")
state.go:1796	row	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Validate() != nil || candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) || receipt.Version != candidate.Version || receipt.Commit != candidate.Commit || receipt.TreeDigest != candidate.TreeDigest	errors.New("Linux clone promotion receipt does not match candidate intent")
state.go:1800	row	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Ref != ref	errors.New("Linux clone promotion ref does not match RunKey")
state.go:1960	row	static-new	(*Store).BeginLinuxCloneCleanup	err != nil || !journal.LinuxCloneCleanupIntent.Equal(intent)	errors.New("Linux clone cleanup intent readback is invalid")
state.go:2014	row	static-new	(*Store).RecordLinuxCloneTargetRemoval	err != nil || proof.Validate() != nil || proof.Disposition != authority.LinuxCloneTargetDispositionRemoved || !proof.TargetNlinkZero || !proof.Intent.Equal(expected)	errors.New("Linux clone target-removal proof does not match cleanup intent")
state.go:2045	row	static-new	(*Store).CompleteLinuxCloneCleanup	err != nil || proof.Validate() != nil || !proof.Intent.Equal(expected)	errors.New("Linux clone cleanup proof does not match cleanup intent")
state.go:2316	row	static-new	(*Store).DeleteJournal	err != nil	errors.New("delete run journal")
state.go:2335	row	static-new	(*Store).listJournalsLocked	err != nil	errors.New("list run journal directory")
state.go:2365	row	static-new	(*Store).forEachJournalLocked	err != nil	errors.New("list run journal directory")
state.go:2386	row	static-new	(*Store).forEachJournalLocked	readErr != nil	errors.New("list run journal directory")
state.go:2397	row	static-new	(*Store).readJournalEntryLocked	err != nil	errors.New("invalid run journal")
state.go:2666	row	static-new	(*Store).CheckLinuxV2NativeAdmission	err != nil || !validLinuxV2AdmissionFence(key, fence) || now.IsZero()	errors.New("Linux v2 native admission RunKey, fence, or time is invalid")
state.go:3035	row	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || expected.CreateStarted || expected.LeafIdentity != nil || expected.Helper != nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil || expected.HelperExit != nil || expected.AbortProof != nil || expected.PreGateStopReceipt != nil || expected.LauncherGrantPossible || expected.Mirror == nil	errors.New("Linux cgroup reservation is not a never-created state")
state.go:3046	row	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || !linuxCgroupReservationAbortMatches(expected, proof)	errors.New("Linux cgroup never-created tombstone is invalid")
state.go:4860	row	static-new	(*Store).loadJournalLocked	err != nil	errors.New("invalid run journal")
state.go:4904	row	static-new	requireLinuxCloneJournalFence	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("Linux clone intent does not match the current admitted Subject")
state.go:5030	row	static-new	validateLinuxCloneReuseRetainedTarget	err != nil	errors.New("run journal Linux clone reuse does not match admitted Subject")
state.go:5052	row	static-new	validateLinuxCloneIntent	err != nil	errors.New("run journal Linux clone intent does not match admitted Subject")
state.go:5157	row	static-new	validateLinuxClonePromotion	promotion.Validate() != nil || err != nil || promotion.Ref != ref || promotion.Commit != promotionIntent.Commit || promotion.TreeDigest != promotionIntent.TreeDigest || !linuxCloneStopProven(journal)	errors.New("run journal Linux clone promotion receipt is invalid")
state.go:5181	row	static-new	validateLinuxClonePromotionWithoutIntent	promotion.Validate() != nil || refErr != nil || promotion.Ref != legacyRef || len(promotion.Commit) != len(intent.BaseCommit) || !linuxCloneStopProven(journal) || journal.WorkspacePath != intent.Target	errors.New("run journal legacy Linux clone promotion receipt is invalid")
state.go:5196	row	static-new	validateLinuxCloneCleanup	err != nil || !cleanupIntent.Equal(*journal.LinuxCloneCleanupIntent)	errors.New("run journal Linux clone cleanup intent is invalid")
state.go:5294	row	static-new	requireLinuxCloneJournalFenceSubject	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("admitted Subject does not match Linux clone intent")
state.go:5305	row	sentinel-return	(*Store).saveJournalLocked	err != nil || len(data) > maxJournalFileBytes	ErrProviderActionCapacity
state.go:5312	row	static-new	(*Store).saveJournalLocked	err != nil	errors.New("write run journal")
state.go:5342	row	static-new	(*Store).readJSONWithLimitAndTags	err != nil	errors.New("read " + resource)
state.go:5352	row	static-new	decodeJSONWithLimitAndTags	err != nil	errors.New("decode " + resource)
state.go:5360	row	static-new	decodeJSONWithLimitAndTags	err != nil	errors.New("decode " + resource)
state.go:5364	row	static-new	decodeJSONWithLimitAndTags	err != io.EOF	errors.New("decode " + resource)
state.go:5384	row	static-new	rejectCaseInsensitiveJSONTagAliases	err != io.EOF	errors.New("trailing JSON data")
state.go:5479	row	static-new	(*Store).writeJSONWithLimit	err != nil || len(data) > limit	errors.New("encode " + resource)
state.go:5486	row	static-new	(*Store).writeJSONWithLimit	err != nil	errors.New("write " + resource)
state.go:5514	row	static-new	(*Store).removeOwnedTempsLocked	err != nil && !errors.Is(err, os.ErrNotExist)	errors.New("remove run journal temporary file")
state.go:6061	row	static-new	validateContainmentAuthority	err != nil || journal.ContainmentAuthority.TargetPID != journal.PID || journal.ContainmentAuthority.TargetIdentity != journal.ProcessIdentity	errors.New("run journal containment authority is invalid")
state.go:6084	row	static-new	validateContainmentHandoff	err != nil	errors.New("run journal containment handoff is invalid")
state.go:6363	row	static-new	terminalTaskResultKind	err != nil	errors.New("completed terminal payload is invalid")
state.go:6373	row	static-new	terminalTaskResultKind	err != nil || !validTaskResultKind(result.Kind)	errors.New("completed terminal task result kind is invalid")
TOTAL reported 57 rows
  rows 57 (static-new 51, static-fmt-no-w 0, sentinel-return 6)
```

Counts: **46 rows in `state.go` (45 `static-new` + 1 `sentinel-return`), 11 in `provider_action.go`
(6 `static-new` + 5 `sentinel-return`)**. The contract's drafting-time "39 static + 1 / 7 + 5" does
not reproduce verbatim at this head: 39/45 differ by the counting rule (the independent review's
same-rule sweep found 45 `state.go` rows — this rule reproduces that count exactly), and
`provider_action.go`'s 7th drafting-time row is the rule-excluded predicate at `:278`
(`!json.Valid(result) || protocol.RejectDuplicateJSONMembers(result) != nil` — no error variable in
scope, listed under "no-cause predicates" below). Contract Change 4 makes the recorded rule govern.

### Classification (all 57 rows)

**WRAP — 20 rows.** The condition's error value is the direct result of a durability, OS, encoding
or lock operation; the message names that operation; the cause is appended as `: %w`.

| line | operation | message (prefix preserved) |
|---|---|---|
| `state.go:913` | `json.Marshal` | `encode Linux clone cleanup fence` |
| `state.go:1202` | `releaseStoreLock` | `release state directory lock` |
| `state.go:1212` | `io.ReadFull(rand.Reader, …)` | `generate daemon instance ID` |
| `state.go:1224` | `io.ReadFull(rand.Reader, …)` | `generate machine token` |
| `state.go:1270` | `os.Remove` | `remove enrollment intent` |
| `state.go:2316` | `os.Remove` | `delete run journal` |
| `state.go:2335` | `os.ReadDir` | `list run journal directory` |
| `state.go:2365` | `os.Open` | `list run journal directory` |
| `state.go:2386` | `directory.ReadDir` (`readErr`) | `list run journal directory` |
| `state.go:5312` | `store.atomicWrite(…)`/`writeAtomic` | `write run journal` |
| `state.go:5342` | `readLimited` | `read <resource>` |
| `state.go:5352` | `protocol.RejectDuplicateJSONMembers` | `decode <resource>` |
| `state.go:5360` | `decoder.Decode` | `decode <resource>` |
| `state.go:5364` | `decoder.Decode != io.EOF` | `decode <resource>` |
| `state.go:5384` | `decoder.Decode != io.EOF` | `trailing JSON data` |
| `state.go:5486` | `store.atomicWrite(…)`/`writeAtomic` | `write <resource>` |
| `state.go:5514` | `os.Remove` (`err != nil && !errors.Is(err, os.ErrNotExist)`) | `remove run journal temporary file` |
| `state.go:6363` | `json.Unmarshal` | `completed terminal payload is invalid` |
| `provider_action.go:288` | `decoder.Token` inside `normalizeProviderActionResult` | `provider action result has trailing data` |
| `provider_action.go:290` | `decoder.Token` inside `normalizeProviderActionResult` | `provider action result has trailing data` |

**SPLIT (condition shared with a predicate) — 6 rows.** The error test becomes its own arm that
wraps; the predicate keeps the byte-identical message, so no verdict text changes.

| line | error arm | predicate arm (unchanged bytes) |
|---|---|---|
| `state.go:5479` | `fmt.Errorf("encode %s: %w", resource, err)` | `errors.New("encode " + resource)` |
| `state.go:6373` | `fmt.Errorf("completed terminal task result kind is invalid: %w", err)` | `errors.New("completed terminal task result kind is invalid")` |
| `provider_action.go:284` | `fmt.Errorf("provider action result must be a JSON object: %w", err)` | `errors.New("provider action result must be a JSON object")` |
| `provider_action.go:358` | `fmt.Errorf("provider action result array is invalid: %w", err)` | `errors.New("provider action result array is invalid")` |
| `provider_action.go:368` | `fmt.Errorf("provider action result object key is invalid: %w", err)` | `errors.New("provider action result object key is invalid")` |
| `provider_action.go:382` | `fmt.Errorf("provider action result object is invalid: %w", err)` | `errors.New("provider action result object is invalid")` |

(Exact row arithmetic: 20 WRAP + 6 SPLIT + 6 SPLIT-CAPACITY = 32 treated rows, plus the 25
verdict cascades = 57. `288`/`290` share the identical trailing-data message, so both wrap — see
"Trailing-data block" below.)

**SPLIT-CAPACITY — 6 rows** (contract Change 2; sizes/limits keep the sentinel exactly):

| line | serialize arm (new) | size arm (verbatim) |
|---|---|---|
| `state.go:5305` | `fmt.Errorf("serialize run journal: %w", err)` | `return ErrProviderActionCapacity` |
| `provider_action.go:267` | `fmt.Errorf("serialize provider action result: %w", err)` | `return ErrProviderActionCapacity` |
| `provider_action.go:541` | `fmt.Errorf("serialize provider action intents: %w", err)` | `return ErrProviderActionCapacity` |
| `provider_action.go:571` | `fmt.Errorf("serialize run journal: %w", err)` | `return ErrProviderActionCapacity` |
| `provider_action.go:592` | `fmt.Errorf("serialize run journal: %w", err)` (via the shared `validateProviderActionJournalSize` call on the reserved copy) | `return ErrProviderActionCapacity` |
| `provider_action.go:613` | `fmt.Errorf("serialize run journal: %w", err)` | `return ErrProviderActionCapacity` |

Second verdict consequence (contract Change 2, P2-3): a serialize failure no longer satisfies
`errors.Is(err, ErrProviderActionCapacity)` at `daemon/internal/app/pi_provider_bridge.go:67`, so
the bridge classifies it `local_persistence_unknown` and calls `RecoverProviderAction` instead of
returning `local_action_rejected`. Pinned by one test in `provider_action_test.go`; the genuine
capacity arm keeps returning `ErrProviderActionCapacity` itself, so the out-of-package `errors.Is`
consumer is unchanged for capacity.

**UNTOUCHED — 25 `state.go` rows: verdict cascades.** The condition's error value is a
validation/derivation call over control state (`X.Validate()`, `validateX`, `requireX`,
`protocol.ParseAdmissionInput`, `linuxCloneCleanupIntentFromJournal`, `LinuxCloneCleanupIntentFor
Journal`, `authority.LinuxClonePromotionRef`) and the returned message is that stage's verdict, not
an operation failure: `state.go:1241`, `1289`, `1422`, `1464`, `1539`, `1707`, `1796`, `1800`,
`1960`, `2014`, `2045`, `2397`, `2666`, `3035`, `3046`, `4860`, `4904`, `5030`, `5052`, `5157`,
`5181`, `5196`, `5294`, `6061`, `6084`. This is the contract's residual class ("proven no-cause
predicates (`X.Validate() != nil` cascades)" — the contract's label; these rows do carry a cause,
they are residual because the returned message is a stage verdict, see "Accounting" above). Four of
them are additionally pinned byte-exactly by
W2-09's first-error pins, and their cases *reach the error arm*, so wrapping them would break a
pinning test (proved by reading the pin case and by running it):
`state.go:6061`/`6084` in `TestJournalValidatorFirstErrorStringsArePinned` (containment-authority
wants `state_test.go:2532`, `:2540`, `:2548` build on `&authority.Supervisor{}` at `:2530` and on
`testSupervisorAuthority(...)` with a TargetPID / target-identity mismatch at `:2538`/`:2546`;
the containment-handoff want `:2573` builds on `&authority.SupervisorHandoff{}` at `:2571`; the
pins assert `err.Error() != test.want` at `:2914`),
`state.go:5030`/`5196` in `linux_clone_journal_test.go` (assertion `err.Error() != test.want` at
`:2043`; the "reuse subject mismatch" case reaches the `requireLinuxCloneJournalFenceSubject`
error arm). The remaining 21 are the same shape: a validation/derivation call plus state
predicates, returning a stage verdict string.

## Accounting (why the classes are exhaustive)

`sweep -all state.go provider_action.go` classifies every static error return in both files as
`row` / `pred` / `other-branch` / `no-branch`; at the base revision:

```
TOTAL reported 549 rows (static returns of errors.New / fmt.Errorf-without-%w / bare Err*)
  row 57  pred 197  other-branch 36  no-branch 259
```

- `pred` (197) — the predicate class: static returns inside a branch that tests only
  calls/fields/selectors against nil *and* mentions no err-ish identifier, which is the rule's
  boundary (`releaseProof.Validate() != nil || …`, `journal.LinuxCloneAbortProof != nil || …`,
  `protocol.RejectDuplicateJSONMembers(result) != nil`, `if !ok`, …). It is a rule class, not a
  claim that no cause exists: a call-shaped predicate can itself be the dropped failure, e.g.
  `state.go:5357` `rejectCaseInsensitiveJSONTagAliases(data, rootType) != nil`. Includes the
  drafting-time `provider_action.go:278` row. Contract Change 1 declares this class residual.
- `other-branch` (36) — static returns inside `for`/`switch`/`select` bodies or non-error `if`
  conditions (`if _, exists := seen[key]; exists`, `switch default:`, unknown-value arms), no
  error value in scope.
- `no-branch` (259) — static returns with no enclosing branch at all (unconditional verdicts or
  branches whose condition has no `!= nil` test, e.g. scalar/enum predicates
  `if journal.LocalState != "running" { … }`).

The `row` class is the class Change 1 wraps: its condition tests an err-ish identifier for failure
(the recorded counting rule above), i.e. the value it inspects is the error whose cause the message
drops. The other classes are *excluded by that rule*, not proven cause-free: `pred` contains
call-shaped predicates whose own failure is dropped (`rejectCaseInsensitiveJSONTagAliases(...) !=
nil`, `protocol.RejectDuplicateJSONMembers(result) != nil`), and the 25 residual `row` rows do carry
a cause from the `X.Validate()`/`parseX` call in their condition — they are residual because the
returned message is that stage's verdict, not an operation failure, which is the contract's
residual class. After the change the `row` output is that 25-row verdict set (pasted in the
delivered-outcome section and in the evidence record).

## Exact semantics

1. **Prefixes are byte-identical.** Every wrapped message is `<existing message>: <cause>`; the two
   test-visible Contains assertions (`goal_session_test.go:281` `decode goal session journal`,
   `app/harness_test.go:3830` `write run journal`) match on the prefix and stay green. No
   pre-existing test asserts any wrapped message by equality (checked repo-wide for all 21 messages:
   the only pre-existing test literals are the two Contains calls above); the increment's own
   `state_wrap_test.go` pins two of the wrapped strings by equality on purpose.
2. **Verdicts unchanged.** Capacity arms still return `ErrProviderActionCapacity` itself; every
   other untouched/static message is byte-identical; a wrapped cause is appended, never substituted.
3. **The capacity split does not reorder or retune guards.** The size comparisons, their limits
   (`maxJournalFileBytes`, `maxProviderActionBytes`, `maxProviderActionResultBytes`) and the
   `hasUnresolvedProviderActions` early return keep their positions; only `serialize` failure and
   over-budget are separated, with `err != nil` checked first — the same order as the base `||`.
4. **One reservation implementation (F9).** `reserveWorstCaseCompletions(journal RunJournal)
   RunJournal` holds the copy + `Outcome=unknown` + max-result + max-failure-code stamp and is
   called by both budget loops; `validateProviderActionJournalSize` is the one shared serialize +
   4 MiB check used by the dispatch loop, the save loop's reserved copy and the completion/recovery
   seams. The provider-only byte pre-check (`len(providerData) > maxProviderActionBytes`) stays an
   explicit extra step in the dispatch path; `hasUnresolvedProviderActions` stays the save guard's
   early return.
5. **The sweep's residual set is the acceptance set.** Clause 1's "after" state is the 25-row
   verdict residual; the wrap test's red/green/`%v`-flip runs and the mutation checks below bind it.

### Trailing-data block (`provider_action.go:288`/`290`)

Base: `if token, tokenErr := decoder.Token(); tokenErr != io.EOF { if tokenErr == nil || token !=
nil { return static } ; return static }` — both inner arms return the same message, and the block
therefore always errors for `tokenErr != io.EOF`. The delivered form keeps every condition and
every early return: `if tokenErr != nil { wrapped }`, `if token != nil { static }`, then the final
`static`. Every base path keeps its outcome and message prefix; only the case `tokenErr != nil` (a
real decoder error) gains the cause.

## Control-flow preservation audit (lesson 32)

Every early return, fall-through and short-circuit of the changed conditions, base → head:

| site | base | head | preserved |
|---|---|---|---|
| `state.go:5305` | `if err != nil \|\| len(data) > limit → sentinel` | `if err != nil → wrap`; `if len > limit → sentinel` | yes: `err` first (same order as `\|\|`), both arms error, size arm identical |
| `state.go:5479` | `if err != nil \|\| len(data) > limit → static` | `if err != nil → wrap`; `if len > limit → static` | yes |
| `state.go:6373` | `if err != nil \|\| !valid(kind) → static` | `if err != nil → wrap`; `if !valid(kind) → static` | yes |
| `provider_action.go:267/541/571/592/613` | `if err != nil \|\| len > limit → sentinel` | `if err != nil → wrap`; `if len > limit → sentinel` | yes |
| `provider_action.go:284/358/368/382` | `if err != nil \|\| <predicate> → static` | `if err != nil → wrap`; `if <predicate> → static` | yes |
| `provider_action.go:288/290` | nested pair, both arms static | `tokenErr != nil → wrap`; `token != nil → static`; final static | yes (see block above) |
| `validateProviderActionDispatchCapacity` | provider pre-check → inline reserve loop → `validateProviderActionJournalSize(reserved)` | pre-check → `validateProviderActionJournalSize(reserveWorstCaseCompletions(journal))` | yes: same check, same loop body moved verbatim, same final call |
| `validateJournalSaveCapacity` | `serialize → sentinel`; `if !hasUnresolved → nil`; reserve loop; `reservedData serialize → sentinel`; `nil` | `serialize → wrap`; `if len > limit → sentinel`; `if !hasUnresolved → nil`; `validateProviderActionJournalSize(reserveWorstCaseCompletions(journal))` | yes: same early return, same limit, same sentinel on the size arm, wrap only on serialize failure |
| `provider_action.go:269` (`ValidateNewProviderActionResult` kind check) | `if !validProviderActionResult → static` (predicate, no error value) | unchanged | yes (n/a) |

The `-all` sweep plus `git diff` are the mechanical backstop; no `if` condition outside the listed
rows is edited.

## Edge cases

- **Durable text is deliberately extended (D1/P2-2).** Wrapped suffixes reach the outbox
  `payload["error"]` (`daemon/internal/app/events.go:530-541` `queueFailure`, `:535`) and the
  durable `UncertainReason` (`daemon/internal/app/goal_admission.go:1410-1415`). No code parses
  those strings: `UncertainReason` is length-checked only (`goal_session.go:1688`, 4096 bytes) and
  `payload["error"]` is opaque to the daemon and Control. `state_wrap_test.go` pins the exact
  wrapped strings for the named sites, so the text cannot drift silently.
- **`%!w(<nil>)` is impossible at wrapped sites**: each wrapped arm returns only when its error
  value is non-nil (single `<err> != nil`, `<err> != io.EOF`, `err != nil && !errors.Is(…)`, or a
  split arm that asserts `err != nil` first).
- **`provider_action.go:267`'s serialize arm is unreachable by construction** (its guard requires a
  valid JSON result that re-encodes), so no test drives it; the split is still the correct
  classification and is covered by the shared serialize-arm assertion for the reachable guards.
- **Reserved-copy serialization** (`592`) now flows through `validateProviderActionJournalSize`;
  the size arm still yields the sentinel value itself, so `errors.Is` consumers see no change.

## Out of scope (contract §4)

W3-08's type unexports; W2-09's validator extraction; `app.go` regions; `pi_provider_bridge.go`'s
consumer logic; new error types or changed exit/verdict semantics anywhere; wrapping the `pred`
class (pointer/`Validate()` predicates) and the `no-branch`/`other-branch` verdict returns.

## Evidence plan

1. Gates (all foreground): `gofmt -l`; `go vet ./...`; `GOOS=windows go build ./...`;
   `GOOS=linux go build ./...`; `GOOS=linux GOARCH=amd64 go vet ./internal/state/`;
   `go test -count=1 ./internal/state/...` (Windows); the same suite under
   `wsl -d Ubuntu-24.04 -u accept` on the worktree's `daemon` directory (WSL mount path elided);
   targeted app oracle
   `go test -count=1 ./internal/app/ -run '^TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure$'`;
   `git diff --check`; host-path scan of the branch diff. No `-race` locally (no cgo).
   Never concurrently with the sweep/measurement tool.
2. Sweep before/after (rows, predicates, all) + `-selfcheck`.
3. Mutation checks — **corrected in fix round 1 to the measured, conductor-ratified set**: (a)
   `TestSaveJournalWrapsAtomicWriteCause` red on base, green on head, red again with `%w` → `%v`
   (after fix round 1 the decode lane's `errors.As` assertion catches the same flip at
   `state.go:5363`; recorded in evidence §6.2 and §9); (b) dropping the reservation call from the
   dispatch guard reddens `provider_action_test.go:615`, dropping it from the save guard reddens
   `:1257`; `:479` is a size assertion and does not redden on the dispatch-use removal (measured;
   conductor-ratified 2026-11-21), and `:528`/`:590` are budget/encoding assertions.
4. Evidence record `docs/evidence/rf-w3-07-state-error-causes-2026-11-21.md`: sweep listings, the
   57-row classification table with before/after, control-flow audit, gate exit codes, mutation
   outputs, Appendix A (sweep tool source), Appendix B (rows-mode before/after listings; the
   `-all` class counts are in the record's §1).
5. `docs/TODO.md`: record the pre-existing dead-code candidate found while auditing the
   trailing-data block (the `tokenErr == nil || token != nil` disjunct cannot change the outcome).

## Risks and trade-offs

- **Wrapping inside `decodeJSONWithLimitAndTags`/`readJSONWithLimitAndTags` makes every state file's
  decode error longer.** Accepted: the prefix is unchanged, and the added cause is exactly the
  field-diagnosability F8 asks for.
- **`fmt.Errorf` allocates a new error per wrapped failure** (not per success): only on already
  failing paths — no hot-path cost.
- **The verdict cascade set (25 rows) is the judgment boundary.** It is defined by the source of the
  error value (validation/derivation call over control state) and corroborated by the four
  byte-exact pins; a reviewer can re-derive the boundary from the sweep's `-all` output.

## Delivered outcome (2026-11-21)

Delivered tree = base `52bdffd2` plus the changes listed at the top of this plan (at implementation
time uncommitted; committed for review as `71e0deb3` and merged as `fd460635`); the base worktree
used for the red runs is a detached `git worktree` of the same commit.

**Sweep after** — exact stdout of `sweep state.go provider_action.go` at the delivered tree (same
columns as the before block):

```
provider_action.go:298	row	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
provider_action.go:300	row	static-new	normalizeProviderActionResult	tokenErr != io.EOF	errors.New("provider action result has trailing data")
state.go:1241	row	static-new	(*Store).LoadEnrollmentIntent	err != nil	errors.New("invalid enrollment intent")
state.go:1289	row	static-new	(*Store).LoadIdentity	err != nil	errors.New("invalid identity record")
state.go:1422	row	static-new	(*Store).ReserveLinuxCloneIntent	err != nil || intent.RunID != key.RunID || intent.Generation != key.Generation	errors.New("Linux clone intent does not match RunKey")
state.go:1464	row	static-new	(*Store).BindLinuxCloneSeed	err != nil || seed.BaseCommit != intent.BaseCommit	errors.New("Linux clone seed receipt does not match its intent")
state.go:1539	row	static-new	(*Store).BindLinuxCloneReuse	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse grant does not match the new RunKey")
state.go:1707	row	static-new	(*Store).AbortLinuxCloneReuseBeforeNativeStart	err != nil || grant.RunID != key.RunID || grant.Generation != key.Generation	errors.New("Linux clone reuse abort grant does not match the RunKey")
state.go:1796	row	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Validate() != nil || candidate.ResourceID != intent.ResourceID || len(candidate.Commit) != len(intent.BaseCommit) || receipt.Version != candidate.Version || receipt.Commit != candidate.Commit || receipt.TreeDigest != candidate.TreeDigest	errors.New("Linux clone promotion receipt does not match candidate intent")
state.go:1800	row	static-new	(*Store).RecordLinuxClonePromotion	err != nil || receipt.Ref != ref	errors.New("Linux clone promotion ref does not match RunKey")
state.go:1960	row	static-new	(*Store).BeginLinuxCloneCleanup	err != nil || !journal.LinuxCloneCleanupIntent.Equal(intent)	errors.New("Linux clone cleanup intent readback is invalid")
state.go:2014	row	static-new	(*Store).RecordLinuxCloneTargetRemoval	err != nil || proof.Validate() != nil || proof.Disposition != authority.LinuxCloneTargetDispositionRemoved || !proof.TargetNlinkZero || !proof.Intent.Equal(expected)	errors.New("Linux clone target-removal proof does not match cleanup intent")
state.go:2045	row	static-new	(*Store).CompleteLinuxCloneCleanup	err != nil || proof.Validate() != nil || !proof.Intent.Equal(expected)	errors.New("Linux clone cleanup proof does not match cleanup intent")
state.go:2397	row	static-new	(*Store).readJournalEntryLocked	err != nil	errors.New("invalid run journal")
state.go:2666	row	static-new	(*Store).CheckLinuxV2NativeAdmission	err != nil || !validLinuxV2AdmissionFence(key, fence) || now.IsZero()	errors.New("Linux v2 native admission RunKey, fence, or time is invalid")
state.go:3035	row	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || expected.CreateStarted || expected.LeafIdentity != nil || expected.Helper != nil || expected.Launcher != nil || expected.LauncherExit != nil || expected.Gate != nil || expected.HelperExit != nil || expected.AbortProof != nil || expected.PreGateStopReceipt != nil || expected.LauncherGrantPossible || expected.Mirror == nil	errors.New("Linux cgroup reservation is not a never-created state")
state.go:3046	row	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	err != nil || !linuxCgroupReservationAbortMatches(expected, proof)	errors.New("Linux cgroup never-created tombstone is invalid")
state.go:4860	row	static-new	(*Store).loadJournalLocked	err != nil	errors.New("invalid run journal")
state.go:4904	row	static-new	requireLinuxCloneJournalFence	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("Linux clone intent does not match the current admitted Subject")
state.go:5030	row	static-new	validateLinuxCloneReuseRetainedTarget	err != nil	errors.New("run journal Linux clone reuse does not match admitted Subject")
state.go:5052	row	static-new	validateLinuxCloneIntent	err != nil	errors.New("run journal Linux clone intent does not match admitted Subject")
state.go:5157	row	static-new	validateLinuxClonePromotion	promotion.Validate() != nil || err != nil || promotion.Ref != ref || promotion.Commit != promotionIntent.Commit || promotion.TreeDigest != promotionIntent.TreeDigest || !linuxCloneStopProven(journal)	errors.New("run journal Linux clone promotion receipt is invalid")
state.go:5181	row	static-new	validateLinuxClonePromotionWithoutIntent	promotion.Validate() != nil || refErr != nil || promotion.Ref != legacyRef || len(promotion.Commit) != len(intent.BaseCommit) || !linuxCloneStopProven(journal) || journal.WorkspacePath != intent.Target	errors.New("run journal legacy Linux clone promotion receipt is invalid")
state.go:5196	row	static-new	validateLinuxCloneCleanup	err != nil || !cleanupIntent.Equal(*journal.LinuxCloneCleanupIntent)	errors.New("run journal Linux clone cleanup intent is invalid")
state.go:5294	row	static-new	requireLinuxCloneJournalFenceSubject	err != nil || !present || admission.Subject.Validate() != nil || admission.Subject.ResourceID != intent.ResourceID || admission.Subject.Commit != intent.BaseCommit || admission.Subject.TreeDigest != intent.BaseTreeDigest	errors.New("admitted Subject does not match Linux clone intent")
state.go:5370	row	static-new	decodeJSONWithLimitAndTags	err != io.EOF	errors.New("decode " + resource)
state.go:5393	row	static-new	rejectCaseInsensitiveJSONTagAliases	err != io.EOF	errors.New("trailing JSON data")
state.go:6073	row	static-new	validateContainmentAuthority	err != nil || journal.ContainmentAuthority.TargetPID != journal.PID || journal.ContainmentAuthority.TargetIdentity != journal.ProcessIdentity	errors.New("run journal containment authority is invalid")
state.go:6096	row	static-new	validateContainmentHandoff	err != nil	errors.New("run journal containment handoff is invalid")
TOTAL reported 29 rows
  rows 29 (static-new 29, static-fmt-no-w 0, sentinel-return 0)
```

28 base rows left the row class: 26 of the 32 treated rows are wraps (20 WRAP + 6 SPLIT arms), the
other 6 are split sentinel conflations, and 4 arms that stayed static are offsets because they now
run only when no cause exists (2 in `provider_action.go`, 2 in `state.go`; evidence record §2.4):
32 − 4 = 28. Statement level, of those 28: 16 have no static error return at their site any more
(the arm is now a `%w` wrap — the 16 `state.go` WRAP rows of §2.1 minus the two SPLIT rows), and 12
still return the same message under a narrowed condition (the 6 capacity sentinels + the 6 SPLIT
predicate arms); two of the 12 (base `provider_action.go:571`/`:592`) land on the one shared
`validateProviderActionJournalSize` size arm, so the listing gains 11 entries (+3
`other-branch`, +8 `no-branch`), not 12.
`sentinel-return` is 0. The 25 remaining `state.go` rows are the verdict cascades the contract
declares residual. `sweep -all`: row 29 / pred 197 / other-branch 39 / no-branch 267 (base
57 / 197 / 36 / 259). `sweep -selfcheck` exits 0.

Apply-stage update (2026-11-21, rf-w3-08 apply stage): the accepted simplify findings S1
(collapsed trailing-data arms), S3 (`serializeRunJournalWithinLimit`) and S4 (propagated
alias-check cause) restructured three of these rows. The block above stays as the recorded
implementation-stage stdout of the tree it names; the republished apply-tip stdout and counts —
`rows 28 (static-new 28, …)`, `sweep -all`: row 28 / pred 196 / other-branch 39 / no-branch 265,
raw listing total 528 — are in
`docs/evidence/rf-w3-07-state-error-causes-2026-11-21.md` §1 and §12 (Appendix B carries the
apply-tip listing).

Moved (2026-11-22): the split of PR #214 moved this apply-stage set to the W3-07 follow-up branch
`refactor/rf-w3-07b-state-error-followups`; the recorded counts reproduce unchanged there and the
cell shifts are named in the evidence record's §12 move note.

**Tests added**: `state_wrap_test.go` (`TestSaveJournalWrapsAtomicWriteCause` with the run-journal
and goal-session lanes, `TestLoadJournalWrapsDecodeCause`, and — fix round 1 —
`TestMarkGoalSessionUncertainReasonBudget`) and `provider_action_test.go`
`TestProviderActionCapacitySplitSeparatesSerializationFromSize` (serialize arms leave the
sentinel; size arms return the sentinel value itself).

**Gates** (exact commands, counts and exit codes: evidence record §5): `gofmt -l` 0 · `go vet
./...` 0 · `GOOS=windows go build ./...` 0 · `GOOS=linux go build ./...` 0 · `GOOS=linux
GOARCH=amd64 go vet ./internal/state/` 0 · state suite Windows 0 · state suite WSL
Ubuntu-24.04 0 · targeted app oracle 0 · app provider-bridge subset 0 · `git diff --check` 0 ·
host-path scan clean.

**Mutation checks**: (a) `TestSaveJournalWrapsAtomicWriteCause` red on base (both lanes), green
on head, red under the `%v` flip (reverted); (b) dispatch-use removal reddens
`provider_action_test.go:615`, save-use removal reddens `:1257` — both uses load-bearing.
Measured divergence from Change 2's text, **ratified by the conductor on 2026-11-21 (fix round
1)**: the dispatch-use removal does **not** redden `:479` at this head. `:479` binary-searches for
the payload size at which the bare candidate already exceeds the 4 MiB budget, so the plain size
check catches it with or without the reservation; `:528`/`:590` are budget/encoding assertions as
the contract says. The ratified reservation-sensitive set is `:615`/`:1257`; `:479` is a size
assertion. Recorded as measured instead of fitting a test to the claim; no test was changed.

**Judging artifacts**: this plan (rule, command, before output, classification table,
control-flow audit) and `docs/evidence/rf-w3-07-state-error-causes-2026-11-21.md` (verbatim
before/after sweep listings, control-flow audit, durable-text evidence, gate exit codes, mutation
runs, Appendix A = sweep tool source, Appendix B = sweep listings). `docs/TODO.md` records the one
dead-code candidate the audit surfaced (the decorative `token != nil` disjunct kept for
control-flow preservation).

## Fix round 1 (2026-11-21) — review findings W307-R1…W307-R6

Review verdict on `fc8e95a2..fd460635`: `MUST_FIX (5)` (R1 P2 + R2–R5 P3, R6 P3 optional
hardening), reconciled by the conductor's binding decision for this round. This round changed the
two state test files and these two records only; **no production file changed**, so the before/after
listings above and every classification in this plan and in the evidence record stay valid.
(Line refs below resolve in the delivered tree.)

- **W307-R1 (behaviour preservation, binding) — FIXED in fix round 2 under the conductor's
  owned-set extension** (round 1 recorded this unmet inside the strict owned set; the conductor's
  W307-R1 decision then extended the owned set to the composition sites and authorised the app-side
  remedy — see "Fix round 2" below for the changed lines, the test and the red/green runs). The
  suffix this slice adds reaches the app's composed
  `UncertainReason` (`daemon/internal/app/goal_admission.go:1410-1415` and `:1468-1473`, plus the
  other error-text-fed compositions at `goal_admission.go:889`, `:950`, `native_run.go:611`, and
  `goal_delivery.go:134`/`:138`/`:142` via `native_run.go:846` → store call `:850`) and the store's
  budget is enforced at
  `goal_session.go:1052-1053` (write) and `:1688` (read), so a large inner cause can push a composed
  reason over 4096 bytes and make `MarkGoalSessionUncertain` reject, leaving the uncertain mark
  unrecorded where the base revision recorded it. Measured at the state boundary with the app's
  exact composition (scratch measurement, repo untouched; the budget itself is pinned by
  `TestMarkGoalSessionUncertainReasonBudget`): prefix
  `native Goal session did not reach a known stopped state` = 55 B, base cause text
  `write goal session journal` = 26 B → base composed reason 83 B, recorded; head composes
  `85 + N` B for an `N`-byte inner writer cause and flips to `uncertain launch reason is invalid` at
  `N ≥ 4012` (`N = 4011` → 4096 B accepted; `N = 4012` → 4097 B rejected). In-set round-1
  artifact: `state_wrap_test.go:TestMarkGoalSessionUncertainReasonBudget` (exactly 4096 B accepted,
  persisted and loadable; 4097 B rejected before the mutation with the journal file byte-unchanged)
  — it pins the budget and the no-side-effect property. The remedy this round applies (rune-safe
  truncation of the reason handed to `MarkGoalSessionUncertain`, budget single-sourced from `state`,
  never the returned/joined error chains) and the app test that observes the app outcome the
  state-package test cannot are recorded in "Fix round 2" below.
- **W307-R2 — fixed** in the evidence record §1: the class arithmetic is now the measured one
  (`row` 57 → 29, `pred` 197 → 197, `other-branch` 36 → 39, `no-branch` 259 → 267, raw total
  549 → 532 = 28 removed − 11 added; statement level 16 arms became `%w` wraps, 12 were narrowed,
  2 of them converging on the shared size arm).
- **W307-R3 — fixed**: the "only class that can drop a cause" / "all proven no-cause" phrasing is
  replaced by the rule boundary (err-ish identifier test) in the accounting section and by
  "excluded by the recorded counting rule" in evidence §2.4, with the call-shaped `pred` counter-
  examples and the residual rows' real causes stated.
- **W307-R4 — fixed**: both output blocks are now the tool's exact stdout (columns `file:line`,
  class, kind, function, condition, returned expression; summary lines after the rows — verified
  byte-identical against the recorded run), the two `provider_action.go` WRAP rows name
  `normalizeProviderActionResult`, and the evidence's split-capacity head cells cite the `return`
  statements, the same convention as §2.1/§2.2 and the listings.
- **W307-R5 — fixed**: the `saveJournalLocked` boundary row is split at the atomic rename
  (pre-rename: previous journal remains; post-rename: `writeAtomic` still returns
  `syncDirectory(directory)`, `state.go:5620`, so a failure there or a crash between the steps is
  committed-but-unacknowledged and must be settled by read-back), and the same two-phase window is
  noted for `writeJSONWithLimit`.
- **W307-R6 — fixed (hardened)**: the decode pin anchors the prefix
  (`strings.HasPrefix`) and the cause identity (`errors.As` → `*json.UnmarshalTypeError`,
  `Value == "array"`); the unreachable equality assertion is gone; the provider serialize arm asserts
  `errors.As` → `*json.SyntaxError` instead of mere chain non-emptiness. Scratch-copy mutations
  (repo untouched, reverted): wrong prefix at `state.go:5363` → red at `state_wrap_test.go:91`; a
  non-nil but wrong cause at `state.go:5363` → red at `:95`; a non-nil but wrong cause in
  `validateProviderActionJournalSize` → red at `provider_action_test.go:1442`.
- **P3 — conductor ratification recorded** (measured divergence: only `:615`/`:1257` are
  reservation-sensitive; `:479` is a size assertion), and the mutation-check expectations in
  "Evidence plan" item 3 were corrected to that measured set.

## Fix round 2 (2026-11-21) — W307-R1 remedy (conductor owned-set extension)

The conductor's W307-R1 decision (owned-set extension, binding) authorised the app-side remedy and
kept the store sink unchanged (a sink clamp would alter other callers). Changed files; line refs
resolve in this round's tree:

| file | change |
|---|---|
| `daemon/internal/state/state.go:42-46` | exported `GoalSessionUncertainReasonBytes = 4096`, the budget's single source |
| `daemon/internal/state/goal_session.go:1052,:1688` | the two enforcement points reference the constant (pure constant references, per the decision) |
| `daemon/internal/app/goal_admission.go:1308-1327` | `boundedGoalSessionUncertainReason` — rune-safe truncation at the budget |
| `daemon/internal/app/goal_admission.go:890,:951,:1443,:1498` | the bound applied to the string handed to `MarkGoalSessionUncertain` (start-failure compositions, `abandonGoalSession`, `abandonGoalSessionUncertain`) |
| `daemon/internal/app/native_run.go:611,:850` | the bound applied (`resolveCancelledNativeStartAbortFailure`; `publishNativeCloseRetryAfterEvidence`, the handoff shared with `goal_delivery.go:134`/`:138`/`:142`) |
| `daemon/internal/app/goal_delivery_test.go:640-678` | `TestAbandonGoalSessionBoundsOversizedUncertainReason` |
| `docs/evidence/rf-w3-07-state-error-causes-2026-11-21.md` §10 | fix-round evidence: red/green runs, commands, exit codes |

**Superseded in fix round 3 (PR #200 review):** the helper row's `:1308-1327` span and "rune-safe
truncation" description, the `:890,:951,:1443,:1498` call-site refs (delivered:
`:889,:950,:1437,:1492`), and the `:640-678` test span describe round 2's bytes only; the
delivered helper (`:1307-1321`) normalizes the over-budget prefix with `strings.ToValidUTF8`, and
the delivered tests are `goal_delivery_test.go:640-714` and `:716-774`. The green bullet's "only
the rune-boundary walk…" sentence is likewise round-2 history. See "Fix round 3" below.

Only the reason string handed to the store is bounded; every returned/joined error chain is
unchanged, no message prefix or verdict changed, and the store-side check is the same check with the
same value (now the constant). Round-2 runs:

- **Red without the bound**: `go test -count=1 ./internal/app/ -run
  '^TestAbandonGoalSessionBoundsOversizedUncertainReason$'` with `boundedGoalSessionUncertainReason`
  temporarily reduced to `return reason` → `FAIL` at `goal_delivery_test.go:660`, exit 1: the
  4,313 B composed reason is rejected by the store, the journal stays `LaunchState:"attached"`,
  `UncertainReason:""`.
- **Green with it**: same command with the bound in place → `PASS`, exit 0; the durable reason is
  4,095 B and valid UTF-8, and keeps the composed prefix and the writer-cause head. The budget cut
  lands mid-rune at 4,096 B, so only the rune-boundary walk keeps the stored text valid for the
  store's JSON round-trip. (Superseded in fix round 3: the delivered bound normalizes the whole
  over-budget prefix with `strings.ToValidUTF8` — see below.)
- **Gates**: `gofmt -l internal/app internal/state` 0 · `go vet ./...` (windows) 0 ·
  `GOOS=linux go vet ./...` 0 · `go test -count=1 ./internal/state/` (windows) 0 · WSL
  `Ubuntu-24.04` `go test -count=1 ./internal/state/...` 0 · app subset (`TestAbandonGoalSession*`
  plus the close/abort/lease composition tests and the slice's app oracle) 0 · `git diff --check` 0 ·
  `check-record-cleanliness.py` on the committed head 0.

## Fix round 3 (2026-11-21) — review #200 fix record (docs only)

PR #200 review round 2 (`200-opus-review-2.md`) verified the P2-1 fix on `f150ed2a` (parent
`848dee6c`) and asked for the slice's records to stop describing fix round 2's superseded tail-only
walk. This section records the delivered bytes; no code changed this round, and the line refs below
resolve at the fix-round-3 head.

| file:line | change |
|---|---|
| `daemon/internal/app/goal_admission.go:1307-1321` | over-budget branch: `strings.ToValidUTF8(reason[:state.GoalSessionUncertainReasonBytes], "")`; the `DecodeLastRuneInString` walk and the `unicode/utf8` import are removed; the within-budget branch stays byte-identical |
| `daemon/internal/app/goal_delivery_test.go:640-714` | `TestBoundedGoalSessionUncertainReason` — 10-case table, exact string equality, `utf8.ValidString` on every over-budget case |
| `daemon/internal/app/goal_delivery_test.go:716-774` | `TestAbandonGoalSessionBoundsOversizedUncertainReason` — two cases; the interior-`\xff` case asserts `LoadGoalSession` succeeds afterwards |
| `docs/TODO.md` "Deferred review findings (PR #200)" | deferred store-side hazard: within-budget invalid UTF-8 can expand past the budget in the JSON round trip (`goal_session.go:1052` write vs `:1688` read); owner: state store; base `52bdffd2` affected |
| `docs/evidence/rf-w3-07-state-error-causes-2026-11-21.md` §11 | the same fix-round record with the red/green transcripts |

Red/green: against the `848dee6c` tail-only helper the new tests fail — table cases at
`goal_delivery_test.go:707` (exact-equality) and the abandon interior case at `:751`
(`LoadGoalSession() error = invalid goal session journal`) — exit 1; with the bound both pass, exit
0 (`200-fix-r1.md` §2 carries the transcripts; review round 2 re-ran the reproduction on the
delivered head: bounded len 4,095, mark accepted, load succeeds, session `uncertain`). Gates for
the unchanged code bytes: `gofmt -l internal/app internal/state` 0 · `go vet ./internal/app/
./internal/state/` (windows) 0 · `GOOS=linux go vet ./internal/app/ ./internal/state/` 0 · targeted
app subset 13/13 PASS, exit 0 · `git diff --check` 0 · `check-record-cleanliness.py` on the
committed head 0.
