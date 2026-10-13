# Claude Code 2.1.281 evidence

Owner directive 2026-09-24: track latest client. This directory supersedes
`../2.1.259/` as the tested Claude Code version.

## Files

- `version.txt` is the `--version` output of the Windows `claude.exe` 2.1.281
  (sha256 `39be063c2512b43347fe7b0ab18c46f1596141701c9c5fc895ddfca9a051067c`),
  captured 2026-09-24 with an isolated `CLAUDE_CONFIG_DIR`. Its `--help` output
  advertises `--print`, `--verbose`, `--input-format stream-json`,
  `--output-format stream-json`, `--session-id <uuid>` and
  `--json-schema <schema>` (inline JSON only, no file variant). The probe now
  requires `--json-schema` in help because the candidate transport depends on it.
- `stream-json.jsonl` is synthetic, Multica-derived framing evidence moved from
  `../2.1.259/`. The envelope framing it models is unchanged on 2.1.281; its
  result record now carries `structured_output` because terminal success
  requires it. Identifiers, messages and usage values are invented.
- `structured-output-task-result-success.jsonl` is a sanitized real 2.1.281
  stream-json capture of `--print --verbose --input-format stream-json
  --output-format stream-json --json-schema <TaskResult schema> --session-id
  <uuid>` against a loopback fake Messages API that answered with one valid
  `StructuredOutput` tool call. The result has `subtype` `success`,
  `terminal_reason` `completed` and a TaskResult object in `structured_output`.
- `structured-output-retry-exhausted.jsonl` is the same transport where the
  fake API returned a schema-invalid TaskResult five times. Claude Code ends
  with `is_error` true, `subtype` `error_max_structured_output_retries`,
  `terminal_reason` `structured_output_retry_exhausted`, an `errors` array, and
  no `result` or `structured_output`.
- `structured-output-missing-after-text-success.jsonl` is the same transport
  where the fake API never called the tool. After one injected
  `[structured-output-enforce]` user turn, Claude Code still ends with
  `subtype` `success` and a text `result` but no `structured_output`. The
  validator must reject this and does (`ErrMissingStructuredOutput`).
- `native-structured-output-windows-2026-09-24.md` records the opt-in native
  smoke run of the production candidate against a Go fake Messages API.

The three `structured-output-*` captures were sanitized: user paths, tool,
MCP, skill, plugin, agent and memory lists in `system/init` are elided, the
fake API key never appears, and session IDs are random. A SessionStart hook
from the capture machine produced the `hook_started`/`hook_response` frames.

## Tool schema projection

Claude Code forwards `--json-schema` verbatim as the `StructuredOutput` tool
`input_schema`. The Anthropic Messages API rejects a tool `input_schema` that has
`oneOf`, `anyOf` or `allOf` at the top level. `claudeCandidateStructuredOutputSchema`
therefore drops the TaskResult root `oneOf`, which couples `kind` with
`proposal` and `reason`. The root object type, properties, required keys,
`additionalProperties: false` and definitions are all kept.
`protocol.ParseTaskResult` still validates the full schema, including that
coupling, before any result counts as success. The `structured-output-*` captures below
were taken with the unprojected schema against a fake API that does not
validate schemas. A rerun with the projected schema was not performed.

## What this does not prove

The fake Messages API is hand-built. These files do not prove real-model tool
compliance, model-fallback retraction behavior, native session resume,
cancellation, guidance, approvals, usage semantics, repository work, or Linux
behavior. The daemon keeps every Claude capability unsupported and still
returns `ErrNativeUnverified` for this version.
