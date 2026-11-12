# pi 1.5.2 native retained-resume receipts

Date: 2026-11-11. Scope: one settled native Pi turn, bounded process stop, then
a new native process resuming the exact retained session and workspace. The
provider is a deterministic loopback OpenAI Responses gateway; no real model,
credential, Control/PostgreSQL durability or capability promotion is proved.

## Exact assets

- Windows `pi.exe` SHA-256:
  `40a062bedd75767e2c3c889265de7c83d64a0ba55c74b60243c2453d97cffd85`
- Linux `pi` SHA-256:
  `6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57`
- Both report `1.5.2`.
- Loopback gateway source SHA-256:
  `a268b5b22b917556ca943c9fc0151e46b070360e146d293c6bff8679c9f2e832`
  (local ignored `.symmetry/native-linux-tools/loopback-responses-gateway.go`).
- Source revision: `a04c4ab02d1a6a35aa682c5b845133c720d3be61` plus the
  working-tree pin/evidence changes.

## Windows

```text
SYMMETRY_PI_NATIVE_REPOSITORY_TASK=1
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_EXECUTABLE=<absolute>/pi.exe
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_PROVIDER=symmetry-native-loopback
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_MODEL=gpt-5.6-terra
SYMMETRY_PI_NATIVE_REPOSITORY_TASK_BASE_URL=http://127.0.0.1:4141/v1

go test ./internal/harness/pi \
  -run '^TestNativeRepositoryTaskRetainedResume$' -count=1 -v -timeout 5m

--- PASS: TestNativeRepositoryTaskRetainedResume (4.50s)
```

## Linux (Ubuntu 24.04 WSL2, x86_64)

```text
--- PASS: TestNativeRepositoryTaskRetainedResume (3.65s)
```

The Linux run used the same gateway source and the downloaded Linux 1.5.2
asset. `TestNativeRepositoryTask` also passed on Windows with the same loopback
gateway (3.14s).

## What the test proves

- The first native turn created the fixed artifact, returned the strict
  TaskResult, and settled before the process was stopped.
- The retained session file remained inside the exact `--session-dir`.
- A second adapter/process opened the exact retained session ID/file and
  recalled the opaque token supplied only in the first prompt.
- Both processes completed a bounded termination barrier. In 1.5.2 the
  `OutputTruncated` marker was false because output ended cleanly; the test
  requires `Terminated`, no sink/output/termination/containment error, not the
  marker.
- The loopback gateway recognized only the fixed task prompts and returned
  deterministic tool-call/final-answer events; it never contacted an upstream
  provider.

## Not proved

- Real provider authentication, served model/effort, billing or usage.
- Daemon/Control admission, PostgreSQL durability, crash recovery, exactly-once
  external effects, or a retry after a lost stop receipt.
- Linux v2 B-clone reuse; this test runs the adapter directly and reuses the
  fixture workspace, not a daemon-owned Linux clone.
