# rf-w1-06 daemon dead-code sweep (obvious set) — plan

Contract: `goal-rf-w1-06-daemon-deadcode.md` (operator GDD state dir, outside this repo). Baseline for
every coordinate below: `a3039f0d17fc7c7905b63bdf247cac5bf339b213` — the worktree HEAD, the branch
point, and the revision the two strategies were run at; the tree was clean at dispatch.

Binding method: the contract's **Plan-review amendments** section supersedes the Change list's
enumeration. A symbol is deletable only when **both** proofs hold:

- (a) it appears in the `deadcode -test ./cmd/...` findings of **all three** targets —
  `GOOS=windows GOARCH=amd64`, `GOOS=linux GOARCH=amd64`, `GOOS=linux GOARCH=arm64`;
- (b) `git grep -nw <symbol>` over the whole repository (including `*_test.go`, `daemon/e2e/`,
  witness tests and docs citations) finds **no reference** beyond its own definition and doc comment.

## Deliverable shape

1. `daemon/internal/harness/registry.go`: delete `Registry.ValidateRequired` (`:215-222` plus its
   separating blank line; `git diff --numstat` reports `0 9`) — the only symbol inside the
   contract's owned file set that satisfies both proofs.
   Nothing else is deleted; no test is deleted, retargeted or weakened (no test names it).
2. `docs/TODO.md` → `## Dead-code candidates`: record the re-scoped residual with both proofs and a
   reproduction command, so the successor owners (W3-04 probe/registry dedup, W6-08 goal-envelope
   family, and whichever later slice owns `daemon/e2e/**`) inherit the evidence instead of
   re-deriving it. This is the only place the residue has: the program plan and the contract live
   outside the repository.
3. No other file is edited. No `//nolint` (`git grep -n nolint -- daemon` returns nothing at the
   baseline and the diff adds none), no test file, no CI file, no e2e/witness file.

## Measurements at the baseline (two strategies)

Strategy (a), exact commands (deadcode v0.53.0, installed into the Go toolchain bin dir):

```
deadcode -test ./cmd/...                          # GOOS=windows GOARCH=amd64 (host)
GOOS=linux  GOARCH=amd64 deadcode -test ./cmd/...  # from daemon/
GOOS=linux  GOARCH=arm64 deadcode -test ./cmd/...
```

Findings (lines of `file:line:col: unreachable func: name`): **141** windows/amd64, **251**
linux/amd64, **159** linux/arm64. Intersection by `(file, symbol)`: **92** symbols. The linux runs
carry the Linux-only execution/platform surfaces, the windows run the `!linux || !amd64` build-tag
surfaces; only the intersection is eligible.

Strategy (b): for each of the 92, `git grep -nw <name>` over the whole repository, hits classified as
definition (`func … <name>(`), comment (`//` before the name) or call site. **88 of 92 have at least
one call site** (most of them in `*_test.go` of packages outside the `./cmd/...` root set, or in the
excluded families below). The 4 with **no call site anywhere**:

| symbol | definition | in owned set? |
| --- | --- | --- |
| `Registry.ValidateRequired` | `daemon/internal/harness/registry.go:216` | yes → deleted |
| `AttachGoalSession` | `daemon/internal/state/goal_session.go:1044` (doc comment `:1042`) | no → TODO candidate |
| `EnsureGoalSessionLineageAvailable` | `daemon/internal/state/goal_session.go:592` (doc comment `:588`) | no → TODO candidate |
| `FailUnresolvedProvideInput` | `daemon/internal/state/state.go:4173` (`func` `:4175`) | no → TODO candidate |

Both proofs for the deleted symbol, verbatim:

```
$ deadcode -test ./cmd/... | grep ValidateRequired
internal\harness\registry.go:216:27: unreachable func: Registry.ValidateRequired     # windows/amd64
$ GOOS=linux GOARCH=amd64 deadcode -test ./cmd/... | grep ValidateRequired
internal/harness/registry.go:216:27: unreachable func: Registry.ValidateRequired     # linux/amd64
$ GOOS=linux GOARCH=arm64 deadcode -test ./cmd/... | grep ValidateRequired
internal/harness/registry.go:216:27: unreachable func: Registry.ValidateRequired     # linux/arm64
$ git grep -nw -- ValidateRequired
daemon/internal/harness/registry.go:215:// ValidateRequired probes one adapter and checks operation requirements.
daemon/internal/harness/registry.go:216:func (registry *Registry) ValidateRequired(ctx context.Context, kind Kind, required ...Capability) (Capabilities, error) {
```

No interface spelling, reflection string, doc citation or generated binding of the name exists: the
two hits above are the entire corpus, and deleting the body removes both. The live primitives remain
reachable without it — `Registry.Probe` (`registry.go:193`, call sites `internal/app/app.go:1104`,
`:4765`, `internal/harness/harness_test.go:64`) and `Capabilities.Require`
(`internal/harness/types.go:236`, call sites `internal/app/app.go:3623`, `:4394`, `:4408`, `:4413`,
`:4428`, `internal/harness/omo/adapter.go:113`, `harness_test.go:90`) — so no second surface becomes
unreachable and the after-run cannot grow new findings from this deletion.

## Candidate table — every surface the Change list names, with both proofs

Membership column: presence in the `deadcode -test` findings of windows/amd64 · linux/amd64 ·
linux/arm64.

| surface | deadcode membership | `git grep -nw` references | disposition |
| --- | --- | --- | --- |
| `Registry.ValidateRequired` (`harness/registry.go:216`) | yes · yes · yes | definition + doc comment only | **deleted** |
| `NewOperatorClient` (`daemon/internal/control/client.go:485`) | yes · yes · yes | 16: `e2e/core_test.go:696`, `e2e/production_linux_containment_witness_test.go:2046`, `daemon/internal/control/client.go` ×2, `client_review_test.go` ×6, `client_test.go` ×6 | kept (amendment exclusion; e2e/witness callers) |
| `OperatorClient.SubmitTask` `:987`, `.GetTask` `:999`, `.CreateTaskCommand` `:1015`, `.operatorRequest` `:1121`, `.operatorRequestWithStatus` `:1125` | yes · yes · yes | call sites inside `daemon/internal/control/client.go:989`, `:1004`, `:1027` and in the e2e files/tests above | kept (same family) |
| `WithMaxResponseBytes` (`daemon/internal/control/client.go:99`) | yes · yes · yes | 6: `app/pi_provider_bridge_test.go:409` (oversized-response case of the live `NewClient`), `daemon/internal/control/client_test.go:420`, `provider_action_client_test.go:425` | kept (live test seam) |
| `validateTaskResponse` `:209`, `validateTaskCommandRequest` `:281`, `validateTaskCommandResponse` `:302`, `validateTaskWaiting` `:334`, `sameCommandPayload` `:441`, `isTaskState` `:611`, `requiresTaskRun` `:629` (`daemon/internal/control/validation.go`) | yes · yes · yes | call sites `daemon/internal/control/client.go:992`, `:1007`, `:1022`, `:1034` (inside the dead operator client) and `daemon/internal/control/supervisory_validation_test.go:39,43,48,55,107,125,129,148,152` | kept (amendment exclusion; a live sibling shares the file) |
| `Task.UnmarshalJSON/HasField`, `TaskWaiting.UnmarshalJSON/HasField` (`protocol/protocol.go`) | yes · yes · yes | `protocol.Task` 65 references over 8 files (`e2e/core_test.go` 37, `daemon/internal/control/client.go` 9, `e2e/production_linux_containment_witness_test.go` 9, `e2e/production_linux_containment_replay_witness_test.go` 4, `e2e/production_daemon_crash_witness_test.go` 2, `daemon/internal/control/validation.go` 2, two control tests 1 each); `TaskWaiting` 8 (7 in `protocol/protocol.go`, 1 in `validation.go`) | kept (amendment exclusion; e2e/witness decoders) |
| `NewClaudeAdapterWithRunner` (`harness/registry.go:254`) | yes · yes · yes | 5: `app/harness_test.go:306`, `harness/harness_test.go:61`, `harness/claude_probe_test.go:96` (+2 in `registry.go`) | kept (test-referenced seam; W3-04 owns the probe/registry dedup) |
| `launchLinuxV2Process` (`execution/linux_owner_v2_unsupported.go:12`) | **yes** (windows only) · no · no | 4: real `linux && amd64` definition `linux_owner_v2_linux.go`, caller `process_launcher_linux.go:33`, test | kept (linux/arm64 builds against the stub; not an intersection member) |
| `validateAcknowledgementResponse` (`daemon/internal/control/validation.go:318`) | no · no · no | live caller `daemon/internal/control/client.go:980` inside `Client.AcknowledgeCommand` | kept (production-live) |
| root probe paths `probeCodexExecutableWithRunner` (`harness/registry.go:388`), `probeClaudeExecutable` (`:308`) | **no · no · no** | main-reachable through the default adapters (`NewCodexAdapter` `:374` → `NewCodexAdapterWithRunner` `:380` → `probeCodexExecutableWithRunner`; `NewClaudeAdapterWithExecutable` `:248` → `newClaudeAdapterWithRunner` → `probeClaudeExecutable`); plus `harness/codex_probe_test.go` ×5, `harness/claude_probe_test.go` ×12, and two docs citations | kept (neither proof holds; W3-04 owns delegation) |

The remaining functions in the cited `validation.go:209-629` range
(`validateTerminalPayloads` `:265`, `validateTaskCommandResource` `:352`,
`sameJSONObject/Value/Number` `:448/:460/:501`, `validateSnapshotParts` `:507`, `validateCommands`
`:519`, `isSupervisoryCommand` `:544`, `ValidateSupervisoryPayload` `:549`, `isTransitionState`
`:620`) appear in **no** target's findings: they are reached from the live `Client` and supervisory
paths (`internal/app/supervisory.go:29` uses `ValidateSupervisoryPayload`). Untouched.

## Acceptance-target conflict, stated rather than hidden

Clause 1 asks for a `deadcode` finding drop of ~1.3–1.6k. That number comes from the pre-amendment
scope (program plan: "LOC ≈ −1,300…−1,600 + test deletions") and counts surfaces the binding
amendment removes from deletability: F-09's operator client (~650-800 LOC) and its validators
(~420 LOC) plus the v1 decoders, `WithMaxResponseBytes`, the probe seams and the stub — every one of
them has live e2e/witness/test or production references today, so the amendment's own criterion
excludes them and clause 3 makes deleting them a failure ("deleting a surface with a live caller …
= not achieved").

Options, as required by the dispatch rule:

- **A (chosen):** implement the amended criterion exactly; report the measured drop (1 finding per
  target, 9 lines) and hand the residue to its owners with both proofs. The increment stays
  mergeable alone, keeps every test, and no build target regresses.
- **B (rejected):** delete the test-referenced surfaces, retargeting the e2e/witness callers. The e2e
  and witness files are outside the owned set (the contract says so, and review A2 confirms), the
  pi-bridge oversized-response test has no equivalent knob replacement, and clause 3 classifies it as
  not achieved.

Recommendation: A. The measured numbers, not the inherited estimate, are the honest evidence for
this slice; the successors' own estimates stand on their own file sets.

## Evidence plan (all foreground, in this order, one heavy job at a time)

1. `gofmt -l daemon/internal/harness/registry.go` (expect empty).
2. `cd daemon && go vet ./...` (host GOOS), `GOOS=linux go vet ./...`, `GOOS=linux GOARCH=arm64 go vet ./...`.
3. `cd daemon && GOOS=linux go build ./...`, `GOOS=windows go build ./...`,
   `GOOS=linux GOARCH=arm64 go build ./...` (the arm64 build is the one that pins the retained
   `launchLinuxV2Process` stub).
4. `cd daemon && go test -count=1 ./internal/harness/... ./internal/control/... ./internal/execution/... ./internal/protocol/...`
   (touched package plus the contract's named suites; no local `-race` — no cgo on this Windows
   host, CI owns the race oracle).
5. Re-run the three `deadcode -test ./cmd/...` commands; expect 140 / 250 / 158 and no new symbol.
6. `git diff --check`; `git diff --numstat` for the exact line delta.
7. `python scripts/ci/test-check-citation-existence.py` is unaffected (the plan, `docs/TODO.md` are
   not under its audited paths, and no citation of mine lands in `docs/archive/CLOSED-TODO.md`), but
   every citation in this plan was re-read against the baseline revision before commit.

Sequencing: the deadcode re-run is the measurement job; it does not run concurrently with the test
suite (the host kills both for memory).

Outcome on the final tree: `gofmt` clean; `go vet ./...` exit 0 on windows/amd64 and linux/amd64;
the **amended arm64 vet oracle fails pre-existing, outside this slice** —
`GOOS=linux GOARCH=arm64 go vet ./...` reports
`internal/state/linux_clone_journal_test.go:1077:19: reader.verify undefined`, because that test file
is `//go:build linux` while `verify` is defined only under `//go:build linux && amd64`
(`linux_clone_cleanup_reader_linux_amd64.go:154`). Both files are unchanged from the baseline
(`git show a3039f0:…` carries the same call at `:1077` and the same tag), so the failure is not
caused by this deletion; the four packages this slice touches vet with exit 0 on all three targets,
and the defect is recorded in `docs/TODO.md` → `## Build-tag parity (arm64 oracle)`. All three
`go build ./...` targets exit 0; the four named suites pass; the three `deadcode` re-runs report
140 / 250 / 158 with `Registry.ValidateRequired` gone and no new symbol.

## Out of scope (with reason)

- The excluded families of the binding amendment (operator client + methods, `WithMaxResponseBytes`,
  the legacy task validators, the v1 decoders, the stub) — they have live references; their removal
  needs an owner for `daemon/e2e/**` and the app/control test seams.
- Probe/registry dedup and the root probe delegation — W3-04's file set and design
  (`RunBoundedProbe[T]`), deliberately not half-done here.
- The goal-envelope family — W6-08 (decision D5); opencode — W6-03 (D1); ack stages — W3.
- `internal/state` (`AttachGoalSession`, `EnsureGoalSessionLineageAvailable`,
  `FailUnresolvedProvideInput`) — outside the owned file set; recorded in `docs/TODO.md`.
- Any test-file deletion or retargeting: no test file exclusively covers a deleted symbol, so there
  is nothing to retarget (clause: "test files deleted only if they exclusively cover deleted
  symbols").

## Design trade-offs

1. **Delete rather than keep a callerless public wrapper.** `Registry.ValidateRequired` composes
   `Registry.Probe` and `Capabilities.Require`, both live and both with their own call sites; the
   wrapper has zero callers and zero coverage, so it is speculative API surface, not a seam. Keeping
   it would keep it as a deadcode finding on all three targets forever.
2. **No new test.** A deletion has no runtime behavior to pin; a reflection pin on method absence
   would be an implementation-coupled tautology over the source text. The oracles that can see this
   change are the compiler (a caller cannot exist without re-adding the symbol), the three-target
   `deadcode` runs and `git grep`; all three are in the evidence plan, before and after.
3. **No mutation check needed.** None is possible or required: the slice deletes no assertion and
   converts none ("any deleted or converted assertion needs a mutation check" — zero such edits).
4. **Residue in `docs/TODO.md`, not in the plan alone.** The plan is the increment's record; the
   TODO is the repository's tracker and the only in-repo place a later slice looks. The entry names
   each symbol with its file and the reproduction command, so a later reader re-derives the
   coordinates instead of trusting them.

## Residual risks and limits

- `deadcode` reachability is RTA from `./cmd/...`; a caller reachable only through reflection or an
  interface value would be invisible to it. Covered by the second strategy: the whole-repo
  `git grep -nw` finds no other spelling, and Go has no dynamic dispatch by name.
- Windows host: no local `-race` (no cgo) and no Linux-only suite execution; the linux/arm64 and
  linux/amd64 evidence here is compile/analysis-level.
- The amended slice's drop is 1 finding per target. Anything larger requires the successor slices,
  which own `daemon/e2e/**` and the excluded families; this plan states that instead of implying a
  −1.3–1.6k result the evidence does not show.
