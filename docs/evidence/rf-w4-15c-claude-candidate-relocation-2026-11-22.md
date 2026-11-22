# rf-w4-15c Claude candidate relocation — evidence record (2026-11-22, implement + apply + fix round R1)

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

An apply stage then ran on the branch after the two origin/main merges `d25bc33a` and `9144e6df`: it
re-bound every number in §1, §2, §8 and §10 to the apply worktree's bytes (the implement-stage
numbers bound `f738ef43`, before the merges), applied the conductor-accepted simplify findings and
re-ran the gates. §13 records the old→new bindings, the merge comment-only audit, the changed
functions, the two new mutant runs and the apply-stage gate table. Nothing is committed there.

A fix round (R1) then applied the PR #236 review round 1 findings (P2-1/D2, P3-1..P3-4) on top of
the apply revision `de9cce07` and re-ran the gates; §14 records the changes, the delivered hashes and
the fix-stage gate table. R1's bytes are committed as the branch head that carries this record.

## 1. Subject bytes

Source files at the delivered revision (sha256; fix round R1 rebound the four changed files, §14):

| File | sha256 |
| --- | --- |
| `daemon/internal/harness/claude/adapter.go` | `22561e5db8197afe93c5ee246dc77b0754ceef4fa564bd4cd141ada0e4dd6747` |
| `daemon/internal/harness/claude/probe.go` | `c55c38b2bb0531444da0ddae25ffbd8c1e58c4858db7463b0525a06cacbda80a` |
| `daemon/internal/harness/registry.go` | `3d931a3eea0aebebdd63eb6c412f33bbdf9575490a4e68e5c40787e579af7596` |
| `daemon/internal/harness/close_gate.go` | `95a503ac91fc2c317484c4c5b4ca1ab34161745d0e91ba5e29631fa756471d20` |
| `daemon/internal/app/probe.go` | `98934f764704f906ad92897f1919dac8310a7ec54c38361360414c8d3419cecc` |

Unchanged by this slice, re-hashed to bind the claim: `daemon/internal/harness/codex_probe.go`
`a34e04e2e7888f3cb5b3050d0fc55d0fb9343baf0c29f11891e51be99906bbf1`,
`daemon/internal/harness/bounded_probe.go`
`34243f53791fc179c29568a5c896631a144b57a331c3b27b854371a0e36afe1a`,
`daemon/internal/harness/claude/stream.go`
`8c3267ec8ecf1d1eaf74a016c6849abd51748827a3388ef791caf4de6602145a` (identical to the base
`registry.go`/`claude_probe.go`/adapter hash snapshot taken before the first edit; `codex_probe.go`
and `bounded_probe.go` were never opened by the slice — every mutant that touched them ran in
scratch copies, §9; `bounded_probe.go`'s implement-time hash was `0e8038e8…` and its current value
differs only by the origin/main comment sweep merged in `d25bc33a`, audited comment-only in §13).

Base bytes for the moved code (sha256, from the base worktree before any edit; they bind the
authoring base `4fdea989`, not the frozen range base `9e36058` — at `9e36058`
`claude_candidate_adapter.go` hashes
`386b88c52561660144a1f599fa5c9fff3438c3c8b64aa0ed8eafc35c83eeb662`, the `Close()` comment reflow
§13.1's merge audit proves comment-only, while `claude_probe.go` and `registry.go` are
byte-identical at both bases):
`claude_candidate_adapter.go` `a98560d4e13f14eac01f6492c7cac4ba67c408dbf7f5fb215b14db5d69d9bc31`,
`claude_probe.go` `e50b5c7cb7f33a9c8ea6ab66fdd9e8f08acd448f5a30daea90f464ec36c9957e`,
`registry.go` `47b49ec408d59ec70cca1a5ac05d151afe75269a85c62a10299f1acfbb0fc7b2`.

Test-file hashes of the final bytes: §8.

## 2. Identifier move table (old → new, call sites after the move)

| Base (root `harness`) | `package claude` | Call sites at HEAD |
| --- | --- | --- |
| `ClaudeCandidateAdapter` | `CandidateAdapter` | `app/probe.go` constructor, `app/harness_test.go` (type assertion + row), `claude/adapter_live_test.go`, `claude/adapter_native_test.go`, `claude/adapter_cmdline_windows_test.go`, `claude/adapter_structured_output_test.go` |
| `NewClaudeCandidateAdapter` | `NewCandidateAdapter` | `daemon/internal/app/probe.go:104`, `daemon/internal/app/harness_test.go:112` and `:299` (composed-probe test + claude row), moved adapter tests, `claude/probe_test.go` |
| `ClaudeCommandRunner` | `commandRunner` (unexported at the apply revision, F4) | `claude/probe.go` |
| `osClaudeCommandRunner` | `osCommandRunner` | `claude/probe.go`, `claude/adapter.go` (Probe), `claude/adapter_live_test.go` |
| `defaultClaudeExecutable` | `defaultExecutable` | `claude/probe.go`, `NewCandidateAdapter` |
| `claudeProbeTimeout` | `probeTimeout` | `probeNativeEvidence` |
| `claudeProbeMaxOutputBytes` | `probeOutputLimitBytes` | `osCommandRunner.Run` |
| `ClaudeTestedVersion` | `testedVersion` (unexported at the apply revision, F4) | `claude/probe.go`, moved tests |
| `ClaudeTestedExecutableHash` | `testedExecutableHash` (unexported at the apply revision, F4) | `claude/probe.go`, moved tests |
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

`close_gate.go` gains the exported constructor; the bare-only rule stays documented once, on the
marker type (fix round R1 dropped the duplicate function doc the move had added, §14):

```go
func PermanentCloseFailure(err error) error { return permanentCloseFailure{Err: err} }

// Classified as permanent only when returned bare: a wrapped or joined marker stays a
// retryable failure, and a nil Err is retryable too.
type permanentCloseFailure struct{ Err error }
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
base 32/39, HEAD 32/39, sets equal.

Full `"…"` literal multiset over the same two-file scope (base `claude_candidate_adapter.go` +
`claude_probe.go`, HEAD `claude/adapter.go` + `claude/probe.go`), with every delta explained (none
is an error or status text):

| Delta | Literal | Cause |
| --- | --- | --- |
| −1 each | `"{"`, `""` | the adapter-local `isClaudeCandidateJSONObject` helper (replaced by `claude/stream.go`'s `isJSONObject`) |
| −1 | `"…/internal/harness/claude"` | the `claudeprotocol` import alias (same package now) |
| +1 | `"claude"` | `claude/probe.go`'s `defaultExecutable`, whose `registry.go` shadow copy is deleted |
| +1 | `"…/internal/execution"` | `claude/probe.go` owns the `osCommandRunner` seam |
| +1 | `"time"` | the `claudeProbeTimeout` bound moved in from `registry.go` as `probeTimeout` |
| +2 | `"…/internal/harness"` | the new root import in `adapter.go` and `probe.go` |

`registry.go`'s deleted shadow declarations (the codex/omp/omo/pi/claude constructors and their
probe helpers) are outside this scope and account for that file's own literal deltas — including
`"codex"` with `defaultCodexExecutable` and one `"Claude Code native session lifecycle is
unverified"`; the session-lifecycle literal survives in this scope's `evidenceCapabilities`, so no
live error or status text is removed.

## 8. Test moves, retargets and deletions

Moved files (bodies, names and comments verbatim; only the package clause, imports, identifier
qualification and the `../testdata/…` prefix differ):

| Root file (base lines) | HEAD file(s) | Lines at HEAD |
| --- | --- | --- |
| `claude_candidate_adapter_test.go` (1,715) | `claude/adapter_test.go` (lines 1-1165) + `claude/adapter_support_test.go` (1166-1715) | 1,162 + 570 |
| `claude_candidate_additional_test.go` (290) | `claude/adapter_additional_test.go` | 287 |
| `claude_candidate_cmdline_windows_test.go` (31) | `claude/adapter_cmdline_windows_test.go` | 31 |
| `claude_candidate_live_test.go` (689) | `claude/adapter_live_test.go` | 690 |
| `claude_candidate_native_test.go` (281) | `claude/adapter_native_test.go` | 282 |
| `claude_candidate_structured_output_test.go` (99) | `claude/adapter_structured_output_test.go` | 100 |
| the promotion half of `claude_probe_test.go` + the eight probe cases | `claude/probe_test.go` | 450 |
| trimmed `claude_probe_test.go` | `TestBoundedProbeRejectsSuccessAfterDeadline` only | 21 |

Final hashes of the moved/edited test files (sha256, refreshed at the fix revision, §14):
`claude/adapter_test.go` `a3ff7e7373d73cbf7b0be793954801ce64e93e97a1c6f5895e75be7488dcf067`,
`claude/adapter_support_test.go` `9d1ff1d68b16453699329da9a4d27f431f41c6d2caf9dea7144cacf3cffbc207`,
`claude/adapter_additional_test.go` `6ac6e0add18a1119a8804b8fb6594f2c82885f3f8955b631141ab0debc0489e5`,
`claude/adapter_cmdline_windows_test.go` `dba7f7cc637f97926df1c7f224d09a490e758f1ab99b4d4ad79fc67a8122f0b2`,
`claude/adapter_live_test.go` `a01b3717440941bcc52d11e851eb4b1c81f42a92ee52d040eb5d44b81f4a7d0e`,
`claude/adapter_native_test.go` `f8660db30cfb195c62fddd833ef28e6782ff9a92c9bc5a655319a4944f71e227`,
`claude/adapter_structured_output_test.go` `070faa821d6bd1a8c7220dcd4ffd8b6074036605695901f9d6933b50ce880250`,
`claude/probe_test.go` `baf16c3bfa1ee4cd7e522495be1898dbceac5b95ca1488cf87aa327b84f4ead8`,
`claude/stream_test.go` `9c2ec2fcfa5814ad147e13438f8d423b2891bd2b4a3d222d7299535fe67f5616` (unchanged),
`harness_test.go` `3162f8f5431ece5fdcd9037375a14be423ff2323d2a6161476d1aed3f9c732f1`,
`codex_probe_test.go` `db1aec275069b656b82dbd682211081f518cbd21c3b0dba45602c59452530f18`,
`claude_probe_test.go` `8864f434e14fbdd365042033bc43c3be4c3f6b26ca94459e40523310a9ba698a`,
`app/harness_test.go` `c3347d5d318e2b7e78732f3d865f978ea493449ae1323a964f3ef15a327d54fc`.

Moved-test verbatim audit: every top-level function of the moved files was extracted from the base
and HEAD bytes, comments/blank lines removed, the rename map and identifier qualification applied,
and compared token-for-token: at the implement revision 110 functions identical, 2 differing, 0
missing. The apply revision supersedes those totals for the touched functions (§13):
`TestClaudeAdapterProbePromotesOnlyTheResolvedTestedBlob` (F2), the two already-differing retargets
below (F5, F8) and six fixture call sites in `claude/adapter_additional_test.go` /
`claude/adapter_support_test.go` (F6) differ from base additionally, and the two helpers named
below are deleted; every other function is unchanged. Fix round R1 removes the F8 half and renames
its test (§14/P3-1), so the delivered retarget set is F2, F5, F6 and the renamed `Probe` half.

- `TestClaudeAdapterProbeFailsClosedWhenExecutableBlobIsUnverified` — the §8 retarget, renamed in
  fix round R1 from `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` (§14/P3-1).
- `TestClaudeProbeKnownVersionAndHelpFailsClosed` — one fixture argument, scrubbed from the base's
  `<HOST-PATH>`-shaped Windows executable carrying the operator profile name to the `runneradmin`
  user-directory shape under the conductor's program-wide rule that no committed file carries a
  host path with a user profile name. The value is consumed only as the `executable` argument of
  the fixture runner, which keys its responses on the argv (`--version`/`--help`), so the assertion
  is unchanged; the `runneradmin` shape already exists in the base's Windows cmdline test.

`assertKnownVersionProjection` was the one intentionally copied helper (its base original lives in
root `harness_test.go`, which is not part of the moved set); the apply stage inlined it into its
only caller (F5) and deleted `jsonObjectForCandidateAdditionalTest` in favour of the literal the
package's other fixture sites already use (F6, §13). The surviving copied test-only helpers are
`testHangGuard` (declared once, in `claude/adapter_support_test.go`) and `sameStrings` (in
`claude/probe_test.go`). Root `harness_test.go` keeps `testHangGuard` for `bounded_probe_test.go`
and `session_scaffolding_test.go`.

Retargeted cases (same test names, same fixtures, same assertions; only the call target and, where
noted, the sentinel that the new production path carries):

| Case | HEAD subject | Reason |
| --- | --- | --- |
| `TestClaudeProbeKnownVersionAndHelpFailsClosed` | `probeCapabilities` | the fail-closed tail now lives in `capabilitiesForPlatform`'s reject paths; pinning an unverified blob makes `ErrNativeUnverified` platform-independent |
| `TestClaudeProbeHelpWithoutJSONSchemaFailsClosed` | `probeCapabilities` | the help gate moved with the probe |
| `TestClaudeProbeUnknownVersionFailsClosed` | `probeCapabilities` | the version gate moved with the probe |
| `TestClaudeProbeAbsentExecutableIsUnavailable` | `probeCapabilities` | the availability projection moved with the probe |
| `TestClaudeAdapterProbeFailsClosedWhenExecutableBlobIsUnverified` (fix round R1 rename of `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence`, §14/P3-1) | `adapter.Probe` over the same fixture | the sentinel assertion (`ErrNativeUnverified` + unverified no-operation projection) survives on the `Probe` half; the `Start` half is dropped because `CandidateAdapter.Start` never consults probe evidence, so it pinned the request-validation refusal rather than the fail-closed probe claim |
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
in §10): every base test name except the five deleted ones and the one fix-round rename (§14/P3-1) is
present exactly once, and the other retargeted names keep their spellings; no name was added.

The moved corpus is 60 names: the intersection of the base root-`harness` name set and the HEAD
`internal/harness/claude` name set holds 59 of them, each appearing exactly once in both lists (the
base list held the 60th once too). Fix round R1 renamed
`TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` to
`TestClaudeAdapterProbeFailsClosedWhenExecutableBlobIsUnverified` (§14, P3-1); all 60 HEAD
spellings, each exactly once:

```
TestClaudeAdapterProbePromotesOnlyTheResolvedTestedBlob,
TestClaudeAdapterProbeFailsClosedWhenExecutableBlobIsUnverified,
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
| `TestClaudeAdapterProbeFailsClosedWhenExecutableBlobIsUnverified` (fix round R1 rename of `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence`, §14) | `UnavailableAdapter.Start` swallows the probe error | red, `claude_probe_test.go:103: Start() error = …, want ErrNativeUnverified` | `probeCapabilities`'s fail-closed tail (`reject`) returns `nil` | the renamed test red on its unchanged `Probe` half at the fix revision (`probe_test.go:108: Probe() = capabilities=…, want unverified no-operation projection`); the `Start` refusal half had no mutant at the implement revision — `CandidateAdapter.Start` never consults `probe` (§8) — and fix round R1 dropped it under the conductor's P3-1 ruling instead of keeping the apply-stage exact-text pin (F8, §13) |
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
`TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` has no mutant at the implement revision
because the production `Start` never consults the probe. The apply stage adds two mutant runs on the
apply revision (§13): the `reject`-returns-`nil` re-run reddens the inlined F5 assertion at
`probe_test.go:28`, and the weakened workspace-guard mutant reddens the F8 pin at `probe_test.go:115`
(that pin was removed in fix round R1, §14/P3-1). Fix round R1 re-ran the `reject`-returns-`nil`
mutant against the delivered `claude/probe.go` bytes through a `go test -overlay` replacement (the
worktree was not mutated): the renamed test reddens at `probe_test.go:108`, exit 1.

## 10. Gates on the final bytes (implement-stage run)

All commands ran in `daemon/` foreground on the working tree at the implement revision. The
apply-stage re-run on the refreshed bytes (§1/§8) is §13.

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
| line cap | `wc -l` over every file this slice creates, moves or splits | maximum `claude/adapter.go` 1,180 and `claude/adapter_test.go` 1,162 at the apply revision (the implement bytes measured 1,181/1,164); all < 1,500. Touched pre-existing over-cap file: `daemon/internal/app/harness_test.go` 6,331 L (6,338 at base; outside this corpus, listed so the exclusion is explicit). Byte-unchanged over-cap files outside the owned set (`codex/adapter_test.go` 3,144, `codex/adapter.go` 2,619, `opencode/adapter_native_*` 2,571/2,487) are W5-16's/W6's |
| whitespace | `git diff --check 9e36058` (frozen range base → final bytes; the implement-stage plain form ran while this record and the plan were untracked) | exit 0 (the two `space before tab in indent` lines the range form flags are re-indented) |
| citation gate | `python3 scripts/ci/check-citation-existence.py --base 4fdea989 --head HEAD` in the mirrored clone | exit 0, §11 |

Test-name bookkeeping (`go test -list '.*'` over `./internal/harness/... ./internal/app/...`):
base 1,124 names → fix revision 1,119. Exactly 65 root `harness` names disappear: 60 reappear once
each in `internal/harness/claude` (47 adapter + 13 probe/promotion, no duplicates) — 59 with their
base spellings and one renamed in fix round R1 (§14/P3-1) — and the five codex cases of §8 are
deleted. `TestBoundedProbeRejectsSuccessAfterDeadline` and the retargeted
`TestProbeRejectsNonExactVersionOutput` keep their names in root `harness`. No name was added; the
R1 rename is the only spelling change. Re-derived at the apply revision: the same command lists
1,119 `Test*` names over the same two package trees, and the apply stage added, renamed or deleted
no test — its two deleted helpers are not tests (§13).

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
worktree's `<HOST-PATH>` distro mount, `go test -count=1 -v ./internal/harness/... ./internal/app/...`):
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
the slice cites nothing stale. Fix round R1 adds to its Claude paragraph the probe-bound rationale
cut from `claude/probe.go` (P3-2), still without naming a moved path; the document stays outside the
citation gate's corpus. The runbook corpus and `docs/design/quality.md` are untouched.

## 12. Residuals and limits

- Live Claude smoke tests (`claude/adapter_live_test.go`, `claude/adapter_native_test.go`) stay
  environment-gated exactly as at base; no live client ran on this host, so promotion verdicts are
  proven by the fixture-based promotion tests, not by a live turn.
- `go test -race` remains the CI job's oracle for the harness tree; the local Windows host has no
  cgo, as the sibling slices record. WSL ran Go 1.27.0; the Windows host runs 1.27.4.
- The WSL gate ran in this worktree's `<HOST-PATH>` distro mount of `daemon/` rather than the
contract's `<HOST-PATH>` main-checkout mount, which does not contain this increment.
- The WSL *whole-tree* invocation does not exit 0 on this host: four packages this slice does not
  edit (`omp`, `opencode`, `pi`, `app`) stall on the distro's loopback condition, proven by the
  loopback probe and reproduced by the sibling slice's record; the moved packages and the touched app
  surface pass under WSL with explicit exit codes (§10.1). On Windows the same whole-tree command
  exits 0 for all ten packages. Re-run twice at the apply revision with a 150 s bound: exit 1, the
  same four packages (§13.4).
- Coverage gap recorded, not hidden: `codex.Probe`'s own nil-context guard has no HEAD test after
  `TestCodexProbeRejectsNilContextWithoutCallingTheRunner` is deleted (the shared guard is pinned by
  `TestRunBoundedProbeRejectsNilContext`); the wrapper's guard text was explicitly not asserted by
  the deleted case, so the loss is unobservable there but the guard is unpinned. W5-16 owns the codex
  adapter and should re-pin it; fix round R1 records it as a `docs/TODO.md` entry (§14/P3-3).
- Superseded in fix round R1: the `Start`-refusal half of the retargeted test asserted the exact
  workspace-refusal text at the apply stage and reddened under the weakened-guard mutant (§9.2, F8,
  §13), but the conductor's P3-1 ruling drops it — `CandidateAdapter.Start` never reads probe
  evidence, so the half did not pin the fail-closed probe claim — and renames the remaining `Probe`
  half to `TestClaudeAdapterProbeFailsClosedWhenExecutableBlobIsUnverified` (§14). The exact
  refusal text therefore loses its only HEAD pin; fail-closed native admission stays owned by the
  app-layer admission tests.
- Deliberate deviation from "test bodies change only by package clause, imports, identifier
  qualification and the testdata prefix": two fixture strings were scrubbed of host paths carrying
  the operator's profile name under the conductor's program-wide rule — the `claude/probe_test.go`
  fixture argument (§8) and, in fix round R1, the retained `codex_probe_test.go` warning fixture the
  conductor amendment (review D2) added to the owned set (§14). Both replacements use the neutral
  `runneradmin` user-directory shape the Windows cmdline test already used; both keep their escaping
  and quoting, and every assertion is unchanged.
- Stale comment left in an unowned file, recorded in `docs/TODO.md`: `claude/stream.go`'s package
  comment still claims the package does not expose a `harness.Adapter`. The apply stage added two
  more `docs/TODO.md` entries: the `controlCapability` copy (`claude/adapter.go:576`) versus the
  root helper (`generic.go:255`, unowned), and the two stale `testdata/claude/**` README identifiers
  (W5-10's surface, finding 7).
- The plan and this record are new documents outside the citation gate's audited corpus; the
  CLOSED-TODO addition is inside it and passed (§11).

## 13. Apply-stage refresh (2026-11-22)

An apply stage ran on the branch head after the two origin/main merges `d25bc33a` and `9144e6df`
(nothing committed). It applied the conductor-accepted subset of the simplify report and re-bound
every number in §1, §2, §8 and §10 to the apply worktree, with the deltas below.

### 13.1 Revision binding (finding D1)

The implement-stage numbers in §1/§8/§10 bound `f738ef43`, the pre-merge authoring revision; the
delivered branch had advanced to `9144e6df`. Both merge steps were audited before the apply edits:

| Merge step | Audit | Result |
| --- | --- | --- |
| `f738ef43` → `d25bc33a` | `git diff --numstat` over the six slice files (`app/probe.go`, `bounded_probe.go`, `claude/adapter.go`, `claude/adapter_test.go`, `claude/probe_test.go`, `close_gate.go`): 30 insertions / 81 deletions, 272 raw diff lines | comment/format-only: parsing both revisions without comments and printing the AST through `go/printer` yields byte-identical source for all six files (throwaway tool at `<HOST-PATH>/.local/tmp/w4-15c-apply-mutant/strip/main.go`, outside the repo) |
| `d25bc33a` → `9144e6df` | `git diff --stat d25bc33a 9144e6df -- daemon/` | empty; that merge carried the frontend `rf-w4-08` increment only |

Old → new bindings (implement revision → apply revision):

| File | implement (bound `f738ef43`) | apply (bound by §1/§8) |
| --- | --- | --- |
| `claude/adapter.go` | `558ba2d9…` | `ea4af145…` |
| `claude/probe.go` | `a9582c9b…` | `767e3310…` |
| `close_gate.go` | `e171bb82…` | `a77b2bd9…` |
| `app/probe.go` | `73b42c9a…` | `98934f76…` |
| `claude/adapter_test.go` | `6ee75b8e…` (1,164 L) | `a3ff7e73…` (1,162 L) |
| `claude/probe_test.go` | `c71e1d98…` (490 L) | `011bef5e…` (457 L) |
| `bounded_probe.go` (never edited by the slice) | `0e8038e8…` | `34243f53…` |

`registry.go` (`efd6473a…`), `codex_probe.go` (`a34e04e2…`) and `claude/stream.go` (`8c3267ec…`)
were byte-identical at both revisions.

### 13.2 Findings applied and rejected (files, then content)

Applied, each inside the contract's owned file set:

| # | File(s) | Change |
| --- | --- | --- |
| 2 | `claude/probe_test.go` | dropped the never-assigned, never-read `wantDigestKnown` fixture field (dead at base too) |
| 4 | `claude/probe.go` plus five moved test files | `TestedVersion`, `TestedExecutableHash` and `CommandRunner` unexported (`testedVersion`, `testedExecutableHash`, `commandRunner`); the whole tree had zero package-external consumers; §2 updated |
| 5 | `claude/probe_test.go` | `assertKnownVersionProjection` (one call site) inlined into `TestClaudeProbeKnownVersionAndHelpFailsClosed`; the `knownVersionProjection` type and the stale "cannot drift between providers" comment deleted; rendered failure texts unchanged |
| 6 | `claude/adapter_additional_test.go`, `claude/adapter_support_test.go` | `jsonObjectForCandidateAdditionalTest` (name bound to its origin file, called cross-file) replaced by the `` json.RawMessage(`{"snapshot":"canonical"}`) `` literal the package's other 17 `Context:` fixture sites and the sibling adapter packages already use; fixture bytes unchanged |
| 7 | `docs/TODO.md` | deferred item for the two stale `testdata/claude/**` README identifiers (`claudeCandidateStructuredOutputSchema` → `candidateStructuredOutputSchema`, `ClaudeTestedExecutableHash` → `testedExecutableHash`); testdata is frozen for this slice and W5-10 owns it |
| 8 | `claude/probe_test.go` | the `Start` half of `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` now asserts the exact workspace-refusal text; the old `err != nil` passed on the request-validation error while claiming a probe-evidence refusal. Superseded in fix round R1: the half was dropped under the conductor's P3-1 ruling (§14) |
| D1 | this record | the rebinding in §13.1 |

Rejected, bytes unchanged:

| # | Reason |
| --- | --- |
| 1 | the fix needs `bounded_probe_test.go`, a 5a helper file outside the owned set; deleting `claude_probe_test.go` without moving its test would delete a test |
| 3 | the fix needs `generic.go` (explicitly not owned); the `controlCapability` copy is the recorded compiler-forced consequence, now also tracked in `docs/TODO.md` |
| D2 | **applied in fix round R1** under the conductor amendment (2026-11-22, review D2): the amendment added the retained-case hunk to the owned set, so the §12 deferral no longer stands; the scrub is recorded in §14 |

### 13.3 Applied-revision mutants

Run in a filesystem copy of the apply worktree
(`<HOST-PATH>/.local/tmp/w4-15c-apply-mutant/daemon`), restored afterwards and verified against the
worktree bytes (`diff -r` over the whole `claude` package reports no difference; the mutated files
re-hash to the §1/§8 values):

| Mutant | Test | Result |
| --- | --- | --- |
| `claude/probe.go` `reject` returns `nil` instead of `harness.ErrNativeUnverified` (the implement-revision killer, re-run against the inlined assertions) | `TestClaudeProbeKnownVersionAndHelpFailsClosed` | red, exit 1 — `probe_test.go:28: Claude probe error = <nil>, want native harness behavior is unverified` |
| `claude/adapter.go` workspace guard weakened (`\|\|` + `!filepath.IsAbs` → `&&` + `filepath.IsAbs`), so an empty workspace no longer refuses | `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` | red, exit 1 — `probe_test.go:115: Start() error = Claude Code native adapter event sink must not be nil, want the absolute-path workspace refusal`. Superseded in fix round R1: the assertion was removed with the `Start` half (§14/P3-1), so this mutant no longer has a named killer; fail-closed native admission stays owned by the app-layer admission tests |

An equivalent-mutant variant that kept `!filepath.IsAbs` under `&&` stayed green: the empty path
satisfies both disjuncts, so that mutation changes no behaviour for this request; the mutant above
is the one that removes the refusal the assertion pins.

### 13.4 Apply-stage gates

All commands ran in `daemon/` on the apply worktree, foreground, except the WSL runs. The Windows
suite and the WSL runs never overlapped.

| Gate | Command | Result |
| --- | --- | --- |
| formatting | `gofmt -l internal/harness internal/app` | no output, exit 0 |
| vet | `go vet ./...` | exit 0 |
| build matrix | `GOOS=windows|linux|darwin go build ./...` and `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 ×4 |
| import graph | `go list -deps ./internal/harness` piped through `grep 'internal/harness/'` | no output (grep exit 1) |
| reachability lint | `GOOS=linux golangci-lint run --config .golangci.yml --enable-only unused ./internal/harness/...` (2.15.0) | `0 issues.`, exit 0 |
| Windows suite | `go test -count=1 ./internal/harness/... ./internal/app/...` | exit 0; 10/10 packages `ok` (app 183.9 s) |
| WSL bounded | `wsl -d Ubuntu-24.04 -u accept` (Go 1.27.0): `go test -count=1` over the six moved/edited harness packages, then `-run 'TestNewHarnessRegistry\|TestComposedClaudeProbe\|TestStartupProbe' ./internal/app/` | 6/6 packages `ok` — harness 0.063 s, claude 11.570 s, codex 13.068 s, linejson 0.019 s, omo 1.510 s, probeconformance 7.034 s — exit 0; app subset `ok` 1.287 s, exit 0 |
| WSL whole tree | `go test -count=1 -timeout 150s ./internal/harness/... ./internal/app/...` | exit 1; 6 packages `ok` — root `harness` 0.061 s, claude 11.251 s, codex 13.017 s, linejson 0.025 s, omo 1.180 s, probeconformance 7.037 s — and 4 `FAIL` at the 150 s bound: `omp` 31.831 s, `opencode` 150.047 s, `pi` 150.037 s, `app` 150.094 s — the same loopback-dependent packages as the implement-stage run (§10.1), reproduced by a second identical invocation. The exit code was captured by a script file (`<HOST-PATH>/.local/tmp/w4-15c-apply-mutant/wsl-tree.sh`) executed through the distro, so it comes from the outer process. The claude package's five environment-gated moved tests still skip (`-v` count 5; provenance unchanged, §10.1) |
| test-name bookkeeping | `go test -list '.*'` over `./internal/harness/... ./internal/app/...` | 1,119 `Test*` names, unchanged; nothing added, renamed or deleted (fix round R1 then renamed one name in place, §14) |
| line cap | `wc -l` over every file this slice creates, moves or splits | maximum `claude/adapter.go` 1,180 and `claude/adapter_test.go` 1,162; all < 1,500. The touched pre-existing over-cap `daemon/internal/app/harness_test.go` (6,331 L) is outside this corpus (§10 row) |
| whitespace | `git diff --check 9e36058` (frozen range base → final bytes) | exit 0; the two `space before tab in indent` lines the frozen tip carried are re-indented |
| record cleanliness | `python3 scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record | exit 0; the implement bytes exited 1 (four lines carried host absolute paths) and now use `<HOST-PATH>` tokens |
| profile-name scan | `git diff` piped through a case-insensitive host-root/profile-name scan (patterns spelled with a character class so this diff does not match itself) | no operator profile name in any added line; the retained `runneradmin` fixture shapes and the then-untouched `codex_probe_test.go` context are as recorded (fix round R1 then scrubbed that context, §14) |
| citation gate | corpus `docs/runbooks/**`, `docs/design/quality.md`, `docs/archive/CLOSED-TODO.md` (`scripts/ci/audited_paths.py`) | the apply diff names none of them, so the gate's selected set is empty; the implement-stage run of §11 stands |
| frozen surfaces | `git diff --name-only` plus a name filter | no `opencode/**`, `pi/**`, `claude/stream*.go`, `testdata/**` or `generic.go` entry |

## 14. Fix round R1 (2026-11-22, PR #236 review round 1)

Review: `236-opus-review-1.md` (P2-1/D2, P3-1..P3-4) against the reviewed apply revision `de9cce07`.
R1 applied every finding, re-ran the gates and rebound §1, §8 and the §10 name accounting to the
delivered bytes. The work is committed on top of `de9cce07`, and the branch also merges
`origin/main` (`837dd974`, the rf-w6-01 CI-structure commit, docs/CI-only) so the pushed head
carries both; the revision-scoped gate exits below were added by the record-update commit after the
merge, which is the pushed head.

### 14.1 Findings

| Finding | Change |
| --- | --- |
| P2-1 / D2 | `codex_probe_test.go` warning fixture: the operator-profile segment replaced by the neutral `runneradmin` shape the Claude fixture uses, keeping the doubled-backslash escaping and the quoting, so the warning still parses as one line and the assertion is unchanged. §13.2's D2 row now records the amendment's owned-set change instead of the rejection |
| P3-1 | `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` lost its `Start` half and was renamed `TestClaudeAdapterProbeFailsClosedWhenExecutableBlobIsUnverified`: the conductor ruling holds because `CandidateAdapter.Start` never reads probe evidence, so the half pinned the request-validation refusal rather than the fail-closed probe claim; fail-closed native admission stays owned by app-layer admission tests. The remaining `Probe` half is unchanged |
| P3-2 | comment-policy fixes: dropped the duplicate `PermanentCloseFailure` doc in `close_gate.go`; deleted the `// The exact version gate has one owner.` what-comment in `codex_probe_test.go`; cut the `probeTimeout`, `testedExecutableHash` and `capabilitiesForPlatform` rationales in `claude/probe.go` to 1-2 line why notes and moved the rationale into `docs/design/harness.md`; cut the `Registry` comment in `registry.go` to its true clause (the "every native kind has an explicit constructor" claim was false for `NewRegistry`, which registers Generic and OpenCode) |
| P3-3 | `docs/TODO.md` gains the `codex.Probe` nil-context guard entry (owner W5-16) with the unpinned string, beside the other rf-w4-15c deferrals |
| P3-4 | `claude/adapter.go` local `expectedCancelled` renamed `cancelExpected`; after the package rename it shadowed `func expectedCancelled` |

Removal/rename accounting: one test name changes spelling in place (the §10 total stays 1,119); the
removed `Start` half is one `Start()` call plus two assertions; the exact workspace-refusal text
loses its only HEAD pin, accepted by the P3-1 ruling (§12, §13.3).

### 14.2 Delivered hashes (rebound in §1/§8; §13.1's apply values in parentheses)

| File | apply revision (§13.1) | fix revision R1 (delivered) |
| --- | --- | --- |
| `claude/adapter.go` | `ea4af145…` | `22561e5d…` |
| `claude/probe.go` | `767e3310…` | `c55c38b2…` |
| `registry.go` | `efd6473a…` | `3d931a3e…` |
| `close_gate.go` | `a77b2bd9…` | `95a503ac…` |
| `claude/probe_test.go` | `011bef5e…` (457 L) | `baf16c3b…` (450 L) |
| `codex_probe_test.go` | `5a6c5130…` | `db1aec27…` |

Every other §1/§8 hash is byte-identical at R1: `app/probe.go` `98934f76…`, `codex_probe.go`
`a34e04e2…`, `bounded_probe.go` `34243f53…`, `claude/stream.go` `8c3267ec…`, and the eleven
unchanged test files (`claude/adapter_test.go` `a3ff7e73…`, `claude/adapter_support_test.go`
`9d1ff1d6…`, `claude/adapter_additional_test.go` `6ac6e0ad…`,
`claude/adapter_cmdline_windows_test.go` `dba7f7cc…`, `claude/adapter_live_test.go` `a01b3717…`,
`claude/adapter_native_test.go` `f8660db3…`, `claude/adapter_structured_output_test.go` `070faa82…`,
`claude/stream_test.go` `9c2ec2fc…`, `harness_test.go` `3162f8f5…`, `claude_probe_test.go`
`8864f434…`, `app/harness_test.go` `c3347d5d…`). Line cap unchanged: the maximum is still
`claude/adapter.go` 1,180.

### 14.3 Fix-revision mutant re-run

The `reject`-returns-`nil` mutant was re-applied to the delivered `claude/probe.go` bytes through a
`go test -overlay` replacement (the worktree was not mutated): the renamed
`TestClaudeAdapterProbeFailsClosedWhenExecutableBlobIsUnverified` reddens — exit 1,
`probe_test.go:108: Probe() = capabilities=…, want unverified no-operation projection`. The
weakened workspace-guard mutant's pin was removed with the `Start` half (§13.3, §12).

### 14.4 Fix-stage gates

All commands ran in `daemon/` foreground, on the fix worktree, except the revision-scoped gates,
which run on the pushed commit (their exit codes are in the fix report
`236-fix-r1.md`).

| Gate | Command | Result |
| --- | --- | --- |
| formatting | `gofmt -l internal/harness internal/app` | no output, exit 0 |
| vet | `go vet ./...` | exit 0 |
| build matrix | `GOOS=linux|windows|darwin go build ./...` and `GOOS=linux GOARCH=arm64 go build ./...` | exit 0 ×4 |
| harness suite | `go test -count=1 ./internal/harness/...` | exit 0; 9/9 packages `ok` (claude 20.1 s) |
| app suite | `go test -timeout 20m -count=1 ./internal/app/` | exit 0; `ok … internal/app` 965.1 s (host-load run) |
| test-name bookkeeping | `go test -list '.*'` over `./internal/harness/... ./internal/app/...` | 1,119 `Test*` names; the rename holds the total, the new spelling appears exactly once, the old spelling zero times |
| whitespace | `git diff --check` on the worktree, then `git diff --check origin/main...HEAD` after the `origin/main` merge (base `837dd974`) | exit 0 both times |
| record cleanliness | `python scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record; the contract's `check-record-cleanliness.py --base $(git merge-base HEAD origin/main) --head HEAD` at the merge revision `e1ee4f5a` | sanitize exit 0; gate: `1 changed record(s) pass the root-only check`, exit 0; re-run at the pushed record-update commit reproduces exit 0 |
| profile / DLP scan | added lines of `git diff origin/main...HEAD` grepped for the operator profile name and for a backslash directly before a lowercase `u` | no match on either; the only `Users\`-shaped added lines are the three neutral `runneradmin` fixtures, and the two `-`-line profile matches are the base bytes this round scrubs |
| frozen surfaces | `git diff --name-only origin/main...HEAD` plus a name filter | no `opencode/**`, `pi/**`, `claude/stream*.go`, `testdata/**` or `generic.go` entry |
