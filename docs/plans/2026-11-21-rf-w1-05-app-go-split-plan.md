# rf-w1-05-app-go-split plan — move-only split of `daemon/internal/app/app.go` into concern files

Contract: `C:/Users/lhan/.local/state/gdd/contracts/goal-rf-w1-05-app-go-split.md` (GDD, 2026-11-21),
including both binding review amendments (`plan-review-ds-1.md` P2-4/A8, `plan-review-opus-1.md` P2-1
and its "update docs citations" clause). Base revision `a3039f0d17fc7c7905b63bdf247cac5bf339b213`;
all `app.go:<line>` citations below resolve at that revision. The subject file is
`daemon/internal/app/app.go`, 12,048 lines holding 419 production functions, 47 types, 6 const
blocks and 3 var blocks (475 top-level declarations).

## Deliverable shape

One move-only refactor: every top-level declaration of `app.go` is relocated by concern into ten
new same-package siblings; `app.go` keeps the core control loop. No declaration is edited,
renamed, split or merged; the declaration set before and after is identical (475 entries), their
extracted bytes are identical, and the import set at package scope is identical (35 imports).

| file | lines | decls | composition |
|---|---:|---:|---|
| `app.go` | 2239 | 94 | 73 funcs, 18 types, 2 const, 1 var (incl. the import declaration) |
| `native_run.go` | 2417 | 90 | 79 funcs, 9 types, 1 const, 1 var |
| `goal_admission.go` | 1584 | 29 | 27 funcs, 2 types |
| `outbox.go` | 1175 | 52 | 48 funcs, 4 types |
| `commands.go` | 1009 | 42 | 38 funcs, 4 types |
| `recovery_glue.go` | 894 | 35 | 35 funcs |
| `goal_delivery.go` | 892 | 30 | 28 funcs, 2 types |
| `events.go` | 603 | 39 | 31 funcs, 7 types, 1 const |
| `lease.go` | 585 | 23 | 22 funcs, 1 var |
| `cleanup_glue.go` | 463 | 22 | 21 funcs, 1 const |
| `probe.go` | 356 | 19 | 17 funcs, 1 type, 1 const |
| total | 12,217 | 475 | |

`options.go` is named by the contract's owned set and carries **no** declarations in this
increment: the `Options` type, the `options` struct, the `With*` constructors and `Run` are the
core loop's composition surface and stay in `app.go`. Creating an empty file would add a file the
package does not need; the contract lists the permitted sibling names, not mandatory ones. Recorded
here as the one contract option exercised.

## Concern boundaries (the review-facing artifact)

Assignment is mechanical and re-runnable: `classify.py` (scratch, see Evidence) holds an ordered
regex rule list `(pattern -> file)` plus an explicit `OVERRIDES` table for the 50 declarations an
independent reviewer flagged; the splitter applies the map and the verifier proves it. The rule
list and the override table are the readable form of 475 decisions.

- **app.go — core control loop.** `Options`/`Run`, `daemon` struct, `initialize`/`close`, `run`,
  background workers, timers/clock, `heartbeat`/`sync`/`reconcile`/`observeControlClock`,
  `startAssignment`/`startAssigned`, run lifecycle (`markRunning`, `releaseRun`, terminal slot
  release, `beginTerminal`, `isCancelled`/`isStale`/`isTerminal`), terminal reservation and
  cancellation re-read helpers, run interfaces (`ControlAPI`, `Process`, `EnrollmentAPI`, …), the
  Linux v2 admission fences (`acquireLinuxV2LaunchPhase`, `linuxV2LiveOwnerFences`, …) and the
  package-level sentinel/error vars the whole package shares.
- **native_run.go — native session and run mechanics.** `runningRun`/`linuxCloneRun` records,
  native admission/preflight (`requiresLinuxV2NativeOwner`, `prepareLinuxV2NativeOwner`, …),
  terminal-ownership claims (`claimNativeTerminalOwner`, `beginNativeStart`,
  `publishNativeStartResult`, `transferNativeStartTerminal`, …), native cancel settlement,
  native usage accounting + retry and its payload types, `waitForNativeRun`, process details,
  `stopAll`/`terminateProcessBounded`, task-result reason carriers.
- **goal_admission.go — Goal admission/attach/resume/abandon.** `startGoalAdmission`,
  `attachPartialNativeSession`, `attachNativeSession`, `recoverNativeGoalSessionForResume`,
  goal session journal helpers, `blockedNativeSession`, `abandonGoalSession*`, session close.
- **goal_delivery.go — Goal outbox delivery.** `deliverGoalDelivery`, attach/stop receipt
  delivery, late usage flush, `recordStoppedGoalSession`, terminal payload construction.
- **commands.go — command dispatch and receipts.** command queue/lanes, `handleCommand`, snapshot
  scheduling, command acknowledgements, claim retry helpers.
- **lease.go — lease windows and renewal.** `renewLeases*`, local lease deadline bookkeeping,
  lease-loss termination, lease deadline math, the lease sentinel error block.
- **outbox.go — outbox retries and ordinary delivery.** `runOutbox`, `flushAll`, `flushRun`, the
  outbox retry records, terminal-transition queueing and delivery, terminal transition readers.
- **events.go — run events and the output pipeline.** `queueEvent`/`queueRawEvent`/`queueOutput`,
  `agentOutput`, the streaming token redactor, the JSONL parser, redaction constants.
- **cleanup_glue.go — process authority / containment stop glue.** the authority and
  containment-stop callbacks, the stop-receipt barrier markers and their wait/assert helpers,
  `clearPersistedProcessDetails`.
- **recovery_glue.go — restart recovery glue.** `recoverUnresolvedInputIntents`, restart input
  recovery, supervisor handoff stop-receipt persistence, recovered journal stop/cancel,
  retained/resumed goal-session predicates, native usage recovery persistence,
  `settleStaleCancellation`.
- **probe.go — harness probe, runtime registration, input helpers.** `ensureHarnessProbe`,
  `configuredHarnessKind`/`nativeGoalHarnessKind`, runtime-registration metadata and payload
  builders, `initialInput`/`resolveProviderAccess`/`inputRecord`.

Ambiguous placements and the decision taken (recorded as required; no silent guesses):

1. **`Linux v2 plumbing` (382 LOC in the audit's cluster table)** stays in `app.go` rather than a
   `linux_v2.go`: the plumbing is the admission fence set the control loop calls on every
   assignment, and the file counts as core. Splitting it out would create a twelfth concern the
   contract did not name.
2. **`options`/`With*`/`Run`** stay in `app.go` (see above); `options.go` stays uncreated.
3. **Six supervisor-handoff callbacks** stay in `cleanup_glue.go` (store-CAS process-authority
   bridges, installed by `startAssigned`/`startGoalAdmission`) while the two
   `*SupervisorHandoffStopReceipt` persistence helpers sit in `recovery_glue.go` (their callers are
   `recovery.go` and the stop-receipt callback). Reviewer recommended this split as the better of
   the two options; applied.
4. **Claim-retry helpers** (`retry`, `retryWithWait`, `waitForRetry`, `nextRetryDelay`) stay in
   `commands.go` although `nextRetryDelay` is also used by outbox/recovery/native: they are the
   command-claim retry family and app.go would be a strictly weaker home.
5. **`persistStaleAndEnqueueCleanup` and the receipt release helpers** stay in `app.go`: they are
   the lease-loss and run-release durability boundaries exercised by `releaseRun`, not command or
   lease mechanics.
6. **`errorText` and `linuxNativeRuntimeRequiresGoalAdmission`** stay in `app.go` on the reviewer's
   evidence (their callers are the admission path and the run-result waiter).

## Exact semantics the contract demands

- **Move-only.** The splitter extracts each declaration as a byte range that includes its doc
  comment cluster and re-emits those bytes unchanged; the verifier re-parses the source and every
  target and compares each declaration's extracted text one-to-one keyed by
  `kind|receiver|name`, after removing all whitespace. Whitespace-only normalization is required
  and bounded: `gofmt` re-aligns exactly one declaration (`systemTimer.Stop`, a one-line func whose
  inter-column padding is a function of its neighbour), which is a formatting consequence of the
  move, not a content change; every other declaration compares byte-identical without any
  normalization. The strict per-declaration hash in `moves.tsv` is the whitespace-stripped SHA-256
  of the declaration text.
- **Comments.** A declaration's own doc comment moves with it: 91 of the 475 declarations carry a
  parser-attached doc comment at the base revision and all 91 still do in the targets (doc-flag
  comparison base -> targets, 0 mismatches; the byte-range extraction attaches exactly those 91
  clusters), and the comment multiset is identical across base and targets (203 groups / 583
  comment lines, 0 added, 0 removed; the reverse - a doc comment left behind - would have been a
  silent loss). Comments inside a declaration stay inside it. The file-level package doc comment
  (`// Package app owns the daemon control loop.`) stays on `app.go`; siblings carry none, so
  `go doc` reports exactly one package comment.
- **Imports.** Each sibling's import block is pruned from actual identifier usage and re-grouped
  (stdlib, third-party, then the `github.com/wxxb789/symmetry/…` module group) by `goimports`
  run with `-local github.com/wxxb789/symmetry`; the package-level import set across the twelve
  files equals the original 35 imports exactly (no additions, no removals).
- **Build constraints.** `app.go` has no build tag, so no sibling may carry one, and none does.
  The contract's amendment (A8) is satisfied vacuously for this slice and is proved by the four
  build targets below; the platform-tagged siblings belong to W1-04.
- **No exported-symbol change.** No declaration is exported differently: the exported symbol set
  is the same 21 distinct exported names (10 exported functions, incl. `Run` and the `With*`
  constructors; 11 exported types; the `Error`/`Unwrap` methods of exported error types; no
  exported receiver type other than those error types); only their file changed.

## Evidence plan (how merge-only is proved)

1. **Extraction + generation** (scratch tool `appsplit`, standard library only, outside the repo):
   `appsplit scan` dumps every declaration with line range and text; `classify.py` maps them;
   `appsplit split` writes the targets and reports each file's declaration count and size.
2. **Declaration-set comparison** (the contract's judging artifact):
   `appsplit verify <original> <targets…> --moves moves.tsv` re-extracts both sides and reports
   `{"original":475,"targets":475,"mismatch":0,"extra":0}` plus
   `import set: original=35 targets=35 identical=true`. `moves.tsv` is the per-declaration
   `kind / receiver / name / original line / target file / target line / text hash` table.
3. **`git diff --color-moved=dimmed-zebra`** over the working tree, plus the `--numstat` shape
   (old `app.go`: `2 insertions, 9811 deletions`, new files untracked at this stage).
4. **Gates** (all foreground, no `-race` locally — no cgo on this Windows host):
   `gofmt -l ./internal/app/`; `go vet ./...`; `go build ./...` for
   `windows/amd64`, `linux/amd64`, `linux/arm64`, `darwin/arm64`; `go test -count=1 ./internal/app/...`;
   `git diff --check`; `python3 scripts/ci/check-citation-existence.py` over the changed documents.
5. **Evidence record** `docs/evidence/rf-w1-05-app-go-split-2026-11-21.md`: exact commands, exit
   codes, counts and the residual limits.

## Design trade-offs

- **Mechanical, re-runnable split rather than hand edits.** A 12 kLOC move by hand cannot be proved
  move-only; the scratch generator plus declaration-set comparison makes the proof a command, and
  the same command re-runs after any rebase (`main` moves daily in this program). Cost: a scratch
  tool that is not part of the repository, recorded in the plan and evidence record with its
  inputs (`map.json`, `classify.py`, both whole artifacts recoverable from the evidence record's
  reproduction section).
- **Concern map reviewed by a second model.** An independent read-only reviewer (475 declarations,
  callers cross-checked) returned 40 misfiled and 28 ambiguous entries; 50 corrections were applied
  and 4 recommendations were declined for the reasons above. The overrides are listed by name in
  `classify.py` so a later reviewer can re-check them without re-reading the bundles.
- **Whitespace normalization in the comparison** (documented above) instead of forcing the
  generator to reproduce `gofmt`'s neighbour-dependent alignment. The alternative would require
  re-implementing the go/printer aligner; the chosen proof still fails on any token-level change,
  which is the contract's "logic edit" boundary.
- **Doc-comment honesty for line citations**: no new `app.go:<line>` citation is added to any
  document; existing citations in the live runbook/plan/scripts corpus are repointed, historical
  goal records and evidence documents keep the revision they were written against (a citation in a
  dated evidence record describes that revision by design).

## Out of scope

- `startGoalAdmission` phase extraction (F2, scheduled as W4-01); callback-factory dedup
  (F4/W2-10); errcheck triage (F3/W3-09); any production behaviour change; any test edit; any
  `//nolint` addition.
- `docs/goals/archive/**` and `docs/evidence/**` historical documents: not repointed (see above).
- Creating `options.go` with no declarations (see Deliverable shape).
