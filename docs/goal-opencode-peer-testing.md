# OpenCode local TCP ownership verification

This check protects the private OpenCode HTTP transport. It does not establish
native lifecycle, terminal-result, permission, usage, or resume support. Those
capabilities remain explicitly unverified under [Goal 0006](goals/0006-durable-engineering-work.md).

## Contract

Before the first HTTP or Basic Auth byte on each new connection, both production
adapter constructors verify the exact connected IPv4 loopback peer against the
persisted server PID and process creation identity. A listener on the same port
is insufficient. IPv6, unsupported operating systems, missing evidence, and
identity mismatches fail closed.

Linux matches the reversed established TCP four-tuple in `/proc/self/net/tcp`
to a nonzero socket inode held in `/proc/<pid>/fd`. It checks process identity
and the shared network namespace before and after the inode/FD observations,
and rechecks the tuple/inode. Windows holds a process handle, checks its creation
identity and liveness, and matches the established four-tuple and owning PID
from `GetExtendedTcpTable`; it rechecks process liveness before returning.

The verifier sets a one-second observation deadline (or the shorter caller
deadline), with 10 ms intervals on the same connection. This accommodates Linux's
connect/accept gap without sending a health request or redialing first. Native table and FD
scans have bounded allocation and scan sizes. Kernel calls themselves are
synchronous; context cancellation is checked around and within scanning work.
Final ownership failures preserve their cause and are not health-retried.

These are bounded kernel snapshot checks, not a defense against a privileged
attacker or an authorized child deliberately transferring its socket. The exact
PID is required; ownership by an arbitrary descendant is not accepted.

## Executable Evidence

Run from `daemon/` on both Windows and Linux:

```text
go test -count=3 ./internal/platform ./internal/harness/opencode
go vet ./...
go test ./...
```

On a runner with race instrumentation, also run:

```text
go test -race ./...
```

When using Docker for these Linux gates, pass `docker run --init` so orphaned
test descendants are reaped. Without an init process, a terminated descendant
can remain a zombie and fail the existing process-absence assertion even after
its stdout descriptor has closed. Do not weaken the assertion to hide an
incorrect test-container setup.

The focused tests cover:

- A separate helper process owns the actual TCP server and accepted socket.
- Linux has no accepted-FD evidence before an explicit pipe-gated accept.
- Wrong live PID, changed creation identity, exited child, and a closed second
  connection with a different client ephemeral port cannot borrow ownership.
- Caller cancellation, caller deadline, Linux's internal observation deadline,
  unsupported addresses, and malformed/truncated native TCP tables fail closed.
- Both production OpenCode factories allow the owned HTTP connection and reject
  a stale identity. A raw TCP reader proves rejection delivers zero bytes and
  EOF, rather than merely observing zero complete HTTP requests.
- Every new HTTP connection is verified; an ownership failure makes exactly one
  health attempt, does not create a session, and terminates the owned process.

The test helpers have independent bounded lifetimes and explicit pipe-based
coordination. No native model credentials are required. Actual native OpenCode
transport and repository-work smoke evidence must be recorded separately; these
tests do not promote any adapter capability.
