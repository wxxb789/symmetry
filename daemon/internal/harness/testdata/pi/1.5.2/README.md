# pi 1.5.2 RPC synthetic fixtures

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client. The pin is now pi `1.5.2`; evidence for `0.85.1` and
`0.87.1` is historical only.

These fixtures model the documented `pi --mode rpc` stream. They do not prove
authenticated native execution or advertise a supported adapter. Probing still
returns `ErrNativeUnverified` and promotes no capability.

- `lifecycle.jsonl` demonstrates a correlated `get_state`, an accepted prompt,
  retry-shaped `agent_end` events, and the only final native boundary:
  `agent_settled`.
- `compaction-continuation.jsonl` demonstrates a legal continuation after an
  `agent_end` whose `willRetry` is false.
- `version.txt` records `pi --version` from the installed `1.5.2` binary.

Both JSONL files were copied unchanged from `../0.87.1/`. The RPC reference for
`1.5.2` still documents JSONL framing, `agent_settled` as the settled boundary,
and the same non-terminal state events (`entry_appended`,
`session_info_changed`, `thinking_level_changed`).

[Windows native transport evidence](native-windows-2026-11-11.md) records the
exact 1.5.2 asset hashes and passing `Open/get_state/Close` plus provider-bridge
extension checks. [Linux native transport evidence](native-linux-2026-11-11.md)
records the passing `Open/get_state/Close` check on Ubuntu 24.04 WSL2.

[Native retained-resume evidence](native-retained-resume-2026-11-11.md)
records a deterministic loopback two-process resume on Windows and Linux.
Real-provider authentication, daemon/Control durability, cancellation and
Linux v2 clone reuse remain pending under Goal 0006f; the older
`native-windows-2026-09-24.md` receipt belongs to `0.87.1` and is not copied
here as 1.5.2 evidence.

Historical real-run records for `0.85.1` and `0.87.1` stay in their own
directories. The opt-in test descriptions and environment variables in
`../0.85.1/README.md` still apply; substitute a `1.5.2` executable.
