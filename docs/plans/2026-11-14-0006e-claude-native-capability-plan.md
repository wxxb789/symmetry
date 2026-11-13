# 0006e implementation plan — Claude Code native capability

Contract: [docs/goals/0006e-claude-native-capability.md](../goals/0006e-claude-native-capability.md).
Shape reference: the merged Codex increment (`a2217b8`, `f7f392a` implementation;
`9cb1e82` archive) and `daemon/internal/harness/testdata/codex/0.168.0/`.

## Measured facts (this host, 2026-11-13)

| Fact | Value | Source |
| --- | --- | --- |
| Installed client | `2.1.323 (Claude Code)` | `claude --version` |
| Tested blob | `sha256:5064e6e0572dca86257363ca04d48653eef20ef27f1bd04b504ed7199ed3fd13`, 367634080 bytes | `sha256sum` of `…/mise/installs/claude-code/2.1.323/claude.exe` |
| PATH shims are different blobs | mise shim `53ca4fc6…`, `~/.local/bin/claude.exe` `85429000…` | `sha256sum` |
| Transport help | `--print`, `--verbose`, `--input-format stream-json`, `--output-format stream-json`, `--json-schema`, `--session-id`, `--permission-mode`, `--permission-prompts`, `--restricted` all advertised | `claude --help` (22494 bytes) |
| Relay | `http://m365.localhost:4141` serves `claude-opus-5.5`/`claude-sonnet-5.5`/`claude-haiku-5.5` etc. | `/v1/models` |
| Default permission mode | **denies** the write that the artifact requires | probe below |

Probe of the current fixed argv (`--print --verbose --input-format stream-json
--output-format stream-json --json-schema <schema> --session-id <uuid>`) against
the exact blob and the local relay, real model, isolated `CLAUDE_CONFIG_DIR`:

```text
system init              session_id=<uuid> (matches --session-id)
tool_use Bash            printf 'hello from claude' > hello.txt
system permission_denied "Output redirection to …\hello.txt needs approval"
tool_use Write           hello.txt = "hello from claude"
system permission_denied "Claude requested permissions to write to …\hello.txt"
tool_use StructuredOutput {"answer":"未能创建 hello.txt…"}   (model reports the denial)
result success           structured_output present
```

So `start`, `events` and structured-output `result` already work against the real
binary; the **artifact is unreachable** under the current permission policy, and
the process stays alive after `result` until closed (bounded `Close` handles it).

## Decision required: the narrow permission policy

The contract requires a real-repository `result` *with its artifact*. Claude Code
grants nothing without an approval path, and this adapter has none
(`docs/design/harness.md`: no approval bridge is claimed; `docs/design/harness.md`
also forbids bypass flags/global allow-all). The design's sanctioned fallback is
"an explicitly configured narrow permission policy". Options:

1. **`--permission-mode acceptEdits` on the fixed argv** (recommended, measured).
   Bounds measured against the exact blob on 2026-11-13: `Write` inside the
   admitted workspace is approved; `Write` outside the session working
   directories is denied; edit-classified `Bash` (for example
   `printf … > hello.txt`) is approved; non-edit `Bash` (for example `curl`) is
   denied with "This command requires approval". One machine value, no bypass
   flag, and the process owner (Windows Job object / Linux owner) stays the
   containment boundary. The native permission classifier is *not* treated as a
   boundary, which is why the receipt records its exact bounds.
2. `--restricted --permission-mode acceptEdits`. Also confines file tools to the
   working directories and ignores user/project settings, but removes the
   code-running tools entirely, so the promoted adapter would no longer be
   "Claude Code doing repository work" and every future method/tool claim needs
   a rethink. Rejected: larger capability deviation than required.
3. Implement the SDK permission-prompt bridge (`--permission-prompts host` +
   `control_request` replies). Rejected here: the goal keeps approval response
   explicitly unsupported, the decoder currently fails closed on
   `control_request` (`ErrControlRequestUnsupported`), and this is a new
   capability needing its own evidence, not a prerequisite of an artifact.
4. `--dangerously-skip-permissions` / `bypassPermissions`. Rejected: bypass flag,
   forbidden by the design and by the recorded 0006b policy.

**Recommendation: option 1**, recorded as an explicit amendment note in
`docs/design/harness.md` (Claude row) and in the 2.1.323 receipt, because it
changes a pinned fixed-argv test
(`TestClaudeCandidateStagesFreshTransportAndBarriers`) and the design's
permission sentence. If the owner rejects it, the artifact clause cannot be
satisfied by any adapter-only change and the promotion must stop at
`start`/`events`/`result` without an artifact — the plan's remaining steps are
unaffected either way.

## Public/API shape

New file `daemon/internal/harness/claude_probe.go` (single promotion authority,
mirroring `internal/harness/codex/probe.go`):

```go
const claudeImplementationVersion = "claude-stream-json-v1"

type claudeProbeEvidence struct {          // no lifecycle, operation or blob claim
    Version              string
    VersionKnown         bool
    TransportVerified    bool
}

// probeClaudeNativeEvidence runs the bounded version/help/blob probe.
func probeClaudeNativeEvidence(ctx, executable string, runner ClaudeCommandRunner) (claudeProbeEvidence, error)

// claudeCapabilitiesForPlatform projects only proved operations.
func claudeCapabilitiesForPlatform(platform string, evidence claudeProbeEvidence, executableHashMatched bool) (Capabilities, error)
```

Semantics (fail closed, every negative leaves `Verified=false` and an explicit
`Unsupported` reason):

- Version gate: `^([0-9]+\.[0-9]+\.[0-9]+) \(Claude Code\)$` anchors the line
  and the captured version is compared for equality with `ClaudeTestedVersion`
  (`2.1.323`) from the bounded merged capture; `2.1.322` or any other string →
  `ErrUnsupportedVersion`.
- Transport gate: help must advertise `--print`, `--output-format`,
  `stream-json`, `--json-schema` (existing `hasClaudeTransportHelp`), else
  `ErrNativeUnverified`.
- Blob gate: SHA-256 of the resolved executable must equal
  `testedClaudeExecutableHash`; unknown or mismatched digest → `ErrNativeUnverified`
  and no operation. Hashing is shared with Codex (one implementation).
- `platform != "windows"` → `ErrNativeUnverified` with the recorded Linux
  conflict (A/B systemd + delegated cgroup-v2 parent + cross-UID peer inspection
  unavailable; Windows evidence is never promoted as Linux support).
- Only on Windows + exact blob: `Verified`, `Start`, `Events`, `Cancel` = true,
  `ImplementationVersion = "claude-stream-json-v1"`, `ProtocolVersion = 1`.
  `Resume`, `Handoff`, `Guidance`, `Pause`, `ApprovalResponse`, `Usage`,
  `HardCostLimit`, `ProviderAccess` stay explicitly unsupported.

`ClaudeCandidateAdapter.Probe` delegates to the same function (injectable probe
seam for tests). The conservative `harness.NewClaudeAdapter*` registry path stays
fail-closed as it is today — matching the Codex layout, where the root-registry
projection never claims a lifecycle the package adapter owns.

## Adapter changes (`claude_candidate_adapter.go`)

1. Fixed argv gains exactly one element: `--permission-mode acceptEdits`
   (option 1). Argv otherwise unchanged; `--json-schema` still carries the
   projected TaskResult schema.
2. `ModelProfile` is accepted instead of rejected. It is the daemon-validated
   admission profile identifier (`admission.model_profile` must equal
   `runtime.agent_profile`), not a native model claim; Codex ignores it the same
   way. Claude's native model identity is reported by `system/init` and is not
   claimed by the projection. The pinned test
   `TestClaudeCandidateRejectsUnmappedModelProfileBeforeStart` is replaced by a
   pin that the admitted profile is accepted and the process starts.
3. Forward `Invocation.LinuxV2Required`/`LinuxV2` for parity with every other
   native adapter (no capability claim; Linux admission stays fail-closed at the
   probe).
4. Registration: `app.go` composes `harness.NewClaudeCandidateAdapter(profile.Command)`
   for `KindClaude`, so the registered adapter is the one whose projection and
   lifecycle are proved. Type/constructor names stay (`ClaudeCandidateAdapter`),
   because renaming touches ~200 test references for no behavioral gain; the doc
   comments are corrected instead.

## Negative controls (recorded, fail closed)

| Control | Where |
| --- | --- |
| version mismatch (`2.1.322 (Claude Code)`) | new probe test → `ErrUnsupportedVersion`, no ops |
| blob mismatch (recorded and real shim digests) | new probe tests → no ops; the live control hashes the real `mise` shim, which reports the same version string |
| executable without `--json-schema`/stream-json help | existing + extended probe test → no ops |
| terminal result without matching `system/init` | existing `TestClaudeCandidateOpenDefersSystemInitUntilFirstTurn` |
| terminal result before `StartTurn` | existing `errClaudeCandidateResultBeforeTurn` path |
| closed or superseded session | new `TestClaudeCandidateRejectsSupersededAndClosedTurns` plus the existing close/cancel/idempotency tests |
| Linux admission | new probe test with `platform != windows` → no ops |

## Implementation deltas from the first draft

- `HashExecutableFile` lives once in `internal/harness/executable_digest.go`; the
  Codex probe now calls it instead of keeping a private copy, so both harnesses
  hash the same way.
- The conservative registry probe (`probeClaudeExecutable`) delegates to the same
  `probeClaudeNativeEvidence` gate and still promotes nothing, mirroring the
  Codex layout where the root-registry projection never claims a lifecycle the
  composed adapter owns. Only the promotion gate hashes the executable, so the
  conservative path pays no hashing cost.
- Live probe tests were added: `TestClaudeCandidateLiveProbePromotesTheTestedBlob`
  (the promotion gate against the real client) and
  `TestClaudeCandidateLiveProbeRejectsDifferentBlob` (the real shim as the
  blob-mismatch control).
- Receipt revision binding is operator-owned and is **done** at the final revision
  `3a6339b` (`fix(claude): treat fence-rejected delivery during an accepted stop
  as shutdown`): the two credentialed smokes were re-executed there with the exact
  blob on a clean worktree, and the receipt's revision-binding and operator
  sections were rewritten from those results. The digest the receipt now binds,
  `git diff --no-color --text 9cb1e82 3a6339b -- '*.go' | sha256sum` =
  `e72f036f634c98443bad0da2496affa7a5d2f980cd94a0ed7ae91ca1c8487dac`, covers the
  12 Go files the range touches (9 modified, 3 added). Every earlier figure quoted
  in this plan or the receipt — `6c144213…`, `0ac83f45…`, and the intermediate
  `41d51da4…` bound to the superseded `5c90a60` — is recorded as superseded, not
  as evidence about the merged revision.

## Evidence plan (real, opt-in, no spend recorded in Git)

New `daemon/internal/harness/claude_candidate_live_test.go`, gated on
`SYMMETRY_CLAUDE_LIVE_SMOKE=1` (mirrors the Codex live smokes):

1. `TestClaudeCandidateLiveRepositoryTurnSmoke`: temp git repo, isolated
   `HOME`/`USERPROFILE`/`CLAUDE_CONFIG_DIR`, credential/base-URL only from
   `SYMMETRY_CLAUDE_LIVE_SMOKE_*` environment values; real adapter `Start` →
   `Open` (no init needed) → `StartTurn` → `WaitTurn` → `Close` → `Wait`;
   asserts the artifact bytes, `protocol.ParseTaskResult` success, contained
   termination, and logs event counts plus init/terminal ordering.
2. `TestClaudeCandidateLiveCancelSmoke`: same launch, cancel after a bounded
   offset while the turn is still active (multi-file goal, one `Write` per file,
   because that is the measured approval and no shell allow rule is configured
   for the smoke), asserting `ControlApplied`, `ResultCancelled`, contained stop.

Commands (from `daemon/`, sequential — never concurrent with the full suite):

```text
SYMMETRY_CLAUDE_LIVE_SMOKE=1 SYMMETRY_CLAUDE_LIVE_SMOKE_EXECUTABLE=<abs claude.exe> \
SYMMETRY_CLAUDE_LIVE_SMOKE_HOME=<isolated home> SYMMETRY_CLAUDE_LIVE_SMOKE_BASE_URL=… \
SYMMETRY_CLAUDE_LIVE_SMOKE_TOKEN=… SYMMETRY_CLAUDE_LIVE_SMOKE_MODEL=… \
  go test ./internal/harness -run '^TestClaudeCandidateLiveRepositoryTurnSmoke$' -count=1 -v -timeout 600s
```

The receipt (`testdata/claude/2.1.323/native-repository-turn-windows-2026-11-13.md`)
records the exact binary, digest, platform, argparse, artifact bytes, result
JSON, cancel receipt, event observations, negative controls and the final commit
Subject the observation was executed at (re-executed after the last code change,
as the Codex 0.168.0 receipt required).

## Testdata

`testdata/claude/2.1.323/`: `version.txt`, `help.txt` (sanitized real capture),
`stream-json.jsonl` (synthetic framing fixture carried from 2.1.322),
`native-repository-turn.jsonl` (sanitized real capture from the smoke),
`fixture-manifest.json` (SHA-256 of every fixture file plus the tested executable
blob, verified by a test), `native-repository-turn-windows-2026-11-13.md`,
`README.md`. `testdata/claude/2.1.322/README.md` gets a historical banner; its
files stay.

## Close-out

`docs/design/harness.md` (Claude row, blob binding, Linux fail-closed, permission
amendment), `docs/goals/README.md` index entry, goal file moved unchanged to
`docs/goals/archive/0006e-claude-native-capability.md`, and `docs/goals/0006-final.md`
only if the new pin makes it stale.

## Out of scope

Resume/handoff/guidance/pause/approval/usage/provider/hard-cost promotion; the
`control_request` approval bridge; Linux admission; renaming `ClaudeCandidate*`;
concurrent-run or multi-turn session reuse; changes to settlement, journal, or
containment code.

## Verification

Commits nothing. Gates from `daemon/`: `gofmt -l .`, `go vet ./...`,
`go test -count=1 -timeout 300s ./...`, `go build ./cmd/symmetry-daemon
./cmd/symmetry-fake-agent`, `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...`;
plus the two opt-in live smokes, run sequentially.

## Independent review pass (2026-11-14)

An independent read-only review of the increment returned six findings; all were
triaged and the confirmed ones fixed before the binding evidence runs:

1. Required fixture set not enforced — `TestClaudeTestdataManifestBindsRecordedDigests`
   now requires exactly `version.txt`, `help.txt`, `stream-json.jsonl` and
   `native-repository-turn.jsonl` plus the pinned executable digest, so a removed
   evidence record fails a test.
2. Ambient settings could widen the permission policy — **not reproduced**: a
   repository-local `deny` was effective, while `allow: ["Bash"]`,
   `allow: ["Bash(*)"]`, `defaultMode: "bypassPermissions"` and an outside-path
   `Write` allow rule all stayed denied. Recorded as a control set in the receipt
   and README instead of changing the argv.
3. In-flight terminal could cross the cancel fence — confirmed and fixed: terminal
   observation rechecks `cancelRequested` under the mutex, with a deterministic
   regression test that fails on the pre-fix revision (`Kind:succeeded` after an
   applied cancel) and passes with the fix.
4. Unpromoted projections carried the promoted implementation identity — fixed:
   `ImplementationVersion` is set only in the promoted branch, pinned by the
   platform table test and by a new composed-adapter app test asserting
   `unavailable` metadata.
5. Credentialed smokes did not verify the binary they execute — fixed: the live
   harness verifies version, transport and the exact blob digest before launching,
   and requires the promoted projection on Windows.
6. Permission documentation over-generalized — fixed: the README and design doc
   state tool-specific measured results and the settings controls, and claim only
   that the mode is not a bypass flag and that the classifier is not a boundary.

The binding evidence runs (turn, cancel, promotion probe, blob control) were
re-executed after these fixes, and the committed capture and receipt numbers come
from that revision.

## Gate-verification hardening

The prescribed gate command surfaced two defects that the live evidence had not:
the new `claude_probe.go` was not gofmt-clean, and `claudeProbeTimeout` (1 s) was
exceeded by the pinned 367 MB client on a loaded host, which failed a live check
and would equally fail a daemon startup (`context.DeadlineExceeded` is not a
tolerated probe sentinel). The bound is now 5 s with the recorded rationale; a
broken, slow or hanging client still fails closed inside it, and the blob-mismatch
control now completes the probe and rejects on the exact-blob reason instead of on
the deadline. Codex keeps its own 1 s bound; only this increment's client was
observed to need more.

## Second review pass and cancellation ownership (2026-11-14)

A second read-only review of the fixed revision confirmed the artifact fixes and
found that cancellation ownership was still incomplete. Three rules were added
to `claude_candidate_adapter.go`, each with a deterministic regression test that
fails on the pre-fix revision:

1. `handleClaudeRecord` treats a result record of a turn the cancellation fence
   already claimed as superseded: it is neither published as success nor allowed
   to classify the turn as failed through terminal-interpretation errors
   (including a missing `structured_output`).
2. `requestClose` no longer claims cancellation for a turn whose terminal (or
   terminal-interpretation failure) was already observed, so a direct `Close`
   cannot make a later `Control(cancel)` answer `applied` for a successful turn.
3. A cancel requested before the deferred `system/init` arrived settles
   `cancelled` instead of failed-missing-init.

The same review recomputed the receipt's digests and found a transcribed
implementation digest (61 of 64 hex characters) and a stale observation window;
both were corrected from the recomputed values. The digest from that pass is not
a claim about the current tree: it bound the revision observed then, and Go files
changed again afterwards, so the receipt's revision-binding section is
operator-owned and is recomputed on the final revision as recorded under
"Implementation deltas from the first draft" above. The evidence runs were
repeated after these Go changes, and the committed capture and receipt numbers
come from that final revision.

Residual limit recorded rather than hidden: a valid terminal that has been
observed but not yet finalized could be classified as failed-missing-result if
the process was reaped in that exact window; the third review pass below closes
the close-path half of it, and the remaining pre-existing settlement window is
not covered by a deterministic test.

## Third review pass (2026-11-14)

A delta review of the fixed revision confirmed the digest and capture binding and
found two further defects, both fixed with deterministic tests that fail on the
pre-fix revision:

1. Removing the false cancellation claim exposed a success-loss path: a terminal
   observed just before a direct `Close` was reclassified as
   `failed/process_failure`, because `closeNormalCandidate` only considered
   `turnFinal`. It now also recognizes a terminal already recorded under the
   mutex (`TestClaudeCandidateDirectClosePreservesPendingTerminalOutcome`
   observed `Kind:failed` pre-fix).
2. `validator.Observe` still ran before the supersession check, so an ignored
   result record's interpretation error was retained and reported by the next
   record of the same chunk, failing a turn the accepted stop owned. Superseded
   result records now bypass the stateful validator
   (`TestClaudeCandidateSupersededResultDoesNotPoisonLaterRecords` observed the
   leaked `missing structured_output` error pre-fix).

Two documentation corrections: the receipt now attributes the recorded artifact
to the tool the committed capture actually contains: `Bash`, whose recorded call
is `printf 'hello from claude' > hello.txt && wc -c hello.txt`, with
`StructuredOutput` used only for the TaskResult channel. (The earlier probe log
is a separate run and shows both a denied `Bash` and a denied `Write`.) The
testdata README distinguishes the shim's exact-blob rejection from the older
client's version rejection.
