# OpenCode V2 2.3.0: explicitly unsupported

This is the unsupported outcome of
[the completion contract](../../../../../../docs/goals/0006g-opencode-support-decision.md),
not a capability promotion. No V1 observation is V2 evidence.

## Exact executable and platforms

- Owner path: `C:\Users\lhan\AppData\Local\mise\installs\aqua-anomalyco-opencode\latest\opencode.exe`.
- Size: 257,553,288 bytes.
- SHA-256: `38153a142354634c82b5cfe6c00b19df15a71e6cf8c17ef5f5c1a4cc684a6917`.
- Windows kernel: `10.0.26300`; distribution: `Windows-11-10.0.26300-SP0`.
- Linux observation surface: `wsl.exe -d Ubuntu-24.04`, Ubuntu 24.04.5 LTS,
  kernel `6.18.58.1-microsoft-standard-WSL2`. The exact blob has PE `MZ` magic.
  Its successful interop version command is a Windows execution, not a Linux
  native lifecycle, ownership or cleanup receipt.

`probe-windows.json` records version/help commands and exit codes.
**The only current Windows receipt is `windows-2026-11-14-r1.json`.** It records
the real relay run, native session/message/event identities, requests without
credentials, terminal events, readbacks and cleanup. Its collector SHA-256 is
`1cee1970f95fd9d5800a3bf7c890d5d21134ff656671fbf0bbab786280cff607`.
`windows-2026-11-14.json` is **superseded history only**, retained unchanged with
its own collector SHA-256
`55501a9f01d3dcd76edbb1983efe0dfe474b38190f6fb07d5cd9558621a6e65e`.
It used the raw-socket SSE reader and the rejected host credential; it is not
current support evidence and is not relabelled as a run of the fixed collector.
`linux-2026-11-14.json` records the exact WSL command, kernel/distribution,
binary identity and all seven unavailable native Linux classes.

## Decisive observations

The configured provider is `tencent-litellm/deepseek-v4.1-flash-fast`. Its HTTPS
relay URL is discovered from the operator's existing OpenCode harness config
and was cross-checked against the existing OMP provider config. For this
re-collection, the credential source was `~/.omp/agent/models.yml`, field
`providers.tencent-litellm.apiKey`, injected into `TENCENT_GLM_API_KEY` only in
the collector's subprocess environment. The inherited host variable was not
used: the operator verified that its value is rejected by the relay with HTTP
401 because the expected `sk-` prefix is missing. Neither credential value is
recorded here or in the receipt. The collector still inherits its credential
only through the environment and retains all credential refusals.

The refreshed run captured `session.execution.succeeded` with a matching
`outcome=succeeded` readback. Interruption now has a native first-token event,
`interrupted=true` and `session.execution.interrupted`, with an
`outcome=interrupted` readback. The continuation attempt reached a second
successful execution terminal, but its read tool was rejected with
`permission.rejected` (`executed=false`): it does not prove successful tool
continuation. No real provider-failure terminal or truncated provider response
was captured in this run. Nonzero native token/cost counters are recorded in
the receipt, not reconciled against provider billing. Missing native Linux and
fault-class evidence still prevents promotion; the decision stays unsupported.

The matrix below compares the superseded run with the only current receipt.
Both the SSE reader and credential source changed, so the changed observations
are not attributed to the parser fix alone.

| Class | Windows before (superseded) | Windows after (current) | Native Linux (unchanged) |
| --- | --- | --- | --- |
| Success | run and failed: relay authentication rejected | observed: native success terminal and matching readback | unavailable: pinned blob is PE, not ELF |
| Provider failure | observed: real `provider.auth`/401, matching terminal/readback | unavailable: no provider failure captured in current run | unavailable: same platform blocker |
| Interruption | run and failed: no first token, `interrupted=false`, failed terminal | observed: first token, `interrupted=true`, interrupted terminal/readback | unavailable: same platform blocker |
| Continuation after step | unavailable: no successful first execution | run and failed: read tool permission rejected, no successful tool continuation | unavailable: same platform blocker |
| Truncated provider response | unavailable: no successful response or approved provider fault injection | unavailable: no approved real-relay fault injection | unavailable: same platform blocker |
| Cleanup | observed: session DELETE/readback and owned process/stream barriers | observed: session DELETE/readback and owned process/stream barriers | unavailable: same platform blocker |
| Usage | observed: zero native counters after failure; billing unverified | observed: nonzero native counters; billable usage and failed-turn accounting unverified | unavailable: same platform blocker |

`HTTP 200`, idle, `Step.Ended`, SSE EOF and files do not establish success.
The old `/api/health` and per-session `/event?after=0` routes returned 404.
The actual `/api/info` returns version/PID; missing authentication returns 401.
Global SSE carries durable aggregate/sequence metadata, but this run does not
prove reconnect/replay or retained-session recovery. No synthetic SSE, dummy
key, local provider gateway, provider narration or journal-only test fills a gap.

`GET /api/model` returns expanded provider settings, including the key in
memory. The witness replaces the exact credential before serializing that
response, refuses credential-bearing payloads/events/logs, and scans its isolated
files before writing. This proves only the recorded scan scope, not an audit of
pre-existing operator state or upstream provider storage.

## Reproduce the observation, not a support claim

From the repository on the authorized Windows host:

Source the credential from `providers.tencent-litellm.apiKey` in
`~/.omp/agent/models.yml` into a collector-only `TENCENT_GLM_API_KEY` environment;
do not print it, persist it, or reuse the rejected inherited host value. The
relay base URL must match the existing OpenCode configuration. Then run:

```text
python3 scripts/ci/opencode-v2-support-witness.py --out .tmp/opencode-v2-windows-final.json
python3 scripts/ci/test-opencode-v2-support-witness.py
```

The collector can exit zero after recording an upstream failure. Its exit code
means collection completed, never that a native turn succeeded. Read the native
events and session outcomes. The first collection had incorrect derived class
labels; they were rejected, the classifier was fixed with regression coverage,
and the final receipt was collected with the corrected classifier. No old
summary is promoted or relabeled as an upstream event.

The F1 re-collection additionally fixes HTTP transfer decoding: SSE lines are
read through `HTTPResponse.readline`, not the raw socket, before JSON parsing.
The 8 MiB capture limit and credential refusal are unchanged. The regression
uses local chunked HTTP responses: one event splits inside its JSON and another
starts immediately after a chunk boundary. Before the reader fix the split
event failed with `JSONDecodeError`; after it both cases pass. The witness test
gate increased from 9 to 11 tests without deleting or weakening existing tests.

Success and interruption are `run and failed` only when their native request
or terminal is captured. A readiness failure before either operation leaves
both classes `unavailable` and preserves the original blocker. The rootless
unavailable-runtime gate requires guidance `unsupported` and usage `unknown`;
legal protocol values such as `next_turn` and `estimated` still fail that gate.

Production uses the existing unavailable registry entry, not this private
diagnostic transport. Every operation remains unsupported; no provider access,
hard cost limit, guidance, pause, approvals, resume or handoff is advertised.
Historical Linux peer-negative fixtures remain security evidence only. The
current Windows positive V1 integration job is retired rather than ported to an
unsupported client. Rootless OpenCode checks the actual unavailable projection
while preserving durable claim, fence, failed transition and no-process effects.
