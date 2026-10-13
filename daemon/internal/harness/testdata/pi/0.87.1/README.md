# pi 0.87.1 RPC synthetic fixtures

The tested pin moved from `0.85.1` to `0.87.1` by owner directive 2026-09-24:
track latest client. Probing still returns `ErrNativeUnverified` and promotes no
capability. A retained session recorded with native version `0.85.1` is now
rejected on resume, which is the intended fail-closed behavior.

These fixtures model the documented `pi --mode rpc` stream only. They do not
prove authenticated native execution or advertise a supported adapter.

- `lifecycle.jsonl` demonstrates a correlated `get_state`, an accepted prompt,
  retry-shaped `agent_end` events, and the only final native boundary:
  `agent_settled`.
- `compaction-continuation.jsonl` demonstrates a legal continuation after an
  `agent_end` whose `willRetry` is false.
- `version.txt` records `pi --version` from the installed 0.87.1 binary.

Both JSONL files were moved unchanged from `../0.85.1/` after checking that the
surface they model did not change between the two releases:

- upstream `packages/coding-agent/src/modes/rpc/rpc-types.ts` is byte-identical
  at `v0.85.1` and `v0.87.1`;
- the `AgentSessionEvent` union in `src/core/agent-session.ts` is identical, and
  `AgentEvent` in `packages/agent/src/types.ts` differs only in one comment;
- the extension `ToolDefinition` interface is identical;
- `pi --help` for 0.87.1 still lists `--mode <mode>` with `rpc` and every flag
  the adapter passes or admits (`ValidateRPCProfileArgs`, `--session`,
  `--extension`, `--no-extensions`).

Historical real-run records for 0.85.1 stay in `../0.85.1/`. They are evidence
for that binary only. The opt-in test descriptions and environment variables in
`../0.85.1/README.md` still apply; substitute a 0.87.1 executable.

[Windows native evidence for 0.87.1](native-windows-2026-09-24.md) records the
local opt-in runs against this binary. No Linux run against 0.87.1 has been
recorded yet.

## Documented state events

0.87.1 `docs/json.md` documents `entry_appended`, `session_info_changed` and
`thinking_level_changed`. All three already existed in 0.85.1. In 0.87.1,
`entry_appended` has new emit sites: auto-retry and overflow recovery append a
context-edit entry, and prompt-cache warming appends a usage entry. `pi/rpc.go`
therefore allowlists all three as non-terminal records that play no part in
settlement. Unknown events still fail the session.
