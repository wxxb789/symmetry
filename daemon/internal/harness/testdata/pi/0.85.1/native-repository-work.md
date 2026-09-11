# Pi 0.85.1 native repository-work evidence

**Receipt status: post-commit native receipt complete.** The source binding is
the exact parent commit `8150d5b43cb6e5258cd15f6be7d42518e1504286`. Both Linux
and Windows focused runs below used that source revision. This receipt remains
bounded native evidence and does not complete Goal 0006.

## Run Receipt

Test: `daemon/internal/harness/pi/adapter_native_work_test.go`,
`TestNativeRepositoryTask`.

| Item | Value |
| --- | --- |
| Final source revision | `8150d5b43cb6e5258cd15f6be7d42518e1504286` |
| Test-file SHA-256 at final revision | `e99001ea0fbb15c1db9c3d0da6e1364c48fd1edb142f236aef6604e633587194` |
| Adapter-file SHA-256 | `aff3c3e72fd223b812dfc6df0c92c809f6b2c52bf15b8ca71a8053ead522032a` |
| Windows test | PASS after final commit, 9.24s focused test (10.838s Go package total) |
| Linux test | PASS after final commit, approximately 1.45s |

The test and adapter hashes are the exact source bytes at the final commit.

## Reproduction

From `daemon/`, configure the opt-in loopback mode:

```text
SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_EXECUTABLE=<absolute Pi 0.85.1 executable>
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_PROVIDER=symmetry-native-loopback
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_MODEL=gpt-5.6-terra
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL=http://127.0.0.1:4141/v1
```

Leave `SYMMETRY_PI_NATIVE_REPOSITORY_TASK_CREDENTIAL_ENV` unset. The focused
command is:

```text
go test ./internal/harness/pi -run '^TestNativeRepositoryTask$' -count=1 -v -timeout 150s
```

Windows used the real Pi 0.85.1 standalone executable. Linux used the upstream
`v0.85.1` `pi-linux-x64.tar.gz` asset in a disposable `golang:1.27` container.
The Linux container used `--init`, mounted the repository at `/repo`, and ran
from `/repo/daemon`. The container built and started the deterministic
`loopback-responses-gateway` from
`.symmetry/native-linux-tools/loopback-responses-gateway.go`; it listened only
on container-local `127.0.0.1:4141`. No host port was published, no
`host.docker.internal` proxy was used, and the gateway makes no outbound or
upstream requests.

## Artifact Provenance

| Artifact | Provenance |
| --- | --- |
| Windows Pi executable | Pi 0.85.1 standalone executable, SHA-256 `2d4d351da30bfe23a473032e66a571b238763565aa93754e74f4a939de13f195` |
| Linux Pi archive | Upstream release `v0.85.1`, asset `pi-linux-x64.tar.gz`, SHA-256 `494e498f47d74d21f40b3386f6a5e921a3d49531a169cab55bbdaca0ea1fe25a` |
| Linux test image | `golang:1.27`, inspected image ID `sha256:512690a5660563b57d37ecc31129e7f136e831db2aed24a1dbeb8ad7380dc0fa` |

The reported runs requested provider `symmetry-native-loopback`, model
`gpt-5.6-terra`, API `openai-responses`, and thinking level `high`. The
loopback path writes a fixed dummy key into isolated Pi configuration; it does
not copy a user credential or read the user's Pi configuration files. The
gateway is deterministic and container-local, binds only to numeric loopback,
and has outbound/upstream forwarding disabled.

## Observed Contract

The production adapter starts the real Pi binary, persists process and native
session identity before exposing the session, performs `Open`, acknowledges one
native prompt, waits for native settlement, and emits one strict semantic
result. The task writes the exact requested artifact bytes into an isolated
temporary repository. Pi leaves `HEAD` unchanged and the final Git status,
including ignored paths, contains only that artifact. `Close/Wait` reaches the
bounded termination barrier; `OutputTruncated=true` means the runner closed its
delivery barrier after semantic settlement, not that an uncooperative sink was
fully drained.

The repository Subject binds the temporary repository baseline. It is not an
acceptance receipt for this Symmetry source revision, and the progress result
is not accepted work or an achieved Goal.

## Explicit Non-Claims

This receipt does **not** prove or promote:

- the served model or effort, gateway upstream authentication, or provider accounting;
- credentialed production-provider support or Pi release capabilities;
- cancellation during work, crash or power-loss recovery, handoff, or external-effect exactly-once behavior;
- daemon admission, workspace-fingerprint ownership, journal recovery, fencing, or Control/PostgreSQL durability;
- a Control integration test or an end-to-end Goal 0006 acceptance;
- completion of Goal 0006.

The isolated repository is a test target, not an OS security sandbox. The
separate retained-resume receipt is required for its distinct recovery claim.
