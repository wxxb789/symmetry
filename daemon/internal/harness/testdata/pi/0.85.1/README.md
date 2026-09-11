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

## Opt-in repository task

[Recorded repository-work evidence](native-repository-work.md) identifies the
exact source revision and separate Windows/Linux outcomes. It does not replace
the capability acceptance requirements below.

`TestNativeRepositoryTask` exercises one native Pi turn through the configured
provider or loopback gateway in an isolated temporary Git repository, using the
production adapter. It is not a daemon/Control E2E
test and does not exercise or establish runtime capability admission. Release
capabilities remain unverified until their complete acceptance evidence exists.

From `daemon/`, explicitly configure these environment variables:

- `SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1`
- `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_EXECUTABLE`: absolute Pi 0.85.1 executable
- `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_PROVIDER`: the authorized native provider
- `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_MODEL`: the authorized native model
- `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_CREDENTIAL_ENV`: optionally, the exact name
  of one already configured credential variable from Pi 0.85.1's documented
  provider credential allowlist (such as `OPENAI_API_KEY`), not its value
- `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL`: optional numeric loopback
  `http`/`https` endpoint, including its exact API base path

```text
go test ./internal/harness/pi -run '^TestNativeRepositoryTask$' -count=1 -timeout=3m -v
```

The standard credential path copies only the selected allowlisted credential
into its isolated native process; loopback mode copies no credential. Neither
path reads the user's Pi credential/configuration files. Optional extensions,
skills, templates, themes and context files are disabled. The only model tool is
`write`. The temporary repository is a test target, not an OS security sandbox.
Missing configuration, authentication failures and timeouts fail an enabled test.
With the opt-in unset, a skip proves only that the test compiles.
Unsupported platforms outside Linux and Windows always skip, including when
the opt-in is set.

When `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL` is set, the test uses the
fixed custom provider `symmetry-native-loopback` and model `gpt-5.6-terra`, and
passes `--thinking high`. The provider is written to the isolated
`PI_CODING_AGENT_DIR/models.json` with `api: "openai-responses"`, the exact
configured base URL, a fixed nonsecret literal dummy key, one reasoning model,
and `thinkingLevelMap.high: "high"`. This mode requires the provider and model
environment variables to match those exact values and rejects
`SYMMETRY_PI_NATIVE_REPOSITORY_TASK_CREDENTIAL_ENV`; it never copies a user
credential or modifies global Pi configuration. Endpoint validation accepts only
numeric loopback hosts and rejects userinfo, query strings, fragments, opaque or
hostless URLs, IPv6 zones, whitespace, and invalid ports. With the endpoint
unset, the existing provider/credential path is unchanged and no `models.json`
is created. The evidence records the requested model/effort only; the served
model/effort and gateway upstream authentication remain unattested, and this
test does not promote native capabilities or provider accounting.

The intended evidence is the actual file mutation, expected worktree change
(including ignored paths) and unchanged HEAD, schema-valid progress result bound
to the repository baseline, and bounded
`Start/Open/StartTurn/WaitTurn/Close/Wait`. A progress result is not accepted work
or an achieved Goal. Cancellation, retained resume, handoff, provider accounting
and Control/PostgreSQL durability require separate evidence.

The test observes `OutputTruncated=true` at the shared runner's explicit
termination barrier after the acknowledged semantic result. This marker closes
subsequent output delivery so termination cannot wait indefinitely on a sink;
it is not evidence of a full output drain. `SinkError`, `OutputError`,
`ContainmentError`, and `TerminationError` remain rejected. Expected forced-close
exit status and `WaitError` do not invalidate the already acknowledged semantic
result.

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
