# OpenCode peer ownership verification

Status: **verification recipe** for the private OpenCode transport in
`daemon/internal/harness/opencode`. It describes the loopback peer-ownership
check that transport performs before it uses a connection, and how to re-run the
checks that cover it. It proves transport ownership only: it does not promote
native lifecycle, terminal-result, permission, usage or resume support, and the
registered production adapter stays unavailable — OpenCode 2.3.0 is explicitly
unsupported per
[0006g](../goals/archive/0006g-opencode-support-decision.md), and
[harness.md](../design/harness.md) is the normative capability status.

## Boundary

Before the first HTTP or Basic Auth byte on each new connection, the transport
verifies the exact connected IPv4 loopback peer against the persisted server PID
and process creation identity. A listener on the same port is not sufficient.
IPv6, unsupported operating systems, missing evidence and identity mismatches
fail closed.

Both transport constructors (`NewAdapter`, `NewAdapterWithRunner`) install the
verifier, which runs inside the HTTP transport's dialer: no request header or
Basic Auth byte can be written before the verifier returns. `ErrPeerOwnership` is
not retryable as a health error; an ownership failure preserves its cause, makes
exactly one health attempt, creates no session, and terminates the owned process.
Every new connection is verified, not only the first.

## Platform mechanisms

- **Windows** (v1 verifier): the transport holds the server process handle, checks
  its creation identity and liveness, and matches the established four-tuple and
  owning PID from `GetExtendedTcpTable`, then rechecks process liveness before
  returning.
- **Linux** (v2 verifier, the production path): the launch retains a pidfd and the
  duplicated socket identity; the verifier checks `/proc/<pid>/fdinfo` inodes and
  duplicates the exact socket with `pidfd_getfd`, rechecking the reversed
  four-tuple, the socket inode and the network namespace. Process identity and the
  shared network namespace are checked before and after the inode/FD
  observations, and the tuple/inode are rechecked. Unavailable `pidfd_getfd`
  permission, procfs visibility or launch-retained capability fails closed rather
  than falling back to a port match. The legacy v1 Linux verifier matches the
  reversed established tuple in `/proc/self/net/tcp` to a socket inode held in
  `/proc/<pid>/fd`.
- Native table and fdinfo/fd scans have bounded allocation and scan sizes. Kernel
  calls are synchronous; context cancellation is checked around and within
  scanning work.

The verifier sets a five-second observation deadline (or the shorter caller
deadline) and re-observes the same connection at 10 ms intervals, which
accommodates the Linux connect/accept gap without sending a health request or
redialing. Kernel snapshots are not a defense against a privileged attacker or an
authorized child deliberately transferring its socket: the exact PID is required,
and ownership by an arbitrary descendant is not accepted.

## Run the checks

From `daemon/` on Windows and Linux:

```text
go test -count=3 ./internal/platform ./internal/harness/opencode
go vet ./...
go test ./...
```

On a runner with race instrumentation, also run:

```text
go test -race ./...
```

When using Docker for the Linux gates, pass `docker run --init` so orphaned test
descendants are reaped. Without an init process, a terminated descendant can
remain a zombie and fail the existing process-absence assertion even after its
stdout descriptor has closed. Do not weaken the assertion to hide an incorrect
test-container setup.

## What the focused tests cover

- A separate helper process owns the actual TCP server and accepted socket; there
  is no accepted-fd evidence on Linux before an explicit pipe-gated accept.
- Wrong live PID, changed creation identity, an exited child, and a closed second
  connection with a different client ephemeral port cannot borrow ownership.
- Caller cancellation, caller deadline, the internal observation deadline,
  unsupported addresses and malformed/truncated native TCP tables fail closed.
- Both constructors allow the owned HTTP connection and reject a stale identity;
  a raw TCP reader proves rejection delivers zero bytes and EOF, rather than
  merely observing zero complete HTTP requests.
- Every new HTTP connection is verified; an ownership failure makes exactly one
  health attempt, does not create a session, and terminates the owned process.

The test helpers have independent bounded lifetimes and explicit pipe-based
coordination; no native model credentials are required. Actual native OpenCode
transport and repository-work smoke evidence is recorded separately beside the
versioned fixtures under `daemon/internal/harness/testdata/opencode/`; these
tests do not promote any adapter capability.
