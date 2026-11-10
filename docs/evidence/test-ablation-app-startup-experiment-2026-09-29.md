# App startup test-ablation experiment

Date: 2026-09-29  
Candidate: Go core inventory C1  
Decision: **remove the duplicated declaration after merging its unique assertion**

## Scope and preservation

- Baseline revision: `d14debbf6ae3ded09e5013ef90952d347c01474d` on `codex/goal-0006b-crash-safe-process-containment`.
- The working tree was already extensively dirty. This experiment changed only `daemon/internal/app/app_test.go` and this report. Existing changes in that test file were preserved.
- Production files were read-only. Every production mutation ran from a Go `-overlay` replacement under `.omo/ablation/app-startup`; the replacements and overlays were removed after the experiment.
- No Git index operation, commit, push, historical-evidence deletion, credentialed/native-host test, child task, or full repository suite was used.

## Bound source snapshots

| Subject | Before SHA-256 | After SHA-256 | Result |
|---|---|---|---|
| `daemon/internal/app/app_test.go` | `c7b34a0216dce620bf97ba76b30e5828bd925a4f7d1bd02f044a1d97349cc1c2` | `ddbd1499c20f8fa2a7cf6858022a4875d605959974d3f2a3d2baf39a02760b2f` | test-only consolidation |
| `daemon/internal/app/app.go` | `4345733d131a3a3e6aa161d931461988dab7cec8168b824aace68acd6fedd7f6` | same | production restored/unchanged |
| `daemon/internal/state/state.go` | `ed29384b00c1f8e0f6198b50779036998e20289943d4407e587d24d257a714bf` | same | production restored/unchanged |

The intermediate merged-test snapshot before correcting cancellation timing was `2380f0983df3dc58b68f1b37eed69b6c4c70e377689bba4d21ac3c7dda05f229`.

## Baseline

Both original declarations passed against the same initial source snapshot.

Native Windows, exit `0`:

```text
cd daemon && go test ./internal/app -run '^(TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration|TestTerminalFlushFailureDoesNotPreventNewSessionRegistration)$' -count=1
```

Result: `ok github.com/wxxb789/symmetry/daemon/internal/app 1.923s`.

WSL Linux (`symmetry-race`, Go at `/opt/go/bin/go`), exit `0`:

```text
wsl.exe -d symmetry-race sh -lc "cd /mnt/q/repos/symmetry/daemon && GOMAXPROCS=2 /opt/go/bin/go test -p 2 ./internal/app -run '^(TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration|TestTerminalFlushFailureDoesNotPreventNewSessionRegistration)$' -count=1"
```

Result: `ok github.com/wxxb789/symmetry/daemon/internal/app 0.506s`.

A first WSL probe accidentally used the default distribution, where `/opt/go/bin/go` was absent, and exited `127`. It was environment discovery, not baseline evidence; the required `symmetry-race` distribution then passed as shown above.

## Consolidation and survivor contract

Removed exactly one declaration:

- `TestTerminalFlushFailureDoesNotPreventNewSessionRegistration`

Retained:

- `TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration`

Replacement assertions in the retained declaration now prove:

1. registration precedes terminal flush: `registeredAfterFlush == false`;
2. exactly one registration: `registerCalls == 1`;
3. the injected terminal flush was actually attempted exactly once: `transitionCalls == 1`;
4. the terminal journal remains loadable after that flush returns `errors.New("temporary failure")`.

The third assertion and `cancelAfterFlush` fixture switch were necessary evidence corrections. The original fixture canceled in `RegisterSession`, so the background outbox could observe cancellation before attempting the purported failed flush. An initial deletion overlay therefore remained green (exit `0`). The survivor now delays cancellation until a post-registration terminal transition, proving the failed-flush retention named by the tests instead of merely assuming it.

No shared helper or unrelated test was removed. Declaration counts after the edit are one retained declaration and zero declarations with the removed name.

## Controlled production mutations

All commands below ran only the merged survivor on WSL Linux. Exit `1` is the required **expected-red** result and is not counted as green verification.

### 1. Startup ordering

Overlay production copy SHA-256: `33b4293376a52755a83892fdc5b0af18a250a994be828e9ed838a48a7c860eaf`.

Mutation: call `daemon.flushAll(ctx)` immediately before `RegisterSession`, making retained terminal work flush before registration.

```text
wsl.exe -d symmetry-race sh -lc "cd /mnt/q/repos/symmetry/daemon && GOMAXPROCS=2 /opt/go/bin/go test -p 2 -overlay=/mnt/q/repos/symmetry/.omo/ablation/app-startup/overlay-order-linux.json ./internal/app -run '^TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration$' -count=1"
```

Exit `1`, expected-red detection:

```text
app_test.go:4112: terminal journal was flushed before new session registration
```

### 2. Duplicate registration

Overlay production copy SHA-256: `104b5805bd1666d53116fe8b90a3e3fce0cc9a7075ed8c200d2312ed7b658058`.

Mutation: invoke `RegisterSession` a second time after the first successful invocation in the real registration retry closure.

```text
wsl.exe -d symmetry-race sh -lc "cd /mnt/q/repos/symmetry/daemon && GOMAXPROCS=2 /opt/go/bin/go test -p 2 -overlay=/mnt/q/repos/symmetry/.omo/ablation/app-startup/overlay-duplicate-linux.json ./internal/app -run '^TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration$' -count=1"
```

Exit `1`, expected-red detection:

```text
app_test.go:4115: RegisterSession calls = 2, want 1
```

### 3. Failed-flush journal deletion

Overlay production-copy SHA-256 values:

- app mutation: `48f7b31f888ac701cc1876d6f7c30509bc3162e3c6d91c1a27406a32e942c201`;
- state mutation: `cd4a3de57d5ff1c1011bdc9c85edb8e01768762adc4313b765e801bfc24ffa30`.

Mutation: after `flushRun` returns an error, the app invokes an overlay-only State method that removes the journal while bypassing normal retention guards. This exercises the real outbox and Store path; no expected assertion was mutated.

```text
wsl.exe -d symmetry-race sh -lc "cd /mnt/q/repos/symmetry/daemon && GOMAXPROCS=2 /opt/go/bin/go test -p 2 -overlay=/mnt/q/repos/symmetry/.omo/ablation/app-startup/overlay-delete-linux.json ./internal/app -run '^TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration$' -count=1"
```

Exit `1`, expected-red detection:

```text
app_test.go:4121: terminal journal = run journal not found, want retained
```

The initial run of this deletion mutation against the intermediate fixture exited `0`; that was rejected as non-discriminating evidence and led to the cancellation-timing correction described above. It was never classified green verification.

## Post-restoration green verification

Production hashes after overlays matched the pre-experiment hashes shown above.

Linux normal, exit `0`:

```text
wsl.exe -d symmetry-race sh -lc "cd /mnt/q/repos/symmetry/daemon && GOMAXPROCS=2 /opt/go/bin/go test -p 2 ./internal/app -run '^TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration$' -count=1"
```

Result: `ok github.com/wxxb789/symmetry/daemon/internal/app 0.460s`.

Linux race, exit `0`:

```text
wsl.exe -d symmetry-race sh -lc "cd /mnt/q/repos/symmetry/daemon && GOMAXPROCS=2 /opt/go/bin/go test -race -p 2 ./internal/app -run '^TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration$' -count=1"
```

Result: `ok github.com/wxxb789/symmetry/daemon/internal/app 1.251s`.

Native Windows normal, exit `0`:

```text
cd daemon && go test ./internal/app -run '^TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration$' -count=1
```

Result: `ok github.com/wxxb789/symmetry/daemon/internal/app 0.695s`.

One intermediate Windows invocation launched concurrently with the Linux check exceeded the outer 120-second command timeout and yielded no test exit code. The immediate isolated diagnostic command below exited `0`, and the required final isolated Windows command above also exited `0`:

```text
cd daemon && go test ./internal/app -run '^TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration$' -count=1 -timeout=20s
```

Result: `ok github.com/wxxb789/symmetry/daemon/internal/app 0.711s`, exit `0`.

Formatting and static boundary checks:

- `gofmt -d daemon/internal/app/app_test.go`: exit `0`, zero output bytes.
- `git diff --check -- daemon/internal/app/app_test.go`: exit `0`.
- Go language-server diagnostics were unavailable because `gopls` is not installed; compilation and focused normal/race executions are the static/executable verification for this bounded track.

## Decision and limits

**Remove** `TestTerminalFlushFailureDoesNotPreventNewSessionRegistration`. Its only unique meaningful assertion, exactly-one registration, now lives in the retained test. The corrected survivor detects all three authorized real-production regressions at the same startup/outbox boundary and is green on native Windows plus Linux ordinary and race execution.

Remaining limits:

- This is intentionally focused C1 evidence, not an `./...` package/repository gate; the parent owns integration and broader verification.
- No credentialed provider, installed native identity, or special host topology is involved in C1. No test was removed because such an environment was missing.
- The experiment makes no production-code or acceptance-contract change.
