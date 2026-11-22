# rf-w4-15c Claude candidate relocation — plan

Contract: `goal-rf-w4-15c-claude-candidate-relocation.md` (GDD state root; external to the
repository). Base revision: `4fdea989a698a96eb01d90a8729e6e8a54457883` (W4-15a merged; `linejson`
leaf and the `claude/stream.go` decoder are in place, `#187`/W3-04 and W3-15 merged too). Branch:
`refactor/rf-w4-15c-claude-candidate-relocation`. Owned files are the contract's strict set plus
this plan, `docs/evidence/rf-w4-15c-claude-candidate-relocation-2026-11-22.md`,
`docs/TODO.md`/`docs/archive/CLOSED-TODO.md` (the W1-06 residual below is closed here).

Goal shape: the concrete Claude adapter and its promotion probe stop living in the root `harness`
package; root stops importing any harness subpackage; close permanence becomes expressible from a
subpackage through one exported constructor with unchanged bare-only classification; `NewRegistry`
stops registering shadow adapters that production immediately overwrites. Wire bytes, persisted
bytes, fixture bytes, every error/log string, probe verdicts, capability projections and CI-pinned
test names stay byte-identical.

## 1. Target package layout and identifier move table

New files under `daemon/internal/harness/claude/` (recorded file names):

| New file | Source | Notes |
| --- | --- | --- |
| `claude/adapter.go` | `claude_candidate_adapter.go` (1,175 L) | package clause, `claudeprotocol` → same package (unqualified), root calls qualified, `claudeCandidate*` → `candidate*` |
| `claude/probe.go` | `claude_probe.go` (239 L) + the Claude block of `registry.go` | probe constants and the `CommandRunner` seam join the probe |
| `claude/adapter_test.go` | `claude_candidate_adapter_test.go:1-1165` | verbatim test functions |
| `claude/adapter_support_test.go` | `claude_candidate_adapter_test.go:1166-1715` | fakes/helpers + the three trailing close tests |
| `claude/adapter_additional_test.go` | `claude_candidate_additional_test.go` | verbatim |
| `claude/adapter_cmdline_windows_test.go` | `claude_candidate_cmdline_windows_test.go` | verbatim |
| `claude/adapter_live_test.go` | `claude_candidate_live_test.go` | verbatim; live tests stay env-gated |
| `claude/adapter_native_test.go` | `claude_candidate_native_test.go` | verbatim; live test stays env-gated |
| `claude/adapter_structured_output_test.go` | `claude_candidate_structured_output_test.go` | verbatim |
| `claude/probe_test.go` | promotion/probe half of `claude_probe_test.go` + the retargeted probe cases (§4) | verbatim functions, retargeted call sites |

No file exceeds 1,500 L: the two split halves are 1,165 L and 550 L; `claude/adapter.go` is
1,175 L base (the move adds only qualifiers), `claude/probe.go` ≈ 320 L.

Identifier moves (old → new, with every call site after the move):

| Base (root `harness`) | `package claude` | Call sites after the move |
| --- | --- | --- |
| `ClaudeCandidateAdapter` | `CandidateAdapter` | `app/probe.go` (`claude.NewCandidateAdapter`), `app/harness_test.go` (type assertion + row), live/native/cmdline/structured tests |
| `NewClaudeCandidateAdapter` | `NewCandidateAdapter` | `app/probe.go:103`, `app/harness_test.go:111`, moved tests, `claude/probe_test.go` probe-injection case |
| `ClaudeCommandRunner` | `CommandRunner` | `claude/probe.go`, moved tests |
| `osClaudeCommandRunner` | `osCommandRunner` | `claude/probe.go`, `claude/adapter.go` Probe, moved live test |
| `defaultClaudeExecutable` | `defaultExecutable` | `claude/probe.go` (`probeNativeEvidence`), `NewCandidateAdapter` |
| `claudeProbeTimeout`, `claudeProbeMaxOutputBytes` | `probeTimeout`, `probeOutputLimitBytes` | `claude/probe.go` (`probeNativeEvidence`, `osCommandRunner.Run`) |
| `ClaudeTestedVersion`, `ClaudeTestedExecutableHash` | `TestedVersion`, `TestedExecutableHash` | moved tests, `claude/probe.go` |
| `claudeImplementationVersion` | `implementationVersion` | `capabilitiesForPlatform`, moved test |
| `claudeProbeEvidence` | `probeEvidence` | `claude/probe.go`, moved tests |
| `claudeVersionPattern`, `parseClaudeVersion`, `hasClaudeTransportHelp` | `versionPattern`, `parseVersion`, `hasTransportHelp` | `claude/probe.go`, moved tests |
| `probeClaudeNativeEvidence` | `probeNativeEvidence` | `claude/probe.go`, `claude/probe_test.go` |
| `claudeExecutableDigest` | `executableDigest` | `probeCapabilities` |
| `rejectClaude`, `claudeCapabilitiesForPlatform`, `claudeEvidenceCapabilities` | `reject`, `capabilitiesForPlatform`, `evidenceCapabilities` | `claude/probe.go`, moved tests |
| `probeClaudeAdapterCapabilities` | `probeCapabilities` | `CandidateAdapter.Probe`, moved tests |
| `claudeCandidate*` internals (`CandidateProcess`, `Session`, `MaxRecordBytes`, `TerminationGrace`, errors `errClaudeCandidate*` → `errCandidate*`, frames, helpers) | `candidate*` | `claude/adapter.go`, moved tests |

Every error/log/status string stays byte-identical, including the `errCandidate*` texts and
`"persist Claude native event: %w"`. No test function is renamed.

## 2. Registry trim

`NewRegistry` registers only the adapters whose implementation lives in root: `KindGeneric` and
`KindOpenCode` (the production OpenCode refusal, frozen E3/D1). Deleted declarations, with their
last caller at the base revision:

| Deleted (root) | Last callers at base |
| --- | --- |
| `NewClaudeAdapter`, `NewClaudeAdapterWithExecutable`, `NewClaudeAdapterWithRunner`, `newClaudeAdapterWithRunner` | `NewRegistry` (`registry.go:86`), `app/harness_test.go:306`, `harness_test.go:61` |
| `probeClaudeExecutable` | `newClaudeAdapterWithRunner` (`registry.go:188`), `claude_probe_test.go` (8 cases) |
| `NewPiAdapter` | `NewRegistry` (`registry.go:87`), `app/harness_test.go:307` |
| `ClaudeCommandRunner`, `osClaudeCommandRunner`, `defaultClaudeExecutable`, `claudeProbeTimeout`, `claudeProbeMaxOutputBytes` | moved into `claude/` (not deleted) |
| `CodexCommandRunner`, `osCodexCommandRunner`, `defaultCodexExecutable`, `testedCodexVersion`, `codexProbeTimeout`, `codexProbeMaxOutputBytes` | `NewCodexAdapter*`, `probeCodexExecutableWithRunner`, `codex_probe_test.go` |
| `NewCodexAdapter`, `NewCodexAdapterWithRunner`, `probeCodexExecutableWithRunner` | `NewRegistry` (`registry.go:85`), `harness_test.go:58`, `codex_probe_test.go` (6 cases) |

Kept in root: `Registry`, `NewRegistry` (trimmed), `Register`, `Lookup`, `Probe`,
`UnavailableAdapter`, `newUnavailableAdapter` (still called by `NewOpenCodeAdapter`),
`NewOpenCodeAdapter`, `AvailabilityError`, `RunBoundedProbe`, `RunProbeCommand`,
`CodexSchemaDigest`, `CommandInvoker`, `ResolveExecutableDigest`, `HashExecutableFile`,
`CodexTestedVersion`/`CodexTestedSchemaHash`/`CodexTestedExecutableHash`,
`ValidateCodexProbeEvidence`/`ValidateCodexVersionOutput`/`ParseCodexVersion`/
`HasCodexStdioAppServerHelp`/`ValidateCodexHelpOutput`, `DeliverEvent`, `IsNilInterface`,
`WaitTurnBarrier`, `WaitResult`, `CloneTaskResult(Pointer)`, `ProcessForwarder`,
`NewProcessForwarder`, `CloseGate` and the `types.go` interfaces. Reachability is proven by the
pinned `unused` run, not by inspection (§6).

After the move, root imports no `harness/*` subpackage at all: the only base edges were
`claude_candidate_adapter.go:17` and the two Claude test files, and `linejson` is imported by the
four adapters, not by root. Proven by `go list -deps ./internal/harness | grep 'internal/harness/'`
(no output) — the contract's "only `linejson`" is a permission, satisfied vacuously.

Production composition is unchanged: `app/probe.go` still registers Codex, Claude, Pi, OMP and OmO
over the root registry, so the daemon's registered-adapter set (Generic, Codex, Claude, Pi, OMP,
OmO, OpenCode) and every projection are byte-identical.

## 3. Close permanence

`close_gate.go` gains one exported constructor, next to the marker it builds:

```go
// PermanentCloseFailure builds the bare marker a close attempt returns when its
// failure must stay sticky. A wrapped marker stays retryable.
func PermanentCloseFailure(err error) error { return permanentCloseFailure{Err: err} }
```

`finish` keeps the bare type assertion (`err.(permanentCloseFailure)`), so a wrapped or joined
marker stays retryable; `session_scaffolding_test.go` (unowned) stays green unchanged and its
`errors.As`/`permanentCloseFailure` uses keep compiling. The moved adapter's `Close` calls
`harness.PermanentCloseFailure(err)` instead of constructing the marker itself. No other `CloseGate`
behaviour changes.

## 4. Test moves, retargets and deletions

Moved tests keep their names and bodies; only the package clause, imports, identifier qualification,
the testdata path prefix (`testdata/claude/...` → `../testdata/claude/...`, as
`claude/stream_test.go` already does) and stale comments change. Test-only helpers the moved files
borrow from root have no cross-package route (`testHangGuard` stays in root for
`bounded_probe_test.go`; `assertKnownVersionProjection`/`knownVersionProjection`/`sameStrings` lose
their root callers), so `claude/adapter_support_test.go` carries a `testHangGuard` constant and
`claude/probe_test.go` carries `sameStrings`, `knownVersionProjection` and
`assertKnownVersionProjection` copies.

Candidate-adapter tests that reach the probe (`TestClaudeCandidateLiveProbe*`, the
`NewCandidateAdapter` cases) keep their bodies and requalify `probeCapabilities`/`TestedVersion`.
The moved tests exercise the adapter's default `osCommandRunner` only behind the live env gate
(`SYMMETRY_CLAUDE_*`, skipped by default), so no host client is required.

**`claude_probe_test.go` probe cases (8)** — the subject `probeClaudeExecutable` is deleted; each
case is retargeted to the production path that carries the same assertion, with the same name,
fixtures and assertions:

| Case (base line) | HEAD call | Why the assertion survives |
| --- | --- | --- |
| `TestClaudeProbeKnownVersionAndHelpFailsClosed` (:19) | `probeCapabilities` (fixture without a digest) | fail-closed tail: `capabilitiesForPlatform` rejects with `ErrNativeUnverified` on every platform when the blob is unverified; `assertKnownVersionProjection` inputs unchanged; `runner.calls` unchanged (the digest seam is not a `Run`) |
| `TestClaudeProbeHelpWithoutJSONSchemaFailsClosed` (:41) | `probeCapabilities` | same `ErrNativeUnverified` naming `--json-schema`, from the moved evidence gate |
| `TestClaudeProbeUnknownVersionFailsClosed` (:56) | `probeCapabilities` | `ErrUnsupportedVersion`, version projection and `["--version"]` call pin unchanged |
| `TestClaudeProbeAbsentExecutableIsUnavailable` (:74) | `probeCapabilities` | `AvailabilityError` → `ErrHarnessUnavailable`, no `ErrNativeUnverified`, `evidenceCapabilities` start reason text |
| `TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence` (:91) | `NewCandidateAdapter("claude")` with `probe` set to the promotion gate over the same fixture | `Start` returns the probe's `ErrNativeUnverified`; same shape as the composed adapter |
| `TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation` (:107) | `probeCapabilities` | local deadline and caller cancellation still surface from `probeNativeEvidence` |
| `TestClaudeProbeRejectsSuccessAfterProbeDeadline` (:165) | `probeCapabilities` | `DeadlineExceeded`, no capability from success-after-deadline output |
| `TestClaudeProbeRejectsNilContextBeforeTheSharedGuard` (:178) | `probeCapabilities` | entry-level `"Claude probe context must not be nil"`, runner never called |
| `TestClaudeProbeDoesNotParseFailedCommandOutput` (:192) | `probeCapabilities` | failed output is never parsed into evidence |

`TestBoundedProbeRejectsSuccessAfterDeadline` (:152) does not move: its subject is root
`RunBoundedProbe`, which stays, and `bounded_probe_test.go` (unowned) covers the caller-cancellation
arm, not the deadline arm. It stays in the trimmed root `claude_probe_test.go`.

**Root `harness_test.go`** (`TestRegistryKeepsUnavailableCapabilitiesExplicit`): the codex, claude
and pi rows and the two shadow registrations (`:58`, `:61`) are removed; the remaining OpenCode row
keeps the whole fail-closed body, which is the production refusal this file can still reach (the
native kinds move behind adapter packages that import root, so root tests cannot compose them).
The same per-kind unavailable projection is carried at HEAD by
`TestStartupProbeRegistersUnavailableNativeProjections` rows (claude, pi) plus the pre-existing
omp/omo rows. `unavailableProbeRunner`, `knownVersionProjection` and
`assertKnownVersionProjection` lose every caller and are deleted.

**Root `codex_probe_test.go`**: five of the six `probeCodexExecutableWithRunner` cases (`:45`, `:64`,
`:110`, `:130`, `:144`) and their `codexFixtureRunner`/`codexFixtureResult` fixtures are deleted;
`TestProbeRejectsNonExactVersionOutput` (`:85`) is retargeted instead, because its first assertion
targets the kept root export `ValidateCodexVersionOutput` — its transcript table, subtest structure
and validator assertion stay, and only the deleted-seam half is dropped (the seam's "stops before the
help probe" pin is carried by the pre-existing `codex/probe_test.go`
`TestProbeUnknownVersionFailsClosed`). The two `ParseCodexVersion` cases stay. Each deleted case's
mutant is injected on the shared
path the case actually pinned (`ParseCodexVersion`, `ValidateCodexVersionOutput`,
`ValidateCodexHelpOutput`, `ValidateCodexProbeEvidence`, `RunBoundedProbe`) and is killed at HEAD by
a pre-existing test: `codex/probe_test.go` (W3-04's production codex probe) and the kept root
`TestCodexParseVersion*`/`bounded_probe_test.go` rows. `sameStrings` loses its last root caller and
is deleted from root (a copy lives in `claude/probe_test.go`).

**`app/harness_test.go`**:
`TestNewHarnessRegistryBindsNativeAdaptersAndUnsupportedOpenCode` and
`TestComposedClaudeProbeReportsUnavailableMetadata` requalify the Claude constructor/type to
`claude.NewCandidateAdapter`/`*claude.CandidateAdapter`; `TestStartupProbeRegistersUnavailableNativeProjections`
retargets the claude row to `claude.NewCandidateAdapter(filepath.Join(t.TempDir(), "absent-claude.exe"))`
and the pi row to `pi.NewAdapter(filepath.Join(t.TempDir(), "absent-pi.exe"))`, exactly the
omp/omo row pattern; the then-unused `absentClaudeRunner` fixture is deleted. Row expectations
(`adapterVersion`, unavailable metadata) are unchanged and re-verified by the run.

## 5. Evidence plan (judging artifact)

`docs/evidence/rf-w4-15c-claude-candidate-relocation-2026-11-22.md` carries: subject file hashes;
the identifier move table with call sites; the deleted-declaration list with last callers and the
`unused` output; `go list -deps` output; the test-name before/after list (`go test -list`, every
moved name exactly once, deleted names listed with reason); the error-string byte-compare
(`rg -o '"[^"]*"'` multiset, base vs HEAD, over the moved code) and the pinned-string table; the
base-vs-HEAD control-flow audit for `Probe`/`Start`/`Close`/`closeOnce` (normalized body diff,
identifier mapping applied, no early return or permanence decision changed); red-mutant records
(close permanence through the exported constructor; each retargeted/deleted shadow-subject case);
the build matrix; the platform suite outputs with the SKIP list; the citation check; and the
`wc -l` cap table.

Gates (foreground, in `daemon/`): `gofmt -l internal/harness internal/app` empty;
`go vet ./...`; `GOOS=linux|windows|darwin go build ./...` and `GOOS=linux GOARCH=arm64 go build
./...`; `go list -deps` check; `GOOS=linux golangci-lint run --config .golangci.yml --enable-only
unused ./internal/harness/...` (2.15.0) clean; Windows `go test -count=1
./internal/harness/... ./internal/app/...`; WSL `go test -count=1 ./internal/harness/...
./internal/app/...` in this worktree (`/mnt/q/repos/wt-w4-15c-claude-relocation/symmetry/daemon` —
the contract's `/mnt/q/repos/symmetry/daemon` is the main checkout, not this worktree);
`git diff --name-only` shows no `opencode/**`, `pi/**`, `claude/stream*.go` or `testdata/**`;
`scripts/ci/check-citation-existence.py`; `git diff --check`.

## 6. Decisions, trade-offs, ambiguities

- **Retarget over deletion for the Claude probe cases**: their subject moved with the code, so the
  same assertions keep the same tests at the same names; deletion would have been allowed only with
  a reason and a mutant record, and would have lost real pins (nil-context guard, deadline,
  failed-output rejection).
- **Codex cases deleted, not retargeted** — except `TestProbeRejectsNonExactVersionOutput`, whose
  first assertion does not depend on the deleted seam: the deleted cases' subject
  (`probeCodexExecutableWithRunner`) is the shadow that production overwrote; the production codex
  probe lives in `codex/` (W3-04), which this slice may not edit, so no in-set retarget target exists
  for them. Their shared-path mutants are killed by pre-existing `codex/probe_test.go` cases,
  recorded per case.
- **Root rows removed rather than re-pointed**: the native kinds' adapters import root `harness`, so
  root cannot compose them (import cycle); the app-level composition is the only place that can, and
  it already carries the equivalent rows.
- **Identifier renaming** is limited to what the package boundary forces plus package-style
  shortening of the exported and probe-local names. The move is compiler-checked, and the
  control-flow audit compares the four critical functions base vs HEAD after applying the recorded
  mapping.
- **Comments**: the contract's move rule (bodies change only by package clause, imports,
  qualification and testdata prefix) is binding, so moved comments travel verbatim; only comments
  the move makes stale are corrected. The lexical length of the `PermanentCloseFailure` doc is the
  only new comment. Debt found: `claude/stream.go`'s package comment still says the package "does not
  expose a `harness.Adapter`" — `stream.go` is not owned here, so it is recorded in `docs/TODO.md`
  and the report instead of edited.
- **`docs/design/harness.md`** names the moved behaviour but not the moved file paths or package
  (`claude_candidate_adapter.go`, `registry.go`, `harness/claude` appear nowhere in it), so no
  citation refresh is needed there; the citation script's audited corpus (runbooks,
  `docs/design/quality.md`, `docs/archive/CLOSED-TODO.md`) stays green.
- **`docs/TODO.md` item closed here**: the W1-06 residual "Revalidate and remove
  `harness.NewClaudeAdapterWithRunner` (three test files)" and the decision note that says the
  shadowed root probe paths are "main-reachable … no deletion pass may remove them either". The
  contract's registry trim (arch-decision-2 O13, ACCEPT) supersedes that note: the item moves to
  `docs/archive/CLOSED-TODO.md` naming this slice, and the decision note keeps its recorded bytes
  with a dated supersession sentence, because the repo's TODO rules keep decision notes in place.
- **WSL gate path**: run in this worktree's mount, not the main checkout (recorded deviation).

## 7. Out of scope

The staged driver and A2 launch grant (W4-15b); adapter-owned policy/kind vocabulary (A3, W5);
OpenCode promote-or-drop (W6-04); fixture pruning (W5-10); the codex adapter split (W5-16);
`claude/stream.go` decoding (W4-15a); `opencode/**`, `pi/**`, other adapters, `testdata/**`,
`generic.go`, `types.go` and the 5a helper files other than `close_gate.go`.

## 8. Verification limits

The live Claude smoke tests (`adapter_live_test.go`, `adapter_native_test.go`) stay gated by the
`SYMMETRY_CLAUDE_*` env variables and are skipped on this host, exactly as at base; promotion
verdicts are proven by the fixture-based promotion tests instead. `go test -race` stays the CI job's
oracle (no cgo on this host for the local Windows run). WSL runs Go 1.27.0 against the Windows
toolchain's 1.27.4; both are the versions the sibling slices recorded.
