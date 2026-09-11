# Pi 0.85.1 retained-session restart evidence

**Receipt status: post-commit native receipt complete.** The source binding is
the exact parent commit `8150d5b43cb6e5258cd15f6be7d42518e1504286`. Both Linux
and Windows retained-resume runs below used that source revision. This receipt
remains bounded native evidence and does not complete Goal 0006.

## Run Receipt

Test: `daemon/internal/harness/pi/adapter_native_resume_test.go`,
`TestNativeRepositoryTaskRetainedResume`.

| Item | Value |
| --- | --- |
| Final source revision | `8150d5b43cb6e5258cd15f6be7d42518e1504286` |
| Retained-resume test-file SHA-256 | `f1b428fda2ec74666816a39a66c3c8a5cb5023304cf654577be627637cb63139` |
| Adapter-file SHA-256 | `aff3c3e72fd223b812dfc6df0c92c809f6b2c52bf15b8ca71a8053ead522032a` |
| Windows test | PASS after final commit, 14.82s focused test (19.149s Go package total) |
| Linux test | PASS after final commit, approximately 2.79s |

The single-turn repository-work receipt uses the same Pi executable, Linux
archive and image provenance recorded in
[native-repository-work.md](native-repository-work.md).

## Reproduction

Use the [repository-task opt-in configuration](README.md#opt-in-repository-task),
including the absolute Pi executable and loopback endpoint. Leave the
credential environment selector unset. From `daemon/`, run on Windows:

```text
go test ./internal/harness/pi -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -v -timeout 5m
```

Run the same source in the Linux container described by the single-turn
receipt, with Docker `--init` and race instrumentation:

```text
go test -race ./internal/harness/pi -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -v -timeout 5m
```

The container publishes no host port. Its deterministic gateway binds only to
numeric loopback inside the container, makes no outbound request, and is
stopped on exit. No user credential or global Pi configuration is copied or
changed.

## Observed Contract

The first real Pi process performs one bounded repository mutation, reaches
native settlement, and passes bounded `Close/Wait` after physical process
termination. Its process identity and native resume handle are persisted and
read back before the first prompt. The retained native session file is a
nonempty regular file under the isolated session directory; the test does not
manufacture it.

The fixture commits the first artifact between turns and derives a second
Subject from that fixture commit. The commit is not credited to Pi and is not
an acceptance receipt. A second adapter/process starts from the persisted
native handle. `Open` returns the exact retained native identity on both opens.

An opaque token is supplied only in the first Goal. It is absent from the first
artifact, expected results, identity record and second Goal/context. With only
the `write` tool enabled, the second turn writes exactly the remembered token
plus a newline to the second artifact. This observes useful native history
across a settled process restart, not merely reuse of metadata.

Each process has its own sink. The test checks decoded native frames, prompt
acknowledgement and settlement, exactly one `session_started` and one strict
`task_result` per turn, and no first-result replay in the second sink. After
each turn, Pi leaves the expected repository state and preserves the first
artifact. `OutputTruncated=true` marks the runner's termination barrier after
semantic settlement, not complete output drain.

## Artifact and Gateway Boundaries

The reported runs requested provider `symmetry-native-loopback`, model `gpt-5.6-terra`,
API `openai-responses`, and thinking level `high`. The gateway is a local-only
loopback service; no upstream authentication, served model/effort, or provider
accounting is attested. The fixed dummy key is not a credential, and no user
credential or global Pi configuration is copied or read.

Linux uses the upstream Pi 0.85.1 archive and `golang:1.27` image with
`--init`; the exact archive and image hashes are recorded in the single-turn
receipt. The container builds and starts the deterministic
`loopback-responses-gateway` from
`.symmetry/native-linux-tools/loopback-responses-gateway.go`, bound only to
container-local `127.0.0.1:4141`. No host port is published, no
`host.docker.internal` proxy is used, and the gateway makes no outbound or
upstream requests. The Windows run uses the real Pi 0.85.1 standalone
executable. These are native binary/process observations, not synthetic
fixture claims.

## Explicit Non-Claims

This receipt does **not** prove or promote:

- graceful native shutdown, interruption during work, safe pause, cancellation during a turn, or crash/power-loss recovery;
- daemon admission, workspace-fingerprint ownership, journal recovery, fencing, Control/PostgreSQL durability, or handoff;
- external-effect exactly-once behavior, provider accounting, gateway upstream authentication, or served model identity;
- release capability admission or Control integration;
- completion of Goal 0006.

The retained handle's local ID and fingerprint are test metadata, not
control-plane authority. Broader Goal 0006 acceptance still requires its
separate evidence matrix.
