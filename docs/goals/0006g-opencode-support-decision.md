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
