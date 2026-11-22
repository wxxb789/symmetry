# rf-w4-12a `journalStore` plan — the app package types its durable store as an interface

Slice: W4-12a (rf-w4-12-go-test-doubles, arch-decision-2 R1 / R0). Contract:
`goal-rf-w4-12-go-test-doubles.md`, section `## Conductor decision (2026-11-21, arch-decision-2 — SUPERSEDES …)`
and the two amendments after it (verification amendments, comment policy, PR #234 TODO entry).

Dispatch HEAD: `adfe7d6a` (`refactor(app): split startGoalAdmission into phases (#234)`), W4-01 merged.
R1 lane position: W4-17a → W4-17b → W4-01 → **W4-12a** → W4-12b → W4-12c → W4-22 → W4-13.

## Measured basis (dispatch HEAD `adfe7d6a`)

Method-set census (one statement, reproducible; the throwaway tool is deleted before the gates):

- `go list -export -deps -json ./internal/app` + `go/types` type-check of the package's non-test files,
  collecting every selector whose receiver type is `*state.Store`
  (environments `windows/amd64`; the linux/darwin/arm64 unions are re-run at the gate step):
  **97 methods**, listed with every call site in the evidence record.
- Field sites: `options.store` (`app.go:189`), `daemon.store` (`app.go:301`).
- Other `*state.Store` declarations in non-test app files (all retyped, see "Deliverable shape"):
  `lease.go:43` (`linuxCloneCleanupPersistMutation` func type),
  `linux_owner_recovery_linux.go:29` (its linux assignment, closure parameter),
  `linux_owner_v2_linux.go:1828` (`persistLinuxV2Mutation`), `pi_provider_bridge.go:49`,
  and 16 parameters in `linux_owner_witness_linux.go`.
- `store == nil` branches that a daemon without a Store can take: 25 sites in
  `app.go` (523, 708, 801), `cleanup.go` (545, 769), `cleanup_glue.go` (42, 59, 78, 93, 290, 396),
  `deterministic_validation.go` (90), `linux_owner_v2_linux.go` (1180, 1209, 1549),
  `native_run.go` (217, 227, 246, 428), `pi_provider_bridge.go` (50, 180), `recovery.go` (619, 635, 705),
  `recovery_glue.go` (33), `linux_owner_witness_linux.go` (1416, 2479, 2599).
- Gate greps at base: `rg -n 'store +\*state\.Store' daemon/internal/app --glob '!*_test.go'` = 21 lines
  (the broader `\*state\.Store` count is 22, which adds the unnamed parameter `lease.go:43`);
  `rg -n 'options\.store|daemon\.store' … --glob '!*_test.go'` = the call sites above plus 5 assignments
  and nil checks.
- No test injects a typed nil: `withStore(nil)`, `store: nil`, `settings.store = nil` have 0 matches
  under `daemon/internal/app`.

## Deliverable shape (public/API shape)

1. New `daemon/internal/app/journal_store.go`:
   - `type journalStoreBase interface { … }` with the 99 methods measured on every build target
     (97 production-selected + `SaveJournal` and `QueueTerminalTransition`, which only app tests
     select), signatures copied verbatim from `*state.Store`.
   - `var _ journalStore = (*state.Store)(nil)` — `*state.Store` satisfies it directly; no wrapper,
     adapter or forwarding type anywhere in production.
   - `journal_store_linux_amd64.go` (`//go:build linux && amd64`) declares
     `type journalStore interface { journalStoreBase; … }` with the 24 methods only that target
     selects (the cgroup-v2 and clone-abort family: `internal/app/linux_owner_v2_linux.go`,
     `linux_owner_recovery_linux.go` and `linux_owner_witness_linux.go` are `linux && amd64`, and
     `*state.Store` has those methods on linux only).
   - `journal_store_other_targets.go` (`//go:build !(linux && amd64)`) aliases
     `type journalStore = journalStoreBase`, so the field type is the measured set on every target
     (97+2 elsewhere, 97+2+24 on linux/amd64).
2. Field types: `options.store` and `daemon.store` become `journalStore`; every assignment stays
   unchanged, so an absent Store remains an **untyped** nil and every `store == nil` check keeps
   its meaning.
3. Same-value retype of the six other production declarations that carry `daemon.store`/`options.store`
   values (they receive the field, so they must accept the interface):
   `lease.go:43`, `linux_owner_recovery_linux.go:29`, `linux_owner_v2_linux.go:1828`,
   `pi_provider_bridge.go:49`, the 16 `linux_owner_witness_linux.go` parameters.
   Callers that pass a concrete `*state.Store` (tests, `state.New`) keep compiling unchanged.
4. New `daemon/internal/app/journal_store_test.go`:
   - `TestJournalStoreBaseMethodSetIsExact` pins the 99 base method names (reflection) against a
     literal list,
     so widening or narrowing the seam reddens; the compile-time assertion pins the signatures.
   - `TestStorelessDaemonTakesEveryStoreNilBranch` runs one probe per `store == nil` branch above
     (23 probes), each asserting that branch's outcome for a daemon built without a Store
     (`&daemon{}`), so a typed nil or a flipped guard reddens.
5. New `daemon/internal/app/journal_store_linux_test.go`: the three linux-only `daemon.store == nil`
branches (`linux_owner_v2_linux.go:1180/1209/1549`), the `persistLinuxV2Mutation` parameter guard
(`:1837`, added in the fix round) plus the witness-parameter nil checks
(`linux_owner_witness_linux.go:1416/2479/2599`), which only compile on linux, plus the
linux/amd64 member-set pin (`TestJournalStoreMethodSetIsExactOnLinuxAMD64`, 123 names).

## Contract-preserving decisions (recorded, not guessed)

### D1 — three `*state.Store` methods cannot be named from package `app` (amendment)

`ListLateGoalUsage() ([]lateGoalUsageLedger, error)`, `MarkLateGoalUsageDelivered(…) (lateGoalUsageLedger, error)`
and `RetireLateGoalUsage(…) (lateGoalUsageLedger, error)` are selected by `daemon.store` in
`goal_delivery.go:778/797/808` (`flushLateGoalUsage`), but their signatures **mention the unexported
type `state.lateGoalUsageLedger`**, which no other package may name. Go therefore cannot declare an
app-local interface method with those signatures, and `*state.Store` would not satisfy a substituted one.
No test-side or production-side trick removes that: the field type is an interface, so those three call
sites are unreachable through it.

Options weighed:

- **A (implemented)**: export the existing type as an alias in `daemon/internal/state/goal_delivery.go`
  — `type LateGoalUsageLedger = lateGoalUsageLedger` (one declaration, zero runtime, wire and persisted
  bytes unchanged; an alias is the same type). `journalStore` then covers all 97 measured methods and
  keeps one store reference.
- **B (rejected)**: keep those three calls on a second concrete `*state.Store` field or re-derive it with
  a type assertion. That leaves the late-usage writes outside the new seam, duplicates the store
  reference (repo rule: no second state authority), and either edits test fixtures that build `&daemon{}`
  directly or silently skips the flush under a future store double.
- **C (rejected)**: leave the three methods out of the interface and out of reach — impossible: the field
  type makes the call sites non-compiling.

Option A is the smallest amendment that preserves the slice's design (R0: one seam, satisfied directly by
the production type, no wrapper); B/C are recorded here and in the evidence record for the conductor.

### D2 — the interface is the measured set, tests included

The interface is derived from the non-test census. If an app test also selects a `*state.Store` method
through `daemon.store`/`options.store` that production does not call, that method is added and listed in
the evidence record ("test-consumed additions"), because "existing app tests run unchanged" outranks a
one-method-stricter seam. (Compilation of both GOOS test binaries is the oracle; the final count is
published.)

### D3 — R9 / `SOURCE_PATHS`

Nothing moves out of a `SOURCE_PATHS` file: `app.go` keeps both field declarations and only their type
spelling changes, and `journal_store.go` is new code, not moved code. The contract's condition ("if it
receives any declaration from a listed path") is therefore false and `SOURCE_PATHS` is unchanged; the R9
gate (`python3 scripts/ci/test-omo-control-native-evidence.py`) is still run green. Alternative reading
(add `journal_store.go` because it projects declarations of `state/state.go`, itself pinned): recorded,
not applied — it widens the native-evidence freeze for every future app edit, which is the conductor's
call.

### D4 — untouched machinery

The 25 mirrors (`cleanup_glue.go` and their reads) stay as they are (W4-12b deletes them).
`selectStoreWrite` is the one recorded exception: D5 rebuilds it to bind the store fallback lazily,
because a method value taken from a nil `journalStore` panics before the guard (conductor amendment
2026-11-22, whole-increment review SPEC-A04, ratified as the smallest in-scope correction); its call
sites, lock site and refusal identity are unchanged. No lint config, no `//nolint`, no production
wrapper.

### D5 — `selectStoreWrite` binds its fallback lazily (required by the typed-nil clause)

At base the 8 `selectStoreWrite(daemon, override, daemon.store.Method)` call sites built the store
method value *before* the guard; with `*state.Store` that is legal for a nil receiver, so the guard
still refused a storeless daemon. With a nil `journalStore` the same expression panics, so the guard
would have changed meaning for its 8 callers. `selectStoreWrite` now takes
`fromStore func(journalStore) Fn` and binds it inside the guard, after the same read lock and under
the same refusal predicate; the store call, its receiver, its order and the error identity are
unchanged for every non-nil path (evidence table in the record, §6).

### D6 — the test injection seam writes an untyped nil

`withStore(store *state.Store)` assigned its argument straight into the interface field, which turns
an absent store into a typed nil (`options.store != nil` would be true and `store == nil` checks
would change meaning). It now writes `nil` for a nil argument; the probe
`app.go:524 injected absent store stays untyped nil` pins it (mutant M4).

## Preserved semantics

- Same `*state.Store` method per call, same order, same arguments, same error wrapping: the interface only
  changes static types; every call site keeps its expression and its receiver.
- `storeLifetimeMu`/`storeClosing` refusal and `close()`'s ownership check
  (`options.store == nil && daemon.store != nil`) keep their exact predicates.
- Absent stores stay untyped nils: `&daemon{}` reads `store == nil`, so all 25 nil branches behave as at
  base (probes below); no test injects a typed nil, so no check changes meaning.
- Comparison semantics of `store != daemon.store` (`linux_owner_v2_linux.go:1837`) are unchanged: both
  operands hold the same `*state.Store` pointer at base and at HEAD.

## Evidence plan (all foreground; heavy suites serialized)

- `gofmt -l daemon/internal/app …`; `git diff --check`;
  `rg -n 'store +\*state\.Store' daemon/internal/app --glob '!*_test.go'` = 0 at HEAD;
  implementer grep of `journalStore` (`var _` + field declarations only).
- `go build ./...`, `go vet ./...`; `GOOS=linux|windows|darwin` builds and `GOOS=linux GOARCH=arm64`;
  `GOOS=linux GOARCH=amd64 go vet ./internal/app/` (linux-only test files compile).
- Windows `go test -count=1 ./internal/app/...`; WSL `wsl -d Ubuntu-24.04 -u accept`
  `cd <worktree>/daemon && go test -count=1 -race ./internal/app/...`
  (this slice's worktree checkout; the main checkout does not contain this slice, and there the
  scoped selection silently matches no tests).
- Red mutants recorded on the delivered bytes: the reflection list (drop one method name / add one),
  a flipped nil guard, a typed-nil store field, and (fix round) one store-term deletion per corrected
  storeless probe (evidence record §11 M2/M3 and M6–M10).
- R2: line coverage of the touched files must not drop (`go test -cover` for `internal/app` before/after).
- R9: `python3 scripts/ci/test-omo-control-native-evidence.py`.
- Census re-run at HEAD over `windows/amd64`, `linux/amd64`, `linux/arm64`, `darwin/amd64` (union), with
  the per-method call-site table and both scopes in the evidence record.

## Delivered revision

Files written: `journal_store.go`, `journal_store_linux_amd64.go`, `journal_store_other_targets.go`,
`journal_store_test.go`, `journal_store_linux_test.go` (new, app); `app.go`, `cleanup_glue.go`,
`lease.go`, `linux_owner_recovery_linux.go`, `linux_owner_v2_linux.go`,
`linux_owner_witness_linux.go`, `pi_provider_bridge.go`, `export_test.go`,
`lease_stale_persistence_test.go` (edited, app); `daemon/internal/state/goal_delivery.go` (one
alias); `docs/TODO.md` (PR #234 amendment); this plan and
`docs/evidence/rf-w4-12a-journal-store-2026-11-22.md`.

Measured sets: 97 production methods on every target, 121 on linux/amd64, +2 test-only
(`SaveJournal`, `QueueTerminalTransition`); declared `journalStore` = 99 / 123. The base-vs-HEAD
storeless-branch audit ran the probe set of that stage (30 probes, 24 cross-target pre-drop and 6
linux) in a throwaway base copy and at HEAD: both green. The delivered set is 30 probes — 23
cross-target after the apply-stage drop of the inert `app.go:189` probe, plus 7 linux after the fix
round added the `persistLinuxV2Mutation` probe (evidence record §8/§11 M6–M10); the fix round also
gave the four sibling-masked probes valid inputs. The conductor ratified D1, D5 and the three
build-split files as in-scope minimal corrections (2026-11-22, whole-increment review SPEC-A04) and
directed the STD-A01..A03 / SPEC-A01..A03 fixes carried by this revision.
Gate commands, exit codes and mutants: evidence record §10–§12.

## Out of scope (with reasons)

- W4-12b/W4-12c/W4-22 machinery (`apptest`, `FaultStore`, `Recorder`, the #185 seal): separate contracts;
  the 25 mirrors stay for W4-12b to delete.
- `daemon/internal/state/**` beyond the one-line alias of D1 (W4-21 owns that package).
- `execution/**`, `harness/**`, `platform/**`, `e2e/**`, `.golangci.yml`: untouched.
- Production clock/seam changes: none needed.
