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
| `daemon/internal/state/state_wrap_test.go` | **new**: pins the exact wrapped strings, the `%w` chain (`errors.Is` through the writer seam), the decode wrap, and that genuine capacity still returns the sentinel value itself |
| `daemon/internal/state/provider_action_test.go` | **+1 test** (contract Change 2): the serialize-failure reclassification out of `ErrProviderActionCapacity` |

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
sweep -predicates state.go provider_action.go   # residual no-cause predicate class
sweep -all state.go provider_action.go          # complete accounting of every static error return
```

**Before output at the base revision** (`sweep state.go provider_action.go`):

```
TOTAL reported 57 rows
  rows 57 (static-new 51, static-fmt-no-w 0, sentinel-return 6)
provider_action.go:267	sentinel-return	ValidateNewProviderActionResult	ErrProviderActionCapacity
provider_action.go:284	static-new	normalizeProviderActionResult	errors.New("provider action result must be a JSON object")
provider_action.go:288	static-new	normalizeProviderActionResult	errors.New("provider action result has trailing data")
provider_action.go:290	static-new	normalizeProviderActionResult	errors.New("provider action result has trailing data")
provider_action.go:358	static-new	decodeProviderActionJSONValue	errors.New("provider action result array is invalid")
provider_action.go:368	static-new	decodeProviderActionJSONValue	errors.New("provider action result object key is invalid")
provider_action.go:382	static-new	decodeProviderActionJSONValue	errors.New("provider action result object is invalid")
provider_action.go:541	sentinel-return	validateProviderActionDispatchCapacity	ErrProviderActionCapacity
provider_action.go:571	sentinel-return	validateJournalSaveCapacity	ErrProviderActionCapacity
provider_action.go:592	sentinel-return	validateJournalSaveCapacity	ErrProviderActionCapacity
provider_action.go:613	sentinel-return	validateProviderActionJournalSize	ErrProviderActionCapacity
state.go:913	static-new	linuxCloneCleanupIntentFromJournal	errors.New("encode Linux clone cleanup fence")
state.go:1202	static-new	(*Store).Close	errors.New("release state directory lock")
state.go:1212	static-new	NewDaemonInstanceID	errors.New("generate daemon instance ID")
state.go:1224	static-new	NewMachineToken	errors.New("generate machine token")
state.go:1241	static-new	(*Store).LoadEnrollmentIntent	errors.New("invalid enrollment intent")
state.go:1270	static-new	(*Store).DeleteEnrollmentIntent	errors.New("remove enrollment intent")
state.go:1289	static-new	(*Store).LoadIdentity	errors.New("invalid identity record")
state.go:1422	static-new	(*Store).ReserveLinuxCloneIntent	errors.New("Linux clone intent does not match RunKey")
state.go:1464	static-new	(*Store).BindLinuxCloneSeed	errors.New("Linux clone seed receipt does not match its intent")
state.go:1539	static-new	(*Store).BindLinuxCloneReuse	errors.New("Linux clone reuse grant does not match the new RunKey")
state.go:1707	static-new	(*Store).AbortLinuxCloneReuseBeforeNativeStart	errors.New("Linux clone reuse abort grant does not match the RunKey")
state.go:1796	static-new	(*Store).RecordLinuxClonePromotion	errors.New("Linux clone promotion receipt does not match candidate intent")
state.go:1800	static-new	(*Store).RecordLinuxClonePromotion	errors.New("Linux clone promotion ref does not match RunKey")
state.go:1960	static-new	(*Store).BeginLinuxCloneCleanup	errors.New("Linux clone cleanup intent readback is invalid")
state.go:2014	static-new	(*Store).RecordLinuxCloneTargetRemoval	errors.New("Linux clone target-removal proof does not match cleanup intent")
state.go:2045	static-new	(*Store).CompleteLinuxCloneCleanup	errors.New("Linux clone cleanup proof does not match cleanup intent")
state.go:2316	static-new	(*Store).DeleteJournal	errors.New("delete run journal")
state.go:2335	static-new	(*Store).listJournalsLocked	errors.New("list run journal directory")
state.go:2365	static-new	(*Store).forEachJournalLocked	errors.New("list run journal directory")
state.go:2386	static-new	(*Store).forEachJournalLocked	errors.New("list run journal directory")
state.go:2397	static-new	(*Store).readJournalEntryLocked	errors.New("invalid run journal")
state.go:2666	static-new	(*Store).CheckLinuxV2NativeAdmission	errors.New("Linux v2 native admission RunKey, fence, or time is invalid")
state.go:3035	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	errors.New("Linux cgroup reservation is not a never-created state")
state.go:3046	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	errors.New("Linux cgroup never-created tombstone is invalid")
state.go:4860	static-new	(*Store).loadJournalLocked	errors.New("invalid run journal")
state.go:4904	static-new	requireLinuxCloneJournalFence	errors.New("Linux clone intent does not match the current admitted Subject")
state.go:5030	static-new	validateLinuxCloneReuseRetainedTarget	errors.New("run journal Linux clone reuse does not match admitted Subject")
state.go:5052	static-new	validateLinuxCloneIntent	errors.New("run journal Linux clone intent does not match admitted Subject")
state.go:5157	static-new	validateLinuxClonePromotion	errors.New("run journal Linux clone promotion receipt is invalid")
state.go:5181	static-new	validateLinuxClonePromotionWithoutIntent	errors.New("run journal legacy Linux clone promotion receipt is invalid")
state.go:5196	static-new	validateLinuxCloneCleanup	errors.New("run journal Linux clone cleanup intent is invalid")
state.go:5294	static-new	requireLinuxCloneJournalFenceSubject	errors.New("admitted Subject does not match Linux clone intent")
state.go:5305	sentinel-return	(*Store).saveJournalLocked	ErrProviderActionCapacity
state.go:5312	static-new	(*Store).saveJournalLocked	errors.New("write run journal")
state.go:5342	static-new	(*Store).readJSONWithLimitAndTags	errors.New("read " + resource)
state.go:5352	static-new	decodeJSONWithLimitAndTags	errors.New("decode " + resource)
state.go:5360	static-new	decodeJSONWithLimitAndTags	errors.New("decode " + resource)
state.go:5364	static-new	decodeJSONWithLimitAndTags	errors.New("decode " + resource)
state.go:5384	static-new	rejectCaseInsensitiveJSONTagAliases	errors.New("trailing JSON data")
state.go:5479	static-new	(*Store).writeJSONWithLimit	errors.New("encode " + resource)
state.go:5486	static-new	(*Store).writeJSONWithLimit	errors.New("write " + resource)
state.go:5514	static-new	(*Store).removeOwnedTempsLocked	errors.New("remove run journal temporary file")
state.go:6061	static-new	validateContainmentAuthority	errors.New("run journal containment authority is invalid")
state.go:6084	static-new	validateContainmentHandoff	errors.New("run journal containment handoff is invalid")
state.go:6363	static-new	terminalTaskResultKind	errors.New("completed terminal payload is invalid")
state.go:6373	static-new	terminalTaskResultKind	errors.New("completed terminal task result kind is invalid")
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
| `provider_action.go:288` | `decoder.Token` inside `providerActionJSONValue` | `provider action result has trailing data` |
| `provider_action.go:290` | `decoder.Token` inside `providerActionJSONValue` | `provider action result has trailing data` |

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
predicates (`X.Validate() != nil` cascades)"). Four of them are additionally pinned byte-exactly by
W2-09's first-error pins, and their cases *reach the error arm*, so wrapping them would break a
pinning test (proved by reading the pin case and by running it):
`state.go:6061`/`6084` in `TestJournalValidatorFirstErrorStringsArePinned` (`state_test.go:2532`,
`:2540`, `:2548` set `&authority.Supervisor{}` / `&authority.SupervisorHandoff{}`, whose
`Validate()` is the failing condition; the pins assert `err.Error() != test.want` at `:2914`),
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

- `pred` (197) — the "no-cause predicate" class: static returns inside a branch that tests only
  calls/fields/selectors against nil with no error variable in scope (`releaseProof.Validate() !=
  nil || …`, `journal.LinuxCloneAbortProof != nil || …`,
  `protocol.RejectDuplicateJSONMembers(result) != nil`, `if !ok`, …). Includes the drafting-time
  `provider_action.go:278` row. Contract Change 1 declares this class residual.
- `other-branch` (36) — static returns inside `for`/`switch`/`select` bodies or non-error `if`
  conditions (`if _, exists := seen[key]; exists`, `switch default:`, unknown-value arms), no
  error value in scope.
- `no-branch` (259) — static returns with no enclosing branch at all (unconditional verdicts or
  branches whose condition has no `!= nil` test, e.g. scalar/enum predicates
  `if journal.LocalState != "running" { … }`).

The `row` class is the only one whose condition can be satisfied by a non-nil error value that is
then dropped, so it is the class Change 1 wraps; `pred`/`other-branch`/`no-branch` have no
droppable error variable. After the change the `row` output is the residual 25-row verdict set
(pasted in the delivered-outcome section and in the evidence record).

## Exact semantics

1. **Prefixes are byte-identical.** Every wrapped message is `<existing message>: <cause>`; the two
   test-visible Contains assertions (`goal_session_test.go:281` `decode goal session journal`,
   `app/harness_test.go:3830` `write run journal`) match on the prefix and stay green. No test in
   the repository asserts any wrapped message by equality (checked repo-wide for all 21 messages:
   the only test literals are the two Contains calls above).
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
   `wsl -d Ubuntu-24.04 -u accept` on `/mnt/q/repos/wt-w3-07-state-error-causes/symmetry/daemon`;
   targeted app oracle
   `go test -count=1 ./internal/app/ -run '^TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure$'`;
   `git diff --check`; host-path scan of the branch diff. No `-race` locally (no cgo).
   Never concurrently with the sweep/measurement tool.
2. Sweep before/after (rows, predicates, all) + `-selfcheck`.
3. Mutation checks: (a) `TestSaveJournalWrapsAtomicWriteCause` red on base, green on head, red
   again with `%w` → `%v`; (b) dropping the reservation call from the dispatch guard reddens
   `provider_action_test.go:479`/`:615`, dropping it from the save guard reddens `:1257`
   (reservation-sensitive set as reconciled; `:528`/`:590` are budget/encoding assertions).
4. Evidence record `docs/evidence/rf-w3-07-state-error-causes-2026-11-21.md`: sweep listings, the
   57-row classification table with before/after, control-flow audit, gate exit codes, mutation
   outputs, Appendix A (sweep tool source), Appendix B (`-all` accounting).
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

Delivered tree = base `52bdffd2` plus the uncommitted changes listed at the top of this plan;
the base worktree used for the red runs is a detached `git worktree` of the same commit.

**Sweep after** (`sweep state.go provider_action.go` at the delivered tree):

```
TOTAL reported 29 rows
  rows 29 (static-new 29, static-fmt-no-w 0, sentinel-return 0)
provider_action.go:298	static-new	normalizeProviderActionResult	errors.New("provider action result has trailing data")
provider_action.go:300	static-new	normalizeProviderActionResult	errors.New("provider action result has trailing data")
state.go:1241	static-new	(*Store).LoadEnrollmentIntent	errors.New("invalid enrollment intent")
state.go:1289	static-new	(*Store).LoadIdentity	errors.New("invalid identity record")
state.go:1422	static-new	(*Store).ReserveLinuxCloneIntent	errors.New("Linux clone intent does not match RunKey")
state.go:1464	static-new	(*Store).BindLinuxCloneSeed	errors.New("Linux clone seed receipt does not match its intent")
state.go:1539	static-new	(*Store).BindLinuxCloneReuse	errors.New("Linux clone reuse grant does not match the new RunKey")
state.go:1707	static-new	(*Store).AbortLinuxCloneReuseBeforeNativeStart	errors.New("Linux clone reuse abort grant does not match the RunKey")
state.go:1796	static-new	(*Store).RecordLinuxClonePromotion	errors.New("Linux clone promotion receipt does not match candidate intent")
state.go:1800	static-new	(*Store).RecordLinuxClonePromotion	errors.New("Linux clone promotion ref does not match RunKey")
state.go:1960	static-new	(*Store).BeginLinuxCloneCleanup	errors.New("Linux clone cleanup intent readback is invalid")
state.go:2014	static-new	(*Store).RecordLinuxCloneTargetRemoval	errors.New("Linux clone target-removal proof does not match cleanup intent")
state.go:2045	static-new	(*Store).CompleteLinuxCloneCleanup	errors.New("Linux clone cleanup proof does not match cleanup intent")
state.go:2397	static-new	(*Store).readJournalEntryLocked	errors.New("invalid run journal")
state.go:2666	static-new	(*Store).CheckLinuxV2NativeAdmission	errors.New("Linux v2 native admission RunKey, fence, or time is invalid")
state.go:3035	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	errors.New("Linux cgroup reservation is not a never-created state")
state.go:3046	static-new	(*Store).AbortNeverCreatedLinuxCgroupReservation	errors.New("Linux cgroup never-created tombstone is invalid")
state.go:4860	static-new	(*Store).loadJournalLocked	errors.New("invalid run journal")
state.go:4904	static-new	requireLinuxCloneJournalFence	errors.New("Linux clone intent does not match the current admitted Subject")
state.go:5030	static-new	validateLinuxCloneReuseRetainedTarget	errors.New("run journal Linux clone reuse does not match admitted Subject")
state.go:5052	static-new	validateLinuxCloneIntent	errors.New("run journal Linux clone intent does not match admitted Subject")
state.go:5157	static-new	validateLinuxClonePromotion	errors.New("run journal Linux clone promotion receipt is invalid")
state.go:5181	static-new	validateLinuxClonePromotionWithoutIntent	errors.New("run journal legacy Linux clone promotion receipt is invalid")
state.go:5196	static-new	validateLinuxCloneCleanup	errors.New("run journal Linux clone cleanup intent is invalid")
state.go:5294	static-new	requireLinuxCloneJournalFenceSubject	errors.New("admitted Subject does not match Linux clone intent")
state.go:5370	static-new	decodeJSONWithLimitAndTags	errors.New("decode " + resource)
state.go:5393	static-new	rejectCaseInsensitiveJSONTagAliases	errors.New("trailing JSON data")
state.go:6073	static-new	validateContainmentAuthority	errors.New("run journal containment authority is invalid")
state.go:6096	static-new	validateContainmentHandoff	errors.New("run journal containment handoff is invalid")
```

28 base rows left the row class: 26 of the 32 treated rows are wraps (20 WRAP + 6 SPLIT arms), the
other 6 are split sentinel conflations, and 4 arms that stayed static are offsets because they now
run only when no cause exists (2 in `provider_action.go`, 2 in `state.go`; evidence record §2.4):
32 − 4 = 28.
`sentinel-return` is 0. The 25 remaining `state.go` rows are the verdict cascades the contract
declares residual. `sweep -all`: row 29 / pred 197 / other-branch 39 / no-branch 267 (base
57 / 197 / 36 / 259). `sweep -selfcheck` exits 0.

**Tests added**: `state_wrap_test.go` (`TestSaveJournalWrapsAtomicWriteCause` with the run-journal
and goal-session lanes, `TestLoadJournalWrapsDecodeCause`) and `provider_action_test.go`
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
Measured divergence from Change 2's text: the dispatch-use removal does **not** redden:
`:479` at this head. `:479` binary-searches for the payload size at which the bare candidate
already exceeds the 4 MiB budget, so the plain size check catches it with or without the
reservation; `:528`/`:590` are budget/encoding assertions as the contract says. Recorded as
measured instead of fitting a test to the claim; no test was changed.

**Judging artifacts**: this plan (rule, command, before output, classification table,
control-flow audit) and `docs/evidence/rf-w3-07-state-error-causes-2026-11-21.md` (verbatim
before/after sweep listings, control-flow audit, durable-text evidence, gate exit codes, mutation
runs, Appendix A = sweep tool source, Appendix B = sweep listings). `docs/TODO.md` records the one
dead-code candidate the audit surfaced (the decorative `token != nil` disjunct kept for
control-flow preservation).
