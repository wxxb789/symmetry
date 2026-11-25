# rf-acc-witness-unresolved-exit — evidence record

Goal: `rf-acc-witness-unresolved-exit` (contract `goal-rf-acc-witness-unresolved-exit.md`, GDD state
root). Branch `refactor/rf-acc-witness-unresolved-exit`, base revision `3b23dead`. Rulings Q1(a) (the
e2e exit assertion is the contract; the three unresolved branches return a deliberate error after
writing the summary), Q2(a) (ratified scratch pattern, dummy pins, at most 3 runs) and ruling E
(2026-11-25, F-ackloss-second-wall: the e2e owner-identity completeness pass is amended — §10). Plan:
`docs/plans/2026-11-25-rf-acc-witness-unresolved-exit-plan.md`.

Line numbers below are the dispatch head `3b23dead`; the contract cites origin/main `33ad88a9`, and
§1 re-locates every cited site by content.

## 1. Clause census at the dispatch head (the contract's site table, re-located)

`grep -n 'Outcome = "unresolved"\|Outcome: "unresolved"\|Outcome == "unresolved"\|containment_unproven\|writeLinuxV2NativeWitnessSummary' daemon/internal/app/linux_owner_witness_linux.go`:

| Contract | Head | Site | Disposition |
| --- | --- | --- | --- |
| `:480` | `:480` | `witness.summary.Outcome = "unresolved"` (legacy-unproven branch) | writer at `:501` converted |
| `:501` | `:501` | `writeLinuxV2NativeWitnessSummaryAndExit` | converted |
| `:538` | `:538` | `witness.summary.Outcome = "unresolved"` (unresolved-authority recovery) | writer at `:556` converted |
| `:556` | `:556` | `writeLinuxV2NativeWitnessSummaryAndExit` | converted |
| `:571` | `:571` | `case "unresolved"` of the `RecoveryState` switch | writer at `:596` converted |
| `:596` | `:596` | `writeLinuxV2NativeWitnessSummaryAndExit` (shared by every switch outcome) | converted; the helper returns nil for proven outcomes |
| `:420` | `:420` | fail-closed struct literal `Outcome: "unresolved"` | no writer of its own; its bytes are written only through the three sites above |
| `:1456` | `:1456` | `Outcome = "unresolved"` in `summarizeResponseLost` | exempt: its only writer (`writeResponseLost`) joins `execution.ErrLinuxV2MutationAckLost` at `:624` before returning |
| `:676` | `:676` | the fresh positive path's writer | untouched, and unreachable for `unresolved`: the path assigns `completed`/`recovered`/`peer_denied` before it |
| `containment_unproven` | `:483`, `:541`, `:573` | the three `FailureCategory` assignments | all three feed the three converted writers; the only other matches are the validator's exact-shape checks (`:738-739`) |

`writeLinuxV2NativeWitnessSummary` remains the literal call at `:618` (response-loss writer) and
`:676` (positive writer). No other function in the file writes `containment_unproven`.

## 2. Probe (lesson 74): characterization baseline before the fix

Throwaway probe `daemon/internal/app/zz_probe_witness_exit_test.go` (deleted; never committed), run in
WSL Ubuntu-24.04 with `GOTOOLCHAIN=go1.27.0`, `go test -count=1 -run TestProbeWitnessExitBaseline -v
./internal/app/` on the base bytes (`ok github.com/wxxb789/symmetry/daemon/internal/app 0.025s`):

| Outcome | Bytes | sha256 of the encoded response | Writer error on base |
| --- | --- | --- | --- |
| `unresolved` | 2053 | `7ad51a380dc5e36afeb0a40047824e053f720320184d82e853b45bf2f9dc632f` | `<nil>` — the defect clause |
| `no_native_effects` | 2060 | `a300ab9d117f3803c18c91bbaad4c42d42f2c79e6a9ce0625c5b347688f32525` | `<nil>` |
| `aborted_no_target` | 2060 | `994af43dafdabd28ca93ae8d5bfdc50e0616da803174232f10f0c2fe4f2f1ae9` | `<nil>` |
| `recovered` | 2052 | `2e1e58e7fffcf9d96f756653ed909a76b7bfe9ae6da2ece3fc8cd1b491bf0309` | `<nil>` |
| `completed` | 2052 | `afc75ddbd803f7e3b9baf22e7b19038f1f273d0274be180e7c6155178d53e38f` | `<nil>` |
| `peer_denied` | 2054 | `91c0b908ae1e9627a51bc6ce81aeff2110a259a093fbe2a1f574adf92f7287bb` | `<nil>` |

The permanent test pins the `unresolved` response above byte for byte (encoded literals captured from
this probe), so the same digest is re-derived by `go test` on the fixed bytes (§3). The end-to-end
failure this clause inverts is already recorded: runs `45481221551`/`45484692731` wrote exactly this
response shape and exited 0, and the e2e stopped at
`unresolved production recovery exit = <nil>, want deliberate nonzero process exit`
(`production_linux_v2_owner_cli_test.go:184` at this head, `:177-193` as cited).

## 3. Local gates (WSL Ubuntu-24.04, `GOTOOLCHAIN=go1.27.0`, working directory `daemon/`)

| Gate | Command | Exit | Result |
| --- | --- | --- | --- |
| gofmt | `gofmt -l .` | 0 | no file listed |
| focused (lesson-74 probe + regression) | `go test -count=1 -run TestLinuxV2NativeWitnessUnresolvedOutcomeFailsTheCommand ./internal/app/` | 0 | ok, 0.024s on the final bytes (the same command was re-run independently by the integrity delta re-check: `ok … 0.167s`, §8b) |
| witness suite | `go test -count=1 -run TestLinuxV2Native ./internal/app/` | 0 | ok, 1.143s on the final bytes |
| vet | `GOOS=linux go vet ./...` | 0 | clean |
| race (package parity) | `go test -race -count=1 -timeout 600s ./internal/app/` | 1 | `FAIL github.com/wxxb789/symmetry/daemon/internal/app 600.104s`, timeout panic naming `running tests: TestRawOutputBudgetDropsWithoutFailingSemanticEvents (1m27s)` in a narrower re-run (`-timeout 100s -v`, same command shape). Not attributable to this diff: that test is pre-existing (`daemon/internal/app/app_test.go:4641` at base `3b23dead`), the diff adds one non-blocking test to a different file, and the `daemon` CI job prepares ordinary private user/PID namespaces first — `scripts/ci/prepare-private-namespace-tests.sh` refuses a root caller (`Namespace tests must run as an ordinary unprivileged user.`) and any non-GitHub-hosted environment, so this WSL root shell cannot reproduce that job's race environment |
| build | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` | 0 | clean |
| acceptance integrity | `git diff --stat -- daemon/e2e/` | 0 | empty |
| product hunk shape | `git diff -- daemon/internal/app/linux_owner_witness_linux.go` | 0 | 3 call sites + sentinel/helper only; no struct/JSON/assertion line |

The `daemon` CI job on the PR head runs the full `-race ./...` pass; this stage ran the package-scoped
race pass and the exact focused/suite commands the contract lists.

## 4. Guest proof: scratch loop (`scratch/rf-acc-witness-unresolved-exit`, cap 3 runs)

Throwaway workflow `.github/workflows/scratch-rf-acc-witness-unresolved-exit.yml` (push-only, copy of
`linux-native-v2-acceptance.yml` without the Environment and with the ratified dummy pins; verbatim in
§7) plus the scratch-only `gh` shim `scripts/ci/scratch-gh-shim` (byte-identical to the ratified
burndown copy, answers only `gh auth login`). Run order: mutants first, final bytes last (lesson 86).

Every scratch job ran with the §7 dummy pins: no `environment:`, no `secrets.*`, no `vars.*`, and no
billed provider turn (all four legs hold before `native_release`). Run order: the two product-mutant
classes first, the final-bytes run last (lesson 86), so the last code-affecting action on the scratch
branch is the delivered product file.

Runs used: 3 of the 3-run cap (classes + 2, lesson 69) — M1, M2, final bytes, in that order, each
settled before the next push; no run was voided and no class needed a repeat. The branch was deleted
after run 3 (`git push origin --delete scratch/rf-acc-witness-unresolved-exit`) and
`git ls-remote origin scratch/rf-acc-witness-unresolved-exit` prints nothing.

| Run | Purpose | Scratch head | Workflow sha256 | Product sha256 | Case sets | Acceptance job | Result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | mutant M1 | `a47a05fc` | `eccc66db…` | `57adce7e…` | `stage_replay_create_started` | `138938170006` (run `45534474018`) | red at the verbatim exit line, §5 |
| 2 | mutant M2 | `ec11c304` | `3ef92a26…` | `c6b37cc6…` | `stage_replay_leaf_bound`, `stage_replay_launcher_bound`, `ack_loss_launcher_bound` | `138941583215` (run `45535320177`), 11m33s | two sets green (they do not use the mutated sites), `ack_loss_launcher_bound` red at §4c's second wall — §5 M2 row |
| 3 | final bytes | `a5dfb1b1` | `f3cb9e92…` | `7484da8b…` | all four sets above | `138945998150` (run `45536878631`), 9m10s | three sets green, `ack_loss_launcher_bound` red at the second wall — §4a/§4c |

Run 3 is the last code-affecting action on the scratch branch: every later change is this record and
`docs/TODO.md`, and the goal worktree's product/test bytes equal the scratch head's byte for byte
(§6). Its artifact `linux-v2-native-evidence-45536878631-1` binds all four exports to
`source_commit=a5dfb1b1…` and carries, per set, `manifest.json` plus the allowlisted witness document.

### 4a. Per-set rows (acceptance)

The four sets' acceptance row is the run-3 row below; the "before" column is the parent goal's
observed failure on the same fixture vehicle (the contract's F1 finding), where the response was
written and only the exit status failed.

| Case set | Expected recovery | Witness document (allowlist) | Before (parent heads) | After (run 3) |
| --- | --- | --- | --- | --- |
| `stage_replay_create_started` | `unresolved` | `stage-crash-replay-create_started.json` | run `45481221551` (`aa58f43b`): hold, marker and boundary pass; the recovery leg writes the exact `unresolved`/`containment_unproven` response and exits 0, first failing check `requireLinuxV2RecoveryExitStatus` | **exit 0**, manifest `status=passed`, witness `recovery_state=unresolved`, `failure_category=containment_unproven`, `journal_unproven=true`; the exit check now sees a deliberate non-zero exit |
| `stage_replay_leaf_bound` | `unresolved` | `stage-crash-replay-leaf_bound.json` | run `45484692731` (`0003f62d`): red with the verbatim line `unresolved production recovery exit = <nil>, want deliberate nonzero process exit` | **exit 0**, same witness facts |
| `stage_replay_launcher_bound` | `unresolved` | `stage-crash-replay-launcher_bound.json` | same run, stage runner (`production_linux_v2_native_witness_test.go:178` frame at this head) | **exit 0**, same witness facts |
| `ack_loss_launcher_bound` | `unresolved` | `ack-loss-replay-launcher_bound.json` | same run, ack-loss runner, same verbatim line | **exit 1, but not at the exit check**: the leg now passes `requireLinuxV2RecoveryExitStatus` (`:3496`) and stops at the next assertion, `linuxV2RequireSameOwnerIdentities` (`:3501`) — `production_linux_v2_native_witness_test.go:217: launcher_bound ACK-loss replay duplicated or replaced an owner process: replayed owner set contains an incomplete process identity`; its manifest is `status=failed`, the witness document is the leg's failure record. §4c gives the root cause; ruling E fixed it — run `45540942939` is green for this set (§10). |

Run 3's per-set lines, verbatim (`Case set <name> exited <n>`): `stage_replay_create_started exited 0`,
`stage_replay_leaf_bound exited 0`, `stage_replay_launcher_bound exited 0`,
`ack_loss_launcher_bound exited 1`, then `Failed case sets: ack_loss_launcher_bound`.

### 4c. The ACK-loss leg's second wall (analysis of what the exit fix exposes)

The M2 run reached a check no earlier run could reach, and the same check sits on the final bytes'
path for that leg (`:501`/`:596` are not on it — see the M2 row's two green sets):

- Position. `linuxV2RequireSameOwnerIdentities` is called only by the ACK-loss replay
  (`production_linux_v2_owner_cli_test.go:3501`) and the helper-response-loss replay (`:3737`), and
  both calls sit *after* `requireLinuxV2RecoveryExitStatus` (`:3496`). Every earlier run — the parent
  goal's runs and this goal's M1 — stopped at the exit check, so this comparison had never executed.
- What the message pins. It is the function's second loop
  ("replayed owner set contains an incomplete process identity"): a **non-nil** owner in the replay
  response has `PID <= 1` or an empty identity; the first loop, which fails on a changed PID/identity
  against a non-nil marker owner, passed.
- Why that is structural, not a fixture accident. The witness summary's owner block has no
  `omitempty`: the encoder always emits all of `anchor`/`helper`/`launcher`/`gate`/`reaper`, and the
  pinned response literal in the new unit test shows exactly that
  (`"owners":{"anchor":{},"helper":{},"launcher":{},"gate":{},"reaper":{}}`). The e2e parses those
  objects into pointers, so at a boundary where the gate is not yet bound
  (`launcher_bound`) the parsed `owners.gate` is a non-nil zero process in **both** the stage marker
  and the replay response. The marker comparison therefore matches (0 == 0, "" == "") while the
  completeness loop rejects the response entry. The check can never pass for an ACK-loss replay held at
  `launcher_bound`; only this leg reaches it (`:3501`, plus the helper-response-loss replay at `:3737`),
  and only after the exit-status check passes.
- Consequence for this goal's acceptance. Run 3 confirms it: the ACK-loss leg's exit is now deliberate
  and it stops here, so `ack_loss_launcher_bound` cannot complete its expected recovery with the ruled
  change alone. Acceptance 1's "each of the four guest case sets exits 0" is met for three sets and
  unmet for this one, and the unmet part is outside this goal's owned files.
- Smallest amendments (for the conductor's ruling, any of which needs its own increment and a binding
  run): **(E)** make the completeness loop skip an entry whose marker counterpart is the same unbound
  placeholder — smallest, keeps the "no duplicated/replaced owner" invariant for bound owners, but it
  is a `daemon/e2e/**` edit, read-only under Q1(a); **(P)** stop emitting unbound owner slots from the
  witness summary (`omitempty`/pointer fields, so the response omits `gate` at a pre-gate boundary) —
  a struct/JSON-tag change beyond ruled Change 1-4 that also moves the response-byte pin; **(D)** move
  this case set's ACK-loss boundary to a post-gate stage in the fixture (`scripts/ci/**`, the parent
  goal's vehicle) — changes what the set proves. Recommendation: **(E)** as the intent-preserving
  minimum, with **(P)** recorded as the product-truth alternative if the owner prefers that unbound
  owners never appear in a witness. It was recorded here, not fixed; ruling E (2026-11-25) then
  amended the completeness pass as (E) describes — §10 carries the change, its mutants and the green
  four-set run.

## 5. Mutants

| id | Mutation | Job that must redden | Observed |
| --- | --- | --- | --- |
| M1 | `:556` back to the bare return (§7) | scratch `linux-native-v2-acceptance`, set `stage_replay_create_started` | **red, right reason.** Run `45534474018` (head `a47a05fc`), acceptance job `138938170006`: `Required native Go witness did not produce exact run/pass/skip=0 events (go test exit=1)`; `witness output: production_linux_v2_native_witness_test.go:178: unresolved production recovery exit = <nil>, want deliberate nonzero process exit`; `--- FAIL: TestProductionLinuxV2StageCrashReplayMatrix/create_started (2.40s)`; the script printed `Case set stage_replay_create_started exited 1` and `Failed case sets: stage_replay_create_started`, and still exported 1 allowlisted witness document (`allowlisted redacted evidence: 1 files; missing=0`) plus its manifest. This also locates the set's recovery leg at the `:556` site. |
| M2 | `:501` and `:596` back to the bare return, `:556` restored (§7) | scratch `linux-native-v2-acceptance`, sets `stage_replay_leaf_bound`, `stage_replay_launcher_bound`, `ack_loss_launcher_bound` | **partly attributable, and it maps the sites.** Run `45535320177` (head `ec11c304`), acceptance job `138941583215`, 11m33s: `Case set stage_replay_leaf_bound exited 0`, `Case set stage_replay_launcher_bound exited 0` (both exported a passing manifest and witness document), `Case set ack_loss_launcher_bound exited 1`, `Failed case sets: ack_loss_launcher_bound`. The two green sets are the finding: with `:501`/`:596` bare and `:556` fixed they still exit non-zero, so their unresolved legs are carried by the `:556` site (the same site M1 reverted) — the contract's row predicted their reds at `:596`, and the observed mapping is the reverse. The third set's red is **not** the exit line: `witness output: production_linux_v2_native_witness_test.go:217: launcher_bound ACK-loss replay duplicated or replaced an owner process: replayed owner set contains an incomplete process identity` — the exit-status check at `:3496` passed (so that leg's exit is carried by `:556` too) and the leg stopped at the *next* check, `linuxV2RequireSameOwnerIdentities` (`production_linux_v2_owner_cli_test.go:3501`). Because `:3501` sits after the exit check, no earlier run could reach it, and the M2 bytes are identical to the final bytes on that leg's path; §4c analyses the consequence. |
| M3 | the helper ignores `Outcome` and returns the sentinel for every non-empty outcome (`:2051` `summary.Outcome == "unresolved"` → `summary.Outcome != ""`); product sha256 `7bb8156f…` | `daemon` (the new unit test; its proven-outcome table must red) | **red, right reason.** Focused command of §3 on the M3 bytes, against the final test file (`7ef3983b…`): `--- FAIL: TestLinuxV2NativeWitnessUnresolvedOutcomeFailsTheCommand` with `linux_owner_witness_linux_test.go:455: no_native_effects owner witness error = owner witness containment is unresolved, want exit 0` and the same for `aborted_no_target` and `recovered`; `FAIL … 0.114s`, exit 1 |
| M4 | the helper returns the sentinel before writing (the `Outcome` check moved above the write); product sha256 `10668131…` | `daemon` (the new unit test; the exact-bytes assertion must red) | **red, right reason.** Same command on the M4 bytes: `linux_owner_witness_linux_test.go:416: unresolved owner witness response = "", want "{\"version\":1,…\"inode\":0}}}\n"` (the response file stayed empty because the write was skipped); `FAIL … 0.182s`, exit 1 |

M3 and M4 are local-only mutants (the guest sets are all `unresolved`, so they cannot reach a proven
outcome or an unwritten response); they are applied in the scratch worktree, exercised with the exact
focused command of §3, and then byte-restored (sha256 re-checked against `7484da8b…`).

## 6. sha256 equality table (the tested bytes)

**Tested bytes = delivered bytes.** The goal worktree's file bytes and the scratch head `a5dfb1b1`'s
file bytes are equal on the two files that carry the behaviour, and the artifacts bind that head:

| Layer | sha256 |
| --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` (goal worktree) | `7484da8b8bf42d11a7294cbd84e9048ee79e2eac37ee97685f54efbde7dc6a54` |
| the same file at scratch head `a5dfb1b1` (what run 3 compiled and executed) | `7484da8b8bf42d11a7294cbd84e9048ee79e2eac37ee97685f54efbde7dc6a54` (equal) |
| `daemon/internal/app/linux_owner_witness_linux_test.go` (goal worktree) | `7ef3983b173f1db2ddeaa5c3b3d32964fc496c9fc44d873ba7fd7d6a358f6115` |
| the same file at scratch head `a5dfb1b1` | `7ef3983b173f1db2ddeaa5c3b3d32964fc496c9fc44d873ba7fd7d6a358f6115` (equal) |
| scratch workflow at `a5dfb1b1` (throwaway, deleted with the branch) | `f3cb9e929780e59583ca23714f27879b5f8046ea87cc292ef43ab2395271d2f4` |
| canonical diff anchor, `git -c diff.mnemonicprefix=false diff -- <the two Go files>` | `47f91d2c141ea0b9a66f05167515232bdf31dd8867fde49d1bca10b718cab7af` |
| the encoded `unresolved` response pinned by the new unit test (§2 probe, 2053 bytes) | `7ad51a380dc5e36afeb0a40047824e053f720320184d82e853b45bf2f9dc632f` |
| run 3 artifact `linux-v2-native-evidence-45536878631-1`: every manifest's `source_commit` | `a5dfb1b1494290a27a8dec71540ab3706c109037` |

Run 3 artifact contents, per set (full sha256):

| Set | `manifest.json` | witness document |
| --- | --- | --- |
| `stage_replay_create_started` | `a58108a68e9af640ce4021abed88495cddd67669d0ef5dd32d3d1b4e10874f9f` | `stage-crash-replay-create_started.json` `0ea0d0f01b376c6710d6ffc83d1331d7f95f41340b9ef1e230d665cd5ba58fe2` |
| `stage_replay_leaf_bound` | `b2555f0085281faacd81b0770b7a8843df245a79b8c3010c6347d76f2fa9e84f` | `stage-crash-replay-leaf_bound.json` `135c7d634b4eec22b3748e5e521a8c38891952bf459789eb3998a9e8b4a95efb` |
| `stage_replay_launcher_bound` | `1e2209c191848cd8f2fb46d202e2e25f2aaa09af8da7acf2f59608fc77baa171` | `stage-crash-replay-launcher_bound.json` `8ef76b55d31390ad00871db44a08eefdfef23aacf847c219bba790600d0e8408` |
| `ack_loss_launcher_bound` (red at §4c's check) | `75c7a5b0422bc6eb960eccae0786abfd9c9afb589a3e3c364938c250237248a3` | `ack-loss-replay-launcher_bound.json` `aca8ffc049d16ae2cab967da8fbb7dd6b03a4fc6c95f149516991e9910799fa6` |

## 7. Appendix: the scratch pins, the workflow and the shim (verbatim, lesson 76)

Scratch runner, values as they ran in every scratch job (no `environment:`, no `secrets.*`, no
`vars.*`; every value below is a dummy and reaches no provider):

```
GH_TOKEN=gho_scratchdummy0000000000000000000000000000
SYMMETRY_OPENAI_API_KEY=sk-scratch-dummy
SYMMETRY_PI_MODEL=deepseek-v4.1-flash-fast
SYMMETRY_OPENCODE_MODEL=deepseek-v4.1-flash-fast
SYMMETRY_OPENAI_BASE_URL=https://litellm-proxy-hk.graydune-4a6be1e2.eastasia.azurecontainerapps.io/v1
GITHUB_EVENT_NAME=workflow_dispatch
```

Throwaway workflow `.github/workflows/scratch-rf-acc-witness-unresolved-exit.yml`: a copy of
`linux-native-v2-acceptance.yml` with the `environment:` block dropped, the dummy pins above in place
of the secret/variable references, and one extra step that installs the scratch-only `gh` shim ahead
of the acceptance steps (its `on: push: branches: [scratch/rf-acc-witness-unresolved-exit]` is the only
trigger). Its per-run sha256:

`scripts/ci/scratch-gh-shim` (sha256 `507340af…`, byte-identical to the copy ratified for
`rf-acc-native-burndown` and reused by the parent goal): answers only `gh auth login --with-token` by
writing `hosts.yml` for the dummy user, then `exec`s the real `gh` for any other invocation. It never
reaches the PR branch and never authenticates a real account.

The M1 and M2 mutants are one-line reverts of the final product file, applied inside the scratch
worktree only (the goal worktree always keeps the final bytes):

| Mutant | Exact edit on the final file's line numbers | Product sha256 |
| --- | --- | --- |
| M1 | `linux_owner_witness_linux.go:556`: `writeLinuxV2NativeWitnessSummaryAndExit(` → `writeLinuxV2NativeWitnessSummary(` | `57adce7e…` |
| M2 | `:501` and `:596` the same revert, `:556` restored | `c6b37cc6…` |

Full-precision digests are in §6.

## 8. Dual audit of the delivered diff

Four independent read-only reviews of the uncommitted diff, two angles × two models (the operator's
dual-audit rule), all on base `3b23dead` with the product file at sha256 `7484da8b…` (unchanged before
and after every review):

| Review | Angle | Product file | Test file read | Verdict |
| --- | --- | --- | --- | --- |
| `AuditInvariantsSol` | recovery/authority invariants | `7484da8b…` | `d79b4db1…` | no code defect; one P3 on the test's proven-outcome tuples (fixed, §5 M3 row and §3) |
| `AuditInvariantsDS` | recovery/authority invariants | `7484da8b…` | `d79b4db1…` | no findings; its census reproduces §1 site by site |
| `AuditIntegritySol` | acceptance integrity | `7484da8b…` | `d79b4db1…`, then the `7ef3983b…` variant | no weakening; P1 (the anchor sha it was handed described the earlier test-file content — the file moved mid-review, and the anchor also lacked its exact command form; both are settled by §6's canonical command and §8b's delta re-check), P2 (a future revert of a call site is not detected by the PR-gated `daemon` job: the e2e is dispatch-only), P3 (the proven-outcome narrowing; the plan now states the three helper-reachable outcomes) |
| `AuditIntegrityDS` | acceptance integrity | `7484da8b…` | `fbf67058…` | no weakening; it independently reconstructed the pinned literal and confirmed its digest equals §2's probe digest, and confirmed the three-outcome table matches the helper's reachable set; P1/P2 as above |

Reconciliation: the only diff-level finding was the test-strength P3 (proven-outcome tuples carried
`RecoveryState:"unresolved"` while asserting nil); the tuples now match what each `RecoveryState`
switch arm requires, and the plan states that `completed`/`peer_denied` never reach the helper. The
refreshment audit of the final test bytes (`7ef3983b…`, the comment-only removal) is §8b.

## 8b. Final-bytes delta re-check

Both integrity reviewers re-checked the frozen bytes after the test-file changes; the anchors were
verified by them, not taken on trust:

- Canonical diff anchor `git -c diff.mnemonicprefix=false diff -- daemon/internal/app/linux_owner_witness_linux.go daemon/internal/app/linux_owner_witness_linux_test.go | sha256sum` →
  `47f91d2c…`, reproduced identically with `3b23dead` as the base (index == base). The earlier
  non-reproducible anchor is explained: the repository sets `diff.mnemonicprefix=true`, so the same
  diff hashes differently under `i/w` (worktree vs index), `c/w` (worktree vs commit) and `a/b`
  prefixes; only the canonical form above is stable.
- `AuditIntegrityDS` (delta re-check): **delta clean** — the product diff is byte-identical to what it
  reviewed (`7484da8b…`), the test file now carries exactly the three helper-reachable proven tuples
  (`no_native_effects`, `aborted_no_target`, `recovered`), the pin was re-derived from the final
  source (2053 bytes, trailing `\n`, sha256 `7ad51a38…` = §2's pre-fix encoder digest), the e2e diff
  is empty, and no skip/removal/struct change was introduced.
- `AuditIntegritySol` (delta re-check): **delta clean** — test-file diff = 98 added, 0 removed lines (99 reproduces only with the `+++` header line counted);
  the byte pin re-derived from the final source (2053 bytes, `7ad51a38…`), the writer-error case still
  two-sided, no `Skip`, `gofmt -l` clean, and the focused command green on the anchored bytes with the
  anchors re-hashed afterwards; it confirmed the plan's reconciled wording matches the test table and
  closed its first-pass P3. Its remaining new finding was a wording fix — this record must say the
  anchor moved because the *test-file content* changed after the pin was issued (5-outcome slice →
  3-tuple table + comment removal), not only because the prefix form was unstated — which §8 now
  states, and the canonical command is §6's.

The earlier pin `94e6dfb8…` therefore names the pre-revision test bytes under the `i/w` prefix form;
it is kept here only as history. The delivered pin is `47f91d2c…` (canonical, both prefix-stable) with
test file `7ef3983b173f1db2ddeaa5c3b3d32964fc496c9fc44d873ba7fd7d6a358f6115`.

## 9. Residual risks and limits

1. **A future revert of a converted call site is not detected by the PR-gated CI.** The `daemon` job
   runs `go test -race ./...` but never the owner-witness e2e (it skips without
   `SYMMETRY_PRODUCTION_LINUX_V2_NATIVE_WITNESS=1`), and `linux-native-v2-acceptance.yml` is
   `workflow_dispatch:` only. The new unit test calls the helper directly, so it pins the helper's
   semantics, not the three call sites. Compensating evidence: the site census (§1), the M1/M2 rows
   (§4) that show each mutant class reddens the guest leg, and the e2e assertion
   (`requireLinuxV2RecoveryExitStatus`) that has stood since `41e64851`.
2. **The choke-point alternative was not adopted.** Both audits proposed moving the sentinel into
   `writeLinuxV2NativeWitnessSummary` itself (with `writeResponseLost` switched to a raw encoder) so a
   future unresolved writer fails closed by construction. That is a different design from the
   contract's ruled Change 1-2 (a separate helper at the three sites), it would make the M1/M2 mutant
   classes undetectable (a reverted call site would still exit non-zero) and it touches the
   response-loss writer, so it needs an owner amendment plus its own binding run; it is recorded here
   as the recommended follow-up hardening rather than changed silently.
3. **Guest rows cover the four owned sets only.** `shared`, `synthetic_goal_cleanup` and the
   release/recovery and owner-loss classes stay with the three carried goals (contract Acceptance 4);
   their `unresolved`-expected legs were not re-measured here.
4. **The race pass was reproduced only as a failure.** §3's row names the exact command, exit code and
   message; the equivalent CI job (with its private-namespace preparation as an unprivileged user) is
   the only environment this stage could not reproduce locally, so the delivered revision's race
   verdict is the PR head's `daemon` job.
5. **No credentialed dispatch.** Q2(a): the four legs hold before `native_release`, so the dummy pins
   suffice and no billed turn is involved; the credentialed rows stay with the parent residual's other
   clause.
6. **The proven-outcome tuples are representative, not tethered.** The helper reads only
   `summary.Outcome`, so the `adjust` closures document each arm's tuple but a future drift in what an
   arm actually sets (for example the switch dropping `NoTargetAbort`) would not red this test; the
   arm-level shape stays pinned by `validateFinalSummary` and its own tests.
7. **`expected == nil` with a bound actual is accepted** (pre-existing semantics; both ruling-E
   auditors recorded it as P3). The witness producer emits all five owner keys with no `omitempty`, so
   the branch is unreachable today; if a future producer may omit keys, red it explicitly.
8. **The table's bound→different case changes PID and identity together**, so pass 1's identity term
   is not isolated (audit P3). A one-line case with equal PIDs and different identities is the
   recommended addition if the file is touched again; it was not added because any edit after run 4
   would invalidate the binding run.

## 10. Ruling-E increment (2026-11-25): the ACK-loss second wall fixed

Conductor ruling E (implement `DECISION_NEEDED F-ackloss-second-wall`; consults
`findings/ackloss-consult-sol.md` and `findings/ackloss-consult-ds.md`, both recommending E and
rejecting P and D) amended Q1(a) for one function: rewrite the second pass of
`linuxV2RequireSameOwnerIdentities` (`daemon/e2e/production_linux_v2_owner_cli_test.go`, that function
only) to iterate the same four expected/actual pairs as the first and skip ONLY pairs whose expected
and actual sides are both the unbound placeholder (`PID <= 1 && Identity == ""`), plus a new
pure-function table test in `daemon/e2e`. Rejected: **P** (omit unbound slots from the witness — loses
the unbound→bound detection and moves pinned bytes across ~20 write sites) and **D** (move the
ACK-loss boundary post-gate — drops pre-gate ACK-loss coverage).

### 10a. The change (frozen bytes)

| Layer | sha256 |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` (goal worktree = scratch head `e150c8e7`) | `99b00fbd84df6425be115593a55a288a57b6ca9ec11f4c79c8692e8782cfec88` |
| `daemon/e2e/production_linux_v2_owner_identity_test.go` (new table test) | `9755123b57cca78eece1b0fc4c88478f9cd032f97bcc3f03b232111a96e5c7fe` |
| canonical diff anchor, `git -c diff.mnemonicprefix=false diff -- daemon/e2e/production_linux_v2_owner_cli_test.go` | `a5e92be78491e9598a0f5763b89e1736e71f2d71ae407043d9848ae0a9affb5e` |
| the two product files' canonical diff anchor (unchanged; §6) | `47f91d2c141ea0b9a66f05167515232bdf31dd8867fde49d1bca10b718cab7af` |
| scratch workflow reused verbatim (runs 3 and 4) | `f3cb9e929780e59583ca23714f27879b5f8046ea87cc292ef43ab2395271d2f4` |
| scratch `gh` shim reused verbatim | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` |

Diff scope: 11 insertions / 3 deletions, both hunks inside the function; the equality condition, its
message, the four slot names and the completeness/duplicate checks are verbatim. The four-pair slice
is lifted to one local shared by both passes so the two cannot drift. The `:3737` helper-response-loss
call site receives the same exemption through the shared function and stays **unexercised** until
`shared` clears its earlier stops (ruling).

### 10b. The four sets on the final bytes (run 4)

Run `45540942939` (acceptance job `138959590401`, 8m51s, conclusion `success`), scratch head
`e150c8e7`; the job's last line is `All 4 case sets exited 0.` This run is the last code-affecting
action on the scratch branch; the branch was deleted afterwards and
`git ls-remote origin scratch/rf-acc-witness-unresolved-exit` prints nothing. Before this run the
leg's failure lines were: `unresolved production recovery exit = <nil>, want deliberate nonzero
process exit` (runs `45481221551`/`45484692731`; fixed by §3) and `launcher_bound ACK-loss replay
duplicated or replaced an owner process: replayed owner set contains an incomplete process identity`
(run `45536878631`, §4c).

| Case set | Boundary | Exit | Witness document | `manifest.json` | Witness facts |
| --- | --- | --- | --- | --- | --- |
| `stage_replay_create_started` | `create_started` | 0 | `stage-crash-replay-create_started.json` `567da6b8…` | `c63d9d1f…`, `status=passed` | `recovery_state=unresolved`, `journal_unproven=true`, `failure_category=containment_unproven` |
| `stage_replay_leaf_bound` | `leaf_bound` | 0 | `stage-crash-replay-leaf_bound.json` `baa222e3…` | `5506d784…`, `status=passed` | same |
| `stage_replay_launcher_bound` | `launcher_bound` | 0 | `stage-crash-replay-launcher_bound.json` `07242ba1…` | `594a0946…`, `status=passed` | same |
| `ack_loss_launcher_bound` | `launcher_bound` (ACK loss) | 0 | `ack-loss-replay-launcher_bound.json` `fa1902f6…` | `76ed78f2…`, `status=passed` | same; `ack_loss={first_response_lost:true, first_exit_code:1, first_failure_category:"response_lost", replay_exit_code:1, replay_outcome:"unresolved", exact_run_key_fence_replay:true, no_duplicate_owner_identity:true}` |

Verbatim per-set lines from the job log: `Case set stage_replay_create_started exited 0`,
`Case set stage_replay_leaf_bound exited 0`, `Case set stage_replay_launcher_bound exited 0`,
`Case set ack_loss_launcher_bound exited 0`, `All 4 case sets exited 0.`

Full artifact digests, artifact `linux-v2-native-evidence-45540942939-1` (id `14989412574`, zip
sha256 `3678a8c6d3035d0cbe288abfeec7d52f7bcb0cf40208a02fe0a4175805276b1c`):

| Set | `manifest.json` sha256 | witness document sha256 |
| --- | --- | --- |
| `stage_replay_create_started` | `c63d9d1f8b225f5b3f125033877c6facab47aae262e740fe73fe0cbcf062d463` | `567da6b83449903ae698c49523c4a248485ee0ea6a4f5580b46af001e3de4939` |
| `stage_replay_leaf_bound` | `5506d7849124fef45e56e432c9c5ed8138a251695e7d6a6237669f5b6a1a55f0` | `baa222e347f83dd8ebb4c6eb36c5acc65513afdc6c0f324dfc420f05a33d06d9` |
| `stage_replay_launcher_bound` | `594a0946e9ebd2b265d0e1428ddecf0eab981c790825c5b7f49839bf007ea99f` | `07242ba19e68e8d0285794c607762b90ca6360e4e5ec5696bb45e3f528328c1d` |
| `ack_loss_launcher_bound` | `76ed78f2ffff791f96f71b3fbd69eb9ee5148fe7464ff4c0b78ac54c317d1666` | `fa1902f6be731086b83a6019d6be0467d477a557c268a411c573e8c36d8f04cb` |

All four manifests bind `source_commit=e150c8e7…` and
`source_tree_digest=sha256:81204f2f8163a33210a369af84780b7afb999364af64f607b488b90c5099769c`, so the
consumed bytes are the frozen bytes above (§10a). The ACK-loss leg now passes both
`linuxV2RequireSameOwnerIdentities` (`:3501`) and the journal check the ruling asked to watch,
`linuxV2ValidateStageReplayJournal` (never executed for this set before) — no new stop, no
`DECISION_NEEDED`. Its test reports `verified TestProductionLinuxV2AckLossReplayMatrix: run=1 pass=1
skip=0` and the same for `/launcher_bound`.

### 10c. Mutants

| id | Mutation | Expected red | Observed |
| --- | --- | --- | --- |
| M-E1 | function restored to dispatch-head bytes (skip reverted); e2e file `74a648da…` | scratch `ack_loss_launcher_bound` red at `:3501`; table test reds on the two placeholder-pass cases | guest: run `45536878631` (head `a5dfb1b1`) — byte-exact: run 3's e2e file *is* the revert's file (`74a648da2712a18bbf23fde2657ffe0db82bfbff0904466f2ff8d2962a3ec4f5`; `git diff 3b23dead a5dfb1b1 -- daemon/e2e/` empty) and its product file is the final `7484da8b…`, red with the `:3501` line above. Local re-run on the reverted bytes: `--- FAIL …/unbound_placeholder_pairs_pass` and `…/pre-gate_rehearsal_passes_with_one_unbound_slot`: `owner identity comparison error = replayed owner set contains an incomplete process identity, want none` |
| M-E2 | the skip moved into the equality pass, the second pass rejecting any incomplete actual again | table test reds | `--- FAIL …/unbound_placeholder_pairs_pass`, `…/pre-gate_rehearsal_passes_with_one_unbound_slot`, same message; file restored `99b00fbd…` |
| M-E3 | the second pass skips any incomplete actual regardless of the marker | table test's `expected == nil` case reds | `--- FAIL …/an_unbound_replay_slot_without_a_marker_slot_fails`: `owner identity comparison error = <nil>, want "incomplete process identity"`; file restored `99b00fbd…` |

M-E1's guest row is the byte-exact run-3 execution instead of a fourth dispatch: the revert restores
the function's dispatch-head bytes, so a fresh dispatch would compile and execute the same two files
(sha256s above); its local table red was re-observed and the mutation restored byte-identically
(`99b00fbd…`, `git status` clean) before the push. M-E2/M-E3 are local-only (the four sets are all
`unresolved`, so they cannot reach a proven outcome), applied in the scratch worktree and restored
byte-identically.

### 10d. Dual audit of the ruling-E delta

Two independent read-only reviews of the exact delta (bytes above), dispatched in parallel; both
returned **no defect**.

| Review | Angle | Verdict |
| --- | --- | --- |
| `AuditE2EInvariants` (Sol-class reviewer) | recovery/authority invariants | no defect. Every pair shape walked: no bound owner can escape completeness or the duplicate checks; the skip is exactly the both-sides-unbound placeholder; equality pass and slot order verbatim. F1-F4 P3 notes: `expected == nil` + bound actual still accepted (pre-existing semantics; unreachable — the producer emits all five keys, no `omitempty`); case 5 does not isolate the identity-only comparator; the `PID <= 1` predicate mirrors the pre-existing completeness check; case 6 pins PID-before-identity ordering |
| `AuditE2EIntegrity` (DeepSeek-class auditor) | acceptance integrity | no defect. Diff is a single hunk inside the function; `requireLinuxV2RecoveryExitStatus` and both call sites byte-identical; all four sha256 anchors match; CI reach verified (`daemon` job `-race ./...` covers `./e2e`; build tag and `TestMain` checked; acceptance `-run` patterns unaffected); every mandated case reds for the ruled reason, with the `expected == nil` case the sole M-E3 discriminator; hygiene clean |

Reconciliation: no P1/P2. Nothing was changed after the audits: the P3 notes stay recorded as limits
(§9 items 7-8) because the ruling's mandated cases are fully covered and any post-run edit would
invalidate run 4.

### 10e. Scratch appendix (lesson 76)

Throwaway workflow `.github/workflows/scratch-rf-acc-witness-unresolved-exit.yml` (sha256
`f3cb9e929780e59583ca23714f27879b5f8046ea87cc292ef43ab2395271d2f4`), the file runs 3 and 4 used,
embedded verbatim:

```yaml
# Throwaway hosted-runner probe for goal rf-acc-witness-unresolved-exit: a copy of
# linux-native-v2-acceptance.yml without the Environment and with dummy credentials.
# Push-only, lives on scratch/rf-acc-witness-unresolved-exit, deleted with it.
name: Scratch witness unresolved exit probe

on:
  push:
    branches: [scratch/rf-acc-witness-unresolved-exit]

permissions:
  contents: read

jobs:
  linux-native-v2-acceptance:
    needs: contracts
    runs-on: ubuntu-latest
    timeout-minutes: 180
    services:
      postgres:
        image: postgres:18.6-alpine
        env:
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: symmetry_linux_v2_acceptance
        ports:
          - 5432:5432
        options: >-
          --health-cmd "pg_isready -U postgres -d symmetry_linux_v2_acceptance"
          --health-interval 5s
          --health-timeout 5s
          --health-retries 12
    env:
      DATABASE_URL: postgresql://postgres:postgres@127.0.0.1:5432/symmetry_linux_v2_acceptance
      SECRET_KEY_BASE: ci-only-linux-v2-secret-key-base-0123456789012345678901234567890123456789
      PHX_HOST: localhost
      PHX_SERVER: "true"
      PORT: "4000"
      SYMMETRY_ENROLLMENT_TOKEN: development-enrollment-token
      SYMMETRY_HEARTBEAT_INTERVAL_MS: "3600000"
      SYMMETRY_POLL_INTERVAL_MS: "1000"
      SYMMETRY_LEASE_DURATION_MS: "3600000"
      SYMMETRY_ASSIGNMENT_DURATION_MS: "300000"
      SYMMETRY_REAPER_INTERVAL_MS: "500"
      SYMMETRY_CONTRACTS_DIR: ${{ github.workspace }}/contracts
    steps:
      - uses: actions/checkout@v7
        with:
          persist-credentials: false
          # The installed Linux owner validates the A-owned Git source as complete history and
          # refuses a `shallow` entry, so this job needs the full repository, not one commit.
          fetch-depth: 0
      - uses: actions/download-artifact@v8
        with:
          name: portal-assets
          path: control/priv/static/portal
      - name: Generate isolated Control operator token
        shell: bash
        run: |
          set -Eeuo pipefail
          token="ci-linux-v2-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-$(openssl rand -hex 32)"
          echo "::add-mask::$token"
          printf 'SYMMETRY_OPERATOR_TOKEN=%s\n' "$token" >> "$GITHUB_ENV"
      - uses: actions/setup-go@v7
        with:
          go-version: '1.27.4'
          cache-dependency-path: daemon/go.sum
      - uses: erlef/setup-beam@v1
        with:
          elixir-version: 1.20.4
          otp-version: 29.0.6
      - name: Build production Control release
        working-directory: control
        run: |
          set -Eeuo pipefail
          python3 ../scripts/ci/linux-v2-source-subject.py "$GITHUB_WORKSPACE" "$GITHUB_SHA" >/dev/null
          MIX_ENV=prod mix deps.get
          MIX_ENV=prod mix release --overwrite
      - name: Install disposable Linux fixture tools
        shell: bash
        run: |
          set -Eeuo pipefail
          sudo apt-get update
          sudo apt-get install --yes --no-install-recommends \
            acl build-essential curl debootstrap gh jq python3 sudo systemd-container
      - name: Probe qualification on this native acceptance host
        shell: bash
        run: bash scripts/ci/linux-v2-host-prerequisite-probe.sh
      - name: Install scratch-only gh credential-ingestion shim
        shell: bash
        run: |
          set -Eeuo pipefail
          shim_dir="$RUNNER_TEMP/scratch-gh-shim"
          mkdir -p "$shim_dir"
          install -m 0755 scripts/ci/scratch-gh-shim "$shim_dir/gh"
          printf '%s\n' "$shim_dir" >> "$GITHUB_PATH"
      - name: Upload narrow host prerequisite evidence
        if: always()
        uses: actions/upload-artifact@v7
        with:
          name: linux-v2-host-prerequisite-${{ github.run_id }}-${{ github.run_attempt }}
          if-no-files-found: warn
          retention-days: 7
          path: ${{ runner.temp }}/linux-v2-host-prerequisite-${{ github.run_id }}-${{ github.run_attempt }}.json
      - name: Build the shared disposable Ubuntu guest rootfs base
        shell: bash
        run: |
          set -Eeuo pipefail
          base="$RUNNER_TEMP/linux-v2-native-rootfs-base-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
          bash scripts/ci/linux-v2-native-rootfs-base.sh "$base"
          printf 'SYMMETRY_LINUX_V2_NATIVE_ROOTFS_BASE=%s\n' "$base" >> "$GITHUB_ENV"
      - name: Run isolated A/B Linux v2 acceptance case sets
        shell: bash
        env:
          GH_TOKEN: gho_scratchdummy0000000000000000000000000000
          SYMMETRY_OPENAI_API_KEY: sk-scratch-dummy
          SYMMETRY_PI_MODEL: deepseek-v4.1-flash-fast
          SYMMETRY_OPENCODE_MODEL: deepseek-v4.1-flash-fast
          SYMMETRY_OPENAI_BASE_URL: https://litellm-proxy-hk.graydune-4a6be1e2.eastasia.azurecontainerapps.io/v1
        run: |
          set -Eeuo pipefail
          evidence_root="$RUNNER_TEMP/linux-v2-native-evidence-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
          case_sets=(
            stage_replay_create_started
            stage_replay_leaf_bound
            stage_replay_launcher_bound
            ack_loss_launcher_bound
          )
          failed_case_sets=()
          for case_set in "${case_sets[@]}"; do
            status=0
            SYMMETRY_LINUX_V2_NATIVE_CASE_SET="$case_set" \
            SYMMETRY_LINUX_V2_NATIVE_EVIDENCE_DIR="$evidence_root/$case_set" \
            GITHUB_EVENT_NAME=workflow_dispatch \
              bash scripts/ci/linux-v2-native-acceptance.sh || status=$?
            if (( status == 0 )) && [[ ! -s "$evidence_root/$case_set/manifest.json" ]]; then
              printf 'Missing sanitized evidence manifest for required case set: %s\n' "$case_set" >&2
              status=1
            fi
            printf 'Case set %s exited %d\n' "$case_set" "$status"
            (( status == 0 )) || failed_case_sets+=("$case_set")
          done
          if (( ${#failed_case_sets[@]} > 0 )); then
            printf 'Failed case sets: %s\n' "${failed_case_sets[*]}" >&2
            exit 1
          fi
          printf 'All %d case sets exited 0.\n' "${#case_sets[@]}"
      - name: Clear isolated Control operator token
        if: always()
        shell: bash
        run: printf 'SYMMETRY_OPERATOR_TOKEN=\n' >> "$GITHUB_ENV"
      - name: Upload allowlisted sanitized Linux v2 evidence
        if: always()
        uses: actions/upload-artifact@v7
        with:
          name: linux-v2-native-evidence-${{ github.run_id }}-${{ github.run_attempt }}
          if-no-files-found: warn
          retention-days: 14
          path: ${{ runner.temp }}/linux-v2-native-evidence-${{ github.run_id }}-${{ github.run_attempt }}/*/*.json

  contracts:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: pnpm/action-setup@v6
        with:
          version: 12.17.0
      - uses: actions/setup-node@v7
        with:
          node-version: 24
          cache: pnpm
      - uses: ./.github/actions/contracts-build
        with:
          build-control-release: "false"
```

The scratch-only `gh` shim `scripts/ci/scratch-gh-shim` (sha256
`507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a`; byte-identical to the copy
ratified for `rf-acc-native-burndown` and reused by the parent goal) answers only
`gh auth login --with-token` by writing `hosts.yml` for the dummy user, then executes the real `gh`
binary for any other invocation; it never reaches the PR branch and never authenticates a real
account. It is pinned by sha256 instead of embedded because its literal binary search path and
config fallback are host locations the record-cleanliness gate refuses; the behavior above is its
whole contract. Both scratch-only files were deleted with the branch and neither ever entered the
goal tree, so this PR carries no workflow change.
