# rf-acc-shared-journal-release — implement plan

Contract: `goal-rf-acc-shared-journal-release.md` (GDD state root, external to the repository).
Base revision: `7111828b902ecfb8885cbd3dd972c6fc0c28918c` (#296 on main).
Branch: `refactor/rf-acc-shared-journal-release`. Bind evidence: `findings/296-binding-run-2.md`
§3.4 (run `45737002125`, log retained at the GDD state root, sha256 `bf00da4f…`).

The accepted plan is the contract plus the Bind evidence; this file resolves the Stage-1
discrimination the contract demands, the fix choice, the proof pins, and what stays out of scope.

## 1. Stage 1 — root cause, named verbatim

The blocking prior record is a **claim-only RunJournal**, not a released or unresolved
containment: `docs/evidence/rf-acc-b-process-readback-2026-11-26.md` and the run log's own
secret-free projection — the journal is inserted at `run-45737002125.log:3146`, the pi leg stops
at `:3226`, and its post-failure projection at `:3229` prints exactly one retained row —
`journal-9c6629….json {"local_state":"claimed","has_claim_grant":true,
"has_clone_intent":false,"has_seed_receipt":false,"recovery_required":false}`.

Why the preceding `alternate_mount` leg leaves it:

1. The witness's fresh path seeds the authentic claim durably before it touches the native owner:
   `checkLinuxV2NativeAdmissionBeforeClaim` → `seedAuthenticClaim` → `prepareLinuxV2NativeOwner`
   (`daemon/internal/app/linux_owner_witness_linux.go:3240,3243,3253`). `seedAuthenticClaim`
   writes `claiming` then `claimed` with the claim grant (`:4257-4292`).
2. The alternate mount is then refused inside the native-owner preflight, after that claim:
   `prepareLinuxV2NativeOwner` builds the factory owner, then `owner.Preflight`
   (`daemon/internal/app/native_run.go:169,197-199`) → `linuxpreflight.VerifyLinuxOwnerInstallation`
   (`daemon/internal/app/linux_owner_v2_linux.go:91`) → `openConfiguredLinuxOwnerParent` →
   `validateLinuxOwnerParentIdentity` (`daemon/internal/linuxpreflight/cgroup_linux.go:80,92-96`;
   the error literal at `:93`). The alternate bind mount's
   measured parent identity differs from the live anchor's manager subgroup by `MountID`
   (`daemon/internal/platform/linux_cgroup_v2_linux.go:239-247`;
   `authority.LinuxCgroupParentIdentity{MountID}` at `daemon/internal/authority/cgroup.go:25-31`).
   Verbatim guest text (probe output, `gdd/native/run-45384436642.log:1227`):
   `-verify-linux-owner-install: configured delegated parent identity differs from the live owner
   anchor's exact manager subgroup`.
3. The refusal path has no release or clear step: the process exits 1 with the claim row, which
   is by design (the journal is written before every effect and is retired only by the product's
   terminal/cleanup path: `cleanupPending` `daemon/internal/app/cleanup.go:893` is the caller of
   `state.DeleteJournal` `daemon/internal/state/run_journal.go:423`; the witness writes no
   terminal transition).

Discrimination against the contract's three candidates:

- (a) **confirmed**: the refusal path legitimately retains an unresolved-by-predicate row; the
  product itself classifies it as blocking — `linuxV2ForeignJournalIsSafelyRetired`
  (`daemon/internal/state/linux_cgroup_journal.go:432-475`) accepts only release/pre-gate/abort/
  reuse-retired/v1-receipt shapes, so `scanLinuxV2NativeAdmissionLocked` refuses a fresh native
  claim with `foreign RunJournal … has unresolved native ownership` (`:382`). The store test
  `TestLinuxV2AdmissionRetainsPreCheckoutCloneAfterRelease` pins "unreleased row" as not retired
  (`daemon/internal/state/linux_cgroup_v2_test.go:973`, row at `:1095`).
- (b) rejected as the fix: making the refusal leg release or retire the row is a production
  change (DECISION_NEEDED, not owned here), and no product command retires a claim-only row in
  bounded time (containment-only recovery no-ops on it: `recoverContainmentOnlyJournals`
  `daemon/internal/app/linux_owner_recovery_linux.go:1622-1650`).
- (c) rejected: relaxing the pi leg's precondition would need to match a product gate that also
  refuses, and the contract forbids weakening it.

## 2. The fix (Stage 2, wrong side): the refusal leg's own case set

Round 0 ordered the leg after the ten fresh/released-Store legs in `shared` (head `84dc22c3`:
call at `scripts/ci/linux-v2-native-acceptance.sh:3187-3190`, runtime order guard at `:3034-3039`)
and pinned its cause from the captured App stderr. The round-1 conductor ruling replaced the
ordering with isolation (option (b)): the leg runs in its own case set `alternate_mount_refusal`,
so its by-design claim-only row lives in that guest's own installed StateDir and is invisible to
every other set. The mount call and the order guard are deleted from `shared`, whose remaining
legs keep base order; the leg's assertions are byte-equivalent. No journal is deleted, no Store is
reset, and no leg's assertions are weakened.

Why the same guest cannot hold an isolated Store: the e2e fixture requires every leg's config
`state_dir` to equal the root-installed `OwnerConfig.StateDir`
(`production_linux_v2_native_witness_test.go:1953-1956`), the production witness refuses any other
StateDir (`linux_owner_witness_linux.go:1217`, pinned by
`TestLinuxV2NativeWitnessStateDirExceptionIsExactAndInstalled`), and the acceptance guest installs
exactly one such StateDir (`linux-v2-native-acceptance.sh:505,567-586`). Each case set is a fresh
guest (`:79`, `:963`; workflow `:119-140`), so a separate set is the only in-scope isolation.

The cause pin stays: the leg reads the App stderr it already captures
(`production_linux_v2_owner_cli_test.go:4857`) and requires the mount-identity preflight refusal
on a line that also carries the witness marker, and forbids the foreign-ownership refusal
(`linuxV2RequireMountRefusalCause`, `:2314`) — a wrong-cause refusal must red the leg, not satisfy
it. The pinned text is grounded in the App's own print path, not in prose: `main.go:35` prints
`-linux-v2-native-owner-witness: <err>`; `linux_owner_witness_linux.go:3258` wraps the preflight
as `preflight installed Linux v2 owner before clone/native work: %w`; `native_run.go:199` wraps it
as `Linux v2 native owner preflight: %w`; `cgroup_linux.go:93` is the bare cause phrase.

## 3. Public/API shape

1. `scripts/ci/linux-v2-native-acceptance.sh` — `alternate_mount_refusal` joins the case-set
   allowlist (`:33`) and the dispatch (`:3213-3215`); the mount-leg call and its two-line order
   comment are deleted from `shared`; the runtime order guard is deleted from
   `run_alternate_mount_refusal_witness`, which keeps a two-line why comment naming the retained
   claim-only row and the isolation.
2. `.github/workflows/linux-native-v2-acceptance.yml` — `alternate_mount_refusal` joins the
   `case_sets` array (`:130`).
3. `scripts/ci/export-linux-v2-redacted-evidence.py` — the mount witness moves from `shared`'s
   allowlist to `alternate_mount_refusal`'s, and the new set joins `PI_DURABILITY_CASE_SETS`
   (the script exports every non-containment set with `--native-kind pi`).
4. `scripts/ci/test-linux-v2-evidence.py` — the case-set pin also checks the dispatch arms, the
   mount-call/arm isolation, the exporter case-set catalog and allowlist disjointness, with two
   committed red mutations.
5. `scripts/ci/test-ci-structure-extraction.py` — a new pin: the workflow `case_sets` array
   equals the script allowlist, with a committed red mutation.
6. `daemon/e2e/production_linux_v2_owner_cli_test.go`
   - `runProductionLinuxV2AlternateMountRefusal`: require the exact refusal cause from
     `linuxV2OwnerWitnessStderrTail` (`daemon/e2e/production_linux_v2_native_witness_test.go:2216`)
     and require the leg's own retained row to be the claim-only shape: found, `claimed`, claim
     id and a claim grant carrying a recorded (non-zero) expiry, no clone intent, no process
     details, no pending containment (authority, handoff, scan ledger or abort-less reservation),
     no unproven stop, no cgroup reservation even when aborted, and no release tombstone.
   - New pure predicates `linuxV2RequireMountRefusalCause(lines)` and
     `linuxV2RequireClaimOnlyRetainedRow(journal, found)`, each with a unit table row per branch:
     7 rows for the cause predicate (an unmarked capture is refused, the foreign-ownership phrase
     is refused anywhere, and the mount phrase is required on a line that also carries the witness
     marker: accepted chain, empty, bare probe, foreign, unrelated, masked, both causes) and 15 for
     the row predicate (zero-value row plus 14 field mutations, with the `found=false` refusal
     pinned by a direct call).
   - One row pinning the capture read refusal
     (`TestLinuxV2AlternateMountRefusalCaptureReadRefused`).
   - One unit row pinning the unchanged precondition's treatment of a claim-only prior record
     (`linuxV2RequireSafelyReleasedPriorJournal` refuses it) as the isolation rationale.
   - The `:1614` comment is why-only; the capture-read comment no longer claims an ordering
     rationale. No assertion changes.
7. `daemon/e2e/production_linux_v2_native_witness_test.go` — no change.
8. Records: this plan, an evidence record, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`.

## 4. Choices the accepted plan left open

- **(b) not (a)**: product-side retirement of claim-only refusal journals stays out of this PR
  (production byte; recorded as a `docs/TODO.md` residual with owner conductor) — the separate
  case set gives the same order-independence without touching durable state or production code.
  Round 0's claim that the opencode leg was harmless was false (its fixture reads the Store at
  `native_witness_test.go:1968-1976` before the App's version gate), which is why ordering alone
  could not deliver the ruling.
- **No ordering left to guard**: with the leg alone in its set there is no prior leg to order
  against, so the runtime guard is deleted rather than kept as dead weight. The stderr cause pin
  stays: a wrong-cause refusal (for example a foreign-journal admission) must red the leg.
- **The set id follows the existing naming**: `alternate_mount_refusal` (lowercase underscores).
- **No new precondition on the moved leg**: requiring the fixture's stricter released-row
  predicate for prior rows is moot in its own Store; the leg asserts only its own cause and its
  own retained row.
- **Pi-durability**: the mount witness binds the staged provider-free pi executable (the writer
  binds it for any provider-free pi witness), so the new set joins `PI_DURABILITY_CASE_SETS` and
  the existing export validation covers it unchanged.
- **Witness allowlists stay disjoint**: the mount witness is expected only by its own set, so a
  `shared` export can no longer require a file `shared` never produces.
- **Subsumed disjuncts dropped**: `ContainmentAuthority != nil` and `ContainmentHandoff != nil`
  were removed from the row predicate after the mutant run showed `HasPendingContainment()` alone
  reds their rows; keeping them would have shipped branches no mutant can distinguish.
- **Aborted reservations stay refused**: `ContainmentCgroup != nil` is kept beside
  `HasPendingContainment()` because an aborted reservation (`AbortProof != nil`) escapes the
  latter; the "an aborted cgroup reservation was written" row is the only one that reds when the
  disjunct goes.
- **Comment wording** stays within the 2-line cap; the script comment names the reason, not the
  whole history.

## 5. Per-clause baseline and proof

| Contract clause | Baseline (before) | Proof |
| --- | --- | --- |
| 1 root cause named verbatim | `:91` stop, run `45737002125`; row shape from the log projection | record §1-§2 with the measured log lines (`:3146`, `:3226`, `:3229`) and the file:line anchors of record §10 |
| 2 fix on the wrong side, no ad-hoc deletion | leg in `shared` (round 0: after the ten legs, with a runtime order guard) | record §15: the mount call and guard are gone from `shared`, no `rm`/Store reset in the diff, the case-set/arm pins red under mutation; the export replica proves the new set's own path |
| 2 no weakened precondition or refusal assertion | — | record §9 + §15: `git diff` shows no change to `linuxV2RequireSafelyReleasedPriorJournal`, and the mount leg keeps every assertion byte-equivalent |
| Invariants: ≤2-line comments, `go vet`, records rule | — | record §15: comment-cap before the re-measure, `go vet` rows, mutants for every new pin |
| Acceptance: `:91` stop stays gone in `shared`; the new set passes its leg | — | met by Bind 2 (run `45778210044` on `66b27e63`; record §Bind 2): `shared` keeps the `:91` stop gone (next stop, the pi admission residual, verbatim); the new set passes and exports its witness |

## 6. Local gates

From `daemon/`: `gofmt -l e2e/`; `go vet ./...` (Windows, pinned go1.27.4) and
`GOOS=linux go vet ./...`; the new rows plus the `./e2e/` package; then the full
`go test ./...` on Windows and in WSL (`wsl -d Ubuntu-24.04`, root and unprivileged, go1.27.0),
never beside a measurement harness. From the repo root: `bash -n
scripts/ci/linux-v2-native-acceptance.sh`; `actionlint` over the workflows; `python
scripts/ci/test-linux-v2-evidence.py` (WSL), `python
scripts/ci/test-ci-structure-extraction.py`, `python
scripts/ci/test-ci-changed-path-classifier.py`. Scratch harnesses, not committed:
`replica-export.py` (green/missing/unbound export rows) and the pin mutants in a scratch clone.
Comment cap: `python <HOST-PATH>/.local/state/gdd/scratch/comment-cap.py <clone> 7111828b`
before the final re-measure of digests and anchors. jscpd via the existing install (symlinked
into the scratch clone; never `pnpm install`), plus `check-record-cleanliness.py` and
`model-routing-record-sanitize.py`. The records round re-ran the script-dependent rows at the
head and the clone gates on a scratch-clone commit of the final records bytes (record §15.4,
§16).

## 7. Out of scope

- Any production change (claim seeding before the preflight, retirement of a claim that
  provably never started native work): recorded as the option-(a) residual in `docs/TODO.md`.
- The opencode leg's pinned-version gate: its own tracked entry. The `stop_readback_reader` row
  was measured clean in Bind 2's `alternate_mount_refusal` (record §Bind 2); `shared` still
  aborts before its call site (`docs/TODO.md`, readback residual).
- Weakening, deleting or renaming any refusal assertion; deleting journals; changing the
  `shared` arm's remaining leg order.
