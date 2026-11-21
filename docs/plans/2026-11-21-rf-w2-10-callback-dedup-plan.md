# callback factory dedup plan — one store-callback selector, ten factories, two measured clone pairs

Contract: `goal-rf-w2-10-callback-dedup.md` (GDD, 2026-11-21), branch
`refactor/rf-w2-10-callback-dedup`, baseline `origin/main` = this worktree's HEAD =
`6117e1983860c8fe1862747a6513bdf33be126a6` (`6117e198`, the merge of #144
`rf-w1-05-app-go-split`) — the contract's dispatch gate, so the pre-split `app.go` region it names
now lives in `daemon/internal/app/cleanup_glue.go`.
Source item: program plan §5 W2-10; `findings/daemon-app.md` §4 S3; contract review rounds 1–2
(`refactor/w2-contract-review-opus-2.md`, `refactor/w2-contract-review-ds-2.md`).

## Binding amendments applied

1. **C1 — #144 merged, callbacks re-verified by name.** At `6117e198` the ten factories and their
   `cleanup_glue.go` heads are `persistProcessAuthorityCallback` `:25`, `markContainmentUnprovenCallback`
   `:82`, `clearContainmentUnprovenCallback` `:100`, `persistContainmentStopReceiptCallback` `:118`,
   `prepareSupervisorHandoffCallback` `:321`, `bindSupervisorHandoffCallback` `:339`,
   `commitSupervisorHandoffCallback` `:357`, `recordSupervisorHandoffStopReceiptCallback` `:381`,
   `clearSupervisorHandoffCallback` `:387`, `clearSupervisorHandoffAfterAbortCallback` `:405` — the
   contract's nine-value list plus `:321`.
2. **C2 — owned set is exactly two files**: `daemon/internal/app/cleanup_glue.go` and
   `daemon/internal/app/process_persistence_test.go`, plus this plan. No caller file
   (`app.go`, `goal_admission.go`, `recovery.go`, `recovery_glue.go`, `linux_owner_witness_linux.go`)
   is edited, so the ten factory names and signatures stay byte-identical.
3. **C3 — pins first, green on base.** The failure-control cases (a)–(e) land in
   `process_persistence_test.go` *before* the production edit and are run against the unmodified
   `cleanup_glue.go`; one closing check is then flipped in a scratch copy to show they are
   non-vacuous.
4. **C4 — dupl acceptance is the threshold-100 measurement.** Measured at `6117e198` (golangci-lint
   `2.15.0`, `--enable-only=dupl`, `dupl.threshold: 100`, exact commands below): GOOS=windows
   **11 issues / 7 distinct declaration pairs**, GOOS=linux **18 issues / 8**. The owned region
   contributes exactly two pairs, in both builds:
   `markContainmentUnprovenCallback` `:82-98` ↔ `clearContainmentUnprovenCallback` `:100-116` and
   `clearSupervisorHandoffCallback` `:387-403` ↔ `clearSupervisorHandoffAfterAbortCallback`
   `:405-421`. Target: both gone, no new pair anywhere in `./internal/app/...`.

## Deliverable shape

1. `daemon/internal/app/cleanup_glue.go`: one generic selector (`selectStoreWrite`) replaces the
   per-factory option-or-Store selection — and the lock/refusal that came with it — in eight
   factories. `persistProcessAuthorityCallback` keeps its own refusal with the identical predicate,
   because its write path and indeterminate-write retry live in `persistProcessAuthority`, an
   unowned file shared with recovery; the selector is not split into a second helper for its one
   caller. `recordSupervisorHandoffStopReceiptCallback` is unchanged: it holds no store CAS of its
   own.
2. `daemon/internal/app/process_persistence_test.go`: the C3 pins — a table over the nine
   store-touching factories (closing refusal with an override, refusal with no Store and no
   override, read lock held across the override), a clone pin for the authority/handoff arguments,
   and an observer-order pin for `persistProcessAuthorityCallback` and
   `commitSupervisorHandoffCallback`.
3. This plan. Gate and mutant outputs go into the stage's `answer.md` (outside the repository).

## Public/API shape and exact semantics preserved

The ten factories keep their names, signatures, and error identity. What changes is where the
selection is written down, not what it decides. The three guard spellings present at base are the
same predicate, which is the fact the new selector relies on:

| base spelling | where | guard |
| --- | --- | --- |
| pattern A | `mark`, `clear`, `prepare`, `bind`, `commit`, `clear`, `clearAfterAbort` | `if override == nil { if storeClosing \|\| store == nil { refuse }; fn = store.<M> } else if storeClosing { refuse }` |
| pattern B | `persistProcessAuthorityCallback`, `persistContainmentStopReceiptCallback` | `if storeClosing \|\| (store == nil && override == nil) { refuse }` |
| pattern C | `persistSupervisorHandoffStopReceipt` (`recovery_glue.go`, unowned) | `if storeClosing { refuse }; if override == nil { if store == nil { refuse }; fn = store.<M> }` |

A and B are the same predicate: `storeClosing || (override == nil && store == nil)`. C differs only
in that its first read of `storeClosing` precedes its read of the option, both under the same read
lock, with the option written only before the daemon starts. The selector implements exactly the A/B
predicate, under the same lock mode, and returns the lock release to the caller:

```go
func selectStoreWrite[Fn storeCallbackWrite](daemon *daemon, override, fromStore Fn) (Fn, func(), error) {
	daemon.storeLifetimeMu.RLock()
	release := daemon.storeLifetimeMu.RUnlock
	if !daemon.storeClosing && (override != nil || daemon.store != nil) {
		if override == nil {
			override = fromStore
		}
		return override, release, nil
	}
	release()
	var refused Fn
	return refused, nil, errDaemonStoreClosing
}
```

Callers `defer release()` immediately, so the store lifetime read lock still spans the complete CAS —
the `close()` write lock still cannot close the Store while a runner decides whether a write outcome
is known. Refusal is returned as `errDaemonStoreClosing` itself, never wrapped, so `errors.Is`
identity is unchanged, and it is returned with a nil write path. Arguments keep their exact
treatment: `.Clone()` stays on the authority/handoff arguments inside the factory body, the observer
still fires only after a successful write, `persistContainmentStopReceiptCallback` still verifies
durability and observes the external stop in the same order, and `persistProcessAuthorityCallback`
still runs its two-attempt CAS through `persistProcessAuthority`.

## Pin design (C3)

One table drives the nine store-touching factories, each entry installing its own override and
invoking its own factory, so the shared predicate is tested through nine independent call sites:

- **(a)** `storeClosing = true` with the override set → `errors.Is(err, errDaemonStoreClosing)` and
  the override ran `0` times;
- **(b)** `storeClosing = false`, `store == nil`, override nil → `errors.Is(err, errDaemonStoreClosing)`;
  this is also the case that would panic if a selector formed a Store method value on a nil Store and
  the refusal path failed to run first;
- **(c)** inside the override, `daemon.storeLifetimeMu.TryLock()` returns false (the read lock spans
  the whole CAS) and the override's own error is the one that surfaces;
- **(d)** the caller's handoff/authority is mutated through its pointer field (`CreatorSessionID`)
  after the call; the value the override received still carries the original session id;
- **(e)** for `persistProcessAuthorityCallback` and `commitSupervisorHandoffCallback`, a shared event
  log shows `write` before `observe`, and no `observe` at all when the override fails.

Non-vacuity: flipping one closing check in a scratch copy of `cleanup_glue.go` must turn the pins red.
The flip is done twice — once on the base text (the pins are written first), once on the refactored
selector (the pins must still exercise the new code).

## Evidence plan (exact commands, all foreground; never a measurement beside the full suite)

Base measurement, already taken at `6117e198` (from `daemon/`):

```
GOOS=windows golangci-lint run -c <STATE>/tmp/rf-w2-10-dupl100.yml --enable-only=dupl ./internal/app/...
GOOS=linux GOARCH=amd64 golangci-lint run -c <STATE>/tmp/rf-w2-10-dupl100.yml --enable-only=dupl ./internal/app/...
```

`<STATE>` is the GDD state root; `rf-w2-10-dupl100.yml` holds only
`version: "2"` + `linters.settings.dupl.threshold: 100` (no repository config exists yet — the CI
quality plane is W1-01's file set, so no `.golangci.yml` is added here). A helper script rewrites
each finding into a declaration-level pair, because raw line ranges move between revisions.

Sequence:

1. RED-first pins: `go test -count=1 -run 'TestStoreCallback' ./internal/app/` on the base tree with
   the pins added → green (the pins describe base behaviour); then the scratch flip → red, reverted.
2. Production edit, then: the same focused run, then the contract's own suite
   `go test -count=1 ./internal/app/...` (≈150 s).
3. dupl after, same two commands, same helper; the after pair set must equal the base set minus the
   two owned pairs.
4. `gofmt -l <changed files>`, `go vet ./...`, `GOOS=linux go build ./...`, `GOOS=windows go build ./...`,
   `git diff --check`, `git diff origin/main...HEAD | grep -i "users/"` (expect no match).
5. CI-only oracle (conductor): the daemon `-race` jobs. Optional local substitute, only if WSL with a
   Linux Go toolchain is available: the focused pins under `-race`; reported either way.

### Measured outcome (delivered revision; full logs in the run's `answer.md`)

- dupl @100 over `./internal/app/...`: GOOS=windows 11 → **7** issues (7 → **5** declaration pairs),
  GOOS=linux GOARCH=amd64 18 → **14** issues (8 → **6** pairs). In both builds the pair-set
  difference is exactly the two owned pairs removed and nothing else.
- Pins `go test -count=1 -run TestStoreCallback ./internal/app/` green on the base tree before the
  production edit and green after it; the scratch flips (one base closing check, then the selector's
  own closing test) turn them red — 1 factory and 8 factories respectively.
- `cleanup_glue.go` 463 → 458 lines: 85 lines of per-factory lock/guard/invoke text removed, 80 added
  (type set 15, selector 21, two blank separator lines 2, retained-call-site comment 3, call-site
  plumbing 39). The plan row's −60 dLOC projected from the "4 dupl blocks" reading that contract C4
  corrects to two measured pairs; the measured pair removal, not the line count, is the acceptance.
- `go test -count=1 ./internal/app/...` on an idle host: exit 0, `ok … 458.523s`. An earlier run on a
  loaded host failed the 2 s-deadline test `TestOmOAttachAckRegressionCancellationAndDeadlineDoNotRenewBudget`
  and then hit Go's 10-minute default timeout on a Store file-open stall; the same focused test passes
  on both the base worktree (23.289 s) and the delivered tree (22.421 s) idle, so both symptoms are
  host-load artifacts, not this slice's regression (full record in the run's `answer.md`).

## Design trade-offs

1. **Free generic function, not a method.** Go forbids type parameters on methods, so the selector is
   a free function taking `daemon *daemon`; it is the package's first generic declaration and carries
   a comment saying why. The constraint is a union of the seven write-path signatures the file serves
   — the smallest constraint in which `override != nil` is legal (`comparable` rejects func types and
   `nil`, measured) and the only one that keeps the check compile-time and allocation-free.
2. **Selector returns the write path; the caller invokes it.** Considered and rejected: passing an
   `invoke func(Fn) error` closure (hides the CAS inside the helper but makes every call site spell
   the wide function type), and a `call func() Fn` store accessor (same spelling cost). Returning the
   resolved function keeps `defer release()` visible at the call site, so the "read lock spans the
   CAS" invariant is auditable per factory.
3. **Eager method values are safe and documented.** `daemon.store.MarkContainmentUnproven` is formed
   before the lock is taken; forming a method value on a nil pointer receiver does not panic
   (measured on go1.27.4), and the refusal path returns before any invocation. `daemon.store` is
   written only during construction (`app.go:549`/`:555`) and by quiescent tests that assign it
   between phases (`harness_test.go:2806`, `linux_owner_witness_linux_test.go:139`) — the base code's
   read lock does not synchronize those writes either, so nothing new is unsynchronized.
4. **`persistProcessAuthorityCallback` keeps its own invocation.** Its write path and its
   single retry of a failed write are `persistProcessAuthority` (`recovery.go`, unowned); only its
   guard moves to `selectStoreWrite`. Rewriting that delegate here would either duplicate the retry
   protocol or change it — both are store-callback semantics changes.
5. **No new suppression.** No `//nolint`, no repository lint config, no second convention for
   selecting a write path.

## Out of scope (with reasons)

- `persistSupervisorHandoffStopReceipt` (`recovery_glue.go`) and `persistProcessAuthority`
  (`recovery.go`): outside the owned two-file set; their internal selection stays as it is.
- `persistProcessMarker` and `clearPersistedProcessDetails` (`cleanup_glue.go`, not among the ten):
  `persistProcessMarker` holds the same lifetime lock and closing guard but re-selects its write path
  inside its replay closure and reads the journal back between attempts, so the select-once selector
  shape does not apply and its guard and lock stay as they are; `clearPersistedProcessDetails` is the
  one with no lock and no closing guard of its own (it delegates to
  `clearContainmentUnprovenCallback`) — changing either is a semantics change outside the contract's
  nine factories.
- The `linux_owner_v2_linux.go` journal-closure pair: outside the owned set (contract C4).
- Test-split work (W3-15), production errcheck triage (W3-09), `startGoalAdmission` split (W4-01),
  the state package, `harness_test.go` (reserved for the W2-12 follow-up).
- `docs/TODO.md`: no entry names this slice or these factories; the one occurrence of
  `cleanup_glue.go` there (`:630`) is a witness-flake diagnosis note, not a tracked item, so nothing
  is closed or moved. No design/runbook document describes the selection or its lock (checked by
  `grep -rn "storeLifetimeMu\|errDaemonStoreClosing\|store callback" docs/`), so no document becomes
  stale.

## Residual risks and limits

- The two removed pairs are gone only if the surviving call sites stay under the 100-token clone
  floor; the measurement, not the argument, decides. A new pair appearing anywhere in the package is
  a failure of this increment even if the two owned pairs vanish.
- `-race` is not available on this Windows host; the selector adds no new lock, but the CI daemon
  race job remains the only native oracle for that property.
- LOC reduction is a consequence, not the acceptance: the contract's plan row projected −60 dLOC from
  a "4 dupl blocks" reading that contract C4 corrects to two measured pairs.
