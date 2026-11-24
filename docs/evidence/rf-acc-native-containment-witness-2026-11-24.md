# rf-acc-native-containment-witness — evidence record (2026-11-24, implement + apply stages)

Contract: `goal-rf-acc-native-containment-witness.md` (GDD state root, external to the repository).
Base `477e4c62` (#277, the rf-acc-native-burndown merge). Branch
`refactor/rf-acc-native-containment-witness`. Scratch branch
`scratch/rf-acc-native-containment-witness` (deleted at stage close, §9). Every hash below is a
working-tree byte hash; the §1 rows cite the final (post-apply) bytes.

Two clauses are proven green by the final run: the containment rows now name their post-seed App
refusal (bounded, redacted, in the job log) and end at a recorded `DECISION_NEEDED` product cause;
`synthetic_goal_cleanup` is past its admission refusal and stops at a later named check.

## 1. Binding — final bytes, runs, and scratch-only bytes

| File (working tree) | sha256 |
| --- | --- |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `80b9f557b396118dbb7f3b0b369e2054ec5010aeeceec43083c3fd243e02e23c` |
| `daemon/e2e/production_linux_v2_native_witness_test.go` | `f297fd5d6b1bef002bc79d5d31bf99651024cc12eb3e972a2881d35fd65bb1d7` |
| `daemon/e2e/production_linux_v2_cleanup_witness_test.go` | `d120677d8718d0ad346d0fdfc9ef6136234d1b48071d77e3506aac4c844f01e6` |
| `scripts/ci/linux-v2-native-acceptance.sh` | `08f4dbd91951e500ad7d549556ff4fe41e94c717f1a375f244b8c606c443788f` |
| `scripts/ci/export-linux-v2-redacted-evidence.py` | `2452803f04223353d35088f041f28490dff0aec5156c38d462fedc3f191cfc2c` |
| `scripts/ci/test-linux-v2-evidence.py` | `0ee65be478bf837b3e21cb9e4586789c7d594439b7bdba488cf2912eadff3622` |
| `docs/plans/2026-11-24-rf-acc-native-containment-witness-plan.md` | `2f15f6a1e74391eaa0540ab858b2695364f7dca3de089b07d4d521c9016e3e64` |

Run `45465331434` ran on scratch head `65bfeaf2`, whose six code/script rows were byte-identical
to the pre-apply copies of the rows above (`cmp` clean for each) plus the two scratch-only files
of §10. The apply stage's four simplifications (§12) are behavior-neutral, and the rows above are
the final bytes; the hosted re-proof of the acceptance clause on these bytes is conductor-owned,
because the apply stage cannot push. `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this
record are documentation-only and postdate the run.

| Run | Head | Sets | Result |
| --- | --- | --- | --- |
| `45388258526` | `99277aed` (before-state, burndown) | all 14 | containment rows unnamed; `synthetic_goal_cleanup` at the admission refusal (§3) |
| `45431551609` | `7072891f` (burndown binding run) | all 14 | pre-change baseline for "no previously green row reddens" (§6) |
| `45452357027` | `05ec6ec8` | `codex_containment`, `claude_containment`, `synthetic_goal_cleanup`, `migration` | names the fixed-B HOME refusal (§7.1) |
| `45455325059` | `305a5d0f` | same four | attempt 1 died in the `contracts` job (known frontend flake, §11); `gh run rerun` passed it; the acceptance job (8m10s) names the clone-admission refusal (§7.2) |
| `45458181149` | `1ff5ad44` | all 14 | every row named; job 23m04s; its failure blocks are byte-identical to run 4's, but one doc comment was trimmed to the two-line comment policy afterwards, so it is not the binding run |
| `45465331434` | `65bfeaf2` | all 14 (final literal, §6) | every row named; job 23m23s |

## 2. Change 1 — the captured App stderr, its redaction, and the failure report

- Sink: `linuxV2OwnerWitnessStderrPath` (`owner-witness.stderr` beside the response file) replaces
  `os.DevNull` as the command's stderr (`daemon/e2e/production_linux_v2_owner_cli_test.go:52`), opened
  `O_CREATE|O_TRUNC|O_WRONLY` 0600, one per command start, in the A-private `artifactDir`.
- Helpers (`production_linux_v2_native_witness_test.go:2001-2083`):
  `linuxV2KnownCredentialValues` (every `os.Environ` name matching
  `linuxV2IsCredentialEnvironmentName` — which covers the writer's `SYMMETRY_OPERATOR_TOKEN` /
  `SYMMETRY_ENROLLMENT_TOKEN` cases — the request's lease token, and the provider token),
  `linuxV2OwnerWitnessStderrTail` (last ≤4 KiB / ≤40 lines, every known value replaced with
  `<redacted>`, then refuses — error, no text — if one survives),
  `linuxV2OwnerWitnessStderrBoundedLines` (≤9 lines, each ≤300 content bytes).
- Report: `linuxV2ReportOwnerWitnessFailure` (`owner_cli_test.go:1573-1592`) waits ≤5 s for the
  pidfd, logs `owner-witness command exit status: <n>`, then the tail as separate `t.Logf` lines
  before each `t.Fatalf`. Call sites: the barrier wait and all eight `waitLinuxV2OwnerWitnessStage`
  waits (`:1744`, `:2377`, `:3387`, `:3566`, `:3797`, `:3963`, `:4122`, `:4444`).
- Local probes, before any dispatch:
  - `print_witness_output_lines` extracted verbatim from the script and fed a real non-verbose
    `go test -json` failing test (throwaway module): the `exit status` and 279-byte diagnostic lines
    printed; a 420-byte content line and a line carrying `development-operator-token` were dropped;
    last-10/400-byte semantics unchanged.
  - Unit tests `TestLinuxV2OwnerWitnessStderrCaptureRedactsAndBounds` (44 lines → last 40, four
    credential values replaced, diagnostic kept, one over-long line truncated, ≤9 logged lines) and
    `TestLinuxV2OwnerWitnessStderrCaptureRefusesMarkerCarriedValue` (clean tail passes; a value the
    marker itself carries refuses with no text). Both PASS on a linux/amd64 test binary (§8).
- Before/after rows: `45388258526` rows 1-2 end at an unnamed 30.7 s barrier wait with no App stderr
  in the log (burndown record §3.13); runs `45452357027`, `45455325059`, `45458181149` and the
  binding run `45465331434` print the bounded, redacted App diagnostic plus the real exit status for
  the same rows (§7).

## 3. Change 2 — the cleanup witness's App admission check

`linuxV2CleanupJournalCallbacks` (`production_linux_v2_cleanup_witness_test.go:1078-1081`) now sets
`CheckNativeAdmission: func() error { return store.CheckLinuxV2NativeAdmission(key, fence, time.Now().UTC(), nil) }`
— the App's own pre-RELEASE CAS (`daemon/internal/app/native_run.go:210-214`,
`daemon/internal/state/linux_cgroup_journal.go:520-537`), with the fixture's nil live-owner map. No
launcher guard and no `daemon/internal/**` byte changed.

- Before: `45388258526` row 4, `production_linux_v2_cleanup_witness_test.go:499: … start "/bin/sh":
  Linux v2 native release requires an App admission check` (2.00 s).
- After: `45458181149` row 4, same call site with a later cause —
  `resume process "/bin/sh": one-use Linux v2 RELEASE result is unresolved: Linux owner helper
  rejected operation (release_outcome_unknown)` (1.91 s), plus the cleanup line
  `production_linux_v2_cleanup_witness_test.go:488: … dial unixpacket: connect: connection refused`.
- The recheck itself fired and passed: a failing recheck would surface as
  `recheck cross-RunKey native admission before Linux v2 RELEASE` (`linux_owner_v2_linux.go:1108`)
  *without* the `one-use … unresolved` wrap, which requires `releaseAttempted`
  (`linux_owner_v2_linux.go:1066`, set only after the recheck at `:1118`). The observed wrap is the
  helper's `release_outcome_unknown` (`daemon/internal/platform/helper_v2_server.go:1188`).
- Mutant row: remove the callback → the `:499` admission refusal returns (the before run).

## 4. Carry-over P3 fixes in `scripts/ci/linux-v2-native-acceptance.sh`

- P3-1: the `shared` set's unprovisioned-gate failure branch now pipes `grep … | head -n 5` through
  `print_filtered_output_lines` (`:1556`), like the `run_go_test` twin.
- P3-2: both Change-1 `systemctl show` blocks and the `journalctl --output=cat -n 80 --grep=…` dump
  (`:1615-1631`) capture to `$fixture_root/daemon-service-failure.log` and print through
  `print_filtered_output_lines`; the narrow `--grep` and `-n 80` bound are unchanged. The branch had
  0 firings in run `45458181149` (0 `systemctl show --property=Result` lines).
- P3-3: the two probe pipelines (`:1699-1708`) became capture-then-filter, so `machine_run_as` runs
  in the parent shell. Probe (throwaway stub): pipeline shape → `count after pipelines=0` and the
  unit `probe-step-1` reused twice; capture shape → `count after captures=2`, units `step-1`,
  `step-2`. Filter probe on the extracted function: a 420-byte line and a secret-bearing line were
  dropped, the last 40 of 48 lines printed, exit 0.

## 5. Exporter exclusion (conductor ruling Q3)

`export-linux-v2-redacted-evidence.py` gains `EXCLUDED_WITNESS_NAMES = ("owner-witness.stderr",)`
and `require_no_excluded_witness_file` (all three modes, before any read): an input root that
carries the capture refuses publication. `test-linux-v2-evidence.py` gains
`test_manual_containment_refuses_the_owner_witness_stderr_capture` (the apply stage removed the
successful-export test's added `assertNotIn`, §12). Mutant row: with the guard stashed, the new
test fails (`AssertionError: 0 == 0` — the export succeeded); with the guard, the full suite is
`Ran 43 tests … OK` (§8).

## 6. The final literal all-14 run (`45465331434`, head `65bfeaf2`, job 23m23s)

Run `45458181149` (job 23m04s) first ran these bytes minus one trimmed doc comment and produced
byte-identical failure blocks; §2's comment trim forced the re-bind, and run 4 is the binding run.
The per-set durations below are run 4's.

Scratch workflow = the real job minus `environment`/`secrets.*`/`vars.*` with the contract's dummy
values and `GITHUB_EVENT_NAME=workflow_dispatch`, plus the ratified scratch-only `gh` shim (the
fixture's `gh auth login` gate refuses the dummy `gho_` token; without it every set dies before the
guest boots). Job-level facts: 1 base build, 14 clones (`Cloning the shared disposable guest rootfs
base for this case set` ×14), 14 `retired` lines (`0` ×1, `5` ×13), 14 daemon smokes
(`Stopping installed daemon.service before direct witnesses` ×14, 0 failed starts, 0 Change-1
diagnostic firings), 220 `owner witness config native harness version differs from the tested
release` lines (210 fixture probes + 10 captured App diagnostics), 12 reaped `owner-witness command
exit status` lines, 0 `stayed live`, 0 `requires an App admission check`, 0 `fixed-B native HOME is
outside`.

| # | Case set | Last step reached | Exit | First failing line / check with its reason |
| --- | --- | --- | --- | --- |
| 1 | `codex_containment` | named witness App refused at the clone-intent reservation, after the claim seed | 1 | `production_linux_v2_native_witness_test.go:101: owner-witness command exit status: 1` + captured stderr `-linux-v2-native-owner-witness: reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject` (30.66 s); `DECISION_NEEDED` §7.2 |
| 2 | `claude_containment` | same | 1 | same at `:109` (30.68 s) |
| 3 | `shared` | owner leg; legs `pi-baseline`, `opencode-baseline`, `cleanup-root`, `cleanup-mounts-root`, `run_unexpected_cgroup_procs_fd_witness` pass (`verified TestInstalledLinuxOwnerRejectsUnexpectedCgroupProcsFD: run=1 pass=1 skip=0`) | 1 | three product-test stops, byte-identical to the burndown's §3.15 rows: `clone_cleanup_linux_test.go:259`, `clone_integration_linux_test.go:240` (+ its `:113` cleanup line), `launcher_integration_linux_test.go:439` |
| 4 | `synthetic_goal_cleanup` | test's real-launcher call past the admission refusal; B worker RELEASE refused by the helper | 1 | `production_linux_v2_cleanup_witness_test.go:499: … one-use Linux v2 RELEASE result is unresolved: Linux owner helper rejected operation (release_outcome_unknown)` (1.54 s) |
| 5 | `stage_replay_create_started` | named witness App refused at the config gate | 1 | `:178` + captured `owner witness config native harness version differs from the tested release` (31.29 s) |
| 6 | `stage_replay_leaf_bound` | same | 1 | `:178`, `leaf_bound` (31.18 s) |
| 7 | `stage_replay_launcher_bound` | same | 1 | `:178`, `launcher_bound` (31.49 s) |
| 8 | `ack_loss_launcher_bound` | same | 1 | `:217: wait for exact launcher_bound ACK boundary` + named config gate (31.30 s) |
| 9 | `stale_leaf` | same | 1 | `:630: wait for exact durable stop receipt` + named config gate (31.22 s) |
| 10 | `migration` | same | 1 | `:616: wait for exact OpenCode native_release boundary` + named config gate (31.20 s) |
| 11 | `stale_journal_run_key` | same | 1 | `TestProductionLinuxV2UnresolvedJournalBlocksFreshRun` at `:534` + named config gate (31.25 s) |
| 12 | `stale_journal_run_key_legacy` | same | 1 | `TestProductionLinuxV2StaleOwnerRequestDenials/stale_journal_run_key` at `:269` + named config gate (31.70 s) |
| 13 | `stale_journal_fence` | same | 1 | `…/stale_journal_fence` at `:269` (31.52 s) |
| 14 | `stale_journal_subject` | same | 1 | `…/stale_journal_subject` at `:269` (31.16 s) |

The ten OpenCode-vehicle rows (5-14) keep their F2-excluded version-gate cause, now *named in the
job log from the App's own stderr* instead of only from fixture probes. No previously green row
reddened: row 3's passing legs and the four `retired`/smoke/leg patterns match the burndown's
binding run `45431551609`, and rows 5-14 keep the same failing test names and stages as the
burndown's table (§3.15). Every row's cause changed from "unnamed wait" to a named one; no row was
left at an unnamed barrier wait.

## 7. Classification of the named containment refusal — one fixture class cleared, then `DECISION_NEEDED`

### 7.1 Cleared: the fixed-B native HOME was outside the isolated owner witness root

Run `45452357027` named it from the captured stderr for both rows (`:101`/`:109`):

```
owner-witness command exit status: 1
owner-witness stderr: -linux-v2-native-owner-witness: fixed-B native HOME is outside the isolated owner witness root
```

`daemon/internal/app/linux_owner_witness_linux.go:2902-2904` requires
`validateLinuxV2NativeWitnessPath(home)` (`:1166-1174`: HOME under `/opt/symmetry-owner-test`) on the
passwd HOME of the fixed-B account. The fixture created B with a passwd HOME outside that root
(`useradd --create-home --home-dir`, `scripts/ci/linux-v2-native-acceptance.sh:570`, unchanged since
`41e64851`), i.e. the refusal named a missing fixture precondition (conductor ruling Q1), not a
product defect. Fix in the owned
fixture: `B_HOME="$OWNER_TEST_ROOT/fixed-b-home"` (distinct from the clone tests'
`CLONE_TEST_B_HOME=$OWNER_TEST_ROOT/b-home`), plus a passwd readback fence
(`:612-613`). Every B-path fence and A-view ACL was already `$B_HOME`-relative, so no other byte
moved. Before/after rows: `45452357027` row 1 → `45455325059` and `45458181149` (rows 1-2 proceed to
the refusal below). Probe of the new fence: synthetic passwd rows → positive passes, a row carrying
the old outside-root HOME and a missing user both fire.

### 7.2 `DECISION_NEEDED`: the witness's clone reservation requires an admitted Subject that the witness request validation forbids

Run `45455325059`/`45458181149` captured stderr (one `Fprintf` of a joined error, three lines):

```
-linux-v2-native-owner-witness: reserve owner witness clone before source Git effects: Linux clone intent does not match the current admitted Subject
replay exact v2 reserve standalone clone intent mutation: Linux clone intent does not match the current admitted Subject
```

The two failing requirements, both binding on the same run:

- Product (`daemon/internal/state/linux_clone_journal.go:864-878`, called by
  `ReserveLinuxCloneIntent` `:208-216`): the reservation requires
  `protocol.ParseAdmissionInput(journal.Work.Input)` to report `present` with
  `Subject.ResourceID/Commit/TreeDigest` equal to the clone intent.
- Witness harness (`daemon/e2e/production_linux_v2_native_witness_test.go:1565-1567`) refuses a
  request whose claim carries an admission: `present` → `owner-witness claim must be a real non-Goal
  Control task`. A legacy input (Control's `{"repository_resource_id": …}`) parses as
  `present=false` (`daemon/internal/protocol/goal_admission.go:486-490,540-546`), and Control's
  schema forbids goal fields on a goal-less task (`tasks_goal_fields_all_or_none`,
  `scripts/ci/provision-linux-v2-control-claims.exs:415-421`).

No fixture input can satisfy both: `present` is exactly what the harness refuses, and `present` is
exactly what the state guard needs. The product's witness also cannot avoid the clone —
`prepareClone` requires a `git_clone_v2` workspace (`linux_owner_witness_linux.go:2981`) — so the
witness mode as wired cannot pass `prepareClone` with a legacy Control claim. This is a
product-vs-harness contradiction, and both sides are outside this goal's sanctioned file set
(`daemon/internal/**` needs a ruling; the harness file is owned only for the capture helper), so the
goal records `DECISION_NEEDED` with the evidence above instead of patching either side.

State evidence for the row: exactly one surviving 1268 B RunJournal
(`runs/journal-38667e585e8837cf15329418499245ca3db7538a9dc57f56aecbb495af2eda22.json 1268`),
`installed_owner_verified: true`, `native_binary: null`, `native_open_reached: null` in the witness
document; the reservation writes nothing (the guard fires before the journal mutation), so the
seeded claim journal is the only state.

Smallest proposed fix (fixture/harness, no `daemon/internal/**` change): run the witness on a
Goal-backed claim whose admission Subject equals the request Subject — (a) relax the harness check to
`present && admission.Subject == request.Subject` (one block,
`production_linux_v2_native_witness_test.go:1565-1567`), and (b) provision the Control fixture's
tasks through the Goal/admission flow with `subject` = the fixture's commit/tree_digest (pattern and
its own subject assertion: `scripts/ci/omo-control-native-fixture.exs:190,262-284,297-303,376`;
helper `scripts/ci/provision-linux-v2-control-claims.exs`). That needs the owned set to grow by the
harness block; the alternative — exempting the witness from the shared clone-v2 admission guard —
would weaken a production guard (`daemon/internal/**`, rejected here).

Consequence for the contract's judging clause: the containment rows now carry the named post-seed
refusal and end at a recorded `daemon/internal/**`-rooted cause, which is the contract's second
allowed outcome for those rows; the rows do not reach the credential-dependent step.

## 8. Gates (final tree; exact commands, exit codes)

| Command (cwd) | Exit | Result |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`) | 0 | 0 lines |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` (`daemon/`) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` (`daemon/`) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o <tmp> ./e2e` then the binary `-test.run 'TestLinuxV2OwnerWitnessStderrCapture'` (WSL Ubuntu-24.04, linux/amd64) | 0 | both new tests PASS |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL Ubuntu-24.04, python 3.12.3) | 0 | `Ran 43 tests … OK` |
| `python3 scripts/ci/test-linux-v2-go-events.py` (same) | 0 | clean |
| `python3 -m py_compile scripts/ci/export-linux-v2-redacted-evidence.py` (same) | 0 | clean |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` | 0 | clean |
| `shellcheck --severity=warning scripts/ci/linux-v2-native-acceptance.sh` (0.11.0) | 1 | 11 findings; finding set byte-identical to the base script apart from line numbers (delta 0); SC histogram `SC1078 ×2, SC2024, SC2043, SC2154 ×8, SC2174, SC2276 ×3, SC2288, SC2289 ×2` identical at base |
| `actionlint .github/workflows/scratch-native-containment-witness.yml` (throwaway, 1.7.12) | 0 | clean; the real workflow is untouched, so no row for it |

Changed-document gates, run in the scratch worktree over a temporary, never-pushed local commit of
this tree plus `docs/TODO.md`, `docs/archive/CLOSED-TODO.md` and this record (each amendment was
re-committed and the gates re-run; the last run is the row below, so the temporary head is not a
delivered artifact):

| Command (`scripts/ci`, `--base 477e4c62 --head <temporary commit>`) | Exit | Result |
| --- | --- | --- |
| `python3 check-record-cleanliness.py` | 0 | `1 changed record(s) pass the root-only check` (the first attempt refused the fixed-B legacy home path quoted in §7.1; the sentence was rewritten to a placeholder) |
| `python3 check-citation-existence.py` | 0 | `1 changed document(s) pass the path and symbol check (593 path citation(s))`; three pre-existing `CLOSED-TODO.md` notes name gitignored paths |
| `python3 jscpd-changed.py` | not run | the pinned `node_modules/jscpd` is not installed on this host and the registry is unreachable (the burndown's round-1 constraint); replacement proxy: every 10-line whitespace-normalized window containing an added line was searched against the whole head tree (1,798 scanned-format files, 647,423 distinct windows) → 0 clone candidates |

The Go unit tests, `gofmt`, `go vet` and the fixture-contract scripts run in the `daemon` CI job
(`.github/workflows/ci.yml`); that job is triggered by `pull_request`, not by a scratch-branch push
(`ci.yml` pushes to `main` only), so this stage's rows for them are the local runs above. The e2e
witnesses themselves need the installed A/B fixture and were exercised only by the hosted scratch
runs, as the contract allows. Git-diff hygiene: `git diff --check` clean; the profile-name and
host-path scans used by the burndown apply (no Windows/WSL host prefixes in added lines).
The apply stage re-ran the rows above on the final bytes; exit codes, counts and the document
gates are in §12.

## 9. Scope and scratch hygiene

Owned files touched: the three `daemon/e2e` files (§2, §3), the acceptance script (§4, §7.1), the
exporter and its fixture suite (§5), the plan, this record, `docs/TODO.md` and
`docs/archive/CLOSED-TODO.md`. Not touched: `daemon/internal/**` (no byte), `control/**`, the real
workflow (no byte), the frozen OpenCode pin/render tokens, any environment/secrets/vars/trigger.

Scratch loop: the branch was pushed twice (runs 1-3, then run 4 after the comment trim) and deleted
twice with `git push origin --delete scratch/rf-acc-native-containment-witness` →
`- [deleted] scratch/rf-acc-native-containment-witness`; `git fetch --prune origin` removed the
remote-tracking ref each time, and after the final deletion
`git ls-remote origin scratch/rf-acc-native-containment-witness` prints nothing (0 lines, exit 0).
The local branch and the throwaway worktree retain the four scratch heads for a follow-up re-push;
the two scratch-only files never existed in the goal worktree (no untracked scratch files to
delete, unlike the burndown's apply-stage finding). The local gate commit that carries this record
for the changed-document gates was discarded by resetting the local scratch branch to its run head.

Scratch budget: the contract's cap is the named classes (containment unnamed stop, cleanup admission
refusal) plus the fixture class the captured refusal named (the fixed-B HOME) plus 2 = 5 runs. Used:
4 dispatches (`45452357027`, `45455325059`, `45458181149`, `45465331434`; the fourth re-bound the
bytes after the two-line comment-policy trim changed one Go file). `45455325059`'s first attempt
died in the unrelated `contracts` flake and was rerun in-run, so no dispatch was spent on it. One
remains in reserve for a flake re-dispatch; the Change-1/Change-2 mutant rows cite the before run
`45388258526` as the contract allows.

## 10. Appendix — scratch pins and mutants used as evidence (verbatim, full sha256)

| Artifact | sha256 |
| --- | --- |
| `.github/workflows/scratch-native-containment-witness.yml` (throwaway, final all-14 form; identical bytes at heads `1ff5ad44` and `65bfeaf2`) | `a3aa2b1c17f6ff3b2d2285e149ba422c9c8b017982e3b6248f20ceeac3eb3566` |
| `scripts/ci/scratch-gh-shim` (ratified in rf-acc-native-hosted-run; byte-identical to `git show scratch/rf-acc-native-burndown:scripts/ci/scratch-gh-shim`) | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` |

Scratch heads: `05ec6ec8` (run 1, narrow), `305a5d0f` (run 2, narrow, B-HOME fix), `1ff5ad44`
(run 3, all 14), `65bfeaf2` (run 4, all 14, the binding head). Dummy values used in every run:
`gho_scratchdummy0000000000000000000000000000`, `sk-scratch-dummy`,
`deepseek-v4.1-flash-fast` models. Log scan of `45465331434`:
`gho_scratchdummy` ×1, `sk-scratch-dummy` ×1, no `ghp_`/`github_pat_`/`Bearer` credentials, and the
45 `lease_token` matches are Control debug-SQL column lists (`r0."lease_token"`), not values. The
captured App stderr lines in the job log carry no credential value: the report refuses to emit a
tail that still contains one (§2), and the fixture's printer independently drops >400-byte or
secret-bearing lines.

## 11. Residual risks and known limits

- The containment judging clause stays unmet beyond the recorded stop: no run reaches the
  credential-dependent step, because the witness cannot reserve its clone until §7.2 is decided.
  Every credential-dependent step of every row remains unproven (the scratch doubles reach no
  provider turn and no GitHub provider action).
- `synthetic_goal_cleanup` stops at the helper's `release_outcome_unknown` with no helper-side error
  text in the log (the helper's response carries a code and stop evidence only; the fixture prints
  neither). Root-causing it needs a bounded owner-service journal dump in the fixture's failure
  context — deliberately not added here, because the contract's Change 2 clause is satisfied by the
  later named check and the class is not one the captured refusal names.
- `45455325059`'s first attempt reddened the `contracts` job on the known,
  cause-unknown frontend flake already filed in `docs/TODO.md` (`goal-attention.test.tsx`, rerun
  green); one infra flake per five dispatches is the observed rate.
- The fixed-B HOME move changes every set's B account HOME; run `45458181149` shows no regression
  (passing legs, smokes, retirement counts and failure names match the pre-change baseline), but
  only the sets' *witness* paths were re-exercised — no other repository fixture uses this account.
- The local shellcheck baseline differs in count from the burndown record's (11 findings on this
  host's shellcheck 0.11.0 versus its 16); the delta versus the base script is the binding number
  and is 0.
- `docs/TODO.md`'s `linux-native-v2-acceptance` dispatch entry and the burndown's other follow-ups
  are untouched: this goal narrows the containment follow-up to §7.2 and closes the capture/cleanup
  half.
- The hosted all-14 run was produced on the pre-apply copies of the §1 rows; §12 records the four
  behavior-neutral apply deltas and leaves the hosted re-proof (or a formal acceptance of the
  amendment) to the conductor.

## 12. Apply stage (2026-11-24) — accepted simplifications and the binding amendment

Source: this goal's simplify-stage report (run `symmetry-rf-acc-native-containment-witness-simpl`,
findings F1-F4). Applied: F1, F2, F3's in-scope half, F4. All four are behavior-neutral; the
paragraphs below name the evidence and each probe re-runs with one command.

- **F1 (Delete)** — `linuxV2ReportOwnerWitnessFailure`'s `len(lines) == 0` branch
  (`owner_cli_test.go:1589-1592`) was unreachable: `linuxV2OwnerWitnessStderrTail` returns either
  an error or `strings.Split`'s result, whose length is ≥ 1 for every input (probe P1), so the
  `capture is empty` line could never print. Deleted; the function now ends at `:1592` and the
  eight `linuxV2ReportOwnerWitnessFailure` call sites shifted to `:1744`, `:2377`, `:3387`,
  `:3566`, `:3797`, `:3963`, `:4122`, `:4444` (§2 and plan §2 carry the shifted refs).
- **F2 (Delete)** — the `assertNotIn("owner-witness.stderr", …)` line the implement stage added
  to `test_manual_containment_export_records_pinned_native_binary`
  (`test-linux-v2-evidence.py:1600`) could not fail: the output directory receives only the
  allowlisted names plus `manifest.json`, and a root without the capture never yields it. Mutant
  P2 (guard call lines removed from a disposable copy) reddens only the dedicated refusal test
  and leaves the export test `ok`, i.e. the deleted line detected nothing about the guard. Q3's
  coverage stays with `test_manual_containment_refuses_the_owner_witness_stderr_capture` (§5).
- **F3 (Refactor, in-scope half)** — `linuxV2KnownCredentialValues`
  (`native_witness_test.go:2025`) now checks `!linuxV2IsCredentialEnvironmentName(key)` alone;
  the dropped `key != "SYMMETRY_OPERATOR_TOKEN" && key != "SYMMETRY_ENROLLMENT_TOKEN"` disjuncts
  were provable no-ops: the predicate is case-insensitive and both names end in `_TOKEN` (probe
  P1: 0 mismatches over 10 keys; exhaustively, a matching name always already skips and a
  non-matching name is neither literal). The pre-existing writer's copy
  (`native_witness_test.go:3372`) is outside this goal's owned region ("only the shared
  redaction/capture helper") and stays untouched; only the increment's own copy is gone, so the
  helper no longer carries the duplicated literals.
- **F4 (Refactor)** — the comment above `print_witness_failure_context`
  (`linux-v2-native-acceptance.sh:1665-1666`) was rewritten to drop the falsified premise that
  the command's stderr is discarded by its own harness (§2, §6, §7.1 show the captured, redacted
  tail in the job log). Same line count; the fixture-state clause stays true.

Binding amendment: the §1 rows are the final bytes. Run `45465331434` was produced on scratch
head `65bfeaf2`, byte-identical to the pre-apply copies of those rows; the four deltas above are
behavior-neutral by P1/P2 and by inspection (F4 touches comment text only). Re-proving the
acceptance clause on the final bytes needs a scratch push and one fresh literal all-14 run; this
stage cannot push, so the re-proof — or a formal acceptance of this amendment — is
conductor-owned (one dispatch of the contract's budget remains).

Census for the "every" clauses this stage touches:

- "redacts every credential value the harness already knows": `linuxV2IsCredentialEnvironmentName`
  call sites, 6 total — definition `native_witness_test.go:1991`; helper `:2025` (converted by
  F3); writer `:3372` (exempt: pre-existing, outside the owned region); `owner_cli_test.go:117`,
  `:133` and `production_linux_v2_rootless_compose_witness_test.go:1115` (pre-existing env
  filtering, untouched — the rootless witness already uses the plain-predicate form F3 adopts).
- Change 1's "all eight call sites": `linuxV2ReportOwnerWitnessFailure(t, fixture, command)`
  appears 8 times, one per wait site, unchanged.
- Q3's "all three modes": `require_no_excluded_witness_file` definition
  (`export-linux-v2-redacted-evidence.py:707`) plus call sites `:1520`, `:1544`, `:1553` — all
  three present, untouched.

Apply-stage evidence appendix (probes; each re-runnable in one command):

P1 — Go equivalence probe (source sha256
`5efac1a3f5f0ba47f4380e844d3ee35bb3531add7054b3143622937f927aaf3a`), built by inlining the
predicate from `production_linux_v2_native_witness_test.go:1991-1999` (checked byte-identical
with `diff`):

```go
func main() {
	fmt.Println("len(strings.Split(\"\", \"\\n\")) =", len(strings.Split("", "\n")))
	keys := []string{"SYMMETRY_OPERATOR_TOKEN", "SYMMETRY_ENROLLMENT_TOKEN", "symmetry_operator_token", "Symmetry_Enrollment_Token", "SYMMETRY_API_KEY", "PATH", "SYMMETRY_OPERATOR_TOKENX", "", "symmetry_credential", "FOO_TOKEN"}
	mismatch := 0
	for _, key := range keys {
		skipOld := !linuxV2IsCredentialEnvironmentName(key) && key != "SYMMETRY_OPERATOR_TOKEN" && key != "SYMMETRY_ENROLLMENT_TOKEN"
		skipNew := !linuxV2IsCredentialEnvironmentName(key)
		if skipOld != skipNew {
			mismatch++
		}
		fmt.Printf("%-28s skipOld=%-5v skipNew=%-5v equal=%v\n", key, skipOld, skipNew, skipOld == skipNew)
	}
	fmt.Println("mismatches:", mismatch)
}
```

Output: `len(strings.Split("", "\n")) = 1`; ten rows all `equal=true`; `mismatches: 0`.

P2 — exporter-guard mutant: copy `scripts/ci` to a disposable directory outside the repository,
drop the three guard call lines from the copy's exporter, run the copy's suite:

```
grep -v "require_no_excluded_witness_file((witness_root, receipt_root))" export-linux-v2-redacted-evidence.py \
  | grep -v "require_no_excluded_witness_file((witness_root,))" > tmp.py
mv tmp.py export-linux-v2-redacted-evidence.py
python3 test-linux-v2-evidence.py
```

Mutant exporter sha256
`f556c15b31cd1b6ef2699a3c74f1a15c9447243ff6514bb73cb6e5aa453bb221`. Result:
`FAILED (failures=1, skipped=1)` with the single failure
`test_manual_containment_refuses_the_owner_witness_stderr_capture` (`AssertionError: 0 == 0`),
and `test_manual_containment_export_records_pinned_native_binary … ok`.

Re-run gate table (apply stage; exit codes as measured). The code rows ran on the §1 code bytes;
the document gates were re-run after this section landed, over the final record bytes, against a
`git stash create` commit of the working tree (which leaves the worktree and the index untouched):

| Command (environment) | Exit | Result |
| --- | --- | --- |
| `gofmt -l .` (`daemon/`, WSL Ubuntu-24.04) | 0 | 0 lines |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./...` (`daemon/`) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...` (`daemon/`) | 0 | clean |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o <tmp> ./e2e` then the binary `-test.run TestLinuxV2OwnerWitnessStderrCapture -test.v` (WSL, linux/amd64) | 0 | both tests PASS |
| `python3 scripts/ci/test-linux-v2-evidence.py` (WSL, python 3.12.3) | 0 | `Ran 43 tests in 32.190s` … `OK` |
| `python3 scripts/ci/test-linux-v2-go-events.py` (same) | 0 | `Ran 6 tests` … `OK` |
| `python3 -m py_compile scripts/ci/export-linux-v2-redacted-evidence.py` (same) | 0 | clean |
| `bash -n scripts/ci/linux-v2-native-acceptance.sh` (same) | 0 | clean |
| `shellcheck --severity=warning -f gcc` on the script, Windows 0.11.0 and WSL 0.9.0 | 1 | 16 findings on both (3 error, 13 warning); histogram `SC1078, SC2024, SC2043, SC2154 ×8, SC2174, SC2276 ×2, SC2288, SC2289`; line:col-stripped multiset delta versus the base `477e4c62` script and versus the pre-apply `1f2c8a13` bytes: 0 and 0 |
| `git diff --check` | 0 | clean |
| `python3 scripts/ci/check-record-cleanliness.py --base 1f2c8a13 --head <stash-create commit>` | 0 | `1 changed record(s) pass the root-only check`; with `--base 477e4c62`: `3 changed record(s) pass` |
| `python3 scripts/ci/check-citation-existence.py --base 1f2c8a13 --head <stash-create commit>` | 0 | no audited document added or modified; with `--base 477e4c62`: `1 changed document(s) pass the path and symbol check (600 path citation(s))`, the three pre-existing ignore notes unchanged |

The §8/§11 shellcheck count (11 findings) is not reproducible with either installed binary today
(both report 16); the binding number, the delta versus the base script, is 0. `actionlint` has no
row: the real workflow is untouched. The four changed code files' sha256 were re-checked after the
last edit to this record (which touches only this file) and still equal the §1 rows.
