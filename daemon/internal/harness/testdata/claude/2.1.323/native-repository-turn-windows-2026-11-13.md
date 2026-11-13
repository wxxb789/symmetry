# Claude Code 2.1.323 Windows native observation

Date: 2026-11-13 17:17–17:18 UTC / 2026-11-14 01:17–01:18 +08. Platform: Windows
10.0.26300 x64. Scope: exact-blob promotion plus credentialed repository
lifecycle, artifact, structured-output result, cancellation and the recorded
negative controls for the Symmetry Claude Code adapter.

## Exact binary

- Source: locally installed stable Claude Code `2.1.323` (`mise` install;
  `claude doctor` not required for this observation).
- `claude --version`: `2.1.323 (Claude Code)`
- `claude.exe` at
  `C:\Users\lhan\AppData\Local\mise\installs\claude-code\2.1.323\claude.exe`
  SHA-256:
  `5064e6e0572dca86257363ca04d48653eef20ef27f1bd04b504ed7199ed3fd13`
  (367634080 bytes)
- PATH shim at `C:\Users\lhan\AppData\Local\mise\shims\claude.exe` reports the
  same version string but is a different blob:
  `53ca4fc6cfaf2b9b9a9635c54f17e5f4bf6ce599693dc67b17783bf382643850`
  (298632 bytes). `C:\Users\lhan\.local\bin\claude.exe` is a third blob
  (`85429000ec9b8008…`, 219715232 bytes) and reports the older
  `2.1.259 (Claude Code)`.
- Committed fixture digests: `fixture-manifest.json` in this directory, verified
  by `TestClaudeTestdataManifestBindsRecordedDigests`.

## Revision binding

The observation binds committed revisions, not a working tree:

- Base commit: `9cb1e82`, the merge base this increment branched from.
- Final revision: `5c90a60` (`fix(claude): close the review findings on
  cancellation ownership and the smoke Subject`) — the revision every number in
  this receipt describes. It follows the independent review of this increment and
  the simplification pass applied from that review.
- Go implementation digest of the increment's range, computed at the repository
  root as `git diff --no-color --text 9cb1e82 5c90a60 -- '*.go' | sha256sum`:
  `41d51da43c3d3dc44dfc28b42cf4edc0f1a8e00e90c40f1dbeac4192b5749b1d`.
  It covers the 12 Go files the range touches — 9 modified and 3 added
  (`git diff --name-status 9cb1e82 5c90a60 -- '*.go'`).
- **Superseded and not evidence:** the earlier values recorded in this section
  (`6c144213…`) and in the plan (`0ac83f45…`) were computed from a working tree
  (`git diff HEAD` plus untracked-file hashing) that was never committed and
  cannot be reconstructed from the repository; re-running the documented command
  against the committed revision does not reproduce them, and the accompanying
  sentence that no Go file changed after those runs was not true of them. They
  are kept only as a record of that transcription error, and no claim in this
  receipt depends on them.
- The committed capture `native-repository-turn.jsonl`
  (`sha256:c9b48635c0e8a5df2d9ba20b28a28736ab3105451578b6aa19a290c8c1a913f5`)
  is the sanitized stream of the repository-turn run recorded here, normalized to
  LF so the committed bytes and the manifest digest are the same on every
  platform: the capture was written on Windows with CRLF endings, and the
  repository normalizes text to LF. `.gitattributes` now pins `*.txt` and
  `*.jsonl` to `eol=lf` as well: measured on this host, an `eol=lf` rule keeps a
  `core.autocrlf=true` checkout at LF while a bare `text` rule does not, so the
  digest bound here would otherwise fail on the platform this promotion names.

The credentialed runs in the operator section below were executed at that final
revision on a clean worktree, so the adapter revision they exercised is the
revision this digest binds.

## Commands and results

Environment for every invocation: isolated `HOME`/`USERPROFILE`/
`CLAUDE_CONFIG_DIR` under a fresh temp directory, provider endpoint
`SYMMETRY_CLAUDE_LIVE_SMOKE_BASE_URL`, a local dummy relay token, and
`SYMMETRY_CLAUDE_LIVE_SMOKE_MODEL=claude-sonnet-5.5`. Credential values are
interpolated by the smoke environment and never recorded here.

```text
SYMMETRY_CLAUDE_LIVE_SMOKE=1 \
SYMMETRY_CLAUDE_LIVE_SMOKE_EXECUTABLE='…\claude-code\2.1.323\claude.exe' \
SYMMETRY_CLAUDE_LIVE_SMOKE_BASE_URL=… SYMMETRY_CLAUDE_LIVE_SMOKE_TOKEN=… \
SYMMETRY_CLAUDE_LIVE_SMOKE_MODEL=claude-sonnet-5.5 \
  go test ./internal/harness -run '^TestClaudeCandidateLiveRepositoryTurnSmoke$' \
    -count=1 -v -timeout 600s
```

Result: PASS (11.90 s; all four live checks `ok … 41.23s`). Observations:

```text
events=8 frames=6 init_frames=1 terminal_frames=1 event_count=8
artifact hello.txt = "hello from claude"  (exact bytes, no trailing newline)
"kind": "candidate_completion"
"subject": {"resource_id":"11111111-1111-4111-8111-111111111111",
            "commit":"1417dd54a3df2ccd75a21ad7e56fc961f5d92079",
            "tree_digest":"sha256:6f6218abebd9d04ea6792898e6a31ec491630a1d7ddac8c30d1ce553064bade7"}
"subject_hash": "sha256:9a5a8c28da3d00e82940dddbd8d71aa599f37c5c6217677a4f33a1e1ee6d4ccd"
"Process": {"ExitCode":0,"Terminated":true,"OutputTruncated":false,
            "ContainmentError":null,"TerminationError":null,"OutputError":null}
native result frame: subtype "success", terminal_reason "completed",
                    stop_reason "tool_use", num_turns 3, is_error false,
                    permission_denials []
```

The same run's first user frame was written while the process was still open:
`Open` returned the requested UUID and **no** `system/init` had been observed at
that moment (asserted), and the single `system/init` frame preceded the single
terminal `result` frame. That is the deferred-init rule enforced end to end.

```text
SYMMETRY_CLAUDE_LIVE_SMOKE=1 … \
  go test ./internal/harness -run '^TestClaudeCandidateLiveCancelSmoke$' \
    -count=1 -v -timeout 600s
```

Result: PASS (21.52 s).

```text
cancel receipt: outcome=applied message="Claude process termination completed"
"Kind": "cancelled"
"Summary": "Claude process was cancelled"
"Process": {"ExitCode":1,"Terminated":true,"OutputTruncated":true,
            "TerminationError":null,"ContainmentError":null}
events=43 frames=42 init_frames=1
```

The cancel landed while the turn was active: an already-observed terminal makes
`Control` reject with `outcome=rejected`, and this run returned `applied` and a
cancelled result. The native process stayed contained; buffered output was
truncated, which is the documented bounded-stop behavior.

```text
SYMMETRY_CLAUDE_LIVE_SMOKE=1 … SYMMETRY_CLAUDE_LIVE_SMOKE_SHIM='…\mise\shims\claude.exe' \
  go test ./internal/harness -run '^TestClaudeCandidateLiveProbe' -count=1 -v -timeout 300s
```

Result: PASS. `TestClaudeCandidateLiveProbePromotesTheTestedBlob` (2.16 s) hashed
the resolved executable to
`sha256:5064e6e0572dca86257363ca04d48653eef20ef27f1bd04b504ed7199ed3fd13` and
projected:

```text
{Kind:claude NativeVersion:2.1.323 ImplementationVersion:claude-stream-json-v1
 ProtocolVersion:1 VersionKnown:true TransportVerified:true Verified:true
 Start:true Events:true Cancel:true Resume:false Handoff:false
 Guidance:unsupported Pause:unsupported ApprovalResponse:false Usage:unknown
 HardCostLimit:false ProviderAccess:false}
```

`TestClaudeCandidateLiveProbeRejectsDifferentBlob` (3.97 s) recorded two
fail-closed blob controls: the real `mise` shim rejects with
`ErrNativeUnverified` and the exact-blob reason (it reports the same version
string while its bytes differ) and promotes nothing, and the same real client
answering version/help while the resolved digest is the shim's real digest also
rejects with `ErrNativeUnverified` and the exact-blob reason. Unit fixtures cover
the recorded-digest variant. `claudeProbeTimeout` is 5 s per command because the
pinned 367 MB client exceeded one second on a loaded host, which would otherwise
turn host contention into a failed daemon startup.

## Permission policy bounds

The fixed aspirational transport argv carries one permission value,
`--permission-mode acceptEdits`. Because a real repository artifact is impossible
without some policy, the bounds were measured against this exact blob with a real
model before the amendment was recorded:

| Attempt | Observed |
| --- | --- |
| default mode (no permission flag): `Write` in the workspace | denied, `permission_denied` |
| default mode: `Bash printf … > hello.txt` in the workspace | denied, `permission_denied` |
| `acceptEdits`: `Write` in the admitted workspace | allowed |
| `acceptEdits`: `Write` to a path outside the working directories | denied, `permission_denied` |
| `acceptEdits`: `Bash whoami` | allowed |
| `acceptEdits`: `Bash curl …` (network) | denied, "This command requires approval" |
| `acceptEdits`: `Bash echo … > <outside path>` | allowed by the classifier; the tool wrote a mangled name inside the workspace |

The last row is why the native permission layer is not treated as a containment
boundary: the daemon's process owner (Windows Job object, Linux owner path)
remains the boundary, and the admitted workspace is an execution starting point.
In the recorded turn the model created the artifact through
`Bash printf 'hello from claude' > hello.txt` (the committed capture contains
that call, its successful result and the `StructuredOutput` call). Another run of
the same smoke used the `Write` tool for the identical artifact; both tool paths
are approved by the measured `acceptEdits` policy, and both appear in the control
table above.

Ambient-settings scope was measured as a separate control set against the same
blob, because production inherits the operator's home and the admitted repository
could carry its own settings:

| Control | Observed result |
| --- | --- |
| repository-local `.claude/settings.json` `permissions.deny: ["Write"]` | loaded and effective: the `Write` tool was removed from the session |
| repository-local `allow: ["Bash"]` under `acceptEdits`, running `curl` | still denied, "This command requires approval" |
| repository-local `defaultMode: "bypassPermissions"` plus `allow: ["Bash(*)"]` | still denied |
| repository-local allow rule for a `Write` path outside the workspace | still denied |
| operator's real home settings (isolated-config comparison) | no widening observed for the tested command |

So repository-local settings were shown to *remove* tools, and no tested allow
rule, mode override or outside-path allow rule widened the effective policy of
the fixed `--permission-mode acceptEdits`. The credentialed captures were taken
with an isolated `CLAUDE_CONFIG_DIR`; operator-owned user settings are not pinned
by the adapter and remain outside the recorded measurement.

## Cancellation fence

Cancellation and terminal observation share one mutex, and the state recorded
first owns the turn. Three rules enforce that for the Claude adapter, each with a
deterministic regression test that fails on the pre-fix revision:

- A result record that arrives after the cancellation fence is neither published
  as success nor allowed to classify the turn as failed through
  terminal-interpretation errors: `TestClaudeCandidateCancellationFenceOutranksInFlightTerminal`
  observed `Kind:succeeded` after an applied cancel, and
  `TestClaudeCandidateCancelFenceSupersedesInvalidTerminal` observed a failed
  `TaskResult` for a terminal without `structured_output`; both now settle
  cancelled and publish no success event.
- A direct `Close`, which the daemon performs outside `Control`, no longer claims
  cancellation for a turn whose terminal was already observed
  (`TestClaudeCandidateCloseAfterTerminalObservationKeepsCancelRejected` failed
  pre-fix by letting a later `Control(cancel)` answer `applied`).
- A cancel requested before the deferred `system/init` arrived settles cancelled
  instead of failed-missing-init
  (`TestClaudeCandidateCancelBeforeDeferredInitSettlesCancelled`).

Genuine transport, decode, persistence, termination and containment failures keep
their existing failure classification: they never travel through a result
record's interpretation or the missing-init branch.

## Capability decision

Promotion is bound to the exact blob above on Windows: `start`, `events`,
`cancel`, structured-output `result` with its repository artifact, and the
deferred `system/init` barrier. Resume, handoff, native guidance, safe pause,
approval response, provider broker, billable usage and hard cost limits stay
explicitly unsupported; the native `usage`/`total_cost_usd` counters in the
committed capture (`input_tokens 4`, `output_tokens 624`,
`cache_creation_input_tokens 33549`, `cache_read_input_tokens 33402`,
`total_cost_usd 0.0934607`) are provider-reported, not promoted as billable
deltas.

Linux admission stays fail-closed with the recorded conflict: the Linux v2 owner
path requires an installed A/B systemd runtime with a delegated cgroup-v2 parent
and cross-UID peer inspection, which this host does not provide. No Windows
observation is promoted as Linux support.

## Operator re-run at the final revision (2026-11-14)

The increment's final revision is `5c90a60` (`fix(claude): close the review
findings on cancellation ownership and the smoke Subject`). The two credentialed
smokes were re-executed by the operator at that revision, on a clean worktree,
with the same exact executable blob and the cheapest relay model, so the recorded
observation is bound to a commit rather than to a dirty tree:

```text
cd daemon
SYMMETRY_CLAUDE_LIVE_SMOKE=1 \
SYMMETRY_CLAUDE_LIVE_SMOKE_EXECUTABLE='…\claude-code\2.1.323\claude.exe' \
SYMMETRY_CLAUDE_LIVE_SMOKE_BASE_URL=… SYMMETRY_CLAUDE_LIVE_SMOKE_TOKEN=… \
SYMMETRY_CLAUDE_LIVE_SMOKE_MODEL='claude-haiku-5.5' \
  go test ./internal/harness -run '^TestClaudeCandidateLiveRepositoryTurnSmoke$' -count=1 -v -timeout 600s
```

Result: PASS (20.67 s; package `ok … 21.118s`).

```text
derived subject: commit=c1838b0fd8200df739b653945dc58b251d0ba270
  tree_digest=sha256:4dda06dc275c4deff63a2db847773836e90dacaf9017b4b991202f905bd6df39
  hash=sha256:ff2af800ececd5f67f33b71aae7bd825b88ed0a31a85c880ac8bf3bd8434f3b0
  (committed artifacts: README.md, notes.txt; untracked: hello.txt;
   checkout digest=sha256:1a0a73d3e8a2626bcf5c4826da0c7dc4511f585c3ac34933a57873c2e1fd2983)
observations: events=21 frames=19 init_frames=1 terminal_frames=1 event_count=21 exit=0
```

The Subject the turn returns is now compared against the production derivation
over the repository's committed objects — `workspace.Manager.MaterializeSubject`,
the same derivation `verifyNativeTaskResultSubject` compares against — instead of
the constant digest this smoke used before the review. The line above also shows
the digest of the checkout bytes differs from the derived tree digest, which is
the assertion that keeps the oracle from degrading back into a restatement of its
own input.

```text
SYMMETRY_CLAUDE_LIVE_SMOKE=1 … \
  go test ./internal/harness -run '^TestClaudeCandidateLiveCancelSmoke$' -count=1 -v -timeout 600s
```

Result: PASS (22.17 s; package `ok … 23.713s`).

```text
cancel receipt: outcome=applied message="Claude process termination completed"
"Kind": "cancelled"
observations: events=66 frames=65 init_frames=1
```

Model substitution is deliberate and recorded: the original capture used
`claude-sonnet-5.5`, the operator re-run used `claude-haiku-5.5` (the cheapest
relay model that completes the turn). The promoted capability is a property of
the adapter and the exact executable blob, not of the model that happened to run
the turn; the observation above is what this receipt binds, and a different model
would require its own re-run to be claimed.

Gate evidence for the same revision, run by the operator on the final tree:
`gofmt -l .` exit 0, `go vet ./...` exit 0, `go test ./... -count=1 -timeout 300s`
exit 0 (all daemon packages `ok`), `go build ./cmd/symmetry-daemon
./cmd/symmetry-fake-agent` exit 0, `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go
build ./...` exit 0. `go test -race` is not available on this Windows host
(`CGO_ENABLED=0`, no cgo toolchain); the CI ubuntu `daemon` job owns race
coverage.

The sanitized capture `native-repository-turn.jsonl` was normalized from the
Windows capture's CRLF endings to LF before it was committed, and its manifest
digest was recomputed accordingly: the repository normalizes text to LF
(`* text=auto`) and the manifest test reads the bytes in the tree, so a CRLF
digest would have passed on this host and failed on Linux CI.
