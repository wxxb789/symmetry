# rf-w4-12b `apptest.FaultStore` plan — one store double replaces the 25 nil-in-production `options` mirrors

Slice: W4-12b (rf-w4-12b-app-fault-store; arch-decision-2 R1/R2). Contract:
`goal-rf-w4-12b-app-fault-store.md` (HOLD lifted by the conductor dispatch of 2026-11-22), its
verification amendments and the comment policy of 2026-11-22.

Dispatch HEAD: `2db5ed0c` (`refactor(app): put the journal store behind a narrow interface (#244)`,
W4-12a merged). Lane position: W4-17a → W4-17b → W4-01 → W4-12a → **W4-12b** → W4-12c → W4-22.

## Measured basis (at dispatch HEAD `2db5ed0c`)

Command: `rg -n "options\.(<25 mirrors>)\b"` over `daemon/internal/app` (one statement; the mirror
name list is the `options` struct block `app.go:206-230`).

- Mirror fields: 25 (`recordWorkspace` … `clearProcessDetails`), `app.go:206-230`.
- Non-test reads: **41** in 9 files — `app.go` 4, `cleanup_glue.go` 13, `goal_admission.go` 4,
  `goal_delivery.go` 2, `native_run.go` 4, `outbox.go` 8, `recovery.go` 4, `recovery_glue.go` 1,
  `supervisory.go` 1.
- Override plumbing: `storeCallbackWrite` (constraint) + `selectStoreWrite` (8 call sites in
  `cleanup_glue.go`), the two inline override branches (`persistProcessAuthorityCallback`,
  `persistProcessMarker`), plus 12 `X := options.<mirror>; if X == nil { X = store.Method }` sites
  and 6 direct `if options.<mirror> != nil { … } else { store… }` sites.
- Test-side mirror writes: **88** sites in **20** app test files (drafting census said ≈80/17).
- Seam doubles: 83 app test types implement ≥1 of the three seam method sets. Receiver-method
  counts at base (one statement, `git grep -o "func ([^)]*) <Method>(" -- 'internal/app/*_test.go' |
  sort -u | wc -l`, module-wide over `internal/**/*_test.go`): `Transition` 24/24, `Wait` 26/35,
  `ProcessDetails` 24/32, `RenewLease` 18/21, `AcknowledgeCommand` 15/15, `AppendEvents` 17/17,
  `Terminate` 19/37, `WriteInput` 19/21, `Claim` 10/10, `RegisterSession` 10/10, `Reconcile` 12/12,
  `Dispatch` 7/7, `Heartbeat` 6/6, `Handle` 0/19 (the sink double is new infrastructure).

## Deliverable shape (public/API shape)

1. New package `daemon/internal/app/apptest` (non-test; imports `internal/state`,
   `internal/authority`, `internal/protocol`, `internal/execution`, `internal/harness`; never
   `internal/app` or an importer of it):
   - `faultstore.go`: `FaultStore` embeds `*state.Store` and carries `OnX` hooks for the **25**
     Store write methods the mirrors covered. A nil hook delegates to the embedded method with the
     same arguments; a set hook replaces the call (exactly the mirror's semantics today).
     `NewFaultStore(directory)` opens a real Store over a test temp directory (`state.New`);
     `NewFaultStoreFrom(*state.Store)` wraps an already-open Store.
   - `control.go`: `ControlAPI` double, one `OnX` hook per method, defaults = `fakeControl`'s
     permissive behaviour (the majority fake), per-method call recording with copy-out accessors.
   - `process.go`: `Process` double, hooks plus `Result`, default `ProcessDetails` = `(42,
     "test:42")` before termination and `(0, "")` after it; `KeepPIDAfterTerminate` reproduces
     `fakeProcess`'s always-live identity.
   - `eventsink.go`: recording `harness.EventSink`; every recorded event is a detached copy.
   - focused unit tests per double with the four mutants (a)-(d) run red.
2. Production deletion (`app.go`, `cleanup_glue.go`, `goal_admission.go`, `goal_delivery.go`,
   `native_run.go`, `outbox.go`, `recovery.go`, `recovery_glue.go`, `supervisory.go`): the 25
   mirror fields, every `options.<mirror>` read and nil check, the override half of
   `selectStoreWrite`/`storeCallbackWrite`, and both inline override branches. The store-lifetime
   read lock, the `storeClosing` refusal and `errDaemonStoreClosing` stay byte-identical; each
   removed branch is dead in production (mirror nil there) and the store-side path stays.
3. Test migration: each mirror-setting test configures the matching `FaultStore` hook; assertions
   and subtest names unchanged. A test that ran with `store == nil` plus a mirror now runs over a
   real temp Store (divergence rows in the evidence record). Mirror-based storeless probes in
   `journal_store_test.go` pin removed mechanism and are replaced by FaultStore probes.
4. Shared doubles for the real seams: per-file fakes that are pure hook configurations migrate to
   the doubles; fakes whose state or channel choreography is read by existing assertions stay
   local and get a divergence row (they cannot move without changing an assertion: their fields
   are lowercase in package `app`, a cross-package double cannot expose them).
5. App test file `apptest_contract_test.go` asserts `var _ journalStore = (*apptest.FaultStore)(nil)`
   (the interface stays unexported).

## Contract-preserving decisions (recorded, not guessed)

### D1 — `selectStoreWrite` keeps its name and loses only the override

`storeCallbackWrite` and the `override Fn` parameter are deleted; the generic constraint becomes
`any` and the function returns the bound store method under the same read lock and the same
`!storeClosing && store != nil` predicate. Alternative (inline the lock into 8 callbacks) rejected:
it would spread one lock/refusal rule over 8 sites and change the lock-site audit surface for no
behavioural gain. Gate reading: "`selectStoreWrite` override branch and `storeCallbackWrite` gone"
= the override branch is gone (`rg -n 'storeCallbackWrite|override'` = 0), the function survives.

### D2 — `withStore` takes the seam type (withdrawn at apply, 2026-11-22)

Superseded by the applied simplify finding S3: the widened `withStore(store journalStore)` had no
caller passing a `FaultStore` (all 12 call sites pass `*state.Store` or an untyped nil) and the
interface parameter weakened the helper's own `store == nil` guard, because a typed-nil
`*state.Store` would become a non-nil interface. `export_test.go` keeps
`withStore(store *state.Store)`: `withStore(nil)` still writes an untyped nil (W4-12a D6 probe
stays green); `*state.Store` callers compile unchanged. The widening can return when a `Run`-based
test actually injects a `FaultStore`.

### D3 — mirror branches are dead in production, the store path is kept verbatim

For all 41 reads the removed branch is the mirror branch. Where the mirror changed the *sequence*
of store calls (`queueNativeUnknownUsageRecovery`, `queueNativeGoalUsageSettlement`, `outbox.go`
`cancelled` special case), the deleted branch is the one production never takes, and the retained
branch keeps today's call, arguments, order and error wrapping. One evidence row per removed
branch.

### D4 — FaultStore hooks are per Store method, not per mirror site

Two sites share `SetProcessDetails` (`app.go:1633`, `cleanup_glue.go:84`) and two share
`QueueTerminalTransitionAt` (`outbox.go:142`, `recovery.go:760`); one hook per method is the
contract's shape ("one optional hook per Store write method the 25 mirrors cover today"). A test
that used the mirror to intercept only one of two call sites keeps its assertion because the hook
is installed on the same daemon's only store.

### D5 — storeless mirror tests become real-Store tests, never assertion changes

A migrated test that previously reached a `store == nil` shortcut now takes the production path.
Where the observed error/value changes (`persistProcessMarker`'s readback, `recovery_glue.go`'s
store-nil refusal position) the row is listed as a divergence with the base and HEAD outcome;
no assertion text changes. Delivered reading of Design 4 (which supersedes the deletion option
this plan originally sketched): no test or subtest is deleted — the three affected
`journal_store_test.go` subtests install a real `FaultStore` and keep their names; the lost
`store == nil` side stays covered by the sibling probes of the same test, which predates the
slice (evidence §5a).

### D6 — `fakeProcess` keeps a live identity through the shared double

`apptest.Process{KeepPIDAfterTerminate: true}` reproduces `fakeProcess` exactly (Wait returns
`Result`, ProcessDetails stays `(42, "test:42")` after Terminate, WriteInput/Terminate succeed).
The default (pid dies with the process) is what mutant (c) pins.

### D7 — per-file fakes stay local; `fakeProcess` is expressible but out of the owned file set

Every migrated control/process fake's observation surface is private state or channel
choreography read by unchanged assertion lines, so `apptest` cannot carry them without an
assertion edit. `fakeProcess` is the one expressible case, but its 46 use sites span nine files
and four of those (`decision_requirement_test.go`, `deterministic_validation_test.go`,
`generic_process_stop_test.go`, `recovery_isolation_test.go`) set no mirror and declare no
migrated fake, i.e. they fall outside this contract's STRICT owned set. It therefore stays local
with a divergence row (evidence §5c) instead of widening the file set; the double's identity
behaviour is pinned by `process_test.go` and mutant (c).

## Preserved semantics

- Same `*state.Store` method per call, same arguments, same order, same error wrapping and the
  same returned error identities; `storeLifetimeMu` lock order and `storeClosing` refusal
  unchanged (one lock/refusal site, `selectStoreWrite`).
- Production never sets a mirror, so every removed branch is unreachable there: the retained path
  is byte-identical to today's fallback.
- Absent stores stay untyped nils: `&daemon{}` still takes every `store == nil` branch.
- CI-pinned test name `TestOpenCodeLegacyAssignmentFailsBeforeRunningWorkspaceAndProcess` and every
  existing subtest name stay untouched.

## Evidence plan (foreground; heavy suites serialized, never concurrent)

- Mirror census at HEAD = 0; `storeCallbackWrite`/`override` grep = 0; `selectStoreWrite` diff.
- Dead-in-production table (one row per removed branch: file, symbol, base predicate, production
  value, retained path).
- Divergence table per migrated fake and per storeless→real-Store test.
- Base-vs-HEAD control-flow audit per deleted fake (lesson 32).
- Receiver-method counts before/after, app-only and module-wide.
- Mutants: (a) control hook ignored, (b) sink copy dropped, (c) process pid kept after terminate,
  a store hook deleted, a store nil-guard flipped, `KeepPIDAfterTerminate` default flipped.
- `gofmt -l`, `git diff --check`, `go vet ./...`, `go build ./...`, `GOOS=linux|windows|darwin`
  builds; Windows `go test -count=1 -timeout 1200s ./internal/app/...` (the repo's CI budget,
  `ci.yml:681`; the default 10m timeout aborts this host at 601 s mid-suite with no failing test —
  observed, evidence §9); WSL
  `wsl -d Ubuntu-24.04 -u accept` `cd /mnt/q/repos/wt-w4-12b-app-fault-store/symmetry/daemon &&
  go test -count=1 -race -timeout 3000s
  ./internal/app/... ./internal/platform/... ./internal/execution/...`.
  (CI's race step keeps a 600 s budget on its runner, `ci.yml:1121`; this host's WSL race leg for
  `internal/app` needs more — the 1200 s attempt timed out on the same
  `TestDurablePiProviderBridgeExecutorReservesCompletionCapacityBeforeDispatch` that the Windows
  default-timeout attempt hit, evidence §9.)
- R2 coverage: `go test -cover` for `internal/app` before/after must not drop.
- R9: no declaration moves out of `app.go`/`cleanup_glue.go` (deletions only); if that changes,
  `SOURCE_PATHS` + `python3 scripts/ci/test-omo-control-native-evidence.py`.
- Lint ratchet note for `apptest` (compiled into normal builds; no `//nolint`).

## Out of scope (with reasons)

- W4-12c's Recorder port, W4-22's #185 seal, Linux v2 journal callbacks (W4-23), cross-package
  helper collisions, `daemon/internal/state/**`.
- Migrating per-file fakes whose assertions read their private state or channel choreography: the
  assertion-unchanged clause forbids it; each is named in the divergence table.

## Delivered revision

Uncommitted working tree over `2db5ed0c` (the implement stage leaves the tree for verification):
9 new files — `daemon/internal/app/apptest/` (8 files, 1,844 lines incl. tests) plus
`apptest_contract_test.go` (7 lines); production deletions in `app.go`, `cleanup_glue.go`,
`goal_admission.go`, `goal_delivery.go`, `native_run.go`, `outbox.go`, `recovery.go`,
`recovery_glue.go`, `supervisory.go`; test migration in 21 files; `export_test.go`'s `withStore`
keeps its `*state.Store` parameter (D2, withdrawn at apply; W4-12a D6 probe stays green). Census,
dead-in-production table,
divergence table, control-flow audit, mutants and gate outputs:
`docs/evidence/rf-w4-12b-app-fault-store-2026-11-22.md`.

## Apply pass (2026-11-22)

The simplify review (`.../2026-11-22/symmetry-rf-w4-12b-app-fault-store-simplify/answer.md`) was
applied on top of the delivered revision (implement commit `b7d42074`, reachable as `635c3f69`):
S1 (`harness_test.go` dead `wire` return), S2 + S9 (`nativeUsageSettlementHook` plus
`TestNativeUsageSettlementAttemptRouting`), S3 (`withStore` back to `*state.Store`; D2 withdrawn
above), S5 (`cleanup_test.go` fixtures take the `FaultStore` directly), S6 (`probeStore` helper),
S7 (`apptest/control.go` doc corrected), D1 and D2 record corrections. Rejected: S4 (needs
`supervisory_test.go` and `decision_requirement_test.go`, outside the STRICT owned set) and S8
(cross-file rename churn; every file already uses one name for the double, and the finding itself
recommends skipping). Gate outputs and the apply revision identity:
`docs/evidence/rf-w4-12b-app-fault-store-2026-11-22.md` §13.
