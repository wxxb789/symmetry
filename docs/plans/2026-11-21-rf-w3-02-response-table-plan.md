# rf-w3-02 response-table plan — one anchor response builder + one request-shape table (DP S-4)

Goal authority (external to this repository, not evidence): the GDD goal contract
`goal-rf-w3-02-response-table.md` (2026-11-21) plus its conductor decisions (D1, D2, D6, P0-1,
P1-1/P1-2, P2-1–P2-3, P3, and the lesson-32 control-flow audit rule). Every code fact below is
restated with an in-repo coordinate; counts are `grep` reproductions from this tree.

Baseline: `origin/main` = HEAD = `3ccc62f5618fdd16b38c2c4b7d6421a95f48bdfb` (#160, W2-11, merged —
D6's dispatch gate). Branch `refactor/rf-w3-02-response-table`. Area:
`daemon/internal/platform` plus this plan.

## Owned set (re-derived at the dispatch head)

- `daemon/internal/platform/anchor_server.go` — the 8-variant writer block
  (`writeResponse` :1719, and the seven `writeResponseWith*` variants :1723-1843) and
  `serveConnection`'s shape gates/dispatch (:1020-1513) plus `serveCloneCleanup` (:1524-1716) and
  `validLinuxOwnerAnchor{ChildOrigin,Gate,StopLeaf}Request` (:1528-1554).
- `daemon/internal/platform/anchor_wire.go` — request/response structs (:23-71), `matches` (:72),
  codecs (:79-140); the new request-shape table lives here (wire-shape home).
- `daemon/internal/platform/anchor_types.go` — the 13 op constants (:27-39); the three literal
  operations become constants.
- `daemon/internal/platform/linux_owner_anchor_linux.go` — the anchor client: builders
  (:303-852), `exchange` (:853-1014).
- `daemon/internal/platform/linux_owner_helper_v2_linux.go` — anchor-client retire paths
  (`RetireHelper`/`RetirePreGateHelper` :1001/:1020 via `exchange`; `retireClearedRunWithProofs`
  :1084-1159 and `RetireAbortedHelper` :1161-1203, the two inline encode/read/verify blocks).
- New tests: `anchor_wire_shape_test.go` (`linux && amd64`, no skips) and
  `anchor_shape_table_test.go` (`linux && amd64`).
- This plan file.

Owned but deliberately unmodified, with the reason (P2-2 narrowed the change to anchor-only):
`helper_v2_stop.go`, `helper_v2_server.go`, `helper_v2_protocol.go` already carry the unified
helper-v2 shape this slice mirrors — `baseResponse` (`helper_v2_stop.go:291`) + one
`helper`-receiver `writeResponse(connection, operation, status, …, fds ...int)`
(`helper_v2_stop.go:299`) + `writeLinuxOwnerHelperV2{Request,Response}` codecs
(`helper_v2_protocol.go:424/469`) whose `len(fds) != 0` rights rule is the precedent this slice
follows. The helper-v2 rejections stay exactly as they are (P2-2), including
`serveOwnerConnection`/`handleRequest`'s per-op `len(rights) != 0` clauses
(`helper_v2_server.go:479-560`, `:587-647`), so those files need no edit; the 30 plain
`anchor.writeResponse` call sites in `helper_v2_recovery.go` are mechanical call-site updates to
the unified anchor writer only.

Oracles only (not edited): `linux_owner_anchor_linux_test.go` (37 tests, 2,245 L),
`linux_owner_helper_v2_linux_test.go` (12 tests, 705 L).

## Public / API shape

One writer, one identity stamper (mirroring the helper-v2 owner-side shape):

```go
func (anchor *linuxOwnerAnchor) baseResponse(request linuxOwnerAnchorRequest, status string) linuxOwnerAnchorResponse
func (anchor *linuxOwnerAnchor) writeResponse(connection *net.UnixConn, response linuxOwnerAnchorResponse, fds ...int)
```

- `baseResponse` stamps `Version`, `Operation`, `Status`, `RunKey`, `Fence`, `LaunchToken`,
  `Anchor`, `Parent`, `OwnerUID`, `ManagerPath`, exactly the fields every base variant stamped,
  plus the clone-cleanup key override under the base condition
  (`Operation == remove-b-clone-after-receipt && request.Cleanup != nil` → `RunKey = request.RunKey`,
  `FenceDigest = request.FenceDigest`, `LaunchToken = request.Cleanup.NormalReleaseProof.LaunchToken`),
  which today lives in `writeResponseWithCloneRemoval` and applies to every clone-cleanup response
  because every clone-cleanup response is written by that variant.
- `writeResponse` marshals, bounds-checks `linuxOwnerAnchorMaxFrame`, and passes
  `syscall.UnixRights(fds...)` only when descriptors are passed — the same rights rule as
  `writeLinuxOwnerHelperV2Response` (`helper_v2_protocol.go:469-483`).
- The 8 variants are deleted; every one of the 101 call sites builds the response value explicitly
  (`response := anchor.baseResponse(request, status)`, optional field assignments, then one
  `writeResponse` call). Field sets per former variant are unchanged and pinned by goldens.
- New named constants (`anchor_types.go`) for the three literal operations:
  `linuxOwnerAnchorOperationIdentity` (`"identity"`), `linuxOwnerAnchorOperationRegisterProcess`
  (`"register-process"`), `linuxOwnerAnchorOperationWaitReap` (`"wait-reap"`); all 8 literal uses
  (server `:1056/:1057/:1061/:1117/:1126/:1302/:1402`, client `:237/:252/:593/:723/:931`) become
  constants so the table keys are exhaustive.

### The request-shape table (`anchor_wire.go`, anchor-only per P2-2)

Columns: `operation`, `required`/`forbidden` request-field sets, `serverRequiredFDs`
(+ `serverRequiredFDsWithTarget` for the clone-cleanup variable count), `clientSendFDs`
(+ `clientSendFDsWithTarget`), `clientCheckedFDs`, `clientFDsMismatch`, `responseFDs`,
`timeoutClass`, `stoppingResponse`, and a `shape` predicate for the relational rules presence sets
cannot express. Field presence is defined once (`linuxOwnerAnchorRequestField` +
`func (request linuxOwnerAnchorRequest) fieldPresent(linuxOwnerAnchorRequestField) bool`), the
generic gates are `func (request linuxOwnerAnchorRequest) exclusiveFieldRejected(row) bool` and
`func (row linuxOwnerAnchorOperationShape) refuses(request, descriptors int) bool`.
`linuxOwnerAnchorOperationShapeFor(operation)` returns the row; an absent row replaces the base
`switch` `default: return`.

Rows are derived 1:1 from the base checks (this is the control-flow audit's source):

| operation | required (presence) | forbidden (presence) | server FDs | client FDs | client checks | response FDs | timeout | stopping answer |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `identity` | — | grant, helper, creator, process, role, release/pre-gate/abort proofs, run key, fence, clone grant/cleanup/target-fd, gate/user-ns, stop fence | 0 | 0 | no | 0 | request | yes |
| `start-helper-v2` | grant | helper, creator, process, role, gate/user-ns, stop fence, clone fields | 2 | 2 | no | 3 | request | yes |
| `attach-helper-v2` | grant, helper | creator, process, role, gate/user-ns, stop fence, clone fields | 0 | 0 | no | 3 | request | no |
| `lookup-helper-v2` | grant | helper, creator, process, role, gate/user-ns, stop fence, clone fields | 0 | 0 | no | 3 (`attached`) / 2 (`helper_exited`) | wait | no |
| `register-process` | grant, helper, creator, process, role | gate/user-ns, stop fence, clone fields; role ∉ {launcher, native, descendant} | 1 | 1 | no | 0 | request | yes |
| `classify-run-child-origin-v2` | grant, helper, process | role, creator, gate/user-ns, stop fence, proofs, run key, fence, fence digest, after-pid, clone fields | 1 | 1 | yes | 0 | wait | yes |
| `register-gate-v2` | grant, helper, gate, user-ns | creator, process, role, stop fence, proofs, run key, fence, fence digest, after-pid, clone fields | 1 | 1 | no | 0 | request | yes |
| `authorize-gate-release-v2` | grant, helper, gate, user-ns | as register-gate | 0 | 0 | no | 0 | request | yes |
| `stop-leaf-v2` | grant, helper, stop fence | creator, process, role, gate/user-ns, proofs, run key, fence, fence digest, after-pid, clone fields; fence must validate for grant+helper | 0 | 0 | yes | 0 | wait | yes |
| `wait-reap` | grant, helper, process, role | creator, gate/user-ns, stop fence, clone fields; role must be a child role | 0 | 0 | no | 0 | wait | no |
| `wait-reap-run-v2` | grant, helper | creator, process, role, gate/user-ns, stop fence, clone fields; after-pid ≥ 0 | 0 | 0 | no | 0 | wait | no |
| `wait-helper-v2` | grant, helper | creator, process, role, gate/user-ns, stop fence, clone fields | 0 | 0 | no | 0 | wait | no |
| `retire-helper-v2` | grant, helper | creator, process, role, abort proof, gate/user-ns, stop fence, clone fields; exactly one of release/pre-gate proof | 0 | 0 | no | 0 | request | no |
| `retire-cleared-run-v2` | helper, run key, fence | grant, abort proof, gate/user-ns, stop fence, clone fields; exactly one of release/pre-gate proof; `Fence.Generation == RunKey.Generation`, valid run id, helper valid | 0 | 0 | no | 0 | wait | no |
| `retire-aborted-helper-v2` | helper, run key, fence, abort proof | grant, release/pre-gate proof, gate/user-ns, stop fence, clone fields; `Fence.Generation == RunKey.Generation`, valid run id, helper valid | 0 | 0 | no | 0 | wait | no |
| `remove-b-clone-after-receipt` | clone grant, cleanup, run key | grant, helper, creator, process, role, proofs, gate/user-ns, stop fence, after-pid, fence; prior-removal proof required iff no target fd, forbidden with one; run key = cleanup intent, fence digest = cleanup digest, target-fd ⇒ 2 descriptors else 1 | 1/2 | 1/2 | yes | 0 | cleanup | no |

The duplicate cap (P2-1) is uniform and lives table-adjacent as
`linuxOwnerAnchorRequestFDCap = 3` (sized by `readLinuxOwnerAnchorRequest`'s 4-descriptor ancillary
buffer); the table test asserts it dominates every row's descriptor counts, so the column is one
constant plus a consistency check instead of 16 identical values.

### Consumers (one source, two sides)

- Server `serveConnection`: `ShapeFor` replaces the `switch` `default`; `exclusiveFieldRejected`
  replaces the three global gates (`:1038-1050`); the stopping gate (`:1053-1058`) is now
  `row.stoppingResponse`; `refuses(request, len(receivedFDs))` replaces every per-op presence and
  descriptor-count clause (case bodies, the retire block `:1078-1097`, and the three
  `validLinuxOwnerAnchor*Request` helpers, which are deleted as dead weight — zero test references,
  `grep` verified); the timeout chains at `:1123-1131` and `:1081` become `row.timeoutClass`.
- Client `exchange`: the three existing descriptor checks collapse to one table-driven site
  (`row.clientCheckedFDs` + `clientSendFDs[WithTarget]` + the row's verbatim error text; checked
  rows = exactly `remove-b-clone-after-receipt`, `classify-run-child-origin-v2`, `stop-leaf-v2`, so
  the client keeps exactly its three rejections); the 5-branch timeout chain becomes
  `row.timeoutClass`; the two retire paths in `linux_owner_helper_v2_linux.go` keep their bespoke
  verification but share one dial/authenticate/encode/send/read helper and take their timeout from
  the row.

## Exact edge-case semantics

1. **Byte identity (D1).** No response field set, JSON tag, `omitempty`, or status changes. The 8
   goldens plus the override and zero-valued cases are captured from the base variants before the
   refactor and re-pinned through `baseResponse` after it; `git diff` of the response struct and
   `matches` is empty.
2. **Two-phase gate ordering.** Base checks three field groups before the stopping gate (clone
   fields for non-clone ops `:1040-1047`; gate fields for non-gate ops `:1048-1050`; stop fence for
   non-stop-leaf ops `:1051-1053`) and everything else after it. The refactor preserves exactly that
   split: phase 1 = `exclusiveFieldRejected` over those six fields before the stopping answer,
   phase 2 = `refuses` after it. This keeps the base observables that a single hoisted gate would
   break — e.g. a stopping anchor answers `anchor_stopping` for a shape-clean `identity` request but
   refuses a clone-field-carrying one silently. Reorders *among rejecting gates* are unobservable
   (no frame is written on either path) and are recorded in the control-flow audit.
3. **Descriptor guard.** Base `writeResponseWithFDs`/`WithRunPins` refused to write when
   `len(fds) == 0`; the unified writer (contract signature `fds ...int`) cannot distinguish "no
   pins" from "empty pins", so its rule becomes the repo's existing helper-v2 rule: attach
   `SCM_RIGHTS` only when descriptors are passed, never a descriptor-less ancillary message. Every
   production pin-carrying call site (`started`, `attached` ×2, `helper_exited`) constructs its
   descriptors immediately before the call and passes them unconditionally, so no reachable path can
   write a pin-bearing status without its pins; clause 3's "sends FDs when the old variant refused
   to" stays false. Reported as the one intentional semantic delta.
4. **Loose fields stay loose.** Base tolerates unconstrained optional fields on several operations
   (e.g. proofs on `start-helper-v2`, after-pid on `wait-reap`). Clause 3 forbids any per-operation
   accept/reject change, so the rows reproduce the base tolerance exactly instead of tightening it;
   the table test pins the tolerated cases as accepted.
5. **Failure control.** No response field set, status, per-op accept/reject set, FD count, or
   timeout class changes; `responseFDs` for `lookup-helper-v2` is status-dependent by construction;
   clients and servers read the same row for FD counts and timeouts.

## Tests

`anchor_wire_shape_test.go` (unprivileged `unixpacket` socketpair against an in-package
`linuxOwnerAnchor`; written to compile against both the base and the head, and committed green on
the pre-refactor head):

1. Per-operation descriptor-count rejection: a request with one descriptor too many (or too few)
   produces no frame for all 16 operations.
2. Exact response bytes for every reachable status: `anchor_stopping` for the 7 rows that answer it,
   with 0 `SCM_RIGHTS`; the two-phase ordering case (clone-carrying `identity` + stopping ⇒ silence).
3. `SCM_RIGHTS` counts of every frame it receives: 0 for all reachable statuses, plus the
   descriptor-carrying writer cases (3 pins / 2 pins) and byte-identity of the frame with and
   without descriptors, which live in `anchor_shape_table_test.go` because they need the
   head-only writer signature.
4. Unreachable statuses (anything requiring a real A/B cgroup topology, a spawned helper, or a
   privileged peer) are listed as unpinned with the reason, per the contract's escape hatch; the
   code paths stay verbatim.

`anchor_shape_table_test.go` (Linux, head):

1. Per-operation accept/reject: for each of the 16 rows and hard-coded expectations (never read from
   the table), the table-consistent request is accepted while each forbidden field present, each
   required field absent, each wrong descriptor count, and each relational violation is rejected.
2. Locally tolerated fields are accepted (edge-case semantics 4).
2b. Each row's declared required and forbidden field names are pinned as strings
   (`TestLinuxOwnerAnchorShapeTableFieldSets`), because the generated rejection cases can only
   test the fields a row still declares: without this pin a row that silently drops a forbidden
   field would widen the accept set and stay green.
3. Eight golden byte strings (one per former variant, every stamped field non-zero), the
   clone-cleanup key-override case, and the zero-valued `identity` case; each golden is captured
   from the base tree and re-pinned through `baseResponse`.
4. `linuxOwnerAnchorRequestFDCap` dominates every row's descriptor counts.

Mutation matrix (WSL, each reverted): (a) one table row mutated (`register-gate-v2` FD 1→0,
`wait-reap` FD 0→1, `stop-leaf-v2` timeout wait→request) ⇒ table test red; (b) one stamping field
removed from `baseResponse` ⇒ the affected goldens red; (c) one JSON tag renamed ⇒ golden and wire
tests red.

## Evidence plan

All foreground, final tree, counts and exit codes recorded:

| Gate | Command | Where |
| --- | --- | --- |
| format | `gofmt -l daemon/internal/platform` | host |
| vet | `go vet ./...` | host (daemon/, windows/amd64) |
| vet (owned files) | `GOOS=linux GOARCH=amd64 go vet ./internal/platform/` | host |
| build | `GOOS=windows go build ./...`, `GOOS=linux GOARCH=arm64 go build ./...` | host |
| Linux package suite | `go test -count=1 -v ./internal/platform/` (records the `--- SKIP` list; 0 skips required in the new wire test) | WSL `Ubuntu-24.04` user `accept` |
| writer inventory | `grep -c "func (anchor \*linuxOwnerAnchor) writeResponse"` = 1 and the 7 variants gone | this tree |
| operation inventory | 16 rows; three literal ops now constants; `grep -rn '"identity"\|"register-process"\|"wait-reap"'` = 0 in owned files | this tree |
| control-flow audit | base-vs-head mapping of every early return/fall-through/default in the touched paths | evidence file |
| whitespace | `git diff --check` | host |
| host-path leak | profile-directory grep over `git diff origin/main...HEAD` and both new files | host |

CI-only oracles stay the daemon race job and the dispatch-only `linux-native-v2-acceptance`
workflow. Sequencing rule honoured: the measurement harness (the WSL suite) is never run
concurrently with another suite on this host.

## Trade-offs and known limits

- **bdry guard delta** (edge-case semantics 3): the sole intentional semantic difference; it
  prevents a descriptor-less `SCM_RIGHTS` and follows the helper-v2 writer, but the base's
  "refuse to write at all" behaviour on an empty pin set is not reproducible with the contract's
  variadic signature and cannot be reached from production call sites.
- **Reachability of the base's post-gate paths**: the unprivileged harness cannot pass
  `verifyLinuxOwnerAnchorManagerMembership` + the peer/manager overlap rule + real parent pins, so
  response bytes for statuses like `started`/`attached`/`retired` cannot be produced end to end
  (they need a real A/B topology and a spawned helper). Those variants are pinned at the writer
  level (goldens + `SCM_RIGHTS` counts) and listed as unpinned end to end.
- **`responseFDs` for `lookup-helper-v2`** is status-dependent (`attached` 3, `helper_exited` 2);
  the column carries the `attached` count and the test hard-codes both.
- **Descriptor-cap column**: uniform per-request value (3), realised as a table-adjacent constant
  plus a dominance assertion (P2-1's intent) instead of 16 duplicate row fields.
- **P2-2 scope**: helper-v2 rejections and the already-unified helper-v2 builder stay untouched, so
  the "one writer" property is anchor-scoped; `helper_v2_stop.go`/`helper_v2_server.go`/
  `helper_v2_protocol.go` are owned-but-unmodified with the reason above.
- **`dLOC` is report-only (D2)**; the explicit per-site response values add lines, so the net
  reduction is expected to land near the review's ≈−150…−200, reported, not gated.

## Out of scope

Supervisor frame family (`containment_supervisor_*.go`, `linux_supervisor_protocol_linux.go`),
`tcp_peer*.go`, session objects / `serveConnection` decomposition (W4-02), typed error sentinels
(W2-11, merged), helper-v2 handler decomposition, `linux-native-v2-acceptance` script changes, and
any change to the two existing protocol test files.

## Handoff to later slices

W4-02 (`anchorConnection` per-operation handlers) can lift each `switch` case into a handler that
receives the row from `linuxOwnerAnchorOperationShapeFor`; the table is the per-op contract it must
preserve. The control-flow audit in the evidence file is the base it must re-derive against.
