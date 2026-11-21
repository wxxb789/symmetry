# daemon-app/state hygiene plan — four dead stores, one mount-escape mis-parse, two one-argument joins

Contract: `goal-rf-w1-07-app-state-hygiene.md` (GDD, 2026-11-21), branch
`refactor/rf-w1-07-app-state-hygiene`, baseline `origin/main` = this worktree's HEAD =
`a3039f0d17fc7c7905b63bdf247cac5bf339b213` (`a3039f0`).
Source item: program plan §4 W1-07 and `findings/daemon-app.md` F10:364-377 / S5:526, as amended by
`plan-review-ds-1.md` P1-2+A4 and `plan-review-opus-1.md` P1-2. Every anchor below was re-verified at
`a3039f0`.

## Binding amendments applied

1. **Dropped:** rewriting `control_failure_test.go:35` / `supervisory_test.go:73`. Both lines deliver
   the same command twice on purpose (replay/idempotency); the SA4000 report is a false positive. The
   two files are therefore **not in this increment's edit set**, and the contract's clause-1 obligation
   "the two repaired tests are non-vacuous" is superseded by amendment 2's red/green test.
2. **Fixed, not deleted:** `linux_owner_preflight_linux.go:1822` is the ineffectual `i += 3` of an
   escape skip whose loop was rewritten as `for i := range len(value)` (range-over-int, Go 1.22+:
   every iteration rebinds `i`). The skip is ineffectual, so `\040`-style mountinfo escapes are
   decoded to their digits instead of the escaped byte. This increment fixes the loop and owns
   `linux_owner_preflight_linux_test.go` (added to the edit set by the amendment) with a red/green
   table test that fails on the pre-fix bytes.
3. **Oracle changed:** `go vet` flags none of the four cited stores. The dead-store and join oracles
   are `GOOS=linux golangci-lint run --default=none -E staticcheck,ineffassign,gocritic
   ./internal/app/...` (golangci-lint `v2.15.0`, measured) before and after, plus the contract's own
   Windows gates.
4. Each remaining dead store is reviewed individually below; the two `filepath.Join` sites keep their
   behavior (argument below, with the one-argument identity recorded rather than asserted by a
   self-referential test).

## Deliverable shape

1. `daemon/internal/app/recovery.go`: drop the dead store at `:299`; discard the unused
   `queueNativeUsageRecoveryTerminal` journal readback at `:334`. No signature, field, or comment
   change elsewhere.
2. `daemon/internal/app/linux_owner_recovery_linux.go`: replace the pre-initialization at `:481` with
   zero-value declarations (both values are unconditionally assigned - `:484` on the helper-exit path,
   `:492` otherwise - before any read). No other line moves.
3. `daemon/internal/app/linux_owner_preflight_linux.go`: restore the three-clause loop in
   `unescapeLinuxMountPath` (`:1799-1825`) so the escape skip lands, and replace the two one-argument
   `filepath.Join` calls at `:1877-1878` with the literals they produce.
4. `daemon/internal/app/linux_owner_preflight_linux_test.go`: one new table test
   (`TestUnescapeLinuxMountPathDecodesMountinfoEscapes`) pinning all four mountinfo escapes, the
   adjacent-escape and escape-then-literal byte runs, the caller's absolute/canonical acceptance
   condition, and the two rejection classes (five inputs).
5. This plan; the gate and mutant-run outputs go into the stage's `answer.md` (outside the repository).

## Site-by-site dead-store review (no blanket deletion)

| site | tool finding at `a3039f0` | what the value is | review outcome | proof |
| --- | --- | --- | --- | --- |
| `recovery.go:299` | `ineffassign: ineffectual assignment to goalSessionUncertainPersisted` | bookkeeping write after the second `MarkGoalSessionUncertain` (`:294-300`); the function reads the flag only at `:294` | **delete** - the guard at `:294` is the intended dedup, the write has no consumer (the function proceeds to `:302-345`, none of which reads it) | linter finding gone; `go test ./internal/app/...` green |
| `recovery.go:334` | `ineffassign: ineffectual assignment to updated` | reloaded journal returned by `queueNativeUsageRecoveryTerminal` | **discard with `_`** - the `NativeUsageRecoveryRequired` block ends in `return` at `:341-344`, so the readback has no consumer here; the sibling that does thread it (`app.go:10738-10753`, `flushRun`) uses `journal = updated` because more work follows | same |
| `linux_owner_recovery_linux.go:481` | `SA4006: this value of updatedReservation is never used` | zero-value seed of the pair returned by `bindRecoveredV2PreGateStopReceipt` | **delete the seed** - both names are written unconditionally before every read (`:484` then `:492` on the else path); declaring them zero-valued is behavior-identical, and every error return already uses explicit zero values | same |
| `linux_owner_preflight_linux.go:1822` | `ineffassign: ineffectual assignment to i` | escape skip in `unescapeLinuxMountPath` | **fix the loop** (amendment 2), not a removal - the skip was intended and its absence is a validation-input mis-parse | new test red on pre-fix bytes, green after |

The four stores are the complete dead-store set in the owned files: the other two `range`-over-int
loops in the same file (`:1505`, `:2671`) and `internal/platform/linux_owner_anchor_linux.go:1891`
(not owned) never mutate their index, which is why `ineffassign` reports nothing at them.

## `unescapeLinuxMountPath` — public/API shape and exact edge-case semantics

Private (`linux && amd64` file), one caller: `verifyLinuxMountAllowsSetID` (`:1754-1797`, reading
`/proc/self/mountinfo`). Signature and error strings are unchanged. Post-fix behavior, with the
pre-fix behavior for contrast:

| input | pre-fix | post-fix | class |
| --- | --- | --- | --- |
| `/mnt/plain` | `/mnt/plain` | unchanged | no escape |
| `/mnt/a\040b` | `/mnt/a 040b` | `/mnt/a b` | single escape, digits leak pre-fix |
| `/mnt/a\134b` | `/mnt/a\134b` | `/mnt/a\b` | backslash escape, digits leak pre-fix |
| `/mnt/a\040\011b` | `/mnt/a 040\t011b` | `/mnt/a \tb` | adjacent escapes |
| `/mnt/a\040b\040c` | `/mnt/a 040b 040c` | `/mnt/a b c` | escape, literal run, escape |
| `/mnt/a\040` | `/mnt/a 040` | `/mnt/a ` | escape at end of value (bound check `i+3 >= len(value)` is already correct) |
| `\`, `abc\`, `/mnt/\04` | `short mount path escape` | unchanged | truncated escape |
| `/mnt/\777`, `/mnt/\04x` | `unknown mount path escape` | unchanged | unknown escape |

Consequence at the caller ([INFERENCE] from reading `:1774-1788`, not observed at runtime): the decoded
mount point drives the absolute/canonical gate, the longest-match selection, and the `nosuid` lookup
against `fields[5]`/`fields[separator+3]`. A mount point containing an escaped byte is therefore
compared as a path that is not a mount at all, so the mount that actually carries the setuid wrapper
can be missed or displaced by a shorter-matching entry - a fail-closed or a wrong-mount `nosuid`
decision, never a silent skip of the wrapper check. The fix restores the identity the mountinfo entry
states.

## The two one-argument joins

`linux_owner_preflight_linux.go:1877` (`filepath.Join("/run/systemd/transient")`) and `:1878`
(`filepath.Join("/run/systemd")`) are `gocritic: badCall: suspicious Join on 1 argument`. Both sites
are constant, already-clean, absolute Linux paths consumed by `verifyLinuxSystemdDirectory`. Fix:
use the literals. Options considered:

- **Chosen: literal constants** (`transientDirectory := "/run/systemd/transient"`, `"/run/systemd"` at
  the call). One-argument `Join` *is* `Clean` (documented), and both literals are already clean, so the
  produced strings are byte-identical on the `linux && amd64` build; the finding disappears.
- **Rejected: `filepath.Clean(x)`.** Same value, no added proof, one more call.
- **Rejected: a new end-to-end test through `verifyLinuxSystemdUnit`.** It reads the real
  `/etc/systemd/system/<unit>`, `/run/systemd[/transient]` and requires root: on a non-root CI runner
  it can only skip - a vacuous pin - and it would write to host systemd directories to reach the
  transient-override branch.
- **Rejected: a repository test asserting the constant equals its own literal.** Self-referential; it
  cannot fail for any plausible defect, so it protects nothing.

Behavior-preservation evidence instead: the three-clause identity `filepath.Join(x) == filepath.Clean(x)
== x` recorded from the same toolchain in WSL for both arguments, the finding-set diff from the lint
oracle (only the target findings leave, none arrive), and the green suites below. The two-argument
`Join` at `:1876` (`filepath.Join("/etc/systemd/system", policy.unit)`) is not flagged; it stays as
it is.

## Evidence plan (exact commands; never a measurement beside the full suite on this host)

RED - the mutant *is* the pre-fix source: the new test lands in the worktree while the production files
still hold `a3039f0` bytes, and the tree is mirrored into WSL Ubuntu-24.04 (`go1.27.0 linux/amd64`,
`gcc 13.3.0`, module cache warm at `/root/go/pkg/mod`) so the tests can run at all (`linux && amd64`
files, `-race` unavailable natively on this Windows host):

- `go test -count=1 -run TestUnescapeLinuxMountPathDecodesMountinfoEscapes -v ./internal/app/`
  → expect the escape cases red with the pre-fix decode recorded, the rejection cases green.

GREEN - the same command after the three production edits, then:

- `go test -count=1 -run 'TestLinux' -v ./internal/app/` (the Linux-only preflight/owner/clone/witness
  set of the touched file's package) in WSL;
- Windows host, from `daemon/`: `gofmt -l <changed files>`, `go vet ./...`,
  `GOOS=linux go build ./...`, `GOOS=windows go build ./...`,
  `go test -count=1 ./internal/app/... ./internal/state/...`;
- lint oracle before/after, `GOOS=linux golangci-lint run --default=none
  -E staticcheck,ineffassign,gocritic ./internal/app/...`, diffed finding sets (expect exactly the six
  target findings removed: 3 `ineffassign`, 1 `SA4006`, 2 `badCall`, and nothing new);
- `git diff --check`.
- CI-only oracle (conductor): the daemon `-race` jobs; not run locally by contract.

## Design trade-offs

1. **No production change beyond the four stores and the two join sites.** The slice is hygiene; the
   escape fix is included because the amendment proves the "dead store" is a live defect, and its
   correct minimal form is restoring the loop, not deleting the skip.
2. **Pre-fix source as the mutant.** The test-first order means no second copy of the file is needed
   for the red run, and the recorded red output names the exact pre-fix decode.
3. **No new suppression and no lint configuration.** No `//nolint`, no `golangci` config file: the CI
   quality plane is W1-01's file set, and the oracle here is a CLI-scoped run.
4. **The two dropped assertion files are left exactly as they are**, including their staticcheck
   SA4000 reports; the W1-01 advisory baseline owns that count.

## Out of scope (with reasons)

- `control_failure_test.go`, `supervisory_test.go` (amendment 1).
- Other findings in the same files, unchanged: `linux_owner_preflight_linux.go:1144` (`QF1001` De
  Morgan), `app.go:2327/3580/5227/7434/10373` (`gocritic` elseif/ifElseChain),
  `linux_owner_witness_linux.go:2289` (elseif), `output_settlement_test.go:294` (`appendAssign`),
  `harness_test.go:2198` / `omo_*_test.go` / `linux_owner_v2_app_test.go` (`QF1003/QF1008/S1016`).
- Test split (W3-15), prod errcheck triage (W3-09), state validator helpers (W2-09), the app.go split
  (W1-05).
- A test seam for `verifyLinuxMountAllowsSetID` (it reads the host's `/proc/self/mountinfo`): the
  decode is pinned directly instead of introducing an injection point for one preflight.
- `docs/design/**`, `docs/runbooks/**`: no document describes the escape decode, the join sites, or
  the removed stores (checked by `grep -rn "mountinfo\|unescape\|dead store\|ineffassign" docs/`), so
  nothing becomes stale; no TODO entry is closed by this slice (`docs/TODO.md` has no W1-07 entry).
  The slice does record three pre-existing dead stores of the same class (`app_test.go:4881`,
  `stale_ack_settlement_test.go:32`, `:125`) as a new `docs/TODO.md` dead-code candidate, with the
  same lint evidence and a per-site review note; they are outside this slice's owned files.

## Residual risks and limits

- The red/green proof runs in WSL, not on the CI runner; the CI daemon jobs are the only place the
  `linux && amd64` package is exercised natively together with `-race`.
- The caller-consequence sentence above is code-reading inference; the pin covers the decode, not a
  live mount table.
- The two join sites are behavior-identical by construction, so their evidence is an identity argument
  plus the lint finding-set diff, not a functional test (see the rejected options).
