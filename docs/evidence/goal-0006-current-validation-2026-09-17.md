# Goal 0006 Current Validation Evidence - 2026-09-17

## Classification and verdict

This receipt records the latest bounded local validation of the Goal 0006
working tree after commits `6e43fba` and `8ec662d`. It is **current
dirty-subject evidence**, not a final acceptance receipt. The Goal remains
**active** and is not complete. The working tree is not a committed exact
Subject, and no model, local test gate, or evidence document can grant the
missing authority.

The latest evidence adds a committed OpenCode terminal candidate witness and a
committed strict Codex probe. It still does not establish the required final
Subject, hosted acceptance, credentialed provider identity/accounting, native
provider capability promotion, or authorized Goal acceptance.

## Source snapshot

The Git snapshot below was captured immediately before rewriting this receipt
and the completion matrix. Both receipt paths are untracked documentation files
and are refreshed below. The source and test paths were not changed by this
documentation task.

| Field | Value |
| --- | --- |
| Branch | `codex/goal-0006-durable-engineering-work` |
| `HEAD` | `8ec662d008f4dcad5f5f71897f5f0d9c2653d5b9` |
| `HEAD^{tree}` | `b409fd41de553f4a0ba8111e380c72cbb75a13ee` |
| Index tree | `b409fd41de553f4a0ba8111e380c72cbb75a13ee` |
| Staged paths | `0` |
| Tracked dirty paths | `132` |
| Untracked paths | `115` |
| `git status --short --untracked-files=all` lines | `247` |
| Working-tree state | dirty; index equals `HEAD` |

## Authoritative post-commit harness frozen snapshot

Before this documentation rewrite, the integration-owned harness gate captured
the committed post-commit state at `HEAD` `8ec662d` and tree
`b409fd41de553f4a0ba8111e380c72cbb75a13ee`. Its status had **242 lines at both
start and end with no drift**. The runner's frozen hashes were:

| Frozen runner item | Value |
| --- | --- |
| Unstaged diff SHA-256 | `125ef3a9ab3a1e62e2f9bf5b9a5430771d286775cfb3060e9770145f1214dfac` |
| Staged diff SHA-256 (empty) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| Untracked-content SHA-256 | `c939b560d3a9c9779c4f98e107bc790a9b6c90bd725335e95c16a49945e8fadc` |
| Status drift | `242` lines at start and end; no drift |

The frozen harness results were:

- Windows harness: 5 packages passed; the app surface had 26 top-level tests
  and 37 tests including subtests; `go vet` and the Windows build passed.
- Linux race harness: 5 packages passed; the full daemon gate reported 18
  `ok` packages and 3 `[no test files]` packages.
- Pinned Windows OpenCode `1.18.30`: all 6 candidate scenarios passed in
  approximately `86.6s` with the exact executable SHA recorded below.

This frozen 242-line receipt predates the live documentation-write snapshot.
After the two untracked evidence documents were written, the live worktree
snapshot became `247` status lines (`132` tracked dirty and `115` untracked),
with the raw local hashes recorded below. The two snapshots must not be merged:
the former is the authoritative post-commit harness gate, while the latter is
the current dirty documentation state.

The prior post-fix Linux Docker gate independently observed **132 modified paths
and 114 untracked paths** before the two new commits. Its runner fingerprints
remain useful for that earlier gate batch, but are not write-time hashes for
`HEAD` `8ec662d`:

```text
status:    B758F7B9FE6503670B841CAA0089875B7B8F5590358987A6B52554E5FD2439A6
diff:      60012921AF1E4E76EDF1ABB10FC1C1CD0A8738C90B54386D3B97F50B95A32CCD
untracked: BDF34127636B81959D17384F0924D3E17405D65C58017C1F9DFFBA7667C9A2E4
```

These are the authoritative runner fingerprints for that prior gate batch; the
runner reported no workspace modification. The commit-specific OpenCode and
Codex receipts below are the new evidence at the current `HEAD`.

For reproducibility, this receipt also records hashes recomputed locally at
rewrite time. The local byte streams are defined explicitly because the gate
runner fingerprints above use its own manifest serialization:

| Hash | Definition | Value |
| --- | --- | --- |
| Local status manifest | SHA-256 of raw UTF-8 `git status --short --untracked-files=all` bytes, including the command's LF separators | `DB586AF3D006F7E4B9BBFAE1E5CCC8CD9ABF4B8B3F6CC33EA33C3F6A6B35A384` |
| Local tracked diff | SHA-256 of raw `git diff --binary --full-index --no-ext-diff --no-textconv` bytes | `50793F90CBFD865F1015F5D43E04984E4E7D1689417F2208EB7E0FD26CD5C0DD` |
| Local untracked manifest | SHA-256 of UTF-8 `git ls-files --others --exclude-standard` path lines plus a final LF | `3D670FF5DD3B9FF675097FDC7912785047BA0203036512889BF58F6134A7DBDE` |
| Local dirty-content aggregate | SHA-256 of UTF-8 sorted `path<TAB>sha256(file-bytes)` records plus a final LF, over the 245 dirty paths after excluding these two owned receipt files | `FBF393E48D9617565DE350D87CCC2FA8291BF6F25BE562C6099E7902F74A4249` |

The two receipt files are excluded from the dirty-content aggregate. Refreshing
their bytes does not change the status-path counts or the source snapshot,
create a committed Subject, or promote any provider capability.

`HEAD` now contains the source commits `6e43fba` (OpenCode terminal candidate
witness) and `8ec662d` (strict Codex capability probing). The remaining 132
tracked and 115 untracked paths are still dirty WIP outside that committed
tree. Therefore these commits are real current provenance, but the repository
as a whole is still not a final exact Subject; all final predicates must be
rerun after the remaining WIP is finalized.

## Subject and artifact hashes

The daemon canonical tree digest was recomputed from committed Git objects for
`HEAD` using `daemon/internal/workspace/subject.go:520-555`:

```text
committed manifest entries: 719
Subject.tree_digest: sha256:5c33e1f4f1526bc5c1fcf7c13ba0d37c8f3fa126cef5b411edd3c20afba3358d
```

This is a committed-tree artifact digest only. It is not a complete Subject,
because the approved repository `resource_id` is unavailable in this receipt
and the dirty worktree is not admissible as the final revision. Consequently
no current `subject_hash` is claimed.

Other relevant artifact identities retained by the current evidence corpus are:

| Artifact | SHA-256 / identity | Boundary |
| --- | --- | --- |
| Pi executable `0.85.1` | `2d4d351da30bfe23a473032e66a571b238763565aa93754e74f4a939de13f195` | Local synthetic-provider cancellation only |
| OpenCode executable `1.18.30` | `c1bdbb18767048e1853af4238311b3f7e16ff2f91b68fb4c7ced3c5175347eea` | Exact pinned Windows candidate witness only; no production terminal promotion |
| OpenCode candidate witness test | `02478ba3752098557213079e8d6f18b99ad80fe6b73760f7375bbd9771dd48dc` | Committed six-scenario candidate evidence; not a production capability receipt |
| Codex strict-probe patch receipt | `FB0A50874C945DA6C34E0EBB5E4980730C77494881E8607401AA62C502EF41FD` | Sol `gpt-5.6-sol/xhigh` APPROVE receipt for the five-file patch; not a Subject hash |
| Pre-authority witness test | `25838b64eb118de6817acd011ca0660f97c98d8569c25fd0c664c4d6cbe5111e` | Current local Windows production-binary witness |
| Enabled witness seam | `24beffceea7f9fb7ec9fb277b860791da72277ceae607be72682191540b6a86b` | Opt-in `symmetry_pre_authority_witness` path |
| Disabled witness seam | `a49d4309165ff744fb273bf16becdb8af7c8f8a2bae584ffcd408d95ff4c37e0` | Default build remains fail-closed |
| Witness command | `1a65a8f1c69e0b41dc3ed49dc39498aabf5ae612ad7b7cae34a1d3fddf9456f7` | Explicit opt-in only |

## Commit-specific evidence at current `HEAD`

The following bounded receipts are tied to the two source commits now present
in `HEAD`. They establish candidate/probe behavior only and do not promote a
production provider capability.

| Surface | Current result | Interpretation |
| --- | --- | --- |
| OpenCode pinned Windows candidate | Exact OpenCode `1.18.30` executable SHA-256 `c1bdbb18767048e1853af4238311b3f7e16ff2f91b68fb4c7ced3c5175347eea`; six scenarios execute. `terminal_stop`, `provider_error`, `step_ended_continuation`, `step_ended_failure`, and `truncated_provider_eof` return exactly `ResultUnknown`; `interrupt` returns exactly `ResultCancelled` with `TaskResultReasonCancelled` and unknown usage. | Committed candidate evidence only; `WaitTurn`/production terminal mapping remains fail-closed and no production terminal promotion occurred. |
| OpenCode worker repetition | Pinned Windows candidate witness `-count=5`: 5/5 runs passed, six scenarios per run (about 419 seconds total). | Repeated candidate evidence at the final test-file bytes; it is not provider-owned acceptance. |
| OpenCode independent review run | Sol `gpt-5.6-sol`/`xhigh` APPROVE receipt for target `daemon/internal/harness/opencode/adapter_native_terminal_candidate_test.go`, SHA-256 `02478ba3752098557213079e8d6f18b99ad80fe6b73760f7375bbd9771dd48dc`; independent exact pinned witness passed all six scenarios in `75.826s`. | Review approval covers the bounded candidate witness and its fail-closed assertions, not production capability promotion. |
| OpenCode Linux package race | Independent Docker package-only check: `go test -race -count=1 -timeout 600s ./internal/harness/opencode` PASS (`4.005s`); normal package check PASS (`1.764s`). | The Windows-only candidate was not executed on Linux. No network-none exact Linux OpenCode `1.18.30` executable was available, so this is package evidence only. |
| Codex strict probe | Commit `8ec662d` adds strict full-input stable-version parsing, rejecting `0.153.4-alpha.1`, `0.153.4+build.1`, and `0.153.4 trailing-token`; registry and concrete probe share schema/version validation. Exact tested schema still returns `ErrNativeUnverified`. | Sol `gpt-5.6-sol`/`xhigh` APPROVE patch receipt SHA-256 `FB0A50874C945DA6C34E0EBB5E4980730C77494881E8607401AA62C502EF41FD`; no native lifecycle or capability promotion. |
| Codex probe checks | `go test -count=1 ./internal/harness ./internal/harness/codex` PASS (2 packages); `go vet ./internal/harness ./internal/harness/codex` PASS; `gofmt -d` and `git diff --check` clean. | The installed Codex is `0.155.0-alpha.2.6`; the pinned `0.153.4` native executable is unavailable, so native execution was not rerun. |

The OpenCode candidate test is now committed by `6e43fba`; the Codex strict
probe is now committed by `8ec662d`. These receipts remain bounded evidence for
those commits. They do not establish hosted CI, credentialed provider identity,
usage/accounting, or an authorized Goal acceptance.

## Prior frozen post-fix gates

These results were supplied as the final integration-owned post-fix re-gate
results before commits `6e43fba` and `8ec662d`. They remain useful bounded local
evidence for their earlier dirty WIP, but are not being relabeled as current
exact-Subject acceptance.

| Surface | Post-fix result | Interpretation |
| --- | --- | --- |
| Windows Go 1.27 | Full `go test ./...` PASS; `go vet ./...` PASS; `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...` PASS; `gofmt` PASS for 231 files | Current Windows daemon source/build gate passed after the post-fix changes |
| Windows focused regressions | State, provider, case-alias, app-bridge, and harness-Pi focused suites passed with `-count=20` | The new ownership, journal, serialization, output, and alias boundaries are repeatedly exercised |
| Linux Docker Go 1.27 | Full `go test ./...` PASS; `go test -race ./internal/state ./internal/app ./internal/harness/pi` PASS (`state 70.349s`, `app 559.331s`, `harness/pi 13.922s`) | Post-fix Linux state, app, and Pi-harness race coverage passed |
| Linux workspace integrity | 132 modified and 114 untracked paths before the two new commits; status/diff/untracked fingerprints remained unchanged; no workspace modification | Gate did not mutate that earlier dirty WIP |
| Windows Claude probe | Prior frozen batch: 50 repetitions PASS | Transport/probe behavior remains bounded; native Claude lifecycle remains unverified |

## Prior frozen batches retained

The following results remain useful, but were not rerun in the post-fix batch and
are therefore labeled prior frozen evidence rather than current post-fix gates:

| Surface | Prior frozen result | Boundary |
| --- | --- | --- |
| Control | `809 passed, 20 skipped`; migration, controller, admission, provider, and receipt gates PASS | Dirty-WIP Control evidence; rerun against the final Subject is still required |
| Contracts | Node `26/26`; `14` schemas; `184` fixtures; Effect validation `184`; parsers, YAML, heredocs, PowerShell, and Compose checks PASS | Dirty-WIP wire/configuration evidence; not exact-Subject acceptance |
| Windows Pi | `--failed`: `3 passed`; canonical: `5 passed`; `25 excluded` | Local noncredentialed Pi evidence; it does not promote Pi |
| Linux cross-builds | `CGO_ENABLED=0` builds for `linux/amd64`, `linux/arm64`, and `linux/riscv64` PASS | Prior frozen cross-build evidence; not native provider evidence |

The post-fix gates add the following implementation and review evidence:

- The authoritative Windows owner-loss message/classification regression is
  covered and passes, preserving the distinction between process ownership
  loss and a generic failure.
- The provider journal now has a no-HTML serializer, legacy normalized exact
  replay, centralized unresolved-liability handling, and stable unknown-effect
  readback semantics.
- Completion persistence enforces a new 64 KiB persisted representation
  boundary.
- Output dropping is atomic across the publication/termination boundary.
- Recursive exact JSON tag alias rejection prevents ambiguous wire fields.
- The independent Luna review findings were resolved and rechecked.
- The Astra medium-effort security-critical final review reports no current
  production P1 or P2 findings, with 61 focused tests covering the reviewed
  boundaries.

The final review retains three nonblocking residuals: reserve a future
non-ASCII `FailureCode` policy before expanding that enum, revisit future
`reflect.Interface` fields if they become part of the persistence surface, and
keep the opaque embedded JSON/token threat boundary explicit. None is a current
P1/P2 finding, and none supplies Goal acceptance authority.

The post-fix and prior frozen results do not include a final committed Subject, an approved
repository identity, a hosted CI run for that commit, or an authorized
operator acceptance receipt.

## Evidence disposition

### Proved on the current dirty WIP

- Goal/Orchestration ownership, revision, dependency, blocker, and settlement
  semantics remain covered by the prior frozen Control batch.
- The prior frozen Windows Go 1.27 full tests, vet, `windows/amd64`
  CGO-disabled build, 231-file formatting, and repeated focused
  state/provider/case-alias/app-bridge/Pi harness tests passed before the two
  new commits.
- The prior frozen Linux Docker Go 1.27 full tests and state/app/Pi-harness race
  suites passed before the two new commits; the current OpenCode package-only
  Linux race check is recorded above.
- Daemon replay, fence, cancellation, output publication, process recovery,
  and authority-boundary tests remain covered by the post-fix Windows/Linux
  gates.
- Wire schemas, generated Contracts, strict parsers, Effect decoding, and
  Compose configuration remain supported by the prior frozen Contracts batch.
- Windows pre-authority and supervisor recovery witnesses pass locally.
- Pi cancellation and bounded native behavior pass with synthetic loopback
  inputs and explicit unknown usage semantics; the prior Pi batch is not a
  provider-identity receipt.
- The new provider journal, persisted completion, atomic output-drop, owner-loss
  classification, and recursive JSON-alias boundaries are covered by the
  focused post-fix tests.
- The committed OpenCode candidate witness is pinned to the exact Windows
  `1.18.30` executable SHA and covers six lifecycle/error/interruption
  scenarios. Non-interrupt scenarios remain exactly `ResultUnknown`; the
  interrupt scenario is exactly `ResultCancelled` with unknown usage. This is
  candidate evidence only and does not promote OpenCode terminal capability.
- The committed Codex strict probe rejects prerelease, build-suffixed, and
  trailing-token version output; registry and concrete adapter schema evidence
  remain in parity, and exact schema matches still return
  `ErrNativeUnverified`.

### Incomplete or externally blocked

- The dirty WIP is not a final committed exact Subject and has no approved
  `resource_id`, complete Subject tuple, or current `subject_hash`.
- Hosted Linux/Windows CI and exact-Subject reruns are absent.
- Provider identity, credentials, usage, billing/accounting, and readback are
  not established by loopback or dummy-key evidence.
- Claude remains probe/transport-only; its native repository/session lifecycle
  is unverified and its adapter remains fail-closed.
- OpenCode has bounded candidate lifecycle evidence, but no provider-owned
  terminal success/error receipt or production terminal promotion. The exact
  Linux network-none `1.18.30` executable is unavailable.
- Codex strict version/help/schema probing is committed and reviewed, but the
  probe still returns `ErrNativeUnverified`; the pinned `0.153.4` native
  executable is unavailable and native repository/lifecycle capability remains
  unverified.
- No exact-current-Subject validator/reviewer receipts or authorized operator
  `achieve` receipt exist.
- The Astra review's nonblocking residuals do not remove the external Subject,
  provider, hosted, or operator blockers.

## Superseded and historical evidence

The following evidence remains useful for the revisions it names, but must not
be treated as current acceptance proof:

- `docs/evidence/goal-0006-completion-matrix-2026-09-16.md` is superseded by
  the 2026-09-17 matrix for the current post-fix counts.
- `docs/evidence/goal-0006-candidate-scope-audit-2026-09-16.md` is a prior
  path/count snapshot and is superseded by the live snapshot above.
- The 2026-09-16 claim-slice, native-terminal, and CI parser reviews are
  historical/superseded reviews; their current-gate surfaces are represented
  by the post-fix Linux and Windows results and the explicitly labeled prior
  Control and Contracts batches above.
- Older Codex, Pi, OpenCode, Claude, handoff, continuation, and dirty-manifest
  receipts remain bound to ancestor commits or older dirty snapshots. They are
  not relabeled as evidence for `8ec662d...` or for this post-documentation
  state; the commit-specific candidate/probe receipts are listed above.
- `docs/evidence/goal-0006-opencode-terminal-blocker-2026-09-16.md` remains
  useful as the pre-candidate blocker analysis. The committed candidate receipt
  narrows that gap but is not a passing production OpenCode receipt.

## Remaining blockers

The following blockers are still required before Goal 0006 can be considered
complete:

1. **Committed exact Subject:** produce the final source commit, approved
   repository `resource_id`, daemon `tree_digest`, canonical `subject_hash`,
   and exact-subject evidence. A later evidence-only commit creates a new
   Subject and requires rerunning affected predicates.
2. **Authorization and hosted CI:** obtain the authorized repository/Goal
   binding and run fresh hosted Linux and Windows checks against the exact
   committed Subject.
3. **Credentialed provider identity and accounting:** establish approved
   Codex, Claude Code, pi, and OpenCode identities, credentials, usage,
   billing/accounting, and unknown-effect readback evidence within permitted
   spend.
4. **Codex native lifecycle:** prove native repository/session/lifecycle work
   for pinned `0.153.4`; the strict probe remains `ErrNativeUnverified` and the
   exact native executable is unavailable.
5. **Claude native lifecycle:** prove native repository/session/cancel/resume
   behavior, not just the current 50x probe/transport result.
6. **OpenCode terminal proof:** provide a provider-owned terminal success/error
   receipt for the pinned `1.18.30` path, including interruption and usage
   semantics; the current six-scenario witness remains candidate evidence and
   OpenCode remains unpromoted. The exact Linux network-none binary is also
   unavailable.
7. **Exact-subject validator/reviewer receipts:** bind every required predicate
   and independent review to the final Subject, not to a dirty tree or an
   ancestor. The Luna and Astra reviews are current WIP review evidence, not
   exact-Subject acceptance receipts.
8. **Authorized operator acceptance:** execute the configured operator review,
   current-revision integration outcome, and authorized `achieve` receipt.

## Final status

Goal 0006 remains **active**. This receipt records current dirty-WIP progress
and explicit limits; it does not change the Goal file or Goal status. No
production source, historical evidence, Goal file, commit, branch, or remote
was changed by this documentation refresh.
