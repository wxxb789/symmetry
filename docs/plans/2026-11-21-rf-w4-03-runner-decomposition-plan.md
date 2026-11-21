# rf-w4-03 runner decomposition plan — `Start` phases + shared close-path helpers

Contract: `goal-rf-w4-03-runner-decomposition.md` (GDD state root, 2026-11-21) with its two
conductor decisions (Wave-4 draft; W4 review reconcile) and the program-wide rules (lesson 32
control-flow preservation, host-path hygiene, and the DLP escape-sequence rule).

Baseline for every coordinate: `daemon/internal/execution/runner.go` at contract re-derivation
`origin/main 0fa9b705`, unchanged at this worktree's HEAD `c9ed23b4`
(`git log 0fa9b705..HEAD -- daemon/internal/execution/` is empty, so every line number below is
valid at both revisions): **2,738 L**; `Start` `:547-944` (**398 L**);
`closeContainmentAttempt` `:2092-2339` (**248 L**); `closeContainmentOwnerLocked` `:2353-2376`;
`cleanupFailedStartStarted` `:1335`; `closeFiles` `:2732`.

Measured site inventory at that revision (exact, re-measured here — matches the contract):

| site family | count | lines |
| --- | ---: | --- |
| `cleanupFailedStartStarted(` calls inside `Start` | 21 | `:627, 654, 682, 702, 711, 732, 749, 762, 781, 794, 808, 827, 837, 848, 857, 866, 876, 885, 894, 906, 915` |
| `closeFiles(` calls inside `Start` | 10 | `:583, :588, :593, :605, :614, :624, :636, :642, :646` (bootstrap) + `:927` (child-end rule) |
| `closeFiles(` calls outside `Start` | 2 | `:1344` (inside `cleanupFailedStartStarted`), `:2732` (definition) |
| `setContainmentError(errors.Join(` inside `closeContainmentAttempt` | 16 | `:2154, 2195, 2200, 2237, 2253, 2265, 2274, 2279, 2285, 2291, 2296, 2302, 2311, 2322, 2327, 2332` |
| `errors.New("containment authority persistence is uncertain")` | 3 | `:2199, :2273, :2310` |

`cleanupFailedStartStarted`'s signature is **frozen**: `runner_output_truncation_test.go:206`
calls it directly with `(started, stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead,
stderrWrite, startedAt, startErr, nil)`. Test files are oracles; the helper keeps its exact
parameter list, and the 21 call sites reach it through one per-run adapter instead.

## Public / API shape

Nothing exported changes: no exported identifier, no wire/persisted/receipt/journal byte, no log
line, no wrapper text, no joined-error order, no test name. The slice adds only unexported
surface:

| addition | kind | home |
| --- | --- | --- |
| `startPipes` + `createStartPipes` + `Close` + `closeChildEnds` | type + 3 funcs | new sibling `runner_pipes.go` (the contract's single allowed new file) |
| `startRun` + `launchStarted`/`persist`/`commitHandoff`/`persistAtomicAuthority`/`persistLegacyIdentity`/`bindStopReceiptPersistence`/`armInitialLease`/`resumeTarget`/`deliverInitialInput`/`fail` | unexported struct + phase methods | `runner.go`, immediately after `Start` |
| `errContainmentAuthorityUncertain` | package var (one construction replaces three) | `runner.go`, immediately above its users |
| `failContainment`, `failContainmentWith`, `persistReceiptOrAbort`, `persistUncertainAuthority`, `closeOwnerOnce` | unexported `*Process` helpers | `runner.go`, next to `closeContainmentAttempt` |
| `closeContainmentAttempt` (body), `closeContainmentOwnerLocked` (body) | rewritten in terms of the helpers | `runner.go` |

Frozen byte-identical (contract D1): the ack-stage constant table `:216-225`, the callback struct
`:162-213`, `ErrLinuxV2MutationAckLost` `:47-58`, the handoff twins
`retrySupervisorHandoffReceipt`/`retrySupervisorHandoffClear` `:2484-2504`,
`publishResumeBarrierMarker` `:1024-1053`, `finalizeSupervisorHandoffLocked`
`:2378-2466`/`recordSupervisorHandoffReceiptLocked` `:2468-2483` (outside the owned region, see
"Out of scope"), and every string literal of the owned regions.

## Phase map (`Start`)

`Start` becomes a validation prologue (unchanged text), pipe bootstrap, then one ordered
composition:

| # | phase function | base region | cleanup sites | closeFiles sites |
| --- | --- | --- | ---: | ---: |
| — | pipe bootstrap (`createStartPipes` + `pipes.Close()`) | `:547-607` | — | 3 (`:583, :588` in the constructor, `:593` in `Start`) |
| 0 | `launchStarted` | `:609-658` | 2 (`:627, :654`) | 5 (`:614, :624, :636, :642, :646`) |
| 1 | `persist` (delegates to `commitHandoff` on the handoff path) | `:660-830` | 3 (`:682, :808, :827`) | — |
| 2 | `commitHandoff` (called by `persist`) | `:691-720` | 2 (`:702, :711`) | — |
| 3 | `persistAtomicAuthority` / `persistLegacyIdentity` (called by `persist`) | `:721-801` | 5 (`:732, :749, :762, :781, :794`) | — |
| 4 | `armInitialLease` | `:832-898` | 7 (`:837, :848, :857, :866, :876, :885, :894`) | — |
| 5 | `resumeTarget` | `:900-918` | 2 (`:906, :915`) | — |
| 6 | `deliverInitialInput` | `:920-943` | — (uses `cleanupAfterInputSetupFailure` twice, unchanged) | 1 (`:927`) |

21 cleanup + 10 closeFiles accounted for: 21 = 2 + 3 + 2 + 5 + 7 + 2; 10 = 3 + 5 + 1 + 1.

Every phase exit calls `run.fail(startErr, containmentErr)`, which forwards `run.started`,
`run.pipes` (all six ends in the base order), `run.startedAt`, and both errors to
`cleanupFailedStartStarted` — the exact tuple each of the 21 inline sites passed. The two wrapper
shapes stay distinguishable: `fail(err, nil)` for the 16 single-error sites, and a non-nil third
argument for the 5 sites that also recorded a containment error — `:627` (raw launcher `err`),
`:654`/`:827` (`wrappedErr`/`attachFailureErr`), and `:732`/`:781` (`errors.New(message)` twice)
(`grep 'return run.fail(' | grep -v ', nil)$'` = 5 at head).

`startRun` is the explicit input/output carrier: immutable inputs (`runner`, `ctx`, `invocation`,
`program`, `startedAt`, `pipes`), the owned process (`started`) and the two cross-phase flags
(`attachFailure`, `markerPersisted`). Phase outputs are `(*Process, error)`: `(nil, nil)` means
"continue", any other pair is `Start`'s terminal result. No phase reads another phase's local
state.

## Ambiguities and the decisions taken (contract asks for options + recommendation)

### A. `persist → commit` ordering: is `commit` a `Start`-level call or a `persist` sub-phase?

The base code serialises durability as `if containmentHandoff != nil { commit } else if … { atomic
persist } else { legacy persist }`, and the pre-branch setup
(`persistContainmentUnproven` binding, the first `installContainmentUnprovenCallback`,
`authorityCapable`) plus the post-branch tail (marker reinstall, capability reset,
`attachFailureErr` check) run around that branch.

- Option 1 (chosen): `Start` calls `persist`; `persist` calls `commitHandoff` on the handoff
  branch. The five named durability/lifecycle phases remain `persist → commit → lease-arm →
  resume → input` in execution order; the commit is the durable write *inside* the persistence
  stage, which is what the base code does.
- Option 2 (rejected): `Start` calls `commit` then `persist`. This moves the commit ahead of the
  pre-branch setup (observer install, stop-receipt provider wiring) that the base code runs
  **before** `commitStartedHandoff` — a real control-flow/ordering change, forbidden by contract
  clause 3.

### B. `startPipes`: one `Close()` for ten sites, but the child-end site closes three

- Option 1 (chosen): `Close()` closes all six ends it owns and is nil-tolerant, so the nine
  bootstrap sites (`:583` two ends, `:588` four ends, `:593/:605/:614/:624/:636/:642/:646` six
  ends) all read `pipes.Close()` with identical effect; the child-end site keeps its own named
  `closeChildEnds()` (stdinRead, stdoutWrite, stderrWrite) with the base comment unchanged.
- Option 2 (rejected): make the child-end site `pipes.Close()` too — it would close
  `stdinWrite`, `stdoutRead`, `stderrRead` in the parent while the parent readers still need them
  **and** before the child's stdio handoff is complete: a functional change to the child-end rule
  the contract explicitly freezes.

`createStartPipes` assigns into the value before each probe, so a failed `os.Pipe` leaves the
uncreated ends nil and `pipes.Close()` closes exactly the prefix the base `closeFiles` calls
closed.

### C. `closeOwnerOnce` and the divergent failure tails

`closeContainmentOwnerLocked` and the non-handoff path of `closeContainmentAttempt` share the
retryer probe, the one-shot `containmentCloseAttempted` guard and the close call; they differ in
that the plain path first persists the durable `ContainmentUnproven` marker (which can extend the
close error) before the stable-error/containment-error bookkeeping.

- Option 1 (chosen): `closeOwnerOnce(persistUnprovenOnFailure bool)`. The flag is an explicit
  input; `false` is the handoff path's exact base behaviour, `true` the plain path's.
- Option 2 (rejected): a callback hook for the marker step — hidden control flow for a
  two-valued input, and it would hide the ordering (marker → retryable probe → stable error) that
  the audit table pins.

The bookkeeping stays base-exact: `errors.Join(process.containmentInitialError, err)` (not the
finalization base error), the `containmentStableError` join, the retryable re-probe, and the
returned error.

### D. The two `(wrapped record, raw return)` join sites

Four of the sixteen join sites record a wrapped text but `return err` raw
(`retry process containment authority persistence`, `persist containment stop receipt`, `release
containment authority`; the close path's own site lives inside `closeOwnerOnce`). `failContainment(err)`
records `join(base, err)` and returns `err`. The wrapped-record sites use
`failContainmentWith(recorded, returned)`, which records exactly `join(base, recorded)` and returns
`returned` — so both the `ContainmentError()` text and the propagated error identity are
byte-identical to base. Option rejected: returning the wrapped error (changes the error a caller
of `closeContainment`/`ContainmentError` observes).

## Edge-case semantics the contract demands

- **Which cleanup runs on which exit**: each base site's `(startErr, containmentErr)` tuple is
  preserved one-for-one through `fail`; the audit table below records every site.
- **Exact error text and join order**: 21 wrapper texts, 16 join sites, 3 sentinel texts and the
  child-end comment survive verbatim; asserted mechanically by the ordered string-literal/call
  comparison in the evidence record, plus the oracle suites that pin many of the texts.
- **Child ends**: closed exactly once, at the base point, after the resume phase and before
  `newProcessFromStarted`/`process.start`.
- **`markerPersisted`**: one shared `*atomic.Bool` per run, installed before the first
  observer, stored by the same three branches, re-read at the same point.
- **`cleanupFailedStartStarted`**: parameter list frozen; `:1344`'s child-end close untouched.
- **Ack-stage table, callback struct, `ErrLinuxV2MutationAckLost`, handoff twins, barrier
  publisher**: untouched (byte-diff asserted in the evidence record).

## Control-flow audit plan (lesson 32)

The evidence record carries one row per base decision point of the two decomposed functions:
base line → base construct (early return / fall-through / error short-circuit / default) → HEAD
equivalent (phase + line) → preserved yes/no → how the phase returns there. Rows are produced from
the base file and the delivered file by the same extraction command
(`awk` over the two revisions for `return`/`if`/`else` structure plus the call sequence), and the
reviewer re-runs the comparison independently.

## Evidence plan (all foreground; the gate list is the contract's, plus two additions)

1. `gofmt -l`, `go vet ./...`, `GOOS=windows|linux`, `GOOS=linux GOARCH=arm64 go build ./...`.
2. `go test -count=1 ./internal/execution/` (Windows subset) **and** the full set via
   `wsl -d Ubuntu-24.04 -u accept` at this worktree's path
   (`/mnt/q/repos/wt-w4-03-runner-decomposition/symmetry/daemon`); `go test -list '^Test'` counts
   recorded beside both.
3. Callers per lesson 32: `go test -count=1 ./internal/harness/...`, `go test -count=1
   ./internal/app/...` (Windows-local). Sequential, never concurrent with a second suite (host
   memory rule).
4. `python scripts/ci/check-citation-existence.py --base <contract base> --head HEAD`;
   `git diff --check`; host-path hygiene `git diff origin/main...HEAD | grep -i "users/"`.
5. Added checks (not tests, no new test names): overlay mutation runs that prove the oracles
   actually pin the two new seams — drop one end from `startPipes.closeChildEnds` and from
   `Close` (`-overlay` copy of `runner_pipes.go`), and drop one join wrapper from
   `failContainmentWith`; each must turn the package red, reverted by discarding the overlay.
6. LOC/function-length table reported, never gated (D2).

## Out of scope (with reasons)

- `finalizeSupervisorHandoffLocked` `:2378-2466` and `recordSupervisorHandoffReceiptLocked`
  `:2468-2483` repeat the same 19 `errors.Join(base, …)` shape, but they sit outside the
  contract's owned regions (`closeContainmentAttempt` `:2092-2339`, `closeContainmentOwnerLocked`
  `:2353-2376`); leaving them alone keeps the byte-identical surface D1 requires and keeps the
  `runner.go` ack-table/retry-family free for W4-05. Recorded as a W4-05-adjacent candidate, not
  touched here.
- `linux_owner_v2_linux.go`, `process_launcher_*.go`, `probe.go`, the authority package, harness
  adapters, app, and all test files: contract out-of-scope list.
- Any behaviour fix inside the moved code (including the double `Clone` in
  `persistUncertainAuthority`, preserved verbatim): this is a decomposition slice.

## Measured results (filled at delivery)

`runner.go` 2738 → 2758 lines plus the 63-line `runner_pipes.go`; `Start` 398 → 81 lines
(decision points 85 → 27); `closeContainmentAttempt` 248 → 143 lines (64 → 41); 21 cleanup sites
→ 21 `run.fail` sites, 10 `closeFiles` sites → 9 `Close()` + 1 `closeChildEnds()`, 16 join sites
+ 3 sentinel constructions → named helpers + one value. Both targets met (81 ≤ 180, 143 ≤ 150);
the row's −200…−300 dLOC estimate is not met (+20 net, report-only per contract D2).

Full evidence — function-length table, the cleanup/closeFiles/join bootstrap tables, the 55-row
base-vs-HEAD control-flow audit, the ordered-text comparison, every gate command with its exit
code and counts, the overlay-mutation battery, the WSL/harness/app suite results, and the
host-load flake attribution — is in `docs/evidence/rf-w4-03-runner-decomposition-2026-11-21.md`
(with the audit section 11 recording two independent read-only audits: no behavioural
divergence, all preservation claims confirmed).
