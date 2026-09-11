# Pi 0.85.1 retained-session restart evidence

Observed on 2026-09-11 at source revision
`d0ce7cf3a671c0da5b6de5b5329ff659a4f52c2e`. The tracked source was clean during
the native runs. Test: `daemon/internal/harness/pi/adapter_native_resume_test.go`,
`TestNativeRepositoryTaskRetainedResume`. SHA-256 of the executed test-file bytes:
`f1b428fda2ec74666816a39a66c3c8a5cb5023304cf654577be627637cb63139`.

| Environment | Result | Test duration |
| --- | --- | --- |
| Windows amd64, mise Pi 0.85.1 standalone executable | PASS | 24.29s |
| Linux amd64, Docker with `--init`, Pi 0.85.1, Go race instrumentation | PASS | 11.31s |

The executable/archive hashes and `golang:1.27.0-bookworm` image ID are unchanged
from the [single-turn repository-work evidence](native-repository-work.md).
Both runs requested `gpt-5.6-terra` with `high` through the fixed
`symmetry-native-loopback` provider. Served identity/effort, gateway upstream
authentication and provider accounting remain unattested. No user credential or
global Pi configuration was copied or changed.

## Reproduction

Use the [repository-task opt-in configuration](README.md#opt-in-repository-task),
including the absolute Pi executable and loopback endpoint. Leave the credential
environment selector unset. From `daemon/`, run on Windows:

```text
go test ./internal/harness/pi -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -v -timeout 5m
```

On Linux, run the same source inside a container with `--init`, repository mount
`/repo`, working directory `/repo/daemon`, and the temporary loopback proxy
described in the single-turn evidence. Enable race instrumentation:

```text
go test -race ./internal/harness/pi -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -v -timeout 5m
```

The container publishes no host port. Its proxy is stopped on exit. The proxy
readiness probe retried an initial connection refusal before the test began;
each native test itself ran once.

## Observed Contract

The first real Pi process performs one bounded repository mutation, returns the
exact strict progress result, reaches native settlement, and passes bounded
`Close/Wait` with the process physically terminated. Its process identity and
native resume handle were written and read back before the turn. The retained
native session file exists, is nonempty and regular, and is under the isolated
session directory after process stop; the test does not manufacture this file.

The test then commits the first artifact in the temporary repository and derives
a second Subject from that new commit. This is a fixture commit, not a native
Pi commit or an acceptance receipt. The second result has a distinct ResultID
and the exact new Subject/hash; both Subjects describe the temporary input
repository, not acceptance of the Symmetry source revision above.

A new adapter starts a second process from the persisted handle. `Open` confirms
the original native ID/file and idle state, and repeated `Open` preserves that
identity. A randomly generated token was supplied only in the first Goal and
not in the first artifact, either expected result, the fixture identity record,
or the second Goal/context. With only the `write` tool enabled, the second turn
writes exactly the remembered token plus a newline to the second artifact.
This observes recovery of useful native history, not merely reuse of metadata.

Each process has its own sink. The test checks decoded native frames, successful
prompt acknowledgement and settlement, exactly one `session_started` and one
strict `task_result`, and no first-result replay in the second sink. After each
turn, HEAD is unchanged by Pi and Git status, including ignored files, contains
only the expected new artifact. The second turn also preserves the first file.

Both process stops reject sink, output, containment and termination errors.
`OutputTruncated=true` marks the runner's termination barrier, not complete
output drain. An in-flight sink must cooperate with context cancellation.

## Supporting Gates

The final ordinary gates used production/test source equivalent to `d0ce7cf`:

| Check | Outcome |
| --- | --- |
| Windows `go test -count=1 -json ./...` | 2138 passed, 17 skipped |
| Linux `go test -race -count=1 -timeout 600s -json ./...`, Docker `--init` | 2142 passed, 17 skipped |
| Windows and Linux `go vet ./...` | PASS |
| Changed Go file `gofmt`; `git diff --check` | PASS |
| Independent scoped review after three-lens simplification | No actionable findings |

The ordinary skip count includes this opt-in test; the enabled native outcomes
are recorded separately above. An earlier Linux full race invocation omitted
the existing `--init` prerequisite and failed two orphan-child containment tests.
Both focused tests and the complete race gate passed after fixing the container
invocation, without code changes or weakened assertions. That earlier failure
is not reported as a passing run.

## Limits

This is a settled-boundary retained restart after bounded process termination,
not graceful Pi shutdown, interruption during work, crash/power-loss recovery,
safe pause, cancellation during a turn, or external-effect exactly-once behavior.
It does not verify daemon admission, workspace-fingerprint ownership, journal
recovery, fencing, Control/PostgreSQL durability, handoff or decision resolution.
The fixture's local handle ID/fingerprint are test metadata, not control-plane
authority. No release capability was promoted; Goal 0006 remains incomplete.
