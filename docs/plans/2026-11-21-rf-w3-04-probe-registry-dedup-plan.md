# rf-w3-04 probe/registry dedup — implementation plan

Contract: `goal-rf-w3-04-probe-registry-dedup.md` (GDD state root), narrowed by the conductor
decision of 2026-11-21: cycle-free dedup only, **no root delegation** (D4). The root Codex and
root Claude registry defaults keep their current call graphs and stay fail-closed.

Baseline for every coordinate in this plan: `36f9a887635ec89d49cce2cd13b3915a8a875a83`
(`origin/main` after #155, the worktree HEAD — the #155 merge gate is satisfied). Branch:
`refactor/rf-w3-04-probe-registry-dedup`. Area: `daemon/internal/harness/**` only.

The increment moves three shared concerns **down** into the root `harness` package: the bounded
probe, the Codex generated-schema digest and the probe command runner. `pi`, `opencode`, `codex`
and `omo` already import `harness` (`pi/probe.go:14`, `opencode/adapter.go:23`,
`codex/probe.go:18`, `omo/probe.go:13` — verified), so the import direction is cycle-free. No
probe behaviour, error string, timeout, output limit, promotion rule or exported probe interface
changes; the proof is the existing probe and conformance suites plus the new root-package tests.

## 1. Duplication measured at the baseline

Non-test probe sources only. Line references are at `36f9a887`.

| Concern | Copies before | Where | After |
| --- | --- | --- | --- |
| bounded probe (`context.WithTimeout` + pre/post context checks) | 8 | `registry.go:257` `runBoundedProbe`, `registry.go:276` `runBoundedSchemaProbe`, `registry.go:295` `runClaudeProbe`, `pi/probe.go:69` `runProbe`, `codex/probe.go:60` `runProbe`, `codex/probe.go:76` `runSchemaProbe`, `opencode/adapter.go:53` `runProbe`, `omo/probe.go:89-100` anonymous closure | 1 implementation (`harness.RunBoundedProbe`), 13 call sites (5 in the root package, 8 across the four subpackages) |
| `SchemaDigest` | 2 | `registry.go:79` `osCodexCommandRunner.SchemaDigest`, `codex/probe.go:104` `osCommandRunner.SchemaDigest` | 1 implementation (`harness.CodexSchemaDigest`), 2 one-line runner methods |
| probe command runner (`BuildEnvironment` + bounded command) | 6 | `registry.go:46` `osClaudeCommandRunner.Run`, `registry.go:67` `osCodexCommandRunner.Run`, `pi/probe.go:85`, `codex/probe.go:92`, `omo/probe.go:38`, `opencode/adapter.go:69` | 1 implementation (`harness.RunProbeCommand`), 6 one-line `Run` methods |

`grep -c "context.WithTimeout"` per probe file: `registry.go` 2 → 0, `pi/probe.go` 1 → 0,
`codex/probe.go` 2 → 0, `omo/probe.go` 1 → 0, `opencode/adapter.go` 4 → 3 (the three survivors
are session-cleanup bounds at `:647`, `:892`, `:1249` at the baseline and `:626`, `:871`, `:1228`
in the final tree, not probe bounds), `claude_probe.go` 0 → 0, `codex_probe.go` 0 → 0; new
`bounded_probe.go` 1 (the single shared bound).

`grep -c "func .* Run(ctx"`: 6 production runner methods before and after; after, each body is a
single delegation to `harness.RunProbeCommand`, so the runner *implementation* exists once.

## 2. Public / API shape

New file `daemon/internal/harness/bounded_probe.go` (package `harness`); every exported symbol is
documented.

```go
// RunBoundedProbe runs one probe command under its own timeout and returns the
// value the delegate produced.
func RunBoundedProbe[T any](ctx context.Context, timeout time.Duration, run func(context.Context) (T, error)) (T, error)

// CommandInvoker runs one bounded probe command invocation and returns its
// captured output. Production implementation: execution.RunBoundedCommand.
type CommandInvoker func(context.Context, execution.Invocation, int) ([]byte, error)

// RunProbeCommand builds the probe environment and runs one bounded command
// through invoker.
func RunProbeCommand(ctx context.Context, invocation execution.Invocation, limit int, invoker CommandInvoker) ([]byte, error)

// CodexSchemaDigest generates the Codex app-server v2 schema bundle into a
// temporary directory and returns the digest of the generated protocol file.
func CodexSchemaDigest(ctx context.Context, executable string, limit int, invoker CommandInvoker) (digest string, resultErr error)
```

Consumers after the change (package-local names all kept):

| File | Edit |
| --- | --- |
| `registry.go` | `osClaudeCommandRunner.Run` / `osCodexCommandRunner.Run` → one-line `RunProbeCommand(..., execution.RunBoundedCommand)`; `osCodexCommandRunner.SchemaDigest` → one-line `CodexSchemaDigest(..., execution.RunBoundedCommand)`; delete `runBoundedProbe`, `runBoundedSchemaProbe`, `runClaudeProbe`; the four call sites inside `probeCodexExecutableWithRunner` use `RunBoundedProbe` directly. `ClaudeCommandRunner`, `CodexCommandRunner`, the two runner types and every `probe*` entry keep their names, signatures and call graph. |
| `claude_probe.go` | the two `runClaudeProbe(ctx, …)` call sites become `RunBoundedProbe(ctx, claudeProbeTimeout, …)`; no other edit. |
| `codex_probe.go` | no edit. |
| `pi/probe.go` | delete `runProbe`; two call sites → `harness.RunBoundedProbe(ctx, probeTimeout, …)`; `osCommandRunner.Run` → one-line `RunProbeCommand(..., execution.RunBoundedCommand)` with `probeOutputLimit`. |
| `codex/probe.go` | delete `runProbe`, `runSchemaProbe`; three call sites → `harness.RunBoundedProbe(ctx, probeTimeout, …)`; `osCommandRunner.Run` and `osCommandRunner.SchemaDigest` delegate; keep the `boundedCommand` field and its `(ctx, invocation, limit)` type; the injected seam now reaches the shared helpers through `runner.invokeBoundedCommand`. |
| `omo/probe.go` | the anonymous `run` closure keeps the 64 KiB guard and calls `harness.RunBoundedProbe(ctx, probeTimeout, …)`; `osCommandRunner.Run` delegates. |
| `opencode/adapter.go` | runner lines only: delete `runProbe` (`:53-67`), two call sites → `harness.RunBoundedProbe(ctx, probeTimeout, …)`, `osCommandRunner.Run` delegates. |

No existing exported symbol is renamed, removed or re-typed; `NewClaudeAdapterWithRunner`,
`NewCodexAdapterWithRunner`, `NewRegistry`, the `Registry` surface and `AvailabilityError` are
untouched. `harness` gains no `harness/*` subpackage import (clause 3): the new file imports only
`context`, `crypto/sha256`, `encoding/hex`, `errors`, `fmt`, `os`, `path/filepath`, `strings`,
`time` and `internal/execution`.

## 3. Exact edge-case semantics

**`RunBoundedProbe`.** The body is the root `runBoundedProbe` body generalised over `T`:
nil `ctx` → zero value and `errors.New("harness probe context must not be nil")` with the
delegate never called; `ctx.Err()` non-nil → zero value and that error; otherwise the delegate
receives `context.WithTimeout(ctx, timeout)` and its own `cancel` runs on return. After the
delegate returns, `ctx.Err()` then `probeContext.Err()` are checked and win over the delegate's
result, so a delegate that answers only after the caller cancelled or the bound fired produces a
context error and a discarded value — never evidence. A delegate error otherwise passes through
unwrapped (identity preserved for `errors.Is` at the call sites that classify it), and a
successful value passes through unchanged.

*Nil ctx is unreachable through every public probe entry* (baseline `36f9a887` coordinates,
final-tree coordinates in parentheses): `UnavailableAdapter.Probe` / `UnavailableAdapter.Start`
(`registry.go:325`, `:339`; `:219`, `:233`), `probeCodexExecutableWithRunner` (`registry.go:379`;
`:273`), `probeClaudeNativeEvidence` (`claude_probe.go:71`; unchanged), `pi.Probe`
(`pi/probe.go:272`; `:251`), `codex.Probe` (`codex/probe.go:197`; `:109`), `opencode`'s adapter
probe entry and `omo.probeForPlatform` (`omo/probe.go:71`; `:67`) all guard a nil context before
the delegate runs. The shared guard only makes a direct nil call fail closed instead of
panicking.

**`RunProbeCommand`.** Builds the process environment once (`execution.BuildEnvironment()`), sets
it on a copy of the caller's `execution.Invocation` and calls `invoker(ctx, invocation, limit)`.
The invoker owns the output bound, so each package's limit and `execution.ErrOutputLimitExceeded`
behaviour are unchanged: `probeOutputLimit` (pi), `probeOutputLimitBytes` (codex, opencode),
`64*1024` (omo), `claudeProbeMaxOutputBytes` / `codexProbeMaxOutputBytes` (root). A package that
injects its own runner seam passes that seam instead of `execution.RunBoundedCommand`, so no
probe test launches a native executable.

**`CodexSchemaDigest`.** Exactly the body of the two former copies, with the command run through
`RunProbeCommand`: nil ctx → `codex schema probe context must not be nil`; `os.MkdirTemp` failure
→ the pre-existing context check then `create Codex schema temp directory: %w`; the temp directory
is always removed, and a cleanup failure joins `remove Codex schema temp directory: %w` onto the
result; a command failure → the pre-existing context check then
`generate Codex app-server schema: %w: %s` with the trimmed output; a missing or unreadable bundle
→ `read generated Codex v2 schema: %w`; success → `"sha256:" + hex(SHA-256(bundle))` of
`codex_app_server_protocol.v2.schemas.json`. One documented micro-difference: when
`execution.BuildEnvironment` fails *and* the caller's context is already done, the shared order
reports the context error where the former copy reported the environment error. Both are
pre-command failures that cannot promote evidence, and the probe entries already prefer the
context error in the same situation; no test reaches either arm.

**Error strings kept byte-identical** (clause 3): `harness probe context must not be nil`,
`codex schema probe context must not be nil`, the three Codex schema directory/command/read
strings, every `harness.ErrHarnessUnavailable` / `ErrNativeUnverified` / `ErrUnsupportedVersion`
wrap in the four probe entries, and `OmO probe output exceeds limit`. Both subpackage
`osCommandRunner.Run` bodies keep returning exactly the `execution` runner's output and error.

**OmO output guard (P1-2).** The 64 KiB guard stays in `omo/probe.go` at its call site, inside the
delegate handed to `RunBoundedProbe`; `errors.New("OmO probe output exceeds limit")` and the `>`
boundary are unchanged, and no other probe gains or loses a limit. Its precedence is preserved:
the guard is evaluated before the shared post-checks, and the shared post-checks only replace the
guard error when the caller's context or the probe bound already ended, which is when the callers'
own post-checks already replaced it.

## 4. Design trade-offs

- **Function seam instead of a runner struct.** A `harness.ProbeRunner` struct holding
  `{Invoker, Limit}` would let each package keep one field, but the `codex` runner is constructed as
  a zero-value struct literal by its tests (`codex/probe_test.go:167` `osCommandRunner{boundedCommand: …}`),
  so a struct field holding the seam would have to be defaulted at every construction or lose the
  field type the tests pin. The function seam keeps every package-local type, field and method
  exactly as it is and makes the shared implementation one call.
- **The environment build moves inside the shared helper.** All six copies built the environment
  verbatim; keeping it in `RunProbeCommand` removes the sixth duplicate and keeps the
  `execution.BuildEnvironment` failure string (`%w` of the environment error) unchanged at every
  call site. §3 records the one unreachable ordering corner this creates.
- **`CommandInvoker` as an exported function type.** Two shared functions need the same seam; the
  named type documents it once and keeps both signatures readable. It is assignable from
  `execution.RunBoundedCommand` and from a package method value with no conversion.
- **Timeout constants stay package-local.** `probeTimeout` (pi `5s`, codex `1s`, opencode `1s`,
  omo `5s`) and `claudeProbeTimeout` (`5s`) carry per-provider evidence comments; the shared
  helper takes the bound as a parameter instead of owning a policy table.
- **Rejected: a shared output-limit parameter.** Folding OmO's guard into `RunBoundedProbe` would
  give every other probe a second limit check and move a provider-specific error string into the
  shared code (P1-2). Rejected: root delegation to `codex.Probe` — `codex/probe.go:18` imports
  `harness`, so that is an import cycle, and it would flip the root projection from
  `ErrNativeUnverified` to the host-hashing promotion path (D4).

## 5. Baseline and expected delta (report-only, D2)

Measured on the final tree with `git diff --numstat` (`36f9a887` → worktree) plus `wc -l` on the
new files: non-test sources 40 inserted / 283 deleted = **−243**, of which `registry.go` −106,
`codex/probe.go` −88, `pi/probe.go` −21, `opencode/adapter.go` −21, `omo/probe.go` −7, and
`claude_probe.go` two call-site lines. The new shared `bounded_probe.go` is 119 lines including
doc comments, so the **net non-test delta is −124**. Test files: `bounded_probe_test.go` +268 (new),
`omo/probe_test.go` +51 (new guard rows), `claude_probe_test.go` 20/3 (one test re-pointed, one
nil-ctx entry row), `codex_probe_test.go` 17/0 (one nil-ctx entry row) — 356 inserted test lines
and 3 rewritten. Overall `git diff --stat` (`36f9a887` → worktree over the 11 harness files, the
new ones included): 515 insertions / 286 deletions; the new files are `bounded_probe.go` 119 lines
and `bounded_probe_test.go` 268 lines.
The contract's −350…−400 band is report-only
(D2, P1-3) and the duplication table of §1 is the judged artifact: 8 bounded-probe
implementations → 1, 2 `SchemaDigest` bodies → 1, 6 runner bodies → 1.

## 6. Evidence plan (all foreground, on the final tree)

| Gate | Command |
| --- | --- |
| format | `gofmt -l` over the changed files (expect empty) |
| vet | `cd daemon && go vet ./...` (host windows/amd64) |
| builds | `GOOS=windows go build ./...`, `GOOS=linux go build ./...` |
| cross vet | `GOOS=linux GOARCH=amd64 go vet ./internal/harness/...` |
| windows suite | `cd daemon && go test -count=1 ./internal/harness/...` |
| linux suite | `wsl -d Ubuntu-24.04 -u accept` → `cd /mnt/q/repos/wt-w3-04-probe-registry-dedup/symmetry/daemon && go test -count=1 ./internal/harness/...` (the contract names the canonical checkout path; the tree under test must be this worktree). **Recorded result on the frozen tree, run alone**: `ok` for root `harness`, `claude`, `codex`, `omo`, `probeconformance`; `FAIL` for `omp`, `opencode` and `pi`. Every failing test is loopback-dependent (`omp` `TestNativeAcceptanceCancelToolOracle` readiness handshake, `opencode/client_test.go` and `pi/provider_bridge_test.go` HTTP), and this distro cannot complete a loopback TCP connect at all: a five-line `python3` bind/listen/connect on 127.0.0.1 times out here. Pre-existence proven in the untouched `/mnt/q/repos/symmetry` checkout (`9747127f`, no slice changes): `TestClientHealthUsesExactEndpointAndBasicAuth` and `TestNativeAcceptanceCancelToolOracle` fail there identically. All clause-1 named probe suites are green on both hosts. Sidenote: the nested `wsl.exe -- bash -lc` path mangles `$?`/`PIPESTATUS` (host bash reports them correctly), so the per-package `ok`/`FAIL` lines and `&&`/`||` probes are the recorded evidence, not an echoed status. |
| whitespace | `git diff --check` |
| host path | `git diff origin/main...HEAD` filtered case-insensitively for a profile-directory separator (expect none) |
| mutation (a) | flip the bound in `RunBoundedProbe` (drop `WithTimeout` / widen it) → the new timeout row must fail red |
| mutation (b) | drop one context check in `RunBoundedProbe` → the new cancellation-after-success row (and, for the pre-check, the expired-context row) must fail red |
| mutation (c) | `grep -c "context.WithTimeout"` and `grep -c "func .* Run(ctx"` per probe file before/after — reported, never a gate |
| mutation (d) | make `CodexSchemaDigest` bypass the injected invoker → `TestOSCommandRunnerSchemaDigestUsesBoundedCommand` (`codex/probe_test.go:163`) and the new shared-seam row must fail red |
| mutation (e) | remove the OmO 64 KiB guard → the new `omo/probe_test.go` row must fail red |

`probeconformance` and `pi/`, `codex/`, `opencode/`, `omo/` probe suites run inside
`./internal/harness/...`. No measurement or benchmark harness runs concurrently with the suite.
Native acceptance suites that `t.Skip` without credentials are reported as not run with their skip
conditions named.

## 7. Which moved strings are already asserted

| String from the shared code | Existing assertion | Compensating action |
| --- | --- | --- |
| `harness probe context must not be nil` | none directly | New root row asserts the exact text and that the delegate is not called. `claude_probe_test.go:153` is re-pointed to `RunBoundedProbe` and keeps asserting nil output + `context.DeadlineExceeded`. |
| Codex schema strings (`create`/`remove`/`generate`/`read`) | none directly | The new root row pins the invoker routing, the invocation shape, the digest format and the nil-ctx text; `codex/probe_test.go:163` keeps pinning the codex runner's seam. |
| `generate Codex app-server schema: %w: %s` | `codex/probe_test.go:107` (bounded command errors) | unchanged text; the codex suite stays the witness. |
| `harness.ErrHarnessUnavailable` / `ErrNativeUnverified` / `ErrUnsupportedVersion` wraps | `codex_probe_test.go` (7 behaviours), `claude_probe_test.go` (13), `pi/probe_test.go`, `opencode/adapter_test.go`, `omo/probe_test.go`, `probeconformance` | untouched by the diff; both suites run on Windows and under WSL. |
| `OmO probe output exceeds limit` | none | New row in `omo/probe_test.go` (below), plus the at-limit boundary row so the `>` comparison is pinned from both sides. |

## 8. Out of scope (with reasons)

- Root Codex/Claude delegation or deletion (D4): the root stays fail-closed, `codex.Probe` is not
  reachable from `harness`, `NewClaudeAdapterWithRunner` stays as the test seam.
- `opencode` promotion or deletion (D1/W6-04): its `Probe` promotion rules are untouched; only the
  three runner lines delegate.
- `harness/codex/adapter_test.go` and `harness/opencode/adapter_native_work_test.go` (W3-15).
- `internal/harness/**` files not listed (adapters' session lifecycle — W2-13, merged).
- The `Registry` lookup/registration surface and `AvailabilityError` semantics.
- The JSONL decoder, `WaitTurn` barriers and the other W2-13 deferrals (recorded in
  `docs/TODO.md` there; other slices).

## 9. Ambiguities resolved

1. **Where the OmO guard row lives.** The Change clause requires "one row pinning the OmO 64 KiB
   guard (`omo/probe.go:96-98`, kept at its call site)". The guard is an unexported closure inside
   `package omo`; the root `harness` package cannot reach it and must not import `harness/omo`
   (clause 3). The strict owned set names the subpackage *probe* files but not their tests, so the
   row is added as a **new, additive test function in `omo/probe_test.go`** (no existing assertion
   is edited; `bytes` is its only new import). It is the minimal, collision-free home for a
   required clause; the owner declares it here and in the report. Rejected alternatives: a
   tautological root row that re-implements the guard, or dropping the clause.
2. **The WSL gate path.** The contract names `/mnt/q/repos/symmetry/daemon` (the canonical
   checkout). Running there would exercise `origin/main`, not this worktree, so the gate runs in
   `/mnt/q/repos/wt-w3-04-probe-registry-dedup/symmetry/daemon`; the exact path and tool versions
   are recorded with the output.
3. **`claude_probe_test.go:153` calls the deleted `runBoundedProbe`.** The file is in the owned
   set; the test is re-pointed to `RunBoundedProbe` with both assertions unchanged (same error,
   same nil output), which keeps it a pin on the shared bound instead of on a wrapper.
4. **Closing the increment.** Contracts, the run ledger and `answer.md` live in the GDD state tree
   and are operator-owned; this repository closes an increment with the plan file, the source and
   tests. `docs/TODO.md` holds an open entry (line 63) that explicitly assigns the shadowed root
   probe paths to this slice *as a deletion candidate for a later pass*; this slice keeps those
   paths (D4) and deletes nothing, so that entry stays open unchanged and no entry closes here.

## 10. Files this slice writes

Source: `daemon/internal/harness/bounded_probe.go` (new), `registry.go`, `claude_probe.go`,
`pi/probe.go`, `codex/probe.go`, `omo/probe.go`, `opencode/adapter.go` (runner lines only).
Tests: `daemon/internal/harness/bounded_probe_test.go` (new), `claude_probe_test.go` (one test
re-pointed, one nil-ctx entry row), `codex_probe_test.go` (one nil-ctx entry row),
`omo/probe_test.go` (one new guard test, §9.1).
Docs: this plan. No `docs/TODO.md` entry opens or closes (§9.4). Nothing under `daemon/internal/app`
is required: `NewClaudeAdapterWithRunner`, `NewCodexAdapterWithRunner` and `NewRegistry` keep their
signatures, so `app/harness_test.go:306` compiles unchanged.
