---
name: symmetry-harness-adapter
description: Add or change a native Symmetry Codex, Claude Code, pi or OpenCode adapter, including session recovery, controls, event parsing and verified capability reporting.
---

# Native harness adapter

Read [harness](../../../docs/design/harness.md), the relevant wire contract and
daemon/AGENTS.md. Preserve the fixed transport and shared process/journal owners.

Inspect the installed executable version and official native protocol/help.
Record exact version and sanitized fixtures. Never use fake-agent JSON compatibility
as evidence that a native harness supports Symmetry controls.

Verify only advertised capabilities: start, event ordering, cancellation, session
identity/resume, guidance, approval response and usage. Unsupported operations
stay explicit. Byte delivery does not prove guidance application; native abort
does not prove safe pause; cumulative usage is not a series of billable deltas.

Exercise malformed/interleaved output, process/pipe shutdown, native refusal,
duplicate replay and recovery of retained work. Persist launch/session identities
before acknowledgement. Keep native credentials and session filenames local.

Use real small-repository smoke evidence in addition to replay fixtures for
each claimed platform/version. When access is missing, deliver code and available
checks, state the missing live evidence, and leave support unverified. Do not
change permissions or install another runtime merely to hide a compatibility gap.
