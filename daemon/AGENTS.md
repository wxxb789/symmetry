# Go execution daemon

Follow the root contract and `docs/design/harness.md`.

- Keep native adapters in `internal/harness`; existing execution/platform/state
  packages retain process supervision, OS behavior, and the local journal.
- Propagate context cancellation through blocking IO. Every goroutine has an
  owner and exit path. Drain/close process pipes, wait for the child, and bound
  shutdown; never leave an unowned process or goroutine.
- Journal launch intent before launch. Persist external session identity before
  exposing successful attachment. Propagate write/fsync/rename failures.
- Keep lease renewal independent of model output, provider calls, filesystem
  cleanup, and backpressure. Preserve Linux/Windows process-tree behavior.
- Use concrete types and small consumer-side interfaces. No custom DI container,
  generic event bus, shell-string command construction, or automatic
  interpretation of unknown native events as successful completion.
- Decode external data at the boundary. Protocol identity and control fields
  fail closed; optional diagnostic fields may be ignored explicitly.
- A native session, worktree, and model are different identities. No cross-host
  session transfer or permission escalation inferred from an opaque session ID.

From daemon/: `gofmt` on changed files; `go vet ./...`; `go test ./...`;
`go test -race ./...` for concurrency/execution changes on supported runners.
Actual native Windows behavior requires Windows tests, not only cross-compilation.
