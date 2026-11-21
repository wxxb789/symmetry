# rf-w3-07-state-error-causes evidence (2026-11-21)

Contract: `goal-rf-w3-07-state-error-causes.md` (GDD, 2026-11-21) with the conductor reconciliation
(A6/P2-1–P2-3/P3, D1–D6) and the program-wide "lesson 32" control-flow rule.
Plan: `docs/plans/2026-11-21-rf-w3-07-state-error-causes-plan.md`.
Base revision: `52bdffd25e46753136cf892a70cfa05e43bc01c8` (`refactor(state): decompose the journal
validators (#165)` — W2-09 merged, so the contract's dispatch gate is met by the branch point).
Delivered tree: the base commit plus the uncommitted working-tree changes listed below; every
`<file>:<line>` in this record resolves in that tree unless marked "base".

## 0. Change inventory

| file | state | lines |
|---|---|---|
| `daemon/internal/state/state.go` | modified | +34 / −19 (21 of 46 swept rows) |
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

**After (delivered tree):** `TOTAL reported 29 rows` — `rows 29 (static-new 29, static-fmt-no-w 0,
sentinel-return 0)`: `sentinel-return` is zero, i.e. every sentinel conflation is split and no
`Err*` value is returned from an error branch any more. The 29 residual rows are listed in §2.4.
Verbatim listings with each row's condition and returned expression: Appendix B.

**Accounting (`-all`).** Every static error return in both files is classified:

| class | base | delivered | meaning |
|---|---:|---:|---|
| `row` | 57 | 29 | err-ish identifier in the branch condition; the only class the rule sweeps |
| `pred` | 197 | 197 | condition tests calls/fields against nil and mentions no err-ish identifier |
| `other-branch` | 36 | 39 | inside `for`/`switch`/`select` or a non-error `if`; no error value |
| `no-branch` | 259 | 267 | no enclosing branch (scalar/state verdicts) |

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
| `state.go:5364` | `:5366` | `decoder.Decode != io.EOF` (guarded arm) — `decode %s` |
| `state.go:5384` | `:5389` | `decoder.Decode != io.EOF` (guarded arm) — `trailing JSON data` |
| `state.go:5486` | `:5498` | `store.atomicWrite`/`writeAtomic` — `write %s` |
| `state.go:5514` | `:5526` | `os.Remove` — `remove run journal temporary file` |
| `state.go:6363` | `:6375` | `json.Unmarshal` — `completed terminal payload is invalid` |
| `provider_action.go:288` | `:295` | `decoder.Token` — `provider action result has trailing data` |
| `provider_action.go:290` | `:300`-block | `decoder.Token` — `provider action result has trailing data` |

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
| `state.go:5305` | `:5305`/`:5308` | `serialize run journal: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:267` | `:268`/`:271` | `serialize provider action result: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:541` | `:560`/`:563` | `serialize provider action intents: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:571` | `:597`/`:600` | `serialize run journal: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:592` | `:605` → `:627` (shared `validateProviderActionJournalSize` on the reserved copy; serialize arm `:624`) | `serialize run journal: %w` | `ErrProviderActionCapacity` |
| `provider_action.go:613` | `:624`/`:627` | `serialize run journal: %w` | `ErrProviderActionCapacity` |

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
failure is dropped (`rejectCaseInsensitiveJSONTagAliases(data, rootType) != nil`,
`protocol.RejectDuplicateJSONMembers(result) != nil`) sit in the `pred` class, and the 25 verdict
cascades below do carry a cause from the `X.Validate()`/`parseX` call in their condition — they are
residual because the returned message is that stage's verdict, not an operation failure.

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
error), `provider_action.go:298` and `:300` (the `tokenErr == nil` arms of the trailing-data
block: a trailing token with no decoder error).

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
| WSL `Ubuntu-24.04` `accept` — `go test -count=1 ./internal/state/...` at `/mnt/q/repos/wt-w3-07-state-error-causes/symmetry/daemon` (go1.27.0 linux/amd64) | 0 | `ok … 20.386s` (the `//go:build linux` clone/cgroup suites run) |
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

After (delivered tree):

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
