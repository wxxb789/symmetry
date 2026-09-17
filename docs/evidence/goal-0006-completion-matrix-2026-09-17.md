# Goal 0006 Completion Matrix - 2026-09-17

## Scope and verdict

This matrix updates the requirement audit for
`docs/goals/0006-durable-engineering-work.md` using the current dirty WIP, the
committed changes in `6e43fba` and `8ec662d`, and the bounded local evidence
recorded in `docs/evidence/goal-0006-current-validation-2026-09-17.md`. It does
not alter the Goal, grant authority, or convert local evidence into acceptance.

**Verdict: Goal 0006 remains active and incomplete.** The current WIP has
strong local implementation and gate evidence, but it is not a final committed
Subject and still lacks authorization, hosted exact-Subject validation,
credentialed provider identity/accounting, native capability promotion,
provider-owned OpenCode terminal proof, exact validator/reviewer receipts, and
authorized operator acceptance.

## Current Subject snapshot

The write-time snapshot was captured immediately before rewriting this matrix
and the companion validation receipt. Both receipt paths are untracked
documentation files; their bytes are refreshed without changing the source/test
scope.

| Field | Value |
| --- | --- |
| Branch | `codex/goal-0006-durable-engineering-work` |
| `HEAD` | `8ec662d008f4dcad5f5f71897f5f0d9c2653d5b9` |
| `HEAD^{tree}` | `b409fd41de553f4a0ba8111e380c72cbb75a13ee` |
| Index tree | `b409fd41de553f4a0ba8111e380c72cbb75a13ee` |
| Staged paths | `0` |
| Tracked dirty paths | `132` |
| Untracked paths | `115` |
| Status lines | `247` |
| Prior post-fix gate status fingerprint | `B758F7B9FE6503670B841CAA0089875B7B8F5590358987A6B52554E5FD2439A6` |
| Prior post-fix gate diff fingerprint | `60012921AF1E4E76EDF1ABB10FC1C1CD0A8738C90B54386D3B97F50B95A32CCD` |
| Prior post-fix gate untracked fingerprint | `BDF34127636B81959D17384F0924D3E17405D65C58017C1F9DFFBA7667C9A2E4` |
| Local status-manifest hash | `DB586AF3D006F7E4B9BBFAE1E5CCC8CD9ABF4B8B3F6CC33EA33C3F6A6B35A384` |
| Local tracked-diff hash | `50793F90CBFD865F1015F5D43E04984E4E7D1689417F2208EB7E0FD26CD5C0DD` |
| Local untracked-manifest hash | `3D670FF5DD3B9FF675097FDC7912785047BA0203036512889BF58F6134A7DBDE` |
| Local dirty-content aggregate (245 paths; owned receipts excluded) | `FBF393E48D9617565DE350D87CCC2FA8291BF6F25BE562C6099E7902F74A4249` |
| Committed canonical tree digest | `sha256:5c33e1f4f1526bc5c1fcf7c13ba0d37c8f3fa126cef5b411edd3c20afba3358d` |

The committed tree digest is not a complete Subject. No approved repository
`resource_id` or current `subject_hash` is available, and the dirty working
tree is not an acceptance revision. `HEAD` includes the two new source commits,
but the remaining 132 tracked and 115 untracked paths are still dirty WIP
outside the committed tree; final predicates must bind to a later exact Subject.

## Authoritative post-commit harness frozen snapshot

Before this documentation rewrite, the integration-owned harness gate captured
`HEAD` `8ec662d008f4dcad5f5f71897f5f0d9c2653d5b9` with tree
`b409fd41de553f4a0ba8111e380c72cbb75a13ee`. Status was **242 lines at both
start and end with no drift**. Its frozen runner hashes were:

| Frozen runner item | Value |
| --- | --- |
| Unstaged diff SHA-256 | `125ef3a9ab3a1e62e2f9bf5b9a5430771d286775cfb3060e9770145f1214dfac` |
| Staged diff SHA-256 (empty) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| Untracked-content SHA-256 | `c939b560d3a9c9779c4f98e107bc790a9b6c90bd725335e95c16a49945e8fadc` |
| Status drift | `242` lines at start and end; no drift |

Frozen harness results: Windows harness 5 packages passed, with 26 top-level
app tests and 37 app tests including subtests; `go vet` and Windows build
passed. Linux race harness passed with 5 packages, and the full daemon gate
reported 18 `ok` packages plus 3 `[no test files]` packages. The pinned Windows
OpenCode `1.18.30` candidate witness passed all 6 scenarios in approximately
`86.6s`.

This 242-line frozen receipt predates the live documentation-write snapshot.
The later live snapshot is `247` status lines (`132` tracked dirty and `115`
untracked), with the raw local hashes above. The frozen harness gate and the
later documentation state are distinct evidence snapshots.

## Status vocabulary

- **proved-current-dirty**: current source and local gates support the named
  behavior, but the evidence is bound to an uncommitted WIP.
- **historical-only**: the useful receipt is bound to an ancestor or older
  dirty snapshot and must not validate the current WIP.
- **incomplete**: a required local/native proof or capability promotion is
  still missing.
- **external-blocked**: completion requires a committed Subject, hosted
  environment, credentials/provider accounting, or authorized operator action.

## Requirement matrix

| # | Requirement | Status | Current evidence | Remaining boundary |
| --- | --- | --- | --- | --- |
| 1 | Sustain a Goal across revisions, bounded Tasks/Runs, wakeups, retries, and sessions. | proved-current-dirty | Prior frozen Control batch: `809 passed, 20 skipped`; current Goal and Orchestration tests cover amendment, admission, settlement, wakeup, and retry paths. | Final exact-Subject validation and acceptance are absent. |
| 2 | Preserve approved intent and revision-scoped authority; stale approvals/results cannot authorize a new revision. | proved-current-dirty | Current ownership checks, private claim-authority kernel, replay tests, and admission/AST guards pass in the prior frozen Control batch. | Must be rerun and receipted for the final Subject. |
| 3 | Preserve immutable ordered context with mandatory-source and budget checks. | proved-current-dirty | Current Goal context/admission tests and daemon transport/decoder tests pass; the prior frozen Contracts batch validates the wire forms. | No exact-subject context receipt or acceptance binding exists. |
| 4 | Preserve dependencies, explicit baselines, and verifiable progress; do not infer completion from board state or model narration. | proved-current-dirty | Dependency DAG, baseline, and no-verified-progress cases remain covered by the prior frozen Control batch. | Final integration evidence and authorized acceptance remain missing. |
| 5 | Keep one bounded scheduler/dispatcher and make blockers inspectable rather than spinning. | proved-current-dirty | Current scheduler/wakeup path and negative policy-edge guards pass; no second writable scheduler is evidenced. | Exact-Subject review still required. |
| 6 | Stop for consequential decisions, resume only after scoped resolution, and retain durable blocked/continuing reasons. | proved-current-dirty | Prior frozen Control decision/blocker tests, post-fix daemon liveness gates, and Windows supervisor recovery evidence pass locally. | No operator acceptance receipt exists. |
| 7 | Separate execution success, accepted WorkItem, and achieved Goal. | proved-current-dirty | Current settlement and acceptance tests preserve distinct run, work, and Goal states. | No authorized final `achieve` receipt for a committed Subject. |
| 8 | Codex `0.153.4` real repository work within a tested native capability range. | incomplete | Commit `8ec662d` adds strict full-input version parsing, rejects `0.153.4-alpha.1`, `0.153.4+build.1`, and `0.153.4 trailing-token`, and keeps registry/concrete schema validation in parity. Exact tested schema matches still return `ErrNativeUnverified`; Sol `gpt-5.6-sol`/`xhigh` approved patch receipt SHA-256 is `FB0A50874C945DA6C34E0EBB5E4980730C77494881E8607401AA62C502EF41FD`. | The pinned `0.153.4` native executable is unavailable; native repository/lifecycle capability, credentials, usage/accounting, and exact-Subject evidence remain missing. |
| 9 | Claude Code `2.1.259` native repository/session/cancel capability. | external-blocked | Windows Claude probe passed 50x; transport and UsageUnknown/unsupported paths are tested. | Native lifecycle, repository work, credentials, usage, and capability promotion remain unverified. |
| 10 | Pi `0.85.1` native RPC repository work, retained resume, and handoff. | incomplete | Windows Pi frozen gate: `--failed` 3 passed, canonical 5 passed, 25 excluded; local cancellation and Control handoff tests pass. | Current exact-Subject repository/resume/handoff and provider/accounting evidence remain pending. |
| 11 | OpenCode `1.18.30` loopback server, repository work, terminal semantics, cancellation, and supported range. | external-blocked | Commit `6e43fba` contains a pinned Windows candidate witness using executable SHA-256 `c1bdbb18767048e1853af4238311b3f7e16ff2f91b68fb4c7ced3c5175347eea`. Six scenarios execute: all non-interrupt cases return exactly `ResultUnknown`, while `interrupt` returns exactly `ResultCancelled` with `TaskResultReasonCancelled` and unknown usage. Worker `-count=5` passed 5/5 with six scenarios per run; the independent Sol `xhigh` witness also passed all six. | This remains candidate evidence only. No provider-owned terminal success/error receipt or production terminal promotion exists; the exact Linux network-none `1.18.30` binary is unavailable. |
| 12 | Linux evidence for each claimed native release capability. | external-blocked | Independent Docker package-only checks passed: `go test -race -count=1 -timeout 600s ./internal/harness/opencode` (`4.005s`) and normal package tests (`1.764s`); the prior Linux Go 1.27 state/app/Pi race gates also pass. | The Windows-only candidate was not executed on Linux. No network-none exact Linux OpenCode binary, hosted final-Subject run, or credentialed provider receipt is available. |
| 13 | Windows evidence for each claimed native release capability. | proved-current-dirty | Windows Go 1.27 full tests/vet/build, 231-file `gofmt`, focused state/provider/case-alias/app-bridge/Pi-harness `-count=20` gates, and the committed OpenCode candidate witness pass; Claude 50x remains prior frozen evidence. The final candidate target file SHA is `02478ba3752098557213079e8d6f18b99ad80fe6b73760f7375bbd9771dd48dc`. | Hosted Windows exact-Subject evidence, provider-owned terminal proof, and provider capability promotion remain missing. |
| 14 | Recover after interruption/crash without duplicate agents or orphaned authority. | proved-current-dirty | Current Windows pre-authority production witness, post-fix app/bridge/owner-loss gates, and Linux daemon gates cover recovery, replay, and cleanup. | Dirty WIP and no hosted exact-Subject receipt. |
| 15 | Resume a retained compatible native session only after exact stop/binding/owner/version/workspace checks. | historical-only | Historical Pi retained-resume receipts and current admission tests cover parts of the contract. | A current exact-Subject native resume receipt is still required. |
| 16 | Handoff preserves artifacts/versioned context by creating a new native session, never transferring a proprietary handle. | proved-current-dirty | Current Control handoff tests preserve lineage, distinct session identity, and receipt identity. | Cross-machine handoff remains unsupported without a separately verified adapter and authorized Git artifact. |
| 17 | Reject stale authority, generations, session bindings, and decisions; exact replay returns the old receipt. | proved-current-dirty | Current admission/replay/affinity/session tests and ownership guards pass. | Final Subject validator/reviewer receipts remain absent. |
| 18 | Preserve lease/fence identity across lost acknowledgements, duplicate delivery, and restart. | proved-current-dirty | Post-fix state/app bridge gates and current lost-response witness pass with durable replay semantics. | Hosted Control/daemon restart matrix on the final Subject remains pending. |
| 19 | Keep terminal observation, cancellation, and failure ordering deterministic; late cancel cannot overwrite a settled terminal. | proved-current-dirty | Latest Linux race suites, Windows focused gates, Claude terminal/failure tests, Pi cancellation, and the committed OpenCode candidate witness pass. The witness fixes non-interrupt `ResultUnknown` and interrupt `ResultCancelled` classifications without promoting terminal success. | OpenCode provider-owned terminal success/error semantics remain unverified despite cancellation ordering evidence. |
| 20 | Distinguish duplicate/replayed delivery from valid progression and bind evidence to the exact Subject. | proved-current-dirty | Protocol Subject, evidence-batch, duplicate/conflict, and generated-contract checks pass. | Current WIP lacks approved `resource_id` and `subject_hash`; exact binding is blocked. |
| 21 | Reject invalid dependencies, outdated context, mismatched validation, missing evidence, and self-validation. | proved-current-dirty | Control and Contracts gates cover validation/evidence admission and decoder rejection cases. | Exact configured validator receipts for the final Subject are missing. |
| 22 | Unknown external effects require readback or explicit unresolved state; recovery must not repeat confirmed effects. | external-blocked | Local provider-action journal and fail-closed unknown-state tests pass. | Credentialed provider readback/accounting evidence is absent. |
| 23 | Record actual usage on failure/cancellation/rejected progress; preserve unknown usage and reservation liability. | proved-current-dirty | Post-fix provider-journal liability/readback tests, Control and daemon usage tests, Claude UsageUnknown, and Pi/OpenCode cancellation semantics pass. | Provider-owned billing and usage provenance are unverified. |
| 24 | Enforce budget reservations, admission/attempt limits, and strict-vs-soft cost semantics. | proved-current-dirty | Control budget/reservation tests and daemon unknown-cost paths pass. | No provider-enforced live billing ceiling is proven. |
| 25 | Keep deterministic checks separate from agent narration; accepted outcome requires every predicate and exact artifact. | external-blocked | Local deterministic validation and acceptance tests pass. | Exact-current-Subject validator/reviewer receipts and integration outcome are absent. |
| 26 | Make blocked/continuing reasons inspectable and prevent retry loops for invalid/consequential blockers. | proved-current-dirty | Current blocker projection, deferred-receipt, quiescence, and retry-loop tests pass. | Projection does not substitute for operator acceptance. |
| 27 | Separate run success, accepted work, and achieved Goal, tracing completion to exact artifacts/checks/authorized acceptance. | external-blocked | Committed `HEAD` tree digest is known, but the complete Subject tuple and subject hash are not. | Requires final commit, approved repository identity, all predicate receipts, and integration acceptance. |
| 28 | Complete final acceptance through the configured operator/deterministic contract; audit/CI cannot self-accept. | external-blocked | Local tests preserve the operator-required boundary; no operator action was executed. | Authorized operator review and `achieve` are required. |
| 29 | Preserve goal-less workflows and provider-owned fields/authentication while adding Goal behavior. | proved-current-dirty | Goal-less parity, provider scope, migration/controller/provider gates, and compatibility tests pass. | Live provider identity and credential custody remain external. |
| 30 | Preserve protocol v1 fences/routes and additive migration behavior. | proved-current-dirty | Prior frozen Contracts batch: `26/26`, 14 schemas, 184 fixtures, Effect 184, and Control migration gates pass. | Clean-subject and hosted reruns remain required. |
| 31 | Keep PostgreSQL as the only durable control authority; use additive migrations, lock order, and backup/restore proof. | proved-current-dirty | Current migration and local backup/restore evidence plus Control migration gates pass. | Hosted/production disaster recovery and exact committed release receipt remain pending. |
| 32 | Do not introduce a second writable scheduler/control plane; keep LoopX as concepts only. | proved-current-dirty | Current scheduler path and ownership/AST guards reject unresolved policy callers. | Final independent review must bind the committed Subject. |
| 33 | Keep native session formats non-portable and expose unsupported operations visibly. | proved-current-dirty | Registry/capability validation remains fail-closed; unsupported native operations are explicit. | This deliberately leaves claimed provider ranges unpromoted until proven. |
| 34 | Exclude native macOS daemon, general workflow DSL, multi-tenant RBAC, and unapproved autonomous expansion. | proved-current-dirty | Current scope tests and design boundaries show no excluded subsystem was added. | Scope preservation is not Goal completion. |
| 35 | Run required quality/release gates on the exact revision, including Linux/Windows/native/CI and independent review. | incomplete | Current `HEAD` includes the OpenCode candidate and Codex strict-probe commits, with Sol `xhigh` APPROVE receipts (`02478ba...` target-file SHA and `FB0A5087...` patch SHA), worker/independent pinned evidence, and Linux package-race evidence. Prior frozen Control/Contracts/Pi batches remain explicitly labeled. | Exact committed Subject, hosted artifacts, credentialed native provider evidence, current full independent review receipt, and authorized release path remain missing. |
| 36 | Missing live environments or acceptance evidence remain incomplete; fake-agent/documentation success cannot finish Goal 0006. | proved-current-dirty | All current receipts preserve explicit dirty, synthetic, transport-only, unknown, or blocked boundaries. | The boundary itself is why the overall Goal remains active. |

## Post-fix changes and independent review

The post-fix implementation and focused validation add these current WIP
results:

- Windows owner-loss handling now preserves the authoritative owner-loss
  message and classification; the regression gate passes.
- Provider journal serialization rejects HTML-bearing output, retains legacy
  normalized exact replay, and centralizes unresolved liability/readback.
- Completion persistence enforces a 64 KiB persisted representation boundary.
- Output dropping is atomic across publication and termination.
- Recursive exact JSON tag alias rejection prevents ambiguous fields in nested
  wire objects.
- The independent Luna review findings were resolved and rechecked.
- The Astra medium-effort security-critical final review reports no current
  production P1 or P2 findings and records 61 focused tests.
- Commit `6e43fba` adds the OpenCode pinned Windows terminal candidate witness.
  Its exact executable SHA is `c1bdbb18767048e1853af4238311b3f7e16ff2f91b68fb4c7ced3c5175347eea`,
  and the final target test-file SHA reviewed by Sol `gpt-5.6-sol`/`xhigh` is
  `02478ba3752098557213079e8d6f18b99ad80fe6b73760f7375bbd9771dd48dc`.
  The worker's pinned `-count=5` run passed 5/5 (six scenarios each), and an
  independent pinned run passed all six scenarios. Non-interrupt scenarios are
  exactly `ResultUnknown`; `interrupt` is exactly `ResultCancelled` with
  unknown usage. This is candidate evidence only; production OpenCode terminal
  promotion remains disabled.
- Commit `8ec662d` adds strict Codex probe validation. Prerelease, build-suffixed,
  and trailing-token version output is rejected; registry and concrete probe
  schema evidence share the same tested constants. Even an exact schema match
  remains `ErrNativeUnverified`. Its Sol `gpt-5.6-sol`/`xhigh` APPROVE patch
  receipt has SHA-256
  `FB0A50874C945DA6C34E0EBB5E4980730C77494881E8607401AA62C502EF41FD`.

The Astra review leaves only nonblocking residuals: reserve a future policy for
non-ASCII `FailureCode` values before expanding that enum, revisit future
`reflect.Interface` fields if they become persistent, and preserve the opaque
embedded JSON/token threat boundary. These are not current P1/P2 findings and
do not provide acceptance authority.

## Gate batch boundaries

The OpenCode and Codex receipts above are commit-specific local evidence at the
current `HEAD`. The broader Linux/Windows post-fix gate counts, and all Control,
Contracts, and Pi counts in this matrix, are explicitly **prior frozen batches**
unless a row says otherwise; they are not silently promoted to hosted receipts.
All such evidence remains dirty-WIP evidence and must be rerun against the final
committed Subject.

## Superseded evidence and binding rules

- The 2026-09-16 completion matrix and candidate scope audit remain historical
  snapshots and are superseded for current counts by this post-fix matrix and
  the companion validation receipt.
- The 2026-09-16 claim-slice, native-terminal, and CI parser reviews are
  superseded findings where the frozen current gates cover the same surface.
- Older provider, continuation, handoff, and dirty-manifest receipts remain
  bound to their named ancestor or dirty snapshot. They cannot validate this
  WIP or a later commit. The new OpenCode candidate and Codex strict-probe
  receipts are the exceptions explicitly bound to `6e43fba`/`8ec662d`.
- The OpenCode terminal blocker remains useful as a pre-candidate analysis, but
  it is not a passing production capability receipt. The committed candidate
  witness narrows the gap without changing the no-promotion boundary.
- Adding these two documents creates a later dirty documentation state. Any
  future committed Subject must recompute its manifest, tree digest, subject
  hash, and all affected predicate receipts after the final commit.

## Remaining completion blockers

1. Produce a committed exact Subject with approved repository `resource_id`,
   daemon `tree_digest`, and canonical `subject_hash`.
2. Obtain the required authorization and fresh hosted Linux/Windows CI for
   that exact Subject.
3. Prove credentialed Codex, Claude Code, pi, and OpenCode provider identity,
   usage, accounting/billing, and unknown-effect readback.
4. Prove pinned Codex `0.153.4` native repository/lifecycle work; the strict
   probe remains `ErrNativeUnverified` and the exact executable is unavailable.
5. Close the Claude native lifecycle gap; the 50x probe is not lifecycle proof.
6. Supply provider-owned OpenCode terminal success/error/interruption proof;
   keep the adapter unpromoted until then. The six-scenario witness is candidate
   evidence only, and the exact Linux network-none binary is unavailable.
7. Bind current validator and independent reviewer receipts to every required
   predicate on the exact Subject. The Luna and Astra reviews are WIP review
   evidence, not exact-Subject acceptance receipts.
8. Execute authorized operator acceptance and `achieve` for the current
   revision and integration WorkItem.

## Final classification

The current WIP is a meaningful **proved-current-dirty** implementation with
green post-fix Linux and Windows gates plus prior frozen Control, Contracts,
and bounded Pi batches. Native capability promotion, exact Subject provenance,
hosted CI, credentialed provider accounting, OpenCode terminal proof, exact
validator/reviewer receipts, and authorized acceptance remain **incomplete** or
**external-blocked**. Goal 0006 therefore remains **active**, not achieved.

No Goal file, production source, historical evidence file, commit, branch, or
remote was changed by this documentation refresh.
