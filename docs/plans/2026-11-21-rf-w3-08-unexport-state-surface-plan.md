# rf-w3-08-unexport-state-surface plan — unexport 10 unused state types + move 5 test-only app options (DA S8)

Contract: `goal-rf-w3-08-unexport-state-surface.md` (GDD, 2026-11-21) including the conductor
reconciliation (D1/D2, P2-1, P3, D6) and the program-wide "lesson 32" control-flow rule.
Diff base revision: `6d10da97e5fb1815ae168198be7c5a411da97ed1` — the `refactor/rf-w3-07-state-error-causes`
branch tip that this worktree is stacked on (W3-07's `state.go` ownership is therefore serialized
ahead of this slice; W2-09 is merged into that ancestry as PR #165, `52bdffd2`). `origin/main` is
one commit ahead at `d324f601` (a `pnpm/action-setup` dependabot bump) and touches no owned file.
Every `<file>:<line>` below resolves at the diff base revision.

Revisions (2026-11-22): the SHA above and `d324f601` are the drafting-time stacked base and the
then-current `origin/main`; both are unreachable from origin after the #200 rebase, and the slice
was then split onto `origin/main` `0132bd2b` (#203), the split-out W3-07-domain half landing as
PR #215 — the evidence record's §10 and §11 record the rebase, the split and the delivered head's
gates. The rename map, the collision guard and the line
citations below were derived at the drafting-time base; the delivered branch applies the same
word-boundary rename map. The `app.go` constructor block's `:241-259` citation is the one site
`#197` moved before the delivery (the removal itself is unchanged, 0/21 lines).

## Deliverable shape

| file | change |
|---|---|
| `daemon/internal/state/goal_delivery.go` | rename `GoalDeliveryRetirement`, `LateGoalUsageLedger` (identifiers + doc comments) |
| `daemon/internal/state/goal_session.go` | rename `GoalSessionLineageKey`, `GoalSessionLineageState`, `GoalSessionLineageStatus`, `GoalSessionControlAttachmentLineage`, `GoalSessionControlAttachment`, `GoalSessionConsumedStopCertificateHistory`, `GoalSessionControlProjection`, `NotFoundError` and the five `GoalSessionLineageState*` constants |
| `daemon/internal/state/state.go` | rename `NotFoundError` (1 declaration, 1 method, 1 `IsNotFound` body, 4 return sites) + 1 `GoalDeliveryRetirement` field type |
| `daemon/internal/state/goal_session_test.go` | mechanical renames of the 3 type names and 8 constant uses that name them |
| `daemon/internal/app/app.go` | **delete** the 5 option constructors and their doc comments (`:240-260`) |
| `daemon/internal/app/export_test.go` | **new**: the same 5 constructors, unexported, byte-identical closures |
| `daemon/internal/app/app_test.go` | 8 call-site lines: `With*` → `with*` |
| `daemon/internal/app/recovery_isolation_test.go` | 3 call-site lines: `With*` → `with*` |
| `daemon/internal/app/app_unsupported_test.go` | 1 call-site line: `WithEnrollment` → `withEnrollment` |
| `docs/evidence/rf-w3-08-unexport-state-surface-2026-11-21.md` | new: evidence record |

No production `internal/app` behaviour changes: the deleted constructors are exported *helpers*
over the unexported `options` struct; `Options`, `Run`, `WithHTTPClient`, `WithNotificationClient`,
`WithLogWriter` and the witness options stay in `app.go` untouched.

## Rename map (contract Change 1 + conductor P2-1)

| base identifier | base site | new identifier |
|---|---|---|
| `GoalDeliveryRetirement` | `goal_delivery.go:72` | `goalDeliveryRetirement` |
| `LateGoalUsageLedger` | `goal_delivery.go:82` | `lateGoalUsageLedger` |
| `GoalSessionLineageKey` | `goal_session.go:96` | `goalSessionLineageKey` |
| `GoalSessionLineageState` | `goal_session.go:104` | `goalSessionLineageState` |
| `GoalSessionLineageStateFree/Reserved/Launching/Attached/Uncertain` | `goal_session.go:50-54` | `goalSessionLineageStateFree/…` |
| `GoalSessionLineageStatus` | `goal_session.go:110` | `goalSessionLineageStatusValue` |
| `GoalSessionControlAttachmentLineage` | `goal_session.go:231` | `goalSessionControlAttachmentLineage` |
| `GoalSessionControlAttachment` | `goal_session.go:239` | `goalSessionControlAttachmentRecord` |
| `GoalSessionConsumedStopCertificateHistory` | `goal_session.go:265` | `goalSessionConsumedStopCertificateHistory` |
| `GoalSessionControlProjection` | `goal_session.go:308` | `goalSessionControlProjection` |
| `NotFoundError` | `state.go:1137` (contract cited `:1068`; drifted by W3-07, re-derived) | `notFoundError` |

**Collision guard (conductor P2-1, re-derived).** The plain lowercase targets
`goalSessionLineageStatus` and `goalSessionControlAttachment` collide with existing functions
`goalSessionLineageStatus` (`goal_session.go:1416`) and `goalSessionControlAttachment`
(`goal_session.go:1817`; the conductor cited `:1761`, drifted — re-derived). Chosen resolution:
the conductor's first option, `goalSessionLineageStatusValue` / `goalSessionControlAttachmentRecord`.
Renaming the two functions instead would churn 15+ call sites of live logic for a rename-only slice;
it buys nothing and widens the diff. All 15 new identifiers are verified free in `daemon/`
(`git grep -w -c` = 0 for each at base).

## Exact edge-case semantics the contract demands

- **Wire format.** JSON tags and every serialized field set stay byte-identical: no tag line is
  edited, no field is added/removed/reordered, and `encoding/json` ignores the Go type name.
  Struct-tag set extraction at base vs delivered must compare equal (evidence §3). The typed
  constants' *string values* (`"free"` … `"uncertain"`) are wire values and are untouched; only the
  constant identifiers change.
- **Error text.** `notFoundError.Error()` stays `err.Resource + " not found"`; `IsNotFound` stays
  exported and keeps `errors.As(err, &target)` for the renamed target type. This is the one name
  this slice unexports *inside* a public predicate — the predicate, not the type, is the API.
- **Out-of-package usability.** After the rename, external packages may still obtain these values by
  inference (`ledger, err := store.ListLateGoalUsage()` in `internal/app/goal_delivery.go:801`),
  read their exported fields and call exported methods; they may not *name* the types. That is the
  intended compiler-enforced boundary. Zero out-of-package occurrences of all 10 names at base
  (`state.<Type>` and bare/aliased forms), so no caller needs the names: verified by the contract
  and re-verified in evidence §2 before and after.
- **Exported methods returning now-unexported types** (`Store.QueueLateGoalUsage`/`LoadLateGoalUsage`/
  `ListLateGoalUsage`/`MarkLateGoalUsageDelivered`/`RetireLateGoalUsage` →
  `lateGoalUsageLedger`; `Store.CheckGoalSessionLineage` → `goalSessionLineageStatusValue`;
  `GoalSessionJournal.ControlProjection()` → `goalSessionControlProjection`) remain legal Go and
  keep working by inference/field access. The advisory `golangci-lint` set in `daemon/.golangci.yml`
  does not enable `revive`/`golint`'s "exported func returns unexported type" rule, and `unused`
  cannot fire because every type is referenced in-package. Recorded limit: the advisory metric is
  not re-measured on this host (CI-only, advisory, not a gate).
- **No `%T`/reflection coupling.** No `%T` assertion in `internal/app` names one of the 10 types:
  `rg -n "%T" daemon/internal/app` returns 13 hits over 3 files (`harness_test.go` ×10,
  `app_test.go` ×2 — `control.ResponseError` and `terminalTransitionConflictError` — and
  `pi_provider_bridge_peer_linux_test.go` ×1), none on a renamed type; no string literal anywhere
  names the 10 types (`git grep` = 0). Doc-comment first words are renamed with the identifiers so
  `go doc` stays accurate. One mid-sentence prose line (`state.go:1282`, "// returns a typed
  NotFoundError.") is reworded to "// returns a typed not-found error." — comment prose only, the
  single enumerated allowance in the rename-only proof.

## App option move (contract Change 2)

Five constructors move out of `app.go` (`WithStore:241`, `WithControl:244`, `WithEnrollment:247-249`,
`WithWorkspace:252-254`, `WithStartProcess:257-259` plus their doc-comment lines) into a new
`daemon/internal/app/export_test.go` under the names `withStore`, `withControl`, `withEnrollment`,
`withWorkspace`, `withStartProcess` (all verified free). `export_test.go` is the Go-standard,
compiler-enforced home for test-only API: it is compiled only when package `app` is tested, so a
production caller of any of the five fails the build.

- All 12 call sites are in-package test files and are renamed: `app_test.go` ×8
  (`:72,120,287,335,1384,4108,4517,9298`), `recovery_isolation_test.go` ×3 (`:398,497,592`),
  `app_unsupported_test.go` ×1 (`:14`, `WithEnrollment` only).
- The closures themselves are moved byte-for-byte (name substitution aside) — the move-only proof
  compares the 5 declaration texts with whitespace stripped (evidence §4).
- `e2e` and `cmd/symmetry-daemon` never referenced the five (only `WithHTTPClient`,
  `WithNotificationClient`, `WithLogWriter`, the witness registry), so no e2e file is touched.

## Design trade-offs

- **Two names deviate from the plain lowercase form** (`…StatusValue`, `…AttachmentRecord`) because
  Go has one identifier namespace per package; the conductor pre-authorized exactly these. The
  suffix reads as "the status value type" / "the attachment record type", which is accurate.
- **`export_test.go` over an internal-package options struct** (`options` is already unexported;
  the constructors must live in package `app` to reach it). Alternative — deleting the constructors
  and constructing `options` inline at each call site — was rejected: it would rewrite 12 test lines
  into 60+ option-literal lines, i.e. far more churn than the contract's move, for no boundary gain.
- **Uniform mechanical rename, comments included**, so the whole diff is provable as a
  word-boundary name substitution (evidence §3) with one declared prose exception.
- **dLOC −10 is report-only** (conductor D2): the diff stat is recorded, never gated. Expect the
  five-constructor deletion (−25 lines with comments) and additions in `export_test.go` (+~30) to
  roughly cancel; the honest number is the `git diff --stat` one.

## Out of scope (contract clause 4)

Other 27 exported state types; W2-09 validator extraction; W3-07 error wrapping; `app.go`'s
notification/recovery regions; widening any error type's semantics; any e2e file; any
`golangci-lint`/CI-config change; the ledger/contract-file archive (operator merge-stage steps).

## Evidence plan (contract clause 5)

1. `gofmt -l .`, `go vet ./...`, `go build ./...`, `GOOS=windows go build ./...`,
   `GOOS=linux go build ./...`, `GOOS=linux GOARCH=amd64 go vet ./e2e/`,
   `GOOS=darwin GOARCH=arm64 go vet ./internal/app/` — all foreground, records exit codes.
2. `go test -count=1 ./internal/state/...` and (slow oracle) `go test -count=1 ./internal/app/`.
3. `git diff --check`; host-path scan of the changed files.
4. Reference-count table before/after (per name, per file, out-of-package = 0 both sides).
5. Planted-reference pair: one scratch `*_test.go` in `internal/app` naming all 10 types + the 5
   constants compiles at base (`go vet ./internal/app/` exit 0) and fails after (exit != 0, exact
   `undefined: state.<Type>` errors listed), then is deleted.
6. Rename-only proof: base file text with the 15 word-boundary renames (plus the one prose line)
   must equal the delivered file byte-for-byte for all four state files.
7. Move-only proof: the 5 constructor declarations, whitespace-stripped, equal base→`export_test.go`
   modulo the 5 names; `git diff --color-moved` review of `app.go`.
8. Control-flow audit (lesson 32): per moved function and per call site, the base→HEAD
   early-return/fall-through/default/equivalent table (the constructors are single-`return`
   closures; the audit must show that, and that no caller's argument order or error path changed).
9. Wire-shape guard (fix round 1, review STD-06): `TestJournalWireShape`
   (`daemon/internal/state/goal_session_test.go`) pins the exact top-level JSON key sets of
   `RunJournal`, `GoalSessionJournal` and `lateGoalUsageLedger` against literal expected sets and
   reds on a renamed tag (evidence §9).

## Risks and known limits

- The rename touches a package the W3-07 slice also touched; the diff base is W3-07's tip, so a
  rebase onto a modified W3-07 re-derives from the same map (mechanical).
- `-race` is unavailable locally (no cgo on Windows); CI remains the race oracle, unchanged by a
  rename/move-only diff.
- WSL is not mandated: no renamed type appears in a linux-gated state file/test (checked: the only
  test file naming them is `goal_session_test.go`).
- Advisory golangci-lint metric not re-measured (above).
