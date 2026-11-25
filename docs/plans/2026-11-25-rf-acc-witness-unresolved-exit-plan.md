# rf-acc-witness-unresolved-exit — implement plan

Contract: `goal-rf-acc-witness-unresolved-exit.md` (GDD state root, external to the repository).
Base revision: `3b23dead` (#281 — the parent goal's merge and this goal's dispatch gate).
Branch: `refactor/rf-acc-witness-unresolved-exit`. Rulings: Q1(a) (the e2e is the contract: the three
unresolved branches return a deliberate error after writing the summary), Q2(a) (the ratified scratch
pattern with dummy pins, at most 3 runs).

The accepted plan is the contract; this file resolves only the implementation choices the contract
leaves open, the per-clause baseline, and what is out of scope.

## 1. Clause census at the dispatch head (before any edit)

Contract line numbers are origin/main `33ad88a9`, re-located by content at `3b23dead`. Every cited
site sits within six lines of its contract address:

| Contract | Dispatch head | Content | Disposition |
| --- | --- | --- | --- |
| `:501` | `:501` | the bare `writeLinuxV2NativeWitnessSummary` return after the legacy-unproven `unresolved` assignment (`:480`) | convert to the helper |
| `:556` | `:556` | the same return after the unresolved-authority recovery assignment (`:538`); the authority guard `:531-534` is unchanged | convert |
| `:596` | `:596` | the same return, shared by every outcome of the `RecoveryState` switch whose `case "unresolved"` sits at `:571` | convert (the helper returns nil for every proven outcome) |
| `:604-625` | `:604-630` | `writeResponseLost` writes the summary at `:618`, then joins `execution.ErrLinuxV2MutationAckLost` at `:624` | exempt: every caller already returns non-nil |
| `:1456` | `:1456` | `Outcome = "unresolved"` inside `summarizeResponseLost` | exempt, same reason (its only caller is `writeResponseLost`) |
| `:420` | `:420` | fail-closed struct-literal initializer `Outcome: "unresolved"` | no writer of its own; its bytes are only written through the sites above |
| `:676` | `:676` | the fresh positive path's final write (`completed`/`recovered`/`peer_denied`) | untouched: proven outcomes keep exit 0 |
| e2e `:176-193` | e2e `:184-199` | `requireLinuxV2RecoveryExitStatus` | read-only under Q1(a); its stage-runner and ack-runner call sites are `:1883`, `:3461`, `:3496`, `:3726`. Ruling E (§7) amends the owned set for `linuxV2RequireSameOwnerIdentities` only, not for this assertion |

Existing pins of the two-sided mapping: `daemon/cmd/symmetry-daemon/main.go:34-37` (error ⇒ one stderr
line plus `os.Exit(1)`), and the unresolved summary shape this helper decides on is already pinned by
`TestLinuxV2NativeWitnessStageReplayRequiresExactUnresolvedCategory` (`linux_owner_witness_linux_test.go:348`).
No existing test calls `writeLinuxV2NativeWitnessSummary` directly, so the probe below is the first
isolated execution of the write path.

Probe first (lesson 74): a throwaway probe test (deleted at the end, never committed) runs the
status-quo writer for the `unresolved` summary and for each proven outcome, and logs the byte length,
the sha256 and the returned error (§4). That is the characterization baseline: on the base bytes
`err == nil` for `unresolved` — exactly the clause this goal inverts — and the e2e's own failing line in
runs `45481221551`/`45484692731` is the already-recorded end-to-end failure.

## 2. Public/API shape

1. `var errLinuxV2NativeWitnessContainmentUnproven = errors.New("owner witness containment is unresolved")`
   next to `writeLinuxV2NativeWitnessSummary` (`:2031`), with one why-only comment (≤2 lines): the
   acceptance job reads the exit status, not only the response file.
2. `func writeLinuxV2NativeWitnessSummaryAndExit(output io.Writer, summary linuxV2NativeWitnessSummary) error`
   — encode through the existing writer first and return its error verbatim (the writer already wraps
   the encoder failure with `%w`); then return the sentinel iff `summary.Outcome == "unresolved"`;
   otherwise nil. Nothing else: no struct, JSON tag or summary field changes.
3. The three call sites above call the helper. Every other `writeLinuxV2NativeWitnessSummary` call
   stays literal, so the response-loss and positive paths keep their current error values.

## 3. Implementation choices the contract leaves open

- **Sentinel text**: `owner witness containment is unresolved`, so the observed stderr line is
  `-linux-v2-native-owner-witness: owner witness containment is unresolved`. It names the invariant
  (unproven containment) instead of one branch, and carries no claim/lease token or secret.
- **Test fixture**: the three cases share one literal `linuxV2NativeWitnessSummary` (version, case,
  harness, run key, fence, subject, outcome). The exact-bytes case pins the full JSON literal (struct
  order, `\n`-terminated), so any response-byte change reds the test; the writer-error case uses a
  one-method failing writer defined in the test file and asserts the wrapped write error is returned and
  the sentinel is not; the proven-outcome case tables the three outcomes the helper can carry —
  `no_native_effects` (legacy else + switch arm), `aborted_no_target` and `recovered` (switch arms) —
  with the field tuple each arm requires, and asserts nil plus non-empty bytes. The fresh positive
  path's `completed`/`peer_denied` never reach the helper (its writer at `:676` stays literal), so
  pinning them here would assert behaviour this helper cannot produce.
- **Scratch run order and budget** (cap 3 = classes + 2, lesson 69): the two product-mutant classes
  redden the guest legs, so they run first — run 1 = M1 (`:556` reverted) with
  `stage_replay_create_started`; run 2 = M2 (`:501` + `:596` reverted) with `stage_replay_leaf_bound`,
  `stage_replay_launcher_bound` and `ack_loss_launcher_bound`; run 3 = the final bytes with all four
  sets, as the last code-affecting action (lesson 86). The scratch-only workflow and `gh` shim are
  copied from the ratified burndown recipe; the goal branch itself is never pushed.
- **TODO amendment (contract §Change 6)**: the "parent residual clause (1)" is the
  `rf-acc-witness-unresolved-exit` bullet inside `## rf-acc-pi-durability-matrices: pi-evidence residual
  (2026-11-24)`; clause (2) is the `rf-acc-release-recovery-diagnostics` bullet and the F4 item is
  `rf-acc-owner-loss-semantics`, both referenced there. The bullet gains its closure statement (branch,
  plan, evidence record) and stays in `docs/TODO.md`; no CLOSED-TODO move while clause (2) is open
  (contract). Its sibling bullets and every other entry stay byte-identical.

## 4. Evidence plan

- `docs/evidence/rf-acc-witness-unresolved-exit-2026-11-25.md`: per-set rows (before line from runs
  `45481221551`/`45484692731`; after: run id, case set, boundary, exit status, manifest), the probe
  logs, the census (`Outcome = "unresolved"` sites, each converted / exempt / covered), the M1-M4
  mutant rows naming the job that reddens, and the sha256 equality table of the tested bytes (goal
  worktree vs scratch head) plus the base-writer vs helper response-byte sha256. Appendix: the scratch
  workflow, the shim and the pins verbatim (lesson 76).
- The TODO amendment above.

## 5. Local gates

WSL Ubuntu-24.04 with `GOTOOLCHAIN=go1.27.0` (`daemon/go.mod` requires `go 1.27`; the WSL default
1.26.8 is refused under `GOTOOLCHAIN=local`): `gofmt -l .`, `GOOS=linux go vet ./...`, the focused test,
the `TestLinuxV2Native` suite, `go test -race -count=1 ./internal/app/`; plus the `git diff` checks that
the product file's diff is the helper/sentinel and the three call sites only and that the e2e diff is
the ruling-E function and its table test only. The `daemon` CI job on the PR head runs the full race
pass. No scratch run is dispatched concurrently with a local Go suite (host memory).

## 6. Out of scope (with reasons)

- `daemon/e2e/**` outside ruling E's two paths (which amend Q1(a) for this function and its
  table test only), `daemon/internal/platform/**`,
  `daemon/internal/execution/**`, `control/**`, `scripts/ci/**`, the parent goal's fixture bytes and
  `docs/design/**`: not owned.
- The release/recovery class, owner-loss resolution semantics and non-retargeted sets: the three
  separate carried goals named in contract Acceptance 4.
- The parent residual's credential-row and frozen-OpenCode bullets stay untouched.

## 7. Ruling-E increment (2026-11-25, implement DECISION_NEEDED F-ackloss-second-wall)

The conductor amended Q1(a)'s read-only e2e boundary (ruling E) after run 3 (`45536878631`)
showed `ack_loss_launcher_bound` passing `requireLinuxV2RecoveryExitStatus` and stopping at
`linuxV2RequireSameOwnerIdentities` (`production_linux_v2_owner_cli_test.go:3501`), a
completeness loop no pre-gate replay can satisfy.

1. `linuxV2RequireSameOwnerIdentities` (same file, that function only): the four
   expected/actual pairs are lifted to one local slice shared by both passes so they cannot
   drift; the second pass iterates it and skips ONLY a pair whose expected and actual sides
   are both the unbound placeholder (`PID <= 1 && Identity == ""`). The equality condition
   and message, the completeness check for every other pair, and the duplicate PID/identity
   checks are unchanged. `expected == nil` with a present actual is not a placeholder pair,
   so an incomplete actual there still fails. (The fix increment later added the journal-bound
   condition — §8.)
2. `daemon/e2e/production_linux_v2_owner_identity_test.go` (new, `//go:build linux && amd64`,
   `package e2e_test`): the mandatory pure-function table test
   `TestLinuxV2RequireSameOwnerIdentities` — equal `{}` pass; the pre-gate rehearsal shape
   (three bound slots plus one unbound) pass; bound↔`{}` reds in both directions;
   bound→different reds; duplicate PID and duplicate identity red; `expected == nil` with an
   unbound actual red. It runs in the PR-gated `daemon` job (`go test -race ./...` from
   `daemon/` covers `./e2e`; the package `TestMain` only swaps the containment supervisor
   and gates nothing).
3. Mutants: M-E2 (the skip moved into the equality pass) and M-E3 (skip any incomplete
   actual regardless of the marker) are local-only and run the focused e2e table test;
   M-E1 (skip reverted) restores the function to its dispatch-head bytes — exactly the e2e
   file run 3 executed (file sha256 `74a648da…` at run 3 and under the revert) — so its
   guest red at `:3501` is run 3's row and its table-test red is re-run locally.
4. Scratch: one new run on the final bytes (the ruling's binding action), all four case
   sets, branch `scratch/rf-acc-witness-unresolved-exit`, the run-3 workflow (`f3cb9e92…`)
   and shim (`507340af…`) reused verbatim. The `:3737` helper-response-loss call site
   receives the same exemption through the shared function and stays unexercised until
   `shared` clears its earlier stops.

## 8. Fix increment (2026-11-25, review MUST_FIX R1/R2)

The review found that the ruling-E exemption could hide a slot the held journal proves bound (R2) and
that §4 of the record contradicted §10 (R1).

R2 is implemented at the exemption's single site: the skip additionally requires that the
boundary-validated journal does not bind the slot, projected the way the witness's
`fillSummaryFromJournal` does from the reservation, the handoff, the authority or the two release
proofs. Both replays pass the journal each already validated against its held marker. The table test
gains the journal column, the journal constructors and the journal-bound placeholder row; ruling
rows (a)/(b) are that new row and the existing pre-gate rehearsal row. M-E1..M-E3 are re-observed on
the new bytes and M-E4 covers the journal condition; because the function now carries the journal
parameter, M-E1's revert is no longer byte-identical to any pushed file.

Because the e2e bytes changed after run 4, run 4's binding is void for the delivered bytes (lesson
86); the fix ruling authorizes a single re-bind run on the final bytes, and it is outstanding. R1's
record amendments are §4/§6/§11 of the record; TODO's clause (1) entry carries the tightening and the
outstanding re-bind.
