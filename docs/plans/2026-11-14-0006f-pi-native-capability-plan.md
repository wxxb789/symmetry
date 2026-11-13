# 0006f implementation plan — pi native capability on the exact tested blob

Contract: [docs/goals/archive/0006f-pi-native-capability.md](../goals/archive/0006f-pi-native-capability.md),
including its operator prerequisite audit (2026-11-14) and Linux disposition.
Shape reference: the merged Claude Code increment (`d7d1400`, base `9cb1e82`),
its probe (`daemon/internal/harness/claude_probe.go`), registered testdata
directory (`daemon/internal/harness/testdata/claude/2.1.323/`), receipt and plan.

## Measured facts (this host, 2026-11-14)

| Fact | Value | Source |
| --- | --- | --- |
| Installed client | `1.5.2` (`pi --version` = `1.5.2\n`, 6 bytes) | `pi --version`, `od -c` |
| Tested blob | `sha256:40a062bedd75767e2c3c889265de7c83d64a0ba55c74b60243c2453d97cffd85` (`…/mise/installs/pi/1.5.2/pi.exe`) | `sha256sum` |
| PATH entry actually resolved | `C:\Users\lhan\AppData\Local\mise\installs\pi\latest\pi.exe` (symlink to `1.5.2`) | `type -a pi` |
| PATH shim is a different blob | `…/mise/shims/pi.exe` = `53ca4fc6cfaf2b9b9a9635c54f17e5f4bf6ce599693dc67b17783bf382643850`, same `1.5.2` line | `sha256sum` |
| RPC transport help | `--mode <mode> Output mode: text (default), json, or rpc` in an 10883-byte help screen | `pi --help` |
| Probe latency | `--version` 660–712 ms; `--help` 1591–1746 ms (3 runs each, direct `1.5.2` exe: 1597–1643 ms) | shell loop with `date +%s%N` |
| Loopback gateway source | `sha256:a268b5b22b917556ca943c9fc0151e46b070360e146d293c6bff8679c9f2e832`, local ignored `.symmetry/native-linux-tools/loopback-responses-gateway.go`, `-port` flag (default 4141) | `sha256sum`, file head |
| Port 4141 | already served by the unrelated `m365.localhost` relay, so evidence runs use another numeric loopback port | `curl /v1/models` 200, `/v1/responses` 404 |
| Credential environment | `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `AZURE_OPENAI_API_KEY`, `GEMINI_API_KEY` all unset | shell environment probe (names only, no values) |

Two gate-relevant defects are measured here and fixed by this increment:

1. `pi.probeTimeout` is 1 s, but the pinned `1.5.2` executable answers `--help`
   in ~1.6 s on an idle host. The help probe therefore returns
   `context.DeadlineExceeded`, which is not a tolerated probe sentinel, so a
   daemon startup would fail and no promotion could ever be observed. The bound
   becomes 5 s with this recorded rationale (same defect class as the Claude
   `claudeProbeTimeout` fix).
2. The promotion gate has no digest gate at all today, so any file that prints
   `1.5.2` and a matching help screen is indistinguishable from the tested
   binary — including the mise shim, which is a different blob.

## Prerequisites this environment cannot satisfy (from the contract amendment)

Recorded, never worked around, and no claim in this increment depends on them:

- **Linux native runner.** No A/B systemd runtime, no delegated cgroup-v2
  parent, no cross-UID `pidfd_getfd`/`fdinfo` peer inspection. Linux admission
  stays fail-closed; the platform gate refuses promotion with that reason and no
  Windows observation is promoted as Linux support.
- **Approved credentials, provider identity, permitted spend.** No approved
  non-production provider resource exists in this environment; that work is
  owned by `0006h-credentialed-provider-accounting.md`. The credentialed turn is
  therefore **unverified** in this increment, and its exact blocker is recorded
  in the receipt. Loopback receipts stay transport-level observations.

## Public/API shape

`daemon/internal/harness/pi/probe.go` is and stays the single promotion
authority for pi (one authority per harness, mirroring
`claude_probe.go`/`codex/probe.go`):

```go
const (
    // TestedExecutableHash is the exact Windows pi 1.5.2 blob this build promotes.
    TestedExecutableHash = "sha256:40a062be…"
    // test implementation identity reported with a verified projection
    piImplementationVersion = "pi-rpc-v1"
)

// piProbeEvidence is the shared result of version and transport verification.
// It contains no lifecycle, operation or blob claim.
type piProbeEvidence struct {
    Version           string
    VersionKnown      bool
    TransportVerified bool
}

// piProbeNativeEvidence runs the bounded version/help probe. A failed probe
// returns the evidence gathered so far.
func probePiNativeEvidence(ctx context.Context, executable string, runner CommandRunner) (piProbeEvidence, error)

// piExecutableDigest returns the SHA-256 of the exact executable bytes, or false
// when they cannot be verified. Only a runner that declares the shared
// harness.ExecutableHasher contract can supply a digest.
func piExecutableDigest(ctx context.Context, executable string, runner CommandRunner) (string, bool)

// piCapabilitiesForPlatform projects only operations proved against the exact
// tested blob on the platform the evidence was taken on.
func piCapabilitiesForPlatform(platform string, evidence piProbeEvidence, executableHashMatched bool) (harness.Capabilities, error)

// testedExecutableHash returns the recorded blob for a native platform.
func testedExecutableHash(platform string) (string, bool)
```

`ProbeResult` gains `ExecutableDigest`, `ExecutableHashKnown` and
`NativeSessionVerified`. `Probe` returns `nil` only when the projection is
promoted and `harness.ErrNativeUnverified` otherwise (unchanged error
vocabulary, unchanged fail-closed reasons). `Adapter.Probe` keeps delegating to
`Probe`, so the composed `app.go` registration
(`pi.NewAdapter(profile.Command)`) becomes the promoted path with no new wiring
and no second adapter type.

Static constants promoted for 1.5.2, replaced wholesale by the next pin:

- Windows promoted blob `sha256:40a062be…`.
- Linux recorded blob `sha256:6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57`
  — historical evidence only, never promoted.

### Declared-digest rule

`piExecutableDigest` resolves a digest only from a runner that implements
`harness.ExecutableHasher`. A deterministic fixture without one is unverifiable
and never promotes, even when the host happens to resolve the tested blob for
the same command name. This is stricter than the shared
`harness.ResolveExecutableDigest` host-hashing fallback, and it is what makes
every fixture test deterministic on a host with `pi` installed.

## Edge-case semantics the contract demands (fail closed)

| Condition | Result |
| --- | --- |
| `--version` output not a bare `1.5.2` line (incl. `0.85.1`, `0.87.1`, `2.0.0`, a shim banner) | `ErrUnsupportedVersion`, no transport probe, no operation, `VersionKnown` reflects the parsed value when one exists |
| version probe fails (deadline/cancel) | the context error itself; nothing is claimed |
| version probe fails otherwise | `ErrHarnessUnavailable`-wrapped, `start` reason = "pi version probe failed" |
| help screen without `--mode <mode>` + `rpc` | `ErrNativeUnverified`, `TransportVerified=false`, no operation |
| help probe fails otherwise | `ErrNativeUnverified` |
| resolved digest unknown, unreadable, or ≠ tested Windows blob | `ErrNativeUnverified`, no operation, exact-blob reason |
| `platform != "windows"` (any evidence quality) | `ErrNativeUnverified`, no operation, Linux conflict reason |
| everything matches on Windows | `Verified`, `Start`, `Events`, `Cancel`, `Resume` = true, `ImplementationVersion="pi-rpc-v1"`, `ProtocolVersion=1` |
| any unpromoted projection | `Guidance=unsupported`, `Pause=unsupported`, `Usage=unknown`, `Handoff/Guidance/Pause/ApprovalResponse/Usage/HardCostLimit/ProviderAccess` all carry an explicit `Unsupported` reason; `ImplementationVersion` stays empty |

`Capabilities.Validate()` is called on every branch before returning, so a
malformed projection can never be observed by a caller.

### Promotion scope decision

Promoted: `start`, `events`, `cancel`, `resume` — exactly the operations the
contract's second Done-when clause requires Windows evidence for (repository
task with artifact and structured result, cancellation draining the in-flight
turn, retained resume across a process boundary).

Left unsupported, with reasons: `handoff`, `guidance`, `pause`,
`approval_response`, `provider_access`, `usage`, `hard_cost_limit`.

Promoting `resume` also opens the daemon's `SessionModeResume` admission for pi
(`admissionLaunchFailure` requires `CapabilityResume` for pi, and pi is the only
adapter with an implemented local resume transport). The adapter-level resume
this increment proves is bounded: it does not prove daemon/Control durability,
PostgreSQL recovery, exactly-once effects or lost-acknowledgement recovery,
which the receipt records as unverified. The alternative — leaving resume
unsupported — would make the contract's retained-resume clause unbacked by any
promoted operation, so the operation is promoted and its limits are recorded
instead of hidden.

`handoff` is the one genuinely ambiguous case, and the options are:

1. Promote it (recommended *against* in this increment): the daemon's fresh
   handoff path already exists and is covered in source by the Control E2E, but
   no handoff observation is bound to the exact `1.5.2` blob, so promoting it
   would advertise an operation this run did not prove for the pinned binary.
2. Keep it unsupported with the exact blocker (chosen): the goal's own sentence
   "Every handoff creates a new native session" describes a daemon behavior, not
   a pi-native operation, and the contract's projection clause says only proved
   operations appear.

### Evidence provenance the tests gain

`nativePiRepositoryTaskLoadConfiguration` (shared by the three opt-in evidence
tests) verifies the resolved executable before launching anything:

- `--version` equals `TestedVersion`, and
- the resolved SHA-256 equals the blob recorded for the current platform.

A shim that forwards to another file therefore cannot produce a run that is
later reported as "exact-binary evidence" (the Claude review's finding 5, applied
here before the evidence runs).

## Negative controls (fail closed, recorded)

| Control | Where | Status |
| --- | --- | --- |
| wrong version (`0.87.1`) | new `pi/probe_test.go` test in the shape of `TestClaudeProbeUnknownVersionFailsClosed` | **new** |
| blob mismatch: mise shim digest, and the real shim executable | new promotion table test + live `TestPiProbeLiveRejectsDifferentBlob` | **new** |
| unverifiable digest (fixture without a declared digest) | new promotion table test | **new** |
| executable without `--mode rpc` help | existing `TestProbeMapsBoundedHelpOutputFailureToNativeUnverified`, `TestProbeDoesNotAdvertiseProviderAccessWithoutReleaseEvidence` + new explicit no-RPC-screen test | extended |
| resume handle for an incompatible version | existing `TestAdapterRejectsMissingOrOverriddenPiResumeBeforeLaunch` ("incompatible native version") | existing |
| stale handle whose retained file is gone | existing same test ("missing native file"), `TestAdapterRejectsPiResumeSymlinkOutsideSessionDirectory` | existing |
| superseded session identity (mismatched id/filename) | existing `TestAdapterRejectsPiResumeWithMismatchedOrActiveStateBeforePrompt`, `TestOpenRejectsKnownCompletedProcessBeforeRegisteringRequest` | existing |
| session with another active owner (streaming/compacting/queued) | existing `TestAdapterRejectsPiResumeWithMismatchedOrActiveStateBeforePrompt` | existing |
| non-Windows platform | new promotion table test (`platform != "windows"` → no operation) | **new** |
| unpromoted projection carries no implementation identity | new promotion table test | **new** |

## Evidence plan (real, opt-in, sequential — never concurrent with the suite)

1. Loopback gateway from the recorded source on a free numeric loopback port
   (`go run .symmetry/native-linux-tools/loopback-responses-gateway.go -port <p>`).
2. `SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1` + `…_EXECUTABLE` (absolute `1.5.2`
   blob) + `…_PROVIDER=symmetry-native-loopback` + `…_MODEL=gpt-5.6-terra` +
   `…_BASE_URL=http://127.0.0.1:<p>/v1` →
   `go test ./internal/harness/pi -run '^TestNativeRepositoryTask$' -count=1 -v -timeout 3m`.
   Expected values are derived, not restated: the Subject comes from the
   production workspace manager over the fixture repository, the artifact bytes
   and canonical `TaskResult` are recomputed by the test, and the repository
   HEAD/status are re-read after the turn.
3. `SYMMETRY_PI_NATIVE_CANCELLATION=1` + `…_EXECUTABLE` (in-process gateway;
   isolated from the port above) → `-run '^TestNativeCancellationDrainsInFlightLoopbackRequest$'`.
   The expectation is the observed in-flight gateway request being cancelled and
   its handler returning, plus an applied cancel receipt — not a restatement of
   the request the adapter sent.
4. `-run '^TestNativeRepositoryTaskRetainedResume$'` with the same loopback
   configuration. The second turn must recall a token generated by the test,
   supplied only in the first prompt, and the test recomputes both Subjects.
5. New live probe tests against the real binary and the real shim (promotion and
   blob-mismatch control).
6. Recorded negative controls re-run for the receipt (not overwritten).

Each command's real output, exit code and duration go into the receipt. The
credentialed turn is recorded as **unverified** with the blocker
("no approved provider resource or provider identity in this environment;
`0006h` owns it"), never replaced by a synthetic success.

## Revision binding

The deliverable is uncommitted by instruction, so the final revision does not
exist yet. The receipt therefore records the base commit `bcac19e`, states that
the final revision is the operator's merge commit of this worktree, and records
the binding command to run at merge:

```text
git diff --no-color --text bcac19e <final> -- '*.go' | sha256sum
git show <final>:daemon/internal/harness/testdata/pi/1.5.2/<fixture> | sha256sum
```

No working-tree digest is recorded as evidence: the previous increment had to
retract exactly that class of value, and a worktree digest is not reproducible
from the repository. Fixture digests are recomputed from committed objects with
`git show <rev>:<path>` once the increment is committed; the manifest digest
values themselves are fixed by the content that this plan commits.

## Testdata

`daemon/internal/harness/testdata/pi/1.5.2/`: keep `version.txt`,
`lifecycle.jsonl`, `compaction-continuation.jsonl`; add `help.txt` (sanitized
real capture), `fixture-manifest.json` (exact fixture set + the tested blob,
enforced by a test so a removed record fails), and the 2026-11-14 Windows
receipts. Existing 2026-11-11 receipts stay as historical records with a
superseded banner; `0.85.1`/`0.87.1` READMEs keep their historical framing.

## Close-out

`docs/design/harness.md` (Pi row: promoted blob, promoted operations,
implementation identity, Linux fail-closed), `docs/goals/README.md` index entry
for 0006f, and the goal file moved **unchanged** to
`docs/goals/archive/0006f-pi-native-capability.md`.

## Out of scope (with reasons)

- Linux promotion and Linux v2 retained resume: prerequisites the amendment
  records as unavailable; owned by `0006-final`.
- Credentialed provider turn, provider identity, billable usage accounting:
  owned by `0006h`; recorded as unverified.
- Handoff promotion: no exact-blob observation (see decision above).
- Guidance/steer, safe pause, approval-response bridge, hard cost limits: no
  evidence for the exact blob; stay explicitly unsupported.
- Renaming `pi.Adapter`/`Probe`: no behavioral gain, large test churn.
- Daemon/Control durability, exactly-once effects and lost-acknowledgement
  recovery for pi: no PostgreSQL/Control harness in this increment's evidence;
  recorded as unverified in the receipt rather than claimed.
- Settlement, journal, containment and workspace code: untouched.

## Verification

From `daemon/`, sequentially: `gofmt -l .`, `go vet ./...`,
`go test ./... -count=1 -timeout 300s`, `go build ./cmd/symmetry-daemon
./cmd/symmetry-fake-agent`, `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...`;
plus the opt-in evidence commands above, run after (never concurrently with) the
suite. `-race` is unavailable on this host (no cgo toolchain); the CI `daemon`
job owns race coverage, and no weaker substitute is used here.

## Deltas between this plan and what shipped (2026-11-14)

1. **Promotion gate placement.** The plan put the promotion authority in
   `probe.go`; that is what shipped, with the extra decision that
   `piExecutableDigest` resolves a digest only from a runner that declares
   `harness.ExecutableHasher`. The shared `harness.ResolveExecutableDigest`
   fallback hashes whatever the host resolves, which would have made the existing
   fixture probe tests promote on a host where `pi` is the tested blob. The
   stricter rule keeps every fixture deterministic; the Claude and Codex probes
   are untouched.
2. **`--mode rpc` transport fixture.** `help.txt` was added to the committed
   fixture set (verbatim 196-line capture, verified byte-identical to the
   capture with `cmp`) together with `fixture-manifest.json`. No native JSONL
   capture of the repository turn was added: the pi opt-in tests do not record
   raw frames, and hand-sanitizing one would have produced an unreproducible
   artifact. The manifest instead binds the two retained framing fixtures, the
   version line and the help screen, and a test requires exactly that set.
3. **Historical records stay byte-stable.** The plan said the 2026-11-11
   receipts would carry a superseded banner. Instead the receipts are untouched
   and the supersession is recorded in `testdata/pi/1.5.2/README.md`, so a
   historical evidence file is never rewritten.
4. **Daemon-level projection test added.** Beyond the plan, `daemon/internal/app`
   gained `TestComposedPiProbePromotesOnlyTheTestedBlobOnWindows`, which drives
   `ensureHarnessProbe` + `buildRuntimeRegistration` through both the promoted
   and the foreign-blob projection. Without it nothing exercised the promoted pi
   identity through the daemon's registration surface.
5. **Opt-in evidence precondition.** The plan's "verify the resolved executable
   before launching" is implemented as `nativePiAssertTestedExecutable`, shared
   by all three opt-in tests, replacing their previous version-only check. Its
   Linux branch uses the recorded Linux blob, so a Linux operator can still run
   the adapter-level evidence; promotion itself stays Windows-only.
6. **Manifest fields.** `executable_bytes` was dropped: nothing in the test could
   assert it without knowing the deploy path, and an unasserted field is dead
   weight. The manifest carries the executable blob, the promoted version line
   and the fixture digests.
7. **Promoted operation set.** As planned: start, events, cancel, resume.
   Handoff stays unsupported although the daemon path exists, because no handoff
   observation is bound to this exact blob.
8. **Revision binding form.** The plan promised no worktree digest. The receipt
   records the base commit, the merge-time range command, and a per-file delivery
   content binding reproducible with `git show <rev>:<path> | sha256sum`; the
   final revision itself is recorded as unbound because this run is uncommitted.
9. **Gate environment.** The host shell exports leftover
   `SYMMETRY_CLAUDE_LIVE_SMOKE*` values from the 0006e run. With
   `SYMMETRY_CLAUDE_LIVE_SMOKE` set but `SYMMETRY_CLAUDE_LIVE_SMOKE_SHIM` absent,
   the unrelated Claude blob-mismatch control fails by its own precondition, so
   the gate runs clear those variables (and the pi opt-ins) rather than weakening
   the control.
10. **Probe bound.** The measured 1.59–1.75 s `--help` latency made the
    one-second bound a gate defect, not a promotion choice: the bound is 5 s, and
    the receipt records the measurements.
11. **Build-constrained caller migrated.** `daemon/e2e/pi-control-witness/main.go`
    (build tag `symmetry_pi_control_e2e`) asserted that the real pi probe must
    *stay* `ErrNativeUnverified`; the promotion breaks that assertion in exactly
    the configuration the CI Windows job prepares (the tested blob, downloaded
    and digest-verified by CI itself). The witness now accepts either the
    fail-closed projection or the promoted operation set this build proves,
    rejects any other promotion, and still presents its own admission
    projection. A tagged regression test (`e2e/pi-control-witness/main_test.go`)
    pins the four delegate states and fails against the pre-fix witness. Because
    `go vet ./...` and `go build ./...` skip build-constrained files, the named
    gates for the tagged package are `gofmt`, `go vet -tags`, `go test -tags` and
    a `GOOS=linux` tagged build, all recorded in the receipt.
