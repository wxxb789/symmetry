# rf-w3-06-witness-writer-errors plan — propagate pre-authority witness report write failures

Contract: `goal-rf-w3-06-witness-writer-errors.md` (GDD, 2026-11-21), program-plan §6 W3-06 (risk
low-med, deps none) from finding F-22 (`<gdd-state>/refactor/findings/daemon-protocol-rest.md:538-545`).
Baseline: this worktree's HEAD `36f9a887635ec89d49cce2cd13b3915a8a875a83` — every coordinate below was
read at that revision. Owned set: `daemon/cmd/symmetry-daemon/preauthority_witness_command_windows.go`,
new `daemon/cmd/symmetry-daemon/preauthority_witness_report_windows_test.go`, this plan. The e2e matrix
(`daemon/e2e/production_pre_authority_crash_witness_test.go`) and the `ci.yml` `daemon-windows` step are
oracles, not owned, and are not touched.

## Baseline defect (re-verified at HEAD)

`writePreAuthorityWitnessReport` (`:479-504`) is `void` and swallows **every** failure:

| step | line | baseline behaviour |
| --- | --- | --- |
| `os.MkdirAll(filepath.Dir(resultFile), 0o700)` | `:483` | bare `return` — no report, no error |
| `json.Marshal(report)` | `:486` | bare `return` (unreachable for this struct, kept for shape) |
| `os.CreateTemp(dir, ".preauthority-report-")` | `:490` | bare `return` |
| `temporary.Write(contents)` | `:496` | `_ = temporary.Close()` + bare `return` |
| `temporary.Close()` | `:500` | bare `return` |
| `os.Rename(temporaryPath, resultFile)` | `:503` | `_ = os.Rename(...)` — fully ignored |
| deferred `os.Remove(temporaryPath)` | `:495` | best-effort by design (temp file, not the report) — stays ignored |
| deferred `store.Close()` (`runPreAuthorityWitness` `:79`) | — | ignored error |

Consequence: a reached stage whose report could not be written still exits `0`; the witness matrix's
only durable artifact is then missing while the invocation looks healthy. 16 call sites
(`:183, :195, :229, :247, :260, :278, :288, :302, :311, :319, :339, :355, :377, :403, :406, :518`):
15 in `runPreAuthorityWitnessStart` (`:119`) / `runPreAuthorityWitnessRecovery` (`:323`), both already
returning `error`, and 1 in `crashPreAuthorityWitnessWithEvidence` (`:510-520`).

## Deliverable shape

1. **Writer signature** — `func writePreAuthorityWitnessReport(options preAuthorityWitnessOptions, report preAuthorityWitnessReport) error`.
   The first failing step returns a wrapped error naming that step; later steps are not attempted.
   Atomic publication is unchanged: `MkdirAll` → `Marshal` → `CreateTemp` in the result directory →
   `Write` → `Close` → `Rename`; the temp path is still removed best-effort by `defer os.Remove(...)`
   and `_ = temporary.Close()` on the write-failure path stays (a failed write must not leak the handle
   before the deferred remove).
   Error texts (step token kept distinct so a partially reverted writer reddens by text, per the
   conductor's P1-1 note): `prepare pre-authority witness result directory`, `encode pre-authority
   witness report`, `create pre-authority witness report temporary file`, `write pre-authority witness
   report`, `close pre-authority witness report`, `publish pre-authority witness report`.
2. **`options.resultFile == ""` stays an early no-op** returning `nil` before any filesystem work
   (conductor P3).
3. **Every non-crash call site checks and returns the error** — `if err := writePreAuthorityWitnessReport(...); err != nil { return err }`
   — so the error reaches `runPreAuthorityWitness`, whose `init()` (`:26-36`, unchanged) prints and
   exits `1`. The 15 start/recovery sites therefore can no longer exit `0` with a missing report; the
   closure sites (`:183`, `:195`) return through `AttachSuspendedProcessWithHandoff`. No site keeps a
   `return nil` after a failed report.
4. **Crash path keeps its contract** — `crashPreAuthorityWitnessWithEvidence` still writes the report
   for the crashed stage and then `os.Exit(137)`; the write failure is now printed to stderr
   (`write pre-authority witness crash report: %v`) before the same exit. Fatal-vs-best-effort is that
   caller's decision; it is already crashing. Stage names, statuses, report fields and exit codes are
   unchanged (`hard-crash`, `137`, timing order write-then-exit).
5. **`store.Close()` join** — `runPreAuthorityWitness` gets a named `err` result; the deferred close
   does `if closeErr := store.Close(); closeErr != nil { err = errors.Join(err, closeErr) }`. Joining
   only on a non-nil close error keeps the returned error's identity (and therefore its text) when the
   close is clean, which is every previously observed case.
6. **No report content change** — `preAuthorityWitnessReport`, `witnessReportFromJournal`, all stage
   and status literals, and the JSON field order/`omitempty` set are untouched. The only new output
   anywhere is the crash-path stderr note.

## Exact edge-case semantics

- **First failure wins**: each failing step returns immediately; the reported error names that step.
- **Temp-file cleanup on every failure path** stays best-effort and non-fatal: `defer os.Remove` on the
  created temp file; `os.CreateTemp` failure leaves nothing behind; `MkdirAll` failure touches nothing
  (`MkdirAll` on an existing file returns `ENOTDIR` without creating anything).
- **Failure is not retried** and the report is never written twice by the change: the crash-at-stage
  double write (a `reached` report from the call site, immediately overwritten by the `hard-crash`
  report from the crash hook) predates this slice and is preserved.
- **Non-crash failure aborts the invocation before the crash point** (exit `1` from `init`), intended
  by the conductor's P3: a missing report must never exit `0`.
- **Atomicity**: publication is still temp-file + rename inside the result directory; a reader never
  observes a partial report, and the result directory never keeps a `.preauthority-report-*` file after
  a failure.

## Evidence plan (all foreground; commands run from `daemon/`)

- Gates as the contract states them: `gofmt -l` on the changed files, `go vet ./...`,
  `go build ./cmd/...`, `GOOS=windows go vet ./cmd/symmetry-daemon/ ./e2e/`,
  `go test -count=1 ./cmd/symmetry-daemon/...`, `git diff --check`.
- Local e2e attempt (allowed, not the gate):
  `SYMMETRY_PRODUCTION_PREAUTHORITY_WITNESS=1 go test -count=1 -run '^TestProductionPreAuthority' ./e2e/`
  — the CI `daemon-windows` matrix is the gate of record.
- **Unit test** (`preauthority_witness_report_windows_test.go`): (a) publish success — one file in the
  directory, decodes to the exact report fields, no `.preauthority-report-*` leftover; (b) result path
  blocked by an existing file (`MkdirAll` `ENOTDIR`) — non-nil error whose text contains
  `result directory`, no report, no temp leftover; (c) result path is an existing directory — the
  reachable `Rename` failure, error text contains `publish`, the directory survives, no temp leftover;
  (d) `resultFile == ""` — `nil`; (e) recovery reaches the writer through `runPreAuthorityWitness`
  itself: seeded journal via `ensurePreAuthorityWitnessJournal` + blocked result path → non-nil error
  through the entry function; the same invocation with a writable path → `nil` and a decodable
  `recover`/`already-clear` report.
- **Mutation checks** (in place, restored by reverse patch with a `sha256` identity check):
  M1 all-swallowing writer restored (every failure path back to `return nil`) → (b) and (e) red;
  M2 only `MkdirAll` swallowed → red by text (`create pre-authority witness report temporary file` is
  reported instead of `result directory`); M3 the recovery call site `:406` discards the error
  (`_ = writePreAuthorityWitnessReport(...)`) → (e) red. M3 is the call-site discrimination the unit
  test can reach; the other 14 sites are proven by the diff and the call-site table in `answer.md`.
- **Call-site table**: `grep -n "writePreAuthorityWitnessReport"` on the final tree, each of the 16
  lines showing its check (15 `if err := …; err != nil` + the crash-path check).

## Design trade-offs

1. **Error text carries the failed step.** Required by the mutation design: a writer that swallows only
   `MkdirAll` still returns a non-nil error from `CreateTemp` in the same broken directory, so an
   assertion limited to "non-nil" would stay green; the text assertion is what discriminates. The texts
   are new strings on a failure path only — no persisted, wire, or success-path output changes.
2. **Join only a non-nil close error** instead of `errors.Join(err, store.Close())` unconditionally:
   unconditional `Join` wraps a sole error in `*errors.joinError`, changing the dynamic type of every
   returned error for no benefit. Behaviour on a real close failure is identical (message and
   `errors.Is/As` chaining preserved; `Store.Close` returns a bare `errors.New("release state
   directory lock")`, so there is no cause to unwrap).
3. **No new fault-injection seam.** The contract (conductor P2-1) accepts diff + call-site table for
   `Write`/`Close`; `Rename` needs no seam at all because "target path is an existing directory" is a
   reachable, deterministic failure that the unit test drives for real.
4. **No helper abstraction** (e.g. a `reportReached` wrapper): the call-site table is itself the
   judging artifact, and a helper would hide the per-site check the clause demands.
5. **`grep`-visible single-line checks** rather than a local `report :=` variable per site: the check
   stays on the `writePreAuthorityWitnessReport` line, which is what the artifact greps.

## Out of scope (with reason)

- `-linux-v2-native-owner-witness` CLI (`main.go`, `main_test.go`) — different command, no writer.
- The e2e sentinel, the crash-witness scenarios and their assertions — oracles; no test change is
  needed because no scenario drives a failing report path.
- Other witness command files — none exist.
- Wrapping `Store.Close`'s internal error with its cause (`daemon/internal/state` is not owned and the
  contract asks only to join it).
- A CI step asserting exit `1` on a blocked report — the local e2e attempt is not a scenario with a
  blocked path; adding one would edit the oracle without a contract requirement.

## Closure actions

`docs/plans/` gains this plan (repo convention for every slice; w2-12a/w2-13 precedents). `docs/TODO.md`
has no entry this slice closes or creates (no dead code was introduced or found in the owned files;
the full call-site table keeps every writer call live). No goal index, contract, or grammar document
names this writer or its error behaviour, so nothing becomes stale. The contract file is
conductor-owned and unchanged.
