# Native structured-output smoke, Windows, 2026-09-24

Owner directive 2026-09-24: track latest client.

## Command

From `daemon/`:

```powershell
$env:SYMMETRY_CLAUDE_NATIVE_SMOKE = "1"
$env:SYMMETRY_CLAUDE_NATIVE_SMOKE_EXECUTABLE = "C:\Users\<user>\AppData\Local\mise\installs\claude-code\2.1.281\claude.exe"
go test -count=1 -v -timeout 200s -run TestNativeClaudeCandidateStructuredOutput ./internal/harness/
```

- Binary: `claude.exe` `2.1.281 (Claude Code)`, native PE32+ console executable,
  sha256 `39be063c2512b43347fe7b0ab18c46f1596141701c9c5fc895ddfca9a051067c`.
- Host: Windows 11 Enterprise 10.0.26300, go1.27.0 windows/amd64.
- Environment: `t.TempDir()` workspace, HOME/USERPROFILE and
  `CLAUDE_CONFIG_DIR` under the temp root, `ANTHROPIC_BASE_URL=<httptest fake>`,
  `ANTHROPIC_API_KEY=sk-ant-test-fake`, `DISABLE_TELEMETRY=1`,
  `DISABLE_AUTOUPDATER=1`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`.
  No real provider, credential, or spend.

## Result: FAIL at Open (lifecycle conflict, not a structured-output failure)

```text
=== RUN   TestNativeClaudeCandidateStructuredOutput
    open native Claude candidate (no system/init before the first user frame): context deadline exceeded
    native event native_frame: {"type":"system","subtype":"hook_started",...,"hook_name":"SessionStart:startup",...}
    native event native_frame: {"type":"system","subtype":"hook_response",...,"outcome":"success",...}
--- FAIL: TestNativeClaudeCandidateStructuredOutput (30.13s)
```

The first run used the 120 s overall deadline and failed the same way. Claude
Code 2.1.281, launched by the production `ClaudeCandidateAdapter` with the
`--json-schema` argv, emits only the SessionStart hook frames while stdin is
open and no user frame has been written. It emits no `system/init` within
30 s or 120 s. `Open` requires a matching `system/init` before `StartTurn` may
write the prompt, so the prompt was never written and the structured-output
assertions were not reached. Process
cleanup was bounded; no `claude.exe` with the candidate argv remained.

The sanitized driver captures in this directory, which write the user frame
immediately, show `system/init` after the hook frames once input arrives.
A SessionStart hook ran even with isolated HOME and `CLAUDE_CONFIG_DIR`, so a
machine-level hook configuration may contribute; this was not isolated.

## Conflict and smallest proposed amendment

Conflict: the candidate's init-before-prompt `Open` barrier cannot be
satisfied by Claude Code 2.1.281 stream-json input mode.

Smallest amendment, for independent review and not implemented here: let
`Open` return after process start and the persisted launch identity, then
require the matching `system/init` (same `--session-id`) after the first user
frame and before any assistant or result record is accepted. The terminal
validator's identity binding stays unchanged. An alternative is the
undocumented stdin `initialize` control_request, which needs `control_response`
decoder support.

## What this does not prove

This run does not prove the native end-to-end `--json-schema` path through the
production adapter. That path is shown only by the driver captures and replay
tests. It does not prove real-model tool compliance, resume, cancellation,
usage, repository work, or Linux behavior. The capability projection stays
fail-closed (`ErrNativeUnverified`).
