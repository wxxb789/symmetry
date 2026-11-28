# Goal rf-acc-pi-subject-hash-carrier — the `pi` provider row needs the canonical `subject_hash` carried to the model

Status: phase-0 contract, authored 2026-11-28. Dispatch base: `origin/main`
`ea54bc91b151799076eb037ab4fa0a316c5e5f7e` (measured after `git fetch --prune origin`). Branch
`refactor/rf-acc-pi-subject-hash-carrier`. Standing brief rules: `refactor/brief-rules.md` in the
GDD state root. The owner rulings of 2026-11-28 are fixed inputs, not open questions: carrier
`1C = BOTH` (C1 fixture-side, C2 product-side), the Tencent LiteLLM DeepSeek V4.1 Flash Fast
credential (the owner's single test key, no key rotation), and judging ONLY the four stipulated
rows on ONE subject.

## Problem (measured)

D3's submission is blocked on one row: the credential-authenticated `pi` provider-action row.

The rejection mechanism, traced end to end:

- The turn prompt the model receives is `buildTurnPrompt`
  (`daemon/internal/harness/pi/adapter.go:712-724`, called from `AdmitNativeTurn` at `:679`): the
  goal text, the `<canonical_context_json>` block, and the embedded TaskResult schema. It supplies
  no `subject_hash` value and states no hash rule.
- For the pi case the context block is `nativeContext()`
  (`daemon/internal/app/linux_owner_witness_linux.go:3948-3954`), which carries exactly `version`,
  `subject` and `run_key`; the subject carries `resource_id`, `commit` and `tree_digest`
  (`daemon/internal/protocol/goal_subject.go:20-24`) and nothing else.
- The model's final assistant message is decoded by `DecodeAssistantTaskResult`
  (`daemon/internal/harness/pi/rpc.go:901`), which requires exactly one `text` block (`:916`) and
  delegates to `DecodeTaskResultJSON` (`:890`) and `protocol.ParseTaskResult`.
- `protocol.ParseTaskResult` rejects a `subject_hash` that is not the canonical hash of the
  embedded subject: `validateSHA256` (`daemon/internal/protocol/goal_results.go:322`) then
  `result.Subject.Hash()` compared against the declared value (`:325-330`, message
  `subject_hash does not match canonical subject`). `Subject.Hash()` is
  `"sha256:" + hex(sha256(canonical JSON))` over the whole sorted-key subject object
  (`daemon/internal/protocol/goal_subject.go:69-77`, `:81-87`), not the `tree_digest`.
- Measured on the LOCAL `off1` arm (`docs/evidence/rf-acc-pi-turn-deadline-2026-11-28.md:124`):
  the one-block gate passes and the whole rejection is
  `pi task result must be one complete JSON object: convert task-result generated DTO: subject_hash does not match canonical subject`
  — the model wrote the subject's `tree_digest` in place of the canonical hash. The `sup1`
  counterfactual (`:125`, `:128`, §3.2) shows the same payload decoding to `candidate_completion`
  once the exact subject and the canonical `subject_hash` are supplied. Cited by
  `refactor/findings/310-review-final.md:18` (GDD state root) as the next verbatim stop.

Recorded residual and owner decision: `docs/TODO.md:2484-2500` (carriers are the fixture's staged
task text or a product-side carrier in the witness context; owner `DECISION_NEEDED`) and the
decision note `docs/TODO.md:2501-2507`. The owner has since ruled `1C = BOTH`, which makes both
carriers in scope and resolves that `DECISION_NEEDED`.

Binding amendment text: the frozen rev10 draft `refactor/d3-amendment-draft.md` in the GDD state
root (frozen copy `refactor/frozen/d3-amendment-draft.rev10.md`, `cmp` rc 0, both sha256
`72e707f1d8e8637886104219f22b63850ce2cbf72cdef5c244b9777dd4489820`): §2 forbids judging the 15-set
picture, §3 fixes the four stipulated rows, §1 P6 fixes the gate, §8 the review rules.

## 1. Observable pass condition

The conductor's ONE credentialed `linux-native-v2-acceptance` dispatch (A3: dispatch on `main`,
owner-approved Environment, no branch-policy change) on ONE subject revision, with ONLY the four
stipulated rows judged (amendment §3, "Judge only the stipulated rows"; the 15-set census is not an
all-green requirement):

| Row | What must be green |
| --- | --- |
| dispatch + probe | The dispatch's own `GITHUB_SHA` and the `linux-v2-host-prerequisite-<run>-1.json` member from the same job and subject recording `status: passed`. |
| codex | `Case set codex_containment exited 0`, with its `manifest.json` binding `codex-cli 0.168.0` plus executable path, SHA-256 and version line, `containment.native_open_reached`, and one replay witness. Turn-free. |
| claude | The same row form for the exact claude binary (`2.1.323 (Claude Code)`); containment-only — the Environment holds no Anthropic credential, so it proves launch, gate release, stop, replay and recovery only. |
| pi | `TestProductionLinuxV2PiNativeProviderAction` passes in the CREDENTIALED arm: the witness summary's `provider_action.outcome == "succeeded"` and `provider_action.completed_count == 1` (fields `daemon/internal/app/linux_owner_witness_linux.go:244-248`, `:309`; the product gate that must not fire is `isolated Pi owner witness did not complete exactly one real brokered resource.sync`, `:827-829`), the test-level `provider_action_succeeded: true` (`daemon/e2e/production_linux_v2_native_witness_test.go:1380`), and the exported member `shared/pi-native-provider-action.json` (exporter `scripts/ci/export-linux-v2-redacted-evidence.py:17`, `shared` member list `:293-294`) binding the exact Pi binary path, SHA-256 and version line (`1.5.2`, pin `scripts/ci/linux-v2-native-tools.sh:41-47`) and the exact subject revision. The arm must be credentialed: the fixture's `placeholder_provider_credential` selector is false (`scripts/ci/linux-v2-native-acceptance.sh:3302-3306`), so no `credential_gated_pi_provider_action` outcome appears for `shared`. |

The pi row must also be reported with the run's pi turn count, which the amendment requires for
every credentialed run (§5 risk 3). Its value is unknown until that dispatch exists; the conductor
measures and records it. The numbers this contract does not state — the run id, the subject
revision, the pi turn count, the exact exported member digests, and which (if any) of the four rows
is not green — are all measured by the conductor's dispatch and named in the acceptance record;
none is guessed here.

Every other case set and leg (the remaining sets, the F2-excluded `shared` OpenCode legs, the
durability and replay sets, `alternate_mount_refusal`) is read per published witness file and
reported with its observed disposition. None of them voids or substitutes for the four rows.

## 2. Judging artifact

The dispatch's acceptance record plus the run's artifact digests. A reader must be able to see:

- the run id, the run's `GITHUB_SHA`, and the artifact id of the exported evidence;
- the four rows above, each with its verbatim log line as quoted in §1 and the sha256 of the
  exported member it binds;
- the probe member's name and `status: passed` with its `source_commit`;
- for the pi row: `shared/pi-native-provider-action.json`'s sha256, its `provider_action` object,
  its `native_binary`/`native_binary_sha256`/`native_version_line`, and its `subject`;
- the bounded, redacted provider error text if the turn fails (amendment §3 pi row), and
- an explicit statement of which of the four rows is NOT green if any is not, with its verbatim
  stop. A credential-gated outcome is not a pass (amendment §1 P6) and must not be reported as
  green.

Records and receipts name the exact revision and digests they gated; a receipt whose gate input
changed afterwards is PARTIAL/SUPERSEDED with the superseding revision named.

## 3. Failure control — what a FALSE green looks like

Two named ways this goal could look done while the row is still unproven:

1. Credential-gated green. The run is the placeholder arm, or the fixture (or the reader) counts
   the named `credential_gated_pi_provider_action` outcome as a pass, so the row is called green
   with no provider turn performed. Excluded by reading the pi row's own published member — not any
   aggregate exit code and not another Bind — and requiring `provider_action.outcome == "succeeded"`,
   `completed_count == 1`, `provider_action_succeeded: true`, and a credentialed arm
   (`placeholder_provider_credential` false, `scripts/ci/linux-v2-native-acceptance.sh:3302-3306`).
   Under 1B1 the gated outcome must stay explicit in the log, the record and the export and must
   never be counted as a pass.
2. Staged text that does not carry the sent subject's hash. C1 could look done — its staging pin
   green, a hash-shaped line staged — while the staged hash is not the canonical hash of the
   subject the witness actually sends, so the row still fails; or a pin could be satisfied by any
   64-hex string. Excluded by a committed row that recomputes the canonical hash from the request's
   own subject triple and compares it to the staged text, with an independent inline literal rather
   than the production constant, and decisively by the credentialed run's
   `provider_action.outcome == "succeeded"`. The C1 worker names the mutant that reddens that
   recomputation row on the delivered bytes.

Judging from a run's aggregate exit code, or combining witnesses across runs or Binds, is
forbidden (amendment §2, §3). A non-zero job exit caused only by F2-excluded legs is expected and is
not a red gate (§3 F2 row).

## 4. Out of scope

- The 15-set workflow's greenness, and any subset or superset of the four stipulated rows
  (amendment §3).
- The codex and claude rows beyond the stipulated content: their own walls are separate goals.
- Key rotation of the exposed provider key (the owner's call; `refactor/findings/310-review-final.md`
  residual).
- The daemon-side residuals carried in `docs/TODO.md`: F1, the unprojected provider `stopReason`
  (`docs/TODO.md:2508-2512`), the credentialed `stale_leaf` and provider-free hold exposure
  (`:2467-2479`), the two frozen arm-parser guard residuals (`:2513-2532`), the
  `native_release_claude` matrix leg, the owner-`Before` ordering residual and the `state_dir` note
  (amendment §6e).
- Relaxing the decoder, the one-block gate or the canonical-subject-hash equality
  (`daemon/internal/protocol/goal_results.go:325-330`) to make the row pass; and any change to the
  Environment's branch policy or credentials (owner ruling 3A3).
- New native version pins — the amendment adds none (amendment §5 risk 5).

## 5. Evidence plan

Owners and userlands, per slice; the conductor is the single integration owner and the only party
that dispatches a credentialed run (A3).

- C1 (fixture side). Worker runs, in the WSL Ubuntu-24.04 userland that hosts `bash` and `python3`:
  `bash -n scripts/ci/linux-v2-native-acceptance.sh` (exit 0) and
  `python3 -B scripts/ci/test-linux-v2-evidence.py` — the staging pin
  (`scripts/ci/test-linux-v2-evidence.py:2925-2945`) and its red-capable mutation rows
  (`:3280-3514`) must stay green, and the NEW recomputation row must be shown RED on the
  pre-change bytes and GREEN on the change. A slice that changes no Go source runs affected-package
  targeted tests only; the head-wide full suites are attributed to CI and to the conductor's record
  (lesson 161).
- C2 (product side). Worker runs `go vet ./...` and `go test ./internal/app/... ./internal/harness/pi/...`
  from `daemon/` on Windows and in WSL. The new row must be red-capable on the pre-change bytes.
- Gate-INPUT rule (lesson 199). The staged task text is an INPUT of the acceptance gate:
  `scripts/ci/linux-v2-native-acceptance.sh` is pinned by `scripts/ci/test-linux-v2-evidence.py`
  (`:2805-2810`, `:2925-2945`), and `nativeContext()` is the second input of the same gate.
  Therefore any Bind or receipt taken BEFORE a landing that changes either is INVALID: the conductor
  re-runs the container `shared` Bind on the merged bytes of both slices before the credentialed
  dispatch.
- Binding of the delivered bytes. Each worker's receipt names the exact revision and sha256 of the
  files it gated; a receipt taken on bytes a later commit changed is PARTIAL/SUPERSEDED with the
  superseding revision named. The final integration Bind and the credentialed dispatch bind the
  merged head. Every mutated assertion is exercised by the conductor's Bind case set or attributed
  to its CI home by job and file (never claimed unqualified).
- Comments and records: zero doc comments on new functions; a comment only beside a hazard line, at
  most two lines; repeated tables list only the rows that differ from an earlier table; no host path
  containing a user profile name in any repository file.

## 6. Mergeable alone

Smallest self-contained increment: C1 alone. It changes the staging text and its own pins, touches
no Go source, and leaves every other case set's behaviour unchanged (the non-`shared` sets stage no
task text and the containment sets stage no Pi home —
`scripts/ci/test-linux-v2-evidence.py:2925-2945`); the repository stays runnable and no unmerged
sibling is required. C2 alone is also independently mergeable: adding a field to the witness context
JSON compiles and passes its own package tests without C1.

Must NOT be bundled with either slice: the other providers' row walls; F1's `stopReason`
projection; the `stale_leaf`/provider-free hold work; the 15-set workflow change; any `docs/TODO.md`
closure that belongs to the amendment PR; and any decoder or hash-equality relaxation.

## The two carriers, their slices and the interface

Both slices are separately acceptable; the conductor cuts them into two workstreams with disjoint
file ownership and integrates them under one Bind.

C1 — fixture side (first). Owned files: `scripts/ci/linux-v2-native-acceptance.sh` (the
`authenticated_pi_task_text` block `:44-49`, the staging write `:1286-1289`, the fixed-B-home
install row `:703-706`) and `scripts/ci/test-linux-v2-evidence.py` (the pin fixtures and predicates
`:2793-2810`, `:2925-2945`, and the mutation rows `:3280-3514`). Change: compute the canonical
`subject_hash` from the fixture's already-resolved subject triple (`source_subject_json`
`scripts/ci/linux-v2-native-acceptance.sh:27-29`, `pi_repository_id` `:1105`, with the same commit
and tree digest handed to the control provisioning at `:1096-1102`) and stage it in the
authenticated Pi task text so the model can copy it verbatim. The request's own subject is written
from exactly those values (`scripts/ci/write-linux-v2-owner-request.py:281-289`), so the staged
value and the sent subject agree by construction. The staging pin currently requires the staged
text to equal the Control claim's `pi_provider_action` description plus the fixed-scope paragraph
(`scripts/ci/test-linux-v2-evidence.py:2930-2932`); the C1 worker must keep that three-way agreement
and extend the pin with the recomputation row of §3, without widening the guard.

C2 — product side (second). Owned files: `daemon/internal/app/linux_owner_witness_linux.go`
(`nativeContext()` `:3948-3954`; both call sites, `:3764` and `:3865`, already read it, so no call
site changes) and a red-capable test in the same package or the witness e2e test. Change: add
`subject_hash`, and no other field, to the `nativeContext()` payload, computed with
`witness.request.Subject.Hash()` (`daemon/internal/protocol/goal_subject.go:81-87`). The subject is
validated before the witness runs (`daemon/e2e/production_linux_v2_native_witness_test.go:1682-1683`),
so `Hash()` cannot fail on that path; the change must not extend the existing discarded error
(`nativeContext()` currently drops its marshal error) into a hash-less context — on any error the
witness fails closed rather than emitting a context without the hash.

Interface between the slices. The shared value is the canonical subject hash for the exact subject
the witness sends:

    subject_hash = "sha256:" + hex(sha256(CanonicalizeJSON({"commit":C,"resource_id":R,"tree_digest":T})))

with object keys sorted (`daemon/internal/protocol/goal_subject.go:69-77`) and `(C,R,T)` the
request's subject. C1 computes it in the fixture from `source_commit`, `pi_repository_id` and
`source_tree_digest`; C2 computes it in Go via `Subject.Hash()`. Neither slice reads the other's
output; both must be byte-identical for the same subject. The field name the model must echo is
`subject_hash` (`daemon/internal/protocol/goal_results.go:290`, `:322-330`), and the model reads the
value from C1's staging file `$B_HOME/.pi/agent/APPEND_SYSTEM.md`
(`scripts/ci/linux-v2-native-acceptance.sh:1286-1289`) and/or from the `<canonical_context_json>`
block of the turn prompt (`daemon/internal/harness/pi/adapter.go:712-724`). With both carriers
present and equal, either suffices for the model; C1 alone proves the plumbing inside the fixture,
C2 alone makes any real dispatch carry the value, and the combined bytes are what the conductor's
credentialed dispatch judges.

## Invariants

- Credential locality: the credential stays B-only and is never echoed; the named-credential-gated
  outcome stays explicit and is never counted as a pass.
- With a real credential present the placeholder-credential detection and staging path must remain
  byte-identical in behaviour.
- The decoder, the one-block gate, `ErrUnexpectedAssistantStop`
  (`daemon/internal/harness/pi/rpc.go:53`) and the canonical-hash equality are never weakened.
- F2 exclusion stands: the `shared` OpenCode legs are not gate evidence.
- Receipts name their revision; every new branch/arm gets a committed red-capable test; a "this is
  caught" claim names the mutant that was actually run.
