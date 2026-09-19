# Claude Code 2.1.259 probe evidence

These files record local executable and CLI-help evidence only. They do not
claim that Claude Code native session, resume, control, approval, usage, or
artifact-recovery semantics are supported by Symmetry.

The observed Windows executable was `claude.exe` version `2.1.259 (Claude
Code)`. Its help advertises `--print`, `--output-format stream-json`,
`--input-format stream-json`, `--resume`, `--session-id`, and
`--permission-prompts`. This proves a potential CLI transport surface, not a
native lifecycle contract. The daemon therefore keeps every executable
capability unsupported and returns `ErrNativeUnverified` for this version.

Future credentialed captures must add sanitized fresh/resume identity,
interleaved stream, cancellation/drain, usage, and retained-artifact recovery
evidence for each claimed platform. Do not treat this probe fixture as a native
smoke test.

`stream-json.jsonl` is synthetic, Multica-derived framing evidence for the
documented Claude Code `stream-json` envelope shape. It covers decoder DTO
handling only; identifiers, messages, result text, and usage values are
invented. It is not a credentialed native capture and cannot verify session
creation, result/usage semantics, controls, resume, or artifact recovery.
