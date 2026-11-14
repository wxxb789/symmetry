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
`windows-2026-11-14.json` records the real relay run, native session/message/event
identities, requests without credentials, terminal events, readbacks and cleanup.
`linux-2026-11-14.json` records the exact WSL command, kernel/distribution,
binary identity and all seven unavailable native Linux classes.

## Decisive observations

The configured provider is `tencent-litellm/deepseek-v4.1-flash-fast`. Its HTTPS
relay URL is discovered from the operator's existing OpenCode harness config
and was cross-checked against the existing OMP provider config. The credential
comes only from `TENCENT_GLM_API_KEY`; neither the config source credential nor
the environment value is copied into a request body or evidence file.

The real relay returned HTTP 401. Native `session.step.failed` and
`session.execution.failed` carry `provider.auth`; matching session readbacks
contain `outcome=failed` and zero token/cost counters. These are native failure
receipts, not success. The native execution-terminal event exists; lack of
success and the missing platform/fault classes prevent its promotion.

| Class | Windows | Native Linux |
| --- | --- | --- |
| Success | run and failed: relay authentication rejected | unavailable: pinned blob is PE, not ELF |
| Provider failure | observed: real `provider.auth`/401, matching terminal/readback | unavailable: same platform blocker |
| Interruption | run and failed: no first token, `interrupted=false`, failed terminal | unavailable: same platform blocker |
| Continuation after step | unavailable: no successful first execution | unavailable: same platform blocker |
| Truncated provider response | unavailable: no successful response or approved provider fault injection | unavailable: same platform blocker |
| Cleanup | session DELETE/readback and owned process/stream barriers recorded | unavailable: same platform blocker |
| Usage | zero native counters observed after failure; successful/billable usage unverified | unavailable: same platform blocker |

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

Production uses the existing unavailable registry entry, not this private
diagnostic transport. Every operation remains unsupported; no provider access,
hard cost limit, guidance, pause, approvals, resume or handoff is advertised.
Historical Linux peer-negative fixtures remain security evidence only. The
current Windows positive V1 integration job is retired rather than ported to an
unsupported client. Rootless OpenCode checks the actual unavailable projection
while preserving durable claim, fence, failed transition and no-process effects.
