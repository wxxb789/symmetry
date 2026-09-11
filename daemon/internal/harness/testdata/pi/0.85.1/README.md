# pi 0.85.1 RPC synthetic fixtures

These fixtures model the installed `pi --mode rpc` documentation only. They do
not prove authenticated native execution or advertise a supported adapter.

- `lifecycle.jsonl` demonstrates a correlated `get_state`, an accepted prompt,
  retry-shaped `agent_end` events, and the only final native boundary:
  `agent_settled`.
- `compaction-continuation.jsonl` demonstrates a legal continuation after an
  `agent_end` whose `willRetry` is false; the documented retry flag does not
  cover compaction or queued-continuation runs.
- `version.txt` records the observed local CLI version.

Separate [native transport evidence](native-smoke.md) records real Windows and
Linux `Open/get_state/Close` checks. It does not upgrade these synthetic stream
fixtures or prove credentialed repository work.

## RPC profile argument boundary

The argument contract was inspected against upstream `v0.85.1`, commit
`d981de1229ef899957bbe968bc8dcda02a21f477`, in
`packages/coding-agent/src/cli/args.ts` and `src/main.ts`. The installed Windows
package reports the same version but does not include those TypeScript sources.
This is source inspection, not a credentialed native lifecycle capture.

`--export` executes before RPC dispatch; help, version and model-list options
also leave the staged RPC path. Positional text is parsed as initial input but
is not submitted by this version's RPC branch. Unknown flags, including builtin
options written as `--flag=value`, enter the extension flag parser.

`ValidateRPCProfileArgs` admits only exact configuration options with their
separate values: `--provider`, `--model`, `--models`, `--thinking`, `--session-dir`,
`--tools`/`-t`, `--exclude-tools`/`-xt`, and `--name`/`-n`. Values must be nonblank,
contain no NUL and not start with a dash. Supported switches disable tools,
extensions, skills, prompt templates, themes, context files or project approval;
`--offline` and `--verbose` are also accepted. The corresponding aliases are
covered in `pi/argv_test.go`.

Session selection and RPC mode belong to the daemon. Commands, positional input,
one-shot actions, extension loaders, credentials in argv, unknown flags and
alternate option syntax are rejected before local launch side effects. The app
and adapter use the same validator. Credentials remain in the existing local
environment/configuration path; this guard does not verify extension behavior
loaded through those configurations or advertise native support.
