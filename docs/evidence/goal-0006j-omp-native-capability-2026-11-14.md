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
