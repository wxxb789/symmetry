# rf-w2-09 state validator section helpers — evidence record (2026-11-21)

Subject: the four top-level state validators at base revision `12a94c79a0057ac762c0b4f4e7a0af5f9b37abcb`
(= `origin/main`; the `rf-w1-08-daemon-test-enablers` `#148` merge) decomposed move-only into
per-section helpers inside the same package:

| validator | base file:line | base span | base cyclo | after span | after cyclo | helpers |
|---|---|---:|---:|---:|---:|---:|
| `validateJournal` | `state.go:5817` | 202 | 171 | 39 | 13 | 11 |
| `validateLinuxCloneJournal` | `state.go:4881` | 173 | 140 | 35 | 13 | 15 |
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
   call lines, `return err` or `return nil` — exactly the C3 letter, with no `return validateX(...)`
   allowance — and the ordered sequence of `errors.New` literals must be identical.

   The fix stage removed the nine `return validateX(...)` clone-stage tail calls the review flagged
   (R3) and rewrote them to the enumerated call form; two structural consequences were needed to
   stay inside the ≤40-line acceptance: the reuse dispatcher `validateLinuxCloneReuseBranch` came
   back out of `validateLinuxCloneJournal` (the apply-stage inlining put the parent at 41 lines) and
   the intent-nil dispatch stayed inside the parent rather than moving into
   `validateLinuxCloneIntent`, because folding it in lets a nil-intent journal fall through to
   `validateLinuxCloneCheckout` and dereference `intent.Target` (a probe caught this; see §3).
   The P0-1 repair (this commit) keeps that dispatcher and makes it take the non-nil grant as an
   argument; the parent returns immediately after it again (see §1a).

```
validmove -base <12a94c79 state.go> -new daemon/internal/state/state.go \
  -only validateJournal -only validateLinuxCloneJournal -only LinuxCgroupReservation
GROUP validateJournal: PASS (base lines=200, errors=37, delivered lines=257, allowed additions=57)
GROUP validateLinuxCloneJournal: PASS (base lines=171, errors=31, delivered lines=253, allowed additions=82)
GROUP (LinuxCgroupReservation).Validate: PASS (base lines=67, errors=17, delivered lines=114, allowed additions=47)
MOVE_EXIT=0
validmove -base <12a94c79 goal_session.go> -new daemon/internal/state/goal_session.go \
  -only validateGoalSessionJournal
GROUP validateGoalSessionJournal: PASS (base lines=71, errors=21, delivered lines=113, allowed additions=42)
MOVE_EXIT=0
```

Zero base lines missing, zero reordered, zero delivered-only lines outside the C3 set in all four
groups. Negative controls on the same tool: one changed error literal and one swapped
authority/handoff block both report FAIL (exit 1), so the check discriminates. The journal group
splices only the 11 helpers extracted from the base `validateJournal`; `validateLinuxCloneJournal`
was a pre-existing helper at the base revision, so its call line is compared as a moved line and its
body is covered by its own group.
Re-run after the P0-1 repair on the delivered bytes: all four groups PASS again with the same counts
and exit 0 (§4); the repair changes which lines the clone group flattens but not the verdict.

2. **`git diff --color-moved=zebra --color-moved-ws=allow-indentation-change`** (C3's own command):
   `daemon/internal/state/state.go | 443 +++---`, `daemon/internal/state/goal_session.go | 62 +++-`,
   408 insertions / 97 deletions over the two production files, exit 0. Every removed
   `if`-condition and `return errors.New(...)` line appears as a moved (relocated) line; the
   non-moved additions are the new helper headers/braces, the call lines, and the `return nil` that
   already terminated an extracted sub-branch. No `return validateX(...)` tail call remains in the
   slice, so the allowed-new-line set is exactly C3's. `git diff --numstat` for the whole slice
   (fix revision 2026-11-21):

| file | + | − |
|---|---:|---:|
| `daemon/internal/state/state.go` | 349 | 94 |
| `daemon/internal/state/goal_session.go` | 59 | 3 |
| `daemon/internal/state/state_test.go` | 879 | 0 |
| `daemon/internal/state/goal_session_test.go` | 222 | 0 |
| `daemon/internal/state/linux_clone_journal_test.go` | 671 | 129 |

3. **No collateral edits.** Every `-U0` hunk of `daemon/internal/state/state.go` starts inside one of
   the three validator regions of that file (hunk starts `145 184 194 204 221 233 240 248 265` ⊆
   `(LinuxCgroupReservation).Validate` + helpers 144-275; `4945 4950 4955 5053 5060 5067 5077 5126
   5135 5158 5198 5203` ⊆ `validateLinuxCloneJournal` + helpers 4944-5231; `5996 6052 6074 6096 6125
   6155 6168 6180 6203 6235 6244` ⊆ `validateJournal` + helpers 5995-6273), and every
   `goal_session.go` hunk starts inside `validateGoalSessionJournal` + helpers (`1647 1653 1658
   1667 1685 1695 1704 1714` ⊆ 1647-1775). Since every hunk start lands inside a sliced declaration
   or one of its helpers, no other declaration in either file is touched.
   The P0-1 repair adds hunks only inside the same regions (parent `4944-4978` and
   `validateLinuxCloneReuseBranch` `4984-4995`) and in the Linux-tagged regression test appended to
   `linux_clone_journal_test.go`; region bounds after `4995` shift by +1 line.

All three checks were re-derived 2026-11-21 on the fix revision (R3 rewritten to the enumerated call
form, the reuse dispatcher restored, the operand-isolation cases added); the base-line counts
(200/171/67/71) and error-literal counts (37/31/17/21) are unchanged by the extraction, and are
again identical after the P0-1 repair's re-run (`validmove` 4/4 PASS, exit 0 — §1a, §4).

Comparison carrier: whitespace-stripped lines (the rf-w1-05 precedent's carrier), because extracted
blocks move one indent level outward; `gofmt -l internal/state/` is empty, so the delivered
indentation is canonical.

### 1a. What this proof does and does not prove — control-flow audit (P0-1)

The three checks above establish **line movement and order**: every base source line reappears in
the flattened delivered group in the original order, the delivered-only lines are inside the C3
set, and the `errors.New` literal sequence is unchanged. They do **not** establish that a base
early `return` still exits the parent the same way at HEAD. `return nil` is itself in the C3
allowed-new-line set and the flattening inlines helpers at their call sites, so a helper that
returns `nil` for a branch whose base code returned *from the parent*, followed by a parent that
falls through, passes all three checks. Review round 1 (opus, finding P0-1) found exactly that
class in `validateLinuxCloneJournal`: the R3 rewrite of the tail calls (commit `0d9ed577`) dropped
the reuse branch's trailing `return nil`, so a valid reuse journal that is retired or aborted
before native start fell through to `validateLinuxCloneIntentAbsent` and was rejected with
`run journal has Linux clone evidence without an intent` — on the save *and* load paths
(`readJournalEntryLocked`/`loadJournalLocked`), which also breaks every already-persisted retired
reuse journal and the Linux app test `TestBindLinuxCloneReuseRequiresStoppedSourceAndStableSession`.

**Fix (this commit).** The parent keeps the base line verbatim and the helper takes the non-nil
grant as an argument — the `5770afd1` shape, not the `journal.LinuxCloneReuseGrant != nil` guard
variant: `validmove` rejects the guard variant because both its new lines (`if
journal.LinuxCloneReuseGrant != nil {` and `reuse := journal.LinuxCloneReuseGrant`) fall outside
the C3 allowed set, while the shape below adds only call lines, braces and `return nil`:

```go
	if reuse := journal.LinuxCloneReuseGrant; reuse != nil {
		if err := validateLinuxCloneReuseBranch(journal, reuse); err != nil {
			return err
		}
		return nil
	}
```

`validateLinuxCloneJournal` is 35 lines / CC 13 and `validateLinuxCloneReuseBranch` 12 / CC 4, both
inside the acceptance. `TestLinuxCloneReuseRetiredJournalsStayLoadable` (Linux-tagged, §4) is the
control-flow oracle: it binds a reuse grant through the clone-pin reuse fixture, saves a retired
alias and an aborted-before-native-start journal through the production persistence seam, and
reads both back with `LoadJournal`. It is red on the pre-fix revision (exit 1, both subtests:
`run journal has Linux clone evidence without an intent`) and green on the delivered bytes
(exit 0).

**Audit of every early exit in the four decomposed validators.** Method: enumerate every `return`
inside the base validators from the merge-base `a1142d4e` (identical for these two files to the
record's `12a94c79`), keep the `return nil` sites, and map each to its HEAD construct. Only
`validateLinuxCloneJournal` has mid-function `return nil` sites (10); `(LinuxCgroupReservation).
Validate`, `validateJournal` and `validateGoalSessionJournal` each have exactly one `return nil` —
the function tail — so the lost-early-return class cannot arise there, and their `return err`
sites are helper-call guards still checked at HEAD (`validateKey`, `validateGoalDeliveries`,
`validateControlCommandIntents`, `validateProviderActionIntents`). `preserved?` is for the
delivered revision:

| base site | base early exit | HEAD site | preserved? |
|---|---|---|---|
| `state.go:4909` | reuse branch end `return nil` | `state.go:4945-4949` parent (`return nil` 4949) | **no in `0d9ed577`; restored in this commit** |
| `state.go:4920` | `intent == nil` block `return nil` | `state.go:4952-4956` parent | yes |
| `state.go:4951` | pending-abort branch `return nil` | `state.go:5072-5076` `validateLinuxCloneAbort` | yes |
| `state.go:4968` | abort-proof branch end `return nil` | `state.go:5078-5081` `validateLinuxCloneAbort` (proof helper tail `5110`) | yes |
| `state.go:4978` | `checkout == nil` branch `return nil` | `state.go:5115-5122` `validateLinuxCloneCheckout` | yes |
| `state.go:4994` | `promotion == nil` inside no-intent branch | `state.go:5171-5175` `validateLinuxClonePromotionWithoutIntent` | yes |
| `state.go:5002` | no-intent branch end `return nil` | `state.go:5135-5139` `validateLinuxClonePromotion` | yes |
| `state.go:5016` | promotion-receipt-nil branch `return nil` | `state.go:5147-5153` `validateLinuxClonePromotion` | yes |
| `state.go:5027` | cleanup-intent-nil branch `return nil` | `state.go:5187-5192` `validateLinuxCloneCleanup` | yes |
| `state.go:5042` | cleanup-pending branch `return nil` | `state.go:5198-5202` `validateLinuxCloneCleanup` | yes |
| `state.go:5052` | final `return nil` | `state.go:4977` parent tail | yes |
| reservation `state.go:144-212` | none mid-function (tail `211`) | `state.go:144-173`: 9 checked calls + tail | n/a |
| journal `state.go:5817-6018` | none mid-function (tail `6017`); 6 helper `return err` guards | `state.go:5996-6034`: 12 checked calls + tail; guards intact | n/a |
| goal session `goal_session.go:1647-1719` | none mid-function (tail `1718`) | `goal_session.go:1647-1676`: 9 checked calls + tail | n/a |

Limit: the audit is structural and one-time. The regression test pins the reuse-branch success
verdicts positively; every other branch's verdicts are pinned by the first-error suites (§3), which
exercise errors but not success readbacks. A future extraction must repeat this audit (§6).

## 2. Measurement (C6) — pinned tools, before and after

The 40-line target is pinned by a span/cyclo check over the sliced function set: a scratch `go/ast`
tool (no repository changes) that reports `end-start+1` lines and a gocyclo-identical visitor
(`IfStmt`/`ForStmt`/`RangeStmt`/non-default `CaseClause`/non-default `CommClause` plus `&&`/`||`,
matching `gocyclo`'s "default clauses do not count" rule). Its base output reproduces C6's values
exactly (202/171, 173/140, 73/70, 69/78), and on the delivered tree it matches `golangci-lint`
2.15.0 `gocyclo` on every one of the 21 production functions that linter still reports (0
mismatches), so it is a faithful carrier for the ≤40-line half that `funlen`'s 120-line limit does
not enforce.

```
funclen -dir daemon/internal/state -match '<the 4 validators and their 44 helpers>' \
  -max-lines 40 -max-cyclo 25
PINNED_LIMIT_EXIT=0 SLICED_FUNCTIONS=49 MAX_SPAN=39 MAX_CYCLO=25
```

49 rows = the 48 sliced functions plus `(NativeUsageObservation).Validate` matched by the shared
`Validate` name filter. Per-function spans and cyclomatic complexity of the delivered slice
(`file,line,span,cyclo`; the 44 new helpers are unexported, the 4 top-level validators keep their
names; fix revision plus the P0-1 repair, 2026-11-21):

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
validateLinuxCloneJournal,state.go,4944,35,13
validateLinuxCloneReuseBranch,state.go,4984,12,4
validateLinuxCloneReuseGrant,state.go,4997,12,15
validateLinuxCloneReuseAbortState,state.go,5010,12,9
validateLinuxCloneReuseRetainedTarget,state.go,5023,11,7
validateLinuxCloneIntentAbsent,state.go,5035,10,13
validateLinuxCloneIntent,state.go,5046,10,7
validateLinuxCloneSeedReceipt,state.go,5057,6,4
validateLinuxCloneAbort,state.go,5064,19,11
validateLinuxCloneAbortPending,state.go,5084,9,5
validateLinuxCloneAbortProof,state.go,5094,18,12
validateLinuxCloneCheckout,state.go,5113,19,11
validateLinuxClonePromotion,state.go,5133,31,20
validateLinuxClonePromotionWithoutIntent,state.go,5165,20,13
validateLinuxCloneCleanup,state.go,5186,23,11
validateLinuxCloneCleanupPending,state.go,5210,11,7
validateLinuxCloneCleanupProof,state.go,5222,11,10
validateJournal,state.go,5996,39,13
validateJournalKeyScalarsAndProcessDetails,state.go,6036,19,24
validateContainmentAuthority,state.go,6056,21,15
validateContainmentHandoff,state.go,6078,21,14
validateContainmentCgroup,state.go,6100,28,22
validateContainmentScans,state.go,6129,29,24
validateContainmentCgroupReleaseProof,state.go,6159,12,14
validateContainmentCgroupPreGateReleaseProof,state.go,6172,11,13
validateJournalLeaseTerminalAndGrantInvariants,state.go,6184,22,25
validatePendingJournalEntriesAndDeliveries,state.go,6207,31,13
validateJournalNativeUsageObservation,state.go,6239,8,3
validateJournalInputAndCommandIntents,state.go,6248,27,13
(NativeUsageObservation).Validate,goal_usage.go,23,8,8
```

Re-derived after the P0-1 repair (`funclen` re-run with the same thresholds): 49 rows, 0
violations, exit 0; `MAX_SPAN=39` (`validateJournal`) and `MAX_CYCLO=25`
(`validateJournalLeaseTerminalAndGrantInvariants`) are unchanged. The two revised declarations are
`validateLinuxCloneJournal` 35/13 and `validateLinuxCloneReuseBranch` 12/4; every later `state.go`
row shifts by 1 line, while `goal_session.go` and the reservation block are untouched.

`golangci-lint run` 2.15.0 with `gocyclo.min-complexity: 25` over `./internal/state/...` (temporary
config outside the repository):

| run | findings | exit | delta |
|---|---:|---:|---|
| base `12a94c79` (`base-gocyclo25.txt`) | 36 | 1 | — |
| apply revision (`apply-gocyclo25.txt`) | 32 | 1 | −4, exactly `validateJournal`, `validateLinuxCloneJournal`, `validateGoalSessionJournal`, `(LinuxCgroupReservation).Validate`; **zero** new findings |
| fix revision, re-run 2026-11-21 (`gocyclo-head2.txt`) | 32 | 1 | finding set identical **by function name** to the apply/review runs; the four validators still absent |
| fix revision + P0-1 repair, re-run 2026-11-21 (`gocyclo25-after-p01.txt`) | 32 | 1 | same 32 findings by function name; the two edited declarations are 35/CC 13 and 12/CC 4, both far below `min-complexity: 25`, so the P0-1 repair cannot change the finding set |

The remaining 32 findings are pre-existing functions outside this slice's owned files or outside its
extraction set (patch/scan/handoff helpers, `validateControlCommandIntents` in `control.go`,
`validateGoalSessionIntent`, `validTerminalState`, `compareGoalSessionIdentityStrict`,
`prepareGoalDelivery`, plus 9 test functions: `control_test.go` 1, `goal_delivery_test.go` 2,
`goal_session_test.go` 4, `state_test.go` 1, `supervisor_handoff_test.go` 1), identical by name
before and after.

## 3. Pins first (C4) and the mutation check

No test asserted any of these strings at the base revision, so three table tests were written
against the pristine tree before the extraction and re-run after it (they must stay green — any
reordered condition, merged/split string or changed verdict turns them red):

| test | driver | subtests | literals | not covered |
|---|---|---:|---:|---|
| `TestJournalValidatorFirstErrorStringsArePinned` (`state_test.go`) | `store.mutateJournal` (load → mutate → `saveJournalWithCapacityGuardLocked` `state.go:5317` → `saveJournalLocked` `state.go:5299` → `validateJournal`); `(LinuxCgroupReservation).Validate` called directly because the journal-level surface collapses its inner strings and `ReserveLinuxCgroup` is `linux/amd64`-gated | 64 subtests | 37/43 | 6 proven unreachable (§5) |
| `TestGoalSessionValidatorFirstErrorStringsArePinned` (`goal_session_test.go`) | `store.mutateGoalSession` | 26 subtests | 17/17 | none |
| `TestLinuxCloneValidatorFirstErrorStringsArePinned` (`linux_clone_journal_test.go`, `//go:build linux`) | `store.mutateJournal` on store-built clone chains | 35 subtests | 31/31 | none |

Counts from `go test -count=1 -v -run FirstErrorStringsArePinned ./internal/state/` (PASS lines
below each top-level pin test, group wrappers included — the journal pin's cases live under two
group subtests):

| platform | journal | goal session | clone | failures |
|---|---:|---:|---:|---:|
| Windows (local, foreground) | 64 | 26 | not compiled (`//go:build linux`) | 0 |
| WSL `Ubuntu-24.04` (Linux) | 64 | 26 | 35 | 0 |

The journal row's denominator: the two validators under the journal pin carry 43 distinct
`errors.New` strings (25 journal + 17 reservation + the pinned delegated
`native usage observation is invalid`). 37 are asserted; the six unasserted ones are exactly the
§5 unreachable conditions.


**Mutation check** (C4, "flip the guarded behaviour, red shown"): inside the extracted helper
`validateGoalSessionStates` (`goal_session.go:1688`) the control-attachment operand
`!validGoalSessionUUID(journal.ControlSessionID)` was flipped to
`validGoalSessionUUID(journal.ControlSessionID)`; the pin went red with an assertion-level mismatch
(`goal_session_test.go:2224: mutateGoalSession() error = goal session control attachment is invalid,
want "goal session journal is invalid"`, plus two order-shifted cases), exit 1; after reverting the
flip the pin is green again (26/26). A second flip (`validateGoalSessionSchema`'s version operand)
turned the same pin red through the seeding path, exit 1. Both flips were reverted; the delivered
tree carries the base conditions.

**Apply-stage operand isolation** (from simplify finding 14): the `attached launch without attempted
launch` case also leaves `NativeSessionID` empty, so it exercises the second operand of
`(!journal.LaunchAttempted || journal.NativeSessionID == "")` — deleting the first operand kept all
pre-existing goal-session cases green. One case was added (`LaunchState = Attached`,
`NativeSessionID = "native-session-1"`, `LaunchAttempted = false`, zero `LaunchAttemptedAt`); with
`!journal.LaunchAttempted` deleted it fails alone
(`goal_session_test.go:2224: mutateGoalSession() error = <nil>, want "goal session native handle is
invalid"`, exit 1) while the other 25 cases stay green; with the deletion reverted the pin is green
again (26/26, exit 0).

**Fix-stage operand isolation (review R2).** The review found operands across the decomposed
validators that no test isolated: deleting them kept both lanes green. Fourteen of them now carry a
table case — ten journal-pin cases (`containment_authority_target_pid_mismatch`,
`containment_authority_target_identity_mismatch`, `containment_handoff_with_process_details`,
`cgroup_reservation_with_containment_scans`, `aborted_cgroup_reservation_with_containment_uncertainty`,
`pre-gate_cgroup_receipt_with_containment_uncertainty`, `containment_scan_version`,
`v1_cgroup_release_proof`, `native_usage_terminal_recovery_without_required_flag`,
`pending_event_without_identity`) and four clone-pin reuse cases (`reuse abort after pending
containment` / `cgroup reservation` / `cgroup release proof` / `pre-gate release proof`) — each
carrying exactly one inconsistency on a valid base. Every case was verified with a
delete-the-operand `go test -overlay` run over a scratch copy of `state.go`: each deletion reddens
its single target case (the group wrapper line is the only companion failure) and no other case;
the combined overlay of all fourteen deletions reddens exactly the fourteen new cases. The
seven operands left over are shadowed by earlier checks (§5) and are recorded in `docs/TODO.md`
instead: no case can isolate an operand that can never decide a verdict.

**Clone-cleanup readback oracle (review R1).** The apply-stage shared-builder refactor had changed
`TestLinuxCloneCleanupBeginCompleteRequiresExactReadbacks`'s expected target-removal proof source
from the submitted checkout literal to the journal readback, so a persistence divergence now passed.
The builder keeps the submitted `checkout` on `pinnedLinuxCloneBase` again and the test asserts the
persisted readback equals it (`linux_clone_journal_test.go:719`) before deriving the proof from the
submitted value. Mutation check on WSL: with the builder persisting `checkout.TargetInode+1` (a
scratch test-file overlay), the test reddens alone at the new assertion
(`persisted checkout receipt = …TargetInode:0x30, want the submitted …TargetInode:0x2f`) and the
rest of the Linux suite stays green. The shared builder's fixture numbers differ from the superseded
per-test literals (seed `Size/Device/Inode` 4096/3/5 → 2048/17/31; checkout `TargetDevice/TargetInode`
17/23 → 23/47); the numbers carry no semantic role, they are only distinct from the sibling reuse
and abort fixtures.

## 4. Gate results (exact commands, exit codes)

All from `daemon/` unless stated; `gofmt`/`vet`/builds/tests foreground, re-run 2026-11-21 on the
fix revision (R3 rewritten to C3's call form, the reuse dispatcher restored, R1's readback oracle
restored, R2's fourteen operand cases added) and again after the P0-1 repair (the reuse early
return restored in the `5770afd1` shape; the positive reuse readback regression added).

| gate | command | result |
|---|---|---|
| gofmt | `gofmt -l internal/state/` | no output, exit 0 |
| vet | `go vet ./...` | no output, exit 0 |
| build linux/amd64 | `GOOS=linux GOARCH=amd64 go build ./...` | exit 0 |
| build windows/amd64 | `GOOS=windows GOARCH=amd64 go build ./...` | exit 0 |
| linux vet (compiles `//go:build linux` state tests plus the app consumer) | `GOOS=linux GOARCH=amd64 go vet ./internal/state/... ./internal/app/` | no output, exit 0 |
| package tests (Windows) | `go test -count=1 ./internal/state/...` | `ok github.com/wxxb789/symmetry/daemon/internal/state 9.462s`, exit 0 |
| package tests (Linux, C2) | `go test -count=1 ./internal/state/...` (WSL `Ubuntu-24.04`, `GOPROXY=off` against the host module cache) | delivered bytes `ok … 12.972s`, exit 0; base merge-base `a1142d4e` copy `ok … 12.856s`, exit 0 |
| app lane (review P0-1 consumer, added on review) | `go test -count=1 -timeout 20m -run 'Clone\|Reuse' ./internal/app/` (WSL) | delivered bytes `ok … 7.511s`, exit 0; base copy `ok … 10.594s`, exit 0; the review's failing `TestBindLinuxCloneReuseRequiresStoppedSourceAndStableSession` is green |
| reuse readback regression (P0-1 oracle) | `go test -count=1 -run TestLinuxCloneReuseRetiredJournalsStayLoadable ./internal/state/` (WSL) | pre-fix HEAD `a9fc5fe0` plus the test: exit 1, both subtests fail `run journal has Linux clone evidence without an intent`; delivered bytes: PASS, `ok … 0.248s`, exit 0 |
| Linux suite detail | `go test -count=1 -v ./internal/state/` (WSL) | 242 top-level PASS, 0 FAIL, 324 subtest PASS lines, `ok … 13.758s`, exit 0 |
| Linux tag detail | same run filtered to the 26 top-level tests declared by `linux_clone_journal_test.go` and `linux_cgroup_v2_test.go` | 131 `=== RUN` lines (26 top-level + 105 subtests), all PASS |
| Linux pin detail | `go test -count=1 -v -run FirstErrorStringsArePinned ./internal/state/` (WSL) | 3 pin tests, 125 subtest PASS lines (64 journal + 26 goal session + 35 clone), 0 FAIL, exit 0 |
| Windows pin detail | `go test -count=1 -v -run FirstErrorStringsArePinned ./internal/state/` | 2 pin tests, 90 subtest PASS lines (64 journal + 26 goal session), 0 FAIL, exit 0 |
| early-return audit (P0-1 class) | every `return` of the four base validators enumerated from the merge-base, each `return nil` mapped to HEAD | 10 mid-function `return nil` sites in `validateLinuxCloneJournal` (all mapped, §1a); 1 lost in `0d9ed577` and restored; the other three validators have no mid-function `return nil` |
| observed flake, not reproduced | `TestLinuxCloneCleanupBeginCompleteRequiresExactReadbacks` failed once in an intermediate full `-v` run at `linux_clone_journal_test.go:777` (`daemon does not hold the exclusive State lock`, the `/proc/locks` flock check) | not reproduced on the same bytes: single test `-count=10` exit 0; two further full `-v` runs exit 0; base copy three full `-v` runs exit 0 |
| whitespace | `git diff --check` | exit 0 |
| host-path scan | branch diff and every changed file grepped for the Windows user-profile directory name, the account name, `AppData`, drive-letter paths and the WSL mount of the checkout drive | exit 0; the only hits were this record's and the plan's own text describing the scan, and generic drive-letter fixture strings that carry no account name |
| move proof | `validmove` over both revisions, 4 groups (44-helper grouping, reuse dispatcher restored) | re-run after the P0-1 repair: PASS, exit 0 (§1, §1a) |
| colour-moved | `git diff --color-moved=zebra --color-moved-ws=allow-indentation-change --stat` | exit 0 (§1) |
| pinned ≤40/≤25 | `funclen … -max-lines 40 -max-cyclo 25` | exit 0, 49 matched functions, max span 39, max cyclo 25 (§2) |
| gocyclo gate | `golangci-lint run --config <gocyclo min-complexity 25> ./internal/state/...` | 32 findings, all pre-existing; delta vs base = exactly the four validators removed; re-run after the P0-1 repair: same 32 by function name (§2) |
| operand isolation (R2) | `go test -overlay <scratch state.go with one operand deleted> -count=1 ./internal/state/` per operand (14 mutations) | each deletion reddens exactly its target case and nothing else; the combined overlay reddens exactly the 14 new cases |
| persistence-divergence mutation (R1) | `go test -overlay <scratch test file with the builder persisting checkout.TargetInode+1> -count=1 ./internal/state/` (WSL) | exit 1; only `TestLinuxCloneCleanupBeginCompleteRequiresExactReadbacks` fails, at the new readback assertion |
| race | not run locally (`-race` needs cgo) | CI daemon race job is the oracle (contract) |

## 5. Delivered contract deviations, all deliberate

1. **`validateJournal` carries 12 checks, not one per enumerated block.** The three-line
   replacement-call form `if err := validateX(journal); err != nil { return err }` is what `gofmt`
   accepts (it splits one-line `if` bodies), so a 13-helper chain would make `validateJournal` 42-45
   lines and miss the ≤40-line acceptance. The smallest allocation that satisfies every acceptance
   is 12 checks: `validateJournalKeyScalarsAndProcessDetails` (folds the moved `validateKey` call
   plus the scalar block), the four reserved containment helpers, the two release-proof helpers,
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
   decomposes that region, and each stage is its own helper. The reuse region is the three reuse
   helpers called in order from `validateLinuxCloneReuseBranch`, the dispatcher the slice's first
   form used; the apply-stage simplification inlined it, and the fix stage restored it because
   C3's enumerated call form puts the inline parent at 41 lines. The P0-1 repair keeps the
   dispatcher and gives it the non-nil grant as an argument so the parent can restore the base
   early return (the guard variant fails `validmove`; §1a). The dispatcher is 12 lines / CC 4, the
   parent 35 lines / CC 13, so both stay inside the acceptance.
3. **Unreachable pre-existing branches are moved, not fixed.** Six conditions cannot fire at the base
   revision; they stay byte-identical (move-only) and are recorded as `docs/TODO.md` candidates with
   the reasoning: `v2 cgroup reservation cannot accompany a v1 authority` and `… a v1 handoff`
   (the earlier authority/handoff blocks return first), `Linux cgroup reservation cannot have both
   abort and physical-stop proof`, `Linux cgroup aborted reservation lost producer-exit evidence`,
   `Linux cgroup reservation has effects before creation intent`, and `unresolved input command has
   an acknowledgement`. The pin test's reachability probes confirmed four of the six; the two
   v1/v2 cgroup-accompany conditions rest on the guard-order reasoning recorded above and in
   `docs/TODO.md`, not on a probe.
   Seven further operands inside reachable conditions are shadowed by earlier checks and can never
   decide a verdict: the scans first-check `journal.ContainmentCgroup != nil`, the scans handoff
   check `handoff.SupervisorPID <= 0`, the handoff first-check `journal.ContainmentAuthority != nil`,
   the abort and pre-gate preconditions' `journal.ContainmentAuthority != nil` and
   `journal.ContainmentHandoff != nil` (both blocks). They are recorded in the second `docs/TODO.md`
   entry with the guard-order argument; no pin case can isolate them, because the mutation keeps
   every verdict unchanged.
4. **Latent nil dereference preserved, not repaired, and now verified.** When a journal carries a
   checkout receipt without a seed receipt, `validateLinuxCloneCheckout` dereferences a nil `*seed`
   (`linuxCloneCheckoutMatches(*intent, *seed, *checkout)`, `state.go:5124`) although the earlier
   seed check only fires for `seed != nil`. Verified on WSL 2026-11-21 with a temporary probe test —
   a store journal built by `newPinnedLinuxCloneBase(…, "seed")` with `LinuxCloneCheckoutReceipt`
   set and `LinuxCloneSeedReceipt` cleared reported `PANIC_RECOVERED: runtime error: invalid memory
   address or nil pointer dereference` (probe removed; the file's diffstat returned to the pin
   test's recorded shape). `readJournalEntryLocked` wraps `validateJournal`, so a hand-crafted
   journal file can panic a `ListJournals`/`LoadJournal` caller. Recorded as a `docs/TODO.md`
   candidate with the recipe; changing it changes a validation verdict and is out of a move-only
   slice.

5. **This evidence record is outside the contract's STRICT owned set.** The owned set lists the
   three production files, the six state test files and the two `docs/TODO.md` files; the program
   rules add only the plan file, and this record is neither. The apply stage wrote it, the operator
   committed it, and the fix stage edited the same file (R1/R2/R3/R4–R7/R9 records) — a single
   writer throughout, no functional risk. The conductor should fold the convention into the owned
   set (`docs/evidence/<date>-<slice>-<date>.md` is an expected write target for the implement,
   apply and fix stages) rather than treat each record as an out-of-set write.

## 6. Residual risks and known limits

- **Pins cover the reachable strings, not all branches.** Six conditions are unreachable (four proven
  by probe in the journal pin, two excluded by the same guard-order reasoning); for those the
  extraction's protection is the ordered-literal/subsequence proof (§1), which does cover them.
  Seven operands inside reachable conditions are shadowed by earlier checks (§5) and cannot be
  isolated by any case; they are recorded in `docs/TODO.md` for a revalidate-or-remove pass.
- **The move proof does not prove control flow.** `validmove` and the colour-moved check prove line
  movement, order and literal order only; an early `return nil` that becomes a helper `return nil`
  plus parent fall-through passes both (§1a). P0-1 was exactly that class, caught by the adversarial
  review, not by the gates. The class is now guarded by the positive reuse readback regression
  (`TestLinuxCloneReuseRetiredJournalsStayLoadable`) and the one-time early-return audit of all four
  validators (§1a); the reuse branch is pinned positively, the other branches negatively by the pin
  suites. A future extraction must repeat the audit, and extend the positive readbacks to the
  branch that is being moved.
- **The clone-journal pins compile only on Linux** (`//go:build linux`, existing file tag), so the
  Windows lane runs 90 of the 125 pin subtests (64 journal + 26 goal session); WSL and CI run all
  125.
- **Measurement tooling is scratch, not committed** (precedent rf-w1-05): the span/cyclo tool, the
  move checker, the temporary `golangci-lint` config and the base-revision copies live outside the
  repository. Their inputs (base revision, group lists, thresholds) are recorded above so the run is
  reproducible.
- **`validateJournalLeaseTerminalAndGrantInvariants` sits at cyclo 25**, the acceptance boundary;
  `validateJournalKeyScalarsAndProcessDetails` and `validateContainmentScans` sit at 24. A future
  condition added to any of them crosses the limit, so the next edit to those blocks is a split,
  not an addition.
- **No new `//nolint`/`credo:disable` suppressions** were added; no gate was relaxed or skipped.
  Re-runs happened only after deliberate changes (the P0-1 repair, the reverted mutation checks).
  One intermediate full `-v` run failed `TestLinuxCloneCleanupBeginCompleteRequiresExactReadbacks`
  at the `/proc/locks` flock check (`daemon does not hold the exclusive State lock`); it did not
  reproduce on the same bytes (single test `-count=10`, two further full `-v` runs and three base
  `-v` runs all green — §4). Sections 1a, 2 and 4 were re-derived after the P0-1 repair; the
  earlier revision's numbers are superseded where they differ.
