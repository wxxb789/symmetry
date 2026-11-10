# Go probe and commit-path ablation experiment

Date: 2026-09-29. Task: `st_01a0ed7d`.

## Scope and preservation

This bounded experiment covers inventory candidates C1 and C4 on the dirty
working tree based on HEAD `d14debbf6ae3ded09e5013ef90952d347c01474d`.
I read `AGENTS.md`, `daemon/AGENTS.md`, `docs/design/quality.md`, the project
ablation brief, and the named Go inventory. Existing dirty work was preserved.
No source/test file, Git index, commit, push, or historical evidence was changed.
Production variants were complete copies under `.omo/ablation/go-probe`, selected
only by Go overlays. Expected-red mutation runs are not green verification.

## Bound subjects

SHA-256 was identical before and after the experiment:

| Path | SHA-256 |
|---|---|
| `internal/harness/registry.go` | `d00b1768c89e0380a4030696598e1e08e5e1d28066df47be2ffbadad81008ae2` |
| `internal/harness/claude_probe_test.go` | `d7fc91be7964cbf4626987fde9295d3c3a866a6143f0451a660f197af40534c1` |
| `internal/control/validation.go` | `f789c5c3ee3fe8c3b63f503e18114645915620db64e9acf9039fda84279109d3` |
| `internal/control/validation_test.go` | `0e19611838e731adb0db5565e3103bafafa43b80c705771acc6f8e490a31fe0c` |
| `internal/protocol/goal_validation.go` | `a16d2ba44f415e71d2cc6e007271d5a92a5d667f6f12210893b7515659470184` |
| `internal/protocol/goal_validation_test.go` | `8640ac5adec5b4aa78fdea6e8570514cb0a806de1b90510353d9c2fbac26c068` |

Paths in this table are relative to `daemon/`. A final scoped
`git diff --exit-code --` over all six exited 0. Scratch mutation hashes were:
`registry-output-deadline.go` `b0bd7e886c772d6c4ee554d7b3139edaad14a3e3e18172618aa0a9966d9c4b1e`;
`registry-output-nil.go` `918c3632c5d6cef2a0459daa0d2b600081c1910b84dfe217f866083f8bfeb10b`;
`control-byte-length.go` `4ff7cfcff3d1e4f0ec833cbe6f8a05155b72a64b83db0f7adcb741c399db393f`.
The three overlay JSON hashes, in C1-deadline/C1-nil/C4 order, were
`1dba0ca4538bab2a218267b93e8238871bccb8d468c70e4093b81220c6672c62`,
`9d58bfc1e8b2de1a44a0a72cb8e0dc6d13cc68bb5ca8970f4d4b6dbdac2e7a9f`, and
`16b46372fbb18470bd8554902723527837fdcf2040d2898578a85cb10f2ac71b`.

## Baselines

Commands ran from `daemon/` using `go1.27.0 windows/amd64`:

- `go test ./internal/harness -run '^(TestBoundedProbeRejectsSuccessAfterDeadline|TestClaudeProbeRejectsSuccessAfterProbeDeadline)$' -count=1` - exit 0.
- `go test ./internal/harness -run '^TestBoundedProbeRejectsSuccessAfterDeadline$' -count=1` - exit 0.
- `go test ./internal/harness -run '^TestClaudeProbeRejectsSuccessAfterProbeDeadline$' -count=1` - exit 0.
- `go test ./internal/control ./internal/protocol -run '^(TestValidateGoalCommitPathUsesUTF8RuneLength|TestValidateCommitPathUsesUTF8RuneLength)$' -count=1` - exit 0.
- `go test ./internal/control -run '^TestValidateGoalCommitPathUsesUTF8RuneLength$' -count=1` - exit 0.
- `go test ./internal/protocol -run '^TestValidateCommitPathUsesUTF8RuneLength$' -count=1` - exit 0.

## C1 results: retain the direct bounded-probe test

The original helper discards output when its derived context has expired. The
caller-level survivor asserts `DeadlineExceeded` and no promoted capabilities;
the direct candidate additionally asserts nil raw output.

Variant A changed only that branch to return callback bytes plus
`DeadlineExceeded`:

- `go test -overlay=../.omo/ablation/go-probe/overlay-c1-output-deadline.json ./internal/harness -run '^TestBoundedProbeRejectsSuccessAfterDeadline$' -count=1` - exit 1, **expected red**: output was `"valid capability output"`, wanted nil.
- `go test -overlay=../.omo/ablation/go-probe/overlay-c1-output-deadline.json ./internal/harness -run '^TestClaudeProbeRejectsSuccessAfterProbeDeadline$' -count=1` - exit 0, mutation survivor green: the error and no-capability assertions still held.

Variant B changed only that branch to return callback bytes plus nil error:

- `go test -overlay=../.omo/ablation/go-probe/overlay-c1-output-nil.json ./internal/harness -run '^TestBoundedProbeRejectsSuccessAfterDeadline$' -count=1` - exit 1, **expected red**: nil error, wanted `DeadlineExceeded`.
- `go test -overlay=../.omo/ablation/go-probe/overlay-c1-output-nil.json ./internal/harness -run '^TestClaudeProbeRejectsSuccessAfterProbeDeadline$' -count=1` - exit 1, **expected red**: help validation produced `ErrNativeUnverified`, wanted `DeadlineExceeded`.

**Decision: retain `TestBoundedProbeRejectsSuccessAfterDeadline`.** Variant A
proves its raw-nil-output assertion is unique; the proposed survivor missed that
regression. Retain the caller test for production-consumer capability promotion.
C1 removals: zero.

## C4 results: retain both package tests

The overlay changed only `control.validateGoalCommitPath` from
`utf8.RuneCountInString(value)` to `len(value)`:

- `go test -overlay=../.omo/ablation/go-probe/overlay-c4-control-byte.json ./internal/control -run '^TestValidateGoalCommitPathUsesUTF8RuneLength$' -count=1` - exit 1, **expected red**: control rejected the valid 1,024-rune/2,048-byte path.
- `go test -overlay=../.omo/ablation/go-probe/overlay-c4-control-byte.json ./internal/protocol -run '^TestValidateCommitPathUsesUTF8RuneLength$' -count=1` - exit 0, mutation survivor green because it does not execute the control validator.

**Decision: retain both tests.** The control failure and protocol survival prove
independent implementations/boundaries. Shared production validation would be a
prerequisite to later consolidation. C4 removals: zero.

## Restored focused verification

Both commands omitted overlays:

- `go test ./internal/harness ./internal/control ./internal/protocol -run '^(TestBoundedProbeRejectsSuccessAfterDeadline|TestClaudeProbeRejectsSuccessAfterProbeDeadline|TestValidateGoalCommitPathUsesUTF8RuneLength|TestValidateCommitPathUsesUTF8RuneLength)$' -count=1` - exit 0.
- In WSL distro `symmetry-race`, from `/mnt/q/repos/symmetry/daemon`:
  `GOMAXPROCS=2 /opt/go/bin/go test -race -p 2 ./internal/harness ./internal/control ./internal/protocol -run '^(TestBoundedProbeRejectsSuccessAfterDeadline|TestClaudeProbeRejectsSuccessAfterProbeDeadline|TestValidateGoalCommitPathUsesUTF8RuneLength|TestValidateCommitPathUsesUTF8RuneLength)$' -count=1` - exit 0 (`go1.27.0 linux/amd64`).

## Limits

This was focused C1/C4 evidence, not a full repository suite; parent integration
owns broad verification. Overlays never mutated source paths, and matching hashes
plus scoped diff exit 0 prove restoration. No native provider, credentialed
service, database, or unavailable host is involved in these tests; no missing
prerequisite was treated as deletion evidence. Actual model:
`ghc/gpt-5.6-sol-fast`, reasoning level `medium`.
