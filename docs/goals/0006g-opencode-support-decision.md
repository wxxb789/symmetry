# 0006g - Evidence-backed OpenCode support decision

Resolve whether the pinned OpenCode `1.18.30` binary has an authoritative native
terminal contract that Symmetry can support. The accepted outcome is either a
precisely bounded capability promotion backed by provider-owned terminal
receipts, or an explicit fail-closed unsupported decision with the blocking
upstream behavior preserved as evidence.

Completion requires exact-binary Linux and Windows observations for success,
provider failure, interruption, continuation after step completion, truncated
responses, cleanup, and usage. `Step.Ended`, idle, HTTP 200, EOF, artifacts, or
synthetic SSE cannot alone establish success.

This goal starts only when both Linux and Windows native runners, the pinned
binary, and any approved credentials are available; the 12-hour bound starts
after all prerequisites are ready. It depends on 0006a and 0006b and produces
one independently mergeable PR. Ambiguous evidence completes only the explicit
unsupported decision, never a capability promotion.
