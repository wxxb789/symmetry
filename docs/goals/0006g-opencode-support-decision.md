# 0006g - Evidence-backed OpenCode V2 support decision

Resolve whether the pinned OpenCode V2 `2.2.7` binary has an authoritative
native terminal contract that Symmetry can support. The accepted outcome is
either a precisely bounded capability promotion backed by provider-owned
terminal receipts, or an explicit fail-closed unsupported decision with the
blocking upstream behavior preserved as evidence.

OpenCode V1.x is intentionally skipped. Do not add or maintain a V1 adapter;
the V2 HTTP API under `/api/...` and its event stream are the only supported
target for this goal. V1 observations remain historical and must not be used to
claim V2 behavior.

Completion requires exact-binary Linux and Windows observations for success,
provider failure, interruption, continuation after step completion, truncated
responses, cleanup, and usage. `Step.Ended`, idle, HTTP 200, EOF, artifacts, or
synthetic SSE cannot alone establish success.

This goal starts only when both Linux and Windows native runners, the pinned
binary, and any approved credentials are available; the 12-hour bound starts
after all prerequisites are ready. It depends on 0006a and 0006b and produces
one independently mergeable PR. Ambiguous evidence completes only the explicit
unsupported decision, never a capability promotion.

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client. The pin is OpenCode V2 `2.2.7`; all `1.18.x` and other V1
observations are historical only and do not satisfy this goal.

Owner decision 2026-11-14 (resolved prerequisites; these supersede the `2.2.7`
pin above and start the goal's 12-hour bound):

- **The pin advances to OpenCode V2 `2.3.0`**, the latest stable client per the
  2026-11-11 directive. The exact installed binary is
  `C:\Users\lhan\AppData\Local\mise\installs\aqua-anomalyco-opencode\latest\opencode.exe`
  (`opencode v2.3.0`, 257,553,288 bytes, dated 2026-11-13). Every observation
  names that blob's path, size and digest; `1.18.x` observations remain
  historical and cannot satisfy this goal.
- **The Linux surface is `wsl.exe -d Ubuntu-24.04`** (Ubuntu 24.04.5 LTS). The
  host's default WSL distribution is an Alpine image without a Python toolchain
  and is not the Linux surface. Each receipt states the kernel and distribution
  that produced it; Windows observations come from this host.
- **The credentialed provider is the operator's Tencent LiteLLM relay**, model
  `tencent-litellm/deepseek-v4.1-flash-fast`, with the credential held in the
  environment variable `TENCENT_GLM_API_KEY`. Credential locality is part of
  what this goal proves: the value never appears in the repository, a receipt, a
  log line, a request payload or durable state, and the relay base URL is
  discovered from the harness configuration that already uses this key rather
  than guessed. Dummy keys, loopback gateways, model narration and local journal
  tests do not satisfy this goal.
