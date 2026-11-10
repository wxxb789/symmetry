# Go surfaces test-ablation inventory

Date: 2026-09-29. Inventory owner: senpi task `st_01a0ed71`.

## Scope, method, and baseline

This report covers every daemon Go test/support surface except `daemon/internal/app`,
`daemon/internal/state`, and `daemon/internal/workspace`. It is source inventory and
candidate discovery only. No source, test, fixture, Git/index, service, credential,
or provider state was changed. No test suite was run: the parent already owns the
known-green Linux ordinary/race results (3,984 pass events and 95 skip events in
each) and final Windows App result, and requested no duplicate suite.

The source snapshot is the dirty working tree based on HEAD
`d14debbf6ae3ded09e5013ef90952d347c01474d`. Many in-scope files were already
modified or untracked; this report does not treat dirty status as evidence of
redundancy. Counts are static top-level `func Test...` declaration counts. Table
subtests are guarded behavior but do not inflate the declaration count. There are
no in-scope `Example...` or `Benchmark...` declarations.

Category meanings: **unit** is an in-process implementation check; **harness** is
an adapter/runner/protocol-harness check (usually with fakes or captured frames);
**smoke** starts a real executable or narrow real transport; **validation** checks
schema/config/input rejection or canonicalization; **verification** crosses a
process, OS, native, control-plane, or production boundary. `Retain` means no
source-grounded deletion is currently justified. `Candidate` means an experiment
is defined below; it is not authorization to remove anything.

## Reconciliation and exhaustive counts

The parent manifest lists exactly the same **114 Go surfaces test files** found in
this assigned scope. It has no test-file omission or false positive here. Those
files declare **1,104 top-level tests**. Two files are intentionally test-only
platform shims and declare zero tests:
`internal/execution/probe_process_exists_unix_test.go` and
`internal/execution/probe_process_exists_windows_test.go`.

| Area | Files | Top-level tests |
|---|---:|---:|
| `daemon/cmd` | 4 | 41 |
| `daemon/e2e` | 11 | 49 |
| `daemon/internal/authority` | 3 | 22 |
| `daemon/internal/config` | 3 | 26 |
| `daemon/internal/contracts` | 1 | 16 |
| `daemon/internal/control` | 7 | 94 |
| `daemon/internal/execution` | 16 | 113 |
| `daemon/internal/harness` | 34 | 382 |
| `daemon/internal/notification` | 1 | 10 |
| `daemon/internal/platform` | 20 | 283 |
| `daemon/internal/protocol` | 11 | 52 |
| `daemon/native` | 3 | 16 |
| **Total** | **114** | **1,104** |

The manifest's 232 additional candidates omit **54 daemon-local paths** relevant
to this scope: 46 harness `testdata` files, two protocol malformed-JSON fixtures,
one embedded contract schema bundle, one daemon config fixture, two E2E helper
entrypoints, and two native C/launcher sources. This is a manifest omission for
responsibility classification, not 54 missing tests. Of these, 18 are
machine-consumed fixtures, two are helper entrypoints, 31 are
README/version/native-capture/schema provenance records with no Go test consumer,
one is an unconsumed config fixture, and two are production native sources.
Details follow the test-file inventory.

## Per-file inventory

Entrypoint is `cd daemon && go test <package>` unless an opt-in environment or OS
is stated. Ordinary `go test ./...` includes portable files and files selected by
the host build tags. Native/provider/control prerequisites are explicit rather
than interpreted as redundancy.

### Commands and E2E

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `cmd/symmetry-daemon/main_test.go` | 4 | unit, validation | Linux-v2 witness opt-in/argument rejection; state-dir canonical resolution, traversal/symlink rejection, and no creation/output on error | `go test ./cmd/symmetry-daemon`; retain command boundary |
| `cmd/symmetry-fake-agent/autonomous_test.go` | 8 | harness | autonomous settings; queued control ordering; pause; guidance/decision artifacts; replay and invalid decisions; artifact/output failures | `go test ./cmd/symmetry-fake-agent`; retain shared E2E fixture behavior |
| `cmd/symmetry-fake-agent/main_test.go` | 22 | harness, validation | subprocess modes, JSON scalar/array/integer fidelity, provider capability, mode precedence, evidence/history, fail-once persistence, input framing, cancellation and child PID | `go test ./cmd/symmetry-fake-agent`; retain executable fixture contract |
| `cmd/symmetry-linux-launcher/main_test.go` | 7 | verification | pinned cleanup walker, hardlink/symlink/exchange/cross-mount attacks, installed cleanup caller and lock serialization | Linux/amd64 package test with installed-host scratch prerequisites; retain native boundary |
| `e2e/core_test.go` | 14 | verification | core daemon workflows, JSON/input fidelity, polling fallback, worktree cleanup, terminal retry, stale epochs, daemon/control restart, real-agent smoke, redaction and fail-closed identity | `SYMMETRY_E2E=1` plus control DB/service; `TestRealCodingAgentSmoke` additionally needs configured real agent; retain |
| `e2e/production_daemon_crash_witness_test.go` | 3 | verification | production daemon crash, committed-response loss/reconnect, selective transition proxy, credential redaction | private control fixture and production witness env; retain crash/HTTP boundary |
| `e2e/production_linux_containment_negative_witness_test.go` | 3 | verification | Linux production negative containment and literal PID-reuse namespace/victim helpers | Linux, control service, namespace/process privileges; retain |
| `e2e/production_linux_containment_replay_witness_test.go` | 1 | verification | real daemon/helper stop/release response loss, receipt replay, helper-death mirror proof and durable clear fences | Linux; `SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_REPLAY_WITNESS=1`, `SYMMETRY_E2E=1`, control and installed helper; retain |
| `e2e/production_linux_containment_witness_test.go` | 3 | verification | production containment spine, unlocated submitted owner rejection, journal credential redaction | Linux production witness/control/helper; retain |
| `e2e/production_linux_v2_cleanup_witness_test.go` | 1 | verification | authenticated native v2 clone cleanup | Linux/amd64 provisioned native gate/control claims; retain |
| `e2e/production_linux_v2_helper_exit_witness_test.go` | 1 | verification | helper exits after release but before native registration | Linux/amd64 provisioned native gate; retain lifecycle boundary |
| `e2e/production_linux_v2_native_witness_test.go` | 18 | verification, smoke | real Pi provider/OpenCode health; helper/daemon SIGKILL; WSL2 denial; stage/ack/helper-response loss matrices; stale owner/journal/migration/PID-reuse/persistence/mount/host refusal; hostile hook entries; v1 replay | Linux/amd64, installed owner/gate, cgroup v2, control and optional native providers; retain all distinct cases |
| `e2e/production_linux_v2_owner_cli_test.go` | 2 | verification | prior journal requires physical release; PID-reuse exchange atomic publication | Linux/amd64 native owner CLI fixture; retain |
| `e2e/production_linux_v2_rootless_compose_witness_test.go` | 1 | verification | rootless-compose native work-item refusal | Linux/amd64 rootless container runtime plus provisioned control; blocked where unavailable, retain |
| `e2e/production_pre_authority_crash_witness_test.go` | 2 | verification | Windows pre-authority crash matrix and wrong-secret first-client race | Windows production witness and control fixture; retain |

### Authority, config, contracts, and control

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `internal/authority/authority_linux_clone_test.go` | 7 | unit, validation | clone authority DTOs, promotion subject, malformed/aliased evidence, legacy read-only target, RunKey namespace, abort and cleanup proofs | `go test ./internal/authority`; retain authority invariants |
| `internal/authority/authority_test.go` | 10 | unit, validation | supervisor handoff conversion/partial fields, release/abort proofs, Linux-vs-Windows owner constraints, propagated binding, stop receipt and creator-session presence | same; retain |
| `internal/authority/authority_v2_test.go` | 5 | unit, validation | legacy JSON shape, v2 leaf/actor proofs, incomplete physical proof, v1/v2 disjointness, reservation abort | same; retain |
| `internal/config/config_harness_test.go` | 8 | validation | legacy/native adapter metadata, Codex model/provider, unavailable metadata, worktree/resource identity, unknown/partial fields | `go test ./internal/config`; retain config boundary |
| `internal/config/config_test.go` | 17 | validation | valid config; Pi share; Linux provider requirement; timeout normalization; unknown/local/control bindings; HTTP opt-in; capacity/workspace/clone-v2/platform constraints; required/unknown fields | same; retain |
| `internal/config/supervisory_test.go` | 1 | validation | supervisory control requires cooperative wire | same; retain |
| `internal/contracts/schema_test.go` | 16 | validation, verification | all goal schemas compile; nullable money, batches/receipts, strict/trailing/duplicate JSON, numeric and UTF-8/surrogate canonicality, commit path and independent schema documents | `go test ./internal/contracts`; retain generated-schema boundary |
| `internal/control/client_review_test.go` | 35 | unit, validation | claim/enroll/session/task/command HTTP trust, lease floor, tokens/IDs/redirects, explicit nulls, precise JSON numbers, fencing, no-content, retry classification and snapshot payloads | `go test ./internal/control`; retain client boundary |
| `internal/control/client_test.go` | 13 | unit, validation | protocol routes, typed errors, cancellation/no mutation retry, API prefix, configuration, task routes/fences/capabilities/input serialization and retry history | same; retain |
| `internal/control/goal_client_test.go` | 26 | unit, validation | goal machine endpoints/fences, evidence batches, receipts/conflicts, session attach/fetch/stopped, usage, strict context/hash/change target, raw schema and typed HTTP errors | same; retain |
| `internal/control/goal_receipt_e2e_test.go` | 1 | verification | real loopback control attach/stop lost-ack and recovery phases, immutable receipts, replay/conflict and private-output redaction | `SYMMETRY_GOAL_RECEIPT_E2E=1` and absolute private input JSON/control service; retain real server boundary |
| `internal/control/provider_action_client_test.go` | 13 | unit, validation | provider-token path, outcomes/replay, typed/additive fields, aliases, invalid grant/path/body, definite/ambiguous errors, redaction, redirect/cancel single attempt | package test; retain |
| `internal/control/supervisory_validation_test.go` | 5 | validation | supervisory payloads/resources, rejected history, generation, paused run/task | package test; retain |
| `internal/control/validation_test.go` | 1 | validation | control-local goal commit path counts UTF-8 runes and rejects invalid UTF-8 | package test; retain independent validator (candidate comparison C4) |

### Execution

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `internal/execution/containment_receipt_test.go` | 3 | unit | receipt persisted before release; retry after wait without restop; durable-handle retry ordering | `go test ./internal/execution`; retain |
| `internal/execution/identity_linux_test.go` | 1 | verification | runner exposes Linux creation identity | Linux package test; retain OS identity boundary |
| `internal/execution/identity_windows_test.go` | 1 | verification | runner exposes Windows creation identity | Windows package test; retain |
| `internal/execution/linux_owner_v2_test.go` | 24 | unit, verification | required-v2 no fallback; admission/grant/lease/release gates; exact readback/mirror/handoff; mutation loss; clone abort/stop/drain/promotion/cancel/finalizer deadlines | Linux/amd64 package test; some helper processes/native paths; retain |
| `internal/execution/linux_owner_v2_unsupported_test.go` | 1 | validation | explicit rejection on unsupported OS/arch | non-Linux or non-amd64 package test; retain platform branch |
| `internal/execution/probe_process_exists_unix_test.go` | 0 | harness support | test-binary implementation maps probe to `unixProcessExists` | compiled only by tests on non-Windows; retain test shim |
| `internal/execution/probe_process_exists_windows_test.go` | 0 | harness support | test-binary implementation maps probe to `windowsProcessExists` | compiled only by tests on Windows; retain test shim |
| `internal/execution/probe_test.go` | 8 | unit, verification | bounded command output/stdin, cancellation/deadline, descendant pipe holder, late success, no durable callbacks; helper subprocess | package test; retain shared process primitive |
| `internal/execution/runner_lifecycle_test.go` | 21 | unit | launcher context, retained ownership/markers/partial containment, authority/handoff and resume/lease barriers, finalize retries, soft/hard termination errors | package test; retain |
| `internal/execution/runner_output_truncation_test.go` | 8 | unit | truncation under silent/delivered/late/queued/cancelled/natural-drain/failed-start paths | package test; retain |
| `internal/execution/runner_result_race_test.go` | 2 | unit | natural-exit vs terminate publication winner | package test; retain deterministic race contract |
| `internal/execution/runner_stop_receipt_p1_test.go` | 8 | unit | persistence fence/retry ordering, legacy callback compatibility, missing receipt fail-close, unknown outcome, legacy Linux, result finalization and startup failure | package test; retain |
| `internal/execution/runner_termination_order_windows_test.go` | 1 | verification | close stdin before blocked containment stop and one cleanup owner | Windows package test; retain |
| `internal/execution/runner_test.go` | 26 | unit, verification | argv/no shell, pipe drain/backpressure/input, exit/result/termination/tree, identity ordering, blocked IO, environment/credential isolation, bare executable; helper subprocess | package test; retain foundational runner coverage |
| `internal/execution/runner_unix_test.go` | 3 | verification | terminated zero exit is failure and root-exit descendant pipe closure; helper | non-Windows package test; retain |
| `internal/execution/runner_windows_test.go` | 6 | verification | no console, suspended identity/persistence cleanup, descendant stop and root-exit pipe holder; helper | Windows package test; retain |

### Harness registry and Claude

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `internal/harness/harness_test.go` | 14 | harness, validation | registry kind/capabilities/version coherence, generic JSON non-interpretation, retained owner, cancel/close retry, unsupported controls and admission restrictions | `go test ./internal/harness`; retain generic contract |
| `internal/harness/claude/stream_test.go` | 12 | harness, validation | captured CRLF/chunks, observed identity/result/structured object, invalid/control/conflict/repeated/trailing/oversized records and native captures | `go test ./internal/harness/claude`; retain decoder/validator boundary |
| `internal/harness/claude_candidate_adapter_test.go` | 22 | harness | staging/barriers/authority, identity, semantic terminal, cancel/deadline/start races, stable close/cancel, deep copy, bounded terminate, unsupported/resume/model and retained owner | `go test ./internal/harness`; retain adapter lifecycle |
| `internal/harness/claude_candidate_additional_test.go` | 6 | harness | terminal-plus-process outcome, duplicate/late records, sink failure, malformed/truncated diagnostics, idempotent open/close | same; retain |
| `internal/harness/claude_candidate_cmdline_windows_test.go` | 1 | validation | Windows command line remains below CreateProcess limit | Windows package test; retain platform limit |
| `internal/harness/claude_candidate_native_test.go` | 1 | smoke, verification | real Claude binary + fake loopback Messages API proves structured-output schema delivery and semantic decode | `SYMMETRY_CLAUDE_NATIVE_SMOKE=1`, absolute tested binary; retain native boundary |
| `internal/harness/claude_candidate_structured_output_test.go` | 3 | harness, validation | invalid structured task result, schema top-level shape, kind/reason mismatch outside tool schema | package test; retain |
| `internal/harness/claude_probe_test.go` | 9 | harness, validation | exact version/help/schema, unavailable/unsupported, adapter fail-close, bounded/cancelled/late-success probe and failed-output rejection | package test; one narrow candidate C1, remainder retain |

### Codex harness

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `internal/harness/codex/adapter_native_test.go` | 1 | smoke, verification | real app-server version/schema, staged open/replay, persistence-before-events, close result/idempotence | Linux/Windows; `SYMMETRY_CODEX_NATIVE_SMOKE=1` and absolute tested executable; retain |
| `internal/harness/codex/adapter_test.go` | 80 | harness, validation | native staging/admission/sandbox, identity/output barriers, cancellation classification, RPC ordering/dedupe/bounds, schema/result/usage/events, close/cleanup and captured lifecycle | `go test ./internal/harness/codex`; retain; shared-contract refactor candidate C2 only |
| `internal/harness/codex/framing_test.go` | 9 | harness, validation | split/malformed/Unicode/incomplete/oversize/whitespace frames, JSON-RPC shape/version, current captures | same; retain |
| `internal/harness/codex/probe_test.go` | 11 | harness, validation | exact version/help/schema, bounded command construction/errors, cancellation/late success/timeouts, adapter start independence | same; retain; registry comparison C3 |
| `internal/harness/codex/rpc_sandbox_test.go` | 3 | harness, validation | workspace-only policy, temporary-root exclusions and root escape/unresolvable rejection | same; retain |
| `internal/harness/codex_probe_test.go` | 4 | harness, validation | registry-facing Codex probe maps shared version/help/schema evidence and failed output to fail-closed capabilities | `go test ./internal/harness`; retain separate registry mapping; comparison C3 |

### OpenCode harness

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `internal/harness/opencode/adapter_native_terminal_candidate_test.go` | 5 | smoke, verification | real terminal-candidate witness, opt-in preflight, global lifecycle identity, early EOF and truncation causality | Linux/Windows; native OpenCode candidate env/executable/provider fixture; retain |
| `internal/harness/opencode/adapter_native_test.go` | 1 | smoke, verification | real version, HTTP transport, staged session/open replay, persisted identity/events and close result | Linux/Windows; `SYMMETRY_OPENCODE_NATIVE_SMOKE=1`, absolute tested executable; retain |
| `internal/harness/opencode/adapter_native_work_test.go` | 17 | smoke, verification | real synthetic tool/prompt/resume captures, metadata redaction, terminal/timeout/disconnect/decode/max-event cases, proxy headers/limits/redirect, loopback config and repository task | Linux/Windows; several explicit native opt-ins and tested executable/provider loopback; retain |
| `internal/harness/opencode/adapter_test.go` | 25 | harness, validation | probe, process persistence/ownership/secrets, fail-closed open, durable replay/prompt cancellation/admission, peer ownership, shared interrupt/close and lifecycle diagnostics | `go test ./internal/harness/opencode`; retain; shared-contract C2 |
| `internal/harness/opencode/client_test.go` | 15 | unit, harness, validation | numeric loopback/basic auth, health/session/prompt/interrupt/SSE endpoints, redirects/limits, identity/payload/status and per-connection peer verification | same; retain |
| `internal/harness/opencode/peer_linux_test.go` | 7 | verification | kernel peer server, v2 binding/namespace/non-dumpable/owner/released-peer rejection and close synchronization | Linux package test with process/namespace capabilities; retain negative peer probes |
| `internal/harness/opencode/peer_test.go` | 2 | verification | legacy Windows owned connection and wrong identity before HTTP bytes | Linux/Windows file but skips both tests on Linux; Windows is effective boundary; retain |
| `internal/harness/opencode/sse_test.go` | 17 | harness, validation | SSE chunks/CRLF/multiline/limits, strict durable cursor/session, known/unknown lifecycle, offsets/metadata/null/nested/duplicate shapes and captures | package test; retain |

### Pi harness

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `internal/harness/pi/adapter_native_resume_test.go` | 1 | smoke, verification | real retained-session repository resume | explicit native Pi/provider/model credentials and repository fixture; retain |
| `internal/harness/pi/adapter_native_test.go` | 1 | smoke, verification | real offline RPC process/get_state/open replay/session-file isolation/close with no work events | `SYMMETRY_PI_NATIVE_SMOKE=1`, absolute tested executable; retain |
| `internal/harness/pi/adapter_native_work_test.go` | 10 | smoke, verification | real repository task/cancellation, credential env rejection, loopback URL/provider/model/model-list, standard/Windows env and baseline subject | explicit native Pi opt-ins, tested executable and loopback or real repository/provider prerequisites; retain |
| `internal/harness/pi/adapter_test.go` | 31 | harness, validation | staging/result, ownership/barriers, RPC ack-vs-exit/cancel races, abort/clear ordering, malformed final content, close bounds, probe, exact resume/mismatch | `go test ./internal/harness/pi`; retain; shared-contract C2 |
| `internal/harness/pi/argv_test.go` | 8 | validation | audited RPC option pairs/toggles, prompt/non-RPC/joined/NUL/blank/trailing rejection and secret-free errors | package test; retain |
| `internal/harness/pi/probe_test.go` | 3 | harness, validation | version/help bounded failures and no provider-access claim without release evidence | package test; retain |
| `internal/harness/pi/provider_bridge_peer_linux_test.go` | 5 | verification | kernel peer client, FD release, v1 unsupported, legacy stub rejection and non-dumpable qualified peer failure before action | Linux process/namespace boundary; retain |
| `internal/harness/pi/provider_bridge_peer_test.go` | 2 | verification | Windows legacy exact peer bind/replay and wrong identity rejection | portable file but both skip off Windows; retain Windows boundary |
| `internal/harness/pi/provider_bridge_test.go` | 27 | harness, validation | loopback/nonce/server bounds, request/grant/action IDs/replay/concurrency, waiter/entry/execution limits, deadline/unknown semantics, close and credential secrecy | package test; retain |
| `internal/harness/pi/provider_extension_native_test.go` | 1 | smoke, verification | real Pi invokes generated provider tool once and round-trips exact action/result to loopback model | `SYMMETRY_PI_PROVIDER_ACTION_SMOKE=1`, absolute Pi executable; retain |
| `internal/harness/pi/provider_extension_test.go` | 14 | harness, validation, smoke | deterministic/immutable/safe shared extension, Windows legacy, args/adapter bind/revoke/failure, invalid inputs; two real extension/lifecycle smokes | package unit tests plus `SYMMETRY_PI_PROVIDER_EXTENSION_SMOKE=1` real executable for two tests; retain; native comparison C7 |
| `internal/harness/pi/rpc_test.go` | 14 | harness, validation | captured lifecycle/compaction, nonterminal events, acceptance/correlation/state, clear-abort ordering, malformed/duplicate/oversize/truncated/CRLF records and pending response | package test; retain |

### Notification and platform

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `internal/notification/client_test.go` | 10 | unit, verification | Phoenix join/auth/heartbeat/hints, reconnect/error/backoff, cancel, dropped hints, stale close, dial deadline, redirect token secrecy and input validation | `go test ./internal/notification`; retain websocket boundary |
| `internal/platform/containment_supervisor_linux_test.go` | 52 | unit, verification | lease/fence/watchdog, FD/protocol/helper lifecycle, mirror identity/scans, response-loss recovery/receipts, release/abort physical proofs, endpoint/error sanitation and deterministic fault gates | Linux package test; helper subprocess and Unix sockets but no external provider; retain lower-boundary fault injection |
| `internal/platform/containment_supervisor_production_windows_test.go` | 2 | verification | production helper lost-response receipt replay and credential-redacted stop diagnostic | Windows; invokes production helper path; retain |
| `internal/platform/containment_supervisor_receipt_windows_test.go` | 8 | unit, verification | exact receipt replay, release proof/ack/auth, durable recovery mismatch and lost-response exact helper exit | Windows; retain |
| `internal/platform/containment_supervisor_windows_test.go` | 28 | unit, verification | job/helper ownership and partial cleanup, recovery identity/protocol/secret, owner watchdog, stop retry, empty proof, lease sequencing/expiry and redaction | Windows; retain |
| `internal/platform/directory_path_other_test.go` | 1 | unit | non-Windows directory and symlink resolution | non-Windows; retain |
| `internal/platform/directory_path_test.go` | 2 | validation | raw path, file and missing-path rejection | portable package test; retain |
| `internal/platform/directory_path_windows_test.go` | 6 | validation, verification | extended DOS forms, slash-dot/drive-relative rejection, absolute forms and strict UTF-16 | Windows; retain |
| `internal/platform/identity_linux_test.go` | 10 | unit, verification | strict v2 process identity/namespace/stat, v1 parse-but-reject, group-absence proof ordering/errors, canonical positive start time | Linux; retain |
| `internal/platform/linux_cgroup_v2_linux_test.go` | 9 | unit, verification | kill attempted/unknown states, openat2 traversal/symlink/pinned leaf, adoption/control/mount/events parsing, release zero and production gate authentication | Linux/cgroup-related syscalls; retain |
| `internal/platform/linux_owner_anchor_linux_test.go` | 33 | verification | pidfd/anchor/subreaper/native helper lifecycle, grant/run identity, namespaces, evidence/release gates, tombstones, cleanup verifier, kill retry, sibling isolation and exact reap sets | Linux/amd64 with helper processes/namespaces; retain |
| `internal/platform/linux_owner_helper_v2_linux_test.go` | 8 | unit, verification | nested run-tree ordering, child reader bounds, foreign child, untrusted stop, exact reap, pre-fork reservation, lease status and response action | Linux/amd64; retain |
| `internal/platform/linux_ptrace_launch_linux_test.go` | 10 | verification | pre-user-code stop/fence, persistent owner thread, terminate retry/group, unavailable pidfd/ptrace/conflicting group/unexpected stop cleanup; helper | Linux ptrace/pidfd capability; retain |
| `internal/platform/linux_supervisor_protocol_linux_test.go` | 3 | unit, validation | unknown/trailing/bounded frames and watchdog disarm-vs-EOF | Linux; retain |
| `internal/platform/native_launch_windows_test.go` | 13 | verification | argv/command/CWD/env/handles, suspended launch/job transfer, orphan prevention, wait/close races; helper | Windows native APIs; retain |
| `internal/platform/process_unix_linux_test.go` | 58 | unit, verification | pidfd support/stat parsing, escaped descendants/scans/uncertainty, attach/release/terminate/close serialization and proof, monitor fences, PID reuse and thread-child helpers | Linux; retain; OS contract comparison C5 |
| `internal/platform/process_windows_test.go` | 26 | unit, verification | headless flags, partial attach/job cleanup/retries, full supervisor receipt/release, empty proof, termination serialization and renewal/authority synchronization; helpers | Windows; retain; C5 |
| `internal/platform/tcp_peer_linux_test.go` | 4 | unit, verification | proc TCP inode parsing/bounds and deferred accept deadline/no-write | Linux; retain |
| `internal/platform/tcp_peer_linux_v2_test.go` | 4 | unit, verification | fdinfo inode, datagram/cookie rejection, retained pidfd and pinned namespace | Linux v2; retain |
| `internal/platform/tcp_peer_test.go` | 2 | verification | invalid peer inputs and owned helper process | Linux/Windows; retain common surface plus real helper |
| `internal/platform/tcp_peer_windows_test.go` | 4 | unit, verification | native established connection, malformed table buffers/tails and exact established owner | Windows; retain |

### Protocol and native gate

| File | Count | Category | Guarded behavior / boundary | Entrypoint and disposition |
|---|---:|---|---|---|
| `internal/protocol/goal_adapter_operations_test.go` | 2 | validation | missing prerequisites and minimal/additive capability combinations | `go test ./internal/protocol`; retain |
| `internal/protocol/goal_admission_input_test.go` | 3 | validation | schema-version/legacy classification, distinctive-field schema requirement and duplicate control members | same; retain |
| `internal/protocol/goal_contract_fixture_test.go` | 2 | validation, verification | shared contract fixtures parse and evidence review verdicts agree | same, reads repository contract fixtures; retain cross-language boundary |
| `internal/protocol/goal_contract_semantics_test.go` | 6 | validation | revision semantics, duplicate predicates, provider scope/resource operations, change-target rules and ECMAScript whitespace | same; retain |
| `internal/protocol/goal_evidence_batch_response_test.go` | 4 | validation | receipt identity/correlation and conflict item/index/key uniqueness | same; retain |
| `internal/protocol/goal_evidence_batch_test.go` | 5 | validation | batch/item identity, duplicate keys/IDs/members and escaped-name canonical duplicate detection | same, two local malformed fixtures; retain |
| `internal/protocol/goal_subject_test.go` | 5 | validation | line separators, literal escapes, JSON string/Unicode, escaped duplicate members and negative zero | same; retain |
| `internal/protocol/goal_transport_test.go` | 13 | validation | canonical JSON/hash, strict DTO enums/fields, work/session/purpose/provider scope, normalized results, blocker/text bounds, evidence consistency and legacy work shape | same; retain |
| `internal/protocol/goal_validation_test.go` | 1 | validation | protocol-local commit path UTF-8 rune bound and invalid UTF-8 | same; retain independent validator (C4) |
| `internal/protocol/registration_capabilities_test.go` | 9 | validation | legacy byte compatibility, canonical capabilities, invalid raw JSON, native/generic metadata, repository identity and impossible/control-incompatible claims | same; retain |
| `internal/protocol/work_capabilities_test.go` | 2 | validation | capability round trip and non-boolean rejection | same; retain |
| `native/linux-gate/protocol/clone_cleanup_linux_test.go` | 4 | verification | missing receipt before launcher, installed clone cleanup, parent-death cleanup and helper | Linux/amd64 installed native gate/scratch; retain |
| `native/linux-gate/protocol/clone_integration_linux_test.go` | 2 | verification | installed owner clone checkout and native Git helper | Linux/amd64, installed owner/gate/Git; retain |
| `native/linux-gate/protocol/launcher_integration_linux_test.go` | 10 | verification | descriptor/version/config compatibility, pinned image, fake namespace, cgroup FD rejection, installed boundary, owner-death and unsupported peer host | Linux/amd64 installed native gate and namespace/cgroup prerequisites; retain |

## Support fixtures and suffixless entrypoints

### Machine-consumed fixtures (18; retain with their named consumer)

| Support path(s) | Consumer and responsibility |
|---|---|
| `internal/contracts/schema_bundle.json` | production-embedded generated schema bundle consumed by `schema.go` and exercised by all 16 `schema_test.go` tests; generated input, not an independent test |
| `internal/protocol/testdata/evidence-batch.duplicate-item.json`, `evidence-batch.duplicate-outer.json` | malformed duplicate-member bytes consumed by `TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers`; retaining raw bytes matters because ordinary JSON construction cannot preserve duplicates |
| `internal/harness/testdata/claude/2.1.281/stream-json.jsonl`, `structured-output-missing-after-text-success.jsonl`, `structured-output-retry-exhausted.jsonl`, `structured-output-task-result-success.jsonl` | captured Claude stream records consumed by `claude/stream_test.go` |
| `internal/harness/testdata/codex/0.156.1/app-server-help.txt`, `app-server-lifecycle.jsonl`, `app-server-start-smoke.jsonl`, `frames.jsonl` | current tested-version help/lifecycle/framing captures consumed by Codex probe/adapter/framing tests |
| `internal/harness/testdata/opencode/1.18.32/lifecycle-terminal.sse`, `lifecycle-tool-retry.sse`, `missing-result.sse`, `session-events.sse`, `unknown-then-terminal.sse` | current OpenCode SSE captures consumed by `sse_test.go` |
| `internal/harness/testdata/pi/0.87.1/compaction-continuation.jsonl`, `lifecycle.jsonl` | current Pi lifecycle captures consumed by `rpc_test.go` |

### Unconsumed config fixture (1; candidate ownership unresolved)

- `cmd/symmetry-daemon/testdata/valid-config.json` has no literal source or test
  reference in the current daemon tree. It is not executable coverage. C9 defines
  the smallest removal experiment, but source inspection cannot decide whether an
  external packager/operator treats it as a sample.

### Test helper entrypoints (2; retain)

- `e2e/pi-control-witness/main.go`: separately built process used by Pi/control E2E
  orchestration; it is executable fixture support, not a test declaration.
- `e2e/preauthority-sentinel/main.go`: Windows pre-authority crash sentinel; its
  process lifetime and exit are the boundary under test.

### Provenance/evidence records with no Go test consumer (31)

These are not active fixtures and should not be counted as executable tests. They
are historical/current native evidence, version pins, schema manifests, and
README provenance. Test ablation gives no basis to delete historical evidence.
A future evidence-retention policy may archive them, but that is not a baseline
versus ablation experiment.

- Claude: `testdata/claude/2.1.259/{README.md,version.txt}` and
  `testdata/claude/2.1.281/{README.md,native-structured-output-windows-2026-09-24.md,version.txt}`.
- Codex 0.153.4: all five files (`README.md`, `app-server-help.txt`,
  `app-server-start-smoke.jsonl`, `native-smoke.md`, `schema-manifest.json`).
- Codex 0.156.1: `README.md`, `native-smoke.md`, `schema-manifest.json`,
  `version.txt`.
- OpenCode 1.18.30: `README.md`, `native-synthetic-tool-current-run-2026-09-13.md`,
  `version.txt`; OpenCode 1.18.32: `README.md`,
  `native-windows-2026-09-24.md`, `version.txt`.
- Pi 0.85.1: all nine README/version/native evidence files; Pi 0.87.1:
  `README.md`, `native-windows-2026-09-24.md`, `version.txt`.

### Production native sources falsely classifiable as test support (2)

- `cmd/symmetry-linux-launcher/c-wrapper/wrapper.c` is the production launcher
  wrapper exercised by launcher/native tests.
- `native/linux-gate/c-gate/gate.c` is the production Linux gate exercised by
  native integration tests.

They are neither fixtures nor removable harnesses. Their unavailability on a host
blocks verification; it does not make the tests redundant.

## Candidate and discriminating-experiment ledger

No whole test file is proven redundant by source inspection alone. The following
are all concrete overlap candidates found. Only C1 is narrow enough to attempt
without first consolidating production code. C2-C8 are useful
ablation/mutation experiments whose current disposition is retain, and C9 is
blocked on non-test ownership. Every proposed deletion has an explicit survivor and a mutation that
can distinguish accidental name-level overlap from equivalent coverage.

### C1 - direct Claude bounded-probe late-success unit

- **Candidate:** `TestBoundedProbeRejectsSuccessAfterDeadline` in
  `claude_probe_test.go`.
- **Overlap:** `TestClaudeProbeRejectsSuccessAfterProbeDeadline` drives the same
  `runBoundedProbe` late-success path through the only production consumer and
  additionally asserts that no capability evidence is promoted. Both use a
  callback/runner that waits for context expiry and then returns apparently valid
  output. The direct test alone additionally requires the private helper's raw
  output return to be nil.
- **Smallest experiment:** baseline only those two tests; temporarily remove the
  direct test; mutate `runBoundedProbe` after deadline to return the callback's
  bytes with `context.DeadlineExceeded`, then separately mutate it to return the
  bytes with nil error. Run only the survivor. The survivor must fail both
  mutations and pass restored source. If it fails only the nil-error mutation,
  the nil-output assertion is distinct and the candidate stays.
- **Required survivor:** `TestClaudeProbeRejectsSuccessAfterProbeDeadline`.
- **Disposition:** candidate, not proven. This is the only plausible direct test
  deletion found; parent owns execution.

### C2 - repeated generic adapter contracts across Codex, OpenCode, and Pi

- **Candidates examined:** same-named timeout/cancellation tests across Codex,
  OpenCode and Pi; `TestAdapterStartForwardsPersistProcessWithAuthority` in Codex
  and Pi; `TestStartRetainsProcessOwnerWhenRunnerReturnsError` in OpenCode and Pi;
  and `TestCloneTaskResultPreservesSchemaValidEmptyArrays` in Codex and Pi.
- **Comparison:** setup and assertions are similar, but every pair executes a
  separate adapter implementation, separate fake process type, and often a
  different transport/close path. OpenCode uses HTTP/SSE ownership, Codex JSON-RPC,
  and Pi line RPC. The clone helpers are separately implemented in
  `codex/adapter.go` and `pi/adapter.go`. Failure modes can diverge independently.
- **Smallest experiment:** for each apparent pair, mutate only one adapter: drop
  one authority callback, return nil rather than a retained owner after runner
  error, accept successful probe output after cancellation, or convert an empty
  semantic array to nil in only one `cloneTaskResult`. Run the proposed survivor
  from the other adapter. It will not touch the mutated implementation; then run
  the same-adapter named test, which must fail. This discriminates similarity from
  coverage.
- **Named survivors if production is first refactored to one shared helper:** keep
  provider-specific lifecycle tests plus one shared conformance case in
  `internal/harness/harness_test.go`; specifically retain
  `codex.TestAdapterStagesNativeTransportAndRequiresMatchingStructuredCompletion`,
  `opencode.TestStartUsesOwnedServeInvocationPersistsBeforeOutputAndWithholdsSecrets`,
  and `pi.TestAdapterStagesPiRPCAndRequiresSettledExplicitTaskResult`.
- **Disposition:** retain now. A table-driven shared conformance helper could
  reduce test code only after production forwarding/clone/probe behavior is also
  shared; deleting one adapter's test before that would lose coverage.

### C3 - Codex registry probe versus concrete Codex package probe

- **Candidates examined:** the four tests in `internal/harness/codex_probe_test.go`
  versus overlapping cases in `internal/harness/codex/probe_test.go`.
- **Comparison:** both consume shared `ValidateCodexVersionOutput`,
  `ValidateCodexHelpOutput`, and `ValidateCodexProbeEvidence`, with similar fixture
  runners. The registry tests assert `harness.Capabilities` mapping and registry
  call order; concrete tests assert `codex.ProbeResult`, schema command bounds,
  cancellation, and adapter behavior. Entrypoints and returned DTOs differ.
- **Smallest experiment:** mutate only `registry.go`'s
  `probeCodexExecutableWithRunner` to set `Start=true` after valid help/schema.
  Required survivor `codex.TestProbeKnownVersionAndHelpStillFailsClosed` will stay
  green; registry test `TestCodexProbeKnownVersionHelpAndSchemaFailsClosed` must
  fail. Conversely mutate `codex.Probe` result mapping and verify only the concrete
  test fails.
- **Disposition:** retain both caller layers. Shared validator assertions could be
  moved to a dedicated validator table, but no current whole test is redundant.

### C4 - control and protocol commit-path UTF-8 tests

- **Candidates examined:** `control.TestValidateGoalCommitPathUsesUTF8RuneLength`
  and `protocol.TestValidateCommitPathUsesUTF8RuneLength`.
- **Comparison:** setup and three assertions are effectively parallel, but they
  invoke independent unexported validators in different packages and protect
  different HTTP/protocol boundaries.
- **Smallest experiment:** change only `control.validateGoalCommitPath` from rune
  count to byte count; the proposed protocol survivor remains green while the
  control test fails. Repeat in the other direction.
- **Named survivors under a future shared production validator:** retain the
  shared validator table and boundary checks
  `control.TestGoalContextRejectsRawSchemaViolationBeforeSemanticValidation` and
  `contracts.TestCommitPathUsesBoundedECMAScriptValidation`.
- **Disposition:** retain now; source consolidation, not test ablation, is the
  prerequisite.

### C5 - same-named Unix and Windows partial-attach tests

- **Candidates examined:** both
  `TestAttachProcessReturnsPartialContainmentAfterIdentityFailure` declarations.
- **Comparison:** Linux configures a process group/pidfd, expects unresolved close
  and non-retryability; Windows configures a job and expects clean close. They use
  different identity hooks and OS process trees. The shared name masks opposite
  close semantics.
- **Smallest experiment:** mutate only Linux partial containment to return nil on
  `Close`; Windows survivor stays green and Linux test fails. Mutate Windows to
  preserve unresolved close; Linux stays green and Windows fails.
- **Named survivors:** the opposite-platform test in each experiment, which
  deliberately demonstrates non-equivalence rather than coverage.
- **Disposition:** retain both; native platform checks cannot substitute for one
  another.

### C6 - negative peer probes across generic/adapter/Linux-v2 boundaries

- **Candidates examined:** OpenCode `peer_test.go` and `peer_linux_test.go`, Pi
  `provider_bridge_peer_test.go` and `provider_bridge_peer_linux_test.go`, plus
  platform `tcp_peer*` tests.
- **Comparison:** Windows legacy tests verify PID/creation identity before any
  HTTP bytes. Linux v2 tests verify retained pidfd, network/user namespace,
  non-dumpable policy, released-owner rejection and active-verification shutdown.
  Platform tests validate raw kernel table/proc parsing. Required environments and
  denial reasons differ.
- **Smallest experiment:** mutate Linux v2 verifier to accept v1 binding or a
  released owner. Named Windows survivor
  `TestLegacyWindowsPeerVerifierRejectsWrongIdentityBeforeWriting` remains green;
  Linux survivor `TestOpenCodeV2VerifierRejectsReleasedPeer` must fail. Conversely
  bypass Windows creation identity: Linux tests remain green and
  `TestLegacyWindowsPeerVerifierRejectsWrongIdentityBeforeWriting` fails. For Pi,
  use `TestProviderBridgeV1BindingIsUnsupportedOnLinux` and
  `TestWindowsLegacyProviderBridgeRejectsDifferentProcessIdentity`.
- **Disposition:** retain all. These are distinct platform/security boundaries,
  not duplicate negative probes.

### C7 - native transport/provider smoke entrypoints

- **Candidates examined:** Codex/OpenCode/Pi open-close smokes; Pi
  `TestGeneratedProviderBridgeExtensionLoadsInNativePi`,
  `TestNativePiProviderBridgeLifecycle`, and
  `TestNativePiProviderBridgeAction`; Claude structured-output smoke; OpenCode
  native work/candidate tests.
- **Comparison:** open-close smokes share staging/persistence assertions but use
  different executable protocols and result semantics. Pi extension-load proves
  generated JS loads and answers `get_state`; lifecycle proves adapter binding and
  revoke/close with peer identity; provider-action proves a model tool call crosses
  extension, authenticated bridge, executor, result, and semantic completion.
  Claude proves CLI schema delivery; OpenCode proves HTTP/SSE and peer ownership.
- **Smallest experiments:** (a) mutate only Pi generated extension's tool
  registration name; transport open-close survives, while named survivor
  `TestNativePiProviderBridgeAction` must fail at advertised tool/execution.
  (b) mutate Pi adapter to omit bridge lifecycle bind; extension-load survives,
  while `TestNativePiProviderBridgeLifecycle` must fail. (c) mutate each provider's
  transport framing independently; the other providers' open-close tests survive
  and the same-provider smoke fails.
- **Disposition:** retain all. Missing tested binaries, credentials, cgroup/systemd
  setup, or native provider access is a blocker, not redundancy.

### C8 - production Linux replay witnesses versus package fault injection

- **Candidates examined:** production containment replay/negative/v2 witness files
  versus `containment_supervisor_linux_test.go`, `process_unix_linux_test.go`, and
  native gate protocol tests.
- **Comparison:** package tests deterministically inject response/scan/signal
  failures and inspect in-memory authority. Production witnesses build/run the
  actual daemon/helper, cross durable journals and control HTTP, crash processes,
  and bind receipts to physical process/cgroup observations. Native gate tests
  cross installed binary/FD/namespace/Git boundaries. No lower-level test observes
  the complete production sequence.
- **Smallest experiment:** mutate daemon recovery wiring to skip persisted receipt
  replay while leaving `linuxSupervisor` unit behavior intact. Named package
  survivor `TestLinuxSupervisorResponseLossStopUsesRecoveryReceipt` may stay green;
  `TestProductionLinuxContainmentReplayWitness/stop_response_lost` must fail.
  Conversely mutate the injected response-drop gate; the production witness may
  be unable to enter its setup, while
  `TestLinuxSupervisorResponseDropStopFiresOnceAndWritesMarker` localizes it.
- **Disposition:** retain both layers. Systemd/native host prerequisites remain
  explicit blockers and cannot be replaced with fake-agent coverage.

### C9 - orphan `valid-config.json` testdata file

- **Candidate:** `cmd/symmetry-daemon/testdata/valid-config.json` has no literal
  consumer in daemon Go source/tests and is not embedded or opened dynamically by
  any inspected package.
- **Smallest experiment:** baseline
  `go test ./cmd/symmetry-daemon ./internal/config`; remove only the JSON file;
  repeat those focused tests and inspect package/release copy manifests for the
  exact path. Then mutate an equivalent required field in the inline valid-config
  setup to prove the survivor is meaningful rather than a no-op.
- **Required survivor:** `config.TestLoadAcceptsValidConfiguration` (and the
  command package's state-dir tests for command packaging/build integrity).
- **Disposition:** blocked, not removal-ready. A green test ablation would prove
  only that Go tests do not consume it; owner/release review must establish that it
  is not an intentional sample artifact.

## Blockers and required environments

- This Windows workstation cannot execute Linux/amd64 cgroup-v2, pidfd, ptrace,
  namespace, installed owner/gate, rootless-compose, or systemd/native-host checks.
- Native Claude, Codex, OpenCode, and Pi smokes require exact tested executables;
  work/provider checks additionally require their explicit opt-ins, loopback
  fixtures or credentials. No credential availability was inspected.
- `goal_receipt_e2e` and daemon E2E witnesses require private control service/DB
  fixture state and absolute private input paths.
- Windows-native execution/platform/pre-authority checks are distinct from the
  parent's reported final Windows App result; this worker did not rerun them.
- The two zero-test `_test.go` shims are build support, not empty redundant suites.
- Historical capture/provenance files without Go consumers are evidence-retention
  responsibility, not executable test-ablation targets.

## Refactoring opportunities (not deletion authorization)

1. Share adapter-neutral probe timeout/cancellation test machinery and production
   helper only if Codex/OpenCode/Pi can genuinely call one implementation; keep a
   provider-specific invocation assertion for every adapter.
2. Share `TaskResult` deep-clone implementation between Codex and Pi, then replace
   parallel schema-valid-empty-array tests with one shared unit plus adapter event
   tests. Until source is shared, both tests are required.
3. Share commit-path rune validation between protocol/control/contracts only if
   dependency direction remains valid; retain each external boundary decode test.
4. Rename platform-specific same-named tests (partial attach and peer verification)
   to expose their opposite close/proof semantics. This improves review but saves
   no test.
5. Split executable native smokes from ordinary package files into documented CI
   targets without weakening opt-in gates. Their skips are prerequisite receipts,
   not evidence to delete them.

## Served model and verification performed

Actual served model: `ghc/gpt-5.6-sol-fast` (`PI_REASONING_LEVEL=medium`).
Verification for this documentation-only wave was static source inspection,
manifest reconciliation, declaration counting, build-tag/entrypoint inspection,
fixture-consumer search, and Git status inspection. No build, test, service,
native executable, credential, or mutation experiment was run; candidate outcomes
remain for the parent-owned experiment phase.
