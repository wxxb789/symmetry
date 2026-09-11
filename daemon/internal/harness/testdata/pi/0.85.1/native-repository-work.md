# Pi 0.85.1 native repository-work evidence

Observed on 2026-09-11 at source revision
`09b5eb32e99d8ff9e74c9874dfb400339db8a929`. The tracked source was clean during
these native runs. The user's untracked mise configuration was not consumed by
the isolated native process or included in this revision.

Test: `daemon/internal/harness/pi/adapter_native_work_test.go`,
`TestNativeRepositoryTask`. SHA-256 of the executed test-file bytes:
`cf7f75aa7c26a2d17f83c7bc3294280e039b825a56810baaafedaf5df98507da`.

| Environment | Result | Test duration |
| --- | --- | --- |
| Windows amd64, mise Pi 0.85.1 standalone executable | PASS | 14.76s |
| Linux amd64, Docker `golang:1.27.0-bookworm`, Pi 0.85.1 standalone executable | PASS | 8.08s |

Windows executable SHA-256:
`2d4d351da30bfe23a473032e66a571b238763565aa93754e74f4a939de13f195`.
Linux archive `pi-linux-x64.tar.gz` SHA-256:
`494e498f47d74d21f40b3386f6a5e921a3d49531a169cab55bbdaca0ea1fe25a`.
Linux image ID:
`sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452`.

## Reproduction

From `daemon/`, configure the opt-in and isolated loopback mode described in
[README.md](README.md#opt-in-repository-task):

```text
SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_EXECUTABLE=<absolute Pi 0.85.1 executable>
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_PROVIDER=symmetry-native-loopback
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_MODEL=gpt-5.6-terra
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL=http://127.0.0.1:4141/v1
```

Leave `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_CREDENTIAL_ENV` unset, then run:

```text
go test ./internal/harness/pi -run '^TestNativeRepositoryTask$' -count=1 -v -timeout 150s
```

Linux used a disposable container with the repository mounted at `/repo` and
working directory `/repo/daemon`. A container-local loopback reverse proxy
forwarded to `host.docker.internal:4141`; no host port was published. The proxy
was stopped with the container. Its startup readiness probe retried an initial
connection refusal before the test started; the test itself ran once.

Both runs requested `gpt-5.6-terra` with `high` through `openai-responses`.
Served model/effort, gateway upstream authentication and provider accounting
were not independently attested. No user credential or global Pi configuration
was copied or changed. The fixed dummy API key is not a credential.

## Proven Scope

The production adapter starts the real binary, records process and native
session identities before the turn, observes decoded native frames, receives
prompt acknowledgement and waits for native settlement. The test asserts one
`session_started` event and one strict semantic result. Required empty result
arrays survive cloning. The result's Subject binds the isolated repository's
baseline; it is not an acceptance receipt for the Symmetry source revision.

The native turn writes the exact requested artifact bytes. HEAD remains
unchanged, and Git status including ignored paths contains only that artifact.
The bounded `Close/Wait` reports termination without sink, output, containment
or termination errors. `OutputTruncated=true` marks the runner's explicit
termination barrier after semantic settlement, not a complete output drain.

## Limits

This is one fresh native turn, not retained resume, crash recovery, cancellation
during work, handoff, decision resolution, daemon/Control/PostgreSQL integration
or accepted work. The isolated repository is not an OS security sandbox. These
runs do not promote release capabilities or complete Goal 0006. The earlier
[transport-only evidence](native-smoke.md) remains a separate historical record.
