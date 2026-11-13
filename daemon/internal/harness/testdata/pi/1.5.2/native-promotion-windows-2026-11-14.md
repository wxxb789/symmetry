# pi 1.5.2 Windows promotion evidence

Date: 2026-11-14 03:25–03:40 +08. Platform: Windows 10.0.26300 x64. Scope: the
exact-blob promotion for pi `1.5.2` plus the loopback repository turn,
cancellation, two-process retained resume and the recorded negative controls for
the Symmetry pi adapter.

This record is transport-level adapter evidence. It is **not** credentialed
provider work, not daemon/Control durability, not an exactly-once or
lost-acknowledgement recovery proof, and not Linux support. The unverified
operations and their exact blockers are listed below rather than replaced by
synthetic success.

## Exact binary

- `pi.exe` at `C:\Users\lhan\AppData\Local\mise\installs\pi\1.5.2\pi.exe`,
  129055232 bytes, SHA-256
  `40a062bedd75767e2c3c889265de7c83d64a0ba55c74b60243c2453d97cffd85`.
  `latest` and `global` resolve to the same directory; the `1.5.2` asset is the
  release asset `pi-windows-x64.zip` from
  `https://github.com/earendil-works/pi/releases/download/v1.5.2/` (recorded
  archive SHA-256 `e6adb8cb8c84eca14391ad24b363de9d2be98a89ca3ba3fdee1fa4de32b7f2e8`,
  see [the transport receipt](native-windows-2026-11-11.md)).
- `pi --version` = `1.5.2` (6 bytes including the newline).
- PATH shim at `C:\Users\lhan\AppData\Local\mise\shims\pi.exe`, 298632 bytes,
  SHA-256 `53ca4fc6cfaf2b9b9a9635c54f17e5f4bf6ce599693dc67b17783bf382643850`.
  It reports the same `1.5.2` line and is a different blob, so it promotes
  nothing.
- The Linux `1.5.2` asset is recorded as
  `6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57` in
  [the retained-resume record](native-retained-resume-2026-11-11.md). It bounds
  the opt-in evidence tests on Linux and never promotes: Linux admission needs
  the Linux v2 owner path (delegated cgroup-v2 parent with cross-UID peer
  inspection), which this environment does not provide.
- Re-verified in this run: the installed Windows `pi.exe` blob (hash of the
  resolved file, cross-checked against `sha256sum` and against the value the
  cumulative probe reports). **Not** re-verified in this run, with the blocker:
  the `pi-windows-x64.zip` archive digest and the Linux asset digest are values
  recorded on 2026-11-11 and this environment holds no `1.5.2` archive to
  recompute them from — the only local Linux tarball is a pre-1.5.2 asset
  (`0.85.1`, dated 2026-09-11, `pi` SHA-256 `443bd83f30e4dbc7bac2eed9c6aa2461b9a15016fd555f48c92a0591d028c403`),
  which is why it cannot stand in for the recorded Linux value. A Linux operator
  who cannot reproduce `6a2ac1c5…` gets a fail-closed refusal naming the expected
  blob, not a promotion.
- Measured probe latency of this exact binary on an idle host, 3 runs each:
  `--version` 660/677/712 ms, `--help` 1591/1637/1746 ms. The one-second probe
  bound this increment replaces reported `context.DeadlineExceeded` for the help
  probe, which is not a tolerated sentinel; the bound is now five seconds.

## Committed fixture binding

`fixture-manifest.json` records the executable blob, the promoted version line
and the SHA-256 of every committed fixture in this directory:

| Fixture | SHA-256 |
| --- | --- |
| `version.txt` | `ded1b601e81d31ca1f65c4f3ccf2ebd7992b6390e37bc2a86f3837282f1e00cf` |
| `help.txt` | `47d1839ce42f284c8253ae1cfa5859324e84a21f1cc4c15aae09279aa48aae43` |
| `lifecycle.jsonl` | `406fade872bf31cd64291930e4b9f28be6463553880e6c1a159e8177080e8e9a` |
| `compaction-continuation.jsonl` | `7f2cdf9c1988eff734179a6db16da0bd4bb50199f54ebea0edb8c8c9e3aee3f4` |

`help.txt` is the verbatim `pi --help` capture of the binary above (196 lines,
10883 bytes); it was verified byte-identical to the captured output with `cmp`.
`TestPiTestdataManifestBindsRecordedDigests` requires exactly this fixture set,
recomputes each digest from the committed file, parses the promoted version
through the probe's own version parser, and requires the help capture to satisfy
the same `--mode rpc` transport gate the probe applies.

Recompute any committed digest at the merged revision:

```text
git show <rev>:daemon/internal/harness/testdata/pi/1.5.2/<file> | sha256sum
```

## Revision binding

- Base commit: `bcac19e` (`docs(goals): bind 0006f to the 1.5.2 pin,
  exact-blob promotion and Windows scope`), the revision this increment branched
  from.
- Final revision: `937cf06` (`test(pi): strengthen the blob-mismatch controls
  and drop dead fixture state`). Range digest at that revision:
  `git diff --no-color --text bcac19e 937cf06 -- '*.go' | sha256sum` =
  `3f64d6aaa5f3724baba2d7569a54c3ca4e59a88f591bc63c61ab03992211950e` over 9 Go
  files. A later change to those files voids this binding and the observations
  below.
- Impact of the post-review simplify round (`3ead838..937cf06`): it changed only
  `probe_live_test.go`, `probe_promotion_test.go`, `app/harness_test.go` and this
  testdata README — no production file, so the promotion gate's decision order
  and behaviour are unchanged. The controls that changed were re-observed at the
  final revision instead of being carried over, because their assertions
  themselves changed.
- Operator re-observation at `937cf06`, after the commit: same gateway source and
  endpoint as recorded below — `TestNativeRepositoryTask` PASS 2.76s,
  `TestNativeRepositoryTaskRetainedResume` PASS 6.20s,
  `TestNativeCancellationDrainsInFlightLoopbackRequest` PASS 1.50s,
  `TestPiProbeLivePromotesTheTestedBlobOnlyOnWindows` PASS 2.53s and
  `TestPiProbeLiveRejectsDifferentBlob` PASS 6.50s (`ok … 19.932s`), each logging
  the resolved executable digest `sha256:40a062be…`.
- History — the increment commit `b7495ae`, superseded on the three files listed
  above: range digest `de952479…5a6bf7` and its own re-observation
  (`TestNativeRepositoryTask` PASS 2.78s, `TestNativeRepositoryTaskRetainedResume`
  PASS 4.79s, `TestNativeCancellationDrainsInFlightLoopbackRequest` PASS 1.62s,
  `TestPiProbeLivePromotesTheTestedBlobOnlyOnWindows` PASS 4.42s,
  `TestPiProbeLiveRejectsDifferentBlob` PASS 7.18s). Reproducible history; it does
  not bind the delivered revision.
- Not re-observed at the final revision: every operation in "Unverified in this
  increment" below stays unverified and unbound — no Linux runtime, no
  credentialed provider resource, no Elixir or PostgreSQL runtime on this host.
  The simplify round did not change that exposure, and nothing here should be read
  as covering it.
- Gateway used for both re-observations: source
  `.symmetry/native-linux-tools/loopback-responses-gateway.go`, SHA-256
  `a268b5b2…`, served at `-port 4142` with base URL
  `http://127.0.0.1:4142/v1`; every opt-in run logged the resolved executable
  digest `sha256:40a062be…`.
- Fixture digests at the final revision: `git show 937cf06:daemon/internal/harness/testdata/pi/1.5.2/<file> | sha256sum`
  reproduces `fixture-manifest.json` exactly (`version.txt` `ded1b601e81d31ca…`,
  `help.txt` `47d1839ce42f284c…`, `lifecycle.jsonl` `406fade872bf31cd…`,
  `compaction-continuation.jsonl` `7f2cdf9c1988eff7…`).
- Delivery content binding: each delivered file's SHA-256, reproducible from the
  merged revision with `git show <rev>:<path> | sha256sum` when the file is
  committed unchanged. Any post-review change to these files voids this binding
  and requires re-running the observations below.

| File | SHA-256 |
| --- | --- |
| `daemon/internal/harness/pi/probe.go` | `1bda348969faf4c176890f1d792505c0498b19a18d1f747f5b38781c38f5277d` |
| `daemon/internal/harness/pi/adapter.go` | `8787793e913aeba7066ca25f9e10ff8e96a47ea8982a3cd414e41addd9e2f2b0` |
| `daemon/internal/harness/pi/probe_promotion_test.go` | `e1fa1cad557020e4b7423e0cf8503f8bc9a8b34e4128ccff7dcfa3d58cf88434` |
| `daemon/internal/harness/pi/probe_live_test.go` | `48892d177f736a54975682439e7350e6daadf5533a612ecdf8b24d8d7ec74e13` |
| `daemon/internal/harness/pi/adapter_native_work_test.go` | `8b2b4ab4520a238828a7b8926d0c496874cf0fd5563865f82f71e1023cf3afe3` |
| `daemon/internal/harness/pi/adapter_native_resume_test.go` | `ea51bdfaa4682a252d59127177ae632d9805f76719a2cdac62be5c5bea734060` |
| `daemon/internal/app/harness_test.go` | `04d86952ffb58263c757a482c5c42e7224a5e10f3af5f87671da4267cc9cf693` |
| `daemon/e2e/pi-control-witness/main.go` | `2705134cb6c7574af0ceb38cbc4d30fb296c4d6b9ba0fa80c760f46e92ab3188` |
| `daemon/e2e/pi-control-witness/main_test.go` | `f8ce7614dfe75f9a538e6f81e0e74a8178c118e49b98ff1db72333d89990bacb` |

## Promotion probe (exact blob)

```text
SYMMETRY_PI_NATIVE_PROBE=1 \
SYMMETRY_PI_NATIVE_PROBE_EXECUTABLE=<absolute>\1.5.2\pi.exe \
SYMMETRY_PI_NATIVE_PROBE_SHIM=<absolute>\mise\shims\pi.exe \
  go test ./internal/harness/pi -run '^TestPiProbeLive' -count=1 -v -timeout 3m

--- PASS: TestPiProbeLivePromotesTheTestedBlobOnlyOnWindows (2.62s)
--- PASS: TestPiProbeLiveRejectsDifferentBlob (7.21s)
ok  	github.com/wxxb789/symmetry/daemon/internal/harness/pi	10.297s
```

Observed projection for the tested blob:

```text
{Kind:pi NativeVersion:1.5.2 ImplementationVersion:pi-rpc-v1 ProtocolVersion:1
 VersionKnown:true TransportVerified:true Verified:true Start:true Events:true
 Cancel:true Resume:true Handoff:false Guidance:unsupported Pause:unsupported
 ApprovalResponse:false Usage:unknown HardCostLimit:false ProviderAccess:false
 Unsupported:map[approval_response:… guidance:… handoff:… hard_cost_limit:…
 pause:pi interruption is cancellation, not safe pause provider_access:…
 usage:…]}
```

The control does not assert that the probe returned what it was given: it hashes
the resolved executable with the production `harness.HashExecutableFile`, checks
the digest against the recorded blob, then checks that the projection promoted
on Windows and that the five unproved operations stayed false with reasons.

Blob-mismatch control: the shim's real digest
(`53ca4fc6…`) was read from the shim file, confirmed different from the tested
blob, and both halves failed closed — probing the shim itself returned
`ErrNativeUnverified` with no operation, and probing the tested executable with
the shim's digest as declared bytes returned `ErrNativeUnverified` with the
exact-blob reason while version and transport stayed verified.

## Evidence operations

Loopback gateway: local ignored `.symmetry/native-linux-tools/loopback-responses-gateway.go`,
SHA-256 `a268b5b22b917556ca943c9fc0151e46b070360e146d293c6bff8679c9f2e832`,
started with `-port 4152` (port 4141 is served by an unrelated local relay) and
stopped after the runs. It forwards nothing upstream and answers only the fixed
repository-task prompts.

```text
SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1 \
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_EXECUTABLE=<absolute>\1.5.2\pi.exe \
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_PROVIDER=symmetry-native-loopback \
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_MODEL=gpt-5.6-terra \
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL=http://127.0.0.1:4152/v1 \
  go test ./internal/harness/pi -run '^TestNativeRepositoryTask$' -count=1 -v -timeout 3m
```

```text
--- PASS: TestNativeRepositoryTask (3.18s)
    adapter_native_work_test.go:88: native Pi executable: platform=windows digest=sha256:40a062be…
    adapter_native_work_test.go:105: native Pi loopback request: provider=symmetry-native-loopback model=gpt-5.6-terra thinking=high; served model/effort unknown
    adapter_native_work_test.go:230: native Pi stop: output_truncated=false after acknowledged semantic settlement
ok  	github.com/wxxb789/symmetry/daemon/internal/harness/pi	3.663s
```

```text
SYMMETRY_PI_NATIVE_CANCELLATION=1 \
SYMMETRY_PI_NATIVE_CANCELLATION_EXECUTABLE=<absolute>\1.5.2\pi.exe \
  go test ./internal/harness/pi -run '^TestNativeCancellationDrainsInFlightLoopbackRequest$' -count=1 -v -timeout 3m
```

```text
--- PASS: TestNativeCancellationDrainsInFlightLoopbackRequest (1.64s)
    adapter_native_work_test.go:314: native Pi executable: platform=windows digest=sha256:40a062be…
ok  	github.com/wxxb789/symmetry/daemon/internal/harness/pi	3.098s
```

```text
SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1 … _BASE_URL=http://127.0.0.1:4152/v1 \
  go test ./internal/harness/pi -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -v -timeout 5m
```

```text
--- PASS: TestNativeRepositoryTaskRetainedResume (4.98s)
    adapter_native_resume_test.go:61: native Pi executable: platform=windows digest=sha256:40a062be…
ok  	github.com/wxxb789/symmetry/daemon/internal/harness/pi	5.425s
```

All three runs verify the launched executable before doing any work (version
line plus the recorded platform blob), so the logged digest is the digest of the
binary that produced the observation, not of a shim that forwards to it.

### What each observation compares against

- Repository turn: the expected Subject comes from the production workspace
  manager over the fixture repository (`workspace.MaterializeSubject`), and the
  expected `TaskResult` is rebuilt from that Subject with the production subject
  hash. The test then requires the exact artifact bytes, the canonical result
  through `protocol.ParseTaskResult`, an unchanged HEAD, and the exact porcelain
  status `?? pi-native-evidence.txt`.
- Cancellation: a `write`-only turn is started against a gateway that holds the
  Responses request open. The control asserts an applied cancel receipt, that the
  gateway observed its in-flight request context cancelled and its handler
  returned, and a contained bounded stop. The expectation is the gateway's
  observed lifecycle, not a restatement of the request the adapter sent.
- Retained resume: the first process settles and stops; a second process resumes
  the exact retained session ID and file. The second artifact must contain an
  opaque token the test generated and supplied only in the first prompt, and both
  turns' Subjects are materialized independently at their fixture revisions.
- Probe promotion: the resolved executable digest is computed by the production
  hasher and compared with the recorded blob before any capability is read.

### Session-directory provenance

The retained session file is observed, not assumed: the repository-turn and
retained-resume runs create the session under an isolated
`--session-dir` directory passed in the fixed profile argv, and
`nativePiRetainedAssertHandleInSessionDirectory` requires the native
`sessionFile` returned by `get_state` to resolve inside that exact directory.
Resume admission re-resolves both the directory and the retained file at launch
and rejects a relative directory, a path outside the configured directory, a
symlink escaping it, a non-directory, a duplicate `--session-dir`, an overridden
profile session, resume or mode option, and a missing file. The adapter's own
comment records the residual limit: pi 1.5.2 accepts only a path and reopens it
for append, so a writer with access to the session directory can replace the
leaf or its content after the check; pi exposes no fd/import API that would close
that window.

## Negative controls

```text
go test ./internal/harness/pi -count=1 -v -timeout 5m -run \
  '^(TestProbeRejectsUnknownOrUnanchoredVersionLines|TestProbePromotesOnlyThroughADeclaredExactDigest|TestProbeRejectsExecutableWithoutRPCModeHelp|TestPiCapabilitiesForPlatformPromotesOnlyProvedOperations|TestPiTestdataManifestBindsRecordedDigests|TestAdapterRejectsMissingOrOverriddenPiResumeBeforeLaunch|TestAdapterRejectsPiResumeWithMismatchedOrActiveStateBeforePrompt)$'

ok  	github.com/wxxb789/symmetry/daemon/internal/harness/pi	0.609s   (exit 0, 7 top-level tests, 37 passing subtests)
```

| Control | Result |
| --- | --- |
| version mismatch: `0.87.1`, `1.6.0` | `ErrUnsupportedVersion`, no transport probe, no operation, runner called only for `--version` |
| unanchored version line: `mise: using pi 1.5.2`, `1.5.2 (symmetry shim)` | `ErrUnsupportedVersion` ("unable to parse"), no operation, no digest resolved |
| executable without `--mode <mode> … rpc` | `ErrNativeUnverified`, transport unverified, no operation |
| blob mismatch: declared `mise` shim digest | `ErrNativeUnverified`, exact-blob reason, no operation |
| unverifiable digest: fixture declaring none, while this host resolves the tested blob for `pi` | `ErrNativeUnverified`, no operation — the gate never falls back to hashing the resolved file for a fixture |
| platform: `linux`, `darwin` with the exact tested blob | `ErrNativeUnverified` with the Linux v2 owner-path reason, no operation |
| malformed evidence: transport verified without a known version | capability-contract failure, zero-value projection returned |
| resume handle for an incompatible version (`0.0.0`) | `ErrResumeRejected` before launch |
| stale handle whose retained file is gone, symlinked outside the session directory | `ErrResumeRejected` |
| superseded session identity (mismatched ID or filename) | `ErrResumeRejected` |
| session with another active owner (streaming, compacting, queued) | `ErrResumeRejected` |
| unpromoted projection | carries no implementation identity and an explicit reason for each staged operation |

Daemon-level counterpart, run separately:
`TestComposedPiProbePromotesOnlyTheTestedBlobOnWindows` passed with the tested
blob (registration metadata `1.5.2` / `pi-rpc-v1`, start/events/cancel/resume
projected) and with the foreign blob (registration `1.5.2` /
`unavailable`, no operation).

Caller migration: the Control/Pi admission witness
(`daemon/e2e/pi-control-witness`, built only under
`-tags symmetry_pi_control_e2e`) previously required the real pi probe to *stay*
`ErrNativeUnverified`, which this promotion violates in exactly the
configuration the CI Windows job prepares (CI downloads the archive and verifies
that same `40a062be…` executable digest before running it). The witness now
accepts the fail-closed projection or the promoted operation set this build
proves, rejects any other promotion or hard probe failure, and still presents
its own admission projection. `TestPiControlE2EWitnessProbeContract` (tagged)
pins the four delegate states and fails against the pre-fix witness. The Elixir
Control E2E that drives this witness needs Elixir and PostgreSQL: it is not
executed in this run, so the witness change is verified by the tagged Go test,
`go vet`, and a `GOOS=linux` tagged build only.

## Unverified in this increment (with blockers)

| Operation | Blocker |
| --- | --- |
| Credentialed provider turn and provider identity | No approved non-production provider resource, provider identity or permitted spend exists in this environment; `0006h-credentialed-provider-accounting.md` owns it. The runs above used the operator-supplied loopback gateway, which the goal classifies as a transport-level observation. |
| Linux promotion and Linux v2 retained resume | No installed A/B systemd runtime, no delegated cgroup-v2 parent, no cross-UID `pidfd_getfd`/`fdinfo` peer inspection. A fixture-workspace resume is not clone reuse. |
| Daemon/Control durability, exactly-once effects, retry after a lost stop acknowledgement | Adapter-level runs only; no PostgreSQL/Control admission or crash-recovery harness was exercised. |
| Billable usage classification | pi's RPC usage is not promoted; the loopback gateway synthesizes its own counters, and no real provider accounting was observed. |
| Fresh-session handoff | The daemon handoff path is covered in source, but no handoff observation is bound to this exact blob, so the operation stays unsupported. |
| Guidance (native steer), safe pause, approval response, provider access, hard cost limit | No evidence for the exact blob; each carries an explicit projection reason. |
| Elixir Control/Pi E2E and its PostgreSQL-backed durability assertions | Not executed in this run: no Elixir or PostgreSQL runtime on this host. Its admission witness is the one caller this increment migrates; the tagged Go test, `go vet` and a `GOOS=linux` tagged build cover that change here. |

## Gates

Run by the operator on the increment worktree with
`python3 .tmp/gdd-adapters/run-gates.py --repo Q:/repos/symmetry` (evidence:
`.tmp/gate-evidence-0006f-final.txt`; head `bcac19e`, 18 dirty paths, host
windows, `CGO_ENABLED=0`). That run gated the increment content, which the
worktree held clean at commit `b7495ae`; the identical chain was re-run after the
simplify round (`.tmp/gate-evidence-0006f-postapply.txt`, ALL GREEN) and its
content is the delivered `937cf06`.

| command | exit | seconds |
| --- | --- | --- |
| `gofmt -l .` (daemon) | 0 | 0.4 |
| `go vet ./...` | 0 | 2.9 |
| `go test ./... -count=1 -timeout 300s` | 0 | 122.8 |
| `go build ./cmd/symmetry-daemon ./cmd/symmetry-fake-agent` | 0 | 2.8 |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` | 0 | 2.5 |

result: ALL GREEN. `go test` covered every package of the module, including
`internal/harness/pi` (12.8s) and `internal/app` (117.9s). The opt-in
credentialed smokes and the live Pi controls stayed skipped without their
environment; `-race` is unavailable on this host (no cgo toolchain) and the CI
ubuntu `daemon` job owns race coverage, so no weaker substitute was used here.
