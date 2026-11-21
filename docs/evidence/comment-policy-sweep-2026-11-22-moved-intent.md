# comment-policy-sweep — moved module intent (2026-11-22)

Contract: `goal-comment-policy-sweep.md` (GDD, 2026-11-22).
Plan: [`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](../plans/2026-11-22-comment-policy-sweep-plan.md).

The sweep's `M1` rule deletes module/file-level intent from source headers and records it in `docs/`
(owner policy 4). This file is the canonical home for that text, quoted from the workstream handoffs at
the delivered tree; each owning slice plan carries a short "Comment-policy sweep" note naming its files
and pointing here, instead of duplicating the text. Line references are at the base revision
`f1fe1e59` and drift after the sweep.

## Moved module intent (M1), by workstream

### ws1-platform-anchor

## Moved intent (M1 — deleted from source)

- `daemon/internal/platform/anchor_gate.go:39-40` (grant doc) — "The anchor keeps no journal copy and
  makes no scheduling or lease decisions." (ownership boundary; the surviving 2-line compression keeps
  only the secret-material hazard.)
- `daemon/internal/platform/anchor_proc.go:23-24` (`LinuxOwnerAnchorCleanupVerifier` doc) — "The
  implementation is supplied by cmd/symmetry-daemon to avoid a platform→State import cycle."
- `daemon/internal/platform/anchor_run.go:22-24` (`LinuxOwnerAnchorRunKey` doc) — "LinuxOwnerAnchorRunKey
  duplicates only the platform-neutral durable RunKey shape. The platform package cannot import state
  because state consumes this package's cgroup release capability."
- `daemon/internal/platform/helper_v2_protocol.go:46-47` (`protocolFence` alias doc) — "protocolFence is
  a local alias which keeps the v2 IPC's exported evidence independent of the state package while
  retaining the exact durable fence."

Secondary rationale dropped while compressing (no M1 row; flagging so it is not silently lost):

- `anchor_server.go:844-851` — the enumeration of the pinned trusted gate's checks (inherited-FD census,
  four-rights RELEASE allowlist, B-only maps, no-new-privileges, zero caps). The surviving 2 lines keep
  the placement-contract dependency and the future-migration requirement.
- `anchor_wire_shape_test.go:495-519` — the golden capture provenance (pre-refactor commit
  `3ccc62f5618fdd16b38c2c4b7d6421a95f48bdfb`, scratch harness path, `capture-output.txt`). Deleted as
  D2 history; the surviving `linuxOwnerAnchorShapeGoldenFrame` doc keeps the one host-dependent uid
  substitution. No `docs/` record carries the capture pointer.
- `anchor_shape_table_test.go:5-15` — the cross-file note that the writer goldens live in
  `anchor_wire_shape_test.go` (still directly visible from the test) and the "eight former
  `writeResponse*` variants" provenance id.

### ws2a-platform-supervisor

## Moved intent

- `supervisor_linux_helper.go` block 924–950 → 2 lines: dropped detail — the two paths that reach
  `enterRecovery` while the owner is still alive (daemon closes its own control writer on
  response-transport loss via `closeControlAfterResponseLossRequestLocked`; a response write can fail
  before any pipe I/O, e.g. a frame above the 16 KiB limit), and the seal-recording asymmetry
  (restarted-daemon path records the seal before the receipt via `RecordSupervisorRecoveryScanSeal`,
  Completed+1; in-process `recoverResponseLossStop` does not — see debt).
- `supervisor_linux_helper.go:515`: "RunContainmentSupervisor is the hidden production helper
  entrypoint." — deleted as a name-labels-nothing doc line; hidden-mode dispatch lives outside this slice.
- `supervisor_windows_client.go:908-909`: "AttachSuspendedProcessWithSupervisorHandoff is the
  descriptive alias kept for callers that use the full authority terminology." — deleted (provenance only;
  the alias function itself remains).

### ws2b-platform-containment

## Moved intent (rule M1)

- `daemon/internal/platform/clone_aliasing_linux_test.go` (head lines 16–20, file header):
  "The platform package keeps hand-written deep-clone functions for authority and evidence values
  (P-08 in the phase-0 audit). A clone that copies a pointer, slice or map field shallowly makes the
  clone share mutable state with its source, and a field added later without extending the clone
  turns that sharing into an aliasing authority bug no existing test or linter sees."
  The rest of that header (22–46: how the reflection gate proves the invariant, the
  `skipExitProofs` load-bearing rule, the per-build-universe split) was deleted as test narration
  (`D3`); all of it is already recorded in
  `docs/plans/2026-11-21-rf-w2-16-clone-aliasing-test-plan.md` (harness semantics, planted-red 4)
  and `docs/TODO.md` "Deferred review findings (rf-w2-16 clone-aliasing gate)".
- `daemon/internal/platform/containment_errors.go` (head lines 10–12): "This value stays in an
  unconstrained file because execution/runner.go consumes it on every platform; the ErrLinuxOwner*
  mismatch sentinels live in containment_errors_linux.go next to their linux && amd64 producing
  sites." (the pre-program contract paragraph at lines 5–8 was kept byte-identical).
- `daemon/internal/platform/containment_errors_linux.go` (head lines 14–17): "All 78 producing
  sites are in linux && amd64 files, so the values live in this linux && amd64 file rather than in an
  unconstrained one, where they would be exported dead API on windows and darwin. The cross-platform
  ErrLinuxSupervisorResponseLost stays in containment_errors.go."

### ws3a-app-prod

## Moved intent (M1)

- `daemon/internal/app/command_queue.go` — module ownership and budget contract, removed from the `commandQueue`/`commandReceipts` docs (the lock-contract and lock-order lines stayed under K1):
  - "commandQueue owns the daemon's command queue: the arrival FIFO, the dedupe index keyed by command key, and the per-run serialization lanes, together with the acknowledgement-receipt bookkeeping (commandReceipts) that fences late control-plane responses against delivered acknowledgements."
  - "A commandQueue may be used as a zero value: every map it reads is created on first use."
  - "It is a value inside commandQueue rather than more queue methods because it keeps its own ordering: every write to it happens inside a queue call under the caller's locks, but markCommandAcknowledgementDelivered orders the tombstone against a journal write with daemon.commandReceiptMu. … commandReceiptMu is held across a journal write only by the terminal-acknowledgement persistence loop (outbox.go:210-241), which never touches this bookkeeping. One intent per type, every write to the bookkeeping inside this file, and the module stays at the contract's binding budget (at most 12 methods: the 8 queue methods above plus the 4 receipt methods below). Lock contract and zero-value tolerance are commandQueue's."
  - "newCommandQueue returns an empty queue. startBackground constructs one per background lifetime, replacing the fields the daemon used to reset there." (`D2` narration; folded into the zero-value item)

### ws3b-app-tests

## Moved intent (M1)

- `daemon/internal/app/command_queue_test.go` (header block, pre-sweep lines 10–15): "This file exercises the command-queue module (command_queue.go) without a daemon: every invariant the queue and its receipt bookkeeping own is asserted at their own interface. The daemon-level behaviour oracle stays the existing app tests, which drive the same invariants through the executor." The trailing red-mutant/slice-evidence sentence was provenance, not moved.

### ws4-authority

## Moved intent (M1)

1. `daemon/internal/authority/supervisor.go:1-4` (package doc, deleted):
   > Package authority contains the small durable binding used to recover a machine-local process
   > containment boundary. It is deliberately independent of platform and state packages so the
   > journal can store the exact authority without making either package own the other's policy.
2. `daemon/internal/authority/linux_clone.go:116-119` (`LinuxCloneReuseStop` doc, deleted):
   > LinuxCloneReuseStop … is deliberately independent of the state package so the RunJournal can
   > persist the authority without an import cycle.

Also deleted as refactor design narration (`D2`, no relocation) if the owning slice plan wants it:
`supervisor.go:251-256` (`supervisorCore` "not a wire shape … carries the shared cloning, equality,
compare-and-set and conversion policy in one place") and `supervisor.go:283-287` (why `Supervisor.core`
is a struct conversion while the handoff/release-proof extractors stay explicit literals).

### ws5-execution

## Moved intent (M1)

- `gate_lifecycle_linux.go:3-6` — "Package protocol implements the fixed-B transient launcher IPC
  used by the Linux native-run helper." The block's second sentence (callers must durably commit the
  RunJournal handoff and receive the initial lease acknowledgement before the one `Release`) is
  retained, compressed, on `PreparedGate`/`Release`.
- `gate_lifecycle_linux.go:212-219` (Prepare) — deleted remainder: Prepare starts only the
  root-owned static A gate inside the caller's pinned cgroup-v2 leaf; it waits for the root core's
  authenticated group-cleared READY frame, records the exact producer through the mandatory hook,
  then waits for the exact launcher exit and verifies A adopted the exact gate child.
  "No native program runs here."
- `gate_lifecycle_linux.go:663-668` (Release) — deleted remainder: clone commands use the v3
  bootstrap and require A to durably record B's checkout-ready progress claim before native
  execution can begin (the B receipt is not an A-certified checkout — that part is retained,
  compressed, at the `onCheckoutReady` call).
- `linux_owner_v2_linux.go:1561-1566` — "runClosePipeline owns the single order both paths share.
  The gate path claims the release capability before the promotion hop (base :1527-1535) and the
  pre-gate path claims after its clear (base :2184-2197), which claimsCapabilityAfterClear selects."
- `linux_owner_v2_linux.go:1580-1587` (runClosePipeline) — "Every step returns its base error
  unwrapped, exactly as the base close paths returned it, so a failure stops the sequence where the
  base path stopped." (The arrow step list is deleted; the order is pinned by the finalize tests and
  by the exit table in `docs/evidence/rf-w4-05-linux-v2-finalize-2026-11-21.md`.)
- `linux_owner_v2_finalize_pipeline_test.go:21-26` — deleted remainder of the file header: these
  tests execute the real close paths on a hand-built owner; every other guard (issued/valid
  capability checks, receipt reconcile, durable mutation order) runs for real.

### ws6-sanitizer

## Moved intent (M1) — deleted from source, text kept here

Linear-time/invariant design rationale for a matcher suite pinned by the two test modules:

- `_render_passes` docstring (rows 606-627): *"One pass is one rebuild. … `_rebuild_with_groups` then
  writes the union back in one join of slices. Rendering k matches over a text of n characters is
  therefore O(n + k) per pass, where the per-match rebuild was O(k*n); `_rebuild_with_groups` is that
  one rebuild, and the wide-record pin counts its calls."* (`_pass_groups` added: *"This union is the
  per-match growth the first version ran, done once per pass, so a match can still never bind a
  replacement's own text."*)
- `_RAW_SPELLING_ESCAPE` guard (rows 424-436): the raw look runs only when a literal's bytes can
  carry a raw Windows spelling at all — *"a backslash run is odd … and that backslash is followed by
  one of the escapes that hides a location from the decoded reading: `\n`, `\r`, `\t`, `\f` and
  `\b`"* — and the guard is also the cost guard: *"keeps a long non-matching backslash run - `file:`
  plus thousands of separators - from being scanned a second time: the UNC family's doubled-run
  leader is superlinear over one run"*.
- `_CONTAINED_RESUME` (rows 802-812): the scan re-enters a kept match, so the window is *"wide enough
  to expose the starts whose own prefix straddles that end: the widest such prefix is a drive
  location's device namespace … plus the drive letter behind it, ten bytes"*, derived from
  `_DEVICE_PREFIX`'s text so widening the pattern widens the window; *"a narrower window hides a
  second location whose start crossed that far behind the stop"*.
- `_raw_literal_spans` scan (rows 975-985): *"`finditer` would resume at a match's end, so a location
  whose tail swallowed the next location's first bytes was hidden from this render and found by the
  next one - the non-idempotence the union exists to close"*; with the bounded re-entry, *"a listing
  whose lines are one run no longer walks that run once per line (the review's P1: 99 KB in 49 s)"*.

### ws7a-ci-model-routing

## Moved intent

- `scripts/ci/test-model-routing-record-sanitize.py` — module docstring sentence (candidate lines 7–10), deleted:
  "This module owns the raw reading's pattern, quoted, URL, space-continuation and CLI battery; the JSON
  reading's escapes, mapping, token opacity and fail-closed property live in
  ``test-model-routing-record-sanitize-json.py``, which loads this module by path for the fixtures above."
- `scripts/ci/test-model-routing-record-sanitize-json.py` — deleted module docstring (candidate lines 2–9):
  "Regression tests for the record sanitizer's JSON reading. These tests are the JSON half of the
  sanitizer's regression evidence: a record whose document parses as JSON is read through its string
  literals (decoded, matched, and re-spelled), and this module pins that reading's escapes, offsets,
  digests, token opacity and fail-closed property. The raw/prose reading lives in
  ``test-model-routing-record-sanitize.py``, whose fixtures this module shares."
- Both are the two suites' ownership/fixture-sharing boundary (module-intent class, `M1`). The
  performance blocks were compressed in place instead: their rationale survives ("one pass is one
  rebuild / `_rebuild_with_groups`; call count must not grow with matches", "the raw look re-enters only
  a small window behind each kept match"), while the deleted text carried round measurements
  (4.3 s/115 s/41.7 s, 99 KB ≈ 49 s, 12.0M bytes, 26,871→222,871) and mutant tallies.

### ws7b-ci-scripts

## Moved intent (M1) — for the owning slice plan / docs/

1. `assert-browser-suite-coverage.py:4-7` — why the gate exists: "Findings T4 and F4: thirteen
   Playwright specs and three node test files live under `browser/`, and six of the specs plus all
   three node tests are reachable only through local driver scripts - roughly 4,600 lines of test
   code with no CI verdict and no explicit label."
2. `assert-browser-suite-coverage.py:24-28` — ownership boundary: "For a wired file this gate proves
   it is *named*, not that the naming step runs it: for the render family,
   `assert-playwright-json.py`'s executed-set assertion is the execution proof and is deliberately
   not duplicated here. `--root` overrides the repository root so the gate itself can be exercised
   against synthetic fixture trees."
3. `assert-browser-suite-coverage.py:16-17,142-144` (deleted as D1/D6) — the manual-matrix reading
   rule the module docstring stated: only the first cell of a table row in `## Manual-only suite
   matrix` counts, so a row's reason/driver columns may name sibling suites freely.
4. `go-test-durations.py:4-6` — wiring: the daemon job's race pass writes
   `$RUNNER_TEMP/daemon-go-race.json` through `go-test-json-gate.py`; any other capture can be fed
   the same way.
5. `go-test-durations.py:32-34,57-60` — budget/ownership: the thresholds are advisory until refactor
   slice W6-12 flips them to blocking (program plan section 10.5); the exit status is the verdict
   either way and the step that runs the tool owns whether it gates the job; the runner's timings
   swing run to run (why the step is `continue-on-error`); re-deriving the band from more than one
   runner run is W6-12's call.
6. `go-test-durations.py:55-56` (deleted as D2) — history: an earlier host-measured non-race proxy
   landed 19x away from the runner's p95 and could not fire.
7. `omo-native-evidence.py:6-8` — the pinned executable's host path comes from
   `SYMMETRY_OMO_BINARY`; the committed size and digest remain the authority; this is
   native-process evidence, not daemon or Control evidence.
8. `opencode-v2-support-witness.py:6-8` — the blob's host path comes from
   `SYMMETRY_OPENCODE_BINARY`; only its exact size and digest are committed; an unset variable, a
   missing file or a different blob refuses the run.
9. `test-check-lockfile-registry.py:13` — the suite's contract boundary: "the contract is the golden
   assertions, which always run", PyYAML being an extra oracle.
10. `test-check-lockfile-registry.py:131-134` — why the stderr stand-in exists: the suite has to
    assert what a run without PyYAML prints and a process cannot read back its own stderr, so the
    module-level probe writes through the wrapper (the notice still reaches the job log and the
    assertion reads the same bytes).
11. `test-ci-changed-path-classifier.py:65-66` — routing rationale: a coverage run is only worth its
    runner time beside the heavy matrix, so each coverage job is gated `run-heavy`.
12. `test-ci-changed-path-classifier.py:515-516` — why the plane check exists: `check_job_gating`'s
    message names the routing rule, not the plane; binding the three names against every fixture
    diff makes a renamed, regated or deleted coverage job redden there too.
13. `test-ci-changed-path-classifier.py:534-536` — contract reference: the coverage-plane contract's
    failure control names a coverage job without the job-level `continue-on-error: true` as
    not-achieved, so it is pinned rather than left to review.
14. `test-ci-changed-path-classifier.py:552-556` — coverage gap the artifact pins close: with only
    the routing/advisory pins, a deleted upload step, a renamed artifact, a moved report path or a
    dropped command leaves the suite green and only the advisory draft-PR run would notice.
15. `test-ci-changed-path-classifier.py:70-71` (deleted as D1) — `COVERAGE_DAEMON_HEADER` is the
    coverage-daemon gated header as the workflow spells it; both coverage mutation tests anchor on
    it, which is why it is spelled once.
16. `export-linux-v2-redacted-evidence.py:544-553` (the deleted bullets) — the daemon-log allowlist
    producer map they recorded: `stage` ← the `queueFailure` stage literals and call sites
    (`events.go`, `app.go`) and the process-exit retry; `reason` ← the literals reaching
    `stopRecoveredJournal` (lease/ownership reasons, `outbox.go`) and the
    `protocol.ReconcileDecisionKind` decisions; `outcome` ← the acknowledgement outcomes
    (`native_run.go`, `commands.go`); `kind` ← the `newIDWithRetry` kinds (`outbox.go`,
    `supervisory.go`). The non-candidate maintenance rule + `msg`/`state` bullets stay in place, and
    `test-linux-v2-evidence.py::test_daemon_log_allowlists_match_daemon_producers` keeps the sets
    complete, so only the stale citations are lost.

### ws8-control-lib

## Moved intent (M1)

- `control/.credo.exs` (header, lines 1-23): *Credo configuration for the Control
  application, added with the advisory `mix credo --strict` CI step (refactor slice
  W1-01); the step does not gate — the strict issue count is a metric and the count
  recorded on the PR is the ratchet baseline the burn-down slices measure against. The
  check list is `mix credo gen.config` output (Credo 1.7.19) with two contract changes;
  measured 459 strict issues with `checks: %{}` at 1.7.19 vs 403 from a Credo that dropped
  AliasOrder/AliasUsage/PreferImplicitTry/StringSigils/Apply (their findings sum to 67).*
  (The `%{}`-fallback why is retained in-file as a 2-line K3 note above `checks: %{`; the
  two per-check whys are retained beside their entries — see below.)
- `control/mix.exs` (13-17): the step is *the advisory `mix test --cover` step of the
  coverage plane (refactor slice W2-17, `.github/workflows/ci.yml`)*; *coverage floors
  become blocking at W6-10/W6-12, not here, so the report is the artifact and the suite's
  own status is the step's status* (the in-file note keeps only the threshold-0 why).
- `control/lib/symmetry_control/integrations/providers/support.ex` (4-9): module purpose
  and ownership boundary — *Shared plumbing for the HTTP-backed provider implementations.
  Provider-specific URL/body mapping, pagination and error atoms stay in the provider
  modules.* (The dropped provenance line: *Every function below was AST-identical across
  GitHub and AzureDevOps before this module existed (`decoded_path_segments/1` differed by
  whitespace only).*)
- `control/lib/symmetry_control/integrations/provider_access.ex` (1230-1231): ownership
  boundary — *The intent-context lock primitives; other locks in this file keep their own
  queries.*
- `control/lib/symmetry_control/workspaces/work_item_eligibility.ex` (moduledoc, deleted
  prose): dropped nuance — *`SymmetryControl.Workspaces` enforces the verdicts — the
  launch transaction (`launch_work_item/3` and `launch_locked_work_item/2`'s fresh-start
  clause), `cancel_work_item/3` through `ensure_legacy_work_item/1`, and
  `retry_work_item/3` — and `PortalJSON` projects the boolean helpers, so one transition
  policy answers both the command and the read instead of two spellings kept in agreement
  by comments*; also *`cancel_gate/1` reads only the item's own membership columns* (the
  portal member names and the preload/fails-closed hazard are kept in the compressed
  moduledoc).
- `control/lib/symmetry_control_web/portal_json.ex` (407-412): dropped half of the
  sentence — *`can_cancel` is withheld from every Goal-managed item in every state: the
  legacy cancel path refuses that item (`ensure_legacy_work_item/1` applies
  `WorkItemEligibility.cancel_gate/1`), and a Goal is the authority that stops its work.*
- `control/lib/symmetry_control_web/controllers/daemon_controller.ex` (510-516): the
  wire-contract half — *turning this check into a served 400 would change an error
  response outside the slice's byte-identical wire contract* (the in-file note keeps only
  "preserve the pre-merge error shape"; the plan path is in debt below).
- `control/lib/symmetry_control_web/plugs/strict_evidence_json.ex` (17-24): dropped
  nuance — *the plug maps the wrapped `Plug.Parsers.ParseError` back to the protocol
  envelope* (the kept single-line `@doc` keeps "Plug wraps them in ParseError; others
  re-raise unchanged").

### ws9a-control-test-support

## Moved intent (M1) — text removed from source, for the owning slice plans

1. `migration_case.ex` (HEAD 33-34) — "afterwards; a failure while starting the Repo still drops
   the schema, and a failure while pointing the dynamic repo at it still stops the Repo." The
   same guarantee is already recorded in `docs/plans/2026-11-21-elixir-fixture-extraction-plan.md`
   ("two failure-path edits are recorded … so a Repo-start failure still drops the schema … a
   failure while pointing the dynamic repo at the private schema still stops the Repo"), which is
   what licensed the deletion. Row 33 is `compressed` because the surviving `afterwards.` is the
   same line with the clause removed; row 34 is `deleted`.
2. `slowest_formatter.ex` (HEAD 5-13) — the module's whole rationale: CI registers this formatter
   as a second `--formatter` so durations reach the log; `--slowest` is forbidden because
   `ExUnit.configure/1` turns `:slowest` into trace mode, `:max_cases` drops to 1 and every test
   timeout becomes `:infinity`; the formatter reads the same `test_finished` events and leaves
   `ExUnit.CLIFormatter` in place. Recorded in the owning slice plan
   `docs/plans/2026-11-21-rf-w2-01-ci-test-observability-plan.md` ("The duration plane", and the
   probe records), and still stated in `.github/workflows/ci.yml:328-329` (owned by ws11).
3. `served_document.ex` (HEAD 101-106) — `gate_members/3` is the union of the definition's
   declared members (read from the bundle) and the frontend-read list, so a schema-side rename and
   a serving-side rename/omission fail the same "stopped serving" message: the assertion cannot
   stay green while schema, projection and read sites disagree. Recorded in
   `docs/plans/2026-11-21-served-doc-gates-plan.md:263-269`.
4. `served_document.ex` (HEAD 116-120) — `declared_members/2` reads the bundle at test time so a
   rename inside it fails the presence assertion until the projection follows; the bundle
   comparison alone cannot refuse it because the definitions are `additionalProperties: true` and
   an optional member may be absent. Recorded in the same plan (:30-31, :263-269).
5. `test_fixtures.ex` (HEAD 8-12) — the builder contract: knobs that converted call sites set
   differently are explicit options; the defaults are the deterministic fixture values the call
   sites rely on; suite-specific values (suite clock, schema base, fresh daemon/claim id) are
   arguments at the call site, never hidden defaults. Partly in
   `docs/plans/2026-11-21-elixir-fixture-extraction-batch-2-plan.md` ("operator_read's converted
   builders keep their fresh ids", :151-155).
6. `test_fixtures.ex` (HEAD 31-36) — `__using__/1` injects `command_opts/0` (rebuilt on every
   call, so lazily-generated `Ecto.UUID.generate()` / `DateTime.utc_now/0` values stay per-call)
   and `command_current/3,4`; a call's `opts` are merged over the defaults so the caller's `:now`,
   `:rollout_enabled`, `:validation_profiles` win; no `:command_opts` injects nothing. The
   one-source injection decision is in the batch-2 plan ("Design trade-offs"); the per-call
   laziness is only in this removed text.
7. `test_fixtures.ex` (HEAD 64-65) — a fresh id is the deliberate choice at the tests that need
   one (replay and duplicate-claim semantics), so it never arrives by default (paired with the
   pinned `@daemon_instance_id` / `@claim_id` values; HEAD 61-63 provenance/`uuid(10)` citation is
   D2/D5 — the literal no longer appears in the converted suites).

### ws9b-control-test

## Moved intent (M1) — removed from source, for the owning slice plans

Quotes are the load-bearing sentences of the deleted blocks; the TSV marks every line `deleted`/`M1`.
Authoring commits from the inventory's blame attribution:

1. `control/test/symmetry_control/workspaces/work_item_eligibility_test.exs` 14-32
   (`6d21e7346`, #209). "The start/cancel/retry work-item gates as one truth table: every
   combination of the six inputs the three verdicts read, with the verdict, the served projection
   boolean and the work-item refusal each combination answers at the baseline revision
   `c0784952`. The rows are literals — a table computed by the module would be tautological. The
   refusal atom of every row is the work-item answer the commands make at the baseline (the
   differential harness recorded in `docs/plans/2026-11-21-rf-a4-core-work-item-eligibility-plan.md`
   drives the real `launch_work_item/3`, `cancel_work_item/3` and `retry_work_item/3` over the same
   matrix), and the oracle suites pin the command endpoints." (Column legend and the `verdict/1`
   code mapping were dropped as duplicates of `assert_row/1`'s destructuring and `verdict/1`.)

2. `control/test/symmetry_control_web/controllers/portal_integrations_test.exs` 113-124
   (`857e763b8`, #145). "The connections-read gate: this real served document is compared with the
   canonical bundle. Every declared member sits in its definition's `required` array — `Connection`
   12, `ConnectionResource` 17, `ConnectionProvider` 3 — so the bundle comparison refuses a serving
   rename of every member the surface reads and no member needs a hand-written presence pin. The
   resource projection also emits one member the definition does not declare (`connection_id`, from
   `PortalJSON.resource/1` under `connection_resource/1`): no connections surface reads it, and the
   workspace gate pins the same serving member in its `WorkspaceResource` row."

3. `control/test/symmetry_control_web/controllers/portal_resources_test.exs`:
   - 132-134 (`c9ed23b40`, #161). "The delivery precedence matrix's inputs. The provider and manual
     values are distinguishable per member, and the CI provider timestamp differs from the change one
     so a member fed the wrong provider context fails its row."
   - 149-155 (#161). "The read model's evidence entries (`workspaces/read_model.ex`): `source/1`
     contributes source/generation/event_id/recorded_at, the kind's own event members are merged over
     it, and `url`/`state` (pull_request) or `status`/`url`/`summary` (ci, review) exist only when the
     payload carried them. `@delivery_bare_agent` is the map the read model does not write (an entry
     without its kind's required member is never recorded), so it pins the `Map.put_new(field, value)`
     half of the agent branch."
   - 189-216 (`857e763b8` #145, `c6b2c352c` #164, `c07849521` #190). "The optional-but-consumed
     members of `workspace-read` (`contracts/v1/workspace-read.schema.json`): each name is read by
     `frontend/src` feature code and absent from its definition's `required` array, so the bundle
     comparison alone cannot refuse a serving-side rename of it. Every presence assertion in 'the real
     served workspace read carries every member the surface reads' walks these lists *plus* the
     definition's own declared members read from the contract bundle (`declared_members/2`): the bundle
     side makes a schema-side rename of a declared member fail red, and the hand lists keep every
     member a consumed surface reads pinned by name even if a schema edit stops declaring it." (The
     per-list `features/**.tsx:line` read-site citations were dropped as stale-prone.)
   - 258-262 (#145). "The runtime row the workspace gate serves: registered with every
     `RuntimeCapabilities` member the surface reads — `adapter` plus the four booleans — so the
     registered map's pass-through is exercised whole, and advertising the adapter `start` operation
     the item's launch needs a selectable runtime for."
   - 951-958 (#161). "The delivery member is the wire's one place where a provider-owned value, an
     operator's manual value and the attempt's agent evidence compete. Only the provider branch adds
     `provider`/`recorded_at`, only the agent branch carries the evidence map's own members; manual
     and none serve the same five, and the pull request's `status` is overridden after resolution
     (`provider_pull_request_state/2`). This matrix pins the full served map — exact member set and
     values — of every branch for every member, on the projection every delivery-serving read shares
     (`PortalJSON.work_item/2`)."
   - 1356-1365 (#145). "The workspace-read gate: a real served document — never a fixture — is
     validated against the canonical bundle AND every member the surface reads is asserted present.
     The bundle comparison refuses a renamed member only where the schema requires it, so the member
     lists above carry the optional-but-consumed names, the class a serving-side rename would
     otherwise leave green. `WorkspaceWorkItem` is one definition body across `workspace-read`,
     `work-detail-read` and `chat-read`, and `PortalJSON.work_item/2` is the one body that serves it:
     the provider-sourced delivery values, the external row and the waiting packet pinned here are
     that shared row's own shape."
   - 1748-1754 (#161). "The delivery precedence matrix: one row per (member, branch). Each row is the
     inputs the four branches switch on plus the *full* served map the branch must produce, so an
     added, dropped or revalued member fails the row. Rows that reach the same branch through a
     different input spelling are kept separately on purpose: \"\" and an absent value are both 'not
     set' (the empty-string fall-through), and an agent map with and without the member's own field
     pins the `Map.put_new(field, value)` half of that branch."
   - 2020-2024 (#161). "One matrix row's served row: the work item carrying the row's provider and
     manual inputs, with the row's evidence attached to its attempt projection — the shape
     `PortalJSON.activity_entry/1` builds — projected by the same `PortalJSON.work_item/2` the workspace
     read, the work detail read, the chat outcome and the activity feed serve their delivery from."

4. `control/test/symmetry_control_web/controllers/portal_workflow_test.exs`:
   - 20-45 (`857e763b8` #145, `c6b2c352c` #164, `c07849521` #190). "The members of `work-detail-read`
     and `work-timeline-read` the surfaces read and the bundle comparison cannot refuse on its own.
     The work row's names are absent from `WorkspaceWorkItem`'s `required` array, and the waiting
     packet's names are absent from `WorkItemWaitingDecision`'s, so a serving-side rename of one of
     those passes the bundle; the timeline item's five names are required inside each `oneOf` variant
     of `WorkItemTimelineItem`, so the bundle refuses their renames and the pin restates them. The
     presence assertions in 'the real served work detail and timeline carry every member the surfaces
     read' are what fails red for the first two classes, and every assertion also walks the
     definition's own declared members read from the contract bundle (`declared_members/2`), so a
     schema-side rename ... fails red as well." (The per-list `features/**.tsx:line` citations were
     dropped as stale-prone.)
   - 1284-1291 (#145). "The work-detail and work-timeline gates: both reads are served from the same
     durable rows, so one scenario drives them and each pin names its own read. The bundle comparison
     refuses a renamed member only where the schema requires it, so the member lists above carry the
     optional-but-consumed names, the class a serving-side rename would otherwise leave green. The
     scenario syncs its own provider repository and imports a tracker row, so the shared row's nested
     consumed members are served and pinned here too, not only through the workspace gate's scenario
     on the shared `PortalJSON.work_item/2` body."

### ws10-frontend

## Moved intent (M1 — delete from source, record here)

### ws11-infra-misc

## Moved intent (M1 — deleted from source, reported here)

- `.github/workflows/ci.yml` coverage plane (head 872-880): the full ownership/budget rationale —
  "Floors stay advisory here (blocking coverage belongs to W6-10/W6-12), so the uploaded report - not
  the exit status - is what this plane produces" and "the `mix test` step is the Control job's
  canonical suite". Only the 2-line advisory + separate-runner why survives.
- `.github/workflows/ci.yml` ruff-metrics job (head 1836-1842): "No new job is added: the
  changed-path classifier test gates the named jobs' routing, and `ci.yml` has one owner per wave" —
  job-addition/ownership boundary.
- `.github/workflows/ci.yml` race tests (head 1684-1690): "Its named witness is a test the ordinary
  pass above already requires to run and pass, so the race configuration enforces one of the same
  names rather than inventing a new requirement" — witness-selection trade-off; only the `-json`
  coupling survives.
- `.github/workflows/ci.yml` browser-suite inventory (head 1825-1833): the inventory contract itself
  ("every `browser/tests/**/*.spec.mjs` and `browser/scripts/**/*.test.mjs` must be named by a
  workflow file outside its YAML comments ... or listed in the `browser/README.md` manual matrix").
  The enforcing script `scripts/ci/assert-browser-suite-coverage.py` and the README remain the record.
- `browser/tests/readable-fixtures.mjs` module header (head 6-12): "The payload literals live here
  once; each suite's own fixture identity (its keys, names, acceptance descriptions and plan titles)
  stays at its call sites as parameters" — shared-fixture module design choice.

### ws12-daemon-misc

## moved intent (M1; removed from source, per RULES never re-homed here)

- `daemon/e2e/internal/operator/client.go` (HEAD lines 1–7, package doc):
```
// Package operator carries the operator task-control driver for its only
// callers — the end-to-end witnesses under daemon/e2e. Production has no
// reachable caller for the driver, so it ships beside those callers instead of
// staying unreachable in the machine control client. The transport, error and
// decode substrate below is a byte-identical copy of the declarations the
// machine client keeps; the driver's operator-exclusive response validators
// live in validation.go and its v1 task projection in task.go.
```
- `daemon/internal/linuxpreflight/preflight_linux.go` and
  `daemon/internal/linuxpreflight/preflight_unsupported.go` (HEAD lines 3–9, same package doc in both):
```
// Package linuxpreflight verifies a Linux amd64 host installation of the
// Symmetry daemon owner: the installed A/B accounts, the delegated cgroup
// parent, the fixed Git clone/repository ownership, the systemd units, and the
// POSIX ACLs that fence A-private state from the fixed B runtime. Every
// verifier is read-only: none of them launches a process, mutates a journal, or
// certifies a future run, and none of them holds daemon state — callers pass the
// daemon configuration and the trusted root-installed owner configuration.
```
