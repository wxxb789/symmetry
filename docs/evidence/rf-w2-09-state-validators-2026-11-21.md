# rf-w2-09 state validator section helpers — evidence record (2026-11-21)

Subject: the four top-level state validators at base revision `12a94c79a0057ac762c0b4f4e7a0af5f9b37abcb`
(= `origin/main`; the `rf-w1-08-daemon-test-enablers` `#148` merge) decomposed move-only into
per-section helpers inside the same package:

| validator | base file:line | base span | base cyclo | after span | after cyclo | helpers |
|---|---|---:|---:|---:|---:|---:|
| `validateJournal` | `state.go:5817` | 202 | 171 | 39 | 13 | 11 |
| `validateLinuxCloneJournal` | `state.go:4881` | 173 | 140 | 29 | 11 | 16 |
| `(LinuxCgroupReservation).Validate` | `state.go:144` | 69 | 78 | 30 | 10 | 9 |
| `validateGoalSessionJournal` | `goal_session.go:1647` | 73 | 70 | 30 | 10 | 8 |

Base spans/cyclo reproduce the program findings (`refactor/findings/daemon-app.md` F5: 202 L/171,
173 L/140, 73 L/70, 69 L/78) and C6's pinned base values exactly. Contract:
`goal-rf-w2-09-state-validators.md` including the conductor decision round 2 (C1–C7); plan:
`docs/plans/2026-11-21-rf-w2-09-state-validators-plan.md`. All gates below ran foreground in this
worktree on 2026-11-21. No `-race` run locally (no cgo on this Windows host — CI is the race
oracle, as the contract states).

## 1. Move-only proof

Three independent checks, all on the delivered tree against `git show 12a94c79:<file>`:

1. **Ordered-subsequence + allowed-new-line proof** (scratch tool `validmove`, `go/ast` + line
   comparison): the validator group flattened by call graph (parent, then the helpers spliced at
   their call sites) must contain every base source line (whitespace-normalised, so
   `--color-moved-ws=allow-indentation-change` re-indentation is allowed) in the original order,
   the delivered-only lines must all be helper headers, braces, `if err := validateX(...); err != nil {`
   call lines, `return err`, `return nil` or `return validateX(...)`, and the ordered sequence of
   `errors.New` literals must be identical.

```
validmove -base <12a94c79 state.go> -new daemon/internal/state/state.go \
  -group validateJournal=<11 helpers> -group validateLinuxCloneJournal=<16 helpers> \
  -group LinuxCgroupReservation.Validate=<9 helpers>
GROUP validateJournal: PASS (base lines=200, errors=37)
GROUP validateLinuxCloneJournal: PASS (base lines=171, errors=31)
GROUP LinuxCgroupReservation.Validate: PASS (base lines=67, errors=17)
STATE_MOVE_EXIT=0
validmove -base <12a94c79 goal_session.go> -new daemon/internal/state/goal_session.go \
  -group validateGoalSessionJournal=<8 helpers>
GROUP validateGoalSessionJournal: PASS (base lines=71, errors=21)
GOAL_MOVE_EXIT=0
```

Zero base lines missing, zero reordered, zero delivered-only lines outside the allowed set in all
four groups. Negative controls on the same tool: one changed error literal and one swapped
authority/handoff block both report FAIL (exit 1), so the check discriminates.

2. **`git diff --color-moved=zebra --color-moved-ws=allow-indentation-change`** (C3's own command):
   `daemon/internal/state/state.go | 423 +++---`, `daemon/internal/state/goal_session.go | 62 +++-`,
   383 insertions / 102 deletions over the two production files, exit 0. Every removed
   `if`-condition and `return errors.New(...)` line appears as a moved (relocated) line; the
   non-moved additions are the new helper headers/braces, the call lines, and the `return nil` that
   already terminated an extracted sub-branch. `git diff --numstat` for the whole slice:

| file | + | − |
|---|---:|---:|
| `daemon/internal/state/state.go` | 324 | 99 |
| `daemon/internal/state/goal_session.go` | 59 | 3 |
| `daemon/internal/state/state_test.go` | 788 | 0 |
| `daemon/internal/state/goal_session_test.go` | 212 | 0 |
| `daemon/internal/state/linux_clone_journal_test.go` | 603 | 0 |

3. **No collateral edits.** Every `-U0` hunk of `daemon/internal/state/state.go` starts inside one of
   the three validator spans of that file (hunk starts `144 152 158 164 177 185 188 192 205` ⊆
   `Validate` 144-212; `4883 … 5034` ⊆ `validateLinuxCloneJournal` 4881-5053; `5817 … 5992` ⊆
   `validateJournal` 5817-6018), and every `goal_session.go` hunk starts inside
   `validateGoalSessionJournal` (1647-1719). No other declaration in either file is touched.

Comparison carrier: whitespace-stripped lines (the rf-w1-05 precedent's carrier), because extracted
blocks move one indent level outward; `gofmt -l internal/state/` is empty, so the delivered
indentation is canonical.

## 2. Measurement (C6) — pinned tools, before and after

The 40-line target is pinned by a span/cyclo check over the sliced function set: a scratch `go/ast`
tool (no repository changes) that reports `end-start+1` lines and a gocyclo-identical visitor
(`FuncDecl`/`FuncLit`/`IfStmt`/`ForStmt`/`RangeStmt`/`CaseClause`/`CommClause` plus `&&`/`||`).
Its base output reproduces C6's values exactly (202/171, 173/140, 73/70, 69/78) and it agrees with
`golangci-lint` 2.15.0 `gocyclo` on every function that linter reports, so it is a faithful carrier
for the ≤40-line half that `funlen`'s 120-line limit does not enforce.

```
funclen -dir daemon/internal/state -match '<the 4 validators and their 44 helpers>' \
  -max-lines 40 -max-cyclo 25
PINNED_LIMIT_EXIT=0 SLICED_FUNCTIONS=49 MAX_SPAN=39 MAX_CYCLO=25
```

49 rows = the 48 sliced functions plus `(NativeUsageObservation).Validate` matched by the shared
`Validate` name filter. Per-function spans and cyclomatic complexity of the delivered slice
(`file,line,span,cyclo`; the 44 new helpers are unexported, the 4 top-level validators keep their
names):

```
validateGoalSessionJournal,goal_session.go,1647,30,10
validateGoalSessionSchema,goal_session.go,1678,8,4
validateGoalSessionStates,goal_session.go,1687,6,10
validateGoalSessionLaunchAttempt,goal_session.go,1694,12,6
validateGoalSessionLaunchOutcome,goal_session.go,1707,21,14
validateGoalSessionControlAttachment,goal_session.go,1729,13,12
validateGoalSessionStopReceiptHistory,goal_session.go,1743,12,8
validateGoalSessionResumeState,goal_session.go,1756,13,13
validateGoalSessionStopReceipt,goal_session.go,1770,6,9
(LinuxCgroupReservation).Validate,state.go,144,30,10
(LinuxCgroupReservation).validateBaseIdentity,state.go,175,11,15
(LinuxCgroupReservation).validateLeafIdentity,state.go,187,9,6
(LinuxCgroupReservation).validateHelperIdentity,state.go,197,9,6
(LinuxCgroupReservation).validateLauncher,state.go,207,16,13
(LinuxCgroupReservation).validateGate,state.go,224,11,12
(LinuxCgroupReservation).validateHelperExitProof,state.go,236,6,4
(LinuxCgroupReservation).validateMirrorAliases,state.go,243,7,8
(LinuxCgroupReservation).validateProofs,state.go,251,16,12
(LinuxCgroupReservation).validateCreationOrder,state.go,268,8,10
validateLinuxCloneJournal,state.go,4944,29,11
validateLinuxCloneReuseBranch,state.go,4974,12,4
validateLinuxCloneReuseGrant,state.go,4987,12,15
validateLinuxCloneReuseAbortState,state.go,5000,12,9
validateLinuxCloneReuseRetainedTarget,state.go,5013,11,7
validateLinuxCloneIntentAbsent,state.go,5025,10,13
validateLinuxCloneIntent,state.go,5036,10,7
validateLinuxCloneSeedReceipt,state.go,5047,6,4
validateLinuxCloneAbort,state.go,5054,13,9
validateLinuxCloneAbortPending,state.go,5068,9,5
validateLinuxCloneAbortProof,state.go,5078,18,12
validateLinuxCloneCheckout,state.go,5097,16,10
validateLinuxClonePromotion,state.go,5114,25,18
validateLinuxClonePromotionWithoutIntent,state.go,5140,20,13
validateLinuxCloneCleanup,state.go,5161,17,9
validateLinuxCloneCleanupPending,state.go,5179,11,7
validateLinuxCloneCleanupProof,state.go,5191,11,10
validateJournal,state.go,5965,39,13
validateJournalKeyAndScalars,state.go,6005,19,24
validateContainmentAuthority,state.go,6025,21,15
validateContainmentHandoff,state.go,6047,21,14
validateContainmentCgroup,state.go,6069,28,22
validateContainmentScans,state.go,6098,29,24
validateContainmentCgroupReleaseProof,state.go,6128,12,14
validateContainmentCgroupPreGateReleaseProof,state.go,6141,11,13
validateJournalLeaseTerminalAndGrantInvariants,state.go,6153,22,25
validatePendingJournalEntriesAndDeliveries,state.go,6176,31,13
validateJournalNativeUsageObservation,state.go,6208,8,3
validateJournalInputAndCommandIntents,state.go,6217,27,13
```

`golangci-lint run` 2.15.0 with `gocyclo.min-complexity: 25` over `./internal/state/...` (temporary
config outside the repository):

| run | findings | exit | delta |
|---|---:|---:|---|
| base `12a94c79` (`base-gocyclo25.txt`) | 36 | 1 | — |
| delivered (`after-gocyclo25.txt`) | 32 | 1 | −4, exactly `validateJournal`, `validateLinuxCloneJournal`, `validateGoalSessionJournal`, `(LinuxCgroupReservation).Validate`; **zero** new findings |

The remaining 32 findings are pre-existing functions outside this slice's owned files or outside its
extraction set (patch/scan/handoff helpers, `validateControlCommandIntents` in `control.go`,
`validateGoalSessionIntent`, `validTerminalState`, plus 12 test functions), identical by name before
and after.

## 3. Pins first (C4) and the mutation check

No test asserted any of these strings at the base revision, so three table tests were written
against the pristine tree before the extraction and re-run after it (they must stay green — any
reordered condition, merged/split string or changed verdict turns them red):

| test | driver | subtests | literals | not covered |
|---|---|---:|---:|---|
| `TestJournalValidatorFirstErrorStringsArePinned` (`state_test.go`) | `store.mutateJournal` (load → mutate → `saveJournalWithLocked` → `validateJournal`); `(LinuxCgroupReservation).Validate` called directly because the journal-level surface collapses its inner strings and `ReserveLinuxCgroup` is `linux/amd64`-gated | 54 subtests | 37/41 | 4 proven unreachable (§5) |
| `TestGoalSessionValidatorFirstErrorStringsArePinned` (`goal_session_test.go`) | `store.mutateGoalSession` | 25 subtests | 17/17 | none |
| `TestLinuxCloneValidatorFirstErrorStringsArePinned` (`linux_clone_journal_test.go`, `//go:build linux`) | `store.mutateJournal` on store-built clone chains | 31 subtests | 31/31 | none |

Counts from `go test -count=1 -v -run FirstErrorStringsArePinned ./internal/state/`:

| platform | journal | goal session | clone | failures |
|---|---:|---:|---:|---:|
| Windows (local, foreground) | 54 | 25 | not compiled (`//go:build linux`) | 0 |
| WSL `Ubuntu-24.04` (Linux) | 54 | 25 | 31 | 0 |

**Mutation check** (C4, "flip the guarded behaviour, red shown"): inside the extracted helper
`validateGoalSessionStates` (`goal_session.go:1688`) the control-attachment operand
`!validGoalSessionUUID(journal.ControlSessionID)` was flipped to
`validGoalSessionUUID(journal.ControlSessionID)`; the pin went red with an assertion-level mismatch
(`goal_session_test.go:2214: mutateGoalSession() error = goal session control attachment is invalid,
want "goal session journal is invalid"`, plus two order-shifted cases), exit 1; after reverting the
flip the pin is green again (25/25). A second flip (`validateGoalSessionSchema`'s version operand)
turned the same pin red through the seeding path, exit 1. Both flips were reverted; the delivered
tree carries the base conditions.

## 4. Gate results (exact commands, exit codes)

All from `daemon/` unless stated; `gofmt`/`vet`/builds/tests foreground on the frozen tree.

| gate | command | result |
|---|---|---|
| gofmt | `gofmt -l internal/state/` | no output, exit 0 |
| vet | `go vet ./...` | no output, exit 0 |
| build linux/amd64 | `GOOS=linux GOARCH=amd64 go build ./...` | exit 0 |
| build windows/amd64 | `GOOS=windows GOARCH=amd64 go build ./...` | exit 0 |
| linux vet (compiles `//go:build linux` state tests) | `GOOS=linux GOARCH=amd64 go vet ./internal/state/` | no output, exit 0 |
| package tests (Windows) | `go test -count=1 ./internal/state/...` | `ok github.com/wxxb789/symmetry/daemon/internal/state`, exit 0 (18.819s mid-slice, 75.131s on the final frozen-tree run alongside the cross-builds) |
| package tests (Linux, C2) | `wsl.exe -d Ubuntu-24.04 -u accept -- bash <scratch>/wsl-verify.sh` (`cd /mnt/q/…/symmetry/daemon`; `go test -count=1 ./internal/state/...`) | `ok github.com/wxxb789/symmetry/daemon/internal/state 17.448s`, exit 0 |
| Linux suite detail | `go test -count=1 -v ./internal/state/` (WSL, final run) | 241 top-level PASS, 0 FAIL, 73 linux-tagged cgroup/clone test runs, `ok … 26.726s`, exit 0 |
| Linux pin detail | `go test -count=1 -v -run FirstErrorStringsArePinned ./internal/state/` (WSL) | 3 pin tests, 58 PASS lines, 0 FAIL, exit 0 |
| whitespace | `git diff --check` | exit 0 |
| host-path scan | `git diff` + all changed/untracked files grepped for `Users/`, `Users\`, `lhan`, `Q:/repos`, `/mnt/q`, `AppData` | exit 0; the only hits are this record's and the plan's own text describing the scan and the elided WSL path prefix `/mnt/q/…`, which carries no account name |
| move proof | `validmove` over both revisions, 4 groups | PASS, exit 0 (§1) |
| colour-moved | `git diff --color-moved=zebra --color-moved-ws=allow-indentation-change --stat` | exit 0 (§1) |
| pinned ≤40/≤25 | `funclen … -max-lines 40 -max-cyclo 25` | exit 0, max span 39, max cyclo 25 (§2) |
| gocyclo gate | `golangci-lint run --config <gocyclo min-complexity 25> ./internal/state/...` | 32 findings, all pre-existing; delta vs base = exactly the four validators removed (§2) |
| race | not run locally (`-race` needs cgo) | CI daemon race job is the oracle (contract) |

## 5. Delivered contract deviations, all deliberate

1. **`validateJournal` carries 12 checks, not one per enumerated block.** The three-line
   replacement-call form `if err := validateX(journal); err != nil { return err }` is what `gofmt`
   accepts (it splits one-line `if` bodies), so a 13-helper chain would make `validateJournal` 42-45
   lines and miss the ≤40-line acceptance. The smallest allocation that satisfies every acceptance
   is 12 checks: `validateJournalKeyAndScalars` (folds the moved `validateKey` call plus the scalar
   block), the four reserved containment helpers, the two release-proof helpers,
   `validateJournalLeaseTerminalAndGrantInvariants` (lease/terminal + claim-grant-free),
   `validateLinuxCloneJournal` and `validateGoalDeliveries`-carrying
   `validatePendingJournalEntriesAndDeliveries` (pending events/transitions/acknowledgements +
   goal deliveries), `validateJournalNativeUsageObservation`, and
   `validateJournalInputAndCommandIntents` (input-command intent + the two moved command-intent
   calls). Every enumerated block is extracted as code in the same order with the same first-error
   strings; none is left inline. The alternative — a table-driven call chain — was rejected because
   it would replace moved call lines with constructed lines that C3's allowed-new-line set does not
   cover.
2. **`validateLinuxCloneNonReuseBranch` is a region, not a function.** The base revision has no such
   declaration; C5's per-stage refinement ("intent, seed, abort, checkout, promotion, cleanup")
   decomposes that region, and each stage is its own helper. The reuse region is
   `validateLinuxCloneReuseBranch`.
3. **Unreachable pre-existing branches are moved, not fixed.** Six conditions cannot fire at the base
   revision; they stay byte-identical (move-only) and are recorded as `docs/TODO.md` candidates with
   the reasoning: `v2 cgroup reservation cannot accompany a v1 authority` and `… a v1 handoff`
   (the earlier authority/handoff blocks return first), `Linux cgroup reservation cannot have both
   abort and physical-stop proof`, `Linux cgroup aborted reservation lost producer-exit evidence`,
   `Linux cgroup reservation has effects before creation intent`, and `unresolved input command has
   an acknowledgement`. The pin test's reachability probes confirmed all six.
4. **Latent nil dereference preserved, not repaired, and now verified.** When a journal carries a
   checkout receipt without a seed receipt, `validateLinuxCloneCheckout` dereferences a nil `*seed`
   (`linuxCloneCheckoutMatches(*intent, *seed, *checkout)`, `state.go:5108`) although the earlier
   seed check only fires for `seed != nil`. Verified on WSL 2026-11-21 with a temporary probe test —
   a store journal built by `newPinnedLinuxCloneBase(…, "seed")` with `LinuxCloneCheckoutReceipt`
   set and `LinuxCloneSeedReceipt` cleared reported `PANIC_RECOVERED: runtime error: invalid memory
   address or nil pointer dereference` (probe removed; the file's diffstat returned to the pin
   test's 603 added lines). `readJournalEntryLocked` wraps `validateJournal`, so a hand-crafted
   journal file can panic a `ListJournals`/`LoadJournal` caller. Recorded as a `docs/TODO.md`
   candidate with the recipe; changing it changes a validation verdict and is out of a move-only
   slice.

## 6. Residual risks and known limits

- **Pins cover the reachable strings, not all branches.** Six conditions are unreachable (four proven
  by probe in the journal pin, two excluded by the same guard-order reasoning); for those the
  extraction's protection is the ordered-literal/subsequence proof (§1), which does cover them.
- **The clone-journal pins compile only on Linux** (`//go:build linux`, existing file tag), so the
  Windows lane runs 79 of the 110 pin subtests (54 journal + 25 goal session); WSL and CI run all
  110.
- **Measurement tooling is scratch, not committed** (precedent rf-w1-05): the span/cyclo tool, the
  move checker, the temporary `golangci-lint` config and the base-revision copies live outside the
  repository. Their inputs (base revision, group lists, thresholds) are recorded above so the run is
  reproducible.
- **`validateJournalLeaseTerminalAndGrantInvariants` sits at cyclo 25**, the acceptance boundary;
  `validateJournalKeyAndScalars` and `validateContainmentScans` sit at 24. A future condition added
  to any of them crosses the limit, so the next edit to those blocks is a split, not an addition.
- **No new `//nolint`/`credo:disable` suppressions** were added; no gate was relaxed, skipped or
  re-run to green: the only re-runs were after the deliberate reverted mutations.
