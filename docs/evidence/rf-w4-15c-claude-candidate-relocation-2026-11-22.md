# rf-w4-15c Claude candidate relocation — evidence record (2026-11-22, implement stage)

Contract: `goal-rf-w4-15c-claude-candidate-relocation.md` (GDD state root). Plan:
`docs/plans/2026-11-22-rf-w4-15c-claude-candidate-relocation-plan.md`.
Base revision: `4fdea989a698a96eb01d90a8729e6e8a54457883` (W4-15a merged; `#187`/W3-04 and W3-15 in
place). Branch: `refactor/rf-w4-15c-claude-candidate-relocation`. Every command in this record ran
in `daemon/` unless its row says otherwise, on the Windows host with Go 1.27.4 (WSL Ubuntu-24.04
runs Go 1.27.0) and `golangci-lint` 2.15.0.

The increment was authored in three stages on one integration branch: source relocation (root
`package harness` → `claude/adapter.go` + `claude/probe.go`, `PermanentCloseFailure`, registry trim,
`app/probe.go` requalification), test relocation (ten moved/split/retargeted test files, shadow-only
cases deleted), and this evidence pass (gates, mutants, records). No file was committed.

## 1. Subject bytes

Source files at this revision (sha256):

| File | sha256 |
| --- | --- |
| `daemon/internal/harness/claude/adapter.go` | `558ba2d99e14faf7d454815b0df809a3e46772f47d9df0071f6646255ac185ca` |
| `daemon/internal/harness/claude/probe.go` | `a9582c9b73e060252048f3e44acf58ef64634a2e29deb3426cdb1d77edfa2585` |
| `daemon/internal/harness/registry.go` | `efd6473a769bdb154637388734924ca9c43f7be6aad233c9a1e4fdb0bf6b3c11` |
| `daemon/internal/harness/close_gate.go` | `e171bb82571abbfdb364b72ecb43e2f50e0b95fa45002d34955a49d8e2b83081` |
| `daemon/internal/app/probe.go` | `73b42c9a4f50e0f187f8a2b35a7525c158d6ee3079f54ec4db482441d33535ef` |

Unchanged by this slice, re-hashed to bind the claim: `daemon/internal/harness/codex_probe.go`
`a34e04e2e7888f3cb5b3050d0fc55d0fb9343baf0c29f11891e51be99906bbf1`,
`daemon/internal/harness/bounded_probe.go`
`0e8038e87c0f0a6885ad8ebd13fa6ad32de867e4c3bf0940dd500ef9a95f05a7`,
`daemon/internal/harness/claude/stream.go`
`8c3267ec8ecf1d1eaf74a016c6849abd51748827a3388ef791caf4de6602145a` (identical to the base
`registry.go`/`claude_probe.go`/adapter hash snapshot taken before the first edit; `codex_probe.go`
and `bounded_probe.go` were never opened — every mutant that touched them ran in scratch copies,
§9).

Base bytes for the moved code (sha256, from the base worktree before any edit):
`claude_candidate_adapter.go` `a98560d4e13f14eac01f6492c7cac4ba67c408dbf7f5fb215b14db5d69d9bc31`,
`claude_probe.go` `e50b5c7cb7f33a9c8ea6ab66fdd9e8f08acd448f5a30daea90f464ec36c9957e`,
`registry.go` `47b49ec408d59ec70cca1a5ac05d151afe75269a85c62a10299f1acfbb0fc7b2`.

Test-file hashes of the final bytes: §8.

## 2. Identifier move table (old → new, call sites after the move)

| Base (root `harness`) | `package claude` | Call sites at HEAD |
| --- | --- | --- |
| `ClaudeCandidateAdapter` | `CandidateAdapter` | `app/probe.go` constructor, `app/harness_test.go` (type assertion + row), `claude/adapter_live_test.go`, `claude/adapter_native_test.go`, `claude/adapter_cmdline_windows_test.go`, `claude/adapter_structured_output_test.go` |
| `NewClaudeCandidateAdapter` | `NewCandidateAdapter` | `daemon/internal/app/probe.go:104`, `daemon/internal/app/harness_test.go:112` and `:299` (composed-probe test + claude row), moved adapter tests, `claude/probe_test.go` |
| `ClaudeCommandRunner` | `CommandRunner` | `claude/probe.go`, `claude/probe_test.go` |
| `osClaudeCommandRunner` | `osCommandRunner` | `claude/probe.go`, `claude/adapter.go` (Probe), `claude/adapter_live_test.go` |
| `defaultClaudeExecutable` | `defaultExecutable` | `claude/probe.go`, `NewCandidateAdapter` |
| `claudeProbeTimeout` | `probeTimeout` | `probeNativeEvidence` |
| `claudeProbeMaxOutputBytes` | `probeOutputLimitBytes` | `osCommandRunner.Run` |
| `ClaudeTestedVersion` | `TestedVersion` | `claude/probe.go`, moved tests |
| `ClaudeTestedExecutableHash` | `TestedExecutableHash` | `claude/probe.go`, moved tests |
| `claudeImplementationVersion` | `implementationVersion` | `capabilitiesForPlatform`, `claude/probe_test.go` |
| `claudeProbeEvidence` | `probeEvidence` | `claude/probe.go`, `claude/probe_test.go` |
| `claudeVersionPattern`, `parseClaudeVersion`, `hasClaudeTransportHelp` | `versionPattern`, `parseVersion`, `hasTransportHelp` | `claude/probe.go`, `claude/probe_test.go` |
| `probeClaudeNativeEvidence` | `probeNativeEvidence` | `claude/probe.go`, `claude/probe_test.go` |
| `claudeExecutableDigest` | `executableDigest` | `probeCapabilities` |
| `rejectClaude` | `reject` | `capabilitiesForPlatform` |
| `claudeCapabilitiesForPlatform` | `capabilitiesForPlatform` | `probeCapabilities`, `claude/probe_test.go` |
| `claudeEvidenceCapabilities` | `evidenceCapabilities` | `probeCapabilities`, `claude/probe_test.go` |
| `probeClaudeAdapterCapabilities` | `probeCapabilities` | `CandidateAdapter.Probe`, `claude/probe_test.go` |
| `claudeCandidate*` (25 adapter-local names: session, process seam, constants, frames, helpers, the seven `errClaudeCandidate*` sentinels) | `candidate*` / `errCandidate*` | `claude/adapter.go`, the moved adapter tests |

Two compiler-forced consequences of the move, both equivalence-checked:

- The adapter-local `isClaudeCandidateJSONObject` helper is gone; `StartTurn` calls the existing
  `claude/stream.go` `isJSONObject`. The two predicates are the same function of their input:
  the base required non-empty trimmed input starting with `{` **and** a successful unmarshal into
  `map[string]json.RawMessage` with a non-nil map; the stream helper requires non-empty input, the
  same successful unmarshal and the same non-nil map. A non-empty JSON text unmarshals into a
  non-nil map only when it is an object (`null` leaves the map nil, arrays/scalars error), so the
  leading-`{` test is implied and implying; the full literal multiset delta (`""` −1, `"{"` −1) is
  in §7. `claude/stream.go` is out of this slice's owned set, which is why the adapter adopted the
  existing helper instead of exporting a new one.
- The root `controlCapability` helper is unexported in `generic.go` (also out of the owned set), so
  `claude/adapter.go` carries a package-local copy with the switch body byte-identical to root's,
  including its `default: return harness.Capability(kind)` arm. The `omo` adapter's variant, whose
  default returns `CapabilityCancel`, was deliberately not copied.

## 3. Deleted declarations and the kept root surface

Deleted from root (`registry.go` base lines; "last caller" = last non-test caller, from the
full-tree grep at the base revision):

| Deleted | Base line | Last callers |
| --- | --- | --- |
| `NewClaudeAdapter` | 167 | `NewRegistry` (`:86`) |
| `NewClaudeAdapterWithExecutable` | 175 | `NewClaudeAdapter` (`:168`) |
| `NewClaudeAdapterWithRunner` | 181 | `app/harness_test.go:306` (test) |
| `newClaudeAdapterWithRunner` | 185 | the two constructors above |
| `probeClaudeExecutable` | 193 | `newClaudeAdapterWithRunner` (`:188`); `claude_probe_test.go` (8 cases) |
| `NewPiAdapter` | 207 | `NewRegistry` (`:87`), `app/harness_test.go:307` (test) |
| `NewCodexAdapter` | 259 | `NewRegistry` (`:85`) |
| `NewCodexAdapterWithRunner` | 265 | `harness_test.go:58` (test) |
| `probeCodexExecutableWithRunner` | 273 | `NewCodexAdapterWithRunner` (`:268`); `codex_probe_test.go` (6 cases) |
| `ClaudeCommandRunner`, `osClaudeCommandRunner` | 36, 40 | moved to `claude/probe.go` (not deleted) |
| `CodexCommandRunner`, `osCodexCommandRunner` | 52, 56 | `NewCodexAdapterWithRunner`, `probeCodexExecutableWithRunner` |
| `defaultCodexExecutable`, `testedCodexVersion`, `codexProbeTimeout`, `codexProbeMaxOutputBytes` | 27-30 | `osCodexCommandRunner`, `probeCodexExecutableWithRunner`, `codex_probe_test.go` |
| `defaultClaudeExecutable`, `claudeProbeTimeout`, `claudeProbeMaxOutputBytes` | 16, 24, 25 | moved to `claude/probe.go` (not deleted) |

Root test declarations deleted with their last callers: `unavailableProbeRunner`
(`harness_test.go:24`; callers `:58`,`:61`), `knownVersionProjection` (`harness_test.go:593`) and
`assertKnownVersionProjection` (`:606`) (callers `claude_probe_test.go:26`, `codex_probe_test.go:71`),
`sameStrings` (`claude_probe_test.go:278`; callers the same two files), `codexFixtureRunner` /
`codexFixtureResult` (`codex_probe_test.go:194`, `:233`; callers the six deleted cases),
`absentClaudeRunner` (`app/harness_test.go:292`; caller the claude row).

Kept in root and proven still used: `Registry`, `NewRegistry` (Generic + OpenCode only), `Register`,
`Lookup`, `Probe`, `UnavailableAdapter` (OpenCode is its production user), `newUnavailableAdapter`,
`NewOpenCodeAdapter`, `AvailabilityError`, `RunBoundedProbe`, `RunProbeCommand`, `CodexSchemaDigest`,
`CommandInvoker`, `ResolveExecutableDigest`, `HashExecutableFile`, `CodexTestedVersion` /
`CodexTestedSchemaHash` / `CodexTestedExecutableHash` (consumed by `codex/probe.go:20-22`),
`ValidateCodexProbeEvidence` / `ValidateCodexVersionOutput` / `ParseCodexVersion` /
`HasCodexStdioAppServerHelp` / `ValidateCodexHelpOutput` (consumed by `codex/probe.go:144`, `:164`,
`:182`, `:280`),
`DeliverEvent`, `IsNilInterface`, `WaitTurnBarrier`, `WaitResult`, `CloneTaskResult` /
`CloneTaskResultPointer`, `ProcessForwarder` / `NewProcessForwarder`, `CloseGate` and the
`types.go` interfaces.

`NewRegistry` at HEAD:

```go
registry := &Registry{adapters: make(map[Kind]Adapter)}
_ = registry.Register(KindGeneric, NewGenericAdapter())
_ = registry.Register(KindOpenCode, NewOpenCodeAdapter())
return registry
```

Production composition `daemon/internal/app/probe.go` (unchanged except the import and the Claude
constructor path) still registers Codex, Claude, Pi, OMP and OmO over that registry, so the
registered-adapter set the daemon sees — Generic, Codex, Claude, Pi, OMP, OmO, OpenCode — and every
projection are unchanged.

## 4. Import graph

`cd daemon && go list -deps ./internal/harness | grep 'internal/harness/'`:

| Revision | Output |
| --- | --- |
| base `4fdea989` | `…/internal/harness/linejson`, `…/internal/harness/claude` |
| HEAD | *(no output; grep exit 1)* |

The base's `linejson` line was transitive (`claude/stream.go` imports it) through the very
`harness/claude` import this slice removes; root files import no harness subpackage at HEAD, which
satisfies the contract's "no harness subpackage except `linejson`" as an empty set. `harness/claude`
imports root exactly like the other five adapter packages.

## 5. Close permanence

`close_gate.go` gains (the only new comment in the slice):

```go
// PermanentCloseFailure builds the bare marker a close attempt returns when its
// failure must stay sticky. A wrapped marker stays retryable.
func PermanentCloseFailure(err error) error { return permanentCloseFailure{Err: err} }
```

`finish` keeps its bare type assertion and `permanentCloseFailure` keeps its name, methods and
documented contract, so the unowned `session_scaffolding_test.go` cases (including
`TestCloseGateTreatsAWrappedPermanentFailureAsRetryable`) stay byte-identical and green. The moved
adapter's `Close` calls `harness.PermanentCloseFailure(err)`.

## 6. Base→HEAD control-flow audit (conductor decision 2026-11-21, lesson 32)

Method: `git show 4fdea989:daemon/internal/harness/claude_candidate_adapter.go` and
`claude/adapter.go` are sliced per function by brace matching, comments and blank lines removed, the
§2 identifier map applied, then diffed. The audit script is a throwaway outside the repository.

| Function | Verdict |
| --- | --- |
| `(*CandidateAdapter).Probe` | identical |
| `(*CandidateAdapter).Start` | identical |
| `(*candidateSession).Close` | identical once the intended substitution is normalized; in strict mode the only difference across all four functions is the single line `permanentCloseFailure{Err: err}` → `PermanentCloseFailure(err)` |
| `(*candidateSession).closeOnce` | identical |

Strict-mode diff, verbatim:

```
DIFFERS    func (session *claudeCandidateSession) Close(
   --- base
   +++ head
   @@ -15,3 +15,3 @@
    		default:
   -			return permanentCloseFailure{Err: err}
   +			return PermanentCloseFailure(err)
    		}
   IDENTICAL  func (session *claudeCandidateSession) closeOnce(
```

No early return, no retryability decision, no lock or channel behaviour changed by the move.

## 7. Error-string byte-compare

Error/log/status literals (`errors.New(`, `fmt.Errorf(`, `Message:`, `Reason:`, `Summary:`,
`Unsupported[<cap>] =`) over the moved code — base
=`claude_candidate_adapter.go` + `claude_probe.go`, HEAD=`claude/adapter.go` + `claude/probe.go`:

| Revision | Occurrences | Distinct | Multiset delta |
| --- | --- | --- | --- |
| base | 12 | 11 | — |
| HEAD | 12 | 11 | none (0 missing, 0 extra) |

The eleven literals (each byte-identical on both sides): `"Claude Code native adapter has no verified
provider broker bridge"`, `"Claude Code native adapter has no verified provider-enforced hard cost
limit"`, `"Claude Code native adapter has no verified resume entry"`, `"Claude Code native adapter
control is unverified for this operation"`, `"Claude emitted stderr"`, `"Claude process termination
completed"`, `"Claude process was cancelled"`, `"Claude terminal observation failed; cancellation
rejected"`, `"Claude terminal was already observed; cancellation rejected"`, `"Claude turn is not
active"` (×2), `"Claude wait cancelled"`. Package sentinels (`errors.New("…")`, distinct/occurrences):
base 31/38, HEAD 31/38, sets equal.

Full `"…"` literal multiset over the same files plus the moved `registry.go` block, with every delta
explained (none is an error or status text):

| Delta | Literal | Cause |
| --- | --- | --- |
| −1 | `"{"`, `""` | the adapter-local `isClaudeCandidateJSONObject` helper (replaced by `claude/stream.go`'s `isJSONObject`) |
| −1 | `"Claude Code native session lifecycle is unverified"` | deleted shadow constructor; the identical literal survives in `evidenceCapabilities` |
| −1 | `"codex"` | deleted `defaultCodexExecutable` (shadow-only) |
| −1 | `"…/internal/harness/claude"` | the `claudeprotocol` import alias (same package now) |
| +1 | `"…/internal/execution"` | `claude/probe.go` owns the `osCommandRunner` |
| +2 | `"…/internal/harness"` | the new root import in `adapter.go` and `probe.go` |
| +1 | `"time"` | the moved `probeTimeout` constant |

## 8. Test moves, retargets and deletions

Moved files (bodies, names and comments verbatim; only the package clause, imports, identifier
qualification and the `../testdata/…` prefix differ):

| Root file (base lines) | HEAD file(s) | Lines at HEAD |
| --- | --- | --- |
| `claude_candidate_adapter_test.go` (1,715) | `claude/adapter_test.go` (lines 1-1165) + `claude/adapter_support_test.go` (1166-1715) | 1,164 + 570 |
| `claude_candidate_additional_test.go` (290) | `claude/adapter_additional_test.go` | 290 |
| `claude_candidate_cmdline_windows_test.go` (31) | `claude/adapter_cmdline_windows_test.go` | 31 |
| `claude_candidate_live_test.go` (689) | `claude/adapter_live_test.go` | 690 |
| `claude_candidate_native_test.go` (281) | `claude/adapter_native_test.go` | 282 |
| `claude_candidate_structured_output_test.go` (99) | `claude/adapter_structured_output_test.go` | 100 |
| the promotion half of `claude_probe_test.go` + the eight probe cases | `claude/probe_test.go` | 490 |
| trimmed `claude_probe_test.go` | `TestBoundedProbeRejectsSuccessAfterDeadline` only | 21 |

Final hashes of the moved/edited test files (sha256):
`claude/adapter_test.go` `6ee75b8e1bfd6737a41a59343a916bc1c78aa27a54eada82554eca8b98eb8a40`,
`claude/adapter_support_test.go` `a56236cb4cf2628b6495769a69c7e7fafdb19df1b8720910f907815750090309`,
`claude/adapter_additional_test.go` `b0322bd2d4813c6df44e32520e5b05ec64e0de2ec38a2f5dd34a5f1f26b784b6`,
`claude/adapter_cmdline_windows_test.go` `6e9eb931bc9001c4b3204ba2312c590c201a3bde34465e4f9d55c57ba081c889`,
`claude/adapter_live_test.go` `f45c8377c34ac0a59b647be5b9028c4662be91c605e14f0607cf409059bddacc`,
`claude/adapter_native_test.go` `682ffd69d880d5cd8602f2c4046c552cc003aad004e8e975468813c2dac5ccb4`,
`claude/adapter_structured_output_test.go` `070faa821d6bd1a8c7220dcd4ffd8b6074036605695901f9d6933b50ce880250`,
`claude/probe_test.go` `c71e1d9808b5f9f0f38d3478f301fda6a2b18bc7ed533c36bb30dc87da09f868`,
`claude/stream_test.go` `9c2ec2fcfa5814ad147e13438f8d423b2891bd2b4a3d222d7299535fe67f5616` (unchanged),
`harness_test.go` `3162f8f5431ece5fdcd9037375a14be423ff2323d2a6161476d1aed3f9c732f1`,
`codex_probe_test.go` `5a6c5130be4a033c81131b0ccbd9a20a6e646f62ac649819268f5ba500cb296c`,
`claude_probe_test.go` `8864f434e14fbdd365042033bc43c3be4c3f6b26ca94459e40523310a9ba698a`,
`app/harness_test.go` `c3347d5d318e2b7e78732f3d865f978ea493449ae1323a964f3ef15a327d54fc`.

Moved-test verbatim audit: every top-level function of the moved files was extracted from the base
and HEAD bytes, comments/blank lines removed, the rename map and identifier qualification applied,
and compared token-for-token: 110 functions identical, 2 differing, 0 missing.

- `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` — the §8 retarget.
- `TestClaudeProbeKnownVersionAndHelpFailsClosed` — one fixture argument, scrubbed from the base's
  `C:\Users\<profile>\.local\bin\claude.exe` to `C:\Users\runneradmin\…` under the conductor's
  program-wide rule that no committed file carries a host path with a user profile name. The value
  is consumed only as the `executable` argument of the fixture runner, which keys its responses on
  the argv (`--version`/`--help`), so the assertion is unchanged; the same shell-shaped value
  (`C:\Users\runneradmin\…`) already exists in the base's Windows cmdline test.

`assertKnownVersionProjection` is the one intentionally copied helper (its base original lives in
root `harness_test.go`, which is not part of the moved set).

Test-only helpers with no cross-package route were copied into the moved package:
`testHangGuard` (declared once, in `claude/adapter_support_test.go`), `sameStrings`,
`knownVersionProjection` and `assertKnownVersionProjection` (in `claude/probe_test.go`). Root
`harness_test.go` keeps `testHangGuard` for `bounded_probe_test.go` and
`session_scaffolding_test.go`.

Retargeted cases (same test names, same fixtures, same assertions; only the call target and, where
noted, the sentinel that the new production path carries):

| Case | HEAD subject | Reason |
| --- | --- | --- |
| `TestClaudeProbeKnownVersionAndHelpFailsClosed` | `probeCapabilities` | the fail-closed tail now lives in `capabilitiesForPlatform`'s reject paths; pinning an unverified blob makes `ErrNativeUnverified` platform-independent |
| `TestClaudeProbeHelpWithoutJSONSchemaFailsClosed` | `probeCapabilities` | the help gate moved with the probe |
| `TestClaudeProbeUnknownVersionFailsClosed` | `probeCapabilities` | the version gate moved with the probe |
| `TestClaudeProbeAbsentExecutableIsUnavailable` | `probeCapabilities` | the availability projection moved with the probe |
| `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` | `adapter.Probe` over the same fixture + `Start` refusal | the composed adapter's `Start` validates the request before the app-level gate; the sentinel assertion is preserved on `Probe`, and the app composition's refusal is pinned by the app suite (§10) |
| `TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation` | `probeCapabilities` | unchanged timing path |
| `TestClaudeProbeRejectsSuccessAfterProbeDeadline` | `probeCapabilities` | the post-bound check is in the shared probe helper |
| `TestClaudeProbeRejectsNilContextBeforeTheSharedGuard` | `probeCapabilities` | the entry-level guard moved with the probe |
| `TestClaudeProbeDoesNotParseFailedCommandOutput` | `probeCapabilities` | the failed-output branch moved with the probe |
| `TestProbeRejectsNonExactVersionOutput` | `ValidateCodexVersionOutput` only | its primary subject is a kept root export; the deleted registry-seam half is pinned by `codex/probe_test.go`'s pre-existing `TestProbeUnknownVersionFailsClosed` (version-only call list) |
| `TestRegistryKeepsUnavailableCapabilitiesExplicit` codex/claude/pi rows | removed | root cannot compose native adapter packages (import cycle); the equivalent rows are `TestStartupProbeRegistersUnavailableNativeProjections` (claude, pi retargeted; omp, omo pre-existing) and the kept OpenCode row |
| `TestStartupProbeRegistersUnavailableNativeProjections` claude/pi rows | `claude.NewCandidateAdapter(<absent>)` / `pi.NewAdapter(<absent>)` | same row pattern as the pre-existing omp/omo rows; every assertion unchanged |

Deleted cases (subject deleted, with the reason and the mutant record in §9):

| Deleted | Reason |
| --- | --- |
| `TestCodexProbeAcceptsWarningPrefixedVersionOutput` | subject is the deleted registry seam; the warning-tolerant parse it re-asserted is pinned by the retained `TestCodexParseVersionToleratesBoundedLeadingWarnings` |
| `TestCodexProbeKnownVersionHelpAndSchemaFailsClosed` | subject is the deleted seam's fail-closed tail; production codex probing is pinned by `codex/probe_test.go` |
| `TestCodexProbeSchemaMismatchFailsClosed` | subject is the deleted seam; `ValidateCodexProbeEvidence` is consumed by `codex/probe.go` and pinned by the pre-existing `TestProbeSchemaMismatchFailsClosedAsUnsupportedVersion` |
| `TestCodexProbeRejectsNilContextWithoutCallingTheRunner` | subject is the deleted seam; its observable pin (no panic, no runner call, no promoted evidence) is the shared guard, pinned by the pre-existing `TestRunBoundedProbeRejectsNilContext`. Gap recorded in §12: `codex.Probe`'s own guard text has no HEAD test |
| `TestCodexProbeDoesNotParseFailedCommandOutput` | subject is the deleted seam; the production equivalent is pinned by the pre-existing `TestProbeMapsBoundedCommandErrorsToFailClosedResults` |

Test-name before/after (`go test -list '.*'`, base list captured before the first edit; the HEAD list
in §10): every base test name except the five deleted ones is present exactly once, and the retargeted
names keep their spellings; no name was added.

The 60 moved names, as the intersection of the base root-`harness` name set and the HEAD
`internal/harness/claude` name set (each appears exactly once in the HEAD list; the base list holds
each once too):

```
TestClaudeAdapterProbePromotesOnlyTheResolvedTestedBlob,
TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence,
TestClaudeCandidateAcceptsAdmittedModelProfile,
TestClaudeCandidateAcceptsVerifiedTaskResultAndPublishesEvent,
TestClaudeCandidateCancelBeforeDeferredInitSettlesCancelled,
TestClaudeCandidateCancelFailureRemainsStableWithoutRepeatedTerminate,
TestClaudeCandidateCancelFenceSupersedesInvalidTerminal,
TestClaudeCandidateCancellationFenceOutranksInFlightTerminal,
TestClaudeCandidateCancellationOwnsFenceRejectedDelivery,
TestClaudeCandidateCancellationOwnsIncompleteTrailingRecord,
TestClaudeCandidateCancellationUsesBoundedTerminate,
TestClaudeCandidateCancelledControlContextDoesNotAcknowledgeFailure,
TestClaudeCandidateCancelledResultRequiresValidTerminationEvidence,
TestClaudeCandidateCloseAfterTerminalObservationKeepsCancelRejected,
TestClaudeCandidateCloseFailureIsStableWithoutReterminating,
TestClaudeCandidateDirectCloseBeforeDeferredInitSettlesCancelled,
TestClaudeCandidateDirectClosePreservesPendingTerminalOutcome,
TestClaudeCandidateDoesNotReplayPreReadyOutputAfterStartError,
TestClaudeCandidateEventSinkFailureAfterTerminalIsNotSuccess,
TestClaudeCandidateIncompleteTrailingRecordAfterTerminalIsNotSuccess,
TestClaudeCandidateLateCancelAfterTerminalObservationIsRejected,
TestClaudeCandidateLiveCancelSmoke, TestClaudeCandidateLiveProbePromotesTheTestedBlob,
TestClaudeCandidateLiveProbeRejectsDifferentBlob, TestClaudeCandidateLiveRepositoryTurnSmoke,
TestClaudeCandidateMalformedRecordPreservesDiagnosticSinkFailure,
TestClaudeCandidateNormalCloseRejectedDeliveryStillFailsClosed,
TestClaudeCandidateOpenAndCloseAreIdempotent,
TestClaudeCandidateOpenDefersSystemInitUntilFirstTurn,
TestClaudeCandidateProcessExitAfterTerminalBindsFinalOutcome,
TestClaudeCandidateRejectsDuplicateAndLateRecordsAfterTerminal,
TestClaudeCandidateRejectsInvalidStructuredOutputTaskResult,
TestClaudeCandidateRejectsKindReasonMismatchOutsideToolSchema,
TestClaudeCandidateRejectsProseTerminalAsSuccess,
TestClaudeCandidateRejectsSupersededAndClosedTurns,
TestClaudeCandidateRetainsProcessOwnerWhenStartReportsError,
TestClaudeCandidateStagesFreshTransportAndBarriers,
TestClaudeCandidateStartForwardsPersistProcessWithAuthority,
TestClaudeCandidateStartTurnCloseRaceCancelsPromptWrite,
TestClaudeCandidateStructuredOutputSchemaHasNoTopLevelCombinator,
TestClaudeCandidateSupersededResultDoesNotPoisonLaterRecords,
TestClaudeCandidateTerminalValidationFailureRejectsLateCancel,
TestClaudeCandidateTruncationWithoutCancellationIsNotCancelled,
TestClaudeCandidateUnsupportedOperationsAndResumeFailClosed,
TestClaudeCandidateWatcherTimeoutCanBeReconciled,
TestClaudeCandidateWindowsCommandLineFitsCreateProcessLimit,
TestClaudeCapabilitiesForPlatformPromotesOnlyTheTestedBlobOnWindows,
TestClaudeLiveSmokeSubjectDigestComesFromTheCommit, TestClaudeProbeAbsentExecutableIsUnavailable,
TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation,
TestClaudeProbeDoesNotParseFailedCommandOutput, TestClaudeProbeHelpWithoutJSONSchemaFailsClosed,
TestClaudeProbeKnownVersionAndHelpFailsClosed,
TestClaudeProbeRejectsNilContextBeforeTheSharedGuard,
TestClaudeProbeRejectsSuccessAfterProbeDeadline, TestClaudeProbeUnknownVersionFailsClosed,
TestClaudeTestdataManifestBindsRecordedDigests, TestClaudeVersionGateAnchorsTheExactClientLine,
TestCloneClaudeCandidateTaskResultDeepCopiesNestedProtocolFields,
TestNativeClaudeCandidateStructuredOutput
```

## 9. Mutant records

Method. Base-side mutants ran in a `git archive` scratch tree of `4fdea989` (`base-tree`); HEAD-side
mutants ran in filesystem copies of the final worktree (`head-tree` for the harness and app
packages, `head-tree-codex` for the codex and pi mutations). Every mutation was applied with the accounted
patch tool, the file restored from the pristine source afterwards, and the copy re-hashed against
the §1/§8 subject hashes; the repository worktree itself was never mutated by a mutant. Each row
names the test that reddens and its observed message; `exit=1` means the test binary failed.

### 9.1 Close permanence (contract Design 2)

| | base | HEAD |
| --- | --- | --- |
| mutant | `Claude` adapter `Close`: `return permanentCloseFailure{Err: err}` → `return fmt.Errorf("permanent close failure: %w", permanentCloseFailure{Err: err})` | `close_gate.go`: the shared constructor returns the wrapped marker instead of the bare one (plus the `fmt` import the mutation needs) |
| test | `TestClaudeCandidateCloseFailureIsStableWithoutReterminating` | same test (moved bytes) |
| result | red, exit 1 — `claude_candidate_adapter_test.go:840: Terminate calls = 2, want one actual attempt` | red, exit 1 — `adapter_test.go:840: Terminate calls = 2, want one actual attempt` |

Deleting the bare-marker construction therefore reddens the suite on both sides; a wrapped marker is
treated as retryable exactly as `session_scaffolding_test.go`'s unowned wrapped-marker case requires.

### 9.2 Retargeted Claude probe cases

| Test | Base mutant (base tree) | Base result | HEAD mutant (copy) | HEAD killer and result |
| --- | --- | --- | --- | --- |
| `TestClaudeProbeKnownVersionAndHelpFailsClosed` | `probeClaudeExecutable`'s fail-closed tail → `nil` | red, `claude_probe_test.go:26: Claude probe error = <nil>, want native harness behavior is unverified` | `reject` returns `nil` | the retargeted same-name test red (`probe_test.go:27`); pre-existing `TestClaudeCapabilitiesForPlatformPromotesOnlyTheTestedBlobOnWindows` also red (`windows_blob_mismatch`) |
| `TestClaudeProbeHelpWithoutJSONSchemaFailsClosed` | `hasClaudeTransportHelp` forced true | red, `claude_probe_test.go:49: … want ErrNativeUnverified naming --json-schema` | `hasTransportHelp` forced true | retargeted test red (`probe_test.go:50`); pre-existing `TestClaudeAdapterProbePromotesOnlyTheResolvedTestedBlob/transport_without_json_schema` red |
| `TestClaudeProbeUnknownVersionFailsClosed` | tested-version equality check dead | red, `claude_probe_test.go:64: … want ErrUnsupportedVersion` | same check dead | retargeted test red (`probe_test.go:65`); pre-existing promotion row `untested_version` red |
| `TestClaudeProbeAbsentExecutableIsUnavailable` | `claudeEvidenceCapabilities`'s `ErrHarnessUnavailable` reason branch dead | red, `claude_probe_test.go:87: start reason = "Claude Code native session lifecycle is unverified", want unavailable reason` | same branch dead | retargeted test red (`probe_test.go:88`, same message) |
| `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` | `UnavailableAdapter.Start` swallows the probe error | red, `claude_probe_test.go:103: Start() error = …, want ErrNativeUnverified` | `probeCapabilities`'s fail-closed tail (`reject`) returns `nil` | retargeted test red on its `Probe` half (`probe_test.go:104: Probe() … want unverified no-operation projection`); the `Start` refusal half has no mutant — `CandidateAdapter.Start` never consults `probe` (§8), recorded as an unpinned new assertion |
| `TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation` | shared bounded-probe timeout removed | red, 30 s guard, `claude_probe_test.go:146: probeClaudeExecutable() did not return after local timeout` | same | pre-existing `TestRunBoundedProbeBoundsHangingDelegate` red (`bounded_probe_test.go:83`) and retargeted test red (`probe_test.go:154`) |
| `TestClaudeProbeRejectsSuccessAfterProbeDeadline` | post-bound value/error check dead | red, `claude_probe_test.go:168: … want context.DeadlineExceeded` | same | retargeted test red (`probe_test.go:163`, 10.02 s); pre-existing `TestRunBoundedProbeRejectsSuccessAfterCallerCancellation` stays green because it exercises the caller-context arm, which the mutation leaves intact |
| `TestClaudeProbeRejectsNilContextBeforeTheSharedGuard` | Claude entry-level nil guard dead | red, exit 1 | same guard dead | retargeted test red (panic on the nil `ctx.Err()`); the shared-guard variant is killed by pre-existing `TestRunBoundedProbeRejectsNilContext` (red, panic) |
| `TestClaudeProbeDoesNotParseFailedCommandOutput` | version-probe error branch dead (failed output parsed) | red, `claude_probe_test.go:209: capabilities = {… VersionKnown:true …}` | same branch dead | retargeted test red (`probe_test.go:204`, same shape) |

### 9.3 Deleted codex cases (subject: the deleted root registry seam)

| Deleted case | Its base mutant | Base result | HEAD twin | HEAD killer (pre-existing unless noted) |
| --- | --- | --- | --- | --- |
| `TestCodexProbeAcceptsWarningPrefixedVersionOutput` | `ParseCodexVersion`'s warning skip dead | red, `codex_probe_test.go:54` | same | root `TestCodexParseVersionToleratesBoundedLeadingWarnings` (retained, unchanged) red, `codex_probe_test.go:19` |
| `TestCodexProbeKnownVersionHelpAndSchemaFailsClosed` | seam tail sets `Verified` (stops failing closed) | red, `codex_probe_test.go:71: Codex capabilities = {… Verified:true …}` | `codex.verifiedCapabilitiesForPlatform` blob gate dead | `codex/probe_test.go` `TestVerifiedCapabilitiesForPlatformIsFailClosed` red, `probe_test.go:73: unbound windows projection error = <nil>` |
| `TestProbeRejectsNonExactVersionOutput` | version pattern unanchored (suffix accepted) | red, subtest `codex-cli 0.168.0-alpha.1` | same | the retained root test itself red (subtest `codex-cli 0.168.0-alpha.1`, `ValidateCodexVersionOutput` arm — retargeted, assertions unchanged) |
| `TestCodexProbeSchemaMismatchFailsClosed` | `ValidateCodexProbeEvidence` forces `SchemaKnown` | red, `codex_probe_test.go:120: … want ErrUnsupportedVersion` | same | `codex/probe_test.go` `TestProbeSchemaMismatchFailsClosedAsUnsupportedVersion` red, `probe_test.go:100` |
| `TestCodexProbeRejectsNilContextWithoutCallingTheRunner` | wrapper guard and shared guard both removed | red, panic (nil dereference) | shared guard removed | `bounded_probe_test.go` `TestRunBoundedProbeRejectsNilContext` red, panic. Gap recorded in §12: `codex.Probe`'s own guard text is unpinned at HEAD |
| `TestCodexProbeDoesNotParseFailedCommandOutput` | seam's version-error branch swallowed | red, `codex_probe_test.go:167: probeCodexExecutable() error = nil, want bounded command failure` | `codex.Probe`'s version-error branch swallowed | `codex/probe_test.go` `TestProbeMapsBoundedCommandErrorsToFailClosedResults/version_output_limit_and_cleanup` red |

### 9.4 Root registry rows and app rows

| Deleted/retargeted row | Base mutant | Base result | HEAD mutant | HEAD killer |
| --- | --- | --- | --- | --- |
| `harness_test.go` codex row | seam's version-error branch swallowed | red (`…/codex` subtest) | `codex.Probe`'s branch swallowed | pre-existing codex test: `TestProbeMapsBoundedCommandErrorsToFailClosedResults/version_output_limit_and_cleanup` red |
| `harness_test.go` claude row | seam's probe-error branch dead | red (`…/claude` subtest) | `probeCapabilities`'s probe-error branch dead | pre-existing app `TestComposedClaudeProbeReportsUnavailableMetadata` red, and the retargeted app row `…/claude` red |
| `harness_test.go` pi/opencode rows | root `newUnavailableAdapter` advertises `Start` | red (`…/pi`, `…/opencode` subtests) | same | the retained OpenCode row red (unchanged bytes of the same test) |
| app claude row | probe-error branch dead | red (`TestStartupProbeRegistersUnavailableNativeProjections/claude`) | `probeCapabilities`'s probe-error branch dead | pre-existing `TestComposedClaudeProbeReportsUnavailableMetadata` red + retargeted row red |
| app pi row | root `newUnavailableAdapter` advertises `Start` | red (`…/pi`) | `pi/probe.go` version-error branch swallowed | pre-existing `pi/probe_test.go` `TestProbeMapsBoundedVersionOutputFailureToHarnessUnavailable` red, plus retargeted row `…/pi` red |

Counts: 19 distinct mutations on the base tree and 20 on the copies, each applied once, shown red on
its named test, reverted, and the mutated file re-hashed against the §1/§8 subject bytes. Two honest
exceptions are named above: under the post-bound mutation the caller-cancellation arm stays green
(the retargeted test is that assertion's killer), and the `Start`-refusal half of the retargeted
`TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` has no mutant because the production
`Start` never consults the probe.

## 10. Gates on the final bytes

All commands ran in `daemon/` foreground on the final working tree (the same bytes as §1/§8).

| Gate | Command | Result |
| --- | --- | --- |
| formatting | `gofmt -l internal/harness internal/app` | no output, exit 0 |
| vet | `go vet ./...` | exit 0 |
| build matrix | `GOOS=windows\|linux\|darwin go build ./...` and `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 ×4 |
| import graph | `go list -deps ./internal/harness` piped through `grep 'internal/harness/'` | no output (grep exit 1), §4 |
| reachability lint | `GOOS=linux golangci-lint run --config .golangci.yml --enable-only unused ./internal/harness/...` (2.15.0) | `0 issues.`, exit 0 |
| Windows suite | `go test -count=1 ./internal/harness/... ./internal/app/...` (verbose re-run on the final bytes) | exit 0; `harness` 0.200 s, `claude` 16.372 s, `codex` 14.366 s, `linejson` 0.164 s, `omo` 3.771 s, `omp` 4.517 s, `opencode` 0.690 s, `pi` 9.446 s, `probeconformance` 7.387 s, `app` 183.936 s |
| WSL suite | `wsl -d Ubuntu-24.04 -u accept` running the same command in this worktree's mount | §10.1 |
| frozen surfaces | `git diff --name-only` plus a name filter | no `opencode/**`, `pi/**`, `claude/stream*.go` or `testdata/**` entry |
| line cap | `wc -l` over every file this slice writes or moves | maximum `claude/adapter.go` 1,181 and `claude/adapter_test.go` 1,164; all < 1,500. Pre-existing over-cap files outside the owned set (`codex/adapter_test.go` 3,144, `codex/adapter.go` 2,619, `opencode/adapter_native_*` 2,571/2,487) are W5-16's/W6's and are byte-unchanged here |
| whitespace | `git diff --check` | exit 0 |
| citation gate | `python3 scripts/ci/check-citation-existence.py --base 4fdea989 --head HEAD` in the mirrored clone | exit 0, §11 |

Test-name bookkeeping (`go test -list '.*'` over `./internal/harness/... ./internal/app/...`):
base 1,124 names → HEAD 1,119. Exactly 65 root `harness` names disappear: 60 reappear once each in
`internal/harness/claude` (47 adapter + 13 probe/promotion, no duplicates), and the five codex cases
of §8 are deleted. `TestBoundedProbeRejectsSuccessAfterDeadline` and the retargeted
`TestProbeRejectsNonExactVersionOutput` keep their names in root `harness`. No name was added.

### 10.1 WSL / linux suite and the SKIP list

Windows verbose suite (`go test -count=1 -v ./internal/harness/... ./internal/app/...`): exit 0,
the ten `ok` lines above (app 183.9 s), 34 `--- SKIP` lines over 31 distinct tests.

Skip provenance, checked rather than asserted: every skipped test's defining file was located in the
tree and compared with this slice's changed-file set. The only skipped tests whose files this slice
changes are the five moved, environment-gated live tests — `TestClaudeCandidateLiveRepositoryTurnSmoke`,
`TestClaudeCandidateLiveCancelSmoke`, `TestClaudeCandidateLiveProbePromotesTheTestedBlob`,
`TestClaudeCandidateLiveProbeRejectsDifferentBlob` (`claude/adapter_live_test.go`) and
`TestNativeClaudeCandidateStructuredOutput` (`claude/adapter_native_test.go`) — and their skip lines
are byte-identical to the base's (`t.Skip("set " + claudeLiveSmokeEnv + "=1 …")` and
`t.Skip("set SYMMETRY_CLAUDE_NATIVE_SMOKE=1 …")`, both unchanged by the verbatim audit of §8). The
remaining 26 skipped tests are defined in files this slice does not touch (`pi/provider_extension*`,
`omp/native_acceptance_test.go`, `opencode/adapter_native_*`, `codex/adapter_live_*`,
`codex/adapter_native_test.go`, `omo/native_test.go`, `omo/adapter_test.go`, `pi/adapter_native_*`,
`pi/probe_live_test.go`, `app/pi_provider_bridge_test.go`). No new skip exists.

WSL/linux suite, whole-tree invocation (`wsl -d Ubuntu-24.04 -u accept`, tree under test = this
worktree's `/mnt/q/...` mount, `go test -count=1 -v ./internal/harness/... ./internal/app/...`):
`WSL_SUITE_EXIT=1`. Six packages `ok` — root `harness` 0.073 s, `claude` 11.598 s, `codex` 13.293 s,
`linejson` 0.043 s, `omo` 1.601 s, `probeconformance` 7.041 s. Four packages `FAIL` — `omp` 32.798 s,
`opencode` 600.065 s, `pi` 427.617 s, `app` 600.053 s — and 39 `--- SKIP` lines. The failing tests
are `omp`'s `TestNativeAcceptanceCancelToolOracle` (its 30 s local readiness handshake),
`opencode`'s `TestNativeOpenCodeRealConfigUsesLoopbackObserverEndpoint` plus twelve `TestClient*`
HTTP tests before the package timeout, `pi`'s whole `TestProviderBridge*` family (30 s dial waits
each) and the `app` package hanging in `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`
(9 m 58 s) until the package timeout. None of those packages is edited by this slice, and all four
are green on the Windows host (§10). A first attempt ran with Windows-side gates concurrently and
failed on exactly the same four packages, so the condition is not load-dependent.

Loopback probe in the same distro (this slice's own, `<HOST-PATH>/tmp/…/loopback_probe.py`: bind a
listener on `127.0.0.1:0`, then connect to it with a 5 s timeout):

```
Traceback (most recent call last):
  File "…/loopback_probe.py", line 40, in <module>
    client.connect(("127.0.0.1", port))
TimeoutError: timed out
```

The distro cannot complete a loopback TCP connect at all, which is the condition the sibling slice
`docs/evidence/rf-w4-15a-jsonl-decoder-leaf-2026-11-22.md` §8 records for this host
(`omp`/`opencode`/`pi` failing on the same HTTP/IPC tests with `dial tcp 127.0.0.1:<port>: i/o
timeout`). Every WSL failure in this slice's runs is in a package whose tests make loopback HTTP/IPC
connections (`opencode`'s client tests, the `app` loopback/claim tests, `omp`'s 30 s readiness
handshake, `pi`'s provider-bridge family), none of which this slice edits, and the same packages are
green on the Windows host (§10).

Slice-relevant WSL surface, bounded invocations with exact exit codes (re-run on an otherwise idle
distro after the whole-tree attempt):

| Invocation | Result |
| --- | --- |
| `go test -count=1 ./internal/harness ./internal/harness/claude ./internal/harness/codex ./internal/harness/linejson ./internal/harness/omo ./internal/harness/probeconformance` (every moved/edited harness package) | 6/6 `ok` — 0.048 s / 11.386 s / 13.372 s / 0.017 s / 1.431 s / 7.048 s, exit 0 |
| `go test -count=1 -run 'TestNewHarnessRegistry\|TestComposedClaudeProbe\|TestStartupProbe' ./internal/app/` (the app composition surface this slice requalifies) | `ok` 1.324 s, exit 0 |

The whole-tree failures are therefore the distro's loopback condition, not the slice: the moved
packages and the touched app surface pass under WSL with explicit exit codes, and the four failing
packages' tests all depend on loopback HTTP/IPC that this distro cannot complete.

## 11. Citation check

The gate's audited corpus (`docs/runbooks/**`, `docs/design/quality.md`,
`docs/archive/CLOSED-TODO.md`) gains one document in this range: `docs/archive/CLOSED-TODO.md` (the
W1-06 residual move and its closure statement). Because the gate reads bytes from the `--head`
revision, the uncommitted worktree was mirrored into a scratch clone of the repository at
`4fdea989`, committed there, and the gate ran against that revision — the clone is a throwaway
outside the deliverable; no commit was made in the deliverable's worktree.

Output: `check-citation-existence: 1 changed document(s) pass the path and symbol check (510 path
citation(s))`, exit 0, with the three pre-existing ignore-covered notices
(`control/priv/static/portal/index.html`, `control/priv/static/portal`, `control/deps/ecto/...`) —
none introduced by this slice. The new citations the slice writes
(`docs/plans/2026-11-22-rf-w4-15c-claude-candidate-relocation-plan.md`,
`docs/evidence/rf-w4-15c-claude-candidate-relocation-2026-11-22.md`, `daemon/internal/app/probe.go`,
`daemon/internal/harness/claude/adapter.go`) all exist in the head tree, and no
`<symbol>()`/`<Type.symbol/arity>` citation binds a deleted symbol to a file.

Citation refresh inside the repository: `docs/design/harness.md` — the only design document that
narrates the moved behaviour — names no moved file path or package (`claude_candidate`,
`claude_probe`, `registry.go` and `harness/claude` have no hit in it, checked before the move), so
the slice cites nothing stale and refreshes nothing. The runbook corpus and
`docs/design/quality.md` are untouched.

## 12. Residuals and limits

- Live Claude smoke tests (`claude/adapter_live_test.go`, `claude/adapter_native_test.go`) stay
  environment-gated exactly as at base; no live client ran on this host, so promotion verdicts are
  proven by the fixture-based promotion tests, not by a live turn.
- `go test -race` remains the CI job's oracle for the harness tree; the local Windows host has no
  cgo, as the sibling slices record. WSL ran Go 1.27.0; the Windows host runs 1.27.4.
- The WSL gate ran in this worktree's mount
(`/mnt/q/repos/wt-w4-15c-claude-relocation/symmetry/daemon`) rather than the contract's
`/mnt/q/repos/symmetry/daemon`, which is the main checkout and does not contain this increment.
- The WSL *whole-tree* invocation does not exit 0 on this host: four packages this slice does not
  edit (`omp`, `opencode`, `pi`, `app`) stall on the distro's loopback condition, proven by the
  loopback probe and reproduced by the sibling slice's record; the moved packages and the touched app
  surface pass under WSL with explicit exit codes (§10.1). On Windows the same whole-tree command
  exits 0 for all ten packages.
- Coverage gap recorded, not hidden: `codex.Probe`'s own nil-context guard has no HEAD test after
  `TestCodexProbeRejectsNilContextWithoutCallingTheRunner` is deleted (the shared guard is pinned by
  `TestRunBoundedProbeRejectsNilContext`); the wrapper's guard text was explicitly not asserted by
  the deleted case, so the loss is unobservable there but the guard is unpinned. W5-16 owns the codex
  adapter and should re-pin it.
- Unpinned new assertion: the `Start`-refusal half of
  `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` (§9.2).
- Deliberate deviation from "test bodies change only by package clause, imports, identifier
  qualification and the testdata prefix": one fixture argument in
  `claude/probe_test.go` was scrubbed of a host path carrying the operator's profile name (§8), under
  the conductor's program-wide rule. The retained `codex_probe_test.go` warning-fixture string that
  contains the same shape is base bytes this slice does not touch (it is unchanged context, and that
  file is owned only for the deleted cases), recorded here so the next slice that opens the file can
  scrub it.
- Stale comment left in an unowned file, recorded in `docs/TODO.md`: `claude/stream.go`'s package
  comment still claims the package does not expose a `harness.Adapter`.
- The plan and this record are new documents outside the citation gate's audited corpus; the
  CLOSED-TODO addition is inside it and passed (§11).
