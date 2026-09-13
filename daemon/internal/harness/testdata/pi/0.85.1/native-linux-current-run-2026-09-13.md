# Pi 0.85.1 Linux current-run evidence

Observed on 2026-09-13. This receipt records Linux evidence from one current
run for daemon source revision
`a4744f860d4058b619856a21dd222d7a59891f04`. It is limited to the exact
committed source revision and does not alter historical receipts.

## Provenance and isolation

| Item | Value |
| --- | --- |
| Pi release archive | `pi-linux-x64.tar.gz`, version `0.85.1` |
| Pi archive SHA-256 | `494e498f47d74d21f40b3386f6a5e921a3d49531a169cab55bbdaca0ea1fe25a` |
| Test source: repository task | `e99001ea0fbb15c1db9c3d0da6e1364c48fd1edb142f236aef6604e633587194` |
| Test source: retained resume | `f1b428fd2ec74666816a39a66c3c8a5cb5023304cf654577be627637cb63139` |
| Adapter source SHA-256 | `aff3c3e72fd223b812dfc6df0c92c809f6b2c52bf15b8ca71a8053ead522032a` |
| Container image | `golang:1.27`, Go `1.27.1`, `linux/amd64` |
| Image digest | `sha256:512690a5660563b57d37ecc31129e7f136e831db2aed24a1dbeb8ad7380dc0fa` |
| Daemon source revision | `a4744f860d4058b619856a21dd222d7a59891f04` |

The run used an ephemeral Docker container with `--rm --init`. A clean Git
archive of the tested source was mounted read-only. The deterministic loopback
gateway was container-local, had no egress, and no host port was published.
The run used no real provider credentials.

## Passing commands

From `daemon/` in the disposable Linux container:

```text
go test ./internal/harness/pi -run '^TestNativeRepositoryTask$' -count=1 -v
PASS: TestNativeRepositoryTask (1.71s); package (1.726s)

go test -race ./internal/harness/pi -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -v -timeout 5m
PASS: TestNativeRepositoryTaskRetainedResume (3.03s); package (4.100s)
```

The repository task and retained-resume tests used the real Pi 0.85.1 Linux
binary from the verified archive. The loopback gateway remained local to the
container and made no upstream request.

## Limitations

This receipt does not prove real provider or credential execution, provider or
model usage accounting, crash or power-loss recovery, handoff, or
Control/PostgreSQL durability. It is evidence only for the exact committed
source revision above and does not complete Goal 0006 or promote any
unverified capability.
