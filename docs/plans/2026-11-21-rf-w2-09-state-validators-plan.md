# rf-w2-09-state-validators plan — per-section validator helpers (move-only)

Contract: `goal-rf-w2-09-state-validators.md` (GDD, 2026-11-21) including the conductor
decision round 2 (C1–C7). Base revision `12a94c79a0057ac762c0b4f4e7a0af5f9b37abcb` (= `origin/main`
at dispatch, i.e. the `#148` `rf-w1-08-daemon-test-enablers` merge; C1's dispatch gate is met by the
branch point itself). All `<file>:<line>` citations below resolve at that revision.

## Deliverable shape

One move-only refactor inside `daemon/internal/state`: the four top-level validators named by the
contract are decomposed into per-section helpers called in the same order, each condition and each
`errors.New` string byte-identical, so no caller observes a different first error.

| validator | base lines | base span | base gocyclo | helpers after | after span | after gocyclo |
|---|---|---:|---:|---|---:|---:|
| `validateJournal` (`state.go:5817`) | 5817–6018 | 202 | 171 | 11 | 39 | 13 |
| `validateLinuxCloneJournal` (`state.go:4881`) | 4881–5053 | 173 | 140 | 15 | 35 | 13 |
| `validateGoalSessionJournal` (`goal_session.go:1647`) | 1647–1719 | 73 | 70 | 8 | 30 | 10 |
| `(LinuxCgroupReservation).Validate` (`state.go:144`) | 144–212 | 69 | 78 | 9 | 30 | 10 |

Measured values (span / gocyclo) of the delivered tree; the before/after table with every helper is
in `docs/evidence/rf-w2-09-state-validators-2026-11-21.md` §2.

Base values are reproduced independently of `golangci-lint` by the scratch span/CC tool (C6's
values: 202/171, 173/140, 73/70, 69/78) and match `golangci-lint run` 2.15.0 `gocyclo`
(`min-complexity: 25`) output at the base revision.

## Public/API shape

No exported symbol is added, removed, renamed or moved between packages. Every new helper is an
unexported package-level function (validators over `RunJournal` / `GoalSessionJournal`) or an
unexported method on the exported value type `LinuxCgroupReservation` — matching the file's
existing idiom for value predicates (`ContainmentScanJournal.clean()`, `RunJournal.Fence()`).
Signatures take the journal (and, where the block already narrowed it, the already-derived
pointer/value) and return `error` only:

- journal helpers: `func validateX(journal RunJournal) error`; stage helpers additionally take the
  already-derived evidence they use (`intent *authority.LinuxCloneIntent`,
  `seed *authority.LinuxCloneSeedReceipt`, `reuse *authority.LinuxCloneReuseGrant`,
  `cleanupIntent authority.LinuxCloneCleanupIntent`).
- value helpers: `func (value LinuxCgroupReservation) validateX() error`.
- goal-session helpers: `func validateGoalSessionX(journal GoalSessionJournal) error`.

Callers of the four validators are unchanged (`SaveJournal`, `saveJournalLocked`,
`readJournalEntryLocked`, `mutateJournal`, the goal-session save/mutate seams, and the
`LinuxCgroupReservation` mutation methods). No new package, file, interface, or error value.

## Exact semantics the contract demands

1. **Order.** Every condition of the base revision keeps its position relative to every other
   condition, including across the new helper boundaries: a journal violating several checks still
   reports the same first error. The only new statements are helper headers, braces, call lines
   (`if err := validateX(...); err != nil { return err }` and `return validateX(...)`), and the
   `return nil` that already terminated an extracted stage.
2. **Strings.** Byte-identical `errors.New` literals, including the intentionally terse repeated
   `"run journal is invalid"` and the two distinct `"run journal containment scan …"` strings.
3. **Guarded blocks keep their original call site.** `if journal.ContainmentAuthority != nil {…}`,
   the abort branch guard, `if checkout == nil {…}`, `if promotionIntent == nil {…}`,
   `if journal.LinuxCloneCleanupIntent == nil {…}` and the `if reuse := …; reuse != nil {…}` reuse
   branch all keep their original position; for four of them the condition line itself moved into
   the helper invoked there (`validateContainmentAuthority`, `validateLinuxCloneCheckout`,
   `validateLinuxClonePromotion`, `validateLinuxCloneCleanup`), while the reuse and abort guard
   lines stay in the parent validator. The "no evidence without an intent" / "git ref promotion has
   no checkout receipt" style probes still run at their original position.
4. **Unreachable branches are not "fixed" here.** Two conditions in `validateContainmentCgroup`
   (`v2 cgroup reservation cannot accompany a v1 authority` / `… a v1 handoff`) cannot fire: the
   authority/handoff blocks return `v1 … cannot use a v2 cgroup reservation` first whenever a v1
   owner is combined with a cgroup reservation, so the later check is dead. They are moved
   byte-identically (no behavior change) and recorded as a dead-code candidate in `docs/TODO.md`
   instead of being deleted in a move-only slice.
5. **Scope of the acceptance numbers.** "Top-level validators ≤40 lines / CC ≤25 each" is enforced
   on the four validators and every helper this slice extracts. It is *not* a package-wide
   threshold: `validateControlCommandIntents` (`control.go`, unowned file, 70 L/42 CC),
   `validateGoalSessionIntent` (`goal_session.go:1612`, 34 L/38 CC), `validTerminalState`
   (`state.go:6068`, 35 L/33 CC) and 30 other functions in `./internal/state/...` are already over
   the threshold at the base revision (36 `gocyclo` findings), lie outside the contract's own
   extraction set (C5) and outside the owned files. The gate therefore asserts (a) every sliced
   function ≤40 L / ≤25 CC and (b) the set of package-wide `gocyclo` findings is identical before and
   after except for the four validators.

## Extraction inventory (same order, same first errors)

Reserved names from the contract are used for their blocks: `validateContainmentAuthority`,
`validateContainmentHandoff`, `validateContainmentCgroup`, `validateContainmentScans`,
`validateLinuxCloneNonReuseBranch`. The last one names a *region*, not a base declaration: the base
`validateLinuxCloneJournal` has no such function, C5 resolves the region into the per-stage helpers
below, and the reuse guard in `validateLinuxCloneJournal` calls `validateLinuxCloneReuseGrant`,
`validateLinuxCloneReuseAbortState` and `validateLinuxCloneReuseRetainedTarget` directly.

`validateJournal` (12 checks, span 39, CC 13):
`validateJournalKeyScalarsAndProcessDetails` (the moved `validateKey` call plus scalar fields, PID
details ×2, containment uncertainty) · `validateContainmentAuthority` · `validateContainmentHandoff` ·
`validateContainmentCgroup` · `validateContainmentScans` · `validateContainmentCgroupReleaseProof` ·
`validateContainmentCgroupPreGateReleaseProof` ·
`validateJournalLeaseTerminalAndGrantInvariants` (lease/work, terminal state, native-usage recovery
marker, dropped output, claim-grant-free) · `validateLinuxCloneJournal` (existing call) ·
`validatePendingJournalEntriesAndDeliveries` (pending events, transitions, attempted-transition
markers, command acknowledgements, and the moved `validateGoalDeliveries` call) ·
`validateJournalNativeUsageObservation` · `validateJournalInputAndCommandIntents` (input-command
intent plus the moved `validateControlCommandIntents` and `validateProviderActionIntents` calls).

Why 12 checks and not one call per enumerated block: `gofmt` expands one-line `if` bodies, so each
replacement call costs three lines; an 18-check chain would make `validateJournal` 45-57 lines and
miss the ≤40-line acceptance that the same contract clause pins. The allocation above is the
smallest one that keeps every reserved name, keeps the two pre-gate/normal release proofs separate
(they measure CC 14/13; together they were 26, over the limit), and keeps every extracted block in
order with its own first-error string. The alternative — a `[]func(RunJournal) error` table — was
rejected: it would replace moved call lines with constructed table entries outside C3's
allowed-new-line set, and it would allocate in the journal-save hot path.

`validateLinuxCloneJournal`: reuse branch · intent-absent · intent (+ fence subject) · seed receipt ·
abort (shared checks + pending branch + proof branch) · checkout · promotion (without-intent
sub-branch + intent/receipt) · cleanup (absent sub-branch + intent + pending + proof). The reuse
branch became three helpers after measurement: the block measured CC 29 as one function, so it is
now `validateLinuxCloneReuseGrant` + `validateLinuxCloneReuseAbortState` +
`validateLinuxCloneReuseRetainedTarget`, called in that order from the reuse guard in the parent.

`validateGoalSessionJournal`: schema · session/launch state scalars · launch-attempt metadata ·
launch outcome (attached/uncertain/closed/recovery/reason/native-handle) · control attachment ·
stop-receipt history · resume state · stop receipt, in the names
`validateGoalSessionSchema`/`…States`/`…LaunchAttempt`/`…LaunchOutcome`/`…ControlAttachment`/
`…StopReceiptHistory`/`…ResumeState`/`…StopReceipt`. `journal.Intent()`'s
`validateGoalSessionIntent` call and its `return err` stay inline (its own CC is a separate finding,
out of this slice).

`(LinuxCgroupReservation).Validate`: base identity · leaf identity · helper identity · launcher ·
gate · helper exit proof · mirror aliases · proofs (abort + pre-gate match, both-proof conflict,
producer-exit evidence) · creation order, as unexported methods on the value type
(`validateBaseIdentity`, `validateLeafIdentity`, `validateHelperIdentity`, `validateLauncher`,
`validateGate`, `validateHelperExitProof`, `validateMirrorAliases`, `validateProofs`,
`validateCreationOrder`), matching the file's existing unexported value predicates
(`ContainmentScanJournal.clean()`).

## Edge cases that force the split

- `validateContainmentCgroupReleaseProofs` measured CC 26 as one helper, one over the limit, so it
  became `validateContainmentCgroupReleaseProof` (14) and
  `validateContainmentCgroupPreGateReleaseProof` (13) in the same order.
- `validateContainmentScans` (24), `validateJournalKeyScalarsAndProcessDetails` (24) and
  `validateJournalLeaseTerminalAndGrantInvariants` (25) sit at or beside the limit: the checker
  asserts the measured values rather than assuming them, and the evidence record flags them as
  split-before-extend.
- The inline reuse region measured CC 29, so it decomposes into the three reuse helpers named above.
- The clone promotion/cleanup stages each contain two `return nil`-terminated sub-branches; each
  becomes its own helper, so no helper carries both stages' CC.

## Out of scope (contract §4)

Error-cause wrapping and the capacity split (W3-07); unexporting the state surface (W3-08);
`app.go` regions; `validGoalSessionUUID` (C7 — recorded in `docs/TODO.md` for W2-12a); fixing the
unreachable cgroup branches (recorded, not fixed); reducing `validateControlCommandIntents` /
`validateGoalSessionIntent` / `validTerminalState` (other owners/waves).

## Evidence plan

1. **Pins first (C4).** Table tests driving each extracted block to its first error, asserting the
   exact string, run green on the pristine tree before extraction:
   - `state_test.go`: `validateJournal` blocks + `(LinuxCgroupReservation).Validate` blocks
     (reservation driven through the exported `Validate` because the journal-level surface collapses
     the inner string to `run journal v2 cgroup reservation is invalid` and `ReserveLinuxCgroup` is
     `linux/amd64`-gated; every other case is driven through `store.mutateJournal` → `SaveJournal`).
   - `goal_session_test.go`: `validateGoalSessionJournal` blocks through `store.mutateGoalSession`
     (the production persist seam; `mutateGoalSession` → `validateGoalSessionJournal`).
   - `linux_clone_journal_test.go` (`//go:build linux`, C2's Linux lane): clone-journal blocks, deep
     stages reusing that file's existing clone fixtures.
   - Mutation check after extraction: one flipped condition inside an extracted helper shows the pin
     red, then reverted.
2. **Move-only proof (C3).** Scratch tool over both revisions: base line multiset ⊆ delivered line
   multiset per validator group; new-only lines restricted to helper headers/braces/call
   lines/`return nil`/`return err`; ordered `errors.New` string sequence identical; group flattened
   by call graph must contain the base line order as a subsequence. Plus
   `git diff --color-moved=zebra --color-moved-ws=allow-indentation-change` for review.
3. **Measurement (C6).** `golangci-lint run` 2.15.0 `gocyclo.min-complexity: 25` over
   `./internal/state/...` (base offender list vs delivered offender list) and the scratch
   span/CC tool for the ≤40-line assertion on every sliced function.
4. **Gates.** `gofmt -l`; `go vet ./...`; `GOOS=linux go build ./...`; `GOOS=windows go build ./...`;
   `go test -count=1 ./internal/state/...`; `GOOS=linux GOARCH=amd64 go vet ./internal/state/`;
   the same test suite on WSL `Ubuntu-24.04` (C2) — never concurrently with the measurement tool;
   `git diff --check`; host-path scan (`git diff origin/main...HEAD | grep -i "users/"`).
5. **Record.** `docs/evidence/rf-w2-09-state-validators-2026-11-21.md` + move manifest, before/after
   table and raw gate output (the manifest is the per-function span/CC table in §2, 47 functions);
   `docs/TODO.md` entries for `validGoalSessionUUID` (C7), the six unreachable conditions and the
   verified nil-deref panic.

Delivered outcome of this plan (all five steps ran; see the evidence record for exit codes and
counts): move proof PASS on all four groups, pinned ≤40-line/≤25-CC gate exit 0 over the 47 sliced
functions, `gocyclo` offender set −4 and unchanged otherwise, Windows and WSL state suites green,
mutation check red-then-green.

## Trade-offs and options considered

- **Direct-value pins for the reservation (chosen)** vs. driving it through `ReserveLinuxCgroup`
  (rejected: platform-gated, and the journal path masks the string). The exported `Validate` is the
  production contract for these strings; the test names the driver in a comment.
- **`linux_clone_journal_test.go` for deep clone pins (chosen)** vs. building cgroup-v2 authority and
  release-proof fixtures in an untagged file (rejected: duplicates linux-only builders
  `bindStateLinuxCgroupV2Handoff` / `testStateLinuxCgroupV2Receipt` / `testStateLinuxCgroupV2ReleaseDTO`
  and would need a new file, outside the owned set). Consequence: those pins run on the Linux lane
  (WSL + CI) and are not compiled into the Windows suite.
- **`(bool, error)` reuse-branch helper (rejected)** vs. keeping the base guard line in the parent and
  calling the three reuse helpers from that guard's body (chosen): the chosen form leaves the
  `if reuse := journal.LinuxCloneReuseGrant; reuse != nil {` line byte-identical instead of inventing
  a new guard condition, which C3 treats as a move violation.
- **One helper for both release proofs** was the first choice; measurement showed CC 26 (>25), so the
  delivered form is two helpers (14 and 13) in the same order.
- **A `[]func(RunJournal) error` call chain (rejected)** — see "Extraction inventory": it would fit
  13+ checks in 40 lines but replaces moved call lines with constructed entries that C3's
  allowed-new-line set does not cover, and it allocates per journal save.

## Risks

- Pin fixtures are order-sensitive by construction: if a fixture also violates an *earlier* block the
  case silently pins the earlier string. Every case is therefore built from a base journal that
  passes the earlier blocks, and the mutation check (evidence plan §1) re-proves the pin's
  sensitivity.
- `linux_clone_journal_test.go` pins need the Linux lane to execute; a Windows-only local run cannot
  show them.
- The delivered spans/CC are asserted by tooling outside the repository (precedent: rf-w1-05's
  splitter); the numbers and commands are recorded in the evidence file.
