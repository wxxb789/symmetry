# Claude Code 2.1.322 evidence

Owner directive 2026-11-11: until the first prototype and release, track the
latest stable client. This directory supersedes `../2.1.281/` as the tested
Claude Code version; 2.1.259 and 2.1.281 observations are historical only.

## Files

- `version.txt` is the `--version` output of the installed Windows `claude.exe`
  2.1.322 (sha256
  `345996b4bdbae21d817f750fae2f34cbf8d81224a5444c78136091d121beb298`),
  captured 2026-11-11.
- `stream-json.jsonl` is the synthetic, Multica-derived framing fixture moved
  unchanged from `../2.1.281/`. The stream-json envelope and terminal-result
  checks are unchanged; identifiers, messages and usage values are invented.

[Windows native structured-output
evidence](native-structured-output-windows-2026-11-11.md) records the exact
2.1.322 binary and the passing production candidate against a loopback fake
Messages API, including the deferred `system/init` behavior.

The sanitized real 2.1.281 captures
(`structured-output-*.jsonl`) and the 2.1.281 native Windows receipt remain in
`../2.1.281/`. They are historical decoder fixtures and do not prove 2.1.322
native behavior. Native repository, resume, cancellation and Linux evidence
remain pending under Goal 0006e.

## What this does not prove

These files do not prove authenticated native execution, real-model tool
compliance, session resume, cancellation, guidance, approvals, usage semantics,
repository work, or Linux behavior. The adapter probe still returns the
unverified capability projection until exact-binary evidence is recorded.
