# Claude Code 2.1.322 Windows structured-output receipt

Date: 2026-11-11. Scope: Windows x64, production `ClaudeCandidateAdapter`
against a loopback fake Messages API. No real model, credential, repository
work, resume, cancellation or Linux behavior is proved.

## Exact binary

- Installed `claude.exe` `2.1.322 (Claude Code)`
- SHA-256:
  `345996b4bdbae21d817f750fae2f34cbf8d81224a5444c78136091d121beb298`

## Result

Source revision `a04c4ab02d1a6a35aa682c5b845133c720d3be61` with the local
2.1.322 adapter changes applied.

```text
SYMMETRY_CLAUDE_NATIVE_SMOKE=1 \
SYMMETRY_CLAUDE_NATIVE_SMOKE_EXECUTABLE=<absolute>/claude.exe \
  go test ./internal/harness -run '^TestNativeClaudeCandidateStructuredOutput$' \
    -count=1 -v -timeout 180s

--- PASS: TestNativeClaudeCandidateStructuredOutput (2.24s)
```

## Deferred system/init

Claude Code 2.1.322 no longer emits `system/init` before the first user frame.
A direct probe with stdin held open produced no stdout until a stream-json user
frame was written; the `system/init` record then carried the requested session
ID. The earlier 2.1.281 assumption therefore failed `Open` with a 30-second
deadline.

The adapter now opens after process readiness without claiming an observed
identity, writes the first user frame on `StartTurn`, and refuses every terminal
result until a matching `system/init` has been observed. The test above proves
the full matching-init path; `TestClaudeCandidateOpenDefersSystemInitUntilFirstTurn`
covers the missing-init and wrong-identity negative paths.
