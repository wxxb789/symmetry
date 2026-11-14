# Goal 0006j: OMP 18.20.0 evidence boundary

Status: **incomplete; no operational capability promoted on either platform**.
This is an implementation and observation record, not a release certificate.
The binding contract is `docs/goals/0006j-omp-native-capability.md`; its owner
decision supersedes the historical 18.18.7 pin. The operator's current dispatch
brief corrects the credential source to the configured virtual key.

## Exact subjects

| Platform | Executed binary | Bytes | SHA-256 |
| --- | --- | ---: | --- |
| Windows host, NT 10.0.26300, x64 | `C:/Users/lhan/AppData/Local/mise/installs/oh-my-pi/18.20.0/omp.exe` | 251502592 | `1b6c7bda8241bc7d948495539f90ec47e6f30d1fec72b6ee097a67a1fc8d8e48` |
| `wsl.exe -d Ubuntu-24.04`, Ubuntu 24.04.5 LTS, kernel `6.18.58.1-microsoft-standard-WSL2`, x86_64 | `/root/.local/bin/omp` | 287484080 | `c8694be8c6e6a1f45e3636dd8a8e9eaa78878ad629a89000b403ec5e456d3f89` |

Both report `omp/18.20.0`. A matching version string or mise shim is not an
equivalent executable. The adapter checks the blob, anchored version and RPC
help before independent protocol negotiation. OMP is registered as `omp`, with
implementation identity `omp-rpc-v2`; it is not Pi. Its Linux route uses the
ordinary existing process owner, not the Pi/OpenCode Linux v2 owner route.

## Observed native transport

The sanitized fixtures and their source/blob/platform digests are under
`daemon/internal/harness/testdata/omp/18.20.0/`. Each source observation has an
`identity.json` and `protocol.jsonl` under the session evidence root
`.tmp/gdd-0006j-session/`.

On both exact binaries, `probe.py` launched `--mode rpc` with an explicit private
session directory and isolated nonsecret provider catalog. It observed:

- An initial `ready` frame at protocol 1, advertising versions `[1, 2]`,
  1048576-byte frames and a 67108864-byte reassembled limit.
- Successful correlated negotiation to protocol 2 and idle `get_state`.
- Explicit rejection of protocol 999.
- Native session ID and absolute filename under the configured directory.
- A wrong-credential prompt ending in matching `prompt_result.status=error`,
  `session_settled` and clean process EOF.
- Abort after prompt acknowledgement, matching `status=aborted`, an empty
  restored queue, settlement and clean EOF. No running repository tool was
  observed: this is **not** in-flight tool cancellation evidence.
- A second process reopening the failed turn's exact session ID/file, without
  another prompt. This is **not** successful retained context continuity.
- Different native identities for fresh sessions. This alone does not prove a
  successful compatible handoff preserving repository artifacts.

The actual production `Adapter.Probe` also ran on both platforms. It returned
`version_known=true`, `transport_verified=true`, `verified=false`, with every
operational capability false and usage unknown. Receipts:
`linux-adapter-probe.log`, `adapter-probe-commands.json`,
`windows-adapter-probe-corrected.log` and
`windows-adapter-probe-corrected-command.json` under the session root.
The Windows throwaway executable included the daemon's actual hidden
containment-supervisor dispatch; the first wrapper omitted it and failed.
That wrapper failure was not accepted as adapter evidence. Its task-owned
processes were stopped and the throwaway source/binary removed.

## Credential correction and zero-spend blocker

The first native prompts inherited the original `TENCENT_GLM_API_KEY` value.
The relay rejected that value with HTTP 401. The operator subsequently clarified
that it differs from the working virtual key in the harness configuration.
The old 401 observations therefore establish only a negative credential path,
not that the approved configured credential is unusable.

The relay base URL was read from the existing provider configuration, not
guessed. The corrected virtual key was read in memory and accepted by a
non-generating authenticated `GET /v1/model/info` on Windows. The response
advertised these rates for `deepseek-v4.1-flash-fast`:

| Metadata field | Advertised value |
| --- | ---: |
| input cost per token | 0.0000001 |
| output cost per token | 0.0000001 |
| cache-read input-token cost | 0.00000001 |

The configured catalog did not supply model cost metadata. The remote rate
metadata is not a billing receipt or proof of the served model. Permitted spend
is zero; no verified zero-cost or hard-zero-budget route was available.
**No generation request used the corrected virtual key.** Positive repository,
resume, handoff and running-tool cancellation acceptance therefore remain
unavailable under the spend constraint. The read-only metadata request is not
native OMP provider-access proof. Safe local receipt:
`provider-pricing-observation.json`.

The isolated catalogs contain only an environment-variable name, never a key
value. Linux propagation uses per-process environment forwarding, not argv.
The final session report records the full-value credential scan and its scope;
the operator's original configuration is a secret source, not a deliverable.

## Implementation and supporting verification

The candidate owns a distinct bounded JSONL/v2 chunk decoder. It rejects
duplicate fields, malformed UTF-8/chunks, mismatched and replayed responses,
unknown protocol versions and incomplete final messages. A successful semantic
result must be the entire final assistant text and pass the canonical
`protocol.ParseTaskResult` schema and domain validation.

Launch remains staged: process persistence, `Open`, durable native-handle
persistence by the caller, normalized session-start persistence, then one
prompt. A sink failure is fatal. Settlement is distinct from physical exit;
completion waits for process exit and output drain. Cancel uses native abort
with bounded physical-stop fallback, never an inferred safe pause.
`StopProofSession.WaitStopped` exposes that physical barrier separately from
the sticky protocol/turn failure returned by `Wait`, so stopped-session cleanup
can converge without erasing the original terminal failure.
Retained resume binds the compatible native version and exact idle contained
session file. Fresh handoff uses a new native session and preserves durable
lineage through the existing App/Control machinery. Provider broker access
remains Pi-only; native model authentication does not imply broker support.

Control accepts `omp` through the existing enum/changeset and database-check
boundaries. The migration widens both harness-kind checks; rollback locks the
tables and refuses to discard existing OMP rows. No wire schema generation is
needed because `Adapter.kind` already uses `ShortIdentifier`.
OMP is Goal-admission-only on both platforms: Control excludes legacy scheduling
and claims, and the daemon rejects a non-admission assignment before publishing
running, preparing a workspace or reaching the raw process starter. Empty
legacy task capability requirements cannot bypass the unpromoted projection.

The focused commands recorded in `focused-green-gates.json` were:

```text
cd daemon
go test ./internal/harness/omp -count=1 -timeout=60s -v
go test ./internal/app -run OMP -count=1 -timeout=60s -v
go test ./internal/config -run OMP -count=1 -timeout=30s -v
```

All three commands exited 0. The config filter matched no tests and is **not**
configuration coverage. The OMP command skipped three opt-in live/helper tests;
those skips are not native acceptance. App's six fresh/handoff/resume ×
lost-attach-ack variants use injected capabilities/native sessions: they support
durable integration, not exact-binary native behavior.

Three strengthened regressions first failed in `omp-regressions-red.log`
(exit 1): missing normalized session-start, idle shutdown inventing a missing
result, and escaped-NUL TaskResult schema bypass. They passed after the fixes.
Start-event persistence refusal additionally verifies that no prompt is sent.
The redundant completed-response map was removed; pending-call correlation
still rejects duplicate/wrong IDs. The native witness forwards optional EOF
cleanup rather than accidentally forcing termination.

After independent review, the cancellation witness's release-file observation
window was removed. Its exact bash command now runs a helper and descendant
with nonce-bound, fsynced production creation identities. Cancellation requires
their readiness handshake; post-stop proof uses pre-cancellation Windows
kernel process handles or Linux production identity absence/replacement.
`TestNativeAcceptanceCancelToolOracle` and the real retained-file/symlink refusal
test both passed on Windows and unprivileged Linux UID 65534 (exit 0;
`windows-oracle-green.log`, `linux-oracle-green.log`). The symlink subtest ran
on both platforms without a skip. The oracle rejects live processes even when
the forbidden artifact is absent. This is supporting process-oracle evidence,
not an actual native OMP in-flight tool cancellation run.

`TestNativeAcceptance` retains strict positive repository, continuity-token,
fresh-handoff, single-write/artifact, Subject and running-tool cancellation
predicates. Its fsynced completion followed by helper exit 73 tests only the
adapter/witness acknowledgement boundary. It explicitly does **not** attest
production App/Control journal/outbox crash recovery. It was not run with the
corrected credential because of the spend blocker; no assertion was weakened.

## Clause-to-evidence boundary

| Contract class | Evidence/outcome |
| --- | --- |
| Exact Linux and Windows pin | The binary table and per-observation fixture manifest bind path, size, digest and platform. |
| Separate harness and independent RPC | Distinct registry kind, decoder, exact probe and two-platform transport fixtures; no Pi capability claim reused. |
| Unknown/incompatible protocol fail closed | Native version-999 rejection plus malformed/unknown negotiation regressions; no operational capability advertised. |
| Repository work | **Unavailable** with corrected credential under zero permitted spend; historical wrong-key prompts produced no artifact. |
| Cancellation | Abort/settle/EOF observed on both binaries; **unverified** for a running repository tool and production durable cancellation. |
| Retained resume | Failed-session ID/file reopening observed; **unverified** for successful retained context/work continuation. |
| Compatible fresh handoff | Fresh identities and supporting App lineage tests; **unverified** for successful native artifact-preserving handoff. |
| Exact session-directory provenance | Explicit directories and observed absolute contained native handles in platform receipts; symlink/compatibility refusal unit checks. |
| Restart/lost acknowledgement/no duplicate effects | Six injected App recovery scenarios pass; strict native witness exists but positive run is unavailable. No claim of exact-binary production recovery. |
| Artifact and Subject integrity | Canonical result/schema/hash and malformed-result checks support the boundary; **unverified** for a successful real native artifact. |
| Usage classification | Always `unknown`; no billable usage, hard cost limit, zero-billing or served-model attestation. |
| Provider access | Authenticated relay metadata succeeds outside OMP; native provider access and Control provider broker remain unsupported. |
| Authorized repository/resource | Only the private fixture repository was cloned locally; no remote write, ref, release or package was created. |
| Independent final review and required gates | Exact review receipt, command outcomes and residual limits are recorded in the session's `answer.md`; this record does not substitute for them. |
| Independently mergeable PR | No commit, push or PR was authorized. The worktree remains dirty; the goal is not declared merge-ready. |

## Capability decision

On **both Windows and Linux**, start, events, cancel, resume, handoff, guidance,
native pause, approval response, provider access and hard cost limits remain
unsupported/unpromoted. Version/transport observations are factual metadata,
not operational support. Usage remains unknown. The goal stays incomplete.

## Review round 1 repairs

The local repairs address every FIX in
`.tmp/gdd-0006j-fix-findings-r1.md`; they do not promote native capabilities or
replace the historical exact-binary observations above.

- R1: final assistant and text-block contract fields use exact JSON keys.
  Case aliases, including the ordered `stopReason=error` / `stopreason=stop`
  reproduction, fail as nonsemantic `failed/missing_result`; no TaskResult event
  is emitted. Unrelated native metadata remains permitted, and protocol and
  physical-stop errors remain visible. Role checks precede message routing, so
  a conflicting user/assistant role cannot be ignored after a valid result and
  a missing canonical role cannot escape the typed-failure classification.
- R2: `SYMMETRY_OMP_NATIVE=1` still selects the opt-in test; it is not spend
  authority. `SYMMETRY_OMP_OWNER_SPEND_MICROUSD` must explicitly authorize a
  positive amount within the contract's approved ceiling. That ceiling remains
  zero, so no environment value authorizes generation in this increment.
  Configuration refuses before credentials, setup or native launch, every real
  prompt has an additional pre-dispatch check, and output names each skipped
  repository, resume, handoff, cancellation or authentication-refusal prompt.
  Non-generating probe paths, TLS and the non-loopback provider checks remain.
- R3: retained identity negatives change only the retained session fixture.
  Runtime identity metadata and operations stay valid; rejection must leave
  Task generation, retained binding, active owner and Run count unchanged.
  Production retained guards are unchanged.
- R4: the acceptance probe decision calls `Capabilities.Validate()` and always
  requires `ErrNativeUnverified` and `Verified=false`. The two demonstrated
  executable-capability escapes are pure negative regressions.
- K1: each chunk ID is bounded to 4 KiB, the permanent ledger to 65,536 entries
  and cumulative ID bytes to 64 MiB. Admission fails before allocating logical
  reassembly state or retaining a new over-budget ID. No historical ID is
  evicted or cleared. A streamed valid-ID regression reaches the exact byte
  ceiling and still rejects the first, middle and last historical replays.

R1's exact conflicting input and K1's 84-frame input both failed their new
regressions against pre-fix source overlays (exit 1); the old ledger delivered
and retained every frame. R4's two escaping return values failed before its
decision fix (exit 1). Native opt-ins with a positive requested spend were
observed to skip every prompt under the unchanged zero ceiling (exit 0).

No runtime benchmark/measurement consumer was found in the scoped OMP/App
tests, Goal 0006j documents, scripts or existing delivery records. Native blob
sizes, prices and historical observations are unchanged; no provider request
or new native generation was issued by this repair stage.

The additional role-routing regressions also failed against the pre-routing
repair overlay (exit 1): a missing canonical role remained unknown, and the
reverse role conflict reused the prior successful result and emitted one
TaskResult event. Independent read-only review then confirmed the routing
repair and reported no residual finding. The scoped simplification pass kept
the exact-field, spend-decision and probe-decision helpers because each protects
a distinct invariant. No wrapper-only abstraction was added.

### Final repair gates and counts

These are local working-tree results, not an exact-commit native acceptance
certificate. An intermediate gate run was cancelled when review exposed the
role-routing defect; no result from that run was accepted. The final run below
used the completed repair source. `-json` records counts without changing the
requested package selection, repetition or timeout.

| Gate | Exit | Actual result |
| --- | ---: | --- |
| `gofmt -l` on the five changed OMP Go files | 0 | No unformatted files. |
| `cd daemon && TMP="$TEMP" go test -json -count=1 -timeout 600s ./internal/app/... ./internal/harness/...` | 0 | All seven packages passed; OMP 5.365 s, App 136.027 s. |
| `python scripts/ci/test-opencode-v2-support-witness.py 2>/dev/null`, after the successful Go gate in `daemon` | 2 | No tests executed: the specified file does not exist. Direct root-path read and repository filename search also found no such script. The requested `|| true` masks this failure; it is not a passing witness gate. |
| Control `mix format --check-formatted`, `mix compile --warnings-as-errors`, `mix test` | Not run | Delegated to CI by the caller; `mix` is unavailable locally. No CI outcome is asserted. |
| Runtime measurement harness | Not applicable | No measured consumer found in the scoped search; no historical measurement was replaced. |

| Count metric | Before repairs | After repairs |
| --- | ---: | ---: |
| Go top-level pass | 950 | 962 |
| Go top-level fail | 0 | 0 |
| Go top-level skip | 32 | 32 |
| Go top-level total | 982 | 994 |
| Go pass including subtests | 1886 | 1928 |
| Go total including subtests and skips | 1918 | 1960 |
| Control target-file test declarations | 76 | 76 |

The baseline counts were recomputed from
`.tmp/gdd-0006j-simplify-go-gate.jsonl` (exit receipt
`.tmp/gdd-0006j-simplify-go-gate.exit`). Final repair counts come from
`.tmp/gdd-0006j-fix-r1-go-gate.jsonl`; matching Go and Python exit receipts are
`.tmp/gdd-0006j-fix-r1-go-gate.exit` and
`.tmp/gdd-0006j-fix-r1-python-gate.exit`. Control declaration counts are static
inventory, not execution evidence. Skips remain unavailable acceptance evidence.
All reproduction overlays and task-owned backup sources were removed. No commit,
push or history rewrite was performed.

### Second repair round — Windows retained paths and OMP registration

This round repairs F1 and F2 from
`.tmp/gdd-0006j-fix-findings-r2.md` on the local working tree based on
`1de506cfe4605e1ac2e941e2f7e0f6e288c8d785`. The first-round numbers above are
historical observations, not results for this round.

- F1: `bindState` revalidates the returned filename with `sessionFilename` and
  the retained filename with `resumeFilename`, using the same configured
  directory, before comparing their canonical paths and exact native ID.
  Both files must still exist, be regular files and remain within that
  directory. A spelling difference is not a different retained identity.
  The idle-handle assertion independently resolves the expected canonical
  filename and retains its no-prompt and nonsemantic idle-cleanup assertions.
- F2: `validateRuntimeMetadata` accepts the separate `omp` harness kind on
  `SessionRegistrationRequest.Validate`'s outbound registration path.
  Registration remains distinct from capability promotion: the tested
  `18.20.0 / omp-rpc-v2 / protocol 2` projection advertises no executable
  operation, no provider access, unsupported guidance/pause and unknown usage.
  Unknown kinds, mismatched metadata and invalid capability claims remain
  rejected. The unrelated `AttachHarnessSession` admission path is unchanged.

The new Windows regression obtained real 8.3 directory aliases with
`windows.GetShortPathName`; the short names resolved to the same long retained
file. Before the production fix, both returned-short and returned-canonical
cases failed at `Open` with `OMP resume rejected: returned native identity
differs` (exit 1). The contained-file/ID/missing-file/outside-directory/nonregular
negative matrix did not depend on the host's `TEMP` spelling. Before F2's
allowlist fix, the complete registration validator and HTTP client positive
cases both failed with `harness_kind "omp" is invalid` (each command exit 1);
unknown-kind rejection passed and sent no HTTP request.

Raw fail-before records are `.tmp/gdd-0006j-r2-f1-before.jsonl`,
`.tmp/gdd-0006j-r2-f2-protocol-before.jsonl` and
`.tmp/gdd-0006j-r2-f2-control-before.jsonl`, with matching `.exit` receipts.
No source overlay or temporary reproduction program was required.

No measured runtime consumer was found via the scoped measurement/benchmark
search of `daemon/internal/harness/omp`, `daemon/internal/app` and `scripts`,
or the existing Goal 0006j evidence receipt. Native blob sizes, prices and
historical provider observations remain unchanged. This round issued no
native generation or provider request and promoted no native capability.

#### Second-round gates and counts

All commands below ran locally with Go `1.27.4 windows/amd64` and `daemon/`
as the working directory. `-json` only records test events; package selection,
`-count=1` and the requested timeout are unchanged.

| Gate | Exit | Actual result |
| --- | ---: | --- |
| `TMP="$TEMP" mise exec go@1.27.4 -- go test -list '^Test' ./internal/app/... ./internal/harness/... ./internal/protocol/...` before edits | 0 | 1,050 top-level tests inventoried; not a baseline test execution. |
| `TMP="$TEMP" mise exec go@1.27.4 -- go test -json -count=1 -timeout 600s ./internal/app/... ./internal/harness/... ./internal/protocol/...` | 0 | All eight packages passed. 1,022 top-level passes, 1,377 subtest passes, 32 top-level skips, no failures. App 124.540 s; OMP 3.662 s. |
| `TMP="$TEMP" mise exec go@1.27.4 -- go test -json -count=1 -timeout 60s ./internal/control -run '^TestRegisterSessionUnpromotedOMPBoundary$'` | 0 | One top-level test and both subtests passed. OMP sent exactly one validated HTTP registration; unknown kind sent none. |
| `mise exec go@1.27.4 -- gofmt -l internal/harness/omp/adapter.go internal/harness/omp/adapter_test.go internal/harness/omp/adapter_windows_test.go internal/protocol/protocol.go internal/protocol/registration_capabilities_test.go internal/control/client_test.go` | 0 | No unformatted files. |
| CI `daemon-windows`: `go test -count=1 -timeout 180s ./...` | Not run | This repair is uncommitted and unpublished under the explicit no-commit/no-push instruction. CI cannot test this local source; the Windows CI job remains the final oracle for the original runner-TEMP symptom. No repaired-tree CI success is claimed. |
| Runtime measurement harness | Not applicable | No measured consumer found in the scoped search; no measurement number was changed. |

Both explicit 8.3 positive subtests passed in the final gate without a skip.
The Open identity/path negative matrix and all existing tests in the requested
package gate also passed. The 32 skipped opt-in/helper tests remain unavailable
acceptance evidence; this round did not run native credentialed acceptance.

| Test-count population | Before this round | After this round |
| --- | ---: | ---: |
| Requested package gate: top-level total | 1,050 | 1,054 |
| OMP package: top-level total | 39 | 41 |
| Protocol package: top-level total | 52 | 54 |
| Other six requested packages: top-level total | 959 | 959 |
| Requested package gate: executed pass events including subtests | Not executed | 2,399 |
| Requested package gate: terminal events including skips | Not executed | 2,431 |

The additional HTTP client regression is outside the requested eight-package
population: one new top-level test with two subtests, all passing. Before counts
come from `.tmp/gdd-0006j-r2-tests-before.txt`; after counts come from
`.tmp/gdd-0006j-r2-go-gate.jsonl`. HTTP and formatting records are
`.tmp/gdd-0006j-r2-control-gate.jsonl` and `.tmp/gdd-0006j-r2-gofmt.txt`;
each final gate has a matching `.exit` receipt. These are the single-source
second-round numbers, not replacements for differently scoped historical runs.

Scoped simplification retained the existing `resumeFilename` validator because
it owns compatibility, retained-file existence and directory provenance; no
production abstraction was added. Independent read-only F1 review found no
P0-P3 defect in the repaired path or its tests. Its source review did not run
gates or cover F2. It also did not establish atomic protection against file
replacement or a new Open-level symlink/junction-escape scenario. The parent
verified F2 through the full request validator and actual HTTP client boundary.
No commit, push or history rewrite was performed.

## Operator audit, spend authorization, and native acceptance receipts

The owner authorized real relay spend on 2026-11-14 ("use LiteLLM for tests"), self-capped here at
US$2 per goal. The cap is compiled into the acceptance gate (`nativeAcceptancePermittedSpendMicrousd
= 2_000_000`); generation still needs an explicit positive `SYMMETRY_OMP_OWNER_SPEND_MICROUSD` that
cannot exceed it, and `TestNativeAcceptancePromptSpendDecision` asserts both the refused forms (empty,
zero, negative, non-numeric, overflow, over-cap) and the accepted boundary values.

An operator audit of every kind-enumerating site on `main` (daemon `config.go`, `app.go`,
`control/validation.go`, `protocol.go`; Control `goals.ex`, `goals/schemas.ex`, `orchestration.ex`
twice, `orchestration/schemas.ex`) found one real omission on this branch: `validateGoalSessionAttach`
rejected `harness_kind "omp"` in `daemon/internal/control/validation.go`. Fixed with
`validation_harness_kind_test.go`; removing the fix makes exactly the `omp` subtest fail
(`goal_session_attach with harness_kind "omp" rejected: harness_kind "omp" is invalid`). The same audit
leaves one open decision: `linuxNativeRuntimeRequiresGoalAdmission` still names only pi and opencode,
while this branch ORs OMP into the admission requirement at one call site only.

The first real acceptance run exposed a second defect in this branch's R2 repair: the lost-ack helper
is a separate process that re-derives its own configuration, but `nativeAcceptanceHelperEnvironment`
did not forward `SYMMETRY_OMP_OWNER_SPEND_MICROUSD`, so the helper skipped instead of reaching its
required exit 73. Fixed in `native_acceptance_test.go` with `native_acceptance_helper_env_test.go`;
removing the forwarding reproduces `helper subprocess cannot authorize the spend it was started with:
explicit positive SYMMETRY_OMP_OWNER_SPEND_MICROUSD authorization is required`.

### Windows native acceptance (real provider turns)

`TestNativeAcceptance` passes on Windows in 28.44 s against `omp.exe` 18.20.0 (251,502,592 bytes) and
the approved relay model `tencent-litellm/deepseek-v4.1-flash-fast`; sanitized receipts are under
`.tmp/omp-native-acceptance-windows/windows-omp-native-4078422222`.

| Class | Receipt record |
| --- | --- |
| Approved-catalog provenance | `approved_catalog_provenance`: credential referenced as `TENCENT_GLM_API_KEY`, endpoint `litellm-proxy-hk…/v1`, approved model exactly once |
| Adapter restart with lost acknowledgement | `adapter_restart_lost_ack`: `helper_exit 73`, `durable_completion_reconciled true`, `old_turn_replayed false`, real native handle, `native_version 18.20.0` |
| Retained resume | `retained_resume`: `retained_context_challenge passed`, `same_id_and_file true`, `prior_artifact_unchanged true` |
| Fresh handoff | `fresh_handoff` with its own, non-reused native identity |
| In-flight cancellation | `cancel_tool_live_before_control`, `cancel_after_native_tool_start`, `cancel_tool_exact_identity_terminated`, `cancel_no_later_mutation` |

Reproduction facts that matter: the fixture repository must be small — Subject derivation launches one
Git process per blob under a 30 s deadline, so the 1,033-blob worktree needed about 79 s and failed
there. The approved clone (`Q:/repos/fixture-0006h`, 1 blob, HEAD `6adc9ef`) is used instead. The
isolated agent directory (`.tmp/omp-acceptance-agent/models.yml`) is a credential-free JSON catalog
that references the key by environment-variable name, so no credential is written to disk. Linux
receipts for the same four classes are pending on this revision.
