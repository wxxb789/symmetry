# Goal 0006b convergence-script simplification review

Date: 2026-09-29  
Scope: read-only review of `scripts/ci/linux-v2-source-subject.py`, `scripts/ci/test-linux-v2-source-subject.py`, `scripts/ci/linux-v2-native-tools.sh`, `scripts/ci/assert-go-test-json.py`, `scripts/ci/test-linux-v2-go-events.py`, `scripts/ci/export-linux-v2-redacted-evidence.py`, and `scripts/ci/test-linux-v2-evidence.py`. The concurrently changing workflow and native/rootless orchestrators were inspected only to identify callers; this review does not approve their final wiring.

## Decision

Do not declare Goal 0006b native acceptance from this review. The checkout is dirty, the qualified installed A/B run was not executed here, and the convergence contract still requires one clean exact commit on the approved amd64 systemd host with protected provider inputs. WSL2 and rootless results remain refusal evidence only.

The source-subject helper, fixed native-tool acquisition helper, and shared Go event gate are justified consolidations. The legacy exporter’s explicit allowlists and projectors are also justified: they are the boundary that removes raw journals, paths, IDs, logs, errors, notes, and credentials while retaining physical-stop and replay facts. Replacing them with a generic recursive schema/redaction layer, copying raw producer objects, or weakening redaction would make the safety argument worse.

The initial review found one blocking correctness issue in the legacy export completion decision. The focused re-review below confirms that issue is resolved. No broad refactor is recommended, and no unresolved finding remains in this reviewed script slice.

## Findings, ordered by impact

### 1. Resolved (initially blocking): a `passed` legacy export could call empty proof collections complete

#### Initial finding

`project_legacy_document` required the `journal_snapshots` and `process_snapshots` members and permitted either a list or `null`, but the completion check marked only `null` incomplete. Therefore `[]` was treated as complete. A direct probe against that version of the projector successfully projected all 16 legacy fixture families after replacing both collections with empty lists. The same probe showed that `project_receipt({})`, `project_owner({})`, and `project_process({})` returned `{}` rather than rejecting an incomplete producer shape.

This mattered because the public legacy artifact is intended to retain meaningful physical-stop/replay evidence, not merely safe JSON. A manifest with `status: "passed"`, `complete: true`, 16 files, and no journal or process observations could satisfy the old exporter. The Go event gate proves that named tests ran and passed, but it does not by itself prove that exported summaries retain named observations after a producer/schema regression.

The smallest correction requested by the initial review was:

1. Treat an empty `journal_snapshots` or `process_snapshots` list exactly like `null`: append that target to `incomplete_evidence_files`, force a legacy manifest to `failed`/`complete: false`, and reject a requested successful export.
2. In the existing explicit projector functions, require the fields that current Go structs emit without `omitempty`, without adding a generic schema framework: the physical receipt, owner identity, process observation, barrier receipt, and journal state fields listed by the initial review.
3. Add focused cases for empty lists and truncated nested receipt/owner objects, asserting that no passed manifest is produced.

#### Resolution re-review

The focused exporter change implements that correction directly:

- `export-linux-v2-redacted-evidence.py:733` now uses `if not projected["journal_snapshots"] or not projected["process_snapshots"]`, so both `null` and `[]` add the target to `incomplete_evidence_files`.
- The existing manifest path derives `incomplete`, forces a legacy status to `failed`, writes `complete: false`, and returns nonzero when the requested run status was `passed` (`:761-784`). It does not fabricate or delete a physical-stop fact.
- `project_receipt`, `project_owner`, `project_journal`, `project_process`, and `project_barrier` now explicitly reject missing current-producer non-`omitempty` fields before projection (`:286-427`). The required sets match the current Go producer shapes inspected in Finding 2; no compatibility was added for stale raw owner fields.
- `test-linux-v2-evidence.py` now covers empty proof lists, null proof lists, and truncated nested receipt/owner fields. Empty/null inputs must produce a failed, incomplete manifest and nonzero exit for a requested passed run; truncated nested proof inputs must be rejected without any passed manifest.
- The worker inspected the successful prepare/no-target producer path and confirmed successful cases append both journal and process snapshots, so the strengthened completeness condition does not reject that valid shape.

The pre-patch red run failed the exact three new assertions. The exporter worker then ran all 10 focused tests successfully in 6.625 seconds. The parent independently inspected the resulting code and ran the same 10 tests on Ubuntu under WSL in 5.472 seconds with exit 0. On code and focused-test evidence, Finding 1 is resolved.

### 2. Current producer compatibility is exact; refusal of stale evidence is deliberate and correct

The current Go legacy producer uses:

- `linuxWitnessOwnerSnapshot` with `owner_context_sha256` and `job_id_sha256` (`production_linux_containment_witness_test.go:242-251`), populated by hashing `OwnerContext` and `JobID` in both handoff and authority projections (`:3315-3344`);
- `linuxReplayReceiptSnapshot` with `owner_context_sha256`, `pipe_token_sha256`, and `job_id_sha256` (`production_linux_containment_replay_witness_test.go:132-146`);
- journal and process snapshot structs whose JSON names align with `JOURNAL_KEYS`, `PROCESS_KEYS`, `OWNER_KEYS`, `RECEIPT_KEYS`, scan keys, barrier keys, and PID-reuse keys in the exporter;
- replay metadata whose `tree_digest` is the bare `git rev-parse HEAD^{tree}` object ID, while `dirty_status_sha256` is SHA-256 (`production_linux_containment_replay_witness_test.go:67-71,148-168`). `SAFE_HASH` correctly accepts either a 40-character Git object ID or a 64-character SHA-256.

The synthetic actual-shape fixtures in `test-linux-v2-evidence.py` match these current structs, including nullable top-level slices, `omitempty` fields, nested scan progress, replay barriers, and PID-reuse details. The explicit allowlists also intentionally accept private producer members such as `raw_output`, `raw_journal_snapshots`, paths, notes, and errors only so the projector can drop them; they are not copied to public output.

The ten historical summaries under `.plans/ci3/witness-36037842424` use old raw `owner_context`/`job_id` members rather than the current hash-only members and are rejected. That refusal must remain. Old evidence cannot certify the new source revision, and adding compatibility aliases would either expose private values or falsely imply current-producer compatibility.

### 3. Source binding is necessary and is not an unnecessary duplicate digest

`linux-v2-source-subject.py` owns one clear invariant: the fixture source is exactly the expected full `HEAD`, with no staged, unstaged, or untracked changes, before it emits `{commit, tree_digest}`. It hashes committed blob contents plus mode, kind, path, and blob digest, rejects non-blob tree entries, and repeats both `HEAD` and dirty-status checks after hashing to avoid issuing a receipt for a checkout changed during the read.

The test exercises stable clean output, wrong commit, staged/unstaged/untracked dirtiness, preservation of dirty files, and a non-repository. The helper replaces duplicated inline native/rootless Git-tree logic and adds the previously missing dirty-tree refusal. Keep the second pre-export invocation in callers: the first binds bytes before effects; the second prevents a successful artifact label after a persistent checkout change.

No deletion is recommended here. The custom `tree_digest` is distinct from the legacy producer’s bare Git `tree_digest`; they occur in different evidence contracts and should not be merged by changing either representation.

Dirty-source labeling judgment: the helper emits no JSON on a wrong commit or dirty checkout, so callers cannot honestly label such a build with the requested commit if they fail closed on its exit. This review does not certify concurrent workflow/orchestrator wiring or a qualified native run.

### 4. The shared Go JSON event gate preserves the proof lost by the five inline parsers

`assert-go-test-json.py` requires each unique expected test to have exactly one `run`, one `pass`, and zero direct skips. It separately rejects skipped descendants of an expected parent, all `fail` events, malformed JSON lines, missing files, and invalid UTF-8. This prevents absent or skipped opt-in production tests from becoming green through package success alone.

The apparent duplication between the per-test `skip` count and the `skipped` descendant list is necessary: direct skips violate the exact count, while descendant skips can coexist with a parent `pass` and therefore need the prefix check. Do not collapse that into a pass-only counter. The two focused tests cover missing run/pass, duplicate run/pass, package failure, parent skip, child skip, and malformed output.

No abstraction or dependency should replace this small parser.

### 5. Fixed native-tool acquisition is appropriately narrow

`linux-v2-native-tools.sh` removes duplicate Pi/OpenCode downloads while keeping URLs, archive hashes, executable hashes, versions, retry policy, extraction layout, and version checks fixed in one function. Its four output variables are used by the two callers with different needs: native installs from the unpack roots, while rootless binds the exact executable paths. Turning this into a configurable downloader or package abstraction would weaken provenance and add owned complexity.

The caller-owned private destination and pinned archive hashes make extraction acceptable in this CI fixture. Keep archive verification before extraction and executable verification/version checks after extraction. No concrete deletion preserves the same provenance more clearly.

### 6. Destination and secret handling are conservative enough for the stated trusted-runner boundary

The exporter requires a canonical absolute, non-symlink directory owned by the target runner with mode `0700`; sources must be mode `0600`, bounded regular files, opened with no-follow where available, and unchanged in identity/size through read. Destinations outside the exact allowlist are rejected, existing destination files are refused, writes use private temporary files with `0600`, fsync, ownership assignment, and atomic replacement. Native/rootless callers additionally require an empty evidence directory before execution.

The legacy projection, rather than a blacklist-only copy, is what prevents leakage when no credential-values file is supplied by the legacy workflow. Dynamic raw journals, output, notes, errors, IDs, and paths are dropped; owner context, job ID, pipe token, source, and binaries are represented by hashes where intended. Preserve the forbidden-key scan and optional exact-secret scan as defense in depth.

I do not recommend adding a generic destination/filesystem abstraction. A hostile same-runner process could mutate artifacts after export regardless of directory-FD pinning; that is outside this trusted CI-runner boundary and is not solved by more local wrapper code. The current no-follow and atomic-write checks are meaningful and should remain.

## Unnecessary-abstraction judgment

No material unnecessary abstraction was found in the reviewed change. In particular:

- the source helper owns exact clean-subject binding previously duplicated in two callers;
- the native-tool helper owns one fixed provenance policy previously duplicated in two callers;
- the event gate owns exact run/pass/no-skip semantics previously duplicated across five parsers;
- the legacy projectors own field-by-field declassification of credential-bearing producer documents.

Minor syntactic reductions such as removing `json.JSONDecodeError` from exception tuples that already contain `ValueError`, changing the one-element `for key in ("signaled",)` loop to an `if`, or removing `None` from the `expected` path type annotation would save only a few lines and would not reduce state ownership or risk. They are not worth a standalone change. Do not merge receipt and owner projection into a generic recursive projector: their required fields and meaning differ.

## Verification and limitations

Executed on the current Windows workstation:

- `python scripts/ci/test-linux-v2-source-subject.py` - 4 tests passed.
- `python scripts/ci/test-linux-v2-go-events.py` - 2 tests passed.
- `python scripts/ci/test-linux-v2-evidence.py` - not runnable on Windows because the Linux-only exporter calls `os.getuid`, `os.getgid`, and `os.geteuid`; all seven local cases consequently failed before exercising exporter behavior. This is a host limitation, not evidence of a Linux CI regression.
- Initial direct read-only projector probe - all 16 fixtures were accepted with empty journal/process lists; empty receipt, owner, and process objects were also accepted. This established the original Finding 1 without altering repository files.
- Focused fix verification supplied for the re-review - the three new assertions failed before the patch; the exporter worker then passed all 10 focused tests in 6.625 seconds; the parent independently inspected the code and passed the same 10 tests on Ubuntu under WSL in 5.472 seconds with exit 0.
- This re-review did not rerun the suite; it inspected only the changed completeness checks and focused tests as requested.

The original seven-test Linux run remains historical evidence for the pre-expansion suite; the current focused suite has 10 passing tests as recorded above. No shared full test suite, native A/B fixture, download, build, workflow, or acceptance run was executed for this review. Final CI source/artifact integration was checked separately by the parent and is outside this scoped re-review. No original native acceptance is approved, and Goal 0006b remains active.
